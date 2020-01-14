package signal

import (
	"github.com/oruby/oruby"
	"strings"
	"syscall"
)

var signals map[string]int

func init() {
	signals = make(map[string]int)

	signals["INT"] = int(syscall.SIGINT)
	signals["KILL"] = int(syscall.SIGABRT)
	platformInitSignals()

	oruby.Gem("signal", func(mrb *oruby.MrbState) {
		mSignal := mrb.DefineModule("Signal")
		mrb.DefineGlobalFunction("trap", sigTrap, mrb.ArgsAny())

		mSignal.DefineModuleFunction("trap", sigTrap, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
		mSignal.DefineModuleFunction("list", sigList, mrb.ArgsNone())
		mSignal.DefineModuleFunction("signame", sigName, mrb.ArgsReq(1))

		eSignal := mrb.DefineClass("SignalException", mrb.EExceptionClass())

		eSignal.DefineMethod("initialize", esignalInit, mrb.ArgsArg(1,1))
		eSignal.DefineMethod("signo", esignalSigno, mrb.ArgsNone())
		eSignal.DefineAlias("signm", "message")

		eInterrupt := mrb.DefineClass("Interrupt", eSignal)
		eInterrupt.DefineMethod("initialize", interruptInit, mrb.ArgsOpt(1))
	})
}

func getSignal(mrb *oruby.MrbState, sv oruby.Value) (int, string, error) {
	if sv.IsString() {
		name := sv.String()
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
	sig, name, err := getSignal(mrb, sigValue)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if platformReservedSignal(sig) {
		return mrb.EArgumentError().Raisef("can't trap reserved signal: %v", name)
	}

	if !command.IsProc() {
		f = sighandler
	} else {
		f = trapHandler(command, sig)
	}

	return trap(sig, f, command)
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
	signo, name, err := getSignal(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}

	obj := mrb.RObject(self)
	obj.SetIV("@signo", signo)
	obj.SetIV( "mesg", "SIG"+name)

	return self
}

func esignalSigno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.RObject(self).GetIV("@signo")
}

func interruptInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	obj := mrb.RObject(self)
	obj.SetIV("@signo", int(syscall.SIGINT))
	obj.SetIV( "mesg", mrb.GetArgsFirst())

	return mrb.Call(self, "super", int(syscall.SIGINT), mrb.GetArgsFirst())
}
