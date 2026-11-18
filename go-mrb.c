#include <stdio.h>
#include <string.h>
#include "go-mrb.h"

// Go-C-Go proxy callback functions
// ret values are allocated on C side

static mrb_value callback_result(mrb_state *mrb, struct RObject *previous, mrb_value ret) {
  if (mrb->exc && (mrb->exc != previous ||
      ((mrb_type(ret) == MRB_TT_EXCEPTION || mrb_type(ret) == MRB_TT_BREAK) && mrb_obj_ptr(ret) == mrb->exc))) {
    mrb_exc_raise(mrb, mrb_obj_value(mrb->exc));
  }
  return ret;
}

mrb_value set_mrb_env_callback(mrb_state *mrb, mrb_value self) {
  struct RObject *previous = mrb->exc;
  mrb_value idx = mrb_proc_cfunc_env_get(mrb, 0);
  mrb_value ret = go_mrb_func_env_callback(_mrb_get_idx(mrb), self, (int)mrb_fixnum(idx));
  return callback_result(mrb, previous, ret);
}

mrb_value set_mrb_proc_callback(mrb_state *mrb, mrb_value self) {
  struct RObject *previous = mrb->exc;
  mrb_value ret = go_mrb_proc_callback(_mrb_get_idx(mrb), self, (void *)_mrb_current_proc(mrb));
  return callback_result(mrb, previous, ret);
}

mrb_value set_gofunc_callback(mrb_state *mrb, mrb_value self) {
  struct RObject *previous = mrb->exc;
  mrb_value idx = mrb_proc_cfunc_env_get(mrb, 0);
  mrb_value ret = go_gofunc_callback(_mrb_get_idx(mrb), self, (int)mrb_fixnum(idx));
  return callback_result(mrb, previous, ret);
}

int set_hash_callback(mrb_state *mrb, mrb_value key, mrb_value val, void *data) {
  return go_hash_callback(mrb, key, val, data);
}

int set_each_object_callback(struct mrb_state *mrb, struct RBasic *obj, void *data) {
  return go_each_object_callback(mrb, obj, data);
}

void _mrb_proc_new_cfunc(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec) {
  mrb_method_t m;
  int ai = mrb_gc_arena_save(mrb);    
  mrb_value at = mrb_fixnum_value(idx);
  struct RProc *proc = mrb_proc_new_cfunc_with_env(mrb, set_gofunc_callback, 1, &at);
  mrb_proc_set_cfunc_aspec(proc, aspec);

  MRB_METHOD_FROM_PROC(m, proc);
  mrb_define_method_raw(mrb, c, id, m);
  mrb_gc_arena_restore(mrb, ai);
}

void _mrb_method_new_cfunc(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec) {
  mrb_method_t m;
  int ai = mrb_gc_arena_save(mrb);    
  mrb_value at = mrb_fixnum_value(idx);
  struct RProc *proc = mrb_proc_new_cfunc_with_env(mrb, set_mrb_env_callback, 1, &at);
  mrb_proc_set_cfunc_aspec(proc, aspec);

  MRB_METHOD_FROM_PROC(m, proc);
  mrb_define_method_raw(mrb, c, id, m);
  mrb_gc_arena_restore(mrb, ai);
}

void _define_class_method(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec) {
  //mrb_value sclass = mrb_singleton_class(mrb, mrb_obj_value(c));
  //  _mrb_method_new_cfunc(mrb, mrb_class_ptr(sclass), id, idx, aspec);
  struct RClass* sclass = mrb_singleton_class_ptr(mrb, mrb_obj_value(c));
  _mrb_method_new_cfunc(mrb, sclass, id, idx, aspec);
}

void _mrb_proc_new_class_method(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec) {
  struct RClass* sclass = mrb_singleton_class_ptr(mrb, mrb_obj_value(c));
  _mrb_proc_new_cfunc(mrb, sclass, id, idx, aspec);
}

void _mrb_proc_new_module_function(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec) {
  mrb_method_t m;
  int ai = mrb_gc_arena_save(mrb);    
  mrb_value at = mrb_fixnum_value(idx);
  struct RProc *proc = mrb_proc_new_cfunc_with_env(mrb, set_gofunc_callback, 1, &at);
  mrb_proc_set_cfunc_aspec(proc, aspec);

  MRB_METHOD_FROM_PROC(m, proc);
  struct RClass* sclass = mrb_singleton_class_ptr(mrb, mrb_obj_value(c));
  mrb_define_method_raw(mrb, sclass, id, m);

  m.flags |= MRB_METHOD_PRIVATE_FL;
  mrb_define_method_raw(mrb, c, id, m);
  mrb_gc_arena_restore(mrb, ai);
}

void _mrb_proc_new_singleton_method(mrb_state *mrb, mrb_value obj, mrb_sym id, int idx, mrb_aspec aspec) {
  struct RClass* sclass = mrb_singleton_class_ptr(mrb, obj);
  _mrb_proc_new_cfunc(mrb, sclass, id, idx, aspec);
}

void _define_singleton_method(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec) {
  struct RClass* sclass = mrb_singleton_class_ptr(mrb, mrb_obj_value(c));
  _mrb_method_new_cfunc(mrb, sclass, id, idx, aspec);
}


// MRI (through 4.1) lets a module alias, or change the visibility of, a
// method it only finds in Object (a Kernel alias of an Object method, say).
// copy_object_method() defines Object's method b in module c as a, the way
// mrb_alias_method() does; it answers FALSE when Object has no method b.
static mrb_bool copy_object_method(mrb_state *mrb, struct RClass *c, mrb_sym a, mrb_sym b) {
  struct RClass *owner = mrb->object_class;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, b);
  if (MRB_METHOD_UNDEF_P(m)) return FALSE;
  if (!MRB_METHOD_CFUNC_P(m)) {
    const struct RProc *p = MRB_METHOD_PROC(m);
    if (!MRB_PROC_CFUNC_P(p) && !MRB_PROC_ALIAS_P(p)) {
      struct RProc *pnew = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb->proc_class);
      int vis = MRB_METHOD_VISIBILITY(m);
      pnew->body.mid = b;
      pnew->upper = p;
      pnew->e.target_class = owner;
      pnew->flags |= MRB_PROC_ALIAS;
      MRB_METHOD_FROM_PROC(m, pnew);
      MRB_METHOD_SET_VISIBILITY(m, vis);
    }
  }
  mrb_define_method_raw(mrb, c, a, m);
  return TRUE;
}

// Module#__alias_from_object(new, old), used by core_ext's alias_method
static mrb_value mod_alias_from_object(mrb_state *mrb, mrb_value self) {
  mrb_sym a, b;
  mrb_get_args(mrb, "nn", &a, &b);
  return mrb_bool_value(copy_object_method(mrb, mrb_class_ptr(self), a, b));
}

