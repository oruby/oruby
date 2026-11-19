# TracePoint, as MRI 4.1 has it. go-trace.c reports the events of the VM
# (TracePoint.__oruby_fire) while one is enabled; :thread_begin and
# :thread_end come from the thread's Ruby side (03_thread.rb).
class TracePoint
  EVENTS = {
    line: 1, call: 2, return: 4, c_call: 8, c_return: 16, raise: 32, b_call: 64, b_return: 128,
    class: 256, end: 512, rescue: 1024, script_compiled: 2048, thread_begin: 4096,
    thread_end: 8192, fiber_switch: 16384,
  }.freeze
  # events go-trace.c reports; the others are fired from Ruby
  VM_EVENTS = 4095
  THREAD_EVENTS = 4096 | 8192
  ALL_EVENTS = EVENTS.values.sum
  # what a Proc's code, and a Method's, can have
  PROC_EVENTS = 1 | 8 | 16 | 32 | 64 | 128 | 256 | 512 | 1024
  METHOD_EVENTS = PROC_EVENTS | 2 | 4
  private_constant :EVENTS, :VM_EVENTS, :THREAD_EVENTS, :ALL_EVENTS, :PROC_EVENTS, :METHOD_EVENTS

  @__oruby_enabled = []

  class << self
    def new(*events, &blk)
      mask = events.empty? ? ALL_EVENTS : events.inject(0) { |m, e| m | __oruby_event_bit(e) }
      raise ArgumentError, "must be called with a block" unless blk
      allocate.__send__(:__oruby_setup, mask, blk)
    end

    def trace(*events, &blk) = new(*events, &blk).tap(&:enable)

    def stat = {}

    # a handler's code is traced in the block
    def allow_reentry(&blk)
      raise ArgumentError, "no block given" unless blk
      __oruby_reentry(&blk)
    end

    def __oruby_event_bit(e)
      sym = if Symbol === e then e
            elsif String === e then e.to_sym
            elsif e.respond_to?(:to_sym) then e.to_sym
            end
      raise TypeError, "wrong argument type #{Kernel.__cname(e)} (expected Symbol)" unless Symbol === sym
      case sym
      when :a_call then 2 | 64 | 8
      when :a_return then 4 | 128 | 16
      else EVENTS[sym] || raise(ArgumentError, "unknown event: #{sym}")
      end
    end

    def __oruby_enabled = @__oruby_enabled

    # the events the enabled TracePoints want, to go-trace.c
    def __oruby_update
      mask = @__oruby_enabled.inject(0) { |m, tp| m | tp.__send__(:__oruby_mask) }
      $__oruby_tp_threads = (mask & THREAD_EVENTS) != 0
      self.__oruby_events = mask & VM_EVENTS
    end

    # an event (go-trace.c): the handlers of the TracePoints that want it,
    # those without a target first, the last enabled first
    def __oruby_fire(event, slf, mid, klass, value, path, line, proc)
      list = @__oruby_enabled
      return nil if list.empty?
      bit = EVENTS[event]
      th = Thread.current
      [false, true].each do |targeted|
        list.dup.each do |tp|
          next unless tp.__send__(:__oruby_targeted?) == targeted
          next unless tp.__send__(:__oruby_wants?, bit, th, path, line)
          tp.__send__(:__oruby_run, event, slf, mid, klass, value, path, line, proc)
        end
      end
      nil
    end

    # :thread_begin, :thread_end (03_thread.rb)
    def __oruby_thread_event(event)
      __oruby_signal(event, Thread.current) if $__oruby_tp_threads
    end
  end

  def enable(target: nil, target_line: nil, target_thread: :default)
    raise ArgumentError, "only target_line is specified" if target_line && target.nil?
    if @__oruby_on && (target || @__oruby_range)
      raise ArgumentError, "can't nest-enable a targeting TracePoint"
    end
    range = nil
    if target
      unless Method === target || UnboundMethod === target || Proc === target
        raise ArgumentError, "specified target is not supported"
      end
      if target_line
        target_line = Kernel.__to_int(target_line)
        if (@__oruby_mask & EVENTS[:line]) == 0
          raise ArgumentError, "target_line is specified, but line event is not specified"
        end
      end
      can = (Proc === target ? PROC_EVENTS : METHOD_EVENTS) & @__oruby_mask
      sr = target.source_range
      raise ArgumentError, "can not enable any hooks" if can == 0 || sr.nil?
      range = [sr.path, sr.start_line, sr.end_line]
      if target_line && !target_line.between?(sr.start_line, sr.end_line)
        raise ArgumentError, "can not enable any hooks"
      end
    end
    if target_thread == :default
      target_thread = block_given? && target.nil? ? Thread.current : nil
    end
    was = @__oruby_on
    @__oruby_range = range
    @__oruby_line = target_line
    @__oruby_thread = target_thread
    __oruby_on
    return was unless block_given?
    begin
      yield
    ensure
      __oruby_off
    end
  end

  def disable
    was = @__oruby_on
    if block_given?
      raise ArgumentError, "can't disable a targeting TracePoint in a block" if @__oruby_range
      __oruby_off
      begin
        return yield
      ensure
        __oruby_on if was
      end
    end
    __oruby_off
    was
  end

  def enabled? = @__oruby_on

  def event = __oruby_ev[0]
  def path = __oruby_ev[5]
  def lineno = __oruby_ev[6]
  def callee_id = __oruby_ev[2]
  def defined_class = __oruby_ev[3]
  define_method(:self) { __oruby_ev[1] }

  def method_id
    ev = __oruby_ev
    mid, klass = ev[2], ev[3]
    return mid if mid.nil? || klass.nil?
    begin
      klass.instance_method(mid).original_name
    rescue NameError
      mid
    end
  end

  def return_value
    ev = __oruby_ev
    raise RuntimeError, "not supported by this event" unless %i[return c_return b_return].include?(ev[0])
    ev[4]
  end

  def raised_exception
    ev = __oruby_ev
    raise RuntimeError, "not supported by this event" unless ev[0] == :raise || ev[0] == :rescue
    ev[4]
  end

  def eval_script
    ev = __oruby_ev
    raise RuntimeError, "not supported by this event" unless ev[0] == :script_compiled
    ev[4]
  end

  def instruction_sequence
    __oruby_ev
    raise RuntimeError, "not supported by this event"
  end

  def parameters
    ev = __oruby_ev
    case ev[0]
    when :call, :return, :b_call, :b_return
      ev[7] ? ev[7].parameters : []
    when :c_call, :c_return
      begin
        ev[3].instance_method(ev[2]).parameters
      rescue NameError
        [[:rest]]
      end
    else
      raise RuntimeError, "not supported by this event"
    end
  end

  def binding
    ev = __oruby_ev
    return nil if ev[0] == :c_call || ev[0] == :c_return
    TracePoint.__oruby_binding
  end

  def inspect
    ev = @__oruby_ev
    return @__oruby_on ? "#<TracePoint:enabled>" : "#<TracePoint:disabled>" unless ev
    case ev[0]
    when :call, :c_call, :return, :c_return
      "#<TracePoint:#{ev[0]} '#{ev[2]}' #{ev[5]}:#{ev[6]}>"
    when :thread_begin, :thread_end
      "#<TracePoint:#{ev[0]} #{ev[1].inspect}>"
    else
      "#<TracePoint:#{ev[0]} #{ev[5]}:#{ev[6]}>"
    end
  end

  private

  def __oruby_setup(mask, blk)
    @__oruby_mask = mask
    @__oruby_blk = blk
    @__oruby_on = false
    @__oruby_ev = nil
    self
  end

  def __oruby_mask = @__oruby_mask
  def __oruby_targeted? = !@__oruby_range.nil?

  def __oruby_on
    unless @__oruby_on
      @__oruby_on = true
      TracePoint.__oruby_enabled.unshift(self)
      TracePoint.__oruby_update
    end
  end

  def __oruby_off
    if @__oruby_on
      @__oruby_on = false
      TracePoint.__oruby_enabled.delete(self)
      TracePoint.__oruby_update
    end
    @__oruby_range = nil
    @__oruby_line = nil
  end

  def __oruby_wants?(bit, th, path, line)
    return false if (@__oruby_mask & bit) == 0
    return false if @__oruby_thread && !@__oruby_thread.equal?(th)
    if (r = @__oruby_range)
      return false unless path == r[0] && line >= r[1] && line <= r[2]
      return false if @__oruby_line && (bit != 1 || line != @__oruby_line)
    end
    true
  end

  def __oruby_run(event, slf, mid, klass, value, path, line, proc)
    prev = @__oruby_ev
    @__oruby_ev = [event, slf, mid, klass, value, path, line, proc]
    begin
      @__oruby_blk.call(self)
    ensure
      @__oruby_ev = prev
    end
  end

  def __oruby_ev = @__oruby_ev || raise(RuntimeError, "access from outside")
end
