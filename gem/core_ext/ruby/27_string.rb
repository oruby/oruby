# String additions and MRI behaviour mruby's String lacks

module Kernel
  def self.__oruby_fstring(str)
    plain = __oruby_plain_string?(str)
    return str if str.frozen? && (!plain || !str.instance_variables.empty?)
    return __oruby_frozen_string_copy(str) unless plain

    t = (@__oruby_fstr ||= {})
    by_enc = (t[str.encoding] ||= {})
    by_enc[str] ||= str.frozen? ? str : __oruby_frozen_string_copy(str)
  end
end

class String
  def -@
    ::Kernel.__oruby_fstring(self)
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

  def grapheme_clusters(&block)
    enc = encoding
    res = if enc == Encoding::UTF_8 || enc == Encoding::US_ASCII
            scan(/\X/)
          elsif enc == Encoding::UTF_16LE || enc == Encoding::UTF_16BE || enc == Encoding::UTF_32LE || enc == Encoding::UTF_32BE
            encode(Encoding::UTF_8).scan(/\X/).map { |g| g.encode(enc) }
          elsif !enc.dummy? && enc != Encoding::BINARY && defined?(Encoding::Converter) && Encoding::Converter.respond_to?(:__char_len)
            out = []
            pos = 0
            n = bytesize
            ename = enc.name
            while pos < n
              cl = Encoding::Converter.__char_len(self, pos, ename)
              cl = 1 if cl <= 0
              out << byteslice(pos, cl).force_encoding(enc)
              pos += cl
            end
            out
          else
            chars
          end
    if block
      res.each(&block)
      return self
    end
    res
  end

  def each_grapheme_cluster(&block)
    return to_enum(:each_grapheme_cluster) { grapheme_clusters.size } unless block
    grapheme_clusters(&block)
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
          raise RuntimeError, "invalid hex escape" unless hex.match?(/\A\h\h\z/)
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
            raise RuntimeError, "invalid Unicode escape" if cps.empty? || cps.any? { |h| !h.match?(/\A\h{1,6}\z/) }
            cps.each { |h| out << h.to_i(16).chr(Encoding::UTF_8).b }
            i = j + 1
          else
            hex = s[i, 4]
            raise RuntimeError, "invalid Unicode escape" unless hex.match?(/\A\h{4}\z/)
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

  # strip/lstrip/rstrip (0-arg and selector forms) are installed in C by go-string.c
end

# String.new(str = "", encoding: nil, capacity: nil) as MRI has it: without
# a string the new one is binary
class String
  alias_method :__oruby_initialize, :initialize
  private :__oruby_initialize

  def initialize(*args, encoding: nil, capacity: nil)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)" if args.size > 1
    if args.empty?
      # no string: self is left as it is; String.new's own empty one is binary
      if encoding
        force_encoding(encoding)
      elsif empty? && !frozen?
        force_encoding(Encoding::BINARY)
      end
      return self
    end
    str = args[0]
    str = Kernel.__implicit(str, String) unless String === str
    __oruby_initialize(str)
    force_encoding(encoding) if encoding
    self
  end
  private :initialize
end

