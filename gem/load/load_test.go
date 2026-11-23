package load

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/oruby/oruby"
)

func TestLoadStartsPrivateVisibilityScope(t *testing.T) {
	const source = `
      def loader_default_private = :private
      raise "visible during load" unless method(:loader_default_private).owner.private_method_defined?(:loader_default_private)

      def loader_named_public = :named
      public :loader_named_public
      public
      def loader_scope_public = :scope
      def loader_named_protected = :protected
      protected :loader_named_protected
      private
      def loader_explicit_private = :explicit
    `
	for _, mode := range []struct {
		name     string
		call     string
		wrapped  bool
		bytecode bool
	}{
		{name: "source load", call: "load"},
		{name: "source require", call: "require"},
		{name: "source wrapped load", call: "load", wrapped: true},
		{name: "bytecode load", call: "load", bytecode: true},
		{name: "bytecode require", call: "require", bytecode: true},
		{name: "bytecode wrapped load", call: "load", wrapped: true, bytecode: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mrb := openLoadTestState(t)
			filename := "visibility.rb"
			data := []byte(source)
			if mode.bytecode {
				context := mrb.MrbCcontextNew()
				defer context.Free()
				context.Filename(filename)
				parser, err := mrb.ParseString(source, context)
				if err != nil {
					t.Fatal(err)
				}
				defer parser.Free()
				proc, err := mrb.GenerateCode(parser)
				if err != nil {
					t.Fatal(err)
				}
				var buf bytes.Buffer
				if _, err := mrb.DumpIrep(proc.IRep(), oruby.DumpDebugInfo, &buf); err != nil {
					t.Fatal(err)
				}
				filename, data = "visibility.mrb", buf.Bytes()
			}
			path := filepath.Join(t.TempDir(), filename)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			target := "Object"
			call := mode.call + "(" + strconv.Quote(path)
			prefix := ""
			if mode.wrapped {
				target = "LoaderVisibilityFixture"
				prefix = target + " = Module.new\n"
				call += ", " + target
			}
			v, err := mrb.Eval(prefix + "public\n" + call + ")\nowner = " + target + `
              def loader_caller_public = :caller
              owner.private_method_defined?(:loader_default_private) &&
                owner.private_method_defined?(:loader_explicit_private) &&
                owner.public_method_defined?(:loader_named_public) &&
                owner.public_method_defined?(:loader_scope_public) &&
                owner.protected_method_defined?(:loader_named_protected) &&
                Object.public_method_defined?(:loader_caller_public)
            `)
			if err != nil || !v.Bool() {
				t.Fatalf("loaded visibility = %v, %v; want true", v, err)
			}
		})
	}
}

func Test_fullName(t *testing.T) {
	//
}

func Test_require(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

}

func openLoadTestState(t *testing.T) *oruby.MrbState {
	t.Helper()
	mrb := oruby.MrbOpen()
	t.Cleanup(mrb.Close)
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	return mrb
}

func writeAutoloadFile(t *testing.T, name, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAutoloadIsDeferredAndModuleScoped(t *testing.T) {
	mrb := openLoadTestState(t)
	alphaPath := writeAutoloadFile(t, "alpha.rb", `AutoloadAlpha::VALUE = "alpha"; $autoload_alpha_count = ($autoload_alpha_count || 0) + 1`)
	betaPath := writeAutoloadFile(t, "beta.rb", `AutoloadBeta::VALUE = "beta"; $autoload_beta_count = ($autoload_beta_count || 0) + 1`)
	globalPath := writeAutoloadFile(t, "global.rb", `TopAutoloadFixture = "global"; $autoload_global_count = ($autoload_global_count || 0) + 1`)
	setup := "module AutoloadAlpha; autoload(:VALUE, " + strconv.Quote(alphaPath) + "); end; " +
		"module AutoloadBeta; autoload(\"VALUE\", " + strconv.Quote(betaPath) + "); end; " +
		"autoload(:TopAutoloadFixture, " + strconv.Quote(globalPath) + ")"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register autoloads: %v", err)
	}
	for _, global := range []string{"$autoload_alpha_count", "$autoload_beta_count", "$autoload_global_count"} {
		got, err := mrb.Eval(global)
		if err != nil || !got.IsNil() {
			t.Fatalf("%s before lookup = %v, %v; want nil", global, got, err)
		}
	}
	for _, tc := range []struct {
		code string
		want string
	}{
		{"AutoloadAlpha.autoload?(:VALUE)", alphaPath},
		{`AutoloadBeta.autoload?("VALUE")`, betaPath},
	} {
		got, err := mrb.Eval(tc.code)
		if err != nil || mrb.String(got) != tc.want {
			t.Fatalf("%s = %q, %v; want %q", tc.code, mrb.String(got), err, tc.want)
		}
	}
	for _, tc := range []struct {
		code string
		want string
	}{
		{"AutoloadAlpha::VALUE", "alpha"},
		{"AutoloadBeta::VALUE", "beta"},
		{"AutoloadAlpha::VALUE", "alpha"},
		{"TopAutoloadFixture", "global"},
	} {
		got, err := mrb.Eval(tc.code)
		if err != nil || mrb.String(got) != tc.want {
			t.Fatalf("%s = %q, %v; want %q", tc.code, mrb.String(got), err, tc.want)
		}
	}
	for _, global := range []string{"$autoload_alpha_count", "$autoload_beta_count", "$autoload_global_count"} {
		got, err := mrb.Eval(global)
		if err != nil || got.Int() != 1 {
			t.Fatalf("%s after lookup = %v, %v; want 1", global, got, err)
		}
	}
	for _, code := range []string{"AutoloadAlpha.autoload?(:VALUE)", `AutoloadBeta.autoload?("VALUE")`} {
		got, err := mrb.Eval(code)
		if err != nil || !got.IsNil() {
			t.Fatalf("%s after loading = %v, %v; want nil", code, got, err)
		}
	}
}

