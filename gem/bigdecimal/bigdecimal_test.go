package bigdecimal_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/bigdecimal"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/regexp"
)

func evalBigDecimal(t *testing.T, code string) {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Eval(code); err != nil {
		t.Fatalf("ruby eval failed:\n%s\nerror: %v", code, err)
	}
}

func TestBigDecimalArithmeticAndFormatting(t *testing.T) {
	evalBigDecimal(t, `
require "bigdecimal"

a = BigDecimal("0.1")
b = BigDecimal("0.2")
raise "0.1 + 0.2 != 0.3" unless (a + b) == BigDecimal("0.3")
raise "to_s('F')" unless (a + b).to_s("F") == "0.3"

c = BigDecimal("12.345")
d = BigDecimal("2.5")
raise "mul" unless (c * d) == BigDecimal("30.8625")
raise "sub" unless (c - d) == BigDecimal("9.845")
raise "div" unless BigDecimal("10").div(BigDecimal("4"), 4) == BigDecimal("2.5")
raise "pow" unless (BigDecimal("1.5") ** 2) == BigDecimal("2.25")
raise "cmp" unless (BigDecimal("1.0") <=> BigDecimal("1")) == 0
raise "neg" unless (-BigDecimal("3.5")).to_s("F") == "-3.5"
raise "abs" unless BigDecimal("-3.5").abs == BigDecimal("3.5")
`)
}

func TestBigDecimalRoundingModes(t *testing.T) {
	evalBigDecimal(t, `
require "bigdecimal"

raise "half_up" unless BigDecimal("2.5").round(0, BigDecimal::ROUND_HALF_UP) == 3
raise "half_down" unless BigDecimal("2.5").round(0, BigDecimal::ROUND_HALF_DOWN) == 2
raise "half_even 2.5" unless BigDecimal("2.5").round(0, BigDecimal::ROUND_HALF_EVEN) == 2
raise "half_even 3.5" unless BigDecimal("3.5").round(0, BigDecimal::ROUND_HALF_EVEN) == 4
raise "down" unless BigDecimal("2.9").round(0, BigDecimal::ROUND_DOWN) == 2
raise "up" unless BigDecimal("2.1").round(0, BigDecimal::ROUND_UP) == 3
raise "ceiling" unless BigDecimal("-2.1").round(0, BigDecimal::ROUND_CEILING) == -2
raise "floor" unless BigDecimal("-2.1").round(0, BigDecimal::ROUND_FLOOR) == -3

BigDecimal.save_rounding_mode do
  BigDecimal.mode(BigDecimal::ROUND_MODE, :half_even)
  raise "saved mode" unless BigDecimal.mode(BigDecimal::ROUND_MODE) == BigDecimal::ROUND_HALF_EVEN
end
raise "restored mode" unless BigDecimal.mode(BigDecimal::ROUND_MODE) == BigDecimal::ROUND_HALF_UP
`)
}

func TestBigDecimalSpecialValuesAndLimit(t *testing.T) {
	evalBigDecimal(t, `
require "bigdecimal"

nan = BigDecimal("NaN")
inf = BigDecimal("Infinity")
ninf = BigDecimal("-Infinity")
zero = BigDecimal("0")

raise "nan?" unless nan.nan? && !nan.finite? && !nan.infinite?
raise "inf?" unless inf.infinite? == 1 && !inf.finite?
raise "ninf?" unless ninf.infinite? == -1 && !ninf.finite?
raise "zero?" unless zero.zero? && zero.finite?

BigDecimal.save_limit do
  BigDecimal.limit(10)
  raise "limit" unless BigDecimal.limit == 10
end
raise "limit restored" unless BigDecimal.limit == 0

s = BigDecimal("2").sqrt(10)
raise "sqrt" unless s.to_s("F").start_with?("1.41421356")
`)
}
