# String methods that take a Regexp, layered on the regexp gem's match
class String
  alias_method :__re_partition, :partition
  def partition(sep)
    unless sep.is_a?(Regexp)
      r = __re_partition(sep)
      # the separator as given (MRI's rb_str_dup(sep)), in its own encoding
      r[1] = sep.dup if String === sep && !r[1].empty?
      return r
    end
    m = sep.match(self)
    return [dup, "", ""] unless m
    [m.pre_match, m[0], m.post_match]
  end

  alias_method :__re_rpartition, :rpartition
  def rpartition(sep)
    unless sep.is_a?(Regexp)
      r = __re_rpartition(sep)
      r[1] = sep.dup if String === sep && !r[1].empty?
      enc = encoding
      r[0].force_encoding(enc) unless r[0].encoding.equal?(enc)
      r[2].force_encoding(enc) unless r[2].encoding.equal?(enc)
      return r
    end
    i = rindex(sep)
    return ["", "", dup] unless i
    m = $~
    [self[0, i], m[0], m.post_match]
  end

  alias_method :__re_start_with?, :start_with?
  def start_with?(*prefixes)
    prefixes.any? do |p|
      if p.is_a?(Regexp)
        m = p.match(self)
        m && m.begin(0) == 0
      else
        __re_start_with?(p)
      end
    end
  end

  alias_method :__re_slice!, :slice!
  def slice!(*args)
    return __re_slice!(*args) unless args[0].is_a?(Regexp)
    re, cap = args
    m = re.match(self)
    return nil unless m
    cap ||= 0
    if Integer === cap
      return nil if cap >= m.size || (cap < 0 && cap + m.size <= 0)
      cap += m.size if cap < 0
    end
    part = m[cap]
    return nil unless part
    b = m.begin(cap)
    self[b, m.end(cap) - b] = ""
    part
  end

  alias_method :__re_aset, :[]=
  def []=(*args)
    return __re_aset(*args) unless args[0].is_a?(Regexp)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 2..3)" unless (2..3).cover?(args.size)
    re = args[0]
    cap = args.size == 3 ? args[1] : 0
    val = args[-1]
    m = re.match(self)
    raise IndexError, "regexp not matched" unless m
    if cap.is_a?(Integer)
      raise IndexError, "index #{cap} out of regexp" if cap >= m.size || cap < -m.size
    end
    b = m.begin(cap)
    raise IndexError, "regexp group #{cap} not matched" unless b
    __re_aset(b, m.end(cap) - b, val)
  end
end
