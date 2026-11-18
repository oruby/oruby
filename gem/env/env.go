package env

import (
	_ "embed"
	"os"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed env.rb
var envRb []byte

func init() {
	oruby.Gem("env", func(mrb *oruby.MrbState) interface{} {
		//env := mrb.HashNew().RValue
		//env := mrb.ClassNew(mrb.HashClass())
		env, _ := mrb.ObjectClass().New()
		mrb.DefineSingletonMethod(env, "__raw_values", envValues, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "__raw_keys", envKeys, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "__raw_get", envGetKey, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "__raw_set", envSetKey, mrb.ArgsReq(2))
		mrb.DefineSingletonMethod(env, "__raw_delete", envDelete, mrb.ArgsReq(1))
		mrb.DefineSingletonMethod(env, "empty?", envIsEmpty, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "length", envSize, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "size", envSize, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "clear", envClear, mrb.ArgsNone())
		mrb.DefineSingletonMethod(env, "to_s", envToS, mrb.ArgsNone())

		mrb.ConstSet(mrb.KernelModule(), oruby.Sym_ENV, env)
		//mrb.DefineGlobalConst("ENV", env)
		rb.LoadRuby(mrb, "env.rb", envRb)
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

func envKeys(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	env := os.Environ()
	ret := mrb.AryNewCapa(len(env))
	for _, s := range env {
		kv := strings.SplitN(s, "=", 2)
		ret.PushString(kv[0])
	}
	return ret
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
