package oruby

// #include "go-mrb.h"
import "C"
import (
	"context"
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
}

func init() {
	sigState.caught = make(map[syscall.Signal]bool)
	sigState.raising = make(map[syscall.Signal]bool)
}

// vmState is a state's side of the requests go-signal.c delivers: its wake
// channel, its context stack and its Timeout.timeout deadlines
type vmState struct {
	sync.Mutex
	wake   chan struct{} // a request was posted: blocking Go code wakes up
	ctxs   []ctxEntry    // innermost last: EvalContext, Timeout.timeout
	next   int64
	timers map[int64]context.CancelFunc
	fired  []int64 // deadlines passed, not yet raised
	closed bool
}

type ctxEntry struct {
	token int64 // 0 for a context pushed by EvalContext
	ctx   context.Context
}

var vmStates sync.Map // *MrbState -> *vmState

func (mrb *MrbState) vm() *vmState {
	if v, ok := vmStates.Load(mrb); ok {
		return v.(*vmState)
	}
	v, _ := vmStates.LoadOrStore(mrb, &vmState{wake: make(chan struct{}, 1), timers: map[int64]context.CancelFunc{}})
	return v.(*vmState)
}

func (vs *vmState) notify() {
	select {
	case vs.wake <- struct{}{}:
	default:
	}
}

// GoContext is the context.Context the Ruby code of mrb runs under: the deadline of
// the innermost Timeout.timeout block, the ctx of EvalContext, or
// context.Background(). A Go method that blocks hands it to what it waits
// on (an http.Request, a net.Conn deadline, a select), so a Ruby timeout
// stops the wait the Go way; the Ruby side then raises at its next
// instruction.
func (mrb *MrbState) GoContext() context.Context {
	vs := mrb.vm()
	vs.Lock()
	defer vs.Unlock()
	if n := len(vs.ctxs); n > 0 {
		return vs.ctxs[n-1].ctx
	}
	return context.Background()
}

func (mrb *MrbState) pushContext(token int64, ctx context.Context) {
	vs := mrb.vm()
	vs.Lock()
	vs.ctxs = append(vs.ctxs, ctxEntry{token, ctx})
	vs.Unlock()
}

func (mrb *MrbState) popContext(ctx context.Context) {
	vs := mrb.vm()
	vs.Lock()
	for i := len(vs.ctxs) - 1; i >= 0; i-- {
		if vs.ctxs[i].ctx == ctx {
			vs.ctxs = append(vs.ctxs[:i], vs.ctxs[i+1:]...)
			break
		}
	}
	vs.Unlock()
}

// timeoutCause marks the deadline of one Timeout.timeout block: a context
// done with it (not with an outer one's) is this block's timeout
type timeoutCause struct{ token int64 }

func (*timeoutCause) Error() string { return "execution expired" }

// TimeoutStart starts the deadline of a Timeout.timeout block and returns
// its token: d from now, a context derived from the current one, which a
// goroutine waits on. When it passes, Timeout.__oruby_expire(token) raises
// in the block at its next instruction, and Go code waiting on GoContext()
// returns.
func (mrb *MrbState) TimeoutStart(d time.Duration) int64 {
	vs := mrb.vm()
	vs.Lock()
	vs.next++
	token := vs.next
	parent := context.Background()
	if n := len(vs.ctxs); n > 0 {
		parent = vs.ctxs[n-1].ctx
	}
	cause := &timeoutCause{token}
	ctx, cancel := context.WithTimeoutCause(parent, d, cause)
	vs.ctxs = append(vs.ctxs, ctxEntry{token, ctx})
	vs.timers[token] = cancel
	vs.Unlock()

	go func() {
		<-ctx.Done()
		if context.Cause(ctx) != cause {
			return // stopped, or an outer deadline: that one raises
		}
		vs.Lock()
		if _, live := vs.timers[token]; live && !vs.closed {
			vs.fired = append(vs.fired, token)
			C._orb_timeout_post(mrb.p)
		}
		vs.Unlock()
		vs.notify()
	}()
	return token
}

