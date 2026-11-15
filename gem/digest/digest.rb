module Digest
  def self.__str(str)
    return str if str.is_a?(String)
    return str.to_str if str.respond_to?(:to_str)
    raise TypeError, "no implicit conversion of #{str.nil? ? 'nil' : str.class} into String"
  end

  def self.hexencode(str)
    __str(str).unpack1('H*')
  end

  VOWELS = "aeiouy"
  CONSONANTS = "bcdfghklmnprstvzx"

  def self.bubblebabble(str)
    str = __str(str)
    bytes = str.bytes
    seed = 1
    out = +"x"
    rounds = bytes.size / 2 + 1
    rounds.times do |i|
      if i + 1 < rounds || bytes.size.odd?
        b1 = bytes[2 * i]
        out << VOWELS[(((b1 >> 6) & 3) + seed) % 6]
        out << CONSONANTS[(b1 >> 2) & 15]
        out << VOWELS[((b1 & 3) + seed / 6) % 6]
        if i + 1 < rounds
          b2 = bytes[2 * i + 1]
          out << CONSONANTS[(b2 >> 4) & 15]
          out << "-"
          out << CONSONANTS[b2 & 15]
          seed = (seed * 5 + b1 * 7 + b2) % 36
        end
      else
        out << VOWELS[seed % 6]
        out << CONSONANTS[16]
        out << VOWELS[seed / 6]
      end
    end
    out << "x"
  end

  module Instance
    def update(str)
      raise RuntimeError, "#{self.class} does not implement update()"
    end

    alias << update

    def finish
      raise RuntimeError, "#{self.class} does not implement finish()"
    end

    def reset
      raise RuntimeError, "#{self.class} does not implement reset()"
    end

    def new
      clone.reset
    end

    def digest(str = nil)
      if str.nil?
        clone.finish
      else
        reset
        update(str)
        value = finish
        reset
        value
      end
    end

    def digest!
      value = finish
      reset
      value
    end

    def hexdigest(str = nil)
      Digest.hexencode(digest(str))
    end

    def hexdigest!
      Digest.hexencode(digest!)
    end

    def to_s
      hexdigest
    end

    def base64digest(str = nil)
      [digest(str)].pack('m0')
    end

    def base64digest!
      [digest!].pack('m0')
    end

    def bubblebabble
      Digest.bubblebabble(digest)
    end

    def inspect
      "#<#{self.class.name}: #{hexdigest}>"
    end

    def ==(other)
      if other.is_a?(Digest::Instance)
        digest == other.digest
      else
        str = (other.to_str rescue nil)
        return false unless str.is_a?(String)
        hexdigest == str
      end
    end

    def digest_length
      digest.length
    end

    def length
      digest_length
    end
    alias size length

    def block_length
      raise RuntimeError, "#{self.class} does not implement block_length()"
    end

    def file(name)
      File.open(name, "rb") do |f|
        while (buf = f.read(16384)) && !buf.empty?
          update(buf)
        end
      end
      self
    end
  end

  class Class
    include Instance

    def self.digest(str, *args)
      raise ArgumentError, "no data given" if str.nil?
      new(*args).digest(str)
    end

    def self.hexdigest(str, *args)
      Digest.hexencode(digest(str, *args))
    end

    def self.base64digest(str, *args)
      [digest(str, *args)].pack('m0')
    end

    def self.file(name, *args)
      new(*args).file(name)
    end

    def self.bubblebabble(*args)
      Digest.bubblebabble(digest(*args))
    end
  end

  class Base < Class
    # Go methods (update, finish, reset, ...) are defined on Base
    def initialize
      __setup(self.class.__algorithm)
    end

    def self.__algorithm
      raise NotImplementedError, "Digest::Base is an abstract class"
    end
  end

  %w[MD5 SHA1 SHA256 SHA384 SHA512].each do |algo|
    klass = ::Class.new(Base)
    klass.define_singleton_method(:__algorithm) { algo }
    const_set(algo, klass)
  end

  class SHA2 < Class
    def initialize(bitlen = 256)
      case bitlen
      when 256 then @sha = Digest::SHA256.new
      when 384 then @sha = Digest::SHA384.new
      when 512 then @sha = Digest::SHA512.new
      else raise ArgumentError, "unsupported bit length: %s" % bitlen.inspect
      end
      @bitlen = bitlen
    end

    def reset
      @sha.reset
      self
    end

    def update(str)
      @sha.update(str)
      self
    end
    alias << update

    def finish
      @sha.digest!
    end

    def block_length
      @sha.block_length
    end

    def digest_length
      @sha.digest_length
    end

    def initialize_copy(other)
      @sha = other.instance_variable_get(:@sha).clone
    end

    def inspect
      "#<%s:%d %s>" % [self.class.name, @bitlen, hexdigest]
    end
  end
end

def Digest(name)
  const = name.to_sym
  if Digest.const_defined?(const)
    Digest.const_get(const)
  else
    raise LoadError, "library not found for class Digest::#{name} -- digest/#{name.to_s.downcase}"
  end
end
