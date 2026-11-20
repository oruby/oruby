# MRI core methods mruby lacks
module Enumerable
  def slice_when(&block)
    raise ArgumentError, "tried to create Proc object without a block" unless block
    chunk_while { |a, b| !block.call(a, b) }
  end
end

class Module
  private def ruby2_keywords(*names)
    names.each do |name|
      sym = case name
            when Symbol then name
            when String then name.to_sym
            else raise TypeError, "#{name.inspect} is not a symbol nor a string"
            end
      orig = instance_method(sym)
      # MRI's: only a method taking *args, without keywords or arguments
      # after the splat, gets the flag
      params = orig.parameters
      rest = params.index { |t, _| t == :rest }
      if rest.nil? || params.any? { |t, _| t == :key || t == :keyreq || t == :keyrest || t == :nokey } ||
         params[(rest + 1)..].any? { |t, _| t == :req || t == :opt }
        warn "Skipping set of ruby2_keywords flag for #{sym} (method accepts keywords or post arguments " \
             "or method does not accept argument splat)", uplevel: 0
        next
      end
      priv = private_method_defined?(sym)
      prot = protected_method_defined?(sym)
      define_method(sym) do |*args, **kw, &blk|
        args << Hash.ruby2_keywords_hash(kw) unless kw.empty?
        orig.bind_call(self, *args, &blk)
      end
      private(sym) if priv
      protected(sym) if prot
    end
    nil
  end
end

class Proc
  # MRI's: only a proc taking *args, without keywords or arguments after
  # the splat, gets the flag (the flag itself is not modelled)
  def ruby2_keywords
    params = parameters
    rest = params.index { |t, _| t == :rest }
    if rest.nil? || params.any? { |t, _| t == :key || t == :keyreq || t == :keyrest || t == :nokey } ||
       params[(rest + 1)..].any? { |t, _| t == :req || t == :opt }
      warn "Skipping set of ruby2_keywords flag for proc (proc accepts keywords or post arguments " \
           "or proc does not accept argument splat)", uplevel: 0
    end
    self
  end

  # BINARY, as MRI's
  alias_method :__oruby_proc_to_s, :to_s
  # a Symbol#to_proc lambda names its Symbol, a Method#to_proc one has no
  # source location
  def to_s
    s = __oruby_proc_to_s
    if (sym = __oruby_symproc_sym)
      s = "#{s[/\A#<Proc:0x\h+/]}(&#{sym.inspect}) (lambda)>"
    elsif instance_variable_defined?(:@__oruby_method)
      s = "#{s[/\A#<Proc:0x\h+/]} (lambda)>"
    end
    s.force_encoding(Encoding::BINARY)
  end
  alias inspect to_s
end

module Kernel
  # Kernel#method for a public method only: MRI's NameError for a private
  # or protected one
  def public_method(name)
    sym = Symbol === name ? name : Kernel.__implicit(name, String).to_sym
    cls = Kernel.instance_method(:class).bind_call(self)
    vis = if singleton_class.private_method_defined?(sym) then "private"
          elsif singleton_class.protected_method_defined?(sym) then "protected"
          end
    raise NameError.new("method '#{sym}' for class '#{cls}' is #{vis}", sym, receiver: self) if vis
    method(sym)
  end
end

class Integer
  # bit reference: int[n], int[n, len], int[range]
  def [](i, len = (no_len = true; nil))
    if no_len
      if Range === i
        b = i.begin.nil? ? 0 : Kernel.__to_int(i.begin)
        if i.end.nil?
          raise ArgumentError, "The beginning of the range should be 0" if b < 0 && self != 0
          return self >> b
        end
        e = Kernel.__to_int(i.end)
        e -= 1 if i.exclude_end?
        return self >> b if e < b && b >= 0
        len = e - b + 1
        return (self >> b) & ((1 << len) - 1) if len > 0
        return self >> b
      end
      i = Kernel.__to_int(i)
      return 0 if i < 0
      return (self >> i) & 1
    end
    i = Kernel.__to_int(i)
    len = Kernel.__to_int(len)
    raise ArgumentError, "negative length" if len < 0 && false
    return self >> i if len < 0
    (self >> i) & ((1 << len) - 1)
  end
end

