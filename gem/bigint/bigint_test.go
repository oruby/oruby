package bigint

import (
	"github.com/oruby/oruby"
	"math/big"
	"testing"
)

func assrt(t *testing.T, code string) {
	t.Helper()

	mrb := oruby.MrbOpen()
	defer mrb.Close()

	result, err := mrb.Eval(code)
	if err != nil {
		t.Fatal(code, ": ", err)
	}

	if !result.IsNil() && !oruby.MrbBoolean(result) {
		t.Error(code, ": result =", mrb.Intf(result))
	}
}

func TestBigInt(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// Direct RBigInt Go usage
	bi := New(big.NewInt(42))
	if bi.String() != "42" {
		t.Errorf("expected '42', got %s", bi.String())
	}

	// Bignum class and constructors
	assrt(t, `b = Bignum.new("123456789012345678901234567890"); b.to_s == "123456789012345678901234567890"`)
	assrt(t, `b = Bignum.new(100); b.to_i == 100`)

	// Arithmetic
	assrt(t, `
		b1 = Bignum.new("10000000000000000000")
		b2 = Bignum.new("20000000000000000000")
		(b1 + b2).to_s == "30000000000000000000"
	`)
	assrt(t, `
		b1 = Bignum.new("30000000000000000000")
		b2 = Bignum.new("10000000000000000000")
		(b1 - b2).to_s == "20000000000000000000"
	`)
	assrt(t, `
		b1 = Bignum.new("10000000000")
		b2 = Bignum.new("20000000000")
		(b1 * b2).to_s == "200000000000000000000"
	`)
	assrt(t, `
		b1 = Bignum.new("20000000000000000000")
		(b1 / 2).to_s == "10000000000000000000"
	`)

	// Comparisons
	assrt(t, `
		b1 = Bignum.new("100")
		b2 = Bignum.new("200")
		b1 < b2 && b2 > b1 && b1 == Bignum.new(100)
	`)

	// Fixnum + Bignum
	assrt(t, `
		b = Bignum.new("200")
		res = 100 + b
		res.is_a?(Bignum) && res.to_s == "300"
	`)
}
