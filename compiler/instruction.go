package compiler

import "fmt"

// Instruction represents a decoded virtual machine instruction.
type Instruction struct {
	Offset int    // Byte offset in the bytecode stream
	Op     Opcode // The operation code
	A      uint32 // Primary operand / register
	B      uint16 // Secondary operand
	C      uint16 // Tertiary operand
	Target int    // Absolute byte offset for branch/jump targets (-1 if not a jump)
	Ext    uint8  // 0: none, 1: EXT1, 2: EXT2, 3: EXT3
}

// IsJump returns true if the instruction is a control-flow jump.
func (ins Instruction) IsJump() bool {
	switch ins.Op {
	case OpJMP, OpJMPIF, OpJMPNOT, OpJMPNIL, OpJMPUW:
		return true
	default:
		return false
	}
}

// IsReturn returns true if the instruction returns from the current frame.
func (ins Instruction) IsReturn() bool {
	switch ins.Op {
	case OpRETURN, OpRETURN_BLK, OpRETSELF, OpRETNIL, OpRETTRUE, OpRETFALSE:
		return true
	default:
		return false
	}
}

// String formats the instruction in assembly syntax similar to mruby's codedump.
func (ins Instruction) String() string {
	switch ins.Op.Format() {
	case FormatZ:
		return fmt.Sprintf("%04d %s", ins.Offset, ins.Op)
	case FormatB:
		return fmt.Sprintf("%04d %s R%d", ins.Offset, ins.Op, ins.A)
	case FormatBB:
		if ins.Op == OpLOADL {
			return fmt.Sprintf("%04d %s R%d Pool[%d]", ins.Offset, ins.Op, ins.A, ins.B)
		}
		if ins.Op == OpLOADSYM || ins.Op == OpGETCONST || ins.Op == OpSETCONST ||
			ins.Op == OpGETIV || ins.Op == OpSETIV || ins.Op == OpGETGV || ins.Op == OpSETGV {
			return fmt.Sprintf("%04d %s R%d Syms[%d]", ins.Offset, ins.Op, ins.A, ins.B)
		}
		return fmt.Sprintf("%04d %s R%d %d", ins.Offset, ins.Op, ins.A, ins.B)
	case FormatBBB:
		if ins.Op == OpSEND || ins.Op == OpSSEND {
			n := ins.C & 0xf
			nk := (ins.C >> 4) & 0xf
			return fmt.Sprintf("%04d %s R%d Syms[%d] (n=%d, nk=%d)", ins.Offset, ins.Op, ins.A, ins.B, n, nk)
		}
		return fmt.Sprintf("%04d %s R%d %d %d", ins.Offset, ins.Op, ins.A, ins.B, ins.C)
	case FormatBS:
		if ins.IsJump() {
			return fmt.Sprintf("%04d %s R%d -> %04d", ins.Offset, ins.Op, ins.A, ins.Target)
		}
		return fmt.Sprintf("%04d %s R%d %d", ins.Offset, ins.Op, ins.A, ins.B)
	case FormatBSS:
		return fmt.Sprintf("%04d %s R%d %d %d", ins.Offset, ins.Op, ins.A, ins.B, ins.C)
	case FormatS:
		if ins.IsJump() {
			return fmt.Sprintf("%04d %s -> %04d", ins.Offset, ins.Op, ins.Target)
		}
		return fmt.Sprintf("%04d %s %d", ins.Offset, ins.Op, ins.A)
	case FormatW:
		return fmt.Sprintf("%04d %s 0x%x", ins.Offset, ins.Op, ins.A)
	default:
		return fmt.Sprintf("%04d %s", ins.Offset, ins.Op)
	}
}
