package stdlib_test

import (
	"net/http"
	"net/http/httptest"
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

func TestPPCustomPrinterAndCycles(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    require "pp"
    class PPExample
      def pretty_print(q); q.text("custom"); end
    end
    output = ""
    PP.pp(PPExample.new, output)
    recursive = []
    recursive << recursive
    PP.pp(recursive, output)
    output == "custom\n[[...]]\n"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("PP custom formatting and recursive arrays = %v, %v; want true, nil", got, err)
	}
}

func TestPrettyPrintLazyLoadAndWrapping(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`defined?(PrettyPrint).nil?`)
	if err != nil || !got.Bool() {
		t.Fatalf("PrettyPrint before require = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    require "prettyprint"
    output = PrettyPrint.format("", 8) do |printer|
      printer.text("alpha")
      printer.breakable
      printer.text("beta")
    end
    output == "alpha\nbeta"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("PrettyPrint wrapping = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    output = ""
    printer = PrettyPrint.new(output)
    printer.text("a")
    printer.flush
    printer.text("b")
    printer.flush
    output == "ab"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("PrettyPrint repeated flush = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    PrettyPrint.singleline_format("", 1) { |q| q.text("a"); q.breakable; q.text("b") } == "a b"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("PrettyPrint.singleline_format = %v, %v; want true, nil", got, err)
	}
}

func TestOptionParserIntoKeyword(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    require "optparse"
    expected = { verbose: true, require: "optparse" }
    %i[parse parse! order order!].all? do |method|
      options = {}
      parser = OptionParser.new do |opts|
        opts.on("-v", "--[no-]verbose", "Run verbosely")
        opts.on("-r", "--require LIBRARY", "Require a library")
      end
      parser.public_send(method, %w[--verbose --require optparse], into: options)
      options == expected
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("OptionParser into: result = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    options = {}
    parser = OptionParser.new { |opts| opts.on("-v", "--[no-]verbose") }
    arguments = ["--no-verbose", "file.rb"]
    remaining = parser.parse(arguments, into: options)
    options == { verbose: false } && remaining == ["file.rb"] && arguments == ["--no-verbose", "file.rb"]
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("OptionParser negative switch/non-mutating parse = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    parser = OptionParser.new { |opts| opts.on("-r", "--require LIBRARY") }
    begin
      parser.parse!(%w[--require])
      false
    rescue OptionParser::ParseError
      true
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("OptionParser missing argument = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    options = {}
    calls = []
    parser = OptionParser.new { |opts| opts.on("--flag") { |value| calls << value } }
    parser.parse(["--flag"], into: options)
    calls == [true] && options == {flag: [true]}
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("OptionParser block with into = %v, %v; want true, nil", got, err)
	}
}

func TestMatrixCoreOperations(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    require "matrix"
    left = Matrix[[1, 2], [3, 4]]
    right = Matrix.identity(2)
    vector = Vector[5, 6]
    (left + right).to_a == [[2, 2], [3, 5]] &&
      (left * right) == left &&
      (left * vector).to_a == [17, 39] &&
      left.transpose.to_a == [[1, 3], [2, 4]] &&
      left.determinant == -2 &&
      Matrix[[6, 1, 1], [4, -2, 5], [2, 8, 7]].determinant == -306 &&
      Vector[1, 2, 3].dot(Vector[4, 5, 6]) == 32 &&
      Matrix.columns([[1, 3], [2, 4]]) == left
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Matrix operations = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    begin
      Matrix[[1, 2]] + Matrix[[1], [2]]
      false
    rescue Matrix::ErrDimensionMismatch
      true
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Matrix dimension error = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    matrix = Matrix[[1, 2]]
    matrix[5, 0].nil? && matrix.row(5).nil? && matrix.column(5).nil? &&
      Matrix.build(2, 2).is_a?(Enumerator)
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("Matrix indexing and build enumerator = %v, %v; want true, nil", got, err)
	}
}

func TestOpenURIHTTPAndBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("open-uri body"))
	}))
	defer server.Close()

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Eval(`require "open-uri"`); err != nil {
		t.Fatal(err)
	}
	url := strconv.Quote(server.URL + "/resource")
	got, err := mrb.Eval(`URI.open(` + url + `).read == "open-uri body"`)
	if err != nil || !got.Bool() {
		t.Fatalf("URI.open read = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`URI.open(` + url + `) { |io| io.read } == "open-uri body"`)
	if err != nil || !got.Bool() {
		t.Fatalf("URI.open block = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    stream = nil
    begin
      URI.open(` + url + `) { |io| stream = io; raise "block failure" }
    rescue RuntimeError => e
      e.message == "block failure" && stream.closed?
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("URI.open closes on block failure = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    begin
      URI.open(` + strconv.Quote(server.URL+"/missing") + `)
      false
    rescue OpenURI::HTTPError
      true
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("URI.open HTTP error = %v, %v; want true, nil", got, err)
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
