# Minimal OpenSSL: SSL constants/errors for net/http (TLS is Go crypto/tls),
# OpenSSL::Digest and OpenSSL::HMAC over Go crypto.
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
      @name = name.to_s.upcase.delete("-")
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
    def self.random_bytes(n)
      ::Random.bytes(n)
    end
  end

  def self.fixed_length_secure_compare(a, b)
    OpenSSL.__fixed_length_secure_compare(a, b)
  end

  def self.secure_compare(a, b)
    OpenSSL.__secure_compare(a, b)
  end
end
