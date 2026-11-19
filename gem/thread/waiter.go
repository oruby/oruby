package thread

import (
	"time"

	"github.com/oruby/oruby"
)

// waiter is one waiting for a Mutex, a Queue or a SizedQueue: a thread
// (w, woken through its channel) or a non-blocking fiber waiting through
// its Fiber scheduler (sched.block(blocker, timeout), woken with
// sched.unblock(blocker, fiber), as MRI's sync_waiter)
type waiter struct {
	w            *oruby.Waiter
	fiber, sched oruby.Value
}

// newWaiter: the current fiber's (through sched), or the thread's
func newWaiter(mrb *oruby.MrbState, sched oruby.Value) *waiter {
	if !sched.IsNil() {
		return &waiter{fiber: mrb.CurrentFiber(), sched: sched}
	}
	return &waiter{w: mrb.NewWaiter()}
}

// wake lets the waiter go on; blocker is what it waits for
func (e *waiter) wake(mrb *oruby.MrbState, blocker oruby.Value) {
	if e.w != nil {
		e.w.Wake()
		return
	}
	mrb.DeferUnblock(e.sched, blocker, e.fiber)
}

// errBlock: the current fiber has to wait through its scheduler; the Go
// method answers :__oruby_block and the Ruby side calls sched.block and
// asks again (a fiber cannot switch across the Go method's frame)
var errBlock = oruby.ERuntimeError("block through the Fiber scheduler")

// dropFiber forgets a waiter of fiber left from its last attempt
func dropFiber(ws *[]*waiter, fiber oruby.Value) {
	for i := 0; i < len(*ws); i++ {
		if e := (*ws)[i]; e.w == nil && e.fiber.Pointer() == fiber.Pointer() {
			*ws = append((*ws)[:i], (*ws)[i+1:]...)
			i--
		}
	}
}

// wait blocks until woken (d < 0: no limit), deadlockable as ThreadWait.
// A fiber waits in sched.block(blocker, timeout), timeout nil or seconds.
func (e *waiter) wait(mrb *oruby.MrbState, blocker, timeout oruby.Value, d time.Duration, deadlockable bool) error {
	if e.w == nil {
		return errBlock
	}
	_, err := mrb.ThreadWait(e.w.C(), d, deadlockable)
	return err
}

func wakeFirstOf(mrb *oruby.MrbState, ws *[]*waiter, blocker oruby.Value) {
	if len(*ws) > 0 {
		e := (*ws)[0]
		*ws = (*ws)[1:]
		e.wake(mrb, blocker)
	}
}

func wakeAllOf(mrb *oruby.MrbState, ws *[]*waiter, blocker oruby.Value) {
	list := *ws
	*ws = nil
	for _, e := range list {
		e.wake(mrb, blocker)
	}
}

func dropWaiter(ws *[]*waiter, e *waiter) {
	for i, x := range *ws {
		if x == e {
			*ws = append((*ws)[:i], (*ws)[i+1:]...)
			return
		}
	}
}

// keepFibers roots, on obj, the fibers and schedulers of its fiber waiters
func keepFibers(mrb *oruby.MrbState, obj oruby.Value, lists ...[]*waiter) {
	ary := mrb.AryNew()
	for _, l := range lists {
		for _, e := range l {
			if e.w == nil {
				mrb.AryPush(ary, e.fiber)
				mrb.AryPush(ary, e.sched)
			}
		}
	}
	mrb.IVSet(obj, mrb.Intern("__oruby_fwaiters"), ary.Value())
}
