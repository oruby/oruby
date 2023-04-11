package oruby

// #include "go-mrb.h"
import "C"
import (
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
