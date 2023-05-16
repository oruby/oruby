package oruby

// #include "go-mrb.h"
import "C"
import (
	"reflect"
	"runtime"
	"unsafe"
)

// REnv struct
type REnv struct {
	p   *C.struct_REnv
	mrb *MrbState
}

// Value implements MrbValue interface
func (e REnv) Value() Value { return mrbObjValue(unsafe.Pointer(e.p)) }

// Type for MrbValue interface
func (e REnv) Type() Type { return e.Value().Type() }

// IsNil check for MrbValue interface
func (e REnv) IsNil() bool { return e.p == nil }

// Len returns number of items
func (e REnv) Len() int { return int(C._MRB_ENV_LEN(e.p)) }

// Unshare stack unshares Env
func (e REnv) Unshare() { C.mrb_env_unshare(e.mrb.p, e.p, true) }

// Stack stack unshares Env
func (e REnv) Stack(index int) Value {
	l := e.Len()
	if index < 0 || index >= l {
		panic("invalid index")
	}
	stack := (*[1 << 28]C.mrb_value)(unsafe.Pointer(e.p.stack))[:l:l]
	return Value{stack[index]}
}

// EnvUnshare unshares Env
func (mrb *MrbState) EnvUnshare(env REnv, noraise bool) {
	C.mrb_env_unshare(mrb.p, env.p, C.mrb_bool(noraise))
}

// SetEnv creates and sets new Env object with stack Values
func (p RProc) SetEnv(stackItems ...Value) {
	if len(stackItems) == 0 {
		C._mrb_create_env(p.mrb.p, p.p, 0, nil)
		return
	}

	C._mrb_create_env(p.mrb.p, p.p, C.mrb_int(len(stackItems)), &(stackItems[0].v))
}

// AdjustStackLength of toplevel environment. Used in imrb
func (e REnv) AdjustStackLength(nlocals int) {
	if e.p != nil {
		if int(C._MRB_ENV_LEN(e.p)) < nlocals {
			C._MRB_ENV_SET_LEN(e.p, C.mrb_int(nlocals))
		}
	}
}

// RProc struct
type RProc struct {
	p   *C.struct_RProc
	mrb *MrbState
}

// RProcPtr wraps pointer to mruby RProc C struct
type RProcPtr struct{ p *C.struct_RProc }

// Value implements MrbValue interface
func (p RProc) Value() Value { return mrbObjValue(unsafe.Pointer(p.p)) }

// Type for MrbValue interface
func (p RProc) Type() Type { return p.Value().Type() }

// IsNil check for MrbValue interface
func (p RProc) IsNil() bool { return p.p == nil }

// IsCFunc returns true if proc is native C or Go function
func (p RProc) IsCFunc() bool { return int(C._MRB_PROC_CFUNC_P(p.p)) != 0 }

// Strict is strict
func (p RProc) Strict() bool { return int(C._MRB_PROC_STRICT_P(p.p)) != 0 }

// Orphan is orphan function
func (p RProc) Orphan() bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ORPHAN) != 0 }

// HasEnv has environment
func (p RProc) HasEnv() bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ENVSET) != 0 }

// IsScope is scope
func (p RProc) IsScope() bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_SCOPE) != 0 }

// IsNoArg is no arg flag set
func (p RProc) IsNoArg() bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_NOARG) != 0 }

// Flags returns rproc flags
func (p RProc) Flags() int { return int(C._mrb_rproc_flags(p.p)) }

// FlagSet set proc flag
func (p RProc) FlagSet(flag int) {
	flags := C._mrb_rproc_flags(p.p) & C.uint32_t(flag)
	C._mrb_rproc_set_flags(p.p, C.uint32_t(flags))
}

// FlagUnset unset proc flag
func (p RProc) FlagUnset(flag int) {
	flags := C._mrb_rproc_flags(p.p) & ^C.uint32_t(flag)
	C._mrb_rproc_set_flags(p.p, C.uint32_t(flags))
}

