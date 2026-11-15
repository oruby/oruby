require "socket"

class IPAddr
  VERSION = "1.2.7"

  IN4MASK = 0xffffffff
  IN6MASK = (1 << 128) - 1
  IN6_V4MAPPED_MASK = IN6MASK ^ 0xffffffff
  IN6_ULA_MASK = 0xfe << 120
  IN6_ULA = 0xfc << 120
  IN6_LL_MASK = 0xffc0 << 112
  IN6_LL = 0xfe80 << 112
  IN6FORMAT = (["%.4x"] * 8).join(":").freeze

  RE_IPV4ADDRLIKE = /\A\d+\.\d+\.\d+\.\d+\z/

  class Error < ArgumentError; end
  class InvalidAddressError < Error; end
  class AddressFamilyError < Error; end
  class InvalidPrefixError < InvalidAddressError; end

  include Comparable

  attr_reader :family

  def self.new_ntoh(addr)
    IPAddr.new(IPAddr.ntop(addr))
  end

  def self.ntop(addr)
    if addr.is_a?(String) && addr.encoding != Encoding::BINARY && addr.respond_to?(:b)
      addr = addr.b
    end
    case addr.bytesize
    when 4
      addr.unpack("C4").join(".")
    when 16
      IN6FORMAT % addr.unpack("n8")
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def &(other)
    self.clone.set(@addr & coerce_other(other).to_i)
  end

  def |(other)
    self.clone.set(@addr | coerce_other(other).to_i)
  end

  def >>(num)
    self.clone.set(@addr >> num)
  end

  def <<(num)
    self.clone.set(addr_mask(@addr << num))
  end

  def ~
    self.clone.set(addr_mask(~@addr))
  end

  def ==(other)
    other = coerce_other(other)
  rescue
    false
  else
    @family == other.family && @addr == other.to_i
  end

  def mask(prefixlen)
    self.clone.mask!(prefixlen)
  end

  def include?(other)
    other = coerce_other(other)
    return false unless other.family == family
    begin_addr <= other.begin_addr && end_addr >= other.end_addr
  end
  alias === include?

  def to_i
    @addr
  end

  def to_s
    str = to_string
    return str if ipv4?
    str.gsub!(/\b0{1,3}([\da-f]+)\b/i, '\1')
    loop do
      break if str.sub!(/\A0:0:0:0:0:0:0:0\z/, "::")
      break if str.sub!(/\b0:0:0:0:0:0:0\b/, ":")
      break if str.sub!(/\b0:0:0:0:0:0\b/, ":")
      break if str.sub!(/\b0:0:0:0:0\b/, ":")
      break if str.sub!(/\b0:0:0:0\b/, ":")
      break if str.sub!(/\b0:0:0\b/, ":")
      break if str.sub!(/\b0:0\b/, ":")
      break
    end
    str.sub!(/:{3,}/, "::")
    if /\A::(ffff:)?([\da-f]{1,4}):([\da-f]{1,4})\z/i =~ str
      str = format("::%s%d.%d.%d.%d", $1, $2.hex / 256, $2.hex % 256, $3.hex / 256, $3.hex % 256)
    end
    str
  end

  def to_string
    str = _to_string(@addr)
    if @family == Socket::AF_INET6
      str << zone_id.to_s
    end
    str
  end

  def cidr
    "#{to_s}/#{prefix}"
  end

  def hton
    case @family
    when Socket::AF_INET
      [@addr].pack("N")
    when Socket::AF_INET6
      (0..7).map { |i| (@addr >> (112 - 16 * i)) & 0xffff }.pack("n8")
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def ipv4?
    @family == Socket::AF_INET
  end

  def ipv6?
    @family == Socket::AF_INET6
  end

  def loopback?
    case @family
    when Socket::AF_INET
      @addr & 0xff000000 == 0x7f000000
    when Socket::AF_INET6
      @addr == 1 || (ipv4_mapped? && (@addr & 0xff000000 == 0x7f000000))
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def private?
    case @family
    when Socket::AF_INET
      @addr & 0xff000000 == 0x0a000000 ||
        @addr & 0xfff00000 == 0xac100000 ||
        @addr & 0xffff0000 == 0xc0a80000
    when Socket::AF_INET6
      @addr & IN6_ULA_MASK == IN6_ULA ||
        (ipv4_mapped? && (@addr & 0xff000000 == 0x0a000000 || @addr & 0xfff00000 == 0xac100000 || @addr & 0xffff0000 == 0xc0a80000))
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def link_local?
    case @family
    when Socket::AF_INET
      @addr & 0xffff0000 == 0xa9fe0000
    when Socket::AF_INET6
      @addr & IN6_LL_MASK == IN6_LL ||
        (ipv4_mapped? && @addr & 0xffff0000 == 0xa9fe0000)
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def ipv4_mapped?
    ipv6? && (@addr >> 32) == 0xffff
  end

  def ipv4_compat?
    warn "IPAddr\##{__method__} is obsolete", uplevel: 1 if $VERBOSE
    _ipv4_compat?
  end

  def _ipv4_compat?
    if !ipv6? || (@addr >> 32) != 0
      return false
    end
    a = (@addr & IN4MASK)
    a != 0 && a != 1
  end
  protected :_ipv4_compat?

  def ipv4_mapped
    if !ipv4?
      raise InvalidAddressError, "not an IPv4 address: #{@addr}"
    end
    clone = self.clone.set(@addr | 0xffff00000000, Socket::AF_INET6)
    clone.instance_variable_set(:@mask_addr, @mask_addr | IN6_V4MAPPED_MASK)
    clone
  end

  def ipv4_compat
    warn "IPAddr\##{__method__} is obsolete", uplevel: 1 if $VERBOSE
    if !ipv4?
      raise InvalidAddressError, "not an IPv4 address: #{@addr}"
    end
    clone = self.clone.set(@addr, Socket::AF_INET6)
    clone.instance_variable_set(:@mask_addr, @mask_addr | IN6_V4MAPPED_MASK)
    clone
  end

  def native
    if !ipv4_mapped? && !_ipv4_compat?
      return self
    end
    self.clone.set(@addr & IN4MASK, Socket::AF_INET)
  end

  def reverse
    case @family
    when Socket::AF_INET
      _reverse + ".in-addr.arpa"
    when Socket::AF_INET6
      ip6_arpa
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def ip6_arpa
    if !ipv6?
      raise InvalidAddressError, "not an IPv6 address: #{@addr}"
    end
    _reverse + ".ip6.arpa"
  end

  def ip6_int
    if !ipv6?
      raise InvalidAddressError, "not an IPv6 address: #{@addr}"
    end
    _reverse + ".ip6.int"
  end

  def succ
    return self.clone.set(@addr + 1, @family)
  end

  def <=>(other)
    other = coerce_other(other)
  rescue
    nil
  else
    @addr <=> other.to_i if other.family == @family
  end

  def eql?(other)
    self.class == other.class && self.hash == other.hash && self == other
  end

  def hash
    [@addr, @mask_addr, (zone_id || "")].hash
  end

  def to_range
    self.class.new(begin_addr, @family)..self.class.new(end_addr, @family)
  end

  def prefix
    case @family
    when Socket::AF_INET
      n = IN4MASK ^ @mask_addr
      i = 32
    when Socket::AF_INET6
      n = IN6MASK ^ @mask_addr
      i = 128
    else
      raise AddressFamilyError, "unsupported address family"
    end
    while n > 0
      n >>= 1
      i -= 1
    end
    i
  end

  def prefix=(prefix)
    case prefix
    when Integer
      mask!(prefix)
    else
      raise InvalidPrefixError, "prefix must be an integer"
    end
  end

  def inspect
    case @family
    when Socket::AF_INET
      af = "IPv4"
    when Socket::AF_INET6
      af = "IPv6"
      zone_id = @zone_id.to_s
    else
      raise AddressFamilyError, "unsupported address family"
    end
    format("#<%s: %s:%s%s/%s>", self.class.name, af, _to_string(@addr), zone_id, _to_string(@mask_addr))
  end

  def netmask
    self.clone.set(@mask_addr)
  end

  def zone_id
    if @family == Socket::AF_INET6
      @zone_id
    else
      raise InvalidAddressError, "not an IPv6 address"
    end
  end

  def zone_id=(zid)
    if @family == Socket::AF_INET6
      case zid
      when nil, /\A%(\w+)\z/
        @zone_id = zid
      else
        raise InvalidAddressError, "invalid zone identifier for address"
      end
    else
      raise InvalidAddressError, "not an IPv6 address"
    end
  end

  protected

  def begin_addr
    @addr & @mask_addr
  end

  def end_addr
    case @family
    when Socket::AF_INET
      @addr | (IN4MASK ^ @mask_addr)
    when Socket::AF_INET6
      @addr | (IN6MASK ^ @mask_addr)
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def set(addr, *family)
    case family[0] ? family[0] : @family
    when Socket::AF_INET
      if addr < 0 || addr > IN4MASK
        raise InvalidAddressError, "invalid address: #{addr}"
      end
    when Socket::AF_INET6
      if addr < 0 || addr > IN6MASK
        raise InvalidAddressError, "invalid address: #{addr}"
      end
    else
      raise AddressFamilyError, "unsupported address family"
    end
    @addr = addr
    if family[0]
      @family = family[0]
      if @family == Socket::AF_INET
        @mask_addr &= IN4MASK
      end
    end
    self
  end

  def mask!(mask)
    case mask
    when String
      case mask
      when /\A(0|[1-9]+\d*)\z/
        prefixlen = mask.to_i
      when /\A\d+\z/
        raise InvalidPrefixError, "leading zeros in prefix"
      else
        m = IPAddr.new(mask)
        raise InvalidPrefixError, "address family is not same" unless m.family == @family
        @mask_addr = m.to_i
        n = @mask_addr ^ (@family == Socket::AF_INET ? IN4MASK : IN6MASK)
        unless ((n + 1) & n).zero?
          raise InvalidPrefixError, "invalid mask #{mask}"
        end
        @addr &= @mask_addr
        return self
      end
    else
      prefixlen = mask
    end
    case @family
    when Socket::AF_INET
      if prefixlen < 0 || prefixlen > 32
        raise InvalidPrefixError, "invalid length"
      end
      masklen = 32 - prefixlen
      @mask_addr = ((IN4MASK >> masklen) << masklen)
    when Socket::AF_INET6
      if prefixlen < 0 || prefixlen > 128
        raise InvalidPrefixError, "invalid length"
      end
      masklen = 128 - prefixlen
      @mask_addr = ((IN6MASK >> masklen) << masklen)
    else
      raise AddressFamilyError, "unsupported address family"
    end
    @addr = ((@addr >> masklen) << masklen)
    self
  end

  private

  def initialize(addr = "::", family = Socket::AF_UNSPEC)
    @mask_addr = nil
    if !addr.kind_of?(String)
      case family
      when Socket::AF_INET, Socket::AF_INET6
        set(addr.to_i, family)
        @mask_addr = (family == Socket::AF_INET) ? IN4MASK : IN6MASK
        return
      when Socket::AF_UNSPEC
        raise AddressFamilyError, "address family must be specified"
      else
        raise AddressFamilyError, "unsupported address family: #{family}"
      end
    end
    prefix, prefixlen = addr.split("/", 2)
    if prefix =~ /\A\[(.*)\]\z/i
      prefix = $1
      family = Socket::AF_INET6
    end
    if prefix =~ /\A(.*)(%\w+)\z/
      prefix = $1
      zone_id = $2
      family = Socket::AF_INET6
    end
    @addr = @family = nil
    if family == Socket::AF_UNSPEC || family == Socket::AF_INET
      @addr = in_addr(prefix)
      if @addr
        @family = Socket::AF_INET
      end
    end
    if !@addr && (family == Socket::AF_UNSPEC || family == Socket::AF_INET6)
      @addr = in6_addr(prefix)
      @family = Socket::AF_INET6
    end
    @zone_id = zone_id
    if family != Socket::AF_UNSPEC && @family != family
      raise AddressFamilyError, "address family mismatch"
    end
    if prefixlen
      mask!(prefixlen)
    else
      @mask_addr = (@family == Socket::AF_INET) ? IN4MASK : IN6MASK
    end
  end

  def coerce_other(other)
    case other
    when IPAddr
      other
    when String
      self.class.new(other)
    else
      self.class.new(other, @family)
    end
  end

  def in_addr(addr)
    case addr
    when Array
      octets = addr
    else
      m = RE_IPV4ADDRLIKE.match(addr) or return nil
      octets = m.to_s.split(".")
    end
    octets.inject(0) do |i, s|
      (n = s.to_i) < 256 or raise InvalidAddressError, "invalid address: #{@addr}"
      (s != "0") && s.start_with?("0") and raise InvalidAddressError, "zero-filled number in IPv4 address is ambiguous: #{@addr}"
      i << 8 | n
    end
  end

  def in6_addr(left)
    case left
    when RE_IPV4ADDRLIKE
      return nil
    when /\A[\da-f:.]+\z/i
      if left.count("::") > 0 && left.scan("::").size > 1
        raise InvalidAddressError, "invalid address: #{@addr}"
      end
      if left.include?(".")
        head, _, v4 = left.rpartition(":")
        v4i = in_addr(v4) or raise InvalidAddressError, "invalid address: #{left}"
        left = "#{head}:#{format('%x:%x', v4i >> 16, v4i & 0xffff)}"
      end
      if left.include?("::")
        l, r = left.split("::", 2)
        lp = l.to_s.empty? ? [] : l.split(":")
        rp = r.to_s.empty? ? [] : r.split(":")
        fill = 8 - lp.size - rp.size
        raise InvalidAddressError, "invalid address: #{left}" if fill < 1
        parts = lp + ["0"] * fill + rp
      else
        parts = left.split(":", -1)
      end
      raise InvalidAddressError, "invalid address: #{left}" unless parts.size == 8
      parts.inject(0) do |i, s|
        raise InvalidAddressError, "invalid address: #{left}" unless s =~ /\A[\da-f]{1,4}\z/i
        i << 16 | s.hex
      end
    else
      raise InvalidAddressError, "invalid address: #{left}"
    end
  end

  def addr_mask(addr)
    case @family
    when Socket::AF_INET
      return addr & IN4MASK
    when Socket::AF_INET6
      return addr & IN6MASK
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def _reverse
    case @family
    when Socket::AF_INET
      return (0..3).map { |i| (@addr >> (8 * i)) & 0xff }.join(".")
    when Socket::AF_INET6
      return @addr.to_s(16).rjust(32, "0").reverse.chars.join(".")
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end

  def _to_string(addr)
    case @family
    when Socket::AF_INET
      return (0..3).map { |i| (addr >> (24 - 8 * i)) & 0xff }.join(".")
    when Socket::AF_INET6
      return addr.to_s(16).rjust(32, "0").scan(/.{4}/).join(":")
    else
      raise AddressFamilyError, "unsupported address family"
    end
  end
end
