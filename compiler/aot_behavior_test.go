package compiler_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/compiler"
)

// aotRun compiles rubySrc to a native binary (a standalone Go program), runs
// it and returns its standard output.
func aotRun(t *testing.T, rubySrc string, pkgSize int) string {
	t.Helper()
	return aotRunFlags(t, rubySrc, pkgSize, "")
}

// aotRunFlags is aotRun with more flags for go build (GOFLAGS, e.g. -tags=oruby_slim).
func aotRunFlags(t *testing.T, rubySrc string, pkgSize int, goflags string) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	scriptPath := filepath.Join(work, "app.rb")
	if err := os.WriteFile(scriptPath, []byte(rubySrc), 0o644); err != nil {
		t.Fatal(err)
	}
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := compiler.New(compiler.Options{PackageName: "main", Standalone: true, GroupSize: pkgSize, ModulePath: "aotbehavior"})
	mainDir := filepath.Join(work, "main")
	if _, err := comp.CompilePackageDir(mrb, scriptPath, nil, nil, mainDir); err != nil {
		t.Fatalf("CompilePackageDir: %v", err)
	}
	mod := "module aotbehavior\n\ngo 1.27.0\n\nrequire github.com/oruby/oruby v0.0.0\n\nreplace github.com/oruby/oruby => " + root + "\n"
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(work, "go.sum"), sum, 0o644)
	}
	bin := filepath.Join(work, "app_bin")
	build := exec.Command("go", "build", "-o", bin, "./main")
	build.Dir = work
	build.Env = append(os.Environ(), "CGO_ENABLED=1", "GOFLAGS=-mod=mod "+goflags, "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("compiled binary failed: %v\n%s", err, out)
	}
	return string(out)
}

// Behavior compiled code must share with the interpreter: each line is what MRI prints.
func TestAOTDynamicDispatchKeywordsAndVisibility(t *testing.T) {
	src := `
def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}"; end
class K
  def self.rb(a, *r, **o) = [a, r, o]
  def priv; :p; end
  private :priv
end
eval("def K.vm(*a, **o) = [a, o]")
t("send kw to compiled") { K.send(:rb, 1, 2, x: 3) }
t("send kw to vm method") { K.send(:vm, 1, x: 2) }
t("__send__ kw") { K.__send__(:vm, 1, x: 2) }
t("public_send kw") { K.public_send(:vm, 1, x: 2) }
t("send kw splat") { h = {x: 2}; K.send(:vm, 1, **h) }
t("send no kw") { K.send(:vm, 1, 2) }
t("send private ok") { K.new.send(:priv) }
t("public_send private") { K.new.public_send(:priv) }
t("send to missing") { K.send(:nope, 1) }
t("send to an alias") { [1, 2, 3].send(:find_index, 2) }
t("File.write via send") { f = "/tmp/aot_behavior_kw.txt"; File.send(:write, f, "abc", mode: "w") }
`
	want := strings.Join([]string{
		`send kw to compiled: [1, [2], {x: 3}]`,
		`send kw to vm method: [[1], {x: 2}]`,
		`__send__ kw: [[1], {x: 2}]`,
		`public_send kw: [[1], {x: 2}]`,
		`send kw splat: [[1], {x: 2}]`,
		`send no kw: [[1, 2], {}]`,
		`send private ok: :p`,
		`public_send private: RAISED NoMethodError`,
		`send to missing: RAISED NoMethodError`,
		`send to an alias: 1`,
		`File.write via send: 3`,
	}, "\n")
	got := strings.TrimSpace(aotRun(t, src, 0))
	if got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Method#arity, #parameters and the argument checks of compiled methods, procs
// and lambdas are those of bytecode ones.
func TestAOTSignatures(t *testing.T) {
	src := `
def m0; end
def m1(a, b = 1, *r, c, k:, o: 2, **kr, &blk); end
def m2(a, k: 1); end
def m3(a, k:); end
def m4(**nil); end
[:m0, :m1, :m2, :m3, :m4].each { |n| puts "#{n}: #{method(n).parameters.inspect} #{method(n).arity}" }
pr = proc { |x, y| }
l = ->(a, b = 1) {}
puts "#{pr.parameters.inspect} #{pr.arity}"
puts "#{l.parameters.inspect} #{l.arity}"
begin; l.call; rescue ArgumentError => e; puts e.message; end
begin; m3(1); rescue ArgumentError => e; puts e.message; end
begin; m3(1, k: 1, z: 2); rescue ArgumentError => e; puts e.message; end
begin; m0(1); rescue ArgumentError => e; puts e.message; end
`
	want := strings.Join([]string{
		`m0: [] 0`,
		`m1: [[:req, :a], [:opt, :b], [:rest, :r], [:req, :c], [:keyreq, :k], [:key, :o], [:keyrest, :kr], [:block, :blk]] -4`,
		`m2: [[:req, :a], [:key, :k]] -2`,
		`m3: [[:req, :a], [:keyreq, :k]] 2`,
		`m4: [[:nokey]] -1`, // mruby's arity of a method with **nil (MRI: 0), as the interpreter's
		`[[:opt, :x], [:opt, :y]] 2`,
		`[[:req, :a], [:opt, :b]] -2`,
		`wrong number of arguments (given 0, expected 1..2)`,
		`missing keyword: :k`,
		`unknown keyword: :z`,
		`wrong number of arguments (given 1, expected 0)`,
	}, "\n")
	got := strings.TrimSpace(aotRun(t, src, 0))
	if got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// aotRunProject is aotRun for a program of several files (name -> source):
// app.rb is the entry, loadPaths the directories of -I, extra are the files added to the bundle without a
// require naming them, and the directories in remove are deleted before the
// binary runs, so it has nothing but what it carries.
func aotRunProject(t *testing.T, files map[string]string, loadPaths, extra, remove []string) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	proj := filepath.Join(work, "proj")
	for name, src := range files {
		path := filepath.Join(proj, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var extraPaths, lps []string
	for _, e := range extra {
		extraPaths = append(extraPaths, filepath.Join(proj, e))
	}
	for _, l := range loadPaths {
		lps = append(lps, filepath.Join(proj, l))
	}
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	comp := compiler.New(compiler.Options{PackageName: "main", Standalone: true, ModulePath: "aotbehavior"})
	if _, err := comp.CompilePackageDir(mrb, filepath.Join(proj, "app.rb"), extraPaths, lps, filepath.Join(work, "main")); err != nil {
		t.Fatalf("CompilePackageDir: %v", err)
	}
	mod := "module aotbehavior\n\ngo 1.27.0\n\nrequire github.com/oruby/oruby v0.0.0\n\nreplace github.com/oruby/oruby => " + root + "\n"
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(work, "go.sum"), sum, 0o644)
	}
	bin := filepath.Join(work, "app_bin")
	build := exec.Command("go", "build", "-o", bin, "./main")
	build.Dir = work
	build.Env = append(os.Environ(), "CGO_ENABLED=1", "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	for _, d := range remove {
		if err := os.RemoveAll(filepath.Join(proj, d)); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin)
	cmd.Dir = proj
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compiled binary failed: %v\n%s", err, out)
	}
	return string(out)
}

