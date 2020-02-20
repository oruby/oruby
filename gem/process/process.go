package process

import (
	"fmt"
	"math"
	"os"
	"syscall"
	"time"

	"github.com/oruby/oruby"
	//_ "github.com/oruby/oruby/gem/thread"
	_ "github.com/oruby/oruby/gem/signal"
)

func init() {
	oruby.Gem("process", func(mrb *oruby.MrbState) interface{} {
		// Require thread, ignore if it is not included in gems
		// mrb.Require("thread")
		mrb.Require("signal")

		mrb.SetGV("$$", os.Getpid())
		mrb.SetGV("$?", nil) // last_status

		mrb.DefineGlobalFunction("exec", procExec, mrb.ArgsAny())
		//mrb.DefineGlobalFunction("fork", procFork, mrb.ArgsAny())
		mrb.DefineGlobalFunction("exit!", procExit, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("system", procSystem, mrb.ArgsAny())
		mrb.DefineGlobalFunction("spawn", procSpawn, mrb.ArgsReq(1)+mrb.ArgsRest())
		mrb.DefineGlobalFunction("sleep", procSleep, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("exit", procExit, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("abort", procAbort, mrb.ArgsOpt(1))

		mProc := mrb.DefineModule("Process")
		mProc.Const("WNOHANG", syscall.WNOHANG)
		mProc.Const("WUNTRACED", syscall.WUNTRACED)

		mrb.DefineMethod(mProc, "exec", procExec, mrb.ArgsAny())
		//mrb.DefineMethod(mProc, "fork", procFork, mrb.ArgsAny())
		mrb.DefineMethod(mProc, "spawn", procSpawn, mrb.ArgsReq(1)+mrb.ArgsRest())
		mrb.DefineMethod(mProc, "exit!", procExit, mrb.ArgsOpt(1))
		mrb.DefineMethod(mProc, "exit", procExit, mrb.ArgsOpt(1))
		mrb.DefineMethod(mProc, "abort", procAbort, mrb.ArgsOpt(1))
		mrb.DefineMethod(mProc, "last_status", procLastStatus, mrb.ArgsNone())
		mrb.DefineMethod(mProc, "kill", procKill, mrb.ArgsReq(2)+mrb.ArgsRest())
		mrb.DefineMethod(mProc, "wait", procWait, mrb.ArgsOpt(2))
		mrb.DefineMethod(mProc, "wait2", procWait2, mrb.ArgsOpt(2))
		mrb.DefineMethod(mProc, "waitpid", procWait, mrb.ArgsOpt(2))
		mrb.DefineMethod(mProc, "waitpid2", procWait2, mrb.ArgsOpt(2))
		mrb.DefineMethod(mProc, "waitall", procWaitall, mrb.ArgsNone())
		mrb.DefineMethod(mProc, "detach", procDetach, mrb.ArgsReq(1))

		if mrb.ClassDefined("Thread") {
			cWaiter := mrb.DefineClassUnder(mProc, "Waiter", mrb.ClassGet("Thread"))
			cWaiter.UndefClassMethod("new")
			//cWaiter.DefineMethod("pid", detachProcessPID, mrb.ArgsNone())
		}

		initStatus(mProc)

		mrb.DefineModuleFunc(mProc, "pid", os.Getpid)
		mrb.DefineModuleFunc(mProc, "ppid", os.Getppid)

		mrb.DefineModuleFunc(mProc, "getpgrp", syscall.Getpgrp)
		mrb.DefineModuleFunc(mProc, "setpgrp", syscall.Setgroups)
		mrb.DefineModuleFunc(mProc, "getpgid", syscall.Getpgid)
		mrb.DefineModuleFunc(mProc, "setpgid", syscall.Setpgid)
		mrb.DefineModuleFunc(mProc, "getsid", syscall.Getsid)
		mrb.DefineModuleFunc(mProc, "setsid", syscall.Setsid)
		mrb.DefineModuleFunc(mProc, "getpriority", syscall.Getpriority)
		mrb.DefineModuleFunc(mProc, "setpriority", syscall.Setpriority)

		mProc.Const("PRIO_PROCESS", syscall.PRIO_PROCESS)
		mProc.Const("PRIO_PGRP", syscall.PRIO_PGRP)
		mProc.Const("PRIO_USER", syscall.PRIO_USER)

		//mrb.DefineModuleFunction(mProc, "getrlimit", procGetrlimit, 1)
		//mrb.DefineModuleFunction(mProc, "setrlimit", procSetrlimit, -1)

		setConsts(mProc)

		mProc.Const("RLIM_SAVED_MAX", RLIM_INFINITY)
		mProc.Const("RLIM_INFINITY", RLIM_INFINITY)
		mProc.Const("RLIM_SAVED_CUR", RLIM_INFINITY)
		mProc.Const("RLIMIT_AS", RLIMIT_AS)
		mProc.Const("RLIMIT_CORE", RLIMIT_CORE)
		mProc.Const("RLIMIT_CPU", RLIMIT_CPU)
		mProc.Const("RLIMIT_DATA", RLIMIT_DATA)
		mProc.Const("RLIMIT_FSIZE", RLIMIT_FSIZE)
		//mProc.Const("RLIMIT_MEMLOCK", RLIMIT_MEMLOCK)
		//mProc.Const("RLIMIT_MSGQUEUE", RLIMIT_MSGQUEUE)
		//mProc.Const("RLIMIT_MSGQUEUE", RLIMIT_MSGQUEUE)
		//mProc.Const("RLIMIT_NICE", RLIMIT_NICE)
		mProc.Const("RLIMIT_NOFILE", RLIMIT_NOFILE)
		//mProc.Const("RLIMIT_NPROC", RLIMIT_NPROC)
		//mProc.Const("RLIMIT_RSS", RLIMIT_RSS)
		//mProc.Const("RLIMIT_RTPRIO", RLIMIT_RTPRIO)
		//mProc.Const("RLIMIT_RTTIME", RLIMIT_RTTIME)
		//mProc.Const("RLIMIT_SBSIZE", RLIMIT_SBSIZE)
		mProc.Const("RLIMIT_STACK", RLIMIT_STACK)

		mrb.DefineModuleFunc(mProc, "uid", os.Getuid)
		mrb.DefineModuleFunc(mProc, "gid", os.Getgid)
		mrb.DefineModuleFunc(mProc, "euid", os.Geteuid)
		mrb.DefineModuleFunc(mProc, "egid", os.Getegid)
		mrb.DefineModuleFunc(mProc, "groups", os.Getgroups)

		mrb.DefineModuleFunc(mProc, "uid=", syscall.Setuid)
		mrb.DefineModuleFunc(mProc, "gid=", syscall.Setgid)
		mrb.DefineModuleFunc(mProc, "euid=", syscall.Seteuid)
		mrb.DefineModuleFunc(mProc, "egid=",  syscall.Setegid)
		//mrb.DefineModuleFunc(mProc, "initgroups", proc_initgroups, 2)
		//mrb.DefineModuleFunc(mProc, "groups=", proc_setgroups, 1)
		//mrb.DefineModuleFunc(mProc, "maxgroups", proc_getmaxgroups, 0)
		//mrb.DefineModuleFunc(mProc, "maxgroups=", proc_setmaxgroups, 1)
		//mrb.DefineModuleFunc(mProc, "daemon", proc_daemon, -1)
		//mrb.DefineModuleFunc(mProc, "times", rb_proc_times, 0)

		//mrb.DefineModuleFunction(mProc, "clock_gettime", rb_clock_gettime, -1)
		//mrb.DefineModuleFunction(mProc, "clock_getres", rb_clock_getres, -1)

		//cProcessTms = rb_struct_define_under(mProc, "Tms", "utime", "stime", "cutime", "cstime", NULL)
		/* An obsolete name of Process::Tms for backward compatibility */
		//mrb.DefineConst(rb_cStruct, "Tms", rb_cProcessTms)
		//rb_deprecate_constant(rb_cStruct, "Tms")
		mProcUID := mrb.DefineModuleUnder(mProc, "UID")
		mProcGID := mrb.DefineModuleUnder(mProc, "GID")
		mrb.DefineModuleFunc(mProcUID, "rid", os.Getuid)
		mrb.DefineModuleFunc(mProcGID, "gid", os.Getgid)
		mrb.DefineModuleFunc(mProcUID, "eid", os.Geteuid)
		mrb.DefineModuleFunc(mProcGID, "gid", os.Getegid)
		//mrb.DefineModuleFunction(mProcUID, "change_privilege", p_uid_change_privilege, 1)
		//mrb.DefineModuleFunction(mProcGID, "change_privilege", p_gid_change_privilege, 1)
		mrb.DefineModuleFunc(mProcUID, "grant_privilege", syscall.Seteuid)
		mrb.DefineModuleFunc(mProcGID, "grant_privilege", syscall.Seteuid)
		mrb.DefineAlias(mrb.SingletonClass(mProcUID), "eid=", "grant_privilege")
		mrb.DefineAlias(mrb.SingletonClass(mProcGID), "eid=", "grant_privilege")
		//mrb.DefineModuleFunction(mProcUID, "re_exchange", p_uid_exchange, 0)
		//mrb.DefineModuleFunction(mProcGID, "re_exchange", p_gid_exchange, 0)
		//mrb.DefineModuleFunction(mProcUID, "re_exchangeable?", p_uid_exchangeable, 0)
		//mrb.DefineModuleFunction(mProcGID, "re_exchangeable?", p_gid_exchangeable, 0)
		//mrb.DefineModuleFunction(mProcUID, "sid_available?", p_uid_have_saved_id, 0)
		//mrb.DefineModuleFunction(mProcGID, "sid_available?", p_gid_have_saved_id, 0)
		//mrb.DefineModuleFunction(mProcUID, "switch", p_uid_switch, 0)
		//mrb.DefineModuleFunction(mProcGID, "switch", p_gid_switch, 0)
		//mrb.DefineModuleFunction(mProcUID, "from_name", p_uid_from_name, 1)
		//mrb.DefineModuleFunction(mProcGID, "from_name", p_gid_from_name, 1)

		mSys := mrb.DefineModuleUnder(mProc, "Sys")
		mrb.DefineModuleFunc(mSys, "getuid", os.Getuid)
		mrb.DefineModuleFunc(mSys, "geteuid", os.Geteuid)
		mrb.DefineModuleFunc(mSys, "getgid", os.Getgid)
		mrb.DefineModuleFunc(mSys, "getegid", os.Getegid)

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
		mrb.DefineModuleFunc(mSys, "issetugid", syscall.Issetugid)

		initPlatform(mrb, mProc, mSys)
		return nil
	})
}

func procExec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetArgs())
	defer runner.cleanup()
	//TODO: start shell
	err := syscall.Exec(runner.cmd.Args[0], runner.cmd.Args, runner.cmd.Env)
	if err != nil {
		return mrb.RaiseError(oruby.EError("SystemCallError", err.Error()))
	}
	return mrb.NilValue()
}

func procSystem(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetArgs())
	defer runner.cleanup()

	pid, err := runner.run()
	if err != nil {
		return mrb.ERuntimeError().RaiseError(err)
	}

	return doWait(mrb, self, pid, 0)
}

