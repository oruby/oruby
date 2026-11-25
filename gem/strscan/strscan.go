// Package strscan provides a native pure-Go implementation of Ruby's
// StringScanner (strscan 3.1.8) with O(1) zero-copy scanning over strings.
package strscan

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	rbregexp "github.com/oruby/oruby/gem/regexp"
)

func init() {
	oruby.LazyGem("strscan", initStrscan)
}

type scanner struct {
	strVal      oruby.Value // Ruby String (kept rooted via @string ivar)
	pos         int         // current byte offset
	prev        int         // previous byte offset (for unscan)
	fixedAnchor bool
	matched     bool
	matchBase   int      // byte offset added to loc entries (0 when fixedAnchor, prev when !fixedAnchor)
	loc         []int    // byte offsets [s0, e0, s1, e1, ...] relative to matchBase
	names       []string // capture group names from Regexp (if any)
}

func (sc *scanner) clearMatch() {
	sc.matched = false
	sc.loc = nil
	sc.names = nil
}

func (sc *scanner) rawString(mrb *oruby.MrbState, self oruby.Value) string {
	sv := mrb.GetIV(self, "@string")
	if sv.IsString() {
		return sv.String()
	}
	return ""
}

func (sc *scanner) encName(mrb *oruby.MrbState, self oruby.Value) string {
	sv := mrb.GetIV(self, "@string")
	if sv.IsString() && mrb.RespondTo(sv, mrb.Intern("encoding")) {
		if ev, err := mrb.Funcall(sv, mrb.Intern("encoding")); err == nil {
			if nv, err2 := mrb.Funcall(ev, oruby.Sym_name); err2 == nil && nv.IsString() {
				return nv.String()
			}
		}
	}
	return "UTF-8"
}

func (sc *scanner) sliceRubyChecked(mrb *oruby.MrbState, self oruby.Value, start, length int) oruby.Value {
	sv := mrb.GetIV(self, "@string")
	s := sv.String()
	if start < 0 || start > len(s) {
		return mrb.NilValue()
	}
	if length < 0 {
		length = 0
	}
	if length > len(s)-start {
		length = len(s) - start
	}
	res := mrb.BytesValue([]byte(s[start : start+length]))
	// Preserve encoding of @string
	if mrb.RespondTo(sv, mrb.Intern("encoding")) {
		if enc, err := mrb.Funcall(sv, mrb.Intern("encoding")); err == nil && !enc.IsNil() {
			_, _ = mrb.Funcall(res, mrb.Intern("force_encoding"), enc)
		}
	}
	return res
}

func isAtCharBoundary(s string, pos int, enc string) bool {
	if pos <= 0 || pos >= len(s) {
		return true
	}
	switch enc {
	case "UTF-16BE", "UTF-16LE", "UTF-16":
		return pos%2 == 0
	case "UTF-32BE", "UTF-32LE", "UCS-4BE", "UCS-4LE", "UTF-32":
		return pos%4 == 0
	case "UTF-8":
		return (s[pos] & 0xC0) != 0x80
	}
	return true
}

func (sc *scanner) sliceRuby(mrb *oruby.MrbState, self oruby.Value, start, length int) oruby.Value {
	sv := mrb.GetIV(self, "@string")
	s := sv.String()
	if start < 0 {
		start = 0
	}
	if start > len(s) {
		start = len(s)
	}
	if length < 0 {
		length = 0
	}
	if length > len(s)-start {
		length = len(s) - start
	}
	res := mrb.BytesValue([]byte(s[start : start+length]))
	// Preserve encoding of @string
	if mrb.RespondTo(sv, mrb.Intern("encoding")) {
		if enc, err := mrb.Funcall(sv, mrb.Intern("encoding")); err == nil && !enc.IsNil() {
			_, _ = mrb.Funcall(res, mrb.Intern("force_encoding"), enc)
		}
	}
	return res
}

