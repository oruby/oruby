package load

import (
	"errors"
	"fmt"
	"github.com/oruby/oruby"
	"os"
	"path/filepath"
)

func init() {
	oruby.Gem("load", func(mrb *oruby.MrbState) interface{} {
		loadPath := mrb.AryNew()
		mrb.SetGV("$LOAD_PATH", loadPath)
		mrb.SetGV("$:", loadPath)
		mrb.SetGV("$-I", loadPath)

		mrb.DefineMethod(loadPath.Class(), "resolve_feature_path", resolveFeaturePath, mrb.ArgsReq(1))

		loadedFeatures := mrb.AryNew()
		mrb.SetGV("$LOADED_FEATURES", loadedFeatures)
		mrb.SetGV(`$"`, loadedFeatures)

		mrb.DefineClass("ELoadError", mrb.EScriptError())

		mrb.DefineGlobalFunction("load", loadLoad, mrb.ArgsReq(1))
		mrb.DefineGlobalFunction("require", loadRequire, mrb.ArgsReq(1))
		mrb.DefineGlobalFunction("require_relative", loadRequireRelative, mrb.ArgsReq(1))
		mrb.ModuleClass().DefineMethod("autoload", loadAutoload, mrb.ArgsReq(2))
		mrb.ModuleClass().DefineMethod("autoload?", loadAutoloadP, mrb.ArgsReq(1))
		mrb.DefineGlobalFunction("autoload", loadAutoload, mrb.ArgsReq(2))
		mrb.DefineGlobalFunction("autoload?", loadAutoloadP, mrb.ArgsReq(1))
		return nil
	})
}

func loadScript(mrb *oruby.MrbState, fileName string) (oruby.RProc, error) {
	cxt := mrb.MrbcContextNew()
	defer cxt.Free()
	cxt.SetCaptureErrors(true)
	cxt.Filename(fileName)

	p, err := mrb.ParseFile(fileName, cxt)
	if err != nil {
		return oruby.RProc{}, err
	}
	defer p.Free()

	// Check parse errors
	if p.NErr() > 0 {
		estr := ""
		for i := 0; i < p.NErr(); i++ {
			e := p.ErrorBuffer(i)
			estr += fmt.Sprintf("%s:%d:%d: %s\n", mrb.SymString(p.Filename()), e.LineNo, e.Column, e.Message)
		}
		return oruby.RProc{}, errors.New(estr)
	}

	return mrb.GenerateCode(p)
}

func loadIrep(mrb *oruby.MrbState, fileName string) (oruby.RProc, error) {
	ai := mrb.GCArenaSave()
	irep, err := mrb.ReadIrepFile(fileName)
	mrb.GCArenaRestore(ai)

	if err != nil {
		return oruby.RProc{}, err
	}

	proc := mrb.ProcNew(irep)
	proc.SetTargetClass(mrb.ObjectClass())
	//mrb.IrepDecref(irep)

	return proc, nil
}

func doLoad(mrb *oruby.MrbState, fullName string, wrap bool) error {
	var proc oruby.RProc
	var err error

	// load in anonymous module as toplevel
	switch filepath.Ext(fullName) {
	case ".rb":
		proc, err = loadScript(mrb, fullName)
	case ".mrb":
		proc, err = loadIrep(mrb, fullName)
	default:
		err = fmt.Errorf("cannot load such file -- %v. '%v' not supported", fullName, filepath.Ext(fullName))
	}

	if err != nil {
		return err
	}

	ai := mrb.GCArenaSave()
	defer mrb.GCArenaRestore(ai)

	_, err = mrb.YieldWithClass(proc, mrb.TopSelf(), mrb.ObjectClass())
	return err
}

func loadingFilesFind(mrb *oruby.MrbState, filepath oruby.Value) bool {
	loadingFiles := mrb.GetGV(`$"_`)
	if loadingFiles.IsNil() {
		return false
	}
	return mrb.HashKeyP(loadingFiles, filepath)
}

func loadingFilesDelete(mrb *oruby.MrbState, filepath oruby.Value) {
	loadingFiles := mrb.GetGV(`$"_`)
	if loadingFiles.IsNil() {
		return
	}
	oldLoader := mrb.HashDeleteKey(loadingFiles, filepath)
	mrb.SetGV(`$"__`, oldLoader)
}

func loadingFilesAdd(mrb *oruby.MrbState, filepath oruby.Value) {
	loadingFiles := mrb.GetGV(`$"_`)
	if loadingFiles.IsNil() {
		loadingFiles = mrb.HashNew().Value()
		mrb.SetGV(`$"_`, loadingFiles)
	}
	oldLoader := mrb.GetGV(`$"__`)
	mrb.SetGV(`$"__`, filepath)
	mrb.HashSet(loadingFiles, filepath, oldLoader)
}

