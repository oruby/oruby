package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

// RArray struct
type RArray struct{ RValue }

// RArrayPtr contains pointer to internal RArray C API struct
type RArrayPtr struct{ p *C.struct_RArray }

func (a RArray) p() *C.struct_RArray { return (*C.struct_RArray)(C._mrb_ptr(a.v)) }

// MrbSharedArray struct
type MrbSharedArray struct{ p *C.struct_mrb_shared_array }

// MrbAryValue value from RArray
func MrbAryValue(ary RArray) Value { return ary.Value() }

func rarray(v Value) *C.struct_RArray { return (*C.struct_RArray)(C._mrb_ptr(v.v)) }

// RArraySlice returns a direct slice view of a C RArray without CGO calls.
func RArraySlice(v Value) []Value {
	if v.Pointer() <= 65536 {
		return nil
	}
	up := v.unsafePtr()
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	var n int
	var ptr *Value
	if (flags & 7) != 0 {
		n = int((flags & 7) - 1)
		ptr = (*Value)(unsafe.Add(up, 16))
	} else {
		n = int(*(*int64)(unsafe.Add(up, 16)))
		ptr = *(**Value)(unsafe.Add(up, 32))
	}
	if n <= 0 || ptr == nil {
		return nil
	}
	return unsafe.Slice(ptr, n)
}

// RArrayLen returns len of array value
func RArrayLen(a MrbValue) int {
	return len(RArraySlice(a.Value()))
}

// RArrayCapa returns capacity of array
func RArrayCapa(a MrbValue) int { return int(C._RARRAY_CAPA(rarray(a.Value()))) }

// Len returns len of array as method
func (a RArray) Len() int { return len(RArraySlice(a.Value())) }

// Capa returns len of array as method
func (a RArray) Capa() int { return int(C._RARRAY_CAPA(a.p())) }

// Item returns value item at index, or Nil if index is invalid
func (a RArray) Item(index int) Value {
	s := RArraySlice(a.Value())
	l := len(s)

	/* range check */
	if index < 0 {
		index += l
	}
	if index < 0 || index >= l {
		return nilValue
	}

	return s[index]
}

// ItemDef returns value item at index, or default value if arg is nil
func (a RArray) ItemDef(index int, def MrbValue) Value {
	v := a.Item(index)
	if MrbNilP(v) {
		return def.Value()
	}
	return v
}

// ItemDefFunc returns value item at index, or default value from func f
// if arg is nil
func (a RArray) ItemDefFunc(index int, defF func() MrbValue) Value {
	v := a.Item(index)
	if MrbNilP(v) {
		return defF().Value()
	}
	return v
}

// ItemDefInt returns value as int at index, or default int value if arg is nil
func (a RArray) ItemDefInt(index, def int) int {
	v := a.Item(index)
	if MrbNilP(v) {
		return def
	}
	return v.Int()
}

// ItemDefBool returns value as bool at index, or default bool value if arg is nil
func (a RArray) ItemDefBool(index int, def bool) bool {
	v := a.Item(index)
	if MrbNilP(v) {
		return def
	}
	return v.Bool()
}

// Slice returns Go slice made of Value items
func (a RArray) Slice() []Value {
	ret := make([]Value, a.Len())
	for i := 0; i < a.Len(); i++ {
		ret[i] = a.Item(i)
	}
	return ret
}

// SliceIntf returns Go slice made of interaface{} items
func (a RArray) SliceIntf() []interface{} {
	ret := make([]interface{}, a.Len())
	for i := 0; i < a.Len(); i++ {
		ret[i] = a.Item(i)
	}
	return ret
}

// MrbAryShared const
const MrbAryShared = 256

// Shared returnes true if internal RArray is shared
func (a RArray) Shared() bool { return a.Flags()|MrbAryShared != 0 }

// SetShared sets internal RArray as shared
func (a RArray) SetShared() { C._RARRAY_SET_SHARED(a.p()) }

// UnsetShared unsets internal RArray as shared
func (a RArray) UnsetShared() { C._RARRAY_UNSET_SHARED(a.p()) }

// AryModify modify array
func (mrb *MrbState) AryModify(a RArray) { C.mrb_ary_modify(mrb.p, a.p()) }

// AryNewCapa Set new array with capacity capa
func (mrb *MrbState) AryNewCapa(capa int) RArray {
	return RArray{RValue{
		C.mrb_ary_new_capa(mrb.p, C.mrb_int(capa)),
		mrb,
	}}
}

// AryNew Initializes a new array
func (mrb *MrbState) AryNew() RArray {
	return ary(C.mrb_ary_new(mrb.p), mrb)
}

// AryNewFromValues Initializes a new array with initial values
func (mrb *MrbState) AryNewFromValues(args ...Value) RArray {
	return ary(mrb.Value(args).v, mrb)
	// pure C.mrb_ary_new_from_values() is never called
}

// AryNewFromMrbValues Initializes a new array with initial values
func (mrb *MrbState) AryNewFromMrbValues(args ...MrbValue) RArray {
	return ary(mrb.Value(args).v, mrb)
	// pure C.mrb_ary_new_from_values() is never called
}

