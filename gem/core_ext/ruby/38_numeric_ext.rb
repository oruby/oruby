# Numeric, Integer, Float, Complex, Rational MRI compatibility extensions

# Shared helpers: pure-Integer Stern-Brocot rationalize, exact IEEE-754 bigint division, Float->(num,den)
module Kernel
  def self.__float_num_den(f)
    return [0, 1] if f == 0.0
    m, e = Math.frexp(f)
    mant = (m * (1 << 53)).to_i
    exp = e - 53
    while mant != 0 && (mant & 1) == 0
      mant >>= 1
      exp += 1
    end
    exp >= 0 ? [mant << exp, 1] : [mant, 1 << (-exp)]
  end

  def self.__bigint_div_to_f(n, d)
    raise ZeroDivisionError, "divided by 0" if d == 0
    return (d < 0 ? -0.0 : 0.0) if n == 0
    neg = (n < 0) ^ (d < 0)
    a = n.abs
    b = d.abs
    e = a.bit_length - b.bit_length
    if e >= 0
      e -= 1 if a < (b << e)
    else
      e -= 1 if (a << (-e)) < b
    end
    return neg ? -Float::INFINITY : Float::INFINITY if e > 1023
    return neg ? -0.0 : 0.0 if e < -1075
    prec = e >= -1022 ? 53 : 1075 + e
    s = (prec - 1) - e
    if s >= 0
      num = a << s
      den = b
    else
      num = a
      den = b << (-s)
    end
    q, r = num.divmod(den)
    cmp = (r << 1) <=> den
    q += 1 if cmp > 0 || (cmp == 0 && q.odd?)
    res = Math.ldexp(q.to_f, -s)
    neg ? -res : res
  end

  def self.__rationalize_int(n, d, eps_n, eps_d)
    eps_n = eps_n.abs
    if eps_n == 0
      g = n.gcd(d)
      return Rational(n / g, d / g)
    end
    neg = n < 0
    n = -n if neg
    # lo = n/d - eps_n/eps_d, hi = n/d + eps_n/eps_d
    cd = d * eps_d
    lo_n = n * eps_d - eps_n * d
    hi_n = n * eps_d + eps_n * d
    return Rational(0, 1) if lo_n <= 0 && hi_n >= 0
    if lo_n < 0
      return Rational(0, 1)
    end
    # Stern-Brocot / continued fraction for simplest p/q in [lo_n/cd, hi_n/cd]
    p0, q0 = 0, 1
    p1, q1 = 1, 0
    x_num, x_den = lo_n, cd
    y_num, y_den = hi_n, cd
    loop do
      a = x_num / x_den
      b = y_num / y_den
      if a != b
        k = (x_num % x_den == 0) ? a : a + 1
        p = p0 + k * p1
        q = q0 + k * q1
        return neg ? Rational(-p, q) : Rational(p, q)
      end
      p2 = p0 + a * p1
      q2 = q0 + a * q1
      p0, q0 = p1, q1
      p1, q1 = p2, q2
      rx = x_num - a * x_den
      ry = y_num - a * y_den
      if rx == 0 || ry == 0
        return neg ? Rational(-p1, q1) : Rational(p1, q1)
      end
      x_num, x_den, y_num, y_den = y_den, ry, x_den, rx
    end
  end

  def self.__num_den_of(x)
    case x
    when Integer then [x, 1]
    when Rational then [x.numerator, x.denominator]
    when Float then __float_num_den(x)
    else
      r = x.to_r
      [r.numerator, r.denominator]
    end
  end

  def self.__check_half(half)
    case half
    when nil, :up, :down, :even
      half || :up
    else
      raise ArgumentError, "invalid rounding mode: #{half}"
    end
  end

  def self.__cplx_builtin?(v)
    Integer === v || Float === v || Rational === v
  end
end

class Numeric
  def abs2
    self * self
  end

  def arg
    self < 0 ? Math::PI : 0
  end
  alias angle arg
  alias phase arg

  def conjugate
    self
  end
  alias conj conjugate

  def real
    self
  end

  def imaginary
    0
  end
  alias imag imaginary

  def rectangular
    [self, 0]
  end
  alias rect rectangular

  def polar
    [abs, arg]
  end

  alias magnitude abs

  def numerator
    to_r.numerator
  end

  def denominator
    to_r.denominator
  end

  def ceil(*args)
    to_f.ceil(*args)
  end

  def floor(*args)
    to_f.floor(*args)
  end

  def round(*args, **kw)
    kw.empty? ? to_f.round(*args) : to_f.round(*args, **kw)
  end

  def truncate(*args)
    to_f.truncate(*args)
  end

  def to_int
    to_i
  end

  def div(other)
    raise ZeroDivisionError, "divided by 0" if other == 0
    (self / other).floor
  end

  def %(other)
    self - other * div(other)
  end
  alias modulo %

  def divmod(other)
    [div(other), self % other]
  end

  def remainder(other)
    a, b = other.coerce(self)
    x = a % b
    if x != 0
      if (a < 0 && b > 0) || (a > 0 && b < 0)
        return x - b
      end
    end
    x
  end

  def quo(other)
    r = to_r
    raise TypeError, "can't convert #{self.class} into Rational (#{self.class}#to_r gives #{r.class})" unless Rational === r
    r / other
  end

  def infinite?
    nil
  end

  def dup
    self
  end

  def nonzero?
    zero? ? nil : self
  end

  def -@
    a, b = coerce(0)
    a - b
  end

  private def singleton_method_added(name)
    raise TypeError, "can't define singleton"
  end
