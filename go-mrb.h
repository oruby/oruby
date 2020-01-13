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

static void _mrb_set_idx(mrb_state *mrb, mrb_int idx) {
  struct RBasic *s = mrb_obj_alloc(mrb, MRB_TT_ISTRUCT, mrb->object_class);
  struct RIStruct *is = (struct RIStruct *)s;
  mrb_int *i = (mrb_int *)is->inline_data;

  *i = idx;

  MRB_SET_FROZEN_FLAG(s);
  mrb_sym sym = mrb_intern_lit(mrb, "$MRB");
  mrb_gv_set(mrb, sym, mrb_obj_value(s));

  // Set pointer to MrbState index
  if (idx != 0) {
    mrb->ud = i;
  }
}

static mrb_int _mrb_get_idx(mrb_state *mrb) {
  return mrb->ud ? *(mrb_int*)mrb->ud : 0;
}

static mrb_int _cmrb_get_idx(uintptr_t cmrb) {
	mrb_state *mrb = (mrb_state *)cmrb;
  return mrb->ud ? *(mrb_int*)mrb->ud : 0;
}

// defines as functions making them visible on the Go side
static mrb_int _RARRAY_LEN(struct RArray *a)    { return ARY_LEN(a); }
static void*   _RARRAY_PTR(struct RArray *a)    { return ARY_PTR(a); }
static mrb_int _RARRAY_CAPA(struct RArray *a)   { return ARY_CAPA(a); }
static mrb_value _ARY_ITEM(struct RArray *a, mrb_int n) { return ARY_PTR(a)[n]; }

static char*   _RSTRING_PTR(mrb_value a)  { return RSTRING_PTR(a); }
static mrb_int _RSTRING_LEN(mrb_value a)  { return RSTRING_LEN(a); }
static mrb_int _RSTRING_CAPA(mrb_value a) { return RSTRING_CAPA(a); }
static char*   _RSTRING_END(mrb_value a)  { return RSTRING_END(a); }

static uint32_t  _mrb_basic_flags(struct RBasic *o)   { return o->flags; }
static mrb_bool  _mrb_basic_frozen(struct RBasic *o) { return MRB_FROZEN_P(o) != 0; }
static void      _mrb_basic_set_color(struct RBasic *o, int c) { o->color = c; }
static int       _mrb_basic_color(struct RBasic *o)  { return o->color; }

static mrb_bool _mrb_nil_p(mrb_value o)     { return mrb_nil_p(o); }
static mrb_int  _mrb_fixnum(mrb_value o)     { return mrb_fixnum(o); }
static double   _mrb_float(mrb_value o)      { return (double)mrb_float(o); }
static void*    _mrb_ptr(mrb_value o)       { return mrb_ptr(o); }
static void*    _mrb_cptr(mrb_value o)      { return mrb_cptr(o); }
static void*    _mrb_range_ptr(mrb_value o) { return mrb_ptr(o); }
static mrb_sym  _mrb_symbol(mrb_value o)    { return mrb_symbol(o); }
static enum mrb_vtype  _mrb_type(mrb_value o)    { return mrb_type(o); }
static struct RObject* _mrb_obj_ptr(mrb_value v) { return mrb_obj_ptr(v); }

static mrb_value _mrb_uintptr_value(mrb_state *mrb, uintptr_t p) { return mrb_cptr_value(mrb, (void *)p); }
static mrb_value _mrb_ptr_to_str(mrb_state *mrb, uintptr_t p) { return mrb_ptr_to_str(mrb, (void *)p); }

// _mrb_is_nil returns true if mrb_value is nil value,
// RBasic object with NIL pointer
static mrb_bool _mrb_is_nil(mrb_value o)    {
	if (mrb_nil_p(o) || (mrb_immediate_p(o) && (mrb_ptr(o) == NULL))) {
		return 1;
	}
	return 0;
}

// Hash
static int  _MRB_RHASH_PROCDEFAULT_P(mrb_value h) { return MRB_RHASH_PROCDEFAULT_P(h); }

