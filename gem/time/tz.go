package time

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oruby/oruby"
)

var (
	tzMu    sync.Mutex
	tzCache = map[string]*time.Location{}
)

// localLocation is the zone of ENV["TZ"] now: MRI rereads TZ on each
// conversion, Go's time.Local is fixed when the process starts
func localLocation() *time.Location {
	tz, ok := os.LookupEnv("TZ")
	if !ok {
		return time.Local
	}
	tzMu.Lock()
	defer tzMu.Unlock()
	if l, ok := tzCache[tz]; ok {
		return l
	}
	l, err := time.LoadLocation(tz)
	if err != nil {
		l = posixZone(tz)
	}
	if l == nil || tz == "" {
		l = time.UTC
	}
	tzCache[tz] = l
	return l
}

// posixZone reads a POSIX TZ value of the plain form NAME[+-]hh[:mm[:ss]]
// (no daylight saving rule), which the zoneinfo files Go loads do not
// cover: "AST-3" is UTC+3, the sign being backwards
func posixZone(tz string) *time.Location {
	i := 0
	for i < len(tz) && (tz[i] >= 'A' && tz[i] <= 'Z' || tz[i] >= 'a' && tz[i] <= 'z') {
		i++
	}
	name, rest := tz[:i], tz[i:]
	if len(name) < 3 || rest == "" {
		return nil
	}
	sign := 1
	switch rest[0] {
	case '+':
		rest = rest[1:]
	case '-':
		sign, rest = -1, rest[1:]
	}
	parts := strings.Split(rest, ":")
	if len(parts) > 3 {
		return nil
	}
	secs := 0
	for k, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		secs += n * []int{3600, 60, 1}[k]
	}
	return time.FixedZone(name, -sign*secs)
}

// zone for mode 0: UTC, 1: local, 2: fixed offset off
func zoneFor(mode, off int) *time.Location {
	switch mode {
	case 0:
		return time.UTC
	case 2:
		return time.FixedZone("", off)
	}
	return localLocation()
}

// timeKind returns 0 for UTC, 1 for local and 2 for a fixed offset
func timeKind(t time.Time) int {
	switch l := t.Location(); {
	case l == time.UTC:
		return 0
	case l.String() == "":
		return 2
	}
	return 1
}

func initTZ(mrb *oruby.MrbState, t oruby.RClass) {
	// Time.__make(sec, nsec, mode, off): the instant in a zone
	t.DefineClassMethod("__make", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		tm := time.Unix(int64(a.Item(0).Int()), int64(a.Item(1).Int())).In(zoneFor(a.Item(2).Int(), a.Item(3).Int()))
		return mrb.DataValue(tm)
	}, mrb.ArgsReq(4))
	// Time.__civil(y, mon, d, h, mi, s, nsec, mode, off): wall time in a zone
	t.DefineClassMethod("__civil", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		tm := time.Date(a.Item(0).Int(), time.Month(a.Item(1).Int()), a.Item(2).Int(), a.Item(3).Int(), a.Item(4).Int(),
			a.Item(5).Int(), a.Item(6).Int(), zoneFor(a.Item(7).Int(), a.Item(8).Int()))
		return mrb.DataValue(tm)
	}, mrb.ArgsReq(9))
	// time.__parts => [sec, nsec, kind, utc_offset, zone abbreviation]
	t.DefineMethod("__parts", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		tm := getTime(mrb, self)
		name, off := tm.Zone()
		ary := mrb.AryNew()
		ary.Push(mrb.FixnumValue(int(tm.Unix())))
		ary.Push(mrb.FixnumValue(tm.Nanosecond()))
		ary.Push(mrb.FixnumValue(timeKind(tm)))
		ary.Push(mrb.FixnumValue(off))
		ary.Push(mrb.StrNew(name))
		return ary
	}, mrb.ArgsNone())
	// time.__at(sec, nsec): a Time at that instant in self's own zone
	t.DefineMethod("__at", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		loc := getTime(mrb, self).Location()
		return mrb.DataValue(time.Unix(int64(a.Item(0).Int()), int64(a.Item(1).Int())).In(loc))
	}, mrb.ArgsReq(2))
	// time.__set(other): replace self's instant and zone
	t.DefineMethod("__set", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		setTime(mrb, self, getTime(mrb, mrb.GetArgsFirst()))
		return self
	}, mrb.ArgsReq(1))
}
