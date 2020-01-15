package complex

import (
	"github.com/oruby/oruby"
	"math"
)

func complexValue(klass oruby.RClass, r, i float64) (oruby.Value, error) {
	cpx := newComplex(r, i)
	ret, err := klass.NewGoInstance(cpx)
	if err != nil {
		return oruby.Nil, err
	}
	return ret, nil
}

func initPolar(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	absV, argV := mrb.GetArgs2(0.0, 0.0)
	abs := absV.Float64()
	arg := argV.Float64()
	cpc := mrb.ClassGet("Complex")

	ret, err := complexValue(cpc, abs*math.Cos(arg), abs*math.Sin(arg))
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return ret.Freeze()
}

func init() {
	oruby.Gem("complex", func(mrb *oruby.MrbState) interface{} {
		cpxClass := mrb.DefineClass("Complex", mrb.ClassGet("Numeric"))
		cpxClass.RegisterGoClass(new(RComplex))
		cpxClass.Populate()

		initComplex := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			r, i := mrb.GetArgs2(0.0, 0.0)
			ret, err := complexValue(cpxClass, r.Float64(), i.Float64())
			if err != nil {
				return mrb.Raise(mrb.EArgumentError(), err.Error())
			}
			return ret.Freeze()
		}

		mrb.DefineMethod(mrb.KernelModule(), "Complex", initComplex, mrb.ArgsArg(1, 1))
		cpxClass.DefineClassMethod("rectangular", initComplex, mrb.ArgsArg(1, 1))
		cpxClass.DefineClassMethod("rect", initComplex, mrb.ArgsArg(1, 1))
		cpxClass.DefineClassMethod("polar", initPolar, mrb.ArgsArg(1, 1))

		cpxClass.DefineAlias("__div__", "divide_with")
		cpxClass.DefineAlias("+@", "unary_plus_operator")
		cpxClass.DefineAlias("-@", "unary_minus_operator")
		cpxClass.DefineAlias("+", "plus_operator")
		cpxClass.DefineAlias("-", "minus_operator")
		cpxClass.DefineAlias("*", "multiply_operator")
		cpxClass.DefineAlias("/", "divide_operator")
		cpxClass.DefineAlias("quo", "divide_operator")
		cpxClass.DefineAlias("==", "equal_operator")

		cpxClass.DefineAlias("magnitude", "abs")
		cpxClass.DefineAlias("angle", "arg")
		cpxClass.DefineAlias("phase", "arg")
		cpxClass.DefineAlias("conj", "conjugate")
		cpxClass.DefineAlias("rect", "rectangular")
		cpxClass.DefineAlias("imag", "imaginary")

		// Replace NUmeric operators with complex suported ones
		cFixnum := mrb.FixnumClass()
		cFloat := mrb.FloatClass()
		newOperator := func(op, oldOp string) oruby.MrbFuncT {
			return func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
				arg := mrb.GetArgsFirst()
				if mrb.ObjIsInstanceOf(arg, cpxClass) {
					cpx := newComplex(self.Float64(), 0)
					v := mrb.GoValue(cpx).Freeze()
					return mrb.Call(v, op, arg).Value().Freeze()
				}
				return mrb.Call(self, oldOp, arg)
			}
		}

		for _, op := range []string{"+", "-", "*", "/", "=="} {
			oldOp := "__orig_" + op
			cFixnum.DefineAlias(oldOp, op)
			cFixnum.DefineMethod(op, newOperator(op, oldOp), mrb.ArgsReq(1))
			cFloat.DefineAlias(oldOp, op)
			cFloat.DefineMethod(op, newOperator(op, oldOp), mrb.ArgsReq(1))
		}
		return nil
	})
}
