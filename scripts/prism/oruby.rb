# frozen_string_literal: true

# The oruby backend of Ruby::Prism, in place of Prism's ffi.rb: the same API on
# the serialization functions go-prism-serialize.c compiles next to the Prism
# parser in libmruby (Kernel.__oruby_prism_*). scripts/gen_prism.rb copies this
# file to ruby/prism/oruby.rb; the option packing and the error handling follow
# ffi.rb.
module Ruby::Prism # :nodoc:
  VERSION = Kernel.__oruby_prism_version.freeze

  # pm_errors_format_type_t
  ERRORS_FORMAT = { PM_ERRORS_FORMAT_PLAIN: 1, PM_ERRORS_FORMAT_STYLE: 2, PM_ERRORS_FORMAT_COLOR: 3 }.freeze
  private_constant :ERRORS_FORMAT

  # Kernel.__oruby_prism_serialize kinds
  SERIALIZE_PARSE = 0
  SERIALIZE_COMMENTS = 1
  SERIALIZE_LEX = 2
  SERIALIZE_PARSE_LEX = 3
  private_constant :SERIALIZE_PARSE, :SERIALIZE_COMMENTS, :SERIALIZE_LEX, :SERIALIZE_PARSE_LEX

  class << self
    # Mirror the Ruby::Prism.dump API by using the serialization API.
    def dump(source, **options)
      dump_common(string_source(source), options)
    end

    # Mirror the Ruby::Prism.dump_file API by using the serialization API.
    def dump_file(filepath, **options)
      dump_common(file_source(filepath, options), options)
    end

    # Mirror the Ruby::Prism.lex API by using the serialization API.
    def lex(code, **options)
      lex_common(string_source(code), code, options)
    end

    # Mirror the Ruby::Prism.lex_file API by using the serialization API.
    def lex_file(filepath, **options)
      source = file_source(filepath, options)
      lex_common(source, source, options)
    end

    # Mirror the Ruby::Prism.parse API by using the serialization API.
    def parse(code, **options)
      parse_common(string_source(code), code, options)
    end

    # Mirror the Ruby::Prism.parse_file API by using the serialization API.
    def parse_file(filepath, **options)
      source = file_source(filepath, options)
      parse_common(source, source, options)
    end

    # Mirror the Ruby::Prism.parse_stream API. The stream is read up to its end
    # or a __END__ line, which is where Prism's stream parser stops reading.
    def parse_stream(stream, **options)
      source = +""
      while (line = String.try_convert(stream.gets))
        source << line
        break if line.chomp == "__END__"
      end
      parse_common(source, source, options)
    end

    # Mirror the Ruby::Prism.parse_comments API by using the serialization API.
    def parse_comments(code, **options)
      parse_comments_common(string_source(code), code, options)
    end

    # Mirror the Ruby::Prism.parse_file_comments API by using the serialization
    # API.
    def parse_file_comments(filepath, **options)
      source = file_source(filepath, options)
      parse_comments_common(source, source, options)
    end

    # Mirror the Ruby::Prism.parse_lex API by using the serialization API.
    def parse_lex(code, **options)
      parse_lex_common(string_source(code), code, options)
    end

    # Mirror the Ruby::Prism.parse_lex_file API by using the serialization API.
    def parse_lex_file(filepath, **options)
      source = file_source(filepath, options)
      parse_lex_common(source, source, options)
    end

    # Mirror the Ruby::Prism.parse_success? API by using the serialization API.
    def parse_success?(code, **options)
      parse_file_success_common(string_source(code), options)
    end

    # Mirror the Ruby::Prism.parse_failure? API by using the serialization API.
    def parse_failure?(code, **options)
      !parse_success?(code, **options)
    end

    # Mirror the Ruby::Prism.parse_file_success? API by using the serialization
    # API.
    def parse_file_success?(filepath, **options)
      parse_file_success_common(file_source(filepath, options), options)
    end

    # Mirror the Ruby::Prism.parse_file_failure? API by using the serialization
    # API.
    def parse_file_failure?(filepath, **options)
      !parse_file_success?(filepath, **options)
    end

    # Mirror the Ruby::Prism.profile API by using the serialization API.
    def profile(source, **options)
      dump(source, **options)
      nil
    end

    # Mirror the Ruby::Prism.profile_file API by using the serialization API.
    def profile_file(filepath, **options)
      dump_file(filepath, **options)
      nil
    end

    private

    def string_source(string) # :nodoc:
      raise TypeError unless string.is_a?(String)
      string
    end

    # The file's bytes, as ffi.rb's mapped pm_source_t gives them (BINARY).
    # File.path validates the path the way the C extension's rb_get_path does.
    def file_source(filepath, options) # :nodoc:
      filepath = File.path(filepath)
      options[:filepath] = filepath
      raise Errno::EISDIR.new(filepath) if File.directory?(filepath)
      File.binread(filepath)
    end

    def dump_common(string, options) # :nodoc:
      if (format_type = raise_error_format_type(options))
        raise_error(string, options, format_type)
      end

      dumped = Kernel.__oruby_prism_serialize(SERIALIZE_PARSE, string, dump_options(options))
      dumped.freeze if options.fetch(:freeze, false)
      dumped
    end

    def lex_common(string, code, options) # :nodoc:
      format_type = raise_error_format_type(options)
      serialized = Kernel.__oruby_prism_serialize(SERIALIZE_LEX, string, dump_options(options))
      result = Serialize.load_lex(code, serialized, options.fetch(:freeze, false))

      raise_error(string, options, format_type) if format_type && result.failure?
      result
    end

    def parse_common(string, code, options) # :nodoc:
      format_type = raise_error_format_type(options)
      serialized = dump_common(string, options)
      result = Serialize.load_parse(code, serialized, options.fetch(:freeze, false))

      raise_error(string, options, format_type) if format_type && result.failure?
      result
    end

    def parse_comments_common(string, code, options) # :nodoc:
      if (format_type = raise_error_format_type(options))
        raise_error(string, options, format_type)
      end

      serialized = Kernel.__oruby_prism_serialize(SERIALIZE_COMMENTS, string, dump_options(options))
      Serialize.load_parse_comments(code, serialized, options.fetch(:freeze, false))
    end

    def parse_lex_common(string, code, options) # :nodoc:
      format_type = raise_error_format_type(options)
      serialized = Kernel.__oruby_prism_serialize(SERIALIZE_PARSE_LEX, string, dump_options(options))
      result = Serialize.load_parse_lex(code, serialized, options.fetch(:freeze, false))

      raise_error(string, options, format_type) if format_type && result.failure?
      result
    end

    def parse_file_success_common(string, options) # :nodoc:
      format_type = raise_error_format_type(options)
      success = Kernel.__oruby_prism_success?(string, dump_options(options))

      raise_error(string, options, format_type) if format_type && !success
      success
    end

    # Extract the raise_error option from the given options hash and convert
    # it into the format type that should be used when formatting errors, or
    # nil if raising is disabled.
    def raise_error_format_type(options) # :nodoc:
      case (value = options.delete(:raise_error))
      when nil, false
        nil
      when true
        # Mirror CRuby: color when $stderr is a terminal (unless NO_COLOR is
        # set), bold styling when NO_COLOR is set, plain otherwise.
        if $stderr.respond_to?(:tty?) && $stderr.tty?
          no_color = ENV["NO_COLOR"]
          (no_color.nil? || no_color.empty?) ? :PM_ERRORS_FORMAT_COLOR : :PM_ERRORS_FORMAT_STYLE
        else
          :PM_ERRORS_FORMAT_PLAIN
        end
      when :plain
        :PM_ERRORS_FORMAT_PLAIN
      when :style
        :PM_ERRORS_FORMAT_STYLE
      when :color
        :PM_ERRORS_FORMAT_COLOR
      when Symbol
        raise ArgumentError, "invalid raise_error value: #{value}"
      else
        raise TypeError, "wrong argument type #{value.class} (expected Symbol)"
      end
    end

    # Parse the given source and format any errors that are encountered into
    # an appropriate exception, then raise it. If the source parses without
    # any errors, then return nil.
    def raise_error(string, options, format_type) # :nodoc:
      level, text = Kernel.__oruby_prism_errors(string, dump_options(options), ERRORS_FORMAT.fetch(format_type))
      return if level.nil?

      encoding_name, _, message = text.partition("\0")

      case level
      when 0 # syntax
        error = SyntaxError.new(message.force_encoding(encoding_name))
        error.instance_variable_set(:@path, options.fetch(:filepath, ""))
        raise error
      when 1 # argument
        raise ArgumentError, message.force_encoding(encoding_name)
      when 2 # load
        error = LoadError.new(message.force_encoding(Encoding.find("locale")))
        error.instance_variable_set(:@path, nil)
        raise error
      else
        raise "Unknown error level: #{level}"
      end
    end

    # Return the value that should be dumped for the command_line option.
    def dump_options_command_line(options)
      command_line = options.fetch(:command_line, "")
      raise ArgumentError, "command_line must be a string" unless command_line.is_a?(String)

      command_line.each_char.inject(0) do |value, char|
        case char
        when "a" then value | 0b000001
        when "e" then value | 0b000010
        when "l" then value | 0b000100
        when "n" then value | 0b001000
        when "p" then value | 0b010000
        when "x" then value | 0b100000
        else raise ArgumentError, "invalid command_line option: #{char}"
        end
      end
    end

    # Return the value that should be dumped for the version option.
    def dump_options_version(version)
      case version
      when "current"
        version_string_to_number(RUBY_VERSION) || raise(CurrentVersionError, RUBY_VERSION)
      when "latest"
        0 # Handled in pm_parser_init
      when "nearest", nil
        dump = version_string_to_number(RUBY_VERSION)
        return dump if dump
        if RUBY_VERSION < "3.3"
          version_string_to_number("3.3")
        else
          0 # Handled in pm_parser_init
        end
      else
        version_string_to_number(version) || raise(ArgumentError, "invalid version: #{version}")
      end
    end

    # Converts a version string like "4.0.0" or "4.0" into a number.
    # Returns nil if the version is unknown.
    def version_string_to_number(version)
      case version
      when /\A3\.3(\.\d+)?\z/
        1
      when /\A3\.4(\.\d+)?\z/
        2
      when /\A3\.5(\.\d+)?\z/, /\A4\.0(\.\d+)?\z/
        3
      when /\A4\.1(\.\d+)?\z/
        4
      end
    end

    # The set of options that are understood by the parsing APIs. Note that
    # raise_error is not listed here because it is deleted from the options
    # hash by raise_error_format_type before the options are dumped.
    DUMP_OPTIONS_KEYS = [:attach_comments, :command_line, :encoding, :filepath, :freeze, :frozen_string_literal, :line, :main_script, :partial_script, :scopes, :version].freeze
    private_constant :DUMP_OPTIONS_KEYS

    # Convert the given options into a serialized options string.
    def dump_options(options)
      unknown_keys = options.keys - DUMP_OPTIONS_KEYS
      raise ArgumentError, "unknown keyword: #{unknown_keys.first}" unless unknown_keys.empty?

      template = +""
      values = []

      template << "L"
      if (filepath = options[:filepath])
        values.push(filepath.bytesize, filepath.b)
        template << "A*"
      else
        values << 0
      end

      template << "l"
      values << options.fetch(:line, 1)

      template << "L"
      if (encoding = options[:encoding])
        name = encoding.is_a?(Encoding) ? encoding.name : encoding
        values.push(name.bytesize, name.b)
        template << "A*"
      else
        values << 0
      end

      template << "C"
      values << (options.fetch(:frozen_string_literal, false) ? 1 : 0)

      template << "C"
      values << dump_options_command_line(options)

      template << "C"
      values << dump_options_version(options[:version])

      template << "C"
      values << (options[:encoding] == false ? 1 : 0)

      template << "C"
      values << (options.fetch(:main_script, false) ? 1 : 0)

      template << "C"
      values << (options.fetch(:partial_script, false) ? 1 : 0)

      template << "C"
      values << (options.fetch(:freeze, false) ? 1 : 0)

      template << "C"
      values << (options.fetch(:attach_comments, false) ? 1 : 0)

      template << "L"
      if (scopes = options[:scopes])
        values << scopes.length

        scopes.each do |scope|
          locals = nil
          forwarding = 0

          case scope
          when Array
            locals = scope
          when Scope
            locals = scope.locals

            scope.forwarding.each do |forward|
              case forward
              when :*     then forwarding |= 0x1
              when :**    then forwarding |= 0x2
              when :&     then forwarding |= 0x4
              when :"..." then forwarding |= 0x8
              else raise ArgumentError, "invalid forwarding value: #{forward}"
              end
            end
          else
            raise TypeError, "wrong argument type #{scope.class.inspect} (expected Array or Ruby::Prism::Scope)"
          end

          template << "L"
          values << locals.length

          template << "C"
          values << forwarding

          locals.each do |local|
            name = local.name
            template << "L"
            values << name.bytesize

            template << "A*"
            values << name.b
          end
        end
      else
        values << 0
      end

      values.pack(template)
    end
  end

  # The class-level StringQuery methods the C extension defines.
  class StringQuery # :nodoc:
    class << self
      # Mirrors the C extension's StringQuery::local? method.
      def local?(string)
        query(Kernel.__oruby_prism_query(0, string, string.encoding.name))
      end

      # Mirrors the C extension's StringQuery::constant? method.
      def constant?(string)
        query(Kernel.__oruby_prism_query(1, string, string.encoding.name))
      end

      # Mirrors the C extension's StringQuery::method_name? method.
      def method_name?(string)
        query(Kernel.__oruby_prism_query(2, string, string.encoding.name))
      end

      # Mirrors the C extension's StringQuery::keyword? method.
      def keyword?(string)
        query(Kernel.__oruby_prism_query(3, string, string.encoding.name))
      end

      private

      # pm_string_query_t: -1 error, 0 false, 1 true
      def query(result)
        raise ArgumentError, "Invalid or non ascii-compatible encoding" if result < 0
        result == 1
      end
    end
  end
end
