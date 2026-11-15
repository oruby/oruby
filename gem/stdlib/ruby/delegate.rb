class Delegator < BasicObject
  kernel = ::Kernel.dup
  kernel.class_eval do
    alias __raise__ raise
    [:to_s, :inspect, :=~, :!~, :===, :<=>, :hash].each do |m|
      undef_method m rescue nil
    end
    private_instance_methods.each do |m|
      if /\Abind_call\z|\Ainstance_variable_|\Aobject_id\z|\Ablock_given\?\z|\A__/ =~ m.to_s
        next
      end
      undef_method m rescue nil
    end
  end
  include kernel

  def self.const_missing(n)
    ::Object.const_get(n)
  end

  def initialize(obj)
    __setobj__(obj)
  end

  def method_missing(m, *args, **kw, &block)
    r = true
    target = __getobj__ { r = false }
    if r && target_respond_to?(target, m, false)
      kw.empty? ? target.__send__(m, *args, &block) : target.__send__(m, *args, **kw, &block)
    elsif ::Kernel.method_defined?(m) || ::Kernel.private_method_defined?(m)
      ::Kernel.instance_method(m).bind_call(self, *args, &block)
    else
      super(m, *args, &block)
    end
  end

  def target_respond_to?(target, m, include_private)
    target.respond_to?(m, include_private)
  end

  def respond_to_missing?(m, include_private)
    r = true
    target = __getobj__ { r = false }
    r &&= target_respond_to?(target, m, include_private)
    if r && include_private && !target_respond_to?(target, m, false)
      warn "delegator does not forward private method \##{m}", uplevel: 3
      return false
    end
    r
  end

  def respond_to?(m, include_private = false)
    return true if [:__getobj__, :__setobj__, :method_missing].include?(m.to_sym)
    ::Kernel.instance_method(:respond_to?).bind_call(self, m, include_private) || respond_to_missing?(m, include_private)
  end

  def ==(obj)
    return true if obj.equal?(self)
    __getobj__ == obj
  end

  def !=(obj)
    return false if obj.equal?(self)
    __getobj__ != obj
  end

  def eql?(obj)
    return true if obj.equal?(self)
    obj.eql?(__getobj__)
  end

  def !
    !__getobj__
  end

  def methods(all = true)
    __getobj__.methods(all) | super
  end

  def public_methods(all = true)
    __getobj__.public_methods(all) | super
  end

  def protected_methods(all = true)
    __getobj__.protected_methods(all) | super
  end

  def freeze
    __getobj__.freeze
    super()
  end

  def frozen?
    ::Kernel.instance_method(:frozen?).bind_call(self)
  end

  def marshal_dump
    ivars = instance_variables.reject { |var| /\A@delegate_/ =~ var.to_s }
    [:__v2__, ivars, ivars.map { |var| instance_variable_get(var) }, __getobj__]
  end

  def marshal_load(data)
    version, vars, values, obj = data
    if version == :__v2__
      vars.each_with_index { |var, i| instance_variable_set(var, values[i]) }
      __setobj__(obj)
    else
      __setobj__(data)
    end
  end

  def initialize_clone(obj, freeze: nil)
    __setobj__(obj.__getobj__.clone(freeze: freeze))
  end

  def initialize_dup(obj)
    __setobj__(obj.__getobj__.dup)
  end

  def __getobj__
    __raise__ ::NotImplementedError, "need to define '__getobj__'"
  end

  def __setobj__(obj)
    __raise__ ::NotImplementedError, "need to define '__setobj__'"
  end

  def self.public_api
    @delegator_api ||= public_instance_methods
  end
end

class SimpleDelegator < Delegator
  def __getobj__
    unless defined?(@delegate_sd_obj)
      return yield if block_given?
      __raise__ ::ArgumentError, "not delegated"
    end
    @delegate_sd_obj
  end

  def __setobj__(obj)
    __raise__ ::ArgumentError, "cannot delegate to self" if self.equal?(obj)
    @delegate_sd_obj = obj
  end

  def inspect
    __getobj__.inspect
  end

  def to_s
    __getobj__.to_s
  end
end

def DelegateClass(superclass, &block)
  klass = Class.new(Delegator)
  ignores = [*::Delegator.public_api, :to_s, :inspect, :=~, :!~, :===]
  protected_instance_methods = superclass.protected_instance_methods
  protected_instance_methods -= ignores
  public_instance_methods = superclass.public_instance_methods
  public_instance_methods -= ignores
  klass.module_eval do
    def __getobj__
      unless defined?(@delegate_dc_obj)
        return yield if block_given?
        __raise__ ::ArgumentError, "not delegated"
      end
      @delegate_dc_obj
    end

    def __setobj__(obj)
      __raise__ ::ArgumentError, "cannot delegate to self" if self.equal?(obj)
      @delegate_dc_obj = obj
    end

    protected_instance_methods.each do |method|
      define_method(method) do |*args, &blk|
        __getobj__.__send__(method, *args, &blk)
      end
      protected method
    end
    public_instance_methods.each do |method|
      define_method(method) do |*args, &blk|
        __getobj__.__send__(method, *args, &blk)
      end
    end
  end
  klass.define_singleton_method :public_instance_methods do |all = true|
    super(all) | superclass.public_instance_methods
  end
  klass.define_singleton_method :protected_instance_methods do |all = true|
    super(all) | superclass.protected_instance_methods
  end
  klass.define_singleton_method :instance_methods do |all = true|
    super(all) | superclass.instance_methods
  end
  klass.define_singleton_method :public_instance_method do |name|
    super(name)
  rescue NameError
    raise unless self.public_instance_methods.include?(name)
    superclass.public_instance_method(name)
  end
  klass.define_singleton_method :instance_method do |name|
    super(name)
  rescue NameError
    raise unless self.instance_methods.include?(name)
    superclass.instance_method(name)
  end
  klass.module_eval(&block) if block
  klass
end
