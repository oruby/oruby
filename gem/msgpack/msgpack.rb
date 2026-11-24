module MessagePack
  VERSION = "1.8.7"

  class ExtensionValue < Struct.new(:type, :payload)
    def to_msgpack(packer_or_io = nil)
      if packer_or_io
        if packer_or_io.is_a?(MessagePack::Packer)
          packer_or_io.write_extension(self)
          packer_or_io
        else
          MessagePack.pack(self, packer_or_io)
        end
      else
        MessagePack.pack(self)
      end
    end
  end

  class Buffer
    undef_method :dup
    undef_method :clone
  end

  class Packer
    undef_method :dup
    undef_method :clone

    def register_type(type, klass, method_name_or_proc = nil, &block)
      raise FrozenError, "can't modify frozen MessagePack::Packer" if frozen?
      raise ArgumentError, "expected Module/Class got: #{klass.inspect}" unless klass.is_a?(Module)
      proc_obj = if block
        block
      elsif method_name_or_proc.is_a?(Proc)
        method_name_or_proc
      elsif method_name_or_proc.is_a?(Symbol) || method_name_or_proc.is_a?(String)
        method_name_or_proc.to_sym.to_proc
      elsif method_name_or_proc.is_a?(Method)
        method_name_or_proc.to_proc
      elsif method_name_or_proc.nil?
        nil
      else
        raise ::TypeError, "expected Proc, got: #{method_name_or_proc.inspect}"
      end
      register_type_internal(type, klass, proc_obj)
    end

    def registered_types
      list = []
      registered_types_internal.each_pair do |klass, ary|
        list << { type: ary[0], class: klass, packer: ary[1] }
      end
      list.sort { |a, b| a[:type] <=> b[:type] }
    end

    def type_registered?(klass_or_type)
      case klass_or_type
      when Module
        registered_types.any? { |entry| klass_or_type <= entry[:class] }
      when Integer
        registered_types.any? { |entry| klass_or_type == entry[:type] }
      else
        raise ArgumentError, "class or type id"
      end
    end
  end

  class Unpacker
    undef_method :dup
    undef_method :clone

    def register_type(type, klass = nil, method_name = nil, &block)
      raise FrozenError, "can't modify frozen MessagePack::Unpacker" if frozen?
      if klass && method_name
        block = klass.method(method_name).to_proc
      elsif !block_given?
        raise ArgumentError, "register_type takes either 3 arguments or a block"
      end
      register_type_internal(type, klass, block)
    end

    def registered_types
      list = []
      registered_types_internal.each_pair do |type, ary|
        list << { type: type, class: ary[0], unpacker: ary[1] }
      end
      list.sort { |a, b| a[:type] <=> b[:type] }
    end

    def type_registered?(klass_or_type)
      case klass_or_type
      when Module
        registered_types.any? { |entry| klass_or_type == entry[:class] }
      when Integer
        registered_types.any? { |entry| klass_or_type == entry[:type] }
      else
        raise ArgumentError, "class or type id"
      end
    end
  end

  class Factory
    def register_type(type, klass, options = nil, **kw)
      raise FrozenError, "can't modify frozen MessagePack::Factory" if frozen?

      if options.nil? && kw.empty?
        options = { packer: :to_msgpack_ext, unpacker: :from_msgpack_ext }
      elsif !kw.empty?
        options = options ? options.merge(kw) : kw
      end

      if options
        options = options.dup
        if options[:oversized_integer_extension] && klass != Integer
          raise ArgumentError, "oversized_integer_extension is only allowed for Integer"
        end
        case packer = options[:packer]
        when nil, Proc
          # ok
        when String, Symbol
          options[:packer] = packer.to_sym.to_proc
        when Method
          options[:packer] = packer.to_proc
        else
          if packer.respond_to?(:call)
            options[:packer] = packer.method(:call).to_proc
          else
            raise ::TypeError, "expected :packer argument to be a callable object, got: #{packer.inspect}"
          end
        end

        case unpacker = options[:unpacker]
        when nil, Proc
          # ok
        when String, Symbol
          options[:unpacker] = klass.method(unpacker).to_proc
        when Method
          options[:unpacker] = unpacker.to_proc
        else
          if unpacker.respond_to?(:call)
            options[:unpacker] = unpacker.method(:call).to_proc
          else
            raise ::TypeError, "expected :unpacker argument to be a callable object, got: #{unpacker.inspect}"
          end
        end
      end

      register_type_internal(type, klass, options)
    end

    def registered_types(selector = :both)
      packer, unpacker = registered_types_internal
      list = []

      case selector
      when :both
        packer.each_pair do |klass, ary|
          type = ary[0]
          packer_proc = ary[1]
          unpacker_proc = nil
          if unpacker.has_key?(type) && unpacker[type][0] == klass
            unpacker_proc = unpacker.delete(type)[1]
          end
          list << { type: type, class: klass, packer: packer_proc, unpacker: unpacker_proc }
        end
        unpacker.each_pair do |type, ary|
          list << { type: type, class: ary[0], packer: nil, unpacker: ary[1] }
        end
      when :packer
        packer.each_pair do |klass, ary|
          list << { type: ary[0], class: klass, packer: ary[1] } if ary[1]
        end
      when :unpacker
        unpacker.each_pair do |type, ary|
          list << { type: type, class: ary[0], unpacker: ary[1] } if ary[1]
        end
      else
        raise ArgumentError, "invalid selector #{selector}"
      end

      list.sort { |a, b| a[:type] <=> b[:type] }
    end

    def type_registered?(klass_or_type, selector = :both)
      case klass_or_type
      when Module
        registered_types(selector).any? { |entry| klass_or_type <= entry[:class] }
      when Integer
        registered_types(selector).any? { |entry| klass_or_type == entry[:type] }
      else
        raise ArgumentError, "class or type id"
      end
    end

    def load(src, param = nil, **kw)
      param = param ? (kw.empty? ? param : param.merge(kw)) : (kw.empty? ? nil : kw)
      if src.is_a?(String)
        u = unpacker(param)
        u.feed(src)
      else
        u = unpacker(src, param)
      end
      u.full_unpack
    end
    alias unpack load

    def dump(v, *rest)
      p = packer(*rest)
      p.write(v)
      p.full_pack
    end
    alias pack dump

    def pool(size = 1, **options)
      Pool.new(frozen? ? self : dup.freeze, size, options.empty? ? nil : options)
    end

    class Pool
      class MemberPool
        def initialize(size, &block)
          @size = size
          @new_member = block
          @members = []
          @mutex = Mutex.new
        end

        def with
          member = @mutex.synchronize { @members.pop } || @new_member.call
          begin
            yield member
          ensure
            member.reset
            @mutex.synchronize do
              @members << member if member && @members.size < @size
            end
          end
        end
      end

      def initialize(factory, size, options = nil)
        options = nil if !options || options.empty?
        @factory = factory
        @packers = MemberPool.new(size) { factory.packer(options).freeze }
        @unpackers = MemberPool.new(size) { factory.unpacker(options).freeze }
      end

      def load(data)
        @unpackers.with do |u|
          u.feed(data)
          u.full_unpack
        end
      end

      def dump(object)
        @packers.with do |p|
          p.write(object)
          p.full_pack
        end
      end

      def unpacker(&block)
        @unpackers.with(&block)
      end

      def packer(&block)
        @packers.with(&block)
      end
    end
  end

  module CoreExt
    def to_msgpack(packer_or_io = nil)
      if packer_or_io
        if packer_or_io.is_a?(MessagePack::Packer)
          to_msgpack_with_packer(packer_or_io)
        else
          MessagePack.pack(self, packer_or_io)
        end
      else
        MessagePack.pack(self)
      end
    end
  end

  class Timestamp
    TYPE = -1
    TIMESTAMP32_MAX_SEC = (1 << 32) - 1
    TIMESTAMP64_MAX_SEC = (1 << 34) - 1

    attr_reader :sec, :nsec

    def initialize(sec, nsec)
      @sec = sec
      @nsec = nsec
    end

    def self.from_msgpack_ext(data)
      case data.bytesize
      when 4
        sec, = data.unpack("L>")
        new(sec, 0)
      when 8
        n, s = data.unpack("L>2")
        sec = ((n & 0b11) << 32) | s
        nsec = n >> 2
        new(sec, nsec)
      when 12
        nsec, sec = data.unpack("L>q>")
        new(sec, nsec)
      else
        raise MalformedFormatError, "Invalid timestamp data size: #{data.bytesize}"
      end
    end

    def self.to_msgpack_ext(sec, nsec)
      if sec >= 0 && nsec >= 0 && sec <= TIMESTAMP64_MAX_SEC
        if nsec == 0 && sec <= TIMESTAMP32_MAX_SEC
          [sec].pack("L>")
        else
          nsec30 = nsec << 2
          sec_high2 = sec >> 32
          sec_low32 = sec & 0xffffffff
          [nsec30 | sec_high2, sec_low32].pack("L>2")
        end
      else
        [nsec, sec].pack("L>q>")
      end
    end

    def to_msgpack_ext
      self.class.to_msgpack_ext(sec, nsec)
    end

    def ==(other)
      other.class == self.class && sec == other.sec && nsec == other.nsec
    end
  end

  module Time
    Unpacker = lambda do |payload|
      tv = MessagePack::Timestamp.from_msgpack_ext(payload)
      ::Time.at(tv.sec, tv.nsec, :nanosecond)
    end

    Packer = lambda do |time|
      MessagePack::Timestamp.to_msgpack_ext(time.tv_sec, time.tv_nsec)
    end
  end

  module Bigint
    CHUNK_BITLENGTH = 32
    CHUNK_FORMAT = "L>*"
    FORMAT = "C#{CHUNK_FORMAT}"
    BASE = (1 << CHUNK_BITLENGTH) - 1

    def self.to_msgpack_ext(bigint)
      members = []
      if bigint < 0
        bigint = -bigint
        members << 1
      else
        members << 0
      end
      while bigint > 0
        members << (bigint & BASE)
        bigint >>= CHUNK_BITLENGTH
      end
      members.pack(FORMAT)
    end

    def self.from_msgpack_ext(data)
      parts = data.unpack(FORMAT)
      sign = parts.shift
      sum = 0
      parts.reverse_each do |chunk|
        sum = (sum << CHUNK_BITLENGTH) | chunk
      end
      sign == 0 ? sum : -sum
    end
  end

  DefaultFactory = MessagePack::Factory.new

  def load(src, param = nil, **kw)
    param = param ? (kw.empty? ? param : param.merge(kw)) : (kw.empty? ? nil : kw)
    if src.is_a?(String)
      u = DefaultFactory.unpacker(param)
      u.feed_reference(src)
    else
      u = DefaultFactory.unpacker(src, param)
    end
    u.full_unpack
  end
  alias unpack load
  module_function :load, :unpack

  def pack(v, *rest)
    p = DefaultFactory.packer(*rest)
    p.write(v)
    p.full_pack
  end
  alias dump pack
  module_function :pack, :dump
end

class NilClass
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_nil
    packer
  end
end

class TrueClass
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_true
    packer
  end
end

class FalseClass
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_false
    packer
  end
end

class Float
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_float(self)
    packer
  end
end

class String
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_string(self)
    packer
  end
end

class Array
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_array(self)
    packer
  end
end

class Hash
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_hash(self)
    packer
  end
end

class Symbol
  include MessagePack::CoreExt

  def to_msgpack_ext
    to_s.b
  end

  def self.from_msgpack_ext(data)
    data.dup.force_encoding(Encoding::UTF_8).to_sym
  rescue EncodingError
    data.dup.force_encoding(Encoding::BINARY).to_sym
  end

  private

  def to_msgpack_with_packer(packer)
    packer.write_symbol(self)
    packer
  end
end

class Integer
  include MessagePack::CoreExt

  private

  def to_msgpack_with_packer(packer)
    packer.write_int(self)
    packer
  end
end
