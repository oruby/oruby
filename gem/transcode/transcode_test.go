package transcode_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/transcode"
)

func eval(t *testing.T, mrb *oruby.MrbState, code string) string {
	t.Helper()
	v, err := mrb.Eval(code)
	if err != nil {
		t.Fatalf("%s: %v", code, err)
	}
	return v.String()
}

func TestEncode(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	for code, want := range map[string]string{
		`"\xA4\xA2".force_encoding("EUC-JP").encode("UTF-8")`:          "あ",
		`"あ".encode("EUC-JP").bytes.inspect`:                           "[164, 162]",
		`"あ".encode("EUC-JP").encoding.name`:                           "EUC-JP",
		`"あいう".encode("ISO-2022-JP").b.inspect`:                        `"\e$B$\"$$$&\e(B"`,
		`"ab\xFFc".encode("ISO-8859-1", invalid: :replace)`:            "ab?c",
		`"あ?あ".encode("EUC-JP", undef: :replace).bytes.size`:           "5",
		`"<ü>".encode("US-ASCII", xml: :text)`:                         "&lt;&#xFC;&gt;",
		`"a\"b".encode("UTF-8", xml: :attr)`:                           `"a&quot;b"`,
		`"a\nb".encode("UTF-8", crlf_newline: true)`:                   "a\r\nb",
		`"B�".encode("US-ASCII", fallback: { "�" => "bar" })`:          "Bbar",
		`"B�".encode("US-ASCII", fallback: ->(c) { c.bytes.inspect })`: "B[239, 191, 189]",
		`("testĀ".encode("Windows-1252") rescue $!.class).to_s`:        "Encoding::UndefinedConversionError",
		`("abc".encode("xyz") rescue $!.message)`:                      "code converter not found (UTF-8 to xyz)",
		`"y".b.encode("Emacs-Mule").encoding.name`:                     "Emacs-Mule",
		`"\xA4\xA2".force_encoding("EUC-JP").valid_encoding?.to_s`:     "true",
		`"\xA4".force_encoding("EUC-JP").valid_encoding?.to_s`:         "false",
		`"a\xA4b".force_encoding("EUC-JP").scrub.b.inspect`:            `"a?b"`,
	} {
		if got := eval(t, mrb, code); got != want {
			t.Errorf("%s = %q, want %q", code, got, want)
		}
	}
}

func TestConverter(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	for code, want := range map[string]string{
		`Encoding::Converter.new("utf-8", "euc-jp").convert("あ").bytes.inspect`:                                                                                    "[164, 162]",
		`ec = Encoding::Converter.new("utf-8", "iso-8859-1"); s = "\xf1abcd".b; r = ec.primitive_convert(s, d = "".b); [r, s, ec.primitive_errinfo].inspect`:       `[:invalid_byte_sequence, "bcd", [:invalid_byte_sequence, "UTF-8", "ISO-8859-1", "\xF1", "a"]]`,
		`ec = Encoding::Converter.new("EUC-JP", "ISO-8859-1"); ec.primitive_convert(s = "abc\xa1def".b, d = "".b, nil, 10); [ec.putback, ec.putback.size].inspect`: `["d", 0]`,
		`ec = Encoding::Converter.new("utf-8", "iso-2022-jp"); [ec.convert("香").b, ec.finish.b].inspect`:                                                           `["\e$B9a", "\e(B"]`,
		`Encoding::Converter.new("ascii", "Big5").convpath.inspect`:                                                                                                "[[#<Encoding:US-ASCII>, #<Encoding:UTF-8>], [#<Encoding:UTF-8>, #<Encoding:Big5>]]",
		`Encoding::Converter.search_convpath("ISO-8859-1", "EUC-JP", crlf_newline: true).last`:                                                                     "crlf_newline",
		`Encoding::Converter.new("utf-8", "utf-16le").inspect`:                                                                                                     "#<Encoding::Converter: UTF-8 to UTF-16LE>",
		`Encoding::Converter.new("us-ascii", "binary").replacement.encoding.name`:                                                                                  "US-ASCII",
		`Encoding::Converter.asciicompat_encoding("UTF-16BE").name`:                                                                                                "UTF-8",
		`begin; Encoding::Converter.new("iso-8859-1", "Big5").convert("\xE9".b); rescue => e; e.message; end`:                                                      "U+00E9 to Big5 in conversion from ISO-8859-1 to UTF-8 to Big5",
		`begin; Encoding::Converter.new("utf-8", "ascii").convert("蝥"); rescue => e; [e.message, e.error_char, e.source_encoding.name].inspect; end`:               `["U+8765 from UTF-8 to US-ASCII", "蝥", "UTF-8"]`,
		`begin; Encoding::Converter.new("utf-8", "utf-8"); rescue => e; e.class.name; end`:                                                                         "Encoding::ConverterNotFoundError",
	} {
		if got := eval(t, mrb, code); got != want {
			t.Errorf("%s =\n  %q, want\n  %q", code, got, want)
		}
	}
}
