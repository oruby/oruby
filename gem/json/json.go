package json

import (
	"encoding/json"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("json", func(mrb *oruby.MrbState) interface{} {
		jsonModule := mrb.DefineModule("JSON")

		jsonModule.DefineClassMethod("stringify", mrbJSONStringify, mrb.ArgsArg(1, 1))
		jsonModule.DefineClassMethod("generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))              // generate(obj, opts = nil)
		jsonModule.DefineClassMethod("pretty_generate", mrbJSONPrettyGenerate, mrb.ArgsArg(1, 1)) // pretty_generate(obj, opts = nil)
		jsonModule.DefineClassMethod("fast_generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))         // fast_generate(obj, opts = nil)

		// indent: a string used to indent levels (default: "),
		// space: a string that is put after, a : or , delimiter (default: "),
		// space_before: a string that is put before a : pair delimiter (default: "),
		// object_nl: a string that is put at the end of a JSON object (default: "),
		// array_nl: a string that is put at the end of a JSON array (default: "),
		// allow_nan: true if NaN, Infinity, and -Infinity should be generated, otherwise an exception is thrown if these values are encountered. This options defaults to false.
		// max_nesting: The maximum depth of nesting allowed in the data structures from which JSON is to be generated. Disable depth checking with :max_nesting => false, it defaults to 100.

		//mrb.Define_class_func(jsonModule,  "load", mrb_json_stringify, ARGS_REQ(1)); // load(source, proc = nil, options = {})
		jsonModule.DefineClassMethod("parse", mrbJSONParse, mrb.ArgsArg(1, 1))  // parse(source, opts = {})
		jsonModule.DefineClassMethod("parse!", mrbJSONParse, mrb.ArgsArg(1, 1)) // parse(source, opts = {})

		// max_nesting:      The maximum depth of nesting allowed in the parsed data structures. Disable depth checking with :max_nesting => false. It defaults to 100.
		// allow_nan:        If set to true, allow NaN, Infinity and -Infinity in defiance of RFC 4627 to be parsed by the Parser. This option defaults to false.
		// symbolize_names:  If set to true, returns symbols for the names (keys) in a JSON object. Otherwise strings are returned. Strings are the default.
		// create_additions: If set to false, the Parser doesn't create additions even if a matching class and ::create_id was found. This option defaults to true.
		// object_class:     Defaults to Hash
		// array_class:      Defaults to Array
		// max_nesting: The maximum depth of nesting allowed in the parsed data structures. Enable depth checking with :max_nesting => anInteger. The parse! methods defaults to not doing max depth checking: This can be dangerous if someone wants to fill up your stack.
		// allow_nan: If set to true, allow NaN, Infinity, and -Infinity in defiance of RFC 4627 to be parsed by the Parser. This option defaults to true.
		// create_additions: If set to false, the Parser doesn't create additions even if a matching class and ::create_id was found. This option defaults to true.
		return nil
	})
}

func mrbJSONStringify(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrbJSONGenerate(mrb, self)
}

func scan(mrb *oruby.MrbState, v oruby.MrbValue, in interface{}) (interface{}, error) {
	if err := mrb.Scan(v, in); err != nil {
		return nil, err
	}
	return in, nil
}

func scanValue(mrb *oruby.MrbState, v oruby.MrbValue) (interface{}, error) {
	if v.IsNil() {
		return nil, nil
	} else if v.Type() == oruby.MrbTTFalse || v.Type() == oruby.MrbTTTrue {
		in := false
		return scan(mrb, v, &in)
	} else if v.Type() == oruby.MrbTTUndef || v.Type() == oruby.MrbTTFree {
		var in interface{}
		return scan(mrb, v, &in)
	} else if v.Type() == oruby.MrbTTSymbol {
		in := ""
		v2 := mrb.SymStr(mrb.ObjToSym(v))
		return scan(mrb, v2, &in)
	} else if v.Type() == oruby.MrbTTArray {
		var in interface{}
		return scan(mrb, v, &in)
	} else if v.Type() < oruby.MrbTTHasBasic {
		in := ""
		return scan(mrb, v, &in)
	} else if v.Type() == oruby.MrbTTHash {
		in := make(map[string]interface{})
		keys := mrb.HashKeys(v)
		kcnt := oruby.RArrayLen(keys)
		for i := 0; i < kcnt; i++ {
			key := mrb.AryRef(keys, i)
			val := mrb.HashGet(v, key)
			if key.Type() != oruby.MrbTTString {
				in[mrb.StringCstr(mrb.ObjAsString(key))] = mrb.Intf(val)
			} else {
				in[mrb.StringCstr(key)] = mrb.Intf(val)
			}
		}
		return in, nil
	}

	in := make(map[string]interface{})
	return scan(mrb, v, in)
}

func mrbJSONGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()

	in, err := scanValue(mrb, v)
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate scan - %v", err)
	}
	ret, err := json.Marshal(in)
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
	ret, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::generate - %v", err)
	}

	return mrb.Value(string(ret))
}

func mrbJSONParse(mrb *oruby.MrbState, salf oruby.Value) oruby.MrbValue {
	var out interface{}
	var in string
	var ok bool

	if in, ok = mrb.Intf(mrb.GetArgsFirst()).(string); !ok {
		return mrb.Raise(mrb.EArgumentError(), "JSON::parse - string argument expected")
	}

	if err := json.Unmarshal(([]byte)(in), &out); err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "JSON::parse - %v", err)
	}

	return mrb.Value(out)
}
