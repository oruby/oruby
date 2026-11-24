package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
