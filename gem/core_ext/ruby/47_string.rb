# String behaviour MRI has and mruby's C methods lack: character-based
# insert, encoding-aware chars/each_char for strings in other encodings.
class String
  # rb_str_insert: a character index, -1 appends, another negative one
  # inserts after that character
  def insert(idx, str)
    idx = Kernel.__to_int(idx)
    str = Kernel.__implicit(str, String) unless String === str
    if idx == -1
      self << str
    else
      pos = idx < 0 ? idx + 1 : idx
      raise IndexError, "index #{pos} out of string" if pos < -length || pos > length
      self[pos, 0] = str
    end
    self
  end

  alias __oruby_plain_chars chars
  alias __oruby_plain_each_char each_char

  # a string in an encoding other than UTF-8/BINARY (tagged) is split by
  # that encoding's characters, each one in the same encoding
  def chars(&block)
    return __oruby_plain_chars(&block) unless __encoding_tag
    res = __oruby_enc_chars
    return res unless block
    res.each(&block)
    self
  end

  def each_char(&block)
    return __oruby_plain_each_char(&block) unless __encoding_tag
    return to_enum(:each_char) { size } unless block
    __oruby_enc_chars.each(&block)
    self
  end

  private def __oruby_enc_chars
    enc = encoding
    ename = enc.name
    out = []
    pos = 0
    n = bytesize
    dummy = enc.dummy?  # a dummy encoding's characters are its bytes
    while pos < n
      cl = dummy ? 1 : Encoding::Converter.__char_len(self, pos, ename)
      cl = 1 if cl <= 0
      out << byteslice(pos, cl).force_encoding(enc)
      pos += cl
    end
    out
  end
end

class String
  # length of a string in an encoding other than UTF-8/BINARY (go-mrb.c's
  # String#length calls it for those): an incomplete UTF-16/UTF-32 unit
  # counts as one character, as in MRI
  private def __oruby_enc_length
    enc = encoding
    n = bytesize
    case enc.name
    when "UTF-32LE", "UTF-32BE"
      (n + 3) / 4
    when "UTF-16LE", "UTF-16BE"
      le = enc.name == "UTF-16LE"
      count = 0
      i = 0
      while i < n
        if i + 1 >= n
          count += 1
          break
        end
        hi = le ? getbyte(i + 1) : getbyte(i)
        if hi >= 0xD8 && hi <= 0xDB && i + 3 < n
          lo = le ? getbyte(i + 3) : getbyte(i + 2)
          i += lo >= 0xDC && lo <= 0xDF ? 4 : 2
        else
          i += 2
        end
        count += 1
      end
      count
    else
      return n if enc.dummy? || (enc.ascii_compatible? && ascii_only?)
      __oruby_enc_chars.size
    end
  end
end

class String
  alias __oruby_mrb_dump dump

  # rb_str_dump: a UTF-8 string's characters past ASCII as \u notation; a
  # string of another encoding keeps it, an ASCII-incompatible one says
  # which with .dup.force_encoding(name)
  def dump
    enc = encoding
    if enc.equal?(Encoding::UTF_8) && !ascii_only?
      out = +'"'
      buf = +""
      each_char do |c|
        if c.bytesize > 1 && c.valid_encoding?
          out << buf.__oruby_mrb_dump[1...-1] unless buf.empty?
          buf.clear
          cp = c.ord
          out << (cp > 0xFFFF ? "\\u{#{cp.to_s(16).upcase}}" : "\\u#{cp.to_s(16).upcase.rjust(4, '0')}")
        else
          buf << c
        end
      end
      out << buf.__oruby_mrb_dump[1...-1] unless buf.empty?
      return out << '"'
    end
    r = b.__oruby_mrb_dump
    return r if enc.equal?(Encoding::UTF_8) || enc.equal?(Encoding::BINARY)
    return r << ".dup.force_encoding(\"#{enc.name}\")" unless enc.ascii_compatible?
    r.force_encoding(enc)
  end

  # chomp/chop of an ASCII-incompatible string (UTF-16/32), as UTF-8
  alias __oruby_wide_chomp chomp
  alias __oruby_wide_chomp! chomp!
  alias __oruby_wide_chop chop
  alias __oruby_wide_chop! chop!

  def chomp(*args)
    return __oruby_wide_chomp(*args) unless __oruby_wide?
    enc = encoding
    args = args.map { |a| String === a && !a.encoding.ascii_compatible? ? a.encode(Encoding::UTF_8) : a }
    encode(Encoding::UTF_8).chomp(*args).encode(enc)
  end

  def chomp!(*args)
    return __oruby_wide_chomp!(*args) unless __oruby_wide?
    r = chomp(*args)
    return nil if r.bytesize == bytesize
    replace(r)
  end

  def chop
    return __oruby_wide_chop unless __oruby_wide?
    encode(Encoding::UTF_8).chop.encode(encoding)
  end

  def chop!
    return __oruby_wide_chop! unless __oruby_wide?
    return nil if empty?
    replace(chop)
  end

  private def __oruby_wide?
    __encoding_tag && !encoding.ascii_compatible? && !encoding.dummy? && valid_encoding?
  end

  alias __oruby_mrb_upto upto
  def upto(max, exclusive = false, &block)
    return to_enum(:upto, max, exclusive) { nil } unless block
    Kernel.__oruby_enc_concat(self, max) if String === max && !(max.ascii_only? && ascii_only?)
    __oruby_mrb_upto(max, exclusive, &block)
  end
