# IO encodings as MRI's io.c keeps them. Each IO has enc and enc2: without
# transcoding enc is the external encoding (nil: Encoding.default_external
# at read time); with transcoding enc2 is the external encoding and enc the
# internal one. They are set from the open mode ("r:ext:int"), the encoding
# options, #set_encoding and #binmode; text read from the IO is tagged with
# (or transcoded into) them.
class IO
  # [readable, writable, binmode] of a mode String or Integer
  def self.__oruby_fmode(mode, flags = nil)
    readable, writable, binmode = true, false, false
    case mode
    when Integer
      acc = mode & 3
      readable = acc != File::Constants::WRONLY
      writable = acc != File::Constants::RDONLY
    when String
      m = mode.split(":", 2)[0]
      case m[0]
      when "w", "a" then readable, writable = false, true
      end
      readable = writable = true if m.include?("+")
      binmode = m.include?("b")
    end
    if flags.is_a?(Integer) && mode.nil?
      acc = flags & 3
      readable = acc != File::Constants::WRONLY
      writable = acc != File::Constants::RDONLY
    end
    [readable, writable, binmode]
  end

  # an Encoding from a name or Encoding; "-" (internal) means none
  def self.__oruby_find_enc(v)
    return v if v.nil? || v.is_a?(Encoding)
    s = Kernel.__implicit(v, String)
    raise ArgumentError, "ASCII incompatible encoding needs binmode" unless s.encoding.ascii_compatible?
    Encoding.find(s)
  end

  # MRI's rb_io_ext_int_to_encs: [enc, enc2]
  def self.__oruby_ext_int(ext, intern, by_bom = false)
    default_ext = false
    if ext.nil?
      ext = Encoding.default_external
      default_ext = true
    end
    if ext == Encoding::BINARY
      intern = nil
    elsif intern.nil?
      intern = Encoding.default_internal
    end
    if intern.nil? || intern == :none || (!by_bom && intern == ext)
      [default_ext && intern != ext ? nil : ext, nil]
    else
      [intern, ext]
    end
  end

  def __oruby_readable?
    @__fmode ? @__fmode[0] : !equal?(STDOUT) && !equal?(STDERR)
  end

  def __oruby_writable?
    @__fmode ? @__fmode[1] : equal?(STDOUT) || equal?(STDERR)
  end

  # parse the encoding part of a mode and the encoding options into
  # [ext, intern, by_bom]
  def self.__oruby_mode_encodings(mode, opts)
    ext = intern = nil
    by_bom = false
    if mode.is_a?(String) && (i = mode.index(":"))
      e, n = mode[i + 1..].split(":", 2)
      ext, intern = e, n
    end
    if opts
      if (enc = opts[:encoding])
        if enc.is_a?(Encoding)
          ext = enc
        else
          ext, intern = Kernel.__implicit(enc, String).split(":", 2)
        end
      end
      ext = opts[:external_encoding] if opts.key?(:external_encoding)
      intern = opts[:internal_encoding] if opts.key?(:internal_encoding)
    end
    if ext.is_a?(String) && ext =~ /\Abom\|/i
      by_bom = true
      ext = ext[4..]
    end
    ext = nil if ext == ""
    intern = (intern == "-" ? :none : intern)
    intern = nil if intern == ""
    [ext && __oruby_find_enc(ext), intern == :none ? :none : (intern && __oruby_find_enc(intern)), by_bom]
  end

  def __oruby_init_encoding(mode, opts)
    flags = opts && opts[:flags]
    mode = opts[:mode] if mode.nil? && opts && opts.key?(:mode)
    @__fmode = IO.__oruby_fmode(mode, flags)
    binmode = @__fmode[2] || (opts && opts[:binmode] == true)
    binmode = false if opts && opts[:textmode] == true
    ext, intern, by_bom = IO.__oruby_mode_encodings(mode, opts)
    ext ||= Encoding::BINARY if binmode
    # sockets are binary streams
    if !binmode && defined?(::BasicSocket) && is_a?(::BasicSocket)
      binmode = true
      ext ||= Encoding::BINARY
    end
    @__binmode = binmode
    @__enc, @__enc2 = IO.__oruby_ext_int(ext, intern, by_bom)
    @__ecopts = nil
    __oruby_set_encoding_by_bom if by_bom && @__fmode[0]
  end

  alias __oruby_go_binmode binmode
  def binmode
    __oruby_go_binmode
    @__binmode = true
    @__enc = Encoding::BINARY
    @__enc2 = nil
    self
  end

  alias __oruby_go_binmode? binmode?
  def binmode?
    raise IOError, "closed stream" if closed?
    @__binmode.nil? ? __oruby_go_binmode? : @__binmode
  end

  def external_encoding
    return @__enc2 if @__enc2
    return @__enc if __oruby_writable?
    @__enc || Encoding.default_external
  end

  def internal_encoding
    @__enc2 ? (@__enc || Encoding.default_external) : nil
  end

  def set_encoding(*args, **opts)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..2)" if args.empty? || args.size > 2
    if (nl = opts[:newline])
      unless %i[universal crlf cr lf].include?(nl)
        raise ArgumentError, nl.is_a?(Symbol) ? "unexpected value for newline option: #{nl}" : "unexpected value for newline option"
      end
    end
    v1, v2 = args
    by_bom = false
    if args.size == 2 && !v2.nil?
      raise TypeError, "no implicit conversion of nil into String" if v1.nil?
      ext = IO.__oruby_find_enc(v1)
      intern = v2.is_a?(Encoding) ? v2 : Kernel.__implicit(v2, String)
      if intern.is_a?(String)
        raise ArgumentError, "ASCII incompatible encoding needs binmode" unless intern.encoding.ascii_compatible?
        intern = intern == "-" ? :none : Encoding.find(intern)
      end
      intern = :none if intern == ext
      @__enc, @__enc2 = IO.__oruby_ext_int(ext, intern)
    elsif v1.nil?
      @__enc, @__enc2 = IO.__oruby_ext_int(nil, nil)
    else
      if v1.is_a?(Encoding)
        ext, intern = v1, nil
      else
        s = Kernel.__implicit(v1, String)
        raise ArgumentError, "ASCII incompatible encoding needs binmode" unless s.encoding.ascii_compatible?
        ext, intern, by_bom = IO.__oruby_mode_encodings("r:#{s}", nil)
        intern = :none if intern == ext
      end
      @__enc, @__enc2 = IO.__oruby_ext_int(ext, intern, by_bom)
    end
    if __oruby_readable? && !@__enc2 && !@__binmode
      e = @__enc || Encoding.default_external
      raise ArgumentError, "ASCII incompatible encoding needs binmode" unless e.ascii_compatible?
    end
    @__ecopts = opts.empty? ? nil : opts
    # a BOM is read now from files only: a pipe or socket would block
    __oruby_set_encoding_by_bom if by_bom && __oruby_readable? && is_a?(File)
    self
  end

  BOMS = [
    ["\x00\x00\xFE\xFF".b, "UTF-32BE"], ["\xFF\xFE\x00\x00".b, "UTF-32LE"],
    ["\xEF\xBB\xBF".b, "UTF-8"], ["\xFE\xFF".b, "UTF-16BE"], ["\xFF\xFE".b, "UTF-16LE"]
  ].freeze
  private_constant :BOMS if respond_to?(:private_constant, true)

  def __oruby_set_encoding_by_bom
    got = "".b
    BOMS.each do |bom, name|
      while got.bytesize < bom.bytesize
        b = getbyte
        break if b.nil?
        got << b.chr
      end
      if got.start_with?(bom)
        got.byteslice(bom.bytesize..).reverse.each_byte { |b| ungetbyte(b) }
        @__enc = Encoding.find(name)
        @__enc2 = nil
        return @__enc
      end
    end
    got.reverse.each_byte { |b| ungetbyte(b) }
    nil
  end

  def set_encoding_by_bom
    return nil unless __oruby_readable?
    raise ArgumentError, "ASCII incompatible encoding needs binmode" unless binmode?
    raise ArgumentError, "encoding conversion is set" if @__enc2
    if @__enc && @__enc != Encoding::BINARY
      raise ArgumentError, "encoding is set to #{@__enc} already"
    end
    __oruby_set_encoding_by_bom
  end

  # text read from the IO, in its read encoding (transcoded when set)
  def __oruby_text(s)
    return s unless s.is_a?(String)
    if @__enc2
      s.force_encoding(@__enc2)
      @__ecopts ? s.encode(@__enc, **@__ecopts) : s.encode(@__enc)
    else
      s.force_encoding(@__enc || Encoding.default_external)
    end
  end

  alias __oruby_go_read read
  def read(length = nil, outbuf = nil)
    return __oruby_go_read(length, outbuf) unless length.nil?
    s = outbuf.nil? ? __oruby_go_read : __oruby_go_read(nil, outbuf)
    __oruby_text(s)
  end

  # one character in the read encoding, read a byte at a time
  def getc
    raise IOError, "closed stream" if closed?
    b = getbyte
    return nil if b.nil?
    enc = @__enc2 || @__enc || Encoding.default_external
    s = b.chr
    unless enc == Encoding::BINARY || (b < 0x80 && enc.ascii_compatible?)
      want = if enc == Encoding::UTF_8
               b >= 0xf0 ? 4 : b >= 0xe0 ? 3 : b >= 0xc0 ? 2 : 1
             else
               4
             end
      extra = "".b
      while s.bytesize < want
        t = s.dup.force_encoding(enc)
        break if enc != Encoding::UTF_8 && t.valid_encoding? && t.size == 1
        nb = getbyte
        break if nb.nil?
        s << nb.chr
      end
      t = s.dup.force_encoding(enc)
      unless t.valid_encoding?
        # an invalid sequence: its first byte is the character
        s.byteslice(1..).reverse.each_byte { |x| ungetbyte(x) }
        s = s.byteslice(0, 1)
      end
    end
    __oruby_text(s)
  end

  def readchar
    c = getc
    raise EOFError, "end of file reached" if c.nil?
    c
  end

  def each_char
    return to_enum(:each_char) unless block_given?
    while (c = getc)
      yield c
    end
    self
  end

  # MRI raises IOError on a closed stream for these; the Go methods don't
  %i[getbyte each_byte lineno lineno= eof eof? fileno to_i sync sync= write << print puts printf putc
     flush fsync fdatasync ungetc ungetbyte readbyte stat isatty tty? each_codepoint close_on_exec?
     autoclose= pread pwrite].each do |m|
    next unless method_defined?(m)
    alias_method :"__oruby_open_#{m}", m
    define_method(m) do |*a, **kw, &b|
      raise IOError, "closed stream" if closed?
      kw.empty? ? __send__(:"__oruby_open_#{m}", *a, &b) : __send__(:"__oruby_open_#{m}", *a, **kw, &b)
    end
  end

  alias_method :isatty, :tty?
  alias_method :to_i, :fileno
  alias_method :eof, :eof?

  alias __oruby_go_gets gets

  alias __oruby_go_lineno lineno
  def lineno
    raise IOError, "closed stream" if closed?
    raise IOError, "not opened for reading" unless __oruby_readable?
    __oruby_go_lineno || 0
  end

  alias __oruby_go_set_lineno lineno=
  def lineno=(n)
    raise IOError, "closed stream" if closed?
    raise IOError, "not opened for reading" unless __oruby_readable?
    n = Kernel.__implicit(n, Integer)
    raise RangeError, "integer #{n} too big to convert to 'int'" if n > 0x7fff_ffff || n < -0x8000_0000
    __oruby_go_set_lineno(n)
  end

  # MRI's prepare_getline_args: [separator, limit, chomp]
  def self.__oruby_getline_args(args, kw)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)" if args.size > 2
    sep = $/
    limit = nil
    conv_limit = lambda do |v|
      raise TypeError, "no implicit conversion of Hash into Integer" if v.is_a?(Hash)
      v = Kernel.__implicit(v, Integer)
      raise RangeError, "bignum too big to convert into 'long'" if v > 0x7fff_ffff_ffff_ffff || v < -0x8000_0000_0000_0000
      v
    end
    case args.size
    when 1
      a = args[0]
      if a.nil?
        sep = nil
      elsif a.is_a?(String)
        sep = a
      elsif a.is_a?(Integer)
        limit = conv_limit.call(a)
      else
        str = a.is_a?(Hash) ? nil : Kernel.__check_implicit(a, String)
        str ? (sep = str) : (limit = conv_limit.call(a))
      end
    when 2
      sep = args[0].nil? ? nil : Kernel.__implicit(args[0], String)
      limit = args[1].nil? ? nil : conv_limit.call(args[1])
    end
    limit = nil if limit && limit < 0
    [sep, limit, kw[:chomp] ? true : false]
  end

  # the next line, or nil at EOF; $_ is left as it was
  def __oruby_getline(sep, limit, chomp)
    raise IOError, "closed stream" if closed?
    return "".force_encoding(@__enc || Encoding.default_external) if limit == 0
    saved = $_
    line = nil
    begin
      if sep.nil?
        line = limit ? __oruby_go_read(limit) : __oruby_go_read
        line = nil if line == ""
        line = __oruby_complete_char(line) if line && limit
        __oruby_go_set_lineno((__oruby_go_lineno || 0) + 1) if line
      elsif sep.empty?
        # paragraph mode: leading newlines are skipped, a paragraph ends at
        # "\n\n" and the newlines after it are consumed
        while (b = getbyte) == 10
        end
        return nil if b.nil?
        ungetbyte(b)
        line = limit ? __oruby_go_gets("\n\n", limit) : __oruby_go_gets("\n\n")
        if line
          if line.end_with?("\n\n")
            while (b = getbyte) == 10
            end
            ungetbyte(b) unless b.nil?
          end
          line = line.sub(/\n+\z/, "") if chomp
        end
      else
        line = limit ? __oruby_go_gets(sep, limit) : __oruby_go_gets(sep)
        line = __oruby_complete_char(line) if line && limit
        if line && chomp
          if sep == "\n"
            line = line.chomp
          elsif line.end_with?(sep)
            line = line.byteslice(0, line.bytesize - sep.bytesize)
          end
        end
      end
    ensure
      $_ = saved
    end
    return nil if line.nil?
    $. = __oruby_go_lineno || 0
    __oruby_text(line)
  end

  # a limited read does not split a character: read on to its end
  def __oruby_complete_char(line)
    enc = @__enc2 || @__enc || Encoding.default_external
    return line if enc == Encoding::BINARY || !enc.ascii_compatible? || line.bytesize == 0
    line = line.b
    16.times do
      break if line.dup.force_encoding(enc).valid_encoding?
      b = getbyte
      break if b.nil?
      line << b.chr
    end
    line
  end

  def gets(*args, **kw)
    sep, limit, chomp = IO.__oruby_getline_args(args, kw)
    $_ = __oruby_getline(sep, limit, chomp)
  end

  def readline(*args, **kw)
    line = gets(*args, **kw)
    raise EOFError, "end of file reached" if line.nil?
    line
  end

  def readlines(*args, **kw)
    sep, limit, chomp = IO.__oruby_getline_args(args, kw)
    raise ArgumentError, "invalid limit: 0 for readlines" if limit == 0
    lines = []
    while (l = __oruby_getline(sep, limit, chomp))
      lines << l
    end
    lines
  end

  def each(*args, **kw)
    sep, limit, chomp = IO.__oruby_getline_args(args, kw)
    # mruby's enum_for would pass the keywords as a positional Hash
    return Enumerator.new { |y| each(*args, **kw) { |l| y << l } } unless block_given?
    raise ArgumentError, "invalid limit: 0 for each_line" if limit == 0
    while (l = __oruby_getline(sep, limit, chomp))
      yield l
    end
    self
  end
  alias each_line each

  alias __oruby_go_reopen reopen
  def reopen(*args, **kw)
    r = kw.empty? ? __oruby_go_reopen(*args) : __oruby_go_reopen(*args, **kw)
    other = args[0]
    if other.is_a?(IO)
      %i[@__fmode @__enc @__enc2 @__binmode @__ecopts].each do |iv|
        instance_variable_set(iv, other.instance_variable_get(iv))
      end
    elsif args.size > 1 || !kw.empty?
      __oruby_init_encoding(args[1], kw)
    end
    r
  end

  class << self
    # IO.readlines / IO.foreach: the keyword arguments other than chomp are
    # open options
    def __oruby_split_kw(kw)
      line_kw = {}
      open_kw = {}
      kw.each { |k, v| k == :chomp ? line_kw[k] = v : open_kw[k] = v }
      [line_kw, open_kw]
    end

    alias __oruby_go_s_readlines readlines
    alias __oruby_go_s_foreach foreach

    # a Go stream given by an embedding program, not a path
    def __oruby_go_stream?(name)
      !(name.nil? || name.is_a?(String) || Kernel.__responds?(name, :to_path) || Kernel.__responds?(name, :to_str))
    end

    def readlines(name, *args, **kw)
      return __oruby_go_s_readlines(name, *args, **kw) if __oruby_go_stream?(name)
      line_kw, open_kw = __oruby_split_kw(kw)
      IO.__oruby_getline_args(args, line_kw)
      File.open(__oruby_path(name), **open_kw) { |f| f.readlines(*args, **line_kw) }
    end

    def foreach(name, *args, **kw, &block)
      return __oruby_go_s_foreach(name, *args, **kw, &block) if __oruby_go_stream?(name)
      return Enumerator.new { |y| foreach(name, *args, **kw) { |l| y << l } } unless block
      line_kw, open_kw = __oruby_split_kw(kw)
      IO.__oruby_getline_args(args, line_kw)
      $_ = nil
      File.open(__oruby_path(name), **open_kw) { |f| f.each_line(*args, **line_kw, &block) }
      nil
    end

    def __oruby_path(name)
      raise TypeError, "no implicit conversion of nil into String" if name.nil?
      return name if name.is_a?(String)
      name.respond_to?(:to_path) ? name.to_path : Kernel.__implicit(name, String)
    end
  end


  # MRI's rb_io_extract_modeenc checks: [mode, opts] with mode coerced
  def self.__oruby_check_mode(mode, opts)
    coerce = lambda do |m|
      next m if m.nil? || m.is_a?(Integer) || m.is_a?(String)
      Kernel.__responds?(m, :to_int) ? Kernel.__implicit(m, Integer) : Kernel.__implicit(m, String)
    end
    mode = coerce.call(mode)
    if opts.key?(:mode)
      raise ArgumentError, "mode specified twice" unless mode.nil?
      mode = coerce.call(opts[:mode])
    end
    raise ArgumentError, "invalid access mode #{mode}" if mode == ""
    if mode.is_a?(String)
      access = mode.split(":", 2)[0]
      b = access.include?("b")
      t = access.include?("t")
      raise ArgumentError, "both textmode and binmode specified" if b && t
      raise ArgumentError, "binmode specified twice" if opts.key?(:binmode) && (b || t)
      raise ArgumentError, "textmode specified twice" if opts.key?(:textmode) && (b || t)
      if mode.include?(":") && (opts.key?(:encoding) || opts.key?(:external_encoding) || opts.key?(:internal_encoding))
        raise ArgumentError, "encoding specified twice"
      end
    end
    raise ArgumentError, "both textmode and binmode specified" if opts[:binmode] && opts[:textmode]
    if opts.key?(:encoding) && !opts[:encoding].nil?
      %i[external_encoding internal_encoding].each do |k|
        if opts.key?(k)
          warn "Ignoring encoding parameter '#{opts[:encoding]}': #{k} is used"
          opts = opts.dup
          opts.delete(:encoding)
          break
        end
      end
    end
    [mode, opts]
  end

  alias __oruby_go_initialize initialize
  def initialize(fd, *rest, **opts, &block)
    raise ArgumentError, "wrong number of arguments (given #{rest.size + 1}, expected 1..2)" if rest.size > 1
    if fd.is_a?(IO) && (fd.instance_variable_defined?(:@__fmode) || fd.equal?(STDIN) || fd.equal?(STDOUT) || fd.equal?(STDERR))
      raise TypeError, "no implicit conversion of #{fd.class} into Integer"
    end
    unless fd.is_a?(Integer) || fd.nil? || Kernel.__responds?(fd, :to_int)
      # a Go stream (io.Reader, *os.File ...) wrapped by an embedding program
      __oruby_go_initialize(fd, *rest, **opts, &block)
      return __oruby_init_encoding(rest[0], opts)
    end
    fd = Kernel.__implicit(fd, Integer) unless fd.is_a?(Integer)
    raise Errno::EBADF, "Bad file descriptor - #{fd}" if fd < 0
    mode, opts = IO.__oruby_check_mode(rest[0], opts)
    go_mode = mode.is_a?(String) ? mode.split(":", 2)[0] : mode
    go_mode.nil? ? __oruby_go_initialize(fd, &block) : __oruby_go_initialize(fd, go_mode, &block)
    __oruby_init_encoding(mode, opts)
    self.autoclose = !!opts[:autoclose] if opts.key?(:autoclose)
  end

  class << self
    def for_fd(*args, **opts, &block)
      opts.empty? ? new(*args, &block) : new(*args, **opts, &block)
    end

    def open(*args, **opts)
      io = opts.empty? ? new(*args) : new(*args, **opts)
      return io unless block_given?
      begin
        yield io
      ensure
        begin
          io.close
        rescue IOError => e
          raise unless e.message == "closed stream"
        end
      end
    end
  end

  class << self
    alias __oruby_pipe pipe
    def pipe(*args, **opts, &block)
      ext = args[0]
      r, w = __oruby_pipe
      r.__oruby_init_encoding("r", nil)
      w.__oruby_init_encoding("w", nil)
      r.set_encoding(*args, **opts) unless args.empty?
      return [r, w] unless block
      begin
        yield r, w
      ensure
        r.close unless r.closed?
        w.close unless w.closed?
      end
    end
  end
end

class File
  alias __oruby_go_file_initialize initialize
  def initialize(*args, **opts, &block)
    opts.empty? ? __oruby_go_file_initialize(*args, &block) : __oruby_go_file_initialize(*args, **opts, &block)
    hash = args.last.is_a?(Hash) ? args.last : nil
    mode = args[1].is_a?(Hash) ? nil : args[1]
    __oruby_init_encoding(mode, hash ? hash.merge(opts) : opts)
  end
end
