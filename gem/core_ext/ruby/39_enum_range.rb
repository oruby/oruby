# Enumerable and Range MRI compatibility extensions

module Enumerable
  def compact
    out = []
    each do |*x|
      v = x.__svalue
      out << v unless v.nil?
    end
    out
  end

  alias __orig_enum_count count
  def count(*args, &block)
    warn("given block not used", uplevel: 1) if !args.empty? && block
    __orig_enum_count(*args, &block)
  end

  alias __orig_enum_chunk_while chunk_while
  def chunk_while(&block)
    raise ArgumentError, "tried to create Proc object without a block" unless block
    __orig_enum_chunk_while(&block)
  end

  def slice_before(*args, &block)
    if block
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0)" unless args.empty?
      Enumerator.new do |y|
        chunk = nil
        each_entry do |elem|
          if block.call(elem)
            y << chunk if chunk
            chunk = [elem]
          else
            (chunk ||= []) << elem
          end
        end
        y << chunk if chunk
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1)" unless args.size == 1
      pat = args[0]
      Enumerator.new do |y|
        chunk = nil
        each_entry do |elem|
          if pat === elem
            y << chunk if chunk
            chunk = [elem]
          else
            (chunk ||= []) << elem
          end
        end
        y << chunk if chunk
      end
    end
  end

  def slice_after(*args, &block)
    if block
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0)" unless args.empty?
      Enumerator.new do |y|
        chunk = nil
        each_entry do |elem|
          (chunk ||= []) << elem
          if block.call(elem)
            y << chunk
            chunk = nil
          end
        end
        y << chunk if chunk
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1)" unless args.size == 1
      pat = args[0]
      Enumerator.new do |y|
        chunk = nil
        each_entry do |elem|
          (chunk ||= []) << elem
          if pat === elem
            y << chunk
            chunk = nil
          end
        end
        y << chunk if chunk
      end
    end
  end

  def slice_when(&block)
    raise ArgumentError, "tried to create Proc object without a block" unless block
    Enumerator.new do |y|
      chunk = nil
      prev = nil
      has_prev = false
      each_entry do |elem|
        if has_prev
          if block.call(prev, elem)
            y << chunk
            chunk = [elem]
          else
            chunk << elem
          end
        else
          chunk = [elem]
          has_prev = true
        end
        prev = elem
      end
      y << chunk if chunk
    end
  end

  def tally(hash = (no_hash = true; nil))
    if no_hash
      h = {}
    else
      h = Kernel.__implicit(hash, Hash)
      raise FrozenError, "can't modify frozen #{h.class}: #{h.inspect}" if h.frozen?
    end
    each_entry do |v|
      h[v] = h.key?(v) ? h[v] + 1 : 1
    end
    h
  end

  def uniq(&block)
    buckets = {}
    out = []
    if block
      each do |*x|
        v = x.__svalue
        k = yield(*x)
        h = k.hash
        list = buckets[h]
        if list.nil?
          buckets[h] = [k]
          out << v
        elsif !list.any? { |e| k.eql?(e) }
          list << k
          out << v
        end
      end
    else
      each do |*x|
        v = x.__svalue
        h = v.hash
        list = buckets[h]
        if list.nil?
          buckets[h] = [v]
          out << v
        elsif !list.any? { |e| v.eql?(e) }
          list << v
          out << v
        end
      end
    end
    out
  end

  def each_with_index(*args, &block)
    return to_enum(:each_with_index, *args) { respond_to?(:size) ? size : nil } unless block
    i = 0
    each(*args) do |*x|
      yield(x.__svalue, i)
      i += 1
    end
    self
  end

  def find_index(val = (no_val = true; nil), &block)
    if !no_val
      warn("given block not used", uplevel: 1) if block
      i = 0
      each_entry do |v|
        return i if v == val
        i += 1
      end
      return nil
    end
    return to_enum(:find_index) { nil } unless block
    i = 0
    each do |*x|
      return i if yield(*x)
      i += 1
    end
    nil
  end

  alias __orig_enum_first first
  def first(n = (no_n = true; nil))
    return __orig_enum_first if no_n
    n = Kernel.__to_int(n)
    raise ArgumentError, "negative array size" if n < 0
    raise RangeError, "bignum too big to convert into `long'" if n > 0x7fff_ffff_ffff_ffff
    __orig_enum_first(n)
  end

  def map(&block)
    return to_enum(:map) { respond_to?(:size) ? size : nil } unless block
    out = []
    case block.arity
    when 1
      each { |a| out << block.call(a) }
    when 2
      each { |a, b| out << block.call(a, b) }
    when 3
      if block.lambda?
        each { |*a| out << block.call(*a) }
      else
        each { |a, b, c| out << block.call(a, b, c) }
      end
    else
      each { |*a| out << block.call(*a) }
    end
    out
  end
  alias collect map

  def inject(*args, &block)
    if args.size > 2
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)"
    end
    if args.size == 2
      warn("given block not used", uplevel: 1) if block
      acc = args[0]
      sym = args[1]
      unless Symbol === sym || String === sym
        raise TypeError, "#{sym.inspect} is not a symbol nor a string" unless sym.respond_to?(:to_str)
        sym = Kernel.__implicit(sym, String)
      end
      sym = sym.to_sym
      each_entry { |v| acc = acc.__send__(sym, v) }
      return acc
    elsif args.size == 1
      if block
        acc = args[0]
        each_entry { |v| acc = yield(acc, v) }
        return acc
      else
        sym = args[0]
        unless Symbol === sym || String === sym
          raise TypeError, "#{sym.inspect} is not a symbol nor a string" unless sym.respond_to?(:to_str)
          sym = Kernel.__implicit(sym, String)
        end
        sym = sym.to_sym
        acc = nil
        first = true
        each_entry do |v|
          if first
            acc = v
            first = false
          else
            acc = acc.__send__(sym, v)
          end
        end
        return acc
      end
    else
      raise ArgumentError, "no block given" unless block
      acc = nil
      first = true
      each_entry do |v|
        if first
          acc = v
          first = false
        else
          acc = yield(acc, v)
        end
      end
      return acc
    end
  end
  alias reduce inject

  def flat_map(&block)
    return to_enum(:flat_map) { respond_to?(:size) ? size : nil } unless block
    out = []
    each do |*args|
      res = yield(*args)
      if Array === res
        out.concat(res)
      elsif res.respond_to?(:to_ary)
        ary = res.to_ary
        if ary.nil?
          out << res
        elsif Array === ary
          out.concat(ary)
        else
          cname = Kernel.__cname(res)
          raise TypeError, "can't convert #{cname} into Array (#{cname}#to_ary gives #{Kernel.__cname(ary)})"
        end
      else
        out << res
      end
    end
    out
  end
  alias collect_concat flat_map

  def to_a(*args)
    out = []
    each(*args) do |*x|
      out << (x.__svalue)
      nil
    end
    out
  end
  alias entries to_a

  def to_h(*args, &block)
    h = {}
    each(*args) do |*x|
      elem = if block
               yield(*x)
             else
               x.__svalue
             end
      elem = Kernel.__implicit(elem, Array) if !(Array === elem) && elem.respond_to?(:to_ary)
      raise TypeError, "wrong element type #{Kernel.__cname(elem)} (expected array)" unless Array === elem
      raise ArgumentError, "element has wrong array length (expected 2, was #{elem.size})" if elem.size != 2
      h[elem[0]] = elem[1]
    end
    h
  end

  def each_entry(*args, &block)
    return to_enum(:each_entry, *args) { respond_to?(:size) ? size : nil } unless block
    each(*args) do |*x|
      yield(x.__svalue)
    end
    self
  end

  def each_cons(n, &block)
    n = Kernel.__to_int(n)
    raise ArgumentError, "invalid size" if n <= 0
    unless block
      return to_enum(:each_cons, n) do
        sz = respond_to?(:size) ? size : nil
        next nil unless Integer === sz || (Float === sz && sz.infinite?)
        sz < n ? 0 : sz - n + 1
      end
    end
    buf = []
    each_entry do |v|
      buf << v
      buf.shift if buf.size > n
      yield(buf.dup) if buf.size == n
    end
    self
  end

  def each_slice(n, &block)
    n = Kernel.__to_int(n)
    raise ArgumentError, "invalid slice size" if n <= 0
    unless block
      return to_enum(:each_slice, n) do
        sz = respond_to?(:size) ? size : nil
        next nil unless Integer === sz || (Float === sz && sz.infinite?)
        Float === sz ? sz : (sz + n - 1) / n
      end
    end
    buf = []
    each_entry do |v|
      buf << v
      if buf.size == n
        yield(buf)
        buf = []
      end
    end
    yield(buf) unless buf.empty?
    self
  end

  def cycle(n = (no_n = true; nil), &block)
    n = Kernel.__to_int(n) unless no_n || n.nil?
    unless block
      return to_enum(:cycle, *(no_n ? [] : [n])) do
        sz = respond_to?(:size) ? size : nil
        next nil unless Integer === sz || (Float === sz && sz.infinite?)
        if n.nil?
          sz == 0 ? 0 : Float::INFINITY
        elsif n <= 0
          0
        else
          sz * n
        end
      end
    end
    return nil if !no_n && !n.nil? && n <= 0
    cache = []
    each do |*x|
      cache << x
      yield(*x)
    end
    return nil if cache.empty?
    if no_n || n.nil?
      loop { cache.each { |x| yield(*x) } }
    else
      (n - 1).times { cache.each { |x| yield(*x) } }
    end
    nil
  end

  def drop_while(&block)
    return to_enum(:drop_while) { nil } unless block
    out = []
    dropping = true
    each_entry do |v|
      if dropping
        next if yield(v)
        dropping = false
      end
      out << v
    end
    out
  end

  def take_while(&block)
    return to_enum(:take_while) { nil } unless block
    out = []
    each do |*x|
      break unless yield(*x)
      out << (x.__svalue)
    end
    out
  end

  def find(ifnone = nil, &block)
    return to_enum(:find, ifnone) { nil } unless block
    each_entry { |v| return v if yield(v) }
    ifnone ? ifnone.call : nil
  end
  alias detect find

  def select(&block)
    return to_enum(:select) { respond_to?(:size) ? size : nil } unless block
    out = []
    each_entry { |v| out << v if yield(v) }
    out
  end
  alias find_all select
  alias filter select

  def reject(&block)
    return to_enum(:reject) { respond_to?(:size) ? size : nil } unless block
    out = []
    each_entry { |v| out << v unless yield(v) }
    out
  end

  def partition(&block)
    return to_enum(:partition) { respond_to?(:size) ? size : nil } unless block
    t, f = [], []
    each_entry { |v| (yield(v) ? t : f) << v }
    [t, f]
  end

  def group_by(&block)
    return to_enum(:group_by) { respond_to?(:size) ? size : nil } unless block
    h = {}
    each_entry do |v|
      k = yield(v)
      (h[k] ||= []) << v
    end
    h
  end

  def grep(pattern, &block)
    out = []
    if block
      each_entry do |v|
        out << yield(v) if pattern === v
      end
    else
      saved = $~
      begin
        each_entry do |v|
          if Regexp === pattern
            out << v if (String === v || Symbol === v || v.respond_to?(:to_str)) && pattern.match?(v)
          else
            out << v if pattern === v
          end
        end
      ensure
        $~ = saved
      end
    end
    out
  end

  def grep_v(pattern, &block)
    out = []
    if block
      each_entry do |v|
        out << yield(v) unless pattern === v
      end
    else
      saved = $~
      begin
        each_entry do |v|
          matched = if Regexp === pattern
                      (String === v || Symbol === v || v.respond_to?(:to_str)) && pattern.match?(v)
                    else
                      pattern === v
                    end
          out << v unless matched
        end
      ensure
        $~ = saved
      end
    end
    out
  end

  def sum(init = 0, &block)
    # MRI's int_range_sum: an Integer Range without a block, in closed form
    if !block && Range === self && Integer === (b = self.begin) && Integer === (e = self.end) && !(Float === init)
      e -= 1 if exclude_end?
      return init if e < b
      return init + (e - b + 1) * (b + e) / 2
    end
    acc = init
    comp = 0.0
    using_kahan = Float === acc
    each_entry do |v|
      v = yield(v) if block
      if using_kahan || Float === v
        unless using_kahan
          acc = acc.to_f
          using_kahan = true
        end
        fv = Float === v ? v : v.to_f
        if acc.infinite? || fv.infinite? || acc.nan? || fv.nan?
          acc = acc + fv
          comp = 0.0
        else
          t = acc + fv
          if acc.abs >= fv.abs
            comp += (acc - t) + fv
          else
            comp += (fv - t) + acc
          end
          acc = t
        end
      else
        acc += v
      end
    end
    using_kahan ? acc + comp : acc
  end

  alias __orig_enum_zip zip
  def zip(*args, &block)
    coerced = args.map do |a|
      if Array === a
        a
      elsif a.respond_to?(:to_ary)
        r = a.to_ary
        r.nil? ? a : r
      else
        a
      end
    end
    __orig_enum_zip(*coerced, &block)
  end

  alias __oruby_max2 max
  def max(n = nil, &block)
    if n.nil?
      return __oruby_max unless block
      best = nil
      found = false
      each_entry do |v|
        if !found
          best = v
          found = true
        else
          c = yield(v, best)
          raise ArgumentError, "comparison of #{v.class} with #{best.class} failed" if c.nil?
          cmp = Integer === c ? c : (c > 0 ? 1 : (c < 0 ? -1 : 0))
          best = v if cmp > 0
        end
      end
      return best
    end
    __oruby_nsel(to_a, n, true, block)
  end

  alias __oruby_min2 min
  def min(n = nil, &block)
    if n.nil?
      return __oruby_min unless block
      best = nil
      found = false
      each_entry do |v|
        if !found
          best = v
          found = true
        else
          c = yield(v, best)
          raise ArgumentError, "comparison of #{v.class} with #{best.class} failed" if c.nil?
          cmp = Integer === c ? c : (c > 0 ? 1 : (c < 0 ? -1 : 0))
          best = v if cmp < 0
        end
      end
      return best
    end
    __oruby_nsel(to_a, n, false, block)
  end

  def max_by(n = nil, &block)
    return to_enum(:max_by, *(n.nil? ? [] : [n])) { respond_to?(:size) ? size : nil } unless block
    if n.nil?
      best = nil
      best_k = nil
      found = false
      each_entry do |v|
        k = yield(v)
        if !found
          best = v; best_k = k; found = true
        else
          c = k <=> best_k
          raise ArgumentError, "comparison of #{k.class} with #{best_k.class} failed" if c.nil?
          if c > 0
            best = v; best_k = k
          end
        end
      end
      return best
    end
    __oruby_nsel_by(to_a, n, true, block)
  end

  def min_by(n = nil, &block)
    return to_enum(:min_by, *(n.nil? ? [] : [n])) { respond_to?(:size) ? size : nil } unless block
    if n.nil?
      best = nil
      best_k = nil
      found = false
      each_entry do |v|
        k = yield(v)
        if !found
          best = v; best_k = k; found = true
        else
          c = k <=> best_k
          raise ArgumentError, "comparison of #{k.class} with #{best_k.class} failed" if c.nil?
          if c < 0
            best = v; best_k = k
          end
        end
      end
      return best
    end
    __oruby_nsel_by(to_a, n, false, block)
  end

  def minmax_by(&block)
    return to_enum(:minmax_by) { respond_to?(:size) ? size : nil } unless block
    min_v = max_v = min_k = max_k = nil
    found = false
    each_entry do |v|
      k = yield(v)
      if !found
        min_v = max_v = v
        min_k = max_k = k
        found = true
      else
        c1 = k <=> min_k
        raise ArgumentError, "comparison of #{k.class} with #{min_k.class} failed" if c1.nil?
        if c1 < 0
          min_v = v; min_k = k
        end
        c2 = k <=> max_k
        raise ArgumentError, "comparison of #{k.class} with #{max_k.class} failed" if c2.nil?
        if c2 > 0
          max_v = v; max_k = k
        end
      end
    end
    [min_v, max_v]
  end
