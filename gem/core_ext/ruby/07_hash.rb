class Hash
  def compare_by_identity
    self
  end

  def compare_by_identity?
    false
  end

  alias __orig_hash hash
  def hash
    Thread.current[:__hash_hash_stack] ||= []
    stack = Thread.current[:__hash_hash_stack]
    return :cycle if stack.include?(object_id)
    stack.push(object_id)
    has_cycle = false
    h = size & 0x3fffffff
    begin
      each do |k, v|
        kh = k.hash
        vh = v.hash
        if kh == :cycle || vh == :cycle
          has_cycle = true
          kh = 0 if kh == :cycle
          vh = 0 if vh == :cycle
        else
          kh = kh.to_int if !kh.is_a?(Integer) && (kh.respond_to?(:to_int) rescue false)
          vh = vh.to_int if !vh.is_a?(Integer) && (vh.respond_to?(:to_int) rescue false)
        end
        h = ((h * 31) + (kh.to_i & 0x3fffffff) + (vh.to_i & 0x3fffffff)) & 0x3fffffff
      end
    ensure
      stack.pop
    end
    return :cycle if has_cycle && (stack.any? || (Thread.current[:__array_hash_stack] && Thread.current[:__array_hash_stack].any?))
    return 0 if has_cycle
    h
  end
end