// public/private/protected/module_function wrap mruby's own: names a module
// lacks but Object has are copied in first, then the original runs in the
// same callinfo, so the no-argument form still sets the caller's default.
static const char *vis_names[] = {"public", "private", "protected", "module_function"};
static mrb_func_t vis_orig[4];

static void vis_prepare_name(mrb_state *mrb, struct RClass *c, mrb_value name) {
  mrb_sym mid;
  if (mrb_symbol_p(name)) mid = mrb_symbol(name);
  else if (mrb_string_p(name)) mid = mrb_intern_str(mrb, name);
  else return;
  struct RClass *owner = c;
  if (!MRB_METHOD_UNDEF_P(mrb_method_search_vm(mrb, &owner, mid))) return;
  copy_object_method(mrb, c, mid, mid);
}

static mrb_value vis_call(mrb_state *mrb, mrb_value self, int which) {
  if (mrb_type(self) == MRB_TT_MODULE) {
    struct RClass *c = mrb_class_ptr(self);
    mrb_int argc = mrb_get_argc(mrb);
    const mrb_value *argv = mrb_get_argv(mrb);
    for (mrb_int i = 0; i < argc; i++) {
      if (mrb_array_p(argv[i])) {
        for (mrb_int j = 0; j < RARRAY_LEN(argv[i]); j++) vis_prepare_name(mrb, c, RARRAY_PTR(argv[i])[j]);
      }
      else {
        vis_prepare_name(mrb, c, argv[i]);
      }
    }
  }
  return vis_orig[which](mrb, self);
}

static mrb_value vis_public(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 0); }
static mrb_value vis_private(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 1); }
static mrb_value vis_protected(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 2); }
static mrb_value vis_module_function(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 3); }

// Strings carry mruby's own UTF-8/binary tag; any other encoding (core_ext's
// Encoding) is kept in a hidden instance variable, which instance_variables
// does not list (no '@') and dup/clone copy with the string.
static mrb_value str_enc_binary_p(mrb_state *mrb, mrb_value self) {
  return mrb_bool_value(RSTR_BINARY_P(mrb_str_ptr(self)));
}

static mrb_value str_enc_tag(mrb_state *mrb, mrb_value self) {
  return mrb_iv_get(mrb, self, MRB_SYM(__encoding));
}

static mrb_value str_enc_tag_set(mrb_state *mrb, mrb_value self) {
  mrb_value enc;
  mrb_get_args(mrb, "o", &enc);
  mrb_sym sym = MRB_SYM(__encoding);
  if (mrb_nil_p(enc)) {
    if (mrb_iv_defined(mrb, self, sym)) mrb_iv_remove(mrb, self, sym);
  }
  else {
    mrb_iv_set(mrb, self, sym, enc);
  }
  return enc;
}

// eval, instance_eval, class_eval/module_eval and Binding#eval get source
// mruby's code generator cannot compile as is (dynamic alias/undef, **nil),
// which Kernel.__preprocess_ruby (preprocess.go) rewrites. The wrappers
// rewrite the source argument in place and run mruby-eval's own function in
// the same callinfo: that is where it finds the caller's locals, constant
// scope and default visibility, which a Ruby wrapper would replace with its own.
#define EVAL_WRAPPED 6
static mrb_func_t eval_orig[EVAL_WRAPPED];

static mrb_bool src_contains(const char *s, mrb_int len, const char *pat) {
  mrb_int n = (mrb_int)strlen(pat);
  for (mrb_int i = 0; i + n <= len; i++) {
    if (s[i] == pat[0] && memcmp(s + i, pat, n) == 0) return TRUE;
  }
  return FALSE;
}

static mrb_value eval_call(mrb_state *mrb, mrb_value self, int which) {
  if (mrb_get_argc(mrb) > 0) {
    mrb_value *argv = (mrb_value *)mrb_get_argv(mrb);
    if (mrb_string_p(argv[0])) {
      const char *s = RSTRING_PTR(argv[0]);
      mrb_int len = RSTRING_LEN(argv[0]);
      if (src_contains(s, len, "alias") || src_contains(s, len, "undef") ||
          src_contains(s, len, "**nil") || src_contains(s, len, "..") ||
          src_contains(s, len, "&.") || src_contains(s, len, "BEGIN") ||
          src_contains(s, len, "END") || src_contains(s, len, "__ENCODING__") ||
          src_contains(s, len, "frozen_string_literal") ||
          src_contains(s, len, "||=") || src_contains(s, len, "&&=") ||
          src_contains(s, len, "::") || src_contains(s, len, "$") || len > 255) {
        argv[0] = mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__preprocess_ruby), 1, argv[0]);
      }
    }
  }
  return eval_orig[which](mrb, self);
}

static mrb_value eval_w0(mrb_state *mrb, mrb_value self) { return eval_call(mrb, self, 0); }
static mrb_value eval_w1(mrb_state *mrb, mrb_value self) { return eval_call(mrb, self, 1); }
static mrb_value eval_w2(mrb_state *mrb, mrb_value self) { return eval_call(mrb, self, 2); }
static mrb_value eval_w3(mrb_state *mrb, mrb_value self) { return eval_call(mrb, self, 3); }
static mrb_value eval_w4(mrb_state *mrb, mrb_value self) { return eval_call(mrb, self, 4); }
static mrb_value eval_w5(mrb_state *mrb, mrb_value self) { return eval_call(mrb, self, 5); }

static void wrap_cfunc(mrb_state *mrb, struct RClass *c, const char *name, mrb_func_t wrapper, mrb_func_t *orig) {
  struct RClass *owner = c;
  mrb_sym mid = mrb_intern_cstr(mrb, name);
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
  if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || MRB_METHOD_CFUNC(m) == wrapper) return;
  *orig = MRB_METHOD_CFUNC(m);
  mrb_method_t w = m;  /* keeps the original's flags, aspec and visibility */
  w.as.func = wrapper;
  mrb_define_method_raw(mrb, owner, mid, w);
}

static void wrap_eval_methods(mrb_state *mrb) {
  mrb_func_t w[EVAL_WRAPPED] = {eval_w0, eval_w1, eval_w2, eval_w3, eval_w4, eval_w5};
  struct RClass *basic = mrb->object_class->super;
  wrap_cfunc(mrb, mrb->kernel_module, "eval", w[0], &eval_orig[0]);
  wrap_cfunc(mrb, mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(mrb->kernel_module))), "eval", w[1], &eval_orig[1]);
  if (basic) wrap_cfunc(mrb, basic, "instance_eval", w[2], &eval_orig[2]);
  wrap_cfunc(mrb, mrb->module_class, "class_eval", w[3], &eval_orig[3]);
  wrap_cfunc(mrb, mrb->module_class, "module_eval", w[4], &eval_orig[4]);
  if (mrb_class_defined_id(mrb, MRB_SYM(Binding))) {
    wrap_cfunc(mrb, mrb_class_get(mrb, "Binding"), "eval", w[5], &eval_orig[5]);
  }
}

