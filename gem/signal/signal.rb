# Signal, SignalException and Interrupt as MRI's signal.c and error.c have
# them. OS signals arrive through Signal.__oruby_signal__, which go-signal.c
# calls at the next VM instruction after Go received the signal.
class SignalException < Exception
  def initialize(*args)
    if args.empty? || args.size > 2
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected #{args.empty? ? 1 : '1..2'})"
    end
    sig = args[0]
    if (signo = Signal.__oruby_to_int(sig))
      if signo < 0 || signo > Signal.__oruby_nsig__
        raise ArgumentError, "invalid signal number (#{signo})"
      end
      if args.size > 1
        msg = args[1]
      else
        name = Signal.signame(signo)
        msg = name ? "SIG#{name}" : "SIG#{signo}"
      end
    else
      raise ArgumentError, "wrong number of arguments (given 2, expected 1)" if args.size > 1
      signo = Signal.__oruby_signo(sig, false, false)
      msg = "SIG#{Signal.signame(signo)}"
    end
    super(msg)
    @signo = signo
  end

  attr_reader :signo

  def signm
    message
  end
end

class Interrupt < SignalException
  def initialize(*args)
    if args.size > 1
      raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)"
    end
    super(2, args.empty? ? "Interrupt" : args[0])
  end
end

