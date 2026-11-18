# IO::Buffer as in Ruby 4.1: IO::Buffer::Storage owns (or borrows) the bytes,
# IO::Buffer::Slice is an [offset, length) view of a parent buffer, resolved
# through its parent chain on every access. Memory is a binary String (or,
# for IO::Buffer.for / .string, the caller's String); IO::Buffer.map reads
# the file region and writes changes back to the file for shared mappings.
class IO
  class Buffer
    include Comparable

    EXTERNAL = 1
    INTERNAL = 2
    MAPPED = 4
    SHARED = 8
    LOCKED = 32
    PRIVATE = 64
    READONLY = 128

    LITTLE_ENDIAN = 4
    BIG_ENDIAN = 8
    HOST_ENDIAN = 4
    NETWORK_ENDIAN = 8

    # PAGE_SIZE is defined by core_ext (Go) before this file loads
    DEFAULT_SIZE = PAGE_SIZE
    MAP_ALIGNMENT = PAGE_SIZE

    class AccessError < RuntimeError; end
    class AllocationError < RuntimeError; end
    class LockedError < RuntimeError; end
    class InvalidatedError < RuntimeError; end
    class MaskError < ArgumentError; end

    TYPES = {
      U8: ["C", 1], S8: ["c", 1],
      u16: ["S<", 2], U16: ["S>", 2], s16: ["s<", 2], S16: ["s>", 2],
      u32: ["L<", 4], U32: ["L>", 4], s32: ["l<", 4], S32: ["l>", 4],
      u64: ["Q<", 8], U64: ["Q>", 8], s64: ["q<", 8], S64: ["q>", 8],
      f32: ["e", 4], F32: ["g", 4], f64: ["E", 8], F64: ["G", 8]
    }.freeze

    class << self
      def new(*args, **kw, &block)
        return Storage.new(*args, **kw, &block) if equal?(IO::Buffer)
        super
      end

      def allocate
        raise TypeError, "allocator undefined for #{self}" if equal?(IO::Buffer)
        super
      end

      def size_of(type)
        return type.sum { |t| size_of(t) } if type.is_a?(Array)
        info = TYPES[type] or raise ArgumentError, "Invalid type name!"
        info[1]
      end

      def for(string, &block)
        Storage.for(string, &block)
      end

      def map(*args)
        Storage.map(*args)
      end

      def string(size, &block)
        Storage.string(size, &block)
      end

      # helpers shared by the views
      def __oruby_int(v)
        raise TypeError, "not an Integer" unless v.is_a?(Integer)
        v
      end

      def __oruby_type(type)
        TYPES[type] or raise ArgumentError, "Invalid type name!"
      end
    end

    # --- view protocol (resolved on each access) ---------------------------

    # [storage, absolute offset, length] or raises InvalidatedError
    def __oruby_view
      v = __oruby_resolve
      raise InvalidatedError, "Buffer has been invalidated!" unless v
      v
    end

    def valid?
      !__oruby_resolve.nil?
    end

    def empty?
      size == 0
    end

    def locked?
      __oruby_root.__oruby_st[:locks][0] > 0
    end

    def locked
      raise LocalJumpError, "no block given" unless block_given?
      __oruby_view
      locks = __oruby_root.__oruby_st[:locks]
      locks[0] += 1
      begin
        yield self
      ensure
        locks[0] -= 1
      end
    end

    def slice(offset = 0, length = nil)
      Slice.new(self, offset, length)
    end

    def __oruby_bounds(offset, length, size)
      offset = Buffer.__oruby_int(offset)
      raise ArgumentError, "Offset can't be negative!" if offset < 0
      raise ArgumentError, "Specified offset exceeds buffer size!" if offset > size
      length = length.nil? ? size - offset : Buffer.__oruby_int(length)
      raise ArgumentError, "Length can't be negative!" if length < 0
      raise ArgumentError, "Specified offset+length is bigger than the buffer size!" if offset + length > size
      [offset, length]
    end

    def __oruby_read(offset, length)
      st, base, = __oruby_view
      st.__oruby_bytes(base + offset, length)
    end

    def __oruby_write(offset, bytes)
      st, base, = __oruby_view
      raise AccessError, "Buffer is not writable!" if readonly?
      st.__oruby_put(base + offset, bytes)
    end

    def get_string(offset = 0, length = nil, encoding = Encoding::BINARY)
      _, _, size = __oruby_view
      offset, length = __oruby_bounds(offset, length, size)
      __oruby_read(offset, length).force_encoding(encoding)
    end

    def set_string(string, offset = 0, length = nil, source_offset = 0)
      _, _, size = __oruby_view
      raise AccessError, "Buffer is not writable!" if readonly?
      string = Kernel.__implicit(string, String).b
      source_offset = Buffer.__oruby_int(source_offset)
      raise ArgumentError, "Source offset can't be negative!" if source_offset < 0
      raise ArgumentError, "Source offset exceeds source size!" if source_offset > string.bytesize
      length = length.nil? ? string.bytesize - source_offset : Buffer.__oruby_int(length)
      offset, length = __oruby_bounds(offset, length, size)
      raise ArgumentError, "Source length exceeds source size!" if source_offset + length > string.bytesize
      __oruby_write(offset, string.byteslice(source_offset, length))
      length
    end

    def __oruby_typed(type, offset)
      fmt, width = Buffer.__oruby_type(type)
      _, _, size = __oruby_view
      offset = Buffer.__oruby_int(offset)
      if offset < 0 || offset + width > size
        raise ArgumentError, "Type extends beyond end of buffer! (offset=#{offset} > size=#{size})"
      end
      [fmt, width, offset]
    end

    def get_value(type, offset)
      fmt, width, offset = __oruby_typed(type, offset)
      __oruby_read(offset, width).unpack1(fmt)
    end

    def set_value(type, offset, value)
      fmt, width, offset = __oruby_typed(type, offset)
      __oruby_write(offset, [value].pack(fmt))
      offset + width
    end

    def get_values(types, offset)
      raise ArgumentError, "Argument types must be an array!" unless types.is_a?(Array)
      types.map do |t|
        v = get_value(t, offset)
        offset += Buffer.size_of(t)
        v
      end
    end

    def set_values(types, offset, values)
      raise ArgumentError, "Argument types must be an array!" unless types.is_a?(Array)
      raise ArgumentError, "Argument values must be an array!" unless values.is_a?(Array)
      raise ArgumentError, "Argument types and values must have the same length!" unless types.size == values.size
      types.each_with_index { |t, i| offset = set_value(t, offset, values[i]) }
      offset
    end

    def each(type = :U8, offset = 0, count = nil)
      return to_enum(:each, type, offset, count) unless block_given?
      width = Buffer.size_of(type)
      count ||= (size - offset) / width
      count.times do
        yield offset, get_value(type, offset)
        offset += width
      end
      self
    end

    def values(type = :U8, offset = 0, count = nil)
      each(type, offset, count).map { |_, v| v }
    end

    def each_byte(offset = 0, count = nil)
      return to_enum(:each_byte, offset, count) unless block_given?
      count ||= size - offset
      __oruby_read(offset, count).each_byte { |b| yield b }
      self
    end

    def copy(source, offset = 0, length = nil, source_offset = 0)
      _, _, size = __oruby_view
      raise AccessError, "Buffer is not writable!" if readonly?
      bytes = source.is_a?(Buffer) ? source.get_string : Kernel.__implicit(source, String).b
      source_offset = Buffer.__oruby_int(source_offset)
      raise ArgumentError, "Source offset exceeds source size!" if source_offset > bytes.bytesize
      length = length.nil? ? bytes.bytesize - source_offset : Buffer.__oruby_int(length)
      offset, length = __oruby_bounds(offset, length, size)
      raise ArgumentError, "Specified source offset+length is bigger than the source buffer size!" if source_offset + length > bytes.bytesize
      __oruby_write(offset, bytes.byteslice(source_offset, length))
      length
    end

    def clear(value = 0, offset = 0, length = nil)
      _, _, size = __oruby_view
      offset, length = __oruby_bounds(offset, length, size)
      __oruby_write(offset, ([value & 0xff].pack("C") * length))
      self
    end

    def <=>(other)
      return nil unless other.is_a?(Buffer)
      a = get_string
      b = other.get_string
      return a.bytesize <=> b.bytesize unless a.bytesize == b.bytesize
      a <=> b
    end

    def __oruby_mask(mask)
      unless mask.is_a?(Buffer)
        name = mask.nil? ? "nil" : mask.class.to_s
        raise TypeError, "wrong argument type #{name} (expected IO::Buffer)"
      end
      m = mask.get_string
      raise MaskError, "Zero-length mask given!" if m.empty?
      m.bytes
    end

    def __oruby_masked(mask, op)
      m = __oruby_mask(mask)
      src = get_string.bytes
      out = src.each_with_index.map { |b, i| op.call(b, m[i % m.size]) }
      out.pack("C*")
    end

    def &(mask)
      Buffer.__oruby_new_internal(__oruby_masked(mask, ->(a, b) { a & b }))
    end

    def |(mask)
      Buffer.__oruby_new_internal(__oruby_masked(mask, ->(a, b) { a | b }))
    end

    def ^(mask)
      Buffer.__oruby_new_internal(__oruby_masked(mask, ->(a, b) { a ^ b }))
    end

    def ~
      Buffer.__oruby_new_internal(get_string.bytes.map { |b| ~b & 0xff }.pack("C*"))
    end

    def __oruby_mask_in_place(mask, op)
      m = __oruby_mask(mask)
      st, base, len = __oruby_view
      raise AccessError, "Buffer is not writable!" if readonly?
      mst, mbase, mlen = mask.__oruby_view
      if len > 0 && st.equal?(mst) && base < mbase + mlen && mbase < base + len
        raise MaskError, "Mask overlaps source buffer!"
      end
      return self if len == 0
      src = get_string.bytes
      __oruby_write(0, src.each_with_index.map { |b, i| op.call(b, m[i % m.size]) }.pack("C*"))
      self
    end

    def and!(mask)
      __oruby_mask_in_place(mask, ->(a, b) { a & b })
    end

    def or!(mask)
      __oruby_mask_in_place(mask, ->(a, b) { a | b })
    end

    def xor!(mask)
      __oruby_mask_in_place(mask, ->(a, b) { a ^ b })
    end

    def not!
      raise AccessError, "Buffer is not writable!" if readonly?
      __oruby_write(0, get_string.bytes.map { |b| ~b & 0xff }.pack("C*"))
      self
    end

    def bit_count(offset = 0, length = nil)
      _, _, size = __oruby_view
      offset, length = __oruby_bounds(offset, length, size)
      __oruby_read(offset, length).each_byte.sum { |b| b.to_s(2).count("1") }
    end

    # read(io, offset = 0, length = rest): bytes read from io into the buffer
    def read(io, offset = 0, length = nil)
      _, _, size = __oruby_view
      offset, length = __oruby_bounds(offset, length, size)
      return 0 if length == 0
      data = io.read(length)
      return 0 if data.nil?
      __oruby_write(offset, data.b)
      data.bytesize
    end

    # write(io, offset = 0, length = rest): bytes written to io
    def write(io, offset = 0, length = nil)
      _, _, size = __oruby_view
      offset, length = __oruby_bounds(offset, length, size)
      return 0 if length == 0
      io.write(__oruby_read(offset, length))
    end

    def pread(io, from, offset = 0, length = nil)
      _, _, size = __oruby_view
      offset, length = __oruby_bounds(offset, length, size)
      data = io.pread(length, from)
      __oruby_write(offset, data.b)
      data.bytesize
    end

    def pwrite(io, from, offset = 0, length = nil)
      _, _, size = __oruby_view
      offset, length = __oruby_bounds(offset, length, size)
      io.pwrite(__oruby_read(offset, length), from)
    end

    def to_s
      st = __oruby_root
      v = __oruby_resolve
      addr = st.__oruby_st[:id] + (v ? v[1] : 0)
      flags = __oruby_flag_names
      "#<#{self.class} 0x#{addr.to_s(16).rjust(16, '0')}+#{size}#{flags.map { |f| " #{f}" }.join}>"
    end

    def inspect
      s = to_s
      return s if !valid? || null? || size > 256 || size == 0
      "#{s.chomp('>')}>\n#{hexdump}"
    end

    def hexdump(offset = 0, length = nil, width = 16)
      bytes = get_string(offset, length).bytes
      lines = []
      bytes.each_slice(width).with_index do |row, i|
        hex = row.map { |b| b.to_s(16).rjust(2, "0") }.join(" ")
        text = row.map { |b| b >= 32 && b < 127 ? b.chr : "." }.join
        lines << "0x#{(offset + i * width).to_s(16).rjust(8, '0')}  #{hex.ljust(width * 3 - 1)} #{text}"
      end
      lines.join("\n")
    end

    def self.__oruby_new_internal(bytes)
      b = Storage.new(bytes.bytesize, bytes.bytesize > 0 ? INTERNAL : nil)
      b.__oruby_st[:data] = bytes.b if bytes.bytesize > 0
      b
    end

    def __oruby_frozen_error
      raise FrozenError.new("can't modify frozen #{self.class}: #{to_s}", receiver: self)
    end

    # --- Storage --------------------------------------------------------------

    class Storage < Buffer
      @__oruby_ids = 0x0000_6000_0000_0000

      def self.__oruby_next_id
        @__oruby_ids += 0x1000
      end

      def self.__oruby_state(data, kind, readonly: false, shared: false, private: false, external: false,
                             source: nil, advance: false, file: nil, file_offset: 0)
        { data: data, base: 0, size: data ? data.bytesize : 0, kind: kind, readonly: readonly,
          shared: shared, private: private, external: external, source: source, advance: advance,
          file: file, file_offset: file_offset, locks: [0], id: __oruby_next_id }
      end

      def __oruby_st
        @st
      end

      def __oruby_st=(st)
        @st = st
      end

      def initialize(size = DEFAULT_SIZE, flags = nil)
        size = Buffer.__oruby_int(size)
        raise ArgumentError, "Size can't be negative!" if size < 0
        unless flags.nil?
          flags = Buffer.__oruby_int(flags)
          raise ArgumentError, "Flags can't be negative!" if flags < 0
        end
        locks = @st ? @st[:locks] : [0]
        if size == 0
          @st = Storage.__oruby_state(nil, nil)
          @st[:locks] = locks
          return
        end
        flags ||= 0
        if flags & INTERNAL != 0 && flags & MAPPED != 0
          raise ArgumentError, "Flags can't include both IO::Buffer::INTERNAL and IO::Buffer::MAPPED!"
        end
        raise ArgumentError, "IO::Buffer::EXTERNAL can't be used with IO::Buffer.new!" if flags & EXTERNAL != 0
        if flags & INTERNAL != 0 && flags & (SHARED | PRIVATE) != 0
          raise ArgumentError, "IO::Buffer::SHARED and IO::Buffer::PRIVATE require IO::Buffer::MAPPED!"
        end
        if flags & SHARED != 0 && flags & PRIVATE != 0
          raise ArgumentError, "Flags can't include both IO::Buffer::SHARED and IO::Buffer::PRIVATE!"
        end
        kind = if flags & (MAPPED | SHARED | PRIVATE) != 0 then :mapped
               elsif flags & INTERNAL != 0 then :internal
               elsif size >= PAGE_SIZE then :mapped
               else :internal
               end
        @st = Storage.__oruby_state("\0".b * size, kind, readonly: flags & READONLY != 0,
                                    shared: flags & SHARED != 0, private: flags & PRIVATE != 0)
        @st[:locks] = locks
      end

      def initialize_copy(orig)
        super
        data = orig.null? ? nil : orig.get_string
        @st = Storage.__oruby_state(data, data && !data.empty? ? :internal : nil)
      end

      def self.for(string)
        string = Kernel.__implicit(string, String)
        b = allocate
        if block_given?
          b.__oruby_st = Storage.__oruby_state(string, nil, readonly: string.frozen?, external: true,
                                               source: string, advance: true)
          begin
            return yield(b)
          ensure
            b.__oruby_free!
          end
        end
        snap = string.frozen? ? string : string.dup.freeze
        b.__oruby_st = Storage.__oruby_state(snap, nil, readonly: true, external: true, source: snap, advance: true)
        b
      end

      def self.string(size)
        raise LocalJumpError, "no block given" unless block_given?
        size = Kernel.__to_int(size)
        raise RangeError, "bignum too big to convert into 'long'" if size > 0x7fff_ffff_ffff_ffff
        raise ArgumentError, "negative string size (or size too big)" if size < 0
        string = "\0".b * size
        b = allocate
        b.__oruby_st = Storage.__oruby_state(string, nil, external: true, source: string, advance: true)
        begin
          yield b
        ensure
          b.__oruby_free!
        end
        string
      end

      def self.map(file, size = nil, offset = 0, flags = 0)
        flags = Buffer.__oruby_int(flags)
        raise ArgumentError, "IO::Buffer::INTERNAL can't be used with IO::Buffer.map!" if flags & INTERNAL != 0
        raise ArgumentError, "IO::Buffer::EXTERNAL can't be used with IO::Buffer.map!" if flags & EXTERNAL != 0
        if flags & SHARED != 0 && flags & PRIVATE != 0
          raise ArgumentError, "Flags can't include both IO::Buffer::SHARED and IO::Buffer::PRIVATE!"
        end
        size = Buffer.__oruby_int(size) unless size.nil?
        offset = Kernel.__implicit(offset, Integer)
        raise ArgumentError, "Offset (#{offset}) can't be negative!" if offset < 0
        if offset % MAP_ALIGNMENT != 0
          raise ArgumentError, "Offset (#{offset}) must be a multiple of IO::Buffer::MAP_ALIGNMENT (#{MAP_ALIGNMENT})!"
        end
        file_size = file.size
        raise ArgumentError, "Invalid negative or zero file size!" if file_size <= 0
        if size
          raise ArgumentError, "Size can't be negative!" if size < 0
          raise ArgumentError, "Size can't be zero!" if size == 0
          raise ArgumentError, "Size (#{size}) can't be larger than file size (#{file_size})" if size > file_size
          if offset + size > file_size
            max = (file_size - size) / MAP_ALIGNMENT * MAP_ALIGNMENT
            raise ArgumentError, "Offset (#{offset}) can't be larger than #{max} for requested size (#{size})"
          end
        else
          raise ArgumentError, "Offset (#{offset}) can't be larger than file size (#{file_size})" if offset >= file_size
          size = file_size - offset
        end
        readonly = flags & READONLY != 0
        priv = flags & PRIVATE != 0
        if !(readonly || priv) && file.respond_to?(:__oruby_writable?, true) && !file.__oruby_writable?
          raise Errno::EACCES, "mmap"
        end
        data = file.pread(size, offset).b
        path = file.respond_to?(:path) ? file.path : nil
        b = allocate
        b.__oruby_st = Storage.__oruby_state(data, :mapped, readonly: readonly, shared: !priv, private: priv,
                                             external: !priv, file: priv ? nil : path, file_offset: offset)
        b
      end

      def __oruby_resolve
        [self, 0, size]
      end

      def __oruby_root
        self
      end

      def __oruby_size
        @st[:size]
      end

      def size
        @st[:size]
      end

      def null?
        @st[:data].nil?
      end

      def readonly?
        frozen? || @st[:readonly]
      end

      def internal?
        @st[:kind] == :internal
      end

      def mapped?
        @st[:kind] == :mapped
      end

      def external?
        @st[:external]
      end

      def shared?
        @st[:shared]
      end

      def private?
        @st[:private]
      end

      def source
        @st[:source]
      end

      def __oruby_flag_names
        f = []
        f << "EXTERNAL" if external?
        f << "INTERNAL" if internal?
        f << "MAPPED" if mapped?
        f << "SHARED" if shared?
        f << "LOCKED" if locked?
        f << "PRIVATE" if private?
        f << "READONLY" if readonly?
        f << "NULL" if null?
        f
      end

      def __oruby_bytes(pos, len)
        data = @st[:data]
        return "".b if data.nil? || len == 0
        data.byteslice(@st[:base] + pos, len).b
      end

      def __oruby_put(pos, bytes)
        data = @st[:data]
        return if data.nil? || bytes.empty?
        at = @st[:base] + pos
        if data.encoding == Encoding::BINARY && !data.frozen?
          data.bytesplice(at, bytes.bytesize, bytes)
        else
          bytes.each_byte.with_index { |b, i| data.setbyte(at + i, b) }
        end
        if (path = @st[:file])
          File.open(path, "r+b") { |f| f.pwrite(bytes, @st[:file_offset] + at) }
        end
        nil
      end

      # in place: a buffer frozen inside IO::Buffer.for is still released
      def __oruby_free!
        locks = @st[:locks]
        @st.replace(Storage.__oruby_state(nil, nil))
        @st[:locks] = locks
        self
      end

      def free
        __oruby_frozen_error if frozen?
        raise LockedError, "Buffer is locked!" if locked?
        __oruby_free!
      end

      def transfer
        __oruby_frozen_error if frozen?
        raise LockedError, "Cannot transfer ownership of locked buffer!" if locked?
        t = self.class.allocate
        st = @st
        @st = Storage.__oruby_state(nil, nil)
        st[:locks] = [0]
        t.__oruby_st = st
        t
      end

      def resize(new_size)
        __oruby_frozen_error if frozen?
        raise LockedError, "Cannot resize locked buffer!" if locked?
        new_size = Buffer.__oruby_int(new_size)
        raise ArgumentError, "Size can't be negative!" if new_size < 0
        if !null? && (@st[:external] || @st[:advance])
          raise AccessError, "Cannot resize external buffer!"
        end
        readonly = @st[:readonly]
        if new_size == 0
          __oruby_free!
          @st[:readonly] = readonly
          return self
        end
        old = null? ? "".b : get_string
        data = old.bytesize >= new_size ? old.byteslice(0, new_size) : old + "\0".b * (new_size - old.bytesize)
        kind = if null?
                 new_size >= PAGE_SIZE ? :mapped : :internal
               elsif @st[:kind] == :mapped && !@st[:private]
                 :internal # mremap is Linux-only
               else
                 @st[:kind]
               end
        @st[:data] = data
        @st[:base] = 0
        @st[:size] = new_size
        @st[:kind] = kind
        @st[:readonly] = readonly
        self
      end

      def advance(amount)
        __oruby_frozen_error if frozen?
        raise AccessError, "Cannot advance an owning buffer!" unless @st[:advance]
        amount = Buffer.__oruby_int(amount)
        raise ArgumentError, "Amount can't be negative!" if amount < 0
        raise ArgumentError, "Advance amount exceeds buffer size!" if amount > size
        @st[:base] += amount
        @st[:size] -= amount
        self
      end
    end

    # --- Slice ----------------------------------------------------------------

    class Slice < Buffer
      class << self
        undef_method :for, :map, :string
      end

      def initialize(parent, offset = 0, length = nil)
        raise TypeError, "wrong argument type #{parent.class} (expected IO::Buffer)" unless parent.is_a?(Buffer)
        _, _, psize = parent.__oruby_view
        offset, length = __oruby_bounds(offset, length, psize)
        @sl = { parent: parent, offset: offset, length: length }
      end

      def initialize_copy(orig)
        super
        @sl = orig.__oruby_sl.dup
      end

      def __oruby_sl
        @sl
      end

      def source
        @sl[:parent]
      end

      def size
        @sl[:length]
      end

      def __oruby_size
        @sl[:length]
      end

      def __oruby_chain
        chain = []
        v = self
        while v.is_a?(Slice)
          chain << v
          v = v.__oruby_sl[:parent]
        end
        [v, chain]
      end

      def __oruby_root
        __oruby_chain[0]
      end

      def __oruby_resolve
        root, chain = __oruby_chain
        base = 0
        avail = root.size
        i = chain.size - 1
        while i >= 0
          sl = chain[i].__oruby_sl
          return nil if sl[:offset] + sl[:length] > avail
          base += sl[:offset]
          avail = sl[:length]
          i -= 1
        end
        [root, base, avail]
      end

      def null?
        __oruby_root.null?
      end

      def readonly?
        return true if frozen?
        v = @sl[:parent]
        while v.is_a?(Slice)
          return true if v.frozen?
          v = v.__oruby_sl[:parent]
        end
        v.readonly?
      end

      def __oruby_flag_names
        f = ["SLICE"]
        f << "LOCKED" if locked?
        f << "READONLY" if readonly?
        f << "NULL" if null?
        f
      end

      def resize(new_size)
        __oruby_frozen_error if frozen?
        new_size = Buffer.__oruby_int(new_size)
        raise ArgumentError, "Size can't be negative!" if new_size < 0
        parent = @sl[:parent]
        pv = parent.__oruby_resolve
        raise InvalidatedError, "Buffer has been invalidated!" unless pv
        psize = pv[2]
        raise InvalidatedError, "Buffer has been invalidated!" if @sl[:offset] > psize
        raise ArgumentError, "Resized slice exceeds its source buffer!" if @sl[:offset] + new_size > psize
        @sl[:length] = new_size
        self
      end

      def advance(amount)
        __oruby_frozen_error if frozen?
        __oruby_view
        amount = Buffer.__oruby_int(amount)
        raise ArgumentError, "Amount can't be negative!" if amount < 0
        raise ArgumentError, "Advance amount exceeds buffer size!" if amount > size
        @sl[:offset] += amount
        @sl[:length] -= amount
        self
      end
    end
  end
end
