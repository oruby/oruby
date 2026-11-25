package msgpack_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/goext/msgpack"
	_ "github.com/oruby/oruby/gem/objspace"
	_ "github.com/oruby/oruby/gem/time"
	_ "github.com/oruby/oruby/gem/transcode"
)

func TestMessagePackVectors(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "msgpack"
		require "msgpack/bigint"

		vectors = [
			[nil, "c0"],
			[true, "c3"],
			[false, "c2"],
			[42, "2a"],
			[-17, "ef"],
			[-100, "d09c"],
			[-20000, "d1b1e0"],
			[-1000000, "d2fff0bdc0"],
			[-(1 << 60), "d3f000000000000000"],
			[200, "ccc8"],
			[50000, "cdc350"],
			[3_000_000_000, "ceb2d05e00"],
			[(1 << 63) + 12345, "cf8000000000003039"],
			[3.141592653589793, "cb400921fb54442d18"],
			["hello \u2603", "a968656c6c6f20e29883"],
			["\x00\x01\xff".b, "c4030001ff"],
			[[1, "two", [3.5, true, nil]], "9301a374776f93cb400c000000000000c3c0"],
			[{"a" => 1, "b" => [2, 3]}, "82a16101a162920203"],
		]

		vectors.each do |val, expected_hex|
			packed = MessagePack.pack(val)
			raise "not binary" unless packed.encoding == Encoding::BINARY
			hex = packed.unpack1("H*")
			raise "pack mismatch for #{val.inspect}: got #{hex}, want #{expected_hex}" unless hex == expected_hex

			unpacked = MessagePack.unpack(packed)
			raise "unpack mismatch for #{val.inspect}: got #{unpacked.inspect}" unless unpacked == val

			# CoreExt #to_msgpack
			raise "to_msgpack mismatch for #{val.inspect}" unless val.to_msgpack == packed
		end

		# Verify string vs binary encoding preserved on unpack
		u_str = MessagePack.unpack(MessagePack.pack("utf8"))
		u_bin = MessagePack.unpack(MessagePack.pack("bin".b))
		raise "expected UTF-8, got #{u_str.encoding}" unless u_str.encoding == Encoding::UTF_8
		raise "expected BINARY, got #{u_bin.encoding}" unless u_bin.encoding == Encoding::BINARY

		# Large strings, bins, arrays, maps (str8/16, bin8/16, array16, map16)
		[40, 300].each do |n|
			s = "a" * n
			raise "str #{n}" unless MessagePack.unpack(MessagePack.pack(s)) == s
			b = ("\x01" * n).b
			rb_b = MessagePack.unpack(MessagePack.pack(b))
			raise "bin #{n}" unless rb_b == b && rb_b.encoding == Encoding::BINARY
			a = Array.new(n) { |i| i }
			raise "ary #{n}" unless MessagePack.unpack(MessagePack.pack(a)) == a
			h = {}
			n.times { |i| h["k#{i}"] = i }
			raise "map #{n}" unless MessagePack.unpack(MessagePack.pack(h)) == h
		end
	`)
	if err != nil {
		t.Fatalf("MessagePack vectors test failed: %v", err)
	}
}

func TestMessagePackStreamingAndOptions(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "msgpack"

		# Streaming unpacker byte-by-byte
		stream = MessagePack.pack({"name" => "alice", "scores" => [10, 20]}) +
			MessagePack.pack([1, 2, 3]) +
			MessagePack.pack("done")

		u = MessagePack::Unpacker.new(symbolize_keys: true)
		results = []
		stream.bytes.each do |b|
			u.feed_each(b.chr) { |obj| results << obj }
		end
		raise "stream count: #{results.size}" unless results.size == 3
		raise "symbolize_keys failed: #{results[0].inspect}" unless results[0] == { name: "alice", scores: [10, 20] }
		raise "stream[1] failed" unless results[1] == [1, 2, 3]
		raise "stream[2] failed" unless results[2] == "done"

		# freeze: true
		frozen_val = MessagePack.unpack(MessagePack.pack({"a" => ["b", 1]}), freeze: true)
		raise "hash not frozen" unless frozen_val.frozen?
		raise "key not frozen" unless frozen_val.keys.first.frozen?
		raise "ary not frozen" unless frozen_val["a"].frozen?
		raise "elem not frozen" unless frozen_val["a"][0].frozen?

		# Manual headers & skip & skip_nil
		p = MessagePack::Packer.new
		p.write_array_header(3)
		p.write_nil
		p.write({"skip" => [1, 2, 3]})
		p.write(99)
		p.write_float32(1.5)
		raw = p.full_pack

		u2 = MessagePack::Unpacker.new
		u2.feed(raw)
		raise "array header" unless u2.read_array_header == 3
		raise "skip_nil true" unless u2.skip_nil == true
		raise "skip_nil false" unless u2.skip_nil == false
		u2.skip
		raise "after skip" unless u2.read == 99
		raise "float32" unless (u2.read - 1.5).abs < 1e-6

		# Extra bytes error in full_unpack
		raised = false
		begin
			MessagePack.unpack(MessagePack.pack(1) + MessagePack.pack(2))
		rescue MessagePack::MalformedFormatError
			raised = true
		end
		raise "expected MalformedFormatError for trailing bytes" unless raised

		# Reserved 0xc1 byte
		raised = false
		begin
			MessagePack.unpack("\xc1".b)
		rescue MessagePack::MalformedFormatError
			raised = true
		end
		raise "expected MalformedFormatError for 0xc1" unless raised

		# UnexpectedTypeError (also is_a? MessagePack::TypeError)
		raised = false
		begin
			u3 = MessagePack::Unpacker.new
			u3.feed(MessagePack.pack(123))
			u3.read_map_header
		rescue MessagePack::TypeError => e
			raised = e.is_a?(MessagePack::UnexpectedTypeError)
		end
		raise "expected UnexpectedTypeError" unless raised

		# EOFError on truncated input
		raised = false
		begin
			MessagePack.unpack("\xda\x00\x05abc".b)
		rescue EOFError
			raised = true
		end
		raise "expected EOFError on truncated input" unless raised

		# UnknownExtTypeError vs allow_unknown_ext: true
		ext_bytes = "\xc7\x02\x05hi".b
		raised = false
		begin
			MessagePack.unpack(ext_bytes)
		rescue MessagePack::UnknownExtTypeError
			raised = true
		end
		raise "expected UnknownExtTypeError" unless raised

		ev = MessagePack.unpack(ext_bytes, allow_unknown_ext: true)
		raise "ExtensionValue mismatch: #{ev.inspect}" unless ev.is_a?(MessagePack::ExtensionValue) && ev.type == 5 && ev.payload == "hi"
		raise "ExtensionValue repack" unless MessagePack.pack(ev) == "\xd5\x05hi".b
	`)
	if err != nil {
		t.Fatalf("MessagePack streaming/options test failed: %v", err)
	}
}

