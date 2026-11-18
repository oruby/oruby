// Ruby threads over one mrb_state (thread.go): each Ruby Thread runs on its
// own goroutine, and so on its own C stack, and only the one holding the GVL
// runs Ruby. What is per thread in the state is swapped at each handoff:
// the running and root contexts, the innermost catch (a jmp_buf on that
// thread's C stack), the pending exception and the GC arena.
//
// The GC marks only the current context, root context and arena. A switched
// out thread is kept alive by its owner: an RFiber over its root context
// (MRB_TT_FIBER marks the whole context while it is not terminated) and an
// Array holding what its arena protected.
#include <stdatomic.h>
#include <string.h>
#include "go-mrb.h"
#include "go-thread.h"
#include "mruby/object.h"

#ifndef MRB_GC_ARENA_SIZE
#define MRB_GC_ARENA_SIZE 100
#endif

/* mruby's STACK_INIT_SIZE and CALLINFO_INIT_SIZE (vm.c); the VM grows both */
#define ORB_STACK_INIT 128
#define ORB_CI_INIT 32

// states with more than their main thread; the VM poll asks Go only then
_Atomic int orb_threads_active;

void _orb_threads_active(int delta) {
  atomic_fetch_add(&orb_threads_active, delta);
}

static struct RFiber *
orb_owner(mrb_state *mrb, struct mrb_context *c, int borrowed)
{
  struct RClass *fc = mrb_class_get_id(mrb, MRB_SYM(Fiber));
  struct RFiber *f = MRB_OBJ_ALLOC(mrb, MRB_TT_FIBER, fc);
  if (borrowed) f->flags |= MRB_FL_FIBER_BORROWED;
  f->cxt = c;
  c->fib = f;
  return f;
}

// the main thread: the state's own root context, its root Fiber as owner
orb_tstate *
_orb_tstate_main(mrb_state *mrb, mrb_value *owner)
{
  orb_tstate *st = (orb_tstate *)calloc(1, sizeof(orb_tstate));
  struct mrb_context *root = mrb->root_c;
  if (!root->fib) orb_owner(mrb, root, 1);
  *owner = mrb_obj_value(root->fib);
  st->owned_arena = 0;
  return st;
}

// a new thread: a fresh root context (the VM allocates its stacks at the
// first call) owned by a Fiber, and an arena of its own
orb_tstate *
_orb_tstate_new(mrb_state *mrb, mrb_value *owner, mrb_value body)
{
  struct mrb_context *c = (struct mrb_context *)mrb_malloc(mrb, sizeof(struct mrb_context));
  memset(c, 0, sizeof(*c));
  c->status = MRB_FIBER_RUNNING; /* a running thread's root, never resumed */
  /* the stacks as mruby-fiber's fiber_init makes them: the base frame
     holds the body (Fiber#inspect and backtraces read it) */
  c->stbase = (mrb_value *)mrb_malloc(mrb, ORB_STACK_INIT * sizeof(mrb_value));
  c->stend = c->stbase + ORB_STACK_INIT;
  for (int i = 0; i < ORB_STACK_INIT; i++) c->stbase[i] = mrb_nil_value();
  c->cibase = (mrb_callinfo *)mrb_calloc(mrb, ORB_CI_INIT, sizeof(mrb_callinfo));
  c->ciend = c->cibase + ORB_CI_INIT;
  c->ci = c->cibase;
  c->ci->u.target_class = mrb->object_class;
  c->ci->proc = mrb_proc_ptr(body);
  c->ci->stack = c->stbase;
  c->ci->vis = 1; /* private, as the top level */
  struct RFiber *f = orb_owner(mrb, c, 0);
  *owner = mrb_obj_value(f);

  orb_tstate *st = (orb_tstate *)calloc(1, sizeof(orb_tstate));
  st->c = st->root_c = c;
  st->arena_capa = MRB_GC_ARENA_SIZE;
  st->arena = (struct RBasic **)mrb_malloc(mrb, sizeof(struct RBasic *) * st->arena_capa);
  st->arena_idx = 0;
  st->owned_arena = 1;
  return st;
}

// save the current thread's part of mrb into st; keep roots what its arena
// protects while another thread's arena is the state's
void
_orb_tstate_save(mrb_state *mrb, orb_tstate *st, mrb_value keep)
{
  mrb_ary_clear(mrb, keep);
  int n = mrb->gc.arena_idx;
  for (int i = 0; i < n; i++) {
    mrb_ary_push(mrb, keep, mrb_obj_value(mrb->gc.arena[i]));
  }
  if (mrb->exc) mrb_ary_push(mrb, keep, mrb_obj_value(mrb->exc));
  // incremental GC: what ran since a context was marked is marked again
  if (mrb->c->fib) mrb_write_barrier(mrb, (struct RBasic *)mrb->c->fib);
  if (mrb->root_c->fib) mrb_write_barrier(mrb, (struct RBasic *)mrb->root_c->fib);

  st->c = mrb->c;
  st->root_c = mrb->root_c;
  st->jmp = mrb->jmp;
  st->exc = mrb->exc;
  st->arena = mrb->gc.arena;
  st->arena_capa = mrb->gc.arena_capa;
  st->arena_idx = mrb->gc.arena_idx;
#ifdef MRB_USE_C_STACK_CHECK
  st->c_stack_limit = mrb->c_stack_limit;
#endif
}

// make st's thread the current one
void
_orb_tstate_load(mrb_state *mrb, orb_tstate *st, mrb_value keep)
{
  mrb->c = st->c;
  mrb->root_c = st->root_c;
  mrb->jmp = st->jmp;
  mrb->exc = st->exc;
  mrb->gc.arena = st->arena;
  mrb->gc.arena_capa = st->arena_capa;
  mrb->gc.arena_idx = st->arena_idx;
#ifdef MRB_USE_C_STACK_CHECK
  mrb->c_stack_limit = st->c_stack_limit;
#endif
  // its arena is marked again as the state's
  mrb_ary_clear(mrb, keep);
}

// the thread's body returned: its root context is over, and the GC frees
// it with the owner
void
_orb_tstate_finish(mrb_state *mrb, orb_tstate *st)
{
  if (st->root_c) st->root_c->status = MRB_FIBER_TERMINATED;
}

// called once another thread's state is loaded
void
_orb_tstate_free(mrb_state *mrb, orb_tstate *st)
{
  if (st->owned_arena && st->arena) mrb_free(mrb, st->arena);
  free(st);
}

// the Fiber a switched out thread runs (Thread#[] of another thread)
mrb_value
_orb_tstate_fiber(mrb_state *mrb, orb_tstate *st)
{
  if (st->c && st->c->fib) return mrb_obj_value(st->c->fib);
  if (st->root_c && st->root_c->fib) return mrb_obj_value(st->root_c->fib);
  return mrb_nil_value();
}

static mrb_value
orb_thread_body(mrb_state *mrb, void *data)
{
  return mrb_funcall_id(mrb, *(mrb_value *)data, MRB_SYM(call), 0);
}

// run a thread's body (a Proc) in its fresh context
int
_orb_thread_run(mrb_state *mrb, mrb_value body, mrb_value *result)
{
  return _go_mrb_protect(mrb, orb_thread_body, &body, result);
}
