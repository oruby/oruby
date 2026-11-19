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
	// Time.__now: Time.now in the local zone, as __make(sec, nsec, 1, 0)
	// builds it (Round(0) drops the monotonic reading __make's times lack)
	t.DefineClassMethod("__now", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.DataValue(time.Now().Round(0).In(localLocation()))
	}, mrb.ArgsNone())
	// time.__fmt(kind, digits): to_s (0), inspect (1) or xmlschema(digits)
	// (2), US-ASCII, for a year in 0..9999 and a whole-minute offset; nil
	// for the others, which 26_time.rb formats (as it does a timezone
	// object's or a Rational offset's Time)
	t.DefineMethod("__fmt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		kind, digits := a.Item(0).Int(), a.Item(1).Int()
		tm := getTime(mrb, self)
		_, off := tm.Zone()
		if y := tm.Year(); y < 0 || y > 9999 || off%60 != 0 || digits < 0 || digits > 9 {
			return mrb.NilValue()
		}
		b := make([]byte, 0, 48)
		if kind == 2 {
			b = tm.AppendFormat(b, "2006-01-02T15:04:05")
		} else {
			b = tm.AppendFormat(b, "2006-01-02 15:04:05")
		}
		ns := tm.Nanosecond()
		frac := strconv.AppendInt(make([]byte, 0, 10), int64(ns)+1e9, 10)[1:] // 9 digits
		switch {
		case kind == 1 && ns != 0:
			n := len(frac)
			for frac[n-1] == '0' {
				n--
			}
			b = append(append(b, '.'), frac[:n]...)
		case kind == 2 && digits > 0:
			b = append(append(b, '.'), frac[:digits]...)
		}
		switch {
		case timeKind(tm) == 0 && kind == 2:
			b = append(b, 'Z')
		case timeKind(tm) == 0:
			b = append(b, " UTC"...)
		default:
			if kind != 2 {
				b = append(b, ' ')
			}
			sign := byte('+')
			if off < 0 {
				sign, off = '-', -off
			}
			h, m := off/3600, off/60%60
			b = append(b, sign, byte('0'+h/10), byte('0'+h%10))
			if kind == 2 {
				b = append(b, ':')
			}
			b = append(b, byte('0'+m/10), byte('0'+m%10))
		}
		return mrb.StrNewUSASCII(string(b))
	}, mrb.ArgsReq(2))
	// Time.__civil(y, mon, d, h, mi, s, nsec, mode, off[, isdst]): wall time in a zone
	t.DefineClassMethod("__civil", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		y, mon, d := a.Item(0).Int(), time.Month(a.Item(1).Int()), a.Item(2).Int()
		h, mi, s, nsec := a.Item(3).Int(), a.Item(4).Int(), a.Item(5).Int(), a.Item(6).Int()
		mode, off := a.Item(7).Int(), a.Item(8).Int()
		loc := zoneFor(mode, off)
		tm := time.Date(y, mon, d, h, mi, s, nsec, loc)
		if a.Len() >= 10 && !a.Item(9).IsNil() && mode == 1 {
			wantDST := a.Item(9).Bool()
			if tm.IsDST() != wantDST {
				shift := time.Hour
				if wantDST {
					shift = -time.Hour
				}
				alt := tm.Add(shift)
				if alt.Year() == y && alt.Month() == mon && alt.Day() == d && alt.Hour() == h && alt.Minute() == mi && alt.IsDST() == wantDST {
					tm = alt
				}
			}
		}
		return mrb.DataValue(tm)
	}, mrb.ArgsArg(9, 1))
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
