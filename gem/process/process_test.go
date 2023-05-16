package process

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
)

func executable() string {
	if os.PathSeparator == '\\' {
		return "'where'"
	}
	return "'/bin/echo'"
}

func oneLiner(eval string) error {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(eval)
	return err
}

func TestDaemon(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval("Process.daemon")
	assert.Error(t, err, "Process.daemon should raise NotImplemented error")
}

func TestGlobals(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval("$$")
	assert.NilError(t, err)
	assert.Equal(t, v.Int(), os.Getpid())

	v, err = mrb.Eval("$?")
	assert.NilError(t, err)
	assert.Expect(t, v.IsNil(), "last status should be nil")

	v, err = mrb.Eval("system 'echo', 'test'")
	assert.NilError(t, err)

	v, err = mrb.Eval("$?")
	assert.NilError(t, err)
	assert.Expect(t, !v.IsNil(), "last status should exist")
}

func TestLastStatus(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("system '/bin/echo', 'test'")
	assert.NilError(t, err)
	assert.Expect(t, !ret.IsNil(), "return value should be not nil")
	assert.Expect(t, ret.Value().IsBool(), "should be bool, got %v: '%v'", mrb.TypeName(ret), ret)
	assert.Expect(t, ret.Value().Bool(), "should return true")

	v := mrb.GetGV("$?")
	last := mrb.Data(v).(*status)
	assert.Expect(t, last.IsExited, "Should be exited")
	assert.Expect(t, last.IsSucess, "Should be sucess")
}

func TestSystem(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("system('echo', 'test')")
	assert.NilError(t, err)
	assert.Expect(t, !ret.IsNil(), "return value should be not nil")
	assert.Expect(t, ret.Value().IsBool(), "should be bool")
	assert.Expect(t, ret.Value().Bool(), "should return true")

	v := mrb.GetGV("$?")
	last := mrb.Data(v).(*status)
	assert.Expect(t, last.IsExited, "Should be exited")
	assert.Expect(t, last.IsSucess, "Should be sucessful")

	ret, err = mrb.Eval("system '/bin/NOT_EXISTS'")
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "return value should be nil; got %v", mrb.String(ret))

	v = mrb.GetGV("$?")
	last = mrb.Data(v).(*status)
	assert.Expect(t, last.IsExited, "Should be exited")
	assert.Expect(t, !last.IsSucess, "Should be unsucessful")
}

func TestSpawn(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// Spawn process
	pid, err := mrb.Eval("$pid = Process.spawn '/bin/echo'")
	assert.NilError(t, err)
	assert.Expect(t, pid.Value().IsInteger(), "expeted pid, got %v", pid.String())

	// Wait for it to finish
	ret, err := mrb.Eval("Process.wait $pid")
	assert.NilError(t, err)
	assert.Expect(t, ret.Value().IsInteger(), "expeted pid, got %v", ret.String())

	// Check LastStatus
	v := mrb.GetGV("$?")
	last := mrb.Data(v).(*status)
	assert.Equal(t, last.Pid, pid.Int())
	assert.Equal(t, last.Pid, ret.Int())
	assert.Expect(t, last.IsExited, "Should be exited")
	assert.Expect(t, last.IsSucess, "Should be sucessful")
}

func TestSpawnWithParams(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	pid, err := mrb.Eval("$pid = Process.spawn('/bin/echo test')")
	assert.NilError(t, err)
	assert.Expect(t, pid.Value().IsInteger(), "expected pid, got %v", pid.String())

	// Wait for it to finish
	ret, err := mrb.Eval("Process.wait $pid")
	assert.NilError(t, err)
	assert.Expect(t, ret.Value().IsInteger(), "expeted pid, got %v", ret.String())
}

