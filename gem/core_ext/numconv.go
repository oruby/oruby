package core_ext

import (
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

// Kernel.__oruby_str_to_f(str, raise): Float(str) as MRI's rb_cstr_to_dbl
// with badcheck: surrounding whitespace, a sign, then decimal digits with
// an optional fraction ("1." and ".5" too) and exponent, or 0x hex digits
// with an optional fraction and a decimal p exponent; an underscore only
// between digits. nil (or ArgumentError when raise) for anything else.
func initNumconv(mrb *oruby.MrbState) {
	mrb.DefineClassMethod(mrb.KernelModule(), "__oruby_str_to_f", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs()
		s := mrb.String(a.Item(0))
		raise := a.Item(1).Bool()
		if strings.IndexByte(s, 0) >= 0 {
			if raise {
				return mrb.EArgumentError().Raise("string for Float contains null byte")
			}
			return mrb.NilValue()
		}
		f, ok := strictFloat(s)
		if !ok {
			if raise {
				return mrb.EArgumentError().Raise("invalid value for Float(): " + mrb.String(mrb.Inspect(a.Item(0))))
			}
			return mrb.NilValue()
		}
		return mrb.FloatValue(f)
	}, mrb.ArgsReq(2))
}

func isSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// digits reads [0-9]+(_[0-9]+)* (hex: hexadecimal digits) from s at i,
// appending them to b; the end, or -1 for a misplaced underscore
func digits(s string, i int, hex bool, b *strings.Builder) (int, bool) {
	isDigit := func(c byte) bool {
		if c >= '0' && c <= '9' {
			return true
		}
		return hex && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
	}
	start := i
	for i < len(s) {
		c := s[i]
		if isDigit(c) {
			b.WriteByte(c)
			i++
			continue
		}
		if c == '_' {
			if i == start || i+1 >= len(s) || !isDigit(s[i+1]) {
				return -1, false
			}
			i++
			continue
		}
		break
	}
	return i, i > start
}

func strictFloat(s string) (float64, bool) {
	i, n := 0, len(s)
	for i < n && isSpace(s[i]) {
		i++
	}
	for n > i && isSpace(s[n-1]) {
		n--
	}
	s = s[i:n]
	if s == "" {
		return 0, false
	}
	var b strings.Builder
	i = 0
	if s[0] == '+' || s[0] == '-' {
		b.WriteByte(s[0])
		i++
	}
	if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') {
		b.WriteString("0x")
		i += 2
		j, intDigits := digits(s, i, true, &b)
		if j < 0 {
			return 0, false
		}
		i = j
		fracDigits := false
		if i < len(s) && s[i] == '.' {
			b.WriteByte('.')
			if j, fracDigits = digits(s, i+1, true, &b); j < 0 {
				return 0, false
			}
			i = j
		}
		if !intDigits && !fracDigits {
			return 0, false
		}
		exp := "p0"
		if i < len(s) && (s[i] == 'p' || s[i] == 'P') {
			var e strings.Builder
			e.WriteByte('p')
			i++
			if i < len(s) && (s[i] == '+' || s[i] == '-') {
				e.WriteByte(s[i])
				i++
			}
			j, ok := digits(s, i, false, &e)
			if j < 0 || !ok {
				return 0, false
			}
			i = j
			exp = e.String()
		}
		if i != len(s) {
			return 0, false
		}
		f, err := strconv.ParseFloat(b.String()+exp, 64)
		if err != nil && !isRange(err) {
			return 0, false
		}
		return f, true
	}
	j, intDigits := digits(s, i, false, &b)
	if j < 0 {
		return 0, false
	}
	i = j
	if i < len(s) && s[i] == '.' {
		// "1." is allowed (Ruby 3.4), ".5" too, "." alone not
		b.WriteByte('.')
		j, fracDigits := digits(s, i+1, false, &b)
		if j < 0 || (!intDigits && !fracDigits) {
			return 0, false
		}
		i = j
	} else if !intDigits {
		return 0, false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		b.WriteByte('e')
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			b.WriteByte(s[i])
			i++
		}
		j, ok := digits(s, i, false, &b)
		if j < 0 || !ok {
			return 0, false
		}
		i = j
	}
	if i != len(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(b.String(), 64)
	if err != nil && !isRange(err) {
		return 0, false
	}
	return f, true
}

// isRange: an overflow (Infinity) or underflow (0) is a value
func isRange(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}
