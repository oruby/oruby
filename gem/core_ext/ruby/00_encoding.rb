# Encoding, as in MRI: a class of frozen Encoding objects, one per encoding.
#
# mruby-encoding defines Encoding as a module of name Strings, and mruby
# strings are tagged UTF-8 or binary only. This replaces the module with the
# MRI class; a string in any other encoding carries mruby's binary tag plus a
# hidden tag naming its Encoding (String#__encoding_tag, go-mrb.c), which
# dup and clone keep and most other string operations drop.
class EncodingError < StandardError; end

Object.__send__(:remove_const, :Encoding)

class Encoding
  class CompatibilityError < EncodingError; end
  class UndefinedConversionError < EncodingError; end
  class InvalidByteSequenceError < EncodingError; end
  class ConverterNotFoundError < EncodingError; end

  # generated from MRI 4.0.9: [name, aliases, dummy?, ascii_compatible?]
  table = [
    ["ASCII-8BIT", ["BINARY"], false, true],
    ["UTF-8", ["CP65001"], false, true],
    ["US-ASCII", ["ASCII", "ANSI_X3.4-1968", "646"], false, true],
    ["UTF-16BE", ["UCS-2BE"], false, false],
    ["UTF-16LE", [], false, false],
    ["UTF-32BE", ["UCS-4BE"], false, false],
    ["UTF-32LE", ["UCS-4LE"], false, false],
    ["UTF-16", [], true, false],
    ["UTF-32", [], true, false],
    ["UTF8-MAC", ["UTF-8-MAC", "UTF-8-HFS"], false, true],
    ["EUC-JP", ["eucJP"], false, true],
    ["Windows-31J", ["CP932", "csWindows31J", "SJIS", "PCK"], false, true],
    ["Big5", [], false, true],
    ["Big5-HKSCS", ["Big5-HKSCS:2008"], false, true],
    ["Big5-UAO", [], false, true],
    ["CESU-8", [], false, true],
    ["CP949", [], false, true],
    ["Emacs-Mule", [], false, true],
    ["EUC-KR", ["eucKR"], false, true],
    ["EUC-TW", ["eucTW"], false, true],
    ["GB18030", [], false, true],
    ["GBK", ["CP936"], false, true],
    ["ISO-8859-1", ["ISO8859-1"], false, true],
    ["ISO-8859-2", ["ISO8859-2"], false, true],
    ["ISO-8859-3", ["ISO8859-3"], false, true],
    ["ISO-8859-4", ["ISO8859-4"], false, true],
    ["ISO-8859-5", ["ISO8859-5"], false, true],
    ["ISO-8859-6", ["ISO8859-6"], false, true],
    ["ISO-8859-7", ["ISO8859-7"], false, true],
    ["ISO-8859-8", ["ISO8859-8"], false, true],
    ["ISO-8859-9", ["ISO8859-9"], false, true],
    ["ISO-8859-10", ["ISO8859-10"], false, true],
    ["ISO-8859-11", ["ISO8859-11"], false, true],
    ["ISO-8859-13", ["ISO8859-13"], false, true],
    ["ISO-8859-14", ["ISO8859-14"], false, true],
    ["ISO-8859-15", ["ISO8859-15"], false, true],
    ["ISO-8859-16", ["ISO8859-16"], false, true],
    ["KOI8-R", ["CP878"], false, true],
    ["KOI8-U", [], false, true],
    ["Shift_JIS", [], false, true],
    ["Windows-1250", ["CP1250"], false, true],
    ["Windows-1251", ["CP1251"], false, true],
    ["Windows-1252", ["CP1252"], false, true],
    ["Windows-1253", ["CP1253"], false, true],
    ["Windows-1254", ["CP1254"], false, true],
    ["Windows-1257", ["CP1257"], false, true],
    ["IBM437", ["CP437"], false, true],
    ["IBM720", ["CP720"], false, true],
    ["IBM737", ["CP737"], false, true],
    ["IBM775", ["CP775"], false, true],
    ["CP850", ["IBM850"], false, true],
    ["IBM852", [], false, true],
    ["CP852", [], false, true],
    ["IBM855", [], false, true],
    ["CP855", [], false, true],
    ["IBM857", ["CP857"], false, true],
    ["IBM860", ["CP860"], false, true],
    ["IBM861", ["CP861"], false, true],
    ["IBM862", ["CP862"], false, true],
    ["IBM863", ["CP863"], false, true],
    ["IBM864", ["CP864"], false, true],
    ["IBM865", ["CP865"], false, true],
    ["IBM866", ["CP866"], false, true],
    ["IBM869", ["CP869"], false, true],
    ["Windows-1258", ["CP1258"], false, true],
    ["GB1988", [], false, true],
    ["macCentEuro", [], false, true],
    ["macCroatian", [], false, true],
    ["macCyrillic", [], false, true],
    ["macGreek", [], false, true],
    ["macIceland", [], false, true],
    ["macRoman", [], false, true],
    ["macRomania", [], false, true],
    ["macThai", [], false, true],
    ["macTurkish", [], false, true],
    ["macUkraine", [], false, true],
    ["CP950", [], false, true],
    ["CP951", [], false, true],
    ["IBM037", ["ebcdic-cp-us"], true, false],
    ["stateless-ISO-2022-JP", [], false, true],
    ["eucJP-ms", ["euc-jp-ms"], false, true],
    ["CP51932", [], false, true],
    ["EUC-JIS-2004", ["EUC-JISX0213"], false, true],
    ["GB2312", ["EUC-CN", "eucCN"], false, true],
    ["GB12345", [], false, true],
    ["ISO-2022-JP", ["ISO2022-JP"], true, false],
    ["ISO-2022-JP-2", ["ISO2022-JP2"], true, false],
    ["CP50220", [], true, false],
    ["CP50221", [], true, false],
    ["Windows-1256", ["CP1256"], false, true],
    ["Windows-1255", ["CP1255"], false, true],
    ["TIS-620", [], false, true],
    ["Windows-874", ["CP874"], false, true],
    ["MacJapanese", ["MacJapan"], false, true],
    ["UTF-7", ["CP65000"], true, false],
    ["UTF8-DoCoMo", [], false, true],
    ["SJIS-DoCoMo", [], false, true],
    ["UTF8-KDDI", [], false, true],
    ["SJIS-KDDI", [], false, true],
    ["ISO-2022-JP-KDDI", [], true, false],
    ["stateless-ISO-2022-JP-KDDI", [], false, true],
    ["UTF8-SoftBank", [], false, true],
    ["SJIS-SoftBank", [], false, true],
  ].freeze
  # constant name => encoding name
  constant_names = {
    "ANSI_X3_4_1968" => "US-ASCII", "ASCII" => "US-ASCII", "ASCII_8BIT" => "ASCII-8BIT", "BIG5" => "Big5",
    "BIG5_HKSCS" => "Big5-HKSCS", "BIG5_HKSCS_2008" => "Big5-HKSCS", "BIG5_UAO" => "Big5-UAO", "BINARY" => "ASCII-8BIT",
    "Big5" => "Big5", "Big5_HKSCS" => "Big5-HKSCS", "Big5_HKSCS_2008" => "Big5-HKSCS", "Big5_UAO" => "Big5-UAO",
    "CESU_8" => "CESU-8", "CP1250" => "Windows-1250", "CP1251" => "Windows-1251", "CP1252" => "Windows-1252",
    "CP1253" => "Windows-1253", "CP1254" => "Windows-1254", "CP1255" => "Windows-1255", "CP1256" => "Windows-1256",
    "CP1257" => "Windows-1257", "CP1258" => "Windows-1258", "CP437" => "IBM437", "CP50220" => "CP50220",
    "CP50221" => "CP50221", "CP51932" => "CP51932", "CP65000" => "UTF-7", "CP65001" => "UTF-8",
    "CP720" => "IBM720", "CP737" => "IBM737", "CP775" => "IBM775", "CP850" => "CP850",
    "CP852" => "CP852", "CP855" => "CP855", "CP857" => "IBM857", "CP860" => "IBM860",
    "CP861" => "IBM861", "CP862" => "IBM862", "CP863" => "IBM863", "CP864" => "IBM864",
    "CP865" => "IBM865", "CP866" => "IBM866", "CP869" => "IBM869", "CP874" => "Windows-874",
    "CP878" => "KOI8-R", "CP932" => "Windows-31J", "CP936" => "GBK", "CP949" => "CP949",
    "CP950" => "CP950", "CP951" => "CP951", "CSWINDOWS31J" => "Windows-31J", "CsWindows31J" => "Windows-31J",
    "EBCDIC_CP_US" => "IBM037", "EMACS_MULE" => "Emacs-Mule", "EUCCN" => "GB2312", "EUCJP" => "EUC-JP",
    "EUCJP_MS" => "eucJP-ms", "EUCKR" => "EUC-KR", "EUCTW" => "EUC-TW", "EUC_CN" => "GB2312",
    "EUC_JISX0213" => "EUC-JIS-2004", "EUC_JIS_2004" => "EUC-JIS-2004", "EUC_JP" => "EUC-JP", "EUC_JP_MS" => "eucJP-ms",
    "EUC_KR" => "EUC-KR", "EUC_TW" => "EUC-TW", "Emacs_Mule" => "Emacs-Mule", "EucCN" => "GB2312",
    "EucJP" => "EUC-JP", "EucJP_ms" => "eucJP-ms", "EucKR" => "EUC-KR", "EucTW" => "EUC-TW",
    "GB12345" => "GB12345", "GB18030" => "GB18030", "GB1988" => "GB1988", "GB2312" => "GB2312",
    "GBK" => "GBK", "IBM037" => "IBM037", "IBM437" => "IBM437", "IBM720" => "IBM720",
    "IBM737" => "IBM737", "IBM775" => "IBM775", "IBM850" => "CP850", "IBM852" => "IBM852",
    "IBM855" => "IBM855", "IBM857" => "IBM857", "IBM860" => "IBM860", "IBM861" => "IBM861",
    "IBM862" => "IBM862", "IBM863" => "IBM863", "IBM864" => "IBM864", "IBM865" => "IBM865",
    "IBM866" => "IBM866", "IBM869" => "IBM869", "ISO2022_JP" => "ISO-2022-JP", "ISO2022_JP2" => "ISO-2022-JP-2",
    "ISO8859_1" => "ISO-8859-1", "ISO8859_10" => "ISO-8859-10", "ISO8859_11" => "ISO-8859-11", "ISO8859_13" => "ISO-8859-13",
    "ISO8859_14" => "ISO-8859-14", "ISO8859_15" => "ISO-8859-15", "ISO8859_16" => "ISO-8859-16", "ISO8859_2" => "ISO-8859-2",
    "ISO8859_3" => "ISO-8859-3", "ISO8859_4" => "ISO-8859-4", "ISO8859_5" => "ISO-8859-5", "ISO8859_6" => "ISO-8859-6",
    "ISO8859_7" => "ISO-8859-7", "ISO8859_8" => "ISO-8859-8", "ISO8859_9" => "ISO-8859-9", "ISO_2022_JP" => "ISO-2022-JP",
    "ISO_2022_JP_2" => "ISO-2022-JP-2", "ISO_2022_JP_KDDI" => "ISO-2022-JP-KDDI", "ISO_8859_1" => "ISO-8859-1", "ISO_8859_10" => "ISO-8859-10",
    "ISO_8859_11" => "ISO-8859-11", "ISO_8859_13" => "ISO-8859-13", "ISO_8859_14" => "ISO-8859-14", "ISO_8859_15" => "ISO-8859-15",
    "ISO_8859_16" => "ISO-8859-16", "ISO_8859_2" => "ISO-8859-2", "ISO_8859_3" => "ISO-8859-3", "ISO_8859_4" => "ISO-8859-4",
    "ISO_8859_5" => "ISO-8859-5", "ISO_8859_6" => "ISO-8859-6", "ISO_8859_7" => "ISO-8859-7", "ISO_8859_8" => "ISO-8859-8",
    "ISO_8859_9" => "ISO-8859-9", "KOI8_R" => "KOI8-R", "KOI8_U" => "KOI8-U", "MACCENTEURO" => "macCentEuro",
    "MACCROATIAN" => "macCroatian", "MACCYRILLIC" => "macCyrillic", "MACGREEK" => "macGreek", "MACICELAND" => "macIceland",
    "MACJAPAN" => "MacJapanese", "MACJAPANESE" => "MacJapanese", "MACROMAN" => "macRoman", "MACROMANIA" => "macRomania",
    "MACTHAI" => "macThai", "MACTURKISH" => "macTurkish", "MACUKRAINE" => "macUkraine", "MacCentEuro" => "macCentEuro",
    "MacCroatian" => "macCroatian", "MacCyrillic" => "macCyrillic", "MacGreek" => "macGreek", "MacIceland" => "macIceland",
    "MacJapan" => "MacJapanese", "MacJapanese" => "MacJapanese", "MacRoman" => "macRoman", "MacRomania" => "macRomania",
    "MacThai" => "macThai", "MacTurkish" => "macTurkish", "MacUkraine" => "macUkraine", "PCK" => "Windows-31J",
    "SHIFT_JIS" => "Shift_JIS", "SJIS" => "Windows-31J", "SJIS_DOCOMO" => "SJIS-DoCoMo", "SJIS_DoCoMo" => "SJIS-DoCoMo",
    "SJIS_KDDI" => "SJIS-KDDI", "SJIS_SOFTBANK" => "SJIS-SoftBank", "SJIS_SoftBank" => "SJIS-SoftBank", "STATELESS_ISO_2022_JP" => "stateless-ISO-2022-JP",
    "STATELESS_ISO_2022_JP_KDDI" => "stateless-ISO-2022-JP-KDDI", "Shift_JIS" => "Shift_JIS", "Stateless_ISO_2022_JP" => "stateless-ISO-2022-JP", "Stateless_ISO_2022_JP_KDDI" => "stateless-ISO-2022-JP-KDDI",
    "TIS_620" => "TIS-620", "UCS_2BE" => "UTF-16BE", "UCS_4BE" => "UTF-32BE", "UCS_4LE" => "UTF-32LE",
    "US_ASCII" => "US-ASCII", "UTF8_DOCOMO" => "UTF8-DoCoMo", "UTF8_DoCoMo" => "UTF8-DoCoMo", "UTF8_KDDI" => "UTF8-KDDI",
    "UTF8_MAC" => "UTF8-MAC", "UTF8_SOFTBANK" => "UTF8-SoftBank", "UTF8_SoftBank" => "UTF8-SoftBank", "UTF_16" => "UTF-16",
    "UTF_16BE" => "UTF-16BE", "UTF_16LE" => "UTF-16LE", "UTF_32" => "UTF-32", "UTF_32BE" => "UTF-32BE",
    "UTF_32LE" => "UTF-32LE", "UTF_7" => "UTF-7", "UTF_8" => "UTF-8", "UTF_8_HFS" => "UTF8-MAC",
    "UTF_8_MAC" => "UTF8-MAC", "WINDOWS_1250" => "Windows-1250", "WINDOWS_1251" => "Windows-1251", "WINDOWS_1252" => "Windows-1252",
    "WINDOWS_1253" => "Windows-1253", "WINDOWS_1254" => "Windows-1254", "WINDOWS_1255" => "Windows-1255", "WINDOWS_1256" => "Windows-1256",
    "WINDOWS_1257" => "Windows-1257", "WINDOWS_1258" => "Windows-1258", "WINDOWS_31J" => "Windows-31J", "WINDOWS_874" => "Windows-874",
    "Windows_1250" => "Windows-1250", "Windows_1251" => "Windows-1251", "Windows_1252" => "Windows-1252", "Windows_1253" => "Windows-1253",
    "Windows_1254" => "Windows-1254", "Windows_1255" => "Windows-1255", "Windows_1256" => "Windows-1256", "Windows_1257" => "Windows-1257",
    "Windows_1258" => "Windows-1258", "Windows_31J" => "Windows-31J", "Windows_874" => "Windows-874",
  }.freeze

  attr_reader :name

  def initialize(name, aliases, dummy, ascii_compatible)
    @name = name.freeze
    @aliases = aliases.map(&:freeze).freeze
    @dummy = dummy
    @ascii_compatible = ascii_compatible
    freeze
  end

  alias to_s name

  def names
    [@name, *@aliases, *Encoding.__dynamic_names(self)]
  end

  def dummy?
    @dummy
  end

  def ascii_compatible?
    @ascii_compatible
  end

  def inspect
    if equal?(BINARY)
      "#<Encoding:BINARY (ASCII-8BIT)>"
    elsif @dummy
      "#<Encoding:#{@name} (dummy)>"
    else
      "#<Encoding:#{@name}>"
    end
  end

  def _dump(_level = -1)
    @name
  end

  UNICODE_VERSION = "17.0.0"

  @by_name = {}
  @list = table.map do |name, aliases, dummy, ascii|
    enc = new(name, aliases, dummy, ascii)
    @by_name[name.upcase] = enc
    aliases.each { |a| @by_name[a.upcase] = enc }
    enc
  end.freeze
  constant_names.each { |const, name| const_set(const, @by_name[name.upcase]) }

  class << self
    undef_method :new

    def allocate
      raise TypeError, "allocator undefined for Encoding"
    end

    def list
      @list.dup
    end

    def name_list
      (@list.flat_map(&:names) + %w[locale external filesystem internal]).uniq
    end

    def aliases
      h = {}
      @list.each { |enc| enc.names.each { |n| h[n] = enc.name unless n == enc.name } }
      h
    end

    def find(name)
      return name if name.is_a?(Encoding)
      raise TypeError, "no implicit conversion of #{name.class} into String" unless name.respond_to?(:to_str)
      key = name.to_str
      unless key.is_a?(String)
        raise TypeError, "can't convert #{name.class} to String (#{name.class}#to_str gives #{key.class})"
      end
      unless key.encoding.ascii_compatible?
        raise ArgumentError, "invalid encoding name (non ASCII)"
      end
      case key.downcase
      when "external" then return default_external
      when "internal" then return default_internal
      when "locale" then return find(locale_charmap)
      when "filesystem" then return default_external
      end
      @by_name[key.upcase] || raise(ArgumentError, "unknown encoding name - #{key}")
    end

    def _load(name)
      find(name)
    end

    def locale_charmap
      (@locale_charmap ||= begin
        lc = (defined?(ENV) ? (ENV["LC_ALL"] || ENV["LC_CTYPE"] || ENV["LANG"]) : nil) || ""
        lc == "C" || lc == "POSIX" ? "US-ASCII" : "UTF-8"
      end).dup
    end

    def default_external
      @default_external ||= find(locale_charmap)
    end

    def default_external=(enc)
      raise ArgumentError, "default external can not be nil" if enc.nil?
      @default_external = find(enc)
    end

    def default_internal
      @default_internal
    end

    def default_internal=(enc)
      @default_internal = enc.nil? ? nil : find(enc)
    end

    # the runtime names MRI adds to the encodings now in use
    def __dynamic_names(enc)
      n = []
      n << "locale" if enc.equal?(find(locale_charmap))
      n << "external" if enc.equal?(default_external)
      n << "filesystem" if enc.equal?(default_external)
      n << "internal" if enc.equal?(default_internal)
      n
    end

    # MRI's rb_enc_compatible: the encoding a concatenation of a and b has,
    # or nil when there is none
    def compatible?(a, b)
      enc1 = __encoding_of(a)
      enc2 = __encoding_of(b)
      return nil if enc1.nil? || enc2.nil?
      return enc1 if enc1.equal?(enc2)
      isstr2 = b.is_a?(String)
      return enc1 if isstr2 && b.empty?
      isstr1 = a.is_a?(String)
      if isstr1 && isstr2 && a.empty?
        return enc1.ascii_compatible? && enc2.ascii_compatible? && b.ascii_only? ? enc1 : enc2
      end
      return nil unless enc1.ascii_compatible? && enc2.ascii_compatible?
      return enc1 if !isstr2 && enc2.equal?(US_ASCII)
      return enc2 if !isstr1 && enc1.equal?(US_ASCII)
      unless isstr1
        a, b = b, a
        isstr1, isstr2 = isstr2, isstr1
      end
      if isstr1
        cr1 = __coderange(a)
        if isstr2
          cr2 = __coderange(b)
          if cr1 != cr2
            return enc2 if cr1 == :seven_bit
            return enc1 if cr2 == :seven_bit
          end
          return enc1 if cr2 == :seven_bit
        end
        return enc2 if cr1 == :seven_bit
      end
      nil
    end

    private

    def __encoding_of(obj)
      case obj
      when Encoding then obj
      when String, Symbol, Regexp then obj.encoding
      end
    end

    def __coderange(str)
      return :seven_bit if str.ascii_only?
      str.valid_encoding? ? :valid : :broken
    end
  end
