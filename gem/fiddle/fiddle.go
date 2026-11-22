// Package fiddle implements Ruby's fiddle standard library backed by gem/ffi
// (libffi + dlopen/LoadLibrary) and standard C allocation primitives.
package fiddle

/*
#include <stdlib.h>
#include <stdint.h>

static uintptr_t oruby_fiddle_free_addr(void) {
    return (uintptr_t)(void *)&free;
}

static uintptr_t oruby_fiddle_malloc(size_t sz) {
    void *p = calloc(1, sz > 0 ? sz : 1);
    return (uintptr_t)p;
}

static uintptr_t oruby_fiddle_realloc(uintptr_t addr, size_t sz) {
    void *p = realloc((void *)addr, sz > 0 ? sz : 1);
    return (uintptr_t)p;
}

static void oruby_fiddle_free(uintptr_t addr) {
    if (addr != 0) {
        free((void *)addr);
    }
}
*/
import "C"

import (
	_ "embed"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/ffi"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed fiddle.rb
var fiddleRb []byte

func init() {
	oruby.LazyGem("fiddle", initFiddle)
	for _, sub := range []string{
		"fiddle/import",
		"fiddle/struct",
		"fiddle/cparser",
		"fiddle/types",
		"fiddle/value",
		"fiddle/pack",
		"fiddle/function",
		"fiddle/closure",
		"fiddle/version",
	} {
		oruby.GemAlias(sub, "fiddle")
	}
}

func initFiddle(mrb *oruby.MrbState) any {
	mrb.Require("ffi")

	mod := mrb.DefineModule("Fiddle")
	mrb.ConstSet(mod, mrb.Intern("RUBY_FREE"), mrb.Uint64Value(uint64(C.oruby_fiddle_free_addr())))

	mod.DefineClassMethod("malloc", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sz := mrb.GetArgsFirst().Int64()
		if sz < 0 {
			return mrb.EArgumentError().Raise("negative allocation size")
		}
		addr := C.oruby_fiddle_malloc(C.size_t(sz))
		if addr == 0 {
			return mrb.Raise(mrb.ERuntimeError(), "failed to allocate memory")
		}
		return mrb.Uint64Value(uint64(addr))
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("realloc", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		addr := uint64(args.Item(0).Int64())
		if args.Item(0).Type() == oruby.MrbTTBigInt {
			if z, err := mrb.BigInt(args.Item(0)); err == nil {
				addr = z.Uint64()
			}
		}
		sz := args.Item(1).Int64()
		if sz < 0 {
			return mrb.EArgumentError().Raise("negative allocation size")
		}
		newAddr := C.oruby_fiddle_realloc(C.uintptr_t(addr), C.size_t(sz))
		if newAddr == 0 {
			return mrb.Raise(mrb.ERuntimeError(), "failed to reallocate memory")
		}
		return mrb.Uint64Value(uint64(newAddr))
	}, mrb.ArgsReq(2))

	mod.DefineClassMethod("free", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		var addr uint64
		if v.Type() == oruby.MrbTTBigInt {
			if z, err := mrb.BigInt(v); err == nil {
				addr = z.Uint64()
			}
		} else if v.IsInt() {
			addr = uint64(v.Int64())
		} else if mrb.RespondTo(v, mrb.Intern("to_i")) {
			iv, err := mrb.Funcall(v, mrb.Intern("to_i"))
			if err == nil {
				if iv.Type() == oruby.MrbTTBigInt {
					if z, err := mrb.BigInt(iv); err == nil {
						addr = z.Uint64()
					}
				} else {
					addr = uint64(iv.Int64())
				}
			}
		}
		C.oruby_fiddle_free(C.uintptr_t(addr))
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	rb.LoadRuby(mrb, "<internal:fiddle>", fiddleRb)
	return nil
}
