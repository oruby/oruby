package websocket_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/goext/websocket"
)

func TestWebSocketMask(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "websocket_mask"

		mask = "\x37\xfa\x21\x3d".b

		# RFC 6455 section 5.7 example: "Hello" masked with 0x37 0xfa 0x21 0x3d
		# -> 0x7f 0x9f 0x4d 0x51 0x58
		orig = "Hello".b
		masked = WebSocket::Mask.mask(orig, mask)
		raise "input mutated" unless orig == "Hello".b
		raise "binary encoding" unless masked.encoding == Encoding::BINARY
		raise "RFC 6455 mismatch: #{masked.unpack1('H*')}" unless masked.unpack1("H*") == "7f9f4d5158"
		raise "unmask mismatch" unless WebSocket::Mask.mask(masked, mask) == orig

		# Lengths across 8-byte word boundaries (0..33 and 10000)
		((0..33).to_a + [10000]).each do |len|
			payload = (0...len).map { |i| (i * 31 + 7) & 0xff }.pack("C*")
			expected = (0...len).map { |i| payload.getbyte(i) ^ mask.getbyte(i % 4) }.pack("C*")
			got = WebSocket::Mask.mask(payload, mask)
			raise "len #{len} mismatch" unless got == expected
			raise "len #{len} involution" unless WebSocket::Mask.mask(got, mask) == payload
		end

		# nil or non-4-byte mask returns payload unchanged
		p_str = "unchanged"
		raise "nil mask" unless WebSocket::Mask.mask(p_str, nil).equal?(p_str)
		raise "short mask" unless WebSocket::Mask.mask(p_str, "abc").equal?(p_str)
		raise "long mask" unless WebSocket::Mask.mask(p_str, "abcde").equal?(p_str)
	`)
	if err != nil {
		t.Fatalf("WebSocket::Mask test failed: %v", err)
	}
}

func TestWebSocketMaskAliases(t *testing.T) {
	for _, req := range []string{"websocket-driver/websocket_mask", "websocket/mask", "websocket/websocket_mask"} {
		mrb := oruby.MrbOpen()
		_, err := mrb.Eval(`
			require "` + req + `"
			raise "missing mask" unless WebSocket::Mask.mask("Hello", "\x37\xfa\x21\x3d".b).unpack1("H*") == "7f9f4d5158"
		`)
		mrb.Close()
		if err != nil {
			t.Fatalf("alias %q failed: %v", req, err)
		}
	}
}
