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
//   - END { ... }                        -> Kernel.__oruby_end__(site) { ... }
//   - frozen string literals             -> "...".-@ (frozen_string_literal)
//   - __ENCODING__ in non-UTF-8 source   -> ::Encoding.find("<source encoding>")
//   - **obj in a call or Hash literal    -> **::Kernel.__oruby_kwsplat((obj)) (#to_hash)
//   - [a, k: v]                          -> [a, {k: v}] (mruby drops the brace-less Hash)
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
		// MRI's built-in aliases ($-0, $-F, $-v, $-w, $PROGRAM_NAME and $> are
		// virtual globals sharing their variable's value, go-mrb.c)
		"$-d": "$DEBUG",
	}
)

type prismRec struct {
	kind, flags int
	s, e        [C.ORB_SPANS]int
}

// PreprocessOptions are per-source settings for the rewriter
type PreprocessOptions struct {
	// SourceEncoding is the encoding of source without a magic comment
	// (the main script under -K); empty means UTF-8
	SourceEncoding string
}

// frozenStringDefault is --enable/--disable-frozen-string-literal: -1 unset
var frozenStringDefault int32 = -1

// SetFrozenStringLiteral sets the frozen_string_literal default for all
// source compiled afterwards, as MRI's --enable-frozen-string-literal does
func SetFrozenStringLiteral(frozen bool) {
	if frozen {
		atomic.StoreInt32(&frozenStringDefault, 1)
	} else {
		atomic.StoreInt32(&frozenStringDefault, 0)
	}
}

