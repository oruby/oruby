// TracePoint (core_ext's 44_tracepoint.rb) over mruby's code_fetch_hook.
//
// While a TracePoint is enabled the state's code_fetch_hook is
// orb_trace_hook, called before every instruction. It polls what the
// one-shot request hook of go-signal.c would (signals, timeouts, thread
// switches), then reports the events at that instruction to
// TracePoint.__oruby_fire(event, self, method, class, value, path, line, proc):
//
//   :call, :b_call, :class   the first instruction of a method's, a block's or
//                            a class body's code
//   :return, :b_return, :end a return from it
//   :line                    the frame's line changes (or a loop jumps back to
//                            the start of a line)
//   :c_call, :c_return       a call of a method written in C, or of a core
//                            method written in Ruby, and the instruction after it
//   :raise, :rescue          a rescue clause's first instruction (OP_EXCEPT):
//                            the exception mruby raised (go-backtrace.c reports
//                            it, _orb_trace_raise), where it was raised
//   :script_compiled         eval's source (go-mrb.c, _orb_trace_script)
//
// Core code (mrblib/, mrbgems/, <internal:...>) has no events of its own, as
// MRI's C code has none. A handler runs with tracing off (TracePoint.allow_reentry
// turns it on again) and with $! cleared. Thread events are fired from Ruby.
#include <string.h>
#include <stdlib.h>
#include "go-mrb.h"
#include <mruby/opcode.h>
#include <mruby/proc.h>
#include <mruby/debug.h>

enum {
  EV_LINE = 1, EV_CALL = 2, EV_RETURN = 4, EV_C_CALL = 8, EV_C_RETURN = 16, EV_RAISE = 32,
  EV_B_CALL = 64, EV_B_RETURN = 128, EV_CLASS = 256, EV_END = 512, EV_RESCUE = 1024,
  EV_SCRIPT = 2048,
};

void _orb_vm_poll(mrb_state *mrb);
void _orb_vm_arm(mrb_state *mrb);
mrb_value mrb_binding_new(mrb_state *mrb, const struct RProc *proc, mrb_value recv, struct REnv *env);
struct REnv *mrb_vm_ci_env_reify(mrb_state *mrb, struct mrb_context *c, mrb_callinfo *ci);

typedef struct {
  const struct mrb_context *c;
  const mrb_irep *irep;
  int32_t line;
  uint32_t off;
  const mrb_code *cret;  /* a C method called: the instruction after the call */
  mrb_sym cmid;
  struct RClass *cklass;
  mrb_value cself;
  uint8_t creg;
  const mrb_code *cls;   /* a class or module opened: the instruction after */
  uint32_t cls_off;
  uint8_t cls_reg;
} orb_frame;

typedef struct {
  mrb_state *mrb;
  uint32_t mask;
  int busy;
  orb_frame *frames;
  size_t cap;
  const mrb_irep *last_irep;
  int last_internal;
  /* the exception being raised and where (for :raise) */
  struct RObject *exc;
  const mrb_irep *exc_irep;
  uint32_t exc_off;
  /* the last class or module opened: :class is at its line */
  const mrb_irep *cls_irep;
  uint32_t cls_off;
  /* the frame of the event a handler runs for (TracePoint#binding) */
  const struct mrb_context *ev_c;
  ptrdiff_t ev_ci;
} orb_trace;

#define ORB_TRACE_STATES 8
static orb_trace traces[ORB_TRACE_STATES];

static orb_trace *trace_of(mrb_state *mrb, int create) {
  for (int i = 0; i < ORB_TRACE_STATES; i++) {
    if (traces[i].mrb == mrb) return &traces[i];
  }
  if (!create) return NULL;
  for (int i = 0; i < ORB_TRACE_STATES; i++) {
    if (!traces[i].mrb) {
      memset(&traces[i], 0, sizeof(traces[i]));
      traces[i].mrb = mrb;
      traces[i].ev_ci = -1;
      return &traces[i];
    }
  }
  return NULL;
}

static int file_internal(const char *f) {
  return f && (strncmp(f, "mrblib/", 7) == 0 || strncmp(f, "mrbgems/", 8) == 0 ||
               strncmp(f, "<internal:", 10) == 0);
}

