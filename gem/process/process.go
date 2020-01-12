package process

import (
	"fmt"
	"github.com/oruby/oruby"
	"math"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func init() {
	oruby.Gem("process", func(mrb *oruby.MrbState) {
		// Require thread, ignore if it is not included in gems
		_,_ = mrb.Require("thread")

		mrb.SetGV("$$", os.Getpid())
		mrb.SetGV("$?", nil) // last_status

		mrb.DefineGlobalFunction("exec", procExec, -1)
		mrb.DefineGlobalFunction("fork", procFork, mrb.ArgsAny())
		mrb.DefineGlobalFunction("exit!", procExit, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("system", procSystem, -1)
		mrb.DefineGlobalFunction("spawn", procSpawn, mrb.ArgsReq(1)+mrb.ArgsRest())
		mrb.DefineGlobalFunction("sleep", procSleep, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("exit", procExit, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("abort", procAbort, mrb.ArgsOpt(1))

		mProc := mrb.DefineModule("Process")
		mProc.Const("WNOHANG", syscall.WNOHANG)
		mProc.Const("WUNTRACED", syscall.WUNTRACED)

		mrb.DefineMethod(mProc, "exec", f_exec, -1)
		mrb.DefineMethod(mProc, "fork", procFork, mrb.ArgsAny())
		mrb.DefineMethod(mProc, "spawn", procSpawn, mrb.ArgsReq(1)+mrb.ArgsRest())
		mrb.DefineMethod(mProc, "exit!", procExit, mrb.ArgsOpt(1))
		mrb.DefineMethod(mProc, "exit", procExit, mrb.ArgsOpt(1))
		mrb.DefineMethod(mProc, "abort", procAbort, mrb.ArgsOpt(1))
		mrb.DefineMethod(mProc, "last_status", proc_s_last_status, 0)
		mrb.DefineMethod(mProc, "kill", proc_rb_f_kill, -1)
		mrb.DefineMethod(mProc, "wait", procWait, mrb.ArgsOpt(2))
		mrb.DefineMethod(mProc, "wait2", proc_wait2, -1)
		mrb.DefineMethod(mProc, "waitpid", procWait, mrb.ArgsOpt(2))
		mrb.DefineMethod(mProc, "waitpid2", proc_wait2, -1)
		mrb.DefineMethod(mProc, "waitall", proc_waitall, 0)
		mrb.DefineMethod(mProc, "detach", procDetach, mrb.ArgsReq(1))

		cThread := mrb.ClassGet("Thread")
		if !cThread.IsNil() {
			cWaiter := mrb.DefineClassUnder(mProc, "Waiter", cThread)
			cWaiter.UndefClassMethod("new")
			cWaiter.DefineMethod("pid", detach_process_pid, 0)
		}

		initStatus(mProc)

		mrb.DefineModuleFunc(mProc, "pid", os.Getpid)
		mrb.DefineModuleFunc(mProc, "ppid", os.Getppid)
		mrb.DefineModuleFunc(mProc, "getpgrp", syscall.Getpgrp)
		mrb.DefineModuleFunc(mProc, "setpgrp", syscall.set proc_setpgrp, 0)
		mrb.DefineModuleFunc(mProc, "getpgid", syscall.Getpgid)
		mrb.DefineModuleFunc(mProc, "setpgid", syscall.Setpgid)
		mrb.DefineModuleFunc(mProc, "getsid", syscall.Getsid)
		mrb.DefineModuleFunc(mProc, "setsid", syscall.Setsid)
		mrb.DefineModuleFunc(mProc, "getpriority", syscall.Getpriority)
		mrb.DefineModuleFunc(mProc, "setpriority", syscall.Setpriority)

		mProc.Const("PRIO_PROCESS", syscall.PRIO_PROCESS)
		mProc.Const("PRIO_PGRP", syscall.PRIO_PGRP)
		mProc.Const("PRIO_USER", syscall.PRIO_USER)

		mrb.DefineModuleFunction(mProc, "getrlimit", proc_getrlimit, 1)
		mrb.DefineModuleFunction(mProc, "setrlimit", proc_setrlimit, -1)

		mProc.Const("RLIM_SAVED_MAX", syscall.RLIM_INFINITY)
		mProc.Const("RLIM_INFINITY", syscall.RLIM_INFINITY)
		mProc.Const("RLIM_SAVED_CUR", syscall.RLIM_INFINITY)
		mProc.Const("RLIMIT_AS", syscall.RLIMIT_AS)
		mProc.Const("RLIMIT_CORE", syscall.RLIMIT_CORE)
		mProc.Const("RLIMIT_CPU", syscall.RLIMIT_CPU)
		mProc.Const("RLIMIT_DATA", syscall.RLIMIT_DATA)
		mProc.Const("RLIMIT_FSIZE", syscall.RLIMIT_FSIZE)
		//mProc.Const("RLIMIT_MEMLOCK", syscall.RLIMIT_MEMLOCK)
		//mProc.Const("RLIMIT_MSGQUEUE", syscall.RLIMIT_MSGQUEUE)
		//mProc.Const("RLIMIT_MSGQUEUE", syscall.RLIMIT_MSGQUEUE)
		//mProc.Const("RLIMIT_NICE", syscall.RLIMIT_NICE)
		mProc.Const("RLIMIT_NOFILE", syscall.RLIMIT_NOFILE)
		//mProc.Const("RLIMIT_NPROC", syscall.RLIMIT_NPROC)
		//mProc.Const("RLIMIT_RSS", syscall.RLIMIT_RSS)
		//mProc.Const("RLIMIT_RTPRIO", syscall.RLIMIT_RTPRIO)
		//mProc.Const("RLIMIT_RTTIME", syscall.RLIMIT_RTTIME)
		//mProc.Const("RLIMIT_SBSIZE", syscall.RLIMIT_SBSIZE)
		mProc.Const("RLIMIT_STACK", syscall.RLIMIT_STACK)

		mrb.DefineModuleFunc(mProc, "uid", os.Getuid)
		mrb.DefineModuleFunc(mProc, "uid=", syscall.Setuid)
		mrb.DefineModuleFunc(mProc, "gid", os.Getgid)
		mrb.DefineModuleFunc(mProc, "gid=", syscall.Setgid)
		mrb.DefineModuleFunc(mProc, "euid", syscall.Geteuid)
		mrb.DefineModuleFunc(mProc, "euid=", syscall.Seteuid)
		mrb.DefineModuleFunc(mProc, "egid", syscall.Getegid)
		mrb.DefineModuleFunc(mProc, "egid=", syscall.Setegid)
		mrb.DefineModuleFunction(mProc, "initgroups", proc_initgroups, 2)
		mrb.DefineModuleFunction(mProc, "groups", proc_getgroups, 0)
		mrb.DefineModuleFunction(mProc, "groups=", proc_setgroups, 1)
		mrb.DefineModuleFunction(mProc, "maxgroups",  proc_getmaxgroups, 0)
		mrb.DefineModuleFunction(mProc, "maxgroups=", proc_setmaxgroups, 1)
		mrb.DefineModuleFunction(mProc, "daemon", proc_daemon, -1)
		mrb.DefineModuleFunction(mProc, "times", rb_proc_times, 0)

		mrb.DefineConst(mProc, "CLOCK_REALTIME", CLOCKID2NUM(CLOCK_REALTIME))
		mrb.DefineConst(mProc, "CLOCK_REALTIME", RUBY_GETTIMEOFDAY_BASED_CLOCK_REALTIME)
		mrb.DefineConst(mProc, "CLOCK_MONOTONIC", CLOCKID2NUM(CLOCK_MONOTONIC))
		mrb.DefineConst(mProc, "CLOCK_MONOTONIC", RUBY_MACH_ABSOLUTE_TIME_BASED_CLOCK_MONOTONIC)
		mrb.DefineConst(mProc, "CLOCK_PROCESS_CPUTIME_ID", CLOCKID2NUM(CLOCK_PROCESS_CPUTIME_ID))
		mrb.DefineConst(mProc, "CLOCK_PROCESS_CPUTIME_ID", RUBY_GETRUSAGE_BASED_CLOCK_PROCESS_CPUTIME_ID)
		mrb.DefineConst(mProc, "CLOCK_THREAD_CPUTIME_ID", CLOCKID2NUM(CLOCK_THREAD_CPUTIME_ID))
		mrb.DefineConst(mProc, "CLOCK_PROF", CLOCKID2NUM(CLOCK_PROF))
		mrb.DefineConst(mProc, "CLOCK_REALTIME_FAST", CLOCKID2NUM(CLOCK_REALTIME_FAST))
		mrb.DefineConst(mProc, "CLOCK_REALTIME_PRECISE", CLOCKID2NUM(CLOCK_REALTIME_PRECISE))
		mrb.DefineConst(mProc, "CLOCK_REALTIME_COARSE", CLOCKID2NUM(CLOCK_REALTIME_COARSE))
		mrb.DefineConst(mProc, "CLOCK_REALTIME_ALARM", CLOCKID2NUM(CLOCK_REALTIME_ALARM))
		mrb.DefineConst(mProc, "CLOCK_MONOTONIC_FAST", CLOCKID2NUM(CLOCK_MONOTONIC_FAST))
		mrb.DefineConst(mProc, "CLOCK_MONOTONIC_PRECISE", CLOCKID2NUM(CLOCK_MONOTONIC_PRECISE))
		mrb.DefineConst(mProc, "CLOCK_MONOTONIC_RAW", CLOCKID2NUM(CLOCK_MONOTONIC_RAW))
		mrb.DefineConst(mProc, "CLOCK_MONOTONIC_RAW_APPROX", CLOCKID2NUM(CLOCK_MONOTONIC_RAW_APPROX))
		mrb.DefineConst(mProc, "CLOCK_MONOTONIC_COARSE", CLOCKID2NUM(CLOCK_MONOTONIC_COARSE))
		mrb.DefineConst(mProc, "CLOCK_BOOTTIME", CLOCKID2NUM(CLOCK_BOOTTIME))
		mrb.DefineConst(mProc, "CLOCK_BOOTTIME_ALARM", CLOCKID2NUM(CLOCK_BOOTTIME_ALARM))
		mrb.DefineConst(mProc, "CLOCK_UPTIME", CLOCKID2NUM(CLOCK_UPTIME))
		mrb.DefineConst(mProc, "CLOCK_UPTIME_FAST", CLOCKID2NUM(CLOCK_UPTIME_FAST))
		mrb.DefineConst(mProc, "CLOCK_UPTIME_PRECISE", CLOCKID2NUM(CLOCK_UPTIME_PRECISE))
		mrb.DefineConst(mProc, "CLOCK_UPTIME_RAW", CLOCKID2NUM(CLOCK_UPTIME_RAW))
		mrb.DefineConst(mProc, "CLOCK_UPTIME_RAW_APPROX", CLOCKID2NUM(CLOCK_UPTIME_RAW_APPROX))
		mrb.DefineConst(mProc, "CLOCK_SECOND", CLOCKID2NUM(CLOCK_SECOND))
		mrb.DefineConst(mProc, "CLOCK_TAI", CLOCKID2NUM(CLOCK_TAI))

		mrb.DefineModuleFunction(mProc, "clock_gettime", rb_clock_gettime, -1)
		mrb.DefineModuleFunction(mProc, "clock_getres", rb_clock_getres, -1)

		//cProcessTms = rb_struct_define_under(mProc, "Tms", "utime", "stime", "cutime", "cstime", NULL)
		/* An obsolete name of Process::Tms for backward compatibility */
		//mrb.DefineConst(rb_cStruct, "Tms", rb_cProcessTms)
		//rb_deprecate_constant(rb_cStruct, "Tms")
		mProcUID := mrb.DefineModuleUnder(mProc, "UID")
		mProcGID := mrb.DefineModuleUnder(mProc, "GID")
		mrb.DefineModuleFunc(mProcUID, "rid", os.Getuid)
		mrb.DefineModuleFunc(mProcGID, "gid", os.Getgid)
		mrb.DefineModuleFunc(mProcUID, "eid",  os.Geteuid)
		mrb.DefineModuleFunc(mProcGID, "gid", os.Getegid)
		mrb.DefineModuleFunction(mProcUID, "change_privilege", p_uid_change_privilege, 1)
		mrb.DefineModuleFunction(mProcGID, "change_privilege", p_gid_change_privilege, 1)
		mrb.DefineModuleFunc(mProcUID, "grant_privilege", syscall.Seteuid)
		mrb.DefineModuleFunc(mProcGID, "grant_privilege", syscall.Seteuid)
		mrb.DefineAlias(mrb.SingletonClass(mProcUID), "eid=", "grant_privilege")
		mrb.DefineAlias(mrb.SingletonClass(mProcGID), "eid=", "grant_privilege")
		mrb.DefineModuleFunction(mProcUID, "re_exchange", p_uid_exchange, 0)
		mrb.DefineModuleFunction(mProcGID, "re_exchange", p_gid_exchange, 0)
		mrb.DefineModuleFunction(mProcUID, "re_exchangeable?", p_uid_exchangeable, 0)
		mrb.DefineModuleFunction(mProcGID, "re_exchangeable?", p_gid_exchangeable, 0)
		mrb.DefineModuleFunction(mProcUID, "sid_available?",  p_uid_have_saved_id, 0)
		mrb.DefineModuleFunction(mProcGID, "sid_available?", p_gid_have_saved_id, 0)
		mrb.DefineModuleFunction(mProcUID, "switch", p_uid_switch, 0)
		mrb.DefineModuleFunction(mProcGID, "switch", p_gid_switch, 0)
		mrb.DefineModuleFunction(mProcUID, "from_name", p_uid_from_name, 1)
		mrb.DefineModuleFunction(mProcGID, "from_name", p_gid_from_name, 1)

		mSys := mrb.DefineModuleUnder(mProc, "Sys")
		mrb.DefineModuleFunc(mSys, "getuid",  os.Getuid)
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
	})
}

func procSpawn(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetAllArgs())
	defer runner.cleanup()

	pid, err := runner.run()
	if err != nil {
		return mrb.ERuntimeError().RaiseError(err)
	}
	return mrb.FixnumValue(pid)
}

func procWait(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pid, flags := mrb.GetArgs2(-1, 0)
	lastState := &status{}

	ret, err := platformWait(pid.Int(), flags.Int(), lastState)
	if err != nil {
		return mrb.ERuntimeError().RaiseError(err)
	}

	mrb.SetGV("$?", mrb.Value(lastState))

	return mrb.FixnumValue(ret)
}

func procDetach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pid := mrb.GetArgsFirst().Int()
	p, err := os.FindProcess(pid)
	if err != nil {
		return mrb.NilValue()
	}

	if p.Release() != nil{
		return mrb.NilValue()
	}
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
	case <-mrb.runChan:
		break
	case <-time.After(duration * time.Millisecond):
		break
	}

	return mrb.NilValue()
}
