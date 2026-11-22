# The Converter class of Encoding and String#encode, as in MRI, on the Go converter
# (transcode.go). Conversions go through UTF-8 unless one side is UTF-8;
# errors name the step of that path they happened in.

class Encoding
  class InvalidByteSequenceError
    attr_reader :source_encoding_name, :destination_encoding_name,
                :source_encoding, :destination_encoding,
                :error_bytes, :readagain_bytes

    def incomplete_input?
      @incomplete_input
    end
  end

  class UndefinedConversionError
    attr_reader :source_encoding_name, :destination_encoding_name,
                :source_encoding, :destination_encoding, :error_char
  end

  class ConverterNotFoundError
  end

  class Converter
    # [Encoding or nil, name] for an encoding argument: an Encoding, or
    # anything with to_str (an unknown name is kept, for the error)
    def self.__enc_arg(enc)
      return [enc, enc.name] if enc.is_a?(Encoding)
      unless enc.respond_to?(:to_str)
        raise TypeError, "no implicit conversion of #{enc.nil? ? 'nil' : enc.class} into String"
      end
      name = enc.to_str
      raise TypeError, "can't convert #{enc.class} to String" unless name.is_a?(String)
      e = begin
        Encoding.find(name)
      rescue ArgumentError
        nil
      end
      [e, e ? e.name : name]
    end

    def self.__not_found(src, dst)
      ConverterNotFoundError.new("code converter not found (#{src} to #{dst})")
    end

    # flags and replacement from the options of #new and String#encode
    def self.__econv_opts(opts)
      flags = 0
      return [0, nil] if opts.nil? || opts.empty?
      case opts[:invalid]
      when nil
      when :replace then flags |= INVALID_REPLACE
      else raise ArgumentError, "unknown value for invalid character option"
      end
      case opts[:undef]
      when nil
      when :replace then flags |= UNDEF_REPLACE
      else raise ArgumentError, "unknown value for undefined character option"
      end
      rep = nil
      if opts.key?(:replace)
        rep = opts[:replace]
        unless rep.nil?
          rep = rep.to_str if !rep.is_a?(String) && rep.respond_to?(:to_str)
          raise TypeError, "no implicit conversion of #{rep.class} into String" unless rep.is_a?(String)
        end
      end
      case opts[:xml]
      when nil
      when :text then flags |= XML_TEXT_DECORATOR | UNDEF_HEX_CHARREF
      when :attr then flags |= XML_ATTR_CONTENT_DECORATOR | XML_ATTR_QUOTE_DECORATOR | UNDEF_HEX_CHARREF
      else raise ArgumentError, "unexpected value for xml option: #{opts[:xml].inspect}"
      end
      case opts[:newline]
      when nil
      when :universal then flags |= UNIVERSAL_NEWLINE_DECORATOR
      when :crlf then flags |= CRLF_NEWLINE_DECORATOR
      when :cr then flags |= CR_NEWLINE_DECORATOR
      when :lf then flags |= LF_NEWLINE_DECORATOR
      else raise ArgumentError, "unexpected value for newline option: #{opts[:newline].inspect}"
      end
      flags |= UNIVERSAL_NEWLINE_DECORATOR if opts[:universal_newline]
      flags |= CRLF_NEWLINE_DECORATOR if opts[:crlf_newline]
      flags |= CR_NEWLINE_DECORATOR if opts[:cr_newline]
      flags |= LF_NEWLINE_DECORATOR if opts[:lf_newline]
      [flags, rep]
    end

    DECORATOR_NAMES = {
      UNIVERSAL_NEWLINE_DECORATOR => "universal_newline",
      XML_TEXT_DECORATOR => "xml_text_escape",
      XML_ATTR_CONTENT_DECORATOR => "xml_attr_content_escape",
      XML_ATTR_QUOTE_DECORATOR => "xml_attr_quote",
      CRLF_NEWLINE_DECORATOR => "crlf_newline",
      CR_NEWLINE_DECORATOR => "cr_newline",
      LF_NEWLINE_DECORATOR => "lf_newline",
    }.freeze
    private_constant :DECORATOR_NAMES

    def self.__decorators(flags)
      DECORATOR_NAMES.select { |bit, _| flags & bit != 0 }.map { |_, name| name }
    end

    def self.__convpath(sname, dname, flags)
      steps = __path(sname, dname)
      raise __not_found(sname, dname) if steps.nil?
      steps.map { |a, b| [Encoding.find(a), Encoding.find(b)] } + __decorators(flags)
    end

    def self.search_convpath(src, dst, **opts)
      _, sname = __enc_arg(src)
      _, dname = __enc_arg(dst)
      flags, = __econv_opts(opts)
      __convpath(sname, dname, flags)
    end

    def self.asciicompat_encoding(enc)
      e = if enc.is_a?(Encoding)
            enc
          else
            begin
              Encoding.find(enc.to_str)
            rescue ArgumentError
              nil
            end
          end
      return nil if e.nil?
      case e.name
      when "UTF-16BE", "UTF-16LE", "UTF-32BE", "UTF-32LE", "UTF-16", "UTF-32"
        Encoding::UTF_8
      when "ISO-2022-JP"
        Encoding.find("stateless-ISO-2022-JP")
      when "ISO-2022-JP-KDDI"
        Encoding.find("stateless-ISO-2022-JP-KDDI")
      end
    end

    # __open(sname, dname, flags, replacement): the converter String#encode
    # uses, names as String#encode resolves them ("" for decorators only)
    def self.__open(sname, dname, flags, rep = nil)
      ec = allocate
      ec.__send__(:__init, sname, dname, flags, rep)
      ec
    end

    def initialize(src, dst, *rest, **opts)
      raise ArgumentError, "wrong number of arguments (given #{rest.size + 2}, expected 2..3)" if rest.size > 1
      flags = 0
      rep = nil
      if rest.size == 1
        opt = rest[0]
        if opt.is_a?(Integer)
          flags = opt
        elsif opt.nil?
        else
          opts = opt.to_hash.merge(opts)
        end
      end
      unless opts.empty?
        f, rep = Converter.__econv_opts(opts)
        flags |= f
      end
      senc, sname = Converter.__enc_arg(src)
      denc, dname = Converter.__enc_arg(dst)
      raise Converter.__not_found(sname, dname) if senc && senc.equal?(denc)
      __init(sname, dname, flags, opts.key?(:replace) ? rep : nil, opts.key?(:replace))
    end

    def __init(sname, dname, flags, rep, rep_given = !rep.nil?)
      raise Converter.__not_found(sname, dname) unless __setup(sname, dname, flags)
      @source_encoding = sname.empty? ? nil : Encoding.find(sname)
      @destination_encoding = dname.empty? ? nil : Encoding.find(dname)
      @flags = flags
      @finished = false
      self.replacement = rep if rep_given && !rep.nil?
    end
    private :__init

    attr_reader :source_encoding, :destination_encoding

    def inspect
      "#<Encoding::Converter: #{@source_encoding&.name} to #{@destination_encoding&.name}>"
    end

    def convpath
      sname, dname, = __names
      Converter.__convpath(sname, dname, @flags)
    end

    def replacement
      bytes, enc = __replacement
      bytes.force_encoding(enc)
    end

    def replacement=(str)
      str = str.to_str if !str.is_a?(String) && str.respond_to?(:to_str)
      raise TypeError, "no implicit conversion of #{str.nil? ? 'nil' : str.class} into String" unless str.is_a?(String)
      unless __set_replacement(str.b, str.encoding.name)
        sname, dname, = __names
        raise UndefinedConversionError, "#{str.dump} from #{str.encoding} to #{dname}"
      end
      str
    end

    def primitive_convert(src, dst, dst_start = nil, dst_size = nil, opt = nil, partial_input: nil, after_output: nil)
      raise FrozenError, "can't modify frozen String: #{dst.inspect}" if dst.frozen?
      flags = 0
      if opt.is_a?(Integer)
        flags = opt
      elsif opt
        h = opt.to_hash
        partial_input = h[:partial_input] if partial_input.nil?
        after_output = h[:after_output] if after_output.nil?
      end
      flags |= PARTIAL_INPUT if partial_input
      flags |= AFTER_OUTPUT if after_output
      start = dst_start.nil? ? dst.bytesize : dst_start.to_int
      raise ArgumentError, "dstsize is too small" if start > dst.bytesize
      raise ArgumentError, "negative offset" if start < 0
      size = dst_size.nil? ? -1 : dst_size.to_int
      input = src.nil? ? nil : src.b
      status, consumed, out = __convert(input, size, flags)
      if src && consumed > 0
        enc = src.encoding
        src.replace(src.b.byteslice(consumed, src.bytesize - consumed))
        src.force_encoding(enc)
      end
      result = dst.b.byteslice(0, start) + out
      dst.replace(result)
      dst.force_encoding(@destination_encoding) if @destination_encoding
      status
    end

    def convert(str)
      raise ArgumentError, "convert() called after finish()" if @finished
      str = str.to_str unless str.is_a?(String)
      src = str.b
      dst = String.new
      loop do
        status = primitive_convert(src, dst, nil, nil, PARTIAL_INPUT)
        case status
        when :source_buffer_empty, :finished then break
        when :destination_buffer_full, :after_output then next
        else raise last_error
        end
      end
      dst
    end

    def finish
      dst = String.new
      @finished = true
      loop do
        status = primitive_convert(nil, dst, nil, nil, 0)
        case status
        when :finished then break
        when :destination_buffer_full, :after_output then next
        else raise last_error
        end
      end
      dst
    end

    def insert_output(str)
      str = str.to_str unless str.is_a?(String)
      unless __insert(str.b, str.encoding.name)
        raise ArgumentError, "too big fallback string"
      end
      nil
    end

    def putback(max = nil)
      bytes = __putback(max.nil? ? -1 : max.to_int)
      bytes.force_encoding(@source_encoding) if @source_encoding
      bytes
    end

    def primitive_errinfo
      info = __errinfo
      if info[3]
        info[3] = info[3].b
        info[4] = info[4].b
      end
      info
    end

    def last_error
      status, esrc, edst, ebytes, ragain = __errinfo
      sname, dname, *path = __names
      case status
      when :invalid_byte_sequence, :incomplete_input
        msg = if status == :incomplete_input
                "incomplete #{ebytes.dump} on #{esrc}"
              elsif ragain.empty?
                "#{ebytes.dump} on #{esrc}"
              else
                "#{ebytes.dump} followed by #{ragain.dump} on #{esrc}"
              end
        e = InvalidByteSequenceError.new(msg)
        e.instance_variable_set(:@error_bytes, ebytes.b)
        e.instance_variable_set(:@readagain_bytes, ragain.b)
        e.instance_variable_set(:@incomplete_input, status == :incomplete_input)
      when :undefined_conversion
        char = ebytes.dup.force_encoding(esrc) rescue ebytes
        dumped = esrc == "UTF-8" ? format("U+%04X", char.ord) : ebytes.dump
        msg = if esrc == sname && edst == dname
                "#{dumped} from #{esrc} to #{edst}"
              else
                "#{dumped} to #{edst} in conversion from #{([sname] + path).join(' to ')}"
              end
        e = UndefinedConversionError.new(msg)
        e.instance_variable_set(:@error_char, char)
      else
        return nil
      end
      e.instance_variable_set(:@source_encoding_name, esrc)
      e.instance_variable_set(:@destination_encoding_name, edst)
      e.instance_variable_set(:@source_encoding, (Encoding.find(esrc) rescue nil))
      e.instance_variable_set(:@destination_encoding, (Encoding.find(edst) rescue nil))
      e
    end
  end
