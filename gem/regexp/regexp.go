package regexp

import (
	"math"
	"strings"
	"time"

	"github.com/oruby/oruby"
)

// oruby regex options
const (
	IgnoreCase    = 1
	Extended      = 2
	Multiline     = 4
	FixedEncoding = 16
	NoEncoding    = 32
)

func init() {
	oruby.RegisterRegexpRangeValidator(validateLiteralRange)
	oruby.Gem("regexp", func(mrb *oruby.MrbState) interface{} {
		// StringScanner is provided by stdlib gem (require "strscan")

		data := mrb.DefineGoClass("MatchData", &MatchData{})
		data.DefineMethod("[]", matchDataIndex, mrb.ArgsArg(1, 1))
		data.DefineMethod("to_a", matchDataToA, mrb.ArgsNone())
		data.DefineMethod("captures", matchDataCaptures, mrb.ArgsNone())
		data.DefineAlias("deconstruct", "captures")
		data.DefineMethod("pre_match", matchDataPreMatch, mrb.ArgsNone())
		data.DefineMethod("post_match", matchDataPostMatch, mrb.ArgsNone())
		data.DefineMethod("string", matchDataString, mrb.ArgsNone())
		data.DefineMethod("initialize_copy", matchDataInitCopy, mrb.ArgsReq(1))
		data.DefineMethod("values_at", matchGoValuesAt, mrb.ArgsAny())
		data.DefineMethod("regexp", matchDataRegexp, mrb.ArgsNone())
		data.DefineMethod("begin", matchDataPos(func(m *MatchData, i int) (interface{}, error) { return m.charBegin(i) }), mrb.ArgsReq(1))
		data.DefineMethod("end", matchDataPos(func(m *MatchData, i int) (interface{}, error) { return m.charEnd(i) }), mrb.ArgsReq(1))
		data.DefineMethod("offset", matchDataPos(func(m *MatchData, i int) (interface{}, error) { return m.charOffset(i) }), mrb.ArgsReq(1))
		data.DefineMethod("byteoffset", matchDataPos(func(m *MatchData, i int) (interface{}, error) { return m.byteOffset(i) }), mrb.ArgsReq(1))
		data.DefineMethod("bytebegin", matchDataPos(func(m *MatchData, i int) (interface{}, error) {
			o, err := m.byteOffset(i)
			if err != nil {
				return nil, err
			}
			return o[0], nil
		}), mrb.ArgsReq(1))
		data.DefineMethod("byteend", matchDataPos(func(m *MatchData, i int) (interface{}, error) {
			o, err := m.byteOffset(i)
			if err != nil {
				return nil, err
			}
			return o[1], nil
		}), mrb.ArgsReq(1))
		// mruby compiles $& and $1..$9 to $~.__group(n)
		data.DefineMethod("__group", matchDataIndex, mrb.ArgsReq(1))
		data.DefineAlias("length", "size")
		// $` and $' compile to $~.__pre_match / $~.__post_match
		data.DefineAlias("__pre_match", "pre_match")
		data.DefineAlias("__post_match", "post_match")

		cls := mrb.DefineGoClass("Regexp", newRegexpGo)
		cls.Const("IGNORECASE", IgnoreCase)
		cls.Const("EXTENDED", Extended)
		cls.Const("MULTILINE", Multiline)
		cls.Const("FIXEDENCODING", FixedEncoding)
		cls.Const("NOENCODING", NoEncoding)

		cls.DefineMethod("source", regexSource, mrb.ArgsNone())
		cls.DefineMethod("~", regexTilde, mrb.ArgsNone())
		cls.UndefMethod("initialize")
		cls.DefineMethod("initialize", regexInit, mrb.ArgsArg(1, 3)+mrb.ArgsKey(0, 1)+mrb.ArgsBlock())
		cls.DefineMethod("encoding", regexEncoding, mrb.ArgsNone())
		cls.DefineMethod("fixed_encoding?", regexFixedEncoding, mrb.ArgsNone())
		cls.DefineMethod("after_init", regexAfterInit, mrb.ArgsNone())
		cls.DefineMethod("initialize_copy", regexInitCopy, mrb.ArgsReq(1))
		cls.DefineMethod("match", regexMatch, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
		cls.DefineMethod("==", regexEqual, mrb.ArgsReq(1))
		cls.DefineAlias("eql?", "==")
		// === sets $~ (used by case/when); match? does not
		cls.DefineMethod("===", regexCaseEq, mrb.ArgsReq(1))
		cls.DefineMethod("match?", regexEqualMatch, mrb.ArgsArg(1, 1))
		cls.DefineMethod("=~", regexMatchOperator, mrb.ArgsReq(1))
		cls.DefineMethod("casefold?", regexCasefold, mrb.ArgsNone())
		cls.DefineMethod("options", regexOptions, mrb.ArgsNone())
		cls.DefineMethod("to_s", regexToS, mrb.ArgsNone())
		cls.DefineMethod("inspect", regexInspect, mrb.ArgsNone())
		cls.DefineMethod("hash", regexHash, mrb.ArgsNone())
		cls.DefineMethod("named_captures", regexNamedCaptures, mrb.ArgsNone())
		cls.DefineMethod("names", regexNames, mrb.ArgsNone())

		cls.DefineClassMethod("quote", regexQuote, mrb.ArgsReq(1))
		cls.DefineClassMethod("escape", regexQuote, mrb.ArgsReq(1))
		cls.DefineClassMethod("compile", regexCompile, mrb.ArgsAny()+mrb.ArgsKey(0, 1)+mrb.ArgsBlock())
		cls.DefineClassMethod("last_match", regexLastMatch, mrb.ArgsOpt(1))
		// re.__match_raw(str): a MatchData or nil, leaving $~ alone (for
		// core Ruby, whose frames are its callers', go-mrb.c)
		cls.DefineMethod("__match_raw", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			arg := mrb.GetArgsFirst()
			re := regexpOf(mrb, self)
			if re == nil || arg.Type() != oruby.MrbTTString {
				return mrb.NilValue()
			}
			s := rstr(arg)
			result, err := re.findErr(s, 0, regexpSubjectEncoding(mrb, arg))
			if err != nil {
				return raiseTimeout(mrb, err)
			}
			if len(result) == 0 {
				return mrb.NilValue()
			}
			md := newMatchValue(mrb, re, s, result)
			_ = mrb.IVSet(md, oruby.IvSym_regexp, self)
			if m, ok := mrb.Data(md).(*MatchData); ok {
				m.enc = encName(mrb, strEnc(mrb, arg))
			}
			return md
		}, mrb.ArgsReq(1))
		cls.DefineClassMethod("try_convert", regexTryConvert, mrb.ArgsReq(1))
		cls.DefineClassMethod("union", regexUnion, mrb.ArgsAny())
		cls.DefineClassMethod("timeout", regexTimeout, mrb.ArgsNone())
		cls.DefineClassMethod("timeout=", regexSetTimeout, mrb.ArgsReq(1))
		cls.DefineMethod("timeout", regexInstanceTimeout, mrb.ArgsNone())
		cls.DefineClassMethod("linear_time?", regexLinearTime, mrb.ArgsArg(1, 1))
		mrb.DefineClassUnder(cls, "TimeoutError", mrb.ERegexpError())

		mrb.DefineGlobalConst("Regexp", cls)

		initString(mrb)
		return nil
	})
}

