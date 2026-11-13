package bigint

import (
	"math/big"
	"math/bits"

	"github.com/oruby/oruby"
)

func bignumValue(mrb *oruby.MrbState, klass oruby.RClass, bi *RBigInt) (oruby.Value, error) {
	ret, err := klass.NewGoInstance(bi)
	if err != nil {
		return oruby.Nil, err
	}
	return ret, nil
}

func init() {
	oruby.Gem("bigint", func(mrb *oruby.MrbState) interface{} {
		numClass := mrb.ClassGet("Numeric")
		intClass := mrb.ClassGet("Integer")
		parentClass := intClass
		if parentClass.IsNil() {
			parentClass = numClass
		}

		bignumClass := mrb.DefineClass("Bignum", parentClass)
		bignumClass.RegisterGoClass(func() *RBigInt { return newBigInt() })
		bignumClass.Populate()

		mrb.DefineGlobalConst("Bignum", bignumClass)
		if !mrb.ConstDefined(mrb.ObjectClass(), mrb.Intern("BigInt")) {
			mrb.DefineGlobalConst("BigInt", bignumClass)
		}

		// Constructor for Bignum
		initBignum := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			arg := mrb.GetArgsFirst()
			var bi *RBigInt
			var err error
			if arg.IsNil() {
				bi = newBigInt()
			} else if mrb.ObjIsInstanceOf(arg, bignumClass) {
				data := mrb.Data(arg)
				if rbi, ok := data.(*RBigInt); ok {
					bi = newBigIntFromBig(rbi.Value())
				} else {
					bi = newBigInt()
				}
			} else if arg.Type() == oruby.MrbTTFixnum {
				bi = newBigIntFromInt64(int64(arg.Int()))
			} else if arg.Type() == oruby.MrbTTString {
				b, e := toBigInt(arg.String())
				if e != nil {
					return mrb.Raise(mrb.EArgumentError(), e.Error())
				}
				bi = newBigIntFromBig(b)
			} else if arg.Type() == oruby.MrbTTFloat {
				b, e := toBigInt(arg.Float64())
				if e != nil {
					return mrb.Raise(mrb.EArgumentError(), e.Error())
				}
				bi = newBigIntFromBig(b)
			} else {
				// try to_int or to_s
				if mrb.RespondTo(arg, mrb.Intern("to_int")) {
					iv := mrb.Call(arg, "to_int")
					if iv.Type() == oruby.MrbTTFixnum {
						bi = newBigIntFromInt64(int64(iv.Int()))
					} else {
						bi = newBigInt()
					}
				} else if mrb.RespondTo(arg, mrb.Intern("to_s")) {
					sv := mrb.Call(arg, "to_s")
					b, e := toBigInt(sv.String())
					if e != nil {
						return mrb.Raise(mrb.EArgumentError(), e.Error())
					}
					bi = newBigIntFromBig(b)
				} else {
					return mrb.Raise(mrb.ETypeError(), "can't convert into Bignum")
				}
			}

			ret, err := bignumValue(mrb, bignumClass, bi)
			if err != nil {
				return mrb.Raise(mrb.EArgumentError(), err.Error())
			}
			return ret
		}

		bignumClass.DefineClassMethod("new", initBignum, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("Bignum", initBignum, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("BigInt", initBignum, mrb.ArgsOpt(1))

		// Aliases
		bignumClass.DefineAlias("+", "plus_operator")
		bignumClass.DefineAlias("-", "minus_operator")
		bignumClass.DefineAlias("*", "multiply_operator")
		bignumClass.DefineAlias("/", "divide_operator")
		bignumClass.DefineAlias("div", "divide_operator")
		bignumClass.DefineAlias("%", "modulo_operator")
		bignumClass.DefineAlias("modulo", "modulo_operator")
		bignumClass.DefineAlias("**", "power_operator")
		bignumClass.DefineAlias("+@", "unary_plus_operator")
		bignumClass.DefineAlias("-@", "unary_minus_operator")
		bignumClass.DefineAlias("~", "invert_operator")
		bignumClass.DefineAlias("&", "bit_and_operator")
		bignumClass.DefineAlias("|", "bit_or_operator")
		bignumClass.DefineAlias("^", "bit_xor_operator")
		bignumClass.DefineAlias("<<", "left_shift_operator")
		bignumClass.DefineAlias(">>", "right_shift_operator")
		bignumClass.DefineAlias("[]", "bit")
		bignumClass.DefineAlias("<=>", "cmp")
		bignumClass.DefineAlias("==", "equal_operator")
		bignumClass.DefineAlias("===", "equal_operator")
		bignumClass.DefineAlias("<", "less_operator")
		bignumClass.DefineAlias("<=", "less_equal_operator")
		bignumClass.DefineAlias(">", "greater_operator")
		bignumClass.DefineAlias(">=", "greater_equal_operator")
		bignumClass.DefineAlias("magnitude", "abs")
		bignumClass.DefineAlias("even?", "is_even")
		bignumClass.DefineAlias("odd?", "is_odd")
		bignumClass.DefineAlias("zero?", "is_zero")
		bignumClass.DefineAlias("to_int", "to_i")

		// Coerce method: self.coerce(other) -> [Bignum(other), self]
		bignumClass.DefineMethod("coerce", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			other := mrb.GetArgsFirst()
			otherBig, err := toBigInt(mrb.Intf(other))
			if err != nil {
				return mrb.Raise(mrb.ETypeError(), "can't coerce into Bignum")
			}
			ov, err := bignumValue(mrb, bignumClass, newBigIntFromBig(otherBig))
			if err != nil {
				return mrb.Raise(mrb.EArgumentError(), err.Error())
			}
			ary := mrb.AryNew()
			ary.Push(ov)
			ary.Push(self)
			return ary
		}, mrb.ArgsReq(1))

		// Replace Fixnum operators with Bignum and overflow-supported ones
		cFixnum := mrb.FixnumClass()

		newFixnumOp := func(op, oldOp string) oruby.MrbFuncT {
			return func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
				arg := mrb.GetArgsFirst()

				// If other is Bignum, promote self to Bignum
				if mrb.ObjIsInstanceOf(arg, bignumClass) {
					selfBig := newBigIntFromInt64(int64(self.Int()))
					sv, _ := bignumValue(mrb, bignumClass, selfBig)
					return mrb.Call(sv, op, arg)
				}

				if op == "==" && arg.Type() != oruby.MrbTTFixnum && arg.Type() != oruby.MrbTTFloat {
					if mrb.RespondTo(arg, mrb.Intern("==")) {
						return mrb.Call(arg, "==", self)
					}
				}

				// Check integer overflow on basic arithmetic
				if arg.Type() == oruby.MrbTTFixnum {
					a := int64(self.Int())
					b := int64(arg.Int())
					overflow := false

					switch op {
					case "+":
						c := a + b
						if (c < a) != (b < 0) {
							overflow = true
						}
					case "-":
						c := a - b
						if (c > a) != (b < 0) {
							overflow = true
						}
					case "*":
						hi, _ := bits.Mul64(uint64(a), uint64(b))
						if a != 0 && b != 0 && hi != 0 {
							overflow = true
						}
					case "<<":
						if b >= 62 || b < 0 {
							overflow = true
						}
					case "**":
						if b > 0 {
							z := new(big.Int).Exp(big.NewInt(a), big.NewInt(b), nil)
							if !z.IsInt64() {
								overflow = true
							}
						}
					}

					if overflow {
						selfBig := newBigIntFromInt64(a)
						sv, _ := bignumValue(mrb, bignumClass, selfBig)
						return mrb.Call(sv, op, arg)
					}
				}

				return mrb.Call(self, oldOp, arg)
			}
		}

		for _, op := range []string{"+", "-", "*", "/", "**", "<=>", "==", "<", "<=", ">", ">=", "<<", ">>", "&", "|", "^"} {
			oldOp := "__orig_bint_" + op
			cFixnum.DefineAlias(oldOp, op)
			cFixnum.DefineMethod(op, newFixnumOp(op, oldOp), mrb.ArgsReq(1))
		}

		return nil
	})
}
