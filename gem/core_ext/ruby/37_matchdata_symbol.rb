# MatchData and Symbol MRI compatibility extensions

if Object.const_defined?(:MatchData)
  class MatchData
    class << self
      undef_method :allocate if method_defined?(:allocate)
    end
    # DefineGoClass exposes Go's IsEqual as #equal?, which shadows BasicObject#equal?
    remove_method :equal? if instance_methods(false).include?(:equal?)

    # #string, #pre_match, #post_match, #to_a, #captures and #[] give the
    # subject's encoding in Go (gem/regexp, mdStr)

    def values_at(*args)
      arr = to_a
      out = []
      args.each do |a|
        if Symbol === a || String === a
          out << self[a]
        elsif Range === a
          n = arr.size
          b = a.begin.nil? ? 0 : Kernel.__to_int(a.begin)
          b += n if b < 0
          raise RangeError, "#{a} out of range" if b < 0
          if a.end.nil?
            e = n - 1
          else
            e = Kernel.__to_int(a.end)
            e += n if e < 0
            e -= 1 if a.exclude_end?
          end
          next if e < b
          (b..e).each { |i| out << (i < n ? arr[i] : nil) }
        else
          i = Kernel.__to_int(a)
          out << arr[i]
        end
      end
      out
    end

    def names
      regexp.names
    end

    def named_captures(symbolize_names: false)
      nc = regexp.named_captures
      out = {}
      nc.each do |name, indices|
        val = nil
        indices.each do |idx|
          v = self[idx]
          val = v unless v.nil?
        end
        key = symbolize_names ? name.to_sym : name
        out[key] = val
      end
      out
    end

    def deconstruct_keys(keys)
      nc = named_captures(symbolize_names: true)
      return nc if keys.nil?
      raise TypeError, "wrong argument type #{Kernel.__cname(keys)} (expected Array)" unless Array === keys
      return {} if keys.size > nc.size
      res = {}
      keys.each do |k|
        raise TypeError, "wrong argument type #{Kernel.__cname(k)} (expected Symbol)" unless Symbol === k
        break unless nc.key?(k)
        res[k] = nc[k]
      end
      res
    end

    def match(n)
      self[n]
    end

    def match_length(n)
      s = self[n]
      s ? s.length : nil
    end

    def integer_at(n, base = 10)
      s = self[n]
      return nil if s.nil?
      base = Kernel.__to_int(base)
      if base == 0
        s = s.lstrip
        sign = 1
        if s.start_with?("-")
          sign = -1
          s = s[1..]
        elsif s.start_with?("+")
          s = s[1..]
        end
        # the prefix and its digits without captures: this core code must
        # leave the caller's $~ alone
        radix, at = if s.match?(/\A0[xX]\h/) then [16, 2]
                    elsif s.match?(/\A0[bB][01]/) then [2, 2]
                    elsif s.match?(/\A0[oO][0-7]/) then [8, 2]
                    elsif s.match?(/\A0[dD]\d/) then [10, 2]
                    elsif s.match?(/\A0[0-7]/) then [8, 1]
                    elsif s.match?(/\A\d/) then [10, 0]
                    end
        return nil unless radix
        digits = "0123456789abcdef"[0, radix]
        n = at
        n += 1 while n < s.size && digits.include?(s[n].downcase)
        return s[at...n].to_i(radix) * sign
      end
      raise ArgumentError, "invalid radix #{base}" if base < 2 || base > 36
      digits = "0123456789abcdefghijklmnopqrstuvwxyz"[0, base]
      s = s.lstrip
      sign = 1
      if s.start_with?("-")
        sign = -1
        s = s[1..]
      elsif s.start_with?("+")
        s = s[1..]
      end
      return nil if s.empty? || !digits.include?(s[0].downcase)
      s.to_i(base) * sign
    end

    def ==(other)
      return true if equal?(other)
      return false unless MatchData === other
      return false unless string == other.string && regexp == other.regexp && size == other.size
      (0...size).all? { |i| byteoffset(i) == other.byteoffset(i) }
    end
    alias eql? ==
  end
end

class String
  alias __orig_str_to_sym to_sym
  def to_sym
    sym = __orig_str_to_sym
    enc = encoding
    if !enc.ascii_compatible? || enc.dummy? || (!ascii_only? && enc != Encoding::UTF_8)
      Symbol.__sym_encodings[sym] = enc
    end
    sym
  end
  alias intern to_sym
end

