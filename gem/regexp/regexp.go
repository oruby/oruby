package regexp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/oruby/oruby"
)

// oruby regex options
const (
	IgnoreCase = 1
	Extended   = 2
	Multiline  = 4
)

func init() {
	oruby.Gem("regexp", func(mrb *oruby.MrbState) interface{} {
		scanner := mrb.DefineGoClass("StringScanner", NewStringScanner)
		scanner.DefineAlias("exist?", "exist")

		data := mrb.DefineGoClass("MatchData", &MatchData{})
		data.DefineMethod("[]", matchDataIndex, mrb.ArgsArg(1, 1))
		data.DefineMethod("values_at", matchGoValuesAt, mrb.ArgsAny())
		data.DefineAlias("length", "size")

		cls := mrb.DefineGoClass("Regexp", regexp.Compile)
		cls.Const("IGNORECASE", IgnoreCase)
		cls.Const("EXTENDED", Extended)
		cls.Const("MULTILINE", Multiline)

		cls.DefineAlias("match_bytes", "match")
		cls.DefineAlias("match", "match_string")
		cls.AttrReader("source")
		cls.UndefMethod("initialize")
		cls.DefineMethod("initialize", regexInit, mrb.ArgsArg(1, 2)+mrb.ArgsBlock())
		cls.DefineMethod("after_init", regexAfterInit, mrb.ArgsNone())
		cls.DefineMethod("initialize_copy", regexInitCopy, mrb.ArgsReq(1))
		cls.UndefMethod("match")
		cls.DefineMethod("match", regexMatch, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
		cls.DefineMethod("==", regexEqual, mrb.ArgsReq(1))
		cls.DefineAlias("eql?", "==")
		cls.DefineMethod("===", regexEqualMatch, mrb.ArgsArg(1, 1))
		cls.DefineAlias("match?", "===")
		cls.DefineMethod("=~", regexMatchOperator, mrb.ArgsReq(1))
		cls.DefineMethod("casefold?", regexCasefold, mrb.ArgsNone())
		cls.DefineMethod("to_s", regexToS, mrb.ArgsNone())
		cls.DefineMethod("inspect", regexInspect, mrb.ArgsNone())
		cls.DefineMethod("named_captures", regexNamedCaptures, mrb.ArgsNone())
		cls.DefineMethod("names", regexNames, mrb.ArgsNone())

		cls.DefineClassFunc("match", regexp.MatchString)
		cls.DefineClassFunc("quote", regexp.QuoteMeta)
		cls.DefineClassFunc("escape", regexp.QuoteMeta)
		cls.DefineClassMethod("compile", regexCompile, mrb.ArgsArg(1, 2)+mrb.ArgsBlock())
		cls.DefineClassMethod("last_match", regexLastMatch, mrb.ArgsOpt(1))
		cls.DefineClassMethod("try_convert", regexTryConvert, mrb.ArgsReq(1))
		cls.DefineClassMethod("union", regexUnion, mrb.ArgsAny())

		mrb.DefineGlobalConst("Regexp", cls)

		initString(mrb)
		return nil
	})
}

func cleanExtended(s string, options int) string {
	if options&Extended == 0 {
		return s
	}

	// Remove leading whitespace
	// Remove the first unescaped `#`, and everything that follows
	// any preceding unescaped spaces,
	// and then remove trailing whitespace on each line, including linebreaks
	str := regexp.MustCompile("(?m)(^\\s+|\\s+$| *#.*(\\z|$)|\\n)").ReplaceAllString(s, "")
	return str
}

func sourceWithOptions(reg string, options int) string {
	y := ""
	n := "-"

	if options&IgnoreCase > 0 {
		y += "i"
	} else {
		n += "i"
	}
	// EXTENDED flag - Ignoring. Comments are not supported in Go regexp
	//if options&EXTENDED > 0 { y+="x" } else { n+="x" }
	if options&Multiline > 0 {
		y += "ms"
	} else {
		n += "ms"
	}
	if n == "-" {
		n = ""
	}

	return fmt.Sprintf("(?%v%v)%v", y, n, cleanExtended(reg, options))
}

func regexCompile(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	source := args.Item(0)
	options := args.Item(1)
	ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("new"), source, options, block)
	return ret
}

// regexAfterInit is called when precompiled *Regex is passed to oruby
//
//	@source -  set from Regex.Source()
//	@options - guessed from source string
func regexAfterInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	reg := mrb.Data(self).(*regexp.Regexp)
	source := reg.String()
	options := 0

	_ = mrb.IVSet(self, mrb.Intern("@source"), mrb.Value(source))

	if strings.HasPrefix(source, "(?m") {
		options = options | Multiline
	}
	if strings.HasPrefix(source, "(?i") {
		options = options | IgnoreCase
	}
	if strings.HasPrefix(source, "(?im") {
		options = options | Multiline | IgnoreCase
	}

	_ = mrb.IVSet(self, mrb.Intern("@options"), mrb.Value(options))
	return self
}

func regexInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	source := args.Item(0)
	options := args.Item(1)
	obj := mrb.RValue(self)

	if reg, ok := mrb.Data(source).(*regexp.Regexp); ok {
		// Source is Go regex, with prepared source
		source = mrb.StringValue(reg.String()).Value()
		options = mrb.IVGet(source, mrb.Intern("@options"))
		obj.Call("init_go", source)
	} else {
		// Actual source and Go Regex.Source() can be different as Go regex does
		// not support EXTENDED flag. Other flags are set via source string
		src := sourceWithOptions(mrb.String(source), oruby.MrbFixnum(options))

		// Init Go object with Go compatible source string
		obj.Call("init_go", src)
	}

	// Override attributes with values provided by oruby constructor
	obj.SetIV("@source", source)
	obj.SetIV("@options", options)

	return obj
}

func regexInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	r := *mrb.Data(mrb.GetArgsFirst()).(*regexp.Regexp)
	mrb.DataSetInterface(self, &r)
	return self
}

func matchDataGetIndex(mrb *oruby.MrbState, m *MatchData, idx oruby.Value) oruby.Value {
	s := ""
	switch idx.Type() {
	case oruby.MrbTTSymbol:
		s = mrb.SymString(idx.Symbol())
	case oruby.MrbTTString:
		s = mrb.StrToCstr(idx)
	case oruby.MrbTTRange:
		a := m.ToA()
		beg, l, err := mrb.RangeBegLen(idx, len(a), true)
		if err != nil {
			return mrb.NilValue()
		}
		return mrb.Value(a[beg : beg+l])
	case oruby.MrbTTFixnum:
		i := oruby.MrbFixnum(idx)
		if s, isNil := m.toString(i); !isNil {
			return mrb.StrNew(s)
		}
		return mrb.NilValue()
	default:
		if mrb.RespondTo(idx, mrb.Intern("to_i")) {
			toI := mrb.Call(idx, "to_i")
			if toI.Type() != oruby.MrbTTFixnum {
				return mrb.NilValue()
			}
			return mrb.Value(m.StringOrNil(oruby.MrbFixnum(toI)))
		}
		return mrb.NilValue()
	}

	for i, name := range m.Names() {
		if i < m.Size() && name == s {
			if str, isNil := m.toString(i + 1); !isNil {
				return mrb.StrNew(str)
			}
			return mrb.NilValue()
		}
	}

	return mrb.NilValue()
}

func matchDataIndex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.Data(self).(*MatchData)
	return matchDataGetIndex(mrb, data, mrb.GetArgsFirst())
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

func regexToS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	obj := mrb.RValue(self)
	source := obj.GetIV("@source").String()
	options := obj.GetIV("@options").Int()

	y := ""
	n := "-"

	if options&Multiline > 0 {
		y += "m"
	} else {
		n += "m"
	}
	if options&IgnoreCase > 0 {
		y += "i"
	} else {
		n += "i"
	}
	if options&Extended > 0 {
		y += "x"
	} else {
		n += "x"
	}
	if n == "-" {
		n = ""
	}

	if options&Extended > 0 {
		source += "\n # GoRegexp: " + sourceWithOptions(source, options) + "\n"

	}
	s := fmt.Sprintf("(?%v%v:%v)", y, n, source)

	return mrb.StringValue(s)
}

func regexInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	obj := mrb.RValue(self)
	source := obj.GetIV("@source").String()
	options := obj.GetIV("@options").Int()

	y := ""
	if options&Multiline > 0 {
		y += "m"
	}
	if options&IgnoreCase > 0 {
		y += "i"
	}
	if options&Extended > 0 {
		y += "x"
	}
	s := fmt.Sprintf("/%v/%v", source, y)

	return mrb.StringValue(s)
}

func regexMatchOperator(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	ret := regexMatch(mrb, self)
	if ret.IsNil() {
		return ret
	}
	data := mrb.Data(ret).(*MatchData)
	idx, _ := data.Begin(0)

	return mrb.FixnumValue(idx)
}

func doMatch(mrb *oruby.MrbState, re *regexp.Regexp, s string, pos int) oruby.MrbValue {
	result := re.FindStringSubmatchIndex(s[pos:])

	regexpClass := mrb.ClassGet("Regexp")
	if len(result) == 0 {
		_ = mrb.IVSet(regexpClass, mrb.Intern("@last_match"), mrb.NilValue())
		mrb.GVSet(mrb.Intern("$~"), mrb.NilValue())
		mrb.GVSet(mrb.Intern("$&"), mrb.NilValue())
		mrb.GVSet(mrb.Intern("$`"), mrb.NilValue())
		mrb.GVSet(mrb.Intern("$'"), mrb.NilValue())
		for i := '1'; i == '9'; i++ {
			mrb.GVSet(mrb.Intern("$"+string(i)), mrb.NilValue())
		}

		return mrb.NilValue()
	}

	m := NewMatchData(re, s, result, pos)
	matchData := mrb.NewInstance("MatchData", m)

	// set last_match and related global consts
	_ = mrb.IVSet(regexpClass, mrb.Intern("@last_match"), matchData)
	mrb.GVSet(mrb.Intern("$~"), matchData)
	mrb.GVSet(mrb.Intern("$&"), mrb.StringValue(m.ToS()))
	mrb.GVSet(mrb.Intern("$`"), mrb.StringValue(m.PreMatch()))
	mrb.GVSet(mrb.Intern("$'"), mrb.StringValue(m.PostMatch()))
	for i := '1'; i == '9'; i++ {
		idx := int(i - '0')
		v := m.StringOrNil(idx)
		mrb.GVSet(mrb.Intern("$"+string(i)), mrb.Value(v))
	}

	return matchData
}

func regexMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var s string
	args, block := mrb.GetArgsWithBlock()
	str := args.Item(0)
	pos := oruby.MrbFixnum(args.ItemDef(1, oruby.MrbFixnumValue(0)))
	switch str.Type() {
	case oruby.MrbTTSymbol:
		s = mrb.SymString(oruby.MrbSymbol(str))
	case oruby.MrbTTString:
		s = mrb.StrToCstr(str)
	default:
		return mrb.NilValue()
	}

	regx := mrb.Data(self).(*regexp.Regexp)
	matchData := doMatch(mrb, regx, s, pos)

	// Yield if block given
	if !block.IsNil() && !matchData.IsNil() {
		return mrb.Yield(block, matchData)
	}

	return matchData
}

func regexEqual(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	source := mrb.IVGet(self, mrb.Intern("@source"))
	dest := mrb.GetArgsFirst()
	if _, ok := mrb.Data(dest).(*regexp.Regexp); !ok {
		return mrb.FalseValue()
	}

	target := mrb.IVGet(dest, mrb.Intern("@source"))
	return mrb.BoolValue(mrb.StrEqual(source, target))
}

func regexCasefold(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	options := mrb.IVGet(self, mrb.Intern("@options"))
	isCaseIgnored := (oruby.MrbFixnum(options) & IgnoreCase) > 0

	return oruby.Bool(isCaseIgnored)
}

// regexEqualMatch used in case statements, return true or false
func regexEqualMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var s string
	args := mrb.GetArgs()
	dest := args.Item(0)
	pos := oruby.MrbFixnum(args.ItemDef(1, oruby.MrbFixnumValue(0)))

	switch dest.Type() {
	case oruby.MrbTTSymbol:
		s = mrb.SymString(oruby.MrbSymbol(dest))
	case oruby.MrbTTString:
		s = mrb.StrToCstr(dest)
	default:
		return oruby.False
	}

	regx := mrb.Data(self).(*regexp.Regexp)
	return oruby.Bool(regx.MatchString(s[pos:]))
}

func regexNamedCaptures(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	regx := mrb.Data(self).(*regexp.Regexp)
	names := regx.SubexpNames()
	ret := mrb.HashNewCapa(len(names))
	idx := 0
	for _, name := range names {
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
		idx++
	}
	return ret
}

func regexNames(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	regx := mrb.Data(self).(*regexp.Regexp)
	names := regx.SubexpNames()
	ret := mrb.AryNewCapa(len(names))
	for idx, name := range names {
		if idx == 0 || name == "" {
			continue
		}
		ret.Push(mrb.StringValue(name))
	}
	return ret
}

func regexTryConvert(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if arg.IsNil() {
		return arg
	}

	toRegexp := mrb.Intern("to_regexp")
	if !mrb.RespondTo(arg, toRegexp) {
		return mrb.NilValue()
	}

	ret, err := mrb.Funcall(arg, toRegexp)
	if (err != nil) || (ret.Type() != oruby.MrbTTCData) {
		return mrb.NilValue()
	}
	if _, ok := mrb.Data(ret).(*regexp.Regexp); !ok {
		return mrb.NilValue()
	}

	return ret
}

func regexUnion(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	ret := make([]string, 0, args.Len())
	for i := 0; i < args.Len(); i++ {
		switch args.Item(i).Type() {
		case oruby.MrbTTString:
			ret = append(ret, regexp.QuoteMeta(mrb.String(args.Item(i))))
		case oruby.MrbTTCData:
			re, ok := mrb.Data(args.Item(i)).(*regexp.Regexp)
			if !ok {
				mrb.Raise(mrb.ETypeError(), "regexp or string expected")
			}
			ret = append(ret, "("+re.String()+")")
		default:
			mrb.Raise(mrb.ETypeError(), "no implicit conversion of Integer into String")
		}
	}

	if len(ret) == 0 {
		r, _ := mrb.ObjNew(mrb.ClassOf(self), "(?!)")
		return r
	}

	r, _ := mrb.ObjNew(mrb.ClassOf(self), strings.Join(ret, "|"))
	return r
}

func regexLastMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	matchValue := mrb.IVGet(self, mrb.Intern("@last_match"))
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
