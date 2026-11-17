// MRI-style backtraces: labels such as 'Foo#bar', 'block in Foo.baz',
// '<class:Foo>', '<main>' and '<top (required)>'.
//
// mruby's packed backtrace keeps only the irep, pc and method name of a
// frame, so the labels are read here from the call frames while they are
// still on the stack: their procs give the block nesting and the scope a
// block is in, the frame's class the method's owner.
//
// The stack is captured for an exception as it is raised: mruby reads `$!`
// (for the implicit cause) while raising, after its own backtrace is kept,
// and `$!` is a virtual global whose getter takes the capture. caller and
// caller_locations capture when called.
//
// Frames of mruby's and core_ext's Ruby-defined core methods (files
// mrblib/, mrbgems/, <internal:...>) are shown as MRI 4.x shows its core
// methods: named, at the location of the code that called them, and
// without the blocks inside them.
#include <string.h>
#include <stdio.h>
#include <pthread.h>
#include <limits.h>
#include <stdlib.h>
#include "go-mrb.h"

enum { BT_NONE, BT_METHOD, BT_CLASS, BT_TOP };

typedef struct {
  const mrb_irep *irep;  /* where the frame is, decoded when shown; NULL: unknown */
  uint32_t idx;
  mrb_sym mid;
  struct RClass *owner;  /* the method's owner, or the class of a class body */
  uint32_t depth;        /* block levels */
  uint8_t kind;
  uint8_t main;          /* BT_TOP: <main>, not <top (required)> */
  uint8_t eval;          /* code run by eval: no absolute path */
  uint8_t internal;      /* a core method: no absolute path */
} bt_frame;

typedef struct {
  mrb_int len;
  bt_frame frames[];
} bt_data;

static void bt_free(mrb_state *mrb, void *ptr) {
  bt_data *bt = (bt_data*)ptr;
  if (!bt) return;
  for (mrb_int i = 0; i < bt->len; i++) {
    if (bt->frames[i].irep) mrb_irep_decref(mrb, (mrb_irep*)bt->frames[i].irep);
  }
  mrb_free(mrb, bt);
}

static const struct mrb_data_type bt_type = {"oruby_backtrace", bt_free};

/* The names used on every raise and rescue, interned once per state: the
   symbol lookup is slow enough to dominate `$!` otherwise. Indexed by the
   state's index (mrb->ud), cleared by _orb_bt_state_closed; an unregistered
   state (index 0) interns each time. */
typedef struct {
  mrb_state *mrb;
  mrb_sym errinfo, bt, owners, main, path, lineno, label, base_label, to_s, abs, locations;
} bt_syms;

#define BT_SYM_CACHE 1024
static bt_syms bt_sym_cache[BT_SYM_CACHE];

static const bt_syms *bt_syms_of(mrb_state *mrb, bt_syms *tmp) {
  mrb_int idx = _mrb_get_idx(mrb);
  bt_syms *s = (idx > 0 && idx < BT_SYM_CACHE) ? &bt_sym_cache[idx] : tmp;
  if (s == tmp || s->mrb != mrb) {
    s->errinfo = mrb_intern_lit(mrb, "__oruby_errinfo__");
    s->bt = mrb_intern_lit(mrb, "__oruby_bt__");
    s->owners = mrb_intern_lit(mrb, "__owners__");
    s->main = mrb_intern_lit(mrb, "__oruby_main__");
    s->path = mrb_intern_lit(mrb, "__path__");
    s->lineno = mrb_intern_lit(mrb, "__lineno__");
    s->label = mrb_intern_lit(mrb, "__label__");
    s->base_label = mrb_intern_lit(mrb, "__base_label__");
    s->to_s = mrb_intern_lit(mrb, "__to_s__");
    s->abs = mrb_intern_lit(mrb, "__abs__");
    s->locations = mrb_intern_lit(mrb, "__bt_locations__");
    s->mrb = mrb;
  }
  return s;
}

#define BT_SYMS(mrb) bt_syms _bt_tmp; const bt_syms *syms = bt_syms_of(mrb, &_bt_tmp)

void _orb_bt_state_closed(mrb_int idx) {
  if (idx > 0 && idx < BT_SYM_CACHE) bt_sym_cache[idx].mrb = NULL;
}

static mrb_bool bt_internal_file(const char *f) {
  return f && (strncmp(f, "mrblib/", 7) == 0 || strncmp(f, "mrbgems/", 8) == 0 ||
               strncmp(f, "<internal:", 10) == 0);
}

