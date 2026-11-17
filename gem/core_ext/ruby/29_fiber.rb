# Fiber#raise: the fiber resumes (or is transferred to) with a request that
# the Fiber.yield / Fiber#transfer it is suspended in raises.
class Fiber
  request = Object.new.freeze

  class << self
    alias_method :__oruby_yield, :yield
  end

  check = lambda do |r|
    ::Kernel.raise(*r[1], **r[2]) if r.is_a?(Array) && r.size == 3 && request.equal?(r[0])
    r
  end

  define_singleton_method(:yield) do |*args|
    check.call(__oruby_yield(*args))
  end

  alias_method :__oruby_transfer, :transfer
  define_method(:transfer) do |*args|
    check.call(__oruby_transfer(*args))
  end

  define_method(:raise) do |*args, **kw|
    ::Kernel.raise(*args, **kw) if equal?(Fiber.current)
    status = inspect[/\((\w+)\)>\z/, 1]
    ::Kernel.raise FiberError, "cannot raise exception on unborn fiber" if status == "created"
    ::Kernel.raise FiberError, "attempt to resume a terminated fiber" if status == "terminated"
    if instance_variable_get(:@__oruby_transferred)
      __oruby_transfer([request, args, kw])
    else
      resume([request, args, kw])
    end
  end

  # a fiber transferred to is resumed by transfer only
  alias_method :__oruby_transfer_raw, :__oruby_transfer
  define_method(:__oruby_transfer) do |*args|
    instance_variable_set(:@__oruby_transferred, true)
    __oruby_transfer_raw(*args)
  end
end
