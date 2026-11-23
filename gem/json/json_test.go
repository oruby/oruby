package json

import (
	"testing"

	"github.com/oruby/oruby"
)

func assert(t *testing.T, desc, code string) {
	t.Helper()

	mrb, _ := oruby.New()
	defer mrb.Close()

	result, err := mrb.Eval(code)
	if err != nil {
		t.Fatalf(desc, ": ", err)
	}

	if !oruby.MrbBoolean(result) {
		t.Error(desc, ": result = ", mrb.Intf(result))
	}
}

func TestGenerate(t *testing.T) {
	assert(t, "stringify nil", `JSON::stringify(nil) == "null"`)
	assert(t, "stringify boolean", `JSON::stringify(true) == "true"`)
	assert(t, "stringify symbol", `JSON::stringify(:symbol) == "\"symbol\""`)
	assert(t, "strnigify object with numeric value", `JSON::stringify({"foo"=>"bar"}) == '{"foo":"bar"}'`)
	assert(t, "strnigify object with string value", `JSON::stringify({"foo"=> 1}) == '{"foo":1}'`)
	assert(t, "stringify object with float value", `JSON::stringify({"foo"=> 2.3}) == '{"foo":2.3}'`)
	assert(t, "stringify object with nil value", `JSON::stringify({"foo"=> nil}) == '{"foo":null}'`)
	assert(t, "stringify object with boolean key and float value", `JSON::stringify({true=> 3.4}) == '{"true":3.4}'`)
	assert(t, "stringify object with object key and float value", `JSON::stringify({{"foo"=> "bar"}=> 1.2}) == '{"{\"foo\" =\u003e \"bar\"}":1.2}'`)
	assert(t, "stringify empty array", `JSON::stringify([]) == "[]"`)
	assert(t, "strnigify array with few elements", `JSON::stringify([1,true,"foo"]) == "[1,true,\"foo\"]"`)
	assert(t, "stringify object with several keys", `JSON::stringify({"foo"=>1, "bar"=> 2}) == '{"bar":2,"foo":1}'`)
	assert(t, "stringify multi-byte", `JSON::stringify({"foo"=>"ふー", "bar"=> "ばー"}) == '{"bar":"ばー","foo":"ふー"}'`)
}

func TestParse(t *testing.T) {
	assert(t, "parse object", `JSON::parse('{"foo": "bar"}') == {"foo"=>"bar"}`)
	assert(t, "parse null", `JSON::parse('{"foo": null}') == {"foo"=>nil}`)
	assert(t, "parse array", `JSON::parse('[true, "foo"]')[1] == "foo"`)
	assert(t, "parse numbers", `JSON::parse('[1, 2, 3]') == [1, 2, 3]`)
	assert(t, "parse nested", `JSON::parse('{"a": {"b": [1, false]}}')["a"]["b"][0] == 1`)
	assert(t, "parse symbolize_names", `JSON.parse('{"a": {"b": 1}}', symbolize_names: true) == {a: {b: 1}}`)
	assert(t, "parse raises ParserError", `begin; JSON.parse("{bad"); false; rescue JSON::ParserError; true; end`)
}

func TestStateAndStdlibCompat(t *testing.T) {
	assert(t, "JSON::State and Ext alias", `
		s = JSON::State.new(indent: "  ")
		s.is_a?(JSON::State) &&
		JSON::Ext::Generator::State == JSON::State &&
		s.generate({"a" => 1}).include?("\n") &&
		JSON.generate(JSON::Fragment.new('{"raw":1}')) == '{"raw":1}' &&
		JSON.load('{"k":2}') == {"k" => 2} &&
		JSON.dump({"k" => 2}) == '{"k":2}' &&
		({"x" => 1}.to_json == '{"x":1}')
	`)
}
