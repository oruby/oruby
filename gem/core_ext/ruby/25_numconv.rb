# String#to_r / #to_c and Kernel#Rational / #Complex conversions as in MRI
# (rational.c read_rat, complex.c read_comp): lenient for String#to_*,
# strict (whole string) for the Kernel functions, exception: false support.
class String
  NUM_RE = '(?:\d+(?:_\d+)*)'
  RAT_RE = "(?:[-+]?(?:#{NUM_RE}(?:\\.#{NUM_RE})?|\\.#{NUM_RE})(?:[eE][-+]?#{NUM_RE})?(?:/#{NUM_RE})?)"
  UNUM_RE = "(?:#{NUM_RE}?(?:\\.#{NUM_RE})?(?:[eE][-+]?#{NUM_RE})?(?:/#{NUM_RE})?)"
  RAT_PREFIX = Regexp.new("\\A\\s*#{RAT_RE}")
  COMP_POLAR = Regexp.new("\\A\\s*(#{RAT_RE})@(#{RAT_RE})")
  COMP_RECT = Regexp.new("\\A\\s*(#{RAT_RE})?(?:([-+])(#{UNUM_RE})?[ij])")
  COMP_IMAG = Regexp.new("\\A\\s*([-+]?#{UNUM_RE}?)[ij]")
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
    if t.include?(".") || t =~ /[eE]/
      return Float(t) unless exact
      m = t.match(/\A([-+])?(\d*)(?:\.(\d+))?(?:[eE]([-+]?\d+))?\z/)
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
    m = RAT_PREFIX.match(self)
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
    if (m = COMP_POLAR.match(s))
      c = Complex.polar(__num(m[1], false), __num(m[2], false))
    elsif (m = COMP_RECT.match(s))
      re = m[1] ? __num(m[1], false) : 0
      im = m[3] && !m[3].empty? ? __num(m[3], false) : 1
      im = -im if m[2] == "-"
      c = __oruby_Complex(re, im)
    elsif (m = COMP_IMAG.match(s))
      t = m[1]
      im = t.empty? || t == "+" ? 1 : t == "-" ? -1 : __num(t, false)
      c = __oruby_Complex(0, im)
    elsif (m = COMP_REAL.match(s))
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

module Kernel
  alias __oruby_Rational Rational
  alias __oruby_Complex Complex
  private :__oruby_Rational, :__oruby_Complex

  def Rational(a, b = (no_b = true; nil), exception: true)
    unless exception == true || exception == false
      raise ArgumentError, "expected true or false as exception: #{exception.inspect}"
    end
    begin
      ra = Kernel.__to_rational(a)
      return ra if no_b
      rb = Kernel.__to_rational(b)
      raise ZeroDivisionError, "divided by 0" if rb == 0
      return ra / rb if Complex === ra || Complex === rb
      r = Rational === ra || Rational === rb || Float === ra || Float === rb ? ra.to_r / rb.to_r : __oruby_Rational(ra, rb)
      Rational === r ? r : __oruby_Rational(r, 1)
    rescue StandardError
      raise if exception
      nil
    end
  end

  def Complex(a, b = (no_b = true; nil), exception: true)
    unless exception == true || exception == false
      raise ArgumentError, "expected true or false as exception: #{exception.inspect}"
    end
    begin
      raise TypeError, "can't convert nil into Complex" if a.nil? || (!no_b && b.nil?)
      ca = Kernel.__to_complex(a)
      return ca if no_b && Complex === ca
      return __oruby_Complex(ca, 0) if no_b
      cb = Kernel.__to_complex(b)
      return ca + cb * Complex::I if Complex === ca || Complex === cb
      __oruby_Complex(ca, cb)
    rescue StandardError
      raise if exception
      nil
    end
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
        m = s.match(/\A[-+]?\d+(?:_\d+)*(?:\.\d+(?:_\d+)*)?(?:[eE][-+]?\d+)?(?:\/\d+(?:_\d+)*)?\z/)
        raise ArgumentError, "invalid value for convert(): #{v.inspect}" unless m
        r = String.__num(s, true)
        Integer === r ? __oruby_Rational(r, 1) : r
      when Complex
        raise RangeError, "can't convert #{v} into Rational" unless v.imaginary.is_a?(Integer) && v.imaginary == 0 || v.imaginary.is_a?(Rational) && v.imaginary == 0
        __to_rational(v.real)
      else
        if __responds?(v, :to_r)
          r = v.to_r
          raise TypeError, "can't convert #{__cname(v)} to Rational (#{__cname(v)}#to_r gives #{__cname(r)})" unless Rational === r
          r
        elsif __responds?(v, :to_int)
          __oruby_Rational(v.to_int, 1)
        else
          raise TypeError, "can't convert #{__cname(v)} into Rational"
        end
      end
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
