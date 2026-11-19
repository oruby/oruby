class Float
  alias to_int to_i
  # Float#== is go-mrb.c's num_eq (MRI's num_equal), a builtin for the VM
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
