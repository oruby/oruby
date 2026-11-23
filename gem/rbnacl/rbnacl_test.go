package rbnacl_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/rbnacl"
)

func TestRbNaCl(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "rbnacl"

# Random & Hash & Util
k = RbNaCl::Random.random_bytes(32)
raise "random len" unless k.bytesize == 32
raise "sha256 len" unless RbNaCl::Hash.sha256("abc").bytesize == 32
raise "blake2b" unless RbNaCl::Hash.blake2b("abc", digest_size: 32).bytesize == 32
raise "hex" unless RbNaCl::Util.hex2bin(RbNaCl::Util.bin2hex(k)) == k
raise "verify32" unless RbNaCl::Util.verify32(k, k.dup)

# HMAC
tag = RbNaCl::HMAC::SHA256.auth(k, "hello")
raise "hmac ok" unless RbNaCl::HMAC::SHA256.verify(k, tag, "hello")

# SecretBox (XSalsa20-Poly1305)
sb = RbNaCl::SecretBox.new(k)
nonce = RbNaCl::Random.random_bytes(sb.nonce_bytes)
ct = sb.encrypt(nonce, "top secret")
raise "secretbox" unless sb.decrypt(nonce, ct) == "top secret"

# Public-key Box (Curve25519-XSalsa20-Poly1305)
alice = RbNaCl::PrivateKey.generate
bob   = RbNaCl::PrivateKey.generate
abox  = RbNaCl::Box.new(bob.public_key, alice)
bbox  = RbNaCl::Box.new(alice.public_key, bob)
ct2   = abox.encrypt(nonce, "hello bob")
raise "box" unless bbox.decrypt(nonce, ct2) == "hello bob"

# SimpleBox
sbox = RbNaCl::SimpleBox.from_secret_key(k)
raise "simplebox" unless sbox.decrypt(sbox.encrypt("easy")) == "easy"

# Ed25519 SigningKey / VerifyKey
sk = RbNaCl::SigningKey.generate
sig = sk.sign("signed message")
raise "ed25519 ok" unless sk.verify_key.verify(sig, "signed message")
bad = false
begin
  sk.verify_key.verify(sig, "tampered")
rescue RbNaCl::BadSignatureError
  bad = true
end
raise "expected BadSignatureError" unless bad

# AEAD ChaCha20Poly1305IETF & XChaCha20Poly1305IETF
aead = RbNaCl::AEAD::ChaCha20Poly1305IETF.new(k)
n12 = RbNaCl::Random.random_bytes(12)
c12 = aead.encrypt(n12, "payload", "aad")
raise "chacha" unless aead.decrypt(n12, c12, "aad") == "payload"

xaead = RbNaCl::AEAD::XChaCha20Poly1305IETF.new(k)
n24 = RbNaCl::Random.random_bytes(24)
c24 = xaead.encrypt(n24, "xpayload", "xaad")
raise "xchacha" unless xaead.decrypt(n24, c24, "xaad") == "xpayload"
`)
	if err != nil {
		t.Fatalf("rbnacl test failed: %v", err)
	}
}
