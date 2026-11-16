#include <stdio.h>
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

void _mrb_init_oruby_core(mrb_state *mrb) {
  mrb_define_private_method(mrb, mrb->module_class, "__alias_from_object", mod_alias_from_object, MRB_ARGS_REQ(2));

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