end

class String
  alias __mrb_force_encoding force_encoding
  alias __mrb_valid_encoding? valid_encoding?
  alias __mrb_b b

  # a string go-mrb.c marked US-ASCII (Integer#to_s and the like, without an
  # instance variable) is US-ASCII while it holds ASCII only and UTF-8 once
  # it does not, as MRI's compatible concatenation makes it; force_encoding
  # keeps the binary flag and the tag
  def encoding
    return __encoding_tag || Encoding::BINARY if __encoding_binary?
    return ascii_only? ? Encoding::US_ASCII : Encoding::UTF_8 if __encoding_us_ascii?
    Encoding::UTF_8
  end

  def force_encoding(enc)
    unless enc.is_a?(Encoding)
      raise TypeError, "no implicit conversion of #{enc.class} into String" unless enc.respond_to?(:to_str)
      enc = Encoding.find(enc.to_str)
    end
    __mrb_force_encoding(enc.equal?(Encoding::UTF_8) ? "UTF-8" : "BINARY")
    self.__encoding_tag = enc.equal?(Encoding::UTF_8) || enc.equal?(Encoding::BINARY) ? nil : enc
    self
  end

  def valid_encoding?
    enc = encoding
    return ascii_only? if enc.equal?(Encoding::US_ASCII)
    __mrb_valid_encoding?
  end

  def b
    s = __mrb_b
    s.__send__(:__encoding_tag=, nil)
    s
  end

  def encode(enc = nil, src = nil, **opts)
    return dup if enc.nil?
    dup.force_encoding(enc)
  end

  def encode!(enc = nil, src = nil, **opts)
    return self if enc.nil?
    force_encoding(enc)
  end
