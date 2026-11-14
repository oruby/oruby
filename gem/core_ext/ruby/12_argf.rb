class ARGFClass
  include Enumerable

  attr_accessor :lineno

  def initialize
    @lineno = 0
    @current_io = nil
    @current_file = nil
    @files = nil
  end

  def path
    filename
  end

  def filename
    ensure_files
    if @current_file
      @current_file
    elsif !@files.empty?
      @files.first
    else
      "-"
    end
  end

  def argv
    ARGV
  end

  def to_a
    entries
  end

  def closed?
    @current_io ? @current_io.closed? : false
  end

  def close
    if @current_io && @current_file != "-" && !@current_io.equal?($stdin)
      @current_io.close unless @current_io.closed?
    end
    self
  end

  def to_io
    ensure_io
    @current_io
  end

  def rewind
    @lineno = 0
    $. = 0
    if @current_io && @current_io.respond_to?(:rewind)
      @current_io.rewind rescue nil
    end
    0
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

  def each_line(*args, &block)
    return to_enum(:each_line, *args) unless block_given?
    while line = gets(*args)
      yield line
    end
    self
  end
  alias each each_line

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
    @files ||= (ARGV.empty? ? ["-"] : ARGV.dup)
  end

  def ensure_io
    ensure_files
    return @current_io if @current_io && !@current_io.closed?

    while !@files.empty?
      @current_file = @files.shift
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
    if @current_io && @current_file != "-" && !@current_io.equal?($stdin)
      @current_io.close unless @current_io.closed?
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
end
