// Package ffi implements a prototype of Ruby's ffi gem backed by libffi and
// dlopen/dlsym via cgo.
package ffi

/*
#cgo darwin LDFLAGS: -lffi
#cgo linux LDFLAGS: -lffi -ldl
#cgo windows LDFLAGS: -lffi
#include "ffi_c.h"
*/
import "C"

import (
	_ "embed"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed ffi.rb
var ffiRb []byte

func init() {
	oruby.LazyGem("ffi", initFFI)
	oruby.GemAlias("ffi_c", "ffi")
}

func u2p(a uintptr) unsafe.Pointer {
	return C.oruby_ffi_u2p(C.uintptr_t(a))
}

// Per-MrbState errno storage
var (
	errnoMu  sync.Mutex
	errnoMap = map[*oruby.MrbState]int{}
)

func setErrno(mrb *oruby.MrbState, e int) {
	errnoMu.Lock()
	errnoMap[mrb] = e
	errnoMu.Unlock()
}

func getErrno(mrb *oruby.MrbState) int {
	errnoMu.Lock()
	e := errnoMap[mrb]
	errnoMu.Unlock()
	return e
}

// ---------------------------------------------------------------------------
// Closure registry (integer handles -> Go callback descriptors)
// ---------------------------------------------------------------------------

type closureEntry struct {
	mrb       *oruby.MrbState
	proc      oruby.Value
	retType   *ffiTypeDesc
	argTypes  []*ffiTypeDesc
	closure   unsafe.Pointer
	codeloc   unsafe.Pointer
	cif       *C.ffi_cif
	cArgTypes **C.ffi_type
}

var (
	closureSeq uint64
	closureMu  sync.RWMutex
	closures   = map[uintptr]*closureEntry{}
)

//export orubyFFIClosureTrampoline
func orubyFFIClosureTrampoline(handle C.uintptr_t, ret unsafe.Pointer, args *unsafe.Pointer) {
	closureMu.RLock()
	entry := closures[uintptr(handle)]
	closureMu.RUnlock()
	if entry == nil {
		return
	}

	mrb := entry.mrb
	nargs := len(entry.argTypes)
	argPtrs := unsafe.Slice(args, nargs)
	rbArgs := make([]any, nargs)
	for i := 0; i < nargs; i++ {
		rbArgs[i] = cPtrToRuby(mrb, entry.argTypes[i], argPtrs[i])
	}

	res, err := mrb.Funcall(entry.proc, mrb.Intern("call"), rbArgs...)
	if err != nil {
		return
	}
	if entry.retType.kind != C.ORUBY_FFI_VOID && ret != nil {
		writeClosureReturn(mrb, entry.retType, res, ret)
	}
}

func writeClosureReturn(mrb *oruby.MrbState, t *ffiTypeDesc, v oruby.Value, ret unsafe.Pointer) {
	switch t.kind {
	case C.ORUBY_FFI_BOOL:
		if v.Bool() {
			*(*C.ffi_arg)(ret) = 1
		} else {
			*(*C.ffi_arg)(ret) = 0
		}
	case C.ORUBY_FFI_INT8:
		*(*C.ffi_sarg)(ret) = C.ffi_sarg(int8(v.Int64()))
	case C.ORUBY_FFI_UINT8:
		*(*C.ffi_arg)(ret) = C.ffi_arg(uint8(v.Int64()))
	case C.ORUBY_FFI_INT16:
		*(*C.ffi_sarg)(ret) = C.ffi_sarg(int16(v.Int64()))
	case C.ORUBY_FFI_UINT16:
		*(*C.ffi_arg)(ret) = C.ffi_arg(uint16(v.Int64()))
	case C.ORUBY_FFI_INT32:
		*(*C.ffi_sarg)(ret) = C.ffi_sarg(int32(v.Int64()))
	case C.ORUBY_FFI_UINT32:
		*(*C.ffi_arg)(ret) = C.ffi_arg(uint32(v.Int64()))
	case C.ORUBY_FFI_INT64:
		*(*int64)(ret) = v.Int64()
	case C.ORUBY_FFI_UINT64:
		*(*uint64)(ret) = rubyToUint64(mrb, v)
	case C.ORUBY_FFI_LONG:
		*(*C.long)(ret) = C.long(v.Int64())
	case C.ORUBY_FFI_ULONG:
		*(*C.ulong)(ret) = C.ulong(rubyToUint64(mrb, v))
	case C.ORUBY_FFI_FLOAT:
		*(*float32)(ret) = float32(rubyToFloat64(v))
	case C.ORUBY_FFI_DOUBLE:
		*(*float64)(ret) = rubyToFloat64(v)
	case C.ORUBY_FFI_POINTER:
		p, _ := extractPointer(mrb, v)
		*(*unsafe.Pointer)(ret) = p
	case C.ORUBY_FFI_STRUCT_VAL:
		if p, ok := extractStructPtr(mrb, v); ok && p != nil && t.size > 0 {
			C.memcpy(ret, p, C.size_t(t.size))
		}
	}
}

// ---------------------------------------------------------------------------
// Type descriptors
// ---------------------------------------------------------------------------

type ffiTypeDesc struct {
	kind        C.int
	size        int
	alignment   int
	ctype       *C.ffi_type
	ownedCType  bool
	structClass oruby.Value
	retType     *ffiTypeDesc
	argTypes    []*ffiTypeDesc
}

func makePrimitiveType(kind C.int) *ffiTypeDesc {
	ct := C.oruby_ffi_primitive_type(kind)
	if ct == nil {
		return nil
	}
	return &ffiTypeDesc{
		kind:      kind,
		size:      int(ct.size),
		alignment: int(ct.alignment),
		ctype:     ct,
	}
}

// ---------------------------------------------------------------------------
// Pointer / Memory wrapper
// ---------------------------------------------------------------------------

type ffiPtr struct {
	addr        unsafe.Pointer
	size        int
	typeSize    int
	owned       bool
	autorelease bool
}

func (p *ffiPtr) freeNow() {
	if p.owned && p.addr != nil {
		C.free(p.addr)
		p.addr = nil
		p.owned = false
	}
}

func (p *ffiPtr) checkAddr(off int) (unsafe.Pointer, error) {
	if p.addr == nil {
		return nil, fmt.Errorf("null pointer access")
	}
	if off < 0 {
		return nil, fmt.Errorf("negative offset %d", off)
	}
	if p.size >= 0 && off > p.size {
		return nil, fmt.Errorf("offset %d out of bounds (size %d)", off, p.size)
	}
	return unsafe.Add(p.addr, off), nil
}

func getPtr(mrb *oruby.MrbState, v oruby.Value) (*ffiPtr, bool) {
	return rb.Get[*ffiPtr](mrb, v)
}

func newPointerVal(mrb *oruby.MrbState, addr unsafe.Pointer, size int, typeSize int) oruby.Value {
	cls := rb.ClassPath(mrb, "FFI::Pointer")
	if typeSize <= 0 {
		typeSize = 1
	}
	p := &ffiPtr{addr: addr, size: size, typeSize: typeSize}
	return mrb.DataWrapInterface(cls, p).Value()
}

func extractPointer(mrb *oruby.MrbState, v oruby.Value) (unsafe.Pointer, error) {
	if v.IsNil() {
		return nil, nil
	}
	if p, ok := getPtr(mrb, v); ok {
		return p.addr, nil
	}
	if fn, ok := rb.Get[*ffiFunc](mrb, v); ok {
		return fn.addr, nil
	}
	if mrb.RespondTo(v, mrb.Intern("to_ptr")) {
		pv, err := mrb.Funcall(v, mrb.Intern("to_ptr"))
		if err != nil {
			return nil, err
		}
		if p, ok := getPtr(mrb, pv); ok {
			return p.addr, nil
		}
	}
	if v.IsInt() {
		return u2p(uintptr(v.Int64())), nil
	}
	return nil, fmt.Errorf("expected FFI::Pointer, got %s", mrb.ClassOf(v).Name())
}

func extractStructPtr(mrb *oruby.MrbState, v oruby.Value) (unsafe.Pointer, bool) {
	if v.IsNil() {
		return nil, false
	}
	if p, ok := getPtr(mrb, v); ok {
		return p.addr, true
	}
	if mrb.RespondTo(v, mrb.Intern("to_ptr")) {
		pv, err := mrb.Funcall(v, mrb.Intern("to_ptr"))
		if err == nil {
			if p, ok := getPtr(mrb, pv); ok {
				return p.addr, true
			}
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// DynamicLibrary wrapper
// ---------------------------------------------------------------------------

type ffiDynLib struct {
	handle unsafe.Pointer
	name   string
	self   bool
}

// ---------------------------------------------------------------------------
// Function wrapper
// ---------------------------------------------------------------------------

type ffiFunc struct {
	addr      unsafe.Pointer
	retType   *ffiTypeDesc
	argTypes  []*ffiTypeDesc
	cif       *C.ffi_cif
	cArgTypes **C.ffi_type
	closureID uintptr
}

// ---------------------------------------------------------------------------
// Value conversions
// ---------------------------------------------------------------------------

func rubyToUint64(mrb *oruby.MrbState, v oruby.Value) uint64 {
	if v.Type() == oruby.MrbTTBigInt {
		if z, err := mrb.BigInt(v); err == nil {
			return z.Uint64()
		}
	}
	return uint64(v.Int64())
}

func rubyToFloat64(v oruby.Value) float64 {
	if v.IsInt() {
		return float64(v.Int64())
	}
	return v.Float64()
}

func cPtrToRuby(mrb *oruby.MrbState, t *ffiTypeDesc, ptr unsafe.Pointer) oruby.Value {
	switch t.kind {
	case C.ORUBY_FFI_VOID:
		return mrb.NilValue()
	case C.ORUBY_FFI_BOOL:
		return mrb.BoolValue(*(*uint8)(ptr) != 0)
	case C.ORUBY_FFI_INT8:
		return mrb.FixnumValue(int(*(*int8)(ptr)))
	case C.ORUBY_FFI_UINT8:
		return mrb.FixnumValue(int(*(*uint8)(ptr)))
	case C.ORUBY_FFI_INT16:
		return mrb.FixnumValue(int(*(*int16)(ptr)))
	case C.ORUBY_FFI_UINT16:
		return mrb.FixnumValue(int(*(*uint16)(ptr)))
	case C.ORUBY_FFI_INT32:
		return mrb.FixnumValue(int(*(*int32)(ptr)))
	case C.ORUBY_FFI_UINT32:
		return mrb.Int64Value(int64(*(*uint32)(ptr)))
	case C.ORUBY_FFI_INT64:
		return mrb.Int64Value(*(*int64)(ptr))
	case C.ORUBY_FFI_UINT64:
		return mrb.Uint64Value(*(*uint64)(ptr))
	case C.ORUBY_FFI_LONG:
		return mrb.Int64Value(int64(*(*C.long)(ptr)))
	case C.ORUBY_FFI_ULONG:
		return mrb.Uint64Value(uint64(*(*C.ulong)(ptr)))
	case C.ORUBY_FFI_FLOAT:
		return mrb.FloatValue(float64(*(*float32)(ptr)))
	case C.ORUBY_FFI_DOUBLE:
		return mrb.FloatValue(*(*float64)(ptr))
	case C.ORUBY_FFI_POINTER:
		return newPointerVal(mrb, *(*unsafe.Pointer)(ptr), -1, 1)
	case C.ORUBY_FFI_STRING:
		cp := *(*unsafe.Pointer)(ptr)
		if cp == nil {
			return mrb.NilValue()
		}
		return mrb.StrNew(C.GoString((*C.char)(cp)))
	case C.ORUBY_FFI_STRPTR:
		cp := *(*unsafe.Pointer)(ptr)
		ary := mrb.AryNewCapa(2)
		if cp == nil {
			mrb.AryPush(ary, mrb.NilValue())
		} else {
			mrb.AryPush(ary, mrb.StrNew(C.GoString((*C.char)(cp))))
		}
		mrb.AryPush(ary, newPointerVal(mrb, cp, -1, 1))
		return ary.Value()
	case C.ORUBY_FFI_STRUCT_VAL:
		mem := C.calloc(1, C.size_t(maxInt(t.size, 1)))
		if t.size > 0 {
			C.memcpy(mem, ptr, C.size_t(t.size))
		}
		mpCls := rb.ClassPath(mrb, "FFI::MemoryPointer")
		mp := &ffiPtr{addr: mem, size: t.size, typeSize: 1, owned: true, autorelease: true}
		runtime.SetFinalizer(mp, func(p *ffiPtr) {
			if p.owned && p.autorelease && p.addr != nil {
				p.freeNow()
			}
		})
		mpVal := mrb.DataWrapInterface(mpCls, mp).Value()
		if !t.structClass.IsNil() {
			s, err := mrb.Funcall(t.structClass, mrb.Intern("new"), mpVal)
			if err == nil {
				return s
			}
		}
		return mpVal
	}
	return mrb.NilValue()
}

func cReturnToRuby(mrb *oruby.MrbState, t *ffiTypeDesc, rval unsafe.Pointer) oruby.Value {
	switch t.kind {
	case C.ORUBY_FFI_VOID:
		return mrb.NilValue()
	case C.ORUBY_FFI_BOOL:
		return mrb.BoolValue(*(*C.ffi_arg)(rval) != 0)
	case C.ORUBY_FFI_INT8:
		return mrb.FixnumValue(int(int8(*(*C.ffi_sarg)(rval))))
	case C.ORUBY_FFI_UINT8:
		return mrb.FixnumValue(int(uint8(*(*C.ffi_arg)(rval))))
	case C.ORUBY_FFI_INT16:
		return mrb.FixnumValue(int(int16(*(*C.ffi_sarg)(rval))))
	case C.ORUBY_FFI_UINT16:
		return mrb.FixnumValue(int(uint16(*(*C.ffi_arg)(rval))))
	case C.ORUBY_FFI_INT32:
		return mrb.FixnumValue(int(int32(*(*C.ffi_sarg)(rval))))
	case C.ORUBY_FFI_UINT32:
		return mrb.Int64Value(int64(uint32(*(*C.ffi_arg)(rval))))
	default:
		return cPtrToRuby(mrb, t, rval)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func marshalArg(mrb *oruby.MrbState, t *ffiTypeDesc, v oruby.Value, cleanups *[]func()) (unsafe.Pointer, error) {
	switch t.kind {
	case C.ORUBY_FFI_BOOL:
		slot := C.malloc(1)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		if v.Bool() {
			*(*uint8)(slot) = 1
		} else {
			*(*uint8)(slot) = 0
		}
		return slot, nil
	case C.ORUBY_FFI_INT8:
		slot := C.malloc(1)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*int8)(slot) = int8(v.Int64())
		return slot, nil
	case C.ORUBY_FFI_UINT8:
		slot := C.malloc(1)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*uint8)(slot) = uint8(v.Int64())
		return slot, nil
	case C.ORUBY_FFI_INT16:
		slot := C.malloc(2)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*int16)(slot) = int16(v.Int64())
		return slot, nil
	case C.ORUBY_FFI_UINT16:
		slot := C.malloc(2)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*uint16)(slot) = uint16(v.Int64())
		return slot, nil
	case C.ORUBY_FFI_INT32:
		slot := C.malloc(4)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*int32)(slot) = int32(v.Int64())
		return slot, nil
	case C.ORUBY_FFI_UINT32:
		slot := C.malloc(4)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*uint32)(slot) = uint32(v.Int64())
		return slot, nil
	case C.ORUBY_FFI_INT64:
		slot := C.malloc(8)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*int64)(slot) = v.Int64()
		return slot, nil
	case C.ORUBY_FFI_UINT64:
		slot := C.malloc(8)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*uint64)(slot) = rubyToUint64(mrb, v)
		return slot, nil
	case C.ORUBY_FFI_LONG:
		slot := C.malloc(C.size_t(unsafe.Sizeof(C.long(0))))
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*C.long)(slot) = C.long(v.Int64())
		return slot, nil
	case C.ORUBY_FFI_ULONG:
		slot := C.malloc(C.size_t(unsafe.Sizeof(C.ulong(0))))
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*C.ulong)(slot) = C.ulong(rubyToUint64(mrb, v))
		return slot, nil
	case C.ORUBY_FFI_FLOAT:
		slot := C.malloc(4)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*float32)(slot) = float32(rubyToFloat64(v))
		return slot, nil
	case C.ORUBY_FFI_DOUBLE:
		slot := C.malloc(8)
		*cleanups = append(*cleanups, func() { C.free(slot) })
		*(*float64)(slot) = rubyToFloat64(v)
		return slot, nil
	case C.ORUBY_FFI_STRING, C.ORUBY_FFI_STRPTR:
		slot := C.malloc(C.size_t(unsafe.Sizeof(unsafe.Pointer(nil))))
		*cleanups = append(*cleanups, func() { C.free(slot) })
		if v.IsNil() {
			*(*unsafe.Pointer)(slot) = nil
			return slot, nil
		}
		if v.IsString() {
			cs := C.CString(v.String())
			*cleanups = append(*cleanups, func() { C.free(unsafe.Pointer(cs)) })
			*(*unsafe.Pointer)(slot) = unsafe.Pointer(cs)
			return slot, nil
		}
		p, err := extractPointer(mrb, v)
		if err != nil {
			return nil, err
		}
		*(*unsafe.Pointer)(slot) = p
		return slot, nil
	case C.ORUBY_FFI_POINTER:
		slot := C.malloc(C.size_t(unsafe.Sizeof(unsafe.Pointer(nil))))
		*cleanups = append(*cleanups, func() { C.free(slot) })
		if v.IsString() {
			bs := v.Bytes()
			buf := C.malloc(C.size_t(len(bs) + 1))
			if len(bs) > 0 {
				C.memcpy(buf, unsafe.Pointer(&bs[0]), C.size_t(len(bs)))
			}
			*(*byte)(unsafe.Add(buf, len(bs))) = 0
			*cleanups = append(*cleanups, func() { C.free(buf) })
			*(*unsafe.Pointer)(slot) = buf
			return slot, nil
		}
		p, err := extractPointer(mrb, v)
		if err != nil {
			return nil, err
		}
		*(*unsafe.Pointer)(slot) = p
		return slot, nil
	case C.ORUBY_FFI_CALLBACK:
		slot := C.malloc(C.size_t(unsafe.Sizeof(unsafe.Pointer(nil))))
		*cleanups = append(*cleanups, func() { C.free(slot) })
		if v.IsNil() {
			*(*unsafe.Pointer)(slot) = nil
			return slot, nil
		}
		if fn, ok := rb.Get[*ffiFunc](mrb, v); ok {
			*(*unsafe.Pointer)(slot) = fn.addr
			return slot, nil
		}
		if p, ok := getPtr(mrb, v); ok {
			*(*unsafe.Pointer)(slot) = p.addr
			return slot, nil
		}
		entry, id, err := allocClosure(mrb, C.ORUBY_FFI_CONV_DEFAULT, t.retType, t.argTypes, v)
		if err != nil {
			return nil, err
		}
		*cleanups = append(*cleanups, func() { freeClosure(id) })
		*(*unsafe.Pointer)(slot) = entry.codeloc
		return slot, nil
	case C.ORUBY_FFI_STRUCT_VAL:
		sz := maxInt(t.size, 1)
		slot := C.calloc(1, C.size_t(sz))
		*cleanups = append(*cleanups, func() { C.free(slot) })
		if p, ok := extractStructPtr(mrb, v); ok && p != nil && t.size > 0 {
			C.memcpy(slot, p, C.size_t(t.size))
		}
		return slot, nil
	}
	return nil, fmt.Errorf("unsupported FFI argument kind %d", t.kind)
}

