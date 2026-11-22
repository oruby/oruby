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

module Kernel
  # rb_check_funcall: a method obj answers, by its respond_to? when that is
  # its own, else by its methods, or through its own method_missing
  def self.__oruby_check_call(obj, meth)
    ro = ::Kernel.instance_method(:respond_to?)
    if ::Kernel === obj
      own_rt = obj.method(:respond_to?).owner != ::Kernel rescue false
      if own_rt
        return [false, nil] unless obj.respond_to?(meth, true)
        begin
          return [true, obj.__send__(meth)]
        rescue ::NoMethodError => e
          raise unless e.name == meth
          return [false, nil]
        end
      end
      return [true, obj.__send__(meth)] if ro.bind_call(obj, meth, true)
    elsif __responds?(obj, meth)
      return [true, obj.__send__(meth)]
    end
    mm = __oruby_lookup_class(obj).instance_method(:method_missing) rescue nil
    return [false, nil] if mm.nil? || mm.owner == ::BasicObject
    begin
      [true, obj.__send__(meth)]
    rescue ::NoMethodError => e
      raise unless e.name == meth
      [false, nil]
    end
  end

  # Kernel#String as MRI's rb_String: #to_str, else #to_s
  def String(obj)
    return obj if ::String === obj
    s = Kernel.__check_implicit(obj, ::String)
    return s unless nil.equal?(s)
    ok, r = Kernel.__oruby_check_call(obj, :to_s)
    name = Kernel.__cname(obj)
    raise TypeError, "can't convert #{name} into String" unless ok
    raise TypeError, "can't convert #{name} into String (#{name}#to_s gives #{Kernel.__cname(r)})" unless ::String === r
    r
  end

  # Kernel#Hash as MRI's rb_Hash: nil and [] are {}, else #to_hash
  def Hash(obj)
    return {} if nil.equal?(obj) || (::Array === obj && obj.empty?)
    return obj if ::Hash === obj
    h = Kernel.__check_implicit(obj, ::Hash)
    raise TypeError, "can't convert #{Kernel.__cname(obj)} into Hash" if nil.equal?(h)
    h
  end
  module_function :String, :Hash
end

module Kernel
  # methods(false) is singleton_methods(false); public_methods(false) and
  # the like are the singleton class's, its extended modules' and the
  # object's class's own (class_instance_method_list without recursion)
  alias __oruby_methods methods
  def methods(all = true)
    return __oruby_methods if all
    singleton_methods(false)
  end

  %i[public private protected].each do |vis|
    orig = :"__oruby_#{vis}_methods"
    alias_method orig, :"#{vis}_methods"
    lister = :"#{vis}_instance_methods"
    define_method(:"#{vis}_methods") do |all = true|
      return __send__(orig, all) if all
      out = []
      Kernel.instance_method(:singleton_class).bind_call(self).ancestors.each do |m|
        out.concat(m.__send__(lister, false))
        break if Class === m && !m.singleton_class?
      end
      out.uniq
    end
  end
end

module Kernel
  # MRI's rb_io_print / rb_io_puts on a $stdout that is not an IO: its
  # #write takes the pieces (goprint.go calls these)
  def self.__oruby_io_print(out, *args)
    args = [$_] if args.empty?
    ofs = $,
    args.each_with_index do |a, i|
      out.write(ofs) if i > 0 && ofs
      out.write(a)
    end
    out.write($\) if $\
    nil
  end

  def self.__oruby_io_puts(out, *args)
    return out.write("\n") && nil if args.empty?
    seen = {}
    line = lambda do |a|
      if ::String === a
        s = a
      elsif ::Array === a || (s = Kernel.__check_implicit(a, ::Array))
        a = s if s
        if seen[a.__id__]
          out.write("[...]\n")
        else
          seen[a.__id__] = true
          a.each { |e| line.(e) }
          seen.delete(a.__id__)
        end
        next
      else
        s = a.to_s
        s = ::Kernel.instance_method(:to_s).bind_call(a) unless ::String === s
      end
      out.write(s)
      out.write("\n") unless s.end_with?("\n")
    end
    args.each { |a| line.(a) }
    nil
  end
end

module Kernel
  # yield_self/then without a block: an Enumerator of size 1
  alias __oruby_yield_self yield_self
  def yield_self(&block)
    return to_enum(:yield_self) { 1 } unless block
    __oruby_yield_self(&block)
  end
  alias then yield_self
end

# A class is not included, prepended nor extended: Class has no such hooks
class Class
  %i[append_features prepend_features extend_object].each do |m|
    undef_method(m) if method_defined?(m) || private_method_defined?(m)
  end
end

