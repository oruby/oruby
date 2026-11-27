package capi

/*
#include <errno.h>
#include <pthread.h>
#include <signal.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "capi.h"

typedef void *(*orb_nogvl_fn)(void *);
typedef void (*orb_ubf_fn)(void *);

// a region in progress: the thread running the function (for RUBY_UBF_IO), if
// it still is in it, and the function's errno
typedef struct {
	pthread_t tid;
	volatile int running;
	int err;
} orb_nogvl_t;

static void orb_nogvl_noop(int sig) { (void)sig; }

// RUBY_UBF_IO: SIGVTALRM, which MRI's timer thread sends too, fails a
// blocking system call with EINTR. The handler has no SA_RESTART.
static void orb_nogvl_install(void) {
	struct sigaction sa;
	memset(&sa, 0, sizeof sa);
	sa.sa_handler = orb_nogvl_noop;
	sigemptyset(&sa.sa_mask);
	sigaction(SIGVTALRM, &sa, NULL);
}

static void *orb_nogvl_call(orb_nogvl_t *st, uintptr_t fn, uintptr_t d) {
	st->tid = pthread_self();
	__sync_synchronize();
	st->running = 1;
	errno = 0;
	void *r = ((orb_nogvl_fn)fn)((void *)d);
	st->err = errno;
	st->running = 0;
	return r;
}

static void orb_nogvl_ubf(orb_nogvl_t *st, uintptr_t ubf, uintptr_t d2) {
	if (ubf == (uintptr_t)-1) {
		if (st->running) pthread_kill(st->tid, SIGVTALRM);
	} else if (ubf != 0) {
		((orb_ubf_fn)ubf)((void *)d2);
	}
}
*/
import "C"

import (
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/oruby/oruby"
)

var nogvlOnce sync.Once

// defineNogvl adds ORubyCAPI.__nogvl(fn, data, ubf, ubf_data, flags), what
// rb_thread_call_without_gvl and rb_nogvl run through (the pointers are
// Integers). It runs fn without the GVL, so other Ruby threads run; when the
// thread is woken (Thread#wakeup), interrupted (Thread#raise, #kill) or
// signalled, ubf is called, again every 10 ms while fn has not returned.
func defineNogvl(mrb *oruby.MrbState) {
	nogvlOnce.Do(func() {
		mod := mrb.ModuleGet("ORubyCAPI")
		mrb.DefineModuleFunction(mod, "__nogvl", nogvl, mrb.ArgsReq(5))
	})
}

const (
	nogvlIntrFail        = 0x1
	nogvlPendingIntrFail = 0x8
)

func nogvl(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	fn, d := C.uintptr_t(a[0].Int64()), C.uintptr_t(a[1].Int64())
	ubf, d2 := C.uintptr_t(a[2].Int64()), C.uintptr_t(a[3].Int64())
	flags := int(a[4].Int64())

	// an interrupt already queued: the function is not run
	if t := mrb.CurrentThread(); t != nil && flags&(nogvlIntrFail|nogvlPendingIntrFail) != 0 && t.InterruptPending() {
		if flags&nogvlIntrFail != 0 && flags&nogvlPendingIntrFail == 0 {
			C.orb_capi_set_nogvl_errno(C.int(syscall.EINTR))
		}
		return mrb.FixnumValue(0)
	}

	st := (*C.orb_nogvl_t)(C.calloc(1, C.size_t(unsafe.Sizeof(C.orb_nogvl_t{}))))
	defer C.free(unsafe.Pointer(st))
	if ubf == ^C.uintptr_t(0) {
		C.orb_nogvl_install()
	}

	wake := mrb.SignalWake()
	select { // what was posted before is not for this wait
	case <-wake:
	default:
	}
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-wake:
		case <-stop:
			return
		}
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			C.orb_nogvl_ubf(st, ubf, d2)
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()

	var ret C.uintptr_t
	mrb.Blocking(func() {
		ret = C.uintptr_t(uintptr(unsafe.Pointer(C.orb_nogvl_call(st, fn, d))))
	})
	close(stop)
	<-finished
	C.orb_capi_set_nogvl_errno(st.err)
	return mrb.Int64Value(int64(ret))
}
