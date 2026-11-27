package oruby

// #include "go-mrb.h"
// #include "go-thread.h"
import "C"

import (
	"context"
	"math"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Ruby threads, as MRI has them: each Ruby Thread runs on its own goroutine
// (so on its own C and Go stack) and all share the one mrb_state. Only the
// thread holding the GVL runs Ruby or touches the state; it gives the GVL
// up where it waits (sleep, join, Mutex, Queue, ConditionVariable, IO and
// other Go waits through Blocking) and at a safepoint when its time slice
// ended and another thread waits. A thread switched out keeps its C frames
// where they are, so it can be suspended anywhere, in a Go method or in
// compiled code too. What of the state is per thread (go-thread.c: the
// contexts, the catch, the exception, the GC arena; here: Go-side scratch,
// the Timeout deadlines and wake channel) moves with the GVL.
//
// A state has no scheduler until its first Thread.new: single-threaded
// programs pay nothing.

// threadQuantum is the time slice: MRI's 100ms (ORUBY_THREAD_QUANTUM_MS)
var threadQuantum = func() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("ORUBY_THREAD_QUANTUM_MS")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 100 * time.Millisecond
}()

// Thread states, as Thread#status reports them
const (
	ThreadRun = iota
	ThreadSleep
	ThreadDead
)

// Thread is one Ruby thread of a state
type Thread struct {
	mrb  *MrbState
	st   *C.orb_tstate
	obj  Value // the Ruby Thread object
	keep Value // Array rooting the thread's arena while it is switched out
	main bool
	vs   *vmState // its Timeout deadlines and wake channel

	gvl  chan struct{} // the GVL was handed to this thread
	done chan struct{} // closed when the thread finished

	// Go-side scratch of the state, the running thread's
	fastArgs       []Value
	hasFastArgs    bool
	aotDepth       int
	aotIntDisabled int

	started atomic.Bool // its body runs: interrupts may be delivered
	intr    atomic.Bool // an interrupt was posted (Thread#raise, #kill)

	fiberSched   bool        // a Fiber scheduler is set (Fiber.set_scheduler)
	curFiber     *fiberUnit  // the goroutine Fiber it runs while it is switched out (fiber_go.go)
	rootUnit     *fiberUnit  // its own context as the unit that resumes a Fiber
	fiberJoiners []fiberWait // non-blocking fibers joining it (unblocked when it ends)

	// guarded by threadSched.mu
	status   int
	sleepers bool            // counted in threadSched.sleepers
	waitCh   <-chan struct{} // what it waits for in a deadlockable wait
	wakeup   bool            // Thread#wakeup / #run came while it slept
}

type threadSched struct {
	mu             sync.Mutex
	cur            *Thread   // holds the GVL; nil while nobody does
	queue          []*Thread // waiting for the GVL, first come first served
	threads        []*Thread // live threads, the main one first
	main           *Thread
	byObj          map[uintptr]*Thread
	sleepers       int // in a wait only another thread could end
	list           Value
	preempt        atomic.Int32 // the time slice ended
	ticking        bool
	freeList       []*C.orb_tstate // states of finished threads, freed once another is loaded
	active         bool            // counted in orb_threads_active
	ignoreDeadlock bool
}

// threadsInit creates the scheduler at the first Thread.new; mainObj is the
// Ruby object of the main thread (Thread.main)
func (mrb *MrbState) threadsInit(mainObj Value) *threadSched {
	if mrb.sched != nil {
		return mrb.sched
	}
	s := &threadSched{byObj: map[uintptr]*Thread{}}
	list := mrb.AryNew().Value()
	mrb.GCRegister(list)
	s.list = list
	var owner C.mrb_value
	st := C._orb_tstate_main(mrb.p, &owner)
	keep := mrb.AryNew().Value()
	main := &Thread{mrb: mrb, st: st, obj: mainObj, keep: keep, main: true, vs: mrb.mainVM(),
		gvl: make(chan struct{}, 1), done: make(chan struct{})}
	main.started.Store(true)
	main.fiberSched = mrb.mainFiberSched
	mrb.IVSet(mainObj, mrb.Intern("__oruby_owner"), Value{owner})
	mrb.IVSet(mainObj, mrb.Intern("__oruby_keep"), keep)
	s.main, s.cur = main, main
	s.threads = []*Thread{main}
	s.byObj[mainObj.Pointer()] = main
	mrb.AryPush(list, mainObj)
	mrb.sched = s
	return s
}

