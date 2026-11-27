# What the IO functions of the C API (capi_io.inc) ask Ruby for.
class IO
  class TimeoutError < IOError; end unless const_defined?(:TimeoutError, false)
end

module ORubyCAPI
  # a place where Thread#kill, #raise and signal handlers are taken
  def self.__check_ints; nil; end

  # [fd, FMODE_* flags] of an open IO; IOError for a closed or an
  # uninitialized one
  def self.io_info(io)
    fd = io.fileno
    fl = begin
      io.fcntl(3) # F_GETFL
    rescue SystemCallError, NotImplementedError
      0
    end
    acc = fl & 3
    mode = acc == 0 ? 1 : (acc == 1 ? 2 : 3)
    mode |= 0x04 if io.binmode?
    mode |= 0x08 if io.sync
    mode |= 0x10 if io.tty?
    mode |= 0x40 if fl & File::APPEND != 0
    if (of = io.instance_variable_get(:@__oruby_oflags))
      mode |= 0x80 if of & File::CREAT != 0
      mode |= 0x400 if of & File::EXCL != 0
      mode |= 0x800 if of & File::TRUNC != 0
    end
    [fd, mode]
  end

  # IO#wait: the events that happened, false when the time passed
  def self.io_wait(io, events, timeout)
    events = Integer(events)
    r = events & 3 != 0 ? [io] : nil
    w = events & 4 != 0 ? [io] : nil
    res = IO.select(r, w, nil, timeout)
    return false unless res
    out = 0
    out |= 1 if res[0].include?(io)
    out |= 4 if res[1].include?(io)
    out
  end

  # rb_io_open_descriptor: the IO of klass for fd; fmode is FMODE_*, the
  # encodings the ones the caller gave (or nil)
  def self.io_open_descriptor(klass, fd, fmode, path, timeout, ext, int)
    mode = (fmode & 3) == 3 ? "r+" : ((fmode & 2) != 0 ? "w" : "r")
    mode += "b" if (fmode & 4) != 0
    io = klass.new(fd, mode)
    io.sync = true if (fmode & 8) != 0
    io.instance_variable_set(:@__oruby_path, String(path).dup) unless path.nil?
    io.timeout = timeout unless timeout.nil?
    io.set_encoding(ext, int) if ext || int
    io
  end
end
