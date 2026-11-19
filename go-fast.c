// C front ends for core methods that core_ext redefines in Ruby.
//
// core_ext's Ruby makes mruby's methods behave as MRI's: argument
// conversion, coercion, error messages, encodings, rare argument forms. In
// the common case most of those definitions end in the builtin they wrap, but
// the Ruby frames (and the splats, the bind_call of 00_convert's implicit
// wrappers) cost 2-100x the builtin; a Ruby Array#[] also turns off the VM's
// inline indexing.
//
// Each entry of fast_defs gets, once all of core_ext is loaded
// (_orb_fast_install), a C method in place of the Ruby one, which is kept as
// __oruby_slow_<name>. The C method tests a predicate on the arguments: when
// it holds, the builtin mruby had at startup (captured by _orb_fast_capture,
// before any Ruby redefined it) or a C version of our own answers, as the
// Ruby chain would; otherwise the call goes on, in the same frame
// (mrb_exec_irep, as send does), to the Ruby method, arguments, keywords and
// block as given. A predicate must only accept arguments for which the Ruby
// definition returns what the fast path returns.
//
// The C method takes the Ruby method's argument spec (its OP_ENTER operand),
// so arity errors, #arity and #parameters stay as they were.
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include <float.h>
#include "go-mrb.h"
#include <mruby/opcode.h>
#include <mruby/proc.h>
#include <mruby/hash.h>
#include <mruby/variable.h>
#include <mruby/throw.h>

mrb_value mrb_exec_irep(mrb_state *mrb, mrb_value self, const struct RProc *p);
mrb_int mrb_ci_bidx(mrb_callinfo *ci);
void mrb_idx_op_rearm(mrb_state *mrb, enum mrb_idx_op_slot slot);

typedef mrb_bool fast_pred_fn(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk);
typedef fast_pred_fn *fast_pred_f;

typedef struct {
  const char *cls;     /* class name */
  mrb_bool single;     /* a singleton method of the class */
  const char *name;
  fast_pred_f pred;    /* the fast path answers */
  mrb_func_t direct;   /* our own C for the fast path; NULL: the builtin */
  int slot;            /* VM inline slot (MRB_IDX_OP_*) re-armed after install, or -1 */
} fast_def;

/* ---- predicates ---- */

static inline mrb_bool int_p(mrb_value v) { return mrb_integer_p(v) || mrb_bigint_p(v); }
static inline mrb_bool num_p(mrb_value v) { return int_p(v) || mrb_float_p(v); }

#define ARGC_IS(n) (argc == (n))
static mrb_bool p_int1(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return ARGC_IS(1) && int_p(argv[0]) && mrb_nil_p(blk);
}
static mrb_bool p_num1(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return ARGC_IS(1) && num_p(argv[0]) && mrb_nil_p(blk);
}
static mrb_bool p_none(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_nil_p(blk);
}
/* no arguments, or a zero digit count */
static mrb_bool p_ndigits0(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk)) return FALSE;
  return argc == 0 || (argc == 1 && mrb_integer_p(argv[0]) && mrb_integer(argv[0]) == 0);
}
/* an Integer's round/floor/ceil/truncate: no digits, or digits >= 0 */
static mrb_bool p_int_ndigits(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk)) return FALSE;
  return argc == 0 || (argc == 1 && mrb_integer_p(argv[0]) && mrb_integer(argv[0]) >= 0 &&
                       mrb_integer(argv[0]) <= 2147483647);
}
static mrb_value d_self(mrb_state *mrb, mrb_value self) { return self; }

/* Integer#fdiv: Integers both within 53 bits (exact as doubles), or a Float */
#define FDIV_MAX ((mrb_int)1 << 53)
static mrb_bool p_fdiv(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 1 || !mrb_nil_p(blk) || !mrb_integer_p(self)) return FALSE;
  mrb_int s = mrb_integer(self);
  if (s > FDIV_MAX || s < -FDIV_MAX) return FALSE;
  if (mrb_integer_p(argv[0])) {
    mrb_int o = mrb_integer(argv[0]);
    return o != 0 && o <= FDIV_MAX && o >= -FDIV_MAX;
  }
  return mrb_float_p(argv[0]) && mrb_float(argv[0]) != 0.0;
}

/* Integer#**: a non-negative Integer, or a Float for a non-negative self */
static mrb_bool p_int_pow(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 1 || !mrb_nil_p(blk) || !mrb_integer_p(self)) return FALSE;
  mrb_value o = argv[0];
  if (mrb_integer_p(o)) return mrb_integer(o) >= 0;
  if (mrb_float_p(o)) return mrb_integer(self) >= 0;
  return FALSE;
}

/* Integer#[]: one Integer bit index of a fixnum */
static mrb_bool p_int_aref(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_integer_p(self) && mrb_integer_p(argv[0]) && mrb_nil_p(blk);
}
static mrb_value d_int_aref(mrb_state *mrb, mrb_value self) {
  mrb_int v = mrb_integer(self), i = mrb_integer(mrb_get_argv(mrb)[0]);
  if (i < 0) return mrb_fixnum_value(0);
  if (i >= 63) return mrb_fixnum_value(v < 0 ? 1 : 0);
  return mrb_fixnum_value((v >> i) & 1);
}

static mrb_value d_int_coerce(mrb_state *mrb, mrb_value self) {
  mrb_value pair[2] = {mrb_get_argv(mrb)[0], self};
  return mrb_ary_new_from_values(mrb, 2, pair);
}

static mrb_bool p_fixnum_self(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_nil_p(blk) && mrb_integer_p(self);
}
static mrb_value d_int_zero_p(mrb_state *mrb, mrb_value self) { return mrb_bool_value(mrb_integer(self) == 0); }
static mrb_value d_int_pred(mrb_state *mrb, mrb_value self) {
  mrb_int v = mrb_integer(self);
  if (v == MRB_INT_MIN) return mrb_funcall_id(mrb, self, MRB_OPSYM(sub), 1, mrb_fixnum_value(1));
  return mrb_fixnum_value(v - 1);
}
static mrb_value d_int_uminus(mrb_state *mrb, mrb_value self) {
  mrb_int v = mrb_integer(self);
  if (v == MRB_INT_MIN) return mrb_funcall_id(mrb, mrb_fixnum_value(0), MRB_OPSYM(sub), 1, self);
  return mrb_fixnum_value(-v);
}
static mrb_bool p_flo_self(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_nil_p(blk) && mrb_float_p(self);
}
static mrb_value d_flo_uminus(mrb_state *mrb, mrb_value self) { return mrb_float_value(mrb, -mrb_float(self)); }

/* Float#%: a number, unless self is -0.0 (MRI keeps the sign) */
static mrb_bool p_flo_mod(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 1 || !mrb_nil_p(blk) || !num_p(argv[0])) return FALSE;
  mrb_float f = mrb_float(self);
  return !(f == 0.0 && signbit(f));
}

/* Float#**: an Integer, or a Float that gives a real result */
static mrb_bool p_flo_pow(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 1 || !mrb_nil_p(blk)) return FALSE;
  mrb_value o = argv[0];
  if (mrb_integer_p(o)) return TRUE;
  if (!mrb_float_p(o)) return FALSE;
  mrb_float s = mrb_float(self), e = mrb_float(o);
  return !(s < 0 && isfinite(e) && e != floor(e));
}

/* Float#round: MRI's (numeric.c flo_round) for no digits within the
   Integer range, and for 1..14 digits rounding half up */
static mrb_bool p_flo_round(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk)) return FALSE;
  mrb_float f = mrb_float(self);
  if (argc == 0 || (argc == 1 && mrb_integer_p(argv[0]) && mrb_integer(argv[0]) == 0)) {
    return isfinite(f) && fabs(f) < 4.0e18;
  }
  if (argc != 1 || !mrb_integer_p(argv[0])) return FALSE;
  mrb_int nd = mrb_integer(argv[0]);
  return nd > 0 && nd <= 14 && isfinite(f);
}

static mrb_func_t flo_round_builtin;

/* MRI's round_half_up(x, s) */
static double round_half_up(double x, double s) {
  double f, xs = x * s;
  f = round(xs);
  if (s == 1.0) return f;
  if (x > 0) {
    if ((double)((f + 0.5) / s) <= x) f += 1;
    x = f;
  }
  else {
    if ((double)((f - 0.5) / s) >= x) f -= 1;
    x = f;
  }
  return x;
}

/* MRI's float_round_overflow / float_round_underflow */
static int float_round_overflow(int ndigits, int binexp) {
  enum { float_dig = DBL_DIG + 2 };
  return ndigits >= float_dig - (binexp > 0 ? binexp / 4 : binexp / 3 - 1);
}
static int float_round_underflow(int ndigits, int binexp) {
  return ndigits < -(binexp > 0 ? binexp / 3 + 1 : binexp / 4);
}

static mrb_value d_flo_round(mrb_state *mrb, mrb_value self) {
  mrb_int argc = mrb_get_argc(mrb);
  if (argc == 0 || mrb_integer(mrb_get_argv(mrb)[0]) == 0) return flo_round_builtin(mrb, self);
  int nd = (int)mrb_integer(mrb_get_argv(mrb)[0]);
  double number = mrb_float(self);
  if (number == 0.0) return self;
  int binexp;
  frexp(number, &binexp);
  if (float_round_overflow(nd, binexp)) return self;
  if (float_round_underflow(nd, binexp)) return mrb_float_value(mrb, 0.0);
  double f = pow(10, nd);
  return mrb_float_value(mrb, round_half_up(number, f) / f);
}

/* ---- Array ---- */

/* Array#[] / #[]=: an Integer index (and value) — what the builtin and the VM's
   inline indexing answer alike */
static mrb_bool p_ary_aref(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_integer_p(argv[0]) && mrb_nil_p(blk);
}
static mrb_bool p_ary_aset(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 2 && mrb_integer_p(argv[0]) && mrb_nil_p(blk) && !mrb_frozen_p(mrb_basic_ptr(self));
}

/* Array iteration, in C as MRI's (array.c, enum.c): a block yielded each
   element in turn, the length read again at each step. Without a block, a
   lambda block for #map (MRI forces its arguments), or arguments core_ext's
   Ruby handles, the call goes on to that Ruby. */

/* A block may Fiber.yield (an Enumerator's #next runs the iteration in a
   fiber), which mruby cannot do across a C frame: the C iterators run only
   on the thread's root fiber; in a fiber the Ruby ones do. */
