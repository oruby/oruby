package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

type (
	// RBasic represents oruby BasicObject
	RBasic struct{ p *C.struct_RBasic }

	// RObject represents oruby Object
	RObject struct {
		v   C.mrb_value
		mrb *MrbState
	}

	// RFiber repesents oruby Fiber object
	RFiber struct{ p *C.struct_RFiber }
)

// MrbValue interface implementation for RObject
// Value{C.mrb_obj_value(unsafe.Pointer(o.p))}
func (obj RObject) Value() Value { return Value{obj.v} }
func (obj RObject) Type() int    { return int(C._mrb_type(obj.v)) }
func (obj RObject) IsNil() bool  { return C._mrb_is_nil(obj.v) != 0 }

// MrbValue interface implementation for RBasic
func (b RBasic) Value() Value { return Value{C.mrb_obj_value(unsafe.Pointer(b.p))} }
func (b RBasic) Type() int    { return b.Value().Type() }
func (b RBasic) IsNil() bool  { return b.p == nil }

// Value implements MrbValue interface
func (f RFiber) Value() Value { return Value{C.mrb_obj_value(unsafe.Pointer(f.p))} }
func (f RFiber) Type() int    { return f.Value().Type() }
func (f RFiber) IsNil() bool  { return f.p == nil }

// ToValue implements Valuer interface for RString
func (obj RObject) ToValue(*MrbState) MrbValue { return obj }

// func MRB_FLAG_TEST(obj, flag) ((obj)->flags & (flag)
// TODO: test_flag

// MrbBasicPtr API
func MrbBasicPtr(v MrbValue) RBasic {
	return RBasic{(*C.struct_RBasic)(C._mrb_ptr(v.Value().v))}
}

// RBASIC return pointer to BasicObject if it exists
func RBASIC(v MrbValue) RBasic {
	if !v.Value().HasBasic() {
		return RBasic{nil}
	}
	return RBasic{(*C.struct_RBasic)(C._mrb_ptr(v.Value().v))}
}

// RBasic return pointer to BasicObject if it exists
func (v Value) RBasic() RBasic {
	if !v.HasBasic() {
		return RBasic{nil}
	}
	return RBasic{(*C.struct_RBasic)(C._mrb_ptr(v.v))}
}

// BasicPtr returns RBasic object pointer from value
func (mrb *MrbState) BasicPtr(v MrbValue) *RBasic {
	if !v.Value().HasBasic() {
		return nil
	}
	return &RBasic{(*C.struct_RBasic)(C._mrb_ptr(v.Value().v))}
}

// MrbFlObjIsFrozen frozen flag
const MrbFlObjIsFrozen = 1 << 20

// MrbFrozenP check if object is frozen
func MrbFrozenP(v MrbValue) bool {
	if !v.Value().HasBasic() {
		return true
	}
	return C._mrb_basic_frozen(RBASIC(v).p) != 0
}

// MrbSetFrozenFlag check if object is frozen
func MrbSetFrozenFlag(v MrbValue) {
	if !v.Value().HasBasic() {
		return
	}

	C._MRB_SET_FROZEN_FLAG(v.Value().v)
}

// MrbUnsetFrozenFlag check if object is frozen
func MrbUnsetFrozenFlag(v MrbValue) {
	if !v.Value().HasBasic() {
		return
	}

	C._MRB_UNSET_FROZEN_FLAG(v.Value().v)
}

// MrbObjPtr returns uintptr of RObject struct,
// please use RObject where oruby object is needed
func MrbObjPtr(v MrbValue) uintptr {
	if !v.Value().HasBasic() {
		return 0
	}
	return uintptr(C._mrb_ptr(v.Value().v))
}

// RObjectPtr returns pointer to Object if it exists in value
func (v Value) RObjectPtr() uintptr {
	if !v.Value().HasBasic() {
		return 0
	}
	return uintptr(C._mrb_ptr(v.v))
}

// MrbSpecialConstP alias for immediateP
func MrbSpecialConstP(x MrbValue) bool { return MrbImmediateP(x) }
