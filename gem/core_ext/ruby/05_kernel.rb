module Kernel
  def warn(*messages, uplevel: nil, category: nil)
    return nil if $VERBOSE.nil?
    return nil if messages.empty?
    if uplevel
      loc = (caller(uplevel + 1, 1) || [])[0]
      loc = loc.sub(/:in .*$/, "") if loc
      if loc && !messages.empty?
        messages = ["#{loc}: warning: #{messages[0].to_s.sub(/^warning:\s*/, "")}"] + messages[1..]
      end
    end
    cat_sym = category ? category.to_sym : nil
    messages.each do |msg|
      str = msg.to_s
      str += "\n" unless str.end_with?("\n")
      if self != Warning
        Warning.warn(str, category: cat_sym)
      else
        $stderr.write(str)
      end
    end
    nil
  end

  alias __builtin_raise__ raise
  # adds raise(exc, message, backtrace); keywords (cause:) pass through
  def raise(*args, **kw)
    if args.size == 3
      exc = args[0].is_a?(Class) ? args[0].new(args[1]) : args[0]
      exc.set_backtrace(args[2]) if exc.respond_to?(:set_backtrace)
      __builtin_raise__(exc, **kw)
    else
      __builtin_raise__(*args, **kw)
    end
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
