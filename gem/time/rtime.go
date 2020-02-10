package time

import (
	"github.com/oruby/oruby"
	"math"
	"time"
)

func zero(t *int) int {
	if t == nil {
		return 0
	}
	return *t
}

func timeValue(mrb *oruby.MrbState, t time.Time) oruby.Value {
	ret := &t
	return mrb.DataValue(ret)
}

func newTime(year, month, day, hour, min, sec, usec *int) (*time.Time, error) {
	var t time.Time

	if year == nil && month == nil && day == nil && hour == nil &&
		 min == nil && sec == nil && usec == nil {
		t = time.Now()
	} else {
		if year == nil  {
			return nil, oruby.ETypeError("no implicit conversion of nil into Integer")
		}

		y := zero(year)
		m := zero(month)
		d := zero(day)
		hh := zero(hour)
		mm := zero(min)
		ss := zero(sec)
		ms := zero(usec)

		if m == 0 {
			m = 1
		} else if m < 0 || m > 12 {
			return nil, oruby.EArgumentError("month out of range")
		}

		t = time.Date(y,time.Month(m), d, hh, mm, ss, ms * 1000, time.Local)
	}

	return &t, nil
}

func timeEq(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	t2, ok := mrb.Data(mrb.GetArgsFirst()).(*time.Time)
	if !ok {
		return mrb.Raisef(mrb.EArgumentError(), "invalid argument %v", t2)
	}

	return oruby.Bool(t2 != nil && t.Equal(*t2))
}

func timeCmp(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	v := mrb.GetArgsFirst()
	if v.IsNil() || t.IsZero() {
		return v
	}
	t2, ok := mrb.Data(v).(*time.Time)
	if !ok || t2 == nil || t2.IsZero() {
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
	t := mrb.Data(self).(*time.Time)
	v := mrb.GetArgsFirst()

	if v.IsNil() {
		return mrb.Raise(mrb.ETypeError(),"can't convert nil into an exact number")
	} else if v.IsFixnum() {
		return timeValue(mrb, t.Add(time.Duration(v.Int())*time.Second))
	}

	switch t2 := mrb.Data(v).(type) {
	case float64, float32:
		f :=  t2.(float64)
		if math.IsNaN(f) || math.IsInf(f,0) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}
		ret := time.Unix(t.Unix()+int64(f), t.UnixNano())
		return timeValue(mrb, ret)
	case int, int64, uint, uint64, int32, uint32, uint16, int16, uint8, int8:
		ret := time.Unix(t.Unix()+t2.(int64), t.UnixNano())
		return timeValue(mrb, ret)
	case *time.Time:
		ret := time.Unix(t.Unix()+t2.Unix(), t.UnixNano()+t2.UnixNano())
		return timeValue(mrb, ret)
	case time.Time:
		ret := time.Unix(t.Unix()+t2.Unix(), t.UnixNano()+t2.UnixNano())
		return timeValue(mrb, ret)
	case time.Duration:
		return timeValue(mrb, t.Add(t2))
	case *time.Duration:
		return timeValue(mrb, t.Add(*t2))
	}

	return mrb.Raisef(mrb.EArgumentError(),"invalid argument %v", mrb.Data(v))
}

func timeMinus(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	v := mrb.GetArgsFirst()

	if v.IsNil() {
		return mrb.Raise(mrb.ETypeError(),"can't convert nil into an exact number")
	} else if v.IsFixnum() {
		return mrb.DataValue(t.Add(time.Duration(-v.Int())*time.Second))
	}

	switch t2 := mrb.Data(v).(type) {
	case float64, float32:
		f :=  t2.(float64)
		if math.IsNaN(f) || math.IsInf(f,0) {
			return mrb.Raise(mrb.EFloatDomainError(), "value out of range")
		}
		ret := time.Unix(t.Unix()-int64(f), t.UnixNano())
		return timeValue(mrb, ret)
	case int, int64, uint, uint64, int32, uint32, uint16, int16, uint8, int8:
		ret := time.Unix(t.Unix()-t2.(int64), t.UnixNano())
		return timeValue(mrb, ret)
	case *time.Time:
		ret := time.Unix(t.Unix()-t2.Unix(), t.UnixNano()-t2.UnixNano())
		return timeValue(mrb, ret)
	case time.Time:
		ret := time.Unix(t.Unix()-t2.Unix(), t.UnixNano()-t2.UnixNano())
		return timeValue(mrb, ret)
	case time.Duration:
		return timeValue(mrb, t.Add(-t2))
	case *time.Duration:
		return timeValue(mrb, t.Add(-*t2))
	}

	return mrb.Raisef(mrb.EArgumentError(),"invalid argument %v", mrb.Data(v))
}

func timeInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)

	if t.Location() == time.Local {
		return mrb.StrNewStatic(t.Format("2006-01-02 15:04:05 -0700"))
	}
	return mrb.StrNewStatic(t.Format("2006-01-02 15:04:05 MST"))
}

func timeToS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return timeInspect(mrb, self)
}
func timeAsctime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return mrb.StrNewStatic(t.Format("Mon Jan _2 15:04:05 2006"))
}
func timeDay(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return mrb.FixnumValue(t.Day())
}
func timeGetlocal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return timeValue(mrb, t.Local())
}
func timeGetutc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return timeValue(mrb, t.UTC())
}
func timeHour(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(t.Hour())
}
func timeLocaltime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return timeValue(mrb, t.Local())
}
func timeMday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(t.Day())
}
func timeMin(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(t.Minute())
}
func timeMon(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(int(t.Month()))
}
func timeSec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(t.Second())
}
func timeToI(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Int64(t.Unix())
}
func timeToF(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return mrb.FloatValue((float64(t.UnixNano()) / 1.e09))
}
func timeUsec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Int64(t.Unix() - t.UnixNano() / 1000)
}
func timeUtc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return timeValue(mrb, t.UTC())
}
func timeIsUtc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Location() == time.UTC)
}
func timeWday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(int(t.Weekday()))
}
func timeYday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(t.YearDay())
}
func timeYear(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Integer(t.Year())
}

func timeIsSunday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Weekday() == time.Sunday)
}

func timeIsMonday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Weekday() == time.Monday)
}

func timeIsTuesday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Weekday() == time.Tuesday)
}

func timeIsWednesday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Weekday() == time.Wednesday)
}

func timeIsThursday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Weekday() == time.Thursday)
}

func timeIsFriday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Weekday() == time.Friday)
}

func timeIsSaturday(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	return oruby.Bool(t.Weekday() == time.Saturday)
}

func timeZone(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)
	zone, _ := t.Zone()
	return mrb.StrNewStatic(zone)
}

func timeIsDst(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.Data(self).(*time.Time)

	_, timeOffset := t.Zone()
	loc := t.Location()

	// Offsets at loc location on 1st of jan and 1st of july
	_, winterOffset := time.Date(t.Year(), 1, 1, 0, 0, 0, 0, loc).Zone()
	_, summerOffset := time.Date(t.Year(), 7, 1, 0, 0, 0, 0, loc).Zone()

	// On south pole locations
	if winterOffset > summerOffset {
		return oruby.Bool(timeOffset == summerOffset)
	}

	// On north pole locations
	return oruby.Bool(timeOffset == winterOffset)
}
