# Time as in MRI over the Go time gem: argument conversion for new/at/utc/
# local, UTC offsets ("+09:00", "Z", military letters, Integer seconds),
# localtime/getlocal with an offset, zone of fixed-offset times, exact
# arithmetic to the nanosecond, round/floor/ceil, deconstruct_keys.
# The instant and zone live in the Go value (gem/time): Time.__make,
# Time.__civil and #__parts; modes 0 UTC, 1 local (ENV["TZ"]), 2 fixed offset.
class Time
  class << self
    alias __orig_now now unless method_defined?(:__orig_now)
  end

  MONTHS = %w[jan feb mar apr may jun jul aug sep oct nov dec].freeze
  private_constant :MONTHS

  class << self
    # a timezone object (MRI): neither a name nor an offset, but something
    # with #local_to_utc / #utc_to_local
    def __tz?(z)
      !(z.nil? || String === z || Numeric === z || Symbol === z) &&
        (Kernel.__responds?(z, :local_to_utc) || Kernel.__responds?(z, :utc_to_local))
    end

    # [mode, offset] for a zone argument (nil: local), or [:tz, zone] for a
    # timezone object (a name klass.find_timezone knows becomes one)
    def __zone(z, klass = nil)
      return [:tz, z] if __tz?(z)
      if String === z && klass && klass.respond_to?(:find_timezone) && __zone_name?(z)
        tz = klass.find_timezone(z)
        return [:tz, tz] if tz
      end
      return [1, 0] if z.nil?
      if Integer === z
        raise ArgumentError, "utc_offset out of range" unless z.abs < 86400
        return [2, z]
      end
      if !(String === z) && Kernel.__responds?(z, :to_int) && !(Numeric === z && !(Integer === z))
        return __zone(Kernel.__implicit(z, Integer))
      end
      raise ArgumentError, "utc_offset out of range" if Numeric === z
      s = Kernel.__check_implicit(z, String)
      raise TypeError, "can't convert #{Kernel.__cname(z)} into an exact number" unless s
      return [0, 0] if s == "UTC" || s == "Z" || s == "-00:00"
      if s.size == 1 && s =~ /\A[A-IK-Z]\z/
        c = s.ord
        h = if c <= "I".ord then c - "A".ord + 1
            elsif c <= "M".ord then c - "K".ord + 10
            elsif c <= "Y".ord then -(c - "N".ord + 1)
            else 0
            end
        return [2, h * 3600]
      end
      if (m = s.match(/\A([-+])(\d\d)(?::?(\d\d)(?::?(\d\d))?)?\z/))
        h, mi, se = m[2].to_i, m[3].to_i, m[4].to_i
        # MRI: minutes or seconds past 59 are a malformed offset, hours past
        # 23 one out of range
        raise ArgumentError, '"+HH:MM", "-HH:MM", "UTC" or "A".."I","K".."Z" expected for utc_offset: ' + s if mi > 59 || se > 59
        raise ArgumentError, "utc_offset out of range" if h > 23
        off = h * 3600 + mi * 60 + se
        return [2, m[1] == "-" ? -off : off]
      end
      shown = s.encoding.ascii_compatible? ? s : s.inspect
      raise ArgumentError, '"+HH:MM", "-HH:MM", "UTC" or "A".."I","K".."Z" expected for utc_offset: ' + shown
    end

    # a String zone that is no offset or UTC: a name for find_timezone
    def __zone_name?(s)
      !(s == "UTC" || s == "Z" || s == "-00:00" || s =~ /\A[A-IK-Z]\z/ || s =~ /\A[-+]\d\d(?::?\d\d(?::?\d\d)?)?\z/)
    end

    # the instant a timezone's #local_to_utc / #utc_to_local result stands
    # for (a Time's own wall clock for utc_to_local)
    def __tz_seconds(r, local)
      return r.to_i + (local ? r.utc_offset : 0) if Time === r
      raise TypeError, "can't convert #{Kernel.__cname(r)} into an exact number" unless Kernel.__responds?(r, :to_i)
      r.to_i
    end

    def __tz_offset!(off)
      raise ArgumentError, "utc_offset out of range" unless off.abs < 86400
      off
    end

    # a Time of klass at sec/ns in timezone tz (its #utc_to_local)
    def __in_tz(klass, sec, ns, tz)
      unless Kernel.__responds?(tz, :utc_to_local)
        raise TypeError, "can't convert #{Kernel.__cname(tz)} into an exact number"
      end
      tl = Time.__make_obj(Time, sec, 0, 0, 0)
      off = __tz_offset!(__tz_seconds(tz.utc_to_local(tl), true) - sec)
      t = __make_obj(klass, sec, ns, 2, off)
      t.__send__(:__tz=, tz)
      t
    end

    def __make_obj(klass, sec, ns, mode, off)
      t = klass.allocate
      t.__set(Time.__make(sec, ns, mode, off))
      t
    end

    # a Time from civil fields in timezone tz (its #local_to_utc gets the
    # wall clock as a UTC Time without its fraction)
    def __from_civil_tz(klass, y, mon, d, h, mi, s, tz, nsec = nil)
      tl = __from_civil(Time, y, mon, d, h, mi, s, 0, 0, 0)
      ns = nsec || __sec(s)[1]
      unless Kernel.__responds?(tz, :local_to_utc)
        raise TypeError, "can't convert #{Kernel.__cname(tz)} into an exact number"
      end
      utc = __tz_seconds(tz.local_to_utc(tl), false)
      off = __tz_offset!(tl.to_i - utc)
      t = __make_obj(klass, utc, ns, 2, off)
      t.__send__(:__tz=, tz)
      t
    end

    def __int(v, name = nil)
      return v if Integer === v
      return Integer(v, 10) if String === v
      Kernel.__to_int(v)
    end

    def __month(v)
      return 1 if v.nil?
      v = v.to_str if !(Integer === v || String === v) && Kernel.__responds?(v, :to_str)
      if String === v
        i = MONTHS.index(v.downcase)
        return i + 1 if i
        return Integer(v, 10)
      end
      __int(v)
    end

    # seconds argument as [Integer seconds, nanoseconds]
    def __sec(v)
      return [0, 0] if v.nil?
      v = Rational(v) if String === v && v.include?(".")
      v = Integer(v, 10) if String === v
      return [v, 0] if Integer === v
      v = Kernel.__implicit(v, Integer) unless Numeric === v
      r = v.to_r
      s = r.floor
      # integer arithmetic: mruby's Rational overflows past 64 bits (a Float
      # has a 2**52 denominator) where its Integer becomes a Bignum
      f = r - s
      [s, f.numerator * 1_000_000_000 / f.denominator]
    end

    # builds a Time from civil fields with MRI's range checks and carries
    def __from_civil(klass, y, mon, d, h, mi, s, mode, off, nsec = nil)
      y = __int(y)
      mon = __month(mon)
      d = d.nil? ? 1 : __int(d)
      h = h.nil? ? 0 : __int(h)
      mi = mi.nil? ? 0 : __int(mi)
      sec, ns = __sec(s)
      ns = nsec if nsec
      raise ArgumentError, "mon out of range" unless (1..12).include?(mon)
      raise ArgumentError, "argument out of range" unless (1..31).include?(d)
      raise ArgumentError, "argument out of range" unless (0..24).include?(h)
      raise ArgumentError, "argument out of range" unless (0..59).include?(mi)
      raise ArgumentError, "argument out of range" unless (0..60).include?(sec)
      raise ArgumentError, "argument out of range" if h == 24 && (mi != 0 || sec != 0)
      t = klass.allocate
      t.__set(Time.__civil(y, mon, d, h, mi, sec, ns, mode, off))
      t
    end

    def __kw(kw, allowed)
      bad = kw.keys - allowed
      raise ArgumentError, "unknown keyword#{bad.size > 1 ? 's' : ''}: #{bad.map(&:inspect).join(', ')}" unless bad.empty?
    end

    # Time.new("2000-12-31 23:59:59.5 +09:00")
    # MRI's time_init_parse: no surrounding whitespace, the time part
    # required, a zone in the string wins over in:
    def __parse_new(klass, str, zone, precision)
      raise ArgumentError, "time string should have ASCII compatible encoding" unless str.encoding.ascii_compatible?
      unless precision.nil? || Integer === precision
        # NUM2INT: a Float or Rational truncated, anything else by #to_int
        precision = Numeric === precision && !(Complex === precision) ? precision.to_i : Kernel.__to_int(precision)
      end
      precision = 9 if precision.nil? || precision < 0 || precision > 9
      m = str.match(/\A(\d{4,})-(\d\d)-(\d\d)(?:[ T](\d\d):(\d\d):(\d\d)(?:\.(\d+))?)?(?: *(\S+))?\z/)
      raise ArgumentError, "can't parse: #{str.inspect}" unless m
      raise ArgumentError, "missing min part: #{m[4]}" if m[4] && m[5].nil?
      raise ArgumentError, "no time information" if m[4].nil?
      zone = m[8] || zone
      mode, off = __zone(zone, klass)
      if mode == :tz
        frac = (m[7] || "")
        frac = frac[0, precision] if precision
        ns = (frac + "000000000")[0, 9].to_i
        return __from_civil_tz(klass, m[1], m[2].to_i, m[3].to_i, m[4] && m[4].to_i, m[5] && m[5].to_i, m[6] && m[6].to_i, off, ns)
      end
      frac = m[7] || ""
      frac = frac[0, precision] if precision
      ns = (frac + "000000000")[0, 9].to_i
      __from_civil(klass, m[1], m[2].to_i, m[3].to_i, m[4] && m[4].to_i, m[5] && m[5].to_i, m[6] && m[6].to_i, mode, off, ns)
    end

    def now(**kw)
      __kw(kw, [:in])
      z = kw[:in]
      t = allocate
      p = __current_parts
      mode, off = __zone(z, self)
      return __in_tz(self, p[0], p[1], off) if mode == :tz
      t.__set(Time.__make(p[0], p[1], mode, off))
      t
    end

    def __current_parts
      __orig_now.__parts
    end

    def at(time, subsec = (no_sub = true; nil), unit = :microsecond, **kw)
      __kw(kw, [:in])
      z = kw[:in]
      if Time === time
        p = time.__parts
        sec, ns = p[0], p[1]
        mode, off = z.nil? ? [p[2], p[3]] : __zone(z, self)
        mode, off = :tz, time.__send__(:__tz) if z.nil? && time.__send__(:__tz)
      else
        raise TypeError, "can't convert #{Kernel.__cname(time)} into an exact number" if time.nil? || String === time
        # MRI's num_exact: a non-Numeric needs #to_int (and is taken by
        # #to_r when it has one)
        unless Numeric === time
          raise TypeError, "can't convert #{Kernel.__cname(time)} into an exact number" unless Kernel.__responds?(time, :to_int)
          time = Kernel.__responds?(time, :to_r) ? time.to_r : time.to_int
        end
        sec, ns = __sec(time)
        mode, off = __zone(z, self)
      end
      unless no_sub
        raise TypeError, "can't convert #{Kernel.__cname(subsec)} into an exact number" if subsec.nil? || String === subsec || Time === time
        unless Numeric === subsec
          raise TypeError, "can't convert #{Kernel.__cname(subsec)} into an exact number" unless Kernel.__responds?(subsec, :to_int)
          subsec = subsec.to_int
        end
        mul = case unit
              when :millisecond then 1_000_000
              when :usec, :microsecond then 1_000
              when :nsec, :nanosecond then 1
              else raise ArgumentError, "unexpected unit: #{unit.inspect}"
              end
        extra = (subsec.to_r * mul)
        total = ns + extra
        sec += (total / 1_000_000_000).floor
        ns = (total % 1_000_000_000).floor
      end
      return __in_tz(self, sec, ns, off) if mode == :tz
      t = allocate
      t.__set(Time.__make(sec, ns, mode, off))
      t
    end

    def __civil_args(args)
      if args.size == 10
        sec, min, hour, day, mon, year = args
        return [year, mon, day, hour, min, sec, nil]
      end
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..8)" if args.empty? || args.size > 8
      y, mon, d, h, mi, s, us = args
      [y, mon, d, h, mi, s, us]
    end

    def utc(*args)
      y, mon, d, h, mi, s, us = __civil_args(args)
      s, ns = __usec_args(s, us)
      __from_civil(self, y, mon, d, h, mi, s, 0, 0, ns)
    end
    alias gm utc

    def local(*args)
      y, mon, d, h, mi, s, us = __civil_args(args)
      s, ns = __usec_args(s, us)
      __from_civil(self, y, mon, d, h, mi, s, 1, 0, ns)
    end

    # MRI's time_arg: with a microsecond argument the seconds are whole
    # (their fraction ignored) and the microseconds give the subsecond
    def __usec_args(s, us)
      return [s, nil] if us.nil?
      us = Integer(us, 10) if String === us
      us = Kernel.__implicit(us, Integer) unless Numeric === us
      ns = (us.to_r * 1000).floor
      raise ArgumentError, "subsecx out of range" unless (0...1_000_000_000).include?(ns)
      [__sec(s)[0], ns]
    end
    alias mktime local
  end

  def initialize(year = (no_year = true; nil), mon = nil, day = nil, hour = nil, min = nil, sec = nil, zone = nil, **kw)
    Time.__kw(kw, [:in, :precision])
    z = kw[:in]
    precision = kw.fetch(:precision, 9)
    raise ArgumentError, "timezone argument given as positional and keyword arguments" if zone && z
    zone ||= z
    if no_year
      p = Time.__current_parts
      mode, off = Time.__zone(zone, self.class)
      return __take(Time.__in_tz(Time, p[0], p[1], off)) if mode == :tz
      return __set(Time.__make(p[0], p[1], mode, off))
    end
    if String === year && mon.nil? && day.nil? && hour.nil? && year !~ /\A\s*-?\d+\s*\z/
      return __take(Time.__parse_new(self.class, year, zone, precision))
    end
    mode, off = Time.__zone(zone, self.class)
    return __take(Time.__from_civil_tz(Time, year, mon, day, hour, min, sec, off)) if mode == :tz
    __set(Time.__from_civil(Time, year, mon, day, hour, min, sec, mode, off))
  end

  # self becomes t (its instant, zone and timezone object)
  def __take(t)
    __set(t)
    self.__tz = t.__send__(:__tz)
    self
  end
  private :__take

  # the timezone object of a Time made with one (Time#zone)
  def __tz = @__tz
  def __tz=(tz)
    @__tz = tz
  end
  private :__tz, :__tz=

  def __ns = __parts[1]
  def __sec_i = __parts[0]

  def utc_offset = __parts[3]
  alias gmt_offset utc_offset
  alias gmtoff utc_offset

  def utc?
    __parts[2] == 0
  end
  alias gmt? utc?

  def zone
    return @__tz if @__tz
    p = __parts
    return "UTC".encode(Encoding::US_ASCII) if p[2] == 0
    return nil if p[2] == 2
    String === p[4] ? p[4].dup.force_encoding(Encoding::US_ASCII) : p[4]
  end

  def localtime(off = nil)
    p = __parts
    # already local: nothing changes (a frozen Time too)
    return self if off.nil? && p[2] == 1 && @__tz.nil?
    mode, o = off.nil? ? [1, 0] : Time.__zone(off, self.class)
    return __take(Time.__in_tz(Time, p[0], p[1], o)) if mode == :tz
    @__tz = nil
    __set(Time.__make(p[0], p[1], mode, o))
  end

  def getlocal(off = nil)
    dup.localtime(off)
  end

  def utc
    p = __parts
    return self if p[2] == 0
    @__tz = nil
    __set(Time.__make(p[0], p[1], 0, 0))
  end
  alias gmtime utc

  def getutc = dup.utc
  alias getgm getutc

  def to_i = __parts[0]
  alias tv_sec to_i
  def nsec = __parts[1]
  alias tv_nsec nsec
  def usec = __parts[1] / 1000
  alias tv_usec usec
  def subsec
    ns = __parts[1]
    ns == 0 ? 0 : Rational(ns, 1_000_000_000)
  end

  def to_r
    p = __parts
    Rational(p[0]) + Rational(p[1], 1_000_000_000)
  end

  def to_f
    p = __parts
    p[0] + p[1] / 1e9
  end

  # a Time of self's class at another instant, in self's own zone (not the
  # local zone of now)
  # (+ and - give a Time, not an instance of self's subclass, as MRI)
  def __with(sec, ns)
    return Time.__in_tz(Time, sec, ns, @__tz) if @__tz
    t = Time.allocate
    t.__set(__at(sec, ns))
    t
  end
  private :__with

  def +(other)
    raise TypeError, "time + time?" if Time === other
    raise TypeError, "can't convert #{Kernel.__cname(other)} into an exact number" if other.nil? || String === other
    d = Numeric === other ? other : Kernel.__implicit(other, Integer)
    p = __parts
    # in nanoseconds with Integer arithmetic: mruby's Rational overflows
    # on a Float's 2**52 denominator
    r = d.to_r
    total = p[1] + (r.numerator * 1_000_000_000).div(r.denominator)
    __with(p[0] + total.div(1_000_000_000), total % 1_000_000_000)
  end

  def -(other)
    p = __parts
    if Time === other
      q = other.__parts
      return (p[0] - q[0]) + (p[1] - q[1]) / 1e9
    end
    raise TypeError, "can't convert #{Kernel.__cname(other)} into an exact number" if other.nil? || String === other
    d = Numeric === other ? other : Kernel.__implicit(other, Integer)
    self + (-d.to_r)
  end

  def <=>(other)
    # MRI's rb_invcmp for a non-Time: other <=> self, inverted
    unless Time === other
      return nil unless Kernel.__responds?(other, :<=>)
      r = other <=> self
      return nil if r.nil?
      return -(r <=> 0) if Integer === r
      return -1 if r > 0
      return 1 if r < 0
      return 0
    end
    a = __parts
    b = other.__parts
    c = a[0] <=> b[0]
    c == 0 ? a[1] <=> b[1] : c
  end

  def ==(other)
    Time === other && (self <=> other) == 0
  end

  def eql?(other)
    Time === other && (self <=> other) == 0
  end

  def hash
    p = __parts
    [p[0], p[1]].hash
  end

  def succ
    warn "Time#succ is obsolete; use time + 1" if $VERBOSE
    self + 1
  end

  def round(ndigits = 0) = __round(ndigits, :round)
  def floor(ndigits = 0) = __round(ndigits, :floor)
  def ceil(ndigits = 0) = __round(ndigits, :ceil)

  def __round(nd, how)
    nd = Kernel.__to_int(nd)
    raise ArgumentError, "negative ndigits given" if nd < 0
    return dup if nd >= 9
    p = __parts
    unit = 10**(9 - nd)
    ns = p[1]
    q = case how
        when :floor then ns / unit * unit
        when :ceil then (ns + unit - 1) / unit * unit
        else
          r = ns % unit
          r * 2 >= unit ? ns - r + unit : ns - r
        end
    # a Time, whatever subclass self is (MRI)
    __at(p[0] + q / 1_000_000_000, q % 1_000_000_000)
  end
  private :__round

  def __offset_str(colon = true)
    off = utc_offset
    sign = off < 0 ? "-" : "+"
    off = off.abs
    h, m, s = off / 3600, off / 60 % 60, off % 60
    str = format("%s%02d%s%02d", sign, h, colon ? ":" : "", m)
    str += format("%s%02d", colon ? ":" : "", s) if s != 0
    str
  end
  private :__offset_str

  alias isdst dst?

  def inspect
    __oruby_inspect.force_encoding(Encoding::US_ASCII)
  end

  def to_s
    __oruby_to_s.force_encoding(Encoding::US_ASCII)
  end

  def __oruby_inspect
    p = __parts
    base = strftime("%Y-%m-%d %H:%M:%S")
    ns = p[1]
    if ns != 0
      frac = format("%09d", ns).sub(/0+\z/, "")
      base += "." + frac
    end
    base + (utc? ? " UTC" : " " + __offset_str(false))
  end

  def __oruby_to_s
    strftime("%Y-%m-%d %H:%M:%S ") + (utc? ? "UTC" : __offset_str(false))
  end
  private :__oruby_inspect, :__oruby_to_s

  def deconstruct_keys(keys)
    all = { year: year, month: month, day: day, yday: yday, wday: wday, hour: hour, min: min, sec: sec,
            subsec: subsec, dst: dst?, zone: zone }
    return all if keys.nil?
    raise TypeError, "wrong argument type #{Kernel.__cname(keys)} (expected Array or nil)" unless Array === keys
    keys.each_with_object({}) { |k, h| h[k] = all[k] if all.key?(k) }
  end

  def xmlschema(fraction_digits = 0)
    fd = Kernel.__to_int(fraction_digits)
    s = strftime("%Y-%m-%dT%H:%M:%S")
    s += "." + format("%09d", nsec)[0, fd] + ("0" * [fd - 9, 0].max) if fd > 0
    s + (utc? ? "Z" : __offset_str(true))
  end
  alias iso8601 xmlschema
end

class Time
  # %Z of a Time with a timezone object: its #abbr (or #name)
  alias __oruby_go_strftime strftime
  def strftime(fmt)
    tz = @__tz
    if tz && fmt.include?("%Z")
      ab = if tz.respond_to?(:abbr) then tz.abbr(self)
           elsif tz.respond_to?(:name) then tz.name
           end
      ab = ab.to_s.gsub("%", "%%")
      fmt = fmt.gsub(/%(%|Z)/) { |m| m == "%%" ? m : ab }
    end
    __oruby_go_strftime(fmt)
  end
end
