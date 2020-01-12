package oruby

// #include "go-mrb.h"
import "C"
import "fmt"


func (obj RObject) p() *C.struct_RObject { return (*C.struct_RObject)(C._mrb_ptr(obj.v)) }

// Dup duplicates object
func (obj RObject) Dup() RObject {
	return RObject{C.mrb_obj_dup(obj.mrb.p, obj.v), obj.mrb}
}

// Freeze freeze value
func (obj RObject) Freeze() RObject {
	return RObject{C.mrb_obj_freeze(obj.mrb.p, obj.v), obj.mrb}
}

// ID returns ruby object id
func (obj RObject) ID() int { return int(C.mrb_obj_id(obj.v)) }

// Classname returns class name of object
func (obj RObject) Classname() string {
	return C.GoString(C.mrb_obj_classname(obj.mrb.p, obj.v))
}

// Class returns class of object
func (obj RObject) Class() RClass {
	return RClass{C.mrb_obj_class(obj.mrb.p, obj.v), obj.mrb}
}

// Check if object kind
func (obj RObject) IsKindOf(c RClass) bool {
	result, err := obj.mrb.try(func() C.mrb_value {
		return C.mrb_bool_value(C.mrb_obj_is_kind_of(obj.mrb.p, obj.v, c.p))
	})
	return (err == nil) && (result.Type() != C.MRB_TT_FALSE)
}

// Inspect object
func (obj RObject) Inspect() RString {
	return RString{RObject{
		C.mrb_obj_inspect(obj.mrb.p, obj.v),
		obj.mrb,
	}}
}

// Clone shallow object
func (obj RObject) Clone() RObject {
	return RObject{C.mrb_obj_clone(obj.mrb.p, obj.v), obj.mrb}
}

// RespondTo checks if object responds to method id
func (obj RObject) RespondTo(mid MrbSym) bool {
	return C.mrb_respond_to(obj.mrb.p, obj.v, C.mrb_sym(mid)) != 0
}

// IsInstanceOf checks if oruby object is direct instance of class
func (obj RObject) ObjIsInstanceOf(klass RClass) bool {
	return C.mrb_obj_is_instance_of(obj.mrb.p, obj.v, klass.p) != 0
}

// Call oruby function, return Go interface,
// in case of error, Call returns NilValue and the error is in mrb.Err()
func (obj RObject) Call(name string, args ...interface{}) RObject {
	return RObject{obj.mrb.Call(obj, name, args...).v, obj.mrb}
}

// Funcall oruby function, return Go interface,
func (obj RObject) Funcall(name string, args ...interface{}) (RObject, error) {
	result, err := obj.mrb.Funcall(obj, obj.mrb.Intern(name), args...)
	if err != nil {
		return RObject{C.mrb_nil_value(), obj.mrb}, err
	}
	return RObject{result.v, obj.mrb}, err
}

// InstanceVariables list
func (obj RObject) InstanceVariables() RArray {
	return ary(C.mrb_obj_instance_variables(obj.mrb.p, obj.v), obj.mrb)
}

// IVGet get instance variable
func (obj RObject) IVGet(sym MrbSym) Value {
	return Value{C.mrb_iv_get(obj.mrb.p, obj.v, C.mrb_sym(sym))}
}

// IVSet set instance variable
func (obj RObject) IVSet(sym MrbSym, v MrbValue) error {
	return obj.mrb.tryE(func() {
		C.mrb_iv_set(obj.mrb.p, obj.Value().v, C.mrb_sym(sym), v.Value().v)
	})
}

// SetIV set instance variable as string
func (obj RObject) SetIV(name string, v interface{}) {
	_ = obj.IVSet(obj.mrb.Intern(name), obj.mrb.Value(v))
}

// IVGet get instance variable
func (obj RObject) GetIV(name string) RObject {
	return RObject{obj.IVGet(obj.mrb.Intern(name)).v, obj.mrb}
}

// IVDefined instance variable defined
func (obj RObject) IVDefined(sym MrbSym) bool {
	return C.mrb_iv_defined(obj.mrb.p, obj.v, C.mrb_sym(sym)) != 0
}

// IVRemove remove instance variable
func (obj RObject) IVRemove(sym MrbSym) Value {
	return Value{C.mrb_iv_remove(obj.mrb.p, obj.v, C.mrb_sym(sym))}
}

// String return object value as string
func (obj RObject) Data() interface{} {
	return obj.mrb.Data(obj)
}

// String return object value as string
func (obj RObject) String() string {
	return obj.mrb.String(obj.Value())
}

// Int return value as string
func (obj RObject) Int() int {
	result, err := obj.mrb.Integer(obj.Value())
	if err != nil {
		panic(err)
	}
	return MrbFixnum(result)
}

// Int return value as string
func (obj RObject) Float64() float64 {
	result, err := obj.mrb.Float(obj.Value())
	if err != nil {
		panic(err)
	}
	return MrbFloat(result)
}

// Int return value as string
func (mrb *MrbState) TypeName(v MrbValue) string {
	switch v.Type() {
	case MrbTTFalse:
		if v.IsNil() {
			return "Nil"
		}
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
	case MrbTTFile:
		return "File"
	case MrbTTEnv:
		return "Env"
	case MrbTTData:
		return "Data"
	case MrbTTFiber:
		return "Fiber"
	case MrbTTIStruct:
		return "IStruct"
	case MrbTTBreak:
		return "Break"
	default:
		return fmt.Sprintf("(unknown type %v)", v.Type())
	}
}
