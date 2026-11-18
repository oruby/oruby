package time

import (
	"encoding/binary"
	"strings"
	"time"

	"github.com/oruby/oruby"
)

// Time#_dump and Time._load in MRI's layout (time_mdump / time_mload), so a
// Marshal dump of a Time reads the same in MRI and oruby: 8 bytes of the UTC
// broken-down time, and instance variables on the String for what they do
// not hold (offset, zone, nanoseconds, a year outside 1900..67435).
func initMarshal(mrb *oruby.MrbState, t oruby.RClass) {
	t.DefineMethod("_dump", timeDump, mrb.ArgsOpt(1))
	t.DefineClassMethod("_load", timeLoad, mrb.ArgsReq(1))
	// private, as in MRI: Marshal calls them
	mrb.LoadString("class Time; private :_dump; class << self; private :_load; end; end")
}

func timeDump(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := getTime(mrb, self)
	u := t.UTC()
	year := u.Year()
	yearExt, hasYear := year, false
	if year > 1900+0xffff {
		year, hasYear = 1900+0xffff, true
	} else if year < 1900 {
		year, hasYear = 1900, true
	}
	nsec := u.Nanosecond()
	utc := timeKind(t) == 0
	var p, s uint32
	p = 1<<31 | uint32(year-1900)<<14 | uint32(u.Month()-1)<<10 | uint32(u.Day())<<5 | uint32(u.Hour())
	if utc {
		p |= 1 << 30
	}
	s = uint32(u.Minute())<<26 | uint32(u.Second())<<20 | uint32(nsec/1000)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, p)
	binary.LittleEndian.PutUint32(buf[4:], s)
	str := mrb.BytesValue(buf)
	if _, err := mrb.Funcall(str, oruby.Sym_force_encoding, "ASCII-8BIT"); err != nil {
		return mrb.RaiseError(err)
	}

	set := func(name string, v interface{}) error {
		return mrb.IVSet(str, mrb.Intern(name), mrb.Value(v))
	}
	var err error
	// the Time's own instance variables come first
	mrb.IVForEach(self, func(name oruby.MrbSym, v oruby.Value) bool {
		if n := mrb.SymName(name); !strings.HasPrefix(n, "__") && !strings.HasPrefix(n, "@__") {
			err = mrb.IVSet(str, name, v)
		}
		return err == nil
	})
	if err != nil {
		return mrb.RaiseError(err)
	}
	if sub := nsec % 1000; sub != 0 {
		if err := set("nano_num", sub); err != nil {
			return mrb.RaiseError(err)
		}
		if err := set("nano_den", 1); err != nil {
			return mrb.RaiseError(err)
		}
		// submicro: the digits under a microsecond as packed BCD
		bcd := []byte{byte(sub/100)<<4 | byte(sub/10%10), byte(sub%10) << 4}
		if bcd[1] == 0 {
			bcd = bcd[:1]
		}
		sm := mrb.BytesValue(bcd)
		mrb.Funcall(sm, oruby.Sym_force_encoding, "ASCII-8BIT")
		if err := mrb.IVSet(str, mrb.Intern("submicro"), sm); err != nil {
			return mrb.RaiseError(err)
		}
	}
	zone, off := t.Zone()
	if !utc {
		if err := set("offset", off); err != nil {
			return mrb.RaiseError(err)
		}
	}
	// a timezone object is dumped by its #name (MRI: none, NoMethodError)
	if tz := mrb.IVGet(self, mrb.Intern("@__tz")); !tz.IsNil() {
		name, err := mrb.Funcall(tz, mrb.Intern("name"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		zone = mrb.String(name)
		z := mrb.StrNew(zone)
		mrb.Funcall(z, oruby.Sym_force_encoding, "US-ASCII")
		if err := mrb.IVSet(str, mrb.Intern("zone"), z); err != nil {
			return mrb.RaiseError(err)
		}
	} else if timeKind(t) != 2 && zone != "" {
		z := mrb.StrNew(zone)
		mrb.Funcall(z, oruby.Sym_force_encoding, "US-ASCII")
		if err := mrb.IVSet(str, mrb.Intern("zone"), z); err != nil {
			return mrb.RaiseError(err)
		}
	}
	if hasYear {
		if err := set("year", yearExt); err != nil {
			return mrb.RaiseError(err)
		}
	}
	return str
}

// timeLoad is Time._load(str); self is Time or the subclass dumped
func timeLoad(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.GetArgsFirst()
	if str.Type() != oruby.MrbTTString {
		return mrb.Raise(mrb.ETypeError(), "no implicit conversion into String")
	}
	data := []byte(mrb.String(str))
	if len(data) != 8 {
		return mrb.Raise(mrb.ETypeError(), "marshaled time format differ")
	}
	p := binary.LittleEndian.Uint32(data)
	s := binary.LittleEndian.Uint32(data[4:])

	iv := func(name string) oruby.Value { return mrb.IVGet(str, mrb.Intern(name)) }
	var t time.Time
	utc := false
	if p&(1<<31) == 0 {
		// before Ruby 1.8: seconds and microseconds since the epoch
		t = time.Unix(int64(p), int64(s)*1000).In(localLocation())
	} else {
		utc = p>>30&1 == 1
		year := int(p>>14&0xffff) + 1900
		if y := iv("year"); y.IsInteger() {
			year = y.Int()
		}
		nsec := int(s&0xfffff) * 1000
		if num := iv("nano_num"); num.IsInteger() {
			den := 1
			if d := iv("nano_den"); d.IsInteger() && d.Int() != 0 {
				den = d.Int()
			}
			nsec += num.Int() / den
		}
		t = time.Date(year, time.Month(p>>10&0xf+1), int(p>>5&0x1f), int(p&0x1f),
			int(s>>26&0x3f), int(s>>20&0x3f), nsec, time.UTC)
	}
	if !utc {
		zone, off := "", 0
		if z := iv("zone"); z.Type() == oruby.MrbTTString {
			zone = mrb.String(z)
		}
		if o := iv("offset"); o.IsInteger() {
			off = o.Int()
		}
		local := t.In(localLocation())
		lz, loff := local.Zone()
		switch {
		case iv("offset").IsNil() && zone == "":
			t = local
		case loff == off && (zone == "" || zone == lz):
			t = local // the local zone, as MRI keeps it
		default:
			t = t.In(time.FixedZone(zone, off))
		}
	}

	v, err := mrb.Funcall(self, oruby.Sym_allocate)
	if err != nil {
		return mrb.RaiseError(err)
	}
	mrb.DataSetInterface(v, t)
	// a zone name the class knows (find_timezone) becomes its timezone
	if z := iv("zone"); z.Type() == oruby.MrbTTString && !utc && mrb.RespondTo(self, mrb.Intern("find_timezone")) {
		tz, err := mrb.Funcall(self, mrb.Intern("find_timezone"), z)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if !tz.IsNil() {
			_ = mrb.IVSet(v, mrb.Intern("@__tz"), tz)
		}
	}
	// the other instance variables are the Time's own
	mrb.IVForEach(str, func(name oruby.MrbSym, val oruby.Value) bool {
		switch mrb.SymName(name) {
		case "offset", "zone", "nano_num", "nano_den", "submicro", "year":
		default:
			if !strings.HasPrefix(mrb.SymName(name), "__") {
				err = mrb.IVSet(v, name, val)
			}
		}
		return err == nil
	})
	if err != nil {
		return mrb.RaiseError(err)
	}
	return v
}
