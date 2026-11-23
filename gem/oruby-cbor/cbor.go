// Package orubycbor implements the Ruby cbor gem backed by
// github.com/fxamacker/cbor/v2 (pure Go), supporting WebAuthn/COSE
// integer-keyed maps, binary vs UTF-8 strings, and CBOR::Tagged values.
// Registered as "oruby-cbor" and aliased to "cbor".
package orubycbor

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	oruby.LazyGem("oruby-cbor", initCBOR)
	oruby.GemAlias("cbor", "oruby-cbor")
}

func isBinaryRubyString(mrb *oruby.MrbState, v oruby.Value) bool {
	if !v.IsString() {
		return false
	}
	if enc, err := mrb.Funcall(v, mrb.Intern("encoding")); err == nil && !enc.IsNil() {
		if name, err := mrb.Funcall(enc, mrb.Intern("name")); err == nil && name.IsString() {
			n := name.String()
			return n == "ASCII-8BIT" || n == "BINARY"
		}
	}
	return false
}

func rubyToGoCBOR(mrb *oruby.MrbState, v oruby.Value, taggedCls, simpleCls oruby.RClass) (any, error) {
	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			return nil, nil
		}
		return false, nil
	case oruby.MrbTTTrue:
		return true, nil
	case oruby.MrbTTInteger:
		return v.Int64(), nil
	case oruby.MrbTTFloat:
		return v.Float64(), nil
	case oruby.MrbTTString:
		if isBinaryRubyString(mrb, v) {
			b := v.Bytes()
			cp := make([]byte, len(b))
			copy(cp, b)
			return cp, nil
		}
		return v.String(), nil
	case oruby.MrbTTSymbol:
		return mrb.SymName(v.Symbol()), nil
	case oruby.MrbTTArray:
		n := oruby.RArrayLen(v)
		out := make([]any, n)
		for i := 0; i < n; i++ {
			elem, err := rubyToGoCBOR(mrb, mrb.AryRef(v, i), taggedCls, simpleCls)
			if err != nil {
				return nil, err
			}
			out[i] = elem
		}
		return out, nil
	case oruby.MrbTTHash:
		keys := mrb.HashKeys(v)
		n := oruby.RArrayLen(keys)
		out := make(map[any]any, n)
		for i := 0; i < n; i++ {
			k := mrb.AryRef(keys, i)
			val, err := mrb.Funcall(v, mrb.Intern("[]"), k)
			if err != nil {
				return nil, err
			}
			gk, err := rubyToGoCBOR(mrb, k, taggedCls, simpleCls)
			if err != nil {
				return nil, err
			}
			gv, err := rubyToGoCBOR(mrb, val, taggedCls, simpleCls)
			if err != nil {
				return nil, err
			}
			out[gk] = gv
		}
		return out, nil
	default:
		if isA, err := mrb.Funcall(v, mrb.Intern("is_a?"), taggedCls.Value()); err == nil && isA.Bool() {
			tagV, err := mrb.Funcall(v, mrb.Intern("tag"))
			if err != nil {
				return nil, err
			}
			valV, err := mrb.Funcall(v, mrb.Intern("value"))
			if err != nil {
				return nil, err
			}
			content, err := rubyToGoCBOR(mrb, valV, taggedCls, simpleCls)
			if err != nil {
				return nil, err
			}
			return cbor.Tag{Number: uint64(tagV.Int64()), Content: content}, nil
		}
		if isA, err := mrb.Funcall(v, mrb.Intern("is_a?"), simpleCls.Value()); err == nil && isA.Bool() {
			valV, err := mrb.Funcall(v, mrb.Intern("value"))
			if err != nil {
				return nil, err
			}
			return cbor.SimpleValue(valV.Int64()), nil
		}
		return nil, fmt.Errorf("cannot encode %s as CBOR", mrb.ClassOf(v).Name())
	}
}

func goCBORToRuby(mrb *oruby.MrbState, g any, taggedCls, simpleCls oruby.RClass) (oruby.Value, error) {
	switch x := g.(type) {
	case nil:
		return mrb.NilValue(), nil
	case bool:
		return mrb.BoolValue(x), nil
	case int64:
		return mrb.Int64Value(x), nil
	case uint64:
		if x <= (1<<63 - 1) {
			return mrb.Int64Value(int64(x)), nil
		}
		return mrb.Uint64Value(x), nil
	case float32:
		return mrb.FloatValue(float64(x)), nil
	case float64:
		return mrb.FloatValue(x), nil
	case string:
		return mrb.StrNew(x), nil
	case []byte:
		return mrb.Call(mrb.BytesValue(x), "force_encoding", mrb.StrNew("ASCII-8BIT")), nil
	case []any:
		ary := mrb.AryNewCapa(len(x))
		for _, item := range x {
			rv, err := goCBORToRuby(mrb, item, taggedCls, simpleCls)
			if err != nil {
				return mrb.NilValue(), err
			}
			mrb.AryPush(ary, rv)
		}
		return ary.Value(), nil
	case map[any]any:
		h := mrb.HashNewCapa(len(x))
		for k, v := range x {
			rk, err := goCBORToRuby(mrb, k, taggedCls, simpleCls)
			if err != nil {
				return mrb.NilValue(), err
			}
			rv, err := goCBORToRuby(mrb, v, taggedCls, simpleCls)
			if err != nil {
				return mrb.NilValue(), err
			}
			if _, err := mrb.Funcall(h, mrb.Intern("[]="), rk, rv); err != nil {
				return mrb.NilValue(), err
			}
		}
		return h.Value(), nil
	case cbor.Tag:
		inner, err := goCBORToRuby(mrb, x.Content, taggedCls, simpleCls)
		if err != nil {
			return mrb.NilValue(), err
		}
		return mrb.Funcall(taggedCls, mrb.Intern("new"), mrb.Uint64Value(x.Number), inner)
	case time.Time:
		return mrb.Funcall(taggedCls, mrb.Intern("new"), mrb.FixnumValue(1), mrb.Int64Value(x.Unix()))
	case *big.Int:
		if x.IsInt64() {
			return mrb.Int64Value(x.Int64()), nil
		}
		return mrb.Call(mrb.StrNew(x.String()), "to_i"), nil
	case cbor.SimpleValue:
		return mrb.Funcall(simpleCls, mrb.Intern("new"), mrb.FixnumValue(int(x)))
	default:
		return mrb.StrNew(fmt.Sprint(x)), nil
	}
}

