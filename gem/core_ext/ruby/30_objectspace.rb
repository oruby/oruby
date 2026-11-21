module ObjectSpace
  class << self
    alias_method :__oruby_each_object, :each_object
    # without a block: an Enumerator, whose #each answers the count
    def each_object(*args, &blk)
      return to_enum(:each_object, *args) unless blk
      __oruby_each_object(*args, &blk)
    end
  end
end
