// Package bigdecimal implements Ruby's BigDecimal with math/big: a finite
// value is sign * coef * 10**exp (coef has no trailing zeros), plus NaN,
// +/-Infinity and -0.
package bigdecimal

import (
	"embed"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

// core.rb is the Ruby half of bigdecimal.so (the C extension in MRI); lib/
// is bigdecimal 4.1.3's Ruby library, unchanged (ruby/bigdecimal v4.1.3)
//
//go:embed core.rb
var coreRb []byte

//go:embed lib
var libFS embed.FS

const (
	kFinite = iota
	kNaN
	kInf
)

// Rounding modes (BigDecimal::ROUND_*)
const (
	roundUp       = 1
	roundDown     = 2
	roundHalfUp   = 3
	roundHalfDown = 4
	roundCeiling  = 5
	roundFloor    = 6
	roundHalfEven = 7
)

type decimal struct {
	kind int
	neg  bool
	coef *big.Int // >= 0
	exp  int64
}

var (
	bigTen  = big.NewInt(10)
	bigZero = big.NewInt(0)
)

func newFinite(neg bool, coef *big.Int, exp int64) *decimal {
	d := &decimal{kind: kFinite, neg: neg, coef: new(big.Int).Set(coef), exp: exp}
	d.normalize()
	return d
}

func (d *decimal) normalize() {
	if d.kind != kFinite {
		return
	}
	if d.coef.Sign() == 0 {
		d.exp = 0
		return
	}
	q, r := new(big.Int), new(big.Int)
	for {
		q.QuoRem(d.coef, bigTen, r)
		if r.Sign() != 0 {
			break
		}
		d.coef.Set(q)
		d.exp++
	}
}

func (d *decimal) isZero() bool { return d.kind == kFinite && d.coef.Sign() == 0 }

// digits returns number of decimal digits of coef
func (d *decimal) digits() int64 {
	if d.coef.Sign() == 0 {
		return 1
	}
	return int64(len(d.coef.String()))
}

// exponent in Ruby's sense: value = 0.xxx * 10**exponent
func (d *decimal) exponent() int64 {
	if d.kind != kFinite || d.isZero() {
		return 0
	}
	return satAdd(d.digits(), d.exp)
}

// The range of BigDecimal#exponent, as MRI's (its exponent counts BASE
// words in an intptr_t): above it a value overflows to Infinity, below it
// underflows to 0.
const (
	expMax = math.MaxInt64 / 9 * 9 // 9223372036854775800
	expMin = -9223372036854775799  // (INTPTR_MIN - 2) / 9 * 9 + 9 + 1, floored
)

// satAdd is a+b, saturating at the int64 bounds (beyond the exponent range
// either way, so an overflowing exponent is an over/underflow, not a wrap)
func satAdd(a, b int64) int64 {
	c := a + b
	if (c > a) != (b > 0) {
		if b > 0 {
			return math.MaxInt64
		}
		return math.MinInt64
	}
	return c
}

func pow10(n int64) *big.Int {
	return new(big.Int).Exp(bigTen, big.NewInt(n), nil)
}

// align returns coefficients of a and b at common exponent
func align(a, b *decimal) (*big.Int, *big.Int, int64) {
	ca, cb := new(big.Int).Set(a.coef), new(big.Int).Set(b.coef)
	e := a.exp
	if b.exp < e {
		e = b.exp
	}
	// a zero is zero at any exponent: never 10**(a huge difference) for it
	if a.coef.Sign() == 0 {
		e = b.exp
	} else if b.coef.Sign() == 0 {
		e = a.exp
	}
	if a.exp > e && a.coef.Sign() != 0 {
		ca.Mul(ca, pow10(a.exp-e))
	}
	if b.exp > e && b.coef.Sign() != 0 {
		cb.Mul(cb, pow10(b.exp-e))
	}
	if a.neg {
		ca.Neg(ca)
	}
	if b.neg {
		cb.Neg(cb)
	}
	return ca, cb, e
}

func fromSigned(v *big.Int, exp int64, negZero bool) *decimal {
	neg := v.Sign() < 0
	if v.Sign() == 0 {
		neg = negZero
	}
	return newFinite(neg, new(big.Int).Abs(v), exp)
}

func nan() *decimal          { return &decimal{kind: kNaN, coef: new(big.Int)} }
func inf(neg bool) *decimal  { return &decimal{kind: kInf, neg: neg, coef: new(big.Int)} }
func zero(neg bool) *decimal { return &decimal{kind: kFinite, neg: neg, coef: new(big.Int)} }
func (d *decimal) sign() int {
	if d.neg {
		return -1
	}
	return 1
}

func add(a, b *decimal) *decimal {
	if a.kind == kNaN || b.kind == kNaN {
		return nan()
	}
	if a.kind == kInf && b.kind == kInf {
		if a.neg != b.neg {
			return nan()
		}
		return inf(a.neg)
	}
	if a.kind == kInf {
		return inf(a.neg)
	}
	if b.kind == kInf {
		return inf(b.neg)
	}
	ca, cb, e := align(a, b)
	sum := new(big.Int).Add(ca, cb)
	return fromSigned(sum, e, a.neg && b.neg)
}

// sticky is b where a+b is rounded to digits significant digits: b itself,
// or, when |b| lies below every digit that rounding reads (bigdecimal 4.1's
// huge exponent differences), a same-signed stand-in just below them, which
// rounds the sum the same way without aligning to b's exponent
func sticky(a, b *decimal, digits int64) *decimal {
	if digits <= 0 || a.kind != kFinite || b.kind != kFinite || a.isZero() || b.isZero() {
		return b
	}
	// a is a multiple of 10**(t+1) and every rounding boundary of the sum a
	// multiple of 10**t: the open interval (a, a+-10**t) rounds as one
	t := a.exp + a.digits() - 1 - digits - 2
	if a.exp < t {
		t = a.exp
	}
	t--
	if b.exp+b.digits() <= t { // |b| < 10**t
		return &decimal{kind: kFinite, neg: b.neg, coef: big.NewInt(5), exp: t - 1}
	}
	return b
}

// addPrec is a+b for rounding to digits significant digits
func addPrec(a, b *decimal, digits int64) *decimal {
	if a.kind == kFinite && b.kind == kFinite && !a.isZero() && !b.isZero() {
		if a.exp+a.digits() >= b.exp+b.digits() {
			b = sticky(a, b, digits)
		} else {
			a = sticky(b, a, digits)
		}
	}
	return add(a, b)
}

func negate(a *decimal) *decimal {
	r := *a
	r.coef = new(big.Int).Set(a.coef)
	if a.kind != kNaN {
		r.neg = !a.neg
	}
	return &r
}

func mul(a, b *decimal) *decimal {
	if a.kind == kNaN || b.kind == kNaN {
		return nan()
	}
	if a.kind == kInf || b.kind == kInf {
		if a.isZero() || b.isZero() {
			return nan()
		}
		return inf(a.neg != b.neg)
	}
	if a.isZero() || b.isZero() {
		return zero(a.neg != b.neg)
	}
	// an exponent beyond int64 saturates: wrap makes it Infinity or 0 under
	// the exception modes
	return newFinite(a.neg != b.neg, new(big.Int).Mul(a.coef, b.coef), satAdd(a.exp, b.exp))
}

// words counts base 10**9 words of internal representation (precision rules)
func (d *decimal) words() int64 {
	if d.kind != kFinite || d.isZero() {
		return 1
	}
	e := d.exponent()
	intDigits, fracDigits := e, d.digits()-e
	if intDigits < 0 {
		fracDigits = d.digits() - e
		intDigits = 0
	}
	if fracDigits < 0 {
		fracDigits = 0
	}
	w := (intDigits+8)/9 + (fracDigits+8)/9
	if w == 0 {
		w = 1
	}
	return w
}

// divPrec is default precision (significant digits) of a / b
func divPrec(a, b *decimal) int64 {
	mx := (a.words() + b.words()) * 9
	if mx < 32 {
		mx = 32
	}
	return mx
}

// div computes a / b rounded to prec significant digits
func div(a, b *decimal, prec int64, mode int) *decimal {
	if a.kind == kNaN || b.kind == kNaN {
		return nan()
	}
	if a.kind == kInf {
		if b.kind == kInf {
			return nan()
		}
		return inf(a.neg != b.neg)
	}
	if b.kind == kInf {
		return zero(a.neg != b.neg)
	}
	if b.isZero() {
		if a.isZero() {
			return nan()
		}
		return inf(a.neg != b.neg)
	}
	if a.isZero() {
		return zero(a.neg != b.neg)
	}
	// scale numerator so quotient has prec+2 digits
	shift := prec + 2 + int64(len(b.coef.String())) - int64(len(a.coef.String()))
	if shift < 0 {
		shift = 0
	}
	num := new(big.Int).Mul(a.coef, pow10(shift))
	q, r := new(big.Int).QuoRem(num, b.coef, new(big.Int))
	if r.Sign() != 0 {
		// sticky digit for correct rounding
		q.Mul(q, bigTen)
		q.Add(q, big.NewInt(1))
		shift++
	}
	res := newFinite(a.neg != b.neg, q, satAdd(satAdd(a.exp, -b.exp), -shift))
	return roundSig(res, prec, mode)
}

// roundSig rounds to n significant digits
func roundSig(d *decimal, n int64, mode int) *decimal {
	if d.kind != kFinite || d.isZero() || n <= 0 {
		return d
	}
	digits := d.digits()
	if digits <= n {
		return d
	}
	return roundAt(d, d.exponent()-n, mode)
}

// roundPos rounds to pos digits after decimal point (pos may be negative)
func roundPos(d *decimal, pos int64, mode int) *decimal {
	return roundAt(d, -pos, mode)
}

// roundAt rounds so that resulting exponent (of coef) is >= e
func roundAt(d *decimal, e int64, mode int) *decimal {
	if d.kind != kFinite || d.isZero() || d.exp >= e {
		return d
	}
	drop := e - d.exp
	var q, r, div *big.Int
	cmp := -1 // the remainder against half of what is dropped
	if drop > d.digits()+1 {
		// all of coef dropped, and it is below half of 10**drop
		q, r = new(big.Int), new(big.Int).Set(d.coef)
	} else {
		div = pow10(drop)
		q, r = new(big.Int).QuoRem(d.coef, div, new(big.Int))
		if r.Sign() != 0 {
			cmp = new(big.Int).Mul(r, big.NewInt(2)).Cmp(div)
		}
	}
	inc := false
	if r.Sign() != 0 {
		switch mode {
		case roundUp:
			inc = true
		case roundDown:
		case roundHalfUp:
			inc = cmp >= 0
		case roundHalfDown:
			inc = cmp > 0
		case roundHalfEven:
			inc = cmp > 0 || (cmp == 0 && q.Bit(0) == 1)
		case roundCeiling:
			inc = !d.neg
		case roundFloor:
			inc = d.neg
		}
	}
	if inc {
		q.Add(q, big.NewInt(1))
	}
	if q.Sign() == 0 {
		return zero(d.neg)
	}
	return newFinite(d.neg, q, e)
}

func cmp(a, b *decimal) (int, bool) {
	if a.kind == kNaN || b.kind == kNaN {
		return 0, false
	}
	if a.kind == kInf || b.kind == kInf {
		av, bv := 0, 0
		if a.kind == kInf {
			av = a.sign()
		}
		if b.kind == kInf {
			bv = b.sign()
		}
		if av == bv {
			if av == 0 {
				return 0, true
			}
			return 0, true
		}
		if av < bv {
			return -1, true
		}
		if av > bv {
			return 1, true
		}
	}
	// different signs, or leading digits at different positions, decide
	// without aligning (10**(a huge exponent difference) otherwise)
	sa, sb := a.sign(), b.sign()
	if a.isZero() {
		sa = 0
	}
	if b.isZero() {
		sb = 0
	}
	if sa != sb {
		if sa < sb {
			return -1, true
		}
		return 1, true
	}
	if sa == 0 {
		return 0, true
	}
	if ta, tb := a.exp+a.digits(), b.exp+b.digits(); ta != tb {
		r := 1
		if ta < tb {
			r = -1
		}
		return r * sa, true
	}
	ca, cb, _ := align(a, b)
	return ca.Cmp(cb), true
}

func sqrt(d *decimal, n int64) *decimal {
	if d.kind == kNaN || (d.neg && !d.isZero()) {
		return nan()
	}
	if d.kind == kInf {
		return inf(false)
	}
	if d.isZero() {
		return zero(false)
	}
	prec := n + 2
	// scale so that coef*10**(2k) has ~2*prec digits and even exponent
	e := d.exp
	c := new(big.Int).Set(d.coef)
	need := 2*prec - int64(len(c.String()))
	if need < 0 {
		need = 0
	}
	if (e-need)%2 != 0 {
		need++
	}
	c.Mul(c, pow10(need))
	e -= need
	s := new(big.Int).Sqrt(c)
	return roundSig(newFinite(false, s, e/2), n, roundDown)
}

// -- parsing ---------------------------------------------------------------

var reDecimal = regexp.MustCompile(`\A\s*([-+])?(\d+(?:_\d+)*)?(?:\.(\d+(?:_\d+)*)?)?(?:[eEdD]([-+]?\d+(?:_\d+)*))?\s*\z`)

// an integer may end in one underscore, with nothing after it ("1_1_1_")
var reTrailingUnderscore = regexp.MustCompile(`\A(\s*[-+]?\d+(?:_\d+)*)_(\s*)\z`)

func parse(s string, strict bool) (*decimal, bool) {
	t := strings.TrimSpace(s)
	switch t {
	case "NaN":
		return nan(), true
	case "Infinity", "+Infinity":
		return inf(false), true
	case "-Infinity":
		return inf(true), true
	}
	if t := reTrailingUnderscore.FindStringSubmatch(s); t != nil {
		s = t[1] + t[2]
	}
	m := reDecimal.FindStringSubmatch(s)
	if m == nil || (m[2] == "" && m[3] == "") {
		if strict {
			return nil, false
		}
		// lenient (String#to_d): longest valid prefix
		re := regexp.MustCompile(`\A\s*[-+]?(?:\d+(?:_\d+)*)?(?:\.\d+(?:_\d+)*)?(?:[eE][-+]?\d+)?`)
		p := re.FindString(s)
		if p == "" {
			return zero(false), true
		}
		d, ok := parse(p, true)
		if !ok {
			return zero(false), true
		}
		return d, true
	}
	intPart := strings.ReplaceAll(m[2], "_", "")
	frac := strings.ReplaceAll(m[3], "_", "")
	var exp int64
	if m[4] != "" {
		v, err := strconv.ParseInt(strings.ReplaceAll(m[4], "_", ""), 10, 64)
		if err != nil { // beyond int64: saturated, for wrap's range check
			v = math.MaxInt64
			if strings.HasPrefix(m[4], "-") {
				v = math.MinInt64
			}
		}
		exp = v
	}
	coef, ok := new(big.Int).SetString(intPart+frac, 10)
	if !ok {
		coef = new(big.Int)
	}
	return newFinite(m[1] == "-", coef, satAdd(exp, -int64(len(frac)))), true
}

// -- formatting ------------------------------------------------------------

func groupDigits(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i += n {
		if i > 0 {
			b.WriteByte(' ')
		}
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(s[i:end])
	}
	return b.String()
}

// toS formats like BigDecimal#to_s(fmt)
func (d *decimal) toS(fmt string) string {
	plus := ""
	if strings.HasPrefix(fmt, "+") {
		plus = "+"
		fmt = fmt[1:]
	} else if strings.HasPrefix(fmt, " ") {
		plus = " "
		fmt = fmt[1:]
	}
	floatFmt := strings.HasSuffix(strings.ToUpper(fmt), "F")
	fmt = strings.TrimRight(fmt, "FfEe")
	group, _ := strconv.Atoi(fmt)

	switch d.kind {
	case kNaN:
		return "NaN"
	case kInf:
		if d.neg {
			return "-Infinity"
		}
		return plus + "Infinity"
	}
	sign := plus
	if d.neg {
		sign = "-"
	}
	if d.isZero() {
		if floatFmt {
			return sign + "0.0"
		}
		return sign + "0.0"
	}
	digits := d.coef.String()
	e := d.exponent()
	if !floatFmt {
		return sign + "0." + groupDigits(digits, group) + "e" + strconv.FormatInt(e, 10)
	}
	var ip, fp string
	switch {
	case e <= 0:
		ip = "0"
		fp = strings.Repeat("0", int(-e)) + digits
	case e >= int64(len(digits)):
		ip = digits + strings.Repeat("0", int(e-int64(len(digits))))
		fp = "0"
	default:
		ip = digits[:e]
		fp = digits[e:]
	}
	if group > 0 {
		// integer part grouped from the right, fraction from the left
		if r := len(ip) % group; r != 0 && len(ip) > group {
			ip = ip[:r] + " " + groupDigits(ip[r:], group)
		} else {
			ip = groupDigits(ip, group)
		}
		fp = groupDigits(fp, group)
	}
	return sign + ip + "." + fp
}

func (d *decimal) toFloat() float64 {
	switch d.kind {
	case kNaN:
		return math.NaN()
	case kInf:
		return math.Inf(d.sign())
	}
	if d.isZero() {
		if d.neg {
			return math.Copysign(0, -1)
		}
		return 0
	}
	f, err := strconv.ParseFloat(d.toS("E"), 64)
	if err != nil {
		if d.exponent() > 0 {
			return math.Inf(d.sign())
		}
		return math.Copysign(0, float64(d.sign()))
	}
	return f
}

// truncInt returns integer part
func (d *decimal) truncInt() *big.Int {
	if d.exp >= 0 {
		v := new(big.Int).Mul(d.coef, pow10(d.exp))
		if d.neg {
			v.Neg(v)
		}
		return v
	}
	if -d.exp >= d.digits() {
		return new(big.Int) // |d| < 1
	}
	v := new(big.Int).Quo(d.coef, pow10(-d.exp))
	if d.neg {
		v.Neg(v)
	}
	return v
}

// -- Ruby binding ----------------------------------------------------------

func init() {
	oruby.LazyGem("bigdecimal.so", func(mrb *oruby.MrbState) interface{} {
		cls := rb.DataClass(mrb.DefineClass("BigDecimal", mrb.ClassGet("Numeric")))
		cls.DefineClassMethod("__new", bdNew, mrb.ArgsReq(3))
		m := func(name string, n uint32, f oruby.MrbFuncT) { cls.DefineMethod(name, f, mrb.ArgsReq(n)) }
		m("__add", 1, binop(add))
		// __add_prec(other, digits, subtract)
		m("__add_prec", 3, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			a := mrb.GetArgs().Slice()
			b := get(mrb, a[0])
			if a[2].Bool() {
				b = negate(b)
			}
			return wrap(mrb, addPrec(get(mrb, self), b, a[1].Int64()))
		})
		m("__sub", 1, binop(func(a, b *decimal) *decimal { return add(a, negate(b)) }))
		m("__mul", 1, binop(mul))
		m("__div", 3, bdDiv)
		m("__cmp", 1, bdCmp)
		m("__round", 2, bdRound)
		m("__round_sig", 2, bdRoundSig)
		m("__sqrt", 1, bdSqrt)
		m("__to_s", 1, bdToS)
		m("__kind", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			k := []string{"finite", "nan", "infinity"}[d.kind]
			return mrb.StrNew(k)
		})
		m("__neg", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.BoolValue(get(mrb, self).neg)
		})
		m("__zero", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.BoolValue(get(mrb, self).isZero())
		})
		m("negate", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return wrap(mrb, negate(get(mrb, self)))
		})
		// x * 10**n, exactly (the library's Ruby scales with it)
		m("__decimal_shift", 1, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			if d.kind != kFinite || d.isZero() {
				return self
			}
			n, err := rb.Int(mrb, mrb.GetArgsFirst())
			if err != nil {
				return rb.TypeError(mrb, err)
			}
			return wrap(mrb, newFinite(d.neg, d.coef, d.exp+n))
		})
		m("exponent", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.FixnumValue(int(get(mrb, self).exponent()))
		})
		m("n_significant_digits", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			if d.kind != kFinite || d.isZero() {
				return mrb.FixnumValue(0)
			}
			return mrb.FixnumValue(int(d.digits()))
		})
		m("precision", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			if d.kind != kFinite || d.isZero() {
				return mrb.FixnumValue(0)
			}
			p := d.digits() + d.exp
			if d.exp < 0 {
				p = d.digits()
				if e := d.exponent(); e < 0 {
					p = d.digits() - e
				}
			}
			return mrb.FixnumValue(int(p))
		})
		m("scale", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			if d.kind != kFinite || d.isZero() || d.exp >= 0 {
				return mrb.FixnumValue(0)
			}
			return mrb.FixnumValue(int(-d.exp))
		})
		m("__digits", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			if d.kind != kFinite || d.isZero() {
				return mrb.StrNew("0")
			}
			return mrb.StrNew(d.coef.String())
		})
		m("to_f", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.FloatValue(get(mrb, self).toFloat())
		})
		m("__int_str", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			if d.kind != kFinite {
				return mrb.NilValue()
			}
			return mrb.StrNew(d.truncInt().String())
		})
		m("__rational", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			num := new(big.Int).Set(d.coef)
			den := big.NewInt(1)
			if d.exp >= 0 {
				num.Mul(num, pow10(d.exp))
			} else {
				den = pow10(-d.exp)
			}
			if d.neg {
				num.Neg(num)
			}
			return mrb.Value([]string{num.String(), den.String()})
		})
		m("hash", 0, func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			d := get(mrb, self)
			h := int64(d.kind)*1000003 + d.exp*31
			for _, b := range d.coef.Bytes() {
				h = h*131 + int64(b)
			}
			if d.neg {
				h = -h
			}
			return mrb.FixnumValue(int(h))
		})
		cls.DefineClassMethod("__from_float", bdFromFloat, mrb.ArgsReq(2))
		cls.DefineClassMethod("__log", bdLog, mrb.ArgsReq(2))
		rb.LoadRuby(mrb, "<internal:bigdecimal.so>", coreRb)
		return nil
	})
	// the library: each file a feature of its own, as on the load path
	for _, f := range []string{"bigdecimal", "bigdecimal/util", "bigdecimal/math", "bigdecimal/math/erf",
		"bigdecimal/math/gamma", "bigdecimal/jacobian", "bigdecimal/ludcmp", "bigdecimal/newton"} {
		src, err := libFS.ReadFile("lib/" + f + ".rb")
		if err != nil {
			panic(err)
		}
		name := f
		oruby.LazyGem(name, func(mrb *oruby.MrbState) interface{} {
			if err := rb.LoadRubyE(mrb, "<internal:"+name+".rb>", src); err != nil {
				return &oruby.InitError{Err: err} // require raises it
			}
			return nil
		})
	}
}

