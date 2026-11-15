require "monitor"

class Logger
  VERSION = "1.6.4"
  ProgName = "#{name}/#{VERSION}"

  module Severity
    DEBUG = 0
    INFO = 1
    WARN = 2
    ERROR = 3
    FATAL = 4
    UNKNOWN = 5

    LEVELS = { "debug" => DEBUG, "info" => INFO, "warn" => WARN, "error" => ERROR, "fatal" => FATAL, "unknown" => UNKNOWN }

    def self.coerce(severity)
      if severity.is_a?(Integer)
        severity
      else
        key = severity.to_s.downcase
        LEVELS[key] || raise(ArgumentError, "invalid log level: #{severity}")
      end
    end
  end
  include Severity

  class Error < RuntimeError; end
  class ShiftingError < Error; end

  class Formatter
    Format = "%.1s, [%s #%d] %5s -- %s: %s\n"
    DatetimeFormat = "%Y-%m-%dT%H:%M:%S.%6N"

    attr_accessor :datetime_format

    def initialize
      @datetime_format = nil
    end

    def call(severity, time, progname, msg)
      sprintf(Format, severity, format_datetime(time), Process.pid, severity, progname, msg2str(msg))
    end

    private

    def format_datetime(time)
      time.strftime(@datetime_format || DatetimeFormat)
    end

    def msg2str(msg)
      case msg
      when ::String
        msg
      when ::Exception
        "#{msg.message} (#{msg.class})\n#{(msg.backtrace || []).join("\n")}"
      else
        msg.inspect
      end
    end
  end

  class LogDevice
    include MonitorMixin

    attr_reader :dev, :filename

    def initialize(log = nil, shift_age: nil, shift_size: nil, shift_period_suffix: nil, binmode: false, reraise_write_errors: [], skip_header: false)
      @dev = @filename = nil
      @binmode = binmode
      @reraise_write_errors = reraise_write_errors
      @skip_header = skip_header
      mon_initialize
      set_dev(log)
    end

    def write(message)
      mon_synchronize do
        begin
          @dev.write(message)
        rescue *@reraise_write_errors => e
          raise e
        rescue Exception => ignored
          warn("log writing failed. #{ignored}")
        end
      end
    end

    def close
      mon_synchronize do
        begin
          @dev.close rescue nil
        rescue Exception
          @dev.close rescue nil
        end
      end
    end

    def reopen(log = nil)
      mon_synchronize do
        if @filename && @dev
          @dev.close rescue nil
        end
        set_dev(log || @filename)
      end
      self
    end

    private

    def set_dev(log)
      if log.respond_to?(:write) && log.respond_to?(:close)
        @dev = log
        @filename = log.respond_to?(:path) ? (log.path rescue nil) : nil
      else
        @dev = open_logfile(log)
        @dev.sync = true
        @filename = log
      end
    end

    def open_logfile(filename)
      if File.exist?(filename)
        File.open(filename, "a")
      else
        logdev = File.open(filename, "a")
        logdev.sync = true
        unless @skip_header
          logdev.write("# Logfile created on #{Time.now} by #{Logger::ProgName}\n")
        end
        logdev
      end
    end
  end

  attr_reader :level
  attr_accessor :progname, :formatter
  attr_reader :datetime_format

  def level=(severity)
    @level = Severity.coerce(severity)
  end

  def datetime_format=(datetime_format)
    @default_formatter.datetime_format = datetime_format
  end

  def datetime_format
    @default_formatter.datetime_format
  end

  def debug?; level <= DEBUG; end
  def info?; level <= INFO; end
  def warn?; level <= WARN; end
  def error?; level <= ERROR; end
  def fatal?; level <= FATAL; end

  def debug!; self.level = DEBUG; end
  def info!; self.level = INFO; end
  def warn!; self.level = WARN; end
  def error!; self.level = ERROR; end
  def fatal!; self.level = FATAL; end

  def initialize(logdev, shift_age = 0, shift_size = 1048576, level: DEBUG, progname: nil, formatter: nil,
                 datetime_format: nil, binmode: false, shift_period_suffix: "%Y%m%d", reraise_write_errors: [],
                 skip_header: false)
    self.level = level
    self.progname = progname
    @default_formatter = Formatter.new
    self.datetime_format = datetime_format
    self.formatter = formatter
    @logdev = nil
    @level_override = {}
    if logdev && logdev != File::NULL
      @logdev = LogDevice.new(logdev, shift_age: shift_age, shift_size: shift_size, binmode: binmode,
                                      reraise_write_errors: reraise_write_errors, skip_header: skip_header)
    end
  end

  def reopen(logdev = nil)
    @logdev&.reopen(logdev)
    self
  end

  def add(severity, message = nil, progname = nil)
    severity ||= UNKNOWN
    return true if @logdev.nil? || severity < level
    progname ||= @progname
    if message.nil?
      if block_given?
        message = yield
      else
        message = progname
        progname = @progname
      end
    end
    @logdev.write(format_message(format_severity(severity), Time.now, progname, message))
    true
  end
  alias log add

  def <<(msg)
    @logdev&.write(msg)
  end

  def debug(progname = nil, &block); add(DEBUG, nil, progname, &block); end
  def info(progname = nil, &block); add(INFO, nil, progname, &block); end
  def warn(progname = nil, &block); add(WARN, nil, progname, &block); end
  def error(progname = nil, &block); add(ERROR, nil, progname, &block); end
  def fatal(progname = nil, &block); add(FATAL, nil, progname, &block); end
  def unknown(progname = nil, &block); add(UNKNOWN, nil, progname, &block); end

  def close
    @logdev&.close
  end

  private

  SEV_LABEL = %w[DEBUG INFO WARN ERROR FATAL ANY].freeze

  def format_severity(severity)
    SEV_LABEL[severity] || "ANY"
  end

  def format_message(severity, datetime, progname, msg)
    (@formatter || @default_formatter).call(severity, datetime, progname, msg)
  end
end
