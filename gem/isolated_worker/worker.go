package isolated_worker

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/oruby/oruby"
)

type workerState struct {
	done       chan struct{}
	result     transferredValue
	failure    string
	valueReady bool
}

type transferredValue struct {
	kind   oruby.Type
	nilVal bool
	intVal int
	float  float64
	text   string
	frozen bool
	items  []transferredValue
}

func snapshotValue(mrb *oruby.MrbState, value oruby.Value, seen map[uintptr]bool) (transferredValue, error) {
	result := transferredValue{kind: value.Type()}
	switch value.Type() {
	case oruby.MrbTTFalse:
		result.nilVal = value.IsNil()
	case oruby.MrbTTTrue:
	case oruby.MrbTTFixnum:
		result.intVal = value.Int()
	case oruby.MrbTTFloat:
		result.float = value.Float64()
	case oruby.MrbTTSymbol:
		result.text = mrb.SymString(value.Symbol())
	case oruby.MrbTTString, oruby.MrbTTArray:
		result.frozen = value.Frozen()
		if seen[value.Pointer()] {
			return result, errors.New("aliased or cyclic worker result")
		}
		seen[value.Pointer()] = true
		if value.Type() == oruby.MrbTTString {
			if mrb.ClassOf(value).Value().Pointer() != mrb.StringClass().Value().Pointer() {
				return result, errors.New("string subclasses cannot be transferred")
			}
			result.text = value.String()
			break
		}
		if mrb.ClassOf(value).Value().Pointer() != mrb.ArrayClass().Value().Pointer() {
			return result, errors.New("array subclasses cannot be transferred")
		}
		for i := 0; i < value.Len(); i++ {
			item, err := snapshotValue(mrb, mrb.AryEntry(value, i), seen)
			if err != nil {
				return result, err
			}
			result.items = append(result.items, item)
		}
	default:
		return result, fmt.Errorf("unsupported worker result (type %d)", value.Type())
	}
	return result, nil
}

func (v transferredValue) materialize(mrb *oruby.MrbState) oruby.Value {
	switch v.kind {
	case oruby.MrbTTFalse:
		if v.nilVal {
			return mrb.NilValue()
		}
		return mrb.FalseValue()
	case oruby.MrbTTTrue:
		return mrb.TrueValue()
	case oruby.MrbTTFixnum:
		return mrb.FixnumValue(v.intVal)
	case oruby.MrbTTFloat:
		return mrb.FloatValue(v.float)
	case oruby.MrbTTSymbol:
		return mrb.SymbolValue(mrb.Intern(v.text))
	case oruby.MrbTTString:
		value := mrb.StrNew(v.text)
		if v.frozen {
			value.RBasic().SetFrozen(true)
		}
		return value
	case oruby.MrbTTArray:
		array := mrb.AryNewCapa(len(v.items))
		for _, item := range v.items {
			array.Push(item.materialize(mrb))
		}
		if v.frozen {
			array.Value().RBasic().SetFrozen(true)
		}
		return array.Value()
	}
	return mrb.NilValue()
}

func initializeWorker(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, proc := mrb.GetArgsWithBlock()
	if proc.IsNil() || proc.IsCFunc() {
		return mrb.Raise(mrb.EArgumentError(), "an interpreted Ruby block is required")
	}
	child := oruby.MrbOpen()
	mrb.CopyLazyFeatures(child)
	context := &Context{mrb: child, mrbCaller: mrb, proc: proc, args: args}
	if err := context.migrateState(); err != nil {
		child.Close()
		return mrb.RaiseError(err)
	}
	child.GCRegister(context.proc)
	for i := 0; i < context.args.Len(); i++ {
		child.GCRegister(context.args.Item(i))
	}
	w := &workerState{done: make(chan struct{})}
	mrb.DataSetInterface(self, w)
	mrb.WaitGroup.Add(1)
	go w.run(mrb, child, context.proc, context.args)
	return self
}

func (w *workerState) run(parent, child *oruby.MrbState, proc oruby.RProc, args oruby.RArgs) {
	defer parent.WaitGroup.Done()
	defer close(w.done)
	defer func() {
		if v := recover(); v != nil {
			w.failure = fmt.Sprintf("worker panic: %v", v)
		}
	}()
	defer child.Close()
	value, err := child.YieldArgvE(proc, args.SliceIntf()...)
	if err != nil {
		var rubyErr *oruby.RubyError
		if errors.As(err, &rubyErr) {
			w.failure = rubyErr.Error()
			rubyErr.Release()
		} else {
			w.failure = err.Error()
		}
		return
	}
	w.result, err = snapshotValue(child, value, make(map[uintptr]bool))
	if err != nil {
		w.failure = err.Error()
	}
}

func joinWorker(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	w, ok := mrb.Data(self).(*workerState)
	if !ok || w == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized isolated worker")
	}
	timeout := mrb.GetArgsFirst()
	if timeout.IsNil() {
		<-w.done
		return self
	}
	if !timeout.IsInteger() && timeout.Type() != oruby.MrbTTFloat {
		return mrb.Raise(mrb.ETypeError(), "timeout must be numeric")
	}
	seconds := timeout.Float64()
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64)/float64(time.Second) {
		return mrb.Raise(mrb.EArgumentError(), "timeout is out of range")
	}
	timer := time.NewTimer(time.Duration(seconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-w.done:
		return self
	case <-timer.C:
		select {
		case <-w.done:
			return self
		default:
			return mrb.NilValue()
		}
	}
}

func workerValue(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	w, ok := mrb.Data(self).(*workerState)
	if !ok || w == nil {
		return mrb.Raise(mrb.ETypeError(), "uninitialized isolated worker")
	}
	<-w.done
	if w.failure != "" {
		return mrb.Raise(mrb.ERuntimeError(), w.failure)
	}
	key := mrb.Intern("@__isolated_worker_result")
	if !w.valueReady {
		mrb.IVSet(self, key, w.result.materialize(mrb))
		w.valueReady = true
	}
	return mrb.IVGet(self, key)
}
