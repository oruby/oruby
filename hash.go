package oruby

// #include "go-mrb.h"
import "C"
import (
	"unsafe"
)

// RHashPtr struct encapsulates C API RHash
type RHashPtr struct{ p *C.struct_RHash }

// RHash is oruby object representing ruby hash
type RHash struct{ RObject }

// MrbHashPtr returns RHash from oruby value
func MrbHashPtr(v Value) RHashPtr { return RHashPtr{(*C.struct_RHash)(C._mrb_ptr(v.v))} }

// MrbHashValue converts RHash to oruby value
func MrbHashValue(hash RHash) Value { return hash.Value() }

// HashNewCapa new hash with capacity capa
func (mrb *MrbState) HashNewCapa(capa int) RHash {
	return RHash{RObject{
		C.mrb_hash_new_capa(mrb.p, C.mrb_int(capa)),
		mrb,
	}}
}

// EnsureHashType new hash with capacity capa
func (mrb *MrbState) EnsureHashType(hash MrbValue) RHash {
	if !hash.Value().IsHash() {
		panic(mrb.TypeName(hash) + " cannot be converted to Hash")
	}

	return RHash{RObject{
		hash.Value().v,
		mrb,
	}}
}

// CheckHashType new hash with capacity capa
func (mrb *MrbState) CheckHashType(hash MrbValue) Value {
	return Value{C.mrb_check_hash_type(mrb.p, hash.Value().v)}
}

// HashNew initializes a new hash
func (mrb *MrbState) HashNew() RHash {
	return RHash{RObject{
		C.mrb_hash_new(mrb.p),
		mrb,
	}}
}

// HashSet sets a keys and values to hashes
func (mrb *MrbState) HashSet(hash, key, val MrbValue) {
	C.mrb_hash_set(mrb.p, hash.Value().v, key.Value().v, val.Value().v)
}

// HashGet gets a value from a key. If the key is not found, the default of the hash is used
func (mrb *MrbState) HashGet(hash, key MrbValue) Value {
	return Value{C.mrb_hash_get(mrb.p, hash.Value().v, key.Value().v)}
}

// HashFetch gets a value from a key. If the key is not found, the default parameter is used
func (mrb *MrbState) HashFetch(hash, key, def MrbValue) Value {
	return Value{C.mrb_hash_fetch(mrb.p, hash.Value().v, key.Value().v, def.Value().v)}
}

// HashDeleteKey deletes hash key and value pair
func (mrb *MrbState) HashDeleteKey(hash, key MrbValue) Value {
	return Value{C.mrb_hash_delete_key(mrb.p, hash.Value().v, key.Value().v)}
}

// HashKeys gets an array of keys
func (mrb *MrbState) HashKeys(hash MrbValue) RArray {
	return ary(C.mrb_hash_keys(mrb.p, hash.Value().v), mrb)
}

// HashKeyP Check if the hash has the key.
func (mrb *MrbState) HashKeyP(hash, key MrbValue) bool {
	return C.mrb_hash_key_p(mrb.p, hash.Value().v, key.Value().v) != C.mrb_bool(0)
}

// HashEmptyP check if the hash is empty
func (mrb *MrbState) HashEmptyP(hash MrbValue) bool {
	return C.mrb_hash_empty_p(mrb.p, hash.Value().v) != C.mrb_bool(0)
}

// HashClear clears the hash
func (mrb *MrbState) HashClear(hash MrbValue) Value {
	return Value{C.mrb_hash_clear(mrb.p, hash.Value().v)}
}

// HashSize get hash size
func (mrb *MrbState) HashSize(hash MrbValue) int {
	return int(C.mrb_hash_size(mrb.p, hash.Value().v))
}

// HashDup copies the hash
func (mrb *MrbState) HashDup(hash MrbValue) Value {
	return Value{C.mrb_hash_dup(mrb.p, hash.Value().v)}
}

// HashMerge merges two hashes. The first hash will be modified by the second hash
func (mrb *MrbState) HashMerge(hash1, hash2 MrbValue) {
	C.mrb_hash_merge(mrb.p, hash1.Value().v, hash2.Value().v)
	return
}

// RHashTbl allocates st_table if not available.
func RHashTbl(h MrbValue) uintptr {
	return uintptr(unsafe.Pointer((*C.struct_RHash)(C._mrb_ptr(h.Value().v)).ht))
}

// RHashIfNone get ifnone value from hash
func (mrb *MrbState) RHashIfNone(h MrbValue) Value {
	return mrb.IVGet(h, mrb.Intern("ifnone"))
}

// RhashProcDefault alias for RHashIfNone
func (mrb *MrbState) RhashProcDefault(h MrbValue) Value {
	return mrb.RHashIfNone(h)
}

// Hash defaults
const (
	MrbHashDefault     = 1
	MrbHashProcDefault = 2
)

// MrbRHashProcDefaultP checks hash procdefault
func MrbRHashProcDefaultP(h MrbValue) bool { return (C._MRB_RHASH_PROCDEFAULT_P(h.Value().v) != 0) }

// MrbHashForeachFuncT is hash foreach callback func. Return non zero to break the loop
type MrbHashForeachFuncT = func(key, val Value) int

//export go_hash_callback
func go_hash_callback(cmrb *C.mrb_state, key, val C.mrb_value, data unsafe.Pointer) C.int {
	mrb := getMrbState(cmrb)

	f, ok := mrb.getHook(data).(MrbHashForeachFuncT)
	if !ok {
		return -1
	}

	return C.int(f(Value{key}, Value{val}))
}

// HashValueForEach wakls the value if it is hash with item pairs
// If the value is not hash, it is skipped
func (mrb *MrbState) HashValueForEach(hash MrbValue, f MrbHashForeachFuncT) {
	if !hash.Value().IsHash() {
		return
	}
	mrb.HashForEach(RHash{RObject{hash.Value().v, mrb}}, f)
}

// HashForEach wakls the hash item pairs
func (mrb *MrbState) HashForEach(hash RHash, f MrbHashForeachFuncT) {
	p := unsafe.Pointer(hash.Ptr().p)

	mrb.setHook(p, f)
	C.mrb_hash_foreach(mrb.p, hash.Ptr().p, (*C.mrb_hash_foreach_func)(C.set_hash_callback), p)
	mrb.setHook(p, nil)
}