func toStrArg(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	if v.IsString() {
		return v, nil
	}
	if mrb.RespondTo(v, oruby.Sym_to_str) {
		sv, err := mrb.Funcall(v, oruby.Sym_to_str)
		if err != nil {
			return mrb.NilValue(), err
		}
		if sv.IsString() {
			return sv, nil
		}
	}
	name := mrb.ClassOf(v).Name()
	if v.IsNil() {
		name = "nil"
	}
	return mrb.NilValue(), oruby.ETypeError("no implicit conversion of %s into String", name)
}

func hexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func toIntArg(mrb *oruby.MrbState, v oruby.Value) (int, error) {
	if v.IsInt() {
		return int(v.Int64()), nil
	}
	if v.Type() == oruby.MrbTTBigInt {
		n, err := mrb.AsInt64(v)
		return int(n), err
	}
	if mrb.RespondTo(v, oruby.Sym_to_int) {
		iv, err := mrb.Funcall(v, oruby.Sym_to_int)
		if err != nil {
			return 0, err
		}
		if iv.IsInt() || iv.Type() == oruby.MrbTTBigInt {
			n, err := mrb.AsInt64(iv)
			return int(n), err
		}
		return 0, oruby.ETypeError("can't convert into Integer")
	}
	name := mrb.ClassOf(v).Name()
	if v.IsNil() {
		name = "nil"
	}
	return 0, oruby.ETypeError("no implicit conversion of %s into Integer", name)
}