// raise(exc, message, backtrace): mruby's raise takes two arguments. The
// three-argument form is handled here, in C, so the backtrace has no extra
// frame; the frame itself is hidden as mruby's raise hides its own.
static mrb_func_t raise_orig[2];
void _orb_bt_keep_now(mrb_state *mrb, mrb_value exc);
mrb_noreturn void mrb_exc_raise_with_cause(mrb_state *mrb, mrb_value exc, mrb_value cause);

static mrb_value raise_call(mrb_state *mrb, mrb_value self, int which) {
  if (mrb_get_argc(mrb) == 3) {
    mrb_value exc, mesg, bt, cause = mrb_undef_value(), rest = mrb_nil_value();
    mrb_sym kw_names[] = { MRB_SYM(cause) };
    mrb_kwargs kw = { 1, 0, kw_names, &cause, &rest };
    if (mrb->c->ci->kw) mrb_get_args(mrb, "ooo:", &exc, &mesg, &bt, &kw);
    else mrb_get_args(mrb, "ooo", &exc, &mesg, &bt);
    if (!mrb_respond_to(mrb, exc, MRB_SYM(exception))) {
      mrb_raise(mrb, E_TYPE_ERROR, "exception class/object expected");
    }
    exc = mrb_funcall_id(mrb, exc, MRB_SYM(exception), 1, mesg);
    if (!mrb_obj_is_kind_of(mrb, exc, E_EXCEPTION)) {
      mrb_raise(mrb, E_TYPE_ERROR, "exception object expected");
    }
    if (!mrb_nil_p(bt)) mrb_funcall_id(mrb, exc, MRB_SYM(set_backtrace), 1, bt);
    mrb->c->ci->mid = 0;
    if (!mrb_undef_p(cause)) {
      _orb_bt_keep_now(mrb, exc);
      mrb_exc_raise_with_cause(mrb, exc, cause);
    }
    mrb_exc_raise(mrb, exc);
  }
  {
    /* raise(obj[, mesg]) with an object that is not an Exception, its class
       or a String: MRI asks obj.exception([mesg]) for the exception, which
       mruby's raise does not */
    mrb_int argc = mrb_get_argc(mrb);
    const mrb_value *argv = mrb_get_argv(mrb);
    /* an Exception with a message too: MRI asks it as well, which an
       exception(*) returning self (Timeout::ExitException) relies on; mruby
       clones it instead */
    if ((argc == 1 || argc == 2) && !mrb_class_p(argv[0]) &&
        (!mrb_exception_p(argv[0]) || argc == 2) &&
        !mrb_string_p(argv[0]) && !mrb_hash_p(argv[0]) &&
        mrb_respond_to(mrb, argv[0], MRB_SYM(exception))) {
      mrb_value exc, mesg = mrb_nil_value(), cause = mrb_undef_value(), rest = mrb_nil_value();
      mrb_sym kw_names[] = { MRB_SYM(cause) };
      mrb_kwargs kw = { 1, 0, kw_names, &cause, &rest };
      mrb_int n = mrb->c->ci->kw ? mrb_get_args(mrb, "o|o:", &exc, &mesg, &kw)
                                 : mrb_get_args(mrb, "o|o", &exc, &mesg);
      exc = n == 2 ? mrb_funcall_id(mrb, exc, MRB_SYM(exception), 1, mesg)
                   : mrb_funcall_id(mrb, exc, MRB_SYM(exception), 0);
      if (!mrb_obj_is_kind_of(mrb, exc, E_EXCEPTION)) {
        mrb_raise(mrb, E_TYPE_ERROR, "exception object expected");
      }
      mrb->c->ci->mid = 0;
      if (!mrb_undef_p(cause)) {
        _orb_bt_keep_now(mrb, exc);
        mrb_exc_raise_with_cause(mrb, exc, cause);
      }
      mrb_exc_raise(mrb, exc);
    }
  }
  if (mrb->c->ci->kw && mrb_get_argc(mrb) > 0) {
    /* raise(exc[, mesg], cause: c), as mrb_f_raise (kernel.c) raises it,
       with the stack kept for the backtrace labels */
    mrb_value exc = mrb_nil_value(), mesg = mrb_nil_value(), cause = mrb_undef_value(), rest = mrb_nil_value();
    mrb_sym kw_names[] = { MRB_SYM(cause) };
    mrb_kwargs kw = { 1, 0, kw_names, &cause, &rest };
    mrb_int argc = mrb_get_args(mrb, "|oo:", &exc, &mesg, &kw);
    if (!mrb_undef_p(cause) && !(mrb_hash_p(rest) && !mrb_hash_empty_p(mrb, rest)) && argc > 0) {
      mrb->c->ci->mid = 0;
      if (argc == 1) {
        if (mrb_string_p(exc)) {
          mesg = exc;
          exc = mrb_obj_value(E_RUNTIME_ERROR);
        }
        else {
          mesg = mrb_nil_value();
        }
      }
      exc = mrb_make_exception(mrb, exc, mesg);
      _orb_bt_keep_now(mrb, exc);
      mrb_exc_raise_with_cause(mrb, exc, cause);
    }
  }
  return raise_orig[which](mrb, self);
}

// Exception#set_backtrace as in MRI: nil, a String, an Array of String or an
// Array of Thread::Backtrace::Location (then kept for backtrace_locations)
void _orb_bt_forget(mrb_state *mrb, mrb_value exc);

