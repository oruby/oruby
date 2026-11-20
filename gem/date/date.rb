class Date
  include Comparable

  class Error < ArgumentError; end

  ITALY = 2299161
  ENGLAND = 2361222
  JULIAN = Float::INFINITY
  GREGORIAN = -Float::INFINITY
  DEFAULT_SG = ITALY
  UNIX_EPOCH_IN_CJD = 2440588

  MONTHNAMES = [nil, "January", "February", "March", "April", "May", "June", "July",
                "August", "September", "October", "November", "December"].map { |s| s&.freeze }.freeze
  ABBR_MONTHNAMES = [nil, "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"].map { |s| s&.freeze }.freeze
  DAYNAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"].map(&:freeze).freeze
  ABBR_DAYNAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"].map(&:freeze).freeze
  VERSION = "3.4.1"

  # -- helpers ---------------------------------------------------------------

  class << self
    def __int(v, name = "integer")
      return v if v.is_a?(Integer)
      return v.to_i if v.is_a?(Numeric)
      raise TypeError, "invalid #{name} (not numeric)" unless v.respond_to?(:to_int)
      v.to_int
    end

    def __sg(sg)
      return sg if sg.is_a?(Float) && sg.infinite?
      return DEFAULT_SG unless sg.is_a?(Numeric)
      sg = sg.to_i if sg.is_a?(Float)
      unless sg.is_a?(Integer) && sg >= 2298874 && sg <= 2426355 || sg.is_a?(Float)
        warn "invalid start is ignored" if $VERBOSE
        return DEFAULT_SG
      end
      sg
    end

    def __civil_jd(y, m, d, sg)
      raise TypeError, "invalid year (not numeric)" unless y.is_a?(Numeric)
      raise TypeError, "invalid month (not numeric)" unless m.is_a?(Numeric)
      raise TypeError, "invalid day (not numeric)" unless d.is_a?(Numeric)
      jd = __valid_civil(y.to_i, m.to_i, d.to_i, sg)
      raise Error, "invalid date" unless jd
      jd
    end

    def __civil_of(jd, sg) = __jd_to_civil(jd, sg)

    def __new_jd(jd, sg = ITALY, df = 0, sf = 0, of = 0)
      obj = allocate
      obj.__send__(:__init, jd, df, sf, of, __sg(sg))
      obj
    end
  end

  def __init(jd, df, sf, of, sg)
    @jd = jd       # Julian Day number at UTC
    @df = df       # seconds in day (UTC)
    @sf = sf       # fraction of second (Rational, seconds)
    @of = of       # UTC offset in seconds
    @sg = sg
    @civil = nil
  end
  private :__init

  # -- constructors ------------------------------------------------------------

  def self.civil(y = -4712, m = 1, d = 1, sg = ITALY)
    sg = __sg(sg)
    __new_jd(__civil_jd(y, m, d, sg), sg)
  end

  def self.new(*args)
    civil(*args)
  end

  def self.jd(jd = 0, sg = ITALY)
    raise TypeError, "invalid jd (not numeric)" unless jd.is_a?(Numeric)
    __new_jd(jd.floor, sg)
  end

  def self.ordinal(y = -4712, yday = 1, sg = ITALY)
    sg = __sg(sg)
    raise TypeError, "invalid year (not numeric)" unless y.is_a?(Numeric)
    raise TypeError, "invalid yday (not numeric)" unless yday.is_a?(Numeric)
    jd = __ordinal_jd(y.to_i, yday.to_i, sg)
    raise Error, "invalid date" unless jd
    __new_jd(jd, sg)
  end

  def self.__ordinal_jd(y, yday, sg)
    first = __valid_civil(y, 1, 1, sg) || __valid_civil(y, 1, 15, sg)
    last = __valid_civil(y, 12, -1, sg)
    return nil unless first && last
    if yday < 0
      jd = last + yday + 1
    else
      jd = first + yday - 1
    end
    return nil if jd < first || jd > last
    jd
  end

  def self.commercial(y = -4712, w = 1, d = 1, sg = ITALY)
    sg = __sg(sg)
    jd = __commercial_jd(y.to_i, w.to_i, d.to_i, sg)
    raise Error, "invalid date" unless jd
    __new_jd(jd, sg)
  end

  def self.__commercial_jd(y, w, d, sg)
    d += 8 if d < 0
    if w < 0
      nw = __jd_to_commercial(__commercial_to_jd(y + 1, 1, 1, sg) + w * 7, sg)
      return nil unless nw[0] == y
      w = nw[1]
    end
    jd = __commercial_to_jd(y, w, d, sg)
    return nil unless __jd_to_commercial(jd, sg) == [y, w, d]
    jd
  end

  def self.today(sg = ITALY)
    t = Time.now
    civil(t.year, t.mon, t.mday, sg)
  end

  def self.valid_civil?(y, m, d, sg = ITALY)
    return false unless y.is_a?(Numeric) && m.is_a?(Numeric) && d.is_a?(Numeric)
    !__valid_civil(y.to_i, m.to_i, d.to_i, __sg(sg)).nil?
  end
  class << self
    alias valid_date? valid_civil?
  end

  def self.valid_jd?(jd, sg = ITALY)
    jd.is_a?(Numeric)
  end

  def self.valid_ordinal?(y, d, sg = ITALY)
    return false unless y.is_a?(Numeric) && d.is_a?(Numeric)
    !__ordinal_jd(y.to_i, d.to_i, __sg(sg)).nil?
  end

  def self.valid_commercial?(y, w, d, sg = ITALY)
    return false unless y.is_a?(Numeric) && w.is_a?(Numeric) && d.is_a?(Numeric)
    !__commercial_jd(y.to_i, w.to_i, d.to_i, __sg(sg)).nil?
  end

  def self.gregorian_leap?(y)
    raise TypeError, "invalid year (not numeric)" unless y.is_a?(Numeric)
    y = y.to_i
    (y % 4 == 0 && y % 100 != 0) || y % 400 == 0
  end

  def self.leap?(y) = gregorian_leap?(y)

  def self.julian_leap?(y)
    raise TypeError, "invalid year (not numeric)" unless y.is_a?(Numeric)
    y.to_i % 4 == 0
  end

  # -- parsing ---------------------------------------------------------------

  def self.__str(str)
    return str if str.is_a?(String)
    return str.to_str if str.respond_to?(:to_str)
    raise TypeError, "no implicit conversion of #{str.nil? ? 'nil' : str.class} into String"
  end

  def self._parse(str, comp = true, limit: 128)
    str = __str(str)
    raise ArgumentError, "string length (#{str.size}) exceeds the limit #{limit}" if limit && str.size > limit
    __parse(str, comp)
  end

  def self._strptime(str, fmt = "%F")
    __strptime(__str(str), fmt)
  end

  def self._iso8601(str, limit: 128) = str.nil? ? {} : __iso8601(__str(str))
  def self._rfc3339(str, limit: 128) = str.nil? ? {} : __rfc3339(__str(str))
  def self._xmlschema(str, limit: 128) = str.nil? ? {} : __xmlschema(__str(str))
  def self._rfc2822(str, limit: 128) = str.nil? ? {} : __rfc2822(__str(str))
  def self._httpdate(str, limit: 128) = str.nil? ? {} : __httpdate(__str(str))
  def self._jisx0301(str, limit: 128) = _iso8601(str)
  class << self
    alias _rfc822 _rfc2822
  end

  def self.zone_to_diff(zone) = __zone_to_diff(zone) || 0

  # complete parsed hash into jd (nil when impossible)
  def self.__complete_jd(h, sg)
    today = nil
    if h[:seconds]
      return nil
    end
    if h[:wnum0] || h[:wnum1]
      y = h[:year] || Date.today.year
      first = __valid_civil(y, 1, 1, sg)
      return nil unless first
      if h[:wnum0]
        fwday = (first + 1) % 7 # 0=Sun..6=Sat
        sun1 = first + (7 - fwday) % 7
        wday = h[:wday] || 0
        return sun1 + (h[:wnum0] - 1) * 7 + wday
      else
        fcwday = first % 7 # 0=Mon..6=Sun
        mon1 = first + (7 - fcwday) % 7
        w = if h[:wday] then (h[:wday] == 0 ? 6 : h[:wday] - 1)
            elsif h[:cwday] then h[:cwday] - 1
            else 0
            end
        return mon1 + (h[:wnum1] - 1) * 7 + w
      end
    end
    if h[:year] && h[:wday] && !h[:mon] && !h[:mday] && !h[:yday]
      first = __valid_civil(h[:year], 1, 1, sg)
      return nil unless first
      fwday = (first + 1) % 7
      return first + (h[:wday] - fwday) % 7
    end
    if h[:year] || h[:mon] || h[:mday]
      if h[:yday] && !h[:mon] && !h[:mday]
        y = h[:year] || (today ||= Date.today).year
        return __ordinal_jd(y, h[:yday], sg)
      end
      y = h[:year] || (today ||= Date.today).year
      m = h[:mon] || (h[:year] ? 1 : (today ||= Date.today).mon)
      d = h[:mday] || 1
      return __valid_civil(y, m, d, sg)
    end
    if h[:yday]
      y = (today ||= Date.today).year
      return __ordinal_jd(y, h[:yday], sg)
    end
    if h[:cwyear] || h[:cweek] || h[:cwday]
      today ||= Date.today
      y = h[:cwyear] || today.cwyear
      w = h[:cweek] || (h[:cwyear] ? 1 : today.cweek)
      d = h[:cwday] || (h[:cweek] || h[:cwyear] ? 1 : today.cwday)
      return __commercial_jd(y, w, d, sg)
    end
    if h[:wday]
      today ||= Date.today
      return __commercial_jd(today.cwyear, today.cweek, h[:wday] == 0 ? 7 : h[:wday], sg)
    end
    nil
  end

  def self.__from_hash(h, sg, klass = self)
    raise Error, "invalid date" if h.nil? || h.empty?
    if h[:seconds]
      sec = h[:seconds]
      of = h[:offset] || 0
      jd, df = (sec + of).divmod(86400)
      return klass.__new_jd(jd + UNIX_EPOCH_IN_CJD, sg, (df - of) % 86400 + 0, 0, of).tap { |d| d.__send__(:__fix_jd_utc) }
    end
    if (h[:hour] && !h[:mday] && !h[:mon] && !h[:year] && !h[:yday] && !h[:cwday] && !h[:wday] && !h[:cweek])
      t = Date.today
      h = h.merge(year: t.year, mon: t.mon, mday: t.mday)
    end
    jd = __complete_jd(h, sg)
    raise Error, "invalid date" unless jd
    if klass <= DateTime
      hour = h[:hour] || 0
      min = h[:min] || 0
      sec = h[:sec] || 0
      sec = 59 if sec == 60
      of = h[:offset] || 0
      raise Error, "invalid date" if hour > 24 || min > 59 || sec > 59
      klass.__from_local(jd, hour, min, sec, h[:sec_fraction] || 0, of, sg)
    else
      klass.__new_jd(jd, sg)
    end
  end

  def self.parse(str = "-4712-01-01", comp = true, sg = ITALY, limit: 128)
    __from_hash(_parse(str, comp, limit: limit), __sg(sg))
  end

  def self.strptime(str = "-4712-01-01", fmt = "%F", sg = ITALY)
    h = _strptime(str, fmt)
    raise Error, "invalid date" if h.nil? || h[:leftover]
    __from_hash(h, __sg(sg))
  end

  %i[iso8601 rfc3339 xmlschema rfc2822 httpdate jisx0301].each do |name|
    define_singleton_method(name) do |str = nil, sg = ITALY, limit: 128|
      h = __send__("_#{name}", str)
      __from_hash(h, __sg(sg))
    end
  end
  class << self
    alias rfc822 rfc2822
  end

  # -- accessors ---------------------------------------------------------------

  # jd/df of local time
  def __local_jd
    return @jd if @of == 0 && @df == 0
    (@jd * 86400 + @df + @of).div(86400)
  end
  protected :__local_jd

  def __local_df
    (@df + @of) % 86400
  end
  protected :__local_df

  def __civil
    @civil ||= self.class.__jd_to_civil(__local_jd, @sg)
  end
  private :__civil

  def year = __civil[0]
  def mon = __civil[1]
  alias month mon
  def mday = __civil[2]
  alias day mday
  def jd = __local_jd
  def start = @sg
  def wday = (jd + 1) % 7
  def mjd = jd - 2400001
  def ld = jd - 2299160
  def day_fraction = Rational(__local_df, 86400) + Rational(@sf, 86400)

  def ajd
    Rational(@jd * 2 - 1, 2) + Rational(@df, 86400) + Rational(@sf, 86400)
  end

  def amjd
    Rational(@jd - 2400001) + Rational(@df, 86400) + Rational(@sf, 86400)
  end

  def yday
    jd - self.class.__ordinal_jd(year, 1, @sg) + 1
  end

  def cwyear = self.class.__jd_to_commercial(jd, @sg)[0]
  def cweek = self.class.__jd_to_commercial(jd, @sg)[1]
  def cwday = self.class.__jd_to_commercial(jd, @sg)[2]

  def julian? = jd < @sg
  def gregorian? = !julian?

  def leap?
    if julian?
      self.class.julian_leap?(year)
    else
      self.class.gregorian_leap?(year)
    end
  end

  DAYNAMES.each_with_index do |name, i|
    define_method("#{name.downcase}?") { wday == i }
  end

  def new_start(sg = ITALY)
    self.class.__new_jd(@jd, sg, @df, @sf, @of)
  end

  def italy = new_start(ITALY)
  def england = new_start(ENGLAND)
  def julian = new_start(JULIAN)
  def gregorian = new_start(GREGORIAN)

  def infinite? = false

  # -- arithmetic --------------------------------------------------------------

  def +(n)
    case n
    when Integer
      self.class.__new_jd(@jd + n, @sg, @df, @sf, @of)
    when Rational, Float
      raise TypeError, "expected numeric" if n.is_a?(Float) && (n.nan? || n.infinite?)
      secs = n * 86400 + @df + @sf
      days, df = secs.divmod(86400)
      sf = df - df.floor
      self.class.__new_jd(@jd + days.to_i, @sg, df.floor, sf, @of)
    when Numeric
      self + n.to_r
    else
      raise TypeError, "expected numeric"
    end
  end

  def -(x)
    case x
    when Date
      d_sec = (@jd - x.instance_variable_get(:@jd)) * 86400 + (@df - x.instance_variable_get(:@df))
      sf1 = @sf.is_a?(Integer) ? 0r : @sf.to_r
      sf2 = x.instance_variable_get(:@sf)
      sf2 = sf2.is_a?(Integer) ? 0r : sf2.to_r
      # BigInt-safe rational subtraction divided by 86400
      n1, d1 = sf1.numerator, sf1.denominator
      n2, d2 = sf2.numerator, sf2.denominator
      num = d_sec * d1 * d2 + n1 * d2 - n2 * d1
      den = 86400 * d1 * d2
      g = num.gcd(den)
      Rational(num / g, den / g)
    when Numeric
      self + -x
    else
      raise TypeError, "expected numeric or date"
    end
  end

  def >>(n)
    raise TypeError, "expected numeric" unless n.is_a?(Numeric)
    t = year * 12 + (mon - 1) + n.to_i
    y, m = t.divmod(12)
    m += 1
    d = mday
    sg = @sg
    jd2 = nil
    while d > 0
      jd2 = self.class.__valid_civil(y, m, d, sg)
      break if jd2
      d -= 1
    end
    raise Error, "invalid date" unless jd2
    self + (jd2 - jd)
  end

  def <<(n)
    raise TypeError, "expected numeric" unless n.is_a?(Numeric)
    self >> -n
  end

  def next_day(n = 1) = self + n
  def prev_day(n = 1) = self - n
  def next_month(n = 1) = self >> n
  def prev_month(n = 1) = self << n
  def next_year(n = 1) = self >> (n * 12)
  def prev_year(n = 1) = self << (n * 12)
  def succ = next_day
  alias next succ

  def step(limit, by = 1)
    return to_enum(:step, limit, by) unless block_given?
    raise ArgumentError, "step can't be 0" if by == 0
    d = self
    if by > 0
      while d <= limit
        yield d
        d += by
      end
    else
      while d >= limit
        yield d
        d += by
      end
    end
    self
  end

  def upto(max, &block)
    return to_enum(:upto, max) unless block
    step(max, 1, &block)
  end

  def downto(min, &block)
    return to_enum(:downto, min) unless block
    step(min, -1, &block)
  end

  # -- comparison --------------------------------------------------------------

  def <=>(other)
    case other
    when Date
      ajd <=> other.ajd
    when Numeric
      ajd <=> other
    else
      nil
    end
  end

  def ==(other)
    return false unless other.is_a?(Date) || other.is_a?(Numeric)
    (self <=> other) == 0
  end

  def ===(other)
    case other
    when Date then jd == other.jd
    when Numeric then jd == other.to_i
    else false
    end
  end

  def eql?(other) = other.is_a?(Date) && self == other
  def hash = ajd.hash

  # -- conversion --------------------------------------------------------------

  def __fields
    y, m, d = __civil
    cw = self.class.__jd_to_commercial(jd, @sg)
    sf = if @sf.is_a?(Integer)
           0
         else
           r = @sf.to_r
           (r.numerator * 1_000_000_000).div(r.denominator)
         end
    df = __local_df
    epoch = (@jd - UNIX_EPOCH_IN_CJD) * 86400 + @df
    [y, m, d, df / 3600, df / 60 % 60, df % 60, sf, wday, yday, @of, zone, cw[0], cw[1], cw[2],
     epoch, epoch * 1000 + sf / 1_000_000]
  end
  private :__fields

  def strftime(fmt = "%F")
    if !@sf.is_a?(Integer) && fmt.include?("N")
      r = @sf.to_r
      fmt = fmt.gsub(/%(%|(\d*)N)/) do |m|
        next m if m == "%%"
        w = $2.empty? ? 9 : $2.to_i
        w = 9 if w <= 0
        (r.numerator * (10**w)).div(r.denominator).to_s.rjust(w, "0")[0, w]
      end
    end
    self.class.__strftime(fmt, __fields)
  end

  def to_s = strftime("%Y-%m-%d")
  def iso8601 = strftime("%Y-%m-%d")
  alias xmlschema iso8601
  def rfc3339 = strftime("%Y-%m-%dT%H:%M:%S%:z")
  def rfc2822 = strftime("%a, %-d %b %Y %T %z")
  alias rfc822 rfc2822
  def httpdate = new_offset(0).strftime("%a, %d %b %Y %T GMT")
  def asctime = strftime("%a %b %e %H:%M:%S %Y")
  alias ctime asctime

  def jisx0301
    if jd < 2405160
      iso8601
    else
      era, base = if jd < 2419614 then ["M", 1867]
                  elsif jd < 2424875 then ["T", 1911]
                  elsif jd < 2447535 then ["S", 1925]
                  elsif jd < 2458605 then ["H", 1988]
                  else ["R", 2018]
                  end
      format("%s%02d.%02d.%02d", era, year - base, mon, mday)
    end
  end

  def zone = format("%s%02d:%02d", @of < 0 ? "-" : "+", @of.abs / 3600, @of.abs / 60 % 60)

  def inspect
    "#<#{self.class}: #{to_s} ((#{@jd}j,#{@df}s,#{@sf.is_a?(Integer) ? @sf : (@sf * 1_000_000_000).to_i}n),#{@of >= 0 ? '+' : '-'}#{@of.abs}s,#{@sg.is_a?(Float) && @sg.infinite? ? (@sg > 0 ? 'Inf' : '-Inf') : @sg.to_i}j)>"
  end

  def to_date = Date.__new_jd(jd, @sg)

  def to_datetime
    DateTime.__new_jd(@jd, @sg, @df, @sf, @of)
  end

  def to_time
    Time.local(year, mon, mday)
  end

  def new_offset(of = 0)
    self.class.__new_jd(@jd, @sg, @df, @sf, DateTime.__offset_seconds(of))
  end
  private :new_offset

  def deconstruct_keys(keys)
    raise TypeError, "wrong argument type #{keys.class} (expected Array or nil)" unless keys.nil? || keys.is_a?(Array)
    h = { year: year, month: mon, day: mday, yday: yday, wday: wday }
    keys ? h.select { |k, _| keys.include?(k) } : h
  end

  def marshal_dump = [0, @jd, @df, @sf, @of, @sg]
  def marshal_load(a)
    _, @jd, @df, @sf, @of, @sg = a
  end

  def __fix_jd_utc
    nil
  end
  private :__fix_jd_utc

  # Date::Infinity (used by ranges)
  class Infinity < Numeric
    def initialize(d = 1) = @d = d <=> 0
    def d = @d
    protected :d
    def zero? = false
    def finite? = false
    def infinite? = d.nonzero?
    def nan? = d.zero?
    def abs = self.class.new
    def -@ = self.class.new(-d)
    def +@ = self.class.new(+d)
    def <=>(other)
      case other
      when Infinity then d <=> other.d
      when Float::INFINITY then d <=> 1
      when -Float::INFINITY then d <=> -1
      when Numeric then d
      end
    end
    def coerce(other)
      case other
      when Numeric then [-d, d]
      else super
      end
    end
    def to_f
      return 0 if @d == 0
      @d > 0 ? Float::INFINITY : -Float::INFINITY
    end
  end
