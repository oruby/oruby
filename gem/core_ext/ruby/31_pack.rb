# Array#pack, String#unpack and String#unpack1 (the work is in pack.go)
module Kernel
  class << self
    # rb_to_float for pack's float directives
    def __pack_float(obj)
      case obj
      when Float then obj
      when nil, true, false then raise TypeError, "can't convert #{obj.inspect} into Float"
      when String then raise TypeError, "no implicit conversion to float from string"
      when Numeric
        raise TypeError, "can't convert #{__cname(obj)} into Float" unless obj.respond_to?(:to_f)
        obj.to_f
      else raise TypeError, "can't convert #{__cname(obj)} into Float"
      end
    end

    # the strings 'p'/'P' pointers in a packed string point to, as MRI
    # associates them with the string (held for the life of the process)
    def __pack_associate(str, targets)
      t = instance_variable_get(:@__oruby_pack_ptrs) || instance_variable_set(:@__oruby_pack_ptrs, {}.compare_by_identity)
      (t[str] ||= []).concat(targets)
      nil
    end

    def __pack_associated(str)
      t = instance_variable_get(:@__oruby_pack_ptrs)
      t && t[str]
    end

    # 'M' packs obj.to_s
    def __pack_to_s(obj)
      s = obj.to_s
      String === s ? s : ::Kernel.instance_method(:to_s).bind_call(obj)
    end
  end
end

class Array
  def pack(fmt, buffer: nil)
    fmt = Kernel.__implicit(fmt, String)
    unless buffer.nil?
      raise TypeError, "buffer must be String, not #{Kernel.__cname(buffer)}" unless String === buffer
      raise FrozenError.new("can't modify frozen String: #{buffer.inspect}", receiver: buffer) if buffer.frozen?
    end
    __oruby_pack(fmt, buffer)
  end
end

class String
  def unpack(fmt, offset: 0, &block)
    fmt = Kernel.__implicit(fmt, String)
    offset = __oruby_unpack_offset(offset)
    result = __oruby_unpack(fmt, offset, false)
    return result unless block
    result.each(&block)
    nil
  end

  def unpack1(fmt, offset: 0)
    fmt = Kernel.__implicit(fmt, String)
    __oruby_unpack(fmt, __oruby_unpack_offset(offset), true)[0]
  end

  # a negative offset counts from the end
  private def __oruby_unpack_offset(offset)
    offset = Kernel.__to_int(offset)
    offset += bytesize if offset < 0
    raise ArgumentError, "offset outside of string" if offset < 0 || offset > bytesize
    offset
  end
end
