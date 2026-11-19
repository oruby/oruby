package thread

import (
	"time"

	"github.com/oruby/oruby"
)

// Thread::Queue, Thread::SizedQueue and Thread::ConditionVariable. The items
// live in a Ruby Array (an instance variable, for the GC); the waits are
// ThreadWait, without the GVL. All state changes happen under the GVL.
// core_ext's 24_queue.rb checks the arguments and calls __oruby_push /
// __oruby_pop.
type rqueue struct {
	closed      bool
	max         int // SizedQueue's limit; 0: none
	popWaiters  []*waiter
	pushWaiters []*waiter
}

type condvar struct {
	waiters []*oruby.Thread
}

func newQueue() *rqueue    { return &rqueue{} }
func newCondvar() *condvar { return &condvar{} }

const ivarItems = "__oruby_items"

func initQueues(mrb *oruby.MrbState, threadClass oruby.RClass) {
	queueClass := mrb.DefineGoClass("Queue", newQueue)
	mrb.ConstSet(threadClass, mrb.Intern("Queue"), queueClass.Value())
	queueClass.DefineMethod("__oruby_push", queuePush, mrb.ArgsArg(3, 1))
	queueClass.DefineMethod("__oruby_pop", queuePop, mrb.ArgsArg(2, 1))
	queueClass.DefineMethod("__oruby_max", queueMax, mrb.ArgsReq(1))
	queueClass.DefineMethod("__oruby_close", queueClose, mrb.ArgsNone())
	queueClass.DefineMethod("closed?", queueClosed, mrb.ArgsNone())
	queueClass.DefineMethod("num_waiting", queueNumWaiting, mrb.ArgsNone())
	queueClass.DefineMethod("__oruby_items", queueItems, mrb.ArgsNone())
	queueClass.DefineMethod("__oruby_space", queueSpace, mrb.ArgsNone())

	sized := mrb.DefineClass("SizedQueue", queueClass)
	mrb.ConstSet(threadClass, mrb.Intern("SizedQueue"), sized.Value())

	cvClass := mrb.DefineGoClassUnder(threadClass, "ConditionVariable", newCondvar)
	mrb.ConstSet(mrb.ObjectClass(), mrb.Intern("ConditionVariable"), cvClass.Value())
	cvClass.DefineMethod("wait", condWait, mrb.ArgsArg(1, 1))
	cvClass.DefineMethod("signal", condSignal, mrb.ArgsNone())
	cvClass.DefineMethod("broadcast", condBroadcast, mrb.ArgsNone())
	cvClass.DefineMethod("marshal_dump", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.Raisef(mrb.ETypeError(), "can't dump %s", mrb.ClassName(mrb.ClassOf(self)))
	}, mrb.ArgsAny())
}

func queueData(mrb *oruby.MrbState, self oruby.Value) *rqueue {
	q, _ := mrb.Data(self).(*rqueue)
	if q == nil {
		q = newQueue()
		mrb.DataSetInterface(self, q)
	}
	return q
}

// items is the queue's Array
func items(mrb *oruby.MrbState, self oruby.Value) oruby.Value {
	key := mrb.Intern(ivarItems)
	a := mrb.IVGet(self, key)
	if a.IsNil() {
		a = mrb.AryNew().Value()
		mrb.IVSet(self, key, a)
	}
	return a
}

// timeoutArg: nil none (-1), else seconds
func timeoutArg(v oruby.Value) time.Duration {
	if v.IsNil() {
		return -1
	}
	secs := v.Float64()
	if v.IsInteger() {
		secs = float64(v.Int())
	}
	if secs < 0 {
		secs = 0
	}
	return time.Duration(secs * float64(time.Second))
}

// Queue#__oruby_pop(non_block, timeout): the first item; nil when closed
// and empty or when the timeout ran out; ThreadError for an empty queue
// with non_block
func queuePop(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	nonBlock, d := args.Item(0).Bool(), timeoutArg(args.Item(1))
	again := args.Len() > 2 && args.Item(2).Bool() // after the scheduler's block
	q := queueData(mrb, self)
	ary := items(mrb, self)
	deadline := time.Now().Add(d)
	if again {
		dropFiber(&q.popWaiters, mrb.CurrentFiber())
	}
	for {
		if oruby.RArrayLen(ary) > 0 {
			v := mrb.AryShift(ary)
			wakeFirstOf(mrb, &q.pushWaiters, self)
			return v
		}
		if nonBlock {
			return mrb.Raise(mrb.ClassGet("ThreadError"), "queue empty")
		}
		if q.closed {
			return oruby.Nil
		}
		left := time.Duration(-1)
		if d >= 0 {
			if left = time.Until(deadline); left <= 0 {
				return oruby.Nil
			}
		}
		// a non-blocking fiber waits through its scheduler: the Ruby side
		// calls block(queue, timeout) and asks again
		sched := mrb.FiberScheduler()
		if !sched.IsNil() && again && d >= 0 {
			return oruby.Nil // the scheduler's timed block ran out
		}
		e := newWaiter(mrb, sched)
		q.popWaiters = append(q.popWaiters, e)
		keepFibers(mrb, self, q.popWaiters, q.pushWaiters)
		if e.w == nil {
			return mrb.Intern("__oruby_block").Value()
		}
		err := e.wait(mrb, self, oruby.Nil, left, d < 0)
		dropWaiter(&q.popWaiters, e)
		keepFibers(mrb, self, q.popWaiters, q.pushWaiters)
		if err != nil {
			return mrb.RaiseError(err)
		}
	}
}

