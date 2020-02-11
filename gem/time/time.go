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
		t.DefineClassMethod("gm", timeNewUTC, mrb.ArgsArg(1,6))
		t.DefineClassMethod("local", timeLocal, mrb.ArgsArg(1,6))
		t.DefineClassMethod("mktime", timeLocal, mrb.ArgsArg(1,6))
		t.DefineClassMethod("now", timeNow, mrb.ArgsNone())
		t.DefineClassMethod("utc", timeNewUTC, mrb.ArgsArg(1,6))

		t.DefineMethod("initialize", timeInit, mrb.ArgsArg(1,6))
		t.DefineMethod("initialize_copy", timeInitCopy, mrb.ArgsReq(1))
		t.DefineMethod("=="     , timeEq     , mrb.ArgsReq(1))
		t.DefineMethod("<=>"    , timeCmp    , mrb.ArgsReq(1))
		t.DefineMethod("+"      , timePlus   , mrb.ArgsReq(1))
		t.DefineMethod("-"      , timeMinus  , mrb.ArgsReq(1))
		t.DefineMethod("to_s"   , timeToS   , mrb.ArgsNone())
		t.DefineMethod("inspect", timeToS   , mrb.ArgsNone())
		t.DefineMethod("asctime", timeAsctime, mrb.ArgsNone())
		t.DefineMethod("ctime"  , timeAsctime, mrb.ArgsNone())
		t.DefineMethod("day"    , timeDay    , mrb.ArgsNone())
		t.DefineMethod("dst?"   , timeIsDst , mrb.ArgsNone())
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
		t.DefineMethod("nsec", timeNsec, mrb.ArgsNone())
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

		t.DefineMethod("to_a", timeToA, mrb.ArgsNone())

		return nil
	})
}

func timeNow(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return timeValue(mrb, time.Now())
}

func timeAt(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	arg1 := args.ItemDef(0, mrb.FixnumValue(0))
	arg2 := args.ItemDef(1, mrb.FixnumValue(0))

	switch arg1.Type() {
	case oruby.MrbTTFixnum:
		if arg2.IsFloat() && (math.IsNaN(arg2.Float64()) || math.IsInf(arg2.Float64(),0)) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}

		t := time.Unix(arg1.Int64(), int64(arg2.Float64()*1000))
		return timeValue(mrb, t)
	case oruby.MrbTTFloat:
		f := arg1.Float64()
		if math.IsNaN(f) || math.IsInf(f,0) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}
		f2 := f-float64(int64(f))
		t := time.Unix(int64(f),  int64(f2) * 1000)
		return timeValue(mrb, t)
	case oruby.MrbTTData:
		if t, ok := mrb.Data(arg1).(*time.Time) ; ok {
			return timeValue(mrb, *t)
		}
	}
	return mrb.ETypeError().Raisef("can't convert %v into an exact number", mrb.TypeName(arg1))
}

//func timeCreate(mrb *oruby.MrbState, args oruby.RArgs) (time.Time, error) {
//
//}

func timeNewUTC(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if args.Len() == 0 {
		return timeValue(mrb, time.Now().UTC())
	}

	if args.Item(0).IsNil()  {
		return mrb.ETypeError().Raise("no implicit conversion of nil into Integer")
	}

	var y, m, d, hh, mm, ss int
	var ns int64
	var mV, ssV, msV oruby.Value

	if args.Len() == 10 {
		ssV = args.Item(0)
		mm = args.ItemDefInt(1, 0)
		hh = args.ItemDefInt(2, 0)
		d = args.ItemDefInt(3, 1)
		mV = args.ItemDef(4, mrb.FixnumValue(0))
		y = args.ItemDefInt(5, 0)
		msV = mrb.FloatValue(0)

		ss = ssV.Int()
		ns = 0
	} else {
		y = args.ItemDefInt(0, 0)
		mV = args.ItemDef(1, mrb.FixnumValue(0))
		d = args.ItemDefInt(2, 1)
		hh = args.ItemDefInt(3, 0)
		mm = args.ItemDefInt(4, 0)
		ssV = args.ItemDef(5, mrb.FixnumValue(0))
		msV = args.ItemDef(6, mrb.FloatValue(0))

		if ssV.IsFixnum() {
			ss = ssV.Int()
			if msV.IsFixnum() {
				ns = msV.Int64() * 1000
			} else {
				ms := msV.Float64()
				ns = int64(ms*1000) - int64(ms)*1000
			}
		} else if ssV.IsFloat() {
			ss = ssV.Int()
			ns = int64(ssV.Float64()*1000*1000) - ssV.Int64()*1000*1000
		}
	}

	// Month
	if mV.IsString() {
		switch mV.String() {
		case "jan":	m = 1
		case "feb": m = 2
		case "mar": m = 3
		case "apr": m = 4
		case "may": m = 5
		case "jun": m = 6
		case "jul": m = 7
		case "aug": m = 8
		case "sep": m = 9
		case "oct": m = 10
		case "nov": m = 11
		case "dec": m = 12
		default:
			return mrb.ETypeError().Raise("mon out of range")
		}
	} else {
		m = mV.Int()
		if m == 0 {
			m = 1
		} else if m < 0 || m > 12 {
			return mrb.EArgumentError().Raise("month out of range")
		}
	}

	t := time.Date(y, time.Month(m), d, hh, mm, ss, int(ns), time.UTC)
	return timeValue(mrb, t)
}

func timeInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	result := timeLocal(mrb, self)
	mrb.DataSetInterface(self, mrb.Data(result))
	return self
}

func timeInitCopy(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	t := *mrb.Data(arg).(*time.Time)
	mrb.DataSetInterface(self, &t)
	return self
}

func timeLocal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if args.Len() == 0 {
		return timeValue(mrb, time.Now())
	}

	if args.Item(0).IsNil()  {
		return mrb.ETypeError().Raise("no implicit conversion of nil into Integer")
	}

	y := args.ItemDefInt(0, 0)
	m := args.ItemDefInt(1, 0)
	d := args.ItemDefInt(2, 1)
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
	return timeValue(mrb, t)
}
