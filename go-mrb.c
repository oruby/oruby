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

// Module#private, #public, #protected and #module_function in Ruby accept
// String method names; mruby's accept Symbols only. These wrappers convert
// String arguments to Symbols in place and call the original C function in
// the same callinfo, so the no-argument form still changes the caller's
// default visibility.
static const char *vis_names[] = {"private", "public", "protected", "module_function"};
static mrb_func_t vis_orig[4];

static mrb_value vis_call(mrb_state *mrb, mrb_value self, int which) {
  mrb_int argc = mrb_get_argc(mrb);
  mrb_value *argv = (mrb_value *)mrb_get_argv(mrb);
  for (mrb_int i = 0; i < argc; i++) {
    if (mrb_string_p(argv[i])) {
      argv[i] = mrb_symbol_value(mrb_intern_str(mrb, argv[i]));
    }
  }
  return vis_orig[which](mrb, self);
}

static mrb_value vis_private(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 0); }
static mrb_value vis_public(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 1); }
static mrb_value vis_protected(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 2); }
static mrb_value vis_module_function(mrb_state *mrb, mrb_value self) { return vis_call(mrb, self, 3); }

void _mrb_patch_visibility(mrb_state *mrb) {
  mrb_func_t wrappers[] = {vis_private, vis_public, vis_protected, vis_module_function};
  for (int i = 0; i < 4; i++) {
    struct RClass *c = mrb->module_class;
    mrb_method_t m = mrb_method_search_vm(mrb, &c, mrb_intern_cstr(mrb, vis_names[i]));
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m)) {
      continue;
    }
    mrb_func_t orig = MRB_METHOD_CFUNC(m);
    if (orig == wrappers[i]) {
      continue;
    }
    vis_orig[i] = orig;
    mrb_define_method(mrb, mrb->module_class, vis_names[i], wrappers[i], MRB_ARGS_ANY());
  }
}

// defined?(X) and defined?(A::B) compile to Kernel#__defined_const? and
// #__defined_const_path?. In Ruby a constant with a pending autoload counts
// as defined. gem/load keeps autoloads in $AUTOLOADS ({module => {sym =>
// path}}), so these wrappers consult it when the original helper (called in
// the same callinfo, keeping its lexical lookup) reports "not defined".
static mrb_func_t defined_const_orig, defined_const_path_orig;

static mrb_bool autoload_pending(mrb_state *mrb, mrb_value mod, mrb_sym name) {
  mrb_value reg = mrb_gv_get(mrb, mrb_intern_lit(mrb, "$AUTOLOADS"));
  if (!mrb_hash_p(reg)) return FALSE;
  enum mrb_vtype t = mrb_type(mod);
  if (t != MRB_TT_CLASS && t != MRB_TT_MODULE && t != MRB_TT_SCLASS) return FALSE;
  mrb_value entries = mrb_hash_get(mrb, reg, mod);
  if (!mrb_hash_p(entries)) return FALSE;
  return !mrb_nil_p(mrb_hash_get(mrb, entries, mrb_symbol_value(name)));
}

static mrb_value defined_const_wrap(mrb_state *mrb, mrb_value self) {
  mrb_value r = defined_const_orig(mrb, self);
  if (!mrb_nil_p(r)) return r;
  mrb_sym sym;
  mrb_get_args(mrb, "n", &sym);
  if (autoload_pending(mrb, self, sym) ||
      autoload_pending(mrb, mrb_obj_value(mrb->object_class), sym)) {
    return mrb_str_new_lit(mrb, "constant");
  }
  return r;
}

static mrb_value defined_const_path_wrap(mrb_state *mrb, mrb_value self) {
  mrb_value r = defined_const_path_orig(mrb, self);
  if (!mrb_nil_p(r)) return r;
  mrb_sym parent, child;
  mrb_get_args(mrb, "nn", &parent, &child);
  mrb_value obj = mrb_obj_value(mrb->object_class);
  if (!mrb_const_defined(mrb, obj, parent)) return r;
  mrb_value pv = mrb_const_get(mrb, obj, parent);
  if (autoload_pending(mrb, pv, child)) return mrb_str_new_lit(mrb, "constant");
  return r;
}

void _mrb_patch_defined(mrb_state *mrb) {
  struct {
    const char *name;
    mrb_func_t *orig;
    mrb_func_t wrap;
  } patches[] = {
    {"__defined_const?", &defined_const_orig, defined_const_wrap},
    {"__defined_const_path?", &defined_const_path_orig, defined_const_path_wrap},
  };
  for (int i = 0; i < 2; i++) {
    struct RClass *c = mrb->kernel_module;
    mrb_method_t m = mrb_method_search_vm(mrb, &c, mrb_intern_cstr(mrb, patches[i].name));
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m)) continue;
    mrb_func_t orig = MRB_METHOD_CFUNC(m);
    if (orig == patches[i].wrap) continue;
    *patches[i].orig = orig;
    mrb_define_method(mrb, mrb->kernel_module, patches[i].name, patches[i].wrap, MRB_ARGS_ANY());
  }
}
