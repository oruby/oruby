package oruby

// #include "go-mrb.h"
import "C"
import (
	"unsafe"
)

// RHashPtr struct encapsulates C API RHash
type RHashPtr struct{ p *C.struct_RHash }

// RHash is oruby object representing ruby hash
type RHash struct{ RValue }

// MrbHashPtr returns RHash from oruby value
func MrbHashPtr(v Value) RHashPtr { return RHashPtr{(*C.struct_RHash)(C._mrb_ptr(v.v))} }

// MrbHashValue converts RHash to oruby value
func MrbHashValue(hash RHash) Value { return hash.Value() }

// HashNewCapa new hash with capacity capa
func (mrb *MrbState) HashNewCapa(capa int) RHash {
	return RHash{RValue{
		C.mrb_hash_new_capa(mrb.p, C.mrb_int(capa)),
		mrb,
	}}
}

// EnsureHashType returns hash as a Hash: one, or what its to_hash returns;
// anything else is a TypeError (mrb_ensure_hash_type)
func (mrb *MrbState) EnsureHashType(hash MrbValue) (RHash, error) {
	v := hash.Value()
	if !v.IsHash() && mrb.RespondTo(v, Sym_to_hash) {
		h, err := mrb.Funcall(v, Sym_to_hash)
		if err != nil {
			return RHash{}, err
		}
		v = h
	}
	r, err := mrb.protectedOp(C.GO_MRB_OP_ENSURE_HASH, v)
	if err != nil {
		return RHash{}, err
	}
	return RHash{RValue{r.v, mrb}}, nil
}

// CheckHashType new hash with capacity capa
func (mrb *MrbState) CheckHashType(hash MrbValue) Value {
	return Value{C.mrb_check_hash_type(mrb.p, hash.Value().v)}
}

// HashNew initializes a new hash
func (mrb *MrbState) HashNew() RHash {
	return RHash{RValue{
		C.mrb_hash_new(mrb.p),
		mrb,
	}}
}

// RHashEntries returns the raw (key, val) entry slice for a C RHash without CGO calls.
// Deleted entries have entry[0].Type() == MrbTTUndef.
func RHashEntries(v Value) [][2]Value {
	if v.Pointer() <= 65536 {
		return nil
	}
	up := v.unsafePtr()
	size := *(*uint32)(unsafe.Add(up, 12))
	if size == 0 {
		return nil
	}
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	nUsed := int(*(*uint32)(unsafe.Add(up, 28)))
	eaPtr := *(*unsafe.Pointer)(unsafe.Add(up, 32))
	if eaPtr == nil || nUsed <= 0 {
		return nil
	}
	if (flags & (1 << 12)) != 0 { // MRB_HASH_HT
		eaPtr = *(*unsafe.Pointer)(eaPtr)
		if eaPtr == nil {
			return nil
		}
	}
	return unsafe.Slice((*[2]Value)(eaPtr), nUsed)
}

// HashSet sets a keys and values to hashes
func (mrb *MrbState) HashSet(hash, key, val MrbValue) {
	C.mrb_hash_set(mrb.p, hash.Value().v, key.Value().v, val.Value().v)
}

// HashGet gets a value from a key. If the key is not found, the default of the hash is used
func (mrb *MrbState) HashGet(hash, key MrbValue) Value {
	hv := hash.Value()
	if hv.Pointer() > 65536 {
		up := hv.unsafePtr()
		flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
		if (flags & ((1 << 10) | (1 << 11) | (1 << 12))) == 0 {
			size := *(*uint32)(unsafe.Add(up, 12))
			if size == 0 {
				return nilValue
			}
			nUsed := int(*(*uint32)(unsafe.Add(up, 28)))
			eaPtr := *(*unsafe.Pointer)(unsafe.Add(up, 32))
			if eaPtr != nil && nUsed > 0 {
				entries := unsafe.Slice((*[2]Value)(eaPtr), nUsed)
				kv := key.Value()
				ktt := kv.Type()
				if ktt == MrbTTString {
					kp, kn := RStringMem(kv)
					ks := unsafe.String(kp, kn)
					for i := 0; i < len(entries); i++ {
						ek := entries[i][0]
						if ek.Type() == MrbTTString {
							ep, en := RStringMem(ek)
							if en == kn && (ep == kp || unsafe.String(ep, en) == ks) {
								return entries[i][1]
							}
						}
					}
					return nilValue
				} else if ktt == MrbTTSymbol || ktt == MrbTTInteger || ktt == MrbTTTrue || ktt == MrbTTFalse {
					for i := 0; i < len(entries); i++ {
						if entries[i][0] == kv {
							return entries[i][1]
						}
					}
					return nilValue
				}
			}
		}
	}
	return Value{C.mrb_hash_get(mrb.p, hv.v, key.Value().v)}
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
	return C.mrb_hash_key_p(mrb.p, hash.Value().v, key.Value().v) != false
}

// HashEmptyP check if the hash is empty
func (mrb *MrbState) HashEmptyP(hash MrbValue) bool {
	return C.mrb_hash_empty_p(mrb.p, hash.Value().v) != false
}

// HashValues returns an array of values, equivalent to hash.values
func (mrb *MrbState) HashValues(hash MrbValue) Value {
	return Value{C.mrb_hash_values(mrb.p, hash.Value().v)}
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

// RHashIfNone get ifnone value from hash
func (mrb *MrbState) RHashIfNone(h MrbValue) Value {
	return mrb.IVGet(h, Sym_ifnone)
}

// RHashProcDefault alias for RHashIfNone
func (mrb *MrbState) RHashProcDefault(h MrbValue) Value {
	return mrb.RHashIfNone(h)
}

// Hash defaults
const (
	MrbHashDefault     = 1
	MrbHashProcDefault = 2
)

// MrbRHashProcDefaultP checks hash procdefault
func MrbRHashProcDefaultP(h MrbValue) bool { return (C._MRB_RHASH_PROCDEFAULT_P(h.Value().v) != 0) }

// MrbHashForeachFuncT is hash foreach callback func. Return non-zero to break the loop
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
	mrb.HashForEach(RHash{RValue{hash.Value().v, mrb}}, f)
}

// HashForEach wakls the hash item pairs
func (mrb *MrbState) HashForEach(hash RHash, f MrbHashForeachFuncT) {
	p := unsafe.Pointer(hash.Ptr().p)

	mrb.setHook(p, f)
	C.mrb_hash_foreach(mrb.p, hash.Ptr().p, (*C.mrb_hash_foreach_func)(C.set_hash_callback), p)
	mrb.setHook(p, nil)
}
