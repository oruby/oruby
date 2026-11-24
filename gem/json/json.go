package json

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
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
	// Float::MAX.to_i = (2^53 - 1) << 971
	rfc8785MaxInt = new(big.Int).Lsh(big.NewInt((1<<53)-1), 971)
)

const (
	defaultMaxNesting = 100
	hardMaxNesting    = 10000
)

type genError struct {
	err        error
	invalidObj oruby.Value
	hasInvalid bool
	causeExc   oruby.Value
	hasCause   bool
	isNesting  bool
	isRubyExc  bool
	isTypeErr  bool
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
		parseErr := mrb.DefineClassUnder(jsonModule, "ParserError", jsonErr)
		mrb.DefineClassUnder(jsonModule, "NestingError", parseErr)
		mrb.DefineClassUnder(jsonModule, "GeneratorError", jsonErr)

		jsonOptArgs := mrb.ArgsArg(1, 2) | mrb.ArgsKey(0, 1)
		jsonModule.DefineClassMethod("stringify", mrbJSONStringify, jsonOptArgs)
		jsonModule.DefineClassMethod("__generate", mrbJSONGenerate, jsonOptArgs)
		jsonModule.DefineClassMethod("__generate_raw", mrbJSONGenerateRaw, jsonOptArgs)
		jsonModule.DefineClassMethod("__parse", mrbJSONParse, jsonOptArgs)
		initResumableParser(mrb, jsonModule)

		_, _ = mrb.Eval(`
module JSON
  @create_id = "json_class"
  @state = nil
  @parser = nil
  @generator = nil

  class << self
    attr_accessor :create_id, :state, :parser, :generator

    def __raise_gen_error(msg, invalid_object, has_invalid, cause_exc)
      exc = GeneratorError.new(msg)
      exc.instance_variable_set(:@invalid_object, invalid_object) if has_invalid
      if cause_exc
        raise exc, cause: cause_exc
      else
        raise exc
      end
    end

    def __encode_utf8(str)
      [str.encode(Encoding::UTF_8), nil]
    rescue EncodingError => e
      [nil, e]
    end
  end

  class ParserError
    attr_reader :line, :column, :byte_offset

    def initialize(message = nil, line: nil, column: nil, byte_offset: nil, json_path: nil)
      super(message)
      @line = line
      @column = column
      @byte_offset = byte_offset
      @json_path = json_path
    end

    def json_path
      return @json_path if String === @json_path

      if Array === @json_path
        path = build_json_path(@json_path)
        @json_path = path unless frozen?
        return path
      end
      @json_path
    end

    private

    def build_json_path(segments)
      error = false
      parts = []
      segments.each do |segment|
        break if error
        if Integer === segment
          parts << "[#{segment}]"
        elsif String === segment || Symbol === segment
          seg = segment.to_s
          if !seg.valid_encoding?
            seg = seg.b.gsub(/["\\]/n) { |m| m == '"' ? '\"' : '\\\\' }.force_encoding(Encoding::UTF_8)
            parts << %{["#{seg}"]}
          elsif seg.match?(/\A[a-zA-Z\$\_][a-zA-Z\$\_0-9]*\z/)
            parts << ".#{seg}"
          else
            seg = seg.gsub(/["\\]/) { |m| m == '"' ? '\"' : '\\\\' }
            parts << %{["#{seg}"]}
          end
        else
          error = true
        end
      end
      "$#{parts.join}".freeze
    end
  end

  class GeneratorError
    attr_reader :invalid_object

    def initialize(message = nil, invalid_object: nil)
      super(message)
      @invalid_object = invalid_object
    end
  end

  module Ext
    module Generator
      class State
        VALID_OPTIONS = [
          :indent, :space, :space_before, :object_nl, :array_nl,
          :max_nesting, :allow_nan, :ascii_only, :script_safe, :escape_slash,
          :strict, :depth, :as_json, :buffer_initial_length, :check_circular,
          :sort_keys, :allow_duplicate_key, :rfc8785
        ].freeze

        DEFAULT_SORT_KEYS = ->(hash) { hash.sort_by { |k, _| k.to_s }.to_h }.freeze

        attr_reader :indent, :space, :space_before, :object_nl, :array_nl,
                    :max_nesting, :allow_nan, :ascii_only, :script_safe,
                    :strict, :depth, :as_json, :buffer_initial_length,
                    :sort_keys, :allow_duplicate_key, :rfc8785

        alias escape_slash script_safe
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
          @as_json = false
          @buffer_initial_length = 1024
          @sort_keys = false
          @allow_duplicate_key = false
          @rfc8785 = false
          configure(opts) if opts
        end

        def indent=(v); check_frozen!; @indent = check_str_opt(:indent, v); end
        def space=(v); check_frozen!; @space = check_str_opt(:space, v); end
        def space_before=(v); check_frozen!; @space_before = check_str_opt(:space_before, v); end
        def object_nl=(v); check_frozen!; @object_nl = check_str_opt(:object_nl, v); end
        def array_nl=(v); check_frozen!; @array_nl = check_str_opt(:array_nl, v); end
        def max_nesting=(v)
          check_frozen!
          @max_nesting = (v == false || v.nil?) ? false : check_int_opt(:max_nesting, v)
        end
        def allow_nan=(v); check_frozen!; @allow_nan = !!v; end
        def ascii_only=(v); check_frozen!; @ascii_only = !!v; end
        def script_safe=(v); check_frozen!; @script_safe = !!v; end
        alias escape_slash= script_safe=
        def strict=(v); check_frozen!; @strict = !!v; end
        def allow_duplicate_key=(v); check_frozen!; @allow_duplicate_key = !!v; end
        def rfc8785=(v); check_frozen!; @rfc8785 = !!v; end

        def depth=(val)
          check_frozen!
          d = check_int_opt(:depth, val)
          raise ArgumentError, "depth must be non-negative" if d < 0
          raise RangeError, "depth too large" if d > 0x3fffffff
          @depth = d
        end

        def as_json=(v)
          check_frozen!
          @as_json = check_callable_opt(:as_json, v)
        end

        def buffer_initial_length=(v)
          check_frozen!
          return if v.nil?
          n = check_int_opt(:buffer_initial_length, v)
          @buffer_initial_length = n <= 0 ? 1024 : n
        end

        def sort_keys=(v)
          check_frozen!
          if !v
            @sort_keys = false
          elsif v.is_a?(Proc)
            @sort_keys = v
          elsif v.respond_to?(:call)
            @sort_keys = v
          else
            @sort_keys = DEFAULT_SORT_KEYS
          end
        end

        def check_circular?
          @max_nesting != false && @max_nesting != 0
        end

        def validate_rfc8785!
          return unless @rfc8785
          raise ArgumentError, "indent cannot be used with rfc8785" unless @indent.empty?
          raise ArgumentError, "space cannot be used with rfc8785" unless @space.empty?
          raise ArgumentError, "space_before cannot be used with rfc8785" unless @space_before.empty?
          raise ArgumentError, "object_nl cannot be used with rfc8785" unless @object_nl.empty?
          raise ArgumentError, "array_nl cannot be used with rfc8785" unless @array_nl.empty?
          raise ArgumentError, "allow_nan cannot be used with rfc8785" if @allow_nan
          raise ArgumentError, "ascii_only cannot be used with rfc8785" if @ascii_only
          raise ArgumentError, "script_safe cannot be used with rfc8785" if @script_safe
        end

        def configure(opts)
          check_frozen!
          if opts.respond_to?(:to_hash)
            opts = opts.to_hash
          elsif opts.respond_to?(:to_h)
            opts = opts.to_h
          else
            raise TypeError, "can't convert #{opts.class} into Hash"
          end
          unknown = []
          opts.each_key do |k|
            key = k.to_sym
            unknown << key unless VALID_OPTIONS.include?(key)
          end
          unless unknown.empty?
            plural = unknown.length > 1 ? "s" : ""
            raise ArgumentError, "unknown keyword#{plural}: #{unknown.map(&:to_s).join(', ')}"
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
              @max_nesting = (v == false || v.nil?) ? false : check_int_opt(key, v)
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
              self.depth = v
            when :as_json
              @as_json = check_callable_opt(key, v)
            when :buffer_initial_length
              self.buffer_initial_length = v
            when :sort_keys
              self.sort_keys = v
            when :allow_duplicate_key
              @allow_duplicate_key = !!v
            when :rfc8785
              @rfc8785 = !!v
            end
          end
          if @rfc8785
            if (opts.key?(:sort_keys) && opts[:sort_keys] == false) || (opts.key?("sort_keys") && opts["sort_keys"] == false)
              raise ArgumentError, "sort_keys cannot be used with rfc8785"
            end
            if (opts.key?(:allow_duplicate_key) && opts[:allow_duplicate_key] == true) || (opts.key?("allow_duplicate_key") && opts["allow_duplicate_key"] == true)
              raise ArgumentError, "allow_duplicate_key cannot be used with rfc8785"
            end
            validate_rfc8785!
            @sort_keys = DEFAULT_SORT_KEYS unless @sort_keys
            @allow_duplicate_key = false
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
            sort_keys: @sort_keys,
            allow_duplicate_key: @allow_duplicate_key,
            rfc8785: @rfc8785,
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
          validate_rfc8785!
          str = JSON.__generate(obj, self)
          if io
            io.write(str)
            io
          else
            str
          end
        end

        private

        def check_frozen!
          raise FrozenError, "can't modify frozen #{self.class}" if frozen?
        end

        def check_str_opt(key, v)
          return "" if v.nil?
          if v.is_a?(String)
            v
          elsif v.respond_to?(:to_str)
            s = v.to_str
            raise TypeError, "can't convert #{v.class} to String" unless s.is_a?(String)
            s
          else
            raise TypeError, "no implicit conversion of #{v.class} into String for #{key}"
          end
        end

        def check_int_opt(key, v)
          return v if v.is_a?(Integer)
          if v.respond_to?(:to_int)
            i = v.to_int
            raise TypeError, "can't convert #{v.class} to Integer" unless i.is_a?(Integer)
            return i
          end
          raise TypeError, "no implicit conversion of #{v.class} into Integer for #{key}"
        end

        def check_callable_opt(key, v)
          return false if v.nil? || v == false
          return v if v.is_a?(Proc)
          if v.is_a?(Method)
            m = v
            return proc { |*a, **kw, &b| m.call(*a, **kw, &b) }
          end
          if v.respond_to?(:to_proc)
            p = v.to_proc
            raise TypeError, "can't convert #{v.class} to Proc (#{v.class}#to_proc gives #{p.class})" unless p.is_a?(Proc)
            return p
          end
          return v if v.respond_to?(:call)
          raise TypeError, "#{key} must respond to #call or #to_proc, got #{v.class}"
        end
      end
    end
  end

  State = Ext::Generator::State

  SAFE_STATE_PROTOTYPE = State.new.freeze
  FAST_STATE_PROTOTYPE = State.new(max_nesting: false).freeze
  PRETTY_STATE_PROTOTYPE = State.new(indent: "  ", space: " ", object_nl: "\n", array_nl: "\n").freeze

  class Fragment
    def initialize(raw)
      if raw.is_a?(String)
        @raw = raw
      elsif raw.respond_to?(:to_str)
        s = raw.to_str
        raise TypeError, "no implicit conversion of #{raw.class} into String" unless s.is_a?(String)
        @raw = s
      else
        raise TypeError, "no implicit conversion of #{raw.class} into String"
      end
    end

    def to_s
      @raw
    end
    alias json to_s

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

  module Ext
    class Parser
      VALID_OPTIONS = [
        :max_nesting, :allow_nan, :symbolize_names, :create_additions,
        :object_class, :array_class, :decimal_class, :freeze,
        :allow_trailing_comma, :allow_duplicate_key, :on_load,
        :match_string, :allow_control_characters, :allow_invalid_escape,
        :allow_invalid_escapes, :allow_comments, :deprecated_create_additions
      ].freeze

      class Config
        def initialize(opts = nil)
          raise FrozenError, "can't modify frozen #{self.class}" if frozen?
          @opts = Parser.validate_opts(opts)
        end

        def parse(source)
          JSON.__parse(source, @opts)
        end
      end

      def self.validate_opts(opts)
        return nil if opts.nil?
        unless opts.is_a?(Hash)
          if opts.respond_to?(:to_hash)
            opts = opts.to_hash
          else
            raise TypeError, "no implicit conversion of #{opts.class} into Hash"
          end
        end
        return nil if opts.empty?
        unknown = []
        out = {}
        opts.each do |k, v|
          key = k.to_sym
          unless VALID_OPTIONS.include?(key)
            unknown << key
            next
          end
          if key == :on_load
            if v.nil? || v == false
              out[key] = nil
            elsif v.is_a?(Proc)
              out[key] = v
            elsif v.is_a?(Method)
              m = v
              out[key] = proc { |*a, **kw, &b| m.call(*a, **kw, &b) }
            elsif v.respond_to?(:to_proc)
              p = v.to_proc
              raise TypeError, "can't convert #{v.class} to Proc (#{v.class}#to_proc gives #{p.class})" unless p.is_a?(Proc)
              out[key] = p
            elsif v.respond_to?(:call)
              out[key] = v
            else
              raise TypeError, "on_load must respond to #call or #to_proc"
            end
          else
            out[key] = v
          end
        end
        unless unknown.empty?
          plural = unknown.length > 1 ? "s" : ""
          raise ArgumentError, "unknown keyword#{plural}: #{unknown.map(&:to_s).join(', ')}"
        end
        out
      end

      attr_reader :source

      def initialize(source, opts = nil)
        if source.is_a?(String)
          @source = source
        elsif source.respond_to?(:to_str)
          s = source.to_str
          raise TypeError, "can't convert #{source.class} to String" unless s.is_a?(String)
          @source = s
        else
          raise TypeError, "no implicit conversion of #{source.class} into String"
        end
        @opts = Parser.validate_opts(opts)
      end

      def parse
        JSON.__parse(@source, @opts)
      end
    end
  end

  Parser = Ext::Parser

  @state = State
  @parser = Parser
  @generator = Ext::Generator

  class Coder
    def initialize(opts = nil, &block)
      merged = {}
      if opts.is_a?(Hash)
        opts.each { |k, v| merged[k.to_sym] = v }
      elsif !opts.nil?
        raise TypeError, "expected Hash"
      end
      if merged.key?(:strict) && !merged[:strict]
        raise ArgumentError, "JSON::Coder requires strict mode"
      end
      merged[:strict] = true
      if block
        merged[:as_json] = block
      end
      @gen_opts = {}
      @parse_opts = {}
      merged.each do |key, v|
        in_gen = State::VALID_OPTIONS.include?(key)
        in_parse = Parser::VALID_OPTIONS.include?(key)
        unless in_gen || in_parse
          raise ArgumentError, "unknown keyword: #{key}"
        end
        @gen_opts[key] = v if in_gen
        @parse_opts[key] = v if in_parse
      end
      @parse_opts = Parser.validate_opts(@parse_opts) || {}
      @state = State.new(@gen_opts)
    end

    def dump(obj, io = nil)
      @state.generate(obj, io)
    end

    def load(source)
      JSON.__parse(source, @parse_opts)
    end
  end

  def self.[](object, opts = nil)
    if object.is_a?(String)
      parse(object, opts)
    elsif !object.is_a?(Hash) && !object.is_a?(Array) && object.respond_to?(:to_str)
      s = object.to_str
      s.is_a?(String) ? parse(s, opts) : generate(object, opts)
    else
      generate(object, opts)
    end
  end

  def parse(source, opts = nil)
    validated = Parser.validate_opts(opts)
    JSON.__parse(source, validated)
  end
  module_function :parse

  def parse!(source, opts = nil)
    merged = { max_nesting: false, allow_nan: true }
    if opts.is_a?(Hash)
      validated = Parser.validate_opts(opts)
      merged.merge!(validated) if validated
    end
    JSON.__parse(source, merged)
  end
  module_function :parse!

  def generate(obj, opts = nil)
    state =
      if opts.is_a?(State)
        opts
      elsif opts.nil?
        SAFE_STATE_PROTOTYPE
      else
        State.from_state(opts)
      end
    state.validate_rfc8785!
    JSON.__generate(obj, state)
  end
  module_function :generate
  alias unparse generate
  module_function :unparse

  def fast_generate(obj, opts = nil)
    state =
      if opts.is_a?(State)
        opts
      elsif opts.nil?
        FAST_STATE_PROTOTYPE
      else
        s = State.new(max_nesting: false)
        s.configure(opts)
        s
      end
    state.validate_rfc8785!
    JSON.__generate(obj, state)
  end
  module_function :fast_generate
  alias fast_unparse fast_generate
  module_function :fast_unparse

  def pretty_generate(obj, opts = nil)
    state =
      if opts.is_a?(State)
        opts
      elsif opts.nil?
        PRETTY_STATE_PROTOTYPE
      else
        s = State.new(indent: "  ", space: " ", object_nl: "\n", array_nl: "\n")
        s.configure(opts)
        s
      end
    state.validate_rfc8785!
    JSON.__generate(obj, state)
  end
  module_function :pretty_generate
  alias pretty_unparse pretty_generate
  module_function :pretty_unparse

  def dump(obj, anIO = nil, limit = nil)
    raw_opts = {}
    if anIO.is_a?(Hash) && limit.nil?
      raw_opts.merge!(anIO)
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
    if limit.is_a?(Hash)
      raw_opts.merge!(limit)
    elsif !limit.nil?
      raw_opts[:max_nesting] = limit
    end
    has_allow_nan = raw_opts.key?(:allow_nan) || raw_opts.key?("allow_nan")
    rfc = !!(raw_opts[:rfc8785] || raw_opts["rfc8785"])
    opts = {}
    opts[:allow_nan] = true unless has_allow_nan || rfc
    opts.merge!(raw_opts)
    state = State.new(opts)
    state.validate_rfc8785!
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

  def load(source, proc = nil, opts = nil)
    if proc.is_a?(Hash) && opts.nil?
      opts = proc
      proc = nil
    end
    allow_blank = true
    extra = {}
    if opts.is_a?(Hash)
      opts.each do |k, v|
        if k.to_sym == :allow_blank
          allow_blank = !!v
        else
          extra[k] = v
        end
      end
    end
    unless source.is_a?(String)
      if source.respond_to?(:to_str)
        source = source.to_str
      elsif source.respond_to?(:read)
        source = source.read
      elsif source.respond_to?(:to_io)
        source = source.to_io.read
      end
    end
    if allow_blank && (source.nil? || (String === source && source.empty?))
      source = "null"
    end
    raise TypeError, "no implicit conversion of #{source.class} into String" unless source.is_a?(String)
    merged = { allow_nan: true }
    if proc
      merged[:on_load] = proc.respond_to?(:to_proc) ? proc.to_proc : proc
    end
    unless extra.empty?
      validated = Parser.validate_opts(extra)
      merged.merge!(validated) if validated
    end
    JSON.__parse(source, merged)
  end
  module_function :load
  alias restore load
  module_function :restore

  def unsafe_load(source, proc = nil, opts = nil)
    if proc.is_a?(Hash) && opts.nil?
      opts = proc
      proc = nil
    end
    merged = { max_nesting: false, create_additions: true }
    merged.merge!(opts) if opts.is_a?(Hash)
    load(source, proc, merged)
  end
  module_function :unsafe_load

  def load_file(path, opts = nil)
    parse(File.binread(path), opts)
  end
  module_function :load_file

  def load_file!(path, opts = nil)
    parse!(File.binread(path), opts)
  end
  module_function :load_file!
end

module Kernel
  private

  def JSON(object, *args)
    if object.is_a?(String)
      ::JSON.parse(object, *args)
    elsif !object.is_a?(Hash) && !object.is_a?(Array) && object.respond_to?(:to_str)
      s = object.to_str
      s.is_a?(String) ? ::JSON.parse(s, *args) : ::JSON.generate(object, *args)
    else
      ::JSON.generate(object, *args)
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
    state = args.first.is_a?(::JSON::State) ? args.first : (args.empty? ? ::JSON::SAFE_STATE_PROTOTYPE : ::JSON::State.from_state(args.first))
    state.validate_rfc8785!
    ::JSON.__generate_raw(self, state)
  end
end
`)
		return nil
	})
}