static inline mrb_bool root_fiber(mrb_state *mrb) { return mrb->c == mrb->root_c; }

/* the block of the call (a frame of a yield has no block slot) */
static inline mrb_value ci_blk(mrb_callinfo *ci) {
  return ci->blk ? mrb_obj_value(ci->blk) : mrb_nil_value();
}
static mrb_value cur_blk(mrb_state *mrb) {
  return ci_blk(mrb->c->ci);
}

static mrb_bool p_blk0(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_proc_p(blk) && root_fiber(mrb);
}
static mrb_bool symproc_p(const struct RProc *p);
/* a block, not a lambda (MRI forces a lambda's arguments for #map); a
   Symbol#to_proc lambda takes a yielded Array as one argument already */
static mrb_bool p_blk0_proc(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_proc_p(blk) && (!MRB_PROC_STRICT_P(mrb_proc_ptr(blk)) || symproc_p(mrb_proc_ptr(blk))) &&
         root_fiber(mrb);
}

static mrb_func_t ary_each_fn; /* our Array#each, once installed */

/* a Symbol#to_proc block yielded one value: its public method, called
   directly (no frame for the lambda); undef when it cannot be */
static mrb_value symproc_call(mrb_state *mrb, mrb_value self);
static mrb_sym blk_sym(mrb_value blk) {
  if (!mrb_proc_p(blk)) return 0;
  struct RProc *p = mrb_proc_ptr(blk);
  if (!MRB_PROC_CFUNC_P(p) || MRB_PROC_CFUNC(p) != symproc_call || !MRB_PROC_ENV_P(p)) return 0;
  mrb_value s = MRB_PROC_ENV(p)->stack[0];
  return mrb_symbol_p(s) ? mrb_symbol(s) : 0;
}
static mrb_value yield1(mrb_state *mrb, mrb_value blk, mrb_sym sym, mrb_value e) {
  if (sym) {
    struct RClass *c = mrb_class(mrb, e);
    mrb_method_t m = mrb_method_search_vm(mrb, &c, sym);
    if (!MRB_METHOD_UNDEF_P(m)) {
      int vis = MRB_METHOD_VISIBILITY(m);
      if (vis != MRB_METHOD_PRIVATE_FL && vis != MRB_METHOD_PROTECTED_FL) {
        return mrb_funcall_with_block(mrb, e, sym, 0, NULL, mrb_nil_value());
      }
    }
  }
  return mrb_yield(mrb, blk, e);
}

/* an Array whose #each is Array's (an Enumerable method may skip calling it) */
static mrb_bool ary_plain_each(mrb_state *mrb, mrb_value self) {
  if (!mrb_array_p(self)) return FALSE;
  struct RClass *c = mrb_class(mrb, self);
  if (c == mrb->array_class && ary_each_fn) {
    mrb_method_t m = mrb_method_search_vm(mrb, &c, MRB_SYM(each));
    return MRB_METHOD_FUNC_P(m) && MRB_METHOD_FUNC(m) == ary_each_fn;
  }
  if (!ary_each_fn) return FALSE;
  mrb_method_t m = mrb_method_search_vm(mrb, &c, MRB_SYM(each));
  return MRB_METHOD_FUNC_P(m) && MRB_METHOD_FUNC(m) == ary_each_fn;
}
static mrb_bool p_enum_blk0(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_proc_p(blk) && root_fiber(mrb) && ary_plain_each(mrb, self);
}

static mrb_value d_ary_each(mrb_state *mrb, mrb_value self) {
  mrb_value blk = cur_blk(mrb);
  mrb_sym sym = blk_sym(blk);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) yield1(mrb, blk, sym, RARRAY_PTR(self)[i]);
  return self;
}
static mrb_value d_ary_map(mrb_state *mrb, mrb_value self) {
  mrb_value blk = cur_blk(mrb);
  mrb_sym sym = blk_sym(blk);
  mrb_value out = mrb_ary_new_capa(mrb, RARRAY_LEN(self));
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value v = yield1(mrb, blk, sym, RARRAY_PTR(self)[i]);
    mrb_ary_push(mrb, out, v);
  }
  return out;
}
static mrb_value ary_filter(mrb_state *mrb, mrb_value self, mrb_bool keep) {
  mrb_value blk = cur_blk(mrb);
  mrb_sym sym = blk_sym(blk);
  mrb_value out = mrb_ary_new(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    if (mrb_test(yield1(mrb, blk, sym, e)) == keep) mrb_ary_push(mrb, out, e);
  }
  return out;
}
static mrb_value d_ary_select(mrb_state *mrb, mrb_value self) { return ary_filter(mrb, self, TRUE); }
static mrb_value d_ary_reject(mrb_state *mrb, mrb_value self) { return ary_filter(mrb, self, FALSE); }

static mrb_value d_ary_each_with_index(mrb_state *mrb, mrb_value self) {
  mrb_value blk = cur_blk(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value v[2] = {RARRAY_PTR(self)[i], mrb_fixnum_value(i)};
    mrb_yield_argv(mrb, blk, 2, v);
  }
  return self;
}
static mrb_value d_ary_find(mrb_state *mrb, mrb_value self) {
  mrb_value blk = cur_blk(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    if (mrb_test(yield1(mrb, blk, blk_sym(blk), e))) return e;
  }
  return mrb_nil_value();
}

/* Array#count: no argument (its size), or a block */
static mrb_bool p_ary_count(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && (mrb_nil_p(blk) || root_fiber(mrb));
}
static mrb_value d_ary_count(mrb_state *mrb, mrb_value self) {
  mrb_value blk = cur_blk(mrb);
  if (mrb_nil_p(blk)) return mrb_fixnum_value(RARRAY_LEN(self));
  mrb_int n = 0;
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    if (mrb_test(yield1(mrb, blk, blk_sym(blk), RARRAY_PTR(self)[i]))) n++;
  }
  return mrb_fixnum_value(n);
}

/* Array#uniq without a block: the first of each eql? group (as MRI, by a Hash) */
/* elements whose #hash and #eql? mruby's Hash uses as MRI's would */
static mrb_bool plain_key(mrb_state *mrb, mrb_value v) {
  switch (mrb_type(v)) {
  case MRB_TT_FALSE: case MRB_TT_TRUE: case MRB_TT_INTEGER: case MRB_TT_FLOAT: case MRB_TT_SYMBOL:
    return TRUE;
  case MRB_TT_STRING:
    return mrb_obj_class(mrb, v) == mrb->string_class;
  default:
    return FALSE;
  }
}
static mrb_bool p_ary_uniq(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 0 || !mrb_nil_p(blk)) return FALSE;
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) if (!plain_key(mrb, RARRAY_PTR(self)[i])) return FALSE;
  return TRUE;
}
static mrb_value d_ary_uniq(mrb_state *mrb, mrb_value self) {
  mrb_value h = mrb_hash_new(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    if (!mrb_hash_key_p(mrb, h, e)) mrb_hash_set(mrb, h, e, e);
  }
  return mrb_hash_values(mrb, h);
}

/* Enumerable#tally / #group_by of an Array */
static mrb_bool p_enum_none0(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 0 || !mrb_nil_p(blk) || !ary_plain_each(mrb, self)) return FALSE;
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) if (!plain_key(mrb, RARRAY_PTR(self)[i])) return FALSE;
  return TRUE;
}
static mrb_value d_ary_tally(mrb_state *mrb, mrb_value self) {
  mrb_value h = mrb_hash_new(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    mrb_value c = mrb_hash_fetch(mrb, h, e, mrb_fixnum_value(0));
    mrb_hash_set(mrb, h, e, mrb_integer_p(c) ? mrb_fixnum_value(mrb_integer(c) + 1)
                                             : mrb_funcall_id(mrb, c, MRB_OPSYM(add), 1, mrb_fixnum_value(1)));
  }
  return h;
}
static mrb_value d_ary_group_by(mrb_state *mrb, mrb_value self) {
  mrb_value blk = cur_blk(mrb);
  mrb_value h = mrb_hash_new(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    mrb_value k = yield1(mrb, blk, blk_sym(blk), e);
    mrb_value g = mrb_hash_fetch(mrb, h, k, mrb_undef_value());
    if (mrb_undef_p(g)) {
      g = mrb_ary_new(mrb);
      mrb_hash_set(mrb, h, k, g);
    }
    mrb_ary_push(mrb, g, e);
  }
  return h;
}

/* Enumerable#inject / #reduce of an Array: a block (and an initial value),
   or an operator Symbol (and an initial value) */
static mrb_bool p_ary_inject(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!ary_plain_each(mrb, self)) return FALSE;
  if (mrb_proc_p(blk)) return argc <= 1 && root_fiber(mrb);
  if (argc == 1) return mrb_symbol_p(argv[0]);
  return argc == 2 && mrb_symbol_p(argv[1]);
}
static mrb_value d_ary_inject(mrb_state *mrb, mrb_value self) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  mrb_value blk = cur_blk(mrb);
  mrb_bool by_block = mrb_proc_p(blk);
  mrb_sym op = 0;
  mrb_value memo;
  mrb_int i = 0;
  if (by_block) {
    if (argc == 1) memo = argv[0];
    else if (RARRAY_LEN(self) == 0) return mrb_nil_value();
    else memo = RARRAY_PTR(self)[i++];
  }
  else {
    op = mrb_symbol(argv[argc - 1]);
    if (argc == 2) memo = argv[0];
    else if (RARRAY_LEN(self) == 0) return mrb_nil_value();
    else memo = RARRAY_PTR(self)[i++];
  }
  for (; i < RARRAY_LEN(self); i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    if (by_block) {
      mrb_value v[2] = {memo, e};
      memo = mrb_yield_argv(mrb, blk, 2, v);
    }
    else if (op == MRB_OPSYM(add) && mrb_integer_p(memo) && mrb_integer_p(e) &&
             !(mrb->bop_redefined & (1u << MRB_BOP_ADD))) {
      mrb_int r;
      if (mrb_int_add_overflow(mrb_integer(memo), mrb_integer(e), &r)) memo = mrb_funcall_id(mrb, memo, op, 1, e);
      else memo = mrb_fixnum_value(r);
    }
    else {
      memo = mrb_funcall_argv(mrb, memo, op, 1, &e);
    }
  }
  return memo;
}

/* Array#sum: MRI's (array.c ary_sum) for Integers and Floats without a block:
   Integers exactly, Floats by Kahan-Babuska summation, anything else by + */