class Exception
  def full_message(highlight: (hl_unset = true; nil), order: :top, **options)
    highlight = Exception.to_tty? if hl_unset
    unless highlight == true || highlight == false
      raise ArgumentError, "expected true or false as highlight: #{highlight.inspect}"
    end
    unless order == :top || order == :bottom || order.nil?
      raise ArgumentError, "expected :top or :bottom as order: #{order.inspect}"
    end
    order ||= :top

    options[:highlight] = highlight
    here = caller(1)
    limit = Thread::Backtrace.limit
    # the exception, then its causes; a cause seen before ends the chain
    parts = []
    seen = {}
    e = self
    while e.is_a?(Exception) && !seen[e.__id__]
      seen[e.__id__] = true
      parts << e.__send__(:__oruby_message_part, options, order, limit, here)
      e = e.cause
    end
    return parts.join if order == :top
    tb = highlight ? "\e[1mTraceback\e[m (most recent call last):\n" : "Traceback (most recent call last):\n"
    tb + parts.reverse.join
  end

  # One exception of full_message: its message and backtrace, the "from"
  # lines cut to `limit` (--backtrace-limit) as MRI's print_backtrace cuts
  private def __oruby_message_part(options, order, limit, here)
    highlight = options[:highlight]
    if respond_to?(:detailed_message)
      head = detailed_message(**options)
      if head.nil?
        cls = self.class.to_s
        head = highlight ? "\e[1;4m#{cls}\e[m" : cls
      else
        head = head.respond_to?(:to_str) ? head.to_str : head.to_s
      end
    else
      cls = self.class.to_s
      head = highlight ? "\e[1;4m#{cls}\e[m" : cls
    end

    bt = backtrace
    bt = here if bt.nil? || bt.empty?
    top = bt.first ? "#{bt.first}: " : ""
    rest = bt.drop(1)
    tail = nil
    if limit >= 0 && bt.size > limit + 2
      tail = "\t ... #{rest.size - limit} levels...\n"
      rest = rest.first(limit)
    end
    if order == :top
      "#{top}#{head}\n" + rest.map { |l| "\tfrom #{l}\n" }.join + tail.to_s
    else
      w = rest.size.to_s.size
      tail.to_s + rest.each_with_index.map { |l, i| "\t#{(i + 1).to_s.rjust(w)}: from #{l}\n" }.reverse.join + "#{top}#{head}\n"
    end
  end
end