// newRegexpGo registers *Regexp as the Go type behind Ruby Regexp objects.
// Ruby-side construction goes through regexInit.
func newRegexpGo(source string) (*Regexp, error) {
	return compileRuby(source, 0)
}

func raiseRegexpError(mrb *oruby.MrbState, err error) oruby.MrbValue {
	msg := err.Error()
	if re, ok := err.(*RegexpError); ok {
		msg = re.msg
	}
	return mrb.Raise(mrb.ERegexpError(), msg)
}

// rubyOptions converts a Ruby options argument: Integer, String ("mix"),
// true (IGNORECASE) or nil/false (none).
func rubyOptions(mrb *oruby.MrbState, v oruby.Value) int {
	switch v.Type() {
	case oruby.MrbTTFixnum:
		return oruby.MrbFixnum(v) & (IgnoreCase | Extended | Multiline | 16 | 32)
	case oruby.MrbTTString:
		opts := 0
		for _, c := range mrb.String(v) {
			switch c {
			case 'i':
				opts |= IgnoreCase
			case 'x':
				opts |= Extended
			case 'm':
				opts |= Multiline
			}
		}
		return opts
	case oruby.MrbTTFalse:
		return 0
	}
	if v.IsNil() {
		return 0
	}
	return IgnoreCase
}

