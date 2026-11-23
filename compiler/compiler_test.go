package compiler

import (
	"context"
	json "encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/thread"
)

func generatedBundleOutput(t *testing.T, files map[string]string, mainBody string, extraFiles ...string) string {
	t.Helper()
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(projectDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	resolver := NewDependencyResolver(mrb, nil)
	extraPaths := make([]string, len(extraFiles))
	for i, path := range extraFiles {
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectDir, path)
		}
		extraPaths[i] = path
	}
	modules, skipped, err := resolver.ResolveWith(filepath.Join(projectDir, "main.rb"), extraPaths)
	if err != nil {
		t.Fatalf("resolve bundle: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("explicit bundle files were skipped: %v", skipped)
	}
	source, err := NewCodeGen(CodeGenOptions{PackageName: "main"}).GenerateBundle(modules)
	if err != nil {
		t.Fatalf("generate bundle: %v", err)
	}

	buildDir := t.TempDir()
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goMod := "module compiled_bundle_test\n\ngo 1.27\n\nrequire github.com/oruby/oruby v0.0.0\n\nreplace github.com/oruby/oruby => " + repoRoot + "\n"
	mainSrc := "package main\n\nimport (\n\t\"fmt\"\n\t\"github.com/oruby/oruby\"\n\t_ \"github.com/oruby/oruby/gem/stdlib\"\n)\n\nfunc main() {\n" + mainBody + "\n}\n"
	for name, content := range map[string]string{"go.mod": goMod, "compiled.go": source, "main.go": mainSrc} {
		if err := os.WriteFile(filepath.Join(buildDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-mod=mod", "-o", "compiled_bundle", ".")
	build.Dir = buildDir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build generated bundle: %v\n%s\nGenerated source:\n%s", err, output, source)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	run := exec.CommandContext(ctx, filepath.Join(buildDir, "compiled_bundle"))
	run.Dir = projectDir
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run generated bundle: %v (context: %v)\n%s", err, ctx.Err(), output)
	}
	return strings.TrimSpace(string(output))
}

func TestGeneratedBundleRequireAliasesArePerStateAndLoadRepeats(t *testing.T) {
	output := generatedBundleOutput(t, map[string]string{
		"main.rb": `$load_count = 0
require_relative "lib/feature"
require "feature"
require "lib/feature"`,
		"lib/feature.rb": `require "feature"
$load_count += 1`,
		"external.rb": `$filesystem_fallback = 29`,
	}, `
check := func(err error) { if err != nil { panic(err) } }
count := func(mrb *oruby.MrbState) string { value, err := mrb.Eval("$load_count"); check(err); return mrb.String(value) }
first := oruby.MrbOpen()
_, err := first.Resolve("load"); check(err)
_, err = Run(first); check(err)
fmt.Println(count(first))
_, err = first.Eval(`+"`load 'feature.rb'`"+`); check(err)
_, err = first.Eval(`+"`load 'feature.rb'`"+`); check(err)
fmt.Println(count(first))
_, err = first.Eval(`+"`require 'lib/feature'`"+`); check(err)
fmt.Println(count(first))
fallback, err := first.Eval(`+"`load './external.rb'`"+`); check(err)
fallback, err = first.Eval("$filesystem_fallback"); check(err)
fmt.Println(first.String(fallback))
first.Close()
second := oruby.MrbOpen()
_, err = second.Resolve("load"); check(err)
_, err = Run(second); check(err)
fmt.Println(count(second))
second.Close()
`)
	want := "1\n3\n3\n29\n1"
	if output != want {
		t.Fatalf("generated bundle output = %q; want %q", output, want)
	}
}

func TestGeneratedBundleRequireRetriesAfterFailure(t *testing.T) {
	output := generatedBundleOutput(t, map[string]string{
		"main.rb": `begin
require_relative "lib/failure"
rescue
end`,
		"lib/failure.rb": `if $retry_attempts == nil
  $retry_attempts = 0
end
$retry_attempts += 1
raise "first load failed" if $retry_attempts == 1`,
	}, `
mrb := oruby.MrbOpen()
if _, err := mrb.Resolve("load"); err != nil { panic(err) }
if _, err := Run(mrb); err != nil { panic(err) }
first, err := mrb.Eval("$retry_attempts"); if err != nil { panic(err) }
fmt.Println(mrb.String(first))
if _, err := mrb.Eval(`+"`require_relative 'lib/failure'`"+`); err != nil { panic(err) }
second, err := mrb.Eval("$retry_attempts"); if err != nil { panic(err) }
fmt.Println(mrb.String(second))
mrb.Close()
`)
	if output != "1\n2" {
		t.Fatalf("generated bundle retry output = %q; want %q", output, "1\n2")
	}
}

func TestGeneratedBundleExplicitExtraFileLoadsByRubyPath(t *testing.T) {
	output := generatedBundleOutput(t, map[string]string{
		"main.rb": `$extra_count = 0
load "features/dynamic.rb"`,
		"features/dynamic.rb": `$extra_count += 1`,
	}, `
mrb := oruby.MrbOpen()
if _, err := mrb.Resolve("load"); err != nil { panic(err) }
if _, err := Run(mrb); err != nil { panic(err) }
value, err := mrb.Eval("$extra_count"); if err != nil { panic(err) }
fmt.Println(mrb.String(value))
mrb.Close()
`, "features/dynamic.rb")
	if output != "1" {
		t.Fatalf("explicit extra file output = %q; want %q", output, "1")
	}
}

func TestCompileBundleReportsInvalidExplicitExtraFile(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir := t.TempDir()
	entry := filepath.Join(dir, "main.rb")
	extra := filepath.Join(dir, "extra.rb")
	if err := os.WriteFile(entry, []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("if true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	comp := New(Options{PackageName: "main"})
	if _, err := comp.CompileBundleWith(mrb, entry, []string{extra}, nil); err != nil {
		t.Fatalf("compile bundle with invalid extra file: %v", err)
	}
	if len(comp.Skipped) != 1 || !strings.Contains(comp.Skipped[0].Error(), extra) {
		t.Fatalf("invalid explicit file was not reported: %v", comp.Skipped)
	}
}

func TestCompiler_CompileRubyToSource(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	rubyCode := `
def multiply(a, b)
  a * b
end

x = 6
y = 7
multiply(x, y)
`
	goSrc, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	t.Logf("Generated Go Code:\n%s", goSrc)

	if !strings.Contains(goSrc, "func IRep_0(") {
		t.Errorf("Expected func IRep_0 in generated code")
	}
	if !strings.Contains(goSrc, "func IRep_1(") {
		t.Errorf("Expected func IRep_1 in generated code for multiply method")
	}
	if !strings.Contains(goSrc, `_ "github.com/oruby/oruby/gem/stdlib"`) {
		t.Errorf("Expected gem/stdlib import in standalone generated code")
	}
	if !strings.Contains(goSrc, "fastMul(mrb,") {
		t.Errorf("Expected fastMul in generated code")
	}
}

func TestCompiler_EndToEndExecution(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	rubyCode := `
def fib(n)
  if n <= 1
    n
  else
    fib(n - 1) + fib(n - 2)
  end
end

fib(10)
`
	goSrc, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	// Create a temporary directory to build and run the generated Go code
	tmpDir, err := os.MkdirTemp("", "oruby_compiler_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFilePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFilePath, []byte(goSrc), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Create a go.mod pointing to current repo
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	repoRoot := filepath.Dir(wd) // compiler is in <repoRoot>/compiler

	goModContent := `module compiled_app

go 1.22

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod error: %v", err)
	}

	// Run go mod tidy
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	if tidyOut, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy error: %v\nOutput: %s", err, string(tidyOut))
	}

	// Build the compiled binary
	binPath := filepath.Join(tmpDir, "compiled_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build error: %v\nOutput: %s\nGenerated Go Source:\n%s", err, string(out), goSrc)
	}

	// Run the compiled binary
	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	out, err = runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	t.Logf("Execution output: %s", resultStr)

	// fib(10) == 55
	if resultStr != "55" {
		t.Errorf("Expected fib(10) output 55, got: %q", resultStr)
	}
}

type exceptionOutcome struct {
	Kind    string
	Class   string
	Message string
	Trace   string
	Pending bool
}

func interpretedExceptionOutcome(mrb *oruby.MrbState, value oruby.MrbValue, err error) exceptionOutcome {
	out := exceptionOutcome{Kind: "return", Trace: mrb.String(mrb.GetGV("$trace")), Pending: mrb.Exc() != nil}
	if err != nil {
		out.Kind = "raise"
		if exc := mrb.Exc(); exc != nil {
			value = exc
		} else {
			out.Message = err.Error()
			return out
		}
	}
	out.Class = mrb.ObjClassname(value)
	if value.Type() == oruby.MrbTTException {
		mrb.GCProtect(value)
		mrb.ExcClear()
		message, err := mrb.Funcall(value, mrb.Intern("message"))
		if err != nil {
			out.Message = "message failed: " + err.Error()
		} else {
			out.Message = mrb.String(message)
		}
	} else {
		out.Message = mrb.String(value)
	}
	return out
}

func compiledExceptionOutcome(t *testing.T, source string) exceptionOutcome {
	t.Helper()
	tmpDir := t.TempDir()
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goMod := "module exception_parity\n\ngo 1.27\n\nrequire github.com/oruby/oruby v0.0.0\n\nreplace github.com/oruby/oruby => " + repoRoot + "\n"
	mainSrc := `package main

import (
	json "encoding/json/v2"
	"os"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/thread"
)

func main() {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	value, err := Run(mrb)
	out := struct { Kind, Class, Message, Trace string; Pending bool }{Kind: "return", Trace: mrb.String(mrb.GetGV("$trace")), Pending: mrb.Exc() != nil}
	if err != nil {
		out.Kind = "raise"
		if exc := mrb.Exc(); exc != nil {
			value = exc.Value()
		} else {
			out.Message = err.Error()
			json.MarshalWrite(os.Stdout, out)
			return
		}
	}
	out.Class = mrb.ObjClassname(value)
	if value.Type() == oruby.MrbTTException {
		mrb.GCProtect(value)
		mrb.ExcClear()
		message, err := mrb.Funcall(value, mrb.Intern("message"))
		if err != nil {
			out.Message = "message failed: " + err.Error()
		} else {
			out.Message = mrb.String(message)
		}
	} else {
		out.Message = mrb.String(value)
	}
	json.MarshalWrite(os.Stdout, out)
}
`
	for name, content := range map[string]string{"go.mod": goMod, "compiled.go": source, "main.go": mainSrc} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-mod=mod", "-o", "compiled_app", ".")
	build.Dir = tmpDir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build generated program: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	run := exec.CommandContext(ctx, filepath.Join(tmpDir, "compiled_app"))
	run.Dir = tmpDir
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run generated program: %v (context: %v)\n%s", err, ctx.Err(), output)
	}
	var out exceptionOutcome
	if err := json.Unmarshal(output, &out); err != nil {
		t.Fatalf("decode generated program output: %v\n%s", err, output)
	}
	return out
}

func TestCompiler_ExceptionParity(t *testing.T) {
	tests := []struct {
		name string
		code string
		want exceptionOutcome
	}{
		{
			name: "nested_rescue_and_ensure",
			code: `$trace = ""
begin
  begin
    $trace += "body;"
    raise ArgumentError, "inner"
  rescue ArgumentError => e
    $trace += "inner rescue;"
    raise RuntimeError, e.message + " rethrown"
  ensure
    $trace += "inner ensure;"
  end
rescue RuntimeError => e
  $trace += "outer rescue;"
  result = e.message
ensure
  $trace += "outer ensure;"
end
result`,
			want: exceptionOutcome{Kind: "return", Class: "String", Message: "inner rethrown", Trace: "body;inner rescue;inner ensure;outer rescue;outer ensure;"},
		},
		{
			name: "reraise_through_nested_ensures",
			code: `$trace = ""
begin
  begin
    raise ArgumentError, "original"
  rescue ArgumentError => e
    $trace += "rescue;"
    raise e
  ensure
    $trace += "inner ensure;"
  end
ensure
  $trace += "outer ensure;"
end`,
			want: exceptionOutcome{Kind: "raise", Class: "ArgumentError", Message: "original", Trace: "rescue;inner ensure;outer ensure;", Pending: true},
		},
		{
			name: "ensure_on_return",
			code: `$trace = ""
def result_with_ensure
  begin
    begin
      return "returned"
    ensure
      $trace += "inner ensure;"
    end
  ensure
    $trace += "outer ensure;"
  end
end
result_with_ensure`,
			want: exceptionOutcome{Kind: "return", Class: "String", Message: "returned", Trace: "inner ensure;outer ensure;"},
		},
		{
			name: "ensure_replaces_exception",
			code: `$trace = ""
begin
  begin
    raise ArgumentError, "original"
  ensure
    $trace += "inner ensure;"
    raise RuntimeError, "replacement"
  end
ensure
  $trace += "outer ensure;"
end`,
			want: exceptionOutcome{Kind: "raise", Class: "RuntimeError", Message: "replacement", Trace: "inner ensure;outer ensure;", Pending: true},
		},
		{
			name: "returned_exception_value",
			code: `$trace = ""
begin
  ArgumentError.new("not raised")
ensure
  $trace += "ensure;"
end`,
			want: exceptionOutcome{Kind: "return", Class: "ArgumentError", Message: "not raised", Trace: "ensure;"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mrb := oruby.MrbOpen()
			defer mrb.Close()
			value, err := mrb.Eval(tt.code)
			interpreted := interpretedExceptionOutcome(mrb, value, err)
			if interpreted != tt.want {
				t.Errorf("interpreted outcome = %+v, want %+v", interpreted, tt.want)
			}

			compilerState := oruby.MrbOpen()
			defer compilerState.Close()
			comp := New(Options{PackageName: "main"})
			source, err := comp.CompileRuby(compilerState, tt.code)
			if err != nil {
				t.Fatalf("CompileRuby: %v", err)
			}
			compiled := compiledExceptionOutcome(t, source)
			if compiled != interpreted {
				t.Errorf("compiled outcome = %+v, interpreted = %+v", compiled, interpreted)
			}
		})
	}
}

// Overflow out of the pure-integer fast path promotes to a bigint, as in MRI.
func TestCompiler_SpecializedIntegerOverflowParity(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "multiply_overflow",
			code: `$trace = ""
def square(value)
  value * value
end
square(3037000500).to_s`,
			want: "9223372037000250000",
		},
		{
			name: "add_overflow",
			code: `$trace = ""
def increment(value)
  value + 1
end
increment(9223372036854775807).to_s`,
			want: "9223372036854775808",
		},
		{
			name: "subtract_overflow",
			code: `$trace = ""
def decrement(value)
  value - 1
end
decrement(-9223372036854775807 - 1).to_s`,
			want: "-9223372036854775809",
		},
		{
			name: "min_integer_multiply_overflow",
			code: `$trace = ""
def negate(value)
  value * -1
end
negate(-9223372036854775807 - 1).to_s`,
			want: "9223372036854775808",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mrb := oruby.MrbOpen()
			defer mrb.Close()
			value, err := mrb.Eval(tt.code)
			interpreted := interpretedExceptionOutcome(mrb, value, err)

			compilerState := oruby.MrbOpen()
			defer compilerState.Close()
			comp := New(Options{PackageName: "main"})
			source, err := comp.CompileRuby(compilerState, tt.code)
			if err != nil {
				t.Fatalf("CompileRuby: %v", err)
			}
			if !strings.Contains(source, "_int(") {
				t.Fatalf("test did not exercise integer specialization:\n%s", source)
			}
			compiled := compiledExceptionOutcome(t, source)
			if compiled != interpreted || compiled.Kind != "return" || compiled.Message != tt.want {
				t.Errorf("compiled outcome = %+v, interpreted = %+v, want %s", compiled, interpreted, tt.want)
			}
		})
	}
}

func TestCompiler_SpecializedIntegerOperatorRedefinitionParity(t *testing.T) {
	code := `
def add_one(value)
  value + 1
end
class Integer
  def +(other)
    99
  end
end
add_one(2).to_s
`
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	value, err := mrb.Eval(code)
	if err != nil {
		t.Fatal(err)
	}
	interpreted := interpretedExceptionOutcome(mrb, value, nil)

	compilerState := oruby.MrbOpen()
	defer compilerState.Close()
	comp := New(Options{PackageName: "main"})
	source, err := comp.CompileRuby(compilerState, code)
	if err != nil {
		t.Fatalf("CompileRuby: %v", err)
	}
	if !strings.Contains(source, "_int(") {
		t.Fatalf("test did not exercise integer specialization:\n%s", source)
	}
	compiled := compiledExceptionOutcome(t, source)
	if compiled != interpreted {
		t.Fatalf("compiled outcome = %+v, interpreted = %+v", compiled, interpreted)
	}
}

func TestCompiler_InlineNumericOperatorRedefinitionParity(t *testing.T) {
	cases := map[string]string{
		"float": `
def scale(x)
  x * 2.0
end
r = [scale(1.5)]
class Float
  def *(other)
    -1.0
  end
end
r << scale(1.5)
r.inspect
`,
		"compare": `
def lower(a, b)
  if a < b then :yes else :no end
end
r = [lower(1, 2)]
class Integer
  def <(other)
    false
  end
end
r << lower(1, 2)
r.inspect
`,
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			mrb := oruby.MrbOpen()
			defer mrb.Close()
			value, err := mrb.Eval(code)
			if err != nil {
				t.Fatal(err)
			}
			interpreted := interpretedExceptionOutcome(mrb, value, nil)

			compilerState := oruby.MrbOpen()
			defer compilerState.Close()
			source, err := New(Options{PackageName: "main"}).CompileRuby(compilerState, code)
			if err != nil {
				t.Fatalf("CompileRuby: %v", err)
			}
			if compiled := compiledExceptionOutcome(t, source); compiled != interpreted {
				t.Fatalf("compiled outcome = %+v, interpreted = %+v", compiled, interpreted)
			}
		})
	}
}

func TestCompiler_IntegerDivisionParity(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{
			name: "floor_rounding",
			code: `
def quotient(left, right)
  left / right
end
[quotient(-7, 3), quotient(7, -3), quotient(-7, -3), quotient(6, -3)].map { |value| value.to_s }.join(",")
`,
		},
		{
			name: "minimum_integer_by_negative_one",
			code: `
def quotient(left, right)
  left / right
end
quotient(-9223372036854775807 - 1, -1).to_s
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mrb := oruby.MrbOpen()
			defer mrb.Close()
			value, err := mrb.Eval(tt.code)
			interpreted := interpretedExceptionOutcome(mrb, value, err)

			compilerState := oruby.MrbOpen()
			defer compilerState.Close()
			comp := New(Options{PackageName: "main"})
			source, err := comp.CompileRuby(compilerState, tt.code)
			if err != nil {
				t.Fatalf("CompileRuby: %v", err)
			}
			if !strings.Contains(source, "aotDivII(") {
				t.Fatalf("test did not exercise integer division specialization:\n%s", source)
			}
			compiled := compiledExceptionOutcome(t, source)
			if compiled != interpreted {
				t.Fatalf("compiled outcome = %+v, interpreted = %+v", compiled, interpreted)
			}
		})
	}
}

func TestCompiler_DirectCallMethodRedefinitionParity(t *testing.T) {
	code := `
def value(amount)
  amount + 1
end
def caller(amount)
  value(amount)
end
eval("def value(amount); amount + 2; end")
caller(1).to_s
`
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	value, err := mrb.Eval(code)
	if err != nil {
		t.Fatal(err)
	}
	interpreted := interpretedExceptionOutcome(mrb, value, nil)

	compilerState := oruby.MrbOpen()
	defer compilerState.Close()
	comp := New(Options{PackageName: "main"})
	source, err := comp.CompileRuby(compilerState, code)
	if err != nil {
		t.Fatalf("CompileRuby: %v", err)
	}
	if !strings.Contains(source, "IRep_1_int(") || strings.Contains(source, "IRep_2_int(") {
		t.Fatalf("rebindable nested call must use generic dispatch:\n%s", source)
	}
	compiled := compiledExceptionOutcome(t, source)
	if interpreted.Message != "3" || compiled != interpreted {
		t.Fatalf("compiled outcome = %+v, interpreted = %+v", compiled, interpreted)
	}
}

func TestCodeGenIntSpecializationRejectsUnsupportedSelf(t *testing.T) {
	unit := &IRepUnit{Instructions: []Instruction{
		{Op: OpENTER},
		{Op: OpLOADSELF, A: 1},
		{Op: OpRETURN, A: 1},
	}}
	codegen := NewCodeGen(CodeGenOptions{})
	codegen.info[unit] = &unitInfo{fn: "method", method: "method"}
	codegen.compiledArity["method"] = 1

	if codegen.canEmitIntSpecialized(unit, 1) {
		t.Fatal("integer specialization accepted OpLOADSELF without an emitter")
	}
}

func TestCompiler_StringConcatenation(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	rubyCode := `
def greet(name)
  "Hello, " + name
end

greet("ORuby")
`
	goSrc, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "oruby_compiler_str_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFilePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFilePath, []byte(goSrc), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	repoRoot := filepath.Dir(wd)

	goModContent := `module compiled_app

go 1.22

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod error: %v", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	if tidyOut, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy error: %v\nOutput: %s", err, string(tidyOut))
	}

	binPath := filepath.Join(tmpDir, "compiled_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s\nGenerated Go Source:\n%s", err, string(out), goSrc)
	}

	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	t.Logf("Execution output: %s", resultStr)

	if resultStr != "Hello, ORuby" {
		t.Errorf("Expected 'Hello, ORuby', got: %q", resultStr)
	}
}

func TestCompiler_ArraysAndBlocks(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	rubyCode := `
total = 0
[10, 20, 30].each do |x|
  total = total + x
end
total
`
	goSrc, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "oruby_compiler_ary_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFilePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFilePath, []byte(goSrc), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	repoRoot := filepath.Dir(wd)

	goModContent := `module compiled_app

go 1.22

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod error: %v", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	_ = tidyCmd.Run()

	binPath := filepath.Join(tmpDir, "compiled_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s\nGenerated Go Source:\n%s", err, string(out), goSrc)
	}

	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	t.Logf("Execution output: %s", resultStr)

	// total should be 60 (10 + 20 + 30)
	if resultStr != "60" {
		t.Errorf("Expected 60, got: %q", resultStr)
	}
}

func TestCompiler_HashAndIndex(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	rubyCode := `
h = { "name" => "oruby", "score" => 100 }
h["score"]
`
	goSrc, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "oruby_compiler_hash_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFilePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFilePath, []byte(goSrc), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	repoRoot := filepath.Dir(wd)

	goModContent := `module compiled_app

go 1.22

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod error: %v", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	_ = tidyCmd.Run()

	binPath := filepath.Join(tmpDir, "compiled_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s\nGenerated Go Source:\n%s", err, string(out), goSrc)
	}

	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	t.Logf("Execution output: %s", resultStr)

	if resultStr != "100" {
		t.Errorf("Expected 100, got: %q", resultStr)
	}
}

func TestCompiler_RubySpecLanguageSuite(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	rubyCode := `
class AssertionFailure < StandardError; end

def assert_equal(actual, expected)
  if actual != expected
    raise AssertionFailure, "Expected #{expected.inspect}, got #{actual.inspect}"
  end
end

# 1. Conditionals (spec/ruby/language/if_spec.rb)
val = if 1 == 1
        "branch_a"
      else
        "branch_b"
      end
assert_equal(val, "branch_a")

# 2. While loop (spec/ruby/language/while_spec.rb)
counter = 0
sum = 0
while counter < 5
  counter += 1
  sum += counter
end
assert_equal(sum, 15)

# 3. Array & Block iteration (spec/ruby/language/block_spec.rb)
doubled = []
[1, 2, 3].each do |item|
  doubled << (item * 2)
end
assert_equal(doubled, [2, 4, 6])

# 4. Hash literals & lookup (spec/ruby/language/hash_spec.rb)
dict = { "key1" => 42, "key2" => 99 }
assert_equal(dict["key1"], 42)

"ALL_SPECS_PASSED"
`
	goSrc, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "oruby_compiler_spec_suite_*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFilePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFilePath, []byte(goSrc), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	repoRoot := filepath.Dir(wd)

	goModContent := `module compiled_app

go 1.22

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod error: %v", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	_ = tidyCmd.Run()

	binPath := filepath.Join(tmpDir, "compiled_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s\nGenerated Go Source:\n%s", err, string(out), goSrc)
	}

	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	t.Logf("Execution output: %s", resultStr)

	if resultStr != "ALL_SPECS_PASSED" {
		t.Errorf("Expected 'ALL_SPECS_PASSED', got: %q", resultStr)
	}
}

