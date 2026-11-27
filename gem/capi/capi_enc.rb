# What the encoding functions of the C API (capi_enc.inc) ask Ruby for.
class Encoding
  # rb_enc_alias: name finds the encoding orig is
  def self.__capi_alias(name, orig)
    enc = find(orig)
    @by_name[name.to_s.upcase] = enc
    enc
  end
end

module ORubyCAPI
  # the place of enc in Encoding.list: what the C API's index is
  def self.enc_index(enc) = Encoding.list.index(enc) || -1
  def self.enc_at(i) = Encoding.list[i]

  CTYPE_RE = {
    1 => /\A[[:alpha:]]\z/, 2 => /\A[[:blank:]]\z/, 3 => /\A[[:cntrl:]]\z/, 4 => /\A[[:digit:]]\z/,
    5 => /\A[[:graph:]]\z/, 6 => /\A[[:lower:]]\z/, 7 => /\A[[:print:]]\z/, 8 => /\A[[:punct:]]\z/,
    9 => /\A[[:space:]]\z/, 10 => /\A[[:upper:]]\z/, 11 => /\A[[:xdigit:]]\z/, 12 => /\A[[:word:]]\z/,
    13 => /\A[[:alnum:]]\z/
  }.freeze

  # rb_enc_isctype: is the character code of that ONIGENC_CTYPE_* in enc
  def self.enc_ctype(code, type, enc)
    return code == 0x0a if type == 0
    re = CTYPE_RE[type] or return false
    ch = begin
      code.chr(enc)
    rescue RangeError, ArgumentError
      return false
    end
    return false unless ch.valid_encoding?
    # the posix classes know Unicode: other encodings are tested as UTF-8
    ch = (ch.encode(Encoding::UTF_8) rescue ch) unless enc == Encoding::UTF_8
    re.match?(ch)
  end
end
