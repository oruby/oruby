# Process.kill as MRI's rb_f_kill: the signal is an Integer, or a name
# (String, Symbol, #to_str) where a leading "-" signals process groups. A
# signal this process sends to itself is handled before kill returns.
module Process
  def self.respond_to?(name, include_all = false)
    s = name.to_sym
    return false if s == :daemon || s == :_fork
    super
  end

  def self.kill(sig, *pids)
    raise ArgumentError, "wrong number of arguments (given 1, expected 2+)" if pids.empty?
    signo = Integer === sig ? sig : Signal.__oruby_signo(sig, true, false)
    pids = pids.map { |pid| Integer === pid ? pid : Signal.send(:__int, pid) }
    to_self = false
    pids.each { |pid| to_self = true if __oruby_kill__(signo, pid) }
    Signal.__oruby_deliver__(signo.abs) if to_self && signo != 0
    pids.size
  end
end
