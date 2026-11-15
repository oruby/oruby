// Package strftime formats dates and times with Ruby's strftime directives
// (shared by Time#strftime and Date/DateTime#strftime).
package strftime

import (
	"strconv"
	"strings"
)

// Fields of the moment to format
type Fields struct {
	Year           int64
	Mon, Day       int
	Hour, Min, Sec int
	Nsec           int64 // fraction of second in nanoseconds
	Wday, Yday     int   // 0..6 (Sunday 0), 1..366
	Offset         int   // UTC offset in seconds
	Zone           string
	CWYear         int64 // ISO 8601 week based year
	CWeek          int   // ISO 8601 week number
	CWday          int   // ISO 8601 day of week (Monday 1)
	Epoch          int64 // seconds since Unix epoch (%s)
	EpochMilli     int64 // milliseconds since Unix epoch (%Q)
	UTC            bool  // %Z prints "UTC"; %-z
}

var (
	monthNames = []string{"January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December"}
	dayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
)

// Format formats f according to Ruby strftime format string
func Format(format string, f *Fields) string {
	var out strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 >= len(format) {
			out.WriteByte(c)
			continue
		}
		start := i
		i++
		// flags
		var padding byte // 0 default, '-' none, '_' space, '0' zero
		upcase, chcase := false, false
		colons := 0
	flags:
		for ; i < len(format); i++ {
			switch format[i] {
			case '-', '_', '0':
				padding = format[i]
			case '^':
				upcase = true
			case '#':
				chcase = true
			default:
				break flags
			}
		}
		width := -1
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			if width < 0 {
				width = 0
			}
			width = width*10 + int(format[i]-'0')
			i++
		}
		for i < len(format) && format[i] == ':' {
			colons++
			i++
		}
		if i < len(format) && (format[i] == 'E' || format[i] == 'O') {
			i++
		}
		if i >= len(format) {
			out.WriteString(format[start:])
			break
		}
		d := format[i]
		s, ok := directive(d, f, padding, width, colons, &upcase, &chcase)
		if !ok {
			out.WriteString(format[start : i+1])
			continue
		}
		if upcase {
			s = strings.ToUpper(s)
		}
		if chcase {
			s = swapCase(s, d)
		}
		out.WriteString(s)
	}
	return out.String()
}

func swapCase(s string, d byte) string {
	switch d {
	case 'p':
		return strings.ToLower(s)
	case 'A', 'a', 'B', 'b', 'h', 'Z':
		return strings.ToUpper(s)
	}
	return s
}

// num formats integer with default padding and width
func num(v int64, defWidth int, defPad byte, padding byte, width int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	w := defWidth
	if width >= 0 {
		w = width
	}
	pad := defPad
	if padding == '0' || padding == '_' {
		pad = padding
		if pad == '_' {
			pad = ' '
		}
	}
	if padding == '-' {
		w = 0
	}
	if neg {
		w--
	}
	if len(s) < w {
		if pad == ' ' {
			s = strings.Repeat(" ", w-len(s)) + s
			if neg {
				i := strings.LastIndexByte(s, ' ')
				return s[:i] + "-" + s[i+1:]
			}
			return s
		}
		s = strings.Repeat("0", w-len(s)) + s
	}
	if neg {
		return "-" + s
	}
	return s
}

// text pads alphabetic directives (space padding by default)
func text(s string, padding byte, width int) string {
	if width > len(s) && padding != '-' {
		pad := " "
		if padding == '0' {
			pad = "0"
		}
		return strings.Repeat(pad, width-len(s)) + s
	}
	return s
}

