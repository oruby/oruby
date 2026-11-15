package oruby

import "testing"

func TestKernelDebugAndHook(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	var captured *DebugState
	var initialX, initialY int
	mrb.SetDebugHook(func(mrb *MrbState, state *DebugState) {
		if state.Reason == "debug" {
			cp := *state
			captured = &cp
			initialX = state.Locals["x"].Int()
			initialY = state.Locals["y"].Int()
			// Modify local variable 'y' while paused at debug breakpoint
			state.SetLocal("y", mrb.FixnumValue(100))
		}
	})

	v, err := mrb.Eval(`
x = 10
y = 20
d = debug(x + y)
x + y + d
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured == nil {
		t.Fatal("expected debug hook to be called")
	}
	if captured.Line != 4 {
		t.Errorf("expected line 4, got %d", captured.Line)
	}
	if len(captured.Args) != 1 || captured.Args[0].Int() != 30 {
		t.Errorf("expected debug arg [30], got %v", captured.Args)
	}
	if initialX != 10 || initialY != 20 {
		t.Errorf("expected locals x=10, y=20, got x=%v, y=%v", initialX, initialY)
	}
	// After SetLocal("y", 100), x + y + d should be 10 + 100 + 30 = 140
	if v.Int() != 140 {
		t.Errorf("expected modified result 140, got %d", v.Int())
	}
}

func TestLineDebugStepping(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	var lines []int
	mrb.SetDebugHook(func(mrb *MrbState, state *DebugState) {
		if state.Reason == "step" {
			lines = append(lines, state.Line)
		}
	})
	mrb.SetLineDebug(true)

	v, err := mrb.Eval(`a = 1
b = 2
a + b
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Int() != 3 {
		t.Errorf("expected 3, got %d", v.Int())
	}
	if len(lines) < 3 {
		t.Errorf("expected at least 3 stepped lines, got %v", lines)
	}
}
