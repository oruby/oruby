package capi_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/capi"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
)

// A C extension built as a library, as mkmf builds one (the C API's
// headers, its symbols left to the running program), and required: the
// library is loaded with dlopen and its Init run.
func TestLoadLibrary(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	dir := t.TempDir()
	ext := ".so"
	args := []string{"-shared", "-fPIC"}
	if runtime.GOOS == "darwin" {
		ext = ".bundle"
		args = []string{"-bundle", "-undefined", "dynamic_lookup"}
	}
	lib := filepath.Join(dir, "capi_testext"+ext)
	args = append(args, "-I", "include", "-o", lib, filepath.Join("testext", "testext.c"))
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	for _, g := range []string{"core_ext", "load"} {
		if _, err := mrb.Resolve(g); err != nil {
			t.Fatal(err)
		}
	}
	mrb.SetGV("$__lib", lib)
	for _, c := range [][2]string{
		{`require $__lib`, `true`},
		{`require $__lib`, `false`},
		{`$LOADED_FEATURES.include?($__lib)`, `true`},
		{`CAPITest::ANSWER`, `42`},
		{`CAPITest.scan(1, 2, 3, 4, 5, k: 6) { }.inspect`, `[5, 1, 2, 3, [4, 5], {k: 6}, true]`},
		{`CAPITest.yield2(3) { |a, b| a + b }`, `9`},
		{`CAPITest.protect(:x).inspect`, `[1, "bad thing 42 x"]`},
		{`CAPITest.block_call([1, 2]).inspect`, `[11, 12]`},
		{`$b = 200.times.map { |i| CAPITest::Box.new("v#{i}" * 3) }; GC.start; 20.times { Array.new(1000) { "x" * 50 } }; GC.start; $b.last.held`, `v199v199v199`},
		{`GC.start; CAPITest.cached`, `kept by rb_gc_register_address`},
	} {
		v, err := mrb.Eval(c[0])
		if err != nil {
			t.Fatalf("%s: %v", c[0], err)
		}
		if got := mrb.String(v); got != c[1] {
			t.Fatalf("%s\n got: %s\nwant: %s", c[0], got, c[1])
		}
	}
	// a library without its Init_ is a LoadError
	bad := filepath.Join(dir, "other"+ext)
	if err := os.Rename(lib, bad); err != nil {
		t.Fatal(err)
	}
	mrb.SetGV("$__bad", bad)
	v, err := mrb.Eval(`begin; require $__bad; rescue LoadError => e; e.class.name; end`)
	if err != nil || mrb.String(v) != "LoadError" {
		t.Fatalf("missing Init: %v %v", v, err)
	}
}
