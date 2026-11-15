class OpenStruct
  VERSION = "0.6.1"

  def initialize(hash = nil)
    @table = {}
    if hash
      update_to_values!(hash)
    end
  end

  private def update_to_values!(hash)
    hash.each_pair do |k, v|
      set_ostruct_member_value!(k, v)
    end
  end

  def initialize_clone(orig)
    super
    @table = @table.dup
  end

  def initialize_dup(orig)
    super
    @table = @table.dup
  end

  def initialize_copy(orig)
    super
    @table = orig.instance_variable_get(:@table).dup
  end

  def to_h(&block)
    if block
      @table.to_h(&block)
    else
      @table.dup
    end
  end

  def each_pair
    return to_enum(__method__) { @table.size } unless block_given?
    @table.each_pair { |p| yield p }
    self
  end

  def marshal_dump
    @table
  end

  def marshal_load(hash)
    @table = hash
  end

  private def new_ostruct_member!(name)
    name = name.to_sym
    unless @table.key?(name) || is_method_protected!(name)
      getter = name
      setter = :"#{name}="
      define_singleton_method(getter) { @table[getter] }
      define_singleton_method(setter) { |x| @table[getter] = x }
    end
  end

  private def is_method_protected!(name)
    if !respond_to?(name, true)
      false
    elsif name.to_s.end_with?("!")
      true
    else
      owner = method(name).owner
      if owner.class == ::Class
        owner < ::OpenStruct
      else
        self.class.ancestors.any? do |mod|
          return false if mod == ::OpenStruct
          mod == owner
        end
      end
    end
  end

  def freeze
    @table.freeze
    super
  end

  private def set_ostruct_member_value!(name, value)
    name = name.to_sym
    new_ostruct_member!(name)
    @table[name] = value
  end

  def method_missing(mid, *args)
    len = args.length
    mname = mid.to_s.end_with?("=") ? mid.to_s.chomp("=") : nil
    if mname
      if len != 1
        raise ArgumentError, "wrong number of arguments (given #{len}, expected 1)", caller(1)
      end
      raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
      set_ostruct_member_value!(mname, args[0])
    elsif len == 0
      @table[mid]
    else
      begin
        super
      rescue NoMethodError => err
        raise NoMethodError.new("undefined method '#{mid}' for an instance of #{self.class}", mid, args)
      end
    end
  end

  def respond_to_missing?(mid, include_private = false)
    mname = mid.to_s.chomp("=").to_sym
    @table&.key?(mname) || super
  end

  def [](name)
    @table[name.to_sym]
  end

  def []=(name, value)
    name = name.to_sym
    raise FrozenError, "can't modify frozen #{self.class}: #{inspect}" if frozen?
    set_ostruct_member_value!(name, value)
  end

  def dig(name, *names)
    begin
      name = name.to_sym
    rescue NoMethodError
      raise TypeError, "#{name} is not a symbol nor a string"
    end
    @table.dig(name, *names)
  end

  def delete_field(name, &block)
    sym = name.to_sym
    begin
      singleton_class.remove_method(sym, "#{sym}=")
    rescue NameError
    end
    @table.delete(sym) do
      return yield if block
      raise NameError.new("no field '#{sym}' in #{self}", sym)
    end
  end

  InspectKey = :__inspect_key__

  def inspect
    ids = (Thread.current[InspectKey] ||= [])
    if ids.include?(object_id)
      detail = " ..."
    else
      ids << object_id
      begin
        detail = @table.map do |key, value|
          " #{key}=#{value.inspect}"
        end.join(",")
      ensure
        ids.pop
      end
    end
    ["#<", self.class.to_s, detail, ">"].join
  end
  alias to_s inspect

  attr_reader :table
  alias table! table
  protected :table!

  def ==(other)
    return false unless other.kind_of?(OpenStruct)
    @table == other.table!
  end

  def eql?(other)
    return false unless other.kind_of?(OpenStruct)
    @table.eql?(other.table!)
  end

  def hash
    @table.hash
  end
end
