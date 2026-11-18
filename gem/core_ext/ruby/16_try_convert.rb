# Class.try_convert for core classes missing it in mruby
class Hash
  def self.try_convert(obj)
    return obj if obj.is_a?(Hash)
    return nil unless obj.respond_to?(:to_hash)
    r = obj.to_hash
    return r if r.nil? || r.is_a?(Hash)
    raise TypeError, "can't convert #{Kernel.__cname(obj)} into Hash (#{Kernel.__cname(obj)}#to_hash gives #{Kernel.__cname(r)})"
  end
end

class String
  def self.try_convert(obj)
    return obj if obj.is_a?(String)
    return nil unless obj.respond_to?(:to_str)
    r = obj.to_str
    return r if r.nil? || r.is_a?(String)
    raise TypeError, "can't convert #{Kernel.__cname(obj)} into String (#{Kernel.__cname(obj)}#to_str gives #{Kernel.__cname(r)})"
  end
end

class Integer
  def self.try_convert(obj)
    return obj if obj.is_a?(Integer)
    return nil unless obj.respond_to?(:to_int)
    r = obj.to_int
    return r if r.nil? || r.is_a?(Integer)
    raise TypeError, "can't convert #{Kernel.__cname(obj)} into Integer (#{Kernel.__cname(obj)}#to_int gives #{Kernel.__cname(r)})"
  end
end

# $+ compiles to $~.__last_group
class MatchData
  def __last_group
    (size - 1).downto(1) do |i|
      v = self[i]
      return v unless v.nil?
    end
    nil
  end
end
