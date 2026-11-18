/*
** go-aot.h - runtime support for ORuby AOT compiled code (compiler/)
**
** Compiled Ruby methods run as Go functions behind C function trampolines.
** A C-level raise (longjmp) must never cross Go frames, so every mruby API
** that can raise is called from here under mrb_protect_error(), which also
** pops call frames left by the unwinding. On error the pending exception
** stays in mrb->exc and the Go side checks for it.
*/

#ifndef GOAOT_H
#define GOAOT_H

#include "go-mrb.h"

/* Bodies below run under _aot_protect(); AOT_PROTECT only marks raising code */
#define AOT_PROTECT(expr) do { expr; } while (0)

/* Run body under mrb_protect_error, keep exception pending in mrb->exc */
static mrb_value
_aot_protect(mrb_state *mrb, mrb_protect_error_func *body, void *ud)
{
  mrb_bool err = FALSE;
  mrb_value r = mrb_protect_error(mrb, body, ud, &err);
  if (err) {
    mrb->exc = mrb_obj_ptr(r);
    return mrb_nil_value();
  }
  return r;
}

int _orb_vm_hooked(mrb_state *mrb);
void _orb_vm_poll(mrb_state *mrb);

static mrb_value
_aot_poll_body(mrb_state *mrb, void *ud)
{
  (void)ud;
  if (_orb_vm_hooked(mrb)) mrb->code_fetch_hook = NULL;
  _orb_vm_poll(mrb);
  return mrb_nil_value();
}

/* Poll pending signals, timeouts, thread interrupts and preemption ticks */
static void
_aot_poll(mrb_state *mrb)
{
  _aot_protect(mrb, _aot_poll_body, NULL);
}

enum {
  AOT_OP_CONST_GET = 1,   /* a::sym (with ancestors, const_missing) */
  AOT_OP_CONST_SET,       /* a::sym = b */
  AOT_OP_CV_GET,          /* class variable sym of module a */
  AOT_OP_CV_SET,          /* class variable sym of module a = b */
  AOT_OP_IV_SET,          /* a.@sym = b */
  AOT_OP_DEFINE_CLASS,    /* class a::sym < b */
  AOT_OP_DEFINE_MODULE,   /* module a::sym */
  AOT_OP_SINGLETON_CLASS, /* a.singleton_class */
  AOT_OP_ARY_CAT,         /* OP_ARYCAT semantics, returns new a */
  AOT_OP_ARY_PUSH,        /* a << b */
  AOT_OP_ARY_SPLAT,       /* [*a] */
  AOT_OP_ARY_SET,         /* a[c] = b (c is integer) */
  AOT_OP_HASH_SET,        /* a[b] = c */
  AOT_OP_HASH_GET,        /* a[b] */
  AOT_OP_HASH_CAT,        /* a.merge!(b) */
  AOT_OP_STR_CAT,         /* a << b (b converted with to_s) */
  AOT_OP_RANGE_NEW,       /* a..b / a...b (c true for exclusive) */
  AOT_OP_INTERN,          /* a.to_sym (a must be String) */
  AOT_OP_ALIAS,           /* alias in module a: sym (new) = b (old symbol) */
  AOT_OP_UNDEF,           /* undef sym in module a */
  AOT_OP_TO_PROC,         /* block argument conversion */
  AOT_OP_RESCUE_P,        /* a.is_a?(b), TypeError unless b is a class/module */
  AOT_OP_GV_SET,          /* $sym = a */
  AOT_OP_ARGNUM_ERROR,    /* raise ArgumentError (given a, expected b..c) */
  AOT_OP_RAISE,           /* raise exception object a */
  AOT_OP_YIELD,           /* call block a with args from array b */
  AOT_OP_METHOD_ADDED,    /* a.method_added(sym) */
  AOT_OP_NOMATCH,         /* raise NoMatchingPatternError */
  AOT_OP_LOCALJUMP,       /* raise LocalJumpError with message from string a */
  AOT_OP_HASH_DUP,        /* a.dup for keyword hashes */
  AOT_OP_KARG,            /* kdict a: fetch and delete key sym, raise if missing */
  AOT_OP_KEYEND,          /* kdict a: raise if any key left */
  AOT_OP_ENSURE_ARY,      /* raise TypeError unless a is an Array */
  AOT_OP_ENSURE_HASH,     /* raise TypeError unless a is a Hash */
  AOT_OP_ENSURE_STR,      /* raise TypeError unless a is a String */
  AOT_OP_NOBLOCK_ERROR,   /* raise ArgumentError, "no block accepted" */
  AOT_OP_CONST_GET_VM,    /* bare constant lookup in ancestors of a (incl. Object) */
};

