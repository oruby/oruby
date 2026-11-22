module Open3
  VERSION = "0.2.1"

  def self.__split_args(cmd)
    env = cmd.first.is_a?(Hash) ? cmd.shift : nil
    opts = cmd.last.is_a?(Hash) ? cmd.pop.dup : {}
    [env, cmd, opts]
  end

  def self.__start(cmd, merge_err)
    env, argv, opts = __split_args(cmd)
    chdir = opts.delete(:chdir)
    stdin_data = opts.delete(:stdin_data)
    binmode = opts.delete(:binmode)
    pid, in_fd, out_fd, err_fd = __spawn(env, argv, chdir, merge_err, nil)
    i = IO.for_fd(in_fd, "w")
    i.sync = true
    o = IO.for_fd(out_fd, "r")
    e = err_fd ? IO.for_fd(err_fd, "r") : nil
    waiter = Process.detach(pid)
    [i, o, e, waiter, stdin_data, binmode]
  end

  def self.__run(ios, waiter, &block)
    return ios + [waiter] unless block
    begin
      block.call(*ios, waiter)
    ensure
      ios.each { |io| io.close unless io.closed? }
      waiter.join
    end
  end

  def popen3(*cmd, &block)
    i, o, e, w = Open3.__start(cmd, false)
    Open3.__run([i, o, e], w, &block)
  end
  module_function :popen3

  def popen2(*cmd, &block)
    i, o, e, w = Open3.__start(cmd, false)
    e.close
    Open3.__run([i, o], w, &block)
  end
  module_function :popen2

  def popen2e(*cmd, &block)
    i, o, _e, w = Open3.__start(cmd, true)
    Open3.__run([i, o], w, &block)
  end
  module_function :popen2e

  def self.__write_stdin(i, data, binmode)
    i.binmode if binmode
    i.write(data.to_s) if data
    i.close
  end

  def capture3(*cmd)
    i, o, e, w, stdin_data, binmode = Open3.__start(cmd, false)
    __write_stdin(i, stdin_data, binmode)
    out = o.read
    err = e.read
    o.close
    e.close
    [out, err, w.value]
  end
  module_function :capture3

  def capture2(*cmd)
    i, o, e, w, stdin_data, binmode = Open3.__start(cmd, false)
    __write_stdin(i, stdin_data, binmode)
    out = o.read
    e.read
    o.close
    e.close
    [out, w.value]
  end
  module_function :capture2

  def capture2e(*cmd)
    i, o, _e, w, stdin_data, binmode = Open3.__start(cmd, true)
    __write_stdin(i, stdin_data, binmode)
    out = o.read
    o.close
    [out, w.value]
  end
  module_function :capture2e

  def self.__shell_cmd(c)
    case c
    when String then c
    when Array
      c = c.dup
      c.shift if c.first.is_a?(Hash)
      c.pop if c.last.is_a?(Hash)
      c.map { |a| a.is_a?(Array) ? a.first : a }.map { |a| "'" + a.to_s.gsub("'", "'\\\\''") + "'" }.join(" ")
    end
  end

  def self.__pipeline(cmds, opts, merge_err: false)
    line = cmds.map { |c| __shell_cmd(c) }
    [line.join(" | ")]
  end

  def pipeline_rw(*cmds, &block)
    opts = cmds.last.is_a?(Hash) ? cmds.pop : {}
    i, o, e, w = Open3.__start(Open3.__pipeline(cmds, opts), false)
    e.close
    if block
      begin
        block.call(i, o, [w])
      ensure
        [i, o].each { |io| io.close unless io.closed? }
        w.join
      end
    else
      [i, o, [w]]
    end
  end
  module_function :pipeline_rw

  def pipeline_r(*cmds, &block)
    opts = cmds.last.is_a?(Hash) ? cmds.pop : {}
    i, o, e, w = Open3.__start(Open3.__pipeline(cmds, opts), false)
    i.close
    if block
      begin
        block.call(o, [w])
      ensure
        o.close unless o.closed?
        w.join
      end
    else
      [o, [w]]
    end
  end
  module_function :pipeline_r

  def pipeline_w(*cmds, &block)
    opts = cmds.last.is_a?(Hash) ? cmds.pop : {}
    i, o, e, w = Open3.__start(Open3.__pipeline(cmds, opts), false)
    o.close
    e.close
    if block
      begin
        block.call(i, [w])
      ensure
        i.close unless i.closed?
        w.join
      end
    else
      [i, [w]]
    end
  end
  module_function :pipeline_w

  def pipeline_start(*cmds, &block)
    opts = cmds.last.is_a?(Hash) ? cmds.pop : {}
    i, o, e, w = Open3.__start(Open3.__pipeline(cmds, opts), false)
    i.close
    o.close
    e.close
    if block
      block.call([w])
    else
      [w]
    end
  end
  module_function :pipeline_start

  def pipeline(*cmds)
    opts = cmds.last.is_a?(Hash) ? cmds.pop : {}
    statuses = cmds.map { |c| Process.wait2(Process.spawn(*Array(c)))[1] rescue nil }
    statuses
  end
  module_function :pipeline
end
