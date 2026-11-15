module URI
  VERSION = "1.0.3"

  class Error < StandardError; end
  class InvalidURIError < Error; end
  class InvalidComponentError < Error; end
  class BadURIError < Error; end

  @schemes = {}

  def self.scheme_list = @schemes

  def self.register_scheme(scheme, klass)
    @schemes[scheme.to_s.upcase] = klass
  end

  def self.for(scheme, *args, default: Generic)
    klass = scheme ? (@schemes[scheme.to_s.upcase] || default) : default
    klass.new(scheme, *args)
  end

  def self.split(uri)
    uri = uri.to_str
    ary = __split(uri)
    raise InvalidURIError, "bad URI (is not URI?): #{uri.inspect}" unless ary
    ary
  end

  def self.parse(uri)
    uri = uri.to_str rescue raise(InvalidURIError, "bad URI (is not URI?): #{uri.inspect}")
    scheme, userinfo, host, port, registry, path, opaque, query, fragment = split(uri)
    klass = scheme ? (@schemes[scheme.upcase] || Generic) : Generic
    klass.new(scheme, userinfo, host, port, registry, path, opaque, query, fragment, DEFAULT_PARSER, true)
  end

  def self.join(*str)
    raise ArgumentError, "wrong number of arguments (given 0, expected 1+)" if str.empty?
    str.map { |s| s.is_a?(Generic) ? s : parse(s) }.inject(:merge)
  end

  # -- encoding ------------------------------------------------------------

  def self.encode_www_form_component(str, enc = nil)
    __escape(str.to_s, "www")
  end

  def self.decode_www_form_component(str, enc = nil)
    str = str.to_s
    raise ArgumentError, "invalid %-encoding (#{str})" if str.gsub(/%[0-9A-Fa-f]{2}/, "").include?("%")
    __unescape(str, true)
  end

  def self.encode_uri_component(str, enc = nil)
    __escape(str.to_s, "www").gsub("+", "%20")
  end

  def self.decode_uri_component(str, enc = nil)
    str = str.to_s
    raise ArgumentError, "invalid %-encoding (#{str})" if str.gsub(/%[0-9A-Fa-f]{2}/, "").include?("%")
    __unescape(str, false)
  end

  def self.encode_www_form(enum, enc = nil)
    enum.map do |k, v|
      if v.nil?
        encode_www_form_component(k)
      elsif v.respond_to?(:to_ary)
        v.to_ary.map do |w|
          w.nil? ? encode_www_form_component(k) : "#{encode_www_form_component(k)}=#{encode_www_form_component(w)}"
        end.join("&")
      else
        "#{encode_www_form_component(k)}=#{encode_www_form_component(v)}"
      end
    end.join("&")
  end

  def self.decode_www_form(str, enc = nil, separator: "&", use__charset_: false, isindex: false)
    raise ArgumentError, "the input of #{self.name}.#{__method__} must be ASCII only string" unless str.ascii_only?
    ary = []
    return ary if str.empty?
    str.split(separator).each do |kv|
      next if kv.empty?
      k, v = kv.split("=", 2)
      ary << [decode_www_form_component(k), decode_www_form_component(v || "")]
    end
    ary
  end

  module Escape
    UNSAFE = "-_.!~*'()a-zA-Z0-9;/?:@&=+$,[]"

    def escape(str, unsafe = nil)
      URI.__escape(str.to_s, "-_.!~*'();/?:@&=+$,[]")
    end
    alias encode escape

    def unescape(str)
      URI.__unescape(str.to_s, false)
    end
    alias decode unescape
  end

  # -- parsers -------------------------------------------------------------

  class RFC3986_Parser
    def split(uri) = URI.split(uri)
    def parse(uri) = URI.parse(uri)
    def join(*uris) = URI.join(*uris)

    def escape(str, unsafe = nil)
      URI.__escape(str.to_s, "-_.!~*'();/?:@&=+$,[]")
    end

    def unescape(str, escaped = nil)
      URI.__unescape(str.to_s, false)
    end

    def extract(str, schemes = nil)
      ary = str.scan(make_regexp(schemes)).map { |m| m.is_a?(Array) ? m.first : m }
      return ary unless block_given?
      ary.each { |u| yield u }
      nil
    end

    def make_regexp(schemes = nil)
      if schemes
        /(?:#{schemes.map { |s| Regexp.quote(s) }.join('|')}):[^\s<>"]+/
      else
        /[a-zA-Z][a-zA-Z0-9+\-.]*:[^\s<>"]+/
      end
    end

    def inspect = "#<#{self.class}>"
  end
  RFC2396_Parser = RFC3986_Parser
  Parser = RFC3986_Parser
  RFC3986_PARSER = RFC3986_Parser.new
  RFC2396_PARSER = RFC3986_PARSER
  DEFAULT_PARSER = RFC3986_PARSER

  def self.parser = DEFAULT_PARSER

  def self.extract(str, schemes = nil, &block) = DEFAULT_PARSER.extract(str, schemes, &block)
  def self.regexp(schemes = nil) = DEFAULT_PARSER.make_regexp(schemes)

  # -- Generic -------------------------------------------------------------

  class Generic
    include URI

    DEFAULT_PORT = nil
    COMPONENT = [:scheme, :userinfo, :host, :port, :registry, :path, :opaque, :query, :fragment].freeze
    USE_REGISTRY = false

    def self.default_port = self::DEFAULT_PORT
    def self.component = self::COMPONENT
    def self.use_registry = self::USE_REGISTRY

    def self.build2(args)
      build(args)
    rescue InvalidComponentError
      if args.is_a?(Array)
        build(args.collect { |x| x.is_a?(String) ? URI::DEFAULT_PARSER.escape(x) : x })
      elsif args.is_a?(Hash)
        build(args.to_a.map { |k, v| [k, v.is_a?(String) ? URI::DEFAULT_PARSER.escape(v) : v] }.to_h)
      end
    end

    def self.build(args)
      if args.is_a?(Array) && args.size == ::URI::Generic::COMPONENT.size
        tmp = args.dup
      elsif args.is_a?(Hash)
        tmp = ::URI::Generic::COMPONENT.map { |c| args.key?(c) ? args[c] : nil }
      else
        component = self::COMPONENT rescue ::URI::Generic::COMPONENT
        raise ArgumentError, "expected Array of or Hash of components of #{self} (#{component.join(', ')})"
      end
      tmp << nil << true
      new(*tmp)
    end

    attr_reader :scheme, :host, :port, :registry, :path, :query, :opaque, :fragment, :parser

    def initialize(scheme, userinfo, host, port, registry, path, opaque, query, fragment,
                   parser = DEFAULT_PARSER, arg_check = false)
      @scheme = nil
      @user = nil
      @password = nil
      @host = nil
      @port = nil
      @path = nil
      @query = nil
      @opaque = nil
      @fragment = nil
      @parser = parser == DEFAULT_PARSER ? nil : parser
      if arg_check
        self.scheme = scheme
        self.userinfo = userinfo
        self.hostname = host
        self.port = port
        self.path = path
        self.query = query
        self.opaque = opaque
        self.fragment = fragment
      else
        set_scheme(scheme)
        set_userinfo(userinfo)
        set_host(host)
        set_port(port)
        set_path(path)
        self.query = query
        set_opaque(opaque)
        self.fragment = fragment
      end
      raise InvalidURIError, "the scheme #{@scheme} does not accept registry part: #{registry} (or bad hostname?)" if registry
      @scheme&.freeze
      self.set_path("") if !@path && !@opaque
      if @host.nil? && !@path.nil? && @scheme && @path.start_with?("//")
        # nothing
      end
      if @port.nil? && @host && self.class.default_port
        @port = self.class.default_port
      end
    end

    def parser
      @parser || DEFAULT_PARSER
    end

    def component = self.class.component
    def default_port = self.class.default_port

    def __check(kind, v)
      return true if v.nil?
      unless URI.__valid(kind, v)
        raise InvalidComponentError, "bad component(expected #{kind} component): #{v}"
      end
      true
    end
    private :__check

    # scheme
    def scheme=(v)
      check_scheme(v)
      set_scheme(v)
      v
    end

    def check_scheme(v)
      __check("scheme", v)
    end
    protected :check_scheme

    def set_scheme(v)
      @scheme = v&.downcase
    end
    protected :set_scheme

    # userinfo
    def user
      @user
    end

    def password
      @password
    end

    def userinfo
      if @user.nil?
        nil
      elsif @password.nil?
        @user
      else
        "#{@user}:#{@password}"
      end
    end

    def userinfo=(userinfo)
      return @user = @password = nil if userinfo.nil?
      check_userinfo(*userinfo)
      set_userinfo(*userinfo)
      userinfo
    end

    def user=(user)
      check_user(user)
      set_user(user)
      user
    end

    def password=(password)
      check_password(password)
      set_password(password)
      password
    end

    def check_userinfo(user, password = nil)
      if !password
        user, password = split_userinfo(user)
      end
      check_user(user)
      check_password(password, user)
      true
    end
    private :check_userinfo

    def check_user(v)
      if @opaque
        raise InvalidURIError, "cannot set user with opaque"
      end
      __check("user", v)
    end
    private :check_user

    def check_password(v, user = @user)
      if @opaque
        raise InvalidURIError, "cannot set password with opaque"
      end
      return v unless v
      raise InvalidURIError, "password component depends user component" unless user
      __check("password", v)
    end
    private :check_password

    def split_userinfo(ui)
      return nil, nil unless ui
      user, password = ui.split(":", 2)
      [user, password]
    end
    private :split_userinfo

    def set_userinfo(user, password = nil)
      unless password
        user, password = split_userinfo(user)
      end
      @user = user
      @password = password if password
      [@user, @password]
    end
    protected :set_userinfo

    def set_user(v)
      set_userinfo(v, @password)
      v
    end
    protected :set_user

    def set_password(v)
      @password = v
    end
    protected :set_password

    # host
    def host=(v)
      check_host(v)
      set_host(v)
      v
    end

    def check_host(v)
      return v unless v
      if @opaque
        raise InvalidURIError, "cannot set host with registry or opaque"
      end
      __check("host", v)
    end
    private :check_host

    def set_host(v)
      @host = v
    end
    protected :set_host

    def hostname
      v = host
      v&.start_with?("[") && v.end_with?("]") ? v[1..-2] : v
    end

    def hostname=(v)
      v = "[#{v}]" if v && !(v.start_with?("[") && v.end_with?("]")) && v.index(":")
      self.host = v
    end

    def authority
      if @port && @port != self.class.default_port
        "#{@host}:#{@port}"
      else
        @host
      end
    end

    # port
    def port=(v)
      check_port(v)
      set_port(v)
      v
    end

    def check_port(v)
      return v unless v
      if @opaque
        raise InvalidURIError, "cannot set port with registry or opaque"
      end
      if !v.is_a?(Integer) && !(v.is_a?(String) && v =~ /\A\d*\z/)
        raise InvalidComponentError, "bad component(expected port component): #{v.inspect}"
      end
      true
    end
    private :check_port

    def set_port(v)
      v = v.empty? ? nil : v.to_i if v.is_a?(String)
      @port = v
    end
    protected :set_port

    def registry=(v)
      raise InvalidURIError, "cannot set registry"
    end

    def set_registry(v)
      raise InvalidURIError, "cannot set registry"
    end
    protected :set_registry

    # path
    def path=(v)
      check_path(v)
      set_path(v)
      v
    end

    def check_path(v)
      if v && @opaque
        raise InvalidURIError, "path conflicts with opaque"
      end
      if @scheme && v && v != "" && !v.start_with?("/")
        raise InvalidComponentError, "bad component(expected absolute path component): #{v}"
      end
      __check("path", v)
    end
    private :check_path

    def set_path(v)
      @path = v
    end
    protected :set_path

    def query=(v)
      return @query = nil unless v
      raise InvalidURIError, "query conflicts with opaque" if @opaque
      x = v.to_str
      v = x.dup if x.equal?(v)
      v = v.b.gsub(/[^!$&'()*+,\/:;=?@\[\]\-._~a-zA-Z0-9%]/) { |c| "%%%02X" % c.ord }
      @query = v
    end

    def opaque=(v)
      check_opaque(v)
      set_opaque(v)
      v
    end

    def check_opaque(v)
      return v unless v
      if @host || @port || @user || @path
        raise InvalidURIError, "cannot set opaque with host, port, userinfo or path"
      end
      __check("opaque", v)
    end
    private :check_opaque

    def set_opaque(v)
      @opaque = v
    end
    protected :set_opaque

    def fragment=(v)
      return @fragment = nil unless v
      x = v.to_str
      v = x.dup if x.equal?(v)
      v = v.b.gsub(/[^!$&'()*+,\/:;=?@\[\]\-._~a-zA-Z0-9%]/) { |c| "%%%02X" % c.ord }
      @fragment = v
    end

    def set_fragment(v)
      @fragment = v
    end
    protected :set_fragment

    def hierarchical?
      !@path.nil?
    end

    def absolute?
      !@scheme.nil?
    end
    alias absolute absolute?

    def relative?
      @scheme.nil?
    end

    # -- merging -------------------------------------------------------------

    def merge!(oth)
      t = merge(oth)
      if self == t
        nil
      else
        replace!(t)
        self
      end
    end

    def merge(oth)
      oth = parser.parse(oth) unless oth.is_a?(Generic)
      raise BadURIError, "both URI are relative" if relative? && oth.relative?
      raise BadURIError, "relative URI: #{self}" if relative?
      return oth.dup if oth.absolute?
      base = to_s
      res = URI.__merge(base, oth.to_s)
      raise BadURIError, "cannot merge #{oth} into #{self}" unless res
      parser.parse(res)
    end
    alias + merge

    def route_from(oth)
      oth = parser.parse(oth) unless oth.is_a?(Generic)
      raise BadURIError, "relative URI: #{self}" if relative?
      raise BadURIError, "relative URI: #{oth}" if oth.relative?
      return dup if scheme != oth.scheme || userinfo != oth.userinfo || host&.downcase != oth.host&.downcase || port != oth.port
      rel = Generic.new(nil, nil, nil, nil, nil, "", nil, query, fragment)
      if path == oth.path
        rel.set_path("")
        rel.query = nil if query == oth.query
        return rel
      end
      rel.set_path(route_from_path(oth.path, path))
      rel
    end

    def route_to(oth)
      oth = parser.parse(oth) unless oth.is_a?(Generic)
      oth.route_from(self)
    end
    alias - route_from

    def route_from_path(src, dst)
      return "" if src == dst
      src_path = src.split("/", -1)
      dst_path = dst.split("/", -1)
      src_path.pop
      dst_last = dst_path.pop
      while !src_path.empty? && !dst_path.empty? && src_path.first == dst_path.first
        src_path.shift
        dst_path.shift
      end
      tmp = dst_path.join("/")
      if src_path.empty?
        return "./" if dst_last.empty? && tmp.empty?
        return "./" + dst_last if tmp.empty? && dst_last.include?(":")
        return (tmp.empty? ? "" : tmp + "/") + dst_last
      end
      ("../" * src_path.size) + (tmp.empty? ? "" : tmp + "/") + dst_last
    end
    private :route_from_path

    def normalize
      dup.normalize!
    end

    def normalize!
      set_path("/") if path&.empty? && host
      set_scheme(scheme.downcase) if scheme && scheme != scheme.downcase
      set_host(host.downcase) if host && host != host.downcase
      self
    end

    def replace!(oth)
      raise ArgumentError, "expected #{self.class} object" unless self.class == oth.class
      component.each { |c| instance_variable_set("@#{c}", oth.__send__(c)) if respond_to?(c) }
      @user = oth.user
      @password = oth.password
    end
    private :replace!

    # -- output --------------------------------------------------------------

    def to_s
      str = +""
      if @scheme
        str << @scheme << ":"
      end
      if @opaque
        str << @opaque
      else
        if @host || %w[file postgres].include?(@scheme)
          str << "//"
        end
        if (ui = userinfo)
          str << ui << "@"
        end
        str << @host if @host
        if @port && @port != self.class.default_port
          str << ":" << @port.to_s
        end
        if @host && @path && !@path.empty? && !@path.start_with?("/")
          str << "/"
        end
        str << @path if @path
        if @query
          str << "?" << @query
        end
      end
      if @fragment
        str << "#" << @fragment
      end
      str
    end
    alias to_str to_s

    def ==(oth)
      if self.class == oth.class
        normalize.component_ary == oth.normalize.component_ary
      else
        false
      end
    end

    def hash = component_ary.hash
    def eql?(oth) = self.class == oth.class && parser == oth.parser && component_ary.eql?(oth.component_ary)

    def component_ary
      component.collect { |x| __send__(x) }
    end
    protected :component_ary

    def select(*components)
      components.collect do |c|
        if component.include?(c)
          __send__(c)
        else
          raise ArgumentError, "expected of components of #{self.class} (#{self.class.component.join(', ')})"
        end
      end
    end

    def inspect
      "#<#{self.class} #{self}>"
    end

    def coerce(oth)
      case oth
      when String
        oth = parser.parse(oth)
      else
        super
      end
      [oth, self]
    end

    def find_proxy(env = ENV)
      nil
    end
  end

  # -- schemes -------------------------------------------------------------

  class HTTP < Generic
    DEFAULT_PORT = 80
    COMPONENT = [:scheme, :userinfo, :host, :port, :path, :query, :fragment].freeze

    def self.build(args)
      tmp = Util.make_components_hash(self, args)
      super(tmp)
    end

    def request_uri
      return unless @path
      url = @query ? "#@path?#@query" : @path.dup
      url.start_with?("/") ? url : "/" + url
    end

    def authority
      if port == default_port
        host
      else
        "#{host}:#{port}"
      end
    end

    def origin
      "#{scheme}://#{authority}"
    end
  end

  class HTTPS < HTTP
    DEFAULT_PORT = 443
  end

  class WS < Generic
    DEFAULT_PORT = 80
    COMPONENT = [:scheme, :userinfo, :host, :port, :path, :query].freeze

    def self.build(args)
      tmp = Util.make_components_hash(self, args)
      super(tmp)
    end

    def resource_name
      return unless @path
      url = @query ? "#@path?#@query" : @path.dup
      url.start_with?("/") ? url : "/" + url
    end
  end

  class WSS < WS
    DEFAULT_PORT = 443
  end

  class FTP < Generic
    DEFAULT_PORT = 21
    COMPONENT = [:scheme, :userinfo, :host, :port, :path, :typecode].freeze
    TYPECODE = ["a", "i", "d"].freeze
    TYPECODE_PREFIX = ";type="

    def self.new2(user, password, host, port, path, typecode = nil, arg_check = true)
      typecode = nil if typecode.size == 0 rescue nil
      if path && path.start_with?("/")
        path = "%2F" + path[1..]
      end
      new("ftp", [user, password], host, port, nil, typecode ? path + TYPECODE_PREFIX + typecode : path,
          nil, nil, nil, DEFAULT_PARSER, arg_check)
    end

    def self.build(args)
      tmp = Util.make_components_hash(self, args)
      if tmp[:path] && tmp[:path].start_with?("/")
        tmp[:path] = "%2F" + tmp[:path][1..]
      end
      if (typecode = tmp[:typecode])
        typecode = typecode.to_s
        raise InvalidComponentError, "bad typecode is specified: #{typecode}" unless TYPECODE.include?(typecode)
        tmp[:path] = (tmp[:path] || "") + TYPECODE_PREFIX + typecode
      end
      return super(tmp)
    end

    attr_reader :typecode

    def initialize(scheme, userinfo, host, port, registry, path, opaque, query, fragment,
                   parser = nil, arg_check = false)
      raise InvalidURIError unless path
      path = path.sub(/^\//, "")
      path = path.sub(/^%2F/, "/")
      super(scheme, userinfo, host, port, registry, path, opaque, query, fragment, parser || DEFAULT_PARSER, arg_check)
      @typecode = nil
      if (tmp = @path.index(TYPECODE_PREFIX))
        typecode = @path[tmp + TYPECODE_PREFIX.size..]
        @path = @path[0, tmp]
        if arg_check
          self.typecode = typecode
        else
          set_typecode(typecode)
        end
      end
    end

    def check_typecode(v)
      if TYPECODE.include?(v)
        true
      else
        raise InvalidComponentError, "bad typecode(expected #{TYPECODE.join(', ')}): #{v}"
      end
    end
    private :check_typecode

    def set_typecode(v)
      @typecode = v
    end
    protected :set_typecode

    def typecode=(typecode)
      check_typecode(typecode)
      set_typecode(typecode)
      typecode
    end

    def merge(oth)
      tmp = super(oth)
      tmp.set_typecode(oth.typecode) if self != tmp && oth.respond_to?(:typecode)
      tmp
    end

    def path
      @path.sub(/^\//, "%2F")
    end

    def set_path(v)
      super("/" + v.sub(/^%2F/, "/"))
    end
    protected :set_path

    def to_s
      save_path = nil
      if @typecode
        save_path = @path
        @path = @path + TYPECODE_PREFIX + @typecode
      end
      str = super
      @path = save_path if save_path
      str
    end
  end

  class File < Generic
    DEFAULT_PORT = nil
    COMPONENT = [:scheme, :host, :path].freeze

    def self.build(args)
      tmp = Util.make_components_hash(self, args)
      super(tmp)
    end

    def set_host(v)
      v = "" if v.nil? || v == "localhost"
      @host = v
    end

    def set_port(v) = raise(InvalidURIError, "cannot set port for file URI")
    def check_userinfo(user) = raise(InvalidURIError, "cannot set userinfo for file URI")
    def check_user(user) = raise(InvalidURIError, "cannot set user for file URI")
    def check_password(user) = raise(InvalidURIError, "cannot set password for file URI")
    def set_userinfo(v) = (v.nil? ? nil : raise(InvalidURIError, "cannot set userinfo for file URI"))
    def set_user(v) = (v.nil? ? nil : raise(InvalidURIError, "cannot set user for file URI"))
    def set_password(v) = (v.nil? ? nil : raise(InvalidURIError, "cannot set password for file URI"))
  end

  class LDAP < Generic
    DEFAULT_PORT = 389
    COMPONENT = [:scheme, :host, :port, :dn, :attributes, :scope, :filter, :extensions].freeze
    SCOPE = [SCOPE_ONE = "one", SCOPE_SUB = "sub", SCOPE_BASE = "base"].freeze

    def self.build(args)
      tmp = Util.make_components_hash(self, args)
      tmp[:dn] = "/" + tmp[:dn].to_s if tmp[:dn] && !tmp[:dn].start_with?("/")
      tmp[:dn] ||= "/"
      query = [tmp.delete(:attributes), tmp.delete(:scope), tmp.delete(:filter), tmp.delete(:extensions)]
      query.pop while !query.empty? && query.last.nil?
      tmp[:query] = query.empty? ? nil : query.map(&:to_s).join("?")
      tmp[:path] = tmp.delete(:dn)
      Generic.instance_method(:initialize)
      new(tmp[:scheme], nil, tmp[:host], tmp[:port], nil, tmp[:path], nil, tmp[:query], nil, DEFAULT_PARSER, true)
    end

    def initialize(*arg)
      super(*arg)
      raise InvalidURIError, "bad LDAP URL" if @fragment
      parse_dn
      parse_query
    end

    def parse_dn
      raise InvalidURIError, "bad LDAP URL" unless @path
      @dn = @path[1..]
    end
    private :parse_dn

    def parse_query
      @attributes = nil
      @scope = nil
      @filter = nil
      @extensions = nil
      if @query
        attrs, scope, filter, extensions = @query.split("?")
        @attributes = attrs if attrs && !attrs.empty?
        @scope = scope if scope && !scope.empty?
        @filter = filter if filter && !filter.empty?
        @extensions = extensions if extensions && !extensions.empty?
      end
    end
    private :parse_query

    def build_path_query
      @path = "/" + @dn
      query = [@attributes, @scope, @filter, @extensions]
      query.pop while !query.empty? && query.last.nil?
      @query = query.empty? ? nil : query.map(&:to_s).join("?")
    end
    private :build_path_query

    attr_reader :dn, :attributes, :scope, :filter, :extensions

    def dn=(val)
      @dn = val
      build_path_query
      val
    end

    def attributes=(val)
      @attributes = val
      build_path_query
      val
    end

    def scope=(val)
      @scope = val
      build_path_query
      val
    end

    def filter=(val)
      @filter = val
      build_path_query
      val
    end

    def extensions=(val)
      @extensions = val
      build_path_query
      val
    end

    def hierarchical? = false
  end

  class LDAPS < LDAP
    DEFAULT_PORT = 636
  end

  class MailTo < Generic
    DEFAULT_PORT = nil
    COMPONENT = [:scheme, :to, :headers].freeze

    def self.build(args)
      tmp = Util.make_components_hash(self, args)
      case tmp[:to]
      when Array
        tmp[:opaque] = tmp[:to].join(",")
      when String
        tmp[:opaque] = tmp[:to].dup
      else
        tmp[:opaque] = +""
      end
      if tmp[:headers]
        query = case tmp[:headers]
                when Array then tmp[:headers].collect { |x| x.size != 2 ? x.first.to_s : x.join("=") }.join("&")
                when Hash then tmp[:headers].collect { |h, v| "#{h}=#{v}" }.join("&")
                else tmp[:headers].to_s
                end
        tmp[:opaque] << "?" << query unless query.empty?
      end
      new(tmp[:scheme], nil, nil, nil, nil, nil, tmp[:opaque], nil, nil, DEFAULT_PARSER, false)
    end

    attr_reader :to, :headers

    def initialize(*arg)
      super(*arg)
      @to = nil
      @headers = []
      if @opaque
        to, header = @opaque.split("?", 2)
        @to = to && !to.empty? ? to : nil
        if header
          @headers = header.split("&").map { |x| x.split("=", 2) }
        end
      end
    end

    def to=(v)
      @to = v
      @opaque = [v, @headers.empty? ? nil : @headers.map { |h| h.join("=") }.join("&")].compact.join("?")
      v
    end

    def headers=(v)
      @headers = v.is_a?(Hash) ? v.to_a : v
      self.to = @to
      v
    end

    def to_mailtext
      to = URI.decode_www_form_component(@to.to_s)
      head = +""
      body = +""
      @headers.each do |x|
        case x[0]
        when "body" then body = URI.decode_www_form_component(x[1])
        when "to" then to << ", " + URI.decode_www_form_component(x[1])
        else head << URI.decode_www_form_component(x[0]).capitalize + ": " + URI.decode_www_form_component(x[1]) + "\n"
        end
      end
      "To: #{to}\n#{head}\n#{body}\n"
    end
    alias to_rfc822text to_mailtext
  end

  module Util
    def self.make_components_hash(klass, array_hash)
      tmp = {}
      if array_hash.is_a?(Array) && array_hash.size == klass.component.size - 1
        klass.component[1..].each_index { |i| tmp[klass.component[i + 1]] = array_hash[i].nil? ? nil : array_hash[i].dup }
      elsif array_hash.is_a?(Hash)
        array_hash.each do |key, value|
          tmp[key] = value.is_a?(String) || value.is_a?(Array) ? value.dup : value
        end
      else
        raise ArgumentError, "expected Array of or Hash of components of #{klass} (#{klass.component[1..].join(', ')})"
      end
      tmp[:scheme] = klass.to_s.sub(/\A.*::/, "").downcase
      tmp
    end
  end

  { "HTTP" => HTTP, "HTTPS" => HTTPS, "FTP" => FTP, "FILE" => File, "LDAP" => LDAP,
    "LDAPS" => LDAPS, "MAILTO" => MailTo, "WS" => WS, "WSS" => WSS }.each { |s, k| register_scheme(s, k) }
end

module Kernel
  def URI(uri)
    case uri
    when URI::Generic then uri
    when String then URI.parse(uri)
    else raise ArgumentError, "bad argument (expected URI object or URI string)"
    end
  end
  module_function :URI
end
