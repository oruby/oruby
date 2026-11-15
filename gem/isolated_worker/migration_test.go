package isolated_worker

import (
	"runtime"
	"testing"

	"github.com/oruby/oruby"
)

func TestMigrationRejectsAliases(t *testing.T) {
	source, destination := oruby.MrbOpen(), oruby.MrbOpen()
	defer source.Close()
	defer destination.Close()
	value, err := source.Eval(`s = "same"; [s, s]`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrateValue(source, destination, value.Value()); err == nil {
		t.Fatal("migrating aliased objects succeeded")
	}
}

func TestMigrationCopiesClosureAndArguments(t *testing.T) {
	source, destination := oruby.MrbOpen(), oruby.MrbOpen()
	defer source.Close()
	defer destination.Close()
	source.DefineGlobalFunction("copy_closure", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args, proc := mrb.GetArgsWithBlock()
		original := args.Item(0)
		c := &Context{mrb: destination, mrbCaller: source, args: args, proc: proc}
		if err := c.migrateState(); err != nil {
			t.Errorf("migrate closure: %v", err)
			return mrb.NilValue()
		}
		if args.Item(0).Pointer() != original.Pointer() || c.args.Item(0).Pointer() == original.Pointer() {
			t.Error("argument storage was not copied into the destination")
		}
		result, err := destination.YieldArgvE(c.proc, c.args.Item(0))
		if err != nil || result.String() != "captured argument" {
			t.Errorf("copied closure returned %v, %v", result, err)
		}
		return mrb.NilValue()
	}, oruby.ArgsReq(1)|oruby.ArgsBlock())
	if _, err := source.Eval(`prefix = "captured "; copy_closure("argument") { |arg| prefix + arg }`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRejectsUnsupportedGraphs(t *testing.T) {
	source, destination := oruby.MrbOpen(), oruby.MrbOpen()
	defer source.Close()
	defer destination.Close()
	for _, code := range []string{
		`a = []; a << a; a`,
		`{a: 1}`,
		`Object.new`,
	} {
		value, err := source.Eval(code)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrateValue(source, destination, value.Value()); err == nil {
			t.Errorf("unsupported transfer %q succeeded", code)
		}
	}
	data := source.Value(&struct{ N int }{1})
	if _, err := migrateValue(source, destination, data); err == nil {
		t.Fatal("Go-backed data transfer succeeded")
	}
}

func TestMigrationRejectsRecursiveClosure(t *testing.T) {
	source, destination := oruby.MrbOpen(), oruby.MrbOpen()
	defer source.Close()
	defer destination.Close()
	source.DefineGlobalFunction("copy_recursive", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		proc := mrb.GetArgsBlock()
		if _, err := migrateRProc(source, destination, proc); err == nil {
			t.Error("recursive closure transfer succeeded")
		}
		return mrb.NilValue()
	}, oruby.ArgsBlock())
	if _, err := source.Eval(`f = nil; f = proc { f.call }; copy_recursive(&f)`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRejectsStrictClosure(t *testing.T) {
	source, destination := oruby.MrbOpen(), oruby.MrbOpen()
	defer source.Close()
	defer destination.Close()
	source.DefineGlobalFunction("copy_strict", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if _, err := migrateRProc(source, destination, mrb.GetArgsBlock()); err == nil {
			t.Error("strict closure transfer succeeded")
		}
		return mrb.NilValue()
	}, oruby.ArgsBlock())
	if _, err := source.Eval(`copy_strict(&lambda { |x| x })`); err != nil {
		t.Fatal(err)
	}
}

func TestMigratedClosureSurvivesSourceClose(t *testing.T) {
	source, destination := oruby.MrbOpen(), oruby.MrbOpen()
	defer source.Close()
	defer destination.Close()
	var copied oruby.RProc
	source.DefineGlobalFunction("copy_closure", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		var err error
		copied, err = migrateRProc(source, destination, mrb.GetArgsBlock())
		if err != nil {
			t.Errorf("migrate closure: %v", err)
			return mrb.NilValue()
		}
		destination.GCRegister(copied)
		return mrb.NilValue()
	}, oruby.ArgsBlock())
	if _, err := source.Eval(`prefix = "captured "; copy_closure { prefix + "result" }`); err != nil {
		t.Fatal(err)
	}
	if copied.IsNil() {
		t.Fatal("closure not copied")
	}
	source.Close()
	runtime.GC()
	destination.FullGC()
	value, err := destination.YieldArgvE(copied)
	if err != nil || value.String() != "captured result" {
		t.Fatalf("copied closure after source close: %v, %v", value, err)
	}
	destination.GCUnregister(copied)
}
