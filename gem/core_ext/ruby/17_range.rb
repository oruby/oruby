# Range#bsearch (find-minimum and find-any modes), as in MRI
class Range
  def bsearch(&block)
    return to_enum(:bsearch) unless block
    b = self.begin
    e = self.end
    if (b.nil? || b.is_a?(Integer)) && (e.nil? || e.is_a?(Integer))
      __bsearch_integer(b, e, &block)
    elsif (b.nil? || b.is_a?(Numeric)) && (e.nil? || e.is_a?(Numeric))
      __bsearch_float((b || -Float::INFINITY).to_f, (e || Float::INFINITY).to_f, exclude_end?, &block)
    else
      raise TypeError, "can't do binary search for #{(b || e).class}"
    end
  end

  private

  # returns [found?, go_left?] for a block result at x
  def __bsearch_check(v)
    case v
    when true then [true, true]
    when false, nil then [false, false]
    when Numeric
      return [true, nil] if v == 0
      [false, v < 0]
    else
      raise TypeError, "wrong argument type #{v.class} (must be numeric, true, false or nil)"
    end
  end

  def __bsearch_integer(lo, hi, &block)
    if lo.nil?
      # beginless: probe left until the block is satisfied on the left side
      step = 1
      hi_v = hi.nil? ? 0 : (exclude_end? ? hi - 1 : hi)
      lo = hi_v - step
      loop do
        found, left = __bsearch_check(yield(lo))
        break unless found || left
        step *= 2
        lo = hi_v - step
      end
    end
    if hi.nil?
      step = 1
      hi = lo + step
      loop do
        found, left = __bsearch_check(yield(hi))
        break if found || left
        step *= 2
        hi = lo + step
      end
    elsif exclude_end?
      hi -= 1
    end
    return nil if lo > hi
    result = nil
    while lo <= hi
      mid = lo + (hi - lo) / 2
      found, left = __bsearch_check(yield(mid))
      if found && left.nil?
        return mid # find-any mode hit
      elsif found
        result = mid
        hi = mid - 1
      elsif left
        hi = mid - 1
      else
        lo = mid + 1
      end
    end
    result
  end

  def __bsearch_float(lo, hi, excl, &block)
    return nil if lo > hi || (excl && lo == hi)
    result = nil
    200.times do
      break if lo > hi
      mid = if lo.infinite? || hi.infinite?
              lo.infinite? && hi.infinite? ? 0.0 : (lo.infinite? ? (hi > 0 ? -hi : hi * 2 - 1) : (lo < 0 ? -lo : lo * 2 + 1))
            else
              lo + (hi - lo) / 2.0
            end
      found, left = __bsearch_check(yield(mid))
      if found && left.nil?
        return mid
      elsif found
        result = mid
        break if hi == mid
        hi = mid
      elsif left
        break if hi == mid
        hi = mid
      else
        break if lo == mid
        lo = mid.next_float
      end
    end
    if result.nil? && !excl && hi.finite?
      found, left = __bsearch_check(yield(hi))
      result = hi if found
    end
    result
  end
end

# min, max, minmax, count and to_set as MRI's range.c has them: endpoints
# are compared, not iterated, and open ends raise instead of looping
class Range
  def __oruby_cmp(a, b)
    c = a <=> b
    raise ArgumentError, "comparison of #{a.class} with #{b.inspect} failed" if c.nil?
    c > 0 ? 1 : (c < 0 ? -1 : 0)
  end
  private :__oruby_cmp

  def min(*args, &block)
    b, e = self.begin, self.end
    raise RangeError, "cannot get the minimum of beginless range" if b.nil?
    if block
      raise RangeError, "cannot get the minimum of endless range with custom comparison method" if e.nil?
      return super
    end
    return first(*args) unless args.empty?
    c = e.nil? ? -1 : __oruby_cmp(b, e)
    return nil if c > 0 || (c == 0 && exclude_end?)
    b
  end

  def max(*args, &block)
    b, e = self.begin, self.end
    raise RangeError, "cannot get the maximum of endless range" if e.nil?
    numeric_end = e.is_a?(Numeric)
    if !args.empty? && !block && e.is_a?(Integer) && (b.nil? || b.is_a?(Integer))
      # the n largest of an Integer range, counted down from the end
      n = Kernel.__to_int(args[0])
      raise ArgumentError, "negative size (#{n})" if n < 0
      top = exclude_end? ? e - 1 : e
      res = []
      v = top
      while res.size < n && (b.nil? || v >= b)
        res << v
        v -= 1
      end
      return res
    end
    if block || (exclude_end? && !numeric_end) || !args.empty?
      raise RangeError, "cannot get the maximum of beginless range with custom comparison method" if b.nil?
      return super
    end
    c = b.nil? ? -1 : __oruby_cmp(b, e)
    return nil if c > 0
    if exclude_end?
      raise TypeError, "cannot exclude non Integer end value" unless e.is_a?(Integer)
      return nil if c == 0
      raise TypeError, "cannot exclude end value with non Integer begin value" unless b.nil? || b.is_a?(Integer)
      return e - 1
    end
    e
  end

  def minmax(&block)
    return super if block
    [min, max]
  end

  def count(*args, &block)
    return super if !args.empty? || block
    b, e = self.begin, self.end
    return Float::INFINITY if b.nil? || e.nil?
    if b.is_a?(Integer)
      s = size
      return s unless s.nil?
    end
    super
  end

  def to_set(&block)
    raise RangeError, "cannot convert endless range to a set" if self.end.nil?
    super(&block)
  end