/* A frame running Ruby code that pack_backtrace() (backtrace.c) counts:
   with an irep, debug info and a pc. The line is decoded only when shown,
   as mruby does: its line table is read from the start each time. */
typedef struct {
  const mrb_irep *irep;
  uint32_t idx;
  uint8_t valid, internal;
} bt_pos;

static void ci_position(mrb_state *mrb, const mrb_callinfo *ci, bt_pos *pos) {
  const struct RProc *p = ci->proc;
  pos->valid = 0;
  pos->internal = 0;
  pos->irep = NULL;
  pos->idx = 0;
  if (!p || MRB_PROC_CFUNC_P(p)) return;
  const mrb_irep *irep = p->body.irep;
  if (!irep || !irep->debug_info || !ci->pc) return;
  pos->valid = 1;
  pos->irep = irep;
  pos->idx = (uint32_t)(&ci->pc[-1] - irep->iseq);
  pos->internal = bt_internal_file(mrb_debug_get_filename(mrb, irep, pos->idx));
}

/* Whether p is a block written in up's code (not code eval runs there) */
static mrb_bool proc_block_of(const struct RProc *p, const struct RProc *up) {
  const mrb_irep *ui = up->body.irep, *pi = p->body.irep;
  if (!ui || !pi) return TRUE;
  for (int i = 0; i < ui->rlen; i++) {
    if (ui->reps[i] == pi) return TRUE;
  }
  return FALSE;
}

static struct RClass *bt_owner(struct RClass *c) {
  if (c && c->tt == MRB_TT_ICLASS) c = c->c;
  return c;
}

/* The frame below i that runs proc p (the method a block is in) */
static mrb_callinfo *bt_frame_of(mrb_callinfo *cibase, ptrdiff_t i, const struct RProc *p) {
  for (ptrdiff_t j = i, n = 0; j >= 0 && n < 256; j--, n++) {
    if (cibase[j].proc == p) return &cibase[j];
  }
  return NULL;
}

struct bt_mt_find { const mrb_irep *irep; mrb_sym found; };

static int bt_mt_find_i(mrb_state *mrb, mrb_sym mid, mrb_method_t m, void *data) {
  struct bt_mt_find *d = (struct bt_mt_find*)data;
  if (MRB_METHOD_UNDEF_P(m) || MRB_METHOD_CFUNC_P(m)) return 0;
  const struct RProc *p = MRB_METHOD_PROC(m);
  if (p && !MRB_PROC_CFUNC_P(p) && p->body.irep == d->irep) {
    d->found = mid;
    return 1;
  }
  return 0;
}

/* The module a method body was written in, when it is installed elsewhere
   (define_method with an UnboundMethod): MRI names the method after the
   module and its name there */
static struct RClass *bt_def_owner(mrb_state *mrb, struct RClass *found, const struct RProc *p, mrb_sym *mid) {
  struct RClass *def = bt_owner(MRB_PROC_TARGET_CLASS(p));
  if (!def || def == found || def->tt != MRB_TT_MODULE || !found) return found;
  if (found->tt == MRB_TT_SCLASS) {
    /* def self.x, module_function */
    mrb_value att = mrb_iv_get(mrb, mrb_obj_value(found), MRB_SYM(__attached__));
    if (mrb_obj_ptr(att) == (struct RObject*)def) return found;
  }
  struct bt_mt_find d = { p->body.irep, 0 };
  mrb_mt_foreach(mrb, def, bt_mt_find_i, &d);
  if (!d.found) return found;
  *mid = d.found;
  return def;
}

static mrb_bool bt_top_main(mrb_state *mrb, const struct RProc *p, mrb_callinfo *cibase, ptrdiff_t i) {
  BT_SYMS(mrb);
  mrb_value main = mrb_iv_get(mrb, mrb_obj_value(mrb->kernel_module), syms->main);
  if (mrb_string_p(main)) {
    const char *f = p->body.irep ? mrb_debug_get_filename(mrb, p->body.irep, 0) : NULL;
    return f && strlen(f) == (size_t)RSTRING_LEN(main) && memcmp(f, RSTRING_PTR(main), RSTRING_LEN(main)) == 0;
  }
  /* without a main script: the outermost top level */
  for (ptrdiff_t j = i; j >= 0; j--) {
    if (cibase[j].proc != p) continue;
    for (ptrdiff_t k = j - 1; k >= 0; k--) {
      if (cibase[k].proc) return FALSE;
    }
    return TRUE;
  }
  return TRUE;
}

