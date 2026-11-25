# The core of bigdecimal 4.1.3 (its C extension, bigdecimal.so, in MRI) over
# Go's math/big: what ext/bigdecimal/bigdecimal.c defines. The library's
# Ruby (lib/, upstream's unchanged) adds power, sqrt, BigMath and to_d.
class BigDecimal
  VERSION = "4.1.3"
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

  class << self
    undef_method :new rescue nil

    def allocate = raise(TypeError, "allocator undefined for BigDecimal")

    # [rounding mode, exception mode, limit] of the current thread (fiber),
    # as MRI keeps them thread-local
    def __modes = (Thread.current[:__bigdecimal_modes] ||= [ROUND_HALF_UP, 0, 0])

    def mode(which, value = :__none)
      raise TypeError, "no implicit conversion into Integer" unless which.is_a?(Integer)
      m = __modes
      if which == ROUND_MODE
        return m[0] if value == :__none || value.nil?
        return m[0] = __round_mode(value)
      end
      raise TypeError, "first argument for BigDecimal.mode invalid" if which <= 0 || (which & ~EXCEPTION_ALL) != 0
      return m[1] if value == :__none || value.nil?
      raise ArgumentError, "second argument must be true or false" unless value == true || value == false
      value ? m[1] |= which : m[1] &= ~which
      m[1]
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

    def __exc?(flag) = (__modes[1] & flag) != 0

    # the significant digits arithmetic results are rounded to (0: no limit)
    def limit(n = nil)
      m = __modes
      old = m[2]
      unless n.nil?
        n = n.to_int unless n.is_a?(Integer)
        raise ArgumentError, "argument must be positive" if n < 0
        m[2] = n
      end
      old
    end
    def double_fig = 16

    def save_rounding_mode
      old = __modes[0]
      yield
    ensure
      __modes[0] = old
    end

    def save_exception_mode
      old = __modes[1]
      yield
    ensure
      __modes[1] = old
    end

    def save_limit
      old = __modes[2]
      yield
    ensure
      __modes[2] = old
    end

    def _load(str)
      v = BigDecimal(str.to_s.sub(/\A\d+:/, ""), exception: false)
      raise TypeError, "load failed: invalid character in the marshaled string" if v.nil?
      v
    end

    # the number a string starts with (String#to_d); a NUL ends it
    def interpret_loosely(str)
      str = str.to_str
      __ascii_compatible!(str)
      __new(str.split("\0", 2).first || "", 0, false)
    end

    def __ascii_compatible!(str)
      return if str.encoding.ascii_compatible?
      raise Encoding::CompatibilityError, "ASCII incompatible encoding: #{str.encoding}"
    end

    # convert other operands to BigDecimal
    def __coerce(v, prec = 0)
      case v
      when BigDecimal then v
      when Integer then __new(v.to_s, 0, true)
      when Float
        __from_float(v, 0) # a Float at its shortest, whatever the precision
      when Rational
        p = prec > 2 * double_fig ? prec : 2 * double_fig
        __new(v.numerator.to_s, 0, true).__div(__new(v.denominator.to_s, 0, true), p, mode(ROUND_MODE))
      else
        nil
      end
    end
  end

  # prec nil: omitted (a Rational needs one; 0 means 32 digits for it)
  def self.__make(v, prec, exception)
    omitted = prec.nil?
    prec ||= 0
    case v
    when BigDecimal
      prec > 0 ? v.round_sig(prec) : v
    when Integer
      __new(v.to_s, prec, true)
    when Float
      if v.nan? || v.infinite?
        # one object each, as MRI's
        r = v.nan? ? (@__nan ||= __new("NaN", 0, true)) : v > 0 ? (@__pinf ||= __new("Infinity", 0, true)) : (@__ninf ||= __new("-Infinity", 0, true))
        __created(r)
      else
        raise ArgumentError, "precision too large." if prec > Float::DIG + 1
        if v.zero?
          return v.equal?(-0.0) || 1.0 / v < 0 ? (@__neg_zero ||= __from_float(v, 0)) : (@__pos_zero ||= __from_float(v, 0))
        end
        __from_float(v, prec)
      end
    when Rational
      raise ArgumentError, "can't omit precision for a Rational." if omitted
      __new(v.numerator.to_s, 0, true).div(__new(v.denominator.to_s, 0, true), prec > 0 ? prec : 2 * double_fig)
    when Complex
      unless v.imaginary.zero?
        return nil unless exception
        raise ArgumentError, "Unable to make a BigDecimal from non-zero imaginary number"
      end
      __make(v.real, omitted ? nil : prec, exception)
    when nil
      raise TypeError, "can't convert nil into BigDecimal"
    else
      if v.respond_to?(:to_str)
        s = v.to_str
        __ascii_compatible!(s)
        if s.include?("\0")
          return nil unless exception
          raise ArgumentError, "string contains null byte"
        end
        begin
          # a literal Infinity or NaN under its exception mode; an exponent
          # beyond the range reads as Infinity or 0 quietly, as MRI's
          r = __new(s, prec, true)
          s.match?(/\A\s*[-+]?(?:Infinity|NaN)\s*\z/) ? __created(r) : r
        rescue ArgumentError => __exc
          raise __exc if exception
          nil
        end
      else
        raise TypeError, "can't convert #{BigDecimal.__coerce_name(v)} into BigDecimal"
      end
    end
  end

  # a value made Infinity or NaN, under the exception modes: "results in"
  def self.__created(r)
    if r.nan? && __exc?(EXCEPTION_NaN)
      raise FloatDomainError, "Computation results in 'NaN' (Not a Number)"
    end
    if r.infinite? && __exc?(EXCEPTION_INFINITY)
      raise FloatDomainError, "Computation results in '#{r.infinite? > 0 ? '' : '-'}Infinity'"
    end
    r
  end

  # a special constant by its inspect (nil, true, :sym), others by class, as
  # MRI's coercion errors
  def self.__coerce_name(v)
    case v
    when nil, true, false, Symbol, Integer, Float then v.inspect
    else v.class.to_s
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

  # -- arithmetic --------------------------------------------------------------

  # v as a BigDecimal, nil for #coerce: a Rational with prec digits, or
  # (prec 0) as many as self has in BASE words, as MRI's GetCoercePrec
  def __other(v, prec = 0)
    prec = (precision + 8) / 9 * 9 if prec == 0 && Rational === v
    o = BigDecimal.__coerce(v, prec)
    if o.nil?
      if v.respond_to?(:coerce)
        return nil
      end
      raise TypeError, "#{BigDecimal.__coerce_name(v)} can't be coerced into BigDecimal"
    end
    o
  end
  private :__other

  # an operation's result under the exception modes: "results to"
  def __check(r)
    if r.nan? && BigDecimal.__exc?(EXCEPTION_NaN)
      raise FloatDomainError, "Computation results to 'NaN'"
    end
    if r.infinite? && BigDecimal.__exc?(EXCEPTION_INFINITY)
      raise FloatDomainError, "Computation results to '#{r.infinite? > 0 ? '' : '-'}Infinity'"
    end
    r
  end
  private :__check

  # BigDecimal.limit: the significant digits of +, -, *, / and what is
  # made of them (add/sub/mult/div with 0 digits), as MRI's
  def __lim(r)
    m = BigDecimal.__modes
    m[2] > 0 && r.finite? ? r.__round_sig(m[2], m[0]) : r
  end
  private :__lim

  def +(v)
    o = __other(v) or return __coerced(:+, v)
    __check(__lim(__add(o)))
  end

  def -(v)
    o = __other(v) or return __coerced(:-, v)
    __check(__lim(__sub(o)))
  end

  def *(v)
    o = __other(v) or return __coerced(:*, v)
    __check(__lim(__mul(o)))
  end

  # max(digits of the operands, double_fig) + double_fig digits, as MRI 4's
  def /(v)
    o = __other(v) or return __coerced(:/, v)
    if o.zero? && !nan?
      raise FloatDomainError, "Division by zero" if !zero? && BigDecimal.__exc?(EXCEPTION_ZERODIVIDE)
      return BigDecimal.__created(__div(o, 1, ROUND_HALF_UP))
    end
    m = BigDecimal.__modes
    prec = [[precision, o.precision].max + BigDecimal.double_fig, 2 * BigDecimal.double_fig].max
    prec = m[2] if m[2] > 0 && m[2] < prec
    __check(__div(o, prec, m[0]))
  end

  def quo(v, digits = nil)
    return self / v if digits.nil?
    div(v, digits)
  end

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

  def add(v, digits) = __add_sub(v, digits, false)
  def sub(v, digits) = __add_sub(v, digits, true)

  def __add_sub(v, digits, subtract)
    digits = __check_prec(digits)
    o = digits > 0 && __other(v, digits)
    return (subtract ? self - v : self + v).then { |r| digits > 0 ? r.round_sig(digits) : r } unless o
    __check(__add_prec(o, digits, subtract)).round_sig(digits)
  end
  private :__add_sub

  def mult(v, digits)
    digits = __check_prec(digits)
    return self * v if digits == 0
    o = __other(v, digits) or return (self * v).round_sig(digits)
    __check(__mul(o)).round_sig(digits)
  end

  def div(v, digits = nil)
    if digits.nil?
      o = __other(v) or return __coerced(:div, v)
      raise ZeroDivisionError, "divided by 0" if o.zero?
      raise FloatDomainError, "Computation results to 'NaN'" if nan? || o.nan?
      raise FloatDomainError, "Computation results to 'Infinity'" if infinite?
      return 0 if o.infinite?
      return __divmod(o)[0].to_i
    end
    digits = __check_prec(digits)
    return self / v if digits == 0
    o = __other(v, digits) or return __coerced(:div, v)
    __check(__div(o, digits, BigDecimal.mode(ROUND_MODE)))
  end

  # [Integer, BigDecimal] since 4.0: a NaN or infinite quotient is a
  # FloatDomainError, as its to_i is
  def divmod(v)
    o = __other(v) or return __coerced(:divmod, v)
    q, r = __divmod(o)
    [q.to_i, r]
  end

  def __divmod(o)
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
    return [BigDecimal("0"), self] if zero? # a signed zero stays as it is
    # the exact integer quotient and remainder, apart from BigDecimal.limit
    q = __div(o, n_significant_digits + o.n_significant_digits + exponent.abs + o.exponent.abs + 20, ROUND_DOWN).__round(0, ROUND_FLOOR)
    [q, __sub(o.__mul(q))]
  end

  def %(v)
    o = __other(v) or return __coerced(:%, v)
    __divmod(o)[1]
  end
  alias modulo %

  def remainder(v)
    o = __other(v) or return __coerced(:remainder, v)
    return BigDecimal("NaN") if nan? || o.nan? || infinite?
    return self if o.infinite?
    raise ZeroDivisionError, "divided by 0" if o.zero?
    return self if zero?
    q = __div(o, n_significant_digits + o.n_significant_digits + exponent.abs + o.exponent.abs + 20, ROUND_DOWN).__round(0, ROUND_DOWN)
    __sub(o.__mul(q))
  end

  def abs = __neg && !nan? ? BigDecimal.__created(negate) : self
  def -@ = BigDecimal.__created(negate)
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
    to_int = mode.nil? && half.nil? && (n.nil? || n.to_int <= 0)
    if to_int
      raise FloatDomainError, "Computation results to 'NaN'(Not a Number)" if nan?
      raise FloatDomainError, "Computation results to 'Infinity'" if infinite?
      return __round(n.nil? ? 0 : n.to_int, m).to_i
    end
    __round(n.nil? ? 0 : n.to_int, m)
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
    __sub(fix)
  end

  # self * 10**n, exactly (the library's Ruby scales with it)
  def _decimal_shift(n)
    return BigDecimal.__created(self) unless finite?
    __decimal_shift(n)
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
    if o.nil?
      return false unless v.respond_to?(:coerce)
      a, b = (v.coerce(self) rescue (return false))
      return a == b
    end
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
    raise TypeError, "#{BigDecimal.__coerce_name(other)} can't be coerced into BigDecimal" unless o
    [o, self]
  end

  # -- conversion --------------------------------------------------------------

  def to_s(fmt = "")
    fmt = fmt.to_s if fmt.is_a?(Integer)
    __to_s(fmt.is_a?(String) ? fmt : fmt.to_s)
  end

  def inspect = __to_s("")

  # beyond Float's range: Infinity or 0.0, a FloatDomainError under
  # EXCEPTION_OVERFLOW / EXCEPTION_UNDERFLOW
  alias __to_f to_f
  def to_f
    f = __to_f
    if finite? && !zero?
      if f.infinite? && BigDecimal.__exc?(EXCEPTION_OVERFLOW)
        raise FloatDomainError, "BigDecimal to Float conversion"
      elsif f.zero? && BigDecimal.__exc?(EXCEPTION_UNDERFLOW)
        raise FloatDomainError, "BigDecimal to Float conversion"
      end
    end
    f
  end

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
  def BigDecimal(v, prec = (omitted = true; nil), exception: true)
    unless omitted || prec.is_a?(Integer)
      raise TypeError, "no implicit conversion of #{prec.nil? ? 'nil' : prec == false ? 'false' : prec.class} into Integer" unless prec.respond_to?(:to_int)
      prec = prec.to_int
    end
    if prec && prec < 0
      return nil unless exception
      raise ArgumentError, "negative precision"
    end
    return BigDecimal.__make(v, prec, exception) if exception
    begin
      BigDecimal.__make(v, prec, exception)
    rescue ArgumentError, TypeError # exception: false: nil for what BigDecimal() cannot take
      nil
    end
  end
  module_function :BigDecimal
end

class BigDecimal
  INFINITY = BigDecimal("Infinity")
  NAN = BigDecimal("NaN")
end
