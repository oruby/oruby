package oruby

// #include "go-mrb.h"
// #include "go-aot.h"
import "C"
import (
	"bytes"
	"unicode/utf8"
	"unsafe"
)

// AotCoreID identifies a core Ruby stdlib method eligible for guarded zero-cgo
// AOT fast-path execution.
type AotCoreID int

const (
	AotCoreAryEach AotCoreID = iota
	AotCoreAryLen
	AotCoreArySize
	AotCoreAryEmpty
	AotCoreAryFirst
	AotCoreAryLast
	AotCoreAryIdx
	AotCoreAryMap
	AotCoreAryCollect
	AotCoreArySelect
	AotCoreAryEachWithIndex
	AotCoreArySum
	AotCoreAryInject
	AotCoreAryReduce
	AotCoreIntTimes
	AotCoreIntUpto
	AotCoreIntDownto
	AotCoreIntStep
	AotCoreRangeEach
	AotCoreStrBytesize
	AotCoreStrLen
	AotCoreStrSize
	AotCoreStrEmpty
	AotCoreStrGetbyte
	AotCoreStrOrd
	AotCoreStrStartWith
	AotCoreStrInclude
	AotCoreStrConcat
	AotCoreStrStrip
	AotCoreStrDowncase
	AotCoreStrUpcase
	AotCoreStrGsub
	AotCoreStrSplit
	AotCoreTrueNot
	AotCoreFalseNot
	AotCoreNilNot
	AotCoreHashSize
	AotCoreHashLength
	AotCoreHashEmpty
	AotCoreMathSqrt
	AotCoreMathSin
	AotCoreMathCos
	AotCoreMathTan
	AotCoreMathLog
	AotCoreMathLog2
	AotCoreMathLog10
	AotCoreMathExp
	AotCoreMathHypot
	aotCoreCount
)

func (mrb *MrbState) aotInitCoreIntrinsics(st *aotState) {
	regCls := func(id AotCoreID, cls *C.struct_RClass, name string) {
		if cls == nil {
			return
		}
		mid := C.mrb_sym(mrb.Intern(name))
		var found C.mrb_bool
		m := C._aot_intrinsic_method(mrb.p, cls, mid, &found)
		if !bool(found) {
			return
		}
		st.coreIntrinsics[id] = aotIntrinsicSlot{
			cls:    cls,
			mid:    mid,
			method: m,
			way0:   aotMethodCacheWay0(cls, mid),
		}
	}

	ac := mrb.p.array_class
	regCls(AotCoreAryEach, ac, "each")
	regCls(AotCoreAryLen, ac, "length")
	regCls(AotCoreArySize, ac, "size")
	regCls(AotCoreAryEmpty, ac, "empty?")
	regCls(AotCoreAryFirst, ac, "first")
	regCls(AotCoreAryLast, ac, "last")
	regCls(AotCoreAryIdx, ac, "[]")
	regCls(AotCoreAryMap, ac, "map")
	regCls(AotCoreAryCollect, ac, "collect")
	regCls(AotCoreArySelect, ac, "select")
	regCls(AotCoreAryEachWithIndex, ac, "each_with_index")
	regCls(AotCoreArySum, ac, "sum")
	regCls(AotCoreAryInject, ac, "inject")
	regCls(AotCoreAryReduce, ac, "reduce")

	regCls(AotCoreIntTimes, mrb.p.integer_class, "times")
	regCls(AotCoreIntUpto, mrb.p.integer_class, "upto")
	regCls(AotCoreIntDownto, mrb.p.integer_class, "downto")
	regCls(AotCoreIntStep, mrb.p.integer_class, "step")
	regCls(AotCoreRangeEach, mrb.p.range_class, "each")

	sc := mrb.p.string_class
	regCls(AotCoreStrBytesize, sc, "bytesize")
	regCls(AotCoreStrLen, sc, "length")
	regCls(AotCoreStrSize, sc, "size")
	regCls(AotCoreStrEmpty, sc, "empty?")
	regCls(AotCoreStrGetbyte, sc, "getbyte")
	regCls(AotCoreStrOrd, sc, "ord")
	regCls(AotCoreStrStartWith, sc, "start_with?")
	regCls(AotCoreStrInclude, sc, "include?")
	regCls(AotCoreStrConcat, sc, "<<")
	regCls(AotCoreStrStrip, sc, "strip")
	regCls(AotCoreStrDowncase, sc, "downcase")
	regCls(AotCoreStrUpcase, sc, "upcase")
	regCls(AotCoreStrGsub, sc, "gsub")
	regCls(AotCoreStrSplit, sc, "split")

	regCls(AotCoreTrueNot, mrb.p.true_class, "!")
	regCls(AotCoreFalseNot, mrb.p.false_class, "!")
	regCls(AotCoreNilNot, mrb.p.nil_class, "!")

	hc := mrb.p.hash_class
	regCls(AotCoreHashSize, hc, "size")
	regCls(AotCoreHashLength, hc, "length")
	regCls(AotCoreHashEmpty, hc, "empty?")

	if mrb.ConstDefined(mrb.ObjectClass(), mrb.Intern("Math")) {
		mathMod := mrb.ConstGet(mrb.ObjectClass(), mrb.Intern("Math"))
		if mathMod.Type() == MrbTTModule && mathMod.Pointer() > 65536 {
			scls := C._aot_singleton_class_ptr(mrb.p, mathMod.v)
			modPtr := mathMod.Pointer()
			regMath := func(id AotCoreID, name string) {
				mid := C.mrb_sym(mrb.Intern(name))
				var found C.mrb_bool
				m := C._aot_intrinsic_method(mrb.p, scls, mid, &found)
				if !bool(found) {
					return
				}
				st.coreIntrinsics[id] = aotIntrinsicSlot{
					cls:    scls,
					modPtr: modPtr,
					mid:    mid,
					method: m,
				}
			}
			regMath(AotCoreMathSqrt, "sqrt")
			regMath(AotCoreMathSin, "sin")
			regMath(AotCoreMathCos, "cos")
			regMath(AotCoreMathTan, "tan")
			regMath(AotCoreMathLog, "log")
			regMath(AotCoreMathLog2, "log2")
			regMath(AotCoreMathLog10, "log10")
			regMath(AotCoreMathExp, "exp")
			regMath(AotCoreMathHypot, "hypot")
		}
	}
}

//go:noinline
func (mrb *MrbState) aotCacheMatchSlow(cls *C.struct_RClass, mid C.mrb_sym, expected C.mrb_method_t) bool {
	var found C.mrb_bool
	m := C._aot_intrinsic_method(mrb.p, cls, mid, &found)
	return bool(found) && m == expected
}

// aotCacheMatch checks mruby's 2-way method cache for (cls, mid) == expected
// in pure Go, warming the cache once on a cold miss.
func (mrb *MrbState) aotCacheMatch(cls *C.struct_RClass, mid C.mrb_sym, expected C.mrb_method_t) bool {
	if (mrb.p.refined_mids[0] | mrb.p.refined_mids[1] | mrb.p.refined_mids[2] | mrb.p.refined_mids[3]) != 0 {
		return false
	}
	way0 := aotMethodCacheWay0(cls, mid)
	mc0 := &mrb.p.cache[way0]
	if mc0.c == cls && mc0.mid == mid {
		return mc0.m == expected
	}
	mc1 := &mrb.p.cache[way0+1]
	if mc1.c == cls && mc1.mid == mid {
		return mc1.m == expected
	}
	return mrb.aotCacheMatchSlow(cls, mid, expected)
}

// AotIdxSlot names a slot of mruby's idx_class (the guards the VM's inline
// index opcodes use). The slot holds the core class while its builtin is the
// method in force and NULL once it is overridden, refined or a module is
// prepended; mruby keeps it up to date on every method definition.
type AotIdxSlot int