// A compiled program finds its files without them on disk: require through a
// directory added to $LOAD_PATH at run time, autoload, and a file added to the
// bundle by hand. Its last value is not printed.
func TestAOTLoader(t *testing.T) {
	files := map[string]string{
		"app.rb": `
$:.unshift File.join(__dir__, "lib")
require 'foo'
autoload :Bar, "bar"
puts Foo.hi, Bar.hi
$:.unshift File.join(__dir__, "dyn")
puts require('baz'), Baz.hi, require('baz')
:not_printed
`,
		"lib/foo.rb": "module Foo; def self.hi = :foo; end\n",
		"lib/bar.rb": "module Bar; def self.hi = :bar; end\n",
		"dyn/baz.rb": "module Baz; def self.hi = :baz; end\n",
	}
	got := strings.TrimSpace(aotRunProject(t, files, []string{"lib"}, []string{"dyn/baz.rb"}, []string{"lib", "dyn"}))
	want := strings.Join([]string{"foo", "bar", "true", "baz", "false"}, "\n")
	if got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A file that needs its own frame (eval with locals, binding, using) stays
// bytecode and runs in the VM; the others are compiled, and they work together.
func TestAOTVMTier(t *testing.T) {
	files := map[string]string{
		"app.rb": `
require_relative 'plain'
require_relative 'framey'
puts Plain.twice(21)
puts Framey.render("x")
puts Framey.refined
`,
		"plain.rb": "module Plain; def self.twice(n) = n * 2; end\n",
		"framey.rb": `
module Framey
  def self.render(s)
    v = 40
    eval("v + 2", binding).to_s + s
  end
  module R; refine(String) { def shout = upcase + "!" }; end
  using R
  def self.refined = "a".shout
end
`,
	}
	got := strings.TrimSpace(aotRunProject(t, files, nil, nil, nil))
	if want := "42\n42x\nA!"; got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}

	// only framey.rb is bytecode
	root, _ := filepath.Abs("..")
	_ = root
	work := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(work, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	comp := compiler.New(compiler.Options{PackageName: "main", Standalone: true})
	if _, err := comp.CompilePackageDir(mrb, filepath.Join(work, "app.rb"), nil, nil, filepath.Join(work, "out")); err != nil {
		t.Fatal(err)
	}
	vm, compiled := 0, 0
	entries, _ := os.ReadDir(filepath.Join(work, "out"))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "aot_mod_") {
			continue
		}
		src, _ := os.ReadFile(filepath.Join(work, "out", e.Name()))
		switch {
		case strings.Contains(string(src), "AotRunVM("):
			vm++
		case strings.Contains(string(src), "oruby-module: plain.rb"):
			compiled++
		}
	}
	if vm != 1 || compiled != 1 {
		t.Fatalf("want framey.rb as bytecode (1) and plain.rb compiled (1), got %d and %d", vm, compiled)
	}
}

