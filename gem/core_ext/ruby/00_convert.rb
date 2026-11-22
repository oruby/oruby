# Implicit conversion (to_int, to_str, to_ary, to_hash, ...) as MRI's
# rb_convert_type / rb_check_convert_type do it, with MRI 4.1 messages.
# mruby core never calls these conversion methods; core_ext uses the helpers
# below in Ruby-level wrappers around the methods that need them.
module Kernel
  class << self
    # conversion method for a target type
    def __conv_method(klass)
      if klass == Integer then :to_int
      elsif klass == String then :to_str
      elsif klass == Array then :to_ary
      elsif klass == Hash then :to_hash
      elsif klass == Symbol then :to_sym
      elsif klass == Proc then :to_proc
      elsif klass == Regexp then :to_regexp
      elsif klass == Float then :to_f
      else :to_io
      end
    end

    # name MRI uses for obj in conversion errors
    def __cname(obj)
      case obj
      when nil then "nil"
      when true then "true"
      when false then "false"
      else ::Kernel.instance_method(:class).bind_call(obj)
      end
    end

    def __responds?(obj, meth)
      if ::Kernel === obj
        obj.respond_to?(meth, true)
      else # BasicObject: no respond_to?
        klass = __oruby_lookup_class(obj)
        klass.method_defined?(meth) || klass.private_method_defined?(meth)
      end
    end

    # rb_convert_type: obj converted to klass or TypeError
    def __implicit(obj, klass, meth = __conv_method(klass))
      return obj if klass === obj
      if !nil.equal?(obj) && __responds?(obj, meth)
        r = obj.__send__(meth)
        return r if klass === r
        raise TypeError, "can't convert #{__cname(obj)} into #{klass} (#{__cname(obj)}##{meth} gives #{__cname(r)})"
      end
      raise TypeError, "no implicit conversion of #{__cname(obj)} into #{klass}"
    end

    # rb_check_convert_type: converted obj, or nil when obj has no conversion
    def __check_implicit(obj, klass, meth = __conv_method(klass))
      return obj if klass === obj
      return nil if nil.equal?(obj) || !__responds?(obj, meth)
      r = obj.__send__(meth)
      return r if klass === r || nil.equal?(r)
      raise TypeError, "can't convert #{__cname(obj)} into #{klass} (#{__cname(obj)}##{meth} gives #{__cname(r)})"
    end

    # rb_num2long: Integer, Float (truncated) or #to_int
    def __to_int(obj)
      case obj
      when Integer then obj
      when Float
        raise FloatDomainError, obj.to_s unless obj.finite?
        obj.to_i
      else __implicit(obj, Integer)
      end
    end
  end
end

class Module
  # Wraps instance method +name+ so positional argument i is converted to
  # types[i] (via to_int / to_str / to_ary / to_hash) when it isn't one already
  # and responds to the conversion; other arguments pass through unchanged, so
  # the original method still raises its own errors. A compiler can drop the
  # wrapper wherever argument types are known.
  private def __implicit_args(name, *types)
    return unless method_defined?(name) || private_method_defined?(name)
    # a C method is wrapped in C (go-fast.c): no Ruby frame on its calls
    return if __oruby_implicit_c(name, types)
    um = instance_method(name)
    vis = if private_method_defined?(name) then :private
          elsif protected_method_defined?(name) then :protected
          else :public
          end
    n = types.size
    define_method(name) do |*args, **kw, &blk|
      i = 0
      while i < n && i < args.size
        t = types[i]
        a = args[i]
        if t && !nil.equal?(a) && !(t === a)
          if t == Integer && Range === a
            # a Range of other bounds: each by #to_int (rb_range_beg_len)
            b = a.begin
            e = a.end
            unless (nil.equal?(b) || Integer === b) && (nil.equal?(e) || Integer === e)
              b = Kernel.__to_int(b) unless nil.equal?(b)
              e = Kernel.__to_int(e) unless nil.equal?(e)
              args[i] = Range.new(b, e, a.exclude_end?)
            end
          else
            c = Kernel.__check_implicit(a, t)
            args[i] = c unless nil.equal?(c)
          end
        end
        i += 1
      end
      kw.empty? ? um.bind_call(self, *args, &blk) : um.bind_call(self, *args, **kw, &blk)
    end
    __send__(vis, name)
  end
end
