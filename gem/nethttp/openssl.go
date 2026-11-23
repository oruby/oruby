package nethttp

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func hashConstructor(name string) (func() hash.Hash, bool) {
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "")) {
	case "MD5":
		return md5.New, true
	case "SHA1":
		return sha1.New, true
	case "SHA224":
		return sha256.New224, true
	case "SHA256":
		return sha256.New, true
	case "SHA384":
		return sha512.New384, true
	case "SHA512":
		return sha512.New, true
	case "SHA512224":
		return sha512.New512_224, true
	case "SHA512256":
		return sha512.New512_256, true
	default:
		return nil, false
	}
}

func x509SigAlgo(name string) x509.SignatureAlgorithm {
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "")) {
	case "MD5":
		return x509.MD5WithRSA
	case "SHA1":
		return x509.SHA1WithRSA
	case "SHA256":
		return x509.SHA256WithRSA
	case "SHA384":
		return x509.SHA384WithRSA
	case "SHA512":
		return x509.SHA512WithRSA
	default:
		return x509.SHA256WithRSA
	}
}

// pbkdf2HMAC implements RFC 2898 PBKDF2 with HMAC.
func pbkdf2HMAC(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte {
	if keyLen == 0 {
		return nil
	}
	prf := hmac.New(h, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	u := make([]byte, hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(buf[:], uint32(block))
		prf.Write(buf[:])
		dk = prf.Sum(dk)
		t := dk[len(dk)-hashLen:]
		copy(u, t)

		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(u)
			u = u[:0]
			u = prf.Sum(u)
			for x := range u {
				t[x] ^= u[x]
			}
		}
	}
	return dk[:keyLen]
}

// -- X.509 Name ---------------------------------------------------------------

const (
	asn1Integer         = 2
	asn1BitString       = 3
	asn1OctetString     = 4
	asn1Null            = 5
	asn1Object          = 6
	asn1UTF8String      = 12
	asn1Sequence        = 16
	asn1Set             = 17
	asn1PrintableString = 19
	asn1T61String       = 20
	asn1IA5String       = 22
	asn1UTCTime         = 23
	asn1GeneralizedTime = 24
	asn1UniversalString = 28
	asn1BMPString       = 30
)

type oidInfo struct {
	oid      asn1.ObjectIdentifier
	asn1Type int
}

var x509NameOIDs = map[string]oidInfo{
	"CN":           {asn1.ObjectIdentifier{2, 5, 4, 3}, asn1UTF8String},
	"commonName":   {asn1.ObjectIdentifier{2, 5, 4, 3}, asn1UTF8String},
	"SN":           {asn1.ObjectIdentifier{2, 5, 4, 4}, asn1UTF8String},
	"serialNumber": {asn1.ObjectIdentifier{2, 5, 4, 5}, asn1PrintableString},
	"C":            {asn1.ObjectIdentifier{2, 5, 4, 6}, asn1PrintableString},
	"countryName":  {asn1.ObjectIdentifier{2, 5, 4, 6}, asn1PrintableString},
	"L":            {asn1.ObjectIdentifier{2, 5, 4, 7}, asn1UTF8String},
	"ST":           {asn1.ObjectIdentifier{2, 5, 4, 8}, asn1UTF8String},
	"STREET":       {asn1.ObjectIdentifier{2, 5, 4, 9}, asn1UTF8String},
	"O":            {asn1.ObjectIdentifier{2, 5, 4, 10}, asn1UTF8String},
	"OU":           {asn1.ObjectIdentifier{2, 5, 4, 11}, asn1UTF8String},
	"title":        {asn1.ObjectIdentifier{2, 5, 4, 12}, asn1UTF8String},
	"GN":           {asn1.ObjectIdentifier{2, 5, 4, 42}, asn1UTF8String},
	"initials":     {asn1.ObjectIdentifier{2, 5, 4, 43}, asn1UTF8String},
	"DC":           {asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 25}, asn1IA5String},
	"UID":          {asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 1}, asn1UTF8String},
	"emailAddress": {asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}, asn1IA5String},
}

type x509NameEntry struct {
	key      string
	val      string
	asn1Type int
	oid      asn1.ObjectIdentifier
}

type x509Name struct {
	entries []x509NameEntry
}

func (n *x509Name) toPKIX() pkix.Name {
	var pn pkix.Name
	for _, e := range n.entries {
		pn.ExtraNames = append(pn.ExtraNames, pkix.AttributeTypeAndValue{
			Type:  e.oid,
			Value: e.val,
		})
	}
	return pn
}

func (n *x509Name) toString() string {
	var b strings.Builder
	for _, e := range n.entries {
		b.WriteByte('/')
		b.WriteString(e.key)
		b.WriteByte('=')
		b.WriteString(e.val)
	}
	return b.String()
}

