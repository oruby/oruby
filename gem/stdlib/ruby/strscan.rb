# StringScanner: lexical scanning over a String (byte positions).
# Patterns are matched against the unscanned rest, so \A and ^ anchor at the
# scan pointer (non fixed_anchor mode).
class StringScanner
  Version = "3.1.6"
  Id = "$Id$"

  class Error < StandardError; end

  def self.must_C_version = self

  attr_reader :string

  def initialize(string, _dup = false, fixed_anchor: false)
    @string = __to_str(string)
    @fixed_anchor = fixed_anchor
    @pos = 0
    __clear
  end

  def initialize_copy(other)
    super
    @string = other.string
    @pos = other.pos
    @match = other.instance_variable_get(:@match)
    @match_pos = other.instance_variable_get(:@match_pos)
    @prev = other.instance_variable_get(:@prev)
    @last = other.instance_variable_get(:@last)
    self
  end

  def fixed_anchor? = @fixed_anchor

  def string=(str)
    @string = __to_str(str)
    reset
    str
  end

  def concat(str)
    @string << __to_str(str)
    self
  end
  alias << concat

  def pos = @pos
  alias pointer pos

  def pos=(n)
    n = n.to_int
    n += @string.bytesize if n < 0
    raise RangeError, "index out of range" if n < 0 || n > @string.bytesize
    @pos = n
  end
  alias pointer= pos=

  def charpos
    @string.byteslice(0, @pos).size
  end

  def reset
    @pos = 0
    __clear
    self
  end

  def terminate
    @pos = @string.bytesize
    __clear
    self
  end
  alias clear terminate

  def unscan
    raise Error, "unscan error: not scanned yet" unless @last
    @pos = @prev
    __clear
    self
  end

  def eos? = @pos >= @string.bytesize
  alias empty? eos?

  def rest? = !eos?

  def rest
    @pos >= @string.bytesize ? +"" : @string.byteslice(@pos, @string.bytesize - @pos)
  end

  def rest_size = [@string.bytesize - @pos, 0].max
  alias restsize rest_size

  def beginning_of_line?
    @pos == 0 || @string.getbyte(@pos - 1) == 10
  end
  alias bol? beginning_of_line?

  # -- scanning ------------------------------------------------------------

  def scan(pattern) = __do(pattern, true, true, true)
  def match?(pattern) = __do(pattern, false, false, true)
  def skip(pattern) = __do(pattern, true, false, true)
  def check(pattern) = __do(pattern, false, true, true)
  def scan_until(pattern) = __do(pattern, true, true, false)
  def exist?(pattern) = __do(pattern, false, false, false)
  def skip_until(pattern) = __do(pattern, true, false, false)
  def check_until(pattern) = __do(pattern, false, true, false)

  def scan_full(pattern, advance, return_string)
    __do(pattern, advance, return_string, true)
  end

  def search_full(pattern, advance, return_string)
    __do(pattern, advance, return_string, false)
  end

  def getch
    return __fail if eos?
    enc = @string.encoding
    if enc != Encoding::UTF_8 && enc != Encoding::BINARY && enc != Encoding::US_ASCII &&
       defined?(Encoding::Converter) && Encoding::Converter.respond_to?(:__char_len)
      len = Encoding::Converter.__char_len(@string, @pos, enc.name)
      c = @string.byteslice(@pos, len > 0 ? len : 1)
    else
      c = rest[0]
    end
    __set_simple(c)
  end

  def get_byte
    return __fail if eos?
    __set_simple(@string.byteslice(@pos, 1))
  end
  alias getbyte get_byte

  def scan_byte
    return __fail if eos?
    b = @string.getbyte(@pos)
    __set_simple(@string.byteslice(@pos, 1))
    b
  end

  def peek_byte
    eos? ? nil : @string.getbyte(@pos)
  end

  def peek(len)
    len = len.to_int
    raise ArgumentError, "negative string size (or size too big)" if len < 0
    return +"" if eos?
    @string.byteslice(@pos, len)
  end
  alias peep peek

  def scan_integer(base: 10)
    raise ArgumentError, "Unsupported integer base: #{base.inspect}, expected 10 or 16" unless base == 10 || base == 16
    unless @string.encoding.ascii_compatible?
      raise Encoding::CompatibilityError, "ASCII incompatible encoding: #{@string.encoding}"
    end
    re = base == 10 ? /[+-]?\d+/ : /[+-]?(?:0x)?[0-9a-fA-F]+/
    s = scan(re)
    s && s.to_i(base)
  end

  # -- match data ----------------------------------------------------------

  def matched?
    !@match.nil?
  end

  def matched
    return nil unless @match
    @match[0]
  end

  def matched_size
    return nil unless @match
    @match[0].bytesize
  end

  def pre_match
    return nil unless @match
    @string.byteslice(0, @match_pos)
  end

  def post_match
    return nil unless @match
    stop = @match_pos + @match[0].bytesize
    @string.byteslice(stop, @string.bytesize - stop)
  end

  def [](i)
    return nil unless @match
    case i
    when Symbol, String
      raise IndexError, "undefined group name reference: #{i}" unless @match.respond_to?(:names) && @match.names.include?(i.to_s)
      @match[i.to_s]
    else
      unless i.is_a?(Integer)
        raise TypeError, "no implicit conversion of #{i.nil? ? 'nil' : i.class} into Integer" unless i.respond_to?(:to_int)
        i = i.to_int
        raise TypeError, "can't convert into Integer" unless i.is_a?(Integer)
      end
      i += @match.size if i < 0
      return nil if i < 0 || i >= @match.size
      @match[i]
    end
  end

  def size
    @match && @match.size
  end

  def captures
    return nil unless @match
    @match.captures
  end

  def values_at(*idx)
    return nil unless @match
    idx.map { |i| self[i] }
  end

  def named_captures
    return {} unless @match && @match.respond_to?(:named_captures)
    @match.named_captures
  end

  def inspect
    return "#<StringScanner fin>" if eos?
    if @pos == 0
      "#<StringScanner #{@pos}/#{@string.bytesize} @ #{__inspect_after}>"
    else
      "#<StringScanner #{@pos}/#{@string.bytesize} #{__inspect_before} @ #{__inspect_after}>"
    end
  end

  private

  def __to_str(obj)
    return obj if obj.is_a?(String)
    return obj.to_str if obj.respond_to?(:to_str)
    raise TypeError, "no implicit conversion of #{obj.nil? ? 'nil' : obj.class} into String"
  end

  def __inspect_before
    s = @pos > 5 ? "..." + @string.byteslice(@pos - 5, 5) : @string.byteslice(0, @pos)
    s.inspect
  end

  def __inspect_after
    r = rest
    (r.bytesize > 5 ? r.byteslice(0, 5) + "..." : r).inspect
  end

  def __clear
    @match = nil
    @match_pos = nil
    @last = false
  end

  def __fail
    __clear
    nil
  end

  # simple 1-element match (getch/get_byte)
  def __set_simple(str)
    @prev = @pos
    @match = [str]
    def @match.captures = []
    def @match.size = 1
    @match_pos = @pos
    @pos += str.bytesize
    @last = true
    str
  end

  def __do(pattern, advance, return_string, anchored)
    __clear
    rest = self.rest
    if pattern.is_a?(String)
      if anchored
        return nil unless rest.start_with?(pattern)
        offset = 0
      else
        offset = rest.byteindex(pattern)
        return nil unless offset
      end
      m = [pattern.dup]
      def m.captures = []
      def m.size = 1
      len = pattern.bytesize
    elsif pattern.is_a?(Regexp)
      if @fixed_anchor
        cp = charpos
        m = pattern.match(@string, cp)
        return nil unless m
        m_byte_pos = m.pre_match.bytesize
        offset = m_byte_pos - @pos
        return nil if offset < 0 || (anchored && offset != 0)
        len = m[0].bytesize
      else
        m = pattern.match(rest)
        return nil unless m
        offset = m.pre_match.bytesize
        return nil if anchored && offset != 0
        len = m[0].bytesize
      end
    else
      raise TypeError, "wrong argument type #{pattern.class} (expected Regexp)"
    end
    @match = m
    @match_pos = @pos + offset
    @prev = @pos
    @last = true
    consumed = offset + len
    if advance
      @pos += consumed
    end
    if return_string
      anchored ? m[0] : @string.byteslice(@prev, consumed)
    else
      consumed
    end
  end
end
