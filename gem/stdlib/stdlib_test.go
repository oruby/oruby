package stdlib_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestStdlibImportRegistersProviders(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	if _, err := mrb.Resolve("base64"); err != nil {
		t.Fatalf("Resolve(base64): %v", err)
	}
	got, err := mrb.Eval(`Base64.strict_encode64("stdlib")`)
	if err != nil || mrb.String(got) != "c3RkbGli" {
		t.Fatalf("Base64.strict_encode64 = %q, %v; want c3RkbGli", mrb.String(got), err)
	}

	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatalf("Resolve(load): %v", err)
	}
	if got, err := mrb.Eval(`require "pp"`); err != nil || !got.Bool() {
		t.Fatalf("first require(pp) = %v, %v; want true, nil", got, err)
	}
	if got, err := mrb.Eval(`require "pp"`); err != nil || got.Bool() {
		t.Fatalf("repeat require(pp) = %v, %v; want false, nil", got, err)
	}
	if loaded, err := mrb.Resolve("pp"); err != nil || loaded {
		t.Fatalf("Resolve(pp) after require = %v, %v; want false, nil", loaded, err)
	}

	if _, err := mrb.Resolve("oruby_missing_stdlib_feature"); err == nil {
		t.Fatal("Resolve(missing feature) should fail")
	}
}

func TestStdlibImportIndependentStates(t *testing.T) {
	first := oruby.MrbOpen()
	defer first.Close()
	second := oruby.MrbOpen()
	defer second.Close()

	for _, mrb := range []*oruby.MrbState{first, second} {
		if _, err := mrb.Resolve("base64"); err != nil {
			t.Fatalf("Resolve(base64): %v", err)
		}
	}
	if _, err := first.Eval(`$stdlib_state = "first"`); err != nil {
		t.Fatal(err)
	}
	got, err := second.Eval(`defined?($stdlib_state)`)
	if err != nil || !got.IsNil() {
		t.Fatalf("independent state global = %v, %v; want nil", got, err)
	}
}

func TestPPLazyLoadAndOutput(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	defined, err := mrb.Eval("defined?(PP)")
	if err != nil {
		t.Fatal(err)
	}
	if !defined.IsNil() {
		t.Fatal("PP should not be defined before require")
	}

	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	loaded, err := mrb.Eval("require 'pp'")
	if err != nil || !loaded.Bool() {
		t.Fatalf("require(pp) = %v, %v; want true, nil", loaded, err)
	}

	got, err := mrb.Eval(`
    class PPCapture
      def initialize; @text = ""; end
      def write(text); @text << text; end
      def to_s; @text; end
    end
    out = PPCapture.new
    result = PP.pp([1, 2, 3], out)
    "#{out.to_s}|#{result.equal?(out)}"
  `)
	if err != nil {
		t.Fatal(err)
	}
	if got := mrb.String(got); got != "[1, 2, 3]\n|true" {
		t.Fatalf("PP.pp output = %q, want %q", got, "[1, 2, 3]\n|true")
	}

	got, err = mrb.Eval(`
    out = PPCapture.new
    PP.pp({'key' => 42}, out)
    out.to_s == {'key' => 42}.inspect + "\n"
  `)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Bool() {
		t.Fatal("PP.pp hash output should match Hash#inspect")
	}

	got, err = mrb.Eval(`
    $stdout = PPCapture.new
    PP.pp([1, 2, 3])
    $stdout.to_s
  `)
	if err != nil {
		t.Fatal(err)
	}
	if got := mrb.String(got); got != "[1, 2, 3]\n" {
		t.Fatalf("PP.pp default output = %q", got)
	}
}

func TestBenchmarkTimingAndErrors(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	loaded, err := mrb.Eval("require 'benchmark'")
	if err != nil || !loaded.Bool() {
		t.Fatalf("require(benchmark) = %v, %v; want true, nil", loaded, err)
	}

	got, err := mrb.Eval(`
    $benchmark_count = 0
    result = Benchmark.measure { $benchmark_count += 1 }
    $benchmark_count == 1 && result.is_a?(Benchmark::Tms) &&
      result.real >= 0 && result.total == 0 &&
      Benchmark.realtime { $benchmark_count += 1 } >= 0 && $benchmark_count == 2
  `)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Bool() {
		t.Fatal("Benchmark should execute blocks once and report nonnegative elapsed time")
	}

	if _, err := mrb.Eval(`Benchmark.measure { raise 'benchmark failure' }`); err == nil || !strings.Contains(err.Error(), "benchmark failure") {
		t.Fatalf("Benchmark.measure error = %v, want propagated block exception", err)
	}
}

func TestFileUtilsSymbolicChmod(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Resolve("io/file"); err != nil {
		t.Fatal(err)
	}

	if loaded, err := mrb.Resolve("fileutils"); err != nil || !loaded {
		t.Fatalf("Resolve(fileutils) = %v, %v; want true, nil", loaded, err)
	}

	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0640); err != nil {
		t.Fatal(err)
	}

	if _, err := mrb.Eval("FileUtils.chmod('u+x', " + strconv.Quote(file) + ")"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0740 {
		t.Fatalf("chmod u+x mode = %v, %v; want 0740", info, err)
	}

	if _, err := mrb.Eval("FileUtils.chmod(0600, " + strconv.Quote(file) + ")"); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Eval("FileUtils.chmod('o+w', " + strconv.Quote(file) + ", noop: true)"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("numeric/noop chmod mode = %v, %v; want 0600", info, err)
	}
	if _, err := mrb.Eval("FileUtils.chmod('%', " + strconv.Quote(file) + ")"); err == nil {
		t.Fatal("invalid symbolic mode should raise")
	}

	tree := filepath.Join(root, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(tree, "child")
	if err := os.WriteFile(child, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Eval("FileUtils.chmod_R('o+x', " + strconv.Quote(tree) + ", noop: true)"); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Eval("FileUtils.chmod_R('g+r', " + strconv.Quote(tree) + ")"); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{tree: 0740, child: 0640} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("chmod_R mode for %s = %v, %v; want %04o", path, info, err, want)
		}
	}

	missing := filepath.Join(root, "missing")
	if _, err := mrb.Eval("FileUtils.chmod_R(0600, " + strconv.Quote(missing) + ", force: true)"); err != nil {
		t.Fatalf("chmod_R force should ignore missing paths: %v", err)
	}
}