func prepCif(conv C.int, ret *ffiTypeDesc, args []*ffiTypeDesc) (*C.ffi_cif, **C.ffi_type, error) {
	cif := (*C.ffi_cif)(C.calloc(1, C.size_t(unsafe.Sizeof(C.ffi_cif{}))))
	var cArgs **C.ffi_type
	n := len(args)
	if n > 0 {
		cArgs = (**C.ffi_type)(C.calloc(C.size_t(n), C.size_t(unsafe.Sizeof((*C.ffi_type)(nil)))))
		argSlice := unsafe.Slice(cArgs, n)
		for i, a := range args {
			argSlice[i] = a.ctype
		}
	}
	st := C.ffi_prep_cif(cif, C.oruby_ffi_abi(conv), C.uint(n), ret.ctype, cArgs)
	if st != C.FFI_OK {
		C.free(unsafe.Pointer(cif))
		if cArgs != nil {
			C.free(unsafe.Pointer(cArgs))
		}
		return nil, nil, fmt.Errorf("ffi_prep_cif failed with status %d", int(st))
	}
	return cif, cArgs, nil
}

func allocClosure(mrb *oruby.MrbState, conv C.int, ret *ffiTypeDesc, args []*ffiTypeDesc, proc oruby.Value) (*closureEntry, uintptr, error) {
	cif, cArgs, err := prepCif(conv, ret, args)
	if err != nil {
		return nil, 0, err
	}
	id := uintptr(atomic.AddUint64(&closureSeq, 1))
	var codeloc unsafe.Pointer
	cl := C.oruby_ffi_alloc_closure(cif, C.uintptr_t(id), &codeloc)
	if cl == nil {
		C.free(unsafe.Pointer(cif))
		if cArgs != nil {
			C.free(unsafe.Pointer(cArgs))
		}
		return nil, 0, fmt.Errorf("ffi_closure_alloc failed")
	}
	entry := &closureEntry{
		mrb:       mrb,
		proc:      proc,
		retType:   ret,
		argTypes:  args,
		closure:   cl,
		codeloc:   codeloc,
		cif:       cif,
		cArgTypes: cArgs,
	}
	closureMu.Lock()
	closures[id] = entry
	closureMu.Unlock()
	return entry, id, nil
}