func TestCompiler_Phase3_HTTPAndJSON(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := New(Options{
		PackageName: "main",
		Standalone:  true,
	})

	rubyCode := `
parsed = JSON.parse('{"status": "ok", "value": 42}')
json_out = JSON.generate({"message" => "Phase3 Success", "val" => parsed["value"]})

server = HTTP::Server.new("127.0.0.1:0")
server.handle("/status") do |req|
  [200, {"Content-Type" => "application/json"}, [json_out]]
end
server.start_async
port = server.port

resp = HTTP.get("http://127.0.0.1:#{port}/status")
server.stop

puts "#{resp.status}:#{resp.body}"
`
	goSrc, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "oruby-phase3-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goSrc), 0644); err != nil {
		t.Fatalf("WriteFile main.go error: %v", err)
	}

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("Abs repoRoot error: %v", err)
	}

	goModContent := `module phase3app

go 1.27

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod error: %v", err)
	}

	// Copy go.sum if present
	if sumBytes, err := os.ReadFile(filepath.Join(repoRoot, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(tmpDir, "go.sum"), sumBytes, 0644)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	_ = tidyCmd.Run()

	binPath := filepath.Join(tmpDir, "phase3_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s", err, string(out))
	}

	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	resultStr := strings.TrimSpace(string(out))
	t.Logf("Execution output: %s", resultStr)

	if !strings.Contains(resultStr, "200:") || !strings.Contains(resultStr, "Phase3 Success") {
		t.Errorf("Unexpected output: %q", resultStr)
	}
}

func TestCompiler_Phase4_OptionalEmbedding(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// 1. Without embed dirs (default: disk files)
	compDefault := New(Options{
		PackageName: "main",
		Standalone:  true,
		EmbedDirs:   nil, // Default: no embedding
	})

	codeDefault := `puts "Running from disk"`
	srcDefault, err := compDefault.CompileRuby(mrb, codeDefault)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}
	if strings.Contains(srcDefault, "//go:embed") || strings.Contains(srcDefault, "\"embed\"") {
		t.Errorf("Default build should NOT include //go:embed or embed package")
	}

	// 2. With embed dirs (optional setting)
	tmpDir, err := os.MkdirTemp("", "oruby-embed-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	viewsDir := filepath.Join(tmpDir, "views")
	_ = os.MkdirAll(viewsDir, 0755)
	_ = os.WriteFile(filepath.Join(viewsDir, "hello.html"), []byte("<h1>Hello Embedded Views!</h1>"), 0644)

	compEmbed := New(Options{
		PackageName: "main",
		Standalone:  true,
		EmbedDirs:   []string{"views/*"},
	})

	rubyCode := `
