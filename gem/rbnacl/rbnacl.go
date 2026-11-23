// Package rbnacl provides a pure-Go implementation of the Ruby RbNaCl gem
// (NaCl/libsodium primitives: SecretBox, Box, SigningKey/VerifyKey, Blake2b,
// HMAC, AEAD ChaCha20Poly1305IETF & XChaCha20Poly1305IETF, SimpleBox, Random, Util)
// backed by Go stdlib crypto and golang.org/x/crypto.
package rbnacl

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"fmt"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"
)

func init() {
	oruby.LazyGem("rbnacl", initRbNaCl)
	oruby.GemAlias("rbnacl/libsodium", "rbnacl")
}

func binStr(mrb *oruby.MrbState, b []byte) oruby.Value {
	return mrb.Call(mrb.BytesValue(b), "force_encoding", mrb.StrNew("ASCII-8BIT"))
}

type secretBoxData struct {
	key [32]byte
}

type pubKeyData struct {
	key [32]byte
}

type privKeyData struct {
	priv [32]byte
	pub  [32]byte
}

type boxData struct {
	shared [32]byte
}

type signKeyData struct {
	priv ed25519.PrivateKey
}

type verifyKeyData struct {
	pub ed25519.PublicKey
}

type aeadData struct {
	key   []byte
	xmode bool // false=ChaCha20Poly1305IETF (12-byte nonce), true=XChaCha20Poly1305IETF (24-byte nonce)
}

