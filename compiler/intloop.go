package compiler

import (
	"fmt"
	"sort"
	"strings"
)

// Pure integer loops.
//
// The generic code keeps every register in a 16 byte boxed Value and checks
// every operation against redefinition. A `while` loop whose body is nothing
// but integer arithmetic, comparisons and jumps (no call, so no code that
// could redefine an operator or run a signal handler) is also compiled to a
// second form on unboxed int64 locals. The form is tried first, behind one
// guard at entry: every register the loop reads before writing holds an
// Integer, the operators it uses are the original ones, and nothing else is
// running (AotIntCanRun). The loop can leave it two ways:
//   - normally, by one of its exits: the registers it wrote are boxed back;
//   - by a deopt, when an operation would overflow int64 (the result is a
//     bignum, which the generic code makes) or a hook is armed (a signal, a
//     Timeout, a thread switch): the loop-carried registers go back to the
//     values they had at the start of the iteration and the generic loop,
//     which follows the typed one and is unchanged, goes on from its header.
// Because the loop is pure, nothing happened that the generic code would not
// repeat.

type regKind uint8

const (
	kUnknown regKind = iota
	kInt
	kBool
	kConflict
)

type intLoop struct {
	head, tail int                        // instruction indexes of the header and of the back jump
	liveIn     []uint32                   // registers read before written: Integers at entry
	carried    []uint32                   // live in and written: restored at a deopt
	modified   []uint32                   // every register the loop writes
	used       []uint32                   // every register it touches
	guards     []string                   // core methods it relies on (AotCoreInt...)
	exits      map[int]map[uint32]regKind // exit jump instruction index -> kinds at it
	moveBool   map[int]bool               // MOVE instruction index -> it moves a Bool
	dead       map[int]bool               // instructions no path reaches (after a jump, before a label): not emitted
	boolRegs   []uint32                   // registers the loop writes a Bool to
}

var intLoopBitOps = map[string]string{
	"&": "AotCoreIntAnd", "|": "AotCoreIntOr", "^": "AotCoreIntXor",
	"<<": "AotCoreIntShl", ">>": "AotCoreIntShr", "%": "AotCoreIntMod",
}

// findIntLoops finds the pure integer loops of u, by header instruction.
func findIntLoops(u *IRepUnit) map[int]*intLoop {
	out := map[int]*intLoop{}
	offIdx := make(map[int]int, len(u.Instructions))
	for i, ins := range u.Instructions {
		offIdx[ins.Offset] = i
	}
	for j, ins := range u.Instructions {
		if ins.Op != OpJMP || ins.Target < 0 || ins.Target >= ins.Offset {
			continue
		}
		h, ok := offIdx[ins.Target]
		if !ok || out[h] != nil {
			continue
		}
		if l := analyzeIntLoop(u, h, j, offIdx); l != nil {
			out[h] = l
		}
	}
	// a loop inside another typed loop is part of it: keep the outer one
	for h, l := range out {
		for h2, l2 := range out {
			if h != h2 && h2 > l.head && l2.tail < l.tail {
				delete(out, h2)
			}
		}
	}
	return out
}

