package brotli_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/brotli"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
)

func TestBrotli(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "brotli"

plain = "Hello Brotli! " * 200
comp = Brotli.deflate(plain, quality: 5)
raise "should compress" unless comp.bytesize < plain.bytesize
raise "roundtrip" unless Brotli.inflate(comp) == plain

# Invalid data raises Brotli::Error
raised = false
begin
  Brotli.inflate("\xFF\xFF\xFF\xFF\xFF".b)
rescue Brotli::Error
  raised = true
end
raise "expected Brotli::Error" unless raised

# Writer & Reader with IO-like buffer
buf = +"".b
io = Object.new
io.define_singleton_method(:write) { |s| buf << s; s.bytesize }
io.define_singleton_method(:read) { buf }

w = Brotli::Writer.new(io, quality: 4)
w.write("part1-")
w.flush
w.write("part2")
w.finish

r = Brotli::Reader.new(io)
raise "reader read" unless r.read == "part1-part2"
raise "reader eof" unless r.eof?
`)
	if err != nil {
		t.Fatalf("brotli test failed: %v", err)
	}
}