func freeClosure(id uintptr) {
	closureMu.Lock()
	entry := closures[id]
	delete(closures, id)
	closureMu.Unlock()
	if entry != nil {
		C.oruby_ffi_free_closure(entry.closure)
		C.free(unsafe.Pointer(entry.cif))
		if entry.cArgTypes != nil {
			C.free(unsafe.Pointer(entry.cArgTypes))
		}
	}
}

// ---------------------------------------------------------------------------
// Init FFI module & classes
// ---------------------------------------------------------------------------

func initFFI(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("FFI")

	lastErr := mrb.DefineModuleUnder(mod, "LastError")
	lastErr.DefineClassMethod("error", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getErrno(mrb))
	}, mrb.ArgsNone())
	lastErr.DefineClassMethod("error=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		e := mrb.GetArgsFirst().Int()
		setErrno(mrb, e)
		return mrb.FixnumValue(e)
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("errno", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getErrno(mrb))
	}, mrb.ArgsNone())

	mod.DefineClassMethod("errno=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		e := mrb.GetArgsFirst().Int()
		setErrno(mrb, e)
		return mrb.FixnumValue(e)
	}, mrb.ArgsReq(1))

	definePlatform(mrb, mod)
	defineTypes(mrb, mod)
	definePointer(mrb, mod)
	defineDynLib(mrb, mod)
	defineFunction(mrb, mod)
	defineVariadic(mrb, mod)

	rb.LoadRuby(mrb, "ffi.rb", ffiRb)
	return nil
}

