package compiler

import (
	"fmt"
	"sort"
)

// regFlowType is the lattice value used during forward register type inference.
type regFlowType uint8

const (
	flowUnvisited regFlowType = iota
	flowInt64
	flowFloat64
	flowBool
	flowString
	flowArrayInt64
	flowArrayFloat64
	flowConst // constant class/module tracked via constName[] (e.g. Math, Calc)
	flowUnknown
)

func primToFlow(p PrimType) regFlowType {
	switch p {
	case TypeInt64:
		return flowInt64
	case TypeFloat64:
		return flowFloat64
	case TypeBool:
		return flowBool
	case TypeString:
		return flowString
	case TypeArrayInt64:
		return flowArrayInt64
	case TypeArrayFloat64:
		return flowArrayFloat64
	default:
		return flowUnknown
	}
}

func flowToPrim(f regFlowType) PrimType {
	switch f {
	case flowInt64:
		return TypeInt64
	case flowFloat64:
		return TypeFloat64
	case flowBool:
		return TypeBool
	case flowString:
		return TypeString
	case flowArrayInt64:
		return TypeArrayInt64
	case flowArrayFloat64:
		return TypeArrayFloat64
	default:
		return TypeUnknown
	}
}

func mergeFlow(a, b regFlowType) regFlowType {
	if a == flowUnvisited {
		return b
	}
	if b == flowUnvisited {
		return a
	}
	if a == b {
		return a
	}
	return flowUnknown
}

// typedCallRef identifies a direct compiled callee invoked by a TypedPlan.
type typedCallRef struct {
	constClass string // empty for self-call (OpSSEND), class/module name for explicit Const.method (OpSEND)
	sym        string
	targetFn   string
	target     *IRepUnit
}

// mathCoreGuard maps a Math module method name to its AotCoreID expression and arity.
type mathOpSpec struct {
	coreID  string
	helper  string
	arity   int
	canFail bool
}

var typedMathOps = map[string]mathOpSpec{
	"sqrt":  {coreID: "oruby.AotCoreMathSqrt", helper: "aotMathSqrt", arity: 1, canFail: true},
	"sin":   {coreID: "oruby.AotCoreMathSin", helper: "aotMathSin", arity: 1, canFail: false},
	"cos":   {coreID: "oruby.AotCoreMathCos", helper: "aotMathCos", arity: 1, canFail: false},
	"tan":   {coreID: "oruby.AotCoreMathTan", helper: "aotMathTan", arity: 1, canFail: false},
	"log":   {coreID: "oruby.AotCoreMathLog", helper: "aotMathLog", arity: 1, canFail: true},
	"log2":  {coreID: "oruby.AotCoreMathLog2", helper: "aotMathLog2", arity: 1, canFail: true},
	"log10": {coreID: "oruby.AotCoreMathLog10", helper: "aotMathLog10", arity: 1, canFail: true},
	"exp":   {coreID: "oruby.AotCoreMathExp", helper: "aotMathExp", arity: 1, canFail: false},
	"hypot": {coreID: "oruby.AotCoreMathHypot", helper: "aotMathHypot", arity: 2, canFail: false},
}

// TypedPlan holds the verified type-inference result for a compiled method
// eligible for guarded pure-Go unboxed codegen (`func <fn>_typed(...)`).
type TypedPlan struct {
	Unit            *IRepUnit
	FnName          string
	Params          []PrimType
	Return          PrimType
	InTypes         [][]PrimType // InTypes[insIdx][reg] = inferred PrimType on entry to insIdx
	InConsts        [][]string   // InConsts[insIdx][reg] = constant class/module name on entry to insIdx
	NeedsIntGuard   bool
	NeedsFloatGuard bool
	NeedsSelf       bool           // true if unit or a self-callee reads/writes @ivars
	HasCalls        bool           // true if unit makes direct calls (needs local _depth int)
	CoreGuards      []string       // AotCoreID expressions checked via AotCoreClassOK
	MathGuards      []string       // Math AotCoreID expressions checked via AotCoreOK(..., Math)
	DirectCalls     []typedCallRef // direct callees inside this unit
	GuardCalls      []typedCallRef // transitive deduplicated callees to guard at entry
	FromRBS         bool
	RBSNote         string
}

