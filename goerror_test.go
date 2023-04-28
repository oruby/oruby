package oruby

import (
	"strings"
	"testing"
)

// TestMrbStackedException ported from mitchellh go-mruby
func TestMrbStackedException(t *testing.T) {
	var testClass RClass

	testFunc := func(mrb *MrbState, self Value) MrbValue {
		args := mrb.GetArgs()

		t, err := testClass.New()
		if err != nil {
			return mrb.EExceptionClass().Raise(err.Error())
		}

		v, err := t.Funcall("dotest!", args.SliceIntf()...)
		if err != nil {
			return mrb.EExceptionClass().Raise(err.Error())
		}

		return v
	}

	doTestFunc := func(mrb *MrbState, self Value) MrbValue {
		err := mrb.EExceptionClass().Raise("Fail us!")
		return err
	}

	mrb := MrbOpen()

	testClass = mrb.DefineClass("TestClass", mrb.ObjectClass())
	testClass.DefineMethod("dotest!", doTestFunc, mrb.ArgsOpt(3))

	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("test", testFunc, ArgsOpt(3))

	_, err := mrb.LoadString("test")
	if err == nil {
		t.Fatal("No exception when one was expected")
		return
	}

	if !strings.Contains(err.Error(), "Fail us!") {
		t.Fatalf("Expected 'Fail us!' got '%v'", err.Error())
		return
	}

	mrb.Close()

	mrb = MrbOpen()
	defer mrb.Close()

	evalFunc := func(mrb *MrbState, self Value) MrbValue {
		arg := mrb.GetArgsBlock()
		return mrb.Yield(arg, mrb.NilValue())
	}

	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("myeval", evalFunc, ArgsBlock())

	result, err := mrb.LoadString("myeval { raise 'foo' }")
	if err == nil {
		t.Fatal("did not error")
		return
	}

	if !strings.HasPrefix(err.Error(), "foo") {
		t.Fatalf("Expected 'foo' got '%v'", err)
		return
	}

	if !result.IsNil() {
		t.Fatal("result was not cleared")
		return
	}
}

func TestInnerRaise(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	result, err := mrb.LoadString("raise 'foo'")
	if err == nil {
		t.Fatal("did not error")
		return
	}

	if !strings.HasPrefix(err.Error(), "foo") {
		t.Fatalf("Expected 'foo' got '%v'", err)
		return
	}

	if !result.IsNil() {
		t.Fatal("result was not cleared")
		return
	}
}

func TestMrbStackedInnerException(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	evalFunc := func(mrb *MrbState, self Value) MrbValue {
		arg := mrb.GetArgsBlock()
		return mrb.Yield(arg, nilValue)
	}

	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("myeval", evalFunc, ArgsBlock())

	result, err := mrb.LoadString("myeval { raise 'foo' }")
	if err == nil {
		t.Fatal("did not error")
		return
	}

	if !strings.HasPrefix(err.Error(), "foo") {
		t.Fatalf("Expected 'foo' got '%v'", err)
		return
	}

	if !result.IsNil() {
		t.Fatal("result was not cleared")
		return
	}
}

func checkRaiserMethod(t *testing.T, mrb *MrbState, f MrbFuncT, aspec MrbAspec) {
	t.Helper()

	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("do_fail", f, aspec)

	result, err := mrb.LoadString("do_fail")
	if err == nil {
		t.Fatal("did not error")
	}

	if !strings.HasPrefix(err.Error(), "Unknown class: NonExistingClass") {
		t.Fatalf("Expected 'Unknown class: NonExistingClass' got '%v'", err)
	}

	if !result.IsNil() {
		t.Fatal("result was not cleared")
		return
	}
}

func TestRaiseFromApi(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	checkRaiserMethod(t, mrb, func(mrb *MrbState, self Value) MrbValue {
		mrb.ClassGet("NonExistingClass")
		return self
	}, mrb.ArgsNone())
}
