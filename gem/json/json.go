package json

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/oruby/oruby"
)

var (
	errNesting = errors.New("nesting too deep")
	errNaNInf  = errors.New("NaN/Infinity not allowed in JSON")
)

const (
	defaultMaxNesting = 100
	hardMaxNesting    = 10000
)

type genError struct {
	err        error
	invalidObj oruby.Value
	hasInvalid bool
	isNesting  bool
	isRubyExc  bool
}

func (e *genError) Error() string { return e.err.Error() }

func init() {
	oruby.Gem("json", func(mrb *oruby.MrbState) interface{} {
		jsonModule := mrb.DefineModule("JSON")
		mrb.ConstSet(jsonModule, mrb.Intern("VERSION"), mrb.StrNew("2.10.2"))
		mrb.ConstSet(jsonModule, mrb.Intern("NaN"), mrb.FloatValue(math.NaN()))
		mrb.ConstSet(jsonModule, mrb.Intern("Infinity"), mrb.FloatValue(math.Inf(1)))
		mrb.ConstSet(jsonModule, mrb.Intern("MinusInfinity"), mrb.FloatValue(math.Inf(-1)))

		jsonErr := mrb.DefineClassUnder(jsonModule, "JSONError", mrb.ExcGet("StandardError"))
		mrb.DefineClassUnder(jsonModule, "ParserError", jsonErr)
		genErr := mrb.DefineClassUnder(jsonModule, "GeneratorError", jsonErr)
		mrb.DefineClassUnder(jsonModule, "NestingError", genErr)

		jsonOptArgs := mrb.ArgsArg(1, 2) | mrb.ArgsKey(0, 1)
		jsonModule.DefineClassMethod("stringify", mrbJSONStringify, jsonOptArgs)
		jsonModule.DefineClassMethod("__generate", mrbJSONGenerate, jsonOptArgs)
		jsonModule.DefineClassMethod("__parse", mrbJSONParse, jsonOptArgs)

		_, _ = mrb.Eval(`
module JSON
  @create_id = "json_class"
  @state = nil
  @parser = nil
  @generator = nil

  class << self
    attr_accessor :create_id, :state, :parser, :generator
  end

  class GeneratorError
    attr_reader :invalid_object

    def initialize(message = nil, invalid_object: nil)
      super(message)
      @invalid_object = invalid_object
    end
  end

  class State
    VALID_OPTIONS = [
      :indent, :space, :space_before, :object_nl, :array_nl,
      :max_nesting, :allow_nan, :ascii_only, :script_safe, :escape_slash,
      :strict, :depth, :as_json, :buffer_initial_length, :check_circular,
      :sort_keys, :allow_duplicate_key, :rfc8785
    ].freeze

    attr_accessor :indent, :space, :space_before, :object_nl, :array_nl,
                  :max_nesting, :allow_nan, :ascii_only, :script_safe,
                  :strict, :depth, :as_json, :buffer_initial_length,
                  :sort_keys, :allow_duplicate_key, :rfc8785

    alias escape_slash script_safe
    alias escape_slash= script_safe=
    alias allow_nan? allow_nan
    alias ascii_only? ascii_only
    alias script_safe? script_safe
    alias escape_slash? script_safe
    alias strict? strict

    def self.from_state(opts)
      case opts
      when self then opts
      when Hash then new(opts)
      when nil then new
      else
        if opts.respond_to?(:to_hash)
          new(opts.to_hash)
        elsif opts.respond_to?(:to_h)
          new(opts.to_h)
        else
          new
        end
      end
    end

    def initialize(opts = nil)
      @indent = ""
      @space = ""
      @space_before = ""
      @object_nl = ""
      @array_nl = ""
      @max_nesting = 100
      @allow_nan = false
      @ascii_only = false
      @script_safe = false
      @strict = false
      @depth = 0
      @as_json = nil
      @buffer_initial_length = 1024
      @sort_keys = false
      @allow_duplicate_key = true
      @rfc8785 = false
      configure(opts) if opts
    end

    def check_circular?
      @max_nesting != false && @max_nesting != 0
    end

    def configure(opts)
      if opts.respond_to?(:to_hash)
        opts = opts.to_hash
      elsif opts.respond_to?(:to_h)
        opts = opts.to_h
      else
        raise TypeError, "can't convert #{opts.class} into Hash"
      end
      opts.each do |k, v|
        key = k.to_sym
        case key
        when :indent
          @indent = check_str_opt(key, v)
        when :space
          @space = check_str_opt(key, v)
        when :space_before
          @space_before = check_str_opt(key, v)
        when :object_nl
          @object_nl = check_str_opt(key, v)
        when :array_nl
          @array_nl = check_str_opt(key, v)
        when :max_nesting
          @max_nesting = (v == false || v.nil?) ? false : v.to_int
        when :check_circular
          @max_nesting = v ? 100 : false
        when :allow_nan
          @allow_nan = !!v
        when :ascii_only
          @ascii_only = !!v
        when :script_safe, :escape_slash
          @script_safe = !!v
        when :strict
          @strict = !!v
        when :depth
          @depth = v.to_int
        when :as_json
          @as_json = v
        when :buffer_initial_length
          @buffer_initial_length = v.to_int if v
        when :sort_keys
          @sort_keys = !!v
        when :allow_duplicate_key
          @allow_duplicate_key = !!v
        when :rfc8785
          @rfc8785 = !!v
          if @rfc8785
            @sort_keys = true
            @allow_nan = false
            @script_safe = false
            @ascii_only = false
            @indent = ""
            @space = ""
            @space_before = ""
            @object_nl = ""
            @array_nl = ""
          end
        else
          raise ArgumentError, "unknown keyword: #{key.inspect}"
        end
      end
      self
    end
    alias merge configure

    def to_h
      {
        indent: @indent,
        space: @space,
        space_before: @space_before,
        object_nl: @object_nl,
        array_nl: @array_nl,
        max_nesting: @max_nesting,
        allow_nan: @allow_nan,
        ascii_only: @ascii_only,
        script_safe: @script_safe,
        strict: @strict,
        depth: @depth,
        as_json: @as_json,
        buffer_initial_length: @buffer_initial_length,
      }
    end
    alias to_hash to_h

    def [](name)
      key = name.to_sym
      to_h[key]
    end

    def []=(name, value)
      configure(name.to_sym => value)
      value
    end

    def generate(obj, io = nil)
      str = JSON.__generate(obj, self)
      if io
        io.write(str)
        io
      else
        str
      end
    end

    private

    def check_str_opt(key, v)
      return "" if v.nil?
      if v.is_a?(String)
        v
      elsif v.respond_to?(:to_str)
        v.to_str
      else
        raise TypeError, "no implicit conversion of #{v.class} into String for #{key}"
      end
    end
  end

  module Ext
    module Generator
      State = ::JSON::State
    end
  end

  SAFE_STATE_PROTOTYPE = State.new.freeze
  FAST_STATE_PROTOTYPE = State.new(max_nesting: false).freeze
  PRETTY_STATE_PROTOTYPE = State.new(indent: "  ", space: " ", object_nl: "\n", array_nl: "\n").freeze

  class Fragment
    def initialize(raw)
      @raw = raw.respond_to?(:to_str) ? raw.to_str : raw.to_s
    end

    def to_s
      @raw
    end

    def to_json(*)
      @raw
    end

    def ==(other)
      other.is_a?(Fragment) && @raw == other.to_s
    end
    alias eql? ==

    def hash
      @raw.hash
    end
  end

  class Parser
    attr_reader :source

    def initialize(source, opts = nil, **kw)
      if source.is_a?(String)
        @source = source
      elsif source.respond_to?(:to_str)
        @source = source.to_str
      else
        raise TypeError, "no implicit conversion of #{source.class} into String"
      end
      @opts = {}
      @opts.merge!(opts) if opts.is_a?(Hash)
      @opts.merge!(kw) unless kw.empty?
    end

    def parse
      JSON.__parse(@source, @opts)
    end
  end

  module Ext
    Parser = ::JSON::Parser
  end

  @state = State
  @parser = Parser
  @generator = Ext::Generator

  class Coder
    def initialize(opts = nil, **kw, &block)
      merged = {}
      merged.merge!(opts) if opts.is_a?(Hash)
      merged.merge!(kw) unless kw.empty?
      merged[:as_json] = block if block
      @gen_opts = {}
      @parse_opts = {}
      merged.each do |k, v|
        key = k.to_sym
        if State::VALID_OPTIONS.include?(key)
          @gen_opts[key] = v
        end
        @parse_opts[key] = v
      end
      @state = State.new(@gen_opts)
    end

    def dump(obj, io = nil)
      @state.generate(obj, io)
    end

    def load(source)
      JSON.parse(source, @parse_opts)
    end
  end

  def self.[](object, opts = {})
    if object.respond_to?(:to_str)
      parse(object.to_str, opts)
    else
      generate(object, opts)
    end
  end

  def parse(source, opts = nil, **kw)
    if opts.nil? && kw.empty?
      JSON.__parse(source, nil)
    else
      merged = opts.is_a?(Hash) ? opts.merge(kw) : kw
      JSON.__parse(source, merged)
    end
  end
  module_function :parse

  def parse!(source, opts = nil, **kw)
    merged = { max_nesting: false, allow_nan: true }
    merged.merge!(opts) if opts.is_a?(Hash)
    merged.merge!(kw) unless kw.empty?
    JSON.__parse(source, merged)
  end
  module_function :parse!

  def generate(obj, opts = nil, **kw)
    state =
      if opts.is_a?(State) && kw.empty?
        opts
      elsif opts.nil? && kw.empty?
        SAFE_STATE_PROTOTYPE
      else
        s = opts.is_a?(State) ? opts.dup : State.from_state(opts)
        s.configure(kw) unless kw.empty?
        s
      end
    JSON.__generate(obj, state)
  end
  module_function :generate
  alias unparse generate
  module_function :unparse

  def fast_generate(obj, opts = nil, **kw)
    state =
      if opts.is_a?(State) && kw.empty?
        opts
      elsif opts.nil? && kw.empty?
        FAST_STATE_PROTOTYPE
      else
        s = opts.is_a?(State) ? opts.dup : State.new(max_nesting: false)
        s.configure(opts) if opts.is_a?(Hash)
        s.configure(kw) unless kw.empty?
        s
      end
    JSON.__generate(obj, state)
  end
  module_function :fast_generate
  alias fast_unparse fast_generate
  module_function :fast_unparse

  def pretty_generate(obj, opts = nil, **kw)
    state =
      if opts.is_a?(State) && kw.empty?
        opts
      elsif opts.nil? && kw.empty?
        PRETTY_STATE_PROTOTYPE
      else
        s = State.new(indent: "  ", space: " ", object_nl: "\n", array_nl: "\n")
        s.configure(opts) if opts
        s.configure(kw) unless kw.empty?
        s
      end
    JSON.__generate(obj, state)
  end
  module_function :pretty_generate
  alias pretty_unparse pretty_generate
  module_function :pretty_unparse

  def dump(obj, anIO = nil, limit = nil, **kw)
    opts = { allow_nan: true }
    if anIO.is_a?(Hash) && limit.nil?
      opts.merge!(anIO)
      anIO = nil
    elsif anIO.is_a?(Integer) && limit.nil?
      limit = anIO
      anIO = nil
    elsif anIO && !anIO.respond_to?(:write)
      if anIO.respond_to?(:to_int)
        limit = anIO.to_int
        anIO = nil
      end
    end
    opts[:max_nesting] = limit unless limit.nil?
    opts.merge!(kw) unless kw.empty?
    state = State.new(opts)
    str = JSON.__generate(obj, state)
    if anIO
      anIO.write(str)
      anIO
    else
      str
    end
  end
  module_function :dump

  def recurse_proc(result, &proc)
    case result
    when Array
      result.each { |x| recurse_proc(x, &proc) }
      proc.call(result)
    when Hash
      result.each { |x, y| recurse_proc(x, &proc); recurse_proc(y, &proc) }
      proc.call(result)
    else
      proc.call(result)
    end
  end
  module_function :recurse_proc

  def load(source, proc = nil, opts = nil, **kw)
    if proc.is_a?(Hash) && opts.nil?
      opts = proc
      proc = nil
    end
    if source.respond_to?(:read)
      source = source.read
    elsif source.respond_to?(:to_str)
      source = source.to_str
    end
    return nil if source.nil? || (source.is_a?(String) && source.empty?)
    merged = { max_nesting: false, allow_nan: true, create_additions: true }
    merged.merge!(opts) if opts.is_a?(Hash)
    merged.merge!(kw) unless kw.empty?
    result = JSON.__parse(source, merged)
    if proc
      JSON.recurse_proc(result, &proc)
    end
    result
  end
  module_function :load
  alias restore load
  module_function :restore
  alias unsafe_load load
  module_function :unsafe_load

  def load_file(path, opts = nil, **kw)
    parse(File.read(path), opts, **kw)
  end
  module_function :load_file

  def load_file!(path, opts = nil, **kw)
    parse!(File.read(path), opts, **kw)
  end
  module_function :load_file!
end

module Kernel
  private

  def JSON(object, *args, **kw)
    if object.respond_to?(:to_str)
      ::JSON.parse(object.to_str, *args, **kw)
    else
      ::JSON.generate(object, *args, **kw)
    end
  end
end

class Object
  def to_json(*args)
    to_s.to_json(*args)
  end unless method_defined?(:to_json)
end

[Array, Hash, String, Integer, Float, TrueClass, FalseClass, NilClass, Symbol].each do |klass|
  klass.define_method(:to_json) do |*args|
    ::JSON.generate(self, *args)
  end
end
`)
		return nil
	})
}

