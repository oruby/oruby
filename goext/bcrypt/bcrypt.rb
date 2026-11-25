module BCrypt
  class Error < StandardError; end

  module Errors
    class InvalidSalt < BCrypt::Error; end
    class InvalidHash < BCrypt::Error; end
    class InvalidCost < BCrypt::Error; end
    class InvalidSecret < BCrypt::Error; end
  end

  class Engine
    DEFAULT_COST = 12
    MIN_COST = 4
    MAX_COST = 31
    MAX_SECRET_BYTESIZE = 72
    MAX_SALT_LENGTH = 16

    private_class_method :__bc_salt, :__bc_crypt, :__random_bytes

    @cost = nil

    def self.cost
      @cost || DEFAULT_COST
    end

    def self.cost=(cost)
      @cost = cost
    end

    def self.hash_secret(secret, salt, _ = nil)
      raise Errors::InvalidSecret, "invalid secret" unless valid_secret?(secret)
      raise Errors::InvalidSalt, "invalid salt" unless valid_salt?(salt)
      secret = secret.to_s
      secret = secret.byteslice(0, MAX_SECRET_BYTESIZE) if secret.bytesize > MAX_SECRET_BYTESIZE
      __bc_crypt(secret, salt)
    end

    def self.generate_salt(cost = self.cost)
      cost = cost.to_i
      raise Errors::InvalidCost, "cost must be numeric and > 0" unless cost > 0
      cost = MIN_COST if cost < MIN_COST
      __bc_salt("$2a$", cost, __random_bytes(MAX_SALT_LENGTH))
    end

    def self.valid_salt?(salt)
      salt.is_a?(String) && /\A\$[0-9a-z]{2,}\$[0-9]{2,}\$[A-Za-z0-9.\/]{22,}\z/.match?(salt)
    end

    def self.valid_secret?(secret)
      secret.respond_to?(:to_s)
    end

    def self.calibrate(upper_time_limit_in_ms)
      (MIN_COST..(MAX_COST - 1)).each do |i|
        start_time = Time.now
        Password.create("testing testing", cost: i + 1)
        end_time = Time.now - start_time
        return i if end_time * 1_000 > upper_time_limit_in_ms
      end
    end

    def self.autodetect_cost(salt)
      salt[4..5].to_i
    end
  end

  class Password < String
    attr_reader :checksum, :salt, :version, :cost

    class << self
      def create(secret, options = {})
        cost = options[:cost] || BCrypt::Engine.cost
        raise ArgumentError if cost > BCrypt::Engine::MAX_COST
        Password.new(BCrypt::Engine.hash_secret(secret, BCrypt::Engine.generate_salt(cost)))
      end

      def valid_hash?(h)
        h.is_a?(String) && /\A\$[0-9a-z]{2}\$[0-9]{2}\$[A-Za-z0-9.\/]{53}\z/.match?(h)
      end
    end

    def initialize(raw_hash)
      unless valid_hash?(raw_hash)
        raise Errors::InvalidHash, "invalid hash"
      end
      replace(raw_hash)
      @version, @cost, @salt, @checksum = split_hash(self)
    end

    def ==(secret)
      other = BCrypt::Engine.hash_secret(secret, @salt)
      return false unless other.bytesize == bytesize
      # Constant-time byte comparison
      diff = 0
      bytesize.times { |i| diff |= getbyte(i) ^ other.getbyte(i) }
      diff == 0
    end
    alias is_password? ==

    private

    def valid_hash?(h)
      self.class.valid_hash?(h)
    end

    def split_hash(h)
      _, v, c, mash = h.split("$")
      [v, c.to_i, h[0, 29], mash[-31, 31]]
    end
  end
end
