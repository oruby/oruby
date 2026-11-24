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

# Version frozen
raise "Version frozen" unless StringScanner::Version.is_a?(String) && StringScanner::Version.frozen?

# integer_at
si = StringScanner.new("before 20260514 0xa_f 111")
si.skip_until(" ")
raise "integer_at scan" unless si.scan(/(?<y>\d{4})(?<m>\d{2})(?<d>\d{2})/) == "20260514"
raise "integer_at 0" unless si.integer_at(0) == 20260514
raise "integer_at 1" unless si.integer_at(1) == 2026
raise "integer_at :m" unless si.integer_at(:m) == 5
raise "integer_at 'd'" unless si.integer_at("d") == 14
raise "integer_at -1" unless si.integer_at(-1) == 14
raise "integer_at oob" unless si.integer_at(4).nil?
si.skip(" ")
si.scan(/0x[\h_]+/)
raise "integer_at base 0" unless si.integer_at(0, 0) == 0xaf
si.skip(" ")
si.scan(/\d+/)
raise "integer_at base 2" unless si.integer_at(0, 2) == 7

# matched / matched_size when underlying string shrinks
sh = StringScanner.new(+"before 29 after")
sh.skip_until(" ")
sh.scan(/\d+/)
raise "shrunk 0" unless sh.matched == "29" && sh.matched_size == 2
sh.string.replace("before 2")
raise "shrunk 1" unless sh.matched == "2" && sh.matched_size == 1 && sh.integer_at(0) == 2
sh.string.replace("before ")
raise "shrunk 2" unless sh.matched == "" && sh.matched_size == 0 && sh.integer_at(0).nil?
sh.string.replace("before")
raise "shrunk 3" unless sh.matched.nil? && sh.matched_size.nil? && sh.integer_at(0).nil?

# named_captures same-name union preserves matching branch
su = StringScanner.new("123")
raise "union match" unless su.match?(/(?<number>0)|(?<number>1)|(?<number>2)/) == 1
raise "union named_captures: #{su.named_captures.inspect}" unless su.named_captures == {"number" => "1"}
`)
	if err != nil {
		t.Fatalf("strscan test failed: %v", err)
	}
}
