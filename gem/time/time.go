package time

import (
	"github.com/oruby/oruby"
	"math"
	"time"
)

func init() {
	oruby.Gem("time", func(mrb *oruby.MrbState)interface{} {
		t := mrb.DefineClass("Time", mrb.ObjectClass())
		t.RegisterGoClass(newTime)
		t.Include(mrb.ModuleGet("Comparable"))

		t.DefineClassMethod("at", timeAt, mrb.ArgsArg(1,1))
		t.DefineClassMethod("gm", timeUTC, mrb.ArgsArg(1,6))
		t.DefineClassMethod("local", timeLocal, mrb.ArgsArg(1,6))
		t.DefineClassMethod("mktime", timeLocal, mrb.ArgsArg(1,6))
		t.DefineClassMethod("now", timeNow, mrb.ArgsNone())
		t.DefineClassMethod("utc",  timeUTC, mrb.ArgsArg(1,6))

		t.DefineMethod("initialize", timeInit, mrb.ArgsArg(1,6))
		t.DefineMethod("=="     , timeEq     , mrb.ArgsReq(1))
		t.DefineMethod("<=>"    , timeCmp    , mrb.ArgsReq(1))
		t.DefineMethod("+"      , timePlus   , mrb.ArgsReq(1))
		t.DefineMethod("-"      , timeMinus  , mrb.ArgsReq(1))
		t.DefineMethod("to_s"   , timeToS   , mrb.ArgsNone())
		t.DefineMethod("inspect", timeToS   , mrb.ArgsNone())
		t.DefineMethod("asctime", timeAsctime, mrb.ArgsNone())
		t.DefineMethod("ctime"  , timeAsctime, mrb.ArgsNone())
		t.DefineMethod("day"    , timeDay    , mrb.ArgsNone())
		t.DefineMethod("dst?"   , timeIsDst  , mrb.ArgsNone())
		t.DefineMethod("getgm"  , timeGetutc , mrb.ArgsNone())
		t.DefineMethod("getlocal",timeGetlocal,mrb.ArgsNone())
		t.DefineMethod("getutc" , timeGetutc , mrb.ArgsNone())
		t.DefineMethod("gmt?"   , timeIsUtc  , mrb.ArgsNone())
		t.DefineMethod("gmtime" , timeUtc    , mrb.ArgsNone())
		t.DefineMethod("hour"   , timeHour, mrb.ArgsNone())
		t.DefineMethod("localtime", timeLocaltime, mrb.ArgsNone())
		t.DefineMethod("mday"   , timeMday, mrb.ArgsNone())
		t.DefineMethod("min"    , timeMin, mrb.ArgsNone())

		t.DefineMethod("mon"  , timeMon, mrb.ArgsNone())
		t.DefineMethod("month", timeMon, mrb.ArgsNone())

		t.DefineMethod("sec" , timeSec, mrb.ArgsNone())
		t.DefineMethod("to_i", timeToI, mrb.ArgsNone())
		t.DefineMethod("to_f", timeToF, mrb.ArgsNone())
		t.DefineMethod("usec", timeUsec, mrb.ArgsNone())
		t.DefineMethod("utc" , timeUtc, mrb.ArgsNone())
		t.DefineMethod("utc?", timeIsUtc,mrb.ArgsNone())
		t.DefineMethod("wday", timeWday, mrb.ArgsNone())
		t.DefineMethod("yday", timeYday, mrb.ArgsNone())
		t.DefineMethod("year", timeYear, mrb.ArgsNone())
		t.DefineMethod("zone", timeZone, mrb.ArgsNone())

		t.DefineMethod("sunday?", timeIsSunday, mrb.ArgsNone())
		t.DefineMethod("monday?", timeIsMonday, mrb.ArgsNone())
		t.DefineMethod("tuesday?", timeIsTuesday, mrb.ArgsNone())
		t.DefineMethod("wednesday?", timeIsWednesday, mrb.ArgsNone())
		t.DefineMethod("thursday?", timeIsThursday, mrb.ArgsNone())
		t.DefineMethod("friday?", timeIsFriday, mrb.ArgsNone())
		t.DefineMethod("saturday?", timeIsSaturday, mrb.ArgsNone())

		return nil
	})
}

func timeNow(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := time.Now()
	return mrb.DataValue(&t)
}

func timeAt(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs(0,0)
	arg1 := args.Item(0)
	arg2 := args.Item(1)

	if arg1.IsFloat() {
		f := arg1.Float64()
		if math.IsNaN(f) || math.IsInf(f,0) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}
	}

	if arg2.IsFloat() {
		f := arg2.Float64()
		if math.IsNaN(f) || math.IsInf(f,0) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}
	}

	t := time.Unix(arg1.Int64(), arg2.Int64())
	return mrb.DataValue(&t)
}

func timeUTC(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if args.Len() == 0 {
		t := time.Now().UTC()
		return mrb.DataValue(&t)
	}

	if args.Item(0).IsNil()  {
		return mrb.ETypeError().Raise("no implicit conversion of nil into Integer")
	}

	y := args.ItemDefInt(0, 0)
	m := args.ItemDefInt(1, 0)
	d := args.ItemDefInt(2, 0)
	hh := args.ItemDefInt(3, 0)
	mm := args.ItemDefInt(4, 0)
	ss := args.ItemDefInt(5, 0)
	ms := args.ItemDefInt(6, 0)

	if m == 0 {
		m = 1
	} else if m < 0 || m > 12 {
		return mrb.EArgumentError().Raise("month out of range")
	}

	t := time.Date(y,time.Month(m), d, hh, mm, ss, ms * 1000, time.UTC)
	return mrb.DataValue(t)
}

func timeInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	result := timeLocal(mrb, self)
	mrb.DataSetInterface(self, mrb.Data(result))
	return self
}

func timeLocal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if args.Len() == 0 {
		t := time.Now().UTC()
		return mrb.DataValue(&t)
	}

	if args.Item(0).IsNil()  {
		return mrb.ETypeError().Raise("no implicit conversion of nil into Integer")
	}

	y := args.ItemDefInt(0, 0)
	m := args.ItemDefInt(1, 0)
	d := args.ItemDefInt(2, 0)
	hh := args.ItemDefInt(3, 0)
	mm := args.ItemDefInt(4, 0)
	ss := args.ItemDefInt(5, 0)
	ms := args.ItemDefInt(6, 0)

	if m == 0 {
		m = 1
	} else if m < 0 || m > 12 {
		return mrb.EArgumentError().Raise("month out of range")
	}

	t := time.Date(y,time.Month(m), d, hh, mm, ss, ms * 1000, time.Local)
	return mrb.DataValue(t)
}
