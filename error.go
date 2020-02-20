package oruby

// #include "go-mrb.h"
import "C"

// RException struct
type RException struct {
	p   *C.struct_RException
	mrb *MrbState
}

// RExceptionPtr struct wrapper around mruby RException struct pointer
type RExceptionPtr struct{ p *C.struct_RException }

// RBreak struct
type RBreak struct{ p *C.struct_RBreak }

// MrbExcPtr returns RException
func MrbExcPtr(v MrbValue) RExceptionPtr {
	return RExceptionPtr{(*C.struct_RException)(C._mrb_ptr(v.Value().v))}
}

// SysFail return SystemCallError with message
func (mrb *MrbState) SysFail(mesg string) Value {
	no := int(C.errno)

	if mrb.ClassDefined("SystemCallError") {
		sce := mrb.ClassGet("SystemCallError")
		var result Value

		if mesg != "" {
			result, _= mrb.FuncallWithBlock(sce, mrb.Intern("_sys_fail"), no, mesg)
		} else {
			result, _= mrb.FuncallWithBlock(sce, mrb.Intern("_sys_fail"), no)
		}
		return result
	} else {
		return mrb.Raise(mrb.ERuntimeError(), mesg)
	}

	// cmesg := C.CString(mesg)
	// defer C.free(unsafe.Pointer(cmesg))
	// C.mrb_sys_fail(mrb.p, cmesg)
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

// ExcBacktrace get exception backtrace as value
func (mrb *MrbState) ExcBacktrace(exc MrbValue) Value {
	return Value{C.mrb_exc_backtrace(mrb.p, exc.Value().v)}
}

// GetBacktrace from current state exception
func (mrb *MrbState) GetBacktrace() Value { return Value{C.mrb_get_backtrace(mrb.p)} }

//func (mrb *MrbState) NoMethodError(id MrbSym, fmt string,  ) {
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
