package load

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("load", func(mrb *oruby.MrbState) interface{} {
		loadPath := mrb.AryNew()
		if rl := os.Getenv("RUBYLIB"); rl != "" {
			for _, p := range filepath.SplitList(rl) {
				loadPath.Push(mrb.StrNew(p))
			}
		}
		mrb.SetGV("$LOAD_PATH", loadPath)
		mrb.SetGV("$:", loadPath)
		mrb.SetGV("$-I", loadPath)

		mrb.DefineMethod(loadPath.Class(), "resolve_feature_path", resolveFeaturePath, mrb.ArgsReq(1))

		loadedFeatures := mrb.AryNew()
		mrb.SetGV("$LOADED_FEATURES", loadedFeatures)
		mrb.SetGV(`$"`, loadedFeatures)

		loadError := mrb.DefineClass("LoadError", mrb.EScriptError())
		mrb.DefineGlobalConst("ELoadError", loadError)

		mrb.DefineGlobalFunction("load", loadLoad, mrb.ArgsReq(1))
		mrb.DefineGlobalFunction("require", loadRequire, mrb.ArgsReq(1))
		mrb.DefineGlobalFunction("require_relative", loadRequireRelative, mrb.ArgsReq(1))
		mrb.ModuleClass().DefineMethod("autoload", loadAutoload, mrb.ArgsReq(2))
		mrb.ModuleClass().DefineMethod("autoload?", loadAutoloadP, mrb.ArgsReq(1))
		mrb.ModuleClass().DefineMethod("const_missing", loadConstMissing, mrb.ArgsReq(1))
		mrb.DefineGlobalFunction("autoload", loadAutoload, mrb.ArgsReq(2))
		mrb.DefineGlobalFunction("autoload?", loadAutoloadP, mrb.ArgsReq(1))
		return nil
	})
}

func loadScript(mrb *oruby.MrbState, fileName string) (oruby.RProc, error) {
	cxt := mrb.MrbCcontextNew()
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
	var err error

	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.Filename(fullName)

	switch filepath.Ext(fullName) {
	case ".mrb":
		_, err = cxt.LoadIrepFile(fullName)
	default:
		_, err = mrb.LoadFileCxt(fullName, cxt)
	}

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
		return mrb.Raise(mrb.ClassGet("ELoadError"), err.Error())
	}
	return mrb.StrNew(name)
}

func loadLoad(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	nameValue, wrap := mrb.GetArgs2(nil, false)
	if nameValue.Type() != oruby.MrbTTString {
		return mrb.Raise(mrb.ETypeError(), "string path param expected")
	}

	name, err := fullName(nameValue.String())
	if err != nil {
		return raiseCannotLoad(mrb, nameValue.String())
	}

	nv := mrb.StrNew(name)
	loadingFilesAdd(mrb, nv)
	defer loadingFilesDelete(mrb, nv)

	err = doLoad(mrb, name, wrap.Bool())
	if err != nil {
		return loadFailure(mrb, err)
	}

	return mrb.TrueValue()
}

// raiseCannotLoad raises LoadError for feature with LoadError#path set
func raiseCannotLoad(mrb *oruby.MrbState, feature string) oruby.MrbValue {
	exc := mrb.Raisef(mrb.ClassGet("LoadError"), "cannot load such file -- %v", feature)
	_ = mrb.IVSet(exc, mrb.Intern("@path"), mrb.StrNew(feature))
	return exc
}

// loadFailure re-raises exception raised by loaded code (SyntaxError,
// SystemExit, NameError...) instead of hiding it in a LoadError
func loadFailure(mrb *oruby.MrbState, err error) oruby.MrbValue {
	if exc := mrb.Exc(); exc != nil {
		return exc.Value()
	}
	return mrb.Raise(mrb.ClassGet("LoadError"), err.Error())
}

func fullName(filename string) (string, error) {
	fi, err := os.Stat(filename)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return "", fmt.Errorf("%s is a directory", filename)
	}
	return filepath.Abs(filename)
}

