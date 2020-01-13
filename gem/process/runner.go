package process

import (
	"github.com/oruby/oruby"
	"io"
	"os"
	"os/exec"
	"strings"
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
	fIn           oruby.Value
	fOut          oruby.Value
	fErr          oruby.Value
	cleanup       func()
	err           error
}

func parseArgs(mrb *oruby.MrbState, args oruby.RArray) *cmdRunner {
	env, command, params, options := parseArgsStructure(mrb, args)

	// Create command
	runner := &cmdRunner{
		mrb:         mrb,
		cmd:         exec.Command(command, params...),
		closeOthers: true,
		fIn:         mrb.NilValue(),
		fOut:        mrb.NilValue(),
		fErr:        mrb.NilValue(),
	}

	mrb.HashValueForEach(options, func(mrb *oruby.MrbState, key, val oruby.Value) int {
		if key.IsFixnum() || key.IsArray() || key.HasBasic() {
			runner.parseFd(key, val)
			return 0
		}

		k := mrb.String(key)

		if strings.HasPrefix(k, "rlimit_") {
			runner.parseRLimit(key, val)
			return 0
		}

		switch k {
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
		case "in":
			runner.fIn = val
		case "out":
			runner.fOut = val
		case "err":
			runner.fErr = val
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
func parseArgsStructure(mrb *oruby.MrbState, args oruby.RArray) (oruby.Value, string, []string, oruby.Value) {
	env := mrb.NilValue()
	options := args.Item(-1)
	command := args.Item(0)
	argStart := 1

	if command.IsHash() {
		env = command
		command = args.Item(1)
		argStart++
	}

	params := make([]string, 0, args.Len()-argStart+1)

	if command.IsArray() {
		params = append(params, mrb.AryRef(command, 1).String())
		command = mrb.AryRef(command, 0)
	} else {
		params = append(params, command.String())
	}

	// Params: from first arg after command, till options
	for i := argStart; i < args.Len()-1; i++ {
		params = append(params, mrb.String(args.Item(i)))
	}

	// If no options given - last item is also param
	if !options.IsHash() {
		params = append(params, mrb.String(options))
		options = mrb.NilValue()
	}
	return env, command.String(), params, options
}

// setENV sets Cmd Environment
func (runner *cmdRunner) setENV(env oruby.Value) {
	mrb := runner.mrb
	EnvIsHash := env.IsHash()

	for _, v := range os.Environ() {
		kv := strings.Split(v, "=")
		key := mrb.StringValue(kv[1])
		if EnvIsHash && mrb.HashKeyP(env, key) {
			// Env has variable - add if not nil
			value := mrb.HashGet(env, key)
			if !value.IsNil() {
				runner.cmd.Env = append(runner.cmd.Env, kv[1]+"="+value.String())
			}

		} else {
			// Variable not in env, add respecting :unsetenv_other option
			if !runner.unsetenvOther {
				runner.cmd.Env = append(runner.cmd.Env, v)
			}
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

	if key.IsFixnum() {
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
		case mrb.Intern("in"):
			runner.cmd.Stdin = runner.getReader(val)
		case mrb.Intern("out"):
			runner.cmd.Stdout = runner.getWriter(val)
		case mrb.Intern("err"):
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

	//if mrb.ObjIsKindOf(o, mrb.ClassGet("IO")) {
	//
	//}

	if key.IsArray() && key.Len() > 0 {
		writer := runner.getWriter(val)
		for i := 0; i < key.Len(); i++ {
			runner.parseFd(mrb.AryRef(key, 0), mrb.Value(writer).Value())
		}
		return
	}

}

func (runner *cmdRunner) getReader(val oruby.Value) io.Reader {
	mrb := runner.mrb
	if val.IsData() {
		if reader, ok := mrb.Data(val).(io.Reader); ok {
			return reader
		}
	}

	return nil
}

func (runner *cmdRunner) getWriter(val oruby.Value) io.Writer {
	mrb := runner.mrb
	if val.IsData() {
		if writer, ok := mrb.Data(val).(io.Writer); ok {
			return writer
		}
	}

	return nil
}

func (runner *cmdRunner) setStdIn(val oruby.Value) {

}

func (runner *cmdRunner) setStdOut(val oruby.Value) {

}

func (runner *cmdRunner) setStdErr(val oruby.Value) {

}
