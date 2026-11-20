package zlibgo

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
)

func TestDeflateBlockCanResume(t *testing.T) {
	testZlibGo(t, `
		input = "abcd" * 12500
		stream = ZlibGo::Deflate.new(ZlibGo::NO_COMPRESSION)
		stream << input
		first = nil
		result = stream.finish do |chunk|
			first = chunk
			break :stopped
		end
		result == :stopped && first.bytesize == 16384 &&
			ZlibGo.inflate(first + stream.finish) == input
	`)
}

func TestInflateReusesOutputBuffer(t *testing.T) {
	testZlibGo(t, `
		input = "a" * 40000
		compressed = ZlibGo.deflate(input)
		stream = ZlibGo::Inflate.new
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
	testZlibGo(t, `
		dict = "preset dictionary"
		deflator = ZlibGo::Deflate.new
		deflator.set_dictionary(dict)
		compressed = deflator.deflate(dict, ZlibGo::FINISH)

		stream = ZlibGo::Inflate.new
		checksum = begin
			stream << compressed
			nil
		rescue ZlibGo::NeedDict
			stream.adler
		end
		wrong_dictionary = begin
			stream.set_dictionary("wrong dictionary")
			false
		rescue ZlibGo::DataError
			true
		end
		stream.set_dictionary(dict)
		recovered = stream.finish
		stream.reset
		stream.add_dictionary(dict)
		checksum == ZlibGo.adler32(dict) && wrong_dictionary &&
			recovered == dict && stream.inflate(compressed) == dict
	`)
}

func testZlibGo(t *testing.T, src string) {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	got, err := mrb.Eval(src)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Bool() {
		t.Fatal("Ruby compatibility assertion failed")
	}
}