// Env, Proc
static void _set_last_stack_value(mrb_state *mrb, mrb_value v) { *(mrb->c->stack + 1) = v; }
static void _MRB_ENV_SET_STACK_LEN(struct REnv *e, mrb_int len) { MRB_ENV_SET_STACK_LEN(e, len); }
static mrb_int _MRB_ENV_STACK_LEN(struct REnv *e) { return MRB_ENV_STACK_LEN(e); }
static uint32_t _mrb_rproc_flags(struct RProc *p) { return p->flags; }
static void _MRB_PROC_SET_TARGET_CLASS(mrb_state *mrb, struct RProc *p, struct RClass *c)  { 
   MRB_PROC_SET_TARGET_CLASS(p,c);
}
static int  _MRB_PROC_CFUNC_P(struct RProc *p)    { return (int)(MRB_PROC_CFUNC_P(p));  }
static mrb_func_t _MRB_PROC_CFUNC(struct RProc *p) { return MRB_PROC_CFUNC(p); }
static int  _MRB_PROC_STRICT_P(struct RProc *p)   { return (int)(MRB_PROC_STRICT_P(p)); }
static mrb_func_t _MRB_METHOD_FUNC(mrb_method_t m) { return MRB_METHOD_CFUNC(m); }
static struct RProc *_MRB_METHOD_PROC(mrb_method_t m) { return MRB_METHOD_PROC(m); }
static mrb_bool _MRB_METHOD_UNDEF_P(mrb_method_t m) { return (mrb_bool)(m == 0); }

static uint16_t _mrb_rproc_nlocals(struct RProc *p)  { return p->body.irep->nlocals; }
static mrb_irep *_rproc_body_irep(struct RProc *p)  { return p->body.irep; }
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

