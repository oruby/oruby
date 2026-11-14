package compiler

import (
	"testing"

	"github.com/oruby/oruby"
)

func TestDecoder_BasicArithmetic(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.SetNoExec(true)

	// Ruby code with variables so constant folding doesn't eliminate addition
	p, err := mrb.ParseString("a = 10; b = 20; x = a + b", cxt)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	proc, err := mrb.GenerateCode(p)
	if err != nil {
		t.Fatalf("GenerateCode error: %v", err)
	}

	decoder := NewDecoder(mrb)
	unit, err := decoder.Decode(proc.IRep())
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if len(unit.Instructions) == 0 {
		t.Fatalf("Expected non-empty instructions")
	}

	disasm := unit.Disassemble()
	t.Logf("Disassembly:\n%s", disasm)

	// Check that we found expected opcodes (LOADI, ADD, RETURN/STOP)
	hasAdd := false
	for _, ins := range unit.Instructions {
		if ins.Op == OpADD || ins.Op == OpADDI {
			hasAdd = true
		}
	}

	if !hasAdd {
		t.Errorf("Expected OpADD or OpADDI in instructions, got:\n%s", disasm)
	}
}

func TestDecoder_MethodDefinition(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.SetNoExec(true)

	rubyCode := `
def add(a, b)
  a + b
end
`
	p, err := mrb.ParseString(rubyCode, cxt)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	proc, err := mrb.GenerateCode(p)
	if err != nil {
		t.Fatalf("GenerateCode error: %v", err)
	}

	decoder := NewDecoder(mrb)
	unit, err := decoder.Decode(proc.IRep())
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	disasm := unit.Disassemble()
	t.Logf("Method Disassembly:\n%s", disasm)

	if len(unit.Children) != 1 {
		t.Fatalf("Expected 1 child irep for method 'add', got %d", len(unit.Children))
	}

	child := unit.Children[0]
	childDisasm := child.Disassemble()
	t.Logf("Child Disassembly:\n%s", childDisasm)

	hasAdd := false
	for _, ins := range child.Instructions {
		if ins.Op == OpADD || ins.Op == OpADDI {
			hasAdd = true
		}
	}
	if !hasAdd {
		t.Errorf("Expected OpADD in child method instructions")
	}
}
