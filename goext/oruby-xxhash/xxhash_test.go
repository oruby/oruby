package orubyxxhash_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/oruby-xxhash"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestXXhash(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "xxhash"
require "stringio"

# Official XXHash test vectors
raise "xxh32 empty: #{XXhash.xxh32('', 0)}" unless XXhash.xxh32("", 0) == 0x02cc5d05
raise "xxh64 empty: #{XXhash.xxh64('', 0)}" unless XXhash.xxh64("", 0) == 0xef46db3751d8e999

payload = "Nobody inspects the spammish repetition"
h32 = XXhash.xxh32(payload, 0)
h64 = XXhash.xxh64(payload, 0)
raise "xxh32 vec: #{h32.to_s(16)}" unless h32 == 0xe2293b2f
raise "xxh64 vec: #{h64.to_s(16)}" unless h64 == 0xfbcea83c8a378bf1

# Streaming matches one-shot across chunk boundaries
s32 = XXhash::StreamingHash32.new(0)
s64 = XXhash::StreamingHash64.new(0)
payload.chars.each_slice(7) { |c| s32 << c.join; s64 << c.join }
raise "stream32" unless s32.digest == h32
raise "stream64" unless s64.digest == h64

raise "io32" unless XXhash.xxh32_stream(StringIO.new(payload), 0, 5) == h32
raise "io64" unless XXhash.xxh64_stream(StringIO.new(payload), 0, 5) == h64
`)
	if err != nil {
		t.Fatalf("xxhash test failed: %v", err)
	}
}
