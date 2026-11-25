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
