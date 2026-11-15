// Package nethttp implements Ruby's net/http library. The protocol layer
// (headers, requests, responses, buffered IO) is Ruby code over the socket
// gem; HTTPS uses Go crypto/tls (Net::TLSSocket). It also provides a small
// "openssl" library: SSL constants, OpenSSL::Digest and OpenSSL::HMAC.
package nethttp

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"hash"
	"net"
	"os"
	"strings"
	"time"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/digest"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed net_http.rb
var netHTTPRb []byte

//go:embed openssl.rb
var opensslRb []byte

func init() {
	oruby.LazyGem("net/http", func(mrb *oruby.MrbState) interface{} {
		for _, dep := range []string{"socket", "uri", "timeout", "stringio"} {
			if _, err := mrb.Resolve(dep); err != nil {
				return err
			}
		}
		netMod := mrb.DefineModule("Net")
		tlsClass := rb.DataClass(mrb.DefineClassUnder(netMod, "TLSSocket", mrb.ObjectClass()))
		tlsClass.DefineClassMethod("__connect", tlsConnect, mrb.ArgsReq(6))
		tlsClass.DefineMethod("__tls_read", tlsRead, mrb.ArgsReq(2))
		tlsClass.DefineMethod("write", tlsWrite, mrb.ArgsReq(1))
		tlsClass.DefineMethod("close", tlsClose, mrb.ArgsNone())
		tlsClass.DefineMethod("closed?", tlsClosed, mrb.ArgsNone())
		tlsClass.DefineMethod("peer_cert_subject", tlsPeerSubject, mrb.ArgsNone())
		tlsClass.DefineMethod("ssl_version", tlsVersion, mrb.ArgsNone())
		rb.LoadRuby(mrb, "net_http.rb", netHTTPRb)
		return nil
	})
	oruby.GemAlias("net/https", "net/http")
	oruby.GemAlias("net/protocol", "net/http")

	oruby.LazyGem("openssl", func(mrb *oruby.MrbState) interface{} {
		if _, err := mrb.Resolve("digest"); err != nil {
			return err
		}
		mod := mrb.DefineModule("OpenSSL")
		mod.DefineClassMethod("__hmac", hmacDigest, mrb.ArgsReq(3))
		mod.DefineClassMethod("__fixed_length_secure_compare", fixedLengthSecureCompare, mrb.ArgsReq(2))
		mod.DefineClassMethod("__secure_compare", secureCompare, mrb.ArgsReq(2))
		rb.LoadRuby(mrb, "openssl.rb", opensslRb)
		return nil
	})
}

type tlsSock struct {
	conn   *tls.Conn
	closed bool
}

func sslError(mrb *oruby.MrbState, err error) oruby.MrbValue {
	cls := rb.ClassPath(mrb, "OpenSSL::SSL::SSLError")
	if cls.IsNil() {
		cls = rb.ClassPath(mrb, "Net::TLSSocket::Error")
	}
	return mrb.Raise(cls, err.Error())
}

// __connect(fd, hostname, verify, ca_file, ca_path, timeout_s) -> Net::TLSSocket
// wraps the connected TCP socket fd (duplicated) into a TLS client.
func tlsConnect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	f := os.NewFile(uintptr(a[0].Int64()), "tls")
	conn, err := net.FileConn(f) // dups the descriptor
	if err != nil {
		return mrb.RaiseError(err)
	}
	host := ""
	if !a[1].IsNil() {
		host = a[1].String()
	}
	cfg := &tls.Config{ServerName: host, InsecureSkipVerify: !a[2].Bool()}
	if !a[3].IsNil() || !a[4].IsNil() {
		pool := x509.NewCertPool()
		if !a[3].IsNil() {
			pem, err := os.ReadFile(a[3].String())
			if err != nil {
				conn.Close()
				return mrb.RaiseError(err)
			}
			pool.AppendCertsFromPEM(pem)
		}
		if !a[4].IsNil() {
			entries, _ := os.ReadDir(a[4].String())
			for _, e := range entries {
				if pem, err := os.ReadFile(a[4].String() + "/" + e.Name()); err == nil {
					pool.AppendCertsFromPEM(pem)
				}
			}
		}
		cfg.RootCAs = pool
	}
	tc := tls.Client(conn, cfg)
	if !a[5].IsNil() {
		tc.SetDeadline(time.Now().Add(time.Duration(a[5].Float64() * float64(time.Second))))
	}
	if err := tc.Handshake(); err != nil {
		tc.Close()
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return rb.Raise(mrb, "Net::OpenTimeout", "SSL handshake timed out")
		}
		return sslError(mrb, err)
	}
	tc.SetDeadline(time.Time{})
	obj := mrb.ObjAlloc(oruby.MrbTTCData, mrb.ClassPtr(self)).Value()
	rb.Set(mrb, obj, &tlsSock{conn: tc})
	return obj
}

