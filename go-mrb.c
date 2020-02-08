#include <stdio.h>
#include "go-mrb.h"

// Go-C-Go proxy callback functions
// ret values are alocated on C side

mrb_value set_mrb_callback(mrb_state *mrb, mrb_value self) {
  mrb_value ret = mrb_nil_value();
  go_mrb_func_callback(mrb, &self, &ret);

  if ((mrb_type(ret) == MRB_TT_EXCEPTION) && (mrb_obj_ptr(ret) == mrb->exc)) {
    mrb_exc_raise(mrb, ret);
  }

  return ret;
}

mrb_value set_mrb_env_callback(mrb_state *mrb, mrb_value self) {
  mrb_value idx = mrb_proc_cfunc_env_get(mrb, 0);
  mrb_value ret = go_mrb_func_env_callback(mrb, self, mrb_fixnum(idx));

  if ((mrb_type(ret) == MRB_TT_EXCEPTION) && (mrb_obj_ptr(ret) == mrb->exc)) {
    mrb_exc_raise(mrb, ret);
  }

  return ret;
}

mrb_value set_mrb_proc_callback(mrb_state *mrb, mrb_value self) {
  mrb_value ret = mrb_nil_value();
  go_mrb_proc_callback(mrb, &self, &ret);

  if ((mrb_type(ret) == MRB_TT_EXCEPTION) && (mrb_obj_ptr(ret) == mrb->exc)) {
    mrb_exc_raise(mrb, ret);
  }

  return ret;
}

mrb_value set_gofunc_callback(mrb_state *mrb, mrb_value self) {
  mrb_value ret;
  go_gofunc_callback(mrb, &self, &ret);

  if ((mrb_type(ret) == MRB_TT_EXCEPTION) && (mrb_obj_ptr(ret) == mrb->exc)) {
    mrb_exc_raise(mrb, ret);
  }

  return ret;
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

  MRB_METHOD_FROM_PROC(m, proc);
  if (aspec == MRB_ARGS_NONE()) {
    MRB_METHOD_NOARG_SET(m);
  }

  mrb_define_method_raw(mrb, c, id, m);

  mrb_gc_arena_restore(mrb, ai);
}

void _mrb_method_new_cfunc(mrb_state *mrb, struct RClass *c, mrb_sym id, int idx, mrb_aspec aspec) {
  mrb_method_t m;
  int ai = mrb_gc_arena_save(mrb);    
  mrb_value at = mrb_fixnum_value(idx);
  struct RProc *proc = mrb_proc_new_cfunc_with_env(mrb, set_mrb_env_callback, 1, &at);

  MRB_METHOD_FROM_PROC(m, proc);
  if (aspec == MRB_ARGS_NONE()) {
    MRB_METHOD_NOARG_SET(m);
  }

  mrb_define_method_raw(mrb, c, id, m);

  mrb_gc_arena_restore(mrb, ai);
}
