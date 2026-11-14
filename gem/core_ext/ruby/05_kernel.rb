module Kernel
  def warn(*msg, uplevel: nil, category: nil)
    return if $VERBOSE.nil?
    if uplevel
      loc = (caller(uplevel + 1, 1) || [])[0]
      loc = loc.sub(/:in .*$/, "") if loc
      msg = msg.map { |m| loc ? "#{loc}: warning: #{m.to_s.sub(/^warning:\s*/, "")}" : m }
    end
    $stderr.puts(*msg)
  end

  def fixnum_max
    (1 << 62) - 1
  end

  def fixnum_min
    -(1 << 62)
  end

  def bignum_value(plus = 0)
    Bignum.new("9223372036854775808") + plus
  end

  alias __orig_to_enum to_enum
  def to_enum(meth = :each, *args, &block)
    enum = __orig_to_enum(meth, *args)
    if block
      enum.instance_variable_set(:@size_block, block)
    end
    enum
  end
  alias enum_for to_enum
end



def bignum_value(plus = 0)
  Bignum.new("9223372036854775808") + plus
end

def fixnum_max
  (1 << 62) - 1
end

def fixnum_min
  -(1 << 62)
end


module Kernel
  def __dir__
    loc = (caller(1, 1) || [])[0]
    return nil unless loc
    file = loc.sub(/:\d+.*$/, "")
    return nil if file == "-e" || file.empty?
    begin
      File.dirname(File.realpath(file))
    rescue
      File.dirname(file)
    end
  end
end