static int irep_internal(mrb_state *mrb, orb_trace *t, const mrb_irep *irep) {
  if (irep != t->last_irep) {
    t->last_irep = irep;
    t->last_internal = !irep->debug_info || file_internal(mrb_debug_get_filename(mrb, irep, 0));
  }
  return t->last_internal;
}

static orb_frame *frame_at(orb_trace *t, size_t depth) {
  if (depth >= t->cap) {
    size_t cap = t->cap ? t->cap * 2 : 64;
    while (cap <= depth) cap *= 2;
    orb_frame *f = (orb_frame *)realloc(t->frames, cap * sizeof(orb_frame));
    if (!f) return NULL;
    memset(f + t->cap, 0, (cap - t->cap) * sizeof(orb_frame));
    t->frames = f;
    t->cap = cap;
  }
  return &t->frames[depth];
}

static struct RClass *real_class(struct RClass *c) {
  if (c && c->tt == MRB_TT_ICLASS) return c->c;
  return c;
}

typedef struct {
  mrb_value argv[8];
} fire_args;

static mrb_value fire_body(mrb_state *mrb, void *ud) {
  fire_args *a = (fire_args *)ud;
  struct RClass *tp = mrb_class_get(mrb, "TracePoint");
  return mrb_funcall_argv(mrb, mrb_obj_value(tp), mrb_intern_lit(mrb, "__oruby_fire"), 8, a->argv);
}

/* runs the handlers for one event; a handler's exception goes on from the
   traced code */
static void fire(mrb_state *mrb, orb_trace *t, const char *event, mrb_value self, mrb_sym mid,
                 struct RClass *klass, mrb_value value, const mrb_irep *irep, uint32_t off,
                 const struct RProc *proc) {
  int ai = mrb_gc_arena_save(mrb);
  int32_t line = 0;
  const char *file = NULL;
  if (irep) mrb_debug_get_position(mrb, irep, off, &line, &file);
  fire_args a;
  a.argv[0] = mrb_symbol_value(mrb_intern_cstr(mrb, event));
  a.argv[1] = self;
  a.argv[2] = mid ? mrb_symbol_value(mid) : mrb_nil_value();
  a.argv[3] = klass ? mrb_obj_value(real_class(klass)) : mrb_nil_value();
  a.argv[4] = value;
  a.argv[5] = file ? mrb_str_new_cstr(mrb, file) : mrb_nil_value();
  a.argv[6] = mrb_fixnum_value(line);
  a.argv[7] = proc ? mrb_obj_value((struct RProc *)proc) : mrb_nil_value();
  t->busy = 1;
  /* $! is nil in the handler */
  mrb_sym errsym = mrb_intern_lit(mrb, "$!");
  mrb_value err = mrb_gv_get(mrb, errsym);
  struct RObject *pending = mrb->exc;
  mrb->exc = NULL;
  mrb_gv_set(mrb, errsym, mrb_nil_value());
  const struct mrb_context *ev_c = t->ev_c;
  ptrdiff_t ev_ci = t->ev_ci;
  t->ev_c = mrb->c;
  t->ev_ci = mrb->c->ci - mrb->c->cibase;
  mrb_bool failed = FALSE;
  mrb_value r = mrb_protect_error(mrb, fire_body, &a, &failed);
  t->busy = 0;
  t->ev_c = ev_c;
  t->ev_ci = ev_ci;
  mrb_gv_set(mrb, errsym, err);
  mrb->exc = pending;
  mrb_gc_arena_restore(mrb, ai);
  if (failed) mrb_exc_raise(mrb, r);
}

static uint32_t insn_len(const mrb_code *p, uint8_t *opp, uint32_t *ap, uint32_t *bp);

/* the name a method frame was called by: mruby's ci->mid is an alias's
   original name; the caller's send has the alias */