func regexCompile(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	if kw := mrb.KeywordArgsValue(); !kw.IsNil() {
		list := mrb.AryNewFromValues(args.Slice()...)
		ret, err := mrb.Funcall(self, oruby.Sym___oruby_compile_keywords, list, kw, block)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return ret
	}
	var short [4]interface{}
	call := short[:0]
	for _, arg := range args.Slice() {
		call = append(call, arg)
	}
	if !block.IsNil() {
		call = append(call, block.Value())
	}
	ret, err := mrb.FuncallWithBlock(self, oruby.Sym_new, call...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return ret
}

// regexSource is Regexp#source, in the regexp's encoding
func regexSource(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if regexpOf(mrb, self) == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized Regexp")
	}
	return mrb.IVGet(self, oruby.IvSym_source)
}

// regexTilde is Regexp#~: self =~ $_
func regexTilde(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	line := mrb.GVGet(mrb.Intern("$_"))
	if line.Type() != oruby.MrbTTString {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	ret, err := mrb.Funcall(self, mrb.Intern("=~"), line)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return ret
}

func regexLinearTime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	source := args.Item(0)

	var r *Regexp
	if other, ok := mrb.Data(source).(*Regexp); ok {
		r = other
		if args.Len() > 1 && !args.Item(1).IsNil() {
			mrb.Call(mrb.ModuleGet("Kernel").Value(), "warn", mrb.StrNew("warning: flags ignored"))
		}
	} else {
		if source.Type() != oruby.MrbTTString {
			if !mrb.RespondTo(source, oruby.Sym_to_str) {
				return mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
			}
			source = mrb.Call(source, "to_str")
		}
		src := mrb.String(source)
		opts := rubyOptions(mrb, args.ItemDef(1, mrb.NilValue()))
		compiled, err := compileRuby(src, opts)
		if err != nil {
			return raiseRegexpError(mrb, err)
		}
		r = compiled
	}

	return oruby.Bool(r != nil && !hasBackref(r.source))
}

// hasBackref: whether a pattern uses a back-reference (\1..\9, \k<name>),
// the one construct Onigmo's memoization cannot match in linear time
// (lookarounds, atomic groups and possessive quantifiers it can)
func hasBackref(src string) bool {
	inClass := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\\' && i+1 < len(src):
			n := src[i+1]
			if inClass == 0 && ((n >= '1' && n <= '9') || n == 'k') {
				return true
			}
			i++
		case c == '[':
			inClass++
		case c == ']' && inClass > 0:
			inClass--
		}
	}
	return false
}

// regexAfterInit is called when a Go-created *Regexp is passed to oruby
func regexAfterInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r, ok := mrb.Data(self).(*Regexp)
	if !ok {
		return self
	}
	_ = mrb.IVSet(self, oruby.IvSym_source, mrb.StrNew(r.source))
	_ = mrb.IVSet(self, oruby.IvSym_options, mrb.FixnumValue(r.options))
	return self
}

// regexInit is Regexp#initialize, as MRI's rb_reg_initialize_m: a Regexp
// copies source, options and encoding ("flags ignored"); a String (by
// #to_str) is compiled with the options (Integer, "imx" or truthy) and the
// encoding letter of a literal (Regexp.compile's third argument). A Regexp
// is frozen (an instance of a subclass is not).
func regexInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if regexpOf(mrb, self) != nil {
		if err := mrb.CheckFrozen(self); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.Raise(mrb.ETypeError(), "already initialized regexp")
	}
	args := mrb.GetArgs()
	source := args.Item(0)
	var src, enc string
	var opts int
	var timeout time.Duration
	if kw := mrb.KeywordArgsValue(); !kw.IsNil() {
		key := mrb.SymbolValue(mrb.Intern("timeout"))
		for _, k := range oruby.RArraySlice(mrb.HashKeys(kw).Value()) {
			if !k.IsSymbol() || k.Symbol() != key.Symbol() {
				return mrb.Raise(mrb.EArgumentError(), "unknown keyword: "+mrb.String(mrb.Inspect(k)))
			}
		}
		var exc oruby.MrbValue
		timeout, exc = regexpTimeoutArg(mrb, mrb.HashGet(kw, key))
		if exc != nil {
			return exc
		}
	}

	if other, ok := mrb.Data(source).(*Regexp); ok && source.Type() == oruby.MrbTTCData {
		if args.Len() > 1 && !args.Item(1).IsNil() {
			warn(mrb, "flags ignored")
		}
		src, opts, enc = ivString(mrb, source, "@source"), ivOptions(mrb, source), regexpEncName(mrb, source)
		if other == nil {
			return mrb.Raise(mrb.ETypeError(), "uninitialized Regexp")
		}
	} else {
		sv, exc := implicitStr(mrb, source)
		if exc != nil {
			return exc
		}
		src = mrb.String(sv)
		if args.Len() > 1 {
			o, exc := regexpOptArg(mrb, args.Item(1))
			if exc != nil {
				return exc
			}
			opts = o
		}
		var lit byte
		if l := args.ItemDef(2, mrb.NilValue()).Value(); l.Type() == oruby.MrbTTString && len(rstr(l)) > 0 {
			lit = rstr(l)[0] | 0x20
		}
		if _, msg := unicodeEscapes(src); msg != "" {
			return mrb.Raise(mrb.ERegexpError(), msg+": /"+src+"/")
		}
		enc, opts = regexpEncoding(mrb, src, strEnc(mrb, sv), opts, lit)
	}

	pattern := regexpPatternText(src, enc)
	r, err := compileRuby(pattern, opts&^NoEncoding)
	if err != nil {
		return raiseRegexpError(mrb, err)
	}
	if r.nestedRepeat {
		warn(mrb, "nested repeat operator '+' and '{...}': /"+src+"/")
	}
	r.enc = enc
	r.source, r.options = src, opts
	if pattern != src {
		r.pattern = pattern
	}
	r.timeout = timeout
	mrb.DataSetInterface(self, r)
	_ = mrb.IVSet(self, oruby.IvSym_source, sourceValue(mrb, src, enc))
	_ = mrb.IVSet(self, oruby.IvSym_options, mrb.FixnumValue(opts))
	if mrb.ObjClass(self).Name() == "Regexp" {
		self.Freeze()
	}
	return self
}

func regexInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src := mrb.GetArgsFirst()
	if r, ok := mrb.Data(src).(*Regexp); ok {
		// compiled patterns are immutable and safe to share
		mrb.DataSetInterface(self, r)
		_ = mrb.IVSet(self, oruby.IvSym_source, sourceValue(mrb, r.source, regexpEncName(mrb, src)))
		_ = mrb.IVSet(self, oruby.IvSym_options, mrb.FixnumValue(r.options))
	}
	return self
}

func regexpOf(mrb *oruby.MrbState, self oruby.Value) *Regexp {
	r, _ := mrb.Data(self).(*Regexp)
	return r
}

func matchDataInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src := mrb.GetArgsFirst()
	if m, ok := mrb.Data(src).(*MatchData); ok {
		cp := *m
		mrb.DataSetInterface(self, &cp)
		if v := mrb.IVGet(src, oruby.IvSym_regexp); !v.IsNil() {
			_ = mrb.IVSet(self, oruby.IvSym_regexp, v)
		}
	}
	return self
}

func matchDataGetIndex(mrb *oruby.MrbState, m *MatchData, idx oruby.Value) oruby.Value {
	s := ""
	switch idx.Type() {
	case oruby.MrbTTSymbol:
		s = mrb.SymString(idx.Symbol())
	case oruby.MrbTTString:
		s = rstr(idx)
	case oruby.MrbTTRange:
		return mrb.Call(mdToA(mrb, m, 0), "[]", idx)
	case oruby.MrbTTFixnum:
		return mdGroup(mrb, m, oruby.MrbFixnum(idx))
	default:
		if mrb.RespondTo(idx, oruby.Sym_to_int) {
			toI, err := mrb.Funcall(idx, oruby.Sym_to_int)
			if err != nil {
				return mrb.RaiseError(err).Value()
			}
			if toI.Type() != oruby.MrbTTFixnum {
				cname := mrb.ClassName(mrb.ObjClass(idx))
				return mrb.ETypeError().Raisef("can't convert %s into Integer (%s#to_int gives %s)", cname, cname, mrb.ClassName(mrb.ObjClass(toI.Value()))).Value()
			}
			return mdGroup(mrb, m, oruby.MrbFixnum(toI.Value()))
		}
		return mrb.NilValue()
	}

	// named group; with duplicate names the last participating group wins
	found := false
	result := mrb.NilValue()
	for i, name := range m.names {
		if i == 0 || name != s {
			continue
		}
		found = true
		if str, isNil := m.toString(i); !isNil {
			result = mdStr(mrb, m, str)
		}
	}
	if !found {
		return mrb.Raise(mrb.EIndexError(), "undefined group name reference: "+s)
	}
	return result
}

