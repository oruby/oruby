package regexp

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed string_ext.rb
var stringExtRb []byte

func initString(mrb *oruby.MrbState) {
	str := mrb.StringClass()
	for _, m := range []struct {
		name, old string
		f         oruby.MrbFuncT
	}{{"rindex", "old_rindex", stringRindex}, {"byteindex", "old_byteindex", stringByteindex},
		{"byterindex", "old_byterindex", stringByterindex}} {
		if mrb.RespondTo(mrb.StrNew(""), mrb.Intern(m.name)) {
			str.DefineAlias(m.old, m.name)
		}
		str.DefineMethod(m.name, m.f, mrb.ArgsArg(1, 1))
	}
	defer rb.LoadRuby(mrb, "string_ext.rb", stringExtRb)

	str.DefineMethod("=~", stringMatchEqual, mrb.ArgsReq(1))
	str.DefineMethod("match", stringMatch, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineMethod("match?", stringIsMatch, mrb.ArgsArg(1, 1))
	str.DefineAlias("old_index", "index")
	str.DefineMethod("index", stringIndex, mrb.ArgsArg(1, 1))
	str.DefineAlias("old_sub", "sub")
	str.DefineMethod("sub", stringSub, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineMethod("sub!", stringSubBang, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineAlias("old_gsub", "gsub")
	str.DefineMethod("gsub", stringGsub, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineMethod("gsub!", stringGsubBang, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	str.DefineMethod("scan", stringScan, mrb.ArgsReq(1)+mrb.ArgsBlock())
	str.DefineAlias("old_slice", "slice")
	str.DefineMethod("slice", stringSlice, mrb.ArgsArg(1, 1))
	str.DefineAlias("old_aref", "[]")
	str.DefineMethod("[]", stringAref, mrb.ArgsArg(1, 1))
	str.DefineAlias("old_split", "split")
	str.DefineMethod("split", stringSplit, mrb.ArgsOpt(2))
}

// literalCache keeps compiled patterns for String arguments of sub/gsub/scan,
// which match literally.
var literalCache sync.Map // string -> *Regexp

// getRegexp returns the pattern of a Regexp argument; a String argument
// matches literally (as in sub, gsub, scan, index, split).
func getRegexp(mrb *oruby.MrbState, value oruby.Value) *Regexp {
	switch value.Type() {
	case oruby.MrbTTCData:
		if reg, ok := mrb.Data(value).(*Regexp); ok {
			return reg
		}
	case oruby.MrbTTString:
		s := rstr(value)
		if r, ok := literalCache.Load(s); ok {
			return r.(*Regexp)
		}
		r, err := compileRuby(QuoteMeta(s), 0)
		if err != nil {
			return nil
		}
		literalCache.Store(s, r)
		return r
	}
	return nil
}

func strOrNil(mrb *oruby.MrbState, s *string) oruby.MrbValue {
	if s == nil {
		return mrb.NilValue()
	}
	return mrb.StrNew(*s)
}

func stringSplit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	pattern := args.ItemDefFunc(0, func() oruby.MrbValue { return mrb.GetGV("$;") })
	limit := args.ItemDefInt(1, 0)

	re, ok := mrb.Data(pattern.Value()).(*Regexp)
	if !ok {
		return mrb.Call(self, "old_split", pattern, limit)
	}
	result := mrb.AryNew()
	s := mrb.String(self)
	if len(s) == 0 {
		return result
	}
	if limit == 1 {
		result.PushString(s)
		return result
	}
	matches, err := re.findAll(s)
	if err != nil {
		return raiseTimeout(mrb, err)
	}

	last := 0
	for _, m := range matches {
		if limit > 0 && result.Len() >= limit-1 {
			break
		}
		// an empty match splits between characters, but never at either end
		// or directly after the previous separator
		if m[0] == m[1] && (m[0] == 0 || m[0] >= len(s) || m[0] == last) {
			continue
		}
		result.PushString(s[last:m[0]])
		for i := 2; i < len(m); i += 2 {
			if m[i] >= 0 {
				result.PushString(s[m[i]:m[i+1]])
			} else {
				result.Push(mrb.NilValue())
			}
		}
		last = m[1]
	}
	result.PushString(s[last:])

	if limit == 0 {
		for result.Len() > 0 && result.Item(-1).Len() == 0 {
			result.Pop()
		}
	}
	return result
}

func stringSlice(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return sliceWith(mrb, self, "old_slice")
}

func stringAref(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return sliceWith(mrb, self, "old_aref")
}

// sliceWith implements String#slice / String#[] for Regexp arguments and
// delegates other argument types to the original method
func sliceWith(mrb *oruby.MrbState, self oruby.Value, old string) oruby.MrbValue {
	args := mrb.GetArgs()
	pattern := args.Item(0)
	r, ok := mrb.Data(pattern).(*Regexp)
	if pattern.Type() != oruby.MrbTTCData || !ok {
		if args.Len() == 1 {
			return mrb.Call(self, old, pattern)
		}
		return mrb.Call(self, old, args.Item(0), args.Item(1))
	}
	capture := args.ItemDef(1, mrb.NilValue())
	matchData := doMatch(mrb, r, mrb.String(self), 0)
	if matchData.Value().IsNil() {
		return matchData
	}
	m := mrb.Data(matchData.Value()).(*MatchData)
	if capture.Value().IsNil() {
		return strOrNil(mrb, m.StringOrNil(0))
	}
	return matchDataGetIndex(mrb, m, capture.Value())
}

// expandReplacement expands \0 \& \1..\N \k<name> \` \' \\ in replace
func expandReplacement(replace string, m *MatchData) string {
	if !strings.Contains(replace, "\\") {
		return replace
	}
	var b strings.Builder
	for i := 0; i < len(replace); i++ {
		c := replace[i]
		if c != '\\' || i+1 >= len(replace) {
			b.WriteByte(c)
			continue
		}
		n := replace[i+1]
		switch {
		case n >= '0' && n <= '9':
			j := i + 1
			for j < len(replace) && replace[j] >= '0' && replace[j] <= '9' {
				j++
			}
			// use the longest group number that exists, as Ruby does
			digits := replace[i+1 : j]
			for len(digits) > 1 {
				if k, _ := strconv.Atoi(digits); k < m.Size() {
					break
				}
				digits = digits[:len(digits)-1]
			}
			k, _ := strconv.Atoi(digits)
			if v, isNil := m.toString(k); !isNil {
				b.WriteString(v)
			}
			i += len(digits)
		case n == '&':
			v, _ := m.toString(0)
			b.WriteString(v)
			i++
		case n == '`':
			b.WriteString(m.PreMatch())
			i++
		case n == '\'':
			b.WriteString(m.PostMatch())
			i++
		case n == '+':
			for g := m.Size() - 1; g > 0; g-- {
				if v, isNil := m.toString(g); !isNil {
					b.WriteString(v)
					break
				}
			}
			i++
		case n == '\\':
			b.WriteByte('\\')
			i++
		case n == 'k' && i+2 < len(replace) && replace[i+2] == '<':
			end := strings.IndexByte(replace[i+3:], '>')
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			name := replace[i+3 : i+3+end]
			for g, nm := range m.names {
				if g > 0 && nm == name {
					if v, isNil := m.toString(g); !isNil {
						b.WriteString(v)
					}
				}
			}
			i += 3 + end
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// replacer produces the replacement for one match, or an exception value
type replacer func(md oruby.Value, m *MatchData) (string, oruby.MrbValue)

func makeReplacer(mrb *oruby.MrbState, replace, block oruby.Value) replacer {
	switch {
	case !block.IsNil():
		return func(md oruby.Value, m *MatchData) (string, oruby.MrbValue) {
			setLastMatch(mrb, md)
			ret := mrb.Yield(block, mrb.StrNew(m.ToS()))
			if mrb.HasExc() {
				return "", ret
			}
			if ret.Type() != oruby.MrbTTString {
				ret = mrb.Call(ret, "to_s")
			}
			return mrb.String(ret), nil
		}
	case replace.Type() == oruby.MrbTTHash:
		return func(md oruby.Value, m *MatchData) (string, oruby.MrbValue) {
			v := mrb.Call(replace, "[]", mrb.StrNew(m.ToS()))
			if v.IsNil() {
				return "", nil
			}
			if v.Type() != oruby.MrbTTString {
				v = mrb.Call(v, "to_s")
			}
			return mrb.String(v), nil
		}
	default:
		str := mrb.String(mrb.EnsureStringType(replace).Value())
		return func(md oruby.Value, m *MatchData) (string, oruby.MrbValue) {
			return expandReplacement(str, m), nil
		}
	}
}

// substitute replaces the first (all=false) or every match of re in s
func substitute(mrb *oruby.MrbState, re *Regexp, s string, all bool, rep replacer) (string, bool, oruby.MrbValue) {
	var matches [][]int
	if all {
		var err error
		if matches, err = re.findAll(s); err != nil {
			return s, false, raiseTimeout(mrb, err)
		}
	} else if m, err := re.findErr(s, 0); err != nil {
		return s, false, raiseTimeout(mrb, err)
	} else if m != nil {
		matches = [][]int{m}
	}
	if len(matches) == 0 {
		setLastMatch(mrb, mrb.NilValue())
		return s, false, nil
	}
	var b strings.Builder
	last := 0
	var md oruby.Value
	for _, m := range matches {
		data := NewMatchData(re, s, m, 0)
		md = mrb.NewInstance("MatchData", data).Value()
		repl, exc := rep(md, data)
		if exc != nil {
			return s, false, exc
		}
		b.WriteString(s[last:m[0]])
		b.WriteString(repl)
		last = m[1]
	}
	b.WriteString(s[last:])
	setLastMatch(mrb, md)
	return b.String(), true, nil
}

func doSubst(mrb *oruby.MrbState, self oruby.Value, all bool, name string) (oruby.MrbValue, bool) {
	args, block := mrb.GetArgsWithBlock()
	pattern := args.Item(0)
	replace := args.ItemDef(1, mrb.NilValue()).Value()
	nargs := args.Len()
	if nargs == 1 {
		// gsub(re, "a" => "b"): mruby passes a brace-less hash as keywords
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && !mrb.Call(kw, "empty?").Bool() {
			replace, nargs = kw, 2
		}
	}
	r := getRegexp(mrb, pattern)
	if r == nil {
		old := "old_sub"
		if all {
			old = "old_gsub"
		}
		ret, _ := mrb.FuncallWithBlock(self, mrb.Intern(old), pattern, replace, block)
		return ret, false
	}
	if mrb.RespondTo(self, oruby.SymQ_valid_encoding) {
		if valid := mrb.Call(self, "valid_encoding?"); !valid.IsNil() && !valid.Bool() {
			enc := mrb.Call(self, "encoding")
			encName := "UTF-8"
			if !enc.IsNil() {
				if nameVal := mrb.Call(enc, "name"); !nameVal.IsNil() && nameVal.IsString() {
					encName = nameVal.String()
				} else {
					encName = mrb.String(enc)
				}
			}
			return mrb.Raise(mrb.EArgumentError(), "invalid byte sequence in "+encName), false
		}
	}
	if block.IsNil() && nargs < 2 {
		if all {
			return mrb.Call(self, "to_enum", mrb.Intern(name), pattern), false
		}
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 1, expected 2)"), false
	}
	blk := block.Value()
	if !block.IsNil() && nargs >= 2 {
		blk = mrb.NilValue()
	}
	s, matched, exc := substitute(mrb, r, mrb.String(self), all, makeReplacer(mrb, replace, blk))
	if exc != nil {
		return exc, false
	}
	return mrb.StringValue(s), matched
}

func stringSub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, _ := doSubst(mrb, self, false, "sub")
	return res
}

func stringSubBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, modified := doSubst(mrb, self, false, "sub!")
	if mrb.HasExc() {
		return res
	}
	if !modified {
		return mrb.NilValue()
	}
	mrb.Call(self, "replace", res)
	return self
}

func stringGsub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, _ := doSubst(mrb, self, true, "gsub")
	return res
}

func stringGsubBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, modified := doSubst(mrb, self, true, "gsub!")
	if mrb.HasExc() {
		return res
	}
	if !modified {
		if res.Value().Type() != oruby.MrbTTString {
			return res // enumerator
		}
		return mrb.NilValue()
	}
	mrb.Call(self, "replace", res)
	return self
}

// patternRegexp converts the argument of match/match?: a String is compiled
// as a regexp (not escaped)
func patternRegexp(mrb *oruby.MrbState, v oruby.Value) (*Regexp, oruby.Value, oruby.MrbValue) {
	if r, ok := mrb.Data(v).(*Regexp); ok && v.Type() == oruby.MrbTTCData {
		return r, v, nil
	}
	if v.Type() == oruby.MrbTTString {
		obj, err := mrb.ObjNew(mrb.ClassGet("Regexp"), v)
		if err != nil {
			return nil, oruby.Nil, mrb.RaiseError(err)
		}
		return mrb.Data(obj.Value()).(*Regexp), obj.Value(), nil
	}
	return nil, oruby.Nil, mrb.Raise(mrb.ETypeError(), "wrong argument type "+mrb.ClassOf(v).Name()+" (expected Regexp)")
}

func stringIsMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	re, _, exc := patternRegexp(mrb, args.Item(0))
	if exc != nil {
		return exc
	}
	s := mrb.String(self)
	pos, ok := byteIndex(s, args.ItemDef(1, mrb.FixnumValue(0)).Int())
	if !ok {
		return oruby.False
	}
	m, err := re.findErr(s, pos)
	if err != nil {
		return raiseTimeout(mrb, err)
	}
	return mrb.BoolValue(m != nil)
}

func stringMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	re, reValue, exc := patternRegexp(mrb, args.Item(0))
	if exc != nil {
		return exc
	}
	s := mrb.String(self)
	pos, ok := byteIndex(s, args.ItemDef(1, mrb.FixnumValue(0)).Int())
	if !ok {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	md := doMatch(mrb, re, s, pos)
	if !md.Value().IsNil() && md.Value().Type() == oruby.MrbTTCData {
		_ = mrb.IVSet(md.Value(), oruby.IvSym_regexp, reValue)
		if !block.IsNil() {
			return mrb.Yield(block, md)
		}
	}
	return md
}

func stringMatchEqual(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	re, ok := mrb.Data(arg).(*Regexp)
	if arg.Type() != oruby.MrbTTCData || !ok {
		if arg.Type() == oruby.MrbTTString {
			return mrb.Raise(mrb.ETypeError(), "wrong argument type String (expected Regexp)")
		}
		return mrb.Call(arg, "=~", self)
	}
	s := mrb.String(self)
	md := doMatch(mrb, re, s, 0)
	if md.Value().IsNil() || md.Value().Type() != oruby.MrbTTCData {
		return md
	}
	data := mrb.Data(md.Value()).(*MatchData)
	idx := data.byteBegin(0)
	return mrb.FixnumValue(charIndex(s, idx))
}

func stringIndex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	arg := args.Item(0)
	re, ok := mrb.Data(arg).(*Regexp)
	if arg.Type() != oruby.MrbTTCData || !ok {
		if args.Len() > 1 {
			return mrb.Call(self, "old_index", arg, args.Item(1))
		}
		return mrb.Call(self, "old_index", arg)
	}
	s := rstr(self)
	pos, ok := byteIndex(s, oruby.MrbFixnum(args.ItemDef(1, mrb.FixnumValue(0))))
	if !ok {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	md := doMatch(mrb, re, s, pos)
	if md.Value().IsNil() || md.Value().Type() != oruby.MrbTTCData {
		return md
	}
	data := mrb.Data(md.Value()).(*MatchData)
	idx := data.byteBegin(0)
	return mrb.FixnumValue(charIndex(s, idx))
}

func stringScan(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	arg := args.Item(0)
	r := getRegexp(mrb, arg)
	if r == nil {
		return mrb.ETypeError().Raisef("wrong argument type %v (expected Regexp)", mrb.ClassOf(arg).Name())
	}

	s := rstr(self)
	results, err := r.findAll(s)
	if err != nil {
		return raiseTimeout(mrb, err)
	}
	ret := mrb.AryNew()
	if len(results) == 0 {
		setLastMatch(mrb, mrb.NilValue())
		if !block.IsNil() {
			return self
		}
		return ret
	}

	groups := r.numSubexp() > 0
	var md oruby.Value
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
		md = mrb.NewInstance("MatchData", m).Value()
		if !block.IsNil() {
			setLastMatch(mrb, md)
			v := mrb.Yield(block, item)
			if mrb.HasExc() {
				return v
			}
		} else {
			ret.Push(item)
		}
	}

	setLastMatch(mrb, md)
	if !block.IsNil() {
		return self
	}
	return ret
}

