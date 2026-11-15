package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

// enum mrb_debug_line_type
const (
	MrbDebugLineAry = iota
	MrbDebugLineFlatMap
	MrbDebugLinePackedMap
)

// MrbIrepDebugInfoLine line
type MrbIrepDebugInfoLine struct {
	p *C.struct_mrb_irep_debug_info_line
}

// MrbIrepDebugInfoFile file
type MrbIrepDebugInfoFile struct {
	p *C.struct_mrb_irep_debug_info_file
}

// MrbIrepDebugInfo debug info
type MrbIrepDebugInfo struct {
	p *C.struct_mrb_irep_debug_info
}

// DebugState represents a snapshot of execution when Kernel#debug is called
// or when the line-stepping debug hook triggers (in both Direct and Compiled modes).
type DebugState struct {
	Reason string           // "debug" (from Kernel#debug) or "step" (from line hook)
	File   string           // Source filename
	Line   int              // 1-based source line number
	Self   Value            // Current 'self' receiver
	Args   []Value          // Arguments passed to Kernel#debug(...)
	Regs   []Value          // Current frame registers (r[0] is self, r[1..nlocals-1] are locals)
	Locals map[string]Value // Local variable name -> current value
	lvMap  map[string]int
	setReg func(idx int, v Value)
}

// SetLocal updates a local variable's value in the paused frame (if present)
// and in s.Locals. Returns true if the local variable existed in the frame.
func (s *DebugState) SetLocal(name string, val MrbValue) bool {
	v := val.Value()
	if s.Locals != nil {
		s.Locals[name] = v
	}
	if s.lvMap != nil {
		if regIdx, ok := s.lvMap[name]; ok {
			if regIdx >= 0 && regIdx < len(s.Regs) {
				s.Regs[regIdx] = v
			}
			if s.setReg != nil {
				s.setReg(regIdx, v)
			}
			return true
		}
	}
	return false
}

// DebugHookFunc is the callback signature invoked on Kernel#debug or line-stepping.
type DebugHookFunc func(mrb *MrbState, state *DebugState)

// SetDebugHook registers a Go callback for Kernel#debug breakpoints and line-stepping.
func (mrb *MrbState) SetDebugHook(hook DebugHookFunc) {
	mrb.debugHook = hook
	mrb.lastDebugIrep = 0
	mrb.lastDebugLine = -1
	if hook != nil && mrb.lineDebug {
		C.set_mrb_injector(mrb.p)
	}
}

// DebugHook returns the currently registered DebugHookFunc.
func (mrb *MrbState) DebugHook() DebugHookFunc {
	return mrb.debugHook
}

// SetLineDebug enables or disables line-by-line stepping via the registered DebugHook.
func (mrb *MrbState) SetLineDebug(enabled bool) {
	mrb.lineDebug = enabled
	mrb.lastDebugIrep = 0
	mrb.lastDebugLine = -1
	if enabled {
		C.set_mrb_injector(mrb.p)
	}
}

// LineDebug returns true if line-by-line debugging is enabled.
func (mrb *MrbState) LineDebug() bool {
	return mrb.lineDebug
}

func extractIrepDebugFrame(mrb *MrbState, cirep *C.struct_mrb_irep, cregs *C.mrb_value) ([]Value, map[string]Value, map[string]int, func(int, Value)) {
	if cirep == nil || cregs == nil {
		return nil, make(map[string]Value), nil, nil
	}
	nregs := int(cirep.nregs)
	nlocals := int(cirep.nlocals)
	cslice := (*[1 << 20]C.mrb_value)(unsafe.Pointer(cregs))[:nregs:nregs]
	regs := make([]Value, nregs)
	for i := 0; i < nregs; i++ {
		regs[i] = Value{cslice[i]}
	}
	locals := make(map[string]Value)
	lvMap := make(map[string]int)
	if cirep.lv != nil && nlocals > 1 {
		lvSlice := (*[1 << 20]C.mrb_sym)(unsafe.Pointer(cirep.lv))[: nlocals-1 : nlocals-1]
		for i := 0; i < nlocals-1; i++ {
			sym := MrbSym(lvSlice[i])
			if sym != 0 {
				name := mrb.SymName(sym)
				if name != "" && i+1 < nregs {
					locals[name] = regs[i+1]
					lvMap[name] = i + 1
				}
			}
		}
	}
	setReg := func(idx int, v Value) {
		if idx >= 0 && idx < nregs {
			cslice[idx] = v.v
		}
	}
	return regs, locals, lvMap, setReg
}

