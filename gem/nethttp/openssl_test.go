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
