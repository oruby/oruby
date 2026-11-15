require "delegate"

# WeakRef keeps a reference to obj; mruby has no weak references, so the
# object stays alive while the WeakRef does (weakref_alive? is always true).
class WeakRef < Delegator
  VERSION = "0.1.3"

  class RefError < StandardError; end

  def initialize(orig)
    super
  end

  def __getobj__
    unless defined?(@__weak_obj)
      return yield if block_given?
      raise RefError, "Invalid Reference - probably recycled"
    end
    @__weak_obj
  end

  def __setobj__(obj)
    @__weak_obj = obj
  end

  def weakref_alive?
    defined?(@__weak_obj) ? true : false
  end
end
