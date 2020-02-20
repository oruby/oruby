package process

import (
	"syscall"

	"github.com/oruby/oruby"
)

// RLimit pseudo constants
const (
	RLIMIT_CPU    = syscall.RLIMIT_CPU
	RLIMIT_FSIZE  = syscall.RLIMIT_FSIZE
	RLIMIT_DATA   = syscall.RLIMIT_DATA
	RLIMIT_STACK  = syscall.RLIMIT_STACK
	RLIMIT_CORE   = syscall.RLIMIT_CORE
	RLIMIT_AS     = syscall.RLIMIT_AS
	RLIMIT_NOFILE = syscall.RLIMIT_NOFILE
	RLIM_INFINITY = syscall.RLIM_INFINITY
)


func initPlatform(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {

	initPlatformUnix(mrb, mProc, mSys)
}


type limitsMap = map[int]syscall.Rlimit

func (runner *cmdRunner) checkLimitOption(limit int, option oruby.Value) {
	mrb := runner.mrb
	if !option.IsNil() {
		var rlimit syscall.Rlimit

		err := syscall.Getrlimit(limit, &rlimit)
		if err != nil {
			mrb.Raise(mrb.EArgumentError(), err.Error())
			return
		}

		if len(runner.limits) == 0 {
			runner.limits = make(map[int]syscall.Rlimit)
		}

		if option.IsArray() {
			rlimit.Cur = uint64(mrb.AryRef(option, 0).Int())
			rlimit.Max = uint64(mrb.AryRef(option, 1).Int())
		} else {
			rlimit.Max = uint64(option.Int())
		}

		runner.limits[limit] = rlimit
	}
}

func (runner *cmdRunner) parseOptionsOS(options oruby.Value) {
	if runner.umask != nil {
		runner.oldUmask = syscall.Umask(*runner.umask)
	}

	if runner.pgroup != nil {
		runner.cmd.SysProcAttr.Pgid = *runner.pgroup
	}
	runner.cleanup = func() {
		if runner.oldUmask != 0 {
			syscall.Umask(runner.oldUmask)
		}
	}
}

func (runner *cmdRunner) run() (int, error) {
	err := runner.cmd.Start()
	if err != nil {
		return 0, err
	}

	//TODO: ptrace with runner.cmd.SysProcAttr.Ptrace
	return runner.cmd.Process.Pid, err
}

func platformWait(pid, flags int, lastState *status) (int, error) {
	var waitStatus syscall.WaitStatus

	ret, err := syscall.Wait4(pid, &waitStatus, flags, nil)
	if err != nil {
		return ret, err
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
	lastState.Stopsig = int(waitStatus.StopSignal())
	lastState.Termsig = int(waitStatus.Signal())

	return int(lastState.ToI), nil
}

func platformKill(pid, sig int) error {
	return syscall.Kill(pid, syscall.Signal(sig))
}