static mrb_method_t _MRB_METHOD_NOARG_SET(mrb_method_t m) { 
  mrb_method_t ret = m;
  MRB_METHOD_NOARG_SET(ret);
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

// Frozen macro proxy calls
static void _MRB_SET_FROZEN_FLAG(mrb_value o)   { MRB_SET_FROZEN_FLAG(mrb_basic_ptr(o)); }
static void _MRB_UNSET_FROZEN_FLAG(mrb_value o) { MRB_UNSET_FROZEN_FLAG(mrb_basic_ptr(o)); }

// vararg proxy calls, formated using Go fmt
static void    _mrb_warn(mrb_state *mrb, const char *msg) { mrb_warn(mrb, msg); }
static void    _mrb_bug(mrb_state *mrb, const char *msg)  { mrb_bug(mrb, msg);  }

// Argument helpers
static mrb_value
_mrb_get_args_first(mrb_state *mrb) {
  mrb_value arg;
  if (mrb_get_args(mrb, "|o", &arg) > 0) {
    return arg;
  } else {
    return mrb_nil_value();
  }
}

static mrb_value
_mrb_get_args_all(mrb_state *mrb) {
  mrb_value *argv;
  mrb_value a;
  int argc;

  mrb_get_args(mrb, "*!", &argv, &argc);
  return  mrb_ary_new_from_values(mrb, argc, argv);
}

static mrb_value
_mrb_get_args_all_with_block(mrb_state *mrb) {
  mrb_value *argv;
  mrb_value a, b;
  int argc;

  mrb_get_args(mrb, "*!&", &argv, &argc, &b);
  a = mrb_ary_new_from_values(mrb, argc, argv);
  mrb_ary_push(mrb, a, b);
  return a;
}

static mrb_int
_mrb_get_args_3(mrb_state *mrb, mrb_value *arg1, mrb_value *arg2, mrb_value *arg3) {
	return mrb_get_args(mrb, "|ooo", arg1, arg2, arg3);
}


static mrb_value
_mrb_get_args_block(mrb_state *mrb) {
  mrb_value arg;

  mrb_get_args(mrb, "&", &arg);
  return arg;
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
static int  _mrbc_on_eval(mrbc_context *c )                       { return c->on_eval;  }
static void _mrbc_set_on_eval(mrbc_context *c, mrb_bool v)        { c->on_eval = v; }

// GC
static mrb_bool _gc_iterating(mrb_state *mrb) { return mrb->gc.iterating; }
static mrb_bool _gc_full(mrb_state *mrb) { return mrb->gc.full; }
static mrb_bool _gc_generational(mrb_state *mrb) { return mrb->gc.generational; }
static mrb_bool _gc_out_of_memory(mrb_state *mrb) { return mrb->gc.out_of_memory; }
static mrb_bool _gc_disabled(mrb_state *mrb) { return mrb->gc.disabled; }
static void _gc_set_disabled(mrb_state *mrb, mrb_bool v) { mrb->gc.disabled = v; }
static struct RBasic *_gc_arena_peek(mrb_state *mrb, mrb_int i) {
	int max = mrb->gc.arena_idx;
	if (i < 0) { i = max + i; };
	if ((i < 0) || (i >= max)) { return NULL; };

	return mrb->gc.arena[i];
}

static int _MRB_FUNCALL_ARGC_MAX() {
#ifndef MRB_FUNCALL_ARGC_MAX
    return 16;
#else
    return MRB_FUNCALL_ARGC_MAX;
#endif
}

// Error formating using go fmt
static void _mrb_name_error(mrb_state *mrb, mrb_sym id, const char *msg) { mrb_name_error(mrb, id, msg); }

// Handle Go funcs as C callbacks
static void
_mrb_proc_set_env(mrb_state *mrb, struct RProc *p, mrb_value v)
{
  struct REnv *e;
  int ai, i;

  if (p == NULL) {
    mrb_raise(mrb, E_TYPE_ERROR, "RProc is empty.");
    return;
  }

  if (MRB_PROC_ENV(p) != NULL) {
    mrb_raise(mrb, E_TYPE_ERROR, "Expected emty RProc enviroment.");
    return;
  }

  ai = mrb_gc_arena_save(mrb);
  e = (struct REnv*)mrb_obj_alloc(mrb, MRB_TT_ENV, NULL);
  p->e.env = e;
  p->flags |= MRB_PROC_ENVSET;
  mrb_gc_arena_restore(mrb, ai);

  MRB_ENV_UNSHARE_STACK(e);
  MRB_ENV_SET_STACK_LEN(e, 1);
  e->stack = (mrb_value*)mrb_malloc(mrb, sizeof(mrb_value));
  e->stack[0] = v;
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
  if (MRB_ENV_STACK_LEN(e) < 1) {
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
  if (idx < 0 || MRB_ENV_STACK_LEN(e) <= idx) {
    mrb_raisef(mrb, E_INDEX_ERROR, "Env index out of range: %S (expected: 0 <= index < %S)",
               mrb_fixnum_value(idx), mrb_fixnum_value(MRB_ENV_STACK_LEN(e)));
  }

  return e->stack[idx];
}

// Callbacks
extern int  go_partial_hook_callback(struct mrb_parser_state *p);
extern void go_mrb_callback(mrb_state *mrb, mrb_value *self, mrb_value *ret);
extern void go_mrb_proc_callback(mrb_state *mrb, mrb_value *self, mrb_value *ret);
extern int  go_hash_callback(mrb_state *mrb, mrb_value key, mrb_value val, void *data);
extern int  go_each_object_callback(mrb_state *mrb, struct RBasic *obj, void *data);
extern void go_gofunc_callback(mrb_state *mrb, mrb_value *self, mrb_value *ret);
extern void go_mrb_func_callback(mrb_state *mrb, mrb_value *self, mrb_value *ret);
extern void go_mrb_func_env_callback(mrb_state *mrb, mrb_value *self, mrb_value *ret);

mrb_value set_mrb_callback(mrb_state *mrb, mrb_value self);
mrb_value set_gofunc_callback(mrb_state *mrb, mrb_value self);
mrb_value set_mrb_env_callback(mrb_state *mrb, mrb_value self);
int set_hash_callback(mrb_state *mrb, mrb_value key, mrb_value val, void *data);
int set_each_object_callback(struct mrb_state *mrb, struct RBasic *obj, void *data);

// static void _mrb_copy_value(mrb_value *v1, mrb_value *v2) { *v1=*v2; }
void _mrb_proc_new_cfunc(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, int param_count);
void _mrb_method_new_cfunc(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec);

// cmd helpers
extern void mrb_codedump_all(mrb_state*, struct RProc*);
static void _set_parser_s(struct mrb_parser_state *parser, char *str) {
    parser->s = str;
    parser->send = str + strlen(str);
};

#if defined(__cplusplus)
}  /* extern "C" { */
#endif

#endif  /* GOMRB_H */