func TestAutoloadFailureCanBeRetried(t *testing.T) {
	mrb := openLoadTestState(t)
	path := filepath.Join(t.TempDir(), "retry.rb")
	if err := os.WriteFile(path, []byte(`raise "autoload failed"`), 0600); err != nil {
		t.Fatal(err)
	}
	setup := "module RetryAutoload; autoload(:VALUE, " + strconv.Quote(path) + "); end"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register autoload: %v", err)
	}
	if _, err := mrb.Eval("RetryAutoload::VALUE"); err == nil {
		t.Fatal("first constant lookup should fail")
	}
	if got, err := mrb.Eval("RetryAutoload.autoload?(:VALUE)"); err != nil || got.IsNil() {
		t.Fatalf("autoload? after failed load = %v, %v; want retained path", got, err)
	}
	if err := os.WriteFile(path, []byte(`RetryAutoload::VALUE = 73`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval("RetryAutoload::VALUE")
	if err != nil || got.Int() != 73 {
		t.Fatalf("retried constant lookup = %v, %v; want 73", got, err)
	}
}

func TestAutoloadMissingFileCanBeAddedAndRetried(t *testing.T) {
	mrb := openLoadTestState(t)
	path := filepath.Join(t.TempDir(), "later.rb")
	setup := "module MissingAutoload; autoload(\"VALUE\", " + strconv.Quote(path) + "); end"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register missing-file autoload: %v", err)
	}
	if _, err := mrb.Eval("MissingAutoload::VALUE"); err == nil {
		t.Fatal("lookup with missing file should fail")
	}
	if err := os.WriteFile(path, []byte(`MissingAutoload::VALUE = 91`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval("MissingAutoload::VALUE")
	if err != nil || got.Int() != 91 {
		t.Fatalf("lookup after adding file = %v, %v; want 91", got, err)
	}
}

func TestAutoloadRecursiveLookupFailsAndCanBeRetried(t *testing.T) {
	mrb := openLoadTestState(t)
	path := writeAutoloadFile(t, "recursive.rb", `RecursiveAutoload::VALUE`)
	setup := "module RecursiveAutoload; autoload(:VALUE, " + strconv.Quote(path) + "); end"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register recursive autoload: %v", err)
	}
	if _, err := mrb.Eval("RecursiveAutoload::VALUE"); err == nil {
		t.Fatal("recursive lookup should fail instead of recursing indefinitely")
	}
	if err := os.WriteFile(path, []byte(`RecursiveAutoload::VALUE = 17`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval("RecursiveAutoload::VALUE")
	if err != nil || got.Int() != 17 {
		t.Fatalf("lookup after recursive failure = %v, %v; want 17", got, err)
	}
}

// A file required through a symlinked directory and through its real path is
// one feature, as in MRI.
func TestRequireDedupesSymlinkedPaths(t *testing.T) {
	mrb := openLoadTestState(t)
	realDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(realDir, "once.rb"), []byte(`$once_count = ($once_count || 0) + 1`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	code := "require " + strconv.Quote(filepath.Join(link, "once")) + "\n" +
		"r = require " + strconv.Quote(filepath.Join(realDir, "once")) + "\n" +
		"[$once_count, r]"
	got, err := mrb.Eval(code)
	if err != nil {
		t.Fatal(err)
	}
	if s := mrb.String(mrb.Call(got, "inspect")); s != "[1, false]" {
		t.Fatalf("symlinked require = %s, want [1, false]", s)
	}
}

func compileTestRite(t *testing.T, mrb *oruby.MrbState, filename, source string) []byte {
	t.Helper()
	c := mrb.MrbCcontextNew()
	defer c.Free()
	c.Filename(filename)
	c.SetNoExec(true)
	val, err := c.LoadBytes([]byte(source))
	if err != nil {
		t.Fatalf("compile %s: %v", filename, err)
	}
	var buf bytes.Buffer
	if _, err := mrb.DumpIrep(mrb.RProc(val).IRep(), oruby.DumpDebugInfo, &buf); err != nil {
		t.Fatalf("dump %s: %v", filename, err)
	}
	return buf.Bytes()
}

func TestTransparentBytecodeAndMrbFallback(t *testing.T) {
	mrb := openLoadTestState(t)
	dir := t.TempDir()

	// 1. Only .mrb exists on disk, but code calls require(".../routes.rb"), load(".../routes.rb"), and autoload(:Val, ".../auto.rb")
	routesMrb := filepath.Join(dir, "routes.mrb")
	if err := os.WriteFile(routesMrb, compileTestRite(t, mrb, "routes.rb", `$routes_loaded = ($routes_loaded || 0) + 1`), 0600); err != nil {
		t.Fatal(err)
	}
	autoMrb := filepath.Join(dir, "auto.mrb")
	if err := os.WriteFile(autoMrb, compileTestRite(t, mrb, "auto.rb", `AutoConst = 99`), 0600); err != nil {
		t.Fatal(err)
	}

	// 2. A file keeps the .rb extension (plus a #! shebang) so Dir.glob("*.rb") sees it, while holding RITE bytecode
	riteInRb := filepath.Join(dir, "in_place.rb")
	shebangRite := append([]byte("#!/usr/bin/env oruby\n"), compileTestRite(t, mrb, "in_place.rb", `$inplace_val = 42`)...)
	if err := os.WriteFile(riteInRb, shebangRite, 0600); err != nil {
		t.Fatal(err)
	}

	routesRbArg := strconv.Quote(filepath.Join(dir, "routes.rb"))
	autoRbArg := strconv.Quote(filepath.Join(dir, "auto.rb"))
	riteInRbArg := strconv.Quote(riteInRb)

	code := `
	  r1 = require ` + routesRbArg + `
	  r2 = require ` + routesRbArg + `
	  l1 = load ` + routesRbArg + `
	  autoload(:AutoConst, ` + autoRbArg + `)
	  r3 = require ` + riteInRbArg + `
	  [r1, r2, l1, $routes_loaded, AutoConst, r3, $inplace_val]
	`
	got, err := mrb.Eval(code)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	want := "[true, false, true, 2, 99, true, 42]"
	if s := mrb.String(mrb.Call(got, "inspect")); s != want {
		t.Fatalf("got %s, want %s", s, want)
	}
}

func TestRequireAndLoadOrbBundle(t *testing.T) {
	mrb := openLoadTestState(t)
	dir := t.TempDir()

	// Build a multi-file .orb bundle with a lib/ directory on $LOAD_PATH,
	// a nested model using require_relative, and an entrypoint.
	b := oruby.NewOrbBuilder(oruby.OrbHdrDebugInfo)
	_ = b.AddDir("lib", true)
	_ = b.AddEntry("lib/orb_formatter.rb", oruby.OrbKindMrb, 0, 0644,
		compileTestRite(t, mrb, "lib/orb_formatter.rb", `module OrbFormatter; def self.tag(s) = "[#{s}]"; end`))
	_ = b.AddEntry("models/account.rb", oruby.OrbKindMrb, 0, 0644,
		compileTestRite(t, mrb, "models/account.rb", `require "orb_formatter"; class OrbAccount; def label = OrbFormatter.tag("acct"); end`))
	_ = b.AddEntry("boot.rb", oruby.OrbKindMrb, 0, 0644,
		compileTestRite(t, mrb, "boot.rb", `require_relative "models/account"; $orb_boot_count = ($orb_boot_count || 0) + 1; $orb_label = OrbAccount.new.label`))
	b.SetEntrypoint("boot.rb")

	orbPath := filepath.Join(dir, "app_bundle.orb")
	if err := b.WriteFile(orbPath); err != nil {
		t.Fatal(err)
	}

	orbArg := strconv.Quote(orbPath)
	code := `
	  r1 = require ` + orbArg + `
	  r2 = require ` + orbArg + `
	  r3 = require "orb_formatter"
	  l1 = load ` + orbArg + `
	  [r1, r2, r3, l1, $orb_boot_count, $orb_label]
	`
	got, err := mrb.Eval(code)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	want := `[true, false, false, true, 2, "[acct]"]`
	if s := mrb.String(mrb.Call(got, "inspect")); s != want {
		t.Fatalf("got %s, want %s", s, want)
	}
}
