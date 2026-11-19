# The Fiber scheduler interface, as MRI 4.1 has it. A scheduler is set per
# thread (Fiber.set_scheduler); a blocking operation of a non-blocking
# fiber then goes through it: Kernel#sleep calls #kernel_sleep, a wait on a
# Mutex, Queue or Thread#join calls #block and its waker #unblock, a child
# wait #process_wait, name resolution #address_resolve, an IO read #io_read
# or #io_wait. The Go side (gem/thread, gem/process, gem/io, gem/socket)
# asks Fiber.__oruby_sched, cheaply skipped while the thread has none
# (Thread.__oruby_scheduler_flag).
class Fiber
  SCHEDULER_HOOKS = %i[block unblock kernel_sleep io_wait fiber_interrupt].freeze
  private_constant :SCHEDULER_HOOKS

  class << self
    def set_scheduler(scheduler)
      unless scheduler.nil?
        SCHEDULER_HOOKS.each do |m|
          raise ArgumentError, "Scheduler must implement ##{m}" unless scheduler.respond_to?(m)
        end
      end
      t = Thread.current
      old = t.instance_variable_get(:@__oruby_scheduler)
      # the one replaced is closed, which runs what it still has to run
      __oruby_scheduler_close(old) if old
      t.instance_variable_set(:@__oruby_scheduler, scheduler)
      Thread.__oruby_scheduler_flag(!scheduler.nil?)
      __oruby_install_wrappers unless scheduler.nil?
      scheduler
    end

    # A fiber cannot switch across a Go method's frame, so the scheduler's
    # block, kernel_sleep and unblock are called from these Ruby wrappers;
    # they are installed with the first scheduler, and a program without one
    # keeps the Go methods
    def __oruby_install_wrappers
      return if $__oruby_fiber_sched
      $__oruby_fiber_sched = true
      ::Kernel.module_eval do
        alias_method :__oruby_go_sleep, :sleep
        def sleep(*args)
          s = Fiber.__oruby_sched
          return __oruby_go_sleep(*args) unless s
          t = Time.now
          s.kernel_sleep(*args)
          (Time.now - t).round
        end
        private :sleep
      end
      # closing an IO a fiber waits on through its scheduler interrupts it
      # (fiber_interrupt(fiber, IOError)), then waits for it to leave
      ::IO.class_eval do
        alias_method :__oruby_go_close, :close
        def close
          ws = @__oruby_io_waiters
          if ws && !ws.empty? && !closed? && !@__oruby_closing
            @__oruby_closing = true
            begin
              err = IOError.new("stream closed in another thread")
              ws.dup.each { |f, s| s.fiber_interrupt(f, err) }
              if (cs = Fiber.__oruby_sched)
                until ws.empty?
                  @__oruby_closer = [Fiber.current, cs]
                  cs.block(self, nil)
                end
              end
            ensure
              @__oruby_closer = nil
              @__oruby_closing = nil
            end
          end
          __oruby_go_close
        end

        # a write to a non-blocking IO that would block waits through the
        # scheduler (io_wait(self, IO::WRITABLE, nil)); writers take turns on
        # the IO's write lock, as MRI's
        alias_method :__oruby_nosched_write, :write
        def write(*objs)
          s = Fiber.__oruby_sched
          unless s && !@__enc && !@__enc2 && !@__ecopts && !closed? && (nonblock? rescue false)
            return __oruby_nosched_write(*objs)
          end
          data = objs.map { |o| (String === o ? o : o.to_s).b }.join
          (@__oruby_wlock ||= Thread::Mutex.new).synchronize do
            total = 0
            until data.empty?
              n = write_nonblock(data, exception: false)
              if :wait_writable.equal?(n)
                __oruby_sched_io_wait(s, IO::WRITABLE, nil, "write(2)")
                next
              end
              total += n
              data = data.byteslice(n, data.bytesize - n)
            end
            total
          end
        end
      end
      ::Thread::Mutex.class_eval do
        alias_method :__oruby_go_lock, :lock
        alias_method :__oruby_go_unlock, :unlock
        alias_method :__oruby_go_sleep, :sleep
        def lock
          r = __oruby_go_lock
          while :__oruby_block.equal?(r)
            Fiber.__oruby_sched.block(self, nil)
            r = __oruby_go_lock
          end
          self
        end
        def unlock
          __oruby_go_unlock
          Thread.__oruby_drain
          self
        end
        def synchronize
          raise ThreadError, "must be called with a block" unless block_given?
          lock
          begin
            yield
          ensure
            unlock
          end
        end
        def sleep(timeout = nil)
          s = Fiber.__oruby_sched
          return __oruby_go_sleep(timeout) unless s
          unless timeout.nil?
            raise TypeError, "can't convert #{Kernel.__cname(timeout)} into time interval" unless Numeric === timeout
            raise ArgumentError, "time interval must not be negative" if timeout < 0
          end
          unlock
          t = Time.now
          begin
            s.kernel_sleep(timeout)
          ensure
            lock
          end
          (Time.now - t).round
        end
      end
    end

    def scheduler = Thread.current.instance_variable_get(:@__oruby_scheduler)

    # the scheduler the current fiber uses: none for a blocking fiber
    def current_scheduler
      s = Thread.current.instance_variable_get(:@__oruby_scheduler)
      s && !Fiber.current.blocking? ? s : nil
    end
    alias __oruby_sched current_scheduler

    def schedule(*args, &block)
      s = scheduler
      raise RuntimeError, "No scheduler is available!" unless s
      s.fiber(*args, &block)
    end

    def blocking? = Fiber.current.blocking? ? 1 : false

    # Fiber.blocking { |fiber| }: the current fiber is blocking meanwhile
    def blocking
      f = Fiber.current
      return yield(f) if f.blocking?
      was = f.instance_variable_get(:@__oruby_blocking)
      f.instance_variable_set(:@__oruby_blocking, true)
      begin
        yield(f)
      ensure
        f.instance_variable_set(:@__oruby_blocking, was)
      end
    end

    def __oruby_scheduler_close(s)
      Thread.__oruby_drain
      if s.respond_to?(:scheduler_close)
        s.scheduler_close
      elsif s.respond_to?(:close)
        s.close
      end
    end
  end
