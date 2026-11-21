require "delegate"

class WeakRef < Delegator
  VERSION = "0.1.3"
  REFERENTS = ::ObjectSpace::WeakMap.new
  private_constant :REFERENTS

  class RefError < StandardError; end

  def __getobj__
    referent = REFERENTS[self]
    # mruby emits a nil test here, even for BasicObject referents.
    return referent unless referent.nil?
    return nil if REFERENTS.key?(self)
    return yield if block_given?

    raise RefError, "Invalid Reference - probably recycled"
  end

  def __setobj__(obj)
    REFERENTS[self] = obj
  end

  def weakref_alive?
    REFERENTS.key?(self)
  end
end
