package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"unsafe"
)

type Type uint32

// ORuby value types (mirror enum mrb_vtype; order changes between mruby versions)
const (
	MrbTTFalse     Type = C.MRB_TT_FALSE
	MrbTTTrue      Type = C.MRB_TT_TRUE
	MrbTTSymbol    Type = C.MRB_TT_SYMBOL
	MrbTTUndef     Type = C.MRB_TT_UNDEF
	MrbTTFree      Type = C.MRB_TT_FREE
	MrbTTFloat     Type = C.MRB_TT_FLOAT
	MrbTTInteger   Type = C.MRB_TT_INTEGER
	MrbTTCptr      Type = C.MRB_TT_CPTR
	MrbTTObject    Type = C.MRB_TT_OBJECT
	MrbTTClass     Type = C.MRB_TT_CLASS
	MrbTTModule    Type = C.MRB_TT_MODULE
	MrbTTSClass    Type = C.MRB_TT_SCLASS
	MrbTTHash      Type = C.MRB_TT_HASH
	MrbTTCData     Type = C.MRB_TT_CDATA
	MrbTTException Type = C.MRB_TT_EXCEPTION
	MrbTTIClass    Type = C.MRB_TT_ICLASS
	MrbTTProc      Type = C.MRB_TT_PROC
	MrbTTArray     Type = C.MRB_TT_ARRAY
	MrbTTString    Type = C.MRB_TT_STRING
	MrbTTRange     Type = C.MRB_TT_RANGE
	MrbTTEnv       Type = C.MRB_TT_ENV
	MrbTTFiber     Type = C.MRB_TT_FIBER
	MrbTTStruct    Type = C.MRB_TT_STRUCT
	MrbTTIStruct   Type = C.MRB_TT_ISTRUCT
	MrbTTBreak     Type = C.MRB_TT_BREAK
	MrbTTComplex   Type = C.MRB_TT_COMPLEX
	MrbTTRational  Type = C.MRB_TT_RATIONAL
	MrbTTBigInt    Type = C.MRB_TT_BIGINT
	MrbTTBacktrace Type = C.MRB_TT_BACKTRACE
	MrbTTSet       Type = C.MRB_TT_SET
	MrbTTSVar      Type = C.MRB_TT_SVAR
	MrbTTMaxdefine Type = C.MRB_TT_MAXDEFINE
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
		return int(*(*int64)(unsafe.Pointer(&v.v)))
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

func (v Value) unsafePtr() unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&v.v))
}

// RStringMem returns the raw byte pointer and length of a Ruby String value without CGO calls.
func RStringMem(v Value) (*byte, int) {
	if v.Pointer() <= 65536 {
		return nil, 0
	}
	up := v.unsafePtr()
	flags := (*(*uint32)(unsafe.Add(up, 8))) >> 12
	if (flags & 8) != 0 { // MRB_STR_EMBED
		n := int((flags >> 4) & 31)
		return (*byte)(unsafe.Add(up, 12)), n
	}
	n := int(*(*int64)(unsafe.Add(up, 16)))
	ptr := *(**byte)(unsafe.Add(up, 32))
	return ptr, n
}

// String returns string from value
func (v Value) String() string {
	switch v.Type() {
	case MrbTTString:
		ptr, n := RStringMem(v)
		if n <= 0 || ptr == nil {
			return ""
		}
		return string(unsafe.Slice(ptr, n))
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
		panic(fmt.Sprintf("TypeError: no direct conversion of '%v' into String. Try mrb.String(v)", TypeName(v.Type())))
	}
}