/* defined?(Const) ancestor part: like _aot_const_get_vm without raising */
static mrb_bool
_aot_const_defined_vm(mrb_state *mrb, mrb_value mod, mrb_sym sym)
{
  struct RClass *base = mrb_class_ptr(mod);
  for (struct RClass *c = base; c; c = c->super) {
    struct RClass *ivc = (c->tt == MRB_TT_ICLASS) ? c->c : c;
    if (!MRB_FLAG_TEST(c, MRB_FL_CLASS_IS_PREPENDED) && mrb_const_defined_at(mrb, mrb_obj_value(ivc), sym)) {
      return TRUE;
    }
  }
  return mrb_const_defined_at(mrb, mrb_obj_value(mrb->object_class), sym);
}

/* defined?(super) for method mid found in target class tc */
static mrb_bool
_aot_super_defined(mrb_state *mrb, struct RClass *tc, mrb_sym mid)
{
  if (tc == NULL || mid == 0 || tc->super == NULL) return FALSE;
  struct RClass *c = tc->super;
  mrb_method_t m = mrb_method_search_vm(mrb, &c, mid);
  return !MRB_METHOD_UNDEF_P(m);
}

/* Ancestor part of OP_GETCONST lookup (lexical scope is searched in Go) */
static mrb_value
_aot_const_get_vm(mrb_state *mrb, mrb_value mod, mrb_sym sym)
{
  struct RClass *base = mrb_class_ptr(mod);
  struct RClass *c = base;

  while (c) {
    struct RClass *ivc = (c->tt == MRB_TT_ICLASS) ? c->c : c;
    if (!MRB_FLAG_TEST(c, MRB_FL_CLASS_IS_PREPENDED) &&
        mrb_const_defined_at(mrb, mrb_obj_value(ivc), sym)) {
      return mrb_const_get(mrb, mrb_obj_value(ivc), sym);
    }
    c = c->super;
  }
  if (base->tt == MRB_TT_MODULE || base->tt == MRB_TT_SCLASS) {
    mrb_value obj = mrb_obj_value(mrb->object_class);
    if (mrb_const_defined_at(mrb, obj, sym)) {
      return mrb_const_get(mrb, obj, sym);
    }
  }
  return mrb_funcall_id(mrb, mod, MRB_SYM(const_missing), 1, mrb_symbol_value(sym));
}

