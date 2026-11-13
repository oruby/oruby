package core_ext

import (
	"github.com/oruby/oruby"
)

const rubyCoreExt = `
# Encoding extensions
class EncodingError < StandardError; end unless defined?(EncodingError)
module Encoding
  class CompatibilityError < ::EncodingError; end

  class Dummy
    attr_reader :name
    def initialize(name)
      @name = name.to_s
    end
    def to_s; @name; end
    def inspect; "#<Encoding:#{@name}>"; end
    def ==(other)
      to_s == other.to_s || (other.respond_to?(:name) && @name == other.name)
    end
    def ascii_compatible?
      !%w[UTF-16BE UTF-16LE UTF-32 UTF-32BE UTF-32LE].include?(@name)
    end
  end
end

class << Encoding
  def default_external
    @default_external ||= (const_defined?(:UTF_8) ? const_get(:UTF_8) : "UTF-8")
  end
  def default_external=(enc)
    @default_external = enc ? (enc.respond_to?(:to_str) ? enc.to_str : find(enc.to_s)) : nil
  end
  def default_internal
    @default_internal ||= nil
  end
  def default_internal=(enc)
    @default_internal = enc ? (enc.respond_to?(:to_str) ? enc.to_str : find(enc.to_s)) : nil
  end

  def find(name)
    s = name.to_s.upcase.tr('-', '_')
    case s
    when 'UTF_8', 'UTF8'
      const_defined?(:UTF_8) ? const_get(:UTF_8) : Encoding::Dummy.new("UTF-8")
    when 'US_ASCII', 'ASCII'
      Encoding::Dummy.new("US-ASCII")
    when 'BINARY', 'ASCII_8BIT'
      const_defined?(:BINARY) ? const_get(:BINARY) : Encoding::Dummy.new("ASCII-8BIT")
    when 'UTF_16BE', 'UTF16BE'
      Encoding::Dummy.new("UTF-16BE")
    when 'UTF_16LE', 'UTF16LE'
      Encoding::Dummy.new("UTF-16LE")
    else
      Encoding::Dummy.new(name.to_s)
    end
  end
end

Encoding::US_ASCII = Encoding::Dummy.new("US-ASCII")
Encoding::UTF_16BE = Encoding::Dummy.new("UTF-16BE")
Encoding::UTF_16LE = Encoding::Dummy.new("UTF-16LE")

class String
  def name
    self
  end

  alias __orig_eq ==
  def ==(other)
    if other.is_a?(Encoding::Dummy)
      return other == self
    end
    __orig_eq(other)
  end

  def ascii_compatible?
    self == "UTF-8" || self == "US-ASCII" || self == "ASCII-8BIT" || self == "BINARY"
  end

  alias __orig_force_encoding force_encoding
  def force_encoding(enc)
    enc = Encoding.find(enc) if enc.is_a?(String) || enc.is_a?(Symbol)
    $__string_encodings ||= {}
    $__string_encodings[object_id] = enc
    begin
      __orig_force_encoding(enc.to_s)
    rescue ArgumentError
      self
    end
  end

  alias __orig_encoding encoding
  def encoding
    $__string_encodings ||= {}
    if $__string_encodings.key?(object_id)
      $__string_encodings[object_id]
    else
      begin
        __orig_encoding
      rescue
        Encoding::UTF_8
      end
    end
  end

  def encode(enc)
    dup.force_encoding(enc)
  end
  def encode!(enc)
    force_encoding(enc)
  end
end

class Float
  alias __orig_eq ==
  def ==(other)
    return true if equal?(other)
    if other.is_a?(Float) || other.is_a?(Integer)
      __orig_eq(other)
    elsif other.respond_to?(:==)
      other == self
    else
      false
    end
  end
end

EncodingError = Class.new(StandardError) unless Object.const_defined?(:EncodingError)

# Thread extensions for local storage
class Thread
  def [](key)
    @__locals ||= {}
    @__locals[key.to_s]
  end
  def []=(key, val)
    @__locals ||= {}
    @__locals[key.to_s] = val
  end
  def key?(key)
    @__locals ||= {}
    @__locals.key?(key.to_s)
  end
  def keys
    @__locals ||= {}
    @__locals.keys.map(&:to_sym)
  end
end

# Helpers for specs
module Kernel
  def warn(*msg)
    $stderr.puts(*msg)
  end

  def fixnum_max
    (1 << 62) - 1
  end

  def fixnum_min
    -(1 << 62)
  end

  def bignum_value(plus = 0)
    Bignum.new("9223372036854775808") + plus
  end

  alias __orig_to_enum to_enum
  def to_enum(meth = :each, *args, &block)
    enum = __orig_to_enum(meth, *args)
    if block
      enum.instance_variable_set(:@size_block, block)
    end
    enum
  end
  alias enum_for to_enum
end

class Enumerator
  alias __orig_size size
  def size
    if instance_variable_defined?(:@size_block)
      sb = instance_variable_get(:@size_block)
      sb ? sb.call : nil
    else
      __orig_size
    end
  end
end

def bignum_value(plus = 0)
  Bignum.new("9223372036854775808") + plus
end

def fixnum_max
  (1 << 62) - 1
end

def fixnum_min
  -(1 << 62)
end

# Array extensions
class Array
  def instance_variable_set(name, val)
    $__array_ivars ||= {}
    ($__array_ivars[object_id] ||= {})[name.to_s] = val
  end

  def instance_variable_get(name)
    $__array_ivars ||= {}
    ($__array_ivars[object_id] ||= {})[name.to_s]
  end

  def instance_variable_defined?(name)
    $__array_ivars ||= {}
    ($__array_ivars[object_id] ||= {}).key?(name.to_s)
  end

  def instance_variables
    $__array_ivars ||= {}
    ($__array_ivars[object_id] ||= {}).keys.map(&:to_sym)
  end

  def hash
    Thread.current[:__array_hash_stack] ||= []
    stack = Thread.current[:__array_hash_stack]
    return :cycle if stack.include?(object_id)
    stack.push(object_id)
    has_cycle = false
    h = size & 0x3fffffff
    begin
      each do |elem|
        eh = elem.hash
        if eh == :cycle
          has_cycle = true
          eh = 0
        else
          eh = eh.to_int if !eh.is_a?(Integer) && (eh.respond_to?(:to_int) rescue false)
        end
        h = ((h * 31) + (eh.to_i & 0x3fffffff)) & 0x3fffffff
      end
    ensure
      stack.pop
    end
    return :cycle if has_cycle && (stack.any? || (Thread.current[:__hash_hash_stack] && Thread.current[:__hash_hash_stack].any?))
    return 0 if has_cycle
    h
  end

  def to_ary
    self
  end

  alias __orig_cmp <=>
  def <=>(other)
    return 0 if equal?(other)
    unless other.is_a?(Array)
      return nil unless other.respond_to?(:to_ary)
      other = other.to_ary
      return nil unless other.is_a?(Array)
    end
    pair_id = [object_id, other.object_id]
    Thread.current[:__array_cmp_pairs] ||= {}
    pairs = Thread.current[:__array_cmp_pairs]
    return 0 if pairs[pair_id]
    pairs[pair_id] = true
    begin
      min_len = length < other.length ? length : other.length
      i = 0
      while i < min_len
        res = (self[i] <=> other[i])
        return res if res != 0
        i += 1
      end
      length <=> other.length
    ensure
      pairs.delete(pair_id)
    end
  end

  alias __orig_eq ==
  def ==(other)
    return true if equal?(other)
    unless other.is_a?(Array)
      if other.respond_to?(:to_ary)
        return other == self
      else
        return false
      end
    end
    __orig_eq(other)
  end

  def self.__coerce_to_int(idx)
    if defined?(Bignum) && (idx.is_a?(Bignum) || idx.class.name == "Bignum")
      raise RangeError, "bignum too big to convert into long"
    end
    return idx if idx.is_a?(Integer)
    if idx.respond_to?(:to_int)
      val = idx.to_int
      if defined?(Bignum) && (val.is_a?(Bignum) || val.class.name == "Bignum")
        raise RangeError, "bignum too big to convert into long"
      end
      raise TypeError, "can't convert #{idx.class} to Integer (#{idx.class}#to_int gives #{val.class})" unless val.is_a?(Integer)
      val
    elsif idx.is_a?(Float)
      begin
        idx.to_i
      rescue RangeError
        raise RangeError, "float too big to convert into long"
      end
    else
      raise TypeError, "no implicit conversion of #{idx.class} into Integer"
    end
  end

  def self.__coerce_to_ary(obj)
    return obj if obj.is_a?(Array)
    if obj.respond_to?(:to_ary)
      val = obj.to_ary
      return nil if val.nil?
      raise TypeError, "can't convert #{obj.class} to Array (#{obj.class}#to_ary gives #{val.class})" unless val.is_a?(Array)
      val
    else
      raise TypeError, "no implicit conversion of #{obj.class} into Array"
    end
  end

  def self.__coerce_to_str(obj)
    return obj if obj.is_a?(String)
    if obj.respond_to?(:to_str)
      val = obj.to_str
      raise TypeError, "can't convert #{obj.class} to String (#{obj.class}#to_str gives #{val.class})" unless val.is_a?(String)
      val
    else
      raise TypeError, "no implicit conversion of #{obj.class} into String"
    end
  end

  def self.__has_custom_method?(item, sym)
    sc = (class << item; self; end rescue nil)
    if sc
      all = (sc.instance_methods(false) rescue []) + (sc.private_instance_methods(false) rescue [])
      return true if all.include?(sym)
    end
    k = item.class rescue nil
    while k && k != Object && k != Kernel && k != BasicObject
      all = (k.instance_methods(false) rescue []) + (k.private_instance_methods(false) rescue [])
      return true if all.include?(sym)
      k = k.superclass rescue nil
    end
    false
  end

  def self.__check_to_ary(item)
    return item if Array === item

    if __has_custom_method?(item, :respond_to_missing?)
      res = (item.__send__(:respond_to_missing?, :to_ary, true) rescue nil)
      return nil unless res
      val = item.to_ary
      return nil if val.nil?
      raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
      val
    elsif __has_custom_method?(item, :respond_to?)
      res = (item.respond_to?(:to_ary, true) rescue false)
      return nil unless res
      val = item.to_ary
      return nil if val.nil?
      raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
      val
    elsif __has_custom_method?(item, :method_missing)
      begin
        val = item.to_ary
        return nil if val.nil?
        raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
        val
      rescue NoMethodError => e
        if e.name == :to_ary
          nil
        else
          raise e
        end
      end
    elsif (item.respond_to?(:to_ary, true) rescue false)
      val = item.to_ary
      return nil if val.nil?
      raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{val.class})" unless Array === val
      val
    else
      nil
    end
  end

  def __wrap_subclass(res)
    if res.is_a?(Array) && self.class != Array
      self.class.allocate.replace(res)
    else
      res
    end
  end

  def self.try_convert(obj)
    return obj if obj.is_a?(Array)
    return nil unless obj.respond_to?(:to_ary)
    res = obj.to_ary
    return nil if res.nil?
    raise TypeError, "can't convert #{obj.class} into Array (#{obj.class}#to_ary gives #{res.class})" unless res.is_a?(Array)
    res
  end

  alias __orig_at at
  def at(idx)
    idx = Array.__coerce_to_int(idx)
    __orig_at(idx)
  end

  alias __orig_aref []
  def [](arg, *rest)
    if rest.empty?
      if arg.is_a?(Range)
        b = arg.begin
        e = arg.end
        b = Array.__coerce_to_int(b) if b
        e = Array.__coerce_to_int(e) if e
        rng = Range.new(b, e, arg.exclude_end?)
        res = __orig_aref(rng)
        return __wrap_subclass(res)
      end
      idx = Array.__coerce_to_int(arg)
      __orig_aref(idx)
    elsif rest.size == 1
      start = Array.__coerce_to_int(arg)
      len = Array.__coerce_to_int(rest[0])
      res = __orig_aref(start, len)
      __wrap_subclass(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{rest.size + 1}, expected 1..2)"
    end
  end
  alias slice []

  alias __orig_aset []=
  def []=(*args)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if args.size == 2
      idx, val = args
      if idx.is_a?(Range)
        b = idx.begin
        e = idx.end
        b = Array.__coerce_to_int(b) if b
        e = Array.__coerce_to_int(e) if e
        val = val.to_ary if !val.is_a?(Array) && val.respond_to?(:to_ary)
        return __orig_aset(Range.new(b, e, idx.exclude_end?), val)
      end
      idx = Array.__coerce_to_int(idx)
      __orig_aset(idx, val)
    elsif args.size == 3
      start, len, val = args
      start = Array.__coerce_to_int(start)
      len = Array.__coerce_to_int(len)
      val = val.to_ary if !val.is_a?(Array) && val.respond_to?(:to_ary)
      __orig_aset(start, len, val)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 2..3)"
    end
  end

  alias __orig_insert insert
  def insert(*args)
    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if args.empty?
    raise FrozenError, "can't modify frozen Array" if frozen?
    pos = Array.__coerce_to_int(args[0])
    return self if args.size == 1
    __orig_insert(pos, *args[1..-1])
  end

  alias __orig_plus +
  def +(other)
    other = Array.__coerce_to_ary(other)
    res = __orig_plus(other)
    res.class == Array ? res : Array.new(res)
  end

  alias __orig_index index
  def index(*args, &block)
    if args.size == 1
      warn "warning: given block not used" if block
      __orig_index(args[0])
    elsif args.empty?
      if block
        __orig_index(&block)
      else
        to_enum(:index) { nil }
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end
  alias find_index index

  alias __orig_rindex rindex
  def rindex(*args, &block)
    if args.size == 1
      warn "warning: given block not used" if block
      __orig_rindex(args[0])
    elsif args.empty?
      if block
        __orig_rindex(&block)
      else
        to_enum(:rindex) { nil }
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  def dig(idx, *rest)
    val = self.at(idx)
    return nil if val.nil?
    return val if rest.empty?
    raise TypeError, "#{val.class} does not have #dig method" unless val.respond_to?(:dig)
    val.dig(*rest)
  end

  alias __orig_fill fill
  def fill(*args, &block)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if block_given?
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)" if args.size > 2
      case args.size
      when 0
        (0...size).each { |i| self[i] = yield(i) }
      when 1
        arg = args[0]
        if arg.is_a?(Range)
          b = arg.begin ? Array.__coerce_to_int(arg.begin) : 0
          e = arg.end ? Array.__coerce_to_int(arg.end) : -1
          b = size + b if b < 0
          raise RangeError, "#{b} out of range" if b < 0
          e = size + e if e < 0
          e -= 1 if arg.exclude_end?
          (b..e).each { |i| self[i] = yield(i) }
        else
          start = arg.nil? ? 0 : Array.__coerce_to_int(arg)
          start = size + start if start < 0
          start = 0 if start < 0
          (start...size).each { |i| self[i] = yield(i) }
        end
      when 2
        start = args[0].nil? ? 0 : Array.__coerce_to_int(args[0])
        len = args[1].nil? ? nil : Array.__coerce_to_int(args[1])
        start = size + start if start < 0
        start = 0 if start < 0
        count = len.nil? ? (size - start) : len
        return self if count <= 0
        (start...(start + count)).each { |i| self[i] = yield(i) }
      end
    else
      raise ArgumentError, "wrong number of arguments (given 0, expected 1..3)" if args.empty?
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..3)" if args.size > 3
      filler = args[0]
      case args.size
      when 1
        (0...size).each { |i| self[i] = filler }
      when 2
        arg = args[1]
        if arg.is_a?(Range)
          b = arg.begin ? Array.__coerce_to_int(arg.begin) : 0
          e = arg.end ? Array.__coerce_to_int(arg.end) : -1
          b = size + b if b < 0
          raise RangeError, "#{b} out of range" if b < 0
          e = size + e if e < 0
          e -= 1 if arg.exclude_end?
          (b..e).each { |i| self[i] = filler }
        else
          start = arg.nil? ? 0 : Array.__coerce_to_int(arg)
          start = size + start if start < 0
          start = 0 if start < 0
          (start...size).each { |i| self[i] = filler }
        end
      when 3
        start = args[1].nil? ? 0 : Array.__coerce_to_int(args[1])
        len = args[2].nil? ? nil : Array.__coerce_to_int(args[2])
        start = size + start if start < 0
        start = 0 if start < 0
        count = len.nil? ? (size - start) : len
        return self if count <= 0
        (start...(start + count)).each { |i| self[i] = filler }
      end
    end
    self
  end



  alias __orig_first first
  def first(*args)
    if args.empty?
      __orig_first
    elsif args.size == 1
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      res = __orig_first(n)
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_last last
  def last(*args)
    if args.empty?
      __orig_last
    elsif args.size == 1
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      res = __orig_last(n)
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_fetch fetch
  def fetch(idx, *rest, &block)
    raise ArgumentError, "wrong number of arguments (given #{1 + rest.size}, expected 1..2)" if rest.size > 1
    warn "warning: block supersedes default value argument" if !rest.empty? && block
    orig_idx = idx
    c_idx = Array.__coerce_to_int(idx)
    len = self.length
    if c_idx < 0
      actual_idx = len + c_idx
    else
      actual_idx = c_idx
    end
    if actual_idx >= 0 && actual_idx < len
      self[actual_idx]
    elsif block
      block.call(orig_idx)
    elsif !rest.empty?
      rest.first
    else
      raise IndexError, "index #{c_idx} outside of array bounds: -#{len}...#{len}"
    end
  end

  alias __orig_bsearch bsearch
  def bsearch(&block)
    return to_enum(:bsearch) { nil } unless block
    __orig_bsearch(&block)
  end

  alias __orig_bsearch_index bsearch_index
  def bsearch_index(&block)
    return to_enum(:bsearch_index) { nil } unless block
    __orig_bsearch_index(&block)
  end

  def self.__combination_size(n, k)
    return 0 if k < 0 || k > n
    return 1 if k == 0 || k == n
    k = n - k if k > n / 2
    res = 1
    (1..k).each { |i| res = (res * (n - i + 1)) / i }
    res
  end

  def self.__permutation_size(n, k)
    return 0 if k < 0 || k > n
    return 1 if k == 0
    res = 1
    (0...k).each { |i| res *= (n - i) }
    res
  end

  def self.__repeated_combination_size(n, k)
    return 0 if k < 0
    return 1 if k == 0
    return 0 if n == 0
    __combination_size(n + k - 1, k)
  end

  def self.__repeated_permutation_size(n, k)
    return 0 if k < 0
    return 1 if k == 0
    return 0 if n == 0
    n ** k
  end

  alias __orig_combination combination
  def combination(k, &block)
    ck = Array.__coerce_to_int(k)
    unless block_given?
      return to_enum(:combination, ck) { Array.__combination_size(size, ck) }
    end
    dup.__orig_combination(ck, &block)
    self
  end

  alias __orig_permutation permutation
  def permutation(k = nil, &block)
    if k.nil?
      unless block_given?
        return to_enum(:permutation) { Array.__permutation_size(size, size) }
      end
      ck = size
    else
      ck = Array.__coerce_to_int(k)
      unless block_given?
        return to_enum(:permutation, ck) { Array.__permutation_size(size, ck) }
      end
    end
    dup.__orig_permutation(ck, &block)
    self
  end

  alias __orig_repeated_combination repeated_combination
  def repeated_combination(k, &block)
    ck = Array.__coerce_to_int(k)
    unless block_given?
      return to_enum(:repeated_combination, ck) { Array.__repeated_combination_size(size, ck) }
    end
    dup.__orig_repeated_combination(ck, &block)
    self
  end

  alias __orig_repeated_permutation repeated_permutation
  def repeated_permutation(k, &block)
    ck = Array.__coerce_to_int(k)
    unless block_given?
      return to_enum(:repeated_permutation, ck) { Array.__repeated_permutation_size(size, ck) }
    end
    dup.__orig_repeated_permutation(ck, &block)
    self
  end

  alias __orig_cycle cycle
  def cycle(nv = nil, &block)
    unless block_given?
      return to_enum(:cycle, nv) do
        if nv.nil?
          Float::INFINITY
        else
          c = Array.__coerce_to_int(nv)
          c <= 0 || empty? ? 0 : size * c
        end
      end
    end
    n = nv.nil? ? -1 : Array.__coerce_to_int(nv)
    return nil if empty? || (n <= 0 && !nv.nil?)
    ary = []
    each do |x|
      ary.push(x)
      yield x
    end
    return nil if ary.empty?
    while n < 0 || (n -= 1) > 0
      ary.each do |x|
        yield x
      end
    end
    nil
  end

  alias __orig_drop drop
  def drop(n)
    n = Array.__coerce_to_int(n)
    raise ArgumentError, "attempt to drop negative size" if n < 0
    res = __orig_drop(n)
    Array.new(res)
  end

  alias __orig_take take
  def take(n)
    n = Array.__coerce_to_int(n)
    raise ArgumentError, "attempt to take negative size" if n < 0
    res = __orig_take(n)
    Array.new(res)
  end

  alias __orig_pop pop
  def pop(*args)
    if args.empty?
      raise FrozenError, "can't modify frozen Array" if frozen?
      __orig_pop
    elsif args.size == 1
      raise FrozenError, "can't modify frozen Array" if frozen?
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      return [] if n == 0
      start = [0, self.length - n].max
      count = [n, self.length].min
      res = self.slice!(start, count) || []
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_shift shift
  def shift(*args)
    if args.empty?
      raise FrozenError, "can't modify frozen Array" if frozen?
      __orig_shift
    elsif args.size == 1
      raise FrozenError, "can't modify frozen Array" if frozen?
      n = Array.__coerce_to_int(args[0])
      raise ArgumentError, "negative array size" if n < 0
      return [] if n == 0
      res = self.slice!(0, n) || []
      Array.new(res)
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
  end

  alias __orig_delete_at delete_at
  def delete_at(idx)
    idx = Array.__coerce_to_int(idx)
    __orig_delete_at(idx)
  end

  alias __orig_rotate rotate
  def rotate(n = 1)
    n = Array.__coerce_to_int(n)
    res = __orig_rotate(n)
    Array.new(res)
  end

  alias __orig_rotate_bang rotate!
  def rotate!(n = 1)
    n = Array.__coerce_to_int(n)
    __orig_rotate_bang(n)
  end

  def values_at(*args)
    res = []
    args.each do |arg|
      if arg.is_a?(Range)
        b = arg.begin
        e = arg.end
        b = Array.__coerce_to_int(b) if b
        e = Array.__coerce_to_int(e) if e
        len = length
        b_idx = (b && b < 0) ? len + b : (b || 0)
        e_idx = (e && e < 0) ? len + e : (e || -1)
        if b_idx < 0 || (b && e && b >= 0 && e >= 0 && b > e)
          next
        end
        rng = Range.new(b_idx, e_idx, arg.exclude_end?)
        rng.each do |i|
          res << self[i]
        end
      else
        idx = Array.__coerce_to_int(arg)
        res << self[idx]
      end
    end
    res
  end

  def self.__resolve_encoding(strings)
    enc = nil
    has_high_utf8 = false
    has_high_binary = false
    strings.each do |s|
      next unless s.is_a?(String)
      e = s.encoding
      e_name = (e.respond_to?(:name) ? e.name : e.to_s).upcase
      is_binary = (e_name == 'BINARY' || e_name == 'ASCII-8BIT')
      is_utf8 = (e_name == 'UTF-8' || e_name == 'UTF8')
      has_high = s.bytes.any? { |b| b > 127 }
      if is_binary && has_high
        has_high_binary = true
      elsif is_utf8 && has_high
        has_high_utf8 = true
      end
      if has_high_binary && has_high_utf8
        raise Encoding::CompatibilityError, "incompatible character encodings: UTF-8 and ASCII-8BIT"
      end
      if enc.nil?
        enc = e
      elsif (enc.name == 'US-ASCII' rescue enc.to_s == 'US-ASCII') && (is_utf8 && has_high)
        enc = e
      end
    end
    enc || (defined?(Encoding::UTF_8) ? Encoding::UTF_8 : "UTF-8")
  end

  alias __orig_join join
  def join(sep = nil)
    if empty?
      res = ""
      res.force_encoding(Encoding::US_ASCII) if defined?(Encoding::US_ASCII)
      return res
    end

    sep = $, if sep.nil?
    if sep.nil?
      sep_str = ""
    else
      sep_str = Array.__coerce_to_str(sep)
    end

    Thread.current[:__array_join_seen] ||= {}
    seen = Thread.current[:__array_join_seen]
    if seen[object_id]
      raise ArgumentError, "recursive array join"
    end
    seen[object_id] = true

    begin
      parts = map do |x|
        if x.is_a?(String)
          x
        else
          str = (x.to_str rescue nil) if x.respond_to?(:to_str)
          if str.is_a?(String)
            str
          else
            ary = (x.to_ary rescue nil) if x.respond_to?(:to_ary)
            if ary.is_a?(Array)
              ary.join(sep)
            elsif x.respond_to?(:to_s)
              x.to_s
            else
              raise NoMethodError, "undefined method 'to_s' for #{x.inspect}"
            end
          end
        end
      end
    ensure
      seen.delete(object_id)
    end

    target_enc = Array.__resolve_encoding(parts + (sep_str.empty? ? [] : [sep_str]))
    res = parts.__orig_join(sep_str)
    res.force_encoding(target_enc) if target_enc && res.respond_to?(:force_encoding)
    res
  end

  def flatten(level = nil)
    res = dup.flatten!(level)
    res = res ? res : dup
    __wrap_subclass(res)
  end

  def flatten!(level = nil)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if level
      level = Array.__coerce_to_int(level)
      return nil if level == 0
      level = -1 if level < 0
    else
      level = -1
    end
    res = []
    modified = false
    rec = ->(ary, curr_level, seen) {
      ary.each do |item|
        if curr_level == 0
          res << item
          next
        end
        sub = Array.__check_to_ary(item)
        if sub
          if !(sub.is_a?(Array) rescue false)
            raise TypeError, "can't convert #{item.class} to Array (#{item.class}#to_ary gives #{sub.class})"
          else
            modified = true
            raise ArgumentError, "tried to flatten recursive array" if seen.include?(sub.__id__)
            seen << sub.__id__
            rec.call(sub, curr_level - 1, seen)
            seen.delete(sub.__id__)
          end
        else
          res << item
        end
      end
    }
    rec.call(self, level, [self.__id__])
    if modified
      self.replace(res)
      self
    else
      nil
    end
  end

  alias __orig_sample sample
  def sample(*args)
    opts = {}
    if args.last.is_a?(Hash)
      opts = args.pop
    elsif args.last.respond_to?(:to_hash)
      opts = args.pop.to_hash
    end
    count_given = !args.empty?
    count = count_given ? Array.__coerce_to_int(args[0]) : nil
    raise ArgumentError, "negative array size" if count && count < 0

    rng = opts[:random]
    if rng
      raise NoMethodError, "undefined method 'rand' for #{rng}" unless rng.respond_to?(:rand)
      return nil if empty? && !count_given
      return [] if empty? && count_given
      return [] if count == 0

      if count_given
        n = [count, size].min
        indices = (0...size).to_a
        selected = []
        n.times do
          max = indices.size
          r = rng.rand(max)
          r = Array.__coerce_to_int(r) unless r.is_a?(Integer)
          raise RangeError, "random value #{r} out of bounds" if r < 0 || r >= max
          selected << self[indices.delete_at(r)]
        end
        selected
      else
        max = size
        r = rng.rand(max)
        r = Array.__coerce_to_int(r) unless r.is_a?(Integer)
        raise RangeError, "random value #{r} out of bounds" if r < 0 || r >= max
        self[r]
      end
    else
      if count_given
        __orig_sample(count)
      else
        __orig_sample
      end
    end
  end

  alias __orig_shuffle shuffle
  def shuffle(*args)
    opts = {}
    if args.last.is_a?(Hash)
      opts = args.pop
    elsif args.last.respond_to?(:to_hash)
      opts = args.pop.to_hash
    end
    rng = opts[:random]
    if rng
      raise NoMethodError, "undefined method 'rand' for #{rng}" unless rng.respond_to?(:rand)
      return [] if empty?
      indices = (0...size).to_a
      res = []
      size.times do
        max = indices.size
        r = rng.rand(max)
        r = Array.__coerce_to_int(r) unless r.is_a?(Integer)
        raise RangeError, "random value #{r} out of bounds" if r < 0 || r >= max
        res << self[indices.delete_at(r)]
      end
      Array.new(res)
    else
      Array.new(__orig_shuffle)
    end
  end

  def shuffle!(*args)
    raise FrozenError, "can't modify frozen Array" if frozen?
    self.replace(shuffle(*args))
    self
  end

  alias __orig_reverse reverse
  def reverse
    res = __orig_reverse
    res.class == Array ? res : Array.new(res)
  end

  alias __orig_reject reject
  def reject(&block)
    return to_enum(:reject) { size } unless block_given?
    res = []
    each do |x|
      res << x unless yield(x)
    end
    res
  end

  alias __orig_reject! reject!
  def reject!(&block)
    return to_enum(:reject!) { size } unless block_given?
    raise FrozenError, "can't modify frozen #{self.class}" if frozen?
    len = size
    w_idx = 0
    idx = 0
    changed = false
    begin
      while idx < len
        elem = self[idx]
        if yield(elem)
          changed = true
        else
          self[w_idx] = elem if w_idx != idx
          w_idx += 1
        end
        idx += 1
      end
    ensure
      if idx < len
        while idx < len
          self[w_idx] = self[idx]
          w_idx += 1
          idx += 1
        end
      end
      if w_idx < len
        slice!(w_idx..-1)
      end
    end
    changed ? self : nil
  end

  alias __orig_delete_if delete_if
  def delete_if(&block)
    return to_enum(:delete_if) { size } unless block_given?
    reject!(&block)
    self
  end

  alias __orig_select select
  def select(&block)
    return to_enum(:select) { size } unless block_given?
    res = []
    each do |x|
      res << x if yield(x)
    end
    res
  end
  alias filter select

  alias __orig_sort sort
  def sort(&block)
    res = __orig_sort(&block)
    res.class == Array ? res : Array.new(res)
  end

  def sort_by!(&block)
    return to_enum(:sort_by!) { size } unless block_given?
    raise FrozenError, "can't modify frozen #{self.class}" if frozen?
    self.replace(self.sort_by(&block))
  end

  alias __orig_slice_bang slice!
  def slice!(*args)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if args.size == 1
      arg = args[0]
      if arg.is_a?(Range)
        b = arg.begin ? Array.__coerce_to_int(arg.begin) : 0
        e = arg.end ? Array.__coerce_to_int(arg.end) : -1
        res = __orig_slice_bang(Range.new(b, e, arg.exclude_end?))
        return (res && res.class != Array) ? Array.new(res) : res
      end
      idx = Array.__coerce_to_int(arg)
      __orig_slice_bang(idx)
    elsif args.size == 2
      start = Array.__coerce_to_int(args[0])
      len = Array.__coerce_to_int(args[1])
      res = __orig_slice_bang(start, len)
      (res && res.class != Array) ? Array.new(res) : res
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..2)"
    end
  end

  def to_h(&block)
    h = {}
    each_with_index do |item, idx|
      kv = block_given? ? yield(item) : item
      unless kv.is_a?(Array)
        if kv.respond_to?(:to_ary)
          kv = kv.to_ary
          raise TypeError, "wrong element type #{kv.class} at #{idx} (expected Array)" unless kv.is_a?(Array)
        else
          raise TypeError, "wrong element type #{kv.class} at #{idx} (expected Array)"
        end
      end
      raise ArgumentError, "wrong array length at #{idx} (expected 2, was #{kv.length})" unless kv.length == 2
      h[kv[0]] = kv[1]
    end
    h
  end

  alias __orig_zip zip
  def zip(*args, &block)
    coerced = args.map do |arg|
      if !arg.is_a?(Array) && arg.respond_to?(:to_ary)
        arg.to_ary
      else
        arg
      end
    end
    __orig_zip(*coerced, &block)
  end

  alias __orig_concat concat
  def concat(*others)
    coerced = others.map do |other|
      o = Array.__coerce_to_ary(other)
      o.equal?(self) ? o.dup : o
    end
    coerced.each do |o|
      __orig_concat(o)
    end
    self
  end

  alias __orig_transpose transpose
  def transpose
    return [] if empty?
    coerced = map { |x| Array.__coerce_to_ary(x) }
    coerced.__orig_transpose
  end

  alias __orig_replace replace
  def replace(other)
    other = Array.__coerce_to_ary(other)
    __orig_replace(other)
  end

  alias __orig_mul *
  def *(arg)
    if arg.respond_to?(:to_str)
      join(arg.to_str)
    elsif arg.respond_to?(:to_int)
      n = arg.to_int
      raise TypeError, "can't convert #{arg.class} to Integer" unless n.is_a?(Integer)
      raise ArgumentError, "negative argument" if n < 0
      __wrap_subclass(__orig_mul(n))
    elsif arg.is_a?(Integer)
      raise ArgumentError, "negative argument" if arg < 0
      __wrap_subclass(__orig_mul(arg))
    elsif arg.is_a?(String)
      join(arg)
    else
      raise TypeError, "no implicit conversion of #{arg.class} into Integer"
    end
  end

  alias __orig_product product
  def product(*others, &block)
    others = others.map { |o| Array.__coerce_to_ary(o) }
    if block_given?
      return self if empty? || others.any?(&:empty?)
      pools = [self] + others
      generate = ->(pool_idx, current) {
        if pool_idx == pools.size
          yield(current.dup)
        else
          pools[pool_idx].each do |el|
            current.push(el)
            generate.call(pool_idx + 1, current)
            current.pop
          end
        end
      }
      generate.call(0, [])
      self
    else
      total = size
      others.each do |o|
        total *= o.size
        raise RangeError, "too big for product" if total > 1_000_000_000
      end
      __orig_product(*others)
    end
  end

  def uniq(&block)
    seen = {}
    res = []
    each do |x|
      val = block_given? ? yield(x) : x
      h = (val.hash rescue nil)
      found = false
      bucket = (seen[h] ||= [])
      bucket.each do |item|
        if (item.equal?(val) rescue false) || (item.eql?(val) rescue false)
          found = true
          break
        end
      end
      unless found
        bucket << val
        res << x
      end
    end
    self.class == Array ? res : self.class.allocate.replace(res)
  end

  def uniq!(&block)
    raise FrozenError, "can't modify frozen #{self.class}" if frozen?
    u = uniq(&block)
    if u.length == self.length
      nil
    else
      self.replace(u)
      self
    end
  end

  alias __orig_init initialize
  def initialize(*args, &block)
    raise FrozenError, "can't modify frozen Array" if frozen?
    if args.empty?
      self.clear
    elsif args.size == 1
      arg = args[0]
      if arg.is_a?(Array)
        self.replace(arg)
      elsif (arg.respond_to?(:to_ary, true) rescue false)
        self.replace(arg.send(:to_ary))
      else
        size = Array.__coerce_to_int(arg)
        raise ArgumentError, "negative array size" if size < 0
        raise ArgumentError, "array size too big" if size > 100_000_000
        self.clear
        if block_given?
          size.times { |i| self << yield(i) }
        else
          size.times { self << nil }
        end
      end
    elsif args.size == 2
      arg0, default_obj = args
      raise TypeError, "wrong argument type Array" if arg0.is_a?(Array) || (arg0.respond_to?(:to_ary, true) rescue false)
      size = Array.__coerce_to_int(arg0)
      raise ArgumentError, "negative array size" if size < 0
      raise ArgumentError, "array size too big" if size > 100_000_000
      self.clear
      if block_given?
        warn "warning: block supersedes default value argument" if $-w || $VERBOSE || (Thread.current[:in_mspec_complain_matcher] rescue false)
        size.times { |i| self << yield(i) }
      else
        size.times { self << default_obj }
      end
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)"
    end
    self
  end

  alias __orig_difference difference
  def difference(*others)
    coerced = others.map { |o| Array.__coerce_to_ary(o) }
    __orig_difference(*coerced)
  end

  alias __orig_minus -
  def -(other)
    __orig_minus(Array.__coerce_to_ary(other))
  end

  alias __orig_union union
  def union(*others)
    coerced = others.map { |o| Array.__coerce_to_ary(o) }
    __orig_union(*coerced)
  end

  alias __orig_or |
  def |(other)
    __orig_or(Array.__coerce_to_ary(other))
  end

  alias __orig_intersection intersection
  def intersection(*others)
    coerced = others.map { |o| Array.__coerce_to_ary(o) }
    __orig_intersection(*coerced)
  end

  alias __orig_and &
  def &(other)
    __orig_and(Array.__coerce_to_ary(other))
  end
end

class Hash
  alias __orig_hash hash
  def hash
    Thread.current[:__hash_hash_stack] ||= []
    stack = Thread.current[:__hash_hash_stack]
    return :cycle if stack.include?(object_id)
    stack.push(object_id)
    has_cycle = false
    h = size & 0x3fffffff
    begin
      each do |k, v|
        kh = k.hash
        vh = v.hash
        if kh == :cycle || vh == :cycle
          has_cycle = true
          kh = 0 if kh == :cycle
          vh = 0 if vh == :cycle
        else
          kh = kh.to_int if !kh.is_a?(Integer) && (kh.respond_to?(:to_int) rescue false)
          vh = vh.to_int if !vh.is_a?(Integer) && (vh.respond_to?(:to_int) rescue false)
        end
        h = ((h * 31) + (kh.to_i & 0x3fffffff) + (vh.to_i & 0x3fffffff)) & 0x3fffffff
      end
    ensure
      stack.pop
    end
    return :cycle if has_cycle && (stack.any? || (Thread.current[:__array_hash_stack] && Thread.current[:__array_hash_stack].any?))
    return 0 if has_cycle
    h
  end
end
`

func init() {
	oruby.Gem("core_ext", func(mrb *oruby.MrbState) interface{} {
		_, err := mrb.Eval(rubyCoreExt)
		if err != nil {
			panic("core_ext init error: " + err.Error())
		}
		return nil
	})
}
