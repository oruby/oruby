# Thread::Queue and Thread::SizedQueue: argument checks as MRI makes them,
# over the Go queue (gem/thread/queue.go), whose waits let other threads
# run. A wait no other thread could end is MRI's deadlock.
class Thread
  Queue = ::Queue unless const_defined?(:Queue, false)
  SizedQueue = ::SizedQueue unless const_defined?(:SizedQueue, false)
end

class Queue
  def initialize(items = (no_items = true; nil))
    unless no_items
      a = if items.respond_to?(:to_a)
            items.to_a
          else
            raise TypeError, "can't convert #{Kernel.__cname(items)} into Array"
          end
      raise TypeError, "can't convert #{Kernel.__cname(items)} into Array (#{Kernel.__cname(items)}#to_a gives #{Kernel.__cname(a)})" unless Array === a
      __oruby_items.concat(a)
    end
  end
  private :initialize

  def push(obj)
    __oruby_push(obj, false, nil)
  end
  alias << push
  alias enq push

  def pop(non_block = false, timeout: nil)
    unless timeout.nil?
      raise ArgumentError, "can't set a timeout if non_block is enabled" if non_block
      timeout = Queue.__timeout(timeout)
    end
    __oruby_pop(non_block, timeout)
  end
  alias shift pop
  alias deq pop

  def self.__timeout(t)
    raise TypeError, "no implicit conversion of #{Kernel.__cname(t)} into Float" unless Numeric === t
    t.to_f
  end

  def empty? = __oruby_items.empty?
  def size = __oruby_items.size
  alias length size

  def clear
    __oruby_items.clear
    __oruby_space
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
    __oruby_max(max)
    super()
  end
  private :initialize

  def max = @__max

  def max=(n)
    n = Kernel.__to_int(n)
    raise ArgumentError, "queue size must be positive" if n <= 0
    @__max = n
    __oruby_max(n)
  end

  def push(obj, non_block = false, timeout: nil)
    unless timeout.nil?
      raise ArgumentError, "can't set a timeout if non_block is enabled" if non_block
      timeout = Queue.__timeout(timeout)
    end
    __oruby_push(obj, non_block, timeout)
  end
  alias << push
  alias enq push
end
