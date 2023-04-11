package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

// Ptr returns RHashPtr from oruby hash value
func (h RHash) Ptr() RHashPtr { return MrbHashPtr(Value{h.v}) }

// Set sets a key and value to hash
func (h RHash) Set(key, val MrbValue) { C.mrb_hash_set(h.mrb.p, h.v, key.Value().v, val.Value().v) }

// SetI sets a keys and value interfaces to hash
func (h RHash) SetI(key, val interface{}) {
	C.mrb_hash_set(h.mrb.p, h.v, h.mrb.Value(key).v, h.mrb.Value(val).v)
}

// Get gets a value from a key. If the key is not found, the default of the hash is used
func (h RHash) Get(key MrbValue) Value {
	return Value{C.mrb_hash_get(h.mrb.p, h.v, key.Value().v)}
}

// Fetch gets a value from a key. If the key is not found, the default parameter is used
func (h RHash) Fetch(key, def MrbValue) Value {
	return Value{C.mrb_hash_fetch(h.mrb.p, h.v, key.Value().v, def.Value().v)}
}

// DeleteKey deletes hash key and value pair
func (h RHash) DeleteKey(key MrbValue) Value {
	return Value{C.mrb_hash_delete_key(h.mrb.p, h.v, key.Value().v)}
}

// Keys gets an array of keys
func (h RHash) Keys() RArray {
	return ary(C.mrb_hash_keys(h.mrb.p, h.v), h.mrb)
}

// KeyP Check if the hash has the key.
func (h RHash) KeyP(key MrbValue) bool {
	return C.mrb_hash_key_p(h.mrb.p, h.v, key.Value().v) != false
}

// EmptyP check if the hash is empty
func (h RHash) EmptyP() bool {
	return C.mrb_hash_empty_p(h.mrb.p, h.v) != false
}

// Values returns values as array
func (h RHash) Values() RArray {
	return RArray{RObject{C.mrb_hash_values(h.mrb.p, h.v), h.mrb}}
}

// Clear clears the hash
func (h RHash) Clear() RHash {
	h.v = C.mrb_hash_clear(h.mrb.p, h.v)
	return h
}

// Size get hash size
func (h RHash) Size(hash MrbValue) int {
	return int(C.mrb_hash_size(h.mrb.p, h.v))
}

// Dup copies the hash
func (h RHash) Dup() RHash {
	h.v = C.mrb_hash_dup(h.mrb.p, h.v)
	return h
}

// Merge merges two hashes. The first hash will be modified by the second hash
func (h RHash) Merge(hash2 MrbValue) {
	C.mrb_hash_merge(h.mrb.p, h.v, hash2.Value().v)
	return
}

// IfNone get ifnone value from hash
func (h RHash) IfNone() Value {
	return h.mrb.IVGet(h, h.mrb.Intern("ifnone"))
}

// ProcDefault alias for IfNone
func (h RHash) ProcDefault() Value {
	return h.mrb.RHashIfNone(h)
}

// ForEach wakls the hash item pairs
func (h RHash) ForEach(f MrbHashForeachFuncT) {
	p := unsafe.Pointer(h.Ptr().p)
	h.mrb.setHook(p, f)

	C.mrb_hash_foreach(h.mrb.p, h.Ptr().p, (*C.mrb_hash_foreach_func)(C.set_hash_callback), p)

	h.mrb.setHook(p, nil)
}
