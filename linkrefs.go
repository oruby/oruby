package oruby

// #include "go-mrb.h"
import "C"
import (
	"sync"
	"unsafe"
)
import "errors"

var mu sync.Mutex
var states []*MrbState

func init() {
	states = make([]*MrbState, 1, 10)
}

// GetMrbState returns Go MrbState from C.mrb_state reference
func getMrbState(cmrb *C.struct_mrb_state) *MrbState {
	idx := int(C._mrb_get_idx(cmrb))
	if idx >= len(states) {
		panic("State index is out of range")
	}

	if states[idx] == nil {
		panic(errors.New("State does not exists"))
	}

	return states[idx]
}

// GetMrbState returns Go MrbState from C.mrb_state reference
func GetMrbState(cmrb uintptr) *MrbState {
	idx := int(C._cmrb_get_idx(C.uintptr_t(cmrb)))
	if idx >= len(states) {
		panic("State index is out of range")
	}

	if states[idx] == nil {
		panic(errors.New("State does not exists"))
	}

	return states[idx]
}

func registerState(mrb *MrbState) {
	mu.Lock()
	defer mu.Unlock()

	if len(states) > 500 {
		for idx, state := range states {
			if (state == nil) && (idx > 0) {
				states[idx] = state
				C._mrb_set_idx(mrb.p, C.mrb_int(idx))
				return
			}
		}
	}

	states = append(states, mrb)
	idx := len(states) - 1
	C._mrb_set_idx(mrb.p, C.mrb_int(idx))
	return
}

func removeStateIndex(index int) {
	mu.Lock()
	defer mu.Unlock()
	states[index] = nil
}

func (mrb *MrbState) registerFunc(f interface{}) C.mrb_value {
	mrb.funcs = append(mrb.funcs, f)
	return C.mrb_fixnum_value((C.mrb_int)(len(mrb.funcs) - 1))
}

func (mrb *MrbState) registerFuncIndex(f interface{}) int {
	mrb.funcs = append(mrb.funcs, f)
	return len(mrb.funcs) - 1
}

func (mrb *MrbState) getFunc(index uint) (interface{}, error) {
	if index >= uint(len(mrb.funcs)) {
		return nil, errors.New("Function index is out of range")
	}

	return mrb.funcs[index], nil
}

func (mrb *MrbState) getMrbFuncT(index uint) MrbFuncT {
	if index >= uint(len(mrb.funcs)) {
		return nil
	}
	f, ok := mrb.funcs[index].(MrbFuncT)
	if !ok {
		return nil
	}

	return f
}

func (mrb *MrbState) setHook(p unsafe.Pointer, v interface{}) {
	mrb.Lock()
	defer mrb.Unlock()

	if v == nil {
		delete(mrb.hooks, p)
	} else {
		mrb.hooks[p] = v
	}
}

func (mrb *MrbState) getHook(p unsafe.Pointer) interface{} {
	mrb.Lock()
	defer mrb.Unlock()

	return mrb.hooks[p]
}
