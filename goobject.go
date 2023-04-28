package oruby

// #include "go-mrb.h"
import "C"
import (
	"fmt"
)

func (obj RValue) HasIV() bool {
	switch obj.Type() {
	case MrbTTObject, MrbTTClass, MrbTTModule, MrbTTSClass, MrbTTHash, MrbTTCData, MrbTTException:
		return true
	default:
		return false
	}
}

func (obj RValue) RObject() RObject {
	return RObject{(*C.struct_RObject)(C._mrb_ptr(obj.v))}
}

// Class returns RClassPtr from object
func (o RObject) Class() RClassPtr { return RClassPtr{o.p.c} }

// Dup duplicates object
func (obj RValue) Dup() RValue {
	return RValue{C.mrb_obj_dup(obj.mrb.p, obj.v), obj.mrb}
}

// Freeze freeze value
func (obj RValue) Freeze() RValue {
	return RValue{C.mrb_obj_freeze(obj.mrb.p, obj.v), obj.mrb}
}

// ID returns ruby object id
func (obj RValue) ID() int { return int(C.mrb_obj_id(obj.v)) }

// Classname returns class name of object
func (obj RValue) Classname() string {
	return C.GoString(C.mrb_obj_classname(obj.mrb.p, obj.v))
}

// Class returns class of object
func (obj RValue) Class() RClass {
	return RClass{C.mrb_obj_class(obj.mrb.p, obj.v), obj.mrb}
}

// IsKindOf checks if object is descendant of c class
func (obj RValue) IsKindOf(c RClass) bool {
	cl := obj.mrb.ClassOf(obj.Value())
	switch cl.Type() {
	case MrbTTModule, MrbTTClass, MrbTTIClass, MrbTTSClass:
		return C.mrb_obj_is_kind_of(obj.mrb.p, obj.v, c.p) != false
	default:
		return false
	}
}

// Inspect object
func (obj RValue) Inspect() RString {
	return RString{RValue{
		C.mrb_obj_inspect(obj.mrb.p, obj.v),
		obj.mrb,
	}}
}

// Clone shallow object
func (obj RValue) Clone() RValue {
	return RValue{C.mrb_obj_clone(obj.mrb.p, obj.v), obj.mrb}
}

// RespondTo checks if object responds to method id
func (obj RValue) RespondTo(mid MrbSym) bool {
	return C.mrb_respond_to(obj.mrb.p, obj.v, C.mrb_sym(mid)) != false
}

// IsInstanceOf checks if oruby object is direct instance of class
func (obj RValue) IsInstanceOf(klass RClass) bool {
	return C.mrb_obj_is_instance_of(obj.mrb.p, obj.v, klass.p) != false
}

// Call oruby function, return Go interface,
// in case of error, Call returns NilValue and the error is in mrb.Err()
func (obj RValue) Call(name string, args ...interface{}) RValue {
	return RValue{obj.mrb.Call(obj, name, args...).v, obj.mrb}
}

// Funcall oruby function, return Go interface,
func (obj RValue) Funcall(name string, args ...interface{}) (RValue, error) {
	result, err := obj.mrb.Funcall(obj, obj.mrb.Intern(name), args...)
	if err != nil {
		return RValue{C.mrb_nil_value(), obj.mrb}, err
	}
	return RValue{result.v, obj.mrb}, err
}

// InstanceVariables list
func (obj RValue) InstanceVariables() RArray {
	return obj.Call("instance_variables").RArray()
}

// IVGet get instance variable
func (obj RValue) IVGet(sym MrbSym) Value {
	return Value{C.mrb_iv_get(obj.mrb.p, obj.v, C.mrb_sym(sym))}
}

// IVSet set instance variable
func (obj RValue) IVSet(sym MrbSym, v MrbValue) error {
	return obj.mrb.tryE(func() {
		C.mrb_iv_set(obj.mrb.p, obj.Value().v, C.mrb_sym(sym), v.Value().v)
	})
}

