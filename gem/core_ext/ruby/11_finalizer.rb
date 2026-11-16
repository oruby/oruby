module ObjectSpace
  @finalizers = {}

  def self.define_finalizer(obj, callable = nil, &block)
    callable ||= block
    raise ArgumentError, "callable or block required" if callable.nil? || !callable.respond_to?(:call)
    case obj
    when Symbol, Numeric, true, false, nil
      raise ArgumentError, "cannot define finalizer on #{obj.class}"
    end

    warn_self = false
    if callable.is_a?(Method) && callable.receiver.equal?(obj)
      warn_self = true
    elsif callable.is_a?(Proc)
      caller_recv = _caller_receiver rescue nil
      if caller_recv && caller_recv.equal?(obj)
        warn_self = true
      end
    end
    warn "warning: finalizer references object to be finalized" if warn_self

    id = obj.object_id
    @finalizers[id] ||= []
    existing = @finalizers[id].find { |f| f == callable }
    if existing
      return [0, existing]
    end

    @finalizers[id] << callable
    [0, callable]
  end

  def self.undefine_finalizer(obj)
    raise FrozenError, "can't modify frozen #{obj.class}" if obj.frozen?
    @finalizers.delete(obj.object_id)
    obj
  end

  def self._finalizers
    @finalizers ||= {}
  end

  def self._run_finalizers
    return unless @finalizers
    until @finalizers.empty?
      keys = @finalizers.keys
      keys.each do |k|
        procs = @finalizers.delete(k) || []
        procs.each do |pr|
          begin
            pr.call(k)
          rescue SystemExit
          rescue Exception => e
            warn "warning: Exception in finalizer: #{e.message} (#{e.class})" unless $VERBOSE.nil?
          end
        end
      end
    end
  end
end

at_exit { ObjectSpace._run_finalizers }

class Object
  alias __orig_dup__ dup
  def dup
    copy = __orig_dup__
    fins = ObjectSpace._finalizers[object_id]
    if fins
      fins.each { |f| ObjectSpace.define_finalizer(copy, f) }
    end
    copy
  end

  alias __orig_clone__ clone
  def clone(*args)
    copy = __orig_clone__(*args)
    fins = ObjectSpace._finalizers[object_id]
    if fins
      fins.each { |f| ObjectSpace.define_finalizer(copy, f) }
    end
    copy
  end
end
