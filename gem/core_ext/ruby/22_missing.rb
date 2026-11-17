# MRI core methods mruby lacks
module Enumerable
  def slice_when(&block)
    raise ArgumentError, "tried to create Proc object without a block" unless block
    chunk_while { |a, b| !block.call(a, b) }
  end
end

class Module
  private def ruby2_keywords(*names)
    nil
  end
end

class Proc
  def ruby2_keywords
    self
  end
end

class Integer
  # bit reference: int[n], int[n, len], int[range]
  def [](i, len = (no_len = true; nil))
    if no_len
      if Range === i
        b = i.begin.nil? ? 0 : Kernel.__to_int(i.begin)
        if i.end.nil?
          raise ArgumentError, "The beginning of the range should be 0" if b < 0 && self != 0
          return self >> b
        end
        e = Kernel.__to_int(i.end)
        e -= 1 if i.exclude_end?
        return self >> b if e < b && b >= 0
        len = e - b + 1
        return (self >> b) & ((1 << len) - 1) if len > 0
        return self >> b
      end
      i = Kernel.__to_int(i)
      return 0 if i < 0
      return (self >> i) & 1
    end
    i = Kernel.__to_int(i)
    len = Kernel.__to_int(len)
    raise ArgumentError, "negative length" if len < 0 && false
    return self >> i if len < 0
    (self >> i) & ((1 << len) - 1)
  end
end

class Exception
  def full_message(highlight: false, order: :top)
    unless highlight == true || highlight == false || highlight.nil?
      raise ArgumentError, "expected true or false as highlight: #{highlight.inspect}"
    end
    unless order == :top || order == :bottom || order.nil?
      raise ArgumentError, "expected :top or :bottom as order: #{order.inspect}"
    end
    order ||= :top
    bt = backtrace
    bt = caller(1) if bt.nil? || bt.empty?
    msg = message.to_s
    cls = self.class.to_s
    lines = msg.split("\n", -1)
    first = lines.shift || ""
    if msg.empty?
      head = highlight ? "\e[1;4munhandled exception\e[m" : "unhandled exception"
      head = cls if cls != "RuntimeError" && !highlight
      head = "\e[1;4m#{cls}\e[m" if cls != "RuntimeError" && highlight
    elsif highlight
      head = "\e[1m#{first} (\e[1;4m#{cls}\e[m\e[1m)\e[m"
      head += lines.map { |l| "\n\e[1m#{l}\e[m" }.join
    else
      head = "#{first} (#{cls})"
      head += lines.map { |l| "\n#{l}" }.join
    end
    top = bt.first ? "#{bt.first}: " : ""
    rest = bt.drop(1)
    if order == :top
      "#{top}#{head}\n" + rest.map { |l| "\tfrom #{l}\n" }.join
    else
      w = rest.size.to_s.size
      tb = highlight ? "\e[1mTraceback\e[m (most recent call last):\n" : "Traceback (most recent call last):\n"
      tb + rest.each_with_index.map { |l, i| "\t#{(i + 1).to_s.rjust(w)}: from #{l}\n" }.reverse.join + "#{top}#{head}\n"
    end
  end
end

