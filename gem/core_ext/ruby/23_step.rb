# Numeric#step, Range#step / Range#% and Enumerator::ArithmeticSequence,
# following MRI's numeric.c (num_step), range.c (range_step, 3.4) and
# enumerator.c (arith_seq_*), including ruby_float_step's error correction.
class Enumerator
  class ArithmeticSequence < Enumerator
    class << self
      undef_method :new
      def allocate
        raise TypeError, "allocator undefined for #{self}" unless @__internal
        super
      end

      def __new(recv, meth, args, kw, b, e, s, x)
        @__internal = true
        o = begin
          Class.instance_method(:new).bind_call(self)
        ensure
          @__internal = false
        end
        o.__send__(:__setup, recv, meth, args, kw, b, e, s, x)
        o
      end

      # MRI's ruby_float_step_size, in doubles: an infinite or NaN count
      # stays one (floor of Infinity is Infinity in C)
      def __float_step_size(beg, en, unit, excl)
        return Float::INFINITY if unit == 0
        if unit.infinite?
          return (unit > 0 ? beg <= en : beg >= en) ? 1 : 0
        end
        n = (en - beg) / unit
        err = (beg.abs + en.abs + (en - beg).abs) / unit.abs * Float::EPSILON
        err = 0.5 if err > 0.5
        if excl
          return 0 if n <= 0
          n = n < 1 ? 0.0 : __ffloor(n - err)
          d = (n + 1) * unit + beg
          if beg < en
            n += 1 if d < en
          elsif beg > en
            n += 1 if d > en
          end
        else
          return 0 if n < 0
          n = __ffloor(n + err)
        end
        n + 1
      end

      def __ffloor(x)
        x.finite? ? x.floor.to_f : x
      end

      # ruby_float_step: iterates when any of from/to/step is a Float
      def __float_step(from, to, step, excl, allow_endless)
        return false unless Float === from || Float === to || Float === step
        unit = step.to_f
        beg = from.to_f
        if to.nil?
          return false unless allow_endless
          en = unit < 0 ? -Float::INFINITY : Float::INFINITY
        else
          en = to.to_f
        end
        n = __float_step_size(beg, en, unit, excl)
        if unit.infinite?
          yield beg if n > 0
        elsif unit == 0
          loop { yield beg }
        else
          i = 0
          while i < n
            d = i * unit + beg
            d = en if unit >= 0 ? en < d : d < en
            yield d
            i += 1
          end
        end
        true
      end

      def __negative?(num)
        return num < 0 if Integer === num || Float === num
        r = begin
          num > 0
        rescue NoMethodError
          raise TypeError, "#{Kernel.__cname(num)} can't be coerced into Integer"
        end
        !r
      end
    end

    def __setup(recv, meth, args, kw, b, e, s, x)
      @__recv = recv
      @__meth = meth
      @__args = args
      @__kw = kw
      @__b = b
      @__e = e
      @__s = s
      @__x = x
      @__init = true
      seq = self
      Enumerator.instance_method(:initialize).bind_call(self, Enumerator::Generator.new { |y| seq.each { |v| y << v } }, :each)
    end
    private :__setup

    def initialize(*) = nil
    private :initialize

    def begin = @__b
    def end = @__e
    def step = @__s
    def exclude_end? = @__x

    def each(&block)
      return self unless block
      c = @__b
      e = @__e
      s = @__s
      x = @__x
      return self if ArithmeticSequence.__float_step(c, e, s, x, true, &block)
      if e.nil?
        loop do
          yield c
          c += s
        end
      end
      if s == 0
        loop { yield c }
      end
      len_1 = (e - c).div(s)
      last = c + s * len_1
      last -= s if x && last == e
      if s < 0
        while c >= last
          yield c
          c += s
        end
      else
        while last >= c
          yield c
          c += s
        end
      end
      self
    end

    def size
      b = @__b
      e = @__e
      s = @__s
      if Float === b || Float === e || Float === s
        ee = e.nil? ? (s < 0 ? -Float::INFINITY : Float::INFINITY) : e.to_f
        n = ArithmeticSequence.__float_step_size(b.to_f, ee, s.to_f, @__x)
        return n if Float === n && (n.infinite? || n.nan?)
        return n.to_i
      end
      return Float::INFINITY if e.nil?
      s = Kernel.__to_int(s) unless Numeric === s
      return Float::INFINITY if s == 0
      len_1 = (e - b).div(s)
      return 0 if len_1 < 0
      last = b + s * len_1
      @__x && last == e ? len_1 : len_1 + 1
    end

    def first(n = (no_n = true; nil))
      if no_n
        each { |v| return v }
        return nil
      end
      n = Kernel.__to_int(n)
      raise ArgumentError, "attempt to take negative size" if n < 0
      take(n)
    end

    def last(n = (no_n = true; nil))
      raise RangeError, "cannot get the last element of endless arithmetic sequence" if @__e.nil?
      a = to_a
      return a.last if no_n
      n = Kernel.__to_int(n)
      raise ArgumentError, "negative array size" if n < 0
      a.last(n)
    end

    def to_a
      raise RangeError, "cannot convert endless range to an array" if @__e.nil?
      r = []
      each { |v| r << v }
      r
    end
    alias entries to_a

    def ==(other)
      ArithmeticSequence === other && @__b == other.begin && @__e == other.end &&
        @__s == other.step && @__x == other.exclude_end?
    end
    alias === ==
    alias eql? ==

    def hash
      [@__b, @__e, @__s, @__x].hash
    end

    def inspect
      args = @__args.map(&:inspect) + @__kw.map { |k, v| "#{k}: #{v.inspect}" }
      recv = Range === @__recv ? "(#{@__recv.inspect})" : @__recv.inspect
      a = args.empty? ? "" : "(#{args.join(', ')})"
      "(#{recv}.#{@__meth}#{a})"
    end
    alias to_s inspect
  end
