package process

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"sync"
	"syscall"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	//_ "github.com/oruby/oruby/gem/thread"
	_ "github.com/oruby/oruby/gem/signal"
)

//go:embed process.rb
var processRb []byte

type processData struct {
	sync.Mutex
	runners     map[int]*cmdRunner
	savedUserID int
	maxGroups   int
	wakeupChan  chan struct{}
	tms         oruby.RClass
}

func (procData *processData) getRunner(pid int) *cmdRunner {
	procData.Lock()
	runner, ok := procData.runners[pid]
	procData.Unlock()
	if ok && runner.cmd.ProcessState == nil {
		return runner
	}
	return nil
}

// forget drops the runner of pid, a child reaped by wait(2) itself: its
// exec.Cmd can no longer wait for it
func (procData *processData) forget(pid int) {
	if pid <= 0 {
		return
	}
	procData.Lock()
	delete(procData.runners, pid)
	procData.Unlock()
}

func init() {
	oruby.Gem("process", func(mrb *oruby.MrbState) interface{} {
		// Require thread, ignore if it is not included in gems
		//mrb.Require("thread")
		mrb.Require("signal")

		mrb.SetGV("$$", os.Getpid())
		mrb.SetGV("$?", nil) // last_status

		mrb.DefineGlobalFunction("exec", procExec, mrb.ArgsAny())
		// Unsupported fork shoud not respond to 'fork' method
		//mrb.DefineGlobalFunction("fork", procFork, mrb.ArgsAny())
		mrb.DefineGlobalFunction("exit!", procExitBang, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("system", procSystem, mrb.ArgsAny())
		mrb.DefineGlobalFunction("`", procBacktick, mrb.ArgsAny())
		mrb.DefineModuleFunction(mrb.KernelModule(), "`", procBacktick, mrb.ArgsAny())
		mrb.DefineGlobalFunction("spawn", procSpawn, mrb.ArgsReq(1)+mrb.ArgsRest())
		mrb.DefineGlobalFunction("sleep", oruby.KernelSleep, mrb.ArgsAny())
		mrb.DefineGlobalFunction("exit", procExit, mrb.ArgsOpt(1))
		mrb.DefineGlobalFunction("abort", procAbort, mrb.ArgsOpt(1))

		tmsV := mrb.Class("Struct").Call("new",
			oruby.Sym_utime,
			oruby.Sym_stime,
			oruby.Sym_cutime,
			oruby.Sym_cstime,
		)

		mProcData := &processData{
			savedUserID: os.Geteuid(),
			maxGroups:   32,
			wakeupChan:  make(chan struct{}),
			tms:         mrb.ClassPtr(tmsV.Value()),
			runners:     make(map[int]*cmdRunner, 10),
		}

		mProc := mrb.DefineGoModule("Process", mProcData)
		mProc.Const("Tms", tmsV)

		mProc.Const("WNOHANG", 1)
		mProc.Const("WUNTRACED", 2)

		mrb.DefineClassMethod(mProc, "exec", procExec, mrb.ArgsAny())
		mrb.DefineClassMethod(mProc, "_fork", mrb.NotImplemented, mrb.ArgsNone())
		mrb.DefineClassMethod(mProc, "daemon", mrb.NotImplemented, mrb.ArgsOpt(2))
		mrb.DefineClassMethod(mProc, "warmup", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return mrb.TrueValue()
		}, mrb.ArgsNone())
		mrb.DefineClassMethod(mProc, "setproctitle", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			s, err := rb.Str(mrb, mrb.GetArgsFirst())
			if err != nil {
				return rb.TypeError(mrb, err)
			}
			v := mrb.StrNew(s)
			mrb.SetGV("$0", v)
			return v
		}, mrb.ArgsReq(1))
		mrb.DefineClassMethod(mProc, "maxgroups", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.FixnumValue(getProcData(mrb, self).maxGroups)
		}, mrb.ArgsNone())
		mrb.DefineClassMethod(mProc, "maxgroups=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			n, err := rb.Int(mrb, mrb.GetArgsFirst())
			if err != nil {
				return rb.TypeError(mrb, err)
			}
			if n <= 0 {
				return mrb.Raise(mrb.EArgumentError(), "maxgroups must be positive")
			}
			getProcData(mrb, self).maxGroups = int(n)
			return mrb.FixnumValue(int(n))
		}, mrb.ArgsReq(1))
		mrb.DefineClassMethod(mProc, "spawn", procSpawn, mrb.ArgsReq(1)+mrb.ArgsRest())
		mrb.DefineClassMethod(mProc, "exit!", procExitBang, mrb.ArgsOpt(1))
		mrb.DefineClassMethod(mProc, "exit", procExit, mrb.ArgsOpt(1))
		mrb.DefineClassMethod(mProc, "abort", procAbort, mrb.ArgsOpt(1))
		mrb.DefineClassMethod(mProc, "last_status", procLastStatus, mrb.ArgsNone())
		mrb.DefineClassMethod(mProc, "__oruby_kill__", procKill, mrb.ArgsReq(2))
		mrb.DefineClassMethod(mProc, "wait", procWait, mrb.ArgsOpt(2))
		mrb.DefineClassMethod(mProc, "wait2", procWait2, mrb.ArgsOpt(2))
		mrb.DefineClassMethod(mProc, "waitpid", procWait, mrb.ArgsOpt(2))
		mrb.DefineClassMethod(mProc, "waitpid2", procWait2, mrb.ArgsOpt(2))
		mrb.DefineClassMethod(mProc, "waitall", procWaitall, mrb.ArgsNone())
		mrb.DefineClassMethod(mProc, "detach", procDetach, mrb.ArgsReq(1))
		mrb.DefineClassMethod(mProc, "_get_cmd", procGetCmd, mrb.ArgsAny())

		if mrb.ClassDefined("Thread") {
			cWaiter := mrb.DefineClassUnder(mProc, "Waiter", mrb.ClassGet("Thread"))
			cWaiter.UndefClassMethod("new")
			//cWaiter.DefineMethod("pid", detachProcessPID, mrb.ArgsNone())
		}

		initStatus(mProc)

		mrb.DefineModuleFunc(mProc, "pid", os.Getpid)
		mrb.DefineModuleFunc(mProc, "ppid", os.Getppid)

		setConsts(mProc)

		mrb.DefineModuleFunc(mProc, "uid", os.Getuid)
		mrb.DefineModuleFunc(mProc, "gid", os.Getgid)
		mrb.DefineModuleFunc(mProc, "euid", os.Geteuid)
		mrb.DefineModuleFunc(mProc, "egid", os.Getegid)
		mrb.DefineModuleFunc(mProc, "groups", procGroups)
		mrb.DefineModuleFunction(mProc, "initgroups", procInitgroups, mrb.ArgsArg(1, 1))

		mProcUID := mrb.DefineModuleUnder(mProc, "UID")
		mProcGID := mrb.DefineModuleUnder(mProc, "GID")
		mrb.DefineModuleFunc(mProcUID, "rid", os.Getuid)
		mrb.DefineModuleFunc(mProcGID, "rid", os.Getgid)
		mrb.DefineModuleFunc(mProcUID, "eid", os.Geteuid)
		mrb.DefineModuleFunc(mProcGID, "eid", os.Getegid)

		mrb.DefineModuleFunc(mProcUID, "from_name", func(name string) (int, error) {
			u, err := user.Lookup(name)
			if err != nil {
				return -1, oruby.EArgumentError("%v", err)
			}
			uid, err := strconv.Atoi(u.Uid)
			if err != nil {
				return -1, oruby.EArgumentError("%v", err)
			}
			return uid, nil
		})

		mrb.DefineModuleFunc(mProcGID, "from_name", func(name string) (int, error) {
			g, err := user.LookupGroup(name)
			if err != nil {
				return -1, oruby.EArgumentError("%v", err)
			}
			gid, err := strconv.Atoi(g.Gid)
			if err != nil {
				return -1, oruby.EArgumentError("%v", err)
			}
			return gid, nil
		})

		mSys := mrb.DefineModuleUnder(mProc, "Sys")
		mrb.DefineModuleFunc(mSys, "getuid", os.Getuid)
		mrb.DefineModuleFunc(mSys, "geteuid", os.Geteuid)
		mrb.DefineModuleFunc(mSys, "getgid", os.Getgid)
		mrb.DefineModuleFunc(mSys, "getegid", os.Getegid)

		initPlatform(mrb, mProc, mProcUID, mProcGID, mSys)
		rb.LoadRuby(mrb, "<internal:process>", processRb)

		return mProcData
	})
}

