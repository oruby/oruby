#include <stdio.h>
#include <string.h>
#include "go-mrb.h"
#include <mruby/proc.h>
#include <mruby/opcode.h>
#include "oruby-vm-compat.h"
#include <mruby/debug.h>
#include <mruby/internal.h>

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

/* public/private/protected with names, as MRI's rb_export_method: an own
   method (of the origin, when the class is prepended) changes in place; an
   inherited one already of that visibility is left alone; another is
   copied into the class with it, and method_added hears of the copy. FALSE:
   a name is not a method here, for mruby's own to raise about. */
static mrb_bool vis_export(mrb_state *mrb, mrb_value self, int which) {
  static const unsigned int vis_fl[3] = {MRB_METHOD_PUBLIC_FL, MRB_METHOD_PRIVATE_FL, MRB_METHOD_PROTECTED_FL};
  if (which > 2 || (!mrb_class_p(self) && !mrb_module_p(self))) return FALSE;
  struct RClass *c = mrb_class_ptr(self), *home = c;
  if (c->flags & MRB_FL_CLASS_IS_PREPENDED) {
    home = c->super;
    while (home && !(home->flags & MRB_FL_CLASS_IS_ORIGIN)) home = home->super;
    if (!home) home = c;
  }
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  mrb_value names = mrb_ary_new(mrb);
  for (mrb_int i = 0; i < argc; i++) {
    if (mrb_array_p(argv[i])) mrb_ary_concat(mrb, names, argv[i]);
    else mrb_ary_push(mrb, names, argv[i]);
  }
  /* every name a method first: else mruby's raises its NameError */
  for (mrb_int i = 0; i < RARRAY_LEN(names); i++) {
    mrb_value n = RARRAY_PTR(names)[i];
    if (!mrb_symbol_p(n) && !mrb_string_p(n)) return FALSE;
    mrb_sym mid = mrb_symbol_p(n) ? mrb_symbol(n) : mrb_intern_str(mrb, n);
    struct RClass *o = home;
    mrb_method_t m = mrb_method_search_vm(mrb, &o, mid);
    if (MRB_METHOD_UNDEF_P(m)) return FALSE;
  }
  if (mrb_frozen_p(mrb_basic_ptr(self))) return FALSE;
  for (mrb_int i = 0; i < RARRAY_LEN(names); i++) {
    mrb_value n = RARRAY_PTR(names)[i];
    mrb_sym mid = mrb_symbol_p(n) ? mrb_symbol(n) : mrb_intern_str(mrb, n);
    struct RClass *o = home;
    mrb_method_t m = mrb_method_search_vm(mrb, &o, mid);
    if (o == home || (o->tt == MRB_TT_ICLASS && o == home)) {
      MRB_METHOD_SET_VISIBILITY(m, vis_fl[which]);
      mrb_define_method_raw(mrb, home, mid, m);
      continue;
    }
    if ((unsigned int)MRB_METHOD_VISIBILITY(m) == vis_fl[which]) continue;
    if (MRB_METHOD_PROC_P(m) && !MRB_PROC_CFUNC_P(MRB_METHOD_PROC(m)) && !MRB_PROC_ALIAS_P(MRB_METHOD_PROC(m))) {
      /* MRI's ZSUPER entry: the method of where it was found, so that a
         `super` in it goes on from there (an alias proc keeps the owner) */
      struct RProc *pnew = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb->proc_class);
      pnew->body.mid = mid;
      pnew->upper = MRB_METHOD_PROC(m);
      pnew->e.target_class = o;
      pnew->flags |= MRB_PROC_ALIAS;
      MRB_METHOD_FROM_PROC(m, pnew);
    }
    MRB_METHOD_SET_VISIBILITY(m, vis_fl[which]);
    mrb_define_method_raw(mrb, c, mid, m);
    mrb_method_added(mrb, c, mid);
  }
  return TRUE;
}

static mrb_value vis_call(mrb_state *mrb, mrb_value self, int which) {
  if (which <= 2 && mrb_get_argc(mrb) > 0 && vis_export(mrb, self, which)) {
    mrb_int argc = mrb_get_argc(mrb);
    const mrb_value *argv = mrb_get_argv(mrb);
    /* MRI returns its argument (an Array of them for several) */
    return argc == 1 ? argv[0] : mrb_ary_new_from_values(mrb, argc, argv);
  }
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
/* MRI's private-by-default names (rb_method_entry_make's list) */
static mrb_bool special_private_name(mrb_sym mid) {
  return mid == MRB_SYM(initialize) || mid == MRB_SYM(initialize_copy) || mid == MRB_SYM(initialize_clone) ||
         mid == MRB_SYM(initialize_dup) || mid == MRB_SYM_Q(respond_to_missing);
}

static void set_own_visibility(mrb_state *mrb, struct RClass *c, mrb_sym mid, unsigned int vis) {
  struct RClass *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
  if (MRB_METHOD_UNDEF_P(m) || owner != c) return;
  MRB_METHOD_SET_VISIBILITY(m, vis);
  mrb_define_method_raw(mrb, c, mid, m);
}

/* module_function of initialize and the like: the module's own copy is
   public, as MRI's (mruby keeps them private) */
static mrb_value vis_module_function(mrb_state *mrb, mrb_value self) {
  mrb_value r = vis_call(mrb, self, 3);
  if (mrb_type(self) == MRB_TT_MODULE) {
    mrb_int argc = mrb_get_argc(mrb);
    const mrb_value *argv = mrb_get_argv(mrb);
    struct RClass *sc = mrb_class_ptr(mrb_singleton_class(mrb, self));
    for (mrb_int i = 0; i < argc; i++) {
      mrb_value n = argv[i];
      mrb_sym mid = mrb_symbol_p(n) ? mrb_symbol(n) : mrb_string_p(n) ? mrb_intern_str(mrb, n) : 0;
      if (mid && special_private_name(mid)) set_own_visibility(mrb, sc, mid, MRB_METHOD_PUBLIC_FL);
    }
  }
  return r;
}

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

void _orb_keep_source(mrb_state *mrb, mrb_value file, mrb_int line, mrb_value src);
void _orb_trace_script(mrb_state *mrb, mrb_value src);

/* MRI names the code eval runs without a file "(eval at FILE:LINE)", the
   Ruby code calling it */
static mrb_value eval_default_file(mrb_state *mrb) {
  mrb_callinfo *ci = mrb->c->ci;
  for (mrb_callinfo *p = ci - 1; p >= mrb->c->cibase; p--) {
    const struct RProc *pr = p->proc;
    if (!pr || MRB_PROC_CFUNC_P(pr) || !pr->body.irep || !p->pc) continue;
    uint32_t off = (uint32_t)(p->pc - pr->body.irep->iseq);
    if (off > 0) off--;
    int32_t line = 0;
    const char *file = NULL;
    if (!mrb_debug_get_position(mrb, pr->body.irep, off, &line, &file) || !file) continue;
    if (strncmp(file, "mrblib/", 7) == 0 || strncmp(file, "mrbgems/", 8) == 0 ||
        strncmp(file, "<internal:", 10) == 0) continue;
    return mrb_format(mrb, "(eval at %s:%d)", file, (int)line);
  }
  return mrb_nil_value();
}

/* `it` as a word: a block's implicit parameter (prism.go's hasBareIt) */
static mrb_bool src_bare_it(const char *s, mrb_int len) {
#define IDENT_C(c) ((c) == '_' || ((c) >= '0' && (c) <= '9') || ((c) >= 'a' && (c) <= 'z') || ((c) >= 'A' && (c) <= 'Z') || (unsigned char)(c) >= 0x80)
  for (mrb_int i = 0; i + 1 < len; i++) {
    if (s[i] == 'i' && s[i + 1] == 't' && (i == 0 || !IDENT_C(s[i - 1])) && (i + 2 == len || !IDENT_C(s[i + 2]))) return TRUE;
  }
  return FALSE;
#undef IDENT_C
}

/* MRI's implicit conversion (00_convert.rb's Kernel.__implicit and its
   messages) of argv[i] into klass, in place */
static void eval_arg_implicit(mrb_state *mrb, mrb_value *argv, mrb_int i, struct RClass *klass) {
  mrb_value cv[2] = {argv[i], mrb_obj_value(klass)};
  argv[i] = mrb_funcall_argv(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__implicit), 2, cv);
}

/* instance_eval / class_eval / module_eval of a string, as MRI's: at most 3
   arguments, the source and file by #to_str, the line by #to_int */
static void eval_string_args(mrb_state *mrb, int which) {
  mrb_callinfo *ci = mrb->c->ci;
  mrb_int argc = mrb_get_argc(mrb);
  if (ci->n >= 15 || ci->kw || !mrb_nil_p(ci->stack[argc + 1])) return; /* a block, or packed */
  if (argc > 3) mrb_raisef(mrb, E_ARGUMENT_ERROR, "wrong number of arguments (given %i, expected 1..3)", argc);
  mrb_value *argv = (mrb_value *)mrb_get_argv(mrb);
  if (argc > 0 && !mrb_string_p(argv[0])) eval_arg_implicit(mrb, argv, 0, mrb->string_class);
  if (argc > 1 && !mrb_string_p(argv[1])) eval_arg_implicit(mrb, argv, 1, mrb->string_class);
  if (argc > 2 && !mrb_integer_p(argv[2])) eval_arg_implicit(mrb, argv, 2, mrb->integer_class);
  (void)which;
}