static mrb_value exc_set_backtrace_m(mrb_state *mrb, mrb_value exc) {
  mrb_value bt = mrb_get_arg1(mrb), strs = bt;
  mrb_sym cache = MRB_SYM(__backtrace__), locs = MRB_SYM(__bt_locations__);
  struct RException *e = mrb_exc_ptr(exc);
  mrb_iv_remove(mrb, exc, cache);
  mrb_iv_remove(mrb, exc, locs);
  _orb_bt_forget(mrb, exc);
  if (mrb_nil_p(bt)) {
    e->backtrace = NULL;
    return bt;
  }
  if (mrb_string_p(bt)) {
    strs = mrb_ary_new_from_values(mrb, 1, &bt);
  }
  else if (mrb_array_p(bt)) {
    mrb_int n = RARRAY_LEN(bt), nstr = 0, nloc = 0;
    struct RClass *loc_class = NULL;
    mrb_value th = mrb_obj_value(mrb->object_class);
    if (mrb_const_defined(mrb, th, MRB_SYM(Thread))) {
      mrb_value t = mrb_const_get(mrb, th, MRB_SYM(Thread));
      if (mrb_const_defined(mrb, t, MRB_SYM(Backtrace))) {
        mrb_value b = mrb_const_get(mrb, t, MRB_SYM(Backtrace));
        if (mrb_const_defined(mrb, b, MRB_SYM(Location))) {
          loc_class = mrb_class_ptr(mrb_const_get(mrb, b, MRB_SYM(Location)));
        }
      }
    }
    for (mrb_int i = 0; i < n; i++) {
      mrb_value v = RARRAY_PTR(bt)[i];
      if (mrb_string_p(v)) nstr++;
      else if (loc_class && mrb_obj_is_kind_of(mrb, v, loc_class)) nloc++;
    }
    if (n > 0 && nloc == n) {
      strs = mrb_ary_new_capa(mrb, n);
      for (mrb_int i = 0; i < n; i++) {
        mrb_ary_push(mrb, strs, mrb_funcall_id(mrb, RARRAY_PTR(bt)[i], MRB_SYM(to_s), 0));
      }
      mrb_iv_set(mrb, exc, locs, mrb_ary_new_from_values(mrb, n, RARRAY_PTR(bt)));
    }
    else if (nstr != n) {
      goto type_err;
    }
  }
  else {
  type_err:
    mrb_raise(mrb, E_TYPE_ERROR, "backtrace must be an Array of String or an Array of Thread::Backtrace::Location");
  }
  e->backtrace = mrb_basic_ptr(strs);
  mrb_field_write_barrier_value(mrb, mrb_basic_ptr(exc), strs);
  return bt;
}

static mrb_value raise_w0(mrb_state *mrb, mrb_value self) { return raise_call(mrb, self, 0); }
static mrb_value raise_w1(mrb_state *mrb, mrb_value self) { return raise_call(mrb, self, 1); }

// respond_to? is false for the methods a platform does not implement
// (Kernel#fork, Process.fork), as for MRI's rb_f_notimplement
static mrb_func_t respond_to_orig;

static mrb_value respond_to_w(mrb_state *mrb, mrb_value self) {
  mrb_value r = respond_to_orig(mrb, self);
  if (mrb_test(r) && mrb_get_argc(mrb) >= 1) {
    mrb_value name = mrb_get_argv(mrb)[0];
    mrb_sym fork = MRB_SYM(fork);
    if ((mrb_symbol_p(name) && mrb_symbol(name) == fork) ||
        (mrb_string_p(name) && RSTRING_LEN(name) == 4 && memcmp(RSTRING_PTR(name), "fork", 4) == 0)) {
      struct RClass *owner = mrb_class(mrb, self);
      mrb_method_t m = mrb_method_search_vm(mrb, &owner, fork);
      struct RClass *process = mrb_class_defined_id(mrb, MRB_SYM(Process)) ?
        mrb_module_get(mrb, "Process") : NULL;
      if (!MRB_METHOD_UNDEF_P(m) && (owner == mrb->kernel_module ||
          (process && owner == mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(process)))))) {
        return mrb_false_value();
      }
    }
  }
  return r;
}

// Encodings other than UTF-8 and BINARY are a hidden tag on the string
// (00_encoding.rb). mruby's string functions don't know it, so the String
// methods that derive a string from the receiver are wrapped to carry the
// receiver's tag over to the result (and to the strings in an Array result).
#define ENC_KEEP_MAX 64
static mrb_func_t enc_keep_orig[ENC_KEEP_MAX];

static void enc_copy_tag(mrb_state *mrb, mrb_value tag, mrb_value from, mrb_value to) {
  mrb_sym sym = MRB_SYM(__encoding);
  if (!mrb_string_p(to) || mrb_obj_eq(mrb, from, to)) return;
  if (RSTR_BINARY_P(mrb_str_ptr(to)) != RSTR_BINARY_P(mrb_str_ptr(from))) return;
  if (!mrb_nil_p(mrb_iv_get(mrb, to, sym))) return;
  if (mrb_frozen_p(mrb_basic_ptr(to))) return;
  mrb_iv_set(mrb, to, sym, tag);
}

static mrb_value enc_keep_call(mrb_state *mrb, mrb_value self, int i) {
  mrb_value r = enc_keep_orig[i](mrb, self);
  if (!mrb_string_p(self)) return r;
  mrb_value tag = mrb_iv_get(mrb, self, MRB_SYM(__encoding));
  if (mrb_nil_p(tag)) return r;
  if (mrb_string_p(r)) {
    enc_copy_tag(mrb, tag, self, r);
  }
  else if (mrb_array_p(r)) {
    for (mrb_int k = 0; k < RARRAY_LEN(r); k++) enc_copy_tag(mrb, tag, self, RARRAY_PTR(r)[k]);
  }
  return r;
}

#define ENC_W(i) static mrb_value enc_keep_w##i(mrb_state *mrb, mrb_value self) { return enc_keep_call(mrb, self, i); }
ENC_W(0) ENC_W(1) ENC_W(2) ENC_W(3) ENC_W(4) ENC_W(5) ENC_W(6) ENC_W(7) ENC_W(8) ENC_W(9)
ENC_W(10) ENC_W(11) ENC_W(12) ENC_W(13) ENC_W(14) ENC_W(15) ENC_W(16) ENC_W(17) ENC_W(18) ENC_W(19)
ENC_W(20) ENC_W(21) ENC_W(22) ENC_W(23) ENC_W(24) ENC_W(25) ENC_W(26) ENC_W(27) ENC_W(28) ENC_W(29)
ENC_W(30) ENC_W(31) ENC_W(32) ENC_W(33) ENC_W(34) ENC_W(35) ENC_W(36) ENC_W(37) ENC_W(38) ENC_W(39)
ENC_W(40) ENC_W(41) ENC_W(42) ENC_W(43) ENC_W(44) ENC_W(45) ENC_W(46) ENC_W(47) ENC_W(48) ENC_W(49)
ENC_W(50) ENC_W(51) ENC_W(52) ENC_W(53) ENC_W(54) ENC_W(55) ENC_W(56) ENC_W(57) ENC_W(58) ENC_W(59)
ENC_W(60) ENC_W(61) ENC_W(62) ENC_W(63)
static mrb_func_t enc_keep_ws[ENC_KEEP_MAX] = {
  enc_keep_w0, enc_keep_w1, enc_keep_w2, enc_keep_w3, enc_keep_w4, enc_keep_w5, enc_keep_w6, enc_keep_w7,
  enc_keep_w8, enc_keep_w9, enc_keep_w10, enc_keep_w11, enc_keep_w12, enc_keep_w13, enc_keep_w14, enc_keep_w15,
  enc_keep_w16, enc_keep_w17, enc_keep_w18, enc_keep_w19, enc_keep_w20, enc_keep_w21, enc_keep_w22, enc_keep_w23,
  enc_keep_w24, enc_keep_w25, enc_keep_w26, enc_keep_w27, enc_keep_w28, enc_keep_w29, enc_keep_w30, enc_keep_w31,
  enc_keep_w32, enc_keep_w33, enc_keep_w34, enc_keep_w35, enc_keep_w36, enc_keep_w37, enc_keep_w38, enc_keep_w39,
  enc_keep_w40, enc_keep_w41, enc_keep_w42, enc_keep_w43, enc_keep_w44, enc_keep_w45, enc_keep_w46, enc_keep_w47,
  enc_keep_w48, enc_keep_w49, enc_keep_w50, enc_keep_w51, enc_keep_w52, enc_keep_w53, enc_keep_w54, enc_keep_w55,
  enc_keep_w56, enc_keep_w57, enc_keep_w58, enc_keep_w59, enc_keep_w60, enc_keep_w61, enc_keep_w62, enc_keep_w63,
};