type jsonGenOpts struct {
	indent            string
	space             string
	spaceBefore       string
	objectNL          string
	arrayNL           string
	maxNesting        int
	initDepth         int
	allowNaN          bool
	asciiOnly         bool
	scriptSafe        bool
	strict            bool
	sortKeys          bool
	rfc8785           bool
	allowDuplicateKey bool
	escapeHTML        bool
	skipRootToJSON    bool
	inAsJSON          bool
	asJSON            oruby.Value
	sortKeysProc      oruby.Value
	stateVal          oruby.Value
}

func defaultGenOpts() jsonGenOpts {
	return jsonGenOpts{
		maxNesting:        defaultMaxNesting,
		allowDuplicateKey: false,
	}
}

func extractJSONGenOpts(mrb *oruby.MrbState, args oruby.RArgs) jsonGenOpts {
	res := defaultGenOpts()
	jsonMod := mrb.ModuleGet("JSON")
	stateCls := mrb.ClassGetUnder(jsonMod, "State")
	defaultSortProc := mrb.ConstGet(stateCls, mrb.Intern("DEFAULT_SORT_KEYS"))

	applySortKeys := func(v oruby.Value) {
		if v.IsNil() || v.Type() == oruby.MrbTTFalse {
			res.sortKeys = false
			res.sortKeysProc = mrb.NilValue()
		} else if !defaultSortProc.IsNil() && mrb.ObjEqual(v, defaultSortProc) {
			res.sortKeys = true
			res.sortKeysProc = mrb.NilValue()
		} else if mrb.RespondTo(v, mrb.Intern("call")) {
			res.sortKeys = false
			res.sortKeysProc = v
		} else {
			res.sortKeys = true
			res.sortKeysProc = mrb.NilValue()
		}
	}

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
		res.rfc8785 = oruby.MrbBoolean(mrb.IVGet(st, mrb.Intern("@rfc8785")))
		applySortKeys(mrb.IVGet(st, mrb.Intern("@sort_keys")))
		if v := mrb.IVGet(st, mrb.Intern("@allow_duplicate_key")); !v.IsNil() {
			res.allowDuplicateKey = oruby.MrbBoolean(v)
		}
		if v := mrb.IVGet(st, mrb.Intern("@as_json")); !v.IsNil() && v.Type() != oruby.MrbTTFalse {
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
		if v, ok := getOpt("rfc8785"); ok {
			res.rfc8785 = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("sort_keys"); ok {
			applySortKeys(v)
		}
		if v, ok := getOpt("allow_duplicate_key"); ok {
			res.allowDuplicateKey = oruby.MrbBoolean(v)
		}
		if v, ok := getOpt("as_json"); ok && !v.IsNil() && v.Type() != oruby.MrbTTFalse {
			res.asJSON = v
		}
	}

	if args.Len() > 1 && !args.Item(1).IsNil() {
		o := args.Item(1)
		if !stateCls.IsNil() && mrb.ObjIsKindOf(o, stateCls) {
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

func isScriptSafeLT(s string, i int) bool {
	rem := s[i+1:]
	if len(rem) >= 3 && rem[0] == '!' && rem[1] == '-' && rem[2] == '-' {
		return true
	}
	if len(rem) >= 6 && strings.EqualFold(rem[:6], "script") {
		return true
	}
	if len(rem) >= 7 && rem[0] == '/' && strings.EqualFold(rem[1:7], "script") {
		return true
	}
	return false
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
			case '<':
				if opts.escapeHTML || (opts.scriptSafe && isScriptSafeLT(s, i)) {
					buf = append(buf, s[start:i]...)
					buf = appendUnicodeEscape(buf, '<')
					i++
					start = i
				} else {
					i++
				}
			case '>', '&':
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
			if opts.asciiOnly {
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

func compareUTF16(a, b string) int {
	ia, ib := 0, 0
	var qa, qb [2]uint16
	na, nb := 0, 0
	pa, pb := 0, 0
	nextUnit := func(s string, idx *int, q *[2]uint16, p, n *int) (uint16, bool) {
		if *p < *n {
			u := q[*p]
			*p++
			return u, true
		}
		if *idx >= len(s) {
			return 0, false
		}
		r, sz := utf8.DecodeRuneInString(s[*idx:])
		*idx += sz
		if r <= 0xFFFF {
			return uint16(r), true
		}
		r1, r2 := utf16.EncodeRune(r)
		q[0] = uint16(r1)
		q[1] = uint16(r2)
		*p = 1
		*n = 2
		return q[0], true
	}
	for {
		ua, oka := nextUnit(a, &ia, &qa, &pa, &na)
		ub, okb := nextUnit(b, &ib, &qb, &pb, &nb)
		if !oka && !okb {
			return 0
		}
		if !oka {
			return -1
		}
		if !okb {
			return 1
		}
		if ua < ub {
			return -1
		}
		if ua > ub {
			return 1
		}
	}
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
	k     string
	val   oruby.Value
	raw   oruby.Value
	isRaw bool // already JSON-encoded (e.g. String subclass custom to_json)
}

func splitSciFloat(f float64) (digits string, exp int) {
	raw := strconv.FormatFloat(f, 'e', -1, 64)
	eIdx := strings.IndexByte(raw, 'e')
	mant := raw[:eIdx]
	exp, _ = strconv.Atoi(raw[eIdx+1:])
	if dotIdx := strings.IndexByte(mant, '.'); dotIdx >= 0 {
		digits = mant[:dotIdx] + mant[dotIdx+1:]
	} else {
		digits = mant
	}
	return digits, exp
}

func appendRFC8785Float(buf []byte, f float64) []byte {
	if f == 0 {
		return append(buf, '0')
	}
	if f < 0 {
		buf = append(buf, '-')
		f = -f
	}
	digits, exp := splitSciFloat(f)
	k := len(digits)
	n := exp + 1
	if k <= n && n <= 21 {
		buf = append(buf, digits...)
		for i := k; i < n; i++ {
			buf = append(buf, '0')
		}
		return buf
	}
	if 0 < n && n <= 21 {
		buf = append(buf, digits[:n]...)
		buf = append(buf, '.')
		buf = append(buf, digits[n:]...)
		return buf
	}
	if -6 < n && n <= 0 {
		buf = append(buf, '0', '.')
		for i := 0; i < -n; i++ {
			buf = append(buf, '0')
		}
		buf = append(buf, digits...)
		return buf
	}
	buf = append(buf, digits[0])
	if k > 1 {
		buf = append(buf, '.')
		buf = append(buf, digits[1:]...)
	}
	buf = append(buf, 'e')
	if exp >= 0 {
		buf = append(buf, '+')
		buf = strconv.AppendInt(buf, int64(exp), 10)
	} else {
		buf = strconv.AppendInt(buf, int64(exp), 10)
	}
	return buf
}

func appendRubyFloat(buf []byte, f float64) []byte {
	if f == 0 {
		if math.Signbit(f) {
			return append(buf, "-0.0"...)
		}
		return append(buf, "0.0"...)
	}
	if f < 0 {
		buf = append(buf, '-')
		f = -f
	}
	digits, exp := splitSciFloat(f)
	k := len(digits)
	n := exp + 1
	if n > 0 {
		if n < k && n <= 16 {
			buf = append(buf, digits[:n]...)
			buf = append(buf, '.')
			buf = append(buf, digits[n:]...)
			return buf
		}
		if n >= k && n <= 15 {
			buf = append(buf, digits...)
			for i := k; i < n; i++ {
				buf = append(buf, '0')
			}
			buf = append(buf, '.', '0')
			return buf
		}
	} else if n >= -8 {
		buf = append(buf, '0', '.')
		for i := 0; i < -n; i++ {
			buf = append(buf, '0')
		}
		buf = append(buf, digits...)
		return buf
	}
	buf = append(buf, digits[0])
	if k > 1 {
		buf = append(buf, '.')
		buf = append(buf, digits[1:]...)
	}
	buf = append(buf, 'e')
	if exp >= 0 {
		buf = append(buf, '+')
		if exp < 10 {
			buf = append(buf, '0')
		}
		buf = strconv.AppendInt(buf, int64(exp), 10)
	} else {
		buf = append(buf, '-')
		ae := -exp
		if ae < 10 {
			buf = append(buf, '0')
		}
		buf = strconv.AppendInt(buf, int64(ae), 10)
	}
	return buf
}

func appendJSONFloat(mrb *oruby.MrbState, buf []byte, f float64, val oruby.Value, depth int, opts *jsonGenOpts) ([]byte, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		if opts.allowNaN {
			if math.IsNaN(f) {
				return append(buf, "NaN"...), nil
			}
			if math.IsInf(f, 1) {
				return append(buf, "Infinity"...), nil
			}
			return append(buf, "-Infinity"...), nil
		}
		if !opts.asJSON.IsNil() && !opts.inAsJSON {
			nextOpts := *opts
			nextOpts.inAsJSON = true
			converted, err := mrb.Funcall(opts.asJSON, mrb.Intern("call"), val, mrb.FalseValue())
			if err != nil {
				return nil, &genError{err: err, isRubyExc: true}
			}
			return appendJSONValue(mrb, buf, converted, depth, &nextOpts)
		}
		msg := "NaN not allowed in JSON"
		if math.IsInf(f, 1) {
			msg = "Infinity not allowed in JSON"
		} else if math.IsInf(f, -1) {
			msg = "-Infinity not allowed in JSON"
		}
		return nil, &genError{err: errors.New(msg), invalidObj: val, hasInvalid: true}
	}
	if opts.rfc8785 {
		return appendRFC8785Float(buf, f), nil
	}
	return appendRubyFloat(buf, f), nil
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

func isAllASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func encodeStringUTF8WithCause(mrb *oruby.MrbState, val oruby.Value) (oruby.Value, *genError) {
	pair, err := mrb.Funcall(mrb.ModuleGet("JSON"), mrb.Intern("__encode_utf8"), val)
	if err != nil {
		return mrb.NilValue(), &genError{err: err, isRubyExc: true}
	}
	elems := oruby.RArraySlice(pair)
	if len(elems) == 2 {
		if !elems[1].IsNil() {
			return mrb.NilValue(), &genError{
				err:        errors.New("source sequence is illegal/malformed utf-8"),
				invalidObj: val,
				hasInvalid: true,
				causeExc:   elems[1],
				hasCause:   true,
			}
		}
		if !elems[0].IsString() {
			return mrb.NilValue(), &genError{err: errors.New("encode did not return String"), isTypeErr: true}
		}
		return elems[0], nil
	}
	return mrb.NilValue(), &genError{
		err:        errors.New("source sequence is illegal/malformed utf-8"),
		invalidObj: val,
		hasInvalid: true,
	}
}

func convertRubyStringToUTF8(mrb *oruby.MrbState, val oruby.Value) (string, error) {
	encName := rubyStringEncodingName(mrb, val)
	if encName == "ASCII-8BIT" {
		ptr, n := oruby.RStringMem(val)
		if n <= 0 || ptr == nil {
			return "", nil
		}
		s := unsafe.String(ptr, n)
		if isAllASCII(s) {
			return s, nil
		}
		conv, ge := encodeStringUTF8WithCause(mrb, val)
		if ge != nil {
			return "", ge
		}
		val = conv
		ptr, n = oruby.RStringMem(val)
		if n <= 0 || ptr == nil {
			return "", nil
		}
		return unsafe.String(ptr, n), nil
	}
	if encName == "US-ASCII" {
		ptr, n := oruby.RStringMem(val)
		if n <= 0 || ptr == nil {
			return "", nil
		}
		s := unsafe.String(ptr, n)
		if isAllASCII(s) {
			return s, nil
		}
		conv, ge := encodeStringUTF8WithCause(mrb, val)
		if ge != nil {
			return "", ge
		}
		val = conv
	} else if encName != "UTF-8" {
		conv, ge := encodeStringUTF8WithCause(mrb, val)
		if ge != nil {
			return "", ge
		}
		val = conv
	}
	ptr, n := oruby.RStringMem(val)
	if n <= 0 || ptr == nil {
		return "", nil
	}
	s := unsafe.String(ptr, n)
	if !utf8.ValidString(s) {
		return "", &genError{
			err:        errors.New("source sequence is illegal/malformed utf-8"),
			invalidObj: val,
			hasInvalid: true,
		}
	}
	return s, nil
}

func hasCustomToJSON(mrb *oruby.MrbState, val oruby.Value, baseClassName string) bool {
	cls := mrb.ClassOf(val)
	baseCls := mrb.ClassGet(baseClassName)
	if mrb.ObjEqual(cls.Value(), baseCls.Value()) {
		return false
	}
	if !mrb.RespondTo(val, mrb.Intern("to_json")) {
		return false
	}
	meth, err := mrb.Funcall(val, mrb.Intern("method"), mrb.SymbolValue(mrb.Intern("to_json")))
	if err != nil {
		return false
	}
	owner, err := mrb.Funcall(meth, mrb.Intern("owner"))
	if err != nil {
		return false
	}
	if mrb.ObjEqual(owner, baseCls.Value()) || mrb.ObjEqual(owner, mrb.ObjectClass().Value()) {
		return false
	}
	return true
}

func setStateDepth(mrb *oruby.MrbState, opts *jsonGenOpts, depth int) {
	if !opts.stateVal.IsNil() {
		mrb.IVSet(opts.stateVal, mrb.Intern("@depth"), mrb.FixnumValue(depth))
	}
}

func callCustomToJSON(mrb *oruby.MrbState, buf []byte, val oruby.Value, depth int, opts *jsonGenOpts) ([]byte, error) {
	setStateDepth(mrb, opts, depth)
	var res oruby.Value
	var err error
	if !opts.stateVal.IsNil() {
		res, err = mrb.Funcall(val, mrb.Intern("to_json"), opts.stateVal)
	} else {
		res, err = mrb.Funcall(val, mrb.Intern("to_json"))
	}
	setStateDepth(mrb, opts, depth)
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
		if opts.rfc8785 {
			return appendRFC8785Float(buf, float64(val.Int64())), nil
		}
		return strconv.AppendInt(buf, val.Int64(), 10), nil
	case oruby.MrbTTBigInt:
		z, err := mrb.BigInt(val)
		if err != nil {
			return nil, &genError{err: err, invalidObj: val, hasInvalid: true}
		}
		if opts.rfc8785 {
			if z.CmpAbs(rfc8785MaxInt) > 0 {
				return nil, &genError{
					err:        errors.New("Integer out of range for RFC 8785"),
					invalidObj: val,
					hasInvalid: true,
				}
			}
			f, _ := new(big.Float).SetPrec(1024).SetInt(z).Float64()
			return appendRFC8785Float(buf, f), nil
		}
		return append(buf, z.String()...), nil
	case oruby.MrbTTFloat:
		return appendJSONFloat(mrb, buf, val.Float64(), val, depth, opts)
	case oruby.MrbTTString:
		skipCustom := opts.skipRootToJSON && depth == opts.initDepth
		if !skipCustom && !opts.strict && hasCustomToJSON(mrb, val, "String") {
			return callCustomToJSON(mrb, buf, val, depth, opts)
		}
		s, err := convertRubyStringToUTF8(mrb, val)
		if err != nil {
			if !opts.asJSON.IsNil() && !opts.inAsJSON && opts.strict {
				var ge *genError
				if !errors.As(err, &ge) || !ge.isTypeErr {
					nextOpts := *opts
					nextOpts.inAsJSON = true
					converted, cerr := mrb.Funcall(opts.asJSON, mrb.Intern("call"), val, mrb.FalseValue())
					if cerr != nil {
						return nil, &genError{err: cerr, isRubyExc: true}
					}
					return appendJSONValue(mrb, buf, converted, depth, &nextOpts)
				}
			}
			return nil, err
		}
		return appendJSONString(buf, s, opts, val)
	case oruby.MrbTTSymbol:
		s := mrb.SymName(val.Symbol())
		if !utf8.ValidString(s) {
			if !opts.asJSON.IsNil() && !opts.inAsJSON && opts.strict {
				symStr, serr := mrb.Funcall(val, oruby.Sym_to_s)
				if serr != nil {
					symStr = mrb.StrNew(s)
				}
				nextOpts := *opts
				nextOpts.inAsJSON = true
				converted, cerr := mrb.Funcall(opts.asJSON, mrb.Intern("call"), symStr, mrb.FalseValue())
				if cerr != nil {
					return nil, &genError{err: cerr, isRubyExc: true}
				}
				return appendJSONValue(mrb, buf, converted, depth, &nextOpts)
			}
			return nil, &genError{
				err:        errors.New("source sequence is illegal/malformed utf-8"),
				invalidObj: val,
				hasInvalid: true,
			}
		}
		return appendJSONString(buf, s, opts, val)
	case oruby.MrbTTArray:
		skipCustom := opts.skipRootToJSON && depth == opts.initDepth
		if !skipCustom && !opts.strict && hasCustomToJSON(mrb, val, "Array") {
			return callCustomToJSON(mrb, buf, val, depth, opts)
		}
		nextDepth := depth + 1
		if nextDepth > opts.maxNesting {
			return nil, &genError{
				err:        fmt.Errorf("nesting of %d is too deep.", nextDepth),
				invalidObj: val,
				hasInvalid: true,
				isNesting:  true,
			}
		}
		elems := oruby.RArraySlice(val)
		if len(elems) == 0 {
			return append(buf, '[', ']'), nil
		}
		childOpts := opts
		if opts.skipRootToJSON {
			c := *opts
			c.skipRootToJSON = false
			childOpts = &c
		}
		setStateDepth(mrb, opts, nextDepth)
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
			buf, err = appendJSONValue(mrb, buf, elem, nextDepth, childOpts)
			if err != nil {
				setStateDepth(mrb, opts, depth)
				return nil, err
			}
		}
		setStateDepth(mrb, opts, depth)
		if opts.arrayNL != "" {
			buf = append(buf, opts.arrayNL...)
			buf = appendIndent(buf, opts.indent, depth)
		}
		buf = append(buf, ']')
		return buf, nil
	case oruby.MrbTTHash:
		skipCustom := opts.skipRootToJSON && depth == opts.initDepth
		if !skipCustom && !opts.strict && hasCustomToJSON(mrb, val, "Hash") {
			return callCustomToJSON(mrb, buf, val, depth, opts)
		}
		nextDepth := depth + 1
		if nextDepth > opts.maxNesting {
			return nil, &genError{
				err:        fmt.Errorf("nesting of %d is too deep.", nextDepth),
				invalidObj: val,
				hasInvalid: true,
				isNesting:  true,
			}
		}
		hashVal := val
		if !opts.sortKeysProc.IsNil() && !opts.rfc8785 {
			sorted, serr := mrb.Funcall(opts.sortKeysProc, mrb.Intern("call"), val)
			if serr != nil {
				return nil, &genError{err: serr, isRubyExc: true}
			}
			if sorted.IsHash() {
				hashVal = sorted
			}
		}
		entries := oruby.RHashEntries(hashVal)
		var stackKVs [16]kvPair
		var kvs []kvPair
		if len(entries) <= len(stackKVs) {
			kvs = stackKVs[:0]
		} else {
			kvs = make([]kvPair, 0, len(entries))
		}
		var seenKeys map[string]struct{}
		if !opts.allowDuplicateKey {
			seenKeys = make(map[string]struct{}, len(entries))
		}
		strClsVal := mrb.StringClass().Value()
		convertKeyViaAsJSON := func(origKey oruby.Value) (string, oruby.Value, error) {
			ck, err := mrb.Funcall(opts.asJSON, mrb.Intern("call"), origKey, mrb.TrueValue())
			if err != nil {
				return "", mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
			if ck.IsString() {
				ks, kerr := convertRubyStringToUTF8(mrb, ck)
				if kerr != nil {
					return "", ck, kerr
				}
				return ks, ck, nil
			}
			if ck.IsSymbol() {
				ks := mrb.SymName(ck.Symbol())
				if !utf8.ValidString(ks) {
					return "", ck, &genError{
						err:        errors.New("source sequence is illegal/malformed utf-8"),
						invalidObj: ck,
						hasInvalid: true,
					}
				}
				return ks, ck, nil
			}
			return "", ck, &genError{
				err:        fmt.Errorf("%s not allowed as object key in JSON", mrb.ClassOf(ck).Name()),
				invalidObj: ck,
				hasInvalid: true,
			}
		}
		for i := range entries {
			key := entries[i][0]
			if key.Type() == oruby.MrbTTUndef {
				continue
			}
			var keyStr string
			keyRaw := key
			isRawKey := false
			switch key.Type() {
			case oruby.MrbTTString:
				isExactStr := mrb.ObjEqual(mrb.ClassOf(key).Value(), strClsVal)
				if isExactStr || opts.strict {
					var err error
					keyStr, err = convertRubyStringToUTF8(mrb, key)
					if err != nil {
						if !opts.asJSON.IsNil() && !opts.inAsJSON && opts.strict {
							var ge *genError
							if !errors.As(err, &ge) || !ge.isTypeErr {
								keyStr, keyRaw, err = convertKeyViaAsJSON(key)
								if err != nil {
									return nil, err
								}
								break
							}
						}
						return nil, err
					}
				} else {
					// String subclass in non-strict mode: call to_s, check String, check custom to_json
					sv, err := mrb.Funcall(key, oruby.Sym_to_s)
					if err != nil {
						return nil, &genError{err: err, isRubyExc: true}
					}
					if !sv.IsString() {
						return nil, &genError{
							err:       fmt.Errorf("can't convert %s to String (%s#to_s gives %s)", mrb.ClassOf(key).Name(), mrb.ClassOf(key).Name(), mrb.ClassOf(sv).Name()),
							isTypeErr: true,
						}
					}
					if !mrb.ObjEqual(mrb.ClassOf(sv).Value(), strClsVal) && hasCustomToJSON(mrb, sv, "String") {
						rawBuf, rerr := callCustomToJSON(mrb, nil, sv, nextDepth, opts)
						if rerr != nil {
							return nil, rerr
						}
						keyStr = string(rawBuf)
						isRawKey = true
					} else {
						keyStr, err = convertRubyStringToUTF8(mrb, sv)
						if err != nil {
							return nil, err
						}
					}
				}
			case oruby.MrbTTSymbol:
				keyStr = mrb.SymName(key.Symbol())
				if !utf8.ValidString(keyStr) {
					if !opts.asJSON.IsNil() && !opts.inAsJSON && opts.strict {
						symStr, serr := mrb.Funcall(key, oruby.Sym_to_s)
						if serr != nil {
							symStr = mrb.StrNew(keyStr)
						}
						var err error
						keyStr, keyRaw, err = convertKeyViaAsJSON(symStr)
						if err != nil {
							return nil, err
						}
					} else {
						return nil, &genError{
							err:        errors.New("source sequence is illegal/malformed utf-8"),
							invalidObj: key,
							hasInvalid: true,
						}
					}
				}
			default:
				if opts.strict {
					if !opts.asJSON.IsNil() {
						var err error
						keyStr, keyRaw, err = convertKeyViaAsJSON(key)
						if err != nil {
							return nil, err
						}
					} else {
						return nil, &genError{
							err:        fmt.Errorf("%s not allowed as object key in JSON", mrb.ClassOf(key).Name()),
							invalidObj: key,
							hasInvalid: true,
						}
					}
				} else {
					sv, err := mrb.Funcall(key, oruby.Sym_to_s)
					if err != nil {
						return nil, &genError{err: err, isRubyExc: true}
					}
					if sv.Type() != oruby.MrbTTString {
						return nil, &genError{
							err:       fmt.Errorf("can't convert %s to String (%s#to_s gives %s)", mrb.ClassOf(key).Name(), mrb.ClassOf(key).Name(), mrb.ClassOf(sv).Name()),
							isTypeErr: true,
						}
					}
					keyStr, err = convertRubyStringToUTF8(mrb, sv)
					if err != nil {
						return nil, err
					}
				}
			}
			if seenKeys != nil {
				if _, dup := seenKeys[keyStr]; dup {
					hashInspect := mrb.String(mrb.Inspect(val))
					return nil, &genError{
						err:        fmt.Errorf("detected duplicate key %q in %s", keyStr, hashInspect),
						invalidObj: keyRaw,
						hasInvalid: true,
					}
				}
				seenKeys[keyStr] = struct{}{}
			}
			kvs = append(kvs, kvPair{k: keyStr, val: entries[i][1], raw: keyRaw, isRaw: isRawKey})
		}
		if len(kvs) == 0 {
			return append(buf, '{', '}'), nil
		}
		if opts.rfc8785 {
			sort.SliceStable(kvs, func(i, j int) bool {
				return compareUTF16(kvs[i].k, kvs[j].k) < 0
			})
		} else if opts.sortKeys {
			sort.SliceStable(kvs, func(i, j int) bool {
				return kvs[i].k < kvs[j].k
			})
		}
		childOpts := opts
		if opts.skipRootToJSON {
			c := *opts
			c.skipRootToJSON = false
			childOpts = &c
		}
		setStateDepth(mrb, opts, nextDepth)
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
			if kv.isRaw {
				buf = append(buf, kv.k...)
			} else {
				buf, err = appendJSONString(buf, kv.k, opts, kv.raw)
				if err != nil {
					setStateDepth(mrb, opts, depth)
					return nil, err
				}
			}
			if opts.spaceBefore != "" {
				buf = append(buf, opts.spaceBefore...)
			}
			buf = append(buf, ':')
			if opts.space != "" {
				buf = append(buf, opts.space...)
			}
			buf, err = appendJSONValue(mrb, buf, kv.val, nextDepth, childOpts)
			if err != nil {
				setStateDepth(mrb, opts, depth)
				return nil, err
			}
		}
		setStateDepth(mrb, opts, depth)
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
		if opts.strict && !opts.asJSON.IsNil() {
			if opts.inAsJSON {
				return nil, &genError{
					err:        fmt.Errorf("%s not allowed in JSON", mrb.ClassOf(val).Name()),
					invalidObj: val,
					hasInvalid: true,
				}
			}
			if depth >= opts.maxNesting {
				return nil, &genError{
					err:        fmt.Errorf("nesting of %d is too deep.", depth+1),
					invalidObj: val,
					hasInvalid: true,
					isNesting:  true,
				}
			}
			nextOpts := *opts
			nextOpts.inAsJSON = true
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
			return appendJSONValue(mrb, buf, converted, depth, &nextOpts)
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
			return callCustomToJSON(mrb, buf, val, depth, opts)
		}
		sv, err := mrb.Funcall(val, oruby.Sym_to_s)
		if err != nil {
			return nil, &genError{err: err, isRubyExc: true}
		}
		if !sv.IsString() {
			return nil, &genError{
				err:       fmt.Errorf("can't convert %s to String (%s#to_s gives %s)", mrb.ClassOf(val).Name(), mrb.ClassOf(val).Name(), mrb.ClassOf(sv).Name()),
				isTypeErr: true,
			}
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
		if ge.isTypeErr {
			return mrb.Raise(mrb.ETypeError(), ge.err.Error())
		}
		if ge.isNesting || errors.Is(ge.err, errNesting) {
			cls := jsonErrorClass(mrb, "NestingError")
			return mrb.Raise(cls, ge.err.Error())
		}
		if ge.hasCause {
			inv := ge.invalidObj
			if !ge.hasInvalid {
				inv = mrb.NilValue()
			}
			_, rerr := mrb.Funcall(mrb.ModuleGet("JSON"), mrb.Intern("__raise_gen_error"),
				mrb.StrNew(ge.err.Error()), inv, mrb.BoolValue(ge.hasInvalid), ge.causeExc)
			if rerr != nil {
				return mrb.RaiseError(rerr)
			}
			return mrb.NilValue()
		}
		cls := jsonErrorClass(mrb, "GeneratorError")
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
		return mrb.Raisef(jsonErrorClass(mrb, "NestingError"), "nesting of %v is too deep.", err)
	}
	if _, ok := err.(oruby.MrbValue); ok {
		return mrb.RaiseError(err)
	}
	return mrb.Raisef(jsonErrorClass(mrb, "GeneratorError"), "JSON::generate - %v", err)
}

func prepareWorkingState(mrb *oruby.MrbState, opts *jsonGenOpts) func() {
	if opts.stateVal.IsNil() {
		return func() {}
	}
	origState := opts.stateVal
	origDepth := opts.initDepth
	frz, err := mrb.Funcall(origState, mrb.Intern("frozen?"))
	if err == nil && oruby.MrbBoolean(frz) {
		if duped, derr := mrb.Funcall(origState, mrb.Intern("dup")); derr == nil && !duped.IsNil() {
			opts.stateVal = duped
		}
		return func() {}
	}
	return func() {
		mrb.IVSet(origState, mrb.Intern("@depth"), mrb.FixnumValue(origDepth))
	}
}

func mrbJSONStringify(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	v := args.Item(0)
	opts := extractJSONGenOpts(mrb, args)
	opts.sortKeys = true
	opts.escapeHTML = true
	opts.allowDuplicateKey = true
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
	restore := prepareWorkingState(mrb, &opts)
	defer restore()
	var localBuf [512]byte
	buf, err := appendJSONValue(mrb, localBuf[:0], v, opts.initDepth, &opts)
	if err != nil {
		return raiseJSONGenErr(mrb, err)
	}
	return mrb.BytesValue(buf)
}

func mrbJSONGenerateRaw(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	v := args.Item(0)
	opts := extractJSONGenOpts(mrb, args)
	opts.skipRootToJSON = true
	restore := prepareWorkingState(mrb, &opts)
	defer restore()
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
	maxNesting             int
	maxNestingDisabled     bool
	symbolizeNames         bool
	freeze                 bool
	allowNaN               bool
	allowTrailingComma     bool
	allowDuplicateKey      bool
	allowComments          bool
	allowControlCharacters bool
	allowInvalidEscape     bool
	createAdditions        bool
	objectClass            oruby.Value
	arrayClass             oruby.Value
	decimalClass           oruby.Value
	onLoad                 oruby.Value
	useSlowPath            bool
}

func extractJSONParseOpts(mrb *oruby.MrbState, args oruby.RArgs) jsonParseOpts {
	res := jsonParseOpts{
		maxNesting:        defaultMaxNesting,
		allowDuplicateKey: false,
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
				res.maxNestingDisabled = true
			} else if v.IsInt() {
				n := int(v.Int64())
				if n <= 0 {
					res.maxNesting = hardMaxNesting
					res.maxNestingDisabled = true
				} else if n > hardMaxNesting {
					res.maxNesting = hardMaxNesting
					res.maxNestingDisabled = true
				} else {
					res.maxNesting = n
					res.maxNestingDisabled = false
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
		}
		if v, ok := getOpt("allow_comments"); ok && oruby.MrbBoolean(v) {
			res.allowComments = true
			res.useSlowPath = true
		}
		if v, ok := getOpt("allow_control_characters"); ok && oruby.MrbBoolean(v) {
			res.allowControlCharacters = true
			res.useSlowPath = true
		}
		if v, ok := getOpt("allow_invalid_escape"); ok && oruby.MrbBoolean(v) {
			res.allowInvalidEscape = true
			res.useSlowPath = true
		}
		if v, ok := getOpt("allow_invalid_escapes"); ok && oruby.MrbBoolean(v) {
			res.allowInvalidEscape = true
			res.useSlowPath = true
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
		if v, ok := getOpt("on_load"); ok && !v.IsNil() && v.Type() != oruby.MrbTTFalse {
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

// Extract a UTF-8 safe snippet of up to 32 bytes from s[pos:], stopping before whitespace.
func errorSnippet(s string, pos int) string {
	if pos >= len(s) {
		return ""
	}
	rem := s[pos:]
	end := 0
	for end < len(rem) {
		b := rem[end]
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == 0 {
			break
		}
		var sz int
		if b < 0x80 {
			sz = 1
		} else {
			_, sz = utf8.DecodeRuneInString(rem[end:])
		}
		if end+sz > 32 {
			break
		}
		end += sz
	}
	return rem[:end]
}

// Full-featured recursive-descent JSON parser for all Ruby JSON options
type rdParser struct {
	mrb      *oruby.MrbState
	s        string
	pos      int
	errPos   int
	hasErrAt bool
	opts     jsonParseOpts
	createID string
	pathVals []oruby.Value
}

func (p *rdParser) setErrPos(pos int) {
	p.errPos = pos
	p.hasErrAt = true
}

func (p *rdParser) location() (line, col, byteOff int) {
	pos := p.pos
	if p.hasErrAt {
		pos = p.errPos
	}
	if pos > len(p.s) {
		pos = len(p.s)
	}
	if pos < 0 {
		pos = 0
	}
	line = 1
	lastNL := -1
	for i := 0; i < pos; i++ {
		if p.s[i] == '\n' {
			line++
			lastNL = i
		}
	}
	// Count column in Unicode codepoints (1-based)
	col = 1
	for i := lastNL + 1; i < pos; {
		if p.s[i] < 0x80 {
			i++
		} else {
			_, sz := utf8.DecodeRuneInString(p.s[i:pos])
			i += sz
		}
		col++
	}
	return line, col, pos
}

func (p *rdParser) skipWS() error {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		case '/':
			if !p.opts.allowComments {
				return nil
			}
			if p.pos+1 < len(p.s) && p.s[p.pos+1] == '/' {
				p.pos += 2
				for p.pos < len(p.s) && p.s[p.pos] != '\n' {
					p.pos++
				}
			} else if p.pos+1 < len(p.s) && p.s[p.pos+1] == '*' {
				start := p.pos
				p.pos += 2
				closed := false
				for p.pos+1 < len(p.s) {
					if p.s[p.pos] == '*' && p.s[p.pos+1] == '/' {
						p.pos += 2
						closed = true
						break
					}
					p.pos++
				}
				if !closed {
					p.setErrPos(start)
					return errors.New("unterminated comment")
				}
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return nil
}

func (p *rdParser) maybeFreeze(v oruby.Value) oruby.Value {
	if p.opts.freeze {
		if v.IsString() {
			if fstr, err := p.mrb.Funcall(v, p.mrb.Intern("-@")); err == nil && !fstr.IsNil() {
				return fstr
			}
		}
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
	if err := p.skipWS(); err != nil {
		return p.mrb.NilValue(), err
	}
	if p.pos >= len(p.s) {
		return p.mrb.NilValue(), errors.New("unexpected end of input")
	}
	var val oruby.Value
	var err error
	c := p.s[p.pos]
	switch c {
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
			err = fmt.Errorf("unexpected token '%s'", errorSnippet(p.s, p.pos))
		}
	case 'f':
		if strings.HasPrefix(p.s[p.pos:], "false") {
			p.pos += 5
			val = p.mrb.FalseValue()
		} else {
			err = fmt.Errorf("unexpected token '%s'", errorSnippet(p.s, p.pos))
		}
	case 'n':
		if strings.HasPrefix(p.s[p.pos:], "null") {
			p.pos += 4
			val = p.mrb.NilValue()
		} else {
			err = fmt.Errorf("unexpected token '%s'", errorSnippet(p.s, p.pos))
		}
	case 'N':
		if p.opts.allowNaN && strings.HasPrefix(p.s[p.pos:], "NaN") {
			p.pos += 3
			val = p.mrb.FloatValue(math.NaN())
		} else {
			err = fmt.Errorf("unexpected token '%s'", errorSnippet(p.s, p.pos))
		}
	case 'I':
		if p.opts.allowNaN && strings.HasPrefix(p.s[p.pos:], "Infinity") {
			p.pos += 8
			val = p.mrb.FloatValue(math.Inf(1))
		} else {
			err = fmt.Errorf("unexpected token '%s'", errorSnippet(p.s, p.pos))
		}
	case '-':
		if p.opts.allowNaN && strings.HasPrefix(p.s[p.pos:], "-Infinity") {
			p.pos += 9
			val = p.mrb.FloatValue(math.Inf(-1))
		} else {
			val, err = p.parseNumber()
		}
	default:
		if c >= '0' && c <= '9' {
			val, err = p.parseNumber()
		} else {
			err = fmt.Errorf("unexpected character: '%s'", errorSnippet(p.s, p.pos))
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
		return "", fmt.Errorf("unexpected character: '%s'", errorSnippet(p.s, p.pos))
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
		if c < 0x20 && !p.opts.allowControlCharacters {
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
				if p.opts.allowInvalidEscape && (esc >= 0x20 || p.opts.allowControlCharacters) {
					sb.WriteByte(esc)
				} else {
					p.pos--
					return "", fmt.Errorf("invalid escape character \\%c", esc)
				}
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
		p.setErrPos(start)
		return p.mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(p.s, start))
	}
	if p.s[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.setErrPos(start)
			return p.mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(p.s, start))
		}
	} else if p.s[p.pos] >= '1' && p.s[p.pos] <= '9' {
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	} else {
		p.setErrPos(start)
		return p.mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(p.s, start))
	}
	isFloat := false
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		isFloat = true
		p.pos++
		if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
			p.setErrPos(start)
			return p.mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(p.s, start))
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
			p.setErrPos(start)
			return p.mrb.NilValue(), fmt.Errorf("invalid number: '%s'", errorSnippet(p.s, start))
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	raw := p.s[start:p.pos]
	if isFloat {
		if !p.opts.decimalClass.IsNil() {
			var v oruby.Value
			var err error
			if p.mrb.RespondTo(p.opts.decimalClass, p.mrb.Intern("new")) {
				v, err = p.mrb.Funcall(p.opts.decimalClass, p.mrb.Intern("new"), p.mrb.StrNew(raw))
			} else {
				v, err = p.mrb.Funcall(p.mrb.TopSelf(), p.mrb.Intern("BigDecimal"), p.mrb.StrNew(raw))
			}
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
	if err := p.skipWS(); err != nil {
		return p.mrb.NilValue(), err
	}
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return p.maybeFreeze(ary), nil
	}
	idx := 0
	for {
		p.pathVals = append(p.pathVals, p.mrb.FixnumValue(idx))
		elem, err := p.parseValue(depth + 1)
		if err != nil {
			return p.mrb.NilValue(), err
		}
		idx++
		if custom {
			if _, err := p.mrb.Funcall(ary, p.mrb.Intern("<<"), elem); err != nil {
				return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
		} else {
			p.mrb.AryPush(ary, elem)
		}
		if err := p.skipWS(); err != nil {
			return p.mrb.NilValue(), err
		}
		if p.pos >= len(p.s) {
			return p.mrb.NilValue(), errors.New("expected ',' or ']' after array value, got: EOF")
		}
		if p.s[p.pos] == ']' {
			p.pathVals = p.pathVals[:len(p.pathVals)-1]
			p.pos++
			return p.maybeFreeze(ary), nil
		}
		if p.s[p.pos] != ',' {
			return p.mrb.NilValue(), fmt.Errorf("expected ',' or ']' after array value, got: '%s'", errorSnippet(p.s, p.pos))
		}
		p.pathVals = p.pathVals[:len(p.pathVals)-1]
		p.pos++
		if err := p.skipWS(); err != nil {
			return p.mrb.NilValue(), err
		}
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
	objStart := p.pos
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
	if err := p.skipWS(); err != nil {
		return p.mrb.NilValue(), err
	}
	if p.pos >= len(p.s) {
		return p.mrb.NilValue(), errors.New("expected object key, got EOF")
	}
	if p.s[p.pos] == '}' {
		p.pos++
		return p.maybeFreeze(obj), nil
	}
	var createClassName string
	for {
		if err := p.skipWS(); err != nil {
			return p.mrb.NilValue(), err
		}
		if p.pos >= len(p.s) {
			return p.mrb.NilValue(), errors.New("expected object key, got EOF")
		}
		if p.s[p.pos] != '"' {
			return p.mrb.NilValue(), fmt.Errorf("expected object key, got: '%s'", errorSnippet(p.s, p.pos))
		}
		kstr, err := p.parseString()
		if err != nil {
			return p.mrb.NilValue(), err
		}
		var kval oruby.Value
		if p.opts.symbolizeNames {
			kval = p.mrb.SymbolValue(p.mrb.Intern(kstr))
		} else {
			kval = p.maybeFreeze(p.mrb.StrNew(kstr))
		}
		if !p.opts.onLoad.IsNil() {
			kval, err = p.maybeOnLoad(kval)
			if err != nil {
				return p.mrb.NilValue(), err
			}
		}
		p.pathVals = append(p.pathVals, kval)
		if seen != nil {
			if _, dup := seen[kstr]; dup {
				p.setErrPos(objStart)
				return p.mrb.NilValue(), fmt.Errorf("duplicate key %q", kstr)
			}
			seen[kstr] = struct{}{}
		}
		if err := p.skipWS(); err != nil {
			return p.mrb.NilValue(), err
		}
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
		if custom {
			if _, err := p.mrb.Funcall(obj, p.mrb.Intern("[]="), kval, val); err != nil {
				return p.mrb.NilValue(), &genError{err: err, isRubyExc: true}
			}
		} else {
			p.mrb.HashSet(obj, kval, val)
		}
		if err := p.skipWS(); err != nil {
			return p.mrb.NilValue(), err
		}
		if p.pos >= len(p.s) {
			return p.mrb.NilValue(), errors.New("expected ',' or '}' after object value, got: EOF")
		}
		if p.s[p.pos] == '}' {
			p.pathVals = p.pathVals[:len(p.pathVals)-1]
			p.pos++
			break
		}
		if p.s[p.pos] != ',' {
			return p.mrb.NilValue(), fmt.Errorf("expected ',' or '}' after object value, got: '%s'", errorSnippet(p.s, p.pos))
		}
		p.pathVals = p.pathVals[:len(p.pathVals)-1]
		p.pos++
		if err := p.skipWS(); err != nil {
			return p.mrb.NilValue(), err
		}
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

	// Handle non-UTF-8 Ruby string encodings (US-ASCII/UTF-16/UTF-32/etc.)
	encName := rubyStringEncodingName(mrb, arg)
	if encName == "US-ASCII" {
		ptr, n := oruby.RStringMem(arg)
		if n > 0 && ptr != nil {
			s := unsafe.String(ptr, n)
			if !isAllASCII(s) {
				utf8Enc, eerr := mrb.Eval("Encoding::UTF_8")
				if eerr == nil && mrb.RespondTo(arg, mrb.Intern("encode")) {
					if _, cerr := mrb.Funcall(arg, mrb.Intern("encode"), utf8Enc); cerr != nil {
						return mrb.RaiseError(cerr)
					}
				}
				encMod := mrb.ModuleGet("Encoding")
				if !encMod.IsNil() && mrb.ConstDefined(encMod, mrb.Intern("InvalidByteSequenceError")) {
					return mrb.Raise(mrb.ClassGetUnder(encMod, "InvalidByteSequenceError"), "invalid byte sequence in US-ASCII")
				}
				return mrb.Raise(mrb.ERuntimeError(), "invalid byte sequence in US-ASCII")
			}
		}
	} else if encName != "UTF-8" && encName != "ASCII-8BIT" {
		if mrb.RespondTo(arg, mrb.Intern("encode")) {
			utf8Enc, eerr := mrb.Eval("Encoding::UTF_8")
			if eerr == nil {
				conv, cerr := mrb.Funcall(arg, mrb.Intern("encode"), utf8Enc)
				if cerr != nil {
					return mrb.RaiseError(cerr)
				}
				if !conv.IsString() {
					return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("no implicit conversion of %s into String", mrb.ClassOf(conv).Name()))
				}
				arg = conv
			}
		}
	}

	ptr, n := oruby.RStringMem(arg)
	var inBytes []byte
	if n > 0 && ptr != nil {
		inBytes = unsafe.Slice(ptr, n)
	}

	opts := extractJSONParseOpts(mrb, args)
	isValidUTF8 := utf8.Valid(inBytes)

	if !opts.useSlowPath && isValidUTF8 {
		reader := bytes.NewReader(inBytes)
		var dec *jsontext.Decoder
		if opts.allowDuplicateKey {
			dec = jsontext.NewDecoder(reader, jsontext.AllowDuplicateNames(true))
		} else {
			dec = jsontext.NewDecoder(reader)
		}
		ops := make([]oruby.BatchOp, 0, 64)
		strBuf := make([]byte, 0, 256)

		err := decodeJSONToBatch(dec, &ops, &strBuf, 0, opts.maxNesting)
		if err == nil {
			if _, terr := dec.ReadToken(); terr == io.EOF {
				res := mrb.ExecBatchOps(ops, strBuf)
				if opts.symbolizeNames {
					return symbolizeKeys(mrb, res)
				}
				return res
			}
		} else if errors.Is(err, errNesting) {
			if opts.maxNestingDisabled {
				return mrb.Raisef(jsonErrorClass(mrb, "ParserError"), "nesting of %d is too deep", opts.maxNesting+1)
			}
			return mrb.Raisef(jsonErrorClass(mrb, "NestingError"), "nesting of %d is too deep", opts.maxNesting+1)
		}
		// Fall through to rdParser for BigInt, float overflow, and detailed error location/path
	}

	// Slow/full-options path (copy string so mutating source during callbacks is safe)
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
	raiseParseErr := func(baseMsg string) oruby.MrbValue {
		line, col, byteOff := p.location()
		fullMsg := fmt.Sprintf("%s at line %d column %d", baseMsg, line, col)
		cls := jsonErrorClass(mrb, "ParserError")
		exc, ferr := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(fullMsg))
		if ferr == nil && !exc.IsNil() {
			mrb.IVSet(exc, mrb.Intern("@line"), mrb.FixnumValue(line))
			mrb.IVSet(exc, mrb.Intern("@column"), mrb.FixnumValue(col))
			mrb.IVSet(exc, mrb.Intern("@byte_offset"), mrb.FixnumValue(byteOff))
			pathAry := mrb.AryNewCapa(len(p.pathVals))
			for _, pv := range p.pathVals {
				pathAry.Push(pv)
			}
			mrb.IVSet(exc, mrb.Intern("@json_path"), pathAry.Value())
			mrb.ExcRaise(exc)
			return mrb.NilValue()
		}
		return mrb.Raisef(cls, "%s", fullMsg)
	}
	val, perr := p.parseValue(0)
	if perr != nil {
		var ge *genError
		if errors.As(perr, &ge) && ge.isRubyExc {
			return mrb.RaiseError(ge.err)
		}
		if errors.Is(perr, errNesting) {
			if opts.maxNestingDisabled {
				return raiseParseErr(fmt.Sprintf("nesting of %d is too deep", opts.maxNesting+1))
			}
			return mrb.Raisef(jsonErrorClass(mrb, "NestingError"), "nesting of %d is too deep", opts.maxNesting+1)
		}
		return raiseParseErr(perr.Error())
	}
	if err := p.skipWS(); err != nil {
		return raiseParseErr(err.Error())
	}
	if p.pos < len(p.s) {
		return raiseParseErr(fmt.Sprintf("unexpected token at end of stream '%s'", errorSnippet(p.s, p.pos)))
	}
	return val
}