func initRbNaCl(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("RbNaCl")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("7.1.2"))

	stdErr := mrb.ExcGet("StandardError")
	cryptoErr := mrb.DefineClassUnder(mod, "CryptoError", stdErr)
	badSigErr := mrb.DefineClassUnder(mod, "BadSignatureError", cryptoErr)
	badAuthErr := mrb.DefineClassUnder(mod, "BadAuthenticatorError", cryptoErr)
	lenErr := mrb.DefineClassUnder(mod, "LengthError", mrb.ExcGet("ArgumentError"))

	// RbNaCl::Random
	rndMod := mrb.DefineModuleUnder(mod, "Random")
	rndMod.DefineClassMethod("random_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		n := 32
		if args := mrb.GetArgs(); args.Len() >= 1 && !args.Item(0).IsNil() {
			n = int(args.Item(0).Int64())
		}
		if n < 0 {
			return mrb.Raise(mrb.EArgumentError(), "negative length")
		}
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			return mrb.Raise(cryptoErr, err.Error())
		}
		return binStr(mrb, b)
	}, mrb.ArgsOpt(1))

	// RbNaCl::Util
	utilMod := mrb.DefineModuleUnder(mod, "Util")
	defineVerifyN := func(name string, sz int) {
		utilMod.DefineClassMethod(name, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			a := mrb.GetArgs().Slice()
			x, y := a[0].Bytes(), a[1].Bytes()
			if len(x) != sz || len(y) != sz {
				return mrb.Raise(lenErr, fmt.Sprintf("expected %d-byte strings", sz))
			}
			return mrb.BoolValue(subtle.ConstantTimeCompare(x, y) == 1)
		}, mrb.ArgsReq(2))
	}
	defineVerifyN("verify16", 16)
	defineVerifyN("verify32", 32)
	defineVerifyN("verify64", 64)

	utilMod.DefineClassMethod("bin2hex", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.StrNew(hex.EncodeToString(mrb.GetArgsFirst().Bytes()))
	}, mrb.ArgsReq(1))
	utilMod.DefineClassMethod("hex2bin", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		b, err := hex.DecodeString(mrb.GetArgsFirst().String())
		if err != nil {
			return mrb.Raise(mrb.EArgumentError(), err.Error())
		}
		return binStr(mrb, b)
	}, mrb.ArgsReq(1))

	// RbNaCl::Hash
	hashMod := mrb.DefineModuleUnder(mod, "Hash")
	hashMod.DefineClassMethod("sha256", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		sum := sha256.Sum256(mrb.GetArgsFirst().Bytes())
		return binStr(mrb, sum[:])
	}, mrb.ArgsReq(1))
	hashMod.DefineClassMethod("sha512", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		sum := sha512.Sum512(mrb.GetArgsFirst().Bytes())
		return binStr(mrb, sum[:])
	}, mrb.ArgsReq(1))
	hashMod.DefineClassMethod("blake2b", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		data := args.Item(0).Bytes()
		var key []byte
		digestSize := 64
		parse := func(h oruby.Value) {
			if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("key").Value()); err == nil && v.IsString() {
				key = v.Bytes()
			}
			if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("digest_size").Value()); err == nil && v.IsInt() {
				digestSize = int(v.Int64())
			}
		}
		if args.Len() >= 2 && args.Item(1).IsHash() {
			parse(args.Item(1))
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			parse(kw)
		}
		h, err := blake2b.New(digestSize, key)
		if err != nil {
			return mrb.Raise(lenErr, err.Error())
		}
		h.Write(data)
		return binStr(mrb, h.Sum(nil))
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	// RbNaCl::HMAC::{SHA256, SHA512, SHA512256}
	hmacMod := mrb.DefineModuleUnder(mod, "HMAC")
	defineHMAC := func(name string, hashFn func(key, data []byte) []byte) {
		cls := mrb.DefineClassUnder(hmacMod, name, mrb.ObjectClass())
		cls.DefineClassMethod("auth", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			a := mrb.GetArgs().Slice()
			return binStr(mrb, hashFn(a[0].Bytes(), a[1].Bytes()))
		}, mrb.ArgsReq(2))
		cls.DefineClassMethod("verify", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			a := mrb.GetArgs().Slice()
			got := hashFn(a[0].Bytes(), a[2].Bytes())
			if subtle.ConstantTimeCompare(got, a[1].Bytes()) != 1 {
				return mrb.Raise(badAuthErr, "HMAC authenticator failed verification")
			}
			return mrb.TrueValue()
		}, mrb.ArgsReq(3))
	}
	defineHMAC("SHA256", func(k, d []byte) []byte {
		m := hmac.New(sha256.New, k)
		m.Write(d)
		return m.Sum(nil)
	})
	defineHMAC("SHA512", func(k, d []byte) []byte {
		m := hmac.New(sha512.New, k)
		m.Write(d)
		return m.Sum(nil)
	})
	defineHMAC("SHA512256", func(k, d []byte) []byte {
		m := hmac.New(sha512.New, k)
		m.Write(d)
		return m.Sum(nil)[:32]
	})

	// RbNaCl::SecretBox (XSalsa20-Poly1305)
	sbCls := rb.DataClass(mrb.DefineClassUnder(mod, "SecretBox", mrb.ObjectClass()))
	mrb.ConstSet(sbCls, mrb.Intern("KEYBYTES"), mrb.FixnumValue(32))
	mrb.ConstSet(sbCls, mrb.Intern("NONCEBYTES"), mrb.FixnumValue(24))
	sbCls.DefineClassMethod("key_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(32) }, mrb.ArgsNone())
	sbCls.DefineClassMethod("nonce_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(24) }, mrb.ArgsNone())
	sbCls.DefineMethod("key_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(32) }, mrb.ArgsNone())
	sbCls.DefineMethod("nonce_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(24) }, mrb.ArgsNone())

	sbCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k := mrb.GetArgsFirst().Bytes()
		if len(k) != 32 {
			return mrb.Raise(lenErr, "SecretBox key must be 32 bytes")
		}
		var d secretBoxData
		copy(d.key[:], k)
		rb.Set(mrb, self, &d)
		return self
	}, mrb.ArgsReq(1))

	sbCls.DefineMethod("encrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*secretBoxData](mrb, self)
		a := mrb.GetArgs().Slice()
		nonceBytes := a[0].Bytes()
		if len(nonceBytes) != 24 {
			return mrb.Raise(lenErr, "SecretBox nonce must be 24 bytes")
		}
		var nonce [24]byte
		copy(nonce[:], nonceBytes)
		out := secretbox.Seal(nil, a[1].Bytes(), &nonce, &d.key)
		return binStr(mrb, out)
	}, mrb.ArgsReq(2))
	sbCls.DefineAlias("box", "encrypt")

	sbCls.DefineMethod("decrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*secretBoxData](mrb, self)
		a := mrb.GetArgs().Slice()
		nonceBytes := a[0].Bytes()
		if len(nonceBytes) != 24 {
			return mrb.Raise(lenErr, "SecretBox nonce must be 24 bytes")
		}
		var nonce [24]byte
		copy(nonce[:], nonceBytes)
		plain, ok := secretbox.Open(nil, a[1].Bytes(), &nonce, &d.key)
		if !ok {
			return mrb.Raise(cryptoErr, "Decryption failed. Ciphertext failed verification")
		}
		return binStr(mrb, plain)
	}, mrb.ArgsReq(2))
	sbCls.DefineAlias("open", "decrypt")

	// RbNaCl::PublicKey & PrivateKey (Curve25519)
	pubCls := rb.DataClass(mrb.DefineClassUnder(mod, "PublicKey", mrb.ObjectClass()))
	privCls := rb.DataClass(mrb.DefineClassUnder(mod, "PrivateKey", mrb.ObjectClass()))

	pubCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k := mrb.GetArgsFirst().Bytes()
		if len(k) != 32 {
			return mrb.Raise(lenErr, "PublicKey must be 32 bytes")
		}
		var d pubKeyData
		copy(d.key[:], k)
		rb.Set(mrb, self, &d)
		return self
	}, mrb.ArgsReq(1))
	pubCls.DefineMethod("to_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*pubKeyData](mrb, self)
		return binStr(mrb, d.key[:])
	}, mrb.ArgsNone())
	pubCls.DefineAlias("to_s", "to_bytes")

	privCls.DefineClassMethod("generate", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		pub, priv, err := box.GenerateKey(rand.Reader)
		if err != nil {
			return mrb.Raise(cryptoErr, err.Error())
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, privCls)
		rb.Set(mrb, obj.Value(), &privKeyData{priv: *priv, pub: *pub})
		return obj
	}, mrb.ArgsNone())

	privCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k := mrb.GetArgsFirst().Bytes()
		if len(k) != 32 {
			return mrb.Raise(lenErr, "PrivateKey must be 32 bytes")
		}
		var d privKeyData
		copy(d.priv[:], k)
		curve25519.ScalarBaseMult(&d.pub, &d.priv)
		rb.Set(mrb, self, &d)
		return self
	}, mrb.ArgsReq(1))
	privCls.DefineMethod("public_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*privKeyData](mrb, self)
		obj := mrb.ObjAlloc(oruby.MrbTTCData, pubCls)
		rb.Set(mrb, obj.Value(), &pubKeyData{key: d.pub})
		return obj
	}, mrb.ArgsNone())
	privCls.DefineMethod("to_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*privKeyData](mrb, self)
		return binStr(mrb, d.priv[:])
	}, mrb.ArgsNone())
	privCls.DefineAlias("to_s", "to_bytes")

	// RbNaCl::Box (Curve25519-XSalsa20-Poly1305)
	boxCls := rb.DataClass(mrb.DefineClassUnder(mod, "Box", mrb.ObjectClass()))
	boxCls.DefineClassMethod("nonce_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(24) }, mrb.ArgsNone())
	boxCls.DefineMethod("nonce_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(24) }, mrb.ArgsNone())

	boxCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		var peerPub, myPriv [32]byte
		if pk, ok := rb.Get[*pubKeyData](mrb, a[0]); ok {
			peerPub = pk.key
		} else if a[0].IsString() && len(a[0].Bytes()) == 32 {
			copy(peerPub[:], a[0].Bytes())
		} else {
			return mrb.Raise(lenErr, "expected 32-byte PublicKey")
		}
		if sk, ok := rb.Get[*privKeyData](mrb, a[1]); ok {
			myPriv = sk.priv
		} else if a[1].IsString() && len(a[1].Bytes()) == 32 {
			copy(myPriv[:], a[1].Bytes())
		} else {
			return mrb.Raise(lenErr, "expected 32-byte PrivateKey")
		}
		var d boxData
		box.Precompute(&d.shared, &peerPub, &myPriv)
		rb.Set(mrb, self, &d)
		return self
	}, mrb.ArgsReq(2))

	boxCls.DefineMethod("encrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*boxData](mrb, self)
		a := mrb.GetArgs().Slice()
		nb := a[0].Bytes()
		if len(nb) != 24 {
			return mrb.Raise(lenErr, "Box nonce must be 24 bytes")
		}
		var nonce [24]byte
		copy(nonce[:], nb)
		out := box.SealAfterPrecomputation(nil, a[1].Bytes(), &nonce, &d.shared)
		return binStr(mrb, out)
	}, mrb.ArgsReq(2))
	boxCls.DefineAlias("box", "encrypt")

	boxCls.DefineMethod("decrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*boxData](mrb, self)
		a := mrb.GetArgs().Slice()
		nb := a[0].Bytes()
		if len(nb) != 24 {
			return mrb.Raise(lenErr, "Box nonce must be 24 bytes")
		}
		var nonce [24]byte
		copy(nonce[:], nb)
		plain, ok := box.OpenAfterPrecomputation(nil, a[1].Bytes(), &nonce, &d.shared)
		if !ok {
			return mrb.Raise(cryptoErr, "Decryption failed. Ciphertext failed verification")
		}
		return binStr(mrb, plain)
	}, mrb.ArgsReq(2))
	boxCls.DefineAlias("open", "decrypt")

	// RbNaCl::SigningKey & VerifyKey (Ed25519)
	vkCls := rb.DataClass(mrb.DefineClassUnder(mod, "VerifyKey", mrb.ObjectClass()))
	skCls := rb.DataClass(mrb.DefineClassUnder(mod, "SigningKey", mrb.ObjectClass()))

	vkCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k := mrb.GetArgsFirst().Bytes()
		if len(k) != ed25519.PublicKeySize {
			return mrb.Raise(lenErr, "VerifyKey must be 32 bytes")
		}
		rb.Set(mrb, self, &verifyKeyData{pub: append(ed25519.PublicKey(nil), k...)})
		return self
	}, mrb.ArgsReq(1))
	vkCls.DefineMethod("verify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*verifyKeyData](mrb, self)
		a := mrb.GetArgs().Slice()
		sig, msg := a[0].Bytes(), a[1].Bytes()
		if len(sig) != ed25519.SignatureSize || !ed25519.Verify(d.pub, msg, sig) {
			return mrb.Raise(badSigErr, "signature was forged/corrupt")
		}
		return mrb.TrueValue()
	}, mrb.ArgsReq(2))
	vkCls.DefineMethod("to_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*verifyKeyData](mrb, self)
		return binStr(mrb, d.pub)
	}, mrb.ArgsNone())
	vkCls.DefineAlias("to_s", "to_bytes")

	skCls.DefineClassMethod("generate", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return mrb.Raise(cryptoErr, err.Error())
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, skCls)
		rb.Set(mrb, obj.Value(), &signKeyData{priv: priv})
		return obj
	}, mrb.ArgsNone())

	skCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		seed := mrb.GetArgsFirst().Bytes()
		if len(seed) != ed25519.SeedSize {
			return mrb.Raise(lenErr, "SigningKey seed must be 32 bytes")
		}
		rb.Set(mrb, self, &signKeyData{priv: ed25519.NewKeyFromSeed(seed)})
		return self
	}, mrb.ArgsReq(1))
	skCls.DefineMethod("sign", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*signKeyData](mrb, self)
		return binStr(mrb, ed25519.Sign(d.priv, mrb.GetArgsFirst().Bytes()))
	}, mrb.ArgsReq(1))
	skCls.DefineMethod("verify_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*signKeyData](mrb, self)
		obj := mrb.ObjAlloc(oruby.MrbTTCData, vkCls)
		rb.Set(mrb, obj.Value(), &verifyKeyData{pub: d.priv.Public().(ed25519.PublicKey)})
		return obj
	}, mrb.ArgsNone())
	skCls.DefineMethod("to_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := rb.Get[*signKeyData](mrb, self)
		return binStr(mrb, d.priv.Seed())
	}, mrb.ArgsNone())
	skCls.DefineAlias("to_s", "to_bytes")

	// RbNaCl::AEAD::{ChaCha20Poly1305IETF, XChaCha20Poly1305IETF}
	aeadMod := mrb.DefineModuleUnder(mod, "AEAD")
	defineAEAD := func(name string, xmode bool) {
		nonceLen := chacha20poly1305.NonceSize
		if xmode {
			nonceLen = chacha20poly1305.NonceSizeX
		}
		cls := rb.DataClass(mrb.DefineClassUnder(aeadMod, name, mrb.ObjectClass()))
		cls.DefineClassMethod("key_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(32) }, mrb.ArgsNone())
		cls.DefineClassMethod("nonce_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(nonceLen) }, mrb.ArgsNone())
		cls.DefineMethod("key_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(32) }, mrb.ArgsNone())
		cls.DefineMethod("nonce_bytes", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue { return mrb.FixnumValue(nonceLen) }, mrb.ArgsNone())

		cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			k := mrb.GetArgsFirst().Bytes()
			if len(k) != 32 {
				return mrb.Raise(lenErr, "AEAD key must be 32 bytes")
			}
			rb.Set(mrb, self, &aeadData{key: append([]byte(nil), k...), xmode: xmode})
			return self
		}, mrb.ArgsReq(1))

		cls.DefineMethod("encrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d, _ := rb.Get[*aeadData](mrb, self)
			a := mrb.GetArgs().Slice()
			nonce := a[0].Bytes()
			if len(nonce) != nonceLen {
				return mrb.Raise(lenErr, fmt.Sprintf("AEAD nonce must be %d bytes", nonceLen))
			}
			msg := a[1].Bytes()
			var ad []byte
			if len(a) >= 3 && !a[2].IsNil() {
				ad = a[2].Bytes()
			}
			var aeadImpl interface {
				Seal(dst, nonce, plaintext, additionalData []byte) []byte
			}
			var err error
			if d.xmode {
				aeadImpl, err = chacha20poly1305.NewX(d.key)
			} else {
				aeadImpl, err = chacha20poly1305.New(d.key)
			}
			if err != nil {
				return mrb.Raise(cryptoErr, err.Error())
			}
			return binStr(mrb, aeadImpl.Seal(nil, nonce, msg, ad))
		}, mrb.ArgsArg(2, 1))

		cls.DefineMethod("decrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d, _ := rb.Get[*aeadData](mrb, self)
			a := mrb.GetArgs().Slice()
			nonce := a[0].Bytes()
			if len(nonce) != nonceLen {
				return mrb.Raise(lenErr, fmt.Sprintf("AEAD nonce must be %d bytes", nonceLen))
			}
			ct := a[1].Bytes()
			var ad []byte
			if len(a) >= 3 && !a[2].IsNil() {
				ad = a[2].Bytes()
			}
			var plain []byte
			var err error
			if d.xmode {
				c, cerr := chacha20poly1305.NewX(d.key)
				if cerr != nil {
					return mrb.Raise(cryptoErr, cerr.Error())
				}
				plain, err = c.Open(nil, nonce, ct, ad)
			} else {
				c, cerr := chacha20poly1305.New(d.key)
				if cerr != nil {
					return mrb.Raise(cryptoErr, cerr.Error())
				}
				plain, err = c.Open(nil, nonce, ct, ad)
			}
			if err != nil {
				return mrb.Raise(cryptoErr, "Decryption failed. Ciphertext failed verification")
			}
			return binStr(mrb, plain)
		}, mrb.ArgsArg(2, 1))
	}
	defineAEAD("ChaCha20Poly1305IETF", false)
	defineAEAD("XChaCha20Poly1305IETF", true)

	// RbNaCl::SimpleBox (Ruby convenience wrapper)
	rb.LoadRuby(mrb, "<internal:rbnacl>", []byte(`
module RbNaCl
  class SimpleBox
    def self.from_secret_key(key)
      new(SecretBox.new(key))
    end

    def self.from_keypair(public_key, private_key)
      new(Box.new(public_key, private_key))
    end

    def initialize(box)
      @box = box
    end

    def encrypt(message)
      nonce = Random.random_bytes(@box.nonce_bytes)
      nonce + @box.encrypt(nonce, message)
    end
    alias box encrypt

    def decrypt(ciphertext)
      nb = @box.nonce_bytes
      nonce = ciphertext.byteslice(0, nb)
      ct = ciphertext.byteslice(nb, ciphertext.bytesize - nb)
      @box.decrypt(nonce, ct)
    end
    alias open decrypt
  end
end
`))

	return nil
}
