class Monitor
  def initialize
    @mon_mutex = ::Mutex.new
    @mon_owner = nil
    @mon_count = 0
  end

  def try_enter
    if @mon_owner != Thread.current
      return false unless @mon_mutex.try_lock
      @mon_owner = Thread.current
      @mon_count = 0
    end
    @mon_count += 1
    true
  end
  alias try_mon_enter try_enter
  alias mon_try_enter try_enter

  def enter
    if @mon_owner != Thread.current
      @mon_mutex.lock
      @mon_owner = Thread.current
      @mon_count = 0
    end
    @mon_count += 1
    nil
  end
  alias mon_enter enter

  def exit
    mon_check_owner
    @mon_count -= 1
    if @mon_count == 0
      @mon_owner = nil
      @mon_mutex.unlock
    end
    nil
  end
  alias mon_exit exit

  def mon_locked?
    @mon_mutex.locked?
  end

  def mon_owned?
    @mon_mutex.locked? && @mon_owner == Thread.current
  end

  def mon_check_owner
    raise ThreadError, "current thread not owner" unless @mon_owner == Thread.current
    true
  end

  def synchronize
    enter
    begin
      yield
    ensure
      self.exit
    end
  end
  alias mon_synchronize synchronize

  def new_cond
    MonitorMixin::ConditionVariable.new(self)
  end

  def wait_for_cond(cond, timeout)
    count = @mon_count
    @mon_owner = nil
    @mon_count = 0
    begin
      cond.__wait(@mon_mutex, timeout)
    ensure
      @mon_owner = Thread.current
      @mon_count = count
    end
    true
  end
end

module MonitorMixin
  class ConditionVariable
    def initialize(monitor)
      @monitor = monitor
      @waiters = []
    end

    def wait(timeout = nil)
      @monitor.mon_check_owner
      @monitor.wait_for_cond(self, timeout)
    end

    def wait_while
      wait while yield
    end

    def wait_until
      wait until yield
    end

    def signal
      @monitor.mon_check_owner
      q = @waiters.shift
      q << true if q
      nil
    end

    def broadcast
      @monitor.mon_check_owner
      @waiters.each { |q| q << true }
      @waiters.clear
      nil
    end

    def __wait(mutex, timeout)
      q = ::Queue.new
      @waiters << q
      mutex.unlock
      begin
        if timeout
          deadline = Time.now + timeout
          while q.empty? && Time.now < deadline
            sleep 0.001
          end
        else
          q.pop
        end
      ensure
        mutex.lock
        @waiters.delete(q)
      end
    end
  end

  def self.extend_object(obj)
    super
    obj.__send__(:mon_initialize)
  end

  def mon_initialize
    @mon_data = ::Monitor.new
    @mon_data_owner_object_id = object_id
  end

  def mon_try_enter = @mon_data.try_enter
  alias try_mon_enter mon_try_enter
  def mon_enter = @mon_data.enter
  def mon_exit
    mon_check_owner
    @mon_data.exit
  end
  def mon_locked? = @mon_data.mon_locked?
  def mon_owned? = @mon_data.mon_owned?
  def mon_synchronize(&b) = @mon_data.synchronize(&b)
  alias synchronize mon_synchronize
  def new_cond = ::MonitorMixin::ConditionVariable.new(@mon_data)

  private

  def mon_check_owner
    @mon_data.mon_check_owner
  end

  def initialize(...)
    super
    mon_initialize
  end
end