// MatchData#[](i) / (name) / (range) / (start, len)
func matchDataIndex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.Data(self).(*MatchData)
	args := mrb.GetArgs()
	if args.Len() == 2 {
		start, err := mrb.Funcall(mrb.KernelModule().Value(), mrb.Intern("__to_int"), args.Item(0))
		if err != nil {
			return mrb.RaiseError(err)
		}
		n, err := mrb.Funcall(mrb.KernelModule().Value(), mrb.Intern("__to_int"), args.Item(1))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.Call(mdToA(mrb, data, 0), "[]", start, n)
	}
	return matchDataGetIndex(mrb, data, args.Item(0))
}

func matchDataRegexp(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if v := mrb.IVGet(self, oruby.IvSym_regexp); !v.IsNil() {
		return v
	}
	data := mrb.Data(self).(*MatchData)
	return mrb.Value(data.r)
}

func matchGoValuesAt(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.Data(self).(*MatchData)
	args := mrb.GetArgs()
	values := mrb.AryNewCapa(args.Len())
	for i := 0; i < args.Len(); i++ {
		value := matchDataGetIndex(mrb, data, args.Item(i))
		values.Push(value)
	}

	return values
}

func optionFlags(options int) (on, off string) {
	for _, f := range []struct {
		bit int
		c   string
	}{{Multiline, "m"}, {IgnoreCase, "i"}, {Extended, "x"}} {
		if options&f.bit != 0 {
			on += f.c
		} else {
			off += f.c
		}
	}
	return on, off
}

func ivString(mrb *oruby.MrbState, self oruby.Value, name string) string {
	if v := mrb.IVGet(self, mrb.Intern(name)); !v.IsNil() {
		return mrb.String(v)
	}
	if r := regexpOf(mrb, self); r != nil && name == "@source" {
		return r.source
	}
	return ""
}

func ivOptions(mrb *oruby.MrbState, self oruby.Value) int {
	if v := mrb.IVGet(self, oruby.IvSym_options); v.Type() == oruby.MrbTTFixnum {
		return oruby.MrbFixnum(v)
	}
	if r := regexpOf(mrb, self); r != nil {
		return r.options
	}
	return 0
}

// regexToS returns (?flags-flags:source), as Regexp#to_s
func regexToS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src, opts := regexToSOptions(ivString(mrb, self, "@source"), ivOptions(mrb, self))
	on, off := optionFlags(opts)
	s := "(?" + on
	if off != "" {
		s += "-" + off
	}
	return mrb.StringValue(s + ":" + src + ")")
}

// escapeSlashes escapes unescaped "/" for Regexp#inspect
func escapeSlashes(src string) string {
	if !strings.Contains(src, "/") {
		return src
	}
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			b.WriteByte(c)
			b.WriteByte(src[i+1])
			i++
			continue
		}
		if c == '/' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

func regexInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	opts := ivOptions(mrb, self)
	on, _ := optionFlags(opts)
	if opts&NoEncoding != 0 {
		on += "n"
	}
	return mrb.StringValue("/" + escapeSlashes(ivString(mrb, self, "@source")) + "/" + on)
}

func regexOptions(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if regexpOf(mrb, self) == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized Regexp")
	}
	return mrb.FixnumValue(ivOptions(mrb, self))
}

func regexHash(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	h := 0
	for _, c := range ivString(mrb, self, "@source") {
		h = h*31 + int(c)
	}
	// the matching options only, as MRI's rb_reg_hash (//n hashes as //)
	return mrb.FixnumValue(h ^ ivOptions(mrb, self)&(IgnoreCase|Extended|Multiline))
}

func regexMatchOperator(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	ret := regexMatch(mrb, self)
	if ret.Value().IsNil() || ret.Value().Type() != oruby.MrbTTCData {
		return ret
	}
	data, ok := mrb.Data(ret.Value()).(*MatchData)
	if !ok {
		return ret
	}
	idx := data.byteBegin(0)
	return mrb.FixnumValue(regexpCharIndex(data.s, idx, data.enc))
}

// charIndex converts a byte offset to a character index
func charIndex(s string, byteOff int) int {
	if byteOff <= 0 {
		return byteOff
	}
	n := 0
	for i := range s {
		if i >= byteOff {
			break
		}
		n++
	}
	return n
}

// byteIndex converts a character index (negative from the end) to a byte offset
func byteIndex(s string, pos int) (int, bool) {
	if pos < 0 {
		n := 0
		for range s {
			n++
		}
		pos += n
		if pos < 0 {
			return 0, false
		}
	}
	if pos == 0 {
		return 0, true
	}
	i := 0
	for b := range s {
		if i == pos {
			return b, true
		}
		i++
	}
	if i == pos {
		return len(s), true
	}
	return 0, false
}

