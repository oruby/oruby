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
	waiters []*waiter
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

// tryLock takes the lock when it is free; err for MRI's deadlocks. sched:
// the current fiber waits through its scheduler, which a fiber of the
// thread holding it can then unblock (no deadlock)
func (rm *rmutex) tryLock(mrb *oruby.MrbState, self, cur, sched oruby.Value) (bool, error) {
	if !rm.locked {
		rm.take(mrb, self, cur)
		return true, nil
	}
	if rm.owner.Pointer() == cur.Pointer() {
		return false, oruby.EThreadError("deadlock; recursive locking")
	}
	if sched.IsNil() && rm.thread == mrb.CurrentThread() {
		return false, oruby.EThreadError("deadlock; lock already owned by another fiber belonging to the same thread")
	}
	return false, nil
}

// lockWait waits until the lock is free and takes it. uninterruptible: an
// interrupt (kill, raise) that comes meanwhile is returned once the lock is
// taken, so ensure clauses see it held (Mutex#sleep's relock)
func (rm *rmutex) lockWait(mrb *oruby.MrbState, self, cur oruby.Value, uninterruptible bool) error {
	var pending error
	var pendingExc *oruby.RException
	dropFiber(&rm.waiters, cur) // its waiter from the last attempt
	for {
		sched := mrb.FiberScheduler()
		ok, err := rm.tryLock(mrb, self, cur, sched)
		if ok || err != nil {
			if err != nil {
				return err
			}
			if pendingExc != nil {
				mrb.RaiseObj(pendingExc.Value())
			}
			return pending
		}
		e := newWaiter(mrb, sched)
		rm.waiters = append(rm.waiters, e)
		keepFibers(mrb, self, rm.waiters)
		if e.w == nil {
			return errBlock // waits in the scheduler; registered until it asks again
		}
		err = e.wait(mrb, self, oruby.Nil, -1, true)
		dropWaiter(&rm.waiters, e)
		keepFibers(mrb, self, rm.waiters)
		if err != nil {
			if !uninterruptible {
				return err
			}
			if pending == nil {
				pending = err
				pendingExc = mrb.Exc()
			}
			mrb.ExcClear()
		}
	}
}

func (rm *rmutex) lock(mrb *oruby.MrbState, self oruby.Value) error {
	return rm.lockWait(mrb, self, currentFiber(mrb), false)
}

// release frees the lock and wakes the first waiter
func (rm *rmutex) release(mrb *oruby.MrbState, self oruby.Value) {
	rm.locked, rm.owner, rm.thread = false, oruby.Nil, nil
	mrb.IVSet(self, mrb.Intern(ivarOwner), oruby.Nil)
	if h := heldSet(mrb); h != nil {
		delete(h, rm)
	}
	rm.self = oruby.Nil
	wakeFirstOf(mrb, &rm.waiters, self)
}

// relock reacquires the mutex at the end of Mutex#sleep before any pending
// interrupt (kill/raise) unwinds the thread, so ensure clauses see the
// mutex locked by cur, as in MRI
func (rm *rmutex) relock(mrb *oruby.MrbState, self, cur oruby.Value) error {
	return rm.lockWait(mrb, self, cur, true)
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
		if err == errBlock {
			return mrb.Intern("__oruby_block").Value()
		}
		return mrb.RaiseError(err)
	}
	return self
}

// Thread.__oruby_release_held: the Mutexes the current thread holds are
// released as it ends (from its Ruby side, so their unblocks run there)
func threadReleaseHeld(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if t := mrb.CurrentThread(); t != nil {
		releaseHeld(mrb, t)
	}
	return oruby.Nil
}

// Thread.__oruby_unblocks: the scheduler unblocks queued meanwhile
func threadUnblocks(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.TakeUnblocks()
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
	ok, _ := rm.tryLock(mrb, self, currentFiber(mrb), oruby.Nil)
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
