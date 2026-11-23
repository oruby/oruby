package orubylz4_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/oruby-lz4"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestLZ4(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "lz4-ruby"

data = ("ORuby LZ4 compression test payload! " * 100).b
c1 = LZ4.compress(data)
raise "shrink" unless c1.bytesize < data.bytesize
raise "roundtrip" unless LZ4.uncompress(c1) == data

c2 = LZ4.compressHC(data)
raise "hc roundtrip" unless LZ4.uncompress(c2) == data

raw = LZ4.raw_compress(data)
raise "raw roundtrip" unless LZ4.raw_uncompress(raw, data.bytesize) == data

fr = LZ4::Frame.compress(data)
raise "frame roundtrip" unless LZ4::Frame.decompress(fr) == data
`)
	if err != nil {
		t.Fatalf("lz4 test failed: %v", err)
	}
}
