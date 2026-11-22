module PTY
  class ChildExited < RuntimeError
    attr_reader :status

    def initialize(status, msg = nil)
      @status = status
      super(msg || "pty - exited: #{status}")
    end
  end

  module_function

  def open
    master, slave = __open
    return [master, slave] unless block_given?
    begin
      yield [master, slave]
    ensure
      master.close unless master.closed?
      slave.close unless slave.closed?
    end
  end

  def spawn(*args, **kw)
    env = nil
    if args.first.is_a?(Hash)
      env = args.shift
    end
    opts = {}
    if args.length > 1 && args.last.is_a?(Hash)
      opts = args.pop
    end
    opts = opts.merge(kw) unless kw.empty?

    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if args.empty?

    cmd = args.shift
    argv = nil
    if cmd.is_a?(Array)
      cmd = cmd[0]
      argv = args.map(&:to_s)
    elsif !args.empty?
      argv = args.map(&:to_s)
    end
    chdir = opts[:chdir]

    r, w, pid = __spawn(env, cmd.to_s, argv, chdir ? chdir.to_s : nil)
    return [r, w, pid] unless block_given?

    begin
      yield r, w, pid
    ensure
      r.close unless r.closed?
      w.close unless w.closed?
      begin
        Process.waitpid(pid)
      rescue Errno::ECHILD
      end
    end
  end
  alias getpty spawn
  module_function :getpty

  def check(pid, raise_err = false)
    res = Process.waitpid2(pid, Process::WNOHANG)
    return nil unless res
    _, status = res
    if raise_err
      raise ChildExited.new(status, "pty - exited: #{pid}")
    end
    status
  rescue Errno::ECHILD
    nil
  end
end