func initCBOR(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("CBOR")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("0.5.9.8"))
	stdErr := mrb.ClassGet("StandardError")
	malformedErr := mrb.DefineClassUnder(mod, "MalformedFormatError", stdErr)
	mrb.DefineClassUnder(mod, "StackError", stdErr)
	typeErr := mrb.DefineClassUnder(mod, "TypeError", mrb.ClassGet("TypeError"))
	taggedCls := mrb.DefineClassUnder(mod, "Tagged", mrb.ObjectClass())
	simpleCls := mrb.DefineClassUnder(mod, "Simple", mrb.ObjectClass())

	decMode, _ := cbor.DecOptions{
		DefaultMapType: nil,
	}.DecMode()

	encodeFn := func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		gv, err := rubyToGoCBOR(mrb, mrb.GetArgsFirst(), taggedCls, simpleCls)
		if err != nil {
			return mrb.Raise(typeErr, err.Error())
		}
		out, err := cbor.Marshal(gv)
		if err != nil {
			return mrb.Raise(malformedErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}
	mod.DefineClassMethod("encode", encodeFn, mrb.ArgsReq(1))
	mod.DefineClassMethod("pack", encodeFn, mrb.ArgsReq(1))

	decodeFn := func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		raw, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		var gv any
		if err := decMode.Unmarshal(raw, &gv); err != nil {
			return mrb.Raise(malformedErr, err.Error())
		}
		rv, err := goCBORToRuby(mrb, gv, taggedCls, simpleCls)
		if err != nil {
			return mrb.Raise(malformedErr, err.Error())
		}
		return rv
	}
	mod.DefineClassMethod("decode", decodeFn, mrb.ArgsReq(1))
	mod.DefineClassMethod("unpack", decodeFn, mrb.ArgsReq(1))

	mod.DefineClassMethod("__decode_stream", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		raw, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		dec := decMode.NewDecoder(bytes.NewReader(raw))
		ary := mrb.AryNew()
		for {
			var gv any
			err := dec.Decode(&gv)
			if err == io.EOF {
				break
			}
			if err != nil {
				return mrb.Raise(malformedErr, err.Error())
			}
			rv, err := goCBORToRuby(mrb, gv, taggedCls, simpleCls)
			if err != nil {
				return mrb.Raise(malformedErr, err.Error())
			}
			mrb.AryPush(ary, rv)
		}
		return ary
	}, mrb.ArgsReq(1))

	rb.LoadRuby(mrb, "<internal:oruby-cbor>", []byte(`
module CBOR
  class Tagged
    attr_reader :tag, :value

    def initialize(tag, value)
      @tag = tag.to_i
      @value = value
    end

    def ==(other)
      other.is_a?(Tagged) && @tag == other.tag && @value == other.value
    end

    def to_cbor
      CBOR.encode(self)
    end
  end

  class Simple
    attr_reader :value

    def initialize(value)
      @value = value.to_i
    end

    def ==(other)
      other.is_a?(Simple) && @value == other.value
    end

    def to_cbor
      CBOR.encode(self)
    end
  end

  class Packer
    def initialize(io = nil)
      @io = io
      @buf = +"".b
    end

    def write(obj)
      @buf << CBOR.encode(obj)
      self
    end
    alias pack write

    def flush
      if @io
        @io.write(@buf)
        @buf = +"".b
      end
      self
    end

    def to_s
      @buf.dup
    end
  end

  class Unpacker
    def initialize(io = nil)
      @io = io
      @buf = +"".b
    end

    def feed(data)
      @buf << data.to_s.b
      self
    end

    def each(&block)
      return to_enum(:each) unless block_given?
      src = @io ? @io.read.to_s.b : @buf
      CBOR.__decode_stream(src).each(&block)
    end

    def read
      src = @io ? @io.read.to_s.b : @buf
      CBOR.decode(src)
    end
  end
end

class Object
  def to_cbor
    CBOR.encode(self)
  end
end
`))
	return nil
}