// String.new(str): the copy has str's encoding
static mrb_func_t str_init_orig;
static mrb_value str_init_w(mrb_state *mrb, mrb_value self) {
  mrb_value r = str_init_orig(mrb, self);
  if (mrb_get_argc(mrb) >= 1) {
    mrb_value src = mrb_get_argv(mrb)[0];
    if (mrb_string_p(src)) {
      mrb_value tag = mrb_iv_get(mrb, src, MRB_SYM(__encoding));
      if (!mrb_nil_p(tag)) enc_copy_tag(mrb, tag, src, self);
    }
  }
  return r;
}

// String#+, #<<, #concat, #prepend check that the encodings are
// compatible (Kernel.__oruby_enc_concat, in Ruby) unless both strings are
// untagged with the same binary flag, and give the result that encoding
#define ENC_CAT_N 4
static mrb_func_t enc_cat_orig[ENC_CAT_N];
static const char *enc_cat_names[ENC_CAT_N] = {"+", "<<", "concat", "prepend"};

static mrb_bool enc_plain_same(mrb_state *mrb, mrb_value a, mrb_value b) {
  mrb_sym sym = MRB_SYM(__encoding);
  if (!mrb_string_p(b)) return TRUE;
  if (RSTR_BINARY_P(mrb_str_ptr(a)) != RSTR_BINARY_P(mrb_str_ptr(b))) return FALSE;
  return mrb_nil_p(mrb_iv_get(mrb, a, sym)) && mrb_nil_p(mrb_iv_get(mrb, b, sym));
}

static mrb_value enc_cat_call(mrb_state *mrb, mrb_value self, int i) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  mrb_bool plain = TRUE;
  for (mrb_int k = 0; k < argc; k++) {
    if (!enc_plain_same(mrb, self, argv[k])) { plain = FALSE; break; }
  }
  if (plain) return enc_cat_orig[i](mrb, self);
  mrb_value enc = mrb_nil_value(), acc = self;
  mrb_value kernel = mrb_obj_value(mrb->kernel_module);
  for (mrb_int k = 0; k < argc; k++) {
    if (!mrb_string_p(argv[k])) continue;
    enc = mrb_funcall_id(mrb, kernel, MRB_SYM(__oruby_enc_concat), 2, acc, argv[k]);
    acc = mrb_funcall_id(mrb, mrb_str_dup(mrb, acc), MRB_SYM(force_encoding), 1, enc);
  }
  mrb_value r = enc_cat_orig[i](mrb, self);
  if (!mrb_nil_p(enc) && mrb_string_p(r)) {
    mrb_funcall_id(mrb, r, MRB_SYM(force_encoding), 1, enc);
  }
  return r;
}
static mrb_value enc_cat_w0(mrb_state *mrb, mrb_value self) { return enc_cat_call(mrb, self, 0); }
static mrb_value enc_cat_w1(mrb_state *mrb, mrb_value self) { return enc_cat_call(mrb, self, 1); }
static mrb_value enc_cat_w2(mrb_state *mrb, mrb_value self) { return enc_cat_call(mrb, self, 2); }
static mrb_value enc_cat_w3(mrb_state *mrb, mrb_value self) { return enc_cat_call(mrb, self, 3); }

void _mrb_wrap_string_concat(mrb_state *mrb) {
  mrb_func_t ws[ENC_CAT_N] = {enc_cat_w0, enc_cat_w1, enc_cat_w2, enc_cat_w3};
  for (int i = 0; i < ENC_CAT_N; i++) {
    struct RClass *owner = mrb->string_class;
    mrb_sym mid = mrb_intern_cstr(mrb, enc_cat_names[i]);
    mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || owner != mrb->string_class || MRB_METHOD_CFUNC(m) == ws[i]) continue;
    enc_cat_orig[i] = MRB_METHOD_CFUNC(m);
    mrb_method_t w = m;
    w.as.func = ws[i];
    mrb_define_method_raw(mrb, owner, mid, w);
  }
}

// Wrap the String methods listed in names (called from Go once the
// encoding layer is loaded)
void _mrb_wrap_string_encoding(mrb_state *mrb) {
  static const char *names[] = {
    "*", "[]", "slice", "slice!", "byteslice", "upcase", "downcase", "capitalize", "swapcase",
    "strip", "lstrip", "rstrip", "chomp", "chop", "reverse", "center", "ljust", "rjust",
    "succ", "next", "squeeze", "delete", "tr", "tr_s", "scrub", "partition", "rpartition",
    "split", "lines", "chars", "delete_prefix", "delete_suffix", "chr", "each_char", "%",
    "sub", "gsub", "insert", "replace", NULL
  };
  int n = 0;
  for (int i = 0; names[i] && n < ENC_KEEP_MAX; i++) {
    struct RClass *owner = mrb->string_class;
    mrb_sym mid = mrb_intern_cstr(mrb, names[i]);
    mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || owner != mrb->string_class) continue;
    /* an alias (next/succ, slice/[]) shares its original's wrapper */
    int j = 0;
    while (j < n && enc_keep_orig[j] != MRB_METHOD_CFUNC(m)) j++;
    if (j == n) {
      enc_keep_orig[n] = MRB_METHOD_CFUNC(m);
      n++;
    }
    mrb_method_t w = m;
    w.as.func = enc_keep_ws[j];
    mrb_define_method_raw(mrb, owner, mid, w);
  }
  struct RClass *owner = mrb->string_class;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, MRB_SYM(initialize));
  if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_CFUNC_P(m) && MRB_METHOD_CFUNC(m) != str_init_w) {
    str_init_orig = MRB_METHOD_CFUNC(m);
    mrb_method_t w = m;
    w.as.func = str_init_w;
    mrb_define_method_raw(mrb, owner, MRB_SYM(initialize), w);
  }
}

