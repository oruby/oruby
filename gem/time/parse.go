package time

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

func initParse(mrb *oruby.MrbState, t oruby.RClass) {
	t.DefineClassMethod("rfc2822", timeParseRFC2822, mrb.ArgsReq(1))
	t.DefineClassMethod("httpdate", timeParseHTTPDate, mrb.ArgsReq(1))
	t.DefineClassMethod("xmlschema", timeParseXMLSchema, mrb.ArgsReq(1))
	sc := mrb.SingletonClass(t.Value())
	sc.DefineAlias("rfc822", "rfc2822")
	sc.DefineAlias("iso8601", "xmlschema")
	t.DefineMethod("to_time", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return self
	}, mrb.ArgsNone())
	t.DefineMethod("to_date", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if _, err := mrb.Resolve("date"); err != nil {
			return mrb.RaiseError(err)
		}
		v, err := mrb.Funcall(self, mrb.Intern("to_date"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return v
	}, mrb.ArgsNone())
	t.DefineMethod("to_datetime", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if _, err := mrb.Resolve("date"); err != nil {
			return mrb.RaiseError(err)
		}
		v, err := mrb.Funcall(self, mrb.Intern("to_datetime"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return v
	}, mrb.ArgsNone())
}

var monthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

var rfc2822ZoneMap = map[string]int{
	"UT": 0, "GMT": 0, "UTC": 0, "Z": 0,
	"EST": -5 * 3600, "EDT": -4 * 3600,
	"CST": -6 * 3600, "CDT": -5 * 3600,
	"MST": -7 * 3600, "MDT": -6 * 3600,
	"PST": -8 * 3600, "PDT": -7 * 3600,
}

var (
	reRFC2822 = regexp.MustCompile(`(?i)\A\s*(?:(?:mon|tue|wed|thu|fri|sat|sun)\s*,\s*)?(\d{1,2})\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\s+(\d{2,4})\s+(\d{2})\s*:\s*(\d{2})(?:\s*:\s*(\d{2}))?\s+([+-]\d{4}|[A-Za-z]+)(?:\s*\([^()]*\))?\s*\z`)
	reHTTP1   = regexp.MustCompile(`(?i)\A\s*(?:mon|tue|wed|thu|fri|sat|sun),\s+(\d{2})\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\s+(\d{4})\s+(\d{2}):(\d{2}):(\d{2})\s+GMT\s*\z`)
	reHTTP2   = regexp.MustCompile(`(?i)\A\s*(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday),\s+(\d{2})-(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)-(\d{2})\s+(\d{2}):(\d{2}):(\d{2})\s+GMT\s*\z`)
	reHTTP3   = regexp.MustCompile(`(?i)\A\s*(?:mon|tue|wed|thu|fri|sat|sun)\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\s+(\d{1,2})\s+(\d{2}):(\d{2}):(\d{2})\s+(\d{4})\s*\z`)
	reXML     = regexp.MustCompile(`\A\s*(-?\d{4,})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|z|[+-]\d{2}:\d{2})?\s*\z`)
)

func parseZoneOffset(z string) (*time.Location, bool) {
	if len(z) == 5 && (z[0] == '+' || z[0] == '-') {
		h, err1 := strconv.Atoi(z[1:3])
		m, err2 := strconv.Atoi(z[3:5])
		if err1 != nil || err2 != nil || m > 59 {
			return nil, false
		}
		off := h*3600 + m*60
		if z[0] == '-' {
			off = -off
		}
		if off == 0 && z[0] == '-' {
			return time.UTC, true
		}
		return time.FixedZone("", off), true
	}
	u := strings.ToUpper(z)
	if off, ok := rfc2822ZoneMap[u]; ok {
		if u == "GMT" || u == "UTC" || u == "UT" || u == "Z" {
			return time.UTC, true
		}
		return time.FixedZone("", off), true
	}
	if len(u) == 1 {
		c := u[0]
		if c >= 'A' && c <= 'I' {
			return time.FixedZone("", -int(c-'A'+1)*3600), true
		}
		if c >= 'K' && c <= 'M' {
			return time.FixedZone("", -int(c-'K'+10)*3600), true
		}
		if c >= 'N' && c <= 'Y' {
			return time.FixedZone("", int(c-'N'+1)*3600), true
		}
	}
	return nil, false
}

func timeParseRFC2822(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	m := reRFC2822.FindStringSubmatch(s)
	if m == nil {
		return mrb.Raisef(mrb.EArgumentError(), "not RFC 2822 compliant date: %q", s)
	}
	day, _ := strconv.Atoi(m[1])
	mon := monthMap[strings.ToLower(m[2])]
	year, _ := strconv.Atoi(m[3])
	if len(m[3]) <= 2 {
		if year < 50 {
			year += 2000
		} else {
			year += 1900
		}
	}
	hour, _ := strconv.Atoi(m[4])
	min, _ := strconv.Atoi(m[5])
	sec := 0
	if m[6] != "" {
		sec, _ = strconv.Atoi(m[6])
	}
	loc, ok := parseZoneOffset(m[7])
	if !ok {
		return mrb.Raisef(mrb.EArgumentError(), "not RFC 2822 compliant date: %q", s)
	}
	tm := time.Date(year, mon, day, hour, min, sec, 0, loc)
	return timeValue(mrb, self, tm)
}

func timeParseHTTPDate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if m := reHTTP1.FindStringSubmatch(s); m != nil {
		day, _ := strconv.Atoi(m[1])
		mon := monthMap[strings.ToLower(m[2])]
		year, _ := strconv.Atoi(m[3])
		hour, _ := strconv.Atoi(m[4])
		min, _ := strconv.Atoi(m[5])
		sec, _ := strconv.Atoi(m[6])
		return timeValue(mrb, self, time.Date(year, mon, day, hour, min, sec, 0, time.UTC))
	}
	if m := reHTTP2.FindStringSubmatch(s); m != nil {
		day, _ := strconv.Atoi(m[1])
		mon := monthMap[strings.ToLower(m[2])]
		year, _ := strconv.Atoi(m[3])
		if year < 50 {
			year += 2000
		} else {
			year += 1900
		}
		hour, _ := strconv.Atoi(m[4])
		min, _ := strconv.Atoi(m[5])
		sec, _ := strconv.Atoi(m[6])
		return timeValue(mrb, self, time.Date(year, mon, day, hour, min, sec, 0, time.UTC))
	}
	if m := reHTTP3.FindStringSubmatch(s); m != nil {
		mon := monthMap[strings.ToLower(m[1])]
		day, _ := strconv.Atoi(m[2])
		hour, _ := strconv.Atoi(m[3])
		min, _ := strconv.Atoi(m[4])
		sec, _ := strconv.Atoi(m[5])
		year, _ := strconv.Atoi(m[6])
		return timeValue(mrb, self, time.Date(year, mon, day, hour, min, sec, 0, time.UTC))
	}
	return mrb.Raisef(mrb.EArgumentError(), "not RFC 2616 compliant date: %q", s)
}

