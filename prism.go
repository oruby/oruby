package oruby

/*
#cgo CFLAGS: -I${SRCDIR}/mruby/build/prism/include -I${SRCDIR}/mruby/mrbgems/mruby-compiler/lib/prism/include
#include <stdlib.h>
#include "go-prism.h"
*/
import "C"

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Source rewriting for Ruby constructs mruby's code generator rejects. The
// source is parsed with Prism (the parser inside libmruby, go-prism.c); each
// rejected node is replaced, by its exact byte span, with equivalent code that
// mruby compiles, usually a call to a Kernel helper defined in mruby.go:
//
//   - alias/undef with dynamic symbols  -> Kernel.__oruby_alias__/__oruby_undef__
//   - **nil parameters                  -> **__no_kw_disallow__
//   - rescue => a&.b, for a&.b in c      -> plain targets plus an assignment
//   - BEGIN { ... }                      -> begin ... end
//   - flip-flops                         -> Kernel.__oruby_ff__ with lazy operands
//   - A::B ||= v, A::B &&= v, A::B op= v -> Kernel.__oruby_cpath_*__
//   - alias $new $old                    -> references to $new compile as $old
//   - integer literals over 255 digits   -> Integer("...")
//
// Rewrites never add or remove newlines, so line numbers stay as in the source.

var (
	prismMu     sync.Mutex
	ffCounter   uint64
	gvarAliasMu sync.RWMutex
	// alias $new $old, process wide: "$new" -> "$old". Seeded with the
	// English library's names: a file is compiled before its own
	// require "English" runs, so those aliases must be known up front
	gvarAliases = map[string]string{
		"$ERROR_INFO": "$!", "$ERROR_POSITION": "$@", "$FS": "$;", "$FIELD_SEPARATOR": "$;",
		"$OFS": "$,", "$OUTPUT_FIELD_SEPARATOR": "$,", "$RS": "$/", "$INPUT_RECORD_SEPARATOR": "$/",
		"$ORS": "$\\", "$OUTPUT_RECORD_SEPARATOR": "$\\", "$NR": "$.", "$INPUT_LINE_NUMBER": "$.",
		"$LAST_READ_LINE": "$_", "$DEFAULT_OUTPUT": "$>", "$DEFAULT_INPUT": "$<", "$PID": "$$",
		"$PROCESS_ID": "$$", "$CHILD_STATUS": "$?", "$LAST_MATCH_INFO": "$~", "$ARGV": "$*",
		"$MATCH": "$&", "$PREMATCH": "$`", "$POSTMATCH": "$'", "$LAST_PAREN_MATCH": "$+",
		// MRI's built-in aliases
		"$PROGRAM_NAME": "$0", "$-v": "$VERBOSE", "$-w": "$VERBOSE", "$-d": "$DEBUG",
		"$-0": "$/", "$-F": "$;",
	}
)

type prismRec struct {
	kind, flags int
	s, e        [C.ORB_SPANS]int
}

func prismScan(src []byte) ([]prismRec, bool) {
	if len(src) == 0 {
		return nil, true
	}
	prismMu.Lock()
	defer prismMu.Unlock()
	var n, errs C.int
	p := C._oruby_prism_scan(nil, (*C.char)(unsafe.Pointer(&src[0])), C.size_t(len(src)), &n, &errs)
	if p == nil {
		return nil, errs == 0
	}
	defer C.free(unsafe.Pointer(p))
	raw := unsafe.Slice(p, int(n))
	recs := make([]prismRec, len(raw))
	for i, r := range raw {
		recs[i].kind, recs[i].flags = int(r.kind), int(r.flags)
		for j := 0; j < C.ORB_SPANS; j++ {
			recs[i].s[j], recs[i].e[j] = int(r.s[j]), int(r.e[j])
		}
	}
	return recs, errs == 0
}

// mayNeedRewrite is a cheap filter before parsing: every rewrite site
// contains one of these
func mayNeedRewrite(src []byte) bool {
	for _, k := range []string{"**nil", "alias", "undef", "&.", "BEGIN", "..", "||=", "&&=", "::"} {
		if bytes.Contains(src, []byte(k)) {
			return true
		}
	}
	if i := bytes.IndexByte(src, '$'); i >= 0 && i+1 < len(src) && hasAliasedGvar(src) {
		return true
	}
	digits := 0
	for _, c := range src {
		if c >= '0' && c <= '9' || c == '_' {
			digits++
			if digits > 255 {
				return true
			}
		} else {
			digits = 0
		}
	}
	return false
}