end

class Numeric
  def step(*args, **kw, &block)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)" if args.size > 2
    bad = kw.keys - [:by, :to]
    raise ArgumentError, "unknown keyword#{bad.size > 1 ? 's' : ''}: #{bad.map(&:inspect).join(', ')}" unless bad.empty?
    no_by = !kw.key?(:by)
    no_to = !kw.key?(:to)
    by = kw[:by]
    to = kw[:to]
    limit = args[0]
    st = args[1]
    unless no_to
      raise ArgumentError, "to is given twice" if args.size > 0
      limit = to
    end
    unless no_by
      raise ArgumentError, "step is given twice" if args.size > 1
      st = by
    end
    unless block
      raise TypeError, "no implicit conversion from nil to integer" if no_by && args.size > 1 && args[1].nil?
      st = 1 if st.nil?
      raise ArgumentError, "step can't be 0" if st == 0
      if (limit.nil? || Numeric === limit) && Numeric === st
        return Enumerator::ArithmeticSequence.__new(self, :step, args, kw, self, limit, st, false)
      end
      # MRI: a plain Enumerator; a bad step fails when it is iterated or
      # sized (num_step_size checks the step's sign)
      sz = -> { Enumerator::ArithmeticSequence.__negative?(st); nil }
      return kw.empty? ? to_enum(:step, *args, &sz) : to_enum(:step, *args, **kw, &sz)
    end
    raise TypeError, "no implicit conversion from nil to integer" if no_by && args.size > 1 && st.nil?
    raise ArgumentError, "step can't be 0" if st == 0
    st = 1 if st.nil?
    desc = Enumerator::ArithmeticSequence.__negative?(st)
    limit = desc ? -Float::INFINITY : Float::INFINITY if limit.nil?
    inf = Float === limit && limit.infinite? && (limit < 0 ? desc : !desc)
    if Integer === self && (inf || Integer === limit) && Integer === st
      i = self
      if inf
        loop do
          yield i
          i += st
        end
      elsif desc
        while i >= limit
          yield i
          i += st
        end
      else
        while i <= limit
          yield i
          i += st
        end
      end
    elsif !Enumerator::ArithmeticSequence.__float_step(self, limit, st, false, false, &block)
      i = self
      if inf
        loop do
          yield i
          i += st
        end
      else
        cmp = desc ? :< : :>
        until i.__send__(cmp, limit)
          yield i
          i += st
        end
      end
    end
    self
  end
