module Forwardable
  VERSION = "1.3.3"
  FORWARDABLE_VERSION = VERSION

  @debug = nil
  class << self
    attr_accessor :debug
  end

  def instance_delegate(hash)
    hash.each do |methods, accessor|
      unless defined?(methods.each)
        def_instance_delegator(accessor, methods)
      else
        methods.each { |method| def_instance_delegator(accessor, method) }
      end
    end
  end

  def def_instance_delegators(accessor, *methods)
    methods.each do |method|
      next if method.to_s == "__send__" || method.to_s == "__id__"
      def_instance_delegator(accessor, method)
    end
  end

  def def_instance_delegator(accessor, method, ali = method)
    accessor = accessor.to_s
    method = method.to_sym
    ali = ali.to_sym
    if accessor.start_with?("@")
      ivar = accessor
      define_method(ali) do |*args, **kw, &block|
        target = instance_variable_get(ivar)
        kw.empty? ? target.__send__(method, *args, &block) : target.__send__(method, *args, **kw, &block)
      end
    else
      acc = accessor.to_sym
      define_method(ali) do |*args, **kw, &block|
        target = __send__(acc)
        kw.empty? ? target.__send__(method, *args, &block) : target.__send__(method, *args, **kw, &block)
      end
    end
    ali
  end

  alias delegate instance_delegate
  alias def_delegators def_instance_delegators
  alias def_delegator def_instance_delegator
end

module SingleForwardable
  def single_delegate(hash)
    hash.each do |methods, accessor|
      unless defined?(methods.each)
        def_single_delegator(accessor, methods)
      else
        methods.each { |method| def_single_delegator(accessor, method) }
      end
    end
  end

  def def_single_delegators(accessor, *methods)
    methods.each do |method|
      next if method.to_s == "__send__" || method.to_s == "__id__"
      def_single_delegator(accessor, method)
    end
  end

  def def_single_delegator(accessor, method, ali = method)
    accessor = accessor.to_s
    method = method.to_sym
    ali = ali.to_sym
    if accessor.start_with?("@")
      ivar = accessor
      define_singleton_method(ali) do |*args, &block|
        instance_variable_get(ivar).__send__(method, *args, &block)
      end
    else
      acc = accessor.to_sym
      define_singleton_method(ali) do |*args, &block|
        __send__(acc).__send__(method, *args, &block)
      end
    end
    ali
  end

  alias delegate single_delegate
  alias def_delegators def_single_delegators
  alias def_delegator def_single_delegator
end
