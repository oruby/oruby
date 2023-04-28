package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"runtime"
	"strconv"
	"unsafe"
)

type Type uint32

// ORuby value types
const (
	MrbTTFalse     Type = iota
	MrbTTTrue           // 1
	MrbTTSymbol         // 2
	MrbTTUndef          // 3
	MrbTTFree           // 4
	MrbTTFloat          // 5
	MrbTTInteger        // 6
	MrbTTCptr           // 7
	MrbTTObject         // 8
	MrbTTClass          // 9
	MrbTTModule         // 10
	MrbTTIClass         // 11
	MrbTTSClass         // 12
	MrbTTProc           // 13
	MrbTTArray          // 14
	MrbTTHash           // 15
	MrbTTString         // 16
	MrbTTRange          // 17
	MrbTTException      // 18
	MrbTTEnv            // 19
	MrbTTCData          // 20
	MrbTTFiber          // 21
	MrbTTStruct         // 22
	MrbTTIStruct        // 23
	MrbTTBreak          // 24
	MrbTTComplex        // 25
	MrbTTRational       // 26
	MrbTTBigInt         // 27
	MrbTTMaxdefine      // 28
)

// MrbTTFixnum is deprecated. MrbTTInteger is preferred
const MrbTTFixnum = MrbTTInteger

// MrbTTHasBasic is first type with object
const MrbTTHasBasic = MrbTTFree

// HasBasic returns true if value is RBasic object
func (v Value) HasBasic() bool {
	return !MrbImmediateP(v)
}

func checkType(valueType, mustBeType Type) {
	if valueType != mustBeType {
		panic("wrong type")
	}
}

// Int returns direct integer from value.
// Suported types are Fixnum, Symbol, Bool as 0 or 1 and Float truncated
func (v Value) Int() int {
	switch v.Type() {
	case MrbTTInteger:
		return int(C._mrb_fixnum(v.v))
	case MrbTTSymbol:
		return int(v.Symbol())
	case MrbTTFalse:
		return 0
	case MrbTTTrue:
		return 1
	case MrbTTFloat:
		return int(C._mrb_float(v.v))
	default:
		panic("value can't be convertend directly to int, try using oruby state functions like to_i")
	}
}

// Int64 returns int64 from value
func (v Value) Int64() int64 {
	return int64(v.Int())
}

// String returns string from value
func (v Value) String() string {
	switch v.Type() {
	case MrbTTString:
		return C.GoStringN(C._RSTRING_PTR(v.v), C.int(C._RSTRING_LEN(v.v)))
	case MrbTTFixnum:
		return strconv.Itoa(v.Int())
	case MrbTTFalse:
		if v.IsNil() {
			return ""
		}
		return "false"
	case MrbTTTrue:
		return "true"
	default:
		panic("TypeError: no direct conversion into String. Try mrb.String(v)")
	}
}

// Bytes returns byte array from value
func (v Value) Bytes() []byte {
	switch v.Type() {
	case MrbTTString:
		return C.GoBytes(unsafe.Pointer(C._RSTRING_PTR(v.v)), C.int(C._RSTRING_LEN(v.v)))
	case MrbTTFalse:
		if v.IsNil() {
			return nil
		}
		return []byte{uint8(0)}
	case MrbTTTrue:
		return []byte{uint8(1)}
	default:
		panic("value cannot be converted directly to string, try converting with oruby state - mrb.String()")
	}
}

// Fixnum returns int from value
func (v Value) Fixnum() int { return v.Int() }

// Bool returns false if false or nil, true othervise
func (v Value) Bool() bool { return v.Type() != C.MRB_TT_FALSE }

// Uintptr returns uintptr from value
func (v Value) Uintptr() uintptr { return uintptr(C._mrb_cptr(v.Value().v)) }

// Float64 returns int from value
func (v Value) Float64() float64 {
	switch v.Type() {
	case MrbTTFloat:
		return float64(C._mrb_float(v.v))
	case MrbTTInteger:
		return float64(C._mrb_fixnum(v.v))
	case MrbTTFalse:
		return 0.0
	case MrbTTTrue:
		return 1.0
	default:
		panic("value can't be converted directly to float, try using oruby state functions like to_f")
	}
}

// Symbol returns symbol from value
func (v Value) Symbol() MrbSym {
	checkType(v.Type(), MrbTTSymbol)
	return MrbSym(C._mrb_symbol(v.v))
}

// Freeze sets value as frozen
func (v Value) Freeze() Value {
	if v.HasBasic() {
		v.RBasic().SetFrozen(true)
	}
	return v
}

func (v Value) Frozen() bool {
	return !v.HasBasic() || v.RBasic().IsFrozen()
}

// Unfreeze removes frozen flag from value
func (v Value) Unfreeze() Value {
	if v.HasBasic() {
		v.RBasic().SetFrozen(false)
	}
	return v
}

