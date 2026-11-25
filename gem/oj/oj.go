// Package oj is the oj gem (3.17.8) for oruby: its C extension compiled in
// through the Ruby C API layer (gem/capi) and its Ruby library, both
// unchanged from ohler55/oj v3.17.8 (ext/oj and lib/; LICENSE). The oj_*.c
// files include the extension's sources; the flags are those oruby's mkmf
// gives the gem's extconf.rb. `require "oj"` loads lib/oj.rb, whose
// `require "oj/oj"` runs Init_oj.
//
// The C API binds to one mrb_state (that of standalone oruby), so Oj works
// in one state of a process.
package oj

/*
#cgo CFLAGS: -I${SRCDIR}/../capi/include -I${SRCDIR}/ext/oj
#cgo CFLAGS: -std=gnu99 -O3 -fno-fast-math -Wall -Wno-unused-parameter -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -DHAVE_RB_GC_MARK_MOVABLE -DHAVE_STPCPY -DHAVE_PTHREAD_MUTEX_INIT -DHAVE_GETRLIMIT
#cgo CFLAGS: -DHAVE_RB_ENC_INTERNED_STR -DHAVE_RB_HASH_NEW_CAPA -DHAVE_RB_EXT_RACTOR_SAFE
#cgo CFLAGS: -DRUBY_TYPE=oruby -DORUBY_RUBY -DRUBY_VERSION=4.1.0 -DRUBY_VERSION_MAJOR=4
#cgo CFLAGS: -DRUBY_VERSION_MINOR=1 -DRUBY_VERSION_MICRO=0 -DIS_WINDOWS=0 -DRSTRUCT_LEN_RETURNS_INTEGER_OBJECT=0
#cgo darwin CFLAGS: -D_XOPEN_SOURCE -D_DARWIN_C_SOURCE -D_DARWIN_UNLIMITED_SELECT -D_REENTRANT
#cgo linux CFLAGS: -D_GNU_SOURCE -D_REENTRANT
#cgo amd64 CFLAGS: -msse2
void Init_oj(void);
*/
import "C"

import (
	"embed"
	"io/fs"
	"strings"
	"unsafe"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/capi"
)

//go:embed lib
var libFS embed.FS

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oj", Version: "3.17.8", Feature: "oj",
		Summary: "Oj C extension over the ORuby C API", Files: []string{"lib/oj.rb"},
	})
	capi.GemExtension("oj", "oj/oj", unsafe.Pointer(C.Init_oj))
	// the library: each file a feature of its own, as on the load path
	err := fs.WalkDir(libFS, "lib", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".rb") {
			return err
		}
		src, err := libFS.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(strings.TrimPrefix(path, "lib/"), ".rb")
		oruby.LazyGem(name, func(mrb *oruby.MrbState) interface{} {
			c := mrb.MrbCcontextNew()
			defer c.Free()
			c.Filename("<internal:" + name + ".rb>")
			if _, err := mrb.LoadBytesCxt(src, c); err != nil {
				return &oruby.InitError{Err: err}
			}
			return nil
		})
		return nil
	})
	if err != nil {
		panic(err)
	}
}
