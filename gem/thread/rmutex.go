package thread

import (
	"math"
	"time"

	"github.com/oruby/oruby"
)

// rmutex is Thread::Mutex as MRI 4.1 has it: the lock belongs to the Fiber
// that took it. A thread locking a mutex another thread holds waits (without
// the GVL) until it is unlocked; one held by another fiber of the same
// thread, or by itself, is MRI's ThreadError. Its state changes only under
// the GVL, so it needs no lock of its own.
type rmutex struct {
	locked  bool
	owner   oruby.Value   // the Fiber holding the lock; also kept in ivarOwner for the GC
	thread  *oruby.Thread // the thread of that Fiber (nil before the first Thread.new)
	waiters []*oruby.Waiter
	self    oruby.Value // the Mutex object, while it is held by a thread
}

// heldSet is the locked mutexes of mrb's threads (stored as the "thread"
// gem's per-state data on mrb), for releaseHeld when a thread finishes
func heldSet(mrb *oruby.MrbState) map[*rmutex]struct{} {
	h, _ := mrb.GemData("thread").(map[*rmutex]struct{})
	return h
}

// releaseHeld unlocks what the finished thread t holds
func releaseHeld(mrb *oruby.MrbState, t *oruby.Thread) {
	for rm := range heldSet(mrb) {
		if rm.locked && rm.thread == t {
			rm.release(mrb, rm.self)
		}
	}
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
		mrb.ExcClear()
		return mrb.NilValue()
	}
	return f
}

func mutexData(mrb *oruby.MrbState, self oruby.Value) *rmutex {
	rm, _ := mrb.Data(self).(*rmutex)
	if rm == nil {
		rm = newMutex()
		mrb.DataSetInterface(self, rm)
	}
	return rm
}

func (rm *rmutex) take(mrb *oruby.MrbState, self, owner oruby.Value) {
	rm.locked, rm.owner, rm.thread = true, owner, mrb.CurrentThread()
	mrb.IVSet(self, mrb.Intern(ivarOwner), owner)
	if rm.thread != nil {
		if h := heldSet(mrb); h != nil {
			rm.self = self
			h[rm] = struct{}{}
		}
	}
}

// tryLock takes the lock when it is free; err for MRI's deadlocks
func (rm *rmutex) tryLock(mrb *oruby.MrbState, self, cur oruby.Value) (bool, error) {
	if !rm.locked {
		rm.take(mrb, self, cur)
		return true, nil
	}
	if rm.owner.Pointer() == cur.Pointer() {
		return false, oruby.EThreadError("deadlock; recursive locking")
	}
	if rm.thread == mrb.CurrentThread() {
		return false, oruby.EThreadError("deadlock; lock already owned by another fiber belonging to the same thread")
	}
	return false, nil
}

func (rm *rmutex) lock(mrb *oruby.MrbState, self oruby.Value) error {
	cur := currentFiber(mrb)
	for {
		ok, err := rm.tryLock(mrb, self, cur)
		if ok || err != nil {
			return err
		}
		w := mrb.NewWaiter()
		rm.waiters = append(rm.waiters, w)
		_, err = mrb.ThreadWait(w.C(), -1, true)
		rm.dropWaiter(w)
		if err != nil {
			return err
		}
	}
}

func (rm *rmutex) dropWaiter(ch *oruby.Waiter) {
	for i, w := range rm.waiters {
		if w == ch {
			rm.waiters = append(rm.waiters[:i], rm.waiters[i+1:]...)
			return
		}
	}
}

// release frees the lock and wakes the first waiter
func (rm *rmutex) release(mrb *oruby.MrbState, self oruby.Value) {
	rm.locked, rm.owner, rm.thread = false, oruby.Nil, nil
	mrb.IVSet(self, mrb.Intern(ivarOwner), oruby.Nil)
	if h := heldSet(mrb); h != nil {
		delete(h, rm)
	}
	rm.self = oruby.Nil
	if len(rm.waiters) > 0 {
		w := rm.waiters[0]
		rm.waiters = rm.waiters[1:]
		w.Wake()
	}
}

// relock reacquires the mutex at the end of Mutex#sleep before any pending
// interrupt (kill/raise) unwinds the thread, so ensure clauses see the
// mutex locked by cur, as in MRI
func (rm *rmutex) relock(mrb *oruby.MrbState, self, cur oruby.Value) error {
	var pending error
	var pendingExc *oruby.RException
	for {
		ok, err := rm.tryLock(mrb, self, cur)
		if ok || err != nil {
			if err != nil {
				return err
			}
			if pendingExc != nil {
				mrb.RaiseObj(pendingExc.Value())
			}
			return pending
		}
		w := mrb.NewWaiter()
		rm.waiters = append(rm.waiters, w)
		_, err = mrb.ThreadWait(w.C(), -1, true)
		rm.dropWaiter(w)
		if err != nil {
			if pending == nil {
				pending = err
				pendingExc = mrb.Exc()
			}
			mrb.ExcClear()
		}
	}
}

func (rm *rmutex) unlock(mrb *oruby.MrbState, self oruby.Value) error {
	if !rm.locked {
		return oruby.EThreadError("Attempt to unlock a mutex which is not locked")
	}
	if rm.owner.Pointer() != currentFiber(mrb).Pointer() {
		return oruby.EThreadError("Attempt to unlock a mutex which is locked by another thread/fiber")
	}
	rm.release(mrb, self)
	return nil
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
	rm := mutexData(mrb, self)
	ok, _ := rm.tryLock(mrb, self, currentFiber(mrb))
	return oruby.Bool(ok)
}

// Mutex#locked?
func mutexLocked(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(mutexData(mrb, self).locked)
}

// Mutex#owned?: locked by the current Fiber
func mutexOwned(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rm := mutexData(mrb, self)
	return oruby.Bool(rm.locked && rm.owner.Pointer() == currentFiber(mrb).Pointer())
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
	ret, blockErr := mrb.YieldArgvE(proc)
	if err := rm.unlock(mrb, self); err != nil {
		if blockErr == nil {
			return mrb.RaiseError(err)
		}
	}
	if blockErr != nil {
		return mrb.RaiseError(blockErr)
	}
	return ret
}

// Mutex#sleep(timeout = nil) -> Integer or nil: releases the lock, sleeps
// until woken (Thread#wakeup, ConditionVariable#signal) or the timeout, and
// takes the lock back. Returns the seconds slept, rounded, when woken, nil
// when the timeout ran out.
func mutexSleep(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rm := mutexData(mrb, self)
	d := time.Duration(-1)
	if t := mrb.GetArgsFirst(); !t.IsNil() {
		var err error
		if d, err = seconds(mrb, t); err != nil {
			return mrb.RaiseError(err)
		}
	}
	cur := currentFiber(mrb)
	if err := rm.unlock(mrb, self); err != nil {
		return mrb.RaiseError(err)
	}
	start := time.Now()
	r, werr := mrb.ThreadWait(nil, d, d < 0)
	var savedExc *oruby.RException
	if werr != nil {
		savedExc = mrb.Exc()
		mrb.ExcClear()
	} else if r == oruby.WaitWoken {
		mrb.TakeWakeup()
	}
	// take the lock back however the sleep ended
	if err := rm.relock(mrb, self, cur); err != nil && werr == nil {
		werr = err
	}
	if werr != nil {
		if savedExc != nil {
			mrb.RaiseObj(savedExc.Value())
		}
		return mrb.RaiseError(werr)
	}
	if r == oruby.WaitTimeout {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(int(math.Round(time.Since(start).Seconds())))
}
