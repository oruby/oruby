package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/oruby/oruby"
)

func TestShellSplit(t *testing.T) {
	got := shellSplit(`-I. -DRUBY_EXTCONF_H=\"extconf.h\" -DA='x y'  "-Ia b" -O2`)
	want := []string{"-I.", `-DRUBY_EXTCONF_H="extconf.h"`, "-DA=x y", "-Ia b", "-O2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestMakefileVars(t *testing.T) {
	dir := t.TempDir()
	mk := "srcdir = /src/ext\nVPATH = $(srcdir):$(srcdir)/lib\nTARGET = parser\nTARGET_NAME = parser\n" +
		"TARGET_ENTRY = Init_$(TARGET_NAME)\ntarget_prefix = /json/ext\nORIG_SRCS = a.c \\\n\tb.c\nSRCS = $(ORIG_SRCS) c.c\n" +
		"OBJS = $(SRCS:.c=.o)\n\nall:\n\t$(CC) -o x\n"
	p := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(p, []byte(mk), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := readMakefileVars(p)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"$(TARGET_ENTRY)":  "Init_parser",
		"$(VPATH)":         "/src/ext:/src/ext/lib",
		"$(target_prefix)": "/json/ext",
		"$(UNKNOWN)x":      "x",
	} {
		if got := v.expand(in); got != want {
			t.Errorf("expand(%s) = %q, want %q", in, got, want)
		}
	}
	if got := strings.Fields(v.expand("$(SRCS)")); !reflect.DeepEqual(got, []string{"a.c", "b.c", "c.c"}) {
		t.Errorf("SRCS = %q", got)
	}
}

func TestGemExtensions(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "specifications"), 0o755)
	spec := "Gem::Specification.new do |s|\n  s.name = \"x\".freeze\n" +
		"  s.extensions = [\"ext/x/extconf.rb\".freeze, \"ext/y/Rakefile\".freeze]\nend\n"
	os.WriteFile(filepath.Join(root, "specifications", "x-1.0.gemspec"), []byte(spec), 0o644)
	got := gemExtensions(&resolvedGemDir{Name: "x", Version: "1.0", GemRoot: root})
	if !reflect.DeepEqual(got, []string{"ext/x/extconf.rb"}) {
		t.Fatalf("got %q", got)
	}
}

func TestWriteExtensionPackage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ext00_x")
	ext := &nativeExt{Gem: "x", Version: "1.0", Feature: "x/x", Init: "Init_x",
		Sources: []string{"/src/x.c", "/src/y.cc"}, CFlags: []string{"-I/inc dir"}, Defines: []string{`H="h.h"`, "HAVE_A"}, LDFlags: []string{"-lz"}}
	if err := writeExtensionPackage(ext, dir, "ext00_x"); err != nil {
		t.Fatal(err)
	}
	g, _ := os.ReadFile(filepath.Join(dir, "ext.go"))
	for _, want := range []string{"#cgo CFLAGS: -I${SRCDIR} '-I/inc dir' -w", "#cgo CXXFLAGS:", "#cgo LDFLAGS: -lz",
		"void Init_x(void);", `capi.GemExtension("x", "x/x", unsafe.Pointer(C.Init_x))`} {
		if !strings.Contains(string(g), want) {
			t.Errorf("ext.go lacks %q:\n%s", want, g)
		}
	}
	d, _ := os.ReadFile(filepath.Join(dir, "oruby_ext_defs.h"))
	if !strings.Contains(string(d), `#define H "h.h"`) || !strings.Contains(string(d), "#define HAVE_A 1") {
		t.Errorf("defines:\n%s", d)
	}
	if w, _ := os.ReadFile(filepath.Join(dir, "src01_y.cc")); !strings.Contains(string(w), `#include "/src/y.cc"`) {
		t.Errorf("wrapper: %s", w)
	}
}

// An embedded gem's (oruby.GemSpec) installed versions up to its own are
// left out of the build (the Go gem stands in for them, nothing to pack or
// compile); a newer one is packed and its extension built, as any gem's.
func TestEmbeddedGemVersionsInBuild(t *testing.T) {
	ev, ok := oruby.EmbeddedGemVersion("msgpack")
	if !ok {
		t.Skip("no embedded msgpack")
	}
	for _, c := range []struct {
		version string
		packed  bool
	}{{"1.0.0", false}, {ev, false}, {"99.0.0", true}} {
		root := t.TempDir()
		dir := filepath.Join(root, "gems", "msgpack-"+c.version)
		if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "specifications"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GEM_PATH", root)
		t.Setenv("GEM_HOME", root)
		app := t.TempDir()
		if err := os.WriteFile(filepath.Join(app, "Gemfile"), []byte("gem 'msgpack'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if c.version == "99.0.0" {
			// locked to the embedded version: what is installed besides does not count
			lock := "GEM\n  specs:\n    msgpack (" + ev + ")\n\nDEPENDENCIES\n  msgpack\n"
			if err := os.WriteFile(filepath.Join(app, "Gemfile.lock"), []byte(lock), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, gems, _ := resolveGemfileGems(filepath.Join(app, "Gemfile"), app, nil, false); len(gems) != 0 {
				t.Errorf("msgpack locked to %s: packed %v", ev, gems[0])
			}
			os.Remove(filepath.Join(app, "Gemfile.lock"))
		}
		_, gems, err := resolveGemfileGems(filepath.Join(app, "Gemfile"), app, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		packed := false
		for _, g := range gems {
			packed = packed || (g.Name == "msgpack" && g.Version == c.version)
		}
		if packed != c.packed {
			t.Errorf("msgpack %s (embedded %s): packed = %v, want %v", c.version, ev, packed, c.packed)
		}
	}
}
