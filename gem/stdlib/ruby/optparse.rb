class OptionParser
  class ParseError < ArgumentError
  end

  def initialize(*args)
    @switches = {}
    yield self if block_given?
  end

  def on(*specifications, &handler)
    flags = specifications.select { |spec| spec.is_a?(String) && spec.start_with?("-") }
    raise ArgumentError, "no option switches given" if flags.empty?
    types = specifications.reject { |spec| spec.is_a?(String) }
    if types.length > 1 || types.any? { |type| ![Integer, Float, String].include?(type) }
      raise NotImplementedError, "unsupported option conversion type"
    end
    type = types.first

    long = flags.find { |flag| flag.start_with?("--") }
    key_source = long || flags.first
    key_source = key_source.split(" ", 2).first.split("[=", 2).first.split("=", 2).first
    key_source = key_source.start_with?("--") ? key_source[2..-1] : key_source[1..-1]
    negated = key_source.start_with?("[no-]")
    key_source = key_source.sub("[no-]", "") if negated
    key = key_source.to_sym

    flags.each do |flag|
      raw_name = flag.split(" ", 2).first
      optional_argument = raw_name.include?("[=")
      name = raw_name.split("[=", 2).first.split("=", 2).first
      takes_argument = optional_argument || flag.include?(" ") || raw_name.include?("=")
      if name.include?("[no-]")
        positive = name.sub("[no-]", "")
        @switches[positive] = [key, true, takes_argument, handler, optional_argument, type]
        @switches[positive.sub("--", "--no-")] = [key, false, takes_argument, handler, optional_argument, type]
      else
        @switches[name] = [key, !name.start_with?("--no-"), takes_argument, handler, optional_argument, type]
      end
    end
    self
  end

  def parse(arguments = ARGV, into: nil)
    parse_arguments(arguments.dup, into, false)
  end

  def parse!(arguments = ARGV, into: nil)
    parse_arguments(arguments, into, false)
  end

  def order(arguments = ARGV, into: nil)
    parse_arguments(arguments.dup, into, true)
  end

  def order!(arguments = ARGV, into: nil)
    parse_arguments(arguments, into, true)
  end

  private

  def parse_arguments(arguments, into, stop_at_argument)
    unless arguments.is_a?(Array)
      raise TypeError, "arguments must be an Array"
    end
    unless into.nil? || into.is_a?(Hash)
      raise TypeError, "into must be a Hash"
    end

    index = 0
    while index < arguments.length
      argument = arguments[index].to_s
      if argument == "--"
        arguments.delete_at(index)
        break
      end
      unless argument.start_with?("-") && argument != "-"
        break if stop_at_argument
        index += 1
        next
      end

      switch_name, inline_value = argument.split("=", 2)
      option = @switches[switch_name]
      if option.nil?
        option = @switches.find do |name, details|
          details[2] && name.start_with?("-") && !name.start_with?("--") &&
            argument.start_with?(name) && argument.length > name.length
        end
        if option
          switch_name, option = option
          inline_value = argument[switch_name.length..-1]
        end
      end
      raise ParseError, "invalid option: #{argument}" unless option

      key, value, takes_argument, handler, optional_argument, type = option
      if takes_argument
        if inline_value.nil?
          if optional_argument
            inline_value = nil
            consumed = 1
          else
            raise ParseError, "missing argument: #{switch_name}" if index + 1 >= arguments.length
            inline_value = arguments[index + 1].to_s
            consumed = 2
          end
        else
          consumed = 1
        end
        value = inline_value
      else
        raise ParseError, "unexpected argument: #{argument}" unless inline_value.nil?
        consumed = 1
      end

      value = convert_argument(value, type)
      value = handler.call(value) if handler
      into[key] = value if into
      arguments.slice!(index, consumed)
    end
    arguments
  end

  def convert_argument(value, type)
    return value if type.nil? || value.nil? || type == String

    if type == Integer
      Integer(value)
    elsif type == Float
      Float(value)
    else
      raise NotImplementedError, "unsupported option conversion type"
    end
  rescue StandardError
    raise ParseError, "invalid argument: #{value}"
  end
end