static mrb_sym callee_of(mrb_state *mrb, mrb_callinfo *ci) {
  if (ci <= mrb->c->cibase) return ci->mid;
  mrb_callinfo *up = ci - 1;
  const struct RProc *p = up->proc;
  if (!p || MRB_PROC_CFUNC_P(p) || !p->body.irep || !up->pc) return ci->mid;
  const mrb_irep *irep = p->body.irep;
  /* the instruction before up->pc */
  uint32_t target = (uint32_t)(up->pc - irep->iseq), i = 0;
  while (i < irep->ilen) {
    uint8_t op;
    uint32_t a, b, n = insn_len(irep->iseq + i, &op, &a, &b);
    if (i + n >= target) {
      if (i + n == target && (op == OP_SEND || op == OP_SEND0 || op == OP_SENDB || op == OP_SSEND ||
                              op == OP_SSEND0 || op == OP_SSENDB) && b < irep->slen) {
        return irep->syms[b];
      }
      break;
    }
    i += n;
  }
  return ci->mid;
}

static void orb_trace_hook(mrb_state *mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs) {
  _orb_vm_poll(mrb);
  orb_trace *t = trace_of(mrb, 0);
  if (!t || !t->mask || t->busy || !irep || !irep->iseq) return;
  mrb_callinfo *ci = mrb->c->ci;
  size_t depth = (size_t)(ci - mrb->c->cibase);
  orb_frame *fr = frame_at(t, depth);
  if (!fr) return;
  uint32_t off = (uint32_t)(pc - irep->iseq);
  int fresh = fr->c != mrb->c || fr->irep != irep;

  /* the C method called from here returned */
  if (fr->cret) {
    if (!fresh && fr->cret == pc && (t->mask & EV_C_RETURN)) {
      mrb_value self = fr->cself, v = regs[fr->creg];
      mrb_sym mid = fr->cmid;
      struct RClass *k = fr->cklass;
      fr->cret = NULL;
      fire(mrb, t, "c_return", self, mid, k, v, irep, off, NULL);
    }
    fr->cret = NULL;
  }
  if (irep_internal(mrb, t, irep)) return;

  uint8_t op;
  uint32_t a, b;
  uint32_t len = insn_len(pc, &op, &a, &b);

  /* a class or module opened without a body (class Foo; end): no code of
     its own runs, so its :class and :end are here */
  if (fr->cls) {
    const mrb_code *after = fr->cls;
    fr->cls = NULL;
    if (!fresh && after == pc && op != OP_EXEC) {
      mrb_value k = regs[fr->cls_reg];
      uint32_t co = fr->cls_off;
      if (t->mask & EV_CLASS) fire(mrb, t, "class", k, 0, NULL, mrb_nil_value(), irep, co, NULL);
      if (t->mask & EV_END) fire(mrb, t, "end", k, 0, NULL, mrb_nil_value(), irep, co, NULL);
      fr = frame_at(t, depth);
    }
  }

  const struct RProc *proc = ci->proc;
  int scope = proc && MRB_PROC_SCOPE_P(proc);
  int method = scope && ci->mid && depth > 0;
  int klass_body = scope && !ci->mid && depth > 0 && (mrb_class_p(regs[0]) || mrb_module_p(regs[0]));

  if (off == 0) {
    fr->line = 0;
    fr->off = 0;
    if (method && (t->mask & EV_CALL)) {
      fire(mrb, t, "call", regs[0], callee_of(mrb, ci), mrb_vm_ci_target_class(ci), mrb_nil_value(), irep, 0, proc);
    }
    else if (!scope && (t->mask & EV_B_CALL)) {
      fire(mrb, t, "b_call", regs[0], ci->mid, NULL, mrb_nil_value(), irep, 0, proc);
    }
    else if (klass_body && (t->mask & EV_CLASS)) {
      const mrb_irep *ci_irep = t->cls_irep ? t->cls_irep : irep;
      uint32_t co = t->cls_irep ? t->cls_off : 0;
      t->cls_irep = NULL;
      fire(mrb, t, "class", regs[0], 0, NULL, mrb_nil_value(), ci_irep, co, NULL);
    }
  }
  fr = frame_at(t, depth);
  fr->c = mrb->c;
  fr->irep = irep;

  /* a rescue clause: the exception raised, and rescued */
  if (op == OP_EXCEPT && mrb->exc && (t->mask & (EV_RAISE | EV_RESCUE))) {
    mrb_value exc = mrb_obj_value(mrb->exc);
    if (t->exc == mrb->exc && (t->mask & EV_RAISE)) {
      const mrb_irep *ri = t->exc_irep ? t->exc_irep : irep;
      uint32_t ro = t->exc_irep ? t->exc_off : off;
      t->exc = NULL;
      fire(mrb, t, "raise", regs[0], ci->mid, NULL, exc, ri, ro, NULL);
    }
    t->exc = NULL;
    if (t->mask & EV_RESCUE) fire(mrb, t, "rescue", regs[0], ci->mid, NULL, exc, irep, off, NULL);
  }

  /* argument setup has the def's or the block's line, which is no line of
     code MRI reports */
  int empty_body = op == OP_RETNIL && (off == 0 || (off == 4 && irep->iseq[0] == OP_ENTER));
  if ((t->mask & EV_LINE) && op != OP_ENTER && !empty_body) {
    int32_t line = mrb_debug_get_line(mrb, irep, off);
    int backjump = !fresh && off < fr->off && line == fr->line && line > 0 &&
                   (off == 0 || mrb_debug_get_line(mrb, irep, off - 1) != line);
    if (line > 0 && (line != fr->line || backjump)) {
      fr->line = line;
      fr->off = off;
      fire(mrb, t, "line", regs[0], ci->mid, NULL, mrb_nil_value(), irep, off, NULL);
      fr = frame_at(t, depth);
    }
  }
  fr->off = off;

  mrb_value rv = mrb_nil_value();
  switch (op) {
  case OP_RETURN: case OP_RETURN_BLK: case OP_BREAK: rv = regs[a]; break;
  case OP_RETSELF: rv = regs[0]; break;
  case OP_RETTRUE: rv = mrb_true_value(); break;
  case OP_RETFALSE: rv = mrb_false_value(); break;
  default: break;
  }
  switch (op) {
  case OP_CLASS: case OP_MODULE: case OP_SCLASS:
    if (t->mask & (EV_CLASS | EV_END)) {
      fr->cls = pc + len;
      fr->cls_off = off;
      fr->cls_reg = (uint8_t)a;
      t->cls_irep = irep;
      t->cls_off = off;
    }
    break;
  case OP_RETURN: case OP_RETURN_BLK: case OP_BREAK:
  case OP_RETSELF: case OP_RETNIL: case OP_RETTRUE: case OP_RETFALSE:
    if (method && (t->mask & EV_RETURN)) {
      fire(mrb, t, "return", regs[0], callee_of(mrb, ci), mrb_vm_ci_target_class(ci), rv, irep, off, proc);
    }
    else if (!scope && (t->mask & EV_B_RETURN)) {
      fire(mrb, t, "b_return", regs[0], ci->mid, NULL, rv, irep, off, proc);
    }
    else if (klass_body && (t->mask & EV_END)) {
      fire(mrb, t, "end", regs[0], 0, NULL, mrb_nil_value(), irep, off, NULL);
    }
    fr = frame_at(t, depth);
    fr->irep = NULL;
    break;
  case OP_SEND: case OP_SEND0: case OP_SENDB: case OP_SSEND: case OP_SSEND0: case OP_SSENDB: {
    if (!(t->mask & (EV_C_CALL | EV_C_RETURN)) || b >= irep->slen) break;
    mrb_value recv = (op == OP_SSEND || op == OP_SSEND0 || op == OP_SSENDB) ? regs[0] : regs[a];
    mrb_sym mid = irep->syms[b];
    struct RClass *cls = mrb_class(mrb, recv);
    mrb_method_t m = mrb_method_search_vm(mrb, &cls, mid);
    if (MRB_METHOD_UNDEF_P(m)) break;
    int c_method = MRB_METHOD_CFUNC_P(m);
    if (!c_method) {
      const struct RProc *p = MRB_METHOD_PROC(m);
      c_method = p && !MRB_PROC_CFUNC_P(p) && p->body.irep && p->body.irep->debug_info &&
                 file_internal(mrb_debug_get_filename(mrb, p->body.irep, 0));
    }
    if (!c_method) break;
    if (t->mask & EV_C_CALL) {
      fire(mrb, t, "c_call", recv, mid, cls, mrb_nil_value(), irep, off, NULL);
      fr = frame_at(t, depth);
    }
    fr->cret = pc + len;
    fr->cmid = mid;
    fr->cklass = cls;
    fr->cself = recv;
    fr->creg = (uint8_t)a;
    break;
  }
  default: break;
  }
}

