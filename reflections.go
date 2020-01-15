package oruby

import "C"
import (
	"reflect"
	"strings"
	"unsafe"
)

func (c RClass) attrGetter(idx int) MrbFuncT {
	return func(mrb *MrbState, self Value) MrbValue {
		strct := mrb.DataCheckGetInterface(self)
		v := reflect.ValueOf(strct).Elem().Field(idx)
		return c.mrb.valueValue(&v)
	}
}

func (c RClass) attrSetter(idx int) MrbFuncT {
	return func(mrb *MrbState, self Value) MrbValue {
		strct := mrb.DataCheckGetInterface(self)
		field := reflect.ValueOf(strct).Elem().Field(idx)
		v := mrb.GetArgsFirst()
		field.Set(reflect.ValueOf(mrb.Intf(v)).Convert(field.Type()))

		return v
	}
}

// Populate methods and fields from go type to oruby class
func (c RClass) Populate() {
	v, ok := c.mrb.getHook(unsafe.Pointer(c.p)).(reflect.Type)
	if !ok {
		panic("unregistered Go type for ruby class " + c.Name())
	}
	mrb := c.mrb

	// Methods
	for i := 0; i < v.NumMethod(); i++ {
		m := v.Method(i)
		// println("METHOD:", name, m.Name)
		if m.PkgPath == "" {
			sName := SnakeCase(m.Name)
			mID := mrb.Intern(sName)

			mrb.DefineMethodFuncID(c, mID, m.Func.Interface())

			// "is_method" is also aliased to "method?"
			if strings.HasPrefix(sName, "is_") {
				mrb.AliasMethod(c, mrb.Intern(sName[3:]+"?"), mID)
			}

			// "method_bang" is also aliased to "method!"
			if strings.HasSuffix(sName, "_bang") {
				mrb.AliasMethod(c, mrb.Intern(sName[:len(sName)-5]+"!"), mID)
			}

		}
	}

	targetType := v.Elem()

	// Only struct types support fields; Intf type does not.
	if targetType.Kind() == reflect.Struct {
		// Fields
		for i := 0; i < targetType.NumField(); i++ {
			// Only public fields are exposed
			if targetType.Field(i).PkgPath == "" {
				fname := targetType.Field(i).Name
				snakeName := SnakeCase(fname)

				// Method for get field
				c.DefineMethod(snakeName, c.attrGetter(i), ArgsNone())
				c.DefineMethod(snakeName+"=", c.attrSetter(i), ArgsReq(1))
			}
		}
	}
}

func afterInit(mrb *MrbState, self Value) Value {
	if mrb.MethodExists(mrb.ClassOf(self), mrb.afterInitSym) {
		ret, _ := mrb.FuncallWithBlock(self, mrb.afterInitSym)
		if mrb.ObjIsKindOf(ret, mrb.EExceptionClass()) {
			return ret
		}
	}
	return self
}
// initGoValue is special constructor from go pointer
func initGoValue(classType reflect.Type) MrbFuncT {
	return func(mrb *MrbState, self Value) MrbValue {
		argValue := mrb.GetArgsFirst()

		if argValue.Type() == MrbTTData {
			in := mrb.DataGetInterface(argValue)
			argType := reflect.TypeOf(in)
			if (argType != nil) && (argType.Kind() == reflect.Ptr) && (argType == classType) {
				mrb.DataSetInterface(self, in)
				return afterInit(mrb, self)
			}
		}
		return mrb.Raise(mrb.EArgumentError(), "value is not of registered Go type")
	}
}

// initGo is special constructor from go pointer or constructor function
func initGo(classType reflect.Type, fn reflect.Value) MrbFuncT {
	return func(mrb *MrbState, self Value) MrbValue {
		// fetch args
		args := mrb.GetAllArgs()
		argc := args.Len()

		// Special constructor - from go pointer
		if argc == 1 {
			argValue := args.Item(0)
			if argValue.Type() == MrbTTData {
				in := mrb.DataGetInterface(argValue)
				argType := reflect.TypeOf(in)
				if (argType != nil) && (argType.Kind() == reflect.Ptr) && (argType == classType) {
					mrb.DataSetInterface(self, in)
					return afterInit(mrb, self)
				}
			}
		}

		result, err := mrb.callFunc(fn, args)
		if err != nil {
			return mrb.Raisef(mrb.ERuntimeError(), "%v.init_go : %v", mrb.ClassOf(self).Name(), err).Value()
		}

		// Handle results
		if len(result) == 0 {
			return mrb.Raise(mrb.ERuntimeError(), "constructor failed to return Go value")
		}

		// Set object as MrbTTData, with Data reference to Go object interface
		mrb.DataSetInterface(self, result[0].Interface())
		return afterInit(mrb, self)
	}
}

// RegisterGoClass connects go type with oruby class
func (c RClass) RegisterGoClass(constructor interface{}) {
	v := reflect.TypeOf(constructor)
	aspec := ArgsAny()

	switch v.Kind() {
	case reflect.Func:
		// Constructor is function
		if v.NumOut() == 0 || v.Out(0).Kind() != reflect.Ptr {
			panic("constructor must return pointer to type as first return value")
		}

		aspec = ArgsArg(uint32(v.NumIn()), 1)
		v = v.Out(0)

		// define init_go method so that initialize could be redefined
		c.DefineMethod("init_go", initGo(v, reflect.ValueOf(constructor)), aspec)
	case reflect.Ptr:
		// Constructor is pointer to value
		aspec = ArgsReq(1)
		c.DefineMethod("init_go", initGoValue(v), aspec)
	default:
		panic("constructor does not return pointer to Go type")
	}

	// Set klass value as RData
	MrbSetInstanceTT(c, MrbTTData)
	c.mrb.Lock()
	c.mrb.classmap[v] = unsafe.Pointer(c.p)
	c.mrb.hooks[unsafe.Pointer(c.p)] = v
	c.mrb.Unlock()
}

// SetAsGoClass defines Go class in oruby state as oruby class
// it registeres oruby class as RDATA and connects it to Go
// interface returned from constructor.
//
// Underlying Go type can be retreived from custom methods with
//     goSelf := mrb.DataGetInterface(self)
//
// Constuctor must be a function returning pointer to Go type,
// or pointer to Go type.
//
// Constructor function is bounded to 'init_go' method in oruby class
// and is aliased to 'initialize'. If 'initialize' iz rewritten with
// custom Go or ruby method, init_go must be called in new method.
//
// Methods are populated from public methods defined for Go type,
// public fields are populated with getters and setters.
// All method names are snake_cased: SomeMethod -> some_method
func (c RClass) SetAsGoClass(constructor interface{}) {
	c.RegisterGoClass(constructor)
	c.DefineAlias("initialize", "init_go")
	c.Populate()
}

// DefineGoClass defines go class in oruby state
func (mrb *MrbState) DefineGoClass(name string, constructor interface{}) RClass {
	klass := mrb.DefineClass(name, mrb.ObjectClass())
	klass.SetAsGoClass(constructor)
	return klass
}

// DefineGoClassUnder defines go class in oruby state
func (mrb *MrbState) DefineGoClassUnder(outer RClass, name string, constructor interface{}) RClass {
	klass := mrb.DefineClassUnder(outer, name, mrb.ObjectClass())
	klass.SetAsGoClass(constructor)
	return klass
}