static mrb_value eval_call(mrb_state *mrb, mrb_value self, int which) {
  if (which >= 2 && which <= 4) eval_string_args(mrb, which);
  else if (mrb_get_argc(mrb) > 0 && !mrb_string_p(mrb_get_argv(mrb)[0])) {
    /* eval(obj): the source by #to_str */
    eval_arg_implicit(mrb, (mrb_value *)mrb_get_argv(mrb), 0, mrb->string_class);
  }
  mrb_int argc = mrb_get_argc(mrb);
  if (argc > 0) {
    mrb_value *argv = (mrb_value *)mrb_get_argv(mrb);
    if (mrb_string_p(argv[0])) {
      /* eval(src, binding, file, line), the others (src, file, line) */
      mrb_int fi = which <= 1 ? 2 : 1;
      mrb_callinfo *ci = mrb->c->ci;
      mrb_value deffile = mrb_nil_value();
      /* not with a block, whose arity error counts the arguments given */
      if (argc <= fi && ci->n < 15 && !ci->kw && mrb_nil_p(ci->stack[argc + 1])) deffile = eval_default_file(mrb);
      /* kept for #source_range (go-srcrange.c) */
      mrb_value file = argc > fi ? argv[fi] : deffile;
      mrb_int line = (argc > fi + 1 && mrb_integer_p(argv[fi + 1])) ? mrb_integer(argv[fi + 1]) : 1;
      _orb_keep_source(mrb, file, line, argv[0]);
      _orb_trace_script(mrb, argv[0]);
      /* coverage set up (gem/coverage): the code with its counters */
      if (mrb_string_p(file) && mrb_class_defined_id(mrb, MRB_SYM(Coverage))) {
        mrb_value cov = mrb_obj_value(mrb_module_get_id(mrb, MRB_SYM(Coverage)));
        if (mrb_respond_to(mrb, cov, MRB_SYM(__oruby_eval))) {
          mrb_value cv = mrb_funcall_id(mrb, cov, MRB_SYM(__oruby_eval), 3, argv[0], file, mrb_int_value(mrb, line));
          if (mrb_string_p(cv)) argv[0] = cv;
        }
      }
      const char *s = RSTRING_PTR(argv[0]);
      mrb_int len = RSTRING_LEN(argv[0]);
      if (!mrb_nil_p(str_enc_tag(mrb, argv[0])) ||
          src_contains(s, len, "alias") || src_contains(s, len, "undef") ||
          src_contains(s, len, "**") || src_contains(s, len, "=>") ||
          src_contains(s, len, ": ") || src_contains(s, len, "..") ||
          src_contains(s, len, "&.") || src_contains(s, len, "BEGIN") ||
          src_contains(s, len, "END") || src_contains(s, len, "__ENCODING__") ||
          src_contains(s, len, "frozen_string_literal") ||
          src_contains(s, len, "||=") || src_contains(s, len, "&&=") ||
          src_contains(s, len, "::") || src_contains(s, len, "$") || src_contains(s, len, "|") || src_contains(s, len, "->") ||
          /* an anonymous *, & argument (prism.go ORB_ANON_ARG) */
          src_contains(s, len, "*)") || src_contains(s, len, "*,") || src_contains(s, len, "*]") ||
          src_contains(s, len, "&)") ||
          /* parse warnings (ORB_PARSE_WARN) */
          src_contains(s, len, "when") || src_contains(s, len, "defined?") ||
          /* an interpolated symbol (ORB_DSYM) */
          src_contains(s, len, ":\"") || src_contains(s, len, "%I") ||
          /* static POSIX class ranges (ORB_REGEXP_RANGE) */
          src_contains(s, len, "[:") ||
          /* interpolated regexp encodings and /o literals in eval */
          ((src_contains(s, len, "o") || src_contains(s, len, "#")) &&
           (src_contains(s, len, "/") || src_contains(s, len, "%r"))) ||
          (src_contains(s, len, ",") && src_contains(s, len, "=") &&
           (src_contains(s, len, ".") || src_contains(s, len, "["))) ||
          src_contains(s, len, "freeze") ||
          src_bare_it(s, len) || len > 255) {
        mrb_value rewritten = mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__preprocess_ruby), 1, argv[0]);
        argv = (mrb_value *)mrb_get_argv(mrb);
        argv[0] = rewritten;
        ci = mrb->c->ci;
      }
      if (!mrb_nil_p(deffile)) {
        /* the file argument added: the arguments are passed packed (n = 15:
           an Array at stack[1], the block at stack[2]), which needs no more
           stack */
        mrb_value args[4];
        mrb_int n = 0;
        for (mrb_int i = 0; i < argc && n < 4; i++) args[n++] = argv[i];
        while (n < fi) args[n++] = mrb_nil_value();
        args[n++] = deffile;
        args[n++] = mrb_fixnum_value(1);
        mrb_value blk = ci->stack[argc + 1];
        ci->stack[1] = mrb_ary_new_from_values(mrb, n, args);
        ci->stack[2] = blk;
        ci->n = 15;
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

static void wrap_cfunc_id(mrb_state *mrb, struct RClass *c, mrb_sym mid, mrb_func_t wrapper, mrb_func_t *orig) {
  struct RClass *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
  if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || MRB_METHOD_CFUNC(m) == wrapper) return;
  *orig = MRB_METHOD_CFUNC(m);
  mrb_method_t w = m;  /* keeps the original's flags, aspec and visibility */
  w.as.func = wrapper;
  mrb_define_method_raw(mrb, owner, mid, w);
}

static void wrap_cfunc(mrb_state *mrb, struct RClass *c, const char *name, mrb_func_t wrapper, mrb_func_t *orig) {
  wrap_cfunc_id(mrb, c, mrb_intern_cstr(mrb, name), wrapper, orig);
}

static void wrap_eval_methods(mrb_state *mrb) {
  mrb_func_t w[EVAL_WRAPPED] = {eval_w0, eval_w1, eval_w2, eval_w3, eval_w4, eval_w5};
  struct RClass *basic = mrb_class_get_id(mrb, MRB_SYM(BasicObject));
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
      if (owner && owner->tt == MRB_TT_ICLASS) owner = owner->c;  /* Kernel as included */
      if (!MRB_METHOD_UNDEF_P(m) && (owner == mrb->kernel_module ||
          owner == mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(mrb->kernel_module))) ||
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

static mrb_func_t str_replace_orig;
static mrb_value str_replace_w(mrb_state *mrb, mrb_value self) {
  mrb_value src = mrb_get_arg1(mrb);
  mrb_value r = str_replace_orig(mrb, self);
  if (!mrb_obj_eq(mrb, src, self)) {
    mrb_sym sym = MRB_SYM(__encoding);
    mrb_value tag = mrb_iv_get(mrb, src, sym);
    if (mrb_nil_p(tag)) {
      if (mrb_iv_defined(mrb, self, sym)) mrb_iv_remove(mrb, self, sym);
    }
    else {
      mrb_iv_set(mrb, self, sym, tag);
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

// String methods that check their String arguments' encodings against the
// receiver's as MRI's rb_enc_check does: Encoding::CompatibilityError (or
// nil for casecmp), and center/ljust/rjust/insert give their result the
// compatible encoding. Untagged strings of one kind need no check.
enum { ENC_CHK_RAISE, ENC_CHK_NIL, ENC_CHK_RESULT, ENC_CHK_SELF };
static const struct { const char *name; int arg; int mode; } enc_chk_defs[] = {
  {"index", 0, ENC_CHK_RAISE}, {"rindex", 0, ENC_CHK_RAISE},
  {"byteindex", 0, ENC_CHK_RAISE}, {"byterindex", 0, ENC_CHK_RAISE},
  {"include?", 0, ENC_CHK_RAISE}, {"start_with?", -1, ENC_CHK_RAISE}, {"end_with?", -1, ENC_CHK_RAISE},
  {"casecmp", 0, ENC_CHK_NIL}, {"casecmp?", 0, ENC_CHK_NIL},
  {"center", 1, ENC_CHK_RESULT}, {"ljust", 1, ENC_CHK_RESULT}, {"rjust", 1, ENC_CHK_RESULT},
  {"insert", 1, ENC_CHK_SELF},
};
#define ENC_CHK_N (sizeof(enc_chk_defs) / sizeof(enc_chk_defs[0]))
static mrb_func_t enc_chk_orig[ENC_CHK_N];

static mrb_value enc_chk_call(mrb_state *mrb, mrb_value self, int i) {
  mrb_int argc = mrb_get_argc(mrb);
  int arg = enc_chk_defs[i].arg, mode = enc_chk_defs[i].mode;
  mrb_value enc = mrb_nil_value();
  for (mrb_int k = arg < 0 ? 0 : arg; k < argc && (arg < 0 || k == arg); k++) {
    mrb_value a = mrb_get_argv(mrb)[k];  /* the stack may move under a call */
    if (!mrb_string_p(a) || enc_plain_same(mrb, self, a)) continue;
    if (mode == ENC_CHK_NIL) {
      mrb_value e = mrb_funcall_id(mrb, mrb_obj_value(mrb_class_get_id(mrb, MRB_SYM(Encoding))), MRB_SYM_Q(compatible), 2, self, a);
      if (mrb_nil_p(e)) return mrb_nil_value();
    }
    else {
      enc = mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_enc_concat), 2, self, a);
    }
  }
  mrb_value r = enc_chk_orig[i](mrb, self);
  if (!mrb_nil_p(enc)) {
    mrb_value t = mode == ENC_CHK_RESULT ? r : mode == ENC_CHK_SELF ? self : mrb_nil_value();
    if (mrb_string_p(t)) mrb_funcall_id(mrb, t, MRB_SYM(force_encoding), 1, enc);
  }
  return r;
}
#define ENC_CHK_W(i) static mrb_value enc_chk_w##i(mrb_state *mrb, mrb_value self) { return enc_chk_call(mrb, self, i); }
ENC_CHK_W(0) ENC_CHK_W(1) ENC_CHK_W(2) ENC_CHK_W(3) ENC_CHK_W(4) ENC_CHK_W(5) ENC_CHK_W(6) ENC_CHK_W(7)
ENC_CHK_W(8) ENC_CHK_W(9) ENC_CHK_W(10) ENC_CHK_W(11) ENC_CHK_W(12) ENC_CHK_W(13) ENC_CHK_W(14) ENC_CHK_W(15)
static const mrb_func_t enc_chk_ws[] = {
  enc_chk_w0, enc_chk_w1, enc_chk_w2, enc_chk_w3, enc_chk_w4, enc_chk_w5, enc_chk_w6, enc_chk_w7,
  enc_chk_w8, enc_chk_w9, enc_chk_w10, enc_chk_w11, enc_chk_w12, enc_chk_w13, enc_chk_w14, enc_chk_w15,
};

static void wrap_string_enc_checks(mrb_state *mrb) {
  mrb_static_assert(ENC_CHK_N <= sizeof(enc_chk_ws) / sizeof(enc_chk_ws[0]), "enc_chk_ws too short");
  for (size_t i = 0; i < ENC_CHK_N; i++) {
    mrb_func_t orig = NULL;
    wrap_cfunc(mrb, mrb->string_class, enc_chk_defs[i].name, enc_chk_ws[i], &orig);
    if (orig) enc_chk_orig[i] = orig;
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
    "sub", "gsub", "insert", NULL
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
  wrap_string_enc_checks(mrb);
  wrap_cfunc(mrb, mrb->string_class, "replace", str_replace_w, &str_replace_orig);
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

/* the values a checked global takes as they are, without
   Kernel.__oruby_gvcheck (every gets sets $.) */
static mrb_bool gv_plain_value(mrb_state *mrb, const char *canon, mrb_value v) {
  if (strcmp(canon, "$.") == 0) return mrb_integer_p(v);
  if (strcmp(canon, "$VERBOSE") == 0) return mrb_nil_p(v) || mrb_true_p(v) || mrb_false_p(v);
  if (strcmp(canon, "$0") == 0) return mrb_string_p(v);
  if (strcmp(canon, "$\\") == 0 || strcmp(canon, "$,") == 0 || strcmp(canon, "$;") == 0) return mrb_nil_p(v);
  return FALSE;
}

static void gv_slot_set(mrb_state *mrb, int i, mrb_value v) {
  mrb_value kernel = mrb_obj_value(mrb->kernel_module);
  mrb_value name = mrb_symbol_value(mrb_intern_cstr(mrb, gv_names[i]));
  if (gv_kinds[i] != 2 && !gv_plain_value(mrb, gv_canon[i], v)) {
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
  /* frame-local, not slots (svar_get below); trace_var leaves them alone */
  if (strcmp(name, "$~") == 0 || strcmp(name, "$_") == 0) return -1;
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

/* ---- $~ and $_: frame-local, as MRI's svar ----
   mruby keeps them per scope (mrb_vm_svar_get/set, vm.c): a C frame reads
   and writes through to the Ruby frame below it, a block shares its
   method's, and a scope that returned keeps them in its env. A frame of
   core Ruby code (core_ext's <internal:...> files, mrblib, mrbgems) stands
   for one of MRI's C methods, so it is passed through too: `gets` written in
   Ruby sets its caller's $_, `Symbol#=~` its caller's $~. */
#define IREP_SVAR_CHECKED 0x40 /* irep->flags bits mruby leaves unused */
#define IREP_SVAR_CORE 0x80

static mrb_bool irep_core_p(mrb_state *mrb, const mrb_irep *irep) {
  /* compiled into libmruby (mrblib, mrbgems), and read-only memory */
  if (irep->flags & MRB_IREP_NO_FREE) return TRUE;
  if (irep->flags & IREP_SVAR_CHECKED) return (irep->flags & IREP_SVAR_CORE) != 0;
  const char *f = mrb_debug_get_filename(mrb, irep, 0);
  mrb_bool core = f && (strncmp(f, "<internal:", 10) == 0 || strncmp(f, "mrblib/", 7) == 0 ||
                        strncmp(f, "mrbgems/", 8) == 0);
  ((mrb_irep *)irep)->flags |= IREP_SVAR_CHECKED | (core ? IREP_SVAR_CORE : 0);
  return core;
}

/* the env of the scope a block's locals resolve to (vm.c's svar_scope_env) */
static struct REnv *svar_home_env(const struct RProc *p) {
  struct REnv *e = MRB_PROC_ENV(p);
  if (!e) return NULL;
  for (;;) {
    const struct RProc *up = p->upper;
    if (!up || MRB_PROC_CFUNC_P(up) || (MRB_PROC_SCOPE_P(up) && !MRB_PROC_ENV_P(up))) return e;
    struct REnv *upenv = MRB_PROC_ENV(up);
    if (!upenv) return e;
    p = up;
    e = upenv;
  }
}

/* a context whose body block has the root slot: a thread's, or a fiber that
   Fiber.new made of a user block (29_fiber.rb marks it); not one of core
   code's own, such as Enumerator#next's, whose blocks share their scope's */
static mrb_bool svar_root_context_p(mrb_state *mrb, struct mrb_context *c) {
  if (c == mrb->root_c) return TRUE;
  if (!c->fib) return FALSE;
  return mrb_test(mrb_iv_get(mrb, mrb_obj_value(c->fib), MRB_IVSYM(__oruby_svar_root)));
}

/* Kernel.__oruby_core_proc?(pr): whether pr is a block of core code */
static mrb_value core_proc_p_m(mrb_state *mrb, mrb_value self) {
  mrb_value pr = mrb_get_arg1(mrb);
  if (!mrb_proc_p(pr)) return mrb_false_value();
  const struct RProc *p = mrb_proc_ptr(pr);
  return mrb_bool_value(MRB_PROC_CFUNC_P(p) || irep_core_p(mrb, p->body.irep));
}

/* The frame to resolve from: the nearest Ruby frame that is not core code.
   *moved: resolution must start there rather than at mrb->c->ci (where C
   frames alone are passed through), because core frames lie above it, or
   because it is a block of a scope on another context: a Thread's or
   Fiber's body, which (core_ext wrapping it in a block of its own) has the
   context's root slot, as MRI's thread and fiber root_svar. */
static mrb_callinfo *svar_start(mrb_state *mrb, mrb_bool *moved) {
  struct mrb_context *c = mrb->c;
  mrb_callinfo *ci = c->ci;
  *moved = FALSE;
  for (; ci > c->cibase; ci--) {
    const struct RProc *p = ci->proc;
    if (!p || MRB_PROC_CFUNC_P(p)) continue;
    if (irep_core_p(mrb, p->body.irep)) {
      *moved = TRUE;
      continue;
    }
    if (!(MRB_PROC_SCOPE_P(p) && !MRB_PROC_ENV_P(p))) {
      /* a scope still standing on another context's stack; one that
         returned keeps its container in the env, which vm.c reads */
      struct REnv *e = svar_home_env(p);
      if (e && MRB_ENV_ONSTACK_P(e) && e->cxt != c && svar_root_context_p(mrb, c)) {
        *moved = TRUE;
        return c->cibase;
      }
    }
    return ci;
  }
  return ci;
}

/* A bare private/protected/public/module_function gives the scope proc of
   the frame it runs in (a class body, a method) the env of that very frame
   (class.c's vis_scope_persist). vm.c's svar_owner_from then no longer takes
   the frame for a scope owning its slot, and looking below it for the env's
   frame finds none: a read answers nil and a write is dropped, although a
   block of that frame resolves to the frame. With the env flag cleared for
   the call the frame owns its slot again, the one its blocks find (both are
   mrb_ci_svar of the frame), and the env it keeps on return is the same. */
static struct RProc *svar_persisted_scope(mrb_callinfo *ci) {
  struct RProc *p = (struct RProc *)ci->proc;
  if (p && !MRB_PROC_CFUNC_P(p) && MRB_PROC_SCOPE_P(p) && MRB_PROC_ENV_P(p) &&
      MRB_PROC_ENV(p) == mrb_vm_ci_env(ci))
    return p;
  return NULL;
}

static mrb_value svar_get(mrb_state *mrb, enum mrb_svar_index key) {
  mrb_bool moved;
  mrb_callinfo *start = svar_start(mrb, &moved);
  struct RProc *ps = start > mrb->c->cibase ? svar_persisted_scope(start) : NULL;
  if (!moved && !ps) return mrb_vm_svar_get(mrb, key);
  /* a read allocates nothing: no GC runs while ci is lowered or the flag
     is off (the proc's e.env then reads as its target class) */
  mrb_callinfo *saved = mrb->c->ci;
  mrb->c->ci = start;
  if (ps) ps->flags &= ~MRB_PROC_ENVSET;
  mrb_value v = mrb_vm_svar_get(mrb, key);
  if (ps) ps->flags |= MRB_PROC_ENVSET;
  mrb->c->ci = saved;
  return v;
}

static void svar_set(mrb_state *mrb, enum mrb_svar_index key, mrb_value v) {
  mrb_bool moved;
  mrb_callinfo *start = svar_start(mrb, &moved);
  struct RProc *ps = start > mrb->c->cibase ? svar_persisted_scope(start) : NULL;
  if (!moved && !ps) {
    mrb_vm_svar_set(mrb, key, v);
    return;
  }
  /* A first write allocates the scope's container. With ci lowered the GC
     would not mark the frames above it, and with the env flag off it would
     mark the proc's env as its target class, so none may run meanwhile. */
  mrb_callinfo *saved = mrb->c->ci;
  mrb_bool gc_off = mrb->gc.disabled;
  mrb->gc.disabled = TRUE;
  mrb->c->ci = start;
  if (ps) ps->flags &= ~MRB_PROC_ENVSET;
  mrb_vm_svar_set(mrb, key, v);
  if (ps) ps->flags |= MRB_PROC_ENVSET;
  mrb->c->ci = saved;
  mrb->gc.disabled = gc_off;
}

static mrb_value backref_get(mrb_state *mrb) { return svar_get(mrb, MRB_SVAR_BACKREF); }

static void backref_set(mrb_state *mrb, mrb_value v) {
  if (!mrb_nil_p(v)) {
    mrb_bool md = mrb_class_defined_id(mrb, MRB_SYM(MatchData)) &&
                  mrb_obj_is_kind_of(mrb, v, mrb_class_get_id(mrb, MRB_SYM(MatchData)));
    mrb_value kernel = mrb_obj_value(mrb->kernel_module);
    if (!md && mrb_respond_to(mrb, kernel, MRB_SYM(__oruby_gvcheck))) {
      /* its TypeError for anything but a MatchData */
      v = mrb_funcall_id(mrb, kernel, MRB_SYM(__oruby_gvcheck), 2, mrb_symbol_value(mrb_intern_lit(mrb, "$~")), v);
    }
  }
  svar_set(mrb, MRB_SVAR_BACKREF, v);
}

static mrb_value lastline_get(mrb_state *mrb) { return svar_get(mrb, MRB_SVAR_LASTLINE); }
static void lastline_set(mrb_state *mrb, mrb_value v) { svar_set(mrb, MRB_SVAR_LASTLINE, v); }

/* ---- Module#const_source_location's records ----
   mruby calls Module#const_added for each constant defined (mrb_const_set,
   autoload). This one records where: the nearest Ruby frame of user code,
   read through Module#const_set or #autoload and core Ruby frames. A
   constant a C or Go method defines (Go gems' classes on require, mruby's
   own) records nothing and answers [], as MRI's from C. The records are a
   hidden ivar of the module: { name => [file, line] }. */
/* MRI's two warnings for a constant assigned again, unless $VERBOSE is nil */
static void const_redefined_warn(mrb_state *mrb, mrb_value mod, mrb_sym name, const char *file, int32_t line, mrb_value prev) {
  if (mrb_nil_p(mrb_gv_get(mrb, mrb_intern_lit(mrb, "$VERBOSE")))) return;
  if (!mrb_class_defined_id(mrb, MRB_SYM(Warning))) return;
  mrb_value w = mrb_obj_value(mrb_module_get_id(mrb, MRB_SYM(Warning)));
  mrb_value msg = mrb_format(mrb, "%s:%d: warning: already initialized constant ", file, (int)line);
  if (mrb_class_ptr(mod) != mrb->object_class) {
    mrb_str_cat_str(mrb, msg, mrb_obj_as_string(mrb, mod));
    mrb_str_cat_lit(mrb, msg, "::");
  }
  mrb_str_cat_str(mrb, msg, mrb_sym_str(mrb, name));
  mrb_str_cat_lit(mrb, msg, "\n");
  mrb_funcall_id(mrb, w, MRB_SYM(warn), 1, msg);
  msg = mrb_format(mrb, "%v:%v: warning: previous definition of %n was here\n", RARRAY_PTR(prev)[0], RARRAY_PTR(prev)[1], name);
  mrb_funcall_id(mrb, w, MRB_SYM(warn), 1, msg);
}

/* ---- Module names as MRI's: a module assigned to a constant of a named
   one gets its permanent name, and so do the modules nested in it that took
   their names from it; one assigned under an anonymous (or temporarily
   named) module gets a temporary path, "#<Module:0x...>::N", at its first
   assignment, which set_temporary_name on it or on its parent replaces.
   mruby stores a nested anonymous path as if permanent: it is dropped.
   Hidden ivars: __oruby_tpath (the temporary path), __oruby_tparent /
   __oruby_tsym (where the module was first assigned). */
static mrb_bool mod_namespace_p(mrb_value v) {
  return mrb_type(v) == MRB_TT_CLASS || mrb_type(v) == MRB_TT_MODULE;
}

static mrb_bool mod_perm_p(mrb_state *mrb, mrb_value c) {
  mrb_value n = mrb_iv_get(mrb, c, MRB_SYM(__classname__));
  if (mrb_symbol_p(n)) return TRUE;
  return mrb_string_p(n) && RSTRING_LEN(n) > 0 && RSTRING_PTR(n)[0] != '#';
}

static mrb_value mod_perm_path(mrb_state *mrb, mrb_value c) {
  mrb_value n = mrb_iv_get(mrb, c, MRB_SYM(__classname__));
  if (mrb_symbol_p(n)) return mrb_sym_str(mrb, mrb_symbol(n));
  return mrb_str_dup(mrb, n);
}

static mrb_func_t mod_to_s_orig, mod_name_orig, mod_tmpname_orig;

/* the path a module nested in c is named after: c's name, else its
   anonymous form */
static mrb_value mod_parent_path(mrb_state *mrb, mrb_value c) {
  if (mod_perm_p(mrb, c)) return mod_perm_path(mrb, c);
  mrb_value t = mrb_iv_get(mrb, c, MRB_SYM(__oruby_tpath));
  if (mrb_string_p(t)) return mrb_str_dup(mrb, t);
  return mrb_any_to_s(mrb, c);
}

struct mod_children { mrb_value parent; mrb_value list; };
static int mod_children_i(mrb_state *mrb, mrb_sym sym, mrb_value v, void *p) {
  struct mod_children *d = (struct mod_children *)p;
  const char *n = mrb_sym_name(mrb, sym);
  if (!n || !ISUPPER(n[0]) || !mod_namespace_p(v) || mrb_obj_eq(mrb, v, d->parent)) return 0;
  mrb_value tp = mrb_iv_get(mrb, v, MRB_SYM(__oruby_tparent));
  if (!mrb_obj_eq(mrb, tp, d->parent)) return 0;
  mrb_ary_push(mrb, d->list, v);
  return 0;
}

static mrb_value mod_children(mrb_state *mrb, mrb_value c) {
  struct mod_children d = {c, mrb_ary_new(mrb)};
  mrb_iv_foreach(mrb, c, mod_children_i, &d);
  return d.list;
}

static mrb_bool mod_visited(mrb_state *mrb, mrb_value seen, mrb_value v) {
  for (mrb_int i = 0; i < RARRAY_LEN(seen); i++) if (mrb_obj_eq(mrb, RARRAY_PTR(seen)[i], v)) return TRUE;
  return FALSE;
}

static mrb_value mod_child_path(mrb_state *mrb, mrb_value path, mrb_value child) {
  mrb_value sym = mrb_iv_get(mrb, child, MRB_SYM(__oruby_tsym));
  if (mrb_nil_p(path) || !mrb_symbol_p(sym)) return mrb_nil_value();
  return mrb_format(mrb, "%v::%n", path, mrb_symbol(sym));
}

/* c and the modules named after it: temporary path (nil: anonymous) */
static void mod_set_tpath(mrb_state *mrb, mrb_value c, mrb_value path, mrb_value seen) {
  if (mod_perm_p(mrb, c) || mod_visited(mrb, seen, c)) return;
  mrb_ary_push(mrb, seen, c);
  if (mrb_string_p(path)) mrb_basic_ptr(path)->frozen = 1;
  mrb_iv_set(mrb, c, MRB_SYM(__oruby_tpath), path);
  mrb_value kids = mod_children(mrb, c);
  for (mrb_int i = 0; i < RARRAY_LEN(kids); i++) {
    mrb_value k = RARRAY_PTR(kids)[i];
    mod_set_tpath(mrb, k, mod_child_path(mrb, path, k), seen);
  }
}

/* c and the modules named after it: permanent names */
static void mod_set_perm(mrb_state *mrb, mrb_value c, mrb_value path, mrb_value seen) {
  if (mod_visited(mrb, seen, c)) return;
  mrb_ary_push(mrb, seen, c);
  if (!mod_perm_p(mrb, c)) {
    mrb_basic_ptr(path)->frozen = 1;
    mrb_iv_set(mrb, c, MRB_SYM(__classname__), path);
  }
  else path = mod_perm_path(mrb, c);
  mrb_iv_remove(mrb, c, MRB_SYM(__oruby_tpath));
  mrb_iv_remove(mrb, c, MRB_SYM(__tmpname__));
  mrb_value kids = mod_children(mrb, c);
  for (mrb_int i = 0; i < RARRAY_LEN(kids); i++) {
    mrb_value k = RARRAY_PTR(kids)[i];
    if (mod_perm_p(mrb, k)) continue;
    mod_set_perm(mrb, k, mod_child_path(mrb, path, k), seen);
  }
}

static void mod_name_assigned(mrb_state *mrb, mrb_value mod, mrb_sym name) {
  if (!mod_namespace_p(mod) && !mrb_sclass_p(mod)) return;
  mrb_value v = mrb_iv_get(mrb, mod, name);
  if (!mod_namespace_p(v) || mrb_obj_eq(mrb, v, mod)) return;
  mrb_value cn = mrb_iv_get(mrb, v, MRB_SYM(__classname__));
  if (mrb_string_p(cn) && RSTRING_LEN(cn) > 0 && RSTRING_PTR(cn)[0] == '#')
    mrb_iv_remove(mrb, v, MRB_SYM(__classname__));  /* mruby's anonymous path */
  mrb_bool parent_perm = !mrb_sclass_p(mod) && (mrb_class_ptr(mod) == mrb->object_class || mod_perm_p(mrb, mod));
  if (mrb_nil_p(mrb_iv_get(mrb, v, MRB_SYM(__oruby_tparent))) && !mod_perm_p(mrb, v)) {
    mrb_iv_set(mrb, v, MRB_SYM(__oruby_tparent), mod);
    mrb_iv_set(mrb, v, MRB_SYM(__oruby_tsym), mrb_symbol_value(name));
    if (!parent_perm && mrb_nil_p(mrb_iv_get(mrb, v, MRB_SYM(__tmpname__)))) {
      /* the parent's ancestry (a cycle) keeps its names */
      mrb_value seen = mrb_ary_new(mrb);
      for (mrb_value p = mod; mod_namespace_p(p) && !mrb_obj_eq(mrb, p, v) && !mod_visited(mrb, seen, p) && RARRAY_LEN(seen) < 64;
           p = mrb_iv_get(mrb, p, MRB_SYM(__oruby_tparent)))
        mrb_ary_push(mrb, seen, p);
      mod_set_tpath(mrb, v, mrb_format(mrb, "%v::%n", mod_parent_path(mrb, mod), name), seen);
    }
  }
  if (parent_perm) {
    mrb_value path = mrb_class_ptr(mod) == mrb->object_class ? mrb_sym_str(mrb, name)
                                                             : mrb_format(mrb, "%v::%n", mod_perm_path(mrb, mod), name);
    mod_set_perm(mrb, v, path, mrb_ary_new(mrb));
  }
}

/* Module#name: the permanent name (the same String each time), else the
   temporary path, else nil */
static mrb_value mod_name_w(mrb_state *mrb, mrb_value self) {
  if (!mod_namespace_p(self)) return mod_name_orig(mrb, self);
  mrb_value cn = mrb_iv_get(mrb, self, MRB_SYM(__classname__));
  if (mrb_string_p(cn) && RSTRING_LEN(cn) > 0 && RSTRING_PTR(cn)[0] != '#') {
    mrb_basic_ptr(cn)->frozen = 1;
    return cn;
  }
  if (mrb_symbol_p(cn)) {
    mrb_value c = mrb_iv_get(mrb, self, MRB_SYM(__oruby_pname));
    if (!mrb_string_p(c)) {
      c = mrb_sym_str(mrb, mrb_symbol(cn));
      mrb_basic_ptr(c)->frozen = 1;
      mrb_iv_set(mrb, self, MRB_SYM(__oruby_pname), c);
    }
    return c;
  }
  if (mrb_iv_defined(mrb, self, MRB_SYM(__oruby_tpath))) return mrb_iv_get(mrb, self, MRB_SYM(__oruby_tpath));
  return mod_name_orig(mrb, self);
}

static mrb_value mod_to_s_w(mrb_state *mrb, mrb_value self) {
  if (mod_namespace_p(self) && !mod_perm_p(mrb, self)) {
    mrb_value t = mrb_iv_get(mrb, self, MRB_SYM(__oruby_tpath));
    if (mrb_string_p(t)) return mrb_str_dup(mrb, t);
    if (mrb_iv_defined(mrb, self, MRB_SYM(__oruby_tpath))) return mrb_any_to_s(mrb, self);
  }
  return mod_to_s_orig(mrb, self);
}

static mrb_value mod_tmpname_w(mrb_state *mrb, mrb_value self) {
  if (mrb_get_argc(mrb) == 1) {
    mrb_value a = mrb_get_argv(mrb)[0];
    /* "::A::B" is a constant path too (mruby's check takes "A::B") */
    if (mrb_string_p(a) && RSTRING_LEN(a) > 2 && RSTRING_PTR(a)[0] == ':' && RSTRING_PTR(a)[1] == ':') {
      const char *p = RSTRING_PTR(a), *e = p + RSTRING_LEN(a);
      mrb_bool path = TRUE;
      while (p < e && path) {
        if (e - p < 3 || p[0] != ':' || p[1] != ':' || !ISUPPER(p[2])) { path = FALSE; break; }
        p += 3;
        while (p < e && (ISALNUM(*p) || *p == '_' || (unsigned char)*p >= 0x80)) p++;
      }
      if (path) mrb_raise(mrb, E_ARGUMENT_ERROR, "the temporary name must not be a constant path to avoid confusion");
    }
  }
  mrb_value r = mod_tmpname_orig(mrb, self);
  mrb_value n = mrb_iv_get(mrb, self, MRB_SYM(__tmpname__));
  if (mrb_nil_p(n)) {
    mod_set_tpath(mrb, self, mrb_nil_value(), mrb_ary_new(mrb));
    mrb_iv_remove(mrb, self, MRB_SYM(__oruby_tparent));
    mrb_iv_remove(mrb, self, MRB_SYM(__oruby_tsym));
  }
  else {
    mod_set_tpath(mrb, self, mrb_str_dup(mrb, n), mrb_ary_new(mrb));
  }
  return r;
}

static void install_module_names(mrb_state *mrb) {
  wrap_cfunc(mrb, mrb->module_class, "name", mod_name_w, &mod_name_orig);
  wrap_cfunc(mrb, mrb->module_class, "to_s", mod_to_s_w, &mod_to_s_orig);
  mrb_alias_method(mrb, mrb->module_class, MRB_SYM(inspect), MRB_SYM(to_s));
  wrap_cfunc(mrb, mrb->module_class, "set_temporary_name", mod_tmpname_w, &mod_tmpname_orig);
}

static mrb_value mod_const_added_m(mrb_state *mrb, mrb_value mod) {
  mrb_sym name = mrb_obj_to_sym(mrb, mrb_get_arg1(mrb));
  mod_name_assigned(mrb, mod, name);
  struct mrb_context *c = mrb->c;
  mrb_bool autoload = FALSE;
  for (mrb_callinfo *ci = c->ci - 1; ci >= c->cibase; ci--) {
    const struct RProc *p = ci->proc;
    if (!p || MRB_PROC_CFUNC_P(p)) {
      if (ci->mid == MRB_SYM(autoload)) autoload = TRUE;
      if (ci->mid == MRB_SYM(const_set) || ci->mid == MRB_SYM(autoload)) continue;
      return mrb_nil_value();
    }
    const mrb_irep *irep = p->body.irep;
    if (irep_core_p(mrb, irep)) continue;
    if (!irep->debug_info || !ci->pc) return mrb_nil_value();
    int32_t line;
    const char *file;
    if (!mrb_debug_get_position(mrb, irep, (uint32_t)(&ci->pc[-1] - irep->iseq), &line, &file) || !file) {
      return mrb_nil_value();
    }
    mrb_value h = mrb_iv_get(mrb, mod, MRB_SYM(__oruby_csl));
    if (!mrb_hash_p(h)) {
      h = mrb_hash_new(mrb);
      mrb_iv_set(mrb, mod, MRB_SYM(__oruby_csl), h);
    }
    /* [file, line, from autoload, value]: an autoload's real definition is
       no redefinition, nor is the same object set again by the same
       statement (a gem's Go code naming the class its Ruby defined) */
    /* the raw slot: mrb_const_get would load an autoload's file */
    mrb_value cur = mrb_iv_get(mrb, mod, name);
    mrb_value prev = mrb_hash_get(mrb, h, mrb_symbol_value(name));
    if (!autoload && mrb_array_p(prev) && RARRAY_LEN(prev) == 4 && !mrb_test(RARRAY_PTR(prev)[2])) {
      mrb_bool same = mrb_obj_eq(mrb, RARRAY_PTR(prev)[3], cur) && mrb_integer(RARRAY_PTR(prev)[1]) == line &&
                      strcmp(RSTRING_PTR(RARRAY_PTR(prev)[0]), file) == 0;
      if (!same) const_redefined_warn(mrb, mod, name, file, line, prev);
    }
    mrb_value rec[4] = {mrb_str_new_cstr(mrb, file), mrb_fixnum_value(line), mrb_bool_value(autoload), cur};
    mrb_hash_set(mrb, h, mrb_symbol_value(name), mrb_ary_new_from_values(mrb, 4, rec));
    return mrb_nil_value();
  }
  return mrb_nil_value();
}

/* Module#__oruby_const_location(sym): the record ([file, line], a copy) or nil */
static mrb_value autoload_active(mrb_state *, mrb_value, mrb_value);
static mrb_bool autoload_other_thread(mrb_state *, mrb_value);
static mrb_value mod_const_location_m(mrb_state *mrb, mrb_value mod) {
  mrb_value mark = autoload_active(mrb, mod, mrb_get_arg1(mrb));
  if (autoload_other_thread(mrb, mark)) {
    mrb_value loc = RARRAY_PTR(mark)[2];
    return mrb_array_p(loc) ? mrb_ary_new_from_values(mrb, 2, RARRAY_PTR(loc)) : mrb_nil_value();
  }
  mrb_value h = mrb_iv_get(mrb, mod, MRB_SYM(__oruby_csl));
  if (!mrb_hash_p(h)) return mrb_nil_value();
  mrb_value loc = mrb_hash_get(mrb, h, mrb_get_arg1(mrb));
  return mrb_array_p(loc) ? mrb_ary_new_from_values(mrb, 2, RARRAY_PTR(loc)) : mrb_nil_value();
}

/* Module#remove_const drops the record too (const_source_location counts a
   record as a definition while an autoload is loading), and the VM's
   constant cache, which mruby's leaves holding the removed value */
void mrb_const_cache_clear(mrb_state *mrb);  /* variable.c */
static mrb_func_t remove_const_orig;
static mrb_value remove_const_w(mrb_state *mrb, mrb_value mod) {
  /* a deprecated one warns as when read (MRI's rb_const_remove); mruby's
     read gives the warning with the caller's location */
  if (mrb_get_argc(mrb) >= 1 && (mrb_type(mod) == MRB_TT_CLASS || mrb_type(mod) == MRB_TT_MODULE)) {
    mrb_value n = mrb_get_argv(mrb)[0];
    mrb_sym sym = mrb_symbol_p(n) ? mrb_symbol(n) : 0;
    if (mrb_string_p(n)) sym = mrb_intern_check_str(mrb, n);
    if (sym && (mrb_const_flags(mrb, mrb_class_ptr(mod), sym) & MRB_CONST_DEPRECATED) &&
        mrb_const_defined_at(mrb, mod, sym))
      (void)mrb_const_get(mrb, mod, sym);
  }
  mrb_value v = remove_const_orig(mrb, mod);
  mrb_const_cache_clear(mrb);
  mrb_value h = mrb_iv_get(mrb, mod, MRB_SYM(__oruby_csl));
  if (mrb_hash_p(h)) {
    mrb_value name = mrb_get_argv(mrb)[0];
    if (mrb_string_p(name)) name = mrb_symbol_value(mrb_intern_str(mrb, name));
    mrb_hash_delete_key(mrb, h, name);
  }
  return v;
}

/* ---- implicit block parameters (it, _1.._9) and hidden locals ----
   mruby names _1.._9 as locals and leaves `it` unnamed (its register 1);
   the Prism rewriter (ORB_IT_BLOCK) marks a block of `it` with the hidden
   local __oruby_it. MRI lists neither among local_variables, nor oruby's
   own hidden locals (__oruby_*, the rewriter's temporaries). */
const struct RProc *mrb_binding_extract_proc(mrb_state *mrb, mrb_value binding);
struct REnv *mrb_binding_extract_env(mrb_state *mrb, mrb_value binding);

static int numbered_param(mrb_state *mrb, mrb_sym sym) {
  mrb_int len;
  const char *n = mrb_sym_name_len(mrb, sym, &len);
  return (n && len == 2 && n[0] == '_' && n[1] >= '1' && n[1] <= '9') ? n[1] - '0' : 0;
}

static mrb_bool hidden_local(mrb_state *mrb, mrb_sym sym) {
  mrb_int len;
  const char *n = mrb_sym_name_len(mrb, sym, &len);
  return numbered_param(mrb, sym) || (n && len > 8 && memcmp(n, "__oruby_", 8) == 0);
}

static mrb_value lvars_filter(mrb_state *mrb, mrb_value ary) {
  if (!mrb_array_p(ary)) return ary;
  mrb_value out = mrb_ary_new_capa(mrb, RARRAY_LEN(ary));
  for (mrb_int i = 0; i < RARRAY_LEN(ary); i++) {
    mrb_value v = RARRAY_PTR(ary)[i];
    if (!mrb_symbol_p(v) || !hidden_local(mrb, mrb_symbol(v))) mrb_ary_push(mrb, out, v);
  }
  return out;
}

static mrb_func_t k_lvars_orig, b_lvars_orig;
static mrb_value k_lvars_w(mrb_state *mrb, mrb_value self) { return lvars_filter(mrb, k_lvars_orig(mrb, self)); }
static mrb_value b_lvars_w(mrb_state *mrb, mrb_value self) { return lvars_filter(mrb, b_lvars_orig(mrb, self)); }

/* the binding's own scope: past mruby-binding's lvspaces (the procs holding
   locals that eval added), with that scope's env */
static const struct RProc *binding_scope(mrb_state *mrb, mrb_value b, struct REnv **ep) {
  const struct RProc *p = mrb_binding_extract_proc(mrb, b);
  struct REnv *e = mrb_binding_extract_env(mrb, b);
  while (p && !MRB_PROC_CFUNC_P(p) && p->body.irep->ilen == 2 && p->body.irep->iseq[0] == OP_RETURN && p->upper) {
    e = MRB_PROC_ENV(p);
    p = p->upper;
  }
  *ep = e;
  return p;
}

/* Binding#__oruby_implicit_params: [:it], [:_1, .., :_n] or [] of its own scope */
/* Binding#__oruby_adopt(b): self takes b's frame (TOPLEVEL_BINDING the
   main script's, as MRI's: Kernel.__oruby_main_binding) */
static mrb_value binding_adopt_m(mrb_state *mrb, mrb_value self) {
  mrb_value b = mrb_get_arg1(mrb);
  const mrb_sym ivs[4] = {MRB_SYM(proc), MRB_SYM(env), MRB_SYM(recv), MRB_SYM(pc)};
  for (int i = 0; i < 4; i++) {
    if (mrb_iv_defined(mrb, b, ivs[i])) mrb_iv_set(mrb, self, ivs[i], mrb_iv_get(mrb, b, ivs[i]));
    else mrb_iv_remove(mrb, self, ivs[i]);
  }
  return self;
}

static mrb_value binding_implicit_m(mrb_state *mrb, mrb_value self) {
  struct REnv *e;
  const struct RProc *p = binding_scope(mrb, self, &e);
  mrb_value out = mrb_ary_new(mrb);
  if (!p || MRB_PROC_CFUNC_P(p) || !p->body.irep->lv) return out;
  const mrb_irep *irep = p->body.irep;
  int max = 0;
  for (int i = 0; i + 1 < irep->nlocals; i++) {
    mrb_sym sym = irep->lv[i];
    if (!sym) continue;
    if (sym == MRB_SYM(__oruby_it)) {
      mrb_ary_push(mrb, out, mrb_symbol_value(MRB_SYM(it)));
      return out;
    }
    int n = numbered_param(mrb, sym);
    if (n > max) max = n;
  }
  for (int n = 1; n <= max; n++) {
    char name[3] = {'_', (char)('0' + n), 0};
    mrb_ary_push(mrb, out, mrb_symbol_value(mrb_intern_cstr(mrb, name)));
  }
  return out;
}

/* Binding#__oruby_it: the value of `it` in its own scope (register 1) */
static mrb_value binding_it_m(mrb_state *mrb, mrb_value self) {
  struct REnv *e;
  binding_scope(mrb, self, &e);
  if (!e || MRB_ENV_LEN(e) < 2) return mrb_nil_value();
  return e->stack[1];
}

/* ---- Method#original_name / UnboundMethod#original_name ----
   An alias of a Ruby method is a proc holding the original name (mruby's
   MRB_PROC_ALIAS). Others are kept per class in a hidden table
   { name => original }: define_method of a Method or UnboundMethod, and
   alias_method of a C method or of a define_method'd one. */
static mrb_value orig_table(mrb_state *mrb, struct RClass *c, mrb_bool make) {
  mrb_value h = mrb_iv_get(mrb, mrb_obj_value(c), MRB_SYM(__oruby_orig));
  if (!mrb_hash_p(h) && make) {
    h = mrb_hash_new(mrb);
    mrb_iv_set(mrb, mrb_obj_value(c), MRB_SYM(__oruby_orig), h);
  }
  return h;
}

/* the original name of method `name` found in `owner`, as a method entry `m` */
static mrb_sym orig_resolve(mrb_state *mrb, struct RClass *owner, mrb_sym name, mrb_method_t m) {
  mrb_value h = orig_table(mrb, owner, FALSE);
  if (mrb_hash_p(h)) {
    mrb_value o = mrb_hash_get(mrb, h, mrb_symbol_value(name));
    if (mrb_symbol_p(o)) return mrb_symbol(o);
  }
  if (!MRB_METHOD_UNDEF_P(m) && !MRB_METHOD_CFUNC_P(m)) {
    const struct RProc *p = MRB_METHOD_PROC(m);
    if (p && MRB_PROC_ALIAS_P(p)) return p->body.mid;
  }
  return name;
}

static mrb_value method_original_name_m(mrb_state *mrb, mrb_value self) {
  mrb_value name = mrb_iv_get(mrb, self, MRB_SYM(_name));
  mrb_value owner = mrb_iv_get(mrb, self, MRB_SYM(_owner));
  mrb_value proc = mrb_iv_get(mrb, self, MRB_SYM(_proc));
  if (!mrb_symbol_p(name)) return name;
  mrb_sym n = mrb_symbol(name);
  if (mrb_proc_p(proc) && MRB_PROC_ALIAS_P(mrb_proc_ptr(proc))) n = mrb_proc_ptr(proc)->body.mid;
  if (mrb_class_p(owner) || mrb_module_p(owner) || mrb_sclass_p(owner)) {
    mrb_value h = orig_table(mrb, mrb_class_ptr(owner), FALSE);
    if (mrb_hash_p(h)) {
      mrb_value o = mrb_hash_get(mrb, h, mrb_symbol_value(n));
      if (mrb_symbol_p(o)) n = mrb_symbol(o);
    }
  }
  return mrb_symbol_value(n);
}

static mrb_func_t define_method_orig, alias_method_orig;

static mrb_value define_method_w(mrb_state *mrb, mrb_value mod) {
  mrb_int argc = mrb_get_argc(mrb);
  mrb_value name = argc > 0 ? mrb_get_argv(mrb)[0] : mrb_nil_value();
  mrb_value body = argc > 1 ? mrb_get_argv(mrb)[1] : mrb_nil_value();
  mrb_value r = define_method_orig(mrb, mod);
  if (!mrb_nil_p(body) && mrb_obj_respond_to(mrb, mrb_class(mrb, body), MRB_SYM(original_name)) &&
      mrb_iv_defined(mrb, body, MRB_SYM(_proc))) {
    mrb_value orig = method_original_name_m(mrb, body);
    mrb_sym n = mrb_obj_to_sym(mrb, name);
    if (mrb_symbol_p(orig) && mrb_symbol(orig) != n) {
      mrb_hash_set(mrb, orig_table(mrb, mrb_class_ptr(mod), TRUE), mrb_symbol_value(n), orig);
    }
  }
  return r;
}

/* MRI 4.1: aliasing a method that a prepended module defines is deprecated.
   mruby's prepend moves the class's own methods to an origin iclass, the
   prepended modules' iclasses in between: a lookup that ends before the
   origin found a prepended method. */
static void prepend_alias_warn(mrb_state *mrb, mrb_value mod, mrb_value oldv) {
  struct RClass *c = mrb_class_ptr(mod);
  if (!(c->flags & MRB_FL_CLASS_IS_PREPENDED) || !(mrb_symbol_p(oldv) || mrb_string_p(oldv))) return;
  if (mrb_nil_p(mrb_gv_get(mrb, MRB_GVSYM(VERBOSE)))) return;
  struct RClass *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mrb_obj_to_sym(mrb, oldv));
  if (MRB_METHOD_UNDEF_P(m) || owner == c) return;
  for (struct RClass *p = c->super; p; p = p->super) {
    if (p->flags & MRB_FL_CLASS_IS_ORIGIN) return;  /* the class's own, or after */
    if (p == owner) break;
  }
  mrb_value k = mrb_obj_value(mrb->kernel_module);
  if (!mrb_respond_to(mrb, k, MRB_SYM(__oruby_warn_here))) return;
  struct RClass *pm = owner->tt == MRB_TT_ICLASS ? owner->c : owner;
  mrb_value msg = mrb_format(mrb, "aliasing %v#%n defined in a prepended module %v is deprecated and will be removed in Ruby 4.3",
                             mrb_inspect(mrb, mod), mrb_obj_to_sym(mrb, oldv), mrb_inspect(mrb, mrb_obj_value(pm)));
  mrb_funcall_id(mrb, k, MRB_SYM(__oruby_warn_here), 2, msg, mrb_symbol_value(MRB_SYM(deprecated)));
}

static mrb_value alias_method_w(mrb_state *mrb, mrb_value mod) {
  mrb_int argc = mrb_get_argc(mrb);
  mrb_value newv = argc > 0 ? mrb_get_argv(mrb)[0] : mrb_nil_value();
  mrb_value oldv = argc > 1 ? mrb_get_argv(mrb)[1] : mrb_nil_value();
  prepend_alias_warn(mrb, mod, oldv);
  if ((mrb_symbol_p(newv) || mrb_string_p(newv)) && (mrb_symbol_p(oldv) || mrb_string_p(oldv)) &&
      mrb_test(mrb_gv_get(mrb, MRB_GVSYM(VERBOSE)))) {
    /* MRI's "method redefined; discarding old" for an unrelated method of
       the class's own that the alias replaces */
    mrb_sym nn = mrb_obj_to_sym(mrb, newv), on = mrb_obj_to_sym(mrb, oldv);
    struct RClass *c = mrb_class_ptr(mod), *o1 = c, *o2 = c;
    mrb_method_t mn = mrb_method_search_vm(mrb, &o1, nn), mo = mrb_method_search_vm(mrb, &o2, on);
    if (nn != on && !MRB_METHOD_UNDEF_P(mn) && o1 == c && !MRB_METHOD_UNDEF_P(mo) &&
        MRB_METHOD_PROC_P(mn) && MRB_METHOD_PROC_P(mo) && MRB_METHOD_PROC(mn) != MRB_METHOD_PROC(mo) &&
        !MRB_PROC_ALIAS_P(MRB_METHOD_PROC(mn))) {
      mrb_value k = mrb_obj_value(mrb->kernel_module);
      if (mrb_respond_to(mrb, k, MRB_SYM(__oruby_warn_here)))
        mrb_funcall_id(mrb, k, MRB_SYM(__oruby_warn_here), 1, mrb_format(mrb, "method redefined; discarding old %n", nn));
    }
  }
  mrb_value r = alias_method_orig(mrb, mod);
  if (mrb_symbol_p(newv) || mrb_string_p(newv)) {
    mrb_sym nn = mrb_obj_to_sym(mrb, newv);
    if (special_private_name(nn)) set_own_visibility(mrb, mrb_class_ptr(mod), nn, MRB_METHOD_PRIVATE_FL);
  }
  if ((mrb_symbol_p(newv) || mrb_string_p(newv)) && (mrb_symbol_p(oldv) || mrb_string_p(oldv))) {
    mrb_sym nn = mrb_obj_to_sym(mrb, newv), on = mrb_obj_to_sym(mrb, oldv);
    struct RClass *owner = mrb_class_ptr(mod);
    mrb_method_t m = mrb_method_search_vm(mrb, &owner, on);
    mrb_sym orig = orig_resolve(mrb, owner, on, m);
    if (orig != nn) mrb_hash_set(mrb, orig_table(mrb, mrb_class_ptr(mod), TRUE), mrb_symbol_value(nn), mrb_symbol_value(orig));
  }
  return r;
}

/* ---- Module#attr: names by #to_str, and the obsolete attr(name, true/false) ----
   mruby's attr is attr_reader, which takes the visibility from its caller's
   frame (caller_scope_visibility), so this stays the same C frame: the
   boolean form calls the reader (and writer) functions with the argument
   count narrowed to the name. */
static mrb_func_t attr_orig, attr_writer_fn, attr_reader_orig, attr_writer_orig, attr_accessor_orig;

/* MRI's attribute names: a Symbol, a String, or what #to_str makes one;
   converted in place (the frame's own arguments). skip_bool: attr's
   obsolete true/false second argument */
static void attr_names(mrb_state *mrb, mrb_bool skip_bool) {
  mrb_int argc = mrb_get_argc(mrb);
  mrb_value *argv = (mrb_value *)mrb_get_argv(mrb);
  for (mrb_int i = 0; i < argc; i++) {
    if (mrb_symbol_p(argv[i]) || mrb_string_p(argv[i])) continue;
    if (skip_bool && i == 1 && argc == 2 && (mrb_true_p(argv[1]) || mrb_false_p(argv[1]))) continue;
    if (!mrb_respond_to(mrb, argv[i], MRB_SYM(to_str))) {
      mrb_raisef(mrb, E_TYPE_ERROR, "%!v is not a symbol nor a string", argv[i]);
    }
    mrb_value cv[2] = {argv[i], mrb_obj_value(mrb->string_class)};
    argv[i] = mrb_funcall_argv(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__implicit), 2, cv);
  }
}

/* MRI announces each attribute method (method_added, or
   singleton_method_added for a singleton class); mruby's attr_* answer the
   names they defined and announce none */
static mrb_value attr_added(mrb_state *mrb, mrb_value mod, mrb_value names) {
  if (!mrb_array_p(names)) return names;
  for (mrb_int i = 0; i < RARRAY_LEN(names); i++) {
    mrb_value n = RARRAY_PTR(names)[i];
    if (mrb_symbol_p(n)) mrb_method_added(mrb, mrb_class_ptr(mod), mrb_symbol(n));
  }
  return names;
}

static mrb_value attr_reader_w(mrb_state *mrb, mrb_value mod) { attr_names(mrb, FALSE); return attr_added(mrb, mod, attr_reader_orig(mrb, mod)); }
static mrb_value attr_writer_w(mrb_state *mrb, mrb_value mod) { attr_names(mrb, FALSE); return attr_added(mrb, mod, attr_writer_orig(mrb, mod)); }
static mrb_value attr_accessor_w(mrb_state *mrb, mrb_value mod) { attr_names(mrb, FALSE); return attr_added(mrb, mod, attr_accessor_orig(mrb, mod)); }

static mrb_value attr_w(mrb_state *mrb, mrb_value mod) {
  mrb_callinfo *ci = mrb->c->ci;
  mrb_int argc = mrb_get_argc(mrb);
  attr_names(mrb, TRUE);
  const mrb_value *argv = mrb_get_argv(mrb);
  if (argc == 2 && ci->n == 2 && (mrb_true_p(argv[1]) || mrb_false_p(argv[1])) && attr_writer_fn) {
    mrb_bool writable = mrb_true_p(argv[1]);
    if (mrb_test(mrb_gv_get(mrb, mrb_intern_lit(mrb, "$VERBOSE"))) && mrb_class_defined_id(mrb, MRB_SYM(Warning))) {
      mrb_funcall_id(mrb, mrb_obj_value(mrb_module_get_id(mrb, MRB_SYM(Warning))), MRB_SYM(warn), 1,
                     mrb_str_new_lit(mrb, "warning: optional boolean argument is obsoleted\n"));
    }
    ci->n = 1;
    mrb_value names = attr_orig(mrb, mod);
    if (writable) mrb_ary_concat(mrb, names, attr_writer_fn(mrb, mod));
    ci->n = 2;
    return attr_added(mrb, mod, names);
  }
  return attr_added(mrb, mod, attr_orig(mrb, mod));
}

/* ---- Proc#parameters / Method#parameters, as MRI's ----
   mruby's answers no :keyreq and loses keyword names. Registers after self: required, optional, rest,
   post, the keyword hash (when there are keywords), the block; keyword
   locals are named by the OP_KARG that fills each (OP_KEY_P first for an
   optional one). lambda: -1 the proc's own kind, 0 as a proc, 1 as a lambda. */
#define OPSIZE_Z 1
#define OPSIZE_B 2
#define OPSIZE_BB 3
#define OPSIZE_BBB 4
#define OPSIZE_BS 4
#define OPSIZE_BSS 6
#define OPSIZE_S 3
#define OPSIZE_W 4
static const uint8_t orb_op_size[] = {
#define OPCODE(x, f) OPSIZE_##f,
#include <mruby/ops.h>
#undef OPCODE
};

mrb_value mrb_proc_parameters(mrb_state *mrb, mrb_value proc);

static mrb_value param_pair(mrb_state *mrb, mrb_sym kind, mrb_sym name) {
  mrb_value a = mrb_ary_new_capa(mrb, 2);
  mrb_ary_push(mrb, a, mrb_symbol_value(kind));
  if (name) {
    /* a nested (a, b) parameter, a hidden local (prism.go ORB_BLOCK_PARAMS):
       MRI names none */
    mrb_int len;
    const char *s = mrb_sym_name_len(mrb, name, &len);
    if (!(s && len > 10 && memcmp(s, "__oruby_bp", 10) == 0)) mrb_ary_push(mrb, a, mrb_symbol_value(name));
  }
  return a;
}

/* Kernel.__oruby_bta?(first, others...) for ORB_BLOCK_PARAMS (prism.go):
   whether the block's frame took one object mruby did not spread, so MRI's
   #to_ary is tried: not a lambda's frame (a strict proc never spreads), its
   first parameter not an Array, the others nil */
static mrb_value block_to_ary_p(mrb_state *mrb, mrb_value self) {
  mrb_int argc;
  const mrb_value *argv;
  mrb_get_args(mrb, "*", &argv, &argc);
  mrb_callinfo *ci = mrb->c->ci;
  if (argc < 1 || ci <= mrb->c->cibase) return mrb_false_value();
  const struct RProc *p = ci[-1].proc;
  if (!p || MRB_PROC_STRICT_P(p) || mrb_array_p(argv[0])) return mrb_false_value();
  for (mrb_int i = 1; i < argc; i++) if (!mrb_nil_p(argv[i])) return mrb_false_value();
  return mrb_true_value();
}

static mrb_value orb_params(mrb_state *mrb, mrb_value procv, int lambda) {
  const struct RProc *proc = mrb_proc_ptr(procv);
  while (MRB_PROC_ALIAS_P(proc)) {
    proc = proc->upper;
    if (!proc) return mrb_ary_new(mrb);
  }
  const mrb_irep *irep = MRB_PROC_CFUNC_P(proc) ? NULL : proc->body.irep;
  if (!irep || !irep->lv || irep->ilen < 4 || irep->iseq[0] != OP_ENTER) return mrb_proc_parameters(mrb, procv);
  mrb_aspec aspec = PEEK_W(irep->iseq + 1);
  mrb_bool strict = lambda < 0 ? MRB_PROC_STRICT_P(proc) : lambda > 0;
  int m1 = MRB_ASPEC_REQ(aspec), o = MRB_ASPEC_OPT(aspec), r = MRB_ASPEC_REST(aspec);
  int m2 = MRB_ASPEC_POST(aspec), k = MRB_ASPEC_KEY(aspec), kd = MRB_ASPEC_KDICT(aspec);
  int nlv = irep->nlocals - 1, reg = 0;
#define LV(i) ((i) < nlv ? irep->lv[i] : 0) /* i is evaluated twice: no side effects */
  mrb_value out = mrb_ary_new(mrb);
  mrb_sym req = strict ? MRB_SYM(req) : MRB_SYM(opt);
  for (int i = 0; i < m1; i++, reg++) mrb_ary_push(mrb, out, param_pair(mrb, req, LV(reg)));
  for (int i = 0; i < o; i++, reg++) mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(opt), LV(reg)));
  if (r) {
    mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(rest), LV(reg)));
    reg++;
  }
  for (int i = 0; i < m2; i++, reg++) mrb_ary_push(mrb, out, param_pair(mrb, req, LV(reg)));
  mrb_sym kdname = 0;
  if (k || kd) {
    kdname = LV(reg);
    reg++;
  }
  /* the block register, then an explicit block's own name */
  mrb_sym blkname = LV(reg + 1);
  /* the keywords, in order, from the code filling them */
  if (k) {
    const mrb_code *pc = irep->iseq + 4, *end = irep->iseq + irep->ilen;
    mrb_sym keyp[32];
    int nkeyp = 0, found = 0;
    mrb_value opts = mrb_ary_new(mrb); /* MRI lists the required keywords first */
    while (pc < end && found < k) {
      int op = *pc, ext = 0;
      if (op == OP_EXT1 || op == OP_EXT2 || op == OP_EXT3) {
        ext = op - OP_EXT1 + 1; /* 1: a 16 bit, 2: b, 3: both */
        pc++;
        op = *pc;
      }
      if (op >= (int)sizeof(orb_op_size)) break;
      if (op == OP_KEY_P || op == OP_KARG) {
        const mrb_code *q = pc + 1;
        q += (ext & 1) ? 2 : 1; /* a */
        uint32_t b = (ext & 2) ? PEEK_S(q) : PEEK_B(q);
        mrb_sym name = b < irep->slen ? irep->syms[b] : 0;
        if (op == OP_KEY_P) {
          if (nkeyp < 32) keyp[nkeyp++] = name;
        } else {
          mrb_bool opt = FALSE;
          for (int i = 0; i < nkeyp; i++) if (keyp[i] == name) opt = TRUE;
          if (opt) mrb_ary_push(mrb, opts, param_pair(mrb, MRB_SYM(key), name));
          else mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(keyreq), name));
          found++;
        }
      }
      pc += orb_op_size[op] + ((ext & 1) ? 1 : 0) + ((ext & 2) ? 1 : 0);
    }
    mrb_ary_concat(mrb, out, opts);
  }
  if (kd) {
    /* **nil: mruby's compiler names its dictionary "**nil" */
    mrb_int kl;
    const char *kn = kdname ? mrb_sym_name_len(mrb, kdname, &kl) : NULL;
    if (kn && kl == 5 && memcmp(kn, "**nil", 5) == 0) mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(nokey), 0));
    else mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(keyrest), kdname));
  }
  if (MRB_ASPEC_BLOCK(aspec)) mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(block), blkname));
  if (MRB_ASPEC_NOBLOCK(aspec)) mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(noblock), 0));
