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
		s = JSON::State.new(indent: "  ", object_nl: "\n")
		s.is_a?(JSON::State) &&
		JSON::Ext::Generator::State == JSON::State &&
		s.generate({"a" => 1}).include?("\n") &&
		JSON.generate(JSON::Fragment.new('{"raw":1}')) == '{"raw":1}' &&
		JSON.load('{"k":2}') == {"k" => 2} &&
		JSON.dump({"k" => 2}) == '{"k":2}' &&
		({"x" => 1}.to_json == '{"x":1}')
	`)
}

func TestJSONNestingAndAllowNaN(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		# Self-referential array must raise JSON::NestingError, not stack-overflow
		a = []
		a << a
		raised = false
		begin
			JSON.generate(a)
		rescue JSON::NestingError
			raised = true
		end
		raise "expected NestingError for circular array" unless raised

		# Deep nesting respects max_nesting
		nested = [[[]]]
		raised2 = false
		begin
			JSON.generate(nested, max_nesting: 2)
		rescue JSON::NestingError
			raised2 = true
		end
		raise "expected NestingError for max_nesting: 2" unless raised2
		raise "expected ok for max_nesting: 3" unless JSON.generate(nested, max_nesting: 3) == "[[[]]]"

		# Deep JSON parse nesting
		raised3 = false
		begin
			JSON.parse("[[[]]]", max_nesting: 2)
		rescue JSON::NestingError
			raised3 = true
		end
		raise "expected parse NestingError" unless raised3

		# NaN / Infinity rejected unless allow_nan: true
		nan_err = false
		begin
			JSON.generate([JSON::NaN])
		rescue JSON::GeneratorError
			nan_err = true
		end
		raise "expected GeneratorError for NaN" unless nan_err
		raise "allow_nan" unless JSON.generate([JSON::NaN, JSON::Infinity, JSON::MinusInfinity], allow_nan: true) == "[NaN,Infinity,-Infinity]"

		# Hash key whose #to_s raises must not longjmp/crash Go
		bad_key = Object.new
		def bad_key.to_s; raise TypeError, "boom"; end
		key_err = false
		begin
			JSON.generate({bad_key => 1})
		rescue TypeError, JSON::GeneratorError
			key_err = true
		end
		raise "expected error for bad key to_s" unless key_err
	`)
	if err != nil {
		t.Fatalf("JSON nesting/NaN test failed: %v", err)
	}
}
