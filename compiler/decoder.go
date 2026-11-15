package compiler

import (
	"errors"
	"fmt"

	"github.com/oruby/oruby"
)

// Decoder converts an mruby compiled IRep into an intermediate representation (IRepUnit).
type Decoder struct {
	mrb *oruby.MrbState
}

// NewDecoder creates a new IRep decoder.
func NewDecoder(mrb *oruby.MrbState) *Decoder {
	return &Decoder{mrb: mrb}
}

// Decode translates an MrbIrep into an IRepUnit tree.
func (d *Decoder) Decode(irep oruby.MrbIrep) (*IRepUnit, error) {
	if irep.IsNil() {
		return nil, errors.New("nil irep provided to decoder")
	}
	unitIdx := 0
	return d.decodeUnit(irep, &unitIdx)
}

func (d *Decoder) decodeUnit(irep oruby.MrbIrep, unitCounter *int) (*IRepUnit, error) {
	currIdx := *unitCounter
	*unitCounter++

	unit := &IRepUnit{
		Index:   currIdx,
		NLocals: irep.NLocals(),
		NRegs:   irep.NRegs(),
		Flags:   irep.Flags(),
	}

	// 1. Decode Symbols
	sLen := irep.SLen()
	unit.Syms = make([]string, sLen)
	for i := 0; i < sLen; i++ {
		sym := irep.Syms(i)
		unit.Syms[i] = d.mrb.SymName(sym)
	}

	// 2. Decode Constant Pool
	pLen := irep.PLen()
	unit.Pool = make([]interface{}, pLen)
	for i := 0; i < pLen; i++ {
		unit.Pool[i] = irep.Pool(i).Value()
	}

	// 3. Decode Bytecode Instructions
	iseq := irep.ISeq()
	insns, err := d.decodeISeq(iseq)
	if err != nil {
		return nil, fmt.Errorf("decoding unit %d iseq: %w", currIdx, err)
	}
	for i := range insns {
		if line, file, ok := d.mrb.DebugGetPosition(irep, uint32(insns[i].Offset)); ok {
			insns[i].Line = int(line)
			if unit.File == "" && file != "" {
				unit.File = file
			}
		}
	}
	unit.Instructions = insns

	// 3b. Decode Local Variable Names
	if irep.NLocals() > 1 {
		unit.Locals = make([]string, irep.NLocals()-1)
		for i := 0; i < irep.NLocals()-1; i++ {
			if sym := irep.Lv(i); sym != 0 {
				unit.Locals[i] = d.mrb.SymName(sym)
			}
		}
	}

	// 4. Decode Child Reps (Blocks, Methods, Lambdas)
	rLen := irep.RLen()
	unit.Children = make([]*IRepUnit, rLen)
	for i := 0; i < rLen; i++ {
		childUnit, err := d.decodeUnit(irep.Reps(i), unitCounter)
		if err != nil {
			return nil, fmt.Errorf("decoding child irep %d of unit %d: %w", i, currIdx, err)
		}
		unit.Children[i] = childUnit
	}

	return unit, nil
}

func (d *Decoder) decodeISeq(iseq []byte) ([]Instruction, error) {
	var insns []Instruction
	pc := 0
	seqLen := len(iseq)
	ext := uint8(0)

	for pc < seqLen {
		offset := pc
		opByte := iseq[pc]
		pc++

		op := Opcode(opByte)

		// Handle extension modifiers
		switch op {
		case OpEXT1:
			ext = 1
			continue
		case OpEXT2:
			ext = 2
			continue
		case OpEXT3:
			ext = 3
			continue
		}

		currentExt := ext
		ext = 0 // resets after current instruction

		ins := Instruction{
			Offset: offset,
			Op:     op,
			Ext:    currentExt,
			Target: -1,
		}

		// Helper closures to read 8, 16, 24 bits
		readB := func() (uint32, error) {
			if pc >= seqLen {
				return 0, fmt.Errorf("unexpected end of iseq reading 8-bit operand at offset %d", offset)
			}
			b := uint32(iseq[pc])
			pc++
			return b, nil
		}

		readS := func() (uint32, error) {
			if pc+1 >= seqLen {
				return 0, fmt.Errorf("unexpected end of iseq reading 16-bit operand at offset %d", offset)
			}
			s := uint32(iseq[pc])<<8 | uint32(iseq[pc+1])
			pc += 2
			return s, nil
		}

		readW := func() (uint32, error) {
			if pc+2 >= seqLen {
				return 0, fmt.Errorf("unexpected end of iseq reading 24-bit operand at offset %d", offset)
			}
			w := uint32(iseq[pc])<<16 | uint32(iseq[pc+1])<<8 | uint32(iseq[pc+2])
			pc += 3
			return w, nil
		}

		var err error

		switch op.Format() {
		case FormatZ:
			// No operands

		case FormatB:
			if currentExt == 1 {
				ins.A, err = readS()
			} else {
				ins.A, err = readB()
			}

		case FormatBB:
			switch currentExt {
			case 1:
				ins.A, err = readS()
				if err == nil {
					var b uint32
					b, err = readB()
					ins.B = uint16(b)
				}
			case 2:
				ins.A, err = readB()
				if err == nil {
					var b uint32
					b, err = readS()
					ins.B = uint16(b)
				}
			case 3:
				ins.A, err = readS()
				if err == nil {
					var b uint32
					b, err = readS()
					ins.B = uint16(b)
				}
			default:
				ins.A, err = readB()
				if err == nil {
					var b uint32
					b, err = readB()
					ins.B = uint16(b)
				}
			}

		case FormatBBB:
			switch currentExt {
			case 1:
				ins.A, err = readS()
				if err == nil {
					var b, c uint32
					b, err = readB()
					if err == nil {
						c, err = readB()
						ins.B = uint16(b)
						ins.C = uint16(c)
					}
				}
			case 2:
				ins.A, err = readB()
				if err == nil {
					var b, c uint32
					b, err = readS()
					if err == nil {
						c, err = readB()
						ins.B = uint16(b)
						ins.C = uint16(c)
					}
				}
			case 3:
				ins.A, err = readS()
				if err == nil {
					var b, c uint32
					b, err = readS()
					if err == nil {
						c, err = readB()
						ins.B = uint16(b)
						ins.C = uint16(c)
					}
				}
			default:
				ins.A, err = readB()
				if err == nil {
					var b, c uint32
					b, err = readB()
					if err == nil {
						c, err = readB()
						ins.B = uint16(b)
						ins.C = uint16(c)
					}
				}
			}

		case FormatBS:
			if currentExt == 1 || currentExt == 3 {
				ins.A, err = readS()
			} else {
				ins.A, err = readB()
			}
			if err == nil {
				var b uint32
				b, err = readS()
				ins.B = uint16(b)
			}

		case FormatBSS:
			if currentExt == 1 || currentExt == 3 {
				ins.A, err = readS()
			} else {
				ins.A, err = readB()
			}
			if err == nil {
				var b, c uint32
				b, err = readS()
				if err == nil {
					c, err = readS()
					ins.B = uint16(b)
					ins.C = uint16(c)
				}
			}

		case FormatS:
			ins.A, err = readS()

		case FormatW:
			ins.A, err = readW()
		}

		if err != nil {
			return nil, err
		}

		// Calculate jump targets if applicable
		if ins.IsJump() {
			switch op {
			case OpJMP, OpJMPUW:
				ins.Target = pc + int(int16(ins.A))
			case OpJMPIF, OpJMPNOT, OpJMPNIL:
				ins.Target = pc + int(int16(ins.B))
			}
		}

		insns = append(insns, ins)
	}

	return insns, nil
}