class String
  def lines(sep = $/, chomp: false, &block)
    if block
      each_line(sep, chomp: chomp, &block)
      return self
    end
    __lines(sep, chomp)
  end

  def each_line(sep = $/, chomp: false, &block)
    return Enumerator.new { |y| __lines(sep, chomp).each { |l| y << l } } unless block
    __lines(sep, chomp).each(&block)
    self
  end

  private def __lines(sep, chomp)
    return [self.dup] if sep.nil? && !empty?
    return [] if empty?
    sep = Kernel.__implicit(sep, String)
    out = []
    if sep.empty? # paragraph mode
      rest = self
      while (m = rest.match(/\n\n+/))
        out << (chomp ? m.pre_match : m.pre_match + m[0])
        rest = m.post_match
      end
      out << (chomp ? rest.chomp : rest) unless rest.empty?
      return out
    end
    pos = 0
    n = bytesize
    sl = sep.bytesize
    while pos < n
      i = byteindex(sep, pos)
      if i.nil?
        out << byteslice(pos, n - pos)
        break
      end
      line = byteslice(pos, i + sl - pos)
      if chomp
        line = byteslice(pos, i - pos)
        line = line.byteslice(0, line.bytesize - 1) if sep == "\n" && line.end_with?("\r")
      end
      out << line
      pos = i + sl
    end
    out
  end

  # case mapping options: :ascii, :turkic, :lithuanian, :fold
  %i[downcase upcase capitalize swapcase].each do |m|
    um = instance_method(m)
    define_method(m) do |*opts|
      String.__case_opts(opts, m)
      if opts.include?(:ascii)
        String.__ascii_case(self, m)
      elsif opts.include?(:turkic)
        String.__turkic_case(self, m, um)
      else
        um.bind_call(self)
      end
    end
    bm = :"#{m}!"
    bum = instance_method(bm)
    define_method(bm) do |*opts|
      String.__case_opts(opts, m)
      if opts.include?(:ascii) || opts.include?(:turkic)
        r = __send__(m, *opts)
        return nil if r == self
        replace(r)
      else
        bum.bind_call(self)
      end
    end
  end

  def self.__case_opts(opts, m)
    raise ArgumentError, "too many options" if opts.size > 2
    opts.each do |o|
      unless o == :ascii || o == :turkic || o == :lithuanian || (o == :fold && m == :downcase)
        raise ArgumentError, "invalid option: #{o.inspect}" if Symbol === o
        raise TypeError, "no implicit conversion of #{Kernel.__cname(o)} into Symbol"
      end
    end
    if opts.size == 2 && !(opts.include?(:turkic) && opts.include?(:lithuanian))
      raise ArgumentError, "too many options"
    end
  end

  def self.__ascii_case(s, m)
    case m
    when :downcase then s.tr("A-Z", "a-z")
    when :upcase then s.tr("a-z", "A-Z")
    when :swapcase then s.tr("a-zA-Z", "A-Za-z")
    else
      s.empty? ? s.dup : s[0].tr("a-z", "A-Z") + s[1..].tr("A-Z", "a-z")
    end
  end

  def self.__turkic_case(s, m, um)
    case m
    when :downcase then um.bind_call(s.gsub("I", "ı").gsub("İ", "i"))
    when :upcase then um.bind_call(s.gsub("i", "İ"))
    when :swapcase
      s.each_char.map { |c| c == "i" ? "İ" : c == "I" ? "ı" : c == "İ" ? "i" : c == "ı" ? "I" : c.swapcase }.join
    else
      return s.dup if s.empty?
      f = s[0] == "i" ? "İ" : s[0].upcase
      f + __turkic_case(s[1..], :downcase, String.instance_method(:downcase))
    end
  end
end

class Enumerator
  def self.product(*enums, **kw, &block)
    raise ArgumentError, "unknown keywords: #{kw.keys.map(&:inspect).join(', ')}" unless kw.empty?
    e = Product.new(*enums)
    return e.each(&block) && nil if block
    e
  end

  class Product < Enumerator
    def initialize(*enums)
      raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
      @__enums = enums
      @__init = true
      super() { |y| __each { |v| y << v } }
    end
    private :initialize

    def initialize_copy(other)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      return self if equal?(other)
      raise TypeError, "wrong argument class" unless Product === other
      raise ArgumentError, "uninitialized product" unless other.instance_variable_get(:@__init)
      @__enums = other.instance_variable_get(:@__enums).dup
      @__init = true
      super
      self
    end
    private :initialize_copy

    def each(*args, &block)
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0)" unless args.empty?
      return self unless block
      __each(&block)
      self
    end

    def size
      total = 1
      @__enums.each do |e|
        return nil unless e.respond_to?(:size)
        s = e.size
        return nil if s.nil? || (Float === s && s.nan?)
        return nil unless Integer === s || (Float === s && s.infinite?)
        total *= s
      end
      total
    end

    def rewind
      @__enums.reverse_each { |e| e.rewind if e.respond_to?(:rewind) }
      super
    end

    def inspect
      return "#<#{self.class}: uninitialized>" unless @__init
      "#<#{self.class}: #{@__enums.inspect}>"
    end
    alias to_s inspect

    private def __each(&block)
      enums = @__enums
      if enums.empty?
        block.call([])
        return
      end
      rec = lambda do |i, acc|
        if i == enums.size
          block.call(acc.dup)
        else
          enums[i].each_entry { |v| rec.call(i + 1, acc + [v]) }
        end
      end
      rec.call(0, [])
    end
  end
