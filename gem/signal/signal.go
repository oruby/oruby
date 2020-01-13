package signal

import (
	"github.com/oruby/oruby"
	"os"
	"strconv"
	"syscall"
)

var signals map[string]int

func init() {
	signals = make(map[string]int)

	signals[os.Interrupt.String()] = int(syscall.SIGINT)
	signals[os.Kill.String()] = int(syscall.SIGABRT)
	platformInitSignals()

	oruby.Gem("signal", func(mrb *oruby.MrbState) {
		mSignal := mrb.DefineModule("Signal")
		mrb.DefineGlobalFunction("trap", sigTrap, mrb.ArgsAny())

		mSignal.DefineModuleFunction("trap", sigTrap, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
		mSignal.DefineModuleFunction("list", sigList, mrb.ArgsNone())
		mSignal.DefineModuleFunction("signame", sigName, mrb.ArgsReq(1))

		eSignal := mrb.DefineClass("SignalException", mrb.EExceptionClass())

		eSignal.DefineMethod("initialize", esignalInit, -1)
		eSignal.DefineMethod("signo", esignalSigno, mrb.ArgsNone())
		eSignal.DefineAlias("signm", "message")

		eInterrupt := mrb.DefineClass("Interrupt", eSignal)
		eInterrupt.DefineMethod("initialize", interruptInit, -1)
	})
}

func sigTrap(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sigValue, command := mrb.GetArgs2(mrb.NilValue(), mrb.GetArgsBlock())
	sig := getSignal(mrb, sigValue)

	if reservedSignal(sig) {
		name := getSignalName(sig)
		_, ok := signals[name]
		if !ok {
			name = strconv.Itoa(sig)
		}
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

}

func esignalSigno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {

}

func interruptInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Call(self, "super", int(syscall.SIGINT), mrb.GetArgsFirst())
}
