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
  def dot_f: (Array[Float] a, Array[Float] b) -> Float
end

class Calc
  def self.mix: (Integer n, Float base) -> Float
  def compute: (Integer n) -> Integer
end

class Point
  @x: Float
  @y: Float
  def dist: () -> Float
  def sum_dist: (Integer n) -> Float
  def scale!: (Float f) -> Float
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

# Math.*, bitwise, power, predicates, rounding, and explicit Class.method inside _typed
#: (Float, Float) -> Float
def euclid(x, y)
  Math.sqrt(x * x + y * y)
end

#: (Integer) -> Integer
def bit_mix(n)
  s = 0
  i = 0
  while i < n
    if i.even?
      s = s + ((i << 2) ^ (i & 7) | 1)
    else
      s = s + (i >> 1)
    end
    i += 1
  end
  s
end

#: (Integer, Float) -> Float
def call_calc_mix(n, b)
  Calc.mix(n, b) + (b.floor + b.ceil + b.round).to_f
end

# Typed Array[Float] indexing + length inside _typed
def dot_f(a, b)
  n = a.length
  acc = 0.0
  i = 0
  while i < n
    acc = acc + a[i] * b[i]
    i += 1
  end
  acc
end

# RBS @ivar reads and immediate writes inside _typed
class Point
  def initialize(x, y)
    @x = x
    @y = y
  end

  def dist
    Math.sqrt(@x * @x + @y * @y)
  end

  def sum_dist(n)
    acc = 0.0
    i = 0
    while i < n
      acc = acc + dist
      i += 1
    end
    acc
  end

  def scale!(f)
    @x = @x * f
    @y = @y * f
    @x + @y
  end
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

# 1. Verify recursive fib(25) + Float arg fallback + BigInt literal & overflow fallback
raise "fib(25)" unless fib(25) == 75025
raise "fib(1.5)" unless fib(1.5) == 0.0

#: (Integer, Integer) -> Integer
def mul_big(a, b)
  a * b
end
raise "mul_big small" unless mul_big(6, 7) == 42
raise "mul_big overflow literal" unless mul_big(1_000_000_000_000, 1_000_000_000_000) == 1_000_000_000_000_000_000_000_000

# 2. Verify mixed int/float compound loop
c = compound(10, 0.1)
raise "compound: #{c}" unless (c - 1.1**10).abs < 1e-9

# 3. Verify external .rbs cross-method float64 calls, Math.sqrt, bitwise, Calc.mix
raise "dist_sq" unless dist_sq(3.0, 4.0) == 25.0
raise "euclid" unless (euclid(3.0, 4.0) - 5.0).abs < 1e-9
merr = begin; euclid(-1.0, 0.0); false; rescue Math::DomainError; true; end
raise "Math.sqrt negative" unless euclid(0.0, 0.0) == 0.0
raise "bit_mix" unless bit_mix(10) == ((0 << 2) ^ 0 | 1) + (1 >> 1) + ((2 << 2) ^ 2 | 1) + (3 >> 1) + ((4 << 2) ^ 4 | 1) + (5 >> 1) + ((6 << 2) ^ 6 | 1) + (7 >> 1) + ((8 << 2) ^ 0 | 1) + (9 >> 1)
raise "call_calc_mix" unless (call_calc_mix(4, 1.4) - (11.4 + 1.0 + 2.0 + 1.0)).abs < 1e-9

# 4. Verify RBS Array[Float] dot product + mixed-type fallback
raise "dot_f" unless (dot_f([1.0, 2.0, 3.0], [4.0, 5.0, 6.0]) - 32.0).abs < 1e-9
raise "dot_f fallback int" unless (dot_f([1, 2], [3.0, 4.0]) - 11.0).abs < 1e-9

# 5. Verify RBS @ivar Point#dist, Point#sum_dist (typed loop calling dist!), and Point#scale!
pt = Point.new(3.0, 4.0)
raise "Point#dist" unless (pt.dist - 5.0).abs < 1e-9
raise "Point#sum_dist" unless (pt.sum_dist(100) - 500.0).abs < 1e-9
raise "Point#scale!" unless (pt.scale!(2.0) - 14.0).abs < 1e-9
raise "Point#dist after scale" unless (pt.dist - 10.0).abs < 1e-9

# 5b. Verify new string & boolean intrinsics (!, length/size, <<, strip/downcase/upcase, gsub, split)
raise "not bool" unless !false == true && !nil == true && !true == false && !0 == false
raise "str length ascii/utf8" unless "hello".length == 5 && "Čćžšđ".length == 5 && "Čćžšđ".bytesize == 10
buf = +""
200.times { buf << "ab" }
raise "str concat" unless buf.bytesize == 400 && buf.start_with?("abab")
raise "strip/down/up" unless "  Hello World! \t\n".strip == "Hello World!" && "AbC123".downcase == "abc123" && "AbC123".upcase == "ABC123"
raise "gsub plain" unless "the fox and the fox".gsub("fox", "wolf") == "the wolf and the wolf"
raise "gsub backref fallback" unless "foo".gsub("o", "\\0!") == "fo!o!"
def gsub_with_matchdata
  r = "abc".gsub("b", "X")
  [r, $~.to_s]