#undef LV
  return out;
}

mrb_value orb_params_c(mrb_state *mrb, mrb_value procv) { return orb_params(mrb, procv, 1); }

/* Proc#__oruby_params(lambda): -1, 0 or 1 (see orb_params) */
static mrb_value proc_params_m(mrb_state *mrb, mrb_value self) {
  mrb_int lambda = -1;
  mrb_get_args(mrb, "|i", &lambda);
  return orb_params(mrb, self, (int)lambda);
}

mrb_value mrb_attr_writer(mrb_state *mrb, mrb_value obj);

/* a core method's parameters as MRI shows its C methods': one [:req] per
   argument of a fixed arity, [[:rest]] for any other */
static mrb_value core_params(mrb_state *mrb, mrb_aspec aspec) {
  mrb_value out = mrb_ary_new(mrb);
  if (MRB_ASPEC_OPT(aspec) || MRB_ASPEC_REST(aspec) || MRB_ASPEC_POST(aspec) || MRB_ASPEC_KEY(aspec) ||
      MRB_ASPEC_KDICT(aspec)) {
    mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(rest), 0));
    return out;
  }
  for (int i = 0; i < MRB_ASPEC_REQ(aspec); i++) mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(req), 0));
  return out;
}

static mrb_value ruby2_keywords_mark_m(mrb_state *mrb, mrb_value self) {
  mrb_value proc = mrb_proc_p(self) ? self : mrb_iv_get(mrb, self, MRB_SYM(_proc));
  if (!mrb_proc_p(proc)) return mrb_false_value();
  const struct RProc *p = mrb_proc_ptr(proc);
  while (p && MRB_PROC_ALIAS_P(p)) p = p->upper;
  if (!p || MRB_PROC_CFUNC_P(p)) return mrb_false_value();
  const mrb_irep *irep = p->body.irep;
  if (!irep || (irep->flags & MRB_IREP_NO_FREE) || irep->ilen < 4 || irep->iseq[0] != OP_ENTER) {
    return mrb_false_value();
  }
  mrb_aspec args = PEEK_W(irep->iseq + 1);
  if (!MRB_ASPEC_REST(args) || MRB_ASPEC_POST(args) || MRB_ASPEC_KEY(args) || MRB_ASPEC_KDICT(args)) {
    return mrb_false_value();
  }
  ((mrb_irep*)irep)->flags |= ORUBY_IREP_RUBY2_KEYWORDS;
  return mrb_true_value();
}

/* Method#parameters, UnboundMethod#parameters: a method takes its
   arguments as a lambda does; a core one (C, Go or core Ruby) shows as
   MRI's C methods do, and one respond_to_missing? answers takes any */
static mrb_value method_params_m(mrb_state *mrb, mrb_value self) {
  mrb_value procv = mrb_iv_get(mrb, self, MRB_SYM(_proc));
  mrb_value out;
  if (!mrb_proc_p(procv)) {
    out = mrb_ary_new(mrb);
    mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(rest), 0));
    return out;
  }
  const struct RProc *p = mrb_proc_ptr(procv);
  while (MRB_PROC_ALIAS_P(p) && p->upper) p = p->upper;
  if (MRB_PROC_CFUNC_P(p)) {
    if (MRB_PROC_CFUNC(p) == mrb_attr_writer) {
      out = mrb_ary_new(mrb);
      mrb_ary_push(mrb, out, param_pair(mrb, MRB_SYM(req), 0));
      return out;
    }
    uint32_t caspec = p->flags & MRB_PROC_CASPEC_MASK;
    if (caspec) return core_params(mrb, mrb_proc_decompress_caspec(caspec));
    if (MRB_PROC_NOARG_P(p)) return mrb_ary_new(mrb);
    return core_params(mrb, MRB_ARGS_ANY());
  }
  const mrb_irep *irep = p->body.irep;
  if (irep && irep_core_p(mrb, irep) && irep->ilen >= 4 && irep->iseq[0] == OP_ENTER) {
    return core_params(mrb, PEEK_W(irep->iseq + 1));
  }
  return orb_params(mrb, procv, 1);
}

/* ---- name arguments by #to_str (MRI's rb_check_id) ----
   mruby takes a Symbol or a String; MRI also what #to_str makes a String
   ("can't convert X into String (X#to_str gives Y)" when it does not), and
   for anything else "... is not a symbol nor a string". Converted in place,
   in the method's own frame (some read their caller's), before the C. */
static void name_arg(mrb_state *mrb, mrb_value *v) {
  if (mrb_symbol_p(*v) || mrb_string_p(*v)) return;
  if (!mrb_respond_to(mrb, *v, MRB_SYM(to_str))) {
    mrb_raisef(mrb, E_TYPE_ERROR, "%!v is not a symbol nor a string", *v);
  }
  mrb_value r = mrb_funcall_id(mrb, *v, MRB_SYM(to_str), 0);
  if (!mrb_string_p(r)) {
    mrb_raisef(mrb, E_TYPE_ERROR, "can't convert %T into String (%T#to_str gives %T)", *v, *v, r);
  }
  *v = r;
}

#define NAME_ALL 0xff
static const struct { int cls; const char *name; uint8_t mask; } name_defs[] = {
  /* cls 0: Module, 1: Kernel, 2: Binding */
  {0, "instance_method", 1}, {0, "public_instance_method", 1}, {0, "method_defined?", 1},
  {0, "public_method_defined?", 1}, {0, "private_method_defined?", 1}, {0, "protected_method_defined?", 1},
  {0, "module_function", NAME_ALL}, {0, "const_defined?", 1}, {0, "const_get", 1}, {0, "class_variable_get", 1},
  {0, "class_variable_set", 1}, {0, "class_variable_defined?", 1}, {0, "remove_class_variable", 1},
  {0, "undef_method", NAME_ALL}, {0, "remove_method", NAME_ALL}, {0, "private_constant", NAME_ALL},
  {0, "public_constant", NAME_ALL}, {0, "define_method", 1}, {0, "alias_method", 3}, {0, "remove_const", 1},
  {1, "instance_variable_get", 1}, {1, "instance_variable_set", 1}, {1, "instance_variable_defined?", 1},
  {1, "remove_instance_variable", 1}, {1, "method", 1}, {1, "singleton_method", 1},
  {2, "local_variable_get", 1}, {2, "local_variable_set", 1}, {2, "local_variable_defined?", 1},
};
#define NAME_N ((int)(sizeof(name_defs) / sizeof(name_defs[0])))
static mrb_func_t name_orig[32];