/* The label of the Ruby frame cibase[i] */
static void bt_label(mrb_state *mrb, mrb_callinfo *cibase, ptrdiff_t i, bt_frame *f) {
  mrb_callinfo *ci = &cibase[i];
  const struct RProc *p = ci->proc, *blk = NULL;
  uint32_t depth = 0;

  while (!MRB_PROC_SCOPE_P(p) && p->upper && !MRB_PROC_CFUNC_P(p->upper)) {
    const struct RProc *up = p->upper;
    if (proc_block_of(p, up)) {
      depth++;
      blk = p;
    }
    else {
      f->eval = 1;   /* code eval runs, as transparent as eval is in MRI */
    }
    p = up;
  }
  f->depth = depth;
  if (MRB_PROC_SCOPE_P(p) && p->upper && !MRB_PROC_STRICT_P(p)) {
    /* a class body (OP_EXEC); a method body (OP_METHOD) is strict */
    f->kind = BT_CLASS;
    f->owner = MRB_PROC_TARGET_CLASS(p);
  }
  else if (!p->upper) {
    f->kind = BT_TOP;
    f->main = bt_top_main(mrb, p, cibase, i);
  }
  else if (MRB_PROC_SCOPE_P(p)) {
    mrb_callinfo *m = bt_frame_of(cibase, i, p);
    struct REnv *e = blk ? MRB_PROC_ENV(blk) : NULL;
    f->kind = BT_METHOD;
    if (m) {
      f->mid = m->mid;
      f->owner = bt_def_owner(mrb, bt_owner(mrb_vm_ci_target_class(m)), p, &f->mid);
    }
    else if (e) {
      f->owner = e->c;
      f->mid = e->mid;
    }
    else {
      f->owner = MRB_PROC_TARGET_CLASS(p);
      f->mid = ci->mid;
    }
    if (!f->mid) f->kind = BT_NONE;
  }
  else {
    /* a proc made in a C function */
    f->mid = ci->mid;
    f->owner = mrb_vm_ci_target_class(ci);
    f->kind = f->mid ? BT_METHOD : BT_NONE;
  }
  f->owner = bt_owner(f->owner);
}

/* The frames from ci index top down; *rawp is the number of frames mruby's
   packed backtrace has for the same stack. The owners are kept in owners. */
static bt_data *bt_walk(mrb_state *mrb, ptrdiff_t top, mrb_int *rawp, mrb_value owners) {
  mrb_callinfo *cibase = mrb->c->cibase;
  ptrdiff_t n = top + 1;
  bt_pos *pos = (bt_pos*)mrb_malloc(mrb, sizeof(bt_pos) * (n > 0 ? n : 1));
  ptrdiff_t *below = (ptrdiff_t*)mrb_malloc(mrb, sizeof(ptrdiff_t) * (n > 0 ? n : 1));
  bt_data *bt = (bt_data*)mrb_malloc(mrb, sizeof(bt_data) + sizeof(bt_frame) * (n > 0 ? n : 1));
  mrb_int raw = 0, len = 0;
  struct RClass *last_owner = NULL;

  /* below[i]: the nearest frame at or below i running non-core Ruby code */
  for (ptrdiff_t i = 0; i < n; i++) {
    ci_position(mrb, &cibase[i], &pos[i]);
    below[i] = (pos[i].valid && !pos[i].internal) ? i : (i > 0 ? below[i-1] : -1);
  }
  for (ptrdiff_t i = top; i >= 0; i--) {
    mrb_callinfo *ci = &cibase[i];
    bt_frame f;
    ptrdiff_t at;

    memset(&f, 0, sizeof(f));
    if (ci->proc && !MRB_PROC_CFUNC_P(ci->proc)) {
      if (!pos[i].valid) continue;
      raw++;
      bt_label(mrb, cibase, i, &f);
      at = i;
      if (pos[i].internal) {
        if (f.depth > 0) continue;
        f.internal = 1;
        if (i > 0 && below[i-1] >= 0) at = below[i-1];
      }
    }
    else {
      if (!ci->mid) continue;
      raw++;
      f.kind = BT_METHOD;
      f.mid = ci->mid;
      f.owner = bt_owner(mrb_vm_ci_target_class(ci));
      f.internal = 1;
      at = (i > 0) ? below[i-1] : -1;
      if (at < 0) {
        /* as pack_backtrace: the nearest Ruby frame, core or not */
        for (ptrdiff_t j = i - 1; j >= 0; j--) {
          if (pos[j].valid) { at = j; break; }
        }
      }
    }
    if (at >= 0 && pos[at].irep && pos[at].irep->refcnt < UINT16_MAX) {
      /* kept as mruby's packed backtrace keeps it (copy_backtrace()) */
      f.irep = pos[at].irep;
      f.idx = pos[at].idx;
      mrb_irep_incref(mrb, (mrb_irep*)f.irep);
    }
    if (f.owner && f.owner != last_owner) {
      mrb_ary_push(mrb, owners, mrb_obj_value(f.owner));
      last_owner = f.owner;
    }
    bt->frames[len++] = f;
  }
  bt->len = len;
  mrb_free(mrb, pos);
  mrb_free(mrb, below);
  *rawp = raw;
  return bt;
}