// ThreadsActive tells whether the state has threads other than its main one
func (mrb *MrbState) ThreadsActive() bool {
	s := mrb.sched
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.threads) > 1
}

// CurrentThread is the running thread (nil before the first Thread.new)
func (mrb *MrbState) CurrentThread() *Thread {
	if s := mrb.sched; s != nil {
		return s.cur
	}
	return nil
}

// ThreadOf is the thread of a Ruby Thread object; nil when it has not
// started or finished
func (mrb *MrbState) ThreadOf(obj Value) *Thread {
	s := mrb.sched
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byObj[obj.Pointer()]
}

// Object is the Ruby Thread object
func (t *Thread) Object() Value { return t.obj }

// Main tells whether t is the main thread
func (t *Thread) Main() bool { return t.main }

// Done is closed when the thread finished
func (t *Thread) Done() <-chan struct{} { return t.done }

// Status is ThreadRun, ThreadSleep or ThreadDead
func (t *Thread) Status() int {
	s := t.mrb.sched
	s.mu.Lock()
	defer s.mu.Unlock()
	return t.status
}

// Fiber is the Fiber t runs now (Nil for the running thread)
func (t *Thread) Fiber() Value {
	mrb := t.mrb
	if mrb.sched.cur == t {
		return Nil
	}
	return Value{C._orb_tstate_fiber(mrb.p, t.st)}
}

// ThreadRootFiber is the root Fiber of the Ruby thread obj (Nil before the
// first Thread.new for the main thread); it outlives the thread
func (mrb *MrbState) ThreadRootFiber(obj Value) Value {
	return mrb.IVGet(obj, mrb.Intern("__oruby_owner"))
}

// ThreadStart starts the Ruby thread obj running body (a Proc taking no
// arguments). mainObj is Thread.main, needed at the first start.
func (mrb *MrbState) ThreadStart(mainObj, obj, body Value) *Thread {
	s := mrb.threadsInit(mainObj)
	var owner C.mrb_value
	st := C._orb_tstate_new(mrb.p, &owner, body.v)
	keep := mrb.AryNew().Value()
	t := &Thread{mrb: mrb, st: st, obj: obj, keep: keep,
		vs:  &vmState{wake: make(chan struct{}, 1), timers: map[int64]context.CancelFunc{}},
		gvl: make(chan struct{}, 1), done: make(chan struct{})}
	// what the GC must keep while the thread lives: its owner Fiber, the
	// arena keeper and the body, through the Thread object in s.list
	mrb.IVSet(obj, mrb.Intern("__oruby_owner"), Value{owner})
	mrb.IVSet(obj, mrb.Intern("__oruby_keep"), keep)
	mrb.IVSet(obj, mrb.Intern("__oruby_body"), body)
	mrb.AryPush(s.list, obj)

	s.mu.Lock()
	s.threads = append(s.threads, t)
	s.byObj[obj.Pointer()] = t
	if !s.active {
		s.active = true
		C._orb_threads_active(1)
	}
	if !s.ticking {
		s.ticking = true
		go mrb.threadTicker(s)
	}
	s.mu.Unlock()

	go mrb.threadMain(t, body)
	return t
}

func (mrb *MrbState) threadMain(t *Thread, body Value) {
	s := mrb.sched
	s.acquire(t)
	var res C.mrb_value
	C._orb_thread_run(mrb.p, body.v, &res)
	mrb.threadFinish(t)
}