static mrb_value name_call(mrb_state *mrb, mrb_value self, int n) {
  mrb_callinfo *ci = mrb->c->ci;
  if (ci->n < 15) { /* not packed */
    mrb_int argc = mrb_get_argc(mrb);
    mrb_value *argv = (mrb_value *)mrb_get_argv(mrb);
    for (mrb_int i = 0; i < argc && i < 8; i++) {
      if (name_defs[n].mask & (1 << i)) name_arg(mrb, &argv[i]);
    }
  }
  return name_orig[n](mrb, self);
}
#define NW(n) static mrb_value name_w_##n(mrb_state *mrb, mrb_value self) { return name_call(mrb, self, n); }
NW(0) NW(1) NW(2) NW(3) NW(4) NW(5) NW(6) NW(7) NW(8) NW(9) NW(10) NW(11) NW(12) NW(13) NW(14) NW(15)
NW(16) NW(17) NW(18) NW(19) NW(20) NW(21) NW(22) NW(23) NW(24) NW(25) NW(26) NW(27) NW(28) NW(29) NW(30) NW(31)
static const mrb_func_t name_fns[32] = {
  name_w_0, name_w_1, name_w_2, name_w_3, name_w_4, name_w_5, name_w_6, name_w_7, name_w_8, name_w_9, name_w_10,
  name_w_11, name_w_12, name_w_13, name_w_14, name_w_15, name_w_16, name_w_17, name_w_18, name_w_19, name_w_20,
  name_w_21, name_w_22, name_w_23, name_w_24, name_w_25, name_w_26, name_w_27, name_w_28, name_w_29, name_w_30, name_w_31};

static void wrap_name_methods(mrb_state *mrb) {
  struct RClass *binding = mrb_class_defined_id(mrb, MRB_SYM(Binding)) ? mrb_class_get_id(mrb, MRB_SYM(Binding)) : NULL;
  for (int n = 0; n < NAME_N && n < 32; n++) {
    struct RClass *c = name_defs[n].cls == 0 ? mrb->module_class : name_defs[n].cls == 1 ? mrb->kernel_module : binding;
    if (c) wrap_cfunc(mrb, c, name_defs[n].name, name_fns[n], &name_orig[n]);
  }
}

/* Fiber#__oruby_status: mruby's status word (its inspect's), for
   29_fiber.rb, which parsed it out of #inspect with a regexp */
static mrb_value fiber_status_m(mrb_state *mrb, mrb_value self) {
  const struct RFiber *f = (const struct RFiber *)mrb_ptr(self);
  if (!f->cxt) return mrb_funcall_id(mrb, self, MRB_SYM(inspect), 0); /* its "uninitialized" error */
  const char *st;
  switch (f->cxt->status) {
  case MRB_FIBER_CREATED: st = "created"; break;
  case MRB_FIBER_RUNNING: st = "resumed"; break;
  case MRB_FIBER_RESUMED: st = "suspended by resuming"; break;
  case MRB_FIBER_SUSPENDED: case MRB_FIBER_TRANSFERRED: st = "suspended"; break;
  case MRB_FIBER_TERMINATED: st = "terminated"; break;
  default: st = "UNKNOWN STATUS (BUG)"; break;
  }
  return mrb_str_new_cstr(mrb, st);
}

/* $=, which MRI keeps only to warn: false, and an assignment ignored */
static void dollar_eq_warn(mrb_state *mrb, const char *msg) {
  if (mrb_nil_p(mrb_gv_get(mrb, mrb_intern_lit(mrb, "$VERBOSE")))) return;
  /* with the caller's file:line, as rb_warn (05_kernel.rb) */
  mrb_value k = mrb_obj_value(mrb->kernel_module);
  if (mrb_respond_to(mrb, k, MRB_SYM(__oruby_warn_here))) {
    mrb_funcall_id(mrb, k, MRB_SYM(__oruby_warn_here), 1, mrb_str_new_cstr(mrb, msg));
  }
}
/* $* and $<: ARGV and ARGF (read-only: prism.go rewrites assignments) */
static mrb_value const_or_nil(mrb_state *mrb, mrb_sym name) {
  if (!mrb_const_defined_at(mrb, mrb_obj_value(mrb->object_class), name)) return mrb_nil_value();
  return mrb_const_get(mrb, mrb_obj_value(mrb->object_class), name);
}
static mrb_value argv_gv_get(mrb_state *mrb) { return const_or_nil(mrb, MRB_SYM(ARGV)); }
static mrb_value argf_gv_get(mrb_state *mrb) { return const_or_nil(mrb, MRB_SYM(ARGF)); }
static void ro_gv_set(mrb_state *mrb, mrb_value v) { (void)mrb; (void)v; }

/* $? is thread-local, as MRI's: held by the running thread's (or fiber's)
   context object, the way $! is (go-backtrace.c errinfo_holder) */
static mrb_value last_status_holder(mrb_state *mrb) {
  if (mrb->c->fib && (mrb->c != mrb->root_c || !MRB_FLAG_TEST(mrb->c->fib, MRB_FL_FIBER_BORROWED)))
    return mrb_obj_value(mrb->c->fib);
  return mrb_obj_value(mrb->kernel_module);
}
static mrb_value last_status_get(mrb_state *mrb) {
  return mrb_iv_get(mrb, last_status_holder(mrb), MRB_SYM(__oruby_last_status));
}
static void last_status_set(mrb_state *mrb, mrb_value v) {
  mrb_iv_set(mrb, last_status_holder(mrb), MRB_SYM(__oruby_last_status), v);
}

static mrb_value dollar_eq_get(mrb_state *mrb) {
  dollar_eq_warn(mrb, "variable $= is no longer effective");
  return mrb_false_value();
}
static void dollar_eq_set(mrb_state *mrb, mrb_value v) {
  (void)v;
  dollar_eq_warn(mrb, "variable $= is no longer effective; ignored");
}

static void gv_init_checked(mrb_state *mrb) {
  static const char *pairs[][2] = {
    {"$/", "$/"}, {"$-0", "$/"}, {"$\\", "$\\"}, {"$,", "$,"}, {"$;", "$;"}, {"$-F", "$;"},
    {"$.", "$."}, {"$stdout", "$stdout"}, {"$>", "$stdout"}, {"$stderr", "$stderr"},
    {"$VERBOSE", "$VERBOSE"}, {"$-v", "$VERBOSE"}, {"$-w", "$VERBOSE"},
    {"$0", "$0"}, {"$PROGRAM_NAME", "$0"}, {NULL, NULL}
  };
  for (int i = 0; pairs[i][0]; i++) _mrb_gv_hook(mrb, pairs[i][0], pairs[i][1], 1);
  mrb_gv_define_virtual(mrb, mrb_intern_lit(mrb, "$~"), backref_get, backref_set);
  mrb_gv_define_virtual(mrb, mrb_intern_lit(mrb, "$_"), lastline_get, lastline_set);
  mrb_gv_define_virtual(mrb, mrb_intern_lit(mrb, "$="), dollar_eq_get, dollar_eq_set);
  mrb_gv_define_virtual(mrb, mrb_intern_lit(mrb, "$?"), last_status_get, last_status_set);
  mrb_gv_define_virtual(mrb, mrb_intern_lit(mrb, "$*"), argv_gv_get, ro_gv_set);
  mrb_gv_define_virtual(mrb, mrb_intern_lit(mrb, "$<"), argf_gv_get, ro_gv_set);
  _mrb_gv_hook(mrb, "$@", "$@", 3);
}

void _orb_bt_init(mrb_state *mrb);

/* the string has mruby's binary flag (BINARY, or a tagged encoding) */
int _orb_str_binary_p(mrb_value s) {
  return mrb_string_p(s) && RSTR_ENCODING(mrb_str_ptr(s)) == 1;
}

/* the string's coderange, computed once and cached (mruby's
   mrb_str_valid_encoding_p): 1 ASCII only, 2 valid UTF-8, 3 broken; 4 for
   mruby's binary flag (BINARY, or an encoding core_ext tags) */
int _orb_str_cr(mrb_state *mrb, mrb_value s) {
  if (!mrb_string_p(s)) return 0;
  struct RString *p = mrb_str_ptr(s);
  if (RSTR_BINARY_P(p)) return 4;
  mrb_str_valid_encoding_p(mrb, s);
  return (int)RSTR_CODERANGE(p);
}

/* mark an ASCII-only string US-ASCII (mruby's encoding flag 2) */
void _orb_str_set_us_ascii(mrb_value s) {
  if (mrb_string_p(s)) RSTR_ENCODING_SET(mrb_str_ptr(s), 2);
}

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

/* Integer#== and Float#== as MRI's num_equal: an operand that is not a
   number is asked (obj == num). They are C methods recorded as the builtins
   of the OP_EQ guard (as class.c's bop_arm records mruby's own), so the VM
   keeps comparing numbers inline and compiled code keeps its integer and
   float specialization; a later redefinition still disarms the guard. The
   VM sends == here only for an operand it does not compare itself. */
static mrb_func_t num_eq_orig[2];

static mrb_value
num_eq(mrb_state *mrb, mrb_value self, int i)
{
  mrb_value other = mrb_get_arg1(mrb);
  if (mrb_integer_p(other) || mrb_float_p(other) ||
      mrb_obj_is_kind_of(mrb, other, mrb->integer_class) ||
      mrb_obj_is_kind_of(mrb, other, mrb->float_class)) {
    return num_eq_orig[i](mrb, self);
  }
  if (!mrb_respond_to(mrb, other, MRB_OPSYM(eq))) return mrb_false_value();
  return mrb_bool_value(mrb_test(mrb_funcall_argv(mrb, other, MRB_OPSYM(eq), 1, &self)));
}

static mrb_value int_eq_m(mrb_state *mrb, mrb_value self) { return num_eq(mrb, self, 0); }
static mrb_value flo_eq_m(mrb_state *mrb, mrb_value self) { return num_eq(mrb, self, 1); }

static void
install_num_eq(mrb_state *mrb)
{
  struct RClass *classes[2] = {mrb->integer_class, mrb->float_class};
  mrb_func_t fns[2] = {int_eq_m, flo_eq_m};
  int slots[2] = {MRB_BOP_EQ, MRB_BOP_COUNT + MRB_BOP_EQ};
  for (int i = 0; i < 2; i++) {
    struct RClass *c = classes[i];
    mrb_method_t m = mrb_method_search_vm(mrb, &c, MRB_OPSYM(eq));
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || MRB_METHOD_CFUNC(m) == fns[i]) continue;
    num_eq_orig[i] = MRB_METHOD_CFUNC(m);
    mrb_define_method_id(mrb, classes[i], MRB_OPSYM(eq), fns[i], MRB_ARGS_REQ(1));
    c = classes[i];
    mrb->bop_builtin[slots[i]] = mrb_method_search_vm(mrb, &c, MRB_OPSYM(eq));
    mrb->bop_redefined &= ~(1u << slots[i]);
  }
}

/* US-ASCII without an instance variable: mruby's encoding field has two
   bits and uses 0 (UTF-8) and 1 (binary); 2 marks a string made US-ASCII
   (core_ext's String#encoding: US-ASCII while its bytes are ASCII, UTF-8
   once they are not, as MRI's compatibility gives). mruby reads it as its
   default encoding, which for ASCII bytes is the same. */
#define ORB_STR_ENCODING_US_ASCII 2

static mrb_value
str_enc_us_ascii_p(mrb_state *mrb, mrb_value self)
{
  return mrb_bool_value(RSTR_ENCODING(mrb_str_ptr(self)) == ORB_STR_ENCODING_US_ASCII);
}

static mrb_value
str_enc_us_ascii_set(mrb_state *mrb, mrb_value self)
{
  RSTR_ENCODING_SET(mrb_str_ptr(self), ORB_STR_ENCODING_US_ASCII);
  return self;
}

/* Integer#to_s / #inspect and Float#to_s / #inspect give US-ASCII strings,
   as MRI */
static mrb_func_t num_to_s_orig[4];

static mrb_value
us_ascii_str(mrb_state *mrb, mrb_value s)
{
  if (mrb_string_p(s)) RSTR_ENCODING_SET(mrb_str_ptr(s), ORB_STR_ENCODING_US_ASCII);
  return s;
}

static mrb_value int_to_s_w(mrb_state *mrb, mrb_value self) { return us_ascii_str(mrb, num_to_s_orig[0](mrb, self)); }
static mrb_value int_inspect_w(mrb_state *mrb, mrb_value self) { return us_ascii_str(mrb, num_to_s_orig[1](mrb, self)); }
static mrb_value flo_to_s_w(mrb_state *mrb, mrb_value self) { return us_ascii_str(mrb, num_to_s_orig[2](mrb, self)); }
static mrb_value flo_inspect_w(mrb_state *mrb, mrb_value self) { return us_ascii_str(mrb, num_to_s_orig[3](mrb, self)); }

static void
install_num_to_s(mrb_state *mrb)
{
  struct RClass *classes[4] = {mrb->integer_class, mrb->integer_class, mrb->float_class, mrb->float_class};
  mrb_sym mids[4] = {MRB_SYM(to_s), MRB_SYM(inspect), MRB_SYM(to_s), MRB_SYM(inspect)};
  mrb_func_t ws[4] = {int_to_s_w, int_inspect_w, flo_to_s_w, flo_inspect_w};
  /* inspect is an alias of to_s, as in MRI: only to_s is wrapped */
  for (int i = 0; i < 4; i += 2) {
    struct RClass *c = classes[i];
    mrb_method_t m = mrb_method_search_vm(mrb, &c, mids[i]);
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_CFUNC_P(m) || MRB_METHOD_CFUNC(m) == ws[i]) continue;
    num_to_s_orig[i] = MRB_METHOD_CFUNC(m);
    mrb_method_t w = m;  /* the original's flags, aspec and visibility */
    w.as.func = ws[i];
    mrb_define_method_raw(mrb, classes[i], mids[i], w);
    mrb_alias_method(mrb, classes[i], mids[i + 1], mids[i]);
  }
}

/* runs under coreMu (mruby.go): it fills the process-wide tables */
void _orb_srcrange_init(mrb_state *mrb);
void _orb_prism_serialize_init(mrb_state *mrb);
void _orb_trace_init(mrb_state *mrb);

void _orb_fast_capture(mrb_state *mrb);
void _orb_fast_init(mrb_state *mrb);

/* The loader records each active autoload until the file has executed.
   The native marker covers lookups by the loading thread. The extra record
   also covers lookups by other threads after the constant has been assigned. */
mrb_bool mrb_autoload_p(mrb_value v);
struct orb_autoload { mrb_bool loading; mrb_int len; char path[]; }; /* mruby's struct autoload */
static mrb_func_t const_missing_orig;

static mrb_value autoload_marks(mrb_state *mrb, mrb_value mod) {
  return mrb_iv_get(mrb, mod, MRB_SYM(__oruby_autoload_marks));
}

/* An active record is [marker, real file, source location]. A completed
   registration without a definition only retains [marker]. */
static mrb_value autoload_active(mrb_state *mrb, mrb_value mod, mrb_value name) {
  mrb_value marks = autoload_marks(mrb, mod);
  if (!mrb_hash_p(marks)) return mrb_nil_value();
  mrb_value mark = mrb_hash_get(mrb, marks, name);
  return mrb_array_p(mark) && RARRAY_LEN(mark) == 3 ? mark : mrb_nil_value();
}

static mrb_value autoload_lookup(mrb_state *mrb, mrb_value mod, mrb_sym sym, mrb_bool inherit) {
  mrb_value name = mrb_symbol_value(sym);
  for (struct RClass *c = mrb_class_ptr(mod); c; c = inherit ? c->super : NULL) {
    if (MRB_FLAG_TEST(c, MRB_FL_CLASS_IS_PREPENDED)) continue;
    mrb_value owner = mrb_obj_value(c->tt == MRB_TT_ICLASS ? c->c : c);
    mrb_value mark = autoload_active(mrb, owner, name);
    if (!mrb_nil_p(mark)) return mark;
    mrb_value slot = mrb_iv_get(mrb, owner, sym);
    if (mrb_iv_defined(mrb, owner, sym) &&
        (!mrb_autoload_p(slot) || !((struct orb_autoload *)DATA_PTR(slot))->loading)) break;
  }
  return mrb_nil_value();
}

static mrb_value autoload_loader_call(mrb_state *mrb, mrb_sym method, mrb_value mark) {
  if (mrb_nil_p(mark)) return mrb_false_value();
  return mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), method, 1, RARRAY_PTR(mark)[1]);
}

static mrb_bool autoload_other_thread(mrb_state *mrb, mrb_value mark) {
  return mrb_test(autoload_loader_call(mrb, MRB_SYM_Q(__oruby_autoload_other_thread), mark));
}

/* An autoload may define the name in an inner lexical scope the VM already
   searched. Recheck the lexical read without loading any more files. */
static mrb_value retry_lexical_const(mrb_state *mrb, mrb_sym sym) {
  mrb_callinfo *ci = mrb->c->ci - 1;
  if (ci < mrb->c->cibase || !ci->proc || MRB_PROC_CFUNC_P(ci->proc)) return mrb_undef_value();
  const mrb_irep *irep = ci->proc->body.irep;
  if (!ci->pc || ci->pc <= irep->iseq || ci->pc > irep->iseq + irep->ilen) return mrb_undef_value();
  for (const mrb_code *pc = irep->iseq; pc < ci->pc;) {
    struct mrb_insn_data insn = mrb_decode_insn(pc);
    if (insn.next == ci->pc && insn.insn == OP_GETCONST && insn.b < irep->slen &&
        irep->syms[insn.b] == sym) {
      mrb_value value = mrb_vm_const_get_noraise(mrb, ci, sym, FALSE);
      if (!mrb_autoload_p(value)) return value;
    }
    pc = insn.next;
  }
  return mrb_undef_value();
}

static mrb_value const_missing_w(mrb_state *mrb, mrb_value mod) {
  /* mruby's Mod::X raises "private constant" only while const_missing is
     its own; this wrapper says so the same way */
  if ((mrb_class_p(mod) || mrb_module_p(mod)) && mrb_get_argc(mrb) == 1 && mrb_symbol_p(mrb_get_argv(mrb)[0])) {
    mrb_sym sym = mrb_symbol(mrb_get_argv(mrb)[0]);
    for (struct RClass *c = mrb_class_ptr(mod); c; c = c->super) {
      struct RClass *owner = c->tt == MRB_TT_ICLASS ? c->c : c;
      if (!mrb_iv_defined(mrb, mrb_obj_value(owner), sym)) continue;
      if (mrb_const_flags(mrb, owner, sym) & MRB_CONST_PRIVATE) {
        mrb_value e = mrb_exc_new_str(mrb, E_NAME_ERROR, mrb_format(mrb, "private constant %C::%n referenced", owner, sym));
        mrb_iv_set(mrb, e, MRB_IVSYM(name), mrb_symbol_value(sym));
        mrb_iv_set(mrb, e, MRB_IVSYM(receiver), mrb_obj_value(owner));
        mrb_exc_raise(mrb, e);
      }
      break;
    }
  }
  if (mrb_class_p(mod) && mrb_get_argc(mrb) == 1 && mrb_symbol_p(mrb_get_argv(mrb)[0])) {
    /* Cls::X goes on into the modules included in Object (MRI skips only
       Object itself) */
    mrb_sym sym = mrb_symbol(mrb_get_argv(mrb)[0]);
    for (struct RClass *c = mrb->object_class->super; c; c = c->super) {
      if (c->tt != MRB_TT_ICLASS) continue;
      if (mrb_iv_defined(mrb, mrb_obj_value(c->c), sym)) return mrb_const_get(mrb, mrb_obj_value(c->c), sym);
    }
  }
  if ((mrb_class_p(mod) || mrb_module_p(mod)) && mod_namespace_p(mod) && !mod_perm_p(mrb, mod) &&
      mrb_get_argc(mrb) == 1 && mrb_symbol_p(mrb_get_argv(mrb)[0])) {
    /* an anonymous module's own #name, else #inspect, names it (MRI's
       rb_const_missing message) */
    mrb_sym sym = mrb_symbol(mrb_get_argv(mrb)[0]);
    struct RClass *sc = mrb_class(mrb, mod), *o1 = sc, *o2 = sc;
    mrb_method_t mn = mrb_method_search_vm(mrb, &o1, MRB_SYM(name)), mi = mrb_method_search_vm(mrb, &o2, MRB_SYM(inspect));
    if ((!MRB_METHOD_UNDEF_P(mn) && o1->tt == MRB_TT_SCLASS) || (!MRB_METHOD_UNDEF_P(mi) && o2->tt == MRB_TT_SCLASS)) {
      mrb_value desc = mrb_funcall_id(mrb, mod, MRB_SYM(name), 0);
      if (!mrb_string_p(desc)) desc = mrb_funcall_id(mrb, mod, MRB_SYM(inspect), 0);
      mrb_value e = mrb_exc_new_str(mrb, E_NAME_ERROR, mrb_format(mrb, "uninitialized constant %v::%n", desc, sym));
      mrb_iv_set(mrb, e, MRB_IVSYM(name), mrb_symbol_value(sym));
      mrb_iv_set(mrb, e, MRB_IVSYM(receiver), mod);
      mrb_exc_raise(mrb, e);
    }
  }
  if ((mrb_class_p(mod) || mrb_module_p(mod)) && mrb_get_argc(mrb) == 1 &&
      mrb_symbol_p(mrb_get_argv(mrb)[0])) {
    mrb_sym sym = mrb_symbol(mrb_get_argv(mrb)[0]);
    mrb_value mark = autoload_lookup(mrb, mod, sym, TRUE);
    if (mrb_test(autoload_loader_call(mrb, MRB_SYM(__oruby_autoload_wait), mark))) {
      return mrb_const_get(mrb, mod, sym);
    }
    mrb_value v = retry_lexical_const(mrb, sym);
    if (!mrb_undef_p(v)) return v;
  }
  return const_missing_orig(mrb, mod);
}

/* Container inspection appends each result with its encoding preserved. */
struct inspect_buffer {
  mrb_value str;
  mrb_value kernel;
  mrb_bool normalize;
  mrb_bool ascii;
  mrb_bool tagged;
};

static mrb_bool inspect_ascii(mrb_value str) {
  if (RSTR_CODERANGE(mrb_str_ptr(str)) == MRB_STR_CODERANGE_7BIT) return TRUE;
  const unsigned char *p = (const unsigned char *)RSTRING_PTR(str);
  for (mrb_int i = 0; i < RSTRING_LEN(str); i++) {
    if (p[i] >= 0x80) return FALSE;
  }
  return TRUE;
}

/* A fragment already in US-ASCII never needs encoding normalization.
   Other untagged ASCII fragments only need it for a wide target encoding. */
static mrb_bool inspect_needs_normalize(mrb_state *mrb, mrb_value part, mrb_bool ascii) {
  if (!ascii) return TRUE;
  int enc = RSTR_ENCODING(mrb_str_ptr(part));
  if (enc == ORB_STR_ENCODING_US_ASCII) return FALSE;
  if (enc == MRB_STR_ENCODING_BINARY && !mrb_nil_p(mrb_iv_get(mrb, part, MRB_SYM(__encoding)))) return TRUE;

  mrb_value encoding = mrb_obj_value(mrb_class_get_id(mrb, MRB_SYM(Encoding)));
  mrb_value target = mrb_iv_get(mrb, encoding, MRB_IVSYM(default_internal));
  if (mrb_nil_p(target)) target = mrb_iv_get(mrb, encoding, MRB_IVSYM(default_external));
  return mrb_nil_p(target) || !mrb_test(mrb_iv_get(mrb, target, MRB_IVSYM(ascii_compatible)));
}

static void inspect_append(mrb_state *mrb, struct inspect_buffer *b, mrb_value part,
                           mrb_bool first, mrb_bool normalize) {
  mrb_bool ascii = inspect_ascii(part);
  if (normalize && b->normalize && inspect_needs_normalize(mrb, part, ascii)) {
    part = mrb_funcall_id(mrb, b->kernel, MRB_SYM(__oruby_inspect_part), 1, part);
    ascii = inspect_ascii(part);
  }
  int current = RSTR_ENCODING(mrb_str_ptr(b->str));
  int encoding = RSTR_ENCODING(mrb_str_ptr(part));
  if ((encoding != current || encoding == MRB_STR_ENCODING_BINARY) &&
      (first || (b->ascii && (!ascii || current == ORB_STR_ENCODING_US_ASCII)))) {
    mrb_sym sym = MRB_SYM(__encoding);
    mrb_value tag = encoding == MRB_STR_ENCODING_BINARY ? mrb_iv_get(mrb, part, sym) : mrb_nil_value();
    RSTR_ENCODING_SET(mrb_str_ptr(b->str), encoding);
    if (!mrb_nil_p(tag)) mrb_iv_set(mrb, b->str, sym, tag);
    else if (b->tagged) mrb_iv_remove(mrb, b->str, sym);
    b->tagged = !mrb_nil_p(tag);
  }
  mrb_str_cat(mrb, b->str, RSTRING_PTR(part), RSTRING_LEN(part));
  b->ascii = b->ascii && ascii;
}

static struct inspect_buffer inspect_buffer_new(mrb_state *mrb, mrb_value str) {
  mrb_value kernel = mrb_obj_value(mrb->kernel_module);
  struct inspect_buffer b = {
    us_ascii_str(mrb, str), kernel,
    mrb_respond_to(mrb, kernel, MRB_SYM(__oruby_inspect_part)), TRUE, FALSE
  };
  return b;
}

static mrb_value array_inspect_w(mrb_state *mrb, mrb_value self) {
  mrb->c->ci->mid = MRB_SYM(inspect);
  if (MRB_RECURSIVE_UNARY_P(mrb, MRB_SYM(inspect), self))
    return us_ascii_str(mrb, mrb_str_new_lit(mrb, "[...]"));

  struct inspect_buffer b = inspect_buffer_new(mrb, mrb_str_new_lit(mrb, "["));
  mrb_str_ptr(b.str)->c = NULL; /* hide the buffer from ObjectSpace.each_object */
  int ai = mrb_gc_arena_save(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value part = mrb_inspect(mrb, RARRAY_PTR(self)[i]);
    if (i > 0) mrb_str_cat_lit(mrb, b.str, ", ");
    inspect_append(mrb, &b, part, i == 0, TRUE);
    mrb_gc_arena_restore(mrb, ai);
  }
  mrb_str_cat_lit(mrb, b.str, "]");
  mrb_str_ptr(b.str)->c = mrb->string_class;
  return b.str;
}

/* Symbol keys without a plain name are written with quotes, e.g. "a b":. */
static mrb_bool
sym_key_needs_quote(mrb_state *mrb, mrb_sym sym)
{
  mrb_int len;
  const char *s = mrb_sym_name_len(mrb, sym, &len);
  if (!s || len == 0) return TRUE;
  const char *d = mrb_sym_dump(mrb, sym);
  if (!d || d[0] == '"') return TRUE;
  if (s[0] == '@' || s[0] == '$' || s[0] == '!') return TRUE;
  return strchr("+-*/`%^&|]<=>~@", s[len - 1]) != NULL;
}

