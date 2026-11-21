# net/http for oruby: HTTP/1.1 client over TCPSocket (TLS via Net::TLSSocket)

module Net
  class Protocol
    VERSION = "0.2.2"
  end

  class ProtocolError < StandardError; end
  class ProtoSyntaxError < ProtocolError; end
  class ProtoFatalError < ProtocolError; end
  class ProtoUnknownError < ProtocolError; end
  class ProtoServerError < ProtocolError; end
  class ProtoAuthError < ProtocolError; end
  class ProtoCommandError < ProtocolError; end
  class ProtoRetriableError < ProtocolError; end
  ProtocRetryError = ProtoRetriableError

  class OpenTimeout < Timeout::Error; end

  class ReadTimeout < Timeout::Error
    attr_reader :io

    def initialize(io = nil)
      @io = io
      super(io ? "Net::ReadTimeout with #{io.inspect}" : "Net::ReadTimeout")
    end
  end

  class WriteTimeout < Timeout::Error
    attr_reader :io

    def initialize(io = nil)
      @io = io
      super(io ? "Net::WriteTimeout with #{io.inspect}" : "Net::WriteTimeout")
    end
  end

  class TLSSocket
    class Error < StandardError; end
  end

  class HTTPBadResponse < StandardError; end
  class HTTPHeaderSyntaxError < StandardError; end

  module HTTPExceptions
    def initialize(msg, res)
      super(msg)
      @response = res
    end
    attr_reader :response
    alias data response
  end

  class HTTPError < ProtocolError
    include HTTPExceptions
  end

  class HTTPRetriableError < ProtoRetriableError
    include HTTPExceptions
  end

  class HTTPClientException < ProtoServerError
    include HTTPExceptions
  end
  HTTPServerException = HTTPClientException
  # for backward compatibility (net-http: renamed in Ruby 2.6)
  deprecate_constant(:HTTPServerException)

  class HTTPFatalError < ProtoFatalError
    include HTTPExceptions
  end

  class ReadAdapter
    def initialize(block)
      @block = block
    end

    def inspect
      "#<#{self.class}>"
    end

    def <<(str)
      @block.call(str)
      self
    end
  end

  # -- buffered IO -------------------------------------------------------------

  class BufferedIO
    BUFSIZE = 1024 * 16

    attr_reader :io
    attr_accessor :read_timeout, :write_timeout, :continue_timeout, :debug_output

    def initialize(io, read_timeout: 60, write_timeout: 60, continue_timeout: nil, debug_output: nil)
      @io = io
      @read_timeout = read_timeout
      @write_timeout = write_timeout
      @continue_timeout = continue_timeout
      @debug_output = debug_output
      @rbuf = "".b
    end

    def inspect
      "#<#{self.class} io=#{@io}>"
    end

    def eof?
      @rbuf.empty? && @io.eof?
    end

    def closed?
      @io.closed?
    end

    def read_eof?
      @read_eof
    end

    def close
      @io.close
    end

    def read(len, dest = "".b, ignore_eof = false)
      LOG "reading #{len} bytes..."
      read_bytes = 0
      begin
        while read_bytes + @rbuf.bytesize < len
          s = rbuf_consume
          read_bytes += s.bytesize
          dest << s
          rbuf_fill
        end
        s = rbuf_consume(len - read_bytes)
        read_bytes += s.bytesize
        dest << s
      rescue EOFError => e
        raise e unless ignore_eof
      end
      LOG "read #{read_bytes} bytes"
      dest
    end

    def read_all(dest = "".b)
      LOG "reading all..."
      read_bytes = 0
      begin
        while true
          s = rbuf_consume
          read_bytes += s.bytesize
          dest << s
          rbuf_fill
        end
      rescue EOFError
        nil
      end
      LOG "read #{read_bytes} bytes"
      dest
    end

    def readuntil(terminator, ignore_eof = false)
      offset = 0
      begin
        until (idx = @rbuf.index(terminator, offset))
          offset = @rbuf.bytesize
          rbuf_fill
        end
        return rbuf_consume(idx + terminator.bytesize)
      rescue EOFError => e
        raise e unless ignore_eof
        return rbuf_consume
      end
    end

    def readline
      readuntil("\n").chop
    end

    def write(*strs)
      writing do
        n = 0
        strs.each { |s| n += write0(s.to_s) }
        n
      end
    end
    alias << write

    def writeline(str)
      writing { write0(str + "\r\n") }
    end

    private

    def rbuf_fill
      if @io.respond_to?(:__tls_read)
        s = @io.__tls_read(BUFSIZE, @read_timeout)
        raise Net::ReadTimeout.new(@io) if s == :timeout
        raise_read_eof if s.nil?
        @rbuf << s
        return
      end
      loop do
        case rv = @io.read_nonblock(BUFSIZE, exception: false)
        when :wait_readable
          unless @io.respond_to?(:wait_readable) && @io.wait_readable(@read_timeout)
            raise Net::ReadTimeout.new(@io)
          end
        when :wait_writable
          @io.wait_writable(@read_timeout) or raise Net::ReadTimeout.new(@io)
        when nil
          raise_read_eof
        else
          @rbuf << rv.b
          return
        end
      end
    end

    def raise_read_eof
      @read_eof = true
      raise EOFError, "end of file reached"
    end

    def rbuf_consume(len = nil)
      if len.nil? || len >= @rbuf.bytesize
        s = @rbuf
        @rbuf = "".b
      else
        s = @rbuf.byteslice(0, len)
        @rbuf = @rbuf.byteslice(len, @rbuf.bytesize - len)
      end
      @debug_output << %Q[-> #{s.dump}\n] if @debug_output
      s
    end

    def writing
      @written_bytes = 0
      @debug_output << "<- " if @debug_output
      yield
      @debug_output << "\n" if @debug_output
      bytes = @written_bytes
      @written_bytes = nil
      bytes
    end

    def write0(str)
      @debug_output << str.dump if @debug_output
      len = @io.write(str)
      @written_bytes += len if @written_bytes
      len
    end

    def LOG(msg)
      return unless @debug_output
      @debug_output << msg + "\n"
    end
  end

  # -- headers -----------------------------------------------------------------

  module HTTPHeader
    MAX_KEY_LENGTH = 1024
    MAX_FIELD_LENGTH = 65536

    def initialize_http_header(initheader)
      @header = {}
      return unless initheader
      initheader.each do |key, value|
        if key?(key) && $VERBOSE
          warn "net/http: duplicated HTTP header: #{key}", uplevel: 3
        end
        if value.nil?
          warn "net/http: nil HTTP header: #{key}", uplevel: 3 if $VERBOSE
        else
          value = value.strip if value.is_a?(String)
          if value.to_s.count("\r\n") > 0
            raise ArgumentError, "header #{key} has field value #{value.inspect}, this cannot include CR/LF"
          end
          @header[key.downcase.to_s] = [value]
        end
      end
    end

    def size
      @header.size
    end
    alias length size

    def [](key)
      a = @header[key.downcase.to_s] or return nil
      a.join(", ")
    end

    def []=(key, val)
      unless val
        @header.delete(key.downcase.to_s)
        return val
      end
      set_field(key, val)
    end

    def add_field(key, val)
      stringified_downcased_key = key.downcase.to_s
      if @header.key?(stringified_downcased_key)
        append_field_value(@header[stringified_downcased_key], val)
      else
        set_field(key, val)
      end
    end

    private def set_field(key, val)
      case val
      when Enumerable
        ary = []
        append_field_value(ary, val)
        @header[key.downcase.to_s] = ary
      else
        val = val.to_s
        if /[\r\n]/n.match?(val.b)
          raise ArgumentError, "header field value cannot include CR/LF"
        end
        @header[key.downcase.to_s] = [val]
      end
    end

    private def append_field_value(ary, val)
      case val
      when Enumerable
        val.each { |x| append_field_value(ary, x) }
      else
        val = val.to_s
        if /[\r\n]/n.match?(val.b)
          raise ArgumentError, "header field value cannot include CR/LF"
        end
        ary.push val
      end
    end

    def get_fields(key)
      stringified_downcased_key = key.downcase.to_s
      return nil unless @header[stringified_downcased_key]
      @header[stringified_downcased_key].dup
    end

    def fetch(key, *args, &block)
      warn "block supersedes default value argument", uplevel: 1 if block && !args.empty?
      a = @header.fetch(key.downcase.to_s, *args, &block)
      a.is_a?(Array) ? a.join(", ") : a
    end

    def each_header
      return to_enum(__method__) { @header.size } unless block_given?
      @header.each do |k, va|
        yield k, va.join(", ")
      end
    end
    alias each each_header

    def each_name(&block)
      return to_enum(__method__) { @header.size } unless block_given?
      @header.each_key(&block)
    end
    alias each_key each_name

    def each_capitalized_name
      return to_enum(__method__) { @header.size } unless block_given?
      @header.each_key do |k|
        yield capitalize(k)
      end
    end

    def each_value
      return to_enum(__method__) { @header.size } unless block_given?
      @header.each_value do |va|
        yield va.join(", ")
      end
    end

    def delete(key)
      @header.delete(key.downcase.to_s)
    end

    def key?(key)
      @header.key?(key.downcase.to_s)
    end

    def to_hash
      @header.dup
    end

    def each_capitalized
      return to_enum(__method__) { @header.size } unless block_given?
      @header.each do |k, v|
        yield capitalize(k), v.join(", ")
      end
    end
    alias canonical_each each_capitalized

    def capitalize(name)
      name.to_s.split("-").map { |s| s.capitalize }.join("-")
    end
    private :capitalize

    def range
      return nil unless @header["range"]
      value = self["Range"]
      m = /\A\s*bytes\s*=\s*(.*)\z/i.match(value) or raise Net::HTTPHeaderSyntaxError, "invalid syntax for byte-ranges-specifier: '#{value}'"
      byte_range_set = m[1]
      byte_range_set.split(",").map do |spec|
        m = /(\d+)?\s*-\s*(\d+)?/i.match(spec) or raise Net::HTTPHeaderSyntaxError, "invalid byte-range-spec: '#{spec}'"
        d1 = m[1] && m[1].to_i
        d2 = m[2] && m[2].to_i
        if d1 && d2
          raise Net::HTTPHeaderSyntaxError, "last-byte-pos MUST greater than or equal to first-byte-pos but '#{spec}'" if d2 < d1
          d1..d2
        elsif d1
          d1..-1
        elsif d2
          -d2..-1
        else
          raise Net::HTTPHeaderSyntaxError, "range is not specified"
        end
      end
    end

    def set_range(r, e = nil)
      unless r
        @header.delete "range"
        return r
      end
      r = (r...r + e) if e
      case r
      when Numeric
        n = r.to_i
        rangestr = (n > 0 ? "0-#{n - 1}" : "-#{-n}")
      when Range
        first = r.first
        last = r.end
        last -= 1 if r.exclude_end?
        if last == -1
          rangestr = (first > 0 ? "#{first}-" : "-#{-first}")
        else
          raise Net::HTTPHeaderSyntaxError, "range.first is negative" if first < 0
          raise Net::HTTPHeaderSyntaxError, "range.last is negative" if last < 0
          raise Net::HTTPHeaderSyntaxError, "must be .first < .last" if first > last
          rangestr = "#{first}-#{last}"
        end
      else
        raise TypeError, "Range/Integer is required"
      end
      @header["range"] = ["bytes=#{rangestr}"]
      r
    end
    alias range= set_range

    def content_length
      return nil unless key?("Content-Length")
      len = self["Content-Length"].slice(/\d+/) or
        raise Net::HTTPHeaderSyntaxError, "wrong Content-Length format"
      len.to_i
    end

    def content_length=(len)
      unless len
        @header.delete "content-length"
        return nil
      end
      @header["content-length"] = [len.to_i.to_s]
    end

    def chunked?
      return false unless @header["transfer-encoding"]
      field = self["Transfer-Encoding"]
      field.split(/[^\-\w]+/).any? { |t| t.casecmp?("chunked") }
    end

    def content_range
      return nil unless @header["content-range"]
      m = %r{\A\s*(\w+)\s+(\d+)-(\d+)/(\d+|\*)}.match(self["Content-Range"]) or
        raise Net::HTTPHeaderSyntaxError, "wrong Content-Range format"
      return nil unless m[1] == "bytes"
      m[2].to_i..m[3].to_i
    end

    def range_length
      r = content_range() or return nil
      r.end - r.begin + 1
    end

    def content_type
      main = main_type()
      return nil unless main
      sub = sub_type()
      sub ? "#{main}/#{sub}" : main
    end

    def main_type
      return nil unless @header["content-type"]
      self["Content-Type"].split(";").first.to_s.split("/")[0].to_s.strip
    end

    def sub_type
      return nil unless @header["content-type"]
      _, sub = *self["Content-Type"].split(";").first.to_s.split("/")
      return nil unless sub
      sub.strip
    end

    def type_params
      result = {}
      list = self["Content-Type"].to_s.split(";")
      list.shift
      list.each do |param|
        k, v = *param.split("=", 2)
        result[k.strip] = v.strip
      end
      result
    end

    def set_content_type(type, params = {})
      @header["content-type"] = [type + params.map { |k, v| "; #{k}=#{v}" }.join("")]
    end
    alias content_type= set_content_type

    def set_form_data(params, sep = "&")
      query = URI.encode_www_form(params)
      query.gsub!(/&/, sep) if sep != "&"
      self.body = query
      self.content_type = "application/x-www-form-urlencoded"
    end
    alias form_data= set_form_data

    def set_form(params, enctype = "application/x-www-form-urlencoded", formopt = {})
      @body_data = params
      @body = nil
      @body_stream = nil
      @form_option = formopt
      case enctype
      when %r{\Aapplication/x-www-form-urlencoded\z}i, %r{\Amultipart/form-data\z}i
        self.content_type = enctype
      else
        raise ArgumentError, "invalid enctype: #{enctype}"
      end
    end

    def basic_auth(account, password)
      @header["authorization"] = [basic_encode(account, password)]
    end

    def proxy_basic_auth(account, password)
      @header["proxy-authorization"] = [basic_encode(account, password)]
    end

    def basic_encode(account, password)
      "Basic " + ["#{account}:#{password}"].pack("m0")
    end
    private :basic_encode

    def connection_close?
      token = /(?:\A|,)\s*close\s*(?:\z|,)/i
      @header["connection"]&.grep(token) { return true }
      @header["proxy-connection"]&.grep(token) { return true }
      false
    end

    def connection_keep_alive?
      token = /(?:\A|,)\s*keep-alive\s*(?:\z|,)/i
      @header["connection"]&.grep(token) { return true }
      @header["proxy-connection"]&.grep(token) { return true }
      false
    end
  end

  # -- requests ----------------------------------------------------------------

  class HTTPGenericRequest
    include HTTPHeader

    def initialize(m, reqbody, resbody, uri_or_path, initheader = nil)
      @method = m
      @request_has_body = reqbody
      @response_has_body = resbody

      if URI === uri_or_path
        raise ArgumentError, "not an HTTP URI" unless URI::HTTP === uri_or_path
        hostname = uri_or_path.hostname
        raise ArgumentError, "no host component for URI" unless hostname && hostname.length > 0
        @uri = uri_or_path.dup
        host = @uri.hostname.dup
        host << ":" << @uri.port.to_s if @uri.port != @uri.default_port
        @path = uri_or_path.request_uri
        raise ArgumentError, "no HTTP request path given" unless @path
      else
        @uri = nil
        host = nil
        raise ArgumentError, "no HTTP request path given" unless uri_or_path
        raise ArgumentError, "HTTP request path is empty" if uri_or_path.empty?
        @path = uri_or_path.dup
      end

      @decode_content = false

      if Net::HTTP::HAVE_ZLIB && @response_has_body && m != "HEAD"
        if !initheader || !initheader.keys.any? { |k| %w[accept-encoding range].include?(k.downcase) }
          @decode_content = true if @response_has_body
          initheader = initheader ? initheader.dup : {}
          initheader["accept-encoding"] = "gzip;q=1.0,deflate;q=0.6,identity;q=0.3"
        end
      end

      initialize_http_header initheader
      self["Accept"] ||= "*/*"
      self["User-Agent"] ||= "Ruby"
      self["Host"] ||= host if host
      @body = nil
      @body_stream = nil
      @body_data = nil
    end

    attr_reader :method, :path, :uri, :decode_content

    def update_uri(addr, port, ssl)
      return unless @uri
      if ssl
        scheme = "https"
        klass = URI::HTTPS
      else
        scheme = "http"
        klass = URI::HTTP
      end
      if (host = self["host"])
        host = host.sub(/:.*/m, "")
      elsif (host = @uri.host)
      else
        host = addr
      end
      @uri = klass.new(scheme, @uri.userinfo, host, port, nil, @uri.path, nil, @uri.query, nil) rescue @uri
      @uri
    end

    def inspect
      "\#<#{self.class} #{@method}>"
    end

    def []=(key, val)
      @decode_content = false if key.downcase == "accept-encoding"
      super(key, val)
    end

    def request_body_permitted?
      @request_has_body
    end

    def response_body_permitted?
      @response_has_body
    end

    def body_exist?
      warn "Net::HTTPRequest#body_exist? is obsolete; use response_body_permitted?", uplevel: 1 if $VERBOSE
      response_body_permitted?
    end

    attr_reader :body

    def body=(str)
      @body = str
      @body_stream = nil
      @body_data = nil
      str
    end

    attr_reader :body_stream

    def body_stream=(input)
      @body = nil
      @body_stream = input
      @body_data = nil
      input
    end

    def set_body_internal(str)
      raise ArgumentError, "both of body argument and HTTPRequest#body set" if str and (@body or @body_stream)
      self.body = str if str
      if @body.nil? && @body_stream.nil? && @body_data.nil? && request_body_permitted?
        self.body = ""
      end
    end

    def exec(sock, ver, path)
      if @body
        send_request_with_body sock, ver, path, @body
      elsif @body_stream
        send_request_with_body_stream sock, ver, path, @body_stream
      elsif @body_data
        send_request_with_body_data sock, ver, path, @body_data
      else
        write_header sock, ver, path
      end
    end

    private

    def send_request_with_body(sock, ver, path, body)
      self.content_length = body.bytesize
      delete "Transfer-Encoding"
      write_header sock, ver, path
      sock.write body
    end

    def send_request_with_body_stream(sock, ver, path, f)
      unless content_length() or chunked?
        raise ArgumentError, "Content-Length not given and Transfer-Encoding is not `chunked'"
      end
      write_header sock, ver, path
      if chunked?
        while (chunk = f.read(16 * 1024))
          sock.write(chunk.bytesize.to_s(16) + "\r\n" + chunk + "\r\n")
        end
        sock.write "0\r\n\r\n"
      else
        while (chunk = f.read(16 * 1024))
          sock.write chunk
        end
      end
    end

    def send_request_with_body_data(sock, ver, path, params)
      if %r{\Amultipart/form-data\z}i !~ self.content_type
        self.content_type = "application/x-www-form-urlencoded"
        return send_request_with_body(sock, ver, path, URI.encode_www_form(params))
      end
      boundary = @form_option[:boundary] || Random.new.bytes(16).unpack1("H*")
      self.set_content_type(self.content_type, boundary: boundary)
      buf = +""
      params.each do |key, value, h = {}|
        buf << "--#{boundary}\r\n"
        if value.respond_to?(:read)
          filename = h[:filename] || (value.respond_to?(:path) ? File.basename(value.path) : "file")
          buf << "Content-Disposition: form-data; name=\"#{key}\"; filename=\"#{filename}\"\r\n"
          buf << "Content-Type: #{h[:content_type] || 'application/octet-stream'}\r\n\r\n"
          buf << value.read << "\r\n"
        else
          buf << "Content-Disposition: form-data; name=\"#{key}\"\r\n\r\n#{value}\r\n"
        end
      end
      buf << "--#{boundary}--\r\n"
      send_request_with_body(sock, ver, path, buf)
    end

    def write_header(sock, ver, path)
      reqline = "#{@method} #{path} HTTP/#{ver}"
      if /[\r\n]/ =~ reqline
        raise ArgumentError, "A Request-Line must not contain CR or LF"
      end
      buf = +""
      buf << reqline << "\r\n"
      each_capitalized do |k, v|
        buf << "#{k}: #{v}\r\n"
      end
      buf << "\r\n"
      sock.write buf
    end
  end

  class HTTPRequest < HTTPGenericRequest
    def initialize(path, initheader = nil)
      super self.class::METHOD,
            self.class::REQUEST_HAS_BODY,
            self.class::RESPONSE_HAS_BODY,
            path, initheader
    end
  end

  # -- responses ---------------------------------------------------------------

  class HTTPResponse
    include HTTPHeader

    class << self
      def body_permitted?
        self::HAS_BODY
      end

      def exception_type
        self::EXCEPTION_TYPE
      end

      def read_new(sock)
        httpv, code, msg = read_status_line(sock)
        res = response_class(code).new(httpv, code, msg)
        each_response_header(sock) do |k, v|
          res.add_field k, v
        end
        res
      end

      private

      def read_status_line(sock)
        str = sock.readline
        m = /\AHTTP(?:\/(\d+\.\d+))?\s+(\d\d\d)(?:\s+(.*))?\z/i.match(str) or
          raise Net::HTTPBadResponse, "wrong status line: #{str.dump}"
        m.captures
      end

      def response_class(code)
        CODE_TO_OBJ[code] or
          CODE_CLASS_TO_OBJ[code[0, 1]] or
          Net::HTTPUnknownResponse
      end

      def each_response_header(sock)
        key = value = nil
        while true
          line = sock.readuntil("\n", true).sub(/\s+\z/, "")
          break if line.empty?
          if line[0] == " " || line[0] == "\t" && value
            value << " " unless value.empty?
            value << line.strip
          else
            yield key, value if key
            key, value = line.strip.split(/\s*:\s*/, 2)
            raise Net::HTTPBadResponse, "wrong header line format" if value.nil?
          end
        end
        yield key, value if key
      end
    end

    def initialize(httpv, code, msg)
      @http_version = httpv
      @code = code
      @message = msg
      initialize_http_header nil
      @body = nil
      @read = false
      @uri = nil
      @decode_content = false
      @body_encoding = false
      @ignore_eof = true
    end

    attr_reader :http_version, :code, :message
    alias msg message
    attr_reader :uri
    attr_accessor :decode_content, :ignore_eof
    attr_reader :body_encoding

    def body_encoding=(value)
      value = Encoding.find(value) if value.is_a?(String)
      @body_encoding = value
    end

    def inspect
      "#<#{self.class} #{@code} #{@message} readbody=#{@read}>"
    end

    def code_type
      self.class
    end

    def error!
      message = @code
      message = "#{message} #{@message.dump}" if @message
      raise error_type().new(message, self)
    end

    def error_type
      self.class::EXCEPTION_TYPE
    end

    def value
      error! unless self.kind_of?(Net::HTTPSuccess)
    end

    def uri=(uri)
      @uri = uri.dup if uri
    end

    def response
      warn "Net::HTTPResponse#response is obsolete", uplevel: 1 if $VERBOSE
      self
    end

    def header
      warn "Net::HTTPResponse#header is obsolete", uplevel: 1 if $VERBOSE
      self
    end

    def read_header
      warn "Net::HTTPResponse#read_header is obsolete", uplevel: 1 if $VERBOSE
      self
    end

    def reading_body(sock, reqmethodallowbody)
      @socket = sock
      @body_exist = reqmethodallowbody && self.class.body_permitted?
      begin
        yield
        self.body
      ensure
        @socket = nil
      end
    end

    def read_body(dest = nil, &block)
      if @read
        raise IOError, "#{self.class}\#read_body called twice" if dest or block
        return @body
      end
      to = procdest(dest, block)
      stream_check
      if @body_exist
        read_body_0 to
        @body = to
      else
        @body = nil
      end
      @read = true
      @body = detect_encoding(@body) if @body.is_a?(String) && @body_encoding
      @body
    end

    def body
      read_body
    end

    def body=(value)
      @body = value
    end
    alias entity body

    private

    def detect_encoding(str)
      enc = @body_encoding == true ? (type_params["charset"] || "UTF-8") : @body_encoding
      str.force_encoding(enc) rescue str
    end

    def stream_check
      raise IOError, "attempt to read body out of block" if @socket.nil? || @socket.closed?
    end

    def procdest(dest, block)
      raise ArgumentError, "both arg and block given for HTTP method" if dest and block
      if block
        Net::ReadAdapter.new(block)
      else
        dest || +""
      end
    end

    def read_body_0(dest)
      encoding = @decode_content ? self["content-encoding"].to_s.downcase : ""
      if encoding == "gzip" || encoding == "deflate" || encoding == "x-gzip"
        raw = "".b
        read_raw_body(raw)
        delete "content-encoding"
        self.content_length = nil
        data = if encoding == "deflate"
                 Zlib::Inflate.inflate(raw)
               else
                 Zlib.gunzip(raw)
               end
        dest << data
      else
        read_raw_body(dest)
      end
    end

    def read_raw_body(dest)
      if chunked?
        read_chunked dest
        return
      end
      clen = content_length()
      if clen
        @socket.read clen, dest, @ignore_eof
        return
      end
      clen = range_length()
      if clen
        @socket.read clen, dest
        return
      end
      @socket.read_all dest
    end

    def read_chunked(dest)
      total = 0
      while true
        line = @socket.readline
        hexlen = line.slice(/[0-9a-fA-F]+/) or
          raise Net::HTTPBadResponse, "wrong chunk size line: #{line}"
        len = hexlen.hex
        break if len == 0
        begin
          @socket.read len, dest
        ensure
          total += len
          @socket.read 2
        end
      end
      until @socket.readline.empty?
      end
    end
  end

  class HTTPUnknownResponse < HTTPResponse
    HAS_BODY = true
    EXCEPTION_TYPE = HTTPError
  end

  class HTTPInformation < HTTPResponse
    HAS_BODY = false
    EXCEPTION_TYPE = HTTPError
  end

  class HTTPSuccess < HTTPResponse
    HAS_BODY = true
    EXCEPTION_TYPE = HTTPError
  end

  class HTTPRedirection < HTTPResponse
    HAS_BODY = true
    EXCEPTION_TYPE = HTTPRetriableError
  end

  class HTTPClientError < HTTPResponse
    HAS_BODY = true
    EXCEPTION_TYPE = HTTPClientException
  end

  class HTTPServerError < HTTPResponse
    HAS_BODY = true
    EXCEPTION_TYPE = HTTPFatalError
  end

  {
    "HTTPContinue" => [HTTPInformation, "100", false],
    "HTTPSwitchProtocol" => [HTTPInformation, "101", false],
    "HTTPProcessing" => [HTTPInformation, "102", false],
    "HTTPEarlyHints" => [HTTPInformation, "103", false],
    "HTTPOK" => [HTTPSuccess, "200", true],
    "HTTPCreated" => [HTTPSuccess, "201", true],
    "HTTPAccepted" => [HTTPSuccess, "202", true],
    "HTTPNonAuthoritativeInformation" => [HTTPSuccess, "203", true],
    "HTTPNoContent" => [HTTPSuccess, "204", false],
    "HTTPResetContent" => [HTTPSuccess, "205", false],
    "HTTPPartialContent" => [HTTPSuccess, "206", true],
    "HTTPMultiStatus" => [HTTPSuccess, "207", true],
    "HTTPAlreadyReported" => [HTTPSuccess, "208", true],
    "HTTPIMUsed" => [HTTPSuccess, "226", true],
    "HTTPMultipleChoices" => [HTTPRedirection, "300", true],
    "HTTPMovedPermanently" => [HTTPRedirection, "301", true],
    "HTTPFound" => [HTTPRedirection, "302", true],
    "HTTPSeeOther" => [HTTPRedirection, "303", true],
    "HTTPNotModified" => [HTTPRedirection, "304", false],
    "HTTPUseProxy" => [HTTPRedirection, "305", false],
    "HTTPTemporaryRedirect" => [HTTPRedirection, "307", true],
    "HTTPPermanentRedirect" => [HTTPRedirection, "308", true],
    "HTTPBadRequest" => [HTTPClientError, "400", true],
    "HTTPUnauthorized" => [HTTPClientError, "401", true],
    "HTTPPaymentRequired" => [HTTPClientError, "402", true],
    "HTTPForbidden" => [HTTPClientError, "403", true],
    "HTTPNotFound" => [HTTPClientError, "404", true],
    "HTTPMethodNotAllowed" => [HTTPClientError, "405", true],
    "HTTPNotAcceptable" => [HTTPClientError, "406", true],
    "HTTPProxyAuthenticationRequired" => [HTTPClientError, "407", true],
    "HTTPRequestTimeout" => [HTTPClientError, "408", true],
    "HTTPConflict" => [HTTPClientError, "409", true],
    "HTTPGone" => [HTTPClientError, "410", true],
    "HTTPLengthRequired" => [HTTPClientError, "411", true],
    "HTTPPreconditionFailed" => [HTTPClientError, "412", true],
    "HTTPPayloadTooLarge" => [HTTPClientError, "413", true],
    "HTTPURITooLong" => [HTTPClientError, "414", true],
    "HTTPUnsupportedMediaType" => [HTTPClientError, "415", true],
    "HTTPRangeNotSatisfiable" => [HTTPClientError, "416", true],
    "HTTPExpectationFailed" => [HTTPClientError, "417", true],
    "HTTPMisdirectedRequest" => [HTTPClientError, "421", true],
    "HTTPUnprocessableEntity" => [HTTPClientError, "422", true],
    "HTTPLocked" => [HTTPClientError, "423", true],
    "HTTPFailedDependency" => [HTTPClientError, "424", true],
    "HTTPUpgradeRequired" => [HTTPClientError, "426", true],
    "HTTPPreconditionRequired" => [HTTPClientError, "428", true],
    "HTTPTooManyRequests" => [HTTPClientError, "429", true],
    "HTTPRequestHeaderFieldsTooLarge" => [HTTPClientError, "431", true],
    "HTTPUnavailableForLegalReasons" => [HTTPClientError, "451", true],
    "HTTPInternalServerError" => [HTTPServerError, "500", true],
    "HTTPNotImplemented" => [HTTPServerError, "501", true],
    "HTTPBadGateway" => [HTTPServerError, "502", true],
    "HTTPServiceUnavailable" => [HTTPServerError, "503", true],
    "HTTPGatewayTimeout" => [HTTPServerError, "504", true],
    "HTTPVersionNotSupported" => [HTTPServerError, "505", true],
    "HTTPVariantAlsoNegotiates" => [HTTPServerError, "506", true],
    "HTTPInsufficientStorage" => [HTTPServerError, "507", true],
    "HTTPLoopDetected" => [HTTPServerError, "508", true],
    "HTTPNotExtended" => [HTTPServerError, "510", true],
    "HTTPNetworkAuthenticationRequired" => [HTTPServerError, "511", true]
  }.each do |name, (parent, _code, body)|
    cls = Class.new(parent)
    cls.const_set(:HAS_BODY, body)
    const_set(name, cls)
  end

  HTTPMovedTemporarily = HTTPFound
  HTTPRequestEntityTooLarge = HTTPPayloadTooLarge
  HTTPRequestURITooLong = HTTPURITooLong
  HTTPRequestURITooLarge = HTTPURITooLong
  HTTPRequestedRangeNotSatisfiable = HTTPRangeNotSatisfiable

  class HTTPResponse
    CODE_CLASS_TO_OBJ = {
      "1" => Net::HTTPInformation,
      "2" => Net::HTTPSuccess,
      "3" => Net::HTTPRedirection,
      "4" => Net::HTTPClientError,
      "5" => Net::HTTPServerError
    }
    CODE_TO_OBJ = {}
    {
      "100" => Net::HTTPContinue, "101" => Net::HTTPSwitchProtocol, "102" => Net::HTTPProcessing,
      "103" => Net::HTTPEarlyHints, "200" => Net::HTTPOK, "201" => Net::HTTPCreated,
      "202" => Net::HTTPAccepted, "203" => Net::HTTPNonAuthoritativeInformation,
      "204" => Net::HTTPNoContent, "205" => Net::HTTPResetContent, "206" => Net::HTTPPartialContent,
      "207" => Net::HTTPMultiStatus, "208" => Net::HTTPAlreadyReported, "226" => Net::HTTPIMUsed,
      "300" => Net::HTTPMultipleChoices, "301" => Net::HTTPMovedPermanently, "302" => Net::HTTPFound,
      "303" => Net::HTTPSeeOther, "304" => Net::HTTPNotModified, "305" => Net::HTTPUseProxy,
      "307" => Net::HTTPTemporaryRedirect, "308" => Net::HTTPPermanentRedirect,
      "400" => Net::HTTPBadRequest, "401" => Net::HTTPUnauthorized, "402" => Net::HTTPPaymentRequired,
      "403" => Net::HTTPForbidden, "404" => Net::HTTPNotFound, "405" => Net::HTTPMethodNotAllowed,
      "406" => Net::HTTPNotAcceptable, "407" => Net::HTTPProxyAuthenticationRequired,
      "408" => Net::HTTPRequestTimeout, "409" => Net::HTTPConflict, "410" => Net::HTTPGone,
      "411" => Net::HTTPLengthRequired, "412" => Net::HTTPPreconditionFailed,
      "413" => Net::HTTPPayloadTooLarge, "414" => Net::HTTPURITooLong,
      "415" => Net::HTTPUnsupportedMediaType, "416" => Net::HTTPRangeNotSatisfiable,
      "417" => Net::HTTPExpectationFailed, "421" => Net::HTTPMisdirectedRequest,
      "422" => Net::HTTPUnprocessableEntity, "423" => Net::HTTPLocked,
      "424" => Net::HTTPFailedDependency, "426" => Net::HTTPUpgradeRequired,
      "428" => Net::HTTPPreconditionRequired, "429" => Net::HTTPTooManyRequests,
      "431" => Net::HTTPRequestHeaderFieldsTooLarge, "451" => Net::HTTPUnavailableForLegalReasons,
      "500" => Net::HTTPInternalServerError, "501" => Net::HTTPNotImplemented,
      "502" => Net::HTTPBadGateway, "503" => Net::HTTPServiceUnavailable,
      "504" => Net::HTTPGatewayTimeout, "505" => Net::HTTPVersionNotSupported,
      "506" => Net::HTTPVariantAlsoNegotiates, "507" => Net::HTTPInsufficientStorage,
      "508" => Net::HTTPLoopDetected, "510" => Net::HTTPNotExtended,
      "511" => Net::HTTPNetworkAuthenticationRequired
    }.each { |k, v| CODE_TO_OBJ[k] = v }
  end

  # -- the client --------------------------------------------------------------

  class HTTP < Protocol
    VERSION = "0.6.0"
    HTTPVersion = "1.1"
    HAVE_ZLIB = Object.const_defined?(:Zlib)

    class Get < HTTPRequest
      METHOD = "GET"; REQUEST_HAS_BODY = false; RESPONSE_HAS_BODY = true
    end

    class Head < HTTPRequest
      METHOD = "HEAD"; REQUEST_HAS_BODY = false; RESPONSE_HAS_BODY = false
    end

    class Post < HTTPRequest
      METHOD = "POST"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class Put < HTTPRequest
      METHOD = "PUT"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class Delete < HTTPRequest
      METHOD = "DELETE"; REQUEST_HAS_BODY = false; RESPONSE_HAS_BODY = true
    end

    class Options < HTTPRequest
      METHOD = "OPTIONS"; REQUEST_HAS_BODY = false; RESPONSE_HAS_BODY = true
    end

    class Trace < HTTPRequest
      METHOD = "TRACE"; REQUEST_HAS_BODY = false; RESPONSE_HAS_BODY = true
    end

    class Patch < HTTPRequest
      METHOD = "PATCH"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class Propfind < HTTPRequest
      METHOD = "PROPFIND"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class Proppatch < HTTPRequest
      METHOD = "PROPPATCH"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class Mkcol < HTTPRequest
      METHOD = "MKCOL"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class Copy < HTTPRequest
      METHOD = "COPY"; REQUEST_HAS_BODY = false; RESPONSE_HAS_BODY = true
    end

    class Move < HTTPRequest
      METHOD = "MOVE"; REQUEST_HAS_BODY = false; RESPONSE_HAS_BODY = true
    end

    class Lock < HTTPRequest
      METHOD = "LOCK"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class Unlock < HTTPRequest
      METHOD = "UNLOCK"; REQUEST_HAS_BODY = true; RESPONSE_HAS_BODY = true
    end

    class << self
      def version_1_2
        true
      end

      def version_1_1
        false
      end

      def version_1_2?
        true
      end
      alias is_version_1_2? version_1_2?

      def version_1_1?
        false
      end
      alias is_version_1_1? version_1_1?

      def default_port
        http_default_port
      end

      def http_default_port
        80
      end

      def https_default_port
        443
      end

      def socket_type
        warn "Net::HTTP.socket_type is obsolete", uplevel: 1 if $VERBOSE
        BufferedIO
      end

      def get_print(uri_or_host, path_or_headers = nil, port = nil)
        get_response(uri_or_host, path_or_headers, port) do |res|
          res.read_body do |chunk|
            $stdout.print chunk
          end
        end
        nil
      end

      def get(uri_or_host, path_or_headers = nil, port = nil)
        get_response(uri_or_host, path_or_headers, port).body
      end

      def get_response(uri_or_host, path_or_headers = nil, port = nil, &block)
        if path_or_headers && !path_or_headers.is_a?(Hash)
          host = uri_or_host
          path = path_or_headers
          new(host, port || HTTP.default_port).start do |http|
            return http.request_get(path, &block)
          end
        else
          uri = uri_or_host
          headers = path_or_headers
          start(uri.hostname, uri.port, use_ssl: uri.scheme == "https") do |http|
            return http.request_get(uri, headers, &block)
          end
        end
      end

      def post(url, data, header = nil)
        start(url.hostname, url.port, use_ssl: url.scheme == "https") do |http|
          http.post(url, data, header)
        end
      end

      def post_form(url, params)
        req = Post.new(url)
        req.form_data = params
        req.basic_auth url.user, url.password if url.user
        start(url.hostname, url.port, use_ssl: url.scheme == "https") do |http|
          http.request(req)
        end
      end

      def put(url, data, header = nil)
        start(url.hostname, url.port, use_ssl: url.scheme == "https") do |http|
          http.put(url, data, header)
        end
      end

      def start(address, *arg, &block)
        arg.pop if (opt = Hash.try_convert(arg[-1]))
        port, p_addr, p_port, p_user, p_pass = *arg
        p_addr = :ENV if arg.size < 2
        port = https_default_port if !port && opt && opt[:use_ssl]
        http = new(address, port, p_addr, p_port, p_user, p_pass)
        http.ipaddr = opt[:ipaddr] if opt && opt[:ipaddr]
        if opt
          if opt[:use_ssl]
            opt = { verify_mode: 1 }.update(opt)
          end
          opt.each do |k, v|
            http.__send__("#{k}=", v) if http.respond_to?("#{k}=")
          end
        end
        http.start(&block)
      end

      alias newobj new

      def new(address, port = nil, p_addr = :ENV, p_port = nil, p_user = nil, p_pass = nil, p_no_proxy = nil, p_use_ssl = nil)
        http = super(address, port)
        if proxy_class?
          http.proxy_from_env = @proxy_from_env
          http.proxy_address = @proxy_address
          http.proxy_port = @proxy_port
          http.proxy_user = @proxy_user
          http.proxy_pass = @proxy_pass
          http.proxy_use_ssl = @proxy_use_ssl
        elsif p_addr == :ENV
          http.proxy_from_env = true
        else
          if p_addr && p_no_proxy && !URI::Generic.use_proxy?(address, address, port, p_no_proxy)
            p_addr = nil
            p_port = nil
          end
          http.proxy_address = p_addr
          http.proxy_port = p_port || default_port
          http.proxy_user = p_user
          http.proxy_pass = p_pass
          http.proxy_use_ssl = p_use_ssl
        end
        http
      end

      def Proxy(p_addr = :ENV, p_port = nil, p_user = nil, p_pass = nil, p_use_ssl = nil)
        return self unless p_addr
        Class.new(self) do
          @is_proxy_class = true
          if p_addr == :ENV
            @proxy_from_env = true
            @proxy_address = nil
            @proxy_port = nil
          else
            @proxy_from_env = false
            @proxy_address = p_addr
            @proxy_port = p_port || default_port
          end
          @proxy_user = p_user
          @proxy_pass = p_pass
          @proxy_use_ssl = p_use_ssl
        end
      end

      def proxy_class?
        defined?(@is_proxy_class) ? @is_proxy_class : false
      end

      attr_reader :proxy_address, :proxy_port, :proxy_user, :proxy_pass, :proxy_use_ssl
    end

    def initialize(address, port = nil)
      @address = address
      @port = port || HTTP.default_port
      @ipaddr = nil
      @local_host = nil
      @local_port = nil
      @curr_http_version = HTTPVersion
      @keep_alive_timeout = 2
      @last_communicated = nil
      @close_on_empty_response = false
      @socket = nil
      @started = false
      @open_timeout = 60
      @read_timeout = 60
      @write_timeout = 60
      @continue_timeout = nil
      @max_retries = 1
      @debug_output = nil
      @response_body_encoding = false
      @ignore_eof = true
      @proxy_from_env = false
      @proxy_uri = nil
      @proxy_address = nil
      @proxy_port = nil
      @proxy_user = nil
      @proxy_pass = nil
      @proxy_use_ssl = nil
      @use_ssl = false
      @verify_mode = nil
      @ca_file = nil
      @ca_path = nil
      @verify_hostname = true
      @ssl_context = nil
    end

    def inspect
      "#<#{self.class} #{@address}:#{@port} open=#{started?}>"
    end

    def set_debug_output(output)
      warn "Net::HTTP#set_debug_output called after HTTP started", uplevel: 1 if started?
      @debug_output = output
    end

    attr_reader :address, :port
    attr_accessor :local_host, :local_port, :open_timeout, :keep_alive_timeout, :max_retries,
                  :close_on_empty_response, :ignore_eof, :response_body_encoding
    attr_reader :read_timeout, :write_timeout, :continue_timeout
    attr_writer :proxy_from_env, :proxy_address, :proxy_port, :proxy_user, :proxy_pass, :proxy_use_ssl
    attr_accessor :verify_mode, :ca_file, :ca_path, :cert, :key, :cert_store, :ciphers, :ssl_version,
                  :min_version, :max_version, :ssl_timeout, :verify_callback, :verify_depth,
                  :verify_hostname, :extra_chain_cert

    def ipaddr
      started? ? @socket.io.peeraddr[3] : @ipaddr
    end

    def ipaddr=(addr)
      raise IOError, "ipaddr value changed, but session already started" if started?
      @ipaddr = addr
    end

    def read_timeout=(sec)
      @socket.read_timeout = sec if @socket
      @read_timeout = sec
    end

    def write_timeout=(sec)
      @socket.write_timeout = sec if @socket
      @write_timeout = sec
    end

    def continue_timeout=(sec)
      @socket.continue_timeout = sec if @socket
      @continue_timeout = sec
    end

    def started?
      @started
    end
    alias active? started?

    def use_ssl?
      @use_ssl
    end

    def use_ssl=(flag)
      flag = flag ? true : false
      if started? && @use_ssl != flag
        raise IOError, "use_ssl value changed, but session already started"
      end
      @use_ssl = flag
    end

    def peer_cert
      return nil unless use_ssl? && @socket
      @socket.io.peer_cert_subject
    end

    def start
      raise IOError, "HTTP session already opened" if @started
      if block_given?
        begin
          do_start
          return yield(self)
        ensure
          do_finish
        end
      end
      do_start
      self
    end

    def finish
      raise IOError, "HTTP session not yet started" unless started?
      do_finish
    end

    # proxy

    def proxy?
      !!(@proxy_from_env ? proxy_uri : @proxy_address)
    end

    def proxy_from_env?
      @proxy_from_env
    end

    def proxy_uri
      return if @proxy_uri == false
      @proxy_uri ||= URI::HTTP.new("http", nil, address, port, nil, nil, nil, nil, nil).find_proxy rescue nil
      @proxy_uri || nil
    end

    def proxy_address
      if @proxy_from_env
        proxy_uri&.hostname
      else
        @proxy_address
      end
    end

    def proxy_port
      if @proxy_from_env
        proxy_uri&.port
      else
        @proxy_port
      end
    end

    def proxy_user
      if @proxy_from_env
        user = proxy_uri&.user
        user ? URI.decode_www_form_component(user) : nil
      else
        @proxy_user
      end
    end

    def proxy_pass
      if @proxy_from_env
        pass = proxy_uri&.password
        pass ? URI.decode_www_form_component(pass) : nil
      else
        @proxy_pass
      end
    end

    def proxy_use_ssl
      @proxy_use_ssl
    end

    alias proxyaddr proxy_address
    alias proxyport proxy_port

    # requests

    def get(path, initheader = nil, dest = nil, &block)
      res = nil
      request(Get.new(path, initheader)) do |r|
        r.read_body dest, &block
        res = r
      end
      res
    end

    def head(path, initheader = nil)
      request(Head.new(path, initheader))
    end

    def post(path, data, initheader = nil, dest = nil, &block)
      send_entity(path, data, initheader, dest, Post, &block)
    end

    def patch(path, data, initheader = nil, dest = nil, &block)
      send_entity(path, data, initheader, dest, Patch, &block)
    end

    def put(path, data, initheader = nil)
      request(Put.new(path, initheader), data)
    end

    def proppatch(path, body, initheader = nil)
      request(Proppatch.new(path, initheader), body)
    end

    def lock(path, body, initheader = nil)
      request(Lock.new(path, initheader), body)
    end

    def unlock(path, body, initheader = nil)
      request(Unlock.new(path, initheader), body)
    end

    def options(path, initheader = nil)
      request(Options.new(path, initheader))
    end

    def propfind(path, body = nil, initheader = { "Depth" => "0" })
      request(Propfind.new(path, initheader), body)
    end

    def delete(path, initheader = { "Depth" => "Infinity" })
      request(Delete.new(path, initheader))
    end

    def move(path, initheader = nil)
      request(Move.new(path, initheader))
    end

    def copy(path, initheader = nil)
      request(Copy.new(path, initheader))
    end

    def mkcol(path, body = nil, initheader = nil)
      request(Mkcol.new(path, initheader), body)
    end

    def trace(path, initheader = nil)
      request(Trace.new(path, initheader))
    end

    def request_get(path, initheader = nil, &block)
      request(Get.new(path, initheader), &block)
    end

    def request_head(path, initheader = nil, &block)
      request(Head.new(path, initheader), &block)
    end

    def request_post(path, data, initheader = nil, &block)
      request Post.new(path, initheader), data, &block
    end

    def request_put(path, data, initheader = nil, &block)
      request Put.new(path, initheader), data, &block
    end

    alias get2 request_get
    alias head2 request_head
    alias post2 request_post
    alias put2 request_put

    def send_request(name, path, data = nil, header = nil)
      has_response_body = name != "HEAD"
      r = HTTPGenericRequest.new(name, (data ? true : false), has_response_body, path, header)
      request r, data
    end

    def request(req, body = nil, &block)
      unless started?
        start do
          req["connection"] ||= "close"
          return request(req, body, &block)
        end
      end
      if proxy_user && !use_ssl?
        req.proxy_basic_auth proxy_user, proxy_pass
      end
      req.set_body_internal body
      res = transport_request(req, &block)
      if sspi_auth?(res)
        res = transport_request(req, &block)
      end
      res
    end

    private

    def sspi_auth?(_res)
      false
    end

    def send_entity(path, data, initheader, dest, type, &block)
      res = nil
      request(type.new(path, initheader), data) do |r|
        r.read_body dest, &block
        res = r
      end
      res
    end

    def do_start
      connect
      @started = true
    end

    def do_finish
      @started = false
      @socket.close if @socket && !@socket.closed?
      @socket = nil
    end

    def conn_address
      @ipaddr || (proxy? ? proxy_address : address)
    end

    def conn_port
      proxy? ? proxy_port : port
    end

    def connect
      s = TCPSocket.open(conn_address, conn_port, @local_host, @local_port, connect_timeout: @open_timeout)
      s.setsockopt(Socket::IPPROTO_TCP, Socket::TCP_NODELAY, 1) rescue nil
      if use_ssl?
        if proxy?
          plain = BufferedIO.new(s, read_timeout: @read_timeout, write_timeout: @write_timeout,
                                    debug_output: @debug_output)
          buf = +"CONNECT #{address}:#{port} HTTP/#{HTTPVersion}\r\nHost: #{address}:#{port}\r\n"
          if proxy_user
            buf << "Proxy-Authorization: Basic #{["#{proxy_user}:#{proxy_pass}"].pack('m0')}\r\n"
          end
          buf << "\r\n"
          plain.write(buf)
          HTTPResponse.read_new(plain).value
        end
        verify = @verify_mode.nil? ? true : @verify_mode != 0
        hostname = @address.to_s.delete_prefix("[").delete_suffix("]")
        s = Net::TLSSocket.__connect(s.fileno, hostname, verify, @ca_file, @ca_path, @open_timeout)
      end
      @socket = BufferedIO.new(s, read_timeout: @read_timeout, write_timeout: @write_timeout,
                                  continue_timeout: @continue_timeout, debug_output: @debug_output)
      @debug_output << "opened connection to #{conn_address}:#{conn_port}\n" if @debug_output
      @last_communicated = nil
    rescue IO::TimeoutError => e
      raise Net::OpenTimeout, "Failed to open TCP connection to #{conn_address}:#{conn_port} (#{e.message})"
    rescue SystemCallError, SocketError => e
      raise e.class, "Failed to open TCP connection to #{conn_address}:#{conn_port} (#{e.message})"
    end

    def edit_path(path)
      if proxy? && !use_ssl?
        "http://#{addr_port}#{path}"
      else
        path
      end
    end

    def addr_port
      addr = address
      addr = "[#{addr}]" if addr.include?(":")
      default_port = use_ssl? ? HTTP.https_default_port : HTTP.http_default_port
      default_port == port ? addr : "#{addr}:#{port}"
    end

    def begin_transport(req)
      if @socket.nil? || @socket.closed?
        connect
      elsif @last_communicated && @last_communicated + @keep_alive_timeout < Process.clock_gettime(Process::CLOCK_MONOTONIC)
        @socket.close
        connect
      end
      if !req.response_body_permitted? && @close_on_empty_response
        req["connection"] ||= "close"
      end
      req.update_uri address, port, use_ssl?
      req["host"] ||= addr_port
    end

    def transport_request(req)
      count = 0
      begin
        begin_transport req
        res = catch(:response) do
          req.exec @socket, @curr_http_version, edit_path(req.path)
          begin
            res = HTTPResponse.read_new(@socket)
            res.decode_content = req.decode_content
            res.body_encoding = @response_body_encoding
            res.ignore_eof = @ignore_eof
          end while res.is_a?(HTTPInformation) && !res.is_a?(HTTPSwitchProtocol)
          res.uri = req.uri
          res.reading_body(@socket, req.response_body_permitted?) do
            yield res if block_given?
          end
          res
        end
      rescue Net::OpenTimeout => e
        raise e
      rescue Net::ReadTimeout, IOError, EOFError, Errno::ECONNRESET, Errno::ECONNABORTED, Errno::EPIPE,
             Errno::ETIMEDOUT, Timeout::Error => exception
        if count < max_retries && idempotent?(req) && !exception.is_a?(Net::ReadTimeout)
          count += 1
          @socket.close if @socket && !@socket.closed?
          retry
        end
        @socket.close if @socket && !@socket.closed?
        raise exception
      end
      end_transport req, res
      res
    end

    def idempotent?(req)
      %w[GET HEAD PUT DELETE OPTIONS TRACE].include?(req.method)
    end

    def end_transport(req, res)
      @curr_http_version = res.http_version
      @last_communicated = nil
      if @socket.closed?
        nil
      elsif @socket.read_eof? || (!res.body && @close_on_empty_response)
        @socket.close
      elsif keep_alive?(req, res)
        @last_communicated = Process.clock_gettime(Process::CLOCK_MONOTONIC)
      else
        @socket.close
      end
    end

    def keep_alive?(req, res)
      return false if req.connection_close?
      if @curr_http_version <= "1.0"
        res.connection_keep_alive?
      else
        !res.connection_close?
      end
    end
  end

  HTTPSession = HTTP

  class HTTP
    ProxyMod = Module.new
    ProxyDelta = ProxyMod
  end
end