func definePlatform(mrb *oruby.MrbState, mod oruby.RClass) {
	plat := mrb.DefineModuleUnder(mod, "Platform")
	ptrSize := int(C.oruby_ffi_sizeof_pointer())
	longSize := int(C.oruby_ffi_sizeof_long())

	mrb.ConstSet(plat, mrb.Intern("ADDRESS_SIZE"), mrb.FixnumValue(ptrSize*8))
	mrb.ConstSet(plat, mrb.Intern("LONG_SIZE"), mrb.FixnumValue(longSize*8))
	mrb.ConstSet(plat, mrb.Intern("BYTE_ORDER"), mrb.FixnumValue(1234))
	mrb.ConstSet(plat, mrb.Intern("LITTLE_ENDIAN"), mrb.FixnumValue(1234))
	mrb.ConstSet(plat, mrb.Intern("BIG_ENDIAN"), mrb.FixnumValue(4321))

	arch := runtime.GOARCH
	switch arch {
	case "arm64":
		arch = "aarch64"
	case "amd64":
		arch = "x86_64"
	case "386":
		arch = "i386"
	}
	mrb.ConstSet(plat, mrb.Intern("ARCH"), mrb.StrNew(arch))
	mrb.ConstSet(plat, mrb.Intern("CPU"), mrb.StrNew(arch))

	osName := runtime.GOOS
	switch runtime.GOOS {
	case "darwin":
		osName = "darwin"
		mrb.ConstSet(plat, mrb.Intern("OS"), mrb.StrNew(osName))
		mrb.ConstSet(plat, mrb.Intern("LIBPREFIX"), mrb.StrNew("lib"))
		mrb.ConstSet(plat, mrb.Intern("LIBSUFFIX"), mrb.StrNew("dylib"))
		mrb.ConstSet(plat, mrb.Intern("LIBC"), mrb.StrNew("libSystem.B.dylib"))
		mrb.ConstSet(plat, mrb.Intern("IS_MAC"), mrb.TrueValue())
		mrb.ConstSet(plat, mrb.Intern("IS_LINUX"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_WINDOWS"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_BSD"), mrb.TrueValue())
		mrb.ConstSet(plat, mrb.Intern("IS_GNU"), mrb.FalseValue())
	case "linux":
		osName = "linux"
		mrb.ConstSet(plat, mrb.Intern("OS"), mrb.StrNew(osName))
		mrb.ConstSet(plat, mrb.Intern("LIBPREFIX"), mrb.StrNew("lib"))
		mrb.ConstSet(plat, mrb.Intern("LIBSUFFIX"), mrb.StrNew("so"))
		mrb.ConstSet(plat, mrb.Intern("LIBC"), mrb.StrNew("libc.so.6"))
		mrb.ConstSet(plat, mrb.Intern("IS_MAC"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_LINUX"), mrb.TrueValue())
		mrb.ConstSet(plat, mrb.Intern("IS_WINDOWS"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_BSD"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_GNU"), mrb.TrueValue())
	case "windows":
		osName = "windows"
		mrb.ConstSet(plat, mrb.Intern("OS"), mrb.StrNew(osName))
		mrb.ConstSet(plat, mrb.Intern("LIBPREFIX"), mrb.StrNew(""))
		mrb.ConstSet(plat, mrb.Intern("LIBSUFFIX"), mrb.StrNew("dll"))
		mrb.ConstSet(plat, mrb.Intern("LIBC"), mrb.StrNew("ucrtbase.dll"))
		mrb.ConstSet(plat, mrb.Intern("IS_MAC"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_LINUX"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_WINDOWS"), mrb.TrueValue())
		mrb.ConstSet(plat, mrb.Intern("IS_BSD"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_GNU"), mrb.FalseValue())
	default:
		mrb.ConstSet(plat, mrb.Intern("OS"), mrb.StrNew(osName))
		mrb.ConstSet(plat, mrb.Intern("LIBPREFIX"), mrb.StrNew("lib"))
		mrb.ConstSet(plat, mrb.Intern("LIBSUFFIX"), mrb.StrNew("so"))
		mrb.ConstSet(plat, mrb.Intern("LIBC"), mrb.StrNew("libc.so"))
		mrb.ConstSet(plat, mrb.Intern("IS_MAC"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_LINUX"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_WINDOWS"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_BSD"), mrb.FalseValue())
		mrb.ConstSet(plat, mrb.Intern("IS_GNU"), mrb.FalseValue())
	}
	mrb.ConstSet(plat, mrb.Intern("NAME"), mrb.StrNew(arch+"-"+osName))
}

func defineTypes(mrb *oruby.MrbState, mod oruby.RClass) {
	typeCls := mrb.DefineClassUnder(mod, "Type", mrb.ObjectClass())
	rb.DataClass(typeCls)

	typeCls.DefineMethod("size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		t, _ := rb.Get[*ffiTypeDesc](mrb, self)
		return mrb.FixnumValue(t.size)
	}, mrb.ArgsNone())

	typeCls.DefineMethod("alignment", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		t, _ := rb.Get[*ffiTypeDesc](mrb, self)
		return mrb.FixnumValue(t.alignment)
	}, mrb.ArgsNone())

	typeCls.DefineMethod("__kind", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		t, _ := rb.Get[*ffiTypeDesc](mrb, self)
		return mrb.FixnumValue(int(t.kind))
	}, mrb.ArgsNone())

	builtinCls := mrb.DefineClassUnder(typeCls, "Builtin", typeCls)
	rb.DataClass(builtinCls)

	builtins := []struct {
		name string
		kind C.int
	}{
		{"VOID", C.ORUBY_FFI_VOID},
		{"BOOL", C.ORUBY_FFI_BOOL},
		{"INT8", C.ORUBY_FFI_INT8},
		{"UINT8", C.ORUBY_FFI_UINT8},
		{"INT16", C.ORUBY_FFI_INT16},
		{"UINT16", C.ORUBY_FFI_UINT16},
		{"INT32", C.ORUBY_FFI_INT32},
		{"UINT32", C.ORUBY_FFI_UINT32},
		{"INT64", C.ORUBY_FFI_INT64},
		{"UINT64", C.ORUBY_FFI_UINT64},
		{"LONG", C.ORUBY_FFI_LONG},
		{"ULONG", C.ORUBY_FFI_ULONG},
		{"FLOAT32", C.ORUBY_FFI_FLOAT},
		{"FLOAT64", C.ORUBY_FFI_DOUBLE},
		{"POINTER", C.ORUBY_FFI_POINTER},
		{"STRING", C.ORUBY_FFI_STRING},
		{"STRPTR", C.ORUBY_FFI_STRPTR},
	}
	for _, b := range builtins {
		td := makePrimitiveType(b.kind)
		v := mrb.DataWrapInterface(builtinCls, td).Value()
		mrb.ConstSet(typeCls, mrb.Intern(b.name), v)
		mrb.ConstSet(mod, mrb.Intern("TYPE_"+b.name), v)
	}
	typeAliases := []struct{ alias, target string }{
		{"CHAR", "INT8"},
		{"UCHAR", "UINT8"},
		{"SHORT", "INT16"},
		{"USHORT", "UINT16"},
		{"INT", "INT32"},
		{"UINT", "UINT32"},
		{"LONG_LONG", "INT64"},
		{"ULONG_LONG", "UINT64"},
		{"FLOAT", "FLOAT32"},
		{"DOUBLE", "FLOAT64"},
		{"LONGDOUBLE", "FLOAT64"},
		{"BUFFER_IN", "POINTER"},
		{"BUFFER_OUT", "POINTER"},
		{"BUFFER_INOUT", "POINTER"},
		{"VARARGS", "VOID"},
	}
	for _, a := range typeAliases {
		tv := mrb.ConstGet(typeCls, mrb.Intern(a.target))
		mrb.ConstSet(typeCls, mrb.Intern(a.alias), tv)
		mrb.ConstSet(mod, mrb.Intern("TYPE_"+a.alias), tv)
	}

	fnTypeCls := mrb.DefineClassUnder(mod, "FunctionType", typeCls)
	rb.DataClass(fnTypeCls)
	fnTypeCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		retVal := args.Item(0)
		paramAry := args.Item(1)
		retDesc, ok := rb.Get[*ffiTypeDesc](mrb, retVal)
		if !ok {
			return mrb.ETypeError().Raise("invalid return type for callback")
		}
		n := oruby.RArrayLen(paramAry)
		pDescs := make([]*ffiTypeDesc, n)
		for i := 0; i < n; i++ {
			d, ok := rb.Get[*ffiTypeDesc](mrb, mrb.AryRef(paramAry, i))
			if !ok {
				return mrb.ETypeError().Raise("invalid param type for callback")
			}
			pDescs[i] = d
		}
		ptrCT := C.oruby_ffi_primitive_type(C.ORUBY_FFI_POINTER)
		td := &ffiTypeDesc{
			kind:      C.ORUBY_FFI_CALLBACK,
			size:      int(ptrCT.size),
			alignment: int(ptrCT.alignment),
			ctype:     ptrCT,
			retType:   retDesc,
			argTypes:  pDescs,
		}
		rb.Set(mrb, self, td)
		mrb.IVSet(self, mrb.Intern("@__ret"), retVal)
		mrb.IVSet(self, mrb.Intern("@__params"), paramAry)
		return self
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	sbvCls := mrb.DefineClassUnder(mod, "StructByValue", typeCls)
	rb.DataClass(sbvCls)
	sbvCls.DefineClassMethod("__build", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		structCls := args.Item(0)
		fieldTypesAry := args.Item(1)
		size := args.Item(2).Int()
		align := args.Item(3).Int()

		n := oruby.RArrayLen(fieldTypesAry)
		cFields := (**C.ffi_type)(C.calloc(C.size_t(maxInt(n, 1)), C.size_t(unsafe.Sizeof((*C.ffi_type)(nil)))))
		defer C.free(unsafe.Pointer(cFields))
		slice := unsafe.Slice(cFields, maxInt(n, 1))
		for i := 0; i < n; i++ {
			d, ok := rb.Get[*ffiTypeDesc](mrb, mrb.AryRef(fieldTypesAry, i))
			if !ok {
				return mrb.ETypeError().Raise("invalid struct field type")
			}
			slice[i] = d.ctype
		}
		st := C.oruby_ffi_new_struct_type(cFields, C.int(n))
		if st == nil {
			return mrb.Raise(mrb.ERuntimeError(), "failed to allocate ffi struct type")
		}
		td := &ffiTypeDesc{
			kind:        C.ORUBY_FFI_STRUCT_VAL,
			size:        size,
			alignment:   align,
			ctype:       st,
			ownedCType:  true,
			structClass: structCls,
		}
		runtime.SetFinalizer(td, func(d *ffiTypeDesc) {
			if d.ownedCType && d.ctype != nil {
				C.oruby_ffi_free_struct_type(d.ctype)
				d.ctype = nil
			}
		})
		v := mrb.DataWrapInterface(sbvCls, td).Value()
		mrb.IVSet(v, mrb.Intern("@struct_class"), structCls)
		return v
	}, mrb.ArgsReq(4))
}

func definePointer(mrb *oruby.MrbState, mod oruby.RClass) {
	absCls := mrb.DefineClassUnder(mod, "AbstractMemory", mrb.ObjectClass())
	rb.DataClass(absCls)
	ptrCls := mrb.DefineClassUnder(mod, "Pointer", absCls)
	rb.DataClass(ptrCls)
	mpCls := mrb.DefineClassUnder(mod, "MemoryPointer", ptrCls)
	rb.DataClass(mpCls)

	absCls.DefineMethod("address", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		return mrb.Uint64Value(uint64(uintptr(p.addr)))
	}, mrb.ArgsNone())

	absCls.DefineMethod("null?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		return mrb.BoolValue(p.addr == nil)
	}, mrb.ArgsNone())

	absCls.DefineMethod("size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		return mrb.FixnumValue(p.size)
	}, mrb.ArgsNone())

	absCls.DefineMethod("total", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		return mrb.FixnumValue(p.size)
	}, mrb.ArgsNone())

	absCls.DefineMethod("type_size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		return mrb.FixnumValue(p.typeSize)
	}, mrb.ArgsNone())

	absCls.DefineMethod("clear", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		if p.addr != nil && p.size > 0 {
			C.memset(p.addr, 0, C.size_t(p.size))
		}
		return self
	}, mrb.ArgsNone())

	defineMemAccessors(mrb, absCls)

	ptrCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		var addr unsafe.Pointer
		typeSize := 1
		if args.Len() == 1 {
			a0 := args.Item(0)
			if op, ok := getPtr(mrb, a0); ok {
				addr = op.addr
				typeSize = op.typeSize
			} else {
				addr = u2p(uintptr(rubyToUint64(mrb, a0)))
			}
		} else if args.Len() >= 2 {
			ts, err := mrb.Funcall(mod, mrb.Intern("type_size"), args.Item(0))
			if err == nil {
				typeSize = ts.Int()
			}
			a1 := args.Item(1)
			if op, ok := getPtr(mrb, a1); ok {
				addr = op.addr
			} else {
				addr = u2p(uintptr(rubyToUint64(mrb, a1)))
			}
		}
		rb.Set(mrb, self, &ffiPtr{addr: addr, size: -1, typeSize: typeSize})
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	ptrCls.DefineMethod("+", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		off := mrb.GetArgsFirst().Int64()
		newSize := -1
		if p.size >= 0 {
			newSize = p.size - int(off)
			if newSize < 0 {
				newSize = 0
			}
		}
		return newPointerVal(mrb, unsafe.Add(p.addr, off), newSize, p.typeSize)
	}, mrb.ArgsReq(1))

	ptrCls.DefineMethod("slice", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		off := args.Item(0).Int64()
		sz := args.Item(1).Int()
		return newPointerVal(mrb, unsafe.Add(p.addr, off), sz, 1)
	}, mrb.ArgsReq(2))

	ptrCls.DefineMethod("==", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p1, _ := getPtr(mrb, self)
		other := mrb.GetArgsFirst()
		p2, ok := getPtr(mrb, other)
		if !ok {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(p1.addr == p2.addr)
	}, mrb.ArgsReq(1))

	mpCls.DefineClassMethod("__alloc", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		typeSize := args.Item(0).Int()
		count := args.Item(1).Int()
		clear := args.Item(2).Bool()
		if typeSize < 0 || count < 0 {
			return mrb.EArgumentError().Raise("negative memory allocation size")
		}
		total := typeSize * count
		allocSz := maxInt(total, 1)
		var mem unsafe.Pointer
		if clear {
			mem = C.calloc(1, C.size_t(allocSz))
		} else {
			mem = C.malloc(C.size_t(allocSz))
		}
		if mem == nil {
			return mrb.Raise(mrb.ERuntimeError(), "failed to allocate memory")
		}
		p := &ffiPtr{
			addr:        mem,
			size:        total,
			typeSize:    maxInt(typeSize, 1),
			owned:       true,
			autorelease: true,
		}
		runtime.SetFinalizer(p, func(fp *ffiPtr) {
			if fp.owned && fp.autorelease && fp.addr != nil {
				fp.freeNow()
			}
		})
		return mrb.DataWrapInterface(mrb.ClassPtr(self), p).Value()
	}, mrb.ArgsReq(3))

	ptrCls.DefineMethod("initialize_copy", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		orig := mrb.GetArgsFirst()
		op, ok := getPtr(mrb, orig)
		if !ok || op == nil {
			return self
		}
		cp := &ffiPtr{
			addr:     op.addr,
			size:     op.size,
			typeSize: op.typeSize,
			owned:    false,
		}
		rb.Set(mrb, self, cp)
		return self
	}, mrb.ArgsReq(1))

	mpCls.DefineMethod("initialize_copy", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		orig := mrb.GetArgsFirst()
		op, ok := getPtr(mrb, orig)
		if !ok || op == nil {
			return self
		}
		allocSz := maxInt(op.size, 1)
		mem := C.malloc(C.size_t(allocSz))
		if mem == nil {
			return mrb.Raise(mrb.ERuntimeError(), "failed to allocate memory")
		}
		if op.addr != nil && op.size > 0 {
			C.memcpy(mem, op.addr, C.size_t(op.size))
		}
		p := &ffiPtr{
			addr:        mem,
			size:        op.size,
			typeSize:    op.typeSize,
			owned:       true,
			autorelease: op.autorelease,
		}
		runtime.SetFinalizer(p, func(fp *ffiPtr) {
			if fp.owned && fp.autorelease && fp.addr != nil {
				fp.freeNow()
			}
		})
		rb.Set(mrb, self, p)
		return self
	}, mrb.ArgsReq(1))

	ptrCls.DefineMethod("free", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		p.freeNow()
		return mrb.NilValue()
	}, mrb.ArgsNone())

	ptrCls.DefineMethod("autorelease=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		b := mrb.GetArgsFirst().Bool()
		p.autorelease = b
		return mrb.BoolValue(b)
	}, mrb.ArgsReq(1))

	ptrCls.DefineMethod("autorelease?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		return mrb.BoolValue(p.autorelease)
	}, mrb.ArgsNone())
}

