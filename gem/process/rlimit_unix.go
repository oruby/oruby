//go:build !windows

package process

import (
	"strings"
	"syscall"

	"github.com/oruby/oruby"
)

// Process.getrlimit / setrlimit as MRI's proc_getrlimit / proc_setrlimit:
// the resource is an Integer, a name (Symbol, String, #to_str; "CORE" for
// RLIMIT_CORE) or #to_int; a limit is an Integer, "INFINITY", "SAVED_MAX",
// "SAVED_CUR" or #to_int.

func initRlimit(mrb *oruby.MrbState, mProc oruby.RClass) {
	for name, v := range rlimitResources {
		if !mProc.ConstDefinedAt("RLIMIT_" + name) {
			mProc.Const("RLIMIT_"+name, v)
		}
	}
	mrb.DefineModuleFunction(mProc, "getrlimit", procGetrlimit, mrb.ArgsReq(1))
	mrb.DefineModuleFunction(mProc, "setrlimit", procSetrlimit, mrb.ArgsArg(2, 1))
}

func rlimitCname(mrb *oruby.MrbState, v oruby.Value) string {
	switch {
	case v.IsNil():
		return "nil"
	case v.Type() == oruby.MrbTTTrue:
		return "true"
	case v.Type() == oruby.MrbTTFalse:
		return "false"
	}
	return mrb.ClassName(mrb.ObjClass(v))
}

// rlimitConv is NUM2INT through #to_int: an Integer (a fixnum, or a bigint:
// RLIM_INFINITY is 2**64-1 on Linux)
func rlimitConv(mrb *oruby.MrbState, v oruby.Value) (oruby.Value, error) {
	if v.IsInteger() || v.Type() == oruby.MrbTTBigInt {
		return v, nil
	}
	if v.IsNil() || !mrb.RespondTo(v, oruby.Sym_to_int) {
		return v, oruby.ETypeError("no implicit conversion of %s into Integer", rlimitCname(mrb, v))
	}
	r, err := mrb.Funcall(v, oruby.Sym_to_int)
	if err != nil {
		return v, err
	}
	if !r.IsInteger() && r.Type() != oruby.MrbTTBigInt {
		c := rlimitCname(mrb, v)
		return v, oruby.ETypeError("can't convert %s into Integer (%s#to_int gives %s)", c, c, rlimitCname(mrb, r.Value()))
	}
	return r.Value(), nil
}

func rlimitToInt(mrb *oruby.MrbState, v oruby.Value) (int64, error) {
	r, err := rlimitConv(mrb, v)
	if err != nil {
		return 0, err
	}
	if r.IsInteger() {
		return r.Int64(), nil
	}
	return 0, oruby.ERangeError("bignum too big to convert into 'int'")
}

// rlimitToUint is an rlim_t: a bigint up to 2**64-1 is as is
func rlimitToUint(mrb *oruby.MrbState, v oruby.Value) (uint64, error) {
	r, err := rlimitConv(mrb, v)
	if err != nil {
		return 0, err
	}
	if r.IsInteger() {
		return uint64(r.Int64()), nil
	}
	z, err := mrb.BigInt(r)
	if err != nil {
		return 0, err
	}
	if !z.IsUint64() {
		return 0, oruby.ERangeError("bignum too big to convert into 'unsigned long'")
	}
	return z.Uint64(), nil
}

// rlimitName: the name a Symbol, String or #to_str gives, ok false for none
func rlimitName(mrb *oruby.MrbState, v oruby.Value) (string, bool, error) {
	if v.IsSymbol() || v.IsString() {
		return mrb.String(v), true, nil
	}
	if !v.IsNil() && !v.IsInteger() && mrb.RespondTo(v, oruby.Sym_to_str) {
		s, err := mrb.Funcall(v, oruby.Sym_to_str)
		if err != nil {
			return "", false, err
		}
		if s.IsString() {
			return mrb.String(s), true, nil
		}
	}
	return "", false, nil
}

func rlimitResource(mrb *oruby.MrbState, v oruby.Value) (int, error) {
	name, ok, err := rlimitName(mrb, v)
	if err != nil {
		return 0, err
	}
	if ok {
		if r, found := rlimitResources[strings.ToUpper(name)]; found {
			return r, nil
		}
		return 0, oruby.EArgumentError("invalid resource name: %s", name)
	}
	n, err := rlimitToInt(mrb, v)
	return int(n), err
}

func rlimitValue(mrb *oruby.MrbState, v oruby.Value) (uint64, error) {
	name, ok, err := rlimitName(mrb, v)
	if err != nil {
		return 0, err
	}
	if ok {
		switch strings.ToUpper(name) {
		case "INFINITY", "SAVED_MAX", "SAVED_CUR":
			return RLIM_INFINITY, nil
		}
		return 0, oruby.EArgumentError("invalid resource value: %s", name)
	}
	return rlimitToUint(mrb, v)
}

// rlimitInt is an rlim_t as an Integer: RLIM_INFINITY is 2**64-1 on Linux
func rlimitInt(mrb *oruby.MrbState, v uint64) oruby.Value {
	return mrb.Uint64Value(v)
}

func procGetrlimit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	res, err := rlimitResource(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(res, &rl); err != nil {
		return mrb.SysFail(err)
	}
	return mrb.AryNewFromValues(rlimitInt(mrb, uint64(rl.Cur)), rlimitInt(mrb, uint64(rl.Max)))
}

func procSetrlimit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	res, err := rlimitResource(mrb, a.Item(0))
	if err != nil {
		return mrb.RaiseError(err)
	}
	cur, err := rlimitValue(mrb, a.Item(1))
	if err != nil {
		return mrb.RaiseError(err)
	}
	max := cur
	if a.Len() > 2 {
		if max, err = rlimitValue(mrb, a.Item(2)); err != nil {
			return mrb.RaiseError(err)
		}
	}
	rl := syscall.Rlimit{Cur: cur, Max: max}
	if err := syscall.Setrlimit(res, &rl); err != nil {
		return mrb.SysFail(err)
	}
	return mrb.NilValue()
}
