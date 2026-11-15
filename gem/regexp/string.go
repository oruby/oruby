package regexp

import (
	"fmt"
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
	str.DefineMethod("sub", stringSub, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineMethod("sub!", stringSubBang, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineAlias("old_gsub", "gsub")
	str.DefineMethod("gsub", stringGsub, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineMethod("gsub!", stringGsubBang, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
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
		return mrb.Call(self, "old_split", pattern, limit)
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

	matches := re.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		result.PushString(s)
		return result
	}

	lastEnd := 0
	for _, m := range matches {
		if limited && (limit-result.Len() <= 1) {
			break
		}
		matchStart := m[0]
		matchEnd := m[1]

		result.PushString(s[lastEnd:matchStart])

		for i := 2; i < len(m); i += 2 {
			if m[i] >= 0 {
				result.PushString(s[m[i]:m[i+1]])
			} else {
				result.Push(mrb.NilValue())
			}
		}

		lastEnd = matchEnd
	}

	if lastEnd <= len(s) {
		result.PushString(s[lastEnd:])
	}

	for limit == 0 && result.Len() > 0 && result.Item(-1).Len() == 0 {
		result.Pop()
	}
	return result
}

func stringSlice(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	pattern := args.Item(0)
	r := getRegexp(mrb, pattern)
	if r == nil {
		if args.Len() == 1 {
			return mrb.Call(self, "old_slice", pattern)
		}
		return mrb.Call(self, "old_slice", args.Item(0), args.Item(1))
	}
	capture := args.ItemDef(1, mrb.NilValue())
	matchData := doMatch(mrb, r, mrb.String(self), 0)
	if matchData.IsNil() {
		return matchData
	}

	m := mrb.Data(matchData).(*MatchData)
	if capture.IsNil() {
		return strOrNil(mrb, m.StringOrNil(0))
	}

	if capture.Type() == oruby.MrbTTFixnum {
		return strOrNil(mrb, m.StringOrNil(capture.Int()))
	}
	return mrb.Value(m.NamedCaptures()[mrb.String(capture)])
}

func strOrNil(mrb *oruby.MrbState, s *string) oruby.MrbValue {
	if s == nil {
		return mrb.NilValue()
	}
	return mrb.StrNew(*s)
}

func interpolateBackrefs(replace string, m *MatchData, s string, start, end int) string {
	if !strings.Contains(replace, "\\") {
		return replace
	}
	wholeMatch, _ := m.toString(0)
	replace = strings.ReplaceAll(replace, "\\&", wholeMatch)
	replace = strings.ReplaceAll(replace, "\\0", wholeMatch)
	replace = strings.ReplaceAll(replace, "\\`", s[:start])
	replace = strings.ReplaceAll(replace, "\\'", s[end:])

	for i := 1; i < m.Size(); i++ {
		v, isNil := m.toString(i)
		if isNil {
			v = ""
		}
		replace = strings.ReplaceAll(replace, "\\"+strconv.Itoa(i), v)
	}
	for name, val := range m.NamedCaptures() {
		if name != "" && val != nil {
			replace = strings.ReplaceAll(replace, "\\k<"+name+">", fmt.Sprint(val))
		}
	}
	return replace
}

func doReplace(mrb *oruby.MrbState, re *regexp.Regexp, s string, pos int, f func(v string) (string, bool)) (string, int) {
	if pos > len(s) {
		return s, len(s) + 1
	}
	matchValue := doMatch(mrb, re, s, pos)
	if matchValue.IsNil() {
		return s, len(s) + 1
	}
	m := mrb.Data(matchValue).(*MatchData)
	start, _ := m.Begin(0)
	end, _ := m.End(0)
	if start < 0 || end < start || start > len(s) || end > len(s) {
		return s, len(s) + 1
	}

	capture := s[start:end]
	replace, lit := f(capture)
	if !lit {
		replace = interpolateBackrefs(replace, m, s, start, end)
	}

	ret := s[:start] + replace + s[end:]
	nextPos := start + len(replace)
	if end == start {
		nextPos++
	}
	return ret, nextPos
}

func doSub(mrb *oruby.MrbState, self oruby.Value) (oruby.MrbValue, bool) {
	args, block := mrb.GetArgsWithBlock()
	pattern := args.Item(0)
	replace := args.ItemDef(1, mrb.NilValue())
	replaceStr := ""
	r := getRegexp(mrb, pattern)
	if r == nil {
		ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("old_sub"), pattern, replace, block)
		return ret, false
	}
	if block.IsNil() && replace.IsNil() {
		return mrb.Call(self, "to_enum", mrb.Intern("sub"), pattern), false
	}
	if block.IsNil() && replace.Type() != oruby.MrbTTHash {
		replaceStr = mrb.EnsureStringType(replace).String()
	}

	orig := mrb.String(self)
	matched := false
	s, _ := doReplace(mrb, r, orig, 0, func(v string) (string, bool) {
		matched = true
		if !block.IsNil() {
			ret := mrb.Yield(block, mrb.StringValue(v))
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

	return mrb.StringValue(s), matched && s != orig
}

func stringSub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, _ := doSub(mrb, self)
	return res
}

func stringSubBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, modified := doSub(mrb, self)
	if !modified {
		return mrb.NilValue()
	}
	mrb.Call(self, "replace", res)
	return self
}

func doGsub(mrb *oruby.MrbState, self oruby.Value) (oruby.MrbValue, bool) {
	args, block := mrb.GetArgsWithBlock()
	pattern := args.Item(0)
	replace := args.ItemDef(1, mrb.NilValue())
	replaceStr := ""
	r := getRegexp(mrb, pattern)
	if r == nil {
		ret, _ := mrb.FuncallWithBlock(self, mrb.Intern("old_gsub"), pattern, replace, block)
		return ret, false
	}
	if block.IsNil() && replace.IsNil() {
		return mrb.Call(self, "to_enum", mrb.Intern("gsub"), pattern), false
	}
	if block.IsNil() && replace.Type() != oruby.MrbTTHash {
		replaceStr = mrb.EnsureStringType(replace).String()
	}

	orig := mrb.String(self)
	s := orig
	pos := 0
	matched := false
	for pos <= len(s) {
		var nextPos int
		var nextS string
		nextS, nextPos = doReplace(mrb, r, s, pos, func(v string) (string, bool) {
			matched = true
			if !block.IsNil() {
				ret := mrb.Yield(block, mrb.StringValue(v))
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
		s = nextS
		if nextPos <= pos || nextPos > len(s) {
			break
		}
		pos = nextPos
	}
	return mrb.StringValue(s), matched && s != orig
}

func stringGsub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, _ := doGsub(mrb, self)
	return res
}

func stringGsubBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, modified := doGsub(mrb, self)
	if !modified {
		return mrb.NilValue()
	}
	mrb.Call(self, "replace", res)
	return self
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
	case oruby.MrbTTCData:
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
		return mrb.ETypeError().Raisef("wrong argument type %v (expected Regexp)", mrb.ClassOf(arg).Name())
	}

	s := mrb.StringCstr(self)
	results := r.FindAllStringSubmatchIndex(s, -1)
	ret := mrb.AryNew()
	if len(results) == 0 {
		mrb.GVSet(mrb.Intern("$~"), mrb.NilValue())
		if !block.IsNil() {
			return self
		}
		return ret
	}

	groups := r.NumSubexp() > 0
	for _, result := range results {
		m := NewMatchData(r, s, result, 0)
		var item oruby.MrbValue
		if groups {
			caps := mrb.AryNew()
			for _, c := range m.Captures() {
				if c == nil {
					caps.Push(mrb.NilValue())
				} else {
					caps.Push(mrb.StrNew(c.(string)))
				}
			}
			item = caps
		} else {
			item = mrb.StrNew(s[result[0]:result[1]])
		}
		if !block.IsNil() {
			matchData := mrb.NewInstance("MatchData", m)
			_ = mrb.IVSet(mrb.ClassGet("Regexp").Value(), mrb.Intern("@last_match"), matchData)
			mrb.GVSet(mrb.Intern("$~"), matchData)
			if _, err := mrb.Funcall(block, mrb.Intern("call"), item); err != nil {
				return mrb.Exc().Value()
			}
		} else {
			ret.Push(item)
		}
	}

	m := NewMatchData(r, s, results[len(results)-1], 0)
	matchData := mrb.NewInstance("MatchData", m)
	_ = mrb.IVSet(mrb.ClassGet("Regexp").Value(), mrb.Intern("@last_match"), matchData)
	mrb.GVSet(mrb.Intern("$~"), matchData)
	if !block.IsNil() {
		return self
	}
	return ret
}
