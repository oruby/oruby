package isolated_worker

import (
	"fmt"
	"sync"
	"time"

	"github.com/oruby/oruby"
)

type ThreadFuncT = func(...interface{}) interface{}

type Context struct {
	sync.Mutex
	Name         string
	mrb          *oruby.MrbState
	mrbCaller    *oruby.MrbState
	args         oruby.RArgs
	proc         oruby.RProc
	f            ThreadFuncT
	result       oruby.Value
	excValue     oruby.Value
	err          error
	resultCaller oruby.Value
	alive        bool
	sleeping     bool
	wakeupChan   chan struct{}
}

var activeContexts sync.Map

func newThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Raise(mrb.ENotImplementedError(), "legacy isolated worker launch is unsafe")
}

// legacyNewThread is retained for research and must not be registered.
func legacyNewThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, proc := mrb.GetArgsWithBlock()

	if proc.IsNil() {
		return mrb.ClassGet("ThreadError").Raise("must be called with a block")
	}

	if proc.IsCFunc() {
		return mrb.ERuntimeError().Raise("forking C defined block")
	}

	c := &Context{
		sync.Mutex{},
		"",
		oruby.MrbOpen(),
		mrb,
		args,
		proc,
		nil,
		mrb.NilValue(),
		oruby.Nil,
		nil,
		mrb.NilValue(),
		false,
		false,
		make(chan struct{}),
	}

	mrb.CopyLazyFeatures(c.mrb)
	err := c.migrateState()
	if err != nil {
		c.result = mrb.NilValue()
		c.mrb.Close()
		c.mrb = nil

		return mrb.ERuntimeError().RaiseError(err)
	}

	c.alive = true
	threadClass := c.mrb.ClassGet("Thread")
	if !threadClass.IsNil() {
		c.mrb.CVSet(threadClass, c.mrb.Intern("@@current"), c.mrb.DataWrapInterface(threadClass, c).Value())
	}
	c.mrb.WaitGroup.Add(1)
	go c.worker()

	mrb.DataSetInterface(self, c)

	return self
}

// threadCurrent returns current thread
func threadCurrent(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	threadClass := mrb.ClassGet("Thread")
	sym := mrb.Intern("@@current")
	val := mrb.ModCVGet(threadClass, sym)
	if !val.IsNil() {
		return val
	}
	c := &Context{
		sync.Mutex{},
		"main",
		mrb,
		nil,
		oruby.RArgs{},
		oruby.RProc{},
		nil,
		mrb.NilValue(),
		oruby.Nil,
		nil,
		mrb.NilValue(),
		true,
		false,
		make(chan struct{}),
	}
	mainThread := mrb.DataWrapInterface(threadClass, c).Value()
	mrb.ModCVSet(threadClass, sym, mainThread)
	return mainThread
}

// goThread creates lightweight thread based go function executed via goroutine
func goThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Raise(mrb.ENotImplementedError(), "direct Go callbacks are unsupported in isolated workers")
}

// legacyGoThread is retained for research and must not be registered.
func legacyGoThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, proc := mrb.GetArgsWithBlock()

	if proc.IsNil() {
		proc = mrb.RProc(args.Item(-1))
		if proc.IsNil() {
			return mrb.ClassGet("ThreadError").Raise("must be called with a block or proc argument")
		}
	}

	c := &Context{
		sync.Mutex{},
		"",
		oruby.MrbOpen(),
		mrb,
		args,
		proc,
		nil,
		mrb.NilValue(),
		oruby.Nil,
		nil,
		mrb.NilValue(),
		true,
		false,
		make(chan struct{}),
	}

	mrb.CopyLazyFeatures(c.mrb)
	switch f := proc.Data().(type) {
	case oruby.MrbFuncT:
		c.proc = proc
	case ThreadFuncT:
		c.f = f
	case nil:
	}

	c.mrb.WaitGroup.Add(1)
	go c.worker()

	mrb.DataSetInterface(self, c)

	return self
}

