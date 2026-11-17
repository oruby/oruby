# ObjectSpace::WeakMap and ObjectSpace::WeakKeyMap. mruby has no weak
# references, so the entries are held strongly; the API and key semantics
# are MRI's.
module ObjectSpace
  class WeakMap
    include Enumerable

    def initialize
      @map = {}.compare_by_identity
    end

    def []=(key, value)
      @map[key] = value
    end

    def [](key)
      @map[key]
    end

    def key?(key)
      @map.key?(key)
    end
    alias include? key?
    alias member? key?

    def delete(key)
      return @map.delete(key) if @map.key?(key)
      block_given? ? yield(key) : nil
    end

    def size
      @map.size
    end
    alias length size

    def keys
      @map.keys
    end

    def values
      @map.values
    end

    def each(&block)
      return self if @map.empty?
      raise LocalJumpError, "no block given (yield)" unless block
      @map.each { |k, v| block.call(k, v) }
      self
    end
    alias each_pair each

    def each_key
      return self if @map.empty?
      raise LocalJumpError, "no block given (yield)" unless block_given?
      @map.each_key { |k| yield k }
      self
    end

    def each_value
      return self if @map.empty?
      raise LocalJumpError, "no block given (yield)" unless block_given?
      @map.each_value { |v| yield v }
      self
    end

    def inspect
      ins = Kernel.instance_method(:inspect)
      show = ->(o) { Kernel.instance_method(:kind_of?).bind_call(o, Kernel) ? o.inspect : ins.bind_call(o) }
      head = Kernel.instance_method(:to_s).bind_call(self).chomp(">")
      return "#{head}>" if @map.empty?
      "#{head}: " + @map.map { |k, v| "#{show.(k)} => #{show.(v)}" }.join(", ") + ">"
    end
  end

  class WeakKeyMap
    def initialize
      @buckets = {}
      @size = 0
    end

    def __collectable?(key)
      !(key.nil? || key == true || key == false || Integer === key || Float === key || Symbol === key)
    end
    private :__collectable?

    # [bucket, index] of key; keys are compared by #hash, then identity or #eql?
    def __find(key)
      bucket = @buckets[key.__send__(:hash)]
      return nil unless bucket
      bucket.each_with_index do |(k, _), i|
        return [bucket, i] if k.equal?(key) || key.eql?(k)
      end
      nil
    end
    private :__find

    def []=(key, value)
      raise ArgumentError, "WeakKeyMap keys must be garbage collectable" unless __collectable?(key)
      h = key.__send__(:hash)
      if (found = __find(key))
        found[0][found[1]][1] = value
      else
        (@buckets[h] ||= []) << [key, value]
        @size += 1
      end
      value
    end

    def [](key)
      return nil unless __collectable?(key)
      found = __find(key)
      found ? found[0][found[1]][1] : nil
    end

    def key?(key)
      return false unless __collectable?(key)
      !__find(key).nil?
    end

    def getkey(key)
      return nil unless __collectable?(key)
      found = __find(key)
      found ? found[0][found[1]][0] : nil
    end

    def delete(key)
      found = __collectable?(key) ? __find(key) : nil
      unless found
        return block_given? ? yield(key) : nil
      end
      @size -= 1
      found[0].delete_at(found[1])[1]
    end

    def clear
      @buckets = {}
      @size = 0
      self
    end

    def inspect
      Kernel.instance_method(:to_s).bind_call(self).chomp(">") + " size=#{@size}>"
    end
  end
end