// SetIV set instance variable as string
func (obj RValue) SetIV(name string, v interface{}) {
	_ = obj.IVSet(obj.mrb.Intern(name), obj.mrb.Value(v))
}

// GetIV gets instance variable by string name
func (obj RValue) GetIV(name string) RValue {
	return RValue{obj.IVGet(obj.mrb.Intern(name)).v, obj.mrb}
}

// IVDefined instance variable defined
func (obj RValue) IVDefined(sym MrbSym) bool {
	return C.mrb_iv_defined(obj.mrb.p, obj.v, C.mrb_sym(sym)) != false
}

// IVRemove remove instance variable
func (obj RValue) IVRemove(sym MrbSym) Value {
	return Value{C.mrb_iv_remove(obj.mrb.p, obj.v, C.mrb_sym(sym))}
}

// Data returns object interface as Go interface
func (obj RValue) Data() interface{} {
	return obj.mrb.Data(obj)
}

// RArray returns Array object
// it panics if object is not RArray type
func (obj RValue) RArray() RArray {
	if obj.Type() != MrbTTArray {
		panic(fmt.Sprintf("array expected, but object of type %v", obj.mrb.TypeName(obj)))
	}
	return RArray{obj}
}

// RHash returns Hash object
// it panics if object is not RArray type
func (obj RValue) RHash() RHash {
	if obj.Type() != MrbTTHash {
		panic(fmt.Sprintf("hash expected, but object of type %v", obj.mrb.TypeName(obj)))
	}
	return RHash{obj}
}

// String return object value as string
func (obj RValue) String() string {
	return obj.mrb.String(obj.Value())
}

// Int return value as int
func (obj RValue) Int() int {
	result, err := obj.mrb.Integer(obj.Value())
	if err != nil {
		panic(err)
	}
	return MrbFixnum(result)
}

// Bool return value as false for nil and false, and true otherwise
func (obj RValue) Bool() bool { return obj.Value().Bool() }

// Float64 return value as float64
func (obj RValue) Float64() float64 {
	result, err := obj.mrb.Float(obj.Value())
	if err != nil {
		panic(err)
	}
	return MrbFloat(result)
}

// TypeName return value ruby type as string
func (mrb *MrbState) TypeName(v MrbValue) string {
	t := v.Type()

	if t == MrbTTFalse && v.IsNil() {
		return "Nil"
	}
	return TypeName(t)
}

// TypeName return value ruby type as string
func TypeName(v Type) string {
	switch v {
	case MrbTTFalse:
		return "False"
	case MrbTTTrue:
		return "True"
	case MrbTTFloat:
		return "Float"
	case MrbTTFixnum:
		return "Fixnum"
	case MrbTTSymbol:
		return "Symbol"
	case MrbTTUndef:
		return "Undef"
	case MrbTTCptr:
		return "CPtr"
	case MrbTTFree:
		return "Free"
	case MrbTTObject:
		return "Object"
	case MrbTTClass:
		return "Class"
	case MrbTTModule:
		return "Module"
	case MrbTTIClass:
		return "IClass"
	case MrbTTSClass:
		return "SClass"
	case MrbTTProc:
		return "Proc"
	case MrbTTArray:
		return "Array"
	case MrbTTHash:
		return "Hash"
	case MrbTTString:
		return "String"
	case MrbTTRange:
		return "Range"
	case MrbTTException:
		return "Exception"
	case MrbTTEnv:
		return "Env"
	case MrbTTCData:
		return "CData"
	case MrbTTFiber:
		return "Fiber"
	case MrbTTStruct:
		return "Struct"
	case MrbTTIStruct:
		return "IStruct"
	case MrbTTBreak:
		return "Break"
	case MrbTTComplex:
		return "Complex"
	case MrbTTRational:
		return "Rational"
	case MrbTTBigInt:
		return "Bigint"
	default:
		return fmt.Sprintf("(unknown type %v)", v)
	}
}
