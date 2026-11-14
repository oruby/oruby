class Float
  alias __orig_eq ==
  def ==(other)
    return true if equal?(other)
    if other.is_a?(Float) || other.is_a?(Integer)
      __orig_eq(other)
    elsif other.respond_to?(:==)
      other == self
    else
      false
    end
  end
end

class Range
  def step(n = 1, &block)
    raise ArgumentError, "step can't be 0" if n == 0
    if block_given?
      cur = self.begin
      last = self.end
      excl = self.exclude_end?
      if n > 0
        while last.nil? || (excl ? cur < last : cur <= last)
          yield cur
          cur += n
        end
      else
        while last.nil? || (excl ? cur > last : cur >= last)
          yield cur
          cur += n
        end
      end
      self
    else
      Enumerator::ArithmeticSequence.new(self, n)
    end
  end
  alias % step
end