end

class Thread
  # the scheduler unblocks the Go side queued (gem/thread's waiters)
  def self.__oruby_drain
    while (list = __oruby_unblocks)
      list.each { |s, blocker, fiber| s.unblock(blocker, fiber) }
    end
    nil
  end
end

# Child waits through the scheduler's #process_wait (when it has one)
module Process
  class Status
    # MRI 3.0's Process::Status.wait(pid = -1, flags = 0): the status, one
    # with pid -1 when there is no child; never the scheduler
    def self.wait(pid = -1, flags = 0)
      pid = Kernel.__to_int(pid) unless Integer === pid
      flags = Kernel.__to_int(flags) unless Integer === flags || flags.nil?
      saved = $?
      begin
        r = Process.__oruby_wait2(pid, flags || 0)
        r ? r[1] : nil
      rescue Errno::ECHILD
        __oruby_nochild
      ensure
        Process.__oruby_set_status(saved)
      end
    end
  end

  class << self
    alias __oruby_wait2 wait2
    alias __oruby_waitall waitall
    alias __oruby_wait wait

    # the status the scheduler's process_wait gives; :none without one
    def __oruby_sched_wait(pid, flags)
      s = Fiber.__oruby_sched
      return :none unless s && s.respond_to?(:process_wait)
      st = s.process_wait(pid, flags)
      unless Process::Status === st
        raise TypeError, "wrong argument type #{st.nil? ? 'nil' : st.class} (expected Process::Status)"
      end
      __oruby_set_status(st)
      raise Errno::ECHILD if st.pid == -1
      st
    end

    def wait2(pid = -1, flags = 0)
      pid = Kernel.__to_int(pid) unless Integer === pid
      flags = Kernel.__to_int(flags) unless Integer === flags || flags.nil?
      flags ||= 0
      st = __oruby_sched_wait(pid, flags)
      return __oruby_wait2(pid, flags) if :none.equal?(st)
      st.pid == 0 ? nil : [st.pid, st]
    end
    alias waitpid2 wait2

    def wait(pid = -1, flags = 0)
      pid = Kernel.__to_int(pid) unless Integer === pid
      flags = Kernel.__to_int(flags) unless Integer === flags || flags.nil?
      flags ||= 0
      st = __oruby_sched_wait(pid, flags)
      return __oruby_wait(pid, flags) if :none.equal?(st)
      st.pid == 0 ? nil : st.pid
    end

    alias waitpid wait

    def waitall
      s = Fiber.__oruby_sched
      return __oruby_waitall unless s && s.respond_to?(:process_wait)
      out = []
      loop do
        st = __oruby_sched_wait(-1, 0)
        out << [st.pid, st]
      rescue Errno::ECHILD
        break
      end
      out
    end
  end
end

module Kernel
  alias __oruby_system system
  # with a scheduler's process_wait: the command is spawned and waited for
  # through it
  def system(*args, **opts)
    s = Fiber.__oruby_sched
    return __oruby_system(*args, **opts) unless s && s.respond_to?(:process_wait)
    exc = opts.delete(:exception)
    begin
      pid = Process.spawn(*args, **opts)
    rescue SystemCallError
      raise if exc
      return nil
    end
    st = s.process_wait(pid, 0)
    raise TypeError, "wrong argument type #{st.nil? ? 'nil' : st.class} (expected Process::Status)" unless Process::Status === st
    Process.__oruby_set_status(st)
    raise RuntimeError, "Command failed with exit #{st.exitstatus}: #{args.join(' ')}" if exc && !st.success?
    st.success?
  end
  private :system
end
