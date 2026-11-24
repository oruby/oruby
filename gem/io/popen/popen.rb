# IO.popen as MRI's io.c takes its arguments: [env,] cmd [, mode] [, opt],
# cmd a String (run by the shell) or an Array (argv, with an optional
# leading env Hash and trailing exec options). Keywords are IO options
# (mode, encodings, ...) or exec options (everything else). The stream is
# an instance of the receiver, closed after the block.
class IO
  POPEN_IO_OPTIONS = %i[mode flags external_encoding internal_encoding encoding textmode binmode
                        autoclose newline universal_newline crlf_newline cr_newline lf_newline
                        invalid undef replace xml fallback].freeze
  private_constant :POPEN_IO_OPTIONS if respond_to?(:private_constant, true)

  def self.popen(*args, **opts, &block)
    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if args.empty?
    env = nil
    if args.size > 1 && Hash === args[0]
      env = args.shift
    end
    raise ArgumentError, "wrong number of arguments (given #{args.size + (env ? 1 : 0)}, expected 1..3)" if args.size > 2
    cmd, mode = args
    if Hash === mode
      opts = mode.merge(opts)
      mode = nil
    end
    exec_opts = {}
    if Array === cmd
      cmd = cmd.dup
      if Hash === cmd.first
        h = cmd.shift
        env = env ? env.merge(h) : h
      end
      exec_opts.update(cmd.pop) if Hash === cmd.last
      raise ArgumentError, "wrong number of arguments" if cmd.empty?
    else
      cmd = ::Kernel.__implicit(cmd, String) unless String === cmd
      raise NotImplementedError, "fork() function is unimplemented on this machine" if cmd == "-"
    end
    io_opts = {}
    opts.each { |k, v| POPEN_IO_OPTIONS.include?(k) ? io_opts[k] = v : exec_opts[k] = v }
    %i[unsetenv_others close_others].each do |k|
      next unless exec_opts.key?(k)
      v = exec_opts[k]
      raise ArgumentError, "expected true or false as #{k}: #{v.inspect}" unless v.nil? || true.equal?(v) || false.equal?(v)
    end
    if exec_opts.key?(:chdir)
      d = exec_opts[:chdir]
      exec_opts[:chdir] = String === d ? d : (::Kernel.__responds?(d, :to_path) ? d.to_path : ::Kernel.__implicit(d, String))
    end
    if respond_to?(:__oruby_check_mode, true)
      mode, io_opts = __oruby_check_mode(mode, io_opts)
    elsif !(mode.nil? || Integer === mode || String === mode)
      mode = ::Kernel.__implicit(mode, String)
    end
    readable, writable = __oruby_popen_access(mode, io_opts[:flags])
    io = allocate
    io.__oruby_popen_start(env, cmd, readable, writable, exec_opts)
    io.sync = true if writable # MRI's popen stream is synchronized
    if io.respond_to?(:__oruby_init_encoding, true)
      io.__oruby_init_encoding(mode || (writable ? (readable ? "r+" : "w") : "r"), io_opts)
    end
    return io unless block
    begin
      yield io
    ensure
      io.close unless io.closed?
    end
  end

  # [readable, writable] of a popen mode ("r" when none is given); the
  # access bits are O_RDONLY 0, O_WRONLY 1, O_RDWR 2 (File may not be loaded)
  def self.__oruby_popen_access(mode, flags)
    case mode
    when nil
      acc = Integer === flags ? flags & 3 : 0
      [acc != 1, acc != 0]
    when Integer
      acc = mode & 3
      [acc != 1, acc != 0]
    else
      m = mode.split(":", 2)[0]
      raise ArgumentError, "invalid access mode #{m}" unless m =~ /\A[rwa][bt]*\+?[bt]*\z/
      m.include?("+") ? [true, true] : [m[0] == "r", m[0] != "r"]
    end
  end

  def pid
    raise IOError, "closed stream" if closed?
    __oruby_popen_pid
  end
end