// TestFlag check if flag is set on object
func (v Value) TestFlag(flag uint32) bool {
	if !v.HasBasic() {
		return false
	}
	return (uint32(v.RBasic().Flags()) & uint32(flag)) != 0
}

// MrbType returns type of oruby value
func MrbType(v MrbValue) Type { return v.Type() }

// MrbPtr return pointer in oruby value
func MrbPtr(v MrbValue) uintptr { return uintptr(C._mrb_ptr(v.Value().v)) }

// MrbFloat returns float form value
func MrbFloat(v MrbValue) float64 { return float64(C._mrb_float(v.Value().v)) }

// MrbDouble returns float from value
func MrbDouble(v MrbValue) float64 { return float64(C._mrb_float(v.Value().v)) }

// MrbFixnum return integer from value
func MrbFixnum(v MrbValue) int { return int(C._mrb_fixnum(v.Value().v)) }

// MrbSymbol returns symbol from value
func MrbSymbol(v MrbValue) MrbSym { return MrbSym(C._mrb_symbol(v.Value().v)) }

// MrbVoidp returns pointer from value
func MrbVoidp(v MrbValue) uintptr { return uintptr(C._mrb_cptr(v.Value().v)) }

// MrbCptr returns pointer from value
func MrbCptr(v MrbValue) uintptr { return uintptr(C._mrb_cptr(v.Value().v)) }

// MrbFixnumP checks Fixnum value
func MrbFixnumP(v MrbValue) bool { return v.Type() == C.MRB_TT_FIXNUM }

// MrbFloatP checks Float value
func MrbFloatP(v MrbValue) bool { return v.Type() == C.MRB_TT_FLOAT }

// MrbUndefP checks Undef value
func MrbUndefP(v MrbValue) bool { return v.Type() == C.MRB_TT_UNDEF }

// MrbNilP checks Nil value
func MrbNilP(v MrbValue) bool {
	return v.IsNil()
}

// MrbSymbolP checks Symbol value
func MrbSymbolP(v MrbValue) bool { return v.Type() == C.MRB_TT_SYMBOL }

// MrbArrayP checks Array value
func MrbArrayP(v MrbValue) bool { return v.Type() == C.MRB_TT_ARRAY }

// MrbStringP checks String value
func MrbStringP(v MrbValue) bool { return v.Type() == C.MRB_TT_STRING }

// MrbHashP checks Hash value
func MrbHashP(v MrbValue) bool { return v.Type() == C.MRB_TT_HASH }

// MrbVoidpP checks Voidp value
func MrbVoidpP(v MrbValue) bool { return v.Type() == C.MRB_TT_CPTR }

// MrbCptrP checks Cptr value
func MrbCptrP(v MrbValue) bool { return v.Type() == C.MRB_TT_CPTR }

// MrbDataP checks Data value
func MrbDataP(v MrbValue) bool { return v.Type() == C.MRB_TT_DATA }

// MrbBoolean checks Boolean value
func MrbBoolean(v MrbValue) bool { return v.Type() != C.MRB_TT_FALSE }

// MrbTest checks Truthy value
func MrbTest(v MrbValue) bool { return v.Type() != C.MRB_TT_FALSE }

// MrbImmediateP checks Immediate value
func MrbImmediateP(x MrbValue) bool {
	return MrbType(x) <= C.MRB_TT_CPTR
}

// FixnumP checks if value is Fixnum
func (mrb *MrbState) FixnumP(v MrbValue) bool { return v.Type() == C.MRB_TT_FIXNUM }

// FloatP checks if value is Float
func (mrb *MrbState) FloatP(v MrbValue) bool { return v.Type() == C.MRB_TT_FLOAT }

// UndefP checks if value is Undef
func (mrb *MrbState) UndefP(v MrbValue) bool { return v.Type() == C.MRB_TT_UNDEF }

// NilP checks if value is Nil
func (mrb *MrbState) NilP(v MrbValue) bool { return v.IsNil() }

// SymbolP checks if value is Symbol
func (mrb *MrbState) SymbolP(v MrbValue) bool { return v.Type() == C.MRB_TT_SYMBOL }

// ArrayP checks if value is Array
func (mrb *MrbState) ArrayP(v MrbValue) bool { return v.Type() == C.MRB_TT_ARRAY }

// AtringP checks if value is Atring
func (mrb *MrbState) AtringP(v MrbValue) bool { return v.Type() == C.MRB_TT_STRING }

// HashP checks if value is Hash
func (mrb *MrbState) HashP(v MrbValue) bool { return v.Type() == C.MRB_TT_HASH }

// VoidpP checks if value is Voidp
func (mrb *MrbState) VoidpP(v MrbValue) bool { return v.Type() == C.MRB_TT_CPTR }

