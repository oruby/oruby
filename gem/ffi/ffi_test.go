package ffi_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/ffi"
	_ "github.com/oruby/oruby/gem/load"
)

func TestFFIBasicLibcAndErrno(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "ffi"

		module LibC
			extend FFI::Library
			ffi_lib FFI::Library::LIBC

			attach_function :getpid, [], :pid_t
			attach_function :abs, [:int], :int
			attach_function :strlen, [:string], :size_t
			attach_function :strcmp, [:string, :string], :int
			attach_function :sin, [:double], :double
			attach_function :cos, [:double], :double
			attach_function :sqrt, [:double], :double
			attach_function :strdup, [:string], :strptr
			attach_function :c_free, :free, [:pointer], :void
			attach_function :c_open, :open, [:string, :int], :int
		end

		raise "getpid" unless LibC.getpid > 0
		raise "abs" unless LibC.abs(-42) == 42
		raise "strlen" unless LibC.strlen("hello oruby") == 11
		raise "strcmp eq" unless LibC.strcmp("abc", "abc") == 0
		raise "strcmp lt" unless LibC.strcmp("abc", "abd") < 0
		raise "sin" unless (LibC.sin(Math::PI / 2) - 1.0).abs < 1e-12
		raise "cos" unless (LibC.cos(0.0) - 1.0).abs < 1e-12
		raise "sqrt" unless (LibC.sqrt(144.0) - 12.0).abs < 1e-12

		# :strptr returns [String, Pointer]
		str, ptr = LibC.strdup("copied string")
		raise "strdup str: #{str.inspect}" unless str == "copied string"
		raise "strdup ptr null" if ptr.null?
		LibC.c_free(ptr)

		# errno / LastError capture (ENOENT == 2 on POSIX/macOS/Linux)
		fd = LibC.c_open("/definitely/does/not/exist/oruby_ffi_test", 0)
		raise "expected -1 fd" unless fd == -1
		raise "expected ENOENT (2), got #{FFI.errno}" unless FFI.errno == 2
		raise "LastError mismatch" unless FFI::LastError.error == 2
		FFI::LastError.error = 0
		raise "LastError reset" unless FFI.errno == 0
		raise "Platform::NAME" unless FFI::Platform::NAME.include?("-")

		# NotFoundError for missing symbol
		raised = false
		begin
			LibC.attach_function :oruby_nonexistent_symbol_xyz, [], :void
		rescue FFI::NotFoundError
			raised = true
		end
		raise "expected FFI::NotFoundError" unless raised
	`)
	if err != nil {
		t.Fatalf("FFI basic libc test failed: %v", err)
	}
}

func TestFFIMemoryPointerAndVariadicAndCallback(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "ffi"

		module LibC2
			extend FFI::Library
			ffi_lib FFI::Library::LIBC

			attach_function :snprintf, [:pointer, :size_t, :string, :varargs], :int
			callback :qsort_cmp, [:pointer, :pointer], :int
			attach_function :qsort, [:pointer, :size_t, :size_t, :qsort_cmp], :void
		end

		# MemoryPointer block auto-free + variadic snprintf
		formatted = FFI::MemoryPointer.new(:char, 128) do |buf|
			n = LibC2.snprintf(buf, 128, "%s=%d (%.2f)", :string, "answer", :int, 42, :double, 3.14)
			raise "snprintf len: #{n}" unless n == 16
			buf.read_string
		end
		raise "snprintf result: #{formatted.inspect}" unless formatted == "answer=42 (3.14)"

		# Callback via libffi closure: qsort a C int32 array using a Ruby lambda!
		nums = [42, -7, 100, 3, 0, -99, 15]
		FFI::MemoryPointer.new(:int32, nums.length) do |arr|
			arr.write_array_of_int32(nums)
			cmp = ->(a, b) { a.read_int32 <=> b.read_int32 }
			LibC2.qsort(arr, nums.length, 4, cmp)
			sorted = arr.read_array_of_int32(nums.length)
			raise "qsort failed: #{sorted.inspect}" unless sorted == nums.sort
		end

		# Explicit FFI::Function closure object
		FFI::MemoryPointer.new(:int32, nums.length) do |arr|
			arr.write_array_of_int32(nums)
			rev_fn = FFI::Function.new(:int, [:pointer, :pointer]) { |a, b| b.read_int32 <=> a.read_int32 }
			LibC2.qsort(arr, nums.length, 4, rev_fn)
			sorted = arr.read_array_of_int32(nums.length)
			rev_fn.free
			raise "reverse qsort failed: #{sorted.inspect}" unless sorted == nums.sort.reverse
		end

		# NullPointerError on NULL access
		raised = false
		begin
			FFI::Pointer::NULL.read_int32
		rescue FFI::NullPointerError
			raised = true
		end
		raise "expected NullPointerError" unless raised
	`)
	if err != nil {
		t.Fatalf("FFI memory/variadic/callback test failed: %v", err)
	}
}