static mrb_bool p_ary_sum(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk) || argc > 1) return FALSE;
  return argc == 0 || mrb_integer_p(argv[0]) || mrb_float_p(argv[0]);
}
static mrb_value d_ary_sum(mrb_state *mrb, mrb_value self) {
  mrb_value v = mrb_get_argc(mrb) == 1 ? mrb_get_argv(mrb)[0] : mrb_fixnum_value(0);
  mrb_int i = 0, len = RARRAY_LEN(self);
  /* Integers */
  if (mrb_integer_p(v) && !(mrb->bop_redefined & (1u << MRB_BOP_ADD))) {
    mrb_int n = mrb_integer(v);
    for (; i < len; i++) {
      mrb_value e = RARRAY_PTR(self)[i];
      mrb_int r;
      if (!mrb_integer_p(e) || mrb_int_add_overflow(n, mrb_integer(e), &r)) break;
      n = r;
    }
    v = mrb_fixnum_value(n);
  }
  /* Floats (and the Integers among them) */
  if (i < len && (mrb_float_p(v) || mrb_float_p(RARRAY_PTR(self)[i])) &&
      !(mrb->bop_redefined & (1u << MRB_BOP_ADD)) && !(mrb->bop_redefined & MRB_BOP_FLOAT(MRB_BOP_ADD))) {
    double f = mrb_float_p(v) ? mrb_float(v) : (double)mrb_integer(v), c = 0.0;
    if (mrb_integer_p(v) || mrb_float_p(v)) {
      for (; i < len; i++) {
        mrb_value e = RARRAY_PTR(self)[i];
        double x;
        if (mrb_float_p(e)) x = mrb_float(e);
        else if (mrb_integer_p(e)) x = (double)mrb_integer(e);
        else break;
        if (isnan(f)) continue;
        if (isnan(x)) { f = x; continue; }
        if (isinf(x)) {
          if (isinf(f) && signbit(x) != signbit(f)) f = NAN;
          else f = x;
          continue;
        }
        if (isinf(f)) continue;
        double t = f + x;
        if (fabs(f) >= fabs(x)) c += ((f - t) + x);
        else c += ((x - t) + f);
        f = t;
      }
      f += c;
      v = mrb_float_value(mrb, f);
    }
  }
  /* the rest */
  for (; i < len; i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    v = mrb_funcall_argv(mrb, v, MRB_OPSYM(add), 1, &e);
  }
  return v;
}

#define ORB_ENC_UTF8 0
#define ORB_ENC_BINARY 1
#define RSTR_SET_ENCODING_FLAG_FOR(s, e) RSTR_ENCODING_SET(mrb_str_ptr(s), (e))
static mrb_bool str_tagged(mrb_state *mrb, mrb_value s);
static mrb_bool ascii_only(mrb_value s);

/* Array#+ of two Arrays */
static mrb_bool p_ary_plus(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_array_p(argv[0]) && mrb_obj_class(mrb, self) == mrb->array_class;
}

/* Array#join of Strings, Integers, Symbols, nil, true and false, by a String
   or no separator: MRI's (array.c rb_ary_join). The result is US-ASCII,
   takes the encoding of a String first element, and that of the first
   non-ASCII piece after ASCII-only ones; two non-ASCII encodings, $, set,
   Floats, nested Arrays and other objects go to core_ext's Ruby. */
static mrb_bool join_piece_p(mrb_state *mrb, mrb_value v) {
  switch (mrb_type(v)) {
  case MRB_TT_FALSE: case MRB_TT_TRUE: case MRB_TT_INTEGER: case MRB_TT_SYMBOL:
    return TRUE;
  case MRB_TT_STRING:
    return !str_tagged(mrb, v);
  default:
    return FALSE;
  }
}
static mrb_bool p_ary_join(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk) || argc > 1) return FALSE;
  if (argc == 1) {
    if (!mrb_string_p(argv[0]) || str_tagged(mrb, argv[0])) return FALSE;
  }
  else if (!mrb_nil_p(mrb_gv_get(mrb, mrb_intern_lit(mrb, "$,")))) {
    return FALSE;
  }
  for (mrb_int i = 0; i < RARRAY_LEN(self); i++) if (!join_piece_p(mrb, RARRAY_PTR(self)[i])) return FALSE;
  return TRUE;
}
#define ORB_ENC_US_ASCII 2
static mrb_value d_ary_join(mrb_state *mrb, mrb_value self) {
  mrb_value sep = mrb_get_argc(mrb) == 1 ? mrb_get_argv(mrb)[0] : mrb_nil_value();
  mrb_int len = RARRAY_LEN(self);
  mrb_value out = mrb_str_new_capa(mrb, len * 4);
  int enc = ORB_ENC_US_ASCII;
  mrb_bool ascii = TRUE; /* the result so far */
  int ai = mrb_gc_arena_save(mrb);
  for (mrb_int i = 0; i < len && i < RARRAY_LEN(self); i++) {
    mrb_value e = RARRAY_PTR(self)[i];
    for (int part = 0; part < 2; part++) {
      mrb_value piece;
      if (part == 0) {
        if (i == 0 || mrb_nil_p(sep)) continue;
        piece = sep;
      }
      else {
        switch (mrb_type(e)) {
        case MRB_TT_STRING: piece = e; break;
        case MRB_TT_INTEGER: piece = mrb_integer_to_str(mrb, e, 10); break;
        case MRB_TT_SYMBOL: piece = mrb_sym_str(mrb, mrb_symbol(e)); break;
        case MRB_TT_TRUE: piece = mrb_str_new_lit(mrb, "true"); break;
        case MRB_TT_FALSE:
          if (mrb_nil_p(e)) continue;
          piece = mrb_str_new_lit(mrb, "false");
          break;
        default: return mrb_funcall_id(mrb, self, MRB_SYM(__oruby_slow_join), 1, sep);
        }
      }
      int penc = RSTR_ENCODING(mrb_str_ptr(piece));
      mrb_bool pascii = ascii_only(piece);
      if (i == 0 && part == 1 && mrb_string_p(e)) {
        enc = penc;
      }
      else if (!pascii) {
        if (ascii) enc = penc;
        else if (penc != enc && !(penc != ORB_ENC_BINARY && enc != ORB_ENC_BINARY)) {
          return mrb_funcall_id(mrb, self, MRB_SYM(__oruby_slow_join), 1, sep);
        }
      }
      ascii = ascii && pascii;
      mrb_str_cat(mrb, out, RSTRING_PTR(piece), RSTRING_LEN(piece));
      mrb_gc_arena_restore(mrb, ai);
    }
  }
  /* US-ASCII with non-ASCII bytes is UTF-8 (both are flag 0 or 2 here) */
  if (enc == ORB_ENC_US_ASCII && !ascii) enc = ORB_ENC_UTF8;
  RSTR_SET_ENCODING_FLAG_FOR(out, enc);
  return out;
}

/* ---- String / Symbol ---- */

static mrb_bool str_tagged(mrb_state *mrb, mrb_value s) {
  /* an encoding other than UTF-8, BINARY or US-ASCII: a binary string with
     the hidden tag (go-mrb.c) */
  return RSTR_ENCODING(mrb_str_ptr(s)) == ORB_ENC_BINARY && mrb_iv_defined(mrb, s, MRB_SYM(__encoding));
}

static mrb_bool ascii_only(mrb_value s) {
  const unsigned char *p = (const unsigned char *)RSTRING_PTR(s);
  mrb_int n = RSTRING_LEN(s);
  for (mrb_int i = 0; i < n; i++) if (p[i] & 0x80) return FALSE;
  return TRUE;
}

/* the two compare as bytes: same flag, or one of them ASCII */
static mrb_bool str_compatible(mrb_value a, mrb_value b) {
  int ea = RSTR_ENCODING(mrb_str_ptr(a)) == ORB_ENC_BINARY, eb = RSTR_ENCODING(mrb_str_ptr(b)) == ORB_ENC_BINARY;
  return ea == eb || ascii_only(a) || ascii_only(b);
}

/* start_with?/end_with?: Strings of the plain encodings */
static mrb_bool p_str_affix(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk) || str_tagged(mrb, self)) return FALSE;
  for (mrb_int i = 0; i < argc; i++) {
    if (!mrb_string_p(argv[i]) || str_tagged(mrb, argv[i]) || !str_compatible(self, argv[i])) return FALSE;
  }
  return TRUE;
}

static inline mrb_bool utf8_cont(unsigned char c) { return (c & 0xc0) == 0x80; }

static mrb_value d_str_start_with(mrb_state *mrb, mrb_value self) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  const char *p = RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  mrb_bool utf8 = RSTR_ENCODING(mrb_str_ptr(self)) != ORB_ENC_BINARY;
  for (mrb_int i = 0; i < argc; i++) {
    mrb_int n = RSTRING_LEN(argv[i]);
    if (n > len) continue;
    if (memcmp(p, RSTRING_PTR(argv[i]), n) != 0) continue;
    /* ends on a character boundary */
    if (utf8 && n < len && n > 0 && utf8_cont((unsigned char)p[n])) continue;
    return mrb_true_value();
  }
  return mrb_false_value();
}

static mrb_value d_str_end_with(mrb_state *mrb, mrb_value self) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  const char *p = RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  mrb_bool utf8 = RSTR_ENCODING(mrb_str_ptr(self)) != ORB_ENC_BINARY;
  for (mrb_int i = 0; i < argc; i++) {
    mrb_int n = RSTRING_LEN(argv[i]);
    if (n > len) continue;
    mrb_int s = len - n;
    if (memcmp(p + s, RSTRING_PTR(argv[i]), n) != 0) continue;
    /* starts at a character boundary */
    if (utf8 && n > 0 && s > 0 && utf8_cont((unsigned char)p[s])) continue;
    return mrb_true_value();
  }
  return mrb_false_value();
}

/* String#[] / #slice: Integer, Range or String arguments (a Regexp goes to
   gem/regexp, another type to the implicit conversion) */
static mrb_bool p_str_aref(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk)) return FALSE;
  if (argc == 1) return mrb_integer_p(argv[0]) || mrb_string_p(argv[0]) || mrb_range_p(argv[0]);
  return argc == 2 && mrb_integer_p(argv[0]) && mrb_integer_p(argv[1]);
}
/* String#index and the like: a String, and an Integer offset */
static mrb_bool p_str_index(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk) || argc < 1 || argc > 2 || !mrb_string_p(argv[0])) return FALSE;
  return argc == 1 || mrb_integer_p(argv[1]);
}