func analyzeIntLoop(u *IRepUnit, h, j int, offIdx map[int]int) *intLoop {
	headOff, tailOff := u.Instructions[h].Offset, u.Instructions[j].Offset
	for _, c := range u.Catch { // no begin/rescue/ensure around it
		if c.Begin <= tailOff && c.End >= headOff {
			return nil
		}
	}
	kinds := map[uint32]regKind{}
	liveIn := map[uint32]bool{}
	modified := map[uint32]bool{}
	used := map[uint32]bool{}
	guards := map[string]bool{}
	pending := map[int]map[uint32]regKind{} // in region forward jump targets: state there
	exits := map[int]map[uint32]regKind{}
	moveBool := map[int]bool{}
	dead := map[int]bool{}
	boolRegs := map[uint32]bool{}
	reachable := true
	bad := false

	clone := func(m map[uint32]regKind) map[uint32]regKind {
		c := make(map[uint32]regKind, len(m))
		for k, v := range m {
			c[k] = v
		}
		return c
	}
	merge := func(a, b map[uint32]regKind) map[uint32]regKind {
		out := clone(a)
		for k, v := range b {
			if out[k] != v {
				out[k] = kConflict
			}
		}
		for k := range a {
			if _, ok := b[k]; !ok && out[k] != kConflict {
				out[k] = kConflict
			}
		}
		return out
	}
	read := func(r uint32, want regKind) {
		used[r] = true
		k := kinds[r]
		switch {
		case k == kUnknown && want == kInt && !modified[r]:
			liveIn[r] = true // an Integer at entry
			kinds[r] = kInt
		case k != want:
			bad = true
		}
	}
	write := func(r uint32, k regKind) {
		used[r] = true
		modified[r] = true
		kinds[r] = k
	}
	jump := func(i int, t int) {
		if idx, in := offIdx[t]; in && idx > h && idx <= j {
			if t <= u.Instructions[i].Offset {
				bad = true // an inner backward jump
				return
			}
			if p, ok := pending[t]; ok {
				pending[t] = merge(p, kinds)
			} else {
				pending[t] = clone(kinds)
			}
			return
		}
		exits[i] = clone(kinds) // leaves the loop
	}

	for i := h; i <= j && !bad; i++ {
		ins := u.Instructions[i]
		if p, ok := pending[ins.Offset]; ok {
			if reachable {
				kinds = merge(kinds, p)
			} else {
				kinds = clone(p)
			}
			reachable = true
			delete(pending, ins.Offset)
		}
		if !reachable {
			dead[i] = true
			continue
		}
		a := ins.A
		switch ins.Op {
		case OpNOP:
		case OpMOVE:
			b := uint32(ins.B)
			used[b] = true
			k := kinds[b]
			if k == kUnknown && !modified[b] {
				liveIn[b] = true
				kinds[b] = kInt
				k = kInt
			}
			if k != kInt && k != kBool {
				bad = true
				break
			}
			if k == kBool {
				moveBool[i] = true
				boolRegs[a] = true
			}
			write(a, k)
		case OpLOADI8, OpLOADINEG, OpLOADI__1, OpLOADI_0, OpLOADI_1, OpLOADI_2, OpLOADI_3,
			OpLOADI_4, OpLOADI_5, OpLOADI_6, OpLOADI_7, OpLOADI16, OpLOADI32:
			write(a, kInt)
		case OpLOADL:
			if int(ins.B) >= len(u.Pool) {
				bad = true
				break
			}
			switch u.Pool[ins.B].(type) {
			case int32, int64:
				write(a, kInt)
			default:
				bad = true
			}
		case OpADD, OpSUB, OpMUL:
			read(a, kInt)
			read(a+1, kInt)
			write(a, kInt)
		case OpADDI, OpSUBI, OpADDILV, OpSUBILV:
			read(a, kInt)
			write(a, kInt)
		case OpEQ, OpLT, OpLE, OpGT, OpGE:
			read(a, kInt)
			read(a+1, kInt)
			write(a, kBool)
			boolRegs[a] = true
		case OpSEND:
			name := ""
			if int(ins.B) < len(u.Syms) {
				name = u.Syms[ins.B]
			}
			if ins.C&0xff != 1 {
				bad = true
				break
			}
			if g, ok := intLoopBitOps[name]; ok {
				read(a, kInt)
				read(a+1, kInt)
				write(a, kInt)
				guards[g] = true
			} else if name == "!=" {
				read(a, kInt)
				read(a+1, kInt)
				write(a, kBool)
				boolRegs[a] = true
			} else {
				bad = true
			}
		case OpJMP:
			if ins.Target < 0 {
				bad = true
				break
			}
			if i == j {
				// the back jump: every loop-carried register is an Integer again
				for r := range liveIn {
					if kinds[r] != kInt {
						bad = true
					}
				}
			} else {
				jump(i, ins.Target)
			}
			reachable = false
		case OpJMPIF, OpJMPNOT:
			read(a, kBool)
			if ins.Target < 0 {
				bad = true
				break
			}
			jump(i, ins.Target)
		default:
			bad = true
		}
	}
	if bad || len(pending) > 0 || len(exits) == 0 || dead[j] {
		return nil // dead[j]: the back jump is never reached, so it is not a loop
	}
	l := &intLoop{head: h, tail: j, exits: exits, moveBool: moveBool, dead: dead}
	for r := range boolRegs {
		l.boolRegs = append(l.boolRegs, r)
	}
	for r := range liveIn {
		l.liveIn = append(l.liveIn, r)
		if modified[r] {
			l.carried = append(l.carried, r)
		}
	}
	for r := range modified {
		l.modified = append(l.modified, r)
	}
	for r := range used {
		l.used = append(l.used, r)
	}
	for g := range guards {
		l.guards = append(l.guards, g)
	}
	for _, s := range [][]uint32{l.liveIn, l.carried, l.modified, l.used, l.boolRegs} {
		sort.Slice(s, func(x, y int) bool { return s[x] < s[y] })
	}
	sort.Strings(l.guards)
	// Only the named locals (registers below nlocals) can be read after the
	// loop; the compiler's temporaries are dead at its exits. A local the loop
	// writes before it reads it (an assignment in the body) might be read
	// after the loop, with the value of the last iteration it was written in:
	// not handled, the loop stays generic. A loop-carried local has one known
	// kind at every exit.
	for _, r := range l.modified {
		if int(r) < u.NLocals && !liveIn[r] {
			return nil
		}
	}
	for _, st := range exits {
		for _, r := range l.carried {
			if k := st[r]; k == kConflict {
				return nil
			}
		}
	}
	return l
}

