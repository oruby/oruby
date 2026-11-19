// Package date implements Ruby's date library (Date, DateTime). Calendar
// conversions, parsing and formatting are done in Go; the Ruby classes are
// defined in date.rb.
package date

import (
	_ "embed"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"github.com/oruby/oruby/gem/internal/strftime"
)

//go:embed date.rb
var dateRb []byte

func init() {
	oruby.LazyGem("date", func(mrb *oruby.MrbState) interface{} {
		cls := mrb.DefineClass("Date", mrb.ObjectClass())
		def := func(name string, n uint32, f oruby.MrbFuncT) { cls.DefineClassMethod(name, f, mrb.ArgsReq(n)) }
		def("__civil_to_jd", 4, civilToJD)
		def("__jd_to_civil", 2, jdToCivil)
		def("__commercial_to_jd", 4, commercialToJD)
		def("__jd_to_commercial", 2, jdToCommercial)
		def("__valid_civil", 4, validCivil)
		def("__strftime", 2, dateStrftime)
		def("__parse", 2, dateParse)
		def("__strptime", 2, dateStrptime)
		def("__iso8601", 1, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return strictParse(mrb, mrb.GetArgsFirst(), isoParsers)
		})
		def("__rfc3339", 1, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return strictParse(mrb, mrb.GetArgsFirst(), rfc3339Parsers)
		})
		def("__xmlschema", 1, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return strictParse(mrb, mrb.GetArgsFirst(), xmlParsers)
		})
		def("__rfc2822", 1, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return strictParse(mrb, mrb.GetArgsFirst(), rfc2822Parsers)
		})
		def("__httpdate", 1, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return strictParse(mrb, mrb.GetArgsFirst(), httpParsers)
		})
		def("__zone_to_diff", 1, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			off, ok := zoneOffset(mrb.GetArgsFirst().String())
			if !ok {
				return mrb.NilValue()
			}
			return mrb.FixnumValue(int(off))
		})
		rb.LoadRuby(mrb, "date.rb", dateRb)
		return nil
	})
}

// -- calendar ---------------------------------------------------------------

func floor(f float64) int64 { return int64(math.Floor(f)) }

// sg is the Julian Day of calendar reform (+Inf: always Julian, -Inf: always Gregorian)
func cCivilToJD(y, m, d int64, sg float64) (int64, bool) {
	if m <= 2 {
		y--
		m += 12
	}
	a := floor(float64(y) / 100.0)
	b := 2 - a + floor(float64(a)/4.0)
	jd := floor(365.25*float64(y+4716)) + floor(30.6001*float64(m+1)) + d + b - 1524
	ns := true
	if float64(jd) < sg {
		jd -= b
		ns = false
	}
	return jd, ns
}

func cJDToCivil(jd int64, sg float64) (y, m, d int64) {
	var a int64
	if float64(jd) < sg {
		a = jd
	} else {
		x := floor((float64(jd) - 1867216.25) / 36524.25)
		a = jd + 1 + x - floor(float64(x)/4.0)
	}
	b := a + 1524
	c := floor((float64(b) - 122.1) / 365.25)
	dd := floor(365.25 * float64(c))
	e := floor(float64(b-dd) / 30.6001)
	dom := b - dd - floor(30.6001*float64(e))
	if e <= 13 {
		m = e - 1
		y = c - 4716
	} else {
		m = e - 13
		y = c - 4715
	}
	return y, m, dom
}

func cFindFDOY(y int64, sg float64) int64 {
	for d := int64(1); d < 31; d++ {
		if jd, ok := cValidCivil(y, 1, d, sg); ok {
			return jd
		}
	}
	jd, _ := cCivilToJD(y, 1, 1, sg)
	return jd
}

func cFindLDOM(y, m int64, sg float64) (int64, bool) {
	for d := int64(31); d > 0; d-- {
		if jd, ok := cValidCivil(y, m, d, sg); ok {
			return jd, true
		}
	}
	return 0, false
}

func cValidCivil(y, m, d int64, sg float64) (int64, bool) {
	if m < 0 {
		m += 13
	}
	if m < 1 || m > 12 {
		return 0, false
	}
	if d < 0 {
		ldom, ok := cFindLDOM(y, m, sg)
		if !ok {
			return 0, false
		}
		ry, rm, rd := cJDToCivil(ldom+d+1, sg)
		if ry != y || rm != m {
			return 0, false
		}
		d = rd
	}
	jd, _ := cCivilToJD(y, m, d, sg)
	ry, rm, rd := cJDToCivil(jd, sg)
	if ry != y || rm != m || rd != d {
		return 0, false
	}
	return jd, true
}

func cCommercialToJD(y, w, d int64, sg float64) int64 {
	rjd2 := cFindFDOY(y, sg) + 3
	return (rjd2 - mod(rjd2, 7)) + 7*(w-1) + (d - 1)
}

func cJDToCommercial(jd int64, sg float64) (y, w, d int64) {
	a, _, _ := cJDToCivil(jd-3, sg)
	j := cCommercialToJD(a+1, 1, 1, sg)
	if jd >= j {
		y = a + 1
	} else {
		j = cCommercialToJD(a, 1, 1, sg)
		y = a
	}
	w = 1 + floorDiv(jd-j, 7)
	d = mod(jd+1, 7)
	if d == 0 {
		d = 7
	}
	return
}

