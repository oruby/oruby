class Set
  alias __orig_init __init
  alias __orig_init_copy initialize_copy
  alias __orig_size size
  alias __orig_empty? empty?
  alias __orig_clear clear
  alias __orig_to_a to_a
  alias __orig_include? include?
  alias __orig_add add
  alias __orig_add? add?
  alias __orig_delete delete
  alias __orig_delete? delete?
  alias __orig_eq ==
  alias __orig_hash hash
  alias __orig_inspect inspect
  alias __orig_reset reset
  alias __orig_flatten flatten
  alias __orig_flatten! flatten!
  alias __orig_superset? superset?
  alias __orig_proper_superset? proper_superset?
  alias __orig_subset? subset?
  alias __orig_proper_subset? proper_subset?
  alias __orig_intersect? intersect?
  alias __orig_disjoint? disjoint?

  private def __check_iterating
    raise RuntimeError, "can't add a new key into hash during iteration" if @__iter && @__iter > 0
  end

  private def __new_like_self
    s = self.class.new
    s.compare_by_identity if @__cbi
    s
  end

  def compare_by_identity
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    unless @__cbi
      h = {}.compare_by_identity
      __orig_to_a.each { |e| h[e] = true }
      __orig_clear
      @__cbi = h
    end
    self
  end

  def compare_by_identity?
    !!@__cbi
  end

  def initialize_copy(orig)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __orig_init_copy(orig)
    if orig.instance_variable_get(:@__cbi)
      h = {}.compare_by_identity
      orig.instance_variable_get(:@__cbi).each_key { |k| h[k] = true }
      @__cbi = h
    else
      @__cbi = nil
    end
    self
  end
  private :initialize_copy

  def size
    @__cbi ? @__cbi.size : __orig_size
  end
  alias length size

  def empty?
    @__cbi ? @__cbi.empty? : __orig_empty?
  end

  def clear
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __check_iterating
    @__cbi ? @__cbi.clear : __orig_clear
    self
  end

  def to_a
    @__cbi ? @__cbi.keys : __orig_to_a
  end

  def include?(o)
    return @__cbi.include?(o) if @__cbi
    return true if __orig_include?(o)
    # mruby's Set asks the member (member.eql?(o)); MRI asks o. They differ
    # only for an o with an eql? of its own (singleton), which is looked for
    # among the members with its hash
    return false if o.__oruby_singleton_class_or_nil.nil?
    oh = o.hash
    __orig_to_a.any? { |e| e.hash == oh && o.eql?(e) }
  end
  alias member? include?
  alias === include?

  def add(o)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __check_iterating
    if @__cbi
      @__cbi[o] = true
      self
    else
      __orig_add(o)
    end
  end
  alias << add

  def add?(o)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __check_iterating
    if @__cbi
      return nil if @__cbi.include?(o)
      @__cbi[o] = true
      self
    else
      __orig_add?(o)
    end
  end

  def delete(o)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    if @__cbi
      @__cbi.delete(o)
      self
    else
      __orig_delete(o)
    end
  end

  def delete?(o)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    if @__cbi
      @__cbi.include?(o) ? (@__cbi.delete(o); self) : nil
    else
      __orig_delete?(o)
    end
  end

  def reset
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __check_iterating
    if @__cbi
      @__cbi.rehash
      self
    else
      __orig_reset
    end
  end

  def each(&block)
    return to_enum(:each) { size } unless block
    if frozen?
      to_a.each(&block)
      return self
    end
    @__iter = (@__iter || 0) + 1
    begin
      to_a.each(&block)
    ensure
      @__iter -= 1
    end
    self
  end

  def __do_with_enum(enum, &block)
    if enum.respond_to?(:each_entry)
      enum.each_entry(&block)
    elsif enum.respond_to?(:each)
      enum.each(&block)
    else
      raise ArgumentError, "value must be enumerable"
    end
  end

  def merge(*enums)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __check_iterating
    enums.each do |enum|
      if Set === enum && !@__cbi && !enum.compare_by_identity?
        __merge(enum)
      else
        __do_with_enum(enum) { |o| add(o) }
      end
    end
    self
  end

  def replace(enum)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __check_iterating
    if Set === enum
      if enum.compare_by_identity?
        @__cbi ||= {}.compare_by_identity
        @__cbi.clear
        __orig_clear
      else
        @__cbi = nil
        __orig_clear
      end
      merge(enum)
    else
      # validate enumerable before clearing
      raise ArgumentError, "value must be enumerable" unless enum.respond_to?(:each_entry) || enum.respond_to?(:each)
      clear
      merge(enum)
    end
    self
  end

  def subtract(enum)
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    __check_iterating
    if Set === enum && !@__cbi && !enum.compare_by_identity?
      __subtract(enum)
    else
      __do_with_enum(enum) { |o| delete(o) }
    end
    self
  end

  def intersection(enum)
    if Set === enum && !@__cbi && !enum.compare_by_identity?
      return __intersection(enum)
    end
    n = __new_like_self
    __do_with_enum(enum) { |o| n.add(o) if include?(o) }
    n
  end
  alias & intersection

  def union(enum)
    if Set === enum && !@__cbi && !enum.compare_by_identity?
      return __union(enum)
    end
    dup.merge(enum)
  end
  alias | union
  alias + union

  def difference(enum)
    if Set === enum && !@__cbi && !enum.compare_by_identity?
      return __difference(enum)
    end
    result = dup
    __do_with_enum(enum) { |o| result.delete(o) }
    result
  end
  alias - difference

  def ^(enum)
    s2 = Set === enum ? enum : Set.new(enum)
    res = __new_like_self
    each { |o| res.add(o) unless s2.include?(o) }
    s2.each { |o| res.add(o) unless include?(o) }
    res
  end

  def collect!
    return to_enum(:collect!) { size } unless block_given?
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    set = __new_like_self
    each { |o| set << yield(o) }
    replace(set)
  end
  alias map! collect!

  def classify
    return to_enum(:classify) { size } unless block_given?
    h = {}
    each do |i|
      x = yield(i)
      (h[x] ||= __new_like_self).add(i)
    end
    h
  end

  def divide(&func)
    return to_enum(:divide) { size } unless block_given?
    if func.arity == 2
      xs = to_a
      n = xs.size
      adj = Array.new(n) { [] }
      n.times do |i|
        n.times do |j|
          next if i == j
          adj[i] << j if func.call(xs[i], xs[j])
        end
      end
      # Tarjan's strongly connected components
      index = 0
      indices = Array.new(n)
      lowlink = Array.new(n)
      on_stack = Array.new(n, false)
      stack = []
      res = Set.new
      strongconnect = lambda do |v|
        indices[v] = index
        lowlink[v] = index
        index += 1
        stack.push(v)
        on_stack[v] = true
        adj[v].each do |w|
          if indices[w].nil?
            strongconnect.call(w)
            lowlink[v] = lowlink[w] if lowlink[w] < lowlink[v]
          elsif on_stack[w]
            lowlink[v] = indices[w] if indices[w] < lowlink[v]
          end
        end
        if lowlink[v] == indices[v]
          s = __new_like_self
          loop do
            w = stack.pop
            on_stack[w] = false
            s.add(xs[w])
            break if w == v
          end
          res.add(s)
        end
      end
      n.times { |v| strongconnect.call(v) if indices[v].nil? }
      return res
    end
    Set.new(classify(&func).values)
  end

  def flatten
    return __orig_flatten unless @__cbi
    target = __new_like_self
    __flatten_into(target, self, {})
    target
  end

  def flatten!
    return __orig_flatten! unless @__cbi
    return nil unless any? { |e| e.is_a?(Set) }
    raise FrozenError, "can't modify frozen Set: #{inspect}" if frozen?
    target = __new_like_self
    __flatten_into(target, self, {})
    replace(target)
  end

  private def __flatten_into(target, source, seen)
    if seen[source.__id__]
      raise ArgumentError, "tried to flatten recursive Set"
    end
    seen[source.__id__] = true
    source.each do |e|
      if e.is_a?(Set)
        __flatten_into(target, e, seen)
      else
        target.add(e)
      end
    end
    seen.delete(source.__id__)
  end

  def ==(other)
    return true if equal?(other)
    return false unless other.is_a?(Set)
    other_cbi = other.respond_to?(:compare_by_identity?) ? other.compare_by_identity? : false
    return false if compare_by_identity? != other_cbi
    return false unless size == other.size
    if @__cbi || !(Set === other)
      other.all? { |o| include?(o) }
    else
      __orig_eq(other)
    end
  end
  alias eql? ==

  def hash
    @__cbi ? @__cbi.hash : __orig_hash
  end

  def superset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return __orig_superset?(other) if Set === other && !@__cbi && !other.compare_by_identity?
    return true if equal?(other)
    size >= other.size && other.all? { |o| include?(o) }
  end
  alias >= superset?

  def proper_superset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return __orig_proper_superset?(other) if Set === other && !@__cbi && !other.compare_by_identity?
    size > other.size && other.all? { |o| include?(o) }
  end
  alias > proper_superset?

  def subset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return __orig_subset?(other) if Set === other && !@__cbi && !other.compare_by_identity?
    return true if equal?(other)
    size <= other.size && all? { |o| other.include?(o) }
  end
  alias <= subset?

  def proper_subset?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return __orig_proper_subset?(other) if Set === other && !@__cbi && !other.compare_by_identity?
    size < other.size && all? { |o| other.include?(o) }
  end
  alias < proper_subset?

  def <=>(other)
    return nil unless other.is_a?(Set)
    case size <=> other.size
    when -1 then proper_subset?(other) ? -1 : nil
    when 1  then proper_superset?(other) ? 1 : nil
    when 0  then self == other ? 0 : nil
    end
  end

  def intersect?(other)
    raise ArgumentError, "value must be a set" unless other.is_a?(Set)
    return __orig_intersect?(other) if Set === other && !@__cbi && !other.compare_by_identity?
    if size < other.size
      any? { |o| other.include?(o) }
    else
      other.any? { |o| include?(o) }
    end
  end

  def disjoint?(other)
    !intersect?(other)
  end

  def join(sep = nil)
    to_a.join(sep)
  end

  def inspect
    cname = self.class.name || Module.instance_method(:to_s).bind_call(self.class)
    return "#{cname}[]" if empty?
    Thread.current[:__set_inspect] ||= {}
    seen = Thread.current[:__set_inspect]
    return "#{cname}[...]" if seen[__id__]
    seen[__id__] = true
    begin
      "#{cname}[#{to_a.map(&:inspect).join(', ')}]"
    ensure
      seen.delete(__id__)
    end
  end
  alias to_s inspect

  def pretty_print(q)
    q.group(1, "#{self.class.name}[", "]") do
      q.seplist(self) { |o| q.pp(o) }
    end
  end

  def pretty_print_cycle(q)
    q.text(empty? ? "#{self.class.name}[]" : "#{self.class.name}[...]")
  end
end
