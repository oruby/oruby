// Package transcode implements Encoding::Converter and String#encode between
// the encodings MRI converts, on golang.org/x/text tables: UTF-8/16/32,
// US-ASCII, ASCII-8BIT, the ISO-8859, Windows, IBM, KOI8 and Mac single-byte
// sets, EUC-JP, Shift_JIS/Windows-31J, ISO-2022-JP, EUC-KR/CP949, GBK,
// GB2312, GB18030 and Big5.
//
// It is part of standalone ORuby (gem/stdlib); embedded use links it only by
// importing it, as x/text's tables are large.
package transcode

import (
	_ "embed"
	"errors"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed transcode.rb
var transcodeRb []byte

func init() {
	oruby.Gem("transcode", func(mrb *oruby.MrbState) interface{} {
		if _, err := mrb.Resolve("core_ext"); err != nil {
			panic(err)
		}
		cls := rb.DataClass(mrb.EncodingClass().DefineClassUnder("Converter", mrb.ObjectClass()))
		cls.DefineMethod("__setup", convSetup, mrb.ArgsReq(3))
		cls.DefineMethod("__convert", convConvert, mrb.ArgsReq(3))
		cls.DefineMethod("__errinfo", convErrinfo, mrb.ArgsNone())
		cls.DefineMethod("__putback", convPutback, mrb.ArgsReq(1))
		cls.DefineMethod("__insert", convInsert, mrb.ArgsReq(2))
		cls.DefineMethod("__replacement", convReplacement, mrb.ArgsNone())
		cls.DefineMethod("__set_replacement", convSetReplacement, mrb.ArgsReq(2))
		cls.DefineMethod("__names", convNames, mrb.ArgsNone())
		cls.DefineClassMethod("__path", convPath, mrb.ArgsReq(2))
		cls.DefineClassMethod("__invalid_spans", convInvalidSpans, mrb.ArgsReq(2))
		cls.DefineClassMethod("__char_len", convCharLen, mrb.ArgsReq(3))
		for name, v := range map[string]int{
			"INVALID_MASK": InvalidMask, "INVALID_REPLACE": InvalidReplace,
			"UNDEF_MASK": UndefMask, "UNDEF_REPLACE": UndefReplace, "UNDEF_HEX_CHARREF": UndefHexCharref,
			"PARTIAL_INPUT": PartialInput, "AFTER_OUTPUT": AfterOutput,
			"UNIVERSAL_NEWLINE_DECORATOR": UniversalNewline, "LF_NEWLINE_DECORATOR": LFNewline,
			"CRLF_NEWLINE_DECORATOR": CRLFNewline, "CR_NEWLINE_DECORATOR": CRNewline,
			"XML_TEXT_DECORATOR": XMLText, "XML_ATTR_CONTENT_DECORATOR": XMLAttrContent,
			"XML_ATTR_QUOTE_DECORATOR": XMLAttrQuote,
		} {
			cls.Const(name, v)
		}
		rb.LoadRuby(mrb, "transcode.rb", transcodeRb)
		return nil
	})
}

func get(mrb *oruby.MrbState, self oruby.Value) (*Converter, oruby.MrbValue) {
	c, ok := rb.Get[*Converter](mrb, self)
	if !ok || c == nil {
		return nil, rb.Raise(mrb, "TypeError", "uninitialized converter")
	}
	return c, nil
}

func bytesOrNil(mrb *oruby.MrbState, b []byte) oruby.Value {
	if b == nil {
		return mrb.NilValue()
	}
	return mrb.BytesValue(b)
}

func sym(mrb *oruby.MrbState, s Status) oruby.Value {
	return mrb.SymbolValue(mrb.Intern(string(s)))
}

// __setup(src, dst, flags): false when there is no converter
func convSetup(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src, dst, flags := mrb.GetArgs3()
	c, err := New(src.String(), dst.String(), flags.Int())
	if err != nil {
		var nf ErrNotFound
		if errors.As(err, &nf) {
			return mrb.FalseValue()
		}
		return rb.Raise(mrb, "ArgumentError", "%v", err)
	}
	rb.Set(mrb, self, c)
	return mrb.TrueValue()
}

// __convert(input or nil, limit, flags) -> [status, consumed, output]
func convConvert(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, e := get(mrb, self)
	if c == nil {
		return e
	}
	in, limit, flags := mrb.GetArgs3()
	var input []byte
	if !in.IsNil() {
		input = in.Bytes()
	}
	st, consumed, out := c.Convert(input, !in.IsNil(), limit.Int(), flags.Int())
	return mrb.AryNewFromValues(sym(mrb, st), mrb.FixnumValue(consumed), mrb.BytesValue(out))
}

// __errinfo -> [status, source, destination, error bytes, read-again bytes]
func convErrinfo(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, e := get(mrb, self)
	if c == nil {
		return e
	}
	l := c.Last
	switch l.Status {
	case InvalidByteSequence, IncompleteInput, UndefinedConversion:
		return mrb.AryNewFromValues(sym(mrb, l.Status), mrb.StringValue(l.Source).Value(), mrb.StringValue(l.Dest).Value(),
			mrb.BytesValue(l.ErrBytes), mrb.BytesValue(l.ReadAgain))
	}
	return mrb.AryNewFromValues(sym(mrb, l.Status), mrb.NilValue(), mrb.NilValue(), mrb.NilValue(), mrb.NilValue())
}

func convPutback(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, e := get(mrb, self)
	if c == nil {
		return e
	}
	return mrb.BytesValue(c.Putback(mrb.GetArgsFirst().Int()))
}

// __insert(str, encoding name): false when str cannot be converted
func convInsert(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, e := get(mrb, self)
	if c == nil {
		return e
	}
	str, enc := mrb.GetArgs2()
	return mrb.BoolValue(c.Insert(str.Bytes(), enc.String()))
}

// __replacement -> [bytes, encoding name]
func convReplacement(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, e := get(mrb, self)
	if c == nil {
		return e
	}
	b, enc := c.Replacement()
	return mrb.AryNewFromValues(mrb.BytesValue(b), mrb.StringValue(enc).Value())
}

func convSetReplacement(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, e := get(mrb, self)
	if c == nil {
		return e
	}
	str, enc := mrb.GetArgs2()
	return mrb.BoolValue(c.SetReplacement(str.Bytes(), enc.String()))
}

// __names -> [source, destination, path destinations...]
func convNames(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, e := get(mrb, self)
	if c == nil {
		return e
	}
	vals := []oruby.Value{mrb.StringValue(c.Src).Value(), mrb.StringValue(c.Dst).Value()}
	for _, step := range Path(c.Src, c.Dst) {
		vals = append(vals, mrb.StringValue(step[1]).Value())
	}
	return mrb.AryNewFromValues(vals...)
}

// Converter.__path(src, dst) -> [[src, dst], ...] or nil
func convPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	src, dst := mrb.GetArgs2()
	p := Path(src.String(), dst.String())
	if p == nil {
		return mrb.NilValue()
	}
	steps := make([]oruby.Value, len(p))
	for i, s := range p {
		steps[i] = mrb.AryNewFromValues(mrb.StringValue(s[0]).Value(), mrb.StringValue(s[1]).Value()).Value()
	}
	return mrb.AryNewFromValues(steps...)
}

// Converter.__invalid_spans(str, encoding name) -> [start, length, ...] or nil
func convInvalidSpans(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str, enc := mrb.GetArgs2()
	spans, ok := InvalidSpans(str.Bytes(), enc.String())
	if !ok {
		return mrb.NilValue()
	}
	vals := make([]oruby.Value, 0, 2*len(spans))
	for _, s := range spans {
		vals = append(vals, mrb.FixnumValue(s[0]), mrb.FixnumValue(s[1]))
	}
	return mrb.AryNewFromValues(vals...)
}

// Converter.__char_len(str, pos, enc_name) -> byte length of the character at pos
func convCharLen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	b := a[0].Bytes()
	pos := a[1].Int()
	if pos < 0 || pos >= len(b) {
		return mrb.FixnumValue(0)
	}
	c, ok := codecs[strings.ToUpper(a[2].String())]
	if !ok {
		return mrb.FixnumValue(1)
	}
	dec := c.dec()
	var buf [8]byte
	_, n, res, _ := dec.decode(buf[:0], b[pos:])
	if res == rInvalid || n <= 0 {
		return mrb.FixnumValue(1)
	}
	return mrb.FixnumValue(n)
}
