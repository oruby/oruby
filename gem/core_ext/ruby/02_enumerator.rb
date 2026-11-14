class Enumerator
  class ArithmeticSequence < Enumerator
    attr_reader :begin, :end, :step
    def initialize(range, step)
      @range = range
      @begin = range.begin
      @end = range.end
      @step = step
      @exclude_end = range.exclude_end?
    end
    def exclude_end?
      @exclude_end
    end
    def ==(other)
      return false unless other.is_a?(Enumerator::ArithmeticSequence)
      @begin == other.begin && @end == other.end && @step == other.step && @exclude_end == other.exclude_end?
    end
    alias eql? ==
    def hash
      [@begin, @end, @step, @exclude_end].hash
    end
    def inspect
      "((#{@range.inspect}).step(#{@step}))"
    end
    def each(&block)
      @range.step(@step, &block)
    end
  end
end

EncodingError = Class.new(StandardError) unless Object.const_defined?(:EncodingError)


class Enumerator
  alias __orig_size size
  def size
    if instance_variable_defined?(:@size_block)
      sb = instance_variable_get(:@size_block)
      sb ? sb.call : nil
    else
      __orig_size
    end
  end
end