content = ORuby::Embed.read("views/hello.html")
puts content
`
	srcEmbed, err := compEmbed.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}

	if !strings.Contains(srcEmbed, "//go:embed views/*") {
		t.Errorf("Expected //go:embed views/* in generated code")
	}
	if !strings.Contains(srcEmbed, "\"embed\"") {
		t.Errorf("Expected \"embed\" import in generated code")
	}

	// Compile and run the binary with embedded views
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(srcEmbed), 0644); err != nil {
		t.Fatalf("WriteFile main.go error: %v", err)
	}

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("Abs repoRoot error: %v", err)
	}

	goModContent := `module embedapp

go 1.27

require github.com/oruby/oruby v0.0.0

replace github.com/oruby/oruby => ` + repoRoot + "\n"

	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644)
	if sumBytes, err := os.ReadFile(filepath.Join(repoRoot, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(tmpDir, "go.sum"), sumBytes, 0644)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	_ = tidyCmd.Run()

	binPath := filepath.Join(tmpDir, "embed_app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\nOutput: %s", err, string(out))
	}

	runCmd := exec.Command(binPath)
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run error: %v\nOutput: %s", err, string(out))
	}

	outputStr := strings.TrimSpace(string(out))
	if outputStr != "<h1>Hello Embedded Views!</h1>" {
		t.Errorf("Unexpected embedded read output: %q", outputStr)
	}
}

func TestCompiler_DebugAndCrossMethodInt(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// 1. Verify cross-method sends stay dynamic when targets can be redefined
	compFast := New(Options{
		PackageName: "compiled",
		Standalone:  false,
	})
	srcFast, err := compFast.CompileRuby(mrb, `