/* String#[]=(i, str) of an ASCII String: one character replaced, as the
   builtin does, the encodings agreeing */
static mrb_bool p_str_aset(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 2 || !mrb_nil_p(blk) || !mrb_integer_p(argv[0]) || !mrb_string_p(argv[1])) return FALSE;
  if (mrb_frozen_p(mrb_basic_ptr(self)) || str_tagged(mrb, self) || str_tagged(mrb, argv[1])) return FALSE;
  if (!ascii_only(self)) return FALSE;
  mrb_int i = mrb_integer(argv[0]), n = RSTRING_LEN(self);
  if (i < -n || i >= n) return FALSE;
  return RSTR_ENCODING(mrb_str_ptr(self)) == RSTR_ENCODING(mrb_str_ptr(argv[1])) || ascii_only(argv[1]);
}

/* String#upcase and the like without options */
static mrb_bool p_str_case(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_nil_p(blk);
}

/* String#to_sym: an encoding a Symbol need not record (core_ext keeps the
   others in Symbol.__sym_encodings) */
static mrb_bool p_str_to_sym(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 0 || !mrb_nil_p(blk)) return FALSE;
  if (RSTR_ENCODING(mrb_str_ptr(self)) != ORB_ENC_BINARY) return TRUE;
  return !mrb_iv_defined(mrb, self, MRB_SYM(__encoding)) && ascii_only(self);
}

/* Symbol#inspect: an ASCII name of no recorded encoding, which mruby quotes as
   MRI does (not &&, ||, nor a constant name ending in ? or !) */
static mrb_bool p_sym_inspect(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (argc != 0 || !mrb_nil_p(blk)) return FALSE;
  mrb_int len;
  const char *s = mrb_sym_name_len(mrb, mrb_symbol(self), &len);
  if (!s) return FALSE;
  for (mrb_int i = 0; i < len; i++) if ((unsigned char)s[i] & 0x80) return FALSE;
  if (len == 2 && (memcmp(s, "&&", 2) == 0 || memcmp(s, "||", 2) == 0)) return FALSE;
  if (len >= 2 && (s[len - 1] == '?' || s[len - 1] == '!') && ((s[0] >= 'A' && s[0] <= 'Z') || s[0] == '_')) return FALSE;
  mrb_value tbl = mrb_iv_get(mrb, mrb_obj_value(mrb->symbol_class), MRB_IVSYM(__sym_encodings));
  if (mrb_hash_p(tbl) && !mrb_hash_empty_p(mrb, tbl) && mrb_hash_key_p(mrb, tbl, self)) return FALSE;
  return TRUE;
}

/* ---- Symbol#to_proc ----
   A C lambda per Symbol (cached, as MRI's): its first argument is the
   receiver of a public method call with the rest. Anything else (a private
   or missing method, keywords, no receiver) goes to core_ext's Ruby lambda
   (Symbol#__oruby_slow_to_proc), in the same frame. */

static mrb_value symproc_call(mrb_state *mrb, mrb_value self) {
  mrb_value sym = mrb_proc_cfunc_env_get(mrb, 0);
  mrb_callinfo *ci = mrb->c->ci;
  mrb_int argc = mrb_get_argc(mrb);
  if (!ci->kw && argc >= 1) {
    const mrb_value *argv = mrb_get_argv(mrb);
    mrb_value recv = argv[0];
    struct RClass *c = mrb_class(mrb, recv);
    mrb_method_t m = mrb_method_search_vm(mrb, &c, mrb_symbol(sym));
    if (!MRB_METHOD_UNDEF_P(m)) {
      int vis = MRB_METHOD_VISIBILITY(m);
      if (vis != MRB_METHOD_PRIVATE_FL && vis != MRB_METHOD_PROTECTED_FL) {
        if (ci->n < 15 && ci->cci == 0 /* CINFO_NONE: called from the VM */) {
          /* this frame becomes the call (as send does): the receiver as
             self, the arguments moved down, the block after them */
          mrb_value *regs = ci->stack;
          int n = ci->n;
          regs[0] = recv;
          for (int i = 1; i < n; i++) regs[i] = regs[i + 1];
          regs[n] = ci->blk ? mrb_obj_value(ci->blk) : mrb_nil_value();
          ci->n = n - 1;
          ci->mid = mrb_symbol(sym);
          ci->u.target_class = c;
          if (MRB_METHOD_FUNC_P(m)) {
            mrb_aspec aspec = MRB_MT_ASPEC(m.flags);
            int min = MRB_ASPEC_REQ(aspec) + MRB_ASPEC_POST(aspec);
            int max = MRB_ASPEC_REST(aspec) ? -1 : min + MRB_ASPEC_OPT(aspec);
            if (n - 1 < min || (max >= 0 && n - 1 > max)) mrb_argnum_error(mrb, n - 1, min, max);
            return MRB_METHOD_FUNC(m)(mrb, recv);
          }
          return mrb_exec_irep(mrb, recv, MRB_METHOD_PROC(m));
        }
        mrb_value blk = ci->blk ? mrb_obj_value(ci->blk) : mrb_nil_value();
        return mrb_funcall_with_block(mrb, recv, mrb_symbol(sym), argc - 1, argv + 1, blk);
      }
    }
  }
  mrb_value lam = mrb_funcall_id(mrb, sym, MRB_SYM(__oruby_slow_to_proc), 0);
  if (!mrb_proc_p(lam)) mrb_raise(mrb, E_TYPE_ERROR, "Symbol#to_proc");
  ci = mrb->c->ci;
  /* the lambda takes its block from the block slot: a yield leaves none */
  ci->stack[mrb_ci_bidx(ci)] = ci->blk ? mrb_obj_value(ci->blk) : mrb_nil_value();
  return mrb_exec_irep(mrb, sym, mrb_proc_ptr(lam));
}

static mrb_value d_sym_to_proc(mrb_state *mrb, mrb_value self) {
  mrb_value sc = mrb_obj_value(mrb->symbol_class);
  mrb_sym iv = MRB_SYM(__oruby_sym_procs);
  mrb_value cache = mrb_iv_get(mrb, sc, iv);
  if (!mrb_hash_p(cache)) {
    cache = mrb_hash_new(mrb);
    mrb_iv_set(mrb, sc, iv, cache);
  }
  mrb_value pr = mrb_hash_get(mrb, cache, self);
  if (mrb_proc_p(pr)) return pr;
  struct RProc *p = mrb_proc_new_cfunc_with_env(mrb, symproc_call, 1, &self);
  p->flags |= MRB_PROC_STRICT; /* a lambda: a yielded Array is not spread */
  pr = mrb_obj_value(p);
  mrb_hash_set(mrb, cache, self, pr);
  return pr;
}

static mrb_bool symproc_p(const struct RProc *p) {
  return MRB_PROC_CFUNC_P(p) && MRB_PROC_CFUNC(p) == symproc_call;
}
static mrb_value d_proc_symproc_p(mrb_state *mrb, mrb_value self) {
  return mrb_bool_value(symproc_p(mrb_proc_ptr(self)));
}

/* ---- Proc ---- */

/* Proc#arity: -2 for a Symbol#to_proc lambda (core_ext's, told by its code) */
static mrb_bool p_proc_arity(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_nil_p(blk);
}
static mrb_func_t proc_arity_builtin;
static mrb_value d_proc_arity(mrb_state *mrb, mrb_value self) {
  struct RProc *p = mrb_proc_ptr(self);
  if (symproc_p(p)) return mrb_fixnum_value(-2);
  if (!MRB_PROC_CFUNC_P(p) && p->body.irep) {
    mrb_value pc = mrb_obj_value(mrb->proc_class);
    mrb_sym iv = MRB_SYM(__oruby_symproc);
    mrb_value sample = mrb_iv_get(mrb, pc, iv);
    if (!mrb_proc_p(sample)) {
      sample = mrb_funcall_id(mrb, mrb_symbol_value(MRB_SYM(to_s)), MRB_SYM(to_proc), 0);
      if (mrb_proc_p(sample)) mrb_iv_set(mrb, pc, iv, sample);
    }
    if (mrb_proc_p(sample) && !MRB_PROC_CFUNC_P(mrb_proc_ptr(sample)) &&
        mrb_proc_ptr(sample)->body.irep == p->body.irep) {
      return mrb_fixnum_value(-2);
    }
  }
  return proc_arity_builtin(mrb, self);
}

/* ---- Hash ---- */

static mrb_bool plain_hash(mrb_state *mrb, mrb_value h) {
  return mrb_hash_p(h) && mrb_obj_class(mrb, h) == mrb->hash_class;
}
static mrb_bool hash_no_ivars(mrb_value h) {
  return mrb_hash_ptr(h)->iv == NULL;
}

/* Hash#initialize: a default value, or no arguments for a Hash without a
   default (core_ext's resets one: h.send(:initialize)) */
static mrb_bool p_hash_init(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  if (!mrb_nil_p(blk) || mrb_frozen_p(mrb_basic_ptr(self))) return FALSE;
  if (argc == 1) return TRUE;
  return argc == 0 && !(mrb_hash_ptr(self)->flags & (MRB_HASH_DEFAULT | MRB_HASH_PROC_DEFAULT));
}

/* Hash.[](hash): a Hash of its pairs */
static mrb_bool p_hash_s_aref(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_nil_p(blk) && mrb_class_ptr(self) == mrb->hash_class && plain_hash(mrb, argv[0]) &&
         !(mrb_hash_ptr(argv[0])->flags & MRB_HASH_COMPARE_BY_IDENTITY);
}
static mrb_value d_hash_s_aref(mrb_state *mrb, mrb_value self) {
  mrb_value h = mrb_hash_new(mrb);
  mrb_hash_merge(mrb, h, mrb_get_argv(mrb)[0]);
  return h;
}

/* Hash#merge(hash) / #update(hash): a Hash, no block */
static mrb_bool p_hash_merge(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_nil_p(blk) && plain_hash(mrb, argv[0]) && plain_hash(mrb, self) && hash_no_ivars(self);
}
static mrb_value d_hash_merge(mrb_state *mrb, mrb_value self) {
  mrb_value h = mrb_hash_dup(mrb, self);
  mrb_hash_merge(mrb, h, mrb_get_argv(mrb)[0]);
  return h;
}
static mrb_bool p_hash_update(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_nil_p(blk) && plain_hash(mrb, argv[0]) && !mrb_frozen_p(mrb_basic_ptr(self));
}
static mrb_value d_hash_update(mrb_state *mrb, mrb_value self) {
  mrb_hash_merge(mrb, self, mrb_get_argv(mrb)[0]);
  return self;
}

