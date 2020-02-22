package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"unsafe"
)

// Error interface for class, usable for oruby Exceptions as Go Errors
func (c RClass) Error() error {
	if c.InstanceTT() == MrbTTException {
		return errors.New(c.Name())
	}
	return nil
}

// RObject returns RObject for MrbValue interface
func (mrb *MrbState) RObject(v MrbValue) RObject {
	return RObject{v.Value().v, mrb}
}

// Super returns super class
func (c RClass) Super() RClass { return RClass{c.p.super, c.mrb} }

// Mrb returns oruby state
func (c RClass) Mrb() *MrbState { return c.mrb }

// InstanceTT returns class instance type
func (c RClass) InstanceTT() int { return int(C._MRB_INSTANCE_TT(c.p)) }

// Real returns top parent class
func (c RClass) Real() RClass { return RClass{C.mrb_class_real(c.p), c.mrb} }

// ClassPath returns class path
func (c RClass) ClassPath() Value { return Value{C.mrb_class_path(c.mrb.p, c.p)} }

// New creates new object instance
func (c RClass) New(args ...interface{}) (RObject, error) {
	obj, err := c.mrb.ObjNew(c, args...)
	if err != nil {
		return RObject{}, err
	}

	return RObject{obj.Value().v, c.mrb}, nil
}

// NewInstance creates new object instance, panics on error
func (c RClass) NewInstance(args ...interface{}) Value {
	argv := make([]C.mrb_value, len(args)+1)
	for i := range args {
		argv[i] = c.mrb.Value(args[i]).v
	}
	v, err := c.mrb.try(func() C.mrb_value {
		return C.mrb_obj_new(
			c.mrb.p,
			c.p,
			C.mrb_int(len(args)),
			&argv[0],
		)
	})

	runtime.KeepAlive(argv)

	if err != nil {
		panic(err)
	}

	return v
}

// DataValue converts go struct or interface to ruby Value
// if first checks if obj implements MrbValue or ValueMigrator interface
// then it checks if obj type is regestered in ruby
// it then creates oruby object
func (mrb *MrbState) DataValue(obj interface{}) Value {
	mrb.Lock()
	clsptr := mrb.classmap[reflect.TypeOf(obj)]
	mrb.Unlock()

	// no registered class - return data
	if clsptr == nil {
		return mrb.DataWrapInterface(mrb.ObjectClass(), obj).Value()
	}

	// there is registered Go class for this interface
	klass := RClass{(*C.struct_RClass)(clsptr), mrb}
	v := mrb.DataWrapInterface(klass, obj).Value()

	// call "after_init" if defined
	if mrb.MethodExists(klass, mrb.afterInitSym) {
		_, _ = mrb.Funcall(v, mrb.afterInitSym)
	}

	return v
}

// NewGoInstance creates new object instance for existing Go object
// it skips "initialize" method, but calls "after_init" so IVs could be set
func (c RClass) NewGoInstance(obj interface{}) (Value, error) {
	mrbObjectClass := c.mrb.p.object_class
	// For non-plain ruby objects, check if class is registered with Go
	if c.p != mrbObjectClass {
		c.mrb.Lock()
		klass := c.mrb.classmap[reflect.TypeOf(obj)]
		c.mrb.Unlock()

		if klass != unsafe.Pointer(c.p) {
			v := mrbObjValue(klass)
			return c.mrb.nilValue, fmt.Errorf(
				"creation failed, expected type for %v class but got %v", c.Name(), c.mrb.ClassPtr(v).Name(),
			)
		}
	}

	// Create data
	v := c.mrb.DataWrapInterface(c, obj).Value()

	// call "after_init" if defined
	var err error
	if c.p != mrbObjectClass && c.mrb.MethodExists(c, c.mrb.afterInitSym) {
		_, err = c.mrb.Funcall(v, c.mrb.afterInitSym)
	}

	return v, err
}

// Data return underlying Go value of oruby object
func (mrb *MrbState) Data(obj MrbValue) interface{} {
	return mrb.DataCheckGetInterface(obj)
}

// Alias creates method alias
func (c RClass) Alias(a, b MrbSym) {
	C.mrb_alias_method(c.mrb.p, c.p, C.mrb_sym(a), C.mrb_sym(b))
}

// DefineAlias for existing method in class
func (c RClass) DefineAlias(name1, name2 string) {
	c.mrb.DefineAlias(c, name1, name2)
}

// Include a module in another class or module.
func (c RClass) Include(module RClass) {
	C.mrb_include_module(c.mrb.p, c.p, module.p)
}

// Prepend a module in another class or module.
func (c RClass) Prepend(module RClass) {
	C.mrb_prepend_module(c.mrb.p, c.p, module.p)
}

// Name returns name of oruby class
func (c RClass) Name() string {
	cstr := C.mrb_class_name(c.mrb.p, c.p)
	return C.GoString(cstr)
}

// Const creates new oruby class const
func (c RClass) Const(name string, value interface{}) {
	c.mrb.DefineConst(c, name, c.mrb.Value(value))
}

// ConstDefined check if const is defined in module/class
// checks also super modules classes
func (c RClass) ConstDefined(name string) bool {
	return c.mrb.ConstDefined(c, c.mrb.Intern(name))
}

