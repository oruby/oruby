class SocketError < StandardError; end

class BasicSocket < IO
end

class Socket < BasicSocket
  class ResolutionError < SocketError
    attr_reader :error_code
    def initialize(msg = nil, code = nil)
      super(msg)
      @error_code = code
    end
  end

  # -- constant name helpers ---------------------------------------------------

  def self.__const_lookup(value, prefixes, what)
    case value
    when Integer
      value
    when nil
      0
    when Symbol, String
      name = value.to_s
      prefixes.each do |pre|
        full = name.start_with?(pre) ? name : pre + name
        return Socket::Constants.const_get(full) if Socket::Constants.const_defined?(full)
      end
      raise SocketError, "unknown #{what}: #{name}"
    else
      if value.respond_to?(:to_int)
        value.to_int
      elsif value.respond_to?(:to_str)
        __const_lookup(value.to_str, prefixes, what)
      else
        raise TypeError, "no implicit conversion of #{value.class} into Integer"
      end
    end
  end

  def self.__family(v)
    __const_lookup(v, %w[AF_ PF_], "socket domain")
  end

  def self.__socktype(v)
    __const_lookup(v, %w[SOCK_], "socket type")
  end

  def self.__level(v, family = nil)
    raise TypeError, "no implicit conversion of nil into Integer" if v.nil?
    v = v.to_str if !v.is_a?(Integer) && !v.is_a?(Symbol) && v.respond_to?(:to_str)
    case v
    when Symbol, String
      n = v.to_s
      return SOL_SOCKET if n == "SOCKET" || n == "SOL_SOCKET"
      __const_lookup(n, %w[IPPROTO_ SOL_], "protocol level")
    else
      __const_lookup(v, [], "protocol level")
    end
  end

  def self.__optname(level, v)
    raise TypeError, "no implicit conversion of nil into Integer" if v.nil?
    v = v.to_str if !v.is_a?(Integer) && !v.is_a?(Symbol) && v.respond_to?(:to_str)
    return __const_lookup(v, [], "socket option") unless v.is_a?(Symbol) || v.is_a?(String)
    pre = case level
          when SOL_SOCKET then %w[SO_]
          when IPPROTO_IP then %w[IP_]
          when IPPROTO_IPV6 then %w[IPV6_]
          when IPPROTO_TCP then %w[TCP_]
          when IPPROTO_UDP then %w[UDP_]
          else %w[SO_ IP_ IPV6_ TCP_]
          end
    __const_lookup(v, pre, "socket option")
  end

  def self.__shut(v)
    v = 2 if v.nil?
    v = v.to_str if !v.is_a?(Integer) && !v.is_a?(Symbol) && v.respond_to?(:to_str)
    __const_lookup(v, %w[SHUT_], "shutdown how")
  end

  def self.__family_name(fam)
    case fam
    when AF_INET then "AF_INET"
    when AF_INET6 then "AF_INET6"
    when AF_UNIX then "AF_UNIX"
    when AF_UNSPEC then "AF_UNSPEC"
    else "unknown:#{fam}"
    end
  end

  def self.__const_name(value, prefix, exclude = [])
    Socket::Constants.constants.each do |c|
      s = c.to_s
      next unless s.start_with?(prefix)
      next if exclude.any? { |e| s.start_with?(e) }
      return s[prefix.size..-1] if Socket::Constants.const_get(c) == value
    end
    nil
  end

  def self.__sockaddr(addr)
    case addr
    when Addrinfo then addr.to_sockaddr
    when String then addr
    else
      if addr.respond_to?(:to_str)
        addr.to_str
      else
        raise TypeError, "no implicit conversion of #{addr.class} into String"
      end
    end
  end

  def self.__str(v)
    return v if v.is_a?(String)
    if v.respond_to?(:to_str)
      r = v.to_str
      return r if r.is_a?(String)
      raise TypeError, "can't convert #{v.class} to String (#{v.class}#to_str gives #{r.class})"
    end
    raise TypeError, "no implicit conversion of #{v.class} into String"
  end

  def self.__int(v)
    return v if v.is_a?(Integer)
    return v.to_int if !v.nil? && !v.is_a?(String) && v.respond_to?(:to_int)
    raise TypeError, "no implicit conversion of #{v.nil? ? 'nil' : v.class} into Integer"
  end

  def self.__resolution_error(msg)
    raise Socket::ResolutionError, msg
  end
end

