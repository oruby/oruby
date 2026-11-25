package nethttp

import (
	"bufio"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

// -- OpenSSL::BN --------------------------------------------------------------

type osslBN struct {
	n *big.Int
}

func newBNValue(mrb *oruby.MrbState, n *big.Int) oruby.Value {
	cls := rb.ClassPath(mrb, "OpenSSL::BN")
	obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
	cp := new(big.Int)
	if n != nil {
		cp.Set(n)
	}
	rb.Set(mrb, obj.Value(), &osslBN{n: cp})
	return obj.Value()
}

func coerceToBigInt(mrb *oruby.MrbState, v oruby.Value) (*big.Int, error) {
	if bn, ok := rb.Get[*osslBN](mrb, v); ok && bn.n != nil {
		return new(big.Int).Set(bn.n), nil
	}
	if v.IsInt() {
		return big.NewInt(v.Int64()), nil
	}
	if mrb.RespondTo(v, mrb.Intern("to_i")) {
		iv, err := mrb.Funcall(v, mrb.Intern("to_i"))
		if err == nil && iv.IsInt() {
			return big.NewInt(iv.Int64()), nil
		}
		if err == nil && mrb.RespondTo(iv, mrb.Intern("to_s")) {
			if sv, err2 := mrb.Funcall(iv, mrb.Intern("to_s")); err2 == nil {
				if bi, ok := new(big.Int).SetString(sv.String(), 10); ok {
					return bi, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("cannot convert %s to OpenSSL::BN", mrb.ClassOf(v).Name())
}

// -- OpenSSL::PKey::EC --------------------------------------------------------

type ecKey struct {
	priv      *ecdsa.PrivateKey
	pub       *ecdsa.PublicKey
	curve     elliptic.Curve
	curveName string
}

type ecGroup struct {
	curve     elliptic.Curve
	curveName string
}

type ecPoint struct {
	curve     elliptic.Curve
	curveName string
	x, y      *big.Int // nil = point at infinity
}

func lookupCurve(name string) (elliptic.Curve, string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "prime256v1", "secp256r1", "p-256", "p256", "nistp256":
		return elliptic.P256(), "prime256v1", nil
	case "secp384r1", "p-384", "p384", "nistp384":
		return elliptic.P384(), "secp384r1", nil
	case "secp521r1", "p-521", "p521", "nistp521":
		return elliptic.P521(), "secp521r1", nil
	case "secp224r1", "p-224", "p224", "nistp224":
		return elliptic.P224(), "secp224r1", nil
	default:
		return nil, "", fmt.Errorf("unknown curve name: %s", name)
	}
}

func canonicalCurveName(c elliptic.Curve) string {
	switch c {
	case elliptic.P256():
		return "prime256v1"
	case elliptic.P384():
		return "secp384r1"
	case elliptic.P521():
		return "secp521r1"
	case elliptic.P224():
		return "secp224r1"
	default:
		if c != nil && c.Params() != nil {
			return c.Params().Name
		}
		return ""
	}
}

func cryptoHashFromRuby(mrb *oruby.MrbState, v oruby.Value) (crypto.Hash, error) {
	name := ""
	if v.IsString() {
		name = v.String()
	} else if mrb.RespondTo(v, oruby.Sym_name) {
		if nv, err := mrb.Funcall(v, oruby.Sym_name); err == nil {
			name = nv.String()
		}
	}
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "")) {
	case "MD5":
		return crypto.MD5, nil
	case "SHA1":
		return crypto.SHA1, nil
	case "SHA224":
		return crypto.SHA224, nil
	case "SHA256":
		return crypto.SHA256, nil
	case "SHA384":
		return crypto.SHA384, nil
	case "SHA512":
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("unsupported digest: %s", name)
	}
}

func digestBytes(h crypto.Hash, data []byte) []byte {
	hasher := h.New()
	hasher.Write(data)
	return hasher.Sum(nil)
}

func x509SigAlgoForKey(digestName string, key any) x509.SignatureAlgorithm {
	if _, isEC := key.(*ecdsa.PrivateKey); isEC {
		switch strings.ToUpper(strings.ReplaceAll(digestName, "-", "")) {
		case "SHA384":
			return x509.ECDSAWithSHA384
		case "SHA512":
			return x509.ECDSAWithSHA512
		default:
			return x509.ECDSAWithSHA256
		}
	}
	return x509SigAlgo(digestName)
}

func encodeUncompressedPoint(c elliptic.Curve, x, y *big.Int) []byte {
	if x == nil || y == nil {
		return []byte{0}
	}
	byteLen := (c.Params().BitSize + 7) / 8
	out := make([]byte, 1+2*byteLen)
	out[0] = 4
	x.FillBytes(out[1 : 1+byteLen])
	y.FillBytes(out[1+byteLen:])
	return out
}

func decodePointBytes(c elliptic.Curve, data []byte) (*big.Int, *big.Int, error) {
	if len(data) == 1 && data[0] == 0 {
		return nil, nil, nil
	}
	byteLen := (c.Params().BitSize + 7) / 8
	if len(data) == 1+2*byteLen && data[0] == 4 {
		x := new(big.Int).SetBytes(data[1 : 1+byteLen])
		y := new(big.Int).SetBytes(data[1+byteLen:])
		if !c.IsOnCurve(x, y) {
			return nil, nil, errors.New("point is not on curve")
		}
		return x, y, nil
	}
	// Compressed point (0x02 or 0x03)
	if len(data) == 1+byteLen && (data[0] == 2 || data[0] == 3) {
		x, y := elliptic.UnmarshalCompressed(c, data)
		if x == nil {
			return nil, nil, errors.New("invalid compressed EC point")
		}
		return x, y, nil
	}
	return nil, nil, errors.New("invalid EC point encoding")
}

// -- OpenSSL::SSL::SSLContext & SSLSocket -------------------------------------

type osslSSLContext struct {
	verifyMode     int
	verifyHostname bool
	cert           *x509Cert
	extraChain     []*x509Cert
	key            any // *rsa.PrivateKey or *ecdsa.PrivateKey
	caFile         string
	caPath         string
	certStore      *x509Store
	minVersion     uint16
	maxVersion     uint16
	alpnProtocols  []string
	options        int64
}

func (ctx *osslSSLContext) buildTLSConfig(serverName string, isServer bool) (*tls.Config, error) {
	cfg := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: ctx.verifyMode == 0,
		MinVersion:         ctx.minVersion,
		MaxVersion:         ctx.maxVersion,
		NextProtos:         append([]string(nil), ctx.alpnProtocols...),
	}
	if cfg.MinVersion == 0 {
		cfg.MinVersion = tls.VersionTLS12
	}

	if ctx.cert != nil && ctx.key != nil {
		if len(ctx.cert.rawDER) == 0 {
			return nil, errors.New("certificate not signed")
		}
		chain := [][]byte{ctx.cert.rawDER}
		for _, ec := range ctx.extraChain {
			if ec != nil && len(ec.rawDER) > 0 {
				chain = append(chain, ec.rawDER)
			}
		}
		cfg.Certificates = []tls.Certificate{{
			Certificate: chain,
			PrivateKey:  ctx.key,
			Leaf:        ctx.cert.parsed,
		}}
	}

	var pool *x509.CertPool
	ensurePool := func() {
		if pool == nil {
			pool = x509.NewCertPool()
		}
	}
	if ctx.certStore != nil && (len(ctx.certStore.certs) > 0 || ctx.certStore.systemRoots) {
		pool = ctx.certStore.rootPool()
	}
	if ctx.caFile != "" {
		pemBytes, err := os.ReadFile(ctx.caFile)
		if err != nil {
			return nil, err
		}
		ensurePool()
		pool.AppendCertsFromPEM(pemBytes)
	}
	if ctx.caPath != "" {
		entries, err := os.ReadDir(ctx.caPath)
		if err == nil {
			ensurePool()
			for _, e := range entries {
				if pemBytes, err := os.ReadFile(ctx.caPath + "/" + e.Name()); err == nil {
					pool.AppendCertsFromPEM(pemBytes)
				}
			}
		}
	}
	if pool != nil {
		if isServer {
			cfg.ClientCAs = pool
		} else {
			cfg.RootCAs = pool
		}
	}
	if isServer {
		if ctx.verifyMode&2 != 0 { // VERIFY_FAIL_IF_NO_PEER_CERT
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else if ctx.verifyMode&1 != 0 { // VERIFY_PEER
			cfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}
	return cfg, nil
}

type osslSSLSocket struct {
	ioVal     oruby.Value
	ctx       *osslSSLContext
	hostname  string
	syncClose bool
	rawConn   net.Conn
	tlsConn   *tls.Conn
	reader    *bufio.Reader
	closed    bool
	eof       bool
	verifyRes int
}

func (s *osslSSLSocket) ensureRawConn(mrb *oruby.MrbState) error {
	if s.rawConn != nil {
		return nil
	}
	var fdVal oruby.Value
	var err error
	if mrb.RespondTo(s.ioVal, mrb.Intern("fileno")) {
		fdVal, err = mrb.Funcall(s.ioVal, mrb.Intern("fileno"))
	} else if mrb.RespondTo(s.ioVal, mrb.Intern("to_i")) {
		fdVal, err = mrb.Funcall(s.ioVal, mrb.Intern("to_i"))
	} else {
		return errors.New("underlying IO does not respond to fileno")
	}
	if err != nil {
		return err
	}
	origFd := int(fdVal.Int64())
	dupFd, err := syscall.Dup(origFd)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(dupFd), "openssl-tls")
	conn, err := net.FileConn(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	s.rawConn = conn
	return nil
}

func rubyTLSVersionToGo(mrb *oruby.MrbState, v oruby.Value) uint16 {
	if v.IsNil() {
		return 0
	}
	if v.IsInt() {
		switch v.Int64() {
		case 0x301:
			return tls.VersionTLS10
		case 0x302:
			return tls.VersionTLS11
		case 0x303:
			return tls.VersionTLS12
		case 0x304:
			return tls.VersionTLS13
		}
	}
	var raw string
	switch {
	case v.IsSymbol():
		raw = mrb.SymName(v.Symbol())
	case v.IsString():
		raw = v.String()
	default:
		return 0
	}
	s := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(raw, "_", ""), ".", ""))
	switch s {
	case "TLS1", "TLS10", "TLSV1", "TLSV10":
		return tls.VersionTLS10
	case "TLS11", "TLSV11":
		return tls.VersionTLS11
	case "TLS12", "TLSV12":
		return tls.VersionTLS12
	case "TLS13", "TLSV13":
		return tls.VersionTLS13
	}
	return 0
}

func goTLSVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLSv1"
	case tls.VersionTLS11:
		return "TLSv1.1"
	case tls.VersionTLS12:
		return "TLSv1.2"
	case tls.VersionTLS13:
		return "TLSv1.3"
	default:
		return "UNKNOWN"
	}
}

