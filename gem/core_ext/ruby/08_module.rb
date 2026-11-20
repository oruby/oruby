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
    if name.is_a?(String) && name.start_with?("::")
      raise NameError.new("wrong constant name #{name}", name.to_sym, receiver: self) if name == "::"
      return Object.__const_get(name[2..], inherit)
    end
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
    pid = Kernel.__to_int(pid)
    # a pid that is not a child (ECHILD) gives nil, as MRI's detach thread
    t = Thread.new do
      begin
        Process.wait2(pid)[1]
      rescue Errno::ECHILD
        nil
      end
    end
    t[:pid] = pid
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

class Module
  # Where a constant was defined: [file, line], [] for one defined in C or
  # Go, nil when the search finds none. go-mrb.c's const_added records it.
  def const_source_location(name, inherit = true)
    name = Kernel.__implicit(name, String) unless Symbol === name || String === name
    mod = self
    if Symbol === name
      sym = Module.__oruby_const_name(name.to_s)
    else
      path = name
      if path.start_with?("::")
        mod = Object
        path = path[2..]
      end
      parts = path.split("::", -1)
      raise NameError, "wrong constant name #{name}" if parts.empty? || parts.include?("")
      sym = Module.__oruby_const_name(parts.pop, name)
      parts.each { |pt| mod = mod.const_get(Module.__oruby_const_name(pt, name), inherit) }
    end
    list = inherit ? mod.ancestors : [mod]
    # a module's search ends at the top level, as a class's does through Object
    list += Object.ancestors if inherit && !(Class === mod)
    list.each do |m|
      return m.__send__(:__oruby_const_location, sym) || [] if m.const_defined?(sym, false)
      # an autoload whose file is loading: mruby's const_defined? is false
      loc = m.__send__(:__oruby_const_location, sym)
      return loc if loc
    end
    nil
  end

  def self.__oruby_const_name(s, full = s)
    raise NameError.new("wrong constant name #{full}", full) unless s.match?(/\A[A-Z]\w*\z/)
    s.to_sym
  end
end
