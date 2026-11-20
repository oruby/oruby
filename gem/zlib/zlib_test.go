package zlib

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
)

func TestDeflateBlockCanResume(t *testing.T) {
	testZlib(t, `
		input = "abcd" * 12500
		stream = Zlib::Deflate.new(Zlib::NO_COMPRESSION)
		stream << input
		first = nil
		result = stream.finish do |chunk|
			first = chunk
			break :stopped
		end
		result == :stopped && first.bytesize == 16384 &&
			Zlib.inflate(first + stream.finish) == input
	`)
}

func TestInflateReusesOutputBuffer(t *testing.T) {
	testZlib(t, `
		input = "a" * 40000
		compressed = Zlib.deflate(input)
		stream = Zlib::Inflate.new
		buffer = +""
		chunks = []
		same_buffer = true
		stream.inflate(compressed, buffer: buffer) do |chunk|
			same_buffer &&= chunk.equal?(buffer)
			chunks << chunk.dup
		end
		same_buffer && chunks.map(&:bytesize) == [16384, 16384, 7232] &&
			chunks.join == input && buffer == chunks.last && buffer.encoding == Encoding::UTF_8
	`)
}

func TestInflatePresetDictionary(t *testing.T) {
	testZlib(t, `
		dict = "preset dictionary"
		deflator = Zlib::Deflate.new
		deflator.set_dictionary(dict)
		compressed = deflator.deflate(dict, Zlib::FINISH)

		stream = Zlib::Inflate.new
		checksum = begin
			stream << compressed
			nil
		rescue Zlib::NeedDict
			stream.adler
		end
		wrong_dictionary = begin
			stream.set_dictionary("wrong dictionary")
			false
		rescue Zlib::DataError
			true
		end
		stream.set_dictionary(dict)
		recovered = stream.finish
		stream.reset
		stream.add_dictionary(dict)
		checksum == Zlib.adler32(dict) && wrong_dictionary &&
			recovered == dict && stream.inflate(compressed) == dict
	`)
}

func TestDeflateCopiesNativeStream(t *testing.T) {
	testZlib(t, `
		stream = Zlib::Deflate.new
		stream.avail_out = 10
		stream << "prefix "
		available = stream.avail_out
		copy = stream.dup
		stream << "one"
		original = stream.finish
		stream.close
		copy.params(Zlib::BEST_SPEED, Zlib::HUFFMAN_ONLY)
		copy << "two"
		available == 8 &&
			Zlib.inflate(original) == "prefix one" &&
			Zlib.inflate(copy.finish) == "prefix two"
	`)
}

func TestNativeChecksums(t *testing.T) {
	testZlib(t, `
		Zlib.adler32(nil, 42) == 1 &&
			Zlib.adler32("", -1) == 917518 &&
			Zlib.crc32(nil, 42) == 0 &&
			Zlib.adler32_combine(Zlib.adler32("foo"), Zlib.adler32("bar"), 3) == Zlib.adler32("foobar") &&
			Zlib.crc32_combine(Zlib.crc32("foo"), Zlib.crc32("bar"), 3) == Zlib.crc32("foobar")
	`)
}

func testZlib(t *testing.T, src string) {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("zlib"); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(src)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Bool() {
		t.Fatal("Ruby compatibility assertion failed")
	}
}
