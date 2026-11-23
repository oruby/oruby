package oruby

// #include "go-mrb.h"
import "C"
import (
	"bytes"
	"os"
	"runtime"
	"unsafe"
)

// enum irep_pool_type
type IrepPoolType uint32

const (
	IrepTtStr    IrepPoolType = 0 /* string (need free) */
	IrepTtSstr   IrepPoolType = 2 /* string (static) */
	IrepTtInt32  IrepPoolType = 1 /* 32bit integer */
	IrepTtInt64  IrepPoolType = 3 /* 64bit integer */
	IrepTtBigint IrepPoolType = 7 /* big integer (not yet supported) */
	IrepTtFloat  IrepPoolType = 5 /* float (double/float) */
)

const (
	IrepTtNflag = 1 /* number (non string) flag */
	IrepTtSflag = 2 /* static string flag */
)

type MrbIrepPool struct {
	v *C.mrb_irep_pool
}

func (pv MrbIrepPool) Type() IrepPoolType {
	if (pv.v.tt & IrepTtNflag) == 0 {
		return IrepPoolType(uint32(pv.v.tt) & 3)
	}
	return IrepPoolType(uint32(pv.v.tt) & 7)
}

func (pv MrbIrepPool) IsString() bool {
	t := pv.Type()
	return (t == IrepTtStr) || (t == IrepTtSstr)
}

func (pv MrbIrepPool) IsStr() bool {
	return IrepPoolType(uint32(pv.v.tt)&3) == IrepTtStr
}

func (pv MrbIrepPool) IsSStr() bool {
	return pv.Type() == IrepTtSstr
}

func (pv MrbIrepPool) Migrate() {
	C._mrb_pool_value_migrate(pv.v)
}

func (pv MrbIrepPool) Str() string {
	if pv.v == nil {
		return ""
	}
	length := C._mrb_pool_len(pv.v)
	ptr := C._mrb_pool_str(pv.v)
	if ptr == nil {
		return ""
	}
	return C.GoStringN(ptr, C.int(length))
}

func (pv MrbIrepPool) Int32() int32 {
	if pv.v == nil {
		return 0
	}
	return int32(C._mrb_pool_i32(pv.v))
}

func (pv MrbIrepPool) Int64() int64 {
	if pv.v == nil {
		return 0
	}
	return int64(C._mrb_pool_i64(pv.v))
}

func (pv MrbIrepPool) Float() float64 {
	if pv.v == nil {
		return 0
	}
	return float64(C._mrb_pool_float(pv.v))
}

func (pv MrbIrepPool) Value() interface{} {
	if pv.v == nil {
		return nil
	}
	switch pv.Type() {
	case IrepTtStr, IrepTtSstr:
		return pv.Str()
	case IrepTtInt32:
		return pv.Int32()
	case IrepTtInt64:
		return pv.Int64()
	case IrepTtFloat:
		return pv.Float()
	default:
		return nil
	}
}

// mrb_catch_type
const (
	MrbCatchRescue = 0
	MrbCatchEnsure = 1
)

type MrbIrepCatchHandler struct {
	p *C.struct_mrb_irep_catch_handler
}

const (
	MrbIseqNoFree = 1
	MrbIrepNoFree = 2
	MrbIrepStatic = MrbIseqNoFree | MrbIrepNoFree
)

// MrbIrep irep struct
type MrbIrep struct {
	p   *C.mrb_irep
	mrb *MrbState
}

// AddIrep irep api
func (mrb *MrbState) AddIrep() MrbIrep {
	return MrbIrep{C.mrb_add_irep(mrb.p), mrb}
}

