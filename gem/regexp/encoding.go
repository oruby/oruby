package regexp

import (
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

// Regexp encodings, as MRI's re.c (rb_reg_initialize_str, rb_reg_preprocess,
// rb_reg_s_union): a regexp has the encoding of its source unless that is
// ASCII-only (US-ASCII, not fixed), a /u /e /s literal fixes UTF-8, EUC-JP or
// Windows-31J, a non-ASCII \u escape UTF-8, and /n (NOENCODING) is BINARY
// with non-ASCII bytes or escapes, US-ASCII without.

// findEncoding is Encoding.find(name); Nil without MRI's Encoding (the
// regexp gem without core_ext)
func findEncoding(mrb *oruby.MrbState, name string) oruby.Value {
	enc := mrb.EncodingClass().Value()
	if !mrb.RespondTo(enc, oruby.Sym_find) {
		return oruby.Nil
	}
	v, err := mrb.Funcall(enc, oruby.Sym_find, mrb.StrNew(name))
	if err != nil {
		mrb.ExcClear()
		return oruby.Nil
	}
	return v.Value()
}

// forceEncoding tags v with the named encoding (none: left as is)
func forceEncoding(mrb *oruby.MrbState, v oruby.Value, name string) {
	if enc := findEncoding(mrb, name); !enc.IsNil() {
		_, _ = mrb.Funcall(v, oruby.Sym_force_encoding, enc)
	}
}

func asciiCompatible(mrb *oruby.MrbState, enc oruby.Value) bool {
	if enc.IsNil() || !mrb.RespondTo(enc, mrb.Intern("ascii_compatible?")) {
		return true
	}
	return mrb.Call(enc, "ascii_compatible?").Bool()
}

// hasHighEscape: a \xHH or octal escape of a byte past 0x7f
func hasHighEscape(src string) bool {
	for i := 0; i+1 < len(src); i++ {
		if src[i] != '\\' {
			continue
		}
		i++
		switch c := src[i]; {
		case c == 'x' && i+1 < len(src):
			j := i + 1
			for j < len(src) && j < i+3 && isHex(src[j]) {
				j++
			}
			if v, err := strconv.ParseUint(src[i+1:j], 16, 8); err == nil && v >= 0x80 {
				return true
			}
		case c >= '0' && c <= '7':
			j := i
			for j < len(src) && j < i+3 && src[j] >= '0' && src[j] <= '7' {
				j++
			}
			if v, err := strconv.ParseUint(src[i:j], 8, 16); err == nil && v >= 0x80 {
				return true
			}
		}
	}
	return false
}

// unicodeEscapes scans the \u escapes of src: whether one is past ASCII, or
// MRI's error for a malformed one
func unicodeEscapes(src string) (nonASCII bool, errMsg string) {
	for i := 0; i+1 < len(src); i++ {
		if src[i] != '\\' {
			continue
		}
		i++
		if src[i] != 'u' {
			continue
		}
		if i+1 < len(src) && src[i+1] == '{' {
			end := strings.IndexByte(src[i+2:], '}')
			if end < 0 {
				return nonASCII, "invalid Unicode list"
			}
			list := src[i+2 : i+2+end]
			fields := strings.Fields(list)
			if len(fields) == 0 {
				return nonASCII, "invalid Unicode list"
			}
			for _, f := range fields {
				for k := 0; k < len(f); k++ {
					if !isHex(f[k]) {
						return nonASCII, "invalid Unicode list"
					}
				}
				if len(f) > 6 {
					return nonASCII, "invalid Unicode range"
				}
				v, _ := strconv.ParseUint(f, 16, 32)
				if v > 0x10ffff {
					return nonASCII, "invalid Unicode range"
				}
				if v >= 0x80 {
					nonASCII = true
				}
			}
			i += 2 + end
			continue
		}
		if i+4 < len(src) {
			if v, err := strconv.ParseUint(src[i+1:i+5], 16, 32); err == nil {
				if v >= 0x80 {
					nonASCII = true
				}
				i += 4
			}
		}
	}
	return nonASCII, ""
}

// regexpEncoding gives a new regexp's encoding name and options: src in
// srcEnc (Nil: UTF-8), lit the encoding letter of a literal (/u /e /s /n)
func regexpEncoding(mrb *oruby.MrbState, src string, srcEnc oruby.Value, opts int, lit byte) (string, int) {
	switch lit {
	case 'u':
		return "UTF-8", opts | FixedEncoding
	case 'e':
		return "EUC-JP", opts | FixedEncoding
	case 's':
		return "Windows-31J", opts | FixedEncoding
	case 'n':
		opts |= NoEncoding
	}
	if !asciiCompatible(mrb, srcEnc) {
		return encName(mrb, srcEnc), opts | FixedEncoding
	}
	if opts&NoEncoding != 0 {
		if isASCII(src) && !hasHighEscape(src) {
			return "US-ASCII", opts &^ FixedEncoding
		}
		return "ASCII-8BIT", opts | FixedEncoding
	}
	if !srcEnc.IsNil() && hasHighEscape(src) && encName(mrb, srcEnc) == "ASCII-8BIT" {
		// \x80.. in a binary source: a binary regexp (rb_reg_preprocess)
		return "ASCII-8BIT", opts | FixedEncoding
	}
	if u, _ := unicodeEscapes(src); u {
		return "UTF-8", opts | FixedEncoding
	}
	if isASCII(src) {
		if opts&FixedEncoding != 0 {
			return encName(mrb, srcEnc), opts
		}
		return "US-ASCII", opts
	}
	return encName(mrb, srcEnc), opts | FixedEncoding
}

// regexpEncName is self's encoding name (a Go-made Regexp: from its source)
func regexpEncName(mrb *oruby.MrbState, self oruby.Value) string {
	r := regexpOf(mrb, self)
	if r != nil && r.enc != "" {
		return r.enc
	}
	if r == nil && mrb.IVGet(self, mrb.Intern("@source")).IsNil() {
		return "ASCII-8BIT" // Regexp.allocate, not initialized (MRI's)
	}
	src := ivString(mrb, self, "@source")
	opts := ivOptions(mrb, self)
	switch {
	case opts&NoEncoding != 0 && !isASCII(src):
		return "ASCII-8BIT"
	case isASCII(src) && opts&FixedEncoding == 0:
		return "US-ASCII"
	}
	return "UTF-8"
}

// sourceValue is the source String, in the regexp's encoding
func sourceValue(mrb *oruby.MrbState, src, enc string) oruby.Value {
	v := mrb.StrNew(src).Value()
	if enc != "UTF-8" {
		forceEncoding(mrb, v, enc)
	}
	return v
}

// verbose: $VERBOSE is true (rb_warning)
func verbose(mrb *oruby.MrbState) bool {
	return mrb.GVGet(mrb.Intern("$VERBOSE")).Type() == oruby.MrbTTTrue
}

func warn(mrb *oruby.MrbState, msg string) {
	if mrb.GVGet(mrb.Intern("$VERBOSE")).IsNil() {
		return
	}
	mrb.Call(mrb.ModuleGet("Kernel").Value(), "warn", mrb.StrNew("warning: "+msg))
}

// regexpOptArg is the options argument of Regexp.new (MRI's
// reg_extract_args): an Integer, a String of "imx", else ignorecase when
// truthy (with a warning in verbose mode for anything but true)
func regexpOptArg(mrb *oruby.MrbState, v oruby.Value) (int, oruby.MrbValue) {
	switch v.Type() {
	case oruby.MrbTTFixnum:
		return oruby.MrbFixnum(v) & (IgnoreCase | Extended | Multiline | FixedEncoding | NoEncoding), nil
	case oruby.MrbTTString:
		s := mrb.String(v)
		opts := 0
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case 'i':
				opts |= IgnoreCase
			case 'x':
				opts |= Extended
			case 'm':
				opts |= Multiline
			default:
				return 0, mrb.Raise(mrb.EArgumentError(), "unknown regexp option: "+s)
			}
		}
		return opts, nil
	case oruby.MrbTTFalse:
		return 0, nil
	case oruby.MrbTTTrue:
		return IgnoreCase, nil
	}
	if v.IsNil() {
		return 0, nil
	}
	if verbose(mrb) {
		warn(mrb, "expected true or false as ignorecase: "+mrb.String(mrb.Inspect(v)))
	}
	return IgnoreCase, nil
}

