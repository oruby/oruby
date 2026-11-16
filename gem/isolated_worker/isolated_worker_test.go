package isolated_worker_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/isolated_worker"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/process"
)

func checkRuby(t *testing.T, mrb *oruby.MrbState, code string) {
	t.Helper()
	v, err := mrb.Eval(code)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool() {
		t.Fatalf("Ruby assertion failed: %s", code)
	}
}

func checkNoImplicitThreadOrFork(t *testing.T, mrb *oruby.MrbState) {
	t.Helper()
	// mruby 4.1 defines Thread as the namespace of Thread::Backtrace, so check
	// for a thread implementation rather than the constant
	checkRuby(t, mrb, `!(Object.const_defined?(:Thread) && Thread.method_defined?(:join)) && !Process.respond_to?(:fork)`)
}

func checkWorkerAvailable(t *testing.T, mrb *oruby.MrbState) {
	t.Helper()
	checkRuby(t, mrb, `ORuby::IsolatedWorker.new { 42 }.value == 42`)
}

func TestLazyResolve(t *testing.T) {
	first := oruby.MrbOpen()
	defer first.Close()
	second := oruby.MrbOpen()
	defer second.Close()

	checkRuby(t, first, `!Object.const_defined?(:ORuby) || !ORuby.const_defined?(:IsolatedWorker)`)
	checkNoImplicitThreadOrFork(t, first)
	loaded, err := first.Resolve("isolated_worker")
	if err != nil || !loaded {
		t.Fatalf("first Resolve: loaded=%v, err=%v", loaded, err)
	}
	checkRuby(t, first, `$worker_class = ORuby::IsolatedWorker; $worker_class.is_a?(Class)`)
	checkNoImplicitThreadOrFork(t, first)
	checkWorkerAvailable(t, first)

	loaded, err = first.Resolve("isolated_worker")
	if err != nil || loaded {
		t.Fatalf("second Resolve: loaded=%v, err=%v", loaded, err)
	}
	checkRuby(t, first, `!require("isolated_worker") && ORuby::IsolatedWorker.equal?($worker_class)`)
	checkRuby(t, second, `!Object.const_defined?(:ORuby) || !ORuby.const_defined?(:IsolatedWorker)`)
	checkNoImplicitThreadOrFork(t, second)
	loaded, err = second.Resolve("isolated_worker")
	if err != nil || !loaded {
		t.Fatalf("Resolve in second state: loaded=%v, err=%v", loaded, err)
	}
	checkRuby(t, second, `ORuby::IsolatedWorker.is_a?(Class)`)
	checkNoImplicitThreadOrFork(t, second)
}

func TestLazyRequire(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	checkRuby(t, mrb, `!Object.const_defined?(:ORuby) || !ORuby.const_defined?(:IsolatedWorker)`)
	checkNoImplicitThreadOrFork(t, mrb)
	checkRuby(t, mrb, `require "isolated_worker"`)
	checkRuby(t, mrb, `$worker_class = ORuby::IsolatedWorker; $worker_class.is_a?(Class)`)
	checkRuby(t, mrb, `!require("isolated_worker") && ORuby::IsolatedWorker.equal?($worker_class)`)
	loaded, err := mrb.Resolve("isolated_worker")
	if err != nil || loaded {
		t.Fatalf("Resolve after require: loaded=%v, err=%v", loaded, err)
	}
	checkNoImplicitThreadOrFork(t, mrb)
	checkWorkerAvailable(t, mrb)
}
