package assert

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Expect is simple testing function which raises error if condition is not met
func Expect(t *testing.T, to bool, eformat string, args ...interface{}) {
	t.Helper()
	if !to {
		t.Errorf(eformat, args...)
	}
}

// Expect is simple testing function which raises error if condition is not met
func Include(t *testing.T, v1 interface{}, in ...interface{}) {
	t.Helper()

	for _,v2 := range in {
		if reflect.DeepEqual(v1, v2) {
			return
		}
	}
	t.Errorf("Expected '%v' to be in %v", v1, in)
}

// ExpectEql expects both arguments to be equal
// Internaly uses reflection.DeepEqual to perform test
func Equal(t *testing.T, v1, v2 interface{}) {
	t.Helper()
	Expect(t, reflect.DeepEqual(v1, v2), "Expected '%v' to equal '%v'", v1, v2)
	//expect(t, (v1 == v2), "Expected %v to equal %v", v1, v2)
}

// EqualE
func EqualE(t *testing.T, v1, err error, v2 interface{}) {
	t.Helper()
	NilError(t, err)
	Expect(t, reflect.DeepEqual(v1, v2), "Expected '%v' to equal '%v'", v1, v2)
	//expect(t, (v1 == v2), "Expected %v to equal %v", v1, v2)
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
func Is64bit() bool { return strings.Contains(runtime.GOARCH, "64") }
