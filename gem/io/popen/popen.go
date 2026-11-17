package popen

import (
	"github.com/oruby/oruby"
	gemIO "github.com/oruby/oruby/gem/io"

	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

func init() {
	oruby.Gem("io/popen", func(mrb *oruby.MrbState) interface{} {
		mrb.Require("io")
		mrb.Require("process")

		cIO := mrb.Class("IO")
		cIO.AttachType((*superPipe)(nil))

		cIO.DefineClassMethod("popen", ioPopen, mrb.ArgsReq(1)+mrb.ArgsRest())
		cIO.DefineMethod("close_read", ioCloseRead, mrb.ArgsNone())
		cIO.DefineMethod("close_write", ioCloseWrite, mrb.ArgsNone())
		cIO.DefineMethod("pid", ioPid, mrb.ArgsNone())
		return nil
	})
}

var errClosed = errors.New("IOError: not opened for writing")

func ioPid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if sp, ok := mrb.Data(self).(*superPipe); ok {
		return oruby.Int(sp.pid)
	}
	return mrb.NilValue()
}

func ioCloseRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sp, ok := mrb.Data(self).(*superPipe)
	if !ok {
		return gemIO.RaiseIOError(mrb, "IO stream is not duplexed")
	}
	_ = sp.ReadCloser.Close()
	return mrb.NilValue()
}

func ioCloseWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sp, ok := mrb.Data(self).(*superPipe)
	if !ok {
		return gemIO.RaiseIOError(mrb, "IO stream is not duplexed")
	}
	_ = sp.WriteCloser.Close()
	return mrb.NilValue()
}

func ioPopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	proc := mrb.ConstGet(mrb.ObjectClass(), oruby.Sym_Process)
	if proc.Type() != oruby.MrbTTModule {
		panic("gem 'process' must be required for IO::popen to work")
	}

	args, block := mrb.GetArgsWithBlock()
	env := args.Item(0)
	command := args.Item(1)
	modeV := args.Item(2)
	opt := mrb.KeywordArgs()

	if !env.IsHash() {
		modeV = command
		command = env
		env = mrb.NilValue()
	}

	if command.IsString() && command.String() == "-" {
		return mrb.EArgumentError().Raise("fork param '-' is not supported")
	}

	if command.IsArray() {
		arg := mrb.AryEntry(command, 0)
		if arg.IsHash() {
			if env.IsNil() {
				env = arg
			}
			mrb.AryShift(command)
		}

		arg = mrb.AryEntry(command, -1)
		if arg.IsHash() {
			mrb.HashMerge(opt, arg)
			mrb.AryPop(command)
		}
	}

	// IO.popen(cmd, {err: ...}): a trailing Hash is the options, not a mode
	if modeV.IsHash() {
		mrb.HashMerge(opt, modeV)
		modeV = mrb.NilValue()
	}
	if extra := args.Item(3); extra.IsHash() {
		mrb.HashMerge(opt, extra)
	}
	errToOut := false
	if e := mrb.HashFetch(opt.Value(), oruby.Sym_err, mrb.NilValue()); e.IsArray() {
		errToOut = mrb.String(mrb.AryEntry(e, 0)) == "child" && mrb.String(mrb.AryEntry(e, 1)) == "out"
	}

	mode, err := parseFlags(mrb, modeV, opt.Value())
	if err != nil {
		return mrb.RaiseError(err)
	}

	// Process.spawn(env, command, opt)
	// popen's Array is the whole argv (spawn's first-argument Array is
	// [command, argv0]), so its elements go to spawn one by one
	var spawnArgs []interface{}
	if !env.IsNil() {
		spawnArgs = append(spawnArgs, env)
	}
	if command.IsArray() {
		for i := 0; i < oruby.RArrayLen(command); i++ {
			spawnArgs = append(spawnArgs, mrb.AryEntry(command, i))
		}
	} else {
		spawnArgs = append(spawnArgs, command)
	}
	spawnArgs = append(spawnArgs, opt)
	cmdV, err := mrb.FuncallWithBlock(proc, oruby.Sym__get_cmd, spawnArgs...)
	if err != nil {
		return mrb.RaiseError(err)
	}

	cmd, ok := mrb.Data(cmdV).(*exec.Cmd)
	if !ok {
		return mrb.ERuntimeError().Raise("Process::_get_cmd does not return command")
	}

	// "r": the child reads the parent's stdin; "w": it writes to the
	// parent's stdout; "r+": both ends are pipes
	acc := mode & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR)
	var r io.ReadCloser = io.NopCloser(eofReader{})
	var w io.WriteCloser = nopWriteCloser{}
	if acc != os.O_WRONLY {
		cmd.Stdout = nil
		r, err = cmd.StdoutPipe()
		if err != nil {
			return mrb.RaiseError(err)
		}
		if errToOut {
			cmd.Stderr = cmd.Stdout // err: [:child, :out]
		}
	} else if errToOut {
		cmd.Stderr = cmd.Stdout
	}
	if acc != os.O_RDONLY {
		cmd.Stdin = nil
		w, err = cmd.StdinPipe()
		if err != nil {
			return mrb.RaiseError(err)
		}
	}

	if err = cmd.Start(); err != nil {
		return mrb.RaiseError(err)
	}

	sp := &superPipe{ReadCloser: r, WriteCloser: w, pid: cmd.Process.Pid, mode: mode, cmd: cmd, mrb: mrb}
	ret := mrb.DataValue(sp)
	if block.IsNil() {
		return ret
	}

	result := mrb.Yield(block, ret)
	if mrb.Exc() != nil {
		return mrb.RaiseError(mrb.Err())
	}

	_ = sp.WriteCloser.Close()
	_ = sp.ReadCloser.Close()
	sp.wait()
	return result
}

