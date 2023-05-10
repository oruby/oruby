package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var errRuntimeError = errors.New("RuntimeError")
var errTypeError = errors.New("TypeError")
var errArgumentError = errors.New("ArgumentError")
var errIndexError = errors.New("IndexError")
var errRangeError = errors.New("RangeError")
var errNameError = errors.New("NameError")
var errNoMethodError = errors.New("NoMethodError")
var errScriptError = errors.New("ScriptError")
var errSyntaxError = errors.New("SyntaxError")
var errLocalJumpError = errors.New("LocalJumpError")
var errRegexpError = errors.New("RegexpError")
var errFrozenError = errors.New("FrozenError")
var errNotImplementedError = errors.New("NotImplementedError")
var errFloatDomainError = errors.New("FloatDomainError")
var errKeyError = errors.New("KeyError")
var errSystemCallError = errors.New("SystemCallError")

// ERuntimeError error helper for Go clases exported to oruby
func ERuntimeError(format string, args ...interface{}) error {
	return Raisef(errRuntimeError, format, args...)
}

// ETypeError error helper for Go clases exported to oruby
func ETypeError(format string, args ...interface{}) error {
	return Raisef(errTypeError, format, args...)
}

// EArgumentError error helper for Go clases exported to oruby
func EArgumentError(format string, args ...interface{}) error {
	return Raisef(errArgumentError, format, args...)
}

// EIndexError error helper for Go clases exported to oruby
func EIndexError(format string, args ...interface{}) error {
	return Raisef(errIndexError, format, args...)
}

// ERangeError error helper for Go clases exported to oruby
func ERangeError(format string, args ...interface{}) error {
	return Raisef(errRangeError, format, args...)
}

// ENameError error helper for Go clases exported to oruby
func ENameError(format string, args ...interface{}) error {
	return Raisef(errNameError, format, args...)
}

// ENoMethodError error helper for Go clases exported to oruby
func ENoMethodError(format string, args ...interface{}) error {
	return Raisef(errNoMethodError, format, args...)
}

// EScriptError error helper for Go clases exported to oruby
func EScriptError(format string, args ...interface{}) error {
	return Raisef(errScriptError, format, args...)
}

// ESyntaxError error helper for Go clases exported to oruby
func ESyntaxError(format string, args ...interface{}) error {
	return Raisef(errSyntaxError, format, args...)
}

// ELocalJumpError error helper for Go clases exported to oruby
func ELocalJumpError(format string, args ...interface{}) error {
	return Raisef(errLocalJumpError, format, args...)
}

// ERegexpError error helper for Go clases exported to oruby
func ERegexpError(format string, args ...interface{}) error {
	return Raisef(errRegexpError, format, args...)
}

// EFrozenError error helper for Go clases exported to oruby
func EFrozenError(format string, args ...interface{}) error {
	return Raisef(errFrozenError, format, args...)
}

// ENotImplementedError error helper for Go clases exported to oruby
func ENotImplementedError(format string, args ...interface{}) error {
	return Raisef(errNotImplementedError, format, args...)
}

// EFloatDomainError error helper for Go clases exported to oruby
func EFloatDomainError(format string, args ...interface{}) error {
	return Raisef(errFloatDomainError, format, args...)
}

// EKeyError error helper for Go clases exported to oruby
func EKeyError(format string, args ...interface{}) error {
	return Raisef(errKeyError, format, args...)
}

// ESystemCallError error helper for Go clases exported to oruby
func ESystemCallError(format string, args ...interface{}) error {
	return Raisef(errSystemCallError, format, args...)
}

// EError error helper for Go clases exported to oruby
func EError(name, format string, args ...interface{}) error {
	return Raisef(errors.New(name), format, args...)
}

// RaiseError containing error type and message
type RaiseError struct {
	err       error
	msg       string
	backtrace string
}

// Raise raises error with type and error message
func Raise(err error, msg string) error {
	return &RaiseError{err: err, msg: msg}
}

// Raisef raises error with type and formated error message
func Raisef(err error, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return Raise(err, msg)
}

