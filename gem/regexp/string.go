package regexp

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

func initString(mrb *oruby.MrbState) {
	str := mrb.StringClass()

	str.DefineMethod("=~", stringMatchEqual, mrb.ArgsReq(1))
	str.DefineMethod("match", stringMatch, mrb.ArgsArg(1, 1))
	str.DefineMethod("match?", stringIsMatch, mrb.ArgsArg(1, 1))
	str.DefineAlias("old_index", "index")
	str.DefineMethod("index", stringIndex, mrb.ArgsArg(1, 1))
	str.DefineAlias("old_sub", "sub")
	str.DefineMethod("sub", stringSub, mrb.ArgsReq(1))
	// str.{
	//DefineMethodf("sub!", stringSubBang, mrb.ArgsReq(1))
	str.DefineAlias("old_gsub", "gsub")
	str.DefineMethod("gsub", stringGsub, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineMethod("gsub!", stringGsubBang, mrb.ArgsReq(1))
	str.DefineMethod("scan", stringScan, mrb.ArgsReq(1))
	str.DefineAlias("old_slice", "slice")
	str.DefineMethod("slice", stringSlice, mrb.ArgsArg(1, 2))
	//str.DefineMethod("slice!", stringSliceBang, mrb.ArgsArg(1,2))
	str.DefineAlias("old_split", "split")
	str.DefineMethod("split", stringSplit, mrb.ArgsOpt(2))
}

func stringSplit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	pattern := args.ItemDefFunc(0, func() oruby.MrbValue { return mrb.GetGV("$;") })
	limit := args.ItemDefInt(1, 0)
	limited := limit > 0

	if self.Value().Len() == 0 {
		return mrb.AryNew()
	}
	if limit == 1 {
		return mrb.AryNewFromValues(self)
	}

	re, ok := mrb.Data(pattern).(*regexp.Regexp)
	if !ok {
		mrb.Call(self, "old_split", pattern, limit)
	}

	result := mrb.AryNew()
	s := mrb.String(self)

	// Empty regex, eg. "some_string",split(//) -> ["s","o","m","e"..."g"]
	if re.String() == "" {
		for _, r := range []rune(s) {
			if limited && (limit-result.Len() <= 1) {
				break
			}

			result.Push(mrb.StringValue(string(r)))
		}

		for limit == 0 && result.Len() > 0 && result.Item(-1).Len() == 0 {
			result.Pop()
		}
		return result
	}

	start := 0
	end := 0
	var lastMatch *MatchData

	matchValue := doMatch(mrb, re, s, start)
	for !matchValue.IsNil() {
		m := mrb.Data(matchValue).(*MatchData)

		if limited && (limit-result.Len() <= 1) {
			break
		}

		if (m.ToS() != "") || (m.result[0] != end) {
			result.PushString(s[end:])
			for i := 1; i < m.Size(); i++ {
				str, _ := m.toString(i)
				result.Push(mrb.StringValue(str))
			}
		}

		if m.ToS() == "" {
			start++
		} else if lastMatch != nil && lastMatch.ToS() == "" {
			start = m.allEnd() + 1
		} else {
			start = m.allEnd()
		}

		lastMatch = m
		end = m.allEnd()

		if len(s) <= start {
			break
		}

		matchValue = doMatch(mrb, re, mrb.String(self), start)
	}

	if lastMatch != nil {
		result.PushString(lastMatch.PostMatch())
	}

	for limit == 0 && result.Len() > 0 && result.Item(-1).Len() == 0 {
		result.Pop()
	}
	return result
}

func stringSlice(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	pattern := args.Item(0)
	capture := args.ItemDef(1, mrb.NilValue())
	r := getRegexp(mrb, pattern)
	if r == nil {
		return mrb.Call(self, "old_slice", pattern, capture)
	}
	matchData := doMatch(mrb, r, mrb.String(self), 0)
	if matchData.IsNil() {
		return matchData
	}

	m := mrb.Data(matchData).(*MatchData)
	if capture.IsNil() {
		return mrb.Value(m.StringOrNil(0))
	}

	if capture.Type() == oruby.MrbTTFixnum {
		return mrb.Value(m.StringOrNil(capture.Int()))
	}
	return mrb.Value(m.NamedCaptures()[mrb.String(capture)])
}

func stringSub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	pattern := args.Item(0)
	replace := args.ItemDef(1, mrb.NilValue())
	replaceStr := ""
	r := getRegexp(mrb, pattern)
	if r == nil {
		ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("old_sub"), pattern, replace, block)
		return ret
	}
	if block.IsNil() && replace.IsNil() {
		return mrb.Call(self, "to_enum", mrb.Intern("gsub"), pattern)
	}
	if block.IsNil() && replace.Type() != oruby.MrbTTHash {
		//	return r.ReplaceAllString(s, replaceStr)
		replaceStr = mrb.EnsureStringType(replace).String()
	}

	s, _ := doReplace(mrb, r, mrb.String(self), 0, func(v string) (string, bool) {
		if !block.IsNil() {
			ret, _ := mrb.Yield(block, mrb.StringValue(v))
			return mrb.String(ret), true
		} else if replace.Type() == oruby.MrbTTHash {
			if mrb.HashKeyP(replace, mrb.StringValue(v)) {
				ret := mrb.HashGet(replace, mrb.StringValue(v))
				return mrb.String(ret), true
			}
			return v, true
		} else {
			return replaceStr, false
		}
	})

	return mrb.StringValue(s)
}

func stringGsubBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	self = stringGsub(mrb, self).Value()
	return self
}