end

class Float
  alias magnitude abs
  alias === ==

  def arg
    return self if nan?
    return Math::PI if self < 0 || (self == 0.0 && (1.0 / self) < 0)
    0
  end
  alias angle arg
  alias phase arg

  def denominator
    return 1 if nan? || infinite?
    to_r.denominator
  end

  def numerator
    return self if nan? || infinite?
    to_r.numerator
  end

  def rationalize(eps = (no_eps = true; nil))
    raise FloatDomainError, to_s if nan? || infinite?
    n, d = Kernel.__float_num_den(self)
    if no_eps
      return Rational(0, 1) if n == 0
      pn, pd = Kernel.__float_num_den(prev_float)
      nn, nd = Kernel.__float_num_den(next_float)
      # eps_lo = (self - prev_float) / 2, eps_hi = (next_float - self) / 2
      # Use common denominator for comparison
      elo_n, elo_d = n * pd - pn * d, 2 * d * pd
      ehi_n, ehi_d = nn * d - n * nd, 2 * d * nd
      if elo_n * ehi_d <= ehi_n * elo_d
        en, ed = elo_n, elo_d
      else
        en, ed = ehi_n, ehi_d
      end
      r = Kernel.__rationalize_int(n, d, en, ed)
      return r if r.to_f == self
      g = n.gcd(d)
      Rational(n / g, d / g)
    else
      en, ed = Kernel.__num_den_of(eps)
      Kernel.__rationalize_int(n, d, en, ed)
    end
  end

  alias __orig_flt_round round
  def round(ndigits = 0, half: nil)
    # the common cases: round half up to an Integer in the Fixnum range (C),
    # to ndigits decimals (Go, floatround.go)
    if half.nil? && Integer === ndigits
      return __orig_flt_round if ndigits == 0 && abs < 4.0e18
      return __oruby_dec_round(ndigits, :up) if ndigits > 0 && ndigits <= 350 && finite?
    end
    mode = Kernel.__check_half(half)
    ndigits = Kernel.__to_int(ndigits)
    if ndigits <= 0
      raise FloatDomainError, to_s if nan? || infinite?
      n, d = Kernel.__float_num_den(self)
      if ndigits < 0
        f = 10 ** (-ndigits)
        d *= f
      end
      neg = n < 0
      na = neg ? -n : n
      q, r = (na * 2 + d).divmod(d * 2)
      if r == 0
        case mode
        when :even then q -= 1 if q.odd?
        when :down then q -= 1
        end
      end
      q = -q if neg
      return ndigits < 0 ? q * (10 ** (-ndigits)) : q
    end
    return self if nan? || infinite?
    return self if ndigits > 350
    # MRI rounds the shortest decimal representation (to_s): gem/core_ext's
    # floatround.go
    __oruby_dec_round(ndigits, mode)
  end

  alias __orig_flt_floor floor
  def floor(ndigits = 0)
    return __orig_flt_floor if Integer === ndigits && ndigits == 0
    ndigits = Kernel.__to_int(ndigits)
    return __orig_flt_floor if ndigits == 0
    return self if ndigits > 0 && (nan? || infinite?)
    raise FloatDomainError, to_s if nan? || infinite?
    return self if ndigits > 350
    return __oruby_dec_floor_ceil(ndigits, false) if ndigits > 0
    n, d = Kernel.__float_num_den(self)
    s = 10 ** (-ndigits)
    n.div(d * s) * s
  end

  alias __orig_flt_ceil ceil
  def ceil(ndigits = 0)
    return __orig_flt_ceil if Integer === ndigits && ndigits == 0
    ndigits = Kernel.__to_int(ndigits)
    return __orig_flt_ceil if ndigits == 0
    return self if ndigits > 0 && (nan? || infinite?)
    raise FloatDomainError, to_s if nan? || infinite?
    return self if ndigits > 350
    return __oruby_dec_floor_ceil(ndigits, true) if ndigits > 0
    n, d = Kernel.__float_num_den(self)
    s = 10 ** (-ndigits)
    -((-n).div(d * s)) * s
  end

  alias __orig_flt_pow **
  def **(other)
    if self < 0 && Float === other && other.finite? && other != other.to_i
      return Complex(self, 0) ** other
    end
    if self < 0 && Rational === other && other.denominator != 1
      return Complex(self, 0) ** other
    end
    __orig_flt_pow(other)
  end

  alias __orig_flt_mod %
  def %(other)
    if self == 0.0 && (1.0 / self) < 0 && Numeric === other && other != 0 && !(Float === other && other.nan?)
      return -0.0
    end
    __orig_flt_mod(other)
  end
  alias modulo %

  alias __orig_flt_cmp <=>
  def <=>(other)
    if infinite? && !(Integer === other || Float === other) && other.respond_to?(:infinite?)
      oi = other.infinite?
      return infinite? <=> (oi || 0)
    end
    __orig_flt_cmp(other)
  end

  # Float#to_s and #inspect are US-ASCII (go-mrb.c's install_num_to_s)