end

class Range
  alias __oruby_each each
  # MRI names the class it can't iterate from
  def each(&block)
    b = self.begin
    unless b.is_a?(Integer) || b.respond_to?(:succ) || !block
      raise TypeError, "can't iterate from #{b.class}"
    end
    __oruby_each(&block)
  end
end

# Range#cover? as MRI's range_cover: a value is between the boundaries
# (r_cover_p); a Range is covered when its begin is and its end does not
# reach past self's (r_cover_range_p, with max for an inclusive other
# range against an exclusive self). Comparisons go through <=> (r_less):
# nil there means not comparable.
class Range
  def cover?(val)
    b = self.begin
    e = self.end
    return __oruby_cover_range?(b, e, val) if Range === val
    __oruby_cover?(b, e, val)
  end

  # MRI's range_eqq: cover? of a value (a Range argument is a value here)
  def ===(val)
    __oruby_cover?(self.begin, self.end, val)
  end

  # MRI's range_include: numeric (and Time) boundaries compare, String ones
  # walk String#upto (one ASCII character each: a byte compare), anything
  # else iterates (Enumerable#include?)
  def include?(val)
    b = self.begin
    e = self.end
    return true if b.nil? && e.nil? && __oruby_linear?(val)
    if __oruby_linear?(b) || __oruby_linear?(e) || b.respond_to?(:to_int) || e.respond_to?(:to_int)
      return __oruby_cover?(b, e, val)
    end
    if String === b || String === e
      raise TypeError, "cannot determine inclusion in beginless/endless ranges" unless String === b && String === e
      return false if val.nil?
      unless String === val
        return false unless val.respond_to?(:to_str)
        v = val.to_str
        raise TypeError, "can't convert #{val.class} to String (#{val.class}#to_str gives #{v.class})" unless String === v
        val = v
      end
      if b.bytesize == 1 && e.bytesize == 1 && b.ascii_only? && e.ascii_only?
        return false unless val.bytesize == 1
        if val.ascii_only?
          return (b <= val && val < e) || (!exclude_end? && val == e)
        end
      end
      b.upto(e, exclude_end?) { |x| return true if x == val }
      return false
    end
    raise TypeError, "cannot determine inclusion in beginless/endless ranges" if b.nil? || e.nil?
    super
  end
  alias member? include?

  private

  def __oruby_linear?(x)
    Numeric === x || (defined?(::Time) && ::Time === x)
  end

  # MRI's r_less: < 0, 0, > 0 (not comparable counts as greater)
  def __oruby_r_less(a, b)
    r = a <=> b
    return 0x7fffffff if r.nil?
    return r <=> 0 if Integer === r
    r > 0 ? 1 : (r < 0 ? -1 : 0)
  end

  def __oruby_cover?(b, e, val)
    if b.nil? || __oruby_r_less(b, val) <= 0
      return true if e.nil? || __oruby_r_less(val, e) <= (exclude_end? ? -1 : 0)
    end
    false
  end

  def __oruby_cover_range?(b, e, val)
    vb = val.begin
    ve = val.end
    return false if !e.nil? && ve.nil?
    return false if !b.nil? && vb.nil?
    return false if !vb.nil? && !ve.nil? && __oruby_r_less(vb, ve) > (val.exclude_end? ? -1 : 0)
    return false if !vb.nil? && !__oruby_cover?(b, e, vb)
    cmp_end = __oruby_r_less(e, ve)
    if exclude_end? == val.exclude_end?
      cmp_end >= 0
    elsif exclude_end?
      cmp_end > 0
    elsif cmp_end >= 0
      true
    else
      # (1..5).cover?(1...6) when the last element of the other is 5
      vmax = begin
        val.max
      rescue TypeError
        nil
      end
      !vmax.nil? && __oruby_r_less(e, vmax) >= 0
    end
  end
end