func defineMemAccessors(mrb *oruby.MrbState, cls oruby.RClass) {
	raiseNull := func(mrb *oruby.MrbState, err error) oruby.MrbValue {
		return rb.Raise(mrb, "FFI::NullPointerError", "%s", err.Error())
	}

	cls.DefineMethod("get_int8", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.FixnumValue(int(*(*int8)(a)))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_int8", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*int8)(a) = int8(args.Item(1).Int64())
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_uint8", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.FixnumValue(int(*(*uint8)(a)))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_uint8", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*uint8)(a) = uint8(args.Item(1).Int64())
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_int16", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.FixnumValue(int(*(*int16)(a)))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_int16", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*int16)(a) = int16(args.Item(1).Int64())
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_uint16", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.FixnumValue(int(*(*uint16)(a)))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_uint16", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*uint16)(a) = uint16(args.Item(1).Int64())
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_int32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.FixnumValue(int(*(*int32)(a)))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_int32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*int32)(a) = int32(args.Item(1).Int64())
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_uint32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.Int64Value(int64(*(*uint32)(a)))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_uint32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*uint32)(a) = uint32(args.Item(1).Int64())
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_int64", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.Int64Value(*(*int64)(a))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_int64", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*int64)(a) = args.Item(1).Int64()
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_uint64", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.Uint64Value(*(*uint64)(a))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_uint64", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*uint64)(a) = rubyToUint64(mrb, args.Item(1))
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_float32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.FloatValue(float64(*(*float32)(a)))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_float32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*float32)(a) = float32(rubyToFloat64(args.Item(1)))
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_float64", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return mrb.FloatValue(*(*float64)(a))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_float64", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		*(*float64)(a) = rubyToFloat64(args.Item(1))
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_pointer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		a, err := p.checkAddr(mrb.GetArgsFirst().Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		return newPointerVal(mrb, *(*unsafe.Pointer)(a), -1, 1)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("put_pointer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		a, err := p.checkAddr(args.Item(0).Int())
		if err != nil {
			return raiseNull(mrb, err)
		}
		target, err := extractPointer(mrb, args.Item(1))
		if err != nil {
			return mrb.ETypeError().Raise(err.Error())
		}
		*(*unsafe.Pointer)(a) = target
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("get_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		off := args.Item(0).Int()
		length := args.Item(1).Int()
		a, err := p.checkAddr(off)
		if err != nil {
			return raiseNull(mrb, err)
		}
		if length <= 0 {
			return mrb.Call(mrb.BytesValue(nil), "b")
		}
		buf := C.GoBytes(a, C.int(length))
		return mrb.Call(mrb.BytesValue(buf), "b")
	}, mrb.ArgsReq(2))

	cls.DefineMethod("put_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		off := args.Item(0).Int()
		str := args.Item(1).Bytes()
		idx := 0
		length := len(str)
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			idx = args.Item(2).Int()
			length = len(str) - idx
		}
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			length = args.Item(3).Int()
		}
		a, err := p.checkAddr(off)
		if err != nil {
			return raiseNull(mrb, err)
		}
		if length > 0 && idx >= 0 && idx+length <= len(str) {
			C.memcpy(a, unsafe.Pointer(&str[idx]), C.size_t(length))
		}
		return self
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(2))

	cls.DefineMethod("get_string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		off := args.Item(0).Int()
		a, err := p.checkAddr(off)
		if err != nil {
			return raiseNull(mrb, err)
		}
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			length := args.Item(1).Int()
			if length <= 0 {
				return mrb.StrNew("")
			}
			bs := C.GoBytes(a, C.int(length))
			for i, c := range bs {
				if c == 0 {
					bs = bs[:i]
					break
				}
			}
			return mrb.BytesValue(bs)
		}
		return mrb.StrNew(C.GoString((*C.char)(a)))
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	cls.DefineMethod("put_string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p, _ := getPtr(mrb, self)
		args := mrb.GetArgs()
		off := args.Item(0).Int()
		bs := args.Item(1).Bytes()
		a, err := p.checkAddr(off)
		if err != nil {
			return raiseNull(mrb, err)
		}
		if len(bs) > 0 {
			C.memcpy(a, unsafe.Pointer(&bs[0]), C.size_t(len(bs)))
		}
		*(*byte)(unsafe.Add(a, len(bs))) = 0
		return self
	}, mrb.ArgsReq(2))
}

