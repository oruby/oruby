# Process-wide hash salt to resist hash collision / DoS attacks (CVE-2011-4815)
module Kernel
  @__hash_salt = (Random.rand(1..0x3fff_ffff_ffff_ffff) rescue 0x5a5a5a5a_5a5a5a5a)

  def self.__hash_salt
    @__hash_salt
  end

  alias __orig_hash hash
  def hash
    __orig_hash ^ Kernel.__hash_salt
  end
end

[Integer, Float, Rational, Complex, String, Regexp].each do |mod|
  next unless mod.respond_to?(:instance_methods) && mod.instance_methods(false).include?(:hash)
  mod.class_eval do
    alias __orig_hash hash
    def hash
      __orig_hash ^ Kernel.__hash_salt
    end
  end
end