/* The stack from ci index top down, as a Data object */
static mrb_value bt_capture(mrb_state *mrb, ptrdiff_t top, mrb_int *rawp) {
  BT_SYMS(mrb);
  mrb_value owners = mrb_ary_new(mrb);
  struct RData *d = mrb_data_object_alloc(mrb, NULL, NULL, &bt_type);
  mrb_value obj = mrb_obj_value(d);
  mrb_iv_set(mrb, obj, syms->owners, owners);
  d->data = bt_walk(mrb, top, rawp, owners);
  return obj;
}

#define BT_IVAR(mrb) (bt_syms_of(mrb, &_bt_tmp)->bt)
#define BT_TMP bt_syms _bt_tmp

/* Keeps the current stack for exc, which is being raised */
void _orb_bt_keep(mrb_state *mrb, mrb_value exc) {
  BT_TMP;
  if (!mrb_exception_p(exc) || mrb_frozen_p(mrb_obj_ptr(exc))) return;
  struct RException *e = mrb_exc_ptr(exc);
  if (!e->backtrace || e->backtrace->tt != MRB_TT_BACKTRACE) return;
  if (mrb_iv_defined(mrb, exc, BT_IVAR(mrb))) return;
  if (!mrb->c->ci) return;
  ptrdiff_t top = mrb->c->ci - mrb->c->cibase;
  if (top >= mrb->c->ciend - mrb->c->cibase) top = mrb->c->ciend - mrb->c->cibase;
  int ai = mrb_gc_arena_save(mrb);
  mrb_int raw;
  mrb_value bt = bt_capture(mrb, top, &raw);
  /* the same frames mruby kept, or this is not the raise that kept them */
  if (raw == mrb_backtrace_len(mrb_obj_value(e->backtrace))) {
    mrb_iv_set(mrb, exc, BT_IVAR(mrb), bt);
  }
  mrb_gc_arena_restore(mrb, ai);
}

/* Keeps the current stack for exc, about to be raised from the C function
   running now (raise with cause:, which mruby raises without reading `$!`) */
void _orb_bt_keep_now(mrb_state *mrb, mrb_value exc) {
  BT_TMP;
  if (!mrb_exception_p(exc) || mrb_frozen_p(mrb_obj_ptr(exc))) return;
  if (mrb_exc_ptr(exc)->backtrace || !mrb->c->ci) return;
  int ai = mrb_gc_arena_save(mrb);
  mrb_int raw;
  mrb_value bt = bt_capture(mrb, mrb->c->ci - mrb->c->cibase, &raw);
  mrb_iv_set(mrb, exc, BT_IVAR(mrb), bt);
  mrb_gc_arena_restore(mrb, ai);
}

void _orb_bt_forget(mrb_state *mrb, mrb_value exc) {
  BT_TMP;
  mrb_iv_remove(mrb, exc, BT_IVAR(mrb));
}

// $!: a virtual global, so that its read in mrb_exc_set() (error.c) takes
// the stack of the exception being raised
static mrb_value errinfo_get(mrb_state *mrb) {
  if (mrb->exc && mrb->exc->tt == MRB_TT_EXCEPTION) _orb_bt_keep(mrb, mrb_obj_value(mrb->exc));
  BT_SYMS(mrb);
  return mrb_iv_get(mrb, mrb_obj_value(mrb->kernel_module), syms->errinfo);
}

static void errinfo_set(mrb_state *mrb, mrb_value v) {
  BT_SYMS(mrb);
  mrb_iv_set(mrb, mrb_obj_value(mrb->kernel_module), syms->errinfo, v);
}

/* Labels */

