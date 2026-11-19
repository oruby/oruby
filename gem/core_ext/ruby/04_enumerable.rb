# Helpers for specs
$VERBOSE = false if $VERBOSE.nil?

module Enumerable
  alias __orig_all? all?
  def all?(*args, &block)
    warn "given block not used", uplevel: 1 if !args.empty? && block
    __orig_all?(*args, &block)
  end

  alias __orig_any? any?
  def any?(*args, &block)
    warn "given block not used", uplevel: 1 if !args.empty? && block
    __orig_any?(*args, &block)
  end

  alias __orig_none? none?
  def none?(*args, &block)
    warn "given block not used", uplevel: 1 if !args.empty? && block
    __orig_none?(*args, &block)
  end

  alias __orig_one? one?
  def one?(*args, &block)
    warn "given block not used", uplevel: 1 if !args.empty? && block
    __orig_one?(*args, &block)
  end

  alias __orig_find find
  def find(*args, &block)
    if block_given?
      __orig_find(*args, &block)
    else
      to_enum(:find, *args) { nil }
    end
  end
  alias detect find
end

# max(n), min(n), max_by(n), min_by(n): the n largest or smallest, as an
# Array (largest first for max, smallest first for min)
module Enumerable
  def __oruby_nsel(items, n, desc, block)
    n = Kernel.__to_int(n)
    raise ArgumentError, "negative size (#{n})" if n < 0
    return [] if n == 0
    cmp = lambda do |a, b|
      c = block ? block.call(a, b) : a <=> b
      raise ArgumentError, "comparison of #{a.class} with #{b.class} failed" if c.nil?
      desc ? -c : c
    end
    items.sort { |a, b| cmp.call(a, b) }.first(n)
  end

  def __oruby_nsel_by(items, n, desc, block)
    n = Kernel.__to_int(n)
    raise ArgumentError, "negative size (#{n})" if n < 0
    pairs = items.map { |e| [block.call(e), e] }
    __oruby_nsel(pairs, n, desc, lambda { |a, b| a[0] <=> b[0] }).map { |pr| pr[1] }
  end
  private :__oruby_nsel, :__oruby_nsel_by

  alias __oruby_max max
  def max(n = (no = true; nil), &block)
    return __oruby_max(&block) if no
    __oruby_nsel(to_a, n, true, block)
  end

  alias __oruby_min min
  def min(n = (no = true; nil), &block)
    return __oruby_min(&block) if no
    __oruby_nsel(to_a, n, false, block)
  end

  alias __oruby_max_by max_by
  def max_by(n = (no = true; nil), &block)
    return to_enum(:max_by, *(no ? [] : [n])) { respond_to?(:size) ? size : nil } unless block
    return __oruby_max_by(&block) if no
    __oruby_nsel_by(to_a, n, true, block)
  end

  alias __oruby_min_by min_by
  def min_by(n = (no = true; nil), &block)
    return to_enum(:min_by, *(no ? [] : [n])) { respond_to?(:size) ? size : nil } unless block
    return __oruby_min_by(&block) if no
    __oruby_nsel_by(to_a, n, false, block)
  end
end

class Array
  alias __oruby_ary_max max
  def max(n = (no = true; nil), &block)
    return __oruby_ary_max(&block) if no
    __oruby_nsel(self, n, true, block)
  end

  alias __oruby_ary_min min
  def min(n = (no = true; nil), &block)
    return __oruby_ary_min(&block) if no
    __oruby_nsel(self, n, false, block)
  end
end
