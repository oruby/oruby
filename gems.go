package oruby

import (
	"errors"
	"fmt"
)

var gems = make(map[string]func(*MrbState))

//type MrbInitFunc func(*MrbState)

// Gem register makes a gem available by the provided name.
// If Register is called twice with the same name it panics.
func Gem(name string, initFn func(*MrbState)) {
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

// Require inits gem from available Go gems if not alreadyinitialized
func (mrb *MrbState) Require(name string) (bool, error) {
	if name == "" {
		return false, errors.New("error - empty")
	}

	_, loaded := mrb.features[name]
	if loaded {
		return false, nil
	}

	initFn, exists := gems[name]
	if !exists {
		return false, fmt.Errorf("error loading '%v'", name)
	}

	initFn(mrb)
	mrb.features[name] = struct{}{}
	return true, nil
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