// RBSDiagnostic describes a static type or arity mismatch found between an RBS
// signature and the compiled Ruby bytecode.
type RBSDiagnostic struct {
	File    string
	Method  string
	Message string
}

func (d RBSDiagnostic) String() string {
	if d.File != "" {
		return fmt.Sprintf("%s (%s): %s", d.File, d.Method, d.Message)
	}
	return fmt.Sprintf("%s: %s", d.Method, d.Message)
}

// lookupCallTarget resolves a receiver-less OpSSEND/OpSSEND0 call from caller
// to a uniquely defined compiled unit (same class scope first, then top-level).
func (g *CodeGen) lookupCallTarget(caller *unitInfo, sym string, numArgs int) (*IRepUnit, bool) {
	if caller == nil {
		return nil, false
	}
	tryKey := func(cls string, singleton bool) (*IRepUnit, bool) {
		k := rbsMethodKey{className: normalizeRBSClass(cls), method: sym, singleton: singleton}
		if g.scopedDefCount[k] != 1 {
			return nil, false
		}
		u := g.scopedDefs[k]
		if u == nil {
			return nil, false
		}
		inf := g.info[u]
		if inf == nil || inf.needCtx {
			return nil, false
		}
		arity, ok := g.compiledArity[inf.fn]
		if !ok || arity != numArgs {
			return nil, false
		}
		return u, true
	}
	if caller.className != "" || caller.singleton {
		if u, ok := tryKey(caller.className, caller.singleton); ok {
			return u, true
		}
	}
	return tryKey("", false)
}

// lookupSingletonCallTarget resolves an explicit Class.method call (e.g. Calc.mix).
func (g *CodeGen) lookupSingletonCallTarget(className, sym string, numArgs int) (*IRepUnit, bool) {
	k := rbsMethodKey{className: normalizeRBSClass(className), method: sym, singleton: true}
	if g.scopedDefCount[k] != 1 {
		return nil, false
	}
	u := g.scopedDefs[k]
	if u == nil {
		return nil, false
	}
	inf := g.info[u]
	if inf == nil || inf.needCtx {
		return nil, false
	}
	arity, ok := g.compiledArity[inf.fn]
	if !ok || arity != numArgs {
		return nil, false
	}
	return u, true
}

func unitHasFloatLiteral(u *IRepUnit) bool {
	for _, p := range u.Pool {
		if _, ok := p.(float64); ok {
			return true
		}
	}
	return false
}