const (
	AotIdxAryAref  = AotIdxSlot(C.MRB_IDX_OP_ARY_AREF)  // Array#[]
	AotIdxHashAref = AotIdxSlot(C.MRB_IDX_OP_HASH_AREF) // Hash#[]
	AotIdxStrAref  = AotIdxSlot(C.MRB_IDX_OP_STR_AREF)  // String#[]
	AotIdxAryAset  = AotIdxSlot(C.MRB_IDX_OP_ARY_ASET)  // Array#[]=
	AotIdxHashAset = AotIdxSlot(C.MRB_IDX_OP_HASH_ASET) // Hash#[]=
	AotIdxStrAset  = AotIdxSlot(C.MRB_IDX_OP_STR_ASET)  // String#[]=
	AotIdxStrAdd   = AotIdxSlot(C.MRB_IDX_OP_STR_ADD)   // String#+
	AotIdxStrEq    = AotIdxSlot(C.MRB_IDX_OP_STR_EQ)    // String#==
)

// AotIdxOK reports whether recv is an instance of exactly the core class slot
// guards and its builtin is still the method in force: one pointer comparison
// in pure Go, as mruby's own OP_GETIDX does. A subclass, a singleton, an
// override and a refinement all fail it, so a user class that defines [] (the
// common case) never affects it.
func (mrb *MrbState) AotIdxOK(slot AotIdxSlot, recv Value) bool {
	if recv.v.tt <= C.MRB_TT_CPTR { // an immediate has no class pointer to read
		return false
	}
	return (*C.struct_RBasic)(aotObjPtr(recv)).c == mrb.p.idx_class[slot]
}

// AotCoreOK checks in pure Go (zero cgo on warm cache) that calling core
// method id on recv resolves to the original unmodified core method (exact
// core class, no monkey-patch, subclass, singleton override, or refinement).
func (mrb *MrbState) AotCoreOK(id AotCoreID, recv Value) bool {
	if mrb.aot == nil || id < 0 || id >= aotCoreCount {
		return false
	}
	s := &mrb.aot.coreIntrinsics[id]
	if s.cls == nil {
		return false
	}
	if s.modPtr != 0 {
		if recv.Pointer() != s.modPtr {
			return false
		}
		if (*C.struct_RBasic)(aotObjPtr(recv)).c != s.cls {
			return false
		}
	} else {
		oc := mrb.aotReceiverClass(recv)
		if oc != s.cls {
			return false
		}
	}
	return mrb.aotCacheMatch(s.cls, s.mid, s.method)
}

// AotCoreClassOK verifies that core method id on its registered class/module
// has not been monkey-patched or refined (zero cgo on warm cache).
func (mrb *MrbState) AotCoreClassOK(id AotCoreID) bool {
	if mrb.aot == nil || id < 0 || id >= aotCoreCount {
		return false
	}
	s := &mrb.aot.coreIntrinsics[id]
	if s.cls == nil {
		return false
	}
	return mrb.aotCacheMatch(s.cls, s.mid, s.method)
}

//go:noinline
func (mrb *MrbState) aotProtectSlow(v Value) Value {
	C.mrb_gc_protect(mrb.p, v.v)
	return v
}

// aotProtect registers a heap Value in mruby's GC arena in pure Go (zero cgo),
// falling back to C only when the arena needs to grow. Immediate values are
// returned untouched.
func (mrb *MrbState) aotProtect(v Value) Value {
	if v.v.tt <= C.MRB_TT_CPTR {
		return v
	}
	idx := mrb.p.gc.arena_idx
	if idx < mrb.p.gc.arena_capa && mrb.p.gc.arena != nil {
		slot := (**C.struct_RBasic)(unsafe.Add(unsafe.Pointer(mrb.p.gc.arena), uintptr(idx)*unsafe.Sizeof((*C.struct_RBasic)(nil))))
		*slot = (*C.struct_RBasic)(aotObjPtr(v))
		mrb.p.gc.arena_idx = idx + 1
		return v
	}
	return mrb.aotProtectSlow(v)
}

// aotAryMem returns the element pointer and length of an RArray in pure Go.
func aotAryMem(ary Value) (*C.mrb_value, int) {
	if ary.Type() != MrbTTArray || ary.Pointer() <= 65536 {
		return nil, 0
	}
	up := aotObjPtr(ary)
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	if embed := flags & 7; embed != 0 { // MRB_ARY_EMBED_MASK
		return (*C.mrb_value)(unsafe.Add(up, 16)), int(embed - 1)
	}
	n := int(*(*int64)(unsafe.Add(up, 16)))
	if n <= 0 {
		return nil, 0
	}
	return *(**C.mrb_value)(unsafe.Add(up, 32)), n
}

// aotStructSlotRef reads slot i of a Struct or Data object (MRB_TT_STRUCT,
// which uses the RArray layout) in pure Go without cgo.
func (mrb *MrbState) aotStructSlotRef(obj Value, i int) (Value, bool) {
	if obj.Type() != MrbTTStruct || obj.Pointer() <= 65536 || i < 0 {
		return nilValue, false
	}
	up := aotObjPtr(obj)
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	var ptr *C.mrb_value
	var n int
	if embed := flags & 7; embed != 0 {
		ptr = (*C.mrb_value)(unsafe.Add(up, 16))
		n = int(embed - 1)
	} else {
		n = int(*(*int64)(unsafe.Add(up, 16)))
		ptr = *(**C.mrb_value)(unsafe.Add(up, 32))
	}
	if i >= n || ptr == nil {
		return nilValue, true
	}
	v := *(*C.mrb_value)(unsafe.Add(unsafe.Pointer(ptr), uintptr(i)*unsafe.Sizeof(C.mrb_value{})))
	return mrb.aotProtect(Value{v}), true
}

// AotAryNewCapa allocates an empty Ruby Array with capacity n and protects it
// in the current GC arena.
func (mrb *MrbState) AotAryNewCapa(n int) Value {
	return mrb.aotProtect(Value{C.mrb_ary_new_capa(mrb.p, C.mrb_int(n))})
}

//go:noinline
func (mrb *MrbState) aotAryPushSlow(ary, v Value) {
	C.mrb_ary_push(mrb.p, ary.v, v.v)
}

// AotAryPushFast appends v to a non-frozen, non-shared plain Array in pure Go
// when capacity suffices, falling back to C.mrb_ary_push only on growth,
// frozen/shared arrays, or non-Array values.
func (mrb *MrbState) AotAryPushFast(ary, v Value) {
	if ary.Type() == MrbTTArray && ary.Pointer() > 65536 {
		up := aotObjPtr(ary)
		word := *(*uint32)(unsafe.Add(up, 8))
		if (word & (1 << 11)) == 0 { // not frozen
			flags := word >> 12
			if embed := flags & 7; embed != 0 {
				n := int(embed - 1)
				if n < int(C.MRB_ARY_EMBED_LEN_MAX) {
					slot := (*C.mrb_value)(unsafe.Add(up, 16+uintptr(n)*unsafe.Sizeof(C.mrb_value{})))
					*slot = v.v
					*(*uint32)(unsafe.Add(up, 8)) = (word &^ (7 << 12)) | (uint32(n+2) << 12)
					if v.v.tt > C.MRB_TT_CPTR && (word&(7<<8)) == (4<<8) {
						mrb.aotWBSlow(ary)
					}
					return
				}
			} else if (flags & 256) == 0 { // not MRB_ARY_SHARED
				n := *(*int64)(unsafe.Add(up, 16))
				capa := *(*int64)(unsafe.Add(up, 24))
				if n < capa {
					if ptr := *(**C.mrb_value)(unsafe.Add(up, 32)); ptr != nil {
						slot := (*C.mrb_value)(unsafe.Add(unsafe.Pointer(ptr), uintptr(n)*unsafe.Sizeof(C.mrb_value{})))
						*slot = v.v
						*(*int64)(unsafe.Add(up, 16)) = n + 1
						if v.v.tt > C.MRB_TT_CPTR && (word&(7<<8)) == (4<<8) {
							mrb.aotWBSlow(ary)
						}
						return
					}
				}
			}
		}
	}
	mrb.aotAryPushSlow(ary, v)
}

