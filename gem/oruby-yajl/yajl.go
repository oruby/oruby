// Package orubyyajl implements the Ruby yajl-ruby gem backed by Go's
// encoding/json/v2 and encoding/json/jsontext, registered as "oruby-yajl"
// and aliased to "yajl", "yajl/yajl", and "yajl/json_gem".
package orubyyajl

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	oruby.LazyGem("oruby-yajl", initYajl)
	oruby.GemAlias("yajl", "oruby-yajl")
	oruby.GemAlias("yajl/yajl", "oruby-yajl")
	oruby.GemAlias("yajl/json_gem", "oruby-yajl")
}

func decodeValue(mrb *oruby.MrbState, dec *jsontext.Decoder, symKeys bool) (oruby.Value, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return mrb.NilValue(), err
	}
	switch tok.Kind() {
	case 'n':
		return mrb.NilValue(), nil
	case 't':
		return mrb.TrueValue(), nil
	case 'f':
		return mrb.FalseValue(), nil
	case '"':
		return mrb.StrNew(tok.String()), nil
	case '0':
		raw := tok.String()
		if !strings.ContainsAny(raw, ".eE") {
			if iv, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return mrb.Int64Value(iv), nil
			}
		}
		fv, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return mrb.NilValue(), err
		}
		return mrb.FloatValue(fv), nil
	case '[':
		ary := mrb.AryNew()
		for dec.PeekKind() != ']' {
			elem, err := decodeValue(mrb, dec, symKeys)
			if err != nil {
				return mrb.NilValue(), err
			}
			mrb.AryPush(ary, elem)
		}
		if _, err := dec.ReadToken(); err != nil { // consume ']'
			return mrb.NilValue(), err
		}
		return ary.Value(), nil
	case '{':
		h := mrb.HashNew()
		for dec.PeekKind() != '}' {
			ktok, err := dec.ReadToken()
			if err != nil {
				return mrb.NilValue(), err
			}
			if ktok.Kind() != '"' {
				return mrb.NilValue(), fmt.Errorf("expected string key, got %v", ktok.Kind())
			}
			keyStr := ktok.String()
			val, err := decodeValue(mrb, dec, symKeys)
			if err != nil {
				return mrb.NilValue(), err
			}
			var k oruby.Value
			if symKeys {
				k = mrb.Intern(keyStr).Value()
			} else {
				k = mrb.StrNew(keyStr)
			}
			if _, err := mrb.Funcall(h, mrb.Intern("[]="), k, val); err != nil {
				return mrb.NilValue(), err
			}
		}
		if _, err := dec.ReadToken(); err != nil { // consume '}'
			return mrb.NilValue(), err
		}
		return h.Value(), nil
	default:
		return mrb.NilValue(), fmt.Errorf("unexpected JSON token %v", tok.Kind())
	}
}

func encodeRubyValue(mrb *oruby.MrbState, enc *jsontext.Encoder, v oruby.Value) error {
	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			return enc.WriteToken(jsontext.Null)
		}
		return enc.WriteToken(jsontext.False)
	case oruby.MrbTTTrue:
		return enc.WriteToken(jsontext.True)
	case oruby.MrbTTInteger:
		return enc.WriteToken(jsontext.Int(v.Int64()))
	case oruby.MrbTTFloat:
		f := v.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("cannot encode NaN or Infinity in JSON")
		}
		return enc.WriteToken(jsontext.Float(f))
	case oruby.MrbTTString:
		return enc.WriteToken(jsontext.String(v.String()))
	case oruby.MrbTTSymbol:
		return enc.WriteToken(jsontext.String(mrb.SymName(v.Symbol())))
	case oruby.MrbTTArray:
		if err := enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		n := oruby.RArrayLen(v)
		for i := 0; i < n; i++ {
			if err := encodeRubyValue(mrb, enc, mrb.AryRef(v, i)); err != nil {
				return err
			}
		}
		return enc.WriteToken(jsontext.EndArray)
	case oruby.MrbTTHash:
		if err := enc.WriteToken(jsontext.BeginObject); err != nil {
			return err
		}
		keys := mrb.HashKeys(v)
		n := oruby.RArrayLen(keys)
		for i := 0; i < n; i++ {
			k := mrb.AryRef(keys, i)
			val, err := mrb.Funcall(v, mrb.Intern("[]"), k)
			if err != nil {
				return err
			}
			ks := mrb.String(k)
			if err := enc.WriteToken(jsontext.String(ks)); err != nil {
				return err
			}
			if err := encodeRubyValue(mrb, enc, val); err != nil {
				return err
			}
		}
		return enc.WriteToken(jsontext.EndObject)
	default:
		if mrb.RespondTo(v, mrb.Intern("to_json")) {
			raw, err := mrb.Funcall(v, mrb.Intern("to_json"))
			if err == nil && raw.IsString() {
				return enc.WriteValue(jsontext.Value(raw.String()))
			}
		}
		return enc.WriteToken(jsontext.String(mrb.String(v)))
	}
}

