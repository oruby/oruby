package popen

import (
	_ "embed"
	"io"
	"os"
	"os/exec"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	_ "github.com/oruby/oruby/gem/io" // the "io" gem popen extends
)

//go:embed popen.rb
var popenRb []byte

func init() {
	oruby.Gem("io/popen", func(mrb *oruby.MrbState) interface{} {
		mrb.Require("io")
		mrb.Require("process")

		cIO := mrb.Class("IO")
		cIO.AttachType((*superPipe)(nil))

		cIO.DefineMethod("__oruby_popen_start", ioPopenStart, mrb.ArgsReq(5))
		cIO.DefineMethod("__oruby_popen_pid", ioPid, mrb.ArgsNone())
		cIO.DefineMethod("__oruby_popen_duplex?", ioDuplex, mrb.ArgsNone())
		cIO.DefineMethod("__oruby_popen_close_read", ioCloseRead, mrb.ArgsNone())
		cIO.DefineMethod("__oruby_popen_close_write", ioCloseWrite, mrb.ArgsNone())
		rb.LoadRuby(mrb, "<internal:popen>", popenRb)
		return nil
	})
}

func ioPid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if sp, ok := mrb.Data(self).(*superPipe); ok {
		return oruby.Int(sp.pid)
	}
	return mrb.NilValue()
}

// a "r+" stream: a pipe each way, closed one at a time by close_read and
// close_write
func ioDuplex(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sp, ok := mrb.Data(self).(*superPipe)
	return mrb.BoolValue(ok && sp.mode&(os.O_WRONLY|os.O_RDWR) == os.O_RDWR)
}

func ioCloseRead(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if sp, ok := mrb.Data(self).(*superPipe); ok {
		_ = sp.ReadCloser.Close()
	}
	return mrb.NilValue()
}

func ioCloseWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if sp, ok := mrb.Data(self).(*superPipe); ok {
		_ = sp.WriteCloser.Close()
	}
	return mrb.NilValue()
}

// io.__oruby_popen_start(env, command, readable, writable, exec_options):
// starts the child (Process._get_cmd builds it, as for Process.spawn) with
// a pipe from its stdout (readable) and one to its stdin (writable), and
// makes io the parent's end. popen.rb sorts out the arguments.
func ioPopenStart(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	proc := mrb.ConstGet(mrb.ObjectClass(), oruby.Sym_Process)
	if proc.Type() != oruby.MrbTTModule {
		return mrb.ERuntimeError().Raise("gem 'process' must be required for IO::popen to work")
	}
	a := mrb.GetArgs()
	env, command, readable, writable, opt := a.Item(0), a.Item(1), a.Item(2).Bool(), a.Item(3).Bool(), a.Item(4)

	errToOut := false
	if e := mrb.HashFetch(opt, oruby.Sym_err, mrb.NilValue()); e.IsArray() && oruby.RArrayLen(e) == 2 {
		// the child's stderr goes where its stdout does: the pipe
		errToOut = mrb.String(mrb.AryEntry(e, 0)) == "child" && mrb.String(mrb.AryEntry(e, 1)) == "out"
	}

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

	mode := os.O_RDONLY
	switch {
	case readable && writable:
		mode = os.O_RDWR
	case writable:
		mode = os.O_WRONLY
	}
	var r io.ReadCloser = io.NopCloser(eofReader{})
	var w io.WriteCloser = nopWriteCloser{}
	if readable {
		cmd.Stdout = nil
		r, err = cmd.StdoutPipe()
		if err != nil {
			return mrb.RaiseError(err)
		}
		if errToOut {
			cmd.Stderr = cmd.Stdout
		}
	} else if errToOut {
		cmd.Stderr = cmd.Stdout
	}
	if writable {
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
	mrb.DataSetInterface(self, sp)
	return self
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

type nopWriteCloser struct{}

func (nopWriteCloser) Write(b []byte) (int, error) { return len(b), nil }
func (nopWriteCloser) Close() error                { return nil }