class BasicSocket < IO
  alias_method :__io_initialize, :initialize
  public :__io_initialize

  @@do_not_reverse_lookup = true

  def self.do_not_reverse_lookup
    @@do_not_reverse_lookup
  end

  def self.do_not_reverse_lookup=(v)
    @@do_not_reverse_lookup = v ? true : false
  end

  def self.for_fd(fd)
    sock = allocate
    sock.__wrap_fd(fd)
    sock
  end

  def __init_socket
    @do_not_reverse_lookup = BasicSocket.do_not_reverse_lookup
    self.sync = true rescue nil
    self
  end

  def inspect
    return "#<#{self.class}:(closed)>" if closed?
    "#<#{self.class}:fd #{fileno}>"
  end

  def __check_readable
    raise IOError, "closed stream" if closed?
    raise IOError, "not opened for reading" if @__closed_read
  end

  def read(*args)
    __check_readable
    buf = args[1]
    enc = buf.encoding if buf.is_a?(String)
    res = super
    if res.is_a?(String)
      if enc
        res.force_encoding(enc)
      else
        res.force_encoding(Encoding::BINARY)
      end
    end
    res
  end

  def gets(*args)
    __check_readable
    super
  end

  def read_nonblock(len, buf = nil, exception: true)
    __check_readable
    enc = buf.encoding if buf.is_a?(String)
    res = super(len, buf, exception: exception)
    if res.is_a?(String)
      if enc
        res.force_encoding(enc)
      else
        res.force_encoding(Encoding::BINARY)
      end
    end
    res
  end

  def __wrap_fd(fd)
    __io_initialize(fd, "r+")
    __init_socket
  end

  def do_not_reverse_lookup
    @do_not_reverse_lookup.nil? ? BasicSocket.do_not_reverse_lookup : @do_not_reverse_lookup
  end

  def do_not_reverse_lookup=(v)
    @do_not_reverse_lookup = v ? true : false
  end

  def __fd
    raise IOError, "closed stream" if closed?
    fileno
  end

  def getsockname
    Socket.__getsockname(__fd)
  end

  def getpeername
    Socket.__getpeername(__fd)
  end

  def __socktype
    getsockopt(Socket::SOL_SOCKET, Socket::SO_TYPE).int
  rescue SystemCallError
    0
  end

  def __family
    sa = getsockname
    Socket.__sa_family(sa)
  end

  def local_address
    sa = getsockname
    Addrinfo.new(sa, Socket.__sa_family(sa), __socktype, 0)
  end

  def remote_address
    sa = getpeername
    Addrinfo.new(sa, Socket.__sa_family(sa), __socktype, 0)
  end

  def connect_address
    addr = local_address
    if addr.ip? && addr.ip_port == 0
      raise SocketError, "unbound IPv#{addr.ipv4? ? 4 : 6} socket"
    end
    if addr.ip?
      if addr.ipv4? && addr.ip_address == "0.0.0.0"
        addr = Addrinfo.new(Socket.sockaddr_in(addr.ip_port, "127.0.0.1"), Socket::PF_INET, addr.socktype, addr.protocol)
      elsif addr.ipv6? && addr.ip_address == "::"
        addr = Addrinfo.new(Socket.sockaddr_in(addr.ip_port, "::1"), Socket::PF_INET6, addr.socktype, addr.protocol)
      end
    elsif addr.unix? && addr.unix_path.empty?
      raise SocketError, "unbound Unix socket"
    end
    addr
  end

  def getsockopt(level, optname = nil)
    level = Socket.__level(level)
    optname = Socket.__optname(level, optname)
    data = Socket.__getsockopt(__fd, level, optname)
    fam = Socket.__sa_family(getsockname) rescue Socket::AF_UNSPEC
    Socket::Option.new(fam || Socket::AF_UNSPEC, level, optname, data)
  end

  def setsockopt(*args)
    if args.size == 1 && args[0].is_a?(Socket::Option)
      opt = args[0]
      level, optname, optval = opt.level, opt.optname, opt.data
    elsif args.size == 3
      level = Socket.__level(args[0])
      optname = Socket.__optname(level, args[1])
      optval = args[2]
    else
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1 or 3)"
    end
    data = case optval
           when Integer then [optval].pack("i")
           when true then [1].pack("i")
           when false then [0].pack("i")
           when nil then raise TypeError, "no implicit conversion of nil into String"
           when String then optval
           else
             if optval.respond_to?(:to_str) then optval.to_str
             else raise TypeError, "no implicit conversion of #{optval.class} into String"
             end
           end
    Socket.__setsockopt(__fd, level, optname, data)
  end

  def shutdown(how = Socket::SHUT_RDWR)
    unless how.is_a?(Integer) || how.is_a?(Symbol) || how.is_a?(String) || how.respond_to?(:to_str)
      raise TypeError, "no implicit conversion of #{how.class} into Integer"
    end
    how = Socket.__shut(how)
    raise ArgumentError, "`how' should be either :SHUT_RD, :SHUT_WR, :SHUT_RDWR" unless [0, 1, 2].include?(how)
    Socket.__shutdown(__fd, how)
  end

  def close_read
    raise IOError, "closed stream" if closed?
    return nil if @__closed_read
    Socket.__shutdown(fileno, Socket::SHUT_RD) rescue nil
    @__closed_read = true
    close if @__closed_write
    nil
  end

  def close_write
    raise IOError, "closed stream" if closed?
    return nil if @__closed_write
    Socket.__shutdown(fileno, Socket::SHUT_WR) rescue nil
    @__closed_write = true
    close if @__closed_read
    nil
  end

  def send(mesg, flags, dest = nil)
    dest = Socket.__sockaddr(dest) if dest
    Socket.__send(__fd, Socket.__str(mesg), flags || 0, dest)
  end

  def write(*args)
    raise IOError, "not opened for writing" if @__closed_write
    super
  end

  def __recv_str(res, outbuf)
    data = res[0]
    if outbuf
      outbuf.replace(data)
      outbuf
    else
      data
    end
  end

  def recv(maxlen, flags = 0, outbuf = nil)
    __check_readable
    res = Socket.__recv(__fd, maxlen, flags || 0, false)
    if res[2] == 0 && maxlen > 0 && __socktype == Socket::SOCK_STREAM
      outbuf&.replace("")
      return nil
    end
    __recv_str(res, outbuf)
  end

  def recv_nonblock(maxlen, flags = 0, outbuf = nil, exception: true)
    raise IOError, "closed stream" if closed?
    begin
      res = Socket.__recv(__fd, maxlen, (flags || 0) | Socket::MSG_DONTWAIT, true)
    rescue IO::WaitReadable => __exc
      raise __exc if exception
      return :wait_readable
    end
    if res[2] == 0 && maxlen > 0 && __socktype == Socket::SOCK_STREAM
      outbuf&.replace("")
      return nil
    end
    __recv_str(res, outbuf)
  end

  def __msg_addr(res)
    st = __socktype
    if res[1] && st != Socket::SOCK_STREAM
      Addrinfo.new(res[1], Socket.__sa_family(res[1]), st, 0)
    else
      Addrinfo.new("", nil, st, 0)
    end
  end

  def __msg_result(res)
    return nil if res[2] == 0 && __socktype == Socket::SOCK_STREAM
    [res[0], __msg_addr(res), 0]
  end

  def recvmsg(maxlen = nil, flags = 0, maxcontrol = nil, scm_rights: false)
    __check_readable
    __msg_result(Socket.__recv(__fd, maxlen || 65536, flags || 0, false))
  end

  def recvmsg_nonblock(maxlen = nil, flags = 0, maxcontrol = nil, scm_rights: false, exception: true)
    begin
      res = Socket.__recv(__fd, maxlen || 65536, (flags || 0) | Socket::MSG_DONTWAIT, true)
    rescue IO::WaitReadable => __exc
      raise __exc if exception
      return :wait_readable
    end
    __msg_result(res)
  end

  def sendmsg(mesg, flags = 0, dest = nil, *controls)
    send(mesg, flags, dest)
  end

  def sendmsg_nonblock(mesg, flags = 0, dest = nil, *controls, exception: true)
    send(mesg, (flags || 0) | Socket::MSG_DONTWAIT, dest)
  rescue IO::WaitWritable => __exc
    raise __exc if exception
    :wait_writable
  end

  def getpeereid
    unless Socket.__sa_family(getsockname) == Socket::AF_UNIX
      raise NoMethodError, "getpeereid is only supported on UNIX sockets"
    end
    [Process.euid, Process.egid]
  end

  def __addr_array(sa, reverse)
    fam = Socket.__sa_family(sa)
    if fam == Socket::AF_UNIX
      return ["AF_UNIX", Socket.__unpack_un(sa)]
    end
    port, ip = Socket.__unpack_in(sa)
    rev = case reverse
          when nil then !do_not_reverse_lookup
          when true, :hostname then true
          when false, :numeric then false
          else raise ArgumentError, "invalid reverse_lookup flag: #{reverse.inspect}"
          end
    host = rev ? (Socket.__getnameinfo(sa, 0)[0] rescue ip) : ip
    [Socket.__family_name(fam), port, host, ip]
  end
end