end
raise "gsub $~ parity" unless gsub_with_matchdata == ["aXc", "b"]
raise "split plain" unless "a,b,c,,".split(",") == ["a", "b", "c"] && "".split(",") == [] && ",,".split(",") == []
raise "split space fallback" unless "  a  b  ".split(" ") == ["a", "b"]

# 6. Verify Integer#times, Range#each, Array#each peepholes + break
ts = 0
10.times { |i| ts += i }
raise "times" unless ts == 45
tb = 10.times { |i| break i * 10 if i == 4 }
raise "times break" unless tb == 40
rs = 0
(1..5).each { |i| rs += i }
raise "range inc" unless rs == 15
re = 0
(1...5).each { |i| re += i }
raise "range exc" unless re == 10
rb = (1..10).each { |i| break i if i == 6 }
raise "range break" unless rb == 6
as = 0
[10, 20, 30].each { |x| as += x }
raise "ary each" unless as == 60

# 7. Verify core stdlib zero-cgo intrinsics
s = "hello world"
raise "bytesize" unless s.bytesize == 11
raise "str empty" unless "".empty? && !s.empty?
raise "getbyte" unless s.getbyte(0) == 104 && s.getbyte(-1) == 100 && s.getbyte(99).nil?
raise "ord ascii" unless s.ord == 104
raise "ord utf8" unless "č".ord == 269
ord_err = begin; "".ord; false; rescue ArgumentError; true; end
raise "ord empty" unless ord_err
raise "start_with?" unless s.start_with?("hello") && !s.start_with?("world")
raise "include?" unless s.include?("lo wo") && !s.include?("xyz")
arr = [7, 8, 9]
raise "ary len/size/empty/first/last" unless arr.length == 3 && arr.size == 3 && !arr.empty? && [].empty? && arr.first == 7 && arr.last == 9 && [].first.nil?
h = { a: 1, b: 2 }
raise "hash size/len/empty" unless h.size == 2 && h.length == 2 && !h.empty? && {}.empty?

# 8. Verify Ruby floor-div / floor-mod semantics (differs from Go truncation on negatives!)
raise "floor_div -5/2" unless floor_div(-5, 2) == -3
raise "floor_div 5/-2" unless floor_div(5, -2) == -3
raise "floor_mod -5%2" unless floor_mod(-5, 2) == 1
raise "floor_mod 5%-2" unless floor_mod(5, -2) == -1
zerr = begin; floor_div(5, 0); false; rescue ZeroDivisionError; true; end
raise "ZeroDivisionError fallback" unless zerr

# 9. Verify unary minus, abs, to_f
raise "neg_abs_f" unless neg_abs_f(-7) == 7.5

# 10. Verify class singleton & subclass override safety
raise "Calc.mix" unless Calc.mix(4, 1.5) == 11.5
raise "Calc#compute" unless Calc.new.compute(5) == 11
raise "SubCalc#compute" unless SubCalc.new.compute(5) == 501

# 11. Verify live monkey-patch invalidates method-cache guard cleanly!
Object.define_method(:fib) { |n| n + 1000 }
raise "monkey-patched fib" unless fib(25) == 1025

Object.define_method(:helper_sq) { |x| x * 10.0 }
raise "monkey-patched helper_sq" unless dist_sq(3.0, 4.0) == 70.0

Calc.define_singleton_method(:mix) { |n, b| b + n.to_f * 100.0 }
raise "monkey-patched Calc.mix" unless (call_calc_mix(4, 1.4) - (401.4 + 4.0)).abs < 1e-9

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
		"_typed(mrb *oruby.MrbState, _depth int, _a1 int64) (int64, bool)",
		"_typed(mrb *oruby.MrbState, _a1 int64, _a2 float64) (float64, bool)",
		"_typed(mrb *oruby.MrbState, _depth int, _a1 float64, _a2 float64) (float64, bool)",
		"_typed(mrb *oruby.MrbState, self oruby.Value) (float64, bool)",
		"_typed(mrb *oruby.MrbState, _a1 oruby.Value, _a2 oruby.Value) (float64, bool)",
		"aotMathSqrt(",
		"AotCompiledMethodOK(",
		"AotConstMethodOK(",
		"AotCoreConstOK(",
		"AotCoreOK(oruby.AotCoreIntTimes",
		"AotCoreOK(oruby.AotCoreRangeEach",
		"AotDefineMethodTrackedSig(",
		"AotBigInt(",
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
