package erubi

import (
	"github.com/oruby/oruby"
	"regexp"
)

func init() {
	oruby.Gem("erb", func(mrb *oruby.MrbState) interface{} {
		erb := mrb.DefineClass("ERB", mrb.ObjectClass())
		erb.DefineClassMethod("version", erbVersion, mrb.ArgsNone())
		erb.DefineClassMethod("initialize", erbInit, mrb.ArgsArg(1, 3))
		erb.DefineMethod("def_class", erbDefClass, mrb.ArgsOpt(2))
		erb.DefineMethod("def_method", erbDefMethod, mrb.ArgsArg(2, 1))
		erb.DefineMethod("def_module", erbDefModule, mrb.ArgsOpt(1))
		//erubi.DefineMethod("location", erbLocation, mrb.Args);
		//erubi.DefineMethod("make_compiler", erbMakeCompiler, mrb.Args);
		//erubi.DefineMethod("result", erbResult, mrb.Args);
		//erubi.DefineMethod("result_with_hash", erbResultWithHash, mrb.Args);
		//erubi.DefineMethod("run", erbRun, mrb.Args);
		//erubi.DefineMethod("set_eoutvar", erbsetEoutvar, mrb.Args);
		return nil
	})
}

func erbVersion(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.StringValue("erubi.rb [2.2.0 2018-11-12]")
}

func erbInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	erb := mrb.RObject(self)
	erb.SetIV("@safe_level", mrb.FixnumValue(0)) //safeLevel
	erb.SetIV("@filename", mrb.NilValue())
	erb.SetIV("@lineno", mrb.FixnumValue(0))
	// compiler = make_compiler(trim_mode)
	//set_eoutvar(compiler, eoutvar)
	//@src, @encoding, @frozen_string = *compiler.compile(str)
	return erb
}

func erbDefClass(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetAllArgs()

	superklass := args.ItemDef(0, mrb.ObjectClass())
	methodname := args.ItemDef(1, mrb.StringValue("result"))
	filename := mrb.IVGet(self, mrb.Intern("@filename"))
	if mrb.NilP(filename) {
		filename = mrb.StringValue("(ERB)").Value()
	}

	cls, _ := mrb.ClassNew(mrb.ClassOf(superklass))
	mrb.Call(self, "def_method", cls, methodname, filename)
	return cls
}

func erbDefMethod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetAllArgs()
	mod := args.Item(0)
	methodname := mrb.String(args.Item(1))
	fname := args.ItemDef(3, mrb.StringValue("(ERB)"))
	src := mrb.IVGet(self, mrb.Intern("@src"))

	r := regexp.MustCompile("^(?!#|$)")
	s := r.ReplaceAllString(mrb.String(src), "def "+methodname+"\n") + "\nend\n"

	ret, _ := mrb.FuncallWithBlock(mod, mrb.Intern("module_eval"), func() oruby.MrbValue {
		binding := mrb.NilValue()
		return mrb.Call(mrb.KernelModule(), "eval", s, binding, fname, -1)
	})
	return ret
}

func erbDefModule(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetAllArgs()
	methodname := args.ItemDef(1, mrb.StringValue("erubi"))
	filename := mrb.IVGet(self, mrb.Intern("@filename"))
	if mrb.NilP(filename) {
		filename = mrb.StringValue("(ERB)").Value()
	}

	mod := mrb.ModuleNew()
	mrb.Call(self, "def_method", mod, methodname, filename)
	return mod
}
