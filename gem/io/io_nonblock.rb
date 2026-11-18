class IO
  module WaitReadable; end
  module WaitWritable; end

  class EAGAINWaitReadable < Errno::EAGAIN
    include IO::WaitReadable
  end
  class EAGAINWaitWritable < Errno::EAGAIN
    include IO::WaitWritable
  end
  EWOULDBLOCKWaitReadable = EAGAINWaitReadable
  EWOULDBLOCKWaitWritable = EAGAINWaitWritable
  if Errno.const_defined?(:EINPROGRESS)
    class EINPROGRESSWaitWritable < Errno::EINPROGRESS
      include IO::WaitWritable
    end
    class EINPROGRESSWaitReadable < Errno::EINPROGRESS
      include IO::WaitReadable
    end
  end

  READABLE = 1 unless const_defined?(:READABLE)
  PRIORITY = 2 unless const_defined?(:PRIORITY)
  WRITABLE = 4 unless const_defined?(:WRITABLE)

  def self.__select_fds(list)
    return [nil, nil] if list.nil?
    raise TypeError, "no implicit conversion of #{list.class} into Array" unless list.is_a?(Array)
    ios = list.map do |o|
      io = o.respond_to?(:to_io) ? o.to_io : o
      raise TypeError, "no implicit conversion of #{o.class} into IO" unless io.is_a?(IO)
      raise IOError, "closed stream" if io.closed?
      io
    end
    [ios, ios.map(&:fileno)]
  end

  def self.select(reads, writes = nil, errs = nil, timeout = nil)
    if timeout
      raise TypeError, "can't convert #{timeout.class} into time interval" unless timeout.is_a?(Numeric)
      raise RangeError, "NaN out of Time range" if timeout.is_a?(Float) && timeout.nan?
      raise ArgumentError, "time interval must not be negative" if timeout < 0
      timeout = nil if timeout == Float::INFINITY
      ms = timeout.nil? ? -1 : (timeout * 1000).ceil
    else
      ms = -1
    end
    r, rfd = __select_fds(reads)
    w, wfd = __select_fds(writes)
    e, efd = __select_fds(errs)
    if rfd.nil? && wfd.nil? && efd.nil?
      sleep(timeout) if timeout
      sleep if timeout.nil?
      return nil
    end
    # bytes already read ahead (or pushed back) make an IO readable now
    if r && (ready = r.each_index.select { |i| r[i].__oruby_buffered? }).any?
      return [ready.map { |i| reads[i] }, [], []]
    end
    res = __poll(rfd, wfd, efd, ms)
    return nil if res.nil?
    [res[0].map { |i| reads[i] }, res[1].map { |i| writes[i] }, res[2].map { |i| errs[i] }]
  end

  def self.__oruby_exception_kw(v)
    return v if true.equal?(v) || false.equal?(v)
    raise ArgumentError, "expected true or false as exception: #{v.inspect}"
  end

  def read_nonblock(maxlen, buffer = nil, exception: true)
    IO.__oruby_exception_kw(exception)
    maxlen = ::Kernel.__implicit(maxlen, Integer)
    raise ArgumentError, "negative length #{maxlen} given" if maxlen < 0
    buffer = ::Kernel.__implicit(buffer, String) unless buffer.nil?
    raise IOError, "closed stream" if closed?
    if maxlen == 0
      buffer&.replace("")
      return buffer || "".b
    end
    res = __read_nonblock(maxlen)
    case res
    when :wait_readable
      return :wait_readable unless exception
      raise IO::EAGAINWaitReadable, "read would block"
    when nil
      buffer&.replace("")
      return nil unless exception
      raise EOFError, "end of file reached"
    end
    if buffer
      # the bytes, in the buffer's own encoding
      enc = buffer.encoding
      buffer.replace(res)
      buffer.force_encoding(enc)
    else
      res
    end
  end

  def write_nonblock(obj, exception: true)
    IO.__oruby_exception_kw(exception)
    raise IOError, "closed stream" if closed?
    raise IOError, "not opened for writing" unless __oruby_writable?
    str = obj.to_s
    return 0 if str.empty?
    res = __write_nonblock(str)
    if res == :wait_writable
      return :wait_writable unless exception
      raise IO::EAGAINWaitWritable, "write would block"
    end
    res
  end

  def wait_readable(timeout = nil)
    IO.select([self], nil, nil, timeout) ? self : nil
  end

  def wait_writable(timeout = nil)
    IO.select(nil, [self], nil, timeout) ? self : nil
  end

  def wait_priority(timeout = nil)
    IO.select(nil, nil, [self], timeout) ? self : nil
  end

  def wait(*args)
    events = READABLE
    timeout = nil
    if args.first.is_a?(Integer) && args.size == 2
      events, timeout = args
    else
      args.each do |a|
        case a
        when :r, :read, :readable then events = READABLE
        when :w, :write, :writable then events = WRITABLE
        when :rw, :read_write, :readable_writable then events = READABLE | WRITABLE
        when Numeric, nil then timeout = a
        end
      end
    end
    r = events & READABLE != 0 ? [self] : nil
    w = events & WRITABLE != 0 ? [self] : nil
    res = IO.select(r, w, nil, timeout)
    return nil unless res
    args.first.is_a?(Integer) ? ((res[0].empty? ? 0 : READABLE) | (res[1].empty? ? 0 : WRITABLE)) : self
  end

  def ready?
    !!IO.select([self], nil, nil, 0)
  end

  def nonblock=(v)
    __nonblock(v)
  end

  def nonblock?
    __nonblock?
  end

  def nonblock(v = true)
    old = nonblock?
    self.nonblock = v
    begin
      yield self
    ensure
      self.nonblock = old
    end
  end
end

class IO
  unless method_defined?(:set_encoding)
    def set_encoding(ext = nil, int = nil, **opts)
      @__set_encoding = [ext, int]
      self
    end
  end
end