class String
  def lines(sep = $/, chomp: false, &block)
    if block
      each_line(sep, chomp: chomp, &block)
      return self
    end
    __lines(sep, chomp)
  end

  def each_line(sep = $/, chomp: false, &block)
    return Enumerator.new { |y| __lines(sep, chomp).each { |l| y << l } } unless block
    __lines(sep, chomp).each(&block)
    self
  end

  private def __lines(sep, chomp)
    return [self.dup] if sep.nil? && !empty?
    return [] if empty?
    sep = Kernel.__implicit(sep, String)
    if __encoding_tag
      enc = encoding
      # a dummy wide encoding has no lines; UTF-7 has no converter
      return [dup] if enc.equal?(Encoding::UTF_16) || enc.equal?(Encoding::UTF_32)
      raise Encoding::ConverterNotFoundError, "code converter not found (UTF-8 to UTF-7)" if enc.equal?(Encoding::UTF_7)
    end
    out = []
    if sep.empty? # paragraph mode: up to two newlines, the ones after skipped
      rest = self
      while (m = /\r?\n\r?\n/.__match_raw(rest))
        out << (chomp ? m.pre_match : m.pre_match + m[0])
        rest = m.post_match
        if (s = /\A(?:\r?\n)+/.__match_raw(rest))
          rest = s.post_match
        end
      end
      out << rest unless rest.empty?
      return out
    end
    pos = 0
    n = bytesize
    sl = sep.bytesize
    while pos < n
      i = byteindex(sep, pos)
      if i.nil?
        out << byteslice(pos, n - pos)
        break
      end
      line = byteslice(pos, i + sl - pos)
      if chomp
        line = byteslice(pos, i - pos)
        line = line.byteslice(0, line.bytesize - 1) if sep == "\n" && line.end_with?("\r")
      end
      out << line
      pos = i + sl
    end
    out
  end

  # case mapping options: :ascii, :turkic, :lithuanian, :fold
  %i[downcase upcase capitalize swapcase].each do |m|
    um = instance_method(m)
    define_method(m) do |*opts|
      String.__case_opts(opts, m)
      enc = encoding
      # an ASCII-incompatible encoding (UTF-16/32) maps as UTF-8
      return encode(Encoding::UTF_8).__send__(m, *opts).encode(enc) unless enc.ascii_compatible? || enc.dummy?
      if opts.include?(:fold)
        String.__fold(um.bind_call(self))
      elsif opts.include?(:ascii)
        String.__ascii_case(self, m)
      elsif opts.include?(:turkic)
        String.__turkic_case(self, m, um)
      else
        um.bind_call(self)
      end
    end
    bm = :"#{m}!"
    bum = instance_method(bm)
    define_method(bm) do |*opts|
      String.__case_opts(opts, m)
      if opts.include?(:ascii) || opts.include?(:turkic) || opts.include?(:fold) || !(encoding.ascii_compatible? || encoding.dummy?)
        r = __send__(m, *opts)
        return nil if r == self
        replace(r)
      else
        bum.bind_call(self)
      end
    end
  end

  # full case folding beyond downcase (Unicode CaseFolding.txt, status F)
  FOLD_FULL = {
    "\u00DF" => "ss", "\u1E9E" => "ss", "\u0149" => "\u02BCn", "\u01F0" => "j\u030C",
    "\u0390" => "\u03B9\u0308\u0301", "\u03B0" => "\u03C5\u0308\u0301", "\u0587" => "\u0565\u0582",
    "\u1E96" => "h\u0331", "\u1E97" => "t\u0308", "\u1E98" => "w\u030A", "\u1E99" => "y\u030A",
    "\u1E9A" => "a\u02BE", "\uFB00" => "ff", "\uFB01" => "fi", "\uFB02" => "fl", "\uFB03" => "ffi",
    "\uFB04" => "ffl", "\uFB05" => "st", "\uFB06" => "st",
  }.freeze
  private_constant :FOLD_FULL

  def self.__fold(s)
    return s if s.ascii_only?
    s.gsub(/[\u00DF\u1E9E\u0149\u01F0\u0390\u03B0\u0587\u1E96-\u1E9A\uFB00-\uFB06]/) { |c| FOLD_FULL[c] || c }
  end

  def self.__case_opts(opts, m)
    raise ArgumentError, "too many options" if opts.size > 2
    opts.each do |o|
      unless o == :ascii || o == :turkic || o == :lithuanian || (o == :fold && m == :downcase)
        raise ArgumentError, "invalid option: #{o.inspect}" if Symbol === o
        raise TypeError, "no implicit conversion of #{Kernel.__cname(o)} into Symbol"
      end
    end
    if opts.size == 2 && !(opts.include?(:turkic) && opts.include?(:lithuanian))
      raise ArgumentError, "too many options"
    end
  end

  def self.__ascii_case(s, m)
    # tr's arguments are UTF-8, which a UTF-16/32 string cannot take
    enc = s.encoding
    return __ascii_case(s.encode(Encoding::UTF_8), m).encode(enc) unless enc.ascii_compatible?
    case m
    when :downcase then s.tr("A-Z", "a-z")
    when :upcase then s.tr("a-z", "A-Z")
    when :swapcase then s.tr("a-zA-Z", "A-Za-z")
    else
      s.empty? ? s.dup : s[0].tr("a-z", "A-Z") + s[1..].tr("A-Z", "a-z")
    end
  end

  def self.__turkic_case(s, m, um)
    case m
    when :downcase then um.bind_call(s.gsub("I", "ı").gsub("İ", "i"))
    when :upcase then um.bind_call(s.gsub("i", "İ"))
    when :swapcase
      s.each_char.map { |c| c == "i" ? "İ" : c == "I" ? "ı" : c == "İ" ? "i" : c == "ı" ? "I" : c.swapcase }.join
    else
      return s.dup if s.empty?
      f = s[0] == "i" ? "İ" : s[0].upcase
      f + __turkic_case(s[1..], :downcase, String.instance_method(:downcase))
    end
  end
end