class IPSocket < BasicSocket
  def addr(reverse_lookup = nil)
    __addr_array(getsockname, reverse_lookup)
  end

  def peeraddr(reverse_lookup = nil)
    __addr_array(getpeername, reverse_lookup)
  end

  def recvfrom(maxlen, flags = 0, outbuf = nil)
    __check_readable
    res = Socket.__recv(__fd, maxlen, flags || 0, false)
    if __socktype == Socket::SOCK_STREAM
      return nil if res[2] == 0 && maxlen > 0
      sa = (getpeername rescue getsockname)
    else
      sa = res[1]
      sa = getsockname if sa.nil? || sa.empty? || Socket.__sa_family(sa) == 0
    end
    [__recv_str(res, outbuf), __addr_array(sa, nil)]
  end

  def inspect
    return "#<#{self.class}:(closed)>" if closed?
    a = addr rescue nil
    return super unless a
    "#<#{self.class}:fd #{fileno}, #{a[0]}, #{a[3]}, #{a[1]}>"
  end

  def self.getaddress(host)
    Socket.getaddrinfo(host, nil, nil, Socket::SOCK_STREAM)[0][3]
  end
end

class TCPSocket < IPSocket
  def self.gethostbyname(host)
    infos = Socket.getaddrinfo(host, nil, nil, Socket::SOCK_STREAM, nil, Socket::AI_CANONNAME)
    name = infos[0][2]
    aliases = []
    fam = infos[0][4]
    addrs = infos.select { |i| i[4] == fam }.map { |i| i[3] }.uniq
    [name, aliases, fam, *addrs]
  end

  def initialize(host, port, local_host = nil, local_port = nil, connect_timeout: nil, resolv_timeout: nil, open_timeout: nil, fast_fallback: nil)
    connect_timeout ||= open_timeout
    host = "localhost" if host.nil?
    infos = Socket.__getaddrinfo(Socket.__host(host), Socket.__service(port), 0, Socket::SOCK_STREAM, 0, 0)
    local = nil
    if local_host || local_port
      local = Socket.__getaddrinfo(local_host, local_port, 0, Socket::SOCK_STREAM, 0, 0)
    end
    last_err = nil
    infos.each do |fam, type, proto, sa, _|
      fd = nil
      begin
        fd = Socket.__socket(fam, type, proto)
        if local
          l = local.find { |i| i[0] == fam }
          raise Errno::EADDRNOTAVAIL, "local address" unless l
          Socket.__setsockopt(fd, Socket::SOL_SOCKET, Socket::SO_REUSEADDR, [1].pack("i"))
          Socket.__bind(fd, l[3])
        end
        __connect_timeout(fd, sa, connect_timeout)
        __wrap_fd(fd)
        return
      rescue SystemCallError, IO::TimeoutError => e
        last_err = e
        Socket.__close_fd(fd) if fd
      end
    end
    raise last_err if last_err
    raise SocketError, "no address"
  end

  def __connect_timeout(fd, sa, timeout)
    if timeout.nil?
      Socket.__connect(fd, sa, false)
      return
    end
    begin
      Socket.__connect(fd, sa, true)
    rescue IO::WaitWritable
      ready = IO.__poll(nil, [fd], nil, (timeout * 1000).ceil)
      raise IO::TimeoutError, "Connect timed out!" unless ready
      err = Socket.__getsockopt(fd, Socket::SOL_SOCKET, Socket::SO_ERROR).unpack1("i")
      if err != 0
        raise SystemCallError.new("connect(2)", err)
      end
    end
  end
end

class IO
  class TimeoutError < IOError; end unless const_defined?(:TimeoutError)
end

class TCPServer < TCPSocket
  def initialize(host = nil, port = nil)
    if port.nil? && !host.nil? && !host.is_a?(String) && !host.respond_to?(:to_str)
      raise TypeError, "no implicit conversion of #{host.class} into String" unless host.is_a?(Integer)
    end
    if port.nil? && (host.is_a?(Integer) || (!host.is_a?(String) && host.respond_to?(:to_str)))
      port = host
      host = nil
    end
    host = Socket.__host(host)
    port = Socket.__service(port)
    host = "0.0.0.0" if host == ""
    infos = Socket.__getaddrinfo(host, port, 0, Socket::SOCK_STREAM, 0, Socket::AI_PASSIVE)
    if host.nil?
      infos = infos.select { |i| i[0] == Socket::AF_INET6 } + infos.select { |i| i[0] == Socket::AF_INET }
    end
    last_err = nil
    infos.each do |fam, type, proto, sa, _|
      fd = nil
      begin
        fd = Socket.__socket(fam, type, proto)
        Socket.__setsockopt(fd, Socket::SOL_SOCKET, Socket::SO_REUSEADDR, [1].pack("i"))
        if fam == Socket::AF_INET6 && (host.nil? || host == "")
          Socket.__setsockopt(fd, Socket::IPPROTO_IPV6, Socket::IPV6_V6ONLY, [0].pack("i")) rescue nil
        end
        Socket.__bind(fd, sa)
        Socket.__listen(fd, Socket::SOMAXCONN)
        __wrap_fd(fd)
        return
      rescue SystemCallError => e
        last_err = e
        Socket.__close_fd(fd) if fd
      end
    end
    raise last_err if last_err
    raise SocketError, "no address"
  end

  def accept
    fd, _ = Socket.__accept(__fd, false)
    TCPSocket.for_fd(fd)
  end

  def accept_nonblock(exception: true)
    fd = nil
    begin
      fd, _ = Socket.__accept(__fd, true)
    rescue IO::WaitReadable => __exc
      raise __exc if exception
      return :wait_readable
    end
    TCPSocket.for_fd(fd)
  end

  def sysaccept
    Socket.__accept(__fd, false)[0]
  end

  def listen(backlog)
    Socket.__listen(__fd, Socket.__int(backlog))
  end
end

class UDPSocket < IPSocket
  def initialize(family = Socket::AF_INET)
    fam = Socket.__family(family)
    fd = Socket.__socket(fam, Socket::SOCK_DGRAM, 0)
    @family = fam
    __wrap_fd(fd)
  end

  def __udp_sockaddr(host, port)
    fam = @family || (__family rescue nil)
    infos = Socket.__getaddrinfo(host, port, fam || 0, Socket::SOCK_DGRAM, 0, host.nil? || host == "" ? Socket::AI_PASSIVE : 0)
    infos.first[3]
  end

  def bind(host, port)
    Socket.__bind(__fd, __udp_sockaddr(host, port))
  end

  def connect(host, port)
    Socket.__connect(__fd, __udp_sockaddr(host, port), false)
  end

  def send(mesg, flags, host = nil, port = nil)
    mesg = Socket.__str(mesg)
    if port
      Socket.__send(__fd, mesg, flags || 0, __udp_sockaddr(host, port))
    elsif host
      Socket.__send(__fd, mesg, flags || 0, Socket.__sockaddr(host))
    else
      Socket.__send(__fd, mesg, flags || 0, nil)
    end
  end

  def recvfrom_nonblock(maxlen, flags = 0, outbuf = nil, exception: true)
    begin
      res = Socket.__recv(__fd, maxlen, (flags || 0) | Socket::MSG_DONTWAIT, true)
    rescue IO::WaitReadable => __exc
      raise __exc if exception
      return :wait_readable
    end
    data = __recv_str(res, outbuf)
    [data, __addr_array(res[1], nil)]
  end
end