// AotAryCapaSlice returns a mutable Go slice over the full allocated capacity
// of a non-frozen, non-shared plain Array in pure Go.
func (mrb *MrbState) AotAryCapaSlice(ary Value) ([]Value, bool) {
	if ary.Type() != MrbTTArray || ary.Pointer() <= 65536 {
		return nil, false
	}
	up := aotObjPtr(ary)
	if (*C.struct_RBasic)(up).c != mrb.p.array_class {
		return nil, false
	}
	word := *(*uint32)(unsafe.Add(up, 8))
	if (word & (1 << 11)) != 0 { // frozen
		return nil, false
	}
	flags := word >> 12
	if (flags & 7) != 0 { // embedded
		ptr := (*Value)(unsafe.Add(up, 16))
		return unsafe.Slice(ptr, int(C.MRB_ARY_EMBED_LEN_MAX)), true
	}
	if (flags & 256) != 0 { // MRB_ARY_SHARED
		return nil, false
	}
	capa := int(*(*int64)(unsafe.Add(up, 24)))
	if capa <= 0 {
		return nil, true
	}
	ptr := *(**Value)(unsafe.Add(up, 32))
	if ptr == nil {
		return nil, false
	}
	return unsafe.Slice(ptr, capa), true
}

// AotArySetLen updates the length of a non-frozen, non-shared plain Array in
// pure Go (caller must ensure n <= capacity).
func (mrb *MrbState) AotArySetLen(ary Value, n int) {
	up := aotObjPtr(ary)
	word := *(*uint32)(unsafe.Add(up, 8))
	if ((word >> 12) & 7) != 0 {
		*(*uint32)(unsafe.Add(up, 8)) = (word &^ (7 << 12)) | (uint32(n+1) << 12)
		return
	}
	*(*int64)(unsafe.Add(up, 16)) = int64(n)
}

// AotArySlice returns a read-only Go slice over the backing mrb_value buffer of
// a plain Array in pure Go (zero cgo).
func (mrb *MrbState) AotArySlice(ary Value) ([]Value, bool) {
	if ary.Type() != MrbTTArray || ary.Pointer() <= 65536 {
		return nil, false
	}
	up := aotObjPtr(ary)
	if (*C.struct_RBasic)(up).c != mrb.p.array_class {
		return nil, false
	}
	ptr, n := aotAryMem(ary)
	if n == 0 || ptr == nil {
		return nil, true
	}
	return unsafe.Slice((*Value)(unsafe.Pointer(ptr)), n), true
}

// AotArySliceMutable returns a mutable Go slice over the backing mrb_value
// buffer of a non-frozen, non-shared plain Array in pure Go (for immediate
// Fixnum/Float/Bool writes without write barriers).
func (mrb *MrbState) AotArySliceMutable(ary Value) ([]Value, bool) {
	if ary.Type() != MrbTTArray || ary.Pointer() <= 65536 {
		return nil, false
	}
	up := aotObjPtr(ary)
	if (*C.struct_RBasic)(up).c != mrb.p.array_class {
		return nil, false
	}
	word := *(*uint32)(unsafe.Add(up, 8))
	if (word & (1 << 11)) != 0 { // frozen
		return nil, false
	}
	flags := word >> 12
	if (flags & 256) != 0 { // MRB_ARY_SHARED
		return nil, false
	}
	ptr, n := aotAryMem(ary)
	if n == 0 || ptr == nil {
		return nil, true
	}
	return unsafe.Slice((*Value)(unsafe.Pointer(ptr)), n), true
}

// AotAryRefRaw reads element i of a plain Array in pure Go without GC arena
// protection (caller must either extract an immediate payload immediately or
// keep ary alive). Returns (val, true) if in bounds, (nilValue, false) if OOB.
func (mrb *MrbState) AotAryRefRaw(ary Value, i int) (Value, bool) {
	ptr, n := aotAryMem(ary)
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n || ptr == nil {
		return nilValue, false
	}
	v := *(*C.mrb_value)(unsafe.Add(unsafe.Pointer(ptr), uintptr(i)*unsafe.Sizeof(C.mrb_value{})))
	return Value{v}, true
}

// AotHashSize returns the number of entries in a plain Hash in pure Go.
func (mrb *MrbState) AotHashSize(h Value) int {
	if h.Type() != MrbTTHash || h.Pointer() <= 65536 {
		return 0
	}
	return int((*C.struct_RHash)(aotObjPtr(h)).size)
}

// AotRangeIntBounds extracts integer range bounds (beg, end, excl) in pure Go
// for an initialized plain Range whose endpoints are both Fixnums.
func (mrb *MrbState) AotRangeIntBounds(v Value) (beg, end int64, excl, ok bool) {
	if v.Type() != MrbTTRange || v.Pointer() <= 65536 {
		return 0, 0, false, false
	}
	up := aotObjPtr(v)
	if (*C.struct_RBasic)(up).c != mrb.p.range_class {
		return 0, 0, false, false
	}
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	if (flags & 1) == 0 { // RANGE_INITIALIZED_FLAG
		return 0, 0, false, false
	}
	edges := *(**C.mrb_range_edges)(unsafe.Add(up, 16))
	if edges == nil || edges.beg.tt != C.MRB_TT_INTEGER || edges.end.tt != C.MRB_TT_INTEGER {
		return 0, 0, false, false
	}
	beg = *(*int64)(unsafe.Pointer(&edges.beg))
	end = *(*int64)(unsafe.Pointer(&edges.end))
	excl = *(*uint8)(unsafe.Add(up, 24)) != 0
	return beg, end, excl, true
}

// AotBigInt creates a Ruby BigInt from irep pool digits and base.
func (mrb *MrbState) AotBigInt(digits string, base int8) Value {
	if len(digits) == 0 {
		return Value{C._aot_bigint_new_str(mrb.p, nil, 0, C.int8_t(base))}
	}
	b := unsafe.StringData(digits)
	return Value{C._aot_bigint_new_str(mrb.p, (*C.char)(unsafe.Pointer(b)), C.mrb_int(len(digits)), C.int8_t(base))}
}

// aotShapeLookup finds the slot index of sym in a shaped RObject's iv_shape
// in pure Go, using mruby's per-state iv_cache for deep shapes (> 12 ivars).
func (mrb *MrbState) aotShapeLookup(shape *C.mrb_iv_shape, sym C.mrb_sym) int {
	if shape == nil {
		return -1
	}
	if shape.count > 12 {
		h := ((uintptr(unsafe.Pointer(shape)) >> 4) ^ (uintptr(sym) * 2654435761)) & (C.MRB_IV_CACHE_SIZE - 1)
		e := &mrb.p.iv_cache[h]
		if e.shape == shape && e.sym == sym {
			return int(e.idx)
		}
		idx := -1
		for s := shape; s != nil && s.count > 0; s = s.parent {
			if s.edge == sym {
				idx = int(s.count) - 1
				break
			}
		}
		e.shape = shape
		e.sym = sym
		e.idx = C.int(idx)
		return idx
	}
	for s := shape; s != nil && s.count > 0; s = s.parent {
		if s.edge == sym {
			return int(s.count) - 1
		}
	}
	return -1
}