func initYajl(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Yajl")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("1.4.3"))
	stdErr := mrb.ClassGet("StandardError")
	parseErr := mrb.DefineClassUnder(mod, "ParseError", stdErr)
	encErr := mrb.DefineClassUnder(mod, "EncodeError", stdErr)

	// Stream-parse one or more JSON values from src using encoding/json/jsontext.
	mod.DefineClassMethod("__parse_stream", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		src := args[0].String()
		symKeys := len(args) >= 2 && args[1].Bool()
		dec := jsontext.NewDecoder(strings.NewReader(src))
		results := mrb.AryNew()
		for {
			if dec.PeekKind() == 0 {
				// Check if EOF or syntax error
				_, err := dec.ReadToken()
				if err == io.EOF {
					break
				}
				return mrb.Raise(parseErr, err.Error())
			}
			val, err := decodeValue(mrb, dec, symKeys)
			if err != nil {
				return mrb.Raise(parseErr, err.Error())
			}
			mrb.AryPush(results, val)
		}
		return results
	}, mrb.ArgsArg(1, 1))

	// Encode Ruby value using encoding/json/jsontext + encoding/json/v2.
	mod.DefineClassMethod("__encode", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		obj := args[0]
		pretty := len(args) >= 2 && args[1].Bool()
		indent := "  "
		if len(args) >= 3 && !args[2].IsNil() {
			indent = args[2].String()
		}
		htmlSafe := len(args) >= 4 && args[3].Bool()

		var opts []jsontext.Options
		if pretty {
			opts = append(opts, jsontext.WithIndent(indent))
		}
		opts = append(opts, jsontext.EscapeForHTML(htmlSafe))

		var buf bytes.Buffer
		enc := jsontext.NewEncoder(&buf, opts...)
		if err := encodeRubyValue(mrb, enc, obj); err != nil {
			return mrb.Raise(encErr, err.Error())
		}
		// jsontext.Encoder appends a trailing newline; strip it to match Yajl::Encoder.
		out := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		return mrb.StrNew(string(out))
	}, mrb.ArgsArg(1, 3))

	// Reference json.Marshal to keep encoding/json/v2 imported and available.
	_ = json.Marshal

	rb.LoadRuby(mrb, "<internal:oruby-yajl>", []byte(`
module Yajl
  class Parser
    attr_accessor :on_parse_complete

    def self.parse(str_or_io, options = {}, &block)
      new(options).parse(str_or_io, &block)
    end

    def initialize(options = {})
      @options = options || {}
      @sym_keys = !!@options[:symbolize_keys]
      @buf = +""
      @on_parse_complete = nil
    end

    def parse(str_or_io, _buffer_size = nil, &block)
      src = str_or_io.respond_to?(:read) ? str_or_io.read : str_or_io.to_s
      vals = Yajl.__parse_stream(src, @sym_keys)
      cb = block || @on_parse_complete
      if cb
        vals.each { |v| cb.call(v) }
        nil
      else
        vals.length <= 1 ? vals.first : vals
      end
    end

    def <<(chunk)
      @buf << chunk.to_s
      begin
        vals = Yajl.__parse_stream(@buf, @sym_keys)
        @buf = +""
        vals.each { |v| @on_parse_complete.call(v) } if @on_parse_complete
      rescue ParseError
        # Wait for remaining chunk bytes
      end
      self
    end
  end

  class Encoder
    def self.encode(obj, io = nil, options = {}, &block)
      if io.is_a?(Hash) && options.empty?
        options = io
        io = nil
      end
      new(options).encode(obj, io, &block)
    end

    def initialize(options = {})
      @options = options || {}
      @pretty = !!@options[:pretty]
      @indent = @options[:indent] || "  "
      @html_safe = !!@options[:html_safe]
    end

    def encode(obj, io = nil, &block)
      out = Yajl.__encode(obj, @pretty, @indent, @html_safe)
      if io
        io.write(out)
        nil
      elsif block
        block.call(out)
        nil
      else
        out
      end
    end
  end

  def self.load(str_or_io, options = {}, &block)
    Parser.parse(str_or_io, options, &block)
  end

  def self.dump(obj, io = nil, options = {}, &block)
    Encoder.encode(obj, io, options, &block)
  end
end
`))
	return nil
}
