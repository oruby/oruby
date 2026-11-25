package oruby

// #include "go-mrb.h"
// #include "go-thread.h"
import "C"

// Fibers over goroutines.
//
// mruby's Fiber switches contexts inside one VM loop, so a Fiber.yield cannot
// cross a C function: the Go frames of compiled code are such functions. A
// compiled program therefore runs each Fiber on a goroutine of its own, as
// each Thread is (thread.go), with the same per thread state (go-thread.c:
// the contexts, the catch on that goroutine's C stack, the exception, the GC
// arena). A switch is a baton handed over a channel: the goroutine that
// resumes saves its part of the state, wakes the fiber's goroutine and waits;
// the fiber loads its own and runs until it yields or ends, and the resumer
// loads its own again. Only one of them runs at a time, so the GVL is
// untouched: the thread that holds it runs whichever of its fibers is current.
//
// It replaces the primitives of mruby-fiber that gem/core_ext/ruby/29_fiber.rb
// builds on (initialize, resume, yield under their __oruby_ names); raise,
// kill, storage, the scheduler hooks and the status words stay as they are.
// Fiber#transfer is not supported.

// fiberUnit is a part of the state that runs on a goroutine of its own: a
// Fiber, or the thread context that resumes one.
type fiberUnit struct {
	st   *C.orb_tstate
	keep Value // rooted: what the unit's arena protects while it is switched out
	obj  Value // the Fiber object that owns the context
	body Value
	root bool // the thread's own context, resuming a fiber

	// the Go side scratch of the state, while the unit is switched out
	fastArgs       []Value
	hasFastArgs    bool
	aotDepth       int
	aotIntDisabled int

	resumeCh chan []Value // to the fiber: the arguments of a resume
	backCh   chan fiberBack
	resumer  *fiberUnit
	started  bool
	dead     bool
}

// fiberBack is what a fiber hands its resumer: the values it yielded or its
// block returned, or the exception that ended it
type fiberBack struct {
	vals []Value
	exc  Value
	done bool
}

func (mrb *MrbState) fiberOut(u *fiberUnit) {
	u.fastArgs, u.hasFastArgs = mrb.fastArgs, mrb.hasFastArgs
	if mrb.aot != nil {
		u.aotDepth = mrb.aot.depth
		u.aotIntDisabled = mrb.aot.intDisabled
	}
	C._orb_tstate_save(mrb.p, u.st, u.keep.v)
}

func (mrb *MrbState) fiberIn(u *fiberUnit) {
	C._orb_tstate_load(mrb.p, u.st, u.keep.v)
	mrb.fastArgs, mrb.hasFastArgs = u.fastArgs, u.hasFastArgs
	if mrb.aot != nil {
		mrb.aot.depth = u.aotDepth
		mrb.aot.intDisabled = u.aotIntDisabled
	}
	if u.root {
		mrb.curFiber = nil
	} else {
		mrb.curFiber = u
	}
	C._orb_ctx_status(mrb.p, C.MRB_FIBER_RUNNING)
	C._orb_vm_arm(mrb.p)
}

// fiberRoot is the unit of the running thread's own context
func (mrb *MrbState) fiberRoot() *fiberUnit {
	slot := &mrb.mainRoot
	if s := mrb.sched; s != nil && s.cur != nil {
		slot = &s.cur.rootUnit
	}
	if *slot == nil {
		keep := mrb.AryNew().Value()
		mrb.GCRegister(keep)
		*slot = &fiberUnit{st: C._orb_tstate_alloc(), keep: keep, root: true}
	}
	return *slot
}

func fiberValue(mrb *MrbState, vals []Value) Value {
	switch len(vals) {
	case 0:
		return nilValue
	case 1:
		return vals[0]
	}
	return mrb.AryNewFromValues(vals...).Value()
}

// EnableGoFibers makes the Fibers of the state run on goroutines. A compiled
// program does it when it starts.
func (mrb *MrbState) EnableGoFibers() {
	if mrb.goFibers || !mrb.ClassDefinedID(Sym_Fiber) {
		return
	}
	mrb.goFibers = true
	mrb.fibers = map[uintptr]*fiberUnit{}
	fc := mrb.ClassGetID(Sym_Fiber)
	fc.DefineMethod("__oruby_initialize", fiberInit, mrb.ArgsAny())
	fc.DefineMethod("__oruby_resume", fiberResume, mrb.ArgsAny())
	fc.DefineClassMethod("__oruby_yield", fiberYield, mrb.ArgsAny())
	fc.DefineMethod("__oruby_transfer_raw", fiberTransfer, mrb.ArgsAny())
	// the thread's own context is running (its root fiber answers so)
	C._orb_ctx_status(mrb.p, C.MRB_FIBER_RUNNING)
}

// fiberTransfer: transferring between fibers of a compiled program is not
// supported; mruby's own transfer would switch contexts under the goroutines
func fiberTransfer(mrb *MrbState, self Value) MrbValue {
	return mrb.fiberError("Fiber#transfer is not supported in a compiled program: resume and Fiber.yield are")
}

func (mrb *MrbState) fiberError(msg string) Value {
	return mrb.Raise(mrb.ExcGet("FiberError"), msg)
}

