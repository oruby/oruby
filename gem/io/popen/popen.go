package popen

import (
	"github.com/oruby/oruby"
	gemIO "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/process"

	"errors"
	"os/exec"
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
	_= sp.ReadCloser.Close()
	return mrb.NilValue()
}

func ioCloseWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sp, ok := mrb.Data(self).(*superPipe)
	if !ok {
		return gemIO.RaiseIOError(mrb, "IO stream is not duplexed")
	}
	_= sp.WriteCloser.Close()
	return mrb.NilValue()
}

func ioPopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	proc := mrb.ConstGet(mrb.ObjectClass(), mrb.Intern("Process"))
	if proc.Type() != oruby.MrbTTModule {
		panic("gem 'process' must be required for IO::popen to work")
	}

	args, block := mrb.GetArgsWithBlock()
	env  := args.Item(0)
	command := args.Item(1)
	modeV := args.Item(2)
	opt  := args.GetLastHash()

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

	mode, err := parseFlags(mrb, modeV, opt)
	if err != nil {
		return mrb.RaiseError(err)
	}

	// Process.spawn(env, command, opt)
	var cmdV oruby.Value
	if env.IsNil() {
		cmdV, err = mrb.FuncallWithBlock(proc, mrb.Intern("_get_cmd"), command, opt)
	} else {
		cmdV, err = mrb.FuncallWithBlock(proc, mrb.Intern("_get_cmd"), env, command, opt)
	}
	if err != nil {
		return mrb.RaiseError(err)
	}

	cmd, ok := mrb.Data(cmdV).(*exec.Cmd)
	if !ok {
		return mrb.ERuntimeError().Raise("Process::_get_cmd does not return command")
	}

	cmd.Stdin = nil
	cmd.Stdout = nil

	r, err := cmd.StdoutPipe()
	if err != nil {
		return mrb.RaiseError(err)
	}

	w, err := cmd.StdinPipe()
	if err != nil {
		return mrb.RaiseError(err)
	}

	if err = cmd.Start(); err != nil {
		return mrb.RaiseError(err)
	}

	ret := mrb.DataValue(&superPipe{r, w, cmd.Process.Pid, mode})
	if block.IsNil() {
		return ret
	}

	result, err := mrb.Yield(block, ret)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if err = cmd.Wait(); err != nil {
		return mrb.RaiseError(err)
	}
	return result
}

func modeToFlags(mrb *oruby.MrbState, mode oruby.Value) (int, error) {
	if mode.IsNil() {
		// Default is RDONLY
		return 0, nil

	} else if mode.IsFixnum() {
		// mode integer: File::RDONLY|File::EXCL
		return mode.Int(), nil

	} else if mode.IsHash() {
		// mode: popen - int only
		mFlags := mrb.HashFetch(mode, mrb.Intern("mode"), oruby.Int(0)).Int()
		fFlags := mrb.HashFetch(mode, mrb.Intern("flags"), oruby.Int(0)).Int()
		return mFlags|fFlags, nil

	} else  {
		return 0, oruby.EArgumentError("illegal access mode %v", mrb.Inspect(mode))
	}
}

func parseFlags(mrb *oruby.MrbState, mode, optHash oruby.Value) (int, error) {
	flags,err := modeToFlags(mrb, mode)
	if err != nil {
		return 0, err
	}

	flagsOpt,err := modeToFlags(mrb, optHash)
	if err != nil {
		return 0, err
	}

	return flags|flagsOpt, nil
}