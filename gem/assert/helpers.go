package assert

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/oruby/oruby"
)

// Expect is simple testing function which raises error if condition is not met
func Expect(t *testing.T, to bool, eformat string, args ...interface{}) {
	t.Helper()
	if !to {
		t.Errorf(eformat, args...)
	}
}

func isIncluded(v1 interface{}, in ...interface{}) bool {
	for _, v2 := range in {
		if reflect.DeepEqual(toInterface(v1), toInterface(v2)) {
			return true
		}
	}

	if len(in) == 1 {
		if v := reflect.ValueOf(in[0]); v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				if reflect.DeepEqual(toInterface(v1), toInterface(v.Index(i).Interface())) {
					return true
				}
			}
		}
	}

	return false
}

// Include expects item to be included in
func Include(t *testing.T, v1 interface{}, in ...interface{}) {
	t.Helper()
	if !isIncluded(v1, in...) {
		t.Errorf("Expected '%v' to be in %v", v1, in)
	}
}

// NotInclude expects item to NOT be included in
func NotInclude(t *testing.T, v1 interface{}, in ...interface{}) {
	t.Helper()
	if isIncluded(v1, in...) {
		t.Errorf("Expected '%v' NOT to be in %v", v1, in)
	}
}

func toInterface(v interface{}) interface{} {
	switch t := v.(type) {
	case oruby.RValue:
		return t.Interface()
	case oruby.Value:
		switch t.Type() {
		case oruby.MrbTTString:
			return t.String()
		case oruby.MrbTTFixnum:
			return t.Int()
		case oruby.MrbTTFalse:
			if t.IsNil() {
				return nil
			}
			return false
		case oruby.MrbTTTrue:
			return true
		case oruby.MrbTTCptr:
			return t.Uintptr()
		}
		return t
	case oruby.MrbValue:
		return toInterface(t.Value())
	}
	return v
}

// ExpectEql expects both arguments to be equal
// Internaly uses reflection.DeepEqual to perform test
func Equal(t *testing.T, v1, v2 interface{}) {
	t.Helper()

	if tmp, ok := v2.(oruby.RValue); ok {
		v2 = tmp.Interface()
	}

	Expect(t, reflect.DeepEqual(toInterface(v1), toInterface(v2)), "\nExpected '%v' \nto equal '%v'", v1, v2)
}

// True  expects argument to be true
func True(t *testing.T, v1 interface{}) {
	t.Helper()
	Expect(t, reflect.DeepEqual(toInterface(v1), true), "Expected '%v' to be true", v1)
}

// False  expects argument to be true
func False(t *testing.T, v1 interface{}) {
	t.Helper()
	Expect(t, reflect.DeepEqual(toInterface(v1), false), "Expected '%v' to be false", v1)
}

// Nil should be used to check returned Go error.
func Nil(t *testing.T, i error, eformat string, args ...interface{}) {
	t.Helper()
	Expect(t, i == nil, eformat, args...)
}

// NilError should be used to check returned Go error.
// Test fails if there is error.
func NilError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
}

// Error expects Go error to be non-nil
func Error(t *testing.T, i error, eformat string, args ...interface{}) {
	t.Helper()
	Expect(t, i != nil, eformat, args...)
}

// Is64bit function returns true if go runtime is 64 bit
func Is64bit() bool {
	return strings.Contains(runtime.GOARCH, "64")
}
