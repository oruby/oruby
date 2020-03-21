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
//type MrbMethodT = uintptr
type MrbMethodT = struct { m C.mrb_method_t }

// RClass struct for oruby class
type RClass struct {
	p   *C.struct_RClass
	mrb *MrbState
}

// Value to satisfy MrbValue interface
func (c RClass) Value() Value { return mrbObjValue(unsafe.Pointer(c.p)) }

// Type for MrbValue interface
func (c RClass) Type() int { return c.Value().Type() }

// IsNil for MrbValue interface
func (c RClass) IsNil() bool { return c.p == nil }

func (c RClass) String() string { return c.Name() }

// Ptr for MrbValue interface
func (c RClass) Ptr() RClassPtr { return RClassPtr{c.p} }

// Ptr for MrbValue interface
func (c RClass) RObjectPtr() RObjectPtr { return RObjectPtr{(*C.struct_RObject)(unsafe.Pointer(c.p))} }

// MrbClassPtr returns RClassPtr struct (without mrb reference) from MrbValue
func MrbClassPtr(v MrbValue) RClassPtr { return RClassPtr{(*C.struct_RClass)(C._mrb_ptr(v.Value().v))} }

// RCLASS returns RClass struct (without mrb reference) from MrbValue
func RCLASS(v MrbValue) RClassPtr { return RClassPtr{(*C.struct_RClass)(C._mrb_ptr(v.Value().v))} }

// MrbClassValue converts RClass to Value interface
func MrbClassValue(c RClass) Value { return c.Value() }

// RClassSuper returns super class
func RClassSuper(c RClass) RClass { return RClass{c.p.super, c.mrb} }

// ClassOf returns class of value as RClass
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

// MrbSetInstanceTT sets instance type
func MrbSetInstanceTT(c RClass, tt uint32) { C._MRB_SET_INSTANCE_TT(c.p, C.uint32_t(tt)) }

// MrbInstanceTT returns class instance type
func MrbInstanceTT(c RClass) uint32 { return uint32(C._MRB_INSTANCE_TT(c.p)) }

// DefineClassID define class by symbol
func (mrb *MrbState) DefineClassID(id MrbSym, c RClass) RClass {
	return RClass{C.mrb_define_class_id(mrb.p, C.mrb_sym(id), c.p), mrb}
}

// DefineModuleID define module by symbol
func (mrb *MrbState) DefineModuleID(id MrbSym) RClass {
	return RClass{C.mrb_define_module_id(mrb.p, C.mrb_sym(id)), mrb}
}

// VMDefineClass define VM class
func (mrb *MrbState) VMDefineClass(v1, v2 MrbValue, id MrbSym) RClass {
	return RClass{C.mrb_vm_define_class(mrb.p, v1.Value().v, v2.Value().v, C.mrb_sym(id)), mrb}
}

// VMDefineModule define VM module
func (mrb *MrbState) VMDefineModule(v MrbValue, id MrbSym) RClass {
	return RClass{C.mrb_vm_define_module(mrb.p, v.Value().v, C.mrb_sym(id)), mrb}
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
		for i := t.NumIn() - 1; i >= 0; i--  {
			if t.In(i).Kind() != reflect.Ptr {
				break
			}
			opt++
		}

		aspec = mrb.ArgsArg(t.NumIn()-opt, opt)
	} else {
		env = mrb.registerFuncIndex(func() interface{} { return f })
	}

	C._mrb_proc_new_cfunc(mrb.p, c.p, C.mrb_sym(mid), C.int(env), C.mrb_aspec(aspec))
}

// DefineClassFuncID define class func
func (mrb *MrbState) DefineClassFuncID(klass RClass, mid MrbAspec, f interface{}) {

}

// AliasMethod creates method alias
func (mrb *MrbState) AliasMethod(c RClass, a, b MrbSym) {
	C.mrb_alias_method(mrb.p, c.p, C.mrb_sym(a), C.mrb_sym(b))
}

// MethodSearchVM finds VM method, method is invalid if not found
func (mrb *MrbState) MethodSearchVM(cl RClass, id MrbSym) MrbMethodT {
	return MrbMethodT{C.mrb_method_search_vm(mrb.p, &(cl.p), C.mrb_sym(id))}
}

// MethodSearch find method using symbol, and error if not found
func (mrb *MrbState) MethodSearch(cl RClass, id MrbSym) (MrbMethodT, error) {
	var m MrbMethodT
	err := mrb.tryE(func() {
		m = MrbMethodT{C.mrb_method_search(mrb.p, cl.p, C.mrb_sym(id))}
	})
	return m, err
}

// MethodExists find method using symbol, and error if not found
func (mrb *MrbState) MethodExists(cl RClass, id MrbSym) bool {
	m := mrb.MethodSearchVM(cl, id)
	return C._MRB_METHOD_UNDEF_P(C.mrb_method_t(m.m)) == 0
}
