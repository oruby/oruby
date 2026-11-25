package orubysnappy_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/oruby-snappy"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestSnappy(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "snappy"
require "stringio"

data = ("hello snappy world! " * 200).b
comp = Snappy.deflate(data)
raise "should shrink" unless comp.bytesize < data.bytesize
raise "roundtrip" unless Snappy.inflate(comp) == data
raise "compress/uncompress" unless Snappy.uncompress(Snappy.compress(data)) == data

# Framing stream Writer / Reader
io = StringIO.new
Snappy::Writer.new(io) { |w| w << "part1-" << "part2-" << ("z" * 100) }
io.rewind
out = Snappy::Reader.new(io).read
raise "framing" unless out == "part1-part2-" + ("z" * 100)
`)
	if err != nil {
		t.Fatalf("snappy test failed: %v", err)
	}
}
