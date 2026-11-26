package compiler

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

type blkRegKind uint8

const (
	bkUnknown blkRegKind = iota
	bkInt64
	bkBool
	bkRoUpvar
)

type pureAccSite struct {
	upSlot uint32
	sym    string
}

// typedBlockLoop describes an inlined block body that can run as an unboxed
// int64/bool Go loop with optional hoisted pure 0-arg accessor reads.
type typedBlockLoop struct {
	child           *IRepUnit
	m1              int
	hasEnter        bool
	bodyStart       int
	nregs           int
	inKinds         [][]blkRegKind
	inRoSlot        [][]uint32
	roReceiverSlots map[uint32]bool
	retKind         blkRegKind
	intUpvars       []uint32
	writtenUpvars   []uint32
	pureAccessors   []pureAccSite
	insAccIdx       map[int]int
	usesI           bool
	usesB           bool
	usesOk          bool
}

func isTypedBlockIntZeroOp(sym string) bool {
	return sym == "-@" || sym == "+@"
}

// skipPrismTwoArgPreamble detects the dead 1-arg #to_ary spread guard emitted
// by prism.go's ORB_BLOCK_PARAMS for 2-arg blocks (`|acc, x|`), and returns the
// instruction index of the real block body when both arguments are guaranteed.
func skipPrismTwoArgPreamble(child *IRepUnit, start int, offToIdx map[int]int) int {
	insns := child.Instructions
	if start+4 >= len(insns) {
		return start
	}
	if insns[start].Op != OpMOVE || insns[start].B != 2 {
		return start
	}
	tmpReg := insns[start].A
	if insns[start+1].Op != OpSEND0 || insns[start+1].A != tmpReg || int(insns[start+1].B) >= len(child.Syms) || child.Syms[insns[start+1].B] != "!" {
		return start
	}
	if insns[start+2].Op != OpJMPNOT || insns[start+2].A != tmpReg {
		return start
	}
	j1Idx, ok1 := offToIdx[insns[start+2].Target]
	if !ok1 || j1Idx <= start+3 || insns[j1Idx].Op != OpJMPNOT || insns[j1Idx].A != tmpReg {
		return start
	}
	j2Idx, ok2 := offToIdx[insns[j1Idx].Target]
	if !ok2 || j2Idx <= j1Idx {
		return start
	}
	hasBta, hasAryOf := false, false
	for k := start + 3; k < j2Idx; k++ {
		op := insns[k].Op
		if (op == OpSEND || op == OpSEND0) && int(insns[k].B) < len(child.Syms) {
			switch child.Syms[insns[k].B] {
			case "__oruby_bta?":
				hasBta = true
			case "__oruby_ary_of":
				hasAryOf = true
			}
		}
	}
	if hasBta && hasAryOf {
		return j2Idx
	}
	return start
}