// stringRindex is String#rindex(regexp, pos): the last match starting at or
// before character pos
func stringRindex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return reverseSearch(mrb, self, "old_rindex", false)
}

// stringByterindex is String#byterindex(regexp, pos) with byte offsets
func stringByterindex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return reverseSearch(mrb, self, "old_byterindex", true)
}

func reverseSearch(mrb *oruby.MrbState, self oruby.Value, old string, bytes bool) oruby.MrbValue {
	args := mrb.GetArgs()
	arg := args.Item(0)
	re, ok := mrb.Data(arg).(*Regexp)
	if arg.Type() != oruby.MrbTTCData || !ok {
		if args.Len() > 1 {
			return mrb.Call(self, old, arg, args.Item(1))
		}
		return mrb.Call(self, old, arg)
	}
	s := rstr(self)
	pos := len(s)
	if args.Len() > 1 {
		n := oruby.MrbFixnum(args.Item(1))
		if bytes {
			if n < 0 {
				n += len(s)
			}
			if n < 0 {
				setLastMatch(mrb, mrb.NilValue())
				return mrb.NilValue()
			}
			if n < len(s) {
				pos = n
			}
		} else {
			b, ok := byteIndex(s, n)
			if !ok {
				if n < 0 {
					setLastMatch(mrb, mrb.NilValue())
					return mrb.NilValue()
				}
				b = len(s)
			}
			pos = b
		}
	}
	for p := pos; p >= 0; {
		m, err := re.findErr(s, p)
		if err != nil {
			return raiseTimeout(mrb, err)
		}
		if m != nil && m[0] == p {
			md := newMatchValue(mrb, re, s, m)
			setLastMatch(mrb, md)
			if bytes {
				return mrb.FixnumValue(p)
			}
			return mrb.FixnumValue(charIndex(s, p))
		}
		if p == 0 {
			break
		}
		_, w := utf8.DecodeLastRuneInString(s[:p])
		p -= w
	}
	setLastMatch(mrb, mrb.NilValue())
	return mrb.NilValue()
}

// stringByteindex is String#byteindex(regexp, pos) with byte offsets
func stringByteindex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	arg := args.Item(0)
	re, ok := mrb.Data(arg).(*Regexp)
	if arg.Type() != oruby.MrbTTCData || !ok {
		if args.Len() > 1 {
			return mrb.Call(self, "old_byteindex", arg, args.Item(1))
		}
		return mrb.Call(self, "old_byteindex", arg)
	}
	s := rstr(self)
	pos := 0
	if args.Len() > 1 {
		pos = oruby.MrbFixnum(args.Item(1))
		if pos < 0 {
			pos += len(s)
		}
	}
	if pos < 0 || pos > len(s) {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	if pos < len(s) && !utf8.RuneStart(s[pos]) {
		return mrb.Raise(mrb.EIndexError(), "offset "+strconv.Itoa(pos)+" does not land on character boundary")
	}
	md := doMatch(mrb, re, s, pos)
	if md.Value().IsNil() || md.Value().Type() != oruby.MrbTTCData {
		return md
	}
	return mrb.FixnumValue(mrb.Data(md.Value()).(*MatchData).byteBegin(0))
}
