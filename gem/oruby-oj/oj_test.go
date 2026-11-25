package oj_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/oruby-oj"
)

func TestOj(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "oj"

# Round-trip load/dump
obj = { "a" => 1, "b" => [true, false, nil, 3.5, "hi\n\"\\"] }
s = Oj.dump(obj)
raise "roundtrip" unless Oj.load(s) == obj

# symbol_keys & pretty indent
sym_obj = Oj.load('{"foo": 42, "nested": {"bar": "ok"}}', symbol_keys: true)
raise "symbol_keys" unless sym_obj[:foo] == 42 && sym_obj[:nested][:bar] == "ok"
pretty = Oj.dump({ a: [1, 2] }, indent: 2)
raise "indent" unless pretty.include?("\n  ")

# Escape modes (rails/xss_safe & ascii)
xss = Oj.dump("<script>&</script>", mode: :rails)
raise "xss_safe=#{xss}" unless xss == '"\u003cscript\u003e\u0026\u003c/script\u003e"'

# Strict mode rejects symbols
raised = false
begin
  Oj.dump(:sym, mode: :strict)
rescue TypeError
  raised = true
end
raise "expected strict TypeError" unless raised

# Multi-document streaming (NDJSON)
docs = []
Oj.load("{\"n\":1}\n{\"n\":2}\n{\"n\":3}") { |d| docs << d["n"] }
raise "stream docs=#{docs.inspect}" unless docs == [1, 2, 3]

# Unicode surrogate pairs
raise "emoji" unless Oj.load('"\uD83D\uDE00"') == "\u{1F600}"

# ParseError on malformed JSON
perr = false
begin
  Oj.load("{bad json")
rescue Oj::ParseError
  perr = true
end
raise "expected Oj::ParseError" unless perr

# Custom as_json
class CustomObj
  def as_json; { "kind" => "custom", "v" => 7 }; end
end
raise "as_json" unless Oj.dump(CustomObj.new, mode: :rails) == '{"kind":"custom","v":7}'

# default_options
Oj.default_options = { symbol_keys: true }
raise "default_options" unless Oj.load('{"k":1}')[:k] == 1
Oj.default_options = { symbol_keys: false }
`)
	if err != nil {
		t.Fatalf("oj test failed: %v", err)
	}
}