// Bytes returns byte array from value
func (v Value) Bytes() []byte {
	switch v.Type() {
	case MrbTTString:
		ptr, n := RStringMem(v)
		if n <= 0 || ptr == nil {
			return []byte{}
		}
		b := make([]byte, n)
		copy(b, unsafe.Slice(ptr, n))
		return b
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
		return *(*float64)(unsafe.Pointer(&v.v))
	case MrbTTInteger:
		return float64(*(*int64)(unsafe.Pointer(&v.v)))
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
	return MrbSym(*(*uint32)(unsafe.Pointer(&v.v)))
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

// Pointer returns direct pointer from value without CGO overhead
func (v Value) Pointer() uintptr {
	return *(*uintptr)(unsafe.Pointer(&v.v))
}

// MrbPtr return pointer in oruby value without CGO overhead
func MrbPtr(v MrbValue) uintptr { return v.Value().Pointer() }

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
	if f != f {
		return Value{C.mrb_float_value(mrb.p, C.mrb_float(f))}
	}
	var cv C.mrb_value
	cv.tt = C.MRB_TT_FLOAT
	*(*float64)(unsafe.Pointer(&cv)) = f
	return Value{cv}
}

// StringValue float to oruby value
func (mrb *MrbState) StringValue(s string) RString {
	return RString{RValue{
		mrb.StrNew(s).v,
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

// FixnumValue converts int to Value without CGO overhead
func (mrb *MrbState) FixnumValue(n int) Value {
	var cv C.mrb_value
	cv.tt = C.MRB_TT_INTEGER
	*(*int64)(unsafe.Pointer(&cv)) = int64(n)
	return Value{cv}
}

// MrbFixnumValue fixnum value without CGO overhead
func MrbFixnumValue(i int) Value {
	var cv C.mrb_value
	cv.tt = C.MRB_TT_INTEGER
	*(*int64)(unsafe.Pointer(&cv)) = int64(i)
	return Value{cv}
}

// WrapSpecialObject wraps a lightweight Go object pool slot ID into a Value without CGO overhead
func WrapSpecialObject(id uintptr) Value {
	var cv C.mrb_value
	cv.tt = C.MRB_TT_OBJECT
	*(*uintptr)(unsafe.Pointer(&cv)) = id
	return Value{cv}
}

// WrapSpecialHash wraps a lightweight Go hash pool slot ID into a Value without CGO overhead
func WrapSpecialHash(id uintptr) Value {
	var cv C.mrb_value
	cv.tt = C.MRB_TT_HASH
	*(*uintptr)(unsafe.Pointer(&cv)) = id
	return Value{cv}
}

// MrbSymbolValue MigrateTo from symbol without CGO overhead
func MrbSymbolValue(i MrbSym) Value {
	var cv C.mrb_value
	cv.tt = C.MRB_TT_SYMBOL
	*(*uint32)(unsafe.Pointer(&cv)) = uint32(i)
	return Value{cv}
}

// SymbolValue converts MrbSym to Value without CGO overhead
func (mrb *MrbState) SymbolValue(sym MrbSym) Value {
	var cv C.mrb_value
	cv.tt = C.MRB_TT_SYMBOL
	*(*uint32)(unsafe.Pointer(&cv)) = uint32(sym)
	return Value{cv}
}

// mrbObjValue converts C object pointer to Value without CGO overhead
func mrbObjValue(p unsafe.Pointer) Value {
	if p == nil {
		return nilValue
	}
	var cv C.mrb_value
	cv.tt = C.enum_mrb_vtype(*(*uint8)(unsafe.Add(p, 8)))
	*(*unsafe.Pointer)(unsafe.Pointer(&cv)) = p
	return Value{cv}
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

// BoolValue from boolean without CGO overhead
func (mrb *MrbState) BoolValue(b bool) Value {
	var cv C.mrb_value
	if b {
		cv.tt = C.MRB_TT_TRUE
	} else {
		cv.tt = C.MRB_TT_FALSE
	}
	*(*int64)(unsafe.Pointer(&cv)) = 1
	return Value{cv}
}

// Predefined helper values
var (
	False    = Value{C.mrb_false_value()}
	Nil      = Value{}
	nilValue = Value{}
	True     = Value{C.mrb_true_value()}
	Undef    = Value{C.mrb_undef_value()}
)

// BatchOp represents a single RPN instruction for batch value construction in C.
type BatchOp struct {
	Op  uint32
	Off uint32
	I   int64
	F   float64
}

// ExecBatchOps executes a batch of RPN value construction operations in a single CGO call.
func (mrb *MrbState) ExecBatchOps(ops []BatchOp, strBuf []byte) Value {
	if len(ops) == 0 {
		return nilValue
	}
	var strPtr *C.char
	if len(strBuf) > 0 {
		strPtr = (*C.char)(unsafe.Pointer(&strBuf[0]))
	}
	v := Value{C._mrb_exec_batch_ops(
		mrb.p,
		(*C.mrb_batch_op)(unsafe.Pointer(&ops[0])),
		C.mrb_int(len(ops)),
		strPtr,
	)}
	runtime.KeepAlive(ops)
	runtime.KeepAlive(strBuf)
	return v
}

// Intentionally left unsuported
// C.mrb_float_read()
// C.mrb_msvc_vsnprintf()
// C.mrb_msvc_snprintf()