func wrapX509Cert(mrb *oruby.MrbState, parsed *x509.Certificate) oruby.Value {
	if parsed == nil {
		return mrb.NilValue()
	}
	cls := rb.ClassPath(mrb, "OpenSSL::X509::Certificate")
	obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
	c := &x509Cert{
		version:   parsed.Version - 1,
		serial:    parsed.SerialNumber,
		notBefore: parsed.NotBefore,
		notAfter:  parsed.NotAfter,
		pubKey:    parsed.PublicKey,
		rawDER:    parsed.Raw,
		parsed:    parsed,
	}
	rb.Set(mrb, obj.Value(), c)
	return obj.Value()
}

// initOpenSSLExt registers OpenSSL::BN, OpenSSL::PKey.read, RSA sign/verify,
// OpenSSL::PKey::EC (+ Group, Point), and OpenSSL::SSL::{SSLContext, SSLSocket}.
func initOpenSSLExt(mrb *oruby.MrbState, mod, osslErr, pkeyMod, pkeyErr, pkeyBase, rsaCls, certCls oruby.RClass) {
	initOpenSSLBN(mrb, mod, osslErr)
	initOpenSSLPKeyExt(mrb, pkeyMod, pkeyErr, pkeyBase, rsaCls)
	initOpenSSLSSL(mrb, mod, osslErr, certCls)
}

