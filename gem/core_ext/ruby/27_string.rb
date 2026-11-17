# String additions and MRI behaviour mruby's String lacks

class String
  # -"str": the deduplicated frozen string, one object per content and
  # encoding (frozen string literals are compiled to this)
  def -@
    t = ::Kernel.instance_variable_get(:@__oruby_fstr) || ::Kernel.instance_variable_set(:@__oruby_fstr, {})
    enc = encoding
    by_enc = (t[enc] ||= {})
    by_enc[self] ||= frozen? && instance_of?(String) ? self : String.new(self).force_encoding(enc).freeze
  end
  alias dedup -@
end

# Ruby 4.1's bit access: bit i is in byte i / 8, LSB first unless
# lsb_first: false
class String
  def __oruby_lsb(lsb_first)
    raise ArgumentError, "invalid lsb_first: #{lsb_first.inspect}" unless lsb_first == true || lsb_first == false
    lsb_first
  end

  def __oruby_bit_mask(i, lsb)
    lsb ? 1 << (i % 8) : 0x80 >> (i % 8)
  end

  # [first bit, count] of a region given as (offset, length) or a Range
  def __oruby_bit_region(a, b, clamp)
    total = bytesize * 8
    if a.is_a?(Range)
      raise ArgumentError, "wrong number of arguments" unless b.nil?
      first = a.begin.nil? ? 0 : Kernel.__to_int(a.begin)
      last = a.end.nil? ? total : Kernel.__to_int(a.end) + (a.exclude_end? ? 0 : 1)
      count = last - first
    else
      first = Kernel.__to_int(a)
      count = Kernel.__to_int(b)
    end
    raise IndexError, "bit offset #{first} out of string" if first < 0
    raise ArgumentError, "negative length #{count}" if count < 0
    if clamp
      first = total if first > total
      count = total - first if first + count > total
    elsif first + count > total
      raise IndexError, "bit region #{first}...#{first + count} out of string"
    end
    [first, count]
  end
  private :__oruby_lsb, :__oruby_bit_mask, :__oruby_bit_region

  def bit_get(i, lsb_first: true)
    lsb = __oruby_lsb(lsb_first)
    i = Kernel.__to_int(i)
    raise IndexError, "bit offset #{i} out of string" if i < 0
    return nil if i >= bytesize * 8
    getbyte(i / 8) & __oruby_bit_mask(i, lsb) == 0 ? 0 : 1
  end

  def bit_set?(i, lsb_first: true)
    v = bit_get(i, lsb_first: lsb_first)
    v.nil? ? nil : v == 1
  end

  def __oruby_bit_apply(a, b, lsb_first)
    raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
    lsb = __oruby_lsb(lsb_first)
    if b.nil? && !a.is_a?(Range)
      i = Kernel.__to_int(a)
      raise IndexError, "bit offset #{i} out of string" if i < 0 || i >= bytesize * 8
      first, count = i, 1
    else
      first, count = __oruby_bit_region(a, b, false)
    end
    (first...first + count).each do |k|
      byte = getbyte(k / 8)
      setbyte(k / 8, yield(byte, __oruby_bit_mask(k, lsb)) & 0xff)
    end
    self
  end
  private :__oruby_bit_apply

  def bit_set(a, b = nil, lsb_first: true)
    __oruby_bit_apply(a, b, lsb_first) { |byte, m| byte | m }
  end

  def bit_clear(a, b = nil, lsb_first: true)
    __oruby_bit_apply(a, b, lsb_first) { |byte, m| byte & ~m }
  end

  def bit_flip(a, b = nil, lsb_first: true)
    __oruby_bit_apply(a, b, lsb_first) { |byte, m| byte ^ m }
  end

  def bit_count(*args, lsb_first: true)
    lsb = __oruby_lsb(lsb_first)
    return each_byte.sum { |x| x.to_s(2).count("1") } if args.empty?
    raise ArgumentError, "bit_count needs a length or a Range" if args.size == 1 && !args[0].is_a?(Range)
    first, count = __oruby_bit_region(args[0], args[1], true)
    (first...first + count).count { |k| getbyte(k / 8) & __oruby_bit_mask(k, lsb) != 0 }
  end

  def __oruby_bitwise(other)
    o = Kernel.__implicit(other, String)
    raise ArgumentError, "byte sizes differ (#{bytesize} and #{o.bytesize})" unless o.bytesize == bytesize
    out = "".b
    bytesize.times { |i| out << yield(getbyte(i), o.getbyte(i)).chr }
    out
  end
  private :__oruby_bitwise

  def bitwise_and(o) = __oruby_bitwise(o) { |x, y| x & y }
  def bitwise_or(o) = __oruby_bitwise(o) { |x, y| x | y }
  def bitwise_xor(o) = __oruby_bitwise(o) { |x, y| x ^ y }

  def bitwise_not
    out = "".b
    each_byte { |x| out << (~x & 0xff).chr }
    out
  end

  %i[bitwise_and bitwise_or bitwise_xor].each do |m|
    define_method(:"#{m}!") do |o|
      raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
      r = __send__(m, o)
      r.bytesize.times { |i| setbyte(i, r.getbyte(i)) }
      self
    end
  end

  def bitwise_not!
    raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
    bytesize.times { |i| setbyte(i, ~getbyte(i) & 0xff) }
    self
  end