func debugReturnValue(mrb *MrbState, args []Value) Value {
	switch len(args) {
	case 0:
		return nilValue
	case 1:
		return args[0]
	default:
		ary := mrb.AryNewCapa(len(args))
		for _, a := range args {
			mrb.AryPush(ary, a)
		}
		return ary.Value()
	}
}

func kernelDebug(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs().Slice()
	if mrb.debugHook != nil {
		info := C._mrb_get_caller_debug_info(mrb.p)
		file := ""
		if info.file != nil {
			file = C.GoString(info.file)
		}
		regs, locals, lvMap, setReg := extractIrepDebugFrame(mrb, info.irep, info.regs)
		callerSelf := self
		if len(regs) > 0 {
			callerSelf = regs[0]
		}
		state := DebugState{
			Reason: "debug",
			File:   file,
			Line:   int(info.line),
			Self:   callerSelf,
			Args:   args,
			Regs:   regs,
			Locals: locals,
			lvMap:  lvMap,
			setReg: setReg,
		}
		mrb.debugHook(mrb, &state)
	}
	return debugReturnValue(mrb, args)
}

// TriggerDebug invokes the registered DebugHookFunc from AOT-compiled Ruby code
// and returns the debug pass-through value (nil, single arg, or array of args).
func (mrb *MrbState) TriggerDebug(reason string, file string, line int, self Value, args []Value, regs []Value, localNames []string) Value {
	if mrb.debugHook != nil {
		locals := make(map[string]Value, len(localNames))
		lvMap := make(map[string]int, len(localNames))
		for i, name := range localNames {
			if name != "" && i+1 < len(regs) {
				locals[name] = regs[i+1]
				lvMap[name] = i + 1
			}
		}
		state := DebugState{
			Reason: reason,
			File:   file,
			Line:   line,
			Self:   self,
			Args:   args,
			Regs:   regs,
			Locals: locals,
			lvMap:  lvMap,
		}
		mrb.debugHook(mrb, &state)
	}
	return debugReturnValue(mrb, args)
}

//export go_has_line_debug
func go_has_line_debug(idx C.mrb_int) C.int {
	mrb := states[int(idx)]
	if mrb != nil && mrb.lineDebug && mrb.debugHook != nil {
		return 1
	}
	return 0
}

//export go_debug_line_hook
func go_debug_line_hook(idx C.mrb_int, cirep *C.struct_mrb_irep, pcOff C.uint32_t, cregs *C.mrb_value, line C.int32_t, cfile *C.char) {
	mrb := states[int(idx)]
	if mrb == nil || !mrb.lineDebug || mrb.debugHook == nil {
		return
	}
	irepPtr := uintptr(unsafe.Pointer(cirep))
	lineInt := int(line)
	if irepPtr == mrb.lastDebugIrep && lineInt == mrb.lastDebugLine {
		return
	}
	mrb.lastDebugIrep = irepPtr
	mrb.lastDebugLine = lineInt

	file := ""
	if cfile != nil {
		file = C.GoString(cfile)
	}
	regs, locals, lvMap, setReg := extractIrepDebugFrame(mrb, cirep, cregs)
	var self Value
	if len(regs) > 0 {
		self = regs[0]
	}
	state := DebugState{
		Reason: "step",
		File:   file,
		Line:   lineInt,
		Self:   self,
		Regs:   regs,
		Locals: locals,
		lvMap:  lvMap,
		setReg: setReg,
	}
	mrb.debugHook(mrb, &state)
}

// DebugGetFilename get line from irep's debug info and program counter
// @return returns NULL if not found
func (mrb *MrbState) DebugGetFilename(irep MrbIrep, pc uint32) string {
	return C.GoString(C.mrb_debug_get_filename(mrb.p, irep.p, C.uint32_t(pc)))
}

// DebugGetLine get line from irep's debug info and program counter
// @return returns -1 if not found
func (mrb *MrbState) DebugGetLine(irep MrbIrep, pc uint32) int32 {
	return int32(C.mrb_debug_get_line(mrb.p, irep.p, C.uint32_t(pc)))
}

// DebugGetPosition get line and filename from irep's debug info and program counter
// @return ok is false if not found
func (mrb *MrbState) DebugGetPosition(irep MrbIrep, pc uint32) (line int32, filename string, ok bool) {
	var cline C.int32_t
	var cfile *C.char
	if C.mrb_debug_get_position(mrb.p, irep.p, C.uint32_t(pc), &cline, &cfile) == false {
		return -1, "", false
	}
	if cfile != nil {
		filename = C.GoString(cfile)
	}
	return int32(cline), filename, true
}

// DebugInfoFree free debug info
func (mrb *MrbState) DebugInfoFree(d MrbIrepDebugInfo) {
	C.mrb_debug_info_free(mrb.p, (*C.mrb_irep_debug_info)(d.p))
}