func initOpenSSLBN(mrb *oruby.MrbState, mod, osslErr oruby.RClass) {
	bnErr := mrb.DefineClassUnder(mod, "BNError", osslErr)
	bnCls := rb.DataClass(mrb.DefineClassUnder(mod, "BN", mrb.ObjectClass()))
	mrb.IncludeModule(bnCls, mrb.ModuleGet("Comparable"))

	bnCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if len(args) == 0 {
			return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1..2)")
		}
		arg := args[0]
		base := 10
		if len(args) >= 2 && !args[1].IsNil() {
			base = int(args[1].Int64())
		}
		if bn, ok := rb.Get[*osslBN](mrb, arg); ok && bn.n != nil {
			rb.Set(mrb, self, &osslBN{n: new(big.Int).Set(bn.n)})
			return self
		}
		if arg.IsInt() {
			rb.Set(mrb, self, &osslBN{n: big.NewInt(arg.Int64())})
			return self
		}
		if arg.IsString() {
			raw := arg.Bytes()
			switch base {
			case 2:
				// OpenSSL::BN.new(bin, 2) interprets raw big-endian unsigned bytes
				n := new(big.Int).SetBytes(raw)
				rb.Set(mrb, self, &osslBN{n: n})
				return self
			case 0:
				// MPI format: 4-byte big-endian length + magnitude (MSB=sign)
				if len(raw) < 4 {
					return mrb.Raise(bnErr, "invalid MPI")
				}
				ln := int(binary.BigEndian.Uint32(raw[:4]))
				if len(raw) < 4+ln {
					return mrb.Raise(bnErr, "truncated MPI")
				}
				body := append([]byte(nil), raw[4:4+ln]...)
				neg := false
				if len(body) > 0 && body[0]&0x80 != 0 {
					neg = true
					body[0] &^= 0x80
				}
				n := new(big.Int).SetBytes(body)
				if neg {
					n.Neg(n)
				}
				rb.Set(mrb, self, &osslBN{n: n})
				return self
			case 10, 16:
				n, ok := new(big.Int).SetString(strings.TrimSpace(string(raw)), base)
				if !ok {
					return mrb.Raise(bnErr, "invalid BN string")
				}
				rb.Set(mrb, self, &osslBN{n: n})
				return self
			default:
				return mrb.Raise(mrb.EArgumentError(), fmt.Sprintf("invalid radix %d", base))
			}
		}
		// Fallback for Bignum/Integer-like objects via to_s
		if mrb.RespondTo(arg, mrb.Intern("to_i")) {
			if iv, err := mrb.Funcall(arg, mrb.Intern("to_i")); err == nil {
				if sv, err2 := mrb.Funcall(iv, mrb.Intern("to_s")); err2 == nil {
					if n, ok := new(big.Int).SetString(sv.String(), 10); ok {
						rb.Set(mrb, self, &osslBN{n: n})
						return self
					}
				}
			}
		}
		return mrb.Raise(mrb.ETypeError(), "Cannot convert into OpenSSL::BN")
	}, mrb.ArgsArg(1, 1))

	getBN := func(mrb *oruby.MrbState, self oruby.Value) *big.Int {
		bn, ok := rb.Get[*osslBN](mrb, self)
		if !ok || bn.n == nil {
			return big.NewInt(0)
		}
		return bn.n
	}

	bnCls.DefineMethod("to_s", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n := getBN(mrb, self)
		base := 10
		if args := mrb.GetArgs().Slice(); len(args) >= 1 && !args[0].IsNil() {
			base = int(args[0].Int64())
		}
		switch base {
		case 10:
			return mrb.StrNew(n.Text(10))
		case 16:
			return mrb.StrNew(strings.ToUpper(n.Text(16)))
		case 2:
			b := n.Bytes()
			return mrb.Call(mrb.BytesValue(b), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		case 0:
			b := n.Bytes()
			if len(b) > 0 && b[0]&0x80 != 0 {
				b = append([]byte{0}, b...)
			}
			if n.Sign() < 0 && len(b) > 0 {
				b[0] |= 0x80
			}
			out := make([]byte, 4+len(b))
			binary.BigEndian.PutUint32(out[:4], uint32(len(b)))
			copy(out[4:], b)
			return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		default:
			return mrb.Raise(mrb.EArgumentError(), fmt.Sprintf("invalid radix %d", base))
		}
	}, mrb.ArgsOpt(1))

	bnCls.DefineMethod("to_i", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n := getBN(mrb, self)
		if n.IsInt64() {
			return mrb.Int64Value(n.Int64())
		}
		v, err := mrb.Funcall(mrb.StrNew(n.Text(10)), mrb.Intern("to_i"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return v
	}, mrb.ArgsNone())
	bnCls.DefineAlias("to_int", "to_i")

	bnCls.DefineMethod("to_bn", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return self
	}, mrb.ArgsNone())

	bnCls.DefineMethod("num_bits", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getBN(mrb, self).BitLen())
	}, mrb.ArgsNone())

	bnCls.DefineMethod("num_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue((getBN(mrb, self).BitLen() + 7) / 8)
	}, mrb.ArgsNone())

	bnCls.DefineMethod("bit_set?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		idx := int(mrb.GetArgsFirst().Int64())
		if idx < 0 {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(getBN(mrb, self).Bit(idx) == 1)
	}, mrb.ArgsReq(1))

	bnCls.DefineMethod("zero?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getBN(mrb, self).Sign() == 0)
	}, mrb.ArgsNone())

	bnCls.DefineMethod("one?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getBN(mrb, self).Cmp(big.NewInt(1)) == 0)
	}, mrb.ArgsNone())

	bnCls.DefineMethod("negative?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getBN(mrb, self).Sign() < 0)
	}, mrb.ArgsNone())

	bnCls.DefineMethod("odd?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getBN(mrb, self).Bit(0) == 1)
	}, mrb.ArgsNone())

	bnCls.DefineMethod("<=>", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		other, err := coerceToBigInt(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.NilValue()
		}
		return mrb.FixnumValue(getBN(mrb, self).Cmp(other))
	}, mrb.ArgsReq(1))

	bnCls.DefineMethod("==", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		other, err := coerceToBigInt(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(getBN(mrb, self).Cmp(other) == 0)
	}, mrb.ArgsReq(1))
	bnCls.DefineAlias("eql?", "==")

	binOp := func(name string, op func(a, b *big.Int) (*big.Int, error)) {
		bnCls.DefineMethod(name, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			other, err := coerceToBigInt(mrb, mrb.GetArgsFirst())
			if err != nil {
				return mrb.Raise(mrb.ETypeError(), err.Error())
			}
			res, err := op(getBN(mrb, self), other)
			if err != nil {
				return mrb.Raise(bnErr, err.Error())
			}
			return newBNValue(mrb, res)
		}, mrb.ArgsReq(1))
	}
	binOp("+", func(a, b *big.Int) (*big.Int, error) { return new(big.Int).Add(a, b), nil })
	binOp("-", func(a, b *big.Int) (*big.Int, error) { return new(big.Int).Sub(a, b), nil })
	binOp("*", func(a, b *big.Int) (*big.Int, error) { return new(big.Int).Mul(a, b), nil })
	binOp("%", func(a, b *big.Int) (*big.Int, error) {
		if b.Sign() == 0 {
			return nil, errors.New("division by zero")
		}
		return new(big.Int).Mod(a, b), nil
	})
	binOp("**", func(a, b *big.Int) (*big.Int, error) { return new(big.Int).Exp(a, b, nil), nil })
	binOp("gcd", func(a, b *big.Int) (*big.Int, error) { return new(big.Int).GCD(nil, nil, a, b), nil })
	binOp("mod_inverse", func(a, b *big.Int) (*big.Int, error) {
		r := new(big.Int).ModInverse(a, b)
		if r == nil {
			return nil, errors.New("no modular inverse")
		}
		return r, nil
	})

	bnCls.DefineMethod("/", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		other, err := coerceToBigInt(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		if other.Sign() == 0 {
			return mrb.Raise(bnErr, "division by zero")
		}
		q, r := new(big.Int).QuoRem(getBN(mrb, self), other, new(big.Int))
		ary := mrb.AryNewCapa(2)
		mrb.AryPush(ary, newBNValue(mrb, q))
		mrb.AryPush(ary, newBNValue(mrb, r))
		return ary
	}, mrb.ArgsReq(1))

	bnCls.DefineMethod("<<", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		shift := uint(mrb.GetArgsFirst().Int64())
		return newBNValue(mrb, new(big.Int).Lsh(getBN(mrb, self), shift))
	}, mrb.ArgsReq(1))

	bnCls.DefineMethod(">>", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		shift := uint(mrb.GetArgsFirst().Int64())
		return newBNValue(mrb, new(big.Int).Rsh(getBN(mrb, self), shift))
	}, mrb.ArgsReq(1))

	bnCls.DefineMethod("mod_exp", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		exp, err := coerceToBigInt(mrb, args[0])
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		modn, err := coerceToBigInt(mrb, args[1])
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		return newBNValue(mrb, new(big.Int).Exp(getBN(mrb, self), exp, modn))
	}, mrb.ArgsReq(2))

	bnCls.DefineMethod("prime?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getBN(mrb, self).ProbablyPrime(20))
	}, mrb.ArgsOpt(2))

	bnCls.DefineClassMethod("rand", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		bits := int(mrb.GetArgsFirst().Int64())
		if bits < 0 {
			return mrb.Raise(mrb.EArgumentError(), "bits must be non-negative")
		}
		if bits == 0 {
			return newBNValue(mrb, big.NewInt(0))
		}
		max := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		r, err := rand.Int(rand.Reader, max)
		if err != nil {
			return mrb.Raise(bnErr, err.Error())
		}
		return newBNValue(mrb, r)
	}, mrb.ArgsArg(1, 2))

	bnCls.DefineClassMethod("generate_prime", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		bits := int(mrb.GetArgsFirst().Int64())
		if bits < 2 {
			return mrb.Raise(bnErr, "bits must be >= 2")
		}
		var p *big.Int
		var err error
		mrb.Blocking(func() {
			p, err = rand.Prime(rand.Reader, bits)
		})
		if err != nil {
			return mrb.Raise(bnErr, err.Error())
		}
		return newBNValue(mrb, p)
	}, mrb.ArgsArg(1, 3))
}

