require "singleton"
require "forwardable"

class Integer
  def self.from_prime_division(pd)
    Prime.int_from_prime_division(pd)
  end

  def prime_division(generator = Prime::Generator23.new)
    Prime.prime_division(self, generator)
  end

  def prime?
    return self >= 2 if self <= 3
    return false if self % 2 == 0 || self % 3 == 0
    return Prime.__probably_prime(self) if self > 0xffffffff
    i = 5
    while i * i <= self
      return false if self % i == 0 || self % (i + 2) == 0
      i += 6
    end
    true
  end

  def self.each_prime(ubound, &block)
    Prime.each(ubound, &block)
  end
end

class Prime
  VERSION = "0.1.3"

  include Enumerable
  include Singleton

  class << self
    extend Forwardable
    include Enumerable

    def method_added(method)
      (class << self; self; end).def_delegator :instance, method
    end
  end

  def self.each(ubound = nil, generator = EratosthenesGenerator.new, &block)
    instance.each(ubound, generator, &block)
  end

  def self.prime?(value, generator = Prime::Generator23.new)
    instance.prime?(value, generator)
  end

  def self.include?(obj)
    instance.include?(obj)
  end

  def self.int_from_prime_division(pd)
    instance.int_from_prime_division(pd)
  end

  def self.prime_division(value, generator = Prime::Generator23.new)
    instance.prime_division(value, generator)
  end

  def self.lazy
    instance.lazy
  end

  def self.first(*args)
    instance.first(*args)
  end

  def self.take(n)
    instance.take(n)
  end

  def self.each_with_index(&block)
    instance.each_with_index(&block)
  end

  def self.to_a
    raise ArgumentError, "infinite sequence"
  end

  def each(ubound = nil, generator = EratosthenesGenerator.new, &block)
    generator.upper_bound = ubound
    generator.each(&block)
  end

  def include?(obj)
    case obj
    when Integer
      prime?(obj)
    when Module
      Module.instance_method(:include?).bind(Prime).call(obj)
    else
      false
    end
  end

  def prime?(value, generator = Prime::Generator23.new)
    raise ArgumentError, "Expected an integer, got #{value}" unless value.respond_to?(:integer?) && value.integer?
    return false if value < 2
    generator.each do |num|
      q, r = value.divmod num
      return true if q < num
      return false if r == 0
    end
  end

  def int_from_prime_division(pd)
    pd.inject(1) { |value, (prime, index)| value * prime**index }
  end

  def prime_division(value, generator = Prime::Generator23.new)
    raise ZeroDivisionError if value == 0
    if value < 0
      value = -value
      pv = [[-1, 1]]
    else
      pv = []
    end
    generator.each do |prime|
      count = 0
      while (value1, mod = value.divmod(prime)
             mod) == 0
        value = value1
        count += 1
      end
      if count != 0
        pv.push [prime, count]
      end
      break if value1 <= prime
    end
    if value > 1
      pv.push [value, 1]
    end
    pv
  end

  class PseudoPrimeGenerator
    include Enumerable

    def initialize(ubound = nil)
      @ubound = ubound
    end

    def upper_bound=(ubound)
      @ubound = ubound
    end

    def upper_bound
      @ubound
    end

    def succ
      raise NotImplementedError, "need to define `succ'"
    end

    def next
      raise NotImplementedError, "need to define `next'"
    end

    def rewind
      raise NotImplementedError, "need to define `rewind'"
    end

    def each
      return self.dup unless block_given?
      if @ubound
        last_value = nil
        loop do
          prime = succ
          break last_value if prime > @ubound
          last_value = yield prime
        end
      else
        begin
          yield succ
        end while true
      end
    end

    def with_index(offset = 0, &block)
      return enum_for(:with_index, offset) { Float::INFINITY } unless block
      return each_with_index(&block) if offset == 0
      each do |prime|
        yield prime, offset
        offset += 1
      end
    end

    def with_object(obj)
      return enum_for(:with_object, obj) { Float::INFINITY } unless block_given?
      each do |prime|
        yield prime, obj
      end
    end

    def size
      Float::INFINITY
    end
  end

  class EratosthenesGenerator < PseudoPrimeGenerator
    def initialize
      @last_prime_index = -1
      super
    end

    def succ
      @last_prime_index += 1
      EratosthenesSieve.instance.get_nth_prime(@last_prime_index)
    end

    def rewind
      initialize
    end
    alias next succ
  end

  class TrialDivisionGenerator < PseudoPrimeGenerator
    def initialize
      @index = -1
      super
    end

    def succ
      TrialDivision.instance[@index += 1]
    end

    def rewind
      initialize
    end
    alias next succ
  end

  class Generator23 < PseudoPrimeGenerator
    def initialize
      @prime = 1
      @step = nil
      super
    end

    def succ
      if @step
        @prime += @step
        @step = 6 - @step
      else
        case @prime
        when 1 then @prime = 2
        when 2 then @prime = 3
        when 3 then @prime = 5; @step = 2
        end
      end
      @prime
    end
    alias next succ

    def rewind
      initialize
    end
  end

  class TrialDivision
    include Singleton

    def initialize
      @primes = [2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79, 83, 89, 97, 101]
      @next_to_check = 103
      @ulticheck_index = 3
      @ulticheck_next_squared = 121
    end

    def cache
      @primes
    end
    alias primes cache
    alias primes_so_far cache

    def [](index)
      while index >= @primes.length
        while @ulticheck_next_squared <= @next_to_check
          @ulticheck_index += 1
          @ulticheck_next_squared = @primes.at(@ulticheck_index + 1)**2
        end
        @primes.push @next_to_check if @primes[2..@ulticheck_index].find { |prime| @next_to_check % prime == 0 }.nil?
        @next_to_check += 4
        @primes.push @next_to_check if @primes[2..@ulticheck_index].find { |prime| @next_to_check % prime == 0 }.nil?
        @next_to_check += 2
      end
      @primes[index]
    end
  end

  class EratosthenesSieve
    include Singleton

    def initialize
      @primes = [2, 3, 5, 7, 11, 13]
      @max_checked = @primes.last + 1
    end

    def get_nth_prime(n)
      compute_primes while @primes.size <= n
      @primes[n]
    end

    private

    def compute_primes
      max_segment_size = 1e6.to_i
      max_cached_prime = @primes.last
      @max_checked = max_cached_prime + 1 if max_cached_prime > @max_checked

      segment_min = @max_checked
      segment_max = [segment_min + max_segment_size, max_cached_prime * 2].min
      root = Integer.sqrt(segment_max)

      segment = ((segment_min + 1)..segment_max).step(2).to_a

      (1..Float::INFINITY).each do |sieving|
        prime = @primes[sieving]
        break if prime > root
        composite_index = (-(segment_min + 1 + prime) / 2) % prime
        while composite_index < segment.size do
          segment[composite_index] = nil
          composite_index += prime
        end
      end

      @primes.concat(segment.compact!)

      @max_checked = segment_max
    end
  end
end
