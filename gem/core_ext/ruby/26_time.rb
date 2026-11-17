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
    # [mode, offset] for a zone argument (nil: local)
    def __zone(z)
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
        raise ArgumentError, "utc_offset out of range" if h > 23 || mi > 59 || se > 59
        off = h * 3600 + mi * 60 + se
        return [2, m[1] == "-" ? -off : off]
      end
      raise ArgumentError, '"+HH:MM", "-HH:MM", "UTC" or "A".."I","K".."Z" expected for utc_offset: ' + s
    end

    def __int(v, name = nil)
      return v if Integer === v
      return Integer(v, 10) if String === v
      Kernel.__to_int(v)
    end

    def __month(v)
      return 1 if v.nil?
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
      r = v.is_a?(Float) ? v.to_r : v.to_r
      s = r.floor
      [s, ((r - s) * 1_000_000_000).floor]
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
    def __parse_new(klass, str, zone, precision)
      m = str.match(/\A\s*(\d{4,})-(\d\d)-(\d\d)(?:[ T](\d\d):(\d\d):(\d\d)(?:\.(\d+))?)?\s*(\S+)?\s*\z/)
      raise ArgumentError, "can't parse: #{str.inspect}" unless m
      raise ArgumentError, "missing min part: #{m[4]}" if m[4] && m[5].nil?
      zone ||= m[8]
      mode, off = __zone(zone)
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
      mode, off = __zone(z)
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
        mode, off = z.nil? ? [p[2], p[3]] : __zone(z)
      else
        raise TypeError, "can't convert #{Kernel.__cname(time)} into an exact number" if time.nil? || String === time
        sec, ns = __sec(Numeric === time ? time : Kernel.__implicit(time, Integer))
        mode, off = __zone(z)
      end
      unless no_sub
        raise TypeError, "can't convert #{Kernel.__cname(subsec)} into an exact number" if subsec.nil? || String === subsec
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
      t = __from_civil(self, y, mon, d, h, mi, s, 0, 0)
      t = t + Rational(us.to_r, 1_000_000) if us
      t
    end
    alias gm utc

    def local(*args)
      y, mon, d, h, mi, s, us = __civil_args(args)
      t = __from_civil(self, y, mon, d, h, mi, s, 1, 0)
      t = t + Rational(us.to_r, 1_000_000) if us
      t
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
      mode, off = Time.__zone(zone)
      return __set(Time.__make(p[0], p[1], mode, off))
    end
    if String === year && mon.nil? && day.nil? && hour.nil? && year !~ /\A\s*-?\d+\s*\z/
      return __set(Time.__parse_new(Time, year, zone, precision))
    end
    mode, off = Time.__zone(zone)
    __set(Time.__from_civil(Time, year, mon, day, hour, min, sec, mode, off))
  end

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
    p = __parts
    return "UTC" if p[2] == 0
    return nil if p[2] == 2
    p[4]
  end

  def localtime(off = nil)
    p = __parts
    mode, o = off.nil? ? [1, 0] : Time.__zone(off)
    __set(Time.__make(p[0], p[1], mode, o))
  end

  def getlocal(off = nil)
    dup.localtime(off)
  end

  def utc
    p = __parts
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

  def __with(sec, ns)
    p = __parts
    t = self.class.allocate
    t.__set(Time.__make(sec, ns, p[2], p[3]))
    t
  end
  private :__with

  def +(other)
    raise TypeError, "time + time?" if Time === other
    raise TypeError, "can't convert #{Kernel.__cname(other)} into an exact number" if other.nil? || String === other
    d = Numeric === other ? other : Kernel.__implicit(other, Integer)
    p = __parts
    total = Rational(p[1], 1_000_000_000) + d.to_r
    sec = p[0] + total.floor
    ns = ((total - total.floor) * 1_000_000_000).floor
    __with(sec, ns)
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
    return nil unless Time === other
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
    __with(p[0] + q / 1_000_000_000, q % 1_000_000_000)
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

  def inspect
    p = __parts
    base = strftime("%Y-%m-%d %H:%M:%S")
    ns = p[1]
    if ns != 0
      frac = format("%09d", ns).sub(/0+\z/, "")
      base += "." + frac
    end
    base + (utc? ? " UTC" : " " + __offset_str(false))
  end

  def to_s
    strftime("%Y-%m-%d %H:%M:%S ") + (utc? ? "UTC" : __offset_str(false))
  end

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
