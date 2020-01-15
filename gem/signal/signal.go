package signal

import (
	"github.com/oruby/oruby"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var signals map[string]int

type handlers struct {
	c chan os.Signal
	current map[os.Signal]oruby.Value
}

func init() {
	signals = make(map[string]int)
	signals["INT"] = int(syscall.SIGINT)
	signals["KILL"] = int(syscall.SIGKILL)
	platformInitSignals()

	oruby.Gem("signal", func(mrb *oruby.MrbState) interface{} {
		mSignal := mrb.DefineModule("Signal")
		mrb.DefineGlobalFunction("trap", sigTrap, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())

		mSignal.DefineModuleFunction("trap", sigTrap, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
		mSignal.DefineModuleFunction("list", sigList, mrb.ArgsNone())
		mSignal.DefineModuleFunction("signame", sigName, mrb.ArgsReq(1))

		eSignal := mrb.DefineClass("SignalException", mrb.EExceptionClass())
		eSignal.DefineMethod("initialize", esignalInit, mrb.ArgsArg(1,1))
		eSignal.DefineMethod("signo", esignalSigno, mrb.ArgsNone())
		eSignal.DefineAlias("signm", "message")

		eInterrupt := mrb.DefineClass("Interrupt", eSignal)
		eInterrupt.DefineMethod("initialize", interruptInit, mrb.ArgsOpt(1))

		return &handlers{
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

func sigTrap(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sigValue, command := mrb.GetArgs2(mrb.NilValue(), mrb.GetArgsBlock())
	sigID, name, err := getSignal(mrb, sigValue)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if platformReservedSignal(sigID) {
		return mrb.EArgumentError().Raisef("can't trap reserved signal: %v", name)
	}

	hdlrs := mrb.GemData("signal").(*handlers)
	sig := syscall.Signal(sigID)

	prev := hdlrs.current[sig]
	hdlrs.current[sig] = command

	if command.IsProc() {

		if len(hdlrs.c) == 0 {
			hdlrs.c = make(chan os.Signal, 1)
			go func(){
				for {
					select {
					case sig := <-hdlrs.c:
						if cmd, ok := hdlrs.current[sig]; ok && cmd.IsProc() {
							mrb.Call(cmd, "call")
						}
					case <-mrb.CloseChan():
						// gracefull close goroutine on mrb state close
						// TODO: call zero signal handler?
						return
					}
				}
			}()
		}
		signal.Notify(hdlrs.c, sig)
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
	args := mrb.GetAllArgs()
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