static mrb_bool plain_symbol_key(const char *name, mrb_int len) {
  if (len == 0 || (!ISALPHA((unsigned char)name[0]) && name[0] != '_')) return FALSE;
  for (mrb_int i = 1; i < len; i++) {
    if (!ISALNUM((unsigned char)name[i]) && name[i] != '_') return FALSE;
  }
  return TRUE;
}

static int hash_inspect_entry(mrb_state *mrb, mrb_value key, mrb_value value, void *data) {
  struct inspect_buffer *b = data;
  int ai = mrb_gc_arena_save(mrb);
  mrb_gc_protect(mrb, value); /* key.inspect can remove the entry */
  mrb_bool first = RSTRING_LEN(b->str) == 1;
  if (!first) mrb_str_cat_lit(mrb, b->str, ", ");
  if (mrb_symbol_p(key)) {
    mrb_sym sym = mrb_symbol(key);
    mrb_int len = 0;
    const char *bytes = mrb_sym_name_len(mrb, sym, &len);
    if (plain_symbol_key(bytes, len)) {
      mrb_str_cat(mrb, b->str, bytes, len);
    }
    else {
      mrb_value name = mrb_sym_str(mrb, sym);
      mrb_bool ascii = inspect_ascii(name);
      if (ascii) us_ascii_str(mrb, name);
      else {
        mrb_value tags = mrb_iv_get(mrb, mrb_obj_value(mrb->symbol_class), MRB_IVSYM(__sym_encodings));
        if (mrb_hash_p(tags)) {
          mrb_value tag = mrb_hash_get(mrb, tags, key);
          if (!mrb_nil_p(tag)) {
            RSTR_ENCODING_SET(mrb_str_ptr(name), MRB_STR_ENCODING_BINARY);
            mrb_iv_set(mrb, name, MRB_SYM(__encoding), tag);
          }
        }
      }
      mrb_bool quote = ascii || !b->normalize ? sym_key_needs_quote(mrb, sym) :
        mrb_test(mrb_funcall_id(mrb, b->kernel, MRB_SYM_Q(__oruby_inspect_symbol_quote), 1, name));
      mrb_value part = quote ? mrb_str_inspect(mrb, name) : name;
      inspect_append(mrb, b, part, first, quote);
    }
    mrb_str_cat_lit(mrb, b->str, ": ");
  }
  else {
    inspect_append(mrb, b, mrb_inspect(mrb, key), first, TRUE);
    mrb_str_cat_lit(mrb, b->str, " => ");
  }
  inspect_append(mrb, b, mrb_inspect(mrb, value), FALSE, TRUE);
  mrb_gc_arena_restore(mrb, ai);
  return 0;
}

static mrb_value hash_inspect_w(mrb_state *mrb, mrb_value self) {
  mrb->c->ci->mid = MRB_SYM(inspect);
  if (MRB_RECURSIVE_UNARY_P(mrb, MRB_SYM(inspect), self))
    return us_ascii_str(mrb, mrb_str_new_lit(mrb, "{...}"));

  struct inspect_buffer b = inspect_buffer_new(mrb, mrb_str_new_lit(mrb, "{"));
  mrb_str_ptr(b.str)->c = NULL;
  mrb_hash_foreach(mrb, mrb_hash_ptr(self), hash_inspect_entry, &b);
  mrb_str_cat_lit(mrb, b.str, "}");
  mrb_str_ptr(b.str)->c = mrb->string_class;
  return b.str;
}

/* Range: MRI freezes a Range made by Range.new (not a subclass's), and a
   dup is not frozen; mruby's initialize and initialize_copy freeze both.
   (A literal is frozen in MRI and not in mruby: that is mruby's
   mrb_range_new, out of reach here.) */
static mrb_func_t range_init_orig, range_init_copy_orig;

static mrb_value range_init_w(mrb_state *mrb, mrb_value self) {
  /* an initialized Range is frozen in MRI (mruby says "called twice") */
  if (RANGE_INITIALIZED_P(mrb_range_raw_ptr(self)) && mrb_obj_class(mrb, self) == mrb->range_class)
    mrb_raisef(mrb, E_FROZEN_ERROR, "can't modify frozen Range: %v", self);
  mrb_value r = range_init_orig(mrb, self);
  if (mrb_obj_class(mrb, self) != mrb->range_class) mrb_obj_ptr(self)->frozen = 0;
  return r;
}

static mrb_value range_init_copy_w(mrb_state *mrb, mrb_value self) {
  mrb_value r = range_init_copy_orig(mrb, self);
  mrb_obj_ptr(self)->frozen = 0;  /* Kernel#clone refreezes a frozen source */
  return r;
}


/* FrozenError: MRI's #receiver and "can't modify frozen X: inspect" message.
   mruby's frozen check (error.c frozen_error) formats only the class name and
   keeps no object, so the raise looks for it again: the object an OP_DEF /
   OP_TDEF / OP_SDEF was defining on (a frozen singleton class stands for its
   attached object), else self of the top frames. It must be frozen and of the
   class the message names. The message is completed by #to_s, lazily, since
   #inspect runs Ruby code. */
static mrb_value frz_candidate(mrb_state *mrb, mrb_value v) {
  if (mrb_type(v) == MRB_TT_SCLASS)
    return mrb_iv_get(mrb, v, MRB_SYM(__attached__));
  return v;
}

static mrb_bool frz_match(mrb_state *mrb, mrb_value v, const char *cname, mrb_int clen) {
  if (!mrb_immediate_p(v) && !mrb_frozen_p(mrb_basic_ptr(v))) return FALSE;
  const char *n = mrb_obj_classname(mrb, v);
  return n && (mrb_int)strlen(n) == clen && memcmp(n, cname, clen) == 0;
}

static mrb_bool mesg_prefix(mrb_value mesg, const char *pre) {
  size_t n = strlen(pre);
  return mrb_string_p(mesg) && (size_t)RSTRING_LEN(mesg) >= n && memcmp(RSTRING_PTR(mesg), pre, n) == 0;
}

static mrb_bool mesg_suffix(mrb_value mesg, const char *suf) {
  size_t n = strlen(suf);
  return mrb_string_p(mesg) && (size_t)RSTRING_LEN(mesg) >= n &&
         memcmp(RSTRING_PTR(mesg) + RSTRING_LEN(mesg) - n, suf, n) == 0;
}

/* mruby's OP_ENTER reports only the required count ("expected 2"); MRI
   gives the range of the method's parameters ("expected 2+", "1..2"). */
mrb_value orb_params_c(mrb_state *mrb, mrb_value procv);
static void argnum_fix(mrb_state *mrb, struct RObject *exc) {
  mrb_callinfo *ci = mrb->c->ci;
  if (ci->mid == MRB_SYM(instance_eval) || ci->mid == MRB_SYM(module_eval) || ci->mid == MRB_SYM(class_eval)) {
    /* the string form takes 1..3 (mruby's aspec says 0..3, for the block) */
    mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
    if (mesg_suffix(mesg, "expected 0..3)")) {
      mrb_value s = mrb_str_new(mrb, RSTRING_PTR(mesg), RSTRING_LEN(mesg) - 5);
      mrb_str_cat_lit(mrb, s, "1..3)");
      mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
    }
    return;
  }
  if (!ci->proc || MRB_PROC_CFUNC_P(ci->proc) || !ci->pc) return;
  const mrb_irep *irep = ci->proc->body.irep;
  if (ci->pc != irep->iseq + 4 || irep->iseq[0] != OP_ENTER) return;
  mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
  if (!mrb_string_p(mesg)) return;
  int given, expected;
  char tail;
  if (sscanf(RSTRING_CSTR(mrb, mesg), "wrong number of arguments (given %d, expected %d%c", &given, &expected, &tail) != 3 || tail != ')') return;
  uint32_t a = PEEK_W(irep->iseq + 1);
  int m1 = MRB_ASPEC_REQ(a), o = MRB_ASPEC_OPT(a), r = MRB_ASPEC_REST(a), m2 = MRB_ASPEC_POST(a);
  if (MRB_ASPEC_KDICT(a) && ci->kw) {
    /* **nil given keywords: MRI's error, whatever the count */
    mrb_value params = orb_params_c(mrb, mrb_obj_value((struct RProc*)ci->proc));
    for (mrb_int i = 0; i < RARRAY_LEN(params); i++) {
      mrb_value pr = RARRAY_PTR(params)[i];
      if (mrb_array_p(pr) && RARRAY_LEN(pr) >= 1 && mrb_symbol(RARRAY_PTR(pr)[0]) == MRB_SYM(nokey)) {
        mrb_exc_mesg_set(mrb, (struct RException*)exc, mrb_str_new_lit(mrb, "no keywords accepted"));
        return;
      }
    }
  }
  /* keywords to a method that takes none are one more positional argument */
  mrb_bool kw_counted = FALSE;
  if (!MRB_ASPEC_KEY(a) && !MRB_ASPEC_KDICT(a) && ci->kw && ci->n < 15 && given == ci->n && ci->n > 0) {
    mrb_value kd = ci->stack[ci->n + 1];
    if (mrb_hash_p(kd) && !mrb_hash_empty_p(mrb, kd)) { given++; kw_counted = TRUE; }
  }
  if (m1 + m2 != expected) return;
  if (o == 0 && r == 0) {
    if (kw_counted)
      mrb_exc_mesg_set(mrb, (struct RException*)exc, mrb_format(mrb, "wrong number of arguments (given %d, expected %d)", given, expected));
    return;
  }
  mrb_value s = r ? mrb_format(mrb, "wrong number of arguments (given %d, expected %d+)", given, m1 + m2)
                  : mrb_format(mrb, "wrong number of arguments (given %d, expected %d..%d)", given, m1 + m2, m1 + o + m2);
  mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
}

/* TypeErrors of `class X < S` / `module X` (OP_CLASS / OP_MODULE) in MRI's
   words: "superclass mismatch for class X", "superclass must be an instance
   of Class (given an instance of K)", "can't make subclass of singleton
   class", "X is not a class\nFILE:LINE: previous definition of X was here" */
static void classdef_fix(mrb_state *mrb, struct RObject *exc) {
  mrb_callinfo *ci = mrb->c->ci;
  if (!ci->proc || MRB_PROC_CFUNC_P(ci->proc) || !ci->pc || !ci->stack) return;
  const mrb_irep *irep = ci->proc->body.irep;
  const mrb_code *pc = ci->pc;
  if (pc - irep->iseq < 3 || (pc[-3] != OP_CLASS && pc[-3] != OP_MODULE)) return;
  mrb_bool is_class = pc[-3] == OP_CLASS;
  int a = pc[-2], b = pc[-1];
  if (b >= irep->slen) return;
  mrb_sym id = irep->syms[b];
  mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
  mrb_value s = mrb_nil_value();
  if (is_class && mesg_prefix(mesg, "superclass mismatch for ")) {
    s = mrb_format(mrb, "superclass mismatch for class %n", id);
  }
  else if (is_class && mesg_prefix(mesg, "superclass must be a Class (")) {
    mrb_value super = ci->stack[a+1];
    if (mrb_type(super) == MRB_TT_SCLASS)
      s = mrb_str_new_lit(mrb, "can't make subclass of singleton class");
    else
      s = mrb_format(mrb, "superclass must be an instance of Class (given an instance of %C)", mrb_obj_class(mrb, super));
  }
  else if (mesg_suffix(mesg, is_class ? " is not a class" : " is not a module")) {
    s = mrb_format(mrb, is_class ? "%n is not a class" : "%n is not a module", id);
    mrb_value base = ci->stack[a];
    if (mrb_nil_p(base)) {
      struct RClass *cref = mrb_vm_cref_class(mrb, ci);
      base = mrb_obj_value(cref ? cref : mrb->object_class);
    }
    if (mrb_class_p(base) || mrb_module_p(base) || mrb_sclass_p(base)) {
      mrb_value h = mrb_iv_get(mrb, base, MRB_SYM(__oruby_csl));
      mrb_value loc = mrb_hash_p(h) ? mrb_hash_get(mrb, h, mrb_symbol_value(id)) : mrb_nil_value();
      if (mrb_array_p(loc) && RARRAY_LEN(loc) >= 2 && mrb_string_p(RARRAY_PTR(loc)[0]))
        s = mrb_format(mrb, "%v\n%v:%v: previous definition of %n was here", s, RARRAY_PTR(loc)[0], RARRAY_PTR(loc)[1], id);
    }
  }
  if (!mrb_nil_p(s)) mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
}

/* SyntaxError "file F line N: msg" (mruby's compiler) as MRI's "F:N: msg",
   the line counted from an eval's line argument */
static void syntax_error_fix(mrb_state *mrb, struct RObject *exc, mrb_value mesg) {
  if (!mesg_prefix(mesg, "file ")) return;
  const char *p = RSTRING_PTR(mesg), *e = p + RSTRING_LEN(mesg);
  const char *sep = NULL;
  for (const char *q = p + 5; q + 6 <= e; q++) {
    if (memcmp(q, " line ", 6) == 0) { sep = q; break; }
  }
  if (!sep) return;
  const char *num = sep + 6, *q = num;
  long line = 0;
  while (q < e && *q >= '0' && *q <= '9') line = line * 10 + (*q++ - '0');
  if (q == num || q + 2 > e || q[0] != ':' || q[1] != ' ') return;
  /* the eval (or instance_eval/class_eval/module_eval) compiling it */
  for (mrb_callinfo *ci = mrb->c->ci; ci >= mrb->c->cibase && ci > mrb->c->ci - 4; ci--) {
    mrb_sym mid = ci->mid;
    int idx = -1;
    if (mid == MRB_SYM(eval)) {
      mrb_bool binding = mrb_class_defined_id(mrb, MRB_SYM(Binding)) && ci->stack &&
                         mrb_obj_class(mrb, ci->stack[0]) == mrb_class_get_id(mrb, MRB_SYM(Binding));
      idx = binding ? 2 : 3;
    }
    else if (mid == MRB_SYM(instance_eval) || mid == MRB_SYM(module_eval) || mid == MRB_SYM(class_eval)) idx = 2;
    if (idx < 0 || !ci->stack) continue;
    const mrb_value *argv = ci->stack + 1;
    mrb_int argc = ci->n;
    if (argc == 15 && mrb_array_p(ci->stack[1])) { argv = RARRAY_PTR(ci->stack[1]); argc = RARRAY_LEN(ci->stack[1]); }
    if (argc > idx && mrb_integer_p(argv[idx])) line += (long)mrb_integer(argv[idx]) - 1;
    break;
  }
  mrb_value s = mrb_str_new(mrb, p + 5, sep - (p + 5));
  mrb_iv_set(mrb, mrb_obj_value(exc), MRB_IVSYM(path), mrb_str_dup(mrb, s));  /* SyntaxError#path */
  mrb_str_cat_str(mrb, s, mrb_format(mrb, ":%d: ", (mrb_int)line));
  mrb_str_cat(mrb, s, q + 2, e - (q + 2));
  mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
}

/* a Hash key as Kernel#inspect shows it, without calling Ruby (the hook
   runs with mrb->exc set) */
static void kw_key_inspect(mrb_state *mrb, mrb_value s, mrb_value k) {
  if (mrb_symbol_p(k)) {
    mrb_int n;
    const char *p = mrb_sym_name_len(mrb, mrb_symbol(k), &n);
    mrb_bool plain = n > 0 && (ISALPHA(p[0]) || p[0] == '_' || ((unsigned char)p[0]) >= 0x80);
    for (mrb_int i = 1; plain && i < n; i++) {
      unsigned char ch = (unsigned char)p[i];
      if (!(ISALNUM(ch) || ch == '_' || ch >= 0x80 || (i == n - 1 && (ch == '?' || ch == '!')))) plain = FALSE;
    }
    mrb_str_cat_cstr(mrb, s, plain ? ":" : ":\"");
    mrb_str_cat(mrb, s, p, n);
    if (!plain) mrb_str_cat_lit(mrb, s, "\"");
  }
  else if (mrb_nil_p(k)) mrb_str_cat_lit(mrb, s, "nil");
  else if (mrb_true_p(k)) mrb_str_cat_lit(mrb, s, "true");
  else if (mrb_false_p(k)) mrb_str_cat_lit(mrb, s, "false");
  else if (mrb_integer_p(k)) mrb_str_cat_str(mrb, s, mrb_integer_to_str(mrb, k, 10));
  else if (mrb_string_p(k)) { mrb_str_cat_lit(mrb, s, "\""); mrb_str_cat_str(mrb, s, k); mrb_str_cat_lit(mrb, s, "\""); }
  else mrb_str_cat_str(mrb, s, mrb_any_to_s(mrb, k));
}

mrb_value orb_params_c(mrb_state *mrb, mrb_value procv);

/* keyword errors as MRI's: every missing (required) keyword or every
   unknown one, each shown as Kernel#inspect does ("missing keywords: :a, :b") */
static void kw_error_fix(mrb_state *mrb, struct RObject *exc, mrb_bool missing) {
  mrb_callinfo *ci = mrb->c->ci;
  if (!ci->proc || MRB_PROC_CFUNC_P(ci->proc) || !ci->pc || !ci->stack) return;
  const mrb_irep *irep = ci->proc->body.irep;
  if (irep->ilen < 4 || irep->iseq[0] != OP_ENTER) return;
  uint32_t aspec = PEEK_W(irep->iseq + 1);
  mrb_int kidx = ci->n < 15 ? ci->n + 1 : MRB_ASPEC_REQ(aspec) + MRB_ASPEC_OPT(aspec) + MRB_ASPEC_REST(aspec) + MRB_ASPEC_POST(aspec) + 1;
  mrb_value kdict = ci->stack[kidx];
  mrb_value names = mrb_ary_new(mrb);
  if (missing) {
    const mrb_code *pc = ci->pc;
    if (pc - irep->iseq < 3 || pc[-3] != OP_KARG || pc[-1] >= irep->slen) return;
    mrb_sym cur = irep->syms[pc[-1]];
    mrb_value params = orb_params_c(mrb, mrb_obj_value((struct RProc*)ci->proc));
    mrb_bool from = FALSE;
    for (mrb_int i = 0; i < RARRAY_LEN(params); i++) {
      mrb_value pr = RARRAY_PTR(params)[i];
      if (!mrb_array_p(pr) || RARRAY_LEN(pr) < 2 || mrb_symbol(RARRAY_PTR(pr)[0]) != MRB_SYM(keyreq)) continue;
      mrb_value nm = RARRAY_PTR(pr)[1];
      if (mrb_symbol(nm) == cur) from = TRUE;
      if (!from) continue;
      if (mrb_hash_p(kdict) && mrb_hash_key_p(mrb, kdict, nm)) continue;
      mrb_ary_push(mrb, names, nm);
    }
    if (RARRAY_LEN(names) == 0) mrb_ary_push(mrb, names, mrb_symbol_value(cur));
  }
  else {
    if (!mrb_hash_p(kdict)) return;
    names = mrb_hash_keys(mrb, kdict);
  }
  mrb_int n = RARRAY_LEN(names);
  if (n == 0) return;
  mrb_value s = mrb_str_new_cstr(mrb, missing ? "missing keyword" : "unknown keyword");
  mrb_str_cat_cstr(mrb, s, n > 1 ? "s: " : ": ");
  for (mrb_int i = 0; i < n; i++) {
    if (i) mrb_str_cat_lit(mrb, s, ", ");
    kw_key_inspect(mrb, s, RARRAY_PTR(names)[i]);
  }
  mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
}

void _orb_frozen_fix(mrb_state *mrb, struct RObject *exc) {
  if (exc->c == E_NOMETHOD_ERROR) {
    /* catch without a block: mruby's (bytecode, so no C pre-check) calls
       nil; MRI's LocalJumpError */
    mrb_callinfo *ci = mrb->c->ci;
    if (ci > mrb->c->cibase && ci->mid != MRB_SYM(catch)) ci--;  /* the method_missing frame */
    if (ci->mid == MRB_SYM(catch) && ci->proc && !MRB_PROC_CFUNC_P(ci->proc) && !ci->blk) {
      mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
      if (mesg_prefix(mesg, "undefined method 'call' for NilClass")) {
        exc->c = E_LOCALJUMP_ERROR;
        mrb_exc_mesg_set(mrb, (struct RException*)exc, mrb_str_new_lit(mrb, "no block given"));
        return;
      }
    }
  }
  if (exc->c == E_LOCALJUMP_ERROR) {
    /* LocalJumpError#exit_value: the value of the return / break the VM
       refused, read from the instruction's register (OP_RETURN_BLK a,
       OP_BREAK a) */
    mrb_callinfo *ci = mrb->c->ci;
    if (ci->proc && !MRB_PROC_CFUNC_P(ci->proc) && ci->pc && ci->stack) {
      const mrb_code *pc = ci->pc;
      const mrb_irep *irep = ci->proc->body.irep;
      if (pc - irep->iseq >= 2 && (pc[-2] == OP_RETURN_BLK || pc[-2] == OP_BREAK) && pc[-1] < irep->nregs) {
        mrb_obj_iv_set(mrb, exc, MRB_IVSYM(exit_value), ci->stack[pc[-1]]);
        if (pc[-2] == OP_BREAK) mrb_obj_iv_set(mrb, exc, MRB_IVSYM(reason), mrb_symbol_value(MRB_SYM(break)));
      }
    }
    return;
  }
  if (exc->c == E_ARGUMENT_ERROR) {
    /* an @ivar set on an immediate (instance_eval { @a = 1 } on nil): MRI's
       FrozenError, the receiver found as for one (below) */
    mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
    mrb_callinfo *ci = mrb->c->ci;
    if (mrb_string_p(mesg) && RSTRING_LEN(mesg) == 28 && memcmp(RSTRING_PTR(mesg), "cannot set instance variable", 28) == 0 &&
        ci->stack && mrb_immediate_p(ci->stack[0])) {
      exc->c = mrb_exc_get_id(mrb, MRB_ERROR_SYM(FrozenError));
      mrb_exc_mesg_set(mrb, (struct RException*)exc, mrb_format(mrb, "can't modify frozen %C", mrb_obj_class(mrb, ci->stack[0])));
    }
    else if (mesg_prefix(mesg, "missing keyword: ") || mesg_prefix(mesg, "unknown keyword: ")) {
      kw_error_fix(mrb, exc, mesg_prefix(mesg, "missing"));
      return;
    }
    else {
      argnum_fix(mrb, exc);
      return;
    }
  }
  if (exc->c == E_TYPE_ERROR) {
    mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
    /* &obj without #to_proc: MRI's implicit conversion message */
    if (mesg_prefix(mesg, "can't convert ") && mesg_suffix(mesg, " into Proc")) {
      mrb_value s = mrb_str_new_lit(mrb, "no implicit conversion of ");
      mrb_str_cat(mrb, s, RSTRING_PTR(mesg) + 14, RSTRING_LEN(mesg) - 14);
      mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
      return;
    }
    /* define_method(name, not_a_proc): MRI names all it takes */
    if (mrb->c->ci->mid == MRB_SYM(define_method) && mesg_suffix(mesg, " (expected Proc)")) {
      mrb_value s = mrb_str_new(mrb, RSTRING_PTR(mesg), RSTRING_LEN(mesg) - 1);
      mrb_str_cat_lit(mrb, s, "/Method/UnboundMethod)");
      mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
      return;
    }
    classdef_fix(mrb, exc);
    return;
  }
  if (exc->c == E_NAME_ERROR) {
    /* Module#instance_method's NameError names the method */
    mrb_value e = mrb_obj_value(exc);
    mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
    /* an undefined @@x read in Ruby code: the receiver is the class whose
       scope it is read in */
    if (mesg_prefix(mesg, "uninitialized class variable ") && !mrb_iv_defined(mrb, e, MRB_IVSYM(receiver))) {
      mrb_callinfo *ci = mrb->c->ci;
      if (ci->proc && !MRB_PROC_CFUNC_P(ci->proc)) {
        struct RClass *c = mrb_vm_cref_class(mrb, ci);
        if (c) mrb_iv_set(mrb, e, MRB_IVSYM(receiver), mrb_obj_value(c));
      }
    }
    if (mrb_nil_p(mrb_iv_get(mrb, e, MRB_IVSYM(name))) && mesg_prefix(mesg, "undefined method '")) {
      const char *p = RSTRING_PTR(mesg) + 18, *q = memchr(p, '\'', RSTRING_LEN(mesg) - 18);
      if (q) mrb_iv_set(mrb, e, MRB_IVSYM(name), mrb_symbol_value(mrb_intern(mrb, p, q - p)));
    }
    return;
  }
  if (mrb_class_defined_id(mrb, MRB_SYM(SyntaxError)) && exc->c == mrb_exc_get_id(mrb, MRB_SYM(SyntaxError))) {
    /* mruby codegen's "generator error, invalid yield (SyntaxError)": MRI's words */
    mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
    static const char gen[] = "generator error, invalid yield (SyntaxError)";
    if (mrb_string_p(mesg) && mesg_suffix(mesg, gen)) {
      mrb_value s = mrb_str_new(mrb, RSTRING_PTR(mesg), RSTRING_LEN(mesg) - (mrb_int)(sizeof(gen) - 1));
      mrb_str_cat_lit(mrb, s, "Invalid yield");
      mrb_exc_mesg_set(mrb, (struct RException*)exc, s);
      mesg = s;
    }
    syntax_error_fix(mrb, exc, mesg);
    return;
  }
  if (exc->c == E_RANGE_ERROR) {
    /* Integer#<< / #>> past the representable: MRI's message */
    mrb_callinfo *ci = mrb->c->ci;
    if ((ci->mid == MRB_OPSYM(lshift) || ci->mid == MRB_OPSYM(rshift)) &&
        (mrb_integer_p(ci->stack[0]) || mrb_bigint_p(ci->stack[0]))) {
      mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
      if (mesg_prefix(mesg, "shift width too large") || mesg_prefix(mesg, "integer out of range"))
        mrb_exc_mesg_set(mrb, (struct RException*)exc, mrb_str_new_lit(mrb, "shift width too big"));
    }
    return;
  }
  struct RClass *fe = mrb_exc_get_id(mrb, MRB_ERROR_SYM(FrozenError));
  if (exc->c != fe) return;
  mrb_value e = mrb_obj_value(exc);
  if (mrb_iv_defined(mrb, e, MRB_IVSYM(receiver))) return;
  mrb_value mesg = mrb_exc_mesg_get(mrb, (struct RException*)exc);
  static const char pre[] = "can't modify frozen ";
  const mrb_int plen = sizeof(pre) - 1;
  if (!mrb_string_p(mesg) || RSTRING_LEN(mesg) <= plen || memcmp(RSTRING_PTR(mesg), pre, plen) != 0) return;
  const char *cname = RSTRING_PTR(mesg) + plen;
  mrb_int clen = RSTRING_LEN(mesg) - plen;

  mrb_value found = mrb_undef_value();
  mrb_callinfo *ci = mrb->c->ci;
  for (int depth = 0; depth < 3 && ci >= mrb->c->cibase && mrb_undef_p(found); depth++, ci--) {
    if (!ci->stack) break;
    if (ci->proc && !MRB_PROC_CFUNC_P(ci->proc) && ci->pc) {
      const mrb_irep *irep = ci->proc->body.irep;
      const mrb_code *pc = ci->pc;
      if (pc - irep->iseq >= 3 && pc[-3] == OP_DEF) {
        mrb_value t = frz_candidate(mrb, ci->stack[pc[-2]]);
        if (frz_match(mrb, t, cname, clen)) found = t;
      }
      if (mrb_undef_p(found) && pc - irep->iseq >= 4 && pc[-4] == OP_SDEF) {
        mrb_value t = ci->stack[pc[-3]];
        if (frz_match(mrb, t, cname, clen)) found = t;
      }
      if (mrb_undef_p(found) && pc - irep->iseq >= 4 && pc[-4] == OP_TDEF) {
        struct RClass *tc = mrb_vm_ci_target_class(ci);
        if (tc) {
          mrb_value t = frz_candidate(mrb, mrb_obj_value(tc));
          if (frz_match(mrb, t, cname, clen)) found = t;
        }
      }
    }
    if (mrb_undef_p(found)) {
      mrb_value self = frz_candidate(mrb, ci->stack[0]);
      if (frz_match(mrb, self, cname, clen)) found = self;
    }
  }
  if (mrb_undef_p(found)) return;
  mrb_iv_set(mrb, e, MRB_IVSYM(receiver), found);
  mrb_iv_set(mrb, e, MRB_SYM(__oruby_frz__), mrb_true_value());
}

