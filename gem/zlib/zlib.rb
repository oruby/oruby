module Zlib
  VERSION = "3.2.1"

  NO_COMPRESSION = 0
  BEST_SPEED = 1
  BEST_COMPRESSION = 9
  DEFAULT_COMPRESSION = -1

  FILTERED = 1
  HUFFMAN_ONLY = 2
  RLE = 3
  FIXED = 4
  DEFAULT_STRATEGY = 0

  MAX_WBITS = 15
  DEF_MEM_LEVEL = 8
  MAX_MEM_LEVEL = 9

  NO_FLUSH = 0
  PARTIAL_FLUSH = 1
  SYNC_FLUSH = 2
  FULL_FLUSH = 3
  FINISH = 4
  BLOCK = 5

  BINARY = 0
  ASCII = 1
  TEXT = 1
  UNKNOWN = 2

  OS_MSDOS = 0x00
  OS_AMIGA = 0x01
  OS_VMS = 0x02
  OS_UNIX = 0x03
  OS_VMCMS = 0x04
  OS_ATARI = 0x05
  OS_OS2 = 0x06
  OS_MACOS = 0x07
  OS_ZSYSTEM = 0x08
  OS_CPM = 0x09
  OS_TOPS20 = 0x0a
  OS_WIN32 = 0x0b
  OS_QDOS = 0x0c
  OS_RISCOS = 0x0d
  OS_UNKNOWN = 0xff
  OS_CODE = OS_UNIX

  class Error < StandardError; end
  class StreamEnd < Error; end
  class NeedDict < Error; end
  class DataError < Error; end
  class StreamError < Error; end
  class MemError < Error; end
  class BufError < Error; end
  class VersionError < Error; end
  class InProgressError < Error; end

  def self.__sio(str)
    require "stringio" unless Object.const_defined?(:StringIO)
    StringIO.new(str)
  end

  def self.zlib_version
    ZLIB_VERSION
  end

  def self.__str(v)
    return v if String === v
    Kernel.__implicit(v, String)
  end

  def self.__int(v, default = nil)
    return default if v.nil? && !default.nil?
    v = Kernel.__to_int(v) unless Integer === v
    raise RangeError, "integer #{v} too big to convert to 'int'" unless (-2**31...2**31).cover?(v)
    v
  end

  def self.adler32(str = nil, adler = nil)
    seed = adler.nil? ? 1 : __ulong(adler)
    str.nil? ? 1 : __adler32(__str(str), seed)
  end

  def self.crc32(str = nil, crc = nil)
    seed = crc.nil? ? 0 : __ulong(crc)
    str.nil? ? 0 : __crc32(__str(str), seed)
  end

  def self.adler32_combine(first, second, length)
    __adler32_combine(__ulong(first), __ulong(second), Kernel.__to_int(length))
  end

  def self.crc32_combine(first, second, length)
    __crc32_combine(__ulong(first), __ulong(second), Kernel.__to_int(length))
  end

  # NUM2ULONG: a negative value wraps, one past unsigned long is a
  # RangeError; the checksums use its low 32 bits
  def self.__ulong(v)
    v = Kernel.__to_int(v) unless Integer === v
    if v >= 2**64 || v < -2**63
      raise RangeError, "integer #{v} too big to convert to 'unsigned long'"
    end
    v & 0xFFFFFFFF
  end

  class ZStream
    CHUNK = 16384
    private_constant :CHUNK

    def initialize
      raise NotImplementedError, "use Zlib::Deflate or Zlib::Inflate"
    end

    def total_in
      __check_open
      __state[0]
    end

    def total_out
      __check_open
      __state[1]
    end

    def finished?
      __check_open
      __state[2]
    end
    alias stream_end? finished?

    def closed?
      @closed ? true : false
    end
    alias ended? closed?

    def close
      __check_open
      __close
      @pending = "".b
      @closed = true
      nil
    end
    alias end close

    def reset
      __check_open
      __reset
      @pending = "".b
      nil
    end

    def adler
      __check_open
      __state[4]
    end

    def data_type
      __check_open
      __state[5]
    end

    def avail_in
      @closed ? 0 : __state[3]
    end

    def avail_out
      @closed ? 0 : __state[6]
    end

    def avail_out=(size)
      __check_open
      size = Kernel.__to_int(size) unless Integer === size
      unless (-2**31...2**32).cover?(size)
        raise RangeError, "integer #{size} too big to convert to 'unsigned int'"
      end
      __set_avail_out(size & 0xFFFFFFFF)
      size
    end

    def flush_next_in
      @closed ? "".b : __take_unused
    end

    def flush_next_out
      return "".b if @closed
      @pending << __take
      __drain
    end

    def __check_open
      raise Zlib::Error, "stream is not ready" if @closed
    end

    private

    def __drain
      out = @pending
      @pending = "".b
      __set_avail_out(0)
      out
    end

    def __hand_out(buffer, block)
      return __with_buffer(__drain, buffer) unless block

      data = @pending
      offset = 0
      complete = finished?
      begin
        while offset < data.bytesize
          size = [CHUNK, data.bytesize - offset].min
          break if size < CHUNK && !complete
          chunk = __with_buffer(data.byteslice(offset, size), buffer)
          offset += size
          block.call(chunk)
        end
      ensure
        @pending = data.byteslice(offset, data.bytesize - offset) if offset > 0
      end
      __with_buffer(@pending, buffer) if offset == 0 && buffer
      nil
    end

    def __with_buffer(out, buffer)
      return out unless buffer
      encoding = buffer.encoding
      buffer.replace(out)
      buffer.force_encoding(encoding)
    end
  end

  class Deflate < ZStream
    def self.deflate(src, level = DEFAULT_COMPRESSION, &block)
      z = new(level)
      begin
        z.deflate(Zlib.__str(src), FINISH, &block)
      ensure
        z.close
      end
    end

    def initialize(level = nil, wbits = nil, memlevel = nil, strategy = nil)
      __deflate_init(Zlib.__int(level, DEFAULT_COMPRESSION),
                     Zlib.__int(wbits, MAX_WBITS),
                     Zlib.__int(memlevel, DEF_MEM_LEVEL),
                     Zlib.__int(strategy, DEFAULT_STRATEGY))
      @pending = "".b
    end

    def initialize_copy(other)
      __copy_deflate(other)
      @pending = @pending.dup
      self
    end

    def deflate(src, flush = NO_FLUSH, &block)
      __check_open
      flush = __flush_mode(flush, NO_FLUSH)
      if src.nil?
        __write("".b, FINISH) unless finished?
      else
        __write(Zlib.__str(src), flush)
      end
      @pending << __take
      __hand_out(nil, block)
    end

    def <<(src)
      __check_open
      if src.nil?
        __write("".b, FINISH) unless finished?
      else
        __write(Zlib.__str(src), NO_FLUSH)
      end
      self
    end

    def flush(flush = SYNC_FLUSH, &block)
      __check_open
      mode = __flush_mode(flush, SYNC_FLUSH)
      __write("".b, mode) unless mode == NO_FLUSH || finished?
      @pending << __take
      __hand_out(nil, block)
    end

    def params(level, strategy)
      __check_open
      __params(Zlib.__int(level), Zlib.__int(strategy))
    end

    def set_dictionary(dict)
      __check_open
      str = Zlib.__str(dict)
      __set_dictionary(str)
      str
    end

    def finish(&block)
      flush(FINISH, &block)
    end

    private

    def __flush_mode(value, default)
      mode = Zlib.__int(value, default)
      raise Zlib::StreamError, "stream error" unless (NO_FLUSH..BLOCK).cover?(mode)
      mode
    end
  end

  # MRI's inflate stream: what is inflated waits in a buffer that inflate
  # and finish hand out (<< does not); once the stream has ended, input
  # passes through to that buffer, the input left over after the end first
  # (after the call that ended it). With a block the buffer goes out in
  # 16 KB pieces, and a break leaves the rest for the next call.
  class Inflate < ZStream
    def self.inflate(src)
      z = new
      begin
        z.__send__(:__run, Zlib.__str(src))
        z.__send__(:__run_finish)
        z.__send__(:__drain)
      ensure
        z.close
      end
    end

    def initialize(wbits = nil)
      __inflate_init(Zlib.__int(wbits, MAX_WBITS))
      @pending = "".b
      @dictionaries = {}
    end

    def inflate(src, buffer: nil, &block)
      __check_open
      unless buffer.nil?
        buffer = Zlib.__str(buffer)
        raise FrozenError, "can't modify frozen String" if buffer.frozen?
      end
      if finished?
        unless src.nil?
          str = Zlib.__str(src)
          @pending << str.b unless buffer
          return __with_buffer("".b, buffer)
        end
      else
        __run(src.nil? ? nil : Zlib.__str(src))
      end
      begin
        __hand_out(buffer, block)
      ensure
        __take_leftover
      end
    end

    def <<(src)
      __check_open
      if finished?
        @pending << Zlib.__str(src).b unless src.nil?
      else
        __run(src.nil? ? nil : Zlib.__str(src))
        __take_leftover
      end
      self
    end

    def finish(&block)
      __check_open
      __run_finish unless finished?
      __hand_out(nil, block)
    end

    def add_dictionary(dict)
      __check_open
      str = Zlib.__str(dict).b
      @dictionaries[Zlib.adler32(str)] = str
      self
    end

    def set_dictionary(dict)
      __check_open
      str = Zlib.__str(dict)
      __set_dictionary(str)
      str
    end

    def sync(str)
      __check_open
      __sync(Zlib.__str(str))
    end

    def sync_point?
      __check_open
      __sync_point
    end

    private

    # inflates src (nil: what input there is): a call that makes no
    # progress on an unfinished stream is zlib's Z_BUF_ERROR
    def __run(src)
      __inflate_bytes(src.nil? ? "".b : src, src.nil? ? FINISH : NO_FLUSH)
      out = __take
      raise Zlib::BufError, "buffer error" if src.nil? && out.empty? && !finished?
      @pending << out
    end

    # Z_FINISH: the stream must end with the input it has
    def __run_finish
      unless finished?
        __inflate_bytes("".b, FINISH)
        @pending << __take
      end
      raise Zlib::BufError, "buffer error" unless finished?
    end

    def __inflate_bytes(src, mode)
      __write(src, mode)
    rescue Zlib::NeedDict
      dict = @dictionaries[adler]
      raise unless dict
      __set_dictionary(dict)
      __write("".b, mode)
    end

    def __take_leftover
      return unless finished?
      @pending << __take_unused
    end
  end

  def self.deflate(src, level = DEFAULT_COMPRESSION)
    Deflate.deflate(src, level)
  end

  def self.inflate(src)
    Inflate.inflate(src)
  end

  # -- gzip -------------------------------------------------------------------

  class GzipFile
    class Error < Zlib::Error
      attr_reader :input

      def inspect
        s = super
        @input ? s.sub(/>\z/, ", input=#{@input.inspect}>") : s
      end
    end
    class NoFooter < Error; end
    class CRCError < Error; end
    class LengthError < Error; end

    def self.wrap(*args, **kw)
      gz = new(*args, **kw)
      return gz unless block_given?
      begin
        yield gz
      ensure
        gz.close unless gz.closed?
      end
    end

    def to_io
      @io
    end

    def closed?
      @closed ? true : false
    end

    def sync
      @sync ? true : false
    end

    def sync=(v)
      @sync = v
    end

    def level
      @level || DEFAULT_COMPRESSION
    end

    def os_code
      @os_code || OS_CODE
    end

    def mtime
      Time.at(@mtime || 0)
    end

    def orig_name
      raise GzipFile::Error, "closed gzip stream" if @closed
      @orig_name
    end

    def comment
      raise GzipFile::Error, "closed gzip stream" if @closed
      @comment
    end

    def crc
      Zlib.crc32(@__data || "")
    end

    def __check_closed
      raise GzipFile::Error, "closed gzip stream" if @closed
    end
  end

  class GzipWriter < GzipFile
    def self.open(filename, level = nil, strategy = nil, **opts, &block)
      io = File.open(filename, "wb")
      wrap(io, level, strategy, **opts, &block)
    end

    def initialize(io, level = nil, strategy = nil, **opts)
      @io = io
      @level = level || DEFAULT_COMPRESSION
      @z = Zlib::Deflate.new(@level, MAX_WBITS + 16, DEF_MEM_LEVEL, strategy || DEFAULT_STRATEGY)
      @started = false
      @mtime = nil
      @pos = 0
      @__data = nil
    end

    def mtime=(t)
      raise GzipFile::Error, "header is already written" if @started
      @mtime = t.to_i
    end

    def orig_name=(name)
      raise GzipFile::Error, "header is already written" if @started
      @orig_name = name.to_s
    end

    def comment=(str)
      raise GzipFile::Error, "header is already written" if @started
      @comment = str.to_s
    end

    def __start
      return if @started
      @z.__set_header(@orig_name, @comment, @mtime || 0)
      @started = true
    end

    def write(*strs)
      __check_closed
      __start
      n = 0
      strs.each do |s|
        s = s.to_s
        @z << s
        n += s.bytesize
        @pos += s.bytesize
      end
      out = @z.flush(Zlib::NO_FLUSH)
      @io.write(out) unless out.empty?
      n
    end

    def <<(str)
      write(str)
      self
    end

    def print(*args)
      args = [$_] if args.empty?
      write(args.join(($, || "").to_s) + ($\ || "").to_s)
      nil
    end

    def puts(*args)
      if args.empty?
        write("\n")
        return nil
      end
      args.flatten.each do |a|
        s = a.to_s
        write(s.end_with?("\n") ? s : s + "\n")
      end
      nil
    end

    def printf(*args)
      write(format(*args))
      nil
    end

    def putc(c)
      write(c.is_a?(Integer) ? (c & 0xff).chr : c.to_s[0])
      c
    end

    def flush(flush = SYNC_FLUSH)
      __check_closed
      __start
      out = @z.flush(flush)
      @io.write(out) unless out.empty?
      @io.flush if @io.respond_to?(:flush)
      self
    end

    def pos
      @pos
    end
    alias tell pos

    def finish
      __check_closed
      __start
      out = @z.finish
      @io.write(out) unless out.empty?
      @closed = true
      @io
    end

    def close
      io = finish
      io.close if io.respond_to?(:close)
      io
    end
  end

  class GzipReader < GzipFile
    include Enumerable

    def self.open(filename, **opts, &block)
      io = File.open(filename, "rb")
      wrap(io, **opts, &block)
    end

    def self.zcat(io, **opts, &block)
      out = +""
      unused = nil
      loop do
        gz = new(io, **opts)
        if block
          gz.each { |line| block.call(line) }
        else
          out << gz.read
        end
        unused = gz.unused
        gz.finish
        break if unused.nil?
        io = Zlib.__sio(unused)
      end
      block ? nil : out
    end

    def initialize(io, external_encoding: nil, internal_encoding: nil, encoding: nil, **opts)
      @io = io
      @io_start = (io.pos rescue nil) if io.respond_to?(:pos)
      # read in pieces, as MRI's gzfile_read_more
      raw = "".b
      while (chunk = io.read(2048))
        break if chunk.empty?
        raw << chunk
      end
      data, @orig_name, @comment, @mtime, @os_code, @unused, @xfl = Zlib.__gunzip(raw)
      @level = case @xfl
               when 2 then BEST_COMPRESSION
               when 4 then BEST_SPEED
               else DEFAULT_COMPRESSION
               end
      @__data = data
      enc = external_encoding || encoding
      enc = Encoding.find(enc.to_s.split(":").first) if enc && !enc.is_a?(Encoding)
      @external_encoding = enc || Encoding.default_external
      @buf = Zlib.__sio(data.dup.force_encoding(@external_encoding))
      @lineno = 0
      @pos_adj = 0  # bytes pushed back before the start: pos goes negative
    end

    attr_reader :unused
    attr_accessor :lineno

    def external_encoding
      @external_encoding
    end

    def read(len = nil, outbuf = nil)
      __check_closed
      raise ArgumentError, "negative length #{len} given" if len && len < 0
      if len
        s = @buf.read(len)
        s&.force_encoding(Encoding::BINARY)
      else
        s = @buf.read
      end
      if outbuf
        outbuf.replace(s || "")
        return s.nil? ? nil : outbuf
      end
      s
    end

    def readpartial(maxlen, outbuf = nil)
      __check_closed
      raise ArgumentError, "negative length #{maxlen} given" if maxlen < 0
      s = @buf.read(maxlen)
      raise EOFError, "end of file reached" if s.nil? && maxlen > 0
      s = s.to_s.force_encoding(Encoding::BINARY)
      if outbuf
        outbuf.replace(s)
        return outbuf
      end
      s
    end

    def gets(*args, **kw)
      __check_closed
      line = @buf.gets(*args, **kw)
      # paragraph mode: StringIO keeps the whole run of newlines after a
      # paragraph, zlib's gzfile_getline keeps two and skips the rest
      if line && "" == args[0] && line.end_with?("\n\n\n")
        line = line.sub(/\n+\z/, "\n\n")
      end
      @lineno += 1 if line
      $_ = line
      line
    end

    def readline(*args, **kw)
      line = gets(*args, **kw)
      raise EOFError, "end of file reached" if line.nil?
      line
    end

    def each(*args, **kw)
      return to_enum(:each, *args, **kw) unless block_given?
      while (line = gets(*args, **kw))
        yield line
      end
      self
    end
    alias each_line each

    def readlines(*args, **kw)
      each(*args, **kw).to_a
    end

    def getc
      __check_closed
      @buf.getc
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

    def getbyte
      __check_closed
      @buf.getbyte
    end

    def readbyte
      b = getbyte
      raise EOFError, "end of file reached" if b.nil?
      b
    end

    def each_byte
      return to_enum(:each_byte) unless block_given?
      while (b = getbyte)
        yield b
      end
      self
    end

    def ungetc(c)
      __check_closed
      n = Integer === c ? c.chr(@external_encoding).bytesize : c.to_s.bytesize
      __unget_adjust(n)
      @buf.ungetc(c)
      nil
    end

    def ungetbyte(b)
      __check_closed
      __unget_adjust(Integer === b ? 1 : b.to_s.bytesize) unless b.nil?
      @buf.ungetbyte(b)
      nil
    end

    private def __unget_adjust(n)
      before = @buf.pos
      @pos_adj += n - before if n > before
    end

    def eof?
      __check_closed
      @buf.eof?
    end
    alias eof eof?

    def pos
      @buf.pos - @pos_adj
    end
    alias tell pos

    def rewind
      __check_closed
      # MRI seeks the IO back to where the gzip data started
      @io.seek(@io_start || 0, IO::SEEK_SET) if @io.respond_to?(:seek)
      @buf = Zlib.__sio(@__data.dup.force_encoding(@external_encoding))
      @pos_adj = 0
      @lineno = 0
      0
    end

    def finish
      __check_closed
      @closed = true
      @io
    end

    def close
      io = finish
      io.close if io.respond_to?(:close)
      io
    end
  end

  def self.gzip(src, level: nil, strategy: nil)
    sio = __sio("".b)
    gz = GzipWriter.new(sio, level, strategy)
    gz.write(src)
    gz.finish
    sio.string
  end

  def self.gunzip(src)
    data, = __gunzip(__str(src))
    data
  end
end

module Zlib
  # MRI's checksum functions are module functions: private instance methods
  # too, for code that includes Zlib
  %i[adler32 adler32_combine crc32 crc32_combine zlib_version].each do |name|
    define_method(name) { |*args| Zlib.__send__(name, *args) }
    private name
  end
end