// CptrP checks if value is Cptr
func (mrb *MrbState) CptrP(v MrbValue) bool { return v.Type() == C.MRB_TT_CPTR }

// DataP checks if value is Data
func (mrb *MrbState) DataP(v MrbValue) bool { return v.Type() == C.MRB_TT_DATA }

// Boolean checks if value is Boolean
func (mrb *MrbState) Boolean(v MrbValue) bool { return v.Type() != C.MRB_TT_FALSE }

// Test checks if value is not nil or false
func (mrb *MrbState) Test(v MrbValue) bool { return v.Type() != C.MRB_TT_FALSE }

// FloatValue float to oruby value
func (mrb *MrbState) FloatValue(f float64) Value {
	return Value{C.mrb_float_value(mrb.p, C.mrb_float(f))}
}

// StringValue float to oruby value
func (mrb *MrbState) StringValue(s string) RString {
	ptr := C.CString(s)
	defer C.free(unsafe.Pointer(ptr))
	return RString{RValue{
		C.mrb_str_new(mrb.p, ptr, C.mrb_int(len(s))),
		mrb,
	}}
}

// BytesValue bytes to oruby string value
func (mrb *MrbState) BytesValue(buf []byte) Value {
	if len(buf) == 0 {
		return Value{C.mrb_str_new(mrb.p, nil, C.mrb_int(0))}
	}
	v := Value{C.mrb_str_new(mrb.p, (*C.char)(unsafe.Pointer((&buf[0]))), C.mrb_int(len(buf)))}

	runtime.KeepAlive(buf)
	return v
}

// FloatPool pool
func (mrb *MrbState) FloatPool(f float64) Value { return mrb.FloatValue(f) }

// FixnumValue converts int to Value
func (mrb *MrbState) FixnumValue(n int) Value { return Value{C.mrb_fixnum_value(C.mrb_int(n))} }

// MrbFixnumValue finxnum value
func MrbFixnumValue(i int) Value { return Value{C.mrb_fixnum_value(C.mrb_int(i))} }

// MrbSymbolValue MigrateTo from symbol
func MrbSymbolValue(i MrbSym) Value { return Value{C.mrb_symbol_value(C.mrb_sym(i))} }

// SymbolValue converts MrbSym to Value
func (mrb *MrbState) SymbolValue(sym MrbSym) Value { return Value{C.mrb_symbol_value(C.mrb_sym(sym))} }

// SymbolValue converts MrbSym to Value
func mrbObjValue(p unsafe.Pointer) Value {
	if p == nil {
		return nilValue
	}
	return Value{C.mrb_obj_value(p)}
}

// MrbObjValue value form oruby object. In Go object must provide MrbValue interface,
// or NilValue is returned
func MrbObjValue(p interface{}) (Value, error) {
	switch v := p.(type) {
	case int, int8, int16, int32, uint, uint8, uint16, uint32:
		return MrbFixnumValue(v.(int)), nil
	case int64, uint64:
		return MrbFixnumValue(v.(int)), nil
	case bool:
		return Bool(v), nil
	case nil:
		return nilValue, nil
	case MrbValue:
		return v.Value(), nil
	default:
		return nilValue, errors.New("direct conversion to oruby.Value not supported")
	}
}

// CPtrValue value from  Pointer
func (mrb *MrbState) CPtrValue(p uintptr) Value {
	return Value{C._mrb_uintptr_value(mrb.p, (C.uintptr_t)(p))}
}

// VoidpValue value from pointer
func (mrb *MrbState) VoidpValue(p uintptr) Value { return mrb.CPtrValue(p) }

// Bool returns Value from boolen
func Bool(b bool) Value {
	if b {
		return Value{C.mrb_true_value()}
	}
	return Value{C.mrb_false_value()}
}

// Integer returns Value from int
func Integer(i int) Value {
	return Value{C.mrb_fixnum_value(C.mrb_int(i))}
}

// Int returns Value from int
func Int(i int) Value {
	return Value{C.mrb_fixnum_value(C.mrb_int(i))}
}

// Int64 returns Value from int64
func Int64(i int64) Value {
	return Value{C.mrb_fixnum_value(C.mrb_int(i))}
}

// BoolValue from boolen
func (mrb *MrbState) BoolValue(b bool) Value {
	if b {
		return Value{C.mrb_true_value()}
	}
	return Value{C.mrb_false_value()}
}

// Predefined helper values
var (
	False    = Value{C.mrb_false_value()}
	Nil      = Value{}
	nilValue = Value{}
	True     = Value{C.mrb_true_value()}
	Undef    = Value{C.mrb_undef_value()}
)

// Intentionally left unsuported
// C.mrb_float_read()
// C.mrb_msvc_vsnprintf()
// C.mrb_msvc_snprintf()
