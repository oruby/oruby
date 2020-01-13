package oruby

// #include "go-mrb.h"
import "C"

func ary(v C.mrb_value, mrb *MrbState) RArray {
	return RArray{RObject{v, mrb}}
}

// Ptr returns RArrayPtr from RArray
func (a RArray) Ptr() RArrayPtr { return RArrayPtr{(*C.struct_RArray)(C._mrb_ptr(a.v))} }

// Modify modify array
func (a RArray) Modify() { C.mrb_ary_modify(a.mrb.p, a.Ptr().p) }

// Concat Concatenate two arrays. The target array will be modified
func (a RArray) Concat(other MrbValue) { C.mrb_ary_concat(a.mrb.p, a.v, other.Value().v) }

// Splat create an array from the input. Tries calling to_a on the value.
// If value does not respond to that, it creates a new array with just this value.
func (a RArray) Splat() RArray {
	return ary(C.mrb_ary_splat(a.mrb.p, a.v), a.mrb)
}

// Push pushes value to array
func (a RArray) Push(val MrbValue) { C.mrb_ary_push(a.mrb.p, a.v, val.Value().v) }

// PushString pushes string to oruby array
func (a RArray) PushString(s string) { C.mrb_ary_push(a.mrb.p, a.v, a.mrb.StrNewStatic(s).v) }

// PushInt pushes int to oruby array
func (a RArray) PushInt(val int) { C.mrb_ary_push(a.mrb.p, a.v, MrbFixnumValue(val).v) }

// PushFloat64 pushes int to oruby array
func (a RArray) PushFloat64(f float64) { C.mrb_ary_push(a.mrb.p, a.v, a.mrb.FloatValue(f).v) }

// Pop pops the last element from the array
func (a RArray) Pop() Value { return Value{C.mrb_ary_pop(a.mrb.p, a.v)} }

// Ref returns a reference to an element of the array on the given index
func (a RArray) Ref(n int) Value {
	return Value{C.mrb_ary_ref(a.mrb.p, a.v, C.mrb_int(n))}
}

// Get returns a reference to an element of the array on the given index
func (a RArray) Get(n int) Value { return a.Get(n) }

// Set Sets a value on an array at the given index
func (a RArray) Set(n int, val MrbValue) {
	C.mrb_ary_set(a.mrb.p, a.v, C.mrb_int(n), val.Value().v)
}

// Replace the array with another array
func (a RArray) Replace(b MrbValue) { C.mrb_ary_replace(a.mrb.p, a.v, b.Value().v) }

// Unshift an element into the array
func (a RArray) Unshift(item MrbValue) Value {
	return Value{C.mrb_ary_unshift(a.mrb.p, a.v, item.Value().v)}
}

// Entry get nth element in the array
func (a RArray) Entry(offset int) Value {
	return Value{C.mrb_ary_entry(a.v, C.mrb_int(offset))}
}

// Splice replace subsequence of an array
func (a RArray) Splice(head, length int, rpl MrbValue) RArray {
	return RArray{RObject{
		C.mrb_ary_splice(a.mrb.p, a.v, C.mrb_int(head), C.mrb_int(length), rpl.Value().v),
		a.mrb,
	}}
}

// Shift shifts the first element from the array
func (a RArray) Shift() Value {
	return Value{C.mrb_ary_shift(a.mrb.p, a.v)}
}

// Clear removes all elements from the array
func (a RArray) Clear() RArray {
	a.v = C.mrb_ary_clear(a.mrb.p, a.v)
	return a
}

// Join join array items to string using separator sep
func (a RArray) Join(separator string) string {
	return a.mrb.String(Value{C.mrb_ary_join(a.mrb.p, a.v, a.mrb.StringValue(separator).v)})
}

// Resize update the capacity of the array
func (a RArray) Resize(newLen int) RArray {
	a.v = C.mrb_ary_resize(a.mrb.p, a.v, C.mrb_int(newLen))
	return a
}
