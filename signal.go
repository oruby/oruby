package oruby

// #include "go-mrb.h"
import "C"
import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// OS signals reach Ruby code through one state, the one that last asked to
// catch a signal (Signal.trap, or the default handlers oruby installs).
// Signals are process-wide, so the set of caught signals is too. A goroutine
// receives them from signal.Notify and posts them to go-signal.c, which runs
// Signal.__oruby_signal__ at the next instruction of that state.
var sigState struct {
	sync.Mutex
	owner   *MrbState
	ch      chan os.Signal
	caught  map[syscall.Signal]bool // signals passed to Ruby
	raising map[syscall.Signal]bool // ... whose Ruby handler is the default raise
	wake    chan struct{}           // a signal was posted: sleeps wake up
}

func init() {
	sigState.caught = make(map[syscall.Signal]bool)
	sigState.raising = make(map[syscall.Signal]bool)
	sigState.wake = make(chan struct{}, 1)
}

// SignalCatch passes sig to this state's Signal.__oruby_signal__. raising
// tells that the Ruby handler is the default one, which raises
// SignalException: a second such signal arriving before Ruby took the first
// (Ruby is stuck in a call that does not return) ends the process the way
// the OS would.
func (mrb *MrbState) SignalCatch(sig syscall.Signal, raising bool) {
	sigState.Lock()
	defer sigState.Unlock()
	if sigState.ch == nil {
		sigState.ch = make(chan os.Signal, 16)
		go sigLoop(sigState.ch)
	}
	sigState.owner = mrb
	sigState.caught[sig] = true
	sigState.raising[sig] = raising
	signal.Notify(sigState.ch, sig)
}

// SignalIgnore makes the process ignore sig
func (mrb *MrbState) SignalIgnore(sig syscall.Signal) {
	sigState.Lock()
	defer sigState.Unlock()
	delete(sigState.caught, sig)
	delete(sigState.raising, sig)
	signal.Ignore(sig)
}

// SignalSystemDefault gives sig the OS default action (MRI's SYSTEM_DEFAULT)
func (mrb *MrbState) SignalSystemDefault(sig syscall.Signal) {
	sigState.Lock()
	defer sigState.Unlock()
	delete(sigState.caught, sig)
	delete(sigState.raising, sig)
	if goRuntimeSignal(sig) {
		// the Go runtime needs it (SIGURG preempts goroutines)
		signal.Reset(sig)
		return
	}
	signal.Ignore(sig) // releases it from the Go runtime
	C._orb_sig_dfl(C.int(sig))
}

// SignalCaught tells whether sig is passed to this state
func (mrb *MrbState) SignalCaught(sig syscall.Signal) bool {
	sigState.Lock()
	defer sigState.Unlock()
	return sigState.owner == mrb && sigState.caught[sig]
}

// SignalWake is signalled when a signal is posted; code that blocks selects
// on it and then runs CheckSignals
func (mrb *MrbState) SignalWake() <-chan struct{} {
	return sigState.wake
}

// SignalWait waits until sig, sent to this process, is pending for Ruby:
// Process.kill to itself delivers the signal before it returns, as in MRI.
func (mrb *MrbState) SignalWait(sig syscall.Signal, timeout time.Duration) {
	if !mrb.SignalCaught(sig) {
		return
	}
	deadline := time.Now().Add(timeout)
	for C._orb_sig_pending_p(C.int(sig)) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Microsecond)
	}
}

// CheckSignals runs the Ruby handlers of pending signals now. It returns
// what a handler raised (the default handler raises SignalException), which
// the caller raises on with RaiseError.
func (mrb *MrbState) CheckSignals() error {
	sigState.Lock()
	owner := sigState.owner == mrb
	sigState.Unlock()
	if !owner {
		return nil
	}
	for {
		signo := int(C._orb_sig_take())
		if signo == 0 {
			return nil
		}
		if !mrb.ClassDefinedID(Sym_Signal) {
			continue
		}
		if _, err := mrb.Funcall(mrb.ModuleGetID(Sym_Signal), Sym___oruby_signal__, signo); err != nil {
			return err
		}
	}
}

// SignalKillSelf ends the process with sig, the OS default action, as MRI
// does for an uncaught SignalException
func SignalKillSelf(sig syscall.Signal) {
	C._orb_sig_kill_self(C.int(sig))
	time.Sleep(200 * time.Millisecond)
	os.Exit(128 + int(sig))
}

func sigLoop(ch chan os.Signal) {
	for s := range ch {
		sig, ok := s.(syscall.Signal)
		if !ok {
			continue
		}
		sigState.Lock()
		mrb := sigState.owner
		if mrb == nil || mrb.p == nil || !sigState.caught[sig] {
			sigState.Unlock()
			continue
		}
		if sigState.raising[sig] && C._orb_sig_pending_p(C.int(sig)) != 0 {
			// Ruby has not taken the last one: it is stuck outside the VM
			sigState.Unlock()
			SignalKillSelf(sig)
		}
		C._orb_sig_post(mrb.p, C.int(sig))
		sigState.Unlock()
		select {
		case sigState.wake <- struct{}{}:
		default:
		}
	}
}

// signalRelease stops passing signals to mrb when it closes
func (mrb *MrbState) signalRelease() {
	sigState.Lock()
	defer sigState.Unlock()
	if sigState.owner != mrb {
		return
	}
	sigState.owner = nil
	for sig := range sigState.caught {
		signal.Reset(sig)
	}
	sigState.caught = make(map[syscall.Signal]bool)
	sigState.raising = make(map[syscall.Signal]bool)
	C._orb_sig_clear(mrb.p)
}
