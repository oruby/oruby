package signal

import (
	"github.com/oruby/oruby"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var signals map[string]int

type sigHandlers struct {
	c chan os.Signal
	current map[os.Signal]oruby.Value
	exitHandler oruby.Value
}

func init() {
	signals = make(map[string]int)
	signals["EXIT"] = 0
	signals["INT"] = int(syscall.SIGINT)
	signals["KILL"] = int(syscall.SIGKILL)
	platformInitSignals()

	oruby.Gem("signal", func(mrb *oruby.MrbState) interface{} {
		mSignal := mrb.DefineModule("Signal")
		mrb.DefineGlobalFunction("trap", sigTrap, mrb.ArgsArg(1, 1)|mrb.ArgsBlock())

		mSignal.DefineModuleFunction("trap", sigTrap, mrb.ArgsArg(1, 1)|mrb.ArgsBlock())
		mSignal.DefineModuleFunction("list", sigList, mrb.ArgsNone())
		mSignal.DefineModuleFunction("signame", sigName, mrb.ArgsReq(1))

		eSignal := mrb.DefineClass("SignalException", mrb.EExceptionClass())
		eSignal.DefineMethod("initialize", esignalInit, mrb.ArgsArg(1,1))
		eSignal.DefineMethod("signo", esignalSigno, mrb.ArgsNone())
		eSignal.DefineAlias("signm", "message")

		eInterrupt := mrb.DefineClass("Interrupt", eSignal)
		eInterrupt.DefineMethod("initialize", interruptInit, mrb.ArgsOpt(1))

		return &sigHandlers{
			current: make(map[os.Signal]oruby.Value),
		}
	})
}

func getSignal(mrb *oruby.MrbState, sv oruby.Value) (int, string, error) {
	if sv.IsString() || sv.IsSymbol() {
		name := mrb.String(sv)
		if signo, ok := signals[name]; ok {
			return signo, name, nil
		}
		// Sig name can have "SIG" prefix - SIGHUP, SIGKILL
		if strings.HasPrefix(name,"SIG") {
			if signo, ok := signals[name[3:]]; ok {
				return signo, name[3:], nil
			}
		}

		return 0, "", oruby.EArgumentError("signal unknown: %v", name)
	}

	var sig int
	if sv.IsFixnum() {
		sig = sv.Int()
	} else {
		sigv := mrb.Call(sv, "to_int")
		if !sigv.IsFixnum() {
			return 0, "", oruby.EArgumentError("signal unknown: %v", mrb.Inspect(sv))
		}
		sig = sigv.Int()
	}

	for name, v := range signals {
		if sig == v {
			return sig, name, nil
		}
	}
	return 0, "", oruby.EArgumentError("signal unknown: %v", sig)
}

func trap(mrb *oruby.MrbState, handlers *sigHandlers) {
	if handlers.c != nil {
		return
	}

	handlers.c = make(chan os.Signal, 1)
	mrb.WaitGroup.Add(1)

	go func(){
		for {
			select {
			case sig := <-handlers.c:
				if sig == nil {
					return
				}
				cmd, ok := handlers.current[sig]
				if ok && cmd.IsProc() {
					//TODO: This doesn't work. It needs to inject into main thread
					mrb.Call(cmd, "call")
				}
			case <-mrb.ExitChan():
				// gracefull close goroutine on mrb state close
				signal.Stop(handlers.c)
				close(handlers.c)

				// zero signal handler, executed at MrbState closing
				if !handlers.exitHandler.IsNil() {
					mrb.Call(handlers.exitHandler, "call")
				}

				handlers.current = nil
				mrb.WaitGroup.Done()
				return
			}
		}
	}()
}

func sigTrap(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	sigValue := args.Item(0)
	command  := args.ItemDef(1, block)

	sigID, name, err := getSignal(mrb, mrb.Value(sigValue))
	if err != nil {
		return mrb.RaiseError(err)
	}

	if platformReservedSignal(sigID) {
		return mrb.EArgumentError().Raisef("can't trap reserved signal: %v", name)
	}

	handlers := mrb.GemData("signal").(*sigHandlers)
	sig := syscall.Signal(sigID)
	var prev oruby.Value

	// EXIT signal 0 handler
	if sigID == 0 {
		prev = handlers.exitHandler
		handlers.exitHandler = command
		if command.IsProc() {
			trap(mrb, handlers)
			return prev
		}
	}

	prev = handlers.current[sig]
	handlers.current[sig] = command

	if command.IsProc() {
		trap(mrb, handlers)
		signal.Notify(handlers.c, sig)
		return prev
	}

	if command.IsString() {
		switch command.String() {
		case "IGNORE", "SIG_IGN":
			signal.Ignore(sig)
		case "DEFAULT", "SIG_DFL":
			signal.Reset(sig)
		case "EXIT":
			mrb.Call(mrb.KernelModule(), "send", mrb.Intern("exit"))
		case "SYSTEM_DEFAULT":
			signal.Reset(sig)
		default:
			return mrb.RaiseError(oruby.EArgumentError("unknown command '%v'", command))
		}
		return prev
	}

	return mrb.RaiseError(oruby.EArgumentError("unknown command '%v'", command))
}

func sigList(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	ret := mrb.HashNewCapa(len(signals))
	for k, v := range signals {
		ret.Set(mrb.StrNewStatic(k), mrb.FixnumValue(v))
	}
	return ret
}

func sigName(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sig := mrb.GetArgsFirst().Int()
	for k, v := range signals {
		if v == sig {
			return mrb.StrNewStatic(k)
		}
	}

	return mrb.NilValue()
}

func esignalInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	signo, name, err := getSignal(mrb, args.Item(0))
	if err != nil {
		return mrb.RaiseError(err)
	}

	obj := mrb.RObject(self)
	obj.SetIV("@signo", signo)
	obj.SetIV( "mesg", args.ItemDef(1, mrb.Value("SIG"+name)))

	return self
}

func esignalSigno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.RObject(self).GetIV("@signo")
}

func interruptInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	obj := mrb.RObject(self)
	obj.SetIV("@signo", int(syscall.SIGINT))

	name := mrb.GetArgsFirst()
	if !name.IsNil() {
		obj.SetIV("mesg", name)
	} else {
		obj.SetIV("mesg", "SIGINT")
	}

	return self
}