type edit struct {
	start, end int
	build      func(r *rewriter) string
}

type rewriter struct {
	src   []byte
	edits []edit
}

func (r *rewriter) text(s, e int) string {
	if s < 0 {
		return ""
	}
	return string(r.src[s:e])
}

// render returns src[s:e] with the edits inside it applied, outermost first
func (r *rewriter) render(s, e int) string {
	if s < 0 {
		return ""
	}
	var b strings.Builder
	pos := s
	for i := 0; i < len(r.edits); i++ {
		ed := r.edits[i]
		if ed.start < pos || ed.end > e || ed.start > e {
			continue
		}
		b.Write(r.src[pos:ed.start])
		b.WriteString(ed.build(r))
		pos = ed.end
		// skip edits nested in this one: the builder rendered them
		for i+1 < len(r.edits) && r.edits[i+1].start < ed.end {
			i++
		}
	}
	b.Write(r.src[pos:e])
	return b.String()
}

func symArg(s string) string {
	if strings.HasPrefix(s, ":") {
		return s
	}
	return ":" + s
}

// hasAliasedGvar reports whether src mentions an aliased global variable
func hasAliasedGvar(src []byte) bool {
	gvarAliasMu.RLock()
	defer gvarAliasMu.RUnlock()
	for i := 0; i < len(src); i++ {
		if src[i] != '$' {
			continue
		}
		j := i + 1
		if j+1 < len(src) && src[j] == '-' {
			j += 2
		}
		for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
			j++
		}
		if _, ok := gvarAliases[string(src[i:j])]; ok && j > i+1 {
			return true
		}
	}
	return false
}

// readOnlyGvar reports match-derived globals ($& $` $' $+ $1...)
func readOnlyGvar(name string) bool {
	switch name {
	case "$&", "$`", "$'", "$+":
		return true
	}
	return len(name) > 1 && name[1] >= '1' && name[1] <= '9'
}

func resolveGvar(name string) string {
	gvarAliasMu.RLock()
	defer gvarAliasMu.RUnlock()
	for i := 0; i < 8; i++ {
		t, ok := gvarAliases[name]
		if !ok {
			break
		}
		name = t
	}
	return name
}