// LoadIrep irep from buffer bytes array
//
// load mruby bytecode functions
// Please note! Currently due to interactions with the GC calling these functions will
// leak one RProc object per function call.
// To prevent this save the current memory arena before calling and restore the arena
// right after, like so:
//
//	ai := mrb.GCArenaSave()
//	status := mrb.LoadIrep(buffer)
//	mrb.GCArenaRestore(ai)
//
// @param buffer []byte - irep code, expected as a literal
func (mrb *MrbState) LoadIrep(buffer []byte) (Value, error) {
	var result C.mrb_value
	var status C.int
	bufLen := len(buffer)
	if bufLen == 0 {
		status = C._mrb_load_irep_empty(mrb.p, &result)
	} else {
		if rite, ok := ExtractRiteBinary(buffer); ok {
			buffer = rite
			bufLen = len(buffer)
		}
		status = C._mrb_load_irep_buf(mrb.p, unsafe.Pointer(&buffer[0]), C.size_t(bufLen), &result)
		runtime.KeepAlive(buffer)
	}
	v := Value{result}
	return mrb.protectedResult(v, status)
}

// LoadIrepBuf irep load from buffer, same as LoadIrep()
func (mrb *MrbState) LoadIrepBuf(buffer []byte) (Value, error) {
	return mrb.LoadIrep(buffer)
}

// LoadIrepCxt irep api
func (mrb *MrbState) LoadIrepCxt(buffer []byte, context *MrbCcontext) (Value, error) {
	var result C.mrb_value
	var status C.int
	bufLen := len(buffer)

	if bufLen == 0 {
		status = C._mrb_load_irep_empty_cxt(mrb.p, context.p, &result)
	} else {
		if rite, ok := ExtractRiteBinary(buffer); ok {
			buffer = rite
			bufLen = len(buffer)
		}
		status = C._mrb_load_irep_buf_cxt(mrb.p, unsafe.Pointer(&buffer[0]), C.size_t(bufLen), context.p, &result)
		runtime.KeepAlive(buffer)
	}

	v := Value{result}
	return mrb.protectedResult(v, status)
}

// LoadIrepBuf for context
func (c *MrbCcontext) LoadIrepBuf(buffer []byte) (Value, error) {
	return c.mrb.LoadIrepCxt(buffer, c)
}

// LoadIrepFile for context
func (c *MrbCcontext) LoadIrepFile(filename string) (Value, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return c.mrb.NilValue(), err
	}

	return c.mrb.LoadIrepCxt(data, c)
	// C.mrb_load_irep_file_cxt() is never called
}

// LoadDetectFile loads file, detecting if data is precompiled or if it contains script that needs to be compiled
// In order to be recognized as a `.mrb` file, the following three points must be satisfied:
// - File starts with "RITE"
// - At least `sizeof(struct rite_binary_header)` bytes can be read
// - `NUL` is included in the first 64 bytes of the file
func (c *MrbCcontext) LoadDetectFile(filename string) (Value, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return c.mrb.NilValue(), err
	}

	return c.mrb.LoadDetectBufCxt(data, c)
	// C.mrb_load_detect_file_cxt() is never called
}

func (c *MrbCcontext) LoadDetectBuf(data []byte) (Value, error) {
	return c.mrb.LoadDetectBufCxt(data, c)
}

// ExtractRiteBinary checks whether data is an mruby RiteVM binary (.mrb),
// optionally preceded by a "#!..." shebang line, and returns the RITE payload.
func ExtractRiteBinary(data []byte) ([]byte, bool) {
	if bytes.HasPrefix(data, []byte("#!")) {
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			data = data[nl+1:]
		}
	}
	if len(data) < 32 || !bytes.HasPrefix(data, []byte(RiteBinaryIdent)) {
		return nil, false
	}
	checkLen := len(data)
	if checkLen > 64 {
		checkLen = 64
	}
	if bytes.IndexByte(data[:checkLen], 0) < 0 {
		return nil, false
	}
	return data, true
}

// IsRiteBinary reports whether data contains a RiteVM binary (.mrb) payload.
func IsRiteBinary(data []byte) bool {
	_, ok := ExtractRiteBinary(data)
	return ok
}

