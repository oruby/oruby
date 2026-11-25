package process

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

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
	argErr        error // a bad command or option: raised before running
	umaskSet      bool
	fdMap         map[int]int // child fd -> parent fd (Integer option keys)
	opened        []*os.File  // files opened or duplicated for the child
	name          string      // the command as given, looked up on PATH
	cmdline       string      // the command for messages
	childStd      [3]int      // [:child, :out]: the child's stream a std stream copies (-1: none)
}

func parseArgs(mrb *oruby.MrbState, args oruby.RArgs) *cmdRunner {
	env, command, params, options, argErr := parseArgsStructure(mrb, args)
	name := command
	if command == platformGetShell() && len(params) == 3 && params[0] == shellArgv0() && params[1] == "-c" {
		name = ""
	}
	cmdline := strings.Join(params, " ")
	if name == "" && len(params) == 3 {
		cmdline = params[2]
	}
	command = lookPath(command, os.Getenv("PATH"))

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
		closeOthers: false, // MRI's default: only close-on-exec descriptors close
		cleanup:     func() {},
		argErr:      argErr,
		name:        name,
		cmdline:     cmdline,
		childStd:    [3]int{-1, -1, -1},
	}

	mrb.HashValueForEach(options, func(key, val oruby.Value) int {
		// MRI's rb_execarg_addopt: an Integer, an IO or an Array of them is
		// a redirection, a Symbol an option; anything else is an error
		if !key.IsSymbol() {
			if key.IsInteger() || key.IsArray() || key.IsData() {
				runner.parseFd(key, val)
			} else {
				runner.fail(oruby.EArgumentError("wrong exec option: %s", mrb.Inspect(key)))
			}
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
			v, err := umaskArg(mrb, val)
			if err != nil {
				runner.fail(err)
				return 0
			}
			runner.umask = &v
		case "pgroup":
			// true or 0: a new group; a pgid: that group; nil, false: none
			var v int
			switch {
			case val.IsNil() || val.Type() == oruby.MrbTTFalse:
				return 0
			case val.Type() == oruby.MrbTTTrue:
				v = 0
			case val.IsInteger():
				v = val.Int()
				if v < 0 {
					runner.fail(oruby.EArgumentError("negative process group ID : %d", v))
					return 0
				}
			default:
				runner.fail(oruby.ETypeError("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ObjClass(val))))
				return 0
			}
			runner.pgroup = &v
		case "chdir":
			dir, err := pathArg(mrb, val)
			if err != nil {
				runner.fail(err)
				return 0
			}
			runner.cmd.Dir = dir
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				// raised when the command would run, as MRI's child reports it
				if err == nil {
					err = &os.PathError{Op: "chdir", Path: dir, Err: syscall.ENOTDIR}
				}
				runner.err = err
			}
		case "new_pgroup":
			if !val.IsNil() && val.Type() != oruby.MrbTTTrue && val.Type() != oruby.MrbTTFalse {
				runner.fail(oruby.EArgumentError("expected true or false as new_pgroup: %s", mrb.Inspect(val)))
			}
		case "close_others", "unsetenv_others", "exception":
			// MRI's check_exec_options: true, false or nil
			if !val.IsNil() && val.Type() != oruby.MrbTTTrue && val.Type() != oruby.MrbTTFalse {
				runner.fail(oruby.EArgumentError("expected true or false as %s: %s", k, mrb.Inspect(val)))
				return 0
			}
			switch k {
			case "close_others":
				runner.closeOthers = val.Bool()
			case "unsetenv_others":
				runner.unsetenvOther = val.Bool()
			default:
				runner.exception = val.Bool()
			}
		default:
			runner.fail(oruby.EArgumentError("wrong exec option symbol: %s", k))
		}

		return 0
	})

	// [:child, :out] and the like: the child's other stream
	for i, src := range runner.childStd {
		switch src {
		case 1:
			runner.setStd(i, runner.cmd.Stdout)
		case 2:
			runner.setStd(i, runner.cmd.Stderr)
		}
	}

	if strings.ContainsRune(command, 0) {
		runner.fail(oruby.EArgumentError("string contains null byte"))
	}
	for _, p := range params {
		if strings.ContainsRune(p, 0) {
			runner.fail(oruby.EArgumentError("string contains null byte"))
		}
	}

	// ENV
	runner.setENV(env)
	if runner.name != "" && !strings.ContainsRune(runner.name, '/') {
		// the command is looked up on the PATH the child gets
		for _, kv := range runner.cmd.Env {
			if strings.HasPrefix(kv, "PATH=") {
				runner.cmd.Path = lookPath(runner.name, kv[5:])
			}
		}
	}

	// Parse OS specific options (applied only when the options are valid)
	if runner.argErr == nil {
		runner.parseOptionsOS(options)
		runner.extraFiles()
	}
	osCleanup := runner.cleanup
	runner.cleanup = func() {
		osCleanup()
		for _, f := range runner.opened {
			_ = f.Close()
		}
		runner.opened = nil
	}

	return runner
}

