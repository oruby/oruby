package process

import (
	"fmt"
	"math"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/oruby/oruby"
	//_ "github.com/oruby/oruby/gem/thread"
	_ "github.com/oruby/oruby/gem/signal"
)

type processData struct {
	savedUserID int
	wakeupChan  chan struct{}
	tms         oruby.RClass
}

func init() {
	oruby.Gem("process", func(mrb *oruby.MrbState) interface{} {
		// Require thread, ignore if it is not included in gems
		//mrb.Require("thread")
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

		tmsV := mrb.Class("Struct").Call("new",
			mrb.Intern("utime"),
			mrb.Intern("stime"),
			mrb.Intern("cutime"),
			mrb.Intern("cstime"),
		)

		mProc := mrb.DefineGoModule("Process", &processData{
			savedUserID: os.Geteuid(),
			wakeupChan:  make(chan struct{}),
			tms: mrb.ClassPtr(tmsV.Value()),
		})

		mProc.Const("WNOHANG",   1)
		mProc.Const("WUNTRACED", 2)

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
		mrb.DefineModuleFunction(mProc, "initgroups", procInitgroups, mrb.ArgsArg(1,1))

		mProcUID := mrb.DefineModuleUnder(mProc, "UID")
		mProcGID := mrb.DefineModuleUnder(mProc, "GID")
		mrb.DefineModuleFunc(mProcUID, "rid", os.Getuid)
		mrb.DefineModuleFunc(mProcGID, "gid", os.Getgid)
		mrb.DefineModuleFunc(mProcUID, "eid", os.Geteuid)
		mrb.DefineModuleFunc(mProcGID, "gid", os.Getegid)

		mrb.DefineModuleFunc(mProcUID, "from_name", func(name string)(int,error) {
			g, err := user.Lookup(name)
			if err != nil {
				return -1, oruby.EArgumentError(err.Error())
			}
			gid, err := strconv.Atoi(g.Gid)
			if err != nil {
				return -1, oruby.EArgumentError(err.Error())
			}
			return gid, nil
		})

		mrb.DefineModuleFunc(mProcGID, "from_name", func(name string)(int,error) {
			g, err := user.LookupGroup(name)
			if err != nil {
				return -1, oruby.EArgumentError(err.Error())
			}
			gid, err := strconv.Atoi(g.Gid)
			if err != nil {
				return -1, oruby.EArgumentError(err.Error())
			}
			return gid, nil
		})

		mSys := mrb.DefineModuleUnder(mProc, "Sys")
		mrb.DefineModuleFunc(mSys, "getuid", os.Getuid)
		mrb.DefineModuleFunc(mSys, "geteuid", os.Geteuid)
		mrb.DefineModuleFunc(mSys, "getgid", os.Getgid)
		mrb.DefineModuleFunc(mSys, "getegid", os.Getegid)

		initPlatform(mrb, mProc, mProcUID, mProcGID, mSys)
		return nil
	})
}

func procInitgroups(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name, gid := mrb.GetArgs2()
	if !name.IsString() {
		return mrb.EArgumentError().Raisef("invalid user name: %v", mrb.String(name))
	}

	u, err := user.Lookup(name.String())
	if err != nil {
		return mrb.SysFail(err)
	}
	ret := mrb.AryNewFromValues(gid)
	isWin := runtime.GOOS == "windows"
	grps, err := u.GroupIds()
	if err != nil {
		return mrb.SysFail(err)
	}

	for _, grp := range grps {
		if isWin {
			ret.PushString(grp)
		} else {
			grpid, err := strconv.Atoi(grp)
			if err != nil {
				// if some Unix variant decides for string groups, add as string
				ret.PushString(grp)
				continue
			}

			ret.PushInt(grpid)
		}
	}
	return ret
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
			return mrb.SysFail(err)
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
		duration = time.Duration(int64(t.Float64() * 1000000000))
	}

	procData := mrb.GetModuleData(self).(*processData)

	select {
	case <-mrb.ExitChan():
		break
	case <- procData.wakeupChan:
		break
	case <-time.After(duration * time.Nanosecond):
		break
	}

	return mrb.NilValue()
}