func procSpawn(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetArgs())
	defer runner.cleanup()

	pid, err := runner.run()
	if err != nil {
		return mrb.ERuntimeError().RaiseError(err)
	}
	return mrb.FixnumValue(pid)
}

func setStatus(mrb *oruby.MrbState, state *status) {
	if state == nil {
		mrb.SetGV("$?", mrb.NilValue())
		return
	}
	mrb.SetGV("$?", mrb.Value(state))
}

func doWait(mrb *oruby.MrbState, self oruby.Value, pid, flags int) oruby.Value {
	lastState := &status{}

	ret, err := platformWait(pid, flags, lastState)
	if err != nil {
		setStatus(mrb, nil)
		return mrb.ERuntimeError().RaiseError(err)
	}

	setStatus(mrb, lastState)

	return mrb.FixnumValue(ret)
}

func procWait(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pid, flags := mrb.GetArgs2(-1, 0)
	return doWait(mrb, self, pid.Int(), flags.Int())
}

func procLastStatus(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.GetGV("$?")
}

func procWait2(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pid, flags := mrb.GetArgs2(-1, 0)

	ret := doWait(mrb, self, pid.Int(), flags.Int())
	if !ret.IsFixnum() {
		return ret
	}

	return mrb.AryNewFromValues(ret, mrb.GetGV("$?"))
}

