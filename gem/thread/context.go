package thread

import (
	"github.com/oruby/oruby"
	"math"
	"runtime"
	"time"
)

type Context struct {
	mrb *oruby.MrbState
	mrbCaller *oruby.MrbState
	args oruby.RArgs
	proc oruby.RProc
	result oruby.Value
	alive bool
}

func (c *Context) thread() {
	c.mrb.WaitGroup.Add(1)

	c.result, _ = c.mrb.YieldWithClass(c.proc, c.mrb.NilValue(), c.mrb.ObjectClass(), c.args.SliceIntf()...)
	c.mrb.GCProtect(c.result)

	c.mrb.WaitGroup.Done()
	c.alive = false
}

func newThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, proc := mrb.GetArgsWithBlock()

	if proc.IsNil() {
		return mrb.ClassGet("ThreadError").Raise("must be called with a block")
	}

	if mrb.RProc(proc).IsCFunc() {
		return mrb.ERuntimeError().Raise("forking C defined block")
	}

	c := &Context{
		mrb: oruby.MrbOpen(),
		mrbCaller: mrb,
		args: args,
		proc: proc,
		result: mrb.NilValue(),
		alive: false,
	}

	err := c.migrateState()
	if err != nil {
		c.result = mrb.NilValue()
		c.mrb.Close()
		c.mrb = nil

		return c.result
	}

	c.alive = true
	go c.thread()

	return mrb.DataValue(c)
}

func (c *Context) Join() (interface{}, error) {
	if !c.alive {
		return c.result, nil
	}

	c.mrb.WaitGroup.Wait()

	v, err := c.migrateValue(c.result)
	if err != nil {
		return nil, err
	}

	c.result = v.Value()

	c.mrb.Close()
	c.mrb = nil
	return c.result, nil
}

func (c *Context) Kill() interface{} {
	if c.mrb == nil {
		return nil
	}

	c.mrb.Close()
	c.mrb = nil

	return c.result
}

func (c *Context) Pass() {
	runtime.Gosched()
}

func (c *Context) IsAlive() bool {
	return c.alive
}

func (c *Context) Sleep(t *int) {
	var duration time.Duration
	if t == nil {
		duration = time.Duration(math.MaxInt64)
	} else {
		duration = time.Duration(int(*t * 1000))
	}

	select {
	case <-c.mrb.ExitChan():
		break
	case <-time.After(duration * time.Millisecond):
		break
	}
}
