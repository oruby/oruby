class Method
  def curry(arity = nil)
    if arity
      to_proc.curry(arity)
    else
      to_proc.curry
    end
  end

  def original_name
    name
  end
end

class UnboundMethod
  def bind_call(obj, *args, &blk)
    bind(obj).call(*args, &blk)
  end
end