def helper(x)
  x * 2 + 1
end

def compute(n)
  s = 0
  i = 0
  while i < n
    s = s + helper(i)
    i = i + 1
  end
  s
end
`)
	if err != nil {
		t.Fatalf("CompileRuby error: %v", err)
	}
	if !strings.Contains(srcFast, "IRep_1_int") || strings.Contains(srcFast, "IRep_2_int") || !strings.Contains(srcFast, "AotCall1") {
		t.Errorf("Expected helper to remain specialized while compute dynamically dispatches to it:\n%s", srcFast)
	}

	// 2. Verify compiled debug mode (Kernel#debug + line stepping)
	compDbg := New(Options{
		PackageName: "main",
		Standalone:  false,
		Debug:       true,
	})
	srcDbg, err := compDbg.CompileRuby(mrb, `
x = 15
y = 25
z = debug(x, y)
x + y
`)
	if err != nil {
		t.Fatalf("CompileRuby debug error: %v", err)
	}
	if !strings.Contains(srcDbg, "mrb.TriggerDebug(\"step\"") || !strings.Contains(srcDbg, "mrb.TriggerDebug(\"debug\"") {
		t.Errorf("Expected step and debug TriggerDebug calls in generated debug code:\n%s", srcDbg)
	}
}

func TestCompiler_Threads(t *testing.T) {
	rubyCode := `
