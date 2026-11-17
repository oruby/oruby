class Exception
  def self.to_tty?
    $stderr.tty? rescue false
  end

  def detailed_message(highlight: false, **opt)
    unless highlight == true || highlight == false
      raise ArgumentError, "expected true or false as highlight: #{highlight.inspect}"
    end
    msg = message.to_s
    cname = self.class.name
    anon = cname.nil? || cname.empty?
    display_class = anon ? self.class.to_s : cname

    if msg.empty?
      if self.instance_of?(RuntimeError)
        return highlight ? "\e[1;4munhandled exception\e[m" : "unhandled exception"
      end
      return highlight ? "\e[1;4m#{display_class}\e[m" : display_class
    end

    return msg if anon

    lines = msg.split("\n", -1)
    first = lines.shift || ""
    if highlight
      head = "\e[1m#{first} (\e[1;4m#{cname}\e[m\e[1m)\e[m"
      head += lines.map { |l| "\n\e[1m#{l}\e[m" }.join unless lines.empty?
      head
    else
      head = "#{first} (#{cname})"
      head += lines.map { |l| "\n#{l}" }.join unless lines.empty?
      head
    end
  end
end

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