func resolveName(mrb *oruby.MrbState, feature string) (string, error) {
	if len(feature) == 0 {
		return "", errors.New("cannot load such file -- ")
	}

	var filenames []string
	if filepath.Ext(feature) == "" {
		filenames = []string{feature + ".rb", feature + ".mrb"}
	} else {
		filenames = []string{feature}
	}

	if feature[0] == '.' || feature[0] == '/' || feature[0] == filepath.Separator ||
		(len(feature) > 1 && feature[1] == ':') {
		for _, fn := range filenames {
			if name, err := fullName(fn); err == nil {
				return name, nil
			}
		}
		if name, err := fullName(feature); err == nil {
			return name, nil
		}
		return "", fmt.Errorf("cannot load such file -- %v", feature)
	}

	paths := mrb.GVGetObj(mrb.Intern("$LOAD_PATH")).RArray()
	for i := 0; i < paths.Len(); i++ {
		path := paths.Item(i).String()
		for _, filename := range filenames {
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
	if loaded, err := mrb.Resolve(feature); err == nil {
		if loaded {
			loadedFeatures := mrb.GVGet(mrb.Intern("$LOADED_FEATURES"))
			mrb.AryPush(loadedFeatures, mrb.StrNew(feature))
			mrb.FeatureAdd(feature)
			return mrb.TrueValue()
		}
		return mrb.FalseValue()
	}

	name, err := resolveName(mrb, feature)
	if err != nil {
		return raiseCannotLoad(mrb, feature)
	}

	// Check if fullpath name is already loaded
	if _, err := mrb.Resolve(name); err == nil {
		return mrb.FalseValue()
	}

	nameValue := mrb.StrNew(name)

	// Check if file is set to be loaded
	if loadingFilesFind(mrb, nameValue) {
		return mrb.FalseValue()
	}

	// Load feature
	loadingFilesAdd(mrb, nameValue)
	defer loadingFilesDelete(mrb, nameValue)
	if err := doLoad(mrb, name, false); err != nil {
		return loadFailure(mrb, err)
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

	var file string
	res, err := mrb.Funcall(mrb.TopSelf(), mrb.Intern("caller"), 0)
	if err == nil && res.Type() == oruby.MrbTTArray {
		ary := mrb.EnsureArrayType(res)
		if ary.Len() > 0 {
			loc := ary.Item(0).String()
			if idx := strings.Index(loc, ":"); idx != -1 {
				file = loc[:idx]
			}
		}
	}

	if file == "" || file == "-e" {
		file = mrb.GVGet(mrb.Intern("$\"__")).String()
	}
	if file == "" {
		file = os.Args[0]
	}

	name, err := filepath.Abs(filepath.Join(filepath.Dir(file), feature))
	if err != nil {
		return mrb.Raise(mrb.ClassGet("LoadError"), err.Error())
	}

	return doRequire(mrb, name)
}

func autoloadOwner(mrb *oruby.MrbState, self oruby.Value) oruby.Value {
	if self.Type() == oruby.MrbTTClass || self.Type() == oruby.MrbTTModule {
		return self
	}
	return mrb.ObjectClass().Value()
}

func autoloadName(mrb *oruby.MrbState, value oruby.Value) (oruby.MrbSym, bool) {
	switch value.Type() {
	case oruby.MrbTTSymbol:
		return value.Symbol(), true
	case oruby.MrbTTString:
		name := value.String()
		if name != "" {
			return mrb.Intern(name), true
		}
	}
	return 0, false
}

func autoloadEntries(mrb *oruby.MrbState, owner oruby.Value, create bool) oruby.Value {
	registry := mrb.GetGV("$AUTOLOADS")
	if registry.IsNil() {
		if !create {
			return oruby.Nil
		}
		registry = mrb.HashNew().Value()
		mrb.SetGV("$AUTOLOADS", registry)
	}
	entries := mrb.HashGet(registry, owner)
	if entries.IsNil() && create {
		entries = mrb.HashNew().Value()
		mrb.HashSet(registry, owner, entries)
	}
	return entries
}

// loadAutoload records a deferred constant-to-file mapping for its owning module.
func loadAutoload(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, pathValue := mrb.GetArgs2()
	id, ok := autoloadName(mrb, name)
	if !ok {
		return mrb.Raise(mrb.ENameError(), "autoload must be called with a constant name")
	}
	if pathValue.Type() != oruby.MrbTTString {
		return mrb.Raisef(mrb.ETypeError(), "no implicit conversion of %s into String", mrb.TypeName(pathValue))
	}
	owner := autoloadOwner(mrb, self)
	if mrb.ConstDefined(owner, id) {
		return mrb.NilValue()
	}
	entries := autoloadEntries(mrb, owner, true)
	mrb.HashSet(entries, mrb.SymbolValue(id), pathValue)
	return mrb.NilValue()
}

// loadAutoloadP returns the deferred path for a constant owned by the receiver.
func loadAutoloadP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst()
	id, ok := autoloadName(mrb, name)
	if !ok {
		return mrb.NilValue()
	}
	entries := autoloadEntries(mrb, autoloadOwner(mrb, self), false)
	if entries.IsNil() {
		return mrb.NilValue()
	}
	return mrb.HashGet(entries, mrb.SymbolValue(id))
}

// autoloadCandidates lists the modules whose autoloads may define a missing
// constant of self, in Ruby lookup order: self, the enclosing namespaces
// (from self's name; mruby's const_missing does not expose the lexical
// scope), self's ancestors, then Object.
func autoloadCandidates(mrb *oruby.MrbState, self oruby.Value) []oruby.Value {
	out := []oruby.Value{self}
	seen := map[oruby.RClass]bool{}
	add := func(v oruby.Value) {
		if v.Type() != oruby.MrbTTClass && v.Type() != oruby.MrbTTModule {
			return
		}
		c := mrb.ClassPtr(v)
		if seen[c] {
			return
		}
		seen[c] = true
		out = append(out, v)
	}
	seen[mrb.ClassPtr(self)] = true
	if path := mrb.ClassPtr(self).Name(); strings.Contains(path, "::") {
		parts := strings.Split(path, "::")
		var chain []oruby.Value
		cur := mrb.ObjectClass()
		for _, part := range parts[:len(parts)-1] {
			if !cur.ConstDefinedAt(part) {
				break
			}
			v := cur.ConstGet(part)
			if v.Type() != oruby.MrbTTClass && v.Type() != oruby.MrbTTModule {
				break
			}
			chain = append(chain, v)
			cur = mrb.ClassPtr(v)
		}
		for i := len(chain) - 1; i >= 0; i-- {
			add(chain[i])
		}
	}
	if anc, err := mrb.Funcall(self, mrb.Intern("ancestors")); err == nil && anc.Type() == oruby.MrbTTArray {
		for _, a := range oruby.RArraySlice(anc) {
			add(a)
		}
	}
	add(mrb.ObjectClass().Value())
	return out
}

func loadConstMissing(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.GetArgsFirst()
	id, ok := autoloadName(mrb, name)
	if ok {
		key := mrb.SymbolValue(id)
		for _, owner := range autoloadCandidates(mrb, self) {
			entries := autoloadEntries(mrb, owner, false)
			if entries.IsNil() {
				continue
			}
			path := mrb.HashGet(entries, key)
			if path.IsNil() {
				continue
			}
			mrb.HashDeleteKey(entries, key)
			result := doRequire(mrb, path.String())
			if mrb.HasExc() {
				if !mrb.ConstDefinedAt(owner, id) {
					mrb.HashSet(entries, key, path)
				}
				return result
			}
			if mrb.ConstDefinedAt(owner, id) {
				return mrb.ConstGet(owner, id)
			}
			break
		}
	}

	constant := "unknown"
	if name.Type() == oruby.MrbTTSymbol {
		constant = mrb.SymName(name.Symbol())
	} else if name.Type() == oruby.MrbTTString {
		constant = name.String()
	}
	if owner := mrb.ClassPtr(self).Name(); owner != "" && owner != "Object" {
		constant = owner + "::" + constant
	}
	return mrb.Raisef(mrb.ENameError(), "uninitialized constant %s", constant)
}