// AotIVGetShaped reads an instance variable from a shaped RObject in pure Go
// without cgo. Returns (val, true) when obj is a shaped MRB_TT_OBJECT (val is
// nilValue if unset); returns (nilValue, false) if caller should fall back to C.
func (mrb *MrbState) AotIVGetShaped(obj Value, sym MrbSym) (Value, bool) {
	if obj.Type() != MrbTTObject || obj.Pointer() <= 65536 {
		return nilValue, false
	}
	up := aotObjPtr(obj)
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	if (flags & (1 << 5)) == 0 { // MRB_FL_OBJ_SHAPED
		return nilValue, false
	}
	siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16))
	if siv == nil {
		return nilValue, true
	}
	idx := mrb.aotShapeLookup(siv.shape, C.mrb_sym(sym))
	if idx < 0 {
		return nilValue, true
	}
	v := *(*C.mrb_value)(unsafe.Add(unsafe.Pointer(&siv.values[0]), uintptr(idx)*unsafe.Sizeof(C.mrb_value{})))
	if v.tt == C.MRB_TT_UNDEF {
		return nilValue, true
	}
	return mrb.aotProtect(Value{v}), true
}

// AotIVSetImmediate writes an immediate Value (Fixnum/Float/Bool/Nil/Symbol)
// into an existing slot of a non-frozen shaped RObject in pure Go (zero cgo,
// no GC write barrier needed for immediates). Returns false if frozen, unshaped,
// or if the ivar slot does not yet exist (requiring shape transition in C).
func (mrb *MrbState) AotIVSetImmediate(obj Value, sym MrbSym, val Value) bool {
	if val.v.tt > C.MRB_TT_CPTR || obj.Type() != MrbTTObject || obj.Pointer() <= 65536 {
		return false
	}
	up := aotObjPtr(obj)
	word := *(*uint32)(unsafe.Add(up, 8))
	// bit 11 is frozen:1 (tt:8, gc_color:3, frozen:1, flags:20)
	if (word & (1 << 11)) != 0 {
		return false
	}
	flags := word >> 12
	if (flags & (1 << 5)) == 0 { // MRB_FL_OBJ_SHAPED
		return false
	}
	siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16))
	if siv == nil {
		return false
	}
	idx := mrb.aotShapeLookup(siv.shape, C.mrb_sym(sym))
	if idx < 0 {
		return false
	}
	slot := (*C.mrb_value)(unsafe.Add(unsafe.Pointer(&siv.values[0]), uintptr(idx)*unsafe.Sizeof(C.mrb_value{})))
	*slot = val.v
	return true
}

const (
	aotFastAttrRead  = -2
	aotFastAttrWrite = -3
	aotFastStructRef = -4
)

// AotShapedBase returns the base values pointer and shape of a shaped RObject
// in pure Go (zero cgo) so _typed methods can hoist slot lookups once at entry.
func (mrb *MrbState) AotShapedBase(obj Value) (unsafe.Pointer, unsafe.Pointer, bool) {
	if obj.Type() != MrbTTObject || obj.Pointer() <= 65536 {
		return nil, nil, false
	}
	up := aotObjPtr(obj)
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	if (flags & (1 << 5)) == 0 { // MRB_FL_OBJ_SHAPED
		return nil, nil, false
	}
	siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16))
	if siv == nil || siv.shape == nil {
		return nil, nil, false
	}
	return unsafe.Pointer(&siv.values[0]), unsafe.Pointer(siv.shape), true
}

// AotShapedBaseMutable is AotShapedBase plus a non-frozen check, used by
// call-free _typed methods that promote @ivars into Go locals and flush on exit.
func (mrb *MrbState) AotShapedBaseMutable(obj Value) (unsafe.Pointer, unsafe.Pointer, bool) {
	if obj.Type() != MrbTTObject || obj.Pointer() <= 65536 {
		return nil, nil, false
	}
	up := aotObjPtr(obj)
	word := *(*uint32)(unsafe.Add(up, 8))
	if (word & (1 << 11)) != 0 { // frozen
		return nil, nil, false
	}
	flags := word >> 12
	if (flags & (1 << 5)) == 0 { // MRB_FL_OBJ_SHAPED
		return nil, nil, false
	}
	siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16))
	if siv == nil || siv.shape == nil {
		return nil, nil, false
	}
	return unsafe.Pointer(&siv.values[0]), unsafe.Pointer(siv.shape), true
}

// AotShapedWriteUnchecked writes an immediate Value into a pre-resolved shaped
// ivar slot when the caller already verified !frozen via AotShapedBaseMutable.
func (mrb *MrbState) AotShapedWriteUnchecked(valsPtr unsafe.Pointer, idx int, v Value) {
	slot := (*C.mrb_value)(unsafe.Add(valsPtr, uintptr(idx)*unsafe.Sizeof(C.mrb_value{})))
	*slot = v.v
}

// AotShapedSlot resolves the slot index of sym in a shape previously returned by
// AotShapedBase, or -1 if absent.
func (mrb *MrbState) AotShapedSlot(shapePtr unsafe.Pointer, sym MrbSym) int {
	return mrb.aotShapeLookup((*C.mrb_iv_shape)(shapePtr), C.mrb_sym(sym))
}

// AotShapedRead reads an already-resolved shaped ivar slot in pure Go.
func (mrb *MrbState) AotShapedRead(valsPtr unsafe.Pointer, idx int) Value {
	v := *(*C.mrb_value)(unsafe.Add(valsPtr, uintptr(idx)*unsafe.Sizeof(C.mrb_value{})))
	if v.tt == C.MRB_TT_UNDEF {
		return nilValue
	}
	return Value{v}
}

// AotShapedWriteImm writes an immediate Value into a pre-resolved shaped ivar
// slot on a non-frozen object in pure Go (zero cgo, no write barrier needed).
func (mrb *MrbState) AotShapedWriteImm(obj Value, valsPtr unsafe.Pointer, idx int, v Value) bool {
	up := aotObjPtr(obj)
	if (*(*uint32)(unsafe.Add(up, 8)) & (1 << 11)) != 0 { // frozen
		return false
	}
	slot := (*C.mrb_value)(unsafe.Add(valsPtr, uintptr(idx)*unsafe.Sizeof(C.mrb_value{})))
	*slot = v.v
	return true
}

