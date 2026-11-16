# ENV methods built on the Go accessors (keys, [], []=, delete)
class << ENV
  include Enumerable

  def to_hash
    h = {}
    keys.each { |k| h[k] = self[k] }
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
    to_hash.each { |k, v| yield k, v }
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

  def fetch(key, *default)
    raise ArgumentError, "wrong number of arguments (given #{default.size + 1}, expected 1..2)" if default.size > 1
    v = self[key]
    return v unless v.nil?
    return yield(key) if block_given?
    return default[0] unless default.empty?
    raise KeyError.new("key not found: #{key.inspect}", receiver: self, key: key)
  end

  def key(value)
    to_hash.key(value)
  end

  def invert
    to_hash.invert
  end

  def assoc(key)
    v = self[key]
    v.nil? ? nil : [key, v]
  end

  def rassoc(value)
    to_hash.rassoc(value)
  end

  def values_at(*names)
    names.map { |n| self[n] }
  end

  def slice(*names)
    names.each_with_object({}) { |n, h| v = self[n]; h[n] = v unless v.nil? }
  end

  def except(*names)
    to_hash.reject { |k, _| names.include?(k) }
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
    before = size
    keep_if(&block)
    size == before ? nil : self
  end
  alias filter! select!

  def reject!(&block)
    before = size
    delete_if(&block)
    size == before ? nil : self
  end

  def update(*hashes)
    hashes.each do |h|
      h.each do |k, v|
        v = yield(k, self[k], v) if block_given? && key?(k)
        self[k] = v
      end
    end
    self
  end
  alias merge! update

  def replace(hash)
    keys.each { |k| delete(k) unless hash.key?(k) }
    hash.each { |k, v| self[k] = v }
    self
  end

  def shift
    k = keys.first
    return nil unless k
    v = self[k]
    delete(k)
    [k, v]
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
    raise TypeError, "Cannot clone ENV, use ENV.to_h to get a copy of ENV as a hash"
  end
end
