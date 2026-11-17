/*
** go-mrb.h -  helpers for Go binding
**
*/

#ifndef GOMRB_H
#define GOMRB_H

#if defined(__cplusplus)
extern "C" {
#endif

#include <limits.h>
#include <stdlib.h>
#include <math.h>
#include "mrbconf.h"
#include "mruby.h"
#include "mruby/proc.h"
#include "mruby/data.h"
#include "mruby/range.h"
#undef INCLUDE_ENCODING
#include "mruby/string.h"
#include "mruby/khash.h"
#include "mruby/hash.h"
#include "mruby/array.h"
#include "mruby/class.h"
#include "mruby/variable.h"
#include "mruby/numeric.h"
#include "mruby/string.h"
#include "mruby/compile.h"
#include "mruby/debug.h"
#include "mruby/gc.h"
#include "mruby/dump.h"
#include "mruby/error.h"
#include "mruby/throw.h"
#include "mruby/istruct.h"
#include "mruby/presym.h"
#include "mruby/internal.h"
#include "mruby/version.h"
#include "mruby/opcode.h"

static inline void _mrb_exc_set_marshaled_message(mrb_state *mrb, mrb_value exc, mrb_value message) {
  struct RException *e = mrb_exc_ptr(exc);
  e->mesg = mrb_nil_p(message) ? NULL : mrb_basic_ptr(message);
  if (!mrb_nil_p(message)) {
    mrb_field_write_barrier_value(mrb, (struct RBasic*)e, message);
  }
}

static inline void _mrb_exc_set_marshaled_backtrace(mrb_state *mrb, mrb_value exc, mrb_value backtrace) {
  struct RException *e = mrb_exc_ptr(exc);
  e->backtrace = mrb_nil_p(backtrace) ? NULL : mrb_basic_ptr(backtrace);
  if (!mrb_nil_p(backtrace)) {
    mrb_field_write_barrier_value(mrb, (struct RBasic*)e, backtrace);
  }
}

static char* _MRUBY_COPYRIGHT()  { return MRUBY_COPYRIGHT; }
static char* _MRUBY_DESCRIPTION()  { return MRUBY_DESCRIPTION; }
static mrb_int _MRUBY_RELEASE_MAJOR()  { return (mrb_int)MRUBY_RELEASE_MAJOR; }
static mrb_int _MRUBY_RELEASE_MINOR()  { return (mrb_int)MRUBY_RELEASE_MINOR; }
static mrb_int _MRUBY_RELEASE_TEENY()  { return (mrb_int)MRUBY_RELEASE_TEENY; }

// The MrbState index (linkrefs.go) is kept in mrb->ud itself, not in a
// heap object: callbacks that run while mrb_close frees the heap (Go-backed
// data's dfree) still read it
static void _mrb_set_idx(mrb_state *mrb, mrb_int idx) {
  mrb->ud = (void *)(intptr_t)idx;
}

static mrb_int _mrb_get_idx(mrb_state *mrb) {
  return (mrb_int)(intptr_t)mrb->ud;
}

static mrb_int _cmrb_get_idx(uintptr_t cmrb) {
  mrb_state *mrb = (mrb_state *)cmrb;
  return (mrb_int)(intptr_t)mrb->ud;
}

/* Inject and run code */

extern struct RProc* inject_run(mrb_int idx);
extern int go_has_line_debug(mrb_int idx);
extern void go_debug_line_hook(mrb_int idx, struct mrb_irep *irep, uint32_t pc_off, mrb_value *regs, int32_t line, char *file);

static void injector(struct mrb_state* mrb, const struct mrb_irep *irep, const mrb_code *pc, mrb_value *regs) {
  mrb_int idx = _mrb_get_idx(mrb);
  struct RProc *p = inject_run(idx);
  if (p) {
    mrb_funcall_with_block(mrb, mrb_obj_value(p), MRB_SYM(call), 0, NULL, mrb_nil_value());
  }
  if (irep && pc && regs && go_has_line_debug(idx)) {
    uint32_t pc_off = (pc >= irep->iseq) ? (uint32_t)(pc - irep->iseq) : 0;
    int32_t line = -1;
    const char *file = NULL;
    if (mrb_debug_get_position(mrb, irep, pc_off, &line, &file) && line > 0) {
      go_debug_line_hook(idx, (struct mrb_irep *)irep, pc_off, regs, line, (char *)file);
    }
  }
}

static void set_mrb_injector(mrb_state *mrb) {
	if (!mrb->code_fetch_hook) {
		mrb->code_fetch_hook = injector;
	}
}

typedef struct _mrb_caller_debug_info {
  const struct mrb_irep *irep;
  mrb_value *regs;
  int32_t line;
  const char *file;
} _mrb_caller_debug_info;

static inline _mrb_caller_debug_info _mrb_get_caller_debug_info(mrb_state *mrb) {
  _mrb_caller_debug_info info = { NULL, NULL, -1, NULL };
  if (!mrb || !mrb->c || !mrb->c->ci) return info;
  for (mrb_callinfo *ci = mrb->c->ci - 1; ci >= mrb->c->cibase; ci--) {
    if (ci->proc && !MRB_PROC_CFUNC_P(ci->proc) && ci->proc->body.irep) {
      const struct mrb_irep *irep = ci->proc->body.irep;
      info.irep = irep;
      info.regs = ci->stack;
      uint32_t pc_off = 0;
      if (ci->pc && ci->pc > irep->iseq) {
        pc_off = (uint32_t)(ci->pc - irep->iseq - 1);
      }
      mrb_debug_get_position(mrb, irep, pc_off, &info.line, &info.file);
      return info;
    }
  }
  return info;
}

// RBasic macro proxy calls
static void      _MRB_SET_FROZEN(struct RBasic *o, mrb_bool value) { o->frozen = value ? 1 : 0; }
static mrb_bool  _mrb_frozen_p(struct RBasic *o) { return mrb_frozen_p(o) != 0; }
static uint32_t  _mrb_basic_flags(struct RBasic *o)   { return o->flags; }
static enum mrb_vtype  _mrb_basic_type(struct RBasic *o)   { return o->tt; }
static void      _mrb_basic_set_color(struct RBasic *o, int c) { o->gc_color = c; }
static int       _mrb_basic_color(struct RBasic *o)  { return o->gc_color; }

// defines as functions making them visible on the Go side
static mrb_int _RARRAY_LEN(struct RArray *a)    { return ARY_LEN(a); }
static void*   _RARRAY_PTR(struct RArray *a)    { return ARY_PTR(a); }
static mrb_int _RARRAY_CAPA(struct RArray *a)   { return ARY_CAPA(a); }
static mrb_value _ARY_ITEM(struct RArray *a, mrb_int n) { return ARY_PTR(a)[n]; }
static void _RARRAY_SET_SHARED(struct RArray *a)   { ARY_SET_SHARED_FLAG(a); }
static void _RARRAY_UNSET_SHARED(struct RArray *a)   { ARY_UNSET_SHARED_FLAG(a); }

static char*   _RSTRING_PTR(mrb_value a)  { return RSTRING_PTR(a); }
static mrb_int _RSTRING_LEN(mrb_value a)  { return RSTRING_LEN(a); }
static mrb_int _RSTRING_CAPA(mrb_value a) { return RSTRING_CAPA(a); }
static char*   _RSTRING_END(mrb_value a)  { return RSTRING_END(a); }

static uint32_t _mrb_value_flags(mrb_value o) {
  return mrb_immediate_p(o) ? mrb_basic_ptr(o)->flags : 0; 
}

static void _mrb_value_set_flags(mrb_value o, uint32_t flags) {
   if (!mrb_immediate_p(o)) {
     return;
   }
   mrb_basic_ptr(o)->flags = flags; 
}

static mrb_bool _mrb_nil_p(mrb_value o)     { return mrb_nil_p(o); }
static mrb_int  _mrb_fixnum(mrb_value o)    { return mrb_fixnum(o); }
static double   _mrb_float(mrb_value o)     { return (double)mrb_float(o); }
static void*    _mrb_ptr(mrb_value o)       { return mrb_ptr(o); }
static void*    _mrb_cptr(mrb_value o)      { return mrb_cptr(o); }
static void*    _mrb_range_ptr(mrb_value o) { return mrb_ptr(o); }
static mrb_sym  _mrb_symbol(mrb_value o)    { return mrb_symbol(o); }
static enum mrb_vtype  _mrb_type(mrb_value o)    { return mrb_type(o); }
static struct RObject* _mrb_obj_ptr(mrb_value v) { return mrb_obj_ptr(v); }

static mrb_value _mrb_uintptr_value(mrb_state *mrb, uintptr_t p) { return mrb_cptr_value(mrb, (void *)p); }
static mrb_value _mrb_ptr_to_str(mrb_state *mrb, uintptr_t p) { return mrb_ptr_to_str(mrb, (void *)p); }

// _mrb_is_nil returns true if mrb_value is nil value
static mrb_bool _mrb_is_nil(mrb_value o)    {
	return mrb_nil_p(o);
}

// Hash
static int  _MRB_RHASH_PROCDEFAULT_P(mrb_value h) { return MRB_RHASH_PROCDEFAULT_P(h); }

// Env, Proc
static inline const struct RProc* _mrb_current_proc(mrb_state *mrb) {
  if (mrb && mrb->c && mrb->c->ci) {
    return mrb->c->ci->proc;
  }
  return NULL;
}
static void _set_last_stack_value(mrb_state *mrb, mrb_value v) { *(mrb->c->ci->stack + 1) = v; }
static void _MRB_ENV_SET_LEN(struct REnv *e, mrb_int len) { MRB_ENV_SET_LEN(e, len); }
static mrb_int _MRB_ENV_LEN(struct REnv *e) { return MRB_ENV_LEN(e); }
static uint32_t _mrb_rproc_flags(struct RProc *p) { return (uint32_t)p->flags; }
static void _mrb_rproc_set_flags(struct RProc *p, uint32_t flags) { p->flags = flags; }
static struct REnv* _MRB_PROC_ENV(struct RProc *p) { return MRB_PROC_ENV(p); }

static struct RClass * _MRB_PROC_TARGET_CLASS(struct RProc *p)  { return MRB_PROC_TARGET_CLASS(p); }
static void _MRB_PROC_SET_TARGET_CLASS(mrb_state *mrb, struct RProc *p, struct RClass *c)  {
   MRB_PROC_SET_TARGET_CLASS(p,c);
}
static int  _MRB_PROC_CFUNC_P(struct RProc *p)    { return (int)(MRB_PROC_CFUNC_P(p));  }
static mrb_func_t _MRB_PROC_CFUNC(struct RProc *p) { return MRB_PROC_CFUNC(p); }
static int  _MRB_PROC_STRICT_P(struct RProc *p)   { return (int)(MRB_PROC_STRICT_P(p)); }
static mrb_func_t _MRB_METHOD_FUNC(mrb_method_t m) { return MRB_METHOD_CFUNC(m); }
static struct RProc *_MRB_METHOD_PROC(mrb_method_t m) { return (struct RProc*)(MRB_METHOD_PROC(m)); }
static mrb_bool _MRB_METHOD_UNDEF_P(mrb_method_t m) { return MRB_METHOD_UNDEF_P(m); }

static uint16_t _mrb_rproc_nlocals(struct RProc *p)  { return p->body.irep->nlocals; }
static const mrb_irep *_rproc_body_irep(struct RProc *p)  { return p->body.irep; }

typedef struct _irep_dump {
  int result;
  uint8_t *bin;
  size_t bin_size;
} _irep_dump;

#define FLAG_BYTEORDER_NATIVE 2
#define FLAG_BYTEORDER_NONATIVE 0

static _irep_dump _dump_irep(mrb_state *mrb, mrb_irep *irep, uint8_t flags) {
  _irep_dump ret;
  ret.bin = NULL;
  ret.bin_size = 0;

  ret.result = mrb_dump_irep(mrb, irep, flags, &ret.bin, &ret.bin_size);
  return ret;
}

extern void p(char *c);

static int print(const char *fmt, ...) {
  char str[255];
  int ret;

  va_list myargs;
  va_start(myargs, fmt);
  ret = vsprintf(str, fmt, myargs);
  va_end(myargs);
  p(str);
  return ret;
}

static mrb_method_t _MRB_METHOD_FROM_PROC(struct RProc *p) {
  mrb_method_t m;
  MRB_METHOD_FROM_PROC(m, p);
  return m;
}

// Instance macro helpers
static void _MRB_SET_INSTANCE_TT(struct RClass *c, uint32_t tt) { MRB_SET_INSTANCE_TT(c, tt); }
static uint32_t _MRB_INSTANCE_TT(struct RClass *c) { return (uint32_t)MRB_INSTANCE_TT(c); }
static struct RClass* _mrb_class_origin(struct RClass *c) { MRB_CLASS_ORIGIN(c); return c; }

// RData value
static struct RData* _RDATA(mrb_value a)      { return RDATA(a);     };
static void* _DATA_PTR(mrb_value d)           { return DATA_PTR(d);  };
static mrb_data_type* _DATA_TYPE(mrb_value d) { return (mrb_data_type*)DATA_TYPE(d); };

// GoMrb RData type
extern void mrb_free_goref(mrb_state *mrb, void *p);
static struct mrb_data_type interface_data_type = {"GoMrb", mrb_free_goref };
static mrb_data_type* mrb_interface_data_type(void) { return &interface_data_type; };

// RRange macros
static mrb_value _RANGE_BEG(struct RRange *r) { return RANGE_BEG(r); }
static mrb_value _RANGE_END(struct RRange *r) { return RANGE_END(r); }
static mrb_bool _RANGE_EXCL(struct RRange *r) { return RANGE_EXCL(r); }

// vararg proxy calls, formated using Go fmt
static void _mrb_warn(mrb_state *mrb, const char *msg) { mrb_warn(mrb, msg); }
static void _mrb_bug(mrb_state *mrb, const char *msg)  { mrb_bug(mrb, msg);  }

// Argument helpers
static mrb_value
_mrb_get_args_first(mrb_state *mrb) {
  if (mrb_get_argc(mrb) > 0) {
    return *mrb_get_argv(mrb);
  } else {
    return mrb_nil_value();
  }
}

static mrb_value
_mrb_get_arg(mrb_value *args, int index) {
	return args[index];
}

// Return block argument, which is last argument
static mrb_value
_mrb_get_args_block(mrb_state *mrb) {
  mrb_callinfo *ci = mrb->c->ci;
  return ci->stack[mrb_ci_bidx(ci)];
}

// Return kwargs hash table
static mrb_value
_mrb_get_args_kw(mrb_state *mrb) {
  mrb_callinfo *ci = mrb->c->ci;
  if (ci->kw) {
    return ci->stack[mrb_ci_bidx(ci)-1];
  }
  return mrb_nil_value();
}

// Bit packed options from struct
static int  _mrbc_capture_errors(mrbc_context *c )                { return c->capture_errors; }
static void _mrbc_set_capture_errors(mrbc_context *c, mrb_bool v) { c->capture_errors = v; }
static int  _mrbc_dump_result(mrbc_context *c )                   { return c->dump_result; }
static void _mrbc_set_dump_result(mrbc_context *c, mrb_bool v)    { c->dump_result = v; }
static int  _mrbc_no_exec(mrbc_context *c )                       { return c->no_exec;  }
static void _mrbc_set_no_exec(mrbc_context *c, mrb_bool v)        { c->no_exec = v; }
static int  _mrbc_keep_lv(mrbc_context *c )                       { return c->keep_lv;  }
static void _mrbc_set_keep_lv(mrbc_context *c, mrb_bool v)        { c->keep_lv = v; }
static int  _mrbc_no_optimize(mrbc_context *c )                   { return c->no_optimize;  }
static void _mrbc_set_no_optimize(mrbc_context *c, mrb_bool v)    { c->no_optimize = v; }
static int  _mrbc_no_ext_ops(mrbc_context *c)                     { return c->no_ext_ops; }
static void _mrbc_set_no_ext_ops(mrbc_context *c, mrb_bool v)     { c->no_ext_ops = v;  }

// GC
static mrb_bool _gc_iterating(mrb_state *mrb) { return mrb->gc.iterating; }
static mrb_bool _gc_full(mrb_state *mrb) { return mrb->gc.full; }
static mrb_bool _gc_generational(mrb_state *mrb) { return mrb->gc.generational; }
static mrb_bool _gc_disabled(mrb_state *mrb) { return mrb->gc.disabled; }
static void _gc_set_disabled(mrb_state *mrb, mrb_bool v) { mrb->gc.disabled = v; }
static struct RBasic *_gc_arena_peek(mrb_state *mrb, mrb_int i) {
	int max = mrb->gc.arena_idx;
	if (i < 0) { i = max + i; };
	if ((i < 0) || (i >= max)) { return NULL; };

	return mrb->gc.arena[i];
}

static int _MRB_FUNCALL_ARGC_MAX() {
#ifdef MRB_FUNCALL_ARGC_MAX
    return MRB_FUNCALL_ARGC_MAX;
#else
    return 16;
#endif
}

// Error formatting using go fmt
static void _mrb_name_error(mrb_state *mrb, mrb_sym id, const char *msg) { mrb_name_error(mrb, id, msg); }

enum go_mrb_outcome {
  GO_MRB_OUTCOME_SUCCESS = 0,
  GO_MRB_OUTCOME_EXCEPTION = 1,
  GO_MRB_OUTCOME_CONTROL = 2
};

static int
_go_mrb_protect(mrb_state *mrb, mrb_protect_error_func *body, void *data, mrb_value *result) {
  struct RObject *previous_exc = mrb->exc;
  if (previous_exc) {
    mrb_gc_protect(mrb, mrb_obj_value(previous_exc));
  }
  mrb->exc = NULL;

  mrb_bool raised = FALSE;
  *result = mrb_protect_error(mrb, body, data, &raised);
  if (!raised && mrb->exc) {
    *result = mrb_obj_value(mrb->exc);
    raised = TRUE;
  }
  mrb_gc_protect(mrb, *result);
  mrb->exc = previous_exc;

  if (raised) {
    return mrb_type(*result) == MRB_TT_BREAK ? GO_MRB_OUTCOME_CONTROL : GO_MRB_OUTCOME_EXCEPTION;
  }
  if (mrb_type(*result) == MRB_TT_BREAK) {
    return GO_MRB_OUTCOME_CONTROL;
  }
  return GO_MRB_OUTCOME_SUCCESS;
}

typedef struct {
  mrb_value receiver;
  mrb_sym mid;
  mrb_int argc;
  const mrb_value *argv;
  mrb_value block;
} _go_mrb_call_data;

mrb_value set_gofunc_callback(mrb_state *mrb, mrb_value self);

static mrb_value
_go_mrb_funcall_body(mrb_state *mrb, void *data) {
  _go_mrb_call_data *call = (_go_mrb_call_data *)data;
  if (mrb_proc_p(call->receiver) && call->mid == MRB_SYM(call) && mrb_nil_p(call->block)) {
    struct RProc *proc = mrb_proc_ptr(call->receiver);
    if (MRB_PROC_CFUNC_P(proc) && MRB_PROC_CFUNC(proc) == set_gofunc_callback) {
      return mrb_yield_argv(mrb, call->receiver, call->argc, call->argv);
    }
  }
  return mrb_funcall_with_block(mrb, call->receiver, call->mid, call->argc, call->argv, call->block);
}

static int
_mrb_funcall_with_block(mrb_state *mrb, mrb_value receiver, mrb_sym mid, mrb_int argc, const mrb_value *argv, mrb_value block, mrb_value *result) {
  _go_mrb_call_data call = { receiver, mid, argc, argv, block };
  return _go_mrb_protect(mrb, _go_mrb_funcall_body, &call, result);
}

typedef struct { mrb_value block; mrb_value arg; } _go_mrb_yield_data;
typedef struct { mrb_value block; mrb_int argc; const mrb_value *argv; } _go_mrb_yield_argv_data;
typedef struct { mrb_value block; mrb_int argc; const mrb_value *argv; mrb_value self; struct RClass *klass; } _go_mrb_yield_class_data;
typedef struct { mrb_value block; mrb_value self; mrb_int argc; const mrb_value *argv; } _go_mrb_yield_cont_data;

static mrb_value
_go_mrb_yield_body(mrb_state *mrb, void *data) {
  _go_mrb_yield_data *yield = (_go_mrb_yield_data *)data;
  return mrb_yield(mrb, yield->block, yield->arg);
}

static int
_mrb_yield(mrb_state *mrb, mrb_value block, mrb_value arg, mrb_value *result) {
  _go_mrb_yield_data yield = { block, arg };
  return _go_mrb_protect(mrb, _go_mrb_yield_body, &yield, result);
}

static mrb_value
_go_mrb_yield_argv_body(mrb_state *mrb, void *data) {
  _go_mrb_yield_argv_data *yield = (_go_mrb_yield_argv_data *)data;
  return mrb_yield_argv(mrb, yield->block, yield->argc, yield->argv);
}

static int
_mrb_yield_argv(mrb_state *mrb, mrb_value block, mrb_int argc, const mrb_value *argv, mrb_value *result) {
  _go_mrb_yield_argv_data yield = { block, argc, argv };
  return _go_mrb_protect(mrb, _go_mrb_yield_argv_body, &yield, result);
}

static mrb_value
_go_mrb_yield_with_class_body(mrb_state *mrb, void *data) {
  _go_mrb_yield_class_data *yield = (_go_mrb_yield_class_data *)data;
  return mrb_yield_with_class(mrb, yield->block, yield->argc, yield->argv, yield->self, yield->klass);
}

static int
_mrb_yield_with_class(mrb_state *mrb, mrb_value block, mrb_int argc, const mrb_value *argv, mrb_value self, struct RClass *klass, mrb_value *result) {
  _go_mrb_yield_class_data yield = { block, argc, argv, self, klass };
  return _go_mrb_protect(mrb, _go_mrb_yield_with_class_body, &yield, result);
}

static mrb_value
_go_mrb_yield_cont_body(mrb_state *mrb, void *data) {
  _go_mrb_yield_cont_data *yield = (_go_mrb_yield_cont_data *)data;
  return mrb_yield_cont(mrb, yield->block, yield->self, yield->argc, yield->argv);
}

static int
_mrb_yield_cont(mrb_state *mrb, mrb_value block, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value *result) {
  _go_mrb_yield_cont_data yield = { block, self, argc, argv };
  return _go_mrb_protect(mrb, _go_mrb_yield_cont_body, &yield, result);
}

typedef struct {
  struct RClass *c;
  mrb_int argc;
  const mrb_value *argv;
} _go_mrb_obj_new_data;

static mrb_value
_go_mrb_obj_new_body(mrb_state *mrb, void *data) {
  _go_mrb_obj_new_data *d = (_go_mrb_obj_new_data *)data;
  return mrb_obj_new(mrb, d->c, d->argc, d->argv);
}

static int
_mrb_obj_new(mrb_state *mrb, struct RClass *c, mrb_int argc, const mrb_value *argv, mrb_value *result) {
  _go_mrb_obj_new_data d = { c, argc, argv };
  return _go_mrb_protect(mrb, _go_mrb_obj_new_body, &d, result);
}

/* Raising mruby API calls, run under mrb_protect_error so a raise comes back
   to Go as an outcome instead of a longjmp across Go frames. */
enum {
  GO_MRB_OP_CHECK_FROZEN = 1,
  GO_MRB_OP_ENSURE_STRING,
  GO_MRB_OP_ENSURE_ARRAY,
  GO_MRB_OP_ENSURE_HASH,
  GO_MRB_OP_ENSURE_INTEGER,
  GO_MRB_OP_ENSURE_INT,
  GO_MRB_OP_ENSURE_FLOAT,
  GO_MRB_OP_AS_INT64,
  GO_MRB_OP_AS_UINT64,
  GO_MRB_OP_ARY_DUP,
  GO_MRB_OP_STR_DUP_FROZEN,
};

typedef struct { int op; mrb_value v; int64_t i64; uint64_t u64; } _go_mrb_op_data;

static mrb_value
_go_mrb_op_body(mrb_state *mrb, void *data) {
  _go_mrb_op_data *d = (_go_mrb_op_data *)data;
  switch (d->op) {
  case GO_MRB_OP_CHECK_FROZEN: mrb_check_frozen_value(mrb, d->v); return d->v;
  case GO_MRB_OP_ENSURE_STRING: return mrb_ensure_string_type(mrb, d->v);
  case GO_MRB_OP_ENSURE_ARRAY: return mrb_ensure_array_type(mrb, d->v);
  case GO_MRB_OP_ENSURE_HASH: return mrb_ensure_hash_type(mrb, d->v);
  case GO_MRB_OP_ENSURE_INTEGER: return mrb_ensure_integer_type(mrb, d->v);
  case GO_MRB_OP_ENSURE_INT: return mrb_ensure_int_type(mrb, d->v);
  case GO_MRB_OP_ENSURE_FLOAT: return mrb_ensure_float_type(mrb, d->v);
  case GO_MRB_OP_AS_INT64: d->i64 = mrb_as_int64(mrb, d->v); return d->v;
  case GO_MRB_OP_AS_UINT64: d->u64 = mrb_as_uint64(mrb, d->v); return d->v;
  case GO_MRB_OP_ARY_DUP: return mrb_ary_dup(mrb, d->v);
  case GO_MRB_OP_STR_DUP_FROZEN: return mrb_str_dup_frozen(mrb, d->v);
  }
  return mrb_nil_value();
}

static int
_mrb_protected_op(mrb_state *mrb, int op, mrb_value v, mrb_value *result, int64_t *i64, uint64_t *u64) {
  _go_mrb_op_data d = { op, v, 0, 0 };
  int status = _go_mrb_protect(mrb, _go_mrb_op_body, &d, result);
  if (i64) *i64 = d.i64;
  if (u64) *u64 = d.u64;
  return status;
}

typedef struct { mrb_value ary; mrb_int len; const mrb_value *vals; } _go_mrb_unshift_data;

static mrb_value
_go_mrb_ary_unshift_values_body(mrb_state *mrb, void *data) {
  _go_mrb_unshift_data *d = (_go_mrb_unshift_data *)data;
  return mrb_ary_unshift_values(mrb, d->ary, d->len, d->vals);
}

static int
_mrb_ary_unshift_values(mrb_state *mrb, mrb_value ary, mrb_int len, const mrb_value *vals, mrb_value *result) {
  _go_mrb_unshift_data d = { ary, len, vals };
  return _go_mrb_protect(mrb, _go_mrb_ary_unshift_values_body, &d, result);
}

typedef struct { mrb_value str; mrb_int beg; mrb_int len; } _go_mrb_subseq_data;

static mrb_value
_go_mrb_str_byte_subseq_body(mrb_state *mrb, void *data) {
  _go_mrb_subseq_data *d = (_go_mrb_subseq_data *)data;
  return mrb_str_byte_subseq(mrb, d->str, d->beg, d->len);
}

static int
_mrb_str_byte_subseq(mrb_state *mrb, mrb_value str, mrb_int beg, mrb_int len, mrb_value *result) {
  _go_mrb_subseq_data d = { str, beg, len };
  return _go_mrb_protect(mrb, _go_mrb_str_byte_subseq_body, &d, result);
}

static mrb_value
_go_mrb_gc_driven_body(mrb_state *mrb, void *data) {
  mrb_gc_scheduler_driven(mrb, *(mrb_bool *)data);
  return mrb_nil_value();
}

static int
_mrb_gc_scheduler_driven(mrb_state *mrb, mrb_bool enable, mrb_value *result) {
  return _go_mrb_protect(mrb, _go_mrb_gc_driven_body, &enable, result);
}

/* mrb_iv_foreach through Go: the callback gets the Go hook key */
extern int go_iv_foreach_callback(mrb_state *mrb, mrb_sym sym, mrb_value val, void *data);

static int
_go_iv_foreach_trampoline(mrb_state *mrb, mrb_sym sym, mrb_value val, void *data) {
  return go_iv_foreach_callback(mrb, sym, val, data);
}

static void
_mrb_iv_foreach(mrb_state *mrb, mrb_value obj, void *data) {
  mrb_iv_foreach(mrb, obj, _go_iv_foreach_trampoline, data);
}

/* clear a pending mrb_vm_interrupt that no VM consumed */
static void
_mrb_vm_interrupt_clear(mrb_state *mrb) {
  mrb->vm_interrupt = FALSE;
}

static mrb_value
_mrb_caller_receiver(mrb_state *mrb) {
  if (mrb->c->ci > mrb->c->cibase + 1) {
    mrb_callinfo *prev = mrb->c->ci - 2;
    if (prev->stack) {
      return prev->stack[0];
    }
  }
  return mrb_nil_value();
}


typedef struct { const char *source; size_t length; mrb_ccontext *context; } _go_mrb_load_string_data;
typedef struct { struct mrb_parser_state *parser; mrb_ccontext *context; } _go_mrb_load_exec_data;
typedef struct { const void *buffer; size_t length; mrb_ccontext *context; } _go_mrb_load_irep_data;
typedef struct { struct RProc *proc; } _go_mrb_load_proc_data;
typedef struct { mrb_value exception; } _go_mrb_exception_data;
typedef struct { mrb_value argument; } _go_mrb_raise_data;

static mrb_value
_go_mrb_load_nstring_body(mrb_state *mrb, void *data) {
  _go_mrb_load_string_data *load = (_go_mrb_load_string_data *)data;
  return mrb_load_nstring_cxt(mrb, load->source, load->length, load->context);
}

static int
_mrb_load_nstring_cxt(mrb_state *mrb, const char *source, size_t length, mrb_ccontext *context, mrb_value *result) {
  _go_mrb_load_string_data load = { source, length, context };
  return _go_mrb_protect(mrb, _go_mrb_load_nstring_body, &load, result);
}

static mrb_value
_go_mrb_load_string_body(mrb_state *mrb, void *data) {
  _go_mrb_load_string_data *load = (_go_mrb_load_string_data *)data;
  return mrb_load_string_cxt(mrb, load->source, load->context);
}

static int
_mrb_load_string_cxt(mrb_state *mrb, const char *source, mrb_ccontext *context, mrb_value *result) {
  _go_mrb_load_string_data load = { source, 0, context };
  return _go_mrb_protect(mrb, _go_mrb_load_string_body, &load, result);
}

static mrb_value
_go_mrb_load_exec_body(mrb_state *mrb, void *data) {
  _go_mrb_load_exec_data *load = (_go_mrb_load_exec_data *)data;
  return mrb_load_exec(mrb, load->parser, load->context);
}

static int
_mrb_load_exec(mrb_state *mrb, struct mrb_parser_state *parser, mrb_ccontext *context, mrb_value *result) {
  _go_mrb_load_exec_data load = { parser, context };
  return _go_mrb_protect(mrb, _go_mrb_load_exec_body, &load, result);
}

static mrb_value
_go_mrb_load_irep_body(mrb_state *mrb, void *data) {
  _go_mrb_load_irep_data *load = (_go_mrb_load_irep_data *)data;
  return mrb_load_irep_buf_cxt(mrb, load->buffer, load->length, load->context);
}

static int
_mrb_load_irep_buf_cxt(mrb_state *mrb, const void *buffer, size_t length, mrb_ccontext *context, mrb_value *result) {
  _go_mrb_load_irep_data load = { buffer, length, context };
  return _go_mrb_protect(mrb, _go_mrb_load_irep_body, &load, result);
}

static mrb_value
_go_mrb_load_irep_body_no_context(mrb_state *mrb, void *data) {
  _go_mrb_load_irep_data *load = (_go_mrb_load_irep_data *)data;
  return mrb_load_irep_buf(mrb, load->buffer, load->length);
}

static int
_mrb_load_irep_buf(mrb_state *mrb, const void *buffer, size_t length, mrb_value *result) {
  _go_mrb_load_irep_data load = { buffer, length, NULL };
  return _go_mrb_protect(mrb, _go_mrb_load_irep_body_no_context, &load, result);
}

static mrb_value
_go_mrb_load_irep_body_empty(mrb_state *mrb, void *data) {
  return mrb_load_irep(mrb, NULL);
}

static int
_mrb_load_irep_empty(mrb_state *mrb, mrb_value *result) {
  return _go_mrb_protect(mrb, _go_mrb_load_irep_body_empty, NULL, result);
}

static mrb_value
_go_mrb_load_irep_body_empty_cxt(mrb_state *mrb, void *data) {
  mrb_ccontext *context = (mrb_ccontext *)data;
  return mrb_load_irep_cxt(mrb, NULL, context);
}

static int
_mrb_load_irep_empty_cxt(mrb_state *mrb, mrb_ccontext *context, mrb_value *result) {
  return _go_mrb_protect(mrb, _go_mrb_load_irep_body_empty_cxt, context, result);
}

static mrb_value
_go_mrb_load_proc_body(mrb_state *mrb, void *data) {
  _go_mrb_load_proc_data *load = (_go_mrb_load_proc_data *)data;
  return mrb_load_proc(mrb, load->proc);
}

static int
_mrb_load_proc(mrb_state *mrb, struct RProc *proc, mrb_value *result) {
  _go_mrb_load_proc_data load = { proc };
  return _go_mrb_protect(mrb, _go_mrb_load_proc_body, &load, result);
}

static mrb_value
_go_mrb_exception_output_body(mrb_state *mrb, void *data) {
  _go_mrb_exception_data *exception = (_go_mrb_exception_data *)data;
  return mrb_exc_get_output(mrb, mrb_obj_ptr(exception->exception));
}

static int
_mrb_exception_output(mrb_state *mrb, mrb_value exception, mrb_value *result) {
  _go_mrb_exception_data data = { exception };
  return _go_mrb_protect(mrb, _go_mrb_exception_output_body, &data, result);
}

static mrb_value
_go_mrb_register_exception_body(mrb_state *mrb, void *data) {
  _go_mrb_exception_data *exception = (_go_mrb_exception_data *)data;
  mrb_gc_register(mrb, exception->exception);
  return mrb_nil_value();
}

static int
_mrb_register_exception(mrb_state *mrb, mrb_value exception) {
  _go_mrb_exception_data data = { exception };
  mrb_value result;
  return _go_mrb_protect(mrb, _go_mrb_register_exception_body, &data, &result);
}

static mrb_value
_go_mrb_exception_class_body(mrb_state *mrb, void *data) {
  _go_mrb_exception_data *exception = (_go_mrb_exception_data *)data;
  return mrb_str_new_cstr(mrb, mrb_obj_classname(mrb, exception->exception));
}

static int
_mrb_exception_class(mrb_state *mrb, mrb_value exception, mrb_value *result) {
  _go_mrb_exception_data data = { exception };
  return _go_mrb_protect(mrb, _go_mrb_exception_class_body, &data, result);
}

typedef struct { mrb_value exception; int backtrace; } _go_mrb_exception_field_data;

static mrb_value
_go_mrb_exception_field_body(mrb_state *mrb, void *data) {
  _go_mrb_exception_field_data *field = (_go_mrb_exception_field_data *)data;
  mrb_sym method = field->backtrace ? mrb_intern_lit(mrb, "backtrace") : mrb_intern_lit(mrb, "message");
  return mrb_funcall_with_block(mrb, field->exception, method, 0, NULL, mrb_nil_value());
}

static int
_mrb_exception_field(mrb_state *mrb, mrb_value exception, int backtrace, mrb_value *result) {
  _go_mrb_exception_field_data data = { exception, backtrace };
  return _go_mrb_protect(mrb, _go_mrb_exception_field_body, &data, result);
}

static mrb_value
_go_mrb_f_raise_body(mrb_state *mrb, void *data) {
  _go_mrb_raise_data *raise = (_go_mrb_raise_data *)data;
  return mrb_f_raise(mrb, raise->argument);
}

static int
_mrb_f_raise_protected(mrb_state *mrb, mrb_value argument, mrb_value *result) {
  _go_mrb_raise_data raise = { argument };
  return _go_mrb_protect(mrb, _go_mrb_f_raise_body, &raise, result);
}


static struct REnv *
_mrb_create_env(mrb_state *mrb, struct RProc *p, mrb_int argc, mrb_value *argv)
{
  struct REnv *e;
  int i;

  e = (struct REnv*)mrb_obj_alloc(mrb, MRB_TT_ENV, mrb->object_class);
  e->stack = NULL;
  MRB_ENV_CLOSE(e);
  if (argc > 0) {
    e->stack = (mrb_value*)mrb_malloc(mrb, sizeof(mrb_value) * argc);
    for (i = 0; i < argc; i++) {
      e->stack[i] = argv[i];
    }
  }
  MRB_ENV_SET_LEN(e, argc);

  p->e.env = e;
  p->flags |= MRB_PROC_ENVSET;
  mrb_field_write_barrier(mrb, (struct RBasic*)p, (struct RBasic*)e);

  return e;
}

static void
_mrb_proc_set_env(mrb_state *mrb, struct RProc *p, mrb_value v)
{
  if (p == NULL) {
    mrb_raise(mrb, E_TYPE_ERROR, "RProc is empty.");
    return;
  }

  if (MRB_PROC_ENV_P(p)) {
    mrb_raise(mrb, E_TYPE_ERROR, "Expected empty RProc environment.");
    return;
  }

  _mrb_create_env(mrb, p, 1, &v);
}

static mrb_bool
_mrb_proc_has_env(mrb_state *mrb, struct RProc *p)
{
  struct REnv *e = MRB_PROC_ENV(p);

  if (!MRB_PROC_CFUNC_P(p)) {
    return 0; // Can't get cfunc env from non-cfunc proc.
  }
  if (!e) {
    return 0; // Can't get cfunc env from cfunc Proc without REnv.
  }
  if (MRB_ENV_LEN(e) < 1) {
    return 0; // Empty env
  }

  return 1;
}

static mrb_value
_mrb_proc_env_get(mrb_state *mrb, struct RProc *p, mrb_int idx)
{
  struct REnv *e = MRB_PROC_ENV(p);

  if (!MRB_PROC_CFUNC_P(p)) {
    mrb_raise(mrb, E_TYPE_ERROR, "Can't get cfunc env from non-cfunc proc.");
  }
  if (!e) {
    mrb_raise(mrb, E_TYPE_ERROR, "Can't get cfunc env from cfunc Proc without REnv.");
  }
  if (idx < 0 || MRB_ENV_LEN(e) <= idx) {
    mrb_raisef(mrb, E_INDEX_ERROR, "Env index out of range: %S (expected: 0 <= index < %S)",
               mrb_fixnum_value(idx), mrb_fixnum_value(MRB_ENV_LEN(e)));
  }

  return e->stack[idx];
}

static void _mrb_pool_value_migrate(mrb_irep_pool *v) {
  if ((v->tt & 3) == IREP_TT_STR) {
    uint32_t len = v->tt >> 2;
    char *p = (char*)malloc(len+1);
    memcpy(p, v->u.str, len+1);
    v->u.str = (const char*)p;
    return;
  }

  if (v->tt == IREP_TT_BIGINT) {
    uint32_t len = strlen(v->u.str);
    char *p = (char*)malloc(len+1);
    memcpy(p, v->u.str, len);
    v->u.str = (const char*)p;
  }
}

static const char* _mrb_pool_str(const mrb_irep_pool *v) { return v->u.str; }
static uint32_t _mrb_pool_len(const mrb_irep_pool *v) { return v->tt >> 2; }
static int32_t _mrb_pool_i32(const mrb_irep_pool *v) { return v->u.i32; }
static int64_t _mrb_pool_i64(const mrb_irep_pool *v) { return v->u.i64; }
static double _mrb_pool_float(const mrb_irep_pool *v) { return (double)v->u.f; }

// Callbacks
extern int  go_partial_hook_callback(struct mrb_parser_state *p);
extern int  go_hash_callback(mrb_state *mrb, mrb_value key, mrb_value val, void *data);
extern int  go_each_object_callback(mrb_state *mrb, struct RBasic *obj, void *data);
extern mrb_value go_gofunc_callback(mrb_int mrbidx, mrb_value self, int idx);
extern mrb_value go_mrb_proc_callback(mrb_int mrbidx, mrb_value self, void *proc_ptr);
extern mrb_value go_mrb_func_env_callback(mrb_int mrbidx, mrb_value self, int idx);

mrb_value set_mrb_proc_callback(mrb_state *mrb, mrb_value self);
mrb_value set_gofunc_callback(mrb_state *mrb, mrb_value self);
mrb_value set_mrb_env_callback(mrb_state *mrb, mrb_value self);
int set_hash_callback(mrb_state *mrb, mrb_value key, mrb_value val, void *data);
int set_each_object_callback(struct mrb_state *mrb, struct RBasic *obj, void *data);

// static void _mrb_copy_value(mrb_value *v1, mrb_value *v2) { *v1=*v2; }
void _mrb_proc_new_cfunc(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec);
void _mrb_proc_new_module_function(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec);
void _mrb_proc_new_class_method(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec);
void _mrb_proc_new_singleton_method(mrb_state *mrb, mrb_value obj, mrb_sym id, int idx, mrb_aspec aspec);
void _mrb_method_new_cfunc(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec);
void _define_class_method(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec);

// Module#private/public/protected/module_function accepting String names
// defined?(X) / defined?(A::B) report pending autoloads (gem/load) as constants

// cmd helpers
extern void mrb_codedump_all(mrb_state*, struct RProc*);
static void _set_parser_s(struct mrb_parser_state *parser, char *str) {
    parser->s = str;
    parser->send = str + strlen(str);
};

typedef struct mrb_batch_op {
  uint32_t op;
  uint32_t off;
  int64_t  i;
  double   f;
} mrb_batch_op;

static inline mrb_value _mrb_exec_batch_ops(mrb_state *mrb, const mrb_batch_op *ops, mrb_int nops, const char *str_buf) {
  int ai = mrb_gc_arena_save(mrb);
  mrb_value stack_buf[256];
  mrb_value *stack = stack_buf;
  mrb_int stack_cap = 256;
  if (nops > stack_cap) {
    stack_cap = nops;
    stack = (mrb_value *)mrb_malloc(mrb, sizeof(mrb_value) * (size_t)stack_cap);
  }
  mrb_int sp = 0;
  for (mrb_int k = 0; k < nops; k++) {
    const mrb_batch_op *op = &ops[k];
    switch (op->op) {
    case 0:
      stack[sp++] = mrb_nil_value();
      break;
    case 1:
      stack[sp++] = mrb_false_value();
      break;
    case 2:
      stack[sp++] = mrb_true_value();
      break;
    case 3:
      stack[sp++] = mrb_fixnum_value((mrb_int)op->i);
      break;
    case 4:
      stack[sp++] = mrb_float_value(mrb, (mrb_float)op->f);
      break;
    case 5:
      stack[sp++] = mrb_str_new(mrb, str_buf + op->off, (mrb_int)op->i);
      break;
    case 6: {
      mrb_int cnt = (mrb_int)op->i;
      sp -= cnt;
      stack[sp] = mrb_ary_new_from_values(mrb, cnt, &stack[sp]);
      sp++;
      mrb_gc_arena_restore(mrb, ai);
      for (mrb_int s = 0; s < sp; s++) {
        if (!mrb_immediate_p(stack[s])) mrb_gc_protect(mrb, stack[s]);
      }
      break;
    }
    case 7: {
      mrb_int pairs = (mrb_int)op->i;
      sp -= pairs * 2;
      mrb_value h = mrb_hash_new_capa(mrb, pairs);
      for (mrb_int j = 0; j < pairs; j++) {
        mrb_hash_set(mrb, h, stack[sp + j*2], stack[sp + j*2 + 1]);
      }
      stack[sp++] = h;
      mrb_gc_arena_restore(mrb, ai);
      for (mrb_int s = 0; s < sp; s++) {
        if (!mrb_immediate_p(stack[s])) mrb_gc_protect(mrb, stack[s]);
      }
      break;
    }
    }
  }
  mrb_value res = sp > 0 ? stack[0] : mrb_nil_value();
  if (stack != stack_buf) {
    mrb_free(mrb, stack);
  }
  mrb_gc_arena_restore(mrb, ai);
  if (!mrb_immediate_p(res)) {
    mrb_gc_protect(mrb, res);
  }
  return res;
}

static inline void _mrb_intern_batch(mrb_state *mrb, const char *buf, const uint32_t *offs, const uint32_t *lens, mrb_sym *out, mrb_int count) {
  for (mrb_int i = 0; i < count; i++) {
    out[i] = mrb_intern(mrb, buf + offs[i], (size_t)lens[i]);
  }
}

static inline void _mrb_irep_catch_handler(const mrb_irep *irep, int i, int *type, uint32_t *begin, uint32_t *end, uint32_t *target) {
  const struct mrb_irep_catch_handler *e = mrb_irep_catch_handler_table(irep) + i;
  *type = e->type;
  *begin = mrb_irep_catch_handler_unpack(e->begin);
  *end = mrb_irep_catch_handler_unpack(e->end);
  *target = mrb_irep_catch_handler_unpack(e->target);
}

void _mrb_init_oruby_core(mrb_state *mrb);
/* go-signal.c */
int _orb_sig_take(void);
void _orb_sig_post(mrb_state *mrb, int signo);
int _orb_sig_pending_p(int signo);
void _orb_sig_clear(mrb_state *mrb);
void _orb_sig_dfl(int signo);
void _orb_sig_kill_self(int signo);
void _mrb_make_method_private(mrb_state *mrb, struct RClass *c, mrb_sym mid);

// Numeric operators the AOT compiler evaluates inline for Integer and Float
// operands (see AotIntOpsRedefined); a set bit means the program redefined one.
#define _AOT_BOPS(f) (f(MRB_BOP_ADD) | f(MRB_BOP_SUB) | f(MRB_BOP_MUL) | f(MRB_BOP_DIV) | \
                      f(MRB_BOP_EQ) | f(MRB_BOP_LT) | f(MRB_BOP_LE) | f(MRB_BOP_GT) | f(MRB_BOP_GE))
enum {
  _AOT_INT_BOP_MASK = _AOT_BOPS(MRB_BOP_INTEGER),
  _AOT_FLOAT_BOP_MASK = _AOT_BOPS(MRB_BOP_FLOAT)
};

#if defined(__cplusplus)
}  /* extern "C" { */
#endif

#endif  /* GOMRB_H */

void _mrb_wrap_string_encoding(mrb_state *mrb);
void _mrb_wrap_string_concat(mrb_state *mrb);
mrb_bool _mrb_gv_hook(mrb_state *mrb, const char *name, const char *canon, int kind);
int _mrb_gv_slot(const char *name, const char *canon, int kind);
void _mrb_gv_hook_slot(mrb_state *mrb, int i);
