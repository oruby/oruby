package oruby

// #include "go-mrb.h"
import "C"
import "unsafe"

// RString struct
type RString struct{ RObject }

// RStringPtr struct holds pointer to C API RString struct
type RStringPtr struct{ p *C.struct_RString }

// RStringEmbed embeded string struct
type RStringEmbed struct{ p *C.struct_RStringEmbed }

// Ptr embeds raw mruby C API RString pointer
func (a RString) Ptr() RStringPtr { return RStringPtr{(*C.struct_RString)(C._mrb_ptr(a.v))} }

// string
// func IS_EVSTR(p, e string) bool
// extern const char mrb_digitmap[]

// MrbStrPtr returns RString from MrbValue interface
func MrbStrPtr(s MrbValue) RStringPtr {
	return RStringPtr{(*C.struct_RString)(C._mrb_ptr(s.Value().v))}
}

// RStringLen Returns string len
func RStringLen(s MrbValue) int { return int(C._RSTRING_LEN(s.Value().v)) }

// C.mrb_str_strlen() never called - it raises on NULL in string, which is fine with Go strings

// RStringCapa Returns string capcaity
func RStringCapa(a MrbValue) int { return int(C._RSTRING_CAPA(a.Value().v)) }

// RStringEnd Returns string end
func RStringEnd(a MrbValue) uintptr { return uintptr(unsafe.Pointer(C._RSTRING_END(a.Value().v))) }

// string flags
const (
	MrbStrShared        = 1
	MrbStrFShared       = 2
	MrbStrNofree        = 4
	MrbStrEmbed         = 8
	MrbStrASCII         = 16
	MrbStrEmbedLenShift = 6
	MrbStrEmbedLenBit   = 5
	MrbStrEmbedLenMask  = ((1 << C.MRB_STR_EMBED_LEN_BIT) - 1) << C.MRB_STR_EMBED_LEN_SHIFT
	MrbStrTypeMask      = 15
)

//func (mrb *MrbState) GCFreeStr(s RString) { C.mrb_gc_free_str(mrb.p, s.Ptr().p) }

// StrModify modify string
func (mrb *MrbState) StrModify(s RString) { C.mrb_str_modify(mrb.p, s.Ptr().p) }

// StrModifyKeepASCII modify stringwith keeping ASCII flag if set
func (mrb *MrbState) StrModifyKeepASCII(s RString) { C.mrb_str_modify_keep_ascii(mrb.p, s.Ptr().p) }

// MrbStrIndex finds the index of a substring in a string
func (mrb *MrbState) MrbStrIndex(str MrbValue, s string, offset int) int {
	cstr := C.CString(s)
	defer C.free(unsafe.Pointer(cstr))
	return int(C.mrb_str_index(mrb.p, str.Value().v, cstr, C.mrb_int(len(s)), C.mrb_int(offset)))
}

// StrConcat appends self to other. Returns self as a concatenated string
func (mrb *MrbState) StrConcat(s1, s2 MrbValue) { C.mrb_str_concat(mrb.p, s1.Value().v, s2.Value().v) }

// StrPlus Adds two strings together.
func (mrb *MrbState) StrPlus(s1, s2 MrbValue) Value {
	return Value{C.mrb_str_plus(mrb.p, s1.Value().v, s2.Value().v)}
}

// PtrToStr represents pointer as a string
func (mrb *MrbState) PtrToStr(p uintptr) RString {
	return RString{RObject{
		C._mrb_ptr_to_str(mrb.p, C.uintptr_t(p)),
		mrb,
	}}
	// C.mrb_ptr_to_str() API is called via underscore helper
}

// ObjAsString Returns an object as a Ruby string
func (mrb *MrbState) ObjAsString(obj MrbValue) RString {
	return RString{RObject{
		C.mrb_obj_as_string(mrb.p, obj.Value().v),
		mrb,
	}}
}

// StrResize Resizes the string. Returns the amount of characters in the specified by len
func (mrb *MrbState) StrResize(str MrbValue, len int) Value {
	return Value{C.mrb_str_resize(mrb.p, str.Value().v, C.mrb_int(len))}
}

// StrSubstr Returns a sub string.
func (mrb *MrbState) StrSubstr(str MrbValue, beg, len int) Value {
	return Value{C.mrb_str_substr(mrb.p, str.Value().v, C.mrb_int(beg), C.mrb_int(len))}
}

// EnsureStringType  Returns a Ruby string type
func (mrb *MrbState) EnsureStringType(str MrbValue) RString {
	if !str.Value().IsString() {
		panic(mrb.TypeName(str) + " cannot be converted to String")
	}

	return RString{RObject{
		str.Value().v,
		mrb,
	}}
	// C.mrb_ensure_string_type() is never called
}

// StringType is obsolete; use EnsureStringType
func (mrb *MrbState) StringType(str MrbValue) RString {
	return mrb.EnsureStringType(str)
	// C.mrb_string_type() is never called
}

// CheckStringType checks string type
func (mrb *MrbState) CheckStringType(str MrbValue) Value {
	return Value{C.mrb_check_string_type(mrb.p, str.Value().v)}
}

// StrBufNew string new with buffer
// obsolete: user StrNewCapa
func (mrb *MrbState) StrBufNew(capa int) RString {
	return mrb.StrNewCapa(capa)
}

