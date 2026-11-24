// Package rbs compiles the C extension of the rbs gem (rbs_extension: its
// parser, which is plain C, and the bridge to Ruby objects) into oruby
// through the Ruby C API layer, gem/capi. The sources are those of the
// vendored gem, lib/oruby/gems/4.1.0/gems/rbs-4.2.0, unchanged; this
// package's .c files include them. `require "rbs_extension"`, which the
// gem's lib/rbs.rb does, runs Init_rbs_extension.
//
// The extension is the C API's proof of concept: rbs calls 43 functions of
// the API, from rb_define_method and TypedData to rb_ensure and rb_intern3.
package rbs

/*
#cgo CFLAGS: -I${SRCDIR}/../capi/include
#cgo CFLAGS: -I${SRCDIR}/../../lib/oruby/gems/4.1.0/gems/rbs-4.2.0/include
#cgo CFLAGS: -I${SRCDIR}/../../lib/oruby/gems/4.1.0/gems/rbs-4.2.0/ext/rbs_extension
#cgo CFLAGS: -std=gnu99 -DNDEBUG -Wno-unused-parameter
void Init_rbs_extension(void);
*/
import "C"

import (
	"unsafe"

	"github.com/oruby/oruby/gem/capi"
)

func init() {
	capi.GemExtension("rbs", "rbs_extension", unsafe.Pointer(C.Init_rbs_extension))
}