// threadFinish ends t, the current thread, and hands the GVL on for good
func (mrb *MrbState) threadFinish(t *Thread) {
	s := mrb.sched
	for _, f := range mrb.threadFinishHooks {
		f(t)
	}
	C._orb_tstate_finish(mrb.p, t.st)
	mrb.IVSet(t.obj, mrb.Intern("__oruby_dead"), True)
	mrb.IVSet(t.obj, mrb.Intern("__oruby_body"), Nil)
	if _, err := mrb.Funcall(s.list, mrb.Intern("delete"), t.obj); err != nil {
		mrb.ExcClear()
	}

	s.mu.Lock()
	t.status = ThreadDead
	if t.sleepers {
		t.sleepers = false
		s.sleepers--
	}
	for i, x := range s.threads {
		if x == t {
			s.threads = append(s.threads[:i], s.threads[i+1:]...)
			break
		}
	}
	delete(s.byObj, t.obj.Pointer())
	s.freeList = append(s.freeList, t.st)
	if len(s.threads) <= 1 && s.active {
		s.active = false
		C._orb_threads_active(-1)
	}
	s.mu.Unlock()
	close(t.done)
	t.vs.release()
	// fibers joining it through their scheduler, now that it is dead:
	// unblock(thread, fiber)
	for _, j := range t.fiberJoiners {
		if _, err := mrb.Funcall(j.sched, mrb.Intern("unblock"), j.thread, j.fiber); err != nil {
			mrb.ExcClear()
		}
	}
	t.fiberJoiners = nil

	// the state still has t's part: the next thread to take the GVL loads
	// its own, and frees t's then
	s.handoff()
}

// acquire waits for the GVL and makes t the running thread
func (s *threadSched) acquire(t *Thread) {
	s.mu.Lock()
	if s.cur == nil {
		s.cur = t
		s.mu.Unlock()
	} else {
		s.queue = append(s.queue, t)
		s.mu.Unlock()
		<-t.gvl
	}
	t.mrb.threadIn(t)
}

// release gives the GVL up: t's part of the state is saved
func (s *threadSched) release(t *Thread) {
	t.mrb.threadOut(t)
	s.handoff()
}

// handoff passes the GVL to the first waiting thread
func (s *threadSched) handoff() {
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.cur = nil
		s.mu.Unlock()
		return
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	s.cur = next
	s.mu.Unlock()
	next.gvl <- struct{}{}
}

// yield lets the threads waiting for the GVL run before t goes on
func (s *threadSched) yield(t *Thread) {
	s.mu.Lock()
	waiting := len(s.queue) > 0
	s.mu.Unlock()
	if waiting {
		s.release(t)
		s.acquire(t)
	}
}

func (mrb *MrbState) threadOut(t *Thread) {
	t.fastArgs, t.hasFastArgs = mrb.fastArgs, mrb.hasFastArgs
	t.curFiber = mrb.curFiber
	if mrb.aot != nil {
		t.aotDepth = mrb.aot.depth
		t.aotIntDisabled = mrb.aot.intDisabled
	}
	C._orb_tstate_save(mrb.p, t.st, t.keep.v)
}

func (mrb *MrbState) threadIn(t *Thread) {
	s := mrb.sched
	C._orb_tstate_load(mrb.p, t.st, t.keep.v)
	mrb.fastArgs, mrb.hasFastArgs = t.fastArgs, t.hasFastArgs
	mrb.curFiber = t.curFiber
	if mrb.aot != nil {
		mrb.aot.depth = t.aotDepth
		mrb.aot.intDisabled = t.aotIntDisabled
	}
	s.mu.Lock()
	free := s.freeList
	s.freeList = nil
	s.mu.Unlock()
	for _, st := range free {
		C._orb_tstate_free(mrb.p, st)
	}
	// what came for it while it was out (interrupts, signals for the main
	// thread, its fired deadlines) is taken at its next instruction
	C._orb_vm_arm(mrb.p)
}

// threadTicker ends time slices while threads wait for the GVL
func (mrb *MrbState) threadTicker(s *threadSched) {
	tk := time.NewTicker(threadQuantum)
	defer tk.Stop()
	for {
		select {
		case <-mrb.exitChan:
			return
		case <-tk.C:
		}
		s.mu.Lock()
		if !s.ticking {
			s.mu.Unlock()
			return
		}
		waiting := len(s.queue) > 0 && s.cur != nil
		if waiting {
			s.preempt.Store(1)
			C._orb_vm_arm(mrb.p)
		}
		s.mu.Unlock()
	}
}

//export go_thread_safepoint
func go_thread_safepoint(idx C.mrb_int) C.int {
	mrb := lookupMrbStateIndex(int(idx))
	if mrb == nil || mrb.sched == nil {
		return 0
	}
	s := mrb.sched
	t := s.cur
	if t == nil {
		return 0
	}
	if s.preempt.Swap(0) == 1 {
		s.yield(t)
	}
	var r C.int
	if t.started.Load() && t.intr.Load() {
		r |= 1
	}
	if !t.main {
		r |= 2
	}
	return r
}