func TestFFIStructByRefByValueEnumAndZlib(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "ffi"

		# Struct by reference: gettimeofday(struct timeval *, void *)
		class Timeval < FFI::Struct
			layout :tv_sec, :long,
			       :tv_usec, :int
		end

		# Struct by value: div_t div(int numer, int denom)
		class DivT < FFI::Struct
			layout :quot, :int,
			       :rem, :int
		end

		# Nested struct + inline array + Union
		class Packet < FFI::Struct
			layout :tag, :uint8,
			       :code, :int32,
			       :label, [:char, 16],
			       :pair, DivT
		end

		class IntFloatUnion < FFI::Union
			layout :i, :uint32,
			       :f, :float
		end

		module LibSys
			extend FFI::Library
			ffi_lib FFI::Library::LIBC

			enum :sign_mode, [:zero, 0, :positive, 1, :negative, -1]

			attach_function :gettimeofday, [Timeval.by_ref, :pointer], :int
			attach_function :c_div, :div, [:int, :int], DivT.by_value
			attach_function :abs_enum, :abs, [:sign_mode], :sign_mode
		end

		# gettimeofday via Struct by_ref
		tv = Timeval.new
		rc = LibSys.gettimeofday(tv, nil)
		raise "gettimeofday rc=#{rc}" unless rc == 0
		raise "tv_sec invalid: #{tv[:tv_sec]}" unless tv[:tv_sec] > 1_600_000_000

		# div() returning struct by value!
		res = LibSys.c_div(17, 5)
		raise "div_t mismatch: quot=#{res[:quot]} rem=#{res[:rem]}" unless res[:quot] == 3 && res[:rem] == 2

		# Enum arg & return conversion
		raise "enum conversion" unless LibSys.abs_enum(:negative) == :positive

		# Alignment, inline char array, nested struct
		raise "Packet alignment" unless Packet.offset_of(:code) == 4
		pkt = Packet.new
		pkt[:tag] = 7
		pkt[:code] = 12345
		pkt[:label] = "oruby"
		pkt[:pair][:quot] = 9
		pkt[:pair][:rem] = 2
		raise "pkt fields" unless pkt[:tag] == 7 && pkt[:code] == 12345 && pkt[:label].to_s == "oruby"
		raise "pkt nested" unless pkt[:pair][:quot] == 9 && pkt[:pair][:rem] == 2

		# Union sharing memory
		u = IntFloatUnion.new
		u[:f] = 1.0
		raise "union IEEE754 1.0: 0x#{u[:i].to_s(16)}" unless u[:i] == 0x3f800000

		# External shared library: libz (zlib)
		module LibZ
			extend FFI::Library
			ffi_lib "z"

			attach_function :zlibVersion, [], :string
			attach_function :crc32, [:ulong, :pointer, :uint], :ulong
		end

		zver = LibZ.zlibVersion
		raise "zlibVersion empty: #{zver.inspect}" unless zver && zver.length > 0
		crc = LibZ.crc32(0, "123456789", 9)
		raise "crc32 mismatch: 0x#{crc.to_s(16)}" unless crc == 0xcbf43926
	`)
	if err != nil {
		t.Fatalf("FFI struct/enum/zlib test failed: %v", err)
	}
}
