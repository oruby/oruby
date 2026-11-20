# ENV methods built on the Go accessors (__raw_keys, __raw_values, __raw_get, __raw_set, __raw_delete)
class << ENV
  include Enumerable

  private def __coerce_str(obj)
    return obj if String === obj
    if obj.respond_to?(:to_str)
      r = obj.to_str
      return r if String === r
      raise TypeError, "can't convert #{obj.class} into String (#{obj.class}#to_str gives #{r.class})"
    end
    raise TypeError, "no implicit conversion of #{obj.class} into String"
  end

  private def __try_str(obj)
    return obj if String === obj
    return nil unless obj.respond_to?(:to_str)
    r = obj.to_str
    return r if String === r
    raise TypeError, "can't convert #{obj.class} into String (#{obj.class}#to_str gives #{r.class})"
  end

  private def __coerce_hash(obj)
    return obj if Hash === obj
    if obj.respond_to?(:to_hash)
      r = obj.to_hash
      return r if Hash === r
      raise TypeError, "can't convert #{obj.class} into Hash (#{obj.class}#to_hash gives #{r.class})"
    end
    raise TypeError, "no implicit conversion of #{obj.class} into Hash"
  end

  private def __encode_str(s)
    return s if @__in_enc || !(defined?(Encoding) && Encoding.respond_to?(:find))
    @__in_enc = true
    begin
      loc = Encoding.find("locale") rescue nil
      s = s.dup.force_encoding(loc) if loc
      di = Encoding.respond_to?(:default_internal) ? Encoding.default_internal : nil
      if di && s.encoding != di
        s = s.encode(di) rescue s
      end
      s
    ensure
      @__in_enc = false
    end
  end

  def [](key)
    k = __coerce_str(key)
    v = __raw_get(k)
    v.nil? ? nil : __encode_str(v).freeze
  end
  alias get []

  def []=(key, val)
    k = __coerce_str(key)
    if val.nil?
      __raw_delete(k)
      return nil
    end
    v = __coerce_str(val)
    __raw_set(k, v)
    val
  end
  alias store []=

  def delete(key)
    k = __coerce_str(key)
    v = __raw_delete(k)
    if v.nil?
      block_given? ? yield(key) : nil
    else
      __encode_str(v)
    end
  end
  alias __delete delete

  def keys
    __raw_keys.map { |k| __encode_str(k) }
  end

  def values
    __raw_values.map { |v| __encode_str(v) }
  end

  def key?(key)
    k = __coerce_str(key)
    !__raw_get(k).nil?
  end
  alias has_key? key?
  alias include? key?
  alias member? key?

  def value?(val)
    v = __try_str(val)
    return nil if v.nil?
    __raw_values.include?(v)
  end
  alias has_value? value?

  def to_hash
    h = {}
    __raw_keys.each do |k|
      v = __raw_get(k)
      h[__encode_str(k).freeze] = __encode_str(v) unless v.nil?
    end
    h
  end

  def to_h(&block)
    block ? to_hash.to_h(&block) : to_hash
  end

  def to_a
    to_hash.to_a
  end

  def each_pair
    return to_enum(:each_pair) { size } unless block_given?
    __raw_keys.each do |k|
      v = __raw_get(k)
      yield __encode_str(k), __encode_str(v) unless v.nil?
    end
    self
  end
  alias each each_pair

  def each_key(&block)
    return to_enum(:each_key) { size } unless block
    keys.each(&block)
    self
  end

  def each_value(&block)
    return to_enum(:each_value) { size } unless block
    values.each(&block)
    self
  end

  def fetch(key, *default, &block)
    raise ArgumentError, "wrong number of arguments (given #{default.size + 1}, expected 1..2)" if default.size > 1
    warn("block supersedes default value argument", uplevel: 1) if !default.empty? && block
    k = __coerce_str(key)
    v = self[k]
    return v unless v.nil?
    return yield(k) if block
    return default[0] unless default.empty?
    raise KeyError.new("key not found: #{k.inspect}", receiver: self, key: k)
  end

  def fetch_values(*names, &block)
    names.map { |n| fetch(n, &block) }
  end

  def key(value)
    v = __coerce_str(value)
    to_hash.key(v)
  end

  def invert
    to_hash.invert
  end

  def assoc(key)
    k = __coerce_str(key)
    v = self[k]
    v.nil? ? nil : [__encode_str(k), v]
  end

  def rassoc(value)
    v = __try_str(value)
    return nil if v.nil?
    to_hash.rassoc(v)
  end

  def values_at(*names)
    names.map { |n| self[n] }
  end

  def slice(*names)
    h = {}
    names.each do |n|
      k = __coerce_str(n)
      v = self[k]
      h[n] = v unless v.nil?
    end
    h
  end

  def except(*names)
    h = to_hash
    names.each { |n| h.delete(__coerce_str(n)) }
    h
  end

  def select(&block)
    return to_enum(:select) { size } unless block
    to_hash.select(&block)
  end
  alias filter select

  def reject(&block)
    return to_enum(:reject) { size } unless block
    to_hash.reject(&block)
  end

  def filter_map(&block)
    to_hash.filter_map(&block)
  end

  def delete_if
    return to_enum(:delete_if) { size } unless block_given?
    to_hash.each { |k, v| delete(k) if yield(k, v) }
    self
  end

  def keep_if
    return to_enum(:keep_if) { size } unless block_given?
    to_hash.each { |k, v| delete(k) unless yield(k, v) }
    self
  end

  def select!(&block)
    return to_enum(:select!) { size } unless block
    before = size
    keep_if(&block)
    size == before ? nil : self
  end
  alias filter! select!

  def reject!(&block)
    return to_enum(:reject!) { size } unless block
    before = size
    delete_if(&block)
    size == before ? nil : self
  end

  def update(*hashes)
    hashes.each do |hash|
      h = __coerce_hash(hash)
      h.each do |k, v|
        ks = __coerce_str(k)
        v = yield(ks, self[ks], v) if block_given? && key?(ks)
        self[ks] = v
      end
    end
    self
  end
  alias merge! update

  def replace(hash)
    h = __coerce_hash(hash)
    existing = __raw_keys
    seen = {}
    h.each do |k, v|
      ks = __coerce_str(k)
      self[ks] = v
      seen[ks] = true
    end
    existing.each { |k| __raw_delete(k) unless seen[k] }
    self
  end

  def shift
    k = __raw_keys.first
    return nil unless k
    v = __raw_delete(k)
    [__encode_str(k), __encode_str(v)]
  end

  def rehash
    nil
  end

  def freeze
    raise TypeError, "cannot freeze ENV"
  end

  def inspect
    to_hash.inspect
  end

  def dup
    raise TypeError, "Cannot dup ENV, use ENV.to_h to get a copy of ENV as a hash"
  end

  def clone(freeze: nil)
    unless freeze.nil? || freeze == true || freeze == false
      raise ArgumentError, "unexpected value for freeze: #{freeze.inspect}"
    end
    raise TypeError, "Cannot clone ENV, use ENV.to_h to get a copy of ENV as a hash"
  end
end
