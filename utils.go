package oruby

import "C"
import (
	"errors"
	"fmt"
	"reflect"
	"unicode"
)

// SnakeCase converts a string into Ruby standard snake_case
// ideas based on stoewer/go-strcase, but using unicode package
func SnakeCase(s string) string {
	buffer := make([]rune, 0, len(s)+5)

	var prev rune
	var curr rune
	for _, next := range s {
		if unicode.IsUpper(curr) {
			if unicode.IsLower(prev) || (prev != 0 && unicode.IsUpper(prev) && unicode.IsLower(next)) {
				buffer = append(buffer, '_')
			}
			buffer = append(buffer, unicode.ToLower(curr))
		} else if curr != 0 {
			buffer = append(buffer, curr)
		}
		prev = curr
		curr = next
	}

	if len(s) > 0 {
		if unicode.IsUpper(curr) && unicode.IsLower(prev) && prev != 0 {
			buffer = append(buffer, '_')
		}
		buffer = append(buffer, unicode.ToLower(curr))
	}

	return string(buffer)
}

// CamelCase converts underscore delimited string to CamelCase
func CamelCase(s string) string {
	buffer := make([]rune, 0, len(s))

	var prev rune
	for _, curr := range s {
		if curr != '_' {
			if (prev == '_') || (prev == 0) {
				buffer = append(buffer, unicode.ToUpper(curr))
			} else {
				buffer = append(buffer, unicode.ToLower(curr))
			}
		}
		prev = curr
	}

	return string(buffer)
}

func reflectErr(err error) []reflect.Value {
	return []reflect.Value{reflect.ValueOf(err)}
}

func (mrb *MrbState) callFunc(fn reflect.Value, args Arguments) []reflect.Value {
	var err error
	// Check function
	if fn.Kind() != reflect.Func {
		return reflectErr(errors.New("constructor failed to provide Go value"))
	}
	ft := fn.Type()

	argc := args.Len()
	if ft.NumIn() > argc {
		argc = ft.NumIn()
	}
	params := make([]reflect.Value, argc)
	variadic := 0
	if ft.IsVariadic() {
		variadic = 1
	}

	for i := argc-1; i >= 0; i-- {
		// Check variadic arguments
		if i >= ft.NumIn() {
			// Skip extra value if not variadic
			if variadic == 0 {
				continue
			}
			arg := args.Get(mrb, i)
			params[i] = reflect.ValueOf(arg)
			continue
		}

		inType := ft.In(i)

		// Allow pointed arguments to allow nil as optional value
		if i >= args.Len() {
			if inType.Kind() != reflect.Ptr {
				return reflectErr(fmt.Errorf("expected %v parameters, supplied %v", ft.NumIn(), argc))
			}
			params[i] = reflect.Zero(inType)
			continue
		}

		arg := args.Get(mrb, i)
		params[i], err = assignValue(arg, inType)
		if err != nil {
			return reflectErr(err)
		}
	}

	// Call
	return fn.Call(params)
}

func assignValue(arg interface{}, outType reflect.Type) (reflect.Value, error) {
	argValue := reflect.ValueOf(arg)

	if arg == nil {
		return reflect.Zero(outType), nil
	} else if argValue.Type().ConvertibleTo(outType) {
		return argValue.Convert(outType), nil
	} else if argValue.Type().ConvertibleTo(outType.Elem()) {
		v := reflect.New(outType.Elem())
		reflect.Indirect(v).Set(argValue.Convert(outType.Elem()))
		return v, nil
	}

	return reflect.Zero(outType), EArgumentError("value %v cannot be converted to type %v", arg, outType)
}

func (mrb *MrbState) handleResults(result []reflect.Value) (Value, error) {
	lenres := len(result)

	// Handle results
	if lenres == 0 {
		return nilValue, nil
	}

	// By Go convention, error is returned as last result. Like func X() (int, error)
	// When X() function is converted to oruby, it will return only one int result,
	// but if there is an error - it will be raised
	if result[lenres-1].Kind() == reflect.Interface {
		if result[lenres-1].Type() == reflect.TypeOf((*error)(nil)).Elem() {
			// Last result value is error interface
			er := result[lenres-1].Interface()
			if er != nil {
				return nilValue, er.(error)
			}

			// If no error returned, that last return value is ignored
			lenres--
		}
	}

	switch lenres {
	case 0:
		// No results - return nil value
		return nilValue, nil

	case 1:
		// One result - return one Value
		return mrb.valueValue(result[0]), nil
	}

	// Multiple results - return RArray
	out := mrb.AryNewCapa(lenres)
	for _, v := range result[:lenres-1] {
		mrb.AryPush(out, mrb.valueValue(v))
	}

	return out.Value(), nil
}

func toMapSI(mrb *MrbState, v Value) map[string]interface{} {
	if !v.IsHash() {
		panic("hash expected, got " + mrb.TypeName(v))
	}
	keys := mrb.HashKeys(v)
	keyCount := keys.Len()
	ret  := make(map[string]interface{}, keyCount)

	for i := 0; i < keyCount; i++ {
		key := keys.Item(i)
		val := mrb.HashGet(v, key)
		ret[mrb.String(key)] = mrb.Intf(val)
	}
	return ret
}

func toMapII(mrb *MrbState, v Value) map[interface{}]interface{} {
	if !v.IsHash() {
		panic("hash expected, got " + mrb.TypeName(v))
	}
	keys := mrb.HashKeys(v)
	keyCount := keys.Len()
	ret  := make(map[interface{}]interface{}, keyCount)

	for i := 0; i < keyCount; i++ {
		key := keys.Item(i)
		val := mrb.HashGet(v, key)
		ret[mrb.Intf(key)] = mrb.Intf(val)
	}
	return ret
}