// StrNewCapa string new with buffer
func (mrb *MrbState) StrNewCapa(capa int) RString {
	return RString{RObject{
		C.mrb_str_new_capa(mrb.p, C.mrb_int(capa)),
		mrb,
	}}
}

// StringCstr string from mrb_value
func (mrb *MrbState) StringCstr(str MrbValue) string {
	v := str.Value().v
	return C.GoString(C.mrb_string_cstr(mrb.p, v))
}

// StringValueCstr string from mrb_value; `str` will be updated
func (mrb *MrbState) StringValueCstr(str MrbValue) string {
	v := str.Value().v
	return C.GoString(C.mrb_string_value_cstr(mrb.p, &v))
}

// StrDup Duplicates a string object.
func (mrb *MrbState) StrDup(str MrbValue) Value {
	return Value{C.mrb_str_dup(mrb.p, str.Value().v)}
}

// StrIntern Returns a Symbol Value from a Ruby string
func (mrb *MrbState) StrIntern(str MrbValue) Value {
	return Value{C.mrb_str_intern(mrb.p, str.Value().v)}
}

// StrToInteger str value to integer value
func (mrb *MrbState) StrToInteger(str MrbValue, base int, badcheck bool) Value {
	return Value{C.mrb_str_to_integer(mrb.p, str.Value().v, C.mrb_int(base), iifmb(badcheck))}
}

// StrToDbl str value to float64
func (mrb *MrbState) StrToDbl(str MrbValue, badcheck bool) float64 {
	return float64(C.mrb_str_to_dbl(mrb.p, str.Value().v, iifmb(badcheck)))
}

// StrToStr Returns a converted string type.
// for type checking, non converting `mrb_to_str` is recommended.
// obsolete: use `mrb_obj_as_string()` instead.
func (mrb *MrbState) StrToStr(str MrbValue) Value {
	return Value{C.mrb_obj_as_string(mrb.p, str.Value().v)}
}

// StrEqual  Returns true if the strings match and false if the strings don't match
func (mrb *MrbState) StrEqual(str1, str2 MrbValue) bool {
	return C.mrb_str_equal(mrb.p, str1.Value().v, str2.Value().v) != false
}

// StrCat Returns a concatenated string comprised of a Ruby string and a C string.
func (mrb *MrbState) StrCat(str MrbValue, s string) Value {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return Value{C.mrb_str_cat(mrb.p, str.Value().v, cs, C.size_t(len(s)))}
}

// StrCatBytes Returns a concatenated string comprised of a Ruby string and []byte
func (mrb *MrbState) StrCatBytes(str MrbValue, b []byte) Value {
	if len(b) == 0 {
		return str.Value()
	}
	return Value{C.mrb_str_cat(mrb.p, str.Value().v, (*C.char)(unsafe.Pointer(&b[0])), C.size_t(len(b)))}
}

// StrCatCstr Returns a concatenated string comprised of a Ruby string and a C string.
func (mrb *MrbState) StrCatCstr(v MrbValue, s string) Value {
	return mrb.StrCat(v, s)
	// pure C.mrb_str_cat_cstr() is never called
}

// StrCatStr concat
func (mrb *MrbState) StrCatStr(str, str2 MrbValue) Value {
	return Value{C.mrb_str_cat_str(mrb.p, str.Value().v, str2.Value().v)}
}

// StrAppend Adds str2 to the end of str1
func (mrb *MrbState) StrAppend(str, str2 MrbValue) Value {
	return Value{C.mrb_str_append(mrb.p, str.Value().v, str2.Value().v)}
}

// StrCmp  Returns 0 if both Ruby strings are equal. Returns a value < 0 if Ruby
// str1 is less than Ruby str2. Returns a value > 0 if Ruby str2 is greater than Ruby str1.
func (mrb *MrbState) StrCmp(str1, str2 MrbValue) int {
	return int(C.mrb_str_cmp(mrb.p, str1.Value().v, str2.Value().v))
}

// StrToCstr Returns a newly allocated string from a Ruby string.
func (mrb *MrbState) StrToCstr(str MrbValue) string {
	if !MrbStringP(str) {
		mrb.Raise(mrb.ETypeError(), "expected String")
		return ""
	}

	return C.GoStringN(C.mrb_str_to_cstr(mrb.p, str.Value().v), C.int(RStringLen(str)))
}

// Bytes returns a bytes from ruby MRbValue interface
// MrbValue must be of string type, or function panics
func (mrb *MrbState) Bytes(str MrbValue) []byte {
	if !MrbStringP(str) {
		panic("expected String value")
	}
	cstr := C.mrb_str_to_cstr(mrb.p, str.Value().v)
	return C.GoBytes(unsafe.Pointer(cstr), C.int(RStringLen(str)))
}

// StrCat2 For backward compatibility
func (mrb *MrbState) StrCat2(str MrbValue, s string) Value {
	return mrb.StrCatCstr(str, s)
}

// StrBufAppend For backward compatibility
func (mrb *MrbState) StrBufAppend(str, str2 MrbValue) Value {
	return mrb.StrCatStr(str, str2)
}

// Intentionally left unsuported
// C.mrb_string_value_ptr() - obsolete
// C.mrb_string_value_len() - obsolete
