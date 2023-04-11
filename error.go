package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

// RException struct
type RException struct {
	p   *C.struct_RException
	mrb *MrbState
}

// RExceptionPtr struct wrapper around mruby RException struct pointer
type RExceptionPtr struct{ p *C.struct_RException }

const (
	MrbExcExit = 65536
)

func (e RException) Value() Value {
	return mrbObjValue(unsafe.Pointer(e.p))
}
func (e RException) Type() int {
	return e.RBasic().Type()
}

func (e RException) RBasic() RBasic {
	return RBasic{(*C.struct_RBasic)(unsafe.Pointer(e.p))}
}

func (e RException) RObject() RObject {
	return RObject{mrbObjValue(unsafe.Pointer(e.p)).v, e.mrb}
}

func (e RException) IsNil() bool {
	return e.p == nil
}

func (e RException) ExcExit() bool {
	return e.RBasic().Flags()&MrbExcExit != 0
}

func (e RException) ExitStatus() int {
	return e.mrb.IVGet(e, e.mrb.Intern("status")).Int()
}

// RBreak struct
type RBreak struct {
	p   *C.struct_RBreak
	mrb *MrbState
}

// RBreakPtr struct
type RBreakPtr struct{ p *C.struct_RBreak }

func (b RBreak) Value() Value {
	return mrbObjValue(unsafe.Pointer(b.p))
}
func (b RBreak) Type() int {
	return b.RBasic().Type()
}

func (b RBreak) IsNil() bool {
	return b.p == nil
}

func (b RBreak) RBasic() RBasic {
	return RBasic{(*C.struct_RBasic)(unsafe.Pointer(b.p))}
}

// ValueGet gets break value
func (b RBreak) ValueGet() Value {
	return Value{b.p.val}
}

// ValueSet sets break value
func (b RBreak) ValueSet(v MrbValue) {
	b.p.val = v.Value().v
}

func (b RBreak) ProcGet() RProc {
	return RProc{b.p.proc, b.mrb}
}

// ProcSet sets break value
func (b RBreak) ProcSet(p RProc) {
	b.p.proc = p.p
}

// MrbExcPtr returns RException
func MrbExcPtr(v MrbValue) RExceptionPtr {
	return RExceptionPtr{(*C.struct_RException)(C._mrb_ptr(v.Value().v))}
}

// SysFail return SystemCallError with message
func (mrb *MrbState) SysFail(err error) Value {
	if mrb.ClassDefined("SystemCallError") {
		// mruby: if class SystemCallError exists, return SystemCallError._sys_fail(no, mesg)
		// oruby: Go handles system call errors, with messages
		return mrb.Raise(mrb.ClassGet("SystemCallError"), err.Error())
	}

	return mrb.Raise(mrb.ERuntimeError(), err.Error())

	// C.mrb_sys_fail(mrb.p, cmesg) never called
}

// ExcNewStr create new exception
func (mrb *MrbState) ExcNewStr(c RClass, str MrbValue) Value {
	return Value{C.mrb_exc_new_str(mrb.p, c.p, str.Value().v)}
}

// MakeException from go values
func (mrb *MrbState) MakeException(args ...interface{}) Value {
	l := len(args)

	if l == 0 {
		return Value{C.mrb_make_exception(mrb.p, 0, nil)}
	}

	argv := make([]C.mrb_value, l)
	for i := range argv {
		argv[i] = mrb.Value(args[i]).Value().v
	}

	return Value{C.mrb_make_exception(mrb.p, (C.mrb_int)(l), (*C.mrb_value)(&argv[0]))}
}

//func (mrb *MrbState) NoMethodError(mid MrbSym, fmt string,  ) {
//   TODO: mrb_no_method_error
//	 C.mrb_no_method_error(mrb_state *mrb, mrb_sym id, mrb_int argc, const mrb_value *argv, const char *fmt, ...);
//}

// FRaise declaration for fail method
func (mrb *MrbState) FRaise(v MrbValue) Value { return Value{C.mrb_f_raise(mrb.p, v.Value().v)} }

// Protect implemented in the oruby-error mrbgem
//func (mrb *MrbState) Protect(body MrbFuncT, data MrbValue, state *bool) MrbValue {
//  cstate := C.mrb_bool(*state)
//  result := C.mrb_protect(mrb.p, mrb_func_t body, data.v, &cstate);
//  state := bool(cstate)
//  return result
//}

// Ensure implemented in the oruby-error mrbgem
//func (mrb *MrbState) Ensure() MrbValue {
//
//}

// Rescue implemented in the oruby-error mrbgem
//func (mrb *MrbState) Rescue() MrbValue {
//
//}

// RescueExceptions implemented in the oruby-error mrbgem
//func (mrb *MrbState) RescueExceptions() MrbValue {
//
//}
