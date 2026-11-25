package orubydigestcrc_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/oruby-digest-crc"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestDigestCRC(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "digest/crc32"
require "digest/crc32c"
require "digest/crc64_xz"
require "digest/crc16_ccitt"

# Standard RFC/IEEE check string "123456789"
raise "crc32: #{Digest::CRC32.hexdigest('123456789')}" unless Digest::CRC32.hexdigest("123456789") == "cbf43926"
raise "crc32c: #{Digest::CRC32c.hexdigest('123456789')}" unless Digest::CRC32c.hexdigest("123456789") == "e3069283"
raise "crc64xz: #{Digest::CRC64XZ.hexdigest('123456789')}" unless Digest::CRC64XZ.hexdigest("123456789") == "995dc9bbdf1939fa"
raise "crc16ccitt: #{Digest::CRC16CCITT.hexdigest('123456789')}" unless Digest::CRC16CCITT.hexdigest("123456789") == "29b1"

# Streaming update + reset
c = Digest::CRC32c.new
c << "1234" << "56789"
raise "stream" unless c.checksum == 0xe3069283 && c.digest.bytesize == 4
c.reset
c.update("123456789")
raise "reset" unless c.hexdigest == "e3069283"
`)
	if err != nil {
		t.Fatalf("digest-crc test failed: %v", err)
	}
}
