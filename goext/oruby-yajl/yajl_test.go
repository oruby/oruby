package orubyyajl_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/oruby-yajl"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestYajl(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "yajl"
require "stringio"

# Basic parse & encode
obj = Yajl::Parser.parse('{"a":1,"b":[true,false,null,3.5,"hi"]}')
raise "parse" unless obj == { "a" => 1, "b" => [true, false, nil, 3.5, "hi"] }

sym = Yajl::Parser.parse('{"x":42,"nested":{"y":"ok"}}', symbolize_keys: true)
raise "sym" unless sym[:x] == 42 && sym[:nested][:y] == "ok"

# Multi-object JSON stream
streamed = []
Yajl::Parser.parse("{\"id\":1}\n{\"id\":2}\n{\"id\":3}") { |o| streamed << o["id"] }
raise "stream: #{streamed}" unless streamed == [1, 2, 3]

# Incremental << feeding
chunks = []
p = Yajl::Parser.new
p.on_parse_complete = ->(o) { chunks << o }
p << '{"k":'
p << '"val"}'
raise "chunks: #{chunks}" unless chunks == [{ "k" => "val" }]

# Encoder options: pretty, indent, html_safe, IO output
pretty = Yajl::Encoder.encode({ "a" => [1, 2] }, pretty: true, indent: "  ")
raise "pretty: #{pretty}" unless pretty.include?("\n  ")

safe = Yajl::Encoder.encode("<script>&</script>", html_safe: true)
raise "html_safe: #{safe}" unless safe == '"\u003cscript\u003e\u0026\u003c/script\u003e"'

io = StringIO.new
Yajl::Encoder.encode([1, 2, 3], io)
raise "io: #{io.string}" unless io.string == "[1,2,3]"
`)
	if err != nil {
		t.Fatalf("yajl test failed: %v", err)
	}
}
