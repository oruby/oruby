package compiler

import (
	"fmt"
	"strings"
)

// IRepUnit represents a compiled procedure or method block (mrb_irep).
type IRepUnit struct {
	Index        int            // Unit index (0 is top-level script)
	Name         string         // Inferred or defined name (e.g. method or block)
	File         string         // Source filename from debug info
	NLocals      int            // Number of local variables
	NRegs        int            // Number of virtual registers
	Flags        int            // Irep flags
	Locals       []string       // Local variable names (Locals[i] corresponds to r[i+1])
	Instructions []Instruction  // Decoded instruction sequence
	Pool         []interface{}  // Constant pool literals
	Syms         []string       // Symbol table
	Children     []*IRepUnit    // Nested procedures (methods, blocks)
	Catch        []CatchHandler // begin/rescue/ensure handler table
}

// Catch handler types (enum mrb_catch_type)
const (
	CatchRescue = 0
	CatchEnsure = 1
)

// CatchHandler maps bytecode range [Begin, End) to handler code at Target.
type CatchHandler struct {
	Type   int
	Begin  int
	End    int
	Target int
}

// Disassemble returns a human-readable disassembly of the unit and its children.
func (u *IRepUnit) Disassemble() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== irep %d (nregs: %d, nlocals: %d, iseq: %d insns, pool: %d, syms: %d, reps: %d) ===\n",
		u.Index, u.NRegs, u.NLocals, len(u.Instructions), len(u.Pool), len(u.Syms), len(u.Children)))

	if len(u.Syms) > 0 {
		sb.WriteString("symbols:\n")
		for i, sym := range u.Syms {
			sb.WriteString(fmt.Sprintf("  [%02d] :%s\n", i, sym))
		}
	}

	if len(u.Pool) > 0 {
		sb.WriteString("pool:\n")
		for i, val := range u.Pool {
			sb.WriteString(fmt.Sprintf("  [%02d] %#v\n", i, val))
		}
	}

	sb.WriteString("code:\n")
	for _, ins := range u.Instructions {
		sb.WriteString("  ")
		sb.WriteString(ins.String())
		sb.WriteString("\n")
	}

	for _, child := range u.Children {
		sb.WriteString("\n")
		sb.WriteString(child.Disassemble())
	}

	return sb.String()
}