// regexQuote is Regexp.escape / Regexp.quote: US-ASCII when the result is
// ASCII-only, otherwise the argument's encoding
func regexQuote(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	var s string
	enc := oruby.Nil
	if arg.Type() == oruby.MrbTTSymbol {
		s = mrb.SymString(arg.Symbol())
	} else {
		v, exc := implicitStr(mrb, arg)
		if exc != nil {
			return exc
		}
		s, enc = mrb.String(v), strEnc(mrb, v)
	}
	q := QuoteMeta(s)
	res := mrb.StrNew(q)
	switch {
	case isASCII(q) && asciiCompatible(mrb, enc):
		forceEncoding(mrb, res.Value(), "US-ASCII")
	case !enc.IsNil():
		_, _ = mrb.Funcall(res, oruby.Sym_force_encoding, enc)
	}
	return res
}

// regexUnion is Regexp.union, as MRI's rb_reg_s_union: strings escaped,
// regexps by #to_s; the encodings of the parts must agree
func regexUnion(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) == 1 && args[0].Type() == oruby.MrbTTArray {
		args = oruby.RArraySlice(args[0])
	}
	cls := mrb.ClassPtr(self)
	if len(args) == 0 {
		r, err := mrb.ObjNew(cls, mrb.StrNew("(?!)"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return r
	}
	if len(args) == 1 {
		if _, ok := mrb.Data(args[0]).(*Regexp); ok {
			return args[0]
		}
		if t := regexTryConvertValue(mrb, args[0]); !t.IsNil() {
			return t
		}
		q := regexQuoteValue(mrb, args[0])
		if mrb.HasExc() {
			return mrb.Exc().Value()
		}
		r, err := mrb.ObjNew(cls, q)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return r
	}
	var b strings.Builder
	incompat, fixed := "", ""
	asciiOnly := false
	conflict := func(a, b string) oruby.MrbValue {
		return mrb.Raise(mrb.EArgumentError(), "incompatible encodings: "+a+" and "+b)
	}
	for i, a := range args {
		if i > 0 {
			b.WriteByte('|')
		}
		if v := regexTryConvertValue(mrb, a); !v.IsNil() {
			a = v
		}
		if _, ok := mrb.Data(a).(*Regexp); ok && a.Type() == oruby.MrbTTCData {
			name := regexpEncName(mrb, a)
			switch {
			case !asciiCompatible(mrb, findEncoding(mrb, name)):
				if incompat == "" {
					incompat = name
				} else if incompat != name {
					return conflict(incompat, name)
				}
			case ivOptions(mrb, a)&FixedEncoding != 0:
				if fixed == "" {
					fixed = name
				} else if fixed != name {
					return conflict(fixed, name)
				}
			default:
				asciiOnly = true
			}
			b.WriteString(mrb.String(regexToS(mrb, a).Value()))
		} else {
			sv, exc := implicitStr(mrb, a)
			if exc != nil {
				return exc
			}
			s := mrb.String(sv)
			enc := strEnc(mrb, sv)
			name := encName(mrb, enc)
			switch {
			case !asciiCompatible(mrb, enc):
				if incompat == "" {
					incompat = name
				} else if incompat != name {
					return conflict(incompat, name)
				}
			case isASCII(s):
				asciiOnly = true
			default:
				if fixed == "" {
					fixed = name
				} else if fixed != name {
					return conflict(fixed, name)
				}
			}
			b.WriteString(QuoteMeta(s))
		}
		if incompat != "" {
			if asciiOnly {
				return mrb.Raise(mrb.EArgumentError(), "ASCII incompatible encoding: "+incompat)
			}
			if fixed != "" {
				return conflict(incompat, fixed)
			}
		}
	}
	resEnc := "ASCII-8BIT"
	switch {
	case incompat != "":
		resEnc = incompat
	case fixed != "":
		resEnc = fixed
	}
	src := mrb.StrNew(b.String())
	if resEnc != "UTF-8" {
		forceEncoding(mrb, src.Value(), resEnc)
	}
	r, err := mrb.ObjNew(cls, src)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return r
}

// regexQuoteValue quotes a String (or Symbol) keeping its encoding
func regexQuoteValue(mrb *oruby.MrbState, v oruby.Value) oruby.Value {
	if v.Type() == oruby.MrbTTSymbol {
		return mrb.StrNew(QuoteMeta(mrb.SymString(v.Symbol()))).Value()
	}
	sv, exc := implicitStr(mrb, v)
	if exc != nil {
		return oruby.Nil
	}
	q := mrb.StrNew(QuoteMeta(mrb.String(sv))).Value()
	if enc := strEnc(mrb, sv); !enc.IsNil() {
		_, _ = mrb.Funcall(q, oruby.Sym_force_encoding, enc)
	}
	return q
}

// regexTryConvertValue: v by #to_regexp, Nil when it has none
func regexTryConvertValue(mrb *oruby.MrbState, v oruby.Value) oruby.Value {
	if _, ok := mrb.Data(v).(*Regexp); ok && v.Type() == oruby.MrbTTCData {
		return v
	}
	if v.Type() == oruby.MrbTTString || !mrb.RespondTo(v, oruby.Sym_to_regexp) {
		return oruby.Nil
	}
	r, err := mrb.Funcall(v, oruby.Sym_to_regexp)
	if err != nil {
		return oruby.Nil
	}
	if _, ok := mrb.Data(r).(*Regexp); ok && r.Type() == oruby.MrbTTCData {
		return r.Value()
	}
	return oruby.Nil
}

// regexToSOptions folds leading (?flags-flags:...) groups spanning the whole
// source into the options, as MRI's rb_reg_to_s
func regexToSOptions(src string, options int) (string, int) {
	orig, origOpts := src, options
	opt := func(c byte) int {
		switch c {
		case 'i':
			return IgnoreCase
		case 'm':
			return Multiline
		case 'x':
			return Extended
		}
		return 0
	}
	for len(src) >= 4 && src[0] == '(' && src[1] == '?' {
		p := src[2:]
		for len(p) > 0 && opt(p[0]) != 0 {
			options |= opt(p[0])
			p = p[1:]
		}
		if len(p) > 1 && p[0] == '-' {
			p = p[1:]
			for len(p) > 0 && opt(p[0]) != 0 {
				options &^= opt(p[0])
				p = p[1:]
			}
		}
		if len(p) > 0 && p[0] == ')' {
			src = p[1:]
			continue
		}
		if len(p) > 0 && p[0] == ':' && p[len(p)-1] == ')' {
			inner := p[1 : len(p)-1]
			if _, err := compileRuby(inner, options&(IgnoreCase|Multiline|Extended)); err == nil {
				return inner, options
			}
		}
		return orig, origOpts
	}
	return src, options
}

// matchCheck is MRI's rb_reg_prepare_enc for matching the Regexp self
// against str: an ArgumentError for a broken string, an
// Encoding::CompatibilityError for encodings that cannot meet, the warning
// for a /n regexp against a non-ASCII UTF-8 string. False when it raised.
// A plain UTF-8 subject costs one read of its cached coderange.
func matchCheck(mrb *oruby.MrbState, self, str oruby.Value) bool {
	if str.Type() != oruby.MrbTTString {
		return true
	}
	cr := mrb.StrCoderange(str)
	compat := func(renc, senc string) bool {
		mrb.Raisef(mrb.ClassGetUnder(mrb.EncodingClass(), "CompatibilityError"),
			"incompatible encoding regexp match (%s regexp with %s string)", renc, senc)
		return false
	}
	r := regexpOf(mrb, self)
	switch cr {
	case oruby.StrASCII:
		// only a regexp of a fixed encoding that is not ASCII compatible refuses it
		if r != nil && r.enc != "" && !asciiCompatibleName(r.enc) {
			return compat(r.enc, "UTF-8")
		}
		return true
	case oruby.StrBroken:
		mrb.Raise(mrb.EArgumentError(), "invalid byte sequence in UTF-8")
		return false
	case oruby.StrValid:
		opts := ivOptions(mrb, self)
		renc := regexpEncName(mrb, self)
		if opts&NoEncoding != 0 {
			warn(mrb, "historical binary regexp match /.../n against UTF-8 string")
			return true
		}
		if (opts&FixedEncoding != 0 || !asciiCompatibleName(renc)) && renc != "UTF-8" {
			return compat(renc, "UTF-8")
		}
		return true
	case oruby.StrBytes:
		senc := encName(mrb, mrb.Call(str, "encoding"))
		renc := regexpEncName(mrb, self)
		if senc == renc {
			return true
		}
		if !asciiCompatibleName(senc) || !asciiCompatibleName(renc) {
			return compat(renc, senc)
		}
		if ivOptions(mrb, self)&FixedEncoding != 0 && !isASCII(rstr(str)) {
			return compat(renc, senc)
		}
	}
	return true
}

// asciiCompatibleName: the encodings oruby knows that are not ASCII
// compatible are the UTF-16 and UTF-32 family
func asciiCompatibleName(name string) bool {
	return !strings.HasPrefix(name, "UTF-16") && !strings.HasPrefix(name, "UTF-32") && !strings.HasPrefix(name, "UTF16") && !strings.HasPrefix(name, "UTF32")
}