func getProcData(mrb *oruby.MrbState, self oruby.Value) *processData {
	if procData, ok := mrb.GetModuleData(self).(*processData); ok {
		return procData
	}

	mProc := mrb.ModuleGet("Process").Value()
	return mrb.GetModuleData(mProc).(*processData)
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

	gids := []int{}
	if gidVal, err := rb.Int(mrb, gid); err == nil {
		gids = append(gids, int(gidVal))
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
			gids = append(gids, grpid)
			ret.PushInt(grpid)
		}
	}
	if !isWin {
		if os.Geteuid() != 0 {
			return mrb.SysFail(syscall.EPERM)
		}
		if err := syscall.Setgroups(gids); err != nil {
			return mrb.SysFail(err)
		}
	}
	return ret
}

func errorState(err error) *status {
	return &status{
		Pid:        0,
		Exitstatus: 127,
		IsSucess:   false,
		IsExited:   true,
		err:        err,
	}
}

func procExec(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetArgs())
	defer runner.cleanup()
	if runner.argErr != nil {
		return mrb.RaiseError(runner.argErr)
	}
	path := runner.cmd.Path
	if path == "" {
		return raiseErrno(mrb, syscall.ENOENT, path)
	}
	if runner.err != nil {
		// a redirection target that could not be opened, a missing chdir
		return raiseExecErr(mrb, runner.err, path)
	}
	restore, err := runner.execPrepare()
	if err == nil {
		err = execCmd(path, runner.cmd.Args, runner.cmd.Env)
	}
	restore()
	setStatus(mrb, errorState(err))
	if errno, ok := err.(syscall.Errno); ok {
		return raiseErrno(mrb, errno, path)
	}
	return mrb.RaiseError(err)
}

