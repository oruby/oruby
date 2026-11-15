class Matrix
  class ErrDimensionMismatch < StandardError
  end

  def initialize(rows)
    raise TypeError, "rows must be an Array" unless rows.is_a?(Array)
    width = rows.empty? ? 0 : rows[0].length
    unless rows.all? { |row| row.is_a?(Array) && row.length == width }
      raise ArgumentError, "rows must have the same size"
    end
    @rows = rows.map { |row| row.dup }
  end

  def self.rows(rows, copy = true)
    new(copy ? rows.map { |row| row.dup } : rows)
  end

  def self.columns(columns)
    rows(columns).transpose
  end

  def self.[](*rows)
    new(rows)
  end

  def self.build(row_count, column_count = row_count)
    rows = Array.new(row_count.to_i) do |row|
      Array.new(column_count.to_i) do |column|
        block_given? ? yield(row, column) : 0
      end
    end
    new(rows)
  end

  def self.zero(row_count, column_count = row_count)
    build(row_count, column_count) { 0 }
  end

  def self.identity(size)
    build(size, size) { |row, column| row == column ? 1 : 0 }
  end

  def row_count
    @rows.length
  end

  def column_count
    @rows.empty? ? 0 : @rows[0].length
  end

  def [](row, column = nil)
    column.nil? ? @rows[row] : @rows[row][column]
  end

  def row(index)
    Vector.new(@rows[index])
  end

  def column(index)
    Vector.new(@rows.map { |row| row[index] })
  end

  def to_a
    @rows.map { |row| row.dup }
  end

  def transpose
    Matrix.rows(Array.new(column_count) { |column| @rows.map { |row| row[column] } })
  end

  def +(other)
    check_matrix_dimensions(other)
    Matrix.rows(Array.new(row_count) do |row|
      Array.new(column_count) { |column| @rows[row][column] + other[row, column] }
    end)
  end

  def -(other)
    check_matrix_dimensions(other)
    Matrix.rows(Array.new(row_count) do |row|
      Array.new(column_count) { |column| @rows[row][column] - other[row, column] }
    end)
  end

  def *(other)
    if other.is_a?(Matrix)
      raise ErrDimensionMismatch, "matrix dimensions do not match" unless column_count == other.row_count
      return Matrix.rows(Array.new(row_count) do |row|
        Array.new(other.column_count) do |column|
          (0...column_count).inject(0) do |sum, index|
            sum + @rows[row][index] * other[index, column]
          end
        end
      end)
    elsif other.is_a?(Vector)
      raise ErrDimensionMismatch, "matrix and vector dimensions do not match" unless column_count == other.size
      return Vector.new(Array.new(row_count) do |row|
        (0...column_count).inject(0) do |sum, index|
          sum + @rows[row][index] * other[index]
        end
      end)
    elsif other.is_a?(Numeric)
      return Matrix.rows(@rows.map { |row| row.map { |value| value * other } })
    end
    raise TypeError, "unsupported matrix multiplier"
  end

  def determinant
    raise ErrDimensionMismatch, "matrix must be square" unless row_count == column_count
    return 1 if row_count == 0
    return @rows[0][0] if row_count == 1

    @rows[0].each_with_index.inject(0) do |sum, pair|
      value = pair[0]
      column = pair[1]
      minor = @rows[1..-1].map do |row|
        row.each_with_index.reject { |_, index| index == column }.map { |pair| pair[0] }
      end
      sign = column.even? ? 1 : -1
      sum + sign * value * Matrix.rows(minor).determinant
    end
  end

  def ==(other)
    other.is_a?(Matrix) && @rows == other.to_a
  end

  private

  def check_matrix_dimensions(other)
    unless other.is_a?(Matrix) && row_count == other.row_count && column_count == other.column_count
      raise ErrDimensionMismatch, "matrix dimensions do not match"
    end
  end
end

class Vector
  include Enumerable

  def initialize(elements)
    raise TypeError, "elements must be an Array" unless elements.is_a?(Array)
    @elements = elements.dup
  end

  def self.[](*elements)
    new(elements)
  end

  def size
    @elements.length
  end

  alias length size

  def [](index)
    @elements[index]
  end

  def to_a
    @elements.dup
  end

  def each(&block)
    return enum_for(:each) unless block
    @elements.each(&block)
    self
  end

  def +(other)
    check_vector_dimensions(other)
    Vector.new(@elements.each_with_index.map { |value, index| value + other[index] })
  end

  def -(other)
    check_vector_dimensions(other)
    Vector.new(@elements.each_with_index.map { |value, index| value - other[index] })
  end

  def *(other)
    if other.is_a?(Numeric)
      Vector.new(@elements.map { |value| value * other })
    elsif other.is_a?(Vector)
      dot(other)
    else
      raise TypeError, "unsupported vector multiplier"
    end
  end

  def dot(other)
    check_vector_dimensions(other)
    @elements.each_with_index.inject(0) do |sum, pair|
      sum + pair[0] * other[pair[1]]
    end
  end

  def ==(other)
    other.is_a?(Vector) && @elements == other.to_a
  end

  private

  def check_vector_dimensions(other)
    unless other.is_a?(Vector) && size == other.size
      raise Matrix::ErrDimensionMismatch, "vector dimensions do not match"
    end
  end
end