// fail keeps the first argument error
func (runner *cmdRunner) fail(err error) {
	if runner.argErr == nil {
		runner.argErr = err
	}
}

// umaskArg is MRI's NUM2MODET of the umask: option
func umaskArg(mrb *oruby.MrbState, v oruby.Value) (int, error) {
	var n float64
	switch {
	case v.IsInteger():
		n = float64(v.Int64())
	case v.Type() == oruby.MrbTTFloat:
		n = v.Float64()
		if n != n {
			return 0, oruby.ERangeError("float NaN out of range of integer")
		}
	case v.Type() == oruby.MrbTTBigInt:
		return 0, oruby.ERangeError("bignum out of range of unsigned int")
	case v.IsNil():
		return 0, oruby.ETypeError("no implicit conversion from nil to integer")
	default:
		return 0, oruby.ETypeError("no implicit conversion of %s into Integer", mrb.ClassName(mrb.ObjClass(v)))
	}
	if n < -2147483648 || n > 4294967295 {
		return 0, oruby.ERangeError("integer %d too big to convert to 'unsigned int'", int64(n))
	}
	return int(int64(n)), nil
}

// extraFiles gives the child the descriptors of the Integer option keys
// above 2 (spawn: exec.Cmd.ExtraFiles, entry i is fd 3+i). Each is a
// duplicate of the parent's, closed after the start.
func (runner *cmdRunner) extraFiles() {
	for child, parent := range runner.fdMap {
		if child < 3 {
			continue
		}
		dup, err := dupFd(parent)
		if err != nil {
			runner.err = err
			continue
		}
		f := os.NewFile(uintptr(dup), "fd")
		runner.opened = append(runner.opened, f)
		for len(runner.cmd.ExtraFiles) < child-2 {
			runner.cmd.ExtraFiles = append(runner.cmd.ExtraFiles, nil)
		}
		runner.cmd.ExtraFiles[child-3] = f
	}
}

// parseArgsStructure returns env, command, params and options from args
func parseArgsStructure(mrb *oruby.MrbState, args oruby.RArgs) (oruby.Value, string, []string, oruby.Value, error) {
	env := mrb.NilValue()
	options := mrb.KeywordArgs()
	command := args.Item(0)
	argStart := 1
	var argErr error

	// the env: a Hash or #to_hash
	if !command.IsHash() && !command.IsString() && !command.IsArray() && !command.IsNil() && mrb.RespondTo(command, oruby.Sym_to_hash) {
		if h, err := mrb.Funcall(command, oruby.Sym_to_hash); err == nil && h.IsHash() {
			command = h
		}
	}
	if command.IsHash() {
		env = command
		command = args.Item(1)
		argStart++
	}

	// the command: a String (#to_str) or [command, argv0] (#to_ary)
	if !command.IsString() && !command.IsArray() && !command.IsNil() {
		if mrb.RespondTo(command, mrb.Intern("to_ary")) {
			if a, err := mrb.Funcall(command, mrb.Intern("to_ary")); err == nil && a.IsArray() {
				command = a
			}
		} else if mrb.RespondTo(command, oruby.Sym_to_str) {
			if str, err := mrb.Funcall(command, oruby.Sym_to_str); err == nil && str.IsString() {
				command = str
			}
		}
	}
	if command.IsNil() || command.IsHash() {
		// spawn({}, {}): an env and options, no command
		argErr = oruby.EArgumentError("wrong number of arguments (given 0, expected 1+)")
		command = mrb.StrNew("")
	}
	if command.IsArray() && oruby.RArrayLen(command) != 2 {
		argErr = oruby.EArgumentError("wrong first argument")
		command = mrb.StrNew("")
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
		params = append([]string{mrb.String(mrb.AryRef(command, 1))}, params...)
		cmd = mrb.String(mrb.AryRef(command, 0))
	} else if command.IsString() && len(params) > 0 {
		// command plus arguments: run directly, no shell (as MRI)
		params = append([]string{command.String()}, params...)
		cmd = command.String()
	} else if command.IsString() {
		if words, direct := shellWords(command.String()); direct {
			// no shell needed: the words are the argv ("" runs nothing: ENOENT)
			if len(words) == 0 {
				words = []string{""}
			}
			params = words
			cmd = words[0]
		} else {
			// /bin/sh, run as "sh" ($0)
			cmd = platformGetShell()
			params = []string{shellArgv0(), "-c", command.String()}
		}
	} else {
		cmd = command.String()
		params = append([]string{cmd}, params...)
	}

	return env, cmd, params, options.Value(), argErr
}

