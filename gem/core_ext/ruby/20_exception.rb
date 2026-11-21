class Exception
  def self.to_tty?
    $stderr.tty? rescue false
  end

  alias __orig_exc_initialize initialize
  private :__orig_exc_initialize

  def initialize(msg = nil)
    if msg.nil?
      @__nil_mesg = true
    elsif instance_variable_defined?(:@__nil_mesg)
      remove_instance_variable(:@__nil_mesg)
    end
    __orig_exc_initialize(msg)
  end
  private :initialize

  alias __orig_exc_to_s to_s
  def to_s
    s = __orig_exc_to_s
    return Module.instance_method(:to_s).bind_call(self.class) if defined?(@__nil_mesg) && @__nil_mesg && s.empty?
    s
  end

  def message
    to_s
  end

  def inspect
    s = to_s
    cname = Module.instance_method(:to_s).bind_call(self.class)
    s.empty? ? cname : "#<#{cname}: #{s}>"
  end

  def ==(other)
    equal?(other) || (
      Exception === other &&
      self.class == other.class &&
      message == other.message &&
      backtrace == other.backtrace
    )
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
      if self.instance_of?(RuntimeError) && !(defined?(@__nil_mesg) && @__nil_mesg)
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

class NoMethodError
  alias __orig_nme_to_s to_s
  def to_s
    s = __orig_nme_to_s
    if instance_variable_defined?(:@receiver) && (m = /\Aundefined method '([^']+)' for (.*)\z/.__match_raw(s))
      recv = @receiver
      desc = if nil.equal?(recv)
               "nil"
             elsif true.equal?(recv)
               "true"
             elsif false.equal?(recv)
               "false"
             elsif Class === recv
               "class #{recv.name || Module.instance_method(:to_s).bind_call(recv)}"
             elsif Module === recv
               "module #{recv.name || Module.instance_method(:to_s).bind_call(recv)}"
             elsif !(Kernel === recv)
               "an instance of #{m[2]}"
             elsif !Kernel.instance_method(:singleton_methods).bind_call(recv, false).empty?
               Kernel.instance_method(:to_s).bind_call(recv)
             else
               cls = Kernel.instance_method(:class).bind_call(recv)
               "an instance of #{cls.name || Module.instance_method(:to_s).bind_call(cls)}"
             end
      return "undefined method '#{m[1]}' for #{desc}"
    end
    s
  end
end

class SyntaxError
  attr_reader :path
end

class LocalJumpError
  def exit_value
    @exit_value
  end

  def reason
    @reason || :return
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

class Object
  alias __orig_ivar_get instance_variable_get
  def instance_variable_get(name)
    __orig_ivar_get(name)
  rescue NameError => e
    e.instance_variable_set(:@name, name)
    e.instance_variable_set(:@receiver, self)
    raise
  end
end

class Module
  alias __orig_cvar_get class_variable_get
  def class_variable_get(name)
    __orig_cvar_get(name)
  rescue NameError => e
    e.instance_variable_set(:@name, name)
    e.instance_variable_set(:@receiver, self)
    raise
  end
end

class Hash
  alias __oruby_fetch fetch
  def fetch(key, *default, &block)
    if !default.empty? && block && default.size == 1
      warn("warning: block supersedes default value argument")
      return key?(key) ? self[key] : block.call(key)
    end
    return __oruby_fetch(key, *default, &block) if !default.empty? || block || key?(key)
    raise KeyError.new("key not found: #{key.inspect}", receiver: self, key: key)
  end
end
