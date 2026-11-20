package core_ext

import (
	"math/big"

	"github.com/oruby/oruby"
)

// powMaxBits is the largest Integer#** result computed: MRI raises its
// ArgumentError past its own size limit (2 ** 40_000_000 is fine,
// 100_000_000 ** 1_000_000_000 is not)
const powMaxBits = 1 << 32

// initBigPow: Integer#__oruby_big_pow(exp), for a result past mruby's
// bigint pow limit (a million bits, RangeError), in Go's math/big
func initBigPow(mrb *oruby.MrbState) {
	mrb.DefineMethod(mrb.ClassGet("Integer"), "__oruby_big_pow", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		e := mrb.GetArgsFirst()
		if e.Type() != oruby.MrbTTFixnum || e.Int64() < 0 {
			return mrb.Raise(mrb.EArgumentError(), "exponent is too large")
		}
		exp := e.Int64()
		base, err := mrb.BigInt(self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if bl := int64(base.BitLen()); bl > 1 && (exp > powMaxBits/bl) {
			return mrb.Raise(mrb.EArgumentError(), "exponent is too large")
		}
		return mrb.BigIntValue(new(big.Int).Exp(base, big.NewInt(exp), nil))
	}, oruby.ArgsReq(1))
}