static mrb_value bt_class_name(mrb_state *mrb, struct RClass *c) {
  if (!c || (c->tt != MRB_TT_CLASS && c->tt != MRB_TT_MODULE)) return mrb_nil_value();
  mrb_value path = mrb_class_path(mrb, c);
  if (!mrb_string_p(path) || RSTRING_LEN(path) == 0 || RSTRING_PTR(path)[0] == '#') return mrb_nil_value();
  return path;
}

/* MRI's name of a method: Owner#name, Owner.name, or name */
static mrb_value bt_method_label(mrb_state *mrb, struct RClass *owner, mrb_sym mid) {
  mrb_value name = mrb_nil_value();
  const char *sep = "#";
  if (owner && owner->tt == MRB_TT_SCLASS) {
    mrb_value att = mrb_iv_get(mrb, mrb_obj_value(owner), MRB_SYM(__attached__));
    if (mrb_class_p(att) || mrb_module_p(att)) name = bt_class_name(mrb, mrb_class_ptr(att));
    sep = ".";
  }
  else {
    name = bt_class_name(mrb, owner);
  }
  if (mrb_nil_p(name)) return mrb_sym_str(mrb, mid);
  mrb_str_cat_cstr(mrb, name, sep);
  mrb_str_cat_cstr(mrb, name, mrb_sym_name(mrb, mid));
  return name;
}

static mrb_value bt_base_label(mrb_state *mrb, const bt_frame *f) {
  switch (f->kind) {
  case BT_METHOD:
    return mrb_sym_str(mrb, f->mid);
  case BT_CLASS: {
    struct RClass *c = f->owner;
    if (!c || c->tt == MRB_TT_SCLASS) return mrb_str_new_lit(mrb, "singleton class");
    const char *name = mrb_class_name(mrb, c), *p;
    if (!name) name = "";
    while ((p = strstr(name, "::")) != NULL) name = p + 2;
    return mrb_format(mrb, c->tt == MRB_TT_MODULE ? "<module:%s>" : "<class:%s>", name);
  }
  case BT_TOP:
    return f->main ? mrb_str_new_lit(mrb, "<main>") : mrb_str_new_lit(mrb, "<top (required)>");
  default:
    return mrb_nil_value();
  }
}

static mrb_value bt_full_label(mrb_state *mrb, const bt_frame *f) {
  mrb_value base = f->kind == BT_METHOD ? bt_method_label(mrb, f->owner, f->mid) : bt_base_label(mrb, f);
  if (mrb_nil_p(base) || f->depth == 0) return base;
  mrb_value s = f->depth == 1 ? mrb_str_new_lit(mrb, "block in ")
                              : mrb_format(mrb, "block (%d levels) in ", (int)f->depth);
  mrb_str_cat_str(mrb, s, base);
  return s;
}

static const char *bt_file_line(mrb_state *mrb, const bt_frame *f, int32_t *line) {
  const char *file = NULL;
  *line = 0;
  if (!f->irep || !mrb_debug_get_position(mrb, f->irep, f->idx, line, &file)) return NULL;
  if (*line < 0) *line = 0;
  return file;
}

static mrb_value bt_string_at(mrb_state *mrb, const char *file, int32_t line, mrb_value label) {
  mrb_value s = file ? mrb_str_new_cstr(mrb, file) : mrb_str_new_lit(mrb, "(unknown)");
  char buf[16];
  snprintf(buf, sizeof(buf), ":%d", (int)line);
  mrb_str_cat_cstr(mrb, s, buf);
  if (!mrb_nil_p(label)) {
    mrb_str_cat_lit(mrb, s, ":in '");
    mrb_str_cat_str(mrb, s, label);
    mrb_str_cat_lit(mrb, s, "'");
  }
  return s;
}

static mrb_value bt_string(mrb_state *mrb, const bt_frame *f, mrb_value label) {
  int32_t line;
  const char *file = bt_file_line(mrb, f, &line);
  return bt_string_at(mrb, file, line, label);
}

static bt_data *bt_get(mrb_state *mrb, mrb_value obj) {
  return (bt_data*)mrb_data_get_ptr(mrb, obj, &bt_type);
}

static mrb_value bt_strings(mrb_state *mrb, bt_data *bt, mrb_int lev, mrb_int n) {
  mrb_value ary = mrb_ary_new_capa(mrb, n);
  int ai = mrb_gc_arena_save(mrb);
  for (mrb_int i = lev; i < lev + n && i < bt->len; i++) {
    const bt_frame *f = &bt->frames[i];
    mrb_ary_push(mrb, ary, bt_string(mrb, f, bt_full_label(mrb, f)));
    mrb_gc_arena_restore(mrb, ai);
  }
  return ary;
}

