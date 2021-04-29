package thread

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/oruby/oruby"
)

type ThreadFuncT = func(...interface{})interface{}

type Context struct {
	sync.Mutex
	Name string
	mrb *oruby.MrbState
	mrbCaller *oruby.MrbState
	args oruby.RArgs
	proc oruby.RProc
	f ThreadFuncT
	result oruby.Value
	err error
	resultCaller oruby.Value
	alive bool
	sleeping bool
	wakeupChan  chan struct{}
}

func newThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
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
		nil,
		mrb.NilValue(),
		false,
		false,
		make(chan struct{}),
	}

	err := c.migrateState()
	if err != nil {
		c.result = mrb.NilValue()
		c.mrb.Close()
		c.mrb = nil

		return mrb.ERuntimeError().RaiseError(err)
	}

	c.alive = true
	c.mrb.ModCVSet(mrb.ClassGet("Thread"), mrb.Intern("@@current"), c.mrb.Value(c))
	c.mrb.WaitGroup.Add(1)
	go c.worker()

	mrb.DataSetInterface(self, c)

	return self
}

// threadCurrent returns current thread
func threadCurrent(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.ModCVGet(mrb.ClassOf(self), mrb.Intern("@@current"))
}

// goThread creates lightweight thread based go function executed via goroutine
func goThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
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
		nil,
		mrb.NilValue(),
		true,
		false,
		make(chan struct{}),
	}

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
	c.result, c.err = c.mrb.YieldWithClass(c.proc, c.mrb.NilValue(), c.mrb.ObjectClass(), c.args.SliceIntf()...)
	c.mrb.GCProtect(c.result)

	c.mrb.WaitGroup.Done()
	c.alive = false
}

func (c *Context) Join() (interface{}, error) {
	if !c.alive {
		return c.resultCaller, nil
	}

	c.mrb.WaitGroup.Wait()

	c.Lock()
	defer c.Unlock()

	v, err := migrateValue(c.mrb, c.mrbCaller, c.result)
	if err != nil {
		return nil, err
	}
	c.resultCaller = v.Value()
	c.mrb.Close()
	c.mrb = nil

	return c.resultCaller, nil
}

func (c *Context) Kill() interface{} {
	if c.mrb == nil {
		return nil
	}

	c.Lock()
	defer c.Unlock()

	c.mrb.Close()
	c.mrb = nil
	c.alive = false

	return c.result
}

func (c *Context) Pass() {
	runtime.Gosched()
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

func (c *Context) Paas() {
	runtime.Gosched()
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
	return c.Join()
}

func (c *Context) ToS() string {
	return fmt.Sprintf("#<Thread:%p %v>", c, c.Status())
}
