package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
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
var errThreadError = errors.New("ThreadError")
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

// EThreadError error helper for Go classes exported to oruby
func EThreadError(format string, args ...interface{}) error {
	return Raisef(errThreadError, format, args...)
}

// ESystemCallError error helper for Go clases exported to oruby
func ESystemCallError(format string, args ...interface{}) error {
	return Raisef(errSystemCallError, format, args...)
}

// EError error helper for Go clases exported to oruby
func EError(name, format string, args ...interface{}) error {
	return Raisef(errors.New(name), format, args...)
}

// ExcValueError is a legacy, unowned exception value. RaiseError rejects it;
// use RubyError for a state-owned, retained exception instead.
type ExcValueError struct {
	Exc Value
	Msg string
}

func (e *ExcValueError) Error() string { return e.Msg }

// RubyError is a snapshot of a Ruby exception and a retained handle to its
// original object. Release must be called on the owning state goroutine.
// Its text and backtrace remain available after release or state close.
type RubyError struct {
	Class     string
	Message   string
	Backtrace []string

	owner     *MrbState
	exception Value
}

func (e *RubyError) Error() string {
	return fmt.Sprintf("%s (%s)", e.Message, e.Class)
}

// Exception returns the retained exception only for its owning, open state.
func (e *RubyError) Exception(mrb *MrbState) (Value, error) {
	if e == nil || e.owner == nil || e.owner != mrb || mrb.p == nil {
		return Nil, errors.New("Ruby exception is released, closed, or belongs to another state")
	}
	return e.exception, nil
}

// Release unroots the exception. It is idempotent and must not race with VM use.
func (e *RubyError) Release() {
	if e == nil || e.owner == nil {
		return
	}
	mrb := e.owner
	delete(mrb.rubyErrors, e)
	C.mrb_gc_unregister(mrb.p, e.exception.v)
	e.owner = nil
	e.exception = Nil
}