func procSystem(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetArgs())
	defer runner.cleanup()
	if runner.argErr != nil {
		return mrb.RaiseError(runner.argErr)
	}

	pid, err := runner.run()
	if err != nil {
		// the command could not run: nil, or its Errno with exception: true
		setStatus(mrb, errorState(err))
		if runner.exception {
			return raiseExecErr(mrb, err, runner.cmd.Path)
		}
		return mrb.NilValue()
	}

	ret, state := runner.Wait(pid, 0)

	// Error
	if !ret.IsInteger() {
		if runner.exception {
			setStatus(mrb, errorState(runner.err))
			return mrb.NilValue()
		}
		if mrb.Exc() != nil {
			setStatus(mrb, errorState(mrb.Err()))
			mrb.ExcClear()
		}
		return mrb.NilValue()
	}

	if state.Exitstatus == 127 {
		return mrb.NilValue()
	}
	if runner.exception && !state.IsSucess {
		// MRI: RuntimeError naming the command and how it ended
		how := fmt.Sprintf("exit %d", state.Exitstatus)
		if state.IsSignaled && state.Termsig != nil {
			how = fmt.Sprintf("signal %d", *state.Termsig)
		}
		return mrb.ERuntimeError().Raisef("Command failed with %s: %s", how, runner.cmdline)
	}
	return mrb.BoolValue(state.Exitstatus == 0)
}