/* an instruction's length, opcode and its a and b operands */
enum { OT_Z, OT_B, OT_BB, OT_BBB, OT_BS, OT_BSS, OT_S, OT_W };
static const uint8_t trace_op_types[] = {
#define OPCODE(x, t) OT_##t,
#include <mruby/ops.h>
#undef OPCODE
};

static uint32_t rd(const mrb_code *p, int n) { return n == 2 ? (uint32_t)(p[0] << 8 | p[1]) : p[0]; }

static uint32_t insn_len(const mrb_code *p, uint8_t *opp, uint32_t *ap, uint32_t *bp) {
  uint32_t i = 0;
  int ext = 0;
  uint8_t op = p[i++];
  if (op == OP_EXT1 || op == OP_EXT2 || op == OP_EXT3) {
    ext = op - OP_EXT1 + 1;
    op = p[i++];
  }
  *opp = op;
  *ap = *bp = 0;
  if (op >= sizeof(trace_op_types)) return i;
  int asz = (ext & 1) ? 2 : 1, bsz = (ext & 2) ? 2 : 1;
  switch (trace_op_types[op]) {
  case OT_Z: break;
  case OT_B: *ap = rd(p + i, asz); i += asz; break;
  case OT_BB: *ap = rd(p + i, asz); i += asz; *bp = rd(p + i, bsz); i += bsz; break;
  case OT_BBB: *ap = rd(p + i, asz); i += asz; *bp = rd(p + i, bsz); i += bsz + 1; break;
  case OT_BS: *ap = rd(p + i, asz); i += asz + 2; break;
  case OT_BSS: *ap = rd(p + i, asz); i += asz + 4; break;
  case OT_S: i += 2; break;
  case OT_W: i += 3; break;
  }
  return i;
}