end

class Integer
  # mruby's Integer.__ensure does not call #to_int on custom objects
  def self.__ensure(v)
    Kernel.__to_int(v)
  end

  def pred
    self - 1
  end

  def ord
    self
  end

  def numerator
    self
  end

  def denominator
    1
  end

  def zero?
    self == 0
  end

  def bit_count
    raise ArgumentError, "negative number" if self < 0
    to_s(2).count("1")
  end

  def rationalize(eps = (no_eps = true; nil))
    Rational(self, 1)
  end

  alias __orig_int_aref []
  def [](idx, len = (no_len = true; nil))
    if no_len && Range === idx && idx.begin.nil? && !idx.end.nil?
      e = Kernel.__to_int(idx.end)
      e -= 1 if idx.exclude_end?
      if e >= 0
        mask = (1 << (e + 1)) - 1
        if (self & mask) != 0
          raise ArgumentError, "The beginless range for Integer#[] results in infinity"
        end
        return 0
      end
      return 0
    end
    no_len ? __orig_int_aref(idx) : __orig_int_aref(idx, len)
  end

  # Integer#== is go-mrb.c's num_eq (MRI's num_equal), a builtin for the VM
  alias === ==

  alias __orig_int_cmp <=>
  def <=>(other)
    return __orig_int_cmp(other) if Integer === other || Float === other
    return nil unless other.respond_to?(:coerce)
    begin
      pair = other.coerce(self)
      return nil unless Array === pair && pair.size == 2
      pair[0] <=> pair[1]
    rescue TypeError
      nil
    end
  end

  alias __orig_int_coerce coerce
  def coerce(other)
    return [other, self] if Integer === other
    [Float(other), to_f]
  end

  alias __orig_int_and &
  def &(other)
    return __orig_int_and(other) if Integer === other
    raise TypeError, "Float can't be coerced into Integer" if Float === other
    if other.respond_to?(:coerce)
      a, b = other.coerce(self)
      return a & b
    end
    raise TypeError, "#{other.class} can't be coerced into Integer"
  end

  alias __orig_int_or |
  def |(other)
    return __orig_int_or(other) if Integer === other
    raise TypeError, "Float can't be coerced into Integer" if Float === other
    if other.respond_to?(:coerce)
      a, b = other.coerce(self)
      return a | b
    end
    raise TypeError, "#{other.class} can't be coerced into Integer"
  end

  alias __orig_int_xor ^
  def ^(other)
    return __orig_int_xor(other) if Integer === other
    raise TypeError, "Float can't be coerced into Integer" if Float === other
    if other.respond_to?(:coerce)
      a, b = other.coerce(self)
      return a ^ b
    end
    raise TypeError, "#{other.class} can't be coerced into Integer"
  end

  def allbits?(mask)
    m = Kernel.__to_int(mask)
    (self & m) == m
  end

  def anybits?(mask)
    (self & Kernel.__to_int(mask)) != 0
  end

  def nobits?(mask)
    (self & Kernel.__to_int(mask)) == 0
  end

  alias __orig_int_gcd gcd
  def gcd(other)
    raise TypeError, "not an integer" unless Integer === other
    __orig_int_gcd(other)
  end

  alias __orig_int_lcm lcm
  def lcm(other)
    raise TypeError, "not an integer" unless Integer === other
    __orig_int_lcm(other)
  end

  def gcdlcm(other)
    raise TypeError, "not an integer" unless Integer === other
    [__orig_int_gcd(other), __orig_int_lcm(other)]
  end

  alias __orig_int_digits digits
  def digits(base = 10)
    raise Math::DomainError, "out of domain" if self < 0
    __orig_int_digits(Kernel.__to_int(base))
  end

  class << self
    alias __orig_int_sqrt sqrt
    def sqrt(n)
      n = Kernel.__to_int(n)
      raise Math::DomainError, 'Numerical argument is out of domain - "isqrt"' if n < 0
      __orig_int_sqrt(n)
    end
  end

  alias __orig_int_div div
  def div(other)
    if Rational === other
      return (Rational(self, 1) / other).floor
    elsif Float === other
      raise ZeroDivisionError, "divided by 0" if other == 0.0
      return (self / other).floor
    end
    __orig_int_div(other)
  end

  alias __orig_int_fdiv fdiv
  def fdiv(other)
    if other == 0
      return Float::NAN if self == 0
      return self > 0 ? Float::INFINITY : -Float::INFINITY
    end
    if Integer === other && (bit_length > 53 || other.bit_length > 53)
      return Kernel.__bigint_div_to_f(self, other)
    end
    __orig_int_fdiv(other)
  end

  def round(ndigits = 0, half: nil)
    mode = Kernel.__check_half(half)
    ndigits = Kernel.__to_int(ndigits)
    raise RangeError, "integer #{ndigits} too big to convert to 'int'" if ndigits > 2147483647 || ndigits < -2147483648
    return self if ndigits >= 0
    f = 10 ** (-ndigits)
    neg = self < 0
    v = neg ? -self : self
    q, r = v.divmod(f)
    cmp = (r * 2) <=> f
    if cmp > 0 || (cmp == 0 && (mode == :up || (mode == :even && q.odd?)))
      q += 1
    end
    res = q * f
    neg ? -res : res
  end

  def floor(ndigits = 0)
    ndigits = Kernel.__to_int(ndigits)
    raise RangeError, "integer #{ndigits} too big to convert to 'int'" if ndigits > 2147483647 || ndigits < -2147483648
    return self if ndigits >= 0
    f = 10 ** (-ndigits)
    (self.div(f)) * f
  end

  def ceil(ndigits = 0)
    ndigits = Kernel.__to_int(ndigits)
    raise RangeError, "integer #{ndigits} too big to convert to 'int'" if ndigits > 2147483647 || ndigits < -2147483648
    return self if ndigits >= 0
    -((-self).floor(ndigits))
  end

  alias __orig_int_pow_m pow
  def pow(b, m = (no_m = true; nil))
    return self ** b if no_m
    __orig_int_pow_m(b, m)
  end

  alias __orig_int_times times
  def times(&block)
    return to_enum(:times) { self < 0 ? 0 : self } unless block
    __orig_int_times(&block)
  end

  alias __orig_int_upto upto
  def upto(limit, &block)
    unless block
      return to_enum(:upto, limit) do
        if !(Integer === limit)
          c = self <=> limit
          raise ArgumentError, "comparison of Integer with #{limit.class} failed" if c.nil?
          c > 0 ? 0 : (limit.floor - self + 1)
        else
          limit < self ? 0 : limit - self + 1
        end
      end
    end
    __orig_int_upto(limit, &block)
  end

  alias __orig_int_downto downto
  def downto(limit, &block)
    unless block
      return to_enum(:downto, limit) do
        if !(Integer === limit)
          c = self <=> limit
          raise ArgumentError, "comparison of Integer with #{limit.class} failed" if c.nil?
          c < 0 ? 0 : (self - limit.ceil + 1)
        else
          limit > self ? 0 : self - limit + 1
        end
      end
    end
    __orig_int_downto(limit, &block)
  end

  # Integer#to_s and #inspect are US-ASCII (go-mrb.c's install_num_to_s)

  alias __orig_int_chr chr
  def chr(enc = (no_enc = true; nil))
    if no_enc
      if self < 0
        msg = self < -0x80000000 ? "bignum out of char range" : "#{self} out of char range"
        raise RangeError, msg
      end
      if self < 128
        return __orig_int_chr.force_encoding(Encoding::US_ASCII)
      elsif self < 256
        return __orig_int_chr.force_encoding(Encoding::BINARY)
      elsif (di = Encoding.default_internal)
        enc = di
      else
        msg = self > 0x7fffffff ? "bignum out of char range" : "#{self} out of char range"
        raise RangeError, msg
      end
    end
    enc_obj =
      if Encoding === enc
        enc
      else
        Encoding.find(Kernel.__implicit(enc, String))
      end
    if self < 0
      msg = self < -0x80000000 ? "bignum out of char range" : "#{self} out of char range"
      raise RangeError, msg
    end
    if self > 0xffffffff
      raise RangeError, "bignum out of char range"
    end
    name = enc_obj.name
    case name
    when "US-ASCII"
      raise RangeError, "#{self} out of char range" if self > 0x7f
      [self].pack("C").force_encoding(enc_obj)
    when "ASCII-8BIT"
      raise RangeError, "#{self} out of char range" if self > 0xff
      [self].pack("C").force_encoding(enc_obj)
    when "UTF-8"
      if self > 0x10ffff || (self >= 0xd800 && self <= 0xdfff)
        raise RangeError, "#{self} out of char range"
      end
      [self].pack("U").force_encoding(enc_obj)
    when "CESU-8"
      if self > 0x10ffff || (self >= 0xd800 && self <= 0xdfff)
        raise RangeError, "#{self} out of char range"
      end
      if self <= 0xffff
        [self].pack("U").force_encoding(enc_obj)
      else
        u = self - 0x10000
        hi = 0xd800 + (u >> 10)
        lo = 0xdc00 + (u & 0x3ff)
        bytes = [
          0xe0 | (hi >> 12), 0x80 | ((hi >> 6) & 0x3f), 0x80 | (hi & 0x3f),
          0xe0 | (lo >> 12), 0x80 | ((lo >> 6) & 0x3f), 0x80 | (lo & 0x3f)
        ]
        bytes.pack("C*").force_encoding(enc_obj)
      end
    when "Shift_JIS", "Windows-31J"
      if self <= 0x7f || (self >= 0xa1 && self <= 0xdf)
        [self].pack("C").force_encoding(enc_obj)
      elsif self >= 0x8140 && self <= 0xfcfc
        hi = self >> 8
        lo = self & 0xff
        valid_hi = (hi >= 0x81 && hi <= 0x9f) || (hi >= 0xe0 && hi <= 0xfc)
        valid_lo = (lo >= 0x40 && lo <= 0x7e) || (lo >= 0x80 && lo <= 0xfc)
        raise RangeError, "#{self} out of char range" unless valid_hi && valid_lo
        [hi, lo].pack("CC").force_encoding(enc_obj)
      else
        raise RangeError, "#{self} out of char range"
      end
    when "EUC-JP"
      if self <= 0x7f
        [self].pack("C").force_encoding(enc_obj)
      elsif self >= 0x8ea1 && self <= 0x8edf
        [self >> 8, self & 0xff].pack("CC").force_encoding(enc_obj)
      elsif self >= 0xa1a1 && self <= 0xfefe
        hi = self >> 8
        lo = self & 0xff
        raise RangeError, "#{self} out of char range" unless hi >= 0xa1 && hi <= 0xfe && lo >= 0xa1 && lo <= 0xfe
        [hi, lo].pack("CC").force_encoding(enc_obj)
      elsif self >= 0x8fa1a1 && self <= 0x8ffefe
        b1 = (self >> 8) & 0xff
        b2 = self & 0xff
        raise RangeError, "#{self} out of char range" unless b1 >= 0xa1 && b1 <= 0xfe && b2 >= 0xa1 && b2 <= 0xfe
        [0x8f, b1, b2].pack("CCC").force_encoding(enc_obj)
      else
        raise RangeError, "#{self} out of char range"
      end
    when "UTF-16", "UTF-16BE", "UTF-16LE"
      if self > 0x10ffff || (self >= 0xd800 && self <= 0xdfff)
        raise RangeError, "#{self} out of char range"
      end
      raise RangeError, "#{self} out of char range" if name == "UTF-16"
      fmt = name == "UTF-16BE" ? "n*" : "v*"
      if self <= 0xffff
        [self].pack(fmt).force_encoding(enc_obj)
      else
        u = self - 0x10000
        [0xd800 + (u >> 10), 0xdc00 + (u & 0x3ff)].pack(fmt).force_encoding(enc_obj)
      end
    when "UTF-32", "UTF-32BE", "UTF-32LE"
      if self > 0x10ffff || (self >= 0xd800 && self <= 0xdfff)
        raise RangeError, "#{self} out of char range"
      end
      raise RangeError, "#{self} out of char range" if name == "UTF-32"
      [self].pack(name == "UTF-32BE" ? "N" : "V").force_encoding(enc_obj)
    else
      raise RangeError, "#{self} out of char range" if self > 0xff
      [self].pack("C").force_encoding(enc_obj)
    end
  end