// setENV sets Cmd Environment: the parent's, changed by the env Hash (keys
// and values are Strings or #to_str, nil unsets), or only the Hash's with
// unsetenv_others: true
func (runner *cmdRunner) setENV(env oruby.Value) {
	mrb := runner.mrb
	if !env.IsHash() && !runner.unsetenvOther {
		runner.cmd.Env = os.Environ()
		return
	}
	type entry struct {
		name  string
		value *string
	}
	var changes []entry
	if env.IsHash() {
		mrb.HashValueForEach(env, func(k, v oruby.Value) int {
			name, err := envStr(mrb, k)
			if err != nil {
				runner.fail(err)
				return 0
			}
			if strings.ContainsRune(name, '=') {
				runner.fail(oruby.EArgumentError("environment name contains a equal : %s", name))
				return 0
			}
			e := entry{name: name}
			if !v.IsNil() {
				value, err := envStr(mrb, v)
				if err != nil {
					runner.fail(err)
					return 0
				}
				e.value = &value
			}
			changes = append(changes, e)
			return 0
		})
	}
	changed := map[string]bool{}
	for _, e := range changes {
		changed[e.name] = true
	}
	runner.cmd.Env = nil
	if !runner.unsetenvOther {
		for _, kv := range os.Environ() {
			if name, _, _ := strings.Cut(kv, "="); !changed[name] {
				runner.cmd.Env = append(runner.cmd.Env, kv)
			}
		}
	}
	for _, e := range changes {
		if e.value != nil {
			runner.cmd.Env = append(runner.cmd.Env, e.name+"="+*e.value)
		}
	}
}

// envStr: an environment name or value, a String or #to_str, no null byte
func envStr(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	if !v.IsString() {
		if !mrb.RespondTo(v, oruby.Sym_to_str) {
			return "", oruby.ETypeError("no implicit conversion of %s into String", mrb.ClassName(mrb.ObjClass(v)))
		}
		s, err := mrb.Funcall(v, oruby.Sym_to_str)
		if err != nil {
			return "", err
		}
		if !s.IsString() {
			return "", oruby.ETypeError("can't convert %s to String", mrb.ClassName(mrb.ObjClass(v)))
		}
		v = s
	}
	str := mrb.String(v)
	if strings.ContainsRune(str, 0) {
		return "", oruby.EArgumentError("string contains null byte")
	}
	return str, nil
}

// pathArg: a path option, a String or #to_path (#to_str)
func pathArg(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	for _, conv := range []oruby.MrbSym{oruby.Sym_to_path, oruby.Sym_to_str} {
		if v.IsString() {
			break
		}
		if mrb.RespondTo(v, conv) {
			r, err := mrb.Funcall(v, conv)
			if err != nil {
				return "", err
			}
			v = r
		}
	}
	if !v.IsString() {
		return "", oruby.ETypeError("no implicit conversion of %s into String", mrb.ClassName(mrb.ObjClass(v)))
	}
	return mrb.String(v), nil
}

