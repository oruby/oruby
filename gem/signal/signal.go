// Package signal implements Ruby's Signal module, SignalException and
// Interrupt. The Ruby side (signal.rb) follows MRI's signal.c; this file
// holds the platform signal table and the OS-level primitives, and
// oruby.MrbState.SignalCatch & co. carry OS signals into the running VM.
package signal

import (
	_ "embed"
	"sort"
	"syscall"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed signal.rb
var signalRb []byte

var signals map[string]int

// names MRI lists after the canonical name of the same number
var aliases = map[string]bool{"IOT": true, "CLD": true, "POLL": true, "UNUSED": true}

// how Signal.__oruby_os__ sets a signal up at the OS level
const (
	osCatch      = 0 // pass to Ruby, a trap handler runs
	osCatchRaise = 1 // pass to Ruby, the default handler raises SignalException
	osIgnore     = 2
	osDefault    = 3 // the OS default action (SYSTEM_DEFAULT)
)

func init() {
	signals = make(map[string]int)
	signals["EXIT"] = 0
	signals["INT"] = int(syscall.SIGINT)
	signals["KILL"] = int(syscall.SIGKILL)
	platformInitSignals()

	oruby.Gem("signal", func(mrb *oruby.MrbState) interface{} {
		mSignal := mrb.DefineModule("Signal")
		mSignal.DefineModuleFunction("__oruby_list__", sigList, mrb.ArgsNone())
		mSignal.DefineModuleFunction("__oruby_nsig__", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return mrb.FixnumValue(platformNSIG)
		}, mrb.ArgsNone())
		mSignal.DefineModuleFunction("__oruby_os__", sigOS, mrb.ArgsReq(2))
		mSignal.DefineModuleFunction("__oruby_deliver__", sigDeliver, mrb.ArgsReq(1))
		rb.LoadRuby(mrb, "<internal:signal>", signalRb)
		return nil
	})
}

// InstallDefaults gives the process MRI's default handlers: SIGINT raises
// Interrupt; SIGHUP, SIGQUIT, SIGTERM, SIGALRM, SIGUSR1 and SIGUSR2 raise
// SignalException. Programs run by oruby get them; an embedding host that
// handles signals itself does not call it.
func InstallDefaults(mrb *oruby.MrbState) error {
	if !mrb.ClassDefinedID(oruby.Sym_Signal) {
		return nil
	}
	_, err := mrb.Funcall(mrb.ModuleGetID(oruby.Sym_Signal), oruby.Sym___oruby_install_defaults__)
	return err
}

// sigList: name => number, in MRI's order (by number, canonical name first)
func sigList(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	names := make([]string, 0, len(signals))
	for k := range signals {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if signals[a] != signals[b] {
			return signals[a] < signals[b]
		}
		if aliases[a] != aliases[b] {
			return !aliases[a]
		}
		return a < b
	})
	ret := mrb.HashNewCapa(len(names))
	for _, k := range names {
		ret.Set(mrb.StrNew(k), mrb.FixnumValue(signals[k]))
	}
	return ret
}

// Signal.__oruby_os__(signo, mode)
func sigOS(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	sig := syscall.Signal(args.Item(0).Int())
	switch args.Item(1).Int() {
	case osCatch:
		mrb.SignalCatch(sig, false)
	case osCatchRaise:
		mrb.SignalCatch(sig, true)
	case osIgnore:
		mrb.SignalIgnore(sig)
	default:
		mrb.SignalSystemDefault(sig)
	}
	return mrb.NilValue()
}

// Signal.__oruby_deliver__(signo): after Process.kill to this process, run
// the handler before kill returns, as MRI does
func sigDeliver(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sig := syscall.Signal(mrb.GetArgsFirst().Int())
	mrb.SignalWait(sig, time.Second)
	if err := mrb.CheckSignals(); err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.NilValue()
}
