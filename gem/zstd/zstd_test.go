package zstd_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/stdlib"
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
rescue RuntimeError
  raised = true
end
raise "expected RuntimeError" unless raised

# Streaming
sc = Zstd::StreamingCompress.new(level: 3)
out = +"".b
out << sc.compress("chunk1-")
out << sc.flush
out << sc.compress("chunk2")
out << sc.finish

sd = Zstd::StreamingDecompress.new
raise "streaming roundtrip" unless sd.decompress(out) == "chunk1-chunk2"

# 2.0.10 API: dictionaries, skippable frames, concatenated frames, IO writer
raise "version" unless Zstd::VERSION == "2.0.10" && Zstd.zstd_version == 10507
raise "empty frame" unless Zstd.compress("").bytesize == 9
dict = "dictionary content " * 20
c = Zstd.compress(plain, dict: Zstd::CDict.new(dict, 5))
raise "dict" unless Zstd.decompress(c, dict: Zstd::DDict.new(dict)) == plain
raise "frames" unless Zstd.decompress(Zstd.compress("a") + Zstd.write_skippable_frame("", "x") + Zstd.compress("b")) == "ab"
raise "skippable" unless Zstd.read_skippable_frame(Zstd.write_skippable_frame("", "meta", magic_variant: 3)) == "meta"
require "stringio"
io = StringIO.new
w = Zstd::StreamWriter.new(io)
w.write("hello ", "world")
w.finish
raise "stream writer" unless Zstd.decompress(io.string) == "hello world"
`)
	if err != nil {
		t.Fatalf("zstd test failed: %v", err)
	}
}
