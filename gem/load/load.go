package load

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/oruby/oruby"
)

// Kernel#require, #require_relative and #load as MRI's load.c does them
// (search_required, rb_feature_p, rb_find_file_ext, rb_require_relative,
// rb_f_load):
//
//   - a feature is a String, #to_path or #to_str
//   - $LOADED_FEATURES decides what is loaded: an entry matches the feature
//     as given (with or without an extension) or as reached through a
//     $LOAD_PATH entry; a C extension loaded under the name lets a .rb of
//     it load, a .rb one makes the name loaded
//   - a path starting with "/", "./", "../" or "~" is expanded; any other
//     is searched in $LOAD_PATH (canonical: entries are real paths), the
//     .rb (and .mrb) form before the C extension one
//   - the stored feature is the expanded path; a file reached through
//     another path to the same real file is not loaded again
//   - C extensions are found but not loaded (LoadError)
//   - MRI's built-in features are in $LOADED_FEATURES from the start

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

		// MRI 4.1's features loaded before the program (provided here by
		// core_ext or by Go gems, initialized when required)
		loadedFeatures := mrb.AryNew()
		for _, f := range []string{"enumerator.so", "monitor.so", "thread.rb", "fiber.so", "rational.so",
			"complex.so", "pathname.so", "ruby2_keywords.rb", "set.rb", "io/wait.rb", "io/wait.so"} {
			loadedFeatures.Push(mrb.StrNew(f))
		}
		mrb.SetGV("$LOADED_FEATURES", loadedFeatures)
		mrb.SetGV(`$"`, loadedFeatures)

		loadError := mrb.DefineClass("LoadError", mrb.EScriptError())
		mrb.DefineGlobalConst("ELoadError", loadError)

		mrb.DefineGlobalFunction("load", loadLoad, mrb.ArgsArg(1, 1))
		mrb.DefineGlobalFunction("require", loadRequire, mrb.ArgsReq(1))
		mrb.DefineGlobalFunction("require_relative", loadRequireRelative, mrb.ArgsReq(1))
		// Module#autoload is mruby's own; it loads files with the require above
		kernel := mrb.KernelModule()
		kernel.DefineClassMethod("__oruby_autoload_registered", autoloadRegistered, mrb.ArgsReq(3))
		kernel.DefineClassMethod("__oruby_autoload_other_thread?", autoloadOtherThread, mrb.ArgsReq(1))
		kernel.DefineClassMethod("__oruby_autoload_wait", autoloadWait, mrb.ArgsReq(1))
		return make(map[string]*loadLock)
	})
}

// dlexts are the C extension suffixes searched for (MRI's DLEXT)
var dlexts = func() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{".bundle"}
	case "windows":
		return []string{".so", ".dll"}
	}
	return []string{".so"}
}()

func isSoExt(e string) bool {
	switch e {
	case ".so", ".o", ".bundle", ".dylib", ".dll":
		return true
	}
	return false
}

// extKind: the feature's kind by its extension: 'r' (Ruby), 's' (C
// extension), 0 (none, or one require does not know)
func extKind(f string) byte {
	switch e := filepath.Ext(f); {
	case e == ".rb" || e == ".mrb":
		return 'r'
	case isSoExt(e):
		return 's'
	}
	return 0
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

	return proc, nil
}

func doLoad(mrb *oruby.MrbState, fullName string) error {
	var err error

	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.Filename(fullName)
	// a syntax error is the SyntaxError's message ("file:line: ..."), as
	// MRI's, not printed
	cxt.SetCaptureErrors(true)

	switch filepath.Ext(fullName) {
	case ".mrb":
		_, err = cxt.LoadIrepFile(fullName)
	default:
		if src, ok := coverageSource(mrb, fullName); ok {
			_, err = mrb.LoadBytesCxt(src, cxt)
		} else {
			_, err = mrb.LoadFileCxt(fullName, cxt)
		}
		if err != nil {
			syntaxErrorAt(mrb, fullName)
		}
	}

	return err
}

