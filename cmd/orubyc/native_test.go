package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/compiler"
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
	ev, ok := oruby.EmbeddedGemVersion("racc")
	if !ok {
		t.Skip("no embedded racc")
	}
	for _, c := range []struct {
		version string
		packed  bool
	}{{"1.0.0", false}, {ev, false}, {"99.0.0", true}} {
		root := t.TempDir()
		dir := filepath.Join(root, "gems", "racc-"+c.version)
		if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "specifications"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GEM_PATH", root)
		t.Setenv("GEM_HOME", root)
		app := t.TempDir()
		if err := os.WriteFile(filepath.Join(app, "Gemfile"), []byte("gem 'racc'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if c.version == "99.0.0" {
			// locked to the embedded version: what is installed besides does not count
			lock := "GEM\n  specs:\n    racc (" + ev + ")\n\nDEPENDENCIES\n  racc\n"
			if err := os.WriteFile(filepath.Join(app, "Gemfile.lock"), []byte(lock), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, gems, _ := resolveGemfileGems(filepath.Join(app, "Gemfile"), app, nil, false); len(gems) != 0 {
				t.Errorf("racc locked to %s: packed %v", ev, gems[0])
			}
			os.Remove(filepath.Join(app, "Gemfile.lock"))
		}
		_, gems, err := resolveGemfileGems(filepath.Join(app, "Gemfile"), app, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		packed := false
		for _, g := range gems {
			packed = packed || (g.Name == "racc" && g.Version == c.version)
		}
		if packed != c.packed {
			t.Errorf("racc %s (embedded %s): packed = %v, want %v", c.version, ev, packed, c.packed)
		}
	}
}

func TestExtPolicy(t *testing.T) {
	// Default mode is "c": no Go extensions unless explicitly requested
	pol, err := newExtPolicy(ExtensionsConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if pol.Mode != "c" {
		t.Fatalf("default mode = %q, want c", pol.Mode)
	}
	if _, ok := pol.wantsGo("pg"); ok {
		t.Fatal("mode=c should not select pg Go ext by default")
	}

	// Explicit Go override with alias
	pol, err = newExtPolicy(ExtensionsConfig{Mode: "c", Go: []string{"zstd", "oruby-vips"}})
	if err != nil {
		t.Fatal(err)
	}
	if sp, ok := pol.wantsGo("zstd-ruby"); !ok || sp.ImportPath != compiler.GoExtModule+"/zstd" {
		t.Fatalf("zstd-ruby: got (%+v, %v)", sp, ok)
	}
	if sp, ok := pol.wantsGo("ruby-vips"); !ok || sp.ImportPath != compiler.GoExtModule+"/oruby-vips" {
		t.Fatalf("ruby-vips: got (%+v, %v)", sp, ok)
	}
	if _, ok := pol.wantsGo("pg"); ok {
		t.Fatal("pg should remain C mode")
	}

	// Mode "go" with per-gem C override
	pol, err = newExtPolicy(ExtensionsConfig{Mode: "go", C: []string{"nokogiri", "zstd"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pol.wantsGo("pg"); !ok {
		t.Fatal("mode=go should select pg")
	}
	if _, ok := pol.wantsGo("nokogiri"); ok {
		t.Fatal("extensions.c should keep nokogiri in C mode")
	}
	if _, ok := pol.wantsGo("zstd-ruby"); ok {
		t.Fatal("extensions.c alias zstd should keep zstd-ruby in C mode")
	}

	// Mode "auto": does not eagerly choose Go, but allows auto fallback unless forced C
	pol, err = newExtPolicy(ExtensionsConfig{Mode: "auto", C: []string{"pg"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pol.wantsGo("msgpack"); ok {
		t.Fatal("mode=auto should try C first")
	}
	if _, ok := pol.canAutoFallback("msgpack"); !ok {
		t.Fatal("mode=auto should allow fallback for msgpack")
	}
	if _, ok := pol.canAutoFallback("pg"); ok {
		t.Fatal("mode=auto must respect extensions.c override for pg")
	}

	// Invalid mode and unknown extension name fail cleanly
	if _, err := newExtPolicy(ExtensionsConfig{Mode: "bogus"}); err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if _, err := newExtPolicy(ExtensionsConfig{Go: []string{"nonexistent-gem-xyz"}}); err == nil {
		t.Fatal("expected error for unknown Go extension")
	}
}

func TestGoExtGemfileResolution(t *testing.T) {
	// Fake GEM_HOME with puma (ExtOnly) installed and pg NOT installed at all
	root := t.TempDir()
	pumaDir := filepath.Join(root, "gems", "puma-6.4.0")
	if err := os.MkdirAll(filepath.Join(pumaDir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pumaDir, "lib", "puma.rb"), []byte("module Puma; end\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "specifications"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "Gem::Specification.new do |s|\n  s.name = \"puma\"\n  s.version = \"6.4.0\"\n  s.extensions = [\"ext/puma_http11/extconf.rb\"]\nend\n"
	if err := os.WriteFile(filepath.Join(root, "specifications", "puma-6.4.0.gemspec"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEM_PATH", root)
	t.Setenv("GEM_HOME", root)
	t.Setenv("HOME", t.TempDir())

	app := t.TempDir()
	if err := os.WriteFile(filepath.Join(app, "Gemfile"), []byte("gem 'pg'\ngem 'puma'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Gemfile.lock"), []byte("GEM\n  specs:\n    pg (1.5.9)\n    puma (6.4.0)\n\nDEPENDENCIES\n  pg\n  puma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pol, err := newExtPolicy(ExtensionsConfig{Mode: "go"})
	if err != nil {
		t.Fatal(err)
	}
	_, resolved, goExts, err := resolveGemfileGemsWithPolicy(filepath.Join(app, "Gemfile"), app, nil, pol, false)
	if err != nil {
		t.Fatal(err)
	}
	// pg is non-ExtOnly -> not in resolved (even without being installed), but IS in goExts
	// puma is ExtOnly -> STILL in resolved (its Ruby lib/ must be packed) AND in goExts
	if len(resolved) != 1 || resolved[0].Name != "puma" {
		t.Fatalf("resolved = %+v, want only puma", resolved)
	}
	if len(goExts) != 2 || goExts[0].Gem != "pg" || goExts[1].Gem != "puma" {
		t.Fatalf("goExts = %+v, want [pg puma]", goExts)
	}
}

func TestOrbRejectsGoExtensions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.rb"), []byte("puts 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if code := runBuildSubcommand(mrb, "oruby", []string{"--target=orb", "--go-ext=fast_blank", dir}); code == 0 {
		t.Fatal("expected target=orb with --go-ext to fail")
	}
	if code := runBuildSubcommand(mrb, "oruby", []string{"--target=orb", "--ext-mode=go", dir}); code == 0 {
		t.Fatal("expected target=orb with --ext-mode=go to fail")
	}
}

func TestNativeTargetWithGoExts(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORUBY_SRC", root)
	oruby.SetInstallPrefix(root)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.rb"), []byte("require 'fast_blank'\nputs '   '.blank?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if code := runBuildSubcommand(mrb, "oruby", []string{"--target=native", "--no-gems", "--go-ext=fast_blank,zstd", dir}); code != 0 {
		t.Fatalf("oruby build --target=native failed with code %d", code)
	}
	goOut := filepath.Join(dir, "build", filepath.Base(dir)+"_go")
	modData, err := os.ReadFile(filepath.Join(goOut, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"replace github.com/oruby/oruby => " + root,
		"replace " + compiler.GoExtModule + " => " + filepath.Join(root, "goext"),
	} {
		if !strings.Contains(string(modData), want) {
			t.Errorf("generated go.mod missing %q:\n%s", want, modData)
		}
	}
	rtData, err := os.ReadFile(filepath.Join(goOut, "main", "aot_runtime.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"` + compiler.GoExtModule + `/oruby-fast_blank"`,
		`"` + compiler.GoExtModule + `/zstd"`,
	} {
		if !strings.Contains(string(rtData), want) {
			t.Errorf("generated aot_runtime.go missing %q", want)
		}
	}
}