end

class String
  def unicode_normalize(form = :nfc)
    require "unicode_normalize/normalize"
    ::UnicodeNormalize.normalize(self, form)
  end

  def unicode_normalize!(form = :nfc)
    replace(unicode_normalize(form))
  end

  def unicode_normalized?(form = :nfc)
    require "unicode_normalize/normalize"
    ::UnicodeNormalize.normalized?(self, form)
  end

  def grapheme_clusters
    return chars unless encoding == Encoding::UTF_8 || encoding == Encoding::US_ASCII
    scan(/\X/)
  end

  def each_grapheme_cluster(&block)
    return to_enum(:each_grapheme_cluster) { grapheme_clusters.size } unless block
    grapheme_clusters.each(&block)
    self
  end

  def sum(bits = 16)
    bits = Kernel.__to_int(bits)
    total = 0
    each_byte { |b| total += b }
    bits > 0 ? total & ((1 << bits) - 1) : total
  end

  def append_as_bytes(*args)
    raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
    bytes = "".b
    args.each do |a|
      case a
      when String then bytes << a.b
      when Integer then bytes << (a & 0xff).chr
      else raise TypeError, "wrong argument type #{Kernel.__cname(a)} (expected String or Integer)"
      end
    end
    enc = encoding
    replace(self.b << bytes)
    force_encoding(enc)
  end

  # the inverse of #dump, as MRI's str_undump
  def undump
    raise Encoding::CompatibilityError, "ASCII incompatible encoding: #{encoding}" unless encoding.ascii_compatible?
    s = b
    raise RuntimeError, "string contains null byte" if s.include?("\0")
    raise RuntimeError, "non-ASCII character detected" unless s.ascii_only?
    invalid = "invalid dumped string; not wrapped with '\"' nor '\"...\".force_encoding(\"...\")' form"
    raise RuntimeError, invalid unless s.start_with?('"')
    out = "".b
    utf8 = binary = false
    i = 1
    n = s.bytesize
    closed = false
    while i < n
      c = s[i]
      if c == '"'
        closed = true
        i += 1
        break
      elsif c == "\\"
        i += 1
        raise RuntimeError, "invalid escape" if i >= n
        e = s[i]
        i += 1
        case e
        when "n" then out << "\n"
        when "r" then out << "\r"
        when "t" then out << "\t"
        when "f" then out << "\f"
        when "v" then out << "\v"
        when "b" then out << "\b"
        when "a" then out << "\a"
        when "e" then out << "\e"
        when "\\", '"', "#" then out << e
        when "x"
          hex = s[i, 2]
          raise RuntimeError, "invalid hex escape" unless hex =~ /\A\h\h\z/
          raise RuntimeError, "hex escape and Unicode escape are mixed" if utf8
          binary = true
          out << hex.to_i(16).chr
          i += 2
        when "u"
          raise RuntimeError, "hex escape and Unicode escape are mixed" if binary
          utf8 = true
          if s[i] == "{"
            j = s.index("}", i)
            raise RuntimeError, "invalid Unicode escape" unless j
            cps = s[i + 1...j].split(" ")
            raise RuntimeError, "invalid Unicode escape" if cps.empty? || cps.any? { |h| h !~ /\A\h{1,6}\z/ }
            cps.each { |h| out << h.to_i(16).chr(Encoding::UTF_8).b }
            i = j + 1
          else
            hex = s[i, 4]
            raise RuntimeError, "invalid Unicode escape" unless hex =~ /\A\h{4}\z/
            out << hex.to_i(16).chr(Encoding::UTF_8).b
            i += 4
          end
        else
          out << "\\" << e
        end
      else
        out << c
        i += 1
      end
    end
    raise RuntimeError, "unterminated dumped string" unless closed
    rest = s[i..]
    enc = encoding
    unless rest.empty?
      m = rest.match(/\A\.(?:dup\.)?force_encoding\("([^"]+)"\)\z/)
      raise RuntimeError, invalid unless m
      enc = begin
        Encoding.find(m[1])
      rescue ArgumentError
        raise RuntimeError, "dumped string has unknown encoding name"
      end
    end
    enc = Encoding::UTF_8 if utf8 && rest.empty?
    out.force_encoding(enc)
  end

  def crypt(salt)
    salt = Kernel.__implicit(salt, String)
    raise ArgumentError, "salt too short (need >=2 bytes)" if salt.bytesize < 2
    raise ArgumentError, "string contains null byte" if include?("\0") || salt.include?("\0")
    Kernel.__oruby_crypt(self, salt)
  end