class UNIXSocket < BasicSocket
  def self.pair(type = Socket::SOCK_STREAM, protocol = 0, &block)
    fds = Socket.__socketpair(Socket::AF_UNIX, Socket.__socktype(type), protocol)
    Socket.__yield_pair(fds.map { |fd| UNIXSocket.for_fd(fd) }, &block)
  end

  def initialize(path, type = Socket::SOCK_STREAM)
    path = path.to_path if path.respond_to?(:to_path)
    raise TypeError, "no implicit conversion of #{path.class} into String" unless path.is_a?(String)
    fd = Socket.__socket(Socket::AF_UNIX, Socket.__socktype(type), 0)
    begin
      Socket.__connect(fd, Socket.__pack_un(path), false)
    rescue SystemCallError => __exc
      Socket.__close_fd(fd)
      raise __exc
    end
    @path = ""
    __wrap_fd(fd)
  end

  def path
    @path ||= (Socket.__unpack_un(getsockname) rescue "")
  end

  def addr
    ["AF_UNIX", Socket.__unpack_un(getsockname)]
  end

  def peeraddr
    ["AF_UNIX", Socket.__unpack_un(getpeername)]
  end

  def recvfrom(maxlen, flags = 0, outbuf = nil)
    res = Socket.__recv(__fd, maxlen, flags || 0, false)
    path = res[1] && Socket.__sa_family(res[1]) == Socket::AF_UNIX ? Socket.__unpack_un(res[1]) : ""
    path = "" if path.nil?
    data = __recv_str(res, outbuf)
    [data, ["AF_UNIX", path]]
  end

  def send_io(io)
    fd = io.is_a?(Integer) ? io : io.to_io.fileno
    Socket.__send_io(__fd, fd)
  end

  def recv_io(klass = IO, mode = nil)
    fd = Socket.__recv_io(__fd)
    return fd if klass.nil?
    mode ? klass.for_fd(fd, mode) : klass.for_fd(fd)
  end

  def inspect
    return "#<#{self.class}:(closed)>" if closed?
    pth = path
    return "#<#{self.class}:fd #{fileno}>" if pth.nil? || pth.empty?
    "#<#{self.class}:fd #{fileno}, AF_UNIX, #{pth}>"
  end
end

class UNIXServer < UNIXSocket
  def initialize(path)
    path = path.to_path if path.respond_to?(:to_path)
    raise TypeError, "no implicit conversion of #{path.class} into String" unless path.is_a?(String)
    fd = Socket.__socket(Socket::AF_UNIX, Socket::SOCK_STREAM, 0)
    begin
      Socket.__bind(fd, Socket.__pack_un(path))
      Socket.__listen(fd, Socket::SOMAXCONN)
    rescue SystemCallError => __exc
      Socket.__close_fd(fd)
      raise __exc
    end
    @path = path
    __wrap_fd(fd)
  end

  def accept
    fd, _ = Socket.__accept(__fd, false)
    UNIXSocket.for_fd(fd)
  end

  def accept_nonblock(exception: true)
    fd = nil
    begin
      fd, _ = Socket.__accept(__fd, true)
    rescue IO::WaitReadable => __exc
      raise __exc if exception
      return :wait_readable
    end
    UNIXSocket.for_fd(fd)
  end

  def sysaccept
    Socket.__accept(__fd, false)[0]
  end

  def listen(backlog)
    Socket.__listen(__fd, Socket.__int(backlog))
  end
end

