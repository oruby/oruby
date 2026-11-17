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
	return C.mrb_object_dead_p(mrb.p, o.p) == true
}

// IsDead checks if value is garbage collected. Values that are not inherited from
// BasicObject, like integers, symbols, booleans, are not subject to GC and are always "alive"
func (mrb *MrbState) IsDead(o MrbValue) bool {
	v := o.Value()
	return v.HasBasic() && MrbObjectDeadP(mrb, v.RBasic())
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

// ObjspaceEachObjects wakls the object space items
func (mrb *MrbState) ObjspaceEachObjects(f MrbEachObjectCallbackT) {
	s := struct{}{}
	p := unsafe.Pointer(&s)

	mrb.setHook(p, f)

	C.mrb_objspace_each_objects(mrb.p, (*C.mrb_each_object_callback)(C.set_each_object_callback), p)
	runtime.KeepAlive(s)

	mrb.setHook(p, nil)
}

// GCIterating flag signals if GC is iterating
func (mrb *MrbState) GCIterating() bool { return C._gc_iterating(mrb.p) != false }

// GCFull flag is set when performing full GC
func (mrb *MrbState) GCFull() bool { return C._gc_full(mrb.p) != false }

// GCGenerational flag is set when GC is generational mode
func (mrb *MrbState) GCGenerational() bool { return C._gc_generational(mrb.p) != false }

// GCState returns current GC state
func (mrb *MrbState) GCState() int { return int(mrb.p.gc.state) }

// GCLiveObjectCount returns count of "live" objects
func (mrb *MrbState) GCLiveObjectCount() uint { return uint(mrb.p.gc.live) }

// GCDisabled is set when GC is disabled
func (mrb *MrbState) GCDisabled() bool { return C._gc_disabled(mrb.p) != false }

// GCEnable enables GC
func (mrb *MrbState) GCEnable() { C._gc_set_disabled(mrb.p, iifmb(false)) }

// GCDisable disables GC
func (mrb *MrbState) GCDisable() { C._gc_set_disabled(mrb.p, iifmb(true)) }

// GCArenaPeek peeks object in GCArena, at given index
func (mrb *MrbState) GCArenaPeek(index int) RBasic {
	return RBasic{C._gc_arena_peek(mrb.p, C.mrb_int(index))}
}

// GCStep advances the incremental collector by one unit of work and returns
// how much it did, 0 while GC is disabled (mrb_gc_step). With generational
// mode on, one step is a whole minor cycle; GCSchedulerDriven makes steps
// small.
func (mrb *MrbState) GCStep() int {
	return int(C.mrb_gc_step(mrb.p))
}

// GCSchedulerDriven hands GC scheduling to the host: allocation stops
// driving collection and generational mode is turned off, so the host calls
// GCStep when it has time, e.g. while Go waits on IO. false gives allocation
// back the driving (generational mode stays off). Fails while GC is disabled
// or an ObjectSpace walk runs (mrb_gc_scheduler_driven).
func (mrb *MrbState) GCSchedulerDriven(enable bool) error {
	var result C.mrb_value
	status := C._mrb_gc_scheduler_driven(mrb.p, iifmb(enable), &result)
	return mrb.protectedOutcomeError(Value{result}, status)
}

// GCSchedulerPending tells whether, host-driven, GC has work worth a GCStep
// now; always false when not host-driven (mrb_gc_scheduler_pending)
func (mrb *MrbState) GCSchedulerPending() bool {
	return C.mrb_gc_scheduler_pending(mrb.p) != false
}
