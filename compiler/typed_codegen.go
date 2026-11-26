package compiler

import (
	"bytes"
	"fmt"
	"math"
	"strings"
)

// emitTypedSpecializedUnit emits a guarded pure-Go unboxed function
// (`func <fn>_typed(...) (<retGoType>, bool)`) supporting int64, float64,
// bool, Array[Integer]/Array[Float], RBS @ivars, Math.*, bitwise/power ops,
// Ruby floor-div/mod, and direct recursive / cross-method / Class.method calls.
//
// Leaf functions omit recursion depth tracking altogether; calling/recursive
// functions pass a local `_depth int` parameter (zero global memory traffic
// and Fiber/re-entrancy safe).
func (g *CodeGen) emitTypedSpecializedUnit(b *bytes.Buffer, plan *TypedPlan) {
	unit := plan.Unit
	inf := g.info[unit]
	fnName := plan.FnName
	jumpTargets := make(map[int]bool)
	for _, ins := range unit.Instructions {
		if ins.IsJump() && ins.Target >= 0 {
			jumpTargets[ins.Target] = true
		}
	}

	nregs := unit.NRegs
	if nregs < len(plan.Params)+2 {
		nregs = len(plan.Params) + 2
	}

	useI, useF, useB, useV := false, false, false, false
	markType := func(t PrimType) {
		switch t {
		case TypeInt64:
			useI = true
		case TypeFloat64:
			useF = true
		case TypeBool:
			useB = true
		case TypeArrayInt64, TypeArrayFloat64:
			useV = true
		}
	}
	for _, p := range plan.Params {
		markType(p)
	}
	for _, row := range plan.InTypes {
		for _, t := range row {
			markType(t)
		}
	}
	markType(plan.Return)

	var body bytes.Buffer
	retZero := "0"
	if plan.Return == TypeBool {
		retZero = "false"
	}
	bail := fmt.Sprintf("return %s, false", retZero)

	regExpr := func(reg uint32, t PrimType) string {
		switch t {
		case TypeInt64:
			useI = true
			return fmt.Sprintf("ri[%d]", reg)
		case TypeFloat64:
			useF = true
			return fmt.Sprintf("rf[%d]", reg)
		case TypeBool:
			useB = true
			return fmt.Sprintf("rb[%d]", reg)
		case TypeArrayInt64, TypeArrayFloat64:
			useV = true
			return fmt.Sprintf("rv[%d]", reg)
		default:
			return ""
		}
	}
	floatExpr := func(reg uint32, t PrimType) string {
		if t == TypeInt64 {
			useI = true
			return fmt.Sprintf("float64(ri[%d])", reg)
		}
		useF = true
		return fmt.Sprintf("rf[%d]", reg)
	}

	useOk := false
	hasReturn := false
	hoistIvars := !plan.HasCalls && len(plan.Ivars) > 0
	hoistAry := !plan.HasCalls && useV
	ivarSlot := make(map[string]string, len(plan.Ivars))
	ivarLocal := make(map[string]string, len(plan.Ivars))
	ivarDirty := make(map[string]string, len(plan.Ivars))
	for i, iv := range plan.Ivars {
		ivarSlot[iv] = fmt.Sprintf("_ix%d", i)
		ivarLocal[iv] = fmt.Sprintf("_iv%d", i)
		ivarDirty[iv] = fmt.Sprintf("_ivd%d", i)
	}
	emitFlushIvars := func(dst *bytes.Buffer) {
		if !hoistIvars || !plan.HasIvarWrites {
			return
		}
		for _, iv := range plan.Ivars {
			if !plan.IvarsWritten[iv] {
				continue
			}
			ivType := g.opts.RBS.LookupIvar(inf.className, iv)
			var valExpr string
			switch ivType {
			case TypeInt64:
				valExpr = fmt.Sprintf("oruby.AotFixnum(%s)", ivarLocal[iv])
			case TypeFloat64:
				valExpr = fmt.Sprintf("mrb.AotFloatValue(%s)", ivarLocal[iv])
			case TypeBool:
				valExpr = fmt.Sprintf("mrb.BoolValue(%s)", ivarLocal[iv])
			}
			fmt.Fprintf(dst, "\tif %s { mrb.AotShapedWriteUnchecked(_svals, %s, %s) }\n", ivarDirty[iv], ivarSlot[iv], valExpr)
		}
	}

	for idx := 0; idx < len(unit.Instructions); idx++ {
		ins := unit.Instructions[idx]
		if jumpTargets[ins.Offset] {
			fmt.Fprintf(&body, "L_%04d:\n", ins.Offset)
		}
		inRow := plan.InTypes[idx]
		if inRow == nil {
			continue
		}
		inConst := plan.InConsts[idx]
		typeOf := func(reg uint32) PrimType {
			if int(reg) < len(inRow) {
				return inRow[reg]
			}
			return TypeUnknown
		}
		constOf := func(reg uint32) string {
			if int(reg) < len(inConst) {
				return inConst[reg]
			}
			return ""
		}

		switch ins.Op {
		case OpNOP, OpGETCONST:
			// OpGETCONST is tracked statically in InConsts for Math.* / Class.method
		case OpENTER:
			for i, pt := range plan.Params {
				fmt.Fprintf(&body, "\t%s = _a%d\n", regExpr(uint32(i+1), pt), i+1)
				if hoistAry && (pt == TypeArrayInt64 || pt == TypeArrayFloat64) {
					fn := "AotArySlice"
					if plan.WritesAry {
						fn = "AotArySliceMutable"
					}
					fmt.Fprintf(&body, "\tif _sv[%d], _ok = mrb.%s(_a%d); !_ok { %s }\n", i+1, fn, i+1, bail)
					useOk = true
				}
			}
		case OpMOVE:
			if constOf(uint32(ins.B)) != "" {
				break
			}
			t := typeOf(uint32(ins.B))
			fmt.Fprintf(&body, "\t%s = %s\n", regExpr(ins.A, t), regExpr(uint32(ins.B), t))
			if hoistAry && (t == TypeArrayInt64 || t == TypeArrayFloat64) {
				fmt.Fprintf(&body, "\t_sv[%d] = _sv[%d]\n", ins.A, ins.B)
			}
		case OpLOADL:
			switch v := unit.Pool[ins.B].(type) {
			case int32:
				fmt.Fprintf(&body, "\t%s = %d\n", regExpr(ins.A, TypeInt64), v)
			case int64:
				fmt.Fprintf(&body, "\t%s = %d\n", regExpr(ins.A, TypeInt64), v)
			case float64:
				fmt.Fprintf(&body, "\t%s = %s\n", regExpr(ins.A, TypeFloat64), formatGoFloat(v))
			}
		case OpLOADI8:
			fmt.Fprintf(&body, "\t%s = %d\n", regExpr(ins.A, TypeInt64), ins.B)
		case OpLOADINEG:
			fmt.Fprintf(&body, "\t%s = -%d\n", regExpr(ins.A, TypeInt64), ins.B)
		case OpLOADI__1:
			fmt.Fprintf(&body, "\t%s = -1\n", regExpr(ins.A, TypeInt64))
		case OpLOADI_0, OpLOADI_1, OpLOADI_2, OpLOADI_3, OpLOADI_4, OpLOADI_5, OpLOADI_6, OpLOADI_7:
			fmt.Fprintf(&body, "\t%s = %d\n", regExpr(ins.A, TypeInt64), int(ins.Op-OpLOADI_0))
		case OpLOADI16:
			fmt.Fprintf(&body, "\t%s = %d\n", regExpr(ins.A, TypeInt64), int16(ins.B))
		case OpLOADI32:
			val := int32(uint32(ins.B)<<16 | uint32(ins.C))
			fmt.Fprintf(&body, "\t%s = %d\n", regExpr(ins.A, TypeInt64), val)
		case OpLOADTRUE:
			fmt.Fprintf(&body, "\t%s = true\n", regExpr(ins.A, TypeBool))
		case OpLOADFALSE:
			fmt.Fprintf(&body, "\t%s = false\n", regExpr(ins.A, TypeBool))
		case OpGETIV:
			ivName := unit.Syms[ins.B]
			ivType := g.opts.RBS.LookupIvar(inf.className, ivName)
			if hoistIvars {
				fmt.Fprintf(&body, "\t%s = %s\n", regExpr(ins.A, ivType), ivarLocal[ivName])
			} else {
				symVar := g.internSym(ivName)
				switch ivType {
				case TypeInt64:
					useI = true
					fmt.Fprintf(&body, "\tif _iv := mrb.AotIVGet(self, %s); _iv.Type() == oruby.MrbTTInteger { ri[%d] = aotInt(_iv) } else { %s }\n", symVar, ins.A, bail)
				case TypeFloat64:
					useF = true
					fmt.Fprintf(&body, "\tif _iv := mrb.AotIVGet(self, %s); _iv.Type() == oruby.MrbTTFloat { rf[%d] = oruby.AotFloat(_iv) } else { %s }\n", symVar, ins.A, bail)
				case TypeBool:
					useB = true
					fmt.Fprintf(&body, "\tif _iv := mrb.AotIVGet(self, %s); _iv.Type() == oruby.MrbTTTrue || (_iv.Type() == oruby.MrbTTFalse && !_iv.IsNil()) { rb[%d] = _iv.Type() == oruby.MrbTTTrue } else { %s }\n", symVar, ins.A, bail)
				}
			}
		case OpSETIV:
			ivName := unit.Syms[ins.B]
			ivType := g.opts.RBS.LookupIvar(inf.className, ivName)
			if hoistIvars {
				fmt.Fprintf(&body, "\t%s = %s\n\t%s = true\n", ivarLocal[ivName], regExpr(ins.A, ivType), ivarDirty[ivName])
			} else {
				symVar := g.internSym(ivName)
				switch ivType {
				case TypeInt64:
					useI = true
					fmt.Fprintf(&body, "\tif !mrb.AotIVSetImmediate(self, %s, oruby.AotFixnum(ri[%d])) { %s }\n", symVar, ins.A, bail)
				case TypeFloat64:
					useF = true
					fmt.Fprintf(&body, "\tif !mrb.AotIVSetImmediate(self, %s, mrb.AotFloatValue(rf[%d])) { %s }\n", symVar, ins.A, bail)
				case TypeBool:
					useB = true
					fmt.Fprintf(&body, "\tif !mrb.AotIVSetImmediate(self, %s, mrb.BoolValue(rb[%d])) { %s }\n", symVar, ins.A, bail)
				}
			}
		case OpGETIDX:
			ta := typeOf(ins.A)
			useI = true
			useV = true
			if hoistAry {
				fmt.Fprintf(&body, "\t{ _ai := int(ri[%d]); _s := _sv[%d]; if _ai < 0 { _ai += len(_s) }; if _ai < 0 || _ai >= len(_s) { %s }; _el := _s[_ai]; ", ins.A+1, ins.A, bail)
				if ta == TypeArrayInt64 {
					fmt.Fprintf(&body, "if _el.Type() != oruby.MrbTTInteger { %s }; ri[%d] = aotInt(_el) }\n", bail, ins.A)
				} else {
					useF = true
					fmt.Fprintf(&body, "if _el.Type() != oruby.MrbTTFloat { %s }; rf[%d] = oruby.AotFloat(_el) }\n", bail, ins.A)
				}
			} else if ta == TypeArrayInt64 {
				fmt.Fprintf(&body, "\tif _el, _eok := mrb.AotAryRefRaw(rv[%d], int(ri[%d])); _eok && _el.Type() == oruby.MrbTTInteger { ri[%d] = aotInt(_el) } else { %s }\n",
					ins.A, ins.A+1, ins.A, bail)
			} else {
				useF = true
				fmt.Fprintf(&body, "\tif _el, _eok := mrb.AotAryRefRaw(rv[%d], int(ri[%d])); _eok && _el.Type() == oruby.MrbTTFloat { rf[%d] = oruby.AotFloat(_el) } else { %s }\n",
					ins.A, ins.A+1, ins.A, bail)
			}
		case OpGETIDX0:
			tb := typeOf(uint32(ins.B))
			useV = true
			if hoistAry {
				fmt.Fprintf(&body, "\t{ _s := _sv[%d]; if len(_s) == 0 { %s }; _el := _s[0]; ", ins.B, bail)
				if tb == TypeArrayInt64 {
					useI = true
					fmt.Fprintf(&body, "if _el.Type() != oruby.MrbTTInteger { %s }; ri[%d] = aotInt(_el) }\n", bail, ins.A)
				} else {
					useF = true
					fmt.Fprintf(&body, "if _el.Type() != oruby.MrbTTFloat { %s }; rf[%d] = oruby.AotFloat(_el) }\n", bail, ins.A)
				}
			} else if tb == TypeArrayInt64 {
				useI = true
				fmt.Fprintf(&body, "\tif _el, _eok := mrb.AotAryRefRaw(rv[%d], 0); _eok && _el.Type() == oruby.MrbTTInteger { ri[%d] = aotInt(_el) } else { %s }\n",
					ins.B, ins.A, bail)
			} else {
				useF = true
				fmt.Fprintf(&body, "\tif _el, _eok := mrb.AotAryRefRaw(rv[%d], 0); _eok && _el.Type() == oruby.MrbTTFloat { rf[%d] = oruby.AotFloat(_el) } else { %s }\n",
					ins.B, ins.A, bail)
			}
		case OpSETIDX:
			ta := typeOf(ins.A)
			useI = true
			useV = true
			if hoistAry {
				fmt.Fprintf(&body, "\t{ _ai := int(ri[%d]); _s := _sv[%d]; if _ai < 0 { _ai += len(_s) }; if _ai < 0 || _ai >= len(_s) { %s }; ", ins.A+1, ins.A, bail)
			} else {
				fmt.Fprintf(&body, "\t{ _s, _sok := mrb.AotArySliceMutable(rv[%d]); if !_sok { %s }; _ai := int(ri[%d]); if _ai < 0 { _ai += len(_s) }; if _ai < 0 || _ai >= len(_s) { %s }; ", ins.A, bail, ins.A+1, bail)
			}
			if ta == TypeArrayInt64 {
				fmt.Fprintf(&body, "_v := ri[%d]; _s[_ai] = oruby.AotFixnum(_v); ri[%d] = _v }\n", ins.A+2, ins.A)
			} else {
				useF = true
				fmt.Fprintf(&body, "_v := rf[%d]; _s[_ai] = mrb.AotFloatValue(_v); rf[%d] = _v }\n", ins.A+2, ins.A)
			}
		case OpADD, OpSUB, OpMUL, OpDIV:
			ta, tb := typeOf(ins.A), typeOf(ins.A+1)
			if ta == TypeInt64 && tb == TypeInt64 {
				useI = true
				useOk = true
				fn := "aotIntAdd64"
				switch ins.Op {
				case OpSUB:
					fn = "aotIntSub64"
				case OpMUL:
					fn = "aotIntMul64"
				case OpDIV:
					fn = "aotIntDiv64"
				}
				fmt.Fprintf(&body, "\tri[%d], _ok = %s(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, fn, ins.A, ins.A+1, bail)
			} else {
				op := "+"
				switch ins.Op {
				case OpSUB:
					op = "-"
				case OpMUL:
					op = "*"
				case OpDIV:
					op = "/"
				}
				fmt.Fprintf(&body, "\t%s = %s %s %s\n", regExpr(ins.A, TypeFloat64), floatExpr(ins.A, ta), op, floatExpr(ins.A+1, tb))
			}
		case OpADDI, OpADDILV, OpSUBI, OpSUBILV:
			imm := int64(ins.B)
			if ins.Op == OpADDILV || ins.Op == OpSUBILV {
				imm = int64(ins.C)
			}
			isSub := ins.Op == OpSUBI || ins.Op == OpSUBILV
			if typeOf(ins.A) == TypeFloat64 {
				op := "+"
				if isSub {
					op = "-"
				}
				fmt.Fprintf(&body, "\t%s = %s %s %d.0\n", regExpr(ins.A, TypeFloat64), regExpr(ins.A, TypeFloat64), op, imm)
			} else {
				useI = true
				useOk = true
				fn := "aotIntAdd64"
				if isSub {
					fn = "aotIntSub64"
				}
				fmt.Fprintf(&body, "\tri[%d], _ok = %s(ri[%d], %d); if !_ok { %s }\n", ins.A, fn, ins.A, imm, bail)
			}
		case OpEQ, OpLT, OpLE, OpGT, OpGE:
			ta, tb := typeOf(ins.A), typeOf(ins.A+1)
			opStr := cmpOpStr(ins.Op)
			var lhs, rhs string
			if ta == TypeInt64 && tb == TypeInt64 {
				lhs, rhs = regExpr(ins.A, TypeInt64), regExpr(ins.A+1, TypeInt64)
			} else if ta == TypeBool && tb == TypeBool {
				lhs, rhs = regExpr(ins.A, TypeBool), regExpr(ins.A+1, TypeBool)
			} else {
				lhs, rhs = floatExpr(ins.A, ta), floatExpr(ins.A+1, tb)
			}
			if idx+1 < len(unit.Instructions) {
				nx := unit.Instructions[idx+1]
				if !jumpTargets[nx.Offset] && (nx.Op == OpJMPNOT || nx.Op == OpJMPIF) && nx.A == ins.A {
					backPoll := ""
					if nx.Target <= nx.Offset {
						backPoll = fmt.Sprintf("if mrb.AotHasHook() { %s }; ", bail)
					}
					if nx.Op == OpJMPNOT {
						fmt.Fprintf(&body, "\tif !(%s %s %s) { %sgoto L_%04d }\n", lhs, opStr, rhs, backPoll, nx.Target)
					} else {
						fmt.Fprintf(&body, "\tif %s %s %s { %sgoto L_%04d }\n", lhs, opStr, rhs, backPoll, nx.Target)
					}
					idx++
					continue
				}
			}
			fmt.Fprintf(&body, "\t%s = %s %s %s\n", regExpr(ins.A, TypeBool), lhs, opStr, rhs)
		case OpJMP:
			if ins.Target <= ins.Offset {
				fmt.Fprintf(&body, "\tif mrb.AotHasHook() { %s }\n", bail)
			}
			fmt.Fprintf(&body, "\tgoto L_%04d\n", ins.Target)
		case OpJMPIF:
			backPoll := ""
			if ins.Target <= ins.Offset {
				backPoll = fmt.Sprintf("if mrb.AotHasHook() { %s }; ", bail)
			}
			fmt.Fprintf(&body, "\tif %s { %sgoto L_%04d }\n", regExpr(ins.A, TypeBool), backPoll, ins.Target)
		case OpJMPNOT:
			backPoll := ""
			if ins.Target <= ins.Offset {
				backPoll = fmt.Sprintf("if mrb.AotHasHook() { %s }; ", bail)
			}
			fmt.Fprintf(&body, "\tif !%s { %sgoto L_%04d }\n", regExpr(ins.A, TypeBool), backPoll, ins.Target)
		case OpSEND, OpSEND0:
			sym := unit.Syms[ins.B]
			n := 0
			if ins.Op == OpSEND {
				n = int(ins.C & 0xf)
			}
			// Constant receiver: Math.<op> or Class.singleton_method
			if cname := constOf(ins.A); cname != "" {
				if cname == "Math" {
					spec := typedMathOps[sym]
					useF = true
					if spec.arity == 1 {
						arg0 := floatExpr(ins.A+1, typeOf(ins.A+1))
						if spec.canFail {
							useOk = true
							fmt.Fprintf(&body, "\trf[%d], _ok = %s(%s); if !_ok { %s }\n", ins.A, spec.helper, arg0, bail)
						} else {
							fmt.Fprintf(&body, "\trf[%d] = %s(%s)\n", ins.A, spec.helper, arg0)
						}
					} else {
						arg0 := floatExpr(ins.A+1, typeOf(ins.A+1))
						arg1 := floatExpr(ins.A+2, typeOf(ins.A+2))
						fmt.Fprintf(&body, "\trf[%d] = %s(%s, %s)\n", ins.A, spec.helper, arg0, arg1)
					}
					break
				}
				target, _ := g.lookupSingletonCallTarget(cname, sym, n)
				targetFn := g.info[target].fn
				cplan := g.typedPlans[targetFn]
				args := []string{"mrb"}
				if cplan.HasCalls {
					args = append(args, "_depth+1")
				}
				for i := 0; i < n; i++ {
					args = append(args, regExpr(ins.A+uint32(i+1), cplan.Params[i]))
				}
				useOk = true
				fmt.Fprintf(&body, "\tif %s, _ok = %s_typed(%s); !_ok {\n\t\t%s\n\t}\n",
					regExpr(ins.A, cplan.Return), targetFn, strings.Join(args, ", "), bail)
				break
			}

			ta := typeOf(ins.A)
			if ta == TypeArrayInt64 || ta == TypeArrayFloat64 {
				useI = true
				useV = true
				if hoistAry {
					fmt.Fprintf(&body, "\tri[%d] = int64(len(_sv[%d]))\n", ins.A, ins.A)
				} else {
					fmt.Fprintf(&body, "\tri[%d] = int64(mrb.AotAryLen(rv[%d]))\n", ins.A, ins.A)
				}
				break
			}

			switch sym {
			case "%":
				tb := typeOf(ins.A + 1)
				if ta == TypeInt64 && tb == TypeInt64 {
					useI = true
					useOk = true
					fmt.Fprintf(&body, "\tri[%d], _ok = aotIntMod64(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, ins.A, ins.A+1, bail)
				} else {
					useF = true
					useOk = true
					fmt.Fprintf(&body, "\trf[%d], _ok = aotFloatMod64(%s, %s); if !_ok { %s }\n", ins.A, floatExpr(ins.A, ta), floatExpr(ins.A+1, tb), bail)
				}
			case "**":
				tb := typeOf(ins.A + 1)
				if ta == TypeInt64 && tb == TypeInt64 {
					useI = true
					useOk = true
					fmt.Fprintf(&body, "\tri[%d], _ok = aotIntPow64(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, ins.A, ins.A+1, bail)
				} else {
					useF = true
					useOk = true
					fmt.Fprintf(&body, "\trf[%d], _ok = aotFloatPow(%s, %s); if !_ok { %s }\n", ins.A, floatExpr(ins.A, ta), floatExpr(ins.A+1, tb), bail)
				}
			case "&", "|", "^":
				useI = true
				fmt.Fprintf(&body, "\tri[%d] = ri[%d] %s ri[%d]\n", ins.A, ins.A, sym, ins.A+1)
			case "<<":
				useI = true
				useOk = true
				fmt.Fprintf(&body, "\tri[%d], _ok = aotIntShl64(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, ins.A, ins.A+1, bail)
			case ">>":
				useI = true
				useOk = true
				fmt.Fprintf(&body, "\tri[%d], _ok = aotIntShr64(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, ins.A, ins.A+1, bail)
			case "~":
				useI = true
				fmt.Fprintf(&body, "\tri[%d] = ^ri[%d]\n", ins.A, ins.A)
			case "!=":
				tb := typeOf(ins.A + 1)
				if ta == TypeInt64 && tb == TypeInt64 {
					fmt.Fprintf(&body, "\t%s = %s != %s\n", regExpr(ins.A, TypeBool), regExpr(ins.A, TypeInt64), regExpr(ins.A+1, TypeInt64))
				} else if ta == TypeBool && tb == TypeBool {
					fmt.Fprintf(&body, "\t%s = %s != %s\n", regExpr(ins.A, TypeBool), regExpr(ins.A, TypeBool), regExpr(ins.A+1, TypeBool))
				} else {
					fmt.Fprintf(&body, "\t%s = %s != %s\n", regExpr(ins.A, TypeBool), floatExpr(ins.A, ta), floatExpr(ins.A+1, tb))
				}
			case "-@":
				if ta == TypeInt64 {
					useI = true
					fmt.Fprintf(&body, "\tif ri[%d] == aotMinInt { %s }; ri[%d] = -ri[%d]\n", ins.A, bail, ins.A, ins.A)
				} else {
					fmt.Fprintf(&body, "\t%s = -%s\n", regExpr(ins.A, TypeFloat64), regExpr(ins.A, TypeFloat64))
				}
			case "+@":
			case "abs":
				if ta == TypeInt64 {
					useI = true
					fmt.Fprintf(&body, "\tif ri[%d] == aotMinInt { %s }; if ri[%d] < 0 { ri[%d] = -ri[%d] }\n", ins.A, bail, ins.A, ins.A, ins.A)
				} else {
					useF = true
					fmt.Fprintf(&body, "\tif rf[%d] < 0 { rf[%d] = -rf[%d] }\n", ins.A, ins.A, ins.A)
				}
			case "zero?":
				fmt.Fprintf(&body, "\t%s = %s == 0\n", regExpr(ins.A, TypeBool), regExpr(ins.A, ta))
			case "positive?":
				fmt.Fprintf(&body, "\t%s = %s > 0\n", regExpr(ins.A, TypeBool), regExpr(ins.A, ta))
			case "negative?":
				fmt.Fprintf(&body, "\t%s = %s < 0\n", regExpr(ins.A, TypeBool), regExpr(ins.A, ta))
			case "even?":
				useI = true
				fmt.Fprintf(&body, "\t%s = (ri[%d] & 1) == 0\n", regExpr(ins.A, TypeBool), ins.A)
			case "odd?":
				useI = true
				fmt.Fprintf(&body, "\t%s = (ri[%d] & 1) != 0\n", regExpr(ins.A, TypeBool), ins.A)
			case "floor", "ceil", "round":
				if ta == TypeFloat64 {
					useI = true
					useF = true
					useOk = true
					hfn := "aotFloatFloor"
					if sym == "ceil" {
						hfn = "aotFloatCeil"
					} else if sym == "round" {
						hfn = "aotFloatRound"
					}
					fmt.Fprintf(&body, "\tri[%d], _ok = %s(rf[%d]); if !_ok { %s }\n", ins.A, hfn, ins.A, bail)
				}
			case "to_f":
				fmt.Fprintf(&body, "\t%s = %s\n", regExpr(ins.A, TypeFloat64), floatExpr(ins.A, ta))
			case "to_i":
				if ta == TypeFloat64 {
					useI = true
					useF = true
					fmt.Fprintf(&body, "\tif rf[%d] != rf[%d] || rf[%d] < -9223372036854775808.0 || rf[%d] >= 9223372036854775808.0 { %s }; ri[%d] = int64(rf[%d])\n",
						ins.A, ins.A, ins.A, ins.A, bail, ins.A, ins.A)
				}
			}
		case OpSSEND, OpSSEND0:
			sym := unit.Syms[ins.B]
			n := 0
			if ins.Op == OpSSEND {
				n = int(ins.C & 0xf)
			}
			target, _ := g.lookupCallTarget(inf, sym, n)
			targetFn := fnName
			cplan := plan
			if target != nil && target != unit {
				targetFn = g.info[target].fn
				cplan = g.typedPlans[targetFn]
			}
			args := []string{"mrb"}
			if cplan.NeedsSelf {
				args = append(args, "self")
			}
			if cplan.HasCalls {
				args = append(args, "_depth+1")
			}
			for i := 0; i < n; i++ {
				args = append(args, regExpr(ins.A+uint32(i+1), cplan.Params[i]))
			}
			useOk = true
			fmt.Fprintf(&body, "\tif %s, _ok = %s_typed(%s); !_ok {\n\t\t%s\n\t}\n",
				regExpr(ins.A, cplan.Return), targetFn, strings.Join(args, ", "), bail)
		case OpRETURN:
			emitFlushIvars(&body)
			fmt.Fprintf(&body, "\treturn %s, true\n", regExpr(ins.A, plan.Return))
			hasReturn = true
		}
	}

	params := []string{"mrb *oruby.MrbState"}
	if plan.NeedsSelf {
		params = append(params, "self oruby.Value")
	}
	if plan.HasCalls {
		params = append(params, "_depth int")
	}
	for i, pt := range plan.Params {
		params = append(params, fmt.Sprintf("_a%d %s", i+1, pt.GoType()))
	}
	if plan.FromRBS {
		fmt.Fprintf(b, "// %s_typed is RBS-guided pure-Go specialization (%s)\n", fnName, plan.RBSNote)
	}
	fmt.Fprintf(b, "func %s_typed(%s) (%s, bool) {\n", fnName, strings.Join(params, ", "), plan.Return.GoType())
	if plan.HasCalls {
		fmt.Fprintf(b, "\tif _depth >= aotIntMaxDepth {\n\t\treturn %s, false\n\t}\n", retZero)
	}
	if useI {
		fmt.Fprintf(b, "\tvar ri [%d]int64\n\t_ = ri\n", nregs)
	}
	if useF {
		fmt.Fprintf(b, "\tvar rf [%d]float64\n\t_ = rf\n", nregs)
	}
	if useB {
		fmt.Fprintf(b, "\tvar rb [%d]bool\n\t_ = rb\n", nregs)
	}
	if useV {
		fmt.Fprintf(b, "\tvar rv [%d]oruby.Value\n\t_ = rv\n", nregs)
		if hoistAry {
			fmt.Fprintf(b, "\tvar _sv [%d][]oruby.Value\n\t_ = _sv\n", nregs)
		}
	}
	if useOk {
		b.WriteString("\tvar _ok bool\n\t_ = _ok\n")
	}
	if hoistIvars {
		baseFn := "AotShapedBase"
		if plan.HasIvarWrites {
			baseFn = "AotShapedBaseMutable"
		}
		b.WriteString(fmt.Sprintf("\t_svals, _sshape, _sok := mrb.%s(self)\n\tif !_sok { return %s, false }\n", baseFn, retZero))
		for i, iv := range plan.Ivars {
			symVar := g.internSym(iv)
			fmt.Fprintf(b, "\t_ix%d := mrb.AotShapedSlot(_sshape, %s)\n\tif _ix%d < 0 { return %s, false }\n", i, symVar, i, retZero)
			ivType := g.opts.RBS.LookupIvar(inf.className, iv)
			fmt.Fprintf(b, "\tvar _iv%d %s\n", i, ivType.GoType())
			if plan.IvarsRead[iv] {
				switch ivType {
				case TypeInt64:
					fmt.Fprintf(b, "\tif _v := mrb.AotShapedRead(_svals, _ix%d); _v.Type() == oruby.MrbTTInteger { _iv%d = aotInt(_v) } else { return %s, false }\n", i, i, retZero)
				case TypeFloat64:
					fmt.Fprintf(b, "\tif _v := mrb.AotShapedRead(_svals, _ix%d); _v.Type() == oruby.MrbTTFloat { _iv%d = oruby.AotFloat(_v) } else { return %s, false }\n", i, i, retZero)
				case TypeBool:
					fmt.Fprintf(b, "\tif _v := mrb.AotShapedRead(_svals, _ix%d); _v.Type() == oruby.MrbTTTrue || (_v.Type() == oruby.MrbTTFalse && !_v.IsNil()) { _iv%d = _v.Type() == oruby.MrbTTTrue } else { return %s, false }\n", i, i, retZero)
				}
			} else {
				fmt.Fprintf(b, "\t_ = _iv%d\n", i)
			}
			if plan.IvarsWritten[iv] {
				fmt.Fprintf(b, "\tvar _ivd%d bool\n", i)
			}
		}
	}
	b.Write(body.Bytes())
	if !hasReturn {
		emitFlushIvars(b)
		fmt.Fprintf(b, "\treturn %s, true\n", retZero)
	}
	b.WriteString("}\n\n")
}

// formatGoFloat renders a Go float64 literal preserving Inf/NaN safely.
func formatGoFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "aotFloatNaN()"
	case math.IsInf(v, 1):
		return "aotFloatInf(1)"
	case math.IsInf(v, -1):
		return "aotFloatInf(-1)"
	default:
		s := fmt.Sprintf("%g", v)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s
	}
}

// emitTypedEntryGuard writes the fast-path entry block inside `<fn>_fN` that
// verifies operator redefinition bits, compiled method identity (zero-cgo on
// warm cache), core/Math intrinsics, and runtime argument types before calling
// `<fn>_typed`.
func (g *CodeGen) emitTypedEntryGuard(out *bytes.Buffer, plan *TypedPlan) {
	var conds []string
	if plan.NeedsIntGuard {
		conds = append(conds, "mrb.AotIntCanRun()")
	}
	if plan.NeedsFloatGuard {
		conds = append(conds, "mrb.AotFloatCanRun()")
	}
	if !plan.NeedsIntGuard && !plan.NeedsFloatGuard {
		conds = append(conds, "mrb.AotIntCanRun()")
	}
	for _, cg := range plan.CoreGuards {
		conds = append(conds, fmt.Sprintf("mrb.AotCoreClassOK(%s)", cg))
	}
	if len(plan.MathGuards) > 0 {
		symMath := g.internSym("Math")
		for _, mg := range plan.MathGuards {
			conds = append(conds, fmt.Sprintf("mrb.AotCoreConstOK(cx, %s, %s)", symMath, mg))
		}
	}
	for _, gc := range plan.GuardCalls {
		symVar := g.internSym(gc.sym)
		slot := g.trackedSlots[gc.target]
		if gc.constClass != "" {
			symCls := g.internSym(gc.constClass)
			conds = append(conds, fmt.Sprintf("mrb.AotConstMethodOK(cx, %s, %s, %d)", symCls, symVar, slot))
		} else {
			conds = append(conds, fmt.Sprintf("mrb.AotCompiledMethodOK(self, %s, %d)", symVar, slot))
		}
	}

	callArgs := []string{"mrb"}
	if plan.NeedsSelf {
		callArgs = append(callArgs, "self")
	}
	if plan.HasCalls {
		callArgs = append(callArgs, "0")
	}
	for i, pt := range plan.Params {
		a := fmt.Sprintf("_a%d", i+1)
		switch pt {
		case TypeInt64:
			conds = append(conds, fmt.Sprintf("%s.Type() == oruby.MrbTTInteger", a))
			callArgs = append(callArgs, fmt.Sprintf("aotInt(%s)", a))
		case TypeFloat64:
			conds = append(conds, fmt.Sprintf("%s.Type() == oruby.MrbTTFloat", a))
			callArgs = append(callArgs, fmt.Sprintf("oruby.AotFloat(%s)", a))
		case TypeBool:
			conds = append(conds, fmt.Sprintf("(%s.Type() == oruby.MrbTTTrue || (%s.Type() == oruby.MrbTTFalse && !%s.IsNil()))", a, a, a))
			callArgs = append(callArgs, fmt.Sprintf("%s.Type() == oruby.MrbTTTrue", a))
		case TypeArrayInt64, TypeArrayFloat64:
			conds = append(conds, fmt.Sprintf("mrb.AotIsPlainArray(%s)", a))
			if len(plan.IdxGuards) > 0 {
				conds = append(conds, fmt.Sprintf("mrb.AotIdxOK(oruby.AotIdxAryAset, %s)", a))
			}
			callArgs = append(callArgs, a)
		}
	}
	retExpr := "oruby.AotFixnum(_v)"
	switch plan.Return {
	case TypeFloat64:
		retExpr = "mrb.AotFloatValue(_v)"
	case TypeBool:
		retExpr = "mrb.BoolValue(_v)"
	}
	fmt.Fprintf(out, "\tif %s {\n\t\tif _v, _ok := %s_typed(%s); _ok {\n\t\t\treturn %s, nil\n\t\t}\n\t\tmrb.AotIntDisable()\n\t\tdefer mrb.AotIntEnable()\n\t}\n",
		strings.Join(conds, " && "), plan.FnName, strings.Join(callArgs, ", "), retExpr)
}