static mrb_value
_aot_op_impl(mrb_state *mrb, int op, mrb_value a, mrb_value b, mrb_value c, mrb_sym sym)
{
  mrb_value result = mrb_nil_value();

  switch (op) {
  case AOT_OP_CONST_GET:
    AOT_PROTECT(result = mrb_const_get(mrb, a, sym));
    break;
  case AOT_OP_CONST_GET_VM:
    AOT_PROTECT(result = _aot_const_get_vm(mrb, a, sym));
    break;
  case AOT_OP_CONST_SET:
    AOT_PROTECT(mrb_const_set(mrb, a, sym, b); result = b);
    break;
  case AOT_OP_CV_GET:
    AOT_PROTECT(result = mrb_mod_cv_get(mrb, mrb_class_ptr(a), sym));
    break;
  case AOT_OP_CV_SET:
    AOT_PROTECT(mrb_mod_cv_set(mrb, mrb_class_ptr(a), sym, b); result = b);
    break;
  case AOT_OP_IV_SET:
    AOT_PROTECT(mrb_iv_set(mrb, a, sym, b); result = b);
    break;
  case AOT_OP_DEFINE_CLASS:
    AOT_PROTECT(result = mrb_obj_value(mrb_vm_define_class(mrb, a, b, sym)));
    break;
  case AOT_OP_DEFINE_MODULE:
    AOT_PROTECT(result = mrb_obj_value(mrb_vm_define_module(mrb, a, sym)));
    break;
  case AOT_OP_SINGLETON_CLASS:
    AOT_PROTECT(result = mrb_singleton_class(mrb, a));
    break;
  case AOT_OP_ARY_CAT:
    AOT_PROTECT(
      if (mrb_nil_p(a)) {
        result = mrb_ary_splat(mrb, b);
      }
      else {
        mrb_value splat = mrb_array_p(b) ? b : mrb_ary_splat(mrb, b);
        mrb_ensure_array_type(mrb, a);
        mrb_ary_concat(mrb, a, splat);
        result = a;
      }
    );
    break;
  case AOT_OP_ARY_PUSH:
    AOT_PROTECT(mrb_ensure_array_type(mrb, a); mrb_ary_push(mrb, a, b); result = a);
    break;
  case AOT_OP_ARY_SPLAT:
    AOT_PROTECT(result = mrb_ary_splat(mrb, a));
    break;
  case AOT_OP_ARY_SET:
    AOT_PROTECT(mrb_ary_set(mrb, a, mrb_integer(c), b); result = b);
    break;
  case AOT_OP_HASH_SET:
    AOT_PROTECT(mrb_ensure_hash_type(mrb, a); mrb_hash_set(mrb, a, b, c); result = c);
    break;
  case AOT_OP_HASH_GET:
    AOT_PROTECT(result = mrb_hash_get(mrb, a, b));
    break;
  case AOT_OP_HASH_CAT:
    AOT_PROTECT(mrb_ensure_hash_type(mrb, a); mrb_hash_merge(mrb, a, b); result = a);
    break;
  case AOT_OP_STR_CAT:
    AOT_PROTECT(mrb_ensure_string_type(mrb, a); mrb_str_concat(mrb, a, b); result = a);
    break;
  case AOT_OP_RANGE_NEW:
    AOT_PROTECT(result = mrb_range_new(mrb, a, b, mrb_test(c)));
    break;
  case AOT_OP_INTERN:
    AOT_PROTECT(mrb_ensure_string_type(mrb, a); result = mrb_symbol_value(mrb_intern_str(mrb, a)));
    break;
  case AOT_OP_ALIAS:
    AOT_PROTECT(
      mrb_alias_method(mrb, mrb_class_ptr(a), sym, mrb_symbol(b));
      mrb_method_added(mrb, mrb_class_ptr(a), sym);
    );
    break;
  case AOT_OP_UNDEF:
    AOT_PROTECT(mrb_undef_method_id(mrb, mrb_class_ptr(a), sym));
    break;
  case AOT_OP_TO_PROC:
    if (mrb_nil_p(a) || mrb_proc_p(a)) return a;
    AOT_PROTECT(result = mrb_type_convert(mrb, a, MRB_TT_PROC, MRB_SYM(to_proc)));
    break;
  case AOT_OP_RESCUE_P:
    AOT_PROTECT(
      switch (mrb_type(b)) {
      case MRB_TT_CLASS:
      case MRB_TT_MODULE:
      case MRB_TT_SCLASS:
        break;
      default:
        mrb_raise(mrb, E_TYPE_ERROR, "class or module required for rescue clause");
      }
      result = mrb_bool_value(mrb_obj_is_kind_of(mrb, a, mrb_class_ptr(b)));
    );
    break;
  case AOT_OP_GV_SET:
    AOT_PROTECT(mrb_gv_set(mrb, sym, a); result = a);
    break;
  case AOT_OP_ARGNUM_ERROR:
    AOT_PROTECT(mrb_argnum_error(mrb, mrb_integer(a), (int)mrb_integer(b), (int)mrb_integer(c)));
    break;
  case AOT_OP_RAISE:
    if (mrb_nil_p(a)) mrb->exc = NULL; else mrb->exc = mrb_obj_ptr(a);
    return mrb_nil_value();
  case AOT_OP_YIELD:
    AOT_PROTECT(result = mrb_yield_argv(mrb, a, RARRAY_LEN(b), RARRAY_PTR(b)));
    break;
  case AOT_OP_METHOD_ADDED:
    AOT_PROTECT(mrb_method_added(mrb, mrb_class_ptr(a), sym));
    break;
  case AOT_OP_NOMATCH:
    AOT_PROTECT(mrb_raise(mrb, mrb_exc_get_id(mrb, MRB_ERROR_SYM(NoMatchingPatternError)), "pattern not matched"));
    break;
  case AOT_OP_LOCALJUMP:
    AOT_PROTECT(mrb_raise(mrb, E_LOCALJUMP_ERROR, RSTRING_PTR(a)));
    break;
  case AOT_OP_HASH_DUP:
    AOT_PROTECT(result = mrb_hash_dup(mrb, a));
    break;
  case AOT_OP_KARG:
    AOT_PROTECT(
      mrb_value k = mrb_symbol_value(sym);
      if (!mrb_hash_p(a) || !mrb_hash_key_p(mrb, a, k)) {
        mrb_raisef(mrb, E_ARGUMENT_ERROR, "missing keyword: %v", k);
      }
      result = mrb_hash_delete_key(mrb, a, k);
    );
    break;
  case AOT_OP_KEYEND:
    AOT_PROTECT(
      if (mrb_hash_p(a) && !mrb_hash_empty_p(mrb, a)) {
        mrb_value key1 = mrb_hash_first_key(mrb, a);
        mrb_raisef(mrb, E_ARGUMENT_ERROR, "unknown keyword: %v", key1);
      }
    );
    break;
  case AOT_OP_ENSURE_ARY:
    AOT_PROTECT(mrb_ensure_array_type(mrb, a); result = a);
    break;
  case AOT_OP_ENSURE_HASH:
    AOT_PROTECT(mrb_ensure_hash_type(mrb, a); result = a);
    break;
  case AOT_OP_ENSURE_STR:
    AOT_PROTECT(mrb_ensure_string_type(mrb, a); result = a);
    break;
  case AOT_OP_NOBLOCK_ERROR:
    AOT_PROTECT(mrb_raise(mrb, E_ARGUMENT_ERROR, "no block accepted"));
    break;
  default:
    break;
  }
  return result;
}

