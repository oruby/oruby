// The C++ side of the test extension: ruby.h in C++, where ANYARGS is (...)
// and a method function of any signature goes to rb_define_method without a
// cast, as MRI's cxxanyargs.hpp allows.
#include <ruby.h>
#include <string>

static VALUE cxx_m0(VALUE self) { return ID2SYM(rb_intern("m0")); }
static VALUE cxx_m1(VALUE self, VALUE a) { return rb_ary_new_from_args(2, ID2SYM(rb_intern("m1")), a); }
static VALUE cxx_mv(int argc, VALUE *argv, VALUE self) { return INT2FIX(argc); }
static VALUE cxx_concat(VALUE self, VALUE a, VALUE b) {
  std::string s(RSTRING_PTR(a), RSTRING_LEN(a));
  s.append(RSTRING_PTR(b), RSTRING_LEN(b));
  return rb_str_new(s.data(), (long)s.size());
}
static VALUE cxx_body(VALUE x) { return rb_ary_new_from_args(1, x); }
static int cxx_count(VALUE k, VALUE v, VALUE acc) {
  rb_ary_push(acc, k);
  return ST_CONTINUE;
}
static VALUE cxx_keys(VALUE self, VALUE h) {
  VALUE acc = rb_ary_new();
  rb_hash_foreach(h, (int (*)(...))cxx_count, acc); // an older extension's cast
  return acc;
}
static VALUE cxx_protect(VALUE self, VALUE x) {
  int state = 0;
  return rb_protect(cxx_body, x, &state);
}

extern "C" void Init_capi_testext_cxx(void) {
  VALUE m = rb_define_module("CAPITestCXX");
  rb_define_module_function(m, "m0", cxx_m0, 0);
  rb_define_module_function(m, "m1", cxx_m1, 1);
  rb_define_module_function(m, "mv", cxx_mv, -1);
  rb_define_singleton_method(m, "concat", (VALUE (*)(...))cxx_concat, 2);
  rb_define_singleton_method(m, "keys", RUBY_METHOD_FUNC(cxx_keys), 1);
  rb_define_module_function(m, "protect", cxx_protect, 1);
}
