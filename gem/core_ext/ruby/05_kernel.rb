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
      if defined?(Warning) && self != Warning && Warning.respond_to?(:warn)
        Warning.warn(str, category: cat_sym)
      else
        $stderr.write(str)
      end
    end
    nil
  end

  alias __builtin_raise__ raise
  def raise(*args)
    if args.size == 3
      exc = args[0].is_a?(Class) ? args[0].new(args[1]) : args[0]
      exc.set_backtrace(args[2]) if exc.respond_to?(:set_backtrace)
      __builtin_raise__(exc)
    else
      __builtin_raise__(*args)
    end
  end

  def fixnum_max
    (1 << 62) - 1
  end

  def fixnum_min
    -(1 << 62)
  end

  def bignum_value(plus = 0)
    Bignum.new("9223372036854775808") + plus
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



def bignum_value(plus = 0)
  Bignum.new("9223372036854775808") + plus
end

def fixnum_max
  (1 << 62) - 1
end

def fixnum_min
  -(1 << 62)
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

  private

  def caller_locations(start = 1, length = nil)
    lines = caller(0) || []
    # Drop caller(0) and caller_locations itself
    lines = lines[2..] || []

    if start.is_a?(Range)
      range = start
      beg = range.begin || 0
      end_idx = range.end
      beg = beg.to_i
      end_idx = end_idx.to_i unless end_idx.nil?
      slice = if end_idx.nil?
        lines[beg..]
      elsif range.exclude_end?
        lines[beg...end_idx]
      else
        lines[beg..end_idx]
      end
      return slice ? slice.map { |l| Thread::Backtrace::Location.new(l) } : nil
    end

    start = start.to_i
    raise ArgumentError, "negative level (#{start})" if start < 0

    if length
      length = length.to_i
      raise ArgumentError, "negative size (#{length})" if length < 0
      lines = lines[start, length]
    else
      lines = lines[start..]
    end

    lines ? lines.map { |l| Thread::Backtrace::Location.new(l) } : nil
  end
end

class Thread
  module Backtrace
    def self.limit
      $__backtrace_limit__ || -1
    end

    class Location
      attr_reader :path, :lineno, :label

      def initialize(str)
        @str = str.to_s
        if @str =~ /^(.*?):(\d+)(?::in [`']?(.*?)'?)?$/
          @path = $1
          @lineno = $2.to_i
          @label = $3 || ""
        else
          @path = @str
          @lineno = 1
          @label = ""
        end
      end

      def absolute_path
        File.expand_path(@path) rescue @path
      end

      def base_label
        @label.sub(/^.*#/, '').sub(/^.*::/, '')
      end

      def to_s
        @str
      end

      def inspect
        @str
      end
    end
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

class BasicObject
  def method_missing(mid, *args, &blk)
    ::Object.instance_method(:method_missing).bind(self).call(mid, *args, &blk)
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