// aotMethodIdxFromCache probes mruby's 2-way method cache in pure Go (zero cgo)
// to resolve recv.mid directly to a compiled AOT method index and owner class.
// Returns (idx, owner, fastVal, mcIdx, mcMethod, hit):
//   - hit == true && idx >= 0: cache hit on a callable compiled method
//   - hit == true && idx < 0:  cache hit on a non-compiled or visibility-gated method
//   - hit == false:            cache miss (caller should probe C to warm cache)
func (mrb *MrbState) aotMethodIdxFromCache(recv Value, mid MrbSym, fcall bool) (idx int, owner *C.struct_RClass, fastVal uintptr, mcIdx uint32, mcMethod C.mrb_method_t, hit bool) {
	if (mrb.p.refined_mids[0] | mrb.p.refined_mids[1] | mrb.p.refined_mids[2] | mrb.p.refined_mids[3]) != 0 {
		return -1, nil, 0, 0, C.mrb_method_t{}, false
	}
	oc := mrb.aotReceiverClass(recv)
	if oc == nil {
		return -1, nil, 0, 0, C.mrb_method_t{}, false
	}
	cmid := C.mrb_sym(mid)
	slotIdx := aotMethodCacheWay0(oc, cmid)
	mc := &mrb.p.cache[slotIdx]
	if mc.c != oc || mc.mid != cmid {
		slotIdx++
		mc = &mrb.p.cache[slotIdx]
		if mc.c != oc || mc.mid != cmid {
			return -1, nil, 0, 0, C.mrb_method_t{}, false
		}
	}
	mflags := mc.m.flags
	// MRB_METHOD_FUNC_FL = 1<<24; check Data direct-ref C getters (data_ref_0..3)
	if (mflags & (1 << 24)) != 0 {
		if fcall || (mflags&((1<<25)|(1<<26))) == 0 {
			fn := *(*unsafe.Pointer)(unsafe.Pointer(&mc.m.as))
			if fn != nil {
				for i := 0; i < 4; i++ {
					if fn == mrb.aot.dataDirectRefPtrs[i] {
						return aotFastStructRef, nil, uintptr(i), slotIdx, mc.m, true
					}
				}
			}
		}
		return -1, nil, 0, slotIdx, mc.m, true
	}
	if !fcall && (mflags&((1<<25)|(1<<26))) != 0 { // private or protected
		return -1, nil, 0, slotIdx, mc.m, true
	}
	proc := *(**C.struct_RProc)(unsafe.Pointer(&mc.m.as))
	if proc == nil {
		return -1, nil, 0, slotIdx, mc.m, true
	}
	pFlags := (*(*uint32)(unsafe.Add(unsafe.Pointer(proc), 8))) >> 12
	// Need MRB_PROC_CFUNC_FL (128) | MRB_PROC_ENVSET (1024), and NOT MRB_PROC_ALIAS (8192)
	if (pFlags & (128 | 1024 | 8192)) != (128 | 1024) {
		return -1, nil, 0, slotIdx, mc.m, true
	}
	fnPtr := *(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(proc), 16))
	env := *(**C.struct_REnv)(unsafe.Add(unsafe.Pointer(proc), 32))
	if env == nil {
		return -1, nil, 0, slotIdx, mc.m, true
	}
	// env: [idx], or [idx, params, info, ORB_AOT_SIG_MARK] of a method with its
	// signature (go-aot.h _aot_define_method_body)
	envLen := ((*(*uint32)(unsafe.Add(unsafe.Pointer(env), 8))) >> 12) & 0xff
	if envLen != 1 && envLen != 4 {
		return -1, nil, 0, slotIdx, mc.m, true
	}
	stack := *(**C.mrb_value)(unsafe.Add(unsafe.Pointer(env), 16))
	if stack == nil {
		return -1, nil, 0, slotIdx, mc.m, true
	}
	if fnPtr == mrb.aot.envCallbackPtr {
		if stack.tt != C.MRB_TT_INTEGER {
			return -1, nil, 0, slotIdx, mc.m, true
		}
		if envLen == 4 {
			mark := (*C.mrb_value)(unsafe.Add(unsafe.Pointer(stack), 3*unsafe.Sizeof(*stack)))
			if mark.tt != C.MRB_TT_INTEGER || *(*int64)(unsafe.Pointer(mark)) != 0x4f524241 {
				return -1, nil, 0, slotIdx, mc.m, true
			}
		}
		return int(*(*int64)(unsafe.Pointer(stack))), mc.c0, 0, slotIdx, mc.m, true
	}
	if envLen == 1 {
		if (fnPtr == mrb.aot.attrReaderPtr || fnPtr == mrb.aot.attrWriterPtr) && stack.tt == C.MRB_TT_SYMBOL {
			sym := uintptr(*(*uint32)(unsafe.Pointer(stack)))
			code := aotFastAttrRead
			if fnPtr == mrb.aot.attrWriterPtr {
				code = aotFastAttrWrite
			}
			return code, nil, sym, slotIdx, mc.m, true
		}
		if (fnPtr == mrb.aot.structRefPtr || fnPtr == mrb.aot.dataRefPtr) && stack.tt == C.MRB_TT_INTEGER {
			slot := uintptr(*(*int64)(unsafe.Pointer(stack)))
			return aotFastStructRef, nil, slot, slotIdx, mc.m, true
		}
	}
	return -1, nil, 0, slotIdx, mc.m, true
}

// AotIC is a per-call-site monomorphic inline cache for zero-arg and one-arg
// calls to attr_reader, attr_writer, Struct/Data accessors, and compiled AOT
// methods.
type AotIC struct {
	cls    uintptr
	mcIdx  uint32
	kind   uint8 // 0=empty, 1=attr_reader, 2=attr_writer, 3=struct/data_ref, 4=compiled
	method C.mrb_method_t
	slot   int32
	shape  uintptr
	ivSym  MrbSym
	owner  uintptr
}

//go:noinline
func (mrb *MrbState) aotCall0ICCompiled(recv Value, slot int32) (Value, error, bool) {
	st := mrb.aot
	if st != nil && st.depth < aotMaxDirectDepth && !mrb.hasFastArgs && mrb.p.code_fetch_hook == nil {
		mi := st.methods[slot]
		if mi != nil && !mi.needCtx {
			ci := mrb.p.c.ci
			vis := ci.vis
			ci.vis = 0
			st.depth++
			res, err := mi.fn(mrb, recv, nil, nilValue, mi.cx, nil)
			st.depth--
			mrb.p.c.ci.vis = vis
			return res, err, true
		}
	}
	return nilValue, nil, false
}

// AotResolvePureAccessor resolves a 0-arg pure attr_reader or Struct/Data
// getter on recv once before a call-free loop, returning a direct pointer to
// its backing mrb_value slot. Caller must ensure no Ruby calls or GC happen
// while using the returned pointer.
func (mrb *MrbState) AotResolvePureAccessor(recv Value, mid MrbSym) (*Value, bool) {
	if (mrb.p.refined_mids[0] | mrb.p.refined_mids[1] | mrb.p.refined_mids[2] | mrb.p.refined_mids[3]) != 0 {
		return nil, false
	}
	idx, _, fastVal, _, _, hit := mrb.aotMethodIdxFromCache(recv, mid, false)
	if !hit {
		_, _ = mrb.aotLookupCompiledSlow(recv, mid, false)
		idx, _, fastVal, _, _, hit = mrb.aotMethodIdxFromCache(recv, mid, false)
	}
	if !hit {
		return nil, false
	}
	switch idx {
	case aotFastAttrRead:
		if recv.Type() != MrbTTObject || recv.Pointer() <= 65536 {
			return nil, false
		}
		up := aotObjPtr(recv)
		if (((*(*uint32)(unsafe.Add(up, 8))) >> 12) & (1 << 5)) == 0 {
			return nil, false
		}
		siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16))
		if siv == nil || siv.shape == nil {
			return nil, false
		}
		slot := mrb.aotShapeLookup(siv.shape, C.mrb_sym(fastVal))
		if slot < 0 {
			return nil, false
		}
		return (*Value)(unsafe.Add(unsafe.Pointer(&siv.values[0]), uintptr(slot)*unsafe.Sizeof(C.mrb_value{}))), true
	case aotFastStructRef:
		if recv.Type() != MrbTTStruct || recv.Pointer() <= 65536 {
			return nil, false
		}
		up := aotObjPtr(recv)
		flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
		var ptr *C.mrb_value
		var n int
		if embed := flags & 7; embed != 0 {
			ptr = (*C.mrb_value)(unsafe.Add(up, 16))
			n = int(embed - 1)
		} else {
			n = int(*(*int64)(unsafe.Add(up, 16)))
			ptr = *(**C.mrb_value)(unsafe.Add(up, 32))
		}
		i := int(fastVal)
		if i < 0 || i >= n || ptr == nil {
			return nil, false
		}
		return (*Value)(unsafe.Add(unsafe.Pointer(ptr), uintptr(i)*unsafe.Sizeof(C.mrb_value{}))), true
	}
	return nil, false
}

