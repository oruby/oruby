package process

import (
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/oruby/oruby"
)

type cmdRunner struct {
	mrb           *oruby.MrbState
	cmd           *exec.Cmd
	limits        limitsMap
	pgroup        *int
	umask         *int
	oldUmask      int
	closeOthers   bool
	unsetenvOther bool
	exception     bool
	cleanup       func()
	err           error
}

func parseArgs(mrb *oruby.MrbState, args oruby.RArgs) *cmdRunner {
	env, command, params, options := parseArgsStructure(mrb, args)

	// Create command
	runner := &cmdRunner{
		mrb: mrb,
		cmd: &exec.Cmd{
			Path:   command,
			Args:   params,
			Stdout: os.Stdout,
			Stdin:  os.Stdin,
			Stderr: os.Stderr,
		},
		closeOthers: true,
		cleanup:     func() {},
	}

	mrb.HashValueForEach(options, func(key, val oruby.Value) int {
		if key.IsInteger() || key.IsArray() || key.HasBasic() {
			runner.parseFd(key, val)
			return 0
		}

		k := mrb.String(key)

		if strings.HasPrefix(k, "rlimit_") {
			runner.parseRLimit(key, val)
			return 0
		}

		switch k {
		case "in", "out", "err":
			runner.parseFd(key, val)
		case "umask":
			v := val.Int()
			runner.umask = &v
		case "pgroup":
			v := val.Int()
			switch val.Type() {
			case oruby.MrbTTTrue:
				v = 0
			case oruby.MrbTTFalse:
				return 0
			}
			runner.pgroup = &v
		case "chdir":
			runner.cmd.Dir = val.String()
		case "close_others":
			runner.closeOthers = val.Bool()
		case "unsetenv_others":
			runner.unsetenvOther = val.Bool()
		case "exception":
			runner.exception = val.Bool()
		}

		return 0
	})

	// ENV
	runner.setENV(env)

	// Parse OS specific options
	runner.parseOptionsOS(options)

	return runner
}

// parseArgsStructure returns env, command, params and options from args
func parseArgsStructure(mrb *oruby.MrbState, args oruby.RArgs) (oruby.Value, string, []string, oruby.Value) {
	env := mrb.NilValue()
	options := mrb.KeywordArgs()
	command := args.Item(0)
	argStart := 1

	if command.IsHash() {
		env = command
		command = args.Item(1)
		argStart++
	}

	params := make([]string, 0, args.Len()-argStart+1)

	// Params: from first arg after command, till options (a trailing Hash)
	last := args.Len()
	if last > argStart && args.Item(last-1).IsHash() {
		last--
		mrb.HashMerge(options, args.Item(last))
	}
	for i := argStart; i < last; i++ {
		params = append(params, mrb.String(args.Item(i)))
	}

	var cmd string

	if command.IsArray() {
		// [command, argv0]: run command directly, argv[0] as given
		params = append([]string{mrb.AryRef(command, 1).String()}, params...)
		cmd = lookPath(mrb.AryRef(command, 0).String())
	} else if command.IsString() && len(params) > 0 {
		// command plus arguments: run directly, no shell (as MRI)
		params = append([]string{command.String()}, params...)
		cmd = lookPath(command.String())
	} else if command.IsString() {
		shell := platformGetShell()
		params = []string{shell, "-c", command.String()}
		cmd = shell
	} else {
		cmd = command.String()
		params = append([]string{cmd}, params...)
	}

	return env, cmd, params, options.Value()
}

// setENV sets Cmd Environment
func (runner *cmdRunner) setENV(env oruby.Value) {
	mrb := runner.mrb
	EnvIsHash := env.IsHash()

	if !EnvIsHash && !runner.unsetenvOther {
		runner.cmd.Env = os.Environ()
		return
	}

	seen := map[string]bool{}
	for _, v := range os.Environ() {
		kv := strings.SplitN(v, "=", 2)
		key := mrb.StrNew(kv[0])
		seen[kv[0]] = true
		if EnvIsHash && mrb.HashKeyP(env, key) {
			// Env has variable - add if not nil
			value := mrb.HashGet(env, key)
			if !value.IsNil() {
				runner.cmd.Env = append(runner.cmd.Env, kv[0]+"="+value.String())
			}
		} else if !runner.unsetenvOther {
			// Variable not in env, add respecting :unsetenv_other option
			runner.cmd.Env = append(runner.cmd.Env, v)
		}
	}
	if EnvIsHash {
		// variables the env Hash adds
		mrb.HashValueForEach(env, func(k, v oruby.Value) int {
			if name := mrb.String(k); !seen[name] && !v.IsNil() {
				runner.cmd.Env = append(runner.cmd.Env, name+"="+mrb.String(v))
			}
			return 0
		})
	}
}

func (runner *cmdRunner) parseRLimit(key, val oruby.Value) {
	if val.IsNil() {
		return
	}

	mrb := runner.mrb
	keySym := mrb.ObjToSym(key)

	// Add only if Process::RLIMIT_name is defined for system
	mProc := mrb.ModuleGet("Process")
	if mProc.ConstDefinedIDAt(keySym) {
		limit := mProc.ConstGetID(keySym)
		runner.checkLimitOption(limit.Int(), val)
	}
}