func procBacktick(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetArgs())
	defer runner.cleanup()
	if runner.argErr != nil {
		return mrb.RaiseError(runner.argErr)
	}

	var stdout bytes.Buffer
	runner.cmd.Stdout = &stdout

	pid, err := runner.run()
	if err != nil {
		setStatus(mrb, errorState(err))
		return raiseExecErr(mrb, err, runner.cmd.Path)
	}

	ret, _ := runner.Wait(pid, 0)

	// Error
	if !ret.IsInteger() {
		if runner.exception {
			setStatus(mrb, errorState(runner.err))
			return mrb.NilValue()
		}
		if mrb.Exc() != nil {
			setStatus(mrb, errorState(mrb.Err()))
			mrb.ExcClear()
		}
		return mrb.NilValue()
	}

	// the output is text in the default external encoding
	out := mrb.StrNew(stdout.String())
	if enc, err := mrb.Funcall(mrb.EncodingClass(), oruby.Sym_default_external); err == nil && !enc.IsNil() {
		_, _ = mrb.Funcall(out, oruby.Sym_force_encoding, enc)
	}
	return out
}

func procGetCmd(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	runner := parseArgs(mrb, args)
	if runner.argErr != nil {
		return mrb.RaiseError(runner.argErr)
	}

	if !block.IsNil() {
		ret := mrb.YieldArgv(block, mrb.DataValue(runner.cmd))
		runner.cleanup()
		return ret
	}
	return mrb.DataValue(runner.cmd)
}

func procSpawn(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	runner := parseArgs(mrb, mrb.GetArgs())
	defer runner.cleanup()
	if runner.argErr != nil {
		return mrb.RaiseError(runner.argErr)
	}

	pid, err := runner.run()
	if err != nil {
		setStatus(mrb, errorState(err))
		return raiseExecErr(mrb, err, runner.cmd.Path)
	}

	procData := getProcData(mrb, self)
	procData.Lock()
	procData.runners[pid] = runner
	procData.Unlock()

	return mrb.FixnumValue(pid)
}

func setStatus(mrb *oruby.MrbState, state *status) {
	if state == nil {
		mrb.SetGV("$?", mrb.NilValue())
		return
	}
	mrb.SetGV("$?", mrb.Value(state))
}

func procWait(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pidV, flagsV := mrb.GetArgs2(-1, 0)
	procData := getProcData(mrb, self)
	pid := pidV.Int()
	flags := flagsV.Int()

	// a child spawned here, waited for without flags (exec.Cmd ignores
	// WNOHANG and the like)
	if pid > 0 && flags == 0 {
		runner := procData.getRunner(pid)
		if runner != nil {
			ret, _ := runner.Wait(runner.cmd.Process.Pid, flags)
			procData.forget(pid)
			return ret
		}
	}

	// any child (pid -1), one of a process group (0, < -1), or with flags:
	// wait(2), whichever child it is
	lastState := &status{Pid: pid}
	ret, err := blockingWait(mrb, pid, flags, lastState)
	if err == nil {
		procData.forget(ret)
	}
	if err == nil && ret == 0 {
		// WNOHANG and the child runs: nil, and $? is cleared (MRI)
		mrb.SetGV("$?", mrb.NilValue())
		return mrb.NilValue()
	}
	mrb.SetGV("$?", mrb.Value(lastState))

	if err != nil {
		return waitError(mrb, err)
	}

	return mrb.FixnumValue(ret)
}

// blockingWait waits for a child without the GVL: other Ruby threads run
func blockingWait(mrb *oruby.MrbState, pid, flags int, state *status) (ret int, err error) {
	mrb.Blocking(func() { ret, err = platformWait(pid, flags, state) })
	return ret, err
}

