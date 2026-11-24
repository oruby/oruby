/* A C extension written against MRI's API, for gem/capi's tests: what rbs
 * (the proof of concept) does not use. */
#include "ruby.h"
#include "ruby/encoding.h"

static VALUE mTest, cBox;
static VALUE cached = Qnil;

/* TypedData whose dmark keeps a VALUE alive */
struct box { VALUE held; long n; };
static void box_mark(void *p) { rb_gc_mark(((struct box *)p)->held); }
static void box_free(void *p) { xfree(p); }
static size_t box_size(const void *p) { (void)p; return sizeof(struct box); }
static const rb_data_type_t box_type = {
  "TestBox", {box_mark, box_free, box_size, NULL, {NULL}}, NULL, NULL, RUBY_TYPED_FREE_IMMEDIATELY,
};
static VALUE box_alloc(VALUE klass) {
  struct box *b;
  VALUE obj = TypedData_Make_Struct(klass, struct box, &box_type, b);
  b->held = Qnil;
  return obj;
}
static VALUE box_init(VALUE self, VALUE v) {
  struct box *b;
  TypedData_Get_Struct(self, struct box, &box_type, b);
  RB_OBJ_WRITE(self, &b->held, v);
  return self;
}
static VALUE box_held(VALUE self) {
  return ((struct box *)rb_check_typeddata(self, &box_type))->held;
}

/* rb_scan_args "12*:&": a, [b, c], *rest, **kw, &blk */
static VALUE t_scan(int argc, VALUE *argv, VALUE self) {
  VALUE a, b, c, rest, kw, blk;
  int n = rb_scan_args(argc, argv, "12*:&", &a, &b, &c, &rest, &kw, &blk);
  return rb_ary_new_from_args(7, INT2FIX(n), a, b, c, rest, kw, RTEST(blk) ? Qtrue : Qfalse);
}

static VALUE t_yield(VALUE self, VALUE v) {
  if (!rb_block_given_p()) return ID2SYM(rb_intern("no_block"));
  return rb_yield_values(2, v, rb_funcall(v, rb_intern("*"), 1, INT2FIX(2)));
}

static VALUE raiser(VALUE arg) { rb_raise(rb_eArgError, "bad %s %d %" PRIsVALUE, "thing", 42, arg); return Qnil; }
static VALUE t_protect(VALUE self, VALUE arg) {
  int state = 0;
  rb_protect(raiser, arg, &state);
  VALUE err = rb_errinfo();
  rb_set_errinfo(Qnil);
  return rb_ary_new_from_args(2, INT2FIX(state), rb_funcall(err, rb_intern("message"), 0));
}
static VALUE rescued(VALUE arg, VALUE exc) { return rb_ary_new_from_args(2, arg, rb_obj_class(exc)); }
static VALUE t_rescue(VALUE self, VALUE arg) { return rb_rescue(raiser, arg, rescued, ID2SYM(rb_intern("caught"))); }

static VALUE ensure_hit;
static VALUE ens(VALUE arg) { ensure_hit = arg; return Qnil; }
static VALUE t_ensure(VALUE self, VALUE arg) {
  ensure_hit = Qfalse;
  rb_ensure(raiser, arg, ens, Qtrue);
  return Qnil;
}
static VALUE t_ensure_hit(VALUE self) { return ensure_hit; }

static VALUE t_numbers(VALUE self, VALUE big) {
  return rb_ary_new_from_args(6, LONG2NUM(FIXNUM_MAX), LL2NUM(-(1LL << 62)), ULL2NUM(18446744073709551615ULL),
                              DBL2NUM(1.5), DBL2NUM(1e300), LONG2NUM(NUM2LONG(big) + 1));
}

static VALUE t_strings(VALUE self, VALUE s) {
  VALUE out = rb_str_new_cstr("<");
  rb_str_cat(out, RSTRING_PTR(s), RSTRING_LEN(s));
  rb_str_cat_cstr(out, ">");
  rb_encoding *enc = rb_enc_get(s);
  VALUE bin = rb_enc_str_new("\xCF\x80", 2, rb_ascii8bit_encoding());
  return rb_ary_new_from_args(4, out, rb_str_new_cstr(rb_enc_name(enc)), bin,
                              ID2SYM(rb_intern3("\xCF\x80", 2, rb_ascii8bit_encoding())));
}

static int sum_pair(VALUE k, VALUE v, VALUE acc) {
  rb_ary_push(acc, rb_sprintf("%" PRIsVALUE "=%+" PRIsVALUE, k, v));
  return ST_CONTINUE;
}
static VALUE t_hash(VALUE self, VALUE h) {
  VALUE acc = rb_ary_new();
  rb_hash_foreach(h, sum_pair, acc);
  return acc;
}

static VALUE t_new_kw(VALUE self, VALUE klass) {
  VALUE kw = rb_hash_new();
  rb_hash_aset(kw, ID2SYM(rb_intern("x")), INT2FIX(1));
  VALUE args[2] = {INT2FIX(0), kw};
  return rb_class_new_instance_kw(2, args, klass, RB_PASS_KEYWORDS);
}

static VALUE each_cb(RB_BLOCK_CALL_FUNC_ARGLIST(x, acc)) { rb_ary_push(acc, rb_funcall(x, rb_intern("+"), 1, INT2FIX(10))); return Qnil; }
static VALUE t_block_call(VALUE self, VALUE ary) {
  VALUE acc = rb_ary_new();
  rb_block_call(ary, rb_intern("each"), 0, NULL, each_cb, acc);
  return acc;
}

static VALUE t_cached(VALUE self) { return cached; }

static VALUE t_types(VALUE self, VALUE ary) {
  VALUE out = rb_ary_new();
  for (long i = 0; i < RARRAY_LEN(ary); i++) rb_ary_push(out, INT2FIX(rb_type(rb_ary_entry(ary, i))));
  return out;
}

void Init_capi_testext(void) {
  mTest = rb_define_module("CAPITest");
  rb_define_module_function(mTest, "scan", t_scan, -1);
  rb_define_module_function(mTest, "yield2", t_yield, 1);
  rb_define_module_function(mTest, "protect", t_protect, 1);
  rb_define_module_function(mTest, "rescue", t_rescue, 1);
  rb_define_module_function(mTest, "ensure", t_ensure, 1);
  rb_define_module_function(mTest, "ensure_hit", t_ensure_hit, 0);
  rb_define_module_function(mTest, "numbers", t_numbers, 1);
  rb_define_module_function(mTest, "strings", t_strings, 1);
  rb_define_module_function(mTest, "hash_pairs", t_hash, 1);
  rb_define_module_function(mTest, "new_kw", t_new_kw, 1);
  rb_define_module_function(mTest, "block_call", t_block_call, 1);
  rb_define_module_function(mTest, "cached", t_cached, 0);
  rb_define_module_function(mTest, "types", t_types, 1);
  cBox = rb_define_class_under(mTest, "Box", rb_cObject);
  rb_define_alloc_func(cBox, box_alloc);
  rb_define_method(cBox, "initialize", box_init, 1);
  rb_define_method(cBox, "held", box_held, 0);
  rb_define_const(mTest, "ANSWER", INT2FIX(42));
  cached = rb_str_new_cstr("kept by rb_gc_register_address");
  rb_gc_register_address(&cached);
}
