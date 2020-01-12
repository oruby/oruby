package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"fmt"
	"strings"
)

var eRuntimeError = errors.New("RuntimeError")
var eTypeError = errors.New("TypeError")
var eArgumentError = errors.New("ArgumentError")
var eIndexError = errors.New("IndexError")
var eRangeError = errors.New("RangeError")
var eNameError = errors.New("NameError")
var eNoMethodError = errors.New("NoMethodError")
var eScriptError = errors.New("ScriptError")
var eSyntaxError = errors.New("SyntaxError")
var eLocalJumpError = errors.New("LocalJumpError")
var eRegexpError = errors.New("RegexpError")
var eFrozenError = errors.New("FrozenError")
var eNotImplementedError = errors.New("NotImplementedError")
var eFloatDomainError = errors.New("FloatDomainError")
var eKeyError = errors.New("KeyError")

func ERuntimeError(format string, args ...interface{}) error {
	return Raisef(eRuntimeError, format, args...)
}
func ETypeError(format string, args ...interface{}) error { return Raisef(eTypeError, format, args...) }
func EArgumentError(format string, args ...interface{}) error {
	return Raisef(eArgumentError, format, args...)
}
func EIndexError(format string, args ...interface{}) error {
	return Raisef(eIndexError, format, args...)
}
func ERangeError(format string, args ...interface{}) error {
	return Raisef(eRangeError, format, args...)
}
func ENameError(format string, args ...interface{}) error { return Raisef(eNameError, format, args...) }
func ENoMethodError(format string, args ...interface{}) error {
	return Raisef(eNoMethodError, format, args...)
}
func EScriptError(format string, args ...interface{}) error {
	return Raisef(eScriptError, format, args...)
}
func ESyntaxError(format string, args ...interface{}) error {
	return Raisef(eSyntaxError, format, args...)
}
func ELocalJumpError(format string, args ...interface{}) error {
	return Raisef(eLocalJumpError, format, args...)
}
func ERegexpError(format string, args ...interface{}) error {
	return Raisef(eRegexpError, format, args...)
}
func EErozenError(format string, args ...interface{}) error {
	return Raisef(eFrozenError, format, args...)
}
func EEotImplementedError(format string, args ...interface{}) error {
	return Raisef(eNotImplementedError, format, args...)
}
func EEloatDomainError(format string, args ...interface{}) error {
	return Raisef(eFloatDomainError, format, args...)
}
func EEeyError(format string, args ...interface{}) error { return Raisef(eKeyError, format, args...) }
func EError(name, format string, args ...interface{}) error {
	return Raisef(errors.New(name), format, args...)
}

// RaiseError containing error type and message
type RaiseError struct {
	err error
	msg string
}

func Raise(err error, msg string) error {
	return &RaiseError{err, msg}
}

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

// Unwrap returns inner error
func (e *RaiseError) Unwrap() error {
	return e.err
}

// Raise exception
//func (mrb *MrbState) Raise(c RClass, msg string) Value {
//cmsg := C.CString(msg)
//defer C.free(unsafe.Pointer(cmsg))
//return mrb.ExcNew(c, msg)
//}

func (mrb *MrbState) getErrorKlass(err error) RClass {
	switch err {
	case eRuntimeError:
		return mrb.ERuntimeError()
	case eTypeError:
		return mrb.ETypeError()
	case eArgumentError:
		return mrb.EArgumentError()
	case eIndexError:
		return mrb.EIndexError()
	case eRangeError:
		return mrb.ERangeError()
	case eNameError:
		return mrb.ENameError()
	case eNoMethodError:
		return mrb.ENoMethodError()
	case eScriptError:
		return mrb.EScriptError()
	case eSyntaxError:
		return mrb.ESyntaxError()
	case eLocalJumpError:
		return mrb.ELocalJumpError()
	case eRegexpError:
		return mrb.ERegexpError()
	case eFrozenError:
		return mrb.EFrozenError()
	case eNotImplementedError:
		return mrb.ENotImplementedError()
	case eFloatDomainError:
		return mrb.EFloatDomainError()
	case eKeyError:
		return mrb.EKeyError()
	}

	if e, ok := err.(*RaiseError); ok {
		return mrb.getErrorKlass(e.err)
	}

	estr := err.Error()

	// Class name shoudld be non-empty, uppercse starting string, without whitespace
	if estr == "" || estr[1] < 'A' || strings.ContainsAny(estr, " \t\r\n") {
		return mrb.EStandardErrorClass()
	}

	c := mrb.ConstGet(mrb.ObjectClass().Value(), mrb.Intern(estr))
	if c.Type() != MrbTTClass {
		return mrb.EStandardErrorClass()
	}

	if mrb.ObjIsKindOf(c, mrb.EExceptionClass()) {
		return mrb.ClassOf(c)
	}

	return mrb.EStandardErrorClass()
}

// Raise raises Exception from class
// If class is Exception descendand - itself is raised
// If class is not Exception descendant - Exception class is raised
func (c RClass) Raise(msg string) Value {
	return c.mrb.Raise(c, msg)
}

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
