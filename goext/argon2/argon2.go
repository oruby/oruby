// Package argon2 provides a pure-Go implementation of the Ruby argon2 gem
// (Argon2id and Argon2i password hashing) backed by golang.org/x/crypto/argon2.
package argon2

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	"golang.org/x/crypto/argon2"
)

func init() {
	oruby.LazyGem("argon2", initArgon2)
}

var rawB64 = base64.RawStdEncoding

func encodePHC(variant string, tCost, memKiB uint32, pCost uint8, salt, key []byte) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		variant, argon2.Version, memKiB, tCost, pCost,
		rawB64.EncodeToString(salt), rawB64.EncodeToString(key))
}

func deriveArgon2(variant string, pass, salt []byte, tCost, memKiB uint32, pCost uint8, keyLen uint32) []byte {
	if variant == "argon2i" {
		return argon2.Key(pass, salt, tCost, memKiB, pCost, keyLen)
	}
	return argon2.IDKey(pass, salt, tCost, memKiB, pCost, keyLen)
}

func verifyPHC(pass []byte, encoded string, secret []byte) (bool, error) {
	parts := strings.Split(encoded, "$")
	// Expected: ["", "argon2id", "v=19", "m=...,t=...,p=...", saltB64, hashB64]
	if len(parts) != 6 || parts[0] != "" {
		return false, fmt.Errorf("invalid argon2 hash format")
	}
	variant := parts[1]
	if variant != "argon2id" && variant != "argon2i" {
		return false, fmt.Errorf("unsupported argon2 variant: %s", variant)
	}
	var memKiB, tCost uint32
	var pCost uint8
	for _, kv := range strings.Split(parts[3], ",") {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			return false, fmt.Errorf("invalid argon2 param: %s", kv)
		}
		k, vs := kv[:eq], kv[eq+1:]
		n, err := strconv.ParseUint(vs, 10, 32)
		if err != nil {
			return false, err
		}
		switch k {
		case "m":
			memKiB = uint32(n)
		case "t":
			tCost = uint32(n)
		case "p":
			pCost = uint8(n)
		}
	}
	if memKiB == 0 || tCost == 0 || pCost == 0 {
		return false, fmt.Errorf("incomplete argon2 parameters")
	}
	salt, err := rawB64.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := rawB64.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	input := pass
	if len(secret) > 0 {
		input = append(append([]byte(nil), pass...), secret...)
	}
	got := deriveArgon2(variant, input, salt, tCost, memKiB, pCost, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type pwHasher struct {
	tCost  uint32
	mCost  uint32 // power of 2 in KiB (default 16 -> 65536 KiB)
	pCost  uint8
	secret []byte
}

func parseArgonOpts(mrb *oruby.MrbState, base pwHasher, h oruby.Value) pwHasher {
	if h.IsNil() || !h.IsHash() {
		return base
	}
	get := func(k string) (oruby.Value, bool) {
		if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern(k).Value()); err == nil && !v.IsNil() {
			return v, true
		}
		return mrb.NilValue(), false
	}
	if v, ok := get("profile"); ok {
		switch mrb.String(v) {
		case "rfc_9106_low_memory":
			base.tCost, base.mCost, base.pCost = 3, 16, 4
		case "rfc_9106_high_memory":
			base.tCost, base.mCost, base.pCost = 1, 21, 4
		case "unsafe_cheapest":
			base.tCost, base.mCost, base.pCost = 1, 3, 1
		}
	}
	if v, ok := get("t_cost"); ok && v.IsInt() {
		base.tCost = uint32(v.Int64())
	}
	if v, ok := get("m_cost"); ok && v.IsInt() {
		base.mCost = uint32(v.Int64())
	}
	if v, ok := get("p_cost"); ok && v.IsInt() {
		base.pCost = uint8(v.Int64())
	}
	if v, ok := get("secret"); ok && v.IsString() {
		base.secret = v.Bytes()
	}
	return base
}

