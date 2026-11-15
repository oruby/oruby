require "stringio"

class CSV
  VERSION = "3.3.2"

  include Enumerable

  class MalformedCSVError < RuntimeError
    attr_reader :line_number
    alias lineno line_number

    def initialize(message, line_number)
      @line_number = line_number
      super("#{message} in line #{line_number}.")
    end
  end

  class InvalidEncodingError < MalformedCSVError; end

  DEFAULT_OPTIONS = {
    col_sep: ",",
    row_sep: :auto,
    quote_char: '"',
    field_size_limit: nil,
    converters: nil,
    unconverted_fields: nil,
    headers: false,
    return_headers: false,
    header_converters: nil,
    skip_blanks: false,
    force_quotes: false,
    skip_lines: nil,
    liberal_parsing: false,
    quote_empty: true,
    write_headers: nil,
    write_nil_value: nil,
    write_empty_value: "",
    strip: false,
    nil_value: nil,
    empty_value: ""
  }.freeze

  Converters = {
    integer: ->(f) { Integer(f) rescue f },
    float: ->(f) { Float(f) rescue f },
    numeric: [:integer, :float],
    date: ->(f) { begin; require "date"; f =~ /\A\s*\d/ ? Date.parse(f) : f; rescue; f; end },
    date_time: ->(f) { begin; require "date"; f =~ /\A\s*\d/ ? DateTime.parse(f) : f; rescue; f; end },
    all: [:date_time, :numeric]
  }

  HeaderConverters = {
    downcase: ->(h) { h.downcase },
    symbol: ->(h) { h.encode("UTF-8").downcase.gsub(/[^\s\w]+/, "").strip.gsub(/\s+/, "_").to_sym },
    symbol_raw: ->(h) { h.to_sym }
  }

  class Row
    include Enumerable

    def initialize(headers, fields, header_row = false)
      @header_row = header_row
      headers.each { |h| h.freeze if h.is_a?(String) }
      @row = if fields.size >= headers.size
               headers.zip(fields)
             else
               fields.zip(headers).map(&:reverse)
             end
    end

    attr_reader :row
    protected :row

    def header_row?
      @header_row
    end

    def field_row?
      !header_row?
    end

    def headers
      @row.map(&:first)
    end

    def fields(*headers_and_or_ranges)
      if headers_and_or_ranges.empty?
        @row.map(&:last)
      else
        headers_and_or_ranges.map { |h| field(h) }
      end
    end
    alias values_at fields

    def field(header_or_index, minimum_index = 0)
      finder = (header_or_index.is_a?(Integer) || header_or_index.is_a?(Range)) ? :[] : :assoc
      pair = @row[minimum_index..-1].public_send(finder, header_or_index)
      if header_or_index.is_a?(Range)
        pair.map(&:last)
      else
        finder == :assoc ? (pair.nil? ? nil : pair.last) : (pair.nil? ? nil : pair.last)
      end
    end
    alias [] field

    def fetch(header, *varargs)
      raise ArgumentError, "Too many arguments" if varargs.length > 1
      pair = @row.assoc(header)
      if pair
        pair.last
      elsif block_given?
        yield header
      elsif varargs.empty?
        raise KeyError, "key not found: #{header}"
      else
        varargs.first
      end
    end

    def has_key?(header)
      !!@row.assoc(header)
    end
    alias include? has_key?
    alias key? has_key?
    alias member? has_key?
    alias header? has_key?

    def []=(*args)
      value = args.pop
      if args.first.is_a?(Integer)
        if @row[args.first].nil?
          @row[args.first] = [nil, value]
        else
          @row[args.first][1] = value
        end
      else
        index = index(*args)
        if index.nil?
          self << [args.first, value]
        else
          @row[index][1] = value
        end
      end
    end

    def <<(arg)
      if arg.is_a?(Array) && arg.size == 2
        @row << arg
      elsif arg.is_a?(Hash)
        arg.each { |pair| @row << pair }
      else
        @row << [nil, arg]
      end
      self
    end

    def push(*args)
      args.each { |arg| self << arg }
      self
    end

    def delete(header_or_index, minimum_index = 0)
      if header_or_index.is_a?(Integer)
        @row.delete_at(header_or_index)
      elsif (i = index(header_or_index, minimum_index))
        @row.delete_at(i)
      else
        []
      end
    end

    def delete_if(&block)
      return enum_for(__method__) { size } unless block_given?
      @row.delete_if(&block)
      self
    end

    def index(header, minimum_index = 0)
      index = headers[minimum_index..-1].index(header)
      index.nil? ? nil : index + minimum_index
    end

    def field?(data)
      fields.include?(data)
    end

    def each(&block)
      return enum_for(__method__) { size } unless block_given?
      @row.each(&block)
      self
    end
    alias each_pair each

    def size
      @row.size
    end
    alias length size

    def empty?
      @row.empty?
    end

    def ==(other)
      return @row == other.row if other.is_a?(CSV::Row)
      @row == other
    end

    def to_h
      hash = {}
      @row.each do |key, _value|
        hash[key] = self[key] unless hash.key?(key)
      end
      hash
    end
    alias to_hash to_h

    def deconstruct_keys(keys)
      keys ? to_h.slice(*keys) : to_h
    end

    def deconstruct
      fields
    end

    alias to_ary to_a

    def dig(index_or_header, *indexes)
      value = field(index_or_header)
      if value.nil?
        nil
      elsif indexes.empty?
        value
      else
        value.dig(*indexes)
      end
    end

    def to_csv(**options)
      fields.to_csv(**options)
    end
    alias to_s to_csv

    def initialize_copy(other)
      super
      @row = other.instance_variable_get(:@row).map(&:dup)
    end

    def inspect
      str = ["#<", self.class.to_s]
      each do |header, field|
        str << " " << (header.is_a?(Symbol) ? header.to_s : header.inspect) << ":" << field.inspect
      end
      str << ">"
      str.join("")
    end
  end

  class Table
    include Enumerable

    def initialize(array_of_rows, headers: nil)
      @table = array_of_rows
      @headers = headers
      unless @headers
        @headers = @table.empty? ? [] : @table.first.headers
      end
      @mode = :col_or_row
    end

    attr_reader :mode, :table
    protected :table

    def by_col; self.class.new(@table.dup).by_col!; end
    def by_col!; @mode = :col; self; end
    def by_col_or_row; self.class.new(@table.dup).by_col_or_row!; end
    def by_col_or_row!; @mode = :col_or_row; self; end
    def by_row; self.class.new(@table.dup).by_row!; end
    def by_row!; @mode = :row; self; end

    def headers
      @table.empty? ? @headers.dup : @table.first.headers
    end

    def [](index_or_header)
      if @mode == :row || (@mode == :col_or_row && (index_or_header.is_a?(Integer) || index_or_header.is_a?(Range)))
        @table[index_or_header]
      else
        @table.map { |row| row[index_or_header] }
      end
    end

    def []=(index_or_header, value)
      if @mode == :row || (@mode == :col_or_row && index_or_header.is_a?(Integer))
        @table[index_or_header] = value.is_a?(Array) ? Row.new(headers, value) : value
      else
        unless index_or_header.is_a?(Integer)
          index = headers.index(index_or_header) || headers.size
          @table.each_with_index do |row, i|
            row[index_or_header] = value.is_a?(Array) ? value[i] : value
          end
        end
      end
    end

    def values_at(*indices_or_headers)
      if @mode == :row || (@mode == :col_or_row && indices_or_headers.all? { |i| i.is_a?(Integer) })
        @table.values_at(*indices_or_headers)
      else
        @table.map { |row| row.values_at(*indices_or_headers) }
      end
    end

    def <<(row_or_array)
      if row_or_array.is_a?(Array)
        @table << Row.new(headers, row_or_array)
      else
        @table << row_or_array
      end
      self
    end

    def push(*rows)
      rows.each { |row| self << row }
      self
    end

    def delete(*indexes_or_headers)
      deleted = indexes_or_headers.map do |ih|
        if @mode == :row || (@mode == :col_or_row && ih.is_a?(Integer))
          @table.delete_at(ih)
        else
          @table.map { |row| row.delete(ih).last }
        end
      end
      indexes_or_headers.size == 1 ? deleted[0] : deleted
    end

    def delete_if(&block)
      return enum_for(__method__) { @mode == :row || @mode == :col_or_row ? size : headers.size } unless block_given?
      if @mode == :row || @mode == :col_or_row
        @table.delete_if(&block)
      else
        headers.each { |h| delete(h) if yield([h, self[h]]) }
      end
      self
    end

    def each(&block)
      return enum_for(__method__) { @mode == :col ? headers.size : size } unless block_given?
      if @mode == :col
        headers.each { |header| yield([header, self[header]]) }
      else
        @table.each(&block)
      end
      self
    end

    def ==(other)
      return @table == other.table if other.is_a?(CSV::Table)
      @table == other
    end

    def to_a
      array = [headers]
      @table.each { |row| array.push(row.fields) unless row.header_row? }
      array
    end

    def to_csv(write_headers: true, limit: nil, **options)
      array = write_headers ? [headers.to_csv(**options)] : []
      limit ||= @table.size
      limit = @table.size + 1 + limit if limit < 0
      limit = 0 if limit < 0
      @table.first(limit).each { |row| array.push(row.fields.to_csv(**options)) unless row.header_row? }
      array.join("")
    end
    alias to_s to_csv

    def dig(index_or_header, *index_or_headers)
      value = self[index_or_header]
      if value.nil? || index_or_headers.empty?
        value
      else
        value.dig(*index_or_headers)
      end
    end

    def size
      @table.size
    end
    alias length size

    def inspect
      "#<#{self.class} mode:#{@mode} row_count:#{to_a.size}>"
    end
  end

  # -- class API -----------------------------------------------------------

  def self.instance(data = $stdout, **options)
    new(data, **options)
  end

  def self.foreach(path, mode = "r", **options, &block)
    return to_enum(__method__, path, mode, **options) unless block_given?
    open(path, mode, **options) do |csv|
      csv.each(&block)
    end
  end

  def self.generate(str = nil, **options)
    str = str ? str : +""
    csv = new(StringIO.new(str), **options)
    csv.instance_variable_set(:@__gen_io, true)
    yield csv
    csv.string
  end

  def self.generate_line(row, **options)
    options = { row_sep: $INPUT_RECORD_SEPARATOR || "\n" }.merge(options)
    str = +""
    csv = new(StringIO.new(str), **options)
    csv << row
    str
  end

  def self.generate_lines(rows, **options)
    generate(**options) do |csv|
      rows.each { |row| csv << row }
    end
  end

  def self.open(filename, mode = "r", **options)
    file_opts = {}
    mode = mode.to_s
    f = File.open(filename, mode)
    begin
      csv = new(f, **options)
    rescue Exception => e
      f.close
      raise e
    end
    if block_given?
      begin
        yield csv
      ensure
        csv.close
      end
    else
      csv
    end
  end

  def self.parse(str, **options, &block)
    csv = new(str, **options)
    return csv.each(&block) if block_given?
    csv.read
  end

  def self.parse_line(line, **options)
    new(line, **options).each.first
  end

  def self.read(path, **options)
    open(path, **options) { |csv| csv.read }
  end

  def self.readlines(path, **options)
    read(path, **options)
  end

  def self.table(path, **options)
    default_options = { headers: true, converters: :numeric, header_converters: :symbol }
    read(path, **default_options.merge(options))
  end

  # -- instance ------------------------------------------------------------

  attr_reader :col_sep, :row_sep, :quote_char, :field_size_limit, :converters, :header_converters,
              :skip_lines, :encoding
  attr_reader :lineno, :line

  def initialize(data = "", col_sep: ",", row_sep: :auto, quote_char: '"', field_size_limit: nil,
                 max_field_size: nil, converters: nil, unconverted_fields: nil, headers: false,
                 return_headers: false, write_headers: nil, header_converters: nil, skip_blanks: false,
                 force_quotes: false, skip_lines: nil, liberal_parsing: false, internal_encoding: nil,
                 external_encoding: nil, encoding: nil, nil_value: nil, empty_value: "", strip: false,
                 quote_empty: true, write_converters: nil, write_nil_value: nil, write_empty_value: "")
    raise ArgumentError, "Cannot parse nil as CSV" if data.nil?
    @io = data.is_a?(String) ? StringIO.new(data) : data
    @string_input = data.is_a?(String)
    @col_sep = col_sep.to_s
    raise ArgumentError, ":col_sep must be 1 or more characters: #{col_sep.inspect}" if @col_sep.empty?
    @row_sep = row_sep
    @quote_char = quote_char.nil? ? nil : quote_char.to_s
    if @quote_char && @quote_char.length != 1
      raise ArgumentError, ":quote_char has to be nil or a single character String"
    end
    @field_size_limit = field_size_limit
    @field_size_limit = max_field_size + 1 if max_field_size
    @converters = []
    add_converters(@converters, Converters, converters)
    @header_converters = []
    add_converters(@header_converters, HeaderConverters, header_converters)
    @unconverted_fields = unconverted_fields
    @use_headers = headers
    @return_headers = return_headers
    @write_headers = write_headers
    @skip_blanks = skip_blanks
    @force_quotes = force_quotes
    @skip_lines = skip_lines.is_a?(String) ? Regexp.new(Regexp.escape(skip_lines)) : skip_lines
    if @skip_lines && !@skip_lines.respond_to?(:match)
      raise ArgumentError, ":skip_lines has to respond to \#match: #{skip_lines.inspect}"
    end
    @liberal_parsing = liberal_parsing
    @nil_value = nil_value
    @empty_value = empty_value
    @strip = strip
    @quote_empty = quote_empty
    @write_nil_value = write_nil_value
    @write_empty_value = write_empty_value
    @headers = nil
    @headers = headers if headers.is_a?(Array)
    @headers = parse_headers_string(headers) if headers.is_a?(String)
    @lineno = 0
    @line = nil
    @eof = false
    @encoding = Encoding::UTF_8
    @header_written = false
  end

  def string
    @io.string
  end

  def to_io
    @io
  end

  def binmode?
    false
  end

  def headers
    @headers || (@use_headers ? true : nil)
  end

  def return_headers?
    @return_headers
  end

  def write_headers?
    @write_headers
  end

  def skip_blanks?
    @skip_blanks
  end

  def force_quotes?
    @force_quotes
  end

  def liberal_parsing?
    @liberal_parsing
  end

  def header_row?
    @use_headers && @headers.nil?
  end

  def eof?
    @io.eof?
  end
  alias eof eof?

  def rewind
    @io.rewind
    @lineno = 0
    @headers = nil unless @use_headers.is_a?(Array)
  end

  def close
    @io.close
  end

  def close_read
    @io.close_read
  end

  def close_write
    @io.close_write
  end

  def closed?
    @io.closed?
  end

  def flush
    @io.flush if @io.respond_to?(:flush)
  end

  def path
    @io.respond_to?(:path) ? @io.path : nil
  end

  def convert(name = nil, &converter)
    add_converters(@converters, Converters, name || converter)
  end

  def header_convert(name = nil, &converter)
    add_converters(@header_converters, HeaderConverters, name || converter)
  end

  def <<(row)
    if @use_headers && !@header_written && @write_headers
      hdrs = @headers.is_a?(Array) ? @headers : (row.is_a?(Row) ? row.headers : (row.is_a?(Hash) ? row.keys : nil))
      @io << format_line(hdrs) if hdrs
      @header_written = true
    end
    fields = case row
             when Row then row.fields
             when Hash
               hdrs = @headers.is_a?(Array) ? @headers : row.keys
               @headers ||= hdrs
               hdrs.map { |h| row[h] }
             else row
             end
    @io << format_line(fields)
    @lineno += 1
    self
  end
  alias add_row <<
  alias puts <<

  def each(&block)
    return to_enum(__method__) unless block_given?
    while (row = shift)
      yield row
    end
    self
  end

  def read
    rows = to_a
    if @use_headers
      Table.new(rows, headers: @headers.is_a?(Array) ? @headers : [])
    else
      rows
    end
  end
  alias readlines read

  def shift
    loop do
      fields = parse_record
      return nil if fields.nil?
      if fields.empty? || fields == [nil]
        next if @skip_blanks
        fields = [] if fields == [nil]
      end
      if @skip_lines && @line && @skip_lines.match(@line.chomp)
        next
      end
      if @use_headers
        if @headers.nil?
          @headers = fields.map { |h| convert_header(h) }
          if @return_headers
            return Row.new(@headers, fields, true)
          end
          next
        end
        return Row.new(@headers, convert_fields(fields))
      end
      return convert_fields(fields)
    end
  end
  alias gets shift
  alias readline shift

  def inspect
    "#<CSV io_type:#{@io.class} lineno:#{@lineno} col_sep:#{@col_sep.inspect} row_sep:#{resolved_row_sep.inspect} quote_char:#{@quote_char.inspect}>"
  end

  private

  def parse_headers_string(str)
    CSV.parse_line(str, col_sep: @col_sep, quote_char: @quote_char)
  end

  def add_converters(list, table, name)
    return if name.nil?
    Array(name).flatten.each do |n|
      if n.respond_to?(:call)
        list << n
      else
        conv = table[n.to_sym]
        raise ArgumentError, "unknown converter name: #{n.inspect}" if conv.nil?
        if conv.is_a?(Array)
          add_converters(list, table, conv)
        else
          list << conv
        end
      end
    end
  end

  def convert_header(h)
    return h if h.nil?
    @header_converters.each do |c|
      h = c.arity == 1 || c.arity == -1 ? c.call(h) : c.call(h, nil)
      break unless h.is_a?(String)
    end
    h
  end

  def convert_fields(fields)
    return fields if @converters.empty?
    fields.each_with_index.map do |f, i|
      next f if f.nil?
      @converters.each do |c|
        break unless f.is_a?(String)
        f = c.arity == 1 || c.arity == -1 ? c.call(f) : c.call(f, nil)
      end
      f
    end
  end

  def resolved_row_sep
    return @resolved_row_sep if @resolved_row_sep
    if @row_sep == :auto
      sample = @io.respond_to?(:string) ? @io.string : nil
      @resolved_row_sep = if sample && sample.include?("\r\n")
                            "\r\n"
                          elsif sample && sample.include?("\r") && !sample.include?("\n")
                            "\r"
                          else
                            $INPUT_RECORD_SEPARATOR || "\n"
                          end
    else
      @resolved_row_sep = @row_sep.to_s
    end
  end

  def read_line
    sep = resolved_row_sep
    l = @io.gets(sep)
    @line = l
    l
  end

  def parse_record
    line = read_line
    return nil if line.nil?
    @lineno += 1
    sep = resolved_row_sep
    fields = []
    field = nil
    quoted = false
    in_quotes = false
    i = 0
    q = @quote_char
    cs = @col_sep
    buf = line.dup
    buf = buf[0...-sep.size] if buf.end_with?(sep)
    full = buf
    while true
      if in_quotes
        j = full.index(q, i)
        if j.nil?
          nl = read_line
          raise MalformedCSVError.new("Unclosed quoted field", @lineno) if nl.nil?
          field << full[i..-1] << sep
          full = nl.end_with?(sep) ? nl[0...-sep.size] : nl
          i = 0
          next
        end
        field << full[i...j]
        if full[j + 1] == q
          field << q
          i = j + 2
          next
        end
        in_quotes = false
        i = j + 1
        if i < full.size && !full[i..-1].start_with?(cs)
          if @liberal_parsing
            k = full.index(cs, i) || full.size
            field = q + field + q + full[i...k]
            i = k
          else
            raise MalformedCSVError.new("Any value after quoted field isn't allowed", @lineno)
          end
        end
        next
      end
      if i >= full.size
        fields << finish_field(field, quoted)
        break
      end
      if full[i..-1].start_with?(cs)
        fields << finish_field(field, quoted)
        field = nil
        quoted = false
        i += cs.size
        if i >= full.size
          fields << finish_field(nil, false)
          break
        end
        next
      end
      if q && full[i] == q && field.nil?
        field = +""
        quoted = true
        in_quotes = true
        i += 1
        next
      end
      k = full.index(cs, i) || full.size
      chunk = full[i...k]
      if q && chunk.include?(q) && !@liberal_parsing
        raise MalformedCSVError.new("Illegal quoting", @lineno)
      end
      if chunk.include?("\r") || chunk.include?("\n")
        raise MalformedCSVError.new("Unquoted fields do not allow new line <#{(chunk.include?("\r") ? "\\r" : "\\n")}>", @lineno)
      end
      field = (field || +"") << chunk
      i = k
    end
    fields = [] if fields == [nil] && line.chomp(sep).empty?
    fields
  end

  def finish_field(field, quoted)
    if field.nil?
      @nil_value
    elsif field.empty? && quoted
      @empty_value
    else
      field = field.strip if @strip == true
      field = field.delete(@strip) if @strip.is_a?(String)
      field
    end
  end

  def format_line(fields)
    sep = @row_sep == :auto ? ($INPUT_RECORD_SEPARATOR || "\n") : @row_sep.to_s
    fields.map { |f| quote_field(f) }.join(@col_sep) + sep
  end

  def quote_field(field)
    if field.nil?
      field = @write_nil_value
      return "" if field.nil?
    elsif field.respond_to?(:empty?) && field.empty?
      field = @write_empty_value
    end
    field = String(field)
    if @force_quotes
      quote(field)
    elsif field.empty?
      @quote_empty ? quote(field) : ""
    elsif @quote_char && (field.include?(@quote_char) || field.include?(@col_sep) || field.include?("\n") ||
                          field.include?("\r") || field.start_with?(" ") || field.end_with?(" "))
      field.start_with?(" ") || field.end_with?(" ") ? (field.match?(/[,"\r\n]/) || field.include?(@col_sep) ? quote(field) : field) : quote(field)
    else
      field
    end
  end

  def quote(field)
    q = @quote_char || '"'
    q + field.gsub(q, q + q) + q
  end
end

class Array
  def to_csv(**options)
    CSV.generate_line(self, **options)
  end
end

class String
  def parse_csv(**options)
    CSV.parse_line(self, **options)
  end
end

def CSV(*args, **options, &block)
  CSV.instance(*args, **options, &block)
end
