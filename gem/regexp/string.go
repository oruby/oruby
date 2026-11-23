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
	// core String methods: <internal:> frames pass $~ through to the caller
	defer rb.LoadRuby(mrb, "<internal:regexp/string_ext>", stringExtRb)

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
	str.DefineMethod("split", stringSplit, mrb.ArgsOpt(2)+mrb.ArgsBlock())
}

// literalCache keeps compiled patterns for String arguments of sub/gsub/scan,
// which match literally.
type literalKey struct{ source, encoding string }

var literalCache sync.Map // literalKey -> *Regexp

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
		key := literalKey{source: s}
		if !isASCII(s) {
			key.encoding = regexpSubjectEncoding(mrb, value)
		}
		if r, ok := literalCache.Load(key); ok {
			return r.(*Regexp)
		}
		normalized := newRegexpText(s, key.encoding).text
		r, err := compileRuby(QuoteMeta(normalized), 0)
		if err != nil {
			return nil
		}
		if normalized != s {
			r.pattern, r.source, r.enc = r.source, QuoteMeta(s), key.encoding
		}
		literalCache.Store(key, r)
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

// stringSplit is String#split, a port of MRI's rb_str_split_m: awk mode
// (nil or " ": runs of ASCII whitespace, leading ones skipped), chars mode
// (""), a String or a Regexp; limit > 0 caps the pieces, limit 0 drops
// trailing empty strings, a negative one keeps them; with a block each
// piece is yielded and self returned. The pieces are Strings (not of a
// subclass) in self's encoding.
func stringSplit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	if args.Len() > 2 {
		return mrb.ArgnumError(args.Len(), 0, 2)
	}
	pattern := args.Item(0)

	lim := 0
	hasLimit := false
	if args.Len() == 2 {
		n, err := splitLimit(mrb, args.Item(1))
		if err != nil {
			return mrb.RaiseError(err)
		}
		lim = n
		hasLimit = lim > 0
	}

	// self's encoding, for the pieces (UTF-8 strings need nothing)
	var enc oruby.MrbValue
	isUTF8 := true
	if bin, err := mrb.Funcall(self, mrb.Intern("__encoding_binary?")); err == nil && bin.Bool() {
		if e, err := mrb.Funcall(self, oruby.Sym_encoding); err == nil {
			enc, isUTF8 = e, false
		}
	}
	s := mrb.String(self)
	withBlock := !block.IsNil()
	result := mrb.AryNew()

	emit := func(piece string) {
		v := mrb.StrNew(piece)
		if !isUTF8 {
			_, _ = mrb.Funcall(v, oruby.Sym_force_encoding, enc)
		}
		if withBlock {
			mrb.Yield(block, v)
		} else {
			result.Push(v)
		}
	}
	done := func() oruby.MrbValue {
		if withBlock {
			return self
		}
		return result
	}

	if hasLimit && lim == 1 {
		if len(s) > 0 {
			emit(s)
		}
		return done()
	}

	// MRI's split_string: with limit 0 empty pieces wait for a non-empty
	// one (trailing ones are dropped)
	emptyCount := -1
	if !hasLimit && lim == 0 {
		emptyCount = 0
	}
	push := func(beg, n int) {
		if emptyCount >= 0 && n == 0 {
			emptyCount++
			return
		}
		for ; emptyCount > 0; emptyCount-- {
			emit("")
		}
		emit(s[beg : beg+n])
	}
	brokenErr := func() oruby.MrbValue {
		name := "UTF-8"
		if !isUTF8 {
			name = mrb.String(enc)
		}
		return mrb.EArgumentError().Raise("invalid byte sequence in " + name)
	}
	valid := func(str oruby.Value, utf8Str bool) bool {
		if utf8Str {
			return utf8.ValidString(mrb.String(str))
		}
		v, err := mrb.Funcall(str, oruby.SymQ_valid_encoding)
		return err != nil || v.Bool()
	}
	charLen := func(i int) int {
		if !isUTF8 {
			return 1
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		return n
	}

	// the pattern: $; when none is given
	if pattern.IsNil() {
		if fs := mrb.GetGV("$;"); !fs.IsNil() {
			// MRI: a deprecation warning, through Kernel#warn
			if mrb.ObjectClass().ConstDefinedAt("Warning") {
				w := mrb.ObjectClass().ConstGet("Warning")
				if on, err := mrb.Funcall(w, oruby.OpSym_aref, mrb.SymbolValue(mrb.Intern("deprecated"))); err == nil && on.Bool() {
					_, _ = mrb.Funcall(mrb.KernelModule().Value(), mrb.Intern("warn"), "warning: $; is set to non-nil value")
				}
			}
			pattern = fs
		}
	}
	var re *Regexp
	mode := "awk"
	spat := ""
	if !pattern.IsNil() {
		if r, ok := mrb.Data(pattern).(*Regexp); ok && pattern.Type() == oruby.MrbTTCData {
			re, mode = r, "regexp"
		} else {
			if !pattern.IsString() {
				if !mrb.RespondTo(pattern, oruby.Sym_to_str) {
					return mrb.ETypeError().Raisef("wrong argument type %s (expected Regexp)", mrb.ClassName(mrb.ObjClass(pattern)))
				}
				p, err := mrb.Funcall(pattern, oruby.Sym_to_str)
				if err != nil {
					return mrb.RaiseError(err)
				}
				pattern = p.Value()
			}
			spat = mrb.String(pattern)
			switch spat {
			case " ":
				mode = "awk"
			case "":
				mode = "chars"
			default:
				mode = "string"
			}
		}
	}
	if mode != "awk" && !valid(self, isUTF8) {
		return brokenErr()
	}

	beg, i := 0, 1
	switch mode {
	case "awk":
		if !valid(self, isUTF8) {
			return brokenErr()
		}
		skip := true
		end := 0
		for p := 0; p < len(s); {
			c := s[p]
			n := 1
			if c >= 0x80 {
				n = charLen(p)
			}
			p += n
			space := n == 1 && (c == ' ' || (c >= '\t' && c <= '\r'))
			if skip {
				if space {
					beg = p
				} else {
					end = p
					skip = false
					if hasLimit && lim <= i {
						break
					}
				}
			} else if space {
				push(beg, end-beg)
				skip = true
				beg = p
				if hasLimit {
					i++
				}
			} else {
				end = p
			}
		}
	case "chars":
		for beg < len(s) {
			n := charLen(beg)
			push(beg, n)
			beg += n
			if hasLimit {
				i++
				if lim <= i {
					break
				}
			}
		}
	case "string":
		if pv := pattern; !valid(pv, isUTF8) || (isUTF8 && !utf8.ValidString(spat)) {
			return brokenErr()
		}
		p := 0
		for p < len(s) {
			k := strings.Index(s[p:], spat)
			if k < 0 {
				break
			}
			at := p + k
			// a match inside a character does not count
			if isUTF8 && at < len(s) && !utf8.RuneStart(s[at]) {
				p = at + 1
				continue
			}
			push(beg, at-beg)
			p = at + len(spat)
			beg = p
			if hasLimit {
				i++
				if lim <= i {
					break
				}
			}
		}
	case "regexp":
		start := 0
		lastNull := false
		enc := regexpSubjectEncoding(mrb, self)
		input := newRegexpText(s, enc)
		deadline := re.deadline()
		for {
			if start > len(s) {
				break
			}
			m, err := re.findText(input, start, deadline)
			if err != nil {
				return raiseTimeout(mrb, err)
			}
			if m == nil {
				break
			}
			end := m[0]
			if start == end && m[0] == m[1] {
				if len(s) == 0 {
					emit("")
					break
				} else if lastNull {
					push(beg, start-beg)
					beg = start
				} else {
					if start == len(s) {
						start++
					} else {
						if legacyRegexpEncoding(enc) {
							start += encodedWidth(s[start:], enc)
						} else {
							start += charLen(start)
						}
					}
					lastNull = true
					continue
				}
			} else {
				push(beg, end-beg)
				beg = m[1]
				start = m[1]
			}
			lastNull = false
			for g := 2; g+1 < len(m); g += 2 {
				if m[g] < 0 {
					continue // a group that did not take part
				}
				push(m[g], m[g+1]-m[g])
			}
			if hasLimit {
				i++
				if lim <= i {
					break
				}
			}
			if start > len(s) {
				break
			}
		}
	}
	if len(s) > 0 && (hasLimit || len(s) > beg || lim < 0) {
		if beg > len(s) {
			beg = len(s)
		}
		push(beg, len(s)-beg)
	}
	return done()
}

