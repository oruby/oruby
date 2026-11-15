package thread

import (
	"math"
	"runtime"
	"time"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("thread", func(mrb *oruby.MrbState) interface{} {
		threadClass := mrb.DefineClass("Thread", mrb.ObjectClass())
		threadClass.DefineMethod("initialize", unsupportedThread, mrb.ArgsAny()|mrb.ArgsBlock())
		threadClass.DefineMethod("kill", unsupportedThread, mrb.ArgsNone())
		threadClass.DefineAlias("terminate", "kill")
		threadClass.DefineClassMethod("start", unsupportedThread, mrb.ArgsAny()|mrb.ArgsBlock())
		threadClass.DefineClassMethod("go", unsupportedThread, mrb.ArgsAny()|mrb.ArgsBlock())
		threadClass.DefineClassMethod("current", threadCurrent, mrb.ArgsNone())
		threadClass.DefineClassMethod("main", threadMain, mrb.ArgsNone())
		threadClass.DefineClassMethod("kill", unsupportedThread, mrb.ArgsReq(1))
		threadClass.DefineClassMethod("pass", threadPass, mrb.ArgsNone())
		threadClass.DefineClassMethod("stop", unsupportedThread, mrb.ArgsNone())

		mutexClass := mrb.DefineGoClass("Mutex", newMutex)
		mrb.ConstSet(threadClass, mrb.Intern("Mutex"), mutexClass.Value())
		mutexClass.DefineMethod("sleep", mutexSleep, mrb.ArgsReq(1))
		mutexClass.DefineMethod("synchronize", mutexSynchronize, mrb.ArgsReq(1))
		mutexClass.DefineAlias("locked?", "locked")

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

		mrb.DefineGlobalFunction("sleep", threadSleep, mrb.ArgsOpt(1))
		mrb.DefineModuleFunction(mrb.KernelModule(), "sleep", threadSleep, mrb.ArgsOpt(1))

		return nil
	})
}

func threadMain(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return threadCurrent(mrb, self)
}

func threadCurrent(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	klass := mrb.ClassGet("Thread")
	key := mrb.Intern("@__main_thread")
	current := mrb.IVGet(klass, key)
	if current.IsNil() {
		current = mrb.ObjAlloc(oruby.MrbTTObject, klass).Value()
		mrb.IVSet(klass, key, current)
	}
	return current
}

func unsupportedThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Raise(mrb.ENotImplementedError(), "Ruby Thread creation and control are not supported")
}

func threadPass(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runtime.Gosched()
	return oruby.Nil
}

func threadSleep(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.GetArgsFirst()
	var duration time.Duration
	if t.IsNil() {
		duration = time.Duration(math.MaxInt64)
	} else {
		duration = time.Duration(int64(t.Float64() * float64(time.Second)))
	}

	start := time.Now()
	select {
	case <-mrb.ExitChan():
	case <-time.After(duration):
	}

	return mrb.FloatValue(float64(time.Since(start)) / float64(time.Second))
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

	return mrb.FixnumValue(int(time.Since(start) / time.Second))
}

func mutexSynchronize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	mutex := mrb.Data(self).(*rmutex)
	proc := mrb.GetArgsBlock()

	if !proc.IsNil() {
		mutex.Lock()
		ret := mrb.YieldArgv(proc)
		mutex.Unlock()
		if mrb.Exc() != nil {
			return mrb.RaiseError(mrb.Err())
		}

		return ret
	}
	return mrb.NilValue()
}
