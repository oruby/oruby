package process

import (
	"github.com/oruby/oruby"
	"syscall"
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
	// runner.cmd.SysProcAttr.Ptrace = True;
	err := runner.cmd.Start()
	if err != nil {
		return 0, err
	}

	return runner.cmd.Process.Pid, err
}

func platformWait(pid, flags int, last_state *status) (int, error) {
	var waitStatus syscall.WaitStatus

	ret, err := syscall.Wait4(pid, &waitStatus, flags, nil)
	if err != nil {
		return ret, err
	}

	return setLastState(mrb, waitStatus)
}