end

# Random#bytes / Random.bytes return binary strings
class Random
  if method_defined?(:bytes)
    alias __oruby_bytes bytes
    def bytes(n)
      __oruby_bytes(Kernel.__to_int(n)).force_encoding(Encoding::BINARY)
    end
  end
  class << self
    if method_defined?(:bytes)
      alias __oruby_bytes bytes
      def bytes(n)
        __oruby_bytes(Kernel.__to_int(n)).force_encoding(Encoding::BINARY)
      end
    end
  end
end

class Symbol
  def match(*args, &block) = to_s.match(*args, &block)
  def match?(*args) = to_s.match?(*args)
  def =~(other) = to_s =~ other
end

# open(2) flags MRI defines everywhere (0 where the platform has no such flag)
module File::Constants
  BINARY = 0 unless const_defined?(:BINARY)
  SHARE_DELETE = 0 unless const_defined?(:SHARE_DELETE)
end
class IO
  BINARY = File::Constants::BINARY unless const_defined?(:BINARY, false)
end

# ConditionVariable for ORuby's single thread: a wait can only time out
# (without a timeout nothing could ever signal it, so it is MRI's deadlock)
class Thread
  class ConditionVariable
    def wait(mutex, timeout = nil)
      raise ThreadError, "No live threads left. Deadlock?" if timeout.nil?
      mutex.unlock
      begin
        sleep(timeout)
      ensure
        mutex.lock
      end
      self
    end

    def signal = self
    def broadcast = self
  end
end
ConditionVariable = Thread::ConditionVariable unless Object.const_defined?(:ConditionVariable)


# mruby's bind_call passes keywords as a positional Hash; bind(...).call
# keeps them apart
class UnboundMethod
  def bind_call(recv, *args, **kw, &block)
    kw.empty? ? bind(recv).call(*args, &block) : bind(recv).call(*args, **kw, &block)
  end
end

# spawn / system / exec argument checks as MRI does them before the Go
# runner (gem/process) sees them: #to_str, null bytes, [command, argv0]
module Process
  def self.__cmd_args(args)
    out = args.dup
    i = Hash === out[0] ? 1 : 0
    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if out.size <= i
    c = out[i]
    if Array === c
      raise ArgumentError, "wrong first argument" unless c.size == 2
      out[i] = c.map { |x| __cmd_str(x) }
    else
      out[i] = __cmd_str(c)
    end
    ((i + 1)...out.size).each { |j| out[j] = __cmd_str(out[j]) }
    out
  end

  def self.__cmd_str(x)
    s = Kernel.__implicit(x, String)
    raise ArgumentError, "string contains null byte" if s.include?("\0")
    s
  end
end

[[Process.singleton_class, :spawn], [Process.singleton_class, :exec], [Kernel, :spawn], [Kernel, :system], [Kernel, :exec]].each do |mod, m|
  next unless mod.method_defined?(m) || mod.private_method_defined?(m)
  um = mod.instance_method(m)
  priv = mod.private_method_defined?(m)
  mod.__send__(:define_method, m) do |*args, **opts, &blk|
    a = Process.__cmd_args(args)
    opts.empty? ? um.bind(self).call(*a, &blk) : um.bind(self).call(*a, **opts, &blk)
  end
  mod.__send__(:private, m) if priv
end
Kernel.__send__(:module_function, :spawn, :system, :exec) rescue nil