// AotCall0IC calls a 0-argument method using a monomorphic inline cache ic.
func (mrb *MrbState) AotCall0IC(self, recv Value, mid MrbSym, fcall bool, ic *AotIC) (Value, error) {
	mrb.aotStress()
	if ic != nil && ic.kind != 0 && (mrb.p.refined_mids[0]|mrb.p.refined_mids[1]|mrb.p.refined_mids[2]|mrb.p.refined_mids[3]) == 0 {
		oc := mrb.aotReceiverClass(recv)
		if oc != nil && uintptr(unsafe.Pointer(oc)) == ic.cls {
			mc := &mrb.p.cache[ic.mcIdx]
			if mc.c == oc && mc.mid == C.mrb_sym(mid) && mc.m == ic.method {
				switch ic.kind {
				case 1: // attr_reader
					if recv.Type() == MrbTTObject && recv.Pointer() > 65536 {
						up := aotObjPtr(recv)
						if (((*(*uint32)(unsafe.Add(up, 8))) >> 12) & (1 << 5)) != 0 {
							siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16))
							if siv != nil && uintptr(unsafe.Pointer(siv.shape)) == ic.shape {
								v := *(*C.mrb_value)(unsafe.Add(unsafe.Pointer(&siv.values[0]), uintptr(ic.slot)*unsafe.Sizeof(C.mrb_value{})))
								if v.tt == C.MRB_TT_UNDEF {
									return nilValue, nil
								}
								return mrb.aotProtect(Value{v}), nil
							}
						}
					}
				case 3: // Struct / Data reader
					if v, ok := mrb.aotStructSlotRef(recv, int(ic.slot)); ok {
						return v, nil
					}
				case 4: // compiled method
					if res, err, ok := mrb.aotCall0ICCompiled(recv, ic.slot); ok {
						return res, err
					}
				}
			}
		}
	}
	return mrb.aotCall0ICSlow(self, recv, mid, fcall, ic)
}

//go:noinline
func (mrb *MrbState) aotCall0ICSlow(self, recv Value, mid MrbSym, fcall bool, ic *AotIC) (Value, error) {
	if mrb.aotSendFamily(mid) {
		return mrb.aotDispatch(0, recv, mid, nil, nil, nilValue)
	}
	st := mrb.aot
	if st != nil && st.depth < aotMaxDirectDepth && !mrb.hasFastArgs {
		idx, owner, fastVal, mcIdx, mcMethod, hit := mrb.aotMethodIdxFromCache(recv, mid, fcall)
		if !hit {
			idx, owner = mrb.aotLookupCompiledSlow(recv, mid, fcall)
			idx2, owner2, fastVal2, mcIdx2, mcMethod2, hit2 := mrb.aotMethodIdxFromCache(recv, mid, fcall)
			if hit2 {
				idx, owner, fastVal, mcIdx, mcMethod, hit = idx2, owner2, fastVal2, mcIdx2, mcMethod2, true
			}
		}
		if hit && ic != nil {
			oc := mrb.aotReceiverClass(recv)
			switch {
			case idx == aotFastAttrRead:
				if recv.Type() == MrbTTObject && recv.Pointer() > 65536 {
					up := aotObjPtr(recv)
					if (((*(*uint32)(unsafe.Add(up, 8))) >> 12) & (1 << 5)) != 0 {
						if siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16)); siv != nil && siv.shape != nil {
							if slot := mrb.aotShapeLookup(siv.shape, C.mrb_sym(fastVal)); slot >= 0 {
								*ic = AotIC{
									cls:    uintptr(unsafe.Pointer(oc)),
									mcIdx:  mcIdx,
									kind:   1,
									method: mcMethod,
									slot:   int32(slot),
									shape:  uintptr(unsafe.Pointer(siv.shape)),
									ivSym:  MrbSym(fastVal),
								}
								v := *(*C.mrb_value)(unsafe.Add(unsafe.Pointer(&siv.values[0]), uintptr(slot)*unsafe.Sizeof(C.mrb_value{})))
								if v.tt == C.MRB_TT_UNDEF {
									return nilValue, nil
								}
								return mrb.aotProtect(Value{v}), nil
							}
						}
					}
				}
			case idx == aotFastStructRef:
				*ic = AotIC{
					cls:    uintptr(unsafe.Pointer(oc)),
					mcIdx:  mcIdx,
					kind:   3,
					method: mcMethod,
					slot:   int32(fastVal),
				}
			case idx >= 0 && idx < len(st.methods) && st.methods[idx] != nil:
				*ic = AotIC{
					cls:    uintptr(unsafe.Pointer(oc)),
					mcIdx:  mcIdx,
					kind:   4,
					method: mcMethod,
					slot:   int32(idx),
					owner:  uintptr(unsafe.Pointer(owner)),
				}
			}
		}
		switch {
		case idx == aotFastAttrRead:
			return mrb.AotIVGet(recv, MrbSym(fastVal)), nil
		case idx == aotFastStructRef:
			if v, ok := mrb.aotStructSlotRef(recv, int(fastVal)); ok {
				return v, nil
			}
		case idx >= 0 && idx < len(st.methods) && st.methods[idx] != nil:
			mi := st.methods[idx]
			if mrb.p.code_fetch_hook != nil {
				C._aot_poll(mrb.p)
				if mrb.p.exc != nil {
					return nilValue, mrb.aotExc()
				}
			}
			mcx := mi.cx
			if mi.needCtx {
				mcx = &AotCtx{Cref: mi.cx.Cref, Mid: mid, Super: unsafe.Pointer(owner)}
			}
			ci := mrb.p.c.ci
			vis := ci.vis
			ci.vis = 0
			st.depth++
			res, err := mi.fn(mrb, recv, nil, nilValue, mcx, nil)
			st.depth--
			mrb.p.c.ci.vis = vis
			return res, err
		}
	}
	var v Value
	if !fcall {
		v = Value{C._aot_funcall_public(mrb.p, self.v, recv.v, C.mrb_sym(mid), 0, nil, C.mrb_nil_value())}
	} else {
		v = Value{C._aot_funcall(mrb.p, recv.v, C.mrb_sym(mid), 0, nil, C.mrb_nil_value())}
	}
	if mrb.p.exc != nil {
		return nilValue, mrb.aotExc()
	}
	return v, nil
}

//go:noinline
func (mrb *MrbState) aotCall1ICCompiled(recv, a Value, slot int32) (Value, error, bool) {
	st := mrb.aot
	if st != nil && st.depth < aotMaxDirectDepth && !mrb.hasFastArgs && mrb.p.code_fetch_hook == nil {
		mi := st.methods[slot]
		if mi != nil && !mi.needCtx {
			argSlot := &st.argBuf[st.depth]
			argSlot[0] = a
			ci := mrb.p.c.ci
			vis := ci.vis
			ci.vis = 0
			st.depth++
			res, err := mi.fn(mrb, recv, argSlot[:1], nilValue, mi.cx, nil)
			st.depth--
			mrb.p.c.ci.vis = vis
			return res, err, true
		}
	}
	return nilValue, nil, false
}

// AotCall1IC calls a 1-argument method using a monomorphic inline cache ic.
func (mrb *MrbState) AotCall1IC(self, recv Value, mid MrbSym, a Value, fcall bool, ic *AotIC) (Value, error) {
	mrb.aotStress()
	if ic != nil && ic.kind != 0 && (mrb.p.refined_mids[0]|mrb.p.refined_mids[1]|mrb.p.refined_mids[2]|mrb.p.refined_mids[3]) == 0 {
		oc := mrb.aotReceiverClass(recv)
		if oc != nil && uintptr(unsafe.Pointer(oc)) == ic.cls {
			mc := &mrb.p.cache[ic.mcIdx]
			if mc.c == oc && mc.mid == C.mrb_sym(mid) && mc.m == ic.method {
				switch ic.kind {
				case 2: // attr_writer (immediate value)
					if a.v.tt <= C.MRB_TT_CPTR && recv.Type() == MrbTTObject && recv.Pointer() > 65536 {
						up := aotObjPtr(recv)
						word := *(*uint32)(unsafe.Add(up, 8))
						if (word&(1<<11)) == 0 && ((word>>12)&(1<<5)) != 0 {
							siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16))
							if siv != nil && uintptr(unsafe.Pointer(siv.shape)) == ic.shape {
								slot := (*C.mrb_value)(unsafe.Add(unsafe.Pointer(&siv.values[0]), uintptr(ic.slot)*unsafe.Sizeof(C.mrb_value{})))
								*slot = a.v
								return a, nil
							}
						}
					}
				case 4: // compiled method
					if res, err, ok := mrb.aotCall1ICCompiled(recv, a, ic.slot); ok {
						return res, err
					}
				}
			}
		}
	}
	return mrb.aotCall1ICSlow(self, recv, mid, a, fcall, ic)
}

