# String#to_r / #to_c and Kernel#Rational / #Complex conversions as in MRI
# (rational.c read_rat, complex.c read_comp): lenient for String#to_*,
# strict (whole string) for the Kernel functions, exception: false support.
class String
  NUM_RE = '(?:\d+(?:_\d+)*)'
  RAT_RE = "(?:[-+]?(?:#{NUM_RE}(?:\\.#{NUM_RE})?|\\.#{NUM_RE})(?:[eE][-+]?#{NUM_RE})?(?:/#{NUM_RE})?)"
  UNUM_RE = "(?:#{NUM_RE}?(?:\\.#{NUM_RE})?(?:[eE][-+]?#{NUM_RE})?(?:/#{NUM_RE})?)"
  RAT_PREFIX = Regexp.new("\\A\\s*#{RAT_RE}")
  COMP_POLAR = Regexp.new("\\A\\s*(#{RAT_RE})@(#{RAT_RE})")
  COMP_RECT = Regexp.new("\\A\\s*(#{RAT_RE})?(?:([-+])(#{UNUM_RE})?[ijIJ])")
  COMP_IMAG = Regexp.new("\\A\\s*([-+]?#{UNUM_RE}?)[ijIJ]")
  COMP_REAL = Regexp.new("\\A\\s*(#{RAT_RE})")
  private_constant :NUM_RE, :RAT_RE, :UNUM_RE, :RAT_PREFIX, :COMP_POLAR, :COMP_RECT, :COMP_IMAG, :COMP_REAL

  # numeric text (already matched) to Integer, Float or Rational
  def self.__num(t, exact)
    t = t.delete("_")
    if t.include?("/")
      n, d = t.split("/", 2)
      r = __num(n, true)
      d = Integer(d, 10)
      raise ZeroDivisionError, "divided by 0" if d == 0
      return r.is_a?(Rational) ? r / d : __oruby_Rational(r, d)
    end
    if t.include?(".") || t.match?(/[eE]/)
      return Float(t) unless exact
      m = /\A([-+])?(\d*)(?:\.(\d+))?(?:[eE]([-+]?\d+))?\z/.__match_raw(t)
      frac = m[3] || ""
      r = __oruby_Rational((m[2] + frac).to_i, 10**frac.size)
      e = m[4].to_i
      r *= __oruby_Rational(10)**e if e != 0
      r = -r if m[1] == "-"
      return r
    end
    Integer(t, 10)
  end

  def to_r
    m = RAT_PREFIX.__match_raw(self)
    return __oruby_Rational(0, 1) unless m
    r = String.__num(m[0].strip, true)
    Integer === r ? __oruby_Rational(r, 1) : r
  end

  def to_c
    String.__to_c(self, false) || __oruby_Complex(0, 0)
  end

  # returns nil when strict and the whole string isn't a complex number
  def self.__to_c(s, strict)
    s = s.b.force_encoding("UTF-8") unless s.valid_encoding?
    if (m = COMP_POLAR.__match_raw(s))
      c = Complex.polar(__num(m[1], false), __num(m[2], false))
    elsif (m = COMP_RECT.__match_raw(s))
      re = m[1] ? __num(m[1], false) : 0
      im = m[3] && !m[3].empty? ? __num(m[3], false) : 1
      im = -im if m[2] == "-"
      c = __oruby_Complex(re, im)
    elsif (m = COMP_IMAG.__match_raw(s))
      t = m[1]
      im = t.empty? || t == "+" ? 1 : t == "-" ? -1 : __num(t, false)
      c = __oruby_Complex(0, im)
    elsif (m = COMP_REAL.__match_raw(s))
      c = __oruby_Complex(__num(m[1], false), 0)
    else
      return nil
    end
    return nil if strict && !m.post_match.strip.empty?
    c
  end
end

class Complex
  I = Complex(0, 1) unless const_defined?(:I, false)
end

# Numeric#real? (true) and Complex#real? (false), as MRI
class Numeric
  def real? = true unless method_defined?(:real?)
end

class Complex
  def real? = false unless method_defined?(:real?, false)
end