func procWaitall(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var pid int
	var err error
	var ret = mrb.AryNew()

	setStatus(mrb, nil)

	for pid >= 0 {
		lastState := &status{}
		pid, err = platformWait(pid, 0, lastState)
		if pid == -1 {
			break
		}
		if err != nil {
			setStatus(mrb, nil)
			return mrb.ERuntimeError().RaiseError(err)
		}
		setStatus(mrb, lastState)
		ret.Push(mrb.AryNewFromValues(mrb.FixnumValue(pid), mrb.Value(lastState)))
	}

	return ret
}

func procDetach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pid := mrb.GetArgsFirst().Int()
	p, err := os.FindProcess(pid)
	if err != nil {
		return mrb.NilValue()
	}

	if p.Release() != nil {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(pid)
}

func procKill(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	sig := args.Item(1)

	for i := 1; i < args.Len(); i++ {
		pid := args.ItemDef(i, mrb.NilValue())
		err := platformKill(pid.Int(), sig.Int())
		if err != nil {
			return mrb.RaiseError(err)
		}
	}
	return mrb.NilValue()
}

func procAbort(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	msg := mrb.GetArgsFirst().String()
	if msg != "" {
		fmt.Println(msg)
	} else if mrb.Exc() != nil {
		fmt.Println(mrb.Err().Error())
	}
	os.Exit(1)
	return mrb.NilValue()
}

func procExit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	status := mrb.GetArgsFirst()

	switch status.Type() {
	case oruby.MrbTTTrue:
		os.Exit(0)
	case oruby.MrbTTFalse:
		if status.IsNil() {
			os.Exit(0)
		} else {
			os.Exit(1)
		}
	default:
		os.Exit(status.Int())
	}
	return mrb.NilValue()
}

func procSleep(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	t := mrb.GetArgsFirst()
	var duration time.Duration
	if t.IsNil() {
		duration = time.Duration(math.MaxInt64)
	} else {
		duration = time.Duration(int(t.Float64() * 1000))
	}

	select {
	case <-mrb.ExitChan():
		break
	case <-time.After(duration * time.Millisecond):
		break
	}

	return mrb.NilValue()
}
