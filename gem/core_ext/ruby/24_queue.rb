# Thread::Queue and Thread::SizedQueue for ORuby's single thread: no other
# thread can push or pop, so where MRI would block, a timeout runs out (nil)
# and an unbounded wait is MRI's deadlock error instead of a hang.
class Thread
  Queue = ::Queue unless const_defined?(:Queue, false)
  SizedQueue = ::SizedQueue unless const_defined?(:SizedQueue, false)
end

class Queue
  def initialize(items = (no_items = true; nil))
    @__q = []
    @__closed = false
    unless no_items
      a = if items.respond_to?(:to_a)
            items.to_a
          else
            raise TypeError, "can't convert #{Kernel.__cname(items)} into Array"
          end
      raise TypeError, "can't convert #{Kernel.__cname(items)} into Array (#{Kernel.__cname(items)}#to_a gives #{Kernel.__cname(a)})" unless Array === a
      @__q.concat(a)
    end
  end
  private :initialize

  def push(obj)
    raise ClosedQueueError, "queue closed" if @__closed
    @__q.push(obj)
    self
  end
  alias << push
  alias enq push

  def pop(non_block = false, timeout: nil)
    if timeout
      raise ArgumentError, "can't set a timeout if non_block is enabled" if non_block
      timeout = Queue.__timeout(timeout)
    end
    return @__q.shift unless @__q.empty?
    return nil if @__closed
    raise ThreadError, "queue empty" if non_block
    if timeout
      sleep(timeout) if timeout > 0
      return nil
    end
    raise ThreadError, "No live threads left. Deadlock?"
  end
  alias shift pop
  alias deq pop

  def self.__timeout(t)
    raise TypeError, "no implicit conversion of #{Kernel.__cname(t)} into Float" unless Numeric === t
    t.to_f
  end

  def close
    @__closed = true
    self
  end

  def closed? = @__closed
  def empty? = @__q.empty?
  def size = @__q.size
  alias length size
  def num_waiting = 0

  def clear
    @__q.clear
    self
  end

  def freeze
    raise TypeError, "cannot freeze #{self}"
  end

  def marshal_dump
    raise TypeError, "can't dump #{self.class}"
  end
end

class SizedQueue

  def initialize(max)
    max = Kernel.__to_int(max)
    raise ArgumentError, "queue size must be positive" if max <= 0
    @__max = max
    super()
  end
  private :initialize

  def max = @__max

  def max=(n)
    n = Kernel.__to_int(n)
    raise ArgumentError, "queue size must be positive" if n <= 0
    @__max = n
  end

  def push(obj, non_block = false, timeout: nil)
    if timeout
      raise ArgumentError, "can't set a timeout if non_block is enabled" if non_block
      timeout = Queue.__timeout(timeout)
    end
    raise ClosedQueueError, "queue closed" if closed?
    if size >= @__max
      raise ThreadError, "queue full" if non_block
      if timeout
        sleep(timeout) if timeout > 0
        return nil
      end
      raise ThreadError, "No live threads left. Deadlock?"
    end
    super(obj)
  end
  alias << push
  alias enq push
end