// waitError raises a failed wait as MRI does: Errno::ECHILD and the like
func waitError(mrb *oruby.MrbState, err error) oruby.MrbValue {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		exc, ferr := mrb.Funcall(mrb.ESystemCallError().Value(), oruby.Sym_new, nil, int(errno))
		if ferr == nil {
			return mrb.RaiseObj(exc)
		}
	}
	return mrb.ERuntimeError().RaiseError(err)
}

func procLastStatus(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.GetGV("$?")
}

func procWait2(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	pidV, flags := mrb.GetArgs2(-1, 0)
	procData := getProcData(mrb, self)
	pid := pidV.Int()
	ret := mrb.NilValue()
	var state *status

	if pid > 0 && flags.Int() == 0 {
		if runner := procData.getRunner(pid); runner != nil {
			ret, state = runner.Wait(pid, 0)
			procData.forget(pid)
		}
	}

	if ret.IsNil() {
		state = &status{Pid: pid}
		result, err := blockingWait(mrb, pid, flags.Int(), state)
		if err == nil {
			procData.forget(result)
		}
		if err == nil && result == 0 {
			// WNOHANG and the child runs: nil, and $? is cleared (MRI)
			mrb.SetGV("$?", mrb.NilValue())
			return mrb.NilValue()
		}
		mrb.SetGV("$?", mrb.Value(state))
		if err != nil {
			return waitError(mrb, err)
		}

		ret = mrb.FixnumValue(result)
	}

	if !ret.IsInteger() {
		return ret
	}

	return mrb.AryNewFromValues(ret, mrb.Value(state))
}

func procWaitall(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var pid int
	var err error
	var ret = mrb.AryNew()
	procData := getProcData(mrb, self)

	setStatus(mrb, nil)

	runnersCleared := false
	for pid >= 0 {
		if !runnersCleared {
			procData.Lock()
			keys := make([]*cmdRunner, 0, len(procData.runners))
			for _, v := range procData.runners {
				if v.cmd.ProcessState != nil {
					continue
				}
				keys = append(keys, v)
			}
			procData.Unlock()

			for _, runner := range keys {
				rret, state := runner.Wait(runner.cmd.Process.Pid, 0)
				if !rret.IsInteger() {
					return rret
				}
				ret.Push(mrb.AryNewFromValues(rret, mrb.Value(state)))
			}
			runnersCleared = true
		}

		lastState := &status{}
		pid, err = blockingWait(mrb, pid, 0, lastState)
		setStatus(mrb, lastState)

		if pid == -1 {
			break
		}
		if err != nil {
			return mrb.SysFail(err)
		}

		ret.Push(mrb.AryNewFromValues(mrb.FixnumValue(pid), mrb.Value(lastState)))
	}

	procData.Lock()
	procData.runners = make(map[int]*cmdRunner, 10)
	procData.Unlock()

	return ret
}

func procDetach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	var p *os.Process
	var err error
	pid := mrb.GetArgsFirst().Int()
	procData := getProcData(mrb, self)

	runner := procData.getRunner(pid)
	if runner != nil {
		procData.Lock()
		delete(procData.runners, pid)
		procData.Unlock()

		p = runner.cmd.Process
	}

	if p == nil {
		p, err = os.FindProcess(pid)
		if err != nil {
			return mrb.NilValue()
		}
	}

	if p.Release() != nil {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(pid)
}

// Process.__oruby_kill__(signo, pid): one kill(2) for Process.kill
// (process.rb); a negative signo signals the process group pid. Returns
// whether the signal reaches this process, which then handles it before
// Process.kill returns.
func procKill(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	sig := args.Item(0).Int()
	pid := args.Item(1).Int()
	target := pid
	if sig < 0 {
		sig, target = -sig, -pid
	}

	procData := getProcData(mrb, self)
	if runner := procData.getRunner(target); target > 0 && runner != nil {
		if err := runner.cmd.Process.Signal(syscall.Signal(sig)); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.FalseValue()
	}
	if err := platformKill(target, sig); err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.BoolValue(platformSelfTarget(target))
}

