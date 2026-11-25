package compiler

import (
	"bytes"
	"fmt"
	"math"
	"strings"
)

// emitTypedSpecializedUnit emits a guarded pure-Go unboxed function
// (`func <fn>_typed(...) (<retGoType>, bool)`) supporting int64, float64,
// bool, mixed numeric promotion, Ruby floor-div/mod, and direct recursive /
// cross-method calls.
func (g *CodeGen) emitTypedSpecializedUnit(b *bytes.Buffer, plan *TypedPlan) {
	unit := plan.Unit
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

	// Determine which typed register arrays are actually used so Go compiles
	// with zero unused variables.
	useI, useF, useB := false, false, false
	for _, p := range plan.Params {
		switch p {
		case TypeInt64:
			useI = true
		case TypeFloat64:
			useF = true
		case TypeBool:
			useB = true
		}
	}
	for _, row := range plan.InTypes {
		for _, t := range row {
			switch t {
			case TypeInt64:
				useI = true
			case TypeFloat64:
				useF = true
			case TypeBool:
				useB = true
			}
		}
	}
	switch plan.Return {
	case TypeInt64:
		useI = true
	case TypeFloat64:
		useF = true
	case TypeBool:
		useB = true
	}

	var body bytes.Buffer
	retZero := "0"
	switch plan.Return {
	case TypeFloat64:
		retZero = "0"
	case TypeBool:
		retZero = "false"
	}
	bail := fmt.Sprintf("aotIntDepth--; return %s, false", retZero)

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

	for idx := 0; idx < len(unit.Instructions); idx++ {
		ins := unit.Instructions[idx]
		if jumpTargets[ins.Offset] {
			fmt.Fprintf(&body, "L_%04d:\n", ins.Offset)
		}
		inRow := plan.InTypes[idx]
		if inRow == nil {
			continue
		}
		typeOf := func(reg uint32) PrimType {
			if int(reg) < len(inRow) {
				return inRow[reg]
			}
			return TypeUnknown
		}

		switch ins.Op {
		case OpNOP:
		case OpENTER:
			for i, pt := range plan.Params {
				fmt.Fprintf(&body, "\t%s = _a%d\n", regExpr(uint32(i+1), pt), i+1)
			}
		case OpMOVE:
			t := typeOf(uint32(ins.B))
			fmt.Fprintf(&body, "\t%s = %s\n", regExpr(ins.A, t), regExpr(uint32(ins.B), t))
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
			// Fuse immediately-following JMPNOT/JMPIF on the same register
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
			ta := typeOf(ins.A)
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
				// no-op for int64/float64
			case "abs":
				if ta == TypeInt64 {
					useI = true
					fmt.Fprintf(&body, "\tif ri[%d] == aotMinInt { %s }; if ri[%d] < 0 { ri[%d] = -ri[%d] }\n", ins.A, bail, ins.A, ins.A, ins.A)
				} else {
					useF = true
					fmt.Fprintf(&body, "\tif rf[%d] < 0 { rf[%d] = -rf[%d] }\n", ins.A, ins.A, ins.A)
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
			target, _ := g.lookupCallTarget(g.info[unit], sym, n)
			targetFn := fnName
			cParams := plan.Params
			cRet := plan.Return
			if target != nil && target != unit {
				targetFn = g.info[target].fn
				cplan := g.typedPlans[targetFn]
				cParams = cplan.Params
				cRet = cplan.Return
			}
			args := []string{"mrb"}
			for i := 0; i < n; i++ {
				args = append(args, regExpr(ins.A+uint32(i+1), cParams[i]))
			}
			useOk = true
			fmt.Fprintf(&body, "\tif %s, _ok = %s_typed(%s); !_ok {\n\t\t%s\n\t}\n",
				regExpr(ins.A, cRet), targetFn, strings.Join(args, ", "), bail)
		case OpRETURN:
			fmt.Fprintf(&body, "\taotIntDepth--\n\treturn %s, true\n", regExpr(ins.A, plan.Return))
			hasReturn = true
		}
	}

	params := []string{"mrb *oruby.MrbState"}
	for i, pt := range plan.Params {
		params = append(params, fmt.Sprintf("_a%d %s", i+1, pt.GoType()))
	}
	if plan.FromRBS {
		fmt.Fprintf(b, "// %s_typed is RBS-guided pure-Go specialization (%s)\n", fnName, plan.RBSNote)
	}
	fmt.Fprintf(b, "func %s_typed(%s) (%s, bool) {\n", fnName, strings.Join(params, ", "), plan.Return.GoType())
	fmt.Fprintf(b, "\tif aotIntDepth >= aotIntMaxDepth {\n\t\treturn %s, false\n\t}\n\taotIntDepth++\n", retZero)
	if useI {
		fmt.Fprintf(b, "\tvar ri [%d]int64\n\t_ = ri\n", nregs)
	}
	if useF {
		fmt.Fprintf(b, "\tvar rf [%d]float64\n\t_ = rf\n", nregs)
	}
	if useB {
		fmt.Fprintf(b, "\tvar rb [%d]bool\n\t_ = rb\n", nregs)
	}
	if useOk {
		b.WriteString("\tvar _ok bool\n\t_ = _ok\n")
	}
	b.Write(body.Bytes())
	if !hasReturn {
		fmt.Fprintf(b, "\taotIntDepth--\n\treturn %s, true\n", retZero)
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
// warm cache), and runtime argument types before calling `<fn>_typed`.
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
	for _, gc := range plan.GuardCalls {
		symVar := g.internSym(gc.sym)
		slot := g.trackedSlots[gc.target]
		conds = append(conds, fmt.Sprintf("mrb.AotCompiledMethodOK(self, %s, %d)", symVar, slot))
	}
	callArgs := []string{"mrb"}
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