struct aot_op_args {
  int op;
  mrb_value a, b, c;
  mrb_sym sym;
};

static mrb_value
_aot_op_body(mrb_state *mrb, void *ud)
{
  struct aot_op_args *x = (struct aot_op_args*)ud;
  return _aot_op_impl(mrb, x->op, x->a, x->b, x->c, x->sym);
}

static mrb_value
_aot_op(mrb_state *mrb, int op, mrb_value a, mrb_value b, mrb_value c, mrb_sym sym)
{
  switch (op) {
  case AOT_OP_RAISE:
    if (mrb_nil_p(a)) mrb->exc = NULL; else mrb->exc = mrb_obj_ptr(a);
    return mrb_nil_value();
  case AOT_OP_TO_PROC:
    if (mrb_nil_p(a) || mrb_proc_p(a)) return a;
    break;
  }
  struct aot_op_args x = { op, a, b, c, sym };
  return _aot_protect(mrb, _aot_op_body, &x);
}

/* Protected method call. Error is reported through mrb->exc. */
struct aot_call_args {
  mrb_value self;
  mrb_sym mid;
  mrb_int argc;
  const mrb_value *argv;
  mrb_value blk;
};

static mrb_value
_aot_funcall_body(mrb_state *mrb, void *ud)
{
  struct aot_call_args *x = (struct aot_call_args*)ud;
  mrb_value blk = x->blk;
  if (!mrb_nil_p(blk) && !mrb_proc_p(blk)) {
    blk = mrb_type_convert(mrb, blk, MRB_TT_PROC, MRB_SYM(to_proc));
  }
  return mrb_funcall_with_block(mrb, x->self, x->mid, x->argc, x->argv, blk);
}

static mrb_value
_aot_funcall(mrb_state *mrb, mrb_value self, mrb_sym mid, mrb_int argc, const mrb_value *argv, mrb_value blk)
{
  struct aot_call_args x = { self, mid, argc, argv, blk };
  return _aot_protect(mrb, _aot_funcall_body, &x);
}

static mrb_value
_aot_yield_body(mrb_state *mrb, void *ud)
{
  struct aot_call_args *x = (struct aot_call_args*)ud;
  return mrb_yield_argv(mrb, x->self, x->argc, x->argv);
}

/* Protected block call */
static mrb_value
_aot_yield(mrb_state *mrb, mrb_value blk, mrb_int argc, const mrb_value *argv)
{
  struct aot_call_args x = { blk, 0, argc, argv, mrb_nil_value() };
  return _aot_protect(mrb, _aot_yield_body, &x);
}

/* Visibility check for explicit receiver calls (recv.foo). */
struct aot_vis_args {
  mrb_value self, recv;
  mrb_sym mid;
  mrb_int argc;
  const mrb_value *argv;
};

static mrb_value
_aot_check_visibility_body(mrb_state *mrb, void *ud)
{
  struct aot_vis_args *x = (struct aot_vis_args*)ud;
  struct RClass *c = mrb_class(mrb, x->recv);
  mrb_method_t m = mrb_method_search_vm(mrb, &c, x->mid);
  if (MRB_METHOD_UNDEF_P(m)) return mrb_true_value(); /* method_missing handles it */
  if (m.flags & MRB_METHOD_PRIVATE_FL) {
    mrb_value args = mrb_ary_new_from_values(mrb, x->argc, x->argv);
    mrb_no_method_error(mrb, x->mid, args, "private method '%n' called for %T", x->mid, x->recv);
  }
  if ((m.flags & MRB_METHOD_PROTECTED_FL) && !mrb_obj_is_kind_of(mrb, x->self, c)) {
    mrb_value args = mrb_ary_new_from_values(mrb, x->argc, x->argv);
    mrb_no_method_error(mrb, x->mid, args, "protected method '%n' called for %T", x->mid, x->recv);
  }
  return mrb_true_value();
}