func (e *unitEmitter) analyzeTypedBlockLoop(child *IRepUnit, yieldArity int, wantRet blkRegKind) *typedBlockLoop {
	m1, hasEnter, ok := e.canInlineLoopBlock(child, yieldArity)
	if !ok {
		return nil
	}
	if e.readOnlyUp0 == nil {
		e.readOnlyUp0 = readOnlyLevel0Upvars(e.u)
	}
	insCount := len(child.Instructions)
	start := 0
	if hasEnter {
		start = 1
	}

	offToIdx := make(map[int]int, insCount)
	for i, ins := range child.Instructions {
		offToIdx[ins.Offset] = i
	}
	bodyStart := start
	if m1 == 2 {
		bodyStart = skipPrismTwoArgPreamble(child, start, offToIdx)
	}

	// Pre-scan for unsupported opcodes and identify read-only level-0 upvar
	// slots that are used as receivers of 0-arg pure accessor calls.
	roReceiverSlots := make(map[uint32]bool)
	for i := bodyStart; i < insCount; i++ {
		ins := child.Instructions[i]
		switch ins.Op {
		case OpBREAK, OpRETURN_BLK:
			return nil
		case OpGETUPVAR, OpSETUPVAR:
			if ins.C != 0 {
				return nil
			}
			if ins.Op == OpGETUPVAR && e.readOnlyUp0[uint32(ins.B)] && i+1 < insCount {
				nx := child.Instructions[i+1]
				if nx.Op == OpSEND0 && nx.A == ins.A && int(nx.B) < len(child.Syms) && !isTypedBlockIntZeroOp(child.Syms[nx.B]) {
					roReceiverSlots[uint32(ins.B)] = true
				}
			}
		}
	}

	nregs := child.NRegs + 3
	if nregs < 4 {
		nregs = 4
	}
	if hasEnter && m1+2 > nregs {
		nregs = m1 + 2
	}

	inKinds := make([][]blkRegKind, insCount)
	inRoSlot := make([][]uint32, insCount)
	initKinds := make([]blkRegKind, nregs)
	initRo := make([]uint32, nregs)
	for i := 1; i <= m1; i++ {
		initKinds[i] = bkInt64
	}
	inKinds[bodyStart] = initKinds
	inRoSlot[bodyStart] = initRo

	mergeInto := func(target int, st []blkRegKind, ro []uint32) bool {
		if target < 0 || target >= insCount {
			return false
		}
		if inKinds[target] == nil {
			inKinds[target] = append([]blkRegKind(nil), st...)
			inRoSlot[target] = append([]uint32(nil), ro...)
			return true
		}
		changed := false
		for r := 0; r < nregs; r++ {
			k := inKinds[target][r]
			if st[r] == bkUnknown {
				continue
			}
			if k == bkUnknown {
				inKinds[target][r] = st[r]
				inRoSlot[target][r] = ro[r]
				changed = true
			} else if k != st[r] || (k == bkRoUpvar && inRoSlot[target][r] != ro[r]) {
				return false
			}
		}
		return changed
	}

	intUpSet := make(map[uint32]bool)
	writtenUpSet := make(map[uint32]bool)
	var pureAccessors []pureAccSite
	accMap := make(map[pureAccSite]int)
	insAccIdx := make(map[int]int)
	retKind := bkUnknown
	var usesI, usesB, usesOk bool

	for iter := 0; iter < insCount*4+8; iter++ {
		progress := false
		clear(intUpSet)
		clear(writtenUpSet)
		pureAccessors = nil
		clear(accMap)
		clear(insAccIdx)
		retKind = bkUnknown
		usesI, usesB, usesOk = false, false, false

		for idx := 0; idx < insCount; idx++ {
			if inKinds[idx] == nil {
				continue
			}
			st := append([]blkRegKind(nil), inKinds[idx]...)
			ro := append([]uint32(nil), inRoSlot[idx]...)
			ins := child.Instructions[idx]
			fallthroughNext := true

			switch ins.Op {
			case OpNOP, OpENTER:
			case OpMOVE:
				if int(ins.A) >= nregs || int(ins.B) >= nregs {
					return nil
				}
				k := st[ins.B]
				if k == bkUnknown {
					return nil
				}
				st[ins.A] = k
				ro[ins.A] = ro[ins.B]
			case OpLOADI8, OpLOADINEG, OpLOADI__1,
				OpLOADI_0, OpLOADI_1, OpLOADI_2, OpLOADI_3, OpLOADI_4, OpLOADI_5, OpLOADI_6, OpLOADI_7,
				OpLOADI16, OpLOADI32:
				if int(ins.A) >= nregs {
					return nil
				}
				st[ins.A] = bkInt64
				ro[ins.A] = 0
				usesI = true
			case OpLOADL:
				if int(ins.A) >= nregs || int(ins.B) >= len(child.Pool) {
					return nil
				}
				switch child.Pool[ins.B].(type) {
				case int32, int64:
					st[ins.A] = bkInt64
					ro[ins.A] = 0
					usesI = true
				default:
					return nil
				}
			case OpLOADTRUE, OpLOADFALSE:
				if int(ins.A) >= nregs {
					return nil
				}
				st[ins.A] = bkBool
				ro[ins.A] = 0
				usesB = true
			case OpGETUPVAR:
				if int(ins.A) >= nregs || ins.C != 0 {
					return nil
				}
				slot := uint32(ins.B)
				if roReceiverSlots[slot] {
					st[ins.A] = bkRoUpvar
					ro[ins.A] = slot
				} else {
					st[ins.A] = bkInt64
					ro[ins.A] = 0
					intUpSet[slot] = true
					usesI = true
				}
			case OpSETUPVAR:
				if int(ins.A) >= nregs || ins.C != 0 || st[ins.A] != bkInt64 {
					return nil
				}
				slot := uint32(ins.B)
				if roReceiverSlots[slot] {
					return nil
				}
				intUpSet[slot] = true
				writtenUpSet[slot] = true
				usesI = true
			case OpADD, OpSUB, OpMUL, OpDIV:
				if int(ins.A+1) >= nregs || st[ins.A] != bkInt64 || st[ins.A+1] != bkInt64 {
					return nil
				}
				st[ins.A] = bkInt64
				ro[ins.A] = 0
				usesI = true
				usesOk = true
			case OpADDI, OpADDILV, OpSUBI, OpSUBILV:
				if int(ins.A) >= nregs || st[ins.A] != bkInt64 {
					return nil
				}
				st[ins.A] = bkInt64
				ro[ins.A] = 0
				usesI = true
				usesOk = true
			case OpEQ, OpLT, OpLE, OpGT, OpGE:
				if int(ins.A+1) >= nregs {
					return nil
				}
				ta, tb := st[ins.A], st[ins.A+1]
				if !((ta == bkInt64 && tb == bkInt64) || (ta == bkBool && tb == bkBool)) {
					return nil
				}
				st[ins.A] = bkBool
				ro[ins.A] = 0
				usesB = true
			case OpJMP:
				fallthroughNext = false
				tIdx, ok := offToIdx[ins.Target]
				if !ok {
					return nil
				}
				if mergeInto(tIdx, st, ro) {
					progress = true
				}
			case OpJMPIF, OpJMPNOT:
				if int(ins.A) >= nregs || st[ins.A] != bkBool {
					return nil
				}
				tIdx, ok := offToIdx[ins.Target]
				if !ok {
					return nil
				}
				if mergeInto(tIdx, st, ro) {
					progress = true
				}
			case OpSEND, OpSEND0:
				if int(ins.B) >= len(child.Syms) {
					return nil
				}
				n := 0
				if ins.Op == OpSEND {
					nk := int((ins.C >> 4) & 0xf)
					if nk != 0 {
						return nil
					}
					n = int(ins.C & 0xf)
				}
				if int(ins.A)+n >= nregs {
					return nil
				}
				sym := child.Syms[ins.B]
				if n == 0 && st[ins.A] == bkRoUpvar {
					site := pureAccSite{upSlot: ro[ins.A], sym: sym}
					accIdx, exists := accMap[site]
					if !exists {
						accIdx = len(pureAccessors)
						accMap[site] = accIdx
						pureAccessors = append(pureAccessors, site)
					}
					insAccIdx[idx] = accIdx
					st[ins.A] = bkInt64
					ro[ins.A] = 0
					usesI = true
				} else if n == 1 && st[ins.A] == bkInt64 && st[ins.A+1] == bkInt64 {
					switch sym {
					case "%", "<<", ">>":
						st[ins.A] = bkInt64
						ro[ins.A] = 0
						usesI = true
						usesOk = true
					case "&", "|", "^":
						st[ins.A] = bkInt64
						ro[ins.A] = 0
						usesI = true
					case "!=":
						st[ins.A] = bkBool
						ro[ins.A] = 0
						usesB = true
					default:
						return nil
					}
				} else if n == 1 && st[ins.A] == bkBool && st[ins.A+1] == bkBool && sym == "!=" {
					st[ins.A] = bkBool
					ro[ins.A] = 0
					usesB = true
				} else if n == 0 && st[ins.A] == bkInt64 {
					switch sym {
					case "-@", "+@":
						st[ins.A] = bkInt64
						ro[ins.A] = 0
						usesI = true
					default:
						return nil
					}
				} else {
					return nil
				}
			case OpRETURN:
				fallthroughNext = false
				if int(ins.A) >= nregs {
					return nil
				}
				rk := st[ins.A]
				if wantRet != bkUnknown && rk != wantRet {
					return nil
				}
				if rk != bkInt64 && rk != bkBool {
					return nil
				}
				if retKind != bkUnknown && retKind != rk {
					return nil
				}
				retKind = rk
			default:
				return nil
			}

			if fallthroughNext && idx+1 < insCount {
				if mergeInto(idx+1, st, ro) {
					progress = true
				}
			}
		}
		if !progress {
			break
		}
	}

	if wantRet != bkUnknown && retKind != wantRet {
		return nil
	}

	var intUpvars, writtenUpvars []uint32
	for u := range intUpSet {
		intUpvars = append(intUpvars, u)
	}
	sort.Slice(intUpvars, func(i, j int) bool { return intUpvars[i] < intUpvars[j] })
	for u := range writtenUpSet {
		writtenUpvars = append(writtenUpvars, u)
	}
	sort.Slice(writtenUpvars, func(i, j int) bool { return writtenUpvars[i] < writtenUpvars[j] })

	return &typedBlockLoop{
		child:           child,
		m1:              m1,
		hasEnter:        hasEnter,
		bodyStart:       bodyStart,
		nregs:           nregs,
		inKinds:         inKinds,
		inRoSlot:        inRoSlot,
		roReceiverSlots: roReceiverSlots,
		retKind:         retKind,
		intUpvars:       intUpvars,
		writtenUpvars:   writtenUpvars,
		pureAccessors:   pureAccessors,
		insAccIdx:       insAccIdx,
		usesI:           usesI,
		usesB:           usesB,
		usesOk:          usesOk,
	}
}