// setLastMatch sets $~, which is frame-local (go-mrb.c's svar_set): the
// caller's, a Go method's frame being passed through
func setLastMatch(mrb *oruby.MrbState, matchData oruby.Value) {
	mrb.GVSet(mrb.Intern("$~"), matchData)
}

func newMatchValue(mrb *oruby.MrbState, re *Regexp, s string, result []int) oruby.Value {
	m := NewMatchData(re, s, result, 0)
	return mrb.DataWrapInterface(mrb.ClassGetID(oruby.Sym_MatchData), m).Value()
}

// doMatch matches at byte offset pos and sets $~; returns nil value on no match
func doMatch(mrb *oruby.MrbState, re *Regexp, s string, pos int, encoding ...string) oruby.MrbValue {
	result, err := re.findErr(s, pos, encoding...)
	if err != nil {
		return raiseTimeout(mrb, err)
	}
	if len(result) == 0 {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	matchData := newMatchValue(mrb, re, s, result)
	if len(encoding) > 0 {
		mrb.Data(matchData).(*MatchData).enc = encoding[0]
	}
	setLastMatch(mrb, matchData)
	return matchData
}

func raiseTimeout(mrb *oruby.MrbState, err error) oruby.MrbValue {
	cls := mrb.ClassGet("Regexp")
	if cls.ConstDefinedAt("TimeoutError") {
		return mrb.Raise(mrb.ClassPtr(cls.ConstGet("TimeoutError")), err.Error())
	}
	return mrb.Raise(mrb.ERuntimeError(), err.Error())
}

// subject returns the string to match: String, Symbol or nil (no match)
func subject(mrb *oruby.MrbState, v oruby.Value) (string, oruby.Value, bool) {
	switch v.Type() {
	case oruby.MrbTTSymbol:
		return mrb.SymString(oruby.MrbSymbol(v)), v, true
	case oruby.MrbTTString:
		return rstr(v), v, true
	}
	if v.IsNil() {
		return "", oruby.Nil, false
	}
	if mrb.RespondTo(v, oruby.Sym_to_str) {
		if s, err := mrb.Funcall(v, oruby.Sym_to_str); err == nil && s.Type() == oruby.MrbTTString {
			return rstr(s), s.Value(), true
		}
		mrb.ExcClear()
	}
	mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
	return "", oruby.Nil, false
}

func regexMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	if r, ok := mrb.Data(self).(*Regexp); !ok || r == nil {
		return mrb.ETypeError().Raisef("uninitialized Regexp")
	}
	s, subjectValue, ok := subject(mrb, args.Item(0))
	if !ok {
		if mrb.HasExc() {
			return mrb.Exc().Value()
		}
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	enc, ok := matchEncoding(mrb, self, subjectValue)
	if !ok {
		return mrb.Exc().Value()
	}
	pos, ok := regexpByteIndex(s, oruby.MrbFixnum(args.ItemDef(1, oruby.MrbFixnumValue(0))), enc)
	if !ok {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}

	regx := regexpOf(mrb, self)
	if regx == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized Regexp")
	}
	matchData := doMatch(mrb, regx, s, pos, enc)
	if mrb.HasExc() {
		return matchData
	}
	if !matchData.Value().IsNil() && matchData.Value().Type() == oruby.MrbTTCData {
		_ = mrb.IVSet(matchData.Value(), oruby.IvSym_regexp, self)
		if subjectValue.IsString() && enc == "UTF-8" {
			mrb.Data(matchData.Value()).(*MatchData).enc = encName(mrb, strEnc(mrb, subjectValue))
		}
	}

	if !block.IsNil() && !matchData.Value().IsNil() {
		return mrb.Yield(block, matchData)
	}
	return matchData
}

func regexEqual(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dest := mrb.GetArgsFirst()
	if _, ok := mrb.Data(dest).(*Regexp); !ok {
		return mrb.FalseValue()
	}
	// as MRI's rb_reg_equal: source, the matching options, the encoding
	// and whether it is fixed (not NOENCODING)
	const mask = IgnoreCase | Extended | Multiline | FixedEncoding
	return mrb.BoolValue(ivString(mrb, self, "@source") == ivString(mrb, dest, "@source") &&
		ivOptions(mrb, self)&mask == ivOptions(mrb, dest)&mask &&
		regexpEncName(mrb, self) == regexpEncName(mrb, dest))
}

func regexCasefold(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(ivOptions(mrb, self)&IgnoreCase != 0)
}