end

class Symbol
  def encoding
    s = to_s
    return Encoding::US_ASCII if s.ascii_only?
    s.valid_encoding? ? Encoding::UTF_8 : Encoding::BINARY
  end
end

class Integer
  alias __mrb_chr chr
  def chr(enc = nil)
    return __mrb_chr if enc.nil?
    enc = Encoding.find(enc)
    return __mrb_chr("UTF-8") if enc.equal?(Encoding::UTF_8)
    return __mrb_chr("BINARY") if enc.equal?(Encoding::BINARY)
    if enc.equal?(Encoding::US_ASCII) && (self < 0 || self > 127)
      raise RangeError, "invalid codepoint 0x#{to_s(16).upcase} in US-ASCII"
    end
    __mrb_chr("BINARY").force_encoding(enc)
  end
end

module Kernel
  private def __ENCODING__
    Encoding::UTF_8
  end
end

module Kernel
  # the encoding a + b has, or Encoding::CompatibilityError (go-mrb.c's
  # String#+ / #<< / #concat / #prepend wrappers call this)
  def self.__oruby_enc_concat(a, b)
    enc = Encoding.compatible?(a, b)
    return enc if enc
    name = ->(e) { e.inspect.delete_prefix("#<Encoding:").delete_suffix(">") }
    raise Encoding::CompatibilityError, "incompatible character encodings: #{name.(a.encoding)} and #{name.(b.encoding)}"
  end
end
