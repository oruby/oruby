# StringIO: IO-like access to a String (byte positions, shared backing String)
class StringIO
  include Enumerable

  VERSION = "3.1.2"
  MAX_LENGTH = 0x7fffffffffffffff

  module GenericReadable; end
  module GenericWritable; end
  include GenericReadable
  include GenericWritable

  def self.open(*args, **opts)
    io = new(*args, **opts)
    return io unless block_given?
    begin
      yield io
    ensure
      io.instance_variable_set(:@string, nil)
      io.instance_variable_set(:@readable, false)
      io.instance_variable_set(:@writable, false)
    end
  end

  attr_reader :lineno

  def initialize(string = nil, mode = nil, **opts)
    __setup(string, mode, opts, true)
  end
  private :initialize

  def initialize_copy(other)
    super
    self
  end

  def reopen(*args, **opts)
    if args.size == 1 && args[0].is_a?(StringIO) && opts.empty?
      other = args[0]
      @string = other.string
      @pos = other.pos
      @lineno = other.lineno
      @readable = !other.closed_read?
      @writable = !other.closed_write?
      @append = false
      return self
    end
    __setup(args[0], args[1], opts, args.empty?)
    self
  end

  def string
    @string
  end

  def string=(str)
    str = __to_str(str)
    @string = str
    @pos = 0
    @lineno = 0
    @readable = true
    @writable = !str.frozen?
    str
  end

  def pos
    __check_open
    @pos
  end
  alias tell pos

  def pos=(n)
    __check_open
    n = __to_int(n)
    raise Errno::EINVAL, "Invalid argument" if n < 0
    @pos = n
  end

  def lineno=(n)
    @lineno = __to_int(n)
  end

  def rewind
    __check_open
    @pos = 0
    @lineno = 0
    0
  end

  def seek(offset, whence = 0)
    raise IOError, "closed stream" if closed?
    offset = __to_int(offset)
    base = case whence
           when 0, :SET, IO::SEEK_SET then 0
           when 1, :CUR, IO::SEEK_CUR then @pos
           when 2, :END, IO::SEEK_END then @string.bytesize
           else raise Errno::EINVAL, "Invalid argument - invalid whence"
           end
    n = base + offset
    raise Errno::EINVAL, "Invalid argument" if n < 0
    @pos = n
    0
  end

  def size
    raise IOError, "not opened" if @string.nil?
    @string.bytesize
  end
  alias length size

  def eof?
    __check_readable
    @pos >= @string.bytesize
  end
  alias eof eof?

  # -- state ---------------------------------------------------------------

  def close
    @readable = false
    @writable = false
    nil
  end

  def close_read
    raise IOError, "closing non-duplex IO for reading" unless @readable_mode
    @readable = false
    nil
  end

  def close_write
    raise IOError, "closing non-duplex IO for writing" unless @writable_mode
    @writable = false
    nil
  end

  def closed?
    !@readable && !@writable
  end

  def closed_read?
    !@readable
  end

  def closed_write?
    !@writable
  end

  def sync = true
  def sync=(v)
    v
  end

  def flush = self
  def fsync = 0
  def fileno = nil
  def isatty = false
  alias tty? isatty
  def pid = nil
  def fcntl(*) = raise(NotImplementedError, "fcntl() function is unimplemented on this machine")
  def binmode
    @binmode = true
    @string.force_encoding(Encoding::BINARY) if @string && !@string.frozen?
    self
  end

  def external_encoding
    @encoding || (@string ? @string.encoding : nil)
  end

  def internal_encoding = nil

  def set_encoding(ext, _int = nil, **)
    enc = case ext
          when nil then nil
          when Encoding then ext
          else Encoding.find(ext.to_s.split(":").first)
          end
    @encoding = enc
    @string.force_encoding(enc) if enc && @string && !@string.frozen?
    self
  end

  def set_encoding_by_bom
    return nil unless @string
    s = @string
    { "\xEF\xBB\xBF".b => "UTF-8", "\xFE\xFF".b => "UTF-16BE", "\xFF\xFE".b => "UTF-16LE" }.each do |bom, enc|
      if s.byteslice(0, bom.bytesize).b == bom
        @pos = bom.bytesize
        set_encoding(enc)
        return Encoding.find(enc)
      end
    end
    nil
  end

  def inspect
    "#<StringIO:0x#{(object_id << 1).to_s(16).rjust(16, '0')}>"
  end

  # -- reading -------------------------------------------------------------

  def read(length = nil, outbuf = nil)
    __check_readable
    unless length.nil?
      length = __to_int(length)
      raise ArgumentError, "negative length #{length} given" if length < 0
    end
    outbuf = __to_str(outbuf) unless outbuf.nil?
    size = @string.bytesize
    if length.nil?
      data = @pos < size ? @string.byteslice(@pos, size - @pos) : +""
      data.force_encoding(external_encoding || Encoding::UTF_8)
      @pos = size if @pos < size
    else
      if length == 0
        data = +""
      elsif @pos >= size
        outbuf&.replace("")
        return nil
      else
        data = @string.byteslice(@pos, length)
        @pos += data.bytesize
      end
      data.force_encoding(Encoding::BINARY)
    end
    if outbuf
      outbuf.replace(data)
      outbuf.force_encoding(data.encoding)
      return outbuf
    end
    data
  end

  def pread(maxlen, offset, outbuf = nil)
    __check_readable
    maxlen = __to_int(maxlen)
    offset = __to_int(offset)
    raise ArgumentError, "negative string size (or size too big): #{maxlen}" if maxlen < 0
    raise Errno::EINVAL, "Invalid argument - pread: Invalid offset argument" if offset < 0
    return (outbuf ? outbuf.replace("") : +"") if maxlen == 0
    raise EOFError, "end of file reached" if offset >= @string.bytesize
    data = @string.byteslice(offset, maxlen).b
    outbuf ? outbuf.replace(data) : data
  end

  def sysread(length = nil, outbuf = nil, exception: true)
    data = read(length, outbuf)
    if data.nil? || (length.nil? ? false : (length.to_int > 0 && data.empty?))
      return nil unless exception
      raise EOFError, "end of file reached"
    end
    data
  end
  alias readpartial sysread

  def read_nonblock(length, outbuf = nil, exception: true)
    data = read(length, outbuf)
    if data.nil? && length.to_int > 0
      return nil unless exception
      raise EOFError, "end of file reached"
    end
    data
  end

  def getc
    __check_readable
    return nil if @pos >= @string.bytesize
    len = __char_len(@pos)
    c = @string.byteslice(@pos, len)
    @pos += len
    c.force_encoding(external_encoding) if external_encoding
    c
  end

  def getbyte
    __check_readable
    return nil if @pos >= @string.bytesize
    b = @string.getbyte(@pos)
    @pos += 1
    b
  end

  def readchar
    c = getc
    raise EOFError, "end of file reached" if c.nil?
    c
  end

  def readbyte
    b = getbyte
    raise EOFError, "end of file reached" if b.nil?
    b
  end

  def each_char
    return to_enum(:each_char) unless block_given?
    while (c = getc)
      yield c
    end
    self
  end

  def each_byte
    return to_enum(:each_byte) unless block_given?
    while (b = getbyte)
      yield b
    end
    self
  end

  def each_codepoint
    return to_enum(:each_codepoint) unless block_given?
    while (c = getc)
      yield c.ord
    end
    self
  end

  def ungetc(c)
    __check_readable
    return nil if c.nil?
    c = case c
        when Integer then c.chr(external_encoding || Encoding::UTF_8) rescue c.chr
        else __to_str(c)
        end
    __unget(c)
  end

  def ungetbyte(c)
    __check_readable
    return nil if c.nil?
    c = c.is_a?(Integer) ? (c & 0xff).chr : __to_str(c)
    __unget(c)
  end

  def gets(*args, chomp: false)
    __check_readable
    sep, limit = __line_args(args)
    line = __getline(sep, limit, chomp)
    @lineno += 1 if line
    $_ = line
    line
  end

  def readline(*args, chomp: false)
    line = gets(*args, chomp: chomp)
    raise EOFError, "end of file reached" if line.nil?
    line
  end

  def each_line(*args, chomp: false)
    return to_enum(:each_line, *args, chomp: chomp) unless block_given?
    __check_readable
    sep, limit = __line_args(args)
    raise ArgumentError, "invalid limit: 0 for each_line" if limit == 0
    while (line = __getline(sep, limit, chomp))
      @lineno += 1
      yield line
    end
    self
  end
  alias each each_line

  def readlines(*args, chomp: false)
    __check_readable
    sep, limit = __line_args(args)
    raise ArgumentError, "invalid limit: 0 for readlines" if limit == 0
    lines = []
    while (line = __getline(sep, limit, chomp))
      @lineno += 1
      lines << line
    end
    lines
  end

  # -- writing -------------------------------------------------------------

  def write(*objs)
    __check_writable
    objs.sum { |obj| __write(obj.to_s) }
  end

  def syswrite(obj)
    write(obj)
  end

  def write_nonblock(obj, exception: true)
    write(obj)
  end

  def <<(obj)
    write(obj)
    self
  end

  def print(*objs)
    objs = [$_] if objs.empty?
    objs.each_with_index do |o, i|
      write($,) if $, && i > 0
      write(o.to_s)
    end
    write($\) if $\
    nil
  end

  def printf(fmt, *args)
    write(format(fmt, *args))
    nil
  end

  def putc(c)
    __check_writable
    s = c.is_a?(String) ? c[0] : (__to_int(c) & 0xff).chr
    __write(s)
    c
  end

  def puts(*objs)
    __check_writable
    if objs.empty?
      __write("\n")
      return nil
    end
    __puts_ary(objs, [])
    nil
  end

  def truncate(length)
    __check_writable
    length = __to_int(length)
    raise Errno::EINVAL, "negative length" if length < 0
    size = @string.bytesize
    if length < size
      @string.replace(@string.byteslice(0, length))
    elsif length > size
      @string << ("\0" * (length - size))
    end
    0
  end

  private

  def __setup(string, mode, opts, default_string)
    if opts.key?(:mode)
      raise ArgumentError, "mode specified twice" if mode && opts[:mode]
      mode ||= opts[:mode]
    end
    if opts[:binmode] && opts[:textmode]
      raise ArgumentError, "both textmode and binmode specified"
    end
    if string.nil?
      string = +"" if default_string
      string ||= +""
    else
      string = __to_str(string)
    end
    mode = __to_str(mode) if mode && !mode.is_a?(Integer)
    readable, writable, append, truncate = __mode(mode, string)
    if writable && string.frozen?
      raise Errno::EACCES, "Permission denied"
    end
    if truncate
      raise FrozenError, "can't modify frozen String" if string.frozen?
      string.replace("")
    end
    @string = string
    @pos = 0
    @lineno = 0
    @readable = @readable_mode = readable
    @writable = @writable_mode = writable
    @append = append
    @encoding = nil
    if opts[:encoding]
      set_encoding(opts[:encoding])
    elsif opts[:external_encoding]
      set_encoding(opts[:external_encoding])
    end
    binmode if opts[:binmode] || (mode.is_a?(String) && mode.include?("b"))
  end

  def __mode(mode, string)
    case mode
    when nil
      string.frozen? ? [true, false, false, false] : [true, true, false, false]
    when Integer
      acc = mode & 3
      readable = acc == File::RDONLY || acc == File::RDWR
      writable = acc == File::WRONLY || acc == File::RDWR
      [readable, writable, (mode & File::APPEND) != 0, (mode & File::TRUNC) != 0]
    else
      m = mode.split(":").first.to_s.delete("bt")
      case m
      when "r" then [true, false, false, false]
      when "r+" then [true, true, false, false]
      when "w" then [false, true, false, true]
      when "w+" then [true, true, false, true]
      when "a" then [false, true, true, false]
      when "a+" then [true, true, true, false]
      else raise ArgumentError, "invalid access mode #{mode}"
      end
    end
  end

  def __check_open
    raise IOError, "not opened" if @string.nil?
  end

  def __check_readable
    raise IOError, "not opened for reading" if @string.nil? || !@readable
  end

  def __check_writable
    raise IOError, "not opened for writing" if @string.nil? || !@writable
    raise IOError, "not modifiable string" if @string.frozen?
  end

  def __to_str(obj)
    return obj if obj.is_a?(String)
    if obj.respond_to?(:to_str)
      s = obj.to_str
      return s if s.is_a?(String)
    end
    raise TypeError, "no implicit conversion of #{obj.nil? ? 'nil' : obj.class} into String"
  end

  def __to_int(obj)
    return obj if obj.is_a?(Integer)
    if obj.respond_to?(:to_int)
      i = obj.to_int
      return i if i.is_a?(Integer)
    end
    raise TypeError, "no implicit conversion of #{obj.nil? ? 'nil' : obj.class} into Integer"
  end

  def __binary?
    enc = external_encoding
    enc == Encoding::BINARY || enc == Encoding::US_ASCII
  end

  # length in bytes of the character starting at byte position pos
  def __char_len(pos)
    return 1 if __binary?
    b = @string.getbyte(pos)
    need = if b < 0x80 then 1
           elsif b >= 0xf0 && b < 0xf8 then 4
           elsif b >= 0xe0 then 3
           elsif b >= 0xc2 && b < 0xe0 then 2
           else return 1
           end
    size = @string.bytesize
    return 1 if pos + need > size
    1.upto(need - 1) do |i|
      cb = @string.getbyte(pos + i)
      return 1 unless cb >= 0x80 && cb < 0xc0
    end
    need
  end

  def __write(str)
    str = str.to_s
    if str.encoding != @string.encoding && !str.ascii_only? && @string.empty?
      @string.force_encoding(str.encoding)
    end
    @pos = @string.bytesize if @append
    size = @string.bytesize
    if @pos > size
      @string << ("\0" * (@pos - size))
      size = @pos
    end
    len = str.bytesize
    if @pos == size
      @string << str
    else
      @string.bytesplice(@pos, [len, size - @pos].min, str.b.force_encoding(@string.encoding))
    end
    @pos += len
    len
  end

  def __unget(str)
    return nil if str.empty?
    len = str.bytesize
    raise IOError, "not modifiable string" if @string.frozen?
    size = @string.bytesize
    @pos = size if @pos > size
    if len > @pos
      @string.bytesplice(0, @pos, str)
      @pos = 0
    else
      @pos -= len
      @string.bytesplice(@pos, len, str)
    end
    nil
  end

  def __puts_ary(objs, seen)
    objs.each do |obj|
      if obj.is_a?(Array)
        if seen.include?(obj.object_id)
          __write("[...]\n")
        else
          __puts_ary(obj, seen + [obj.object_id])
        end
        next
      end
      if obj.respond_to?(:to_ary) && !obj.is_a?(String)
        ary = obj.to_ary
        if ary.is_a?(Array)
          __puts_ary(ary, seen)
          next
        end
      end
      s = obj.nil? ? "" : obj.to_s
      s = s.is_a?(String) ? s : obj.inspect
      __write(s)
      __write("\n") unless s.end_with?("\n")
    end
  end

  def __line_args(args)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)" if args.size > 2
    sep = $/
    limit = nil
    case args.size
    when 1
      a = args[0]
      if a.nil?
        sep = nil
      elsif a.is_a?(String)
        sep = a
      elsif a.is_a?(Integer) || (a.respond_to?(:to_int) && !a.respond_to?(:to_str))
        limit = __to_int(a)
      else
        sep = __to_str(a)
      end
    when 2
      sep = args[0].nil? ? nil : __to_str(args[0])
      limit = args[1].nil? ? nil : __to_int(args[1])
    end
    limit = nil if limit && limit < 0
    [sep, limit]
  end

  def __getline(sep, limit, chomp)
    size = @string.bytesize
    return nil if @pos >= size
    return +"" if limit == 0
    start = @pos
    paragraph = sep == ""
    if paragraph
      # skip leading newlines
      start += 1 while start < size && @string.getbyte(start) == 10
      return nil if start >= size
      sep_used = "\n\n"
    else
      sep_used = sep
    end
    if sep_used.nil?
      stop = size
      sep_len = 0
    else
      idx = @string.byteindex(sep_used, start)
      if idx
        stop = idx + sep_used.bytesize
        sep_len = sep_used.bytesize
      else
        stop = size
        sep_len = 0
      end
    end
    if limit && stop - start > limit
      stop = start + limit
      # do not split a character
      unless __binary?
        p = start
        while p < stop
          p += __char_len(p)
        end
        stop = p
      end
      sep_len = 0 if sep_len > 0 && stop < (idx || size) + sep_len
      sep_len = 0
    end
    line = @string.byteslice(start, stop - start)
    @pos = stop
    if paragraph
      @pos += 1 while @pos < size && @string.getbyte(@pos) == 10
    end
    line.force_encoding(external_encoding) if external_encoding
    if chomp
      if paragraph
        line = line.sub(/\n+\z/, "")
      elsif sep_len > 0 && line.end_with?(sep_used)
        line = line.byteslice(0, line.bytesize - sep_len)
        line = line.chomp("\r") if sep_used == "\n"
      elsif sep_used.nil?
        # chomp only removes a trailing default separator for nil separator? no
      end
    end
    line
  end
end