func getTLS(mrb *oruby.MrbState, self oruby.Value) (*tlsSock, oruby.MrbValue) {
	s, ok := rb.Get[*tlsSock](mrb, self)
	if !ok || s.closed {
		return nil, mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
	}
	return s, nil
}

// __tls_read(maxlen, timeout_s) -> String, nil (EOF) or :timeout
func tlsRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getTLS(mrb, self)
	if exc != nil {
		return exc
	}
	a := mrb.GetArgs().Slice()
	if a[1].IsNil() {
		s.conn.SetReadDeadline(time.Time{})
	} else {
		s.conn.SetReadDeadline(time.Now().Add(time.Duration(a[1].Float64() * float64(time.Second))))
	}
	buf := make([]byte, a[0].Int64())
	n, err := s.conn.Read(buf)
	if n > 0 {
		return mrb.BytesValue(buf[:n])
	}
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return mrb.SymbolValue(mrb.Intern("timeout"))
		}
		if errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), "EOF") {
			return mrb.NilValue()
		}
		return sslError(mrb, err)
	}
	return mrb.NilValue()
}

func tlsWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getTLS(mrb, self)
	if exc != nil {
		return exc
	}
	data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	n, err := s.conn.Write(data)
	if err != nil {
		return sslError(mrb, err)
	}
	return mrb.FixnumValue(n)
}

func tlsClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if s, ok := rb.Get[*tlsSock](mrb, self); ok && !s.closed {
		s.closed = true
		s.conn.Close()
	}
	return mrb.NilValue()
}

func tlsClosed(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, ok := rb.Get[*tlsSock](mrb, self)
	return mrb.BoolValue(!ok || s.closed)
}

func tlsPeerSubject(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getTLS(mrb, self)
	if exc != nil {
		return exc
	}
	certs := s.conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return mrb.NilValue()
	}
	return mrb.StrNew(certs[0].Subject.String())
}

func tlsVersion(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getTLS(mrb, self)
	if exc != nil {
		return exc
	}
	switch s.conn.ConnectionState().Version {
	case tls.VersionTLS10:
		return mrb.StrNew("TLSv1")
	case tls.VersionTLS11:
		return mrb.StrNew("TLSv1.1")
	case tls.VersionTLS12:
		return mrb.StrNew("TLSv1.2")
	case tls.VersionTLS13:
		return mrb.StrNew("TLSv1.3")
	}
	return mrb.NilValue()
}

// OpenSSL.__hmac(algorithm, key, data) -> binary digest
func hmacDigest(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	var h func() hash.Hash
	switch strings.ToUpper(strings.ReplaceAll(a[0].String(), "-", "")) {
	case "MD5":
		h = md5.New
	case "SHA1":
		h = sha1.New
	case "SHA224":
		h = sha256.New224
	case "SHA256":
		h = sha256.New
	case "SHA384":
		h = sha512.New384
	case "SHA512":
		h = sha512.New
	default:
		return rb.Raise(mrb, "RuntimeError", "Unsupported digest algorithm (%s)", a[0].String())
	}
	key, err := rb.Bytes(mrb, a[1])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	data, err := rb.Bytes(mrb, a[2])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	m := hmac.New(h, key)
	m.Write(data)
	v := mrb.BytesValue(m.Sum(nil))
	return mrb.Call(v, "force_encoding", mrb.StrNew("ASCII-8BIT"))
}

func fixedLengthSecureCompare(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	a, err := rb.Bytes(mrb, args[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	b, err := rb.Bytes(mrb, args[1])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if len(a) != len(b) {
		return mrb.Raise(mrb.EArgumentError(), "inputs must be of equal length")
	}
	return mrb.BoolValue(subtle.ConstantTimeCompare(a, b) == 1)
}

func secureCompare(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	a, err := rb.Bytes(mrb, args[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	b, err := rb.Bytes(mrb, args[1])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	ha := sha256.Sum256(a)
	hb := sha256.Sum256(b)
	return mrb.BoolValue(subtle.ConstantTimeCompare(ha[:], hb[:]) == 1)
}