/* Hash#to_h of a Hash, without a block: itself */
static mrb_bool p_hash_to_h(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_nil_p(blk) && mrb_obj_class(mrb, self) == mrb->hash_class;
}

/* Hash#select / #filter / #reject with a block: a new Hash (MRI's: plain,
   identity kept, no default), pairs yielded as two values */
static mrb_bool p_hash_blk(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && !mrb_nil_p(blk) && mrb->c == mrb->root_c;
}
static mrb_value hash_filter(mrb_state *mrb, mrb_value self, mrb_bool keep) {
  mrb_callinfo *ci = mrb->c->ci;
  mrb_value blk = ci_blk(ci);
  mrb_value keys = mrb_hash_keys(mrb, self), vals = mrb_hash_values(mrb, self);
  mrb_value out = mrb_hash_new(mrb);
  if (mrb_hash_ptr(self)->flags & MRB_HASH_COMPARE_BY_IDENTITY) {
    mrb_funcall_id(mrb, out, MRB_SYM(compare_by_identity), 0);
  }
  int ai = mrb_gc_arena_save(mrb);
  for (mrb_int i = 0; i < RARRAY_LEN(keys); i++) {
    mrb_value kv[2] = {RARRAY_PTR(keys)[i], RARRAY_PTR(vals)[i]};
    if (mrb_test(mrb_yield_argv(mrb, blk, 2, kv)) == keep) mrb_hash_set(mrb, out, kv[0], kv[1]);
    mrb_gc_arena_restore(mrb, ai);
  }
  return out;
}
static mrb_value d_hash_select(mrb_state *mrb, mrb_value self) { return hash_filter(mrb, self, TRUE); }
static mrb_value d_hash_reject(mrb_state *mrb, mrb_value self) { return hash_filter(mrb, self, FALSE); }

/* ---- #hash of Array, Struct and Hash ----
   core_ext's algorithm (06_array.rb, 36_struct_data.rb, 07_hash.rb), in C: the
   elements' #hash combined in 30 bits; an object met again while its own
   hash is computed answers :cycle to its container, and a value with a
   cycle in it hashes to 0. The objects being hashed are kept per fiber
   context; deeper than HASH_DEPTH, the Ruby definition goes on. */

#define HASH_DEPTH 128
#define HASH_MASK 0x3fffffffULL
typedef struct {
  const struct mrb_context *c;
  const void *objs[HASH_DEPTH];
  int depth;
} hash_stack;
static hash_stack hash_stacks[8];

static hash_stack *hash_stack_of(mrb_state *mrb) {
  hash_stack *free_one = NULL;
  for (int i = 0; i < 8; i++) {
    if (hash_stacks[i].c == mrb->c) return &hash_stacks[i];
    if (hash_stacks[i].depth == 0 && !free_one) free_one = &hash_stacks[i];
  }
  if (free_one) {
    free_one->c = mrb->c;
    free_one->depth = 0;
  }
  return free_one;
}

/* the low 30 bits of an element's #hash, or -1 for :cycle */
static int64_t elem_hash(mrb_state *mrb, mrb_value e) {
  mrb_value h = mrb_funcall_id(mrb, e, MRB_SYM(hash), 0);
  if (mrb_symbol_p(h)) return -1; /* :cycle */
  if (!mrb_integer_p(h)) {
    if (!mrb_bigint_p(h) && mrb_respond_to(mrb, h, MRB_SYM(to_int))) h = mrb_funcall_id(mrb, h, MRB_SYM(to_int), 0);
    if (!mrb_integer_p(h)) h = mrb_funcall_id(mrb, h, MRB_OPSYM(and), 1, mrb_fixnum_value((mrb_int)HASH_MASK));
    if (!mrb_integer_p(h)) return 0;
  }
  return (int64_t)((uint64_t)mrb_integer(h) & HASH_MASK);
}

static mrb_value hash_result(mrb_state *mrb, hash_stack *st, uint64_t h, mrb_bool cycle) {
  st->depth--;
  if (cycle) return st->depth > 0 ? mrb_symbol_value(MRB_SYM(cycle)) : mrb_fixnum_value(0);
  return mrb_fixnum_value((mrb_int)h);
}

/* h for a sequence of values: core_ext's Array#hash */
static mrb_value seq_hash(mrb_state *mrb, mrb_value self, mrb_value vals, uint64_t h, mrb_sym slow) {
  hash_stack *st = hash_stack_of(mrb);
  if (!st || st->depth >= HASH_DEPTH) return mrb_funcall_id(mrb, self, slow, 0);
  for (int i = 0; i < st->depth; i++) {
    if (st->objs[i] == mrb_ptr(self)) return mrb_symbol_value(MRB_SYM(cycle));
  }
  st->objs[st->depth++] = mrb_ptr(self);
  mrb_bool cycle = FALSE;
  struct mrb_jmpbuf *prev = mrb->jmp, c_jmp;
  MRB_TRY(&c_jmp) {
    mrb->jmp = &c_jmp;
    for (mrb_int i = 0; i < RARRAY_LEN(vals); i++) {
      int64_t eh = elem_hash(mrb, RARRAY_PTR(vals)[i]);
      if (eh < 0) { cycle = TRUE; eh = 0; }
      h = ((h * 31) + (uint64_t)eh) & HASH_MASK;
    }
    mrb->jmp = prev;
  }
  MRB_CATCH(&c_jmp) {
    mrb->jmp = prev;
    st->depth--;
    MRB_THROW(prev);
  }
  MRB_END_EXC(&c_jmp);
  return hash_result(mrb, st, h, cycle);
}

static mrb_value d_ary_hash(mrb_state *mrb, mrb_value self) {
  return seq_hash(mrb, self, self, (uint64_t)RARRAY_LEN(self) & HASH_MASK, MRB_SYM(__oruby_slow_hash));
}

static mrb_value d_struct_hash(mrb_state *mrb, mrb_value self) {
  mrb_value cls = mrb_obj_value(mrb_obj_class(mrb, self));
  mrb_value ch = mrb_funcall_id(mrb, cls, MRB_SYM(hash), 0);
  uint64_t h = mrb_integer_p(ch) ? ((uint64_t)mrb_integer(ch) & HASH_MASK) : 0;
  mrb_value vals = mrb_funcall_id(mrb, self, MRB_SYM(values), 0);
  if (!mrb_array_p(vals)) return mrb_funcall_id(mrb, self, MRB_SYM(__oruby_slow_hash), 0);
  return seq_hash(mrb, self, vals, h, MRB_SYM(__oruby_slow_hash));
}

static mrb_value d_hash_hash(mrb_state *mrb, mrb_value self) {
  hash_stack *st = hash_stack_of(mrb);
  if (!st || st->depth >= HASH_DEPTH) return mrb_funcall_id(mrb, self, MRB_SYM(__oruby_slow_hash), 0);
  for (int i = 0; i < st->depth; i++) {
    if (st->objs[i] == mrb_ptr(self)) return mrb_symbol_value(MRB_SYM(cycle));
  }
  st->objs[st->depth++] = mrb_ptr(self);
  mrb_value keys = mrb_hash_keys(mrb, self), vals = mrb_hash_values(mrb, self);
  uint64_t h = (uint64_t)RARRAY_LEN(keys) & HASH_MASK;
  mrb_bool cycle = FALSE;
  struct mrb_jmpbuf *prev = mrb->jmp, c_jmp;
  MRB_TRY(&c_jmp) {
    mrb->jmp = &c_jmp;
    for (mrb_int i = 0; i < RARRAY_LEN(keys); i++) {
      int64_t kh = elem_hash(mrb, RARRAY_PTR(keys)[i]);
      int64_t vh = elem_hash(mrb, RARRAY_PTR(vals)[i]);
      if (kh < 0) { cycle = TRUE; kh = 0; }
      if (vh < 0) { cycle = TRUE; vh = 0; }
      uint64_t pair = ((uint64_t)kh ^ ((uint64_t)vh * 31)) & HASH_MASK;
      h = (h + pair) & HASH_MASK;
    }
    mrb->jmp = prev;
  }
  MRB_CATCH(&c_jmp) {
    mrb->jmp = prev;
    st->depth--;
    MRB_THROW(prev);
  }
  MRB_END_EXC(&c_jmp);
  return hash_result(mrb, st, h, cycle);
}

/* ---- Struct ---- */

/* the new of a Struct class (36_struct_data.rb's __setup_struct_class):
   allocated with every member, then #initialize; with keywords, the Ruby
   definition (kept as __oruby_slow_new) */
static mrb_func_t struct_init_fn; /* Struct#initialize */

static mrb_value struct_new_c(mrb_state *mrb, mrb_value klass) {
  mrb_callinfo *ci = mrb->c->ci;
  if (ci->kw) {
    struct RClass *sc = mrb_class(mrb, klass);
    mrb_method_t m = mrb_method_search_vm(mrb, &sc, MRB_SYM(__oruby_slow_new));
    if (MRB_METHOD_UNDEF_P(m) || MRB_METHOD_FUNC_P(m)) mrb_raise(mrb, E_NOTIMP_ERROR, "Struct.new");
    ci->u.target_class = sc;
    return mrb_exec_irep(mrb, klass, MRB_METHOD_PROC(m));
  }
  mrb_int argc = mrb_get_argc(mrb);
  /* Struct's own #initialize sets every member: as mruby's new */
  struct RClass *ic = mrb_class_ptr(klass);
  mrb_method_t im = mrb_method_search_vm(mrb, &ic, MRB_SYM(initialize));
  if (struct_init_fn && MRB_METHOD_FUNC_P(im) && MRB_METHOD_FUNC(im) == struct_init_fn) {
    return mrb_obj_new(mrb, mrb_class_ptr(klass), argc, mrb_get_argv(mrb));
  }
  /* an #initialize of the class's own, which may not call super: every
     member there before it runs */
  mrb_value blk = ci_blk(ci);
  mrb_value args = mrb_ary_new_from_values(mrb, argc, mrb_get_argv(mrb));
  mrb_value obj = mrb_funcall_id(mrb, klass, MRB_SYM(allocate), 0);
  mrb_value n = mrb_iv_get(mrb, klass, MRB_SYM(__oruby_nmembers));
  if (mrb_integer_p(n) && mrb_integer(n) > 0) {
    mrb_value av[2] = {mrb_fixnum_value(mrb_integer(n) - 1), mrb_nil_value()};
    mrb_funcall_argv(mrb, obj, MRB_OPSYM(aset), 2, av);
  }
  mrb_funcall_with_block(mrb, obj, MRB_SYM(initialize), RARRAY_LEN(args), RARRAY_PTR(args), blk);
  return obj;
}