// LoadDetectBufCxt load buffer with context, detecting if data is precompiled
// or if it contains script that needs to be compiled
// In order to be recognized as a `.mrb` file, the following three points must be satisfied:
// - File starts with "RITE" (optionally after a #! shebang line)
// - At least `sizeof(struct rite_binary_header)` bytes can be read
// - `NUL` is included in the first 64 bytes of the RITE payload
func (mrb *MrbState) LoadDetectBufCxt(data []byte, c *MrbCcontext) (Value, error) {
	if rite, ok := ExtractRiteBinary(data); ok {
		return mrb.LoadIrepCxt(rite, c)
	}

	// Parse as script, then load and execute
	p, err := mrb.ParseString(string(data), c)
	if err != nil {
		return nilValue, err
	}
	return mrb.LoadExecE(p, c)
}

// LoadIrepBufCxt irep load form buffer with context, same as LoadIrepCxt
func (mrb *MrbState) LoadIrepBufCxt(buffer []byte, context *MrbCcontext) (Value, error) {
	return mrb.LoadIrepCxt(buffer, context)
}

// IrepIncref increase reference to irep
func (mrb *MrbState) IrepIncref(irep MrbIrep) { C.mrb_irep_incref(mrb.p, irep.p) }

// IrepDecref decrease reference to irep
func (mrb *MrbState) IrepDecref(irep MrbIrep) { C.mrb_irep_decref(mrb.p, irep.p) }

// IrepCutref cut reference form irep
func (mrb *MrbState) IrepCutref(irep MrbIrep) { C.mrb_irep_cutref(mrb.p, irep.p) }

// IsNil returns true if irep is empty
func (irep MrbIrep) IsNil() bool { return irep.p == nil }

// Free irep
func (irep MrbIrep) Free() { C.mrb_irep_free(irep.mrb.p, irep.p) }

// Incref increase reference to irep
func (irep MrbIrep) Incref() { C.mrb_irep_incref(irep.mrb.p, irep.p) }

// Decref decrease reference to irep
func (irep MrbIrep) Decref() { C.mrb_irep_decref(irep.mrb.p, irep.p) }

// Cutref cut reference form irep
func (irep MrbIrep) Cutref() { C.mrb_irep_cutref(irep.mrb.p, irep.p) }

// NLocals returns number of local variables
func (irep MrbIrep) NLocals() int {
	return int(irep.p.nlocals)
}

// Lv returns local variable symbol at index (0 <= index < NLocals()-1)
func (irep MrbIrep) Lv(index int) MrbSym {
	if irep.p == nil || irep.p.lv == nil || index < 0 || index >= irep.NLocals()-1 {
		return MrbSym(0)
	}
	l := irep.NLocals() - 1
	slice := (*[1 << 28]C.mrb_sym)(unsafe.Pointer(irep.p.lv))[:l:l]
	return MrbSym(slice[index])
}

// NRegs returns number of register variables
func (irep MrbIrep) NRegs() int {
	return int(irep.p.nregs)
}

// CLen returns number of catch handlers
func (irep MrbIrep) CLen() int {
	return int(irep.p.clen)
}

// CatchHandler returns catch handler at index (0 <= index < CLen()).
// Type is 0 for rescue and 1 for ensure handlers. Handler covers
// bytecode range [begin, end) and jumps to target.
func (irep MrbIrep) CatchHandler(index int) (typ int, begin, end, target uint32) {
	if index < 0 || index >= irep.CLen() {
		return -1, 0, 0, 0
	}
	var t C.int
	var b, e, tg C.uint32_t
	C._mrb_irep_catch_handler(irep.p, C.int(index), &t, &b, &e, &tg)
	return int(t), uint32(b), uint32(e), uint32(tg)
}

// Flags returns irep flags
func (irep MrbIrep) Flags() int {
	return int(irep.p.flags)
}

// FlagSet sets irep flag
func (irep MrbIrep) FlagSet(flag int) {
	irep.p.flags &= C.uint8_t(flag)
}

// FlagUnset sets irep flag
func (irep MrbIrep) FlagUnset(flag int) {
	irep.p.flags &= ^C.uint8_t(flag)
}

// PLen returns number of pool values
func (irep MrbIrep) PLen() int {
	return int(irep.p.plen)
}