static struct RClass *bt_location_class(mrb_state *mrb) {
  struct RClass *thread = mrb_class_get_id(mrb, MRB_SYM(Thread));
  struct RClass *backtrace = mrb_class_get_under_id(mrb, thread, MRB_SYM(Backtrace));
  return mrb_class_get_under_id(mrb, backtrace, MRB_SYM(Location));
}

static mrb_value bt_location(mrb_state *mrb, struct RClass *cls, const bt_frame *f) {
  struct RObject *loc = MRB_OBJ_ALLOC(mrb, MRB_TT_OBJECT, cls);
  BT_SYMS(mrb);
  mrb_value label = bt_full_label(mrb, f);
  int32_t line;
  const char *file = bt_file_line(mrb, f, &line);
  mrb_obj_iv_set_force(mrb, loc, syms->path,
                       file ? mrb_str_new_cstr(mrb, file) : mrb_str_new_lit(mrb, "(unknown)"));
  mrb_obj_iv_set_force(mrb, loc, syms->lineno, mrb_fixnum_value(line));
  mrb_obj_iv_set_force(mrb, loc, syms->label, label);
  mrb_obj_iv_set_force(mrb, loc, syms->base_label, bt_base_label(mrb, f));
  mrb_obj_iv_set_force(mrb, loc, syms->to_s, bt_string_at(mrb, file, line, label));
  mrb_obj_iv_set_force(mrb, loc, syms->abs, mrb_bool_value(!f->eval && !f->internal && file));
  return mrb_obj_value(loc);
}

static mrb_value bt_locations(mrb_state *mrb, bt_data *bt, mrb_int lev, mrb_int n) {
  struct RClass *cls = bt_location_class(mrb);
  mrb_value ary = mrb_ary_new_capa(mrb, n);
  int ai = mrb_gc_arena_save(mrb);
  for (mrb_int i = lev; i < lev + n && i < bt->len; i++) {
    mrb_ary_push(mrb, ary, bt_location(mrb, cls, &bt->frames[i]));
    mrb_gc_arena_restore(mrb, ai);
  }
  return ary;
}

/* Exception#backtrace and #backtrace_locations */

static mrb_func_t exc_backtrace_orig, exc_locations_orig;

static mrb_value exc_backtrace_m(mrb_state *mrb, mrb_value exc) {
  BT_TMP;
  struct RException *e = mrb_exc_ptr(exc);
  if (e->backtrace && e->backtrace->tt == MRB_TT_BACKTRACE) {
    mrb_sym cache = MRB_SYM(__backtrace__);
    mrb_value strs = mrb_iv_get(mrb, exc, cache);
    if (mrb_array_p(strs)) return strs;
    mrb_value bt = mrb_iv_get(mrb, exc, BT_IVAR(mrb));
    if (mrb_data_p(bt) && DATA_TYPE(bt) == &bt_type) {
      bt_data *d = bt_get(mrb, bt);
      strs = bt_strings(mrb, d, 0, d->len);
      mrb_obj_iv_set_force(mrb, mrb_obj_ptr(exc), cache, strs);
      return strs;
    }
  }
  if (e->backtrace && e->backtrace->tt == MRB_TT_BACKTRACE) {
    /* raised without a capture (raise with cause:): mruby's frames, quoted */
    mrb_value packed = mrb_obj_value(e->backtrace);
    mrb_int n = mrb_backtrace_len(packed);
    mrb_value strs = mrb_ary_new_capa(mrb, n);
    int ai = mrb_gc_arena_save(mrb);
    for (mrb_int i = 0; i < n; i++) {
      const char *file = NULL;
      int32_t line = 0;
      mrb_sym mid = 0;
      if (!mrb_backtrace_location(mrb, packed, i, &file, &line, &mid, NULL)) file = NULL;
      if (line < 0) line = 0;
      mrb_ary_push(mrb, strs, bt_string_at(mrb, file, line, mid ? mrb_sym_str(mrb, mid) : mrb_nil_value()));
      mrb_gc_arena_restore(mrb, ai);
    }
    mrb_obj_iv_set_force(mrb, mrb_obj_ptr(exc), MRB_SYM(__backtrace__), strs);
    return strs;
  }
  return exc_backtrace_orig(mrb, exc);
}

