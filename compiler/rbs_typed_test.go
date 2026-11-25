package compiler_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/compiler"
)

func TestRBSParsingAndInlineComments(t *testing.T) {
	rbsSrc := `
class Object
  def fib: (Integer n) -> Integer
end

module Math2
  def self.dist_sq: (Float x, Float y) -> Float
  def scale: (Integer n, Float factor) -> Float
end
`
	env := compiler.ParseRBSSource(rbsSrc, "sig/math.rbs")
	if sig := env.LookupMethod("", "fib", false); sig == nil || len(sig.Params) != 1 || sig.Params[0] != compiler.TypeInt64 || sig.Return != compiler.TypeInt64 {
		t.Fatalf("fib sig = %+v", sig)
	}
	if sig := env.LookupMethod("Math2", "dist_sq", true); sig == nil || len(sig.Params) != 2 || sig.Params[0] != compiler.TypeFloat64 || sig.Return != compiler.TypeFloat64 {
		t.Fatalf("Math2.dist_sq sig = %+v", sig)
	}
	if sig := env.LookupMethod("Math2", "scale", false); sig == nil || len(sig.Params) != 2 || sig.Params[0] != compiler.TypeInt64 || sig.Params[1] != compiler.TypeFloat64 || sig.Return != compiler.TypeFloat64 {
		t.Fatalf("Math2#scale sig = %+v", sig)
	}

	inlineSrc := `
#: (Integer, Float) -> Float
def compound(n, rate)
  acc = 1.0
  acc
end

class Calc
  # @rbs x: Float
  # @rbs y: Float
  # @rbs return: Float
  def self.add(x, y)
    x + y
  end
end
`
	ienv := compiler.ExtractInlineRBS(inlineSrc, "app.rb")
	if sig := ienv.LookupMethod("", "compound", false); sig == nil || len(sig.Params) != 2 || sig.Params[0] != compiler.TypeInt64 || sig.Params[1] != compiler.TypeFloat64 || sig.Return != compiler.TypeFloat64 {
		t.Fatalf("inline compound sig = %+v", sig)
	}
	if sig := ienv.LookupMethod("Calc", "add", true); sig == nil || len(sig.Params) != 2 || sig.Params[0] != compiler.TypeFloat64 || sig.Return != compiler.TypeFloat64 {
		t.Fatalf("inline Calc.add sig = %+v", sig)
	}
}

func TestRBSTypeCheckingDiagnostics(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// 1. Return type mismatch: declares Integer but returns Float
	badRet := `
#: (Integer) -> Integer
def bad_ret(n)
  n + 1.5
end
`
	comp := compiler.New(compiler.Options{PackageName: "main", CheckRBS: true})
	if _, err := comp.CompileRuby(mrb, badRet); err == nil || !strings.Contains(err.Error(), "RBS return type mismatch") {
		t.Fatalf("expected RBS return type mismatch error, got: %v", err)
	}

	// 2. Arity mismatch: declares 2 params, Ruby def has 1
	badArity := `
#: (Integer, Integer) -> Integer
def bad_arity(n)
  n + 1
end
`
	comp2 := compiler.New(compiler.Options{PackageName: "main", CheckRBS: true})
	if _, err := comp2.CompileRuby(mrb, badArity); err == nil || !strings.Contains(err.Error(), "expects 2 params, Ruby def has 1") {
		t.Fatalf("expected RBS arity mismatch error, got: %v", err)
	}
}

