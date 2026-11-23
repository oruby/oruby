class IO
  def expect(pattern, timeout = 9_999_999)
    pattern = Regexp.new(Regexp.escape(pattern)) if String === pattern
    raise TypeError, "unsupported pattern class: #{pattern.class}" unless Regexp === pattern

    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + timeout if timeout
    pending = @__oruby_expect_pending
    buffer = +""
    result = nil
    loop do
      if pending && !pending.empty?
        char = pending.slice!(0)
      else
        remaining = deadline && [deadline - Process.clock_gettime(Process::CLOCK_MONOTONIC), 0].max
        break unless IO.select([self], nil, nil, remaining)
        char = getc
        break unless char
      end
      buffer << char
      if $expect_verbose
        $stdout.print(char)
        $stdout.flush
      end
      if match = pattern.match(buffer)
        result = [buffer, *match.captures]
        break
      end
    end
    @__oruby_expect_pending = result ? pending : buffer
    return result unless block_given?
    yield result
    nil
  end
end