end

class String
  # chomp: the separator defaults to $/ (mruby's to "\n"); nil chomps nothing
  alias __oruby_chomp chomp
  alias __oruby_chomp! chomp!

  def chomp(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)" if args.size > 1
    if args.empty?
      sep = $/
      return String.new(self) if sep.nil?
      return String === sep && sep != "\n" ? __oruby_chomp(sep) : __oruby_chomp
    end
    sep = args[0]
    return String.new(self) if sep.nil?
    __oruby_chomp(Kernel.__implicit(sep, String))
  end

  def chomp!(*args)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)" if args.size > 1
    raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
    if args.empty?
      sep = $/
      return nil if sep.nil?
      return String === sep && sep != "\n" ? __oruby_chomp!(sep) : __oruby_chomp!
    end
    sep = args[0]
    return nil if sep.nil?
    __oruby_chomp!(Kernel.__implicit(sep, String))
  end

  # Ruby 4.1: strip/lstrip/rstrip take character selectors, as #count does
  %i[strip lstrip rstrip].each do |m|
    alias_method :"__oruby_#{m}", m
    alias_method :"__oruby_#{m}!", :"#{m}!"
  end

  def __oruby_sel_strip(sel, left, right)
    sel = sel.map { |s| Kernel.__implicit(s, String) }
    cs = chars
    i = 0
    j = cs.size
    i += 1 while left && i < j && cs[i].count(*sel) > 0
    j -= 1 while right && j > i && cs[j - 1].count(*sel) > 0
    String.new(self[i, j - i])
  end
  private :__oruby_sel_strip

  def strip(*sel) = sel.empty? ? __oruby_strip : __oruby_sel_strip(sel, true, true)
  def lstrip(*sel) = sel.empty? ? __oruby_lstrip : __oruby_sel_strip(sel, true, false)
  def rstrip(*sel) = sel.empty? ? __oruby_rstrip : __oruby_sel_strip(sel, false, true)

  %i[strip lstrip rstrip].each do |m|
    define_method(:"#{m}!") do |*sel|
      return __send__(:"__oruby_#{m}!") if sel.empty?
      raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
      r = __send__(m, *sel)
      r == self ? nil : replace(r)
    end
  end
end
