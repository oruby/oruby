package thread

import (
	"time"

	"github.com/oruby/oruby"
)

// Ruby threads over the state's scheduler (oruby's thread.go): the Go side
// of Thread, Mutex, ConditionVariable, Queue and SizedQueue, and Kernel#sleep.
// The Ruby side (core_ext 03_thread.rb, 24_queue.rb) keeps what is Ruby:
// argument checks, thread-local storage, exception reporting, the
// interrupt queue.
func init() {
	oruby.Gem("thread", func(mrb *oruby.MrbState) interface{} {
		threadClass := mrb.DefineClass("Thread", mrb.ObjectClass())
		// the Ruby side of Thread is core_ext's (03_thread.rb), which
		// replaces these
		for _, m := range []string{"new", "start", "fork"} {
			threadClass.DefineClassMethod(m, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
				return mrb.Raise(mrb.ENotImplementedError(), "Thread needs the core_ext gem")
			}, mrb.ArgsAny()|mrb.ArgsBlock())
		}
		threadClass.DefineClassMethod("current", threadCurrent, mrb.ArgsNone())
		threadClass.DefineClassMethod("main", threadMain, mrb.ArgsNone())
		threadClass.DefineClassMethod("list", threadList, mrb.ArgsNone())
		threadClass.DefineClassMethod("pass", threadPass, mrb.ArgsNone())
		threadClass.DefineClassMethod("__oruby_started", threadStarted, mrb.ArgsNone())
		threadClass.DefineClassMethod("__oruby_stop", threadStop, mrb.ArgsNone())
		threadClass.DefineClassMethod("__oruby_interrupt_taken", threadInterruptTaken, mrb.ArgsReq(1))
		threadClass.DefineClassMethod("__oruby_ignore_deadlock", threadIgnoreDeadlock, mrb.ArgsReq(1))
		threadClass.DefineClassMethod("__oruby_deadlock_error", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			v, err := mrb.ObjNew(fatalClass(mrb), mrb.StrNew("No live threads left. Deadlock?"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			return v
		}, mrb.ArgsNone())
		threadClass.DefineClassMethod("__oruby_deadlock", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return mrb.Raise(fatalClass(mrb), "No live threads left. Deadlock?")
		}, mrb.ArgsNone())
		threadClass.DefineMethod("__oruby_start", threadStart, mrb.ArgsReq(1))
		threadClass.DefineMethod("__oruby_state", threadState, mrb.ArgsNone())
		threadClass.DefineMethod("__oruby_notify", threadNotify, mrb.ArgsNone())
		threadClass.DefineMethod("__oruby_wakeup", threadWakeup, mrb.ArgsNone())
		threadClass.DefineMethod("__oruby_join", threadJoin, mrb.ArgsReq(1))
		threadClass.DefineMethod("__oruby_fiber", threadFiber, mrb.ArgsNone())
		threadClass.DefineMethod("__oruby_root_fiber", threadRootFiber, mrb.ArgsNone())
		threadClass.DefineMethod("native_thread_id", threadNativeID, mrb.ArgsNone())

		mutexClass := mrb.DefineGoClass("Mutex", newMutex)
		mrb.ConstSet(threadClass, oruby.Sym_Mutex, mutexClass.Value())
		mutexClass.DefineMethod("lock", mutexLock, mrb.ArgsNone())
		mutexClass.DefineMethod("unlock", mutexUnlock, mrb.ArgsNone())
		mutexClass.DefineMethod("try_lock", mutexTryLock, mrb.ArgsNone())
		mutexClass.DefineMethod("locked?", mutexLocked, mrb.ArgsNone())
		mutexClass.DefineMethod("owned?", mutexOwned, mrb.ArgsNone())
		mutexClass.DefineMethod("sleep", mutexSleep, mrb.ArgsOpt(1))
		mutexClass.DefineMethod("synchronize", mutexSynchronize, mrb.ArgsNone()|mrb.ArgsBlock())

		initQueues(mrb, threadClass)
		// a thread that finishes releases the Mutexes it holds, as in MRI
		mrb.OnThreadFinish(func(t *oruby.Thread) { releaseHeld(mrb, t) })

		mrb.DefineClass("ThreadError", mrb.EStandardErrorClass())
		mrb.DefineClass("ClosedQueueError", mrb.ClassGet("StopIteration"))

		mrb.DefineGlobalFunction("sleep", oruby.KernelSleep, mrb.ArgsAny())
		mrb.DefineModuleFunction(mrb.KernelModule(), "sleep", oruby.KernelSleep, mrb.ArgsAny())

		return map[*rmutex]struct{}{}
	})
}

// mainObject is Thread.main's object, made at the first ask
func mainObject(mrb *oruby.MrbState) oruby.Value {
	if t := mrb.MainThread(); t != nil {
		return t.Object()
	}
	klass := mrb.ClassGet("Thread")
	key := oruby.IvSym___main_thread
	current := mrb.IVGet(klass, key)
	if current.IsNil() {
		current = mrb.ObjAlloc(oruby.MrbTTObject, klass).Value()
		mrb.IVSet(klass, key, current)
	}
	return current
}

func threadMain(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mainObject(mrb)
}