func prismScan(src []byte, opts *PreprocessOptions) ([]prismRec, string, bool) {
	if len(src) == 0 {
		return nil, "", true
	}
	prismMu.Lock()
	defer prismMu.Unlock()
	var n, errs C.int
	var so C.orb_scan_opts
	so.frozen = C.int(atomic.LoadInt32(&frozenStringDefault))
	if opts != nil && opts.SourceEncoding != "" {
		cs := C.CString(opts.SourceEncoding)
		defer C.free(unsafe.Pointer(cs))
		so.encoding = cs
	}
	var encName [64]C.char
	p := C._oruby_prism_scan(nil, (*C.char)(unsafe.Pointer(&src[0])), C.size_t(len(src)), &so, &n, &errs, &encName[0], C.size_t(len(encName)))
	enc := C.GoString(&encName[0])
	if p == nil {
		return nil, enc, errs == 0
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
	return recs, enc, errs == 0
}

// mayNeedRewrite is a cheap filter before parsing: every rewrite site
// contains one of these
func mayNeedRewrite(src []byte, opts *PreprocessOptions) bool {
	if atomic.LoadInt32(&frozenStringDefault) == 1 || opts != nil && opts.SourceEncoding != "" {
		return true
	}
	if hasMultiAssign(src) || hasRegexpEncFlag(src) {
		return true
	}
	if hasArrayKwHash(src) {
		return true
	}
	// "**": **obj (ORB_KWSPLAT)
	for _, k := range []string{"**", "alias", "undef", "&.", "BEGIN", "END", "..", "||=", "&&=", "::",
		"__ENCODING__", "frozen_string_literal", "coding"} {
		if bytes.Contains(src, []byte(k)) {
			return true
		}
	}
	if i := bytes.IndexByte(src, '$'); i >= 0 && i+1 < len(src) && (hasAliasedGvar(src) || hasReadonlyGvar(src)) {
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
	seq        int  // order added
	closer     bool // inserted after a span (addAfter)
	from       int  // a closer's span start
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

// readonlyGvars cannot be assigned from Ruby code
var readonlyGvars = map[string]bool{
	"$!": true, "$:": true, "$\"": true, "$<": true, "$FILENAME": true, "$?": true,
	"$-a": true, "$-l": true, "$-p": true, "$$": true, "$LOAD_PATH": true, "$LOADED_FEATURES": true,
}

// hasMultiAssign: a line with a comma before a lone "=" may be a, b = v
// hasRegexpEncFlag: an interpolation and what may close a regexp with an
// encoding flag (/n, }u, ...)
func hasRegexpEncFlag(src []byte) bool {
	if !bytes.Contains(src, []byte("#")) {
		return false
	}
	for i := 1; i < len(src); i++ {
		if strings.IndexByte("/})]>!|", src[i-1]) < 0 {
			continue
		}
		j, enc := i, false
		for j < len(src) && strings.IndexByte("mixounse", src[j]) >= 0 {
			enc = enc || strings.IndexByte("nues", src[j]) >= 0
			j++
		}
		if enc && (j == len(src) || !(src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9' || src[j] == '_')) {
			return true
		}
	}
	return false
}

func hasMultiAssign(src []byte) bool {
	for _, line := range bytes.Split(src, []byte("\n")) {
		c := bytes.IndexByte(line, ',')
		if c < 0 {
			continue
		}
		rest := line[c:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == '=' && (i+1 >= len(rest) || rest[i+1] != '=' && rest[i+1] != '>' && rest[i+1] != '~') &&
				(i == 0 || bytes.IndexByte([]byte("=!<>+-*/%&|^"), rest[i-1]) < 0) {
				// a, b = x, y or a, b = [..] needs no conversion: only a
				// single value that is not an Array literal does
				rhs := bytes.TrimSpace(rest[i+1:])
				if end := bytes.IndexAny(rhs, ";)"); end >= 0 {
					rhs = rhs[:end]
				}
				if len(rhs) > 0 && rhs[0] != '[' && rhs[0] != '*' && bytes.IndexByte(rhs, ',') < 0 {
					return true
				}
			}
		}
	}
	return false
}

// hasArrayKwHash: a line where a "[" is followed, before its "]", by a
// "=>" or a "label: " (ORB_ARRAY_KWHASH, [a, k: v])
func hasArrayKwHash(src []byte) bool {
	for len(src) > 0 {
		line := src
		if i := bytes.IndexByte(src, '\n'); i >= 0 {
			line, src = src[:i], src[i+1:]
		} else {
			src = nil
		}
		open := bytes.IndexByte(line, '[')
		if open < 0 {
			continue
		}
		rest := line[open+1:]
		if c := bytes.IndexByte(rest, ']'); c >= 0 {
			rest = rest[:c]
		}
		if bytes.Contains(rest, []byte("=>")) {
			return true
		}
		for i := 1; i+1 < len(rest); i++ {
			if rest[i] == ':' && rest[i+1] == ' ' && rest[i-1] != ':' && rest[i-1] != ' ' {
				return true
			}
		}
	}
	return false
}

// rubyStrEscape escapes s for a double-quoted Ruby string
func rubyStrEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `#`, `\#`).Replace(s)
}

// hasReadonlyGvar reports whether src mentions a read-only global variable
func hasReadonlyGvar(src []byte) bool {
	for i := bytes.IndexByte(src, '$'); i >= 0 && i < len(src); {
		for name := range readonlyGvars {
			if bytes.HasPrefix(src[i:], []byte(name)) {
				return true
			}
		}
		j := bytes.IndexByte(src[i+1:], '$')
		if j < 0 {
			break
		}
		i += j + 1
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
	return PreprocessRubyWith(src, nil)
}

var endSiteCounter, masgnSiteCounter uint64

// PreprocessRubyWith is PreprocessRubyBytes with per-source options
func PreprocessRubyWith(src []byte, opts *PreprocessOptions) []byte {
	if !mayNeedRewrite(src, opts) {
		return src
	}
	recs, encName, ok := prismScan(src, opts)
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
			if s[2] >= 0 && readonlyGvars[resolveGvar(name)] {
				// assigning a read-only global is a NameError; the value is still
				// evaluated. (Checked globals such as $/ are virtual globals with
				// a setter, go-mrb.c; these are written by oruby's own C and Go.)
				r.add(s[1], e[1], func(r *rewriter) string {
					return `::Kernel.__oruby_gv_readonly("` + rubyStrEscape(name) + `", (` + r.render(s[2], e[2]) + `))`
				})
				break
			}
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
		case C.ORB_END_BLOCK:
			id := atomic.AddUint64(&endSiteCounter, 1)
			r.add(s[0], e[1], func(*rewriter) string {
				return "::Kernel.__oruby_end__(" + strconv.FormatUint(id, 10) + ") {"
			})
		case C.ORB_FROZEN_STRING:
			r.addAfter(s[0], e[0], func(*rewriter) string { return ".-@" })
		case C.ORB_MASGN_VALUE:
			// a, b = v -> __oruby_masgn(id, (a, b = __oruby_to_ary(id, (v)))):
			// the value is converted, the expression still returns v. The
			// inserts are zero-width, so the code inside is rewritten as usual
			id := strconv.FormatUint(atomic.AddUint64(&masgnSiteCounter, 1), 10)
			r.add(s[1], s[1], func(*rewriter) string { return "::Kernel.__oruby_masgn(" + id + ", (" })
			r.add(s[0], s[0], func(*rewriter) string { return "::Kernel.__oruby_to_ary(" + id + ", (" })
			r.addAfter(s[0], e[0], func(*rewriter) string { return "))" })
			r.addAfter(s[1], e[1], func(*rewriter) string { return "))" })
		case C.ORB_ARRAY_KWHASH:
			r.add(s[0], s[0], func(*rewriter) string { return "{" })
			r.addAfter(s[0], e[0], func(*rewriter) string { return "}" })
		case C.ORB_KWSPLAT:
			r.add(s[0], s[0], func(*rewriter) string { return "::Kernel.__oruby_kwsplat((" })
			r.addAfter(s[0], e[0], func(*rewriter) string { return "))" })
		case C.ORB_LITERAL_ENCODING:
			// MRI tags a literal with its source encoding; \u escapes make it
			// UTF-8, which is oruby's default already
			switch {
			case rc.flags&1 != 0:
			case rc.flags&2 != 0:
				r.addAfter(s[0], e[0], func(*rewriter) string { return ".force_encoding(::Encoding::BINARY)" })
			default:
				r.addAfter(s[0], e[0], func(*rewriter) string { return `.force_encoding(::Encoding.find("` + encName + `"))` })
			}
		case C.ORB_SOURCE_ENCODING:
			if encName != "" && encName != "UTF-8" {
				r.add(s[0], e[0], func(*rewriter) string { return `::Encoding.find("` + encName + `")` })
			}
		case C.ORB_BIG_INTEGER:
			r.add(s[0], e[0], func(r *rewriter) string { return `Integer("` + r.text(s[0], e[0]) + `")` })
		case C.ORB_REGEXP_ENC:
			// /a#{x}/n -> ::Regexp.compile("a#{x}", nil, "n"): the literal
			// text verbatim (a regexp source), the embedded code rewritten
			letter := string(rune(rc.flags))
			type part struct {
				s, e int
				text bool
			}
			var parts []part
			for i+1 < len(recs) && recs[i+1].kind == C.ORB_LIST_ITEM {
				i++
				parts = append(parts, part{recs[i].s[0], recs[i].e[0], recs[i].flags == 1})
			}
			r.add(s[0], e[0], func(r *rewriter) string {
				var b strings.Builder
				b.WriteString(`::Regexp.compile("`)
				for _, p := range parts {
					if !p.text {
						b.WriteString(r.render(p.s, p.e))
						continue
					}
					for _, c := range []byte(r.text(p.s, p.e)) {
						if c == '\\' || c == '"' || c == '#' {
							b.WriteByte('\\')
						}
						b.WriteByte(c)
					}
				}
				b.WriteString(`", nil, "` + letter + `")`)
				return b.String()
			})
		}
	}
	if len(r.edits) == 0 {
		return src
	}
	sort.SliceStable(r.edits, func(a, b int) bool {
		ea, eb := r.edits[a], r.edits[b]
		if ea.start != eb.start {
			return ea.start < eb.start
		}
		// at one position, insertions come before the replacement starting there
		if (ea.start == ea.end) != (eb.start == eb.end) {
			return ea.start == ea.end
		}
		if ea.start == ea.end {
			// what ends here before what starts here; the inner end first
			if ea.closer != eb.closer {
				return ea.closer
			}
			if ea.closer && ea.from != eb.from {
				return ea.from > eb.from
			}
			return ea.seq < eb.seq
		}
		return ea.end > eb.end
	})
	return []byte(r.render(0, len(src)))
}

func (r *rewriter) add(s, e int, build func(r *rewriter) string) {
	if s < 0 || e < s {
		return
	}
	r.edits = append(r.edits, edit{s, e, build, len(r.edits), false, 0})
}

// addAfter inserts at pos, the end of the span from..pos, the end of a
// construct wrapping it; of those ending at one position the inner one (the
// later start) is closed first, those of one span in the order added
func (r *rewriter) addAfter(from, pos int, build func(r *rewriter) string) {
	if pos < 0 {
		return
	}
	r.edits = append(r.edits, edit{pos, pos, build, len(r.edits), true, from})
}

// PreprocessRuby is PreprocessRubyBytes for strings.
func PreprocessRuby(source string) string {
	b := PreprocessRubyBytes([]byte(source))
	return string(b)
}

// SplitBeginBlocks moves the program's BEGIN { ... } blocks out of src, for
// the -n/-p loop: begins holds them, in order; in body each is blanked out,
// newlines kept, so line numbers stay as in src.
func SplitBeginBlocks(src []byte) (begins, body []byte) {
	if !bytes.Contains(src, []byte("BEGIN")) {
		return nil, src
	}
	recs, _, ok := prismScan(src, nil)
	if !ok {
		return nil, src
	}
	body = append([]byte(nil), src...)
	var b bytes.Buffer
	for _, rc := range recs {
		if rc.kind != C.ORB_BEGIN_BLOCK || rc.s[0] < 0 || rc.e[2] < 0 {
			continue
		}
		b.Write(src[rc.s[0]:rc.e[2]])
		b.WriteString("; ")
		for i := rc.s[0]; i < rc.e[2]; i++ {
			if body[i] != '\n' {
				body[i] = ' '
			}
		}
	}
	return b.Bytes(), body
}
