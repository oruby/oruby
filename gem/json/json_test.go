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
	assert(t, "stringify object with object key and float value", `JSON::stringify({{"foo"=> "bar"}=> 1.2}) == '{"{\"foo\"=\u003e\"bar\"}":1.2}'`)
	assert(t, "stringify empty array", `JSON::stringify([]) == "[]"`)
	assert(t, "strnigify array with few elements", `JSON::stringify([1,true,"foo"]) == "[1,true,\"foo\"]"`)
	assert(t, "stringify object with several keys", `JSON::stringify({"foo"=>1, "bar"=> 2}) == '{"bar":2,"foo":1}'`)
	assert(t, "stringify multi-byte", `JSON::stringify({"foo"=>"ふー", "bar"=> "ばー"}) == '{"bar":"ばー","foo":"ふー"}'`)
}

func TestParse(t *testing.T) {
	assert(t, "parse object", `JSON::parse('{"foo": "bar"}') == {"foo"=>"bar"}`)
	assert(t, "parse null", `JSON::parse('{"foo": null}') == {"foo"=>nil}`)
	assert(t, "parse array", `JSON::parse('[true, "foo"]')[1] == "foo"`)
}
