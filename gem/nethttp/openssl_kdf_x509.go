package nethttp

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/scrypt"
)

type rawPKey struct {
	alg      string // "ED25519" or "X25519"
	edPriv   ed25519.PrivateKey
	edPub    ed25519.PublicKey
	ecdhPriv *ecdh.PrivateKey
	ecdhPub  *ecdh.PublicKey
}

type x509ReqData struct {
	version int
	subject *x509Name
	pubKey  any
	rawDER  []byte
	parsed  *x509.CertificateRequest
}

type x509RevokedData struct {
	serial *big.Int
	t      time.Time
}

type x509CRLData struct {
	version    int
	issuer     *x509Name
	lastUpdate time.Time
	nextUpdate time.Time
	revoked    []*x509RevokedData
	rawDER     []byte
	parsed     *x509.RevocationList
}

func extractAnySigner(mrb *oruby.MrbState, v oruby.Value) (crypto.Signer, error) {
	if rk, ok := rb.Get[*rsaKey](mrb, v); ok && rk.priv != nil {
		return rk.priv, nil
	}
	if ek, ok := rb.Get[*ecKey](mrb, v); ok && ek.priv != nil {
		return ek.priv, nil
	}
	if pk, ok := rb.Get[*rawPKey](mrb, v); ok && pk.edPriv != nil {
		return pk.edPriv, nil
	}
	return nil, fmt.Errorf("expected private signing key")
}

func extractAnyPublicKey(mrb *oruby.MrbState, v oruby.Value) (any, error) {
	if rk, ok := rb.Get[*rsaKey](mrb, v); ok {
		if rk.pub != nil {
			return rk.pub, nil
		}
		if rk.priv != nil {
			return &rk.priv.PublicKey, nil
		}
	}
	if ek, ok := rb.Get[*ecKey](mrb, v); ok {
		if ek.pub != nil {
			return ek.pub, nil
		}
		if ek.priv != nil {
			return &ek.priv.PublicKey, nil
		}
	}
	if pk, ok := rb.Get[*rawPKey](mrb, v); ok {
		if pk.edPub != nil {
			return pk.edPub, nil
		}
		if pk.ecdhPub != nil {
			return pk.ecdhPub, nil
		}
	}
	return nil, fmt.Errorf("expected public key")
}

func wrapAnyPublicKey(mrb *oruby.MrbState, pub any) oruby.Value {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::RSA")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &rsaKey{pub: k})
		return obj.Value()
	case *ecdsa.PublicKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::EC")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &ecKey{pub: k, curve: k.Curve, curveName: canonicalCurveName(k.Curve)})
		return obj.Value()
	case ed25519.PublicKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::PKey")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &rawPKey{alg: "ED25519", edPub: k})
		return obj.Value()
	case *ecdh.PublicKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::PKey")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &rawPKey{alg: "X25519", ecdhPub: k})
		return obj.Value()
	}
	return mrb.NilValue()
}

func kwHashVal(mrb *oruby.MrbState, h oruby.Value, key string) (oruby.Value, bool) {
	if h.IsNil() || !h.IsHash() {
		return mrb.NilValue(), false
	}
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern(key).Value()); err == nil && !v.IsNil() {
		return v, true
	}
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.StrNew(key)); err == nil && !v.IsNil() {
		return v, true
	}
	return mrb.NilValue(), false
}

func x509SigAlgFor(signer crypto.Signer, digestVal oruby.Value) x509.SignatureAlgorithm {
	if _, isEd := signer.(ed25519.PrivateKey); isEd {
		return x509.PureEd25519
	}
	name := "SHA256"
	if !digestVal.IsNil() {
		if digestVal.IsString() {
			name = strings.ToUpper(digestVal.String())
		} else if n, err := mrbDigestName(digestVal); err == nil && n != "" {
			name = strings.ToUpper(n)
		}
	}
	_, isEC := signer.(*ecdsa.PrivateKey)
	switch strings.ReplaceAll(name, "-", "") {
	case "SHA1":
		if isEC {
			return x509.ECDSAWithSHA1
		}
		return x509.SHA1WithRSA
	case "SHA384":
		if isEC {
			return x509.ECDSAWithSHA384
		}
		return x509.SHA384WithRSA
	case "SHA512":
		if isEC {
			return x509.ECDSAWithSHA512
		}
		return x509.SHA512WithRSA
	default:
		if isEC {
			return x509.ECDSAWithSHA256
		}
		return x509.SHA256WithRSA
	}
}