end

class Range
  def clamp(*args)
    if args.size == 1
      r = args[0]
      unless Range === r || (r.respond_to?(:begin) && r.respond_to?(:end) && r.respond_to?(:exclude_end?))
        raise TypeError, "wrong argument type #{Kernel.__cname(r)} (expected Range)"
      end
      min_v = r.begin
      max_v = r.end
      max_excl = r.exclude_end?
    elsif args.size == 2
      min_v, max_v = args
      max_excl = false
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..2)"
    end

    if !min_v.nil? && !max_v.nil?
      c = min_v <=> max_v
      raise ArgumentError, "comparison of #{min_v.class} with #{max_v.class} failed" if c.nil?
      raise ArgumentError, "min argument must be smaller than max argument" if c > 0
    end

    b = self.begin
    e = self.end
    excl = exclude_end?

    # Check compatibility between self and bounds
    cmp_val = lambda do |x, y|
      c = x <=> y
      raise ArgumentError, "comparison of #{x.class} with #{y.class} failed" if c.nil?
      c
    end

    # Clamp begin
    if b.nil?
      new_b = min_v
    elsif min_v.nil?
      new_b = b
    else
      new_b = cmp_val.call(b, min_v) < 0 ? min_v : b
    end
    if !new_b.nil? && !max_v.nil? && cmp_val.call(new_b, max_v) > 0
      return Range.new(max_v, max_v, true)
    end

    # Clamp end
    if e.nil?
      new_e = max_v
      new_excl = max_excl
    elsif max_v.nil?
      new_e = e
      new_excl = excl
    else
      ce = cmp_val.call(e, max_v)
      if ce < 0
        new_e = e
        new_excl = excl
      elsif ce > 0
        new_e = max_v
        new_excl = max_excl
      else
        new_e = e
        new_excl = excl || max_excl
      end
    end

    if !new_e.nil? && !min_v.nil?
      cm = cmp_val.call(new_e, min_v)
      if cm < 0 || (cm == 0 && new_excl)
        return Range.new(min_v, min_v, true)
      end
    end

    if !new_b.nil? && !new_e.nil?
      cb = cmp_val.call(new_b, new_e)
      if cb > 0
        return Range.new(new_e, new_e, true)
      end
    end

    Range.new(new_b, new_e, new_excl)
  end

  alias __orig_rng_overlap? overlap?
  def overlap?(other)
    raise TypeError, "wrong argument type #{Kernel.__cname(other)} (expected Range)" unless Range === other
    __orig_rng_overlap?(other)
  end

  alias __orig_rng_inspect inspect
  def inspect
    return exclude_end? ? "nil...nil" : "nil..nil" if self.begin.nil? && self.end.nil?
    __orig_rng_inspect
  end

  alias __orig_rng_eq ==
  def ==(other)
    return false unless Range === other
    self.begin == other.begin && self.end == other.end && exclude_end? == other.exclude_end?
  end

  def __float_to_ord(f)
    bits = [f].pack("G").unpack1("Q>")
    (bits >> 63) != 0 ? -(bits & 0x7fff_ffff_ffff_ffff) : bits
  end
  private :__float_to_ord

  def __ord_to_float(k)
    bits = k < 0 ? ((-k) | (1 << 63)) : k
    [bits].pack("Q>").unpack1("G")
  end
  private :__ord_to_float

  alias __orig_rng_bsearch bsearch
  def bsearch(&block)
    b = self.begin
    e = self.end
    unless (b.nil? || Numeric === b) && (e.nil? || Numeric === e)
      raise TypeError, "can't do binary search for #{(b || e).class}"
    end
    return to_enum(:bsearch) { nil } unless block
    if Float === b || Float === e
      bf = b.nil? ? -Float::INFINITY : b.to_f
      ef = e.nil? ? Float::INFINITY : e.to_f
      return nil if bf.nan? || ef.nan?
      lo = __float_to_ord(bf)
      hi = __float_to_ord(ef)
      hi -= 1 if exclude_end?
      return nil if lo > hi
      satisfied = nil
      while lo <= hi
        mid = (lo + hi) >> 1
        val = __ord_to_float(mid)
        v = yield(val)
        case v
        when Numeric
          if v == 0
            return val
          elsif v < 0
            hi = mid - 1
          else
            lo = mid + 1
          end
        when true
          satisfied = val
          hi = mid - 1
        when false, nil
          lo = mid + 1
        else
          raise TypeError, "wrong argument type #{v.class} (must be numeric, true, false or nil)"
        end
      end
      return satisfied
    end
    __orig_rng_bsearch(&block)
  end

  def reverse_each(&block)
    unless block
      return to_enum(:reverse_each) do
        b = self.begin
        e = self.end
        if Integer === e && b.nil?
          Float::INFINITY
        elsif b.nil? || !b.respond_to?(:succ) || e.nil?
          raise TypeError, "can't iterate from #{e.class}"
        else
          size
        end
      end
    end
    b = self.begin
    e = self.end
    raise TypeError, "can't iterate from NilClass" if e.nil?
    if Integer === e && (b.nil? || Integer === b)
      cur = exclude_end? ? e - 1 : e
      while b.nil? || cur >= b
        yield(cur)
        cur -= 1
      end
      return self
    end
    raise TypeError, "can't iterate from #{b.class}" if b.nil? || !b.respond_to?(:succ)
    to_a.reverse_each(&block)
    self
  end

  alias __orig_rng_size size
  def size
    b = self.begin
    raise TypeError, "can't iterate from #{b.class}" unless b.respond_to?(:succ)
    __orig_rng_size
  end

  alias __orig_rng_max max
  def max(*args, &block)
    b = self.begin
    e = self.end
    raise RangeError, "cannot get the maximum of endless range" if e.nil?
    if !args.empty? && !block && b.nil? && !(Integer === e)
      raise TypeError, "cannot get the maximum of non-Integer range"
    end
    if args.empty? && !block
      if !b.nil? && exclude_end? && !(Numeric === e)
        c = b <=> e
        raise ArgumentError, "comparison of #{b.class} with #{e.inspect} failed" if c.nil?
        return nil if c >= 0
      end
    end
    __orig_rng_max(*args, &block)
  end
end

if defined?(Method)
  class Method
    alias __orig_method_to_proc to_proc
    # a lambda of the method's arity; Proc#source_location and #parameters
    # (37_matchdata_symbol.rb) report the method's
    def to_proc
      m = self
      pr = case arity
           when 0 then lambda { |&b| m.call(&b) }
           when 1 then lambda { |a, &b| m.call(a, &b) }
           when 2 then lambda { |a, x, &b| m.call(a, x, &b) }
           when 3 then lambda { |a, x, y, &b| m.call(a, x, y, &b) }
           else return __orig_method_to_proc
           end
      pr.instance_variable_set(:@__oruby_method, m)
      pr
    end
  end
end
