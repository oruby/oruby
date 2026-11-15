module Syslog
  module Constants
    LOG_EMERG = 0; LOG_ALERT = 1; LOG_CRIT = 2; LOG_ERR = 3
    LOG_WARNING = 4; LOG_NOTICE = 5; LOG_INFO = 6; LOG_DEBUG = 7
    LOG_KERN = 0; LOG_USER = 8; LOG_MAIL = 16; LOG_DAEMON = 24; LOG_AUTH = 32
    LOG_SYSLOG = 40; LOG_LPR = 48; LOG_NEWS = 56; LOG_UUCP = 64; LOG_CRON = 72
    LOG_AUTHPRIV = 80; LOG_FTP = 88
    LOG_LOCAL0 = 128; LOG_LOCAL1 = 136; LOG_LOCAL2 = 144; LOG_LOCAL3 = 152
    LOG_LOCAL4 = 160; LOG_LOCAL5 = 168; LOG_LOCAL6 = 176; LOG_LOCAL7 = 184
    LOG_PID = 1; LOG_CONS = 2; LOG_ODELAY = 4; LOG_NDELAY = 8; LOG_NOWAIT = 16; LOG_PERROR = 32

    def self.LOG_MASK(pri) = 1 << pri
    def self.LOG_UPTO(pri) = (1 << (pri + 1)) - 1
  end

  module Macros
    def LOG_MASK(pri) = 1 << pri
    def LOG_UPTO(pri) = (1 << (pri + 1)) - 1
    def self.LOG_MASK(pri) = 1 << pri
    def self.LOG_UPTO(pri) = (1 << (pri + 1)) - 1
  end

  include Constants
  extend Macros

  @opened = false
  @ident = nil
  @options = nil
  @facility = nil
  @mask = nil

  class << self
    def open(ident = nil, options = nil, facility = nil)
      raise RuntimeError, "syslog already open" if @opened
      ident ||= $0
      options ||= LOG_PID | LOG_CONS
      facility ||= LOG_USER
      raise TypeError, "no implicit conversion into Integer" unless options.is_a?(Integer) && facility.is_a?(Integer)
      @ident = ident.to_s
      @options = options
      @facility = facility
      @mask = 255
      __open(@ident, @facility)
      @opened = true
      if block_given?
        begin
          yield self
        ensure
          close
        end
      end
      self
    end

    def reopen(*args, &block)
      close
      open(*args, &block)
    end
    alias open! reopen

    def opened? = @opened

    def close
      raise RuntimeError, "syslog not opened" unless @opened
      __close
      @opened = false
      @ident = @options = @facility = @mask = nil
      nil
    end

    def ident = (@opened ? @ident : nil)
    def options = (@opened ? @options : nil)
    def facility = (@opened ? @facility : nil)

    def mask
      @opened ? @mask : nil
    end

    def mask=(m)
      raise RuntimeError, "must open syslog before setting log mask" unless @opened
      raise TypeError, "no implicit conversion into Integer" unless m.is_a?(Integer)
      @mask = m
    end

    def log(pri, format, *args)
      raise RuntimeError, "must open syslog before write" unless @opened
      raise TypeError, "no implicit conversion of #{pri.class} into Integer" unless pri.is_a?(Integer)
      raise TypeError, "no implicit conversion of nil into String" if format.nil?
      return self if (@mask & (1 << (pri & 7))) == 0
      msg = args.empty? ? format.to_s : format % args
      msg = "#{@ident}[#{$$}]: #{msg}".sub(/\A[^:]*: /, "") if false
      __write(pri, msg, (@options & LOG_PERROR) != 0, @options & LOG_PID != 0 ? @ident : @ident)
      self
    end

    { emerg: LOG_EMERG, alert: LOG_ALERT, crit: LOG_CRIT, err: LOG_ERR,
      warning: LOG_WARNING, notice: LOG_NOTICE, info: LOG_INFO, debug: LOG_DEBUG }.each do |name, pri|
      define_method(name) do |format, *args|
        raise RuntimeError, "must open syslog before write" unless @opened
        log(pri, format, *args)
      end
    end

    def instance = self

    def inspect
      if @opened
        "<#Syslog: opened=true, ident=\"#{@ident}\", options=#{@options}, facility=#{@facility}, mask=#{@mask}>"
      else
        "<#Syslog: opened=false>"
      end
    end
  end
end