static mrb_value exc_locations_m(mrb_state *mrb, mrb_value exc) {
  BT_TMP;
  mrb_sym cache = bt_syms_of(mrb, &_bt_tmp)->locations;
  mrb_value locs = mrb_iv_get(mrb, exc, cache);
  if (mrb_array_p(locs)) return locs;
  struct RException *e = mrb_exc_ptr(exc);
  if (e->backtrace && e->backtrace->tt == MRB_TT_BACKTRACE) {
    mrb_value bt = mrb_iv_get(mrb, exc, BT_IVAR(mrb));
    if (mrb_data_p(bt) && DATA_TYPE(bt) == &bt_type) {
      bt_data *d = bt_get(mrb, bt);
      locs = bt_locations(mrb, d, 0, d->len);
      mrb_obj_iv_set_force(mrb, mrb_obj_ptr(exc), cache, locs);
      return locs;
    }
  }
  return exc_locations_orig ? exc_locations_orig(mrb, exc) : mrb_nil_value();
}

/* caller and caller_locations: the frames after the method calling them */

static mrb_bool caller_range(mrb_state *mrb, mrb_int bt_len, mrb_int *levp, mrb_int *np) {
  mrb_value v, len;
  mrb_int argc, lev, n = 0;

  argc = mrb_get_args(mrb, "|oo", &v, &len);
  if (argc == 2) {
    if (mrb_nil_p(len)) argc = 1;
    else n = mrb_as_int(mrb, len);
  }
  switch (argc) {
  case 0:
    lev = 1;
    n = bt_len - 1;
    break;
  case 1:
    if (mrb_range_p(v)) {
      mrb_int beg, rlen;
      if (mrb_range_beg_len(mrb, v, &beg, &rlen, bt_len, TRUE) != MRB_RANGE_OK) return FALSE;
      lev = beg;
      n = rlen;
    }
    else {
      lev = mrb_as_int(mrb, v);
      if (lev < 0) mrb_raisef(mrb, E_ARGUMENT_ERROR, "negative level (%v)", v);
      n = bt_len - lev;
    }
    break;
  default:
    lev = mrb_as_int(mrb, v);
    break;
  }
  if (lev >= bt_len) return FALSE;
  if (lev < 0) mrb_raisef(mrb, E_ARGUMENT_ERROR, "negative level (%v)", v);
  if (n < 0) mrb_raisef(mrb, E_ARGUMENT_ERROR, "negative size (%d)", n);
  if (bt_len <= n + lev) n = bt_len - lev - 1;
  *levp = lev;
  *np = n;
  return TRUE;
}

/* The current stack without the caller/caller_locations frame itself */
static bt_data *caller_capture(mrb_state *mrb, mrb_value *keep) {
  mrb_int raw;
  ptrdiff_t top = mrb->c->ci - mrb->c->cibase;
  /* the frame calling caller is at 0 */
  *keep = bt_capture(mrb, top - 1, &raw);
  return bt_get(mrb, *keep);
}

static mrb_value f_caller(mrb_state *mrb, mrb_value self) {
  mrb_value keep;
  bt_data *bt = caller_capture(mrb, &keep);
  mrb_int lev, n;
  if (!caller_range(mrb, bt->len + 1, &lev, &n)) return mrb_nil_value();
  return bt_strings(mrb, bt, lev, n);
}

static mrb_value f_caller_locations(mrb_state *mrb, mrb_value self) {
  mrb_value keep;
  bt_data *bt = caller_capture(mrb, &keep);
  mrb_int lev, n;
  if (!caller_range(mrb, bt->len + 1, &lev, &n)) return mrb_nil_value();
  return bt_locations(mrb, bt, lev, n);
}

/* Thread::Backtrace::Location */

static mrb_func_t loc_orig[6];
static const char *loc_names[6] = {"path", "lineno", "label", "base_label", "to_s", "inspect"};
static const char *loc_ivars[6] = {"__path__", "__lineno__", "__label__", "__base_label__", "__to_s__", "__to_s__"};

static mrb_value loc_call(mrb_state *mrb, mrb_value self, int i) {
  mrb_sym iv = mrb_intern_cstr(mrb, loc_ivars[i]);
  if (!mrb_iv_defined(mrb, self, iv)) {
    return loc_orig[i] ? loc_orig[i](mrb, self) : mrb_nil_value();
  }
  mrb_value v = mrb_iv_get(mrb, self, iv);
  if (i == 5) return mrb_str_inspect(mrb, v);
  if (mrb_string_p(v)) return mrb_str_dup(mrb, v);
  return v;
}

