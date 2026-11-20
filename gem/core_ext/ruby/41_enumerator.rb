# Enumerator, Enumerator::Yielder, Enumerator::Chain, and Enumerator::Lazy MRI 4.1 compatibility

module Kernel
  def to_enum(meth = :each, *args, **kwd, &size_block)
    Enumerator.__for_method(self, meth, args, kwd, size_block)
  end
  alias enum_for to_enum

  alias __orig_enum_loop loop
  def loop(&block)
    return to_enum(:loop) { Float::INFINITY } unless block
    __orig_enum_loop(&block)
  end
  module_function :loop
end

class Enumerator
  class Yielder
    def to_proc
      ->(*args, **kw) { kw.empty? ? self.yield(*args) : self.yield(*args, kw) }
    end

    def <<(arg)
      self.yield(arg)
      self
    end
  end

  def self.__for_method(obj, meth, args, kwd, size_block)
    e = allocate
    e.instance_variable_set(:@obj, obj)
    e.instance_variable_set(:@meth, meth)
    e.instance_variable_set(:@args, args)
    e.instance_variable_set(:@kwd, kwd)
    e.instance_variable_set(:@fib, nil)
    e.instance_variable_set(:@dst, nil)
    e.instance_variable_set(:@lookahead, nil)
    e.instance_variable_set(:@feedvalue, nil)
    e.instance_variable_set(:@stop_exc, false)
    e.instance_variable_set(:@size_block, size_block) if size_block
    e
  end

  def initialize(size = (no_size = true; nil), &block)
    raise ArgumentError, "wrong number of arguments (given 0, expected 0..1)" if !block && no_size
    raise ArgumentError, "Enumerator.new without a block is removed" unless block
    @obj = Generator.new(&block)
    @meth = :each
    @args = []
    @kwd = {}
    @fib = nil
    @dst = nil
    @lookahead = nil
    @feedvalue = nil
    @stop_exc = false
    @size_val = no_size ? nil : size
    @size_block = nil
    self
  end

  alias __orig_enum_init_copy initialize_copy
  private :__orig_enum_init_copy
  private def initialize_copy(obj)
    __orig_enum_init_copy(obj)
    @size_val = obj.instance_variable_get(:@size_val) if obj.instance_variable_defined?(:@size_val)
    @size_block = obj.instance_variable_get(:@size_block) if obj.instance_variable_defined?(:@size_block)
    self
  end

  def size
    if instance_variable_defined?(:@size_block) && @size_block
      @size_block.call
    elsif instance_variable_defined?(:@size_val) && !@size_val.nil?
      @size_val.respond_to?(:call) ? @size_val.call : @size_val
    elsif instance_variable_defined?(:@size) && @size
      @size
    elsif defined?(@obj) && @obj && @obj.respond_to?(:size)
      @obj.size
    else
      nil
    end
  end

  alias __orig_enum_inspect inspect
  def inspect
    return "#<#{self.class}: uninitialized>" unless defined?(@obj) && !@obj.nil?
    __orig_enum_inspect
  end

  def each(*argv, &block)
    obj = self
    if !argv.empty?
      obj = self.dup
      args = obj.instance_variable_get(:@args)
      args = args.empty? ? argv.dup : (args.dup.concat(argv))
      obj.instance_variable_set(:@args, args)
    end
    return obj unless block
    obj.__send__(:__enumerator_block_call, &block)
  end

  def each_with_object(object, &block)
    return to_enum(:each_with_object, object) { size } unless block
    __enumerator_block_call do |*i|
      block.call(i.__svalue, object)
    end
    object
  end
  alias with_object each_with_object

  def self.produce(*args, size: (no_size = true; nil), **kw, &block)
    raise ArgumentError, "unknown keyword#{kw.size > 1 ? 's' : ''}: #{kw.keys.map(&:inspect).join(', ')}" unless kw.empty?
    raise ArgumentError, "no block given" unless block
    raise ArgumentError, "wrong number of arguments (given #{args.size}, expected 0..1)" if args.size > 1
    has_init = !args.empty?
    init = args[0]
    sz = no_size ? Float::INFINITY : size
    Enumerator.new(sz) do |y|
      if has_init
        val = init
        y.yield(val)
      else
        val = nil
      end
      begin
        loop do
          y.yield(val = block.call(val))
        end
      rescue StopIteration
      end
    end
  end

  class Chain
    def initialize(*args)
      @enums = args.freeze
      @pos = -1
      self
    end

    def size
      total = 0
      @enums.each do |e|
        return nil unless e.respond_to?(:size)
        s = e.size
        return nil if s.nil?
        return s if Float === s && s.infinite?
        total += s
      end
      total
    end

    alias __orig_chain_inspect inspect
    def inspect
      return "#<#{self.class}: uninitialized>" unless defined?(@enums) && !@enums.nil?
      __orig_chain_inspect
    end
  end

  class Lazy < Enumerator
    def initialize(obj, size = nil, &block)
      raise ArgumentError, "tried to call lazy new without a block" unless block
      super(size) do |yielder, *each_args|
        begin
          obj.each(*each_args) do |*x|
            block.call(yielder, *x)
          end
        rescue StopIteration
        end
      end
      @__lazy_obj = obj
      self
    end

    def lazy
      self
    end

    def eager
      src = self
      Enumerator.new(-> { src.size }) do |y|
        src.each { |*v| y.yield(*v) }
      end
    end

    def force(*args)
      to_a(*args)
    end

    def to_enum(meth = :each, *args, **kwd, &size_block)
      src = self
      sb = size_block || -> { nil }
      gen = Enumerator.new(sb) do |yielder|
        res = if kwd.empty?
                src.__send__(meth, *args) { |*v| yielder.yield(*v) }
              else
                src.__send__(meth, *args, **kwd) { |*v| yielder.yield(*v) }
              end
        res.each {} if Enumerator::Lazy === res
      end
      Lazy.new(gen, sb) { |y, *v| y.yield(*v) }
    end
    alias enum_for to_enum

    def map(&block)
      raise ArgumentError, "tried to call lazy map without a block" unless block
      src = self
      Lazy.new(self, -> { src.size }) do |yielder, *v|
        yielder.yield(block.call(*v))
      end
    end
    alias collect map

    def select(&block)
      raise ArgumentError, "tried to call lazy select without a block" unless block
      Lazy.new(self, nil) do |yielder, *v|
        val = v.__svalue
        yielder.yield(val) if block.call(val)
      end
    end
    alias find_all select
    alias filter select

    def reject(&block)
      raise ArgumentError, "tried to call lazy reject without a block" unless block
      Lazy.new(self, nil) do |yielder, *v|
        val = v.__svalue
        yielder.yield(val) unless block.call(val)
      end
    end

    def filter_map(&block)
      raise ArgumentError, "tried to call lazy filter_map without a block" unless block
      Lazy.new(self, nil) do |yielder, *v|
        val = v.__svalue
        res = block.call(val)
        yielder.yield(res) if res
      end
    end

    def compact
      Lazy.new(self, nil) do |yielder, *v|
        val = v.__svalue
        yielder.yield(val) unless val.nil?
      end
    end

    def grep(pattern, &block)
      if block
        Lazy.new(self, nil) do |yielder, *v|
          val = v.__svalue
          yielder.yield(block.call(val)) if pattern === val
        end
      else
        Lazy.new(self, nil) do |yielder, *v|
          val = v.__svalue
          yielder.yield(val) if pattern === val
        end
      end
    end

    def grep_v(pattern, &block)
      if block
        Lazy.new(self, nil) do |yielder, *v|
          val = v.__svalue
          yielder.yield(block.call(val)) unless pattern === val
        end
      else
        Lazy.new(self, nil) do |yielder, *v|
          val = v.__svalue
          yielder.yield(val) unless pattern === val
        end
      end
    end

    def flat_map(&block)
      raise ArgumentError, "tried to call lazy flat_map without a block" unless block
      Lazy.new(self, nil) do |yielder, *v|
        res = block.call(*v)
        if Array === res
          res.each { |e| yielder.yield(e) }
        elsif res.respond_to?(:force) && res.respond_to?(:each)
          res.each { |*e| yielder.yield(*e) }
        elsif res.respond_to?(:to_ary)
          ary = res.to_ary
          if ary.nil?
            yielder.yield(res)
          elsif Array === ary
            ary.each { |e| yielder.yield(e) }
          else
            raise TypeError, "can't convert #{res.class} into Array (#{res.class}#to_ary gives #{ary.class})"
          end
        else
          yielder.yield(res)
        end
      end
    end
    alias collect_concat flat_map

    def take(n)
      n = Kernel.__to_int(n)
      raise ArgumentError, "attempt to take negative size" if n < 0
      src = self
      sz = -> {
        s = src.size
        next nil unless Integer === s || (Float === s && s.infinite?)
        s < n ? s : n
      }
      if n == 0
        return Lazy.new([], 0) { |y, *v| }
      end
      gen = Enumerator.new(sz) do |yielder|
        remain = n
        begin
          src.each do |*v|
            val = v.__svalue
            yielder.yield(val)
            remain -= 1
            break if remain <= 0
          end
        rescue StopIteration
        end
      end
      Lazy.new(gen, sz) { |y, *v| y.yield(*v) }
    end

    def take_while(&block)
      raise ArgumentError, "tried to call lazy take_while without a block" unless block
      Lazy.new(self, nil) do |yielder, *v|
        val = v.__svalue
        raise StopIteration unless block.call(*v)
        yielder.yield(val)
      end
    end

    def drop(n)
      n = Kernel.__to_int(n)
      raise ArgumentError, "attempt to drop negative size" if n < 0
      src = self
      sz = -> {
        s = src.size
        next nil unless Integer === s || (Float === s && s.infinite?)
        s <= n ? 0 : s - n
      }
      gen = Enumerator.new(sz) do |yielder|
        dropped = 0
        begin
          src.each do |*v|
            if dropped < n
              dropped += 1
            else
              val = v.__svalue
              yielder.yield(val)
            end
          end
        rescue StopIteration
        end
      end
      Lazy.new(gen, sz) { |y, *v| y.yield(*v) }
    end

    def drop_while(&block)
      raise ArgumentError, "tried to call lazy drop_while without a block" unless block
      src = self
      gen = Enumerator.new(nil) do |yielder|
        dropping = true
        begin
          src.each do |*v|
            val = v.__svalue
            if dropping
              next if block.call(*v)
              dropping = false
            end
            yielder.yield(val)
          end
        rescue StopIteration
        end
      end
      Lazy.new(gen, nil) { |y, *v| y.yield(*v) }
    end

    def uniq(&block)
      src = self
      gen = Enumerator.new(nil) do |yielder|
        buckets = {}
        begin
          src.each do |*v|
            val = v.__svalue
            k = block ? block.call(val) : val
            h = k.hash
            list = buckets[h]
            if list.nil?
              buckets[h] = [k]
              yielder.yield(val)
            elsif !list.any? { |e| k.eql?(e) }
              list << k
              yielder.yield(val)
            end
          end
        rescue StopIteration
        end
      end
      Lazy.new(gen, nil) { |y, *v| y.yield(*v) }
    end

    def with_index(offset = 0, &block)
      off = offset.nil? ? 0 : Kernel.__to_int(offset)
      src = self
      sz = -> { src.size }
      gen = Enumerator.new(sz) do |yielder|
        idx = off
        begin
          src.each do |*v|
            val = v.__svalue
            if block
              block.call(val, idx)
              yielder.yield(val)
            else
              yielder.yield([val, idx])
            end
            idx += 1
          end
        rescue StopIteration
        end
      end
      Lazy.new(gen, sz) { |y, *v| y.yield(*v) }
    end

    def zip(*args, &block)
      coerced = args.map do |a|
        if Array === a
          [:ary, a]
        elsif a.respond_to?(:to_ary)
          r = a.to_ary
          if r.nil?
            raise TypeError, "wrong argument type #{a.class} (must respond to :each)" unless a.respond_to?(:each)
            [:enum, a]
          elsif Array === r
            [:ary, r]
          else
            raise TypeError, "can't convert #{a.class} into Array (#{a.class}#to_ary gives #{r.class})"
          end
        elsif a.respond_to?(:each)
          [:enum, a]
        else
          raise TypeError, "wrong argument type #{a.class} (must respond to :each)"
        end
      end
      if block
        iters = coerced.map { |kind, obj| kind == :ary ? [:ary, obj] : [:enum, obj.to_enum(:each)] }
        idx = 0
        each do |*v|
          val = v.__svalue
          row = [val]
          iters.each do |kind, obj|
            if kind == :ary
              row << obj[idx]
            else
              row << (obj.next rescue nil)
            end
          end
          idx += 1
          block.call(row)
        end
        return nil
      end
      src = self
      sz = -> { src.size }
      gen = Enumerator.new(sz) do |yielder|
        iters = coerced.map { |kind, obj| kind == :ary ? [:ary, obj] : [:enum, obj.to_enum(:each)] }
        idx = 0
        begin
          src.each do |*v|
            val = v.__svalue
            row = [val]
            iters.each do |kind, obj|
              if kind == :ary
                row << obj[idx]
              else
                row << (obj.next rescue nil)
              end
            end
            idx += 1
            yielder.yield(row)
          end
        rescue StopIteration
        end
      end
      Lazy.new(gen, sz) { |y, *v| y.yield(*v) }
    end

    def chunk(&block)
      unless block
        lz = Lazy.allocate
        lz.instance_variable_set(:@obj, self)
        lz.instance_variable_set(:@meth, :chunk)
        lz.instance_variable_set(:@args, [])
        lz.instance_variable_set(:@kwd, {})
        lz.instance_variable_set(:@fib, nil)
        lz.instance_variable_set(:@dst, nil)
        lz.instance_variable_set(:@lookahead, nil)
        lz.instance_variable_set(:@feedvalue, nil)
        lz.instance_variable_set(:@stop_exc, false)
        lz.instance_variable_set(:@size_val, nil)
        lz.instance_variable_set(:@size_block, -> { nil })
        return lz
      end
      src = self
      gen = Enumerator.new(nil) do |yielder|
        prev_key = nil
        chunk_ary = nil
        has_prev = false
        begin
          src.each do |*v|
            val = v.__svalue
            key = block.call(val)
            if key.nil? || key == :_separator
              if has_prev
                yielder.yield([prev_key, chunk_ary])
                has_prev = false
                chunk_ary = nil
              end
            elsif key == :_alone
              if has_prev
                yielder.yield([prev_key, chunk_ary])
                has_prev = false
                chunk_ary = nil
              end
              yielder.yield([:_alone, [val]])
            elsif Symbol === key && key.to_s.start_with?("_")
              raise RuntimeError, "symbols beginning with an underscore are reserved"
            else
              if has_prev && prev_key == key
                chunk_ary << val
              else
                yielder.yield([prev_key, chunk_ary]) if has_prev
                prev_key = key
                chunk_ary = [val]
                has_prev = true
              end
            end
          end
        rescue StopIteration
        end
        yielder.yield([prev_key, chunk_ary]) if has_prev
      end
      Lazy.new(gen, nil) { |y, *v| y.yield(*v) }
    end

    def chunk_while(&block)
      e = super(&block)
      Lazy.new(e, nil) { |y, *v| y.yield(*v) }
    end

    def slice_before(*args, &block)
      e = super(*args, &block)
      Lazy.new(e, nil) { |y, *v| y.yield(*v) }
    end

    def slice_after(*args, &block)
      e = super(*args, &block)
      Lazy.new(e, nil) { |y, *v| y.yield(*v) }
    end

    def slice_when(&block)
      e = super(&block)
      Lazy.new(e, nil) { |y, *v| y.yield(*v) }
    end
  end
end

module Enumerable
  def lazy
    src = self
    sz = respond_to?(:size) ? -> { src.size } : nil
    Enumerator::Lazy.new(self, sz) { |yielder, *v| yielder.yield(*v) }
  end
end