func initStrscan(mrb *oruby.MrbState) any {
	cls := rb.DataClass(mrb.DefineClass("StringScanner", mrb.ObjectClass()))
	ver := mrb.StrNew("3.1.8")
	mrb.ObjFreeze(ver)
	mrb.ConstSet(cls, mrb.Intern("Version"), ver)
	id := mrb.StrNew("$Id$")
	mrb.ObjFreeze(id)
	mrb.ConstSet(cls, mrb.Intern("Id"), id)
	errCls := mrb.DefineClassUnder(cls, "Error", mrb.ExcGet("StandardError"))

	cls.DefineClassMethod("must_C_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return self
	}, mrb.ArgsNone())

	getSC := func(mrb *oruby.MrbState, self oruby.Value) *scanner {
		sc, _ := rb.Get[*scanner](mrb, self)
		if sc == nil {
			sc = &scanner{}
			rb.Set(mrb, self, sc)
		}
		return sc
	}

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		sv, err := toStrArg(mrb, args.Item(0))
		if err != nil {
			return mrb.RaiseError(err)
		}
		fixed := false
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			if fv, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern("fixed_anchor").Value()); err == nil && !fv.IsNil() {
				fixed = fv.Bool()
			}
		}
		sc := &scanner{strVal: sv, fixedAnchor: fixed}
		rb.Set(mrb, self, sc)
		mrb.SetIV(self, "@string", sv)
		return self
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	cls.DefineMethod("initialize_copy", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		otherVal := mrb.GetArgsFirst()
		other := getSC(mrb, otherVal)
		cp := &scanner{
			strVal:      mrb.GetIV(otherVal, "@string"),
			pos:         other.pos,
			prev:        other.prev,
			fixedAnchor: other.fixedAnchor,
			matched:     other.matched,
			matchBase:   other.matchBase,
			loc:         append([]int(nil), other.loc...),
			names:       append([]string(nil), other.names...),
		}
		rb.Set(mrb, self, cp)
		mrb.SetIV(self, "@string", cp.strVal)
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fixed_anchor?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getSC(mrb, self).fixedAnchor)
	}, mrb.ArgsNone())

	cls.DefineMethod("string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@string")
	}, mrb.ArgsNone())

	cls.DefineMethod("string=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		arg := mrb.GetArgsFirst()
		sv, err := toStrArg(mrb, arg)
		if err != nil {
			return mrb.RaiseError(err)
		}
		sc := getSC(mrb, self)
		sc.strVal = sv
		sc.pos = 0
		sc.clearMatch()
		mrb.SetIV(self, "@string", sv)
		return arg
	}, mrb.ArgsReq(1))

	concatFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sv, err := toStrArg(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		cur := mrb.GetIV(self, "@string")
		if _, err := mrb.Funcall(cur, mrb.Intern("<<"), sv); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}
	cls.DefineMethod("concat", concatFn, mrb.ArgsReq(1))
	cls.DefineMethod("<<", concatFn, mrb.ArgsReq(1))

	posFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getSC(mrb, self).pos)
	}
	cls.DefineMethod("pos", posFn, mrb.ArgsNone())
	cls.DefineMethod("pointer", posFn, mrb.ArgsNone())

	setPosFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		n, err := toIntArg(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		total := len(sc.rawString(mrb, self))
		if n < 0 {
			n += total
		}
		if n < 0 || n > total {
			return mrb.Raise(mrb.ERangeError(), "index out of range")
		}
		sc.pos = n
		return mrb.FixnumValue(n)
	}
	cls.DefineMethod("pos=", setPosFn, mrb.ArgsReq(1))
	cls.DefineMethod("pointer=", setPosFn, mrb.ArgsReq(1))

	cls.DefineMethod("charpos", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		p := sc.pos
		if p > len(s) {
			p = len(s)
		}
		enc := sc.encName(mrb, self)
		if enc == "UTF-8" || enc == "US-ASCII" {
			return mrb.FixnumValue(utf8.RuneCountInString(s[:p]))
		}
		if enc == "ASCII-8BIT" || enc == "BINARY" {
			return mrb.FixnumValue(p)
		}
		prefix := sc.sliceRuby(mrb, self, 0, p)
		sz, err := mrb.Funcall(prefix, mrb.Intern("size"))
		if err != nil {
			return mrb.FixnumValue(p)
		}
		return sz
	}, mrb.ArgsNone())

	cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		sc.pos = 0
		sc.clearMatch()
		return self
	}, mrb.ArgsNone())

	termFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		sc.pos = len(sc.rawString(mrb, self))
		sc.clearMatch()
		return self
	}
	cls.DefineMethod("terminate", termFn, mrb.ArgsNone())
	cls.DefineMethod("clear", termFn, mrb.ArgsNone())

	cls.DefineMethod("unscan", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched {
			return mrb.Raise(errCls, "unscan error: not scanned yet")
		}
		sc.pos = sc.prev
		sc.clearMatch()
		return self
	}, mrb.ArgsNone())

	eosFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		return mrb.BoolValue(sc.pos >= len(sc.rawString(mrb, self)))
	}
	cls.DefineMethod("eos?", eosFn, mrb.ArgsNone())
	cls.DefineMethod("empty?", eosFn, mrb.ArgsNone())

	cls.DefineMethod("rest?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		return mrb.BoolValue(sc.pos < len(sc.rawString(mrb, self)))
	}, mrb.ArgsNone())

	cls.DefineMethod("rest", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		if sc.pos >= len(s) {
			return sc.sliceRuby(mrb, self, len(s), 0)
		}
		return sc.sliceRuby(mrb, self, sc.pos, len(s)-sc.pos)
	}, mrb.ArgsNone())

	restSizeFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		rem := len(sc.rawString(mrb, self)) - sc.pos
		if rem < 0 {
			rem = 0
		}
		return mrb.FixnumValue(rem)
	}
	cls.DefineMethod("rest_size", restSizeFn, mrb.ArgsNone())
	cls.DefineMethod("restsize", restSizeFn, mrb.ArgsNone())

	bolFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		return mrb.BoolValue(sc.pos == 0 || (sc.pos <= len(s) && s[sc.pos-1] == '\n'))
	}
	cls.DefineMethod("beginning_of_line?", bolFn, mrb.ArgsNone())
	cls.DefineMethod("bol?", bolFn, mrb.ArgsNone())

	// Core zero-copy scan engine
	doScan := func(mrb *oruby.MrbState, self oruby.Value, pattern oruby.Value, advance, returnString, anchored bool) oruby.MrbValue {
		sc := getSC(mrb, self)
		sc.clearMatch()
		s := sc.rawString(mrb, self)
		if sc.pos > len(s) {
			sc.pos = len(s)
		}
		rest := s[sc.pos:]

		var offset, matchLen int
		var loc []int
		var names []string
		var base int

		enc := sc.encName(mrb, self)
		if !isAtCharBoundary(s, sc.pos, enc) {
			return mrb.NilValue()
		}

		if pattern.IsString() {
			sv := mrb.GetIV(self, "@string")
			if encCls := mrb.ClassGet("Encoding"); !encCls.IsNil() && mrb.RespondTo(encCls.Value(), mrb.Intern("compatible?")) {
				if compat, err := mrb.Funcall(encCls, mrb.Intern("compatible?"), sv, pattern); err == nil && compat.IsNil() {
					return rb.Raise(mrb, "Encoding::CompatibilityError", "incompatible encoding regexp match (%s regexp with %s string)", enc, enc)
				}
			}
			pat := pattern.String()
			if anchored {
				if !strings.HasPrefix(rest, pat) {
					return mrb.NilValue()
				}
				offset = 0
			} else {
				idx := strings.Index(rest, pat)
				if idx < 0 {
					return mrb.NilValue()
				}
				offset = idx
			}
			matchLen = len(pat)
			base = sc.pos
			loc = []int{offset, offset + matchLen}
		} else if re, ok := mrb.Data(pattern).(*rbregexp.Regexp); ok && re != nil {
			if sc.fixedAnchor {
				found, err := re.FindAtBytes(s, sc.pos, enc)
				if err != nil {
					return mrb.RaiseError(err)
				}
				if len(found) < 2 {
					return mrb.NilValue()
				}
				offset = found[0] - sc.pos
				if offset < 0 || (anchored && offset != 0) {
					return mrb.NilValue()
				}
				matchLen = found[1] - found[0]
				base = 0
				loc = found
				names = re.SubexpNames()
			} else {
				found, err := re.FindAtBytes(rest, 0, enc)
				if err != nil {
					return mrb.RaiseError(err)
				}
				if len(found) < 2 {
					return mrb.NilValue()
				}
				offset = found[0]
				if anchored && offset != 0 {
					return mrb.NilValue()
				}
				matchLen = found[1] - found[0]
				base = sc.pos
				loc = found
				names = re.SubexpNames()
			}
		} else {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected Regexp)", mrb.ClassOf(pattern).Name()))
		}

		sc.prev = sc.pos
		sc.matched = true
		sc.matchBase = base
		sc.loc = loc
		sc.names = names

		consumed := offset + matchLen
		if advance {
			sc.pos += consumed
		}
		if returnString {
			if anchored {
				return sc.sliceRuby(mrb, self, base+loc[0], matchLen)
			}
			return sc.sliceRuby(mrb, self, sc.prev, consumed)
		}
		return mrb.FixnumValue(consumed)
	}

	defineScanOp := func(name string, advance, returnString, anchored bool) {
		cls.DefineMethod(name, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return doScan(mrb, self, mrb.GetArgsFirst(), advance, returnString, anchored)
		}, mrb.ArgsReq(1))
	}
	defineScanOp("scan", true, true, true)
	defineScanOp("match?", false, false, true)
	defineScanOp("skip", true, false, true)
	defineScanOp("check", false, true, true)
	defineScanOp("scan_until", true, true, false)
	defineScanOp("exist?", false, false, false)
	defineScanOp("skip_until", true, false, false)
	defineScanOp("check_until", false, true, false)

	cls.DefineMethod("scan_full", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		return doScan(mrb, self, a.Item(0), a.Item(1).Bool(), a.Item(2).Bool(), true)
	}, mrb.ArgsReq(3))

	cls.DefineMethod("search_full", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		return doScan(mrb, self, a.Item(0), a.Item(1).Bool(), a.Item(2).Bool(), false)
	}, mrb.ArgsReq(3))

	// Single char / byte helpers
	cls.DefineMethod("getch", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		if sc.pos >= len(s) {
			sc.clearMatch()
			return mrb.NilValue()
		}
		enc := sc.encName(mrb, self)
		clen := 1
		if enc == "UTF-8" {
			_, clen = utf8.DecodeRuneInString(s[sc.pos:])
			if clen <= 0 {
				clen = 1
			}
		} else if enc != "ASCII-8BIT" && enc != "BINARY" && enc != "US-ASCII" {
			// Delegate multi-byte legacy encodings to Encoding::Converter.__char_len if available
			convCls := rb.ClassPath(mrb, "Encoding::Converter")
			if !convCls.IsNil() && mrb.RespondTo(convCls.Value(), mrb.Intern("__char_len")) {
				if lv, err := mrb.Funcall(convCls, mrb.Intern("__char_len"), mrb.GetIV(self, "@string"), mrb.FixnumValue(sc.pos), mrb.StrNew(enc)); err == nil && lv.IsInt() && lv.Int() > 0 {
					clen = lv.Int()
				}
			}
		}
		if sc.pos+clen > len(s) {
			clen = len(s) - sc.pos
		}
		sc.prev = sc.pos
		sc.matched = true
		sc.matchBase = sc.pos
		sc.loc = []int{0, clen}
		sc.names = nil
		sc.pos += clen
		return sc.sliceRuby(mrb, self, sc.prev, clen)
	}, mrb.ArgsNone())

	getByteFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		if sc.pos >= len(s) {
			sc.clearMatch()
			return mrb.NilValue()
		}
		sc.prev = sc.pos
		sc.matched = true
		sc.matchBase = sc.pos
		sc.loc = []int{0, 1}
		sc.names = nil
		sc.pos++
		return sc.sliceRuby(mrb, self, sc.prev, 1)
	}
	cls.DefineMethod("get_byte", getByteFn, mrb.ArgsNone())
	cls.DefineMethod("getbyte", getByteFn, mrb.ArgsNone())

	cls.DefineMethod("scan_byte", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		if sc.pos >= len(s) {
			sc.clearMatch()
			return mrb.NilValue()
		}
		b := s[sc.pos]
		sc.prev = sc.pos
		sc.matched = true
		sc.matchBase = sc.pos
		sc.loc = []int{0, 1}
		sc.names = nil
		sc.pos++
		return mrb.FixnumValue(int(b))
	}, mrb.ArgsNone())

	cls.DefineMethod("peek_byte", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		if sc.pos >= len(s) {
			return mrb.NilValue()
		}
		return mrb.FixnumValue(int(s[sc.pos]))
	}, mrb.ArgsNone())

	peekFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		n, err := toIntArg(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if n < 0 {
			return mrb.Raise(mrb.EArgumentError(), "negative string size (or size too big)")
		}
		s := sc.rawString(mrb, self)
		if sc.pos >= len(s) {
			return sc.sliceRuby(mrb, self, len(s), 0)
		}
		return sc.sliceRuby(mrb, self, sc.pos, n)
	}
	cls.DefineMethod("peek", peekFn, mrb.ArgsReq(1))
	cls.DefineMethod("peep", peekFn, mrb.ArgsReq(1))

	cls.DefineMethod("scan_integer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		base := 10
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			if bv, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern("base").Value()); err == nil && !bv.IsNil() {
				if !bv.IsInt() || (bv.Int64() != 10 && bv.Int64() != 16) {
					return mrb.Raise(mrb.EArgumentError(), fmt.Sprintf("Unsupported integer base: %s, expected 10 or 16", mrb.String(bv)))
				}
				base = int(bv.Int64())
			}
		}
		sc := getSC(mrb, self)
		sv := mrb.GetIV(self, "@string")
		if mrb.RespondTo(sv, mrb.Intern("encoding")) {
			if enc, err := mrb.Funcall(sv, mrb.Intern("encoding")); err == nil {
				if ac, err2 := mrb.Funcall(enc, mrb.Intern("ascii_compatible?")); err2 == nil && !ac.Bool() {
					return rb.Raise(mrb, "Encoding::CompatibilityError", "ASCII incompatible encoding: %s", sc.encName(mrb, self))
				}
			}
		}
		s := sc.rawString(mrb, self)
		sc.clearMatch()
		if sc.pos >= len(s) {
			return mrb.NilValue()
		}
		i := sc.pos
		if s[i] == '+' || s[i] == '-' {
			i++
		}
		if base == 16 && i+2 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') && hexDigit(s[i+2]) {
			i += 2
		}
		digStart := i
		for i < len(s) {
			c := s[i]
			if base == 10 {
				if c >= '0' && c <= '9' {
					i++
					continue
				}
			} else if hexDigit(c) {
				i++
				continue
			}
			break
		}
		if i == digStart {
			return mrb.NilValue()
		}
		tok := s[sc.pos:i]
		sc.prev = sc.pos
		sc.matched = true
		sc.matchBase = sc.pos
		sc.loc = []int{0, i - sc.pos}
		sc.names = nil
		sc.pos = i
		// Strip optional 0x prefix for strconv / Ruby to_i
		cleanTok := tok
		if base == 16 {
			cleanTok = strings.Replace(strings.Replace(cleanTok, "0x", "", 1), "0X", "", 1)
		}
		if iv, err := strconv.ParseInt(cleanTok, base, 64); err == nil {
			return mrb.Int64Value(iv)
		}
		res, err := mrb.Funcall(mrb.StrNew(tok), oruby.Sym_to_i, mrb.FixnumValue(base))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return res
	}, mrb.ArgsKey(0, 1))

	// Match data accessors
	cls.DefineMethod("matched?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getSC(mrb, self).matched)
	}, mrb.ArgsNone())

	cls.DefineMethod("matched", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched || len(sc.loc) < 2 {
			return mrb.NilValue()
		}
		return sc.sliceRubyChecked(mrb, self, sc.matchBase+sc.loc[0], sc.loc[1]-sc.loc[0])
	}, mrb.ArgsNone())

	cls.DefineMethod("matched_size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched || len(sc.loc) < 2 {
			return mrb.NilValue()
		}
		s := sc.rawString(mrb, self)
		start := sc.matchBase + sc.loc[0]
		end := sc.matchBase + sc.loc[1]
		if start < 0 || start > len(s) {
			return mrb.NilValue()
		}
		if end > len(s) {
			end = len(s)
		}
		if end < start {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(end - start)
	}, mrb.ArgsNone())

	cls.DefineMethod("pre_match", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched || len(sc.loc) < 2 {
			return mrb.NilValue()
		}
		return sc.sliceRuby(mrb, self, 0, sc.matchBase+sc.loc[0])
	}, mrb.ArgsNone())

	cls.DefineMethod("post_match", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched || len(sc.loc) < 2 {
			return mrb.NilValue()
		}
		stop := sc.matchBase + sc.loc[1]
		total := len(sc.rawString(mrb, self))
		return sc.sliceRuby(mrb, self, stop, total-stop)
	}, mrb.ArgsNone())

	groupAt := func(mrb *oruby.MrbState, self oruby.Value, sc *scanner, idx oruby.Value) (oruby.Value, error) {
		if !sc.matched {
			return mrb.NilValue(), nil
		}
		ng := len(sc.loc) / 2
		if idx.IsSymbol() || idx.IsString() {
			want := ""
			if idx.IsSymbol() {
				want = mrb.SymName(idx.Symbol())
			} else {
				want = idx.String()
			}
			// Find last matching group name (Ruby semantics for duplicate names)
			foundIdx := -1
			for i := 1; i < len(sc.names) && i < ng; i++ {
				if sc.names[i] == want {
					foundIdx = i
					if sc.loc[2*i] >= 0 {
						// Prefer last participating group
					}
				}
			}
			if foundIdx < 0 {
				return mrb.NilValue(), oruby.EIndexError("undefined group name reference: %s", want)
			}
			// Pick last participating match with this name, or nil
			for i := len(sc.names) - 1; i >= 1; i-- {
				if i < ng && sc.names[i] == want && sc.loc[2*i] >= 0 {
					return sc.sliceRubyChecked(mrb, self, sc.matchBase+sc.loc[2*i], sc.loc[2*i+1]-sc.loc[2*i]), nil
				}
			}
			return mrb.NilValue(), nil
		}
		i, err := toIntArg(mrb, idx)
		if err != nil {
			return mrb.NilValue(), err
		}
		if i < 0 {
			i += ng
		}
		if i < 0 || i >= ng {
			return mrb.NilValue(), nil
		}
		s, e := sc.loc[2*i], sc.loc[2*i+1]
		if s < 0 || e < 0 {
			return mrb.NilValue(), nil
		}
		return sc.sliceRubyChecked(mrb, self, sc.matchBase+s, e-s), nil
	}

	cls.DefineMethod("[]", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		v, err := groupAt(mrb, self, sc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return v
	}, mrb.ArgsReq(1))

	cls.DefineMethod("integer_at", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		sc := getSC(mrb, self)
		v, err := groupAt(mrb, self, sc, args.Item(0))
		if err != nil {
			return mrb.RaiseError(err)
		}
		if v.IsNil() || !v.IsString() || len(v.String()) == 0 {
			return mrb.NilValue()
		}
		base := 10
		if args.Len() > 1 {
			b, bErr := toIntArg(mrb, args.Item(1))
			if bErr != nil {
				return mrb.RaiseError(bErr)
			}
			base = b
		}
		res, rErr := mrb.Funcall(v, oruby.Sym_to_i, mrb.FixnumValue(base))
		if rErr != nil {
			return mrb.RaiseError(rErr)
		}
		return res
	}, mrb.ArgsArg(1, 1))

	cls.DefineMethod("size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched {
			return mrb.NilValue()
		}
		return mrb.FixnumValue(len(sc.loc) / 2)
	}, mrb.ArgsNone())

	cls.DefineMethod("captures", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched {
			return mrb.NilValue()
		}
		ng := len(sc.loc) / 2
		ary := mrb.AryNewCapa(ng - 1)
		for i := 1; i < ng; i++ {
			s, e := sc.loc[2*i], sc.loc[2*i+1]
			if s < 0 || e < 0 {
				mrb.AryPush(ary, mrb.NilValue())
			} else {
				mrb.AryPush(ary, sc.sliceRuby(mrb, self, sc.matchBase+s, e-s))
			}
		}
		return ary
	}, mrb.ArgsNone())

	cls.DefineMethod("values_at", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		if !sc.matched {
			return mrb.NilValue()
		}
		args := mrb.GetArgs().Slice()
		ary := mrb.AryNewCapa(len(args))
		for _, a := range args {
			v, err := groupAt(mrb, self, sc, a)
			if err != nil {
				return mrb.RaiseError(err)
			}
			mrb.AryPush(ary, v)
		}
		return ary
	}, mrb.ArgsAny())

	cls.DefineMethod("named_captures", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		h := mrb.HashNew()
		if !sc.matched {
			return h
		}
		ng := len(sc.loc) / 2
		for i := 1; i < len(sc.names) && i < ng; i++ {
			if sc.names[i] == "" {
				continue
			}
			key := mrb.StrNew(sc.names[i])
			s, e := sc.loc[2*i], sc.loc[2*i+1]
			if s >= 0 && e >= 0 {
				mrb.HashSet(h, key, sc.sliceRubyChecked(mrb, self, sc.matchBase+s, e-s))
			} else if !mrb.HashKeyP(h, key) {
				mrb.HashSet(h, key, mrb.NilValue())
			}
		}
		return h
	}, mrb.ArgsNone())

	cls.DefineMethod("inspect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sc := getSC(mrb, self)
		s := sc.rawString(mrb, self)
		if sc.pos >= len(s) {
			return mrb.StrNew("#<StringScanner fin>")
		}
		after := s[sc.pos:]
		if len(after) > 5 {
			after = after[:5] + "..."
		}
		afterQ, _ := mrb.Funcall(mrb.StrNew(after), mrb.Intern("inspect"))
		if sc.pos == 0 {
			return mrb.StrNew(fmt.Sprintf("#<StringScanner 0/%d @ %s>", len(s), afterQ.String()))
		}
		before := s[:sc.pos]
		if len(before) > 5 {
			before = "..." + before[len(before)-5:]
		}
		beforeQ, _ := mrb.Funcall(mrb.StrNew(before), mrb.Intern("inspect"))
		return mrb.StrNew(fmt.Sprintf("#<StringScanner %d/%d %s @ %s>", sc.pos, len(s), beforeQ.String(), afterQ.String()))
	}, mrb.ArgsNone())

	return nil
}