/* Struct.__oruby_fast_new(klass, n): the C new above for klass, its Ruby
   one kept */
static mrb_value struct_fast_new_m(mrb_state *mrb, mrb_value self) {
  mrb_value klass;
  mrb_int n;
  mrb_get_args(mrb, "oi", &klass, &n);
  if (!mrb_class_p(klass)) return mrb_nil_value();
  if (!struct_init_fn) {
    struct RClass *st = mrb_class_ptr(self);
    mrb_method_t im = mrb_method_search_vm(mrb, &st, MRB_SYM(initialize));
    if (!MRB_METHOD_UNDEF_P(im) && MRB_METHOD_FUNC_P(im)) struct_init_fn = MRB_METHOD_FUNC(im);
  }
  struct RClass *sc = mrb_class_ptr(mrb_singleton_class(mrb, klass));
  struct RClass *owner = sc;
  mrb_method_t cur = mrb_method_search_vm(mrb, &owner, MRB_SYM(new));
  if (MRB_METHOD_UNDEF_P(cur) || (MRB_METHOD_FUNC_P(cur) && MRB_METHOD_FUNC(cur) == struct_new_c)) return mrb_nil_value();
  mrb_define_method_raw(mrb, sc, MRB_SYM(__oruby_slow_new), cur);
  mrb_iv_set(mrb, klass, MRB_SYM(__oruby_nmembers), mrb_fixnum_value(n));
  mrb_define_method(mrb, sc, "new", struct_new_c, MRB_ARGS_ANY());
  mrb_alias_method(mrb, sc, MRB_OPSYM(aref), MRB_SYM(new));
  return mrb_true_value();
}

/* Struct#each: its values, yielded in turn */
static mrb_value d_struct_each(mrb_state *mrb, mrb_value self) {
  mrb_value blk = cur_blk(mrb);
  mrb_value vals = mrb_funcall_id(mrb, self, MRB_SYM(values), 0);
  if (!mrb_array_p(vals)) return mrb_nil_value();
  for (mrb_int i = 0; i < RARRAY_LEN(vals); i++) mrb_yield(mrb, blk, RARRAY_PTR(vals)[i]);
  return self;
}

/* ---- Set ----
   35_set.rb keeps a compare_by_identity Set in a Hash (@__cbi), counts the
   iterations running (@__iter) and asks MRI's way round for a member with an
   eql? of its own; without those, mruby-set's C answers as that Ruby does */

static mrb_bool set_plain(mrb_state *mrb, mrb_value self) {
  return !mrb_test(mrb_iv_get(mrb, self, MRB_IVSYM(__cbi)));
}
static mrb_bool set_idle(mrb_state *mrb, mrb_value self) {
  mrb_value it = mrb_iv_get(mrb, self, MRB_IVSYM(__iter));
  return mrb_nil_p(it) || (mrb_integer_p(it) && mrb_integer(it) <= 0);
}
static mrb_bool no_singleton(mrb_value o) {
  if (mrb_immediate_p(o)) return TRUE;
  struct RClass *c = mrb_basic_ptr(o)->c;
  return !c || c->tt != MRB_TT_SCLASS;
}
static mrb_bool p_set_read(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 0 && mrb_nil_p(blk) && set_plain(mrb, self);
}
static mrb_bool p_set_include(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_nil_p(blk) && no_singleton(argv[0]) && set_plain(mrb, self);
}
static mrb_bool p_set_add(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_nil_p(blk) && !mrb_frozen_p(mrb_basic_ptr(self)) && set_plain(mrb, self) && set_idle(mrb, self);
}
static mrb_bool p_set_delete(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && mrb_nil_p(blk) && !mrb_frozen_p(mrb_basic_ptr(self)) && set_plain(mrb, self);
}

/* ---- Kernel helpers of core_ext ---- */

/* Kernel.__to_int(int) / Kernel.__implicit(obj, klass) where obj is one */
static mrb_bool p_to_int(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 1 && int_p(argv[0]);
}
static mrb_value d_arg0(mrb_state *mrb, mrb_value self) { return mrb_get_argv(mrb)[0]; }
static mrb_bool p_implicit(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, mrb_value blk) {
  return argc == 2 && mrb_class_p(argv[1]) && mrb_obj_is_kind_of(mrb, argv[0], mrb_class_ptr(argv[1]));
}

/* ---- the table ---- */

static const fast_def fast_defs[] = {
  /* Integer */
  {"Integer", 0, "&", p_int1, NULL, -1},
  {"Integer", 0, "|", p_int1, NULL, -1},
  {"Integer", 0, "^", p_int1, NULL, -1},
  {"Integer", 0, "<=>", p_num1, NULL, -1},
  {"Integer", 0, "div", p_int1, NULL, -1},
  {"Integer", 0, "fdiv", p_fdiv, NULL, -1},
  {"Integer", 0, "**", p_int_pow, NULL, -1},
  {"Integer", 0, "[]", p_int_aref, d_int_aref, -1},
  {"Integer", 0, "gcd", p_int1, NULL, -1},
  {"Integer", 0, "lcm", p_int1, NULL, -1},
  {"Integer", 0, "<<", p_int1, NULL, -1},
  {"Integer", 0, ">>", p_int1, NULL, -1},
  {"Integer", 0, "coerce", p_int1, d_int_coerce, -1},
  {"Integer", 0, "round", p_int_ndigits, d_self, -1},
  {"Integer", 0, "floor", p_int_ndigits, d_self, -1},
  {"Integer", 0, "ceil", p_int_ndigits, d_self, -1},
  {"Integer", 0, "truncate", p_int_ndigits, d_self, -1},
  {"Integer", 0, "zero?", p_fixnum_self, d_int_zero_p, -1},
  {"Integer", 0, "pred", p_fixnum_self, d_int_pred, -1},
  {"Integer", 0, "-@", p_fixnum_self, d_int_uminus, -1},
  /* Float */
  {"Float", 0, "%", p_flo_mod, NULL, -1},
  {"Float", 0, "**", p_flo_pow, NULL, -1},
  {"Float", 0, "<=>", p_num1, NULL, -1},
  {"Float", 0, "floor", p_ndigits0, NULL, -1},
  {"Float", 0, "ceil", p_ndigits0, NULL, -1},
  {"Float", 0, "round", p_flo_round, d_flo_round, -1},
  {"Float", 0, "-@", p_flo_self, d_flo_uminus, -1},
  /* Array */
  {"Array", 0, "[]", p_ary_aref, NULL, MRB_IDX_OP_ARY_AREF},
  {"Array", 0, "[]=", p_ary_aset, NULL, MRB_IDX_OP_ARY_ASET},
  {"Array", 0, "each", p_blk0, d_ary_each, -1},
  {"Array", 0, "+", p_ary_plus, NULL, -1},
  {"Array", 0, "first", p_none, NULL, -1},
  {"Array", 0, "last", p_none, NULL, -1},
  {"Array", 0, "join", p_ary_join, d_ary_join, -1},
  {"Array", 0, "map", p_blk0_proc, d_ary_map, -1},
  {"Array", 0, "select", p_blk0, d_ary_select, -1},
  {"Array", 0, "reject", p_blk0, d_ary_reject, -1},
  {"Array", 0, "count", p_ary_count, d_ary_count, -1},
  {"Array", 0, "uniq", p_ary_uniq, d_ary_uniq, -1},
  {"Array", 0, "sum", p_ary_sum, d_ary_sum, -1},
  {"Array", 0, "each_with_index", p_enum_blk0, d_ary_each_with_index, -1},
  {"Array", 0, "find", p_enum_blk0, d_ary_find, -1},
  {"Array", 0, "inject", p_ary_inject, d_ary_inject, -1},
  {"Array", 0, "tally", p_enum_none0, d_ary_tally, -1},
  {"Array", 0, "group_by", p_enum_blk0, d_ary_group_by, -1},
  /* String, Symbol */
  {"String", 0, "[]", p_str_aref, NULL, -1},
  {"String", 0, "index", p_str_index, NULL, -1},
  {"String", 0, "rindex", p_str_index, NULL, -1},
  {"String", 0, "byteindex", p_str_index, NULL, -1},
  {"String", 0, "byterindex", p_str_index, NULL, -1},
  {"String", 0, "start_with?", p_str_affix, d_str_start_with, -1},
  {"String", 0, "[]=", p_str_aset, NULL, -1},
  {"String", 0, "upcase", p_str_case, NULL, -1},
  {"String", 0, "downcase", p_str_case, NULL, -1},
  {"String", 0, "capitalize", p_str_case, NULL, -1},
  {"String", 0, "swapcase", p_str_case, NULL, -1},
  {"String", 0, "end_with?", p_str_affix, d_str_end_with, -1},
  {"String", 0, "to_sym", p_str_to_sym, NULL, -1},
  {"Symbol", 0, "inspect", p_sym_inspect, NULL, -1},
  {"Symbol", 0, "to_proc", p_none, d_sym_to_proc, -1},
  {"Proc", 0, "__oruby_sym_proc?", p_none, d_proc_symproc_p, -1},
  /* Proc */
  {"Proc", 0, "arity", p_proc_arity, d_proc_arity, -1},
  /* Hash */
  {"Hash", 0, "initialize", p_hash_init, NULL, -1},
  {"Hash", 1, "[]", p_hash_s_aref, d_hash_s_aref, -1},
  {"Hash", 0, "merge", p_hash_merge, d_hash_merge, -1},
  {"Hash", 0, "merge!", p_hash_update, d_hash_update, -1},
  {"Hash", 0, "to_h", p_hash_to_h, d_self, -1},
  {"Hash", 0, "select", p_hash_blk, d_hash_select, -1},
  {"Hash", 0, "reject", p_hash_blk, d_hash_reject, -1},
  {"Hash", 0, "hash", p_none, d_hash_hash, -1},
  {"Array", 0, "hash", p_none, d_ary_hash, -1},
  {"Struct", 0, "hash", p_none, d_struct_hash, -1},
  {"Struct", 0, "each", p_blk0, d_struct_each, -1},
  /* Set */
  {"Set", 0, "include?", p_set_include, NULL, -1},
  {"Set", 0, "add", p_set_add, NULL, -1},
  {"Set", 0, "add?", p_set_add, NULL, -1},
  {"Set", 0, "delete", p_set_delete, NULL, -1},
  {"Set", 0, "size", p_set_read, NULL, -1},
  {"Set", 0, "empty?", p_set_read, NULL, -1},
  {"Set", 0, "to_a", p_set_read, NULL, -1},
  /* core_ext's own helpers */
  {"Kernel", 1, "__to_int", p_to_int, d_arg0, -1},
  {"Kernel", 1, "__implicit", p_implicit, d_arg0, -1},
};
#define FAST_N ((int)(sizeof(fast_defs) / sizeof(fast_defs[0])))

