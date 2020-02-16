package oruby

// #include "go-mrb.h"
import "C"

// RArray struct
type RArray struct{ RObject }

// RArrayPtr contains pointer to internal RArray C API struct
type RArrayPtr struct{ p *C.struct_RArray }

func (a RArray) p() *C.struct_RArray { return (*C.struct_RArray)(C._mrb_ptr(a.v)) }

// MrbSharedArray struct
type MrbSharedArray struct{ p *C.struct_mrb_shared_array }

// MrbAryValue value from RArray
func MrbAryValue(ary RArray) Value { return ary.Value() }

func rarray(v Value) *C.struct_RArray { return (*C.struct_RArray)(C._mrb_ptr(v.v)) }

// RArrayLen returns len of array value
func RArrayLen(a MrbValue) int {
	return int(C._RARRAY_LEN(rarray(a.Value())))
}

// RArrayCapa returns capacity of array
func RArrayCapa(a MrbValue) int { return int(C._RARRAY_CAPA(rarray(a.Value()))) }

// Len returns len of array as method
func (a RArray) Len() int { return int(C._RARRAY_LEN(a.p())) }

// Capa returns len of array as method
func (a RArray) Capa() int { return int(C._RARRAY_CAPA(a.p())) }

// Item returns value item at index, or Nil if index is invalid
func (a RArray) Item(index int) Value {
	l := a.Len()

	/* range check */
	if index < 0 {
		index += l
	}
	if index < 0 || index >= l {
		return nilValue
	}

	return Value{C._ARY_ITEM(a.p(), C.mrb_int(index))}
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

// AryModify modify array
func (mrb *MrbState) AryModify(a RArray) { C.mrb_ary_modify(mrb.p, a.p()) }

// AryNewCapa Set new array with capacity capa
func (mrb *MrbState) AryNewCapa(capa int) RArray {
	return RArray{RObject{
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

// AryNewFromValues Initializes a new array with initial values
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
	return Value{C.mrb_ary_ref(mrb.p, ary.Value().v, C.mrb_int(n))}
}

// ArySet Sets a value on an array at the given index
func (mrb *MrbState) ArySet(ary MrbValue, n int, val MrbValue) {
	C.mrb_ary_set(mrb.p, ary.Value().v, C.mrb_int(n), val.Value().v)
}

// AryReplace Replace the array with another array
func (mrb *MrbState) AryReplace(a, b MrbValue) { C.mrb_ary_replace(mrb.p, a.Value().v, b.Value().v) }

// EnsureArrayType checks array value
func (mrb *MrbState) EnsureArrayType(v MrbValue) RArray {
	if !v.Value().IsArray() {
		panic(mrb.TypeName(v) + " cannot be converted to Array")
	}

	return ary(v.Value().v,	mrb)
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
func (mrb *MrbState) AryShift(ary MrbValue) Value { return Value{C.mrb_ary_shift(mrb.p, ary.Value().v)} }

// AryClear removes all elements from the array
func (mrb *MrbState) AryClear(ary MrbValue) Value { return Value{C.mrb_ary_clear(mrb.p, ary.Value().v)} }

// AryJoin join array items to string using separator sep
func (mrb *MrbState) AryJoin(ary, sep MrbValue) Value {
	return Value{C.mrb_ary_join(mrb.p, ary.Value().v, sep.Value().v)}
}

// AryResize update the capacity of the array
func (mrb *MrbState) AryResize(ary MrbValue, newLen int) Value {
	return Value{C.mrb_ary_resize(mrb.p, ary.Value().v, C.mrb_int(newLen))}
}