static int frz_inspecting;

static mrb_value frz_inspect_body(mrb_state *mrb, void *ud) {
  return mrb_inspect(mrb, *(mrb_value*)ud);
}

static mrb_value frozen_error_to_s(mrb_state *mrb, mrb_value self) {
  struct RException *exc = mrb_exc_ptr(self);
  if (mrb_test(mrb_iv_get(mrb, self, MRB_SYM(__oruby_frz__)))) {
    mrb_iv_remove(mrb, self, MRB_SYM(__oruby_frz__));
    mrb_value mesg = mrb_exc_mesg_get(mrb, exc);
    if (mrb_string_p(mesg)) {
      mrb_value recv = mrb_iv_get(mrb, self, MRB_IVSYM(receiver));
      mrb_value s = mrb_str_dup(mrb, mesg);
      mrb_str_cat_lit(mrb, s, ": ");
      mrb_value insp = mrb_nil_value();
      if (!frz_inspecting) {
        mrb_bool err = FALSE;
        frz_inspecting++;
        insp = mrb_protect_error(mrb, frz_inspect_body, &recv, &err);
        frz_inspecting--;
        if (err || !mrb_string_p(insp)) insp = mrb_nil_value();
      }
      if (mrb_string_p(insp)) mrb_str_cat_str(mrb, s, insp);
      else mrb_str_cat_lit(mrb, s, " ...");
      mrb_exc_mesg_set(mrb, exc, s);
    }
  }
  mrb_value mesg = mrb_exc_mesg_get(mrb, exc);
  if (!mrb_string_p(mesg)) return mrb_str_new_cstr(mrb, mrb_obj_classname(mrb, self));
  return mesg;
}


/* ---- MRI's argument and state checks in front of mruby C methods ----
   Each entry runs its check, then the original C function. */
enum chk_kind {
  CHK_BLOCK_LJE,    /* LocalJumpError "no block given" without a block */
  CHK_BLOCK_ARG,    /* ArgumentError(msg) without a block */
  CHK_ARGC_MIN1,    /* ArgumentError "given 0, expected 1+" */
  CHK_IVAR_FROZEN,  /* an @ivar name on a frozen/immediate self: FrozenError */
  CHK_FROZEN_SELF,  /* FrozenError on a frozen self */
  CHK_FROZEN_ARGS,  /* FrozenError on a frozen self given any argument */
  CHK_NOARGS_SELF,  /* no arguments: self, before any frozen check */
  CHK_NOT_ROOT,     /* TypeError on BasicObject (Module#dup) */
  CHK_NOT_CLASS,    /* TypeError on Class itself (Class#allocate) */
  CHK_SINGLETON,    /* TypeError unless self is a singleton class */
  CHK_SUPERCLASS,   /* Class.new(super): super must be a Class */
  CHK_UNDEF_NAMES,  /* NameError naming module/class for missing names */
  CHK_FROZEN_P,     /* Integer (bignum) is frozen */
  CHK_DEL_AFFIX,    /* delete_prefix/suffix(!): whole characters only; msg "!" nil unchanged */
  CHK_STR_EQ,       /* String#== / eql? (msg "eql"): #to_str objects, comparable encodings */
  CHK_STR_CMP,      /* String#<=>: #to_str, else the inverse <=>; encodings of equal bytes */
  CHK_ASCII_ONLY,   /* String#ascii_only?: never for an ASCII-incompatible encoding */
  CHK_STR_LENGTH,   /* String#length of a tagged string: by its encoding (Ruby) */
  CHK_STR_ORD,      /* String#ord of a broken US-ASCII character: ArgumentError */
  CHK_BINDING_SELF, /* Kernel#binding: self is the caller's, not the receiver */
  CHK_SCLASS_FSTR,  /* singleton_class of a deduplicated String (-"str"): TypeError */
  CHK_NAME_BYTES,   /* alias_method: a String name's bytes valid, as String#to_sym */
  CHK_STR_TO_SYM,   /* String#to_sym of broken UTF-8: EncodingError */
  CHK_STR_INSPECT,  /* String#inspect: \u for Unicode controls; a tagged one in Ruby */
  CHK_ASCII_COMPAT, /* String#to_f / #to_c: an ASCII-incompatible string raises */
  CHK_PATH_ARG1,    /* argument 1 a path: #to_path, then #to_str */
  CHK_OBJ_INSPECT,  /* Kernel#inspect: #instance_variables_to_inspect (4.0), BasicObject ivars */
};

struct chk_def {
  int owner;        /* 0 Kernel, 1 BasicObject, 2 Module, 3 Class, 4 Class singleton, 5 Proc singleton, 6 String */
  const char *name;
  enum chk_kind kind;
  const char *msg;
  mrb_func_t orig;
};

static struct chk_def chk_defs[] = {
  {1, "instance_exec", CHK_BLOCK_LJE, NULL, NULL},
  {2, "module_exec", CHK_BLOCK_LJE, NULL, NULL},
  {0, "catch", CHK_BLOCK_LJE, NULL, NULL},
  {0, "at_exit", CHK_BLOCK_ARG, "called without a block", NULL},
  {0, "proc", CHK_BLOCK_ARG, "tried to create Proc object without a block", NULL},
  {5, "new", CHK_BLOCK_ARG, "tried to create Proc object without a block", NULL},
  {0, "extend", CHK_ARGC_MIN1, NULL, NULL},
  {2, "include", CHK_ARGC_MIN1, NULL, NULL},
  {2, "prepend", CHK_ARGC_MIN1, NULL, NULL},
  {0, "instance_variable_set", CHK_IVAR_FROZEN, NULL, NULL},
  {0, "remove_instance_variable", CHK_IVAR_FROZEN, NULL, NULL},
  {0, "initialize_copy", CHK_FROZEN_SELF, NULL, NULL},
  {2, "undef_method", CHK_UNDEF_NAMES, NULL, NULL},
  {2, "remove_method", CHK_NOARGS_SELF, NULL, NULL},
  {2, "dup", CHK_NOT_ROOT, NULL, NULL},
  {3, "allocate", CHK_NOT_CLASS, NULL, NULL},
  {3, "attached_object", CHK_SINGLETON, NULL, NULL},
  {4, "new", CHK_SUPERCLASS, NULL, NULL},
  {0, "frozen?", CHK_FROZEN_P, NULL, NULL},
  {6, "delete_prefix", CHK_DEL_AFFIX, "p", NULL},
  {6, "delete_prefix!", CHK_DEL_AFFIX, "p!", NULL},
  {6, "delete_suffix", CHK_DEL_AFFIX, "s", NULL},
  {6, "delete_suffix!", CHK_DEL_AFFIX, "s!", NULL},
  {6, "==", CHK_STR_EQ, "==", NULL},
  {6, "eql?", CHK_STR_EQ, "eql", NULL},
  {6, "<=>", CHK_STR_CMP, NULL, NULL},
  {6, "ascii_only?", CHK_ASCII_ONLY, NULL, NULL},
  {6, "length", CHK_STR_LENGTH, NULL, NULL},
  {6, "ord", CHK_STR_ORD, NULL, NULL},
  {0, "binding", CHK_BINDING_SELF, NULL, NULL},
  {0, "singleton_class", CHK_SCLASS_FSTR, NULL, NULL},
  {2, "alias_method", CHK_NAME_BYTES, NULL, NULL},
  {8, "binding", CHK_BINDING_SELF, NULL, NULL},
  {6, "to_sym", CHK_STR_TO_SYM, NULL, NULL},
  {6, "inspect", CHK_STR_INSPECT, NULL, NULL},
  {6, "to_f", CHK_ASCII_COMPAT, NULL, NULL},
  {6, "to_c", CHK_ASCII_COMPAT, NULL, NULL},
  {2, "autoload", CHK_PATH_ARG1, NULL, NULL},
  {0, "inspect", CHK_OBJ_INSPECT, NULL, NULL},
};
#define CHK_N (sizeof(chk_defs) / sizeof(chk_defs[0]))

static mrb_bool chk_has_block(mrb_state *mrb) {
  mrb_callinfo *ci = mrb->c->ci;
  return ci->blk != NULL;
}

static mrb_bool chk_ivar_name_p(mrb_state *mrb, mrb_value name) {
  const char *p;
  mrb_int len;
  if (mrb_symbol_p(name)) p = mrb_sym_name_len(mrb, mrb_symbol(name), &len);
  else if (mrb_string_p(name)) { p = RSTRING_PTR(name); len = RSTRING_LEN(name); }
  else return FALSE;
  return p && len >= 2 && p[0] == '@' && p[1] != '@';
}

static mrb_bool chk_method_known(mrb_state *mrb, struct RClass *c, mrb_sym mid) {
  struct RClass *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
  return !MRB_METHOD_UNDEF_P(m);
}

/* whether p[0, n] is well-formed UTF-8 */
static mrb_bool utf8_valid(const unsigned char *p, mrb_int n) {
  mrb_int i = 0;
  while (i < n) {
    unsigned char c = p[i];
    int len;
    if (c < 0x80) { i++; continue; }
    if (c >= 0xC2 && c <= 0xDF) len = 2;
    else if (c >= 0xE0 && c <= 0xEF) len = 3;
    else if (c >= 0xF0 && c <= 0xF4) len = 4;
    else return FALSE;
    if (i + len > n) return FALSE;
    for (int k = 1; k < len; k++) if ((p[i + k] & 0xC0) != 0x80) return FALSE;
    if (c == 0xE0 && p[i + 1] < 0xA0) return FALSE;
    if (c == 0xED && p[i + 1] > 0x9F) return FALSE;
    if (c == 0xF0 && p[i + 1] < 0x90) return FALSE;
    if (c == 0xF4 && p[i + 1] > 0x8F) return FALSE;
    i += len;
  }
  return TRUE;
}

/* String#inspect of a UTF-8 string as MRI's: control characters (C0, DEL,
   C1) in \u notation, where mruby writes \xHH or the raw C1 bytes */
static mrb_value str_inspect_unicode(mrb_state *mrb, mrb_value r) {
  const unsigned char *p = (const unsigned char *)RSTRING_PTR(r);
  mrb_int n = RSTRING_LEN(r), i;
  for (i = 0; i < n; i++) {
    if (p[i] == '\\' && i + 1 < n && p[i + 1] == 'x') break;
    if (p[i] == 0xC2 && i + 1 < n && p[i + 1] >= 0x80 && p[i + 1] <= 0x9F) break;
    if (p[i] == '\\') i++;
  }
  if (i >= n) return r;
  mrb_value out = mrb_str_new(mrb, (const char *)p, i);
  static const char hex[] = "0123456789ABCDEF";
  while (i < n) {
    unsigned char c = p[i];
    if (c == '\\' && i + 3 < n && p[i + 1] == 'x') {
      int hi = p[i + 2], lo = p[i + 3];
      int v = -1;
      const char *h1 = strchr(hex, hi), *h2 = strchr(hex, lo);
      if (h1 && h2 && hi && lo) v = (int)(h1 - hex) * 16 + (int)(h2 - hex);
      if (v >= 0 && (v < 0x20 || v == 0x7F)) {
        char buf[7] = {'\\', 'u', '0', '0', hex[v >> 4], hex[v & 15], 0};
        mrb_str_cat(mrb, out, buf, 6);
        i += 4;
        continue;
      }
      mrb_str_cat(mrb, out, (const char *)p + i, 2);
      i += 2;
      continue;
    }
    if (c == '\\' && i + 1 < n) {
      mrb_str_cat(mrb, out, (const char *)p + i, 2);
      i += 2;
      continue;
    }
    if (c == 0xC2 && i + 1 < n && p[i + 1] >= 0x80 && p[i + 1] <= 0x9F) {
      char buf[7] = {'\\', 'u', '0', '0', hex[p[i + 1] >> 4], hex[p[i + 1] & 15], 0};
      mrb_str_cat(mrb, out, buf, 6);
      i += 2;
      continue;
    }
    mrb_str_cat(mrb, out, (const char *)p + i, 1);
    i++;
  }
  return out;
}

/* "#<Class:0x... @a=1, @b=2>" of the ivars names lists (all when nil) */
/* the objects whose ivars are being inspected: one seen again is "..." */
#define OBJ_INSPECT_MAX 64
static struct { mrb_state *mrb; struct RBasic *o; } obj_inspect_stack[OBJ_INSPECT_MAX];
static int obj_inspect_depth;

static mrb_value obj_inspect_ivars_body(mrb_state *mrb, mrb_value obj, mrb_value names);
static mrb_value obj_inspect_ivars_protected(mrb_state *mrb, void *p) {
  mrb_value *a = (mrb_value *)p;
  return obj_inspect_ivars_body(mrb, a[0], a[1]);
}

static mrb_value obj_inspect_ivars(mrb_state *mrb, mrb_value obj, mrb_value names) {
  for (int i = 0; i < obj_inspect_depth; i++) {
    if (obj_inspect_stack[i].mrb == mrb && obj_inspect_stack[i].o == mrb_basic_ptr(obj)) {
      mrb_value s = mrb_any_to_s(mrb, obj);
      mrb_str_resize(mrb, s, RSTRING_LEN(s) - 1);
      mrb_str_cat_lit(mrb, s, " ...>");
      return s;
    }
  }
  if (obj_inspect_depth >= OBJ_INSPECT_MAX) return obj_inspect_ivars_body(mrb, obj, names);
  int d = obj_inspect_depth++;
  obj_inspect_stack[d].mrb = mrb;
  obj_inspect_stack[d].o = mrb_basic_ptr(obj);
  mrb_value args[2] = {obj, names};
  mrb_bool err = FALSE;
  mrb_value r = mrb_protect_error(mrb, obj_inspect_ivars_protected, args, &err);
  obj_inspect_depth = d;
  if (err) mrb_exc_raise(mrb, r);
  return r;
}

static mrb_value obj_inspect_ivars_body(mrb_state *mrb, mrb_value obj, mrb_value names) {
  mrb_value s = mrb_any_to_s(mrb, obj);
  if (mrb_nil_p(names)) names = mrb_obj_instance_variables(mrb, obj);
  mrb_str_resize(mrb, s, RSTRING_LEN(s) - 1);  /* the closing ">" */
  mrb_int shown = 0;
  for (mrb_int i = 0; i < RARRAY_LEN(names); i++) {
    mrb_value n = RARRAY_PTR(names)[i];
    if (!mrb_symbol_p(n) || !mrb_iv_defined(mrb, obj, mrb_symbol(n))) continue;
    mrb_str_cat_cstr(mrb, s, shown++ ? ", " : " ");
    mrb_str_cat_str(mrb, s, mrb_sym_str(mrb, mrb_symbol(n)));
    mrb_str_cat_lit(mrb, s, "=");
    mrb_str_cat_str(mrb, s, mrb_inspect(mrb, mrb_iv_get(mrb, obj, mrb_symbol(n))));
  }
  mrb_str_cat_lit(mrb, s, ">");
  return s;
}

/* the String's tag (an Encoding other than UTF-8/BINARY), or nil */
static mrb_value str_tag(mrb_state *mrb, mrb_value s) {
  return mrb_iv_get(mrb, s, MRB_SYM(__encoding));
}

static mrb_bool str_tag_ascii_incompatible(mrb_state *mrb, mrb_value s) {
  mrb_value tag = str_tag(mrb, s);
  return !mrb_nil_p(tag) && !mrb_test(mrb_iv_get(mrb, tag, MRB_IVSYM(ascii_compatible)));
}

/* rb_invcmp's recursion guard: (a, b) pairs being compared inversely */
#define INVCMP_MAX 16
static struct { mrb_state *mrb; struct RBasic *a, *b; } invcmp_stack[INVCMP_MAX];
static int invcmp_depth;

static mrb_bool invcmp_active(mrb_state *mrb, mrb_value a, mrb_value b) {
  struct RBasic *pa = mrb_immediate_p(a) ? NULL : mrb_basic_ptr(a), *pb = mrb_immediate_p(b) ? NULL : mrb_basic_ptr(b);
  for (int k = 0; k < invcmp_depth; k++) {
    if (invcmp_stack[k].mrb != mrb) continue;
    if ((invcmp_stack[k].a == pa && invcmp_stack[k].b == pb) || (invcmp_stack[k].a == pb && invcmp_stack[k].b == pa)) return TRUE;
  }
  return FALSE;
}

struct invcmp_args { mrb_value self, other; };
static mrb_value invcmp_body(mrb_state *mrb, void *ud) {
  struct invcmp_args *x = (struct invcmp_args *)ud;
  return mrb_funcall_id(mrb, x->other, MRB_OPSYM(cmp), 1, x->self);
}

static mrb_value str_invcmp(mrb_state *mrb, mrb_value self, mrb_value other) {
  if (invcmp_active(mrb, self, other) || invcmp_depth >= INVCMP_MAX) return mrb_nil_value();
  invcmp_stack[invcmp_depth].mrb = mrb;
  invcmp_stack[invcmp_depth].a = mrb_basic_ptr(self);
  invcmp_stack[invcmp_depth].b = mrb_immediate_p(other) ? NULL : mrb_basic_ptr(other);
  invcmp_depth++;
  struct invcmp_args x = {self, other};
  mrb_bool err = FALSE;
  mrb_value r = mrb_protect_error(mrb, invcmp_body, &x, &err);
  invcmp_depth--;
  if (err) mrb_exc_raise(mrb, r);
  if (mrb_nil_p(r)) return r;
  if (mrb_integer_p(r)) return mrb_int_value(mrb, -mrb_integer(r));
  /* rb_cmpint of another result */
  if (mrb_test(mrb_funcall_id(mrb, r, MRB_OPSYM(gt), 1, mrb_int_value(mrb, 0)))) return mrb_int_value(mrb, -1);
  if (mrb_test(mrb_funcall_id(mrb, r, MRB_OPSYM(lt), 1, mrb_int_value(mrb, 0)))) return mrb_int_value(mrb, 1);
  return mrb_int_value(mrb, 0);
}

