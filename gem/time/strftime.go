package time

import (
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	"github.com/oruby/oruby/gem/internal/strftime"
)

// Fields returns strftime fields of t
func Fields(t time.Time) *strftime.Fields {
	y, w := t.ISOWeek()
	name, off := t.Zone()
	return &strftime.Fields{
		Year: int64(t.Year()), Mon: int(t.Month()), Day: t.Day(),
		Hour: t.Hour(), Min: t.Minute(), Sec: t.Second(), Nsec: int64(t.Nanosecond()),
		Wday: int(t.Weekday()), Yday: t.YearDay(),
		Offset: off, Zone: name,
		CWYear: int64(y), CWeek: w, CWday: (int(t.Weekday())+6)%7 + 1,
		Epoch: t.Unix(), EpochMilli: t.UnixMilli(),
		UTC: t.Location() == time.UTC,
	}
}

func timeStrftime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t, err := getTimeCheck(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	format, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	return mrb.StrNew(strftime.Format(format, Fields(t)))
}

func timeUTCOffset(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t, err := getTimeCheck(mrb, self)
	if err != nil {
		return mrb.RaiseError(err)
	}
	_, off := t.Zone()
	return mrb.FixnumValue(off)
}