// Upper returns upper proc
func (p RProc) Upper() RProc { return RProc{p.p.upper, p.mrb} }

// SetUpper returns upper proc
func (p RProc) SetUpper(upper RProc) { p.p.upper = upper.p }

// IRep from function
func (p RProc) IRep() MrbIrep {
	if p.IsCFunc() {
		return MrbIrep{nil, p.mrb}
	}
	return MrbIrep{C._rproc_body_irep(p.p), p.mrb}
}

// Env returns REnv proc environment
func (p RProc) Env() REnv {
	if !p.HasEnv() {
		return REnv{nil, p.mrb}
	}
	return REnv{C._MRB_PROC_ENV(p.p), p.mrb}
}

// EnvGet returns REnv Value at index i
func (p RProc) EnvGet(i int) Value {
	if !p.HasEnv() {
		return nilValue
	}
	return p.mrb.ProcCFuncEnvGet(i)
}

// Data returns Go function from C proc
func (p RProc) Data() interface{} {
	if !p.IsCFunc() {
		return nil
	}

	if !p.HasEnv() {
		return p.mrb.mrbProcs[unsafe.Pointer(p.p)]
	}

	i := p.mrb.ProcCFuncEnvGet(0)
	if !i.IsInteger() {
		return p.mrb.mrbProcs[unsafe.Pointer(p.p)]
	}
	return p.mrb.funcs[i.Int()]
}

// SetTargetClass sets target class for proc
func (p RProc) SetTargetClass(c RClass) {
	C._MRB_PROC_SET_TARGET_CLASS(c.mrb.p, p.p, c.p)
}

// TargetClass sets target class for proc
func (p RProc) TargetClass() RClass {
	return RClass{C._MRB_PROC_TARGET_CLASS(p.p), p.mrb}
}

// Load procedure
func (p RProc) Load() Value {
	return Value{C.mrb_load_proc(p.mrb.p, p.p)}
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
	MrbProcNoArg  = 4096
)

// MrbProcCFuncP is cfunc flag set
func MrbProcCFuncP(p RProc) bool { return int(C._MRB_PROC_CFUNC_P(p.p)) != 0 }

// MrbProcStrictP is strict flag set
func MrbProcStrictP(p RProc) bool { return int(C._MRB_PROC_STRICT_P(p.p)) != 0 }

// MrbProcOrphanP is orphan flag set
func MrbProcOrphanP(p RProc) bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ORPHAN) != 0 }

// MrbProcEnvP has env
func MrbProcEnvP(p RProc) bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_ENVSET) != 0 }

// MrbProcScopeP is scope flag set
func MrbProcScopeP(p RProc) bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_SCOPE) != 0 }

// MrbProcNoargP is scope
func MrbProcNoargP(p RProc) bool { return (C._mrb_rproc_flags(p.p) & C.MRB_PROC_NOARG) != 0 }

// MrbProcPtr returns RProc from oruby value
func MrbProcPtr(v MrbValue) RProcPtr { return RProcPtr{(*C.struct_RProc)(C._mrb_ptr(v.Value().v))} }

// MrbProcValue converts RProc to value
func MrbProcValue(p RProc) Value { return p.Value() }

// ProcNew creates new RProc from irep
func (mrb *MrbState) ProcNew(irep MrbIrep) RProc { return RProc{C.mrb_proc_new(mrb.p, irep.p), mrb} }

// RProc returns RProc struct from proc ruby value, or nil if vale is not proc
func (mrb *MrbState) RProc(v MrbValue) RProc {
	value := v.Value()

	if value.IsNil() {
		return RProc{nil, mrb}
	}

	if !value.IsProc() {
		panic("value is not RProc")
	}
	return RProc{(*C.struct_RProc)(C._mrb_ptr(value.v)), mrb}
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
	p := C.mrb_proc_new_cfunc(mrb.p, (*[0]byte)(C.set_mrb_proc_callback))
	mrb.Lock()
	mrb.mrbProcs[unsafe.Pointer(p)] = f
	mrb.Unlock()
	return RProc{p, mrb}
}

