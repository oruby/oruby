class Hash
  # Avoid calling self.size or self[] inside each: Hash subclasses (e.g.
  # Zip::ExtraField overriding #size, Mail::ParameterHash overriding #[])
  # otherwise infinitely recurse through Hash#each.
  alias __oruby_hash_keys keys
  alias __oruby_hash_aref []
  alias __oruby_hash_key_p key?
  private :__oruby_hash_keys, :__oruby_hash_aref, :__oruby_hash_key_p

  def each(&block)
    return to_enum(:each) { size } unless block
    ks = __oruby_hash_keys
    i = 0
    len = ks.length
    while i < len
      k = ks[i]
      if __oruby_hash_key_p(k)
        yield [k, __oruby_hash_aref(k)]
      end
      i += 1
    end
    self
  end
  alias each_pair each

  alias __orig_hash hash
  def hash
    Thread.current[:__hash_hash_stack] ||= []
    stack = Thread.current[:__hash_hash_stack]
    return :cycle if stack.include?(object_id)
    stack.push(object_id)
    has_cycle = false
    h = size & 0x3fffffff
    begin
      each do |k, v|
        kh = k.hash
        vh = v.hash
        if kh == :cycle || vh == :cycle
          has_cycle = true
          kh = 0 if kh == :cycle
          vh = 0 if vh == :cycle
        else
          kh = kh.to_int if !kh.is_a?(Integer) && (kh.respond_to?(:to_int) rescue false)
          vh = vh.to_int if !vh.is_a?(Integer) && (vh.respond_to?(:to_int) rescue false)
        end
        pair_h = (kh.to_i ^ (vh.to_i * 31)) & 0x3fffffff
        h = (h + pair_h) & 0x3fffffff
      end
    ensure
      stack.pop
    end
    return :cycle if has_cycle && (stack.any? || (Thread.current[:__array_hash_stack] && Thread.current[:__array_hash_stack].any?))
    return 0 if has_cycle
    h
  end

  def self.ruby2_keywords_hash?(h)
    raise TypeError, "wrong argument type #{Kernel.__cname(h)} (expected Hash)" unless Hash === h
    !!h.instance_variable_get(:@__ruby2_keywords)
  end

  def self.ruby2_keywords_hash(h)
    raise TypeError, "wrong argument type #{Kernel.__cname(h)} (expected Hash)" unless Hash === h
    copy = h.dup
    copy.instance_variable_set(:@__ruby2_keywords, true)
    copy
  end

  alias __orig_init_copy initialize_copy
  def initialize_copy(other)
    __orig_init_copy(other)
    # a copy is no ruby2_keywords Hash (only cleared when set: an ivar table
    # on every copy makes go-fast.c's merge take the slow path)
    remove_instance_variable(:@__ruby2_keywords) if instance_variable_defined?(:@__ruby2_keywords)
    self
  end
  private :initialize_copy

  def self.[](*args)
    h = allocate
    return h if args.empty?
    if args.size == 1
      arg = args[0]
      if Hash === arg || (!(Array === arg) && arg.respond_to?(:to_hash))
        th = Hash === arg ? arg : arg.to_hash
        unless th.nil?
          unless Hash === th
            cname = Kernel.__cname(arg)
            raise TypeError, "can't convert #{cname} into Hash (#{cname}#to_hash gives #{Kernel.__cname(th)})"
          end
          th.each { |k, v| h[k] = v }
          return h
        end
      end
      if Array === arg || arg.respond_to?(:to_ary)
        ta = Array === arg ? arg : arg.to_ary
        unless ta.nil?
          unless Array === ta
            cname = Kernel.__cname(arg)
            raise TypeError, "can't convert #{cname} into Array (#{cname}#to_ary gives #{Kernel.__cname(ta)})"
          end
          ta.each_with_index do |el, idx|
            el = el.to_ary if !(Array === el) && el.respond_to?(:to_ary)
            unless Array === el
              tname = el.nil? ? "nil" : (true.equal?(el) ? "true" : (false.equal?(el) ? "false" : Kernel.__cname(el)))
              raise ArgumentError, "wrong element type #{tname} at #{idx} (expected array)"
            end
            if el.size < 1 || el.size > 2
              raise ArgumentError, "invalid number of elements (#{el.size} for 1..2)"
            end
            h[el[0]] = el[1]
          end
          return h
        end
      end
    end
    raise ArgumentError, "odd number of arguments for Hash" if args.size.odd?
    i = 0
    while i < args.size
      h[args[i]] = args[i + 1]
      i += 2
    end
    h
  end

  alias __orig_initialize initialize
  def initialize(*args, **kw, &block)
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    unless kw.empty?
      if args.empty? && !block && kw.key?(:capacity) && kw.size == 1
        cap = kw[:capacity]
        Kernel.__to_int(cap) unless cap.nil?
        kw = {}
      else
        # If no positional args and kwargs isn't just capacity:, check if unknown keyword or positional hash
        if kw.key?(:capacity)
          other_keys = kw.keys - [:capacity]
          raise ArgumentError, "unknown keyword: #{other_keys.first.inspect}" unless other_keys.empty?
          cap = kw[:capacity]
          Kernel.__to_int(cap) unless cap.nil?
        elsif !args.empty?
          args = args + [kw]
        else
          raise ArgumentError, "unknown keyword: #{kw.keys.first.inspect}"
        end
      end
    end
    self.default = nil if args.empty? && !block
    __orig_initialize(*args, &block)
  end
  private :initialize

  alias __orig_default_proc_set default_proc=
  def default_proc=(pr)
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    if !pr.nil? && !(Proc === pr)
      unless pr.respond_to?(:to_proc)
        raise TypeError, "wrong default_proc type #{Kernel.__cname(pr)} (expected Proc)"
      end
      res = pr.to_proc
      unless Proc === res
        cname = Kernel.__cname(pr)
        raise TypeError, "can't convert #{cname} into Proc (#{cname}#to_proc gives #{Kernel.__cname(res)})"
      end
      pr = res
    end
    __orig_default_proc_set(pr)
  end

  def assoc(obj)
    each { |k, v| return [k, v] if obj == k }
    nil
  end

  def rassoc(obj)
    each { |k, v| return [k, v] if obj == v }
    nil
  end

  def keep_if(&block)
    return to_enum(:keep_if) { size } unless block
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    each { |k, v| delete(k) unless yield(k, v) }
    self
  end

  def delete_if(&block)
    return to_enum(:delete_if) { size } unless block
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    each { |k, v| delete(k) if yield(k, v) }
    self
  end

  def select(&block)
    return to_enum(:select) { size } unless block
    h = {}
    h.compare_by_identity if compare_by_identity?
    each { |k, v| h[k] = v if yield(k, v) }
    h
  end
  alias filter select

  def select!(&block)
    return to_enum(:select!) { size } unless block
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    n = size
    each { |k, v| delete(k) unless yield(k, v) }
    size == n ? nil : self
  end
  alias filter! select!

  def reject(&block)
    return to_enum(:reject) { size } unless block
    h = {}
    h.compare_by_identity if compare_by_identity?
    each { |k, v| h[k] = v unless yield(k, v) }
    h
  end

  def reject!(&block)
    return to_enum(:reject!) { size } unless block
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    n = size
    each { |k, v| delete(k) if yield(k, v) }
    size == n ? nil : self
  end

  def invert
    h = {}
    each { |k, v| h[v] = k }
    h
  end

  def to_proc
    h = self
    ->(x) { h[x] }
  end

  def to_h(&blk)
    unless blk
      return self if instance_of?(Hash)
      h = default_proc ? Hash.new(&default_proc) : Hash.new(default)
      h.compare_by_identity if compare_by_identity?
      each { |k, v| h[k] = v }
      return h
    end
    h = {}
    each do |k, v|
      pair = blk.call(k, v)
      pair = Kernel.__implicit(pair, Array) if !(Array === pair) && pair.respond_to?(:to_ary)
      raise TypeError, "wrong element type #{Kernel.__cname(pair)} (expected array)" unless Array === pair
      raise ArgumentError, "element has wrong array length (expected 2, was #{pair.size})" if pair.size != 2
      h[pair[0]] = pair[1]
    end
    h
  end

  def merge!(*others, &block)
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    others.each do |other|
      other = Kernel.__implicit(other, Hash) unless Hash === other
      if block
        other.each { |k, v| self[k] = key?(k) ? yield(k, self[k], v) : v }
      else
        __merge(other)
      end
    end
    self
  end
  alias update merge!

  def merge(*others, &block)
    dup.merge!(*others, &block)
  end

  alias __orig_replace replace
  def replace(other)
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    other = Kernel.__implicit(other, Hash) unless Hash === other
    __orig_replace(other)
  end

  alias __orig_lt <
  def <(other)
    other = Kernel.__implicit(other, Hash) unless Hash === other
    __orig_lt(other)
  end

  alias __orig_le <=
  def <=(other)
    other = Kernel.__implicit(other, Hash) unless Hash === other
    __orig_le(other)
  end

  alias __orig_gt >
  def >(other)
    other = Kernel.__implicit(other, Hash) unless Hash === other
    __orig_gt(other)
  end

  alias __orig_ge >=
  def >=(other)
    other = Kernel.__implicit(other, Hash) unless Hash === other
    __orig_ge(other)
  end

  def transform_keys(hash2 = (no_hash = true; nil), &block)
    return to_enum(:transform_keys) { size } if no_hash && !block
    hash2 = Kernel.__implicit(hash2, Hash) unless no_hash || Hash === hash2
    res = {}
    each do |k, v|
      new_k = if !no_hash && hash2.key?(k)
                hash2[k]
              elsif block
                block.call(k)
              else
                k
              end
      res[new_k] = v
    end
    res
  end

  def transform_keys!(hash2 = (no_hash = true; nil), &block)
    return to_enum(:transform_keys!) { size } if no_hash && !block
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    hash2 = Kernel.__implicit(hash2, Hash) unless no_hash || Hash === hash2
    orig = to_a
    transformed = []
    i = 0
    raised = false
    begin
      while i < orig.size
        k, v = orig[i]
        i += 1
        new_k = if !no_hash && hash2.key?(k)
                  hash2[k]
                elsif block
                  yield(k)
                else
                  k
                end
        transformed << [new_k, v]
      end
    rescue Exception
      raised = true
      raise
    ensure
      unless raised
        clear
        transformed.each { |nk, nv| self[nk] = nv }
        while i < orig.size
          rk, rv = orig[i]
          self[rk] = rv unless key?(rk)
          i += 1
        end
      end
    end
    self
  end
end
