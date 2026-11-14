class ARGFClass
  include Enumerable

  attr_accessor :lineno, :inplace_mode

  def initialize(*argv)
    @lineno = 0
    @current_io = nil
    @last_io = nil
    @current_file = nil
    @inplace_mode = nil
    if argv.empty?
      @argv = nil
      @files = nil
    else
      @argv = argv
      @files = argv.dup
    end
  end

  def path
    filename
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

  def argv
    @argv || (defined?(ARGV) ? ARGV : [])
  end

  def file
    @current_io || ensure_io || @last_io
  end
  alias to_io file

  def binmode
    io = ensure_io
    io.binmode if io && io.respond_to?(:binmode)
    self
  end

  def binmode?
    io = ensure_io
    io.respond_to?(:binmode?) ? io.binmode? : false
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
    raise ArgumentError, "closed stream" if @current_io.nil? || @current_io.closed?
    @current_io.pos
  end
  alias tell pos

  def pos=(offset)
    raise ArgumentError, "closed stream" if @current_io.nil? || @current_io.closed?
    @current_io.pos = offset
  end

  def fileno
    raise ArgumentError, "closed stream" if @current_io.nil? || @current_io.closed?
    @current_io.fileno
  end
  alias to_i fileno

  def seek(amount, whence = (defined?(IO::SEEK_SET) ? IO::SEEK_SET : 0))
    io = ensure_io
    raise ArgumentError, "no stream to seek" unless io
    io.seek(amount, whence)
  end

  def rewind
    @lineno = 0
    $. = 0
    if @current_io && @current_io.respond_to?(:rewind)
      @current_io.rewind rescue nil
    end
    0
  end

  def eof?
    raise IOError, "closed stream" if @current_io.nil? || @current_io.closed?
    @current_io.eof?
  end
  alias eof eof?

  def closed?
    io = @current_io || @last_io
    io ? io.closed? : false
  end

  def close
    ensure_io
    if @current_io
      @last_io = @current_io
      if @current_file != "-" && !@current_io.equal?($stdin)
        @current_io.close unless @current_io.closed?
      end
    end
    self
  end

  def skip
    if @current_io
      next_file
    end
    self
  end

  def set_encoding(*args)
    io = ensure_io
    io.set_encoding(*args) if io && io.respond_to?(:set_encoding)
    self
  end

  def external_encoding
    io = ensure_io
    io.respond_to?(:external_encoding) ? io.external_encoding : nil
  end

  def internal_encoding
    io = ensure_io
    io.respond_to?(:internal_encoding) ? io.internal_encoding : nil
  end

  def gets(*args)
    line = nil
    loop do
      io = ensure_io
      break unless io
      line = io.gets(*args)
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

  def readline(*args)
    res = gets(*args)
    raise EOFError, "end of file reached" if res.nil?
    res
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
    return to_enum(:each_byte) unless block_given?
    while b = getbyte
      yield b
    end
    self
  end
  alias bytes each_byte

  def each_char
    return to_enum(:each_char) unless block_given?
    while c = getc
      yield c
    end
    self
  end
  alias chars each_char

  def each_codepoint
    return to_enum(:each_codepoint) unless block_given?
    while c = getc
      yield c.ord
    end
    self
  end
  alias codepoints each_codepoint

  def each_line(*args, &block)
    return to_enum(:each_line, *args) unless block_given?
    while line = gets(*args)
      yield line
    end
    self
  end
  alias each each_line

  def readlines(*args)
    lines = []
    while line = gets(*args)
      lines << line
    end
    lines
  end
  alias to_a readlines

  def read(length = nil, outbuf = nil)
    res = +""
    loop do
      io = ensure_io
      break unless io
      if length.nil?
        chunk = io.read
        res << chunk if chunk
        next_file
      else
        needed = length - res.bytesize
        break if needed <= 0
        chunk = io.read(needed)
        if chunk && !chunk.empty?
          res << chunk
          break if res.bytesize >= length
        else
          next_file
        end
      end
    end
    return nil if length && res.empty? && !@current_io
    outbuf.replace(res) if outbuf
    res
  end

  def readpartial(length, outbuf = nil)
    io = ensure_io
    raise EOFError, "end of file reached" unless io
    begin
      chunk = io.readpartial(length, outbuf)
      return chunk
    rescue EOFError
      next_file
      io = ensure_io
      raise EOFError, "end of file reached" unless io
      io.readpartial(length, outbuf)
    end
  end

  def read_nonblock(maxlen, *args)
    io = ensure_io
    raise EOFError, "end of file reached" unless io
    io.read_nonblock(maxlen, *args)
  end

  private

  def ensure_files
    @files ||= begin
      cur_argv = argv
      (cur_argv.nil? || cur_argv.empty?) ? ["-"] : cur_argv.dup
    end
  end

  def ensure_io
    ensure_files
    return @current_io if @current_io && !@current_io.closed?

    while !@files.empty?
      @current_file = @files.shift
      @argv.shift if @argv && @argv != ARGV && !@argv.empty?
      $FILENAME = @current_file
      if @current_file == "-"
        @current_io = $stdin
        return @current_io
      else
        begin
          @current_io = File.open(@current_file, "r")
          return @current_io
        rescue => e
          $stderr.puts "cannot open #{@current_file}: #{e.message}"
        end
      end
    end

    if @current_file.nil? && @files.empty?
      @current_file = "-"
      $FILENAME = "-"
      @current_io = $stdin
      return @current_io
    end

    nil
  end

  def next_file
    if @current_io
      @last_io = @current_io
      if @current_file != "-" && !@current_io.equal?($stdin)
        @current_io.close unless @current_io.closed?
      end
    end
    @current_io = nil
  end
end

ARGF = ARGFClass.new

module Kernel
  def gets(*args)
    ARGF.gets(*args)
  end

  def readline(*args)
    ARGF.readline(*args)
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

  def readlines(*args)
    ARGF.readlines(*args)
  end
end
