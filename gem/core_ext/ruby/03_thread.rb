# Thread as MRI 4.1 has it, over oruby's scheduler (thread.go, gem/thread):
# each thread runs on its own goroutine, one at a time under the GVL.
#
# Interrupts: Thread#raise and #kill queue what they bring on the target
# (@__oruby_pending) and wake it; the target takes them at its next
# safepoint or blocking wait (Thread.__oruby_interrupt), where
# Thread.handle_interrupt masks apply. A kill unwinds the thread with throw
# to a catch around its body, so ensure clauses run and `rescue Exception`
# does not stop it, as in MRI.
class Thread
  kill_tag = Object.new.freeze
  define_singleton_method(:__oruby_kill_tag) { kill_tag }

  @__oruby_report = true
  @__oruby_abort = false

  class << self
    alias __oruby_allocate allocate
    private :__oruby_allocate

    def allocate
      ::Kernel.raise TypeError, "allocator undefined for #{self}"
    end

    def new(*args, &block)
      t = __oruby_allocate
      t.__send__(:initialize, *args, &block)
      unless t.instance_variable_get(:@__oruby_started)
        ::Kernel.raise ThreadError, "uninitialized thread - check '#{self}#initialize'"
      end
      t
    end

    # start and fork do not call a subclass's initialize
    def start(*args, &block)
      ::Kernel.raise ArgumentError, "tried to create Proc object without a block" unless block
      t = __oruby_allocate
      t.__send__(:__oruby_init, args, block)
      t
    end
    alias fork start

    def report_on_exception = @__oruby_report
    def report_on_exception=(v)
      @__oruby_report = v ? true : false
    end

    def abort_on_exception = @__oruby_abort
    def abort_on_exception=(v)
      @__oruby_abort = v ? true : false
    end

    def ignore_deadlock = @__oruby_ignore_deadlock ? true : false
    def ignore_deadlock=(v)
      @__oruby_ignore_deadlock = v ? true : false
      __oruby_ignore_deadlock(@__oruby_ignore_deadlock)
      v
    end

    def stop
      if Thread.list.size == 1
        ::Kernel.raise ThreadError, "stopping only thread\n\tnote: use sleep to stop forever"
      end
      __oruby_stop
      nil
    end

    def exit = Thread.current.kill
    alias terminate exit

    def kill(thread)
      ::Kernel.raise TypeError, "wrong argument type #{Kernel.__cname(thread)} (expected VM/thread)" unless Thread === thread
      thread.kill
    end

    def pending_interrupt?(err = nil) = Thread.current.pending_interrupt?(err)

    def each_caller_location(*args, **kw)
      ::Kernel.raise ArgumentError, "unknown keyword: #{kw.keys.first.inspect}" unless kw.empty?
      ::Kernel.raise LocalJumpError, "no block given" unless block_given?
      # the frames caller_locations gives where this is called
      (caller_locations(2, *args) || []).each { |loc| yield loc }
      nil
    end

    # Thread.handle_interrupt(ExceptionClass => :immediate | :on_blocking |
    # :never) { }: when interrupts of those classes are taken
    def handle_interrupt(hash)
      ::Kernel.raise ArgumentError, "block is needed." unless block_given?
      ::Kernel.raise TypeError, "no implicit conversion of #{Kernel.__cname(hash)} into Hash" unless Hash === hash
      hash.each do |k, v|
        unless v == :immediate || v == :on_blocking || v == :never
          ::Kernel.raise ArgumentError, "unknown mask signature"
        end
        ::Kernel.raise TypeError, "class or module required for rescue clause" unless Module === k
      end
      t = Thread.current
      masks = t.instance_variable_get(:@__oruby_masks) || t.instance_variable_set(:@__oruby_masks, [])
      masks.push(hash)
      begin
        Thread.__oruby_interrupt
        yield
      ensure
        masks.pop
        Thread.__oruby_interrupt
      end
    end

    # what the mask stack of t says for an interrupt of class k
    def __oruby_timing(t, k)
      masks = t.instance_variable_get(:@__oruby_masks)
      return :immediate if masks.nil? || masks.empty?
      masks.reverse_each do |h|
        h.each { |key, timing| return timing if k <= key }
      end
      :immediate
    end

    # an exception for Thread#raise, made as Kernel#raise makes it
    def __oruby_exception(args, cause)
      exc = case args.size
            when 0 then RuntimeError.new("")
            when 1
              a = args[0]
              String === a ? RuntimeError.new(a) : __oruby_exception_of(a, [])
            else
              __oruby_exception_of(args[0], [args[1]])
            end
      exc.set_backtrace(args[2]) if args.size > 2
      exc.instance_variable_set(:@__oruby_cause, cause) unless cause.equal?(Thread.__oruby_kill_tag)
      exc.instance_variable_set(:@__oruby_from_raise, true)
      exc
    end

    def __oruby_exception_of(a, rest)
      ::Kernel.raise TypeError, "exception class/object expected" unless a.respond_to?(:exception)
      e = a.exception(*rest)
      ::Kernel.raise TypeError, "exception object expected" unless Exception === e
      e
    end
  end

  # the interrupts queued for the current thread that may be taken now:
  # the first is raised (a kill throws). blocking: at a blocking wait,
  # where :on_blocking ones are taken too.
  define_singleton_method(:__oruby_interrupt) do |blocking = false|
    t = Thread.current
    # a fiber of the thread is being killed: the kill goes on once it ended
    killing = t.instance_variable_get(:@__oruby_killing)
    return nil if killing && killing.equal?(Fiber.current)
    q = t.instance_variable_get(:@__oruby_pending)
    if q.nil? || q.empty?
      __oruby_interrupt_taken(false)
      return nil
    end
    i = q.index do |x|
      timing = Thread.__oruby_timing(t, kill_tag.equal?(x) ? Integer : x.class)
      timing == :immediate || (blocking && timing == :on_blocking)
    end
    if i.nil?
      __oruby_interrupt_taken(true)
      return nil
    end
    x = q.delete_at(i)
    __oruby_interrupt_taken(!q.empty?)
    if kill_tag.equal?(x)
      t.instance_variable_set(:@__oruby_aborting, true)
      root = t.__oruby_root_fiber
      if root && !root.equal?(Fiber.current)
        # in a fiber of the thread: that fiber ends first (its ensure
        # clauses run), then the kill goes on in the fiber that resumed it
        # (Fiber#resume)
        q.unshift(kill_tag)
        __oruby_interrupt_taken(true)
        t.instance_variable_set(:@__oruby_killing, Fiber.current)
        Fiber.current.kill
      end
      throw kill_tag
    end
    has_cause = x.instance_variable_defined?(:@__oruby_cause)
    cause = x.remove_instance_variable(:@__oruby_cause) if has_cause
    if x.instance_variable_defined?(:@__oruby_from_raise)
      x.remove_instance_variable(:@__oruby_from_raise)
      x = x.exception
    end
    # the backtrace is where the thread was interrupted
    if x.backtrace.nil?
      bt = caller(1)
      bt.shift while bt.first&.include?("__oruby_interrupt")
      Exception.instance_method(:set_backtrace).bind_call(x, bt)
    end
    if has_cause
      cause.equal?(x) ? ::Kernel.raise(x) : ::Kernel.raise(x, cause: cause)
    else
      ::Kernel.raise x
    end
  end

  def initialize(*args, &block)
    ::Kernel.raise ThreadError, "must be called with a block" unless block
    ::Kernel.raise ThreadError, "already initialized thread" if @__oruby_started
    __oruby_init(args, block)
  end
  private :initialize

  define_method(:__oruby_init) do |args, block|
    @__oruby_started = true
    @__oruby_pending ||= []
    loc = block.source_location
    @__oruby_location = loc ? "#{loc[0]}:#{loc[1]}" : nil
    @__oruby_report = Thread.report_on_exception
    @__oruby_group = Thread.current.group
    storage = Fiber.current.__send__(:__oruby_storage_copy)
    body = proc do
      catch(kill_tag) do
        begin
          Thread.__oruby_started
          Fiber.current.__send__(:__oruby_storage_set, storage) if storage
          Thread.__oruby_interrupt
          TracePoint.__oruby_thread_event(:thread_begin) if $__oruby_tp_threads
          @__oruby_value = block.call(*args)
        rescue Exception => e
          __oruby_died(e)
        ensure
          # the Mutexes it holds are released as it ends (their fibers
          # unblocked from here), and its scheduler is closed, running what
          # it still has, as MRI's
          TracePoint.__oruby_thread_event(:thread_end) if $__oruby_tp_threads
          Thread.__oruby_release_held
          Thread.__oruby_drain if $__oruby_fiber_sched
          if instance_variable_get(:@__oruby_scheduler)
            begin
              Fiber.set_scheduler(nil)
            rescue Exception => e
              __oruby_died(e)
            end
          end
        end
      end
    end
    __oruby_start(body)
    self
  end
  private :__oruby_init

  # an exception ended the thread: reported (report_on_exception), raised in
  # the main thread for abort_on_exception, and SystemExit ends the program
  def __oruby_died(e)
    @__oruby_exception = e
    if SystemExit === e
      Thread.main.__send__(:__oruby_post, e)
      return
    end
    if @__oruby_report
      begin
        $stderr.write("#{inspect} terminated with exception (report_on_exception is true):\n#{e.full_message(highlight: false)}")
      rescue Exception
        nil
      end
    end
    Thread.main.__send__(:__oruby_post, e) if @__oruby_abort || Thread.abort_on_exception
  end
  private :__oruby_died

  def __oruby_post(exc)
    (@__oruby_pending ||= []) << exc
    __oruby_notify
    nil
  end
  private :__oruby_post

  def status
    case __oruby_state
    when 0 then @__oruby_aborting ? "aborting" : "run"
    when 1 then "sleep"
    when 2 then @__oruby_exception ? nil : false
    else "run"
    end
  end

  def alive? = __oruby_state != 2

  def stop?
    s = __oruby_state
    s == 1 || s == 2
  end

  def join(limit = nil)
    unless limit.nil?
      ::Kernel.raise TypeError, "no implicit conversion of #{Kernel.__cname(limit)} into Float" unless Numeric === limit
    end
    ::Kernel.raise ThreadError, "Target thread must not be current thread" if equal?(Thread.current)
    ::Kernel.raise ThreadError, "Target thread must not be main thread" if equal?(Thread.main)
    r = __oruby_join(limit)
    if :__oruby_block.equal?(r)
      # a non-blocking fiber joins through its scheduler: block(thread,
      # limit), unblocked when the thread is dead
      Fiber.__oruby_sched.block(self, limit && limit.to_f)
      __oruby_unjoin
      return nil if limit && alive?
      r = Fiber.blocking { __oruby_join(limit) }
    end
    return nil unless r
    e = @__oruby_exception
    # re-raised as it is: the joiner's $! is not its cause
    ::Kernel.raise e, cause: e.cause if e && !(SystemExit === e)
    self
  end

  def value
    join
    @__oruby_value
  end

  # Thread#raise(exception = RuntimeError, message, backtrace, cause: $!):
  # keywords other than cause go to the exception, as Kernel#raise has them;
  # the cause is the caller's $!
  define_method(:raise) do |*args, **kw|
    has_cause = kw.key?(:cause)
    cause = kw.delete(:cause)
    args << kw unless kw.empty?
    if has_cause
      ::Kernel.raise ArgumentError, "only cause is given with no arguments" if args.empty?
      ::Kernel.raise TypeError, "exception object expected" unless cause.nil? || Exception === cause
    end
    return nil unless alive?
    if equal?(Thread.current)
      # no arguments: a RuntimeError, not $! again (MRI's rb_threadptr_raise)
      args = [RuntimeError, ""] if args.empty?
      return has_cause ? ::Kernel.raise(*args, cause: cause) : ::Kernel.raise(*args)
    end
    cause = $! unless has_cause
    __oruby_post(Thread.__oruby_exception(args, cause))
    nil
  end

  define_method(:kill) do
    return self unless alive?
    if equal?(Thread.current)
      ::Kernel.exit(0) if equal?(Thread.main)
      @__oruby_aborting = true
      throw kill_tag
    end
    if equal?(Thread.main)
      __oruby_post(SystemExit.new(0))
    else
      __oruby_post(kill_tag)
    end
    self
  end
  alias exit kill
  alias terminate kill

  def wakeup
    ::Kernel.raise ThreadError, "killed thread" unless alive?
    __oruby_wakeup
    self
  end

  def run
    wakeup
    Thread.pass
    self
  end

  def pending_interrupt?(err = nil)
    q = @__oruby_pending || []
    return !q.empty? if err.nil?
    ::Kernel.raise TypeError, "class or module required for rescue clause" unless Module === err
    tag = Thread.__oruby_kill_tag
    q.any? { |x| (tag.equal?(x) ? Integer : x.class) <= err }
  end

  # fiber-local variables (Thread#[]): the variables of the fiber the thread
  # runs, kept on that Fiber
  def __oruby_locals
    fib = equal?(Thread.current) ? Fiber.current : __oruby_fiber
    return (@__oruby_locals ||= {}) if fib.nil?
    fib.instance_variable_get(:@__oruby_tlocals) || fib.instance_variable_set(:@__oruby_tlocals, {})
  end
  private :__oruby_locals

  def self.__oruby_key(key)
    return key if Symbol === key
    return key.to_sym if String === key
    s = key.respond_to?(:to_str) ? key.to_str : nil
    return s.to_sym if String === s
    ::Kernel.raise TypeError, "#{key.inspect} is not a symbol nor a string"
  end

  def [](key)
    __oruby_locals[Thread.__oruby_key(key)]
  end

  def []=(key, val)
    k = Thread.__oruby_key(key)
    ::Kernel.raise FrozenError, "can't modify frozen thread locals" if frozen?
    l = __oruby_locals
    val.nil? ? l.delete(k) : l[k] = val
    val
  end

  def key?(key)
    __oruby_locals.key?(Thread.__oruby_key(key))
  end

  def keys = __oruby_locals.keys

  def fetch(key, *default)
    ::Kernel.raise ArgumentError, "wrong number of arguments (given #{default.size + 1}, expected 1..2)" if default.size > 1
    k = Thread.__oruby_key(key)
    l = __oruby_locals
    return l[k] if l.key?(k)
    if block_given?
      warn("warning: block supersedes default value argument") unless default.empty?
      return yield(key)
    end
    return default[0] unless default.empty?
    ::Kernel.raise KeyError.new("key not found: #{k.inspect}", receiver: self, key: k)
  end

  # thread variables (not fiber-local); keys are symbols as in MRI
  def thread_variable_get(key)
    (@__thread_vars || {})[Thread.__oruby_key(key)]
  end

  def thread_variable_set(key, value)
    k = Thread.__oruby_key(key)
    ::Kernel.raise FrozenError, "can't modify frozen thread locals" if frozen?
    vars = (@__thread_vars ||= {})
    value.nil? ? vars.delete(k) : vars[k] = value
    value
  end

  def thread_variable?(key)
    (@__thread_vars || {}).key?(Thread.__oruby_key(key))
  end

  def thread_variables
    (@__thread_vars || {}).keys
  end

  def name = @__oruby_name

  def name=(n)
    unless n.nil?
      n = n.to_str if !(String === n) && n.respond_to?(:to_str)
      ::Kernel.raise TypeError, "no implicit conversion of #{Kernel.__cname(n)} into String" unless String === n
      ::Kernel.raise ArgumentError, "string contains null byte" if n.include?("\0")
      n = n.dup.freeze
    end
    @__oruby_name = n
  end

  def inspect
    s = __oruby_state
    word = case s
           when 1 then "sleep"
           when 2 then "dead"
           else @__oruby_aborting ? "aborting" : "run"
           end
    head = ::Kernel.instance_method(:to_s).bind_call(self).chomp(">")
    head += "@#{@__oruby_name}" if @__oruby_name
    head += " #{@__oruby_location}" if @__oruby_location
    s = "#{head} #{word}>"
    @__oruby_name ? s : s.force_encoding(Encoding::BINARY)
  end
  alias to_s inspect

  def report_on_exception
    @__oruby_report.nil? ? Thread.report_on_exception : @__oruby_report
  end

  def report_on_exception=(v)
    @__oruby_report = v ? true : false
  end

  def abort_on_exception = @__oruby_abort ? true : false

  def abort_on_exception=(v)
    @__oruby_abort = v ? true : false
  end

  def priority = @__oruby_priority || 0

  def priority=(n)
    n = Kernel.__to_int(n)
    @__oruby_priority = n.clamp(-3, 3)
  end

  def group
    return nil unless alive?
    @__oruby_group || ThreadGroup::Default
  end

  def __oruby_group=(g)
    @__oruby_group = g
  end
end

class ThreadGroup
  Default = new

  def add(thread)
    ::Kernel.raise TypeError, "wrong argument type #{Kernel.__cname(thread)} (expected VM/thread)" unless Thread === thread
    ::Kernel.raise ThreadError, "can't move to the enclosed thread group" if enclosed?
    old = thread.group
    ::Kernel.raise ThreadError, "can't move from the enclosed thread group" if old&.enclosed?
    thread.__oruby_group = self
    self
  end

  def list = Thread.list.select { |t| t.group.equal?(self) }

  def enclose
    @__oruby_enclosed = true
    self
  end

  def enclosed? = @__oruby_enclosed ? true : false
end