/* the exception mruby is raising (go-backtrace.c's $! getter) */
void _orb_trace_raise(mrb_state *mrb, struct RObject *exc) {
  orb_trace *t = trace_of(mrb, 0);
  if (!t || !(t->mask & EV_RAISE) || t->busy || t->exc == exc) return;
  t->exc = exc;
  t->exc_irep = NULL;
  /* where: the nearest frame running Ruby code */
  for (mrb_callinfo *ci = mrb->c->ci; ci >= mrb->c->cibase; ci--) {
    const struct RProc *p = ci->proc;
    if (p && !MRB_PROC_CFUNC_P(p) && p->body.irep && ci->pc) {
      t->exc_irep = p->body.irep;
      t->exc_off = (uint32_t)(ci->pc - p->body.irep->iseq);
      if (t->exc_off > 0) t->exc_off--;
      break;
    }
  }
  /* kept from the GC until it is reported */
  mrb_iv_set(mrb, mrb_obj_value(mrb->kernel_module), mrb_intern_lit(mrb, "__oruby_trace_exc"), mrb_obj_value(exc));
}

/* eval's source, about to be compiled (go-mrb.c) */
void _orb_trace_script(mrb_state *mrb, mrb_value src) {
  orb_trace *t = trace_of(mrb, 0);
  if (!t || !(t->mask & EV_SCRIPT) || t->busy) return;
  mrb_callinfo *ci = mrb->c->ci;
  const mrb_irep *irep = NULL;
  uint32_t off = 0;
  if (ci > mrb->c->cibase) {
    const struct RProc *p = ci[-1].proc;
    if (p && !MRB_PROC_CFUNC_P(p) && ci[-1].pc) {
      irep = p->body.irep;
      off = (uint32_t)(ci[-1].pc - irep->iseq);
      if (off > 0) off--;
    }
  }
  fire(mrb, t, "script_compiled", mrb_nil_value(), 0, NULL, mrb_str_dup(mrb, src), irep, off, NULL);
}

static void orb_trace_hook(mrb_state *mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs);

int _orb_trace_hooked(mrb_state *mrb) { return mrb->code_fetch_hook == orb_trace_hook; }