//go:noinline
func (mrb *MrbState) aotCall1ICSlow(self, recv Value, mid MrbSym, a Value, fcall bool, ic *AotIC) (Value, error) {
	if mrb.aotSendFamily(mid) {
		return mrb.aotDispatch(0, recv, mid, nil, []Value{a}, nilValue)
	}
	st := mrb.aot
	if st != nil && st.depth < aotMaxDirectDepth && !mrb.hasFastArgs {
		idx, owner, fastVal, mcIdx, mcMethod, hit := mrb.aotMethodIdxFromCache(recv, mid, fcall)
		if !hit {
			idx, owner = mrb.aotLookupCompiledSlow(recv, mid, fcall)
			idx2, owner2, fastVal2, mcIdx2, mcMethod2, hit2 := mrb.aotMethodIdxFromCache(recv, mid, fcall)
			if hit2 {
				idx, owner, fastVal, mcIdx, mcMethod, hit = idx2, owner2, fastVal2, mcIdx2, mcMethod2, true
			}
		}
		if hit && ic != nil {
			oc := mrb.aotReceiverClass(recv)
			switch {
			case idx == aotFastAttrWrite:
				if a.v.tt <= C.MRB_TT_CPTR && recv.Type() == MrbTTObject && recv.Pointer() > 65536 {
					up := aotObjPtr(recv)
					word := *(*uint32)(unsafe.Add(up, 8))
					if (word&(1<<11)) == 0 && ((word>>12)&(1<<5)) != 0 {
						if siv := *(**C.mrb_shaped_iv)(unsafe.Add(up, 16)); siv != nil && siv.shape != nil {
							if slot := mrb.aotShapeLookup(siv.shape, C.mrb_sym(fastVal)); slot >= 0 {
								*ic = AotIC{
									cls:    uintptr(unsafe.Pointer(oc)),
									mcIdx:  mcIdx,
									kind:   2,
									method: mcMethod,
									slot:   int32(slot),
									shape:  uintptr(unsafe.Pointer(siv.shape)),
									ivSym:  MrbSym(fastVal),
								}
								sptr := (*C.mrb_value)(unsafe.Add(unsafe.Pointer(&siv.values[0]), uintptr(slot)*unsafe.Sizeof(C.mrb_value{})))
								*sptr = a.v
								return a, nil
							}
						}
					}
				}
			case idx >= 0 && idx < len(st.methods) && st.methods[idx] != nil:
				*ic = AotIC{
					cls:    uintptr(unsafe.Pointer(oc)),
					mcIdx:  mcIdx,
					kind:   4,
					method: mcMethod,
					slot:   int32(idx),
					owner:  uintptr(unsafe.Pointer(owner)),
				}
			}
		}
		switch {
		case idx == aotFastAttrWrite:
			if mrb.AotIVSetImmediate(recv, MrbSym(fastVal), a) {
				return a, nil
			}
		case idx >= 0 && idx < len(st.methods) && st.methods[idx] != nil:
			mi := st.methods[idx]
			if mrb.p.code_fetch_hook != nil {
				C._aot_poll(mrb.p)
				if mrb.p.exc != nil {
					return nilValue, mrb.aotExc()
				}
			}
			mcx := mi.cx
			if mi.needCtx {
				mcx = &AotCtx{Cref: mi.cx.Cref, Mid: mid, Super: unsafe.Pointer(owner)}
			}
			slot := &st.argBuf[st.depth]
			slot[0] = a
			ci := mrb.p.c.ci
			vis := ci.vis
			ci.vis = 0
			st.depth++
			res, err := mi.fn(mrb, recv, slot[:1], nilValue, mcx, nil)
			st.depth--
			mrb.p.c.ci.vis = vis
			return res, err
		}
	}
	return mrb.aotCall1Slow(self, recv, mid, a, fcall)
}

func aotConstHash(key, sym uintptr) uint32 {
	x := uint32(key ^ sym)
	x = ((x >> 16) ^ x) * 0x45d9f3b
	x = ((x >> 16) ^ x) * 0x45d9f3b
	x = (x >> 16) ^ x
	return x & (C.MRB_CONST_CACHE_SIZE - 1)
}

// AotConstPeek reads a constant without ever invoking const_missing or raising.
// Zero cgo on warm mrb->const_cache.
func (mrb *MrbState) AotConstPeek(cx *AotCtx, sym MrbSym) (Value, bool) {
	var cref *AotCref
	if cx != nil {
		cref = cx.Cref
	}
	if cref == nil || cref.Up == nil {
		baseCls := mrb.p.object_class
		if cref != nil && cref.Class.Pointer() > 65536 {
			baseCls = (*C.struct_RClass)(aotObjPtr(cref.Class))
		}
		key := (*C.struct_mrb_irep)(unsafe.Pointer(baseCls))
		h := aotConstHash(uintptr(unsafe.Pointer(key)), uintptr(sym))
		cc := &mrb.p.const_cache[h]
		if cc.irep == key && cc.sym == C.mrb_sym(sym) {
			return Value{cc.value}, true
		}
	}
	for c := cref; c != nil; c = c.Up {
		if C.mrb_const_defined_at(mrb.p, c.Class.v, C.mrb_sym(sym)) != false {
			v := mrb.AotConstGet(cx, sym)
			if mrb.p.exc != nil {
				mrb.p.exc = nil
				return nilValue, false
			}
			return v, true
		}
	}
	base := mrb.ObjectClass().Value()
	if cref != nil {
		base = cref.Class
	}
	if C._aot_const_defined_vm(mrb.p, base.v, C.mrb_sym(sym)) == false {
		return nilValue, false
	}
	v := mrb.AotConstGet(cx, sym)
	if mrb.p.exc != nil {
		mrb.p.exc = nil
		return nilValue, false
	}
	return v, true
}

// AotCoreConstOK verifies without side effects that constant sym resolves to
// the expected core module (e.g. Math) and core method id is unmodified.
func (mrb *MrbState) AotCoreConstOK(cx *AotCtx, sym MrbSym, id AotCoreID) bool {
	v, ok := mrb.AotConstPeek(cx, sym)
	if !ok {
		return false
	}
	return mrb.AotCoreOK(id, v)
}

// AotConstMethodOK verifies without side effects that constant clsSym resolves
// to a receiver whose method mid matches compiled method slot.
func (mrb *MrbState) AotConstMethodOK(cx *AotCtx, clsSym, mid MrbSym, slot int) bool {
	v, ok := mrb.AotConstPeek(cx, clsSym)
	if !ok {
		return false
	}
	return mrb.AotCompiledMethodOK(v, mid, slot)
}

// AotStrGetbyte implements String#getbyte(i) in pure Go without cgo.
// Returns (byteVal, isInt, ok): if !ok caller must fall back; if ok && !isInt result is nil.
func (mrb *MrbState) AotStrGetbyte(s Value, idx int64) (int64, bool, bool) {
	b, ok := mrb.AotStringBytes(s)
	if !ok {
		return 0, false, false
	}
	n := int64(len(b))
	if idx < 0 {
		idx += n
	}
	if idx < 0 || idx >= n {
		return 0, false, true
	}
	return int64(b[idx]), true, true
}

// AotStrOrd implements String#ord in pure Go without cgo for ASCII/BINARY and
// valid UTF-8 strings. Returns (0, false) on empty or invalid UTF-8 so Ruby
// raises ArgumentError via the fallback path.
func (mrb *MrbState) AotStrOrd(s Value) (int64, bool) {
	b, ok := mrb.AotStringBytes(s)
	if !ok || len(b) == 0 {
		return 0, false
	}
	if b[0] < 0x80 {
		return int64(b[0]), true
	}
	up := aotObjPtr(s)
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	enc := (flags >> 11) & 3 // MRB_STR_ENCODING_SHIFT = 11, 2 bits (1 = BINARY)
	if enc == 1 {
		return int64(b[0]), true
	}
	r, sz := utf8.DecodeRune(b)
	if r == utf8.RuneError && sz == 1 {
		return 0, false
	}
	return int64(r), true
}