// AssocNew Initializes a new array with two initial values
func (mrb *MrbState) AssocNew(car, cdr MrbValue) RArray {
	return ary(C.mrb_assoc_new(mrb.p, car.Value().v, cdr.Value().v), mrb)
}

// AryConcat Concatenate two arrays. The target array will be modified
func (mrb *MrbState) AryConcat(self, other MrbValue) {
	C.mrb_ary_concat(mrb.p, self.Value().v, other.Value().v)
}

// ArySplat Create an array from the input. Tries calling to_a on the value.
// If value does not respond to that, it creates a new array with just this value.
func (mrb *MrbState) ArySplat(value MrbValue) Value {
	return Value{C.mrb_ary_splat(mrb.p, value.Value().v)}
}

// AryPush pushes value to array
func (mrb *MrbState) AryPush(ary, val MrbValue) { C.mrb_ary_push(mrb.p, ary.Value().v, val.Value().v) }

// AryPop pops the last element from the array
func (mrb *MrbState) AryPop(ary MrbValue) Value { return Value{C.mrb_ary_pop(mrb.p, ary.Value().v)} }

// AryRef returns a reference to an element of the array on the given index
func (mrb *MrbState) AryRef(ary MrbValue, n int) Value {
	v := ary.Value()
	if v.Type() == MrbTTArray {
		s := RArraySlice(v)
		if n < 0 {
			n += len(s)
		}
		if n < 0 || n >= len(s) {
			return nilValue
		}
		return s[n]
	}
	return Value{C.mrb_ary_entry(v.v, C.mrb_int(n))}
}

// ArySet Sets a value on an array at the given index
func (mrb *MrbState) ArySet(ary MrbValue, n int, val MrbValue) {
	C.mrb_ary_set(mrb.p, ary.Value().v, C.mrb_int(n), val.Value().v)
}

// AryReplace Replace the array with another array
func (mrb *MrbState) AryReplace(a, b MrbValue) { C.mrb_ary_replace(mrb.p, a.Value().v, b.Value().v) }

// EnsureArrayType checks array value
func (mrb *MrbState) EnsureArrayType(v MrbValue) (RArray, error) {
	r, err := mrb.protectedOp(C.GO_MRB_OP_ENSURE_ARRAY, v)
	if err != nil {
		return RArray{}, err
	}
	return ary(r.v, mrb), nil
}

// CheckArrayType checks array value
func (mrb *MrbState) CheckArrayType(ary MrbValue) Value {
	return Value{C.mrb_check_array_type(mrb.p, ary.Value().v)}
}

// AryUnshift unshift an element into the array
func (mrb *MrbState) AryUnshift(ary, item MrbValue) Value {
	return Value{C.mrb_ary_unshift(mrb.p, ary.Value().v, item.Value().v)}
}

// AryEntry get nth element in the array
func (mrb *MrbState) AryEntry(ary MrbValue, offset int) Value {
	return Value{C.mrb_ary_entry(ary.Value().v, C.mrb_int(offset))}
}

// ArySplice replace subsequence of an array
func (mrb *MrbState) ArySplice(ary MrbValue, head, length int, rpl MrbValue) Value {
	return Value{C.mrb_ary_splice(mrb.p, ary.Value().v, C.mrb_int(head), C.mrb_int(length), rpl.Value().v)}
}

// AryShift shifts the first element from the array
func (mrb *MrbState) AryShift(ary MrbValue) Value {
	return Value{C.mrb_ary_shift(mrb.p, ary.Value().v)}
}

// AryClear removes all elements from the array
func (mrb *MrbState) AryClear(ary MrbValue) Value {
	return Value{C.mrb_ary_clear(mrb.p, ary.Value().v)}
}

// AryJoin join array items to string using separator sep
func (mrb *MrbState) AryJoin(ary, sep MrbValue) Value {
	return Value{C.mrb_ary_join(mrb.p, ary.Value().v, sep.Value().v)}
}

// AryResize update the capacity of the array
func (mrb *MrbState) AryResize(ary MrbValue, newLen int) Value {
	return Value{C.mrb_ary_resize(mrb.p, ary.Value().v, C.mrb_int(newLen))}
}

// AryDup returns a shallow copy of ary (mrb_ary_dup)
func (mrb *MrbState) AryDup(a MrbValue) (RArray, error) {
	r, err := mrb.protectedOp(C.GO_MRB_OP_ARY_DUP, a)
	if err != nil {
		return RArray{}, err
	}
	return ary(r.v, mrb), nil
}

// AryUnshiftValues inserts vals at the front of ary, in order
// (mrb_ary_unshift_values); FrozenError for a frozen array
func (mrb *MrbState) AryUnshiftValues(a MrbValue, vals ...MrbValue) error {
	if len(vals) == 0 {
		return nil
	}
	argv := make([]C.mrb_value, len(vals))
	for i, v := range vals {
		argv[i] = v.Value().v
	}
	var result C.mrb_value
	status := C._mrb_ary_unshift_values(mrb.p, a.Value().v, C.mrb_int(len(argv)), &argv[0], &result)
	return mrb.protectedOutcomeError(Value{result}, status)
}
