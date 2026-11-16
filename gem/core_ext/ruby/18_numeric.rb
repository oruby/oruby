# Numeric#coerce, as in MRI (mruby-coerce defines it on Integer and Float only)
class Numeric
  def coerce(other)
    return [other, self] if other.instance_of?(self.class)
    [Float(other), Float(self)]
  end
end

# Integer#** with a negative Integer or a Rational exponent, as in MRI:
# 2 ** -2 is (1/4), 4 ** (1/2r) is 2.0
class Integer
  alias __pow **
  def **(other)
    if other.is_a?(Integer) && other < 0
      return 1 if self == 1
      return other.even? ? 1 : -1 if self == -1
      raise ZeroDivisionError, "divided by 0" if self == 0
      Rational(1, __pow(-other))
    elsif other.is_a?(Rational)
      return self ** other.numerator if other.denominator == 1
      to_f ** other.to_f
    else
      __pow(other)
    end
  end
end
