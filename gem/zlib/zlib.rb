module Zlib
  VERSION = "3.2.1"
  ZLIB_VERSION = "1.3.1"

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
    return v if v.is_a?(String)
    return v.to_str if v.respond_to?(:to_str)
    raise TypeError, "no implicit conversion of #{v.nil? ? 'nil' : v.class} into String"
  end

  def self.adler32(str = nil, adler = 1)
    return adler if str.nil?
    __adler32(__str(str), adler)
  end

  def self.crc32(str = nil, crc = 0)
    return crc if str.nil?
    __crc32(__str(str), crc)
  end

  class ZStream
    def initialize
      raise NotImplementedError, "use Zlib::Deflate or Zlib::Inflate"
    end

    def total_in
      __state[0]
    end

    def total_out
      __state[1]
    end

    def finished?
      __state[2]
    end
    alias stream_end? finished?

    def closed?
      @closed ? true : false
    end
    alias ended? closed?

    def close
      raise Zlib::Error, "stream is not ready" if @closed
      @closed = true
      nil
    end
    alias end close

    def reset
      __reset
      @pending = "".b
      nil
    end

    def adler
      __state[4]
    end

    def data_type
      Zlib::BINARY
    end

    def avail_in
      0
    end

    def avail_out
      0
    end

    def avail_out=(size)
      size
    end

    def flush_next_in
      "".b
    end

    def flush_next_out
      out = __collect
      out
    end

    def finish
      __check_open
      __finish
    end

    def __check_open
      raise Zlib::Error, "stream is not ready" if @closed
    end
  end

  class Deflate < ZStream
    def self.deflate(src, level = DEFAULT_COMPRESSION, &block)
      z = new(level)
      begin
        out = z.deflate(src, FINISH)
        if block
          out.bytes.each_slice(16384) { |chunk| block.call(chunk.pack("C*")) }
          nil
        else
          out
        end
      ensure
        z.close
      end
    end

    def initialize(level = nil, wbits = nil, memlevel = nil, strategy = nil)
      level ||= DEFAULT_COMPRESSION
      wbits ||= MAX_WBITS
      __deflate_init(level, wbits, memlevel || DEF_MEM_LEVEL, strategy || DEFAULT_STRATEGY)
      @level = level
      @wbits = wbits
    end

    def initialize_copy(other)
      raise NotImplementedError, "Zlib::Deflate#dup is not supported"
    end

    def deflate(src, flush = NO_FLUSH, &block)
      __check_open
      __write(Zlib.__str(src)) unless src.nil?
      __flush(src.nil? ? FINISH : flush)
      out = __take
      if block
        block.call(out) unless out.empty?
        return nil
      end
      out
    end

    def <<(src)
      __check_open
      __write(Zlib.__str(src))
      self
    end

    def flush(flush = SYNC_FLUSH, &block)
      __check_open
      __flush(flush) unless flush == NO_FLUSH
      out = __take
      if block
        block.call(out) unless out.empty?
        return nil
      end
      out
    end

    def params(level, strategy)
      __flush(SYNC_FLUSH)
      @level = level
      nil
    end

    def set_dictionary(dict)
      __set_dictionary(Zlib.__str(dict))
      dict
    end

    def __finish
      __flush(FINISH)
      __take
    end

    def __collect
      __take
    end
  end

  class Inflate < ZStream
    def self.inflate(src)
      z = new
      begin
        out = z.inflate(src)
        out << z.finish
        out
      ensure
        z.close
      end
    end

    def initialize(wbits = MAX_WBITS)
      __inflate_init(wbits)
      @pending = "".b
    end

    def inflate(src, buffer: nil, &block)
      __check_open
      out = src.nil? ? "".b : __write(Zlib.__str(src))
      out = @pending + out unless @pending.empty?
      @pending = "".b
      if src.nil?
        u = __state[3]
        out << u if u && finished?
        __clear_unused if u
      end
      if buffer
        buffer.replace(out)
        out = buffer
      end
      if block
        block.call(out) unless out.empty?
        return nil
      end
      out
    end

    def <<(src)
      __check_open
      if src.nil?
        @pending << inflate(nil)
      else
        @pending << __write(Zlib.__str(src))
      end
      self
    end

    def add_dictionary(dict)
      __set_dictionary(Zlib.__str(dict))
      self
    end

    def set_dictionary(dict)
      __set_dictionary(Zlib.__str(dict))
      dict
    end

    def sync(str)
      false
    end

    def sync_point?
      false
    end

    def unused
      __state[3]
    end

    def __clear_unused
      @unused_cleared = true
    end

    def __finish
      out = @pending
      @pending = "".b
      u = __state[3]
      if u && !@unused_cleared
        out << u
        @unused_cleared = true
      end
      out
    end

    def __collect
      __finish
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
      raw = io.read
      raw = "".b if raw.nil?
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
      @buf.ungetc(c)
      nil
    end

    def ungetbyte(b)
      __check_closed
      @buf.ungetbyte(b)
      nil
    end

    def eof?
      __check_closed
      @buf.eof?
    end
    alias eof eof?

    def pos
      @buf.pos
    end
    alias tell pos

    def rewind
      __check_closed
      @buf.rewind
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
