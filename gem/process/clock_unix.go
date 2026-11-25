//go:build darwin || linux || freebsd

package process

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	"golang.org/x/sys/unix"
)

func initClock(mrb *oruby.MrbState, mProc oruby.RClass) {
	mProc.Const("CLOCK_REALTIME", unix.CLOCK_REALTIME)
	mProc.Const("CLOCK_MONOTONIC", unix.CLOCK_MONOTONIC)
	mProc.Const("CLOCK_PROCESS_CPUTIME_ID", unix.CLOCK_PROCESS_CPUTIME_ID)
	mProc.Const("CLOCK_THREAD_CPUTIME_ID", unix.CLOCK_THREAD_CPUTIME_ID)
	mProc.Const("CLOCK_MONOTONIC_RAW", unix.CLOCK_MONOTONIC_RAW)
	initClockOS(mProc)
	mProc.DefineModuleFunction("clock_gettime", clockGettime, mrb.ArgsArg(1, 1))
	mProc.DefineModuleFunction("clock_getres", clockGetres, mrb.ArgsArg(1, 1))
}

var clockSymbols = map[string]int32{
	"GETTIMEOFDAY_BASED_CLOCK_REALTIME":        unix.CLOCK_REALTIME,
	"TIME_BASED_CLOCK_REALTIME":                unix.CLOCK_REALTIME,
	"TIMES_BASED_CLOCK_PROCESS_CPUTIME_ID":     unix.CLOCK_PROCESS_CPUTIME_ID,
	"GETRUSAGE_BASED_CLOCK_PROCESS_CPUTIME_ID": unix.CLOCK_PROCESS_CPUTIME_ID,
	"CLOCK_BASED_CLOCK_PROCESS_CPUTIME_ID":     unix.CLOCK_PROCESS_CPUTIME_ID,
	"MACH_ABSOLUTE_TIME_BASED_CLOCK_MONOTONIC": unix.CLOCK_MONOTONIC,
	"TIMES_BASED_CLOCK_MONOTONIC":              unix.CLOCK_MONOTONIC,
}

func clockID(mrb *oruby.MrbState, v oruby.Value) (int32, oruby.MrbValue) {
	switch v.Type() {
	case oruby.MrbTTInteger:
		return int32(v.Int64()), nil
	case oruby.MrbTTSymbol:
		if id, ok := clockSymbols[mrb.SymString(v.Symbol())]; ok {
			return id, nil
		}
	}
	return 0, mrb.Raise(rb.ClassPath(mrb, "Errno::EINVAL"), "Invalid argument - clock_gettime")
}

func clockValue(mrb *oruby.MrbState, ns int64, unit oruby.Value) oruby.MrbValue {
	name := "float_second"
	if !unit.IsNil() {
		if unit.Type() != oruby.MrbTTSymbol {
			return mrb.EArgumentError().Raisef("unexpected unit: %v", mrb.Inspect(unit))
		}
		name = mrb.SymString(unit.Symbol())
	}
	switch name {
	case "float_second":
		return mrb.FloatValue(float64(ns) / 1e9)
	case "float_millisecond":
		return mrb.FloatValue(float64(ns) / 1e6)
	case "float_microsecond":
		return mrb.FloatValue(float64(ns) / 1e3)
	case "second":
		return mrb.FixnumValue(int(floorDiv(ns, 1e9)))
	case "millisecond":
		return mrb.FixnumValue(int(floorDiv(ns, 1e6)))
	case "microsecond":
		return mrb.FixnumValue(int(floorDiv(ns, 1e3)))
	case "nanosecond":
		return mrb.FixnumValue(int(ns))
	}
	return mrb.EArgumentError().Raisef("unexpected unit: %v", name)
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func clockGettime(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	id, exc := clockID(mrb, a[0])
	if exc != nil {
		return exc
	}
	var ts unix.Timespec
	if err := unix.ClockGettime(id, &ts); err != nil {
		return mrb.RaiseError(err)
	}
	unit := oruby.Nil
	if len(a) > 1 {
		unit = a[1]
	}
	return clockValue(mrb, ts.Nano(), unit)
}

// clockGetres reports the clock resolution
func clockGetres(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	if _, exc := clockID(mrb, a[0]); exc != nil {
		return exc
	}
	res := int64(1000)
	if a[0].Type() == oruby.MrbTTSymbol && mrb.SymString(a[0].Symbol()) == "TIME_BASED_CLOCK_REALTIME" {
		res = 1_000_000_000
	}
	unit := oruby.Nil
	if len(a) > 1 {
		unit = a[1]
	}
	return clockValue(mrb, res, unit)
}
