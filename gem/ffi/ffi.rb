module FFI
  VERSION = "1.17.4"

  def self.make_shareable(obj)
    if defined?(Ractor) && Ractor.respond_to?(:make_shareable)
      Ractor.make_shareable(obj)
    else
      obj.freeze
    end
  end

  class NotFoundError < LoadError
    def initialize(function, *libraries)
      super("Function '#{function}' not found in [#{libraries.compact.join(', ')}]")
    end
  end

  class NullPointerError < RuntimeError; end

  module DataConverter
    def native_type(type = nil)
      if type
        @native_type = FFI.find_type(type)
      elsif @native_type
        @native_type
      else
        raise NotImplementedError, "native_type method not overridden and no native_type set"
      end
    end

    def to_native(value, ctx = nil)
      value
    end

    def from_native(value, ctx = nil)
      value
    end

    def size
      native_type.size
    end

    def alignment
      native_type.alignment
    end
  end

  class LibraryPath
    attr_reader :name, :abi_number, :root

    def initialize(name, abi_number: nil, root: nil)
      @name = name
      @abi_number = abi_number
      @root = root
    end

    def full_name
      base = FFI.map_library_name(@name)
      if @abi_number
        if Platform.mac?
          base = base.sub(/\.dylib\z/, ".#{@abi_number}.dylib")
        elsif Platform.windows?
          base = base.sub(/\.dll\z/i, "-#{@abi_number}.dll")
        else
          base = "#{base}.#{@abi_number}"
        end
      end
      @root ? File.join(@root.to_s, base) : base
    end
    alias to_s full_name

    def self.wrap(value)
      return value if value.is_a?(self)
      new(value)
    end
  end

  module Platform
    def self.mac?
      IS_MAC
    end

    def self.linux?
      IS_LINUX
    end

    def self.windows?
      IS_WINDOWS
    end

    def self.bsd?
      IS_BSD
    end

    def self.unix?
      !IS_WINDOWS
    end

    def self.arch_for_cpu(cpu)
      case cpu.to_s.downcase
      when /\Ai[3456]86\z/, "i86pc"
        "i386"
      when "amd64", "x86_64", "x64"
        "x86_64"
      when "ppc64le", "powerpc64le"
        "powerpc64le"
      when "ppc64", "powerpc64"
        "powerpc64"
      when "ppc", "powerpc"
        "powerpc"
      when "sparc64", "sparcv9"
        "sparcv9"
      when "arm64", "aarch64"
        "aarch64"
      else
        cpu.to_s.downcase
      end
    end
  end

  TypeDefs = {
    void: Type::VOID,
    bool: Type::BOOL,
    char: Type::INT8,
    uchar: Type::UINT8,
    int8: Type::INT8,
    uint8: Type::UINT8,
    int8_t: Type::INT8,
    uint8_t: Type::UINT8,
    short: Type::INT16,
    ushort: Type::UINT16,
    int16: Type::INT16,
    uint16: Type::UINT16,
    int16_t: Type::INT16,
    uint16_t: Type::UINT16,
    int: Type::INT32,
    uint: Type::UINT32,
    int32: Type::INT32,
    uint32: Type::UINT32,
    int32_t: Type::INT32,
    uint32_t: Type::UINT32,
    long: Type::LONG,
    ulong: Type::ULONG,
    long_long: Type::INT64,
    ulong_long: Type::UINT64,
    int64: Type::INT64,
    uint64: Type::UINT64,
    int64_t: Type::INT64,
    uint64_t: Type::UINT64,
    ptrdiff_t: Platform::ADDRESS_SIZE == 64 ? Type::INT64 : Type::INT32,
    float: Type::FLOAT32,
    float32: Type::FLOAT32,
    double: Type::FLOAT64,
    float64: Type::FLOAT64,
    long_double: Type::LONGDOUBLE,
    pointer: Type::POINTER,
    buffer_in: Type::POINTER,
    buffer_out: Type::POINTER,
    buffer_inout: Type::POINTER,
    string: Type::STRING,
    strptr: Type::STRPTR,
    size_t: Platform::ADDRESS_SIZE == 64 ? Type::UINT64 : Type::UINT32,
    ssize_t: Platform::ADDRESS_SIZE == 64 ? Type::INT64 : Type::INT32,
    intptr_t: Platform::ADDRESS_SIZE == 64 ? Type::INT64 : Type::INT32,
    uintptr_t: Platform::ADDRESS_SIZE == 64 ? Type::UINT64 : Type::UINT32,
    off_t: Type::INT64,
    pid_t: Type::INT32,
    uid_t: Type::UINT32,
    gid_t: Type::UINT32,
    time_t: Platform::LONG_SIZE == 64 ? Type::INT64 : Type::INT32,
  }

  def self.add_typedef(old, add)
    TypeDefs[add.to_sym] = find_type(old)
  end

  class AutoPointerConverter
    include DataConverter
    def initialize(cls)
      @cls = cls
    end
    def native_type
      Type::POINTER
    end
    def to_native(val, _ctx = nil)
      return Pointer::NULL if val.nil?
      val
    end
    def from_native(val, _ctx = nil)
      @cls.new(val)
    end
  end

  def self.find_type(name, type_map = nil)
    return name if name.is_a?(Type)
    return name if name.is_a?(Enum)
    return name if name.is_a?(StructByReference)
    return name if name.is_a?(DataConverter)
    return name.by_ref if name.is_a?(Class) && name < Struct
    return AutoPointerConverter.new(name) if name.is_a?(Class) && name <= AutoPointer

    if type_map && type_map.key?(name)
      return type_map[name]
    end
    sym = name.is_a?(Symbol) ? name : (name.respond_to?(:to_sym) ? name.to_sym : nil)
    if sym && type_map && type_map.key?(sym)
      return type_map[sym]
    end
    if sym && TypeDefs.key?(sym)
      return TypeDefs[sym]
    end
    raise TypeError, "unable to resolve type '#{name.inspect}'"
  end

  def self.type_size(type)
    return type.size if type.is_a?(Class) && type < Struct
    find_type(type).size
  end

  def self.map_library_name(lib)
    return nil if lib.nil?
    lib = lib.to_s
    return Platform::LIBC if lib == "c"
    if Platform.windows?
      return lib if lib.include?("\\") || lib.include?("/") || lib.downcase.end_with?(".dll")
      return "#{lib}.dll"
    end
    return lib if lib.include?("/") || lib.end_with?(".#{Platform::LIBSUFFIX}") || lib.include?(".so.")
    "#{Platform::LIBPREFIX}#{lib}.#{Platform::LIBSUFFIX}"
  end

  # -------------------------------------------------------------------------
  # AbstractMemory / Pointer / MemoryPointer / AutoPointer / Buffer
  # -------------------------------------------------------------------------

  class AbstractMemory
    alias get_char get_int8
    alias put_char put_int8
    alias get_uchar get_uint8
    alias put_uchar put_uint8
    alias get_short get_int16
    alias put_short put_int16
    alias get_ushort get_uint16
    alias put_ushort put_uint16
    alias get_int get_int32
    alias put_int put_int32
    alias get_uint get_uint32
    alias put_uint put_uint32
    alias get_long_long get_int64
    alias put_long_long put_int64
    alias get_ulong_long get_uint64
    alias put_ulong_long put_uint64
    alias get_float get_float32
    alias put_float put_float32
    alias get_double get_float64
    alias put_double put_float64

    if Platform::LONG_SIZE == 64
      alias get_long get_int64
      alias put_long put_int64
      alias get_ulong get_uint64
      alias put_ulong put_uint64
    else
      alias get_long get_int32
      alias put_long put_int32
      alias get_ulong get_uint32
      alias put_ulong put_uint32
    end

    %w[
      int8 uint8 char uchar
      int16 uint16 short ushort
      int32 uint32 int uint
      int64 uint64 long_long ulong_long
      long ulong
      float32 float float64 double
      pointer
    ].each do |t|
      define_method("read_#{t}") { send("get_#{t}", 0) }
      define_method("write_#{t}") { |v| send("put_#{t}", 0, v) }
    end

    def read_bytes(length)
      get_bytes(0, length)
    end

    def write_bytes(str, index = 0, length = nil)
      put_bytes(0, str, index, length)
    end

    def read_string(length = nil)
      if length
        get_bytes(0, length)
      else
        get_string(0)
      end
    end

    def read_string_length(length)
      get_bytes(0, length)
    end

    def read_string_to_null
      get_string(0)
    end

    def write_string(str, length = nil)
      if length
        put_bytes(0, str, 0, length)
      else
        put_bytes(0, str)
      end
    end

    def write_string_length(str, length)
      put_bytes(0, str, 0, length)
    end

    {
      int8: 1, uint8: 1, char: 1, uchar: 1,
      int16: 2, uint16: 2, short: 2, ushort: 2,
      int32: 4, uint32: 4, int: 4, uint: 4,
      int64: 8, uint64: 8, long_long: 8, ulong_long: 8,
      long: Platform::LONG_SIZE / 8, ulong: Platform::LONG_SIZE / 8,
      float: 4, float32: 4, double: 8, float64: 8,
      pointer: Platform::ADDRESS_SIZE / 8
    }.each do |t, sz|
      define_method("get_array_of_#{t}") do |offset, length|
        Array.new(length) { |i| send("get_#{t}", offset + i * sz) }
      end

      define_method("put_array_of_#{t}") do |offset, ary|
        ary.each_with_index { |v, i| send("put_#{t}", offset + i * sz, v) }
        self
      end

      define_method("read_array_of_#{t}") do |length|
        send("get_array_of_#{t}", 0, length)
      end

      define_method("write_array_of_#{t}") do |ary|
        send("put_array_of_#{t}", 0, ary)
      end
    end

    def [](idx)
      self + (idx * type_size)
    end

    def get_array_of_string(offset, count = nil)
      psz = Platform::ADDRESS_SIZE / 8
      res = []
      if count
        count.times do |i|
          p = get_pointer(offset + i * psz)
          res << (p.null? ? nil : p.read_string)
        end
      else
        i = 0
        loop do
          p = get_pointer(offset + i * psz)
          break if p.null?
          res << p.read_string
          i += 1
        end
      end
      res
    end

    def read_array_of_string(count = nil)
      get_array_of_string(0, count)
    end

    def to_ptr
      self
    end
  end

  class Pointer
    NULL = Pointer.new(0)
    SIZE = Platform::ADDRESS_SIZE / 8

    def self.size
      SIZE
    end

    def read_pointer
      get_pointer(0)
    end

    def inspect
      "#<#{self.class} address=0x#{address.to_s(16)}>"
    end
  end

  class MemoryPointer
    def self.new(size, count = 1, clear = true, &block)
      type_size =
        if size.is_a?(Integer)
          size
        elsif size.is_a?(Class) && size < Struct
          size.size
        else
          FFI.type_size(size)
        end
      ptr = __alloc(type_size, count, clear)
      if block
        begin
          return yield(ptr)
        ensure
          ptr.free
        end
      end
      ptr
    end

    def self.from_string(str)
      s = str.to_s
      ptr = new(1, s.bytesize + 1, false)
      ptr.put_string(0, s)
      ptr
    end
  end

  class Buffer < MemoryPointer
    class << self
      def alloc_in(size, count = 1, clear = true, &block)
        new(size, count, clear, &block)
      end
      alias new_in alloc_in

      def alloc_out(size, count = 1, clear = true, &block)
        new(size, count, clear, &block)
      end
      alias new_out alloc_out

      def alloc_inout(size, count = 1, clear = true, &block)
        new(size, count, clear, &block)
      end
      alias new_inout alloc_inout
    end

    def length
      size / type_size
    end
  end

  class AutoPointer < Pointer
    def initialize(pointer, proc = nil, &block)
      raise TypeError, "expected FFI::Pointer" unless pointer.is_a?(Pointer)
      super(pointer.address)
      @releaser = proc || block || (self.class.respond_to?(:release) ? self.class.method(:release) : nil)
      @autorelease = true
      @freed = false
    end

    def autorelease=(val)
      @autorelease = !!val
    end

    def autorelease?
      @autorelease
    end

    def free
      return if @freed || null?
      @freed = true
      @releaser.call(Pointer.new(address)) if @releaser
    end
  end

  # -------------------------------------------------------------------------
  # Enum & Bitmask
  # -------------------------------------------------------------------------

  class Enum
    attr_reader :tag, :native_type

    def initialize(info, tag = nil, native_type = Type::INT32)
      @tag = tag
      @native_type = FFI.find_type(native_type)
      @kv_map = {}
      @vk_map = {}
      next_val = 0
      i = 0
      while i < info.length
        key = info[i]
        if i + 1 < info.length && info[i + 1].is_a?(Integer)
          val = info[i + 1]
          i += 2
        else
          val = next_val
          i += 1
        end
        @kv_map[key] = val
        @vk_map[val] ||= key
        next_val = val + 1
      end
    end

    def size
      @native_type.size
    end

    def alignment
      @native_type.alignment
    end

    def symbols
      @kv_map.keys
    end

    def [](query)
      case query
      when Symbol
        @kv_map[query]
      when Integer
        @vk_map[query]
      else
        raise ArgumentError, "invalid enum query: #{query.inspect}"
      end
    end
    alias find []

    def to_native(val, _ctx = nil)
      return val if val.is_a?(Integer)
      v = @kv_map[val]
      raise ArgumentError, "invalid enum value #{val.inspect}" if v.nil?
      v
    end

    def from_native(val, _ctx = nil)
      @vk_map.fetch(val, val)
    end

    def symbol_map
      @kv_map.dup
    end
    alias to_h symbol_map
  end

  class Bitmask < Enum
    def initialize(info, tag = nil, native_type = Type::INT32)
      @tag = tag
      @native_type = FFI.find_type(native_type)
      @kv_map = {}
      @vk_map = {}
      next_bit = 0
      i = 0
      while i < info.length
        key = info[i]
        if i + 1 < info.length && info[i + 1].is_a?(Integer)
          bit = info[i + 1]
          i += 2
        else
          bit = next_bit
          i += 1
        end
        val = 1 << bit
        @kv_map[key] = val
        @vk_map[val] ||= key
        next_bit = bit + 1
      end
    end

    def to_native(val, _ctx = nil)
      return val if val.is_a?(Integer)
      return @kv_map[val] || raise(ArgumentError, "invalid bitmask #{val.inspect}") if val.is_a?(Symbol)
      if val.is_a?(Array)
        val.inject(0) { |acc, item| acc | to_native(item) }
      else
        raise ArgumentError, "invalid bitmask value #{val.inspect}"
      end
    end

    def from_native(val, _ctx = nil)
      out = []
      @kv_map.each do |sym, bit|
        out << sym if (val & bit) == bit && bit != 0
      end
      out
    end
  end

  # -------------------------------------------------------------------------
  # StructByReference / StructLayout / Struct / Union / ManagedStruct
  # -------------------------------------------------------------------------

  class StructByReference
    attr_reader :struct_class

    def initialize(struct_class)
      @struct_class = struct_class
    end

    def native_type
      Type::POINTER
    end

    def size
      Type::POINTER.size
    end

    def alignment
      Type::POINTER.alignment
    end

    def to_native(val, _ctx = nil)
      return Pointer::NULL if val.nil?
      return val.to_ptr if val.respond_to?(:to_ptr)
      val
    end

    def from_native(val, _ctx = nil)
      @struct_class.new(val)
    end
  end

  class StructLayout
    Field = Struct.new(:name, :offset, :type, :size, :alignment, :array_len, :struct_class)

    attr_reader :fields, :size, :alignment

    def initialize(fields, size, alignment)
      @fields = fields
      @field_map = {}
      fields.each { |f| @field_map[f.name] = f }
      @size = size
      @alignment = alignment
    end

    def members
      @fields.map(&:name)
    end

    def [](name)
      @field_map[name]
    end

    def offset_of(name)
      f = @field_map[name]
      raise ArgumentError, "No such field '#{name}'" unless f
      f.offset
    end

    def ffi_field_types
      types = []
      @fields.each do |f|
        if f.array_len
          elem = f.struct_class ? f.struct_class.by_value : f.type
          f.array_len.times { types << elem }
        elsif f.struct_class
          types << f.struct_class.by_value
        elsif f.type.is_a?(Enum)
          types << f.type.native_type
        else
          types << f.type
        end
      end
      types
    end
  end

  class Struct
    class InlineArray
      include Enumerable

      def initialize(ptr, offset, elem_type, count, struct_class = nil)
        @ptr = ptr
        @offset = offset
        @elem_type = elem_type
        @count = count
        @struct_class = struct_class
        @elem_size = struct_class ? struct_class.size : elem_type.size
      end

      def size
        @count
      end

      def [](idx)
        raise IndexError, "index #{idx} out of bounds" if idx < 0 || idx >= @count
        off = @offset + idx * @elem_size
        if @struct_class
          @struct_class.new(@ptr.slice(off, @elem_size))
        else
          Struct.__read_field(@ptr, off, @elem_type)
        end
      end

      def []=(idx, val)
        raise IndexError, "index #{idx} out of bounds" if idx < 0 || idx >= @count
        off = @offset + idx * @elem_size
        Struct.__write_field(@ptr, off, @elem_type, val)
      end

      def to_s
        if @elem_type == Type::INT8 || @elem_type == Type::UINT8
          @ptr.get_string(@offset, @count)
        else
          to_a.inspect
        end
      end

      def to_ptr
        @ptr.slice(@offset, @count * @elem_size)
      end

      def each
        return to_enum(:each) unless block_given?
        @count.times { |i| yield self[i] }
      end
    end

    class << self
      def enclosing_module
        n = name
        return nil unless n && n.include?("::")
        parts = n.split("::")[0..-2]
        mod = Object
        parts.each do |part|
          return nil unless mod.const_defined?(part)
          mod = mod.const_get(part)
        end
        mod
      end

      def find_struct_type(raw_type)
        # Walk enclosing modules (e.g., GObject::GParamSpec -> GObject) for module-scoped typedefs
        n = name
        if n && n.include?("::")
          parts = n.split("::")[0..-2]
          while !parts.empty?
            mod = Object
            ok = true
            parts.each do |part|
              if mod.const_defined?(part)
                mod = mod.const_get(part)
              else
                ok = false
                break
              end
            end
            if ok && mod.respond_to?(:find_type)
              begin
                return mod.find_type(raw_type)
              rescue TypeError
              end
            end
            parts.pop
          end
        end
        FFI.find_type(raw_type)
      end

      def layout(*spec)
        is_union = self <= Union
        fields = []
        offset = 0
        max_align = 1
        max_size = 0
        i = 0

        while i < spec.length
          name = spec[i].to_sym
          raw_type = spec[i + 1]
          i += 2

          explicit_offset = nil
          if i < spec.length && spec[i].is_a?(Integer)
            explicit_offset = spec[i]
            i += 1
          end

          array_len = nil
          struct_cls = nil
          ftype = nil
          fsize = 0
          falign = 1

          if raw_type.is_a?(Array)
            elem_raw, array_len = raw_type[0], raw_type[1]
            if elem_raw.is_a?(Class) && elem_raw < Struct
              struct_cls = elem_raw
              fsize = elem_raw.size * array_len
              falign = elem_raw.alignment
            else
              ftype = find_struct_type(elem_raw)
              fsize = ftype.size * array_len
              falign = ftype.alignment
            end
          elsif raw_type.is_a?(Class) && raw_type < Struct
            struct_cls = raw_type
            fsize = raw_type.size
            falign = raw_type.alignment
          else
            ftype = find_struct_type(raw_type)
            fsize = ftype.size
            falign = ftype.alignment
          end

          falign = 1 if falign <= 0
          falign = @pack if @pack && falign > @pack
          max_align = falign if falign > max_align

          field_off =
            if explicit_offset
              explicit_offset
            elsif is_union
              0
            else
              rem = offset % falign
              rem == 0 ? offset : offset + (falign - rem)
            end

          fields << StructLayout::Field.new(name, field_off, ftype, fsize, falign, array_len, struct_cls)
          end_off = field_off + fsize
          max_size = end_off if end_off > max_size
          offset = end_off unless is_union
        end

        rem = max_size % max_align
        total_size = rem == 0 ? max_size : max_size + (max_align - rem)
        @layout = StructLayout.new(fields, total_size, max_align)
      end

      def struct_layout
        @layout
      end

      def size
        @layout ? @layout.size : 0
      end

      def alignment
        @layout ? @layout.alignment : 1
      end

      def members
        @layout ? @layout.members : []
      end

      def offset_of(name)
        @layout.offset_of(name)
      end

      def packed(align = 1)
        @pack = align
      end
      alias pack packed

      def by_ref
        StructByReference.new(self)
      end
      alias ptr by_ref
      alias in by_ref
      alias out by_ref
      alias inout by_ref

      def by_value
        @by_value ||= StructByValue.__build(self, @layout.ffi_field_types, size, alignment)
      end
      alias val by_value
    end

    def self.__read_field(ptr, off, ftype)
      return ftype.from_native(__read_field(ptr, off, ftype.native_type)) if ftype.is_a?(Enum)
      case ftype
      when Type::BOOL    then ptr.get_uint8(off) != 0
      when Type::INT8    then ptr.get_int8(off)
      when Type::UINT8   then ptr.get_uint8(off)
      when Type::INT16   then ptr.get_int16(off)
      when Type::UINT16  then ptr.get_uint16(off)
      when Type::INT32   then ptr.get_int32(off)
      when Type::UINT32  then ptr.get_uint32(off)
      when Type::INT64   then ptr.get_int64(off)
      when Type::UINT64  then ptr.get_uint64(off)
      when Type::LONG    then ptr.get_long(off)
      when Type::ULONG   then ptr.get_ulong(off)
      when Type::FLOAT32 then ptr.get_float32(off)
      when Type::FLOAT64 then ptr.get_float64(off)
      when Type::POINTER then ptr.get_pointer(off)
      when Type::STRING
        p = ptr.get_pointer(off)
        p.null? ? nil : p.read_string
      when StructByReference
        p = ptr.get_pointer(off)
        ftype.from_native(p)
      when FunctionType
        ptr.get_pointer(off)
      else
        raise TypeError, "unsupported struct field type #{ftype.inspect}"
      end
    end

    def self.__write_field(ptr, off, ftype, val)
      if ftype.is_a?(Enum)
        return __write_field(ptr, off, ftype.native_type, ftype.to_native(val))
      end
      case ftype
      when Type::BOOL    then ptr.put_uint8(off, val ? 1 : 0)
      when Type::INT8    then ptr.put_int8(off, val)
      when Type::UINT8   then ptr.put_uint8(off, val)
      when Type::INT16   then ptr.put_int16(off, val)
      when Type::UINT16  then ptr.put_uint16(off, val)
      when Type::INT32   then ptr.put_int32(off, val)
      when Type::UINT32  then ptr.put_uint32(off, val)
      when Type::INT64   then ptr.put_int64(off, val)
      when Type::UINT64  then ptr.put_uint64(off, val)
      when Type::LONG    then ptr.put_long(off, val)
      when Type::ULONG   then ptr.put_ulong(off, val)
      when Type::FLOAT32 then ptr.put_float32(off, val)
      when Type::FLOAT64 then ptr.put_float64(off, val)
      when Type::POINTER then ptr.put_pointer(off, val)
      when StructByReference then ptr.put_pointer(off, ftype.to_native(val))
      when FunctionType  then ptr.put_pointer(off, ftype.to_native(val))
      else
        raise TypeError, "unsupported struct field type #{ftype.inspect}"
      end
    end

    def initialize(pointer = nil)
      @pointer = pointer ? pointer : MemoryPointer.new(1, self.class.size, true)
    end

    def initialize_copy(orig)
      @pointer = orig.pointer.dup
      self
    end

    def pointer
      @pointer
    end
    alias to_ptr pointer

    def size
      self.class.size
    end

    def members
      self.class.members
    end

    def null?
      @pointer.null?
    end

    def clear
      @pointer.clear
      self
    end

    def [](field_name)
      f = self.class.struct_layout[field_name]
      raise ArgumentError, "Unknown field #{field_name.inspect}" unless f
      if f.array_len
        InlineArray.new(@pointer, f.offset, f.type, f.array_len, f.struct_class)
      elsif f.struct_class
        f.struct_class.new(@pointer.slice(f.offset, f.size))
      else
        Struct.__read_field(@pointer, f.offset, f.type)
      end
    end

    def []=(field_name, val)
      f = self.class.struct_layout[field_name]
      raise ArgumentError, "Unknown field #{field_name.inspect}" unless f
      if f.array_len
        if (f.type == Type::INT8 || f.type == Type::UINT8) && val.is_a?(String)
          bytes = val.byteslice(0, f.array_len)
          @pointer.put_bytes(f.offset, bytes)
          if bytes.bytesize < f.array_len
            @pointer.put_uint8(f.offset + bytes.bytesize, 0)
          end
        elsif val.is_a?(Array)
          ia = self[field_name]
          val.each_with_index { |v, idx| ia[idx] = v }
        else
          raise TypeError, "cannot assign #{val.class} to inline array field #{field_name}"
        end
      elsif f.struct_class
        src = val.respond_to?(:to_ptr) ? val.to_ptr : val
        @pointer.put_bytes(f.offset, src.read_bytes(f.size))
      else
        Struct.__write_field(@pointer, f.offset, f.type, val)
      end
      val
    end

    def values
      members.map { |m| self[m] }
    end
  end

  class Union < Struct
  end

  class ManagedStruct < Struct
    def initialize(pointer = nil)
      if pointer
        super(AutoPointer.new(pointer, self.class.method(:release)))
      else
        super(nil)
      end
    end
  end

  # -------------------------------------------------------------------------
  # Function & VariadicInvoker
  # -------------------------------------------------------------------------

  class FunctionType
    attr_accessor :param_types, :ret_type

    def to_native(val, _ctx = nil)
      return val if val.nil? || val.is_a?(Pointer)
      p_types = @param_types
      r_type = @ret_type
      needs_p = p_types && p_types.any? { |t| t.respond_to?(:from_native) }
      needs_r = r_type && r_type.respond_to?(:to_native)
      return val unless needs_p || needs_r
      proc do |*raw_args|
        conv_args = raw_args.each_with_index.map do |a, i|
          pt = p_types && p_types[i]
          pt && pt.respond_to?(:from_native) ? pt.from_native(a) : a
        end
        res = val.call(*conv_args)
        needs_r ? r_type.to_native(res) : res
      end
    end
  end

  class Function
    def self.new(ret_type, arg_types, ptr = nil, options = nil, &block)
      target = ptr || block
      raise ArgumentError, "expected Pointer or block" unless target
      r = FFI.find_type(ret_type)
      r_native = r.respond_to?(:native_type) ? r.native_type : r
      a_natives = arg_types.map do |t|
        ft = FFI.find_type(t)
        ft.respond_to?(:native_type) ? ft.native_type : ft
      end
      conv = options.is_a?(Hash) && options[:convention] ? options[:convention].to_s : nil
      __create(r_native, a_natives, target, conv)
    end

    def attach(mod, name)
      fn = self
      mod.define_singleton_method(name) { |*args| fn.call(*args) }
      mod.send(:define_method, name) { |*args| fn.call(*args) }
      self
    end
  end

  class VariadicInvoker
    def initialize(fn_ptr, fixed_types, ret_type, type_map = nil, convention = nil)
      @fn_ptr = fn_ptr
      @fixed_types = fixed_types
      @ret_type = ret_type
      @type_map = type_map
      @convention = convention ? convention.to_s : nil
    end

    def call(*args)
      nfixed = @fixed_types.length
      if args.length < nfixed
        raise ArgumentError, "wrong number of arguments (given #{args.length}, expected #{nfixed}+)"
      end
      var_args = args[nfixed..-1]
      if (var_args.length % 2) != 0
        raise ArgumentError, "variadic arguments must be pairs of (type, value)"
      end

      all_types = @fixed_types.map { |t| t.respond_to?(:native_type) ? t.native_type : t }
      all_vals = args[0, nfixed].each_with_index.map do |v, idx|
        ft = @fixed_types[idx]
        ft.respond_to?(:to_native) ? ft.to_native(v) : v
      end

      i = 0
      while i < var_args.length
        vt = FFI.find_type(var_args[i], @type_map)
        vv = var_args[i + 1]
        all_types << (vt.respond_to?(:native_type) ? vt.native_type : vt)
        all_vals << (vt.respond_to?(:to_native) ? vt.to_native(vv) : vv)
        i += 2
      end

      r_native = @ret_type.respond_to?(:native_type) ? @ret_type.native_type : @ret_type
      res = VariadicInvoker.__invoke(@fn_ptr, r_native, nfixed, all_types, all_vals, @convention)
      @ret_type.respond_to?(:from_native) ? @ret_type.from_native(res) : res
    end
  end

  # -------------------------------------------------------------------------
  # FFI::Library DSL
  # -------------------------------------------------------------------------

  module Library
    CURRENT_PROCESS = "__CURRENT_PROCESS__"
    LIBC = Platform::LIBC

    def ffi_lib_flags(*flags)
      @ffi_lib_flags = flags.inject(0) do |acc, f|
        acc | case f
              when :lazy   then DynamicLibrary::RTLD_LAZY
              when :now    then DynamicLibrary::RTLD_NOW
              when :local  then DynamicLibrary::RTLD_LOCAL
              when :global then DynamicLibrary::RTLD_GLOBAL
              when Integer then f
              else raise ArgumentError, "unknown library flag: #{f.inspect}"
              end
      end
    end

    def ffi_convention(conv = nil)
      @ffi_convention = conv if conv
      @ffi_convention || :default
    end

    def ffi_lib(*names)
      flags = @ffi_lib_flags || (DynamicLibrary::RTLD_LAZY | DynamicLibrary::RTLD_LOCAL)
      @ffi_libs = names.map do |name|
        if name == CURRENT_PROCESS
          DynamicLibrary.open(nil, flags)
        else
          candidates = name.is_a?(Array) ? name : [name]
          opened = nil
          last_err = nil
          candidates.each do |cand|
            mapped = FFI.map_library_name(cand)
            paths = [cand.to_s, mapped]
            if Platform.mac? && !mapped.include?("/")
              paths << "/opt/homebrew/lib/#{mapped}"
              paths << "/usr/local/lib/#{mapped}"
              paths << "/opt/local/lib/#{mapped}"
            elsif Platform.unix? && !Platform.mac? && !mapped.include?("/")
              paths << "/usr/local/lib/#{mapped}"
            elsif Platform.windows? && (cand.to_s == "c" || cand.to_s == Platform::LIBC)
              paths << "msvcrt.dll"
            end
            paths.uniq.each do |path|
              begin
                opened = DynamicLibrary.open(path, flags)
                break
              rescue LoadError => e
                last_err = e
                # glibc's libc.so, libm.so, ... are GNU ld scripts: open the
                # library the script names, as the ffi gem does
                if (script = ld_script_target(e.message))
                  begin
                    opened = DynamicLibrary.open(script, flags)
                    break
                  rescue LoadError => e2
                    last_err = e2
                  end
                end
              end
            end
            break if opened
          end
          raise(last_err || LoadError.new("Could not open library '#{name}'")) unless opened
          opened
        end
      end
    end

    def ld_script_target(message)
      return unless message =~ /(([^ \t()])+\.so([^ \t:()])*):([ \t])*(invalid ELF header|file too short|invalid file format)/
      File.binread($1) =~ /(?:GROUP|INPUT) *\( *([^ \)]+)/ ? $1 : nil
    rescue SystemCallError
      nil
    end
    private :ld_script_target

    def ffi_libraries
      @ffi_libs ||= [DynamicLibrary.open(nil, DynamicLibrary::RTLD_LAZY | DynamicLibrary::RTLD_LOCAL)]
    end

    def typedef(old, add)
      @ffi_typedefs ||= {}
      @ffi_typedefs[add.to_sym] = find_type(old)
    end

    def enum(*args)
      @ffi_typedefs ||= {}
      @ffi_enums ||= {}
      @ffi_enum_list ||= []
      native_type = Type::INT32
      tag = nil
      if args.length == 3
        if args[0].is_a?(Type) || (args[0].is_a?(Symbol) && (FFI::TypeDefs.key?(args[0]) || @ffi_typedefs.key?(args[0])))
          native_type = find_type(args.shift)
          tag = args.shift
        else
          tag = args.shift
          native_type = find_type(args.shift)
        end
      elsif args.length == 2 && args[1].is_a?(Array)
        if args[0].is_a?(Type)
          native_type = args.shift
        elsif args[0].is_a?(Symbol) && (FFI::TypeDefs.key?(args[0]) || @ffi_typedefs.key?(args[0]))
          native_type = find_type(args.shift)
        else
          tag = args.shift
        end
      end
      values = args.first.is_a?(Array) ? args.first : args
      e = Enum.new(values, tag, native_type)
      @ffi_enum_list << e
      @ffi_typedefs[tag] = e if tag
      @ffi_enums[tag] = e if tag
      e
    end

    def bitmask(*args)
      @ffi_typedefs ||= {}
      @ffi_enums ||= {}
      @ffi_enum_list ||= []
      native_type = Type::INT32
      tag = nil
      if args.length == 3
        if args[0].is_a?(Type) || (args[0].is_a?(Symbol) && (FFI::TypeDefs.key?(args[0]) || @ffi_typedefs.key?(args[0])))
          native_type = find_type(args.shift)
          tag = args.shift
        else
          tag = args.shift
          native_type = find_type(args.shift)
        end
      elsif args.length == 2 && args[1].is_a?(Array)
        if args[0].is_a?(Type)
          native_type = args.shift
        elsif args[0].is_a?(Symbol) && (FFI::TypeDefs.key?(args[0]) || @ffi_typedefs.key?(args[0]))
          native_type = find_type(args.shift)
        else
          tag = args.shift
        end
      end
      values = args.first.is_a?(Array) ? args.first : args
      b = Bitmask.new(values, tag, native_type)
      @ffi_enum_list << b
      @ffi_typedefs[tag] = b if tag
      @ffi_enums[tag] = b if tag
      b
    end

    def enum_type(name)
      @ffi_enums && @ffi_enums[name]
    end

    def enum_value(symbol)
      return nil unless @ffi_enum_list
      @ffi_enum_list.each do |e|
        v = e[symbol]
        return v unless v.nil?
      end
      nil
    end

    def callback(*args)
      @ffi_typedefs ||= {}
      if args.length == 3
        name, params, ret = args
      elsif args.length == 2
        name = nil
        params, ret = args
      else
        raise ArgumentError, "wrong number of arguments for callback"
      end
      r = find_type(ret)
      p_types = params.map { |t| find_type(t) }
      r_native = r.respond_to?(:native_type) ? r.native_type : r
      p_natives = p_types.map { |t| t.respond_to?(:native_type) ? t.native_type : t }
      ft = FunctionType.new(r_native, p_natives)
      ft.param_types = p_types
      ft.ret_type = r
      @ffi_typedefs[name.to_sym] = ft if name
      ft
    end

    def find_type(t)
      FFI.find_type(t, @ffi_typedefs)
    end

    def attach_function(name, func, args = nil, returns = nil, options = nil)
      if args.nil? && returns.nil?
        raise ArgumentError, "attach_function requires parameter and return types"
      end
      if func.is_a?(Array)
        # attach_function :name, [args], ret, options
        options = returns
        returns = args
        args = func
        func = name
      end

      c_name = func.to_s
      rb_name = name.to_sym

      sym_ptr = nil
      ffi_libraries.each do |lib|
        sym_ptr = lib.find_function(c_name)
        break if sym_ptr
      end
      unless sym_ptr
        raise NotFoundError.new(c_name, *ffi_libraries.map(&:name))
      end

      ret_type = find_type(returns)
      is_variadic = args.last == :varargs

      conv = (options.is_a?(Hash) && options[:convention]) || ffi_convention
      conv_s = conv ? conv.to_s : nil

      if is_variadic
        fixed_types = args[0...-1].map { |a| find_type(a) }
        invoker = VariadicInvoker.new(sym_ptr, fixed_types, ret_type, @ffi_typedefs, conv_s)
        define_singleton_method(rb_name) { |*call_args| invoker.call(*call_args) }
        define_method(rb_name) { |*call_args| invoker.call(*call_args) }
        return invoker
      end

      arg_types = args.map { |a| find_type(a) }
      r_native = ret_type.respond_to?(:native_type) ? ret_type.native_type : ret_type
      a_natives = arg_types.map { |t| t.respond_to?(:native_type) ? t.native_type : t }

      fn = Function.__create(r_native, a_natives, sym_ptr, conv_s)
      needs_arg_conv = arg_types.any? { |t| t.respond_to?(:to_native) }
      needs_ret_conv = ret_type.respond_to?(:from_native)

      if !needs_arg_conv && !needs_ret_conv
        define_singleton_method(rb_name) { |*call_args| fn.call(*call_args) }
        define_method(rb_name) { |*call_args| fn.call(*call_args) }
      else
        define_singleton_method(rb_name) do |*call_args|
          if needs_arg_conv
            call_args = call_args.each_with_index.map do |v, idx|
              at = arg_types[idx]
              at && at.respond_to?(:to_native) ? at.to_native(v) : v
            end
          end
          res = fn.call(*call_args)
          needs_ret_conv ? ret_type.from_native(res) : res
        end
        define_method(rb_name) do |*call_args|
          if needs_arg_conv
            call_args = call_args.each_with_index.map do |v, idx|
              at = arg_types[idx]
              at && at.respond_to?(:to_native) ? at.to_native(v) : v
            end
          end
          res = fn.call(*call_args)
          needs_ret_conv ? ret_type.from_native(res) : res
        end
      end

      fn
    end

    def attach_variable(mname, a1, a2 = nil)
      cname, type = a2 ? [a1.to_s, a2] : [mname.to_s, a1]
      sym_ptr = nil
      ffi_libraries.each do |lib|
        sym_ptr = lib.find_variable(cname)
        break if sym_ptr
      end
      raise NotFoundError.new(cname, *ffi_libraries.map(&:name)) unless sym_ptr

      ftype = find_type(type)
      define_singleton_method(mname) { Struct.__read_field(sym_ptr, 0, ftype) }
      define_singleton_method("#{mname}=") { |v| Struct.__write_field(sym_ptr, 0, ftype, v) }
    end
  end
end
