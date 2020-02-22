package thread

import (
	"github.com/oruby/oruby"
	"runtime"
	"sync"
)

type ThreadFuncT = func(...interface{})interface{}

type Context struct {
	sync.Mutex
	mrb *oruby.MrbState
	mrbCaller *oruby.MrbState
	args oruby.RArgs
	proc oruby.RProc
	f ThreadFuncT
	result oruby.Value
	resultCaller oruby.Value
	alive bool
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
		oruby.MrbOpen(),
		mrb,
		args,
		proc,
		nil,
		mrb.NilValue(),
		mrb.NilValue(),
		false,
	}

	err := c.migrateState()
	if err != nil {
		c.result = mrb.NilValue()
		c.mrb.Close()
		c.mrb = nil

		return c.result
	}

	c.alive = true
	c.mrb.WaitGroup.Add(1)
	go c.worker()

	mrb.DataSetInterface(self, c)

	return self
}

// goThread creates lightweit thread based go function executed via goroutine
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
		oruby.MrbOpen(),
		mrb,
		args,
		proc,
		nil,
		mrb.NilValue(),
		mrb.NilValue(),
		true,
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
	c.result, _ = c.mrb.YieldWithClass(c.proc, c.mrb.NilValue(), c.mrb.ObjectClass(), c.args.SliceIntf()...)
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

	v, err := c.migrateValue(c.result)
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

