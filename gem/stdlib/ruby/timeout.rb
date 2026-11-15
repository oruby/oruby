module Timeout
  VERSION = "0.4.3"

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

  # The block runs in the calling thread. oruby cannot interrupt running
  # Ruby code, so the timeout is checked when the block returns.
  def self.timeout(sec, klass = nil, message = nil, &block)
    return yield(sec) if sec.nil? || sec == 0
    raise ArgumentError, "Timeout sec must be a non-negative number" if sec < 0
    message ||= "execution expired"
    start = Time.now
    result = yield(sec)
    if Time.now - start > sec
      raise (klass || Error), message
    end
    result
  end

  def timeout(*args, &block)
    Timeout.timeout(*args, &block)
  end
  private :timeout
end
