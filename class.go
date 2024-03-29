package oruby

// #include "go-mrb.h"
import "C"
import (
	"reflect"
	"unsafe"
)

// RClassPtr holds single C API RClass pointer. It is used in call refs
type RClassPtr struct{ p *C.struct_RClass }

// MrbMethodT oruby method pointer
// type MrbMethodT = uintptr
type MrbMethodT = struct{ m C.mrb_method_t }

// RClass struct for oruby class
type RClass struct {
	p   *C.struct_RClass
	mrb *MrbState
}

// Value to satisfy MrbValue interface
func (c RClass) Value() Value { return mrbObjValue(unsafe.Pointer(c.p)) }

// Type for MrbValue interface
func (c RClass) Type() Type { return c.Value().Type() }

// IsNil for MrbValue interface
func (c RClass) IsNil() bool { return c.p == nil }

func (c RClass) String() string { return c.Name() }

// Ptr for MrbValue interface
func (c RClass) Ptr() RClassPtr { return RClassPtr{c.p} }

// MrbClassPtr returns RClassPtr struct (without mrb reference) from MrbValue
func MrbClassPtr(v MrbValue) RClassPtr { return RClassPtr{(*C.struct_RClass)(C._mrb_ptr(v.Value().v))} }

// RCLASS returns RClass struct (without mrb reference) from MrbValue
func RCLASS(v MrbValue) RClassPtr { return RClassPtr{(*C.struct_RClass)(C._mrb_ptr(v.Value().v))} }

// MrbClassValue converts RClass to Value interface
func MrbClassValue(c RClass) Value { return c.Value() }

// RClassSuper returns super class
func RClassSuper(c RClass) RClass { return RClass{c.p.super, c.mrb} }

// ClassOf returns class of value as RClass
// Note: this function calls mrb_class API
func (mrb *MrbState) ClassOf(v MrbValue) RClass {
	return RClass{C.mrb_class(mrb.p, v.Value().v), mrb}
}

// ClassPtr returns class of value as RClass
func (mrb *MrbState) ClassPtr(v Value) RClass {
	if v.Type() == MrbTTClass || v.Type() == MrbTTModule || v.Type() == MrbTTSClass {
		return RClass{(*C.struct_RClass)(C._mrb_ptr(v.Value().v)), mrb}
	}
	return RClass{C.mrb_class(mrb.p, v.v), mrb}
}

// Class returns class of value as RClass
func (mrb *MrbState) Class(name string, outer ...RClass) RClass {
	if len(outer) == 0 {
		return mrb.ClassGet(name)
	}
	return mrb.ClassGetUnder(outer[0], name)
}

// State returns MrbState from class
func (c RClass) State() *MrbState { return c.mrb }

const (
	MrbFlClassIsPrepended = 1 << 19
	MrbFlClassIsOrigin    = 1 << 18
	MrbFlClassIsInherited = 1 << 17
	MrbTTINstanceMask     = 0xFF
)

func (c RClass) Origin() RClass {
	return RClass{C._mrb_class_origin(c.p), c.mrb}
}

// MrbSetInstanceTT sets instance type
func MrbSetInstanceTT(c RClass, tt Type) { C._MRB_SET_INSTANCE_TT(c.p, C.uint32_t(tt)) }

// MrbInstanceTT returns class instance type
func MrbInstanceTT(c RClass) Type { return Type(C._MRB_INSTANCE_TT(c.p)) }

// DefineClassID define class by symbol
func (mrb *MrbState) DefineClassID(id MrbSym, c RClass) RClass {
	return RClass{C.mrb_define_class_id(mrb.p, C.mrb_sym(id), c.p), mrb}
}

// DefineModuleID define module by symbol
func (mrb *MrbState) DefineModuleID(id MrbSym) RClass {
	return RClass{C.mrb_define_module_id(mrb.p, C.mrb_sym(id)), mrb}
}

// DefineMethodRaw define method via symbol and RProc
func (mrb *MrbState) DefineMethodRaw(c RClass, id MrbSym, methodID MrbMethodT) {
	C.mrb_define_method_raw(mrb.p, c.p, C.mrb_sym(id), methodID.m)
}

// DefineMethodID define method via symbol and function type
func (mrb *MrbState) DefineMethodID(c RClass, mid MrbSym, f MrbFuncT, aspec MrbAspec) {
	idx := mrb.registerFuncIndex(f)
	C._mrb_method_new_cfunc(mrb.p, c.p, C.mrb_sym(mid), C.int(idx), C.mrb_aspec(aspec))
}

// DefineMethodFuncID Define method as oruby func
func (mrb *MrbState) DefineMethodFuncID(c RClass, mid MrbSym, f interface{}) {
	var env int // C.mrb_value
	aspec := ArgsNone()
	v := reflect.ValueOf(f)

	if v.Kind() == reflect.Func {
		env = mrb.registerFuncIndex(f)
		t := v.Type()

		opt := 0
		for i := t.NumIn() - 1; i >= 0; i-- {
			if t.In(i).Kind() != reflect.Ptr {
				break
			}
			opt++
		}

		aspec = mrb.ArgsArg(uint32(t.NumIn()-opt), uint32(opt))
	} else {
		env = mrb.registerFuncIndex(func() interface{} { return f })
	}

	C._mrb_proc_new_cfunc(mrb.p, c.p, C.mrb_sym(mid), C.int(env), C.mrb_aspec(aspec))
	// C.mrb_define_method_id() never called
}

// DefineClassFuncID define class func
func (mrb *MrbState) DefineClassFuncID(klass RClass, mid MrbAspec, f interface{}) {
	// TODO: DefineClassFuncID
}

// AliasMethod creates method alias
func (mrb *MrbState) AliasMethod(c RClass, a, b MrbSym) {
	C.mrb_alias_method(mrb.p, c.p, C.mrb_sym(a), C.mrb_sym(b))
}

// RemoveMethod removes method from class
func (mrb *MrbState) RemoveMethod(c RClass, sym MrbSym) {
	C.mrb_remove_method(mrb.p, c.p, C.mrb_sym(sym))
}

// MethodSearchVM finds VM method, method is invalid if not found
func (mrb *MrbState) MethodSearchVM(cl RClass, id MrbSym) MrbMethodT {
	return MrbMethodT{C.mrb_method_search_vm(mrb.p, &(cl.p), C.mrb_sym(id))}
}

// MethodSearch find method using symbol, and error if not found
func (mrb *MrbState) MethodSearch(cl RClass, id MrbSym) (MrbMethodT, error) {
	m := mrb.MethodSearchVM(cl, id)
	if C._MRB_METHOD_UNDEF_P(C.mrb_method_t(m.m)) != false {
		return m, ENameError("undefined method '%v' for class %v", id, cl.Name())
	}
	return m, nil
	// C.mrb_method_search() never called as it raises C side exception
}

// MethodExists find method using symbol, and error if not found
func (mrb *MrbState) MethodExists(cl RClass, id MrbSym) bool {
	m := mrb.MethodSearchVM(cl, id)
	return C._MRB_METHOD_UNDEF_P(C.mrb_method_t(m.m)) == false
}

// ClassReal returns real class
func (mrb *MrbState) ClassReal(cl RClass) RClass {
	return RClass{C.mrb_class_real(cl.p), mrb}
}
