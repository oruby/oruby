# The Ruby half of zstd-ruby's zstdruby.so (the C extension in MRI): argument
# handling and the IO-like writers; the codec is Go (zstd.go)
module Zstd
  def self.__str(v)
    return v if String === v
    if !v.nil? && v.respond_to?(:to_str)
      s = v.to_str
      return s if String === s
    end
    name = v.nil? ? "nil" : v == true ? "true" : v == false ? "false" : v.class
    raise TypeError, "no implicit conversion of #{name} into String"
  end

  def self.__level(level)
    return 3 if level.nil?
    raise TypeError, "compression level must be an Integer" unless Integer === level
    level
  end

  # [level, dictionary bytes or nil] for compress(level:, dict:)
  def self.__cparams(level, dict)
    level = __level(level)
    case dict
    when nil then [level, nil]
    when CDict then [dict.__level, dict.__dict]
    when String then [level, dict]
    else raise ArgumentError, "`dict:` must be a Zstd::CDict or a String"
    end
  end

  def self.__dparams(dict)
    case dict
    when nil then nil
    when DDict then dict.__dict
    when String then dict
    else raise ArgumentError, "`dict:` must be a Zstd::DDict or a String"
    end
  end

  def self.compress(src, level: nil, dict: nil)
    src = __str(src)
    __compress(src, *__cparams(level, dict))
  end

  def self.decompress(src, dict: nil)
    src = __str(src)
    __decompress(src, __dparams(dict))
  end

  def self.write_skippable_frame(src, skip, magic_variant: 0)
    __str(src)
    __write_skippable(__str(skip), Integer(magic_variant))
  end

  def self.read_skippable_frame(src)
    __read_skippable(__str(src))
  end

  class << self
    private :__compress, :__decompress, :__write_skippable, :__read_skippable
  end

  class CDict
    def initialize(dict, level = nil)
      @level = Zstd.__level(level)
      @dict = Zstd.__str(dict).b.freeze
    end
    private :initialize

    def initialize_copy(_)
      raise RuntimeError, "CDict cannot be duplicated"
    end

    def __level
      @level
    end

    def __dict
      @dict
    end
  end

  class DDict
    def initialize(dict)
      @dict = Zstd.__str(dict).b.freeze
    end
    private :initialize

    def initialize_copy(_)
      raise RuntimeError, "CDict cannot be duplicated"
    end

    def __dict
      @dict
    end
  end

  class StreamingCompress
    CONTINUE = 0
    FLUSH = 1
    const_set(:END, 2) # ZSTD_e_end (END is a keyword)

    def initialize(level: nil, dict: nil)
      @dict = dict
      __init(*Zstd.__cparams(level, dict))
      @pending = "".b
    end

    def compress(src)
      __write(Zstd.__str(src))
    end

    def write(*data)
      total = 0
      data.each do |s|
        s = Zstd.__str(s)
        @pending << __write(s)
        total += s.bytesize
      end
      total
    end

    def <<(src)
      write(src)
      self
    end

    def print(*args)
      args = [$_] if args.empty?
      write(*args.map(&:to_s), *($\ ? [$\] : []))
      nil
    end

    def puts(*args)
      return (write("\n"); nil) if args.empty?
      args.flatten.each do |a|
        s = a.nil? ? "" : a.to_s
        write(s.end_with?("\n") ? s : "#{s}\n")
      end
      nil
    end

    def printf(*args)
      write(format(*args))
      nil
    end

    def flush
      out = @pending + __flush
      @pending = "".b
      out
    end

    def finish
      out = @pending + __finish
      @pending = "".b
      out
    end
  end

  class StreamingDecompress
    def initialize(dict: nil)
      @dict = dict
      __init(Zstd.__dparams(dict))
    end

    def decompress(src)
      __feed(Zstd.__str(src))
    end

    def decompress_with_pos(src)
      src = Zstd.__str(src)
      [__feed(src), src.bytesize]
    end
  end
end
