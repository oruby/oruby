package json

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("json", func(mrb *oruby.MrbState) interface{} {
		jsonModule := mrb.DefineModule("JSON")

		jsonModule.DefineClassMethod("stringify", mrbJSONStringify, mrb.ArgsArg(1, 1))
		jsonModule.DefineClassMethod("generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))              // generate(obj, opts = nil)
		jsonModule.DefineClassMethod("pretty_generate", mrbJSONPrettyGenerate, mrb.ArgsArg(1, 1)) // pretty_generate(obj, opts = nil)
		jsonModule.DefineClassMethod("fast_generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))         // fast_generate(obj, opts = nil)

		jsonModule.DefineClassMethod("parse", mrbJSONParse, mrb.ArgsArg(1, 1))  // parse(source, opts = {})
		jsonModule.DefineClassMethod("parse!", mrbJSONParse, mrb.ArgsArg(1, 1)) // parse(source, opts = {})
		return nil
	})
}

func mrbJSONStringify(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrbJSONGenerate(mrb, self)
}

func scanValue(mrb *oruby.MrbState, v oruby.MrbValue) (interface{}, error) {
	if v.IsNil() {
		return nil, nil
	}
	val := v.Value()
	switch val.Type() {
	case oruby.MrbTTFalse:
		return false, nil
	case oruby.MrbTTTrue:
		return true, nil
	case oruby.MrbTTInteger:
		return val.Int(), nil
	case oruby.MrbTTFloat:
		return val.Float64(), nil
	case oruby.MrbTTSymbol:
		return mrb.SymName(val.Symbol()), nil
	case oruby.MrbTTString:
		return val.String(), nil
	case oruby.MrbTTArray:
		cnt := oruby.RArrayLen(val)
		arr := make([]interface{}, cnt)
		for i := 0; i < cnt; i++ {
			elem := mrb.AryRef(val, i)
			elemVal, err := scanValue(mrb, elem)
			if err != nil {
				return nil, err
			}
			arr[i] = elemVal
		}
		return arr, nil
	case oruby.MrbTTHash:
		in := make(map[string]interface{})
		keys := mrb.HashKeys(val)
		kcnt := oruby.RArrayLen(keys)
		for i := 0; i < kcnt; i++ {
			key := mrb.AryRef(keys, i)
			hashVal := mrb.HashGet(val, key)
			vVal, err := scanValue(mrb, hashVal)
			if err != nil {
				return nil, err
			}
			var keyStr string
			if key.Type() == oruby.MrbTTString {
				keyStr = key.String()
			} else if key.Type() == oruby.MrbTTSymbol {
				keyStr = mrb.SymName(key.Symbol())
			} else {
				keyStr = mrb.ObjAsString(key).String()
			}
			in[keyStr] = vVal
		}
		return in, nil
	default:
		return mrb.Intf(v), nil
	}
}

func mrbJSONGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()

	in, err := scanValue(mrb, v)
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate scan - %v", err)
	}
	ret, err := json.Marshal(in, json.Deterministic(true), jsontext.EscapeForHTML(true))
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate - %v", err)
	}

	return mrb.Value(string(ret))
}

func mrbJSONPrettyGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()

	in, err := scanValue(mrb, v)
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate scan - %v", err)
	}
	ret, err := json.Marshal(in, json.Deterministic(true), jsontext.EscapeForHTML(true), jsontext.WithIndent("  "))
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate - %v", err)
	}

	return mrb.Value(string(ret))
}

func goToValue(mrb *oruby.MrbState, v interface{}) oruby.Value {
	if v == nil {
		return mrb.NilValue()
	}
	switch val := v.(type) {
	case bool:
		return mrb.BoolValue(val)
	case int:
		return mrb.FixnumValue(val)
	case int64:
		return mrb.FixnumValue(int(val))
	case float64:
		return mrb.FloatValue(val)
	case string:
		return mrb.StrNew(val)
	case []interface{}:
		arr := mrb.AryNewCapa(len(val))
		for _, item := range val {
			mrb.AryPush(arr, goToValue(mrb, item))
		}
		return arr.Value()
	case map[string]interface{}:
		h := mrb.HashNewCapa(len(val))
		for k, v := range val {
			mrb.HashSet(h, mrb.StrNew(k), goToValue(mrb, v))
		}
		return h.Value()
	default:
		return mrb.Value(v)
	}
}

func mrbJSONParse(mrb *oruby.MrbState, salf oruby.Value) oruby.MrbValue {
	var out interface{}
	var in string
	var ok bool

	if in, ok = mrb.Intf(mrb.GetArgsFirst()).(string); !ok {
		return mrb.Raise(mrb.EArgumentError(), "JSON::parse - string argument expected")
	}

	if err := json.Unmarshal([]byte(in), &out); err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::parse - %v", err)
	}

	return goToValue(mrb, out)
}