// ThreadPass is Thread.pass: the threads waiting for the GVL run first
func (mrb *MrbState) ThreadPass() {
	if s := mrb.sched; s != nil && s.cur != nil {
		s.yield(s.cur)
	}
}

// Blocking runs f, which must not use the state, without the GVL: other
// Ruby threads run meanwhile. Go methods wrap what may block for long (IO,
// network, child processes, external waits) in it; f should also return
// when mrb.SignalWake() fires, for Thread#raise, #kill, signals and
// Timeout. With no other thread it just runs f.
func (mrb *MrbState) Blocking(f func()) {
	s := mrb.sched
	if s == nil || s.cur == nil {
		f()
		return
	}
	t := s.cur
	s.mu.Lock()
	t.status = ThreadSleep
	s.mu.Unlock()
	s.release(t)
	defer func() {
		s.acquire(t)
		s.mu.Lock()
		t.status = ThreadRun
		s.mu.Unlock()
	}()
	f()
}

// Wait outcomes of ThreadWait
const (
	WaitReady   = iota // the channel fired
	WaitTimeout        // the time passed
	WaitWoken          // Thread#wakeup, a signal handler ran, a Timeout or interrupt came
)

// ThreadWait blocks the current Ruby thread, without the GVL, until ready
// fires, d passes (d < 0: no limit) or it is woken. deadlockable is a wait
// that only another Ruby thread can end (Mutex, Queue, join, Thread.stop):
// when every thread is in one, it is MRI's deadlock, raised in the main
// thread. The error is what an interrupt, a signal handler or a Timeout
// raised; the caller raises it (RaiseError).
func (mrb *MrbState) ThreadWait(ready <-chan struct{}, d time.Duration, deadlockable bool) (int, error) {
	vs := mrb.vm()
	// a request posted before the wait is taken now
	select {
	case <-vs.wake:
	default:
	}
	if err := mrb.CheckSignals(); err != nil {
		return WaitWoken, err
	}
	var timer <-chan time.Time
	if d >= 0 {
		tm := time.NewTimer(d)
		defer tm.Stop()
		timer = tm.C
	}
	s := mrb.sched
	var t *Thread
	if s != nil {
		t = s.cur
	}
	if deadlockable && d < 0 {
		if err := mrb.deadlockCheck(t); err != nil {
			return WaitWoken, err
		}
	}
	if t != nil {
		s.mu.Lock()
		t.status = ThreadSleep
		t.wakeup = false
		if deadlockable && d < 0 {
			t.sleepers = true
			t.waitCh = ready
			s.sleepers++
		}
		s.mu.Unlock()
		s.release(t)
	}
	// a Timeout.timeout deadline or an EvalContext cancel ends the wait
	ctx := mrb.GoContext()
	r := WaitWoken
	ctxDone := false
	select {
	case <-ready:
		r = WaitReady
	case <-timer:
		r = WaitTimeout
	case <-vs.wake:
	case <-ctx.Done():
		ctxDone = true
	case <-mrb.exitChan:
		r = WaitTimeout
	}
	if t != nil {
		// runnable again: no longer a sleeper for the deadlock check, though
		// it waits for the GVL
		s.mu.Lock()
		if t.sleepers {
			t.sleepers = false
			t.waitCh = nil
			s.sleepers--
		}
		s.mu.Unlock()
		s.acquire(t)
		s.mu.Lock()
		t.status = ThreadRun
		s.mu.Unlock()
	}
	if ctxDone {
		// a Timeout raises now; an EvalContext stops the VM at its next instruction
		if err := mrb.ContextErr(ctx); err != nil {
			return WaitWoken, err
		}
		return WaitWoken, nil
	}
	if err := mrb.CheckSignals(); err != nil {
		return WaitWoken, err
	}
	return r, nil
}

// KernelSleep is Kernel#sleep(secs = nil) -> Integer, the seconds slept,
// rounded. Other threads run meanwhile; Thread#wakeup, a signal handler,
// an interrupt or a Timeout end it early.
func KernelSleep(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	d := time.Duration(-1)
	if args.Len() > 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 0..1)", args.Len())
	}
	if args.Len() > 0 && !args.Item(0).IsNil() {
		var err error
		if d, err = mrb.SleepDuration(args.Item(0)); err != nil {
			return mrb.RaiseError(err)
		}
	}
	start := time.Now()
	r, err := mrb.ThreadWait(nil, d, false)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if r == WaitWoken {
		mrb.TakeWakeup()
	}
	return mrb.FixnumValue(int(math.Round(time.Since(start).Seconds())))
}

