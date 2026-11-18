class Struct
  class << self
    alias __orig_new new
    def new(*args, **kw, &block)
      if !args.empty? && !args[0].nil? && !(Symbol === args[0])
        name = Kernel.__implicit(args[0], String)
        args[0] = name
        if name =~ /\A[A-Z]/ && const_defined?(name.to_sym, false)
          warn("warning: redefining constant Struct::#{name}")
          remove_const(name.to_sym)
        end
      end
      c = kw.empty? ? __orig_new(*args) : __orig_new(*args, **kw)
      __setup_struct_class(c)
      c.class_eval(&block) if block
      c
    end

    def inherited(sub)
      super
      __setup_struct_class(sub) unless sub.superclass == Struct
    end

    def __setup_struct_class(c)
      c.define_singleton_method(:new) do |*args, **kw, &blk|
        obj = allocate
        mems = (self.members rescue nil)
        obj[mems.size - 1] = nil if mems && !mems.empty?
        if kw.empty?
          obj.__send__(:initialize, *args, &blk)
        else
          obj.__send__(:initialize, *args, **kw, &blk)
        end
        obj
      end
      c.singleton_class.__send__(:alias_method, :[], :new)
    end
  end

  private :initialize

  def each(&block)
    return to_enum(:each) { size } unless block
    self.class.members.each { |field| yield(self[field]) }
    self
  end

  def each_pair(&block)
    return to_enum(:each_pair) { size } unless block
    self.class.members.each { |field| yield([field.to_sym, self[field]]) }
    self
  end

  def select(&block)
    return to_enum(:select) { size } unless block
    ary = []
    self.class.members.each do |field|
      val = self[field]
      ary.push(val) if yield(val)
    end
    ary
  end
  alias filter select

  def to_h(&blk)
    h = __to_h
    return h unless blk
    ret = {}
    h.each do |k, v|
      pair = blk.call(k, v)
      pair = Kernel.__implicit(pair, Array) if !(Array === pair) && pair.respond_to?(:to_ary)
      raise TypeError, "wrong element type #{Kernel.__cname(pair)} (expected array)" unless Array === pair
      raise ArgumentError, "element has wrong array length (expected 2, was #{pair.size})" if pair.size != 2
      ret[pair[0]] = pair[1]
    end
    ret
  end

  def dig(idx, *args)
    n = begin
      self[idx]
    rescue NameError, IndexError
      nil
    end
    return n if args.empty? || n.nil?
    raise TypeError, "#{n.class} does not have #dig method" unless n.respond_to?(:dig)
    n.dig(*args)
  end

  def deconstruct_keys(keys)
    return to_h if keys.nil?
    raise TypeError, "wrong argument type #{Kernel.__cname(keys)} (expected Array or nil)" unless Array === keys
    mems = members
    return {} if keys.size > mems.size
    res = {}
    keys.each do |k|
      case k
      when Symbol
        break unless mems.include?(k)
        res[k] = self[k]
      when String
        sym = k.to_sym
        break unless mems.include?(sym)
        res[k] = self[sym]
      else
        i = Kernel.__to_int(k)
        idx = i < 0 ? mems.size + i : i
        break if idx < 0 || idx >= mems.size
        res[k] = self[idx]
      end
    end
    res
  end

  def values_at(*args)
    n = size
    out = []
    args.each do |a|
      if Range === a
        b = a.begin.nil? ? 0 : Kernel.__to_int(a.begin)
        b += n if b < 0
        raise RangeError, "#{a} out of range" if b < 0
        if a.end.nil?
          e = n - 1
        else
          e = Kernel.__to_int(a.end)
          e += n if e < 0
          e -= 1 if a.exclude_end?
        end
        next if e < b
        (b..e).each { |i| out << (i < n ? self[i] : nil) }
      else
        i = Kernel.__to_int(a)
        idx = i < 0 ? n + i : i
        raise IndexError, "offset #{i} too small for struct(size:#{n})" if idx < 0
        raise IndexError, "offset #{i} too large for struct(size:#{n})" if idx >= n
        out << self[idx]
      end
    end
    out
  end

  def hash
    Thread.current[:__array_hash_stack] ||= []
    stack = Thread.current[:__array_hash_stack]
    return :cycle if stack.include?(object_id)
    stack.push(object_id)
    has_cycle = false
    h = self.class.hash & 0x3fffffff
    begin
      values.each do |elem|
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

  def inspect
    cname = Module.instance_method(:to_s).bind_call(self.class)
    anon = cname.start_with?("#<")
    head = anon ? "#<struct" : "#<struct #{cname}"
    Thread.current[:__struct_inspect] ||= {}
    seen = Thread.current[:__struct_inspect]
    return "#{head}:...>" if seen[__id__]
    seen[__id__] = true
    begin
      fields = members.map { |m| "#{m}=#{self[m].inspect}" }.join(", ")
      fields.empty? ? "#{head}>" : "#{head} #{fields}>"
    ensure
      seen.delete(__id__)
    end
  end
  alias to_s inspect
end

