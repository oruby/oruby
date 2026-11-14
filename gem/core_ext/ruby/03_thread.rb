# Thread extensions for local storage
class Thread
  def self.current
    @__current ||= (allocate rescue new rescue Object.new)
  end unless respond_to?(:current)

  def [](key)
    @__locals ||= {}
    @__locals[key.to_s]
  end
  def []=(key, val)
    @__locals ||= {}
    @__locals[key.to_s] = val
  end
  def key?(key)
    @__locals ||= {}
    @__locals.key?(key.to_s)
  end
  def keys
    @__locals ||= {}
    @__locals.keys.map(&:to_sym)
  end
end