// ConstDefinedID check if const is defined in module/class
// check also super modules classes
func (c RClass) ConstDefinedID(id MrbSym) bool {
	return c.mrb.ConstDefined(c, id)
}

// ConstDefinedAt check if const is defined in module/class
// does not checks super modules/classes
func (c RClass) ConstDefinedAt(name string) bool {
	return c.mrb.ConstDefinedAt(c, c.mrb.Intern(name))
}

// ConstDefinedIDAt check if const is defined in module/class
// does not checks super modules/classes
func (c RClass) ConstDefinedIDAt(id MrbSym) bool {
	return c.mrb.ConstDefinedAt(c, id)
}

// ConstGet returns oruby class const by name
func (c RClass) ConstGet(name string) Value {
	return c.mrb.ConstGet(c, c.mrb.Intern(name))
}

// ConstGetID returns oruby class const by symbol id
func (c RClass) ConstGetID(id MrbSym) Value {
	return c.mrb.ConstGet(c, id)
}

// DefineClassMethod defines class method
func (c RClass) DefineClassMethod(name string, f MrbFuncT, count MrbAspec) {
	c.mrb.DefineClassMethod(c, name, f, count)
}

// DefineClassFunc defines class method from Go function
func (c RClass) DefineClassFunc(name string, f interface{}) {
	c.mrb.DefineClassFunc(c, name, f)
}

// DefF defines Go function as method. Supports following calls:
//   class.DefF(SomeGoFunction) -> equals to def some_go_function(arg1, arg2...)
func (c RClass) DefF(f interface{}) {
	v := reflect.ValueOf(f)
	name := SnakeCase(v.Type().Name())
	if name == "" {
		panic("you must name annonymous func: ...Def(\"func_name\", func(){ return 1 })")
	}
	c.mrb.DefineMethodFuncID(c, c.mrb.Intern(name), f)
	return
}

// UndefMethod removes method from oruby class
func (c RClass) UndefMethod(name string) {
	c.mrb.UndefMethod(c, name)
}

// UndefClassMethod removes method from oruby class
func (c RClass) UndefClassMethod(name string) {
	c.mrb.UndefClassMethod(c, name)
}

// DefineMethod on class via MrbFuncT type function
func (c RClass) DefineMethod(name string, f MrbFuncT, params MrbAspec) {
	c.mrb.DefineMethod(c, name, f, params)
}

// Def define method. Supports following calls:
//   class.Def(\"func_name\", SomeGoFunction)
//   class.Def(\"func_name\", func(){ return 1 })
//   class.Def(\"func_name\", MrbMethodT function, MrbAspec) -> alias for DefineMethod
func (c RClass) Def(name string, f interface{}, args ...interface{}) {
	mID := c.mrb.Intern(name)
	if mrbFunc, ok := f.(func(*MrbState, Value) MrbValue); ok {
		if len(args) == 0 {
			panic("type MrbFuncT reqires MrbAspec parameter: ...Def(\"f\", mrbFunc, mrb.ArgsNone())")
		}
		c.mrb.DefineMethodID(c, mID, mrbFunc, args[0].(MrbAspec))
		return
	}

	c.mrb.DefineMethodFuncID(c, mID, f)
}

// DefineMethodFunc defines function
func (mrb *MrbState) DefineMethodFunc(klass RClass, name string, f interface{}) {
	mrb.DefineMethodFuncID(klass, mrb.Intern(name), f)
}

// DefineModuleFunction defines module function
func (c RClass) DefineModuleFunction(name string, f MrbFuncT, params MrbAspec) {
	c.mrb.DefineModuleFunction(c, name, f, params)
}

// DefineModuleFunc defines module Go function
func (c RClass) DefineModuleFunc(name string, f interface{}) {
	c.mrb.DefineModuleFunc(c, name, f)
}

// DefineClassUnder defines class under module or class, descending from super
func (c RClass) DefineClassUnder(name string, super RClass) RClass {
	return c.mrb.DefineClassUnder(c, name, super)
}

// Call shortcut for mrb.Call(klass, method, args)
func (c RClass) Call(name string, args ...interface{}) Value {
	return c.mrb.Call(c, name, args...)
}

// AttrReader creates getter method for instance variable with same name.
// for example obj.AttrReader("name") defines method obj.name which returns value of @name
func (c RClass) AttrReader(name string) {
	c.mrb.DefineMethod(c, name, func(mrb *MrbState, self Value) MrbValue {
		return mrb.IVGet(self, mrb.Intern("@"+name))
	}, ArgsNone())
}

// AttrWriter creates setter method for instance variable with same name.
// for example obj.AttrWriter("name") defines method obj.name= which sets value of @name
func (c RClass) AttrWriter(name string) {
	c.mrb.DefineMethod(c, name+"=", func(mrb *MrbState, self Value) MrbValue {
		v := mrb.GetArgsFirst()
		_ = mrb.IVSet(self, mrb.Intern("@"+name), v)
		return v
	}, ArgsReq(1))
}

// AttrAccessor creates both setter and getter methods for instance variable with same name.
// for example obj.AttrAccessor("name") defines methods obj.name and obj.name= which
// gets and sets value of @name
func (c RClass) AttrAccessor(name string) {
	c.AttrReader(name)
	c.AttrWriter(name)
}