static mrb_value chk_run(mrb_state *mrb, mrb_value self, int i) {
  struct chk_def *d = &chk_defs[i];
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  switch (d->kind) {
  case CHK_BLOCK_LJE:
    if (!chk_has_block(mrb)) mrb_raise(mrb, E_LOCALJUMP_ERROR, "no block given");
    break;
  case CHK_BLOCK_ARG:
    if (!chk_has_block(mrb)) mrb_raise(mrb, E_ARGUMENT_ERROR, d->msg);
    break;
  case CHK_ARGC_MIN1:
    if (argc == 0) mrb_argnum_error(mrb, 0, 1, -1);
    if (mrb->refinement_class) {
      for (mrb_int j = 0; j < argc; j++) {
        if (mrb_obj_class(mrb, argv[j]) != mrb->refinement_class) continue;
        const char *w = d->name[0] == 'e' ? "Cannot extend object with refinement" :
                        d->name[0] == 'p' ? "Cannot prepend refinement" : "Cannot include refinement";
        mrb_raise(mrb, E_TYPE_ERROR, w);
      }
    }
    break;
  case CHK_IVAR_FROZEN:
    if (argc >= 1 && chk_ivar_name_p(mrb, argv[0])) mrb_check_frozen_value(mrb, self);
    break;
  case CHK_FROZEN_SELF:  /* initialize_copy: a copy of itself does nothing */
    if (argc >= 1 && mrb_obj_eq(mrb, argv[0], self)) return self;
    mrb_check_frozen_value(mrb, self);
    break;
  case CHK_FROZEN_ARGS:
    if (argc > 0 && !mrb_immediate_p(self)) mrb_check_frozen(mrb, mrb_basic_ptr(self));
    break;
  case CHK_NOARGS_SELF:
    if (argc == 0) return self;
    break;
  case CHK_NOT_ROOT:
    if (mrb_class_p(self) && mrb_class_ptr(self) == mrb_class_get_id(mrb, MRB_SYM(BasicObject)))
      mrb_raise(mrb, E_TYPE_ERROR, "can't copy the root class");
    break;
  case CHK_NOT_CLASS:
    if (mrb_class_p(self) && mrb_class_ptr(self) == mrb->class_class)
      mrb_raise(mrb, E_TYPE_ERROR, "allocator undefined for Class");
    if (mrb_class_p(self) && mrb_class_ptr(self) == mrb->string_class) {
      /* String.allocate is a binary String, as MRI's */
      mrb_value r = d->orig(mrb, self);
      if (mrb_string_p(r)) RSTR_ENCODING_SET(mrb_str_ptr(r), MRB_STR_ENCODING_BINARY);
      return r;
    }
    break;
  case CHK_SINGLETON:
    if (!mrb_sclass_p(self)) mrb_raisef(mrb, E_TYPE_ERROR, "'%v' is not a singleton class", self);
    break;
  case CHK_SUPERCLASS:
    if (argc >= 1 && !mrb_class_p(argv[0])) {
      if (mrb_sclass_p(argv[0])) mrb_raise(mrb, E_TYPE_ERROR, "can't make subclass of singleton class");
      mrb_raisef(mrb, E_TYPE_ERROR, "superclass must be an instance of Class (given an instance of %C)", mrb_obj_class(mrb, argv[0]));
    }
    break;
  case CHK_UNDEF_NAMES:
    for (mrb_int j = 0; j < argc; j++)  /* the original raises its TypeError first */
      if (!mrb_symbol_p(argv[j]) && !mrb_string_p(argv[j])) return d->orig(mrb, self);
    if (argc > 0 && !mrb_immediate_p(self)) mrb_check_frozen(mrb, mrb_basic_ptr(self));
    for (mrb_int j = 0; j < argc; j++) {
      mrb_value n = argv[j];
      mrb_sym mid;
      if (mrb_symbol_p(n)) mid = mrb_symbol(n);
      else if (mrb_string_p(n)) mid = mrb_intern_str(mrb, n);
      else continue;  /* the original raises its TypeError */
      if (!chk_method_known(mrb, mrb_class_ptr(self), mid)) {
        mrb_value shown = mrb_sclass_p(self) ? mrb_iv_get(mrb, self, MRB_SYM(__attached__)) : self;
        if (!mrb_class_p(shown) && !mrb_module_p(shown) && !mrb_sclass_p(shown)) shown = self;
        mrb_name_error(mrb, mid, "undefined method '%n' for %s '%v'", mid,
                       mrb_module_p(self) ? "module" : "class", shown);
      }
    }
    break;
  case CHK_DEL_AFFIX: {
    mrb_bool suffix = d->msg[0] == 's', bang = d->msg[1] == '!';
    if (argc == 1 && mrb_string_p(argv[0]) && mrb_string_p(self) && !RSTR_BINARY_P(mrb_str_ptr(self))) {
      mrb_int n = RSTRING_LEN(argv[0]), len = RSTRING_LEN(self);
      const char *p = RSTRING_PTR(self);
      if (n > 0 && n < len && memcmp(suffix ? p + len - n : p, RSTRING_PTR(argv[0]), n) == 0) {
        mrb_int pos = suffix ? len - n : n;  /* where the cut would be */
        if (((unsigned char)p[pos] & 0xC0) == 0x80)
          return bang ? mrb_nil_value() : mrb_str_dup(mrb, self);
      }
    }
    if (bang) {
      mrb_int before = RSTRING_LEN(self);
      mrb_value r = d->orig(mrb, self);
      return mrb_string_p(self) && RSTRING_LEN(self) == before ? mrb_nil_value() : r;
    }
    break;
  }
  case CHK_STR_EQ: {
    if (argc != 1 || !mrb_string_p(self)) break;
    mrb_value o = argv[0];
    if (!mrb_string_p(o)) {
      if (d->msg[0] == 'e') return mrb_false_value();
      if (!mrb_obj_eq(mrb, o, self) && mrb_respond_to(mrb, o, MRB_SYM(to_str)))
        return mrb_bool_value(mrb_test(mrb_funcall_id(mrb, o, MRB_OPSYM(eq), 1, self)));
      return mrb_false_value();
    }
    if (enc_plain_same(mrb, self, o)) break;
    if (RSTRING_LEN(self) != RSTRING_LEN(o)) return mrb_false_value();
    mrb_value k = mrb_obj_value(mrb->kernel_module);
    if (!mrb_test(mrb_funcall_id(mrb, k, MRB_SYM_Q(__oruby_str_comparable), 2, self, o))) return mrb_false_value();
    break;
  }
  case CHK_STR_CMP: {
    if (argc != 1 || !mrb_string_p(self)) break;
    mrb_value o = argv[0];
    if (!mrb_string_p(o)) {
      if (mrb_respond_to(mrb, o, MRB_SYM(to_str))) {
        mrb_value t = mrb_funcall_id(mrb, o, MRB_SYM(to_str), 0);
        if (mrb_string_p(t)) return mrb_funcall_id(mrb, self, MRB_OPSYM(cmp), 1, t);
      }
      if (!mrb_respond_to(mrb, o, MRB_OPSYM(cmp))) return mrb_nil_value();
      return str_invcmp(mrb, self, o);
    }
    if (enc_plain_same(mrb, self, o) || RSTRING_LEN(self) != RSTRING_LEN(o) ||
        memcmp(RSTRING_PTR(self), RSTRING_PTR(o), RSTRING_LEN(self)) != 0) break;
    /* the same bytes in encodings that do not compare: by encoding index */
    return mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_str_enc_cmp), 2, self, o);
  }
  case CHK_ASCII_ONLY:
    if (mrb_string_p(self) && RSTRING_LEN(self) > 0 && str_tag_ascii_incompatible(mrb, self)) return mrb_false_value();
    if (mrb_string_p(self) && RSTRING_LEN(self) == 0 && str_tag_ascii_incompatible(mrb, self)) return mrb_false_value();
    break;
  case CHK_BINDING_SELF: {
    mrb_value b = d->orig(mrb, self);
    mrb_callinfo *ci = mrb->c->ci;
    if (ci > mrb->c->cibase && ci[-1].stack && !mrb_immediate_p(b))
      mrb_iv_set(mrb, b, MRB_SYM(recv), ci[-1].stack[0]);
    return b;
  }
  case CHK_NAME_BYTES:
    for (mrb_int k = 0; k < mrb_get_argc(mrb); k++) {
      mrb_value a = mrb_get_argv(mrb)[k];
      if (mrb_string_p(a) && !RSTR_BINARY_P(mrb_str_ptr(a)) &&
          !utf8_valid((const unsigned char *)RSTRING_PTR(a), RSTRING_LEN(a)))
        mrb_raisef(mrb, mrb_exc_get_id(mrb, MRB_SYM(EncodingError)), "invalid symbol in encoding UTF-8 :%v", mrb_inspect(mrb, a));
    }
    break;
  case CHK_SCLASS_FSTR:
    if (mrb_string_p(self) && mrb_frozen_p(mrb_basic_ptr(self))) {
      /* String#-@'s table (27_string.rb): encoding => {str => fstr} */
      mrb_value t = mrb_iv_get(mrb, mrb_obj_value(mrb->kernel_module), mrb_intern_lit(mrb, "@__oruby_fstr"));
      if (mrb_hash_p(t)) {
        mrb_value vals = mrb_hash_values(mrb, t);
        for (mrb_int k = 0; k < RARRAY_LEN(vals); k++) {
          mrb_value h = RARRAY_PTR(vals)[k];
          if (mrb_hash_p(h) && mrb_obj_eq(mrb, mrb_hash_get(mrb, h, self), self))
            mrb_raise(mrb, E_TYPE_ERROR, "can't define singleton");
        }
      }
    }
    break;
  case CHK_STR_ORD:
    if (mrb_string_p(self) && RSTRING_LEN(self) > 0 && ((unsigned char)RSTRING_PTR(self)[0]) >= 0x80 &&
        RSTR_BINARY_P(mrb_str_ptr(self))) {
      mrb_value tag = str_tag(mrb, self);
      if (!mrb_nil_p(tag) && mrb_class_defined_id(mrb, MRB_SYM(Encoding)) &&
          mrb_obj_eq(mrb, tag, mrb_const_get(mrb, mrb_obj_value(mrb_class_get_id(mrb, MRB_SYM(Encoding))), MRB_SYM(US_ASCII))))
        mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in US-ASCII");
    }
    break;
  case CHK_STR_LENGTH:
    if (mrb_string_p(self) && RSTR_BINARY_P(mrb_str_ptr(self)) && !mrb_nil_p(str_tag(mrb, self)))
      return mrb_funcall_id(mrb, self, MRB_SYM(__oruby_enc_length), 0);
    break;
  case CHK_STR_TO_SYM:
    if (mrb_string_p(self) && !RSTR_BINARY_P(mrb_str_ptr(self)) &&
        !utf8_valid((const unsigned char *)RSTRING_PTR(self), RSTRING_LEN(self))) {
      mrb_raisef(mrb, mrb_exc_get_id(mrb, MRB_SYM(EncodingError)), "invalid symbol in encoding UTF-8 :%v", mrb_inspect(mrb, self));
    }
    break;
  case CHK_STR_INSPECT:
    if (!mrb_string_p(self)) break;
    if (RSTR_BINARY_P(mrb_str_ptr(self))) {
      if (mrb_nil_p(str_tag(mrb, self))) break;
      return mrb_funcall_id(mrb, self, MRB_SYM(__oruby_enc_inspect), 0);
    }
    return str_inspect_unicode(mrb, d->orig(mrb, self));
  case CHK_ASCII_COMPAT:
    if (d->name[3] == 'f' && mrb_string_p(self) && memchr(RSTRING_PTR(self), '_', RSTRING_LEN(self))) {
      /* to_f: an underscore not between two digits ends the number */
      const char *p = RSTRING_PTR(self);
      mrb_int n = RSTRING_LEN(self);
      for (mrb_int k = 0; k < n; k++) {
        if (p[k] != '_') continue;
        if (k == 0 || k + 1 >= n || !ISDIGIT(p[k - 1]) || !ISDIGIT(p[k + 1])) {
          mrb_value t = mrb_str_new(mrb, p, k);
          return d->orig(mrb, t);
        }
      }
    }
    if (mrb_string_p(self) && str_tag_ascii_incompatible(mrb, self)) {
      struct RClass *enc = mrb_class_get_id(mrb, MRB_SYM(Encoding));
      mrb_value name = mrb_funcall_id(mrb, str_tag(mrb, self), MRB_SYM(name), 0);
      mrb_raisef(mrb, mrb_class_get_under_id(mrb, enc, MRB_SYM(CompatibilityError)), "ASCII incompatible encoding: %v", name);
    }
    break;
  case CHK_PATH_ARG1:
    if (argc >= 2 && !mrb_string_p(argv[1])) {
      mrb_value v = argv[1];
      if (mrb_respond_to(mrb, v, MRB_SYM(to_path))) v = mrb_funcall_id(mrb, v, MRB_SYM(to_path), 0);
      if (!mrb_string_p(v)) {
        mrb_value cv[2] = {v, mrb_obj_value(mrb->string_class)};
        v = mrb_funcall_argv(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__implicit), 2, cv);
      }
      ((mrb_value *)mrb_get_argv(mrb))[1] = v;
    }
    {
      /* [mod, name, path] of each autoload, for require (gem/load) to find
         the autoloads of a file it loads directly */
      mrb_value r = d->orig(mrb, self);
      argv = mrb_get_argv(mrb);
      if (argc >= 2 && mrb_symbol_p(argv[0]) && mrb_string_p(argv[1]) &&
          mrb_autoload_p(mrb_iv_get(mrb, self, mrb_symbol(argv[0])))) {
        mrb_value k = mrb_obj_value(mrb->kernel_module);
        mrb_value reg = mrb_iv_get(mrb, k, MRB_SYM(__oruby_autoloads));
        if (!mrb_array_p(reg)) {
          reg = mrb_ary_new(mrb);
          mrb_iv_set(mrb, k, MRB_SYM(__oruby_autoloads), reg);
        }
        mrb_value e[3] = {self, argv[0], mrb_str_dup(mrb, argv[1])};
        mrb_ary_push(mrb, reg, mrb_ary_new_from_values(mrb, 3, e));
        struct RClass *owner = mrb_class(mrb, k);
        if (mrb_obj_respond_to(mrb, owner, MRB_SYM(__oruby_autoload_registered))) {
          mrb_funcall_argv(mrb, k, MRB_SYM(__oruby_autoload_registered), 3, e);
        }
      }
      return r;
    }
  case CHK_OBJ_INSPECT: {
    if (!mrb_object_p(self)) break;
    struct RClass *c = mrb_class(mrb, self), *owner = c;
    mrb_method_t m = mrb_method_search_vm(mrb, &owner, MRB_SYM(instance_variables_to_inspect));
    if (!MRB_METHOD_UNDEF_P(m)) {
      mrb_value names = mrb_funcall_id(mrb, self, MRB_SYM(instance_variables_to_inspect), 0);
      if (!mrb_nil_p(names) && !mrb_array_p(names))
        mrb_raisef(mrb, E_TYPE_ERROR, "Expected #instance_variables_to_inspect to return an Array or nil, but it returned %C",
                   mrb_obj_class(mrb, names));
      if (mrb_array_p(names)) return obj_inspect_ivars(mrb, self, names);
      break;
    }
    /* an object with ivars shows them as MRI's: whatever its #to_s (mruby's
       shows them only for the basic one), a BasicObject's too, and one seen
       again as "#<C:0x... ...>" (mruby's leaves "-<C" there) */
    owner = c;
    if (MRB_METHOD_UNDEF_P(mrb_method_search_vm(mrb, &owner, MRB_SYM(to_s))) ||
        RARRAY_LEN(mrb_obj_instance_variables(mrb, self)) > 0)
      return obj_inspect_ivars(mrb, self, mrb_nil_value());
    break;
  }
  case CHK_FROZEN_P:
    if (mrb_integer_p(self) || mrb_float_p(self) || mrb_bigint_p(self)) return mrb_true_value();
    break;
  }
  return d->orig(mrb, self);
}

#define CHK_W(i) static mrb_value chk_w##i(mrb_state *mrb, mrb_value self) { return chk_run(mrb, self, i); }
CHK_W(0) CHK_W(1) CHK_W(2) CHK_W(3) CHK_W(4) CHK_W(5) CHK_W(6) CHK_W(7) CHK_W(8) CHK_W(9)
CHK_W(10) CHK_W(11) CHK_W(12) CHK_W(13) CHK_W(14) CHK_W(15) CHK_W(16) CHK_W(17) CHK_W(18) CHK_W(19)
CHK_W(20) CHK_W(21) CHK_W(22) CHK_W(23) CHK_W(24) CHK_W(25) CHK_W(26) CHK_W(27) CHK_W(28) CHK_W(29)
CHK_W(30) CHK_W(31) CHK_W(32) CHK_W(33) CHK_W(34) CHK_W(35) CHK_W(36) CHK_W(37) CHK_W(38) CHK_W(39)
static mrb_func_t chk_ws[] = {
  chk_w0, chk_w1, chk_w2, chk_w3, chk_w4, chk_w5, chk_w6, chk_w7, chk_w8, chk_w9,
  chk_w10, chk_w11, chk_w12, chk_w13, chk_w14, chk_w15, chk_w16, chk_w17, chk_w18, chk_w19,
  chk_w20, chk_w21, chk_w22, chk_w23, chk_w24, chk_w25, chk_w26, chk_w27, chk_w28, chk_w29,
  chk_w30, chk_w31, chk_w32, chk_w33, chk_w34, chk_w35, chk_w36, chk_w37, chk_w38, chk_w39,
};

static void install_checks(mrb_state *mrb) {
  mrb_static_assert(CHK_N <= sizeof(chk_ws) / sizeof(chk_ws[0]), "chk_ws too short");
  for (size_t i = 0; i < CHK_N; i++) {
    struct chk_def *d = &chk_defs[i];
    struct RClass *c = NULL;
    switch (d->owner) {
    case 0: c = mrb->kernel_module; break;
    case 1: c = mrb_class_get_id(mrb, MRB_SYM(BasicObject)); break;
    case 2: c = mrb->module_class; break;
    case 3: c = mrb->class_class; break;
    case 4: c = mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(mrb->class_class))); break;
    case 5: c = mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(mrb->proc_class))); break;
    case 6: c = mrb->string_class; break;
    case 7: c = mrb->array_class; break;
    case 8: c = mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(mrb->kernel_module))); break;
    }
    mrb_func_t orig = NULL;
    wrap_cfunc(mrb, c, d->name, chk_ws[i], &orig);
    if (orig) d->orig = orig;
  }
  /* aliases stay the same method as their primary */
  mrb_alias_method(mrb, mrb->module_class, MRB_SYM(class_exec), MRB_SYM(module_exec));
  mrb_alias_method(mrb, mrb->string_class, MRB_OPSYM(eqq), MRB_OPSYM(eq));
  mrb_alias_method(mrb, mrb->string_class, MRB_SYM(size), MRB_SYM(length));
  mrb_alias_method(mrb, mrb->array_class, MRB_SYM(to_s), MRB_SYM(inspect));
}


/* ---- Method / UnboundMethod identity, as MRI's method_eq / method_hash:
   the same definition (aliases resolved), and for a Method the same
   receiver. A C method's proc is made anew for each Method object, so C
   ones compare by function and the values of its env (attr names, the Go
   method index). */
static const struct RProc *meth_base(mrb_state *mrb, mrb_value m) {
  mrb_value proc = mrb_iv_get(mrb, m, MRB_SYM(_proc));
  if (!mrb_proc_p(proc)) return NULL;
  const struct RProc *p = mrb_proc_ptr(proc);
  while (p && MRB_PROC_ALIAS_P(p)) p = p->upper;
  return p;
}

static mrb_bool meth_env_eq(const struct RProc *p, const struct RProc *q) {
  mrb_bool pe = MRB_PROC_ENV_P(p) && p->e.env, qe = MRB_PROC_ENV_P(q) && q->e.env;
  if (!pe || !qe) return pe == qe;
  const struct REnv *a = p->e.env, *b = q->e.env;
  if (a == b) return TRUE;
  if (MRB_ENV_LEN(a) != MRB_ENV_LEN(b)) return FALSE;
  for (mrb_int i = 0; i < MRB_ENV_LEN(a); i++) {
    if (!mrb_obj_eq(NULL, a->stack[i], b->stack[i])) return FALSE;
  }
  return TRUE;
}

static mrb_bool meth_same_def(mrb_state *mrb, mrb_value a, mrb_value b) {
  const struct RProc *p = meth_base(mrb, a), *q = meth_base(mrb, b);
  if (!p || !q) {
    /* respond_to_missing? methods: by name */
    if (p || q) return FALSE;
    return mrb_obj_eq(mrb, mrb_iv_get(mrb, a, MRB_SYM(_name)), mrb_iv_get(mrb, b, MRB_SYM(_name)));
  }
  if (p == q) return TRUE;
  if (MRB_PROC_CFUNC_P(p) || MRB_PROC_CFUNC_P(q)) {
    if (!MRB_PROC_CFUNC_P(p) || !MRB_PROC_CFUNC_P(q)) return FALSE;
    return MRB_PROC_CFUNC(p) == MRB_PROC_CFUNC(q) && meth_env_eq(p, q);
  }
  return p->body.irep == q->body.irep && MRB_PROC_ENV_P(p) == MRB_PROC_ENV_P(q) &&
         (!MRB_PROC_ENV_P(p) || p->e.env == q->e.env);
}

static mrb_value meth_eq_m(mrb_state *mrb, mrb_value self) {
  mrb_value other = mrb_get_arg1(mrb);
  if (mrb_obj_class(mrb, self) != mrb_obj_class(mrb, other)) return mrb_false_value();
  if (!mrb_iv_defined(mrb, other, MRB_SYM(_proc))) return mrb_false_value();
  if (!mrb_obj_eq(mrb, mrb_iv_get(mrb, self, MRB_SYM(_recv)), mrb_iv_get(mrb, other, MRB_SYM(_recv))))
    return mrb_false_value();
  return mrb_bool_value(meth_same_def(mrb, self, other));
}

static mrb_value meth_hash_m(mrb_state *mrb, mrb_value self) {
  const struct RProc *p = meth_base(mrb, self);
  uint64_t h = 0x9e3779b97f4a7c15ULL;
  if (!p) {
    h ^= (uint64_t)mrb_obj_id(mrb_iv_get(mrb, self, MRB_SYM(_name)));
  }
  else if (MRB_PROC_CFUNC_P(p)) {
    h ^= (uint64_t)(uintptr_t)MRB_PROC_CFUNC(p);
    if (MRB_PROC_ENV_P(p) && p->e.env) {
      for (mrb_int i = 0; i < MRB_ENV_LEN(p->e.env); i++)
        h = h * 31 + (uint64_t)mrb_obj_id(p->e.env->stack[i]);
    }
  }
  else {
    h ^= (uint64_t)(uintptr_t)p->body.irep;
    if (MRB_PROC_ENV_P(p)) h ^= (uint64_t)(uintptr_t)p->e.env;
  }
  mrb_value recv = mrb_iv_get(mrb, self, MRB_SYM(_recv));
  if (!mrb_nil_p(recv)) h = h * 1000003 + (uint64_t)mrb_obj_id(recv);
  return mrb_int_value(mrb, (mrb_int)(h >> 2));
}

/* Method#__oruby_mklass: the class the method was looked up from */
static mrb_value meth_mklass_m(mrb_state *mrb, mrb_value self) {
  return mrb_iv_get(mrb, self, MRB_SYM(_klass));
}

/* Proc.new as MRI's rb_proc_s_new: the block's own Proc when it is of the
   class already (else a copy of that class), then #initialize with the
   arguments (mruby's passes none) */
static mrb_value proc_s_new_m(mrb_state *mrb, mrb_value klass) {
  const mrb_value *argv;
  mrb_int argc;
  mrb_value blk;
  mrb_get_args(mrb, "*&", &argv, &argc, &blk);
  if (mrb_nil_p(blk)) mrb_raise(mrb, E_ARGUMENT_ERROR, "tried to create Proc object without a block");
  mrb_value proc = blk;
  if (mrb_obj_class(mrb, blk) != mrb_class_ptr(klass)) {
    struct RProc *p = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb_class_ptr(klass));
    mrb_proc_copy(mrb, p, mrb_proc_ptr(blk));
    proc = mrb_obj_value(p);
    if (!MRB_PROC_STRICT_P(p) && mrb->c->ci > mrb->c->cibase && MRB_PROC_ENV(p) == mrb->c->ci[-1].u.env)
      p->flags |= MRB_PROC_ORPHAN;
  }
  mrb_funcall_with_block(mrb, proc, MRB_SYM(initialize), argc, argv, proc);
  return proc;
}

static mrb_value mod_autoload_loading_m(mrb_state *mrb, mrb_value self) {
  mrb_sym sym;
  mrb_value path, real;
  mrb_get_args(mrb, "nSS", &sym, &path, &real);
  mrb_value marker = mrb_iv_get(mrb, self, sym);
  if (!mrb_autoload_p(marker)) return mrb_fixnum_value(0);
  struct orb_autoload *al = (struct orb_autoload *)DATA_PTR(marker);
  if (al->len != RSTRING_LEN(path) || memcmp(al->path, RSTRING_PTR(path), al->len) != 0) {
    return mrb_fixnum_value(0);
  }

  mrb_value name = mrb_symbol_value(sym);
  mrb_value marks = autoload_marks(mrb, self);
  if (!mrb_hash_p(marks)) {
    marks = mrb_hash_new(mrb);
    mrb_obj_iv_set_force(mrb, mrb_obj_ptr(self), MRB_SYM(__oruby_autoload_marks), marks);
  }
  mrb_value record = mrb_hash_get(mrb, marks, name);
  if (mrb_array_p(record) && mrb_obj_eq(mrb, RARRAY_PTR(record)[0], marker)) {
    return mrb_fixnum_value(0);
  }

  int status = al->loading ? 2 : 1;
  al->loading = TRUE;
  mrb_value locations = mrb_iv_get(mrb, self, MRB_SYM(__oruby_csl));
  mrb_value location = mrb_hash_p(locations) ? mrb_hash_get(mrb, locations, name) : mrb_nil_value();
  mrb_value values[] = {marker, real, location};
  mrb_hash_set(mrb, marks, name, mrb_ary_new_from_values(mrb, 3, values));
  return mrb_fixnum_value(status);
}

static mrb_value mod_autoload_spend_m(mrb_state *mrb, mrb_value self) {
  mrb_sym sym;
  mrb_int mode;
  mrb_get_args(mrb, "ni", &sym, &mode);
  mrb_value name = mrb_symbol_value(sym);
  mrb_value record = autoload_active(mrb, self, name);
  if (mrb_nil_p(record)) return mrb_nil_value();

  mrb_value marks = autoload_marks(mrb, self);
  mrb_value marker = RARRAY_PTR(record)[0];
  mrb_value warning = mrb_nil_value();
  if (mrb_obj_eq(mrb, mrb_iv_get(mrb, self, sym), marker)) {
    ((struct orb_autoload *)DATA_PTR(marker))->loading = mode != 0;
    if (mode == 1) {
      mrb_hash_set(mrb, marks, name, mrb_ary_new_from_values(mrb, 1, &marker));
      return mrb_nil_value();
    }
    if (mode == 2 && mrb_true_p(mrb_gv_get(mrb, MRB_GVSYM(VERBOSE))) &&
        mrb_class_defined_id(mrb, MRB_SYM(Warning))) {
      warning = mrb_format(mrb, "warning: Expected %v to define %C::%n but it didn't\n",
                           RARRAY_PTR(record)[1], mrb_class_ptr(self), sym);
    }
  }
  mrb_hash_delete_key(mrb, marks, name);
  if (!mrb_nil_p(warning)) {
    mrb_funcall_id(mrb, mrb_obj_value(mrb_module_get_id(mrb, MRB_SYM(Warning))), MRB_SYM(warn), 1, warning);
  }
  return mrb_nil_value();
}

static mrb_func_t autoload_p_orig;
static mrb_value mod_autoload_p_w(mrb_state *mrb, mrb_value mod) {
  mrb_sym sym;
  mrb_bool inherit = TRUE;
  mrb_get_args(mrb, "n|b", &sym, &inherit);
  mrb_value mark = autoload_lookup(mrb, mod, sym, inherit);
  if (autoload_other_thread(mrb, mark)) {
    struct orb_autoload *al = (struct orb_autoload *)DATA_PTR(RARRAY_PTR(mark)[0]);
    return mrb_str_new(mrb, al->path, al->len);
  }
  return autoload_p_orig(mrb, mod);
}

static mrb_value autoload_of_const_arg(mrb_state *mrb, mrb_value mod) {
  mrb_int argc = mrb_get_argc(mrb);
  if (argc < 1 || argc > 2) return mrb_nil_value();
  const mrb_value *argv = mrb_get_argv(mrb);
  mrb_sym sym = mrb_symbol_p(argv[0]) ? mrb_symbol(argv[0]) :
                mrb_string_p(argv[0]) ? mrb_intern_check_str(mrb, argv[0]) : 0;
  if (!sym) return mrb_nil_value();
  return autoload_lookup(mrb, mod, sym, argc == 1 || mrb_test(argv[1]));
}

static mrb_func_t const_defined_orig, const_get_orig;
static mrb_value mod_const_defined_w(mrb_state *mrb, mrb_value mod) {
  mrb_value result = const_defined_orig(mrb, mod);
  if (mrb_test(result)) return result;
  return mrb_bool_value(autoload_other_thread(mrb, autoload_of_const_arg(mrb, mod)));
}

static mrb_value mod_const_get_w(mrb_state *mrb, mrb_value mod) {
  autoload_loader_call(mrb, MRB_SYM(__oruby_autoload_wait), autoload_of_const_arg(mrb, mod));
  return const_get_orig(mrb, mod);
}

static mrb_value defined_const_path_body(mrb_state *mrb, void *unused) {
  (void)unused;
  mrb_value start, path;
  mrb_get_args(mrb, "oA", &start, &path);
  mrb_int len = RARRAY_LEN(path);
  if (!len || !mrb_symbol_p(RARRAY_PTR(path)[0])) return mrb_nil_value();
  mrb_int i = 0;
  mrb_value outer = start;
  if (mrb_nil_p(start)) {
    mrb_callinfo *ci = &mrb->c->ci[-1];
    if (ci < mrb->c->cibase || !ci->proc) return mrb_nil_value();
    outer = mrb_vm_const_get_noraise(mrb, ci, mrb_symbol(RARRAY_PTR(path)[0]), len > 1);
    if (mrb_undef_p(outer)) return mrb_nil_value();
    i = 1;
  }
  for (; i < RARRAY_LEN(path); i++) {
    if (!mrb_class_p(outer) && !mrb_module_p(outer) && mrb_type(outer) != MRB_TT_SCLASS) return mrb_nil_value();
    mrb_value name = RARRAY_PTR(path)[i];
    if (!mrb_symbol_p(name)) return mrb_nil_value();
    mrb_sym sym = mrb_symbol(name);
    mrb_bool final = i == RARRAY_LEN(path) - 1;
    mrb_value v = mrb_const_get_noraise(mrb, mrb_class_ptr(outer), sym, !final);
    if (mrb_undef_p(v)) {
      /* The final name is a query, so another thread's autoload remains
         visible without waiting for the file to finish. */
      if (final && !(mrb_const_flags(mrb, mrb_class_ptr(outer), sym) & MRB_CONST_PRIVATE) &&
          autoload_other_thread(mrb, autoload_lookup(mrb, outer, sym, FALSE))) break;
      return mrb_nil_value();
    }
    outer = v;
  }
  return mrb_str_new_lit_frozen(mrb, "constant");
}

static mrb_value defined_const_path_w(mrb_state *mrb, mrb_value self) {
  (void)self;
  mrb_bool error;
  mrb_value result = mrb_protect_error(mrb, defined_const_path_body, NULL, &error);
  if (error && mrb_type(result) == MRB_TT_BREAK) mrb_exc_raise(mrb, result);
  return error ? mrb_nil_value() : result;
}

/* Kernel.autoload / Kernel.autoload?: on the caller's cref, as
   Kernel#autoload (not on Kernel, the receiver) */
static mrb_value kernel_s_autoload_call(mrb_state *mrb, mrb_sym mid) {
  const mrb_value *argv;
  mrb_int argc;
  mrb_get_args(mrb, "*", &argv, &argc);
  mrb_callinfo *ci = &mrb->c->ci[-1];
  struct RClass *c = NULL;
  if (ci >= mrb->c->cibase && ci->proc) c = mrb_vm_cref_class(mrb, ci);
  if (!c || c->tt == MRB_TT_SCLASS) c = mrb->object_class;
  return mrb_funcall_argv(mrb, mrb_obj_value(c), mid, argc, argv);
}
static mrb_value kernel_s_autoload(mrb_state *mrb, mrb_value self) { return kernel_s_autoload_call(mrb, MRB_SYM(autoload)); }
static mrb_value kernel_s_autoload_p(mrb_state *mrb, mrb_value self) { return kernel_s_autoload_call(mrb, MRB_SYM_Q(autoload)); }