end

class String
  UNICODE_WIDE = %w[UTF-16LE UTF-16BE UTF-32LE UTF-32BE].freeze
  private_constant :UNICODE_WIDE

  # rb_str_inspect of a string in another encoding (go-mrb.c's
  # String#inspect calls it for those): ASCII as usual, a Unicode character
  # in \u notation, another one as \x{HEX} of its bytes, a broken byte \xHH
  private def __oruby_enc_inspect
    enc = encoding
    unicode = UNICODE_WIDE.include?(enc.name)
    out = +'"'
    run = +""
    flush = -> { out << (unicode ? run : run.b).inspect[1...-1] unless run.empty?; run.clear }
    __oruby_enc_chars.each do |c|
      if unicode
        if c.valid_encoding?
          u = c.encode(Encoding::UTF_8)
          if u.ascii_only?
            run << u
          else
            flush.()
            cp = u.ord
            out << (cp > 0xFFFF ? "\\u{#{cp.to_s(16).upcase}}" : "\\u#{cp.to_s(16).upcase.rjust(4, '0')}")
          end
        else
          flush.()
          c.each_byte { |b| out << "\\x#{b.to_s(16).upcase.rjust(2, '0')}" }
        end
      elsif c.bytesize == 1 && c.getbyte(0) < 0x80 && enc.ascii_compatible?
        run << c.b.force_encoding(Encoding::UTF_8)
      else
        flush.()
        if c.bytesize > 1 && c.valid_encoding?
          out << "\\x{#{c.unpack1('H*').upcase}}"
        else
          c.each_byte { |b| out << "\\x#{b.to_s(16).upcase.rjust(2, '0')}" }
        end
      end
    end
    flush.()
    out << '"'
  end
end

module Kernel
  INSPECT_ESCAPES = {
    0 => '\0', 7 => '\a', 8 => '\b', 9 => '\t', 10 => '\n',
    11 => '\v', 12 => '\f', 13 => '\r', 27 => '\e', 127 => '\c?'
  }.freeze
  private_constant :INSPECT_ESCAPES

  def self.__oruby_inspect_symbol_quote?(name)
    enc = Encoding.default_internal || Encoding.default_external
    return true if name.encoding != enc || !enc.ascii_compatible? || !name.valid_encoding?
    !name.encode(Encoding::UTF_8).match?(/\A(?:[a-zA-Z_]|[^\x00-\x7F])(?:[a-zA-Z0-9_]|[^\x00-\x7F])*[!?]?\z/)
  end

  # Make an inspection fragment readable in the selected encoding while
  # preserving the syntax already produced by the object's inspect method.
  def self.__oruby_inspect_part(str)
    target = Encoding.default_internal || Encoding.default_external
    enc = str.encoding
    if str.ascii_only?
      return str if target.ascii_compatible? || enc.equal?(Encoding::US_ASCII)
      return str.dup.force_encoding(Encoding::US_ASCII)
    end
    return str if enc.equal?(target) && target.ascii_compatible?
    unicode = %w[UTF-8 UTF8-MAC UTF-16LE UTF-16BE UTF-32LE UTF-32BE].include?(enc.name)
    out = +""
    str.each_char do |char|
      if !char.valid_encoding?
        char.each_byte { |byte| out << format('\x%02X', byte) }
        next
      end
      cp = unicode && !enc.equal?(Encoding::UTF_8) ? char.encode(Encoding::UTF_8).ord : char.ord
      if INSPECT_ESCAPES.key?(cp)
        out << INSPECT_ESCAPES[cp]
      elsif cp.between?(32, 126)
        out << cp.chr
      elsif unicode
        out << (cp <= 0xFFFF ? format('\u%04X', cp) : format('\u{%X}', cp))
      else
        out << (cp <= 0xFF ? format('\x%02X', cp) : format('\x{%X}', cp))
      end
    end
    out.force_encoding(Encoding::US_ASCII)
  end
end

class String
  alias __oruby_mrb_each_byte each_byte
  def each_byte(&block)
    return to_enum(:each_byte) { bytesize } unless block
    __oruby_mrb_each_byte(&block)
  end

  alias __oruby_mrb_bytes bytes
  def bytes(&block)
    return __oruby_mrb_bytes unless block
    __oruby_mrb_bytes.each(&block)
    self
  end

  # each_codepoint without a block is an Enumerator (codepoints an Array);
  # a broken string fails only when iterated
  alias __oruby_mrb_codepoints codepoints
  def each_codepoint(&block)
    return to_enum(:each_codepoint) { size } unless block
    __oruby_mrb_codepoints(&block)
  end
end
