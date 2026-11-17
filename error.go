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
	return e.mrb.IVGet(e, Sym_status).Int()
}

func (e RException) Backtrace() RValue {
	return RValue{C.mrb_exc_backtrace(e.mrb.p, e.v), e.mrb}
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

// SysFail return SystemCallError with message
func (mrb *MrbState) SysFail(err error) Value {
	return mrb.RaiseError(err)
}

// ExcNewStr create new exception
func (mrb *MrbState) ExcNewStr(c RClass, str MrbValue) Value {
	return Value{C.mrb_exc_new_str(mrb.p, c.p, str.Value().v)}
}

// ExcMessage returns the raw message value stored in an exception, or nil.
func (mrb *MrbState) ExcMessage(exc MrbValue) Value {
	if exc.Type() != MrbTTException {
		return Nil
	}
	return Value{C.mrb_exc_mesg_get(mrb.p, (*C.struct_RException)(C._mrb_ptr(exc.Value().v)))}
}

// ExcBacktrace returns the raw backtrace value stored in an exception, or nil.
func (mrb *MrbState) ExcBacktrace(exc MrbValue) Value {
	if exc.Type() != MrbTTException {
		return Nil
	}
	return Value{C.mrb_exc_backtrace(mrb.p, exc.Value().v)}
}

// SetExcMessage restores a Marshal exception message without calling Ruby methods.
func (mrb *MrbState) SetExcMessage(exc, message MrbValue) error {
	if exc.Type() != MrbTTException {
		return ETypeError("Exception value expected")
	}
	if !message.IsNil() && message.Type() != MrbTTString {
		return ETypeError("exception message must be a String or nil in this runtime")
	}
	C._mrb_exc_set_marshaled_message(mrb.p, exc.Value().v, message.Value().v)
	return nil
}

// SetExcBacktrace restores a Marshal exception backtrace without calling Ruby methods.
func (mrb *MrbState) SetExcBacktrace(exc, backtrace MrbValue) error {
	if exc.Type() != MrbTTException {
		return ETypeError("Exception value expected")
	}
	if !backtrace.IsNil() {
		if backtrace.Type() != MrbTTArray {
			return ETypeError("exception backtrace must be an Array or nil")
		}
		for _, frame := range RArraySlice(backtrace.Value()) {
			if frame.Type() != MrbTTString {
				return ETypeError("exception backtrace entries must be Strings")
			}
		}
	}
	C._mrb_exc_set_marshaled_backtrace(mrb.p, exc.Value().v, backtrace.Value().v)
	return nil
}

func (mrb *MrbState) NoMethodError(methodId MrbSym, args MrbValue, fmt string, fmtArgs ...interface{}) Value {
	ret := mrb.ENoMethodError().Raisef(fmt, fmtArgs...)
	mrb.SetIV(ret, "name", mrb.SymbolValue(methodId))
	mrb.SetIV(ret, "args", args)
	return ret

	// C.mrb_no_method_error() is never called
}

// FRaise invokes Kernel#raise behind a C-local protection boundary.
func (mrb *MrbState) FRaise(v MrbValue) Value {
	var result C.mrb_value
	status := C._mrb_f_raise_protected(mrb.p, v.Value().v, &result)
	value := Value{result}
	if status != C.GO_MRB_OUTCOME_SUCCESS {
		mrb.RaiseObj(value)
	}
	return value
}

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
