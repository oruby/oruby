package compiler

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/oruby/oruby"
)

// The VM tier. Compiled Go code keeps its locals in Go variables, so a method
// or file that needs its own frame (eval, binding, local_variables, using, a
// string instance_eval) cannot be compiled to Go. Such a file stays bytecode:
// the compiled program carries it in the same RITE form the interpreter runs
// and runs it in the mruby VM when the file is loaded, as a bytecode file.
// Everything else in the program is still compiled.

// vmSendNames are the methods that read or define the locals, scope or
// refinements of the frame that calls them.
var vmSendNames = map[string]bool{
	"eval":                    true,
	"binding":                 true,
	"local_variables":         true,
	"local_variable_get":      true,
	"local_variable_set":      true,
	"local_variable_defined?": true,
	"using":                   true,
}

// vmEvalNames evaluate a string in the scope of the caller when given one
// (a block, which a compiled call passes as a block, is compiled as usual).
var vmEvalNames = map[string]bool{
	"instance_eval": true,
	"class_eval":    true,
	"module_eval":   true,
}

func isSendOp(op Opcode) bool {
	info, ok := opInfos[op]
	return ok && (strings.HasPrefix(info.Name, "SEND") || strings.HasPrefix(info.Name, "SSEND"))
}

// unitNeedsVM reports whether u, or a unit nested in it, needs the VM tier,
// and what in it does.
func unitNeedsVM(u *IRepUnit) (bool, string) {
	var all []*IRepUnit
	collectUnits(u, &all)
	for _, unit := range all {
		for _, ins := range unit.Instructions {
			switch {
			case isSendOp(ins.Op):
				name := ""
				if int(ins.B) < len(unit.Syms) {
					name = unit.Syms[ins.B]
				}
				if vmSendNames[name] {
					return true, name
				}
				// C is the argument count of a send; a string argument is one
				if vmEvalNames[name] && ins.C > 0 {
					return true, name + " with a string"
				}
			case ins.Op == OpGETCONST:
				if int(ins.B) < len(unit.Syms) && unit.Syms[ins.B] == "TOPLEVEL_BINDING" {
					return true, "TOPLEVEL_BINDING"
				}
			}
		}
	}
	return false, ""
}

// markVM puts mod in the VM tier when it needs it, keeping its bytecode.
// payload is the file's bytecode when it came as such, nil when irep must be
// dumped.
func (r *DependencyResolver) markVM(mod *ModuleUnit, irep oruby.MrbIrep, payload []byte) error {
	need, why := unitNeedsVM(mod.Root)
	if !need {
		return nil
	}
	if payload == nil {
		var buf bytes.Buffer
		if _, err := r.mrb.DumpIrep(irep, oruby.DumpDebugInfo, &buf); err != nil {
			return fmt.Errorf("dumping bytecode of %s: %w", mod.RelPath, err)
		}
		payload = buf.Bytes()
	}
	mod.VM = true
	mod.VMReason = why
	mod.Bytecode = payload
	return nil
}

// emitVMStub writes the entry function of a VM tier module: it has the
// signature of a compiled module's and runs the module's bytecode.
func (g *CodeGen) emitVMStub(b *bytes.Buffer, mod *ModuleUnit) {
	fn := g.info[mod.Root].fn
	file := mod.FilePath
	if file == "" {
		file = mod.RelPath
	}
	fmt.Fprintf(b, "// %s runs %s as bytecode: it uses %s, which needs its own frame\n", fn, mod.RelPath, mod.VMReason)
	fmt.Fprintf(b, "func %s(mrb *oruby.MrbState, self oruby.Value, args []oruby.Value, blk oruby.Value, cx *oruby.AotCtx, up *oruby.AotEnv) (oruby.Value, error) {\n", fn)
	// base64: the text of a grouped package is searched for its sym_N names, which bytecode could hold
	fmt.Fprintf(b, "\treturn mrb.AotRunVM(%q, %q)\n}\n\n", base64.StdEncoding.EncodeToString(mod.Bytecode), file)
}

// compiledModules are the modules compiled to Go: the whole-program analysis
// of top-level definitions is of these alone.
func compiledModules(modules []*ModuleUnit) []*ModuleUnit {
	out := make([]*ModuleUnit, 0, len(modules))
	for _, m := range modules {
		if !m.VM {
			out = append(out, m)
		}
	}
	return out
}
