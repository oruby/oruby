package oruby

import (
	"fmt"
)

var gems = make(map[string]func(*MrbState) interface{})

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
//    false - if feature is already initialized
//    true - if feature is found and initialized
//
//    error is returned if feature cannot be found
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
		return false, EKeyError("error loading '%v'", name)
	}

	mrb.features[name] = initFn(mrb)
	return true, nil
}

// Require inits gem from available Go gems if not already initialized
// returns:
//    false - if feature is already initialized
//    true - if feature is found and initialized
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

	_, exists = mrb.features[name]
	return exists
}