end

# Patch Integer#** for negative ** fractional Float/Rational
class Integer
  alias __oruby_pow2 **
  def **(other)
    if self < 0 && Float === other && other.finite? && other != other.to_i
      return Complex(self, 0) ** other
    end
    if self < 0 && Rational === other && other.denominator != 1
      return Complex(self, 0) ** other
    end
    __oruby_pow2(other)
  end
end

class Complex
  undef_method :positive? if method_defined?(:positive?)
  undef_method :negative? if method_defined?(:negative?)

  class << self
    alias __orig_cplx_rect rectangular
    def rectangular(r, i = 0)
      if Complex === r
        raise TypeError, "not a real" unless r.imag == 0
        r = r.real
      elsif !(Numeric === r && r.real?)
        raise TypeError, "not a real"
      end
      if Complex === i
        raise TypeError, "not a real" unless i.imag == 0
        i = i.real
      elsif !(Numeric === i && i.real?)
        raise TypeError, "not a real"
      end
      if Kernel.__cplx_builtin?(r) && Kernel.__cplx_builtin?(i)
        __orig_cplx_rect(r, i)
      else
        __oruby_new__(r, i)
      end
    end
    alias rect rectangular
  end

  def coerce(other)
    if Complex === other
      [other, self]
    elsif Numeric === other && other.real?
      [Complex.rectangular(other, 0), self]
    else
      raise TypeError, "#{other.class} can't be coerced into Complex"
    end
  end

  def denominator
    real.denominator.lcm(imag.denominator)
  end

  def numerator
    d = denominator
    Complex.rectangular(real.numerator * (d / real.denominator), imag.numerator * (d / imag.denominator))
  end

  def finite?
    real.finite? && imag.finite?
  end

  def infinite?
    (real.infinite? || imag.infinite?) ? 1 : nil
  end

  alias __orig_cplx_uminus -@
  def -@
    r, i = real, imag
    if Kernel.__cplx_builtin?(r) && Kernel.__cplx_builtin?(i)
      __orig_cplx_uminus
    else
      Complex.rectangular(-r, -i)
    end
  end

  alias __orig_cplx_eq ==
  def ==(other)
    r, i = real, imag
    if Kernel.__cplx_builtin?(r) && Kernel.__cplx_builtin?(i)
      if Complex === other
        or_, oi = other.real, other.imag
        if Kernel.__cplx_builtin?(or_) && Kernel.__cplx_builtin?(oi)
          return __orig_cplx_eq(other)
        end
        return r == or_ && i == oi
      elsif Kernel.__cplx_builtin?(other)
        return __orig_cplx_eq(other)
      end
    end
    if Complex === other
      r == other.real && i == other.imag
    elsif Numeric === other && other.real?
      i == 0 && r == other
    else
      other == self
    end
  end

  alias __orig_cplx_eql eql?
  def eql?(other)
    return false unless Complex === other
    r, i = real, imag
    or_, oi = other.real, other.imag
    r.class == or_.class && i.class == oi.class && r == or_ && i == oi
  end

  alias __orig_cplx_to_s to_s
  def to_s
    r, i = real, imag
    return __orig_cplx_to_s if Kernel.__cplx_builtin?(r) && Kernel.__cplx_builtin?(i)
    rs = r.to_s
    is = i.to_s
    neg_i = (i < 0) rescue false
    sign = neg_i || is.start_with?("-") ? "" : "+"
    suffix = (!is.empty? && is[-1] >= "0" && is[-1] <= "9") ? "i" : "*i"
    "#{rs}#{sign}#{is}#{suffix}"
  end

  alias __orig_cplx_inspect inspect
  def inspect
    r, i = real, imag
    return __orig_cplx_inspect if Kernel.__cplx_builtin?(r) && Kernel.__cplx_builtin?(i)
    rs = r.inspect
    is = i.inspect
    neg_i = (i < 0) rescue false
    sign = neg_i || is.start_with?("-") ? "" : "+"
    suffix = (!is.empty? && is[-1] >= "0" && is[-1] <= "9") ? "i" : "*i"
    "(#{rs}#{sign}#{is}#{suffix})"
  end

  def rationalize(eps = (no_eps = true; nil))
    raise RangeError, "can't convert #{self} into Rational" unless imag == 0 && !(Float === imag)
    no_eps ? real.rationalize : real.rationalize(eps)
  end

  alias __orig_cplx_to_f to_f
  def to_f
    raise RangeError, "can't convert #{self} into Float" unless imag == 0 && !(Float === imag)
    real.to_f
  end

  alias __orig_cplx_to_i to_i
  def to_i
    raise RangeError, "can't convert #{self} into Integer" unless imag == 0 && !(Float === imag)
    real.to_i
  end

  alias __orig_cplx_to_r to_r
  def to_r
    raise RangeError, "can't convert #{self} into Rational" unless imag == 0
    real.to_r
  end

  private def marshal_dump
    [real, imag]
  end

  %i[+ - * / **].each do |op|
    orig = :"__orig_cplx_#{op}"
    alias_method orig, op
    define_method(op) do |other|
      r, i = real, imag
      if op == :/ && Float === other && other == 0.0
        return Complex.rectangular(r.fdiv(0.0), i.fdiv(0.0))
      end
      builtin_self = Kernel.__cplx_builtin?(r) && Kernel.__cplx_builtin?(i)
      if builtin_self && (Kernel.__cplx_builtin?(other) || (Complex === other && Kernel.__cplx_builtin?(other.real) && Kernel.__cplx_builtin?(other.imag)))
        if op == :/ && Complex === other
          or_, oi = other.real, other.imag
          if !(Float === r || Float === i || Float === or_ || Float === oi)
            denom = or_ * or_ + oi * oi
            raise ZeroDivisionError, "divided by 0" if denom == 0
            nr = (r * or_ + i * oi).quo(denom)
            ni = (i * or_ - r * oi).quo(denom)
            return Complex.rectangular(nr, ni)
          end
        end
        return __send__(orig, other)
      end
      if Complex === other
        or_, oi = other.real, other.imag
        case op
        when :+ then return Complex.rectangular(r + or_, i + oi)
        when :- then return Complex.rectangular(r - or_, i - oi)
        when :* then return Complex.rectangular(r * or_ - i * oi, r * oi + i * or_)
        when :/
          denom = or_ * or_ + oi * oi
          raise ZeroDivisionError, "divided by 0" if denom == 0
          return Complex.rectangular((r * or_ + i * oi).quo(denom), (i * or_ - r * oi).quo(denom))
        end
      end
      if Numeric === other && other.real?
        case op
        when :+ then return Complex.rectangular(r + other, i)
        when :- then return Complex.rectangular(r - other, i)
        when :* then return Complex.rectangular(r * other, i * other)
        when :/ then return Complex.rectangular(r.quo(other), i.quo(other))
        end
      end
      if other.respond_to?(:coerce)
        a, b = other.coerce(self)
        return op == :/ && Numeric === other ? a.quo(b) : a.__send__(op, b)
      end
      raise TypeError, "#{other.class} can't be coerced into Complex"
    end
  end
  alias quo /
