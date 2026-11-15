package oruby

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
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
		t.Error("No exception when one was expected")
		return
	}

	if !strings.Contains(err.Error(), "Fail us!") {
		t.Errorf("Expected 'Fail us!' got '%v'", err.Error())
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
		t.Error("did not error")
		return
	}

	if !strings.HasPrefix(err.Error(), "foo") {
		t.Errorf("Expected 'foo' got '%v'", err)
		return
	}

	if !result.IsNil() {
		t.Error("result was not cleared")
		return
	}
}

func TestInnerRaise(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	result, err := mrb.LoadString("raise 'foo'")
	if err == nil {
		t.Error("did not error")
		return
	}

	if !strings.HasPrefix(err.Error(), "foo") {
		t.Errorf("Expected 'foo' got '%v'", err)
		return
	}

	if !result.IsNil() {
		t.Error("result was not cleared")
		return
	}
}

func TestFuncallReturnsExceptionObjectWithoutError(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	exc := mrb.ExcNew(mrb.EArgumentError(), "ordinary value")
	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("exception_value", func(mrb *MrbState, self Value) MrbValue {
		return exc
	}, ArgsNone())

	got, err := mrb.Funcall(mrb.TopSelf(), mrb.Intern("exception_value"))
	if err != nil {
		t.Fatalf("normal exception-object return reported an error: %v", err)
	}
	if got.Pointer() != exc.Pointer() {
		t.Fatalf("returned exception object changed: got %#x, want %#x", got.Pointer(), exc.Pointer())
	}
	if mrb.Exc() != nil {
		t.Fatalf("normal exception-object return left a pending exception: %v", mrb.Exc())
	}
}

func TestLoadReturnsExceptionObjectWithoutError(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()
	value, err := mrb.Eval(`ArgumentError.new("ordinary value")`)
	if err != nil || value.Type() != MrbTTException || mrb.Exc() != nil {
		t.Fatalf("normal exception-object return: value %v, error %v", value.Type(), err)
	}
}

func TestFuncallGoProc(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()
	proc := mrb.Value(func(s string) string { return "Prefix-" + s })
	got, err := mrb.Funcall(proc, mrb.Intern("call"), "X")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "Prefix-X" {
		t.Fatalf("got %q", got.String())
	}
}

func TestRubyRescueEnsureAfterProtectedYield(t *testing.T) {
	if protectedSubprocess(t) {
		return
	}
	runRubyRescueEnsureAfterProtectedYield(t)
}

func protectedSubprocess(t *testing.T) bool {
	t.Helper()
	const key = "ORUBY_PROTECTED_SUBPROCESS"
	if os.Getenv(key) == t.Name() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), key+"="+t.Name())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("protected subprocess failed: %v\n%s", err, output)
	}
	return true
}

