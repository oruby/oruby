module Ed25519
  VERSION = "1.4.0"
  KEY_SIZE = 32
  SIGNATURE_SIZE = 64

  VerifyError = Class.new(StandardError)
  SelfTestFailure = Class.new(StandardError)

  class << self
    attr_accessor :provider
  end

  self.provider = Ed25519::Provider::Ref10

  module_function

  def validate_key_bytes(key_bytes)
    raise TypeError, "expected String, got #{key_bytes.class}" unless key_bytes.is_a?(String)
    return true if key_bytes.bytesize == KEY_SIZE

    raise ArgumentError, "expected #{KEY_SIZE}-byte String, got #{key_bytes.bytesize}"
  end

  def self_test
    signature_key = Ed25519::SigningKey.new("A" * 32)
    unless signature_key.verify_key.to_bytes.unpack1("H*") == "db995fe25169d141cab9bbba92baa01f9f2e1ece7df4cb2ac05190f37fcc1f9d"
      raise SelfTestFailure, "failed to generate verify key correctly"
    end

    message = "crypto libraries should self-test on boot"
    signature = signature_key.sign(message)
    unless signature.unpack1("H*") == "c62c12a3a6cbfa04800d4be81468ef8aecd152a6a26a81d91257baecef13ba209531fe905a843e833c8b71cee04400fa2af3a29fef1152ece470421848758d0a"
      raise SelfTestFailure, "failed to generate correct signature"
    end

    verify_key = signature_key.verify_key
    raise SelfTestFailure, "failed to verify a valid signature" unless verify_key.verify(signature, message)

    bad_signature = "#{signature.byteslice(0, 63)}X"
    ex = nil
    begin
      verify_key.verify(bad_signature, message)
    rescue Ed25519::VerifyError => ex
    end
    raise SelfTestFailure, "failed to detect an invalid signature" unless ex.is_a?(Ed25519::VerifyError)
    true
  end

  class SigningKey
    attr_reader :seed, :keypair, :verify_key

    def self.generate
      new(Ed25519::Provider::Ref10.__random_seed)
    end

    def self.from_keypair(keypair)
      raise TypeError, "expected String, got #{keypair.class}" unless keypair.is_a?(String)
      raise ArgumentError, "expected 64-byte String, got #{keypair.bytesize}" unless keypair.bytesize == 64

      key = new(keypair.byteslice(0, KEY_SIZE))
      raise ArgumentError, "corrupt keypair" unless keypair.byteslice(KEY_SIZE, KEY_SIZE) == key.verify_key.to_bytes
      key
    end

    def initialize(seed)
      Ed25519.validate_key_bytes(seed)
      @seed = seed
      @keypair = Ed25519.provider.create_keypair(seed)
      @verify_key = VerifyKey.new(@keypair.byteslice(32, 32))
    end

    def sign(message)
      Ed25519.provider.sign(@keypair, message)
    end

    def inspect
      to_s
    end

    def to_bytes
      @seed
    end
    alias to_str to_bytes
  end

  class VerifyKey
    def initialize(key)
      Ed25519.validate_key_bytes(key)
      @key_bytes = key
    end

    def verify(signature, message)
      if !signature.is_a?(String)
        raise TypeError, "expected String, got #{signature.class}"
      end
      if signature.bytesize != SIGNATURE_SIZE
        raise ArgumentError, "expected #{SIGNATURE_SIZE} byte signature, got #{signature.bytesize}"
      end
      return true if Ed25519.provider.verify(@key_bytes, signature, message)

      raise VerifyError, "signature verification failed!"
    end

    def to_bytes
      @key_bytes
    end
    alias to_str to_bytes

    def inspect
      "#<#{self.class}:#{@key_bytes.unpack1('H*')}>"
    end
  end
end

Ed25519.self_test