func get(mrb *oruby.MrbState, v oruby.Value) *decimal {
	d, ok := rb.Get[*decimal](mrb, v)
	if !ok || d == nil {
		return nan()
	}
	return d
}

// wrap makes d a BigDecimal: one beyond the exponent range is Infinity or
// 0, a FloatDomainError where BigDecimal.mode has EXCEPTION_OVERFLOW or
// EXCEPTION_UNDERFLOW on. BigDecimal.limit is the Ruby side's (core.rb).
func wrap(mrb *oruby.MrbState, d *decimal) oruby.Value { return wrapMode(mrb, d, true) }

// wrapQuiet is wrap for a value read from a string: beyond the exponent
// range it is Infinity or 0, as MRI's, except that an overflow raises
// "exponent overflow" under EXCEPTION_OVERFLOW (an underflow never does)
func wrapQuiet(mrb *oruby.MrbState, d *decimal) oruby.Value { return wrapMode(mrb, d, false) }

func wrapMode(mrb *oruby.MrbState, d *decimal, raise bool) oruby.Value {
	if d.kind == kFinite && !d.isZero() {
		// an exponent that saturated (satAdd) is out of range however
		// its digits place it
		if e := d.exponent(); e > expMax || e < expMin || d.exp == math.MinInt64 || d.exp == math.MaxInt64 {
			over := e > expMax || d.exp == math.MaxInt64
			flag, msg := 0x04, "Exponent underflow"
			if over {
				flag, msg = 0x01, "Exponent overflow"
			}
			if !raise && over {
				raise, msg = true, "exponent overflow"
			}
			if raise {
				cls := mrb.ClassGet("BigDecimal")
				if v, err := mrb.Funcall(cls.Value(), mrb.Intern("__exc?"), mrb.FixnumValue(flag)); err == nil && v.Value().Bool() {
					return mrb.Raise(mrb.ClassGet("FloatDomainError"), msg).Value()
				}
			}
			if over {
				d = inf(d.neg)
			} else {
				d = zero(d.neg)
			}
		}
	}
	cls := mrb.ClassGet("BigDecimal")
	obj, err := mrb.ObjNew(cls)
	if err != nil {
		return mrb.NilValue()
	}
	rb.Set(mrb, obj.Value(), d)
	return mrb.ObjFreeze(obj.Value()) // every BigDecimal is frozen, as MRI's
}

