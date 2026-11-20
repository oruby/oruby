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
    # mruby's dup calls initialize_copy; MRI calls initialize_dup, which
    # calls it: a custom one runs now, its super not copying twice
    if Kernel.__oruby_own_hook?(copy, :initialize_dup)
      Kernel.__oruby_copied[copy.__id__] = true
      begin
        copy.__send__(:initialize_dup, self)
      ensure
        Kernel.__oruby_copied.delete(copy.__id__)
      end
    end
    fins = ObjectSpace._finalizers[object_id]
    if fins
      fins.each { |f| ObjectSpace.define_finalizer(copy, f) }
    end
    copy
  end

  # MRI's rb_obj_clone2: freeze: nil keeps the frozen state, true freezes
  # the copy, false leaves it mutable; with true or false a custom
  # initialize_clone gets freeze: as a keyword. Numerics and immediates are
  # their own clones.
  alias __orig_clone__ clone
  def clone(freeze: nil)
    unless freeze.nil? || true.equal?(freeze) || false.equal?(freeze)
      raise ArgumentError, "unexpected value for freeze: #{Kernel.__cname(freeze)}"
    end
    if Numeric === self || nil.equal?(self) || true.equal?(self) || false.equal?(self) || Symbol === self
      raise ArgumentError, "can't unfreeze #{Kernel.__cname(self)}" if false.equal?(freeze)
      return self
    end
    copy = __orig_clone__
    custom = Kernel.__oruby_own_hook?(copy, :initialize_clone)
    copy.__oruby_clone_unfreeze if !freeze.nil? && copy.frozen?
    if custom
      Kernel.__oruby_copied[copy.__id__] = true
      begin
        freeze.nil? ? copy.__send__(:initialize_clone, self) : copy.__send__(:initialize_clone, self, freeze: freeze)
      ensure
        Kernel.__oruby_copied.delete(copy.__id__)
      end
    end
    copy.freeze if freeze
    fins = ObjectSpace._finalizers[object_id]
    if fins
      fins.each { |f| ObjectSpace.define_finalizer(copy, f) }
    end
    copy
  end
end
