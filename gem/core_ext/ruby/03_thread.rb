# Thread extensions for local storage
class Thread
  def [](key)
    @__locals ||= {}
    @__locals[key.to_s]
  end
  def []=(key, val)
    @__locals ||= {}
    @__locals[key.to_s] = val
  end
  def key?(key)
    @__locals ||= {}
    @__locals.key?(key.to_s)
  end
  def keys
    @__locals ||= {}
    @__locals.keys.map(&:to_sym)
  end

  # thread variables (not fiber-local); keys are symbols as in MRI
  def thread_variable_get(key)
    (@__thread_vars ||= {})[key.to_sym]
  end

  def thread_variable_set(key, value)
    raise FrozenError, "can't modify frozen thread locals" if frozen?
    (@__thread_vars ||= {})[key.to_sym] = value
  end

  def thread_variable?(key)
    (@__thread_vars ||= {}).key?(key.to_sym)
  end

  def thread_variables
    (@__thread_vars ||= {}).keys
  end
end

# Ruby names the synchronization classes under Thread as well
class Thread
  Queue = ::Queue
  SizedQueue = ::SizedQueue
end

# oruby runs one Ruby thread, so a locked mutex is owned by the current thread
class Mutex
  def owned?
    locked?
  end
end

class Thread
  # only the current thread exists: raising in it raises here
  def raise(*args, **kw)
    ::Kernel.raise(*args, **kw) if equal?(Thread.current)
    ::Kernel.raise ThreadError, "killed thread"
  end
end