func TestMessagePackFactoryAndExtensions(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "msgpack"
		require "msgpack/bigint"

		f = MessagePack::Factory.new
		f.register_type(
			MessagePack::Timestamp::TYPE,
			Time,
			packer: MessagePack::Time::Packer,
			unpacker: MessagePack::Time::Unpacker
		)
		f.register_type(0x00, Symbol)
		f.register_type(
			0x01,
			Integer,
			packer: MessagePack::Bigint.method(:to_msgpack_ext),
			unpacker: MessagePack::Bigint.method(:from_msgpack_ext),
			oversized_integer_extension: true
		)

		raise "type_registered? Symbol" unless f.type_registered?(Symbol)
		raise "type_registered? -1" unless f.type_registered?(-1)

		# Exact MRI hex vectors for Time32, Time64, Time96, Symbol, Bigint
		t32 = Time.at(1700000000, 0)
		p32 = f.pack(t32)
		raise "time32 hex: #{p32.unpack1('H*')}" unless p32.unpack1("H*") == "d6ff6553f100"
		raise "time32 roundtrip" unless f.unpack(p32) == t32

		t64 = Time.at(1700000000, 123456789, :nanosecond)
		p64 = f.pack(t64)
		raise "time64 hex: #{p64.unpack1('H*')}" unless p64.unpack1("H*") == "d7ff1d6f34546553f100"
		u64 = f.unpack(p64)
		raise "time64 roundtrip" unless u64.tv_sec == t64.tv_sec && u64.tv_nsec == t64.tv_nsec

		t96 = Time.at(-100, 500, :nanosecond)
		p96 = f.pack(t96)
		raise "time96 hex: #{p96.unpack1('H*')}" unless p96.unpack1("H*") == "c70cff000001f4ffffffffffffff9c"
		u96 = f.unpack(p96)
		raise "time96 roundtrip" unless u96.tv_sec == t96.tv_sec && u96.tv_nsec == t96.tv_nsec

		psym = f.pack(:hello)
		raise "sym_ext hex: #{psym.unpack1('H*')}" unless psym.unpack1("H*") == "c7050068656c6c6f"
		raise "sym_ext roundtrip" unless f.unpack(psym) == :hello

		big_pos = 1 << 100
		pbig = f.pack(big_pos)
		raise "bigint_ext hex: #{pbig.unpack1('H*')}" unless pbig.unpack1("H*") == "c711010000000000000000000000000000000010"
		raise "bigint_ext roundtrip" unless f.unpack(pbig) == big_pos

		big_neg = -(1 << 100)
		pnbig = f.pack(big_neg)
		raise "neg_bigint_ext hex: #{pnbig.unpack1('H*')}" unless pnbig.unpack1("H*") == "c711010100000000000000000000000000000010"
		raise "neg_bigint_ext roundtrip" unless f.unpack(pnbig) == big_neg

		# Custom class with recursive: true
		Point = Struct.new(:x, :y)
		f.register_type(
			0x02,
			Point,
			recursive: true,
			packer: ->(pt, packer) { packer.write(pt.x); packer.write(pt.y) },
			unpacker: ->(unpacker) { Point.new(unpacker.read, unpacker.read) }
		)
		pt = Point.new(10, 20)
		raise "recursive ext roundtrip" unless f.unpack(f.pack(pt)) == pt

		# Pool
		pool = f.pool(2)
		raise "pool roundtrip" unless pool.load(pool.dump([:ok, pt, big_pos])) == [:ok, pt, big_pos]

		# Buffer operations
		buf = MessagePack::Buffer.new
		buf << "hello"
		buf.write(" world")
		raise "buf size" unless buf.size == 11
		raise "buf read(5)" unless buf.read(5) == "hello"
		raise "buf skip(1)" unless buf.skip(1) == 1
		raise "buf read_all(5)" unless buf.read_all(5) == "world"
		raise "buf empty" unless buf.empty?
	`)
	if err != nil {
		t.Fatalf("MessagePack factory/extensions test failed: %v", err)
	}
}

func TestMessagePackTruncatedHugeHeadersDoNotCrash(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "msgpack"

		# Truncated array32 / map32 / str32 / bin32 claiming 4 billion elements must raise EOFError, not OOM/longjmp
		[
			"\xdd\xff\xff\xff\xff".b,
			"\xdf\xff\xff\xff\xff".b,
			"\xdb\xff\xff\xff\xff".b,
			"\xc6\xff\xff\xff\xff".b,
		].each do |raw|
			raised = false
			begin
				MessagePack.unpack(raw)
			rescue EOFError, MessagePack::UnpackError
				raised = true
			end
			raise "expected EOFError for #{raw.unpack1('H*')}" unless raised
		end

		# ExtensionValue out-of-range type raises RangeError
		[-129, -65540, -(2**40), 128, 65540, 2**40].each do |bad_type|
			got_range = false
			begin
				MessagePack::ExtensionValue.new(bad_type, "a").to_msgpack
			rescue RangeError
				got_range = true
			end
			raise "expected RangeError for ext type #{bad_type}" unless got_range
		end

		# String/Symbol encoding transcoding
		raise "us-ascii" unless MessagePack.unpack(MessagePack.pack("hi".force_encoding(Encoding::US_ASCII))).encoding == Encoding::UTF_8
		u16 = MessagePack.unpack(MessagePack.pack("string".encode(Encoding::UTF_16)))
		raise "utf-16" unless u16 == "string" && u16.encoding == Encoding::UTF_8
		euc = MessagePack.unpack(MessagePack.pack([0xa4, 0xa2].pack("C*").force_encoding(Encoding::EUC_JP)))
		raise "euc-jp" unless euc == "\xE3\x81\x82".force_encoding("UTF-8") && euc.encoding == Encoding::UTF_8
		sjis_sym = MessagePack.unpack(MessagePack.pack("\xE3\x81\x82".encode("Shift_JIS").to_sym))
		raise "sjis sym" unless sjis_sym == "\xE3\x81\x82".force_encoding("UTF-8") && sjis_sym.encoding == Encoding::UTF_8
		bin_sym = MessagePack.unpack(MessagePack.pack("\xE3\x81\x82".b.to_sym))
		raise "bin sym" unless bin_sym == "\xE3\x81\x82".b && bin_sym.encoding == Encoding::ASCII_8BIT

		# Core type subclass extension registration
		class CustomHash < Hash; end
		class CustomArray < Array; end
		class CustomString < String; end
		f2 = MessagePack::Factory.new
		f2.register_type(0x10, CustomHash, packer: ->(h) { h.size.to_s }, unpacker: ->(s) { [:h, s.to_i] })
		f2.register_type(0x11, CustomArray, packer: ->(a) { a.size.to_s }, unpacker: ->(s) { [:a, s.to_i] })
		f2.register_type(0x12, CustomString, packer: ->(s) { s.upcase }, unpacker: ->(s) { [:s, s] })
		ch = CustomHash.new; ch["x"] = 1
		ca = CustomArray.new([1, 2, 3])
		cs = CustomString.new("ab")
		raise "custom hash" unless f2.unpack(f2.pack(ch)) == [:h, 1]
		raise "custom array" unless f2.unpack(f2.pack(ca)) == [:a, 3]
		raise "custom string" unless f2.unpack(f2.pack(cs)) == [:s, "AB"]

		# Keyword-hash as sole arg to pack/dump
		raise "pack kw hash" unless MessagePack.unpack(MessagePack.pack(foo: "bar")) == {"foo" => "bar"}
		raise "factory dump kw hash" unless f2.load(f2.dump(x: 1, y: 2)) == {"x" => 1, "y" => 2}

		# freeze: true deduplicates strings
		fstr = MessagePack.unpack(MessagePack.pack("dedup_me"), freeze: true)
		raise "freeze dedup" unless fstr.equal?("dedup_me".freeze)

		# Packer#register_type accepts Proc and rejects non-Proc with TypeError
		p2 = MessagePack::Packer.new
		p2.register_type(0x01, CustomString, ->(_) { "ok" })
		p2.write(cs)
		raise "packer proc ext" unless p2.full_pack == "\xd5\x01ok".b
		bad_cb = false
		begin
			p2.register_type(0x02, CustomHash, Object.new)
		rescue TypeError
			bad_cb = true
		end
		raise "expected TypeError for non-Proc callback" unless bad_cb

		# Type/argument checks on write_array, write_symbol, write_float32
		[
			-> { MessagePack::Packer.new.write_array("bad") },
			-> { MessagePack::Packer.new.write_symbol("bad") },
		].each do |fn|
			ok = false
			begin; fn.call; rescue TypeError; ok = true; end
			raise "expected TypeError" unless ok
		end
		ok_f32 = false
		begin; MessagePack::Packer.new.write_float32("abc"); rescue ArgumentError; ok_f32 = true; end
		raise "expected ArgumentError for write_float32" unless ok_f32

		# Unpacker#skip raises StackError at depth 512
		deep_p = MessagePack::Packer.new
		512.times { deep_p.write_array_header(1) }
		deep_p.write(nil)
		deep_u = MessagePack::Unpacker.new
		deep_u.feed(deep_p.to_s)
		got_stack = false
		begin; deep_u.skip; rescue MessagePack::StackError; got_stack = true; end
		raise "expected StackError on deep skip" unless got_stack

		# oversized_integer_extension and optimized_symbols_parsing
		f3 = MessagePack::Factory.new
		f3.register_type(0x01, Integer, packer: :to_s, unpacker: ->(s) { Integer(s) })
		got_range = false
		begin; f3.dump(10**150); rescue RangeError; got_range = true; end
		raise "expected RangeError without oversized_integer_extension" unless got_range
		f4 = MessagePack::Factory.new
		f4.register_type(0x01, Integer, packer: :to_s, unpacker: ->(s) { Integer(s) }, oversized_integer_extension: true)
		raise "oversized int roundtrip" unless f4.load(f4.dump(10**150)) == 10**150
		f4.register_type(0x02, Symbol, packer: :to_msgpack_ext, unpacker: ->(_) { raise "not optimized" }, optimized_symbols_parsing: true)
		raise "optimized sym ascii" unless f4.load(f4.dump(:hello)) == :hello && f4.load(f4.dump(:hello)).encoding == Encoding::US_ASCII
		raise "optimized sym utf8" unless f4.load(f4.dump(:"fée")) == :"fée" && f4.load(f4.dump(:"fée")).encoding == Encoding::UTF_8

		# ObjectSpace.memsize_of reflects Buffer and Factory Go allocations
		require "objspace"
		mb = MessagePack::Buffer.new
		empty_sz = ObjectSpace.memsize_of(mb)
		10.times { mb << "a" * 500 }
		full_sz = ObjectSpace.memsize_of(mb)
		raise "buffer memsize grew" unless full_sz > empty_sz
		mb.read(10)
		raise "buffer memsize stable" unless ObjectSpace.memsize_of(mb) == full_sz
		mb.read_all
		raise "buffer memsize cleared" unless ObjectSpace.memsize_of(mb) == empty_sz

		mf = MessagePack::Factory.new
		fbase = ObjectSpace.memsize_of(mf)
		mf.register_type(0x0a, Symbol)
		raise "factory memsize grew" unless ObjectSpace.memsize_of(mf) > fbase
	`)
	if err != nil {
		t.Fatalf("MessagePack huge-header safety test failed: %v", err)
	}
}