end

class DateTime < Date
  def self.__offset_seconds(of)
    case of
    when String
      Date.__zone_to_diff(of) || 0
    when Rational, Integer, Float
      (of * 86400).round
    else
      0
    end
  end

  def self.__check_whole(v, name)
    raise TypeError, "invalid #{name} (not numeric)" unless v.is_a?(Numeric)
    raise Date::Error, "invalid date" unless v == v.to_i
    v.to_i
  end

  def self.__split_hms(h, min, s)
    h = __check_whole(h, "hour")
    min = __check_whole(min, "minute")
    raise TypeError, "invalid second (not numeric)" unless s.is_a?(Numeric)
    sf = 0
    if s.is_a?(Rational) || s.is_a?(Float)
      sr = s.to_r
      raise Date::Error, "invalid date" if sr < -60 || sr >= 60
      si = sr.floor
      sf = sr - si
      s = si
    else
      s = s.to_i
    end
    [h, min, s, sf]
  end

  # build from local fields
  def self.__from_local(jd, h, min, s, sf, of, sg)
    raise Date::Error, "invalid date" if h < -24 || h > 24 || min < -60 || min > 59 || s < -60 || s > 59
    h += 24 if h < 0
    min += 60 if min < 0
    s += 60 if s < 0
    raise Date::Error, "invalid date" if h == 24 && (min != 0 || s != 0 || sf != 0)
    sf = sf.is_a?(Integer) ? 0 : sf.to_r
    local = h * 3600 + min * 60 + s
    utc = local - of
    days, df = utc.divmod(86400)
    __new_jd(jd + days, sg, df, sf, of)
  end

  def self.civil(y = -4712, m = 1, d = 1, h = 0, min = 0, s = 0, of = 0, sg = ITALY)
    sg = __sg(sg)
    d = __check_whole(d, "day")
    jd = __civil_jd(y, m, d, sg)
    h, min, s, sf = __split_hms(h, min, s)
    __from_local(jd, h, min, s, sf, __offset_seconds(of), sg)
  end

  def self.new(*args) = civil(*args)

  def self.jd(jd = 0, h = 0, min = 0, s = 0, of = 0, sg = ITALY)
    h, min, s, sf = __split_hms(h, min, s)
    __from_local(jd.floor, h, min, s, sf, __offset_seconds(of), __sg(sg))
  end

  def self.ordinal(y = -4712, yday = 1, h = 0, min = 0, s = 0, of = 0, sg = ITALY)
    sg = __sg(sg)
    jd = __ordinal_jd(y.to_i, __check_whole(yday, "yday"), sg)
    raise Date::Error, "invalid date" unless jd
    h, min, s, sf = __split_hms(h, min, s)
    __from_local(jd, h, min, s, sf, __offset_seconds(of), sg)
  end

  def self.commercial(y = -4712, w = 1, d = 1, h = 0, min = 0, s = 0, of = 0, sg = ITALY)
    sg = __sg(sg)
    jd = __commercial_jd(y.to_i, w.to_i, __check_whole(d, "day"), sg)
    raise Date::Error, "invalid date" unless jd
    h, min, s, sf = __split_hms(h, min, s)
    __from_local(jd, h, min, s, sf, __offset_seconds(of), sg)
  end

  def self.now(sg = ITALY)
    t = Time.now
    t.to_datetime
  end

  def self.parse(str = "-4712-01-01T00:00:00+00:00", comp = true, sg = ITALY, limit: 128)
    Date.__from_hash(_parse(str, comp, limit: limit), __sg(sg), self)
  end

  def self.strptime(str = "-4712-01-01T00:00:00+00:00", fmt = "%FT%T%z", sg = ITALY)
    h = _strptime(str, fmt)
    raise Date::Error, "invalid date" if h.nil? || h[:leftover]
    Date.__from_hash(h, __sg(sg), self)
  end

  %i[iso8601 rfc3339 xmlschema rfc2822 httpdate jisx0301].each do |name|
    define_singleton_method(name) do |str = nil, sg = ITALY, limit: 128|
      Date.__from_hash(__send__("_#{name}", str), __sg(sg), self)
    end
  end

  def hour = __local_df / 3600
  def min = __local_df / 60 % 60
  alias minute min
  def sec = __local_df % 60
  alias second sec
  def sec_fraction = @sf.is_a?(Integer) ? Rational(0) : @sf
  alias second_fraction sec_fraction
  def offset = Rational(@of, 86400)

  def new_offset(of = 0)
    self.class.__new_jd(@jd, @sg, @df, @sf, DateTime.__offset_seconds(of))
  end
  public :new_offset

  def to_s = strftime("%Y-%m-%dT%H:%M:%S%:z")

  def strftime(fmt = "%FT%T%:z")
    super(fmt)
  end

  def iso8601(n = 0)
    if n > 0
      strftime("%Y-%m-%dT%H:%M:%S.%#{n}N%:z")
    else
      strftime("%Y-%m-%dT%H:%M:%S%:z")
    end
  end
  alias xmlschema iso8601
  alias rfc3339 iso8601

  def jisx0301(n = 0)
    super() + (n > 0 ? strftime("T%H:%M:%S.%#{n}N%:z") : strftime("T%H:%M:%S%:z"))
  end

  def to_date = Date.__new_jd(jd, @sg)
  def to_datetime = self

  def to_time
    sec = (@jd - UNIX_EPOCH_IN_CJD) * 86400 + @df
    sf = @sf.is_a?(Integer) ? 0 : @sf
    t = Time.at(sec, sf * 1_000_000, :usec, in: @of)
    t
  end

  def deconstruct_keys(keys)
    raise TypeError, "wrong argument type #{keys.class} (expected Array or nil)" unless keys.nil? || keys.is_a?(Array)
    h = { year: year, month: mon, day: mday, yday: yday, wday: wday, hour: hour, min: min,
          sec: sec, sec_fraction: sec_fraction, zone: zone }
    keys ? h.select { |k, _| keys.include?(k) } : h
  end
end

class Time
  def to_date
    jd = Date.__valid_civil(year, mon, mday, Date::GREGORIAN)
    Date.__new_jd(jd, Date::ITALY)
  end

  def to_datetime
    of = utc_offset rescue 0
    jd = Date.__valid_civil(year, mon, mday, Date::GREGORIAN)
    DateTime.__from_local(jd, hour, min, sec, subsec, of, Date::ITALY)
  end

  def to_time = self
end
