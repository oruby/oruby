class Method
  def curry(arity = nil)
    if arity
      to_proc.curry(arity)
    else
      to_proc.curry
    end
  end

  # original_name is go-mrb.c's
end

class UnboundMethod
  def bind_call(obj, *args, &blk)
    bind(obj).call(*args, &blk)
  end
end

# Ruby 4.0's implicit block parameters (it, _1.._9) of a binding's own scope
# (go-mrb.c's __oruby_implicit_params; a parent's or nested block's are not)
class Binding
  def implicit_parameters = __oruby_implicit_params

  def implicit_parameter_defined?(name)
    __oruby_implicit_params.include?(Binding.__oruby_implicit_name(name))
  end

  def implicit_parameter_get(name)
    sym = Binding.__oruby_implicit_name(name)
    unless __oruby_implicit_params.include?(sym)
      raise NameError.new("implicit parameter '#{sym}' is not defined for #{inspect}", sym)
    end
    sym == :it ? __oruby_it : (respond_to?(:__oruby_local_variable_get, true) ? __oruby_local_variable_get(sym) : local_variable_get(sym))
  end

  def self.__oruby_implicit_name(name)
    sym = case name
          when Symbol then name
          when String then name.to_sym
          else
            raise TypeError, "#{name.inspect} is not a symbol nor a string" unless Kernel.__responds?(name, :to_str)
            Kernel.__implicit(name, String).to_sym
          end
    s = sym.to_s
    unless s == "it" || (s.size == 2 && s.getbyte(0) == 95 && s.getbyte(1).between?(49, 57))
      raise NameError.new("'#{sym}' is not an implicit parameter", sym)
    end
    sym
  end
end

# MRI's composition: f << g is a lambda when g is (any callable but a Proc
# counting as one), f >> g when f is; anything that is not callable is a
# TypeError (no #to_proc coercion)
class Proc
  def <<(g)
    Proc.__oruby_callable!(g)
    f = self
    if Proc === g ? g.lambda? : true
      ->(*a, **kw, &b) { f.call(g.call(*a, **kw, &b)) }
    else
      proc { |*a, **kw, &b| f.call(g.call(*a, **kw, &b)) }
    end
  end

  def >>(g)
    Proc.__oruby_callable!(g)
    f = self
    if lambda?
      ->(*a, **kw, &b) { g.call(f.call(*a, **kw, &b)) }
    else
      proc { |*a, **kw, &b| g.call(f.call(*a, **kw, &b)) }
    end
  end

  def self.__oruby_callable!(g)
    return if Proc === g || Method === g || g.respond_to?(:call)
    raise TypeError, "callable object is expected"
  end
end

class Method
  def <<(g) = to_proc << g
  def >>(g) = to_proc >> g
end

# MRI's prelude: binding.irb loads IRB, whose Binding#irb replaces this one.
# IRB is a bundled gem: where RubyGems was not loaded at startup (the mspec
# runner's children), it is loaded to find it.
class Binding
  def irb
    begin
      require 'irb'
    rescue LoadError
      raise if defined?(::Gem)
      require 'rubygems'
      require 'irb'
    end
    irb
  end

  # suppress redefinition warning
  alias irb irb
end
