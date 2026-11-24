package fiddle_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/fiddle"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/regexp/full"
)

func TestFiddleCoreAndClosures(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "fiddle"

		libc = Fiddle.dlopen(FFI::Platform::LIBC)

		# Basic Fiddle::Function calls
		c_abs = Fiddle::Function.new(libc["abs"], [Fiddle::TYPE_INT], Fiddle::TYPE_INT)
		raise "abs" unless c_abs.call(-99) == 99

		c_strlen = Fiddle::Function.new(libc["strlen"], [Fiddle::TYPE_VOIDP], Fiddle::TYPE_SIZE_T)
		raise "strlen" unless c_strlen.call("hello fiddle") == 12

		# sin from the process: libm on glibc, not libc
		c_sin = Fiddle::Function.new(Fiddle::Handle::DEFAULT["sin"], [Fiddle::TYPE_DOUBLE], Fiddle::TYPE_DOUBLE)
		raise "sin" unless (c_sin.call(Math::PI / 2) - 1.0).abs < 1e-12

		# Variadic Fiddle::Function (snprintf)
		c_snprintf = Fiddle::Function.new(
			libc["snprintf"],
			[Fiddle::TYPE_VOIDP, Fiddle::TYPE_SIZE_T, Fiddle::TYPE_CONST_STRING, Fiddle::TYPE_VARIADIC],
			Fiddle::TYPE_INT
		)
		res = Fiddle::Pointer.malloc(64, Fiddle::RUBY_FREE) do |buf|
			n = c_snprintf.call(buf, 64, "%s:%d", Fiddle::TYPE_CONST_STRING, "k", Fiddle::TYPE_INT, 77)
			raise "snprintf n=#{n}" unless n == 4
			buf.to_s
		end
		raise "snprintf result: #{res.inspect}" unless res == "k:77"

		# Pointer operations & dlwrap/dlunwrap
		p = Fiddle::Pointer.malloc(16, Fiddle::RUBY_FREE)
		p[0, 5] = "hello"
		raise "to_str" unless p.to_str(5) == "hello"
		raise "index" unless p[0] == "h".ord
		raise "ptr arith" unless (p + 2).to_str(3) == "llo"
		rp = p.ref
		raise "ref/ptr" unless rp.ptr == p
		rp.call_free
		p.call_free
		raise "freed?" unless p.freed?

		obj = { hello: "fiddle" }
		wrapped = Fiddle.dlwrap(obj)
		raise "dlunwrap" unless Fiddle.dlunwrap(wrapped).equal?(obj)

		# Fiddle::Closure::BlockCaller with C qsort!
		c_qsort = Fiddle::Function.new(
			libc["qsort"],
			[Fiddle::TYPE_VOIDP, Fiddle::TYPE_SIZE_T, Fiddle::TYPE_SIZE_T, Fiddle::TYPE_VOIDP],
			Fiddle::TYPE_VOID
		)

		nums = [30, -5, 12, 0, 99, -100]
		packed = nums.pack("l<*")
		buf = Fiddle::Pointer.malloc(packed.bytesize, Fiddle::RUBY_FREE)
		buf[0, packed.bytesize] = packed

		cmp = Fiddle::Closure::BlockCaller.new(Fiddle::TYPE_INT, [Fiddle::TYPE_VOIDP, Fiddle::TYPE_VOIDP]) do |a, b|
			va = a.to_str(4).unpack1("l<")
			vb = b.to_str(4).unpack1("l<")
			va <=> vb
		end
		c_qsort.call(buf, nums.length, 4, cmp)
		sorted = buf.to_str(packed.bytesize).unpack("l<*")
		cmp.free
		buf.call_free
		raise "qsort via BlockCaller failed: #{sorted.inspect}" unless sorted == nums.sort

		# Fiddle::Closure subclass
		class RevCmp < Fiddle::Closure
			def call(a, b)
				b.to_str(4).unpack1("l<") <=> a.to_str(4).unpack1("l<")
			end
		end
		buf2 = Fiddle::Pointer.malloc(packed.bytesize, Fiddle::RUBY_FREE)
		buf2[0, packed.bytesize] = packed
		rcmp = RevCmp.new(Fiddle::TYPE_INT, [Fiddle::TYPE_VOIDP, Fiddle::TYPE_VOIDP])
		c_qsort.call(buf2, nums.length, 4, rcmp)
		rsorted = buf2.to_str(packed.bytesize).unpack("l<*")
		rcmp.free
		buf2.call_free
		raise "qsort via Closure subclass failed: #{rsorted.inspect}" unless rsorted == nums.sort.reverse
	`)
	if err != nil {
		t.Fatalf("Fiddle core/closure test failed: %v", err)
	}
}

func TestFiddleImporterAndStruct(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "fiddle/import"
		require "fiddle/struct"
		require "fiddle/types"

		module LibM
			extend Fiddle::Importer
			dlload FFI::Platform::LIBC
			include Fiddle::BasicTypes

			typealias "my_int", "int"

			Timeval = struct [
				"long tv_sec",
				"int tv_usec"
			]

			IntFloat = union [
				"unsigned int i",
				"float f"
			]

			extern "my_int abs(my_int)"
			extern "size_t strlen(const char *)"
			extern "int gettimeofday(void *, void *)"
		end

		raise "Importer abs" unless LibM.abs(-123) == 123
		raise "Importer strlen" unless LibM.strlen("importer!") == 9
		raise "sizeof" unless LibM.sizeof("int") == 4

		LibM::Timeval.malloc do |tv|
			rc = LibM.gettimeofday(tv.to_ptr, nil)
			raise "gettimeofday rc=#{rc}" unless rc == 0
			raise "tv_sec invalid: #{tv.tv_sec}" unless tv.tv_sec > 1_600_000_000
		end

		LibM::IntFloat.malloc do |u|
			u.f = 1.0
			raise "union IEEE754 1.0: 0x#{u.i.to_s(16)}" unless u.i == 0x3f800000
		end

		# External libz via Fiddle::Importer
		module LibZ
			extend Fiddle::Importer
			dlload FFI.map_library_name("z")
			extern "unsigned long crc32(unsigned long, const unsigned char *, unsigned int)"
		end

		crc = LibZ.crc32(0, "123456789", 9)
		raise "zlib crc32 mismatch: 0x#{crc.to_s(16)}" unless crc == 0xcbf43926
	`)
	if err != nil {
		t.Fatalf("Fiddle Importer/Struct test failed: %v", err)
	}
}
