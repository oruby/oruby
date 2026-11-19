class BigDecimal
  VERSION = "3.1.9"
  BASE = 1_000_000_000

  EXCEPTION_ALL = 0xff
  EXCEPTION_NaN = 0x02
  EXCEPTION_INFINITY = 0x01
  EXCEPTION_UNDERFLOW = 0x04
  EXCEPTION_OVERFLOW = 0x01
  EXCEPTION_ZERODIVIDE = 0x10
  ROUND_MODE = 0x100
  ROUND_UP = 1
  ROUND_DOWN = 2
  ROUND_HALF_UP = 3
  ROUND_HALF_DOWN = 4
  ROUND_CEILING = 5
  ROUND_FLOOR = 6
  ROUND_HALF_EVEN = 7

  SIGN_NaN = 0
  SIGN_POSITIVE_ZERO = 1
  SIGN_NEGATIVE_ZERO = -1
  SIGN_POSITIVE_FINITE = 2
  SIGN_NEGATIVE_FINITE = -2
  SIGN_POSITIVE_INFINITE = 3
  SIGN_NEGATIVE_INFINITE = -3

  @rounding_mode = ROUND_HALF_UP
  @exception_mode = 0

  class << self
    undef_method :new rescue nil

    def mode(which, value = :__none)
      if which == ROUND_MODE
        return @rounding_mode if value == :__none || value.nil?
        @rounding_mode = __round_mode(value)
      else
        return @exception_mode if value == :__none || value.nil?
        raise ArgumentError, "second argument must be true or false" unless value == true || value == false
        if value
          @exception_mode |= which
        else
          @exception_mode &= ~which
        end
        @exception_mode
      end
    end

    def __round_mode(v)
      case v
      when Integer
        raise ArgumentError, "invalid rounding mode" unless (1..7).include?(v)
        v
      when :up then ROUND_UP
      when :down, :truncate then ROUND_DOWN
      when :half_up, :default then ROUND_HALF_UP
      when :half_down then ROUND_HALF_DOWN
      when :half_even, :banker then ROUND_HALF_EVEN
      when :ceiling, :ceil then ROUND_CEILING
      when :floor then ROUND_FLOOR
      else raise ArgumentError, "invalid rounding mode (#{v})"
      end
    end

    def __exc?(flag) = (@exception_mode & flag) != 0

    def limit(n = nil) = __limit(n)
    def double_fig = 16

    def save_rounding_mode
      old = @rounding_mode
      yield
    ensure
      @rounding_mode = old
    end

    def save_exception_mode
      old = @exception_mode
      yield
    ensure
      @exception_mode = old
    end

    def save_limit
      old = limit
      yield
    ensure
      limit(old)
    end

    def _load(str) = BigDecimal(str.sub(/\A\d+:/, ""))

    def interpret_loosely(str) = __new(str.to_s, 0, false)

    # convert other operands to BigDecimal
    def __coerce(v, prec = 0)
      case v
      when BigDecimal then v
      when Integer then __new(v.to_s, 0, true)
      when Float
        __from_float(v, prec)
      when Rational
        p = prec > 0 ? prec : 32
        __new(v.numerator.to_s, 0, true).div(__new(v.denominator.to_s, 0, true), p)
      else
        nil
      end
    end
  end

  def self.__make(v, prec, exception)
    case v
    when BigDecimal
      prec > 0 ? v.round_sig(prec) : v
    when Integer
      __new(v.to_s, prec, true)
    when Float
      if v.nan? || v.infinite?
        __new(v.nan? ? "NaN" : (v > 0 ? "Infinity" : "-Infinity"), 0, true)
      else
        raise ArgumentError, "precision too large." if prec > 17
        __from_float(v, prec)
      end
    when Rational
      raise ArgumentError, "can't omit precision for a Rational." if prec == 0
      __new(v.numerator.to_s, 0, true).div(__new(v.denominator.to_s, 0, true), prec)
    when Complex
      raise ArgumentError, "Unable to make a BigDecimal from non-zero imaginary number" unless v.imaginary.zero?
      __make(v.real, prec, exception)
    when nil
      raise TypeError, "can't convert nil into BigDecimal"
    else
      if v.respond_to?(:to_str)
        s = v.to_str
        begin
          r = __new(s, prec, true)
          if r.infinite? && !(s.strip == "Infinity" || s.strip == "+Infinity" || s.strip == "-Infinity") && BigDecimal.__exc?(EXCEPTION_OVERFLOW)
            raise FloatDomainError, "Computation results in '#{r.infinite? > 0 ? '' : '-'}Infinity'"
          end
          r
        rescue ArgumentError => __exc
          raise __exc if exception
          nil
        end
      else
        raise TypeError, "can't convert #{v.class} into BigDecimal"
      end
    end
  end

  def round_sig(n) = __round_sig(n, BigDecimal.mode(ROUND_MODE))

  # -- predicates --------------------------------------------------------------

  def nan? = __kind == "nan"
  def finite? = __kind == "finite"
  def infinite? = __kind == "infinity" ? (__neg ? -1 : 1) : nil
  def zero? = __zero
  def nonzero? = zero? ? nil : self
  def negative? = __neg && !nan? && !zero?
  def positive? = !__neg && !nan? && !zero?
  def integer? = false
  def real? = true

  def sign
    case __kind
    when "nan" then SIGN_NaN
    when "infinity" then __neg ? SIGN_NEGATIVE_INFINITE : SIGN_POSITIVE_INFINITE
    else
      if zero?
        __neg ? SIGN_NEGATIVE_ZERO : SIGN_POSITIVE_ZERO
      else
        __neg ? SIGN_NEGATIVE_FINITE : SIGN_POSITIVE_FINITE
      end
    end
  end

  def precs = [n_significant_digits, n_significant_digits + 9]

  # -- arithmetic --------------------------------------------------------------

  def __other(v, op = nil)
    o = BigDecimal.__coerce(v, op == :div ? 0 : 0)
    if o.nil?
      if v.respond_to?(:coerce)
        return nil
      end
      raise TypeError, "#{v.nil? ? 'nil' : v.class} can't be coerced into BigDecimal"
    end
    o
  end
  private :__other

  def __check(r)
    if r.nan? && BigDecimal.__exc?(EXCEPTION_NaN)
      raise FloatDomainError, "Computation results to 'NaN'(Not a Number)"
    end
    if r.infinite? && BigDecimal.__exc?(EXCEPTION_INFINITY)
      raise FloatDomainError, "Computation results in '#{r.infinite? > 0 ? '' : '-'}Infinity'"
    end
    r
  end
  private :__check

  def +(v)
    o = __other(v) or return __coerced(:+, v)
    __check(__add(o))
  end

  def -(v)
    o = __other(v) or return __coerced(:-, v)
    __check(__sub(o))
  end

  def *(v)
    o = __other(v) or return __coerced(:*, v)
    __check(__mul(o))
  end

  def /(v)
    o = __other(v) or return __coerced(:/, v)
    if o.zero? && !nan? && !zero? && BigDecimal.__exc?(EXCEPTION_ZERODIVIDE)
      raise FloatDomainError, "Division by zero"
    end
    __check(__div(o, 0, BigDecimal.mode(ROUND_MODE)))
  end
  alias quo /

  def __coerced(op, v)
    a, b = v.coerce(self)
    a.__send__(op, b)
  end
  private :__coerced

  def __check_prec(digits)
    unless digits.is_a?(Integer)
      raise TypeError, "no implicit conversion of #{digits.nil? ? 'nil' : digits.class} into Integer" unless digits.respond_to?(:to_int)
      digits = digits.to_int
      raise TypeError, "can't convert into Integer" unless digits.is_a?(Integer)
    end
    raise ArgumentError, "negative precision" if digits < 0
    digits
  end
  private :__check_prec

  def add(v, digits)
    digits = __check_prec(digits)
    r = self + v
    digits > 0 ? r.round_sig(digits) : r
  end

  def sub(v, digits)
    digits = __check_prec(digits)
    r = self - v
    digits > 0 ? r.round_sig(digits) : r
  end

  def mult(v, digits)
    digits = __check_prec(digits)
    r = self * v
    digits > 0 ? r.round_sig(digits) : r
  end

  def div(v, digits = nil)
    if digits.nil?
      o = __other(v) or return __coerced(:div, v)
      raise ZeroDivisionError, "divided by 0" if o.zero?
      raise FloatDomainError, "Computation results to 'NaN'" if nan? || o.nan?
      raise FloatDomainError, "Computation results to 'Infinity'" if infinite?
      return 0 if o.infinite?
      return (self / o).floor
    end
    digits = __check_prec(digits)
    o = __other(v)
    return self / o if digits == 0
    __check(__div(o, digits, BigDecimal.mode(ROUND_MODE)))
  end

  def divmod(v)
    o = __other(v) or return __coerced(:divmod, v)
    if nan? || o.nan? || (infinite? && o.infinite?)
      return [BigDecimal("NaN"), BigDecimal("NaN")]
    end
    raise ZeroDivisionError, "divided by 0" if o.zero?
    if infinite?
      return [BigDecimal(((__neg ^ o.__neg) ? "-" : "") + "Infinity"), BigDecimal("NaN")]
    end
    if o.infinite?
      return zero? || (__neg == o.__neg) ? [BigDecimal("0"), self] : [BigDecimal("-1"), o]
    end
    q = __div(o, n_significant_digits + o.n_significant_digits + exponent.abs + o.exponent.abs + 20, ROUND_DOWN).floor
    q = BigDecimal(q.to_s)
    [q, self - o * q]
  end

  def %(v)
    divmod(v)[1]
  end
  alias modulo %

  def remainder(v)
    o = __other(v) or return __coerced(:remainder, v)
    return BigDecimal("NaN") if nan? || o.nan? || infinite?
    return self if o.infinite?
    q = __div(o, n_significant_digits + o.n_significant_digits + exponent.abs + o.exponent.abs + 20, ROUND_DOWN).truncate
    self - o * BigDecimal(q.to_s)
  end

  def **(n)
    power(n)
  end

  def power(n, prec = nil)
    if n.is_a?(Integer)
      return BigDecimal("NaN") if nan?
      if infinite?
        return BigDecimal("1") if n == 0
        return n > 0 ? (n.odd? || !__neg ? self : -self) : BigDecimal(n.odd? && __neg ? "-0" : "0")
      end
      if zero?
        return BigDecimal("1") if n == 0
        return n > 0 ? self : BigDecimal((n.odd? && __neg ? "-" : "") + "Infinity")
      end
      result = BigDecimal("1")
      base = self
      e = n.abs
      while e > 0
        result *= base if e.odd?
        base *= base
        e >>= 1
      end
      result = BigDecimal("1").div(result, prec || [n_significant_digits * n.abs + 32, 64].max) if n < 0
      return prec ? result.round_sig(prec) : result
    end
    if n.is_a?(BigDecimal) && n.frac.zero? && n.finite?
      return power(n.to_i, prec)
    end
    raise TypeError, "wrong argument type #{n.class} (expected scalar Numeric)" unless n.is_a?(Numeric)
    BigDecimal(to_f**n.to_f, prec || 16)
  end

  def sqrt(n)
    n = __check_prec(n)
    raise FloatDomainError, "sqrt of negative value" if negative?
    raise FloatDomainError, "sqrt of 'NaN'(Not a Number)" if nan?
    __sqrt(n)
  end

  def abs = __neg && !nan? ? negate : self
  def -@ = negate
  def +@ = self

  # -- rounding ----------------------------------------------------------------

  def round(n = nil, mode = nil, half: nil)
    if n.is_a?(Symbol) || n.is_a?(String)
      mode = n
      n = nil
    end
    m = if half
          case half.to_s
          when "up" then ROUND_HALF_UP
          when "down" then ROUND_HALF_DOWN
          when "even" then ROUND_HALF_EVEN
          else raise ArgumentError, "invalid rounding mode (#{half})"
          end
        elsif mode
          BigDecimal.__round_mode(mode)
        else
          BigDecimal.mode(ROUND_MODE)
        end
    if n.nil?
      raise FloatDomainError, "Computation results to 'NaN'(Not a Number)" if nan?
      raise FloatDomainError, "Computation results to 'Infinity'" if infinite?
      return __round(0, m).to_i
    end
    __round(n.to_int, m)
  end

  def floor(n = nil)
    if n.nil?
      raise FloatDomainError, "Computation results in 'NaN' (Not a Number)" if nan?
      raise FloatDomainError, "Computation results in 'Infinity'" if infinite?
      return __round(0, ROUND_FLOOR).to_i
    end
    __round(n, ROUND_FLOOR)
  end

  def ceil(n = nil)
    if n.nil?
      raise FloatDomainError, "Computation results in 'NaN' (Not a Number)" if nan?
      raise FloatDomainError, "Computation results in 'Infinity'" if infinite?
      return __round(0, ROUND_CEILING).to_i
    end
    __round(n, ROUND_CEILING)
  end

  def truncate(n = nil)
    if n.nil?
      raise FloatDomainError, "Computation results in 'NaN' (Not a Number)" if nan?
      raise FloatDomainError, "Computation results in 'Infinity'" if infinite?
      return __round(0, ROUND_DOWN).to_i
    end
    __round(n, ROUND_DOWN)
  end

  def fix
    return self unless finite?
    __round(0, ROUND_DOWN)
  end

  def frac
    return self unless finite?
    self - fix
  end

  # -- comparison --------------------------------------------------------------

  def <=>(v)
    o = BigDecimal.__coerce(v, 0)
    if o.nil?
      return nil unless v.respond_to?(:coerce)
      a, b = v.coerce(self) rescue (return nil)
      return a <=> b
    end
    __cmp(o)
  end

  def ==(v)
    o = BigDecimal.__coerce(v, 0)
    return (v == self) if o.nil? && v.respond_to?(:coerce) && !v.is_a?(Numeric) rescue false
    return false if o.nil?
    __cmp(o) == 0
  end
  alias === ==
  alias eql? ==

  %i[< <= > >=].each do |op|
    define_method(op) do |v|
      o = BigDecimal.__coerce(v, 0)
      if o.nil?
        raise ArgumentError, "comparison of BigDecimal with #{v.nil? ? 'nil' : v.inspect} failed" unless v.respond_to?(:coerce)
        a, b = v.coerce(self)
        return a.__send__(op, b)
      end
      c = __cmp(o)
      return false if c.nil?
      c.__send__(op, 0)
    end
  end

  def coerce(other)
    o = BigDecimal.__coerce(other)
    raise TypeError, "#{other.class} can't be coerced into BigDecimal" unless o
    [o, self]
  end

  # -- conversion --------------------------------------------------------------

  def to_s(fmt = "")
    fmt = fmt.to_s if fmt.is_a?(Integer)
    __to_s(fmt.is_a?(String) ? fmt : fmt.to_s)
  end

  def inspect = __to_s("")

  def to_i
    raise FloatDomainError, nan? ? "NaN" : (infinite? > 0 ? "Infinity" : "-Infinity") unless finite?
    __int_str.to_i
  end
  alias to_int to_i

  def to_r
    raise FloatDomainError, nan? ? "NaN" : (infinite? > 0 ? "Infinity" : "-Infinity") unless finite?
    n, d = __rational
    Rational(n.to_i, d.to_i)
  end

  def to_d = self
  def to_digits = to_s("F")

  def split
    s = __neg ? -1 : 1
    case __kind
    when "nan" then [0, "NaN", 10, 0]
    when "infinity" then [s, "Infinity", 10, 0]
    else
      zero? ? [s, "0", 10, 0] : [s, __digits, 10, exponent]
    end
  end

  def _dump(level = nil) = "18:#{to_s}"
  def clone = self
  alias dup clone
  def precision_scale = [precision, scale]