func (c *Context) worker() {
	activeContexts.Store(c.mrb, c)
	defer activeContexts.Delete(c.mrb)

	c.result = c.mrb.YieldWithClass(c.proc, c.mrb.NilValue(), c.mrb.ObjectClass(), c.args.SliceIntf()...)
	c.err = c.mrb.Err()
	if exc := c.mrb.Exc(); exc != nil {
		c.excValue = exc.Value()
		c.mrb.GCProtect(c.excValue)
	}
	c.mrb.GCProtect(c.result)

	c.mrb.WaitGroup.Done()
	c.alive = false
}

// Join will make the calling thread suspend execution and run this thr.
// Does not return until thr exits or until the given limit seconds have passed.
//
// If the time limit expires, nil will be returned, otherwise thr is returned.
//
// Any threads not joined will be killed when the main program exits.
//
// [-] If thr had previously raised an exception and the ::abort_on_exception or $DEBUG flags are not set,
// [-] (so the exception has not yet been processed), it will be processed at this time.
// a = Thread.new { print "a"; sleep(10); print "b"; print "c" }
func (c *Context) Join(limit ...int) (interface{}, error) {
	c.Lock()
	finished := c.mrb == nil
	c.Unlock()
	if finished {
		return c.resultCaller, c.err
	}

	c.Wakeup()

	if len(limit) > 0 && limit[0] > 0 {
		done := make(chan struct{})
		go func() {
			defer close(done)
			c.mrb.WaitGroup.Wait()
		}()
		select {
		case <-done:
			break // completed normally
		case <-time.After(time.Duration(limit[0]) * time.Second):
			// timout expired - return nil
			return oruby.Nil, nil
		}
	} else {
		// Wait forever
		c.mrb.WaitGroup.Wait()
	}

	c.Lock()
	defer c.Unlock()
	if c.mrb == nil {
		return c.resultCaller, c.err
	}

	v, err := migrateValue(c.mrb, c.mrbCaller, c.result)
	if err != nil {
		return nil, err
	}
	c.resultCaller = v.Value()
	if c.err != nil && c.excValue.Type() == oruby.MrbTTException {
		if ev, err := migrateValue(c.mrb, c.mrbCaller, c.excValue); err == nil && ev.Value().Type() == oruby.MrbTTException {
			c.mrbCaller.GCProtect(ev)
			c.err = &oruby.ExcValueError{Exc: ev.Value(), Msg: c.err.Error()}
		}
	}
	c.mrb.Close()
	c.mrb = nil

	return c.resultCaller, nil
}

func (c *Context) Kill() interface{} {
	c.Lock()
	defer c.Unlock()

	if c.mrb == nil {
		return nil
	}

	c.mrb.Close()
	c.mrb = nil
	c.alive = false

	return c.result
}

func (c *Context) IsAlive() bool {
	c.Lock()
	defer c.Unlock()

	return c.alive
}

func (c *Context) isSleeping() bool {
	c.Lock()
	defer c.Unlock()

	return c.sleeping
}

func (c *Context) IsStop() bool {
	c.Lock()
	defer c.Unlock()

	return c.sleeping || !c.alive
}

func (c *Context) Stop() {
	if !c.IsAlive() || c.isSleeping() {
		return
	}

	c.mrb.InjectFunc(func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c.Lock()
		c.sleeping = true
		c.Unlock()

		select {
		case <-mrb.ExitChan():
			break
		case <-c.wakeupChan:
			break
		}

		c.Lock()
		c.sleeping = false
		c.Unlock()

		return oruby.Nil
	})
}

func (c *Context) Wakeup() *Context {
	if c.isSleeping() {
		c.wakeupChan <- struct{}{}
	}
	return c
}

func (c *Context) Status() interface{} {
	c.Lock()
	defer c.Unlock()

	// "aborting" If this thread is aborting

	if !c.alive {
		if c.err != nil {
			return nil
		}
		return false
	}

	if c.sleeping {
		return "sleep"
	}

	// 	When this thread is executing
	return "run"
}

func (c *Context) Value() (interface{}, error) {
	_, _ = c.Join()
	return c.resultCaller, c.err
}

func (c *Context) ToS() string {
	return fmt.Sprintf("#<Thread:%p %v>", c, c.Status())
}
