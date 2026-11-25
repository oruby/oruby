#ifndef GO_THREAD_H
#define GO_THREAD_H

#include <mruby.h>

// The per-thread part of an mrb_state (thread.go): what a Ruby Thread
// takes with it when it gives up the GVL and puts back when it takes the
// GVL again. Everything else in the state is shared by all threads.
typedef struct orb_tstate {
  struct mrb_context *c;      // the running context (a fiber in the thread)
  struct mrb_context *root_c; // the thread's root context
  struct mrb_jmpbuf *jmp;     // its innermost catch, on its own C stack
  struct RObject *exc;        // its pending exception
  struct RBasic **arena;      // its GC arena
  int arena_capa;
  int arena_idx;
  void *c_stack_limit;
  int owned_arena;            // the arena buffer is the thread's own (not the state's)
} orb_tstate;

orb_tstate *_orb_tstate_main(mrb_state *mrb, mrb_value *owner);
orb_tstate *_orb_tstate_new(mrb_state *mrb, mrb_value *owner, mrb_value body);
void _orb_tstate_save(mrb_state *mrb, orb_tstate *st, mrb_value keep);
void _orb_tstate_load(mrb_state *mrb, orb_tstate *st, mrb_value keep);
void _orb_tstate_finish(mrb_state *mrb, orb_tstate *st);
void _orb_tstate_free(mrb_state *mrb, orb_tstate *st);
mrb_value _orb_tstate_fiber(mrb_state *mrb, orb_tstate *st);
int _orb_thread_run(mrb_state *mrb, mrb_value body, mrb_value *result);

// Fibers over goroutines (fiber_go.go): the context of a Fiber object, made
// as a thread's is, and the calls that switch it
orb_tstate *_orb_fiber_init(mrb_state *mrb, mrb_value fiber, mrb_value body);
orb_tstate *_orb_tstate_alloc(void);
void _orb_ctx_status(mrb_state *mrb, int status);
int _orb_fiber_run(mrb_state *mrb, mrb_value body, mrb_int argc, const mrb_value *argv, mrb_value *result);
void _orb_threads_active(int delta);

#endif
