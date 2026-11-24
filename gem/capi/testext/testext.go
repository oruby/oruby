// Package testext is a C extension written against MRI's API (testext.c),
// for gem/capi's tests: `require "capi_testext"`.
package testext

/*
#cgo CFLAGS: -I${SRCDIR}/../include
void Init_capi_testext(void);
*/
import "C"

import (
	"unsafe"

	"github.com/oruby/oruby/gem/capi"
)

func init() {
	capi.Extension("capi_testext", unsafe.Pointer(C.Init_capi_testext))
}
