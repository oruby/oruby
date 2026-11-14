# Encoding extensions
class EncodingError < StandardError; end unless defined?(EncodingError)
module Encoding
  class CompatibilityError < ::EncodingError; end

  class Dummy
    attr_reader :name
    def initialize(name)
      @name = name.to_s
    end
    def to_s; @name; end
    def inspect; "#<Encoding:#{@name}>"; end
    def ==(other)
      to_s == other.to_s || (other.respond_to?(:name) && @name == other.name)
    end
    def ascii_compatible?
      !%w[UTF_16BE UTF_16LE UTF_32 UTF_32BE UTF_32LE].include?(@name.to_s.upcase.tr('-', '_'))
    end
    def dummy?
      false
    end
  end
end

class << Encoding
  def list
    [find("UTF-8"), find("US-ASCII"), find("ASCII-8BIT"), find("UTF-16BE"), find("UTF-16LE")]
  end
  def default_external
    @default_external ||= (const_defined?(:UTF_8) ? const_get(:UTF_8) : "UTF-8")
  end
  def default_external=(enc)
    @default_external = enc ? (enc.respond_to?(:to_str) ? enc.to_str : find(enc.to_s)) : nil
  end
  def default_internal
    @default_internal ||= nil
  end
  def default_internal=(enc)
    @default_internal = enc ? (enc.respond_to?(:to_str) ? enc.to_str : find(enc.to_s)) : nil
  end

  def find(name)
    @cache ||= {}
    s = name.to_s.upcase.tr('-', '_')
    return @cache[s] if @cache.key?(s)
    enc = case s
    when 'UTF_8', 'UTF8', 'FILESYSTEM', 'LOCALE'
      const_defined?(:UTF_8) ? const_get(:UTF_8) : Encoding::Dummy.new("UTF-8")
    when 'US_ASCII', 'ASCII'
      Encoding::Dummy.new("US-ASCII")
    when 'BINARY', 'ASCII_8BIT'
      const_defined?(:BINARY) ? const_get(:BINARY) : Encoding::Dummy.new("ASCII-8BIT")
    when 'UTF_16BE', 'UTF16BE'
      Encoding::Dummy.new("UTF-16BE")
    when 'UTF_16LE', 'UTF16LE'
      Encoding::Dummy.new("UTF-16LE")
    else
      if s.start_with?("ISO_8859_")
        Encoding::Dummy.new("ISO-8859-" + s[9..-1])
      elsif s.start_with?("ISO8859")
        Encoding::Dummy.new("ISO-8859-" + s[7..-1])
      else
        Encoding::Dummy.new(name.to_s.upcase.tr('_', '-'))
      end
    end
    @cache[s] = enc
    enc
  end

  def const_missing(sym)
    enc = find(sym.to_s) rescue nil
    if enc
      const_set(sym, enc)
      return enc
    end
    super
  end
end

Encoding::US_ASCII = Encoding::Dummy.new("US-ASCII")
Encoding::UTF_16BE = Encoding::Dummy.new("UTF-16BE")
Encoding::UTF_16LE = Encoding::Dummy.new("UTF-16LE")

class String
  def ascii_compatible?
    !%w[UTF_16BE UTF_16LE UTF_32 UTF_32BE UTF_32LE].include?(to_s.upcase.tr('-', '_'))
  end

  def dummy?
    false
  end

  def name
    self
  end

  alias __orig_eq ==
  def ==(other)
    if other.is_a?(Encoding::Dummy)
      return other == self
    end
    __orig_eq(other)
  end

  def ascii_compatible?
    self == "UTF-8" || self == "US-ASCII" || self == "ASCII-8BIT" || self == "BINARY"
  end

  alias __orig_force_encoding force_encoding
  def force_encoding(enc)
    enc = Encoding.find(enc) if enc.is_a?(String) || enc.is_a?(Symbol)
    $__string_encodings ||= {}
    $__string_encodings[object_id] = enc
    begin
      __orig_force_encoding(enc.to_s)
    rescue ArgumentError
      self
    end
  end

  alias __orig_encoding encoding
  def encoding
    $__string_encodings ||= {}
    if $__string_encodings.key?(object_id)
      $__string_encodings[object_id]
    else
      begin
        __orig_encoding
      rescue
        Encoding::UTF_8
      end
    end
  end

  def encode(enc)
    dup.force_encoding(enc)
  end
  def encode!(enc)
    force_encoding(enc)
  end
end