// SleepDuration converts a time interval argument as MRI's sleep does: a
// Numeric (by #divmod for a non-Integer/Float), not negative; -1 for
// Float::INFINITY
func (mrb *MrbState) SleepDuration(t Value) (time.Duration, error) {
	var secs float64
	if !mrb.ObjIsKindOf(t, mrb.ClassGet("Numeric")) {
		// MRI's rb_time_interval: anything with #divmod (seconds, fraction)
		if !mrb.RespondTo(t, mrb.Intern("divmod")) {
			return 0, ETypeError("can't convert %s into time interval", mrb.ClassName(mrb.ClassOf(t)))
		}
		dm, err := mrb.Funcall(t, mrb.Intern("divmod"), 1)
		if err != nil {
			return 0, err
		}
		parts := RArraySlice(dm)
		if len(parts) != 2 {
			return 0, ETypeError("can't convert %s into time interval", mrb.ClassName(mrb.ClassOf(t)))
		}
		for _, p := range parts {
			f, err := mrb.Funcall(p, mrb.Intern("to_f"))
			if err != nil {
				return 0, err
			}
			secs += f.Float64()
		}
		t = Nil
	}
	switch {
	case t.IsNil():
	case t.IsInteger():
		secs = float64(t.Int())
	case t.Type() == MrbTTFloat:
		secs = t.Float64()
	default:
		f, err := mrb.Funcall(t, mrb.Intern("to_f"))
		if err != nil {
			return 0, err
		}
		secs = f.Float64()
	}
	if secs < 0 {
		return 0, EArgumentError("time interval must not be negative")
	}
	if math.IsInf(secs, 1) {
		return -1, nil
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// deadlockCheck: t is about to wait for another thread forever. When every
// other live thread does too, the main thread gets MRI's fatal; for the
// main thread itself it is the error now.
func (mrb *MrbState) deadlockCheck(t *Thread) error {
	s := mrb.sched
	if s == nil || t == nil {
		return mrb.deadlockError()
	}
	s.mu.Lock()
	// a sleeper already signalled (its channel ready, a wakeup or an
	// interrupt posted) is not stuck: it just has not run yet
	stuck := 0
	for _, x := range s.threads {
		if x != t && x.sleepers && !chanReady(x.waitCh) && len(x.vs.wake) == 0 && !x.intr.Load() {
			stuck++
		}
	}
	all := stuck+1 >= len(s.threads) && !s.ignoreDeadlock
	main := s.main
	s.mu.Unlock()
	if !all {
		return nil
	}
	if t == main {
		return mrb.deadlockError()
	}
	if exc := mrb.deadlockException(); !exc.IsNil() {
		mrb.ThreadPost(main, exc)
	}
	return nil
}

func (mrb *MrbState) deadlockException() Value {
	if !mrb.ClassDefinedID(Sym_Thread) {
		return Nil
	}
	v, err := mrb.Funcall(mrb.ClassGetID(Sym_Thread).Value(), mrb.Intern("__oruby_deadlock_error"))
	if err != nil {
		mrb.ExcClear()
		return Nil
	}
	return v
}

func (mrb *MrbState) deadlockError() error {
	exc := mrb.deadlockException()
	if exc.IsNil() {
		return EThreadError("No live threads left. Deadlock?")
	}
	return mrb.exceptionError(exc)
}

// ThreadIgnoreDeadlock is Thread.ignore_deadlock=
func (mrb *MrbState) ThreadIgnoreDeadlock(on bool) {
	if s := mrb.sched; s != nil {
		s.mu.Lock()
		s.ignoreDeadlock = on
		s.mu.Unlock()
	}
}

// ThreadPost raises exc in t at its next safepoint (Thread#raise): the Ruby
// side queues it
func (mrb *MrbState) ThreadPost(t *Thread, exc Value) {
	if _, err := mrb.Funcall(t.obj, mrb.Intern("__oruby_post"), exc); err != nil {
		mrb.ExcClear()
	}
}

// ThreadInterrupt marks an interrupt queued for t and wakes t if it waits
func (mrb *MrbState) ThreadInterrupt(t *Thread) {
	t.intr.Store(true)
	mrb.sched.runnable(t)
	t.vs.notify()
	if mrb.sched.cur == t {
		C._orb_vm_arm(mrb.p) // its own: taken at its next instruction
	}
}

// OnThreadFinish registers f, run (under the GVL) when a thread finishes:
// Mutexes it holds are released
func (mrb *MrbState) OnThreadFinish(f func(t *Thread)) {
	mrb.threadFinishHooks = append(mrb.threadFinishHooks, f)
}

// runnable: t was woken; it is no longer a sleeper for the deadlock check
func (s *threadSched) runnable(t *Thread) {
	s.mu.Lock()
	if t.sleepers {
		t.sleepers = false
		t.waitCh = nil
		s.sleepers--
	}
	s.mu.Unlock()
}

// Waiter is what a thread waits on (ThreadWait(w.C(), ...)) until another
// one wakes it; Wake makes it runnable at once, which the deadlock check
// needs to know before the woken thread has run
type Waiter struct {
	mrb *MrbState
	ch  chan struct{}
	t   *Thread
}

// NewWaiter is a Waiter for the current thread
func (mrb *MrbState) NewWaiter() *Waiter {
	return &Waiter{mrb: mrb, ch: make(chan struct{}, 1), t: mrb.CurrentThread()}
}

// C is the channel to wait on
func (w *Waiter) C() <-chan struct{} { return w.ch }

// Thread is the waiting thread (nil before the first Thread.new)
func (w *Waiter) Thread() *Thread { return w.t }

// Wake makes the waiter ready
func (w *Waiter) Wake() {
	if s := w.mrb.sched; s != nil && w.t != nil {
		s.runnable(w.t)
	}
	select {
	case w.ch <- struct{}{}:
	default:
	}
}

// ThreadInterruptTaken: the Ruby side took t's interrupts (more: some are
// left, deferred by Thread.handle_interrupt)
func (t *Thread) InterruptTaken(more bool) { t.intr.Store(more) }

// InterruptPending tells whether an interrupt (Thread#raise, #kill) is queued
// for t and not yet taken; with Thread.handle_interrupt it can stay so
func (t *Thread) InterruptPending() bool { return t.intr.Load() }

// Started: t's body runs, interrupts may be delivered
func (t *Thread) Started() { t.started.Store(true) }

// ThreadWakeup is Thread#wakeup: t stops sleeping
func (mrb *MrbState) ThreadWakeup(t *Thread) {
	s := mrb.sched
	s.mu.Lock()
	t.wakeup = true
	s.mu.Unlock()
	s.runnable(t)
	t.vs.notify()
}

// TakeWakeup tells whether the current thread was woken (and forgets it)
func (mrb *MrbState) TakeWakeup() bool {
	s := mrb.sched
	if s == nil || s.cur == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.cur.wakeup
	s.cur.wakeup = false
	return w
}

// Threads lists the live threads, the main one first
func (mrb *MrbState) Threads() []*Thread {
	s := mrb.sched
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Thread(nil), s.threads...)
}

// MainThread is the main thread (nil before the first Thread.new)
func (mrb *MrbState) MainThread() *Thread {
	if s := mrb.sched; s != nil {
		return s.main
	}
	return nil
}

// onMainThread: the running thread is the main one
func (mrb *MrbState) onMainThread() bool {
	s := mrb.sched
	return s == nil || s.cur == nil || s.cur.main
}

// threadsTerminate kills the other threads when the main one ends, as MRI
// does after at_exit, and waits for them to finish (their ensure clauses)
func (mrb *MrbState) threadsTerminate() {
	s := mrb.sched
	if s == nil || s.cur == nil || !s.cur.main {
		return
	}
	for _, t := range mrb.Threads() {
		if t.main {
			continue
		}
		if _, err := mrb.Funcall(t.obj, mrb.Intern("kill")); err != nil {
			mrb.ExcClear()
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, t := range mrb.Threads() {
		if t.main {
			continue
		}
		for t.Status() != ThreadDead && time.Now().Before(deadline) {
			if _, err := mrb.ThreadWait(t.done, time.Until(deadline), false); err != nil {
				mrb.ExcClear()
			}
		}
	}
	s.mu.Lock()
	s.ticking = false
	if s.active {
		s.active = false
		C._orb_threads_active(-1)
	}
	s.mu.Unlock()
}

// chanReady tells, without taking it, whether a receive on ch would not
// block: a buffered channel with a token, or a closed one
func chanReady(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	if cap(ch) > 0 {
		return len(ch) > 0
	}
	select {
	case <-ch:
		return true // closed: receiving takes nothing
	default:
		return false
	}
}

// fiberWait is a non-blocking fiber waiting through its Fiber scheduler
type fiberWait struct {
	fiber, sched, thread Value // thread: the joining fiber's
}

// SetFiberScheduler records whether the running thread has a Fiber
// scheduler (Fiber.set_scheduler): blocking operations ask for it only then
func (mrb *MrbState) SetFiberScheduler(on bool) {
	if t := mrb.CurrentThread(); t != nil {
		t.fiberSched = on
		return
	}
	mrb.mainFiberSched = on
}

// FiberScheduler is the scheduler a blocking operation of the current fiber
// goes through (Fiber.current_scheduler): Nil without one, or for a
// blocking fiber
func (mrb *MrbState) FiberScheduler() Value {
	on := mrb.mainFiberSched
	if t := mrb.CurrentThread(); t != nil {
		on = t.fiberSched
	}
	if !on || !mrb.ClassDefinedID(Sym_Fiber) {
		return Nil
	}
	v, err := mrb.Funcall(mrb.ClassGetID(Sym_Fiber).Value(), mrb.Intern("__oruby_sched"))
	if err != nil {
		mrb.ExcClear()
		return Nil
	}
	return v
}

// CurrentFiber is Fiber.current
func (mrb *MrbState) CurrentFiber() Value {
	v, err := mrb.Funcall(mrb.ClassGetID(Sym_Fiber).Value(), mrb.Intern("current"))
	if err != nil {
		mrb.ExcClear()
		return Nil
	}
	return v
}

// DeferUnblock queues sched.unblock(blocker, fiber) for the Ruby side to
// call (TakeUnblocks): a scheduler's unblock may switch fibers, which a
// fiber cannot do across a Go method's frame
func (mrb *MrbState) DeferUnblock(sched, blocker, fiber Value) {
	if mrb.pendingUnblocks.IsNil() {
		mrb.pendingUnblocks = mrb.AryNew().Value()
		mrb.GCRegister(mrb.pendingUnblocks)
	}
	mrb.AryPush(mrb.pendingUnblocks, mrb.AryNewFromValues(sched, blocker, fiber))
}

// TakeUnblocks: the queued unblocks, as [scheduler, blocker, fiber] triples
func (mrb *MrbState) TakeUnblocks() Value {
	if mrb.pendingUnblocks.IsNil() || RArrayLen(mrb.pendingUnblocks) == 0 {
		return Nil
	}
	out := mrb.Call(mrb.pendingUnblocks, "dup")
	mrb.Call(mrb.pendingUnblocks, "clear")
	return out
}

// ThreadUnjoinFiber drops fiber from t's joiners (its join timed out)
func (mrb *MrbState) ThreadUnjoinFiber(t *Thread, fiber Value) {
	for i, j := range t.fiberJoiners {
		if j.fiber.Pointer() == fiber.Pointer() {
			t.fiberJoiners = append(t.fiberJoiners[:i], t.fiberJoiners[i+1:]...)
			return
		}
	}
}

// ThreadJoinFiber registers the current non-blocking fiber, waiting through
// sched for t to end: when it does, sched.unblock(t, fiber) is called. The
// fiber and scheduler are kept on t's object for the GC.
func (mrb *MrbState) ThreadJoinFiber(t *Thread, sched Value) {
	f := mrb.CurrentFiber()
	joiner := Nil
	if c := mrb.CurrentThread(); c != nil {
		joiner = c.obj
	}
	t.fiberJoiners = append(t.fiberJoiners, fiberWait{f, sched, joiner})
	keep := mrb.IVGet(t.obj, mrb.Intern("__oruby_joiners"))
	if keep.IsNil() {
		keep = mrb.AryNew().Value()
		mrb.IVSet(t.obj, mrb.Intern("__oruby_joiners"), keep)
	}
	mrb.AryPush(keep, f)
	mrb.AryPush(keep, sched)
}
