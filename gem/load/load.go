package load

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
		for _, p := range oruby.DefaultLoadPath() {
			loadPath.Push(mrb.StrNew(p))
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
		// Module#autoload is mruby's own; it loads files with the require above
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
	_ = mrb.IVSet(exc, oruby.IvSym_path, mrb.StrNew(feature))
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

	paths := mrb.GVGetObj(oruby.GvSym_LOAD_PATH).RArray()
	for i := 0; i < paths.Len(); i++ {
		path, err := pathArg(mrb, paths.Item(i))
		if err != nil {
			continue
		}
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

// upstreamLib returns the shipped upstream copy of library feature in the
// install layout's rubylibdir (lib/oruby/<ruby_version>), or "". An upstream
// copy is preferred over ORuby's embedded rewrite of the same library; the
// rewrite stays the fallback for embedded use without the install layout.
func upstreamLib(feature string) string {
	if feature == "" || filepath.IsAbs(feature) || strings.HasPrefix(feature, ".") {
		return ""
	}
	name := feature
	if !strings.HasSuffix(name, ".rb") {
		name += ".rb"
	}
	p := filepath.Join(oruby.RubyLibPrefix(), oruby.RubyVersion, name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

func doRequire(mrb *oruby.MrbState, feature string) oruby.MrbValue {
	if p := upstreamLib(feature); p != "" {
		feature = p
	}
	// If there is implemented Go feature
	if loaded, err := mrb.Resolve(feature); err == nil {
		if loaded {
			loadedFeatures := mrb.GVGet(oruby.GvSym_LOADED_FEATURES)
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

	// A file reached through a symlink is the same feature as its real path,
	// as in MRI: $LOADED_FEATURES keeps the expanded path, but the checks
	// below also go by the realpath
	real := name
	if r, err := filepath.EvalSymlinks(name); err == nil {
		real = r
	}

	// already loaded: MRI goes by $LOADED_FEATURES, which programs (and
	// specs) edit or replace
	if featureLoaded(mrb, name, real) {
		return mrb.FalseValue()
	}

	nameValue := mrb.StrNew(name)
	realValue := mrb.StrNew(real)

	// Check if file is set to be loaded
	if loadingFilesFind(mrb, realValue) {
		return mrb.FalseValue()
	}

	// Load feature
	loadingFilesAdd(mrb, realValue)
	defer loadingFilesDelete(mrb, realValue)
	if err := doLoad(mrb, name, false); err != nil {
		return loadFailure(mrb, err)
	}

	// Remember loaded feature
	loadedFeatures := mrb.GVGet(oruby.GvSym_LOADED_FEATURES)
	mrb.AryPush(loadedFeatures, nameValue)
	mrb.FeatureAdd(name)
	mrb.FeatureAdd(real)

	return mrb.TrueValue()
}

func loadRequire(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	feature, err := pathArg(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	return doRequire(mrb, feature)
}

// pathArg converts a feature or $LOAD_PATH entry: a String, #to_path, then
// #to_str (rb_get_path)
func pathArg(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	if v.IsString() {
		return v.String(), nil
	}
	if !v.IsNil() && mrb.RespondTo(v, oruby.Sym_to_path) {
		p, err := mrb.Funcall(v, oruby.Sym_to_path)
		if err != nil {
			return "", err
		}
		v = p
		if v.IsString() {
			return v.String(), nil
		}
	}
	s, err := mrb.Funcall(mrb.KernelModule(), oruby.Sym___implicit, v, mrb.StringClass().Value())
	if err != nil {
		return "", err
	}
	return s.String(), nil
}

var (
	realpathMu    sync.Mutex
	realpathCache = map[string]string{}
)

// realpathOf is filepath.EvalSymlinks, cached: $LOADED_FEATURES entries are
// compared by real path on every require
func realpathOf(p string) string {
	realpathMu.Lock()
	defer realpathMu.Unlock()
	if r, ok := realpathCache[p]; ok {
		return r
	}
	r := p
	if e, err := filepath.EvalSymlinks(p); err == nil {
		r = e
	}
	realpathCache[p] = r
	return r
}

// featureLoaded reports whether $LOADED_FEATURES holds name, or a path with
// the same real path, as MRI's loaded-features realpath index does
func featureLoaded(mrb *oruby.MrbState, name, real string) bool {
	lf := mrb.GVGet(oruby.GvSym_LOADED_FEATURES)
	if !lf.IsArray() {
		return false
	}
	for i := 0; i < oruby.RArrayLen(lf); i++ {
		e := mrb.AryEntry(lf, i)
		if !e.IsString() {
			continue
		}
		s := e.String()
		if s == name || s == real || filepath.IsAbs(s) && realpathOf(s) == real {
			return true
		}
	}
	return false
}

func loadRequireRelative(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	feature := mrb.GetArgsFirst().String()

	var file string
	res, err := mrb.Funcall(mrb.TopSelf(), oruby.Sym_caller, 0)
	if err == nil && res.Type() == oruby.MrbTTArray {
		ary, _ := mrb.EnsureArrayType(res) // res is an Array: checked above
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
