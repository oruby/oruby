package oruby

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var gems = make(map[string]func(*MrbState) interface{})

// lazyGems are initialized on first require, not when state is created
var lazyGems = make(map[string]func(*MrbState) interface{})

// LazyGem registers gem which is initialized by `require name` (stdlib
// style libraries), keeping interpreter startup fast.
func LazyGem(name string, initFn func(*MrbState) interface{}) {
	if name == "" {
		panic("error - empty name not allowed")
	}
	if _, dup := lazyGems[name]; dup {
		panic("lazy gem register called twice for gem " + name)
	}
	if _, dup := gems[name]; dup {
		panic("gem register called twice for gem " + name)
	}
	lazyGems[name] = initFn
}

// GemAlias registers require name which loads (or is provided by) gem
// target, e.g. "stringio" provided by "io".
func GemAlias(name, target string) {
	LazyGem(name, func(mrb *MrbState) interface{} {
		if _, err := mrb.Resolve(target); err != nil {
			panic(fmt.Sprintf("gem alias %v: %v", name, err))
		}
		return nil
	})
}

// sortedGemNames returns eagerly initialized gem names in stable order
func sortedGemNames() []string {
	names := make([]string, 0, len(gems))
	for k := range gems {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

//type MrbInitFunc func(*MrbState)

// Gem register makes a gem available by the provided name.
// If Register is called twice with the same name it panics.
func Gem(name string, initFn func(*MrbState) interface{}) {
	if name == "" {
		panic("error - empty name not allowed")
	}

	if _, dup := gems[name]; dup {
		panic("gem register called twice for gem " + name)
	}
	gems[name] = initFn
}

// GemNames lists the names of the registered Go gems, sorted: what `require`
// can find among them.
func GemNames() []string {
	names := make([]string, 0, len(gems)+len(lazyGems))
	for n := range gems {
		names = append(names, n)
	}
	for n := range lazyGems {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// GemExists checks if gem was already required
func GemExists(name string) bool {
	_, exists := gems[name]
	return exists
}

// GemData retreives data initialized with to gem
func (mrb *MrbState) GemData(name string) interface{} {
	return mrb.features[name]
}

// Resolve inits gem from available Go gems if not already initialized
// returns:
//
//	false - if feature is already initialized
//	true - if feature is found and initialized
//
//	error is returned if feature cannot be found
func (mrb *MrbState) Resolve(name string) (bool, error) {
	if name == "" {
		return false, EArgumentError("error - empty feature name")
	}

	_, loaded := mrb.features[name]
	if loaded {
		return false, nil
	}

	initFn, exists := gems[name]
	if !exists {
		initFn, exists = lazyGems[name]
	}
	if !exists {
		return false, EKeyError("error loading '%v'", name)
	}

	// mark first, so recursive requires of the same feature terminate
	mrb.features[name] = nil
	data := initFn(mrb)
	if ie, ok := data.(*InitError); ok {
		delete(mrb.features, name)
		return true, ie
	}
	mrb.features[name] = data
	return true, nil
}

// InitError is what a gem's init function returns when the gem could not
// be set up: Resolve reports it (its Err, a Ruby exception from a Funcall
// among them, is what require raises) and the gem stays unloaded.
type InitError struct{ Err error }

func (e *InitError) Error() string { return e.Err.Error() }
func (e *InitError) Unwrap() error { return e.Err }

// GemSpec describes a compiled-in Go gem so RubyGems and Bundler can discover
// and satisfy dependencies on it without external gemspec files or C builds.
type GemSpec struct {
	Name    string   // Gem name (e.g. "nokogiri", "oruby-vips", "ruby-vips")
	Version string   // Gem version string (e.g. "1.19.7")
	Feature string   // Registered Go gem/lazyGem feature name to verify availability
	Summary string   // Short one-line summary
	Files   []string // Requirable library files (e.g. "lib/nokogiri.rb")
}

var builtinGemSpecs []GemSpec

// RegisterGemSpec registers metadata for a built-in Go gem.
func RegisterGemSpec(spec GemSpec) {
	builtinGemSpecs = append(builtinGemSpecs, spec)
}

// BuiltinGemSpecs returns a copy of all registered built-in gem specs.
func BuiltinGemSpecs() []GemSpec {
	out := make([]GemSpec, len(builtinGemSpecs))
	copy(out, builtinGemSpecs)
	return out
}

// EmbeddedGemVersion is the version of the embedded gem name (a Go gem
// with a GemSpec): it stands in for the gem's versions up to its own.
func EmbeddedGemVersion(name string) (string, bool) {
	for _, sp := range builtinGemSpecs {
		if sp.Name == name {
			return sp.Version, true
		}
	}
	return "", false
}

// CompareGemVersions compares dotted release versions ("1.8.7", "1.10"):
// -1, 0 or 1, numerically per segment; a missing segment is 0.
func CompareGemVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// GemDirVersion is the gem name and version of a path inside a gem's
// directory (".../gems/<name>-<version>[-<platform>]/..."), as RubyGems
// installs it and `oruby build` packs it.
func GemDirVersion(path string) (name, version string, ok bool) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] != "gems" {
			continue
		}
		dir := parts[i+1]
		for j := 1; j < len(dir)-1; j++ {
			if dir[j] == '-' && dir[j+1] >= '0' && dir[j+1] <= '9' {
				version, _, _ = strings.Cut(dir[j+1:], "-")
				return dir[:j], version, true
			}
		}
	}
	return "", "", false
}

var nativeExtensionGems = map[string]bool{}

var afterLoad = map[string][]string{}

// AfterLoad registers Ruby source that runs right after require loads the
// library file lib of the ones ORuby ships unchanged (a path below
// lib/oruby/<version>, as "bundler/resolver.rb"): ORuby's adjustment to an
// upstream library, made where the library defines what it changes.
func AfterLoad(lib, src string) { afterLoad[lib] = append(afterLoad[lib], src) }

// AfterLoadSources is what AfterLoad registered for the file at path, nil
// for a file that is not one of ORuby's libraries or has nothing registered.
func AfterLoadSources(path string) (lib string, srcs []string) {
	if len(afterLoad) == 0 {
		return "", nil
	}
	// path is require's real path: the library directory's, if a symlink
	// leads to it (an in-tree prefix)
	base := filepath.Join(RubyLibPrefix(), RubyVersion)
	for _, dir := range []string{base, realDir(base)} {
		if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
			rel = filepath.ToSlash(rel)
			return rel, afterLoad[rel]
		}
	}
	return "", nil
}

