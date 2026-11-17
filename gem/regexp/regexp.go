package regexp

import (
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
	oruby.Gem("regexp", func(mrb *oruby.MrbState) interface{} {
		// StringScanner is provided by stdlib gem (require "strscan")

		data := mrb.DefineGoClass("MatchData", &MatchData{})
		data.DefineMethod("[]", matchDataIndex, mrb.ArgsArg(1, 1))
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

		cls.AttrReader("source")
		cls.UndefMethod("initialize")
		cls.DefineMethod("initialize", regexInit, mrb.ArgsArg(1, 3)+mrb.ArgsBlock())
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

		cls.DefineClassFunc("quote", QuoteMeta)
		cls.DefineClassFunc("escape", QuoteMeta)
		cls.DefineClassMethod("compile", regexCompile, mrb.ArgsArg(1, 2)+mrb.ArgsBlock())
		cls.DefineClassMethod("last_match", regexLastMatch, mrb.ArgsOpt(1))
		cls.DefineClassMethod("try_convert", regexTryConvert, mrb.ArgsReq(1))
		cls.DefineClassMethod("union", regexUnion, mrb.ArgsAny())
		cls.DefineClassMethod("timeout", regexTimeout, mrb.ArgsNone())
		cls.DefineClassMethod("timeout=", regexSetTimeout, mrb.ArgsReq(1))
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
	source := args.Item(0)
	options := args.ItemDef(1, mrb.NilValue())
	ret, _ := mrb.FuncallWithBlock(self, oruby.Sym_new, source, options, block)
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

	return oruby.Bool(r != nil && r.re2 != nil)
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

func regexInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	source := args.Item(0)
	var src string
	var opts int

	if other, ok := mrb.Data(source).(*Regexp); ok {
		// Regexp.new(regexp) copies source and options
		src, opts = other.source, other.options
	} else {
		if source.Type() != oruby.MrbTTString {
			if !mrb.RespondTo(source, oruby.Sym_to_str) {
				return mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
			}
			source = mrb.Call(source, "to_str")
		}
		src = mrb.String(source)
		opts = rubyOptions(mrb, args.ItemDef(1, mrb.NilValue()))
		// third argument of Regexp.compile from literals: "n" (/re/n) or "u"
		if enc := args.ItemDef(2, mrb.NilValue()).Value(); enc.Type() == oruby.MrbTTString {
			switch rstr(enc) {
			case "n", "N":
				opts |= NoEncoding
			case "u", "U":
				opts |= FixedEncoding
			}
		}
	}

	r, err := compileRuby(src, opts)
	if err != nil {
		return raiseRegexpError(mrb, err)
	}
	mrb.DataSetInterface(self, r)
	_ = mrb.IVSet(self, oruby.IvSym_source, mrb.StrNew(src))
	_ = mrb.IVSet(self, oruby.IvSym_options, mrb.FixnumValue(opts))
	return self
}

func regexInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src := mrb.GetArgsFirst()
	if r, ok := mrb.Data(src).(*Regexp); ok {
		// compiled patterns are immutable and safe to share
		mrb.DataSetInterface(self, r)
		_ = mrb.IVSet(self, oruby.IvSym_source, mrb.StrNew(r.source))
		_ = mrb.IVSet(self, oruby.IvSym_options, mrb.FixnumValue(r.options))
	}
	return self
}

func regexpOf(mrb *oruby.MrbState, self oruby.Value) *Regexp {
	r, _ := mrb.Data(self).(*Regexp)
	return r
}

func matchDataGetIndex(mrb *oruby.MrbState, m *MatchData, idx oruby.Value) oruby.Value {
	s := ""
	switch idx.Type() {
	case oruby.MrbTTSymbol:
		s = mrb.SymString(idx.Symbol())
	case oruby.MrbTTString:
		s = rstr(idx)
	case oruby.MrbTTRange:
		a := m.ToA()
		beg, l, err := mrb.RangeBegLen(idx, len(a), true)
		if err != nil {
			return mrb.NilValue()
		}
		return mrb.Value(a[beg : beg+l])
	case oruby.MrbTTFixnum:
		i := oruby.MrbFixnum(idx)
		if i < 0 {
			i += m.Size()
		}
		if s, isNil := m.toString(i); !isNil {
			return mrb.StrNew(s)
		}
		return mrb.NilValue()
	default:
		if mrb.RespondTo(idx, oruby.Sym_to_int) {
			toI := mrb.Call(idx, "to_int")
			if toI.Type() != oruby.MrbTTFixnum {
				return mrb.NilValue()
			}
			return strOrNil(mrb, m.StringOrNil(oruby.MrbFixnum(toI))).Value()
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
			result = mrb.StrNew(str)
		}
	}
	if !found {
		return mrb.Raise(mrb.EIndexError(), "undefined group name reference: "+s)
	}
	return result
}

func matchDataIndex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.Data(self).(*MatchData)
	return matchDataGetIndex(mrb, data, mrb.GetArgsFirst())
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
	on, off := optionFlags(ivOptions(mrb, self))
	s := "(?" + on
	if off != "" {
		s += "-" + off
	}
	return mrb.StringValue(s + ":" + ivString(mrb, self, "@source") + ")")
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
	return mrb.FixnumValue(ivOptions(mrb, self))
}