func parseX509Name(s string) (*x509Name, error) {
	if s == "" {
		return &x509Name{}, nil
	}
	var parts []string
	if strings.HasPrefix(s, "/") {
		parts = strings.Split(s[1:], "/")
	} else if strings.Contains(s, ",") || strings.Contains(s, "=") {
		parts = strings.Split(s, ",")
	} else {
		return nil, &nameTypeErr{msg: "invalid X509::Name format"}
	}

	name := &x509Name{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			return nil, &nameTypeErr{msg: fmt.Sprintf("invalid X509::Name entry: %q", part)}
		}
		k := strings.TrimSpace(part[:eq])
		v := strings.TrimSpace(part[eq+1:])
		info, ok := x509NameOIDs[k]
		if !ok {
			return nil, &nameInvalidKeyErr{msg: fmt.Sprintf("invalid field name: %s", k)}
		}
		name.entries = append(name.entries, x509NameEntry{
			key:      k,
			val:      v,
			asn1Type: info.asn1Type,
			oid:      info.oid,
		})
	}
	if len(name.entries) == 0 {
		return nil, &nameTypeErr{msg: "empty X509::Name"}
	}
	return name, nil
}

type nameTypeErr struct{ msg string }

func (e *nameTypeErr) Error() string { return e.msg }

type nameInvalidKeyErr struct{ msg string }

func (e *nameInvalidKeyErr) Error() string { return e.msg }

// -- PKey::RSA ----------------------------------------------------------------

type rsaKey struct {
	priv *rsa.PrivateKey
	pub  *rsa.PublicKey
}

// -- X.509 Extension / Certificate / Store ------------------------------------

type x509Ext struct {
	oid      string
	value    string
	critical bool
	subject  *x509Cert
	issuer   *x509Cert
}

type x509Cert struct {
	version   int
	serial    *big.Int
	subject   *x509Name
	issuer    *x509Name
	pubKey    any // *rsa.PublicKey or *ecdsa.PublicKey
	notBefore time.Time
	notAfter  time.Time
	exts      []*x509Ext
	rawDER    []byte
	parsed    *x509.Certificate
}

type x509Store struct {
	certs       []*x509.Certificate
	lastErr     int
	lastErrText string
}

func rubyTimeToGo(mrb *oruby.MrbState, v oruby.Value) (time.Time, error) {
	if v.IsNil() {
		return time.Time{}, nil
	}
	secVal, err := mrb.Funcall(v, oruby.Sym_to_i)
	if err != nil {
		return time.Time{}, err
	}
	nsecVal, err := mrb.Funcall(v, mrb.Intern("nsec"))
	if err != nil {
		mrb.SetExc(nil)
		return time.Unix(secVal.Int64(), 0).UTC(), nil
	}
	return time.Unix(secVal.Int64(), nsecVal.Int64()).UTC(), nil
}

func goTimeToRuby(mrb *oruby.MrbState, t time.Time) oruby.Value {
	if t.IsZero() {
		return mrb.NilValue()
	}
	timeCls := mrb.ClassGet("Time")
	v, err := mrb.Funcall(timeCls, mrb.Intern("at"), t.Unix(), t.Nanosecond()/1000)
	if err != nil {
		mrb.SetExc(nil)
		return mrb.NilValue()
	}
	u, err := mrb.Funcall(v, mrb.Intern("utc"))
	if err != nil {
		mrb.SetExc(nil)
		return v.Value()
	}
	return u.Value()
}

func keyHashID(pub any) []byte {
	if pub == nil {
		return nil
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil
	}
	sum := sha1.Sum(der)
	return sum[:]
}

