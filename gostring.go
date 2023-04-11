package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

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

// Modify modify string
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

// ModifyKeepASCII modify stringwith keeping ASCII flag if set
func (s RString) ModifyKeepASCII() { C.mrb_str_modify_keep_ascii(s.mrb.p, s.Ptr().p) }

// Clone returns copy of string
func (s RString) Clone() RString {
	return RString{RObject{
		C.mrb_str_dup(s.mrb.p, s.v),
		s.mrb,
	}}
}

// Concat appends self to other. self as a concatenated string
func (s RString) Concat(str MrbValue) { C.mrb_str_concat(s.mrb.p, s.v, str.Value().v) }

// cloneV clone string with new C mrb_value
func (s RString) cloneV(v C.mrb_value) RString {
	return RString{RObject{v, s.mrb}}
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

// CheckStringType checks string type
func (s RString) CheckStringType(str MrbValue) (RString, error) {
	var err error
	v, err := s.mrb.try(func() C.mrb_value {
		return C.mrb_check_string_type(s.mrb.p, str.Value().v)
	})
	if err != nil {
		return s, err
	}

	return s.cloneV(v.v), err
}

// Dup Duplicates a string object.
func (s RString) Dup() RString {
	return RString{RObject{
		C.mrb_str_dup(s.mrb.p, s.v),
		s.mrb,
	}}
}

// Intern Returns a symbol from a passed in Ruby string.
func (s RString) Intern() Value {
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

// Cat Returns a concatenated string comprised of a Ruby string and a C string.
func (s RString) Cat(str string) RString {
	cs := C.CString(str)
	defer C.free(unsafe.Pointer(cs))
	return s.cloneV(C.mrb_str_cat(s.mrb.p, s.v, cs, C.size_t(len(str))))
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
