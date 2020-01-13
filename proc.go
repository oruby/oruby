package oruby

// #include "go-mrb.h"
import "C"
import (
	"reflect"
	"runtime"
	"unsafe"
)

// REnv struct
type REnv struct{ p *C.struct_REnv }

// Value implements MrbValue interface
func (e REnv) Value() Value { return Value{C.mrb_obj_value(unsafe.Pointer(e.p))} }

// Type for MrbValue interface
func (e REnv) Type() int { return e.Value().Type() }

// IsNil check for MrbValue interface
func (e REnv) IsNil() bool { return e.p == nil }

// EnvUnshare unshares Env
func (mrb *MrbState) EnvUnshare(env REnv) {
	C.mrb_env_unshare(mrb.p, env.p)
}

// AdjustStackLength of toplevel environment. Used in imrb
func (e REnv) AdjustStackLength(nlocals int) {
	if e.p != nil {
		if int(C._MRB_ENV_STACK_LEN(e.p)) < nlocals {
			C._MRB_ENV_SET_STACK_LEN(e.p, C.mrb_int(nlocals))
		}
	}
}

// RProc struct
type RProc struct{ p *C.struct_RProc }

// Value implements MrbValue interface
func (p RProc) Value() Value { return Value{C.mrb_obj_value(unsafe.Pointer(p.p))} }

// Type for MrbValue interface
func (p RProc) Type() int { return p.Value().Type() }

// IsNil check for MrbValue interface
func (p RProc) IsNil() bool { return p.p == nil }

// IsCFunc is cfunc
func (p RProc) IsCFunc() bool { return int(C._MRB_PROC_CFUNC_P(p.p)) != 0 }

// Strict is strict
func (p RProc) Strict() bool { return int(C._MRB_PROC_STRICT_P(p.p)) != 0 }

// Orphan is orphan
func (p RProc) Orphan() bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ORPHAN) != 0 }

// HasEnv has env
func (p RProc) HasEnv() bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ENVSET) != 0 }

// IsScope is scope
func (p RProc) IsScope() bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_SCOPE) != 0 }

// IRep from function
func (p RProc) IRep() MrbIrep {
	if p.IsCFunc() {
		return MrbIrep{nil}
	}
	return MrbIrep{C._rproc_body_irep(p.p)}
}

// SetTargetClass sets target class for proc
func (p RProc) SetTargetClass(c RClass) {
	C._MRB_PROC_SET_TARGET_CLASS(c.mrb.p, p.p, c.p)
}

// MrbAspecReq required
func MrbAspecReq(a uint32) uint32 { return (a >> 18) & 0x1f }

// MrbAspecOpt optional
func MrbAspecOpt(a uint32) uint32 { return (a >> 13) & 0x1f }

// MrbAspecRest rest
func MrbAspecRest(a uint32) uint32 { return a & (1 << 12) }

// MrbAspecPost post
func MrbAspecPost(a uint32) uint32 { return (a >> 7) & 0x1f }

// MrbAspecKey keyword
func MrbAspecKey(a uint32) uint32 { return (a >> 2) & 0x1f }

// MrbAspecKdict kdictionary
func MrbAspecKdict(a uint32) uint32 { return a & (1 << 1) }

// MrbAspecBlock block
func MrbAspecBlock(a uint32) uint32 { return a & 1 }

// Proc states
const (
	MrbProcCFunc  = 128
	MrbProcStrict = 256
	MrbProcOrphan = 512
	MrbProcEnvSet = 1024
	MrbProcScope  = 2048
)

// MrbProcCFuncP is cfunc
func MrbProcCFuncP(p RProc) bool { return int(C._MRB_PROC_CFUNC_P(p.p)) != 0 }

// MrbProcStrictP is strict
func MrbProcStrictP(p RProc) bool { return int(C._MRB_PROC_STRICT_P(p.p)) != 0 }

// MrbProcOrphanP is orphan
func MrbProcOrphanP(p RProc) bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ORPHAN) != 0 }

// MrbProcEnvP has env
func MrbProcEnvP(p RProc) bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ENVSET) != 0 }

// MrbProcScopeP is scope
func MrbProcScopeP(p RProc) bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_SCOPE) != 0 }

// MrbProcPtr returns RProc from oruby value
func MrbProcPtr(v MrbValue) RProc { return RProc{(*C.struct_RProc)(C._mrb_ptr(v.Value().v))} }

