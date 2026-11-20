package process

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"golang.org/x/sys/unix"
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
	mrb.DefineModuleFunction(mProc, "getpgid", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		pid, err := rb.Int(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		pgid, err := syscall.Getpgid(int(pid))
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.FixnumValue(pgid)
	}, mrb.ArgsReq(1))
	mrb.DefineModuleFunc(mProc, "setpgid", syscall.Setpgid)
	mrb.DefineModuleFunc(mProc, "setsid", syscall.Setsid)
	mrb.DefineModuleFunction(mProc, "getpriority", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		which, err := rb.Int(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		who, err := rb.Int(mrb, a[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		prio, err := syscall.Getpriority(int(which), int(who))
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.FixnumValue(prio)
	}, mrb.ArgsReq(2))
	mrb.DefineModuleFunc(mProc, "setpriority", func(which, who, prio int) (int, error) {
		err := syscall.Setpriority(which, who, prio)
		if err != nil {
			return 0, err
		}
		return 0, nil
	})

	mProc.Const("PRIO_PROCESS", syscall.PRIO_PROCESS)
	mProc.Const("PRIO_PGRP", syscall.PRIO_PGRP)
	mProc.Const("PRIO_USER", syscall.PRIO_USER)

	initRlimit(mrb, mProc)

	mrb.DefineModuleFunction(mProc, "uid=", setUIDFunc(syscall.Setuid), mrb.ArgsReq(1))
	mrb.DefineModuleFunction(mProc, "gid=", setGIDFunc(syscall.Setgid), mrb.ArgsReq(1))
	mrb.DefineModuleFunction(mProc, "euid=", setUIDFunc(syscall.Seteuid), mrb.ArgsReq(1))
	mrb.DefineModuleFunction(mProc, "egid=", setGIDFunc(syscall.Setegid), mrb.ArgsReq(1))
	mrb.DefineModuleFunction(mProc, "groups=", procSetGroups, mrb.ArgsReq(1))

	mrb.DefineModuleFunction(mProc, "times", procTimes, mrb.ArgsNone())

	initClock(mrb, mProc)

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

func resolveUID(mrb *oruby.MrbState, v oruby.Value) (int, oruby.MrbValue) {
	if v.IsString() {
		u, err := user.Lookup(v.String())
		if err != nil {
			return 0, mrb.Raise(mrb.EArgumentError(), "can't find user for "+v.String())
		}
		id, err := strconv.Atoi(u.Uid)
		if err != nil {
			return 0, mrb.Raise(mrb.EArgumentError(), err.Error())
		}
		return id, nil
	}
	id, err := rb.Int(mrb, v)
	if err != nil {
		return 0, rb.TypeError(mrb, err)
	}
	return int(id), nil
}

func resolveGID(mrb *oruby.MrbState, v oruby.Value) (int, oruby.MrbValue) {
	if v.IsString() {
		g, err := user.LookupGroup(v.String())
		if err != nil {
			return 0, mrb.Raise(mrb.EArgumentError(), "can't find group for "+v.String())
		}
		id, err := strconv.Atoi(g.Gid)
		if err != nil {
			return 0, mrb.Raise(mrb.EArgumentError(), err.Error())
		}
		return id, nil
	}
	id, err := rb.Int(mrb, v)
	if err != nil {
		return 0, rb.TypeError(mrb, err)
	}
	return int(id), nil
}

func setUIDFunc(fn func(int) error) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		arg := mrb.GetArgsFirst()
		id, exc := resolveUID(mrb, arg)
		if exc != nil {
			return exc
		}
		if err := fn(id); err != nil {
			return mrb.SysFail(err)
		}
		return arg
	}
}

func setGIDFunc(fn func(int) error) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		arg := mrb.GetArgsFirst()
		id, exc := resolveGID(mrb, arg)
		if exc != nil {
			return exc
		}
		if err := fn(id); err != nil {
			return mrb.SysFail(err)
		}
		return arg
	}
}

func procSetGroups(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgsFirst()
	if !arg.IsArray() {
		return mrb.Raisef(mrb.ETypeError(), "no implicit conversion of %s into Array", mrb.ClassOf(arg).Name())
	}
	items := oruby.RArraySlice(arg)
	gids := make([]int, len(items))
	for i, it := range items {
		id, exc := resolveGID(mrb, it)
		if exc != nil {
			return exc
		}
		gids[i] = id
	}
	if err := syscall.Setgroups(gids); err != nil {
		return mrb.SysFail(err)
	}
	return arg
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
		runner.umaskSet = true
	}

	if runner.pgroup != nil {
		// setpgid(0, pgid) in the child: 0 makes a new group (no new session)
		if runner.cmd.SysProcAttr == nil {
			runner.cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		runner.cmd.SysProcAttr.Pgid = *runner.pgroup
		runner.cmd.SysProcAttr.Setpgid = true
	}

	runner.cleanup = func() {
		if runner.umaskSet {
			syscall.Umask(runner.oldUmask)
			runner.umaskSet = false
		}
	}
}

func dupFd(fd int) (int, error) { return unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0) }

