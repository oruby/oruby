package bcrypt_pbkdf_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/bcrypt_pbkdf"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
)

func TestBCryptPbkdfVectors(t *testing.T) {
	mrb, err := oruby.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mrb.Close()

	_, err = mrb.Eval(`
		require "bcrypt_pbkdf"

		k1 = BCryptPbkdf.key("my secret", "my salt", 64, 10)
		raise "enc" unless k1.encoding == Encoding::BINARY
		raise "k1: #{k1.unpack1('H*')}" unless k1.unpack1("H*") == "3e4ae360ab9c7b105251f9fa75982b05768f0d801f7d79ce468a3b7f98861b55fe98663372ec17ca942c31172337a0fc0c6b84265d8f6bb96689b7c9f7250293"

		k2 = BCryptPbkdf.key("password", "salt", 32, 4)
		raise "k2: #{k2.unpack1('H*')}" unless k2.unpack1("H*") == "5bbf0cc293587f1c3635555c27796598d47e579071bf427e9d8fbe842aba34d9"

		k3 = BCryptPbkdf.key("pass", "1234567890123456", 48, 2)
		raise "k3: #{k3.unpack1('H*')}" unless k3.unpack1("H*") == "f940f823db525494f09fcb3d117f6ae6a1a3717f57080dd5813f1195c419391edab3906156754fd94da6d3985ba8879c"

		# Low-level Engine method matches too
		k4 = BCryptPbkdf::Engine.__bc_crypt_pbkdf("password", "salt", 32, 4)
		raise "engine mismatch" unless k4 == k2

		# Upstream bcrypt_pbkdf-1.1.2 test_ext.rb vectors
		table = [
			["pass2", "salt2", 12, 2, [214, 14, 48, 162, 131, 206, 121, 176, 50, 104, 231, 252]],
			["\u0000\u0001foo", "\u0001\u0002fooo3", 14, 5, [46, 189, 32, 185, 94, 85, 232, 10, 84, 26, 44, 161, 49, 126]],
			["doozoasd", "fooo$AS!", 14, 22, [57, 62, 50, 107, 70, 155, 65, 5, 129, 211, 189, 169, 188, 65]],
		]
		table.each do |p, s, l, r, want|
			raise "upstream key mismatch" unless BCryptPbkdf.key(p, s, l, r).bytes == want
			raise "upstream pbkdf mismatch" unless BCryptPbkdf::Engine.__bc_crypt_pbkdf(p, s, l, r).bytes == want
		end
		h = BCryptPbkdf::Engine.__bc_crypt_hash("a" * 64, "b" * 64)
		raise "hash len" unless h.bytesize == 32 && h.encoding == Encoding::BINARY
	`)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
}
