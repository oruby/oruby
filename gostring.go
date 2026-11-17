package oruby

// #include "go-mrb.h"
import "C"
import (
	"runtime"
	"unsafe"
)

// Index finds the index of a substring in a string
func (s RString) Index(str string, offset int) int {
	cstr := C.CString(str)
	defer C.free(unsafe.Pointer(cstr))
	return int(C.mrb_str_index(s.mrb.p, s.v, cstr, C.mrb_int(len(str)), C.mrb_int(offset)))
}

// Len Returns string len
func (s RString) Len() int { return int(C._RSTRING_LEN(s.v)) }

// Capa Returns string capacity
func (s RString) Capa() int { return int(C._RSTRING_CAPA(s.v)) }

// Modify string
func (s RString) Modify() { C.mrb_str_modify(s.mrb.p, s.Ptr().p) }

// Flags return string object flags
func (s RString) Flags() int { return int(C._mrb_value_flags(s.v)) }

// IsFrozen returns true if string is frozen
func (s RString) IsFrozen() bool { return s.Flags()&MrbFlObjIsFrozen != 0 }

// IsShared returns true if string is shared
func (s RString) IsShared() bool { return s.Flags()&MrbStrShared != 0 }

// IsFShared returns true if string is fshared
func (s RString) IsFShared() bool { return s.Flags()&MrbStrFShared != 0 }

// IsNoFree returns true if string is marked with MrbStrNofree flag
func (s RString) IsNoFree() bool { return s.Flags()&MrbStrNofree != 0 }

// Clone returns copy of string
func (s RString) Clone() RString {
	return RString{RValue{
		C.mrb_str_dup(s.mrb.p, s.v),
		s.mrb,
	}}
}

// Concat appends str. self as a concatenated string
func (s RString) Concat(str MrbValue) { C.mrb_str_concat(s.mrb.p, s.v, str.Value().v) }

// cloneV clone string with new C mrb_value
func (s RString) cloneV(v C.mrb_value) RString {
	return RString{RValue{v, s.mrb}}
}

// Plus Adds two strings together.
func (s RString) Plus(str MrbValue) RString {
	return s.cloneV(C.mrb_str_plus(s.mrb.p, s.v, str.Value().v))
}

// Resize Resizes the string. Returns the amount of characters in the specified by len
func (s RString) Resize(len int) RString {
	return s.cloneV(C.mrb_str_resize(s.mrb.p, s.v, C.mrb_int(len)))
}

// Substr returns a sub string.
func (s RString) Substr(beg, len int) RString {
	return s.cloneV(C.mrb_str_substr(s.mrb.p, s.v, C.mrb_int(beg), C.mrb_int(len)))
}

// CheckStringType checks string type, returns nil value is not of string type
func (s RString) CheckStringType(str MrbValue) Value {
	return Value{C.mrb_check_string_type(s.mrb.p, str.Value().v)}
}

// Dup Duplicates a string object.
func (s RString) Dup() RString {
	return s.cloneV(C.mrb_str_dup(s.mrb.p, s.v))
}

// Intern Returns a symbol from a passed in Ruby string.
func (s RString) Intern() Value {
	return Value{C.mrb_str_intern(s.mrb.p, s.v)}
}

// ToSym Returns a symbol from a passed in Ruby string.
func (s RString) ToSym() Value {
	return Value{C.mrb_str_intern(s.mrb.p, s.v)}
}

// ToInteger str value to integer
func (s RString) ToInteger(base int, badcheck bool) Value {
	return Value{C.mrb_str_to_integer(s.mrb.p, s.v, C.mrb_int(base), iifmb(badcheck))}
}

// ToInum alias for ToInteger (deprecated)
func (s RString) ToInum(base int, badcheck bool) Value {
	return s.ToInteger(base, badcheck)
}

// ToDbl str value to float64
func (s RString) ToDbl(badcheck bool) float64 {
	return float64(C.mrb_str_to_dbl(s.mrb.p, s.v, iifmb(badcheck)))
}

// Equal  Returns true if the strings match and false if the strings don't match
func (s RString) Equal(str2 MrbValue) bool {
	return C.mrb_str_equal(s.mrb.p, s.v, str2.Value().v) != false
}

// Cat Returns a concatenated string comprised of a Ruby string and a Go string.
func (s RString) Cat(str string) RString {
	if len(str) == 0 {
		return s
	}
	v := s.cloneV(C.mrb_str_cat(s.mrb.p, s.v, (*C.char)(unsafe.Pointer(unsafe.StringData(str))), C.size_t(len(str))))
	runtime.KeepAlive(str)
	return v
}

// CatStr concat
func (s RString) CatStr(str2 MrbValue) RString {
	return s.cloneV(C.mrb_str_cat_str(s.mrb.p, s.v, str2.Value().v))
}

// Append Adds str2 to the end of str1
func (s RString) Append(str2 MrbValue) RString {
	return s.cloneV(C.mrb_str_append(s.mrb.p, s.v, str2.Value().v))
}

// Cmp returns 0 if both Ruby strings are equal. Returns a value < 0 if Ruby
// str1 is less than Ruby str2. Returns a value > 0 if Ruby str2 is greater than Ruby str1.
func (s RString) Cmp(str2 MrbValue) int {
	return int(C.mrb_str_cmp(s.mrb.p, s.v, str2.Value().v))
}

// String Returns a newly allocated string from a Ruby string.
func (s RString) String() string {
	return C.GoStringN(C.mrb_str_to_cstr(s.mrb.p, s.v), C.int(s.Len()))
}

// FSPath returns path string v as the file system takes it. macOS file names
// are UTF-8, and MRI converts a path in another encoding to UTF-8 first; on
// other systems, and for UTF-8, binary and ASCII-only paths, the bytes are
// used as they are.
func (mrb *MrbState) FSPath(v Value) string {
	if !MrbStringP(v) {
		// rb_get_path: #to_path, then #to_str; anything else is no path (an
		// empty name fails in the OS call instead of naming a file after inspect)
		for _, m := range []string{"to_path", "to_str"} {
			if mrb.RespondTo(v, mrb.Intern(m)) {
				if p, err := mrb.Funcall(v, mrb.Intern(m)); err == nil && MrbStringP(p) {
					return mrb.FSPath(p)
				}
				mrb.ExcClear()
				return ""
			}
		}
		return ""
	}
	s := v.String()
	if runtime.GOOS != "darwin" {
		return s
	}
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	enc, err := mrb.Funcall(v, mrb.Intern("encoding"))
	if err != nil {
		mrb.ExcClear()
		return s
	}
	name, err := mrb.Funcall(enc, mrb.Intern("name"))
	if err != nil || name.String() == "UTF-8" || name.String() == "ASCII-8BIT" {
		mrb.ExcClear()
		return s
	}
	conv, err := mrb.Funcall(v, mrb.Intern("encode"), mrb.StrNew("UTF-8"))
	if err != nil || !MrbStringP(conv) {
		mrb.ExcClear()
		return s
	}
	return conv.String()
}
