# Minimal OpenSSL: SSL constants/errors for net/http (TLS is Go crypto/tls),
# OpenSSL::Digest, OpenSSL::HMAC and secure random bytes over Go crypto.
module OpenSSL
  VERSION = "3.3.0"
  OPENSSL_VERSION = "Go crypto/tls"
  OPENSSL_LIBRARY_VERSION = OPENSSL_VERSION
  OPENSSL_VERSION_NUMBER = 0x30000000

  class OpenSSLError < StandardError; end

  module SSL
    VERIFY_NONE = 0
    VERIFY_PEER = 1
    VERIFY_FAIL_IF_NO_PEER_CERT = 2
    VERIFY_CLIENT_ONCE = 4
    OP_ALL = 0x80000BFF
    TLS1_VERSION = 0x301
    TLS1_1_VERSION = 0x302
    TLS1_2_VERSION = 0x303
    TLS1_3_VERSION = 0x304

    class SSLError < OpenSSLError; end
    class SSLErrorWaitReadable < SSLError
      include IO::WaitReadable
    end
    class SSLErrorWaitWritable < SSLError
      include IO::WaitWritable
    end

    class SSLContext
      DEFAULT_PARAMS = {
        verify_mode: VERIFY_PEER,
        verify_hostname: true,
      }.freeze

      def set_params(params = {})
        DEFAULT_PARAMS.merge(params || {}).each do |k, v|
          setter = "#{k}="
          public_send(setter, v) if respond_to?(setter)
        end
        self
      end
    end

    class SSLSocket
      def fileno
        @io.fileno
      end
      alias to_i fileno

      def puts(*objs)
        if objs.empty?
          write("\n")
        else
          objs.each do |o|
            s = o.to_s
            write(s.end_with?("\n") ? s : "#{s}\n")
          end
        end
        nil
      end

      def print(*objs)
        objs.each { |o| write(o.to_s) }
        nil
      end
      alias << write

      def peeraddr
        @io.peeraddr
      end

      def addr
        @io.addr
      end
    end

    class SSLServer
      attr_accessor :start_immediately

      def initialize(svr, ctx)
        @svr = svr
        @ctx = ctx
        @start_immediately = true
      end

      def to_io
        @svr
      end

      def listen(backlog = 5)
        @svr.listen(backlog)
        0
      end

      def addr
        @svr.addr
      end

      def setsockopt(*args)
        @svr.setsockopt(*args)
      end

      def accept
        sock = @svr.accept
        begin
          ssl = SSLSocket.new(sock, @ctx)
          ssl.sync_close = true
          ssl.accept if @start_immediately
          ssl
        rescue
          sock.close rescue nil
          raise
        end
      end

      def close
        @svr.close
      end

      def closed?
        @svr.closed?
      end
    end
  end

  class Digest
    class DigestError < OpenSSLError; end

    ALGORITHMS = {
      "MD5" => "MD5", "SHA1" => "SHA1", "SHA256" => "SHA256",
      "SHA384" => "SHA384", "SHA512" => "SHA512"
    }

    def self.__impl(name)
      key = name.to_s.upcase.delete("-")
      cls = ALGORITHMS[key]
      raise RuntimeError, "Unsupported digest algorithm (#{name}).: unknown object name" unless cls
      ::Digest.const_get(cls)
    end

    attr_reader :name

    def initialize(name, data = nil)
      if OpenSSL::Digest === name
        @name = name.name
      elsif String === name
        @name = name.upcase.delete("-")
      else
        raise TypeError, "wrong argument type #{name.class} (expected String or OpenSSL::Digest)"
      end
      @impl = Digest.__impl(@name).new
      @impl.update(data) if data
    end

    def update(data)
      @impl.update(data)
      self
    end
    alias << update

    def reset
      @impl.reset
      self
    end

    def digest(data = nil)
      return Digest.__impl(@name).digest(data) if data
      @impl.digest
    end

    def hexdigest(data = nil)
      return Digest.__impl(@name).hexdigest(data) if data
      @impl.hexdigest
    end

    def base64digest(data = nil)
      [digest(data)].pack("m0")
    end

    def digest_length
      @impl.digest_length
    end

    def block_length
      @impl.block_length
    end

    def self.digest(name, data)
      new(name).digest(data)
    end

    def self.hexdigest(name, data)
      new(name).hexdigest(data)
    end

    def self.base64digest(name, data)
      new(name).base64digest(data)
    end

    ALGORITHMS.each_key do |algo|
      klass = Class.new(self) do
        define_method(:initialize) { |data = nil| super(algo, data) }
      end
      const_set(algo, klass)
      klass.define_singleton_method(:digest) { |data| new.digest(data) }
      klass.define_singleton_method(:hexdigest) { |data| new.hexdigest(data) }
      klass.define_singleton_method(:base64digest) { |data| new.base64digest(data) }
    end
  end

  class HMACError < OpenSSLError; end

  class HMAC
    def self.__name(digest)
      digest.is_a?(OpenSSL::Digest) ? digest.name : digest.to_s
    end

    def self.digest(digest, key, data)
      OpenSSL.__hmac(__name(digest), key, data)
    end

    def self.hexdigest(digest, key, data)
      digest(digest, key, data).unpack1("H*")
    end

    def self.base64digest(digest, key, data)
      [digest(digest, key, data)].pack("m0")
    end

    def initialize(key, digest)
      @key = key
      @name = HMAC.__name(digest)
      @data = "".b
    end

    def update(data)
      @data << data
      self
    end
    alias << update

    def reset
      @data = "".b
      self
    end

    def digest
      OpenSSL.__hmac(@name, @key, @data)
    end

    def hexdigest
      digest.unpack1("H*")
    end
    alias to_s hexdigest
    alias inspect hexdigest

    def base64digest
      [digest].pack("m0")
    end

    def ==(other)
      other.is_a?(HMAC) && digest == other.digest
    end
  end

  module Random
    class RandomError < OpenSSLError; end

    # Limit each request to 16 MiB to avoid unbounded allocation.
    def self.random_bytes(n)
      unless n.is_a?(Integer) || n.is_a?(Float)
        unless n.respond_to?(:to_int, true)
          name = n.nil? ? "nil" : (n.equal?(true) || n.equal?(false) ? n.to_s : n.class.to_s)
          message = n.nil? ? "no implicit conversion from nil to integer" : "no implicit conversion of #{name} into Integer"
          raise TypeError, message
        end
        original = n
        n = n.__send__(:to_int)
        unless n.is_a?(Integer)
          raise TypeError, "can't convert #{original.class} into Integer (#{original.class}#to_int gives #{n.class})"
        end
      end
      OpenSSL.__random_bytes(n)
    end
  end

  def self.fixed_length_secure_compare(a, b)
    OpenSSL.__fixed_length_secure_compare(a, b)
  end

  def self.secure_compare(a, b)
    OpenSSL.__secure_compare(a, b)
  end

  class Cipher
    class AES < Cipher
      def initialize(*args)
        case args.length
        when 1 then super("AES-#{args[0]}-CBC")
        when 2 then super("AES-#{args[0]}-#{args[1]}")
        else raise ArgumentError, "wrong number of arguments"
        end
      end
    end

    class AES128 < Cipher
      def initialize(mode = "CBC"); super("AES-128-#{mode}"); end
    end

    class AES192 < Cipher
      def initialize(mode = "CBC"); super("AES-192-#{mode}"); end
    end

    class AES256 < Cipher
      def initialize(mode = "CBC"); super("AES-256-#{mode}"); end
    end
  end

  module ASN1
    TAG_CLASSES = {
      UNIVERSAL: 0,
      APPLICATION: 1,
      CONTEXT_SPECIFIC: 2,
      PRIVATE: 3,
    }.freeze

    class ASN1Data
      attr_accessor :value, :tag, :tag_class

      def initialize(value, tag, tag_class = :UNIVERSAL)
        @value = value
        @tag = tag
        @tag_class = tag_class
      end

      def to_der
        tc = TAG_CLASSES[@tag_class] || 0
        if @value.is_a?(Array)
          body = +"".b
          @value.each { |el| body << el.to_der }
          ASN1.__encode_elem(tc, @tag, true, body)
        else
          ASN1.__encode_elem(tc, @tag, false, @value.to_s.b)
        end
      end
    end

    class Primitive < ASN1Data
      def initialize(value, tag = nil, tagging = nil, tag_class = :UNIVERSAL)
        super(value, tag || self.class::TAG, tag_class)
      end
    end

    class Constructive < ASN1Data
      include Enumerable

      def initialize(value, tag = nil, tagging = nil, tag_class = :UNIVERSAL)
        super(value, tag || self.class::TAG, tag_class)
      end

      def each(&block)
        @value.each(&block)
      end
    end

    class Boolean < Primitive
      TAG = 1
      def to_der
        ASN1.__encode_elem(0, 1, false, @value ? "\xFF".b : "\x00".b)
      end
    end

    class Integer < Primitive
      TAG = 2
      def initialize(value, tag = nil, tagging = nil, tag_class = :UNIVERSAL)
        v = value.is_a?(OpenSSL::BN) ? value : OpenSSL::BN.new(value)
        super(v, tag || 2, tagging, tag_class)
      end

      def to_der
        ASN1.__encode_int(@value)
      end
    end

    class BitString < Primitive
      TAG = 3
      attr_accessor :unused_bits

      def initialize(value, tag = nil, tagging = nil, tag_class = :UNIVERSAL)
        @unused_bits = 0
        super(value, tag || 3, tagging, tag_class)
      end

      def to_der
        ASN1.__encode_elem(0, 3, false, (@unused_bits || 0).chr.b + @value.to_s.b)
      end
    end

    class OctetString < Primitive
      TAG = 4
    end

    class Null < Primitive
      TAG = 5
      def initialize(value = nil, tag = nil, tagging = nil, tag_class = :UNIVERSAL)
        super(nil, tag || 5, tagging, tag_class)
      end
      def to_der
        "\x05\x00".b
      end
    end

    class ObjectId < Primitive
      TAG = 6
      alias oid value
      def to_der
        ASN1.__encode_oid(@value.to_s)
      end
    end

    class UTF8String < Primitive; TAG = 12; end
    class PrintableString < Primitive; TAG = 19; end
    class IA5String < Primitive; TAG = 22; end
    class Sequence < Constructive; TAG = 16; end
    class Set < Constructive; TAG = 17; end
  end
end

class Integer
  def to_bn
    OpenSSL::BN.new(self)
  end
end