func modeToFlags(mrb *oruby.MrbState, mode oruby.Value) (int, error) {
	if mode.IsNil() {
		// Default is RDONLY
		return 0, nil

	} else if mode.IsString() {
		// mode string: "r", "w", "r+", "rb", "w:utf-8", ...
		return popenModeString(mode.String())

	} else if mode.IsInteger() {
		// mode integer: File::RDONLY|File::EXCL
		return mode.Int(), nil

	} else if mode.IsHash() {
		// mode: popen - int only
		mFlags := mrb.HashFetch(mode, oruby.Sym_mode, oruby.Int(0)).Int()
		fFlags := mrb.HashFetch(mode, oruby.Sym_flags, oruby.Int(0)).Int()
		return mFlags | fFlags, nil

	} else {
		return 0, oruby.EArgumentError("illegal access mode %v", mrb.Inspect(mode))
	}
}

func parseFlags(mrb *oruby.MrbState, mode, optHash oruby.Value) (int, error) {
	flags, err := modeToFlags(mrb, mode)
	if err != nil {
		return 0, err
	}

	flagsOpt, err := modeToFlags(mrb, optHash)
	if err != nil {
		return 0, err
	}

	return flags | flagsOpt, nil
}

func popenModeString(m string) (int, error) {
	if i := strings.IndexByte(m, ':'); i >= 0 {
		m = m[:i]
	}
	if m == "" {
		return 0, oruby.EArgumentError("illegal access mode %q", m)
	}
	flags := 0
	switch m[0] {
	case 'r':
		flags = os.O_RDONLY
	case 'w', 'a':
		flags = os.O_WRONLY
	default:
		return 0, oruby.EArgumentError("illegal access mode %q", m)
	}
	for _, c := range m[1:] {
		switch c {
		case '+':
			flags = os.O_RDWR
		case 'b', 't':
		default:
			return 0, oruby.EArgumentError("illegal access mode %q", m)
		}
	}
	return flags, nil
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

type nopWriteCloser struct{}

func (nopWriteCloser) Write(b []byte) (int, error) { return len(b), nil }
func (nopWriteCloser) Close() error                { return nil }