// splitLimit is NUM2INT of the limit
func splitLimit(mrb *oruby.MrbState, v oruby.Value) (int, error) {
	var n int64
	switch {
	case v.IsInteger():
		n = v.Int64()
	case v.Type() == oruby.MrbTTFloat:
		n = int64(v.Float64())
	case v.IsNil():
		return 0, oruby.ETypeError("no implicit conversion from nil to integer")
	case mrb.RespondTo(v, oruby.Sym_to_int):
		r, err := mrb.Funcall(v, oruby.Sym_to_int)
		if err != nil {
			return 0, err
		}
		if !r.IsInteger() {
			return 0, oruby.ETypeError("can't convert %s to Integer", mrb.ClassName(mrb.ObjClass(v)))
		}
		n = r.Int64()
	default:
		return 0, oruby.ETypeError("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ObjClass(v)))
	}
	if v.Type() == oruby.MrbTTBigInt || n > 2147483647 || n < -2147483648 {
		return 0, oruby.ERangeError("integer %d too big to convert to 'int'", n)
	}
	return int(n), nil
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
	hasCap := args.Len() > 1
	capture := args.ItemDef(1, mrb.NilValue())
	if hasCap {
		switch capture.Type() {
		case oruby.MrbTTFixnum, oruby.MrbTTString, oruby.MrbTTSymbol:
		default:
			if !capture.IsNil() && mrb.RespondTo(capture, oruby.Sym_to_int) {
				toI, err := mrb.Funcall(capture, oruby.Sym_to_int)
				if err != nil {
					return mrb.RaiseError(err)
				}
				if toI.Type() != oruby.MrbTTFixnum {
					cname := mrb.ClassName(mrb.ObjClass(capture))
					return mrb.ETypeError().Raisef("can't convert %s into Integer (%s#to_int gives %s)", cname, cname, mrb.ClassName(mrb.ObjClass(toI.Value())))
				}
				capture = toI.Value()
			} else {
				return mrb.ETypeError().Raisef("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ObjClass(capture)))
			}
		}
	}
	matchData := doMatch(mrb, r, mrb.String(self), 0, regexpSubjectEncoding(mrb, self))
	if matchData.Value().Type() != oruby.MrbTTCData {
		return matchData
	}
	m := mrb.Data(matchData.Value()).(*MatchData)
	m.enc = encName(mrb, strEnc(mrb, self))
	if !hasCap {
		return mdGroup(mrb, m, 0)
	}
	if capture.Type() == oruby.MrbTTFixnum {
		i := oruby.MrbFixnum(capture)
		if i < 0 && i+m.Size() <= 0 {
			return mrb.NilValue()
		}
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
			// one digit, as MRI's rb_reg_regsub (\10 is \1 then "0"); with
			// named groups the numbered ones are not captures
			k := int(n - '0')
			if k == 0 || !m.hasNames() {
				if v, isNil := m.toString(k); !isNil {
					b.WriteString(v)
				}
			}
			i++
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

// replacer produces the replacement for one match and its encoding (Nil
// for UTF-8), or an exception value
type replacer func(md oruby.Value, m *MatchData) (string, oruby.Value, oruby.MrbValue)

// strEnc is v's Encoding, Nil for UTF-8 (the common case, without a call)
func strEnc(mrb *oruby.MrbState, v oruby.Value) oruby.Value {
	if v.Type() == oruby.MrbTTString && !oruby.StrBinary(v) {
		return oruby.Nil // UTF-8 (or US-ASCII, as compatible here)
	}
	if bin, err := mrb.Funcall(v, oruby.SymQ___encoding_binary); err == nil && bin.Bool() {
		if e, err := mrb.Funcall(v, oruby.Sym_encoding); err == nil {
			return e.Value()
		}
	}
	return oruby.Nil
}

// encBuf assembles a substitution as MRI's rb_enc_cr_str_buf_cat: an
// ASCII-only piece keeps the encoding, a piece in another encoding turns an
// ASCII-only result into that one and is an Encoding::CompatibilityError
// otherwise
type encBuf struct {
	b     strings.Builder
	enc   oruby.Value // Nil: UTF-8
	ascii bool
}

func sameEnc(a, b oruby.Value) bool {
	if a.IsNil() || b.IsNil() {
		return a.IsNil() == b.IsNil()
	}
	return a.Pointer() == b.Pointer()
}

func encName(mrb *oruby.MrbState, e oruby.Value) string {
	if e.IsNil() {
		return "UTF-8"
	}
	name := mrb.String(mrb.Call(e, "to_s"))
	if name == "BINARY" {
		return "ASCII-8BIT"
	}
	return name
}

func (e *encBuf) add(mrb *oruby.MrbState, s string, enc oruby.Value) oruby.MrbValue {
	if len(s) == 0 {
		return nil
	}
	sa := isASCII(s)
	if !sa && !sameEnc(e.enc, enc) {
		if !e.ascii {
			return mrb.Raisef(mrb.ClassGetUnder(mrb.EncodingClass(), "CompatibilityError"),
				"incompatible character encodings: %s and %s", encName(mrb, e.enc), encName(mrb, enc))
		}
		e.enc = enc
	}
	e.ascii = e.ascii && sa
	e.b.WriteString(s)
	return nil
}

// implicitStr is MRI's rb_str_to_str: a String, or #to_str's String
func implicitStr(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, oruby.MrbValue) {
	if v.Type() == oruby.MrbTTString {
		return v, nil
	}
	cname := mrb.ClassName(mrb.ObjClass(v))
	if v.IsNil() {
		cname = "nil"
	}
	if !mrb.RespondTo(v, oruby.Sym_to_str) {
		return oruby.Nil, mrb.ETypeError().Raisef("no implicit conversion of %s into String", cname)
	}
	r, err := mrb.Funcall(v, oruby.Sym_to_str)
	if err != nil {
		return oruby.Nil, mrb.RaiseError(err)
	}
	if r.Type() != oruby.MrbTTString {
		return oruby.Nil, mrb.ETypeError().Raisef("can't convert %s into String (%s#to_str gives %s)", cname, cname, mrb.ClassName(mrb.ObjClass(r.Value())))
	}
	return r.Value(), nil
}

// valueEnc: a non-ASCII replacement's encoding
func valueEnc(mrb *oruby.MrbState, v oruby.Value, s string) oruby.Value {
	if isASCII(s) {
		return oruby.Nil
	}
	return strEnc(mrb, v)
}

// needsMatch: the replacer looks at each match's MatchData (a block sees $~)
func needsMatch(replace, block oruby.Value) bool {
	return !block.IsNil() || replace.Type() == oruby.MrbTTHash
}

func makeReplacer(mrb *oruby.MrbState, self, replace, block, selfEnc oruby.Value) (replacer, oruby.MrbValue) {
	switch {
	case !block.IsNil():
		size := mrb.Call(self, "bytesize").Int()
		return func(md oruby.Value, m *MatchData) (string, oruby.Value, oruby.MrbValue) {
			setLastMatch(mrb, md)
			ret := mrb.Yield(block, mrb.StrNew(m.ToS()))
			if mrb.HasExc() {
				return "", oruby.Nil, ret
			}
			// MRI's str_mod_check: the block may not resize self
			if mrb.Call(self, "bytesize").Int() != size {
				return "", oruby.Nil, mrb.ERuntimeError().Raisef("string modified")
			}
			if ret.Type() != oruby.MrbTTString {
				ret = mrb.Call(ret, "to_s")
			}
			str := mrb.String(ret)
			return str, valueEnc(mrb, ret, str), nil
		}, nil
	case replace.Type() == oruby.MrbTTHash:
		return func(md oruby.Value, m *MatchData) (string, oruby.Value, oruby.MrbValue) {
			v := mrb.Call(replace, "[]", mrb.StrNew(m.ToS()))
			if v.IsNil() {
				return "", oruby.Nil, nil
			}
			if v.Type() != oruby.MrbTTString {
				v = mrb.Call(v, "to_s")
			}
			str := mrb.String(v)
			return str, valueEnc(mrb, v, str), nil
		}, nil
	default:
		rs, exc := implicitStr(mrb, replace)
		if exc != nil {
			return nil, exc
		}
		str := mrb.String(rs)
		// an ASCII-only template's non-ASCII comes from self's captures
		enc := selfEnc
		if !isASCII(str) {
			enc = strEnc(mrb, rs)
		}
		return func(md oruby.Value, m *MatchData) (string, oruby.Value, oruby.MrbValue) {
			return expandReplacement(str, m), enc, nil
		}, nil
	}
}

// substitute replaces the first (all=false) or every match of re in s
func substitute(mrb *oruby.MrbState, re *Regexp, s string, all bool, rep replacer, selfEnc oruby.Value, eachMatch bool) (oruby.MrbValue, bool, oruby.MrbValue) {
	var matches [][]int
	enc := encName(mrb, selfEnc)
	if all {
		var err error
		if matches, err = re.findAll(s, enc); err != nil {
			return nil, false, raiseTimeout(mrb, err)
		}
	} else if m, err := re.findErr(s, 0, enc); err != nil {
		return nil, false, raiseTimeout(mrb, err)
	} else if m != nil {
		matches = [][]int{m}
	}
	b := encBuf{enc: selfEnc, ascii: true}
	if len(matches) == 0 {
		setLastMatch(mrb, mrb.NilValue())
		b.add(mrb, s, selfEnc)
		return b.result(mrb), false, nil
	}
	last := 0
	md := oruby.Nil
	var data *MatchData
	for _, m := range matches {
		data = NewMatchData(re, s, m, 0)
		data.enc = enc
		// a MatchData object for each match only when the replacement sees
		// it; $~ is the last one
		if eachMatch {
			md = mrb.DataWrapInterface(mrb.ClassGetID(oruby.Sym_MatchData), data).Value()
		}
		repl, enc, exc := rep(md, data)
		if exc != nil {
			return nil, false, exc
		}
		if exc := b.add(mrb, s[last:m[0]], selfEnc); exc != nil {
			return nil, false, exc
		}
		if exc := b.add(mrb, repl, enc); exc != nil {
			return nil, false, exc
		}
		last = m[1]
	}
	if exc := b.add(mrb, s[last:], selfEnc); exc != nil {
		return nil, false, exc
	}
	if !eachMatch {
		md = mrb.DataWrapInterface(mrb.ClassGetID(oruby.Sym_MatchData), data).Value()
	}
	setLastMatch(mrb, md)
	return b.result(mrb), true, nil
}

// result is the String, in the encoding reached
func (e *encBuf) result(mrb *oruby.MrbState) oruby.MrbValue {
	v := mrb.StrNew(e.b.String())
	if !e.enc.IsNil() {
		_, _ = mrb.Funcall(v, oruby.Sym_force_encoding, e.enc)
	}
	return v
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
	if r == nil && !pattern.IsString() {
		// MRI's get_pat_quoted: a pattern other than a Regexp is a String
		// by #to_str
		if !mrb.RespondTo(pattern, oruby.Sym_to_str) {
			return mrb.ETypeError().Raisef("wrong argument type %s (expected Regexp)", mrb.ClassName(mrb.ObjClass(pattern))), false
		}
		p, err := mrb.Funcall(pattern, oruby.Sym_to_str)
		if err != nil {
			return mrb.RaiseError(err), false
		}
		if p.Type() != oruby.MrbTTString {
			return mrb.ETypeError().Raisef("wrong argument type %s (expected Regexp)", mrb.ClassName(mrb.ObjClass(pattern))), false
		}
		pattern = p.Value()
		r = getRegexp(mrb, pattern)
	}
	if r == nil {
		old := "old_sub"
		if all {
			old = "old_gsub"
		}
		ret, _ := mrb.FuncallWithBlock(self, mrb.Intern(old), pattern, replace, block)
		return ret, false
	}
	if oruby.StrBinary(self) && mrb.RespondTo(self, oruby.SymQ_valid_encoding) || !oruby.StrBinary(self) && !utf8.ValidString(rstr(self)) {
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
			// an enumerator without a size, as MRI
			e := mrb.Call(self, "to_enum", mrb.Intern(name), pattern)
			// a size block that answers nil (a nil one means "none set")
			_ = mrb.IVSet(e, mrb.Intern("@size_block"), mrb.ProcNewCFunc(func(*oruby.MrbState, oruby.Value) oruby.MrbValue { return oruby.Nil }).Value())
			return e, false
		}
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 1, expected 2)"), false
	}
	blk := block.Value()
	if !block.IsNil() && nargs >= 2 {
		blk = mrb.NilValue()
	}
	selfEnc := strEnc(mrb, self)
	rep, exc := makeReplacer(mrb, self, replace, blk, selfEnc)
	if exc != nil {
		return exc, false
	}
	res, matched, exc := substitute(mrb, r, mrb.String(self), all, rep, selfEnc, needsMatch(replace, blk))
	if exc != nil {
		return exc, false
	}
	return res, matched
}

func stringSub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, _ := doSubst(mrb, self, false, "sub")
	return res
}

func stringSubBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := mrb.CheckFrozen(self); err != nil {
		return mrb.RaiseError(err)
	}
	res, modified := doSubst(mrb, self, false, "sub!")
	if mrb.HasExc() {
		return res
	}
	if !modified {
		return mrb.NilValue()
	}
	mrb.Call(self, "replace", res)
	// replace keeps mruby's binary flag, not the encoding
	if enc := strEnc(mrb, res.Value()); !sameEnc(enc, strEnc(mrb, self)) || !enc.IsNil() {
		if enc.IsNil() {
			enc = mrb.Call(mrb.EncodingClass().Value(), "find", mrb.StrNew("UTF-8")).Value()
		}
		_, _ = mrb.Funcall(self, oruby.Sym_force_encoding, enc)
	}
	return self
}

