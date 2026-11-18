class ARGFClass
  include Enumerable

  def inplace_mode
    @use_global_argv ? $-i : @inplace_mode
  end

  def inplace_mode=(val)
    if @use_global_argv
      $-i = val
    else
      @inplace_mode = val
    end
  end

  def initialize(*argv)
    @lineno = 0
    @current_io = nil
    @last_io = nil
    @current_file = nil
    @inplace_mode = nil
    @inplace_out = nil
    @orig_stdout = nil
    @binmode = false
    @ext_enc = nil
    @int_enc = nil
    @use_global_argv = false
    @argv = argv
    @files = nil
    @started = false
  end

  def filename
    ensure_files
    if @current_file
      @current_file
    elsif @files && !@files.empty?
      @files.first
    else
      "-"
    end
  end
  alias path filename

  def argv
    @use_global_argv ? (defined?(ARGV) ? ARGV : []) : @argv
  end

  def file
    @current_io || ensure_io || @last_io
  end
  alias to_io file

  def binmode
    @binmode = true
    @ext_enc = Encoding::BINARY if defined?(Encoding)
    io = ensure_io
    if io
      io.binmode if io.respond_to?(:binmode)
      io.set_encoding(Encoding::BINARY) if defined?(Encoding) && io.respond_to?(:set_encoding)
    end
    self
  end

  def binmode?
    return true if @binmode
    io = ensure_io
    io && io.respond_to?(:binmode?) ? io.binmode? : false
  end

  def lineno
    @lineno
  end

  def lineno=(n)
    @lineno = n
    $. = n
  end

  def to_s
    "ARGF"
  end
  alias inspect to_s

  def pos
    raise ArgumentError, "closed stream" if @current_io.nil? || io_closed?(@current_io)
    @current_io.pos
  end
  alias tell pos

  def pos=(offset)
    raise ArgumentError, "closed stream" if @current_io.nil? || io_closed?(@current_io)
    @current_io.pos = offset
  end

  def fileno
    raise ArgumentError, "closed stream" if @current_io.nil? || io_closed?(@current_io)
    @current_io.fileno
  end
  alias to_i fileno

  def seek(amount, whence = (defined?(IO::SEEK_SET) ? IO::SEEK_SET : 0))
    io = ensure_io
    raise ArgumentError, "no stream to seek" unless io
    io.seek(amount, whence)
  end

  def rewind
    io = ensure_io
    raise ArgumentError, "no stream to rewind" unless io
    @lineno -= io.lineno if io.respond_to?(:lineno)
    @lineno = 0 if @lineno < 0
    $. = @lineno
    io.rewind if io.respond_to?(:rewind)
    0
  end

  def eof?
    raise IOError, "closed stream" if @current_io.nil? || io_closed?(@current_io)
    @current_io.eof?
  end
  alias eof eof?

  def closed?
    io = @current_io || @last_io
    io ? io_closed?(io) : false
  end

  def close
    ensure_io
    if @current_io
      @last_io = @current_io
      if @current_file != "-" && !@current_io.equal?($stdin)
        @current_io.close unless io_closed?(@current_io)
      end
    end
    close_inplace
    self
  end

  def skip
    if @current_io
      next_file
    end
    self
  end

  def set_encoding(*args)
    if args.size == 1 && String === args[0] && args[0].include?(":")
      ext_s, int_s = args[0].split(":", 2)
      @ext_enc = ext_s.empty? ? nil : Encoding.find(ext_s)
      @int_enc = int_s.empty? ? nil : Encoding.find(int_s)
    else
      @ext_enc = args[0]
      @int_enc = args[1] if args.size > 1
    end
    io = ensure_io
    io.set_encoding(*args) if io && io.respond_to?(:set_encoding)
    self
  end

  def external_encoding
    io = ensure_io
    io && io.respond_to?(:external_encoding) ? io.external_encoding : @ext_enc
  end

  def internal_encoding
    io = ensure_io
    io && io.respond_to?(:internal_encoding) ? io.internal_encoding : @int_enc
  end

  def gets(*args, **kw)
    loop do
      io = ensure_io
      break unless io
      line = kw.empty? ? io.gets(*args) : io.gets(*args, **kw)
      if line
        @lineno += 1
        $. = @lineno
        $_ = line
        return line
      else
        next_file
      end
    end
    $_ = nil
    nil
  end

  def readline(*args, **kw)
    loop do
      io = ensure_io
      raise EOFError, "end of file reached" unless io
      if !(IO === io)
        line = kw.empty? ? io.readline(*args) : io.readline(*args, **kw)
        @lineno += 1
        $. = @lineno
        $_ = line
        return line
      end
      line = kw.empty? ? io.gets(*args) : io.gets(*args, **kw)
      if line
        @lineno += 1
        $. = @lineno
        $_ = line
        return line
      else
        next_file
      end
    end
  end

  def getc
    loop do
      io = ensure_io
      return nil unless io
      c = io.getc rescue nil
      if c.nil? && io.respond_to?(:read)
        c = io.read(1)
      end
      if c && !c.empty?
        return c
      else
        next_file
      end
    end
  end

  def readchar
    c = getc
    raise EOFError, "end of file reached" if c.nil?
    c
  end

  def getbyte
    loop do
      io = ensure_io
      return nil unless io
      b = io.getbyte rescue nil
      if b.nil? && io.respond_to?(:read)
        ch = io.read(1)
        b = ch ? ch.ord : nil
      end
      if b
        return b
      else
        next_file
      end
    end
  end

  def readbyte
    b = getbyte
    raise EOFError, "end of file reached" if b.nil?
    b
  end

  def each_byte
    return to_enum(:each_byte) { nil } unless block_given?
    while b = getbyte
      yield b
    end
    self
  end
  alias bytes each_byte

  def each_char
    return to_enum(:each_char) { nil } unless block_given?
    while c = getc
      yield c
    end
    self
  end
  alias chars each_char

  def each_codepoint
    return to_enum(:each_codepoint) { nil } unless block_given?
    while c = getc
      yield c.ord
    end
    self
  end
  alias codepoints each_codepoint

  def each_line(*args, **kw, &block)
    unless block
      return to_enum(:each_line, *args) { nil } if kw.empty?
      return Enumerator.new { |y| each_line(*args, **kw) { |l| y << l } }
    end
    io = ensure_io
    if io && !(IO === io)
      if kw.empty?
        io.each_line(*args) do |line|
          @lineno += 1
          $. = @lineno
          yield line
        end
      else
        io.each_line(*args, **kw) do |line|
          @lineno += 1
          $. = @lineno
          yield line
        end
      end
      next_file
      return self
    end
    while line = (kw.empty? ? gets(*args) : gets(*args, **kw))
      yield line
    end
    self
  end
  alias each each_line

  def readlines(*args, **kw)
    io = ensure_io
    if io && !(IO === io)
      lines = kw.empty? ? io.readlines(*args) : io.readlines(*args, **kw)
      next_file
      return lines
    end
    lines = []
    while line = (kw.empty? ? gets(*args) : gets(*args, **kw))
      lines << line
    end
    lines
  end
  alias to_a readlines

  def read(length = nil, outbuf = nil)
    res = nil
    loop do
      io = ensure_io
      break unless io
      if length.nil?
        chunk = io.read
        if chunk
          if res.nil?
            res = chunk.dup
          else
            res << chunk
          end
        end
        next_file
      else
        needed = length - (res ? res.bytesize : 0)
        break if needed <= 0
        chunk = io.read(needed)
        if chunk && !chunk.empty?
          if res.nil?
            res = chunk.dup
          else
            res << chunk
          end
          break if res.bytesize >= length
        else
          next_file
        end
      end
    end
    if res.nil?
      if length && !@current_io
        outbuf.replace("") if outbuf
        return nil
      end
      res = +""
      if defined?(Encoding) && @ext_enc
        res.force_encoding(@ext_enc)
      elsif defined?(Encoding) && Encoding.default_external
        res.force_encoding(Encoding.default_external)
      end
    end
    outbuf.replace(res) if outbuf
    res
  end

  def readpartial(length, outbuf = nil)
    io = ensure_io
    unless io
      outbuf.replace("") if outbuf
      raise EOFError, "end of file reached"
    end
    begin
      return outbuf ? io.readpartial(length, outbuf) : io.readpartial(length)
    rescue EOFError
      outbuf.replace("") if outbuf
      if @files && !@files.empty?
        next_file
        return outbuf || +""
      end
      next_file
      raise EOFError, "end of file reached"
    end
  end

  def read_nonblock(maxlen, outbuf = (no_buf = true; nil), exception: true)
    unless exception == true || exception == false
      raise ArgumentError, "expected true or false as exception: #{exception.inspect}"
    end
    io = ensure_io
    unless io
      outbuf.replace("") if !no_buf && outbuf
      return nil unless exception
      raise EOFError, "end of file reached"
    end
    begin
      if no_buf
        res = io.read_nonblock(maxlen, exception: exception)
      else
        res = io.read_nonblock(maxlen, outbuf, exception: exception)
      end
      if res.nil? && !exception
        if @files && !@files.empty?
          next_file
          return (!no_buf && outbuf) ? outbuf.replace("") : +""
        end
        next_file
      end
      res
    rescue EOFError
      outbuf.replace("") if !no_buf && outbuf
      if @files && !@files.empty?
        next_file
        return (!no_buf && outbuf) ? outbuf : +""
      end
      next_file
      raise EOFError, "end of file reached"
    end
  end

  private

  def io_closed?(io)
    io.respond_to?(:closed?) ? io.closed? : false
  end

  def close_inplace
    if @inplace_out
      @inplace_out.close unless @inplace_out.closed?
      @inplace_out = nil
    end
    if @orig_stdout
      $stdout = @orig_stdout
      @orig_stdout = nil
    end
  end

  def ensure_files
    return if @started
    @started = true
    cur_argv = argv
    @files = (cur_argv.nil? || cur_argv.empty?) ? ["-"] : cur_argv.dup
  end

  def open_inplace(path)
    mode = inplace_mode
    if mode.empty?
      io = File.open(path, "r")
      File.unlink(path)
      @inplace_out = File.open(path, "w")
    else
      backup = path + mode
      File.rename(path, backup)
      io = File.open(backup, "r")
      @inplace_out = File.open(path, "w")
    end
    @orig_stdout ||= $stdout
    $stdout = @inplace_out
    io
  end

  def ensure_io
    ensure_files
    return @current_io if @current_io && !io_closed?(@current_io)

    while !@files.empty?
      @current_file = @files.shift
      cur_argv = argv
      cur_argv.shift if cur_argv && !cur_argv.empty?
      ::Kernel.__oruby_gv_raw_set(:$FILENAME, @current_file)
      if @current_file == "-"
        @current_io = $stdin
        return @current_io
      else
        begin
          io = inplace_mode ? open_inplace(@current_file) : File.open(@current_file, "r")
          if @binmode
            io.binmode if io.respond_to?(:binmode)
            io.set_encoding(Encoding::BINARY) if defined?(Encoding) && io.respond_to?(:set_encoding)
          elsif @ext_enc
            if @int_enc
              io.set_encoding(@ext_enc, @int_enc)
            else
              io.set_encoding(@ext_enc)
            end
          end
          @current_io = io
          return @current_io
        rescue => e
          $stderr.puts "cannot open #{@current_file}: #{e.message}"
        end
      end
    end

    close_inplace
    nil
  end

  def next_file
    if @current_io
      @last_io = @current_io
      if @current_file != "-" && !@current_io.equal?($stdin)
        @current_io.close unless io_closed?(@current_io)
      end
    end
    close_inplace
    @current_io = nil
  end
end

ARGF = ARGFClass.new
ARGF.instance_variable_set(:@use_global_argv, true)

module Kernel
  def gets(*args, **kw)
    kw.empty? ? ARGF.gets(*args) : ARGF.gets(*args, **kw)
  end

  def readline(*args, **kw)
    kw.empty? ? ARGF.readline(*args) : ARGF.readline(*args, **kw)
  end

  def getc
    ARGF.getc
  end

  def getbyte
    ARGF.getbyte
  end

  def readchar
    ARGF.readchar
  end

  def readbyte
    ARGF.readbyte
  end

  def readlines(*args, **kw)
    kw.empty? ? ARGF.readlines(*args) : ARGF.readlines(*args, **kw)
  end
end