// ProcNewGofunc creaetes new RProc from go function
// Go funcs are created with env which contains func reference
// This call is redirected to ProcNewGofuncWithEnv
func (mrb *MrbState) ProcNewGofunc(f interface{}) RProc {
	proc, _ := mrb.ProcNewGofuncWithEnv(f)
	return proc
}

// ClosureNewCfunc creates new closure from Go function
func (mrb *MrbState) ClosureNewCfunc(f MrbFuncT, nlocals int32) RProc {
	p := C.mrb_closure_new_cfunc(mrb.p, (*[0]byte)(C.set_mrb_proc_callback), C.int(nlocals))
	mrb.Lock()
	mrb.mrbProcs[unsafe.Pointer(p)] = f
	mrb.Unlock()
	return RProc{p, mrb}
}

// ProcNewCFuncWithEnv creates function with attached env
func (mrb *MrbState) ProcNewCFuncWithEnv(f MrbFuncT, env ...MrbValue) RProc {
	argc := len(env)
	if argc == 0 {
		return mrb.ProcNewCFunc(f)
	}

	argv := make([]C.mrb_value, argc)
	for i := range env {
		argv[i] = mrb.Value(env[i]).v
	}

	p := C.mrb_proc_new_cfunc_with_env(mrb.p, (*[0]byte)(C.set_mrb_proc_callback), C.mrb_int(argc), &argv[0])
	runtime.KeepAlive(argv)

	mrb.Lock()
	mrb.mrbProcs[unsafe.Pointer(p)] = f
	mrb.Unlock()
	return RProc{p, mrb}
}

// ProcCFuncEnvGet retreive function env
func (mrb *MrbState) ProcCFuncEnvGet(index int) Value {
	return Value{C.mrb_proc_cfunc_env_get(mrb.p, C.mrb_int(index))}
}

// ProcNewGofuncWithEnv new Go function with env
func (mrb *MrbState) ProcNewGofuncWithEnv(f interface{}, env ...interface{}) (RProc, MrbAspec) {
	v := reflect.ValueOf(f)

	if v.Kind() != reflect.Func {
		panic("Function type argument is required")
	}

	argc := len(env) + 1
	args := make([]C.mrb_value, argc)

	// First env param is go function reference
	args[0] = mrb.registerFunc(f)

	for i := 1; i < argc; i++ {
		args[i] = mrb.Value(env[i-1]).v
	}

	proc := C.mrb_proc_new_cfunc_with_env(mrb.p, (*[0]byte)(C.set_gofunc_callback), C.mrb_int(argc), &args[0])

	runtime.KeepAlive(args)
	return RProc{proc, mrb}, ArgsReq(uint32(v.Type().NumIn()))
}

// LoadProc loads and executes proc
func (mrb *MrbState) LoadProc(proc RProc) Value {
	return Value{C.mrb_load_proc(mrb.p, proc.p)}
}

// ProcSet sets proc on call info
func (ci MrbCallInfo) ProcSet(p RProc) {
	C.mrb_vm_ci_proc_set(ci.p, p.p)
}

// TargetClass returnc target class pointer from call info
func (ci MrbCallInfo) TargetClass() RClassPtr {
	return RClassPtr{C.mrb_vm_ci_target_class(ci.p)}
}

// TargetClassSet sets target class in call info
func (ci MrbCallInfo) TargetClassSet(tc RClass) {
	C.mrb_vm_ci_target_class_set(ci.p, tc.p)
}

// Env returns env from call info
func (ci MrbCallInfo) Env() REnv {
	return REnv{C.mrb_vm_ci_env(ci.p), nil}
}

// EnvSet sets env on call info
func (ci MrbCallInfo) EnvSet(e *REnv) {
	if e == nil {
		C.mrb_vm_ci_env_set(ci.p, nil)
		return
	}
	C.mrb_vm_ci_env_set(ci.p, e.p)
}
