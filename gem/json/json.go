package json

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"strconv"
	"unsafe"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("json", func(mrb *oruby.MrbState) interface{} {
		jsonModule := mrb.DefineModule("JSON")
		mrb.ConstSet(jsonModule, mrb.Intern("VERSION"), mrb.StrNew("2.10.2"))

		jsonErr := mrb.DefineClassUnder(jsonModule, "JSONError", mrb.ExcGet("StandardError"))
		mrb.DefineClassUnder(jsonModule, "ParserError", jsonErr)
		genErr := mrb.DefineClassUnder(jsonModule, "GeneratorError", jsonErr)
		mrb.DefineClassUnder(jsonModule, "NestingError", genErr)

		jsonModule.DefineClassMethod("stringify", mrbJSONStringify, mrb.ArgsArg(1, 1))
		jsonModule.DefineClassMethod("generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))              // generate(obj, opts = nil)
		jsonModule.DefineClassMethod("unparse", mrbJSONGenerate, mrb.ArgsArg(1, 1))               // unparse(obj, opts = nil)
		jsonModule.DefineClassMethod("pretty_generate", mrbJSONPrettyGenerate, mrb.ArgsArg(1, 1)) // pretty_generate(obj, opts = nil)
		jsonModule.DefineClassMethod("pretty_unparse", mrbJSONPrettyGenerate, mrb.ArgsArg(1, 1))  // pretty_unparse(obj, opts = nil)
		jsonModule.DefineClassMethod("fast_generate", mrbJSONGenerate, mrb.ArgsArg(1, 1))         // fast_generate(obj, opts = nil)
		jsonModule.DefineClassMethod("fast_unparse", mrbJSONGenerate, mrb.ArgsArg(1, 1))          // fast_unparse(obj, opts = nil)

		jsonModule.DefineClassMethod("parse", mrbJSONParse, mrb.ArgsArg(1, 1))  // parse(source, opts = {})
		jsonModule.DefineClassMethod("parse!", mrbJSONParse, mrb.ArgsArg(1, 1)) // parse!(source, opts = {})

		_, _ = mrb.Eval(`
module JSON
  class State
    def self.from_state(opts)
      case opts
      when self then opts
      when Hash then new(opts)
      else new
      end
    end

    def initialize(opts = nil)
      @opts = opts.is_a?(Hash) ? opts.dup : {}
    end

    def configure(opts)
      @opts.merge!(opts) if opts.is_a?(Hash)
      self
    end
    alias merge configure

    def to_h
      @opts.dup
    end
    alias to_hash to_h

    def [](name)
      @opts[name.to_sym]
    end

    def []=(name, value)
      @opts[name.to_sym] = value
    end

    def generate(obj)
      if @opts[:indent] && !@opts[:indent].empty?
        JSON.pretty_generate(obj, self)
      else
        JSON.generate(obj, self)
      end
    end
  end

  module Ext
    module Generator
      State = ::JSON::State
    end
  end

  SAFE_STATE_PROTOTYPE = State.new
  FAST_STATE_PROTOTYPE = State.new
  PRETTY_STATE_PROTOTYPE = State.new(indent: "  ", space: " ", object_nl: "\n", array_nl: "\n")

  class Fragment
    def initialize(raw)
      @raw = raw.to_s
    end

    def to_s
      @raw
    end

    def to_json(*)
      @raw
    end
  end

  def self.[](object, opts = {})
    if object.respond_to?(:to_str)
      parse(object.to_str, opts)
    else
      generate(object, opts)
    end
  end

  def self.dump(obj, anIO = nil, limit = nil)
    str = generate(obj)
    if anIO
      anIO.write(str)
      anIO
    else
      str
    end
  end

  def self.load(source, proc = nil, options = {})
    source = source.read if source.respond_to?(:read)
    source = source.to_str if source.respond_to?(:to_str)
    result = parse(source, options)
    proc.call(result) if proc
    result
  end

  class << self
    alias restore load
    alias unsafe_load load
  end
end

[Object, Array, Hash, String, Integer, Float, TrueClass, FalseClass, NilClass].each do |klass|
  unless klass.method_defined?(:to_json)
    klass.define_method(:to_json) { |*_args| ::JSON.generate(self) }
  end
end
`)
		return nil
	})
}

func mrbJSONStringify(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrbJSONGenerate(mrb, self)
}

func appendJSONString(buf []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x80 || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&' {
			out, err := json.Marshal(s, jsontext.EscapeForHTML(true))
			if err == nil {
				return append(buf, out...)
			}
			break
		}
	}
	buf = append(buf, '"')
	buf = append(buf, s...)
	buf = append(buf, '"')
	return buf
}