static mrb_bool
_aot_check_visibility(mrb_state *mrb, mrb_value self, mrb_value recv, mrb_sym mid, mrb_int argc, const mrb_value *argv)
{
  /* fast path: public method */
  struct RClass *c = mrb_class(mrb, recv);
  mrb_method_t m = mrb_method_search_vm(mrb, &c, mid);
  if (MRB_METHOD_UNDEF_P(m) || !(m.flags & (MRB_METHOD_PRIVATE_FL|MRB_METHOD_PROTECTED_FL))) return TRUE;
  struct aot_vis_args x = { self, recv, mid, argc, argv };
  _aot_protect(mrb, _aot_check_visibility_body, &x);
  return mrb->exc == NULL;
}

/*
** __aot_dispatch__(mode, mid, tc, *args, [kdict], &blk)
**
** Re-dispatches a call from inside a C frame, which lets compiled code
** pass keyword arguments (mode & 2) and call super methods (mode & 1)
** starting the method search above target class tc.
*/
static mrb_value
_aot_dispatch_cfunc(mrb_state *mrb, mrb_value self)
{
  mrb_callinfo *ci = mrb->c->ci;
  mrb_int n = ci->n;
  mrb_value *argv;
  mrb_int argc;
  mrb_value blk;

  if (n == 15) {
    mrb_value packed = ci->stack[1];
    argv = RARRAY_PTR(packed);
    argc = RARRAY_LEN(packed);
    blk = ci->stack[2];
  }
  else {
    argv = ci->stack + 1;
    argc = n;
    blk = ci->stack[n + 1];
  }
  if (argc < 3) {
    mrb_raise(mrb, E_ARGUMENT_ERROR, "__aot_dispatch__: bad arguments");
  }

  mrb_int mode = mrb_integer(argv[0]);
  mrb_sym mid = mrb_symbol(argv[1]);
  struct RClass *tc = mrb_nil_p(argv[2]) ? NULL : (struct RClass*)mrb_cptr(argv[2]);
  mrb_int kw = (mode & 2) ? 1 : 0;
  mrb_int pargc = argc - 3 - kw;
  mrb_value kdict = kw ? argv[argc - 1] : mrb_nil_value();
  mrb_value *pargv = argv + 3;

  struct RClass *c;
  if (mode & 1) {
    if (tc == NULL) {
      mrb_raise(mrb, E_NOMETHOD_ERROR, "super called outside of method");
    }
    c = tc->super;
  }
  else {
    c = mrb_class(mrb, self);
  }

  mrb_method_t m;
  mrb_bool found = FALSE;
  if (c != NULL) {
    m = mrb_method_search_vm(mrb, &c, mid);
    found = !MRB_METHOD_UNDEF_P(m);
  }
  if (!found) {
    mrb_value args = mrb_ary_new_from_values(mrb, pargc, pargv);
    if (kw && mrb_hash_p(kdict) && !mrb_hash_empty_p(mrb, kdict)) {
      mrb_ary_push(mrb, args, kdict);
    }
    if (mode & 1) {
      mrb_no_method_error(mrb, mid, args, "super: no superclass method '%n' for %T", mid, self);
    }
    mrb_ary_unshift(mrb, args, mrb_symbol_value(mid));
    return mrb_funcall_with_block(mrb, self, MRB_SYM(method_missing), RARRAY_LEN(args), RARRAY_PTR(args), blk);
  }

  /* Rewrite the current frame so it looks like a direct call of mid. */
  mrb_gc_protect(mrb, blk);
  if (!mrb_nil_p(kdict)) mrb_gc_protect(mrb, kdict);
  if (pargc < 15) {
    mrb_value tmp[16];
    for (mrb_int i = 0; i < pargc; i++) tmp[i] = pargv[i];
    mrb_stack_extend(mrb, pargc + 3);
    ci = mrb->c->ci;
    mrb_value *st = ci->stack;
    for (mrb_int i = 0; i < pargc; i++) st[i + 1] = tmp[i];
    if (kw) st[pargc + 1] = kdict;
    st[pargc + 1 + kw] = blk;
    ci->n = (uint8_t)pargc;
  }
  else {
    mrb_value ary = mrb_ary_new_from_values(mrb, pargc, pargv);
    mrb_stack_extend(mrb, 4);
    ci = mrb->c->ci;
    mrb_value *st = ci->stack;
    st[1] = ary;
    if (kw) st[2] = kdict;
    st[2 + kw] = blk;
    ci->n = 15;
  }
  ci->kw = kw;
  ci->mid = mid;
  ci->blk = mrb_proc_p(blk) ? mrb_proc_ptr(blk) : NULL;
  mrb_vm_ci_target_class_set(ci, c);

  if (MRB_METHOD_FUNC_P(m)) {
    return MRB_METHOD_FUNC(m)(mrb, self);
  }
  return mrb_exec_irep(mrb, self, MRB_METHOD_PROC(m));
}