class Data
  class << self
    alias __orig_define define
    def define(*members, &block)
      c = __orig_define(*members)
      __setup_subclass(c)
      c.class_eval(&block) if block
      c
    end

    def inherited(sub)
      super
      __setup_subclass(sub)
    end

    def __setup_subclass(c)
      c.define_singleton_method(:new) do |*args, **kwargs, &blk|
        mems = self.members
        if !args.empty?
          if args.size > mems.size
            raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..#{mems.size})"
          end
          unless kwargs.empty?
            raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0)"
          end
          kw = {}
          args.each_with_index { |v, i| kw[mems[i]] = v }
          obj = allocate
          obj.__send__(:initialize, **kw, &blk)
          obj
        else
          obj = allocate
          obj.__send__(:initialize, **kwargs, &blk)
          obj
        end
      end
      c.singleton_class.__send__(:alias_method, :[], :new)
    end
  end

  alias __orig_initialize initialize
  def initialize(*args, **kwargs)
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    unless args.empty?
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0)"
    end
    mems = self.class.members
    norm = {}
    unknown = []
    kwargs.each do |k, v|
      case k
      when Symbol
        if mems.include?(k)
          norm[k] = v
        else
          unknown << k.inspect
        end
      when String
        sym = k.to_sym
        if mems.include?(sym)
          norm[sym] = v
        else
          unknown << k.inspect
        end
      else
        raise TypeError, "#{k.inspect} is not a symbol nor a string" unless k.respond_to?(:to_str)
        s = Kernel.__implicit(k, String)
        sym = s.to_sym
        if mems.include?(sym)
          norm[sym] = v
        else
          unknown << s.inspect
        end
      end
    end
    unless unknown.empty?
      raise ArgumentError, "unknown keyword#{unknown.size > 1 ? 's' : ''}: #{unknown.join(', ')}"
    end
    missing = mems.reject { |m| norm.key?(m) }
    unless missing.empty?
      raise ArgumentError, "missing keyword#{missing.size > 1 ? 's' : ''}: #{missing.map(&:inspect).join(', ')}"
    end
    __orig_initialize(**norm)
  end
  private :initialize

  def with(*args, **kwargs)
    unless args.empty?
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0)"
    end
    return self if kwargs.empty?
    base = {}
    __to_h.each { |k, v| base[k] = v }
    kwargs.each do |k, v|
      base.delete(k.to_sym) if String === k
      base[k] = v
    end
    obj = self.class.allocate
    obj.__send__(:initialize, **base)
    obj
  end

  def to_h(&blk)
    h = __to_h
    return h unless blk
    ret = {}
    h.each do |k, v|
      pair = blk.call(k, v)
      pair = Kernel.__implicit(pair, Array) if !(Array === pair) && pair.respond_to?(:to_ary)
      raise TypeError, "wrong element type #{Kernel.__cname(pair)} (expected array)" unless Array === pair
      raise ArgumentError, "element has wrong array length (expected 2, was #{pair.size})" if pair.size != 2
      ret[pair[0]] = pair[1]
    end
    ret
  end

  def deconstruct_keys(keys)
    return __to_h if keys.nil?
    raise TypeError, "wrong argument type #{Kernel.__cname(keys)} (expected Array or nil)" unless Array === keys
    mems = members
    return {} if keys.size > mems.size
    h = __to_h
    res = {}
    keys.each do |k|
      case k
      when Symbol
        break unless mems.include?(k)
        res[k] = h[k]
      when String
        sym = k.to_sym
        break unless mems.include?(sym)
        res[k] = h[sym]
      else
        raise TypeError, "#{k.inspect} is not a symbol nor a string" unless k.respond_to?(:to_str)
        s = Kernel.__implicit(k, String)
        sym = s.to_sym
        break unless mems.include?(sym)
        res[s] = h[sym]
      end
    end
    res
  end

  def ==(other)
    return true if equal?(other)
    return false unless self.class.equal?(other.class)
    pair = [__id__, other.__id__]
    Thread.current[:__data_eq] ||= {}
    seen = Thread.current[:__data_eq]
    return true if seen[pair]
    seen[pair] = true
    begin
      a = deconstruct
      b = other.deconstruct
      return false unless a.size == b.size
      a.each_index { |i| return false unless a[i] == b[i] }
      true
    ensure
      seen.delete(pair)
    end
  end

  def eql?(other)
    return true if equal?(other)
    return false unless self.class.equal?(other.class)
    pair = [__id__, other.__id__]
    Thread.current[:__data_eql] ||= {}
    seen = Thread.current[:__data_eql]
    return true if seen[pair]
    seen[pair] = true
    begin
      a = deconstruct
      b = other.deconstruct
      return false unless a.size == b.size
      a.each_index { |i| return false unless a[i].eql?(b[i]) }
      true
    ensure
      seen.delete(pair)
    end
  end

  def hash
    Thread.current[:__array_hash_stack] ||= []
    stack = Thread.current[:__array_hash_stack]
    return :cycle if stack.include?(object_id)
    stack.push(object_id)
    has_cycle = false
    h = self.class.hash & 0x3fffffff
    begin
      deconstruct.each do |elem|
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

  def inspect
    cname = Module.instance_method(:to_s).bind_call(self.class)
    anon = cname.start_with?("#<")
    Thread.current[:__data_inspect] ||= {}
    seen = Thread.current[:__data_inspect]
    if seen[__id__]
      return "#<data #{cname}:...>"
    end
    seen[__id__] = true
    begin
      h = __to_h
      fields = members.map { |m| "#{m}=#{h[m].inspect}" }.join(", ")
      head = anon ? "#<data" : "#<data #{cname}"
      fields.empty? ? "#{head}>" : "#{head} #{fields}>"
    ensure
      seen.delete(__id__)
    end
  end
  alias to_s inspect
end