// Pool returns Value at index
func (irep MrbIrep) Pool(index int) MrbIrepPool {
	if index < 0 || index >= irep.PLen() {
		return MrbIrepPool{}
	}

	l := int(irep.p.plen)
	slice := (*[1 << 28]C.mrb_irep_pool)(unsafe.Pointer(irep.p.pool))[:l:l]

	return MrbIrepPool{&slice[index]}
}

// ILen returns number of ISeq MrbCode items
func (irep MrbIrep) ILen() int {
	return int(irep.p.ilen)
}

// ISeq returns MrbCode at index
func (irep MrbIrep) ISeq() []MrbCode {
	l := irep.p.ilen
	slice := (*[1 << 28]MrbCode)(unsafe.Pointer(irep.p.iseq))[:l:l]
	return slice
}

// ISeqItem returns MrbCode at index
func (irep MrbIrep) ISeqItem(index int) MrbCode {
	if index < 0 || index >= irep.ILen() {
		return MrbCode(0)
	}

	l := int(irep.p.ilen)
	slice := (*[1 << 28]C.mrb_code)(unsafe.Pointer(irep.p.iseq))[:l:l]

	return MrbCode(slice[index])
}

// RLen returns number of Reps MrbIrep items
func (irep MrbIrep) RLen() int {
	return int(irep.p.rlen)
}

// Reps returns Value at index
func (irep MrbIrep) Reps(index int) MrbIrep {
	if index < 0 || index >= irep.RLen() {
		return MrbIrep{nil, irep.mrb}
	}

	l := int(irep.p.rlen)
	slice := (*[1 << 28]*C.mrb_irep)(unsafe.Pointer(irep.p.reps))[:l:l]

	return MrbIrep{slice[index], irep.mrb}
}

// SLen returns number of Syms MrbSym items
func (irep MrbIrep) SLen() int {
	return int(irep.p.slen)
}

// Syms returns MrbSym at index
func (irep MrbIrep) Syms(index int) MrbSym {
	if index < 0 || index >= irep.SLen() {
		return MrbSym(0)
	}

	l := int(irep.p.slen)
	slice := (*[1 << 28]C.mrb_sym)(unsafe.Pointer(irep.p.syms))[:l:l]

	return MrbSym(slice[index])
}

// SetSyms sets IRep syms
func (irep MrbIrep) SetSyms(syms ...MrbSym) {
	buf := irep.mrb.Malloc(uint(C.sizeof_mrb_sym * len(syms)))

	size := C.size_t(C.sizeof_mrb_sym * len(syms))
	if size > 0 {
		C.memcpy(buf.p, unsafe.Pointer(&syms[0]), size)
	}

	irep.p.syms = (*C.mrb_sym)(buf.p)
	irep.p.slen = C.ushort(len(syms))
}

// SetISeq rsets irep code
func (irep MrbIrep) SetISeq(iseq []MrbCode) {
	if len(iseq) == 0 {
		irep.p.iseq = nil
		irep.p.ilen = 0
		return
	}

	size := C.size_t(C.sizeof_mrb_code * len(iseq))

	p := C.mrb_malloc(irep.mrb.p, size)
	C.memcpy(p, unsafe.Pointer(&iseq[0]), size)
	irep.p.iseq = (*C.mrb_code)(p)

	// Since iseq is copied and alocated, unmark NO_FREE flag
	irep.FlagUnset(MrbIseqNoFree)
}

// CopyISeq copies irep code from source irep
func (irep MrbIrep) CopyISeq(source MrbIrep) {
	if source.p.iseq == nil {
		irep.p.iseq = nil
		irep.p.ilen = 0
		return
	}

	size := C.size_t(C.sizeof_mrb_code * source.p.ilen)
	p := C.mrb_malloc(irep.mrb.p, size)
	C.memcpy(p, unsafe.Pointer(source.p.iseq), size)

	irep.p.iseq = (*C.mrb_code)(p)

	// Since iseq is copied and alocated, unmark NO_FREE flag
	irep.FlagUnset(MrbIseqNoFree)
}

type MrbInsnData struct {
	d *C.struct_mrb_insn_data
}