/*
** Proc#call replacement. mruby implements Proc#call with an OP_CALL iseq,
** which mishandles C function procs when entered from C (mrb_funcall), as
** done by compiled code and e.g. Hash default procs. C function procs are
** called directly here; other procs use the original implementation.
*/
static const struct RProc *_aot_orig_call_proc = NULL;

static mrb_value
_aot_proc_call(mrb_state *mrb, mrb_value self)
{
  struct RProc *p = mrb_proc_ptr(self);
  if (MRB_PROC_CFUNC_P(p)) {
    mrb_callinfo *ci = mrb->c->ci;
    ci->proc = p;
    return MRB_PROC_CFUNC(p)(mrb, self);
  }
  return mrb_exec_irep(mrb, self, _aot_orig_call_proc);
}

static void
_aot_init(mrb_state *mrb)
{
  {
    struct RClass *pc = mrb->proc_class;
    mrb_method_t m = mrb_method_search_vm(mrb, &pc, MRB_SYM(call));
    if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_PROC_P(m) && !MRB_PROC_CFUNC_P(MRB_METHOD_PROC(m))) {
      _aot_orig_call_proc = MRB_METHOD_PROC(m);
      const char *names[] = { "call", "[]", "===", "yield" };
      for (int i = 0; i < 4; i++) {
        mrb_define_method_id(mrb, mrb->proc_class, mrb_intern_cstr(mrb, names[i]), _aot_proc_call, MRB_ARGS_ANY());
      }
    }
  }
  mrb_define_method_id(mrb, mrb->object_class, mrb_intern_lit(mrb, "__aot_dispatch__"), _aot_dispatch_cfunc, MRB_ARGS_ANY());
  /* define on BasicObject too so every receiver has it */
  struct RClass *bo = mrb_class_get(mrb, "BasicObject");
  if (bo) {
    mrb_define_method_id(mrb, bo, mrb_intern_lit(mrb, "__aot_dispatch__"), _aot_dispatch_cfunc, MRB_ARGS_ANY());
  }
}

/* Define compiled method (Go callback idx) using scope default visibility */
struct aot_def_args {
  struct RClass *c;
  mrb_sym mid;
  mrb_int idx;
};

static mrb_value
_aot_define_method_body(mrb_state *mrb, void *ud)
{
  struct aot_def_args *x = (struct aot_def_args*)ud;
  mrb_method_t m;
  mrb_value at = mrb_fixnum_value(x->idx);
  struct RProc *proc = mrb_proc_new_cfunc_with_env(mrb, set_mrb_env_callback, 1, &at);
  mrb_proc_set_cfunc_aspec(proc, MRB_ARGS_ANY());
  MRB_METHOD_FROM_PROC(m, proc);
  MRB_METHOD_SET_VISIBILITY(m, MRB_METHOD_VDEFAULT_FL);
  mrb_define_method_raw(mrb, x->c, x->mid, m);
  mrb_method_added(mrb, x->c, x->mid);
  return mrb_nil_value();
}

static void
_aot_define_method(mrb_state *mrb, struct RClass *c, mrb_sym mid, mrb_int idx)
{
  struct aot_def_args x = { c, mid, idx };
  _aot_protect(mrb, _aot_define_method_body, &x);
}

/*
** Compiled top level code and class bodies run in the callinfo of their
** caller. Like VM frames they get their own default visibility scope
** (vis: 0 public, 1 private), restored afterwards. mruby keeps the scope
** either in the env attached to the callinfo or in the callinfo itself;
** neither ci->u nor ci->proc may be replaced: the VM relocates on-stack
** envs through ci->u when the stack is reallocated.
*/
struct aot_scope {
  struct REnv *env;
  uint32_t env_flags;
  uint8_t vis;
};

static void
_aot_scope_push(mrb_state *mrb, int vis, struct aot_scope *s)
{
  mrb_callinfo *ci = mrb->c->ci;
  struct REnv *e = (ci->u.env && ci->u.env->tt == MRB_TT_ENV) ? ci->u.env : NULL;
  s->env = e;
  s->vis = ci->vis;
  if (e) {
    s->env_flags = e->flags;
    MRB_ENV_SET_VISIBILITY(e, vis);
    MRB_ENV_CLEAR_MODFUNC(e);
    MRB_CI_SET_VISIBILITY(ci, vis);
  }
  else {
    s->env_flags = 0;
    ci->vis = 0;
    MRB_CI_SET_VISIBILITY(ci, vis);
  }
}

