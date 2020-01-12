package regexp

import (
	"testing"

	"github.com/oruby/oruby"
)

func errorHandler(t *testing.T) {
	if r := recover(); r != nil {
		t.Error(r)
	}
}

func assert(t *testing.T, desc, code string) {
	t.Helper()
	defer errorHandler(t)

	mrb, _ := oruby.New()
	defer mrb.Close()

	result, err := mrb.Eval(code)
	if err != nil {
		t.Fatal(desc, ": ", err)
	}

	if !oruby.MrbBoolean(result) {
		t.Error(desc, ": result =", mrb.Intf(result))
	}
}

func TestRegexpConsts(t *testing.T) {
	assert(t, "Regexp::CONSTANT", `Regexp::IGNORECASE == 1 and Regexp::EXTENDED == 2 and Regexp::MULTILINE == 4`)
}

func TestRegexp_New(t *testing.T) {
	assert(t, "Regexp.new", `Regexp.new(".*")`)
}

func TestRegexp_Compile(t *testing.T) {
	assert(t, "Regexp.compile", `Regexp.compile(".*")`)
}

func TestRegexp_New2(t *testing.T) {
	assert(t, "Regexp.new", `Regexp.new(".*", Regexp::MULTILINE)`)
}

func TestRegexp_New3(t *testing.T) {
	assert(t, "Regexp.new", `Regexp.new(".*") and Regexp.new(".*", Regexp::MULTILINE)`)
}

func TestRegexp(t *testing.T) {
	assert(t, "Regexp#==",
		`reg1 = reg2 = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+")
  reg3 = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+")
  reg4 = Regexp.new("(https://[^/]+)[-a-zA-Z0-9./]+")

  reg1 == reg2 and reg1 == reg3 and !(reg1 == reg4)
`)

	assert(t, "Regexp#===",
		`reg = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+")
  (reg === "http://example.com") == true and (reg === "htt://example.com") == false
`)

	assert(t, "Regexp#casefold?",
		`reg1 = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+", Regexp::MULTILINE)
  reg2 = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+", Regexp::IGNORECASE | Regexp::EXTENDED)
  reg3 = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+", Regexp::MULTILINE | Regexp::IGNORECASE)
  reg4 = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+")
  reg5 = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+", true)

  reg1.casefold? == false and reg2.casefold? == true and reg3.casefold? == true and
    reg4.casefold? == false and reg5.casefold? == true
`)
}

func TestRegexp_New4(t *testing.T) {
	assert(t, "Regexp#match",
		`reg = Regexp.new("(https?://[^/]+)[-a-zA-Z0-9./]+")
     reg.match("http://masamitsu-murase.12345/hoge.html") and reg.match("http:///masamitsu-murase.12345/hoge.html").nil?
`)
}

func TestRegexp_New5(t *testing.T) {
	assert(t, "Regexp#source",
		`str = "(https?://[^/]+)[-a-zA-Z0-9./]+"
	  reg = Regexp.new(str)
	  reg.source == str
`)

	assert(t, "Regexp#match (no flags)",
		`patterns = [
    [ Regexp.new(".*"), "abcd\nefg", "abcd" ],
    [ Regexp.new("^a."), "abcd\naefg", "ab" ],
    [ Regexp.new("^b."), "bacd\naefg", "ba" ],
    [ Regexp.new(".$"), "bacd\naefg", "g" ]
  ]

  patterns.all?{ |reg, str, result| reg.match(str)[0] == result }
`)
}

func TestRegexp_Multiline(t *testing.T) {
	assert(t, "Regexp#match (multiline)", `patterns = [
    [ Regexp.new(".*", Regexp::MULTILINE), "abc\ndef", "abc\ndef" ],
    [ Regexp.new(".*"), "abc\ndef", "abc" ],
    [ Regexp.new(".c$", Regexp::MULTILINE), "abc\nefc", "bc" ],
    [ Regexp.new(".c$"), "abc\nefc", "fc" ]
  ]

  patterns.all?{ |reg, str, result| reg.match(str)[0] == result}
`)
}

func TestRegexp_Ignorecase(t *testing.T) {
	assert(t, "Regexp#match (ignorecase)", `patterns = [
    [ Regexp.new("aBcD",      Regexp::IGNORECASE|Regexp::EXTENDED), "00AbcDef",   "AbcD" ],
    [ Regexp.new("0x[a-f]+",  Regexp::IGNORECASE|Regexp::EXTENDED), "00XaBCdefG", "0XaBCdef" ],
    [ Regexp.new("0x[^c-f]+", Regexp::IGNORECASE|Regexp::EXTENDED), "00XaBCdefG", "0XaB" ]
  ]

  patterns.all? { |reg, str, result|
	reg.match(str)[0] == result 
  }
`)
}

func TestRegexp_Extended(t *testing.T) {
	assert(t, "Regexp#match (extended)", `
	src = <<-END  
      ^             # match the beginning of the line
      (\\w+) (\\w+) # capture the first two words
END
	reg = Regexp.new(src, Regexp::EXTENDED|Regexp::MULTILINE)
	m = reg.match('foo bar baz')
    m[1] == 'foo' && m[2] == 'bar'
	`)
}

func TestRegexp_Extended2(t *testing.T) {
	assert(t, "Regexp#match (extended)", `
	src = <<-END  
		(?P<year>[0-9]{4})-?  # year
        (?P<month>[0-9]{2})-? # month
        (?P<day>[0-9]{2})     # day
	END
	reg = Regexp.new(src, Regexp::EXTENDED|Regexp::MULTILINE)
	m = reg.match('1984-06-20')
    m[:year] == '1984' && m[:month] == '06' && m[:day] == '20'
	`)
}
