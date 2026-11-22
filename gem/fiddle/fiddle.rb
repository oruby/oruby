module Fiddle
  VERSION = "1.1.8"
  WINDOWS = FFI::Platform.windows?
  BUILD_RUBY_PLATFORM = RUBY_PLATFORM

  class Error < StandardError; end
  class DLError < Error; end
  class ClearedReferenceError < Error; end

  RTLD_LAZY   = FFI::DynamicLibrary::RTLD_LAZY
  RTLD_NOW    = FFI::DynamicLibrary::RTLD_NOW
  RTLD_GLOBAL = FFI::DynamicLibrary::RTLD_GLOBAL

  # Exact MRI Fiddle::TYPE_* values (negative = unsigned)
  TYPE_VOID         = 0
  TYPE_VOIDP        = 1
  TYPE_CHAR         = 2
  TYPE_UCHAR        = -2
  TYPE_SHORT        = 3
  TYPE_USHORT       = -3
  TYPE_INT          = 4
  TYPE_UINT         = -4
  TYPE_LONG         = 5
  TYPE_ULONG        = -5
  TYPE_LONG_LONG    = 6
  TYPE_ULONG_LONG   = -6
  TYPE_FLOAT        = 7
  TYPE_DOUBLE       = 8
  TYPE_VARIADIC     = 9
  TYPE_CONST_STRING = 10
  TYPE_BOOL         = 11

  TYPE_INT8_T   = TYPE_CHAR
  TYPE_UINT8_T  = TYPE_UCHAR
  TYPE_INT16_T  = TYPE_SHORT
  TYPE_UINT16_T = TYPE_USHORT
  TYPE_INT32_T  = TYPE_INT
  TYPE_UINT32_T = TYPE_UINT
  TYPE_INT64_T  = TYPE_LONG_LONG
  TYPE_UINT64_T = TYPE_ULONG_LONG

  SIZEOF_VOIDP      = FFI::Type::POINTER.size
  SIZEOF_CHAR       = FFI::Type::INT8.size
  SIZEOF_UCHAR      = FFI::Type::UINT8.size
  SIZEOF_SHORT      = FFI::Type::INT16.size
  SIZEOF_USHORT     = FFI::Type::UINT16.size
  SIZEOF_INT        = FFI::Type::INT32.size
  SIZEOF_UINT       = FFI::Type::UINT32.size
  SIZEOF_LONG       = FFI::Type::LONG.size
  SIZEOF_ULONG      = FFI::Type::ULONG.size
  SIZEOF_LONG_LONG  = FFI::Type::INT64.size
  SIZEOF_ULONG_LONG = FFI::Type::UINT64.size
  SIZEOF_FLOAT      = FFI::Type::FLOAT32.size
  SIZEOF_DOUBLE     = FFI::Type::FLOAT64.size
  SIZEOF_BOOL       = FFI::Type::BOOL.size
  SIZEOF_CONST_STRING = SIZEOF_VOIDP
  SIZEOF_INT8_T     = 1
  SIZEOF_UINT8_T    = 1
  SIZEOF_INT16_T    = 2
  SIZEOF_UINT16_T   = 2
  SIZEOF_INT32_T    = 4
  SIZEOF_UINT32_T   = 4
  SIZEOF_INT64_T    = 8
  SIZEOF_UINT64_T   = 8
  SIZEOF_SIZE_T     = SIZEOF_VOIDP
  SIZEOF_SSIZE_T    = SIZEOF_VOIDP
  SIZEOF_PTRDIFF_T  = SIZEOF_VOIDP
  SIZEOF_INTPTR_T   = SIZEOF_VOIDP
  SIZEOF_UINTPTR_T  = SIZEOF_VOIDP

  TYPE_SIZE_T    = SIZEOF_SIZE_T == SIZEOF_ULONG ? TYPE_ULONG : TYPE_ULONG_LONG
  TYPE_SSIZE_T   = SIZEOF_SSIZE_T == SIZEOF_LONG ? TYPE_LONG : TYPE_LONG_LONG
  TYPE_PTRDIFF_T = TYPE_SSIZE_T
  TYPE_INTPTR_T  = TYPE_SSIZE_T
  TYPE_UINTPTR_T = TYPE_SIZE_T

  ALIGN_VOIDP      = FFI::Type::POINTER.alignment
  ALIGN_CHAR       = FFI::Type::INT8.alignment
  ALIGN_SHORT      = FFI::Type::INT16.alignment
  ALIGN_INT        = FFI::Type::INT32.alignment
  ALIGN_LONG       = FFI::Type::LONG.alignment
  ALIGN_LONG_LONG  = FFI::Type::INT64.alignment
  ALIGN_FLOAT      = FFI::Type::FLOAT32.alignment
  ALIGN_DOUBLE     = FFI::Type::FLOAT64.alignment
  ALIGN_BOOL       = FFI::Type::BOOL.alignment
  ALIGN_INT8_T     = ALIGN_CHAR
  ALIGN_INT16_T    = ALIGN_SHORT
  ALIGN_INT32_T    = ALIGN_INT
  ALIGN_INT64_T    = ALIGN_LONG_LONG
  ALIGN_SIZE_T     = ALIGN_VOIDP
  ALIGN_SSIZE_T    = ALIGN_VOIDP
  ALIGN_PTRDIFF_T  = ALIGN_VOIDP
  ALIGN_INTPTR_T   = ALIGN_VOIDP
  ALIGN_UINTPTR_T  = ALIGN_VOIDP

  FFI_TYPE_MAP = {
    TYPE_VOID         => FFI::Type::VOID,
    TYPE_VOIDP        => FFI::Type::POINTER,
    TYPE_CHAR         => FFI::Type::INT8,
    TYPE_UCHAR        => FFI::Type::UINT8,
    TYPE_SHORT        => FFI::Type::INT16,
    TYPE_USHORT       => FFI::Type::UINT16,
    TYPE_INT          => FFI::Type::INT32,
    TYPE_UINT         => FFI::Type::UINT32,
    TYPE_LONG         => FFI::Type::LONG,
    TYPE_ULONG        => FFI::Type::ULONG,
    TYPE_LONG_LONG    => FFI::Type::INT64,
    TYPE_ULONG_LONG   => FFI::Type::UINT64,
    TYPE_FLOAT        => FFI::Type::FLOAT32,
    TYPE_DOUBLE       => FFI::Type::FLOAT64,
    TYPE_CONST_STRING => FFI::Type::STRING,
    TYPE_BOOL         => FFI::Type::BOOL,
  }

  def self.__to_ffi_type(t)
    return t if t.is_a?(FFI::Type)
    ft = FFI_TYPE_MAP[t]
    return ft if ft
    FFI.find_type(t)
  end

  def self.last_error
    FFI::LastError.error
  end

  def self.last_error=(err)
    FFI::LastError.error = err
  end

  def self.win32_last_error
    FFI::LastError.error
  end

  def self.win32_last_error=(err)
    FFI::LastError.error = err
  end

  # GC-safe object <-> integer handle registry for dlwrap / dlunwrap
  @__dlwrap_seq = 0
  @__dlwrap_o2i = {}
  @__dlwrap_i2o = {}

  def self.dlwrap(obj)
    id = @__dlwrap_o2i[obj.object_id]
    unless id
      @__dlwrap_seq += 1
      id = @__dlwrap_seq
      @__dlwrap_o2i[obj.object_id] = id
      @__dlwrap_i2o[id] = obj
    end
    id
  end

  def self.dlunwrap(addr)
    @__dlwrap_i2o[addr]
  end

  def self.dlopen(library)
    Handle.new(library)
  end

  # -------------------------------------------------------------------------
  # Fiddle::Pointer
  # -------------------------------------------------------------------------

  class Pointer
    attr_accessor :size, :free

    def self.malloc(size, freefunc = nil)
      addr = Fiddle.malloc(size)
      ptr = new(addr, size, freefunc)
      if block_given?
        begin
          return yield(ptr)
        ensure
          ptr.call_free if ptr.free && !ptr.freed?
          Fiddle.free(ptr.to_i) unless ptr.free || ptr.freed?
        end
      end
      ptr
    end

    def self.to_ptr(val)
      case val
      when Pointer
        val
      when FFI::Pointer
        new(val.address, val.size >= 0 ? val.size : 0)
      when String
        p = malloc(val.bytesize + 1, Fiddle::RUBY_FREE)
        p[0, val.bytesize] = val
        p.size = val.bytesize
        p
      when Integer
        new(val)
      when nil
        NULL
      else
        if val.respond_to?(:to_ptr)
          val.to_ptr
        else
          raise DLError, "cannot convert #{val.class} into Fiddle::Pointer"
        end
      end
    end

    class << self
      alias [] to_ptr
    end

    def initialize(addr, size = 0, freefunc = nil)
      @addr =
        case addr
        when Integer then addr
        when Pointer then addr.to_i
        when FFI::Pointer then addr.address
        else addr.to_i
        end
      @ffi_ptr = FFI::Pointer.new(@addr)
      @size = size || 0
      @free = freefunc
      @freed = false
    end

    def to_i
      @addr
    end
    alias to_int to_i

    def to_ptr
      self
    end

    def __ffi_ptr
      @ffi_ptr
    end

    def null?
      @addr == 0
    end

    def freed?
      @freed
    end

    def call_free
      return if @freed || @addr == 0 || @free.nil?
      @freed = true
      if @free == Fiddle::RUBY_FREE
        Fiddle.free(@addr)
      elsif @free.respond_to?(:call)
        @free.call(self)
      elsif @free.is_a?(Integer) && @free != 0
        Function.new(@free, [TYPE_VOIDP], TYPE_VOID).call(self)
      end
      nil
    end

    def to_str(len = nil)
      raise ClearedReferenceError, "freed pointer" if @freed
      return "" if @addr == 0
      if len
        @ffi_ptr.get_bytes(0, len)
      elsif @size > 0
        @ffi_ptr.get_bytes(0, @size)
      else
        @ffi_ptr.get_string(0)
      end
    end

    def to_s(len = nil)
      raise ClearedReferenceError, "freed pointer" if @freed
      return "" if @addr == 0
      if len
        @ffi_ptr.get_string(0, len)
      else
        @ffi_ptr.get_string(0)
      end
    end

    def to_value
      Fiddle.dlunwrap(@addr)
    end

    def +(delta)
      Pointer.new(@addr + delta, @size > delta ? @size - delta : 0)
    end

    def -(delta)
      Pointer.new(@addr - delta, @size + delta)
    end

    def [](index, length = nil)
      raise ClearedReferenceError, "freed pointer" if @freed
      if length
        @ffi_ptr.get_bytes(index, length)
      else
        @ffi_ptr.get_int8(index)
      end
    end

    def []=(index, len_or_val, val = nil)
      raise ClearedReferenceError, "freed pointer" if @freed
      if val.nil?
        if len_or_val.is_a?(String)
          @ffi_ptr.put_bytes(index, len_or_val)
        else
          @ffi_ptr.put_int8(index, len_or_val)
        end
        len_or_val
      else
        @ffi_ptr.put_bytes(index, val.to_s, 0, len_or_val)
        val
      end
    end

    def ptr
      Pointer.new(@ffi_ptr.get_pointer(0).address)
    end

    def ref
      p = Pointer.malloc(SIZEOF_VOIDP, Fiddle::RUBY_FREE)
      p.__ffi_ptr.put_pointer(0, @ffi_ptr)
      p
    end

    def ==(other)
      return false unless other.is_a?(Pointer)
      @addr == other.to_i
    end
    alias eql? ==

    def <=>(other)
      return nil unless other.is_a?(Pointer)
      @addr <=> other.to_i
    end

    def inspect
      "#<#{self.class}:0x#{@addr.to_s(16)} size=#{@size}>"
    end
  end

  NULL = Pointer.new(0)

  # -------------------------------------------------------------------------
  # Fiddle::Handle
  # -------------------------------------------------------------------------

  class Handle
    def self.sym(func)
      DEFAULT.sym(func)
    end

    class << self
      alias [] sym
    end

    def initialize(library = nil, flags = RTLD_LAZY | RTLD_GLOBAL)
      @library = library
      @flags = flags
      @closed = false
      @enable_close = false
      @dl =
        if library.nil?
          FFI::DynamicLibrary.open(nil, flags)
        else
          begin
            FFI::DynamicLibrary.open(library.to_s, flags)
          rescue LoadError => e
            raise DLError, e.message
          end
        end
      if block_given?
        begin
          yield self
        ensure
          close
        end
      end
    end

    def sym(func)
      raise DLError, "closed handle" if @closed
      p = @dl.find_symbol(func.to_s)
      raise DLError, "unknown symbol \"#{func}\"" unless p
      p.address
    end
    alias [] sym

    def file_name
      @library
    end

    def close
      @closed = true
      0
    end

    def enable_close
      @enable_close = true
    end

    def disable_close
      @enable_close = false
    end

    def close_enabled?
      @enable_close
    end

    def to_i
      0
    end

    DEFAULT = new(nil)
  end

  # -------------------------------------------------------------------------
  # Fiddle::Function
  # -------------------------------------------------------------------------

  class Function
    DEFAULT = 1
    STDCALL = 2

    attr_reader :abi, :ptr, :name

    def initialize(ptr, args, ret_type, abi = DEFAULT, kwargs = nil, name: nil, need_gvl: false)
      @ptr = ptr.is_a?(Pointer) ? ptr.to_i : ptr.to_i
      @args = args
      @ret_type = ret_type
      @abi = abi
      @name = name
      @need_gvl = need_gvl
      conv = abi == STDCALL ? "stdcall" : nil

      @ffi_ret = Fiddle.__to_ffi_type(ret_type)
      @variadic = args.last == TYPE_VARIADIC

      if @variadic
        fixed = args[0...-1].map { |t| Fiddle.__to_ffi_type(t) }
        @invoker = FFI::VariadicInvoker.new(FFI::Pointer.new(@ptr), fixed, @ffi_ret, nil, conv)
        @fixed_types = args[0...-1]
      else
        @ffi_args = args.map { |t| Fiddle.__to_ffi_type(t) }
        @fn = FFI::Function.__create(@ffi_ret, @ffi_args, FFI::Pointer.new(@ptr), conv)
      end
    end

    def need_gvl?
      @need_gvl
    end

    def to_i
      @ptr
    end

    def to_proc
      method(:call).to_proc
    end

    def __convert_arg(val, ftype)
      case ftype
      when TYPE_VOIDP
        case val
        when Pointer then val.__ffi_ptr
        when FFI::Pointer then val
        when String then val
        when Integer then FFI::Pointer.new(val)
        when nil then FFI::Pointer::NULL
        else
          val.respond_to?(:to_ptr) ? val.to_ptr.__ffi_ptr : val
        end
      when TYPE_CONST_STRING
        val.is_a?(Pointer) ? val.__ffi_ptr : val
      else
        val
      end
    end

    def __convert_ret(res)
      if @ret_type == TYPE_VOIDP
        res.is_a?(FFI::Pointer) ? Pointer.new(res.address) : Pointer.new(res.to_i)
      else
        res
      end
    end

    def call(*call_args)
      if @variadic
        nfixed = @fixed_types.length
        fixed_vals = call_args[0, nfixed].each_with_index.map { |v, i| __convert_arg(v, @fixed_types[i]) }
        rest = call_args[nfixed..-1] || []
        pairs = []
        i = 0
        while i < rest.length
          ft = rest[i]
          fv = rest[i + 1]
          pairs << Fiddle.__to_ffi_type(ft)
          pairs << __convert_arg(fv, ft)
          i += 2
        end
        res = @invoker.call(*(fixed_vals + pairs))
        return __convert_ret(res)
      end

      converted = call_args.each_with_index.map { |v, i| __convert_arg(v, @args[i]) }
      __convert_ret(@fn.call(*converted))
    end
  end

  # -------------------------------------------------------------------------
  # Fiddle::Closure & Fiddle::Closure::BlockCaller
  # -------------------------------------------------------------------------

  class Closure
    attr_reader :ctype, :args, :abi

    def initialize(ret, args, abi = Function::DEFAULT)
      @ctype = ret
      @args = args
      @abi = abi
      @freed = false

      ffi_ret = Fiddle.__to_ffi_type(ret)
      ffi_args = args.map { |t| Fiddle.__to_ffi_type(t) }
      conv = abi == Function::STDCALL ? "stdcall" : nil

      this = self
      wrapper = lambda do |*raw_args|
        rb_args = raw_args.each_with_index.map do |v, i|
          args[i] == TYPE_VOIDP && v.is_a?(FFI::Pointer) ? Pointer.new(v.address) : v
        end
        res = this.call(*rb_args)
        if ret == TYPE_VOIDP
          res.is_a?(Pointer) ? res.__ffi_ptr : (res.nil? ? FFI::Pointer::NULL : FFI::Pointer.new(res.to_i))
        else
          res
        end
      end

      @ffi_fn = FFI::Function.__create(ffi_ret, ffi_args, wrapper, conv)
    end

    def to_i
      @ffi_fn.address
    end

    def to_ptr
      Pointer.new(to_i)
    end

    def free
      return if @freed
      @freed = true
      @ffi_fn.free
      nil
    end

    def freed?
      @freed
    end

    class BlockCaller < Closure
      def initialize(ctype, args, abi = Function::DEFAULT, &block)
        @block = block
        super(ctype, args, abi)
      end

      def call(*args)
        @block.call(*args)
      end
    end
  end

  # -------------------------------------------------------------------------
  # Fiddle::CParser
  # -------------------------------------------------------------------------

  module CParser
    def parse_struct_signature(signature, tymap = nil)
      if signature.is_a?(String)
        signature = signature.split(/\s*,\s*/)
      end
      mems = []
      tys = []
      signature.each do |msig|
        tks = msig.strip.split(/\s+(\*?)/)
        ty_str = msig.strip
        if (m = /\A(.+?)(\*?)\s*([A-Za-z_]\w*)(?:\s*\[\s*(\d+)\s*\])?\z/.match(ty_str))
          base_ty = (m[1].strip + (m[2].empty? ? "" : " *")).strip
          name = m[3]
          arr_len = m[4] ? m[4].to_i : nil
          ct = parse_ctype(base_ty, tymap)
          tys << (arr_len ? [ct, arr_len] : ct)
          mems << name
        else
          raise DLError, "cannot parse struct member: #{msig.inspect}"
        end
      end
      [tys, mems]
    end

    def parse_signature(signature, tymap = nil)
      sig = signature.gsub(/\s+/, " ").strip
      m = /\A(.+?)\s*([A-Za-z_]\w*)\s*\((.*)\)\z/.match(sig)
      raise DLError, "cannot parse function signature: #{signature.inspect}" unless m
      ret_str = m[1].strip
      func_name = m[2].strip
      args_str = m[3].strip

      ret_type = parse_ctype(ret_str, tymap)
      arg_types = []
      if !args_str.empty? && args_str != "void"
        args_str.split(",").each do |part|
          p = part.strip
          if p == "..."
            arg_types << TYPE_VARIADIC
          else
            # Strip trailing parameter name if present (e.g. "int count" or "const char *buf")
            if p.end_with?("*")
              ty_part = p
            elsif (pm = /\A(.+?[\s*])[A-Za-z_]\w*(?:\[\d*\])?\z/.match(p))
              candidate = pm[1].strip
              # Make sure candidate is actually a type, not just "unsigned" or "const"
              if %w[unsigned signed long short struct union enum const].include?(candidate)
                ty_part = p
              else
                ty_part = candidate
              end
            else
              ty_part = p
            end
            arg_types << parse_ctype(ty_part, tymap)
          end
        end
      end
      [func_name, ret_type, arg_types]
    end

    def parse_ctype(ty, tymap = nil)
      ty = ty.to_s.gsub(/\s+/, " ").strip
      return tymap[ty] if tymap && tymap.key?(ty)
      return TYPE_VOIDP if ty.end_with?("*") || ty.include?("*")
      ty = ty.sub(/\Aconst\s+/, "").strip
      return tymap[ty] if tymap && tymap.key?(ty)

      case ty
      when "void"                     then TYPE_VOID
      when "bool", "_Bool"            then TYPE_BOOL
      when "char", "signed char", "int8_t" then TYPE_CHAR
      when "unsigned char", "uchar", "uint8_t" then TYPE_UCHAR
      when "short", "short int", "signed short", "int16_t" then TYPE_SHORT
      when "unsigned short", "unsigned short int", "ushort", "uint16_t" then TYPE_USHORT
      when "int", "signed int", "signed", "int32_t" then TYPE_INT
      when "unsigned int", "unsigned", "uint", "uint32_t" then TYPE_UINT
      when "long", "long int", "signed long" then TYPE_LONG
      when "unsigned long", "unsigned long int", "ulong" then TYPE_ULONG
      when "long long", "long long int", "signed long long", "int64_t" then TYPE_LONG_LONG
      when "unsigned long long", "unsigned long long int", "ulong_long", "uint64_t" then TYPE_ULONG_LONG
      when "float"                    then TYPE_FLOAT
      when "double"                   then TYPE_DOUBLE
      when "size_t"                   then TYPE_SIZE_T
      when "ssize_t"                  then TYPE_SSIZE_T
      when "ptrdiff_t"                then TYPE_PTRDIFF_T
      when "intptr_t"                 then TYPE_INTPTR_T
      when "uintptr_t"                then TYPE_UINTPTR_T
      when "..."                      then TYPE_VARIADIC
      else
        raise DLError, "unknown type: #{ty.inspect}"
      end
    end
  end

  # -------------------------------------------------------------------------
  # Fiddle::CStruct / CUnion / CStructBuilder
  # -------------------------------------------------------------------------

  class CStruct
    def self.entity_class
      CStructEntity
    end
  end

  class CUnion < CStruct
    def self.entity_class
      CUnionEntity
    end
  end

  class CStructEntity < Pointer
    def self.size(types)
      offset = 0
      max_align = 1
      types.each do |t|
        ft, count = t.is_a?(Array) ? [t[0], t[1]] : [t, 1]
        ffi_t = Fiddle.__to_ffi_type(ft)
        al = ffi_t.alignment
        max_align = al if al > max_align
        rem = offset % al
        offset += (al - rem) if rem != 0
        offset += ffi_t.size * count
      end
      rem = offset % max_align
      rem == 0 ? offset : offset + (max_align - rem)
    end

    def self.malloc(types, func = nil)
      sz = size(types)
      new(Fiddle.malloc(sz), types, func)
    end

    def initialize(addr, types, func = nil)
      @ctypes = types
      @offsets = compute_offsets(types)
      super(addr, self.class.size(types), func)
    end

    def compute_offsets(types)
      offs = []
      offset = 0
      types.each do |t|
        ft, count = t.is_a?(Array) ? [t[0], t[1]] : [t, 1]
        ffi_t = Fiddle.__to_ffi_type(ft)
        al = ffi_t.alignment
        rem = offset % al
        offset += (al - rem) if rem != 0
        offs << offset
        offset += ffi_t.size * count
      end
      offs
    end

    def assign_names(names)
      @members = names.map(&:to_s)
    end

    def [](name)
      idx = @members.index(name.to_s)
      raise ArgumentError, "no such member #{name.inspect}" unless idx
      off = @offsets[idx]
      t = @ctypes[idx]
      if t.is_a?(Array)
        ft, count = t
        ffi_t = Fiddle.__to_ffi_type(ft)
        Array.new(count) { |i| read_typed(off + i * ffi_t.size, ft) }
      else
        read_typed(off, t)
      end
    end

    def []=(name, val)
      idx = @members.index(name.to_s)
      raise ArgumentError, "no such member #{name.inspect}" unless idx
      off = @offsets[idx]
      t = @ctypes[idx]
      if t.is_a?(Array)
        ft, count = t
        ffi_t = Fiddle.__to_ffi_type(ft)
        if val.is_a?(String)
          @ffi_ptr.put_bytes(off, val, 0, [val.bytesize, count].min)
        else
          val.each_with_index { |v, i| write_typed(off + i * ffi_t.size, ft, v) if i < count }
        end
      else
        write_typed(off, t, val)
      end
      val
    end

    private

    def read_typed(off, t)
      case t
      when TYPE_BOOL       then @ffi_ptr.get_uint8(off) != 0
      when TYPE_CHAR       then @ffi_ptr.get_int8(off)
      when TYPE_UCHAR      then @ffi_ptr.get_uint8(off)
      when TYPE_SHORT      then @ffi_ptr.get_int16(off)
      when TYPE_USHORT     then @ffi_ptr.get_uint16(off)
      when TYPE_INT        then @ffi_ptr.get_int32(off)
      when TYPE_UINT       then @ffi_ptr.get_uint32(off)
      when TYPE_LONG       then @ffi_ptr.get_long(off)
      when TYPE_ULONG      then @ffi_ptr.get_ulong(off)
      when TYPE_LONG_LONG  then @ffi_ptr.get_int64(off)
      when TYPE_ULONG_LONG then @ffi_ptr.get_uint64(off)
      when TYPE_FLOAT      then @ffi_ptr.get_float32(off)
      when TYPE_DOUBLE     then @ffi_ptr.get_float64(off)
      when TYPE_VOIDP      then Pointer.new(@ffi_ptr.get_pointer(off).address)
      else
        raise DLError, "unsupported struct field type #{t.inspect}"
      end
    end

    def write_typed(off, t, v)
      case t
      when TYPE_BOOL       then @ffi_ptr.put_uint8(off, v ? 1 : 0)
      when TYPE_CHAR       then @ffi_ptr.put_int8(off, v)
      when TYPE_UCHAR      then @ffi_ptr.put_uint8(off, v)
      when TYPE_SHORT      then @ffi_ptr.put_int16(off, v)
      when TYPE_USHORT     then @ffi_ptr.put_uint16(off, v)
      when TYPE_INT        then @ffi_ptr.put_int32(off, v)
      when TYPE_UINT       then @ffi_ptr.put_uint32(off, v)
      when TYPE_LONG       then @ffi_ptr.put_long(off, v)
      when TYPE_ULONG      then @ffi_ptr.put_ulong(off, v)
      when TYPE_LONG_LONG  then @ffi_ptr.put_int64(off, v)
      when TYPE_ULONG_LONG then @ffi_ptr.put_uint64(off, v)
      when TYPE_FLOAT      then @ffi_ptr.put_float32(off, v)
      when TYPE_DOUBLE     then @ffi_ptr.put_float64(off, v)
      when TYPE_VOIDP
        p = v.is_a?(Pointer) ? v.__ffi_ptr : (v.nil? ? FFI::Pointer::NULL : FFI::Pointer.new(v.to_i))
        @ffi_ptr.put_pointer(off, p)
      else
        raise DLError, "unsupported struct field type #{t.inspect}"
      end
    end
  end

  class CUnionEntity < CStructEntity
    def self.size(types)
      max_sz = 0
      types.each do |t|
        ft, count = t.is_a?(Array) ? [t[0], t[1]] : [t, 1]
        sz = Fiddle.__to_ffi_type(ft).size * count
        max_sz = sz if sz > max_sz
      end
      max_sz
    end

    def compute_offsets(types)
      Array.new(types.length, 0)
    end
  end

  module CStructBuilder
    def self.create(klass, types, members)
      new_class = Class.new(klass) do
        define_singleton_method(:types) { types }
        define_singleton_method(:members) { members }
        define_singleton_method(:size) { klass.entity_class.size(types) }
        define_singleton_method(:malloc) do |func = Fiddle::RUBY_FREE, &block|
          ent = new(klass.entity_class.malloc(types, func))
          if block
            begin
              return block.call(ent)
            ensure
              ent.to_ptr.call_free
            end
          end
          ent
        end

        def initialize(addr)
          @entity =
            if addr.is_a?(CStructEntity)
              addr
            else
              self.class.superclass.entity_class.new(addr, self.class.types)
            end
          @entity.assign_names(self.class.members)
        end

        def to_ptr
          @entity
        end

        def to_i
          @entity.to_i
        end

        members.each do |name|
          define_method(name) { @entity[name] }
          define_method("#{name}=") { |v| @entity[name] = v }
        end
      end
      new_class
    end
  end

  # -------------------------------------------------------------------------
  # Fiddle::CompositeHandler & Fiddle::Importer
  # -------------------------------------------------------------------------

  class CompositeHandler
    attr_reader :handlers

    def initialize(handlers)
      @handlers = handlers
    end

    def sym(symbol)
      @handlers.each do |h|
        begin
          return h.sym(symbol)
        rescue DLError
        end
      end
      raise DLError, "unknown symbol \"#{symbol}\""
    end
    alias [] sym
  end

  module Importer
    include CParser

    def dlload(*libs)
      handles = libs.map do |lib|
        lib.is_a?(Handle) ? lib : Fiddle.dlopen(lib)
      end
      @handler = CompositeHandler.new(handles)
      @func_map = {}
      @type_alias = {}
    end

    def handler
      @handler ||= CompositeHandler.new([Handle::DEFAULT])
    end

    def typealias(alias_type, orig_type)
      @type_alias ||= {}
      @type_alias[alias_type.to_s] = parse_ctype(orig_type.to_s, @type_alias)
    end

    def sizeof(ty)
      if ty.is_a?(Class) && ty.respond_to?(:size)
        return ty.size
      end
      ct = parse_ctype(ty.to_s, @type_alias)
      Fiddle.__to_ffi_type(ct).size
    end

    def struct(signature)
      tys, mems = parse_struct_signature(signature, @type_alias)
      CStructBuilder.create(CStruct, tys, mems)
    end

    def union(signature)
      tys, mems = parse_struct_signature(signature, @type_alias)
      CStructBuilder.create(CUnion, tys, mems)
    end

    def extern(signature, *opts)
      symname, ctype, argtype = parse_signature(signature, @type_alias)
      abi = opts.include?(:stdcall) ? Function::STDCALL : Function::DEFAULT
      addr = handler.sym(symname)
      f = Function.new(addr, argtype, ctype, abi, name: symname)
      @func_map ||= {}
      @func_map[symname] = f

      define_singleton_method(symname) { |*args| f.call(*args) }
      define_method(symname) { |*args| f.call(*args) }
      f
    end

    def bind(signature, *opts, &block)
      name, ctype, argtype = parse_signature(signature, @type_alias)
      abi = opts.include?(:stdcall) ? Function::STDCALL : Function::DEFAULT
      Closure::BlockCaller.new(ctype, argtype, abi, &block)
    end
    alias bind_function bind

    def import_symbol(name)
      Pointer.new(handler.sym(name.to_s))
    end
  end

  module BasicTypes
    def self.included(m)
      m.typealias("uint", "unsigned int")
      m.typealias("ulong", "unsigned long")
      m.typealias("uchar", "unsigned char")
      m.typealias("ushort", "unsigned short")
    end
  end

  module Win32Types
    def self.included(m)
      m.typealias("DWORD", "unsigned long")
      m.typealias("PDWORD", "unsigned long *")
      m.typealias("WORD", "unsigned short")
      m.typealias("BYTE", "unsigned char")
      m.typealias("BOOL", "int")
      m.typealias("HANDLE", "void *")
      m.typealias("HWND", "void *")
      m.typealias("LPCSTR", "const char *")
      m.typealias("LPSTR", "char *")
      m.typealias("LPVOID", "void *")
    end
  end
end