func initArgon2(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Argon2")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("2.3.0"))
	errCls := mrb.DefineClassUnder(mod, "ArgonHashFail", mrb.ExcGet("StandardError"))

	hashWith := func(mrb *oruby.MrbState, pass []byte, cfg pwHasher) oruby.MrbValue {
		if cfg.tCost < 1 || cfg.mCost < 3 || cfg.mCost > 24 || cfg.pCost < 1 {
			return mrb.Raise(errCls, "invalid argon2 cost parameters")
		}
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		input := pass
		if len(cfg.secret) > 0 {
			input = append(append([]byte(nil), pass...), cfg.secret...)
		}
		memKiB := uint32(1) << cfg.mCost
		var key []byte
		mrb.Blocking(func() {
			key = argon2.IDKey(input, salt, cfg.tCost, memKiB, cfg.pCost, 32)
		})
		return mrb.StrNew(encodePHC("argon2id", cfg.tCost, memKiB, cfg.pCost, salt, key))
	}

	// Argon2::Password
	pwCls := rb.DataClass(mrb.DefineClassUnder(mod, "Password", mrb.ObjectClass()))
	pwCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		cfg := pwHasher{tCost: 2, mCost: 16, pCost: 1}
		if args := mrb.GetArgs(); args.Len() >= 1 && args.Item(0).IsHash() {
			cfg = parseArgonOpts(mrb, cfg, args.Item(0))
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			cfg = parseArgonOpts(mrb, cfg, kw)
		}
		rb.Set(mrb, self, &cfg)
		return self
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	pwCls.DefineMethod("create", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		cfg, ok := rb.Get[*pwHasher](mrb, self)
		if !ok {
			return mrb.Raise(errCls, "Password not initialized")
		}
		pass, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		return hashWith(mrb, pass, *cfg)
	}, mrb.ArgsReq(1))

	pwCls.DefineClassMethod("create", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		pass, err := rb.Bytes(mrb, args.Item(0))
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		cfg := pwHasher{tCost: 2, mCost: 16, pCost: 1}
		if args.Len() >= 2 && args.Item(1).IsHash() {
			cfg = parseArgonOpts(mrb, cfg, args.Item(1))
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			cfg = parseArgonOpts(mrb, cfg, kw)
		}
		return hashWith(mrb, pass, cfg)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	pwCls.DefineClassMethod("verify_password", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		pass, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		hashStr := args[1].String()
		var secret []byte
		if len(args) >= 3 && !args[2].IsNil() {
			secret = args[2].Bytes()
		}
		var ok bool
		var verr error
		mrb.Blocking(func() {
			ok, verr = verifyPHC(pass, hashStr, secret)
		})
		if verr != nil {
			return mrb.Raise(errCls, verr.Error())
		}
		return mrb.BoolValue(ok)
	}, mrb.ArgsArg(2, 1))

	// Argon2::Engine
	engMod := mrb.DefineModuleUnder(mod, "Engine")
	engMod.DefineClassMethod("hash_argon2id_encode", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		pass := a[0].Bytes()
		salt := a[1].Bytes()
		tCost := uint32(a[2].Int64())
		mCost := uint32(a[3].Int64())
		pCost := uint8(a[4].Int64())
		var secret []byte
		if len(a) >= 6 && !a[5].IsNil() {
			secret = a[5].Bytes()
		}
		input := pass
		if len(secret) > 0 {
			input = append(append([]byte(nil), pass...), secret...)
		}
		memKiB := uint32(1) << mCost
		key := argon2.IDKey(input, salt, tCost, memKiB, pCost, 32)
		return mrb.StrNew(encodePHC("argon2id", tCost, memKiB, pCost, salt, key))
	}, mrb.ArgsArg(5, 1))

	engMod.DefineClassMethod("hash_argon2i_encode", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		pass := a[0].Bytes()
		salt := a[1].Bytes()
		tCost := uint32(a[2].Int64())
		mCost := uint32(a[3].Int64())
		pCost := uint8(a[4].Int64())
		memKiB := uint32(1) << mCost
		key := argon2.Key(pass, salt, tCost, memKiB, pCost, 32)
		return mrb.StrNew(encodePHC("argon2i", tCost, memKiB, pCost, salt, key))
	}, mrb.ArgsArg(5, 1))

	engMod.DefineClassMethod("argon2_verify", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		pass := a[0].Bytes()
		hashStr := a[1].String()
		var secret []byte
		if len(a) >= 3 && !a[2].IsNil() {
			secret = a[2].Bytes()
		}
		ok, err := verifyPHC(pass, hashStr, secret)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.BoolValue(ok)
	}, mrb.ArgsArg(2, 1))

	return nil
}