func mod(a, b int64) int64 {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func ints(mrb *oruby.MrbState, args []oruby.Value, n int) []int64 {
	out := make([]int64, n)
	for i := 0; i < n; i++ {
		v, _ := rb.Int(mrb, args[i])
		out[i] = v
	}
	return out
}

func sgArg(v oruby.Value) float64 {
	if v.Type() == oruby.MrbTTFloat {
		return v.Float64()
	}
	return float64(v.Int64())
}

func civilToJD(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	v := ints(mrb, a, 3)
	jd, _ := cCivilToJD(v[0], v[1], v[2], sgArg(a[3]))
	return mrb.FixnumValue(int(jd))
}

func jdToCivil(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	y, m, d := cJDToCivil(a[0].Int64(), sgArg(a[1]))
	return mrb.Value([]int{int(y), int(m), int(d)})
}

func commercialToJD(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	v := ints(mrb, a, 3)
	return mrb.FixnumValue(int(cCommercialToJD(v[0], v[1], v[2], sgArg(a[3]))))
}

func jdToCommercial(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	y, w, d := cJDToCommercial(a[0].Int64(), sgArg(a[1]))
	return mrb.Value([]int{int(y), int(w), int(d)})
}

// validCivil returns jd or nil (negative month/day count from the end)
func validCivil(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	v := ints(mrb, a, 3)
	jd, ok := cValidCivil(v[0], v[1], v[2], sgArg(a[3]))
	if !ok {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(int(jd))
}

// dateStrftime(format, [year, mon, mday, hour, min, sec, nsec, wday, yday,
// offset, zone, cwyear, cweek, cwday, epoch_sec, epoch_ms])
func dateStrftime(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	format, err := rb.Str(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	f := mrb.RValue(a[1]).RArray()
	get := func(i int) int64 { return f.Item(i).Int64() }
	fields := &strftime.Fields{
		Year: get(0), Mon: int(get(1)), Day: int(get(2)), Hour: int(get(3)), Min: int(get(4)),
		Sec: int(get(5)), Nsec: get(6), Wday: int(get(7)), Yday: int(get(8)), Offset: int(get(9)),
		Zone: f.Item(10).String(), CWYear: get(11), CWeek: int(get(12)), CWday: int(get(13)),
		Epoch: get(14), EpochMilli: get(15),
	}
	return mrb.StrNew(strftime.Format(format, fields))
}

// -- parsing ----------------------------------------------------------------

type parsed map[string]interface{}

func (p parsed) toRuby(mrb *oruby.MrbState) oruby.Value {
	h := mrb.HashNew()
	for _, k := range []string{"year", "mon", "mday", "yday", "cwyear", "cweek", "cwday", "wday",
		"wnum0", "wnum1", "hour", "min", "sec", "sec_fraction", "zone", "offset", "seconds", "_comp", "_bc", "leftover"} {
		v, ok := p[k]
		if !ok {
			continue
		}
		var rv oruby.Value
		switch x := v.(type) {
		case int64:
			rv = mrb.FixnumValue(int(x))
		case int:
			rv = mrb.FixnumValue(x)
		case string:
			rv = mrb.StrNew(x)
		case bool:
			rv = mrb.BoolValue(x)
		case [2]int64: // rational numerator/denominator
			r, err := mrb.Funcall(mrb.ObjectClass().Value(), oruby.Sym_Rational, mrb.FixnumValue(int(x[0])), mrb.FixnumValue(int(x[1])))
			if err != nil {
				mrb.ExcClear()
				continue
			}
			rv = r
		default:
			continue
		}
		mrb.HashSet(h, mrb.SymbolValue(mrb.Intern(k)), rv)
	}
	return h.Value()
}

var monthAbbr = map[string]int64{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7,
	"aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var dayAbbr = map[string]int64{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

const (
	monPat  = `(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`
	dayPat  = `(sun(?:day)?|mon(?:day)?|tue(?:sday)?|wed(?:nesday)?|thu(?:rsday)?|fri(?:day)?|sat(?:urday)?)`
	zonePat = `(z\b|[-+]\d{1,2}(?::?\d{2}(?::?\d{2})?)?(?:\b|$)|(?:gmt|utc)(?:\s*[-+]\d{1,2}(?::?\d{2})?)?\b|[a-z]{3,4}(?:\s+(?:standard|daylight)\s+time)?\b)`
)

var (
	reDay   = regexp.MustCompile(`(?i)\b` + dayPat + `[^-/\d\s]*`)
	reTime  = regexp.MustCompile(`(?i)(\d+):(\d+)(?::(\d+)(?:[,.](\d+))?)?\s*(?:([ap])(?:m\b|\.m\.))?(?:\s*` + zonePat + `)?|(\d+)\s*([ap])(?:m\b|\.m\.)(?:\s*` + zonePat + `)?`)
	reEU    = regexp.MustCompile(`(?i)'?(\d+)[^-\d\s]*\s*` + monPat + `[^-\d\s']*(?:\s*(?:\s*,\s*)?(c(?:e|\.e\.)|b(?:ce|\.c\.e\.)|a(?:d|\.d\.)|b(?:c|\.c\.))?\s*('?-?\d+(?:(?:st|nd|rd|th)\b)?))?`)
	reUS    = regexp.MustCompile(`(?i)\b` + monPat + `[^-\d\s']*\s*('?-?\d+)[^-\d\s']*(?:\s*,?\s*(c(?:e|\.e\.)|b(?:ce|\.c\.e\.)|a(?:d|\.d\.)|b(?:c|\.c\.))?\s*('?-?\d+))?`)
	reISO   = regexp.MustCompile(`('?[-+]?\d+)-(\d+)-('?-?\d+)`)
	reVMS1  = regexp.MustCompile(`(?i)('?-?\d+(?:st|nd|rd|th)?)[-/.]` + monPat + `[^\s-/.]*[-/.]('?-?\d+(?:st|nd|rd|th)?)`)
	reVMS2  = regexp.MustCompile(`(?i)\b` + monPat + `[^\s-/.]*[-/.]('?-?\d+(?:st|nd|rd|th)?)(?:[-/.]('?-?\d+))?`)
	reSla   = regexp.MustCompile(`('?-?\d+)/\s*('?\d+)(?:\D\s*('?-?\d+))?`)
	reSlaM  = regexp.MustCompile(`(?i)('?-?\d+)/\s*` + monPat + `(?:\D\s*('?-?\d+))?`)
	reDot   = regexp.MustCompile(`('?-?\d+)\.\s*('?\d+)\.\s*('?-?\d+)`)
	reIsoW  = regexp.MustCompile(`(?i)\b(\d{2}|\d{4})?-?w(\d{2})(?:-?(\d))?\b`)
	reIsoWD = regexp.MustCompile(`-w-(\d)\b`)
	reIsoMD = regexp.MustCompile(`--(\d{2})?-(\d{2})\b`)
	reIsoM  = regexp.MustCompile(`--(\d{2})(\d{2})?\b`)
	reIsoOr = regexp.MustCompile(`(?:^|[^-\d])(\d{2}|\d{4})-(\d{3})\b`)
	reYear  = regexp.MustCompile(`'(\d+)\b`)
	reMon   = regexp.MustCompile(`(?i)\b` + monPat + `\S*`)
	reMday  = regexp.MustCompile(`(?i)(\d+)(st|nd|rd|th)\b`)
	reDDD   = regexp.MustCompile(`([-+]?)(\d{2,14})(?:\s*t?\s*(\d{2,6})?(?:[,.](\d*))?)?(?:\s*(z\b|[-+]\d{1,4}\b|\[[-+]?\d[^\]]*\]))?`)
	reBC    = regexp.MustCompile(`(?i)\b(bc\b|bce\b|b\.c\.|b\.c\.e\.)`)
	reFrag  = regexp.MustCompile(`\A\s*(\d{1,2})\s*\z`)
)

func atoi(s string) int64 {
	s = strings.TrimLeft(s, "'")
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func fraction(s string) [2]int64 {
	if len(s) > 18 {
		s = s[:18]
	}
	num := atoi(s)
	den := int64(math.Pow10(len(s)))
	return [2]int64{num, den}
}

func setZone(p parsed, z string) {
	z = strings.TrimSpace(z)
	if z == "" {
		return
	}
	p["zone"] = z
	if off, ok := zoneOffset(z); ok {
		p["offset"] = off
	}
}

var zoneTable = map[string]int64{
	"ut": 0, "gmt": 0, "utc": 0, "z": 0, "est": -5 * 3600, "edt": -4 * 3600, "cst": -6 * 3600, "cdt": -5 * 3600,
	"mst": -7 * 3600, "mdt": -6 * 3600, "pst": -8 * 3600, "pdt": -7 * 3600, "jst": 9 * 3600, "cet": 3600,
	"cest": 2 * 3600, "eet": 2 * 3600, "eest": 3 * 3600, "wet": 0, "west": 3600, "bst": 3600, "ist": 19800,
	"hst": -10 * 3600, "akst": -9 * 3600, "akdt": -8 * 3600, "ast": -4 * 3600, "adt": -3 * 3600,
	"a": 3600, "b": 7200, "c": 10800, "d": 14400, "e": 18000, "f": 21600, "g": 25200, "h": 28800, "i": 32400,
	"k": 36000, "l": 39600, "m": 43200, "n": -3600, "o": -7200, "p": -10800, "q": -14400, "r": -18000,
	"s": -21600, "t": -25200, "u": -28800, "v": -32400, "w": -36000, "x": -39600, "y": -43200,
	"eastern": -5 * 3600, "central": -6 * 3600, "mountain": -7 * 3600, "pacific": -8 * 3600,
	"msk": 3 * 3600, "nzst": 12 * 3600, "nzdt": 13 * 3600, "aest": 10 * 3600, "aedt": 11 * 3600,
}

var reOff = regexp.MustCompile(`\A([-+])(\d{1,2})(?::?(\d{2})(?::?(\d{2}))?)?\z`)
var reOffDec = regexp.MustCompile(`\A([-+])(\d+)[.,](\d+)\z`)

func zoneOffset(z string) (int64, bool) {
	s := strings.ToLower(strings.TrimSpace(z))
	dst := false
	if strings.HasSuffix(s, " standard time") {
		s = strings.TrimSuffix(s, " standard time")
	} else if strings.HasSuffix(s, " daylight time") {
		s = strings.TrimSuffix(s, " daylight time")
		dst = true
	} else if strings.HasSuffix(s, " dst") {
		s = strings.TrimSuffix(s, " dst")
		dst = true
	}
	if v, ok := zoneTable[s]; ok {
		if dst {
			v += 3600
		}
		return v, true
	}
	for _, pre := range []string{"gmt", "utc"} {
		if strings.HasPrefix(s, pre) && len(s) > len(pre) {
			s = strings.TrimSpace(s[len(pre):])
			break
		}
	}
	if m := reOff.FindStringSubmatch(s); m != nil {
		var h, mi, se int64
		if m[3] == "" && len(m[2]) > 2 {
			return 0, false
		}
		h = atoi(m[2])
		if m[3] == "" && len(m[2]) <= 2 {
			// "+9" or "+09"
		}
		mi, se = atoi(m[3]), atoi(m[4])
		off := h*3600 + mi*60 + se
		if m[1] == "-" {
			off = -off
		}
		return off, true
	}
	if m := regexp.MustCompile(`\A([-+])(\d{3,4})\z`).FindStringSubmatch(s); m != nil {
		v := atoi(m[2])
		off := v/100*3600 + v%100*60
		if m[1] == "-" {
			off = -off
		}
		return off, true
	}
	if m := reOffDec.FindStringSubmatch(s); m != nil {
		h := atoi(m[2])
		frac := atoi(m[3])
		off := h*3600 + int64(float64(frac)/math.Pow10(len(m[3]))*3600)
		if m[1] == "-" {
			off = -off
		}
		return off, true
	}
	return 0, false
}

// blank replaces matched text by spaces so later parsers do not see it
func blank(s string, loc []int) string {
	return s[:loc[0]] + strings.Repeat(" ", loc[1]-loc[0]) + s[loc[1]:]
}

func sub(s string, m []int, i int) string {
	if m[2*i] < 0 {
		return ""
	}
	return s[m[2*i]:m[2*i+1]]
}

func stripOrd(s string) string {
	low := strings.ToLower(s)
	for _, suf := range []string{"st", "nd", "rd", "th"} {
		if strings.HasSuffix(low, suf) {
			return s[:len(s)-2]
		}
	}
	return s
}

func isLongOrSignedYear(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") || strings.HasPrefix(s, "'") {
		return true
	}
	return len(s) >= 3
}

// s3e applies MRI's 3-element year/month/day disambiguation
func setS3E(p parsed, a, b, c string) {
	a, c = stripOrd(a), stripOrd(c)
	if isLongOrSignedYear(c) && !isLongOrSignedYear(a) {
		setYMD(p, c, b, a, false)
	} else {
		setYMD(p, a, b, c, false)
	}
}

func setYMD(p parsed, y, m, d string, bc bool) {
	y = stripOrd(y)
	d = stripOrd(d)
	// When only day and no year was matched, a 3+ digit or signed number is actually the year (e.g. "feb 2008")
	if y == "" && d != "" && (isLongOrSignedYear(d) || atoi(d) > 31) {
		y, d = d, ""
	}
	// When first token was a 4-digit year and last token is a day (e.g. "2008 feb 23")
	if d != "" && y != "" && isLongOrSignedYear(d) && !isLongOrSignedYear(y) {
		y, d = d, y
	}
	if y != "" {
		if !strings.HasPrefix(y, "'") && len(strings.TrimLeft(y, "-+")) > 2 {
			p["_comp"] = false
		}
		yy := atoi(y)
		if strings.HasPrefix(y, "'") {
			p["_comp"] = true
		}
		if bc {
			yy = -yy + 1
		}
		p["year"] = yy
		if len(strings.TrimLeft(y, "'-+")) <= 2 && !strings.HasPrefix(y, "-") && !strings.HasPrefix(y, "+") && !strings.HasPrefix(y, "'") {
			p["_y2"] = true
		}
	}
	if m != "" {
		if v, ok := monthAbbr[strings.ToLower(m)[:min(3, len(m))]]; ok {
			p["mon"] = v
		} else {
			p["mon"] = atoi(m)
		}
	}
	if d != "" {
		p["mday"] = atoi(d)
	}
}

func parseDate(str string, comp bool) parsed {
	p := parsed{}
	s := str
	// non-printable/odd chars become spaces
	s = strings.Map(func(r rune) rune {
		if r < 32 {
			return ' '
		}
		return r
	}, s)

	if m := reDay.FindStringSubmatchIndex(s); m != nil {
		p["wday"] = dayAbbr[strings.ToLower(sub(s, m, 1))[:3]]
		s = blank(s, m)
	}
	if m := reTime.FindStringSubmatchIndex(s); m != nil {
		if sub(s, m, 1) != "" {
			p["hour"] = atoi(sub(s, m, 1))
			p["min"] = atoi(sub(s, m, 2))
			if v := sub(s, m, 3); v != "" {
				p["sec"] = atoi(v)
			}
			if v := sub(s, m, 4); v != "" {
				p["sec_fraction"] = fraction(v)
			}
			if ap := sub(s, m, 5); ap != "" {
				h := p["hour"].(int64) % 12
				if strings.ToLower(ap) == "p" {
					h += 12
				}
				p["hour"] = h
			}
			setZone(p, sub(s, m, 6))
		} else {
			h := atoi(sub(s, m, 7)) % 12
			if strings.ToLower(sub(s, m, 8)) == "p" {
				h += 12
			}
			p["hour"] = h
			setZone(p, sub(s, m, 9))
		}
		s = blank(s, m)
	}

	bcGlobal := reBC.MatchString(s)
	switch {
	case matchApply(&s, reVMS1, func(m []int, t string) {
		a, mo, c := stripOrd(sub(t, m, 1)), sub(t, m, 2), stripOrd(sub(t, m, 3))
		if isLongOrSignedYear(a) && !isLongOrSignedYear(c) {
			setYMD(p, a, mo, c, false)
		} else {
			setYMD(p, c, mo, a, false)
		}
	}):
	case matchApply(&s, reVMS2, func(m []int, t string) {
		mo, d, y := sub(t, m, 1), stripOrd(sub(t, m, 2)), stripOrd(sub(t, m, 3))
		setYMD(p, y, mo, d, false)
	}):
	case matchApply(&s, reEU, func(m []int, t string) {
		bc := strings.HasPrefix(strings.ToLower(sub(t, m, 3)), "b")
		d, mo, y := stripOrd(sub(t, m, 1)), sub(t, m, 2), stripOrd(sub(t, m, 4))
		if y != "" && isLongOrSignedYear(d) && !isLongOrSignedYear(y) {
			setYMD(p, d, mo, y, bc)
		} else {
			setYMD(p, y, mo, d, bc)
		}
	}):
	case matchApply(&s, reUS, func(m []int, t string) {
		bc := strings.HasPrefix(strings.ToLower(sub(t, m, 3)), "b")
		setYMD(p, sub(t, m, 4), sub(t, m, 1), sub(t, m, 2), bc)
	}):
	case matchApply(&s, reISO, func(m []int, t string) {
		setS3E(p, sub(t, m, 1), sub(t, m, 2), sub(t, m, 3))
	}):
	case matchApply(&s, reSlaM, func(m []int, t string) {
		setYMD(p, sub(t, m, 3), sub(t, m, 2), sub(t, m, 1), false)
	}):
	case matchApply(&s, reSla, func(m []int, t string) {
		a, b, c := sub(t, m, 1), sub(t, m, 2), sub(t, m, 3)
		if c == "" {
			setYMD(p, "", a, b, false) // mon/mday
		} else {
			setS3E(p, a, b, c)
		}
	}):
	case matchApply(&s, reDot, func(m []int, t string) {
		setS3E(p, sub(t, m, 1), sub(t, m, 2), sub(t, m, 3))
	}):
	case matchApply(&s, reIsoW, func(m []int, t string) {
		if y := sub(t, m, 1); y != "" {
			p["cwyear"] = atoi(y)
		}
		p["cweek"] = atoi(sub(t, m, 2))
		if d := sub(t, m, 3); d != "" {
			p["cwday"] = atoi(d)
		}
	}):
	case matchApply(&s, reIsoWD, func(m []int, t string) {
		p["cwday"] = atoi(sub(t, m, 1))
	}):
	case matchApply(&s, reIsoMD, func(m []int, t string) {
		if v := sub(t, m, 1); v != "" {
			p["mon"] = atoi(v)
		}
		p["mday"] = atoi(sub(t, m, 2))
	}):
	case matchApply(&s, reIsoOr, func(m []int, t string) {
		p["year"] = atoi(sub(t, m, 1))
		p["yday"] = atoi(sub(t, m, 2))
	}):
	default:
		if m := reYear.FindStringSubmatchIndex(s); m != nil {
			p["year"] = atoi(sub(s, m, 1))
			s = blank(s, m)
		}
		if m := reMon.FindStringSubmatchIndex(s); m != nil {
			p["mon"] = monthAbbr[strings.ToLower(sub(s, m, 1))[:3]]
			s = blank(s, m)
		}
		if m := reMday.FindStringSubmatchIndex(s); m != nil {
			p["mday"] = atoi(sub(s, m, 1))
			s = blank(s, m)
		}
		if m := reDDD.FindStringSubmatchIndex(s); m != nil {
			parseDDD(p, s, m)
			s = blank(s, m)
		}
	}
	if bcGlobal {
		if y, ok := p["year"].(int64); ok {
			if _, done := p["_bcdone"]; !done {
				p["year"] = -y + 1
			}
		}
	}
	// two digit year completion
	if y2, ok := p["_y2"]; ok && y2.(bool) {
		if comp {
			if c, ok := p["_comp"].(bool); !ok || c {
				if y, ok := p["year"].(int64); ok && y >= 0 && y <= 99 {
					if y >= 69 {
						p["year"] = y + 1900
					} else {
						p["year"] = y + 2000
					}
				}
			}
		}
	}
	delete(p, "_y2")
	delete(p, "_comp")
	delete(p, "_bcdone")
	if _, ok := p["mon"]; !ok {
		if _, ok := p["year"]; !ok {
			if _, ok := p["mday"]; !ok {
				if m := reFrag.FindStringSubmatch(s); m != nil {
					if _, ok := p["hour"]; ok {
						p["mday"] = atoi(m[1])
					}
				}
			}
		}
	}
	return p
}

func matchApply(s *string, re *regexp.Regexp, f func(m []int, t string)) bool {
	m := re.FindStringSubmatchIndex(*s)
	if m == nil {
		return false
	}
	f(m, *s)
	*s = blank(*s, m[:2])
	return true
}

// parseDDD interprets runs of digits (date_parse.c parse_ddd_cb)
func parseDDD(p parsed, s string, m []int) {
	sign := sub(s, m, 1)
	digits := sub(s, m, 2)
	n := len(digits)
	y := func(v string) { p["year"] = atoi(sign + v) }
	switch n {
	case 2:
		if _, ok := p["hour"]; ok && sub(s, m, 3) == "" {
			p["mday"] = atoi(digits)
		} else {
			p["mday"] = atoi(digits)
		}
	case 3:
		p["yday"] = atoi(digits)
	case 4:
		p["mon"] = atoi(digits[:2])
		p["mday"] = atoi(digits[2:])
	case 5:
		y(digits[:2])
		p["yday"] = atoi(digits[2:])
		p["_y2"] = true
	case 6:
		y(digits[:2])
		p["mon"] = atoi(digits[2:4])
		p["mday"] = atoi(digits[4:])
		p["_y2"] = true
	case 7:
		y(digits[:4])
		p["yday"] = atoi(digits[4:])
	case 8, 10, 12, 14:
		y(digits[:4])
		p["mon"] = atoi(digits[4:6])
		p["mday"] = atoi(digits[6:8])
		if n >= 10 {
			p["hour"] = atoi(digits[8:10])
		}
		if n >= 12 {
			p["min"] = atoi(digits[10:12])
		}
		if n >= 14 {
			p["sec"] = atoi(digits[12:14])
		}
	}
	if t := sub(s, m, 3); t != "" {
		switch len(t) {
		case 2, 4, 6:
			p["hour"] = atoi(t[:2])
			if len(t) >= 4 {
				p["min"] = atoi(t[2:4])
			}
			if len(t) >= 6 {
				p["sec"] = atoi(t[4:6])
			}
		}
	}
	if f := sub(s, m, 4); f != "" {
		p["sec_fraction"] = fraction(f)
	}
	if z := sub(s, m, 5); z != "" {
		z = strings.Trim(z, "[]")
		setZone(p, z)
	}
}

func dateParse(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	str, err := rb.Str(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if len(str) > 128 {
		return mrb.Raise(mrb.EArgumentError(), "string length ("+strconv.Itoa(len(str))+") exceeds the limit 128")
	}
	return parseDate(str, a[1].Bool()).toRuby(mrb)
}

// -- strict formats -----------------------------------------------------------

type strictParser struct {
	re *regexp.Regexp
	f  func(p parsed, m []string)
}

var (
	isoParsers = []strictParser{
		{regexp.MustCompile(`(?i)\A\s*(?:([-+]?\d{2,}|-)-(\d{2})?(?:-(\d{2}))?|([-+]?\d{2,})?-(\d{3})|(\d{4}|\d{2})?-w(\d{2})-(\d)|-w-(\d))(?:t(\d{2}):(\d{2})(?::(\d{2})(?:[,.](\d+))?)?(z|[-+]\d{2}(?::?\d{2})?)?)?\s*\z`), isoExtended},
		{regexp.MustCompile(`(?i)\A\s*(?:([-+]?(?:\d{4}|\d{2})|--)(\d{2}|-)(\d{2})|([-+]?(?:\d{4}|\d{2}))(\d{3})|-(\d{3})|(\d{4}|\d{2})w(\d{2})(\d)|-w(\d{2})(\d)|-w-(\d))(?:t?(\d{2})(\d{2})(?:(\d{2})(?:[,.](\d+))?)?(z|[-+]\d{2}(?:\d{2})?)?)?\s*\z`), isoBasic},
		{regexp.MustCompile(`(?i)\A\s*(?:(\d{2}):(\d{2})(?::(\d{2})(?:[,.](\d+))?)?(z|[-+]\d{2}(?::?\d{2})?)?)\s*\z`), isoTime},
	}
	rfc3339Parsers = []strictParser{
		{regexp.MustCompile(`(?i)\A\s*(-?\d{4})-(\d{2})-(\d{2})(?:t|\s)(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(z|[-+]\d{2}:\d{2})\s*\z`), func(p parsed, m []string) {
			setYMD(p, m[1], m[2], m[3], false)
			p["hour"], p["min"], p["sec"] = atoi(m[4]), atoi(m[5]), atoi(m[6])
			if m[7] != "" {
				p["sec_fraction"] = fraction(m[7])
			}
			setZone(p, m[8])
		}},
	}
	xmlParsers = []strictParser{
		{regexp.MustCompile(`(?i)\A\s*(-?\d{4,})(?:-(\d{2})(?:-(\d{2}))?)?(?:t(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?)?(z|[-+]\d{2}:\d{2})?\s*\z`), func(p parsed, m []string) {
			setYMD(p, m[1], m[2], m[3], false)
			if m[4] != "" {
				p["hour"], p["min"], p["sec"] = atoi(m[4]), atoi(m[5]), atoi(m[6])
			}
			if m[7] != "" {
				p["sec_fraction"] = fraction(m[7])
			}
			setZone(p, m[8])
		}},
		{regexp.MustCompile(`(?i)\A\s*(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(z|[-+]\d{2}:\d{2})?\s*\z`), func(p parsed, m []string) {
			p["hour"], p["min"], p["sec"] = atoi(m[1]), atoi(m[2]), atoi(m[3])
			if m[4] != "" {
				p["sec_fraction"] = fraction(m[4])
			}
			setZone(p, m[5])
		}},
		{regexp.MustCompile(`(?i)\A\s*(?:--(\d{2})(?:-(\d{2}))?|---(\d{2}))(z|[-+]\d{2}:\d{2})?\s*\z`), func(p parsed, m []string) {
			if m[1] != "" {
				p["mon"] = atoi(m[1])
			}
			if m[2] != "" {
				p["mday"] = atoi(m[2])
			}
			if m[3] != "" {
				p["mday"] = atoi(m[3])
			}
			setZone(p, m[4])
		}},
	}
	rfc2822Parsers = []strictParser{
		{regexp.MustCompile(`(?i)\A\s*(?:` + dayPat + `\s*,\s+)?(\d{1,2})\s+` + monPat + `\s+(-?\d{2,})\s+(\d{2}):(\d{2})(?::(\d{2}))?\s*([-+]\d{4}|ut|gmt|e[sd]t|c[sd]t|m[sd]t|p[sd]t|[a-ik-z])\s*\z`), func(p parsed, m []string) {
			if m[1] != "" {
				p["wday"] = dayAbbr[strings.ToLower(m[1])[:3]]
			}
			setYMD(p, m[4], m[3], m[2], false)
			if y := p["year"].(int64); len(m[4]) < 4 {
				if y < 50 {
					p["year"] = y + 2000
				} else {
					p["year"] = y + 1900
				}
			}
			p["hour"], p["min"] = atoi(m[5]), atoi(m[6])
			if m[7] != "" {
				p["sec"] = atoi(m[7])
			}
			setZone(p, m[8])
		}},
	}
	httpParsers = []strictParser{
		{regexp.MustCompile(`(?i)\A\s*` + dayPat + `\s*,\s+(\d{2})\s+` + monPat + `\s+(-?\d{4})\s+(\d{2}):(\d{2}):(\d{2})\s+(gmt)\s*\z`), func(p parsed, m []string) {
			p["wday"] = dayAbbr[strings.ToLower(m[1])[:3]]
			setYMD(p, m[4], m[3], m[2], false)
			p["hour"], p["min"], p["sec"] = atoi(m[5]), atoi(m[6]), atoi(m[7])
			setZone(p, m[8])
		}},
		{regexp.MustCompile(`(?i)\A\s*` + dayPat + `\s*,\s+(\d{2})\s*-\s*` + monPat + `\s*-\s*(\d{2})\s+(\d{2}):(\d{2}):(\d{2})\s+(gmt)\s*\z`), func(p parsed, m []string) {
			p["wday"] = dayAbbr[strings.ToLower(m[1])[:3]]
			y := atoi(m[4])
			if y < 50 {
				y += 2000
			} else {
				y += 1900
			}
			setYMD(p, "", m[3], m[2], false)
			p["year"] = y
			p["hour"], p["min"], p["sec"] = atoi(m[5]), atoi(m[6]), atoi(m[7])
			setZone(p, m[8])
		}},
		{regexp.MustCompile(`(?i)\A\s*` + dayPat + `\s+` + monPat + `\s+(\d{1,2})\s+(\d{2}):(\d{2}):(\d{2})\s+(\d{4})\s*\z`), func(p parsed, m []string) {
			p["wday"] = dayAbbr[strings.ToLower(m[1])[:3]]
			setYMD(p, m[7], m[2], m[3], false)
			p["hour"], p["min"], p["sec"] = atoi(m[4]), atoi(m[5]), atoi(m[6])
		}},
	}
)

func isoTimePart(p parsed, h, mi, s, f, z string) {
	if h == "" {
		return
	}
	p["hour"], p["min"] = atoi(h), atoi(mi)
	if s != "" {
		p["sec"] = atoi(s)
	}
	if f != "" {
		p["sec_fraction"] = fraction(f)
	}
	setZone(p, z)
}

func isoYear(p parsed, y string) {
	if y == "" || y == "-" || y == "--" {
		return
	}
	v := atoi(y)
	if len(strings.TrimLeft(y, "-+")) == 2 && y[0] != '-' && y[0] != '+' {
		if v >= 69 {
			v += 1900
		} else {
			v += 2000
		}
	}
	p["year"] = v
}

func isoExtended(p parsed, m []string) {
	switch {
	case m[1] != "" || m[2] != "":
		isoYear(p, m[1])
		if m[2] != "" {
			p["mon"] = atoi(m[2])
		}
		if m[3] != "" {
			p["mday"] = atoi(m[3])
		}
	case m[5] != "":
		isoYear(p, m[4])
		p["yday"] = atoi(m[5])
	case m[7] != "":
		if m[6] != "" {
			v := atoi(m[6])
			if len(m[6]) == 2 {
				if v >= 69 {
					v += 1900
				} else {
					v += 2000
				}
			}
			p["cwyear"] = v
		}
		p["cweek"], p["cwday"] = atoi(m[7]), atoi(m[8])
	case m[9] != "":
		p["cwday"] = atoi(m[9])
	}
	isoTimePart(p, m[10], m[11], m[12], m[13], m[14])
}

func isoBasic(p parsed, m []string) {
	switch {
	case m[3] != "":
		isoYear(p, m[1])
		if m[2] != "-" {
			p["mon"] = atoi(m[2])
		}
		p["mday"] = atoi(m[3])
	case m[5] != "":
		isoYear(p, m[4])
		p["yday"] = atoi(m[5])
	case m[6] != "":
		p["yday"] = atoi(m[6])
	case m[8] != "":
		isoYear(p, m[7])
		p["cwyear"] = p["year"]
		delete(p, "year")
		p["cweek"], p["cwday"] = atoi(m[8]), atoi(m[9])
	case m[10] != "":
		p["cweek"], p["cwday"] = atoi(m[10]), atoi(m[11])
	case m[12] != "":
		p["cwday"] = atoi(m[12])
	}
	isoTimePart(p, m[13], m[14], m[15], m[16], m[17])
}

func isoTime(p parsed, m []string) {
	isoTimePart(p, m[1], m[2], m[3], m[4], m[5])
}

func strictParse(mrb *oruby.MrbState, v oruby.Value, parsers []strictParser) oruby.MrbValue {
	str, err := rb.Str(mrb, v)
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if len(str) > 128 {
		return mrb.Raise(mrb.EArgumentError(), "string length ("+strconv.Itoa(len(str))+") exceeds the limit 128")
	}
	for _, sp := range parsers {
		if m := sp.re.FindStringSubmatch(str); m != nil {
			p := parsed{}
			sp.f(p, m)
			return p.toRuby(mrb)
		}
	}
	return mrb.HashNew().Value()
}

// -- strptime -------------------------------------------------------------------

var strptimeNum = regexp.MustCompile(`\A[-+]?\d+`)

func dateStrptime(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	str, err := rb.Str(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	format, err := rb.Str(mrb, a[1])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	p := parsed{}
	rest, ok := strptime(str, format, p)
	if !ok {
		return mrb.NilValue()
	}
	if rest != "" {
		p["leftover"] = rest
	}
	if c, ok := p["_cent"].(int64); ok {
		if y, ok := p["year"].(int64); ok {
			p["year"] = c*100 + (y % 100)
		} else {
			p["year"] = c * 100
		}
		if y, ok := p["cwyear"].(int64); ok {
			p["cwyear"] = c*100 + (y % 100)
		}
	}
	if mer, ok := p["_merid"].(int64); ok {
		if h, ok := p["hour"].(int64); ok {
			p["hour"] = h%12 + mer
		}
	}
	return p.toRuby(mrb)
}

func readNum(s string, maxw int) (int64, string, bool) {
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	j := i
	for j < len(s) && j-i < maxw && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0, s, false
	}
	return atoi(s[:j]), s[j:], true
}

func strptime(str, format string, p parsed) (string, bool) {
	s := str
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c == ' ' || c == '\t' || c == '\n' {
			s = strings.TrimLeft(s, " \t\n\v\f\r")
			continue
		}
		if c != '%' || i+1 >= len(format) {
			if s == "" || s[0] != c {
				return s, false
			}
			s = s[1:]
			continue
		}
		i++
		for i < len(format) && strings.IndexByte("-_0^#", format[i]) >= 0 {
			i++
		}
		width := 0
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			width = width*10 + int(format[i]-'0')
			i++
		}
		colons := 0
		for i < len(format) && format[i] == ':' {
			colons++
			i++
		}
		if i < len(format) && (format[i] == 'E' || format[i] == 'O') {
			i++
		}
		if i >= len(format) {
			return s, false
		}
		var v int64
		var ok bool
		num := func(def int) bool {
			w := def
			if width > 0 {
				w = width
			}
			v, s, ok = readNum(s, w)
			return ok
		}
		switch format[i] {
		case 'Y':
			if !num(40) {
				return s, false
			}
			p["year"] = v
		case 'C':
			if !num(2) {
				return s, false
			}
			p["_cent"] = v
		case 'y':
			if !num(2) {
				return s, false
			}
			if v < 69 {
				v += 2000
			} else {
				v += 1900
			}
			p["year"] = v
		case 'm':
			if !num(2) || v < 1 || v > 12 {
				return s, false
			}
			p["mon"] = v
		case 'd', 'e':
			s = strings.TrimLeft(s, " ")
			if !num(2) || v < 1 || v > 31 {
				return s, false
			}
			p["mday"] = v
		case 'j':
			if !num(3) || v < 1 || v > 366 {
				return s, false
			}
			p["yday"] = v
		case 'H', 'k':
			s = strings.TrimLeft(s, " ")
			if !num(2) || v > 24 {
				return s, false
			}
			p["hour"] = v
		case 'I', 'l':
			s = strings.TrimLeft(s, " ")
			if !num(2) || v < 1 || v > 12 {
				return s, false
			}
			p["hour"] = v
		case 'M':
			if !num(2) || v > 59 {
				return s, false
			}
			p["min"] = v
		case 'S':
			if !num(2) || v > 60 {
				return s, false
			}
			p["sec"] = v
		case 'L', 'N':
			j := 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' && (width == 0 || j < width) {
				j++
			}
			if j == 0 {
				return s, false
			}
			p["sec_fraction"] = fraction(s[:j])
			s = s[j:]
		case 'p', 'P':
			low := strings.ToLower(s)
			switch {
			case strings.HasPrefix(low, "am"), strings.HasPrefix(low, "pm"):
				p["_merid"] = map[bool]int64{true: 12, false: 0}[low[0] == 'p']
				s = s[2:]
			case strings.HasPrefix(low, "a.m."), strings.HasPrefix(low, "p.m."):
				p["_merid"] = map[bool]int64{true: 12, false: 0}[low[0] == 'p']
				s = s[4:]
			default:
				return s, false
			}
		case 'A', 'a':
			low := strings.ToLower(s)
			found := false
			for _, name := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
				if strings.HasPrefix(low, name) {
					p["wday"] = dayAbbr[name[:3]]
					s = s[len(name):]
					found = true
					break
				}
				if strings.HasPrefix(low, name[:3]) {
					p["wday"] = dayAbbr[name[:3]]
					s = s[3:]
					found = true
					break
				}
			}
			if !found {
				return s, false
			}
		case 'B', 'b', 'h':
			low := strings.ToLower(s)
			found := false
			for _, name := range []string{"january", "february", "march", "april", "may", "june", "july",
				"august", "september", "october", "november", "december"} {
				if strings.HasPrefix(low, name) {
					p["mon"] = monthAbbr[name[:3]]
					s = s[len(name):]
					found = true
					break
				}
				if strings.HasPrefix(low, name[:3]) {
					p["mon"] = monthAbbr[name[:3]]
					s = s[3:]
					found = true
					break
				}
			}
			if !found {
				return s, false
			}
		case 'u':
			if !num(1) || v < 1 || v > 7 {
				return s, false
			}
			p["cwday"] = v
		case 'w':
			if !num(1) || v > 6 {
				return s, false
			}
			p["wday"] = v
		case 'G':
			if !num(40) {
				return s, false
			}
			p["cwyear"] = v
		case 'g':
			if !num(2) {
				return s, false
			}
			if v < 69 {
				v += 2000
			} else {
				v += 1900
			}
			p["cwyear"] = v
		case 'V':
			if !num(2) || v < 1 || v > 53 {
				return s, false
			}
			p["cweek"] = v
		case 'U':
			if !num(2) || v > 53 {
				return s, false
			}
			p["wnum0"] = v
		case 'W':
			if !num(2) || v > 53 {
				return s, false
			}
			p["wnum1"] = v
		case 's':
			if !num(40) {
				return s, false
			}
			p["seconds"] = v
		case 'Q':
			if !num(40) {
				return s, false
			}
			p["seconds"] = [2]int64{v, 1000}
		case 'z', 'Z':
			m := regexp.MustCompile(`(?i)\A(?:gmt|utc?)?[-+]\d+(?:[.,]\d+)?(?::\d+(?::\d+)?)?|(?i)\A[a-z.\s]+(?:standard|daylight)\s+time\b|(?i)\A[a-z]+(?:\s+dst)?\b`).FindString(s)
			if m == "" {
				return s, false
			}
			setZone(p, m)
			s = s[len(m):]
		case 'n', 't':
			s = strings.TrimLeft(s, " \t\n")
		case '%':
			if s == "" || s[0] != '%' {
				return s, false
			}
			s = s[1:]
		case 'F':
			r, ok := strptime(s, "%Y-%m-%d", p)
			if !ok {
				return r, false
			}
			s = r
		case 'D', 'x':
			r, ok := strptime(s, "%m/%d/%y", p)
			if !ok {
				return r, false
			}
			s = r
		case 'T', 'X':
			r, ok := strptime(s, "%H:%M:%S", p)
			if !ok {
				return r, false
			}
			s = r
		case 'R':
			r, ok := strptime(s, "%H:%M", p)
			if !ok {
				return r, false
			}
			s = r
		case 'r':
			r, ok := strptime(s, "%I:%M:%S %p", p)
			if !ok {
				return r, false
			}
			s = r
		case 'c':
			r, ok := strptime(s, "%a %b %e %H:%M:%S %Y", p)
			if !ok {
				return r, false
			}
			s = r
		case 'v':
			r, ok := strptime(s, "%e-%b-%Y", p)
			if !ok {
				return r, false
			}
			s = r
		case '+':
			r, ok := strptime(s, "%a %b %e %H:%M:%S %Z %Y", p)
			if !ok {
				return r, false
			}
			s = r
		default:
			return s, false
		}
		_ = colons
	}
	return s, true
}
