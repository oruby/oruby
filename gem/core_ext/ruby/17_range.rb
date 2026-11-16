# Range#bsearch (find-minimum and find-any modes), as in MRI
class Range
  unless method_defined?(:bsearch)
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
end
