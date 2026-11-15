package isolated_worker

import (
	"bytes"

	"github.com/oruby/oruby"
)

type transfer struct {
	source, destination *oruby.MrbState
	seen                map[uintptr]bool
}

func newTransfer(source, destination *oruby.MrbState) *transfer {
	return &transfer{source: source, destination: destination, seen: make(map[uintptr]bool)}
}

func migrateValue(source, destination *oruby.MrbState, value oruby.Value) (oruby.MrbValue, error) {
	return newTransfer(source, destination).copy(value)
}

func (t *transfer) copy(value oruby.Value) (oruby.MrbValue, error) {
	switch value.Type() {
	case oruby.MrbTTFalse, oruby.MrbTTTrue, oruby.MrbTTFixnum:
		return value, nil
	case oruby.MrbTTFloat:
		return t.destination.FloatValue(value.Float64()), nil
	case oruby.MrbTTSymbol:
		return t.destination.SymbolValue(t.destination.Intern(t.source.SymString(value.Symbol()))), nil
	case oruby.MrbTTString, oruby.MrbTTArray:
		if t.seen[value.Pointer()] {
			return oruby.Nil, oruby.EArgumentError("aliased or cyclic values cannot be transferred")
		}
		t.seen[value.Pointer()] = true
		if value.Type() == oruby.MrbTTString {
			if t.source.ClassOf(value).Value().Pointer() != t.source.StringClass().Value().Pointer() {
				return oruby.Nil, oruby.ETypeError("string subclasses cannot be transferred")
			}
			copy := t.destination.StringValue(value.String())
			if value.Frozen() {
				copy.Value().RBasic().SetFrozen(true)
			}
			return copy, nil
		}
		if t.source.ClassOf(value).Value().Pointer() != t.source.ArrayClass().Value().Pointer() {
			return oruby.Nil, oruby.ETypeError("array subclasses cannot be transferred")
		}
		array := t.destination.AryNewCapa(value.Len())
		for i := 0; i < value.Len(); i++ {
			item, err := t.copy(t.source.AryEntry(value, i))
			if err != nil {
				return oruby.Nil, err
			}
			array.Push(item)
		}
		if value.Frozen() {
			array.Value().RBasic().SetFrozen(true)
		}
		return array, nil
	default:
		return oruby.Nil, oruby.ETypeError("unsupported isolated worker transfer (type %d)", value.Type())
	}
}

func (c *Context) migrateState() error {
	t := newTransfer(c.mrbCaller, c.mrb)
	proc, err := t.copyProc(c.proc)
	if err != nil {
		return err
	}
	args := make([]oruby.Value, c.args.Len())
	for i := range args {
		value, err := t.copy(c.args.Item(i))
		if err != nil {
			return err
		}
		args[i] = value.Value()
	}
	c.proc = proc
	c.args = oruby.RArgsNew(args...)
	return nil
}

func migrateRProc(source, destination *oruby.MrbState, proc oruby.RProc) (oruby.RProc, error) {
	return newTransfer(source, destination).copyProc(proc)
}

func (t *transfer) copyProc(proc oruby.RProc) (oruby.RProc, error) {
	if proc.IsNil() || proc.IsCFunc() {
		return oruby.RProc{}, oruby.ETypeError("native or missing blocks cannot be transferred")
	}
	if proc.Strict() || proc.Orphan() {
		return oruby.RProc{}, oruby.ETypeError("strict or orphaned closures cannot be transferred")
	}
	if t.seen[proc.Value().Pointer()] {
		return oruby.RProc{}, oruby.EArgumentError("recursive or aliased closures cannot be transferred")
	}
	t.seen[proc.Value().Pointer()] = true
	var stack []oruby.Value
	if proc.HasEnv() {
		env := proc.Env()
		if t.seen[env.Value().Pointer()] {
			return oruby.RProc{}, oruby.EArgumentError("aliased closure environments cannot be transferred")
		}
		t.seen[env.Value().Pointer()] = true
		stack = make([]oruby.Value, env.Len())
		for i := range stack {
			value := env.Stack(i)
			if i == 0 && value.Pointer() == t.source.TopSelf().Pointer() {
				stack[i] = t.destination.TopSelf()
				continue
			}
			copied, err := t.copy(value)
			if err != nil {
				return oruby.RProc{}, err
			}
			stack[i] = copied.Value()
		}
	}
	var upper oruby.RProc
	if !proc.Upper().IsNil() {
		var err error
		upper, err = t.copyProc(proc.Upper())
		if err != nil {
			return oruby.RProc{}, err
		}
	}
	var bytecode bytes.Buffer
	if _, err := t.source.DumpIrep(proc.IRep(), 0, &bytecode); err != nil {
		return oruby.RProc{}, err
	}
	irep, err := t.destination.ReadIrep(bytecode.Bytes())
	if err != nil {
		return oruby.RProc{}, err
	}
	copied := t.destination.ProcNew(irep)
	t.destination.IrepDecref(copied.IRep())
	copied.SetTargetClass(t.destination.ObjectClass())
	if proc.HasEnv() {
		copied.SetEnv(stack...)
	}
	if !upper.IsNil() {
		copied.SetUpper(upper)
	}
	return copied, nil
}
