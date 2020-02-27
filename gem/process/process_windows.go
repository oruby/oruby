package process

import (
	"github.com/oruby/oruby"
	"os"
)

// RLimit pseudo constants
const (
	RLIMIT_CPU    = 0x0
	RLIMIT_FSIZE  = 0x1
	RLIMIT_DATA   = 0x2
	RLIMIT_STACK  = 0x3
	RLIMIT_CORE   = 0x4
	RLIMIT_AS     = 0x5
	RLIMIT_NOFILE = 0x8
	RLIM_INFINITY = 0x7fffffffffffffff
)

func initPlatform(mrb *oruby.MrbState, mProc, mProcUID, mProcGID, mSys oruby.RClass) {

}

type limitsMap = int

func (runner *cmdRunner) checkLimitOption(limit int, option oruby.Value) {
	// do nothing on windows - rlimits are not supported
}

func (runner *cmdRunner) parseOptionsOS(options oruby.Value) func() {
	ret := func() {}
	mrb := runner.mrb
	if !options.IsHash() {
		return ret
	}

	if o := mrb.HashGet(options, mrb.Intern("new_pgroup")); o.Bool() {
		runner.cmd.SysProcAttr.CreationFlags += CREATE_NEW_PROCESS_GROUP
	}

	return ret
}

func platformGetShell() string {
	if shell, ok := os.LookupEnv("ComSpec"); ok {
		return shell
	}
	if shell, ok := os.LookupEnv("COMSPEC"); ok {
		return shell
	}
	return os.GetEnv("SYSTEMROOT") + "\\System32\\cmd.exe"
}


func (runner *cmdRunner) run() (int, error) {
	os.StartProcess()
	err := runner.cmd.Start()
	if err != nil {
		return 0, err
	}
	return runner.cmd.Process.Pid, nil
}

func (runner *cmdRunner) platformWait(pid, flags int, lastState *status) (int, error) {
	var waitStatus syscall.WaitStatus
	ret, err := syscall.Wait4(pid, &waitStatus, flags, nil)
	platformUpdateState(lastState, waitStatus)

	return ret, err
}

func  platformUpdateState(lastState *status, sysState interface{}) {
	waitStatus, ok := sysState.(syscall.WaitStatus)
	if !ok {
		return
	}

	lastState.Pid = pid
	lastState.Exitstatus = waitStatus.ExitStatus()
	lastState.ToI = uint32(waitStatus.ExitStatus())
	lastState.IsCoredump = waitStatus.CoreDump()
	lastState.IsExited = waitStatus.Exited()
	lastState.IsSignaled = waitStatus.Signaled()
	lastState.IsStopped = waitStatus.Stopped()
	lastState.IsSucess = waitStatus.ExitStatus() == 0
	lastState.platformData = &waitStatus
	if waitStatus.StopSignal() >= 0 {
		*lastState.Stopsig = int(waitStatus.StopSignal())
	}
	if waitStatus.Signal() >= 0 {
		*lastState.Termsig = int(waitStatus.Signal())
	}
}

func (runner *cmdRunner) forkExec() (int, error) {
	files := make([]uintptr, len(runner.cmd.ExtraFiles))
	for i, f := range runner.cmd.ExtraFiles {
		files[i] = f.Fd()
	}

	return syscall.ForkExec(runner.cmd.Args[0], runner.cmd.Args, &syscall.ProcAttr{
		Dir:   runner.cmd.Dir,
		Env:   runner.cmd.Env,
		Files: files,
		Sys:   runner.cmd.SysProcAttr,
	})
}