// Queue#__oruby_push(obj, non_block, timeout): self; nil when the timeout
// of a full SizedQueue ran out
func queuePush(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	obj, nonBlock, d := args.Item(0), args.Item(1).Bool(), timeoutArg(args.Item(2))
	again := args.Len() > 3 && args.Item(3).Bool() // after the scheduler's block
	q := queueData(mrb, self)
	ary := items(mrb, self)
	deadline := time.Now().Add(d)
	if again {
		dropFiber(&q.pushWaiters, mrb.CurrentFiber())
	}
	for {
		if q.closed {
			return mrb.Raise(mrb.ClassGet("ClosedQueueError"), "queue closed")
		}
		if q.max == 0 || oruby.RArrayLen(ary) < q.max {
			mrb.AryPush(ary, obj)
			wakeFirstOf(mrb, &q.popWaiters, self)
			return self
		}
		if nonBlock {
			return mrb.Raise(mrb.ClassGet("ThreadError"), "queue full")
		}
		left := time.Duration(-1)
		if d >= 0 {
			if left = time.Until(deadline); left <= 0 {
				return oruby.Nil
			}
		}
		// a non-blocking fiber waits through its scheduler: the Ruby side
		// calls block(queue, timeout) and asks again
		sched := mrb.FiberScheduler()
		if !sched.IsNil() && again && d >= 0 {
			return oruby.Nil // the scheduler's timed block ran out
		}
		e := newWaiter(mrb, sched)
		q.pushWaiters = append(q.pushWaiters, e)
		keepFibers(mrb, self, q.popWaiters, q.pushWaiters)
		if e.w == nil {
			return mrb.Intern("__oruby_block").Value()
		}
		err := e.wait(mrb, self, oruby.Nil, left, d < 0)
		dropWaiter(&q.pushWaiters, e)
		keepFibers(mrb, self, q.popWaiters, q.pushWaiters)
		if err != nil {
			return mrb.RaiseError(err)
		}
	}
}

// Queue#__oruby_max(n): SizedQueue's limit; a larger one lets pushers in
func queueMax(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	q := queueData(mrb, self)
	n := mrb.GetArgsFirst().Int()
	grow := n > q.max
	q.max = n
	if grow {
		wakeAllOf(mrb, &q.pushWaiters, self)
	}
	return mrb.FixnumValue(n)
}

// Queue#__oruby_space: a pusher waits no more (after #clear, #pop)
func queueSpace(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	wakeAllOf(mrb, &queueData(mrb, self).pushWaiters, self)
	return oruby.Nil
}

func queueClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	q := queueData(mrb, self)
	q.closed = true
	wakeAllOf(mrb, &q.popWaiters, self)
	wakeAllOf(mrb, &q.pushWaiters, self)
	return self
}

func queueClosed(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(queueData(mrb, self).closed)
}

func queueNumWaiting(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	q := queueData(mrb, self)
	return mrb.FixnumValue(len(q.popWaiters) + len(q.pushWaiters))
}

func queueItems(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return items(mrb, self)
}

func condData(mrb *oruby.MrbState, self oruby.Value) *condvar {
	c, _ := mrb.Data(self).(*condvar)
	if c == nil {
		c = newCondvar()
		mrb.DataSetInterface(self, c)
	}
	return c
}

// ConditionVariable#wait(mutex, timeout = nil) -> self: mutex.sleep(timeout),
// which #signal and #broadcast wake
func condWait(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	mutex := args.Item(0)
	timeout := oruby.Nil
	if args.Len() > 1 {
		timeout = args.Item(1)
	}
	c := condData(mrb, self)
	t := mrb.CurrentThread()
	if t != nil {
		c.waiters = append(c.waiters, t)
	}
	_, err := mrb.Funcall(mutex, mrb.Intern("sleep"), timeout)
	if t != nil {
		for i, w := range c.waiters {
			if w == t {
				c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
				break
			}
		}
	}
	if err != nil {
		return mrb.RaiseError(err)
	}
	return self
}

func condSignal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c := condData(mrb, self)
	if len(c.waiters) > 0 {
		t := c.waiters[0]
		c.waiters = c.waiters[1:]
		mrb.ThreadWakeup(t)
	}
	return self
}

func condBroadcast(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c := condData(mrb, self)
	for _, t := range c.waiters {
		mrb.ThreadWakeup(t)
	}
	c.waiters = nil
	return self
}