// Global variables with MRI setter semantics, and trace_var, as mruby
// virtual globals: mrb_gv_get/mrb_gv_set (every assignment, from Ruby, C or
// Go) go through the slot's getter and setter. Values live in a Hash hidden
// on Kernel, under the slot's canonical name ($-0 and $/ share one).
//   kind 1: checked by Kernel.__oruby_gvcheck(name, value) -> stored value
//   kind 2: plain (traced only)
//   kind 3: computed by Kernel.__oruby_gvget(name) ($@)
// The setter then calls Kernel.__oruby_trace_fire(name, value) when
// trace_var hooks exist for the name.
#define GV_SLOTS 128
/* The slot tables and the wrapped originals are process-wide, and states
   are opened concurrently (from goroutines). The Go side serializes every
   write to them with coreMu (mruby.go): _mrb_init_oruby_core runs under it,
   and so does _mrb_gv_slot when trace_var hooks a global later. */
static char *gv_names[GV_SLOTS];
static char *gv_canon[GV_SLOTS];
static int gv_kinds[GV_SLOTS];
static int gv_n;

static mrb_value gv_storage(mrb_state *mrb) {
  mrb_value k = mrb_obj_value(mrb->kernel_module);
  mrb_sym sym = MRB_SYM(__oruby_gv);
  mrb_value h = mrb_iv_get(mrb, k, sym);
  if (!mrb_hash_p(h)) {
    h = mrb_hash_new(mrb);
    mrb_iv_set(mrb, k, sym, h);
  }
  return h;
}

static mrb_value gv_slot_get(mrb_state *mrb, int i) {
  if (gv_kinds[i] == 3) {
    return mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_gvget), 1,
                          mrb_symbol_value(mrb_intern_cstr(mrb, gv_names[i])));
  }
  return mrb_hash_get(mrb, gv_storage(mrb), mrb_symbol_value(mrb_intern_cstr(mrb, gv_canon[i])));
}

static void gv_slot_set(mrb_state *mrb, int i, mrb_value v) {
  mrb_value kernel = mrb_obj_value(mrb->kernel_module);
  mrb_value name = mrb_symbol_value(mrb_intern_cstr(mrb, gv_names[i]));
  if (gv_kinds[i] != 2) {
    mrb_sym check = MRB_SYM(__oruby_gvcheck);
    /* until core_ext defines the checks, the startup values are stored as is */
    if (mrb_respond_to(mrb, kernel, check)) v = mrb_funcall_id(mrb, kernel, check, 2, name, v);
  }
  mrb_hash_set(mrb, gv_storage(mrb), mrb_symbol_value(mrb_intern_cstr(mrb, gv_canon[i])), v);
  mrb_value hooks = mrb_iv_get(mrb, kernel, MRB_IVSYM(__oruby_trace_var));
  if (mrb_hash_p(hooks) && mrb_hash_key_p(mrb, hooks, name)) {
    mrb_funcall_id(mrb, kernel, MRB_SYM(__oruby_trace_fire), 2, name, v);
  }
}

#define GV_SLOT(i) \
  static mrb_value gv_get_##i(mrb_state *mrb) { return gv_slot_get(mrb, i); } \
  static void gv_set_##i(mrb_state *mrb, mrb_value v) { gv_slot_set(mrb, i, v); }
