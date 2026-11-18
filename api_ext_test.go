package oruby

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestInterruptEvalContext(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := mrb.EvalContext(ctx, `i = 0; loop { i += 1 }`)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("interrupt took %v", d)
	}
	// the state keeps working, and a late cancel does not stop the next run
	v, err := mrb.EvalContext(context.Background(), `1 + 2`)
	if err != nil || v.Int() != 3 {
		t.Errorf("after interrupt: %v %v", v, err)
	}

	mrb.Interrupt()
	mrb.ClearInterrupt()
	if v, err := mrb.Eval(`[1, 2].sum`); err != nil || v.Int() != 3 {
		t.Errorf("cleared interrupt still fired: %v %v", v, err)
	}
}

func TestCheckFrozenMessage(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	s, _ := mrb.Eval(`"abc".freeze`)
	err := mrb.CheckFrozen(s)
	if err == nil || !strings.Contains(err.Error(), `can't modify frozen String: "abc"`) {
		t.Errorf("CheckFrozen: %v", err)
	}
	if err := mrb.CheckFrozen(mrb.StrNew("x")); err != nil {
		t.Errorf("unfrozen: %v", err)
	}
}

func TestEnsureTypes(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	if _, err := mrb.EnsureStringType(mrb.FixnumValue(1)); err == nil {
		t.Error("EnsureStringType(1) should be a TypeError")
	}
	if s, err := mrb.EnsureStringType(mrb.StrNew("a")); err != nil || s.String() != "a" {
		t.Errorf("EnsureStringType: %v", err)
	}
	if _, err := mrb.EnsureArrayType(mrb.FixnumValue(1)); err == nil {
		t.Error("EnsureArrayType(1) should be a TypeError")
	}
	if _, err := mrb.EnsureHashType(mrb.FixnumValue(1)); err == nil {
		t.Error("EnsureHashType(1) should be a TypeError")
	}
	r, _ := mrb.Eval(`Rational(7, 2)`)
	if v, err := mrb.EnsureIntegerType(r); err != nil || v.Int() != 3 {
		t.Errorf("EnsureIntegerType(7/2): %v %v", v, err)
	}
	big, _ := mrb.Eval(`2**80`)
	if _, err := mrb.EnsureIntType(big); err == nil {
		t.Log("EnsureIntType(2**80) truncates, as mruby does")
	}
	if v, err := mrb.EnsureFloatType(mrb.FixnumValue(2)); err != nil || v.Float64() != 2 {
		t.Errorf("EnsureFloatType(2): %v %v", v, err)
	}
	if _, err := mrb.EnsureFloatType(mrb.StrNew("x")); err == nil {
		t.Error(`EnsureFloatType("x") should be a TypeError`)
	}
}

func TestInt64Conversions(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	if s := mrb.String(mrb.Inspect(mrb.Uint64Value(math.MaxUint64))); s != "18446744073709551615" {
		t.Errorf("Uint64Value: %s", s)
	}
	v, _ := mrb.Eval(`2**64 - 1`)
	if u, err := mrb.AsUint64(v); err != nil || u != math.MaxUint64 {
		t.Errorf("AsUint64: %v %v", u, err)
	}
	if _, err := mrb.AsInt64(v); err == nil {
		t.Error("AsInt64(2**64-1) should be a RangeError")
	}
	if _, err := mrb.AsUint64(mrb.FixnumValue(-1)); err == nil {
		t.Error("AsUint64(-1) should be a RangeError")
	}
	if i, err := mrb.AsInt64(mrb.Int64Value(math.MinInt64)); err != nil || i != math.MinInt64 {
		t.Errorf("AsInt64(min): %v %v", i, err)
	}
}

