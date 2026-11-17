package core_ext

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

// formatFloat renders |f| for sprintf conversion conv (e E f g G a A) with
// precision prec (-1: default) and the '#' flag; sign, width and padding are
// applied by the Ruby side (ruby/19_format.rb).
func formatFloat(conv byte, prec int, alt bool, f float64) string {
	f = math.Abs(f)
	switch conv {
	case 'a', 'A':
		s := strconv.FormatFloat(f, 'x', prec, 64) // 0x1.88p+07
		p := strings.IndexByte(s, 'p')
		mant, exp := s[:p], s[p+1:]
		sign := exp[0]
		exp = strings.TrimLeft(exp[1:], "0")
		if exp == "" {
			exp = "0"
		}
		if alt && !strings.Contains(mant, ".") {
			mant += "."
		}
		s = mant + "p" + string(sign) + exp
		if conv == 'A' {
			s = strings.ToUpper(s)
		}
		return s
	}
	if prec < 0 {
		prec = 6
	}
	verb := "%"
	if alt {
		verb += "#"
	}
	verb += ".*" + string(conv)
	return fmt.Sprintf(verb, prec, f)
}

func initFormat(mrb *oruby.MrbState) {
	mrb.DefineClassMethod(mrb.StringClass(), "__format_float", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		conv := mrb.String(args.Item(0))
		return mrb.StrNew(formatFloat(conv[0], args.Item(1).Int(), args.Item(2).Bool(), args.Item(3).Float64()))
	}, oruby.ArgsReq(4))
}
