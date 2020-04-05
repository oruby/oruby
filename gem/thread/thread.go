package thread

import (
	"github.com/oruby/oruby"
	"math"
	"time"
)

func init() {
	oruby.Gem("thread", func(mrb *oruby.MrbState)interface{} {
		threadClass := mrb.DefineClass("Thread", mrb.ObjectClass())
		threadClass.RegisterGoClass(func() *Context { return &Context{} })
		threadClass.Populate()
		threadClass.Const("COPY_VALUES", true)

		threadClass.DefineMethod("initialize", newThread, mrb.ArgsAny()+mrb.ArgsBlock())
		threadClass.DefineAlias("terminate", "kill")
		threadClass.DefineModuleFunction("start", newThread, mrb.ArgsAny()+mrb.ArgsBlock())
		threadClass.DefineModuleFunction("go", goThread, mrb.ArgsAny()+mrb.ArgsBlock())

		mutexClass := mrb.DefineGoClass( "Mutex", newMutex)
		mutexClass.DefineMethod("sleep", mutexSleep,mrb.ArgsReq(1))
		mutexClass.DefineMethod("synchronize", mutexSynchronize, mrb.ArgsReq(1))

		queueClass := mrb.DefineGoClass("Queue", &queue{})
		queueClass.DefineMethod("initialize", newQueue, mrb.ArgsOpt(1))

		queueClass.DefineAlias("<<", "push")
		queueClass.DefineAlias("enq", "push")
		queueClass.DefineAlias("unshift", "push")
		queueClass.DefineAlias("deq", "pop")
		queueClass.DefineAlias("shift", "pop")

		sizedQueueClass := mrb.DefineClass("SizedQueue", queueClass)
		sizedQueueClass.AttachType((*sizedQueue)(nil))
		sizedQueueClass.DefineMethod("initialize", newSizedQueue, mrb.ArgsReq(1))

		mrb.DefineClass("ThreadError", mrb.EStandardErrorClass())
		mrb.DefineClass("ClosedQueueError", mrb.ClassGet("StopIteration"))

		return nil
	})
}


func mutexSleep(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	mutex := mrb.Data(self).(*rmutex)
	t := mrb.GetArgsFirst()
	duration := time.Duration(math.MaxInt64)

	mutex.Unlock()

	if !t.IsNil() {
		duration = time.Duration(t.Int()) * time.Second
	}

	start := time.Now()

	select {
	case <-time.After(duration):
		mutex.Lock()
	case <-mrb.ExitChan():
	}

	return mrb.FixnumValue(int(time.Since(start)/time.Second))
}

func mutexSynchronize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	mutex := mrb.Data(self).(*rmutex)
	proc := mrb.GetArgsBlock()

	if !proc.IsNil() {
		mutex.Lock()
		ret, err := mrb.YieldArgv(proc)
		mutex.Unlock()
		if err != nil {
			return mrb.RaiseError(err)
		}

		return ret
	}
	return mrb.NilValue()
}