/* nil.to_s / true.to_s / false.to_s: one frozen String each, as MRI's */
static mrb_value special_to_s(mrb_state *mrb, mrb_value self) {
  struct RClass *c = mrb_class(mrb, self);
  mrb_value s = mrb_iv_get(mrb, mrb_obj_value(c), MRB_SYM(__oruby_to_s));
  if (!mrb_string_p(s)) {
    s = mrb_nil_p(self) ? mrb_str_new_lit(mrb, "") : mrb_true_p(self) ? mrb_str_new_lit(mrb, "true") : mrb_str_new_lit(mrb, "false");
    str_enc_us_ascii_set(mrb, s);  /* US-ASCII, as MRI's */
    mrb_basic_ptr(s)->frozen = 1;
    mrb_iv_set(mrb, mrb_obj_value(c), MRB_SYM(__oruby_to_s), s);
  }
  return s;
}

/* Module#method_added (mruby's does nothing): initialize_dup,
   initialize_clone and respond_to_missing? are private once defined, as in
   MRI (mruby makes initialize and initialize_copy so itself) */
static mrb_value mod_method_added_m(mrb_state *mrb, mrb_value self) {
  mrb_sym mid;
  mrb_get_args(mrb, "n", &mid);
  if ((mid == MRB_SYM(initialize_dup) || mid == MRB_SYM(initialize_clone) || mid == MRB_SYM_Q(respond_to_missing)) &&
      (mrb_class_p(self) || mrb_module_p(self)))
    set_own_visibility(mrb, mrb_class_ptr(self), mid, MRB_METHOD_PRIVATE_FL);
  return mrb_nil_value();
}

/* __method__ / __callee__ as MRI's: the method a block belongs to (through
   its enclosing procs, define_method bodies included), never the C method
   that yields to it; __callee__ the name it was called by (an alias), read
   from the caller's send. */
static mrb_sym frame_called_name(mrb_state *mrb, mrb_callinfo *f) {
  mrb_callinfo *cf = f - 1;
  if (cf < mrb->c->cibase || !cf->proc || MRB_PROC_CFUNC_P(cf->proc) || !cf->pc) return f->mid;
  const mrb_irep *irep = cf->proc->body.irep;
  const mrb_code *pc = cf->pc;
  ptrdiff_t off = pc - irep->iseq;
  int b = -1;
  if (off >= 4 && (pc[-4] == OP_SEND || pc[-4] == OP_SSEND || pc[-4] == OP_SENDB || pc[-4] == OP_SSENDB)) b = pc[-2];
  else if (off >= 3 && (pc[-3] == OP_SEND0 || pc[-3] == OP_SSEND0)) b = pc[-1];
  if (b < 0 || b >= irep->slen) return f->mid;
  mrb_sym called = irep->syms[b];
  if (called == f->mid || !f->stack) return f->mid;
  /* the alias the caller named, when it is one of this method */
  struct RClass *c = mrb_class(mrb, f->stack[0]);
  mrb_method_t m = mrb_method_search_vm(mrb, &c, called);
  if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_PROC_P(m) && MRB_PROC_ALIAS_P(MRB_METHOD_PROC(m)) &&
      MRB_METHOD_PROC(m)->body.mid == f->mid)
    return called;
  return f->mid;
}

static mrb_value frame_method(mrb_state *mrb, mrb_bool callee) {
  mrb_callinfo *ci = mrb->c->ci - 1;
  if (ci < mrb->c->cibase || !ci->proc) return mrb_nil_value();
  const struct RProc *p = ci->proc;
  if (MRB_PROC_CFUNC_P(p)) return ci->mid ? mrb_symbol_value(ci->mid) : mrb_nil_value();
  while (p && !MRB_PROC_SCOPE_P(p)) p = p->upper;
  if (!p) return mrb_nil_value();
  for (mrb_callinfo *f = ci; f >= mrb->c->cibase; f--) {
    if (f->proc != p) continue;
    if (!f->mid) return mrb_nil_value();
    return mrb_symbol_value(callee ? frame_called_name(mrb, f) : f->mid);
  }
  /* the method's frame is gone (a block that outlived it) */
  if (ci->proc->e.env && ci->proc->e.env->tt == MRB_TT_ENV && ci->proc->e.env->mid)
    return mrb_symbol_value(ci->proc->e.env->mid);
  return mrb_nil_value();
}
static mrb_value k_method_m(mrb_state *mrb, mrb_value self) { return frame_method(mrb, FALSE); }
static mrb_value k_callee_m(mrb_state *mrb, mrb_value self) { return frame_method(mrb, TRUE); }

/* Method#super_method / UnboundMethod#super_method: the next definition
   after the method's own place in the ancestry it was looked up in (an
   included module's place too, which mruby's skipped), an alias resolved
   to where its original was found */
static mrb_value meth_super_method_m(mrb_state *mrb, mrb_value self) {
  mrb_value recv = mrb_iv_get(mrb, self, MRB_SYM(_recv));
  mrb_value klass = mrb_iv_get(mrb, self, MRB_SYM(_klass));
  mrb_value owner = mrb_iv_get(mrb, self, MRB_SYM(_owner));
  mrb_value name = mrb_iv_get(mrb, self, MRB_SYM(_name));
  mrb_value procv = mrb_iv_get(mrb, self, MRB_SYM(_proc));
  if (!mrb_symbol_p(name) || !(mrb_class_p(owner) || mrb_module_p(owner) || mrb_sclass_p(owner))) return mrb_nil_value();
  struct RClass *own = mrb_class_ptr(owner), *at = NULL;
  mrb_sym mid = mrb_symbol(name);
  if (mrb_proc_p(procv)) {
    const struct RProc *p = mrb_proc_ptr(procv);
    while (p && MRB_PROC_ALIAS_P(p)) {
      if (p->e.target_class) { own = p->e.target_class; mid = p->body.mid; }
      p = p->upper;
    }
  }
  if (own->tt == MRB_TT_ICLASS) at = own;
  struct RClass *start = (mrb_class_p(klass) || mrb_module_p(klass) || mrb_sclass_p(klass)) ? mrb_class_ptr(klass)
                         : mrb_class(mrb, recv);
  if (!at) {
    for (struct RClass *k = start; k; k = k->super) {
      if (k == own || (k->tt == MRB_TT_ICLASS && k->c == own)) { at = k; break; }
    }
  }
  if (!at) at = own;
  struct RClass *f = at->super;
  if (!f) return mrb_nil_value();
  mrb_method_t m = mrb_method_search_vm(mrb, &f, mid);
  if (MRB_METHOD_UNDEF_P(m) || !f) return mrb_nil_value();
  const struct RProc *proc;
  if (MRB_METHOD_PROC_P(m)) proc = MRB_METHOD_PROC(m);
  else {
    struct RProc *cp = mrb_proc_new_cfunc(mrb, MRB_METHOD_FUNC(m));
    proc = cp;
  }
  struct RClass *fowner = f->tt == MRB_TT_ICLASS ? f->c : f;
  struct RObject *me = (struct RObject*)mrb_obj_alloc(mrb, MRB_TT_OBJECT, mrb_obj_class(mrb, self));
  mrb_obj_iv_set(mrb, me, MRB_SYM(_owner), mrb_obj_value(fowner));
  mrb_obj_iv_set(mrb, me, MRB_SYM(_recv), recv);
  mrb_obj_iv_set(mrb, me, MRB_SYM(_name), mrb_symbol_value(mid));
  mrb_obj_iv_set(mrb, me, MRB_SYM(_proc), mrb_obj_value((void*)proc));
  mrb_obj_iv_set(mrb, me, MRB_SYM(_klass), mrb_obj_value(start));
  return mrb_obj_value(me);
}

/* Kernel.__oruby_own_hook?(obj, name): whether obj's class defines name
   itself (not Kernel's / BasicObject's): Object#dup and #clone call a
   custom initialize_dup / initialize_clone (11_finalizer.rb) */
static mrb_value k_own_hook_p(mrb_state *mrb, mrb_value self) {
  mrb_value obj;
  mrb_sym name;
  mrb_get_args(mrb, "on", &obj, &name);
  struct RClass *c = mrb_class(mrb, obj);
  mrb_method_t m = mrb_method_search_vm(mrb, &c, name);
  if (MRB_METHOD_UNDEF_P(m) || !c) return mrb_false_value();
  struct RClass *owner = c->tt == MRB_TT_ICLASS ? c->c : c;
  return mrb_bool_value(owner != mrb->kernel_module && owner != mrb_class_get_id(mrb, MRB_SYM(BasicObject)));
}

/* Kernel.__oruby_fwd(:* / :** / :&): the value of the method's anonymous
   parameter, for eval'd code forwarding it (prism.go's ORB_ANON_ARG): the
   local of that name, through the eval'd code's enclosing frames */
static mrb_value k_fwd_m(mrb_state *mrb, mrb_value self) {
  mrb_sym sym;
  mrb_get_args(mrb, "n", &sym);
  mrb_callinfo *ci = mrb->c->ci - 1;
  if (ci < mrb->c->cibase || !ci->proc) return mrb_nil_value();
  const struct RProc *q = ci->proc;
  const mrb_value *regs = ci->stack;
  while (q && !MRB_PROC_CFUNC_P(q) && regs) {
    const mrb_irep *irep = q->body.irep;
    if (irep->lv) {
      for (int i = 0; i + 1 < irep->nlocals; i++) {
        if (irep->lv[i] == sym) return regs[i + 1];
      }
    }
    if (!q->upper || !MRB_PROC_ENV_P(q) || !q->e.env) break;
    regs = q->e.env->stack;
    q = q->upper;
  }
  const char *what = sym == MRB_OPSYM(mul) ? "rest" : sym == MRB_OPSYM(pow) ? "keyword rest" : "block";
  mrb_raisef(mrb, mrb_exc_get_id(mrb, MRB_SYM(SyntaxError)), "no anonymous %s parameter", what);
  return mrb_nil_value();
}

/* a symbol from a String's bytes, which a UTF-8 string must have valid, as
   String#to_sym (CHK_STR_TO_SYM) */
static mrb_value str_checked_intern(mrb_state *mrb, mrb_value s) {
  if (!mrb_string_p(s)) return mrb_funcall_id(mrb, s, MRB_SYM(to_sym), 0);
  if (!RSTR_BINARY_P(mrb_str_ptr(s)) && !utf8_valid((const unsigned char *)RSTRING_PTR(s), RSTRING_LEN(s)))
    mrb_raisef(mrb, mrb_exc_get_id(mrb, MRB_SYM(EncodingError)), "invalid symbol in encoding UTF-8 :%v", mrb_inspect(mrb, s));
  return mrb_symbol_value(mrb_intern_str(mrb, s));
}

/* Kernel.__oruby_dsym(str), .__oruby_dsyms(ary): :"a#{x}" and %I[] (prism.go
   ORB_DSYM) */
static mrb_value k_dsym_m(mrb_state *mrb, mrb_value self) {
  return str_checked_intern(mrb, mrb_get_arg1(mrb));
}
static mrb_value k_dsyms_m(mrb_state *mrb, mrb_value self) {
  mrb_value a = mrb_get_arg1(mrb);
  if (!mrb_array_p(a)) return a;
  for (mrb_int i = 0; i < RARRAY_LEN(a); i++) mrb_ary_set(mrb, a, i, str_checked_intern(mrb, RARRAY_PTR(a)[i]));
  return a;
}

static void install_method_identity(mrb_state *mrb) {
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__oruby_fwd), k_fwd_m, MRB_ARGS_REQ(1));
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__oruby_dsym), k_dsym_m, MRB_ARGS_REQ(1));
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__oruby_dsyms), k_dsyms_m, MRB_ARGS_REQ(1));
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM_Q(__oruby_own_hook), k_own_hook_p, MRB_ARGS_REQ(2));
  if (mrb_class_defined_id(mrb, MRB_SYM(Method))) {
    mrb_define_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(Method)), MRB_SYM(super_method), meth_super_method_m, MRB_ARGS_NONE());
    mrb_define_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(UnboundMethod)), MRB_SYM(super_method), meth_super_method_m, MRB_ARGS_NONE());
  }
  mrb_define_private_method_id(mrb, mrb->kernel_module, MRB_SYM(__method__), k_method_m, MRB_ARGS_NONE());
  mrb_define_private_method_id(mrb, mrb->kernel_module, MRB_SYM(__callee__), k_callee_m, MRB_ARGS_NONE());
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__method__), k_method_m, MRB_ARGS_NONE());
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__callee__), k_callee_m, MRB_ARGS_NONE());
  mrb_define_private_method_id(mrb, mrb->module_class, MRB_SYM(method_added), mod_method_added_m, MRB_ARGS_REQ(1));
  /* Fixnum is gone since Ruby 3.2 */
  if (mrb_const_defined_at(mrb, mrb_obj_value(mrb->object_class), MRB_SYM(Fixnum)))
    mrb_const_remove(mrb, mrb_obj_value(mrb->object_class), MRB_SYM(Fixnum));
  mrb_define_method_id(mrb, mrb->nil_class, MRB_SYM(to_s), special_to_s, MRB_ARGS_NONE());
  mrb_define_method_id(mrb, mrb->true_class, MRB_SYM(to_s), special_to_s, MRB_ARGS_NONE());
  mrb_define_method_id(mrb, mrb->false_class, MRB_SYM(to_s), special_to_s, MRB_ARGS_NONE());
  mrb_alias_method(mrb, mrb->true_class, MRB_SYM(inspect), MRB_SYM(to_s));
  mrb_alias_method(mrb, mrb->false_class, MRB_SYM(inspect), MRB_SYM(to_s));
  mrb_define_private_method_id(mrb, mrb->module_class, MRB_SYM(__oruby_autoload_loading), mod_autoload_loading_m, MRB_ARGS_REQ(3));
  mrb_define_private_method_id(mrb, mrb->module_class, MRB_SYM(__oruby_autoload_spend), mod_autoload_spend_m, MRB_ARGS_REQ(2));
  wrap_cfunc_id(mrb, mrb->module_class, MRB_SYM_Q(autoload), mod_autoload_p_w, &autoload_p_orig);
  wrap_cfunc_id(mrb, mrb->module_class, MRB_SYM_Q(const_defined), mod_const_defined_w, &const_defined_orig);
  wrap_cfunc_id(mrb, mrb->module_class, MRB_SYM(const_get), mod_const_get_w, &const_get_orig);
  mrb_define_private_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(BasicObject)), MRB_SYM_Q(__defined_const_path), defined_const_path_w, MRB_ARGS_REQ(2));
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(autoload), kernel_s_autoload, MRB_ARGS_ANY());
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM_Q(autoload), kernel_s_autoload_p, MRB_ARGS_ANY());
  mrb_define_class_method_id(mrb, mrb->proc_class, MRB_SYM(new), proc_s_new_m, MRB_ARGS_ANY() | MRB_ARGS_BLOCK());
  const mrb_sym names[2] = {MRB_SYM(Method), MRB_SYM(UnboundMethod)};
  for (int i = 0; i < 2; i++) {
    if (!mrb_class_defined_id(mrb, names[i])) continue;
    struct RClass *c = mrb_class_get_id(mrb, names[i]);
    mrb_define_method_id(mrb, c, MRB_OPSYM(eq), meth_eq_m, MRB_ARGS_REQ(1));
    mrb_alias_method(mrb, c, MRB_SYM_Q(eql), MRB_OPSYM(eq));
    mrb_define_method_id(mrb, c, MRB_SYM(hash), meth_hash_m, MRB_ARGS_NONE());
    mrb_define_private_method_id(mrb, c, MRB_SYM(__oruby_mklass), meth_mklass_m, MRB_ARGS_NONE());
  }
  /* aliases MRI has, the same method as their primary */
  if (mrb_class_defined_id(mrb, MRB_SYM(Method)))
    mrb_alias_method(mrb, mrb_class_get_id(mrb, MRB_SYM(Method)), MRB_OPSYM(eqq), MRB_SYM(call));
  mrb_alias_method(mrb, mrb->proc_class, MRB_OPSYM(eqq), MRB_SYM(call));
  mrb_alias_method(mrb, mrb->proc_class, MRB_SYM(yield), MRB_SYM(call));
  mrb_alias_method(mrb, mrb->nil_class, MRB_OPSYM(xor), MRB_OPSYM(or));
  mrb_alias_method(mrb, mrb->false_class, MRB_OPSYM(xor), MRB_OPSYM(or));
  mrb_alias_method(mrb, mrb->module_class, MRB_SYM(class_eval), MRB_SYM(module_eval));
}

/* MRI optimizes literal.freeze only while the original method is installed. */
static mrb_value literal_freeze_m(mrb_state *mrb, mrb_value self) {
  mrb_value str, freeze_call;
  mrb_get_args(mrb, "o&!", &str, &freeze_call);
  struct RClass *c = mrb_class(mrb, str);
#ifdef MRB_USE_REFINEMENTS
  mrb_method_t method = mrb_vm_find_method_in_scope(mrb, mrb_vm_caller_refinements(mrb), c, &c, MRB_SYM(freeze));
#else
  mrb_method_t method = mrb_vm_find_method(mrb, c, &c, MRB_SYM(freeze));
#endif
  if (MRB_METHOD_CFUNC_P(method) && MRB_METHOD_CFUNC(method) == mrb_obj_freeze &&
      MRB_METHOD_VISIBILITY(method) == MRB_METHOD_PUBLIC_FL &&
      mrb_respond_to(mrb, self, MRB_SYM(__oruby_fstring))) {
    return mrb_funcall_id(mrb, self, MRB_SYM(__oruby_fstring), 1, str);
  }
  /* Dispatch from the original lexical scope when freeze has changed. */
  return mrb_yield(mrb, freeze_call, str);
}

static mrb_value plain_string_p(mrb_state *mrb, mrb_value self) {
  (void)self;
  return mrb_bool_value(mrb_class(mrb, mrb_get_arg1(mrb)) == mrb->string_class);
}

static mrb_value frozen_string_copy_m(mrb_state *mrb, mrb_value self) {
  (void)self;
  mrb_value str;
  mrb_get_args(mrb, "S", &str);
  mrb_value copy = mrb_str_dup(mrb, str);
  mrb_value tag = mrb_iv_get(mrb, str, MRB_SYM(__encoding));
  if (!mrb_nil_p(tag)) enc_copy_tag(mrb, tag, str, copy);
  struct RClass *c = mrb_obj_class(mrb, str);
  mrb_basic_ptr(copy)->c = c;
  mrb_field_write_barrier(mrb, mrb_basic_ptr(copy), (struct RBasic*)c);
  return mrb_obj_freeze(mrb, copy);
}

static mrb_value regexp_part_m(mrb_state *mrb, mrb_value self) {
  (void)self;
  return mrb_obj_as_string(mrb, mrb_get_arg1(mrb));
}

void _mrb_init_oruby_core(mrb_state *mrb) {
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__oruby_regexp_part), regexp_part_m, MRB_ARGS_REQ(1));
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__oruby_literal_freeze), literal_freeze_m, MRB_ARGS_REQ(1) | MRB_ARGS_BLOCK());
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM_Q(__oruby_plain_string), plain_string_p, MRB_ARGS_REQ(1));
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM(__oruby_frozen_string_copy), frozen_string_copy_m, MRB_ARGS_REQ(1));
  _orb_fast_init(mrb);
  install_num_eq(mrb);
  install_num_to_s(mrb);
  _mrb_wrap_string_encoding(mrb);
  gv_init_checked(mrb);
  wrap_cfunc(mrb, mrb->kernel_module, "respond_to?", respond_to_w, &respond_to_orig);
  mrb_define_method_id(mrb, mrb->array_class, MRB_SYM(inspect), array_inspect_w, MRB_ARGS_NONE());
  mrb_define_method_id(mrb, mrb->hash_class, MRB_SYM(inspect), hash_inspect_w, MRB_ARGS_NONE());
  wrap_cfunc(mrb, mrb->range_class, "initialize", range_init_w, &range_init_orig);
  wrap_cfunc(mrb, mrb->range_class, "initialize_copy", range_init_copy_w, &range_init_copy_orig);
  mrb_alias_method(mrb, mrb->array_class, MRB_SYM(to_s), MRB_SYM(inspect));
  mrb_alias_method(mrb, mrb->hash_class, MRB_SYM(to_s), MRB_SYM(inspect));
  wrap_cfunc(mrb, mrb->module_class, "const_missing", const_missing_w, &const_missing_orig);
  wrap_cfunc(mrb, mrb->module_class, "remove_const", remove_const_w, &remove_const_orig);
  wrap_cfunc(mrb, mrb->kernel_module, "local_variables", k_lvars_w, &k_lvars_orig);
  wrap_cfunc(mrb, mrb->module_class, "define_method", define_method_w, &define_method_orig);
  {
    struct RClass *o = mrb->module_class;
    mrb_method_t m = mrb_method_search_vm(mrb, &o, MRB_SYM(attr_writer));
    if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_CFUNC_P(m)) attr_writer_fn = MRB_METHOD_CFUNC(m);
  }
  wrap_cfunc(mrb, mrb->module_class, "attr", attr_w, &attr_orig);
  wrap_cfunc(mrb, mrb->module_class, "attr_reader", attr_reader_w, &attr_reader_orig);
  wrap_cfunc(mrb, mrb->module_class, "attr_writer", attr_writer_w, &attr_writer_orig);
  wrap_cfunc(mrb, mrb->module_class, "attr_accessor", attr_accessor_w, &attr_accessor_orig);
  wrap_name_methods(mrb);
  wrap_cfunc(mrb, mrb->module_class, "alias_method", alias_method_w, &alias_method_orig);
  if (mrb_class_defined_id(mrb, MRB_SYM(Method))) {
    mrb_define_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(Method)), MRB_SYM(original_name), method_original_name_m, MRB_ARGS_NONE());
    mrb_define_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(Method)), MRB_SYM(parameters), method_params_m, MRB_ARGS_NONE());
  }
  if (mrb_class_defined_id(mrb, MRB_SYM(UnboundMethod))) {
    mrb_define_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(UnboundMethod)), MRB_SYM(original_name), method_original_name_m, MRB_ARGS_NONE());
    mrb_define_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(UnboundMethod)), MRB_SYM(parameters), method_params_m, MRB_ARGS_NONE());
    mrb_define_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(UnboundMethod)),
                         mrb_intern_lit(mrb, "__oruby_ruby2_keywords"), ruby2_keywords_mark_m, MRB_ARGS_NONE());
  }
  mrb_define_private_method_id(mrb, mrb->proc_class, mrb_intern_lit(mrb, "__oruby_ruby2_keywords"),
                               ruby2_keywords_mark_m, MRB_ARGS_NONE());
  mrb_define_private_method_id(mrb, mrb->proc_class, MRB_SYM(__oruby_params), proc_params_m, MRB_ARGS_OPT(1));
  if (mrb_class_defined_id(mrb, MRB_SYM(Binding))) {
    struct RClass *bc = mrb_class_get_id(mrb, MRB_SYM(Binding));
    wrap_cfunc(mrb, bc, "local_variables", b_lvars_w, &b_lvars_orig);
    mrb_define_private_method_id(mrb, bc, MRB_SYM(__oruby_implicit_params), binding_implicit_m, MRB_ARGS_NONE());
    mrb_define_private_method_id(mrb, bc, MRB_SYM(__oruby_adopt), binding_adopt_m, MRB_ARGS_REQ(1));
    mrb_define_private_method_id(mrb, bc, MRB_SYM(__oruby_it), binding_it_m, MRB_ARGS_NONE());
  }
  wrap_eval_methods(mrb);
  install_checks(mrb);
  install_method_identity(mrb);
  install_module_names(mrb);
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
  mrb_define_method_id(mrb, mrb_exc_get_id(mrb, MRB_ERROR_SYM(FrozenError)), MRB_SYM(to_s), frozen_error_to_s, MRB_ARGS_NONE());
  _orb_bt_init(mrb);
  _orb_srcrange_init(mrb);
  _orb_prism_serialize_init(mrb);
  _orb_trace_init(mrb);
  mrb_define_class_method(mrb, mrb->symbol_class, "all_symbols", sym_all_symbols_m, MRB_ARGS_NONE());
  if (mrb_class_defined(mrb, "Complex")) {
    mrb_define_class_method(mrb, mrb_class_get(mrb, "Complex"), "__oruby_new__", complex_oruby_new_m, MRB_ARGS_REQ(2));
  }
  mrb_define_private_method(mrb, mrb->module_class, "__alias_from_object", mod_alias_from_object, MRB_ARGS_REQ(2));
  mrb_define_private_method(mrb, mrb->string_class, "__encoding_binary?", str_enc_binary_p, MRB_ARGS_NONE());
  mrb_define_private_method(mrb, mrb->string_class, "__encoding_us_ascii?", str_enc_us_ascii_p, MRB_ARGS_NONE());
  mrb_define_private_method(mrb, mrb->string_class, "__encoding_us_ascii!", str_enc_us_ascii_set, MRB_ARGS_NONE());
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM_Q(__oruby_core_proc), core_proc_p_m, MRB_ARGS_REQ(1));
  mrb_define_class_method_id(mrb, mrb->kernel_module, MRB_SYM_Q(__oruby_bta), block_to_ary_p, MRB_ARGS_ANY());
  mrb_define_private_method_id(mrb, mrb->module_class, MRB_SYM(const_added), mod_const_added_m, MRB_ARGS_REQ(1));
  mrb_define_private_method_id(mrb, mrb->module_class, MRB_SYM(__oruby_const_location), mod_const_location_m, MRB_ARGS_REQ(1));
  if (mrb_class_defined_id(mrb, MRB_SYM(Fiber))) {
    mrb_define_private_method_id(mrb, mrb_class_get_id(mrb, MRB_SYM(Fiber)), MRB_SYM(__oruby_status), fiber_status_m, MRB_ARGS_NONE());
  }
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
  /* the C functions go-fast.c's front ends call: mruby's, with the wrappers
     above (string encodings), before core_ext's Ruby replaces them */
  _orb_fast_capture(mrb);
}

// Make c's own method mid private (the instance half of a module function).
void _mrb_make_method_private(mrb_state *mrb, struct RClass *c, mrb_sym mid) {
  struct RClass *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mid);
  if (MRB_METHOD_UNDEF_P(m) || owner != c) return;
  MRB_METHOD_SET_VISIBILITY(m, MRB_METHOD_PRIVATE_FL);
  mrb_define_method_raw(mrb, c, mid, m);
}
