package oruby

// #include "go-mrb.h"
import "C"

// RException struct
type RException struct{ RValue }

// RBasic returns pointer to mruby basic object
func (e RException) RBasic() RBasic { return RBasic{(*C.struct_RBasic)(C._mrb_ptr(e.v))} }

// RObject returns pointer to mruby object which has instance variables (iv table)
func (e RException) RObject() RObject { return RObject{(*C.struct_RObject)(C._mrb_ptr(e.v))} }

// RExceptionPtr struct wrapper around mruby RException struct pointer
type RExceptionPtr struct{ p *C.struct_RException }

func (e RException) ptr() *C.struct_RException {
	return (*C.struct_RException)(C._mrb_ptr(e.v))
}

const (
	MrbExcExit = 65536
)

func (e RException) ExcExit() bool {
	return e.Flags()&MrbExcExit != 0
}

func (e RException) ExitStatus() int {
	return e.mrb.IVGet(e, e.mrb.Intern("status")).Int()
}

func (e RException) Backtrace() Value {
	return Value{C.mrb_exc_backtrace(e.mrb.p, e.v)}
}

// MrbExcPtr returns RException
func MrbExcPtr(v MrbValue) RExceptionPtr {
	if v.Type() != MrbTTException {
		panic("Exception value expected")
	}
	return RExceptionPtr{(*C.struct_RException)(C._mrb_ptr(v.Value().v))}
}

// RBreak struct
type RBreak struct{ RValue }

// RBreakPtr struct
type RBreakPtr struct{ p *C.struct_RBreak }

func (b RBreak) ptr() *C.struct_RBreak {
	return (*C.struct_RBreak)(C._mrb_ptr(b.v))
}

// ValueGet gets break value
func (b RBreak) ValueGet() Value {
	brk := b.ptr()
	return Value{brk.val}
}

// ValueSet sets break value
func (b RBreak) ValueSet(v MrbValue) {
	brk := b.ptr()
	brk.val = v.Value().v
}

func (b RBreak) ProcGet() RProc {
	brk := b.ptr()
	return RProc{brk.proc, b.mrb}
}

// ProcSet sets break value
func (b RBreak) ProcSet(p RProc) {
	brk := b.ptr()
	brk.proc = p.p
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

func (mrb *MrbState) NoMethodError(methodId MrbSym, args MrbValue, fmt string, fmtArgs ...interface{}) Value {
	ret := mrb.ENoMethodError().Raisef(fmt, fmtArgs...)
	mrb.SetIV(ret, "name", mrb.SymbolValue(methodId))
	mrb.SetIV(ret, "args", args)
	return ret

	// C.mrb_no_method_error() is never called
}

// FRaise declaration for fail method
func (mrb *MrbState) FRaise(v MrbValue) Value { return Value{C.mrb_f_raise(mrb.p, v.Value().v)} }

// Protect implemented in the mruby-error mrbgem
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
