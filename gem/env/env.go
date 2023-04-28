package env

import (
	"os"
	"strings"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("env", func(mrb *oruby.MrbState) interface{} {
		env := mrb.HashNew().RValue
		mrb.DefineSingletonMethod(env, "values", envValues, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "value?", envHasValue, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "has_value?", envHasValue, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "keys", envKeys, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "key?", envHasKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "has_key?", envHasKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "member?", envHasKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "include?", envHasKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "[]", envGetKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "[]=", envSetKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "store", envSetKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "empty?", envIsEmpty, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "length", envSize, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "size", envSize, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "clear", envClear, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "__delete", envDelete, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "to_s", envToS, mrb.ArgsNone())

		mrb.ConstSet(mrb.KernelModule(), mrb.Intern("ENV"), env)
		return nil
	})
}

func envValues(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	env := os.Environ()
	ret := mrb.AryNewCapa(len(env))
	for _, s := range env {
		kv := strings.SplitN(s, "=", 2)
		ret.PushString(kv[1])
	}
	return ret
}

func envHasValue(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.String(mrb.GetArgsFirst())
	for _, s := range os.Environ() {
		kv := strings.SplitN(s, "=", 2)
		if arg == kv[1] {
			return mrb.TrueValue()
		}
	}
	return mrb.FalseValue()
}

func envKeys(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	env := os.Environ()
	ret := mrb.AryNewCapa(len(env))
	for _, s := range env {
		kv := strings.SplitN(s, "=", 2)
		ret.PushString(kv[0])
	}
	return ret
}

func envHasKey(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.String(mrb.GetArgsFirst())
	for _, s := range os.Environ() {
		kv := strings.SplitN(s, "=", 2)
		if arg == kv[0] {
			return mrb.TrueValue()
		}
	}
	return mrb.FalseValue()
}

func envIsEmpty(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(len(os.Environ()) == 0)
}

func envSize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Int(len(os.Environ()))
}

func envToS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.StrNew("ENV")
}

func envClear(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	os.Clearenv()
	return self
}

func envSetKey(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	k, v := mrb.GetArgs2()

	if v.IsNil() {
		envDelete(mrb, self)
		return v
	}

	name := mrb.String(k)

	err := os.Setenv(name, mrb.String(v))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return v
}

func envGetKey(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	k := mrb.GetArgsFirst()
	v, exists := os.LookupEnv(mrb.String(k))
	if !exists {
		return mrb.NilValue()
	}
	return mrb.StrNew(v)
}

func envDelete(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	k := mrb.GetArgsFirst()
	key := mrb.String(k)
	v, exists := os.LookupEnv(key)
	if !exists {
		return mrb.NilValue()
	}
	err := os.Unsetenv(key)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(v)
}