func runRubyRescueEnsureAfterProtectedYield(t *testing.T) {
	t.Helper()
	mrb := MrbOpen()
	defer mrb.Close()

	goDeferred := false
	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("go_yield", func(mrb *MrbState, self Value) MrbValue {
		defer func() { goDeferred = true }()
		return mrb.YieldArgv(mrb.GetArgsBlock())
	}, ArgsBlock())
	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("go_exc_raise", func(mrb *MrbState, self Value) MrbValue {
		exception := mrb.ExcNew(mrb.EArgumentError(), "from ExcRaise")
		mrb.ExcRaise(exception)
		return exception
	}, ArgsNone())
	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("go_f_raise", func(mrb *MrbState, self Value) MrbValue {
		return mrb.FRaise(self)
	}, ArgsReq(1))
	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("go_inner", func(mrb *MrbState, self Value) MrbValue {
		defer func() { goDeferred = true }()
		value, err := mrb.Funcall(self, mrb.Intern("ruby_inner"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return value
	}, ArgsNone())

	result, err := mrb.LoadString(`
		begin
		  go_yield { raise "from block" }
		rescue RuntimeError => exception
		  $rescued_message = exception.message
		ensure
		  $ensured = true
		end
		begin
		  go_exc_raise
		rescue ArgumentError => exception
		  $exc_raise_message = exception.message
		end
		begin
		  go_f_raise("from FRaise")
		rescue RuntimeError => exception
		  $f_raise_message = exception.message
		end
		$events = []
		$original = ArgumentError.new("nested")
		def ruby_inner
		  begin
		    raise $original
		  ensure
		    $events << "inner ensure"
		  end
		end
		begin
		  go_inner
		rescue ArgumentError => exception
		  $events << (exception.equal?($original) ? "same exception" : "wrong exception")
		ensure
		  $events << "outer ensure"
		end
		[$rescued_message, $ensured, $exc_raise_message, $f_raise_message, $events.join(",")]
	`)
	if err != nil {
		t.Fatalf("Ruby rescue did not handle the protected yield error: %v", err)
	}
	if !goDeferred {
		t.Fatal("Go defer did not run before the callback returned")
	}
	if mrb.AryEntry(result, 0).String() != "from block" || !mrb.AryEntry(result, 1).Bool() ||
		mrb.AryEntry(result, 2).String() != "from ExcRaise" || mrb.AryEntry(result, 3).String() != "from FRaise" ||
		mrb.AryEntry(result, 4).String() != "inner ensure,same exception,outer ensure" {
		t.Fatalf("unexpected rescue/ensure result: %v", mrb.String(result))
	}
	if mrb.Exc() != nil {
		t.Fatalf("handled Ruby exception remained pending: %v", mrb.Exc())
	}
}

func TestRubyBreakAfterProtectedYield(t *testing.T) {
	if protectedSubprocess(t) {
		return
	}
	mrb := MrbOpen()
	defer mrb.Close()

	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("go_yield", func(mrb *MrbState, self Value) MrbValue {
		return mrb.YieldArgv(mrb.GetArgsBlock())
	}, ArgsBlock())

	result, err := mrb.Eval(`go_yield { break 42 }`)
	if err != nil {
		t.Fatalf("break did not propagate through protected yield: %v", err)
	}
	if result.Int() != 42 {
		t.Fatalf("break value = %v, want 42", result.Int())
	}
}

func TestIgnoredCheckedYieldStillPropagates(t *testing.T) {
	if protectedSubprocess(t) {
		return
	}
	mrb := MrbOpen()
	defer mrb.Close()
	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("ignored_yield", func(mrb *MrbState, self Value) MrbValue {
		mrb.YieldArgvE(mrb.GetArgsBlock())
		return mrb.FixnumValue(7)
	}, ArgsBlock())
	result, err := mrb.Eval(`begin; ignored_yield { raise "ignored" }; rescue => e; e.message; end`)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.String(); got != "ignored" {
		t.Fatalf("got %q, want ignored", got)
	}
}

func TestCheckedYieldBreakPropagation(t *testing.T) {
	if protectedSubprocess(t) {
		return
	}
	mrb := MrbOpen()
	defer mrb.Close()
	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("checked_yield", func(mrb *MrbState, self Value) MrbValue {
		v, err := mrb.YieldArgvE(mrb.GetArgsBlock())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return v
	}, ArgsBlock())
	result, err := mrb.Eval(`checked_yield { break 42 }`)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Int(); got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
}

func TestProtectedCallRestoresPendingException(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()
	old := mrb.Raise(mrb.EArgumentError(), "pending")
	value, err := mrb.LoadString(`40 + 2`)
	if err != nil || value.Int() != 42 {
		t.Fatalf("nested successful call: value %v, error %v", value, err)
	}
	if mrb.Exc() == nil || mrb.Exc().Value().Pointer() != old.Pointer() {
		t.Fatal("nested call did not restore the pending exception")
	}
	mrb.ExcClear()
}

func TestRubyErrorLifetime(t *testing.T) {
	mrb := MrbOpen()
	other := MrbOpen()
	defer other.Close()
	ai := mrb.GCArenaSave()
	_, err := mrb.Eval(`def fail_here; raise ArgumentError, "boom"; end; fail_here`)
	var rubyErr *RubyError
	if !errors.As(err, &rubyErr) {
		t.Fatalf("got %T, want *RubyError: %v", err, err)
	}
	if rubyErr.Class != "ArgumentError" || rubyErr.Message != "boom" || len(rubyErr.Backtrace) == 0 {
		t.Fatalf("unexpected snapshot: %#v", rubyErr)
	}
	mrb.ExcClear()
	mrb.GCArenaRestore(ai)
	mrb.FullGC()
	value, err := rubyErr.Exception(mrb)
	if err != nil || value.Type() != MrbTTException {
		t.Fatalf("rooted exception: %v, %v", value, err)
	}
	if _, err := rubyErr.Exception(other); err == nil {
		t.Fatal("cross-state exception access succeeded")
	}
	if raised := other.RaiseError(rubyErr); other.ClassOf(raised).Name() != "TypeError" {
		t.Fatal("cross-state re-raise did not fail safely")
	}
	other.ExcClear()
	rubyErr.Release()
	rubyErr.Release()
	if _, err := rubyErr.Exception(mrb); err == nil {
		t.Fatal("released exception remains usable")
	}
	mrb.Close()
	if rubyErr.Class != "ArgumentError" || rubyErr.Message != "boom" || rubyErr.Error() == "" {
		t.Fatal("snapshot changed after release and close")
	}
}

func TestRubyErrorInvalidatedByClose(t *testing.T) {
	mrb := MrbOpen()
	_, err := mrb.Eval(`raise "closed"`)
	var rubyErr *RubyError
	if !errors.As(err, &rubyErr) {
		t.Fatalf("got %T, want *RubyError", err)
	}
	mrb.Close()
	if _, err := rubyErr.Exception(mrb); err == nil {
		t.Fatal("exception remains usable after state close")
	}
	rubyErr.Release()
}

func TestAotBoundaryErrorIsRetained(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()
	mrb.Raise(mrb.EArgumentError(), "compiled")
	err := mrb.AotBoundaryErr()
	var rubyErr *RubyError
	if !errors.As(err, &rubyErr) {
		t.Fatalf("got %T, want *RubyError", err)
	}
	defer rubyErr.Release()
	mrb.ExcClear()
	mrb.FullGC()
	if _, err := rubyErr.Exception(mrb); err != nil {
		t.Fatal(err)
	}
}

func TestRubyErrorFormattingFailureKeepsOriginal(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
		class FormattingFailure < StandardError
		  def message; raise "format failed"; end
		  def backtrace; raise "trace failed"; end
		end
		raise FormattingFailure, "original"
	`)
	var rubyErr *RubyError
	if !errors.As(err, &rubyErr) {
		t.Fatalf("got %T, want *RubyError: %v", err, err)
	}
	defer rubyErr.Release()
	if rubyErr.Class != "FormattingFailure" || rubyErr.Message != "(message unavailable)" {
		t.Fatalf("wrong snapshot: %s %s", rubyErr.Class, rubyErr.Message)
	}
	if mrb.Exc() == nil || mrb.ClassOf(mrb.Exc().Value()).Name() != "FormattingFailure" {
		t.Fatal("formatting failure replaced the original pending exception")
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
		t.Error("did not error")
		return
	}

	if !strings.HasPrefix(err.Error(), "foo") {
		t.Errorf("Expected 'foo' got '%v'", err)
		return
	}

	if !result.IsNil() {
		t.Error("result was not cleared")
		return
	}
}

func checkRaiserMethod(t *testing.T, mrb *MrbState, f MrbFuncT, aspec MrbAspec) {
	t.Helper()

	mrb.SingletonClass(mrb.TopSelf()).DefineMethod("do_fail", f, aspec)

	result, err := mrb.LoadString("do_fail")
	if err == nil {
		t.Error("did not error")
		return
	}

	if !strings.HasPrefix(err.Error(), "Unknown class: NonExistingClass") {
		t.Errorf("Expected 'Unknown class: NonExistingClass' got '%v'", err)
		return
	}

	if !result.IsNil() {
		t.Error("result was not cleared")
		return
	}
}

// TestRaiseFromApi ClassGet("NonExistingClass") panics on Go side. Panic() can be
// intercepted via mrb.Try() helper - it will intercept Go panics/exceptions and
// converted result to ORuby MrbValue which contains RException MrbValue
// That exception is re-raised on C side, so Go stack is preserved and error can be handled
func TestRaiseFromApi(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	checkRaiserMethod(t, mrb, func(mrb *MrbState, self Value) MrbValue {
		return mrb.Try(func() MrbValue {
			return mrb.ClassGet("NonExistingClass")
		})
	}, mrb.ArgsNone())
}