GV_SLOT(0) GV_SLOT(1) GV_SLOT(2) GV_SLOT(3) GV_SLOT(4) GV_SLOT(5) GV_SLOT(6) GV_SLOT(7)
GV_SLOT(8) GV_SLOT(9) GV_SLOT(10) GV_SLOT(11) GV_SLOT(12) GV_SLOT(13) GV_SLOT(14) GV_SLOT(15)
GV_SLOT(16) GV_SLOT(17) GV_SLOT(18) GV_SLOT(19) GV_SLOT(20) GV_SLOT(21) GV_SLOT(22) GV_SLOT(23)
GV_SLOT(24) GV_SLOT(25) GV_SLOT(26) GV_SLOT(27) GV_SLOT(28) GV_SLOT(29) GV_SLOT(30) GV_SLOT(31)
GV_SLOT(32) GV_SLOT(33) GV_SLOT(34) GV_SLOT(35) GV_SLOT(36) GV_SLOT(37) GV_SLOT(38) GV_SLOT(39)
GV_SLOT(40) GV_SLOT(41) GV_SLOT(42) GV_SLOT(43) GV_SLOT(44) GV_SLOT(45) GV_SLOT(46) GV_SLOT(47)
GV_SLOT(48) GV_SLOT(49) GV_SLOT(50) GV_SLOT(51) GV_SLOT(52) GV_SLOT(53) GV_SLOT(54) GV_SLOT(55)
GV_SLOT(56) GV_SLOT(57) GV_SLOT(58) GV_SLOT(59) GV_SLOT(60) GV_SLOT(61) GV_SLOT(62) GV_SLOT(63)
GV_SLOT(64) GV_SLOT(65) GV_SLOT(66) GV_SLOT(67) GV_SLOT(68) GV_SLOT(69) GV_SLOT(70) GV_SLOT(71)
GV_SLOT(72) GV_SLOT(73) GV_SLOT(74) GV_SLOT(75) GV_SLOT(76) GV_SLOT(77) GV_SLOT(78) GV_SLOT(79)
GV_SLOT(80) GV_SLOT(81) GV_SLOT(82) GV_SLOT(83) GV_SLOT(84) GV_SLOT(85) GV_SLOT(86) GV_SLOT(87)
GV_SLOT(88) GV_SLOT(89) GV_SLOT(90) GV_SLOT(91) GV_SLOT(92) GV_SLOT(93) GV_SLOT(94) GV_SLOT(95)
GV_SLOT(96) GV_SLOT(97) GV_SLOT(98) GV_SLOT(99) GV_SLOT(100) GV_SLOT(101) GV_SLOT(102) GV_SLOT(103)
GV_SLOT(104) GV_SLOT(105) GV_SLOT(106) GV_SLOT(107) GV_SLOT(108) GV_SLOT(109) GV_SLOT(110) GV_SLOT(111)
GV_SLOT(112) GV_SLOT(113) GV_SLOT(114) GV_SLOT(115) GV_SLOT(116) GV_SLOT(117) GV_SLOT(118) GV_SLOT(119)
GV_SLOT(120) GV_SLOT(121) GV_SLOT(122) GV_SLOT(123) GV_SLOT(124) GV_SLOT(125) GV_SLOT(126) GV_SLOT(127)
static const struct { mrb_value (*get)(mrb_state *); void (*set)(mrb_state *, mrb_value); } gv_fns[GV_SLOTS] = {
  {gv_get_0, gv_set_0},
  {gv_get_1, gv_set_1},
  {gv_get_2, gv_set_2},
  {gv_get_3, gv_set_3},
  {gv_get_4, gv_set_4},
  {gv_get_5, gv_set_5},
  {gv_get_6, gv_set_6},
  {gv_get_7, gv_set_7},
  {gv_get_8, gv_set_8},
  {gv_get_9, gv_set_9},
  {gv_get_10, gv_set_10},
  {gv_get_11, gv_set_11},
  {gv_get_12, gv_set_12},
  {gv_get_13, gv_set_13},
  {gv_get_14, gv_set_14},
  {gv_get_15, gv_set_15},
  {gv_get_16, gv_set_16},
  {gv_get_17, gv_set_17},
  {gv_get_18, gv_set_18},
  {gv_get_19, gv_set_19},
  {gv_get_20, gv_set_20},
  {gv_get_21, gv_set_21},
  {gv_get_22, gv_set_22},
  {gv_get_23, gv_set_23},
  {gv_get_24, gv_set_24},
  {gv_get_25, gv_set_25},
  {gv_get_26, gv_set_26},
  {gv_get_27, gv_set_27},
  {gv_get_28, gv_set_28},
  {gv_get_29, gv_set_29},
  {gv_get_30, gv_set_30},
  {gv_get_31, gv_set_31},
  {gv_get_32, gv_set_32},
  {gv_get_33, gv_set_33},
  {gv_get_34, gv_set_34},
  {gv_get_35, gv_set_35},
  {gv_get_36, gv_set_36},
  {gv_get_37, gv_set_37},
  {gv_get_38, gv_set_38},
  {gv_get_39, gv_set_39},
  {gv_get_40, gv_set_40},
  {gv_get_41, gv_set_41},
  {gv_get_42, gv_set_42},
  {gv_get_43, gv_set_43},
  {gv_get_44, gv_set_44},
  {gv_get_45, gv_set_45},
  {gv_get_46, gv_set_46},
  {gv_get_47, gv_set_47},
  {gv_get_48, gv_set_48},
  {gv_get_49, gv_set_49},
  {gv_get_50, gv_set_50},
  {gv_get_51, gv_set_51},
  {gv_get_52, gv_set_52},
  {gv_get_53, gv_set_53},
  {gv_get_54, gv_set_54},
  {gv_get_55, gv_set_55},
  {gv_get_56, gv_set_56},
  {gv_get_57, gv_set_57},
  {gv_get_58, gv_set_58},
  {gv_get_59, gv_set_59},
  {gv_get_60, gv_set_60},
  {gv_get_61, gv_set_61},
  {gv_get_62, gv_set_62},
  {gv_get_63, gv_set_63},
  {gv_get_64, gv_set_64},
  {gv_get_65, gv_set_65},
  {gv_get_66, gv_set_66},
  {gv_get_67, gv_set_67},
  {gv_get_68, gv_set_68},
  {gv_get_69, gv_set_69},
  {gv_get_70, gv_set_70},
  {gv_get_71, gv_set_71},
  {gv_get_72, gv_set_72},
  {gv_get_73, gv_set_73},
  {gv_get_74, gv_set_74},
  {gv_get_75, gv_set_75},
  {gv_get_76, gv_set_76},
  {gv_get_77, gv_set_77},
  {gv_get_78, gv_set_78},
  {gv_get_79, gv_set_79},
  {gv_get_80, gv_set_80},
  {gv_get_81, gv_set_81},
  {gv_get_82, gv_set_82},
  {gv_get_83, gv_set_83},
  {gv_get_84, gv_set_84},
  {gv_get_85, gv_set_85},
  {gv_get_86, gv_set_86},
  {gv_get_87, gv_set_87},
  {gv_get_88, gv_set_88},
  {gv_get_89, gv_set_89},
  {gv_get_90, gv_set_90},
  {gv_get_91, gv_set_91},
  {gv_get_92, gv_set_92},
  {gv_get_93, gv_set_93},
  {gv_get_94, gv_set_94},
  {gv_get_95, gv_set_95},
  {gv_get_96, gv_set_96},
  {gv_get_97, gv_set_97},
  {gv_get_98, gv_set_98},
  {gv_get_99, gv_set_99},
  {gv_get_100, gv_set_100},
  {gv_get_101, gv_set_101},
  {gv_get_102, gv_set_102},
  {gv_get_103, gv_set_103},
  {gv_get_104, gv_set_104},
  {gv_get_105, gv_set_105},
  {gv_get_106, gv_set_106},
  {gv_get_107, gv_set_107},
  {gv_get_108, gv_set_108},
  {gv_get_109, gv_set_109},
  {gv_get_110, gv_set_110},
  {gv_get_111, gv_set_111},
  {gv_get_112, gv_set_112},
  {gv_get_113, gv_set_113},
  {gv_get_114, gv_set_114},
  {gv_get_115, gv_set_115},
  {gv_get_116, gv_set_116},
  {gv_get_117, gv_set_117},
  {gv_get_118, gv_set_118},
  {gv_get_119, gv_set_119},
  {gv_get_120, gv_set_120},
  {gv_get_121, gv_set_121},
  {gv_get_122, gv_set_122},
  {gv_get_123, gv_set_123},
  {gv_get_124, gv_set_124},
  {gv_get_125, gv_set_125},
  {gv_get_126, gv_set_126},
  {gv_get_127, gv_set_127},
};

// _mrb_gv_slot finds or claims the process-wide slot for name; -1 when the
// slots are used up. It touches only the tables: the caller holds coreMu.
int _mrb_gv_slot(const char *name, const char *canon, int kind) {
  int i;
  for (i = 0; i < gv_n; i++) {
    if (strcmp(gv_names[i], name) == 0) return i;
  }
  if (gv_n == GV_SLOTS) return -1;
  gv_names[i] = strdup(name);
  gv_canon[i] = strdup(canon);
  gv_kinds[i] = kind;
  gv_n++;
  return i;
}

