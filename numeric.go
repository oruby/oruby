package oruby

// #include "go-mrb.h"
import "C"
import (
	"strings"
	"unsafe"
)

// POSFIXABLE is positive number fixable
func POSFIXABLE(f int) bool { return (f <= C.MRB_INT_MAX) }

// NEGFIXABLE is negative number fixable
func NEGFIXABLE(f int) bool { return (f >= C.MRB_INT_MIN) }

// FIXABLE is number fixable
func FIXABLE(f int) bool { return POSFIXABLE(f) && NEGFIXABLE(f) }

// FloToFixnum converts float to fixnum
func (mrb *MrbState) FloToFixnum(val MrbValue) Value {
	return Value{C.mrb_flo_to_fixnum(mrb.p, val.Value().v)}
}

// FixnumToStr convert fixnum to strng
func (mrb *MrbState) FixnumToStr(x MrbValue, base int) Value {
	return Value{C.mrb_fixnum_to_str(mrb.p, x.Value().v, C.mrb_int(base))}
}

// NumPlus fixnum addition
func (mrb *MrbState) NumPlus(x, y MrbValue) Value {
	return Value{C.mrb_num_plus(mrb.p, x.Value().v, y.Value().v)}
}

// NumMinus fixnum substraction
func (mrb *MrbState) NumMinus(x, y MrbValue) Value {
	return Value{C.mrb_num_minus(mrb.p, x.Value().v, y.Value().v)}
}

// NumMul fixnum multiplication
func (mrb *MrbState) NumMul(x, y MrbValue) Value {
	return Value{C.mrb_num_mul(mrb.p, x.Value().v, y.Value().v)}
}

// ToFlo convert fixnum to float
func (mrb *MrbState) ToFlo(x MrbValue) float64 {
	return float64(C.mrb_to_flo(mrb.p, x.Value().v))
}

// FloatToStr convert fixnum to string
func (mrb *MrbState) FloatToStr(x MrbValue, fmt string) Value {
	cfmt := C.CString(fmt)
	defer C.free(unsafe.Pointer(cfmt))
	return Value{C.mrb_float_to_str(mrb.p, x.Value().v, cfmt)}
}

// FloatToCStr formats float f as string using format fmt
func (mrb *MrbState) FloatToCStr(fmt string, f float64) string {
	const bufSize = 255
	cfmt := C.CString(fmt)
	defer C.free(unsafe.Pointer(cfmt))
	buf := C.CString(strings.Repeat(" ", bufSize))
	defer C.free(unsafe.Pointer(buf))

	l := C.mrb_float_to_cstr(mrb.p, buf, C.size_t(bufSize), cfmt, C.mrb_float(f))

	return C.GoStringN(buf, l)
}
