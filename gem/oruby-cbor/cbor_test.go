package orubycbor_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/oruby-cbor"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestCBORWebAuthnCOSE(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "cbor"

# COSE EC2 public key map (integer & negative integer keys + binary strings)
cose_key = {
  1 => 2,
  3 => -7,
  -1 => 1,
  -2 => ("\xaa" * 32).b,
  -3 => ("\xbb" * 32).b,
}
bin = CBOR.encode(cose_key)
dec = CBOR.decode(bin)
raise "cose kty" unless dec[1] == 2 && dec[3] == -7 && dec[-1] == 1
raise "cose x" unless dec[-2] == ("\xaa" * 32).b && dec[-2].encoding == Encoding::ASCII_8BIT
raise "cose y" unless dec[-3] == ("\xbb" * 32).b

# UTF-8 string vs binary string distinction
mixed = { "text" => "héllo", "bytes" => "\x00\xff\x01".b, "nums" => [0, -42, 3.25, true, false, nil] }
dec_mixed = CBOR.decode(mixed.to_cbor)
raise "utf8" unless dec_mixed["text"] == "héllo" && dec_mixed["text"].encoding == Encoding::UTF_8
raise "bytes" unless dec_mixed["bytes"] == "\x00\xff\x01".b && dec_mixed["bytes"].encoding == Encoding::ASCII_8BIT
raise "nums" unless dec_mixed["nums"] == [0, -42, 3.25, true, false, nil]

# CBOR::Tagged (both RFC epoch tag 1 and URI tag 32)
t = CBOR::Tagged.new(1, 1700000000)
t2 = CBOR.decode(CBOR.encode(t))
raise "tagged 1" unless t2.is_a?(CBOR::Tagged) && t2.tag == 1 && t2.value == 1700000000
u = CBOR::Tagged.new(32, "https://example.com")
u2 = CBOR.decode(CBOR.encode(u))
raise "tagged 32" unless u2.is_a?(CBOR::Tagged) && u2.tag == 32 && u2.value == "https://example.com"

# Packer / Unpacker streaming
packer = CBOR::Packer.new
packer.write("one").write(2).write([3])
unpacker = CBOR::Unpacker.new
unpacker.feed(packer.to_s)
items = unpacker.each.to_a
raise "stream: #{items}" unless items == ["one", 2, [3]]
`)
	if err != nil {
		t.Fatalf("cbor test failed: %v", err)
	}
}
