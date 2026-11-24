// Package capi is ORuby's Ruby C API: MRI's C extension API (ruby.h)
// implemented over the mruby C API, so a C extension compiles from source
// into a standalone oruby process or a compiled oruby app.
//
// The API has no mrb_state argument, so it is bound to one state: the one
// that requires the first extension. That is the process's only state in
// standalone oruby; an embedding that opens more states can use the
// extensions from the first one alone (a require from another raises).
//
// An extension is compiled by a Go package of its own, which includes the
// extension's C sources and registers it with Extension: `require name`
// then runs the extension's Init function. The headers are in include/
// (cgo: -I${SRCDIR}/../capi/include).
package capi

/*
#cgo CFLAGS: -I${SRCDIR}/include -I${SRCDIR}/../.. -I${SRCDIR}/../../mruby/include -I${SRCDIR}/../../mruby/build/host/include
#cgo CFLAGS: -DMRB_INT64 -DMRB_HIGH_PROFILE -DMRB_NO_BOXING -DMRB_SYMBOL_MAX=0 -DMRB_USE_DEBUG_HOOK -DHAVE_MRUBY_ENCODING_GEM -DMRB_UTF8_STRING -DMRB_USE_RATIONAL -DMRB_USE_SET -DMRB_USE_COERCE -DMRB_USE_BIGINT -DMRB_USE_COMPLEX -DMRB_USE_REFINEMENTS -DMRB_USE_GC_WEAK
#cgo linux LDFLAGS: -rdynamic -ldl
#include <stdlib.h>
#include "capi.h"
*/
import "C"

import (
	"errors"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/oruby/oruby"
)

func init() { oruby.SetCExtensionLoader(LoadLibrary) }

// LoadLibrary loads the C extension library at path (.so/.bundle) built
// against the C API's headers (mkmf, `gem install`): it is opened, its
// Init_<name> found and run, as `require` of it runs in MRI. The library
// takes the C API's functions from the running oruby, which exports them.
// A raise in Init is the error.
func LoadLibrary(mrb *oruby.MrbState, path string) error {
	if err := Bind(mrb); err != nil {
		return err
	}
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	cpath, cinit := C.CString(path), C.CString("Init_"+name)
	defer C.free(unsafe.Pointer(cpath))
	defer C.free(unsafe.Pointer(cinit))
	var msg [512]C.char
	fn := C.orb_capi_dlopen(cpath, cinit, &msg[0], C.size_t(len(msg)))
	if fn == nil {
		return oruby.EError("LoadError", "%s", C.GoString(&msg[0]))
	}
	if !mrb.ClassDefined("ORubyCAPI") {
		return ErrOtherState
	}
	mod := mrb.ModuleGet("ORubyCAPI")
	if _, err := mrb.Funcall(mod.Value(), mrb.Intern("__init_extension"), mrb.CPtrValue(uintptr(fn))); err != nil {
		return err
	}
	return nil
}

// ErrOtherState is returned when an extension is required from another
// mrb_state than the one the C API is bound to.
var ErrOtherState = errors.New("the Ruby C API is bound to another mrb_state (C extensions run in one state per process)")

// Bind binds the C API to mrb, the state extensions run in. Extension does
// it on the first require; it is an error once another state is bound.
func Bind(mrb *oruby.MrbState) error {
	if C.orb_capi_bind_addr(C.uintptr_t(mrb.State())) == 0 {
		return ErrOtherState
	}
	return nil
}

// GemExtension is Extension for the C extension of the named gem, which
// RubyGems then takes as built.
func GemExtension(gem, name string, init unsafe.Pointer) {
	oruby.RegisterNativeExtensionGem(gem)
	Extension(name, init)
}

// Extension registers a C extension compiled into the binary under its
// feature name (the name of its .so/.bundle in MRI, e.g. "rbs_extension"):
// `require name` (or "name.so", "name.bundle") binds the C API and calls init, the extension's
// Init_<name> function (unsafe.Pointer(C.Init_name)). A raise in it is
// the require's error.
func Extension(name string, init unsafe.Pointer) {
	// named as its library: a Ruby file of the name on the load path comes
	// first, as in MRI (gem/load)
	oruby.LazyGem(name+".so", func(mrb *oruby.MrbState) interface{} {
		if err := Bind(mrb); err != nil {
			return &oruby.InitError{Err: err}
		}
		if !mrb.ClassDefined("ORubyCAPI") {
			return &oruby.InitError{Err: ErrOtherState}
		}
		mod := mrb.ModuleGet("ORubyCAPI")
		if _, err := mrb.Funcall(mod.Value(), mrb.Intern("__init_extension"), mrb.CPtrValue(uintptr(init))); err != nil {
			return &oruby.InitError{Err: err}
		}
		return nil
	})
}
