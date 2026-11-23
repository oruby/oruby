package nethttp

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"software.sslmate.com/src/go-pkcs12"
)

type osslPKCS12 struct {
	der []byte
}

func wrapPKey(mrb *oruby.MrbState, key any) oruby.Value {
	if key == nil {
		return mrb.NilValue()
	}
	switch k := key.(type) {
	case *rsa.PrivateKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::RSA")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &rsaKey{priv: k, pub: &k.PublicKey})
		return obj.Value()
	case *rsa.PublicKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::RSA")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &rsaKey{pub: k})
		return obj.Value()
	case *ecdsa.PrivateKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::EC")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &ecKey{priv: k, pub: &k.PublicKey, curve: k.Curve, curveName: canonicalCurveName(k.Curve)})
		return obj.Value()
	case *ecdsa.PublicKey:
		cls := rb.ClassPath(mrb, "OpenSSL::PKey::EC")
		obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
		rb.Set(mrb, obj.Value(), &ecKey{pub: k, curve: k.Curve, curveName: canonicalCurveName(k.Curve)})
		return obj.Value()
	default:
		return mrb.NilValue()
	}
}

func pkixNameToRuby(mrb *oruby.MrbState, pn pkix.Name) oruby.Value {
	nameCls := rb.ClassPath(mrb, "OpenSSL::X509::Name")
	obj := mrb.ObjAlloc(oruby.MrbTTCData, nameCls)
	xn := &x509Name{}
	oidToKey := func(oidStr string) string {
		switch oidStr {
		case "2.5.4.3":
			return "CN"
		case "2.5.4.6":
			return "C"
		case "2.5.4.7":
			return "L"
		case "2.5.4.8":
			return "ST"
		case "2.5.4.10":
			return "O"
		case "2.5.4.11":
			return "OU"
		case "2.5.4.5":
			return "serialNumber"
		case "1.2.840.113549.1.9.1":
			return "emailAddress"
		default:
			return oidStr
		}
	}
	for _, atv := range pn.Names {
		s, ok := atv.Value.(string)
		if !ok {
			continue
		}
		xn.entries = append(xn.entries, x509NameEntry{
			key: oidToKey(atv.Type.String()),
			val: s,
			oid: atv.Type,
		})
	}
	rb.Set(mrb, obj.Value(), xn)
	return obj.Value()
}

func initOpenSSLPKCS12(mrb *oruby.MrbState, mod, osslErr oruby.RClass) {
	p12Cls := rb.DataClass(mrb.DefineClassUnder(mod, "PKCS12", mrb.ObjectClass()))
	p12Err := mrb.DefineClassUnder(p12Cls, "PKCS12Error", osslErr)

	p12Cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if len(args) == 0 || args[0].IsNil() {
			rb.Set(mrb, self, &osslPKCS12{})
			return self
		}
		der, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		pass := ""
		if len(args) >= 2 && !args[1].IsNil() {
			pass = args[1].String()
		}
		priv, cert, caCerts, err := pkcs12.DecodeChain(der, pass)
		if err != nil {
			return mrb.Raise(p12Err, err.Error())
		}
		rb.Set(mrb, self, &osslPKCS12{der: append([]byte(nil), der...)})
		mrb.SetIV(self, "@key", wrapPKey(mrb, priv))
		mrb.SetIV(self, "@certificate", wrapX509Cert(mrb, cert))
		caAry := mrb.AryNewCapa(len(caCerts))
		for _, c := range caCerts {
			mrb.AryPush(caAry, wrapX509Cert(mrb, c))
		}
		mrb.SetIV(self, "@ca_certs", caAry.Value())
		return self
	}, mrb.ArgsOpt(2))

	p12Cls.DefineClassMethod("create", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		pass := ""
		if !args[0].IsNil() {
			pass = args[0].String()
		}
		// args[1] = friendly name (optional)
		var priv any
		if rk, ok := rb.Get[*rsaKey](mrb, args[2]); ok && rk.priv != nil {
			priv = rk.priv
		} else if ek, ok := rb.Get[*ecKey](mrb, args[2]); ok && ek.priv != nil {
			priv = ek.priv
		} else {
			return mrb.Raise(p12Err, "private key required")
		}
		c, ok := rb.Get[*x509Cert](mrb, args[3])
		if !ok || c.parsed == nil {
			return mrb.Raise(p12Err, "signed X509::Certificate required")
		}
		var caCerts []*x509.Certificate
		if len(args) >= 5 && args[4].IsArray() {
			for i := 0; i < oruby.RArrayLen(args[4]); i++ {
				if cc, ok := rb.Get[*x509Cert](mrb, mrb.AryRef(args[4], i)); ok && cc.parsed != nil {
					caCerts = append(caCerts, cc.parsed)
				}
			}
		}
		var pfx []byte
		var err error
		if pass == "" {
			pfx, err = pkcs12.Passwordless.Encode(priv, c.parsed, caCerts, "")
		} else {
			pfx, err = pkcs12.Modern.Encode(priv, c.parsed, caCerts, pass)
		}
		if err != nil {
			return mrb.Raise(p12Err, err.Error())
		}
		_ = rand.Reader
		obj := mrb.ObjAlloc(oruby.MrbTTCData, p12Cls)
		rb.Set(mrb, obj.Value(), &osslPKCS12{der: pfx})
		mrb.SetIV(obj.Value(), "@key", args[2])
		mrb.SetIV(obj.Value(), "@certificate", args[3])
		if len(args) >= 5 && args[4].IsArray() {
			mrb.SetIV(obj.Value(), "@ca_certs", args[4])
		} else {
			mrb.SetIV(obj.Value(), "@ca_certs", mrb.AryNew().Value())
		}
		return obj
	}, mrb.ArgsArg(4, 5))

	p12Cls.DefineMethod("key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@key")
	}, mrb.ArgsNone())

	p12Cls.DefineMethod("certificate", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@certificate")
	}, mrb.ArgsNone())

	p12Cls.DefineMethod("ca_certs", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@ca_certs")
	}, mrb.ArgsNone())

	p12Cls.DefineMethod("to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, ok := rb.Get[*osslPKCS12](mrb, self)
		if !ok || len(p.der) == 0 {
			return mrb.Raise(p12Err, "PKCS12 not initialized")
		}
		return mrb.Call(mrb.BytesValue(p.der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())
}
