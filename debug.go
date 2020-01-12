package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"unsafe"
)

//enum mrb_debug_line_type
const (
	MrbDebugLineAry = iota
	MrbDebugLineFlatMap
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
	return C.GoString(C.mrb_debug_get_filename(mrb.p, irep.p, C.ptrdiff_t(pc)))
}

// DebugGetLine get line from irep's debug info and program counter
// @return returns -1 if not found
func (mrb *MrbState) DebugGetLine(irep MrbIrep, pc uint32) uint32 {
	return uint32(C.mrb_debug_get_line(mrb.p, irep.p, C.ptrdiff_t(pc)))
}

// DebugInfoAlloc allocate debug info
func (mrb *MrbState) DebugInfoAlloc(irep MrbIrep) MrbIrepDebugInfo {
	return MrbIrepDebugInfo{(*C.struct_mrb_irep_debug_info)(C.mrb_debug_info_alloc(mrb.p, irep.p))}
}

// DebugInfoAppendFile append to file
func (mrb *MrbState) DebugInfoAppendFile(info MrbIrepDebugInfo, filename string, lines *uint16, startPos, endPos uint32) (MrbIrepDebugInfoFile, error) {
	cfilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cfilename))
	clines := C.uint16_t(*lines)
	p := C.mrb_debug_info_append_file(mrb.p, info.p, cfilename, &clines, C.uint32_t(startPos), C.uint32_t(endPos))

	if p == nil {
		return MrbIrepDebugInfoFile{nil}, errors.New("Debug_info_append_file error")
	}

	*lines = (uint16)(clines)
	return MrbIrepDebugInfoFile{(*C.struct_mrb_irep_debug_info_file)(p)}, nil
}

// DebugInfoFree free debug info
func (mrb *MrbState) DebugInfoFree(d MrbIrepDebugInfo) {
	C.mrb_debug_info_free(mrb.p, (*C.mrb_irep_debug_info)(d.p))
}