func realDir(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return dir
}

// cExtensionLoader loads a C extension file (gem/capi registers it)
var cExtensionLoader func(mrb *MrbState, path string) error

// SetCExtensionLoader names what loads a C extension library file
// (.so/.bundle) that `require` finds: gem/capi's dlopen of it.
func SetCExtensionLoader(fn func(mrb *MrbState, path string) error) { cExtensionLoader = fn }

// CExtensionLoader is the loader SetCExtensionLoader named, nil without one.
func CExtensionLoader() func(mrb *MrbState, path string) error { return cExtensionLoader }

// RegisterNativeExtensionGem records that the named gem's C extension is
// compiled into the binary (gem/capi): RubyGems takes its extension as
// built.
func RegisterNativeExtensionGem(name string) { nativeExtensionGems[name] = true }

var cExtensionFeatures = map[string]bool{}

// RegisterCExtension records that the Go gem feature ("name.so") is a C
// extension compiled in through the Ruby C API (gem/capi), not a Go gem's
// own core that happens to have a library's name (bigdecimal.so)
func RegisterCExtension(feature string) { cExtensionFeatures[feature] = true }

// IsCExtension reports whether RegisterCExtension recorded feature
func IsCExtension(feature string) bool { return cExtensionFeatures[feature] }

// NativeExtensionGem reports whether the named gem's C extension is
// compiled into the binary.
func NativeExtensionGem(name string) bool { return nativeExtensionGems[name] }

// GemInitialized reports whether the Go gem name is initialized in mrb
func (mrb *MrbState) GemInitialized(name string) bool {
	_, ok := mrb.features[name]
	return ok
}

// GemAvailable reports whether name is a registered Go gem (initialized or
// not); it initializes nothing
func (mrb *MrbState) GemAvailable(name string) bool {
	if _, ok := gems[name]; ok {
		return true
	}
	_, ok := lazyGems[name]
	return ok
}

// Require inits gem from available Go gems if not already initialized
// returns:
//
//	false - if feature is already initialized
//	true - if feature is found and initialized
//
// Panics is feature cannot be found
func (mrb *MrbState) Require(name string) bool {
	if name == "" {
		panic("error - empty feature name")
	}

	_, loaded := mrb.features[name]
	if loaded {
		return false
	}

	initFn, exists := gems[name]
	if !exists {
		initFn, exists = lazyGems[name]
	}
	if !exists {
		panic(fmt.Sprintf("error loading '%v'", name))
	}

	mrb.features[name] = initFn(mrb)
	return true
}

// FeatureAdd sets feature as loaded
func (mrb *MrbState) FeatureAdd(name string) {
	mrb.features[name] = struct{}{}
}

// FeatureExists checks if feature exists
func (mrb *MrbState) FeatureExists(name string) bool {
	_, exists := gems[name]
	if exists {
		return exists
	}
	if _, exists = lazyGems[name]; exists {
		return exists
	}

	_, exists = mrb.features[name]
	return exists
}

// CopyLazyFeatures requires in state to all lazily loaded gems (stdlib
// libraries) required in mrb, e.g. for thread states which receive copies
// of objects of these classes.
func (mrb *MrbState) CopyLazyFeatures(to *MrbState) {
	names := make([]string, 0)
	for name := range mrb.features {
		if _, lazy := lazyGems[name]; lazy {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		to.Resolve(name)
	}
	for _, orb := range mrb.MountedOrbs() {
		_, _ = to.MountOrb(orb)
	}
}
