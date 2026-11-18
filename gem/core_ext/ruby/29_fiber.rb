# Fiber as MRI 4.1 has it, over mruby-fiber.
#
# Requests: Fiber#raise and Fiber#kill resume (or transfer to) the fiber with
# a request that the Fiber.yield / Fiber#transfer it is suspended in acts on.
# A kill unwinds the fiber with throw to a catch around its block, so ensure
# clauses run and `rescue Exception` does not stop it, as in MRI.
class Fiber
  raise_request = Object.new.freeze
  kill_request = Object.new.freeze
  kill_tag = Object.new.freeze

  # mruby's status word, from its inspect: created, running, suspended,
  # "suspended by resuming" (resumed another fiber), transferred, terminated
  alias_method :__oruby_inspect, :inspect
  define_method(:__oruby_status) { __oruby_inspect[/\(([^()]*)\)>\z/, 1] }
  private :__oruby_status

  # a kill that came while the fiber was resuming another: it dies once it
  # runs again
  define_method(:__oruby_kill_pending) do
    if instance_variable_get(:@__oruby_kill)
      instance_variable_set(:@__oruby_kill, false)
      throw kill_tag
    end
  end
  private :__oruby_kill_pending

  check = lambda do |r|
    throw kill_tag if kill_request.equal?(r)
    if r.is_a?(Array) && r.size == 3 && raise_request.equal?(r[0])
      ::Kernel.raise(*r[1], **r[2])
    end
    r
  end

  alias_method :__oruby_initialize, :initialize
  define_method(:initialize) do |blocking: false, **opts, &block|
    ::Kernel.raise ArgumentError, "tried to create Proc object without a block" unless block
    storage = opts[:storage]
    ::Kernel.raise FrozenError, "storage must not be frozen" if storage.is_a?(Hash) && storage.frozen?
    instance_variable_set(:@__oruby_location, block.source_location&.join(":"))
    instance_variable_set(:@__oruby_blocking, blocking ? true : false) # also marks a Fiber.new fiber
    body = proc do |*args|
      catch(kill_tag) do
        throw kill_tag if kill_request.equal?(args[0]) # killed before it started
        block.call(*args)
      end
    end
    __oruby_initialize(**opts, &body)
  end
  private :initialize

  class << self
    alias_method :__oruby_yield, :yield
    alias_method :__oruby_aref, :[]
    alias_method :__oruby_aset, :[]=
  end

  define_singleton_method(:yield) do |*args|
    check.call(__oruby_yield(*args))
  end

  # storage keys: a Symbol, or a String (or #to_str) taken as its Symbol
  key = lambda do |k|
    return k if Symbol === k
    return k.to_sym if String === k
    s = k.respond_to?(:to_str) ? k.to_str : nil
    String === s ? s.to_sym : k
  end
  define_singleton_method(:[]) { |k| __oruby_aref(key.call(k)) }
  define_singleton_method(:[]=) { |k, v| __oruby_aset(key.call(k), v) }

  alias_method :__oruby_storage_set, :storage=
  define_method(:storage=) do |h|
    if Warning[:experimental]
      warn "Fiber#storage= is experimental and may be removed in the future!", uplevel: 1, category: :experimental
    end
    if equal?(Fiber.current) && h.is_a?(Hash) && h.frozen?
      ::Kernel.raise FrozenError, "storage must not be frozen"
    end
    __oruby_storage_set(h)
  end

  alias_method :__oruby_resume, :resume
  define_method(:resume) do |*args|
    case __oruby_status
    when "terminated"
      ::Kernel.raise FiberError, "attempt to resume a terminated fiber"
    when "suspended by resuming"
      # MRI: the root fiber is resuming; any other was resumed itself
      if instance_variable_get(:@__oruby_blocking).nil?
        ::Kernel.raise FiberError, "attempt to resume a resuming fiber"
      end
      ::Kernel.raise FiberError, "attempt to resume a resumed fiber (double resume)"
    end
    r = __oruby_resume(*args)
    Fiber.current.__send__(:__oruby_kill_pending)
    r
  end

  alias_method :__oruby_transfer, :transfer
  define_method(:transfer) do |*args|
    ::Kernel.raise FiberError, "attempt to transfer to a terminated fiber" if __oruby_status == "terminated"
    r = check.call(__oruby_transfer(*args))
    Fiber.current.__send__(:__oruby_kill_pending)
    r
  end

  # a fiber transferred to is resumed by transfer only
  alias_method :__oruby_transfer_raw, :__oruby_transfer
  define_method(:__oruby_transfer) do |*args|
    instance_variable_set(:@__oruby_transferred, true)
    __oruby_transfer_raw(*args)
  end
  private :__oruby_transfer, :__oruby_transfer_raw

  define_method(:raise) do |*args, **kw|
    # raising on the current fiber or on one it was resumed from (MRI: the
    # same as Kernel#raise here) raises here
    status = __oruby_status
    ::Kernel.raise(*args, **kw) if equal?(Fiber.current) || status == "suspended by resuming"
    ::Kernel.raise FiberError, "cannot raise exception on unborn fiber" if status == "created"
    ::Kernel.raise FiberError, "attempt to resume a terminated fiber" if status == "terminated"
    # checked here, in the caller, as MRI makes the exception here
    if kw.key?(:cause)
      ::Kernel.raise ArgumentError, "only cause is given with no arguments" if args.empty?
      c = kw[:cause]
      ::Kernel.raise TypeError, "exception object expected" unless c.nil? || c.is_a?(Exception)
    end
    # the cause is the caller's $!, not what the fiber was handling (MRI 4.0)
    unless args.empty? || kw.key?(:cause)
      kw = kw.merge(cause: args[0].equal?($!) ? nil : $!)
    end
    if instance_variable_get(:@__oruby_transferred)
      __oruby_transfer([raise_request, args, kw])
    else
      resume([raise_request, args, kw])
    end
  end

  # Fiber#kill: ends the fiber, running its ensure clauses; returns the fiber
  define_method(:kill) do
    status = __oruby_status
    return self if status == "terminated"
    if equal?(Fiber.current)
      # the root fiber is the thread: killing it ends the program
      ::Kernel.exit if instance_variable_get(:@__oruby_blocking).nil?
      throw kill_tag
    end
    if status == "suspended by resuming"
      instance_variable_set(:@__oruby_kill, true)
      return self
    end
    # one kill: an ensure clause that yields leaves the fiber suspended
    if instance_variable_get(:@__oruby_transferred)
      __oruby_transfer(kill_request)
    else
      __oruby_resume(kill_request)
    end
    self
  end

  # the root fiber is blocking; Fiber.new's as asked
  define_method(:blocking?) do
    b = instance_variable_get(:@__oruby_blocking)
    b.nil? ? true : b
  end

  # the bookkeeping above is not the fiber's own
  define_method(:instance_variables) do
    super().reject { |n| n.start_with?("@__oruby_") }
  end

  define_method(:backtrace) do |*args|
    return (args.empty? ? caller(0) : caller(*args)) if equal?(Fiber.current)
    __oruby_backtrace(*args)
  end

  define_method(:backtrace_locations) do |*args|
    return (args.empty? ? caller_locations(0) : caller_locations(*args)) if equal?(Fiber.current)
    __oruby_backtrace_locations(*args)
  end

  # MRI's form: #<Fiber:0x... file:line (status)>; created, resumed,
  # suspended or terminated
  define_method(:inspect) do
    addr = __oruby_inspect[/\A#<Fiber:(0x\h+)/, 1]
    status = case __oruby_status
             when "running", "suspended by resuming" then "resumed"
             when "transferred" then "suspended"
             else __oruby_status
             end
    loc = instance_variable_get(:@__oruby_location)
    loc ? "#<Fiber:#{addr} #{loc} (#{status})>" : "#<Fiber:#{addr} (#{status})>"
  end
  alias_method :to_s, :inspect
end
