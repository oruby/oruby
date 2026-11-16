package oruby

// #include "go-mrb.h"
import "C"
import (
	"math/big"
	"unsafe"
)

// POSFIXABLE is positive number fixable
func POSFIXABLE(f int) bool { return (f <= C.MRB_INT_MAX) }

// NEGFIXABLE is negative number fixable
func NEGFIXABLE(f int) bool { return (f >= C.MRB_INT_MIN) }

// FIXABLE is number fixable
func FIXABLE(f int) bool { return POSFIXABLE(f) && NEGFIXABLE(f) }

// FloatToInteger converts float to fixnum
func (mrb *MrbState) FloatToInteger(val MrbValue) Value {
	return Value{C.mrb_float_to_integer(mrb.p, val.Value().v)}
}

// FixnumToStr convert fixnum to strng
func (mrb *MrbState) IntegerToStr(x MrbValue, base int) Value {
	return Value{C.mrb_integer_to_str(mrb.p, x.Value().v, C.mrb_int(base))}
}

// NumAdd fixnum addition
func (mrb *MrbState) NumAdd(x, y MrbValue) Value {
	return Value{C.mrb_num_add(mrb.p, x.Value().v, y.Value().v)}
}

// NumSub fixnum substraction
func (mrb *MrbState) NumSub(x, y MrbValue) Value {
	return Value{C.mrb_num_sub(mrb.p, x.Value().v, y.Value().v)}
}

// NumMul fixnum multiplication
func (mrb *MrbState) NumMul(x, y MrbValue) Value {
	return Value{C.mrb_num_mul(mrb.p, x.Value().v, y.Value().v)}
}

// FloatToStr convert fixnum to string
func (mrb *MrbState) FloatToStr(x MrbValue, fmt string) Value {
	cfmt := C.CString(fmt)
	defer C.free(unsafe.Pointer(cfmt))
	return Value{C.mrb_float_to_str(mrb.p, x.Value().v, cfmt)}
}

// IntegerToBytes returns the magnitude of Integer x as big-endian bytes,
// leading zeroes dropped, and its sign (-1, 0 or 1).
func (mrb *MrbState) IntegerToBytes(x MrbValue) ([]byte, int, error) {
	v := x.Value()
	if t := v.Type(); t != MrbTTInteger && t != MrbTTBigInt {
		return nil, 0, ETypeError("%s is not an Integer", mrb.TypeName(v))
	}
	var sign C.int
	n := C.mrb_integer_to_bytes(mrb.p, v.v, nil, 0, &sign)
	buf := make([]byte, int(n))
	if n > 0 {
		C.mrb_integer_to_bytes(mrb.p, v.v, (*C.uint8_t)(unsafe.Pointer(&buf[0])), n, &sign)
	}
	return buf, int(sign), nil
}

// IntegerFromBytes returns the Integer whose magnitude is the big-endian
// bytes b and whose sign is sign (-1, 0 or 1); a bigint when it does not fit
// mrb_int.
func (mrb *MrbState) IntegerFromBytes(b []byte, sign int) Value {
	var p *C.uint8_t
	if len(b) > 0 {
		p = (*C.uint8_t)(unsafe.Pointer(&b[0]))
	}
	return Value{C.mrb_integer_from_bytes(mrb.p, p, C.size_t(len(b)), C.int(sign))}
}

// BigInt converts a Ruby Integer (fixnum or bigint) to a Go *big.Int.
func (mrb *MrbState) BigInt(x MrbValue) (*big.Int, error) {
	v := x.Value()
	if v.Type() == MrbTTInteger {
		return big.NewInt(v.Int64()), nil
	}
	b, sign, err := mrb.IntegerToBytes(v)
	if err != nil {
		return nil, err
	}
	z := new(big.Int).SetBytes(b)
	if sign < 0 {
		z.Neg(z)
	}
	return z, nil
}

// BigIntValue converts a Go *big.Int to a Ruby Integer.
func (mrb *MrbState) BigIntValue(z *big.Int) Value {
	if z.IsInt64() {
		return mrb.FixnumValue(int(z.Int64()))
	}
	return mrb.IntegerFromBytes(z.Bytes(), z.Sign())
}