func TestTypedAOTExecutionAndSoundness(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs an AOT Go binary with RBS-guided typed specialization")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	rbsSig := `
class Object
  def helper_sq: (Float x) -> Float
  def dist_sq: (Float x, Float y) -> Float
end

class Calc
  def self.mix: (Integer n, Float base) -> Float
  def compute: (Integer n) -> Integer
end
`
	rubySrc := `
# Automatic int64 recursive specialization (no RBS needed!)
def fib(n)
  return n if n <= 1
  fib(n - 1) + fib(n - 2)
end

# Inline RBS mixed Integer + Float loop
#: (Integer, Float) -> Float
def compound(n, rate)
  acc = 1.0
  i = 0
  while i < n
    acc = acc * (1.0 + rate)
    i += 1
  end
  acc
end

# External .rbs cross-method pure float64 calls
def helper_sq(x)
  x * x
end

def dist_sq(x, y)
  helper_sq(x) + helper_sq(y)
end

# Ruby floor-division, floor-modulo, unary minus, abs, to_f, to_i
#: (Integer, Integer) -> Integer
def floor_div(a, b)
  a / b
end

#: (Integer, Integer) -> Integer
def floor_mod(a, b)
  a % b
end

#: (Integer) -> Float
def neg_abs_f(n)
  (-n).abs.to_f + 0.5
end

# Class & singleton methods + subclass override check
class Calc
  def self.mix(n, base)
    base + n.to_f * 2.5
  end

  def helper(n)
    n * 2
  end

  def compute(n)
    helper(n) + 1
  end
end

class SubCalc < Calc
  def helper(n)
    n * 100
  end
end

# 1. Verify recursive fib(25) + Float arg fallback + BigInt overflow fallback
raise "fib(25)" unless fib(25) == 75025
raise "fib(1.5)" unless fib(1.5) == 0.0

#: (Integer, Integer) -> Integer
def mul_big(a, b)
  a * b
end
raise "mul_big small" unless mul_big(6, 7) == 42
raise "mul_big overflow" unless mul_big(1_000_000_000_000, 1_000_000_000_000).to_s == "1000000000000000000000000"

# 2. Verify mixed int/float compound loop
c = compound(10, 0.1)
raise "compound: #{c}" unless (c - 1.1**10).abs < 1e-9

# 3. Verify external .rbs cross-method float64 calls
raise "dist_sq" unless dist_sq(3.0, 4.0) == 25.0

# 4. Verify Ruby floor-div / floor-mod semantics (differs from Go truncation on negatives!)
raise "floor_div -5/2" unless floor_div(-5, 2) == -3
raise "floor_div 5/-2" unless floor_div(5, -2) == -3
raise "floor_mod -5%2" unless floor_mod(-5, 2) == 1
raise "floor_mod 5%-2" unless floor_mod(5, -2) == -1
zerr = begin; floor_div(5, 0); false; rescue ZeroDivisionError; true; end
raise "ZeroDivisionError fallback" unless zerr

# 5. Verify unary minus, abs, to_f
raise "neg_abs_f" unless neg_abs_f(-7) == 7.5

# 6. Verify class singleton & subclass override safety
raise "Calc.mix" unless Calc.mix(4, 1.5) == 11.5
raise "Calc#compute" unless Calc.new.compute(5) == 11
raise "SubCalc#compute" unless SubCalc.new.compute(5) == 501

# 7. Verify live monkey-patch invalidates method-cache guard cleanly!
Object.define_method(:fib) { |n| n + 1000 }
raise "monkey-patched fib" unless fib(25) == 1025

Object.define_method(:helper_sq) { |x| x * 10.0 }
raise "monkey-patched helper_sq" unless dist_sq(3.0, 4.0) == 70.0

puts "ALL_TYPED_AOT_OK"
`

	work := t.TempDir()
	sigDir := filepath.Join(work, "sig")
	if err := os.MkdirAll(sigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sigDir, "app.rbs"), []byte(rbsSig), 0o644); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(work, "app.rb")
	if err := os.WriteFile(scriptPath, []byte(rubySrc), 0o644); err != nil {
		t.Fatal(err)
	}

	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := compiler.New(compiler.Options{
		PackageName: "main",
		Standalone:  true,
		RBSPaths:    []string{sigDir},
		CheckRBS:    true,
	})
	mainDir := filepath.Join(work, "main")
	if _, err := comp.CompilePackageDir(mrb, scriptPath, nil, nil, mainDir); err != nil {
		t.Fatalf("CompilePackageDir: %v", err)
	}

	// Verify typed Go functions were emitted in aot_mod_000.go
	mod0, err := os.ReadFile(filepath.Join(mainDir, "aot_mod_000.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(mod0)
	for _, want := range []string{
		"_typed(mrb *oruby.MrbState, _a1 int64) (int64, bool)",
		"_typed(mrb *oruby.MrbState, _a1 int64, _a2 float64) (float64, bool)",
		"_typed(mrb *oruby.MrbState, _a1 float64, _a2 float64) (float64, bool)",
		"AotCompiledMethodOK(",
		"AotDefineMethodTracked(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("aot_mod_000.go missing %q", want)
		}
	}

	// Build and run the compiled native binary
	modText := fmt.Sprintf("module aottypecheck\n\ngo 1.27.0\n\nrequire github.com/oruby/oruby v0.0.0\n\nreplace github.com/oruby/oruby => %s\n", root)
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(modText), 0o644); err != nil {
		t.Fatal(err)
	}
	if sumBytes, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(work, "go.sum"), sumBytes, 0o644)
	}

	bin := filepath.Join(work, "typed_bin")
	build := exec.Command("go", "build", "-o", bin, "./main")
	build.Dir = work
	build.Env = append(os.Environ(), "CGO_ENABLED=1", "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("compiled binary failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "ALL_TYPED_AOT_OK" {
		t.Fatalf("output = %q, want ALL_TYPED_AOT_OK", got)
	}
}