class Symbol
  @__sym_encodings = {}
  @__sym_names = {}

  def self.__sym_encodings
    @__sym_encodings
  end

  def self.__sym_names
    @__sym_names
  end

  alias __orig_sym_to_s to_s
  alias __orig_sym_inspect inspect

  def encoding
    if (e = Symbol.__sym_encodings[self])
      return e
    end
    s = __orig_sym_to_s
    return Encoding::US_ASCII if s.ascii_only?
    s.valid_encoding? ? Encoding::UTF_8 : Encoding::BINARY
  end

  # one frozen String per Symbol, in its encoding, for to_s and name alike
  # (MRI 4.1 returns the same object)
  def to_s
    Symbol.__sym_names[self] ||= begin
      s = __orig_sym_to_s
      e = encoding
      if e.equal?(Encoding::US_ASCII)
        s = s.dup.__send__(:__encoding_us_ascii!)
      elsif !s.encoding.equal?(e)
        s = s.dup.force_encoding(e)
      end
      s.freeze
    end
  end
  alias id2name to_s
  alias name to_s

  alias === ==

  def [](*args)
    to_s[*args]
  end
  alias slice []

  def start_with?(*args)
    to_s.start_with?(*args)
  end

  def end_with?(*args)
    to_s.end_with?(*args)
  end

  def casecmp(other)
    return nil unless Symbol === other
    to_s.casecmp(other.to_s)
  end

  def casecmp?(other)
    return nil unless Symbol === other
    a = to_s
    b = other.to_s
    return nil unless Encoding.compatible?(a, b)
    a.casecmp?(b)
  end

  def upcase(*opts)
    to_s.upcase(*opts).to_sym
  end

  def downcase(*opts)
    to_s.downcase(*opts).to_sym
  end

  def capitalize(*opts)
    to_s.capitalize(*opts).to_sym
  end

  def swapcase(*opts)
    to_s.swapcase(*opts).to_sym
  end

  def inspect
    enc = encoding
    raw = __orig_sym_to_s
    if enc.dummy?
      return ':"' + raw.bytes.map { |b| format('\\x%02x', b) }.join + '"'
    end
    unless enc.ascii_compatible?
      return ':"' + raw.delete("\0") + '"'
    end
    return ':"&&"' if raw == "&&"
    return ':"||"' if raw == "||"
    # match?, not =~: this core code must leave the caller's $~ alone; the
    # name in the symbol's own encoding (a BINARY one is no broken UTF-8)
    raw = raw.dup.force_encoding(enc) unless raw.encoding == enc
    return ":#{raw}" if raw.match?(/\A[A-Z_][A-Za-z0-9_]*[!?]\z/)
    if enc == Encoding::UTF_8 && Encoding.default_external == Encoding::UTF_8 && raw.valid_encoding? && !raw.ascii_only?
      unless raw.match?(/[\x00-\x20"#'(),.:;<>?\[\\\]`{|}~]/)
        return ":#{raw}"
      end
    end
    __orig_sym_inspect
  end

  # Symbol#to_proc as MRI's: a lambda calling the public method on its first
  # argument ("no receiver given" without one). Kernel#public_send is bound
  # only for an object that is not a Kernel (a BasicObject). Proc#arity,
  # #parameters and #source_location tell these lambdas by their location.
  PUB_SEND = Kernel.instance_method(:public_send)
  def to_proc
    mid = self
    ->(obj = (no_recv = true; nil), *args, **opts, &block) do
      ::Kernel.raise ArgumentError, "no receiver given" if no_recv
      if Kernel === obj
        opts.empty? ? obj.public_send(mid, *args, &block) : obj.public_send(mid, *args, **opts, &block)
      else
        opts.empty? ? PUB_SEND.bind_call(obj, mid, *args, &block) : PUB_SEND.bind_call(obj, mid, *args, **opts, &block)
      end
    end
  end
end

class String
  alias __oruby_sw_orig start_with?
  def start_with?(*prefixes)
    prefixes.any? do |p|
      if Regexp === p
        m = p.match(self)
        if m && m.begin(0) == 0
          true
        else
          $~ = nil
          false
        end
      else
        s = String === p ? p : Kernel.__implicit(p, String)
        unless Encoding.compatible?(self, s)
          raise Encoding::CompatibilityError, "incompatible character encodings: #{encoding} and #{s.encoding}"
        end
        next true if s.empty?
        next false if s.bytesize > bytesize
        next false unless byteslice(0, s.bytesize).b == s.b
        if !ascii_only? && encoding == Encoding::UTF_8
          # ensure match ends on a character boundary
          pos = 0
          ok = false
          each_char do |c|
            pos += c.bytesize
            if pos == s.bytesize
              ok = true
              break
            end
            break if pos > s.bytesize
          end
          ok
        else
          true
        end
      end
    end
  end

  alias __oruby_ew_orig end_with?
  def end_with?(*suffixes)
    suffixes.any? do |p|
      s = String === p ? p : Kernel.__implicit(p, String)
      unless Encoding.compatible?(self, s)
        raise Encoding::CompatibilityError, "incompatible character encodings: #{encoding} and #{s.encoding}"
      end
      next true if s.empty?
      next false if s.bytesize > bytesize
      start = bytesize - s.bytesize
      next false unless byteslice(start, s.bytesize).b == s.b
      if !ascii_only? && encoding == Encoding::UTF_8
        pos = 0
        ok = (start == 0)
        unless ok
          each_char do |c|
            pos += c.bytesize
            if pos == start
              ok = true
              break
            end
            break if pos > start
          end
        end
        ok
      else
        true
      end
    end
  end
end

class Proc
  # a lambda made by Symbol#to_proc: its body is the one literal there
  def __oruby_sym_proc?
    loc = __orig_proc_source_location
    return false unless loc
    @@__oruby_sym_proc_loc ||= :to_s.to_proc.__orig_proc_source_location
    loc == @@__oruby_sym_proc_loc
  end

  alias __orig_proc_arity arity
  def arity
    return -2 if __oruby_sym_proc?
    __orig_proc_arity
  end

  alias __orig_proc_parameters parameters
  # MRI's parameters(lambda: nil): go-mrb.c's __oruby_params
  def parameters(lambda: nil)
    return [[:req], [:rest]] if __oruby_sym_proc?
    m = @__oruby_method
    return m.parameters if m
    __oruby_params(lambda.nil? ? -1 : (lambda ? 1 : 0))
  end

  alias __orig_proc_source_location source_location
  def source_location
    return nil if __oruby_sym_proc?
    m = @__oruby_method
    return m.source_location if m
    __orig_proc_source_location
  end
end