// super inside a method defined by define_method, a block with no method
// context of its own: its caller's frame is the method.
func TestAOTSuperInDefineMethod(t *testing.T) {
	src := `
def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}"; end
class A; def hi(x = 1) = [:a, x]; def kw(k: 0) = [:akw, k]; end
class B < A
  define_method(:hi) { |x = 2| [:b, super(x)] }
  define_method(:kw) { |k: 5| [:bkw, super(k: k)] }
end
class C < A; define_method(:hi) { |*a| super(*a) }; end
module M; def hi(x = 1) = [:m, super(x)]; end
class F < A; include M; define_method(:hi) { |x = 3| [:f, super(x)] }; end
t("super(x)") { B.new.hi }
t("super(k:)") { B.new.kw }
t("super(*a)") { C.new.hi(7) }
t("through a module") { F.new.hi }
t("twice") { b = B.new; [b.hi, b.hi(4)] }
t("singleton") { o = A.new; o.define_singleton_method(:hi) { |x = 5| [:s, super(x)] }; o.hi }
`
	want := strings.Join([]string{
		`super(x): [:b, [:a, 2]]`,
		`super(k:): [:bkw, [:akw, 5]]`,
		`super(*a): [:a, 7]`,
		`through a module: [:f, [:m, [:a, 3]]]`,
		`twice: [[:b, [:a, 2]], [:b, [:a, 4]]]`,
		`singleton: [:s, [:a, 5]]`,
	}, "\n")
	if got := strings.TrimSpace(aotRun(t, src, 0)); got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Semantics compiled code must keep: defined? on scoped constants, a call that
// breaks a method's visibility reaching method_missing, Proc#== telling two
// compiled methods and blocks apart, and the length of a non-UTF-8 string.
func TestAOTSemantics(t *testing.T) {
	src := `
def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}"; end
module Mo; X = 1; module In; Y = 2; end; end
t("defined? A::B") { [defined?(Mo::X), defined?(Mo::In::Y), defined?(Mo::Nope), defined?(::String), defined?(File::RDONLY)] }
class Ghost
  def method_missing(n, *a) = [:mm, n, a]
  def respond_to_missing?(*) = true
  private def hidden(x) = :hidden
  protected def guarded = :guarded
end
t("private -> method_missing") { Ghost.new.hidden(1) }
t("protected -> method_missing") { Ghost.new.guarded }
def m1; end
def m2; end
t("Method#== of two methods") { [method(:m1) == method(:m1), method(:m1) == method(:m2)] }
t("Proc#== of two blocks") { a = proc { 1 }; b = proc { 1 }; [a == a, a == b, a.dup == a] }
t("UTF-16 length") { ["abあ".encode("UTF-16LE").length, "abあ".encode("UTF-32LE").length, "abあ".length] }
`
	want := strings.Join([]string{
		`defined? A::B: ["constant", "constant", nil, "constant", "constant"]`,
		`private -> method_missing: [:mm, :hidden, [1]]`,
		`protected -> method_missing: [:mm, :guarded, []]`,
		`Method#== of two methods: [true, false]`,
		`Proc#== of two blocks: [true, false, true]`,
		`UTF-16 length: [3, 3, 3]`,
	}, "\n")
	if got := strings.TrimSpace(aotRun(t, src, 0)); got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Fibers of a compiled program run on goroutines: Fiber.yield crosses the Go
// frames of compiled blocks and methods, and Enumerator#next works.
func TestAOTFibers(t *testing.T) {
	src := `
def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}: #{e.message}"; end
t("basic") { f = Fiber.new { |x| y = Fiber.yield(x + 1); z = Fiber.yield(y * 2); :done }; [f.resume(1), f.resume(10), f.resume(0), f.alive?] }
t("yield in a block") { f = Fiber.new { [1, 2, 3].each { |x| Fiber.yield x }; :end }; [f.resume, f.resume, f.resume, f.resume] }
t("yield in a method") { def gen; 3.times { |i| Fiber.yield i * 10 }; :fin; end; f = Fiber.new { gen }; [f.resume, f.resume, f.resume, f.resume] }
t("Enumerator#next") { e = [10, 20, 30].each; [e.next, e.next, e.next, (e.next rescue :stop)] }
t("Enumerator.new") { e = Enumerator.new { |y| y << 1; y << 2 }; [e.next, e.next, e.take(2)] }
t("lazy") { (1..Float::INFINITY).lazy.map { |x| x * 2 }.select { |x| x % 3 == 0 }.first(3) }
t("exception") { f = Fiber.new { raise ArgumentError, "boom" }; begin; f.resume; rescue => e; [e.class, e.message, f.alive?]; end }
t("terminated") { f = Fiber.new { 1 }; f.resume; begin; f.resume; rescue FiberError => e; e.message; end }
t("root yield") { begin; Fiber.yield; rescue FiberError => e; e.message; end }
t("nested") { a = Fiber.new { b = Fiber.new { Fiber.yield :in1; :in2 }; [b.resume, Fiber.yield(:out1), b.resume] }; [a.resume, a.resume(:x)] }
t("raise into") { f = Fiber.new { begin; Fiber.yield 1; rescue => e; [:caught, e.message]; end }; f.resume; f.raise("injected") }
t("kill runs ensure") { f = Fiber.new { begin; Fiber.yield 1; ensure; $ens = :ran; end }; f.resume; f.kill; [f.alive?, $ens] }
t("200 fibers") { fs = 200.times.map { |i| Fiber.new { Fiber.yield i; i * 2 } }; fs.map(&:resume).sum + fs.map(&:resume).sum }
t("in a thread") { Thread.new { f = Fiber.new { Fiber.yield 1; 2 }; [f.resume, f.resume] }.value }
t("resume itself") { f = Fiber.new { f.resume }; begin; f.resume; rescue FiberError => e; e.message; end }
t("resume a resuming fiber") { root = Fiber.current; f = Fiber.new { root.resume }; begin; f.resume; rescue FiberError => e; e.message; end }
t("transfer") { begin; Fiber.new { 1 }.transfer; rescue FiberError => e; e.message[0, 31]; end }
t("storage") { Fiber[:k] = 1; [Fiber.new { Fiber[:k] }.resume, Fiber.new(storage: nil) { Fiber[:k] }.resume, Fiber.new(storage: {k: 2}) { Fiber[:k] }.resume] }
t("root status") { Fiber.current.inspect.end_with?("(resumed)>") }
`
	want := strings.Join([]string{
		`basic: [2, 20, :done, false]`,
		`yield in a block: [1, 2, 3, :end]`,
		`yield in a method: [0, 10, 20, :fin]`,
		`Enumerator#next: [10, 20, 30, :stop]`,
		`Enumerator.new: [1, 2, [1, 2]]`,
		`lazy: [6, 12, 18]`,
		`exception: [ArgumentError, "boom", false]`,
		`terminated: "attempt to resume a terminated fiber"`,
		`root yield: "attempt to yield on a not resumed fiber"`,
		`nested: [:out1, [:in1, :x, :in2]]`,
		`raise into: [:caught, "injected"]`,
		`kill runs ensure: [false, :ran]`,
		`200 fibers: 59700`,
		`in a thread: [1, 2]`,
		`resume itself: "attempt to resume the current fiber"`,
		`resume a resuming fiber: "attempt to resume a resuming fiber"`,
		`transfer: "Fiber#transfer is not supported"`,
		`storage: [1, nil, 2]`,
		`root status: true`,
	}, "\n")
	if got := strings.TrimSpace(aotRun(t, src, 0)); got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A define_method block becomes a method body: its `return` ends the method,
// not the frame the block was written in (a class body or the script itself).
func TestAOTDefineMethodReturn(t *testing.T) {
	src := `
def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}"; end
k = Class.new do
  define_method(:a) { |x| return x * 2 }
  define_method(:b) { |x, y = 1| return x, y }
  define_method(:c) { |x| [1, 2, 3].each { |v| return v if v == x }; :none }
  define_method(:d) { |x| if x > 0 then return :pos end; :nonpos }
  define_method(:e) { return }
  define_method(:f) { |x| begin; return :from_begin; ensure; $ens = x; end }
end
o = k.new
o.define_singleton_method(:g) { |x| return x + 100 }
t("value") { o.a(21) }
t("several values") { o.b(1) }
t("from a nested block") { o.c(2) }
t("conditional") { [o.d(1), o.d(-1)] }
t("bare") { o.e }
t("with ensure") { [o.f(7), $ens] }
t("singleton") { o.g(1) }
puts "the script goes on"
`
	want := strings.Join([]string{
		`value: 42`,
		`several values: [1, 1]`,
		`from a nested block: 2`,
		`conditional: [:pos, :nonpos]`,
		`bare: nil`,
		`with ensure: [:from_begin, 7]`,
		`singleton: 101`,
		`the script goes on`,
	}, "\n")
	if got := strings.TrimSpace(aotRun(t, src, 0)); got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A compiled program ends as oruby does: at_exit handlers run, SystemExit
// gives the status, and SIGTERM raises SignalException (SIGINT: Interrupt).
func TestAOTProgramEnd(t *testing.T) {
	src := `
at_exit { puts "at_exit 1" }
at_exit { puts "at_exit 2" }
begin
  Process.kill(:TERM, Process.pid)
  sleep 1
rescue SignalException => e
  puts "signal: #{e.signm}"
end
begin
  Process.kill(:INT, Process.pid)
  sleep 1
rescue Interrupt
  puts "interrupt"
end
puts "end of script"
`
	want := strings.Join([]string{"signal: SIGTERM", "interrupt", "end of script", "at_exit 2", "at_exit 1"}, "\n")
	if got := strings.TrimSpace(aotRun(t, src, 0)); got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A slim binary (oruby_slim: without the web gems) runs a program that does
// not need them, and a require of a web gem is a LoadError, not a crash.
func TestAOTSlimBuild(t *testing.T) {
	src := `
require "json"
begin
  require "socket"
  puts "socket"
rescue LoadError
  puts "no socket"
end
puts JSON.generate({a: 1})
`
	got := strings.TrimSpace(aotRunFlags(t, src, 0, "-tags=oruby_slim"))
	if want := "no socket\n{\"a\":1}"; got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A self-recursive method is specialized to unboxed integers by speculation
// (the return type is assumed, then checked); every case the speculation
// cannot cover falls back to the generic code with the same answer.
func TestAOTSpeculatedRecursion(t *testing.T) {
	src := `
def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}"; end
def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)
def fact(n) = n <= 1 ? 1 : n * fact(n - 1)
def down(n) = n == 0 ? nil : down(n - 1)
def sum_to(n) = n == 0 ? 0 : n + sum_to(n - 1)
def maybe(n) = n > 3 ? "big" : maybe(n + 1)
t("fib") { fib(20) }
t("fact past 64 bits") { fact(25) }
t("float argument") { fib(5.5) }
t("string argument") { begin; fib("a"); rescue => e; e.class; end }
t("nil result") { down(3) }
t("sum") { sum_to(1000) }
t("other return type") { maybe(0) }
class Integer; def fib_check = fib(self); end
t("redefined +") { class Integer; alias_method :orig_plus, :+; def +(o) = orig_plus(o).orig_plus(1); end; r = fib(5); class Integer; alias_method :+, :orig_plus; end; r }
t("after restoring +") { fib(10) }
`
	want := strings.Join([]string{
		`fib: 6765`,
		`fact past 64 bits: 15511210043330985984000000`,
		`float argument: 9.0`,
		`string argument: ArgumentError`,
		`nil result: nil`,
		`sum: 500500`,
		`other return type: "big"`,
		`redefined +: 12`,
		`after restoring +: 55`,
	}, "\n")
	got := strings.TrimSpace(aotRun(t, src, 0))
	if got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Pure integer loops run unboxed (compiler/intloop.go) and give the answers
// of MRI 4.1: overflow to bignum, break and next, if/else and elsif inside the
// loop, variables assigned in the loop, floats, a redefined operator, a
// Timeout that interrupts the loop, and the modulo and shift edges. The
// expected output is MRI 4.1.0's own.
func TestAOTIntegerLoops(t *testing.T) {
	src := `require "timeout"
def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}"; end
t("sum") { i = 0; s = 0; while i < 1000; s += i; i += 1; end; [s, i] }
t("mul overflow to bignum") { s = 1; i = 0; while i < 100; s = s * 3; i += 1; end; s }
t("add overflow to bignum") { s = 1; i = 0; while i < 80; s += s; i += 1; end; s }
t("shift") { s = 1; i = 0; while i < 70; s = s << 1; i += 1; end; s }
t("shift right neg") { s = -1000; i = 0; while i < 5; s = s >> 1; i += 1; end; s }
t("mod neg") { s = -17; r = []; i = 0; while i < 4; r << (s % 5); s -= 3; i += 1; end; r }
t("mod loop sum") { i = 0; s = 0; while i < 100; s += i % 7; i += 1; end; s }
t("mod by zero") { i = 0; s = 5; while i < 3; s = s % (2 - i); i += 1; end; s }
t("bit ops") { i = 0; s = 0; while i < 64; s ^= (i & 5) | (i << 1); i += 1; end; s }
t("break") { i = 0; s = 0; while true; s += i; i += 1; break if i > 10; end; [s, i] }
t("next") { i = 0; s = 0; while i < 20; i += 1; next if i % 3 == 0; s += i; end; [s, i] }
t("while with condition var") { i = 0; n = 50; s = 0; while i < n; s += n - i; i += 1; end; s }
t("carried read after") { i = 0; s = 0; while i < 10; s += 2; i += 1; end; s * i }
t("assigned in loop, read after") { i = 0; last = nil; while i < 10; last = i * 2; i += 1; end; last }
t("conditionally assigned") { i = 0; hit = nil; while i < 10; hit = i if i == 4; i += 1; end; hit }
t("nested") { s = 0; i = 0; while i < 30; j = 0; while j < 30; s += i * j; j += 1; end; i += 1; end; s }
t("float counter") { i = 0.0; s = 0.0; while i < 10.0; s += i; i += 0.5; end; [s, i] }
t("mixed float") { i = 0; s = 0.0; while i < 10; s += i; i += 1; end; s }
t("nil operand") { i = 0; s = nil; begin; while i < 3; s += 1; i += 1; end; rescue NoMethodError; :nomethod; end }
t("comparison chain") { i = 0; c = 0; while i < 100; c += 1 if i > 10 && i <= 50; i += 1; end; c }
t("neq") { i = 0; c = 0; while i != 25; c += 2; i += 1; end; [c, i] }
t("negative literals") { i = 100; s = 0; while i > -100; s += i; i -= 7; end; [s, i] }
t("large literals") { i = 4_000_000_000; s = 0; while i < 4_000_000_005; s += i; i += 1; end; s }
t("timeout interrupts a typed loop") { begin; Timeout.timeout(0.3) { i = 0; while true; i += 1; end }; rescue Timeout::Error; :timed_out; end }
t("if else in loop") { i = 0; a = 0; b = 0; while i < 40; if i % 2 == 0 then a += i else b += i end; i += 1; end; [a, b] }
t("elsif chain") { i = 0; r = [0, 0, 0]; while i < 30; if i % 3 == 0 then r[0] += 1 elsif i % 3 == 1 then r[1] += 1 else r[2] += 1 end; i += 1; end; r }
t("break in if else") { i = 0; s = 0; while i < 100; if i > 20 then break else s += i end; i += 1; end; [s, i] }
t("next in else") { i = 0; s = 0; while i < 20; i += 1; if i < 5 then s += 100 else next end; s += 1; end; [s, i] }
t("unless/until") { i = 0; s = 0; until i >= 15; s += i unless i % 4 == 0; i += 1; end; s }
t("ternary in loop") { i = 0; s = 0; while i < 12; s += i.odd? ? 1 : 2; i += 1; end; s }
t("break out of nested") { s = 0; i = 0; while i < 10; j = 0; while j < 10; break if j > i; s += 1; j += 1; end; i += 1; end; s }
t("loop result value") { i = 0; r = while i < 3; i += 1; end; [r, i] }
t("break with value") { i = 0; r = while i < 10; i += 1; break i * 100 if i == 4; end; r }
t("user class defines %") { c = Class.new { def %(x) = :user }; i = 0; s = 0; while i < 50; s += i % 7; i += 1; end; [s, c.new % 1] }
t("redefine Integer#%") { class Integer; alias_method :old_mod, :%; def %(o) = old_mod(o) + 1000; end; i = 0; s = 0; while i < 5; s += i % 3; i += 1; end; class Integer; alias_method :%, :old_mod; end; s }
t("restored %") { i = 0; s = 0; while i < 5; s += i % 3; i += 1; end; s }
t("redefine Integer#<<") { class Integer; alias_method :old_shl, :<<; def <<(o) = old_shl(o) + 1; end; i = 0; s = 0; while i < 4; s += 1 << i; i += 1; end; class Integer; alias_method :<<, :old_shl; end; s }
t("prepend Integer &") { Integer.prepend(Module.new { def &(o) = 42 }); i = 0; s = 0; while i < 4; s += i & 1; i += 1; end; s }
t("redefined +") { class Integer; alias_method :old_plus, :+; def +(o) = old_plus(o).old_plus(1); end; i = 0; s = 0; while i < 5; s += 1; i = i.old_plus(1); end; class Integer; alias_method :+, :old_plus; end; s }
t("after restore") { i = 0; s = 0; while i < 5; s += 1; i += 1; end; s }
t("method loop") { def m(n); i = 0; s = 0; while i < n; s += i * i; i += 1; end; s; end; [m(100), m(0), m(3)] }
t("block loop") { r = []; 3.times { |k| i = 0; s = 0; while i < k * 10; s += i; i += 1; end; r << s }; r }
t("max int") { i = 9223372036854775800; while i < 9223372036854775810; i += 1; end; i }
t("min int") { i = -9223372036854775800; while i > -9223372036854775810; i -= 1; end; i }
nil
`
	want := `sum: [499500, 1000]
mul overflow to bignum: 515377520732011331036461129765621272702107522001
add overflow to bignum: 1208925819614629174706176
shift: 1180591620717411303424
shift right neg: -32
mod neg: [3, 0, 2, 4]
mod loop sum: 295
mod by zero: RAISED ZeroDivisionError
bit ops: 0
break: [55, 11]
next: [147, 20]
while with condition var: 1275
carried read after: 200
assigned in loop, read after: 18
conditionally assigned: 4
nested: 189225
float counter: [95.0, 10.0]
mixed float: 45.0
nil operand: :nomethod
comparison chain: 40
neq: [50, 25]
negative literals: [58, -103]
large literals: 20000000010
timeout interrupts a typed loop: :timed_out
if else in loop: [380, 400]
elsif chain: [10, 10, 10]
break in if else: [210, 21]
next in else: [404, 20]
unless/until: 81
ternary in loop: 18
break out of nested: 55
loop result value: [nil, 3]
break with value: 400
user class defines %: [147, :user]
redefine Integer#%: 5004
restored %: 4
redefine Integer#<<: 19
prepend Integer &: 168
redefined +: 10
after restore: 5
method loop: [328350, 0, 5]
block loop: [0, 45, 190]
max int: 9223372036854775810
min int: -9223372036854775810`
	got := strings.TrimSpace(aotRun(t, src, 0))
	if got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// [] and []= on Array and Hash are guarded by mruby's own idx_class slots: one
// pointer comparison that a subclass, a singleton, an override, an alias, a
// prepended module or a restored method all answer as MRI does. The expected
// output is MRI 4.1.0's own.
func TestAOTIndexOperators(t *testing.T) {
	src := `def t(name); r = yield; puts "#{name}: #{r.inspect}"; rescue Exception => e; puts "#{name}: RAISED #{e.class}"; end
t("array") { a = [10, 20, 30]; [a[0], a[2], a[3], a[-1], a[-4]] }
t("array set") { a = [1, 2]; a[0] = 9; a[5] = 7; a }
t("hash") { h = {a: 1, "b" => 2, 3 => 4}; [h[:a], h["b"], h[3], h[:zz]] }
t("hash default") { h = Hash.new(0); h[:x] += 1; h[:x] += 1; [h[:x], h[:y]] }
t("hash default proc") { h = Hash.new { |hh, k| hh[k] = k.to_s * 2 }; [h[:ab], h.size, h[:ab]] }
t("hash set") { h = {}; h[:a] = 1; h["s"] = 2; h[[1, 2]] = 3; [h, h[[1, 2]]] }
t("string index") { s = "hello"; [s[1], s[1, 3], s[1..2], s[-1], s["ell"], s[9]] }
t("frozen array set") { a = [1].freeze; begin; a[0] = 2; rescue => e; e.class; end }
t("frozen hash set") { h = {a: 1}.freeze; begin; h[:b] = 2; rescue => e; e.class; end }
t("custom class []") { c = Class.new { def [](i) = i * 2; def []=(i, v); @last = [i, v]; end; attr_reader :last }; o = c.new; o[4] = 5; [o[21], o.last] }
t("hash subclass []") { c = Class.new(Hash) { def [](k) = "sub:#{super}" }; h = c.new; h[:a] = 1; [h[:a], h[:b]] }
t("array subclass []") { c = Class.new(Array) { def [](i) = "sub#{i}" }; [c.new([1, 2])[1]] }
t("hash subclass []=") { c = Class.new(Hash) { def []=(k, v); super(k, v * 10); end }; h = c.new; h[:a] = 1; h }
t("singleton []") { h = {a: 1}; def h.[](k) = :single; [h[:a], {a: 1}[:a]] }
t("redefine Hash#[]") { class Hash; alias_method :old_aref, :[]; def [](k) = [:redef, old_aref(k)]; end; r = {a: 1}[:a]; class Hash; alias_method :[], :old_aref; end; r }
t("after restoring Hash#[]") { {a: 1}[:a] }
t("redefine Array#[]") { class Array; alias_method :old_aref, :[]; def [](i) = [:redef, old_aref(i)]; end; r = [5, 6][1]; class Array; alias_method :[], :old_aref; end; r }
t("redefine Array#[]=") { class Array; alias_method :old_aset, :[]=; def []=(i, v); old_aset(i, v + 100); end; end; a = [0, 0]; a[1] = 1; class Array; alias_method :[]=, :old_aset; end; a }
t("redefine Hash#[]=") { class Hash; alias_method :old_hset, :[]=; def []=(k, v); old_hset(k, v + 100); end; end; h = {}; h[:a] = 1; class Hash; alias_method :[]=, :old_hset; end; h }
t("after restoring []=") { a = [0, 0]; a[1] = 1; h = {}; h[:a] = 1; [a, h] }
t("prepend on Array") { m = Module.new { def [](i) = [:prepended, super] }; Array.prepend(m); r = [1, 2][0]; r }
t("hash key with custom hash") { k = Class.new { attr_reader :v; def initialize(v); @v = v; end; def hash = @v.hash; def eql?(o) = v == o.v }; h = {k.new(1) => :one}; [h[k.new(1)], h[k.new(2)]] }
t("hash key raising") { k = Class.new { def hash = raise("boom") }; begin; {}[k.new]; rescue => e; e.message; end }
t("many lookups") { h = {}; 1000.times { |i| h[i] = i * i }; s = 0; 1000.times { |i| s += h[i] }; s }
nil
`
	want := `array: [10, 30, nil, 30, nil]
array set: [9, 2, nil, nil, nil, 7]
hash: [1, 2, 4, nil]
hash default: [2, 0]
hash default proc: ["abab", 1, "abab"]
hash set: [{a: 1, "s" => 2, [1, 2] => 3}, 3]
string index: ["e", "ell", "el", "o", "ell", nil]
frozen array set: FrozenError
frozen hash set: FrozenError
custom class []: [42, [4, 5]]
hash subclass []: ["sub:1", "sub:"]
array subclass []: ["sub1"]
hash subclass []=: {a: 10}
singleton []: [:single, 1]
redefine Hash#[]: [:redef, 1]
after restoring Hash#[]: 1
redefine Array#[]: [:redef, 6]
redefine Array#[]=: [0, 101]
redefine Hash#[]=: {a: 101}
after restoring []=: [[0, 1], {a: 1}]
prepend on Array: [:prepended, 1]
hash key with custom hash: [:one, nil]
hash key raising: nil
many lookups: 332833500`
	got := strings.TrimSpace(aotRun(t, src, 0))
	if got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAOTInlinedBlockLoopsAndIC(t *testing.T) {
	src := `
def ret_in_block(arr)
  arr.each { |x| return x * 10 if x == 3 }
  :not_found
end

def ret_in_times(n)
  n.times { |i| return i + 100 if i == 2 }
  :none
end

s_next = 0
1.upto(6) { |i| next if i.even?; s_next += i }
puts "next: #{s_next}"

b1 = [1, 2, 3, 4].each { |x| break x * 20 if x == 3 }
b2 = [1, 2, 3, 4].map { |x| break :stopped if x == 2; x + 1 }
b3 = [1, 2, 3, 4].select { |x| break :sel_stop if x == 3; x.odd? }
b4 = 5.times { |i| break i + 50 if i == 3 }
b5 = 10.downto(1) { |i| break i * 2 if i == 7 }
b6 = 1.step(10, 2) { |i| break i + 1 if i == 5 }
b7 = (1..5).each { |i| break i * 3 if i == 4 }
puts "breaks: #{[b1, b2, b3, b4, b5, b6, b7].inspect}"

puts "returns: #{[ret_in_block([1, 2, 3, 4]), ret_in_block([1, 2]), ret_in_times(5), ret_in_times(1)].inspect}"

exc_msg = begin
  [1, 2, 3].map { |x| raise "boom_#{x}" if x == 2; x }
rescue => e
  e.message
end
puts "exc: #{exc_msg}"

mut = [1, 2, 3]
seen = []
mut.each { |x| seen << x; mut << x + 10 if x < 3 }
puts "mut_each: #{seen.inspect}"

class Integer
  alias_method :orig_times, :times
  def times(&blk); :patched_times; end
end
puts "patched_times: #{3.times { }.inspect}"
class Integer
  alias_method :times, :orig_times
end
t_restored = 0
3.times { |i| t_restored += i }
puts "restored_times: #{t_restored}"

class Array
  alias_method :orig_map, :map
  def map(&blk); [:patched_map]; end
end
puts "patched_map: #{[1, 2].map { |x| x }.inspect}"
class Array
  alias_method :map, :orig_map
end
puts "restored_map: #{[1, 2].map { |x| x * 2 }.inspect}"

class Box
  attr_accessor :v
  def initialize(v); @v = v; end
  def add_extra; @extra = 99; end
end
SP = Struct.new(:a, :b)
DP = Data.define(:a, :b)

box = Box.new(10)
sp = SP.new(1, 2)
dp = DP.new(3, 4)
sum_ic = 0
5.times do |i|
  box.v = i
  box.add_extra if i == 2
  sum_ic += box.v + sp.a + dp.b
end
puts "ic_loop: #{sum_ic}"

fbox = Box.new(1).freeze
frozen_err = begin
  fbox.v = 2
  :ok
rescue => e
  e.class
end
puts "frozen_writer: #{frozen_err}"

Box.define_method(:v) { 1000 }
SP.define_method(:a) { 2000 }
DP.define_method(:b) { 3000 }
puts "ic_patched: #{box.v + sp.a + dp.b}"

module BoxRef
  refine Box do
    def v; 777; end
  end
end
using BoxRef
puts "ic_refined: #{box.v}"
`
	want := strings.Join([]string{
		`next: 9`,
		`breaks: [60, :stopped, :sel_stop, 53, 14, 6, 12]`,
		`returns: [30, :not_found, 102, :none]`,
		`exc: boom_2`,
		`mut_each: [1, 2, 3, 11, 12]`,
		`patched_times: :patched_times`,
		`restored_times: 3`,
		`patched_map: [:patched_map]`,
		`restored_map: [2, 4]`,
		`ic_loop: 35`,
		`frozen_writer: FrozenError`,
		`ic_patched: 6000`,
		`ic_refined: 777`,
	}, "\n")
	if got := strings.TrimSpace(aotRun(t, src, 0)); got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAOTTypedBlockLoopsAndDeopt(t *testing.T) {
	src := `
require 'timeout'

#: (Array[Integer], Integer) -> Integer
def scale_arr!(a, k)
  n = a.length
  i = 0
  s = 0
  while i < n
    v = a[i] * k
    a[i] = v
    s += v
    i += 1
  end
  s
end

ia = [1, 2, 3, 4]
puts "scale_arr!: #{scale_arr!(ia, 3)} #{ia.inspect}"

big_base = 9223372036854775805
m_ov = [1, 2, 5, 10].map { |x| big_base + x }
puts "map_ov: #{m_ov.inspect}"

s_ov = [big_base, 10, 20].sum { |x| x + 1 }
puts "sum_ov: #{s_ov}"

inj_ov = [10, 20].inject(big_base) { |acc, x| acc + x }
puts "inject_ov: #{inj_ov}"

ts_ov = big_base
4.times { |i| ts_ov += i }
puts "times_ov: #{ts_ov}"

up_ov = big_base
1.upto(4) { |i| up_ov += i }
puts "upto_ov: #{up_ov}"

het_map = [1, 2, "ab", 4].map { |x| x * 2 }
puts "het_map: #{het_map.inspect}"

het_sum = [1, 2, 3.5, 4].sum { |x| x + 1 }
puts "het_sum: #{het_sum}"

class Box
  attr_accessor :v
  def initialize(v); @v = v; end
end
bx = Box.new(10)
acc_sum = 0
4.times do |i|
  bx.v = 1.5 if i == 2
  acc_sum += bx.v + i
end
puts "acc_deopt: #{acc_sum}"

timed_out = begin
  Timeout.timeout(0.02) do
    s = 0
    1_000_000_000.times { |i| s += i }
    s
  end
rescue Timeout::Error
  :timed_out
end
puts "timeout_times: #{timed_out}"
`
	want := strings.Join([]string{
		`scale_arr!: 30 [3, 6, 9, 12]`,
		`map_ov: [9223372036854775806, 9223372036854775807, 9223372036854775810, 9223372036854775815]`,
		`sum_ov: 9223372036854775838`,
		`inject_ov: 9223372036854775835`,
		`times_ov: 9223372036854775811`,
		`upto_ov: 9223372036854775815`,
		`het_map: [2, 4, "abab", 8]`,
		`het_sum: 14.5`,
		`acc_deopt: 29.0`,
		`timeout_times: timed_out`,
	}, "\n")
	if got := strings.TrimSpace(aotRun(t, src, 0)); got != want {
		t.Fatalf("output differs\n got:\n%s\nwant:\n%s", got, want)
	}
}