func mrbDigestName(v oruby.Value) (string, error) {
	if v.IsString() {
		return v.String(), nil
	}
	return "", nil
}

func initOpenSSLKDFX509PKey(mrb *oruby.MrbState, mod, osslErr, kdfMod, kdfErr, pkeyMod, pkeyErr, pkeyBase, rsaCls, x509Mod, nameCls oruby.RClass) {
	// -------------------------------------------------------------------------
	// OpenSSL::KDF.hkdf & OpenSSL::KDF.scrypt
	// -------------------------------------------------------------------------
	kdfMod.DefineClassMethod("hkdf", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		ikm, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		opts := mrb.KeywordArgs().Value()
		if opts.IsNil() && len(args) >= 2 && args[1].IsHash() {
			opts = args[1]
		}
		var salt, info []byte
		if v, ok := kwHashVal(mrb, opts, "salt"); ok {
			salt, _ = rb.Bytes(mrb, v)
		}
		if v, ok := kwHashVal(mrb, opts, "info"); ok {
			info, _ = rb.Bytes(mrb, v)
		}
		length := 0
		if v, ok := kwHashVal(mrb, opts, "length"); ok {
			length = v.Int()
		}
		if length <= 0 {
			return mrb.Raise(kdfErr, "length must be positive")
		}
		hashVal, _ := kwHashVal(mrb, opts, "hash")
		h, err := cryptoHashFromRuby(mrb, hashVal)
		if err != nil {
			return mrb.Raise(kdfErr, err.Error())
		}
		r := hkdf.New(h.New, ikm, salt, info)
		out := make([]byte, length)
		if _, err := io.ReadFull(r, out); err != nil {
			return mrb.Raise(kdfErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1))

	kdfMod.DefineClassMethod("scrypt", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		pass, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		opts := mrb.KeywordArgs().Value()
		if opts.IsNil() && len(args) >= 2 && args[1].IsHash() {
			opts = args[1]
		}
		var salt []byte
		if v, ok := kwHashVal(mrb, opts, "salt"); ok {
			salt, _ = rb.Bytes(mrb, v)
		}
		nVal, rVal, pVal, keyLen := 16384, 8, 1, 32
		if v, ok := kwHashVal(mrb, opts, "N"); ok {
			nVal = v.Int()
		}
		if v, ok := kwHashVal(mrb, opts, "r"); ok {
			rVal = v.Int()
		}
		if v, ok := kwHashVal(mrb, opts, "p"); ok {
			pVal = v.Int()
		}
		if v, ok := kwHashVal(mrb, opts, "length"); ok {
			keyLen = v.Int()
		}
		out, err := scrypt.Key(pass, salt, nVal, rVal, pVal, keyLen)
		if err != nil {
			return mrb.Raise(kdfErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1))

	// -------------------------------------------------------------------------
	// OpenSSL::PKey.generate_key / new_raw_private_key / new_raw_public_key
	// & Ed25519 / X25519 methods on OpenSSL::PKey::PKey
	// -------------------------------------------------------------------------
	pkeyMod.DefineClassMethod("generate_key", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		alg := strings.ToUpper(args[0].String())
		switch alg {
		case "ED25519":
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
			rb.Set(mrb, obj.Value(), &rawPKey{alg: "ED25519", edPriv: priv, edPub: pub})
			return obj
		case "X25519":
			priv, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
			rb.Set(mrb, obj.Value(), &rawPKey{alg: "X25519", ecdhPriv: priv, ecdhPub: priv.PublicKey()})
			return obj
		case "RSA":
			bits := 2048
			if len(args) >= 2 && args[1].IsHash() {
				if v, ok := kwHashVal(mrb, args[1], "rsa_keygen_bits"); ok {
					bits = v.Int()
				}
			}
			priv, err := rsa.GenerateKey(rand.Reader, bits)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			obj := mrb.ObjAlloc(oruby.MrbTTCData, rsaCls)
			rb.Set(mrb, obj.Value(), &rsaKey{priv: priv, pub: &priv.PublicKey})
			return obj
		case "EC":
			curve := elliptic.P256()
			if len(args) >= 2 && args[1].IsHash() {
				if v, ok := kwHashVal(mrb, args[1], "ec_paramgen_curve"); ok {
					if c, _, err := lookupCurve(v.String()); err == nil {
						curve = c
					}
				}
			}
			priv, err := ecdsa.GenerateKey(curve, rand.Reader)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			ecCls := rb.ClassPath(mrb, "OpenSSL::PKey::EC")
			obj := mrb.ObjAlloc(oruby.MrbTTCData, ecCls)
			rb.Set(mrb, obj.Value(), &ecKey{priv: priv, pub: &priv.PublicKey, curve: curve, curveName: canonicalCurveName(curve)})
			return obj
		default:
			return mrb.Raise(pkeyErr, fmt.Sprintf("unsupported key algorithm: %s", alg))
		}
	}, mrb.ArgsArg(1, 1))

	pkeyMod.DefineClassMethod("new_raw_private_key", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		alg := strings.ToUpper(args[0].String())
		raw, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		switch alg {
		case "ED25519":
			if len(raw) != ed25519.SeedSize {
				return mrb.Raise(pkeyErr, "invalid ED25519 private key length")
			}
			priv := ed25519.NewKeyFromSeed(raw)
			obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
			rb.Set(mrb, obj.Value(), &rawPKey{alg: "ED25519", edPriv: priv, edPub: priv.Public().(ed25519.PublicKey)})
			return obj
		case "X25519":
			priv, err := ecdh.X25519().NewPrivateKey(raw)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
			rb.Set(mrb, obj.Value(), &rawPKey{alg: "X25519", ecdhPriv: priv, ecdhPub: priv.PublicKey()})
			return obj
		default:
			return mrb.Raise(pkeyErr, fmt.Sprintf("unsupported raw key algorithm: %s", alg))
		}
	}, mrb.ArgsReq(2))

	pkeyMod.DefineClassMethod("new_raw_public_key", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		alg := strings.ToUpper(args[0].String())
		raw, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		switch alg {
		case "ED25519":
			if len(raw) != ed25519.PublicKeySize {
				return mrb.Raise(pkeyErr, "invalid ED25519 public key length")
			}
			pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
			copy(pub, raw)
			obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
			rb.Set(mrb, obj.Value(), &rawPKey{alg: "ED25519", edPub: pub})
			return obj
		case "X25519":
			pub, err := ecdh.X25519().NewPublicKey(raw)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
			rb.Set(mrb, obj.Value(), &rawPKey{alg: "X25519", ecdhPub: pub})
			return obj
		default:
			return mrb.Raise(pkeyErr, fmt.Sprintf("unsupported raw key algorithm: %s", alg))
		}
	}, mrb.ArgsReq(2))

	pkeyBase.DefineMethod("oid", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if pk, ok := rb.Get[*rawPKey](mrb, self); ok {
			return mrb.StrNew(pk.alg)
		}
		return mrb.StrNew("PKey")
	}, mrb.ArgsNone())

	pkeyBase.DefineMethod("private?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if pk, ok := rb.Get[*rawPKey](mrb, self); ok {
			return mrb.BoolValue(pk.edPriv != nil || pk.ecdhPriv != nil)
		}
		return mrb.FalseValue()
	}, mrb.ArgsNone())

	pkeyBase.DefineMethod("public?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if pk, ok := rb.Get[*rawPKey](mrb, self); ok {
			return mrb.BoolValue(pk.edPub != nil || pk.ecdhPub != nil)
		}
		return mrb.FalseValue()
	}, mrb.ArgsNone())

	pkeyBase.DefineMethod("raw_private_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "not a raw key")
		}
		if pk.edPriv != nil {
			return mrb.Call(mrb.BytesValue(pk.edPriv.Seed()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		if pk.ecdhPriv != nil {
			return mrb.Call(mrb.BytesValue(pk.ecdhPriv.Bytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		return mrb.Raise(pkeyErr, "private key not present")
	}, mrb.ArgsNone())

	pkeyBase.DefineMethod("raw_public_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "not a raw key")
		}
		if pk.edPub != nil {
			return mrb.Call(mrb.BytesValue([]byte(pk.edPub)), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		if pk.ecdhPub != nil {
			return mrb.Call(mrb.BytesValue(pk.ecdhPub.Bytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		return mrb.Raise(pkeyErr, "public key not present")
	}, mrb.ArgsNone())

	signRawFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok || pk.edPriv == nil {
			return mrb.Raise(pkeyErr, "ED25519 private key required")
		}
		args := mrb.GetArgs().Slice()
		data, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		sig := ed25519.Sign(pk.edPriv, data)
		return mrb.Call(mrb.BytesValue(sig), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}
	pkeyBase.DefineMethod("sign", signRawFn, mrb.ArgsReq(2))
	pkeyBase.DefineMethod("sign_raw", signRawFn, mrb.ArgsReq(2))

	verifyRawFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok || pk.edPub == nil {
			return mrb.Raise(pkeyErr, "ED25519 public key required")
		}
		args := mrb.GetArgs().Slice()
		sig, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		data, err := rb.Bytes(mrb, args[2])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		return mrb.BoolValue(ed25519.Verify(pk.edPub, data, sig))
	}
	pkeyBase.DefineMethod("verify", verifyRawFn, mrb.ArgsReq(3))
	pkeyBase.DefineMethod("verify_raw", verifyRawFn, mrb.ArgsReq(3))

	pkeyBase.DefineMethod("derive", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok || pk.ecdhPriv == nil {
			return mrb.Raise(pkeyErr, "X25519 private key required for derive")
		}
		peer, ok := rb.Get[*rawPKey](mrb, mrb.GetArgsFirst())
		if !ok || peer.ecdhPub == nil {
			return mrb.Raise(pkeyErr, "X25519 peer public key required")
		}
		secret, err := pk.ecdhPriv.ECDH(peer.ecdhPub)
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(secret), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	pkeyBase.DefineMethod("public_to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "key not initialized")
		}
		var pub any
		if pk.edPub != nil {
			pub = pk.edPub
		} else {
			pub = pk.ecdhPub
		}
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	pkeyBase.DefineMethod("public_to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "key not initialized")
		}
		var pub any
		if pk.edPub != nil {
			pub = pk.edPub
		} else {
			pub = pk.ecdhPub
		}
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	}, mrb.ArgsNone())

	pkeyBase.DefineMethod("to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "key not initialized")
		}
		if pk.edPriv != nil || pk.ecdhPriv != nil {
			var priv any
			if pk.edPriv != nil {
				priv = pk.edPriv
			} else {
				priv = pk.ecdhPriv
			}
			der, err := x509.MarshalPKCS8PrivateKey(priv)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			return mrb.Call(mrb.BytesValue(der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		var pub any
		if pk.edPub != nil {
			pub = pk.edPub
		} else {
			pub = pk.ecdhPub
		}
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	pkeyBase.DefineMethod("to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		pk, ok := rb.Get[*rawPKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "key not initialized")
		}
		if pk.edPriv != nil || pk.ecdhPriv != nil {
			var priv any
			if pk.edPriv != nil {
				priv = pk.edPriv
			} else {
				priv = pk.ecdhPriv
			}
			der, err := x509.MarshalPKCS8PrivateKey(priv)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})))
		}
		var pub any
		if pk.edPub != nil {
			pub = pk.edPub
		} else {
			pub = pk.ecdhPub
		}
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	}, mrb.ArgsNone())
	pkeyBase.DefineAlias("to_s", "to_pem")
	pkeyBase.DefineAlias("export", "to_pem")

	// -------------------------------------------------------------------------
	// OpenSSL::X509::Request (CSR)
	// -------------------------------------------------------------------------
	reqErrCls := mrb.DefineClassUnder(x509Mod, "RequestError", osslErr)
	reqCls := rb.DataClass(mrb.DefineClassUnder(x509Mod, "Request", mrb.ObjectClass()))

	reqCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if len(args) == 0 || args[0].IsNil() {
			rb.Set(mrb, self, &x509ReqData{version: 0, subject: &x509Name{}})
			return self
		}
		raw, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		der := raw
		if block, _ := pem.Decode(raw); block != nil {
			der = block.Bytes
		}
		parsed, err := x509.ParseCertificateRequest(der)
		if err != nil {
			return mrb.Raise(reqErrCls, err.Error())
		}
		rb.Set(mrb, self, &x509ReqData{
			version: parsed.Version,
			subject: x509NameFromPKIX(parsed.Subject),
			pubKey:  parsed.PublicKey,
			rawDER:  parsed.Raw,
			parsed:  parsed,
		})
		return self
	}, mrb.ArgsOpt(1))

	reqCls.DefineMethod("version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		return mrb.FixnumValue(r.version)
	}, mrb.ArgsNone())

	reqCls.DefineMethod("version=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		v := mrb.GetArgsFirst()
		r.version = v.Int()
		return v
	}, mrb.ArgsReq(1))

	reqCls.DefineMethod("subject", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		if r.subject == nil {
			return mrb.NilValue()
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, nameCls)
		rb.Set(mrb, obj.Value(), r.subject)
		return obj
	}, mrb.ArgsNone())

	reqCls.DefineMethod("subject=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		v := mrb.GetArgsFirst()
		if n, ok := rb.Get[*x509Name](mrb, v); ok {
			r.subject = n
		}
		return v
	}, mrb.ArgsReq(1))

	reqCls.DefineMethod("public_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		return wrapAnyPublicKey(mrb, r.pubKey)
	}, mrb.ArgsNone())

	reqCls.DefineMethod("public_key=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		v := mrb.GetArgsFirst()
		pub, err := extractAnyPublicKey(mrb, v)
		if err != nil {
			return mrb.Raise(reqErrCls, err.Error())
		}
		r.pubKey = pub
		return v
	}, mrb.ArgsReq(1))

	reqCls.DefineMethod("sign", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		args := mrb.GetArgs().Slice()
		signer, err := extractAnySigner(mrb, args[0])
		if err != nil {
			return mrb.Raise(reqErrCls, err.Error())
		}
		var digVal oruby.Value = mrb.NilValue()
		if len(args) >= 2 {
			digVal = args[1]
		}
		tmpl := &x509.CertificateRequest{
			SignatureAlgorithm: x509SigAlgFor(signer, digVal),
		}
		if r.subject != nil {
			tmpl.Subject = r.subject.toPKIX()
		}
		der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, signer)
		if err != nil {
			return mrb.Raise(reqErrCls, err.Error())
		}
		parsed, err := x509.ParseCertificateRequest(der)
		if err != nil {
			return mrb.Raise(reqErrCls, err.Error())
		}
		r.rawDER = der
		r.parsed = parsed
		r.pubKey = parsed.PublicKey
		return self
	}, mrb.ArgsArg(1, 1))

	reqCls.DefineMethod("verify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		if r.parsed == nil {
			return mrb.FalseValue()
		}
		if err := r.parsed.CheckSignature(); err != nil {
			return mrb.FalseValue()
		}
		return mrb.TrueValue()
	}, mrb.ArgsReq(1))

	reqCls.DefineMethod("to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		return mrb.Call(mrb.BytesValue(r.rawDER), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	reqCls.DefineMethod("to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: r.rawDER})))
	}, mrb.ArgsNone())
	reqCls.DefineAlias("to_s", "to_pem")

	reqCls.DefineMethod("signature_algorithm", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*x509ReqData](mrb, self)
		if r.parsed == nil {
			return mrb.StrNew("")
		}
		return mrb.StrNew(r.parsed.SignatureAlgorithm.String())
	}, mrb.ArgsNone())

	// -------------------------------------------------------------------------
	// OpenSSL::X509::Revoked & OpenSSL::X509::CRL
	// -------------------------------------------------------------------------
	crlErrCls := mrb.DefineClassUnder(x509Mod, "CRLError", osslErr)
	revCls := rb.DataClass(mrb.DefineClassUnder(x509Mod, "Revoked", mrb.ObjectClass()))
	crlCls := rb.DataClass(mrb.DefineClassUnder(x509Mod, "CRL", mrb.ObjectClass()))

	revCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, &x509RevokedData{serial: big.NewInt(0), t: time.Now().UTC()})
		return self
	}, mrb.ArgsNone())

	revCls.DefineMethod("serial", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rv, _ := rb.Get[*x509RevokedData](mrb, self)
		bnCls := rb.ClassPath(mrb, "OpenSSL::BN")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, bnCls)
		rb.Set(mrb, obj.Value(), &osslBN{n: new(big.Int).Set(rv.serial)})
		return obj
	}, mrb.ArgsNone())

	revCls.DefineMethod("serial=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rv, _ := rb.Get[*x509RevokedData](mrb, self)
		v := mrb.GetArgsFirst()
		if bn, ok := rb.Get[*osslBN](mrb, v); ok && bn.n != nil {
			rv.serial = new(big.Int).Set(bn.n)
		} else if v.IsInt() {
			rv.serial = big.NewInt(v.Int64())
		}
		return v
	}, mrb.ArgsReq(1))

	revCls.DefineMethod("time", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rv, _ := rb.Get[*x509RevokedData](mrb, self)
		return mrb.Call(mrb.ClassGet("Time"), "at", mrb.Int64Value(rv.t.Unix()))
	}, mrb.ArgsNone())

	revCls.DefineMethod("time=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rv, _ := rb.Get[*x509RevokedData](mrb, self)
		v := mrb.GetArgsFirst()
		if sec, err := mrb.Funcall(v, mrb.Intern("to_i")); err == nil {
			rv.t = time.Unix(sec.Int64(), 0).UTC()
		}
		return v
	}, mrb.ArgsReq(1))

	crlCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if len(args) == 0 || args[0].IsNil() {
			rb.Set(mrb, self, &x509CRLData{
				version:    1,
				issuer:     &x509Name{},
				lastUpdate: time.Now().UTC(),
				nextUpdate: time.Now().UTC().Add(24 * time.Hour),
			})
			return self
		}
		raw, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		der := raw
		if block, _ := pem.Decode(raw); block != nil {
			der = block.Bytes
		}
		parsed, err := x509.ParseRevocationList(der)
		if err != nil {
			return mrb.Raise(crlErrCls, err.Error())
		}
		var rev []*x509RevokedData
		for _, entry := range parsed.RevokedCertificateEntries {
			rev = append(rev, &x509RevokedData{serial: new(big.Int).Set(entry.SerialNumber), t: entry.RevocationTime})
		}
		rb.Set(mrb, self, &x509CRLData{
			version:    1,
			issuer:     x509NameFromPKIX(parsed.Issuer),
			lastUpdate: parsed.ThisUpdate,
			nextUpdate: parsed.NextUpdate,
			revoked:    rev,
			rawDER:     parsed.Raw,
			parsed:     parsed,
		})
		return self
	}, mrb.ArgsOpt(1))

	crlCls.DefineMethod("version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		return mrb.FixnumValue(c.version)
	}, mrb.ArgsNone())
	crlCls.DefineMethod("version=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		v := mrb.GetArgsFirst()
		c.version = v.Int()
		return v
	}, mrb.ArgsReq(1))

	crlCls.DefineMethod("issuer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		if c.issuer == nil {
			return mrb.NilValue()
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, nameCls)
		rb.Set(mrb, obj.Value(), c.issuer)
		return obj
	}, mrb.ArgsNone())
	crlCls.DefineMethod("issuer=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		v := mrb.GetArgsFirst()
		if n, ok := rb.Get[*x509Name](mrb, v); ok {
			c.issuer = n
		}
		return v
	}, mrb.ArgsReq(1))

	crlCls.DefineMethod("last_update", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		return mrb.Call(mrb.ClassGet("Time"), "at", mrb.Int64Value(c.lastUpdate.Unix()))
	}, mrb.ArgsNone())
	crlCls.DefineMethod("last_update=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		v := mrb.GetArgsFirst()
		if sec, err := mrb.Funcall(v, mrb.Intern("to_i")); err == nil {
			c.lastUpdate = time.Unix(sec.Int64(), 0).UTC()
		}
		return v
	}, mrb.ArgsReq(1))

	crlCls.DefineMethod("next_update", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		return mrb.Call(mrb.ClassGet("Time"), "at", mrb.Int64Value(c.nextUpdate.Unix()))
	}, mrb.ArgsNone())
	crlCls.DefineMethod("next_update=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		v := mrb.GetArgsFirst()
		if sec, err := mrb.Funcall(v, mrb.Intern("to_i")); err == nil {
			c.nextUpdate = time.Unix(sec.Int64(), 0).UTC()
		}
		return v
	}, mrb.ArgsReq(1))

	crlCls.DefineMethod("add_revoked", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		v := mrb.GetArgsFirst()
		rv, ok := rb.Get[*x509RevokedData](mrb, v)
		if !ok {
			return mrb.Raise(crlErrCls, "expected OpenSSL::X509::Revoked")
		}
		c.revoked = append(c.revoked, rv)
		return v
	}, mrb.ArgsReq(1))

	crlCls.DefineMethod("revoked", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		ary := mrb.AryNewCapa(len(c.revoked))
		for _, rv := range c.revoked {
			obj := mrb.ObjAlloc(oruby.MrbTTCData, revCls)
			rb.Set(mrb, obj.Value(), rv)
			mrb.AryPush(ary, obj)
		}
		return ary
	}, mrb.ArgsNone())

	crlCls.DefineMethod("sign", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		args := mrb.GetArgs().Slice()
		signer, err := extractAnySigner(mrb, args[0])
		if err != nil {
			return mrb.Raise(crlErrCls, err.Error())
		}
		var digVal oruby.Value = mrb.NilValue()
		if len(args) >= 2 {
			digVal = args[1]
		}
		entries := make([]x509.RevocationListEntry, 0, len(c.revoked))
		for _, rv := range c.revoked {
			entries = append(entries, x509.RevocationListEntry{
				SerialNumber:   rv.serial,
				RevocationTime: rv.t,
			})
		}
		pubDER, _ := x509.MarshalPKIXPublicKey(signer.Public())
		ski := pubDER
		if len(ski) > 20 {
			ski = ski[:20]
		}
		issuerCert := &x509.Certificate{
			Subject:               c.issuer.toPKIX(),
			PublicKey:             signer.Public(),
			SubjectKeyId:          ski,
			KeyUsage:              x509.KeyUsageCRLSign,
			BasicConstraintsValid: true,
			IsCA:                  true,
		}
		tmpl := &x509.RevocationList{
			Number:                    big.NewInt(1),
			ThisUpdate:                c.lastUpdate,
			NextUpdate:                c.nextUpdate,
			RevokedCertificateEntries: entries,
			SignatureAlgorithm:        x509SigAlgFor(signer, digVal),
		}
		der, err := x509.CreateRevocationList(rand.Reader, tmpl, issuerCert, signer)
		if err != nil {
			return mrb.Raise(crlErrCls, err.Error())
		}
		parsed, err := x509.ParseRevocationList(der)
		if err != nil {
			return mrb.Raise(crlErrCls, err.Error())
		}
		c.rawDER = der
		c.parsed = parsed
		return self
	}, mrb.ArgsArg(1, 1))

	crlCls.DefineMethod("verify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		if c.parsed == nil {
			return mrb.FalseValue()
		}
		v := mrb.GetArgsFirst()
		if cert, ok := rb.Get[*x509Cert](mrb, v); ok && cert.parsed != nil {
			return mrb.BoolValue(cert.parsed.CheckSignature(c.parsed.SignatureAlgorithm, c.parsed.RawTBSRevocationList, c.parsed.Signature) == nil)
		}
		pub, err := extractAnyPublicKey(mrb, v)
		if err != nil {
			return mrb.FalseValue()
		}
		fakeCert := &x509.Certificate{PublicKey: pub}
		return mrb.BoolValue(fakeCert.CheckSignature(c.parsed.SignatureAlgorithm, c.parsed.RawTBSRevocationList, c.parsed.Signature) == nil)
	}, mrb.ArgsReq(1))

	crlCls.DefineMethod("to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		return mrb.Call(mrb.BytesValue(c.rawDER), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	crlCls.DefineMethod("to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509CRLData](mrb, self)
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: c.rawDER})))
	}, mrb.ArgsNone())
	crlCls.DefineAlias("to_s", "to_pem")
}

func x509NameFromPKIX(pn pkix.Name) *x509Name {
	n := &x509Name{}
	for _, tv := range pn.Names {
		key := tv.Type.String()
		for k, info := range x509NameOIDs {
			if info.oid.Equal(tv.Type) && len(k) <= 4 {
				key = k
				break
			}
		}
		n.entries = append(n.entries, x509NameEntry{
			key:      key,
			val:      fmt.Sprint(tv.Value),
			asn1Type: asn1UTF8String,
			oid:      tv.Type,
		})
	}
	return n
}