end

# Also let Kernel#Complex accept custom real Numerics without forcing Float
module Kernel
  alias __orig_kernel_Complex Complex
  def Complex(*args, exception: true)
    if args.size == 1 && Numeric === args[0] && !Kernel.__cplx_builtin?(args[0]) && !(Complex === args[0]) && args[0].real?
      return Complex.rectangular(args[0], 0)
    elsif args.size == 2 && Numeric === args[0] && Numeric === args[1] &&
          (!Kernel.__cplx_builtin?(args[0]) || !Kernel.__cplx_builtin?(args[1])) &&
          !(Complex === args[0]) && !(Complex === args[1]) && args[0].real? && args[1].real?
      return Complex.rectangular(args[0], args[1])
    end
    __orig_kernel_Complex(*args, exception: exception)
  end
  module_function :Complex
end

class Rational
  private def marshal_dump
    [numerator, denominator]
  end

  def rationalize(eps = (no_eps = true; nil))
    return self if no_eps
    en, ed = Kernel.__num_den_of(eps)
    Kernel.__rationalize_int(numerator, denominator, en, ed)
  end

  alias __orig_rat_to_f to_f
  def to_f
    n = numerator
    d = denominator
    if n.bit_length > 53 || d.bit_length > 53
      return Kernel.__bigint_div_to_f(n, d)
    end
    __orig_rat_to_f
  end

  def __exact_decimal_places
    d = denominator
    p2 = 0
    while (d & 1) == 0
      d >>= 1
      p2 += 1
    end
    p5 = 0
    while (d % 5) == 0
      d /= 5
      p5 += 1
    end
    d == 1 ? (p2 > p5 ? p2 : p5) : nil
  end

  def floor(ndigits = 0)
    raise TypeError, "not an integer" unless Integer === ndigits
    return numerator.div(denominator) if ndigits == 0
    if ndigits > 0
      ep = __exact_decimal_places
      return self if ep && ndigits >= ep
      s = 10 ** ndigits
      return Rational((numerator * s).div(denominator), s)
    end
    s = 10 ** (-ndigits)
    numerator.div(denominator * s) * s
  end

  def ceil(ndigits = 0)
    raise TypeError, "not an integer" unless Integer === ndigits
    return -((-numerator).div(denominator)) if ndigits == 0
    if ndigits > 0
      ep = __exact_decimal_places
      return self if ep && ndigits >= ep
    end
    -((-self).floor(ndigits))
  end

  def truncate(ndigits = 0)
    raise TypeError, "not an integer" unless Integer === ndigits
    negative? ? ceil(ndigits) : floor(ndigits)
  end

  def round(ndigits = 0, half: nil)
    mode = Kernel.__check_half(half)
    raise TypeError, "not an integer" unless Integer === ndigits
    if ndigits > 0
      ep = __exact_decimal_places
      return self if ep && ndigits >= ep
      s = 10 ** ndigits
      n, d = (numerator * s).abs, denominator
      q, r = (n * 2 + d).divmod(d * 2)
      if r == 0
        case mode
        when :even then q -= 1 if q.odd?
        when :down then q -= 1
        end
      end
      return Rational(negative? ? -q : q, s)
    elsif ndigits < 0
      s = 10 ** (-ndigits)
      n, d = numerator.abs, denominator * s
      q, r = (n * 2 + d).divmod(d * 2)
      if r == 0
        case mode
        when :even then q -= 1 if q.odd?
        when :down then q -= 1
        end
      end
      return (negative? ? -q : q) * s
    end
    n, d = numerator.abs, denominator
    q, r = (n * 2 + d).divmod(d * 2)
    if r == 0
      case mode
      when :even then q -= 1 if q.odd?
      when :down then q -= 1
      end
    end
    negative? ? -q : q
  end

  def div(other)
    raise ZeroDivisionError, "divided by 0" if other == 0
    if Integer === other
      return numerator.div(denominator * other)
    elsif Rational === other
      return (numerator * other.denominator).div(denominator * other.numerator)
    end
    (self / other).floor
  end

  def %(other)
    raise ZeroDivisionError, "divided by 0" if other == 0
    divmod(other)[1]
  end
  alias modulo %

  def divmod(other)
    raise ZeroDivisionError, "divided by 0" if other == 0
    if Integer === other
      a = numerator
      b = denominator
      q = a.div(b * other)
      rem_n = a - q * b * other
      g = rem_n.gcd(b)
      return [q, Rational(rem_n / g, b / g)]
    elsif Rational === other
      a, b = numerator, denominator
      c, d = other.numerator, other.denominator
      q = (a * d).div(b * c)
      rem_n = a * d - q * b * c
      rem_d = b * d
      g = rem_n.gcd(rem_d)
      return [q, Rational(rem_n / g, rem_d / g)]
    end
    q = (self / other).floor
    [q, self - other * q]
  end

  %i[+ - * / **].each do |op|
    orig = :"__orig_rat_#{op}"
    alias_method orig, op
    define_method(op) do |other|
      if Float === other
        return __send__(orig, other) if op == :** && self >= 0
        if op == :**
          if self < 0 && other.finite? && other != other.to_i
            return Complex(self, 0) ** other
          end
          return self.to_f ** other
        end
        return self.to_f.__send__(op, other)
      end
      if op == :**
        if Integer === other
          if self == 0
            raise ZeroDivisionError, "divided by 0" if other < 0
            return Rational(1, 1) if other == 0
            return Rational(0, 1)
          end
          return Rational(1, 1) if self == 1 || other == 0
          return Rational(other.even? ? 1 : -1, 1) if self == -1
          if other > 0x7fff_ffff || other < -0x8000_0000
            raise ArgumentError, "exponent is too large"
          end
          return __send__(orig, other)
        elsif Rational === other
          if self == 0
            raise ZeroDivisionError, "divided by 0" if other < 0
            return Rational(1, 1) if other == 0
            return Rational(0, 1)
          end
          return Rational(1, 1) if other == 0
          if other.denominator == 1
            return self ** other.numerator
          end
          if self < 0
            return Complex(self, 0) ** other
          end
          return self.to_f ** other.to_f
        end
      end
      if (op == :+ || op == :-) && Rational === other
        begin
          return __send__(orig, other)
        rescue RangeError
          a = self.numerator
          b = self.denominator
          c = other.numerator
          d = other.denominator
          g = b.gcd(d)
          bd = b / g
          dd = d / g
          num = op == :+ ? (a * dd + c * bd) : (a * dd - c * bd)
          den = bd * d
          g2 = num.gcd(den)
          return Rational(num / g2, den / g2)
        end
      end
      if Integer === other || Rational === other || Complex === other
        return __send__(orig, other)
      end
      if other.respond_to?(:coerce)
        a, b = other.coerce(self)
        return op == :/ && Numeric === other ? a.quo(b) : a.__send__(op, b)
      end
      raise TypeError, "#{other.class} can't be coerced into Rational"
    end
  end
  alias quo /

  alias __orig_rat_cmp <=>
  def <=>(other)
    if Integer === other || Float === other || Rational === other
      return __orig_rat_cmp(other)
    end
    return nil unless other.respond_to?(:coerce)
    a, b = other.coerce(self)
    a <=> b
  end
end