class Enumerator
  def self.product(*enums, **kw, &block)
    raise ArgumentError, "unknown keywords: #{kw.keys.map(&:inspect).join(', ')}" unless kw.empty?
    e = Product.new(*enums)
    return e.each(&block) && nil if block
    e
  end

  class Product < Enumerator
    def initialize(*enums)
      raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
      @__enums = enums
      @__init = true
      super() { |y| __each { |v| y << v } }
      self
    end
    private :initialize

    def initialize_copy(other)
      return self if equal?(other)
      raise FrozenError, "can't modify frozen #{self.class}" if frozen?
      raise TypeError, "initialize_copy should take same class object" unless self.class == other.class
      raise ArgumentError, "uninitialized product" unless other.instance_variable_get(:@__init)
      @__enums = other.instance_variable_get(:@__enums).dup
      @__init = true
      super
      self
    end
    private :initialize_copy

    def each(&block)
      return self unless block
      __each(&block)
      self
    end

    def size
      total = 1
      @__enums.each do |e|
        return nil unless e.respond_to?(:size)
        s = e.size
        return nil if s.nil? || (Float === s && s.nan?)
        return nil unless Integer === s || (Float === s && s.infinite?)
        total *= s
      end
      total
    end

    def rewind
      @__enums.each { |e| e.rewind if e.respond_to?(:rewind) }
      super
    end

    INSPECT_GUARD = {}
    def inspect
      return "#<#{self.class}: uninitialized>" unless @__init
      oid = object_id
      return "#<#{self.class}: ...>" if INSPECT_GUARD[oid]
      INSPECT_GUARD[oid] = true
      begin
        "#<#{self.class}: #{@__enums.inspect}>"
      ensure
        INSPECT_GUARD.delete(oid)
      end
    end
    alias to_s inspect

    private def __each(&block)
      enums = @__enums
      if enums.empty?
        block.call([])
        return
      end
      rec = lambda do |i, acc|
        if i == enums.size
          block.call(acc.dup)
        else
          enums[i].each_entry { |v| rec.call(i + 1, acc + [v]) }
        end
      end
      rec.call(0, [])
    end
  end
end

# Random#bytes / Random.bytes return binary strings
class Random
  if method_defined?(:bytes)
    alias __oruby_bytes bytes
    def bytes(n)
      __oruby_bytes(Kernel.__to_int(n)).force_encoding(Encoding::BINARY)
    end
  end
  class << self
    if method_defined?(:bytes)
      alias __oruby_bytes bytes
      def bytes(n)
        __oruby_bytes(Kernel.__to_int(n)).force_encoding(Encoding::BINARY)
      end
    end
  end
end

class Symbol
  def match(*args, &block) = to_s.match(*args, &block)
  def match?(*args) = to_s.match?(*args)
  def =~(other) = to_s =~ other
end

# open(2) flags MRI defines everywhere (0 where the platform has no such flag)
module File::Constants
  BINARY = 0 unless const_defined?(:BINARY)
  SHARE_DELETE = 0 unless const_defined?(:SHARE_DELETE)
end
class IO
  BINARY = File::Constants::BINARY unless const_defined?(:BINARY, false)
end



# mruby's bind_call passes keywords as a positional Hash; bind(...).call
# keeps them apart
class UnboundMethod
  def bind_call(recv, *args, **kw, &block)
    kw.empty? ? bind(recv).call(*args, &block) : bind(recv).call(*args, **kw, &block)
  end
end

# spawn / system / exec argument checks as MRI does them before the Go
# runner (gem/process) sees them: #to_str, null bytes, [command, argv0]
module Process
  # MRI's rb_exec_getargs: [env (Hash or #to_hash),] command (String or
  # [cmdname, argv0], #to_ary), args... [, options Hash]
  def self.__cmd_args(args)
    out = args.dup
    if !(Hash === out[0]) && out.size > 1 && !(String === out[0]) && Kernel.__responds?(out[0], :to_hash)
      h = out[0].to_hash
      out[0] = h if Hash === h
    end
    i = Hash === out[0] ? 1 : 0
    # an env and an options Hash but no command
    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if out.size <= i || (out.size == i + 1 && Hash === out[i])
    last = out.size
    last -= 1 if last > i + 1 && Hash === out[-1]
    c = out[i]
    c = c.to_ary if !(Array === c) && !(String === c) && Kernel.__responds?(c, :to_ary)
    if Array === c
      raise ArgumentError, "wrong first argument" unless c.size == 2
      out[i] = c.map { |x| __cmd_str(x) }
    else
      out[i] = __cmd_str(c)
    end
    ((i + 1)...last).each { |j| out[j] = __cmd_str(out[j]) }
    out
  end

  def self.__cmd_str(x)
    s = Kernel.__implicit(x, String)
    raise ArgumentError, "string contains null byte" if s.include?("\0")
    s
  end
end

[[Process.singleton_class, :spawn], [Process.singleton_class, :exec], [Kernel, :spawn], [Kernel, :system], [Kernel, :exec]].each do |mod, m|
  next unless mod.method_defined?(m) || mod.private_method_defined?(m)
  um = mod.instance_method(m)
  priv = mod.private_method_defined?(m)
  mod.__send__(:define_method, m) do |*args, **opts, &blk|
    a = Process.__cmd_args(args)
    opts.empty? ? um.bind(self).call(*a, &blk) : um.bind(self).call(*a, **opts, &blk)
  end
  mod.__send__(:private, m) if priv
end
Kernel.__send__(:module_function, :spawn, :system, :exec) rescue nil
