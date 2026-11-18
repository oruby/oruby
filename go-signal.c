// Delivery of asynchronous requests into a running VM: OS signals and
// Timeout.timeout deadlines.
//
// Go receives them on goroutines (signal.Notify, timers; see signal.go).
// Those cannot run Ruby, so they mark the request pending here and arm a
// one-shot code_fetch_hook. The VM calls the hook before its next
// instruction, on the thread that runs Ruby; the hook disarms itself and
// runs the request: Signal.__oruby_signal__(signo), which runs the trap
// handler or raises SignalException, or Timeout.__oruby_expire(token), which
// raises the timeout error inside the block. Both are ordinary raises at
// that instruction. Go code that blocks (sleep) takes pending requests
// itself (MrbState.CheckSignals).
//
// The hook is installed only where none is: a state that has the injector
// (Inject, the line debugger) keeps it, and the injector polls the same
// requests (_orb_vm_poll), so no previous hook has to be kept and put back.
// The hook costs nothing while nothing is pending: the VM tests
// code_fetch_hook for NULL at every instruction anyway (MRB_USE_DEBUG_HOOK).

#include <stdatomic.h>
#include <signal.h>
#include <string.h>
#include <unistd.h>
#ifndef _WIN32
#include <pthread.h>
#endif
#include "go-mrb.h"

// one bit per signal number; signals are process-wide, so is the set
static _Atomic uint64_t orb_sig_pending;
// timeouts that fired and no state has taken yet, over all states; which
// state each belongs to is kept in Go
static _Atomic int orb_timeouts_fired;

extern mrb_int go_take_timeout(mrb_int idx);

// threads (go-thread.c, thread.go): the safepoint may hand the GVL to
// another thread; it answers 1 when the current thread has an interrupt
// (Thread#raise, #kill) to take, 2 when it is not the main thread, which
// alone runs signal handlers
extern _Atomic int orb_threads_active;
extern int go_thread_safepoint(mrb_int idx);

static void orb_vm_hook(mrb_state *mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs);

// _orb_vm_arm makes mrb poll at its next instruction
void _orb_vm_arm(mrb_state *mrb) {
  if (mrb->code_fetch_hook == NULL) mrb->code_fetch_hook = orb_vm_hook;
}

// _orb_vm_hooked: the poll hook is what the state has (the injector may
// take its place)
int _orb_vm_hooked(mrb_state *mrb) {
  return mrb->code_fetch_hook == orb_vm_hook;
}

// next pending signal, cleared from the set; 0 when none is pending
int _orb_sig_take(void) {
  uint64_t bits = atomic_load(&orb_sig_pending);
  while (bits) {
    uint64_t low = bits & (~bits + 1);
    if (atomic_compare_exchange_weak(&orb_sig_pending, &bits, bits & ~low)) {
      return __builtin_ctzll(low);
    }
  }
  return 0;
}

// called from the Go signal goroutine: mark signo pending for mrb
void _orb_sig_post(mrb_state *mrb, int signo) {
  if (signo <= 0 || signo >= 64) return;
  atomic_fetch_or(&orb_sig_pending, (uint64_t)1 << signo);
  _orb_vm_arm(mrb);
}

int _orb_sig_pending_p(int signo) {
  if (signo <= 0 || signo >= 64) return 0;
  return (atomic_load(&orb_sig_pending) & ((uint64_t)1 << signo)) != 0;
}

void _orb_sig_clear(mrb_state *mrb) {
  atomic_store(&orb_sig_pending, 0);
}

// called from a Go timer goroutine: a deadline of mrb's passed
void _orb_timeout_post(mrb_state *mrb) {
  atomic_fetch_add(&orb_timeouts_fired, 1);
  _orb_vm_arm(mrb);
}

// Go took (or dropped) a fired timeout
void _orb_timeout_taken(void) {
  atomic_fetch_sub(&orb_timeouts_fired, 1);
}