end

[Integer, Float].each do |c|
  c.__send__(:remove_method, :step) if c.instance_methods(false).include?(:step)
end

class Range
  def step(*args, &block)
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)" if args.size > 1
    b = self.begin
    e = self.end
    b_num = Numeric === b
    e_num = Numeric === e
    str_b = String === b ? b : nil
    sym_b = Symbol === b ? b : nil
    if args.empty?
      raise ArgumentError, "step is required for non-numeric ranges" unless b_num || str_b || sym_b || (b.nil? && e_num)
      st = 1
    else
      st = args[0]
    end
    st_num = Numeric === st
    raise ArgumentError, "step can't be 0" if st_num && b_num && st == 0
    unless block
      if st_num && ((b_num && (e.nil? || e_num)) || (b.nil? && e_num))
        return Enumerator::ArithmeticSequence.__new(self, :step, args, {}, b, e, st, exclude_end?)
      end
      raise ArgumentError, "#step for non-numeric beginless ranges is meaningless" if b.nil?
      return to_enum(:step, *args) { nil }
    end
    raise ArgumentError, "#step iteration for beginless ranges is meaningless" if b.nil?
    x = exclude_end?
    if Integer === b && (e.nil? || Integer === e) && Integer === st
      i = b
      if e.nil?
        loop do
          yield i
          i += st
        end
      elsif st < 0
        fin = x ? e : e - 1
        while i > fin
          yield i
          i += st
        end
      else
        fin = x ? e : e + 1
        while i < fin
          yield i
          i += st
        end
      end
    elsif b_num && st_num && Enumerator::ArithmeticSequence.__float_step(b, e, st, x, true, &block)
      # done
    elsif (str_b || sym_b) && Integer === st
      raise ArgumentError, "step can't be negative" if st < 0
      raise ArgumentError, "step can't be 0" if st == 0
      n = 0
      src = sym_b ? Range.new(sym_b.to_s, e && e.to_s, x) : self
      if e.nil?
        s = str_b || sym_b.to_s
        loop do
          yield(sym_b ? s.to_sym : s) if n % st == 0
          n += 1
          s = s.succ
        end
      else
        src.each do |s|
          yield(sym_b ? s.to_sym : s) if n % st == 0
          n += 1
        end
      end
    elsif e.nil?
      v = b
      loop do
        yield v
        v += st
      end
    elsif b_num && st_num && st < 0
      v = b
      if x
        while __r_less(e, v) < 0
          yield v
          v += st
        end
      else
        while (c = __r_less(e, v)) <= 0
          yield v
          break if c == 0
          v += st
        end
      end
    elsif (dir = __r_less(b, e)) == 0
      yield b unless x
    elsif dir == __r_less(b, b + st)
      v = b
      if x
        while __r_less(v, e) == dir
          yield v
          v += st
        end
      else
        while (c = __r_less(v, e)) == dir || c == 0
          yield v
          break if c == 0
          v += st
        end
      end
    end
    self
  end

  def %(step)
    return Enumerator::ArithmeticSequence.__new(self, :%, [step], {}, self.begin, self.end, step, exclude_end?) if Numeric === step && (Numeric === self.begin || Numeric === self.end)
    step(step)
  end

  private def __r_less(a, b)
    c = a <=> b
    raise ArgumentError, "comparison of #{Kernel.__cname(a)} with #{Kernel.__cname(b)} failed" if c.nil?
    c <=> 0
  end
end