// Error implements error interface
func (e *RaiseError) Error() string {
	return e.msg
}

// String implements stringer interface
func (e *RaiseError) String() string {
	return e.msg
}

// Backtrace contains backtrace if provided
func (e *RaiseError) Backtrace() string {
	return e.backtrace
}

// Unwrap returns inner error
func (e *RaiseError) Unwrap() error {
	return e.err
}

func (mrb *MrbState) getErrorKlass(err error) RClass {
	switch err {
	case errRuntimeError:
		return mrb.ERuntimeError()
	case errTypeError:
		return mrb.ETypeError()
	case errArgumentError:
		return mrb.EArgumentError()
	case errIndexError:
		return mrb.EIndexError()
	case errRangeError:
		return mrb.ERangeError()
	case errNameError:
		return mrb.ENameError()
	case errNoMethodError:
		return mrb.ENoMethodError()
	case errScriptError:
		return mrb.EScriptError()
	case errSyntaxError:
		return mrb.ESyntaxError()
	case errLocalJumpError:
		return mrb.ELocalJumpError()
	case errRegexpError:
		return mrb.ERegexpError()
	case errFrozenError:
		return mrb.EFrozenError()
	case errNotImplementedError:
		return mrb.ENotImplementedError()
	case errFloatDomainError:
		return mrb.EFloatDomainError()
	case errKeyError:
		return mrb.EKeyError()
	}

	if e, ok := err.(*RaiseError); ok {
		return mrb.getErrorKlass(e.err)
	}

	if errors.Is(err, &os.SyscallError{}) || os.IsExist(err) || os.IsNotExist(err) ||
		os.IsPermission(err) || os.IsTimeout(err) {
		return mrb.ESystemCallError()
	}

	if _, ok := err.(*os.PathError); ok {
		return mrb.ESystemCallError()
	}

	sep := strings.SplitN(err.Error(), ": ", 2)
	if len(sep) == 0 {
		return mrb.EStandardErrorClass()
	}
	estr := sep[0]

	// Class name should be non-empty, uppercase starting string, without whitespace
	if estr == "" || estr[1] < 'A' || strings.ContainsAny(estr, " \t\r\n") {
		return mrb.EStandardErrorClass()
	}

	eid := mrb.Intern(estr)
	if mrb.ConstDefined(mrb.ObjectClass(), eid) {
		c := mrb.ConstGet(mrb.ObjectClass(), eid)
		if c.Type() != MrbTTClass {
			return mrb.EStandardErrorClass()
		}

		if mrb.ObjIsKindOf(c, mrb.EExceptionClass()) {
			return mrb.ClassOf(c)
		}
	}

	return mrb.EStandardErrorClass()
}

// Raise raises Exception from class
// If class is Exception descendand - itself is raised
// If class is not Exception descendant - Exception class is raised
func (c RClass) Raise(msg string) Value {
	return c.mrb.Raise(c, msg)
}

// Raisef raises formated Exception from class
// If class is Exception descendand - itself is raised
// If class is not Exception descendant - Exception class is raised
func (c RClass) Raisef(format string, args ...interface{}) Value {
	msg := fmt.Sprintf(format, args...)
	return c.Raise(msg)
}

// RaiseError raises Exception with message from error
// If class is Exception descendand - itself is raised
// If class is not Exception descendant - Exception class is raised
func (c RClass) RaiseError(err error) Value {
	return c.Raise(err.Error())
}

func mrbErrorHandler(mrb *MrbState, old *C.struct_mrb_jmpbuf, err *error) {
	mrb.p.jmp = old
	if r := recover(); r != nil {
		switch x := r.(type) {
		case string:
			*err = errors.New(x)
		case error:
			*err = x
		default:
			*err = errors.New("unknown error")
		}
	}

	if *err == nil {
		*err = mrb.Err()
	}
}

func (mrb *MrbState) try(f func() C.mrb_value) (result Value, err error) {
	old := mrb.p.jmp
	mrb.p.jmp = nil
	defer mrbErrorHandler(mrb, old, &err)

	result = Value{f()}

	return result, err
}
