package env

import (
	_ "embed"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
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

		mrb.ConstSet(mrb.ObjectClass(), oruby.Sym_ENV, env) // Object::ENV, as MRI
		//mrb.DefineGlobalConst("ENV", env)
		rb.LoadRuby(mrb, "env.rb", envRb)
		return nil
	})
}

func envValues(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	env := sysEnviron()
	ret := mrb.AryNewCapa(len(env))
	for _, s := range env {
		kv := strings.SplitN(s, "=", 2)
		ret.PushString(kv[1])
	}
	return ret
}

func envKeys(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	env := sysEnviron()
	ret := mrb.AryNewCapa(len(env))
	for _, s := range env {
		kv := strings.SplitN(s, "=", 2)
		ret.PushString(kv[0])
	}
	return ret
}

func envIsEmpty(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Bool(len(sysEnviron()) == 0)
}

func envSize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.Int(len(sysEnviron()))
}

func envToS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.StrNew("ENV")
}

func envClear(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sysClearenv()
	return self
}

func envSetKey(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	k, v := mrb.GetArgs2()

	if v.IsNil() {
		envDelete(mrb, self)
		return v
	}

	name := mrb.String(k)

	err := sysSetenv(name, mrb.String(v))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return v
}

func envGetKey(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	k := mrb.GetArgsFirst()
	v, exists := sysLookupEnv(mrb.String(k))
	if !exists {
		return mrb.NilValue()
	}
	return mrb.StrNew(v)
}

func envDelete(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	k := mrb.GetArgsFirst()
	key := mrb.String(k)
	v, exists := sysLookupEnv(key)
	if !exists {
		return mrb.NilValue()
	}
	err := sysUnsetenv(key)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(v)
}