func TestReflectIntegerRanges(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	k := mrb.KernelModule()
	mrb.DefineModuleFunc(k, "u64", func(x uint64) uint64 { return x })
	mrb.DefineModuleFunc(k, "i64", func(x int64) int64 { return x })
	mrb.DefineModuleFunc(k, "u32", func(x uint32) uint32 { return x })
	mrb.DefineModuleFunc(k, "i8", func(x int8) int8 { return x })
	mrb.DefineModuleFunc(k, "maxu64", func() uint64 { return math.MaxUint64 })
	mrb.DefineModuleFunc(k, "optu64", func(x *uint64) uint64 {
		if x == nil {
			return 7
		}
		return *x
	})
	for code, want := range map[string]string{
		"u64(2**64-1)":               "18446744073709551615",
		"u64(2**63)":                 "9223372036854775808",
		"u64(5)":                     "5",
		"i64(-2**63)":                "-9223372036854775808",
		"u64(1.9)":                   "1",
		"maxu64":                     "18446744073709551615",
		"i8(-128)":                   "-128",
		"optu64(2**64-1)":            "18446744073709551615",
		"u64(-1) rescue $!.class":    "RangeError",
		"i64(2**63) rescue $!.class": "RangeError",
		"u32(2**32) rescue $!.class": "RangeError",
		"u32(-1) rescue $!.class":    "RangeError",
		"i8(128) rescue $!.class":    "RangeError",
		"u64(2**64) rescue $!.class": "RangeError",
	} {
		v, err := mrb.Eval("(" + code + ").inspect")
		if err != nil || mrb.String(v) != want {
			t.Errorf("%s => %v (err %v), want %s", code, mrb.String(v), err, want)
		}
	}

	var u uint64
	big, _ := mrb.Eval(`2**64 - 1`)
	if err := mrb.Scan(big, &u); err != nil || u != math.MaxUint64 {
		t.Errorf("Scan uint64: %v %v", u, err)
	}
	var u8 uint8
	if err := mrb.Scan(mrb.FixnumValue(300), &u8); err == nil {
		t.Errorf("Scan 300 into uint8 should fail, got %v", u8)
	}
	var us []uint64
	ary, _ := mrb.Eval(`[1, 2**64 - 1]`)
	if err := mrb.Scan(ary, &us); err != nil || len(us) != 2 || us[1] != math.MaxUint64 {
		t.Errorf("Scan []uint64: %v %v", us, err)
	}
}

func TestMrbObjValueInts(t *testing.T) {
	for _, x := range []interface{}{int(1), int8(-3), int16(4), int32(7), int64(-5), uint(6), uint8(8), uint16(9), uint32(10), uint64(11)} {
		v, err := MrbObjValue(x)
		if err != nil || !v.IsInteger() {
			t.Errorf("MrbObjValue(%T): %v", x, err)
		}
	}
	if _, err := MrbObjValue(uint64(math.MaxUint64)); err == nil {
		t.Error("MrbObjValue(MaxUint64) should be a RangeError")
	}
}

func TestIVForEachAndClassOuter(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	o, _ := mrb.Eval(`module Outer; class Inner; end; end; o = Object.new; o.instance_variable_set(:@a, 1); o.instance_variable_set(:@b, 2); o`)
	got := map[string]int{}
	mrb.IVForEach(o, func(name MrbSym, val Value) bool {
		got[mrb.SymName(name)] = val.Int()
		return true
	})
	if len(got) != 2 || got["@a"] != 1 || got["@b"] != 2 {
		t.Errorf("IVForEach: %v", got)
	}
	n := 0
	mrb.IVForEach(o, func(MrbSym, Value) bool { n++; return false })
	if n != 1 {
		t.Errorf("IVForEach did not stop: %d", n)
	}

	inner := mrb.ClassGetUnder(mrb.ModuleGet("Outer"), "Inner")
	if outer, ok := mrb.ClassOuter(inner); !ok || outer.Name() != "Outer" {
		t.Errorf("ClassOuter: %v %v", outer, ok)
	}
	if _, ok := mrb.ClassOuter(mrb.ClassGet("String")); ok {
		t.Error("ClassOuter(String) should be top level")
	}
	anon, _ := mrb.Eval(`c = Class.new; c.const_set(:Nested, Class.new); c::Nested`)
	// mruby's own answer: the outer has no name yet
	if _, ok := mrb.ClassOuter(mrb.ClassPtr(anon.Value())); !ok {
		t.Error("ClassOuter(anonymous::Nested) should be the anonymous class")
	}
}

