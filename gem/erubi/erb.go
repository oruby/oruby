package erubi

import (
	_ "embed"
	"regexp"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed erb.rb
var erbRuby []byte

func init() {
	oruby.Gem("erb", func(mrb *oruby.MrbState) interface{} {
		erb := mrb.DefineClass("ERB", mrb.ObjectClass())
		erb.DefineClassMethod("version", erbVersion, mrb.ArgsNone())
		erb.DefineMethod("initialize", erbInit, mrb.ArgsArg(1, 3))
		erb.DefineMethod("src", erbSrc, mrb.ArgsNone())
		erb.DefineMethod("result", erbResult, mrb.ArgsOpt(1))
		erb.DefineMethod("run", erbRun, mrb.ArgsOpt(1))
		erb.DefineMethod("def_class", erbDefClass, mrb.ArgsOpt(2))
		erb.DefineMethod("def_method", erbDefMethod, mrb.ArgsArg(2, 1))
		erb.DefineMethod("def_module", erbDefModule, mrb.ArgsOpt(1))
		//erubi.DefineMethod("location", erbLocation, mrb.Args);
		//erubi.DefineMethod("make_compiler", erbMakeCompiler, mrb.Args);
		//erubi.DefineMethod("result", erbResult, mrb.Args);
		//erubi.DefineMethod("result_with_hash", erbResultWithHash, mrb.Args);
		//erubi.DefineMethod("run", erbRun, mrb.Args);
		//erubi.DefineMethod("set_eoutvar", erbsetEoutvar, mrb.Args);
		escMod := mrb.DefineModuleUnder(erb, "Escape")
		escMod.DefineModuleFunction("html_escape", erbHTMLEscape, mrb.ArgsReq(1))
		escMod.DefineModuleFunction("__url_encode", erbURLEncode, mrb.ArgsReq(1))
		rb.LoadRuby(mrb, "erb.rb", erbRuby)
		return nil
	})
	oruby.GemAlias("erb/escape", "erb")
	oruby.GemAlias("erb/util", "erb")
}

func erbURLEncode(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	var s string
	if arg.IsString() {
		s = arg.String()
	} else {
		sv, err := mrb.Funcall(arg, oruby.Sym_to_s)
		if err != nil {
			return mrb.RaiseError(err)
		}
		s = sv.String()
	}
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
	}
	return mrb.StrNew(b.String())
}

func erbHTMLEscape(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	var s string
	if arg.IsString() {
		s = arg.String()
	} else {
		sv, err := mrb.Funcall(arg, oruby.Sym_to_s)
		if err != nil {
			return mrb.RaiseError(err)
		}
		s = sv.String()
	}
	if !strings.ContainsAny(s, `&<>"'`) {
		return mrb.StrNew(s)
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteByte(s[i])
		}
	}
	return mrb.StrNew(b.String())
}

func erbVersion(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.StringValue("erubi.rb [2.2.0 2018-11-12]")
}

func erbInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	erb := mrb.RValue(self)
	args := mrb.GetArgs()
	input := args.Item(0)
	if !input.IsString() {
		return mrb.Raise(mrb.ETypeError(), "ERB template must be a String")
	}
	if args.Len() > 1 {
		return mrb.Raise(mrb.EArgumentError(), "ERB positional options are not supported")
	}
	kwargs := mrb.KeywordArgs()
	trimKey := mrb.SymbolValue(oruby.Sym_trim_mode)
	outvarKey := mrb.SymbolValue(oruby.Sym_eoutvar)
	known := 0
	if kwargs.KeyP(trimKey) {
		known++
		if !kwargs.Get(trimKey).IsNil() {
			return mrb.Raise(mrb.ENotImplementedError(), "ERB trim_mode is not supported")
		}
	}
	options := map[string]interface{}{"trim": false, "outvar": "_erbout"}
	if kwargs.KeyP(outvarKey) {
		known++
		outvar := kwargs.Get(outvarKey)
		if !outvar.IsString() {
			return mrb.Raise(mrb.ETypeError(), "ERB eoutvar must be a String")
		}
		name := mrb.String(outvar)
		if !regexp.MustCompile(`^[@$]?[a-z_][a-zA-Z0-9_]*$`).MatchString(name) {
			return mrb.Raise(mrb.EArgumentError(), "invalid ERB eoutvar")
		}
		options["outvar"] = name
	}
	if kwargs.Size(kwargs.Value()) != known {
		return mrb.Raise(mrb.EArgumentError(), "unknown ERB keyword")
	}
	engine, err := NewInit(mrb.String(input), options)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	erb.SetIV("@src", mrb.StrNew(engine.Src()))
	erb.SetIV("@safe_level", mrb.FixnumValue(0)) //safeLevel
	erb.SetIV("@filename", mrb.NilValue())
	erb.SetIV("@lineno", mrb.FixnumValue(0))
	return erb
}

func erbSrc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.IVGet(self, oruby.IvSym_src)
}

func erbResult(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	source := mrb.IVGet(self, oruby.IvSym_src)
	binding := mrb.GetArgs().Item(0)
	filename := mrb.IVGet(self, oruby.IvSym_filename)
	if filename.IsNil() {
		filename = mrb.StringValue("(erb)").Value()
	}
	lineno := mrb.IVGet(self, oruby.IvSym_lineno)
	return mrb.Call(mrb.KernelModule(), "eval", source, binding, filename, lineno)
}

func erbRun(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	result := erbResult(mrb, self)
	if exc := mrb.Exc(); exc != nil {
		return exc.Value()
	}
	mrb.Call(mrb.GetGV("$stdout"), "write", result)
	if exc := mrb.Exc(); exc != nil {
		return exc.Value()
	}
	return mrb.NilValue()
}

func erbDefClass(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Raise(mrb.ENotImplementedError(), "ERB#def_class is not supported")
}

func erbDefMethod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Raise(mrb.ENotImplementedError(), "ERB#def_method is not supported")
}

func erbDefModule(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Raise(mrb.ENotImplementedError(), "ERB#def_module is not supported")
}