end

class String
  def encode(*args, **opts)
    r = __transcode(args, opts)
    r.equal?(self) ? dup : r
  end

  def encode!(*args, **opts)
    raise FrozenError, "can't modify frozen String: #{inspect}" if frozen?
    r = __transcode(args, opts)
    unless r.equal?(self)
      enc = r.encoding
      replace(r)
      force_encoding(enc)
    end
    self
  end

  # MRI's str_transcode0: self when nothing is to be done, else a new String
  def __transcode(args, opts)
    if args.size > 2
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)"
    end
    if args.size == 1 && opts.empty? && args[0].is_a?(Hash)
      opts = args.pop
    end
    flags, rep = Encoding::Converter.__econv_opts(opts)
    explicitly_invalid_replace = true
    if args.empty?
      to = Encoding.default_internal
      if to.nil?
        return self if flags == 0
        to = encoding
      end
      explicitly_invalid_replace = false if flags & Encoding::Converter::INVALID_MASK == 0
      flags |= Encoding::Converter::INVALID_REPLACE | Encoding::Converter::UNDEF_REPLACE
    else
      to = args[0]
    end
    denc, dname = Encoding::Converter.__enc_arg(to)
    if args.size < 2
      senc = encoding
      sname = senc.name
    else
      senc, sname = Encoding::Converter.__enc_arg(args[1])
    end
    str = self
    decorators = Encoding::Converter::XML_TEXT_DECORATOR | Encoding::Converter::XML_ATTR_CONTENT_DECORATOR |
                 Encoding::Converter::XML_ATTR_QUOTE_DECORATOR | 0x00007f00
    if flags & decorators == 0
      if senc && senc.equal?(denc)
        if flags & Encoding::Converter::INVALID_MASK != 0 && explicitly_invalid_replace
          source = encoding.equal?(senc) ? self : dup.force_encoding(senc)
          return source.scrub(rep)
        end
        return args.size < 2 ? self : dup.force_encoding(denc)
      end
      if senc && denc && senc.ascii_compatible? && denc.ascii_compatible? && ascii_only?
        return dup.force_encoding(denc)
      end
      return (args.size < 2 ? self : dup.force_encoding(denc)) if sname.casecmp?(dname)
    else
      if senc && denc && !senc.ascii_compatible? && !denc.ascii_compatible?
        str = dup.force_encoding(senc).encode(Encoding::UTF_8)
        senc = Encoding::UTF_8
        sname = "UTF-8"
      end
      sname = dname = "" if sname.casecmp?(dname)
    end
    fallback = opts[:fallback]
    ec = Encoding::Converter.__open(sname, dname, flags, rep)
    src = str.b
    dst = String.new
    loop do
      status = ec.primitive_convert(src, dst, nil, nil, 0)
      case status
      when :finished then break
      when :undefined_conversion
        raise ec.last_error if fallback.nil?
        char = ec.last_error.error_char
        r = case fallback
            when Hash then fallback[char]
            when Proc, Method then fallback.call(char)
            else fallback.respond_to?(:[]) ? fallback[char] : nil
            end
        raise ec.last_error if r.nil?
        unless r.is_a?(String)
          raise TypeError, "no implicit conversion of #{r.class} into String" unless r.respond_to?(:to_str)
          r = r.to_str
        end
        ec.insert_output(r)
      else
        raise ec.last_error
      end
    end
    dst.force_encoding(denc || Encoding::BINARY)
  end
  private :__transcode

  alias __mrb_scrub scrub
  def scrub(rep = nil, &block)
    enc = encoding
    return __mrb_scrub(rep, &block) if enc.equal?(Encoding::UTF_8) || enc.equal?(Encoding::BINARY)
    spans = Encoding::Converter.__invalid_spans(b, enc.name)
    return dup if spans.nil? || spans.empty?
    if rep.nil? && !block
      rep = Encoding::Converter.__unicode?(enc) ? "�".encode(enc) : "?".encode(enc)
    end
    bytes = b
    out = String.new
    pos = 0
    spans.each_slice(2) do |start, len|
      out << bytes.byteslice(pos, start - pos)
      bad = bytes.byteslice(start, len).force_encoding(enc)
      out << (block ? block.call(bad) : rep).b
      pos = start + len
    end
    out << bytes.byteslice(pos, bytes.bytesize - pos)
    out.force_encoding(enc)
  end

  def scrub!(rep = nil, &block)
    r = scrub(rep, &block)
    return self if r.bytesize == bytesize && r == self  # nothing to replace: no modification
    enc = r.encoding
    replace(r)
    force_encoding(enc)
  end

  alias __enc_valid_encoding? valid_encoding?
  def valid_encoding?
    enc = encoding
    return __enc_valid_encoding? if enc.equal?(Encoding::UTF_8) || enc.equal?(Encoding::BINARY) || enc.equal?(Encoding::US_ASCII)
    spans = Encoding::Converter.__invalid_spans(b, enc.name)
    spans.nil? || spans.empty?
  end
end

class Encoding::Converter
  def self.__unicode?(enc)
    %w[UTF-8 UTF8-MAC UTF-16BE UTF-16LE UTF-32BE UTF-32LE UTF-16 UTF-32].include?(enc.name)
  end
end