func threadCurrent(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.CurrentThread(); t != nil {
		return t.Object()
	}
	return mainObject(mrb)
}

func threadList(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	ts := mrb.Threads()
	if ts == nil {
		return mrb.AryNewFromValues(mainObject(mrb))
	}
	ary := mrb.AryNew()
	for _, t := range ts {
		mrb.AryPush(ary, t.Object())
	}
	return ary
}

func threadPass(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	mrb.ThreadPass()
	return oruby.Nil
}

// Thread#__oruby_start(body): run body (the wrapper Thread#initialize made)
func threadStart(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	body := mrb.GetArgsFirst()
	mrb.ThreadStart(mainObject(mrb), self, body)
	return self
}

// Thread.__oruby_started: the body runs; interrupts may be delivered
func threadStarted(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.CurrentThread(); t != nil {
		t.Started()
	}
	return oruby.Nil
}

// Thread#__oruby_state: 0 running, 1 sleeping, 2 dead, nil not started
func threadState(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.ThreadOf(self); t != nil {
		return mrb.FixnumValue(t.Status())
	}
	if self.Pointer() == mainObject(mrb).Pointer() {
		return mrb.FixnumValue(oruby.ThreadRun)
	}
	if mrb.IVGet(self, mrb.Intern("__oruby_dead")).Bool() {
		return mrb.FixnumValue(oruby.ThreadDead)
	}
	return oruby.Nil
}

// Thread#__oruby_notify: an interrupt was queued for self
func threadNotify(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.ThreadOf(self); t != nil {
		mrb.ThreadInterrupt(t)
	}
	return oruby.Nil
}

// Thread.__oruby_interrupt_taken(more)
func threadInterruptTaken(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.CurrentThread(); t != nil {
		t.InterruptTaken(mrb.GetArgsFirst().Bool())
	}
	return oruby.Nil
}

func threadIgnoreDeadlock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	mrb.ThreadIgnoreDeadlock(v.Bool())
	return v
}

func threadWakeup(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.ThreadOf(self); t != nil {
		mrb.ThreadWakeup(t)
	}
	return self
}

// Thread.__oruby_stop: Thread.stop, until Thread#wakeup or #run
func threadStop(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	for {
		r, err := mrb.ThreadWait(nil, -1, true)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if mrb.TakeWakeup() || r != oruby.WaitWoken {
			return oruby.Nil
		}
	}
}

// Thread#__oruby_join(limit): true once self finished, false when limit
// (seconds, nil for none) ran out
func threadJoin(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.ThreadOf(self)
	if t == nil {
		return oruby.True
	}
	d := time.Duration(-1)
	if lim := mrb.GetArgsFirst(); !lim.IsNil() {
		secs := lim.Float64()
		if lim.IsInteger() {
			secs = float64(lim.Int())
		}
		if secs <= 0 {
			if t.Status() == oruby.ThreadDead {
				return oruby.True
			}
			return oruby.False
		}
		d = time.Duration(secs * float64(time.Second))
	}
	deadline := time.Now().Add(d)
	for t.Status() != oruby.ThreadDead {
		left := time.Duration(-1)
		if d >= 0 {
			left = time.Until(deadline)
			if left <= 0 {
				return oruby.False
			}
		}
		r, err := mrb.ThreadWait(t.Done(), left, d < 0)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if r == oruby.WaitTimeout && d >= 0 {
			return oruby.Bool(t.Status() == oruby.ThreadDead)
		}
	}
	return oruby.True
}

// Thread#__oruby_fiber: the Fiber self runs now (nil: the current thread),
// its root Fiber once it finished
func threadFiber(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.ThreadOf(self); t != nil {
		return t.Fiber()
	}
	return mrb.ThreadRootFiber(self)
}

// Thread#__oruby_root_fiber
func threadRootFiber(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.ThreadRootFiber(self)
}

// Thread#native_thread_id: an id while the thread lives
func threadNativeID(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if self.Pointer() == mainObject(mrb).Pointer() {
		return mrb.FixnumValue(1)
	}
	if t := mrb.ThreadOf(self); t != nil {
		return mrb.FixnumValue(int(self.Pointer() >> 3 & 0x7fffffff))
	}
	return oruby.Nil
}

// seconds converts a time interval argument as MRI does
func seconds(mrb *oruby.MrbState, t oruby.Value) (time.Duration, error) {
	return mrb.SleepDuration(t)
}

// fatalClass is MRI's fatal: an Exception subclass with no constant, which
// a deadlock raises
func fatalClass(mrb *oruby.MrbState) oruby.RClass {
	key := mrb.Intern("__oruby_fatal__")
	if v := mrb.IVGet(mrb.KernelModule(), key); v.Type() == oruby.MrbTTClass {
		return mrb.ClassPtr(v)
	}
	c, err := mrb.ClassNew(mrb.EExceptionClass())
	if err != nil {
		return mrb.EExceptionClass()
	}
	name := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue { return mrb.StrNew("fatal") }
	for _, m := range []string{"name", "to_s", "inspect"} {
		c.DefineClassMethod(m, name, mrb.ArgsNone())
	}
	mrb.IVSet(mrb.KernelModule(), key, c.Value())
	return c
}
