# Kernel methods mruby lacks; 99_visibility.rb makes them module functions

module Kernel
  def putc(c)
    $stdout.putc(c)
  end

  def pp(*objs)
    require 'pp' unless defined?(::PP)
    objs.each { |o| $stdout.write(::PP.pp(o, +"")) }
    objs.size <= 1 ? objs.first : objs
  end

  def select(*args)
    IO.select(*args)
  end

  def syscall(*)
    raise NotImplementedError, "syscall() function is unimplemented on this machine"
  end

  def set_trace_func(proc)
    raise TypeError, "trace_func needs to be Proc" unless proc.nil? || proc.is_a?(Proc)
    proc
  end

  # trace_var records the hooks; mruby has no global variable assignment
  # hook, so they are not called
  def trace_var(name, cmd = nil, &block)
    cmd ||= block
    raise ArgumentError, "tried to create Proc object without a block" if cmd.nil?
    hooks = (::Kernel.instance_variable_get(:@__oruby_trace_var) || ::Kernel.instance_variable_set(:@__oruby_trace_var, {}))
    (hooks[name.to_sym] ||= []).unshift(cmd)
    nil
  end

  def untrace_var(name, cmd = nil)
    hooks = ::Kernel.instance_variable_get(:@__oruby_trace_var) || {}
    name = name.to_sym
    unless global_variables.include?(name) || hooks.key?(name)
      raise NameError.new("undefined global variable #{name}", name)
    end
    list = hooks[name] || []
    if cmd
      list.delete(cmd) ? [cmd] : nil
    else
      hooks.delete(name)
      list
    end
  end

  def test(cmd, file1, file2 = nil)
    cmd = cmd.ord if cmd.is_a?(String)
    cmd = Kernel.__to_int(cmd)
    path = ->(f) { f.respond_to?(:to_path) ? f.to_path : (f.is_a?(IO) ? f : String(f)) }
    a = path.(file1)
    b = file2.nil? ? nil : path.(file2)
    case cmd.chr
    when "A" then File.atime(a)
    when "b" then File.blockdev?(a)
    when "c" then File.chardev?(a)
    when "C" then File.ctime(a)
    when "d" then File.directory?(a)
    when "e" then File.exist?(a)
    when "f" then File.file?(a)
    when "g" then File.setgid?(a)
    when "G" then File.grpowned?(a)
    when "k" then File.sticky?(a)
    when "l" then File.symlink?(a)
    when "M" then File.mtime(a)
    when "o" then File.owned?(a)
    when "O" then File.owned?(a)
    when "p" then File.pipe?(a)
    when "r" then File.readable?(a)
    when "R" then File.readable_real?(a)
    when "s" then File.size?(a)
    when "S" then File.socket?(a)
    when "u" then File.setuid?(a)
    when "w" then File.writable?(a)
    when "W" then File.writable_real?(a)
    when "x" then File.executable?(a)
    when "X" then File.executable_real?(a)
    when "z" then File.zero?(a)
    when "=" then File.mtime(a) == File.mtime(b)
    when "<" then File.mtime(a) < File.mtime(b)
    when ">" then File.mtime(a) > File.mtime(b)
    when "-" then File.identical?(a, b)
    else
      raise ArgumentError, "unknown command '#{cmd.chr}'"
    end
  end

  private def initialize_dup(orig)
    initialize_copy(orig)
    self
  end

  private def initialize_clone(orig, freeze: nil)
    unless freeze.nil? || freeze == true || freeze == false
      raise ArgumentError, "unexpected value for freeze: #{freeze.class}"
    end
    initialize_copy(orig)
    self
  end
end
