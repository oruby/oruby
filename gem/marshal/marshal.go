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
	mod.DefineClassMethod("load", marshalLoad, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	mod.DefineClassMethod("restore", marshalLoad, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
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
	if mrb.RespondTo(value, mrb.Intern("force_encoding")) {
		value = mrb.Call(value, "force_encoding", "ASCII-8BIT")
	}
	if output.IsNil() {
		return value
	}
	if _, err := mrb.Funcall(output, mrb.Intern("write"), value); err != nil {
		return mrb.RaiseError(err)
	}
	return output
}

func marshalLoad(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arguments, block := mrb.GetArgsWithBlock()
	if !block.IsNil() {
		return mrb.Raise(mrb.ExcGet("NotImplementedError"), "Marshal.load proc callbacks are not implemented")
	}
	args := arguments.Slice()
	if len(args) > 1 && !args[1].IsNil() {
		return mrb.Raise(mrb.ExcGet("NotImplementedError"), "Marshal.load proc callbacks are not implemented")
	}
	var source byteSource
	if args[0].Type() == oruby.MrbTTString {
		data := rubyBytes(args[0])
		if len(data) > maxInputBytes {
			return mrb.Raise(mrb.ERangeError(), "Marshal input exceeds size limit")
		}
		source = bytes.NewReader(data)
	} else if mrb.RespondTo(args[0], mrb.Intern("read")) {
		source = &rubyReader{mrb: mrb, object: args[0]}
	} else {
		return mrb.Raise(mrb.ETypeError(), "instance of IO needed")
	}
	value, err := loadValue(mrb, source)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return value
}