static void orb_sig_dispatch(mrb_state *mrb, int signo) {
  if (!mrb_class_defined_id(mrb, MRB_SYM(Signal))) return;
  mrb_value sig = mrb_obj_value(mrb_module_get_id(mrb, MRB_SYM(Signal)));
  if (!mrb_respond_to(mrb, sig, MRB_SYM(__oruby_signal__))) return;
  mrb_funcall_id(mrb, sig, MRB_SYM(__oruby_signal__), 1, mrb_fixnum_value(signo));
}

static void orb_timeout_dispatch(mrb_state *mrb, mrb_int token) {
  if (!mrb_class_defined_id(mrb, MRB_SYM(Timeout))) return;
  mrb_value t = mrb_obj_value(mrb_module_get_id(mrb, MRB_SYM(Timeout)));
  if (!mrb_respond_to(mrb, t, MRB_SYM(__oruby_expire))) return;
  mrb_funcall_id(mrb, t, MRB_SYM(__oruby_expire), 1, mrb_fixnum_value(token));
}

static void orb_thread_interrupt(mrb_state *mrb) {
  if (!mrb_class_defined_id(mrb, MRB_SYM(Thread))) return;
  mrb_value t = mrb_obj_value(mrb_class_get_id(mrb, MRB_SYM(Thread)));
  if (!mrb_respond_to(mrb, t, MRB_SYM(__oruby_interrupt))) return;
  mrb_funcall_id(mrb, t, MRB_SYM(__oruby_interrupt), 0);
}

// _orb_vm_poll runs one pending request of mrb's, at an instruction
// boundary (the hook, the injector); the rest are polled at the next
// instruction, so one that raises does not drop them
void _orb_vm_poll(mrb_state *mrb) {
  int main_thread = 1;
  if (atomic_load(&orb_threads_active)) {
    int r = go_thread_safepoint(_mrb_get_idx(mrb));
    main_thread = !(r & 2);
    if (r & 1) {
      if (atomic_load(&orb_sig_pending) || atomic_load(&orb_timeouts_fired)) _orb_vm_arm(mrb);
      orb_thread_interrupt(mrb);
    }
  }
  if (!atomic_load(&orb_timeouts_fired) && (!main_thread || !atomic_load(&orb_sig_pending))) return;
  int signo = main_thread ? _orb_sig_take() : 0;
  if (signo) {
    if (atomic_load(&orb_sig_pending) || atomic_load(&orb_timeouts_fired)) _orb_vm_arm(mrb);
    orb_sig_dispatch(mrb, signo);
    return;
  }
  mrb_int token = go_take_timeout(_mrb_get_idx(mrb));
  if (token) {
    if (atomic_load(&orb_timeouts_fired)) _orb_vm_arm(mrb);
    orb_timeout_dispatch(mrb, token);
  }
}

static void orb_vm_hook(mrb_state *mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs) {
  // disarm first: a request posted from now on arms the hook again
  if (mrb->code_fetch_hook == orb_vm_hook) mrb->code_fetch_hook = NULL;
  _orb_vm_poll(mrb);
}

// Set signo to the OS default action. Go's signal.Reset gives Go's own
// default (a SIGUSR1 nobody waits for is dropped, SIGPIPE only ends the
// program on stdout/stderr), so after signal.Ignore releases it from the Go
// runtime this installs SIG_DFL itself, as MRI's SYSTEM_DEFAULT does.
void _orb_sig_dfl(int signo) {
#ifndef _WIN32
  struct sigaction sa;
  memset(&sa, 0, sizeof(sa));
  sa.sa_handler = SIG_DFL;
  sigemptyset(&sa.sa_mask);
  sigaction(signo, &sa, NULL);
#else
  signal(signo, SIG_DFL);
#endif
}

// End the process with signo, as an uncaught SignalException does in MRI.
void _orb_sig_kill_self(int signo) {
  _orb_sig_dfl(signo);
#ifndef _WIN32
  sigset_t set;
  sigemptyset(&set);
  sigaddset(&set, signo);
  pthread_sigmask(SIG_UNBLOCK, &set, NULL);
  kill(getpid(), signo);
#else
  raise(signo);
#endif
}