// analyzeTypedUnits runs RBS type validation and fixed-point type inference
// across all compiled methods, populating g.typedPlans and g.rbsDiagnostics.
func (g *CodeGen) analyzeTypedUnits() {
	if g.opts.Debug {
		return
	}

	// Sort units deterministically by generated Go function name.
	units := make([]*IRepUnit, 0, len(g.info))
	for u, inf := range g.info {
		if inf.kind != kindMethod || inf.method == "" || len(u.Instructions) == 0 || len(u.Catch) > 0 {
			continue
		}
		arity, ok := g.compiledArity[inf.fn]
		if !ok || arity < 0 || arity > 6 {
			continue
		}
		units = append(units, u)
	}
	sort.Slice(units, func(i, j int) bool { return g.info[units[i]].fn < g.info[units[j]].fn })

	// 1. Validate RBS parameter arity mismatches upfront.
	rbsSigs := make(map[*IRepUnit]*RBSMethodSig)
	if g.opts.RBS != nil {
		for _, u := range units {
			inf := g.info[u]
			sig := g.opts.RBS.LookupMethod(inf.className, inf.method, inf.singleton)
			if sig == nil {
				continue
			}
			arity := g.compiledArity[inf.fn]
			if len(sig.Params) != arity {
				g.rbsDiagnostics = append(g.rbsDiagnostics, RBSDiagnostic{
					File:    inf.file,
					Method:  formatMethodDisplay(inf),
					Message: fmt.Sprintf("RBS signature (%s) expects %d params, Ruby def has %d", sig.Source, len(sig.Params), arity),
				})
				continue
			}
			rbsSigs[u] = sig
		}
	}

	// 2. Fixed-point inference loop so mutually calling / helper methods specialize together.
	rbsReturnChecked := make(map[*IRepUnit]bool)
	changed := true
	for pass := 0; changed && pass < 16; pass++ {
		changed = false
		for _, u := range units {
			inf := g.info[u]
			if _, already := g.typedPlans[inf.fn]; already {
				continue
			}
			arity := g.compiledArity[inf.fn]
			sig := rbsSigs[u]

			if sig != nil {
				allOK := true
				for _, p := range sig.Params {
					if !p.IsSpecializableParam() {
						allOK = false
						break
					}
				}
				if allOK {
					plan, mismatch := g.inferTypedPlan(u, sig.Params, sig.Return, true, sig.Source)
					if mismatch != "" && !rbsReturnChecked[u] {
						rbsReturnChecked[u] = true
						g.rbsDiagnostics = append(g.rbsDiagnostics, RBSDiagnostic{
							File:    inf.file,
							Method:  formatMethodDisplay(inf),
							Message: mismatch,
						})
					}
					if plan != nil {
						g.typedPlans[inf.fn] = plan
						changed = true
					}
				}
				continue
			}

			// Automatic specialization (no RBS signature): require 1..3 params
			if arity < 1 || arity > 3 {
				continue
			}
			intParams := make([]PrimType, arity)
			for i := range intParams {
				intParams[i] = TypeInt64
			}
			if plan, _ := g.inferTypedPlan(u, intParams, TypeUnknown, false, ""); plan != nil {
				g.typedPlans[inf.fn] = plan
				changed = true
				continue
			}
			if unitHasFloatLiteral(u) {
				fltParams := make([]PrimType, arity)
				for i := range fltParams {
					fltParams[i] = TypeFloat64
				}
				if plan, _ := g.inferTypedPlan(u, fltParams, TypeUnknown, false, ""); plan != nil {
					g.typedPlans[inf.fn] = plan
					changed = true
				}
			}
		}
	}

	// 3. Collect transitive direct-call, core, math, and NeedsSelf requirements,
	//    and assign tracked method slots.
	nextSlot := 0
	for _, u := range units {
		inf := g.info[u]
		plan := g.typedPlans[inf.fn]
		if plan == nil {
			continue
		}
		plan.HasCalls = len(plan.DirectCalls) > 0

		seenCall := make(map[string]bool)
		seenUnit := make(map[*IRepUnit]bool)
		coreSet := make(map[string]bool)
		mathSet := make(map[string]bool)
		for _, cg := range plan.CoreGuards {
			coreSet[cg] = true
		}
		for _, mg := range plan.MathGuards {
			mathSet[mg] = true
		}

		type stackItem struct {
			unit   *IRepUnit
			isSelf bool
		}
		var stack []stackItem
		addCall := func(dc typedCallRef, viaSelf bool) {
			key := dc.constClass + "#" + dc.sym + ":" + dc.targetFn
			if !seenCall[key] {
				seenCall[key] = true
				plan.GuardCalls = append(plan.GuardCalls, dc)
			}
			if !seenUnit[dc.target] {
				seenUnit[dc.target] = true
				stack = append(stack, stackItem{unit: dc.target, isSelf: viaSelf && dc.constClass == ""})
			}
		}
		for _, dc := range plan.DirectCalls {
			addCall(dc, true)
		}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cplan := g.typedPlans[g.info[cur.unit].fn]
			if cplan == nil {
				continue
			}
			if cplan.NeedsIntGuard {
				plan.NeedsIntGuard = true
			}
			if cplan.NeedsFloatGuard {
				plan.NeedsFloatGuard = true
			}
			if cur.isSelf && cplan.NeedsSelf {
				plan.NeedsSelf = true
			}
			for _, cg := range cplan.CoreGuards {
				coreSet[cg] = true
			}
			for _, mg := range cplan.MathGuards {
				mathSet[mg] = true
			}
			for _, dc := range cplan.DirectCalls {
				addCall(dc, cur.isSelf)
			}
		}

		plan.CoreGuards = sortedKeys(coreSet)
		plan.MathGuards = sortedKeys(mathSet)
		sort.Slice(plan.GuardCalls, func(i, j int) bool {
			if plan.GuardCalls[i].constClass != plan.GuardCalls[j].constClass {
				return plan.GuardCalls[i].constClass < plan.GuardCalls[j].constClass
			}
			return plan.GuardCalls[i].targetFn < plan.GuardCalls[j].targetFn
		})
		for _, gc := range plan.GuardCalls {
			if _, ok := g.trackedSlots[gc.target]; !ok {
				g.trackedSlots[gc.target] = nextSlot
				nextSlot++
			}
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatMethodDisplay(inf *unitInfo) string {
	if inf.className == "" {
		if inf.singleton {
			return "self." + inf.method
		}
		return inf.method
	}
	if inf.singleton {
		return inf.className + "." + inf.method
	}
	return inf.className + "#" + inf.method
}

// inferTypedPlan runs forward dataflow type inference on unit seeded with
// paramTypes and expectedReturn (TypeUnknown if unspecified).
func (g *CodeGen) inferTypedPlan(unit *IRepUnit, paramTypes []PrimType, expectedReturn PrimType, fromRBS bool, rbsSource string) (*TypedPlan, string) {
	inf := g.info[unit]
	arity := len(paramTypes)
	nregs := unit.NRegs
	if nregs < arity+2 {
		nregs = arity + 2
	}
	insCount := len(unit.Instructions)
	offToIdx := make(map[int]int, insCount)
	for i, ins := range unit.Instructions {
		offToIdx[ins.Offset] = i
	}

	selfRetGuess := primToFlow(expectedReturn)
	if selfRetGuess == flowUnknown {
		selfRetGuess = flowUnvisited
	}

	runPass := func(selfRet regFlowType) (*TypedPlan, regFlowType, string) {
		in := make([][]regFlowType, insCount)
		inC := make([][]string, insCount)
		in[0] = make([]regFlowType, nregs)
		inC[0] = make([]string, nregs)
		for i, pt := range paramTypes {
			in[0][i+1] = primToFlow(pt)
		}

		var needsInt, needsFloat, needsSelf bool
		coreGuards := make(map[string]bool)
		mathGuards := make(map[string]bool)
		var directCalls []typedCallRef
		directSeen := make(map[string]bool)
		retFlow := flowUnvisited

		mergeInto := func(targetIdx int, state []regFlowType, cstate []string) bool {
			if targetIdx < 0 || targetIdx >= insCount {
				return false
			}
			if in[targetIdx] == nil {
				in[targetIdx] = append([]regFlowType(nil), state...)
				inC[targetIdx] = append([]string(nil), cstate...)
				return true
			}
			changed := false
			for r := 0; r < nregs; r++ {
				m := mergeFlow(in[targetIdx][r], state[r])
				if m != in[targetIdx][r] {
					in[targetIdx][r] = m
					changed = true
				}
				if m == flowConst {
					if inC[targetIdx][r] != cstate[r] {
						in[targetIdx][r] = flowUnknown
						inC[targetIdx][r] = ""
						changed = true
					}
				} else if inC[targetIdx][r] != "" {
					inC[targetIdx][r] = ""
				}
			}
			return changed
		}

		for iter := 0; iter < insCount*4+16; iter++ {
			progress := false
			needsInt, needsFloat, needsSelf = false, false, false
			clear(coreGuards)
			clear(mathGuards)
			retFlow = flowUnvisited
			directCalls = nil
			clear(directSeen)

			for idx := 0; idx < insCount; idx++ {
				if in[idx] == nil {
					continue
				}
				st := append([]regFlowType(nil), in[idx]...)
				cst := append([]string(nil), inC[idx]...)
				ins := unit.Instructions[idx]
				setReg := func(reg uint32, t regFlowType) bool {
					if int(reg) >= nregs {
						return false
					}
					st[reg] = t
					cst[reg] = ""
					return true
				}
				setConst := func(reg uint32, name string) bool {
					if int(reg) >= nregs {
						return false
					}
					st[reg] = flowConst
					cst[reg] = name
					return true
				}
				getReg := func(reg uint32) regFlowType {
					if int(reg) >= nregs {
						return flowUnknown
					}
					return st[reg]
				}
				getConst := func(reg uint32) string {
					if int(reg) >= nregs {
						return ""
					}
					return cst[reg]
				}

				fallthroughNext := true
				switch ins.Op {
				case OpNOP, OpENTER:
				case OpMOVE:
					bt := getReg(uint32(ins.B))
					if bt == flowConst {
						if !setConst(ins.A, getConst(uint32(ins.B))) {
							return nil, flowUnknown, ""
						}
					} else if !setReg(ins.A, bt) {
						return nil, flowUnknown, ""
					}
				case OpLOADI8, OpLOADINEG, OpLOADI__1,
					OpLOADI_0, OpLOADI_1, OpLOADI_2, OpLOADI_3, OpLOADI_4, OpLOADI_5, OpLOADI_6, OpLOADI_7,
					OpLOADI16, OpLOADI32:
					if !setReg(ins.A, flowInt64) {
						return nil, flowUnknown, ""
					}
				case OpLOADTRUE, OpLOADFALSE:
					if !setReg(ins.A, flowBool) {
						return nil, flowUnknown, ""
					}
				case OpLOADL:
					if int(ins.B) >= len(unit.Pool) {
						return nil, flowUnknown, ""
					}
					switch unit.Pool[ins.B].(type) {
					case int32, int64:
						setReg(ins.A, flowInt64)
					case float64:
						setReg(ins.A, flowFloat64)
					default:
						return nil, flowUnknown, ""
					}
				case OpGETCONST:
					if int(ins.B) >= len(unit.Syms) {
						return nil, flowUnknown, ""
					}
					cname := unit.Syms[ins.B]
					if !setConst(ins.A, cname) {
						return nil, flowUnknown, ""
					}
				case OpGETIV:
					if g.opts.RBS == nil || inf.singleton || inf.className == "" || int(ins.B) >= len(unit.Syms) {
						return nil, flowUnknown, ""
					}
					ivType := g.opts.RBS.LookupIvar(inf.className, unit.Syms[ins.B])
					if !ivType.IsSpecializable() {
						return nil, flowUnknown, ""
					}
					needsSelf = true
					if !setReg(ins.A, primToFlow(ivType)) {
						return nil, flowUnknown, ""
					}
				case OpSETIV:
					if g.opts.RBS == nil || inf.singleton || inf.className == "" || int(ins.B) >= len(unit.Syms) {
						return nil, flowUnknown, ""
					}
					ivType := g.opts.RBS.LookupIvar(inf.className, unit.Syms[ins.B])
					if !ivType.IsSpecializable() || getReg(ins.A) != primToFlow(ivType) {
						return nil, flowUnknown, ""
					}
					needsSelf = true
				case OpGETIDX:
					ta, tb := getReg(ins.A), getReg(ins.A+1)
					if tb != flowInt64 {
						return nil, flowUnknown, ""
					}
					switch ta {
					case flowArrayInt64:
						coreGuards["oruby.AotCoreAryIdx"] = true
						setReg(ins.A, flowInt64)
					case flowArrayFloat64:
						coreGuards["oruby.AotCoreAryIdx"] = true
						setReg(ins.A, flowFloat64)
					default:
						return nil, flowUnknown, ""
					}
				case OpGETIDX0:
					tb := getReg(uint32(ins.B))
					switch tb {
					case flowArrayInt64:
						coreGuards["oruby.AotCoreAryIdx"] = true
						setReg(ins.A, flowInt64)
					case flowArrayFloat64:
						coreGuards["oruby.AotCoreAryIdx"] = true
						setReg(ins.A, flowFloat64)
					default:
						return nil, flowUnknown, ""
					}
				case OpADD, OpSUB, OpMUL, OpDIV:
					ta, tb := getReg(ins.A), getReg(ins.A+1)
					if ta == flowInt64 && tb == flowInt64 {
						needsInt = true
						setReg(ins.A, flowInt64)
					} else if (ta == flowFloat64 || ta == flowInt64) && (tb == flowFloat64 || tb == flowInt64) {
						needsFloat = true
						setReg(ins.A, flowFloat64)
					} else {
						return nil, flowUnknown, ""
					}
				case OpADDI, OpADDILV, OpSUBI, OpSUBILV:
					ta := getReg(ins.A)
					switch ta {
					case flowInt64:
						needsInt = true
						setReg(ins.A, flowInt64)
					case flowFloat64:
						needsFloat = true
						setReg(ins.A, flowFloat64)
					default:
						return nil, flowUnknown, ""
					}
				case OpEQ, OpLT, OpLE, OpGT, OpGE:
					ta, tb := getReg(ins.A), getReg(ins.A+1)
					switch {
					case ta == flowInt64 && tb == flowInt64:
						needsInt = true
					case (ta == flowFloat64 || ta == flowInt64) && (tb == flowFloat64 || tb == flowInt64):
						needsFloat = true
					case ins.Op == OpEQ && ta == flowBool && tb == flowBool:
					default:
						return nil, flowUnknown, ""
					}
					setReg(ins.A, flowBool)
				case OpJMP:
					fallthroughNext = false
					tidx, ok := offToIdx[ins.Target]
					if !ok {
						return nil, flowUnknown, ""
					}
					if mergeInto(tidx, st, cst) {
						progress = true
					}
				case OpJMPIF, OpJMPNOT:
					if getReg(ins.A) != flowBool {
						return nil, flowUnknown, ""
					}
					tidx, ok := offToIdx[ins.Target]
					if !ok {
						return nil, flowUnknown, ""
					}
					if mergeInto(tidx, st, cst) {
						progress = true
					}
				case OpSEND, OpSEND0:
					if int(ins.B) >= len(unit.Syms) {
						return nil, flowUnknown, ""
					}
					sym := unit.Syms[ins.B]
					n := 0
					nk := 0
					if ins.Op == OpSEND {
						n = int(ins.C & 0xf)
						nk = int((ins.C >> 4) & 0xf)
					}
					if nk != 0 || n == 15 {
						return nil, flowUnknown, ""
					}
					ta := getReg(ins.A)

					// Case A: Constant receiver (Math.<fn> or Class.singleton_method)
					if ta == flowConst {
						cname := getConst(ins.A)
						if cname == "Math" {
							spec, ok := typedMathOps[sym]
							if !ok || spec.arity != n {
								return nil, flowUnknown, ""
							}
							for i := 0; i < n; i++ {
								at := getReg(ins.A + uint32(i+1))
								if at != flowInt64 && at != flowFloat64 {
									return nil, flowUnknown, ""
								}
							}
							mathGuards[spec.coreID] = true
							needsFloat = true
							setReg(ins.A, flowFloat64)
							break
						}
						target, ok := g.lookupSingletonCallTarget(cname, sym, n)
						if !ok {
							return nil, flowUnknown, ""
						}
						targetFn := g.info[target].fn
						tp := g.typedPlans[targetFn]
						if tp == nil || len(tp.Params) != n || tp.NeedsSelf {
							return nil, flowUnknown, ""
						}
						for i := 0; i < n; i++ {
							if getReg(ins.A+uint32(i+1)) != primToFlow(tp.Params[i]) {
								return nil, flowUnknown, ""
							}
						}
						if !tp.Return.IsSpecializable() {
							return nil, flowUnknown, ""
						}
						if tp.NeedsIntGuard {
							needsInt = true
						}
						if tp.NeedsFloatGuard {
							needsFloat = true
						}
						setReg(ins.A, primToFlow(tp.Return))
						dkey := cname + "." + sym + ":" + targetFn
						if !directSeen[dkey] {
							directSeen[dkey] = true
							directCalls = append(directCalls, typedCallRef{
								constClass: cname,
								sym:        sym,
								targetFn:   targetFn,
								target:     target,
							})
						}
						break
					}

					// Case B: Typed Array[Integer] / Array[Float] methods
					if ta == flowArrayInt64 || ta == flowArrayFloat64 {
						if (sym == "length" || sym == "size") && n == 0 {
							if sym == "length" {
								coreGuards["oruby.AotCoreAryLen"] = true
							} else {
								coreGuards["oruby.AotCoreArySize"] = true
							}
							setReg(ins.A, flowInt64)
							break
						}
						return nil, flowUnknown, ""
					}

					// Case C: Built-in numeric/bool methods
					switch {
					case sym == "%" && n == 1:
						tb := getReg(ins.A + 1)
						if ta == flowInt64 && tb == flowInt64 {
							needsInt = true
							setReg(ins.A, flowInt64)
						} else if (ta == flowFloat64 || ta == flowInt64) && (tb == flowFloat64 || tb == flowInt64) {
							needsFloat = true
							setReg(ins.A, flowFloat64)
						} else {
							return nil, flowUnknown, ""
						}
					case sym == "**" && n == 1:
						tb := getReg(ins.A + 1)
						if ta == flowInt64 && tb == flowInt64 {
							needsInt = true
							setReg(ins.A, flowInt64)
						} else if (ta == flowFloat64 || ta == flowInt64) && (tb == flowFloat64 || tb == flowInt64) {
							needsFloat = true
							setReg(ins.A, flowFloat64)
						} else {
							return nil, flowUnknown, ""
						}
					case (sym == "&" || sym == "|" || sym == "^" || sym == "<<" || sym == ">>") && n == 1:
						tb := getReg(ins.A + 1)
						if ta == flowInt64 && tb == flowInt64 {
							needsInt = true
							setReg(ins.A, flowInt64)
						} else {
							return nil, flowUnknown, ""
						}
					case sym == "~" && n == 0 && ta == flowInt64:
						needsInt = true
						setReg(ins.A, flowInt64)
					case sym == "!=" && n == 1:
						tb := getReg(ins.A + 1)
						if ta == flowInt64 && tb == flowInt64 {
							needsInt = true
						} else if (ta == flowFloat64 || ta == flowInt64) && (tb == flowFloat64 || tb == flowInt64) {
							needsFloat = true
						} else if ta == flowBool && tb == flowBool {
						} else {
							return nil, flowUnknown, ""
						}
						setReg(ins.A, flowBool)
					case (sym == "-@" || sym == "+@" || sym == "abs") && n == 0:
						if ta == flowInt64 {
							needsInt = true
							setReg(ins.A, flowInt64)
						} else if ta == flowFloat64 {
							needsFloat = true
							setReg(ins.A, flowFloat64)
						} else {
							return nil, flowUnknown, ""
						}
					case (sym == "zero?" || sym == "positive?" || sym == "negative?") && n == 0 && (ta == flowInt64 || ta == flowFloat64):
						if ta == flowInt64 {
							needsInt = true
						} else {
							needsFloat = true
						}
						setReg(ins.A, flowBool)
					case (sym == "even?" || sym == "odd?") && n == 0 && ta == flowInt64:
						needsInt = true
						setReg(ins.A, flowBool)
					case (sym == "floor" || sym == "ceil" || sym == "round") && n == 0 && (ta == flowInt64 || ta == flowFloat64):
						if ta == flowFloat64 {
							needsFloat = true
						}
						needsInt = true
						setReg(ins.A, flowInt64)
					case sym == "to_f" && n == 0 && (ta == flowInt64 || ta == flowFloat64):
						if ta == flowInt64 {
							needsInt = true
						}
						needsFloat = true
						setReg(ins.A, flowFloat64)
					case sym == "to_i" && n == 0 && (ta == flowInt64 || ta == flowFloat64):
						if ta == flowFloat64 {
							needsFloat = true
						}
						needsInt = true
						setReg(ins.A, flowInt64)
					default:
						return nil, flowUnknown, ""
					}
				case OpSSEND, OpSSEND0:
					if int(ins.B) >= len(unit.Syms) {
						return nil, flowUnknown, ""
					}
					sym := unit.Syms[ins.B]
					n := 0
					nk := 0
					if ins.Op == OpSSEND {
						n = int(ins.C & 0xf)
						nk = int((ins.C >> 4) & 0xf)
					}
					if nk != 0 || n == 15 {
						return nil, flowUnknown, ""
					}
					target, ok := g.lookupCallTarget(inf, sym, n)
					if !ok {
						return nil, flowUnknown, ""
					}
					targetFn := g.info[target].fn
					var cParams []PrimType
					var cRet regFlowType
					if target == unit {
						cParams = paramTypes
						cRet = selfRet
					} else if tp := g.typedPlans[targetFn]; tp != nil {
						cParams = tp.Params
						cRet = primToFlow(tp.Return)
						if tp.NeedsIntGuard {
							needsInt = true
						}
						if tp.NeedsFloatGuard {
							needsFloat = true
						}
						if tp.NeedsSelf {
							needsSelf = true
						}
					} else {
						return nil, flowUnknown, ""
					}
					if len(cParams) != n {
						return nil, flowUnknown, ""
					}
					for i := 0; i < n; i++ {
						if getReg(ins.A+uint32(i+1)) != primToFlow(cParams[i]) {
							return nil, flowUnknown, ""
						}
					}
					if cRet == flowUnvisited {
						return nil, retFlow, ""
					}
					if !flowToPrim(cRet).IsSpecializable() {
						return nil, flowUnknown, ""
					}
					setReg(ins.A, cRet)
					if !directSeen[targetFn] {
						directSeen[targetFn] = true
						directCalls = append(directCalls, typedCallRef{
							sym:      sym,
							targetFn: targetFn,
							target:   target,
						})
					}
				case OpRETURN:
					fallthroughNext = false
					rt := getReg(ins.A)
					if rt == flowUnvisited {
						return nil, flowUnknown, ""
					}
					retFlow = mergeFlow(retFlow, rt)
				default:
					return nil, flowUnknown, ""
				}

				if fallthroughNext && idx+1 < insCount {
					if mergeInto(idx+1, st, cst) {
						progress = true
					}
				}
			}
			if !progress {
				break
			}
		}

		retPrim := flowToPrim(retFlow)
		if !retPrim.IsSpecializable() {
			return nil, retFlow, ""
		}
		if expectedReturn != TypeUnknown && expectedReturn != retPrim {
			return nil, retFlow, fmt.Sprintf("RBS return type mismatch (%s): declared %s, inferred %s", rbsSource, expectedReturn, retPrim)
		}

		inPrims := make([][]PrimType, insCount)
		for i := 0; i < insCount; i++ {
			if in[i] == nil {
				continue
			}
			row := make([]PrimType, nregs)
			for r := 0; r < nregs; r++ {
				row[r] = flowToPrim(in[i][r])
			}
			inPrims[i] = row
		}

		plan := &TypedPlan{
			Unit:            unit,
			FnName:          inf.fn,
			Params:          append([]PrimType(nil), paramTypes...),
			Return:          retPrim,
			InTypes:         inPrims,
			InConsts:        inC,
			NeedsIntGuard:   needsInt,
			NeedsFloatGuard: needsFloat,
			NeedsSelf:       needsSelf,
			CoreGuards:      sortedKeys(coreGuards),
			MathGuards:      sortedKeys(mathGuards),
			DirectCalls:     directCalls,
			FromRBS:         fromRBS,
			RBSNote:         rbsSource,
		}
		return plan, retFlow, ""
	}

	plan, guessedRet, mismatch := runPass(selfRetGuess)
	if plan != nil || mismatch != "" {
		return plan, mismatch
	}
	if selfRetGuess == flowUnvisited && flowToPrim(guessedRet).IsSpecializable() {
		plan, _, mismatch = runPass(guessedRet)
		return plan, mismatch
	}
	return nil, ""
}