func stringGsub(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, _ := doSubst(mrb, self, true, "gsub")
	return res
}

func stringGsubBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := mrb.CheckFrozen(self); err != nil {
		return mrb.RaiseError(err)
	}
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
	// replace keeps mruby's binary flag, not the encoding
	if enc := strEnc(mrb, res.Value()); !sameEnc(enc, strEnc(mrb, self)) || !enc.IsNil() {
		if enc.IsNil() {
			enc = mrb.Call(mrb.EncodingClass().Value(), "find", mrb.StrNew("UTF-8")).Value()
		}
		_, _ = mrb.Funcall(self, oruby.Sym_force_encoding, enc)
	}
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
	return nil, oruby.Nil, mrb.ETypeError().Raisef("wrong argument type %s (expected Regexp)", mrb.ClassName(mrb.ObjClass(v)))
}

func stringIsMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	re, reValue, exc := patternRegexp(mrb, args.Item(0))
	if exc != nil {
		return exc
	}
	if !matchCheck(mrb, reValue, self) {
		return mrb.Exc().Value()
	}
	s := mrb.String(self)
	enc := regexpSubjectEncoding(mrb, self)
	pos, ok := regexpByteIndex(s, args.ItemDef(1, mrb.FixnumValue(0)).Int(), enc)
	if !ok {
		return oruby.False
	}
	m, err := re.findErr(s, pos, enc)
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
	// a Regexp subclass: its own #match (rb_str_match_m calls it)
	if args.Item(0).Type() == oruby.MrbTTCData && mrb.ObjClass(reValue).Value().Pointer() != mrb.ClassGet("Regexp").Value().Pointer() {
		vals := []interface{}{self}
		if args.Len() > 1 {
			vals = append(vals, args.Item(1))
		}
		if !block.IsNil() {
			vals = append(vals, block)
		}
		v, err := mrb.FuncallWithBlock(reValue, mrb.Intern("match"), vals...)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return v
	}
	if !matchCheck(mrb, reValue, self) {
		return mrb.Exc().Value()
	}
	s := mrb.String(self)
	enc := regexpSubjectEncoding(mrb, self)
	pos, ok := regexpByteIndex(s, args.ItemDef(1, mrb.FixnumValue(0)).Int(), enc)
	if !ok {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	md := doMatch(mrb, re, s, pos, enc)
	if !md.Value().IsNil() && md.Value().Type() == oruby.MrbTTCData {
		_ = mrb.IVSet(md.Value(), oruby.IvSym_regexp, reValue)
		if m, ok := mrb.Data(md.Value()).(*MatchData); ok {
			m.enc = encName(mrb, strEnc(mrb, self))
		}
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
	if !matchCheck(mrb, arg, self) {
		return mrb.Exc().Value()
	}
	s := mrb.String(self)
	md := doMatch(mrb, re, s, 0, regexpSubjectEncoding(mrb, self))
	if md.Value().IsNil() || md.Value().Type() != oruby.MrbTTCData {
		return md
	}
	data := mrb.Data(md.Value()).(*MatchData)
	data.enc = encName(mrb, strEnc(mrb, self))
	idx := data.byteBegin(0)
	return mrb.FixnumValue(regexpCharIndex(s, idx, data.enc))
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
	if !matchCheck(mrb, arg, self) {
		return mrb.Exc().Value()
	}
	s := rstr(self)
	enc := regexpSubjectEncoding(mrb, self)
	pos, ok := regexpByteIndex(s, oruby.MrbFixnum(args.ItemDef(1, mrb.FixnumValue(0))), enc)
	if !ok {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	md := doMatch(mrb, re, s, pos, enc)
	if md.Value().IsNil() || md.Value().Type() != oruby.MrbTTCData {
		return md
	}
	data := mrb.Data(md.Value()).(*MatchData)
	data.enc = encName(mrb, strEnc(mrb, self))
	idx := data.byteBegin(0)
	return mrb.FixnumValue(regexpCharIndex(s, idx, data.enc))
}

func stringScan(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	arg := args.Item(0)
	r := getRegexp(mrb, arg)
	if r == nil {
		return mrb.ETypeError().Raisef("wrong argument type %s (expected Regexp)", mrb.ClassName(mrb.ObjClass(arg)))
	}

	s := rstr(self)
	results, err := r.findAll(s, regexpSubjectEncoding(mrb, self))
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
	enc := encName(mrb, strEnc(mrb, self))
	for _, result := range results {
		m := NewMatchData(r, s, result, 0)
		m.enc = enc
		var item oruby.MrbValue
		if groups {
			caps := mrb.AryNew()
			for _, c := range m.Captures() {
				if c == nil {
					caps.Push(mrb.NilValue())
				} else {
					caps.Push(mdStr(mrb, m, c.(string)))
				}
			}
			item = caps
		} else {
			item = mdStr(mrb, m, s[result[0]:result[1]])
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
	if !matchCheck(mrb, arg, self) {
		return mrb.Exc().Value()
	}
	s := rstr(self)
	enc := regexpSubjectEncoding(mrb, self)
	input := newRegexpText(s, enc)
	pos := len(s)
	if args.Len() > 1 {
		if args.Item(1).IsNil() {
			return mrb.ETypeError().Raisef("no implicit conversion from nil to integer")
		}
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
			b, ok := regexpByteIndex(s, n, enc)
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
	deadline := re.deadline()
	find := func(pos int) ([]int, error) { return re.findText(input, pos, deadline) }
	// Each candidate moves backwards, while \G stays at the given offset.
	if engine, ok := re.engine.(interface {
		WithSearchOrigin(string, int) (Engine, error)
	}); ok && strings.Contains(re.source, `\G`) {
		if !input.boundary(s, pos) {
			return mrb.Raisef(mrb.EIndexError(), "offset %d does not land on character boundary", pos)
		}
		fixed, err := engine.WithSearchOrigin(input.text, input.engineOffset(pos))
		if err != nil {
			return raiseRegexpError(mrb, err)
		}
		find = func(pos int) ([]int, error) {
			match, err := re.findWith(fixed, input.text, input.engineOffset(pos), deadline)
			return input.sourceMatch(match), err
		}
	}
	for p := pos; p >= 0; {
		m, err := find(p)
		if err != nil {
			return raiseTimeout(mrb, err)
		}
		if m != nil && m[0] == p {
			md := newMatchValue(mrb, re, s, m)
			mrb.Data(md).(*MatchData).enc = enc
			setLastMatch(mrb, md)
			if bytes {
				return mrb.FixnumValue(p)
			}
			return mrb.FixnumValue(regexpCharIndex(s, p, enc))
		}
		if p == 0 {
			break
		}
		p = input.previous(s, p)
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
	enc := regexpSubjectEncoding(mrb, self)
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
	input := newRegexpText(s, enc)
	if !input.boundary(s, pos) {
		return mrb.Raise(mrb.EIndexError(), "offset "+strconv.Itoa(pos)+" does not land on character boundary")
	}
	result, err := re.findText(input, pos, re.deadline())
	if err != nil {
		return raiseTimeout(mrb, err)
	}
	if result == nil {
		setLastMatch(mrb, mrb.NilValue())
		return mrb.NilValue()
	}
	md := newMatchValue(mrb, re, s, result)
	mrb.Data(md).(*MatchData).enc = enc
	setLastMatch(mrb, md)
	if md.Value().IsNil() || md.Value().Type() != oruby.MrbTTCData {
		return md
	}
	return mrb.FixnumValue(mrb.Data(md.Value()).(*MatchData).byteBegin(0))
}
