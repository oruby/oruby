package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

// RBasic represents oruby BasicObject
type RBasic struct {
	p *C.struct_RBasic
}

// Value  MrbValue interface implementation for RBasic
func (b RBasic) Value() Value {
	if b.p == nil {
		return nilValue
	}
	return Value{C.mrb_obj_value(unsafe.Pointer(b.p))}
}

// Type implements MrbValue interface
func (b RBasic) Type() Type {
	return Type(C._mrb_basic_type(b.p))
}

// IsNil check for MrbValue interface
func (b RBasic) IsNil() bool {
	return b.p == nil
}

// IsFrozen return true if object is frozen
func (b RBasic) IsFrozen() bool { return C._mrb_basic_frozen(b.p) != false }

// SetFrozen sets or unsets frozen flag
func (b RBasic) SetFrozen(v bool) {
	if v {
		C._MRB_SET_FROZEN_FLAG(b.p)
	} else {
		C._MRB_UNSET_FROZEN_FLAG(b.p)
	}
}

// Flags return object flags
func (b RBasic) Flags() uint32 { return uint32(C._mrb_basic_flags(b.p)) }

func (b RBasic) TestFlag(flag uint32) bool {
	return uint32(C._mrb_basic_flags(b.p))&flag != 0
}

// MrbTestFlag test if MrbValue has given flag set
func MrbTestFlag(obj MrbValue, flag uint32) bool { return obj.Value().TestFlag(flag) }

// MrbBasicPtr API
func MrbBasicPtr(v MrbValue) RBasic {
	return RBasic{(*C.struct_RBasic)(C._mrb_ptr(v.Value().v))}
}

// RBasic return pointer to BasicObject if it exists
func (v Value) RBasic() RBasic {
	if !v.HasBasic() {
		return RBasic{nil}
	}
	return RBasic{(*C.struct_RBasic)(C._mrb_ptr(v.v))}
}

// RBasicPtr returns RBasic object pointer from value
func (mrb *MrbState) RBasicPtr(v MrbValue) *RBasic {
	if v.Type() <= MrbTTCptr {
		return nil
	}
	return &RBasic{(*C.struct_RBasic)(C._mrb_ptr(v.Value().v))}
}

// RObject represents RObject pointer
type RObject struct{ p *C.struct_RObject }

// RValue represents oruby value with internal reference to state
type RValue struct {
	v   C.mrb_value
	mrb *MrbState
}

// Value implements MrbValue interface for RValue
func (obj RValue) Value() Value { return Value{obj.v} }

// Type implements MrbValue interface
func (obj RValue) Type() Type { return Type(C._mrb_type(obj.v)) }

// IsNil check for MrbValue interface
func (obj RValue) IsNil() bool { return C._mrb_is_nil(obj.v) != false }

// Flags return object flags
func (obj RValue) Flags() uint32 { return uint32(C._mrb_value_flags(obj.v)) }

// Interface returns Go interface from RValue
func (obj RValue) Interface() interface{} {
	return obj.mrb.Intf(obj)
}

// MigrateTo implements ValueMigrator interface for RString
func (obj RValue) MigrateTo(mrb2 *MrbState) Value {
	v2 := obj.Interface()
	return mrb2.Value(v2)
}

// MrbFlObjIsFrozen frozen flag
const MrbFlObjIsFrozen = 1 << 20

// MrbFrozenP check if object is frozen
func MrbFrozenP(v MrbValue) bool {
	value := v.Value()

	if !value.HasBasic() {
		return true
	}
	return value.RBasic().IsFrozen()
}

// MrbSetFrozenFlag check if object is frozen
func MrbSetFrozenFlag(v MrbValue) {
	value := v.Value()
	if !value.HasBasic() {
		return
	}

	C._MRB_SET_FROZEN_FLAG(value.RBasic().p)
}

// MrbUnsetFrozenFlag check if object is frozen
func MrbUnsetFrozenFlag(v MrbValue) {
	value := v.Value()
	if !value.HasBasic() {
		return
	}

	C._MRB_UNSET_FROZEN_FLAG(value.RBasic().p)
}

// MrbObjPtr returns uintptr of MrbValue object
// if MrbValue has RBasic - actual pointer is returned,
// if it is simple value, like int, uintptr(0) is returned
func MrbObjPtr(v MrbValue) uintptr {
	if !v.Value().HasBasic() {
		return 0
	}
	return uintptr(C._mrb_ptr(v.Value().v))
}

// Ptr returns pointer to mruby object if it exists in value
func (v Value) Ptr() uintptr {
	if !v.HasBasic() {
		return 0
	}
	return uintptr(C._mrb_ptr(v.v))
}

// MrbSpecialConstP alias for immediateP
func MrbSpecialConstP(x MrbValue) bool { return MrbImmediateP(x) }

// RFiber represents oruby Fiber object
type RFiber struct {
	RBasic
}

func (f RFiber) ptr() *C.struct_RFiber {
	return (*C.struct_RFiber)(unsafe.Pointer(f.p))
}
