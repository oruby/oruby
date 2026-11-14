package compiler

import (
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
