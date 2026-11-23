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

func TestOpenSSLCipherPKCS5AndASN1(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "openssl"

# AES-256-GCM (Rails MessageEncryptor / ActiveRecord::Encryption)
enc = OpenSSL::Cipher.new("aes-256-gcm")
enc.encrypt
raise "gcm auth?" unless enc.authenticated?
key = enc.random_key
iv  = enc.random_iv
enc.auth_data = "aad-header"
ct  = enc.update("secret payload ") + enc.update("part 2") + enc.final
tag = enc.auth_tag
raise "tag len" unless tag.bytesize == 16

dec = OpenSSL::Cipher.new("aes-256-gcm")
dec.decrypt
dec.key = key
dec.iv  = iv
dec.auth_tag = tag
dec.auth_data = "aad-header"
plain = dec.update(ct) + dec.final
raise "gcm roundtrip=#{plain.inspect}" unless plain == "secret payload part 2"

# Tampered tag raises CipherError
bad = false
begin
  dec2 = OpenSSL::Cipher.new("aes-256-gcm")
  dec2.decrypt
  dec2.key = key
  dec2.iv = iv
  dec2.auth_tag = "\x00".b * 16
  dec2.auth_data = "aad-header"
  dec2.update(ct) + dec2.final
rescue OpenSSL::Cipher::CipherError
  bad = true
end
raise "expected CipherError on tampered tag" unless bad

# AES-256-CBC with PKCS#7 padding
cbc = OpenSSL::Cipher::AES256.new(:CBC)
cbc.encrypt
k2 = cbc.random_key
iv2 = cbc.random_iv
ct2 = cbc.update("hello cbc world!") + cbc.final
cbc_d = OpenSSL::Cipher.new("AES-256-CBC")
cbc_d.decrypt
cbc_d.key = k2
cbc_d.iv = iv2
raise "cbc roundtrip" unless (cbc_d.update(ct2) + cbc_d.final) == "hello cbc world!"

# AES-128-CTR & ChaCha20-Poly1305
ctr = OpenSSL::Cipher.new("aes-128-ctr").encrypt
k3 = ctr.random_key
iv3 = ctr.random_iv
ct3 = ctr.update("streaming ctr") + ctr.final
ctr_d = OpenSSL::Cipher.new("aes-128-ctr").decrypt
ctr_d.key = k3; ctr_d.iv = iv3
raise "ctr roundtrip" unless (ctr_d.update(ct3) + ctr_d.final) == "streaming ctr"

cc = OpenSSL::Cipher.new("chacha20-poly1305").encrypt
k4 = cc.random_key; iv4 = cc.random_iv
cc.auth_data = "x"
ct4 = cc.update("chacha") + cc.final
tag4 = cc.auth_tag
cc_d = OpenSSL::Cipher.new("chacha20-poly1305").decrypt
cc_d.key = k4; cc_d.iv = iv4; cc_d.auth_tag = tag4; cc_d.auth_data = "x"
raise "chacha roundtrip" unless (cc_d.update(ct4) + cc_d.final) == "chacha"

# OpenSSL::PKCS5 (Rails ActiveSupport::KeyGenerator)
dk1 = OpenSSL::PKCS5.pbkdf2_hmac("password", "salt", 100, 32, "SHA256")
dk2 = OpenSSL::PKCS5.pbkdf2_hmac_sha1("password", "salt", 100, 20)
raise "pkcs5" unless dk1.bytesize == 32 && dk2.bytesize == 20

# OpenSSL::ASN1 encode & decode (JWT ES256 DER signatures)
seq = OpenSSL::ASN1::Sequence.new([
  OpenSSL::ASN1::Integer.new(OpenSSL::BN.new("12345678901234567890")),
  OpenSSL::ASN1::Integer.new(OpenSSL::BN.new("98765432109876543210")),
  OpenSSL::ASN1::OctetString.new("abc"),
  OpenSSL::ASN1::ObjectId.new("1.2.840.10045.3.1.7"),
])
der = seq.to_der
dec_seq = OpenSSL::ASN1.decode(der)
raise "asn1 seq" unless dec_seq.is_a?(OpenSSL::ASN1::Sequence) && dec_seq.value.length == 4
raise "asn1 int 0" unless dec_seq.value[0].value.to_s == "12345678901234567890"
raise "asn1 int 1" unless dec_seq.value[1].value.to_s == "98765432109876543210"
raise "asn1 octet" unless dec_seq.value[2].value == "abc"
raise "asn1 oid" unless dec_seq.value[3].value == "1.2.840.10045.3.1.7"

# Decode real ECDSA signature via ASN1
ec = OpenSSL::PKey::EC.generate("prime256v1")
esig = ec.sign("SHA256", "jwt payload")
asn_sig = OpenSSL::ASN1.decode(esig)
raise "ecdsa asn1" unless asn_sig.is_a?(OpenSSL::ASN1::Sequence) && asn_sig.value.length == 2
`)
	if err != nil {
		t.Fatalf("OpenSSL Cipher/PKCS5/ASN1 test failed: %v", err)
	}
}

func TestOpenSSLKDFX509RequestCRLEd25519(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "openssl"

# 1. OpenSSL::KDF.hkdf & scrypt
hk = OpenSSL::KDF.hkdf("secret-ikm", salt: "my-salt", info: "app-ctx", length: 32, hash: "SHA256")
raise "hkdf len" unless hk.bytesize == 32
hk2 = OpenSSL::KDF.hkdf("secret-ikm", salt: "my-salt", info: "app-ctx", length: 32, hash: "SHA256")
raise "hkdf det" unless hk == hk2

sc = OpenSSL::KDF.scrypt("password", salt: "salt1234", N: 1024, r: 8, p: 1, length: 32)
raise "scrypt len" unless sc.bytesize == 32

# 2. Ed25519 & X25519 via OpenSSL::PKey
ed = OpenSSL::PKey.generate_key("ED25519")
raise "ed oid" unless ed.oid == "ED25519" && ed.private? && ed.public?
sig = ed.sign(nil, "hello ed25519")
raise "ed verify" unless ed.verify(nil, sig, "hello ed25519")
raise "ed bad verify" if ed.verify(nil, sig, "tampered")
ed_pub = OpenSSL::PKey.read(ed.public_to_pem)
raise "ed pub verify" unless !ed_pub.private? && ed_pub.verify(nil, sig, "hello ed25519")
ed_raw = OpenSSL::PKey.new_raw_private_key("ED25519", ed.raw_private_key)
raise "ed raw" unless ed_raw.raw_public_key == ed.raw_public_key

x1 = OpenSSL::PKey.generate_key("X25519")
x2 = OpenSSL::PKey.generate_key("X25519")
s1 = x1.derive(OpenSSL::PKey.new_raw_public_key("X25519", x2.raw_public_key))
s2 = x2.derive(OpenSSL::PKey.read(x1.public_to_pem))
raise "x25519 derive" unless s1 == s2 && s1.bytesize == 32

# 3. X509::Request (CSR)
csr_key = OpenSSL::PKey::EC.generate("prime256v1")
req = OpenSSL::X509::Request.new
req.version = 0
req.subject = OpenSSL::X509::Name.parse("/CN=invoice.example.com/O=Acme")
req.public_key = csr_key
req.sign(csr_key, "SHA256")
req2 = OpenSSL::X509::Request.new(req.to_pem)
raise "csr verify" unless req2.verify(csr_key)
raise "csr subject" unless req2.subject.to_s.include?("invoice.example.com")

# 4. X509::CRL & X509::Revoked
crl = OpenSSL::X509::CRL.new
crl.version = 1
crl.issuer = OpenSSL::X509::Name.parse("/CN=Acme CA")
crl.last_update = Time.now - 60
crl.next_update = Time.now + 86400
rev = OpenSSL::X509::Revoked.new
rev.serial = 42
rev.time = Time.now
crl.add_revoked(rev)
crl.sign(csr_key, "SHA256")
crl2 = OpenSSL::X509::CRL.new(crl.to_pem)
raise "crl verify" unless crl2.verify(csr_key)
raise "crl revoked" unless crl2.revoked.length == 1 && crl2.revoked.first.serial.to_i == 42
`)
	if err != nil {
		t.Fatalf("OpenSSL KDF/X509/Ed25519 test failed: %v", err)
	}
}