# String#bytesplice as MRI's rb_str_bytesplice: (index, length, str
# [, str_index, str_length]) or (range, str [, str_range]); the offsets
# must be in range and on character boundaries, of self and of str
class String
  # mruby's, bytes in place whatever the characters (StringIO splices at
  # byte offsets, as MRI's C StringIO does)
  alias __oruby_raw_bytesplice bytesplice
  private :__oruby_raw_bytesplice

  def bytesplice(*args)
    n = args.size
    raise ArgumentError, "wrong number of arguments (given #{n}, expected 2..5)" if n < 2 || n > 5
    raise ArgumentError, "wrong number of arguments (given #{n}, expected 2, 3, or 5)" if n == 4
    if n == 2 || (n == 3 && !(Integer === args[0]))
      beg, len = String.__oruby_range_beg_len(args[0], bytesize)
      raise TypeError, "wrong argument type #{Kernel.__cname(args[0])} (expected Range)" if beg.nil?
      val = Kernel.__implicit(args[1], String)
      if n == 2
        vbeg, vlen = 0, val.bytesize
      else
        vbeg, vlen = String.__oruby_range_beg_len(args[2], val.bytesize)
        raise TypeError, "wrong argument type #{Kernel.__cname(args[2])} (expected Range)" if vbeg.nil?
      end
    else
      beg = Kernel.__to_int(args[0])
      len = Kernel.__to_int(args[1])
      val = Kernel.__implicit(args[2], String)
      if n == 3
        vbeg, vlen = 0, val.bytesize
      else
        vbeg = Kernel.__to_int(args[3])
        vlen = Kernel.__to_int(args[4])
      end
    end
    beg, len = __oruby_check_beg_len(beg, len)
    vbeg, vlen = val.__send__(:__oruby_check_beg_len, vbeg, vlen)
    raise FrozenError.new("can't modify frozen String: #{inspect}", receiver: self) if frozen?
    enc = encoding
    unless enc == val.encoding
      enc = Encoding.compatible?(self, val)
      raise Encoding::CompatibilityError, "incompatible character encodings: #{encoding} and #{val.encoding}" unless enc
    end
    out = byteslice(0, beg).b << val.byteslice(vbeg, vlen).b << byteslice(beg + len, bytesize - beg - len).b
    replace(out.force_encoding(enc))
  end

  # MRI's rb_range_beg_len(range, len, err = 2): [beg, len], nil for a
  # non-Range, RangeError when it starts outside
  def self.__oruby_range_beg_len(r, len)
    return nil unless Range === r
    b = r.begin.nil? ? 0 : Kernel.__to_int(r.begin)
    excl = r.end.nil? ? false : r.exclude_end?
    e = r.end.nil? ? -1 : Kernel.__to_int(r.end)
    if b < 0
      b += len
      raise RangeError, "#{r.inspect} out of range" if b < 0
    end
    e += len if e < 0
    e += 1 unless excl
    raise RangeError, "#{r.inspect} out of range" if b > len
    e = len if e > len
    l = e - b
    [b, l < 0 ? 0 : l]
  end

  private

  # MRI's str_check_beg_len
  def __oruby_check_beg_len(beg, len)
    slen = bytesize
    raise IndexError, "negative length #{len}" if len < 0
    raise IndexError, "index #{beg} out of string" if slen < beg || (beg < 0 && beg + slen < 0)
    beg += slen if beg < 0
    len = slen - beg if len > slen - beg
    __oruby_char_boundary!(beg)
    __oruby_char_boundary!(beg + len)
    [beg, len]
  end

  def __oruby_char_boundary!(pos)
    return if pos == 0 || pos == bytesize || ascii_only?
    at = 0
    each_char do |c|
      return if at == pos
      at += c.bytesize
      break if at > pos
    end
    return if at == pos
    raise IndexError, "offset #{pos} does not land on character boundary"
  end
end

# String#[]= as MRI's rb_str_aset_m around mruby's own: indexes and counts
# by #to_int, the replacement by #to_str, a Range by rb_range_beg_len
# (RangeError out of it), a regexp capture by number (negative from the
# end) or name; the result has the encoding compatible with both strings
# (Encoding::CompatibilityError when there is none)
class String
  alias __oruby_mrb_aset []=
  private :__oruby_mrb_aset

  def []=(*args)
    n = args.size
    raise ArgumentError, "wrong number of arguments (given #{n}, expected 2..3)" unless n == 2 || n == 3
    val = args[-1]
    idx = args[0]
    # a regexp's match and group are checked before the replacement's
    # #to_str, as MRI
    val = Kernel.__implicit(val, String) unless String === val || Regexp === idx
    if Regexp === idx
      ref = n == 3 ? args[1] : 0
      ref = Kernel.__to_int(ref) unless Integer === ref || String === ref || Symbol === ref
      m = idx.match(self)
      raise IndexError, "regexp not matched" unless m
      if Integer === ref
        nregs = m.size
        raise IndexError, "index #{ref} out of regexp" if ref >= nregs || -ref >= nregs
        ref += nregs if ref < 0
      end
      b = m.begin(ref)
      raise IndexError, "regexp group #{ref} not matched" if b.nil?
      beg, len = b, m.end(ref) - b
      val = Kernel.__implicit(val, String) unless String === val
    elsif n == 3
      beg = Kernel.__to_int(idx)
      len = Kernel.__to_int(args[1])
    elsif String === idx
      i = index(idx)
      raise IndexError, "string not matched" if i.nil?
      beg, len = i, idx.size
    elsif Range === idx
      beg, len = String.__oruby_range_beg_len(idx, self.size)
    else
      beg, len = Kernel.__to_int(idx), 1
    end
    enc = encoding
    unless enc == val.encoding
      c = Encoding.compatible?(self, val)
      raise Encoding::CompatibilityError, "incompatible character encodings: #{enc} and #{val.encoding}" unless c
      enc = c
    end
    raise IndexError, "negative length #{len}" if len < 0
    sz = self.size
    raise IndexError, "index #{beg} out of string" if beg > sz || (beg < 0 && beg + sz < 0)
    __oruby_mrb_aset(beg, len, val)
    force_encoding(enc) unless encoding == enc
    args[-1]
  end
end
