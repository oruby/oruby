package scrypt_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/scrypt"
)

func TestSCrypt(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "scrypt"

pw = SCrypt::Password.create("my secret", cost: "400$8$1$")
raise "pw is String" unless pw.is_a?(String)
raise "pw match" unless pw == "my secret"
raise "pw mismatch" if pw == "wrong"

reloaded = SCrypt::Password.new(pw.to_s)
raise "reloaded match" unless reloaded.is_password?("my secret")
raise "cost" unless reloaded.cost == "400$8$1$"

# Direct raw scrypt key derivation
dk = SCrypt::Engine.scrypt("password", "NaCl", 1024, 8, 16, 64)
raise "raw scrypt len" unless dk.bytesize == 64
raise "raw scrypt vec" unless dk.unpack1("H*").start_with?("fdbabe1c9d3472007856e7190d01e9fe")
`)
	if err != nil {
		t.Fatalf("scrypt test failed: %v", err)
	}
}