/* TracePoint.__oruby_events = mask: the events enabled TracePoints have */
static mrb_value tp_events_set(mrb_state *mrb, mrb_value self) {
  mrb_int mask;
  mrb_get_args(mrb, "i", &mask);
  orb_trace *t = trace_of(mrb, mask != 0);
  if (!t) return mrb_fixnum_value(mask);
  t->mask = (uint32_t)mask;
  if (mask) {
    if (mrb->code_fetch_hook == NULL || mrb->code_fetch_hook != orb_trace_hook) {
      /* the one-shot request hook gives way: this one polls too */
      mrb->code_fetch_hook = orb_trace_hook;
    }
  }
  else {
    if (mrb->code_fetch_hook == orb_trace_hook) {
      mrb->code_fetch_hook = NULL;
      _orb_vm_arm(mrb);
    }
    t->exc = NULL;
    for (size_t i = 0; i < t->cap; i++) t->frames[i].cret = NULL;
  }
  return mrb_fixnum_value(mask);
}

/* TracePoint.__oruby_binding: a Binding of the frame of the event being
   handled */
static mrb_value tp_binding(mrb_state *mrb, mrb_value self) {
  orb_trace *t = trace_of(mrb, 0);
  if (!t || t->ev_c != mrb->c || t->ev_ci < 0) return mrb_nil_value();
  mrb_callinfo *ci = mrb->c->cibase + t->ev_ci;
  const struct RProc *proc = ci->proc;
  if (!proc || MRB_PROC_CFUNC_P(proc)) return mrb_nil_value();
  struct REnv *env = mrb_vm_ci_env_reify(mrb, mrb->c, ci);
  if (!env) return mrb_nil_value();
  mrb_value b = mrb_binding_new(mrb, proc, ci->stack[0], env);
  if (ci->pc && proc->body.irep) {
    mrb_iv_set(mrb, b, mrb_intern_lit(mrb, "pc"), mrb_fixnum_value(ci->pc - proc->body.irep->iseq));
  }
  return b;
}

static mrb_value reentry_body(mrb_state *mrb, void *ud) {
  return mrb_yield_argv(mrb, *(mrb_value *)ud, 0, NULL);
}

/* TracePoint.__oruby_reentry { }: tracing on in a handler */
static mrb_value tp_reentry(mrb_state *mrb, mrb_value self) {
  mrb_value blk;
  mrb_get_args(mrb, "&!", &blk);
  orb_trace *t = trace_of(mrb, 0);
  if (!t || !t->busy) mrb_raise(mrb, E_RUNTIME_ERROR, "No need to allow reentrance.");
  t->busy = 0;
  mrb_bool failed = FALSE;
  mrb_value r = mrb_protect_error(mrb, reentry_body, &blk, &failed);
  t->busy = 1;
  if (failed) mrb_exc_raise(mrb, r);
  return r;
}

/* TracePoint.__oruby_signal(event, self): an event the Ruby side has
   (:thread_begin, :thread_end) */
static mrb_value tp_signal(mrb_state *mrb, mrb_value self) {
  mrb_sym ev;
  mrb_value obj;
  mrb_get_args(mrb, "no", &ev, &obj);
  orb_trace *t = trace_of(mrb, 1);
  if (!t || t->busy) return mrb_nil_value();
  fire(mrb, t, mrb_sym_name(mrb, ev), obj, 0, NULL, mrb_nil_value(), NULL, 0, NULL);
  return mrb_nil_value();
}

void _orb_trace_init(mrb_state *mrb) {
  struct RClass *tp = mrb_define_class(mrb, "TracePoint", mrb->object_class);
  mrb_define_class_method(mrb, tp, "__oruby_events=", tp_events_set, MRB_ARGS_REQ(1));
  mrb_define_class_method(mrb, tp, "__oruby_binding", tp_binding, MRB_ARGS_NONE());
  mrb_define_class_method(mrb, tp, "__oruby_reentry", tp_reentry, MRB_ARGS_BLOCK());
  mrb_define_class_method(mrb, tp, "__oruby_signal", tp_signal, MRB_ARGS_REQ(2));
}

/* the state is closed */
void _orb_trace_close(mrb_state *mrb) {
  orb_trace *t = trace_of(mrb, 0);
  if (!t) return;
  free(t->frames);
  memset(t, 0, sizeof(*t));
}
