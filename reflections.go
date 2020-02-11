package oruby

import "C"
import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unsafe"
)

func (c RClass) attrGetter(idx int) MrbFuncT {
	return func(mrb *MrbState, self Value) MrbValue {
		strct := mrb.DataCheckGetInterface(self)
		v := reflect.ValueOf(strct).Elem().Field(idx)
		return c.mrb.valueValue(v)
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
			if (argType != nil) && (argType == classType) {
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
		args := mrb.GetArgs()
		argc := args.Len()

		// Special constructor - from go pointer
		if argc == 1 {
			argValue := args.Item(0)
			if argValue.Type() == MrbTTData {
				in := mrb.DataGetInterface(argValue)
				argType := reflect.TypeOf(in)
				if (argType != nil) && (argType == classType) {
					mrb.DataSetInterface(self, in)
					return afterInit(mrb, self)
				}
			}
		}

		result := mrb.callFunc(fn, args)

		// Check error
		if len(result) > 0 && result[len(result)-1].Type() == reflect.TypeOf((*error)(nil)).Elem() {
			err := result[len(result)-1].Interface()
			if err != nil {
				return mrb.getErrorKlass(err.(error)).Raisef("%v.init_go : %v", mrb.ClassOf(self).Name(), err)
			}
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

	switch v.Kind() {
	case reflect.Func:
		// Constructor is function
		if v.NumOut() == 0 || v.Out(0).Kind() != reflect.Ptr {
			panic("constructor must return pointer to type as first return value")
		}

		opt := 0
		for i := v.NumIn() - 1; i >= 0; i--  {
			if v.In(i).Kind() != reflect.Ptr {
				break
			}
			opt++
		}

		aspec := ArgsArg(v.NumIn()-opt, opt)
		v = v.Out(0)

		// define init_go method so that initialize could be redefined
		c.DefineMethod("init_go", initGo(v, reflect.ValueOf(constructor)), aspec)
	case reflect.Ptr, reflect.Struct, reflect.Interface:
		// Constructor is pointer to value
		// define init_go method so that initialize could be redefined
		c.DefineMethod("init_go", initGoValue(v), ArgsReq(1))
	default:
		panic("constructor does not return pointer to Go type")
	}

	// Set klass type as RData
	MrbSetInstanceTT(c, MrbTTData)

	// Connect with Go world
	c.mrb.Lock()
	c.mrb.classmap[v] = unsafe.Pointer(c.p)
	c.mrb.hooks[unsafe.Pointer(c.p)] = v
	c.mrb.Unlock()
}

func (c RClass) AttachType(zeroType interface{}) {
	t := reflect.TypeOf(zeroType)
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr, reflect.UnsafePointer,
		reflect.Float32, reflect.Float64:
		panic("simple types (Ints, Bool, Floats, Pointers) are not supported")
	case reflect.String:
		panic("string type is not supported as attached type")
	}

	// Connect with Go world
	c.mrb.Lock()
	c.mrb.classmap[t] = unsafe.Pointer(c.p)
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

// Scan oruby value to pointed variable
func (mrb *MrbState) Scan(o MrbValue, in interface{}) (err error) {
	defer errorHandler(&err)

	v := reflect.ValueOf(in)
	if v.Kind() != reflect.Ptr {
		return errors.New("scaned interface must be pointer to value")
	}

	return mrb.scanValue(o, v.Elem())
}

// Scan oruby value to pointed variable
func (mrb *MrbState) scanValue(o MrbValue, vel reflect.Value) (err error) {
	velType := vel.Type()

	// generic interface
	if velType.Kind() == reflect.Interface && velType.NumMethod() == 0 {
		vel.Set(reflect.ValueOf(mrb.Intf(o)))
		return nil
	}

	if velType.Kind() == reflect.String {
		s := reflect.ValueOf(mrb.String(o))
		vel.Set(s)
		return nil
	}

	if velType.Kind() == reflect.Bool {
		truthy := o.Type() > MrbTTFalse
		s := reflect.ValueOf(truthy)
		vel.Set(s)
		return nil
	}

	switch o.Type() {
	case MrbTTArray:
		{
			if velType.Kind() != reflect.Slice {
				return errors.New("slice type required")
			}

			var f func(MrbValue) reflect.Value

			switch velType.Elem().Kind() {
			case reflect.String:
				f = func(v MrbValue) reflect.Value { return reflect.ValueOf(mrb.String(o)) }
			case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int8,
				reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint8:
				f = func(v MrbValue) reflect.Value {
					in, _ := mrb.Integer(o)
					return reflect.ValueOf(MrbFixnum(in)).Convert(velType.Elem())
				}
			default:
				f = func(v MrbValue) reflect.Value { return reflect.ValueOf(mrb.Intf(o)).Convert(velType.Elem()) }
			}

			for i := 0; i < RArrayLen(o); i++ {
				av := mrb.AryRef(o, i)
				reflect.Append(vel, f(av))
			}

			return nil
		}

	case MrbTTHash:
		{
			keys := mrb.HashKeys(o)
			kcnt := RArrayLen(keys)

			// Unmarshall to map
			switch velType.Kind() {
			case reflect.Map:
				switch m := vel.Interface().(type) {
				case map[string]interface{}:
					for i := 0; i < kcnt; i++ {
						key := mrb.AryRef(keys, i)
						val := mrb.HashGet(o, key)
						m[mrb.String(key)] = mrb.Intf(val)
					}

				case map[interface{}]interface{}:
					for i := 0; i < kcnt; i++ {
						key := mrb.AryRef(keys, i)
						val := mrb.HashGet(o, key)
						m[mrb.Intf(key)] = mrb.Intf(val)
					}

				default:
					return errors.New("unmarshaling maps is supported to [string]interface{} or [interface{}]interface{}")
				}

			case reflect.Struct:

				for i := 0; i < kcnt; i++ {
					key := mrb.AryRef(keys, i)
					val := mrb.HashGet(o, key)
					field := vel.FieldByName(mrb.String(key))

					if field.IsValid() && field.CanSet() && field.Type().PkgPath() == "" {
						field.Set(reflect.ValueOf(mrb.Intf(val)).Convert(field.Type()))
					}
				}

			default:
				return errors.New("supported types for hash scans are Go maps with string or interface keys and structs, not " + velType.Kind().String())
			}

			return nil
		}

	case MrbTTObject:
		{
			unmarshalSym := mrb.Intern("unmarshal")
			if !mrb.MethodExists(mrb.ClassOf(o), unmarshalSym) {
				return errors.New("oruby Object does not support unmarshaling")
			}

			_, err = mrb.Funcall(o, unmarshalSym, vel.Interface())
			return err
		}
	case MrbTTData:
		{
			data := reflect.ValueOf(mrb.Data(o))
			if velType == data.Type() {
				vel.Set(data)
				return nil
			}

			if mrb.ObjIsKindOf(o, mrb.ClassGet("Time")) && (velType == reflect.TypeOf(time.Time{})) {
				vtoi := mrb.Call(o, "to_i")
				vusec := mrb.Call(o, "usec")
				t := time.Unix(int64(MrbFixnum(vtoi)), int64(MrbFixnum(vusec))*1000)
				vel.Set(reflect.ValueOf(t))
				return nil
			}

			unmarshalSym := mrb.Intern("unmarshal")
			if mrb.MethodExists(mrb.ClassOf(o), unmarshalSym) {
				_, err = mrb.Funcall(o, unmarshalSym, vel.Interface())
				return err
			}

			return fmt.Errorf("unsupported interface '%v' for class '%v'", data.Type(), mrb.ClassOf(o).Name())
		}

	//case C.MRB_TT_UNDEF,
	//case C.MRB_TT_FLOAT, C.MRB_TT_FIXNUM, C.MRB_TT_CPTR:
	//case C.MRB_TT_CLASS, C.MRB_TT_MODULE, C.MRB_TT_SCLASS:
	//case C.MRB_TT_PROC:
	//case C.MRB_TT_SYMBOL:

	default:
		govalue := mrb.Intf(o)
		vel.Set(reflect.ValueOf(govalue).Convert(velType))
	}
	return nil
}