func aotStrEncCompat(a, b Value) bool {
	fa := (*(*uint32)(unsafe.Add(aotObjPtr(a), 8))) >> 12
	fb := (*(*uint32)(unsafe.Add(aotObjPtr(b), 8))) >> 12
	ea := (fa >> 11) & 3
	eb := (fb >> 11) & 3
	if ea == eb {
		return true
	}
	// ASCII-7BIT coderange (1) is compatible with both UTF-8 and BINARY
	cra := (fa >> 9) & 3
	crb := (fb >> 9) & 3
	return cra == 1 || crb == 1
}

// AotStrStartWith implements single-argument String#start_with?(prefix) in pure Go.
func (mrb *MrbState) AotStrStartWith(s, prefix Value) (bool, bool) {
	bs, ok1 := mrb.AotStringBytes(s)
	bp, ok2 := mrb.AotStringBytes(prefix)
	if !ok1 || !ok2 || !aotStrEncCompat(s, prefix) {
		return false, false
	}
	return bytes.HasPrefix(bs, bp), true
}

// AotStrInclude implements String#include?(sub) in pure Go.
func (mrb *MrbState) AotStrInclude(s, sub Value) (bool, bool) {
	bs, ok1 := mrb.AotStringBytes(s)
	bp, ok2 := mrb.AotStringBytes(sub)
	if !ok1 || !ok2 || !aotStrEncCompat(s, sub) {
		return false, false
	}
	return bytes.Contains(bs, bp), true
}

// AotStrLen implements String#length / String#size in pure Go without cgo.
// O(1) for 7-bit ASCII and BINARY strings; utf8.RuneCount for valid UTF-8.
func (mrb *MrbState) AotStrLen(s Value) (int64, bool) {
	b, ok := mrb.AotStringBytes(s)
	if !ok {
		return 0, false
	}
	if len(b) == 0 {
		return 0, true
	}
	flags := (*(*uint32)(unsafe.Add(aotObjPtr(s), 8))) >> 12
	cr := (flags >> 9) & 3
	enc := (flags >> 11) & 3
	// A string of any encoding but UTF-8 is flagged BINARY and carries its
	// encoding as a tag the flags do not show (gem/core_ext 00_encoding.rb:
	// UTF-16 and UTF-32 are not one byte a character): String#length decides.
	if enc == 1 {
		return 0, false
	}
	if cr == 1 { // MRB_STR_CODERANGE_7BIT
		return int64(len(b)), true
	}
	if cr == 2 || (cr == 0 && utf8.Valid(b)) { // MRB_STR_CODERANGE_VALID
		return int64(utf8.RuneCount(b)), true
	}
	return 0, false
}

// AotStrConcatInPlace appends src onto dst in pure Go (zero cgo) when dst is
// a non-frozen, non-chilled, normal heap RString with spare capacity and src
// is a distinct String with compatible encoding. Returns false when growth,
// unsharing, frozen/chilled error, or Integer coercion requires the fallback.
func (mrb *MrbState) AotStrConcatInPlace(dst, src Value) bool {
	if dst.Type() != MrbTTString || src.Type() != MrbTTString {
		return false
	}
	if dst.Pointer() <= 65536 || src.Pointer() <= 65536 || dst.Pointer() == src.Pointer() {
		return false
	}
	up := aotObjPtr(dst)
	word := *(*uint32)(unsafe.Add(up, 8))
	// Reject frozen (bit 11)
	if (word & (1 << 11)) != 0 {
		return false
	}
	flags := word >> 12
	// Require MRB_STR_NORMAL (low 4 bits == 0) and not ORUBY_STRING_CHILLED (bit 18)
	if (flags&15) != 0 || (flags&(1<<18)) != 0 {
		return false
	}
	sb, ok := mrb.AotStringBytes(src)
	if !ok || !aotStrEncCompat(dst, src) {
		return false
	}
	if len(sb) == 0 {
		return true
	}
	dlen := *(*int64)(unsafe.Add(up, 16))
	dcapa := *(*int64)(unsafe.Add(up, 24))
	newLen := dlen + int64(len(sb))
	if newLen > dcapa {
		return false
	}
	dp := *(**byte)(unsafe.Add(up, 32))
	if dp == nil {
		return false
	}
	dstSlice := unsafe.Slice(dp, int(dcapa)+1)
	copy(dstSlice[dlen:], sb)
	dstSlice[newLen] = 0
	*(*int64)(unsafe.Add(up, 16)) = newLen
	// Update coderange conservatively: keep 7BIT only if both are 7BIT, else clear to UNKNOWN
	sflags := (*(*uint32)(unsafe.Add(aotObjPtr(src), 8))) >> 12
	if ((flags>>9)&3) != 1 || ((sflags>>9)&3) != 1 {
		*(*uint32)(unsafe.Add(up, 8)) = word &^ (3 << (12 + 9))
	}
	return true
}

// AotStrASCIIOp runs strip (0), downcase (1), or upcase (2) in 1 cgo call
// without VM frame/visibility overhead when s is 7-bit ASCII or BINARY.
func (mrb *MrbState) AotStrASCIIOp(s Value, op int) (Value, bool) {
	if s.Type() != MrbTTString || s.Pointer() <= 65536 {
		return nilValue, false
	}
	flags := (*(*uint32)(unsafe.Add(aotObjPtr(s), 8))) >> 12
	cr := (flags >> 9) & 3
	enc := (flags >> 11) & 3
	if cr != 1 && enc != 1 {
		// Check ASCII quickly for small/unknown coderange strings
		if cr != 0 {
			return nilValue, false
		}
		b, ok := mrb.AotStringBytes(s)
		if !ok {
			return nilValue, false
		}
		for _, ch := range b {
			if ch >= 0x80 {
				return nilValue, false
			}
		}
	}
	return Value{C._aot_str_ascii_op(mrb.p, s.v, C.int(op))}, true
}

// AotStrGsubPlain runs plain-string gsub(pat, rep) in 1 cgo call when pat is a
// non-empty String, rep is a String with no backslash escapes, and encodings match.
func (mrb *MrbState) AotStrGsubPlain(s, pat, rep Value) (Value, bool) {
	if s.Type() != MrbTTString || pat.Type() != MrbTTString || rep.Type() != MrbTTString {
		return nilValue, false
	}
	pb, ok1 := mrb.AotStringBytes(pat)
	rb, ok2 := mrb.AotStringBytes(rep)
	if !ok1 || !ok2 || len(pb) == 0 || bytes.IndexByte(rb, '\\') >= 0 {
		return nilValue, false
	}
	if !aotStrEncCompat(s, pat) || !aotStrEncCompat(s, rep) {
		return nilValue, false
	}
	return Value{C._aot_str_gsub_plain(mrb.p, s.v, pat.v, rep.v)}, true
}

// AotStrSplitPlain runs 1-argument split(sep) in 1 cgo call when sep is a
// non-empty, non-single-space String with compatible encoding.
func (mrb *MrbState) AotStrSplitPlain(s, sep Value) (Value, bool) {
	if s.Type() != MrbTTString || sep.Type() != MrbTTString {
		return nilValue, false
	}
	sb, ok := mrb.AotStringBytes(sep)
	if !ok || len(sb) == 0 || (len(sb) == 1 && sb[0] == ' ') {
		return nilValue, false
	}
	if !aotStrEncCompat(s, sep) {
		return nilValue, false
	}
	return Value{C._aot_str_split_plain(mrb.p, s.v, sep.v)}, true
}