// MrbProcValue converts RProc to value
func MrbProcValue(p RProc) Value { return p.Value() }

// ProcNew creates new RProc from irep
func (mrb *MrbState) ProcNew(irep MrbIrep) RProc { return RProc{C.mrb_proc_new(mrb.p, irep.p)} }

// ProcPtr eturns RProc from oruby value
func (mrb *MrbState) ProcPtr(v MrbValue) RProc {
	return RProc{(*C.struct_RProc)(C._mrb_ptr(v.Value().v))}
}

// NLocals get locals count for irep proc
func (p RProc) NLocals() int {
	if MrbProcCFuncP(p) {
		return 0
	}
	return int(C._mrb_rproc_nlocals(p.p))
}

// ProcNewCFunc creaetes new RProc from go function
func (mrb *MrbState) ProcNewCFunc(f MrbFuncT) RProc {
	p := C.mrb_proc_new_cfunc(mrb.p, (*[0]byte)(C.set_mrb_callback))
	mrb.Lock()
	mrb.mrbProcs[RProc{p}] = f
	mrb.Unlock()
	return RProc{p}
}

// ProcNewGofunc creaetes new RProc from go function
// Go funcs are created with env which contains func reference
// This call is redirected to ProcNewGofuncWithEnv
func (mrb *MrbState) ProcNewGofunc(f interface{}) RProc {
	proc, _ := mrb.ProcNewGofuncWithEnv(f)
	return proc
}

// ClosureNew creates new closure from irep
func (mrb *MrbState) ClosureNew(irep MrbIrep) RProc { return RProc{C.mrb_closure_new(mrb.p, irep.p)} }

// ClosureNewCfunc creates new closure from Go function
func (mrb *MrbState) ClosureNewCfunc(f MrbFuncT, nlocals int32) RProc {
	p := C.mrb_closure_new_cfunc(mrb.p, (*[0]byte)(C.set_mrb_callback), C.int(nlocals))
	mrb.Lock()
	mrb.mrbProcs[RProc{p}] = f
	mrb.Unlock()
	return RProc{p}
}

// ProcCopy copies RProc value in oruby state
func (mrb *MrbState) ProcCopy(a, b RProc) {
	C.mrb_proc_copy(a.p, b.p)

	if C._MRB_PROC_CFUNC_P(a.p) != 0 {
		mrb.Lock()
		mrb.mrbProcs[RProc{b.p}] = mrb.mrbProcs[RProc{a.p}]
		mrb.Unlock()
	}
}

// ProcNewCFuncWithEnv creates function with attached env
func (mrb *MrbState) ProcNewCFuncWithEnv(f MrbFuncT, env ...MrbValue) RProc {
	argc := len(env)
	if argc == 0 {
		return mrb.ProcNewCFunc(f)
	}

	argv := make([]C.mrb_value, argc)
	for i := range env {
		argv[i] = mrb.Value(env[i]).Value().v
	}

	p := C.mrb_proc_new_cfunc_with_env(mrb.p, (*[0]byte)(C.set_mrb_callback), C.mrb_int(argc), &argv[0])
	runtime.KeepAlive(argv)

	mrb.Lock()
	mrb.mrbProcs[RProc{p}] = f
	mrb.Unlock()
	return RProc{p}
}

// ProcCFuncEnvGet retreive function env
func (mrb *MrbState) ProcCFuncEnvGet(index int) MrbValue {
	return Value{C.mrb_proc_cfunc_env_get(mrb.p, C.mrb_int(index))}
}

// ProcNewGofuncWithEnv new Go function with env
func (mrb *MrbState) ProcNewGofuncWithEnv(f interface{}, env ...interface{}) (RProc, MrbAspec) {
	v := reflect.ValueOf(f)

	if v.Kind() != reflect.Func {
		mrb.Raise(mrb.ETypeError(), "Function type argument is required")
		return RProc{nil}, ArgsNone()
	}

	argc := len(env) + 1
	args := make([]C.mrb_value, argc)

	// First env param is go function reference
	args[0] = mrb.registerFunc(f)

	for i := 1; i < argc; i++ {
		args[i] = mrb.Value(env[i-1]).Value().v
	}

	proc := C.mrb_proc_new_cfunc_with_env(mrb.p, (*[0]byte)(C.set_gofunc_callback), C.mrb_int(argc), &args[0])
	runtime.KeepAlive(args)
	return RProc{proc}, ArgsReq(uint32(v.Type().NumIn()))
}
