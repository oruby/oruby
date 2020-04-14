package complex

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"reflect"
	"testing"
)

func assrt(t *testing.T, code string) {
	t.Helper()

	mrb, _ := oruby.New()
	defer mrb.Close()

	result, err := mrb.Eval(code)
	if err != nil {
		t.Fatal(code, ": ", err)
	}

	if !result.IsNil() && !oruby.MrbBoolean(result) {
		t.Error(code, ": result =", mrb.Intf(result))
	}
}

func expect(t *testing.T, condition bool, eformat string, args ...interface{}) {
	t.Helper()
	if !condition {
		t.Errorf(eformat, args...)
	}
}

func expectEql(t *testing.T, v1, v2 interface{}) {
	t.Helper()
	expect(t, reflect.DeepEqual(v1, v2), "Expected '%v' to equal '%v'", v1, v2)
}

func TestComplex(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v := mrb.Value(complex(11.11, 88.88))
	expectEql(t, mrb.Value(v), v)

	assrt(t, `c = 123i; Complex == c.class`)
	assrt(t, `c = 123i; [c.real, c.imaginary] == [0, 123]`)
	assrt(t, `	
		c = 123 + -1.23i
		[c.real, c.imaginary] == [123, -1.23]
	`)
}

func TestComplex2(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	assrt(t, `c = 123i; [c.real, c.imaginary] == [0, 123]`)
}

func TestComplex3(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	assert.AssertFile(t, mrb,"complex_test.rb")
}

func TestComplexRect(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	assrt(t, `Complex(5, 6).rectangular == [5, 6]`)
}
