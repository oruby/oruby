package core_ext

import (
	"math"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

// Float#round, #floor and #ceil with ndigits > 0 work on the shortest
// decimal representation of the Float (what #to_s prints), as MRI's results
// are: 2.675.round(2) is 2.68, though the double is 2.67499999... The digits
// are rounded as decimal digits and the result read back as a Float.
// 38_numeric_ext.rb checks the arguments and calls these.

func initFloatRound(mrb *oruby.MrbState) {
	fc := mrb.FloatClass()
	mrb.DefineMethod(fc, "__oruby_dec_round", floatDecRound, oruby.ArgsReq(2))
	mrb.DefineMethod(fc, "__oruby_dec_floor_ceil", floatDecFloorCeil, oruby.ArgsReq(2))
}

// shortestDigits: the significant digits of f's shortest representation and
// where the decimal point is in them (f = 0.digits × 10**dot)
func shortestDigits(f float64) (neg bool, digits string, dot int) {
	s := strconv.FormatFloat(f, 'e', -1, 64) // [-]d[.ddd]e±XX
	if s[0] == '-' {
		neg, s = true, s[1:]
	}
	e := strings.IndexByte(s, 'e')
	exp, _ := strconv.Atoi(s[e+1:])
	return neg, strings.Replace(s[:e], ".", "", 1), exp + 1
}

// decimalFloat is sign head × 10**-ndigits, correctly rounded
func decimalFloat(neg bool, head int64, ndigits int) float64 {
	s := strconv.FormatInt(head, 10) + "e-" + strconv.Itoa(ndigits)
	f, _ := strconv.ParseFloat(s, 64)
	if neg {
		return -f
	}
	return f
}

// Float#__oruby_dec_round(ndigits, half): ndigits > 0, a finite self; half
// is :up, :even or :down
func floatDecRound(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	ndigits := args.Item(0).Int()
	mode := mrb.SymString(args.Item(1).Symbol())
	f := self.Float64()
	neg, digits, dot := shortestDigits(f)
	keep := dot + ndigits
	if keep >= len(digits) {
		return self
	}
	if keep < 0 {
		return mrb.FloatValue(math.Copysign(0, f))
	}
	var head int64
	if keep > 0 {
		head, _ = strconv.ParseInt(digits[:keep], 10, 64)
	}
	next := digits[keep] - '0'
	tail := strings.TrimRight(digits[keep+1:], "0") != ""
	up := false
	switch {
	case next > 5:
		up = true
	case next < 5:
	case tail:
		up = true
	default:
		up = mode == "up" || (mode == "even" && head%2 == 1)
	}
	if up {
		head++
	}
	return mrb.FloatValue(decimalFloat(neg, head, ndigits))
}

// Float#__oruby_dec_floor_ceil(ndigits, ceil): ndigits > 0, a finite self
func floatDecFloorCeil(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	ndigits := args.Item(0).Int()
	ceil := args.Item(1).Bool()
	neg, digits, dot := shortestDigits(self.Float64())
	keep := dot + ndigits
	if keep >= len(digits) {
		return self
	}
	start := keep
	if start < 0 {
		start = 0
	}
	any := strings.TrimRight(digits[start:], "0") != ""
	var head int64
	if keep > 0 {
		head, _ = strconv.ParseInt(digits[:keep], 10, 64)
	}
	if any && ceil != neg {
		head++
	}
	if head == 0 {
		return mrb.FloatValue(0)
	}
	return mrb.FloatValue(decimalFloat(neg, head, ndigits))
}
