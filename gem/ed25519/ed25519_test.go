package ed25519_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/ed25519"
	_ "github.com/oruby/oruby/gem/load"
)

func TestEd25519(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "ed25519"

		raise "self_test failed" unless Ed25519.self_test

		# Generate random signing key and sign/verify
		sk = Ed25519::SigningKey.generate
		raise "seed len" unless sk.seed.bytesize == 32
		raise "keypair len" unless sk.keypair.bytesize == 64
		raise "pub len" unless sk.verify_key.to_bytes.bytesize == 32

		msg = "hello ed25519 in oruby"
		sig = sk.sign(msg)
		raise "sig len" unless sig.bytesize == 64
		raise "sig binary" unless sig.encoding == Encoding::BINARY
		raise "verify" unless sk.verify_key.verify(sig, msg)

		# Restore from seed and from keypair
		sk2 = Ed25519::SigningKey.new(sk.to_bytes)
		raise "seed restore" unless sk2.sign(msg) == sig
		sk3 = Ed25519::SigningKey.from_keypair(sk.keypair)
		raise "keypair restore" unless sk3.sign(msg) == sig
		raise "verify restored" unless Ed25519::VerifyKey.new(sk.verify_key.to_bytes).verify(sig, msg)

		# Corrupt keypair rejected
		bad_kp = sk.keypair.byteslice(0, 63) + ((sk.keypair.getbyte(63) ^ 1).chr)
		raised = false
		begin
			Ed25519::SigningKey.from_keypair(bad_kp)
		rescue ArgumentError
			raised = true
		end
		raise "expected ArgumentError on corrupt keypair" unless raised

		# Tampered message rejected with VerifyError
		raised = false
		begin
			sk.verify_key.verify(sig, msg + "!")
		rescue Ed25519::VerifyError
			raised = true
		end
		raise "expected VerifyError on tampered message" unless raised

		# Tampered signature rejected with VerifyError
		bad_sig = sig.byteslice(0, 63) + ((sig.getbyte(63) ^ 1).chr)
		raised = false
		begin
			sk.verify_key.verify(bad_sig, msg)
		rescue Ed25519::VerifyError
			raised = true
		end
		raise "expected VerifyError on tampered signature" unless raised

		# Short seed rejected
		raised = false
		begin
			Ed25519::SigningKey.new("short")
		rescue ArgumentError
			raised = true
		end
		raise "expected ArgumentError on short seed" unless raised
	`)
	if err != nil {
		t.Fatalf("Ed25519 test failed: %v", err)
	}
}

func TestEd25519Ref10Direct(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "ed25519_ref10"
		kp = Ed25519::Provider::Ref10.create_keypair("A" * 32)
		raise "kp len" unless kp.bytesize == 64
		pub = kp.byteslice(32, 32)
		sig = Ed25519::Provider::Ref10.sign(kp, "test")
		raise "verify true" unless Ed25519::Provider::Ref10.verify(pub, sig, "test") == true
		raise "verify false" unless Ed25519::Provider::Ref10.verify(pub, sig, "other") == false
	`)
	if err != nil {
		t.Fatalf("Ed25519 ref10 direct test failed: %v", err)
	}
}