results = []

# 1. Thread.new + Mutex + Queue
m = Mutex.new
q = Queue.new
sum = 0
threads = (1..4).map do |i|
  Thread.new(i) do |x|
    m.synchronize { sum += x }
    q << (x * 10)
  end
end
threads.each(&:join)
popped = []
4.times { popped << q.pop }
results << "sum=#{sum}:q=#{popped.sort.join(",")}"

# 2. Preemption of a compiled tight loop (and _int-specialized loop)
def int_spin(limit)
  i = 0
  while i < limit
    i += 1
  end
  i
end

$ready = false
$stop = false
spinner = Thread.new do
  $ready = true
  i = 0
  while !$stop
    i += 1
  end
  i
end
Thread.pass until $ready
# Main thread gets scheduled via time-slice preemption while spinner is in a compiled tight loop
$stop = true
results << "preempt=#{spinner.value > 0}"

# 3. Thread#raise and Thread#kill run ensure in compiled code
$in_loop = false
$ensured_raise = false
t_raise = Thread.new do
  begin
    $in_loop = true
    i = 0
    while true
      i += 1
    end
  rescue RuntimeError => e
    "caught:#{e.message}"
  ensure
    $ensured_raise = true
  end
end
Thread.pass until $in_loop
t_raise.raise(RuntimeError, "boom")
results << "raise=#{t_raise.value}:ens=#{$ensured_raise}"

