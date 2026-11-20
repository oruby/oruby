# MRI argument and state checks mruby's Ruby-level and C methods lack (more
# in go-mrb.c's chk_defs, which front C methods without a Ruby frame).
class String
  alias __chk_force_encoding force_encoding
  def force_encoding(enc)
    raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
    __chk_force_encoding(enc)
  end

  alias __chk_slice_bang slice!
  def slice!(*args)
    raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
    __chk_slice_bang(*args)
  end
end

class Integer
  alias __chk_pow pow
  def pow(b, m = (no_m = true; nil))
    return self ** b if no_m
    unless Integer === b
      raise TypeError, "Integer#pow() 2nd argument not allowed unless a 1st argument is integer" if Numeric === b
    end
    unless Integer === m
      raise TypeError, "Integer#pow() 2nd argument not allowed unless all arguments are integers" if Numeric === m
    end
    if Integer === b && b < 0
      raise RangeError, "Integer#pow() 1st argument cannot be negative when 2nd argument specified"
    end
    __chk_pow(b, m)
  end
end

class NilClass
  def rationalize(eps = (no_eps = true; nil))
    0r
  end
end

module Comparable
  # rb_cmperr: a special constant or Float other is shown by #inspect
  def self.__cmperr(a, b)
    desc = (nil.equal?(b) || true.equal?(b) || false.equal?(b) || Integer === b || Float === b || Symbol === b) ? b.inspect : b.class
    raise ArgumentError, "comparison of #{a.class} with #{desc} failed"
  end

  # rb_cmpint: a non-Integer result of <=> is placed by comparing it with 0
  def self.__cmpint(c, a, b)
    return c if Integer === c
    __cmperr(a, b) if nil.equal?(c)
    return 1 if c > 0
    return -1 if c < 0
    0
  end

  def ==(other)
    return true if equal?(other)
    c = (self <=> other)
    return false if nil.equal?(c)
    Comparable.__cmpint(c, self, other) == 0
  end

  def <(other)
    c = (self <=> other)
    return c < 0 if Integer === c
    Comparable.__cmpint(c, self, other) < 0
  end

  def <=(other)
    c = (self <=> other)
    return c <= 0 if Integer === c
    Comparable.__cmpint(c, self, other) <= 0
  end

  def >(other)
    c = (self <=> other)
    return c > 0 if Integer === c
    Comparable.__cmpint(c, self, other) > 0
  end

  def >=(other)
    c = (self <=> other)
    return c >= 0 if Integer === c
    Comparable.__cmpint(c, self, other) >= 0
  end
end

class Method
  # the arity checked against the method's own parameters (its to_proc is
  # not always a lambda of the same arity)
  def curry(arity = nil)
    return to_proc.curry if nil.equal?(arity)
    req = opt = 0
    rest = false
    parameters.each do |kind, _|
      case kind
      when :req then req += 1
      when :opt then opt += 1
      when :rest then rest = true
      end
    end
    if arity < req || (!rest && arity > req + opt)
      expected = rest ? "#{req}+" : (opt.zero? ? req.to_s : "#{req}..#{req + opt}")
      raise ArgumentError, "wrong number of arguments (given #{arity}, expected #{expected})"
    end
    to_proc.curry(arity)
  end
end
