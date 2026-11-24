package oruby

import (
	"fmt"
	"sort"
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