// emitIntLoop writes the typed form of l; it falls into the generic loop's
// header when it does not apply or deopts.
func (e *unitEmitter) emitIntLoop(l *intLoop) {
	u := e.u
	headOff := u.Instructions[l.head].Offset
	// the block goes to a buffer first: Go rejects a label nothing jumps to, so
	// the ones no goto names are dropped before it is written
	var buf strings.Builder
	w := func(format string, args ...interface{}) { fmt.Fprintf(&buf, format, args...) }
	defer func() {
		text := buf.String()
		var out []string
		for _, line := range strings.Split(text, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "Tt_") || strings.HasPrefix(trim, "Tx_") || strings.HasPrefix(trim, "Td_") {
				if name := strings.TrimSuffix(trim, ":"); name != trim && !strings.Contains(text, "goto "+name) {
					continue
				}
			}
			out = append(out, line)
		}
		e.w("%s", strings.Join(out, "\n"))
	}()
	isLive := map[uint32]bool{}
	for _, r := range l.liveIn {
		isLive[r] = true
	}
	hasBool := map[uint32]bool{}
	for _, r := range l.boolRegs {
		hasBool[r] = true
	}
	// labels the loop uses: its header, and the in loop targets of its branches
	labels := map[int]bool{headOff: true}
	for i := l.head; i <= l.tail; i++ {
		ins := u.Instructions[i]
		if _, exit := l.exits[i]; !exit && !l.dead[i] && (ins.Op == OpJMPIF || ins.Op == OpJMPNOT || (ins.Op == OpJMP && i != l.tail)) {
			labels[ins.Target] = true
		}
	}
	needDeopt := false
	deopt := func() string { needDeopt = true; return fmt.Sprintf("Td_%04d", headOff) }

	var cond []string
	cond = append(cond, "mrb.AotIntCanRun()")
	for _, r := range l.liveIn {
		cond = append(cond, fmt.Sprintf("r[%d].Type() == oruby.MrbTTInteger", r))
	}
	if len(l.guards) > 0 { // % & | ^ << >>: one mask test of the extra bop slots
		cond = append(cond, "!mrb.AotIntBitOpsRedefined()")
	}
	w("\t// typed form of the integer loop at %04d\n", headOff)
	w("\tif %s {\n", strings.Join(cond, " && "))
	var ints, bools []string
	for _, r := range l.used {
		if !isLive[r] {
			ints = append(ints, fmt.Sprintf("ti%d", r))
		}
		if hasBool[r] {
			bools = append(bools, fmt.Sprintf("tb%d", r))
		}
	}
	for _, r := range l.liveIn {
		w("\t\tti%d := aotInt(r[%d])\n", r, r)
	}
	if len(ints) > 0 {
		w("\t\tvar %s int64\n", strings.Join(ints, ", "))
		for _, v := range ints {
			w("\t\t_ = %s\n", v)
		}
	}
	if len(bools) > 0 {
		w("\t\tvar %s bool\n", strings.Join(bools, ", "))
		for _, v := range bools {
			w("\t\t_ = %s\n", v)
		}
	}
	var snaps []string
	for _, r := range l.carried {
		snaps = append(snaps, fmt.Sprintf("ts%d", r))
	}
	if len(snaps) > 0 {
		w("\t\tvar %s int64\n", strings.Join(snaps, ", "))
	}
	// a register boxed back as the kind it has where the loop is left
	writeBack := func(regs []uint32, st map[uint32]regKind) {
		for _, r := range regs {
			if st != nil && st[r] == kBool {
				w("\t\tr[%d] = mrb.BoolValue(tb%d)\n", r, r)
			} else {
				w("\t\tr[%d] = oruby.AotFixnum(ti%d)\n", r, r)
			}
		}
	}
	// the iteration
	w("\tTt_%04d:\n", headOff)
	w("\t\tif mrb.AotHookPending() {\n")
	writeBack(l.carried, nil)
	w("\t\t\tgoto L_%04d\n\t\t}\n", headOff)
	for _, r := range l.carried {
		w("\t\tts%d = ti%d\n", r, r)
	}
	for i := l.head; i <= l.tail; i++ {
		ins := u.Instructions[i]
		a := ins.A
		if i != l.head && labels[ins.Offset] {
			w("\tTt_%04d:\n", ins.Offset)
		}
		if l.dead[i] {
			continue
		}
		switch ins.Op {
		case OpNOP:
		case OpMOVE:
			if l.moveBool[i] {
				w("\t\ttb%d = tb%d\n", a, ins.B)
			} else {
				w("\t\tti%d = ti%d\n", a, ins.B)
			}
		case OpLOADI8:
			w("\t\tti%d = %d\n", a, ins.B)
		case OpLOADINEG:
			w("\t\tti%d = -%d\n", a, ins.B)
		case OpLOADI__1:
			w("\t\tti%d = -1\n", a)
		case OpLOADI_0, OpLOADI_1, OpLOADI_2, OpLOADI_3, OpLOADI_4, OpLOADI_5, OpLOADI_6, OpLOADI_7:
			w("\t\tti%d = %d\n", a, int(ins.Op-OpLOADI_0))
		case OpLOADI16:
			w("\t\tti%d = %d\n", a, int16(ins.B))
		case OpLOADI32:
			w("\t\tti%d = %d\n", a, int32(uint32(ins.B)<<16|uint32(ins.C)))
		case OpLOADL:
			switch v := u.Pool[ins.B].(type) {
			case int32:
				w("\t\tti%d = %d\n", a, v)
			case int64:
				w("\t\tti%d = %d\n", a, v)
			}
		case OpADD:
			w("\t\t{ z := ti%d + ti%d; if (ti%d^z)&(ti%d^z) < 0 { goto %s }; ti%d = z }\n", a, a+1, a, a+1, deopt(), a)
		case OpSUB:
			w("\t\t{ z := ti%d - ti%d; if (ti%d^ti%d)&(ti%d^z) < 0 { goto %s }; ti%d = z }\n", a, a+1, a, a+1, a, deopt(), a)
		case OpMUL:
			w("\t\t{ x, y := ti%d, ti%d; if x > -(1<<31) && x < 1<<31 && y > -(1<<31) && y < 1<<31 { ti%d = x * y } else { z := x * y; if x != 0 && (z/x != y || (x == -1 && y == aotMinInt)) { goto %s }; ti%d = z } }\n",
				a, a+1, a, deopt(), a)
		case OpADDI, OpSUBI, OpADDILV, OpSUBILV:
			imm := int(ins.B)
			if ins.Op == OpADDILV || ins.Op == OpSUBILV {
				imm = int(ins.C)
			}
			if ins.Op == OpSUBI || ins.Op == OpSUBILV {
				imm = -imm
			}
			w("\t\t{ x := ti%d; z := x + int64(%d); if (x^z)&(int64(%d)^z) < 0 { goto %s }; ti%d = z }\n", a, imm, imm, deopt(), a)
		case OpEQ, OpLT, OpLE, OpGT, OpGE:
			w("\t\ttb%d = ti%d %s ti%d\n", a, a, cmpOpStr(ins.Op), a+1)
		case OpSEND:
			name := u.Syms[ins.B]
			switch name {
			case "&":
				w("\t\tti%d = ti%d & ti%d\n", a, a, a+1)
			case "|":
				w("\t\tti%d = ti%d | ti%d\n", a, a, a+1)
			case "^":
				w("\t\tti%d = ti%d ^ ti%d\n", a, a, a+1)
			case "!=":
				w("\t\ttb%d = ti%d != ti%d\n", a, a, a+1)
			case ">>":
				w("\t\t{ s := ti%d; if s < 0 || s > 63 { goto %s }; ti%d = ti%d >> uint(s) }\n", a+1, deopt(), a, a)
			case "<<":
				w("\t\t{ s := ti%d; if s < 0 || s > 62 { goto %s }; z := ti%d << uint(s); if z>>uint(s) != ti%d { goto %s }; ti%d = z }\n", a+1, deopt(), a, a, deopt(), a)
			case "%%":
			case "%":
				w("\t\t{ x, y := ti%d, ti%d; if y == 0 || (x == aotMinInt && y == -1) { goto %s }; m := x %% y; if m != 0 && (m < 0) != (y < 0) { m += y }; ti%d = m }\n", a, a+1, deopt(), a)
			}
		case OpJMP:
			if i == l.tail {
				w("\t\tgoto Tt_%04d\n", headOff)
			} else {
				w("\t\tgoto %s\n", e.intLoopTarget(l, i, ins.Target))
			}
		case OpJMPIF, OpJMPNOT:
			neg := ""
			if ins.Op == OpJMPNOT {
				neg = "!"
			}
			w("\t\tif %stb%d { goto %s }\n", neg, a, e.intLoopTarget(l, i, ins.Target))
		}
	}
	// exits: the registers the loop wrote are boxed back
	exitIdx := make([]int, 0, len(l.exits))
	for i := range l.exits {
		exitIdx = append(exitIdx, i)
	}
	sort.Ints(exitIdx)
	for _, i := range exitIdx {
		w("\tTx_%04d:\n", u.Instructions[i].Offset)
		writeBack(l.carried, l.exits[i])
		w("\t\tgoto L_%04d\n", u.Instructions[i].Target)
	}
	// deopt: the loop-carried registers back to the start of the iteration
	if needDeopt {
		w("\tTd_%04d:\n", headOff)
		for _, r := range l.carried {
			w("\t\tti%d = ts%d\n", r, r)
		}
		writeBack(l.carried, nil)
		w("\t\tgoto L_%04d\n", headOff)
	}
	w("\t}\n")
}

// intLoopTarget names the label a jump of the loop goes to: inside the loop
// its typed instruction, outside it the exit that boxes the registers back.
func (e *unitEmitter) intLoopTarget(l *intLoop, i, target int) string {
	if _, exit := l.exits[i]; exit {
		return fmt.Sprintf("Tx_%04d", e.u.Instructions[i].Offset)
	}
	return fmt.Sprintf("Tt_%04d", target)
}