// execPrepare does in this process what spawn does in the child before
// exec(2) (MRI's rb_execarg_run_options): chdir, the standard streams and
// the Integer fd keys. The function it returns undoes the chdir and the
// standard streams when exec fails; an fd kept by {fd => fd} stays
// inheritable, as in MRI.
func (runner *cmdRunner) execPrepare() (func(), error) {
	var undo []func()
	restore := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	if dir := runner.cmd.Dir; dir != "" {
		old, err := os.Getwd()
		if err != nil {
			return restore, err
		}
		if err := syscall.Chdir(dir); err != nil {
			return restore, err
		}
		undo = append(undo, func() { _ = syscall.Chdir(old) })
	}
	for i, s := range []interface{}{runner.cmd.Stdin, runner.cmd.Stdout, runner.cmd.Stderr} {
		f, ok := s.(*os.File)
		if !ok || f == nil {
			continue
		}
		fd := rawFd(f)
		if fd < 0 || fd == i {
			continue
		}
		saved, err := dupFd(i)
		if err != nil {
			return restore, err
		}
		if err := unix.Dup2(fd, i); err != nil {
			_ = syscall.Close(saved)
			return restore, err
		}
		target := i
		undo = append(undo, func() { _ = unix.Dup2(saved, target); _ = syscall.Close(saved) })
	}
	if runner.closeOthers {
		undo = append(undo, closeOthers(runner.fdMap))
	}
	for child, parent := range runner.fdMap {
		if child == parent {
			if _, err := unix.FcntlInt(uintptr(child), unix.F_SETFD, 0); err != nil {
				return restore, err
			}
			continue
		}
		if err := unix.Dup2(parent, child); err != nil {
			return restore, err
		}
	}
	return restore, nil
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

// closeOthers is close_others: true: every descriptor >= 3 the child would
// inherit gets close-on-exec for the start (Go's fork has no hook in the
// child); the function returned clears the flag again. keep: descriptors
// the child is given ({fd => fd}).
func closeOthers(keep map[int]int) func() {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return func() {}
	}
	var marked []int
	for _, e := range entries {
		fd, err := strconv.Atoi(e.Name())
		if err != nil || fd < 3 {
			continue
		}
		if _, ok := keep[fd]; ok {
			continue
		}
		fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil || fl&unix.FD_CLOEXEC != 0 {
			continue // closed (the directory's own) or closing already
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, fl|unix.FD_CLOEXEC); err == nil {
			marked = append(marked, fd)
		}
	}
	return func() {
		for _, fd := range marked {
			if fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
				_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFD, fl&^unix.FD_CLOEXEC)
			}
		}
	}
}

func (runner *cmdRunner) run() (int, error) {
	if runner.err != nil {
		// a redirection target or chdir directory that is not there
		return 0, runner.err
	}
	if runner.closeOthers {
		defer closeOthers(nil)()
	}
	err := runner.cmd.Start()
	if errors.Is(err, syscall.ENOEXEC) {
		// an executable with no #! line: MRI runs it with /bin/sh
		c := runner.cmd
		sh := &exec.Cmd{
			Path: platformGetShell(), Args: append([]string{"sh", c.Path}, c.Args[1:]...),
			Env: c.Env, Dir: c.Dir, Stdin: c.Stdin, Stdout: c.Stdout, Stderr: c.Stderr,
			ExtraFiles: c.ExtraFiles, SysProcAttr: c.SysProcAttr,
		}
		runner.cmd = sh
		err = sh.Start()
	}
	if err != nil {
		return 0, err
	}

	//TODO: ptrace with runner.cmd.SysProcAttr.Ptrace
	return runner.cmd.Process.Pid, err
}

// platformGetShell: MRI runs a command string with /bin/sh, whatever
// $SHELL says, as "sh" ($0)
func platformGetShell() string { return "/bin/sh" }

func shellArgv0() string { return "sh" }

// execCmd is exec(2) of the command, with /bin/sh for an executable that
// has no #! line (ENOEXEC), as MRI does
func execCmd(path string, args, env []string) error {
	err := syscall.Exec(path, args, env)
	if errors.Is(err, syscall.ENOEXEC) {
		err = syscall.Exec(platformGetShell(), append([]string{"sh", path}, args[1:]...), env)
	}
	return err
}

// shellWords is MRI's rb_exec_fillarg test: a command string without shell
// metacharacters, whose first word is no reserved word, special builtin or
// variable assignment, runs directly, split on blanks. ok false: it needs
// the shell.
func shellWords(s string) ([]string, bool) {
	if strings.ContainsAny(s, "*?{}[]<>()~&|\\$;'`\"\n#") {
		return nil, false
	}
	words := strings.Fields(s)
	if len(words) > 0 {
		if strings.Contains(words[0], "=") || shellReserved[words[0]] {
			return nil, false
		}
	}
	return words, true
}

var shellReserved = map[string]bool{
	"case": true, "do": true, "done": true, "elif": true, "else": true, "esac": true, "fi": true,
	"for": true, "if": true, "in": true, "then": true, "until": true, "while": true,
	"!": true, ".": true, ":": true, "break": true, "continue": true, "eval": true, "exec": true,
	"exit": true, "export": true, "readonly": true, "return": true, "set": true, "shift": true,
	"times": true, "trap": true, "unset": true,
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
	err := syscall.Kill(pid, syscall.Signal(sig))
	// a SIGKILL sent to this process: kill(2) delivers it before it returns
	// to a single-threaded caller, here another thread may take it, so wait
	// for it rather than run on (Process.kill(:KILL, $$); exit(42))
	if err == nil && syscall.Signal(sig) == syscall.SIGKILL && (pid == os.Getpid() || pid == 0 || pid == -syscall.Getpgrp()) {
		time.Sleep(time.Second)
	}
	return err
}

// platformSelfTarget: kill(2) to pid reaches this process
func platformSelfTarget(pid int) bool {
	return pid == os.Getpid() || pid == 0 || pid == -1 || pid == -syscall.Getpgrp()
}
