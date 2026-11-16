package full_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/regexp/full"
)

func eval(t *testing.T, code string) string {
	t.Helper()
	mrb, err := oruby.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mrb.Close()
	v, err := mrb.Eval(code)
	if err != nil {
		t.Fatalf("%s: %v", code, err)
	}
	return mrb.String(mrb.Call(v, "inspect"))
}

// Ruby-only constructs run on the regexp2 fallback; expected values are MRI's
func TestRubyOnlyConstructs(t *testing.T) {
	cases := []struct{ code, want string }{
		{`"ab ac".index(/a(?=c)/)`, `3`},
		{`"100px 200"[/\d+(?!px|\d)/]`, `"200"`},
		{`"100px"[/\d+(?!px)/]`, `"10"`},
		{`"$10 €20"[/(?<=€)\d+/]`, `"20"`},
		{`"xa ya".index(/(?<!x)a/)`, `4`},
		{`["aaa" =~ /(?>a+)a/, "aaab" =~ /(?>a+)b/]`, `[nil, 0]`},
		{`["aaa" =~ /a++a/, "aaab"[/a++b/]]`, `[nil, "aaab"]`},
		{`["abab"[/(ab)\1/], "aaaa"[/(a+)a\1/]]`, `["abab", "aaa"]`},
		{`"AbcABC"[/(abc)\1/i]`, `"AbcABC"`},
		{`"abcabc"[/(?<w>abc)\k<w>/]`, `"abcabc"`},
		{`"price: 42".sub(/price: \K\d+/, "99")`, `"price: 99"`},
		{`"aab".scan(/\Ga/)`, `["a", "a"]`},
		{`["a\n" =~ /a\Z/, "a\nb" =~ /a\Z/]`, `[0, nil]`},
		{`"a\r\nb\nc".split(/\R/)`, `["a", "b", "c"]`},
		{`"hello world".scan(/[a-z&&[^aeiou]]/).join`, `"hllwrld"`},
		{`["<a>" =~ /(<)?a(?(1)>|)/, "a" =~ /(<)?a(?(1)>|)/]`, `[0, 0]`},
		{`"1234567".gsub(/(\d)(?=(\d{3})+$)/, '\1,')`, `"1,234,567"`},
		{`"HelloWorldFoo".split(/(?=[A-Z])/)`, `["Hello", "World", "Foo"]`},
		{`["ab".match(/^b/, 1), "a\nb".match(/^b/, 2)[0]]`, `[nil, "b"]`},
		{`m = /(?<=é)(b)/.match("éb"); [m.begin(0), m.pre_match, m[1]]`, `[1, "é", "b"]`},
	}
	for _, c := range cases {
		if got := eval(t, c.code); got != c.want {
			t.Errorf("%s = %s, want %s", c.code, got, c.want)
		}
	}
}

// RubyGems' version pattern uses an atomic group
func TestRubyGemsVersionPattern(t *testing.T) {
	got := eval(t, `
		pat = '[0-9]+(?>\.[0-9a-zA-Z]+)*(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?'
		re = /\A\s*(#{pat})?\s*\z/
		["1.2.3-pre.1", "3.4.0.rc1", " 2 ", "x"].map { |v| re.match?(v) }`)
	if got != "[true, true, true, false]" {
		t.Error(got)
	}
}

func TestUnsupportedConstructsRaise(t *testing.T) {
	for _, src := range []string{`(?<a>x\g<a>?)`, `(?~abc)`} {
		got := eval(t, `begin; Regexp.new('`+src+`'); :no_error; rescue RegexpError => e; e.class; end`)
		if got != "RegexpError" {
			t.Errorf("%s: %s", src, got)
		}
	}
}