func procAbort(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	// as MRI: print and raise SystemExit(1), so ensure clauses, rescue and
	// at_exit still run (core_ext replaces this with the full MRI version)
	msg := mrb.GetArgsFirst().String()
	if msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	} else {
		msg = "exit"
		if mrb.Exc() != nil {
			fmt.Fprintln(os.Stderr, mrb.Err().Error())
		}
	}
	sysExitClass := mrb.ClassGet("SystemExit")
	exc, err := mrb.ObjNew(sysExitClass, 1, msg)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.RaiseObj(exc)
}

func procExit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	status := mrb.GetArgsFirst()
	code := 0
	switch status.Type() {
	case oruby.MrbTTTrue:
		code = 0
	case oruby.MrbTTFalse:
		if status.IsNil() {
			code = 0
		} else {
			code = 1
		}
	default:
		code = status.Int()
	}
	sysExitClass := mrb.ClassGet("SystemExit")
	if !sysExitClass.IsNil() {
		exc, err := mrb.ObjNew(sysExitClass, code)
		if err == nil {
			mrb.IVSet(exc, oruby.IvSym_status, mrb.FixnumValue(code))
			return mrb.RaiseObj(exc)
		}
	}
	mrb.RunAtExitHandlers()
	os.Exit(code)
	return mrb.NilValue()
}

func procExitBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	status := mrb.GetArgsFirst()
	code := 0
	switch status.Type() {
	case oruby.MrbTTTrue:
		code = 0
	case oruby.MrbTTFalse:
		if status.IsNil() {
			code = 0
		} else {
			code = 1
		}
	default:
		code = status.Int()
	}
	os.Exit(code)
	return mrb.NilValue()
}

// Kernel#sleep: signals that arrive meanwhile run their handlers, then the
// sleep goes on, as in MRI; one that raises ends it. Returns the seconds
// slept, rounded.
// raiseErrno raises Errno::EXXX "<message> - path", as MRI's exec does
func raiseErrno(mrb *oruby.MrbState, errno syscall.Errno, path string) oruby.MrbValue {
	exc, err := mrb.Funcall(mrb.ESystemCallError().Value(), oruby.Sym_new, path, int(errno))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.RaiseObj(exc)
}

// raiseExecErr: a command that did not start, as its Errno (ENOENT for a
// missing one)
func raiseExecErr(mrb *oruby.MrbState, err error, path string) oruby.MrbValue {
	// a file or directory the options name: its own path
	var pe *os.PathError
	if errors.As(err, &pe) && (pe.Op == "chdir" || pe.Op == "open" || pe.Op == "stat") {
		path = pe.Path
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return raiseErrno(mrb, errno, path)
	}
	if errors.Is(err, exec.ErrNotFound) || path == "" {
		return raiseErrno(mrb, syscall.ENOENT, path)
	}
	return mrb.RaiseError(err)
}

// procGroups is Process.groups: getgroups(2), which on macOS stops at 16
// groups unless built for unlimited ones (MRI is); the user's full group
// list there comes from getgrouplist (os/user)
func procGroups() ([]int, error) {
	gs, err := os.Getgroups()
	if err != nil || runtime.GOOS != "darwin" {
		return gs, err
	}
	u, uerr := user.Current()
	if uerr != nil {
		return gs, nil
	}
	ids, gerr := u.GroupIds()
	if gerr != nil {
		return gs, nil
	}
	seen := make(map[int]bool, len(gs))
	for _, g := range gs {
		seen[g] = true
	}
	for _, id := range ids {
		if g, cerr := strconv.Atoi(id); cerr == nil && !seen[g] {
			seen[g] = true
			gs = append(gs, g)
		}
	}
	return gs, nil
}
