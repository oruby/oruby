package strscan_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/regexp/full"
	_ "github.com/oruby/oruby/gem/strscan"
)

func TestStringScanner(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "strscan"

s = StringScanner.new("This is an example string")
raise "eos?" if s.eos?
raise "bol?" unless s.bol?
raise "scan 1" unless s.scan(/\w+/) == "This"
raise "pos" unless s.pos == 4
raise "scan nil" unless s.scan(/\w+/).nil?
raise "skip" unless s.skip(/\s+/) == 1
raise "check" unless s.check(/\w+/) == "is" && s.pos == 5
raise "match?" unless s.match?(/\w+/) == 2 && s.pos == 5
raise "scan_until" unless s.scan_until(/example/) == "is an example"
raise "pre_match" unless s.pre_match == "This is an "
raise "post_match" unless s.post_match == " string"
raise "matched" unless s.matched == "example" && s.matched_size == 7
s.unscan
raise "unscan pos" unless s.pos == 5

# Named captures & values_at
s2 = StringScanner.new("key=42 rest")
raise "named" unless s2.scan(/(?<k>\w+)=(?<v>\d+)/) == "key=42"
raise "[] sym" unless s2[:k] == "key" && s2[:v] == "42"
raise "[] int" unless s2[0] == "key=42" && s2[1] == "key" && s2[-1] == "42"
raise "captures" unless s2.captures == ["key", "42"]
raise "named_captures" unless s2.named_captures == { "k" => "key", "v" => "42" }
raise "values_at" unless s2.values_at(1, :v) == ["key", "42"]

# String literal pattern (strscan 3.1+)
s3 = StringScanner.new("hello world")
raise "str scan" unless s3.scan("hello") == "hello"
raise "str scan_until" unless s3.scan_until("ld") == " world"

# fixed_anchor mode
fa = StringScanner.new("foo\nbar", fixed_anchor: true)
raise "fa 1" unless fa.scan(/\Afoo/) == "foo"
raise "fa 2" unless fa.scan(/\A\n/).nil?
raise "fa 3" unless fa.scan(/\nbar/) == "\nbar"

# getch, get_byte, scan_byte, peek, scan_integer, concat, dup
s4 = StringScanner.new("é-123 0x1f")
raise "getch utf8" unless s4.getch == "é" && s4.charpos == 1 && s4.pos == 2
raise "scan_integer 10" unless s4.scan_integer == -123
s4.skip(/\s+/)
raise "scan_integer 16" unless s4.scan_integer(base: 16) == 31
s4 << "!"
raise "peek_byte" unless s4.peek_byte == "!".ord
raise "scan_byte" unless s4.scan_byte == "!".ord && s4.eos?

# unscan without scan raises StringScanner::Error
raised = false
begin
  StringScanner.new("abc").unscan
rescue StringScanner::Error
  raised = true
end
raise "expected StringScanner::Error" unless raised
`)
	if err != nil {
		t.Fatalf("strscan test failed: %v", err)
	}
}
