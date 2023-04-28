package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"unsafe"
)

// RRange struct
type RRange struct {
	RBasic
}

func (r RRange) ptr() *C.struct_RRange { return (*C.struct_RRange)(unsafe.Pointer(r.p)) }

// MrbRangePtr retrieve RRange from oruby value
func MrbRangePtr(r MrbValue) RRange {
	return RRange{RBasic{(*C.struct_RBasic)(C._mrb_ptr(r.Value().v))}}
}

// Begin value of range
func (r RRange) Begin() Value { return Value{C._RANGE_BEG(r.ptr())} }

// End value of range
func (r RRange) End() Value { return Value{C._RANGE_END(r.ptr())} }

// Exclusive is true if range excludes end value
func (r RRange) Exclusive() bool { return C._RANGE_EXCL(r.ptr()) != false }

// RangeNew creates new range, n include or not end value
func (mrb *MrbState) RangeNew(v1, v2 MrbValue, n bool) Value {
	return Value{C.mrb_range_new(mrb.p, v1.Value().v, v2.Value().v, iifmb(n))}
}

// MrbRangePtr retrieve RRange from Value. This method returns error if range is uninitialized
func (mrb *MrbState) MrbRangePtr(r MrbValue) (RRange, error) {
	const RangeInitializedFlag = uint32(1)
	b := mrb.RBasicPtr(r)
	ret := RRange{}

	if b.IsNil() || !b.TestFlag(RangeInitializedFlag) {
		return ret, EArgumentError("uninitialized range")
	}

	ret.RBasic = *b
	return ret, nil
}

// RangeBegLen return values
const (
	MrbRangeTypeMismatch = 0 // (failure) not range
	MrbRangeOK           = 1 // (success) range
	MrbRangeOut          = 2 // (failure) out of range
)

// RangeBegLen convets rangeto starting index and length for given array length
//
//   - r must be Range value
//
//   - length of target array
//
//   - trunc truncates to length if range is larger
//
//     returns error if r is not range value, or range is out of given length
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
		err = errors.New("value is not RRange type")
	case MrbRangeOut:
		err = errors.New("out of range")
	default:
		err = nil
	}
	return int(begp), int(lenp), err
}
