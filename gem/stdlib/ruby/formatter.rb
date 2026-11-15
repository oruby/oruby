class Random
  # Methods generating random values from #bytes (or #gen_random)
  module Formatter
    def random_bytes(n = nil)
      n = n ? n.to_int : 16
      __bytes(n)
    end

    def hex(n = nil)
      random_bytes(n).unpack1("H*")
    end

    def base64(n = nil)
      [random_bytes(n)].pack("m0")
    end

    def urlsafe_base64(n = nil, padding = false)
      s = [random_bytes(n)].pack("m0")
      s.tr!("+/", "-_")
      s.delete!("=") unless padding
      s
    end

    def uuid
      ary = random_bytes(16).unpack("NnnnnN")
      ary[2] = (ary[2] & 0x0fff) | 0x4000
      ary[3] = (ary[3] & 0x3fff) | 0x8000
      "%08x-%04x-%04x-%04x-%04x%08x" % ary
    end
    alias uuid_v4 uuid

    def uuid_v7
      ms = (Time.now.to_f * 1000).to_i
      ary = random_bytes(10).unpack("nnnN")
      "%08x-%04x-%04x-%04x-%04x%08x" % [ms >> 16, ms & 0xffff, (ary[0] & 0x0fff) | 0x7000,
                                        (ary[1] & 0x3fff) | 0x8000, ary[2], ary[3]]
    end

    ALPHANUMERIC = [*'A'..'Z', *'a'..'z', *'0'..'9']

    def alphanumeric(n = nil, chars: ALPHANUMERIC)
      n = 16 if n.nil?
      raise ArgumentError, "invalid size: #{n.inspect}" unless n.is_a?(Integer)
      choose(chars, n)
    end

    def random_number(n = 0)
      __random_value(n, true)
    end

    def rand(n = 0)
      __random_value(n, false)
    end

    private

    def choose(source, n)
      size = source.size
      (0...n).map { source[__random_int(size)].to_s }.join
    end

    def __bytes(n)
      if respond_to?(:bytes)
        bytes(n)
      else
        gen_random(n)
      end
    end

    def __random_int(n)
      return 0 if n <= 1
      bits = (n - 1).to_s(2).size
      nbytes = (bits + 7) / 8
      mask = (1 << bits) - 1
      loop do
        v = __bytes(nbytes).unpack1("H*").to_i(16) & mask
        return v if v < n
      end
    end

    def __random_float
      __random_int(1 << 53) / 9007199254740992.0
    end

    def __random_value(n, lenient)
      case n
      when nil
        __random_float
      when Integer
        n > 0 ? __random_int(n) : (lenient ? __random_float : raise(ArgumentError, "invalid argument - #{n}"))
      when Float
        n > 0 ? __random_float * n : (lenient ? __random_float : raise(ArgumentError, "invalid argument - #{n}"))
      when Range
        first, last = n.begin, n.end
        if first.is_a?(Integer) && last.is_a?(Integer)
          last -= 1 if n.exclude_end?
          return nil if last < first && lenient
          raise ArgumentError, "invalid argument - #{n}" if last < first
          first + __random_int(last - first + 1)
        else
          first = first.to_f
          last = last.to_f
          first + __random_float * (last - first)
        end
      else
        raise ArgumentError, "invalid argument - #{n.inspect}" unless lenient
        __random_float
      end
    end
  end
end
