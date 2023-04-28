package time

import (
	"fmt"
	"math"
	"time"

	"github.com/oruby/oruby"
)

func getTime(mrb *oruby.MrbState, self oruby.Value) time.Time {
	switch t := mrb.Data(self).(type) {
	case time.Time:
		return t
	case *time.Time:
		return *t
	}

	panic(fmt.Sprintf("unexpected value %v, for Time value", mrb.Data(self)))
}

func getTimeCheck(mrb *oruby.MrbState, self oruby.Value) (time.Time, error) {
	switch t := mrb.Data(self).(type) {
	case time.Time:
		return t, nil
	case *time.Time:
		return *t, nil
	}

	return time.Time{}, oruby.EArgumentError("invalud value")
}

func setTime(mrb *oruby.MrbState, v oruby.Value, t time.Time) {
	switch d := mrb.Data(v).(type) {
	case time.Time:
		mrb.DataSetInterface(v, t)
	case *time.Time:
		*d = t
	case nil:
		mrb.DataSetInterface(v, t)
	}
}

func timeValue(mrb *oruby.MrbState, self oruby.Value, t time.Time) oruby.Value {
	switch mrb.Data(self).(type) {
	case time.Time:
		return mrb.DataValue(t)
	case *time.Time:
		return mrb.DataValue(&t)
	}

	return mrb.DataValue(t)
}

func timeEq(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	t2, err := getTimeCheck(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.Raisef(mrb.EArgumentError(), "invalid argument %v", t2)
	}

	return oruby.Bool(!t2.IsZero() && t.Equal(t2))
}

func timeCmp(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	v := mrb.GetArgsFirst()
	if v.IsNil() || t.IsZero() {
		return v
	}
	t2, err := getTimeCheck(mrb, v)
	if err != nil || t2.IsZero() {
		return nil
	}

	if t.UnixNano() > t2.UnixNano() {
		return oruby.Integer(1)
	} else if t.UnixNano() < t2.UnixNano() {
		return oruby.Integer(-1)
	}

	return oruby.Integer(0)
}

func timePlus(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	v := mrb.GetArgsFirst()

	if v.IsNil() {
		return mrb.Raise(mrb.ETypeError(), "can't convert nil into an exact number")
	} else if v.IsInteger() {
		return timeValue(mrb, self, t.Add(time.Duration(v.Int())*time.Second))
	} else if v.IsFloat() {
		f := v.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}

		d := int64(f * 10e9)
		return timeValue(mrb, self, t.Add(time.Duration(d)*time.Nanosecond))
	}

	switch t2 := mrb.Data(v).(type) {
	case *time.Time:
		ret := time.Unix(t.Unix()+t2.Unix(), t.UnixNano()+t2.UnixNano())
		return timeValue(mrb, self, ret)
	case time.Time:
		ret := time.Unix(t.Unix()+t2.Unix(), t.UnixNano()+t2.UnixNano())
		return timeValue(mrb, self, ret)
	case time.Duration:
		return timeValue(mrb, self, t.Add(t2))
	case *time.Duration:
		return timeValue(mrb, self, t.Add(*t2))
	}

	return mrb.Raisef(mrb.EArgumentError(), "invalid argument %v", mrb.Data(v))
}

func timeMinus(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	v := mrb.GetArgsFirst()

	if v.IsNil() {
		return mrb.Raise(mrb.ETypeError(), "can't convert nil into an exact number")
	} else if v.IsInteger() {
		return timeValue(mrb, self, t.Add(time.Duration(-v.Int())*time.Second))
	} else if v.IsFloat() {
		f := v.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}

		d := int64(f * 10e9)
		return timeValue(mrb, self, t.Add(time.Duration(-d)*time.Nanosecond))
	}

	switch t2 := mrb.Data(v).(type) {
	case *time.Time:
		ret := time.Unix(t.Unix()-t2.Unix(), t.UnixNano()-t2.UnixNano())
		return timeValue(mrb, self, ret)
	case time.Time:
		ret := time.Unix(t.Unix()-t2.Unix(), t.UnixNano()-t2.UnixNano())
		return timeValue(mrb, self, ret)
	case time.Duration:
		return timeValue(mrb, self, t.Add(-t2))
	case *time.Duration:
		return timeValue(mrb, self, t.Add(-*t2))
	}

	return mrb.Raisef(mrb.EArgumentError(), "invalid argument %v", mrb.Data(v))
}

func timeInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)

	if t.Location() == time.Local {
		return mrb.StrNew(t.Format("2006-01-02 15:04:05 -0700"))
	}
	return mrb.StrNew(t.Format("2006-01-02 15:04:05 MST"))
}

func timeToS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return timeInspect(mrb, self)
}

func timeAsctime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return mrb.StrNew(t.Format("Mon Jan _2 15:04:05 2006"))
}

func timeDay(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return mrb.FixnumValue(t.Day())
}

func timeGetlocal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return timeValue(mrb, self, t.Local())
}

func timeGetutc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return timeValue(mrb, self, t.UTC())
}

func timeHour(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(t.Hour())
}

func timeLocaltime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	setTime(mrb, self, t.Local())
	return self
}

func timeMday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(t.Day())
}

func timeMin(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(t.Minute())
}

func timeMon(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(int(t.Month()))
}

func timeSec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(t.Second())
}

func timeToI(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Int64(t.Unix())
}

func timeToF(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return mrb.FloatValue(float64(t.UnixNano() / 1.e09))
}

func timeUsec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Int64((t.UnixNano() - t.Unix()*1.e09) / 1000)
}

func timeNsec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Int64(t.UnixNano() - t.Unix()*1.e09)
}

func timeUtc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	setTime(mrb, self, t.UTC())
	return self
}

func timeIsUtc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Location() == time.UTC)
}

func timeWday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(int(t.Weekday()))
}

func timeYday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(t.YearDay())
}

func timeYear(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Integer(t.Year())
}

func timeIsSunday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Weekday() == time.Sunday)
}

func timeIsMonday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Weekday() == time.Monday)
}

func timeIsTuesday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Weekday() == time.Tuesday)
}

func timeIsWednesday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Weekday() == time.Wednesday)
}

func timeIsThursday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Weekday() == time.Thursday)
}

func timeIsFriday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Weekday() == time.Friday)
}

func timeIsSaturday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(t.Weekday() == time.Saturday)
}

func timeZone(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	zone, _ := t.Zone()
	return mrb.StrNew(zone)
}

// isTimeDST returns true if time t occurs within daylight saving time
// for its time zone. (via https://stackoverflow.com/a/53052382)
func isDST(t time.Time) bool {
	// If the most recent (within the last year) clock change
	// was forward then assume the change was from std to dst.
	hh, mm, _ := t.UTC().Clock()
	tClock := hh*60 + mm
	for m := -1; m > -12; m-- {
		// assume dst lasts for least one month
		hh, mm, _ := t.AddDate(0, m, 0).UTC().Clock()
		clock := hh*60 + mm
		if clock != tClock {
			if clock > tClock {
				// std to dst
				return true
			}
			// dst to std
			return false
		}
	}
	// assume no dst
	return false
}

func timeIsDst(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	return oruby.Bool(isDST(t))
}

func timeToA(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	zone, _ := t.Zone()

	return mrb.AryNewFromValues(
		oruby.Integer(t.Second()),
		oruby.Integer(t.Minute()),
		oruby.Integer(t.Hour()),
		oruby.Integer(t.Day()),
		oruby.Integer(int(t.Month())),
		oruby.Integer(t.Year()),
		oruby.Integer(int(t.Weekday())),
		oruby.Integer(t.YearDay()),
		oruby.Bool(isDST(t)),
		mrb.StrNew(zone),
	)
}