// syntaxErrorAt gives the pending SyntaxError of loading file MRI's
// "file:line: message" and its #path (mruby's captured error has only the
// message; the line is the parser's first error)
func syntaxErrorAt(mrb *oruby.MrbState, file string) {
	exc := mrb.Exc()
	if exc == nil || !mrb.ClassDefined("SyntaxError") || !mrb.ObjIsKindOf(exc.Value(), mrb.ClassGet("SyntaxError")) {
		return
	}
	msg := mrb.String(mrb.ExcMessage(exc.Value()))
	if strings.HasPrefix(msg, file+":") {
		return
	}
	line := 1
	cxt := mrb.MrbCcontextNew()
	cxt.SetCaptureErrors(true)
	cxt.Filename(file)
	if p, perr := mrb.ParseFile(file, cxt); perr == nil {
		if p.NErr() > 0 {
			line = int(p.ErrorBuffer(0).LineNo)
		}
		p.Free()
	}
	cxt.Free()
	_ = mrb.SetExcMessage(exc.Value(), mrb.StrNew(fmt.Sprintf("%s:%d: %s", file, line, msg)))
	_ = mrb.IVSet(exc.Value(), mrb.Intern("@path"), mrb.StrNew(file))
}

// coverageSource is the file's source with Coverage's counters, while
// coverage is set up (gem/coverage); ok false otherwise
func coverageSource(mrb *oruby.MrbState, fullName string) ([]byte, bool) {
	if !mrb.ClassDefined("Coverage") {
		return nil, false
	}
	cov := mrb.ModuleGet("Coverage")
	if !mrb.RespondTo(cov.Value(), mrb.Intern("__oruby_instrument")) {
		return nil, false
	}
	v, err := mrb.Funcall(cov.Value(), mrb.Intern("__oruby_instrument"), fullName)
	if err != nil || !v.IsString() {
		return nil, false
	}
	return v.Bytes(), true
}

// doLoadWrapped is load(file, wrap): the file's code runs with a copy of
// main extended with mod as self and mod as where its definitions go; its
// top-level methods are private, as MRI's
func doLoadWrapped(mrb *oruby.MrbState, fullName string, mod oruby.Value) error {
	data, err := os.ReadFile(fullName)
	if err != nil {
		return err
	}
	main := mrb.TopSelf()
	self, err := mrb.Funcall(main, mrb.Intern("clone"))
	if err != nil {
		return err
	}
	if _, err := mrb.Funcall(self, oruby.Sym_extend, mod); err != nil {
		return err
	}
	if err := mrb.IVSet(self, oruby.Sym___oruby_load_wrapper, mod); err != nil {
		return err
	}
	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.Filename(fullName)
	if filepath.Ext(fullName) == ".mrb" {
		_, err := mrb.LoadIrepBytesWrapped(data, cxt, mrb.ClassPtr(mod), self)
		return err
	}
	if src, ok := coverageSource(mrb, fullName); ok {
		data = src
	}
	_, err = mrb.LoadBytesWrapped(data, cxt, mrb.ClassPtr(mod), self)
	return err
}

// loadLock is MRI's per-feature load lock: the thread requiring a file
// (by real path) and the threads waiting for it to finish
type loadLock struct {
	owner    *oruby.Thread
	waiters  []*oruby.Waiter
	marks    []autoloadMark
	autoload bool // the require started at a native autoload lookup
}

type autoloadMark struct {
	mod, name oruby.Value
	during    bool // registered while the file was loading
	trigger   bool // its constant lookup started the require
}

func loadLocks(mrb *oruby.MrbState) map[string]*loadLock {
	locks, _ := mrb.GemData("load").(map[string]*loadLock)
	return locks
}

func (lk *loadLock) ownedBy(thread *oruby.Thread) bool {
	return lk.owner == thread || lk.owner == nil && thread != nil && thread.Main()
}