$in_kill = false
$ensured_kill = false
t_kill = Thread.new do
  begin
    $in_kill = true
    int_spin(1_000_000_000_000)
  ensure
    $ensured_kill = true
  end
end
Thread.pass until $in_kill
t_kill.kill
t_kill.join
results << "kill_ens=#{$ensured_kill}"

# 4. Cross-thread non-local return/break in compiled method raises LocalJumpError
def cross_thread_return
  t = Thread.new do
    Thread.current.report_on_exception = false
    return 42
  end
  t.join
end
begin
  cross_thread_return
  results << "return=no_error"
rescue LocalJumpError
  results << "return=LocalJumpError"
end

def cross_thread_break
  t = Thread.new do
    Thread.current.report_on_exception = false
    break 42
  end
  t.join
end
begin
  cross_thread_break
  results << "break=no_error"
rescue LocalJumpError
  results << "break=LocalJumpError"
end

results.join("|")
`
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	comp := New(Options{PackageName: "main"})
	source, err := comp.CompileRuby(mrb, rubyCode)
	if err != nil {
		t.Fatalf("CompileRuby: %v", err)
	}
	got := compiledExceptionOutcome(t, source)
	want := "sum=10:q=10,20,30,40|preempt=true|raise=caught:boom:ens=true|kill_ens=true|return=LocalJumpError|break=LocalJumpError"
	if got.Kind != "return" || got.Message != want {
		t.Fatalf("compiled outcome = %+v, want %q", got, want)
	}
}
