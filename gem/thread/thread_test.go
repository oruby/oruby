package thread

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/isolated_worker"
)

func TestCurrentAndMain(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`
		Thread.current.equal?(Thread.current) &&
		  Thread.main.equal?(Thread.main) &&
		  Thread.current.equal?(Thread.main) &&
		  Thread.current.is_a?(Thread)
	`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool() {
		t.Fatal("Thread.current and Thread.main must return the same Thread")
	}
}

func TestThreadMutexConstantAliasesMutex(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(`Thread::Mutex.equal?(Mutex) && Thread::Mutex.new.synchronize { true }`)
	if err != nil || !v.Bool() {
		t.Fatalf("Thread::Mutex alias and synchronize = %v, %v; want true, nil", v, err)
	}
}

func TestIsolatedWorkerJoinTimeoutDuringSleep(t *testing.T) {
	const key = "ORUBY_WORKER_TIMEOUT_SUBPROCESS"
	if os.Getenv(key) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIsolatedWorkerJoinTimeoutDuringSleep$")
		cmd.Env = append(os.Environ(), key+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("worker timeout subprocess: %v\n%s", err, output)
		}
		return
	}
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	value, err := mrb.Eval(`
		$w = ORuby::IsolatedWorker.new { sleep 0.3; 42 }
		$w.join(0.03).nil?
	`)
	if err != nil {
		t.Fatal(err)
	}
	if !value.Bool() {
		t.Fatal("timed join did not time out")
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("timed join took %v", elapsed)
	}
	result, err := mrb.Eval(`$w.join.equal?($w) && $w.value == 42`)
	if err != nil || !result.Bool() {
		t.Fatalf("worker completion: %v, %v", result, err)
	}
}

func TestThreadCreationUnsupported(t *testing.T) {
	for _, method := range []string{"new", "start", "go"} {
		t.Run(method, func(t *testing.T) {
			for _, args := range []string{"", "(1, 2)", "(1, 2) { ran = true }"} {
				t.Run(args, func(t *testing.T) {
					mrb := oruby.MrbOpen()
					defer mrb.Close()

					v, err := mrb.Eval(`
						ran = false
						begin
						  Thread.` + method + args + `
						  false
						rescue NotImplementedError
						  !ran
						end
					`)
					if err != nil {
						t.Fatal(err)
					}
					if !v.Bool() {
						t.Fatalf("Thread.%s%s must raise NotImplementedError without running its block", method, args)
					}
				})
			}
		})
	}
}

func TestMutexUnlockUnlockedRaisesThreadError(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`Mutex.new.unlock`)
	if err == nil {
		t.Fatal("expected unlock on unlocked mutex to raise ThreadError")
	}
	if !strings.Contains(err.Error(), "ThreadError") {
		t.Fatalf("expected ThreadError, got: %v", err)
	}
}

func TestMutexSleepUnlockedRaisesThreadError(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`Mutex.new.sleep`)
	if err == nil {
		t.Fatal("expected sleep on unlocked mutex to raise ThreadError")
	}
	if !strings.Contains(err.Error(), "ThreadError") {
		t.Fatalf("expected ThreadError, got: %v", err)
	}
}

func TestMutexRecursiveLockRaisesThreadError(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`m = Mutex.new; m.lock; m.lock`)
	if err == nil {
		t.Fatal("expected recursive lock to raise ThreadError")
	}
	if !strings.Contains(err.Error(), "ThreadError") {
		t.Fatalf("expected ThreadError, got: %v", err)
	}
}

func TestMutexSleepNegativeDurationRaisesArgumentError(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`Mutex.new.sleep(-1)`)
	if err == nil {
		t.Fatal("expected negative sleep to raise ArgumentError")
	}
	if !strings.Contains(err.Error(), "ArgumentError") {
		t.Fatalf("expected ArgumentError, got: %v", err)
	}
}

func TestMutexSleepLockedAndWoken(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(`
		m = Mutex.new
		m.lock
		slept = m.sleep(0.01)
		slept.nil? && m.locked?
	`)
	if err != nil || !v.Bool() {
		t.Fatalf("expected a timed-out sleep to return nil (as MRI) and relock the mutex, got %v, err: %v", v, err)
	}
}

func TestMutexLockReturnsSelfAndOwner(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(`
		m = Mutex.new
		ok = m.lock.equal?(m) && m.owned?
		f = Fiber.new { [m.locked?, m.owned?, (m.unlock rescue $!.class), (m.lock rescue $!.class)] }
		ok && f.resume == [true, false, ThreadError, ThreadError] && m.unlock.equal?(m) && !m.owned?
	`)
	if err != nil || !v.Bool() {
		t.Fatalf("lock/unlock must return self and the lock belongs to its Fiber: %v, err: %v", v, err)
	}
}

func TestMutexSynchronizeUnlockedInBlockRaises(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(`
		m = Mutex.new
		r = (m.synchronize { m.unlock; :done } rescue $!.class)
		r == ThreadError && !m.locked?
	`)
	if err != nil || !v.Bool() {
		t.Fatalf("synchronize whose block unlocked must raise ThreadError: %v, err: %v", v, err)
	}
}

func TestMutexSleepForeverIsFatal(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(`
		m = Mutex.new
		m.lock
		e = begin; m.sleep; rescue Exception => x; x; end
		[e.class.name, e.class.superclass, m.locked?, (m.sleep("1") rescue $!.class)] == ["fatal", Exception, true, TypeError]
	`)
	if err != nil || !v.Bool() {
		t.Fatalf("sleep without timeout must raise fatal and keep the lock: %v, err: %v", v, err)
	}
}

func TestMutexSynchronizeNonRecursive(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(`
		m = Mutex.new
		raised = false
		m.synchronize do
			begin
				m.synchronize { }
			rescue ThreadError
				raised = true
			end
		end
		raised && !m.locked?
	`)
	if err != nil || !v.Bool() {
		t.Fatalf("expected nested synchronize to raise ThreadError and unlock cleanly, got %v, err: %v", v, err)
	}
}
