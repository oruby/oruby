# Timeout as MRI's timeout.rb has it, with oruby's own deadline instead of a
# timer thread: each block gets a Go context with the deadline (derived from
# the one around it, so blocks nest; Go methods that block wait on it). When
# it passes, Timeout.__oruby_expire raises in the block at its next
# instruction, a sleep included, as MRI's timer thread does with Thread#raise.
module Timeout
  VERSION = "0.4.3"

  # raised in the block when no exception class was given; never rescued by
  # the block's own `rescue => e`, turned into Timeout::Error outside it
  class ExitException < Exception
    def exception(*) = self
  end

  class Error < RuntimeError
    def self.handle_timeout(message)
      exc = ExitException.new(message)
      begin
        yield exc
      rescue ExitException => e
        raise new(message) if exc.equal?(e)
        raise e
      end
    end
  end

  @__oruby_requests = {}

  def self.timeout(sec, klass = nil, message = nil, &block)
    return yield(sec) if sec.nil? || sec.zero?
    raise ArgumentError, "Timeout sec must be a non-negative number" if 0 > sec
    message ||= "execution expired"
    perform = proc do |exc|
      token = ::Kernel.__oruby_timeout_start(sec)
      @__oruby_requests[token] = [exc, message]
      begin
        return yield(sec)
      ensure
        @__oruby_requests.delete(token)
        ::Kernel.__oruby_timeout_stop(token)
      end
    end
    klass ? perform.call(klass) : Error.handle_timeout(message, &perform)
  end

  # the deadline of block token passed (go-signal.c, MrbState.CheckSignals)
  def self.__oruby_expire(token)
    exc, message = @__oruby_requests.delete(token)
    raise exc, message if exc
  end

  def timeout(*args, &block)
    Timeout.timeout(*args, &block)
  end
  private :timeout
end