// _mrb_gv_hook_slot makes slot i's global virtual in mrb; its current value
// is kept. It runs Ruby (the global's getter), so no lock is held.
void _mrb_gv_hook_slot(mrb_state *mrb, int i) {
  const char *name = gv_names[i];
  mrb_sym sym = mrb_intern_cstr(mrb, name);
  mrb_value key = mrb_symbol_value(mrb_intern_cstr(mrb, gv_canon[i]));
  mrb_value store = gv_storage(mrb);
  mrb_value cur = mrb_gv_get(mrb, sym);   /* before it turns virtual */
  if (!mrb_hash_key_p(mrb, store, key)) mrb_hash_set(mrb, store, key, cur);
  mrb_gv_define_virtual(mrb, sym, gv_fns[i].get, gv_fns[i].set);
}

// _mrb_gv_hook is _mrb_gv_slot and _mrb_gv_hook_slot together, for state
// init (under coreMu). Returns FALSE when the slots are used up.
mrb_bool _mrb_gv_hook(mrb_state *mrb, const char *name, const char *canon, int kind) {
  int i = _mrb_gv_slot(name, canon, kind);
  if (i < 0) return FALSE;
  _mrb_gv_hook_slot(mrb, i);
  return TRUE;
}

static void gv_init_checked(mrb_state *mrb) {
  static const char *pairs[][2] = {
    {"$/", "$/"}, {"$-0", "$/"}, {"$\\", "$\\"}, {"$,", "$,"}, {"$;", "$;"}, {"$-F", "$;"},
    {"$.", "$."}, {"$stdout", "$stdout"}, {"$>", "$stdout"}, {"$stderr", "$stderr"},
    {"$VERBOSE", "$VERBOSE"}, {"$-v", "$VERBOSE"}, {"$-w", "$VERBOSE"},
    {"$0", "$0"}, {"$PROGRAM_NAME", "$0"}, {"$~", "$~"}, {NULL, NULL}
  };
  for (int i = 0; pairs[i][0]; i++) _mrb_gv_hook(mrb, pairs[i][0], pairs[i][1], 1);
  _mrb_gv_hook(mrb, "$@", "$@", 3);
}

void _orb_bt_init(mrb_state *mrb);

static mrb_value sym_all_symbols_m(mrb_state *mrb, mrb_value self) {
  mrb_sym presym_max = mrb_presym_max();
  mrb_value ary = mrb_ary_new_capa(mrb, presym_max + mrb->symidx);
  for (mrb_sym i = 1; i <= presym_max; i++) {
    mrb_ary_push(mrb, ary, mrb_symbol_value(i));
  }
  for (mrb_sym i = 1; i <= mrb->symidx; i++) {
    mrb_int len;
    if (mrb_sym_name_len(mrb, i + presym_max, &len) != NULL) {
      mrb_ary_push(mrb, ary, mrb_symbol_value(i + presym_max));
    }
  }
  return ary;
}

static mrb_value complex_oruby_new_m(mrb_state *mrb, mrb_value self) {
  mrb_value r, i;
  (void)self;
  mrb_get_args(mrb, "oo", &r, &i);
  return mrb_complex_new_value(mrb, r, i);
}

/* runs under coreMu (mruby.go): it fills the process-wide tables */
void _mrb_init_oruby_core(mrb_state *mrb) {
  _mrb_wrap_string_encoding(mrb);
  gv_init_checked(mrb);
  wrap_cfunc(mrb, mrb->kernel_module, "respond_to?", respond_to_w, &respond_to_orig);
  wrap_eval_methods(mrb);
  {
    /* redefined rather than wrapped: the arity check must admit 3 */
    struct RClass *ksing = mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(mrb->kernel_module)));
    struct RClass *targets[2] = {mrb->kernel_module, ksing};
    mrb_func_t ws[2] = {raise_w0, raise_w1};
    for (int i = 0; i < 2; i++) {
      struct RClass *owner = targets[i];
      mrb_method_t m = mrb_method_search_vm(mrb, &owner, MRB_SYM(raise));
      if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || MRB_METHOD_CFUNC(m) == ws[i]) continue;
      raise_orig[i] = MRB_METHOD_CFUNC(m);
      if (i == 0) mrb_define_private_method(mrb, owner, "raise", ws[i], MRB_ARGS_OPT(3) | MRB_ARGS_KEY(1, 1));
      else mrb_define_method(mrb, owner, "raise", ws[i], MRB_ARGS_OPT(3) | MRB_ARGS_KEY(1, 1));
      mrb_alias_method(mrb, owner, MRB_SYM(fail), MRB_SYM(raise));
    }
  }
  mrb_define_method(mrb, mrb->eException_class, "set_backtrace", exc_set_backtrace_m, MRB_ARGS_REQ(1));
  _orb_bt_init(mrb);
  mrb_define_class_method(mrb, mrb->symbol_class, "all_symbols", sym_all_symbols_m, MRB_ARGS_NONE());
  if (mrb_class_defined(mrb, "Complex")) {
    mrb_define_class_method(mrb, mrb_class_get(mrb, "Complex"), "__oruby_new__", complex_oruby_new_m, MRB_ARGS_REQ(2));
  }
  mrb_define_private_method(mrb, mrb->module_class, "__alias_from_object", mod_alias_from_object, MRB_ARGS_REQ(2));
  mrb_define_private_method(mrb, mrb->string_class, "__encoding_binary?", str_enc_binary_p, MRB_ARGS_NONE());
  mrb_define_private_method(mrb, mrb->string_class, "__encoding_tag", str_enc_tag, MRB_ARGS_NONE());
  mrb_define_private_method(mrb, mrb->string_class, "__encoding_tag=", str_enc_tag_set, MRB_ARGS_REQ(1));

  mrb_func_t wrappers[] = {vis_public, vis_private, vis_protected, vis_module_function};
  for (int i = 0; i < 4; i++) {
    struct RClass *c = mrb->module_class;
    mrb_method_t m = mrb_method_search_vm(mrb, &c, mrb_intern_cstr(mrb, vis_names[i]));
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || MRB_METHOD_CFUNC(m) == wrappers[i]) continue;
    vis_orig[i] = MRB_METHOD_CFUNC(m);
    mrb_method_t w = m;  /* keeps the original's flags, aspec and visibility */
    w.as.func = wrappers[i];
    mrb_define_method_raw(mrb, c, mrb_intern_cstr(mrb, vis_names[i]), w);
  }
}

// Make c's own method mid private (the instance half of a module function).
void _mrb_make_method_private(mrb_state *mrb, struct RClass *c, mrb_sym mid) {
  struct RClass *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
  if (MRB_METHOD_UNDEF_P(m) || owner != c) return;
  MRB_METHOD_SET_VISIBILITY(m, MRB_METHOD_PRIVATE_FL);
  mrb_define_method_raw(mrb, c, mid, m);
}