type kvPair struct {
	k string
	v oruby.Value
}

func appendJSONValue(mrb *oruby.MrbState, buf []byte, val oruby.Value) ([]byte, error) {
	if val.IsNil() {
		return append(buf, "null"...), nil
	}
	switch val.Type() {
	case oruby.MrbTTFalse:
		return append(buf, "false"...), nil
	case oruby.MrbTTTrue:
		return append(buf, "true"...), nil
	case oruby.MrbTTInteger:
		return strconv.AppendInt(buf, val.Int64(), 10), nil
	case oruby.MrbTTFloat:
		return strconv.AppendFloat(buf, val.Float64(), 'g', -1, 64), nil
	case oruby.MrbTTString:
		ptr, n := oruby.RStringMem(val)
		if n <= 0 || ptr == nil {
			return append(buf, `""`...), nil
		}
		return appendJSONString(buf, unsafe.String(ptr, n)), nil
	case oruby.MrbTTSymbol:
		return appendJSONString(buf, mrb.SymName(val.Symbol())), nil
	case oruby.MrbTTArray:
		elems := oruby.RArraySlice(val)
		buf = append(buf, '[')
		for i, elem := range elems {
			if i > 0 {
				buf = append(buf, ',')
			}
			var err error
			buf, err = appendJSONValue(mrb, buf, elem)
			if err != nil {
				return nil, err
			}
		}
		buf = append(buf, ']')
		return buf, nil
	case oruby.MrbTTHash:
		entries := oruby.RHashEntries(val)
		var stackKVs [16]kvPair
		var kvs []kvPair
		if len(entries) <= len(stackKVs) {
			kvs = stackKVs[:0]
		} else {
			kvs = make([]kvPair, 0, len(entries))
		}
		for i := range entries {
			key := entries[i][0]
			if key.Type() == oruby.MrbTTUndef {
				continue
			}
			var keyStr string
			switch key.Type() {
			case oruby.MrbTTString:
				ptr, n := oruby.RStringMem(key)
				if n > 0 && ptr != nil {
					keyStr = unsafe.String(ptr, n)
				}
			case oruby.MrbTTSymbol:
				keyStr = mrb.SymName(key.Symbol())
			default:
				keyStr = mrb.ObjAsString(key).String()
			}
			kvs = append(kvs, kvPair{k: keyStr, v: entries[i][1]})
		}
		// Insertion sort by key name for deterministic output
		for i := 1; i < len(kvs); i++ {
			cur := kvs[i]
			j := i - 1
			for j >= 0 && kvs[j].k > cur.k {
				kvs[j+1] = kvs[j]
				j--
			}
			kvs[j+1] = cur
		}
		buf = append(buf, '{')
		for i, kv := range kvs {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSONString(buf, kv.k)
			buf = append(buf, ':')
			var err error
			buf, err = appendJSONValue(mrb, buf, kv.v)
			if err != nil {
				return nil, err
			}
		}
		buf = append(buf, '}')
		return buf, nil
	default:
		if mrb.ClassOf(val).Name() == "JSON::Fragment" {
			raw := mrb.IVGet(val, mrb.Intern("@raw"))
			return append(buf, mrb.String(raw)...), nil
		}
		out, err := json.Marshal(mrb.Intf(val), json.Deterministic(true), jsontext.EscapeForHTML(true))
		if err != nil {
			return nil, err
		}
		return append(buf, out...), nil
	}
}

func jsonErrorClass(mrb *oruby.MrbState, name string) oruby.RClass {
	mod := mrb.ModuleGet("JSON")
	if !mod.IsNil() && mrb.ConstDefined(mod, mrb.Intern(name)) {
		cls := mrb.ClassGetUnder(mod, name)
		if !cls.IsNil() {
			return cls
		}
	}
	return mrb.EArgumentError()
}

var genBuf [512]byte

func mrbJSONGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	buf, err := appendJSONValue(mrb, genBuf[:0], v)
	if err != nil {
		return mrb.Raisef(jsonErrorClass(mrb, "GeneratorError"), "JSON::generate - %v", err)
	}
	return mrb.BytesValue(buf)
}

func mrbJSONPrettyGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	raw, err := appendJSONValue(mrb, nil, v)
	if err != nil {
		return mrb.Raisef(jsonErrorClass(mrb, "GeneratorError"), "JSON::generate - %v", err)
	}
	val := jsontext.Value(raw)
	if err := val.Format(jsontext.WithIndent("  ")); err != nil {
		return mrb.Raisef(jsonErrorClass(mrb, "GeneratorError"), "JSON::generate - %v", err)
	}
	return mrb.BytesValue(val)
}