static void
_aot_scope_pop(mrb_state *mrb, struct aot_scope *s)
{
  mrb_callinfo *ci = mrb->c->ci;
  if (s->env) {
    /* restore only visibility (bits 16-17) and module_function (bit 19) */
    const uint32_t mask = (UINT32_C(3) << 16) | (UINT32_C(1) << 19);
    s->env->flags = (s->env->flags & ~mask) | (s->env_flags & mask);
  }
  else {
    ci->vis = s->vis;
  }
}

/*
** Fast path for calls between compiled methods: if recv.mid resolves to a
** compiled method (C function with Go callback index env) callable here
** without visibility checks, return its callback index and owner class.
*/
static mrb_int
_aot_lookup_compiled(mrb_state *mrb, mrb_value recv, mrb_sym mid, int fcall, struct RClass **owner)
{
  struct RClass *c = mrb_class(mrb, recv);
  mrb_method_t m = mrb_method_search_vm(mrb, &c, mid);
  if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_PROC_P(m)) return -1;
  if (!fcall && (m.flags & (MRB_METHOD_PRIVATE_FL|MRB_METHOD_PROTECTED_FL))) return -1;
  const struct RProc *p = MRB_METHOD_PROC(m);
  if (!p || !MRB_PROC_CFUNC_P(p) || MRB_PROC_ALIAS_P(p) || !MRB_PROC_ENV_P(p)) return -1;
  if (MRB_PROC_CFUNC(p) != set_mrb_env_callback) return -1;
  struct REnv *e = MRB_PROC_ENV(p);
  if (MRB_ENV_LEN(e) != 1 || !mrb_integer_p(e->stack[0])) return -1;
  *owner = c;
  return mrb_integer(e->stack[0]);
}

static const struct mrb_data_type _aot_handle_type;

/* Handle id of compiled block proc, or -1 */
static mrb_int
_aot_proc_block_id(mrb_value blk)
{
  if (!mrb_proc_p(blk)) return -1;
  const struct RProc *p = mrb_proc_ptr(blk);
  if (!MRB_PROC_CFUNC_P(p) || !MRB_PROC_ENV_P(p) || MRB_PROC_CFUNC(p) != set_mrb_env_callback) return -1;
  struct REnv *e = MRB_PROC_ENV(p);
  if (MRB_ENV_LEN(e) < 4) return -1;
  mrb_value d = e->stack[1];
  if (!mrb_data_p(d) || DATA_TYPE(d) != &_aot_handle_type) return -1;
  return (mrb_int)(intptr_t)DATA_PTR(d);
}

/* Arguments of current C function call in one crossing */
struct aot_ci_args {
  const mrb_value *argv;
  mrb_int argc;
  mrb_value kdict;
  mrb_value blk;
  mrb_value self_proc_env; /* unused */
};

static void
_aot_ci_args(mrb_state *mrb, struct aot_ci_args *a)
{
  mrb_callinfo *ci = mrb->c->ci;
  mrb_int n = ci->n;
  if (n == 15) {
    struct RArray *ary = mrb_ary_ptr(ci->stack[1]);
    a->argv = ARY_PTR(ary);
    a->argc = ARY_LEN(ary);
    n = 1;
  }
  else {
    a->argv = ci->stack + 1;
    a->argc = n;
  }
  a->kdict = ci->kw ? ci->stack[n + 1] : mrb_nil_value();
  a->blk = ci->stack[n + 1 + (ci->kw ? 1 : 0)];
}

/* Keyword hash passed to the current C function, or nil */
static mrb_value
_aot_ci_kdict(mrb_state *mrb)
{
  mrb_callinfo *ci = mrb->c->ci;
  if (!ci->kw) return mrb_nil_value();
  mrb_int n = ci->n == 15 ? 1 : ci->n;
  return ci->stack[n + 1];
}

/* Block passed to the current C function, or nil */
static mrb_value
_aot_ci_block(mrb_state *mrb)
{
  mrb_callinfo *ci = mrb->c->ci;
  mrb_int n = ci->n == 15 ? 1 : ci->n;
  return ci->stack[n + 1 + (ci->kw ? 1 : 0)];
}

static mrb_sym
_aot_ci_mid(mrb_state *mrb)
{
  return mrb->c->ci->mid;
}

static struct RClass *
_aot_ci_target_class(mrb_state *mrb)
{
  return mrb_vm_ci_target_class(mrb->c->ci);
}

/* Allocate a GC visible register file: n slots plus a link to parent */
static mrb_value
_aot_frame_new(mrb_state *mrb, mrb_int n, mrb_value parent)
{
  mrb_value ary = mrb_ary_new_capa(mrb, n + 1);
  struct RArray *a = mrb_ary_ptr(ary);
  mrb_value *p = ARY_PTR(a);
  for (mrb_int i = 0; i < n; i++) p[i] = mrb_nil_value();
  p[n] = parent;
  ARY_SET_LEN(a, n + 1);
  return ary;
}

