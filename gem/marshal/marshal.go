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
	mod.DefineClassMethod("__oruby_load", marshalLoad, mrb.ArgsArg(1, 2))
	if _, err := mrb.Eval(marshalRuby); err != nil {
		panic(err)
	}
	mod.Const("MAJOR_VERSION", 4)
	mod.Const("MINOR_VERSION", 8)
	return nil
}

// Marshal.dump(obj[, io][, limit]): a negative limit (the default) is none
func marshalDump(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	var output oruby.Value
	limit := -1
	switch len(args) {
	case 2:
		if args[1].Type() == oruby.MrbTTInteger {
			limit = int(args[1].Int())
		} else {
			output = args[1]
		}
	case 3:
		output = args[1]
		limit = int(args[2].Int())
	}
	if !output.IsNil() {
		if !mrb.RespondTo(output, oruby.Sym_write) {
			return mrb.Raise(mrb.ETypeError(), "instance of IO needed")
		}
		if mrb.RespondTo(output, oruby.Sym_binmode) {
			if _, err := mrb.Funcall(output, oruby.Sym_binmode); err != nil {
				return mrb.RaiseError(err)
			}
		}
	}
	data, err := dumpValue(mrb, args[0], limit)
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

// Marshal.__oruby_load(source, proc, freeze): a String (or #to_str), or an
// IO that has getbyte and read
func marshalLoad(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	src, proc, freeze := args[0], oruby.Nil, false
	if len(args) > 1 {
		proc = args[1]
	}
	if len(args) > 2 {
		freeze = args[2].Bool()
	}
	if src.Type() != oruby.MrbTTString && mrb.RespondTo(src, oruby.Sym_to_str) {
		s, err := mrb.Funcall(src, oruby.Sym_to_str)
		if err != nil {
			return mrb.RaiseError(err)
		}
		src = s
	}
	var source byteSource
	fromIO := false
	switch {
	case src.Type() == oruby.MrbTTString:
		if rubyByteLen(src) > maxInputBytes {
			return mrb.Raise(mrb.ERangeError(), "Marshal input exceeds size limit")
		}
		source = bytes.NewReader(rubyBytes(src))
	case mrb.RespondTo(src, oruby.Sym_getbyte) && mrb.RespondTo(src, oruby.Sym_read):
		if mrb.RespondTo(src, oruby.Sym_binmode) {
			if _, err := mrb.Funcall(src, oruby.Sym_binmode); err != nil {
				return mrb.RaiseError(err)
			}
		}
		source, fromIO = &rubyReader{mrb: mrb, object: src}, true
	default:
		return mrb.Raise(mrb.ETypeError(), "instance of IO needed")
	}
	value, err := loadValueWith(mrb, source, proc, freeze, fromIO)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return value
}

const marshalRuby = `
  module Marshal
    class << self
      def load(source, proc = nil, freeze: false)
        __oruby_load(source, proc, freeze)
      end
      alias restore load

      # a Struct or Data filled as MRI's rb_struct_initialize does: with its
      # base initialize, not one the class defines
      def __oruby_init_struct(obj, values)
        if defined?(::Data) && ::Data === obj
          ::Data.instance_method(:initialize).bind_call(obj, **obj.class.members.zip(values).to_h)
        elsif obj.class.respond_to?(:keyword_init?) && obj.class.keyword_init?
          ::Struct.instance_method(:initialize).bind_call(obj, **obj.class.members.zip(values).to_h)
        else
          ::Struct.instance_method(:initialize).bind_call(obj, *values)
        end
        obj
      end

      def __oruby_new_regexp(klass, source, options)
        obj = klass.allocate
        ::Regexp.instance_method(:initialize).bind_call(obj, source, options)
        obj
      end
    end
  end
`
