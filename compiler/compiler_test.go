package compiler

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oruby/oruby"
)

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
	"encoding/json"
	"os"

	"github.com/oruby/oruby"
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
			json.NewEncoder(os.Stdout).Encode(out)
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
	json.NewEncoder(os.Stdout).Encode(out)
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

	// 1. Verify cross-method _int specialization
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
	if !strings.Contains(srcFast, "IRep_1_int") || !strings.Contains(srcFast, "IRep_2_int") {
		t.Errorf("Expected both helper (IRep_1_int) and compute (IRep_2_int) to be _int specialized:\n%s", srcFast)
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
