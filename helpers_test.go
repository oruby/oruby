package oruby

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Pending is used to singnal that test is waiting to be coded
func Pending(t *testing.T, s string) {
	t.Helper()
	t.Log("Pending: ", s)
}

// Expect is simple testing function which raises error if condition is not met
func Expect(t *testing.T, condition bool, eformat string, args ...interface{}) {
	t.Helper()
	if !condition {
		t.Errorf(eformat, args...)
	}
}

// ExpectEql expects both arguments to be equal
// Internaly it uses reflection.DeepEqual to perform test
func ExpectEql(t *testing.T, v1, v2 interface{}) {
	t.Helper()
	Expect(t, reflect.DeepEqual(v1, v2), "Expected '%v' to equal '%v'", v1, v2)
	//expect(t, (v1 == v2), "Expected %v to equal %v", v1, v2)
}

// ExpectNil should be used to check returned Go error.
// SecondValue fails if there is error.
func ExpectNil(t *testing.T, i error, eformat string, args ...interface{}) {
	t.Helper()
	Expect(t, i == nil, eformat, args...)
}

// ExpectNil should be used to check returned Go error.
// Test fails if there is error.
func ExpectNilError(t *testing.T, i error) {
	t.Helper()
	Expect(t, i == nil, "Error: %v", i)
}

// ExpectErr should be used to chach if Go error is raised.
// SecondValue fails if there is no error.
func ExpectErr(t *testing.T, i error, eformat string, args ...interface{}) {
	t.Helper()
	Expect(t, i != nil, eformat, args...)
}

// Support function which returns true if go runtime is 64 bit.
func Go64bit() bool { return strings.Contains(runtime.GOARCH, "64") }
