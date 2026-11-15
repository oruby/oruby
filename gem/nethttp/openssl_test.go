package nethttp_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"
)

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
