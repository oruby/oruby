package oruby

// #include "go-mrb.h"
import "C"

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