func applyExtensions(tmpl *x509.Certificate, exts []*x509Ext) {
	for _, ext := range exts {
		switch strings.ToLower(ext.oid) {
		case "basicconstraints":
			tmpl.BasicConstraintsValid = true
			for _, tok := range strings.Split(ext.value, ",") {
				tok = strings.TrimSpace(strings.ToUpper(tok))
				if tok == "CA:TRUE" {
					tmpl.IsCA = true
				} else if tok == "CA:FALSE" {
					tmpl.IsCA = false
				}
			}
		case "keyusage":
			var ku x509.KeyUsage
			for _, tok := range strings.Split(ext.value, ",") {
				switch strings.ToLower(strings.TrimSpace(tok)) {
				case "digitalsignature":
					ku |= x509.KeyUsageDigitalSignature
				case "nonrepudiation", "contentcommitment":
					ku |= x509.KeyUsageContentCommitment
				case "keyencipherment":
					ku |= x509.KeyUsageKeyEncipherment
				case "dataencipherment":
					ku |= x509.KeyUsageDataEncipherment
				case "keyagreement":
					ku |= x509.KeyUsageKeyAgreement
				case "keycertsign":
					ku |= x509.KeyUsageCertSign
				case "crlsign":
					ku |= x509.KeyUsageCRLSign
				case "encipheronly":
					ku |= x509.KeyUsageEncipherOnly
				case "decipheronly":
					ku |= x509.KeyUsageDecipherOnly
				}
			}
			tmpl.KeyUsage = ku
		case "subjectkeyidentifier":
			if strings.EqualFold(strings.TrimSpace(ext.value), "hash") {
				pub := tmpl.PublicKey
				if ext.subject != nil && ext.subject.pubKey != nil {
					pub = ext.subject.pubKey
				}
				if pub != nil {
					tmpl.SubjectKeyId = keyHashID(pub)
				}
			}
		case "authoritykeyidentifier":
			if ext.issuer != nil && ext.issuer.pubKey != nil {
				tmpl.AuthorityKeyId = keyHashID(ext.issuer.pubKey)
			}
		case "subjectaltname":
			for _, tok := range strings.Split(ext.value, ",") {
				tok = strings.TrimSpace(tok)
				if strings.HasPrefix(tok, "DNS:") {
					tmpl.DNSNames = append(tmpl.DNSNames, strings.TrimSpace(tok[4:]))
				} else if strings.HasPrefix(tok, "IP:") {
					if ip := net.ParseIP(strings.TrimSpace(tok[3:])); ip != nil {
						tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
					}
				} else if strings.HasPrefix(tok, "email:") {
					tmpl.EmailAddresses = append(tmpl.EmailAddresses, strings.TrimSpace(tok[6:]))
				}
			}
		}
	}
}

