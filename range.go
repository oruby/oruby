package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"unsafe"
)

// RRange struct
type RRange struct{ p *C.struct_RRange }

// Value implements MrbValue interface
func (r RRange) Value() Value { return mrbObjValue(unsafe.Pointer(r.p)) }

// Type for MrbValue interface
func (r RRange) Type() int { return MrbTTRange }

// IsNil check for MrbValue interface
func (r RRange) IsNil() bool { return r.p == nil }

// MrbRangePtr retreive RRange from oruby value
func MrbRangePtr(r MrbValue) RRange { return RRange{(*C.struct_RRange)(C._mrb_ptr(r.Value().v))} }

// RangeValue returns oruby value from RRange
func RangeValue(r RRange) MrbValue { return r.Value() }

// Begin value of range
func (r RRange) Begin() Value { return Value{C._RANGE_BEG(r.p)} }

// End value of range
func (r RRange) End() Value { return Value{C._RANGE_END(r.p)} }

// Exclusive is true if range excludes end value
func (r RRange) Exclusive() bool { return C._RANGE_EXCL(r.p) != 0 }

// RangeNew creates new range, n include or not end value
func (mrb *MrbState) RangeNew(v1, v2 MrbValue, n bool) Value {
	return Value{C.mrb_range_new(mrb.p, v1.Value().v, v2.Value().v, iifmb(n))}
}

// RangeBegLen return values
const (
	MrbRangeTypeMismatch = 0 // (failure) not range
	MrbRangeOK           = 1 // (success) range
	MrbRangeOut          = 2 // (failure) out of range
)

// RangeBegLen convets rangeto starting index and length for given array length
//   * r must be Range value
//   * length of target array
//   * trunc truncates to length if range is larger
//
//  returns error if r is not range value, or range is out of given length
func (mrb *MrbState) RangeBegLen(r MrbValue, length int, trunc bool) (int, int, error) {
	var err error
	begp := C.mrb_int(0)
	lenp := C.mrb_int(0)

	ret := int(C.mrb_range_beg_len(
		mrb.p,
		r.Value().v,
		&begp,
		&lenp,
		C.mrb_int(length),
		iifmb(trunc),
	))

	switch ret {
	case MrbRangeTypeMismatch:
		err = errors.New("Value is not RRange type")
	case MrbRangeOut:
		err = errors.New("Out of range")
	default:
		err = nil
	}
	return int(begp), int(lenp), err
}