func timeParseXMLSchema(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	m := reXML.FindStringSubmatch(s)
	if m == nil {
		return mrb.Raisef(mrb.EArgumentError(), "invalid xmlschema format: %q", s)
	}
	year, _ := strconv.Atoi(m[1])
	mon, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	hour, _ := strconv.Atoi(m[4])
	min, _ := strconv.Atoi(m[5])
	sec, _ := strconv.Atoi(m[6])
	if mon < 1 || mon > 12 || day < 1 || day > 31 || hour > 24 || min > 59 || sec > 60 {
		return mrb.Raisef(mrb.EArgumentError(), "invalid xmlschema format: %q", s)
	}
	nsec := 0
	if m[7] != "" {
		frac := m[7]
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}
		nsec, _ = strconv.Atoi(frac)
	}
	var loc *time.Location
	switch z := m[8]; {
	case z == "":
		loc = localLocation()
	case z == "Z" || z == "z":
		loc = time.UTC
	default:
		zh, _ := strconv.Atoi(z[1:3])
		zm, _ := strconv.Atoi(z[4:6])
		off := zh*3600 + zm*60
		if z[0] == '-' {
			off = -off
		}
		loc = time.FixedZone("", off)
	}
	return timeValue(mrb, self, time.Date(year, time.Month(mon), day, hour, min, sec, nsec, loc))
}
