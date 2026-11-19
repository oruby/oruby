package time

import (
	"math/big"
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

// civilFixnums reads n civil fields (year, mon, day, hour, min, sec, usec)
// from a, starting at from: Fixnums, or nil after the year for the default.
// It is false when one is anything else or out of MRI's ranges.
func civilFixnums(a oruby.RArgs, from, n, mode int) (time.Time, bool) {
	f := [7]int64{0, 1, 1, 0, 0, 0, 0}
	for i := 0; i < n && from+i < a.Len(); i++ {
		v := a.Item(from + i)
		switch {
		case v.Type() == oruby.MrbTTFixnum:
			f[i] = v.Int64()
		case i > 0 && v.IsNil():
		default:
			return time.Time{}, false
		}
	}
	y, mon, d, h, mi, s, us := f[0], f[1], f[2], f[3], f[4], f[5], f[6]
	if y < -1_000_000_000 || y > 1_000_000_000 || mon < 1 || mon > 12 || d < 1 || d > 31 ||
		h < 0 || h > 24 || mi < 0 || mi > 59 || s < 0 || s > 60 || (h == 24 && (mi != 0 || s != 0)) ||
		us < 0 || us >= 1_000_000 {
		return time.Time{}, false
	}
	loc := time.UTC
	if mode == 1 {
		loc = localLocation()
	}
	return time.Date(int(y), time.Month(mon), int(d), int(h), int(mi), int(s), int(us)*1000, loc), true
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
	// Fast paths for 26_time.rb, on the Go value alone: a timezone object,
	// a Rational offset or a sub-nanosecond fraction (ivars) is its own.

	// time.__sec_i, __ns, __kind: whole seconds, nanoseconds, zone kind
	t.DefineMethod("__sec_i", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return oruby.Int64(getTime(mrb, self).Unix())
	}, mrb.ArgsNone())
	t.DefineMethod("__ns", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return oruby.Int64(int64(getTime(mrb, self).Nanosecond()))
	}, mrb.ArgsNone())
	t.DefineMethod("__kind", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(timeKind(getTime(mrb, self)))
	}, mrb.ArgsNone())
	// time.__add_i(n): a Time n (a Fixnum) seconds later in self's zone, or
	// nil for a huge n
	t.DefineMethod("__add_i", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.Type() != oruby.MrbTTFixnum {
			return mrb.NilValue()
		}
		n := v.Int64()
		if n <= -(1<<53) || n >= 1<<53 {
			return mrb.NilValue()
		}
		tm := getTime(mrb, self)
		return mrb.DataValue(time.Unix(tm.Unix()+n, int64(tm.Nanosecond())).In(tm.Location()))
	}, mrb.ArgsReq(1))
	// time.__diff(other): self - other in seconds, the exact difference
	// rounded once (MRI's)
	t.DefineMethod("__diff", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := getTime(mrb, self)
		b, err := getTimeCheck(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.NilValue()
		}
		ds, dn := a.Unix()-b.Unix(), int64(a.Nanosecond()-b.Nanosecond())
		if ds > -(1<<33) && ds < 1<<33 {
			d := ds*1_000_000_000 + dn
			if d > -(1<<53) && d < 1<<53 {
				return mrb.FloatValue(float64(d) / 1e9) // exact operands: one rounding
			}
			f, _ := new(big.Rat).SetFrac64(d, 1_000_000_000).Float64()
			return mrb.FloatValue(f)
		}
		num := new(big.Int).Sub(big.NewInt(a.Unix()), big.NewInt(b.Unix()))
		num.Mul(num, big.NewInt(1_000_000_000)).Add(num, big.NewInt(dn))
		f, _ := new(big.Rat).SetFrac(num, big.NewInt(1_000_000_000)).Float64()
		return mrb.FloatValue(f)
	}, mrb.ArgsReq(1))
	// time.__cmp(other): -1, 0 or 1 by seconds and nanoseconds
	t.DefineMethod("__cmp", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := getTime(mrb, self)
		b, err := getTimeCheck(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.NilValue()
		}
		as, bs := a.Unix(), b.Unix()
		an, bn := a.Nanosecond(), b.Nanosecond()
		switch {
		case as < bs || (as == bs && an < bn):
			return mrb.FixnumValue(-1)
		case as > bs || an > bn:
			return mrb.FixnumValue(1)
		}
		return mrb.FixnumValue(0)
	}, mrb.ArgsReq(1))
	// time.__hash: of seconds and nanoseconds (eql? Times have both equal)
	t.DefineMethod("__hash", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		tm := getTime(mrb, self)
		h := uint64(tm.Unix())*0x9E3779B97F4A7C15 ^ uint64(tm.Nanosecond())*0xC2B2AE3D27D4EB4F
		return oruby.Int64(int64(h >> 2))
	}, mrb.ArgsNone())
	// time.__getutc: a new Time of self's instant in UTC
	t.DefineMethod("__getutc", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.DataValue(getTime(mrb, self).UTC())
	}, mrb.ArgsNone())
	// time.__utc!: self in UTC
	t.DefineMethod("__utc!", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		setTime(mrb, self, getTime(mrb, self).UTC())
		return self
	}, mrb.ArgsNone())
	// Time.__at_i(sec): Time.at(a Fixnum) in the local zone
	t.DefineClassMethod("__at_i", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.Type() != oruby.MrbTTFixnum {
			return mrb.NilValue()
		}
		return mrb.DataValue(time.Unix(v.Int64(), 0).In(localLocation()))
	}, mrb.ArgsReq(1))
	// Time.__civil_i(mode, y[, mon, d, h, mi, s, usec]): Time.utc (mode 0)
	// or Time.local (1) of Fixnum (or nil) fields; nil for anything else,
	// out of range ones too, which 26_time.rb converts or raises on
	t.DefineClassMethod("__civil_i", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		if a.Len() < 2 || a.Len() > 8 {
			return mrb.NilValue()
		}
		tm, ok := civilFixnums(a, 1, 7, a.Item(0).Int())
		if !ok {
			return mrb.NilValue()
		}
		return mrb.DataValue(tm)
	}, mrb.ArgsAny())
	// time.__civil_set(y[, mon, d, h, mi, s]): Time.new's local fields in
	// self, as __civil_i; nil when not all Fixnums in range
	t.DefineMethod("__civil_set", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		if a.Len() < 1 || a.Len() > 6 {
			return mrb.NilValue()
		}
		tm, ok := civilFixnums(a, 0, 6, 1)
		if !ok {
			return mrb.NilValue()
		}
		setTime(mrb, self, tm)
		return self
	}, mrb.ArgsAny())

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
