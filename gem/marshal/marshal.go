package marshal

import (
	"bytes"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("marshal", initMarshal)
}

func initMarshal(mrb *oruby.MrbState) interface{} {
	mod := mrb.DefineModule("Marshal")
	mod.DefineClassMethod("dump", marshalDump, mrb.ArgsArg(1, 2))
	mod.DefineClassMethod("load", marshalLoad, mrb.ArgsArg(1, 2)+mrb.ArgsBlock())
	mod.DefineClassMethod("restore", marshalLoad, mrb.ArgsArg(1, 2)+mrb.ArgsBlock())
	// freeze: keyword (the Go loader takes positional arguments only)
	if _, err := mrb.Eval(loadFreezeRuby); err != nil {
		panic(err)
	}
	mod.Const("MAJOR_VERSION", 4)
	mod.Const("MINOR_VERSION", 8)
	return nil
}

func marshalDump(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	var output oruby.Value
	depth := defaultDepth
	if len(args) >= 2 {
		if args[1].Type() == oruby.MrbTTInteger {
			depth = int(args[1].Int())
		} else {
			output = args[1]
		}
	}
	if len(args) == 3 {
		depth = int(args[2].Int())
	}
	data, err := dumpValue(mrb, args[0], depth)
	if err != nil {
		return mrb.RaiseError(err)
	}
	value := mrb.BytesValue(data)
	if mrb.RespondTo(value, oruby.Sym_force_encoding) {
		value = mrb.Call(value, "force_encoding", "ASCII-8BIT")
	}
	if output.IsNil() {
		return value
	}
	if _, err := mrb.Funcall(output, oruby.Sym_write, value); err != nil {
		return mrb.RaiseError(err)
	}
	return output
}

func marshalLoad(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arguments, _ := mrb.GetArgsWithBlock()
	args := arguments.Slice()
	proc, freeze := oruby.Nil, false
	for _, a := range args[1:] {
		if a.IsHash() {
			freeze = mrb.HashFetch(a, oruby.Sym_freeze, mrb.FalseValue()).Bool()
		} else if !a.IsNil() {
			proc = a
		}
	}
	var source byteSource
	if args[0].Type() == oruby.MrbTTString {
		if rubyByteLen(args[0]) > maxInputBytes {
			return mrb.Raise(mrb.ERangeError(), "Marshal input exceeds size limit")
		}
		data := rubyBytes(args[0])
		source = bytes.NewReader(data)
	} else if mrb.RespondTo(args[0], oruby.Sym_read) {
		source = &rubyReader{mrb: mrb, object: args[0]}
	} else {
		return mrb.Raise(mrb.ETypeError(), "instance of IO needed")
	}
	value, err := loadValueWith(mrb, source, proc, freeze)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return value
}

const loadFreezeRuby = `
  module Marshal
    class << self
      alias __oruby_load load
      def load(source, proc = nil, freeze: false)
        v = __oruby_load(source, proc)
        freeze ? Marshal.__deep_freeze(v, {}) : v
      end
      alias restore load

      def __deep_freeze(v, seen)
        return v if Module === v || seen.key?(v.__id__)
        seen[v.__id__] = true
        case v
        when Array then v.each { |e| __deep_freeze(e, seen) }
        when Hash then v.each { |k, e| __deep_freeze(k, seen); __deep_freeze(e, seen) }
        end
        v.instance_variables.each { |iv| __deep_freeze(v.instance_variable_get(iv), seen) } unless String === v
        v.freeze
      end
    end
  end
`