func (e *unitEmitter) emitTypedBlockBody(tb *typedBlockLoop, site int, yieldExprs []string, retVar string, deoptLbl string) {
	child := tb.child
	jumpTargets := make(map[int]bool)
	for _, ins := range child.Instructions {
		if ins.IsJump() && ins.Target >= 0 {
			jumpTargets[ins.Target] = true
		}
	}

	var body bytes.Buffer
	refs := make(map[string]bool)
	lbl := func(off int) string {
		l := fmt.Sprintf("TB%d_%04d", site, off)
		refs[l] = true
		return l
	}
	nextLbl := fmt.Sprintf("TB%d_next", site)
	deoptStmt := fmt.Sprintf("_ti = _i; goto %s", deoptLbl)
	useI, useB, useOk := tb.usesI, tb.usesB, tb.usesOk
	if len(yieldExprs) > 0 {
		useI = true
	}

	for idx := tb.bodyStart; idx < len(child.Instructions); idx++ {
		ins := child.Instructions[idx]
		if jumpTargets[ins.Offset] {
			fmt.Fprintf(&body, "TB%d_%04d:\n", site, ins.Offset)
		}
		kinds := tb.inKinds[idx]
		if kinds == nil {
			continue
		}
		switch ins.Op {
		case OpNOP, OpENTER:
		case OpMOVE:
			if kinds[ins.B] == bkBool {
				useB = true
				fmt.Fprintf(&body, "\trb[%d] = rb[%d]\n", ins.A, ins.B)
			} else if kinds[ins.B] == bkInt64 {
				useI = true
				fmt.Fprintf(&body, "\tri[%d] = ri[%d]\n", ins.A, ins.B)
			}
		case OpLOADI8:
			useI = true
			fmt.Fprintf(&body, "\tri[%d] = %d\n", ins.A, ins.B)
		case OpLOADINEG:
			useI = true
			fmt.Fprintf(&body, "\tri[%d] = -%d\n", ins.A, ins.B)
		case OpLOADI__1:
			useI = true
			fmt.Fprintf(&body, "\tri[%d] = -1\n", ins.A)
		case OpLOADI_0, OpLOADI_1, OpLOADI_2, OpLOADI_3, OpLOADI_4, OpLOADI_5, OpLOADI_6, OpLOADI_7:
			useI = true
			fmt.Fprintf(&body, "\tri[%d] = %d\n", ins.A, int(ins.Op-OpLOADI_0))
		case OpLOADI16:
			useI = true
			fmt.Fprintf(&body, "\tri[%d] = %d\n", ins.A, int16(ins.B))
		case OpLOADI32:
			useI = true
			fmt.Fprintf(&body, "\tri[%d] = %d\n", ins.A, int32(uint32(ins.B)<<16|uint32(ins.C)))
		case OpLOADL:
			useI = true
			switch v := child.Pool[ins.B].(type) {
			case int32:
				fmt.Fprintf(&body, "\tri[%d] = %d\n", ins.A, v)
			case int64:
				fmt.Fprintf(&body, "\tri[%d] = %d\n", ins.A, v)
			}
		case OpLOADTRUE:
			useB = true
			fmt.Fprintf(&body, "\trb[%d] = true\n", ins.A)
		case OpLOADFALSE:
			useB = true
			fmt.Fprintf(&body, "\trb[%d] = false\n", ins.A)
		case OpGETUPVAR:
			if !tb.roReceiverSlots[uint32(ins.B)] {
				useI = true
				fmt.Fprintf(&body, "\tri[%d] = _uv%d\n", ins.A, ins.B)
			}
		case OpSETUPVAR:
			useI = true
			fmt.Fprintf(&body, "\t_uv%d = ri[%d]\n", ins.B, ins.A)
		case OpADD, OpSUB, OpMUL, OpDIV:
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
			fmt.Fprintf(&body, "\tri[%d], _ok = %s(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, fn, ins.A, ins.A+1, deoptStmt)
		case OpADDI, OpADDILV, OpSUBI, OpSUBILV:
			imm := int64(ins.B)
			if ins.Op == OpADDILV || ins.Op == OpSUBILV {
				imm = int64(ins.C)
			}
			fn := "aotIntAdd64"
			if ins.Op == OpSUBI || ins.Op == OpSUBILV {
				fn = "aotIntSub64"
			}
			useI = true
			useOk = true
			fmt.Fprintf(&body, "\tri[%d], _ok = %s(ri[%d], %d); if !_ok { %s }\n", ins.A, fn, ins.A, imm, deoptStmt)
		case OpEQ, OpLT, OpLE, OpGT, OpGE:
			opStr := cmpOpStr(ins.Op)
			lhs, rhs := fmt.Sprintf("ri[%d]", ins.A), fmt.Sprintf("ri[%d]", ins.A+1)
			if kinds[ins.A] == bkBool {
				useB = true
				lhs, rhs = fmt.Sprintf("rb[%d]", ins.A), fmt.Sprintf("rb[%d]", ins.A+1)
			} else {
				useI = true
			}
			if idx+1 < len(child.Instructions) {
				nx := child.Instructions[idx+1]
				if !jumpTargets[nx.Offset] && (nx.Op == OpJMPNOT || nx.Op == OpJMPIF) && nx.A == ins.A {
					backPoll := ""
					if nx.Target <= nx.Offset {
						backPoll = fmt.Sprintf("if mrb.AotHookPending() { %s }; ", deoptStmt)
					}
					if nx.Op == OpJMPNOT {
						fmt.Fprintf(&body, "\tif !(%s %s %s) { %sgoto %s }\n", lhs, opStr, rhs, backPoll, lbl(nx.Target))
					} else {
						fmt.Fprintf(&body, "\tif %s %s %s { %sgoto %s }\n", lhs, opStr, rhs, backPoll, lbl(nx.Target))
					}
					idx++
					continue
				}
			}
			useB = true
			fmt.Fprintf(&body, "\trb[%d] = %s %s %s\n", ins.A, lhs, opStr, rhs)
		case OpJMP:
			if ins.Target <= ins.Offset {
				fmt.Fprintf(&body, "\tif mrb.AotHookPending() { %s }\n", deoptStmt)
			}
			fmt.Fprintf(&body, "\tgoto %s\n", lbl(ins.Target))
		case OpJMPIF:
			useB = true
			backPoll := ""
			if ins.Target <= ins.Offset {
				backPoll = fmt.Sprintf("if mrb.AotHookPending() { %s }; ", deoptStmt)
			}
			fmt.Fprintf(&body, "\tif rb[%d] { %sgoto %s }\n", ins.A, backPoll, lbl(ins.Target))
		case OpJMPNOT:
			useB = true
			backPoll := ""
			if ins.Target <= ins.Offset {
				backPoll = fmt.Sprintf("if mrb.AotHookPending() { %s }; ", deoptStmt)
			}
			fmt.Fprintf(&body, "\tif !rb[%d] { %sgoto %s }\n", ins.A, backPoll, lbl(ins.Target))
		case OpSEND, OpSEND0:
			sym := child.Syms[ins.B]
			if accIdx, isAcc := tb.insAccIdx[idx]; isAcc {
				useI = true
				fmt.Fprintf(&body, "\tif _av := *_ap%d; _av.Type() == oruby.MrbTTInteger { ri[%d] = aotInt(_av) } else { %s }\n", accIdx, ins.A, deoptStmt)
				break
			}
			switch sym {
			case "%":
				useI, useOk = true, true
				fmt.Fprintf(&body, "\tri[%d], _ok = aotIntMod64(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, ins.A, ins.A+1, deoptStmt)
			case "<<":
				useI, useOk = true, true
				fmt.Fprintf(&body, "\tri[%d], _ok = aotIntShl64(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, ins.A, ins.A+1, deoptStmt)
			case ">>":
				useI, useOk = true, true
				fmt.Fprintf(&body, "\tri[%d], _ok = aotIntShr64(ri[%d], ri[%d]); if !_ok { %s }\n", ins.A, ins.A, ins.A+1, deoptStmt)
			case "&", "|", "^":
				useI = true
				fmt.Fprintf(&body, "\tri[%d] = ri[%d] %s ri[%d]\n", ins.A, ins.A, sym, ins.A+1)
			case "!=":
				useB = true
				if kinds[ins.A] == bkBool {
					fmt.Fprintf(&body, "\trb[%d] = rb[%d] != rb[%d]\n", ins.A, ins.A, ins.A+1)
				} else {
					useI = true
					fmt.Fprintf(&body, "\trb[%d] = ri[%d] != ri[%d]\n", ins.A, ins.A, ins.A+1)
				}
			case "-@":
				useI = true
				fmt.Fprintf(&body, "\tif ri[%d] == aotMinInt { %s }; ri[%d] = -ri[%d]\n", ins.A, deoptStmt, ins.A, ins.A)
			case "+@":
			}
		case OpRETURN:
			if retVar != "" {
				if tb.retKind == bkBool {
					useB = true
					fmt.Fprintf(&body, "\t%s = rb[%d]\n", retVar, ins.A)
				} else {
					useI = true
					fmt.Fprintf(&body, "\t%s = ri[%d]\n", retVar, ins.A)
				}
			}
			if idx < len(child.Instructions)-1 {
				refs[nextLbl] = true
				fmt.Fprintf(&body, "\tgoto %s\n", nextLbl)
			}
		}
	}

	lblPrefix := fmt.Sprintf("TB%d_", site)
	var sb strings.Builder
	for _, line := range strings.SplitAfter(body.String(), "\n") {
		if strings.HasPrefix(line, lblPrefix) && strings.HasSuffix(line, ":\n") {
			if !refs[strings.TrimSuffix(line, ":\n")] {
				continue
			}
		}
		sb.WriteString(line)
	}

	e.w("\t\t\t{\n")
	if useI {
		e.w("\t\t\t\tvar ri [%d]int64\n\t\t\t\t_ = ri\n", tb.nregs)
	}
	if useB {
		e.w("\t\t\t\tvar rb [%d]bool\n\t\t\t\t_ = rb\n", tb.nregs)
	}
	if useOk {
		e.w("\t\t\t\tvar _ok bool\n\t\t\t\t_ = _ok\n")
	}
	for i := 0; i < tb.m1 && i < len(yieldExprs); i++ {
		e.w("\t\t\t\tri[%d] = %s\n", i+1, yieldExprs[i])
	}
	e.b.WriteString(sb.String())
	if refs[nextLbl] {
		e.w("%s:\n\t\t\t\t_ = 0\n", nextLbl)
	}
	e.w("\t\t\t}\n")
}