static mrb_value *
_aot_frame_ptr(mrb_value ary)
{
  return ARY_PTR(mrb_ary_ptr(ary));
}

static void
_aot_write_barrier(mrb_state *mrb, mrb_value ary)
{
  mrb_write_barrier(mrb, (struct RBasic*)mrb_obj_ptr(ary));
}

static void
_aot_arena_reset(mrb_state *mrb, int ai, mrb_value frame)
{
  mrb_gc_arena_restore(mrb, ai);
  if (!mrb_nil_p(frame)) {
    mrb_write_barrier(mrb, (struct RBasic*)mrb_obj_ptr(frame));
  }
}

/*
** Compiled blocks: one shared Go trampoline (env[0] is its function index)
** plus a handle Data object (env[1]) referring to per-block Go state, freed
** by GC together with the Proc. env[2] is frame register file, env[3] self.
*/
extern void go_aot_handle_free(mrb_int state_idx, mrb_int id);

static void
_aot_handle_dfree(mrb_state *mrb, void *p)
{
  go_aot_handle_free(_mrb_get_idx(mrb), (mrb_int)(intptr_t)p);
}

static const struct mrb_data_type _aot_handle_type = { "ORubyAotHandle", _aot_handle_dfree };

static struct RProc *
_aot_proc_new(mrb_state *mrb, mrb_int tramp, mrb_int id, mrb_value frame, mrb_value self, mrb_bool lambda)
{
  mrb_value env[4];
  struct RData *d = mrb_data_object_alloc(mrb, mrb->object_class, (void*)(intptr_t)id, &_aot_handle_type);
  env[0] = mrb_fixnum_value(tramp);
  env[1] = mrb_obj_value(d);
  env[2] = frame;
  env[3] = self;
  struct RProc *p = mrb_proc_new_cfunc_with_env(mrb, set_mrb_env_callback, 4, env);
  if (lambda) p->flags |= MRB_PROC_STRICT;
  return p;
}

/* Handle id of currently running compiled block */
static mrb_int
_aot_current_block_id(mrb_state *mrb)
{
  const struct RProc *p = mrb->c->ci->proc;
  if (!p || !MRB_PROC_CFUNC_P(p) || !MRB_PROC_ENV_P(p)) return -1;
  struct REnv *e = MRB_PROC_ENV(p);
  if (MRB_ENV_LEN(e) < 4) return -1;
  mrb_value d = e->stack[1];
  if (!mrb_data_p(d) || DATA_TYPE(d) != &_aot_handle_type) return -1;
  return (mrb_int)(intptr_t)DATA_PTR(d);
}

/* Is self given to a block its lexical self (plain yield or Proc#call)? */
static mrb_bool
_aot_block_self_lexical(mrb_state *mrb, mrb_value self)
{
  if (mrb_nil_p(self)) return TRUE;
  if (!mrb_proc_p(self)) return FALSE;
  const struct RProc *cur = mrb->c->ci->proc;
  const struct RProc *sp = mrb_proc_ptr(self);
  if (sp == cur) return TRUE;
  if (cur && MRB_PROC_ENV_P(cur) && MRB_PROC_ENV_P(sp) && MRB_PROC_ENV(cur) == MRB_PROC_ENV(sp)) return TRUE;
  return FALSE;
}

static mrb_value
_aot_exc_value(mrb_state *mrb)
{
  if (mrb->exc == NULL) return mrb_nil_value();
  return mrb_obj_value(mrb->exc);
}

static inline mrb_int _aot_ary_len(mrb_value a) { return RARRAY_LEN(a); }

/* Loads which do not allocate keep the value in GC arena: compiled register
   files are not scanned by GC (or are GC objects needing write barriers) */
static inline mrb_value
_aot_protected(mrb_state *mrb, mrb_value v)
{
  if (!mrb_immediate_p(v)) mrb_gc_protect(mrb, v);
  return v;
}

static inline mrb_value
_aot_iv_get(mrb_state *mrb, mrb_value obj, mrb_sym sym)
{
  return _aot_protected(mrb, mrb_iv_get(mrb, obj, sym));
}

static inline mrb_value
_aot_gv_get(mrb_state *mrb, mrb_sym sym)
{
  return _aot_protected(mrb, mrb_gv_get(mrb, sym));
}

static inline mrb_value
_aot_ary_ref(mrb_state *mrb, mrb_value ary, mrb_int i)
{
  return _aot_protected(mrb, mrb_ary_entry(ary, i));
}
static inline void _aot_arena_restore(mrb_state *mrb, int ai) { mrb_gc_arena_restore(mrb, ai); }

#endif /* GOAOT_H */