/* aliases of those, the same method again after the install (MRI's are) */
static const struct { const char *cls, *alias, *name; } fast_aliases[] = {
  {"Array", "slice", "[]"},
  {"Array", "collect", "map"},
  {"Array", "filter", "select"},
  {"Array", "detect", "find"},
  {"Array", "reduce", "inject"},
  {"Set", "member?", "include?"},
  {"Set", "===", "include?"},
  {"Set", "<<", "add"},
  {"Set", "length", "size"},
  {"String", "slice", "[]"},
  {"Float", "modulo", "%"},
  {"String", "intern", "to_sym"},
  {"Hash", "update", "merge!"},
  {"Hash", "filter", "select"},
};

static mrb_func_t fast_builtin[FAST_N];

/* each state's __oruby_slow_<name> symbols (symbols are per state) */
#define FAST_STATES 16
static struct { mrb_state *mrb; mrb_sym slow[FAST_N]; } fast_states[FAST_STATES];

static mrb_sym slow_sym(mrb_state *mrb, int n) {
  int free_i = -1;
  for (int i = 0; i < FAST_STATES; i++) {
    if (fast_states[i].mrb == mrb) {
      if (!fast_states[i].slow[n]) goto intern;
      return fast_states[i].slow[n];
    }
    if (!fast_states[i].mrb && free_i < 0) free_i = i;
  }
  if (free_i >= 0) {
    memset(&fast_states[free_i], 0, sizeof(fast_states[free_i]));
    fast_states[free_i].mrb = mrb;
  }
intern:;
  char buf[96];
  snprintf(buf, sizeof(buf), "__oruby_slow_%s", fast_defs[n].name);
  mrb_sym sym = mrb_intern_cstr(mrb, buf);
  for (int i = 0; i < FAST_STATES; i++) {
    if (fast_states[i].mrb == mrb) fast_states[i].slow[n] = sym;
  }
  return sym;
}

/* the state is closed */
void _orb_fast_close(mrb_state *mrb) {
  for (int i = 0; i < FAST_STATES; i++) {
    if (fast_states[i].mrb == mrb) fast_states[i].mrb = NULL;
  }
  for (int i = 0; i < 8; i++) {
    if (hash_stacks[i].c && hash_stacks[i].depth == 0) hash_stacks[i].c = NULL;
  }
}

/* the Ruby definition, in the frame of the call */
static mrb_value fast_slow(mrb_state *mrb, mrb_value self, int n) {
  mrb_sym sym = slow_sym(mrb, n);
  struct RClass *c = mrb_class(mrb, self);
  mrb_method_t m = mrb_method_search_vm(mrb, &c, sym);
  if (MRB_METHOD_UNDEF_P(m)) {
    mrb_raisef(mrb, E_NOTIMP_ERROR, "%s (%n on %C): no Ruby definition", fast_defs[n].name,
               mrb->c->ci->mid, mrb_class(mrb, self));
  }
  mrb->c->ci->u.target_class = c;
  if (MRB_METHOD_FUNC_P(m)) return MRB_METHOD_FUNC(m)(mrb, self);
  return mrb_exec_irep(mrb, self, MRB_METHOD_PROC(m));
}

static mrb_value fast_call(mrb_state *mrb, mrb_value self, int n) {
  const fast_def *d = &fast_defs[n];
  mrb_callinfo *ci = mrb->c->ci;
  if (!ci->kw) {
    mrb_int argc = mrb_get_argc(mrb);
    const mrb_value *argv = mrb_get_argv(mrb);
    mrb_value blk = ci->blk ? mrb_obj_value(ci->blk) : mrb_nil_value();
    if (d->pred(mrb, self, argc, argv, blk)) {
      if (d->direct) return d->direct(mrb, self);
      return fast_builtin[n](mrb, self);
    }
  }
  return fast_slow(mrb, self, n);
}

/* one C function per entry: fw_<b>_<k> is entry b*8+k */
#define FW(b, k) static mrb_value fw_##b##_##k(mrb_state *mrb, mrb_value self) { return fast_call(mrb, self, (b) * 8 + (k)); }
#define FW8(b) FW(b, 0) FW(b, 1) FW(b, 2) FW(b, 3) FW(b, 4) FW(b, 5) FW(b, 6) FW(b, 7)
FW8(0) FW8(1) FW8(2) FW8(3) FW8(4) FW8(5) FW8(6) FW8(7) FW8(8) FW8(9) FW8(10) FW8(11)
#define FR(b, k) fw_##b##_##k
#define FR8(b) FR(b, 0), FR(b, 1), FR(b, 2), FR(b, 3), FR(b, 4), FR(b, 5), FR(b, 6), FR(b, 7)
static const mrb_func_t fast_fns[] = {FR8(0), FR8(1), FR8(2), FR8(3), FR8(4), FR8(5), FR8(6), FR8(7),
                                      FR8(8), FR8(9), FR8(10), FR8(11)};

static struct RClass *fast_class(mrb_state *mrb, const fast_def *d) {
  mrb_sym sym = mrb_intern_cstr(mrb, d->cls);
  if (!mrb_const_defined(mrb, mrb_obj_value(mrb->object_class), sym)) return NULL;
  mrb_value v = mrb_const_get(mrb, mrb_obj_value(mrb->object_class), sym);
  if (!mrb_class_p(v) && !mrb_module_p(v)) return NULL;
  struct RClass *c = mrb_class_ptr(v);
  if (d->single) c = mrb_class_ptr(mrb_singleton_class(mrb, mrb_obj_value(c)));
  return c;
}

/* at startup, before core_ext: the builtins */
void _orb_fast_capture(mrb_state *mrb) {
  for (int n = 0; n < FAST_N && n < (int)(sizeof(fast_fns) / sizeof(fast_fns[0])); n++) {
    const fast_def *d = &fast_defs[n];
    struct RClass *c = fast_class(mrb, d);
    if (!c) continue;
    struct RClass *owner = c;
    mrb_method_t m = mrb_method_search_vm(mrb, &owner, mrb_intern_cstr(mrb, d->name));
    mrb_func_t f = NULL;
    if (!MRB_METHOD_UNDEF_P(m)) {
      const struct RProc *p = MRB_METHOD_FUNC_P(m) ? NULL : MRB_METHOD_PROC(m);
      if (MRB_METHOD_FUNC_P(m)) f = MRB_METHOD_FUNC(m);
      else if (p && MRB_PROC_CFUNC_P(p) && !MRB_PROC_ENV_P(p)) f = MRB_PROC_CFUNC(p);
    }
    if (f && f != fast_fns[n] && !fast_builtin[n]) fast_builtin[n] = f;
  }
  if (!flo_round_builtin) {
    struct RClass *fc = mrb->float_class;
    mrb_method_t m = mrb_method_search_vm(mrb, &fc, MRB_SYM(round));
    if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_FUNC_P(m)) flo_round_builtin = MRB_METHOD_FUNC(m);
  }
  if (!proc_arity_builtin) {
    struct RClass *pc = mrb->proc_class;
    mrb_method_t m = mrb_method_search_vm(mrb, &pc, MRB_SYM(arity));
    if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_FUNC_P(m)) proc_arity_builtin = MRB_METHOD_FUNC(m);
  }
}

/* the argument spec of a method as it stands */
static mrb_aspec method_aspec(mrb_method_t m) {
  if (MRB_METHOD_FUNC_P(m)) return MRB_MT_ASPEC(m.flags);
  const struct RProc *p = MRB_METHOD_PROC(m);
  if (!p) return MRB_ARGS_ANY();
  if (MRB_PROC_ALIAS_P(p)) p = p->upper;
  if (!p || MRB_PROC_CFUNC_P(p)) return MRB_ARGS_ANY();
  const mrb_irep *irep = p->body.irep;
  if (irep && irep->ilen >= 4 && irep->iseq[0] == OP_ENTER) {
    return (mrb_aspec)PEEK_W(irep->iseq + 1);
  }
  return MRB_ARGS_NONE();
}

/* after core_ext: the C methods in place of the Ruby ones */
void _orb_fast_install(mrb_state *mrb) {
  for (int n = 0; n < FAST_N && n < (int)(sizeof(fast_fns) / sizeof(fast_fns[0])); n++) {
    const fast_def *d = &fast_defs[n];
    if (!d->direct && !fast_builtin[n]) continue;
    struct RClass *c = fast_class(mrb, d);
    if (!c) continue;
    mrb_sym mid = mrb_intern_cstr(mrb, d->name);
    struct RClass *owner = c;
    mrb_method_t cur = mrb_method_search_vm(mrb, &owner, mid);
    if (MRB_METHOD_UNDEF_P(cur)) continue;
    if (MRB_METHOD_FUNC_P(cur) && MRB_METHOD_FUNC(cur) == fast_fns[n]) continue;
    /* the builtin itself is still the method: nothing to speed up, unless
       there is a C version of ours */
    if (!d->direct && MRB_METHOD_FUNC_P(cur) && MRB_METHOD_FUNC(cur) == fast_builtin[n]) continue;
    char buf[96];
    snprintf(buf, sizeof(buf), "__oruby_slow_%s", d->name);
    mrb_sym slow = mrb_intern_cstr(mrb, buf);
    /* kept where it is defined, so that super from it goes on from there */
    mrb_define_method_raw(mrb, owner, slow, cur);
    mrb_method_t m;
    MRB_METHOD_FROM_FUNC(m, fast_fns[n]);
    m.flags |= method_aspec(cur);
    MRB_METHOD_SET_VISIBILITY(m, MRB_METHOD_VISIBILITY(cur));
    mrb_define_method_raw(mrb, c, mid, m);
    if (d->slot >= 0) mrb_idx_op_rearm(mrb, (enum mrb_idx_op_slot)d->slot);
    if (d->direct == d_ary_each) ary_each_fn = fast_fns[n];
  }
  for (size_t i = 0; i < sizeof(fast_aliases) / sizeof(fast_aliases[0]); i++) {
    fast_def a = {fast_aliases[i].cls, 0, NULL, NULL, NULL, -1};
    struct RClass *c = fast_class(mrb, &a);
    if (!c) continue;
    struct RClass *owner = c;
    mrb_method_t m = mrb_method_search_vm(mrb, &owner, mrb_intern_cstr(mrb, fast_aliases[i].name));
    if (MRB_METHOD_UNDEF_P(m) || !MRB_METHOD_FUNC_P(m)) continue;
    mrb_bool ours = FALSE;
    for (int n = 0; n < FAST_N; n++) if (MRB_METHOD_FUNC(m) == fast_fns[n]) ours = TRUE;
    if (ours) mrb_alias_method(mrb, c, mrb_intern_cstr(mrb, fast_aliases[i].alias), mrb_intern_cstr(mrb, fast_aliases[i].name));
  }
  _orb_string_install(mrb);
}