func (lk *loadLock) wait(mrb *oruby.MrbState) error {
	w := mrb.NewWaiter()
	lk.waiters = append(lk.waiters, w)
	_, err := mrb.ThreadWait(w.C(), -1, true)
	return err
}

// the files being required, by real path (a circular require), per state
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
	feature, err := pathArg(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	lp := expandedLoadPath(mrb)
	exts := []string{""}
	if extKind(feature) == 0 {
		exts = append([]string{".rb", ".mrb"}, dlexts...)
	}
	name, _ := findFile(feature, exts, lp)
	if name == "" {
		// a library built into oruby (a Go gem): Ruby code of no file
		if !isExplicit(feature) && mrb.GemAvailable(feature) {
			return mrb.AryNewFromValues(mrb.SymbolValue(mrb.Intern("rb")), mrb.StrNew("<internal:oruby>/"+feature+".rb"))
		}
		return mrb.NilValue()
	}
	// MRI's [type, path]: :rb for Ruby code, :so for an extension
	kind := "rb"
	if extKind(name) == 's' {
		kind = "so"
	}
	return mrb.AryNewFromValues(mrb.SymbolValue(mrb.Intern(kind)), mrb.StrNew(name))
}

// load(file, wrap = false): file is looked up as require does (without
// extensions), then as given; it is run every time, not recorded
func loadLoad(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	feature, err := pathArg(mrb, args.Item(0))
	if err != nil {
		return mrb.RaiseError(err)
	}
	wrap := args.Item(1)

	path, _ := findFile(feature, []string{""}, expandedLoadPath(mrb))
	if path == "" && fileLoadOK(feature) {
		path = feature
	}
	if path == "" {
		return raiseCannotLoad(mrb, feature)
	}

	// the file being run (require_relative's fallback base); load does not
	// count as requiring it, so a require of it inside loads it once
	prev := mrb.GetGV(`$"__`)
	mrb.SetGV(`$"__`, mrb.StrNew(path))
	defer mrb.SetGV(`$"__`, prev)

	if wrap.Bool() {
		mod := wrap
		if mod.Type() != oruby.MrbTTModule {
			m, err := mrb.Funcall(mrb.ModuleClass().Value(), oruby.Sym_new)
			if err != nil {
				return mrb.RaiseError(err)
			}
			mod = m.Value()
		}
		err = doLoadWrapped(mrb, path, mod)
	} else {
		err = doLoad(mrb, path)
	}
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

// raiseCExtension: a C extension was found; oruby does not load them (MRI
// fails in dlopen here when the file is no library: path stays nil)
func raiseCExtension(mrb *oruby.MrbState, path string) oruby.MrbValue {
	return mrb.Raisef(mrb.ClassGet("LoadError"), "dlopen(%v): oruby does not load C extensions", path)
}

// loadFailure re-raises exception raised by loaded code (SyntaxError,
// SystemExit, NameError...) instead of hiding it in a LoadError
func loadFailure(mrb *oruby.MrbState, err error) oruby.MrbValue {
	if exc := mrb.Exc(); exc != nil {
		return exc.Value()
	}
	return mrb.RaiseError(err)
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

// fileLoadOK: p is a file that can be read
func fileLoadOK(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// isExplicit: a path require does not search $LOAD_PATH for
func isExplicit(f string) bool {
	return filepath.IsAbs(f) || f == "~" || strings.HasPrefix(f, "~") ||
		strings.HasPrefix(f, "./") || strings.HasPrefix(f, "../") || f == "." || f == ".."
}

// expandPath is File.expand_path: "~" and "~user" too
func expandPath(f string) string {
	if strings.HasPrefix(f, "~") {
		rest := f[1:]
		name := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name, rest = rest[:i], rest[i:]
		} else {
			rest = ""
		}
		home := ""
		if name == "" {
			home = os.Getenv("HOME")
		} else if u, err := user.Lookup(name); err == nil {
			home = u.HomeDir
		}
		if home != "" {
			f = home + rest
		}
	}
	if a, err := filepath.Abs(f); err == nil {
		return a
	}
	return f
}

var (
	realpathMu    sync.Mutex
	realpathCache = map[string]string{}
)

// realpathOf is filepath.EvalSymlinks, cached: $LOADED_FEATURES entries and
// $LOAD_PATH entries are compared by real path on every require
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

// expandedLoadPath: $LOAD_PATH entries (String or #to_path), expanded and
// canonical (MRI's expanded load path, realpath'd)
func expandedLoadPath(mrb *oruby.MrbState) []string {
	lpv := mrb.GVGet(oruby.GvSym_LOAD_PATH)
	if !lpv.IsArray() {
		return nil
	}
	a := mrb.GVGetObj(oruby.GvSym_LOAD_PATH).RArray()
	out := make([]string, 0, a.Len())
	for i := 0; i < a.Len(); i++ {
		p, err := pathArg(mrb, a.Item(i))
		if err != nil {
			continue
		}
		out = append(out, realpathOf(expandPath(p)))
	}
	return out
}

// findFile: the first file feature+ext names: an explicit path expanded,
// any other in a $LOAD_PATH entry
func findFile(feature string, exts, lp []string) (string, string) {
	if isExplicit(feature) {
		base := expandPath(feature)
		for _, e := range exts {
			if fileLoadOK(base + e) {
				return base + e, e
			}
		}
		return "", ""
	}
	// every directory for one extension before the next: a .rb later on
	// the path wins over a C extension earlier
	for _, e := range exts {
		for _, dir := range lp {
			p := filepath.Join(dir, feature+e)
			if fileLoadOK(p) {
				return p, e
			}
		}
	}
	return "", ""
}

// featureP is MRI's rb_feature_p: how $LOADED_FEATURES has feature: 'r' as
// Ruby, 's' as a C extension, 'u' by the name alone, 0 not. kind is the
// feature's own extension kind (0: none); lp (nil for an expanded path)
// also matches entries reached through a $LOAD_PATH directory.
func featureP(mrb *oruby.MrbState, feature string, kind byte, lp []string) byte {
	lf := mrb.GVGet(oruby.GvSym_LOADED_FEATURES)
	if !lf.IsArray() {
		return 0
	}
	match := func(e, base string) byte {
		if kind != 0 {
			if e == base {
				return kind
			}
			return 0
		}
		if e == base {
			return 'u'
		}
		if strings.HasPrefix(e, base) {
			rest := e[len(base):]
			switch {
			case rest == ".rb" || rest == ".mrb":
				return 'r'
			case isSoExt(rest):
				return 's'
			}
		}
		return 0
	}
	viaLoadPath := lp != nil && !isExplicit(feature)
	for i := 0; i < oruby.RArrayLen(lf); i++ {
		ev := mrb.AryEntry(lf, i)
		if !ev.IsString() {
			continue
		}
		e := mrb.String(ev)
		if !strings.Contains(e, feature) {
			continue
		}
		if k := match(e, feature); k != 0 {
			return k
		}
		if viaLoadPath {
			for _, dir := range lp {
				if k := match(e, dir+"/"+feature); k != 0 {
					return k
				}
			}
		}
	}
	return 0
}

// loadedByRealpath: $LOADED_FEATURES has a path to the same real file
func loadedByRealpath(mrb *oruby.MrbState, name, real string) bool {
	lf := mrb.GVGet(oruby.GvSym_LOADED_FEATURES)
	if !lf.IsArray() {
		return false
	}
	for i := 0; i < oruby.RArrayLen(lf); i++ {
		ev := mrb.AryEntry(lf, i)
		if !ev.IsString() {
			continue
		}
		s := mrb.String(ev)
		if s == name || s == real || filepath.IsAbs(s) && realpathOf(s) == real {
			return true
		}
	}
	return false
}

// upstreamLib returns the shipped upstream copy of library feature in the
// install layout's rubylibdir (lib/oruby/<ruby_version>), or "". An upstream
// copy is preferred over ORuby's embedded rewrite of the same library; the
// rewrite stays the fallback for embedded use without the install layout.
func upstreamLib(feature string) string {
	if feature == "" || isExplicit(feature) {
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
	if feature == "" {
		return raiseCannotLoad(mrb, feature)
	}
	if p := upstreamLib(feature); p != "" {
		feature = p
	}
	lp := expandedLoadPath(mrb)

	// a Go gem: initialized once; MRI's built-in features stay "loaded"
	if !isExplicit(feature) && mrb.GemAvailable(feature) {
		provided := featureP(mrb, feature, 0, lp) != 0
		loaded, err := mrb.Resolve(feature)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if provided || !loaded {
			return mrb.FalseValue()
		}
		mrb.AryPush(mrb.GVGet(oruby.GvSym_LOADED_FEATURES), mrb.StrNew(feature))
		return mrb.TrueValue()
	}

	switch extKind(feature) {
	case 'r':
		if featureP(mrb, feature, 'r', lp) != 0 {
			return mrb.FalseValue()
		}
		path, _ := findFile(feature, []string{""}, lp)
		if path == "" {
			return raiseCannotLoad(mrb, feature)
		}
		return requireFile(mrb, path)
	case 's':
		if featureP(mrb, feature, 's', lp) != 0 {
			return mrb.FalseValue()
		}
		// .so and .o name the platform's library suffix
		base, exts := feature, []string{""}
		if e := filepath.Ext(feature); e == ".so" || e == ".o" {
			base, exts = strings.TrimSuffix(feature, e), dlexts
		}
		path, _ := findFile(base, exts, lp)
		if path == "" {
			return raiseCannotLoad(mrb, feature)
		}
		if featureP(mrb, path, 's', nil) != 0 {
			return mrb.FalseValue()
		}
		return raiseCExtension(mrb, path)
	}

	ft := featureP(mrb, feature, 0, lp)
	if ft == 'r' {
		return mrb.FalseValue()
	}
	exts := []string{".rb", ".mrb"}
	if ft != 's' {
		exts = append(exts, dlexts...)
	}
	path, e := findFile(feature, exts, lp)
	if path == "" {
		if ft != 0 {
			return mrb.FalseValue() // provided under the name
		}
		return raiseCannotLoad(mrb, feature)
	}
	if isSoExt(e) {
		if featureP(mrb, path, 's', nil) != 0 {
			return mrb.FalseValue()
		}
		return raiseCExtension(mrb, path)
	}
	return requireFile(mrb, path)
}

// requireFile loads the Ruby file at (expanded) path unless it is loaded or
// being loaded, and records it in $LOADED_FEATURES
func requireFile(mrb *oruby.MrbState, path string) oruby.MrbValue {
	// a file reached through a symlink is the same feature as its real
	// path, as in MRI
	real := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		real = r
	}
	locks := loadLocks(mrb)
	for {
		if featureP(mrb, path, 'r', nil) != 0 || loadedByRealpath(mrb, path, real) {
			return mrb.FalseValue()
		}
		lk := locks[real]
		if lk == nil {
			break
		}
		if lk.ownedBy(mrb.CurrentThread()) {
			warnCircular(mrb, path)
			return mrb.FalseValue()
		}
		// another thread loads it: wait for it, then look again (it is
		// loaded, or that require failed and this one loads it)
		if err := lk.wait(mrb); err != nil {
			return mrb.RaiseError(err)
		}
	}
	realValue := mrb.StrNew(real)
	lk := &loadLock{owner: mrb.CurrentThread()}
	locks[real] = lk
	defer func() {
		delete(locks, real)
		for _, w := range lk.waiters {
			w.Wake()
		}
	}()

	loadingFilesAdd(mrb, realValue)
	defer loadingFilesDelete(mrb, realValue)
	lk.markAutoloads(mrb, path, real)
	if err := doLoad(mrb, path); err != nil {
		exc := mrb.Exc()
		if exc != nil {
			mrb.GCProtect(exc.Value())
			mrb.ExcClear()
		}
		lk.finishAutoloads(mrb, false)
		if exc != nil {
			return mrb.RaiseObj(exc.Value())
		}
		return loadFailure(mrb, err)
	}
	mrb.AryPush(mrb.GVGet(oruby.GvSym_LOADED_FEATURES), mrb.StrNew(path))
	lk.finishAutoloads(mrb, true)
	return mrb.TrueValue()
}

func autoloadFile(path string, lp []string) string {
	exts := []string{""}
	if extKind(path) == 0 {
		exts = []string{".rb", ".mrb"}
	}
	full, _ := findFile(path, exts, lp)
	return full
}

// markAutoloads finds pending registrations for this file, including paths
// reached through the load path or a symlink.
func (lk *loadLock) markAutoloads(mrb *oruby.MrbState, path, real string) {
	reg := mrb.IVGet(mrb.KernelModule(), oruby.Sym___oruby_autoloads)
	if !reg.IsArray() {
		return
	}
	var lp []string
	n := oruby.RArrayLen(reg)
	for i := 0; i < n; i++ {
		e := mrb.AryRef(reg, i)
		if !e.IsArray() || oruby.RArrayLen(e) != 3 {
			continue
		}
		mod, sym, p := mrb.AryRef(e, 0), mrb.AryRef(e, 1), mrb.AryRef(e, 2)
		feature := mrb.String(p)
		if lp == nil && !isExplicit(feature) {
			lp = expandedLoadPath(mrb)
		}
		full := autoloadFile(feature, lp)
		if full == "" || (full != path && realpathOf(full) != real) {
			continue
		}
		lk.markAutoload(mrb, mod, sym, p, real, false)
	}
}

func (lk *loadLock) markAutoload(mrb *oruby.MrbState, mod, sym, path oruby.Value, real string, during bool) {
	status, err := mrb.Funcall(mod, oruby.Sym___oruby_autoload_loading, sym, path, real)
	if err == nil && status.Int() != 0 {
		trigger := status.Int() == 2
		lk.autoload = lk.autoload || trigger
		lk.marks = append(lk.marks, autoloadMark{mod, sym, during, trigger})
	}
}

func (lk *loadLock) finishAutoloads(mrb *oruby.MrbState, loaded bool) {
	for i := len(lk.marks) - 1; i >= 0; i-- {
		mark := lk.marks[i]
		// An autoload can share its file with other registrations; leave
		// siblings pending so they can load it again if the feature is removed.
		mode := 0
		if loaded && mark.trigger {
			mode = 2
		} else if loaded && (!lk.autoload || mark.during) {
			mode = 1
		}
		_, _ = mrb.Funcall(mark.mod, oruby.Sym___oruby_autoload_spend, mark.name, mode)
	}
}

func autoloadRegistered(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs()
	p := a.Item(2)
	path := mrb.String(p)
	var lp []string
	if !isExplicit(path) {
		lp = expandedLoadPath(mrb)
	}
	full := autoloadFile(path, lp)
	if full != "" {
		real := realpathOf(full)
		if lock := loadLocks(mrb)[real]; lock != nil {
			lock.markAutoload(mrb, a.Item(0), a.Item(1), p, real, true)
		} else if featureP(mrb, full, 'r', nil) != 0 || loadedByRealpath(mrb, full, real) {
			lock := loadLock{}
			lock.markAutoload(mrb, a.Item(0), a.Item(1), p, real, true)
			lock.finishAutoloads(mrb, true)
		}
	}
	return mrb.NilValue()
}

func autoloadOtherThread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	lock := loadLocks(mrb)[mrb.String(mrb.GetArgsFirst())]
	return mrb.BoolValue(lock != nil && !lock.ownedBy(mrb.CurrentThread()))
}

func autoloadWait(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	real := mrb.String(mrb.GetArgsFirst())
	waited := false
	for lock := loadLocks(mrb)[real]; lock != nil && !lock.ownedBy(mrb.CurrentThread()); lock = loadLocks(mrb)[real] {
		if err := lock.wait(mrb); err != nil {
			return mrb.RaiseError(err)
		}
		waited = true
	}
	return mrb.BoolValue(waited)
}

// warnCircular is MRI's verbose-only warning for a require of a file that
// is being required
func warnCircular(mrb *oruby.MrbState, path string) {
	if v := mrb.GetGV("$VERBOSE"); v.Type() != oruby.MrbTTTrue {
		return
	}
	if mrb.RespondTo(mrb.KernelModule().Value(), oruby.Sym_warn) {
		_, _ = mrb.Funcall(mrb.KernelModule().Value(), oruby.Sym_warn,
			"warning: loading in progress, circular require considered harmful - "+path)
	}
}

func loadRequire(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	feature, err := pathArg(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	return doRequire(mrb, feature)
}

// pathArg converts a feature or $LOAD_PATH entry as rb_get_path does: a
// String, else #to_path, then #to_str of what that gives
func pathArg(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	if v.IsString() {
		return mrb.String(v), nil
	}
	if !v.IsNil() && mrb.RespondTo(v, oruby.Sym_to_path) {
		p, err := mrb.Funcall(v, oruby.Sym_to_path)
		if err != nil {
			return "", err
		}
		v = p.Value()
		if v.IsString() {
			return mrb.String(v), nil
		}
	}
	cname := func(x oruby.Value) string {
		switch {
		case x.IsNil():
			return "nil"
		case x.Type() == oruby.MrbTTTrue:
			return "true"
		case x.Type() == oruby.MrbTTFalse:
			return "false"
		}
		return mrb.ClassName(mrb.ObjClass(x))
	}
	if v.IsNil() || !mrb.RespondTo(v, oruby.Sym_to_str) {
		return "", oruby.ETypeError("no implicit conversion of %s into String", cname(v))
	}
	s, err := mrb.Funcall(v, oruby.Sym_to_str)
	if err != nil {
		return "", err
	}
	if !s.IsString() {
		return "", oruby.ETypeError("can't convert %s into String (%s#to_str gives %s)", cname(v), cname(v), cname(s.Value()))
	}
	return mrb.String(s), nil
}

// callerFile returns the path recorded when the calling file was opened.
// An eval with a supplied filename can still provide its display path.
func callerFile(mrb *oruby.MrbState) string {
	res, err := mrb.Funcall(mrb.TopSelf(), oruby.Sym_caller_locations, 0, 1)
	if err != nil || !res.IsArray() || oruby.RArrayLen(res) == 0 {
		return ""
	}
	loc := mrb.AryEntry(res, 0)
	if path, err := mrb.Funcall(loc, oruby.Sym_absolute_path); err == nil && path.IsString() {
		if absolute := mrb.String(path); filepath.IsAbs(absolute) {
			return absolute
		}
	}
	path, err := mrb.Funcall(loc, oruby.Sym_path)
	if err != nil || !path.IsString() {
		return ""
	}
	base := expandPath(mrb.String(path))
	if real, err := filepath.EvalSymlinks(base); err == nil {
		return real
	}
	return base
}

// require_relative(path): path relative to the directory of the real path
// of the calling file (MRI's rb_current_realfilepath)
func loadRequireRelative(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	feature, err := pathArg(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	if !filepath.IsAbs(feature) {
		file := callerFile(mrb)
		if file == "" {
			if v := mrb.GVGet(mrb.Intern(`$"__`)); v.IsString() {
				file = mrb.String(v)
			}
		}
		if file == "" {
			return mrb.Raise(mrb.ClassGet("LoadError"), "cannot infer basepath")
		}
		feature = filepath.Join(filepath.Dir(file), feature)
	}
	return doRequire(mrb, feature)
}
