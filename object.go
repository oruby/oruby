package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

// RBasic represents oruby BasicObject
type RBasic struct{ p *C.struct_RBasic }

// RObject represents oruby Object
type RObject struct {
	v   C.mrb_value
	mrb *MrbState
}

// RObjectPtr represents oruby RObject pointer
type RObjectPtr struct{ p *C.struct_RObject }

// RFiber repesents oruby Fiber object
type RFiber struct{ p *C.struct_RFiber }

// Value implements MrbValue interface for RObject
func (obj RObject) Value() Value { return Value{obj.v} }

// Type implements MrbValue interface
func (obj RObject) Type() int { return int(C._mrb_type(obj.v)) }

// IsNil check for MrbValue interface
func (obj RObject) IsNil() bool { return C._mrb_is_nil(obj.v) != 0 }

// Flags return object flags
func (obj RObject) Flags() int { return int(C._mrb_value_flags(obj.v)) }

// Value  MrbValue interface implementation for RBasic
func (b RBasic) Value() Value { return mrbObjValue(unsafe.Pointer(b.p)) }

// Type implements MrbValue interface
func (b RBasic) Type() int { return b.Value().Type() }

// IsNil check for MrbValue interface
func (b RBasic) IsNil() bool { return b.p == nil }

// IsFrozen return true if object is frozen
func (b RBasic) IsFrozen() bool { return C._mrb_basic_frozen(b.p) != 0 }

// Flags return object flags
func (b RBasic) Flags() int { return int(C._mrb_basic_flags(b.p)) }

// Value implements MrbValue interface
func (f RFiber) Value() Value { return mrbObjValue(unsafe.Pointer(f.p)) }

// Type implements MrbValue interface
func (f RFiber) Type() int { return f.Value().Type() }

// IsNil check for MrbValue interface
func (f RFiber) IsNil() bool { return f.p == nil }

// Interface returns Go interface from RObject
func (obj RObject) Interface() interface{} {
	return obj.mrb.Intf(obj)
}

// MigrateTo implements ValueMigrator interface for RString
func (obj RObject) MigrateTo(mrb2 *MrbState) Value {
	v2 := obj.Interface()
	return mrb2.Value(v2)
}

// MrbTestFlag test if MrbValue has given flag set
func MrbTestFlag(obj MrbValue, flag int) bool { return obj.Value().TestFlag(flag) }

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

// RBasicPtr returns RBasic object pointer from value
func (mrb *MrbState) RBasicPtr(v MrbValue) *RBasic {
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

// Ptr returns pointer to mruby object if it exists in value
func (v Value) Ptr() uintptr {
	if !v.Value().HasBasic() {
		return 0
	}
	return uintptr(C._mrb_ptr(v.v))
}

// MrbSpecialConstP alias for immediateP
func MrbSpecialConstP(x MrbValue) bool { return MrbImmediateP(x) }