module Signal
  # signals MRI handles in Ruby from the start: INT raises Interrupt, the
  # others SignalException
  RUBY_DEFAULT = %w[INT HUP QUIT TERM ALRM USR1 USR2].freeze
  RESERVED = %w[SEGV BUS ILL FPE VTALRM].freeze

  # signo => command: a callable, a String to evaluate, nil (ignored, reported
  # as nil), :ignore, :exit, or :none (no command: the handler is @func's)
  @cmd = {}
  # signo => how the OS handles it: :ruby (Ruby's own handler), :chld (MRI's
  # SIGCHLD handler: nothing visible), :ign, :dfl (OS default), :other
  @func = {}
  @defaults = false
  @exit_cmd = nil

  class << self
    def list
      __oruby_list__
    end

    def signame(signo)
      list.key(__int(signo))
    end

    def trap(sig, *cmd, &block)
      if cmd.size > 1
        raise ArgumentError, "wrong number of arguments (given #{cmd.size + 1}, expected 1..2)"
      end
      signo = __trap_signo(sig)
      if cmd.empty?
        raise ArgumentError, "tried to create Proc object without a block" unless block
        cmd = block
      else
        cmd = cmd[0]
      end
      name = signame(signo)
      raise ArgumentError, "can't trap reserved signal: SIG#{name}" if RESERVED.include?(name)
      raise Errno::EINVAL, "SIG#{name}" if name == "KILL" || name == "STOP"
      return __trap_exit(cmd) if signo == 0

      func, cmd = __handler(signo, cmd)
      prev = __report(signo)
      @cmd[signo] = cmd
      @func[signo] = func
      __os(signo, func, cmd)
      prev
    end

    # Signal.__oruby_deliver__ and the VM hook run this when signo arrives
    def __oruby_signal__(signo)
      cmd = @cmd.fetch(signo, :none)
      case cmd
      when :ignore, nil
        nil
      when :none
        return unless __func(signo) == :ruby
        exc = signo == 2 ? Interrupt.new("") : SignalException.new(signo)
        # raised where the signal arrived, without the delivery frames
        raise exc, exc.message, caller(1).reject { |l| l.include?("'Signal.__oruby_") }
      when :exit
        ::Kernel.exit
      when String
        ::Kernel.eval(cmd, TOPLEVEL_BINDING)
      else
        cmd.call(signo)
      end
    end

    def __oruby_install_defaults__
      return if @defaults
      @defaults = true
      RUBY_DEFAULT.each do |name|
        signo = list[name]
        __os(signo, :ruby, :none) if signo && !@func.key?(signo)
      end
      nil
    end

    # Integer, or Float & co. through to_int; nil for names
    def __oruby_to_int(sig)
      return sig if Integer === sig
      return nil if sig.nil? || String === sig || Symbol === sig
      return nil unless sig.respond_to?(:to_int)
      __int(sig)
    end

    # signal name (String, Symbol or #to_str) to its number, MRI's
    # signm2signo; a leading "-" negates it where negative is true (kill)
    def __oruby_signo(sig, negative, exit_ok)
      name =
        case sig
        when Symbol then sig.to_s
        when String then sig
        else
          s = __str(sig)
          raise ArgumentError, "bad signal type #{sig.class}" unless s
          s
        end
      neg = name.start_with?("-")
      if neg
        raise ArgumentError, "negative signal name: #{name}" unless negative
        name = name[1..]
      end
      base = name.start_with?("SIG") ? name[3..] : name
      signo = list[base]
      signo = nil if signo == 0 && !exit_ok
      raise ArgumentError, "unsupported signal 'SIG#{base}'" unless signo
      neg ? -signo : signo
    end

    private

    # implicit conversions (rb_to_int, rb_check_string_type)
    def __int(obj)
      return obj if Integer === obj
      cname = obj.nil? ? "nil" : obj.class
      raise TypeError, "no implicit conversion of #{cname} into Integer" unless !obj.nil? && obj.respond_to?(:to_int)
      r = obj.to_int
      return r if Integer === r
      raise TypeError, "can't convert #{cname} to Integer (#{cname}#to_int gives #{r.class})"
    end

    def __str(obj)
      return obj if String === obj
      return nil if obj.nil? || !obj.respond_to?(:to_str)
      r = obj.to_str
      return r if String === r
      raise TypeError, "can't convert #{obj.class} to String (#{obj.class}#to_str gives #{r.class})"
    end

    # MRI trap_signm
    def __trap_signo(sig)
      if Integer === sig
        raise ArgumentError, "invalid signal number (#{sig})" if sig < 0 || sig >= __oruby_nsig__
        return sig
      end
      __oruby_signo(sig, false, true)
    end

    def __func(signo)
      @func.fetch(signo) { __initial_func(signo) }
    end

    # before any trap: MRI's handlers once installed, the OS's otherwise
    def __initial_func(signo)
      @defaults ? __default_func(signo) : :dfl
    end

    # what "DEFAULT" means for signo (MRI default_handler)
    def __default_func(signo)
      name = signame(signo)
      if RUBY_DEFAULT.include?(name) then :ruby
      elsif name == "CHLD" then :chld
      elsif name == "PIPE" then :other
      else :dfl
      end
    end

    # MRI trap_handler: [func, cmd] for a command given to trap
    def __handler(signo, cmd)
      return [:ign, nil] if cmd.nil?
      str = Symbol === cmd ? cmd.to_s : __str(cmd)
      return [:ruby, cmd] unless str
      case str
      when "", "SIG_IGN", "IGNORE" then [:ign, :ignore]
      when "SYSTEM_DEFAULT" then [:dfl, :none]
      when "SIG_DFL", "DEFAULT" then [__default_func(signo), :none]
      when "EXIT" then [:ruby, :exit]
      else [:ruby, str.dup.freeze]
      end
    end

    # MRI trap's report of the previous handler
    def __report(signo)
      cmd = @cmd.fetch(signo, :none)
      case cmd
      when :none, :ignore
        case __func(signo)
        when :ign then "IGNORE"
        when :dfl then "SYSTEM_DEFAULT"
        when :ruby, :chld then "DEFAULT"
        end
      when :exit then "EXIT"
      else cmd
      end
    end

    def __os(signo, func, cmd)
      case func
      when :ruby then __oruby_os__(signo, cmd == :none ? 1 : 0)
      when :ign, :other then __oruby_os__(signo, 2)
      else __oruby_os__(signo, 3)
      end
    end

    # trap(:EXIT, ...): the handler runs at exit, before at_exit handlers
    def __trap_exit(cmd)
      prev = @exit_cmd
      str = Symbol === cmd ? cmd.to_s : __str(cmd)
      if cmd.nil? || %w[SIG_IGN IGNORE SIG_DFL DEFAULT SYSTEM_DEFAULT EXIT].include?(str) || str == ""
        @exit_cmd = nil
        $__exit_trap_proc__ = nil
      elsif str
        @exit_cmd = str.dup.freeze
        $__exit_trap_proc__ = proc { ::Kernel.eval(str, TOPLEVEL_BINDING) }
      else
        @exit_cmd = cmd
        $__exit_trap_proc__ = Proc === cmd ? cmd : proc { cmd.call(0) }
      end
      prev
    end
  end
end

module Kernel
  def trap(*args, &block)
    Signal.trap(*args, &block)
  end
  module_function :trap
end
