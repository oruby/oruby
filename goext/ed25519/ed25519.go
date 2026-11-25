// Package ed25519 implements Ruby's ed25519 gem (and its ed25519_ref10 native
// extension) in pure Go using Go's standard library crypto/ed25519.
package ed25519

import (
	goed25519 "crypto/ed25519"
	"crypto/rand"
	_ "embed"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed ed25519.rb
var ed25519Rb []byte

func init() {
	oruby.LazyGem("ed25519_ref10", initRef10)
	oruby.LazyGem("ed25519", func(mrb *oruby.MrbState) any {
		initRef10(mrb)
		rb.LoadRuby(mrb, "ed25519.rb", ed25519Rb)
		return nil
	})
}

func initRef10(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Ed25519")
	prov := mrb.DefineModuleUnder(mod, "Provider")
	ref10 := mrb.DefineModuleUnder(prov, "Ref10")

	ref10.DefineClassMethod("create_keypair", createKeypair, mrb.ArgsReq(1))
	ref10.DefineClassMethod("sign", signMessage, mrb.ArgsReq(2))
	ref10.DefineClassMethod("verify", verifyMessage, mrb.ArgsReq(3))
	ref10.DefineClassMethod("__random_seed", randomSeed, mrb.ArgsNone())
	return nil
}

func randomSeed(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	buf := make([]byte, goed25519.SeedSize)
	if _, err := rand.Read(buf); err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}
	return mrb.Call(mrb.BytesValue(buf), "b")
}

func createKeypair(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	seedVal := mrb.GetArgsFirst()
	if seedVal.Type() != oruby.MrbTTString {
		return mrb.ETypeError().Raisef("expected String, got %v", mrb.ClassOf(seedVal).Name())
	}
	seed := seedVal.Bytes()
	if len(seed) != goed25519.SeedSize {
		return mrb.EArgumentError().Raisef("expected %d-byte String, got %d", goed25519.SeedSize, len(seed))
	}
	priv := goed25519.NewKeyFromSeed(seed)
	return mrb.Call(mrb.BytesValue([]byte(priv)), "b")
}

func signMessage(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	kpVal := args.Item(0)
	msgVal := args.Item(1)
	if kpVal.Type() != oruby.MrbTTString {
		return mrb.ETypeError().Raisef("expected String, got %v", mrb.ClassOf(kpVal).Name())
	}
	if msgVal.Type() != oruby.MrbTTString {
		return mrb.ETypeError().Raisef("expected String, got %v", mrb.ClassOf(msgVal).Name())
	}
	kp := kpVal.Bytes()
	if len(kp) != goed25519.PrivateKeySize {
		return mrb.EArgumentError().Raisef("expected %d-byte keypair, got %d", goed25519.PrivateKeySize, len(kp))
	}
	sig := goed25519.Sign(goed25519.PrivateKey(kp), msgVal.Bytes())
	return mrb.Call(mrb.BytesValue(sig), "b")
}

func verifyMessage(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	pubVal := args.Item(0)
	sigVal := args.Item(1)
	msgVal := args.Item(2)
	if pubVal.Type() != oruby.MrbTTString {
		return mrb.ETypeError().Raisef("expected String, got %v", mrb.ClassOf(pubVal).Name())
	}
	if sigVal.Type() != oruby.MrbTTString {
		return mrb.ETypeError().Raisef("expected String, got %v", mrb.ClassOf(sigVal).Name())
	}
	if msgVal.Type() != oruby.MrbTTString {
		return mrb.ETypeError().Raisef("expected String, got %v", mrb.ClassOf(msgVal).Name())
	}
	pub := pubVal.Bytes()
	sig := sigVal.Bytes()
	if len(pub) != goed25519.PublicKeySize {
		return mrb.EArgumentError().Raisef("expected %d-byte public key, got %d", goed25519.PublicKeySize, len(pub))
	}
	if len(sig) != goed25519.SignatureSize {
		return mrb.EArgumentError().Raisef("expected %d-byte signature, got %d", goed25519.SignatureSize, len(sig))
	}
	return mrb.BoolValue(goed25519.Verify(goed25519.PublicKey(pub), msgVal.Bytes(), sig))
}