end


module Kernel
  def BigDecimal(v, prec = 0, exception: true)
    unless prec.is_a?(Integer)
      raise TypeError, "can't convert #{prec.class} into Integer" unless prec.respond_to?(:to_int)
      prec = prec.to_int
    end
    raise ArgumentError, "negative precision" if prec < 0
    BigDecimal.__make(v, prec, exception)
  end
  module_function :BigDecimal
end

module BigMath
  module_function

  def sqrt(x, prec) = BigDecimal(x).sqrt(prec)

  def log(x, prec)
    raise ArgumentError, "Zero or negative precision for log" if prec <= 0
    x = case x
        when BigDecimal then x
        when Rational then BigDecimal(x, prec + 16)
        when Integer then BigDecimal(x)
        when Float then BigDecimal(x, 0)
        else raise TypeError, "can't convert #{x.class} into BigDecimal"
        end
    BigDecimal.__log(x, prec)
  end

  def PI(prec)
    raise ArgumentError, "Zero or negative precision for PI" if prec <= 0
    n = prec + 16
    one = BigDecimal("1")
    # Machin: pi = 16 atan(1/5) - 4 atan(1/239)
    atan_inv = lambda do |k|
      x = one.div(k, n)
      x2 = BigDecimal(k * k)
      sum = x
      term = x
      i = 1
      loop do
        term = term.div(x2, n)
        t = term.div(2 * i + 1, n)
        break if t.zero? || t.exponent < -n
        sum = i.odd? ? sum - t : sum + t
        i += 1
      end
      sum
    end
    (atan_inv.call(5) * 16 - atan_inv.call(239) * 4).round_sig(prec)
  end

  def E(prec)
    raise ArgumentError, "Zero or negative precision for E" if prec <= 0
    n = prec + 16
    sum = BigDecimal("1")
    term = BigDecimal("1")
    i = 1
    loop do
      term = term.div(i, n)
      break if term.zero? || term.exponent < -n
      sum += term
      i += 1
    end
    sum.round_sig(prec)
  end
end

class Integer
  def to_d = BigDecimal(self)
end

class Float
  def to_d(prec = 0) = BigDecimal(self, prec > 0 ? prec : 0)
end

class String
  def to_d
    BigDecimal.interpret_loosely(self)
  end
end

class Rational
  def to_d(prec) = BigDecimal(self, prec)
end

class NilClass
  def to_d = BigDecimal("0")
end
class BigDecimal
  INFINITY = BigDecimal("Infinity")
  NAN = BigDecimal("NaN")
end
