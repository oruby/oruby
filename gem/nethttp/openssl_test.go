package nethttp_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func checkOpenSSLRandom(t *testing.T, script string) {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	got, err := mrb.Eval("require 'openssl'\n" + script)
	if err != nil || !got.Bool() {
		t.Fatalf("OpenSSL::Random check = %v, %v; want true, nil", got, err)
	}
}

type chunkReader struct{ io.Reader }

func (r chunkReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

type zeroReader struct{ read int }

func (r *zeroReader) Read(p []byte) (int, error) {
	clear(p)
	r.read += len(p)
	return len(p), nil
}

func TestOpenSSLRandomBytesReader(t *testing.T) {
	original := rand.Reader
	t.Cleanup(func() { rand.Reader = original })
	rand.Reader = chunkReader{bytes.NewReader([]byte{0, 0xff, 0x80, 0x41})}
	checkOpenSSLRandom(t, `
    value = OpenSSL::Random.random_bytes(4)
    value.bytes == [0, 255, 128, 65] && value.encoding.name == "ASCII-8BIT"
  `)
}

func TestOpenSSLRandomBytesFailure(t *testing.T) {
	original := rand.Reader
	t.Cleanup(func() { rand.Reader = original })
	rand.Reader = failingReader{}
	checkOpenSSLRandom(t, `
    empty = OpenSSL::Random.random_bytes(0)
    begin
      OpenSSL::Random.random_bytes(1)
      false
    rescue OpenSSL::Random::RandomError => e
      empty.bytesize == 0 && e.is_a?(OpenSSL::OpenSSLError) && e.message.include?("entropy unavailable")
    end
  `)
}

func TestOpenSSLRandomBytesShortRead(t *testing.T) {
	original := rand.Reader
	t.Cleanup(func() { rand.Reader = original })
	rand.Reader = bytes.NewReader([]byte{1, 2})
	checkOpenSSLRandom(t, `
    begin
      OpenSSL::Random.random_bytes(3)
      false
    rescue OpenSSL::Random::RandomError
      true
    end
  `)
}

func TestOpenSSLRandomBytesLimit(t *testing.T) {
	original := rand.Reader
	t.Cleanup(func() { rand.Reader = original })
	reader := &zeroReader{}
	rand.Reader = reader
	checkOpenSSLRandom(t, `
    value = OpenSSL::Random.random_bytes(16777216)
    raise "wrong size" unless value.bytesize == 16777216
    raise "wrong data" unless value.getbyte(0) == 0 && value.getbyte(-1) == 0
    begin
      OpenSSL.__random_bytes(16777217)
      false
    rescue ArgumentError
      true
    end
  `)
	if reader.read != 16777216 {
		t.Fatalf("read %d bytes; want exactly 16777216", reader.read)
	}
}

func TestOpenSSLRandomBytesArguments(t *testing.T) {
	checkOpenSSLRandom(t, `
    def expect_random_error(klass)
      begin
        yield
      rescue => e
        raise "expected #{klass}, got #{e.class}: #{e.message}" unless e.is_a?(klass)
        return
      end
      raise "expected #{klass}"
    end
    converted = Object.new
    def converted.to_int; 2; end
    private_conversion = Class.new do
      private
      def to_int; 1; end
    end.new
    invalid = Object.new
    def invalid.to_int; "2"; end
    broken = Object.new
    def broken.to_int; raise ArgumentError, "coercion failed"; end
    raise "float" unless OpenSSL::Random.random_bytes(3.9).bytesize == 3
    raise "negative float" unless OpenSSL::Random.random_bytes(-0.5).bytesize == 0
    raise "to_int" unless OpenSSL::Random.random_bytes(converted).bytesize == 2
    raise "private to_int" unless OpenSSL::Random.random_bytes(private_conversion).bytesize == 1
    expect_random_error(TypeError) { OpenSSL::Random.random_bytes(nil) }
    expect_random_error(TypeError) { OpenSSL::Random.random_bytes("3") }
    expect_random_error(TypeError) { OpenSSL::Random.random_bytes(invalid) }
    begin
      OpenSSL::Random.random_bytes(broken)
      raise "coercion did not raise"
    rescue ArgumentError => e
      raise "coercion error changed" unless e.message == "coercion failed"
    end
    expect_random_error(ArgumentError) { OpenSSL::Random.random_bytes(-1) }
    expect_random_error(ArgumentError) { OpenSSL::Random.random_bytes(16777217) }
    expect_random_error(ArgumentError) { OpenSSL::Random.random_bytes(16777217.0) }
    expect_random_error(RangeError) { OpenSSL::Random.random_bytes(1.0 / 0.0) }
    expect_random_error(RangeError) { OpenSSL::Random.random_bytes(-1.0 / 0.0) }
    expect_random_error(RangeError) { OpenSSL::Random.random_bytes(0.0 / 0.0) }
    expect_random_error(RangeError) { OpenSSL::Random.random_bytes(1.0e30) }
    expect_random_error(RangeError) { OpenSSL::Random.random_bytes(-1.0e30) }
    expect_random_error(TypeError) { OpenSSL.__random_bytes("3") }
    expect_random_error(ArgumentError) { OpenSSL.__random_bytes(-1) }
    expect_random_error(ArgumentError) { OpenSSL::Random.random_bytes }
    expect_random_error(ArgumentError) { OpenSSL::Random.random_bytes(1, 2) }
    true
  `)
}

func TestOpenSSLRandomBytesRealEntropy(t *testing.T) {
	checkOpenSSLRandom(t, `
    a = OpenSSL::Random.random_bytes(32)
    b = OpenSSL::Random.random_bytes(32)
    a.bytesize == 32 && b.bytesize == 32 && a != b
  `)
}

func TestOpenSSLSecureCompare(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	got, err := mrb.Eval(`
    require "openssl"
    OpenSSL.fixed_length_secure_compare("same", "same") &&
      !OpenSSL.fixed_length_secure_compare("same", "else") &&
      OpenSSL.secure_compare("short", "short") &&
      !OpenSSL.secure_compare("short", "longer")
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("OpenSSL secure comparisons = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    begin
      OpenSSL.fixed_length_secure_compare("short", "longer")
      false
    rescue ArgumentError
      true
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("OpenSSL fixed-length mismatch = %v, %v; want true, nil", got, err)
	}
}

func TestOpenSSLBNAndPKeyECAndRSA(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "openssl"

# OpenSSL::BN
a = OpenSSL::BN.new("123456789012345678901234567890")
b = OpenSSL::BN.new("FF", 16)
raise "bn hex" unless b.to_i == 255
raise "bn to_s(16)" unless b.to_s(16) == "FF"
raise "bn bin" unless OpenSSL::BN.new("\x01\x00".b, 2).to_i == 256
raise "bn arith" unless (42.to_bn + 8.to_bn) == 50.to_bn
raise "bn mod_exp" unless 2.to_bn.mod_exp(10, 1000) == 24.to_bn
raise "bn gcd" unless 48.to_bn.gcd(18.to_bn) == 6.to_bn
raise "bn prime" unless 104729.to_bn.prime? && !104728.to_bn.prime?
raise "bn rand" unless OpenSSL::BN.rand(64).num_bits <= 64
raise "bn add big" unless (a + 1).to_s == "123456789012345678901234567891"

# RSA sign/verify & PKey.read
rsa = OpenSSL::PKey::RSA.new(2048)
raise "rsa n/e" unless rsa.n.num_bits >= 2040 && rsa.e.to_i == 65537
sig = rsa.sign("SHA256", "hello rsa")
raise "rsa verify" unless rsa.public_key.verify("SHA256", sig, "hello rsa")
raise "rsa bad verify" if rsa.public_key.verify("SHA256", sig, "tampered")
rsa2 = OpenSSL::PKey.read(rsa.to_pem)
raise "PKey.read rsa" unless rsa2.is_a?(OpenSSL::PKey::RSA) && rsa2.verify("SHA256", sig, "hello rsa")

# EC generate, sign/verify, Group/Point, ECDH, PEM/DER, PKey.read
ec = OpenSSL::PKey::EC.generate("prime256v1")
raise "ec group" unless ec.group.curve_name == "prime256v1" && ec.group.degree == 256
raise "ec priv/pub" unless ec.private? && ec.public? && ec.check_key
raise "ec point" unless ec.public_key.is_a?(OpenSSL::PKey::EC::Point) && ec.public_key.on_curve?
esig = ec.sign("SHA256", "hello ecdsa")
raise "ec verify" unless ec.verify("SHA256", esig, "hello ecdsa")
raise "ec bad verify" if ec.verify("SHA256", esig, "bad")

ec_pub = OpenSSL::PKey.read(ec.public_to_pem)
raise "PKey.read ec pub" unless ec_pub.is_a?(OpenSSL::PKey::EC) && !ec_pub.private? && ec_pub.verify("SHA256", esig, "hello ecdsa")
ec_priv = OpenSSL::PKey.read(ec.to_pem)
raise "PKey.read ec priv" unless ec_priv.is_a?(OpenSSL::PKey::EC) && ec_priv.private?

# ECDH key agreement
alice = OpenSSL::PKey::EC.generate("prime256v1")
bob   = OpenSSL::PKey::EC.generate("prime256v1")
s1 = alice.dh_compute_key(bob.public_key)
s2 = bob.dh_compute_key(alice.public_key)
raise "ecdh mismatch" unless s1 == s2 && s1.bytesize == 32
`)
	if err != nil {
		t.Fatalf("OpenSSL BN/PKey/EC test failed: %v", err)
	}
}

func TestOpenSSLTLSServerAndClient(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "socket"
require "openssl"

# Generate self-signed EC certificate for localhost
key = OpenSSL::PKey::EC.generate("prime256v1")
cert = OpenSSL::X509::Certificate.new
cert.version = 2
cert.serial = 1
cert.subject = OpenSSL::X509::Name.parse("/CN=localhost")
cert.issuer = cert.subject
cert.public_key = key
cert.not_before = Time.now - 60
cert.not_after = Time.now + 3600
ef = OpenSSL::X509::ExtensionFactory.new
ef.subject_certificate = cert
ef.issuer_certificate = cert
cert.add_extension(ef.create_extension("subjectAltName", "DNS:localhost,IP:127.0.0.1"))
cert.sign(key, OpenSSL::Digest::SHA256.new)

srv_ctx = OpenSSL::SSL::SSLContext.new
srv_ctx.cert = cert
srv_ctx.key = key
srv_ctx.alpn_protocols = ["h2", "http/1.1"]

tcp_srv = TCPServer.new("127.0.0.1", 0)
port = tcp_srv.addr[1]
ssl_srv = OpenSSL::SSL::SSLServer.new(tcp_srv, srv_ctx)

server_thread = Thread.new do
  client = ssl_srv.accept
  line = client.gets
  client.puts("ECHO:#{line.strip}")
  client.close
end

cli_ctx = OpenSSL::SSL::SSLContext.new
cli_ctx.verify_mode = OpenSSL::SSL::VERIFY_PEER
store = OpenSSL::X509::Store.new
store.add_cert(cert)
cli_ctx.cert_store = store
cli_ctx.alpn_protocols = ["http/1.1"]

tcp_cli = TCPSocket.new("127.0.0.1", port)
ssl_cli = OpenSSL::SSL::SSLSocket.new(tcp_cli, cli_ctx)
ssl_cli.hostname = "localhost"
ssl_cli.sync_close = true
ssl_cli.connect
ssl_cli.post_connection_check("localhost")

raise "alpn" unless ssl_cli.alpn_protocol == "http/1.1"
raise "peer_cert" unless ssl_cli.peer_cert.to_der == cert.to_der
raise "ssl_version" unless ssl_cli.ssl_version.start_with?("TLSv1.")

ssl_cli.puts("hello tls")
reply = ssl_cli.gets
ssl_cli.close
server_thread.join
ssl_srv.close

raise "reply=#{reply.inspect}" unless reply == "ECHO:hello tls\n"
`)
	if err != nil {
		t.Fatalf("OpenSSL TLS server/client test failed: %v", err)
	}
}