func coerceInt64(mrb *oruby.MrbState, v oruby.Value) (int64, error) {
	if v.IsInt() {
		return v.Int64(), nil
	}
	if mrb.RespondTo(v, oruby.Sym_to_int) {
		ret, err := mrb.Funcall(v, oruby.Sym_to_int)
		if err != nil {
			return 0, err
		}
		if ret.IsInt() {
			return ret.Int64(), nil
		}
		return 0, fmt.Errorf("can't convert %s into Integer (%s#to_int gives %s)",
			mrb.ClassName(mrb.ClassOf(v)), mrb.ClassName(mrb.ClassOf(v)), mrb.ClassName(mrb.ClassOf(ret)))
	}
	return 0, fmt.Errorf("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ClassOf(v)))
}

func initOpenSSLGo(mrb *oruby.MrbState, mod oruby.RClass) {
	osslErr := mrb.DefineClassUnder(mod, "OpenSSLError", mrb.ClassGet("StandardError"))

	// OpenSSL::Cipher + CipherError
	cipherCls := mrb.DefineClassUnder(mod, "Cipher", mrb.ObjectClass())
	mrb.DefineClassUnder(cipherCls, "CipherError", osslErr)

	// OpenSSL::ASN1 constants
	asn1Mod := mrb.DefineModuleUnder(mod, "ASN1")
	asn1Mod.Const("INTEGER", asn1Integer)
	asn1Mod.Const("BIT_STRING", asn1BitString)
	asn1Mod.Const("OCTET_STRING", asn1OctetString)
	asn1Mod.Const("NULL", asn1Null)
	asn1Mod.Const("OBJECT", asn1Object)
	asn1Mod.Const("UTF8STRING", asn1UTF8String)
	asn1Mod.Const("SEQUENCE", asn1Sequence)
	asn1Mod.Const("SET", asn1Set)
	asn1Mod.Const("PRINTABLESTRING", asn1PrintableString)
	asn1Mod.Const("T61STRING", asn1T61String)
	asn1Mod.Const("IA5STRING", asn1IA5String)
	asn1Mod.Const("UTCTIME", asn1UTCTime)
	asn1Mod.Const("GENERALIZEDTIME", asn1GeneralizedTime)
	asn1Mod.Const("UNIVERSALSTRING", asn1UniversalString)
	asn1Mod.Const("BMPSTRING", asn1BMPString)

	// OpenSSL::KDF + KDFError + pbkdf2_hmac (pure Go)
	kdfMod := mrb.DefineModuleUnder(mod, "KDF")
	kdfErr := mrb.DefineClassUnder(kdfMod, "KDFError", osslErr)
	kdfMod.DefineClassMethod("pbkdf2_hmac", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		kw := mrb.KeywordArgs().Value()
		if !kw.IsHash() && args.Len() > 1 && args.Item(1).IsHash() {
			kw = args.Item(1)
		}
		reqKeys := []string{"salt", "iterations", "length", "hash"}
		var missing []string
		vals := make([]oruby.Value, len(reqKeys))
		for i, k := range reqKeys {
			sym := mrb.SymbolValue(mrb.Intern(k))
			if !kw.IsHash() || !mrb.HashKeyP(kw, sym) {
				missing = append(missing, ":"+k)
			} else {
				vals[i] = mrb.HashGet(kw, sym)
			}
		}
		if len(missing) == 1 {
			return mrb.Raisef(mrb.EArgumentError(), "missing keyword: %s", missing[0])
		}
		if len(missing) > 1 {
			return mrb.Raisef(mrb.EArgumentError(), "missing keywords: %s", strings.Join(missing, ", "))
		}
		pass, err := rb.Bytes(mrb, args.Item(0))
		if err != nil {
			if mrb.Exc() != nil {
				return mrb.NilValue()
			}
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		salt, err := rb.Bytes(mrb, vals[0])
		if err != nil {
			if mrb.Exc() != nil {
				return mrb.NilValue()
			}
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		iter, err := coerceInt64(mrb, vals[1])
		if err != nil {
			if mrb.Exc() != nil {
				return mrb.NilValue()
			}
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		keyLen, err := coerceInt64(mrb, vals[2])
		if err != nil {
			if mrb.Exc() != nil {
				return mrb.NilValue()
			}
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		hv := vals[3]
		var algoName string
		digestCls := rb.ClassPath(mrb, "OpenSSL::Digest")
		if !digestCls.IsNil() && mrb.ObjIsKindOf(hv, digestCls) {
			nv, err := mrb.Funcall(hv, oruby.Sym_name)
			if err != nil {
				return mrb.RaiseError(err)
			}
			algoName = nv.String()
		} else if hv.IsString() {
			algoName = hv.String()
		} else {
			return mrb.Raisef(mrb.ETypeError(), "wrong argument type %s (expected String or OpenSSL::Digest)", mrb.ClassName(mrb.ClassOf(hv)))
		}
		if iter <= 0 {
			return mrb.Raise(kdfErr, "PKCS5_PBKDF2_HMAC: invalid iteration count")
		}
		if keyLen < 0 {
			return mrb.Raise(kdfErr, "PKCS5_PBKDF2_HMAC: invalid length")
		}
		h, ok := hashConstructor(algoName)
		if !ok {
			return rb.Raise(mrb, "OpenSSL::Digest::DigestError", "Unsupported digest algorithm (%s)", algoName)
		}
		dk := pbkdf2HMAC(pass, salt, int(iter), int(keyLen), h)
		return mrb.Call(mrb.BytesValue(dk), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	// OpenSSL::PKey + PKeyError + PKey + RSA
	pkeyMod := mrb.DefineModuleUnder(mod, "PKey")
	pkeyErr := mrb.DefineClassUnder(pkeyMod, "PKeyError", osslErr)
	pkeyBase := rb.DataClass(mrb.DefineClassUnder(pkeyMod, "PKey", mrb.ObjectClass()))
	rsaCls := rb.DataClass(mrb.DefineClassUnder(pkeyMod, "RSA", pkeyBase))

	rsaCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if len(args) == 0 {
			rb.Set(mrb, self, &rsaKey{})
			return self
		}
		if args[0].IsInt() {
			bits := int(args[0].Int64())
			var priv *rsa.PrivateKey
			var err error
			mrb.Blocking(func() {
				priv, err = rsa.GenerateKey(rand.Reader, bits)
			})
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
			rb.Set(mrb, self, &rsaKey{priv: priv, pub: &priv.PublicKey})
			return self
		}
		b, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		der := b
		if block, _ := pem.Decode(b); block != nil {
			der = block.Bytes
		}
		if priv, err := x509.ParsePKCS1PrivateKey(der); err == nil {
			rb.Set(mrb, self, &rsaKey{priv: priv, pub: &priv.PublicKey})
			return self
		}
		if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
			if priv, ok := k.(*rsa.PrivateKey); ok {
				rb.Set(mrb, self, &rsaKey{priv: priv, pub: &priv.PublicKey})
				return self
			}
		}
		if k, err := x509.ParsePKIXPublicKey(der); err == nil {
			if pub, ok := k.(*rsa.PublicKey); ok {
				rb.Set(mrb, self, &rsaKey{pub: pub})
				return self
			}
		}
		return mrb.Raise(pkeyErr, "Neither PUB key nor PRIV key")
	}, mrb.ArgsArg(0, 2))

	rsaCls.DefineMethod("public_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(pkeyErr, "RSA key not initialized")
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, rsaCls)
		rb.Set(mrb, obj.Value(), &rsaKey{pub: k.pub})
		return obj
	}, mrb.ArgsNone())

	rsaCls.DefineMethod("private?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		return mrb.BoolValue(ok && k.priv != nil)
	}, mrb.ArgsNone())

	rsaCls.DefineMethod("public?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		return mrb.BoolValue(ok && k.pub != nil)
	}, mrb.ArgsNone())

	rsaCls.DefineMethod("to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "RSA key not initialized")
		}
		var der []byte
		var err error
		if k.priv != nil {
			der = x509.MarshalPKCS1PrivateKey(k.priv)
		} else if k.pub != nil {
			der, err = x509.MarshalPKIXPublicKey(k.pub)
			if err != nil {
				return mrb.Raise(pkeyErr, err.Error())
			}
		}
		return mrb.Call(mrb.BytesValue(der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	rsaCls.DefineMethod("to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok {
			return mrb.Raise(pkeyErr, "RSA key not initialized")
		}
		if k.priv != nil {
			der := x509.MarshalPKCS1PrivateKey(k.priv)
			return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})))
		}
		der, err := x509.MarshalPKIXPublicKey(k.pub)
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	}, mrb.ArgsNone())

	// OpenSSL::X509
	x509Mod := mrb.DefineModuleUnder(mod, "X509")
	nameErrCls := mrb.DefineClassUnder(x509Mod, "NameError", osslErr)
	certErrCls := mrb.DefineClassUnder(x509Mod, "CertificateError", osslErr)
	storeErrCls := mrb.DefineClassUnder(x509Mod, "StoreError", osslErr)
	x509Mod.Const("V_OK", 0)
	x509Mod.Const("V_ERR_CERT_NOT_YET_VALID", 9)
	x509Mod.Const("V_ERR_CERT_HAS_EXPIRED", 10)
	x509Mod.Const("V_ERR_DEPTH_ZERO_SELF_SIGNED_CERT", 18)
	x509Mod.Const("V_ERR_SELF_SIGNED_CERT_IN_CHAIN", 19)
	x509Mod.Const("V_ERR_UNABLE_TO_GET_ISSUER_CERT_LOCALLY", 20)

	// OpenSSL::X509::Name
	nameCls := rb.DataClass(mrb.DefineClassUnder(x509Mod, "Name", mrb.ObjectClass()))
	nameCls.DefineClassMethod("parse", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		arg := mrb.GetArgsFirst()
		s, err := rb.Str(mrb, arg)
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		n, err := parseX509Name(s)
		if err != nil {
			if _, ok := err.(*nameInvalidKeyErr); ok {
				return mrb.Raise(nameErrCls, err.Error())
			}
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, nameCls)
		rb.Set(mrb, obj.Value(), n)
		return obj
	}, mrb.ArgsArg(1, 1))

	nameCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, &x509Name{})
		return self
	}, mrb.ArgsOpt(2))

	nameCls.DefineMethod("to_s", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, ok := rb.Get[*x509Name](mrb, self)
		if !ok {
			return mrb.StrNew("")
		}
		return mrb.StrNew(n.toString())
	}, mrb.ArgsOpt(1))

	nameCls.DefineMethod("to_a", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, ok := rb.Get[*x509Name](mrb, self)
		out := mrb.AryNew()
		if !ok {
			return out
		}
		for _, e := range n.entries {
			row := mrb.AryNewCapa(3)
			row.Push(mrb.StrNew(e.key))
			row.Push(mrb.StrNew(e.val))
			row.Push(mrb.FixnumValue(e.asn1Type))
			out.Push(row)
		}
		return out
	}, mrb.ArgsNone())

	nameCls.DefineMethod("==", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n1, ok1 := rb.Get[*x509Name](mrb, self)
		n2, ok2 := rb.Get[*x509Name](mrb, mrb.GetArgsFirst())
		if !ok1 || !ok2 {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(n1.toString() == n2.toString())
	}, mrb.ArgsReq(1))

	// OpenSSL::X509::Extension & ExtensionFactory
	extCls := rb.DataClass(mrb.DefineClassUnder(x509Mod, "Extension", mrb.ObjectClass()))
	extCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		ext := &x509Ext{}
		if len(args) >= 2 {
			ext.oid = args[0].String()
			ext.value = args[1].String()
		}
		if len(args) >= 3 {
			ext.critical = args[2].Bool()
		}
		rb.Set(mrb, self, ext)
		return self
	}, mrb.ArgsArg(1, 2))
	extCls.DefineMethod("oid", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ext, _ := rb.Get[*x509Ext](mrb, self)
		if ext == nil {
			return mrb.StrNew("")
		}
		return mrb.StrNew(ext.oid)
	}, mrb.ArgsNone())
	extCls.DefineMethod("value", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ext, _ := rb.Get[*x509Ext](mrb, self)
		if ext == nil {
			return mrb.StrNew("")
		}
		return mrb.StrNew(ext.value)
	}, mrb.ArgsNone())
	extCls.DefineMethod("critical?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ext, _ := rb.Get[*x509Ext](mrb, self)
		return mrb.BoolValue(ext != nil && ext.critical)
	}, mrb.ArgsNone())

	efCls := mrb.DefineClassUnder(x509Mod, "ExtensionFactory", mrb.ObjectClass())
	efCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if len(args) >= 1 {
			mrb.SetIV(self, "@subject_certificate", args[0])
		}
		if len(args) >= 2 {
			mrb.SetIV(self, "@issuer_certificate", args[1])
		}
		return self
	}, mrb.ArgsOpt(4))
	efCls.DefineMethod("subject_certificate=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		mrb.SetIV(self, "@subject_certificate", v)
		return v
	}, mrb.ArgsReq(1))
	efCls.DefineMethod("subject_certificate", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@subject_certificate")
	}, mrb.ArgsNone())
	efCls.DefineMethod("issuer_certificate=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		mrb.SetIV(self, "@issuer_certificate", v)
		return v
	}, mrb.ArgsReq(1))
	efCls.DefineMethod("issuer_certificate", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@issuer_certificate")
	}, mrb.ArgsNone())
	efCls.DefineMethod("create_extension", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		oid := args[0].String()
		val := args[1].String()
		crit := false
		if len(args) >= 3 {
			crit = args[2].Bool()
		}
		ext := &x509Ext{oid: oid, value: val, critical: crit}
		if sc := mrb.GetIV(self, "@subject_certificate"); !sc.IsNil() {
			ext.subject, _ = rb.Get[*x509Cert](mrb, sc)
		}
		if ic := mrb.GetIV(self, "@issuer_certificate"); !ic.IsNil() {
			ext.issuer, _ = rb.Get[*x509Cert](mrb, ic)
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, extCls)
		rb.Set(mrb, obj.Value(), ext)
		return obj
	}, mrb.ArgsArg(2, 1))

	// OpenSSL::X509::Certificate
	certCls := rb.DataClass(mrb.DefineClassUnder(x509Mod, "Certificate", mrb.ObjectClass()))
	certCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := &x509Cert{serial: big.NewInt(0)}
		args := mrb.GetArgs().Slice()
		if len(args) >= 1 && !args[0].IsNil() {
			b, err := rb.Bytes(mrb, args[0])
			if err != nil {
				return rb.TypeError(mrb, err)
			}
			der := b
			if block, _ := pem.Decode(b); block != nil {
				der = block.Bytes
			}
			parsed, err := x509.ParseCertificate(der)
			if err != nil {
				return mrb.Raise(certErrCls, err.Error())
			}
			c.rawDER = der
			c.parsed = parsed
			c.version = parsed.Version - 1
			c.serial = parsed.SerialNumber
			c.notBefore = parsed.NotBefore
			c.notAfter = parsed.NotAfter
			if rp, ok := parsed.PublicKey.(*rsa.PublicKey); ok {
				c.pubKey = rp
			}
		}
		rb.Set(mrb, self, c)
		return self
	}, mrb.ArgsOpt(1))

	certCls.DefineMethod("version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509Cert](mrb, self)
		if c == nil {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(c.version)
	}, mrb.ArgsNone())
	certCls.DefineMethod("version=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509Cert](mrb, self)
		v := mrb.GetArgsFirst()
		if c != nil {
			c.version = int(v.Int64())
		}
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("serial", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509Cert](mrb, self)
		if c == nil || c.serial == nil {
			return mrb.FixnumValue(0)
		}
		return oruby.Int64(c.serial.Int64())
	}, mrb.ArgsNone())
	certCls.DefineMethod("serial=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509Cert](mrb, self)
		v := mrb.GetArgsFirst()
		if c != nil {
			iv, err := mrb.Funcall(v, oruby.Sym_to_i)
			if err != nil {
				return mrb.RaiseError(err)
			}
			c.serial = big.NewInt(iv.Int64())
		}
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("subject", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if v := mrb.GetIV(self, "@subject"); !v.IsNil() {
			return v
		}
		if c, _ := rb.Get[*x509Cert](mrb, self); c != nil && c.parsed != nil {
			v := pkixNameToRuby(mrb, c.parsed.Subject)
			mrb.SetIV(self, "@subject", v)
			return v
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
	certCls.DefineMethod("subject=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		c, _ := rb.Get[*x509Cert](mrb, self)
		n, ok := rb.Get[*x509Name](mrb, v)
		if !ok {
			return mrb.Raise(mrb.ETypeError(), "wrong argument type (expected OpenSSL::X509::Name)")
		}
		if c != nil {
			c.subject = n
		}
		mrb.SetIV(self, "@subject", v)
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("issuer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if v := mrb.GetIV(self, "@issuer"); !v.IsNil() {
			return v
		}
		if c, _ := rb.Get[*x509Cert](mrb, self); c != nil && c.parsed != nil {
			v := pkixNameToRuby(mrb, c.parsed.Issuer)
			mrb.SetIV(self, "@issuer", v)
			return v
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
	certCls.DefineMethod("issuer=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		c, _ := rb.Get[*x509Cert](mrb, self)
		n, ok := rb.Get[*x509Name](mrb, v)
		if !ok {
			return mrb.Raise(mrb.ETypeError(), "wrong argument type (expected OpenSSL::X509::Name)")
		}
		if c != nil {
			c.issuer = n
		}
		mrb.SetIV(self, "@issuer", v)
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("public_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if v := mrb.GetIV(self, "@public_key"); !v.IsNil() {
			return v
		}
		if c, _ := rb.Get[*x509Cert](mrb, self); c != nil && c.parsed != nil {
			v := wrapPKey(mrb, c.parsed.PublicKey)
			mrb.SetIV(self, "@public_key", v)
			return v
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
	certCls.DefineMethod("public_key=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		c, _ := rb.Get[*x509Cert](mrb, self)
		var pub any
		if k, ok := rb.Get[*rsaKey](mrb, v); ok && k.pub != nil {
			pub = k.pub
		} else if k, ok := rb.Get[*ecKey](mrb, v); ok && k.pub != nil {
			pub = k.pub
		} else {
			return mrb.Raise(mrb.ETypeError(), "wrong argument type (expected OpenSSL::PKey::PKey)")
		}
		if c != nil {
			c.pubKey = pub
		}
		mrb.SetIV(self, "@public_key", v)
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("not_before", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509Cert](mrb, self)
		if c == nil {
			return mrb.NilValue()
		}
		return goTimeToRuby(mrb, c.notBefore)
	}, mrb.ArgsNone())
	certCls.DefineMethod("not_before=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		t, err := rubyTimeToGo(mrb, v)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if c, _ := rb.Get[*x509Cert](mrb, self); c != nil {
			c.notBefore = t
		}
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("not_after", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := rb.Get[*x509Cert](mrb, self)
		if c == nil {
			return mrb.NilValue()
		}
		return goTimeToRuby(mrb, c.notAfter)
	}, mrb.ArgsNone())
	certCls.DefineMethod("not_after=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		t, err := rubyTimeToGo(mrb, v)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if c, _ := rb.Get[*x509Cert](mrb, self); c != nil {
			c.notAfter = t
		}
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("add_extension", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		ext, ok := rb.Get[*x509Ext](mrb, v)
		if !ok {
			return mrb.Raise(mrb.ETypeError(), "wrong argument type (expected OpenSSL::X509::Extension)")
		}
		if c, _ := rb.Get[*x509Cert](mrb, self); c != nil {
			c.exts = append(c.exts, ext)
		}
		return v
	}, mrb.ArgsReq(1))

	certCls.DefineMethod("sign", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		c, ok := rb.Get[*x509Cert](mrb, self)
		if !ok {
			return mrb.Raise(certErrCls, "certificate not initialized")
		}
		var signerPriv any
		var signerPub any
		if rk, ok := rb.Get[*rsaKey](mrb, args[0]); ok && rk.priv != nil {
			signerPriv = rk.priv
			signerPub = &rk.priv.PublicKey
		} else if ek, ok := rb.Get[*ecKey](mrb, args[0]); ok && ek.priv != nil {
			signerPriv = ek.priv
			signerPub = &ek.priv.PublicKey
		} else {
			return mrb.Raise(certErrCls, "private key required")
		}
		digestName := "SHA256"
		if args[1].IsString() {
			digestName = args[1].String()
		} else if mrb.RespondTo(args[1], oruby.Sym_name) {
			if nv, err := mrb.Funcall(args[1], oruby.Sym_name); err == nil {
				digestName = nv.String()
			}
		}
		tmpl := &x509.Certificate{
			SerialNumber:       c.serial,
			NotBefore:          c.notBefore,
			NotAfter:           c.notAfter,
			PublicKey:          c.pubKey,
			SignatureAlgorithm: x509SigAlgoForKey(digestName, signerPriv),
		}
		if tmpl.SerialNumber == nil {
			tmpl.SerialNumber = big.NewInt(1)
		}
		if c.subject != nil {
			tmpl.Subject = c.subject.toPKIX()
		}
		applyExtensions(tmpl, c.exts)

		parent := tmpl
		if c.issuer != nil && (c.subject == nil || c.issuer.toString() != c.subject.toString()) {
			parent = &x509.Certificate{
				Subject:      c.issuer.toPKIX(),
				SubjectKeyId: tmpl.AuthorityKeyId,
			}
		}
		pub := c.pubKey
		if pub == nil {
			pub = signerPub
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signerPriv)
		if err != nil {
			return mrb.Raise(certErrCls, err.Error())
		}
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			return mrb.Raise(certErrCls, err.Error())
		}
		c.rawDER = der
		c.parsed = parsed
		return self
	}, mrb.ArgsReq(2))

	certCls.DefineMethod("to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, ok := rb.Get[*x509Cert](mrb, self)
		if !ok || len(c.rawDER) == 0 {
			return mrb.Raise(certErrCls, "certificate not signed")
		}
		return mrb.Call(mrb.BytesValue(c.rawDER), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	certCls.DefineMethod("to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, ok := rb.Get[*x509Cert](mrb, self)
		if !ok || len(c.rawDER) == 0 {
			return mrb.Raise(certErrCls, "certificate not signed")
		}
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.rawDER})))
	}, mrb.ArgsNone())

	// OpenSSL::X509::Store
	storeCls := rb.DataClass(mrb.DefineClassUnder(x509Mod, "Store", mrb.ObjectClass()))
	storeCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, &x509Store{lastErr: 0, lastErrText: "ok"})
		return self
	}, mrb.ArgsNone())

	storeCls.DefineMethod("add_cert", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st, ok := rb.Get[*x509Store](mrb, self)
		if !ok {
			return mrb.Raise(storeErrCls, "store not initialized")
		}
		c, ok := rb.Get[*x509Cert](mrb, mrb.GetArgsFirst())
		if !ok || c.parsed == nil {
			return mrb.Raise(storeErrCls, "invalid certificate")
		}
		st.certs = append(st.certs, c.parsed)
		return self
	}, mrb.ArgsReq(1))

	storeCls.DefineMethod("verify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st, ok := rb.Get[*x509Store](mrb, self)
		if !ok {
			return mrb.Raise(storeErrCls, "store not initialized")
		}
		args := mrb.GetArgs().Slice()
		c, ok := rb.Get[*x509Cert](mrb, args[0])
		if !ok || c.parsed == nil {
			st.lastErr = 20
			st.lastErrText = "invalid certificate"
			return mrb.FalseValue()
		}
		roots := x509.NewCertPool()
		intermediates := x509.NewCertPool()
		for _, sc := range st.certs {
			if bytes.Equal(sc.RawIssuer, sc.RawSubject) {
				roots.AddCert(sc)
			} else {
				intermediates.AddCert(sc)
			}
		}
		if len(args) >= 2 && args[1].IsArray() {
			for i := 0; i < oruby.RArrayLen(args[1]); i++ {
				if ic, ok := rb.Get[*x509Cert](mrb, mrb.AryEntry(args[1], i)); ok && ic.parsed != nil {
					intermediates.AddCert(ic.parsed)
				}
			}
		}
		opts := x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}
		_, err := c.parsed.Verify(opts)
		if err != nil {
			var inv x509.CertificateInvalidError
			if errors.As(err, &inv) && inv.Reason == x509.Expired {
				st.lastErr = 10
				st.lastErrText = "certificate has expired"
			} else {
				st.lastErr = 20
				st.lastErrText = err.Error()
			}
			return mrb.FalseValue()
		}
		st.lastErr = 0
		st.lastErrText = "ok"
		return mrb.TrueValue()
	}, mrb.ArgsArg(1, 1))

	storeCls.DefineMethod("error", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st, _ := rb.Get[*x509Store](mrb, self)
		if st == nil {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(st.lastErr)
	}, mrb.ArgsNone())

	storeCls.DefineMethod("set_default_paths", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.NilValue()
	}, mrb.ArgsNone())

	storeCls.DefineMethod("error_string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st, _ := rb.Get[*x509Store](mrb, self)
		if st == nil {
			return mrb.StrNew("ok")
		}
		return mrb.StrNew(st.lastErrText)
	}, mrb.ArgsNone())

	initOpenSSLExt(mrb, mod, osslErr, pkeyMod, pkeyErr, pkeyBase, rsaCls, certCls)
	initOpenSSLCipherPKCS5ASN1(mrb, mod, osslErr, cipherCls, asn1Mod)
	initOpenSSLPKCS12(mrb, mod, osslErr)
}
