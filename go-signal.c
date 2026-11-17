// Signal delivery into a running VM.
//
// Go receives OS signals on a goroutine (signal.Notify, see signal.go). That
// goroutine cannot run Ruby, so it marks the signal pending here and arms a
// one-shot code_fetch_hook. The VM calls the hook before its next instruction,
// on the thread that runs Ruby; the hook disarms itself and calls
// Signal.__oruby_signal__(signo), which runs the trap handler or raises the
// SignalException as an ordinary raise at that instruction. A state not
// running Ruby when the signal arrives picks it up at its next instruction;
// Go code that blocks (sleep) or signals itself (Process.kill) takes pending
// signals with _orb_sig_take and runs them itself (MrbState.CheckSignals).
//
// The hook costs nothing while no signal is pending: the VM tests
// code_fetch_hook for NULL at every instruction anyway (MRB_USE_DEBUG_HOOK).

#include <stdatomic.h>
#include <signal.h>
#include <string.h>
#include <unistd.h>
#ifndef _WIN32
#include <pthread.h>
#endif
#include "go-mrb.h"

typedef void (*orb_fetch_hook)(mrb_state *mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs);

// one bit per signal number; signals are process-wide, so is the set
static _Atomic uint64_t orb_sig_pending;
// the hook that was installed when the signal hook was armed (the injector)
static _Atomic(orb_fetch_hook) orb_sig_prev;

static void orb_sig_hook(mrb_state *mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs);

static void orb_sig_arm(mrb_state *mrb) {
  orb_fetch_hook h = mrb->code_fetch_hook;
  if (h != orb_sig_hook) {
    atomic_store(&orb_sig_prev, h);
    mrb->code_fetch_hook = orb_sig_hook;
  }
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
  orb_sig_arm(mrb);
}

int _orb_sig_pending_p(int signo) {
  if (signo <= 0 || signo >= 64) return 0;
  return (atomic_load(&orb_sig_pending) & ((uint64_t)1 << signo)) != 0;
}

void _orb_sig_clear(mrb_state *mrb) {
  atomic_store(&orb_sig_pending, 0);
  if (mrb && mrb->code_fetch_hook == orb_sig_hook) {
    mrb->code_fetch_hook = atomic_load(&orb_sig_prev);
  }
}

static void orb_sig_dispatch(mrb_state *mrb, int signo) {
  if (!mrb_class_defined_id(mrb, MRB_SYM(Signal))) return;
  mrb_value sig = mrb_obj_value(mrb_module_get_id(mrb, MRB_SYM(Signal)));
  if (!mrb_respond_to(mrb, sig, MRB_SYM(__oruby_signal__))) return;
  mrb_funcall_id(mrb, sig, MRB_SYM(__oruby_signal__), 1, mrb_fixnum_value(signo));
}

static void orb_sig_hook(mrb_state *mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs) {
  // disarm first: a signal posted from now on arms the hook again
  orb_fetch_hook prev = atomic_load(&orb_sig_prev);
  mrb->code_fetch_hook = prev;
  int signo = _orb_sig_take();
  if (signo) {
    // the others run at the next instruction, so one that raises does not
    // drop them
    if (atomic_load(&orb_sig_pending)) orb_sig_arm(mrb);
    orb_sig_dispatch(mrb, signo);
  }
  if (prev) prev(mrb, irep, pc, regs);
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