func initOpenSSLPKeyExt(mrb *oruby.MrbState, pkeyMod, pkeyErr, pkeyBase, rsaCls oruby.RClass) {
	// -- RSA extra methods: sign, verify, sign_pss, verify_pss, public_encrypt, private_decrypt, n/e/d --
	rsaCls.DefineMethod("n", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.NilValue()
		}
		return newBNValue(mrb, k.pub.N)
	}, mrb.ArgsNone())

	rsaCls.DefineMethod("e", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.NilValue()
		}
		return newBNValue(mrb, big.NewInt(int64(k.pub.E)))
	}, mrb.ArgsNone())

	rsaCls.DefineMethod("d", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.priv == nil {
			return mrb.NilValue()
		}
		return newBNValue(mrb, k.priv.D)
	}, mrb.ArgsNone())

	rsaCls.DefineMethod("sign", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.priv == nil {
			return mrb.Raise(pkeyErr, "private key is needed")
		}
		args := mrb.GetArgs().Slice()
		h, err := cryptoHashFromRuby(mrb, args[0])
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		data, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		sig, err := rsa.SignPKCS1v15(rand.Reader, k.priv, h, digestBytes(h, data))
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(sig), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(2))

	rsaCls.DefineMethod("verify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(pkeyErr, "public key is needed")
		}
		args := mrb.GetArgs().Slice()
		h, err := cryptoHashFromRuby(mrb, args[0])
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		sig, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		data, err := rb.Bytes(mrb, args[2])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		err = rsa.VerifyPKCS1v15(k.pub, h, digestBytes(h, data), sig)
		return mrb.BoolValue(err == nil)
	}, mrb.ArgsReq(3))

	rsaCls.DefineMethod("sign_pss", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.priv == nil {
			return mrb.Raise(pkeyErr, "private key is needed")
		}
		args := mrb.GetArgs().Slice()
		h, err := cryptoHashFromRuby(mrb, args[0])
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		data, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		sig, err := rsa.SignPSS(rand.Reader, k.priv, h, digestBytes(h, data), &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(sig), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(2)|mrb.ArgsKey(0, 1))

	rsaCls.DefineMethod("verify_pss", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(pkeyErr, "public key is needed")
		}
		args := mrb.GetArgs().Slice()
		h, err := cryptoHashFromRuby(mrb, args[0])
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		sig, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		data, err := rb.Bytes(mrb, args[2])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		err = rsa.VerifyPSS(k.pub, h, digestBytes(h, data), sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto})
		return mrb.BoolValue(err == nil)
	}, mrb.ArgsReq(3)|mrb.ArgsKey(0, 1))

	rsaCls.DefineMethod("public_to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(pkeyErr, "RSA key not initialized")
		}
		der, err := x509.MarshalPKIXPublicKey(k.pub)
		if err != nil {
			return mrb.Raise(pkeyErr, err.Error())
		}
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	}, mrb.ArgsNone())
	rsaCls.DefineAlias("to_s", "to_pem")
	rsaCls.DefineAlias("export", "to_pem")
	rsaCls.DefineMethod("to_text", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*rsaKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(pkeyErr, "RSA key not initialized")
		}
		bits := k.pub.N.BitLen()
		if k.priv != nil {
			return mrb.StrNew(fmt.Sprintf("RSA Private-Key: (%d bit, 2 primes)\nmodulus: %x\npublicExponent: %d\n", bits, k.pub.N, k.pub.E))
		}
		return mrb.StrNew(fmt.Sprintf("RSA Public-Key: (%d bit)\nModulus: %x\nExponent: %d\n", bits, k.pub.N, k.pub.E))
	}, mrb.ArgsNone())

	// -- OpenSSL::PKey::EC, EC::Group, EC::Point -------------------------------
	ecCls := rb.DataClass(mrb.DefineClassUnder(pkeyMod, "EC", pkeyBase))
	ecErr := mrb.DefineClassUnder(pkeyMod, "ECError", pkeyErr)
	groupCls := rb.DataClass(mrb.DefineClassUnder(ecCls, "Group", mrb.ObjectClass()))
	pointCls := rb.DataClass(mrb.DefineClassUnder(ecCls, "Point", mrb.ObjectClass()))
	mrb.DefineClassUnder(groupCls, "Error", pkeyErr)
	mrb.DefineClassUnder(pointCls, "Error", pkeyErr)

	wrapGroup := func(mrb *oruby.MrbState, c elliptic.Curve, name string) oruby.Value {
		obj := mrb.ObjAlloc(oruby.MrbTTCData, groupCls)
		rb.Set(mrb, obj.Value(), &ecGroup{curve: c, curveName: name})
		return obj.Value()
	}
	wrapPoint := func(mrb *oruby.MrbState, c elliptic.Curve, name string, x, y *big.Int) oruby.Value {
		obj := mrb.ObjAlloc(oruby.MrbTTCData, pointCls)
		var xc, yc *big.Int
		if x != nil && y != nil {
			xc = new(big.Int).Set(x)
			yc = new(big.Int).Set(y)
		}
		rb.Set(mrb, obj.Value(), &ecPoint{curve: c, curveName: name, x: xc, y: yc})
		return obj.Value()
	}

	// EC::Group
	groupCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		arg := mrb.GetArgsFirst()
		if g, ok := rb.Get[*ecGroup](mrb, arg); ok {
			rb.Set(mrb, self, &ecGroup{curve: g.curve, curveName: g.curveName})
			return self
		}
		c, name, err := lookupCurve(arg.String())
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		rb.Set(mrb, self, &ecGroup{curve: c, curveName: name})
		return self
	}, mrb.ArgsReq(1))

	groupCls.DefineMethod("curve_name", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := rb.Get[*ecGroup](mrb, self)
		if g == nil {
			return mrb.NilValue()
		}
		return mrb.StrNew(g.curveName)
	}, mrb.ArgsNone())

	groupCls.DefineMethod("degree", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := rb.Get[*ecGroup](mrb, self)
		if g == nil || g.curve == nil {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(g.curve.Params().BitSize)
	}, mrb.ArgsNone())

	groupCls.DefineMethod("order", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := rb.Get[*ecGroup](mrb, self)
		if g == nil || g.curve == nil {
			return mrb.NilValue()
		}
		return newBNValue(mrb, g.curve.Params().N)
	}, mrb.ArgsNone())

	groupCls.DefineMethod("generator", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := rb.Get[*ecGroup](mrb, self)
		if g == nil || g.curve == nil {
			return mrb.NilValue()
		}
		p := g.curve.Params()
		return wrapPoint(mrb, g.curve, g.curveName, p.Gx, p.Gy)
	}, mrb.ArgsNone())

	groupCls.DefineMethod("==", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g1, ok1 := rb.Get[*ecGroup](mrb, self)
		g2, ok2 := rb.Get[*ecGroup](mrb, mrb.GetArgsFirst())
		return mrb.BoolValue(ok1 && ok2 && g1.curve == g2.curve)
	}, mrb.ArgsReq(1))
	groupCls.DefineAlias("eql?", "==")

	// EC::Point
	pointCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		g, ok := rb.Get[*ecGroup](mrb, args[0])
		if !ok {
			if p, ok2 := rb.Get[*ecPoint](mrb, args[0]); ok2 {
				rb.Set(mrb, self, &ecPoint{curve: p.curve, curveName: p.curveName, x: new(big.Int).Set(p.x), y: new(big.Int).Set(p.y)})
				return self
			}
			return mrb.Raise(mrb.ETypeError(), "expected OpenSSL::PKey::EC::Group")
		}
		if len(args) == 1 {
			rb.Set(mrb, self, &ecPoint{curve: g.curve, curveName: g.curveName})
			return self
		}
		var raw []byte
		if bn, ok := rb.Get[*osslBN](mrb, args[1]); ok && bn.n != nil {
			raw = bn.n.Bytes()
		} else if args[1].IsString() {
			raw = args[1].Bytes()
		} else {
			return mrb.Raise(mrb.ETypeError(), "expected OpenSSL::BN or String")
		}
		x, y, err := decodePointBytes(g.curve, raw)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		rb.Set(mrb, self, &ecPoint{curve: g.curve, curveName: g.curveName, x: x, y: y})
		return self
	}, mrb.ArgsArg(1, 1))

	pointCls.DefineMethod("group", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := rb.Get[*ecPoint](mrb, self)
		if p == nil {
			return mrb.NilValue()
		}
		return wrapGroup(mrb, p.curve, p.curveName)
	}, mrb.ArgsNone())

	pointCls.DefineMethod("infinity?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := rb.Get[*ecPoint](mrb, self)
		return mrb.BoolValue(p == nil || p.x == nil || p.y == nil)
	}, mrb.ArgsNone())

	pointCls.DefineMethod("on_curve?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := rb.Get[*ecPoint](mrb, self)
		if p == nil || p.x == nil || p.y == nil {
			return mrb.TrueValue()
		}
		return mrb.BoolValue(p.curve.IsOnCurve(p.x, p.y))
	}, mrb.ArgsNone())

	pointCls.DefineMethod("to_octet_string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := rb.Get[*ecPoint](mrb, self)
		if p == nil {
			return mrb.Raise(ecErr, "point not initialized")
		}
		form := "uncompressed"
		if args := mrb.GetArgs().Slice(); len(args) >= 1 && !args[0].IsNil() {
			form = mrb.String(args[0])
		}
		var out []byte
		if form == "compressed" && p.x != nil && p.y != nil {
			out = elliptic.MarshalCompressed(p.curve, p.x, p.y)
		} else {
			out = encodeUncompressedPoint(p.curve, p.x, p.y)
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsOpt(1))

	pointCls.DefineMethod("to_bn", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := rb.Get[*ecPoint](mrb, self)
		if p == nil {
			return mrb.Raise(ecErr, "point not initialized")
		}
		out := encodeUncompressedPoint(p.curve, p.x, p.y)
		return newBNValue(mrb, new(big.Int).SetBytes(out))
	}, mrb.ArgsOpt(1))

	pointCls.DefineMethod("==", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p1, ok1 := rb.Get[*ecPoint](mrb, self)
		p2, ok2 := rb.Get[*ecPoint](mrb, mrb.GetArgsFirst())
		if !ok1 || !ok2 || p1.curve != p2.curve {
			return mrb.FalseValue()
		}
		if p1.x == nil || p2.x == nil {
			return mrb.BoolValue(p1.x == nil && p2.x == nil)
		}
		return mrb.BoolValue(p1.x.Cmp(p2.x) == 0 && p1.y.Cmp(p2.y) == 0)
	}, mrb.ArgsReq(1))
	pointCls.DefineAlias("eql?", "==")

	// EC key helpers
	parseECBytes := func(b []byte) (*ecKey, error) {
		der := b
		if block, _ := pem.Decode(b); block != nil {
			der = block.Bytes
		}
		if priv, err := x509.ParseECPrivateKey(der); err == nil {
			return &ecKey{priv: priv, pub: &priv.PublicKey, curve: priv.Curve, curveName: canonicalCurveName(priv.Curve)}, nil
		}
		if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
			if priv, ok := k.(*ecdsa.PrivateKey); ok {
				return &ecKey{priv: priv, pub: &priv.PublicKey, curve: priv.Curve, curveName: canonicalCurveName(priv.Curve)}, nil
			}
		}
		if k, err := x509.ParsePKIXPublicKey(der); err == nil {
			if pub, ok := k.(*ecdsa.PublicKey); ok {
				return &ecKey{pub: pub, curve: pub.Curve, curveName: canonicalCurveName(pub.Curve)}, nil
			}
		}
		return nil, errors.New("Neither PUB key nor PRIV key")
	}

	ecCls.DefineClassMethod("generate", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		arg := mrb.GetArgsFirst()
		var c elliptic.Curve
		var name string
		if g, ok := rb.Get[*ecGroup](mrb, arg); ok {
			c, name = g.curve, g.curveName
		} else {
			var err error
			c, name, err = lookupCurve(arg.String())
			if err != nil {
				return mrb.Raise(ecErr, err.Error())
			}
		}
		priv, err := ecdsa.GenerateKey(c, rand.Reader)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, ecCls)
		rb.Set(mrb, obj.Value(), &ecKey{priv: priv, pub: &priv.PublicKey, curve: c, curveName: name})
		return obj
	}, mrb.ArgsReq(1))

	ecCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if len(args) == 0 {
			rb.Set(mrb, self, &ecKey{})
			return self
		}
		if other, ok := rb.Get[*ecKey](mrb, args[0]); ok {
			cp := *other
			rb.Set(mrb, self, &cp)
			return self
		}
		if g, ok := rb.Get[*ecGroup](mrb, args[0]); ok {
			rb.Set(mrb, self, &ecKey{curve: g.curve, curveName: g.curveName})
			return self
		}
		b, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		// Try curve name first if short ASCII identifier without PEM header
		if len(b) < 32 && !strings.Contains(string(b), "-----") {
			if c, name, err := lookupCurve(string(b)); err == nil {
				rb.Set(mrb, self, &ecKey{curve: c, curveName: name})
				return self
			}
		}
		ek, err := parseECBytes(b)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		rb.Set(mrb, self, ek)
		return self
	}, mrb.ArgsArg(0, 2))

	ecCls.DefineMethod("generate_key!", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.curve == nil {
			return mrb.Raise(ecErr, "group is not set")
		}
		priv, err := ecdsa.GenerateKey(k.curve, rand.Reader)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		k.priv = priv
		k.pub = &priv.PublicKey
		return self
	}, mrb.ArgsNone())
	ecCls.DefineAlias("generate_key", "generate_key!")

	ecCls.DefineMethod("group", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.curve == nil {
			return mrb.NilValue()
		}
		return wrapGroup(mrb, k.curve, k.curveName)
	}, mrb.ArgsNone())

	ecCls.DefineMethod("private?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		return mrb.BoolValue(ok && k.priv != nil)
	}, mrb.ArgsNone())
	ecCls.DefineAlias("private_key?", "private?")

	ecCls.DefineMethod("public?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		return mrb.BoolValue(ok && k.pub != nil)
	}, mrb.ArgsNone())
	ecCls.DefineAlias("public_key?", "public?")

	ecCls.DefineMethod("private_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.priv == nil {
			return mrb.NilValue()
		}
		return newBNValue(mrb, k.priv.D)
	}, mrb.ArgsNone())

	ecCls.DefineMethod("public_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.NilValue()
		}
		return wrapPoint(mrb, k.curve, k.curveName, k.pub.X, k.pub.Y)
	}, mrb.ArgsNone())

	ecCls.DefineMethod("check_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.pub == nil || !k.curve.IsOnCurve(k.pub.X, k.pub.Y) {
			return mrb.Raise(ecErr, "invalid EC key")
		}
		return mrb.TrueValue()
	}, mrb.ArgsNone())

	ecCls.DefineMethod("sign", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.priv == nil {
			return mrb.Raise(ecErr, "private key is needed")
		}
		args := mrb.GetArgs().Slice()
		h, err := cryptoHashFromRuby(mrb, args[0])
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		data, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digestBytes(h, data))
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(sig), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(2))

	ecCls.DefineMethod("verify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(ecErr, "public key is needed")
		}
		args := mrb.GetArgs().Slice()
		h, err := cryptoHashFromRuby(mrb, args[0])
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		sig, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		data, err := rb.Bytes(mrb, args[2])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		return mrb.BoolValue(ecdsa.VerifyASN1(k.pub, digestBytes(h, data), sig))
	}, mrb.ArgsReq(3))

	ecCls.DefineMethod("dsa_sign_asn1", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.priv == nil {
			return mrb.Raise(ecErr, "private key is needed")
		}
		digest, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(sig), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	ecCls.DefineMethod("dsa_verify_asn1", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(ecErr, "public key is needed")
		}
		args := mrb.GetArgs().Slice()
		digest, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		sig, err := rb.Bytes(mrb, args[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		return mrb.BoolValue(ecdsa.VerifyASN1(k.pub, digest, sig))
	}, mrb.ArgsReq(2))

	ecCls.DefineMethod("dh_compute_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.priv == nil {
			return mrb.Raise(ecErr, "private key is needed")
		}
		peerPt, ok := rb.Get[*ecPoint](mrb, mrb.GetArgsFirst())
		if !ok || peerPt.x == nil || peerPt.y == nil {
			return mrb.Raise(ecErr, "expected OpenSSL::PKey::EC::Point")
		}
		privECDH, err := k.priv.ECDH()
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		pubECDH, err := privECDH.Curve().NewPublicKey(encodeUncompressedPoint(peerPt.curve, peerPt.x, peerPt.y))
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		secret, err := privECDH.ECDH(pubECDH)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(secret), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	ecCls.DefineMethod("to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok {
			return mrb.Raise(ecErr, "EC key not initialized")
		}
		var der []byte
		var err error
		if k.priv != nil {
			der, err = x509.MarshalECPrivateKey(k.priv)
		} else if k.pub != nil {
			der, err = x509.MarshalPKIXPublicKey(k.pub)
		} else {
			return mrb.Raise(ecErr, "empty EC key")
		}
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	ecCls.DefineMethod("to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok {
			return mrb.Raise(ecErr, "EC key not initialized")
		}
		if k.priv != nil {
			der, err := x509.MarshalECPrivateKey(k.priv)
			if err != nil {
				return mrb.Raise(ecErr, err.Error())
			}
			return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})))
		}
		if k.pub != nil {
			der, err := x509.MarshalPKIXPublicKey(k.pub)
			if err != nil {
				return mrb.Raise(ecErr, err.Error())
			}
			return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
		}
		return mrb.Raise(ecErr, "empty EC key")
	}, mrb.ArgsNone())
	ecCls.DefineAlias("to_s", "to_pem")
	ecCls.DefineAlias("export", "to_pem")

	ecCls.DefineMethod("public_to_der", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(ecErr, "public key not initialized")
		}
		der, err := x509.MarshalPKIXPublicKey(k.pub)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	ecCls.DefineMethod("public_to_pem", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		k, ok := rb.Get[*ecKey](mrb, self)
		if !ok || k.pub == nil {
			return mrb.Raise(ecErr, "public key not initialized")
		}
		der, err := x509.MarshalPKIXPublicKey(k.pub)
		if err != nil {
			return mrb.Raise(ecErr, err.Error())
		}
		return mrb.StrNew(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	}, mrb.ArgsNone())

	// -- OpenSSL::PKey.read(pem_or_der, pass = nil) ----------------------------
	pkeyMod.DefineClassMethod("read", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		b, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		der := b
		if block, _ := pem.Decode(b); block != nil {
			der = block.Bytes
		}
		if priv, err := x509.ParsePKCS1PrivateKey(der); err == nil {
			obj := mrb.ObjAlloc(oruby.MrbTTCData, rsaCls)
			rb.Set(mrb, obj.Value(), &rsaKey{priv: priv, pub: &priv.PublicKey})
			return obj
		}
		if priv, err := x509.ParseECPrivateKey(der); err == nil {
			obj := mrb.ObjAlloc(oruby.MrbTTCData, ecCls)
			rb.Set(mrb, obj.Value(), &ecKey{priv: priv, pub: &priv.PublicKey, curve: priv.Curve, curveName: canonicalCurveName(priv.Curve)})
			return obj
		}
		if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
			switch kt := k.(type) {
			case *rsa.PrivateKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, rsaCls)
				rb.Set(mrb, obj.Value(), &rsaKey{priv: kt, pub: &kt.PublicKey})
				return obj
			case *ecdsa.PrivateKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, ecCls)
				rb.Set(mrb, obj.Value(), &ecKey{priv: kt, pub: &kt.PublicKey, curve: kt.Curve, curveName: canonicalCurveName(kt.Curve)})
				return obj
			case ed25519.PrivateKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
				rb.Set(mrb, obj.Value(), &rawPKey{alg: "ED25519", edPriv: kt, edPub: kt.Public().(ed25519.PublicKey)})
				return obj
			case *ecdh.PrivateKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
				rb.Set(mrb, obj.Value(), &rawPKey{alg: "X25519", ecdhPriv: kt, ecdhPub: kt.PublicKey()})
				return obj
			}
		}
		if k, err := x509.ParsePKIXPublicKey(der); err == nil {
			switch kt := k.(type) {
			case *rsa.PublicKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, rsaCls)
				rb.Set(mrb, obj.Value(), &rsaKey{pub: kt})
				return obj
			case *ecdsa.PublicKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, ecCls)
				rb.Set(mrb, obj.Value(), &ecKey{pub: kt, curve: kt.Curve, curveName: canonicalCurveName(kt.Curve)})
				return obj
			case ed25519.PublicKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
				rb.Set(mrb, obj.Value(), &rawPKey{alg: "ED25519", edPub: kt})
				return obj
			case *ecdh.PublicKey:
				obj := mrb.ObjAlloc(oruby.MrbTTCData, pkeyBase)
				rb.Set(mrb, obj.Value(), &rawPKey{alg: "X25519", ecdhPub: kt})
				return obj
			}
		}
		return mrb.Raise(pkeyErr, "Could not parse PKey")
	}, mrb.ArgsArg(1, 1))
}