func TestWait(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// wait if any lingering children from other tests
	mrb.Run("Process.wait")

	pid1, _ := mrb.Eval("Process.spawn " + executable())
	pid2, _ := mrb.Eval("Process.spawn " + executable())
	pid3, _ := mrb.Eval("Process.spawn " + executable())
	p1, p2, p3 := pid1.Int(), pid2.Int(), pid3.Int()

	wpid1, _ := mrb.Eval("Process.wait -1")
	wpid2, _ := mrb.Eval("Process.wait") // -1 (wait any child) is default

	// 0 (wait any child ni group) - this will fallback to platform specific Wait
	wpid3, err := mrb.Eval("Process.wait(0)")
	assert.NilError(t, err)

	assert.Include(t, p1, wpid1.Int(), wpid2.Int(), wpid3.Int())
	assert.Include(t, p2, wpid1.Int(), wpid2.Int(), wpid3.Int())
	assert.Include(t, p3, wpid1.Int(), wpid2.Int(), wpid3.Int())

	v := mrb.GetGV("$?")
	last := mrb.Data(v).(*status)
	assert.Equal(t, last.Pid, wpid3.Int())

	// Wait non existing pid
	_, err = mrb.Eval("Process.wait " + wpid3.String())
	assert.Error(t, err, "process should not exists")
}

func TestAbort(t *testing.T) {
	if os.Getenv("GO_ABORT_GO") == "1" {
		err := oneLiner("abort")
		assert.NilError(t, err)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestAbort")
	cmd.Env = append(os.Environ(), "GO_ABORT_GO=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Errorf("'abort' with err %v, want exit status 1", err)
}

func TestExitFalse(t *testing.T) {
	if os.Getenv("GO_EXIT_GO") == "1" {
		err := oneLiner("exit(false)")
		assert.NilError(t, err)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestExit")
	cmd.Env = append(os.Environ(), "GO_EXIT_GO=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Errorf("'exit' with err %v, want exit status 0", err)
}

func TestExitTrue(t *testing.T) {
	if os.Getenv("GO_EXIT_GO") == "1" {
		err := oneLiner("exit(true)")
		assert.NilError(t, err)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestExitTrue")
	cmd.Env = append(os.Environ(), "GO_EXIT_GO=1")
	err := cmd.Run()
	if err == nil {
		return
	}
	if e, ok := err.(*exec.ExitError); ok && e.Success() {
		return
	}
	t.Errorf("'exit' with err %v, want exit status 0", err)
}

func TestKill(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval("$pid = Process.spawn " + executable())
	assert.NilError(t, err)
	_, err = mrb.Eval("Process.kill :KILL, $pid")
	assert.NilError(t, err)
	_, err = mrb.Eval("Process.detach $pid")
	assert.NilError(t, err)
	_, err = mrb.Eval("Process.wait $pid")
	assert.NilError(t, err)

	v := mrb.GetGV("$?")
	last := mrb.Data(v).(*status)
	assert.Equal(t, last.IsSignaled, true)
	assert.Expect(t, last.Termsig != nil, "Should be terminated with signal")
	assert.Expect(t, syscall.Signal(*last.Termsig) == os.Kill, "killled with SIGKILL")

	_, err = mrb.Eval("Process.kill :KILL, $pid")
	assert.Error(t, err, "process should be unknown")
}

func TestDetach(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	pid, err := mrb.Eval("$pid = Process.spawn " + executable())
	assert.NilError(t, err)
	_, err = mrb.Eval("Process.detach $pid")
	assert.NilError(t, err)

	state := getProcData(mrb, mrb.NilValue())
	for _, r := range state.runners {
		assert.Expect(t, r == nil || r.cmd.Process.Pid != pid.Int(), "Pid should be detached")
	}

	p, err := os.FindProcess(pid.Int())
	assert.NilError(t, err)
	err = p.Kill()
	assert.NilError(t, err)
}

func TestWait2(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	pid1, _ := mrb.Eval("Process.spawn " + executable())

	ret, err := mrb.Eval("Process.wait2 " + pid1.String())
	assert.NilError(t, err)
	assert.Expect(t, ret.Value().IsArray(), "Array expected")
	assert.Expect(t, ret.RArray().Len() == 2, "Array size 2 expected")

	p := ret.RArray().Item(0)
	stat := ret.RArray().Item(1)

	assert.Expect(t, p.Int() == pid1.Int(), "Pid should exist")

	v := mrb.GetGV("$?")
	last := mrb.Data(v).(*status)
	assert.Equal(t, last, mrb.Data(stat))
}

func TestWaitpid(t *testing.T) {
	TestWait(t)
}

func TestWaitpid2(t *testing.T) {
	TestWait2(t)
}

func TestWaitall(t *testing.T) {

}
