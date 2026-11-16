class Module
  # Constant visibility is recorded, but mruby resolves Mod::CONST in C,
  # so private constants stay accessible.
  def private_constant(*names)
    __aot_check_constants(names)
    (@__private_constants ||= {}).tap { |h| names.each { |n| h[n.to_sym] = true } }
    nil
  end

  def public_constant(*names)
    __aot_check_constants(names)
    names.each { |n| @__private_constants.delete(n.to_sym) } if @__private_constants
    nil
  end

  def deprecate_constant(*names)
    __aot_check_constants(names)
    self
  end

  private def __aot_check_constants(names)
    names.each do |n|
      raise NameError.new("constant #{self}::#{n} not defined", n.to_sym) unless const_defined?(n, false)
    end
  end
end

module Kernel
  def fork(*)
    raise NotImplementedError, "fork() function is unimplemented on this machine"
  end
  module_function :fork
end

module Process
  def self.fork(*)
    raise NotImplementedError, "fork() function is unimplemented on this machine"
  end
end

module Kernel
  # pp library protocol (used by mspec for failure messages)
  def pretty_inspect
    inspect + "\n"
  end
end

if defined?(File::Constants) && !IO.include?(File::Constants)
  class IO
    include File::Constants
  end
end

class Module
  unless method_defined?(:private_class_method)
    def private_class_method(*names)
      names.flatten.each { |n| singleton_class.__send__(:private, n.to_sym) }
      nil
    end

    def public_class_method(*names)
      names.flatten.each { |n| singleton_class.__send__(:public, n.to_sym) }
      nil
    end
  end
end

class Module
  # method_defined?(name, inherit = true), and the visibility-specific forms
  alias_method :__method_defined?, :method_defined?
  def method_defined?(name, inherit = true)
    return __method_defined?(name) if inherit
    instance_methods(false).include?(name.to_sym)
  end

  unless method_defined?(:public_method_defined?)
    def public_method_defined?(name, inherit = true)
      public_instance_methods(inherit).include?(name.to_sym)
    end
  end

  unless method_defined?(:private_method_defined?)
    def private_method_defined?(name, inherit = true)
      private_instance_methods(inherit).include?(name.to_sym)
    end
  end

  unless method_defined?(:protected_method_defined?)
    def protected_method_defined?(name, inherit = true)
      protected_instance_methods(inherit).include?(name.to_sym)
    end
  end
end

class Module
  # const_get(name, inherit = true), including "A::B" paths
  alias_method :__const_get, :const_get
  def const_get(name, inherit = true)
    if name.is_a?(String) && name.include?("::")
      parts = name.split("::")
      mod = parts.first.empty? ? (parts.shift; Object) : self
      return parts.inject(mod) { |m, part| m.const_get(part, inherit) }
    end
    if inherit
      # modules also see Object's constants, as in MRI
      if !is_a?(Class) && !const_defined?(name) && Object.const_defined?(name)
        return Object.__const_get(name)
      end
      __const_get(name)
    elsif const_defined?(name, false)
      __const_get(name)
    else
      raise NameError.new("uninitialized constant #{self == Object ? '' : "#{self}::"}#{name}", name.to_sym)
    end
  end
end

class LoadError < ScriptError
  # the feature that failed to load (set by require/load)
  attr_accessor :path
end

module Process
  # Process.detach returns a Thread whose value is the Process::Status
  def self.detach(pid)
    pid = Integer(pid)
    t = Thread.new { Process.wait2(pid)[1] }
    t.define_singleton_method(:pid) { pid }
    t
  end
end