func fiberInit(mrb *MrbState, self Value) MrbValue {
	_, blkProc := mrb.GetArgsWithBlock()
	blk := blkProc.Value()
	if blk.IsNil() {
		return mrb.Raise(mrb.EArgumentError(), "tried to create Proc object without a block")
	}
	if exc := mrb.fiberStorageInit(self); !exc.IsNil() {
		return exc
	}
	u := &fiberUnit{obj: self, body: blk}
	u.st = C._orb_fiber_init(mrb.p, self.v, blk.v)
	u.keep = mrb.AryNew().Value()
	// what the GC must keep while the fiber lives: its arena keeper, the body
	mrb.IVSet(self, mrb.Intern("__oruby_keep"), u.keep)
	mrb.IVSet(self, mrb.Intern("__oruby_body"), blk)
	u.resumeCh = make(chan []Value)
	u.backCh = make(chan fiberBack)
	mrb.fibers[self.Pointer()] = u
	return self
}

func fiberResume(mrb *MrbState, self Value) MrbValue {
	u := mrb.fibers[self.Pointer()]
	if u == nil {
		if self.Type() == MrbTTFiber {
			return mrb.fiberError("attempt to resume a terminated fiber")
		}
		return mrb.fiberError("not a fiber")
	}
	if u.dead {
		return mrb.fiberError("attempt to resume a terminated fiber")
	}
	if u == mrb.curFiber {
		return mrb.fiberError("attempt to resume the current fiber")
	}
	args := append([]Value(nil), mrb.GetArgs().Slice()...)

	ru := mrb.curFiber
	if ru == nil {
		ru = mrb.fiberRoot()
	}
	u.resumer = ru
	C._orb_ctx_status(mrb.p, C.MRB_FIBER_RESUMED)
	mrb.fiberOut(ru)
	if !u.started {
		u.started = true
		go mrb.fiberMain(u, args)
	} else {
		u.resumeCh <- args
	}
	back := <-u.backCh
	mrb.fiberIn(ru)

	if back.done {
		// the fiber's goroutine is over, and the state it ran in is not the
		// state's now: its context and arena can go
		C._orb_tstate_free(mrb.p, u.st)
		u.st = nil
		delete(mrb.fibers, self.Pointer())
	}
	if !back.exc.IsNil() {
		return mrb.RaiseObj(back.exc)
	}
	return fiberValue(mrb, back.vals)
}

func fiberYield(mrb *MrbState, self Value) MrbValue {
	u := mrb.curFiber
	if u == nil {
		return mrb.fiberError("attempt to yield on a not resumed fiber")
	}
	vals := append([]Value(nil), mrb.GetArgs().Slice()...)
	C._orb_ctx_status(mrb.p, C.MRB_FIBER_SUSPENDED)
	mrb.fiberOut(u)
	u.backCh <- fiberBack{vals: vals}
	args := <-u.resumeCh
	mrb.fiberIn(u)
	return fiberValue(mrb, args)
}

// fiberMain is the goroutine of fiber u: it runs the block, and hands the
// result (or the exception) to the resumer when it ends
func (mrb *MrbState) fiberMain(u *fiberUnit, args []Value) {
	mrb.fiberIn(u)
	var res C.mrb_value
	status := C._orb_fiber_run(mrb.p, u.body.v, C.mrb_int(len(args)), valuesPtr(args), &res)
	back := fiberBack{done: true}
	r := Value{res}
	if status == C.GO_MRB_OUTCOME_SUCCESS {
		back.vals = []Value{r}
	} else {
		back.exc = r
	}
	// the arena that protected the result goes with the fiber's state: keep it
	// until the resumer has it
	mrb.AryPush(u.keep, r)
	C._orb_tstate_finish(mrb.p, u.st)
	u.dead = true
	u.backCh <- back
}

// fiberStorageInit gives the new fiber its storage as mruby-fiber's initialize
// does: storage: true (the default) a copy of the creator's, nil none, a Hash
// of Symbol keys a copy of it. It returns the raised exception, if any.
func (mrb *MrbState) fiberStorageInit(self Value) Value {
	var opts Value = nilValue
	if args := mrb.GetArgsKw().Slice(); len(args) > 0 && args[len(args)-1].IsHash() {
		opts = args[len(args)-1]
	}
	storage := True.Value()
	if !opts.IsNil() {
		key := mrb.SymbolValue(mrb.Intern("storage"))
		if mrb.HashKeyP(opts, key) {
			storage = mrb.HashGet(opts, key)
		}
	}
	storageSym := mrb.Intern("__storage__")
	switch {
	case storage.IsNil():
		return nilValue
	case storage.Type() == MrbTTTrue:
		cur, err := mrb.Funcall(mrb.ClassGetID(Sym_Fiber).Value(), mrb.Intern("current"))
		if err != nil {
			mrb.ExcClear()
			return nilValue
		}
		if st := mrb.IVGet(cur, storageSym); st.IsHash() {
			mrb.IVSet(self, storageSym, mrb.HashDup(st))
		}
	case storage.IsHash():
		keys := mrb.HashKeys(storage).Value()
		for i, n := 0, mrb.AotAryLen(keys); i < n; i++ {
			if mrb.AotAryRef(keys, i).Type() != MrbTTSymbol {
				return mrb.Raise(mrb.ETypeError(), "storage key must be a Symbol")
			}
		}
		mrb.IVSet(self, storageSym, mrb.HashDup(storage))
	default:
		return mrb.Raise(mrb.ETypeError(), "storage must be a hash")
	}
	return nilValue
}