module Kernel
  alias __oruby_Rational Rational
  alias __oruby_Complex Complex
  private :__oruby_Rational, :__oruby_Complex

  alias __oruby_mrb_Integer Integer
  private :__oruby_mrb_Integer

  # MRI's rb_convert_to_integer: a String is parsed (in base), a Float
  # truncated; anything else by #to_int (an Integer result), #to_str, then
  # #to_i; exception: false gives nil instead of raising
  def Integer(arg, base = nil, exception: true)
    unless true.equal?(exception) || false.equal?(exception)
      raise ArgumentError, "expected true or false as exception: #{exception.inspect}"
    end
    base = Kernel.__to_int(base) unless base.nil?
    base = nil if base == 0
    if base
      s = String === arg ? arg : (Kernel.__responds?(arg, :to_str) ? arg.to_str : nil)
      unless String === s
        return nil unless exception
        raise ArgumentError, "base specified for non string value"
      end
      arg = s
    end
    case arg
    when Integer then return arg
    when Float
      return nil if !exception && (arg.nan? || arg.infinite?)
      return arg.to_i
    when String then return Kernel.__oruby_str_to_i(arg, base, exception)
    when nil
      return nil unless exception
      raise TypeError, "can't convert nil into Integer"
    end
    if Kernel.__responds?(arg, :to_int)
      t = begin
        arg.to_int
      rescue StandardError
        nil
      end
      return t if Integer === t
    end
    if Kernel.__responds?(arg, :to_str)
      s = arg.to_str
      return Kernel.__oruby_str_to_i(s, base, exception) if String === s
    end
    unless exception
      return begin
        r = Kernel.__responds?(arg, :to_i) ? arg.to_i : nil
        Integer === r ? r : nil
      rescue StandardError
        nil
      end
    end
    c = Kernel.__cname(arg)
    raise TypeError, "can't convert #{c} into Integer" unless Kernel.__responds?(arg, :to_i)
    r = arg.to_i
    raise TypeError, "can't convert #{c} into Integer (#{c}#to_i gives #{Kernel.__cname(r)})" unless Integer === r
    r
  end

  # MRI's rb_convert_to_float: Strings parsed strictly (hex floats too),
  # numbers converted, nil/true/false rejected, else #to_f (a Float)
  def Float(arg, exception: true)
    unless true.equal?(exception) || false.equal?(exception)
      raise ArgumentError, "expected true or false as exception: #{exception.inspect}"
    end
    case arg
    when Float then return arg
    when Integer, Rational then return arg.to_f
    when String then return Kernel.__oruby_str_to_f(arg, exception)
    when nil, true, false
      return nil unless exception
      raise TypeError, "can't convert #{arg.inspect} into Float"
    end
    begin
      c = Kernel.__cname(arg)
      raise TypeError, "can't convert #{c} into Float" unless Kernel.__responds?(arg, :to_f)
      r = arg.to_f
      raise TypeError, "can't convert #{c} to Float (#{c}#to_f gives #{Kernel.__cname(r)})" unless Float === r
      r
    rescue StandardError
      raise if exception
      nil
    end
  end
  module_function :Integer, :Float

  def self.__oruby_str_to_i(s, base, exception)
    base ? __oruby_mrb_Integer(s, base) : __oruby_mrb_Integer(s)
  rescue ArgumentError => e
    return nil unless exception
    raise if e.message.include?("null byte") || e.message.include?("radix")
    raise ArgumentError, "invalid value for Integer(): #{s.inspect}"
  end

  def Rational(a, b = (no_b = true; nil), exception: true)
    unless exception == true || exception == false
      raise ArgumentError, "expected true or false as exception: #{exception.inspect}"
    end
    begin
      ra = !no_b && Complex === a && !Kernel.__exact_zero?(a.imaginary) ? a : Kernel.__to_rational(a)
      return ra if no_b
      rb = Complex === b && !Kernel.__exact_zero?(b.imaginary) ? b : Kernel.__to_rational(b)
      raise ZeroDivisionError, "divided by 0" if rb == 0
      return ra / rb if Complex === ra || Complex === rb
      r = Rational === ra || Rational === rb || Float === ra || Float === rb ? ra.to_r / rb.to_r : __oruby_Rational(ra, rb)
      Rational === r ? r : __oruby_Rational(r, 1)
    rescue StandardError
      raise if exception
      nil
    end
  end

  # MRI's nucomp_convert: Strings parsed strictly; a Complex with an exact
  # zero imaginary part counts as its real part; non-real Numerics give
  # a + b*i; the rest goes to Complex.new (TypeError "not a real" even with
  # exception: false)
  def Complex(a, b = (no_b = true; nil), exception: true)
    unless exception == true || exception == false
      raise ArgumentError, "expected true or false as exception: #{exception.inspect}"
    end
    if a.nil? || (!no_b && b.nil?)
      return nil unless exception
      raise TypeError, "can't convert nil into Complex"
    end
    if String === a
      a = Kernel.__str_to_c_strict(a, exception)
      return nil if a.nil?
    end
    if !no_b && String === b
      b = Kernel.__str_to_c_strict(b, exception)
      return nil if b.nil?
    end
    a = a.real if Complex === a && Kernel.__exact_zero?(a.imaginary)
    b = b.real if !no_b && Complex === b && Kernel.__exact_zero?(b.imaginary)
    return a if Complex === a && (no_b || Kernel.__exact_zero?(b))
    if no_b
      return a if Numeric === a && !a.real?
      unless Numeric === a
        return a.to_c if exception
        return begin
          a.to_c
        rescue StandardError
          nil
        end
      end
    elsif Numeric === a && Numeric === b && (!a.real? || !b.real?)
      return a + b * Complex::I
    end
    # a non-Numeric imaginary part is nil with exception: false (a
    # non-Numeric real part is a TypeError either way)
    return nil if !exception && !no_b && !(Numeric === b)
    im = no_b ? 0 : b
    [a, im].each do |x|
      raise TypeError, "not a real" unless Numeric === x && x.real?
    end
    __oruby_Complex(a, im)
  end
  module_function :Rational, :Complex

  class << self
    def __to_rational(v)
      case v
      when nil then raise TypeError, "can't convert nil into Rational"
      when Rational then v
      when Integer then __oruby_Rational(v, 1)
      when Float then v.to_r
      when String
        s = v.strip
        m = /\A[-+]?(?:\d+(?:_\d+)*(?:\.\d+(?:_\d+)*)?|\.\d+(?:_\d+)*)(?:[eE][-+]?\d+)?(?:\/\d+(?:_\d+)*)?\z/.__match_raw(s)
        raise ArgumentError, "invalid value for convert(): #{v.inspect}" unless m
        r = String.__num(s, true)
        Integer === r ? __oruby_Rational(r, 1) : r
      when Complex
        raise RangeError, "can't convert #{v} into Rational" unless v.imaginary.is_a?(Integer) && v.imaginary == 0 || v.imaginary.is_a?(Rational) && v.imaginary == 0
        __to_rational(v.real)
      else
        if __responds?(v, :to_r)
          r = v.to_r
          raise TypeError, "can't convert #{__cname(v)} into Rational (#{__cname(v)}#to_r gives #{__cname(r)})" unless Rational === r
          r
        else
          # MRI: #to_int under rb_protect (an exception in it is dropped)
          t = begin
            __responds?(v, :to_int) ? v.to_int : nil
          rescue StandardError
            nil
          end
          return __oruby_Rational(t, 1) if Integer === t
          raise TypeError, "can't convert #{__cname(v)} into Rational"
        end
      end
    end

    def __exact_zero?(x)
      (Integer === x || Rational === x) && x == 0
    end

    # MRI's string_to_c_strict: ASCII-compatible, no NUL, the whole string
    def __str_to_c_strict(s, raise_error)
      raise Encoding::CompatibilityError, "ASCII incompatible encoding: #{s.encoding}" unless s.encoding.ascii_compatible?
      if s.include?("\0")
        return nil unless raise_error
        raise ArgumentError, "string contains null byte"
      end
      c = String.__to_c(s, true)
      return c if c
      return nil unless raise_error
      raise ArgumentError, "invalid value for convert(): #{s.inspect}"
    end

    def __to_complex(v)
      case v
      when Complex, Numeric then v
      when String
        String.__to_c(v, true) || raise(ArgumentError, "invalid value for convert(): #{v.inspect}")
      else
        return v.to_c if __responds?(v, :to_c)
        raise TypeError, "can't convert #{__cname(v)} into Complex"
      end
    end
  end
end