func initOpenSSLSSL(mrb *oruby.MrbState, mod, osslErr, certCls oruby.RClass) {
	sslMod := mrb.DefineModuleUnder(mod, "SSL")
	sslErrCls := mrb.DefineClassUnder(sslMod, "SSLError", osslErr)

	// -- OpenSSL::SSL::SSLContext ---------------------------------------------
	ctxCls := rb.DataClass(mrb.DefineClassUnder(sslMod, "SSLContext", mrb.ObjectClass()))
	ctxCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, &osslSSLContext{verifyMode: 0, verifyHostname: true})
		return self
	}, mrb.ArgsOpt(1))

	getCtx := func(mrb *oruby.MrbState, self oruby.Value) *osslSSLContext {
		c, _ := rb.Get[*osslSSLContext](mrb, self)
		if c == nil {
			c = &osslSSLContext{}
			rb.Set(mrb, self, c)
		}
		return c
	}

	ctxCls.DefineMethod("verify_mode", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getCtx(mrb, self).verifyMode)
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("verify_mode=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			getCtx(mrb, self).verifyMode = 0
		} else {
			getCtx(mrb, self).verifyMode = int(v.Int64())
		}
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("verify_hostname", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getCtx(mrb, self).verifyHostname)
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("verify_hostname=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		getCtx(mrb, self).verifyHostname = v.Bool()
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("cert", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@cert")
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("cert=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		ctx := getCtx(mrb, self)
		if v.IsNil() {
			ctx.cert = nil
		} else {
			c, ok := rb.Get[*x509Cert](mrb, v)
			if !ok {
				return mrb.Raise(mrb.ETypeError(), "expected OpenSSL::X509::Certificate")
			}
			ctx.cert = c
		}
		mrb.SetIV(self, "@cert", v)
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("extra_chain_cert", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@extra_chain_cert")
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("extra_chain_cert=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		ctx := getCtx(mrb, self)
		ctx.extraChain = nil
		if v.IsArray() {
			for i := 0; i < oruby.RArrayLen(v); i++ {
				if c, ok := rb.Get[*x509Cert](mrb, mrb.AryEntry(v, i)); ok {
					ctx.extraChain = append(ctx.extraChain, c)
				}
			}
		}
		mrb.SetIV(self, "@extra_chain_cert", v)
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@key")
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("key=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		ctx := getCtx(mrb, self)
		if v.IsNil() {
			ctx.key = nil
		} else if rk, ok := rb.Get[*rsaKey](mrb, v); ok && rk.priv != nil {
			ctx.key = rk.priv
		} else if ek, ok := rb.Get[*ecKey](mrb, v); ok && ek.priv != nil {
			ctx.key = ek.priv
		} else {
			return mrb.Raise(mrb.ETypeError(), "expected OpenSSL::PKey private key")
		}
		mrb.SetIV(self, "@key", v)
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("ca_file", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if s := getCtx(mrb, self).caFile; s != "" {
			return mrb.StrNew(s)
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("ca_file=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			getCtx(mrb, self).caFile = ""
		} else {
			getCtx(mrb, self).caFile = v.String()
		}
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("ca_path", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if s := getCtx(mrb, self).caPath; s != "" {
			return mrb.StrNew(s)
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("ca_path=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			getCtx(mrb, self).caPath = ""
		} else {
			getCtx(mrb, self).caPath = v.String()
		}
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("cert_store", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@cert_store")
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("cert_store=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		ctx := getCtx(mrb, self)
		if v.IsNil() {
			ctx.certStore = nil
		} else if st, ok := rb.Get[*x509Store](mrb, v); ok {
			ctx.certStore = st
		} else {
			return mrb.Raise(mrb.ETypeError(), "expected OpenSSL::X509::Store")
		}
		mrb.SetIV(self, "@cert_store", v)
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("min_version=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		getCtx(mrb, self).minVersion = rubyTLSVersionToGo(mrb, v)
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("max_version=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		getCtx(mrb, self).maxVersion = rubyTLSVersionToGo(mrb, v)
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("alpn_protocols", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		protos := getCtx(mrb, self).alpnProtocols
		if protos == nil {
			return mrb.NilValue()
		}
		ary := mrb.AryNewCapa(len(protos))
		for _, p := range protos {
			mrb.AryPush(ary, mrb.StrNew(p))
		}
		return ary
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("alpn_protocols=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		ctx := getCtx(mrb, self)
		ctx.alpnProtocols = nil
		if v.IsArray() {
			for i := 0; i < oruby.RArrayLen(v); i++ {
				ctx.alpnProtocols = append(ctx.alpnProtocols, mrb.AryEntry(v, i).String())
			}
		}
		return v
	}, mrb.ArgsReq(1))

	ctxCls.DefineMethod("options", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.Int64Value(getCtx(mrb, self).options)
	}, mrb.ArgsNone())
	ctxCls.DefineMethod("options=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		getCtx(mrb, self).options = v.Int64()
		return v
	}, mrb.ArgsReq(1))

	// -- OpenSSL::SSL::SSLSocket ----------------------------------------------
	sockCls := rb.DataClass(mrb.DefineClassUnder(sslMod, "SSLSocket", mrb.ObjectClass()))

	sockCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		ioVal := args[0]
		var ctxVal oruby.Value
		var ctx *osslSSLContext
		if len(args) >= 2 && !args[1].IsNil() {
			ctxVal = args[1]
			var ok bool
			ctx, ok = rb.Get[*osslSSLContext](mrb, ctxVal)
			if !ok {
				return mrb.Raise(mrb.ETypeError(), "expected OpenSSL::SSL::SSLContext")
			}
		} else {
			ctxVal, _ = mrb.Funcall(ctxCls, mrb.Intern("new"))
			ctx, _ = rb.Get[*osslSSLContext](mrb, ctxVal)
		}
		s := &osslSSLSocket{ioVal: ioVal, ctx: ctx}
		rb.Set(mrb, self, s)
		mrb.SetIV(self, "@io", ioVal)
		mrb.SetIV(self, "@context", ctxVal)
		return self
	}, mrb.ArgsArg(1, 1))

	getSSLSock := func(mrb *oruby.MrbState, self oruby.Value) (*osslSSLSocket, oruby.MrbValue) {
		s, ok := rb.Get[*osslSSLSocket](mrb, self)
		if !ok || s == nil {
			return nil, mrb.Raise(sslErrCls, "SSLSocket not initialized")
		}
		return s, nil
	}

	sockCls.DefineMethod("io", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@io")
	}, mrb.ArgsNone())
	sockCls.DefineAlias("to_io", "io")

	sockCls.DefineMethod("context", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@context")
	}, mrb.ArgsNone())

	sockCls.DefineMethod("hostname", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if s.hostname == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(s.hostname)
	}, mrb.ArgsNone())
	sockCls.DefineMethod("hostname=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			s.hostname = ""
		} else {
			s.hostname = v.String()
		}
		return v
	}, mrb.ArgsReq(1))

	sockCls.DefineMethod("sync_close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		return mrb.BoolValue(s.syncClose)
	}, mrb.ArgsNone())
	sockCls.DefineMethod("sync_close=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		s.syncClose = v.Bool()
		return v
	}, mrb.ArgsReq(1))

	handshake := func(mrb *oruby.MrbState, self oruby.Value, isServer bool) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if err := s.ensureRawConn(mrb); err != nil {
			return mrb.Raise(sslErrCls, err.Error())
		}
		cfg, err := s.ctx.buildTLSConfig(s.hostname, isServer)
		if err != nil {
			return mrb.Raise(sslErrCls, err.Error())
		}
		if isServer {
			s.tlsConn = tls.Server(s.rawConn, cfg)
		} else {
			s.tlsConn = tls.Client(s.rawConn, cfg)
		}
		var herr error
		mrb.Blocking(func() {
			herr = s.tlsConn.Handshake()
		})
		if herr != nil {
			s.verifyRes = 20
			return mrb.Raise(sslErrCls, herr.Error())
		}
		s.verifyRes = 0
		s.reader = bufio.NewReader(s.tlsConn)
		return self
	}

	sockCls.DefineMethod("connect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return handshake(mrb, self, false)
	}, mrb.ArgsNone())
	sockCls.DefineMethod("connect_nonblock", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return handshake(mrb, self, false)
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	sockCls.DefineMethod("accept", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return handshake(mrb, self, true)
	}, mrb.ArgsNone())
	sockCls.DefineMethod("accept_nonblock", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return handshake(mrb, self, true)
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	sockCls.DefineMethod("post_connection_check", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if s.tlsConn == nil {
			return mrb.Raise(sslErrCls, "SSL session not established")
		}
		host := mrb.GetArgsFirst().String()
		state := s.tlsConn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			return mrb.Raise(sslErrCls, "no peer certificate")
		}
		if err := state.PeerCertificates[0].VerifyHostname(host); err != nil {
			return mrb.Raise(sslErrCls, err.Error())
		}
		return mrb.TrueValue()
	}, mrb.ArgsReq(1))

	sockCls.DefineMethod("write", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if s.closed || s.tlsConn == nil {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		data := mrb.String(mrb.GetArgsFirst())
		var n int
		var err error
		mrb.Blocking(func() {
			n, err = s.tlsConn.Write([]byte(data))
		})
		if err != nil {
			return mrb.Raise(sslErrCls, err.Error())
		}
		return mrb.FixnumValue(n)
	}, mrb.ArgsReq(1))
	sockCls.DefineAlias("syswrite", "write")
	sockCls.DefineMethod("write_nonblock", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if s.closed || s.tlsConn == nil {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		raiseOnWait := true
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			if ev, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern("exception").Value()); err == nil && !ev.IsNil() {
				raiseOnWait = ev.Bool()
			}
		}
		data := mrb.String(mrb.GetArgs().Item(0))
		var n int
		var err error
		_ = s.tlsConn.SetWriteDeadline(time.Now().Add(time.Millisecond))
		mrb.Blocking(func() {
			n, err = s.tlsConn.Write([]byte(data))
		})
		_ = s.tlsConn.SetWriteDeadline(time.Time{})
		if n > 0 {
			return mrb.FixnumValue(n)
		}
		if err != nil {
			var ne net.Error
			if (errors.As(err, &ne) && ne.Timeout()) || errors.Is(err, os.ErrDeadlineExceeded) {
				if !raiseOnWait {
					return mrb.Intern("wait_writable").Value()
				}
				return mrb.Raise(rb.ClassPath(mrb, "OpenSSL::SSL::SSLErrorWaitWritable"), "write would block")
			}
			return mrb.Raise(sslErrCls, err.Error())
		}
		return mrb.FixnumValue(n)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	sockCls.DefineMethod("read", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if s.closed || s.tlsConn == nil {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		args := mrb.GetArgs().Slice()
		var out []byte
		var err error
		if len(args) == 0 || args[0].IsNil() {
			mrb.Blocking(func() {
				out, err = io.ReadAll(s.reader)
			})
			if err != nil {
				return mrb.Raise(sslErrCls, err.Error())
			}
			s.eof = true
			return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		want := int(args[0].Int64())
		if want == 0 {
			return mrb.Call(mrb.StrNew(""), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		buf := make([]byte, want)
		var n int
		mrb.Blocking(func() {
			n, err = io.ReadFull(s.reader, buf)
		})
		if n == 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
			s.eof = true
			return mrb.NilValue()
		}
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return mrb.Raise(sslErrCls, err.Error())
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			s.eof = true
		}
		res := mrb.Call(mrb.BytesValue(buf[:n]), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		if len(args) >= 2 && args[1].IsString() {
			_, _ = mrb.Funcall(args[1], mrb.Intern("replace"), res)
			return args[1]
		}
		return res
	}, mrb.ArgsOpt(2))

	readPartialFn := func(mrb *oruby.MrbState, self oruby.Value, nonblock bool) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if s.closed || s.tlsConn == nil {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		args := mrb.GetArgs().Slice()
		maxlen := int(args[0].Int64())
		if maxlen <= 0 {
			return mrb.Call(mrb.StrNew(""), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		raiseOnWait := true
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			if ev, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern("exception").Value()); err == nil && !ev.IsNil() {
				raiseOnWait = ev.Bool()
			}
		}
		buf := make([]byte, maxlen)
		var n int
		var err error
		if nonblock && s.reader.Buffered() == 0 {
			_ = s.tlsConn.SetReadDeadline(time.Now().Add(time.Millisecond))
			mrb.Blocking(func() {
				n, err = s.reader.Read(buf)
			})
			_ = s.tlsConn.SetReadDeadline(time.Time{})
		} else {
			mrb.Blocking(func() {
				n, err = s.reader.Read(buf)
			})
		}
		if n > 0 {
			res := mrb.Call(mrb.BytesValue(buf[:n]), "force_encoding", mrb.StrNew("ASCII-8BIT"))
			if len(args) >= 2 && args[1].IsString() {
				_, _ = mrb.Funcall(args[1], mrb.Intern("replace"), res)
				return args[1]
			}
			return res
		}
		if err != nil {
			var ne net.Error
			if nonblock && (errors.As(err, &ne) && ne.Timeout() || errors.Is(err, os.ErrDeadlineExceeded)) {
				if !raiseOnWait {
					return mrb.Intern("wait_readable").Value()
				}
				return mrb.Raise(rb.ClassPath(mrb, "OpenSSL::SSL::SSLErrorWaitReadable"), "read would block")
			}
			if errors.Is(err, io.EOF) {
				s.eof = true
				return mrb.Raise(mrb.ClassGet("EOFError"), "end of file reached")
			}
			return mrb.Raise(sslErrCls, err.Error())
		}
		return mrb.Raise(mrb.ClassGet("EOFError"), "end of file reached")
	}

	sockCls.DefineMethod("readpartial", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return readPartialFn(mrb, self, false)
	}, mrb.ArgsArg(1, 1))
	sockCls.DefineAlias("sysread", "readpartial")

	sockCls.DefineMethod("read_nonblock", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return readPartialFn(mrb, self, true)
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	sockCls.DefineMethod("gets", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, exc := getSSLSock(mrb, self)
		if exc != nil {
			return exc
		}
		if s.closed || s.tlsConn == nil {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		sep := byte('\n')
		if args := mrb.GetArgs().Slice(); len(args) >= 1 && args[0].IsString() && len(args[0].String()) > 0 {
			sep = args[0].String()[0]
		}
		var line []byte
		var err error
		mrb.Blocking(func() {
			line, err = s.reader.ReadBytes(sep)
		})
		if len(line) > 0 {
			return mrb.BytesValue(line)
		}
		if errors.Is(err, io.EOF) {
			s.eof = true
			return mrb.NilValue()
		}
		return mrb.Raise(sslErrCls, err.Error())
	}, mrb.ArgsOpt(2))

	sockCls.DefineMethod("eof?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		return mrb.BoolValue(s == nil || s.eof || s.closed)
	}, mrb.ArgsNone())
	sockCls.DefineAlias("eof", "eof?")

	sockCls.DefineMethod("closed?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		return mrb.BoolValue(s == nil || s.closed)
	}, mrb.ArgsNone())

	sockCls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil || s.closed {
			return mrb.NilValue()
		}
		s.closed = true
		if s.tlsConn != nil {
			_ = s.tlsConn.Close()
		} else if s.rawConn != nil {
			_ = s.rawConn.Close()
		}
		if s.syncClose && !s.ioVal.IsNil() && mrb.RespondTo(s.ioVal, mrb.Intern("close")) {
			_, _ = mrb.Funcall(s.ioVal, mrb.Intern("close"))
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
	sockCls.DefineAlias("sysclose", "close")

	sockCls.DefineMethod("flush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return self
	}, mrb.ArgsNone())

	sockCls.DefineMethod("peer_cert", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil || s.tlsConn == nil {
			return mrb.NilValue()
		}
		certs := s.tlsConn.ConnectionState().PeerCertificates
		if len(certs) == 0 {
			return mrb.NilValue()
		}
		return wrapX509Cert(mrb, certs[0])
	}, mrb.ArgsNone())

	sockCls.DefineMethod("peer_cert_chain", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil || s.tlsConn == nil {
			return mrb.NilValue()
		}
		certs := s.tlsConn.ConnectionState().PeerCertificates
		if len(certs) == 0 {
			return mrb.NilValue()
		}
		ary := mrb.AryNewCapa(len(certs))
		for _, c := range certs {
			mrb.AryPush(ary, wrapX509Cert(mrb, c))
		}
		return ary
	}, mrb.ArgsNone())

	sockCls.DefineMethod("ssl_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil || s.tlsConn == nil {
			return mrb.NilValue()
		}
		return mrb.StrNew(goTLSVersionName(s.tlsConn.ConnectionState().Version))
	}, mrb.ArgsNone())

	sockCls.DefineMethod("cipher", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil || s.tlsConn == nil {
			return mrb.NilValue()
		}
		st := s.tlsConn.ConnectionState()
		ary := mrb.AryNewCapa(4)
		mrb.AryPush(ary, mrb.StrNew(tls.CipherSuiteName(st.CipherSuite)))
		mrb.AryPush(ary, mrb.StrNew(goTLSVersionName(st.Version)))
		mrb.AryPush(ary, mrb.FixnumValue(256))
		mrb.AryPush(ary, mrb.FixnumValue(256))
		return ary
	}, mrb.ArgsNone())

	sockCls.DefineMethod("alpn_protocol", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil || s.tlsConn == nil {
			return mrb.NilValue()
		}
		if p := s.tlsConn.ConnectionState().NegotiatedProtocol; p != "" {
			return mrb.StrNew(p)
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	sockCls.DefineMethod("verify_result", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(s.verifyRes)
	}, mrb.ArgsNone())

	sockCls.DefineMethod("session_reused?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*osslSSLSocket](mrb, self)
		if s == nil || s.tlsConn == nil {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(s.tlsConn.ConnectionState().DidResume)
	}, mrb.ArgsNone())
}