func directive(d byte, f *Fields, padding byte, width, colons int, upcase, chcase *bool) (string, bool) {
	switch d {
	case 'Y':
		if f.Year < 0 {
			return num(f.Year, 5, '0', padding, width), true
		}
		return num(f.Year, 4, '0', padding, width), true
	case 'C':
		return num(floorDiv(f.Year, 100), 2, '0', padding, width), true
	case 'y':
		return num(mod(f.Year, 100), 2, '0', padding, width), true
	case 'm':
		return num(int64(f.Mon), 2, '0', padding, width), true
	case 'B':
		return text(monthNames[f.Mon-1], padding, width), true
	case 'b', 'h':
		return text(monthNames[f.Mon-1][:3], padding, width), true
	case 'd':
		return num(int64(f.Day), 2, '0', padding, width), true
	case 'e':
		return num(int64(f.Day), 2, ' ', padding, width), true
	case 'j':
		return num(int64(f.Yday), 3, '0', padding, width), true
	case 'H':
		return num(int64(f.Hour), 2, '0', padding, width), true
	case 'k':
		return num(int64(f.Hour), 2, ' ', padding, width), true
	case 'I':
		return num(int64(hour12(f.Hour)), 2, '0', padding, width), true
	case 'l':
		return num(int64(hour12(f.Hour)), 2, ' ', padding, width), true
	case 'P':
		if f.Hour < 12 {
			return text("am", padding, width), true
		}
		return text("pm", padding, width), true
	case 'p':
		s := "PM"
		if f.Hour < 12 {
			s = "AM"
		}
		return text(s, padding, width), true
	case 'M':
		return num(int64(f.Min), 2, '0', padding, width), true
	case 'S':
		return num(int64(f.Sec), 2, '0', padding, width), true
	case 'L', 'N':
		w := width
		if w < 0 {
			if d == 'L' {
				w = 3
			} else {
				w = 9
			}
		}
		s := strconv.FormatInt(f.Nsec, 10)
		s = strings.Repeat("0", 9-len(s)) + s
		if w <= 9 {
			return s[:w], true
		}
		return s + strings.Repeat("0", w-9), true
	case 'z':
		return offset(f.Offset, padding, width, colons), true
	case 'Z':
		if f.Zone == "" && f.UTC {
			return text("UTC", padding, width), true
		}
		return text(f.Zone, padding, width), true
	case 'A':
		return text(dayNames[f.Wday], padding, width), true
	case 'a':
		return text(dayNames[f.Wday][:3], padding, width), true
	case 'u':
		return num(int64(f.CWday), 1, '0', padding, width), true
	case 'w':
		return num(int64(f.Wday), 1, '0', padding, width), true
	case 'G':
		if f.CWYear < 0 {
			return num(f.CWYear, 5, '0', padding, width), true
		}
		return num(f.CWYear, 4, '0', padding, width), true
	case 'g':
		return num(mod(f.CWYear, 100), 2, '0', padding, width), true
	case 'V':
		return num(int64(f.CWeek), 2, '0', padding, width), true
	case 'U':
		return num(int64((f.Yday-1-f.Wday+7)/7), 2, '0', padding, width), true
	case 'W':
		return num(int64((f.Yday-1-(f.Wday+6)%7+7)/7), 2, '0', padding, width), true
	case 's':
		return num(f.Epoch, 1, '0', padding, width), true
	case 'Q':
		return num(f.EpochMilli, 1, '0', padding, width), true
	case 'n':
		return "\n", true
	case 't':
		return "\t", true
	case '%':
		return "%", true
	case 'c':
		return text(Format("%a %b %e %H:%M:%S %Y", f), padding, width), true
	case 'D', 'x':
		return text(Format("%m/%d/%y", f), padding, width), true
	case 'F':
		return text(Format("%Y-%m-%d", f), padding, width), true
	case 'v':
		*upcase = *upcase || false
		return text(Format("%e-%^b-%4Y", f), padding, width), true
	case 'X', 'T':
		return text(Format("%H:%M:%S", f), padding, width), true
	case 'r':
		return text(Format("%I:%M:%S %p", f), padding, width), true
	case 'R':
		return text(Format("%H:%M", f), padding, width), true
	case '+':
		return text(Format("%a %b %e %H:%M:%S %Z %Y", f), padding, width), true
	}
	return "", false
}

func offset(off int, padding byte, width, colons int) string {
	sign := byte('+')
	if off < 0 {
		sign = '-'
		off = -off
	}
	h, m, s := off/3600, off/60%60, off%60
	var body string
	switch colons {
	case 1:
		body = pad2(h) + ":" + pad2(m)
	case 2:
		body = pad2(h) + ":" + pad2(m) + ":" + pad2(s)
	case 3:
		switch {
		case s != 0:
			body = pad2(h) + ":" + pad2(m) + ":" + pad2(s)
		case m != 0:
			body = pad2(h) + ":" + pad2(m)
		default:
			body = pad2(h)
		}
	default:
		body = pad2(h) + pad2(m)
	}
	if padding == '-' {
		body = strings.TrimPrefix(body, "0")
		if body == "" || body[0] == ':' || len(body) < 2 {
			body = "0" + body
		}
	}
	s2 := string(sign) + body
	if width > len(s2) {
		fill := "0"
		if padding == '_' {
			fill = " "
			return strings.Repeat(fill, width-len(s2)) + s2
		}
		return string(sign) + strings.Repeat(fill, width-len(s2)) + body
	}
	return s2
}

func pad2(v int) string {
	if v < 10 {
		return "0" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

func hour12(h int) int {
	h %= 12
	if h == 0 {
		return 12
	}
	return h
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func mod(a, b int64) int64 {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}