static mrb_value loc_m0(mrb_state *mrb, mrb_value self) { return loc_call(mrb, self, 0); }
static mrb_value loc_m1(mrb_state *mrb, mrb_value self) { return loc_call(mrb, self, 1); }
static mrb_value loc_m2(mrb_state *mrb, mrb_value self) { return loc_call(mrb, self, 2); }
static mrb_value loc_m3(mrb_state *mrb, mrb_value self) { return loc_call(mrb, self, 3); }
static mrb_value loc_m4(mrb_state *mrb, mrb_value self) { return loc_call(mrb, self, 4); }
static mrb_value loc_m5(mrb_state *mrb, mrb_value self) { return loc_call(mrb, self, 5); }

/* The real path of the location's file; nil for code eval ran and for core
   methods */
static mrb_func_t loc_abs_orig;

static mrb_value loc_absolute_path(mrb_state *mrb, mrb_value self) {
  mrb_sym abs = mrb_intern_lit(mrb, "__abs__");
  if (!mrb_iv_defined(mrb, self, abs)) {
    return loc_abs_orig ? loc_abs_orig(mrb, self) : mrb_nil_value();
  }
  if (!mrb_test(mrb_iv_get(mrb, self, abs))) return mrb_nil_value();
  mrb_value path = mrb_iv_get(mrb, self, mrb_intern_lit(mrb, "__path__"));
  if (!mrb_string_p(path)) return mrb_nil_value();
  char buf[PATH_MAX];
  if (realpath(mrb_string_cstr(mrb, path), buf)) return mrb_str_new_cstr(mrb, buf);
  return mrb_str_dup(mrb, path);
}

static mrb_func_t bt_replace(mrb_state *mrb, struct RClass *c, const char *name, mrb_func_t w, mrb_aspec aspec, mrb_bool priv) {
  struct RClass *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, mrb_intern_cstr(mrb, name));
  mrb_func_t orig = NULL;
  if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_CFUNC_P(m) && MRB_METHOD_CFUNC(m) != w) orig = MRB_METHOD_CFUNC(m);
  if (priv) mrb_define_private_method(mrb, c, name, w, aspec);
  else mrb_define_method(mrb, c, name, w, aspec);
  return orig;
}

/* the originals are the same functions in every state */
void _orb_bt_init(mrb_state *mrb, pthread_mutex_t *mu) {
  mrb_func_t locw[6] = {loc_m0, loc_m1, loc_m2, loc_m3, loc_m4, loc_m5};
  struct RClass *ksing = mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(mrb->kernel_module)));
  mrb_func_t o;

  pthread_mutex_lock(mu);
  o = bt_replace(mrb, mrb->eException_class, "backtrace", exc_backtrace_m, MRB_ARGS_NONE(), FALSE);
  if (o) exc_backtrace_orig = o;
  o = bt_replace(mrb, mrb->eException_class, "backtrace_locations", exc_locations_m, MRB_ARGS_NONE(), FALSE);
  if (o) exc_locations_orig = o;
  if (mrb_class_defined_id(mrb, MRB_SYM(Thread))) {
    struct RClass *loc = bt_location_class(mrb);
    for (int i = 0; i < 6; i++) {
      o = bt_replace(mrb, loc, loc_names[i], locw[i], MRB_ARGS_NONE(), FALSE);
      if (o) loc_orig[i] = o;
    }
    o = bt_replace(mrb, loc, "absolute_path", loc_absolute_path, MRB_ARGS_NONE(), FALSE);
    if (o) loc_abs_orig = o;
  }
  pthread_mutex_unlock(mu);
  mrb_define_private_method(mrb, mrb->kernel_module, "caller", f_caller, MRB_ARGS_OPT(2));
  mrb_define_private_method(mrb, mrb->kernel_module, "caller_locations", f_caller_locations, MRB_ARGS_OPT(2));
  mrb_define_method(mrb, ksing, "caller", f_caller, MRB_ARGS_OPT(2));
  mrb_define_method(mrb, ksing, "caller_locations", f_caller_locations, MRB_ARGS_OPT(2));

  mrb_sym bang = mrb_intern_lit(mrb, "$!");
  errinfo_set(mrb, mrb_gv_get(mrb, bang));
  mrb_gv_define_virtual(mrb, bang, errinfo_get, errinfo_set);
}
