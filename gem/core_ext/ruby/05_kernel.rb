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

  def initialize(*args)
    if args.empty?
      @status = 0
      super("SystemExit")
    elsif args.size == 1
      arg = args[0]
      if arg == true
        @status = 0
        super("SystemExit")
      elsif arg == false
        @status = 1
        super("SystemExit")
      elsif arg.is_a?(Numeric)
        @status = arg.to_i
        super("SystemExit")
      else
        @status = 0
        super(arg.to_s)
      end
    else
      status_arg, msg = args[0], args[1]
      if status_arg == true
        @status = 0
      elsif status_arg == false
        @status = 1
      else
        @status = status_arg.to_i
      end
      super(msg.to_s)
    end
  end

  def success?
    @status == 0
  end
end

module Kernel
  def exit(status = 0)
    raise SystemExit.new(status)
  end
  module_function :exit
end

module Process
  def self.exit(status = 0)
    ::Kernel.exit(status)
  end

  def self.argv0
    @argv0 ||= ($0 ? $0.dup.freeze : "".freeze)
  end
end



# MRI defines Kernel#warn as a module function (also Kernel.warn)
module Kernel
  module_function :warn
end
