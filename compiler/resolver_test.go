package compiler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oruby/oruby"
)

func TestCompiler_MultiFileBundle(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// 1. Setup multi-file Ruby project
	srcDir, err := os.MkdirTemp("", "ruby_project_*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(srcDir)

	if err := os.MkdirAll(filepath.Join(srcDir, "math"), 0755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "models"), 0755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	calcRuby := `
class MathCalc
  def multiply(a, b)
    a * b
  end
end
`
	if err := os.WriteFile(filepath.Join(srcDir, "math", "calc.rb"), []byte(calcRuby), 0644); err != nil {
		t.Fatalf("WriteFile calc.rb error: %v", err)
	}

	userRuby := `
class User
  def initialize(name)
    @name = name
  end
  def name
    @name
  end
end
`
	if err := os.WriteFile(filepath.Join(srcDir, "models", "user.rb"), []byte(userRuby), 0644); err != nil {
		t.Fatalf("WriteFile user.rb error: %v", err)
	}

	mainRuby := `
require_relative "math/calc"
require_relative "models/user"

c = MathCalc.new
u = User.new("Antigravity")
puts "#{u.name} calculated #{c.multiply(6, 7)}"
`
	entryFile := filepath.Join(srcDir, "main.rb")
	if err := os.WriteFile(entryFile, []byte(mainRuby), 0644); err != nil {
		t.Fatalf("WriteFile main.rb error: %v", err)
	}

	// 2. Precompile all required files into a single bundle
	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	goSrc, err := comp.CompileBundle(mrb, entryFile, []string{srcDir})
	if err != nil {
		t.Fatalf("CompileBundle error: %v", err)
	}

	t.Logf("Generated Bundled Go Source:\n%s", goSrc)

	// 3. Build standalone Go binary in a separate clean directory
	buildDir, err := os.MkdirTemp("", "oruby_bundle_build_*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(buildDir)

	if err := os.WriteFile(filepath.Join(buildDir, "main.go"), []byte(goSrc), 0644); err != nil {
		t.Fatalf("WriteFile main.go error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	repoRoot := filepath.Dir(wd)

	goModContent := `module bundled_app

go 1.22

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod error: %v", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = buildDir
	_ = tidyCmd.Run()

	binPath := filepath.Join(buildDir, "bundled_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = buildDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s\nSource:\n%s", err, string(out), goSrc)
	}

	// 4. DELETE the source directory completely to guarantee binary is 100% self-contained!
	_ = os.RemoveAll(srcDir)

	// 5. Run the binary from an unrelated working directory
	runCmd := exec.Command(binPath)
	runCmd.Dir = os.TempDir()
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	t.Logf("Execution output: %s", resultStr)

	expected := "Antigravity calculated 42"
	if resultStr != expected {
		t.Errorf("Expected %q, got: %q", expected, resultStr)
	}
}

func compileRubyFileToMrb(t *testing.T, mrb *oruby.MrbState, filename, rubySrc, outMrbPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(outMrbPath), 0755); err != nil {
		t.Fatal(err)
	}
	c := mrb.MrbCcontextNew()
	defer c.Free()
	c.Filename(filename)
	c.SetNoExec(true)
	val, err := c.LoadBytes([]byte(rubySrc))
	if err != nil {
		t.Fatalf("compile %s to irep: %v", filename, err)
	}
	var buf bytes.Buffer
	if st, err := mrb.DumpIrepBinary(mrb.RProc(val).IRep(), oruby.MrbDumpDebugInfo, &buf); err != nil || st != oruby.MrbDumpOK {
		t.Fatalf("dump %s: st=%d err=%v", filename, st, err)
	}
	if err := os.WriteFile(outMrbPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCompiler_PrecompiledMrbBytecodeBundle(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// 1. Create a project containing ONLY precompiled .mrb bytecode files (no .rb sources on disk)
	srcDir := t.TempDir()

	calcRuby := `
class MathCalc
  TAG = /calc_tag/
  def multiply(a, b)
    a * b
  end
end
`
	compileRubyFileToMrb(t, mrb, "math/calc.rb", calcRuby, filepath.Join(srcDir, "math", "calc.mrb"))

	userRuby := `
class User
  TAG = /user_tag/
  def initialize(name)
    @name = name
  end
  def name
    @name
  end
end
`
	compileRubyFileToMrb(t, mrb, "models/user.rb", userRuby, filepath.Join(srcDir, "models", "user.mrb"))

	pluginRuby := `
$plugin_loaded = "plugin_ok"
`
	pluginMrb := filepath.Join(srcDir, "plugins", "extra.mrb")
	compileRubyFileToMrb(t, mrb, "plugins/extra.rb", pluginRuby, pluginMrb)

	mainRuby := `
require_relative "math/calc"
require "models/user"
load "plugins/extra.rb"

c = MathCalc.new
u = User.new("BytecodeRails")
puts "#{u.name} calculated #{c.multiply(6, 7)} (#{$plugin_loaded}, #{MathCalc::TAG.source}, #{User::TAG.source})"
`
	entryMrb := filepath.Join(srcDir, "main.mrb")
	compileRubyFileToMrb(t, mrb, "main.rb", mainRuby, entryMrb)

	// 2. Transpile the precompiled .mrb bundle into Go source
	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	goSrc, err := comp.CompileBundleWith(mrb, entryMrb, []string{pluginMrb}, []string{srcDir})
	if err != nil {
		t.Fatalf("CompileBundleWith(.mrb) error: %v", err)
	}
	if len(comp.Skipped) != 0 {
		t.Fatalf("unexpected skipped .mrb files: %v", comp.Skipped)
	}

	// 3. Build standalone native Go executable in a clean directory
	buildDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(buildDir, "main.go"), []byte(goSrc), 0644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(wd)

	goModContent := "module mrb_bundled_app\n\ngo 1.27\n\nrequire github.com/oruby/oruby v0.0.0\n\nreplace github.com/oruby/oruby => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	binPath := filepath.Join(buildDir, "mrb_bundled_app")
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", binPath, ".")
	buildCmd.Dir = buildDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s\nSource:\n%s", err, string(out), goSrc)
	}

	// 4. Remove the .mrb directory so the native binary runs completely standalone
	_ = os.RemoveAll(srcDir)

	runCmd := exec.Command(binPath)
	runCmd.Dir = os.TempDir()
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	expected := "BytecodeRails calculated 42 (plugin_ok, calc_tag, user_tag)"
	if resultStr != expected {
		t.Fatalf("Expected %q, got %q", expected, resultStr)
	}
}

func TestCompiler_OrbBundleToMultiFileGoPackage(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	compileRite := func(filename, rubySrc string) []byte {
		c := mrb.MrbCcontextNew()
		defer c.Free()
		c.Filename(filename)
		c.SetNoExec(true)
		val, err := c.LoadBytes([]byte(rubySrc))
		if err != nil {
			t.Fatalf("compile %s: %v", filename, err)
		}
		var buf bytes.Buffer
		if st, err := mrb.DumpIrepBinary(mrb.RProc(val).IRep(), oruby.MrbDumpDebugInfo, &buf); err != nil || st != oruby.MrbDumpOK {
			t.Fatalf("dump %s: st=%d err=%v", filename, st, err)
		}
		return buf.Bytes()
	}

	// 1. Build a single .orb archive containing multiple compiled .mrb modules + a static asset
	orbBuilder := oruby.NewOrbBuilder(oruby.OrbHdrDebugInfo)
	_ = orbBuilder.AddDir("lib", true)
	_ = orbBuilder.AddEntry("config/version.txt", oruby.OrbKindAsset, 0, 0644, []byte("v2.5"))
	_ = orbBuilder.AddEntry("lib/math_service.rb", oruby.OrbKindMrb, 0, 0644, compileRite("lib/math_service.rb", `
class MathService
  def compute(x, y)
    x * y + 2
  end
end
`))
	_ = orbBuilder.AddEntry("lib/lazy_tax.rb", oruby.OrbKindMrb, 0, 0644, compileRite("lib/lazy_tax.rb", `
module LazyTax
  def self.rate = "TAX10"
end
`))
	_ = orbBuilder.AddEntry("app/models/invoice.rb", oruby.OrbKindMrb, 0, 0644, compileRite("app/models/invoice.rb", `
require "math_service"
Object.autoload(:LazyTax, "lazy_tax")

class Invoice
  def total(a, b)
    ver = File.read(File.expand_path("../../config/version.txt", __dir__)).strip
    models = Dir.children(__dir__).join(",")
    "INV-#{MathService.new.compute(a, b)}-#{LazyTax.rate}-#{ver}-#{models}-#{__dir__}"
  end
end
`))
	_ = orbBuilder.AddEntry("main.rb", oruby.OrbKindMrb, 0, 0644, compileRite("main.rb", `
require_relative "app/models/invoice"
puts Invoice.new.total(5, 8)
`))
	orbBuilder.SetEntrypoint("main.rb")

	orbDir := t.TempDir()
	orbPath := filepath.Join(orbDir, "application.orb")
	if err := orbBuilder.WriteFile(orbPath); err != nil {
		t.Fatal(err)
	}

	// 2. Transpile the .orb archive into a multi-file Go package directory
	pkgDir := t.TempDir()
	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})
	written, err := comp.CompilePackageDir(mrb, orbPath, nil, nil, pkgDir)
	if err != nil {
		t.Fatalf("CompilePackageDir(.orb) error: %v", err)
	}
	// Expect aot_runtime.go + 4 aot_mod_NNN.go files = 5 Go files
	if len(written) != 5 {
		t.Fatalf("expected 5 generated Go files, got %d: %v", len(written), written)
	}

	// Ensure zero build-machine absolute paths (orbDir) leaked into generated Go files!
	for _, gf := range written {
		srcBytes, rerr := os.ReadFile(gf)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if strings.Contains(string(srcBytes), orbDir) {
			t.Fatalf("build-machine path %q leaked into generated Go file %s", orbDir, gf)
		}
	}

	// 3. Compile the multi-file Go package into a native executable
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(wd)
	goModContent := "module orb_aot_app\n\ngo 1.27\n\nrequire github.com/oruby/oruby v0.0.0\n\nreplace github.com/oruby/oruby => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	binPath := filepath.Join(pkgDir, "orb_aot_app")
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", binPath, ".")
	buildCmd.Dir = pkgDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s", err, string(out))
	}

	// 4. Delete the .orb archive and run the standalone native binary
	_ = os.RemoveAll(orbDir)

	runCmd := exec.Command(binPath)
	runCmd.Dir = os.TempDir()
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}
	want := "INV-42-TAX10-v2.5-invoice.rb-/@orb/0/app/models"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