func (runner *cmdRunner) parseFd(key, val oruby.Value) {
	mrb := runner.mrb

	if key.IsInteger() {
		switch key.Int() {
		case 0:
			runner.cmd.Stdin = runner.getReader(val)
		case 1:
			runner.cmd.Stdout = runner.getWriter(val)
		case 2:
			runner.cmd.Stderr = runner.getWriter(val)
		}
		return
	}

	if key.IsSymbol() {
		switch key.Symbol() {
		case oruby.Sym_in:
			runner.cmd.Stdin = runner.getReader(val)
		case oruby.Sym_out:
			runner.cmd.Stdout = runner.getWriter(val)
		case oruby.Sym_err:
			runner.cmd.Stderr = runner.getWriter(val)
		}
		return
	}

	if key.IsData() {
		if file, ok := mrb.Data(val).(*os.File); ok {
			switch file {
			case os.Stdin:
				runner.cmd.Stdin = runner.getReader(val)
			case os.Stdout:
				runner.cmd.Stdout = runner.getWriter(val)
			case os.Stderr:
				runner.cmd.Stderr = runner.getWriter(val)
			}
		}
	}

	if key.IsArray() && key.Len() > 0 {
		writer := runner.getWriter(val)
		for i := 0; i < key.Len(); i++ {
			runner.parseFd(mrb.AryRef(key, i), mrb.Value(writer).Value())
		}
		return
	}
}

func (runner *cmdRunner) getReader(val oruby.Value) io.Reader {
	if r, ok := runner.redirect(val, false).(io.Reader); ok {
		return r
	}
	return nil
}

func (runner *cmdRunner) getWriter(val oruby.Value) io.Writer {
	if w, ok := runner.redirect(val, true).(io.Writer); ok {
		return w
	}
	return nil
}

// redirect resolves the target of a redirection option as MRI's spawn
// takes it: an IO, a descriptor number, :in/:out/:err (the parent's
// stream), a path (String or #to_path; out/err open it WRONLY|CREAT|TRUNC,
// in RDONLY) or [path, mode, perm]. nil for anything else.
func (runner *cmdRunner) redirect(val oruby.Value, write bool) interface{} {
	mrb := runner.mrb
	switch {
	case val.IsData():
		return mrb.Data(val)
	case val.IsInteger():
		switch val.Int() {
		case 0:
			return os.Stdin
		case 1:
			return os.Stdout
		case 2:
			return os.Stderr
		}
		return os.NewFile(uintptr(val.Int()), "fd")
	case val.IsSymbol():
		switch val.Symbol() {
		case oruby.Sym_in:
			return os.Stdin
		case oruby.Sym_out:
			return os.Stdout
		case oruby.Sym_err:
			return os.Stderr
		}
		return nil
	}
	flags := os.O_RDONLY
	if write {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	perm := os.FileMode(0644)
	pathV := val
	if val.IsArray() {
		if oruby.RArrayLen(val) == 0 || mrb.AryEntry(val, 0).IsSymbol() {
			return nil // [:child, :out]: the caller's business
		}
		pathV = mrb.AryEntry(val, 0)
		if m := mrb.AryEntry(val, 1); m.IsInteger() {
			flags = m.Int()
		} else if m.IsString() {
			flags = modeFlags(m.String())
		}
		if p := mrb.AryEntry(val, 2); p.IsInteger() {
			perm = os.FileMode(p.Int())
		}
	}
	if !pathV.IsString() && mrb.RespondTo(pathV, oruby.Sym_to_path) {
		if p, err := mrb.Funcall(pathV, oruby.Sym_to_path); err == nil {
			pathV = p
		}
	}
	if !pathV.IsString() {
		return nil
	}
	f, err := os.OpenFile(mrb.String(pathV), flags, perm)
	if err != nil {
		runner.err = err
		return nil
	}
	return f
}

// modeFlags: the open flags of an fopen-style mode ("r", "w", "a", "r+" ...)
func modeFlags(m string) int {
	m, _, _ = strings.Cut(m, ":")
	flags := os.O_RDONLY
	switch {
	case strings.HasPrefix(m, "w"):
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case strings.HasPrefix(m, "a"):
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	if strings.Contains(m, "+") {
		flags = flags&^(os.O_WRONLY|os.O_RDONLY) | os.O_RDWR
	}
	return flags
}

func (runner *cmdRunner) Wait(pid, flags int) (oruby.Value, *status) {
	var err error
	var ret int
	mrb := runner.mrb

	lastState := &status{Pid: pid}
	if (runner.cmd == nil) || (runner.cmd.Process == nil) || (runner.cmd.Process.Pid != pid) {
		ret, err = platformWait(pid, flags, lastState)
	} else {
		// Error ignoead as it can be only "Wait already called", in which case
		// ProcessState exists
		err = runner.cmd.Wait()
		if err != nil && runner.cmd.ProcessState == nil {
			mrb.SetGV("$?", nil)
			return mrb.ERuntimeError().RaiseError(err), lastState
		}
		state := runner.cmd.ProcessState
		ret = pid

		lastState.Pid = pid
		lastState.Exitstatus = state.ExitCode()
		lastState.ToI = uint32(state.ExitCode())
		lastState.IsCoredump = false
		lastState.IsExited = state.Exited()
		lastState.IsSignaled = false
		lastState.IsStopped = state.Exited()
		lastState.IsSucess = state.Success()
		lastState.platformData = state.Sys()
		lastState.Stopsig = nil
		lastState.Termsig = nil

		platformUpdateState(lastState, state.Sys())
	}

	mrb.SetGV("$?", mrb.Value(lastState))

	if err != nil && runner.cmd != nil && runner.cmd.ProcessState == nil {
		return mrb.ERuntimeError().RaiseError(err), lastState
	}

	return mrb.FixnumValue(ret), lastState
}

// lookPath resolves a command name on PATH like execvp; names with a slash
// are used as given
func lookPath(name string) string {
	if strings.ContainsRune(name, '/') {
		return name
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}