// TimeoutStop ends the deadline of a block that finished (or raised)
func (mrb *MrbState) TimeoutStop(token int64) {
	vs := mrb.vm()
	vs.Lock()
	cancel := vs.timers[token]
	delete(vs.timers, token)
	for i := len(vs.ctxs) - 1; i >= 0; i-- {
		if vs.ctxs[i].token == token {
			vs.ctxs = append(vs.ctxs[:i], vs.ctxs[i+1:]...)
			break
		}
	}
	for i := 0; i < len(vs.fired); i++ {
		if vs.fired[i] == token {
			vs.fired = append(vs.fired[:i], vs.fired[i+1:]...)
			C._orb_timeout_taken()
			i--
		}
	}
	vs.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ContextErr is for Go code that waited on GoContext() and saw it done: a
// Timeout deadline is raised now (the error to RaiseError), not at some
// later instruction; for a done EvalContext it is nil, and the VM stops at
// its next instruction.
func (mrb *MrbState) ContextErr(ctx context.Context) error {
	if c, ok := context.Cause(ctx).(*timeoutCause); ok {
		mrb.awaitFired(c.token)
	}
	return mrb.CheckSignals()
}

// awaitFired waits (briefly) for the goroutine of token's deadline to mark
// it fired: Done is visible before that goroutine has run
func (mrb *MrbState) awaitFired(token int64) {
	vs := mrb.vm()
	for i := 0; i < 20000; i++ {
		vs.Lock()
		_, live := vs.timers[token]
		fired := false
		for _, t := range vs.fired {
			fired = fired || t == token
		}
		vs.Unlock()
		if fired || !live {
			return
		}
		time.Sleep(50 * time.Microsecond)
	}
}

// takeTimeout pops a passed deadline of mrb's; 0 when none
func (mrb *MrbState) takeTimeout() int64 {
	vs := mrb.vm()
	vs.Lock()
	defer vs.Unlock()
	if len(vs.fired) == 0 {
		return 0
	}
	token := vs.fired[0]
	vs.fired = vs.fired[1:]
	C._orb_timeout_taken()
	return token
}

//export go_take_timeout
func go_take_timeout(idx C.mrb_int) C.mrb_int {
	mrb := lookupMrbStateIndex(int(idx))
	if mrb == nil {
		return 0
	}
	return C.mrb_int(mrb.takeTimeout())
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

// SignalWake is signalled when a signal or a timeout is posted for mrb;
// Go code that blocks selects on it (and on GoContext().Done()) and then runs
// CheckSignals
func (mrb *MrbState) SignalWake() <-chan struct{} {
	return mrb.vm().wake
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
	for owner {
		signo := int(C._orb_sig_take())
		if signo == 0 {
			break
		}
		if !mrb.ClassDefinedID(Sym_Signal) {
			continue
		}
		if _, err := mrb.Funcall(mrb.ModuleGetID(Sym_Signal), Sym___oruby_signal__, signo); err != nil {
			return err
		}
	}
	// deadlines of Timeout.timeout blocks that passed
	for {
		token := mrb.takeTimeout()
		if token == 0 {
			return nil
		}
		if !mrb.ClassDefinedID(Sym_Timeout) {
			continue
		}
		if _, err := mrb.Funcall(mrb.ModuleGetID(Sym_Timeout), Sym___oruby_expire, token); err != nil {
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
		mrb.vm().notify()
	}
}

// signalRelease stops passing signals and timeouts to mrb when it closes
func (mrb *MrbState) signalRelease() {
	if v, ok := vmStates.LoadAndDelete(mrb); ok {
		vs := v.(*vmState)
		vs.Lock()
		vs.closed = true
		for _, cancel := range vs.timers {
			cancel()
		}
		vs.timers = nil
		for range vs.fired {
			C._orb_timeout_taken()
		}
		vs.fired = nil
		vs.Unlock()
	}
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