# A refinement is not included, prepended nor extended (MRI undefines the
# hooks); refined_class is gone since Ruby 3.4
if defined?(Refinement)
  class Refinement
    %i[append_features prepend_features extend_object refined_class].each do |m|
      undef_method(m) if method_defined?(m) || private_method_defined?(m)
    end
  end
end

# Module's append_features / prepend_features / extend_object need a
# Module receiver that is not a Class (MRI raises when called so bound)
class UnboundMethod
  alias __oruby_chk_bind bind
  def bind(obj)
    if ::Class === obj && owner.equal?(::Module) && %i[append_features prepend_features extend_object].include?(name)
      raise TypeError, "wrong argument type Class (expected Module)"
    end
    __oruby_chk_bind(obj)
  end
end

# main's define_method makes a public method of Object, its ruby2_keywords
# is Object's (MRI's main has both as private singleton methods)
class << self
  private

  def define_method(*args, &block)
    name = Object.__send__(:define_method, *args, &block)
    Object.__send__(:public, name)
    name
  end

  def ruby2_keywords(*names)
    Object.__send__(:ruby2_keywords, *names)
  end
end

class Module
  # Ruby 4.1: method_defined?(name, inherit = true, include_private = false)
  alias __oruby_method_defined? method_defined?
  def method_defined?(name, inherit = true, include_private = false)
    return true if __oruby_method_defined?(name, inherit)
    include_private ? private_method_defined?(name, inherit) : false
  end
end


module Kernel
  # case/in without a matching clause nor else (prism.go's ORB_CASE_IN)
  def self.__oruby_nomatch(v)
    raise NoMatchingPatternError, v.inspect
  end

  # ... whose one clause is a Hash pattern of keys: MRI's single pattern
  # messages, a missing key a NoMatchingPatternKeyError
  def self.__oruby_nomatch_key(v, keys)
    unless v.respond_to?(:deconstruct_keys)
      raise NoMatchingPatternError, "#{v.inspect}: #{v.inspect} does not respond to #deconstruct_keys"
    end
    h = v.deconstruct_keys(keys)
    if ::Hash === h
      keys.each do |k|
        next if h.key?(k)
        raise NoMatchingPatternKeyError.new("#{v.inspect}: key not found: #{k.inspect}", matchee: h, key: k)
      end
    end
    raise NoMatchingPatternError, v.inspect
  end
end

class NoMatchingPatternKeyError
  def initialize(message = nil, matchee: (no_matchee = true; nil), key: (no_key = true; nil))
    super(message)
    @matchee = matchee unless no_matchee
    @key = key unless no_key
  end

  def matchee
    raise ArgumentError, "no matchee is available" unless instance_variable_defined?(:@matchee)
    @matchee
  end

  def key
    raise ArgumentError, "no key is available" unless instance_variable_defined?(:@key)
    @key
  end
end

class Hash
  # a hash pattern's keys, read with the #key? and #[] a subclass of Hash
  # may have its own (mruby's __pat_values reads the table)
  alias __oruby_pat_values __pat_values
  def __pat_values(keys)
    return __oruby_pat_values(keys) if ::Hash.equal?(::Kernel.instance_method(:class).bind_call(self))
    keys.map { |k| return false unless key?(k); self[k] }
  end
end

class Binding
  # numbered parameters and `it` are no local variables of a binding
  %i[local_variable_get local_variable_defined? local_variable_set].each do |m|
    orig = :"__oruby_#{m.to_s.delete('?')}"
    alias_method orig, m
    define_method(m) do |name, *rest|
      sym = Symbol === name ? name : (String === name ? name.to_sym : nil)
      sym = Kernel.__implicit(name, String).to_sym if sym.nil? && !nil.equal?(name) && Kernel.__responds?(name, :to_str)
      name = sym if sym
      if Symbol === sym
        s = sym.name
        if s.size == 2 && s[0] == "_" && s[1] >= "1" && s[1] <= "9"
          raise NameError.new("numbered parameter '#{s}' is not a local variable", sym)
        end
        if sym == :it && m != :local_variable_set && !local_variables.include?(:it)
          raise NameError.new("local variable 'it' is not defined for #{inspect}", sym) if m == :local_variable_get
          return false
        end
      end
      __send__(orig, name, *rest)
    end
  end
end

module Kernel
  # TOPLEVEL_BINDING is the main script's binding, as MRI's: cmd/oruby has
  # the script hand its own over first (variables set in it before, by
  # required files, are kept)
  def self.__oruby_main_binding(b)
    tb = ::TOPLEVEL_BINDING
    old = tb.local_variables.map { |n| [n, tb.local_variable_get(n)] }
    tb.__send__(:__oruby_adopt, b)
    old.each { |n, v| tb.local_variable_set(n, v) unless tb.local_variable_defined?(n) }
    nil
  end
end