// setStd sets the child's stream i (0 in, 1 out, 2 err)
func (runner *cmdRunner) setStd(i int, v interface{}) {
	switch i {
	case 0:
		if r, ok := v.(io.Reader); ok {
			runner.cmd.Stdin = r
		}
	case 1:
		if w, ok := v.(io.Writer); ok {
			runner.cmd.Stdout = w
		}
	case 2:
		if w, ok := v.(io.Writer); ok {
			runner.cmd.Stderr = w
		}
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
		switch k := key.Int(); k {
		case 0:
			runner.cmd.Stdin = runner.getReader(val)
		case 1:
			runner.cmd.Stdout = runner.getWriter(val)
		case 2:
			runner.cmd.Stderr = runner.getWriter(val)
		default:
			// child fd k: the parent's descriptor the value names
			if fd, ok := runner.redirectFd(val, true); ok {
				if runner.fdMap == nil {
					runner.fdMap = map[int]int{}
				}
				runner.fdMap[k] = fd
			}
		}
		return
	}

	if key.IsSymbol() {
		std := map[oruby.MrbSym]int{oruby.Sym_in: 0, oruby.Sym_out: 1, oruby.Sym_err: 2}[key.Symbol()]
		// [:child, :out]: the child's stdout (applied once all are parsed)
		if val.IsArray() && oruby.RArrayLen(val) == 2 && mrb.AryEntry(val, 0).IsSymbol() &&
			mrb.String(mrb.AryEntry(val, 0)) == "child" {
			switch mrb.String(mrb.AryEntry(val, 1)) {
			case "out":
				runner.childStd[std] = 1
			case "err":
				runner.childStd[std] = 2
			}
			return
		}
		if std == 0 {
			runner.cmd.Stdin = runner.getReader(val)
		} else {
			runner.setStd(std, runner.getWriter(val))
		}
		return
	}

	// an IO key is the child's descriptor of its number (w => w keeps a
	// pipe end open in the child, as MRI's)
	if !key.IsArray() && mrb.RespondTo(key, oruby.Sym_fileno) {
		if fd, err := mrb.Funcall(key, oruby.Sym_fileno); err == nil && fd.Value().IsInteger() {
			runner.parseFd(fd.Value(), val)
			return
		}
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
		// a duplicate: an *os.File of the parent's own number would close it
		// when collected
		dup, err := dupFd(val.Int())
		if err != nil {
			runner.err = err
			return nil
		}
		f := os.NewFile(uintptr(dup), "fd")
		runner.opened = append(runner.opened, f)
		return f
	case val.IsSymbol():
		switch val.Symbol() {
		case oruby.Sym_in:
			return os.Stdin
		case oruby.Sym_out:
			return os.Stdout
		case oruby.Sym_err:
			return os.Stderr
		}
		if mrb.String(val) == "close" {
			// a closed stream: /dev/null opened the other way, so the child's
			// reads or writes fail with EBADF as on a closed descriptor
			fl := os.O_WRONLY
			if write {
				fl = os.O_RDONLY
			}
			f, err := os.OpenFile(os.DevNull, fl, 0)
			if err != nil {
				runner.err = err
				return nil
			}
			runner.opened = append(runner.opened, f)
			return f
		}
		return nil
	case !val.IsString() && !val.IsArray() && mrb.RespondTo(val, oruby.Sym_to_io):
		// an object wrapping an IO
		if io, err := mrb.Funcall(val, oruby.Sym_to_io); err == nil && io.IsData() {
			return mrb.Data(io)
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
	runner.opened = append(runner.opened, f)
	return f
}

// redirectFd: the parent's descriptor number a redirection value names (an
// IO, an Integer, or a path opened for it)
func (runner *cmdRunner) redirectFd(val oruby.Value, write bool) (int, bool) {
	mrb := runner.mrb
	if val.IsInteger() {
		return val.Int(), true
	}
	if val.IsData() {
		if f, ok := mrb.Data(val).(*os.File); ok {
			fd := rawFd(f)
			return fd, fd >= 0
		}
		if f, ok := mrb.Data(val).(interface{ Fd() uintptr }); ok {
			return int(f.Fd()), true
		}
		return 0, false
	}
	if f, ok := runner.redirect(val, write).(*os.File); ok {
		fd := rawFd(f)
		return fd, fd >= 0
	}
	return 0, false
}

// rawFd is f's descriptor without switching it to blocking mode (Fd does)
func rawFd(f *os.File) int {
	fd := -1
	if sc, err := f.SyscallConn(); err == nil {
		_ = sc.Control(func(p uintptr) { fd = int(p) })
	}
	return fd
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
		ret, err = blockingWait(mrb, pid, flags, lastState)
	} else {
		// Error ignoead as it can be only "Wait already called", in which case
		// ProcessState exists
		mrb.Blocking(func() { err = runner.cmd.Wait() })
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

// lookPath resolves a command name on path (a PATH value) like execvp;
// names with a slash, or not found, are used as given
func lookPath(name, path string) string {
	if name == "" || strings.ContainsRune(name, '/') {
		return name
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p
		}
	}
	return name
}
