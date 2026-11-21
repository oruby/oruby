module Kernel
  def warn(*messages, uplevel: nil, category: nil)
    return nil if $VERBOSE.nil?
    msgs = messages.flatten
    return nil if msgs.empty?
    unless uplevel.nil?
      uplevel = Kernel.__to_int(uplevel)
      raise ArgumentError, "negative level (#{uplevel})" if uplevel < 0
      # MRI's: frames of core methods written in Ruby (<internal:...>) do
      # not count
      locs = (caller_locations(1) || []).reject { |l| (l.path || "").start_with?("<internal:") }
      l = locs[uplevel]
      msgs[0] = l ? "#{l.path}:#{l.lineno}: warning: #{msgs[0]}" : "warning: #{msgs[0]}"
    end
    unless category.nil? || Symbol === category
      unless Kernel.__responds?(category, :to_sym)
        raise TypeError, "no implicit conversion of #{Kernel.__cname(category)} into Symbol"
      end
      category = category.to_sym
    end
    # a category turned off (Warning[:deprecated] = false) prints nothing
    return nil if category && Warning::CATEGORIES.include?(category) && !Warning[category]
    str = msgs.map { |m| s = m.to_s; s.end_with?("\n") ? s : s + "\n" }.join("")
    # Kernel#warn bound to Warning itself writes, as MRI's, rather than recurse
    return $stderr.write(str) && nil if equal?(Warning)
    # MRI's rb_warn_category: a Warning.warn of arity 1 gets the message
    # alone, any other the category too
    if Warning.method(:warn).arity == 1
      Warning.warn(str)
    else
      Warning.warn(str, category: category)
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
    loc = (caller_locations(1, 1) || [])[0]
    return nil unless loc
    file = loc.path
    return nil if file == "-e" || file.empty? || file.start_with?("(eval")
    absolute = loc.absolute_path
    return File.dirname(absolute) if absolute
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
  # the current thread's stack (this method's own frame is the first, as in
  # MRI); another thread's from where it waits; nil for a dead one
  def backtrace(*args)
    if equal?(Thread.current)
      return args.empty? ? caller(0) : caller(*args)
    end
    return nil unless alive?
    fib = __oruby_fiber
    fib ? fib.__send__(:__oruby_backtrace, *args) : []
  end

  def backtrace_locations(*args)
    if equal?(Thread.current)
      return args.empty? ? caller_locations(0) : caller_locations(*args)
    end
    return nil unless alive?
    fib = __oruby_fiber
    fib ? fib.__send__(:__oruby_backtrace_locations, *args) : []
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

# MRI's rb_obj_singleton_methods: the public and protected methods of the
# singleton class and, with all, of the singleton classes and extended
# modules after it (up to the first ordinary class). The closest definition
# of a name decides: a private one hides the others.
module Kernel
  def singleton_methods(all = true)
    # the singleton class there is (asking does not make one, as in MRI)
    sc = __oruby_singleton_class_or_nil
    return [] if sc.nil?
    chain = [sc]
    if all
      anc = sc.ancestors
      i = anc.index { |c| !c.equal?(sc) && c.instance_of?(Class) && !c.singleton_class? } || anc.size
      chain = anc[0...i]
      # modules prepended to that class come before it in ancestors, but
      # the superclass chain reaches the class first
      if (stop = anc[i])
        chain -= stop.ancestors.take_while { |c| !c.equal?(stop) }
      end
    end
    seen = {}
    out = []
    chain.each do |c|
      c.private_instance_methods(false).each { |m| seen[m] = true }
      (c.public_instance_methods(false) + c.protected_instance_methods(false)).each do |m|
        next if seen[m]
        seen[m] = true
        out << m
      end
    end
    out
  end
end

module Kernel
  # MRI's rb_Array: #to_ary (private too), then #to_a, each answer an Array
  # or nil; anything else is [arg]
  def Array(arg)
    return arg if Array === arg
    return [] if arg.nil?
    # the common kinds, none with #to_ary
    case arg
    when String, Symbol, Integer, Float, true, false then return [arg]
    when Hash then return arg.to_a
    end
    if Kernel.__responds?(arg, :to_ary)
      r = arg.__send__(:to_ary)
      return Kernel.__oruby_ary_conv(arg, r, :to_ary) unless r.nil?
    end
    if Kernel.__responds?(arg, :to_a)
      r = arg.__send__(:to_a)
      return Kernel.__oruby_ary_conv(arg, r, :to_a) unless r.nil?
    end
    [arg]
  end

  def self.__oruby_ary_conv(arg, r, meth)
    return r if Array === r
    c = Kernel.__cname(arg)
    raise TypeError, "can't convert #{c} into Array (#{c}##{meth} gives #{Kernel.__cname(r)})"
  end

  # MRI's Kernel#open: an object with #to_open opens itself (the block gets
  # what it answers); anything else must be a path
  alias_method :__oruby_kernel_open, :open
  def open(name, *rest, **kw, &blk)
    unless String === name
      if Kernel.__responds?(name, :to_open)
        io = kw.empty? ? name.to_open(*rest) : name.to_open(*rest, **kw)
        return blk ? yield(io) : io
      end
      unless Kernel.__responds?(name, :to_str) || Kernel.__responds?(name, :to_path)
        raise TypeError, "no implicit conversion of #{Kernel.__cname(name)} into String"
      end
    end
    kw.empty? ? __oruby_kernel_open(name, *rest, &blk) : __oruby_kernel_open(name, *rest, **kw, &blk)
  end
  module_function :Array, :open
  private :__oruby_kernel_open
end

module Kernel
  # rb_warn for C: "file:line: warning: msg" at the nearest user frame
  def self.__oruby_warn_here(msg, category = nil) = warn(msg, uplevel: 0, category: category)
end

module Kernel
  # the parse warnings of a source (prism.go's ORB_PARSE_WARN): [line,
  # verbose only, text], printed as MRI does while parsing
  def self.__oruby_parse_warn(file, warns)
    return if $VERBOSE.nil?
    warns.each do |line, verbose, msg|
      next if verbose && !$VERBOSE
      warn "#{file}:#{line}: warning: #{msg}"
    end
    nil
  end
end
