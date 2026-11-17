# Kernel#format / sprintf / printf and String#% following MRI's sprintf.c
# (rb_str_format): flags, width, precision, n$ / * / %<name> / %{name}
# references, implicit conversions, two's complement for negative bBoxX and
# result-encoding negotiation. Float digits come from String.__format_float
# (format.go); sign, width and padding are applied here.
class String
  FMT_SPACE = 1
  FMT_SHARP = 2
  FMT_MINUS = 4
  FMT_PLUS = 8
  FMT_ZERO = 16
  FMT_WIDTH = 32
  FMT_PREC = 64
  FMT_PREC0 = 128
  private_constant :FMT_SPACE, :FMT_SHARP, :FMT_MINUS, :FMT_PLUS, :FMT_ZERO, :FMT_WIDTH, :FMT_PREC, :FMT_PREC0

  class << self
    def __format(fmt, args)
      fmt = __format_str(fmt)
      st = FormatState.new(fmt, args)
      st.run
    end

    def __format_str(fmt)
      Kernel.__implicit(fmt, String)
    end
  end

  class FormatState # :nodoc:
    def initialize(fmt, args)
      @fmt = fmt
      @args = args
      @enc = fmt.encoding
      @out = "".b
      @posarg = 0 # >0: last unnumbered index, -1: numbered used, -2: named used
      @nextarg = 1
      @hash = nil
    end

    def run
      fmt = @fmt
      n = fmt.bytesize
      i = 0
      while i < n
        j = i
        j += 1 while j < n && fmt.getbyte(j) != 37 # '%'
        @out << fmt.byteslice(i, j - i) if j > i
        break if j >= n
        i = spec(j + 1)
      end
      if @posarg >= 0 && @nextarg < @args.size + 1 && !(@args.size == 1 && Hash === @args[0])
        raise ArgumentError, "too many arguments for format string" if $DEBUG
        warn "too many arguments for format string" if $VERBOSE
      end
      @out.force_encoding(@enc)
    end

    private

    def arg_error(msg)
      raise ArgumentError, msg
    end

    def next_arg
      arg_error("unnumbered(#{@nextarg}) mixed with numbered") if @posarg == -1
      arg_error("unnumbered(#{@nextarg}) mixed with named") if @posarg == -2
      @posarg = @nextarg
      @nextarg += 1
      arg_error("too few arguments") if @posarg > @args.size
      @args[@posarg - 1]
    end

    def pos_arg(n)
      arg_error("numbered(#{n}) after unnumbered(#{@posarg})") if @posarg > 0
      arg_error("numbered(#{n}) after named") if @posarg == -2
      arg_error("invalid index - #{n}$") if n < 1
      @posarg = -1
      arg_error("too few arguments") if n > @args.size
      @args[n - 1]
    end

    def get_hash
      return @hash if @hash
      arg_error("one hash required") unless @args.size == 1
      h = @args[0]
      h = Kernel.__check_implicit(h, Hash)
      arg_error("one hash required") unless h
      @hash = h
    end

    def num_arg(v)
      Kernel.__to_int(v)
    end

    def check_flags(flags, what = nil)
      arg_error("flag after width") if flags & FMT_WIDTH != 0
      arg_error("flag after precision") if flags & FMT_PREC0 != 0
    end

    def read_num(i, what = "width")
      fmt = @fmt
      v = 0
      while (c = fmt.getbyte(i)) && c >= 48 && c <= 57
        v = v * 10 + (c - 48)
        arg_error("#{what} too big") if v > 0x7fffffff
        i += 1
      end
      [v, i]
    end

    def malformed(c)
      if c.nil?
        arg_error("incomplete format specifier; use %% (double %) instead")
      elsif c >= 0x20 && c < 0x7f
        arg_error("malformed format string - %#{c.chr}")
      else
        arg_error("malformed format string")
      end
    end

    # parses one conversion starting after '%', returns the index after it
    def spec(i)
      fmt = @fmt
      flags = 0
      width = 0
      prec = -1
      value = nil
      have_value = false
      name = nil
      start = i
      malformed(nil) if i >= fmt.bytesize
      loop do
        c = fmt.getbyte(i)
        if c.nil?
          arg_error("malformed format string") if i > start
          malformed(nil)
        end
        case c
        when 32 # ' '
          check_flags(flags); flags |= FMT_SPACE; i += 1
        when 35 # '#'
          check_flags(flags); flags |= FMT_SHARP; i += 1
        when 43 # '+'
          check_flags(flags); flags |= FMT_PLUS; i += 1
        when 45 # '-'
          check_flags(flags); flags |= FMT_MINUS; i += 1
        when 48 # '0'
          check_flags(flags); flags |= FMT_ZERO; i += 1
        when 49..57 # 1-9
          v, i = read_num(i)
          if fmt.getbyte(i) == 36 # '$'
            arg_error("value given twice - #{v}$") if have_value
            value = pos_arg(v)
            have_value = true
            i += 1
          else
            arg_error("width given twice") if flags & FMT_WIDTH != 0
            arg_error("width after precision") if flags & FMT_PREC0 != 0
            width = v
            flags |= FMT_WIDTH
          end
        when 60, 123 # '<' '{'
          term = c == 60 ? 62 : 125
          j = i + 1
          j += 1 while j < fmt.bytesize && fmt.getbyte(j) != term
          arg_error("malformed name - unmatched parenthesis") if j >= fmt.bytesize
          label = fmt.byteslice(i, j - i + 1)
          arg_error("named#{label} after <#{name}>") if name
          arg_error("named#{label} after unnumbered(#{@posarg})") if @posarg > 0
          arg_error("named#{label} after numbered") if @posarg == -1
          @posarg = -2
          name = fmt.byteslice(i + 1, j - i - 1)
          key = name.to_sym
          h = get_hash
          v = h.key?(key) ? h[key] : h.default(key)
          if v.nil? && !h.key?(key)
            raise KeyError.new("key#{label} not found", receiver: h, key: key)
          end
          i = j + 1
          if term == 125
            return format_s(v.to_s, flags, width, prec, i)
          end
          value = v
          have_value = true
        when 42 # '*'
          arg_error("width given twice") if flags & FMT_WIDTH != 0
          arg_error("width after precision") if flags & FMT_PREC0 != 0
          v, j = read_num(i + 1)
          if fmt.getbyte(j) == 36
            w = pos_arg(v)
            i = j + 1
          else
            w = next_arg
            i += 1
          end
          width = num_arg(w)
          if width < 0
            flags |= FMT_MINUS
            width = -width
          end
          flags |= FMT_WIDTH
        when 46 # '.'
          arg_error("precision given twice") if flags & FMT_PREC0 != 0
          flags |= FMT_PREC | FMT_PREC0
          i += 1
          if fmt.getbyte(i) == 42 # '*'
            v, j = read_num(i + 1)
            if fmt.getbyte(j) == 36
              pv = pos_arg(v)
              i = j + 1
            else
              pv = next_arg
              i += 1
            end
            prec = num_arg(pv)
            flags &= ~FMT_PREC if prec < 0
          else
            prec, i = read_num(i, "precision")
          end
        when 37 # '%'
          arg_error("invalid format character - %") if flags != 0 || have_value
          @out << "%"
          return i + 1
        else
          conv = c.chr
          v = have_value ? value : nil
          getv = -> { have_value ? value : next_arg }
          case conv
          when "c"
            format_c(getv.call, flags, width)
          when "s", "p"
            a = getv.call
            str = conv == "p" ? a.inspect : __as_string(a)
            format_s(str, flags, width, prec, i)
          when "d", "i", "u", "x", "X", "o", "b", "B"
            format_int(conv, getv.call, flags, width, prec)
          when "f"
            a = getv.call
            if Integer === a || (defined?(Rational) && Rational === a)
              format_exact(a, flags, width, prec)
            else
              format_float(conv, a, flags, width, prec)
            end
          when "e", "E", "g", "G", "a", "A"
            format_float(conv, getv.call, flags, width, prec)
          else
            malformed(c)
          end
          return i + 1
        end
      end
    end

    def __as_string(a)
      s = a.to_s
      String === s ? s : Kernel.instance_method(:to_s).bind_call(a)
    end

    def merge_enc(str)
      e = str.encoding
      return if e == @enc
      cur = @out.dup.force_encoding(@enc)
      ce = Encoding.compatible?(cur, str)
      unless ce
        raise Encoding::CompatibilityError, "incompatible character encodings: #{@enc} and #{e}"
      end
      @enc = ce
    end

    def pad(body, flags, width, len = body.length)
      if width > len
        fill = " " * (width - len)
        flags & FMT_MINUS != 0 ? body + fill : fill + body
      else
        body
      end
    end

    def format_s(str, flags, width, prec, i)
      merge_enc(str)
      if flags & FMT_PREC != 0 && prec < str.length
        str = str[0, prec]
      end
      @out << pad(str, flags, width).b
      i
    end

    def format_c(a, flags, width)
      s = Integer === a ? nil : Kernel.__check_implicit(a, String)
      if s
        ch = s[0] || ""
      else
        code = num_arg(a)
        ch = __chr(code)
      end
      merge_enc(ch)
      @out << pad(ch, flags, width).b
    end

    def __chr(code)
      raise RangeError, "#{code} out of char range" if code < 0
      enc = @enc
      if enc == Encoding::UTF_8 || enc == Encoding::US_ASCII && code < 0x80
        raise RangeError, "invalid character" if enc == Encoding::UTF_8 && (code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff))
        return [code].pack("U").force_encoding(enc)
      end
      if code < 0x100 && (enc == Encoding::BINARY || !enc.ascii_compatible? || code < 0x80 || __single_byte?(enc))
        return code.chr.force_encoding(enc)
      end
      if enc == Encoding::US_ASCII || __single_byte?(enc)
        raise RangeError, "#{code} out of char range"
      end
      bytes = []
      c = code
      while c > 0
        bytes.unshift(c & 0xff)
        c >>= 8
      end
      s = bytes.pack("C*").force_encoding(enc)
      raise RangeError, "invalid character" unless s.valid_encoding?
      s
    end

    def __single_byte?(enc)
      n = enc.name
      n.start_with?("ISO-8859", "Windows-125", "KOI8", "IBM", "CP85", "mac", "TIS", "Windows-874") || enc == Encoding::BINARY
    end

    def format_int(conv, a, flags, width, prec)
      v = case a
          when Integer then a
          when Float
            raise FloatDomainError, a.to_s unless a.finite?
            a.to_i
          when String then Integer(a)
          when nil then raise TypeError, "can't convert nil into Integer"
          else
            r = Kernel.__check_implicit(a, Integer)
            r = Kernel.__check_implicit(a, Integer, :to_i) if r.nil?
            raise TypeError, "can't convert #{Kernel.__cname(a)} into Integer" if r.nil?
            r
          end
      base = case conv
             when "x", "X" then 16
             when "o" then 8
             when "b", "B" then 2
             else 10
             end
      sign = base == 10 || flags & (FMT_PLUS | FMT_SPACE) != 0
      sc = nil
      dots = false
      if sign
        s = v.abs.to_s(base)
        if v < 0
          sc = "-"
        elsif flags & FMT_PLUS != 0
          sc = "+"
        elsif flags & FMT_SPACE != 0
          sc = " "
        end
      elsif v < 0
        dots = true
        digit = (base - 1).to_s(base)
        k = v.abs.to_s(base).size + 1
        s = (v + base**k).to_s(base)
        s = s.sub(/\A#{digit}+/, "")
        s = digit + s
      else
        s = v.to_s(base)
      end
      s = s.upcase if conv == "X"
      prefix = nil
      if flags & FMT_SHARP != 0
        prefix = case conv
                 when "x" then "0x"
                 when "X" then "0X"
                 when "o" then "0"
                 when "b" then "0b"
                 when "B" then "0B"
                 end
      end
      len = s.size
      if dots
        prec -= 2
        width -= 2
      end
      if prefix == "0"
        if dots
          prefix = nil
        elsif len == 1 && s == "0"
          len = 0
          prec -= 1 if flags & FMT_PREC != 0
        elsif flags & FMT_PREC != 0 && prec > len
          prefix = nil
        end
      elsif prefix && len == 1 && s == "0"
        prefix = nil
      end
      width -= prefix.size if prefix
      width -= 1 if sc
      if flags & (FMT_ZERO | FMT_MINUS | FMT_PREC) == FMT_ZERO
        prec = width
        width = 0
      else
        if prec < len
          len = 0 if !prefix && prec == 0 && len == 1 && s == "0"
          prec = len
        end
        width -= prec
      end
      r = +""
      r << " " * width if flags & FMT_MINUS == 0 && width > 0
      r << sc if sc
      r << prefix if prefix
      r << ".." if dots
      if prec > len
        if dots
          fill = (base - 1).to_s(base)
          fill = fill.upcase if conv == "X"
          r << fill * (prec - len)
        elsif flags & (FMT_MINUS | FMT_PREC) != FMT_MINUS
          r << "0" * (prec - len)
        end
      end
      r << s[0, len]
      r << " " * width if flags & FMT_MINUS != 0 && width > 0
      @out << r
    end

    def sign_char(neg, flags)
      if neg then "-"
      elsif flags & FMT_PLUS != 0 then "+"
      elsif flags & FMT_SPACE != 0 then " "
      end
    end

    def pad_num(sc, prefix, body, flags, width)
      len = (sc ? 1 : 0) + prefix.size + body.size
      if width <= len
        "#{sc}#{prefix}#{body}"
      elsif flags & FMT_MINUS != 0
        "#{sc}#{prefix}#{body}" + " " * (width - len)
      elsif flags & FMT_ZERO != 0
        "#{sc}#{prefix}" + "0" * (width - len) + body
      else
        " " * (width - len) + "#{sc}#{prefix}#{body}"
      end
    end

    def format_exact(a, flags, width, prec)
      prec = 6 if flags & FMT_PREC == 0
      if Integer === a
        num = a
        den = 1
      else
        num = a.numerator
        den = a.denominator
      end
      neg = num < 0
      num = -num if neg
      if den != 1
        num *= 10**prec
        num += den / 2
        num /= den
      elsif prec > 0
        num *= 10**prec
      end
      digits = num.to_s
      if prec > 0
        digits = "0" * (prec + 1 - digits.size) + digits if digits.size <= prec
        body = digits[0, digits.size - prec] + "." + digits[digits.size - prec, prec]
      else
        body = digits
        body += "." if flags & FMT_SHARP != 0
      end
      @out << pad_num(sign_char(neg, flags), "", body, flags, width)
    end

    def format_float(conv, a, flags, width, prec)
      f = Float === a ? a : Float(a)
      if f.nan? || f.infinite?
        body = f.nan? ? "NaN" : "Inf"
        sc = sign_char(!f.nan? && f < 0, flags)
        @out << pad_num(sc, "", body, flags & ~FMT_ZERO, width)
        return
      end
      neg = f < 0 || (f == 0 && 1.0 / f < 0)
      body = String.__format_float(conv, flags & FMT_PREC != 0 ? prec : -1, flags & FMT_SHARP != 0, f)
      prefix = ""
      if conv == "a" || conv == "A"
        prefix = body[0, 2]
        body = body[2..]
      end
      @out << pad_num(sign_char(neg, flags), prefix, body, flags, width)
    end
  end
  private_constant :FormatState

  def %(args)
    a = Kernel.__check_implicit(args, Array) || [args]
    String.__format(self, a)
  end
end

class IO
  def printf(fmt, *args)
    write(String.__format(fmt, args))
    nil
  end
end

module Kernel
  def sprintf(fmt, *args)
    String.__format(fmt, args)
  end

  alias format sprintf

  def printf(*args)
    return nil if args.empty?
    if String === args[0]
      $stdout.write(String.__format(args[0], args[1..]))
    else
      io = args.shift
      io.write(String.__format(args.shift, args))
    end
    nil
  end
  module_function :format, :sprintf, :printf
end