var (
	parseReader bytes.Reader
	parseDec    *jsontext.Decoder
	parseOps    = make([]oruby.BatchOp, 0, 64)
	parseStrBuf = make([]byte, 0, 256)
)

func decodeJSONToBatch(dec *jsontext.Decoder, ops *[]oruby.BatchOp, strBuf *[]byte) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case 'n':
		*ops = append(*ops, oruby.BatchOp{Op: 0})
	case 'f':
		*ops = append(*ops, oruby.BatchOp{Op: 1})
	case 't':
		*ops = append(*ops, oruby.BatchOp{Op: 2})
	case '0':
		if i, err := tok.Int(); err == nil {
			*ops = append(*ops, oruby.BatchOp{Op: 3, I: i})
		} else {
			f, ferr := tok.Float()
			if ferr != nil {
				return ferr
			}
			*ops = append(*ops, oruby.BatchOp{Op: 4, F: f})
		}
	case '"':
		s := tok.String()
		off := uint32(len(*strBuf))
		*strBuf = append(*strBuf, s...)
		*ops = append(*ops, oruby.BatchOp{Op: 5, Off: off, I: int64(len(s))})
	case '[':
		var cnt int64
		for dec.PeekKind() != ']' {
			if err := decodeJSONToBatch(dec, ops, strBuf); err != nil {
				return err
			}
			cnt++
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		*ops = append(*ops, oruby.BatchOp{Op: 6, I: cnt})
	case '{':
		var pairs int64
		for dec.PeekKind() != '}' {
			ktok, err := dec.ReadToken()
			if err != nil {
				return err
			}
			ks := ktok.String()
			off := uint32(len(*strBuf))
			*strBuf = append(*strBuf, ks...)
			*ops = append(*ops, oruby.BatchOp{Op: 5, Off: off, I: int64(len(ks))})
			if err := decodeJSONToBatch(dec, ops, strBuf); err != nil {
				return err
			}
			pairs++
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		*ops = append(*ops, oruby.BatchOp{Op: 7, I: pairs})
	}
	return nil
}

func symbolizeKeys(mrb *oruby.MrbState, v oruby.Value) oruby.Value {
	switch v.Type() {
	case oruby.MrbTTHash:
		out := mrb.HashNew()
		entries := oruby.RHashEntries(v)
		for i := range entries {
			k := entries[i][0]
			if k.Type() == oruby.MrbTTUndef {
				continue
			}
			val := symbolizeKeys(mrb, entries[i][1])
			if k.Type() == oruby.MrbTTString {
				k = mrb.SymbolValue(mrb.Intern(mrb.String(k)))
			}
			mrb.HashSet(out, k, val)
		}
		return out.Value()
	case oruby.MrbTTArray:
		elems := oruby.RArraySlice(v)
		out := mrb.AryNewCapa(len(elems))
		for _, elem := range elems {
			out.Push(symbolizeKeys(mrb, elem))
		}
		return out.Value()
	default:
		return v
	}
}

func mrbJSONParse(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	arg := args.Item(0)
	if arg.Type() != oruby.MrbTTString {
		return mrb.Raise(mrb.ETypeError(), "JSON::parse - string argument expected")
	}
	symbolize := false
	checkOpts := func(opts oruby.Value) {
		if opts.IsHash() {
			if v := mrb.HashGet(opts, mrb.SymbolValue(mrb.Intern("symbolize_names"))); oruby.MrbBoolean(v) {
				symbolize = true
			} else if v := mrb.HashGet(opts, mrb.StrNew("symbolize_names")); oruby.MrbBoolean(v) {
				symbolize = true
			}
		}
	}
	if args.Len() > 1 {
		checkOpts(args.Item(1))
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() {
		checkOpts(kw)
	}

	ptr, n := oruby.RStringMem(arg)
	var inBytes []byte
	if n > 0 && ptr != nil {
		inBytes = unsafe.Slice(ptr, n)
	}

	parseReader.Reset(inBytes)
	if parseDec == nil {
		parseDec = jsontext.NewDecoder(&parseReader)
	} else {
		parseDec.Reset(&parseReader)
	}
	parseOps = parseOps[:0]
	parseStrBuf = parseStrBuf[:0]

	if err := decodeJSONToBatch(parseDec, &parseOps, &parseStrBuf); err != nil {
		return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "JSON::parse - %v", err)
	}
	if _, err := parseDec.ReadToken(); err != io.EOF {
		return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "JSON::parse - unexpected trailing data")
	}

	res := mrb.ExecBatchOps(parseOps, parseStrBuf)
	if symbolize {
		return symbolizeKeys(mrb, res)
	}
	return res
}
