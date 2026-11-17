# MRI exception attributes mruby lacks: KeyError#receiver/#key and the
# receiver/key Hash#fetch attaches to the KeyError it raises.
class KeyError
  def initialize(msg = nil, receiver: (no_receiver = true; nil), key: (no_key = true; nil))
    super(msg)
    @receiver = receiver unless no_receiver
    @key = key unless no_key
  end

  def receiver
    raise ArgumentError, "no receiver is available" unless instance_variable_defined?(:@receiver)
    @receiver
  end

  def key
    raise ArgumentError, "no key is available" unless instance_variable_defined?(:@key)
    @key
  end
end

class Hash
  alias __oruby_fetch fetch
  def fetch(key, *default, &block)
    return __oruby_fetch(key, *default, &block) if !default.empty? || block || key?(key)
    raise KeyError.new("key not found: #{key.inspect}", receiver: self, key: key)
  end
end
