# MRI-compatible MT19937 Random implementation

class Random
  def self.urandom(size)
    n = Kernel.__to_int(size)
    raise ArgumentError, "negative string size (or size too big)" if n < 0
    __urandom(n).force_encoding(Encoding::BINARY)
  end

  def self.new_seed
    b = __urandom(16)
    val = 0
    i = 15
    while i >= 0
      val = (val << 8) | b.getbyte(i)
      i -= 1
    end
    val
  end

  def self.__coerce_seed(s)
    case s
    when Integer
      s
    when Float
      s.to_i
    when Rational
      s.to_i
    when Complex
      raise RangeError, "can't convert #{s} into Integer" unless s.imag == 0 && !(Float === s.imag)
      s.real.to_i
    else
      Kernel.__to_int(s)
    end
  end

  def self.__seed_words(n)
    a = n.abs
    return [0] if a == 0
    words = []
    while a > 0
      words << (a & 0xffffffff)
      a >>= 32
    end
    words
  end

  def initialize(seed = (no_seed = true; nil))
    s = no_seed ? Random.new_seed : Random.__coerce_seed(seed)
    @__seed = s
    @__mt = Random::MTState.new.seed_words(Random.__seed_words(s))
  end

  def initialize_copy(other)
    raise TypeError, "wrong argument type #{other.class} (expected Random)" unless Random === other
    @__seed = other.seed
    @__mt = Random::MTState.new.copy_from(other.instance_variable_get(:@__mt))
    self
  end

  def seed
    @__seed
  end

  private def state
    b = @__mt.state_bytes
    val = 0
    i = b.bytesize - 1
    while i >= 0
      val = (val << 8) | b.getbyte(i)
      i -= 1
    end
    val
  end

  private def left
    @__mt.left
  end

  def ==(other)
    return false unless Random === other
    seed == other.seed &&
      @__mt.left == other.instance_variable_get(:@__mt).left &&
      @__mt.state_bytes == other.instance_variable_get(:@__mt).state_bytes
  end

  def bytes(size)
    n = Kernel.__to_int(size)
    raise ArgumentError, "negative string size (or size too big)" if n < 0
    @__mt.bytes(n).force_encoding(Encoding::BINARY)
  end

  private def __rand_int(limit)
    n = limit - 1
    return 0 if n == 0
    # MRI's limited_rand in Go (random.go)
    return @__mt.limited(n) if n < 0x7fff_ffff_ffff_ffff
    bl = n.bit_length
    if bl <= 32
      mask = (1 << bl) - 1
      loop do
        v = @__mt.int32 & mask
        return v if v <= n
      end
    else
      words = (bl + 31) / 32
      top_bits = bl % 32
      top_mask = top_bits == 0 ? 0xffffffff : (1 << top_bits) - 1
      loop do
        v = 0
        i = words - 1
        while i >= 0
          w = @__mt.int32
          w &= top_mask if i == words - 1
          v = (v << 32) | w
          i -= 1
        end
        return v if v <= n
      end
    end
  end

  def rand(max = (no_max = true; nil))
    return @__mt.real if no_max
    case max
    when Integer
      raise ArgumentError, "invalid argument - #{max}" if max <= 0
      __rand_int(max)
    when Float
      raise ArgumentError, "invalid argument - #{max}" if max <= 0.0 || max.nan? || max.infinite?
      @__mt.real * max
    when Range
      b = max.begin
      e = max.end
      raise ArgumentError, "invalid argument - #{max}" if b.nil? || e.nil?
      if Integer === b && Integer === e
        limit = max.exclude_end? ? (e - b) : (e - b + 1)
        return nil if limit <= 0
        return b + __rand_int(limit)
      elsif Numeric === b && Numeric === e
        bf = b.to_f
        ef = e.to_f
        raise ArgumentError, "invalid argument - #{max}" if bf.nan? || ef.nan? || bf.infinite? || ef.infinite?
        diff = ef - bf
        return nil if diff < 0.0 || (diff == 0.0 && max.exclude_end?)
        return bf if diff == 0.0
        bf + @__mt.real * diff
      else
        begin
          diff = e - b
        rescue NoMethodError, TypeError
          raise ArgumentError, "invalid argument - #{max}"
        end
        unless Numeric === diff
          if diff.respond_to?(:to_int)
            diff = diff.to_int
          elsif diff.respond_to?(:to_f)
            diff = diff.to_f
          end
        end
        raise ArgumentError, "invalid argument - #{max}" unless Numeric === diff
        if Integer === diff
          limit = max.exclude_end? ? diff : diff + 1
          return nil if limit <= 0
          val = __rand_int(limit)
        else
          df = diff.to_f
          return nil if df < 0.0 || (df == 0.0 && max.exclude_end?)
          val = df == 0.0 ? 0.0 : @__mt.real * df
        end
        begin
          b + val
        rescue NoMethodError, TypeError
          raise ArgumentError, "invalid argument - #{max}"
        end
      end
    when nil
      raise ArgumentError, "invalid argument - nil"
    else
      n = Kernel.__to_int(max)
      raise ArgumentError, "invalid argument - #{max}" if n <= 0
      __rand_int(n)
    end
  end

  private_constant :MTState
  remove_const(:DEFAULT) if const_defined?(:DEFAULT, false)
  @__default = begin
    b = __boot_seed
    val = 0
    i = 15
    while i >= 0
      val = (val << 8) | b.getbyte(i)
      i -= 1
    end
    Random.new(val)
  end

  class << self
    def default
      @__default
    end

    def seed
      @__default.seed
    end

    def srand(seed_val = (no_seed = true; nil))
      old = @__default.seed
      s = no_seed ? self.new_seed : __coerce_seed(seed_val)
      @__default.instance_variable_set(:@__seed, s)
      # reseeded in place: random.go keeps the default generator
      @__default.instance_variable_get(:@__mt).seed_words(__seed_words(s))
      old
    end

    def rand(max = (no_max = true; nil))
      no_max ? @__default.rand : @__default.rand(max)
    end

    def bytes(size)
      @__default.bytes(size)
    end
  end
end

module Kernel
  def srand(new_seed = (no_seed = true; nil))
    no_seed ? Random.srand : Random.srand(new_seed)
  end
  module_function :srand

  # Kernel#rand is random.go's for no argument, nil and a Fixnum; the rest
  def __oruby_rand_slow(max = (no_max = true; nil))
    return Random.rand if no_max
    if max.nil? || (Numeric === max && !(Range === max) && max == 0)
      return Random.rand
    end
    if Integer === max && max < 0
      return Random.rand(-max)
    elsif Float === max
      raise ArgumentError, "invalid argument - #{max}" if max.nan? || max.infinite?
      n = max.abs.to_i
      return n == 0 ? Random.rand : Random.rand(n)
    elsif !(Range === max) && !(Integer === max)
      n = Kernel.__to_int(max)
      return n == 0 ? Random.rand : Random.rand(n.abs)
    end
    Random.rand(max)
  end
  module_function :__oruby_rand_slow
end
