module Kernel
  def warn(*messages, uplevel: nil, category: nil)
    # Warning.warn(msg) resolves here when Warning is the receiver
    return Warning::Base.instance_method(:warn).bind_call(self, *messages, category: category) if equal?(Warning) && defined?(Warning::Base)
    return nil if $VERBOSE.nil?
    msgs = messages.flatten
    return nil if msgs.empty?
    unless uplevel.nil?
      uplevel = Kernel.__to_int(uplevel)
      raise ArgumentError, "negative level (#{uplevel})" if uplevel < 0
      loc = (caller(uplevel + 1, 1) || [])[0]
      loc = loc.sub(/:in .*$/, "") if loc
      msgs[0] = "#{loc || 'warning'}: warning: #{msgs[0]}"
    end
    category = category.to_sym unless category.nil? || Symbol === category
    str = msgs.map { |m| s = m.to_s; s.end_with?("\n") ? s : s + "\n" }.join
    if Warning.method(:warn).parameters.any? { |t, _| t == :key || t == :keyreq || t == :keyrest }
      Warning.warn(str, category: category)
    else
      Warning.warn(str)
    end
    nil
  end

  alias __orig_to_enum to_enum
  def to_enum(meth = :each, *args, &block)
    enum = __orig_to_enum(meth, *args)
    if block
      enum.instance_variable_set(:@size_block, block)
    end
    enum
  end
  alias enum_for to_enum
end





module Kernel
  def __dir__
    loc = (caller(1, 1) || [])[0]
    return nil unless loc
    file = loc.sub(/:\d+.*$/, "")
    return nil if file == "-e" || file.empty?
    begin
      File.dirname(File.realpath(file))
    rescue
      File.dirname(file)
    end
  end
end

class Thread::Backtrace
  def self.limit
    $__backtrace_limit__ || -1
  end
end

class Thread
  # the current thread's stack; this method's own frame is the first, as in MRI
  def backtrace(*args)
    return nil unless equal?(Thread.current)
    args.empty? ? caller(0) : caller(*args)
  end

  def backtrace_locations(*args)
    return nil unless equal?(Thread.current)
    args.empty? ? caller_locations(0) : caller_locations(*args)
  end
end

class SystemExit < Exception
  attr_reader :status

  # MRI exit_initialize: a leading true/false/Integer (or #to_int) is the
  # status, the rest goes to Exception#initialize
  def initialize(*args)
    status = 0
    unless args.empty?
      a = args[0]
      if true.equal?(a)
        args.shift
      elsif false.equal?(a)
        status = 1
        args.shift
      elsif ::Integer === a
        status = args.shift
      elsif !a.nil? && ::Kernel.__responds?(a, :to_int) && ::Integer === (i = a.to_int)
        status = i
        args.shift
      end
    end
    super(*args)
    @status = status
  end

  def success?
    @status == 0
  end
end

module Kernel
  # MRI exit_status_code: true, false or an Integer that fits a C int
  def self.__exit_status(status)
    return 0 if true.equal?(status)
    return 1 if false.equal?(status)
    i = __to_int(status)
    raise RangeError, "integer #{i} too big to convert to 'int'" if i > 2147483647
    raise RangeError, "integer #{i} too small to convert to 'int'" if i < -2147483648
    i
  end

  def exit(status = true)
    raise SystemExit.new(::Kernel.__exit_status(status), "exit")
  end
  module_function :exit

  # MRI rb_f_abort: without a message, print the error being handled ($!);
  # with one, print it to $stderr and make it the SystemExit's message
  def abort(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)" if args.size > 1
    if args.empty?
      $stderr.write($!.full_message) if $!
      raise SystemExit.new(1, "exit")
    end
    msg = ::Kernel.__implicit(args[0], String)
    $stderr.puts(msg)
    raise SystemExit.new(1, msg)
  end
  module_function :abort
end

module Process
  def self.exit(status = true)
    raise SystemExit.new(::Kernel.__exit_status(status), "exit")
  end

  def self.abort(*args)
    ::Kernel.abort(*args)
  end

  def self.argv0
    @argv0 ||= ($0 ? $0.dup.freeze : "".freeze)
  end
end



# MRI defines Kernel#warn as a module function (also Kernel.warn)
module Kernel
  module_function :warn
end