class Socket < BasicSocket
  def initialize(domain, type, protocol = nil)
    domain = Socket.__str(domain) unless domain.is_a?(Integer) || domain.is_a?(Symbol) || domain.is_a?(String)
    type = Socket.__str(type) unless type.is_a?(Integer) || type.is_a?(Symbol) || type.is_a?(String)
    proto = protocol.nil? ? 0 : Socket.__int(protocol)
    fd = Socket.__socket(Socket.__family(domain), Socket.__socktype(type), proto)
    __wrap_fd(fd)
  end

  def self.__close_fd(fd)
    IO.for_fd(fd).close
  rescue StandardError
    nil
  end

  def self.__yield_pair(socks)
    return socks unless block_given?
    begin
      yield socks
    ensure
      socks.each do |s|
        begin
          s.close unless s.closed?
        rescue StandardError
          nil
        end
      end
    end
  end

  def bind(addr)
    Socket.__bind(__fd, Socket.__sockaddr(addr))
  end

  def connect(addr)
    Socket.__connect(__fd, Socket.__sockaddr(addr), false)
  end

  def connect_nonblock(addr, exception: true)
    begin
      Socket.__connect(__fd, Socket.__sockaddr(addr), true)
    rescue IO::WaitWritable => __exc
      raise __exc if exception
      return :wait_writable
    rescue Errno::EISCONN => __exc
      raise __exc if exception
      return 0
    end
  end

  def listen(backlog)
    Socket.__listen(__fd, Socket.__int(backlog))
  end

  def __accepted(fd, sa)
    [Socket.for_fd(fd), Addrinfo.new(sa, Socket.__sa_family(sa), __socktype, 0)]
  end

  def accept
    fd, sa = Socket.__accept(__fd, false)
    __accepted(fd, sa)
  end

  def accept_nonblock(exception: true)
    begin
      fd, sa = Socket.__accept(__fd, true)
    rescue IO::WaitReadable => __exc
      raise __exc if exception
      return :wait_readable
    end
    __accepted(fd, sa)
  end

  def sysaccept
    fd, sa = Socket.__accept(__fd, false)
    [fd, Addrinfo.new(sa, Socket.__sa_family(sa), __socktype, 0)]
  end

  def __recvfrom_addr(res)
    sa = res[1]
    if sa.nil? || sa.empty? || Socket.__sa_family(sa) == 0
      if __socktype == Socket::SOCK_STREAM
        sa = (getpeername rescue nil)
      end
      sa ||= (getsockname rescue nil)
    end
    sa ? Addrinfo.new(sa, Socket.__sa_family(sa), __socktype, 0) : nil
  end

  def recvfrom(maxlen, flags = 0, outbuf = nil)
    res = Socket.__recv(__fd, maxlen, flags || 0, false)
    return nil if res[2] == 0 && maxlen > 0 && __socktype == Socket::SOCK_STREAM
    [__recv_str(res, outbuf), __recvfrom_addr(res)]
  end

  def recvfrom_nonblock(maxlen, flags = 0, outbuf = nil, exception: true)
    begin
      res = Socket.__recv(__fd, maxlen, (flags || 0) | Socket::MSG_DONTWAIT, true)
    rescue IO::WaitReadable => __exc
      raise __exc if exception
      return :wait_readable
    end
    return nil if res[2] == 0 && maxlen > 0 && __socktype == Socket::SOCK_STREAM
    [__recv_str(res, outbuf), __recvfrom_addr(res)]
  end

  def ipv6only!
    setsockopt(Socket::IPPROTO_IPV6, Socket::IPV6_V6ONLY, true) if __family == AF_INET6
    nil
  end


  # -- class methods ------------------------------------------------------------

  def self.pair(domain, type, protocol = 0, &block)
    domain = __str(domain) unless domain.is_a?(Integer) || domain.is_a?(Symbol) || domain.is_a?(String)
    type = __str(type) unless type.is_a?(Integer) || type.is_a?(Symbol) || type.is_a?(String)
    fds = __socketpair(__family(domain), __socktype(type), protocol || 0)
    __yield_pair(fds.map { |fd| Socket.for_fd(fd) }, &block)
  end

  def self.gethostname
    __gethostname
  end

  def self.__reverse_flag(reverse)
    case reverse
    when nil then !BasicSocket.do_not_reverse_lookup
    when true, :hostname then true
    when false, :numeric then false
    else raise ArgumentError, "invalid reverse_lookup flag: #{reverse.inspect}"
    end
  end

  def self.__service(service)
    case service
    when nil, Integer then service
    when String then service
    else
      if service.respond_to?(:to_str) then service.to_str
      elsif service.respond_to?(:to_int) then service.to_int
      else raise TypeError, "no implicit conversion of #{service.class} into String"
      end
    end
  end

  def self.__host(host)
    case host
    when nil, String then host
    else
      if host.respond_to?(:to_str) then host.to_str
      else raise TypeError, "no implicit conversion of #{host.class} into String"
      end
    end
  end

  def self.getaddrinfo(host, service, family = nil, socktype = nil, protocol = nil, flags = nil, reverse_lookup = nil)
    host = __host(host)
    infos = __getaddrinfo(host, __service(service), family.nil? ? 0 : __family(family),
                          socktype.nil? ? 0 : __socktype(socktype), protocol || 0, flags || 0)
    rev = __reverse_flag(reverse_lookup)
    infos.map do |fam, type, proto, sa, _|
      port, ip = __unpack_in(sa)
      name = rev ? (__getnameinfo(sa, 0)[0] rescue ip) : ip
      [__family_name(fam), port, name, ip, fam, type, proto]
    end
  end

  def self.getnameinfo(addr, flags = 0)
    sa = case addr
         when Array
           raise ArgumentError, "array size should be 3 or 4, #{addr.size} given" unless addr.size == 3 || addr.size == 4
           host = addr.size == 4 ? (addr[3] || addr[2]) : addr[2]
           fam = addr[0].is_a?(String) ? __family(addr[0]) : addr[0]
           if fam && fam != 0 && fam != AF_INET && fam != AF_INET6
             raise Socket::ResolutionError.new("getnameinfo: ai_family not supported", Socket::EAI_FAMILY)
           end
           sa = sockaddr_in(addr[1], host)
           if fam && fam != 0 && __sa_family(sa) != fam
             raise SocketError, "getnameinfo: ai_family not supported" unless fam == AF_INET || fam == AF_INET6
             sa = __getaddrinfo(host, addr[1], fam, 0, 0, 0).first[3]
           end
           sa
         else __sockaddr(addr)
         end
    __getnameinfo(sa, flags || 0)
  end

  def self.sockaddr_in(port, host)
    __pack_in(__service(port), host.is_a?(Integer) ? host : __host(host))
  end


  def self.unpack_sockaddr_in(addr)
    if addr.is_a?(Addrinfo)
      raise ArgumentError, "not an AF_INET/AF_INET6 sockaddr" unless addr.ip?
      return [addr.ip_port, addr.ip_address]
    end
    __unpack_in(__sockaddr(addr))
  end

  def self.sockaddr_un(path)
    path = path.to_path if path.respond_to?(:to_path)
    __pack_un(path)
  end


  def self.unpack_sockaddr_un(addr)
    if addr.is_a?(Addrinfo)
      raise ArgumentError, "not an AF_UNIX sockaddr" unless addr.unix?
      return addr.unix_path
    end
    __unpack_un(__sockaddr(addr))
  end

  def self.getservbyname(name, proto = "tcp")
    __getservbyname(name, proto)
  end

  def self.getservbyport(port, proto = "tcp")
    raise RangeError, "integer #{port} too big to convert to 'unsigned short'" if port > 65535
    raise RangeError, "integer #{port} too small to convert to 'unsigned short'" if port < 0
    name = __getservbyport(port, proto)
    raise SocketError, "no such service for port #{port}/#{proto}" unless name
    name
  end

  def self.ip_address_list
    __ip_address_list.map { |ip| Addrinfo.ip(ip) }
  end

  def self.gethostbyname(host)
    infos = getaddrinfo(host, nil, nil, SOCK_STREAM)
    fam = infos[0][4]
    addrs = infos.select { |i| i[4] == fam }.map { |i| (fam == AF_INET ? __pack_in(0, i[3]).byteslice(4, 4) : __pack_in(0, i[3]).byteslice(8, 16)) }.uniq
    [host, [], fam, *addrs]
  end

  def self.gethostbyaddr(addr, family = nil)
    ip = if addr.bytesize == 4
           addr.bytes.join(".")
         elsif addr.bytesize == 16
           addr.unpack("n8").map { |x| x.to_s(16) }.join(":")
         else
           raise SocketError, "unknown address"
         end
    name = (__getnameinfo(__pack_in(0, ip), 0)[0] rescue ip)
    fam = addr.bytesize == 4 ? AF_INET : AF_INET6
    [name, [], fam, addr]
  end

  def self.tcp(host, port, local_host = nil, local_port = nil, connect_timeout: nil, resolv_timeout: nil, open_timeout: nil, fast_fallback: nil)
    sock = nil
    last = nil
    Addrinfo.getaddrinfo(host, port, nil, :STREAM).each do |ai|
      begin
        if local_host || local_port
          local = Addrinfo.getaddrinfo(local_host, local_port, ai.afamily, :STREAM).first
          sock = ai.connect_from(local, timeout: connect_timeout || open_timeout)
        else
          sock = ai.connect(timeout: connect_timeout || open_timeout)
        end
        break
      rescue SystemCallError, IO::TimeoutError => e
        last = e
      end
    end
    raise last if sock.nil? && last
    return sock unless block_given?
    begin
      yield sock
    ensure
      sock.close unless sock.closed?
    end
  end

  def self.unix(path)
    sock = Addrinfo.unix(path).connect
    return sock unless block_given?
    begin
      yield sock
    ensure
      sock.close unless sock.closed?
    end
  end

  def self.unix_server_socket(path)
    sock = Addrinfo.unix(path).listen
    return sock unless block_given?
    begin
      yield sock
    ensure
      sock.close unless sock.closed?
      File.unlink(path) rescue nil
    end
  end

  def self.unix_server_loop(path, &block)
    unix_server_socket(path) do |serv|
      accept_loop(serv, &block)
    end
  end

  def self.tcp_server_sockets(host = nil, port = nil)
    if port.nil?
      port = host
      host = nil
    end
    sockets = []
    infos = Addrinfo.getaddrinfo(host, port, nil, :STREAM, nil, AI_PASSIVE)
    infos = infos.select(&:ipv6?) + infos.select(&:ipv4?)
    infos.each do |ai|
      begin
        p = port
        if (p.nil? || p == 0 || p == "0") && !sockets.empty?
          p = sockets.first.local_address.ip_port
          ai = Addrinfo.tcp(ai.ip_address, p)
        end
        s = Socket.new(ai.pfamily, ai.socktype, ai.protocol)
        s.setsockopt(SOL_SOCKET, SO_REUSEADDR, true)
        s.ipv6only! if ai.ipv6?
        s.bind(ai)
        s.listen(SOMAXCONN)
        sockets << s
      rescue SystemCallError => __exc
        s&.close rescue nil
        raise __exc if infos.size == 1
      end
    end
    raise SocketError, "no addresses" if sockets.empty?
    return sockets unless block_given?
    begin
      yield sockets
    ensure
      sockets.each { |s| s.close unless s.closed? }
    end
  end

  def self.accept_loop(*sockets)
    sockets = sockets.flatten
    loop do
      readable, = IO.select(sockets)
      readable.each do |r|
        begin
          sock, addr = r.accept_nonblock
        rescue IO::WaitReadable
          next
        end
        yield sock, addr
      end
    end
  end

  def self.tcp_server_loop(host = nil, port = nil, &block)
    tcp_server_sockets(host, port) do |sockets|
      accept_loop(sockets, &block)
    end
  end

  def self.udp_server_sockets(host = nil, port = nil)
    if port.nil?
      port = host
      host = nil
    end
    sockets = []
    infos = Addrinfo.getaddrinfo(host, port, nil, :DGRAM, nil, AI_PASSIVE)
    infos = infos.select(&:ipv6?) + infos.select(&:ipv4?)
    infos.each do |ai|
      begin
        if (port.nil? || port == 0 || port == "0") && !sockets.empty?
          ai = Addrinfo.udp(ai.ip_address, sockets.first.local_address.ip_port)
        end
        s = Socket.new(ai.pfamily, ai.socktype, ai.protocol)
        s.ipv6only! if ai.ipv6?
        s.bind(ai)
        sockets << s
      rescue SystemCallError => __exc
        s&.close rescue nil
        raise __exc if infos.size == 1
      end
    end
    return sockets unless block_given?
    begin
      yield sockets
    ensure
      sockets.each { |s| s.close unless s.closed? }
    end
  end

  class UDPSource
    attr_reader :remote_address, :local_address

    def initialize(remote_address, local_address, &reply_proc)
      @remote_address = remote_address
      @local_address = local_address
      @reply_proc = reply_proc
    end

    def reply(msg)
      @reply_proc.call(msg)
    end

    def inspect
      "\#<#{self.class}: #{@remote_address.inspect_sockaddr} to #{@local_address.inspect_sockaddr}>"
    end
  end

  def self.udp_server_recv(sockets)
    sockets.each do |r|
      begin
        msg, ai = r.recvfrom_nonblock(65536)
      rescue IO::WaitReadable
        next
      end
      src = UDPSource.new(ai, r.local_address) { |reply| r.send(reply, 0, ai) }
      yield msg, src
    end
  end

  def self.udp_server_loop_on(sockets, &block)
    loop do
      readable, = IO.select(sockets)
      udp_server_recv(readable, &block)
    end
  end

  def self.udp_server_loop(host = nil, port = nil, &block)
    udp_server_sockets(host, port) do |sockets|
      udp_server_loop_on(sockets, &block)
    end
  end

  class Option
    attr_reader :family, :level, :optname, :data

    def initialize(family, level, optname, data)
      @family = Socket.__family(family)
      @level = Socket.__level(level)
      @optname = Socket.__optname(@level, optname)
      raise TypeError, "no implicit conversion of #{data.class} into String" unless data.is_a?(String)
      @data = data
    end

    def self.int(family, level, optname, value)
      new(family, level, optname, [value].pack("i"))
    end

    def self.bool(family, level, optname, value)
      new(family, level, optname, [value ? 1 : 0].pack("i"))
    end

    def self.linger(onoff, secs)
      onoff = case onoff
              when true then 1
              when false, nil then 0
              when Integer then onoff
              else raise TypeError, "no implicit conversion of #{onoff.class} into Integer"
              end
      new(AF_UNSPEC, SOL_SOCKET, SO_LINGER, [onoff, secs].pack("ii"))
    end

    def self.ipv4_multicast_loop(v)
      new(AF_INET, IPPROTO_IP, IP_MULTICAST_LOOP, [v].pack("C"))
    end

    def self.ipv4_multicast_ttl(v)
      new(AF_INET, IPPROTO_IP, IP_MULTICAST_TTL, [v].pack("C"))
    end

    def int
      unless @data.bytesize == 4
        raise TypeError, "size differ.  expected as sizeof(int)=4 but #{@data.bytesize}"
      end
      @data.unpack1("i")
    end

    def bool
      case @data.bytesize
      when 1 then @data.unpack1("C") != 0
      when 4 then @data.unpack1("i") != 0
      else raise TypeError, "size differ.  expected as sizeof(int)=4 but #{@data.bytesize}"
      end
    end

    def linger
      unless @level == SOL_SOCKET && @optname == SO_LINGER
        raise TypeError, "linger socket option expected"
      end
      unless @data.bytesize == 8
        raise TypeError, "size differ.  expected as sizeof(struct linger)=8 but #{@data.bytesize}"
      end
      onoff, secs = @data.unpack("ii")
      [onoff == 0 ? false : (onoff == 1 ? true : onoff), secs]
    end

    def ipv4_multicast_loop
      @data.bytesize == 1 ? @data.unpack1("C") : int
    end

    def ipv4_multicast_ttl
      @data.bytesize == 1 ? @data.unpack1("C") : int
    end

    def unpack(template)
      @data.unpack(template)
    end

    def to_s
      @data
    end

    def inspect
      fam = Socket.__const_name(@family, "AF_") || @family.to_s
      if @level == SOL_SOCKET
        lev = "SOCKET"
        opt = Socket.__const_name(@optname, "SO_") || @optname.to_s
      else
        lev = Socket.__const_name(@level, "IPPROTO_") || "level:#{@level}"
        pre = { IPPROTO_IP => "IP_", IPPROTO_IPV6 => "IPV6_", IPPROTO_TCP => "TCP_" }[@level]
        opt = (pre && Socket.__const_name(@optname, pre)) || "optname:#{@optname}"
      end
      val = if @level == SOL_SOCKET && @optname == SO_LINGER && @data.bytesize == 8
              onoff, secs = @data.unpack("ii")
              "#{onoff == 0 ? 'off' : 'on'} #{secs}sec"
            elsif @data.bytesize == 4
              @data.unpack1("i").to_s
            else
              @data.inspect
            end
      "#<Socket::Option: #{fam} #{lev} #{opt} #{val}>"
    end
  end
