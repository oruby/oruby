package oruby

// #include "go-mrb.h"
import "C"
import (
	"runtime"
	"unsafe"
)

// GC_STATE enums
const (
	GcStateRoot = iota
	GcStateMark
	GcStateSweep
)

// MrbFreeContext free context
func MrbFreeContext(mrb *MrbState, c *MrbContext) {
	C.mrb_free_context(mrb.p, c.p)
}

// MrbObjectDeadP checks if object is dead
func MrbObjectDeadP(mrb *MrbState, o RBasic) bool {
	return C.mrb_object_dead_p(mrb.p, o.p) != 0
}

// IsDead checks if value is garbage collected. Values that are not inherited from
// BasicObject, like fixnums, symbols, booleans, are not subject to GC and are always "alive"
func (mrb *MrbState) IsDead(o MrbValue) bool {
	return o.Value().HasBasic() && MrbObjectDeadP(mrb, RBASIC(o))
}

// MrbEachObj constants
const (
	MrbEachObjOK = iota
	MrbEachObjBreak
)

// MrbEachObjectCallbackT callback for each obj
type MrbEachObjectCallbackT func(mrb *MrbState, obj RBasic) int

//export go_each_object_callback
func go_each_object_callback(cmrb *C.mrb_state, obj *C.struct_RBasic, data unsafe.Pointer) C.int {
	mrb := getMrbState(cmrb)

	f, ok := mrb.getHook(data).(MrbEachObjectCallbackT)
	if !ok {
		return MrbEachObjBreak
	}

	return C.int(f(mrb, RBasic{obj}))
}

// HashForEach wakls the hash item pairs
func (mrb *MrbState) ObjspaceEachObjects(f MrbEachObjectCallbackT) {
	s := struct{}{}
	p := unsafe.Pointer(&s)

	mrb.setHook(p, f)

	C.mrb_objspace_each_objects(mrb.p, (*C.mrb_each_object_callback)(C.set_each_object_callback), p)
	runtime.KeepAlive(s)

	mrb.setHook(p, nil)
}

// Helper GC functions
func (mrb *MrbState) GCIterating() bool       { return C._gc_iterating(mrb.p) != 0 }
func (mrb *MrbState) GCFull() bool            { return C._gc_full(mrb.p) != 0 }
func (mrb *MrbState) GCGenerational() bool    { return C._gc_generational(mrb.p) != 0 }
func (mrb *MrbState) GCOutOfMemory() bool     { return C._gc_out_of_memory(mrb.p) != 0 }
func (mrb *MrbState) GCState() int            { return int(mrb.p.gc.state) }
func (mrb *MrbState) GCLiveObjectCount() uint { return uint(mrb.p.gc.live) }
func (mrb *MrbState) GCDisabled() bool        { return C._gc_disabled(mrb.p) != 0 }
func (mrb *MrbState) GCEnable()               { C._gc_set_disabled(mrb.p, iifmb(false)) }
func (mrb *MrbState) GCDisable()              { C._gc_set_disabled(mrb.p, iifmb(true)) }
func (mrb *MrbState) GCArenaPeek(index int) RBasic {
	return RBasic{C._gc_arena_peek(mrb.p, C.mrb_int(index))}
}
