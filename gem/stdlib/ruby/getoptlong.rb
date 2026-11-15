class GetoptLong
  VERSION = "0.2.1"

  ORDERINGS = [REQUIRE_ORDER = 0, PERMUTE = 1, RETURN_IN_ORDER = 2]
  ARGUMENT_FLAGS = [NO_ARGUMENT = 0, REQUIRED_ARGUMENT = 1, OPTIONAL_ARGUMENT = 2]
  STATUS_YET, STATUS_STARTED, STATUS_TERMINATED = 0, 1, 2

  class Error < StandardError; end
  class AmbiguousOption < Error; end
  class NeedlessArgument < Error; end
  class MissingArgument < Error; end
  class InvalidOption < Error; end

  attr_reader :ordering, :error
  attr_accessor :quiet
  alias quiet? quiet
  alias error? error

  def initialize(*arguments)
    if ENV.include?("POSIXLY_CORRECT")
      @ordering = REQUIRE_ORDER
    else
      @ordering = PERMUTE
    end
    @canonical_names = {}
    @argument_flags = {}
    @quiet = false
    @status = STATUS_YET
    @error = nil
    @error_message = nil
    @rest_singles = ""
    @non_option_arguments = []
    set_options(*arguments) if arguments.size > 0
  end

  def ordering=(ordering)
    if @status != STATUS_YET
      set_error(ArgumentError, "argument error")
      raise RuntimeError, "invoke ordering=, but option processing has already started"
    end
    if !ORDERINGS.include?(ordering)
      raise ArgumentError, "invalid ordering `#{ordering}'"
    end
    if ordering == PERMUTE && ENV.include?("POSIXLY_CORRECT")
      @ordering = REQUIRE_ORDER
    else
      @ordering = ordering
    end
  end

  def set_options(*arguments)
    if @status != STATUS_YET
      raise RuntimeError, "invoke set_options, but option processing has already started"
    end
    @canonical_names.clear
    @argument_flags.clear
    arguments.each do |arg|
      if !arg.is_a?(Array)
        raise ArgumentError, "the option list contains non-Array argument"
      end
      argument_flag = nil
      arg.each do |i|
        if ARGUMENT_FLAGS.include?(i)
          if argument_flag != nil
            raise ArgumentError, "too many argument-flags"
          end
          argument_flag = i
        end
      end
      raise ArgumentError, "no argument-flag" if argument_flag == nil
      canonical_name = nil
      arg.each do |i|
        next if i == argument_flag
        begin
          if !i.is_a?(String) || i !~ /\A-([^-]|-.+)\z/
            raise ArgumentError, "an invalid option `#{i}'"
          end
          if @canonical_names.include?(i)
            raise ArgumentError, "option redefined `#{i}'"
          end
        rescue ArgumentError => e
          @canonical_names.clear
          @argument_flags.clear
          raise e
        end
        if canonical_name == nil
          canonical_name = i
        end
        @canonical_names[i] = canonical_name
        @argument_flags[i] = argument_flag
      end
      raise ArgumentError, "no option name" if canonical_name == nil
    end
    self
  end

  def terminate
    return nil if @status == STATUS_TERMINATED
    raise RuntimeError, "an error has occurred" if @error != nil
    @status = STATUS_TERMINATED
    @non_option_arguments.reverse_each do |argument|
      ARGV.unshift(argument)
    end
    @canonical_names = nil
    @argument_flags = nil
    @rest_singles = nil
    @non_option_arguments = nil
    self
  end

  def terminated?
    @status == STATUS_TERMINATED
  end

  def set_error(type, message)
    $stderr.print("#{$0}: #{message}\n") if !@quiet
    @error = type
    @error_message = message
    @canonical_names = nil
    @argument_flags = nil
    @rest_singles = nil
    @non_option_arguments = nil
    raise type, message
  end
  protected :set_error

  def error_message
    @error_message
  end

  def get
    option_name, option_argument = nil, ""
    return nil if @error != nil
    case @status
    when STATUS_YET
      @status = STATUS_STARTED
    when STATUS_TERMINATED
      return nil
    end
    if 0 < @rest_singles.length
      argument = "-" + @rest_singles
    elsif ARGV.length == 0
      terminate
      return nil
    elsif @ordering == PERMUTE
      while 0 < ARGV.length && ARGV[0] !~ /\A-./
        @non_option_arguments.push(ARGV.shift)
      end
      if ARGV.length == 0
        terminate
        return nil
      end
      argument = ARGV.shift
    elsif @ordering == REQUIRE_ORDER
      if ARGV[0] !~ /\A-./
        terminate
        return nil
      end
      argument = ARGV.shift
    else
      if ARGV[0] !~ /\A-./
        return "", ARGV.shift
      end
      argument = ARGV.shift
    end

    if argument == "--" && @rest_singles.length == 0
      terminate
      return nil
    end

    if argument =~ /\A(--[^=]+)/ && @rest_singles.length == 0
      pattern = $1
      if @canonical_names.include?(pattern)
        option_name = pattern
      else
        matches = []
        @canonical_names.each_key do |key|
          if key.index(pattern) == 0
            option_name = key
            matches << key
          end
        end
        if 2 <= matches.length
          set_error(AmbiguousOption, "option `#{argument}' is ambiguous between #{matches.join(', ')}")
        elsif matches.length == 0
          set_error(InvalidOption, "unrecognized option `#{argument}'")
        end
      end

      if @argument_flags[option_name] == REQUIRED_ARGUMENT
        if argument =~ /=(.*)\z/m
          option_argument = $1
        elsif 0 < ARGV.length
          option_argument = ARGV.shift
        else
          set_error(MissingArgument, "option `#{argument}' requires an argument")
        end
      elsif @argument_flags[option_name] == OPTIONAL_ARGUMENT
        if argument =~ /=(.*)\z/m
          option_argument = $1
        elsif 0 < ARGV.length && ARGV[0] !~ /\A-./
          option_argument = ARGV.shift
        else
          option_argument = ""
        end
      elsif argument =~ /=(.*)\z/m
        set_error(NeedlessArgument, "option `#{option_name}' doesn't allow an argument")
      end
    elsif argument =~ /\A(-(.))(.*)/m
      option_name, ch, @rest_singles = $1, $2, $3
      if @canonical_names.include?(option_name)
        if @argument_flags[option_name] == REQUIRED_ARGUMENT
          if 0 < @rest_singles.length
            option_argument = @rest_singles
            @rest_singles = ""
          elsif 0 < ARGV.length
            option_argument = ARGV.shift
          else
            set_error(MissingArgument, "option requires an argument -- #{ch}")
          end
        elsif @argument_flags[option_name] == OPTIONAL_ARGUMENT
          if 0 < @rest_singles.length
            option_argument = @rest_singles
            @rest_singles = ""
          elsif 0 < ARGV.length && ARGV[0] !~ /\A-./
            option_argument = ARGV.shift
          else
            option_argument = ""
          end
        end
      else
        if ENV.include?("POSIXLY_CORRECT")
          set_error(InvalidOption, "invalid option -- #{ch}")
        else
          set_error(InvalidOption, "invalid option -- #{ch}")
        end
      end
    else
      return "", argument
    end

    return @canonical_names[option_name], option_argument
  end
  alias get_option get

  def each
    loop do
      option_name, option_argument = get_option
      break if option_name == nil
      yield option_name, option_argument
    end
  end
  alias each_option each
end