func binop(f func(a, b *decimal) *decimal) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return wrap(mrb, f(get(mrb, self), get(mrb, mrb.GetArgsFirst())))
	}
}

// __new(value_string_or_int, prec, strict)
func bdNew(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	s, err := rb.Str(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	d, ok := parse(s, a[2].Bool())
	if !ok {
		return mrb.Raise(mrb.EArgumentError(), "invalid value for BigDecimal(): \""+s+"\"")
	}
	if p := a[1].Int64(); p > 0 {
		d = roundSig(d, p, roundHalfUp)
	}
	return wrapQuiet(mrb, d)
}

func bdDiv(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	x, y := get(mrb, self), get(mrb, a[0])
	prec := a[1].Int64()
	if prec <= 0 {
		prec = divPrec(x, y)
	}
	return wrap(mrb, div(x, y, prec, int(a[2].Int64())))
}

func bdCmp(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c, ok := cmp(get(mrb, self), get(mrb, mrb.GetArgsFirst()))
	if !ok {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(c)
}

func bdRound(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	return wrap(mrb, roundPos(get(mrb, self), a[0].Int64(), int(a[1].Int64())))
}

func bdRoundSig(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	return wrap(mrb, roundSig(get(mrb, self), a[0].Int64(), int(a[1].Int64())))
}

func bdSqrt(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	n := mrb.GetArgsFirst().Int64()
	d := get(mrb, self)
	if n <= 0 {
		n = d.digits() + 16
		if n < 16 {
			n = 16
		}
	}
	return wrap(mrb, sqrt(d, n))
}

func bdToS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := mrb.StrNew(get(mrb, self).toS(mrb.GetArgsFirst().String()))
	// US-ASCII as MRI's; the Encoding object skips Encoding.find by name
	enc := mrb.EncodingClass().ConstGet("US_ASCII")
	if _, err := mrb.Funcall(s, oruby.Sym_force_encoding, enc); err != nil {
		return mrb.RaiseError(err)
	}
	return s
}

func bdFromFloat(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	f := a[0].Float64()
	prec := a[1].Int64()
	if math.IsNaN(f) {
		return wrap(mrb, nan())
	}
	if math.IsInf(f, 0) {
		return wrap(mrb, inf(f < 0))
	}
	var s string
	if prec > 0 {
		s = strconv.FormatFloat(f, 'e', int(prec-1), 64)
	} else {
		s = strconv.FormatFloat(f, 'g', -1, 64)
	}
	d, ok := parse(s, true)
	if !ok {
		return mrb.Raise(mrb.EArgumentError(), "invalid value for BigDecimal()")
	}
	if prec == 0 && d.digits() > 16 {
		d = roundSig(d, 16, roundDown)
	} else if prec > 0 {
		d = roundSig(d, prec, roundHalfUp)
	}
	return wrap(mrb, d)
}

// bigFloatLnNear1 computes ln(z) for z near 1 using 2*(u + u^3/3 + u^5/5 + ...)
// where u = (z-1)/(z+1).
func bigFloatLnNear1(z *big.Float, precBits uint) *big.Float {
	one := new(big.Float).SetPrec(precBits).SetInt64(1)
	num := new(big.Float).SetPrec(precBits).Sub(z, one)
	den := new(big.Float).SetPrec(precBits).Add(z, one)
	u := new(big.Float).SetPrec(precBits).Quo(num, den)
	u2 := new(big.Float).SetPrec(precBits).Mul(u, u)
	sum := new(big.Float).SetPrec(precBits).Set(u)
	term := new(big.Float).SetPrec(precBits).Set(u)
	tmp := new(big.Float).SetPrec(precBits)
	kVal := new(big.Float).SetPrec(precBits)
	epsExp := -int(precBits) - 8
	for k := int64(3); ; k += 2 {
		term.Mul(term, u2)
		kVal.SetInt64(k)
		tmp.Quo(term, kVal)
		sum.Add(sum, tmp)
		if tmp.Sign() == 0 || tmp.MantExp(nil) < epsExp {
			break
		}
	}
	two := new(big.Float).SetPrec(precBits).SetInt64(2)
	return sum.Mul(sum, two)
}

func bdLog(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	d := get(mrb, a[0])
	prec := a[1].Int64()
	if prec <= 0 {
		return mrb.Raise(mrb.EArgumentError(), "Zero or negative precision for log")
	}
	if d.kind == kNaN || d.neg || d.isZero() {
		return rb.Raise(mrb, "Math::DomainError", "Domain error - log")
	}
	if d.kind == kInf {
		return wrap(mrb, inf(false))
	}
	workDigits := prec + 24
	precBits := uint(float64(workDigits)*3.4) + 128
	// Parse exact decimal into big.Float
	s := d.coef.String() + "e" + strconv.FormatInt(d.exp, 10)
	x, _, err := big.ParseFloat(s, 10, precBits, big.ToNearestEven)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), "invalid argument for log")
	}
	// Reduce x = m * 2^e with m in [0.5, 1.0)
	mant := new(big.Float).SetPrec(precBits)
	exp2 := x.MantExp(mant)
	// Adjust so mant is in [0.75, 1.5) for faster convergence
	if mant.Cmp(new(big.Float).SetPrec(precBits).SetFloat64(0.75)) < 0 {
		mant.Mul(mant, new(big.Float).SetPrec(precBits).SetInt64(2))
		exp2--
	}
	res := bigFloatLnNear1(mant, precBits)
	if exp2 != 0 {
		// ln(2) = ln(4/3) + ln(3/2)
		f43 := new(big.Float).SetPrec(precBits).Quo(
			new(big.Float).SetPrec(precBits).SetInt64(4),
			new(big.Float).SetPrec(precBits).SetInt64(3),
		)
		f32 := new(big.Float).SetPrec(precBits).Quo(
			new(big.Float).SetPrec(precBits).SetInt64(3),
			new(big.Float).SetPrec(precBits).SetInt64(2),
		)
		ln2 := new(big.Float).SetPrec(precBits).Add(bigFloatLnNear1(f43, precBits), bigFloatLnNear1(f32, precBits))
		eTerm := new(big.Float).SetPrec(precBits).Mul(ln2, new(big.Float).SetPrec(precBits).SetInt64(int64(exp2)))
		res.Add(res, eTerm)
	}
	outStr := res.Text('e', int(prec+10))
	outDec, ok := parse(outStr, true)
	if !ok {
		return wrap(mrb, nan())
	}
	return wrap(mrb, roundSig(outDec, prec+1, roundHalfUp))
}