// PreprocessRubyBytes rewrites the constructs mruby can't compile; source
// that has none, or that doesn't parse, is returned unchanged.
func PreprocessRubyBytes(src []byte) []byte {
	if !mayNeedRewrite(src) {
		return src
	}
	recs, ok := prismScan(src)
	if !ok || len(recs) == 0 {
		return src
	}
	r := &rewriter{src: src}
	// aliases first: later references in this source use them too
	for _, rc := range recs {
		if rc.kind == C.ORB_ALIAS_GVAR {
			nw, old := r.text(rc.s[1], rc.e[1]), resolveGvar(r.text(rc.s[2], rc.e[2]))
			if nw != old {
				gvarAliasMu.Lock()
				gvarAliases[nw] = old
				gvarAliasMu.Unlock()
			}
		}
	}
	for i := 0; i < len(recs); i++ {
		rc := recs[i]
		s, e := rc.s, rc.e
		switch rc.kind {
		case C.ORB_NO_KEYWORDS:
			r.add(s[0], e[0], func(*rewriter) string { return "**__no_kw_disallow__" })
		case C.ORB_ALIAS_DYNAMIC:
			r.add(s[0], e[0], func(r *rewriter) string {
				return "::Kernel.__oruby_alias__(self, " + symArg(r.render(s[1], e[1])) + ", " + symArg(r.render(s[2], e[2])) + ")"
			})
		case C.ORB_UNDEF_DYNAMIC:
			var names [][2]int
			for i+1 < len(recs) && recs[i+1].kind == C.ORB_LIST_ITEM {
				i++
				names = append(names, [2]int{recs[i].s[0], recs[i].e[0]})
			}
			r.add(s[0], e[0], func(r *rewriter) string {
				parts := make([]string, len(names))
				for k, n := range names {
					parts[k] = symArg(r.render(n[0], n[1]))
				}
				return "::Kernel.__oruby_undef__(self, " + strings.Join(parts, ", ") + ")"
			})
		case C.ORB_RESCUE_SAFE_NAV:
			target := r.text(s[0], e[0])
			r.add(s[0], e[0], func(*rewriter) string {
				return "__rescued_exc__; " + target + " = __rescued_exc__"
			})
		case C.ORB_FOR_SAFE_NAV:
			target := r.text(s[0], e[0])
			r.add(s[0], e[0], func(*rewriter) string { return "__for_elem__" })
			at := e[1]
			if s[2] >= 0 {
				at = e[2]
			}
			r.add(at, at, func(*rewriter) string { return "; " + target + " = __for_elem__;" })
		case C.ORB_BEGIN_BLOCK:
			r.add(s[0], e[1], func(*rewriter) string { return "begin " })
			r.add(s[2], e[2], func(*rewriter) string { return " end" })
		case C.ORB_FLIP_FLOP:
			id := atomic.AddUint64(&ffCounter, 1)
			flags := rc.flags
			r.add(s[0], e[0], func(r *rewriter) string {
				l, rt := r.render(s[1], e[1]), r.render(s[2], e[2])
				if flags&2 != 0 {
					l = "$. == " + l
				}
				if flags&4 != 0 {
					rt = "$. == " + rt
				}
				return "::Kernel.__oruby_ff__(" + strconv.FormatUint(id, 10) + ", " + strconv.FormatBool(flags&1 != 0) +
					", ->{ " + l + " }, ->{ " + rt + " })"
			})
		case C.ORB_CPATH_OR_WRITE, C.ORB_CPATH_AND_WRITE, C.ORB_CPATH_OP_WRITE:
			kind := rc.kind
			r.add(s[0], e[0], func(r *rewriter) string {
				parent := "::Object"
				if s[1] >= 0 {
					parent = r.render(s[1], e[1])
				}
				name := ":" + r.text(s[2], e[2])
				value := r.render(s[3], e[3])
				switch kind {
				case C.ORB_CPATH_OR_WRITE:
					return "::Kernel.__oruby_cpath_or__(" + parent + ", " + name + ") { " + value + " }"
				case C.ORB_CPATH_AND_WRITE:
					return "::Kernel.__oruby_cpath_and__(" + parent + ", " + name + ") { " + value + " }"
				}
				op := strings.TrimSuffix(r.text(s[4], e[4]), "=")
				return "::Kernel.__oruby_cpath_op__(" + parent + ", " + name + ", :" + op + ") { " + value + " }"
			})
		case C.ORB_ALIAS_GVAR:
			nw, old := r.text(s[1], e[1]), r.text(s[2], e[2])
			r.add(s[0], e[0], func(*rewriter) string {
				return "::Kernel.__oruby_alias_gvar__(:" + nw + ", :" + old + ")"
			})
		case C.ORB_GVAR_REF:
			name := r.text(s[0], e[0])
			if t := resolveGvar(name); t != name {
				if s[1] >= 0 && readOnlyGvar(t) {
					// $alias_of_backref = v is MRI's runtime NameError, not a syntax error
					r.add(s[1], e[1], func(*rewriter) string {
						return `::Kernel.raise(::NameError, "` + name + ` is a read-only variable")`
					})
				} else {
					r.add(s[0], e[0], func(*rewriter) string { return t })
				}
			}
		case C.ORB_BIG_INTEGER:
			r.add(s[0], e[0], func(r *rewriter) string { return `Integer("` + r.text(s[0], e[0]) + `")` })
		}
	}
	if len(r.edits) == 0 {
		return src
	}
	sort.SliceStable(r.edits, func(a, b int) bool {
		if r.edits[a].start != r.edits[b].start {
			return r.edits[a].start < r.edits[b].start
		}
		return r.edits[a].end > r.edits[b].end
	})
	return []byte(r.render(0, len(src)))
}

func (r *rewriter) add(s, e int, build func(r *rewriter) string) {
	if s < 0 || e < s {
		return
	}
	r.edits = append(r.edits, edit{s, e, build})
}

// PreprocessRuby is PreprocessRubyBytes for strings.
func PreprocessRuby(source string) string {
	b := PreprocessRubyBytes([]byte(source))
	return string(b)
}