type jsonGenOpts struct {
	indent      string
	space       string
	spaceBefore string
	objectNL    string
	arrayNL     string
	maxNesting  int
	initDepth   int
	allowNaN    bool
	asciiOnly   bool
	scriptSafe  bool
	strict      bool
	sortKeys    bool
	escapeHTML  bool
	asJSON      oruby.Value
	stateVal    oruby.Value
}

func defaultGenOpts() jsonGenOpts {
	return jsonGenOpts{
		maxNesting: defaultMaxNesting,
	}
}

func extractJSONGenOpts(mrb *oruby.MrbState, args oruby.RArgs) jsonGenOpts {
	res := defaultGenOpts()
	applyState := func(st oruby.Value) {
		res.stateVal = st
		if v := mrb.IVGet(st, mrb.Intern("@indent")); v.IsString() {
			res.indent = mrb.String(v)
		}
		if v := mrb.IVGet(st, mrb.Intern("@space")); v.IsString() {
			res.space = mrb.String(v)
		}
		if v := mrb.IVGet(st, mrb.Intern("@space_before")); v.IsString() {
			res.spaceBefore = mrb.String(v)
		}
		if v := mrb.IVGet(st, mrb.Intern("@object_nl")); v.IsString() {
			res.objectNL = mrb.String(v)
		}
		if v := mrb.IVGet(st, mrb.Intern("@array_nl")); v.IsString() {
			res.arrayNL = mrb.String(v)
		}
		if v := mrb.IVGet(st, mrb.Intern("@depth")); v.IsInt() {
			res.initDepth = int(v.Int64())
		}
		if v := mrb.IVGet(st, mrb.Intern("@max_nesting")); v.IsNil() || v.Type() == oruby.MrbTTFalse {
			res.maxNesting = hardMaxNesting
		} else if v.IsInt() {
			n := int(v.Int64())
			if n <= 0 || n > hardMaxNesting {
				res.maxNesting = hardMaxNesting
			} else {
				res.maxNesting = n
			}
		}
		res.allowNaN = oruby.MrbBoolean(mrb.IVGet(st, mrb.Intern("@allow_nan")))
		res.asciiOnly = oruby.MrbBoolean(mrb.IVGet(st, mrb.Intern("@ascii_only")))
		res.scriptSafe = oruby.MrbBoolean(mrb.IVGet(st, mrb.Intern("@script_safe")))
		res.strict = oruby.MrbBoolean(mrb.IVGet(st, mrb.Intern("@strict")))
		res.sortKeys = oruby.MrbBoolean(mrb.IVGet(st, mrb.Intern("@sort_keys")))
		if v := mrb.IVGet(st, mrb.Intern("@as_json")); !v.IsNil() {
			res.asJSON = v
		}
	}
	applyHash := func(opts oruby.Value) {
		if !opts.IsHash() {
			return
		}
		getOpt := func(name string) (oruby.Value, bool) {
			sym := mrb.SymbolValue(mrb.Intern(name))
			if mrb.HashKeyP(opts, sym) {
				return mrb.HashGet(opts, sym), true
			}
			str := mrb.StrNew(name)
			if mrb.HashKeyP(opts, str) {
				return mrb.HashGet(opts, str), true
			}
			return mrb.NilValue(), false
		}
		if v, ok := getOpt("indent"); ok && v.IsString() {
			res.indent = mrb.String(v)
		}
		if v, ok := getOpt("space"); ok && v.IsString() {
			res.space = mrb.String(v)
		}
		if v, ok := getOpt("space_before"); ok && v.IsString() {
			res.spaceBefore = mrb.String(v)
		}
		if v, ok := getOpt("object_nl"); ok && v.IsString() {
			res.objectNL = mrb.String(v)
		}
		if v, ok := getOpt("array_nl"); ok && v.IsString() {
			res.arrayNL = mrb.String(v)
		}
		if v, ok := getOpt("depth"); ok && v.IsInt() {
			res.initDepth = int(v.Int64())
		}
		if v, ok := getOpt("max_nesting"); ok {
			if v.IsNil() || v.Type() == oruby.MrbTTFalse {
				res.maxNesting = hardMaxNesting
			} else if v.IsInt() {
				n := int(v.Int64())
				if n <= 0 || n > hardMaxNesting {
					res.maxNesting = hardMaxNesting
				} else {
					res.maxNesting = n
				}
			}
		}
		if v, ok := getOpt("allow_nan"); ok {
			res.allowNaN = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("ascii_only"); ok {
			res.asciiOnly = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("script_safe"); ok {
			res.scriptSafe = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("escape_slash"); ok {
			res.scriptSafe = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("strict"); ok {
			res.strict = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("as_json"); ok && !v.IsNil() {
			res.asJSON = v
		}
	}

	if args.Len() > 1 && !args.Item(1).IsNil() {
		o := args.Item(1)
		if mrb.ClassOf(o).Name() == "JSON::State" {
			applyState(o)
		} else if o.IsHash() {
			applyHash(o)
		} else if mrb.RespondTo(o, mrb.Intern("to_h")) {
			if h, err := mrb.Funcall(o, mrb.Intern("to_h")); err == nil && h.IsHash() {
				applyHash(h)
			}
		}
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() {
		applyHash(kw)
	}
	return res
}

const hexDigits = "0123456789abcdef"

func appendUnicodeEscape(buf []byte, r rune) []byte {
	if r <= 0xFFFF {
		return append(buf, '\\', 'u',
			hexDigits[(r>>12)&0xF],
			hexDigits[(r>>8)&0xF],
			hexDigits[(r>>4)&0xF],
			hexDigits[r&0xF],
		)
	}
	r1, r2 := utf16.EncodeRune(r)
	buf = append(buf, '\\', 'u',
		hexDigits[(r1>>12)&0xF],
		hexDigits[(r1>>8)&0xF],
		hexDigits[(r1>>4)&0xF],
		hexDigits[r1&0xF],
	)
	return append(buf, '\\', 'u',
		hexDigits[(r2>>12)&0xF],
		hexDigits[(r2>>8)&0xF],
		hexDigits[(r2>>4)&0xF],
		hexDigits[r2&0xF],
	)
}

func appendJSONString(buf []byte, s string, opts *jsonGenOpts, origVal oruby.Value) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, &genError{
			err:        errors.New("source sequence is illegal/malformed utf-8"),
			invalidObj: origVal,
			hasInvalid: true,
		}
	}
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < 0x80 {
			switch b {
			case '"':
				buf = append(buf, s[start:i]...)
				buf = append(buf, '\\', '"')
				i++
				start = i
			case '\\':
				buf = append(buf, s[start:i]...)
				buf = append(buf, '\\', '\\')
				i++
				start = i
			case '\b':
				buf = append(buf, s[start:i]...)
				buf = append(buf, '\\', 'b')
				i++
				start = i
			case '\f':
				buf = append(buf, s[start:i]...)
				buf = append(buf, '\\', 'f')
				i++
				start = i
			case '\n':
				buf = append(buf, s[start:i]...)
				buf = append(buf, '\\', 'n')
				i++
				start = i
			case '\r':
				buf = append(buf, s[start:i]...)
				buf = append(buf, '\\', 'r')
				i++
				start = i
			case '\t':
				buf = append(buf, s[start:i]...)
				buf = append(buf, '\\', 't')
				i++
				start = i
			case '/':
				if opts.scriptSafe {
					buf = append(buf, s[start:i]...)
					buf = append(buf, '\\', '/')
					i++
					start = i
				} else {
					i++
				}
			case '<', '>', '&':
				if opts.escapeHTML {
					buf = append(buf, s[start:i]...)
					buf = appendUnicodeEscape(buf, rune(b))
					i++
					start = i
				} else {
					i++
				}
			default:
				if b < 0x20 {
					buf = append(buf, s[start:i]...)
					buf = appendUnicodeEscape(buf, rune(b))
					i++
					start = i
				} else {
					i++
				}
			}
		} else {
			r, sz := utf8.DecodeRuneInString(s[i:])
			if opts.asciiOnly || (opts.scriptSafe && (r == '\u2028' || r == '\u2029')) {
				buf = append(buf, s[start:i]...)
				buf = appendUnicodeEscape(buf, r)
				i += sz
				start = i
			} else {
				i += sz
			}
		}
	}
	buf = append(buf, s[start:]...)
	buf = append(buf, '"')
	return buf, nil
}

func appendIndent(buf []byte, indent string, depth int) []byte {
	if indent == "" || depth <= 0 {
		return buf
	}
	for i := 0; i < depth; i++ {
		buf = append(buf, indent...)
	}
	return buf
}

type kvPair struct {
	k   string
	val oruby.Value
	raw oruby.Value
}

func appendJSONFloat(buf []byte, f float64, val oruby.Value, opts *jsonGenOpts) ([]byte, error) {
	if math.IsNaN(f) {
		if !opts.allowNaN {
			return nil, &genError{err: errNaNInf, invalidObj: val, hasInvalid: true}
		}
		return append(buf, "NaN"...), nil
	}
	if math.IsInf(f, 1) {
		if !opts.allowNaN {
			return nil, &genError{err: errNaNInf, invalidObj: val, hasInvalid: true}
		}
		return append(buf, "Infinity"...), nil
	}
	if math.IsInf(f, -1) {
		if !opts.allowNaN {
			return nil, &genError{err: errNaNInf, invalidObj: val, hasInvalid: true}
		}
		return append(buf, "-Infinity"...), nil
	}
	start := len(buf)
	buf = strconv.AppendFloat(buf, f, 'g', -1, 64)
	// Ensure Ruby Float formatting (1.0 -> "1.0", not "1")
	hasDotOrExp := false
	for i := start; i < len(buf); i++ {
		if buf[i] == '.' || buf[i] == 'e' || buf[i] == 'E' {
			hasDotOrExp = true
			break
		}
	}
	if !hasDotOrExp {
		buf = append(buf, '.', '0')
	}
	return buf, nil
}

func rubyStringEncodingName(mrb *oruby.MrbState, val oruby.Value) string {
	if !mrb.RespondTo(val, mrb.Intern("encoding")) {
		return "UTF-8"
	}
	encVal, err := mrb.Funcall(val, mrb.Intern("encoding"))
	if err != nil || encVal.IsNil() {
		return "UTF-8"
	}
	if encVal.IsString() {
		return mrb.String(encVal)
	}
	if mrb.RespondTo(encVal, mrb.Intern("name")) {
		encNameVal, nerr := mrb.Funcall(encVal, mrb.Intern("name"))
		if nerr == nil && encNameVal.IsString() {
			return mrb.String(encNameVal)
		}
	}
	return "UTF-8"
}

func convertRubyStringToUTF8(mrb *oruby.MrbState, val oruby.Value) (string, error) {
	encName := rubyStringEncodingName(mrb, val)
	if encName != "UTF-8" && encName != "US-ASCII" && encName != "ASCII-8BIT" {
		if mrb.RespondTo(val, mrb.Intern("encode")) {
			utf8Enc, eerr := mrb.Eval("Encoding::UTF_8")
			if eerr == nil {
				conv, cerr := mrb.Funcall(val, mrb.Intern("encode"), utf8Enc)
				if cerr != nil {
					return "", &genError{err: cerr, invalidObj: val, hasInvalid: true}
				}
				if conv.IsString() {
					val = conv
				}
			}
		}
	}
	ptr, n := oruby.RStringMem(val)
	if n <= 0 || ptr == nil {
		return "", nil
	}
	return unsafe.String(ptr, n), nil
}

func hasCustomToJSON(mrb *oruby.MrbState, val oruby.Value, baseClassName string) bool {
	cls := mrb.ClassOf(val)
	if cls.Name() == baseClassName {
		return false
	}
	// Subclass of Array/Hash/String: check if to_json owner differs from base class
	meth, err := mrb.Funcall(val, mrb.Intern("method"), mrb.SymbolValue(mrb.Intern("to_json")))
	if err != nil {
		return false
	}
	owner, err := mrb.Funcall(meth, mrb.Intern("owner"))
	if err != nil {
		return false
	}
	ownerName := mrb.String(owner)
	return ownerName != baseClassName && ownerName != "Object"
}

func callCustomToJSON(mrb *oruby.MrbState, buf []byte, val oruby.Value, opts *jsonGenOpts) ([]byte, error) {
	var res oruby.Value
	var err error
	if !opts.stateVal.IsNil() {
		res, err = mrb.Funcall(val, mrb.Intern("to_json"), opts.stateVal)
	} else {
		res, err = mrb.Funcall(val, mrb.Intern("to_json"))
	}
	if err != nil {
		return nil, &genError{err: err, isRubyExc: true}
	}
	if !res.IsString() {
		return nil, &genError{
			err:        fmt.Errorf("to_json must return a String, got %s", mrb.ClassOf(res).Name()),
			invalidObj: val,
			hasInvalid: true,
		}
	}
	ptr, n := oruby.RStringMem(res)
	if n > 0 && ptr != nil {
		buf = append(buf, unsafe.Slice(ptr, n)...)
	}
	return buf, nil
}

func appendJSONValue(mrb *oruby.MrbState, buf []byte, val oruby.Value, depth int, opts *jsonGenOpts) ([]byte, error) {
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
	case oruby.MrbTTBigInt:
		z, err := mrb.BigInt(val)
		if err != nil {
			return nil, &genError{err: err, invalidObj: val, hasInvalid: true}
		}
		return append(buf, z.String()...), nil
	case oruby.MrbTTFloat:
		return appendJSONFloat(buf, val.Float64(), val, opts)
	case oruby.MrbTTString:
		if !opts.strict && hasCustomToJSON(mrb, val, "String") {
			return callCustomToJSON(mrb, buf, val, opts)
		}
		s, err := convertRubyStringToUTF8(mrb, val)
		if err != nil {
			return nil, err
		}
		return appendJSONString(buf, s, opts, val)
	case oruby.MrbTTSymbol:
		if opts.strict {
			if !opts.asJSON.IsNil() {
				converted, err := mrb.Funcall(opts.asJSON, mrb.Intern("call"), val, mrb.FalseValue())
				if err != nil {
					return nil, &genError{err: err, isRubyExc: true}
				}
				return appendJSONValue(mrb, buf, converted, depth+1, opts)
			}
			return nil, &genError{err: errors.New("Symbol not allowed in JSON"), invalidObj: val, hasInvalid: true}
		}
		return appendJSONString(buf, mrb.SymName(val.Symbol()), opts, val)
	case oruby.MrbTTArray:
		if !opts.strict && hasCustomToJSON(mrb, val, "Array") {
			return callCustomToJSON(mrb, buf, val, opts)
		}
		if depth >= opts.maxNesting {
			return nil, &genError{err: errNesting, invalidObj: val, hasInvalid: true, isNesting: true}
		}
		elems := oruby.RArraySlice(val)
		if len(elems) == 0 {
			return append(buf, '[', ']'), nil
		}
		nextDepth := depth + 1
		buf = append(buf, '[')
		if opts.arrayNL != "" {
			buf = append(buf, opts.arrayNL...)
		}
		for i, elem := range elems {
			if i > 0 {
				buf = append(buf, ',')
				if opts.arrayNL != "" {
					buf = append(buf, opts.arrayNL...)
				}
			}
			if opts.arrayNL != "" {
				buf = appendIndent(buf, opts.indent, nextDepth)
			}
			var err error
			buf, err = appendJSONValue(mrb, buf, elem, nextDepth, opts)
			if err != nil {
				return nil, err
			}
		}
		if opts.arrayNL != "" {
			buf = append(buf, opts.arrayNL...)
			buf = appendIndent(buf, opts.indent, depth)
		}
		buf = append(buf, ']')
		return buf, nil
	case oruby.MrbTTHash:
		if !opts.strict && hasCustomToJSON(mrb, val, "Hash") {
			return callCustomToJSON(mrb, buf, val, opts)
		}
		if depth >= opts.maxNesting {
			return nil, &genError{err: errNesting, invalidObj: val, hasInvalid: true, isNesting: true}
		}
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
				var err error
				keyStr, err = convertRubyStringToUTF8(mrb, key)
				if err != nil {
					return nil, err
				}
			case oruby.MrbTTSymbol:
				keyStr = mrb.SymName(key.Symbol())
			default:
				if opts.strict && opts.asJSON.IsNil() {
					return nil, &genError{
						err:        fmt.Errorf("%s not allowed as JSON object key", mrb.ClassOf(key).Name()),
						invalidObj: key,
						hasInvalid: true,
					}
				}
				if !opts.asJSON.IsNil() {
					ck, err := mrb.Funcall(opts.asJSON, mrb.Intern("call"), key, mrb.TrueValue())
					if err != nil {
						return nil, &genError{err: err, isRubyExc: true}
					}
					if ck.IsString() {
						keyStr, err = convertRubyStringToUTF8(mrb, ck)
						if err != nil {
							return nil, err
						}
					} else if ck.IsSymbol() {
						keyStr = mrb.SymName(ck.Symbol())
					} else {
						return nil, &genError{err: errors.New("as_json must return String or Symbol for Hash key"), invalidObj: key, hasInvalid: true}
					}
				} else {
					sv, err := mrb.Funcall(key, oruby.Sym_to_s)
					if err != nil {
						return nil, &genError{err: err, isRubyExc: true}
					}
					if sv.Type() != oruby.MrbTTString {
						return nil, &genError{err: errors.New("key#to_s did not return String"), invalidObj: key, hasInvalid: true}
					}
					keyStr, err = convertRubyStringToUTF8(mrb, sv)
					if err != nil {
						return nil, err
					}
				}
			}
			kvs = append(kvs, kvPair{k: keyStr, val: entries[i][1], raw: key})
		}
		if len(kvs) == 0 {
			return append(buf, '{', '}'), nil
		}
		if opts.sortKeys {
			for i := 1; i < len(kvs); i++ {
				cur := kvs[i]
				j := i - 1
				for j >= 0 && kvs[j].k > cur.k {
					kvs[j+1] = kvs[j]
					j--
				}
				kvs[j+1] = cur
			}
		}
		nextDepth := depth + 1
		buf = append(buf, '{')
		if opts.objectNL != "" {
			buf = append(buf, opts.objectNL...)
		}
		for i, kv := range kvs {
			if i > 0 {
				buf = append(buf, ',')
				if opts.objectNL != "" {
					buf = append(buf, opts.objectNL...)
				}
			}
			if opts.objectNL != "" {
				buf = appendIndent(buf, opts.indent, nextDepth)
			}
			var err error
			buf, err = appendJSONString(buf, kv.k, opts, kv.raw)
			if err != nil {
				return nil, err
			}
			if opts.spaceBefore != "" {
				buf = append(buf, opts.spaceBefore...)
			}
			buf = append(buf, ':')
			if opts.space != "" {
				buf = append(buf, opts.space...)
			}
			buf, err = appendJSONValue(mrb, buf, kv.val, nextDepth, opts)
			if err != nil {
				return nil, err
			}
		}
		if opts.objectNL != "" {
			buf = append(buf, opts.objectNL...)
			buf = appendIndent(buf, opts.indent, depth)
		}
		buf = append(buf, '}')
		return buf, nil
	default:
		if mrb.ClassOf(val).Name() == "JSON::Fragment" {
			raw := mrb.IVGet(val, mrb.Intern("@raw"))
			return append(buf, mrb.String(raw)...), nil
		}
		if !opts.asJSON.IsNil() {
			if depth >= opts.maxNesting {
				return nil, &genError{err: errNesting, invalidObj: val, hasInvalid: true, isNesting: true}
			}
			converted, err := mrb.Funcall(opts.asJSON, mrb.Intern("call"), val, mrb.FalseValue())
			if err != nil {
				return nil, &genError{err: err, isRubyExc: true}
			}
			if mrb.ObjEqual(converted, val) {
				return nil, &genError{
					err:        fmt.Errorf("%s not allowed in JSON", mrb.ClassOf(val).Name()),
					invalidObj: val,
					hasInvalid: true,
				}
			}
			return appendJSONValue(mrb, buf, converted, depth+1, opts)
		}
		if opts.strict {
			return nil, &genError{
				err:        fmt.Errorf("%s not allowed in JSON", mrb.ClassOf(val).Name()),
				invalidObj: val,
				hasInvalid: true,
			}
		}
		// Check if object overrides Object#to_json
		if hasCustomToJSON(mrb, val, "Object") {
			return callCustomToJSON(mrb, buf, val, opts)
		}
		sv, err := mrb.Funcall(val, oruby.Sym_to_s)
		if err != nil {
			return nil, &genError{err: err, isRubyExc: true}
		}
		if !sv.IsString() {
			return nil, &genError{err: errors.New("to_s did not return String"), invalidObj: val, hasInvalid: true}
		}
		s, err := convertRubyStringToUTF8(mrb, sv)
		if err != nil {
			return nil, err
		}
		return appendJSONString(buf, s, opts, val)
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

func raiseJSONGenErr(mrb *oruby.MrbState, err error) oruby.MrbValue {
	var ge *genError
	if errors.As(err, &ge) {
		if ge.isRubyExc {
			return mrb.RaiseError(ge.err)
		}
		clsName := "GeneratorError"
		if ge.isNesting || errors.Is(ge.err, errNesting) {
			clsName = "NestingError"
		}
		cls := jsonErrorClass(mrb, clsName)
		exc, ferr := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(ge.err.Error()))
		if ferr == nil && !exc.IsNil() {
			if ge.hasInvalid {
				mrb.IVSet(exc, mrb.Intern("@invalid_object"), ge.invalidObj)
			}
			mrb.ExcRaise(exc)
			return mrb.NilValue()
		}
		return mrb.Raisef(cls, "%v", ge.err)
	}
	if errors.Is(err, errNesting) {
		return mrb.Raisef(jsonErrorClass(mrb, "NestingError"), "nesting of %v is too deep", err)
	}
	if _, ok := err.(oruby.MrbValue); ok {
		return mrb.RaiseError(err)
	}
	return mrb.Raisef(jsonErrorClass(mrb, "GeneratorError"), "JSON::generate - %v", err)
}

func mrbJSONStringify(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	v := args.Item(0)
	opts := extractJSONGenOpts(mrb, args)
	opts.sortKeys = true
	opts.escapeHTML = true
	var localBuf [512]byte
	buf, err := appendJSONValue(mrb, localBuf[:0], v, opts.initDepth, &opts)
	if err != nil {
		return raiseJSONGenErr(mrb, err)
	}
	return mrb.BytesValue(buf)
}

func mrbJSONGenerate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	v := args.Item(0)
	opts := extractJSONGenOpts(mrb, args)
	var localBuf [512]byte
	buf, err := appendJSONValue(mrb, localBuf[:0], v, opts.initDepth, &opts)
	if err != nil {
		return raiseJSONGenErr(mrb, err)
	}
	return mrb.BytesValue(buf)
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

type jsonParseOpts struct {
	maxNesting         int
	symbolizeNames     bool
	freeze             bool
	allowNaN           bool
	allowTrailingComma bool
	allowDuplicateKey  bool
	createAdditions    bool
	objectClass        oruby.Value
	arrayClass         oruby.Value
	decimalClass       oruby.Value
	onLoad             oruby.Value
	useSlowPath        bool
}

func extractJSONParseOpts(mrb *oruby.MrbState, args oruby.RArgs) jsonParseOpts {
	res := jsonParseOpts{
		maxNesting:        defaultMaxNesting,
		allowDuplicateKey: true,
	}
	apply := func(opts oruby.Value) {
		if !opts.IsHash() {
			return
		}
		getOpt := func(name string) (oruby.Value, bool) {
			sym := mrb.SymbolValue(mrb.Intern(name))
			if mrb.HashKeyP(opts, sym) {
				return mrb.HashGet(opts, sym), true
			}
			str := mrb.StrNew(name)
			if mrb.HashKeyP(opts, str) {
				return mrb.HashGet(opts, str), true
			}
			return mrb.NilValue(), false
		}
		if v, ok := getOpt("max_nesting"); ok {
			if v.IsNil() || v.Type() == oruby.MrbTTFalse {
				res.maxNesting = hardMaxNesting
			} else if v.IsInt() {
				n := int(v.Int64())
				if n <= 0 || n > hardMaxNesting {
					res.maxNesting = hardMaxNesting
				} else {
					res.maxNesting = n
				}
			}
		}
		if v, ok := getOpt("symbolize_names"); ok {
			res.symbolizeNames = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("freeze"); ok && oruby.MrbBoolean(v) {
			res.freeze = true
			res.useSlowPath = true
		}
		if v, ok := getOpt("allow_nan"); ok && oruby.MrbBoolean(v) {
			res.allowNaN = true
			res.useSlowPath = true
		}
		if v, ok := getOpt("allow_trailing_comma"); ok && oruby.MrbBoolean(v) {
			res.allowTrailingComma = true
			res.useSlowPath = true
		}
		if v, ok := getOpt("allow_duplicate_key"); ok {
			res.allowDuplicateKey = oruby.MrbBoolean(v)
			if !res.allowDuplicateKey {
				res.useSlowPath = true
			}
		}
		if v, ok := getOpt("create_additions"); ok && oruby.MrbBoolean(v) {
			res.createAdditions = true
			res.useSlowPath = true
		}
		if v, ok := getOpt("object_class"); ok && !v.IsNil() {
			res.objectClass = v
			res.useSlowPath = true
		}
		if v, ok := getOpt("array_class"); ok && !v.IsNil() {
			res.arrayClass = v
			res.useSlowPath = true
		}
		if v, ok := getOpt("decimal_class"); ok && !v.IsNil() {
			res.decimalClass = v
			res.useSlowPath = true
		}
		if v, ok := getOpt("on_load"); ok && !v.IsNil() {
			res.onLoad = v
			res.useSlowPath = true
		}
	}
	if args.Len() > 1 && !args.Item(1).IsNil() {
		apply(args.Item(1))
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() {
		apply(kw)
	}
	return res
}

func decodeJSONToBatch(dec *jsontext.Decoder, ops *[]oruby.BatchOp, strBuf *[]byte, depth, maxNesting int) error {
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
			// Could be a BigInt or a Float; if it has no '.'/'e'/'E', bail to slow path for BigInt!
			raw := tok.String()
			if !strings.ContainsAny(raw, ".eE") {
				return errUseSlowPath
			}
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
		if depth >= maxNesting {
			return errNesting
		}
		var cnt int64
		for dec.PeekKind() != ']' {
			if err := decodeJSONToBatch(dec, ops, strBuf, depth+1, maxNesting); err != nil {
				return err
			}
			cnt++
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		*ops = append(*ops, oruby.BatchOp{Op: 6, I: cnt})
	case '{':
		if depth >= maxNesting {
			return errNesting
		}
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
			if err := decodeJSONToBatch(dec, ops, strBuf, depth+1, maxNesting); err != nil {
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

var errUseSlowPath = errors.New("use slow path")

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

// Full-featured recursive-descent JSON parser for all Ruby JSON options
type rdParser struct {
	mrb      *oruby.MrbState
	s        string
	pos      int
	opts     jsonParseOpts
	createID string
}

func (p *rdParser) skipWS() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *rdParser) maybeFreeze(v oruby.Value) oruby.Value {
	if p.opts.freeze {
		_, _ = p.mrb.Funcall(v, p.mrb.Intern("freeze"))
	}
	return v
}

func (p *rdParser) maybeOnLoad(v oruby.Value) (oruby.Value, error) {
	if !p.opts.onLoad.IsNil() {
		res, err := p.mrb.Funcall(p.opts.onLoad, p.mrb.Intern("call"), v)
		if err != nil {
			return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
		}
		return res, nil
	}
	return v, nil
}

func (p *rdParser) parseValue(depth int) (oruby.Value, error) {
	p.skipWS()
	if p.pos >= len(p.s) {
		return p.mrb.NilValue(), errors.New("unexpected end of input")
	}
	var val oruby.Value
	var err error
	switch p.s[p.pos] {
	case '{':
		val, err = p.parseObject(depth)
	case '[':
		val, err = p.parseArray(depth)
	case '"':
		var str string
		str, err = p.parseString()
		if err == nil {
			val = p.maybeFreeze(p.mrb.StrNew(str))
		}
	case 't':
		if strings.HasPrefix(p.s[p.pos:], "true") {
			p.pos += 4
			val = p.mrb.TrueValue()
		} else {
			err = errors.New("unexpected token")
		}
	case 'f':
		if strings.HasPrefix(p.s[p.pos:], "false") {
			p.pos += 5
			val = p.mrb.FalseValue()
		} else {
			err = errors.New("unexpected token")
		}
	case 'n':
		if strings.HasPrefix(p.s[p.pos:], "null") {
			p.pos += 4
			val = p.mrb.NilValue()
		} else {
			err = errors.New("unexpected token")
		}
	case 'N':
		if p.opts.allowNaN && strings.HasPrefix(p.s[p.pos:], "NaN") {
			p.pos += 3
			val = p.mrb.FloatValue(math.NaN())
		} else {
			err = errors.New("unexpected token 'NaN'")
		}
	case 'I':
		if p.opts.allowNaN && strings.HasPrefix(p.s[p.pos:], "Infinity") {
			p.pos += 8
			val = p.mrb.FloatValue(math.Inf(1))
		} else {
			err = errors.New("unexpected token 'Infinity'")
		}
	case '-':
		if p.opts.allowNaN && strings.HasPrefix(p.s[p.pos:], "-Infinity") {
			p.pos += 9
			val = p.mrb.FloatValue(math.Inf(-1))
		} else {
			val, err = p.parseNumber()
		}
	default:
		if p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			val, err = p.parseNumber()
		} else {
			err = fmt.Errorf("unexpected character %q", p.s[p.pos])
		}
	}
	if err != nil {
		return p.mrb.NilValue(), err
	}
	return p.maybeOnLoad(val)
}

func (p *rdParser) parseHex4() (rune, error) {
	if p.pos+4 > len(p.s) {
		return 0, errors.New("incomplete unicode escape")
	}
	var r rune
	for i := 0; i < 4; i++ {
		c := p.s[p.pos+i]
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			r |= rune(c - 'A' + 10)
		default:
			return 0, errors.New("invalid hex digit in unicode escape")
		}
	}
	p.pos += 4
	return r, nil
}

func (p *rdParser) parseString() (string, error) {
	if p.pos >= len(p.s) || p.s[p.pos] != '"' {
		return "", errors.New("expected '\"'")
	}
	p.pos++
	start := p.pos
	var sb strings.Builder
	hasEscape := false
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '"' {
			if !hasEscape {
				res := p.s[start:p.pos]
				p.pos++
				return res, nil
			}
			sb.WriteString(p.s[start:p.pos])
			p.pos++
			return sb.String(), nil
		}
		if c < 0x20 {
			return "", errors.New("unescaped control character in string")
		}
		if c == '\\' {
			if !hasEscape {
				hasEscape = true
			}
			sb.WriteString(p.s[start:p.pos])
			p.pos++
			if p.pos >= len(p.s) {
				return "", errors.New("unterminated escape sequence")
			}
			esc := p.s[p.pos]
			p.pos++
			switch esc {
			case '"', '\\', '/':
				sb.WriteByte(esc)
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				r1, err := p.parseHex4()
				if err != nil {
					return "", err
				}
				if utf16.IsSurrogate(r1) {
					if r1 >= 0xDC00 {
						return "", errors.New("unexpected low surrogate")
					}
					if p.pos+2 > len(p.s) || p.s[p.pos] != '\\' || p.s[p.pos+1] != 'u' {
						return "", errors.New("incomplete surrogate pair")
					}
					p.pos += 2
					r2, err := p.parseHex4()
					if err != nil {
						return "", err
					}
					if r2 < 0xDC00 || r2 > 0xDFFF {
						return "", errors.New("invalid low surrogate")
					}
					sb.WriteRune(utf16.DecodeRune(r1, r2))
				} else {
					sb.WriteRune(r1)
				}
			default:
				return "", fmt.Errorf("invalid escape character \\%c", esc)
			}
			start = p.pos
		} else {
			p.pos++
		}
	}
	return "", errors.New("unterminated string")
}

func (p *rdParser) parseNumber() (oruby.Value, error) {
	start := p.pos
	if p.pos < len(p.s) && p.s[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.s) {
		return p.mrb.NilValue(), errors.New("invalid number")
	}
	if p.s[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			return p.mrb.NilValue(), errors.New("leading zero in number")
		}
	} else if p.s[p.pos] >= '1' && p.s[p.pos] <= '9' {
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return p.mrb.NilValue(), errors.New("invalid number")
	}
	isFloat := false
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		isFloat = true
		p.pos++
		if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
			return p.mrb.NilValue(), errors.New("invalid fraction in number")
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.s) && (p.s[p.pos] == 'e' || p.s[p.pos] == 'E') {
		isFloat = true
		p.pos++
		if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
			return p.mrb.NilValue(), errors.New("invalid exponent in number")
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	raw := p.s[start:p.pos]
	if isFloat {
		if !p.opts.decimalClass.IsNil() {
			v, err := p.mrb.Funcall(p.opts.decimalClass, p.mrb.Intern("new"), p.mrb.StrNew(raw))
			if err != nil {
				return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
			return v, nil
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return p.mrb.NilValue(), err
		}
		return p.mrb.FloatValue(f), nil
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return p.mrb.Int64Value(i), nil
	}
	// BigInt fallback via Ruby String#to_i
	v, err := p.mrb.Funcall(p.mrb.StrNew(raw), p.mrb.Intern("to_i"))
	if err != nil {
		return p.mrb.NilValue(), err
	}
	return v, nil
}

func (p *rdParser) parseArray(depth int) (oruby.Value, error) {
	if depth >= p.opts.maxNesting {
		return p.mrb.NilValue(), errNesting
	}
	p.pos++ // skip '['
	var ary oruby.Value
	custom := !p.opts.arrayClass.IsNil()
	if custom {
		var err error
		ary, err = p.mrb.Funcall(p.opts.arrayClass, p.mrb.Intern("new"))
		if err != nil {
			return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
		}
	} else {
		ary = p.mrb.AryNew().Value()
	}
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return p.maybeFreeze(ary), nil
	}
	for {
		elem, err := p.parseValue(depth + 1)
		if err != nil {
			return p.mrb.NilValue(), err
		}
		if custom {
			if _, err := p.mrb.Funcall(ary, p.mrb.Intern("<<"), elem); err != nil {
				return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
		} else {
			p.mrb.AryPush(ary, elem)
		}
		p.skipWS()
		if p.pos >= len(p.s) {
			return p.mrb.NilValue(), errors.New("unterminated array")
		}
		if p.s[p.pos] == ']' {
			p.pos++
			return p.maybeFreeze(ary), nil
		}
		if p.s[p.pos] != ',' {
			return p.mrb.NilValue(), errors.New("expected ',' or ']' in array")
		}
		p.pos++
		p.skipWS()
		if p.opts.allowTrailingComma && p.pos < len(p.s) && p.s[p.pos] == ']' {
			p.pos++
			return p.maybeFreeze(ary), nil
		}
	}
}

func (p *rdParser) parseObject(depth int) (oruby.Value, error) {
	if depth >= p.opts.maxNesting {
		return p.mrb.NilValue(), errNesting
	}
	p.pos++ // skip '{'
	var obj oruby.Value
	custom := !p.opts.objectClass.IsNil()
	if custom {
		var err error
		obj, err = p.mrb.Funcall(p.opts.objectClass, p.mrb.Intern("new"))
		if err != nil {
			return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
		}
	} else {
		obj = p.mrb.HashNew().Value()
	}
	var seen map[string]struct{}
	if !p.opts.allowDuplicateKey {
		seen = make(map[string]struct{})
	}
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return p.maybeFreeze(obj), nil
	}
	var createClassName string
	for {
		p.skipWS()
		kstr, err := p.parseString()
		if err != nil {
			return p.mrb.NilValue(), err
		}
		if seen != nil {
			if _, dup := seen[kstr]; dup {
				return p.mrb.NilValue(), fmt.Errorf("duplicate key %q", kstr)
			}
			seen[kstr] = struct{}{}
		}
		p.skipWS()
		if p.pos >= len(p.s) || p.s[p.pos] != ':' {
			return p.mrb.NilValue(), errors.New("expected ':' after object key")
		}
		p.pos++
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return p.mrb.NilValue(), err
		}
		if p.opts.createAdditions && kstr == p.createID && val.IsString() {
			createClassName = p.mrb.String(val)
		}
		var kval oruby.Value
		if p.opts.symbolizeNames {
			kval = p.mrb.SymbolValue(p.mrb.Intern(kstr))
		} else {
			kval = p.maybeFreeze(p.mrb.StrNew(kstr))
		}
		if custom {
			if _, err := p.mrb.Funcall(obj, p.mrb.Intern("[]="), kval, val); err != nil {
				return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
		} else {
			p.mrb.HashSet(obj, kval, val)
		}
		p.skipWS()
		if p.pos >= len(p.s) {
			return p.mrb.NilValue(), errors.New("unterminated object")
		}
		if p.s[p.pos] == '}' {
			p.pos++
			break
		}
		if p.s[p.pos] != ',' {
			return p.mrb.NilValue(), errors.New("expected ',' or '}' in object")
		}
		p.pos++
		p.skipWS()
		if p.opts.allowTrailingComma && p.pos < len(p.s) && p.s[p.pos] == '}' {
			p.pos++
			break
		}
	}
	if p.opts.createAdditions && createClassName != "" {
		klass, err := p.mrb.Eval(createClassName)
		if err == nil && !klass.IsNil() && p.mrb.RespondTo(klass, p.mrb.Intern("json_create")) {
			created, cerr := p.mrb.Funcall(klass, p.mrb.Intern("json_create"), obj)
			if cerr != nil {
				return p.mrb.NilValue(), &genError{err: cerr, isRubyExc: true}
			}
			return created, nil
		}
	}
	return p.maybeFreeze(obj), nil
}

func mrbJSONParse(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	arg := args.Item(0)
	if arg.Type() != oruby.MrbTTString {
		if mrb.RespondTo(arg, mrb.Intern("to_str")) {
			sv, err := mrb.Funcall(arg, mrb.Intern("to_str"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			if !sv.IsString() {
				return mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
			}
			arg = sv
		} else {
			return mrb.Raise(mrb.ETypeError(), "JSON::parse - string argument expected")
		}
	}

	// Handle non-UTF-8 Ruby string encodings (UTF-16/UTF-32/etc.)
	encName := rubyStringEncodingName(mrb, arg)
	if encName != "UTF-8" && encName != "US-ASCII" && encName != "ASCII-8BIT" {
		if mrb.RespondTo(arg, mrb.Intern("encode")) {
			utf8Enc, eerr := mrb.Eval("Encoding::UTF_8")
			if eerr == nil {
				conv, cerr := mrb.Funcall(arg, mrb.Intern("encode"), utf8Enc)
				if cerr != nil {
					return mrb.RaiseError(cerr)
				}
				if conv.IsString() {
					arg = conv
				}
			}
		}
	}

	ptr, n := oruby.RStringMem(arg)
	var inBytes []byte
	if n > 0 && ptr != nil {
		inBytes = unsafe.Slice(ptr, n)
	}
	if !utf8.Valid(inBytes) {
		return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "JSON::parse - invalid utf-8 byte sequence")
	}

	opts := extractJSONParseOpts(mrb, args)

	if !opts.useSlowPath {
		reader := bytes.NewReader(inBytes)
		dec := jsontext.NewDecoder(reader)
		ops := make([]oruby.BatchOp, 0, 64)
		strBuf := make([]byte, 0, 256)

		err := decodeJSONToBatch(dec, &ops, &strBuf, 0, opts.maxNesting)
		if err == nil {
			if _, terr := dec.ReadToken(); terr != io.EOF {
				return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "JSON::parse - unexpected trailing data")
			}
			res := mrb.ExecBatchOps(ops, strBuf)
			if opts.symbolizeNames {
				return symbolizeKeys(mrb, res)
			}
			return res
		}
		if !errors.Is(err, errUseSlowPath) {
			if errors.Is(err, errNesting) {
				return mrb.Raisef(jsonErrorClass(mrb, "NestingError"), "JSON::parse - %v", err)
			}
			return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "JSON::parse - %v", err)
		}
	}

	// Slow/full-options path
	createID := "json_class"
	if opts.createAdditions {
		if cid, cerr := mrb.Funcall(mrb.ModuleGet("JSON"), mrb.Intern("create_id")); cerr == nil && cid.IsString() {
			createID = mrb.String(cid)
		}
	}
	p := &rdParser{
		mrb:      mrb,
		s:        string(inBytes),
		opts:     opts,
		createID: createID,
	}
	val, perr := p.parseValue(0)
	if perr != nil {
		var ge *genError
		if errors.As(perr, &ge) && ge.isRubyExc {
			return mrb.RaiseError(ge.err)
		}
		if errors.Is(perr, errNesting) {
			return mrb.Raisef(jsonErrorClass(mrb, "NestingError"), "JSON::parse - %v", perr)
		}
		return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "JSON::parse - %v", perr)
	}
	p.skipWS()
	if p.pos < len(p.s) {
		return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "JSON::parse - unexpected trailing data")
	}
	return val
}
