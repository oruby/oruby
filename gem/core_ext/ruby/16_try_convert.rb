# Class.try_convert for core classes missing it in mruby
class Hash
  def self.try_convert(obj)
    return obj if obj.is_a?(Hash)
    return nil unless obj.respond_to?(:to_hash)
    r = obj.to_hash
    return r if r.nil? || r.is_a?(Hash)
    raise TypeError, "can't convert #{obj.class} to Hash (#{obj.class}#to_hash gives #{r.class})"
  end unless respond_to?(:try_convert)
end

class String
  def self.try_convert(obj)
    return obj if obj.is_a?(String)
    return nil unless obj.respond_to?(:to_str)
    r = obj.to_str
    return r if r.nil? || r.is_a?(String)
    raise TypeError, "can't convert #{obj.class} to String (#{obj.class}#to_str gives #{r.class})"
  end unless respond_to?(:try_convert)
end

class Integer
  def self.try_convert(obj)
    return obj if obj.is_a?(Integer)
    return nil unless obj.respond_to?(:to_int)
    r = obj.to_int
    return r if r.nil? || r.is_a?(Integer)
    raise TypeError, "can't convert #{obj.class} to Integer (#{obj.class}#to_int gives #{r.class})"
  end unless respond_to?(:try_convert)
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