func defineDynLib(mrb *oruby.MrbState, mod oruby.RClass) {
	dlCls := mrb.DefineClassUnder(mod, "DynamicLibrary", mrb.ObjectClass())
	rb.DataClass(dlCls)

	mrb.ConstSet(dlCls, mrb.Intern("RTLD_LAZY"), mrb.FixnumValue(int(C.ORUBY_RTLD_LAZY)))
	mrb.ConstSet(dlCls, mrb.Intern("RTLD_NOW"), mrb.FixnumValue(int(C.ORUBY_RTLD_NOW)))
	mrb.ConstSet(dlCls, mrb.Intern("RTLD_LOCAL"), mrb.FixnumValue(int(C.ORUBY_RTLD_LOCAL)))
	mrb.ConstSet(dlCls, mrb.Intern("RTLD_GLOBAL"), mrb.FixnumValue(int(C.ORUBY_RTLD_GLOBAL)))

	dlCls.DefineClassMethod("open", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		nameVal := args.Item(0)
		flags := int(C.ORUBY_RTLD_LAZY | C.ORUBY_RTLD_LOCAL)
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			flags = args.Item(1).Int()
		}
		var cpath *C.char
		var name string
		isSelf := false
		if nameVal.IsNil() {
			cpath = nil
			name = "[current process]"
			isSelf = true
		} else {
			name = nameVal.String()
			cpath = C.CString(name)
			defer C.free(unsafe.Pointer(cpath))
		}
		var errC *C.char
		h := C.oruby_ffi_dlopen(cpath, C.int(flags), &errC)
		if h == nil {
			msg := "could not open library"
			if errC != nil {
				msg = C.GoString(errC)
				C.free(unsafe.Pointer(errC))
			}
			return mrb.Raise(mrb.ExcGet("LoadError"), msg)
		}
		dl := &ffiDynLib{handle: h, name: name, self: isSelf}
		return mrb.DataWrapInterface(dlCls, dl).Value()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	dlCls.DefineMethod("name", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		dl, _ := rb.Get[*ffiDynLib](mrb, self)
		return mrb.StrNew(dl.name)
	}, mrb.ArgsNone())

	findSym := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		dl, _ := rb.Get[*ffiDynLib](mrb, self)
		symName := mrb.GetArgsFirst().String()
		cname := C.CString(symName)
		defer C.free(unsafe.Pointer(cname))
		var errC *C.char
		sym := C.oruby_ffi_dlsym(dl.handle, cname, &errC)
		if errC != nil {
			C.free(unsafe.Pointer(errC))
			return mrb.NilValue()
		}
		if sym == nil {
			return mrb.NilValue()
		}
		return newPointerVal(mrb, sym, -1, 1)
	}
	dlCls.DefineMethod("find_symbol", findSym, mrb.ArgsReq(1))
	dlCls.DefineMethod("find_function", findSym, mrb.ArgsReq(1))
	dlCls.DefineMethod("find_variable", findSym, mrb.ArgsReq(1))
}