func doReplace(mrb *oruby.MrbState, re *regexp.Regexp, s string, pos int, f func(v string) (string, bool)) (string, int) {
	matchValue := doMatch(mrb, re, s, pos)
	if matchValue.IsNil() {
		return s, len(s)
	}
	m := mrb.Data(matchValue).(*MatchData)
	ret := s
	for i := 1; i < m.Size(); i++ {
		capture, isNil := m.toString(i)
		if isNil {
			continue
		}
		replace, lit := f(capture)
		if lit && (replace == capture) {
			continue
		}
		if !lit {
			for i, name := range m.Names() {
				v, isNil := m.toString(i)
				if isNil {
					continue
				}
				if name != "" {
					replace = strings.ReplaceAll(replace, "\\k<"+name+">", v)
				} else {
					replace = strings.ReplaceAll(replace, "\\"+strconv.Itoa(i), v)
				}
			}
		}
		start, _ := m.Begin(i)
		end, _ := m.End(i)

		ret = ret[:start-1] + replace + ret[end+1:]

		// Adjust sizes if replacment is different size
		diff := len(capture) - len(replace)
		m.result[2*i+1] += diff
		for _, idx := range m.result {
			if idx > i {
				m.result[idx] += diff
			}
		}
	}
	return ret, m.allEnd()
}

func stringGsub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	pattern := args.Item(0)
	replace := args.ItemDef(1, mrb.NilValue())
	replaceStr := ""
	r := getRegexp(mrb, pattern)
	if r == nil {
		ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("old_gsub"), pattern, replace, block)
		return ret
	}
	if block.IsNil() && replace.IsNil() {
		return mrb.Call(self, "to_enum", mrb.Intern("gsub"), pattern)
	}
	if block.IsNil() && replace.Type() != oruby.MrbTTHash {
		//	return r.ReplaceAllString(s, replaceStr)
		replaceStr = mrb.EnsureStringType(replace).String()
	}

	s := mrb.String(self)
	pos := 0
	for pos < len(s) {
		s, pos = doReplace(mrb, r, s, pos, func(v string) (string, bool) {
			if !block.IsNil() {
				ret, _ := mrb.Yield(block, mrb.StringValue(v))
				return mrb.String(ret), true
			} else if replace.Type() == oruby.MrbTTHash {
				if mrb.HashKeyP(replace, mrb.StringValue(v)) {
					ret := mrb.HashGet(replace, mrb.StringValue(v))
					return mrb.String(ret), true
				}
				return v, true
			} else {
				return replaceStr, false
			}
		})
	}
	return mrb.StringValue(s)
}

func stringIsMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	re := getRegexp(mrb, args.Item(0))
	if re == nil {
		return mrb.Raise(mrb.ETypeError(), "wrong argument type (expected Regexp)")
	}
	s := mrb.String(self)
	pos := args.ItemDef(1, mrb.FixnumValue(0)).Int()

	result := re.FindStringSubmatchIndex(s[pos:])
	return mrb.BoolValue(len(result) > 0)
}

func stringMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	re := getRegexp(mrb, args.Item(0))
	if re == nil {
		return mrb.NilValue()
	}
	pos := args.ItemDef(1, mrb.FixnumValue(0)).Int()
	return doMatch(mrb, re, mrb.String(self), pos)
}

func stringMatchEqual(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	ret := stringMatch(mrb, self)
	if ret.IsNil() {
		return ret
	}
	data := mrb.Data(ret).(*MatchData)
	idx, _ := data.Begin(0)

	return mrb.FixnumValue(idx)
}

func stringIndex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	arg := args.Item(0)
	pos := oruby.MrbFixnum(args.ItemDef(1, mrb.FixnumValue(0)))
	re, ok := mrb.Data(arg).(*regexp.Regexp)
	if !ok {
		return mrb.Call(self, "old_index", arg, pos)
	}
	s := mrb.StrToCstr(self)

	// Negative pos is position from last
	if pos < 0 {
		pos += len(s)
	}
	// Invalid position - return nil
	if pos < 0 || pos >= len(s) {
		return mrb.NilValue()
	}

	result := re.FindStringSubmatchIndex(s[pos:])
	if len(result) == 0 {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(result[0] + pos)
}

func getRegexp(mrb *oruby.MrbState, value oruby.Value) *regexp.Regexp {
	switch value.Type() {
	case oruby.MrbTTData:
		if reg, ok := mrb.Data(value).(*regexp.Regexp); ok {
			return reg
		}
	case oruby.MrbTTString:
		s := mrb.StringCstr(value)
		return regexp.MustCompile(regexp.QuoteMeta(s))
	}
	return nil
}

func stringScan(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	arg := args.Item(0)
	r := getRegexp(mrb, arg)
	if r == nil {
		return mrb.NilValue()
	}

	s := mrb.StringCstr(self)
	results := r.FindAllStringSubmatchIndex(s, -1)
	if len(results) == 0 {
		return mrb.NilValue()
	}

	ret := make([][]interface{}, 0, len(results))
	var m *MatchData
	for _, result := range results {
		m = NewMatchData(r, s, result, 0)
		astr := m.Captures()

		if !block.IsNil() {
			matchData := mrb.NewInstance("MatchData", m)
			_ = mrb.IVSet(mrb.ClassOf(self).Real(), mrb.Intern("@last_match"), matchData)
			mrb.YieldArgv(block, astr...)
		} else {
			ret = append(ret, astr)
		}
	}

	if !block.IsNil() {
		return self
	}

	m = NewMatchData(r, s, results[len(results)-1], 0)
	matchData := mrb.NewInstance("MatchData", m)
	_ = mrb.IVSet(mrb.ClassOf(self).Real(), mrb.Intern("@last_match"), matchData)
	return mrb.Value(ret)
}