func TestRecursiveMethodP(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	mrb.DefineMethod(mrb.ObjectClass(), "go_walk", func(mrb *MrbState, self Value) MrbValue {
		if mrb.RecursiveMethodP(mrb.Intern("go_walk"), self, Nil) {
			return mrb.StrNew("cycle")
		}
		return mrb.Call(mrb.GetArgsFirst(), "go_walk", self)
	}, ArgsReq(1))
	v, err := mrb.Eval(`a = Object.new; b = Object.new; a.go_walk(b)`)
	if err != nil || mrb.String(v) != "cycle" {
		t.Errorf("RecursiveMethodP: %v %v", mrb.String(v), err)
	}
}

func TestArrayStringHelpers(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	a, _ := mrb.Eval(`[3, 4]`)
	d, err := mrb.AryDup(a)
	if err != nil || d.Len() != 2 || d.Value() == a.Value() {
		t.Errorf("AryDup: %v", err)
	}
	if err := mrb.AryUnshiftValues(a, mrb.FixnumValue(1), mrb.FixnumValue(2)); err != nil {
		t.Fatal(err)
	}
	if s := mrb.String(mrb.Inspect(a)); s != "[1, 2, 3, 4]" {
		t.Errorf("AryUnshiftValues: %s", s)
	}
	f, _ := mrb.Eval(`[1].freeze`)
	if err := mrb.AryUnshiftValues(f, mrb.FixnumValue(0)); err == nil {
		t.Error("unshift into a frozen Array should fail")
	}

	s := mrb.StrNew("héllo")
	fz, err := mrb.StrDupFrozen(s)
	if err != nil || !fz.RBasic().IsFrozen() || fz == s {
		t.Errorf("StrDupFrozen: %v", err)
	}
	sub, err := mrb.StrByteSubseq(s, 1, 2)
	if err != nil || mrb.String(sub) != "é" {
		t.Errorf("StrByteSubseq: %q %v", mrb.String(sub), err)
	}
	if _, err := mrb.StrByteSubseq(s, 4, 10); err == nil {
		t.Error("StrByteSubseq out of range should fail")
	}
}

func TestGCSchedulerDriven(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	if err := mrb.GCSchedulerDriven(true); err != nil {
		t.Fatal(err)
	}
	mrb.Eval(`200.times { "x" * 100 }`)
	steps := 0
	for mrb.GCSchedulerPending() && steps < 100000 {
		mrb.GCStep()
		steps++
	}
	if mrb.GCSchedulerPending() {
		t.Errorf("GC still pending after %d steps", steps)
	}
	if err := mrb.GCSchedulerDriven(false); err != nil {
		t.Fatal(err)
	}
	mrb.GCDisable()
	if err := mrb.GCSchedulerDriven(true); err == nil {
		t.Error("GCSchedulerDriven while GC is disabled should fail")
	}
	mrb.GCEnable()
}

// trace_var (Kernel.__oruby_gv_hook) claims process-wide global slots (go-mrb.c) under coreMu while
// other states do the same
func TestConcurrentTraceVar(t *testing.T) {
	done := make(chan error, 8)
	for g := 0; g < 8; g++ {
		go func(g int) {
			mrb := MrbOpen()
			defer mrb.Close()
			// what trace_var does (core_ext): make the globals virtual
			v, err := mrb.Eval(`
				$oruby_traced_a = :before
				Kernel.__oruby_gv_hook("$oruby_traced_a")
				Kernel.__oruby_gv_hook("$oruby_traced_b")
				kept = $oruby_traced_a == :before
				$oruby_traced_a = 1; $oruby_traced_b = 2
				kept && [$oruby_traced_a, $oruby_traced_b] == [1, 2]`)
			if err == nil && !v.Bool() {
				err = errors.New("hooked globals lost their values")
			}
			done <- err
		}(g)
	}
	for g := 0; g < 8; g++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

// $! belongs to the fiber that is handling the exception
func TestErrinfoPerFiber(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(`
		f = Fiber.new { begin; raise "inner"; rescue; Fiber.yield $!.message; $!.message; end }
		a = f.resume
		root = $!
		b = f.resume
		[a, root, b] == ["inner", nil, "inner"]`)
	if err != nil || !v.Bool() {
		t.Errorf("$! leaked between fibers: %v %v", v, err)
	}
}
