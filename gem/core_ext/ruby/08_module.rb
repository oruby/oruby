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

class IO
  include File::Constants
end

class Module
  # mruby rejects a path rooted at Object ("::String")
  alias_method :__const_get, :const_get
  def const_get(name, inherit = true)
    return Object.__const_get(name[2..], inherit) if name.is_a?(String) && name.start_with?("::")
    __const_get(name, inherit)
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

class Module
  # a module may alias a method found only in Object, as in MRI through 4.1
  alias __alias_method alias_method
  def alias_method(new_name, old_name)
    __alias_method(new_name, old_name)
  rescue NameError => e
    raise unless e.name == old_name.to_sym
    return new_name.to_sym if instance_of?(Module) && __alias_from_object(new_name, old_name)
    kind = instance_of?(Module) ? "module" : "class"
    raise NameError.new("undefined method '#{old_name}' for #{kind} '#{self}'", old_name.to_sym)
  end
end

class Module
  # Ruby 4.1: the modules and classes that include, prepend or inherit self
  def descendants
    found = []
    ObjectSpace.each_object(Module) do |m|
      next if m.equal?(self) || m.singleton_class? || (defined?(Refinement) && Refinement === m)
      found << m if m.ancestors.include?(self) && !m.equal?(self)
    end
    found
  end

  # Ruby 4.1: autoload with a path relative to the calling file
  def autoload_relative(name, path)
    autoload(name, Module.__oruby_relative_path(path, caller_locations(1, 1)[0]))
  end

  def self.__oruby_relative_path(path, loc)
    path = path.respond_to?(:to_path) ? path.to_path : Kernel.__implicit(path, String)
    file = loc && loc.path
    if file.nil? || file.start_with?("(eval") || file == "-e"
      raise LoadError, "autoload_relative called without file context"
    end
    File.expand_path(path, File.dirname(File.expand_path(file)))
  end
end

module Kernel
  def autoload_relative(name, path)
    Object.autoload(name, Module.__oruby_relative_path(path, caller_locations(1, 1)[0]))
  end
end