// regexCaseEq is Regexp#===: like =~ but returns true/false (sets $~)
func regexCaseEq(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dest := mrb.GetArgsFirst()
	var s string
	switch dest.Type() {
	case oruby.MrbTTSymbol:
		s = mrb.SymString(oruby.MrbSymbol(dest))
	case oruby.MrbTTString:
		s = rstr(dest)
	default:
		if dest.IsNil() || !mrb.RespondTo(dest, oruby.Sym_to_str) {
			setLastMatch(mrb, mrb.NilValue())
			return oruby.False
		}
		v, err := mrb.Funcall(dest, oruby.Sym_to_str)
		if err != nil || v.Type() != oruby.MrbTTString {
			mrb.ExcClear()
			return oruby.False
		}
		s = rstr(v)
		dest = v.Value()
	}
	enc, ok := matchEncoding(mrb, self, dest)
	if !ok {
		return mrb.Exc().Value()
	}
	md := doMatch(mrb, regexpOf(mrb, self), s, 0, enc)
	if mrb.HasExc() {
		return mrb.Exc().Value()
	}
	return oruby.Bool(!md.Value().IsNil())
}

// regexEqualMatch is Regexp#match?: does not set $~
func regexEqualMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	s, subjectValue, ok := subject(mrb, args.Item(0))
	if !ok {
		if mrb.HasExc() {
			return mrb.Exc().Value()
		}
		return oruby.False
	}
	enc, ok := matchEncoding(mrb, self, subjectValue)
	if !ok {
		return mrb.Exc().Value()
	}
	pos, ok := regexpByteIndex(s, oruby.MrbFixnum(args.ItemDef(1, oruby.MrbFixnumValue(0))), enc)
	if !ok {
		return oruby.False
	}
	m, err := regexpOf(mrb, self).findErr(s, pos, enc)
	if err != nil {
		return raiseTimeout(mrb, err)
	}
	return oruby.Bool(m != nil)
}

func regexNamedCaptures(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := regexpOf(mrb, self).subexpNames()
	ret := mrb.HashNewCapa(len(names))
	for idx, name := range names {
		if idx == 0 || name == "" {
			continue
		}
		key := mrb.StringValue(name)
		ary := ret.Get(key)
		if ary.IsNil() {
			ary = mrb.AryNew().Value()
			ret.Set(key, ary)
		}
		mrb.AryPush(ary, mrb.FixnumValue(idx))
	}
	return ret
}

func regexNames(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := regexpOf(mrb, self).subexpNames()
	ret := mrb.AryNewCapa(len(names))
	seen := map[string]bool{}
	for idx, name := range names {
		if idx == 0 || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		ret.Push(mrb.StringValue(name))
	}
	return ret
}

func regexTryConvert(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if arg.IsNil() {
		return arg
	}
	if _, ok := mrb.Data(arg).(*Regexp); ok {
		return arg
	}

	toRegexp := oruby.Sym_to_regexp
	if !mrb.RespondTo(arg, toRegexp) {
		return mrb.NilValue()
	}

	ret, err := mrb.Funcall(arg, toRegexp)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if ret.IsNil() {
		return ret
	}
	if _, ok := mrb.Data(ret).(*Regexp); !ok || ret.Type() != oruby.MrbTTCData {
		cname := mrb.ClassName(mrb.ObjClass(arg))
		return mrb.ETypeError().Raisef("can't convert %s into Regexp (%s#to_regexp gives %s)", cname, cname, mrb.ClassName(mrb.ObjClass(ret.Value())))
	}
	return ret
}

func regexLastMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	matchValue := mrb.GVGet(mrb.Intern("$~")) // the caller's (frame-local)
	m, ok := mrb.Data(matchValue).(*MatchData)
	if !ok {
		return mrb.NilValue()
	}

	arg := mrb.GetArgsFirst()
	if arg.IsNil() {
		return matchValue
	}
	switch arg.Type() {
	case oruby.MrbTTFixnum, oruby.MrbTTString, oruby.MrbTTSymbol:
	default:
		if !mrb.RespondTo(arg, oruby.Sym_to_int) {
			return mrb.ETypeError().Raisef("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ObjClass(arg)))
		}
	}
	return matchDataGetIndex(mrb, m, arg)
}

func regexTimeout(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return regexpTimeoutValue(mrb, MatchTimeout())
}

func regexInstanceTimeout(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r := regexpOf(mrb, self)
	if r == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized Regexp")
	}
	return regexpTimeoutValue(mrb, r.timeout)
}

