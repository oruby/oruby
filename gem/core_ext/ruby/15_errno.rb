# Errno and SystemCallError MRI compatibility

module Errno
  def self.const_defined?(name, inherit = true)
    return true if __errno_defined?(name)
    super(name, inherit)
  end

  if const_defined?(:EAGAIN, false) && const_defined?(:EWOULDBLOCK, false)
    if Errno::EAGAIN::Errno == Errno::EWOULDBLOCK::Errno && !Errno::EAGAIN.equal?(Errno::EWOULDBLOCK)
      remove_const(:EWOULDBLOCK)
      EWOULDBLOCK = EAGAIN
    end
  end

  if const_defined?(:EOPNOTSUPP, false)
    if const_defined?(:ENOTSUP, false)
      if Errno::ENOTSUP::Errno == Errno::EOPNOTSUPP::Errno && !Errno::ENOTSUP.equal?(Errno::EOPNOTSUPP)
        remove_const(:ENOTSUP)
        ENOTSUP = EOPNOTSUPP
      end
    else
      ENOTSUP = EOPNOTSUPP
    end
  end
end

class SystemCallError
  alias_method :__sce_initialize, :initialize
  private :__sce_initialize

  def initialize(*args)
    if instance_of?(SystemCallError) || !self.class.const_defined?(:Errno)
      if args.size == 0 || args.size > 3
        raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 1..3)"
      end
      if args.size == 1
        a0 = args[0]
        if a0.nil? || String === a0 || Integer === a0
          return __sce_initialize(a0)
        elsif Float === a0
          return __sce_initialize(a0.to_i)
        elsif a0.respond_to?(:to_int)
          return __sce_initialize(Kernel.__to_int(a0))
        else
          return __sce_initialize(Kernel.__implicit(a0, String))
        end
      end
      msg = args[0].nil? ? nil : Kernel.__implicit(args[0], String)
      eno_arg = args[1]
      if eno_arg.nil?
        eno = nil
      elsif Integer === eno_arg
        eno = eno_arg
      elsif Float === eno_arg
        eno = eno_arg.to_i
      elsif Complex === eno_arg
        raise RangeError, "can't convert #{eno_arg} into Integer" unless eno_arg.imaginary == 0
        eno = eno_arg.real.to_i
      else
        eno = Kernel.__to_int(eno_arg)
      end
      loc = args.size == 3 && !args[2].nil? ? args[2].to_s : nil
      if loc
        base = eno ? SystemCallError.new(nil, eno).message : "unknown error"
        full = msg ? "#{base} @ #{loc} - #{msg}" : "#{base} @ #{loc}"
        __sce_initialize(full, eno)
        Exception.instance_method(:initialize).bind_call(self, full)
        return self
      end
      return __sce_initialize(msg, eno)
    end

    eno = self.class::Errno
    return __sce_initialize(*args) unless eno.is_a?(Integer) && eno > 0
    if args.size > 2
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..2)"
    end
    m = args[0]
    m = Kernel.__implicit(m, String) unless m.nil?
    loc = args.size == 2 && !args[1].nil? ? args[1].to_s : nil
    str = SystemCallError.new(nil, eno).message
    str = "#{str} @ #{loc}" if loc
    str = "#{str} - #{m}" unless m.nil?
    Exception.instance_method(:initialize).bind_call(self, str)
  end
  private :initialize
end
