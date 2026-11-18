class Rational
  alias to_int to_i

  def floor(ndigits = nil)
    return numerator.div(denominator) if ndigits.nil? || ndigits == 0
    s = 10**ndigits.abs
    if ndigits > 0
      Rational((self * s).floor, s)
    else
      (self / s).floor * s
    end
  end

  def ceil(ndigits = nil)
    return -((-numerator).div(denominator)) if ndigits.nil? || ndigits == 0
    -((-self).floor(ndigits))
  end

  def truncate(ndigits = nil)
    negative? ? ceil(ndigits) : floor(ndigits)
  end

  def round(ndigits = nil, half: :up)
    if ndigits && ndigits != 0
      s = 10**ndigits.abs
      return ndigits > 0 ? Rational((self * s).round(half: half), s) : (self / s).round(half: half) * s
    end
    n, d = numerator.abs, denominator
    q, r = (n * 2 + d).divmod(d * 2)
    if r == 0 # exactly half
      case half
      when :even, "even" then q -= 1 if q.odd?
      when :down, "down" then q -= 1
      end
    end
    negative? ? -q : q
  end

  def divmod(other)
    q = (self / other).floor
    [q, self - other * q]
  end

  def div(other) = (self / other).floor
  def %(other) = self - other * (self / other).floor
  alias modulo %
  def remainder(other) = self - other * (self / other).truncate
  def abs = negative? ? -self : self
  alias magnitude abs
  def zero? = numerator == 0
  def positive? = numerator > 0
  def integer? = false
  def finite? = true
  def infinite? = nil
  def fdiv(other) = to_f / other
  def rationalize(eps = nil) = self
  def eql?(other) = other.is_a?(Rational) && self == other
  def coerce(other)
    case other
    when Integer then [Rational(other, 1), self]
    when Float then [other, to_f]
    else raise TypeError, "#{other.class} can't be coerced into Rational"
    end
  end
end