func regexHash(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	h := 0
	for _, c := range ivString(mrb, self, "@source") {
		h = h*31 + int(c)
	}
	return mrb.FixnumValue(h ^ ivOptions(mrb, self))
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
	return mrb.FixnumValue(charIndex(data.s, idx))
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

func setLastMatch(mrb *oruby.MrbState, matchData oruby.Value) {
	regexpClass := mrb.ClassGet("Regexp")
	_ = mrb.IVSet(regexpClass, oruby.IvSym_last_match, matchData)
	mrb.GVSet(mrb.Intern("$~"), matchData)
}

func newMatchValue(mrb *oruby.MrbState, re *Regexp, s string, result []int) oruby.Value {
	m := NewMatchData(re, s, result, 0)
	return mrb.NewInstance("MatchData", m).Value()
}

// doMatch matches at byte offset pos and sets $~; returns nil value on no match
func doMatch(mrb *oruby.MrbState, re *Regexp, s string, pos int) oruby.MrbValue {
	result, err := re.findErr(s, pos)
	if err != nil {
		return raiseTimeout(mrb, err)
	}
	if len(result) == 0 {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	matchData := newMatchValue(mrb, re, s, result)
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
func subject(mrb *oruby.MrbState, v oruby.Value) (string, bool) {
	switch v.Type() {
	case oruby.MrbTTSymbol:
		return mrb.SymString(oruby.MrbSymbol(v)), true
	case oruby.MrbTTString:
		return rstr(v), true
	}
	if v.IsNil() {
		return "", false
	}
	if mrb.RespondTo(v, oruby.Sym_to_str) {
		if s, err := mrb.Funcall(v, oruby.Sym_to_str); err == nil && s.Type() == oruby.MrbTTString {
			return rstr(s), true
		}
		mrb.ExcClear()
	}
	mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
	return "", false
}

func regexMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	s, ok := subject(mrb, args.Item(0))
	if !ok {
		if mrb.HasExc() {
			return mrb.Exc().Value()
		}
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	pos, ok := byteIndex(s, oruby.MrbFixnum(args.ItemDef(1, oruby.MrbFixnumValue(0))))
	if !ok {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}

	regx := regexpOf(mrb, self)
	matchData := doMatch(mrb, regx, s, pos)
	if !matchData.Value().IsNil() && matchData.Value().Type() == oruby.MrbTTCData {
		_ = mrb.IVSet(matchData.Value(), oruby.IvSym_regexp, self)
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
	return mrb.BoolValue(ivString(mrb, self, "@source") == ivString(mrb, dest, "@source") &&
		ivOptions(mrb, self) == ivOptions(mrb, dest))
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
	}
	md := doMatch(mrb, regexpOf(mrb, self), s, 0)
	if mrb.HasExc() {
		return mrb.Exc().Value()
	}
	return oruby.Bool(!md.Value().IsNil())
}

// regexEqualMatch is Regexp#match?: does not set $~
func regexEqualMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	s, ok := subject(mrb, args.Item(0))
	if !ok {
		if mrb.HasExc() {
			return mrb.Exc().Value()
		}
		return oruby.False
	}
	pos, ok := byteIndex(s, oruby.MrbFixnum(args.ItemDef(1, oruby.MrbFixnumValue(0))))
	if !ok {
		return oruby.False
	}
	m, err := regexpOf(mrb, self).findErr(s, pos)
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
	if (err != nil) || (ret.Type() != oruby.MrbTTCData) {
		return mrb.NilValue()
	}
	if _, ok := mrb.Data(ret).(*Regexp); !ok {
		return mrb.NilValue()
	}
	return ret
}

// regexUnion is Regexp.union: strings are escaped, regexps keep their
// options via to_s
func regexUnion(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) == 1 && args[0].Type() == oruby.MrbTTArray {
		args = oruby.RArraySlice(args[0])
	}
	if len(args) == 1 {
		if _, ok := mrb.Data(args[0]).(*Regexp); ok {
			return args[0]
		}
	}
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if _, ok := mrb.Data(a).(*Regexp); ok {
			parts = append(parts, mrb.String(regexToS(mrb, a).Value()))
			continue
		}
		if a.Type() != oruby.MrbTTString {
			if !mrb.RespondTo(a, oruby.Sym_to_str) {
				return mrb.Raise(mrb.ETypeError(), "no implicit conversion of "+mrb.ClassOf(a).Name()+" into String")
			}
			a = mrb.Call(a, "to_str")
		}
		parts = append(parts, QuoteMeta(mrb.String(a)))
	}
	src := "(?!)"
	if len(parts) > 0 {
		src = strings.Join(parts, "|")
	}
	r, err := mrb.ObjNew(mrb.ClassPtr(self), mrb.StrNew(src))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return r
}

func regexLastMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	matchValue := mrb.IVGet(self, oruby.IvSym_last_match)
	m, ok := mrb.Data(matchValue).(*MatchData)
	if !ok {
		return mrb.NilValue()
	}

	arg := mrb.GetArgsFirst()
	if arg.IsNil() {
		return matchValue
	}
	return matchDataGetIndex(mrb, m, arg)
}

func regexTimeout(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := MatchTimeout()
	if d <= 0 {
		return mrb.NilValue()
	}
	return mrb.FloatValue(d.Seconds())
}

// regexSetTimeout is Regexp.timeout=; it applies to regexps compiled
// afterwards by the backtracking engine (RE2 matching is linear-time).
func regexSetTimeout(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	var d time.Duration
	switch v.Type() {
	case oruby.MrbTTFixnum:
		d = time.Duration(oruby.MrbFixnum(v)) * time.Second
	case oruby.MrbTTFloat:
		d = time.Duration(v.Float64() * float64(time.Second))
	default:
		if !v.IsNil() {
			return mrb.Raise(mrb.ETypeError(), "timeout must be a Numeric or nil")
		}
	}
	if d < 0 {
		return mrb.Raise(mrb.EArgumentError(), "invalid timeout")
	}
	SetMatchTimeout(d)
	return v
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
			return mrb.Raise(mrb.ETypeError(), "no implicit conversion into Integer")
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

// regexEncoding is Regexp#encoding: US-ASCII for ASCII-only sources without
// a fixed encoding, ASCII-8BIT for /n with non-ASCII bytes, otherwise UTF-8
func regexEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src := ivString(mrb, self, "@source")
	opts := ivOptions(mrb, self)
	switch {
	case opts&NoEncoding != 0 && !isASCII(src):
		return encodingConst(mrb, "ASCII_8BIT")
	case isASCII(src) && opts&FixedEncoding == 0:
		return encodingConst(mrb, "US_ASCII")
	}
	return encodingConst(mrb, "UTF_8")
}

func regexFixedEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	opts := ivOptions(mrb, self)
	if opts&NoEncoding != 0 {
		return oruby.False
	}
	return oruby.Bool(opts&FixedEncoding != 0 || !isASCII(ivString(mrb, self, "@source")))
}
