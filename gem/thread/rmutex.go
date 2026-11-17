package thread

import (
	"math"
	"sync"
	"time"

	"github.com/oruby/oruby"
)

// rmutex is Thread::Mutex as MRI 4.1 has it, for oruby's one Ruby thread:
// the lock belongs to the Fiber that took it. Locking a mutex that is
// already held can never succeed (no other thread could release it), so it
// raises ThreadError as MRI's deadlock check does instead of blocking. mu
// guards the state: Ruby runs on one goroutine at a time, but a Go host may
// hand the object to others.
type rmutex struct {
	mu     sync.Mutex
	locked bool
	owner  oruby.Value // the Fiber holding the lock; also kept in ivarOwner for the GC
}

func newMutex() *rmutex { return &rmutex{} }

// the hidden instance variable that keeps the owning Fiber alive
const ivarOwner = "__oruby_owner__"

func currentFiber(mrb *oruby.MrbState) oruby.Value {
	if !mrb.ClassDefined("Fiber") {
		return mrb.NilValue()
	}
	f, err := mrb.Funcall(mrb.ClassGet("Fiber"), mrb.Intern("current"))
	if err != nil {
		return mrb.NilValue()
	}
	return f
}

func mutexData(mrb *oruby.MrbState, self oruby.Value) *rmutex {
	rm, _ := mrb.Data(self).(*rmutex)
	return rm
}

func (rm *rmutex) setOwner(mrb *oruby.MrbState, self, owner oruby.Value, locked bool) {
	rm.locked, rm.owner = locked, owner
	mrb.IVSet(self, mrb.Intern(ivarOwner), owner)
}

func (rm *rmutex) lock(mrb *oruby.MrbState, self oruby.Value) error {
	cur := currentFiber(mrb)
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.locked {
		if rm.owner == cur {
			return oruby.EThreadError("deadlock; recursive locking")
		}
		return oruby.EThreadError("deadlock; lock already owned by another fiber belonging to the same thread")
	}
	rm.setOwner(mrb, self, cur, true)
	return nil
}

func (rm *rmutex) unlock(mrb *oruby.MrbState, self oruby.Value) error {
	cur := currentFiber(mrb)
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if !rm.locked {
		return oruby.EThreadError("Attempt to unlock a mutex which is not locked")
	}
	if rm.owner != cur {
		return oruby.EThreadError("Attempt to unlock a mutex which is locked by another thread/fiber")
	}
	rm.setOwner(mrb, self, mrb.NilValue(), false)
	return nil
}

func (rm *rmutex) isLocked() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.locked
}

func (rm *rmutex) ownedBy(cur oruby.Value) bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.locked && rm.owner == cur
}

// Mutex#lock -> self
func mutexLock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := mutexData(mrb, self).lock(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	return self
}

// Mutex#unlock -> self
func mutexUnlock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if err := mutexData(mrb, self).unlock(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	return self
}

// Mutex#try_lock -> true or false
func mutexTryLock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(mutexData(mrb, self).lock(mrb, self) == nil)
}

// Mutex#locked?
func mutexLocked(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(mutexData(mrb, self).isLocked())
}

// Mutex#owned?: locked by the current Fiber
func mutexOwned(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(mutexData(mrb, self).ownedBy(currentFiber(mrb)))
}

// Mutex#synchronize { } -> the block's value; the lock is released however
// the block ends, and a failing unlock (the block unlocked it) raises, as
// MRI's ensure does
func mutexSynchronize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rm := mutexData(mrb, self)
	proc := mrb.GetArgsBlock()
	if proc.IsNil() {
		return mrb.Raise(mrb.ClassGet("ThreadError"), "must be called with a block")
	}
	if err := rm.lock(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}

	ret := mrb.YieldArgv(proc)
	pending := mrb.Exc() != nil
	var blockErr error
	if pending {
		blockErr = mrb.Err()
	}
	if err := rm.unlock(mrb, self); err != nil {
		mrb.ExcClear()
		return mrb.RaiseError(err)
	}
	if pending {
		return mrb.RaiseError(blockErr)
	}
	return ret
}

// Mutex#sleep(timeout = nil) -> Integer or nil: releases the lock, sleeps,
// and takes the lock back. Returns the seconds slept, rounded, when woken (a
// signal handler ran), nil when the timeout ran out. Without a timeout
// nothing could wake the one Ruby thread, which is MRI's fatal deadlock.
func mutexSleep(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rm := mutexData(mrb, self)
	t := mrb.GetArgsFirst()
	forever := t.IsNil()
	var duration time.Duration
	if !forever {
		if !mrb.ObjIsKindOf(t, mrb.ClassGet("Numeric")) {
			return mrb.Raise(mrb.ETypeError(), "can't convert "+mrb.ClassName(mrb.ClassOf(t))+" into time interval")
		}
		f, err := mrb.EnsureFloatType(t)
		if err != nil {
			return mrb.RaiseError(err)
		}
		secs := f.Float64()
		if secs < 0 {
			return mrb.Raise(mrb.EArgumentError(), "time interval must not be negative")
		}
		duration = time.Duration(secs * float64(time.Second))
	}

	if err := rm.unlock(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	// take the lock back however the sleep ends; nothing else could have
	// taken it meanwhile but this Fiber's own signal handlers
	cur := currentFiber(mrb)
	relock := func() {
		rm.mu.Lock()
		rm.setOwner(mrb, self, cur, true)
		rm.mu.Unlock()
	}

	if forever {
		relock()
		return mrb.Raise(fatalClass(mrb), "No live threads left. Deadlock?")
	}

	start := time.Now()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	woken := false
wait:
	for {
		select {
		case <-timer.C:
			break wait
		case <-mrb.ExitChan():
			break wait
		case <-mrb.SignalWake():
			if err := mrb.CheckSignals(); err != nil {
				relock()
				return mrb.RaiseError(err)
			}
			woken = true
			break wait
		}
	}
	relock()
	if !woken {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(int(math.Round(time.Since(start).Seconds())))
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