func regexpTimeoutValue(mrb *oruby.MrbState, d time.Duration) oruby.MrbValue {
	if d <= 0 {
		return mrb.NilValue()
	}
	return mrb.FloatValue(d.Seconds())
}

func regexSetTimeout(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	d, exc := regexpTimeoutArg(mrb, v)
	if exc != nil {
		return exc
	}
	SetMatchTimeout(d)
	return v
}

func regexpTimeoutArg(mrb *oruby.MrbState, v oruby.Value) (time.Duration, oruby.MrbValue) {
	var seconds float64
	switch v.Type() {
	case oruby.MrbTTFixnum:
		seconds = float64(oruby.MrbFixnum(v))
	case oruby.MrbTTFloat:
		seconds = v.Float64()
	default:
		if v.IsNil() {
			return 0, nil
		}
		if !mrb.ObjIsKindOf(v, mrb.ClassGet("Numeric")) {
			return 0, mrb.Raise(mrb.ETypeError(), "timeout must be a Numeric or nil")
		}
		number, err := mrb.Funcall(v, mrb.Intern("to_f"))
		if err != nil {
			return 0, mrb.RaiseError(err)
		}
		seconds = number.Float64()
	}
	if seconds <= 0 {
		return 0, mrb.Raise(mrb.EArgumentError(), "invalid timeout: "+mrb.String(mrb.Inspect(v)))
	}
	nanos := seconds * float64(time.Second)
	if math.IsNaN(nanos) {
		return 0, nil
	}
	if nanos >= math.MaxInt64 {
		return time.Duration(math.MaxInt64), nil
	}
	return time.Duration(nanos), nil
}

// matchDataPos adapts a group-position accessor; the group is an index or name
func matchDataPos(f func(m *MatchData, i int) (interface{}, error)) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		m := mrb.Data(self).(*MatchData)
		arg := mrb.GetArgsFirst()
		idx := -1
		switch arg.Type() {
		case oruby.MrbTTFixnum:
			idx = oruby.MrbFixnum(arg)
		case oruby.MrbTTString, oruby.MrbTTSymbol:
			name := mrb.String(arg)
			if arg.Type() == oruby.MrbTTSymbol {
				name = mrb.SymString(arg.Symbol())
			}
			for i, n := range m.names {
				if i > 0 && n == name {
					idx = i
				}
			}
			if idx < 0 {
				return mrb.Raise(mrb.EIndexError(), "undefined group name reference: "+name)
			}
		default:
			if mrb.RespondTo(arg, oruby.Sym_to_int) {
				v, err := mrb.Funcall(arg, oruby.Sym_to_int)
				if err != nil {
					return mrb.RaiseError(err)
				}
				if v.Type() != oruby.MrbTTFixnum {
					cname := mrb.ClassName(mrb.ObjClass(arg))
					return mrb.ETypeError().Raisef("can't convert %s into Integer (%s#to_int gives %s)", cname, cname, mrb.ClassName(mrb.ObjClass(v.Value())))
				}
				idx = oruby.MrbFixnum(v.Value())
			} else {
				return mrb.ETypeError().Raisef("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ObjClass(arg)))
			}
		}
		v, err := f(m, idx)
		if err != nil {
			return mrb.Raise(mrb.EIndexError(), err.Error())
		}
		return mrb.Value(v)
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func encodingConst(mrb *oruby.MrbState, name string) oruby.MrbValue {
	obj := mrb.ObjectClass()
	if !obj.ConstDefinedAt("Encoding") {
		return mrb.NilValue()
	}
	encv := obj.ConstGet("Encoding")
	if encv.Type() != oruby.MrbTTClass && encv.Type() != oruby.MrbTTModule {
		return mrb.NilValue()
	}
	enc := mrb.ClassPtr(encv)
	if !enc.ConstDefinedAt(name) {
		return mrb.NilValue()
	}
	return enc.ConstGet(name)
}

// regexEncoding is Regexp#encoding
func regexEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return findEncoding(mrb, regexpEncName(mrb, self))
}

func regexFixedEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if r := regexpOf(mrb, self); r != nil && r.enc != "" {
		return oruby.Bool(ivOptions(mrb, self)&FixedEncoding != 0)
	}
	opts := ivOptions(mrb, self)
	if opts&NoEncoding != 0 {
		return oruby.False
	}
	return oruby.Bool(opts&FixedEncoding != 0 || !isASCII(ivString(mrb, self, "@source")))
}
