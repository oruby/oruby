package isolated_worker_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/oruby/oruby"
)

func TestWorkerValueAndJoin(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		t.Fatal(err)
	}
	checkRuby(t, mrb, `
		w = ORuby::IsolatedWorker.new("abc") { |s| s + "!" }
		w.join.equal?(w) && w.join.equal?(w) && w.value == "abc!" && w.value == "abc!"
	`)
}

func TestWorkerFailure(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		t.Fatal(err)
	}
	checkRuby(t, mrb, `
		w = ORuby::IsolatedWorker.new { raise "worker failure" }
		w.join.equal?(w) && (begin; w.value; false; rescue RuntimeError => e; e.message.include?("worker failure"); end) &&
		  (begin; w.value; false; rescue RuntimeError => e; e.message.include?("worker failure"); end)
	`)
}

func TestWorkerResultSurvivesGC(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		t.Fatal(err)
	}
	ai := mrb.GCArenaSave()
	checkRuby(t, mrb, `$w = ORuby::IsolatedWorker.new { ["result", :ok] }; $w.value == ["result", :ok]`)
	mrb.GCArenaRestore(ai)
	mrb.FullGC()
	checkRuby(t, mrb, `$w.value == ["result", :ok]`)
}

func TestWorkerPreservesFrozenValues(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		t.Fatal(err)
	}
	checkRuby(t, mrb, `
		w = ORuby::IsolatedWorker.new(["argument".freeze].freeze) { |arg| [arg.frozen?, arg[0].frozen?].freeze }
		v = w.value
		v.frozen? && v[0] && v[1]
	`)
}

func TestWorkerRejectsUnsupportedTransfers(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		t.Fatal(err)
	}
	checkRuby(t, mrb, `begin; ORuby::IsolatedWorker.new({a: 1}) { |x| x }; false; rescue Exception; true; end`)
	checkRuby(t, mrb, `w = ORuby::IsolatedWorker.new { x = []; [x, x] }; begin; w.value; false; rescue RuntimeError; true; end`)
	checkRuby(t, mrb, `w = ORuby::IsolatedWorker.new { 1 }; begin; w.kill; false; rescue NotImplementedError; true; end`)
}

func TestWorkerCloseWaits(t *testing.T) {
	const key = "ORUBY_WORKER_CLOSE_SUBPROCESS"
	if os.Getenv(key) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkerCloseWaits$")
		cmd.Env = append(os.Environ(), key+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("worker shutdown subprocess: %v\n%s", err, output)
		}
		return
	}
	mrb := oruby.MrbOpen()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Eval(`ORuby::IsolatedWorker.new { i = 0; i += 1 while i < 100_000; i }`); err != nil {
		t.Fatal(err)
	}
	mrb.Close()
}