func (mrb *MrbState) exceptionError(exception Value) error {
	e := &RubyError{Class: "Exception", Message: "(message unavailable)"}
	if C._mrb_register_exception(mrb.p, exception.v) == C.GO_MRB_OUTCOME_SUCCESS {
		e.owner, e.exception = mrb, exception
		mrb.rubyErrors[e] = struct{}{}
	}
	var result C.mrb_value
	if C._mrb_exception_class(mrb.p, exception.v, &result) == C.GO_MRB_OUTCOME_SUCCESS {
		v := Value{result}
		if v.Type() == MrbTTString {
			e.Class = v.String()
		}
	}
	if C._mrb_exception_field(mrb.p, exception.v, 0, &result) == C.GO_MRB_OUTCOME_SUCCESS {
		v := Value{result}
		if v.Type() == MrbTTString {
			e.Message = v.String()
		}
	}
	if C._mrb_exception_field(mrb.p, exception.v, 1, &result) == C.GO_MRB_OUTCOME_SUCCESS {
		v := Value{result}
		if v.Type() == MrbTTArray {
			for i := 0; i < RArrayLen(v); i++ {
				line := mrb.AryEntry(v, i)
				if line.Type() == MrbTTString {
					e.Backtrace = append(e.Backtrace, line.String())
				}
			}
		}
	}
	return e
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

func (mrb *MrbState) getErrnoClass(name string) RClass {
	if !mrb.ClassDefined("Errno") {
		return mrb.ESystemCallError()
	}
	mod := mrb.ModuleGet("Errno")
	val := mod.ConstGet(name)
	if val.IsClass() {
		return mrb.ClassPtr(val)
	}
	return mrb.ESystemCallError()
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
	case errThreadError:
		return mrb.ExcGet("ThreadError")
	}

	if e, ok := err.(*RaiseError); ok {
		return mrb.getErrorKlass(e.err)
	}

	var sysErr syscall.Errno
	if errors.As(err, &sysErr) {
		switch sysErr {
		case syscall.ENOENT:
			return mrb.getErrnoClass("ENOENT")
		case syscall.ENOTDIR:
			return mrb.getErrnoClass("ENOTDIR")
		case syscall.EEXIST:
			return mrb.getErrnoClass("EEXIST")
		case syscall.EACCES, syscall.EPERM:
			return mrb.getErrnoClass("EACCES")
		case syscall.EINVAL:
			return mrb.getErrnoClass("EINVAL")
		case syscall.EBADF:
			return mrb.getErrnoClass("EBADF")
		case syscall.EISDIR:
			return mrb.getErrnoClass("EISDIR")
		case syscall.ENOTEMPTY:
			return mrb.getErrnoClass("ENOTEMPTY")
		case syscall.EBUSY:
			return mrb.getErrnoClass("EBUSY")
		case syscall.EXDEV:
			return mrb.getErrnoClass("EXDEV")
		case syscall.ENOSPC:
			return mrb.getErrnoClass("ENOSPC")
		case syscall.EPIPE:
			return mrb.getErrnoClass("EPIPE")
		case syscall.ESRCH:
			return mrb.getErrnoClass("ESRCH")
		case syscall.ELOOP:
			return mrb.getErrnoClass("ELOOP")
		case syscall.EMFILE:
			return mrb.getErrnoClass("EMFILE")
		case syscall.ENFILE:
			return mrb.getErrnoClass("ENFILE")
		case syscall.EFBIG:
			return mrb.getErrnoClass("EFBIG")
		case syscall.EROFS:
			return mrb.getErrnoClass("EROFS")
		case syscall.ENAMETOOLONG:
			return mrb.getErrnoClass("ENAMETOOLONG")
		case syscall.ENODEV:
			return mrb.getErrnoClass("ENODEV")
		case syscall.ESPIPE:
			return mrb.getErrnoClass("ESPIPE")
		case syscall.EAGAIN:
			return mrb.getErrnoClass("EAGAIN")
		case syscall.EFAULT:
			return mrb.getErrnoClass("EFAULT")
		case syscall.ECONNREFUSED:
			return mrb.getErrnoClass("ECONNREFUSED")
		case syscall.ECONNRESET:
			return mrb.getErrnoClass("ECONNRESET")
		case syscall.ECONNABORTED:
			return mrb.getErrnoClass("ECONNABORTED")
		case syscall.ETIMEDOUT:
			return mrb.getErrnoClass("ETIMEDOUT")
		default:
			// SystemCallError.new(nil, errno) resolves the Errno::EXXX class
			if exc, err := mrb.Funcall(mrb.ESystemCallError().Value(), Sym_new, nil, int(sysErr)); err == nil {
				return mrb.ClassOf(exc)
			}
			mrb.ExcClear()
		}
	}

	if strings.Contains(err.Error(), "too many links") {
		return mrb.getErrnoClass("ELOOP")
	}

	if os.IsNotExist(err) {
		return mrb.getErrnoClass("ENOENT")
	}
	if os.IsPermission(err) {
		return mrb.getErrnoClass("EACCES")
	}
	if os.IsExist(err) {
		return mrb.getErrnoClass("EEXIST")
	}

	if errors.Is(err, &os.SyscallError{}) || os.IsTimeout(err) {
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
	if estr == "" || estr[0] < 'A' || estr[0] > 'Z' || strings.ContainsAny(estr, " \t\r\n") {
		return mrb.EStandardErrorClass()
	}

	eid := mrb.Intern(estr)
	if mrb.ConstDefined(mrb.ObjectClass(), eid) {
		c := mrb.ConstGet(mrb.ObjectClass(), eid)
		if c.Type() == MrbTTClass {
			cls := mrb.ClassPtr(c)
			for cur := cls.p; cur != nil; cur = cur.super {
				if cur == mrb.EExceptionClass().p {
					return cls
				}
			}
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

func ErrorHandler(mrb *MrbState, result *MrbValue) {
	if r := recover(); r != nil {
		switch x := r.(type) {
		case string:
			*result = mrb.RaiseError(errors.New(x))
		case error:
			*result = mrb.RaiseError(x)
		default:
			*result = mrb.RaiseError(errors.New("unknown error"))
		}
	}
	if mrb.Exc() != nil {
		*result = *mrb.Exc()
	}
}

func (mrb *MrbState) Try(f func() MrbValue) (result MrbValue) {
	defer ErrorHandler(mrb, &result)

	mrb.ExcClear()
	result = f()

	return result
}
