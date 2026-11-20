# Method#inspect / UnboundMethod#inspect as MRI's method_inspect:
#   #<Method: Klass(Owner)#name(orig)(params) file:line>
module Kernel
  def self.__oruby_method_params(params)
    forwarding = params.any? { |k, n| k == :rest && n == :* } &&
                 params.any? { |k, n| k == :keyrest && n == :** } &&
                 params.any? { |k, n| k == :block && n == :& }
    parts = []
    params.each do |kind, name|
      case kind
      when :req then parts << (name || "_").to_s
      when :opt then parts << "#{name || '_'}=..."
      when :keyreq then parts << "#{name}:"
      when :key then parts << "#{name}: ..."
      when :rest
        next if forwarding
        parts << (name.nil? || name == :* ? "*" : "*#{name}")
      when :keyrest
        next if forwarding
        parts << (name.nil? || name == :** ? "**" : "**#{name}")
      when :block
        if forwarding then parts << "..."
        else parts << (name.nil? || name == :& ? "&" : "&#{name}")
        end
      when :nokey then parts << "**nil"
      end
    end
    "(#{parts.join(', ')})"
  end

  def self.__oruby_method_inspect(m, unbound)
    owner = m.owner
    name = m.name
    s = +"#<#{::Kernel.instance_method(:class).bind_call(m)}: "
    sharp = "#"
    if owner.singleton_class?
      att = owner.attached_object
      if unbound
        s << owner.inspect
      else
        recv = m.receiver
        if recv.equal?(att)
          s << recv.inspect
        else
          s << "#{recv.inspect}(#{att.inspect})"
        end
        sharp = "."
      end
    else
      mklass = unbound ? owner : m.__send__(:__oruby_mklass)
      mklass = owner unless Module === mklass
      if mklass.singleton_class?
        att = mklass.attached_object
        mklass = mklass.superclass while mklass.singleton_class? && !(Module === att)
      end
      s << mklass.inspect
      s << "(#{owner.inspect})" unless mklass.equal?(owner)
    end
    s << sharp << name.to_s
    orig = m.original_name
    s << "(#{orig})" if orig != name
    s << __oruby_method_params(m.parameters)
    loc = m.source_location
    s << " #{loc[0]}:#{loc[1]}" if loc
    s << ">"
  end
end

class Method
  def inspect = Kernel.__oruby_method_inspect(self, false)
  alias to_s inspect
end

class UnboundMethod
  def inspect = Kernel.__oruby_method_inspect(self, true)
  alias to_s inspect
end

# a C method's arity from its parameters where mruby says -1 (attr_writer's
# setter takes one)
class Method
  alias __oruby_mrb_arity arity
  def arity
    a = __oruby_mrb_arity
    return a unless a == -1 && source_location.nil?
    ps = parameters
    ps.all? { |k, _| k == :req } ? ps.size : a
  end
end

class UnboundMethod
  alias __oruby_mrb_arity arity
  def arity
    a = __oruby_mrb_arity
    return a unless a == -1 && source_location.nil?
    ps = parameters
    ps.all? { |k, _| k == :req } ? ps.size : a
  end
end


# mruby's own Ruby-defined core methods (mrblib/, mrbgems/) are MRI's
# <internal:...> files
module Kernel
  def self.__oruby_internal_loc(loc)
    return loc unless ::Array === loc && ::String === (f = loc[0])
    return loc unless f.start_with?("mrblib/", "mrbgems/")
    ["<internal:#{f}>", loc[1]]
  end
end

[Method, UnboundMethod, Proc].each do |k|
  k.class_eval do
    alias_method :__oruby_raw_source_location, :source_location
    def source_location = Kernel.__oruby_internal_loc(__oruby_raw_source_location)
  end
end

# Proc#curry: MRI's curried procs are C level functions: no binding, no
# source location, parameters [[:rest]]. The curry is mruby's; the proc it
# returns is marked (not the partial applications it makes later).
class Proc
  alias __oruby_curry_raw curry
  def curry(arity = (no_arity = true; nil))
    c = no_arity ? __oruby_curry_raw : __oruby_curry_raw(arity)
    Proc.__oruby_mark_curried(c)
  end

  def self.__oruby_mark_curried(c)
    c.instance_variable_set(:@__oruby_curried, true) if Proc === c
    c
  end


  alias __oruby_binding_raw binding
  def binding
    raise ArgumentError, "Can't create Binding from C level function" if @__oruby_curried
    __oruby_binding_raw
  end
end
