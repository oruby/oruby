// Package testext is a C extension written against MRI's API (testext.c),
// for gem/capi's tests: `require "capi_testext"`; testext_cxx.cc is one in
// C++ (`require "capi_testext_cxx"`).
package testext

/*
#cgo CFLAGS: -I${SRCDIR}/../include
#cgo CXXFLAGS: -I${SRCDIR}/../include -std=c++11
void Init_capi_testext(void);
void Init_capi_testext_cxx(void);
*/
import "C"

import (
	"unsafe"

	"github.com/oruby/oruby/gem/capi"
)

func init() {
	capi.Extension("capi_testext", unsafe.Pointer(C.Init_capi_testext))
	capi.Extension("capi_testext_cxx", unsafe.Pointer(C.Init_capi_testext_cxx))
}