/* ---- implicit argument conversion (00_convert.rb's __implicit_args) ----
   A C function wrapped for to_int/to_str conversion of its arguments: those
   already Integers or Strings (or nil) go to it as they are; another is
   converted in place (Kernel.__check_implicit) when it has the conversion
   method. No Ruby frame, no bind_call. */

enum { IMP_NONE, IMP_INT, IMP_STR };
#define IMP_TYPES 4
typedef struct {
  mrb_func_t orig;   /* a plain C function; NULL: a C proc (a Go method), kept as __oruby_imp_<slot> */
  char key[64];      /* Class#name: a proc's slot is its own */
  int n;
  uint8_t t[IMP_TYPES];
} imp_def;
#define IMP_MAX 96
static imp_def imps[IMP_MAX];
static int imp_count;

static mrb_value imp_call(mrb_state *mrb, mrb_value self, int slot) {
  const imp_def *d = &imps[slot];
  mrb_int argc = mrb_get_argc(mrb);
  for (int i = 0; i < d->n && i < argc; i++) {
    mrb_value a = mrb_get_argv(mrb)[i];
    if (d->t[i] == IMP_NONE || mrb_nil_p(a)) continue;
    if (d->t[i] == IMP_INT ? int_p(a) : mrb_string_p(a)) continue;
    struct RClass *k = d->t[i] == IMP_INT ? mrb->integer_class : mrb->string_class;
    mrb_value cv[2] = {a, mrb_obj_value(k)};
    mrb_value c = mrb_funcall_argv(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__check_implicit), 2, cv);
    /* the stack may have moved */
    if (!mrb_nil_p(c)) ((mrb_value *)mrb_get_argv(mrb))[i] = c;
  }
  if (d->orig) return d->orig(mrb, self);
  /* a C proc: called as itself, in this frame (its env names the Go method) */
  char buf[32];
  snprintf(buf, sizeof(buf), "__oruby_imp_%d", slot);
  struct RClass *c = mrb_class(mrb, self);
  mrb_method_t m = mrb_method_search_vm(mrb, &c, mrb_intern_cstr(mrb, buf));
  if (MRB_METHOD_UNDEF_P(m) || MRB_METHOD_FUNC_P(m)) mrb_raise(mrb, E_NOTIMP_ERROR, "implicit wrapper lost its method");
  const struct RProc *p = MRB_METHOD_PROC(m);
  mrb_callinfo *ci = mrb->c->ci;
  const struct RProc *mine = ci->proc;
  ci->proc = p; /* a C proc: no pc to set */
  mrb_value r = MRB_PROC_CFUNC(p)(mrb, self);
  mrb->c->ci->proc = mine;
  return r;
}

#define IW(b, k) static mrb_value iw_##b##_##k(mrb_state *mrb, mrb_value self) { return imp_call(mrb, self, (b) * 8 + (k)); }
#define IW8(b) IW(b, 0) IW(b, 1) IW(b, 2) IW(b, 3) IW(b, 4) IW(b, 5) IW(b, 6) IW(b, 7)
IW8(0) IW8(1) IW8(2) IW8(3) IW8(4) IW8(5) IW8(6) IW8(7) IW8(8) IW8(9) IW8(10) IW8(11)
#define IR(b, k) iw_##b##_##k
#define IR8(b) IR(b, 0), IR(b, 1), IR(b, 2), IR(b, 3), IR(b, 4), IR(b, 5), IR(b, 6), IR(b, 7)
static const mrb_func_t imp_fns[IMP_MAX] = {IR8(0), IR8(1), IR8(2), IR8(3), IR8(4), IR8(5), IR8(6), IR8(7),
                                            IR8(8), IR8(9), IR8(10), IR8(11)};

/* Module#__oruby_implicit_c(name, types): true when name is a C method now
   wrapped in C; false leaves it to the Ruby wrapper */
static mrb_value mod_implicit_c(mrb_state *mrb, mrb_value self) {
  mrb_sym name;
  mrb_value types;
  mrb_get_args(mrb, "nA", &name, &types);
  struct RClass *c = mrb_class_ptr(self), *owner = c;
  mrb_method_t m = mrb_method_search_vm(mrb, &owner, name);
  if (MRB_METHOD_UNDEF_P(m)) return mrb_false_value();
  mrb_func_t f = NULL;
  mrb_bool proc = FALSE;
  mrb_aspec aspec = MRB_ARGS_ANY();
  if (MRB_METHOD_FUNC_P(m)) {
    f = MRB_METHOD_FUNC(m);
    aspec = MRB_MT_ASPEC(m.flags);
  }
  else if (MRB_METHOD_PROC(m) && MRB_PROC_CFUNC_P(MRB_METHOD_PROC(m))) {
    f = MRB_PROC_CFUNC(MRB_METHOD_PROC(m));
    if (MRB_PROC_ENV_P(MRB_METHOD_PROC(m))) proc = TRUE;
  }
  if (!f) return mrb_false_value();
  for (int i = 0; i < IMP_MAX; i++) if (imp_fns[i] == f) return mrb_true_value(); /* wrapped already */
  imp_def d = {proc ? NULL : f, {0}, (int)RARRAY_LEN(types), {0}};
  if (proc) {
    const char *cn = mrb_class_name(mrb, c);
    snprintf(d.key, sizeof(d.key), "%s#%s", cn ? cn : "?", mrb_sym_name(mrb, name));
  }
  if (d.n > IMP_TYPES) return mrb_false_value();
  for (int i = 0; i < d.n; i++) {
    mrb_value t = RARRAY_PTR(types)[i];
    if (mrb_nil_p(t)) d.t[i] = IMP_NONE;
    else if (mrb_class_p(t) && mrb_class_ptr(t) == mrb->integer_class) d.t[i] = IMP_INT;
    else if (mrb_class_p(t) && mrb_class_ptr(t) == mrb->string_class) d.t[i] = IMP_STR;
    else return mrb_false_value();
  }
  /* the same function and types in every state: one slot */
  int slot = -1;
  for (int i = 0; i < imp_count; i++) {
    if (imps[i].orig == d.orig && imps[i].n == d.n && memcmp(imps[i].t, d.t, IMP_TYPES) == 0 &&
        strcmp(imps[i].key, d.key) == 0) { slot = i; break; }
  }
  if (slot < 0) {
    if (imp_count >= IMP_MAX) return mrb_false_value();
    slot = imp_count;
    imps[imp_count++] = d;
  }
  if (proc) {
    char buf[32];
    snprintf(buf, sizeof(buf), "__oruby_imp_%d", slot);
    mrb_define_method_raw(mrb, c, mrb_intern_cstr(mrb, buf), m);
  }
  if (getenv("ORUBY_FAST_DEBUG")) fprintf(stderr, "implicit %s#%s slot %d proc %d\n", mrb_class_name(mrb, c), mrb_sym_name(mrb, name), slot, proc);
  mrb_method_t w;
  MRB_METHOD_FROM_FUNC(w, imp_fns[slot]);
  w.flags |= aspec;
  MRB_METHOD_SET_VISIBILITY(w, MRB_METHOD_VISIBILITY(m));
  mrb_define_method_raw(mrb, c, name, w);
  return mrb_true_value();
}

/* Kernel.__oruby_relit(id) / __oruby_relit_set(id, re): the Regexp of a
   literal site (prism.go's ORB_REGEXP_LITERAL), made once */
static mrb_value relit_table(mrb_state *mrb) {
  mrb_value k = mrb_obj_value(mrb->kernel_module);
  mrb_sym iv = MRB_SYM(__oruby_relits);
  mrb_value t = mrb_iv_get(mrb, k, iv);
  if (!mrb_hash_p(t)) {
    t = mrb_hash_new(mrb);
    mrb_iv_set(mrb, k, iv, t);
  }
  return t;
}
static mrb_value relit_m(mrb_state *mrb, mrb_value self) {
  mrb_int id;
  mrb_get_args(mrb, "i", &id);
  return mrb_hash_get(mrb, relit_table(mrb), mrb_fixnum_value(id));
}
static mrb_value relit_set_m(mrb_state *mrb, mrb_value self) {
  mrb_int id;
  mrb_value re;
  mrb_get_args(mrb, "io", &id, &re);
  mrb_hash_set(mrb, relit_table(mrb), mrb_fixnum_value(id), re);
  return re;
}

void _orb_fast_init(mrb_state *mrb) {
  mrb_define_module_function(mrb, mrb->kernel_module, "__oruby_relit", relit_m, MRB_ARGS_REQ(1));
  mrb_define_module_function(mrb, mrb->kernel_module, "__oruby_relit_set", relit_set_m, MRB_ARGS_REQ(2));
  if (mrb_class_defined(mrb, "Struct")) {
    mrb_define_class_method(mrb, mrb_class_get(mrb, "Struct"), "__oruby_fast_new", struct_fast_new_m, MRB_ARGS_REQ(2));
  }
  mrb_define_private_method(mrb, mrb->module_class, "__oruby_implicit_c", mod_implicit_c, MRB_ARGS_REQ(2));
}
