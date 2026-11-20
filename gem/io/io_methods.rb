# IO methods MRI's io.c builds on IO#write (so a subclass, or a stream that
# only defines write, gets them too) and the small ones whose rules are
# easier in Ruby. The Go side (stream.go) does the reading and writing.
class IO
  def <<(obj)
    write(obj)
    self
  end

  # MRI's rb_io_print: $, between the objects, $\ after them, $_ alone
  def print(*args)
    args = [$_] if args.empty?
    ofs = $,
    args.each_with_index do |a, i|
      write(ofs) if ofs && i > 0
      write(a)
    end
    write($\) if $\
    nil
  end

  def printf(*args)
    write(format(*args)) unless args.empty?
    nil
  end

  # MRI's rb_io_putc: the first character of a String, the low byte of an
  # Integer
  def putc(ch)
    s = String === ch ? ch[0].to_s : (::Kernel.__to_int(ch) & 0xff).chr
    write(s)
    ch
  end

  # MRI's rb_io_puts: an Array (or #to_ary) element by element, a recursive
  # one as [...]; anything else as to_s, a newline after it unless it ends
  # with one; $/ plays no part
  def puts(*args)
    if args.empty?
      write("\n")
      return nil
    end
    args.each { |a| __oruby_puts(a, {}) }
    nil
  end

  def __oruby_puts(obj, seen)
    unless String === obj
      ary = __oruby_check_ary(obj)
      if ary
        if seen.key?(ary.__id__)
          write("[...]\n")
        else
          seen[ary.__id__] = true
          ary.each { |e| __oruby_puts(e, seen) }
          seen.delete(ary.__id__)
        end
        return
      end
      s = obj.to_s
      obj = String === s ? s : ::Kernel.instance_method(:to_s).bind_call(obj)
    end
    write(obj.end_with?("\n") ? obj : obj + "\n")
  end
  private :__oruby_puts

  # rb_check_array_type: to_ary when it answers, method_missing included
  def __oruby_check_ary(obj)
    return obj if Array === obj
    r = begin
      obj.to_ary
    rescue NoMethodError => e
      raise unless e.name == :to_ary
      return nil
    end
    return nil if r.nil?
    return r if Array === r
    raise TypeError, "can't convert #{obj.class} into Array (#{obj.class}#to_ary gives #{r.class})"
  end
  private :__oruby_check_ary

  # sync is the program's choice: oruby writes straight through
  def sync
    raise IOError, "closed stream" if closed?
    @__oruby_sync.nil? ? equal?(STDERR) : @__oruby_sync
  end

  def sync=(v)
    raise IOError, "closed stream" if closed?
    @__oruby_sync = v ? true : false
    v
  end

  # MRI's rb_io_close_read / close_write for a stream that is not duplex:
  # it closes the stream, unless the stream is open the other way too
  # A duplex stream (IO.popen "r+") closes one side; the IO is closed once
  # both are.
  def close_read
    return nil if closed?
    if respond_to?(:__oruby_popen_duplex?, true) && __oruby_popen_duplex?
      __oruby_popen_close_read
      @__fmode = [false, *(@__fmode || [true, true])[1..]]
      close unless __oruby_writable?
      return nil
    end
    raise IOError, "closing non-duplex IO for reading" if __oruby_writable?
    close
  end

  def close_write
    return nil if closed?
    if respond_to?(:__oruby_popen_duplex?, true) && __oruby_popen_duplex?
      __oruby_popen_close_write
      f = @__fmode || [true, true]
      @__fmode = [f[0], false, *f[2..]]
      close unless __oruby_readable?
      return nil
    end
    raise IOError, "closing non-duplex IO for writing" if __oruby_readable?
    close
  end

  # an IO not made by IO.popen has no process
  def pid
    raise IOError, "closed stream" if closed?
    nil
  end

  # MRI's rb_io_advise; the advice is a hint, which this platform takes as
  # given (posix_fadvise is not everywhere)
  def advise(advice, offset = 0, len = 0)
    raise TypeError, "advice must be a Symbol" unless Symbol === advice
    unless %i[normal sequential random willneed dontneed noreuse].include?(advice)
      raise NotImplementedError, "Unsupported advice: #{advice.inspect}"
    end
    raise IOError, "closed stream" if closed?
    [offset, len].each do |n|
      n = ::Kernel.__to_int(n)
      raise RangeError, "bignum too big to convert into 'long long'" unless n.between?(-2**63, 2**63 - 1)
    end
    nil
  end

  def fcntl(cmd, arg = 0)
    raise IOError, "closed stream" if closed?
    cmd = ::Kernel.__to_int(cmd)
    arg = case arg
          when nil, false then 0
          when true then 1
          when String then raise NotImplementedError, "fcntl with a String argument is unimplemented on this machine"
          else ::Kernel.__to_int(arg)
          end
    __oruby_fcntl(cmd, arg)
  end

  def ioctl(*)
    raise IOError, "closed stream" if closed?
    raise NotImplementedError, "ioctl() function is unimplemented on this machine"
  end

  # the path given when the IO was made (IO.new(fd, path:)), the name of a
  # standard stream, or nil
  def path
    return @__oruby_path if @__oruby_path
    return "<STDIN>" if equal?(STDIN)
    return "<STDOUT>" if equal?(STDOUT)
    return "<STDERR>" if equal?(STDERR)
    nil
  end
  alias to_path path

  # MRI's IO.copy_stream: each end is an IO (or #to_io), a path (a String
  # or #to_path), or any object with #readpartial / #read and #write. IO to
  # IO is the Go copy; anything else goes through a buffer (MRI's fallback).
  def self.copy_stream(src, dst, copy_length = nil, src_offset = nil)
    copy_length = ::Kernel.__to_int(copy_length) unless copy_length.nil?
    src_offset = ::Kernel.__to_int(src_offset) unless src_offset.nil?
    opened = []
    s, sk = __oruby_copy_end(src)
    if sk == :path
      s = File.open(s, "rb")
      opened << s
      sk = :io
    end
    d, dk = __oruby_copy_end(dst)
    if dk == :path
      d = File.open(d, "wb")
      opened << d
      dk = :io
    end
    if sk == :io && dk == :io
      return __oruby_copy_stream(s, d, copy_length, src_offset)
    end
    raise ArgumentError, "cannot specify src_offset for non-IO" if src_offset && sk != :io
    total = 0
    buf = "".b
    while copy_length.nil? || total < copy_length
      len = copy_length ? [copy_length - total, 16384].min : 16384
      if sk == :io
        chunk = begin
          src_offset ? s.pread(len, src_offset + total) : s.readpartial(len)
        rescue EOFError
          nil
        end
        break if chunk.nil?
        buf = chunk
      elsif ::Kernel.__responds?(s, :readpartial)
        begin
          s.readpartial(len, buf)
        rescue EOFError
          break
        end
      else
        break if s.read(len, buf).nil?
      end
      break if buf.empty?
      d.write(buf)
      total += buf.bytesize
    end
    total
  ensure
    opened&.each { |f| f.close unless f.closed? }
  end

  def self.__oruby_copy_end(x)
    return [x, :io] if IO === x
    io = IO.try_convert(x)
    return [io, :io] if io
    return [x, :path] if String === x
    if ::Kernel.__responds?(x, :to_path)
      path = x.to_path
      unless String === path
        raise TypeError, "can't convert #{::Kernel.__cname(x)} into String (#{::Kernel.__cname(x)}#to_path gives #{::Kernel.__cname(path)})"
      end
      return [path, :path]
    end
    [x, :obj]
  end

  def self.try_convert(obj)
    return obj if IO === obj
    return nil unless ::Kernel.__responds?(obj, :to_io)
    r = obj.to_io
    return r if r.nil? || IO === r
    cname = ::Kernel.__cname(obj)
    raise TypeError, "can't convert #{cname} into IO (#{cname}#to_io gives #{::Kernel.__cname(r)})"
  end
end

module Kernel
  # the Enumerator of IO#each_byte & co. without a block: it has no size
  def self.__oruby_io_enum(io, meth)
    Enumerator.new { |y| io.__send__(meth) { |x| y << x } }
  end
end