end

class Addrinfo
  attr_reader :afamily, :pfamily, :socktype, :protocol, :canonname

  def initialize(sockaddr, family = nil, socktype = nil, protocol = nil)
    if sockaddr.is_a?(Array)
      fam = Socket.__family(sockaddr[0])
      if fam == Socket::AF_UNIX
        sa = Socket.__pack_un(sockaddr[1])
      else
        sa = Socket.__pack_in(sockaddr[1], sockaddr[3] || sockaddr[2])
        if Socket.__sa_family(sa) != fam
          raise SocketError, "getaddrinfo: Address family for hostname not supported"
        end
      end
    else
      sa = Socket.__sockaddr(sockaddr)
    end
    @sockaddr = sa
    @afamily = Socket.__sa_family(sa) || Socket::AF_UNSPEC
    @pfamily = family.nil? ? (sockaddr.is_a?(Array) ? @afamily : Socket::PF_UNSPEC) : Socket.__family(family)
    @socktype = socktype.nil? ? 0 : Socket.__socktype(socktype)
    @protocol = protocol.nil? ? 0 : Socket.__const_lookup(protocol, %w[IPPROTO_], "protocol")
    @canonname = nil
    __validate_hints if sockaddr.is_a?(Array) && ip?
  end

  def __validate_hints
    err = "getaddrinfo: ai_family not supported"
    unless @pfamily == Socket::PF_INET || @pfamily == Socket::PF_INET6
      raise SocketError, err
    end
    ok_proto = case @socktype
               when 0, Socket::SOCK_DGRAM then [0, Socket::IPPROTO_UDP]
               when Socket::SOCK_STREAM then [0, Socket::IPPROTO_TCP]
               when Socket::SOCK_RAW then nil
               when Socket::SOCK_SEQPACKET then [0]
               else raise SocketError, "getaddrinfo: ai_socktype not supported"
               end
    if ok_proto && !ok_proto.include?(@protocol)
      raise SocketError, "getaddrinfo: ai_socktype not supported"
    end
  end

  def __canonname=(name)
    @canonname = name
  end

  def self.__new(sa, fam, type, proto, canon = nil)
    ai = new(sa, fam, type, proto)
    ai.__canonname = canon
    ai
  end

  def self.getaddrinfo(node, service, family = nil, socktype = nil, protocol = nil, flags = nil, timeout: nil)
    node = Socket.__host(node)
    infos = Socket.__getaddrinfo(node, Socket.__service(service), family.nil? ? 0 : Socket.__family(family),
                                 socktype.nil? ? 0 : Socket.__socktype(socktype),
                                 protocol.nil? ? 0 : Socket.__const_lookup(protocol, %w[IPPROTO_], "protocol"),
                                 flags || 0)
    infos.map { |fam, type, proto, sa, canon| __new(sa, fam, type, proto, canon) }
  end

  def self.foreach(*args, **kw, &block)
    list = getaddrinfo(*args, **kw)
    return list.each unless block
    list.each(&block)
  end

  def self.tcp(host, port)
    getaddrinfo(host, port, nil, Socket::SOCK_STREAM, Socket::IPPROTO_TCP).first
  end

  def self.udp(host, port)
    getaddrinfo(host, port, nil, Socket::SOCK_DGRAM, Socket::IPPROTO_UDP).first
  end

  def self.ip(host)
    ai = getaddrinfo(host, nil, nil, :STREAM).first
    __new(ai.to_sockaddr, ai.pfamily, 0, 0)
  end

  def self.unix(path, socktype = Socket::SOCK_STREAM)
    path = path.to_path if path.respond_to?(:to_path)
    new(Socket.__pack_un(path), Socket::AF_UNIX, socktype, 0)
  end

  def to_sockaddr
    @sockaddr
  end
  alias_method :to_s, :to_sockaddr

  def ip?
    @afamily == Socket::AF_INET || @afamily == Socket::AF_INET6
  end

  def ipv4?
    @afamily == Socket::AF_INET
  end

  def ipv6?
    @afamily == Socket::AF_INET6
  end

  def unix?
    @afamily == Socket::AF_UNIX
  end

  def ip_unpack
    raise SocketError, "need IPv4 or IPv6 address" unless ip?
    port, ip = Socket.__unpack_in(@sockaddr)
    [ip, port]
  end

  def ip_address
    ip_unpack[0]
  end

  def ip_port
    ip_unpack[1]
  end

  def unix_path
    raise SocketError, "need AF_UNIX address" unless unix?
    Socket.__unpack_un(@sockaddr)
  end

  def __ip_bytes
    if ipv4?
      @sockaddr.byteslice(4, 4).bytes
    else
      @sockaddr.byteslice(8, 16).bytes
    end
  end

  def __ip4_int
    b = __ip_bytes
    (b[0] << 24) | (b[1] << 16) | (b[2] << 8) | b[3]
  end

  def ipv4_loopback?
    ipv4? && __ip_bytes[0] == 127
  end

  def ipv4_private?
    return false unless ipv4?
    b = __ip_bytes
    b[0] == 10 || (b[0] == 172 && (b[1] & 0xf0) == 16) || (b[0] == 192 && b[1] == 168)
  end

  def ipv4_multicast?
    ipv4? && (__ip_bytes[0] & 0xf0) == 0xe0
  end

  def ipv6_loopback?
    ipv6? && __ip_bytes == ([0] * 15 + [1])
  end

  def ipv6_unspecified?
    ipv6? && __ip_bytes == [0] * 16
  end

  def ipv6_multicast?
    ipv6? && __ip_bytes[0] == 0xff
  end

  def ipv6_linklocal?
    b = __ip_bytes
    ipv6? && b[0] == 0xfe && (b[1] & 0xc0) == 0x80
  end

  def ipv6_sitelocal?
    b = __ip_bytes
    ipv6? && b[0] == 0xfe && (b[1] & 0xc0) == 0xc0
  end

  def ipv6_unique_local?
    ipv6? && (__ip_bytes[0] & 0xfe) == 0xfc
  end

  def ipv6_v4mapped?
    b = __ip_bytes
    ipv6? && b[0, 10] == [0] * 10 && b[10] == 0xff && b[11] == 0xff
  end

  def ipv6_v4compat?
    return false unless ipv6?
    b = __ip_bytes
    return false unless b[0, 12] == [0] * 12
    tail = b[12, 4]
    tail != [0, 0, 0, 0] && tail != [0, 0, 0, 1]
  end

  def __mc_scope(scope)
    ipv6_multicast? && (__ip_bytes[1] & 0x0f) == scope
  end

  def ipv6_mc_nodelocal?;  __mc_scope(1); end
  def ipv6_mc_linklocal?;  __mc_scope(2); end
  def ipv6_mc_sitelocal?;  __mc_scope(5); end
  def ipv6_mc_orglocal?;   __mc_scope(8); end
  def ipv6_mc_global?;     __mc_scope(0xe); end

  def ipv6_to_ipv4
    return nil unless ipv6?
    return nil unless ipv6_v4mapped? || ipv6_v4compat?
    b = __ip_bytes[12, 4]
    Addrinfo.new(Socket.__pack_in(ip_port, b.join(".")), Socket::AF_INET, @socktype, @protocol)
  end

  def inspect_sockaddr
    if ipv4?
      ip, port = ip_unpack
      port == 0 ? ip : "#{ip}:#{port}"
    elsif ipv6?
      ip, port = ip_unpack
      port == 0 ? ip : "[#{ip}]:#{port}"
    elsif unix?
      path = unix_path
      if path.start_with?("/")
        path
      elsif path.empty?
        "UNIX"
      else
        "UNIX #{path}"
      end
    else
      "unknown address family #{@afamily}"
    end
  end

  def inspect
    s = "#<Addrinfo: #{inspect_sockaddr}"
    s << " #{Socket.__const_name(@pfamily, 'PF_') || @pfamily}" if @pfamily != 0 && @pfamily != @afamily
    internet = @pfamily == Socket::PF_INET || @pfamily == Socket::PF_INET6
    if internet && @socktype == Socket::SOCK_STREAM && (@protocol == 0 || @protocol == Socket::IPPROTO_TCP)
      s << " TCP"
    elsif internet && @socktype == Socket::SOCK_DGRAM && (@protocol == 0 || @protocol == Socket::IPPROTO_UDP)
      s << " UDP"
    else
      if @socktype != 0
        n = Socket.__const_name(@socktype, "SOCK_")
        s << (n ? " SOCK_#{n}" : " socktype:#{@socktype}")
      end
      if @protocol != 0
        n = Socket.__const_name(@protocol, "IPPROTO_")
        s << (n ? " #{n}" : " UNKNOWN_PROTOCOL(#{@protocol})")
      end
    end
    s << " (#{@canonname})" if @canonname
    s << ">"
  end

  def getnameinfo(flags = 0)
    raise SocketError, "getnameinfo: ai_family not supported" unless ip?
    Socket.__getnameinfo(@sockaddr, flags)
  end

  def family_addrinfo(*args)
    raise ArgumentError, "no address specified" if args.empty?
    if args[0].is_a?(Addrinfo)
      raise ArgumentError, "too many arguments" if args.size > 1
      ai = args[0]
      raise ArgumentError, "Addrinfo type mismatch" if ai.afamily != @afamily || ai.socktype != @socktype
      return ai
    end
    if unix?
      raise ArgumentError, "too many arguments" unless args.size == 1
      Addrinfo.unix(args[0], @socktype)
    elsif ip?
      raise ArgumentError, "IP address needs host and port but #{args.size} arguments given" unless args.size == 2
      Addrinfo.getaddrinfo(args[0], args[1], @pfamily == 0 ? @afamily : @pfamily, @socktype, @protocol)[0]
    else
      raise "unexpected family"
    end
  end

  def __socket
    Socket.new(@pfamily == 0 ? @afamily : @pfamily, @socktype, @protocol)
  end

  def bind
    sock = __socket
    sock.setsockopt(Socket::SOL_SOCKET, Socket::SO_REUSEADDR, true) if ip?
    sock.bind(self)
    return sock unless block_given?
    begin
      yield sock
    ensure
      sock.close unless sock.closed?
    end
  end

  def listen(backlog = Socket::SOMAXCONN)
    sock = __socket
    sock.setsockopt(Socket::SOL_SOCKET, Socket::SO_REUSEADDR, true) if ip?
    sock.bind(self)
    sock.listen(backlog)
    return sock unless block_given?
    begin
      yield sock
    ensure
      sock.close unless sock.closed?
    end
  end

  def __connect_internal(local, timeout)
    sock = __socket
    begin
      sock.bind(local) if local
      if timeout
        case sock.connect_nonblock(self, exception: false)
        when :wait_writable
          unless sock.wait_writable(timeout)
            raise IO::TimeoutError, "Connect timed out!"
          end
          err = sock.getsockopt(Socket::SOL_SOCKET, Socket::SO_ERROR).int
          raise SystemCallError.new("connect(2) for #{inspect_sockaddr}", err) if err != 0
        end
      else
        sock.connect(self)
      end
    rescue Exception => __exc
      sock.close unless sock.closed?
      raise __exc
    end
    sock
  end

  def connect(timeout: nil, &block)
    sock = __connect_internal(nil, timeout)
    return sock unless block
    begin
      yield sock
    ensure
      sock.close unless sock.closed?
    end
  end

  def connect_from(*args, timeout: nil, &block)
    local = args.size == 1 && args[0].is_a?(Addrinfo) ? args[0] : family_addrinfo(*args)
    sock = __connect_internal(local, timeout)
    return sock unless block
    begin
      yield sock
    ensure
      sock.close unless sock.closed?
    end
  end

  def connect_to(*args, timeout: nil, &block)
    remote = args.size == 1 && args[0].is_a?(Addrinfo) ? args[0] : family_addrinfo(*args)
    remote.connect_from(self, timeout: timeout, &block)
  end

  def marshal_dump
    afam = Socket.__family_name(@afamily)
    pfam = Socket.__const_name(@pfamily, "PF_")
    pfam = pfam ? "PF_#{pfam}" : @pfamily.to_s
    stype = @socktype == 0 ? 0 : "SOCK_#{Socket.__const_name(@socktype, 'SOCK_')}"
    proto = @protocol == 0 ? 0 : (n = Socket.__const_name(@protocol, "IPPROTO_")) ? "IPPROTO_#{n}" : @protocol
    if unix?
      [afam, unix_path, pfam, stype, proto]
    else
      host = getnameinfo(Socket::NI_NUMERICHOST | Socket::NI_NUMERICSERV)
      res = [afam, [host[0], host[1]], pfam, stype, proto]
      res << @canonname if @canonname
      res
    end
  end

  def marshal_load(ary)
    afam, addr, pfam, stype, proto, canon = ary
    fam = Socket.__family(afam)
    sa = fam == Socket::AF_UNIX ? Socket.__pack_un(addr) : Socket.__pack_in(addr[1].to_i, addr[0])
    initialize(sa, pfam, stype, proto)
    @canonname = canon
    self
  end
end

class BasicSocket
  def self.__family_of(sa)
    Socket.__sa_family(sa)
  end
end

[TCPSocket, UDPSocket, UNIXSocket, TCPServer, UNIXServer, Socket].each do |cls|
  cls.define_singleton_method(:new) do |*args, **kw, &block|
    warn "warning: #{self}::new() does not take block; use #{self}::open() instead", uplevel: 1 if block
    obj = allocate
    kw.empty? ? obj.__send__(:initialize, *args) : obj.__send__(:initialize, *args, **kw)
    obj
  end
  cls.define_singleton_method(:open) do |*args, **kw, &block|
    sock = kw.empty? ? new(*args) : new(*args, **kw)
    return sock unless block
    begin
      block.call(sock)
    ensure
      sock.close unless sock.closed?
    end
  end
end

class << Socket
  alias_method :pack_sockaddr_in, :sockaddr_in
  alias_method :pack_sockaddr_un, :sockaddr_un
  alias_method :socketpair, :pair
end

class << UNIXSocket
  alias_method :socketpair, :pair
end
