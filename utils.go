package oruby

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

func (mrb *MrbState) callFunc(fn reflect.Value, args RArray) ([]reflect.Value, error) {
	// Check function
	if fn.Kind() != reflect.Func {
		return nil, errors.New("constructor failed to provide Go value")
	}

	argc := args.Len()
	if argc < fn.Type().NumIn() {
		return nil, fmt.Errorf("expected %v parameters, supplied %v", fn.Type().NumIn(), argc)
	}

	params := make([]reflect.Value, argc)
	for i := 0; i < fn.Type().NumIn(); i++ {
		arg := mrb.Intf(args.Item(i))
		params[i] = reflect.ValueOf(arg).Convert(fn.Type().In(i))
	}

	// Call
	var result []reflect.Value
	if fn.Type().IsVariadic() {
		result = fn.CallSlice(params)
	} else {
		result = fn.Call(params)
	}

	// Check error
	if len(result) > 0 && result[len(result)-1].Type() == reflect.TypeOf((*error)(nil)).Elem() {
		err := result[len(result)-1].Interface()
		if err != nil {
			return result, err.(error)
		}
	}

	return result, nil
}
