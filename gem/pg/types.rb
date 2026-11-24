# frozen_string_literal: true

module PG
  # ==========================================================================
  # PG::Coder hierarchy
  # ==========================================================================

  class Coder
    TIMESTAMP_DB_UTC = 0
    TIMESTAMP_DB_LOCAL = 1
    TIMESTAMP_APP_UTC = 0
    TIMESTAMP_APP_LOCAL = 2

    FORMAT_ERROR_MASK = 0xc
    FORMAT_ERROR_TO_RAISE = 0x4
    FORMAT_ERROR_TO_STRING = 0x8
    FORMAT_ERROR_TO_PARTIAL = 0xc

    module BinaryFormatting
      def initialize(hash = nil, **kwargs)
        if hash
          warn("PG::Coder.new(hash) is deprecated. Please use keyword arguments instead! Called from #{caller.first}", category: :deprecated) unless hash.empty?
          super(format: 1, **hash, **kwargs)
        else
          super(format: 1, **kwargs)
        end
      end
    end

    def initialize(hash = nil, **kwargs)
      if self.class == PG::Coder
        raise TypeError, "cannot instantiate PG::Coder directly"
      end
      warn("PG::Coder.new(hash) is deprecated. Please use keyword arguments instead! Called from #{caller.first}", category: :deprecated) if hash
      @oid = 0
      @format = 0
      @flags = 0
      @name = nil
      opts = hash ? hash.merge(kwargs) : kwargs
      opts.each do |key, val|
        send("#{key}=", val)
      end
    end

    attr_reader :oid, :format, :flags, :name

    def oid=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise TypeError, "oid must be an Integer" unless v.is_a?(Integer)
      @oid = v
    end

    def format=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise TypeError, "format must be an Integer" unless v.is_a?(Integer)
      @format = v
    end

    def flags=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise TypeError, "flags must be an Integer" unless v.is_a?(Integer)
      @flags = v
    end

    def name=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      @name = v.nil? ? nil : v.to_s
    end

    def dup
      self.class.new(**to_h)
    end

    def to_h
      {
        oid: @oid,
        format: @format,
        flags: @flags,
        name: @name,
      }
    end

    def ==(other)
      self.class == other.class && to_h == other.to_h
    end

    def marshal_dump
      Marshal.dump(to_h)
    end

    def marshal_load(str)
      initialize(**Marshal.load(str))
    end

    def inspect
      str = super
      oid_str = " oid=#{oid}" unless oid == 0
      format_str = " format=#{format}" unless format == 0
      name_str = " #{name.inspect}" if name
      str[-1, 0] = "#{name_str} #{oid_str}#{format_str}"
      str
    end

    def inspect_short
      str = case format
            when 0 then "T"
            when 1 then "B"
            else format.to_s
            end
      str += "E" if respond_to?(:encode)
      str += "D" if respond_to?(:decode)
      "#{name || self.class.name}:#{str}"
    end
  end

  class SimpleCoder < Coder
  end

  class SimpleEncoder < SimpleCoder
    def encode(value, _encoding = nil)
      value.to_s
    end
  end

  class SimpleDecoder < SimpleCoder
    def decode(string, _tuple = nil, _field = nil)
      string
    end
  end

  class CompositeCoder < Coder
    def initialize(hash = nil, **kwargs)
      @elements_type = nil
      @needs_quotation = true
      @delimiter = ","
      @dimensions = nil
      super
    end

    attr_reader :elements_type, :delimiter, :dimensions

    def needs_quotation?
      @needs_quotation
    end

    def needs_quotation=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      @needs_quotation = !!v
    end

    def elements_type=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      if !v.nil? && !v.is_a?(PG::Coder)
        raise TypeError, "elements_type must be a PG::Coder or nil"
      end
      @elements_type = v
    end

    def delimiter=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      s = v.to_s
      raise ArgumentError, "delimiter must be a single byte" unless s.bytesize == 1
      @delimiter = s
    end

    def dimensions=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      if !v.nil? && (!v.is_a?(Integer) || v < 1)
        raise ArgumentError, "dimensions must be a positive Integer or nil"
      end
      @dimensions = v
    end

    def to_h
      h = super.merge(
        elements_type: @elements_type,
        needs_quotation: @needs_quotation,
        delimiter: @delimiter
      )
      h[:dimensions] = @dimensions if @dimensions
      h
    end

    def inspect
      str = super
      str[-1, 0] = " elements_type=#{elements_type.inspect} #{needs_quotation? ? 'needs' : 'no'} quotation#{dimensions && " #{dimensions} dimensions"}"
      str
    end
  end

  class CompositeEncoder < CompositeCoder
  end

  class CompositeDecoder < CompositeCoder
  end

  class CopyCoder < Coder
    def initialize(hash = nil, **kwargs)
      @type_map = nil
      @delimiter = "\t"
      @null_string = "\\N"
      super
    end

    attr_reader :type_map, :delimiter, :null_string

    def type_map=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      if !v.nil? && !v.is_a?(PG::TypeMap)
        raise TypeError, "type_map must be a PG::TypeMap"
      end
      @type_map = v
    end

    def delimiter=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      @delimiter = v.to_s
    end

    def null_string=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      @null_string = v.to_s
    end

    def to_h
      super.merge(
        type_map: @type_map,
        delimiter: @delimiter,
        null_string: @null_string
      )
    end
  end

  class CopyEncoder < CopyCoder
  end

  class CopyDecoder < CopyCoder
  end

  class RecordCoder < Coder
    def initialize(hash = nil, **kwargs)
      @type_map = nil
      super
    end

    attr_reader :type_map

    def type_map=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      if !v.nil? && !v.is_a?(PG::TypeMap)
        raise TypeError, "type_map must be a PG::TypeMap"
      end
      @type_map = v
    end

    def to_h
      super.merge(type_map: @type_map)
    end
  end

  class RecordEncoder < RecordCoder
  end

  class RecordDecoder < RecordCoder
  end

  # ==========================================================================
  # Timestamp / Date helpers
  # ==========================================================================

  module CoderHelpers
    PG_EPOCH_UNIX = 946684800 # 2000-01-01 00:00:00 UTC
    PG_EPOCH_JD = 2451545     # Date.new(2000, 1, 1).jd
    INT64_MAX = (1 << 63) - 1
    INT64_MIN = -(1 << 63)
    INT32_MAX = (1 << 31) - 1
    INT32_MIN = -(1 << 31)

    def self.parse_timestamp(str, mode)
      return nil if str.nil?
      raise TypeError, "wrong argument type #{str.class} (expected String)" unless str.is_a?(::String)
      return str unless str.ascii_only?
      return str if str.empty? || str.getbyte(0) <= 32 || str.getbyte(-1) <= 32
      s = str
      return str if s.casecmp("infinity") == 0 || s.casecmp("-infinity") == 0

      bc = false
      if s.end_with?(" BC") || s.end_with?(" bc")
        bc = true
        s = s[0...-3]
      end

      m = /\A(\d{4,})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(?:([+-])(\d{2})(?::?(\d{2})(?::?(\d{2}))?)?|([Zz]))?\z/.match(s)
      return str unless m

      year = m[1].to_i
      year = 1 - year if bc
      mon = m[2].to_i
      day = m[3].to_i
      hour = m[4].to_i
      min = m[5].to_i
      sec = m[6].to_i
      frac = m[7] ? Rational("0.#{m[7]}") : 0
      sec_with_frac = sec + frac

      if m[8] || m[12]
        # Explicit timezone in string
        if m[12]
          tz = "+00:00"
        else
          sign = m[8]
          tzh = m[9]
          tzm = m[10] || "00"
          tzs = m[11]
          tz = tzs ? "#{sign}#{tzh}:#{tzm}:#{tzs}" : "#{sign}#{tzh}:#{tzm}"
        end
        t = Time.new(year, mon, day, hour, min, sec_with_frac, tz)
        case mode
        when :utc then t.utc
        when :local then t.getlocal
        else t
        end
      else
        case mode
        when :utc
          Time.utc(year, mon, day, hour, min, sec_with_frac)
        when :utc_to_local
          Time.utc(year, mon, day, hour, min, sec_with_frac).getlocal
        else
          Time.new(year, mon, day, hour, min, sec_with_frac)
        end
      end
    end

    def self.format_timestamp(val, mode)
      return nil if val.nil?
      return val if val.is_a?(String)
      raise TypeError, "wrong argument type #{val.class} (expected Time)" unless val.is_a?(Time)
      t = case mode
          when :utc then val.utc
          else val
          end
      year = t.year
      bc_suffix = ""
      if year <= 0
        year = 1 - year
        bc_suffix = " BC"
      end
      base = format("%04d-%02d-%02d %02d:%02d:%02d", year, t.mon, t.day, t.hour, t.min, t.sec)
      nsec = t.nsec
      if nsec != 0
        frac = format("%09d", nsec).sub(/0+\z/, "")
        base = "#{base}.#{frac}"
      end
      if mode == :with_tz
        off = t.utc_offset
        sign = off < 0 ? "-" : "+"
        off = off.abs
        oh = off / 3600
        om = (off % 3600) / 60
        os = off % 60
        tz = os == 0 ? format(" %s%02d:%02d", sign, oh, om) : format(" %s%02d:%02d:%02d", sign, oh, om, os)
        "#{base}#{tz}#{bc_suffix}"
      else
        "#{base}#{bc_suffix}"
      end
    end
  end

  # ==========================================================================
  # PG::TextEncoder
  # ==========================================================================

  module TextEncoder
    class Boolean < SimpleEncoder
      def encode(value, _encoding = nil)
        return nil if value.nil?
        case value
        when true then "t"
        when false then "f"
        else value.to_s
        end
      end
    end

    class Integer < SimpleEncoder
      def encode(value, _encoding = nil)
        return nil if value.nil?
        if value.is_a?(::Integer)
          value.to_s
        elsif value.is_a?(::String)
          value.to_i.to_s
        elsif value.respond_to?(:to_int)
          value.to_int.to_s
        else
          raise TypeError, "can't convert #{value.class} into Integer"
        end
      end
    end

    class Float < SimpleEncoder
      def encode(value, _encoding = nil)
        return nil if value.nil?
        f = value.is_a?(::Float) ? value : Kernel.Float(value)
        if f.nan?
          "NaN"
        elsif f.infinite?
          f > 0 ? "Infinity" : "-Infinity"
        else
          f.to_s
        end
      end
    end

    class Numeric < SimpleEncoder
      def encode(value, _encoding = nil)
        return nil if value.nil?
        case value
        when ::Integer then value.to_s
        when ::Float
          return "NaN" if value.nan?
          return (value > 0 ? "Infinity" : "-Infinity") if value.infinite?
          value.to_s
        else
          if defined?(::BigDecimal) && value.is_a?(::BigDecimal)
            return "NaN" if value.nan?
            return (value > 0 ? "Infinity" : "-Infinity") if value.infinite?
            value.to_s("F")
          else
            value.to_s
          end
        end
      end
    end

    class String < SimpleEncoder
      def encode(value, _encoding = nil)
        return nil if value.nil?
        value.to_s
      end
    end

    class Bytea < SimpleEncoder
      def encode(value, _encoding = nil)
        return nil if value.nil?
        s = value.to_s
        raise ArgumentError, "bytea string too large" if s.bytesize >= (1 << 30)
        PG::Connection.escape_bytea(s)
      end
    end

    class Identifier < SimpleEncoder
      def encode(value, encoding = nil)
        return nil if value.nil?
        enc = encoding ? Encoding.find(encoding) : nil
        quote_one = lambda do |v|
          raise TypeError, "wrong argument type #{v.class} (expected String)" unless v.is_a?(::String)
          s = enc ? v.encode(enc) : v
          q = '"'.encode(s.encoding)
          qq = '""'.encode(s.encoding)
          q + s.gsub(q, qq) + q
        end
        if value.is_a?(::Array)
          sep = ".".encode(enc || Encoding::UTF_8)
          value.map(&quote_one).join(sep)
        else
          quote_one.call(value)
        end
      end
    end

    class ToBase64 < CompositeEncoder
      def encode(value, encoding = nil)
        return nil if value.nil?
        require "base64"
        raw = @elements_type ? @elements_type.encode(value, encoding) : value.to_s
        return nil if raw.nil?
        raw = raw.encode(Encoding.find(encoding)) if encoding && raw.encoding != Encoding::ASCII_8BIT
        ::Base64.strict_encode64(raw.b).force_encoding(Encoding::UTF_8)
      end
    end

    class TimestampWithoutTimeZone < SimpleEncoder
      def encode(value, _encoding = nil)
        CoderHelpers.format_timestamp(value, :local)
      end
    end
    TimestampLocal = TimestampWithoutTimeZone

    class TimestampUtc < SimpleEncoder
      def encode(value, _encoding = nil)
        CoderHelpers.format_timestamp(value, :utc)
      end
    end

    class TimestampWithTimeZone < SimpleEncoder
      def encode(value, _encoding = nil)
        CoderHelpers.format_timestamp(value, :with_tz)
      end
    end

    class Date < SimpleEncoder
      def encode(value, _encoding = nil)
        return value if value.is_a?(::String)
        if defined?(::Date) && value.is_a?(::Date)
          # PostgreSQL uses proleptic Gregorian calendar
          d = value.gregorian
          year = d.year
          if year <= 0
            format("%04d-%02d-%02d BC", 1 - year, d.mon, d.day)
          else
            format("%04d-%02d-%02d", year, d.mon, d.day)
          end
        elsif value.is_a?(::Time)
          format("%04d-%02d-%02d", value.year, value.mon, value.day)
        else
          raise TypeError, "wrong argument type #{value.class} (expected Date)"
        end
      end
    end

    class JSON < SimpleEncoder
      def encode(value, _encoding = nil)
        return nil if value.nil?
        require "json"
        ::JSON.generate(value)
      end
    end

    class Inet < SimpleEncoder
      def encode(value, _encoding = nil)
        if defined?(::IPAddr) && value.is_a?(::IPAddr)
          if value.ipv4? && value.prefix == 32
            value.to_s
          elsif value.ipv6? && value.prefix == 128
            value.to_s
          else
            "#{value}/#{value.prefix}"
          end
        else
          value.to_s
        end
      end
    end

    class Array < CompositeEncoder
      def encode(value, encoding = nil)
        raise TypeError, "wrong argument type #{value.class} (expected Array)" unless value.is_a?(::Array)
        encode_array(value, encoding)
      end

      private

      def encode_array(arr, encoding)
        parts = arr.map do |el|
          if el.nil?
            "NULL"
          elsif el.is_a?(::Array)
            encode_array(el, encoding)
          else
            s = @elements_type ? @elements_type.encode(el, encoding) : el.to_s
            if @needs_quotation
              '"' + s.gsub('\\', '\\\\').gsub('"', '\\"') + '"'
            else
              s
            end
          end
        end
        "{#{parts.join(@delimiter)}}"
      end
    end

    class CopyRow < CopyEncoder
      def encode(value, encoding = nil)
        raise TypeError, "wrong argument type #{value.class} (expected Array)" unless value.is_a?(::Array)
        enc = encoding ? Encoding.find(encoding) : Encoding::UTF_8
        tm = @type_map
        total = 0
        parts = value.each_with_index.map do |el, idx|
          if el.nil?
            @null_string.encode(enc)
          else
            s = if tm
                  res = tm.__encode_param(el, idx)
                  res ? res[0] : el.to_s
                else
                  el.to_s
                end
            if s.nil?
              @null_string.encode(enc)
            else
              raise ArgumentError, "copy row string size too large" if s.bytesize >= (1 << 30) || (total += s.bytesize) >= (1 << 30)
              s = s.encode(enc) if s.encoding != enc && s.encoding != Encoding::ASCII_8BIT
              s.gsub("\\", "\\\\").gsub("\n", "\\n").gsub("\r", "\\r").gsub("\t", "\\t")
            end
          end
        end
        delim = @delimiter.encode(enc)
        nl = "\n".encode(enc)
        (parts.join(delim) + nl).force_encoding(enc)
      end
    end

    class Record < RecordEncoder
      def encode(value, _encoding = nil)
        raise TypeError, "wrong argument type #{value.class} (expected Array)" unless value.is_a?(::Array)
        tm = @type_map
        parts = value.each_with_index.map do |el, idx|
          if el.nil?
            ""
          else
            s = if tm
                  enc = tm.__encode_param(el, idx)
                  enc ? enc[0] : el.to_s
                else
                  el.to_s
                end
            if s.nil?
              ""
            elsif s.empty? || s.include?(",") || s.include?("(") || s.include?(")") || s.include?('"') || s.include?("\\") || s =~ /\s/
              '"' + s.gsub("\\", "\\\\").gsub('"', '""') + '"'
            else
              s
            end
          end
        end
        "(#{parts.join(",")})"
      end
    end
  end

  # ==========================================================================
  # PG::TextDecoder
  # ==========================================================================

  module TextDecoder
    class Boolean < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        case string
        when "t", "true", "TRUE", "1" then true
        when "f", "false", "FALSE", "0" then false
        else string
        end
      end
    end

    class Integer < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        return nil if string.nil?
        raise TypeError, "wrong argument type #{string.class} (expected String)" unless string.is_a?(::String)
        s = string.strip
        if s =~ /\A[+-]?\d+\z/
          s.to_i
        else
          string
        end
      end
    end

    class Float < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        case string
        when "NaN", "nan" then ::Float::NAN
        when "Infinity", "infinity" then ::Float::INFINITY
        when "-Infinity", "-infinity" then -::Float::INFINITY
        else
          begin
            Kernel.Float(string)
          rescue ArgumentError
            string
          end
        end
      end
    end

    class Numeric < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        PG.require_bigdecimal_without_warning
        case string
        when "NaN", "nan" then Kernel.BigDecimal("NaN")
        when "Infinity", "infinity" then Kernel.BigDecimal("Infinity")
        when "-Infinity", "-infinity" then Kernel.BigDecimal("-Infinity")
        else Kernel.BigDecimal(string)
        end
      end
    end

    class String < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        return nil if string.nil?
        raise TypeError, "wrong argument type #{string.class} (expected String)" unless string.is_a?(::String)
        string
      end
    end

    class Bytea < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        PG::Connection.unescape_bytea(string)
      end
    end

    class Identifier < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        out = []
        i = 0
        n = string.length
        while i < n
          if string[i] == '"'
            i += 1
            buf = +""
            while i < n
              if string[i] == '"'
                if string[i + 1] == '"'
                  buf << '"'
                  i += 2
                else
                  i += 1
                  break
                end
              else
                buf << string[i]
                i += 1
              end
            end
            out << buf
            i += 1 if string[i] == "."
          else
            j = string.index(".", i) || n
            out << string[i...j]
            i = j + 1
          end
        end
        out
      end
    end

    class FromBase64 < CompositeDecoder
      def decode(string, tuple = nil, field = nil)
        return nil if string.nil?
        require "base64"
        raw = ::Base64.decode64(string)
        if @elements_type
          @elements_type.decode(raw.b, tuple, field)
        else
          raw.force_encoding(string.encoding)
        end
      end
    end

    class TimestampWithoutTimeZone < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        CoderHelpers.parse_timestamp(string, :local)
      end
    end
    TimestampLocal = TimestampWithoutTimeZone

    class TimestampUtc < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        CoderHelpers.parse_timestamp(string, :utc)
      end
    end

    class TimestampUtcToLocal < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        CoderHelpers.parse_timestamp(string, :utc_to_local)
      end
    end

    class TimestampWithTimeZone < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        CoderHelpers.parse_timestamp(string, :with_tz)
      end
    end

    class Date < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        return nil if string.nil?
        raise TypeError, "wrong argument type #{string.class} (expected String)" unless string.is_a?(::String)
        require "date"
        return string unless string.ascii_only?
        m = /\A(\d{4})-(\d{2})-(\d{2})\z/.match(string)
        return string unless m
        ::Date.new(m[1].to_i, m[2].to_i, m[3].to_i, ::Date::GREGORIAN)
      end
    end

    class JSON < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        return nil if string.nil?
        require "json"
        ::JSON.parse(string)
      end
    end

    class Inet < SimpleDecoder
      def decode(string, _tuple = nil, _field = nil)
        require "ipaddr"
        ::IPAddr.new(string)
      end
    end

    class Array < CompositeDecoder
      def decode(string, tuple = nil, field = nil)
        s = string
        # Skip optional dimension prefix like [1:2]={...}
        if s.start_with?("[")
          eq = s.index("=")
          s = s[(eq + 1)..-1] if eq
        end
        return string unless s.start_with?("{") && s.end_with?("}")
        arr, _ = parse_array(s, 0, tuple, field)
        arr
      end

      private

      def parse_array(s, pos, tuple, field)
        pos += 1 # skip '{'
        out = []
        n = s.length
        delim = @delimiter
        while pos < n
          c = s[pos]
          if c == "}"
            return [out, pos + 1]
          elsif c == "{"
            sub, pos = parse_array(s, pos, tuple, field)
            out << sub
            pos += 1 if pos < n && s[pos] == delim
          elsif c == '"'
            pos += 1
            buf = +""
            while pos < n
              ch = s[pos]
              if ch == "\\" && pos + 1 < n
                buf << s[pos + 1]
                pos += 2
              elsif ch == '"'
                pos += 1
                break
              else
                buf << ch
                pos += 1
              end
            end
            out << (@elements_type ? @elements_type.decode(buf, tuple, field) : buf)
            pos += 1 if pos < n && s[pos] == delim
          else
            buf = +""
            while pos < n && s[pos] != delim && s[pos] != "}"
              buf << s[pos]
              pos += 1
            end
            if buf.casecmp("NULL") == 0
              out << nil
            else
              out << (@elements_type ? @elements_type.decode(buf, tuple, field) : buf)
            end
            pos += 1 if pos < n && s[pos] == delim
          end
        end
        [out, pos]
      end
    end

    class CopyRow < CopyDecoder
      def decode(string, _tuple = nil, _field = nil)
        line = string.sub(/\r?\n\z/, "")
        return [] if line.empty? && @delimiter != ""
        fields = line.split(@delimiter, -1)
        tm = @type_map
        fields.each_with_index.map do |raw, idx|
          if raw == @null_string
            nil
          else
            val = unescape_copy_text(raw)
            tm ? tm.__decode_copy_field(val, idx, 0) : val
          end
        end
      end

      private

      def unescape_copy_text(s)
        return s unless s.include?("\\")
        out = +""
        i = 0
        n = s.length
        while i < n
          if s[i] == "\\" && i + 1 < n
            nxt = s[i + 1]
            case nxt
            when "b" then out << "\b"; i += 2
            when "f" then out << "\f"; i += 2
            when "n" then out << "\n"; i += 2
            when "r" then out << "\r"; i += 2
            when "t" then out << "\t"; i += 2
            when "v" then out << "\v"; i += 2
            when "\\" then out << "\\"; i += 2
            else out << nxt; i += 2
            end
          else
            out << s[i]
            i += 1
          end
        end
        out
      end
    end

    class Record < RecordDecoder
      def decode(string, _tuple = nil, _field = nil)
        return string unless string.start_with?("(") && string.end_with?(")")
        s = string[1...-1]
        fields = []
        i = 0
        n = s.length
        while i <= n
          if i == n
            fields << nil if !fields.empty? && string[-2] == ","
            break
          end
          if s[i] == '"'
            i += 1
            buf = +""
            while i < n
              if s[i] == '"'
                if s[i + 1] == '"'
                  buf << '"'
                  i += 2
                else
                  i += 1
                  break
                end
              elsif s[i] == "\\" && i + 1 < n
                buf << s[i + 1]
                i += 2
              else
                buf << s[i]
                i += 1
              end
            end
            fields << buf
            i += 1 if i < n && s[i] == ","
          else
            j = s.index(",", i) || n
            tok = s[i...j]
            fields << (tok.empty? ? nil : tok)
            i = j + 1
          end
        end
        if @type_map
          fields.each_with_index.map do |v, idx|
            v.nil? ? nil : @type_map.__decode_copy_field(v, idx, 0)
          end
        else
          fields
        end
      end
    end
  end

  # ==========================================================================
  # PG::BinaryEncoder
  # ==========================================================================

  module BinaryEncoder
    class Boolean < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        case value
        when true then "\x01".b
        when false then "\x00".b
        else raise TypeError, "wrong argument type #{value.class} (expected true or false)"
        end
      end
    end

    class Int2 < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        v = value.is_a?(::String) ? value.to_i : Kernel.Integer(value)
        [v].pack("s>")
      end
    end

    class Int4 < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        v = value.is_a?(::String) ? value.to_i : Kernel.Integer(value)
        [v].pack("l>")
      end
    end

    class Int8 < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        v = value.is_a?(::String) ? value.to_i : Kernel.Integer(value)
        [v].pack("q>")
      end
    end

    class Float4 < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        [Kernel.Float(value)].pack("g")
      end
    end

    class Float8 < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        [Kernel.Float(value)].pack("G")
      end
    end

    class String < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        value.to_s
      end
    end

    class Bytea < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        return nil if value.nil?
        value.to_s.b
      end
    end

    class FromBase64 < CompositeEncoder
      include Coder::BinaryFormatting
      def encode(value, encoding = nil)
        return nil if value.nil?
        require "base64"
        raw = @elements_type ? @elements_type.encode(value, encoding) : value.to_s
        return nil if raw.nil?
        ::Base64.decode64(raw).b
      end
    end

    class TimestampUtc < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        if value.is_a?(::String)
          s = value.strip.downcase
          return [CoderHelpers::INT64_MAX].pack("q>") if s == "infinity"
          return [CoderHelpers::INT64_MIN].pack("q>") if s == "-infinity"
          raise TypeError, "wrong argument type String (expected Time)"
        end
        raise TypeError, "wrong argument type #{value.class} (expected Time)" unless value.is_a?(::Time)
        usec = (value.tv_sec - CoderHelpers::PG_EPOCH_UNIX) * 1_000_000 + value.tv_usec
        [usec].pack("q>")
      end
    end

    class TimestampLocal < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        if value.is_a?(::String)
          s = value.strip.downcase
          return [CoderHelpers::INT64_MAX].pack("q>") if s == "infinity"
          return [CoderHelpers::INT64_MIN].pack("q>") if s == "-infinity"
          raise TypeError, "wrong argument type String (expected Time)"
        end
        raise TypeError, "wrong argument type #{value.class} (expected Time)" unless value.is_a?(::Time)
        t = value.getlocal
        usec = (t.tv_sec + t.utc_offset - CoderHelpers::PG_EPOCH_UNIX) * 1_000_000 + t.tv_usec
        [usec].pack("q>")
      end
    end

    class Date < SimpleEncoder
      include Coder::BinaryFormatting
      def encode(value, _encoding = nil)
        if value.is_a?(::String)
          s = value.strip.downcase
          return [CoderHelpers::INT32_MAX].pack("l>") if s == "infinity"
          return [CoderHelpers::INT32_MIN].pack("l>") if s == "-infinity"
          raise TypeError, "wrong argument type String (expected Date)"
        end
        require "date"
        if value.is_a?(::Date)
          days = value.jd - CoderHelpers::PG_EPOCH_JD
          [days].pack("l>")
        elsif value.is_a?(::Time)
          days = ::Date.new(value.year, value.mon, value.day).jd - CoderHelpers::PG_EPOCH_JD
          [days].pack("l>")
        else
          raise TypeError, "wrong argument type #{value.class} (expected Date)"
        end
      end
    end

    class Array < CompositeEncoder
      include Coder::BinaryFormatting

      def encode(value, encoding = nil)
        raise TypeError, "wrong argument type #{value.class} (expected Array)" unless value.is_a?(::Array)
        dims = []
        cur = value
        while cur.is_a?(::Array)
          dims << cur.length
          cur = cur.first
        end
        elem_oid = @elements_type ? @elements_type.oid : 25
        flat = []
        has_null = 0
        collect = lambda do |arr, depth|
          raise ArgumentError, "inconsistent array dimensions" unless arr.length == dims[depth]
          arr.each do |el|
            if depth + 1 < dims.length
              raise ArgumentError, "inconsistent array dimensions" unless el.is_a?(::Array)
              collect.call(el, depth + 1)
            else
              if el.nil?
                has_null = 1
                flat << nil
              else
                enc = @elements_type ? @elements_type.encode(el, encoding) : el.to_s
                flat << enc.b
              end
            end
          end
        end
        collect.call(value, 0) unless dims == [0]
        if dims == [0]
          return [0, 0, elem_oid].pack("l>l>L>")
        end
        buf = [dims.length, has_null, elem_oid].pack("l>l>L>")
        dims.each { |d| buf << [d, 1].pack("l>l>") }
        flat.each do |el|
          if el.nil?
            buf << [-1].pack("l>")
          else
            buf << [el.bytesize].pack("l>") << el
          end
        end
        buf
      end
    end

    class CopyRow < CopyEncoder
      include Coder::BinaryFormatting

      def encode(value, encoding = nil)
        raise TypeError, "wrong argument type #{value.class} (expected Array)" unless value.is_a?(::Array)
        enc = encoding ? Encoding.find(encoding) : nil
        tm = @type_map
        buf = [value.length].pack("s>")
        total = 2
        value.each_with_index do |el, idx|
          if el.nil?
            buf << [-1].pack("l>")
            total += 4
          else
            raw = if tm
                    res = tm.__encode_param(el, idx)
                    res ? res[0] : el.to_s
                  else
                    el.to_s
                  end
            if raw.nil?
              buf << [-1].pack("l>")
              total += 4
            else
              raw = raw.encode(enc) if enc && raw.encoding != enc && raw.encoding != Encoding::ASCII_8BIT
              raise ArgumentError, "binary copy row size too large" if raw.bytesize >= (1 << 30) || (total += raw.bytesize + 4) >= (1 << 30)
              b = raw.b
              buf << [b.bytesize].pack("l>") << b
            end
          end
        end
        enc ? buf.force_encoding(enc) : buf
      end
    end
  end

  # ==========================================================================
  # PG::BinaryDecoder
  # ==========================================================================

  module BinaryDecoder
    class Boolean < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        string.getbyte(0) != 0
      end
    end

    class Integer < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        case string.bytesize
        when 2 then string.unpack1("s>")
        when 4 then string.unpack1("l>")
        when 8 then string.unpack1("q>")
        else raise ArgumentError, "unexpected integer byte length #{string.bytesize}"
        end
      end
    end

    class Float < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        case string.bytesize
        when 4 then string.unpack1("g")
        when 8 then string.unpack1("G")
        else raise ArgumentError, "unexpected float byte length #{string.bytesize}"
        end
      end
    end

    class String < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        return nil if string.nil?
        string.dup.force_encoding(Encoding::UTF_8)
      end
    end

    class Bytea < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        return nil if string.nil?
        string.b
      end
    end

    class ToBase64 < CompositeDecoder
      include Coder::BinaryFormatting
      def decode(string, tuple = nil, field = nil)
        return nil if string.nil?
        require "base64"
        raw = ::Base64.strict_encode64(string).b
        @elements_type ? @elements_type.decode(raw, tuple, field) : raw
      end
    end

    class TimestampUtc < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        usec = string.unpack1("q>")
        return "infinity" if usec == CoderHelpers::INT64_MAX
        return "-infinity" if usec == CoderHelpers::INT64_MIN
        sec, rem = usec.divmod(1_000_000)
        Time.at(sec + CoderHelpers::PG_EPOCH_UNIX, rem, :usec).utc
      end
    end

    class TimestampUtcToLocal < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        usec = string.unpack1("q>")
        return "infinity" if usec == CoderHelpers::INT64_MAX
        return "-infinity" if usec == CoderHelpers::INT64_MIN
        sec, rem = usec.divmod(1_000_000)
        Time.at(sec + CoderHelpers::PG_EPOCH_UNIX, rem, :usec).getlocal
      end
    end

    class TimestampLocal < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        usec = string.unpack1("q>")
        return "infinity" if usec == CoderHelpers::INT64_MAX
        return "-infinity" if usec == CoderHelpers::INT64_MIN
        sec, rem = usec.divmod(1_000_000)
        u = Time.at(sec + CoderHelpers::PG_EPOCH_UNIX, rem, :usec).utc
        Time.new(u.year, u.mon, u.day, u.hour, u.min, u.sec + Rational(u.usec, 1_000_000))
      end
    end

    class Date < SimpleDecoder
      include Coder::BinaryFormatting
      def decode(string, _tuple = nil, _field = nil)
        require "date"
        days = string.unpack1("l>")
        return "infinity" if days == CoderHelpers::INT32_MAX
        return "-infinity" if days == CoderHelpers::INT32_MIN
        ::Date.jd(days + CoderHelpers::PG_EPOCH_JD)
      end
    end

    class Array < CompositeDecoder
      include Coder::BinaryFormatting

      def decode(string, tuple = nil, field = nil)
        b = string.b
        ndim, _has_null, _elem_oid = b.unpack("l>l>L>")
        return [] if ndim == 0
        pos = 12
        dims = []
        ndim.times do
          dim_len, _lbound = b.byteslice(pos, 8).unpack("l>l>")
          dims << dim_len
          pos += 8
        end
        read_dim = lambda do |depth|
          len = dims[depth]
          arr = ::Array.new(len)
          len.times do |i|
            if depth + 1 < dims.length
              arr[i] = read_dim.call(depth + 1)
            else
              elen = b.byteslice(pos, 4).unpack1("l>")
              pos += 4
              if elen < 0
                arr[i] = nil
              else
                raw = b.byteslice(pos, elen)
                pos += elen
                arr[i] = @elements_type ? @elements_type.decode(raw, tuple, field) : raw
              end
            end
          end
          arr
        end
        read_dim.call(0)
      end
    end

    class CopyRow < CopyDecoder
      include Coder::BinaryFormatting

      BINARY_SIG = "PGCOPY\n\377\r\n\0".b

      def decode(string, _tuple = nil, _field = nil)
        b = string.b
        pos = 0
        if b.bytesize >= 19 && b.byteslice(0, 11) == BINARY_SIG
          ext_len = b.byteslice(15, 4).unpack1("l>")
          pos = 19 + ext_len
        end
        return nil if pos + 2 > b.bytesize
        ncols = b.byteslice(pos, 2).unpack1("s>")
        pos += 2
        return nil if ncols < 0 # trailer 0xFFFF == -1
        tm = @type_map
        out = ::Array.new(ncols)
        ncols.times do |i|
          flen = b.byteslice(pos, 4).unpack1("l>")
          pos += 4
          if flen < 0
            out[i] = nil
          else
            raw = b.byteslice(pos, flen)
            pos += flen
            out[i] = tm ? tm.__decode_copy_field(raw, i, 1) : raw
          end
        end
        out
      end
    end
  end

  # ==========================================================================
  # PG::TypeMap hierarchy
  # ==========================================================================

  class TypeMap
    def initialize
      @default_type_map = nil
    end

    def default_type_map
      @default_type_map || TypeMapAllStrings.new
    end

    def default_type_map=(tm)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise TypeError, "wrong argument type #{tm.class} (expected PG::TypeMap)" unless tm.is_a?(PG::TypeMap)
      @default_type_map = tm
    end

    def with_default_type_map(tm)
      self.default_type_map = tm
      self
    end

    def fit_to_result(_result)
      raise NotImplementedError, "type map #{self.class} is not suitable to map result values"
    end

    def fit_to_query(_params)
      raise NotImplementedError, "type map #{self.class} is not suitable to map query params"
    end

    def query_param_encoders(params)
      fit_to_query(params)
      Array.new(params.length, nil)
    end

    def __decode_cell(result, row, col, raw, oid, fmt)
      if @default_type_map
        @default_type_map.__decode_cell(result, row, col, raw, oid, fmt)
      else
        raw
      end
    end

    def __decode_copy_field(raw, col, fmt)
      if @default_type_map
        @default_type_map.__decode_copy_field(raw, col, fmt)
      else
        raw
      end
    end

    def __encode_param(value, idx)
      if @default_type_map
        @default_type_map.__encode_param(value, idx)
      else
        nil
      end
    end
  end

  class TypeMapAllStrings < TypeMap
    def initialize
      @default_type_map = nil
    end

    def fit_to_result(_result)
      self
    end

    def fit_to_query(_params)
      self
    end

    def __decode_cell(_result, _row, _col, raw, _oid, _fmt)
      raw
    end

    def __decode_copy_field(raw, _col, _fmt)
      raw
    end

    def __encode_param(_value, _idx)
      nil
    end
  end

  class TypeMapByColumn < TypeMap
    def initialize(coders)
      super()
      raise TypeError, "wrong argument type #{coders.class} (expected Array)" unless coders.is_a?(::Array)
      @coders = coders.map do |c|
        if c.nil? || c.is_a?(PG::Coder)
          c
        else
          raise TypeError, "wrong argument type #{c.class} (expected PG::Coder or nil)"
        end
      end
    end

    def coders
      @coders ? @coders.dup : []
    end

    def oids
      (@coders || []).map { |c| c ? c.oid : nil }
    end

    def inspect
      "#<#{self.class} #{(@coders || []).map { |c| c ? c.inspect_short : 'nil' }.join(' ')}>"
    end

    def fit_to_result(result)
      raise NotImplementedError, "#{self.class} not initialized" unless @coders
      if result.nfields != @coders.length
        raise ArgumentError, "number of result fields (#{result.nfields}) does not match number of mapped columns (#{@coders.length})"
      end
      self
    end

    def fit_to_query(params)
      raise NotImplementedError, "#{self.class} not initialized" unless @coders
      if params.length != @coders.length
        raise ArgumentError, "number of query params (#{params.length}) does not match number of mapped columns (#{@coders.length})"
      end
      self
    end

    def query_param_encoders(params)
      fit_to_query(params)
      @coders.dup
    end

    def __decode_cell(result, row, col, raw, oid, fmt)
      raise NotImplementedError, "#{self.class} not initialized" unless @coders
      c = @coders[col]
      if c && c.respond_to?(:decode)
        c.decode(raw, row, col)
      else
        super
      end
    end

    def __decode_copy_field(raw, col, fmt)
      raise NotImplementedError, "#{self.class} not initialized" unless @coders
      c = @coders[col]
      if c && c.respond_to?(:decode)
        c.decode(raw, 0, col)
      else
        super
      end
    end

    def __encode_param(value, idx)
      raise NotImplementedError, "#{self.class} not initialized" unless @coders
      c = @coders[idx]
      if c && c.respond_to?(:encode)
        [c.encode(value), c.oid, c.format]
      else
        super
      end
    end
  end

  class TypeMapByOid < TypeMap
    def initialize
      super()
      @coders = [{}, {}] # [0]=text, [1]=binary
      @max_rows_for_online_lookup = 10
    end

    def fit_to_result(_result)
      self
    end

    attr_reader :max_rows_for_online_lookup

    def max_rows_for_online_lookup=(v)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      @max_rows_for_online_lookup = Kernel.Integer(v)
    end

    def add_coder(coder)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise TypeError, "wrong argument type #{coder.class} (expected PG::Coder)" unless coder.is_a?(PG::Coder)
      fmt = coder.format
      raise ArgumentError, "Invalid format value #{fmt.inspect}" unless fmt == 0 || fmt == 1
      @coders[fmt][coder.oid] = coder
      self
    end

    def rm_coder(format, oid)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise ArgumentError, "Invalid format value #{format.inspect}" unless format == 0 || format == 1
      @coders[format].delete(oid)
    end

    def coders
      @coders[0].values + @coders[1].values
    end

    def build_column_map(result)
      raise TypeError, "wrong argument type #{result.class} (expected PG::Result)" unless result.is_a?(PG::Result)
      cols = Array.new(result.nfields) do |i|
        fmt = result.fformat(i)
        oid = result.ftype(i)
        @coders[fmt] && @coders[fmt][oid]
      end
      tm = TypeMapByColumn.new(cols)
      tm.default_type_map = @default_type_map if @default_type_map
      tm
    end

    def __decode_cell(result, row, col, raw, oid, fmt)
      c = @coders[fmt] && @coders[fmt][oid]
      if c && c.respond_to?(:decode)
        c.decode(raw, row, col)
      else
        super
      end
    end
  end

  class TypeMapByClass < TypeMap
    def initialize
      super()
      @coders = {}
    end

    def fit_to_query(params)
      @default_type_map.fit_to_query(params) if @default_type_map
      self
    end

    def query_param_encoders(params)
      fit_to_query(params)
      def_encs = @default_type_map ? @default_type_map.query_param_encoders(params) : nil
      params.each_with_index.map do |val, idx|
        c = val.nil? ? nil : resolve_coder(val)
        c || (def_encs && def_encs[idx])
      end
    end

    def []=(klass, coder)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise TypeError, "key must be a Module or Class" unless klass.is_a?(Module)
      if coder.nil?
        @coders.delete(klass)
      elsif coder.is_a?(PG::Coder) || coder.is_a?(Symbol) || coder.respond_to?(:call)
        @coders[klass] = coder
      else
        raise TypeError, "value must be a PG::Coder, Symbol, Proc, or nil"
      end
      coder
    end

    def [](klass)
      @coders[klass]
    end

    def coders
      @coders.dup
    end

    def __encode_param(value, idx)
      c = resolve_coder(value)
      if c.is_a?(PG::Coder) && c.respond_to?(:encode)
        [c.encode(value), c.oid, c.format]
      else
        super
      end
    end

    private

    def resolve_coder(value)
      c = lookup_coder(value)
      if c.is_a?(Symbol)
        c = send(c, value)
      elsif c && !c.is_a?(PG::Coder) && c.respond_to?(:call)
        c = c.call(value)
      end
      if !c.nil? && !c.is_a?(PG::Coder)
        raise TypeError, "wrong encoder type #{c.class} (expected PG::Coder or nil)"
      end
      c
    end

    def lookup_coder(value)
      k = value.class
      return @coders[k] if @coders.key?(k)
      k.ancestors.each do |anc|
        return @coders[anc] if @coders.key?(anc)
      end
      nil
    end
  end

  class TypeMapByMriType < TypeMap
    VALID_TYPES = %w[
      T_NONE T_OBJECT T_CLASS T_MODULE T_FLOAT T_STRING T_REGEXP T_ARRAY
      T_HASH T_STRUCT T_BIGNUM T_FILE T_DATA T_MATCH T_COMPLEX T_RATIONAL
      T_NIL T_TRUE T_FALSE T_SYMBOL T_FIXNUM T_UNDEF T_IMEMO T_NODE T_ICLASS T_ZOMBIE
    ].freeze

    def initialize
      super()
      @coders = {}
    end

    def fit_to_query(params)
      @default_type_map.fit_to_query(params) if @default_type_map
      self
    end

    def query_param_encoders(params)
      fit_to_query(params)
      def_encs = @default_type_map ? @default_type_map.query_param_encoders(params) : nil
      params.each_with_index.map do |val, idx|
        c = val.nil? ? nil : resolve_coder(val)
        c || (def_encs && def_encs[idx])
      end
    end

    def []=(mri_type, coder)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      key = mri_type.to_s
      raise ArgumentError, "invalid mri_type #{mri_type.inspect}" unless VALID_TYPES.include?(key)
      if coder.nil?
        @coders.delete(key)
      elsif coder.is_a?(PG::Coder) || coder.is_a?(Symbol) || coder.respond_to?(:call)
        @coders[key] = coder
      else
        raise TypeError, "value must be a PG::Coder, Symbol, Proc, or nil"
      end
      coder
    end

    def [](mri_type)
      @coders[mri_type.to_s]
    end

    def coders
      @coders.dup
    end

    def __encode_param(value, idx)
      c = resolve_coder(value)
      if c.is_a?(PG::Coder) && c.respond_to?(:encode)
        [c.encode(value), c.oid, c.format]
      else
        super
      end
    end

    private

    def resolve_coder(value)
      key = mri_type_of(value)
      c = @coders[key]
      if c.is_a?(Symbol)
        c = send(c, value)
      elsif c && !c.is_a?(PG::Coder) && c.respond_to?(:call)
        c = c.call(value)
      end
      if !c.nil? && !c.is_a?(PG::Coder)
        raise TypeError, "wrong encoder type #{c.class} (expected PG::Coder or nil)"
      end
      c
    end

    def mri_type_of(v)
      case v
      when nil then "T_NIL"
      when true then "T_TRUE"
      when false then "T_FALSE"
      when ::Integer then "T_FIXNUM"
      when ::Float then "T_FLOAT"
      when ::String then "T_STRING"
      when ::Symbol then "T_SYMBOL"
      when ::Array then "T_ARRAY"
      when ::Hash then "T_HASH"
      when ::Regexp then "T_REGEXP"
      when ::Class then "T_CLASS"
      when ::Module then "T_MODULE"
      when ::Struct then "T_STRUCT"
      when ::File then "T_FILE"
      when ::Rational then "T_RATIONAL"
      when ::Complex then "T_COMPLEX"
      else "T_OBJECT"
      end
    end
  end

  class TypeMapInRuby < TypeMap
    def fit_to_result(_result)
      self
    end

    def fit_to_query(_params)
      self
    end

    def fit_to_copy_get
      0
    end

    def typecast_result_value(result, tuple, field)
      raw = result.__raw_value(tuple, field)
      return nil if raw.nil?
      if @default_type_map
        @default_type_map.__decode_cell(result, tuple, field, raw, result.ftype(field), result.fformat(field))
      else
        raw
      end
    end

    def typecast_query_param(param, field)
      if @default_type_map
        res = @default_type_map.__encode_param(param, field)
        res ? res[0] : param
      else
        param
      end
    end

    def typecast_copy_get(field_str, fieldno, format, _enc)
      if @default_type_map
        @default_type_map.__decode_copy_field(field_str, fieldno, format)
      else
        field_str
      end
    end

    def __decode_cell(result, row, col, _raw, _oid, _fmt)
      typecast_result_value(result, row, col)
    end

    def __decode_copy_field(raw, col, fmt)
      typecast_copy_get(raw, col, fmt, Encoding::UTF_8)
    end

    def __encode_param(value, idx)
      res = typecast_query_param(value, idx)
      if res.is_a?(PG::Coder)
        [res.encode(value), res.oid, res.format]
      elsif res.nil?
        [nil, 0, 0]
      else
        [res.to_s, 0, 0]
      end
    end
  end

  # ==========================================================================
  # PG::BasicTypeRegistry & BasicTypeMap*
  # ==========================================================================

  class BasicTypeRegistry
    class CoderMap
      DONT_QUOTE_TYPES = %w[
        int2 int4 int8
        float4 float8
        oid
        bool
        date timestamp timestamptz
      ].each_with_object({}) { |e, h| h[e] = true }.freeze
      private_constant :DONT_QUOTE_TYPES

      def initialize(result, coders_by_name, format, arraycoder)
        coder_map = {}
        arrays, nodes = result.partition { |row| row["typinput"] == "array_in" }

        nodes.each do |row|
          next unless coders_by_name.key?(row["typname"])
          coder = coders_by_name[row["typname"]].dup
          coder.oid = row["oid"].to_i
          coder.name = row["typname"]
          coder.format = format
          coder_map[coder.oid] = coder.freeze
        end

        if arraycoder
          arrays.each do |row|
            elements_coder = coder_map[row["typelem"].to_i]
            next unless elements_coder
            coder = arraycoder.new
            coder.oid = row["oid"].to_i
            coder.name = row["typname"]
            coder.format = format
            coder.elements_type = elements_coder
            coder.needs_quotation = !DONT_QUOTE_TYPES[elements_coder.name]
            coder_map[coder.oid] = coder.freeze
          end
        end

        @coders = coder_map.values.freeze
        @coders_by_name = @coders.each_with_object({}) { |t, h| h[t.name] = t }.freeze
        @coders_by_oid = @coders.each_with_object({}) { |t, h| h[t.oid] = t }.freeze
        freeze
      end

      attr_reader :coders, :coders_by_oid, :coders_by_name

      def coder_by_name(name)
        @coders_by_name[name]
      end

      def coder_by_oid(oid)
        @coders_by_oid[oid]
      end
    end

    class CoderMapsBundle
      attr_reader :typenames_by_oid

      BUILTIN_PG_TYPES = [
        { "oid" => "16", "typname" => "bool", "typelem" => "0", "typdelim" => ",", "typinput" => "boolin" },
        { "oid" => "17", "typname" => "bytea", "typelem" => "0", "typdelim" => ",", "typinput" => "byteain" },
        { "oid" => "20", "typname" => "int8", "typelem" => "0", "typdelim" => ",", "typinput" => "int8in" },
        { "oid" => "21", "typname" => "int2", "typelem" => "0", "typdelim" => ",", "typinput" => "int2in" },
        { "oid" => "23", "typname" => "int4", "typelem" => "0", "typdelim" => ",", "typinput" => "int4in" },
        { "oid" => "25", "typname" => "text", "typelem" => "0", "typdelim" => ",", "typinput" => "textin" },
        { "oid" => "26", "typname" => "oid", "typelem" => "0", "typdelim" => ",", "typinput" => "oidin" },
        { "oid" => "114", "typname" => "json", "typelem" => "0", "typdelim" => ",", "typinput" => "json_in" },
        { "oid" => "700", "typname" => "float4", "typelem" => "0", "typdelim" => ",", "typinput" => "float4in" },
        { "oid" => "701", "typname" => "float8", "typelem" => "0", "typdelim" => ",", "typinput" => "float8in" },
        { "oid" => "1043", "typname" => "varchar", "typelem" => "0", "typdelim" => ",", "typinput" => "varcharin" },
        { "oid" => "1082", "typname" => "date", "typelem" => "0", "typdelim" => ",", "typinput" => "date_in" },
        { "oid" => "1114", "typname" => "timestamp", "typelem" => "0", "typdelim" => ",", "typinput" => "timestamp_in" },
        { "oid" => "1184", "typname" => "timestamptz", "typelem" => "0", "typdelim" => ",", "typinput" => "timestamptz_in" },
        { "oid" => "1700", "typname" => "numeric", "typelem" => "0", "typdelim" => ",", "typinput" => "numeric_in" },
        { "oid" => "3802", "typname" => "jsonb", "typelem" => "0", "typdelim" => ",", "typinput" => "jsonb_in" },
      ].freeze
      private_constant :BUILTIN_PG_TYPES

      def initialize(connection, registry: nil)
        registry ||= DEFAULT_TYPE_REGISTRY
        result = connection.exec(<<-SQL).to_a
          SELECT t.oid, t.typname, t.typelem, t.typdelim, ti.proname AS typinput
          FROM pg_type as t
          JOIN pg_proc as ti ON ti.oid = t.typinput
        SQL
        result = BUILTIN_PG_TYPES if result.empty?
        init_maps(registry, result.freeze)
        freeze
      end

      def each_format(direction)
        @maps.map { |f| f[direction] }
      end

      def map_for(format, direction)
        @maps[format][direction]
      end

      private

      def init_maps(registry, result)
        @maps = [
          [0, :encoder, PG::TextEncoder::Array],
          [0, :decoder, PG::TextDecoder::Array],
          [1, :encoder, PG::BinaryEncoder::Array],
          [1, :decoder, PG::BinaryDecoder::Array],
        ].each_with_object([]) do |(format, direction, arraycoder), h|
          coders = registry.coders_for(format, direction) || {}
          h[format] ||= {}
          h[format][direction] = CoderMap.new(result, coders, format, arraycoder)
        end.each(&:freeze).freeze

        @typenames_by_oid = result.each_with_object({}) { |t, h| h[t["oid"].to_i] = t["typname"] }.freeze
      end
    end

    module Checker
      ValidFormats = { 0 => true, 1 => true }.freeze
      ValidDirections = { encoder: true, decoder: true }.freeze
      private_constant :ValidFormats, :ValidDirections

      def check_format_and_direction(format, direction)
        raise ArgumentError, "Invalid format value #{format.inspect}" unless ValidFormats[format]
        raise ArgumentError, "Invalid direction #{direction.inspect}" unless ValidDirections[direction]
      end
      protected :check_format_and_direction

      def build_coder_maps(conn_or_maps, registry: nil)
        if conn_or_maps.is_a?(PG::BasicTypeRegistry::CoderMapsBundle)
          raise ArgumentError, "registry argument must be given to CoderMapsBundle" if registry
          conn_or_maps
        else
          PG::BasicTypeRegistry::CoderMapsBundle.new(conn_or_maps, registry: registry).freeze
        end
      end
      protected :build_coder_maps
    end

    include Checker

    def initialize
      @coders_by_name = []
    end

    def coders_for(format, direction)
      check_format_and_direction(format, direction)
      @coders_by_name[format] && @coders_by_name[format][direction]
    end

    def register_coder(coder)
      h = @coders_by_name[coder.format] ||= { encoder: {}, decoder: {} }
      name = coder.name || raise(ArgumentError, "name of #{coder.inspect} must be defined")
      h[:encoder][name] = coder if coder.respond_to?(:encode)
      h[:decoder][name] = coder if coder.respond_to?(:decode)
      self
    end

    def register_type(format, name, encoder_class, decoder_class)
      register_coder(encoder_class.new(name: name, format: format).freeze) if encoder_class
      register_coder(decoder_class.new(name: name, format: format).freeze) if decoder_class
      self
    end

    def alias_type(format, new_name, old_name)
      [:encoder, :decoder].each do |dir|
        enc = @coders_by_name[format][dir][old_name]
        if enc
          @coders_by_name[format][dir][new_name] = enc
        else
          @coders_by_name[format][dir].delete(new_name)
        end
      end
      self
    end

    def register_default_types
      register_type 0, "int2", PG::TextEncoder::Integer, PG::TextDecoder::Integer
      alias_type    0, "int4", "int2"
      alias_type    0, "int8", "int2"
      alias_type    0, "oid",  "int2"

      begin
        PG.require_bigdecimal_without_warning
        register_type 0, "numeric", PG::TextEncoder::Numeric, PG::TextDecoder::Numeric
      rescue LoadError
      end
      register_type 0, "text", PG::TextEncoder::String, PG::TextDecoder::String
      alias_type 0, "varchar", "text"
      alias_type 0, "char", "text"
      alias_type 0, "bpchar", "text"
      alias_type 0, "xml", "text"
      alias_type 0, "name", "text"

      register_type 0, "bytea", PG::TextEncoder::Bytea, PG::TextDecoder::Bytea
      register_type 0, "bool", PG::TextEncoder::Boolean, PG::TextDecoder::Boolean
      register_type 0, "float4", PG::TextEncoder::Float, PG::TextDecoder::Float
      alias_type 0, "float8", "float4"

      register_type 0, "timestamp", PG::TextEncoder::TimestampWithoutTimeZone, PG::TextDecoder::TimestampWithoutTimeZone
      register_type 0, "timestamptz", PG::TextEncoder::TimestampWithTimeZone, PG::TextDecoder::TimestampWithTimeZone
      register_type 0, "date", PG::TextEncoder::Date, PG::TextDecoder::Date
      register_type 0, "json", PG::TextEncoder::JSON, PG::TextDecoder::JSON
      alias_type    0, "jsonb", "json"
      register_type 0, "inet", PG::TextEncoder::Inet, PG::TextDecoder::Inet
      alias_type    0, "cidr", "inet"
      register_type 0, "record", PG::TextEncoder::Record, PG::TextDecoder::Record

      register_type 1, "int2", PG::BinaryEncoder::Int2, PG::BinaryDecoder::Integer
      register_type 1, "int4", PG::BinaryEncoder::Int4, PG::BinaryDecoder::Integer
      register_type 1, "int8", PG::BinaryEncoder::Int8, PG::BinaryDecoder::Integer
      alias_type    1, "oid",  "int2"

      register_type 1, "text", PG::BinaryEncoder::String, PG::BinaryDecoder::String
      alias_type 1, "varchar", "text"
      alias_type 1, "char", "text"
      alias_type 1, "bpchar", "text"
      alias_type 1, "xml", "text"
      alias_type 1, "name", "text"

      register_type 1, "bytea", PG::BinaryEncoder::Bytea, PG::BinaryDecoder::Bytea
      register_type 1, "bool", PG::BinaryEncoder::Boolean, PG::BinaryDecoder::Boolean
      register_type 1, "float4", PG::BinaryEncoder::Float4, PG::BinaryDecoder::Float
      register_type 1, "float8", PG::BinaryEncoder::Float8, PG::BinaryDecoder::Float
      register_type 1, "timestamp", PG::BinaryEncoder::TimestampUtc, PG::BinaryDecoder::TimestampUtc
      register_type 1, "timestamptz", PG::BinaryEncoder::TimestampUtc, PG::BinaryDecoder::TimestampUtcToLocal
      register_type 1, "date", PG::BinaryEncoder::Date, PG::BinaryDecoder::Date

      self
    end
    alias define_default_types register_default_types

    DEFAULT_TYPE_REGISTRY = PG.make_shareable(PG::BasicTypeRegistry.new.register_default_types)
    private_constant :DEFAULT_TYPE_REGISTRY
  end

  class BasicTypeMapForResults < TypeMapByOid
    include BasicTypeRegistry::Checker

    class WarningTypeMap < TypeMapInRuby
      def initialize(typenames)
        super()
        @already_warned = {}
        @typenames_by_oid = typenames
      end

      def typecast_result_value(result, _tuple, field)
        format = result.fformat(field)
        oid = result.ftype(field)
        unless @already_warned[format] && @already_warned[format][oid]
          warn "Warning: no type cast defined for type #{@typenames_by_oid[oid].inspect} format #{format} with oid #{oid}. Please cast this type explicitly to TEXT to be safe for future changes."
          unless frozen?
            @already_warned[format] ||= {}
            @already_warned[format][oid] = true
          end
        end
        super
      end
    end

    def initialize(connection_or_coder_maps, registry: nil)
      super()
      @coder_maps = build_coder_maps(connection_or_coder_maps, registry: registry)
      @coder_maps.each_format(:decoder).flat_map(&:coders).each do |coder|
        add_coder(coder)
      end
      self.default_type_map = WarningTypeMap.new(@coder_maps.typenames_by_oid)
    end

    def coder_maps_bundle
      @coder_maps
    end
  end

  class BasicTypeMapBasedOnResult < TypeMapByOid
    include BasicTypeRegistry::Checker

    def initialize(connection_or_coder_maps, registry: nil)
      super()
      @coder_maps = build_coder_maps(connection_or_coder_maps, registry: registry)
      @coder_maps.each_format(:encoder).flat_map(&:coders).each do |coder|
        add_coder(coder)
      end
    end

    def coder_maps_bundle
      @coder_maps
    end
  end

  class BasicTypeMapForQueries < TypeMapByClass
    class BinaryData < String
    end

    class UndefinedEncoder < RuntimeError
    end

    include BasicTypeRegistry::Checker

    class UndefinedDefault
      def self.call(oid_name, format)
        raise UndefinedEncoder, "no encoder defined for type #{oid_name.inspect} format #{format}"
      end
    end
    private_constant :UndefinedDefault

    def initialize(connection_or_coder_maps, registry: nil, if_undefined: nil)
      super()
      @coder_maps = build_coder_maps(connection_or_coder_maps, registry: registry)
      @array_encoders_by_klass = build_array_encoders_by_klass
      @encode_array_as = :array
      @if_undefined = if_undefined || UndefinedDefault
      init_encoders
    end

    def coder_maps_bundle
      @coder_maps
    end

    attr_reader :encode_array_as

    def encode_array_as=(pg_type)
      case pg_type
      when :array, :json, :record, /\A_/
      else
        raise ArgumentError, "invalid pg_type #{pg_type.inspect}"
      end
      @encode_array_as = pg_type
      init_encoders
    end

    private

    def init_encoders
      coders.each { |kl, _| self[kl] = nil }
      populate_encoder_list
      @textarray_encoder = coder_by_name(0, :encoder, "_text")
    end

    def coder_by_name(format, direction, name)
      check_format_and_direction(format, direction)
      @coder_maps.map_for(format, direction).coder_by_name(name)
    end

    def undefined(name, format)
      @if_undefined.call(name, format)
    end

    def populate_encoder_list
      default_type_map_spec.each do |klass, selector|
        if selector.is_a?(::Array)
          format, name, oid_name = selector
          base = coder_by_name(format, :encoder, name)
          if base
            coder = base.dup
            if oid_name
              oid_coder = coder_by_name(format, :encoder, oid_name)
              if oid_coder
                coder.oid = oid_coder.oid
              else
                undefined(oid_name, format)
              end
            else
              coder.oid = 0
            end
            self[klass] = coder
          else
            undefined(name, format)
          end
        else
          case @encode_array_as
          when :array
            self[klass] = selector
          when :json
            self[klass] = PG::TextEncoder::JSON.new
          when :record
            self[klass] = PG::TextEncoder::Record.new(type_map: self)
          when /\A_/
            coder = coder_by_name(0, :encoder, @encode_array_as)
            if coder
              self[klass] = coder
            else
              undefined(@encode_array_as, 0)
            end
          else
            raise ArgumentError, "invalid pg_type #{@encode_array_as.inspect}"
          end
        end
      end
    end

    def build_array_encoders_by_klass
      h = {}
      default_array_type_map_spec.each do |klass, (format, name)|
        h[klass] = coder_by_name(format, :encoder, name)
      end
      h
    end

    def get_array_type(value)
      elem = value
      while elem.is_a?(::Array)
        elem = elem.first
      end
      @array_encoders_by_klass[elem.class] ||
        elem.class.ancestors.each { |anc| break @array_encoders_by_klass[anc] if @array_encoders_by_klass[anc] }.then { |v| v.is_a?(PG::Coder) ? v : nil } ||
        @textarray_encoder
    end

    def default_type_map_spec
      require "date"
      begin
        require "ipaddr"
      rescue LoadError
      end
      m = {
        TrueClass => [0, "bool", "bool"],
        FalseClass => [0, "bool", "bool"],
        ::Integer => [0, "int8"],
        ::Float => [0, "float8"],
        ::Time => [0, "timestamptz"],
        ::Date => [0, "date"],
        ::Hash => [0, "json"],
        ::Array => :get_array_type,
        BinaryData => [1, "bytea"],
      }
      m[::IPAddr] = [0, "inet"] if defined?(::IPAddr)
      begin
        PG.require_bigdecimal_without_warning
        m[::BigDecimal] = [0, "numeric"] if defined?(::BigDecimal)
      rescue LoadError
      end
      m
    end

    def default_array_type_map_spec
      require "date"
      m = {
        TrueClass => [0, "_bool"],
        FalseClass => [0, "_bool"],
        ::Integer => [0, "_int8"],
        ::String => [0, "_text"],
        ::Float => [0, "_float8"],
        ::Time => [0, "_timestamptz"],
        ::Date => [0, "_date"],
        BinaryData => [0, "_bytea"],
      }
      begin
        require "ipaddr"
        m[::IPAddr] = [0, "_inet"] if defined?(::IPAddr)
      rescue LoadError
      end
      begin
        PG.require_bigdecimal_without_warning
        m[::BigDecimal] = [0, "_numeric"] if defined?(::BigDecimal)
      rescue LoadError
      end
      m
    end
  end
end
