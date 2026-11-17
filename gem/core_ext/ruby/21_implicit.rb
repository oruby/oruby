# Implicit argument conversion for mruby core methods (see 00_convert.rb)
class String
  __implicit_args :center, Integer, String
  __implicit_args :ljust, Integer, String
  __implicit_args :rjust, Integer, String
  __implicit_args :*, Integer
  __implicit_args :[], Integer, Integer
  alias slice []
  __implicit_args :slice!, Integer, Integer
  __implicit_args :byteslice, Integer, Integer
  __implicit_args :byteindex, String, Integer
  __implicit_args :byterindex, String, Integer
  __implicit_args :index, String, Integer
  __implicit_args :rindex, String, Integer
  __implicit_args :insert, Integer, String
  __implicit_args :include?, String
  __implicit_args :+, String
  __implicit_args :chomp, String
  __implicit_args :getbyte, Integer
  __implicit_args :setbyte, Integer, Integer
  __implicit_args :split, String, Integer
  __implicit_args :tr, String, String
  __implicit_args :tr_s, String, String
  __implicit_args :count, String
  __implicit_args :delete, String
  __implicit_args :squeeze, String
  __implicit_args :casecmp, String
  __implicit_args :casecmp?, String
  __implicit_args :sum, Integer
  __implicit_args :each_line, String
  __implicit_args :partition, String
  __implicit_args :rpartition, String
  __implicit_args :delete_prefix, String
  __implicit_args :delete_suffix, String
end

class Integer
  __implicit_args :<<, Integer
  __implicit_args :>>, Integer
  __implicit_args :round, Integer
  __implicit_args :floor, Integer
  __implicit_args :ceil, Integer
  __implicit_args :truncate, Integer
  __implicit_args :digits, Integer
  __implicit_args :gcd, Integer
  __implicit_args :lcm, Integer
end

class IO
  # IO.new / IO.for_fd / IO.open: the descriptor goes through #to_int
  alias __oruby_initialize initialize
  def initialize(fd, *rest, **kw, &block)
    fd = Kernel.__implicit(fd, Integer) unless Integer === fd || IO === fd
    kw.empty? ? __oruby_initialize(fd, *rest, &block) : __oruby_initialize(fd, *rest, **kw, &block)
  end
  private :initialize
end
