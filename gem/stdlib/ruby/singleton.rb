module Singleton
  VERSION = "0.3.0"

  def clone
    raise TypeError, "can't clone instance of singleton #{self.class}"
  end

  def dup
    raise TypeError, "can't dup instance of singleton #{self.class}"
  end

  def _dump(depth = -1)
    ""
  end

  module SingletonClassMethods
    def clone
      Singleton.__init__(super)
    end

    def _load(str)
      instance
    end

    def instance
      @singleton__instance__ || @singleton__mutex__.synchronize { @singleton__instance__ ||= new }
    end

    private

    def inherited(sub_klass)
      super
      Singleton.__init__(sub_klass)
    end

    def set_instance(val)
      @singleton__instance__ = val
    end

    def set_mutex(val)
      @singleton__mutex__ = val
    end
  end

  def self.module_with_class_methods
    SingletonClassMethods
  end

  class << Singleton
    def __init__(klass)
      klass.instance_eval do
        set_instance(nil)
        set_mutex(Mutex.new)
      end
      klass
    end

    private

    undef_method :extend_object rescue nil

    def append_features(mod)
      unless mod.instance_of?(Class)
        raise TypeError, "Inclusion of the OO-Singleton module in module #{mod}"
      end
      super
    end

    def included(klass)
      super
      klass.private_class_method :new, :allocate
      klass.extend module_with_class_methods
      Singleton.__init__(klass)
    end
  end
end
