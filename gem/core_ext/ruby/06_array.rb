# Array extensions
class Array
  def hash
    Thread.current[:__array_hash_stack] ||= []
    stack = Thread.current[:__array_hash_stack]
    return :cycle if stack.include?(object_id)
    stack.push(object_id)
    has_cycle = false
    h = size & 0x3fffffff
    begin
      each do |elem|
        eh = elem.hash
        if eh == :cycle
          has_cycle = true
          eh = 0
        else
          eh = eh.to_int if !eh.is_a?(Integer) && (eh.respond_to?(:to_int) rescue false)
        end
        h = ((h * 31) + (eh.to_i & 0x3fffffff)) & 0x3fffffff
      end
    ensure
      stack.pop
    end
    return :cycle if has_cycle && (stack.any? || (Thread.current[:__hash_hash_stack] && Thread.current[:__hash_hash_stack].any?))
    return 0 if has_cycle
    h
  end

  def to_ary
    self
  end

  alias __orig_cmp <=>
  def <=>(other)
    return 0 if equal?(other)
    unless other.is_a?(Array)
      return nil unless other.respond_to?(:to_ary)
      other = other.to_ary
      return nil unless other.is_a?(Array)
    end
    pair_id = [object_id, other.object_id]
    Thread.current[:__array_cmp_pairs] ||= {}
    pairs = Thread.current[:__array_cmp_pairs]
    return 0 if pairs[pair_id]
    pairs[pair_id] = true
    begin
      min_len = length < other.length ? length : other.length
      i = 0
      while i < min_len
        res = (self[i] <=> other[i])
        return res if res != 0
        i += 1
      end
      length <=> other.length
    ensure
      pairs.delete(pair_id)
    end
  end

  alias __orig_eq ==
  def ==(other)
    return true if equal?(other)
    unless other.is_a?(Array)
      if other.respond_to?(:to_ary)
        return other == self
      else
        return false
      end
    end
    __orig_eq(other)
  end

  def self.__coerce_to_int(idx)
    if idx.is_a?(Integer)
      raise RangeError, "bignum too big to convert into 'long'" if idx > 0x7fff_ffff_ffff_ffff || idx < -0x8000_0000_0000_0000
      return idx
    end
    if idx.respond_to?(:to_int)
      val = idx.to_int
      if val.is_a?(Integer) && (val > 0x7fff_ffff_ffff_ffff || val < -0x8000_0000_0000_0000)
        raise RangeError, "bignum too big to convert into 'long'"
      end
      raise TypeError, "can't convert #{idx.class} to Integer (#{idx.class}#to_int gives #{val.class})" unless val.is_a?(Integer)
      val
    elsif idx.is_a?(Float)
      begin
        idx.to_i
      rescue RangeError
        raise RangeError, "float too big to convert into long"
      end
    else
      raise TypeError, "no implicit conversion of #{idx.class} into Integer"
    end
  end

  def self.__coerce_to_ary(obj)
    return obj if obj.is_a?(Array)
    val = __check_to_ary(obj)
    return val if val
    raise TypeError, "no implicit conversion of #{obj.class} into Array"
  end

  def self.__coerce_to_str(obj)
    return obj if obj.is_a?(String)
    if obj.respond_to?(:to_str)
      val = obj.to_str
      raise TypeError, "can't convert #{obj.class} to String (#{obj.class}#to_str gives #{val.class})" unless val.is_a?(String)
      val
    else
      raise TypeError, "no implicit conversion of #{obj.class} into String"
    end
  end

  def self.__has_custom_method?(item, sym)
    sc = (class << item; self; end rescue nil)
    if sc
      all = (sc.instance_methods(false) rescue []) + (sc.private_instance_methods(false) rescue [])
      return true if all.include?(sym)
    end
    k = item.class rescue nil
    while k && k != Object && k != Kernel && k != BasicObject
      all = (k.instance_methods(false) rescue []) + (k.private_instance_methods(false) rescue [])
      return true if all.include?(sym)
      k = k.superclass rescue nil
    end
    false
  end

  def self.__check_to_ary(item)
    return item if Array === item

    if __has_custom_method?(item, :respond_to_missing?)
      res = (item.__send__(:respond_to_missing?, :to_ary, true) rescue nil)
      return nil unless res
      val = item.__send__(:to_ary)
      return nil if val.nil?
      raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
      val
    elsif __has_custom_method?(item, :respond_to?)
      res = (item.respond_to?(:to_ary, true) rescue false)
      return nil unless res
      val = item.__send__(:to_ary)
      return nil if val.nil?
      raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
      val
    elsif __has_custom_method?(item, :method_missing)
      begin
        val = item.__send__(:to_ary)
        return nil if val.nil?
        raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
        val
      rescue NoMethodError => e
        if e.name == :to_ary
          nil
        else
          raise e
        end
      end
    elsif (item.respond_to?(:to_ary, true) rescue false)
      val = item.__send__(:to_ary)
      return nil if val.nil?
      raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
      val
    else
      nil
    end
  end

  def __ensure_array(res)
    if res.is_a?(Array) && res.class != Array
      [].replace(res)
    else
      res
    end
  end

  def self.try_convert(obj)
    return obj if obj.is_a?(Array)
    return nil unless obj.respond_to?(:to_ary)
    res = obj.to_ary
    return nil if res.nil?
    raise TypeError, "can't convert #{obj.class} into Array (#{obj.class}#to_ary gives #{res.class})" unless res.is_a?(Array)
    res
  end

  alias __orig_at at
  def at(idx)
    idx = Array.__coerce_to_int(idx)
    __orig_at(idx)
  end

  def __slice_arith_seq(seq)
    len = self.length
    step = seq.step || 1
    raise ArgumentError, "slice step cannot be zero" if step == 0

    b = seq.begin
    e = seq.end
    excl = seq.exclude_end?

    if step < 0
      if excl && !e.nil?
        e = e + 1
        excl = false
      end
      b, e = e, b
    end

    beg = b.nil? ? 0 : Array.__coerce_to_int(b)
    fin = e.nil? ? -1 : Array.__coerce_to_int(e)
    excl = false if e.nil?

    if beg < 0
      beg += len
      if beg < 0
        if step.abs != 1
          raise RangeError, "#{seq.inspect} out of range"
        else
          return nil
        end
      end
    end

    if fin < 0
      fin += len
    end

    if !excl
      fin += 1
    end

    if step == 1 || step == -1
      if beg > len
        return nil
      end
      fin = len if fin > len
      slen = fin > beg ? fin - beg : 0
    else
      slen = fin > beg ? fin - beg : 0
      if beg > len || slen > len
        raise RangeError, "#{seq.inspect} out of range"
      end
    end

    if beg + slen > len
      slen = len - beg
    end
    return [] if slen <= 0

    if step == 1
      res = self[beg, slen]
      return __ensure_array(res)
    end

    orig_len = slen
    if step > 0 && step >= slen
      return [self[beg]]
    elsif step < 0 && step < -slen
      step = -slen
    end

    ustep = step.abs
    count = (slen + ustep - 1) / ustep

    j = beg + (step > 0 ? 0 : (orig_len - 1))
    res = []
    count.times do
      res << self[j]
      j += step
    end
    res
  end

  alias __orig_aref []
  def [](arg, *rest)
    if rest.empty?
      if arg.is_a?(Enumerator::ArithmeticSequence)
        return __slice_arith_seq(arg)
      end
      if arg.is_a?(Range)
        b = arg.begin
        e = arg.end
        b = Array.__coerce_to_int(b) if b
        e = Array.__coerce_to_int(e) if e
        excl = e.nil? ? false : arg.exclude_end?
        rng = Range.new(b, e, excl)
        res = __orig_aref(rng)
        return __ensure_array(res)
      end
      idx = Array.__coerce_to_int(arg)
      __orig_aref(idx)
    elsif rest.size == 1
      start = Array.__coerce_to_int(arg)
      len = Array.__coerce_to_int(rest[0])
      res = __orig_aref(start, len)
      __ensure_array(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{rest.size + 1}, expected 1..2)"
    end
  end
  alias slice []

  alias __orig_aset []=
  def []=(*args)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if args.size == 2
      idx, val = args
      if idx.is_a?(Range)
        b = idx.begin
        e = idx.end
        b = Array.__coerce_to_int(b) if b
        e = Array.__coerce_to_int(e) if e
        val = val.to_ary if !val.is_a?(Array) && val.respond_to?(:to_ary)
        return __orig_aset(Range.new(b, e, idx.exclude_end?), val)
      end
      idx = Array.__coerce_to_int(idx)
      __orig_aset(idx, val)
    elsif args.size == 3
      start, len, val = args
      start = Array.__coerce_to_int(start)
      len = Array.__coerce_to_int(len)
      val = val.to_ary if !val.is_a?(Array) && val.respond_to?(:to_ary)
      __orig_aset(start, len, val)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 2..3)"
    end
  end

  alias __orig_insert insert
  def insert(*args)
    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if args.empty?
    raise FrozenError, "can't modify frozen Array" if frozen?
    pos = Array.__coerce_to_int(args[0])
    return self if args.size == 1
    __orig_insert(pos, *args[1..-1])
  end

  alias __orig_plus +
  def +(other)
    other = Array.__coerce_to_ary(other)
    res = __orig_plus(other)
    res.class == Array ? res : Array.new(res)
  end

  alias __orig_index index
  def index(*args, &block)
    if args.size == 1
      warn "given block not used", uplevel: 1 if block
      __orig_index(args[0])
    elsif args.empty?
      if block
        __orig_index(&block)
      else
        to_enum(:index) { nil }
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end
  alias find_index index

  alias __orig_rindex rindex
  def rindex(*args, &block)
    if args.size == 1
      warn "given block not used", uplevel: 1 if block
      __orig_rindex(args[0])
    elsif args.empty?
      if block
        __orig_rindex(&block)
      else
        to_enum(:rindex) { nil }
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  def dig(idx, *rest)
    val = self.at(idx)
    return nil if val.nil?
    return val if rest.empty?
    raise TypeError, "#{val.class} does not have #dig method" unless val.respond_to?(:dig)
    val.dig(*rest)
  end

  alias __orig_fill fill
  def fill(*args, &block)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if block_given?
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)" if args.size > 2
      case args.size
      when 0
        (0...size).each { |i| self[i] = yield(i) }
      when 1
        arg = args[0]
        if arg.is_a?(Range)
          b = arg.begin ? Array.__coerce_to_int(arg.begin) : 0
          e = arg.end ? Array.__coerce_to_int(arg.end) : (size - 1)
          b = size + b if b < 0
          raise RangeError, "#{b} out of range" if b < 0
          e = size + e if e < 0
          e -= 1 if arg.exclude_end? && !arg.end.nil?
          (b..e).each { |i| self[i] = yield(i) }
        else
          start = arg.nil? ? 0 : Array.__coerce_to_int(arg)
          start = size + start if start < 0
          start = 0 if start < 0
          (start...size).each { |i| self[i] = yield(i) }
        end
      when 2
        start = args[0].nil? ? 0 : Array.__coerce_to_int(args[0])
        len = args[1].nil? ? nil : Array.__coerce_to_int(args[1])
        start = size + start if start < 0
        start = 0 if start < 0
        count = len.nil? ? (size - start) : len
        return self if count <= 0
        raise ArgumentError, "argument too big" if start + count > 1152921504606846975 # MRI's ARY_MAX_SIZE
        (start...(start + count)).each { |i| self[i] = yield(i) }
      end
    else
      raise ArgumentError, "wrong number of arguments (given 0, expected 1..3)" if args.empty?
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..3)" if args.size > 3
      filler = args[0]
      case args.size
      when 1
        (0...size).each { |i| self[i] = filler }
      when 2
        arg = args[1]
        if arg.is_a?(Range)
          b = arg.begin ? Array.__coerce_to_int(arg.begin) : 0
          e = arg.end ? Array.__coerce_to_int(arg.end) : (size - 1)
          b = size + b if b < 0
          raise RangeError, "#{b} out of range" if b < 0
          e = size + e if e < 0
          e -= 1 if arg.exclude_end? && !arg.end.nil?
          (b..e).each { |i| self[i] = filler }
        else
          start = arg.nil? ? 0 : Array.__coerce_to_int(arg)
          start = size + start if start < 0
          start = 0 if start < 0
          (start...size).each { |i| self[i] = filler }
        end
      when 3
        start = args[1].nil? ? 0 : Array.__coerce_to_int(args[1])
        len = args[2].nil? ? nil : Array.__coerce_to_int(args[2])
        start = size + start if start < 0
        start = 0 if start < 0
        count = len.nil? ? (size - start) : len
        return self if count <= 0
        raise ArgumentError, "argument too big" if start + count > 1152921504606846975 # MRI's ARY_MAX_SIZE
        (start...(start + count)).each { |i| self[i] = filler }
      end
    end
    self
  end



  alias __orig_first first
  def first(*args)
    if args.empty?
      __orig_first
    elsif args.size == 1
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      res = __orig_first(n)
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_last last
  def last(*args)
    if args.empty?
      __orig_last
    elsif args.size == 1
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      res = __orig_last(n)
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_fetch fetch
  def fetch(idx, *rest, &block)
    raise ArgumentError, "wrong number of arguments (given #{1 + rest.size}, expected 1..2)" if rest.size > 1
    warn "warning: block supersedes default value argument" if !rest.empty? && block
    orig_idx = idx
    c_idx = Array.__coerce_to_int(idx)
    len = self.length
    if c_idx < 0
      actual_idx = len + c_idx
    else
      actual_idx = c_idx
    end
    if actual_idx >= 0 && actual_idx < len
      self[actual_idx]
    elsif block
      block.call(orig_idx)
    elsif !rest.empty?
      rest.first
    else
      bounds = len == 0 ? "0...0" : "-#{len}...#{len}"
      raise IndexError, "index #{c_idx} outside of array bounds: #{bounds}"
    end
  end

  def fetch_values(*indices, &block)
    indices.map do |idx|
      c_idx = Array.__coerce_to_int(idx)
      if block_given?
        fetch(c_idx) { block.call(idx) }
      else
        fetch(c_idx)
      end
    end
  end

  alias __orig_bsearch bsearch
  def bsearch(&block)
    return to_enum(:bsearch) { nil } unless block
    __orig_bsearch(&block)
  end

  alias __orig_bsearch_index bsearch_index
  def bsearch_index(&block)
    return to_enum(:bsearch_index) { nil } unless block
    __orig_bsearch_index(&block)
  end

  def self.__combination_size(n, k)
    return 0 if k < 0 || k > n
    return 1 if k == 0 || k == n
    k = n - k if k > n / 2
    res = 1
    (1..k).each { |i| res = (res * (n - i + 1)) / i }
    res
  end

  def self.__permutation_size(n, k)
    return 0 if k < 0 || k > n
    return 1 if k == 0
    res = 1
    (0...k).each { |i| res *= (n - i) }
    res
  end

  def self.__repeated_combination_size(n, k)
    return 0 if k < 0
    return 1 if k == 0
    return 0 if n == 0
    __combination_size(n + k - 1, k)
  end

  def self.__repeated_permutation_size(n, k)
    return 0 if k < 0
    return 1 if k == 0
    return 0 if n == 0
    n ** k
  end

  alias __orig_combination combination
  def combination(k, &block)
    ck = Array.__coerce_to_int(k)
    unless block_given?
      return to_enum(:combination, ck) { Array.__combination_size(size, ck) }
    end
    dup.__orig_combination(ck, &block)
    self
  end

  alias __orig_permutation permutation
  def permutation(k = nil, &block)
    if k.nil?
      unless block_given?
        return to_enum(:permutation) { Array.__permutation_size(size, size) }
      end
      ck = size
    else
      ck = Array.__coerce_to_int(k)
      unless block_given?
        return to_enum(:permutation, ck) { Array.__permutation_size(size, ck) }
      end
    end
    dup.__orig_permutation(ck, &block)
    self
  end

  alias __orig_repeated_combination repeated_combination
  def repeated_combination(k, &block)
    ck = Array.__coerce_to_int(k)
    unless block_given?
      return to_enum(:repeated_combination, ck) { Array.__repeated_combination_size(size, ck) }
    end
    dup.__orig_repeated_combination(ck, &block)
    self
  end

  alias __orig_repeated_permutation repeated_permutation
  def repeated_permutation(k, &block)
    ck = Array.__coerce_to_int(k)
    unless block_given?
      return to_enum(:repeated_permutation, ck) { Array.__repeated_permutation_size(size, ck) }
    end
    dup.__orig_repeated_permutation(ck, &block)
    self
  end

  alias __orig_cycle cycle
  def cycle(nv = nil, &block)
    unless block_given?
      return to_enum(:cycle, nv) do
        if empty?
          0
        elsif nv.nil?
          Float::INFINITY
        else
          c = Array.__coerce_to_int(nv)
          c <= 0 ? 0 : size * c
        end
      end
    end
    n = nv.nil? ? -1 : Array.__coerce_to_int(nv)
    return nil if empty? || (n <= 0 && !nv.nil?)
    ary = []
    each do |x|
      ary.push(x)
      yield x
    end
    return nil if ary.empty?
    while n < 0 || (n -= 1) > 0
      ary.each do |x|
        yield x
      end
    end
    nil
  end

  alias __orig_drop drop
  def drop(n)
    n = Array.__coerce_to_int(n)
    raise ArgumentError, "attempt to drop negative size" if n < 0
    res = __orig_drop(n)
    Array.new(res)
  end

  alias __orig_take take
  def take(n)
    n = Array.__coerce_to_int(n)
    raise ArgumentError, "attempt to take negative size" if n < 0
    res = __orig_take(n)
    Array.new(res)
  end

  alias __orig_pop pop
  def pop(*args)
    if args.empty?
      raise FrozenError, "can't modify frozen Array" if frozen?
      __orig_pop
    elsif args.size == 1
      raise FrozenError, "can't modify frozen Array" if frozen?
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      return [] if n == 0
      start = [0, self.length - n].max
      count = [n, self.length].min
      res = self.slice!(start, count) || []
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_shift shift
  def shift(*args)
    if args.empty?
      raise FrozenError, "can't modify frozen Array" if frozen?
      __orig_shift
    elsif args.size == 1
      raise FrozenError, "can't modify frozen Array" if frozen?
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      return [] if n == 0
      res = self.slice!(0, n) || []
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_delete_at delete_at
  def delete_at(idx)
    idx = Array.__coerce_to_int(idx)
    __orig_delete_at(idx)
  end

  alias __orig_rotate rotate
  def rotate(n = 1)
    n = Array.__coerce_to_int(n)
    res = __orig_rotate(n)
    Array.new(res)
  end

  alias __orig_rotate_bang rotate!
  def rotate!(n = 1)
    n = Array.__coerce_to_int(n)
    __orig_rotate_bang(n)
  end

  def values_at(*args)
    res = []
    args.each do |arg|
      if arg.is_a?(Range)
        b = arg.begin
        e = arg.end
        b = Array.__coerce_to_int(b) if b
        e = Array.__coerce_to_int(e) if e
        olen = length
        beg = b.nil? ? 0 : b
        fin = e.nil? ? -1 : e
        excl = e.nil? ? false : arg.exclude_end?

        beg += olen if beg < 0
        raise RangeError, "#{arg.inspect} out of range" if beg < 0

        fin += olen if fin < 0
        fin += 1 unless excl

        slen = fin > beg ? fin - beg : 0
        if slen > 0
          end_pos = beg + slen
          if beg < olen
            copy_len = end_pos > olen ? (olen - beg) : slen
            (beg...(beg + copy_len)).each { |i| res << self[i] }
          end
          if end_pos > olen
            nil_count = beg > olen ? slen : (end_pos - olen)
            nil_count.times { res << nil }
          end
        end
      else
        idx = Array.__coerce_to_int(arg)
        res << at(idx)
      end
    end
    res
  end

  def self.__resolve_encoding(strings)
    enc = nil
    has_high_utf8 = false
    has_high_binary = false
    strings.each do |s|
      next unless s.is_a?(String)
      e = s.encoding
      e_name = (e.respond_to?(:name) ? e.name : e.to_s).upcase
      is_binary = (e_name == 'BINARY' || e_name == 'ASCII-8BIT')
      is_utf8 = (e_name == 'UTF-8' || e_name == 'UTF8')
      has_high = s.bytes.any? { |b| b > 127 }
      if is_binary && has_high
        has_high_binary = true
      elsif is_utf8 && has_high
        has_high_utf8 = true
      end
      if has_high_binary && has_high_utf8
        raise Encoding::CompatibilityError, "incompatible character encodings: UTF-8 and ASCII-8BIT"
      end
      if enc.nil?
        enc = e
      elsif (enc.name == 'US-ASCII' rescue enc.to_s == 'US-ASCII') && (is_utf8 && has_high)
        enc = e
      end
    end
    enc || (defined?(Encoding::UTF_8) ? Encoding::UTF_8 : "UTF-8")
  end

  alias __orig_join join
  def join(sep = nil)
    if sep.nil? && $,
      warn "warning: $, is set to non-nil value", uplevel: 1
    end
    if empty?
      res = ""
      res.force_encoding(Encoding::US_ASCII)
      return res
    end
    sep = $, if sep.nil?
    if sep.nil?
      sep_str = ""
    else
      sep_str = Array.__coerce_to_str(sep)
    end

    Thread.current[:__array_join_seen] ||= {}
    seen = Thread.current[:__array_join_seen]
    if seen[object_id]
      raise ArgumentError, "recursive array join"
    end
    seen[object_id] = true

    begin
      parts = map do |x|
        if x.is_a?(String)
          x
        else
          str = (x.to_str rescue nil) if x.respond_to?(:to_str)
          if str.is_a?(String)
            str
          else
            ary = (x.to_ary rescue nil) if x.respond_to?(:to_ary)
            if ary.is_a?(Array)
              ary.join(sep)
            elsif x.respond_to?(:to_s)
              x.to_s
            else
              raise NoMethodError, "undefined method 'to_s' for #{x.inspect}"
            end
          end
        end
      end
    ensure
      seen.delete(object_id)
    end

    target_enc = Array.__resolve_encoding(parts + (sep_str.empty? ? [] : [sep_str]))
    res = parts.__orig_join(sep_str)
    res.force_encoding(target_enc) if target_enc && res.respond_to?(:force_encoding)
    res
  end

  def flatten(level = nil)
    res = dup.flatten!(level)
    res = res ? res : dup
    __ensure_array(res)
  end

  def flatten!(level = nil)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if level
      level = Array.__coerce_to_int(level)
      return nil if level == 0
      level = -1 if level < 0
    else
      level = -1
    end
    res = []
    modified = false
    rec = ->(ary, curr_level, seen) {
      ary.each do |item|
        if curr_level == 0
          res << item
          next
        end
        sub = Array.__check_to_ary(item)
        if sub
          if !(sub.is_a?(Array) rescue false)
            raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{sub.class})"
          else
            modified = true
            raise ArgumentError, "tried to flatten recursive array" if seen.include?(sub.__id__)
            seen << sub.__id__
            rec.call(sub, curr_level - 1, seen)
            seen.delete(sub.__id__)
          end
        else
          res << item
        end
      end
    }
    rec.call(self, level, [self.__id__])
    if modified
      self.replace(res)
      self
    else
      nil
    end
  end

  alias __orig_sample sample
  def sample(*args)
    opts = {}
    if args.last.is_a?(Hash)
      opts = args.pop
    elsif args.last.respond_to?(:to_hash)
      opts = args.pop.to_hash
    end
    count_given = !args.empty?
    count = count_given ? Array.__coerce_to_int(args[0]) : nil
    raise ArgumentError, "negative array size" if count && count < 0

    rng = opts[:random]
    if rng
      raise NoMethodError, "undefined method 'rand' for #{rng}" unless rng.respond_to?(:rand)
      return nil if empty? && !count_given
      return [] if empty? && count_given
      return [] if count == 0

      if count_given
        n = [count, size].min
        indices = (0...size).to_a
        selected = []
        n.times do
          max = indices.size
          r = rng.rand(max)
          r = Array.__coerce_to_int(r) unless r.is_a?(Integer)
          raise RangeError, "random value #{r} out of bounds" if r < 0 || r >= max
          selected << self[indices.delete_at(r)]
        end
        selected
      else
        max = size
        r = rng.rand(max)
        r = Array.__coerce_to_int(r) unless r.is_a?(Integer)
        raise RangeError, "random value #{r} out of bounds" if r < 0 || r >= max
        self[r]
      end
    else
      if count_given
        __orig_sample(count)
      else
        __orig_sample
      end
    end
  end

  alias __orig_shuffle shuffle
  def shuffle(*args)
    opts = {}
    if args.last.is_a?(Hash)
      opts = args.pop
    elsif args.last.respond_to?(:to_hash)
      opts = args.pop.to_hash
    end
    rng = opts[:random]
    if rng
      raise NoMethodError, "undefined method 'rand' for #{rng}" unless rng.respond_to?(:rand)
    end
    len = size
    return [] if len == 0
    res = Array.new(self)
    i = len
    while i > 1
      r = rng ? rng.rand(i) : Random.rand(i)
      r = Array.__coerce_to_int(r) unless r.is_a?(Integer)
      raise RangeError, "random value #{r} out of bounds" if r < 0 || r >= i
      i -= 1
      tmp = res[i]
      res[i] = res[r]
      res[r] = tmp
    end
    res
  end

  def shuffle!(*args)
    raise FrozenError, "can't modify frozen Array" if frozen?
    self.replace(shuffle(*args))
    self
  end

  alias __orig_reverse reverse
  def reverse
    res = __orig_reverse
    res.class == Array ? res : Array.new(res)
  end

  alias __orig_reject reject
  def reject(&block)
    return to_enum(:reject) { size } unless block_given?
    res = []
    each do |x|
      res << x unless yield(x)
    end
    res
  end

  alias __orig_reject! reject!
  def reject!(&block)
    return to_enum(:reject!) { size } unless block_given?
    raise FrozenError, "can't modify frozen #{self.class}" if frozen?
    len = size
    w_idx = 0
    idx = 0
    changed = false
    begin
      while idx < len
        elem = self[idx]
        if yield(elem)
          changed = true
        else
          self[w_idx] = elem if w_idx != idx
          w_idx += 1
        end
        idx += 1
      end
    ensure
      if idx < len
        while idx < len
          self[w_idx] = self[idx]
          w_idx += 1
          idx += 1
        end
      end
      if w_idx < len
        slice!(w_idx..-1)
      end
    end
    changed ? self : nil
  end

  alias __orig_delete_if delete_if
  def delete_if(&block)
    return to_enum(:delete_if) { size } unless block_given?
    reject!(&block)
    self
  end

  alias __orig_select select
  def select(&block)
    return to_enum(:select) { size } unless block_given?
    res = []
    each do |x|
      res << x if yield(x)
    end
    res
  end
  alias filter select

  alias __orig_sort sort
  def sort(&block)
    res = __orig_sort(&block)
    res.class == Array ? res : Array.new(res)
  end

  def sort_by!(&block)
    return to_enum(:sort_by!) { size } unless block_given?
    raise FrozenError, "can't modify frozen #{self.class}" if frozen?
    self.replace(self.sort_by(&block))
  end

  alias __orig_slice_bang slice!
  def slice!(*args)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if args.size == 1
      arg = args[0]
      if arg.is_a?(Range)
        b = arg.begin ? Array.__coerce_to_int(arg.begin) : 0
        e = arg.end ? Array.__coerce_to_int(arg.end) : -1
        excl = arg.end.nil? ? false : arg.exclude_end?
        res = __orig_slice_bang(Range.new(b, e, excl))
        return (res && res.class != Array) ? Array.new(res) : res
      end
      idx = Array.__coerce_to_int(arg)
      __orig_slice_bang(idx)
    elsif args.size == 2
      start = Array.__coerce_to_int(args[0])
      len = Array.__coerce_to_int(args[1])
      res = __orig_slice_bang(start, len)
      (res && res.class != Array) ? Array.new(res) : res
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..2)"
    end
  end

  def to_h(&block)
    h = {}
    each_with_index do |item, idx|
      kv = block_given? ? yield(item) : item
      unless kv.is_a?(Array)
        if kv.respond_to?(:to_ary)
          kv = kv.to_ary
          raise TypeError, "wrong element type #{kv.class} at #{idx} (expected Array)" unless kv.is_a?(Array)
        else
          raise TypeError, "wrong element type #{kv.class} at #{idx} (expected Array)"
        end
      end
      raise ArgumentError, "wrong array length at #{idx} (expected 2, was #{kv.length})" unless kv.length == 2
      h[kv[0]] = kv[1]
    end
    h
  end

  alias __orig_zip zip
  def zip(*args, &block)
    coerced = args.map do |arg|
      if !arg.is_a?(Array) && arg.respond_to?(:to_ary)
        arg.to_ary
      else
        arg
      end
    end
    __orig_zip(*coerced, &block)
  end

  alias __orig_concat concat
  def concat(*others)
    coerced = others.map do |other|
      o = Array.__coerce_to_ary(other)
      o.equal?(self) ? o.dup : o
    end
    coerced.each do |o|
      __orig_concat(o)
    end
    self
  end

  alias __orig_transpose transpose
  def transpose
    return [] if empty?
    coerced = map { |x| Array.__coerce_to_ary(x) }
    coerced.__orig_transpose
  end

  alias __orig_replace replace
  def replace(other)
    other = Array.__coerce_to_ary(other)
    __orig_replace(other)
  end

  alias __orig_mul *
  def *(arg)
    if arg.respond_to?(:to_str)
      join(arg.to_str)
    elsif arg.respond_to?(:to_int)
      n = arg.to_int
      raise TypeError, "can't convert #{arg.class} to Integer" unless n.is_a?(Integer)
      raise ArgumentError, "negative argument" if n < 0
      __ensure_array(__orig_mul(n))
    elsif arg.is_a?(Integer)
      raise ArgumentError, "negative argument" if arg < 0
      __ensure_array(__orig_mul(arg))
    elsif arg.is_a?(String)
      join(arg)
    else
      raise TypeError, "no implicit conversion of #{arg.class} into Integer"
    end
  end

  alias __orig_product product
  def product(*others, &block)
    others = others.map { |o| Array.__coerce_to_ary(o) }
    if block_given?
      return self if empty? || others.any?(&:empty?)
      pools = [self] + others
      generate = ->(pool_idx, current) {
        if pool_idx == pools.size
          yield(current.dup)
        else
          pools[pool_idx].each do |el|
            current.push(el)
            generate.call(pool_idx + 1, current)
            current.pop
          end
        end
      }
      generate.call(0, [])
      self
    else
      total = size
      others.each do |o|
        total *= o.size
        raise RangeError, "too big for product" if total > 1_000_000_000
      end
      __orig_product(*others)
    end
  end

  def uniq(&block)
    seen = {}
    res = []
    each do |x|
      val = block_given? ? yield(x) : x
      h = (val.hash rescue nil)
      found = false
      bucket = (seen[h] ||= [])
      bucket.each do |item|
        if (item.equal?(val) rescue false) || (item.eql?(val) rescue false)
          found = true
          break
        end
      end
      unless found
        bucket << val
        res << x
      end
    end
    res
  end

  def uniq!(&block)
    raise FrozenError, "can't modify frozen #{self.class}" if frozen?
    u = uniq(&block)
    if u.length == self.length
      nil
    else
      self.replace(u)
      self
    end
  end

  alias __orig_init initialize
  def initialize(*args, &block)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if args.empty?
      warn "given block not used", uplevel: 1 if block_given?
      self.clear
    elsif args.size == 1
      arg = args[0]
      if arg.is_a?(Array)
        self.replace(arg)
      elsif (arg.respond_to?(:to_ary, true) rescue false)
        self.replace(arg.send(:to_ary))
      else
        size = Array.__coerce_to_int(arg)
        raise ArgumentError, "negative array size" if size < 0
        raise ArgumentError, "array size too big" if size > 100_000_000
        self.clear
        if block_given?
          size.times { |i| self << yield(i) }
        else
          size.times { self << nil }
        end
      end
    elsif args.size == 2
      arg0, default_obj = args
      raise TypeError, "wrong argument type Array" if arg0.is_a?(Array) || (arg0.respond_to?(:to_ary, true) rescue false)
      size = Array.__coerce_to_int(arg0)
      raise ArgumentError, "negative array size" if size < 0
      raise ArgumentError, "array size too big" if size > 100_000_000
      self.clear
      if block_given?
        warn "warning: block supersedes default value argument"
        size.times { |i| self << yield(i) }
      else
        size.times { self << default_obj }
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)"
    end
    self
  end

  alias __orig_difference difference
  def difference(*others)
    others = others.map { |o| Array.__coerce_to_ary(o) }
    if length <= 16 || others.any? { |o| o.length <= 16 }
      res = []
      each do |x|
        unless others.any? { |ary| ary.any? { |y| (x.equal?(y) rescue false) || (x.eql?(y) rescue false) } }
          res << x
        end
      end
      res
    else
      __orig_difference(*others)
    end
  end
  alias - difference

  alias __orig_intersection intersection
  def intersection(*others)
    others = others.map { |o| Array.__coerce_to_ary(o) }
    if length <= 16 && others.all? { |o| o.length <= 16 }
      res = []
      each do |x|
        if others.all? { |ary| ary.any? { |y| (x.equal?(y) rescue false) || (x.eql?(y) rescue false) } }
          unless res.any? { |y| (x.equal?(y) rescue false) || (x.eql?(y) rescue false) }
            res << x
          end
        end
      end
      res
    else
      __orig_intersection(*others)
    end
  end
  alias & intersection

  alias __orig_union union
  def union(*others)
    others = others.map { |o| Array.__coerce_to_ary(o) }
    total_len = length + others.map(&:length).inject(0, :+)
    if total_len <= 16
      res = []
      ([self] + others).each do |ary|
        ary.each do |x|
          unless res.any? { |y| (x.equal?(y) rescue false) || (x.eql?(y) rescue false) }
            res << x
          end
        end
      end
      res
    else
      __orig_union(*others)
    end
  end
  alias | union

  alias __orig_intersect_p intersect?
  def intersect?(other)
    other = Array.__coerce_to_ary(other)
    __orig_intersect_p(other)
  end

  alias detect find

  alias __orig_assoc assoc
  def assoc(obj)
    each do |elem|
      ary = elem.is_a?(Array) ? elem : (elem.respond_to?(:to_ary) ? elem.to_ary : nil)
      if ary.is_a?(Array) && ary.size > 0 && ary[0] == obj
        return ary
      end
    end
    nil
  end

  alias __orig_rassoc rassoc
  def rassoc(obj)
    each do |elem|
      ary = elem.is_a?(Array) ? elem : (elem.respond_to?(:to_ary) ? elem.to_ary : nil)
      if ary.is_a?(Array) && ary.size > 1 && ary[1] == obj
        return ary
      end
    end
    nil
  end

  alias __orig_count count
  def count(*args, &block)
    if !args.empty? && block_given?
      warn "given block not used", uplevel: 1
      return __orig_count(*args)
    end
    __orig_count(*args, &block)
  end

  def find(ifnone = nil, &block)
    unless block_given?
      return to_enum(:find, ifnone) { nil }
    end
    i = 0
    while i < size
      elem = self[i]
      return elem if yield(elem)
      i += 1
    end
    ifnone ? ifnone.call : nil
  end
  alias detect find

  def rfind(ifnone = nil, &block)
    unless block_given?
      return to_enum(:rfind, ifnone) { nil }
    end
    i = size - 1
    while i >= 0 && i < size
      elem = self[i]
      return elem if yield(elem)
      i = [i - 1, size - 1].min
    end
    ifnone ? ifnone.call : nil
  end

  alias __orig_map_bang map!
  def map!(&block)
    return to_enum(:map!) { size } unless block_given?
    raise FrozenError, "can't modify frozen Array" if frozen?
    i = 0
    while i < size
      self[i] = yield(self[i])
      i += 1
    end
    self
  end
  alias collect! map!

  alias __orig_select_bang select!
  def select!(&block)
    return to_enum(:select!) { size } unless block_given?
    raise FrozenError, "can't modify frozen Array" if frozen?
    i = 0
    j = 0
    changed = false
    begin
      while i < size
        elem = self[i]
        matched = yield(elem)
        if matched
          if i != j
            self[j] = elem
          end
          j += 1
        else
          changed = true
        end
        i += 1
      end
    ensure
      if i < size
        rem = self[i...size]
        rem.each do |x|
          self[j] = x
          j += 1
        end
      end
      if j < size
        self.slice!(j..-1)
      end
    end
    changed ? self : nil
  end
  alias filter! select!

  alias __orig_keep_if keep_if
  def keep_if(&block)
    return to_enum(:keep_if) { size } unless block_given?
    select!(&block)
    self
  end

  alias __orig_reject_bang reject!
  def reject!(&block)
    return to_enum(:reject!) { size } unless block_given?
    raise FrozenError, "can't modify frozen Array" if frozen?
    i = 0
    j = 0
    changed = false
    begin
      while i < size
        elem = self[i]
        matched = yield(elem)
        if !matched
          if i != j
            self[j] = elem
          end
          j += 1
        else
          changed = true
        end
        i += 1
      end
    ensure
      if i < size
        rem = self[i...size]
        rem.each do |x|
          self[j] = x
          j += 1
        end
      end
      if j < size
        self.slice!(j..-1)
      end
    end
    changed ? self : nil
  end

  alias __orig_delete_if delete_if
  def delete_if(&block)
    return to_enum(:delete_if) { size } unless block_given?
    reject!(&block)
    self
  end

  alias __orig_sum sum
  def sum(init = 0, &block)
    sum = init
    c = 0.0
    has_float = init.is_a?(Float)
    each do |x|
      x = yield(x) if block_given?
      if x.is_a?(Float) || has_float
        has_float = true
        sum = sum.to_f unless sum.is_a?(Float)
        x = x.to_f unless x.is_a?(Float)
        if (sum.respond_to?(:infinite?) && sum.infinite?) || (x.respond_to?(:infinite?) && x.infinite?) || (sum.respond_to?(:nan?) && sum.nan?) || (x.respond_to?(:nan?) && x.nan?)
          sum = sum + x
        else
          t = sum + x
          if sum.abs >= x.abs
            c += (sum - t) + x
          else
            c += (x - t) + sum
          end
          sum = t
        end
      else
        sum = sum + x
      end
    end
    has_float && !((sum.respond_to?(:infinite?) && sum.infinite?) || (sum.respond_to?(:nan?) && sum.nan?)) ? sum + c : sum
  end


end

