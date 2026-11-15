class PrettyPrint
  def initialize(output = "", maxwidth = 79, newline = "\n")
    @output = output
    @maxwidth = maxwidth.to_i
    @newline = newline.to_s
    @indent = 0
    @tokens = []
    @column = 0
  end

  def text(object, width = nil)
    value = object.to_s
    @tokens << [:text, value, width.nil? ? value.length : width.to_i]
    self
  end

  def breakable(separator = " ", width = nil)
    value = separator.to_s
    @tokens << [:break, value, width.nil? ? value.length : width.to_i, @indent]
    self
  end

  def group(indent = 0, open_object = nil, close_object = nil)
    text(open_object) unless open_object.nil?
    nest(indent) { yield self } if block_given?
    text(close_object) unless close_object.nil?
    self
  end

  def nest(indent)
    @indent += indent.to_i
    begin
      yield self if block_given?
    ensure
      @indent -= indent.to_i
    end
    self
  end

  def seplist(list, separator = nil)
    separator ||= proc { breakable }
    first = true
    list.each do |item|
      unless first
        separator.respond_to?(:call) ? separator.call : text(separator)
      end
      first = false
      yield item if block_given?
    end
    self
  end

  def flush
    column = @column
    @tokens.each_with_index do |token, index|
      case token[0]
      when :text
        write(token[1])
        column += token[2]
      when :break
        if column + token[2] + flat_remaining(index + 1) > @maxwidth
          write(@newline + (" " * token[3]))
          column = token[3]
        else
          write(token[1])
          column += token[2]
        end
      when :newline
        write(@newline + (" " * token[1]))
        column = token[1]
      end
    end
    @column = column
    @tokens.clear
    self
  end

  def self.format(output = "", maxwidth = 79, newline = "\n")
    printer = new(output, maxwidth, newline)
    yield printer if block_given?
    printer.flush
    output
  end

  def self.singleline_format(output = "", maxwidth = 79, newline = "\n")
    printer = new(output, 1 << 60, newline)
    yield printer if block_given?
    printer.flush
    output
  end

  private

  def flat_remaining(start)
    length = 0
    index = start
    while index < @tokens.length
      token = @tokens[index]
      break if token[0] == :newline
      length += token[2]
      index += 1
    end
    length
  end

  def write(value)
    if @output.respond_to?(:write)
      @output.write(value)
    else
      @output << value
    end
  end
end