func defineFunction(mrb *oruby.MrbState, mod oruby.RClass) {
	ptrCls := rb.ClassPath(mrb, "FFI::Pointer")
	fnCls := mrb.DefineClassUnder(mod, "Function", ptrCls)
	rb.DataClass(fnCls)

	fnCls.DefineClassMethod("__create", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		retVal := args.Item(0)
		argTypesVal := args.Item(1)
		targetVal := args.Item(2)
		conv := C.int(C.ORUBY_FFI_CONV_DEFAULT)
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			if args.Item(3).String() == "stdcall" {
				conv = C.ORUBY_FFI_CONV_STDCALL
			}
		}

		retDesc, ok := rb.Get[*ffiTypeDesc](mrb, retVal)
		if !ok {
			return mrb.ETypeError().Raise("invalid return type")
		}
		n := oruby.RArrayLen(argTypesVal)
		argDescs := make([]*ffiTypeDesc, n)
		for i := 0; i < n; i++ {
			d, ok := rb.Get[*ffiTypeDesc](mrb, mrb.AryRef(argTypesVal, i))
			if !ok {
				return mrb.ETypeError().Raise("invalid argument type")
			}
			argDescs[i] = d
		}

		if p, ok := getPtr(mrb, targetVal); ok {
			if p.addr == nil {
				return rb.Raise(mrb, "FFI::NullPointerError", "cannot create FFI::Function from NULL pointer")
			}
			cif, cArgs, err := prepCif(conv, retDesc, argDescs)
			if err != nil {
				return mrb.Raise(mrb.ERuntimeError(), err.Error())
			}
			fn := &ffiFunc{
				addr:      p.addr,
				retType:   retDesc,
				argTypes:  argDescs,
				cif:       cif,
				cArgTypes: cArgs,
			}
			runtime.SetFinalizer(fn, func(f *ffiFunc) {
				if f.cif != nil {
					C.free(unsafe.Pointer(f.cif))
					f.cif = nil
				}
				if f.cArgTypes != nil {
					C.free(unsafe.Pointer(f.cArgTypes))
					f.cArgTypes = nil
				}
			})
			v := mrb.DataWrapInterface(fnCls, fn).Value()
			mrb.IVSet(v, mrb.Intern("@__ret"), retVal)
			mrb.IVSet(v, mrb.Intern("@__args"), argTypesVal)
			return v
		}

		entry, cid, err := allocClosure(mrb, conv, retDesc, argDescs, targetVal)
		if err != nil {
			return mrb.Raise(mrb.ERuntimeError(), err.Error())
		}
		fn := &ffiFunc{
			addr:      entry.codeloc,
			retType:   retDesc,
			argTypes:  argDescs,
			cif:       entry.cif,
			cArgTypes: entry.cArgTypes,
			closureID: cid,
		}
		runtime.SetFinalizer(fn, func(f *ffiFunc) {
			if f.closureID != 0 {
				freeClosure(f.closureID)
				f.closureID = 0
			}
		})
		v := mrb.DataWrapInterface(fnCls, fn).Value()
		mrb.IVSet(v, mrb.Intern("@__proc"), targetVal)
		mrb.IVSet(v, mrb.Intern("@__ret"), retVal)
		mrb.IVSet(v, mrb.Intern("@__args"), argTypesVal)
		return v
	}, mrb.ArgsReq(3)|mrb.ArgsOpt(1))

	fnCls.DefineMethod("address", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fn, _ := rb.Get[*ffiFunc](mrb, self)
		return mrb.Uint64Value(uint64(uintptr(fn.addr)))
	}, mrb.ArgsNone())

	fnCls.DefineMethod("null?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fn, _ := rb.Get[*ffiFunc](mrb, self)
		return mrb.BoolValue(fn.addr == nil)
	}, mrb.ArgsNone())

	fnCls.DefineMethod("free", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fn, _ := rb.Get[*ffiFunc](mrb, self)
		if fn.closureID != 0 {
			freeClosure(fn.closureID)
			fn.closureID = 0
			fn.addr = nil
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	fnCls.DefineMethod("call", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fn, _ := rb.Get[*ffiFunc](mrb, self)
		args := mrb.GetArgs()
		n := len(fn.argTypes)
		if args.Len() != n {
			return mrb.EArgumentError().Raisef("wrong number of arguments (given %d, expected %d)", args.Len(), n)
		}

		var cleanups []func()
		defer func() {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		}()

		var avalues *unsafe.Pointer
		if n > 0 {
			avalues = (*unsafe.Pointer)(C.calloc(C.size_t(n), C.size_t(unsafe.Sizeof(unsafe.Pointer(nil)))))
			cleanups = append(cleanups, func() { C.free(unsafe.Pointer(avalues)) })
			avSlice := unsafe.Slice(avalues, n)
			for i := 0; i < n; i++ {
				slot, err := marshalArg(mrb, fn.argTypes[i], args.Item(i), &cleanups)
				if err != nil {
					return mrb.ETypeError().Raise(err.Error())
				}
				avSlice[i] = slot
			}
		}

		retSz := maxInt(int(unsafe.Sizeof(C.ffi_arg(0))), fn.retType.size)
		rval := C.calloc(1, C.size_t(retSz))
		cleanups = append(cleanups, func() { C.free(rval) })

		var cErrno C.int
		C.oruby_ffi_call_cif(fn.cif, fn.addr, rval, avalues, &cErrno)
		setErrno(mrb, int(cErrno))

		return cReturnToRuby(mrb, fn.retType, rval)
	}, mrb.ArgsAny())
}

