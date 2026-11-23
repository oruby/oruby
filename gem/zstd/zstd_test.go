package zstd_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/zstd"
)

func TestZstd(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "zstd-ruby"

plain = "Hello Zstandard compression! " * 200
comp = Zstd.compress(plain, level: 5)
raise "should compress" unless comp.bytesize < plain.bytesize
raise "roundtrip" unless Zstd.decompress(comp) == plain

raised = false
begin
  Zstd.decompress("not zstd data".b)
rescue Zstd::Error
  raised = true
end
raise "expected Zstd::Error" unless raised

# Streaming
sc = Zstd::StreamingCompress.new(level: 3)
out = +"".b
out << sc.compress("chunk1-")
out << sc.flush
out << sc.compress("chunk2")
out << sc.finish

sd = Zstd::StreamingDecompress.new
raise "streaming roundtrip" unless sd.decompress(out) == "chunk1-chunk2"
`)
	if err != nil {
		t.Fatalf("zstd test failed: %v", err)
	}
}
