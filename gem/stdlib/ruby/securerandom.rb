module SecureRandom
  extend Random::Formatter

  def self.random_bytes(n = nil)
    n = n ? n.to_int : 16
    bytes(n)
  end
end