func defineVariadic(mrb *oruby.MrbState, mod oruby.RClass) {
	vCls := mrb.DefineClassUnder(mod, "VariadicInvoker", mrb.ObjectClass())

	vCls.DefineClassMethod("__invoke", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		fnPtrVal := args.Item(0)
		retVal := args.Item(1)
		nfixed := args.Item(2).Int()
		typesAry := args.Item(3)
		valsAry := args.Item(4)

		fnPtr, err := extractPointer(mrb, fnPtrVal)
		if err != nil || fnPtr == nil {
			return rb.Raise(mrb, "FFI::NullPointerError", "invalid function pointer")
		}
		retDesc, ok := rb.Get[*ffiTypeDesc](mrb, retVal)
		if !ok {
			return mrb.ETypeError().Raise("invalid return type")
		}
		ntotal := oruby.RArrayLen(typesAry)
		if oruby.RArrayLen(valsAry) != ntotal {
			return mrb.EArgumentError().Raise("variadic types/values length mismatch")
		}

		var cleanups []func()
		defer func() {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		}()

		var cArgTypes **C.ffi_type
		var avalues *unsafe.Pointer
		if ntotal > 0 {
			cArgTypes = (**C.ffi_type)(C.calloc(C.size_t(ntotal), C.size_t(unsafe.Sizeof((*C.ffi_type)(nil)))))
			cleanups = append(cleanups, func() { C.free(unsafe.Pointer(cArgTypes)) })
			atSlice := unsafe.Slice(cArgTypes, ntotal)

			avalues = (*unsafe.Pointer)(C.calloc(C.size_t(ntotal), C.size_t(unsafe.Sizeof(unsafe.Pointer(nil)))))
			cleanups = append(cleanups, func() { C.free(unsafe.Pointer(avalues)) })
			avSlice := unsafe.Slice(avalues, ntotal)

			for i := 0; i < ntotal; i++ {
				td, ok := rb.Get[*ffiTypeDesc](mrb, mrb.AryRef(typesAry, i))
				if !ok {
					return mrb.ETypeError().Raise("invalid variadic argument type")
				}
				if i >= nfixed {
					switch td.kind {
					case C.ORUBY_FFI_FLOAT:
						td = makePrimitiveType(C.ORUBY_FFI_DOUBLE)
					case C.ORUBY_FFI_INT8, C.ORUBY_FFI_INT16:
						td = makePrimitiveType(C.ORUBY_FFI_INT32)
					case C.ORUBY_FFI_UINT8, C.ORUBY_FFI_UINT16, C.ORUBY_FFI_BOOL:
						td = makePrimitiveType(C.ORUBY_FFI_UINT32)
					}
				}
				atSlice[i] = td.ctype
				slot, err := marshalArg(mrb, td, mrb.AryRef(valsAry, i), &cleanups)
				if err != nil {
					return mrb.ETypeError().Raise(err.Error())
				}
				avSlice[i] = slot
			}
		}

		retSz := maxInt(int(unsafe.Sizeof(C.ffi_arg(0))), retDesc.size)
		rval := C.calloc(1, C.size_t(retSz))
		cleanups = append(cleanups, func() { C.free(rval) })

		conv := C.int(C.ORUBY_FFI_CONV_DEFAULT)
		if args.Len() >= 6 && !args.Item(5).IsNil() && args.Item(5).String() == "stdcall" {
			conv = C.ORUBY_FFI_CONV_STDCALL
		}

		var cErrno C.int
		st := C.oruby_ffi_call_var(conv, retDesc.ctype, C.uint(nfixed), C.uint(ntotal), cArgTypes, fnPtr, rval, avalues, &cErrno)
		if st != 0 {
			return mrb.Raise(mrb.ERuntimeError(), fmt.Sprintf("ffi_prep_cif_var failed (%d)", int(st)))
		}
		setErrno(mrb, int(cErrno))
		return cReturnToRuby(mrb, retDesc, rval)
	}, mrb.ArgsReq(5)|mrb.ArgsOpt(1))
}
