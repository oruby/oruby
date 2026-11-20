package oruby

// #include "go-mrb.h"
import "C"
import (
	"runtime"
	"sync"
	"unsafe"
)
import "errors"

var mu sync.RWMutex
var states []*MrbState

func init() {
	states = make([]*MrbState, 1, 10)
}

// GetMrbState returns Go MrbState from C.mrb_state reference
func getMrbState(cmrb *C.struct_mrb_state) *MrbState {
	return getMrbStateIndex(int(C._mrb_get_idx(cmrb)))
}

// GetMrbState returns Go MrbState from C.mrb_state reference
func getMrbStateIndex(idx int) *MrbState {
	mu.RLock()
	defer mu.RUnlock()
	if idx < 0 || idx >= len(states) {
		panic("State index is out of range")
	}

	if states[idx] == nil {
		panic(errors.New("State does not exists"))
	}

	return states[idx]
}

func lookupMrbStateIndex(idx int) *MrbState {
	mu.RLock()
	defer mu.RUnlock()
	if idx <= 0 || idx >= len(states) {
		return nil
	}
	return states[idx]
}

// GetMrbState returns Go MrbState from C.mrb_state reference
func GetMrbState(cmrb uintptr) *MrbState {
	return getMrbStateIndex(int(C._cmrb_get_idx(C.uintptr_t(cmrb))))
}

func registerState(mrb *MrbState) {
	mu.Lock()
	idx := 1
	for ; idx < len(states); idx++ {
		if states[idx] == nil {
			states[idx] = mrb
			break
		}
	}
	if idx == len(states) {
		states = append(states, mrb)
	}
	mu.Unlock()
	C._mrb_set_idx(mrb.p, C.mrb_int(idx))
}

func removeStateIndex(index int) {
	mu.Lock()
	defer mu.Unlock()
	states[index] = nil
}

func (mrb *MrbState) registerFunc(f interface{}) C.mrb_value {
	idx := mrb.registerFuncIndex(f)
	return C.mrb_fixnum_value((C.mrb_int)(idx))
}

// funcvalOf is the identity of a func value: its funcval. A top-level
// function has one funcval wherever it is named, a closure one per
// creation (so closures over different values stay apart).
func funcvalOf(f MrbFuncT) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&f))
}

// registerFuncIndex: a function registered again gets its first index, so
// the methods defined with it are one method definition (Method#== of
// aliases: Dir.getwd and Dir.pwd)
func (mrb *MrbState) registerFuncIndex(f interface{}) int {
	if cf, ok := f.(MrbFuncT); ok {
		return mrb.registerMrbFuncT(cf)
	}
	mrb.funcs = append(mrb.funcs, f)
	mrb.cfuncs = append(mrb.cfuncs, nil)
	return len(mrb.funcs) - 1
}

func (mrb *MrbState) registerMrbFuncT(f MrbFuncT) int {
	key := funcvalOf(f)
	if i, ok := mrb.funcIndex[key]; ok {
		return i
	}
	mrb.funcs = append(mrb.funcs, f)
	mrb.cfuncs = append(mrb.cfuncs, f)
	if mrb.funcIndex == nil {
		mrb.funcIndex = map[unsafe.Pointer]int{}
	}
	mrb.funcIndex[key] = len(mrb.cfuncs) - 1
	return len(mrb.cfuncs) - 1
}

func (mrb *MrbState) getFunc(index uint) (interface{}, error) {
	if index >= uint(len(mrb.funcs)) {
		return nil, errors.New("Function index is out of range")
	}

	return mrb.funcs[index], nil
}

func (mrb *MrbState) getMrbFuncT(index uint) MrbFuncT {
	if index >= uint(len(mrb.cfuncs)) {
		return nil
	}
	return mrb.cfuncs[index]
}

func (mrb *MrbState) setHook(p unsafe.Pointer, v interface{}) {
	mrb.Lock()
	defer mrb.Unlock()

	if v == nil {
		delete(mrb.hooks, p)
	} else {
		mrb.hooks[p] = v
		if _, ok := v.(Freer); ok {
			runtime.SetFinalizer(&v, func(v interface{}) {
				v.(Freer).Free()
			})
		}
	}
}

func (mrb *MrbState) getHook(p unsafe.Pointer) interface{} {
	mrb.Lock()
	defer mrb.Unlock()

	return mrb.hooks[p]
}