func resolveFeaturePath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	feature := mrb.GetArgsFirst().String()
	name, err := resolveName(mrb, feature)
	if err != nil {
		return mrb.Raisef(mrb.ClassGet("ELoadError"), err.Error())
	}
	return mrb.StrNewStatic(name)
}

func loadLoad(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	nameValue, wrap := mrb.GetArgs2(nil, false)
	if nameValue.Type() != oruby.MrbTTString {
		return mrb.Raise(mrb.ETypeError(), "string path param expected")
	}

	name, err := fullName(nameValue.String())
	if err != nil {
		return mrb.Raisef(mrb.ClassGet("ELoadError"), "cannot load such file -- %v", nameValue.String())
	}

	err = doLoad(mrb, name, wrap.Bool())
	if err != nil {
		return mrb.Raise(mrb.ClassGet("ELoadError"), err.Error())
	}

	return mrb.TrueValue()
}

func fullName(filename string) (string, error) {
	if _, err := os.Stat(filename); err != nil {
		return "", err
	}
	return filepath.Abs(filename)
}

func resolveName(mrb *oruby.MrbState, feature string) (string, error) {
	const supportedExtCount = 2

	filenames := make([]string, supportedExtCount)
	if filepath.Ext(feature) == "" {
		filenames[0] = feature + ".rb"
		filenames[1] = feature + ".mrb"
	} else {
		filenames[0] = feature
	}

	if feature[0] == '.' || feature[0] == '/' || feature[0] == filepath.Separator ||
		(len(feature) > 1 && feature[1] == ':') {
		return fullName(feature)
	}

	paths := mrb.GVGetObj(mrb.Intern("$LOAD_PATH")).RArray()
	for i := 0; i < paths.Len(); i++ {
		path := paths.Item(i).String()
		for _, filename := range filenames {
			if filename == "" {
				continue
			}
			fName := filepath.Join(path, filename)
			name, err := fullName(fName)
			if err == nil {
				return name, nil
			}
		}
	}

	return "", fmt.Errorf("cannot load such file -- %v", feature)
}

func doRequire(mrb *oruby.MrbState, feature string) oruby.MrbValue {
	// If there is implemented Go feature
	if _, err := mrb.Resolve(feature); err == nil {
		return mrb.FalseValue()
	}

	name, err := resolveName(mrb, feature)
	if err != nil {
		return mrb.Raisef(mrb.ClassGet("ELoadError"), "cannot load such file -- %v", feature)
	}

	// Check if fullpath name is already loaded
	if _, err := mrb.Resolve(name); err == nil {
		return mrb.FalseValue()
	}

	nameValue := mrb.StrNewStatic(name)

	// Check if file is set to be loaded
	if loadingFilesFind(mrb, nameValue) {
		return mrb.FalseValue()
	}

	// Load feature
	loadingFilesAdd(mrb, nameValue)
	defer loadingFilesDelete(mrb, nameValue)
	if err := doLoad(mrb, name, true); err != nil {
		return mrb.Raise(mrb.ClassGet("ELoadError"), err.Error())
	}

	// Remember loaded feature
	loadedFeatures := mrb.GVGet(mrb.Intern("$LOADED_FEATURES"))
	mrb.AryPush(loadedFeatures, nameValue)
	mrb.FeatureAdd(name)

	return mrb.TrueValue()
}

func loadRequire(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	feature := mrb.GetArgsFirst().String()
	return doRequire(mrb, feature)
}

func loadRequireRelative(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	feature := mrb.GetArgsFirst().String()
	file := mrb.GVGet(mrb.Intern("$\"__")).String()
	if file == "" {
		file = os.Args[0]
	}

	name, err := filepath.Abs(filepath.Join(filepath.Dir(file), feature))
	if err != nil {
		return mrb.Raise(mrb.ClassGet("ELoadError"), err.Error())
	}

	return doRequire(mrb, name)
}

// loadAutoload autoload feature is not suported. It immediatly requires feature
func loadAutoload(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, pathValue := mrb.GetArgs2()
	path := pathValue.String()
	if name.Type() != oruby.MrbTTSymbol || name.Type() != oruby.MrbTTString {
		return mrb.Raise(mrb.ClassGet("ELoadError"), "Symbol expected")
	}

	ret := doRequire(mrb, path)

	autoloads := mrb.GetGV("$AUTOLOADS")
	if autoloads.IsNil() {
		autoloads = mrb.HashNew().Value()
		mrb.SetGV("$AUTOLOADS", autoloads)
	}
	mrb.HashSet(autoloads, name, pathValue)

	return ret
}

// loadAutoloadP autoload feature is not suported. It immediatly requires feature
func loadAutoloadP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst()
	autoloads := mrb.GetGV("$AUTOLOADS")
	if autoloads.IsNil() || name.Type() != oruby.MrbTTSymbol || name.Type() != oruby.MrbTTString {
		return mrb.NilValue()
	}
	return mrb.HashGet(autoloads, name)
}
