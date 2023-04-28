package process

import (
	"os"
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

func initPlatform(mrb *oruby.MrbState, mProc, mProcUID, mProcGID, mSys oruby.RClass) {
	mrb.DefineModuleFunc(mProc, "getpgrp", syscall.Getpgrp)
	mrb.DefineModuleFunc(mProc, "setpgrp", syscall.Setgroups)
	mrb.DefineModuleFunc(mProc, "getpgid", syscall.Getpgid)
	mrb.DefineModuleFunc(mProc, "setpgid", syscall.Setpgid)
	mrb.DefineModuleFunc(mProc, "setsid", syscall.Setsid)
	mrb.DefineModuleFunc(mProc, "getpriority", syscall.Getpriority)
	mrb.DefineModuleFunc(mProc, "setpriority", syscall.Setpriority)

	mProc.Const("PRIO_PROCESS", syscall.PRIO_PROCESS)
	mProc.Const("PRIO_PGRP", syscall.PRIO_PGRP)
	mProc.Const("PRIO_USER", syscall.PRIO_USER)

	mrb.DefineModuleFunc(mProc, "getrlimit", syscall.Getrlimit)
	mrb.DefineModuleFunc(mProc, "setrlimit", syscall.Setrlimit)

	mrb.DefineModuleFunc(mProc, "uid=", syscall.Setuid)
	mrb.DefineModuleFunc(mProc, "gid=", syscall.Setgid)
	mrb.DefineModuleFunc(mProc, "euid=", syscall.Seteuid)
	mrb.DefineModuleFunc(mProc, "egid=", syscall.Setegid)
	mrb.DefineModuleFunc(mProc, "groups=", syscall.Setgroups)

	//mrb.DefineModuleFunc(mProc, "maxgroups", proc_getmaxgroups, 0)
	//mrb.DefineModuleFunc(mProc, "maxgroups=", proc_setmaxgroups, 1)
	mrb.DefineModuleFunction(mProc, "daemon", mrb.NotImplemented, 0)
	mrb.DefineModuleFunction(mProc, "times", procTimes, mrb.ArgsNone())

	//mrb.DefineModuleFunction(mProc, "clock_gettime", rb_clock_gettime, -1)
	//mrb.DefineModuleFunction(mProc, "clock_getres", rb_clock_getres, -1)

	//mrb.DefineModuleFunction(mProcUID, "change_privilege", p_uid_change_privilege, 1)
	//mrb.DefineModuleFunction(mProcGID, "change_privilege", p_gid_change_privilege, 1)
	mrb.DefineModuleFunc(mProcUID, "grant_privilege", syscall.Seteuid)
	mrb.DefineModuleFunc(mProcGID, "grant_privilege", syscall.Seteuid)
	mrb.DefineAlias(mrb.SingletonClass(mProcUID), "eid=", "grant_privilege")
	mrb.DefineAlias(mrb.SingletonClass(mProcGID), "eid=", "grant_privilege")
	mrb.DefineModuleFunction(mProcUID, "re_exchange", puidExchange, mrb.ArgsNone())
	//mrb.DefineModuleFunction(mProcGID, "re_exchange", p_gid_exchange, 0)
	mrb.DefineModuleFunction(mProcUID, "re_exchangeable?", trueFunc, mrb.ArgsNone())
	mrb.DefineModuleFunction(mProcGID, "re_exchangeable?", trueFunc, mrb.ArgsNone())
	mrb.DefineModuleFunction(mProcUID, "sid_available?", trueFunc, mrb.ArgsNone())
	mrb.DefineModuleFunction(mProcGID, "sid_available?", trueFunc, mrb.ArgsNone())
	mrb.DefineModuleFunction(mProcUID, "switch", puidSwitch, mrb.ArgsNone())
	//mrb.DefineModuleFunction(mProcGID, "switch", p_gid_switch, 0)

	mrb.DefineModuleFunc(mSys, "setuid", syscall.Setuid)
	mrb.DefineModuleFunc(mSys, "setgid", syscall.Setgid)
	//mrb.DefineModuleFunc(mSys, "setruid", syscall.Setruid)
	//mrb.DefineModuleFunc(mSys, "setrgid", syscall.Setrgid)
	mrb.DefineModuleFunc(mSys, "seteuid", syscall.Seteuid)
	mrb.DefineModuleFunc(mSys, "setegid", syscall.Setegid)
	mrb.DefineModuleFunc(mSys, "setreuid", syscall.Setreuid)
	mrb.DefineModuleFunc(mSys, "setregid", syscall.Setregid)
	//mrb.DefineModuleFunc(mSys, "setresuid", syscall.Setresuid)
	//mrb.DefineModuleFunc(mSys, "setresgid", syscall.Setresgid)

	initPlatformUnix(mrb, mProc, mSys)
}

func trueFunc(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.TrueValue()
}

func procTimes(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var usageSelf, usageChidren syscall.Rusage

	err := syscall.Getrusage(syscall.RUSAGE_SELF, &usageSelf)
	if err != nil {
		return mrb.SysFail(err)
	}
	err = syscall.Getrusage(syscall.RUSAGE_CHILDREN, &usageChidren)
	if err != nil {
		return mrb.SysFail(err)
	}

	utime := float64(usageSelf.Utime.Sec) + float64(usageSelf.Utime.Usec)/1e6
	stime := float64(usageSelf.Stime.Sec) + float64(usageSelf.Stime.Usec)/1e6
	cutime := float64(usageChidren.Utime.Sec) + float64(usageChidren.Utime.Usec)/1e6
	cstime := float64(usageChidren.Stime.Sec) + float64(usageChidren.Stime.Usec)/1e6

	procData := mrb.GetModuleData(self).(*processData)

	return procData.tms.NewInstance(utime, stime, cutime, cstime)
}

func puidExchange(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	uid := syscall.Getuid()
	euid := syscall.Geteuid()

	err := syscall.Setreuid(euid, uid)
	if err != nil {
		return mrb.SysFail(err)
	}

	procData := mrb.GetModuleData(self).(*processData)
	procData.savedUserID = uid

	return mrb.FixnumValue(uid)
}

func puidSwitch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	uid := syscall.Getuid()
	euid := syscall.Geteuid()
	procData := mrb.GetModuleData(self).(*processData)
	procData.savedUserID = uid

	if uid != euid {
		if err := syscall.Seteuid(uid); err != nil {
			return mrb.SysFail(err)
		}

		if !block.IsNil() {
			ret := mrb.YieldArgv(block)
			if err := syscall.Seteuid(procData.savedUserID); err != nil {
				return mrb.SysFail(err)
			}
			return ret
		}
		return mrb.FixnumValue(euid)

	} else if euid != procData.savedUserID {
		if err := syscall.Seteuid(procData.savedUserID); err != nil {
			return mrb.SysFail(err)
		}
		if !block.IsNil() {
			ret := mrb.YieldArgv(block)
			if err := syscall.Seteuid(euid); err != nil {
				return mrb.SysFail(err)
			}
			return ret
		}
		return mrb.FixnumValue(uid)
	}

	return mrb.SysFail(os.ErrPermission)
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
		runner.cmd.SysProcAttr.Setpgid = true
		runner.cmd.SysProcAttr.Setsid = true
	}

	runner.cleanup = func() {
		if runner.oldUmask != 0 {
			syscall.Umask(runner.oldUmask)
		}
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

func (runner *cmdRunner) run() (int, error) {
	err := runner.cmd.Start()
	if err != nil {
		return 0, err
	}

	//TODO: ptrace with runner.cmd.SysProcAttr.Ptrace
	return runner.cmd.Process.Pid, err
}

func platformGetShell() string {
	if shell, ok := os.LookupEnv("SHELL"); ok {
		return shell
	}
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err == nil {
		return shell
	}
	return "/bin/sh"
}

func platformWait(pid, flags int, lastState *status) (int, error) {
	var waitStatus syscall.WaitStatus
	var err error
	var ret int

	ret, err = syscall.Wait4(pid, &waitStatus, flags, nil)
	lastState.Pid = ret
	platformUpdateState(lastState, waitStatus)

	return ret, err
}

func platformUpdateState(lastState *status, sysState interface{}) {
	waitStatus, ok := sysState.(syscall.WaitStatus)
	if !ok {
		return
	}

	lastState.Exitstatus = waitStatus.ExitStatus()
	lastState.ToI = uint32(waitStatus.ExitStatus())
	lastState.IsCoredump = waitStatus.CoreDump()
	lastState.IsExited = waitStatus.Exited()
	lastState.IsSignaled = waitStatus.Signaled()
	lastState.IsStopped = waitStatus.Stopped()
	lastState.IsSucess = waitStatus.ExitStatus() == 0
	lastState.platformData = &waitStatus

	if waitStatus.StopSignal() >= 0 {
		stopSig := int(waitStatus.StopSignal())
		lastState.Stopsig = &stopSig
	}
	if waitStatus.Signal() >= 0 {
		termSig := int(waitStatus.Signal())
		lastState.Termsig = &termSig
	}
}

func platformKill(pid, sig int) error {
	return syscall.Kill(pid, syscall.Signal(sig))
}
