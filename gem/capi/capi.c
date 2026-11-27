/* ORuby's Ruby C API (see include/ruby/ruby.h): MRI's extension API over
 * the mruby C API, for the one mrb_state a standalone oruby process (or a
 * compiled oruby app) runs, which orb_capi_bind() names.
 *
 * ruby.h is included first, so every definition here is checked against its
 * declaration; mruby's headers reuse a few of its macro names (RSTRING_PTR,
 * DATA_PTR, ...), which are undefined in between, so below this point they
 * are mruby's.
 */
#define ORUBY_CAPI_IMPL 1
#include "ruby.h"

#undef RSTRING_PTR
#undef RSTRING_LEN
#undef RSTRING_END
#undef RSTRING_LENINT
#undef RSTRING_GETMEM
#undef RARRAY_LEN
#undef RARRAY_LENINT
#undef RARRAY_AREF
#undef RARRAY_ASET
#undef RARRAY_PTR
#undef RARRAY_CONST_PTR
#undef RARRAY_PTR_USE
#undef DATA_PTR
#undef TRUE
#undef FALSE
#undef rb_array_len
#undef Data_Wrap_Struct
#undef Data_Make_Struct
#undef Data_Get_Struct
#undef POSFIXABLE
#undef NEGFIXABLE
#undef FIXABLE

#include <mruby.h>
#include <mruby/array.h>
#include <mruby/class.h>
#include <mruby/data.h>
#include <mruby/error.h>
#include <mruby/hash.h>
#include <mruby/numeric.h>
#include <mruby/proc.h>
#include <mruby/range.h>
#include <mruby/string.h>
#include <mruby/variable.h>
#include <mruby/presym.h>
#include <stdio.h>
#include <ctype.h>
#include <math.h>
#include <pthread.h>
#include <dlfcn.h>
#include "oruby-vm-compat.h"
#include "capi.h"

static mrb_state *M;
unsigned int rb_capi_generation;

/* MRI's Fixnum range (mruby's FIXABLE is its own) */
#undef FIXABLE
#define FIXABLE(f) ((f) <= RUBY_FIXNUM_MAX && (f) >= RUBY_FIXNUM_MIN)

/* a name capi.c calls, looked up once per bound state */
#define SYM(name) __extension__ ({ \
    static mrb_sym capi_sym; static unsigned int capi_gen; \
    if (capi_gen != rb_capi_generation) { capi_sym = mrb_intern_lit(M, name); capi_gen = rb_capi_generation; } \
    capi_sym; })

/* ------------------------------------------------------------------ */
/* VALUE <-> mrb_value                                                */

/* What has no VALUE of its own (an Integer past the Fixnum range, a Float
   no flonum holds, a C pointer) is boxed in a Data object of this type,
   whose address is the VALUE. */
static void box_free(mrb_state *mrb, void *p) { (void)mrb; free(p); }
static const mrb_data_type box_type = {"oruby_capi_box", box_free};
static struct RClass *box_class;

static VALUE box(mrb_value v) {
  mrb_value *p = (mrb_value *)malloc(sizeof(mrb_value));
  if (!p) rb_memerror();
  *p = v;
  struct RData *d = mrb_data_object_alloc(M, box_class, p, &box_type);
  return (VALUE)d;
}

#define ROTL3(v) (((v) << 3) | ((v) >> 61))
#define ROTR3(v) (((v) >> 3) | ((v) << 61))

static VALUE flonum(double d) {
  union { double d; VALUE v; } t;
  t.d = d;
  int bits = (int)((t.v >> 60) & 0x7);
  if (t.v != 0x3000000000000000 && !((bits - 3) & ~0x01)) {
    return (ROTL3(t.v) & ~(VALUE)0x01) | 0x02;
  }
  if (t.v == 0) return (VALUE)0x8000000000000002;
  return 0;
}

static double flonum_value(VALUE v) {
  if (v == (VALUE)0x8000000000000002) return 0.0;
  VALUE b63 = v >> 63;
  union { double d; VALUE v; } t;
  t.v = ROTR3((2 - b63) | (v & ~(VALUE)0x03));
  return t.d;
}

VALUE orb_capi_value(mrb_value v) {
  switch (mrb_type(v)) {
  case MRB_TT_FALSE: return mrb_nil_p(v) ? Qnil : Qfalse;
  case MRB_TT_TRUE: return Qtrue;
  case MRB_TT_UNDEF: return Qundef;
  case MRB_TT_INTEGER: {
    mrb_int i = mrb_integer(v);
    if (FIXABLE(i)) return INT2FIX(i);
    return box(v);
  }
  case MRB_TT_SYMBOL: return ID2SYM(mrb_symbol(v));
  case MRB_TT_FLOAT: {
    VALUE f = flonum(mrb_float(v));
    return f ? f : box(v);
  }
  case MRB_TT_CPTR: return box(v);
  default: return (VALUE)mrb_ptr(v);
  }
}

mrb_value orb_capi_mrb(VALUE v) {
  if (v == Qfalse) return mrb_false_value();
  if (v == Qnil) return mrb_nil_value();
  if (v == Qtrue) return mrb_true_value();
  if (v == Qundef) return mrb_undef_value();
  if (FIXNUM_P(v)) return mrb_int_value(M, FIX2LONG(v));
  if (STATIC_SYM_P(v)) return mrb_symbol_value((mrb_sym)(v >> RUBY_SPECIAL_SHIFT));
  if (FLONUM_P(v)) return mrb_float_value(M, flonum_value(v));
  struct RBasic *p = (struct RBasic *)v;
  if (p->tt == MRB_TT_CDATA && ((struct RData *)p)->type == &box_type) {
    return *(mrb_value *)((struct RData *)p)->data;
  }
  return mrb_obj_value(p);
}

#define V(x) orb_capi_value(x)
#define MV(x) orb_capi_mrb(x)
#define CLS(x) mrb_class_ptr(MV(x))
#define B(x) ((x) ? Qtrue : Qfalse)

static mrb_value *mv_array(int argc, const VALUE *argv, mrb_value *buf, int bufn) {
  mrb_value *mv = argc <= bufn ? buf : (mrb_value *)mrb_malloc(M, sizeof(mrb_value) * (size_t)argc);
  for (int i = 0; i < argc; i++) mv[i] = MV(argv[i]);
  return mv;
}
static void mv_done(mrb_value *mv, mrb_value *buf) { if (mv != buf) mrb_free(M, mv); }

static void ptr_flush(void);
static void ptr_next_gen(void);

/* calls by a literal method name: its symbol cached at the call site
   (SYM), not interned on each call (the C API's hottest cost otherwise) */
static mrb_value call_id(mrb_value recv, mrb_sym mid, mrb_int argc, const mrb_value *argv) {
  ptr_next_gen(); /* Ruby code may change what a snapshot shows */
  return mrb_funcall_argv(M, recv, mid, argc, argv);
}
#define call(recv, name, argc, argv) call_id((recv), SYM(name), (argc), (argv))
#define call0(recv, name) call_id((recv), SYM(name), 0, NULL)
#define call1(recv, name, a) __extension__ ({ mrb_value call1_arg_ = (a); call_id((recv), SYM(name), 1, &call1_arg_); })
static mrb_value kernel(void) { return mrb_obj_value(M->kernel_module); }

/* ------------------------------------------------------------------ */
/* global classes                                                      */

VALUE rb_mKernel, rb_mComparable, rb_mEnumerable, rb_mErrno, rb_mGC, rb_mMath, rb_mProcess;
VALUE rb_cBasicObject, rb_cObject, rb_cModule, rb_cClass, rb_cArray, rb_cHash, rb_cString,
  rb_cSymbol, rb_cInteger, rb_cFloat, rb_cNumeric, rb_cNilClass, rb_cTrueClass, rb_cFalseClass, rb_cProc,
  rb_cMethod, rb_cRange, rb_cRegexp, rb_cMatch, rb_cStruct, rb_cTime, rb_cIO, rb_cFile, rb_cEncoding,
  rb_cRational, rb_cComplex, rb_cRandom, rb_cThread, rb_cEnumerator;
VALUE rb_eException, rb_eStandardError, rb_eRuntimeError, rb_eArgError, rb_eTypeError,
  rb_eNameError, rb_eNoMethodError, rb_eIndexError, rb_eKeyError, rb_eRangeError, rb_eNotImpError,
  rb_eIOError, rb_eEOFError, rb_eFrozenError, rb_eStopIteration, rb_eZeroDivError, rb_eFloatDomainError,
  rb_eScriptError, rb_eLoadError, rb_eSyntaxError, rb_eNoMemError, rb_eSystemExit, rb_eInterrupt,
  rb_eSignal, rb_eFatal, rb_eSecurityError, rb_eEncodingError, rb_eEncCompatError, rb_eLocalJumpError,
  rb_eRegexpError, rb_eSystemCallError, rb_eThreadError, rb_eMathDomainError, rb_eNoMatchingPatternError,
  rb_eNoMatchingPatternKeyError, rb_eSysStackError;
VALUE rb_stdout, rb_stderr, rb_stdin, rb_fs, rb_output_fs, rb_rs, rb_default_rs, rb_output_rs;

static VALUE named(const char *path) {
  mrb_value v = mrb_obj_value(M->object_class);
  const char *p = path;
  while (*p) {
    const char *e = strstr(p, "::");
    size_t n = e ? (size_t)(e - p) : strlen(p);
    mrb_sym s = mrb_intern(M, p, n);
    if (!mrb_const_defined(M, v, s)) return Qnil;
    v = mrb_const_get(M, v, s);
    p += n + (e ? 2 : 0);
  }
  mrb_gc_register(M, v);
  return V(v);
}

static const char capi_ruby[] =
  "module Kernel\n"
  "  def self.__capi_install_new(klass)\n"
  "    klass.singleton_class.__send__(:define_method, :new) do |*a, **k, &b|\n"
  "      o = allocate\n"
  "      o.__send__(:initialize, *a, **k, &b)\n"
  "      o\n"
  "    end\n"
  "  end\n"
  "  def self.__capi_send_kw(recv, mid, a, h, b) = recv.__send__(mid, *a, **h, &b)\n"
  "  def self.__capi_public_send_kw(recv, mid, a, h, b) = recv.public_send(mid, *a, **h, &b)\n"
  "  def self.__capi_warn(msg) = warn(msg, uplevel: 0)\n"
  "end\n"
  "class Encoding\n"
  "  def self.__capi_find(name) = (find(name) rescue nil)\n"
  "  # rb_define_dummy_encoding: a dummy encoding of that name, made unless one is\n"
  "  def self.__capi_define_dummy(name)\n"
  "    __capi_find(name) || begin\n"
  "      enc = Class.instance_method(:new).bind_call(self, name.to_s, [], true, false)\n"
  "      @by_name[name.to_s.upcase] = enc\n"
  "      @list = (@list + [enc]).freeze\n"
  "      enc\n"
  "    end\n"
  "  end\n"
  "end\n";

static void root_hook(mrb_state *mrb);
static mrb_value init_extension_m(mrb_state *mrb, mrb_value self);
static size_t cdata_mark_hook(mrb_state *mrb, struct RData *d);

static void capi_unbind(mrb_state *mrb);

/* Binds the C API to `mrb`: 0 when it is bound already to another state.
   It is unbound when that state closes. */
int orb_capi_bind(mrb_state *mrb) {
  if (M) return M == mrb;
  M = mrb;
  rb_capi_generation++;
  mrb_state_atexit(M, capi_unbind);
  struct RClass *capi = mrb_define_module(M, "ORubyCAPI");
  mrb_define_class_method(M, capi, "__init_extension", init_extension_m, MRB_ARGS_REQ(1));
  box_class = mrb_define_class_under(M, capi, "Box", M->object_class);
  MRB_SET_INSTANCE_TT(box_class, MRB_TT_CDATA);
  oruby_gc_root_hook = root_hook;
  oruby_gc_cdata_mark_hook = cdata_mark_hook;
  mrb_load_string(M, capi_ruby);
  if (M->exc) { M->exc = NULL; }

  rb_mKernel = (VALUE)M->kernel_module; rb_cBasicObject = (VALUE)M->object_class->super;
  rb_cObject = (VALUE)M->object_class; rb_cModule = (VALUE)M->module_class; rb_cClass = (VALUE)M->class_class;
  rb_cArray = (VALUE)M->array_class; rb_cHash = (VALUE)M->hash_class; rb_cString = (VALUE)M->string_class;
  rb_cSymbol = (VALUE)M->symbol_class; rb_cInteger = (VALUE)M->integer_class; rb_cFloat = (VALUE)M->float_class;
  rb_cNilClass = (VALUE)M->nil_class; rb_cTrueClass = (VALUE)M->true_class; rb_cFalseClass = (VALUE)M->false_class;
  rb_cProc = (VALUE)M->proc_class; rb_cRange = (VALUE)M->range_class;
  rb_cBasicObject = named("BasicObject");
  rb_mComparable = named("Comparable"); rb_mEnumerable = named("Enumerable"); rb_mErrno = named("Errno");
  rb_mGC = named("GC"); rb_mMath = named("Math"); rb_mProcess = named("Process");
  rb_cNumeric = named("Numeric"); rb_cMethod = named("Method"); rb_cRegexp = named("Regexp");
  rb_cMatch = named("MatchData"); rb_cStruct = named("Struct"); rb_cTime = named("Time"); rb_cIO = named("IO");
  rb_cFile = named("File"); rb_cEncoding = named("Encoding"); rb_cRational = named("Rational");
  rb_cComplex = named("Complex"); rb_cRandom = named("Random"); rb_cThread = named("Thread");
  rb_cEnumerator = named("Enumerator");
  rb_eException = named("Exception"); rb_eStandardError = named("StandardError");
  rb_eRuntimeError = named("RuntimeError"); rb_eArgError = named("ArgumentError"); rb_eTypeError = named("TypeError");
  rb_eNameError = named("NameError"); rb_eNoMethodError = named("NoMethodError"); rb_eIndexError = named("IndexError");
  rb_eKeyError = named("KeyError"); rb_eRangeError = named("RangeError"); rb_eNotImpError = named("NotImplementedError");
  rb_eIOError = named("IOError"); rb_eEOFError = named("EOFError"); rb_eFrozenError = named("FrozenError");
  rb_eStopIteration = named("StopIteration"); rb_eZeroDivError = named("ZeroDivisionError");
  rb_eFloatDomainError = named("FloatDomainError"); rb_eScriptError = named("ScriptError");
  rb_eSysStackError = named("SystemStackError"); rb_eLoadError = named("LoadError"); rb_eSyntaxError = named("SyntaxError"); rb_eNoMemError = named("NoMemoryError");
  rb_eSystemExit = named("SystemExit"); rb_eInterrupt = named("Interrupt"); rb_eSignal = named("SignalException");
  rb_eFatal = named("fatal"); rb_eSecurityError = named("SecurityError"); rb_eEncodingError = named("EncodingError");
  rb_eEncCompatError = named("Encoding::CompatibilityError"); rb_eLocalJumpError = named("LocalJumpError");
  rb_eRegexpError = named("RegexpError"); rb_eSystemCallError = named("SystemCallError");
  rb_eThreadError = named("ThreadError"); rb_eMathDomainError = named("Math::DomainError");
  rb_eNoMatchingPatternError = named("NoMatchingPatternError");
  rb_eNoMatchingPatternKeyError = named("NoMatchingPatternKeyError");
  if (NIL_P(rb_eFatal)) rb_eFatal = rb_eException;
  rb_stdout = named("STDOUT"); rb_stderr = named("STDERR"); rb_stdin = named("STDIN");
  rb_fs = Qnil; rb_output_fs = Qnil; rb_output_rs = Qnil;
  rb_default_rs = rb_rs = rb_str_new_frozen(rb_str_new_cstr("\n"));
  rb_gc_register_mark_object(rb_rs);
  return 1;
}

mrb_state *orb_capi_state(void) { return M; }
int orb_capi_bind_addr(uintptr_t mrb) { return orb_capi_bind((mrb_state *)mrb); }

/* ORubyCAPI.__init_extension(ptr): runs an extension's Init_ function, a C
   method so that Go calls it through a protected funcall and a raise in it
   comes back to Go as an error, never as a longjmp past Go frames */
static mrb_value init_extension_m(mrb_state *mrb, mrb_value self) {
  (void)self;
  void (*fn)(void) = (void (*)(void))mrb_cptr(mrb_get_arg1(mrb));
  fn();
  return mrb_nil_value();
}

/* ------------------------------------------------------------------ */
/* types                                                               */

int rb_type(VALUE v) {
  if (v == Qnil) return T_NIL;
  if (v == Qtrue) return T_TRUE;
  if (v == Qfalse) return T_FALSE;
  if (v == Qundef) return T_UNDEF;
  if (FIXNUM_P(v)) return T_FIXNUM;
  if (STATIC_SYM_P(v)) return T_SYMBOL;
  if (FLONUM_P(v)) return T_FLOAT;
  mrb_value m = MV(v);
  switch (mrb_type(m)) {
  case MRB_TT_FALSE: return mrb_nil_p(m) ? T_NIL : T_FALSE;
  case MRB_TT_TRUE: return T_TRUE;
  case MRB_TT_INTEGER: return T_FIXNUM;
  case MRB_TT_FLOAT: return T_FLOAT;
  case MRB_TT_SYMBOL: return T_SYMBOL;
  case MRB_TT_CPTR: return T_DATA;
  case MRB_TT_CLASS: case MRB_TT_SCLASS: return T_CLASS;
  case MRB_TT_MODULE: return T_MODULE;
  case MRB_TT_ICLASS: return T_ICLASS;
  case MRB_TT_STRING: return T_STRING;
  case MRB_TT_ARRAY: return T_ARRAY;
  case MRB_TT_HASH: return T_HASH;
  case MRB_TT_RANGE: return T_STRUCT;
#ifdef MRB_USE_BIGINT
  case MRB_TT_BIGINT: return T_BIGNUM;
#endif
#ifdef MRB_USE_RATIONAL
  case MRB_TT_RATIONAL: return T_RATIONAL;
#endif
#ifdef MRB_USE_COMPLEX
  case MRB_TT_COMPLEX: return T_COMPLEX;
#endif
  case MRB_TT_STRUCT: return T_STRUCT;
  case MRB_TT_OBJECT: case MRB_TT_EXCEPTION: break;
  default: break;
  }
  struct RClass *c = mrb_obj_class(M, m);
  if (!NIL_P(rb_cRegexp) && mrb_obj_is_kind_of(M, m, CLS(rb_cRegexp))) return T_REGEXP;
  if (!NIL_P(rb_cMatch) && mrb_obj_is_kind_of(M, m, CLS(rb_cMatch))) return T_MATCH;
  if (!NIL_P(rb_cIO) && mrb_obj_is_kind_of(M, m, CLS(rb_cIO))) return T_FILE;
  if (mrb_type(m) == MRB_TT_CDATA || mrb_type(m) == MRB_TT_PROC) return T_DATA;
  (void)c;
  return mrb_type(m) == MRB_TT_OBJECT || mrb_type(m) == MRB_TT_EXCEPTION ? T_OBJECT : T_DATA;
}

static const char *type_name(int t) {
  switch (t) {
  case T_NIL: return "nil"; case T_OBJECT: return "Object"; case T_CLASS: return "Class";
  case T_MODULE: return "Module"; case T_FLOAT: return "Float"; case T_STRING: return "String";
  case T_REGEXP: return "Regexp"; case T_ARRAY: return "Array"; case T_FIXNUM: return "Integer";
  case T_HASH: return "Hash"; case T_STRUCT: return "Struct"; case T_BIGNUM: return "Integer";
  case T_FILE: return "File"; case T_TRUE: return "true"; case T_FALSE: return "false";
  case T_SYMBOL: return "Symbol"; case T_DATA: return "Data"; case T_MATCH: return "MatchData";
  case T_RATIONAL: return "Rational"; case T_COMPLEX: return "Complex";
  default: return "unknown";
  }
}

static const char *display_class(VALUE v) {
  if (v == Qnil) return "nil";
  if (v == Qtrue) return "true";
  if (v == Qfalse) return "false";
  return rb_obj_classname(v);
}

void rb_check_type(VALUE v, int t) {
  int vt = rb_type(v);
  if (vt == t || (t == T_FIXNUM && vt == T_BIGNUM) || (t == T_BIGNUM && vt == T_FIXNUM)) return;
  if (t == T_DATA && vt == T_DATA) return;
  rb_raise(rb_eTypeError, "wrong argument type %s (expected %s)", display_class(v), type_name(t));
}

/* ------------------------------------------------------------------ */
/* numbers                                                             */

VALUE rb_int2big(intptr_t i) { return box(mrb_int_value(M, (mrb_int)i)); }
VALUE rb_int2inum(intptr_t i) { return FIXABLE(i) ? INT2FIX(i) : rb_int2big(i); }
VALUE rb_uint2inum(uintptr_t u) {
  if (u <= (uintptr_t)FIXNUM_MAX) return INT2FIX((long)u);
  if (u <= (uintptr_t)MRB_INT_MAX) return box(mrb_int_value(M, (mrb_int)u));
  /* past mrb_int: (u >> 1) * 2 + (u & 1), as a big Integer */
  mrb_value h = call1(mrb_int_value(M, (mrb_int)(u >> 1)), "*", mrb_int_value(M, 2));
  return V(call1(h, "+", mrb_int_value(M, (mrb_int)(u & 1))));
}
VALUE rb_ll2inum(long long i) { return rb_int2inum((intptr_t)i); }
VALUE rb_ull2inum(unsigned long long u) { return rb_uint2inum((uintptr_t)u); }

long rb_num2long(VALUE v) {
  if (FIXNUM_P(v)) return FIX2LONG(v);
  if (FLONUM_P(v)) {
    double d = flonum_value(v);
    if (d < (double)LONG_MIN || d >= (double)LONG_MAX) rb_raise(rb_eRangeError, "float %g out of range of integer", d);
    return (long)d;
  }
  if (v == Qnil) rb_raise(rb_eTypeError, "no implicit conversion from nil to integer");
  if (v == Qtrue || v == Qfalse) rb_raise(rb_eTypeError, "no implicit conversion of %s into Integer", v == Qtrue ? "true" : "false");
  mrb_value m = MV(v);
  if (mrb_float_p(m)) return (long)mrb_float(m);
  return (long)mrb_as_int64(M, mrb_ensure_int_type(M, m));
}
/* an unsigned conversion takes a Bignum up to 2**64-1 (rb_big2ull); a
   negative Integer wraps, as MRI's */
unsigned long long rb_num2ull(VALUE v) {
  if (!SPECIAL_CONST_P(v) && rb_type(v) == T_BIGNUM) return rb_big2ull(v);
  return (unsigned long long)rb_num2long(v);
}
unsigned long rb_num2ulong(VALUE v) { return (unsigned long)rb_num2ull(v); }
long long rb_num2ll(VALUE v) { return (long long)rb_num2long(v); }
char rb_num2chr(VALUE v) {
  if (!SPECIAL_CONST_P(v) && rb_type(v) == T_STRING && RSTRING_LEN(MV(v)) >= 1) return RSTRING_PTR(MV(v))[0];
  return (char)rb_num2long(v);
}
double rb_num2dbl(VALUE v) {
  if (FLONUM_P(v)) return flonum_value(v);
  if (FIXNUM_P(v)) return (double)FIX2LONG(v);
  if (v == Qnil) rb_raise(rb_eTypeError, "can't convert nil into Float");
  mrb_value m = MV(v);
  if (mrb_string_p(m)) rb_raise(rb_eTypeError, "no implicit conversion to float from string");
  return mrb_as_float(M, m);
}
VALUE rb_float_new_in_heap(double d) { return box(mrb_float_value(M, d)); }
VALUE rb_float_new(double d) { VALUE f = flonum(d); return f ? f : rb_float_new_in_heap(d); }
double rb_float_value(VALUE v) { return FLONUM_P(v) ? flonum_value(v) : mrb_float(MV(v)); }
VALUE rb_Integer(VALUE v) { mrb_value a = MV(v); return V(mrb_funcall_argv(M, kernel(), SYM("Integer"), 1, &a)); }
VALUE rb_Float(VALUE v) { mrb_value a = MV(v); return V(mrb_funcall_argv(M, kernel(), SYM("Float"), 1, &a)); }
VALUE rb_String(VALUE v) { mrb_value a = MV(v); return V(mrb_funcall_argv(M, kernel(), SYM("String"), 1, &a)); }
VALUE rb_Array(VALUE v) { mrb_value a = MV(v); return V(mrb_funcall_argv(M, kernel(), SYM("Array"), 1, &a)); }
VALUE rb_str2inum(VALUE s, int base) {
  mrb_value a[2] = {MV(s), mrb_int_value(M, base)};
  return V(mrb_funcall_argv(M, kernel(), SYM("Integer"), 2, a));
}
VALUE rb_cstr2inum(const char *s, int base) { return rb_str2inum(rb_str_new_cstr(s), base); }
/* badcheck: anything but the number is an ArgumentError (Integer()), else
   the number the string starts with (String#to_i) */
VALUE rb_str_to_inum(VALUE s, int base, int badcheck) {
  if (badcheck) return rb_str2inum(s, base == 0 ? 10 : base);
  return V(call1(MV(s), "to_i", mrb_int_value(M, base == 0 ? 10 : base)));
}
VALUE rb_cstr_to_inum(const char *s, int base, int badcheck) { return rb_str_to_inum(rb_str_new_cstr(s), base, badcheck); }
double rb_str_to_dbl(VALUE s, int badcheck) {
  if (badcheck) return mrb_float(mrb_funcall_argv(M, kernel(), SYM("Float"), 1, (mrb_value[]){MV(s)}));
  return mrb_float(call0(MV(s), "to_f"));
}
double rb_cstr_to_dbl(const char *s, int badcheck) { return rb_str_to_dbl(rb_str_new_cstr(s), badcheck); }
VALUE rb_to_int(VALUE v) { return FIXNUM_P(v) ? v : V(mrb_ensure_int_type(M, MV(v))); }
VALUE rb_fix2str(VALUE v, int base) { return V(call1(MV(v), "to_s", mrb_int_value(M, base))); }
int rb_cmpint(VALUE r, VALUE a, VALUE b) {
  if (NIL_P(r)) rb_cmperr(a, b);
  if (FIXNUM_P(r)) { long l = FIX2LONG(r); return l > 0 ? 1 : l < 0 ? -1 : 0; }
  mrb_value m = MV(r);
  if (mrb_test(call1(m, ">", mrb_int_value(M, 0)))) return 1;
  if (mrb_test(call1(m, "<", mrb_int_value(M, 0)))) return -1;
  return 0;
}

/* ------------------------------------------------------------------ */
/* symbols                                                             */

ID rb_intern(const char *name) { return mrb_intern_cstr(M, name); }
ID rb_intern2(const char *name, long len) { return mrb_intern(M, name, (size_t)len); }
ID rb_intern_const(const char *name) { return mrb_intern_cstr(M, name); }
ID rb_intern_str(VALUE s) { return mrb_symbol(call0(MV(s), "to_sym")); }
ID rb_to_id(VALUE v) {
  if (STATIC_SYM_P(v)) return (ID)(v >> RUBY_SPECIAL_SHIFT);
  mrb_value m = MV(v);
  if (mrb_string_p(m)) return rb_intern_str(v);
  if (mrb_respond_to(M, m, SYM("to_str"))) return rb_intern_str(V(call0(m, "to_str")));
  rb_raise(rb_eTypeError, "%+" PRIsVALUE " is not a symbol nor a string", v);
}
ID rb_check_id(volatile VALUE *v) {
  if (STATIC_SYM_P(*v)) return (ID)(*v >> RUBY_SPECIAL_SHIFT);
  mrb_value m = MV(*v);
  if (!mrb_string_p(m)) {
    if (!mrb_respond_to(M, m, SYM("to_str"))) rb_raise(rb_eTypeError, "%+" PRIsVALUE " is not a symbol nor a string", *v);
    m = call0(m, "to_str");
    *v = V(m);
  }
  mrb_value s = mrb_check_intern_str(M, m);
  return mrb_nil_p(s) ? 0 : mrb_symbol(s);
}
VALUE rb_check_symbol(volatile VALUE *v) { ID id = rb_check_id(v); return id ? ID2SYM(id) : Qnil; }
VALUE rb_check_symbol_cstr(const char *p, long n, rb_encoding *enc) {
  volatile VALUE s = rb_enc_str_new(p, n, enc ? enc : rb_utf8_encoding());
  return rb_check_symbol(&s);
}
const char *rb_id2name(ID id) { return mrb_sym_name(M, (mrb_sym)id); }
VALUE rb_id2str(ID id) {
  if (id == 0) return Qfalse;
  return V(mrb_sym_str(M, (mrb_sym)id));
}
VALUE rb_id2sym(ID id) { return ID2SYM(id); }
ID rb_sym2id(VALUE v) {
  if (STATIC_SYM_P(v)) return (ID)(v >> RUBY_SPECIAL_SHIFT);
  rb_raise(rb_eTypeError, "wrong argument type %s (expected Symbol)", display_class(v));
}
VALUE rb_sym2str(VALUE v) { return V(call0(MV(v), "to_s")); }
VALUE rb_to_symbol(VALUE v) { return ID2SYM(rb_to_id(v)); }
VALUE rb_str_intern(VALUE s) { return V(call0(MV(s), "to_sym")); }

/* ------------------------------------------------------------------ */
/* method definitions                                                  */

struct capi_method { rb_capi_anyargs_func fn; int arity; };

#define CAPI_ALLOC (-100)

typedef VALUE (*f0)(VALUE);
typedef VALUE (*f1)(VALUE, VALUE);
typedef VALUE (*f2)(VALUE, VALUE, VALUE);
typedef VALUE (*f3)(VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*f4)(VALUE, VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*f5)(VALUE, VALUE, VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*f6)(VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*f7)(VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*f8)(VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*f9)(VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*f10)(VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE, VALUE);
typedef VALUE (*fv)(int, const VALUE *, VALUE);

static mrb_value trampoline(mrb_state *mrb, mrb_value self) {
  const struct capi_method *cm = (const struct capi_method *)mrb_cptr(mrb_proc_cfunc_env_get(mrb, 0));
  const mrb_value *args;
  mrb_int argc;
  mrb_value kw = mrb_nil_value(), blk;
  mrb_kwargs kwargs = {0, 0, NULL, NULL, &kw};
  mrb_get_args(mrb, "*!:&", &args, &argc, &kwargs, &blk);
  /* keywords are a last Hash argument, as MRI gives a C method them */
  if (mrb_hash_p(kw) && mrb_hash_empty_p(mrb, kw)) kw = mrb_nil_value();
  int n = (int)argc + (mrb_nil_p(kw) ? 0 : 1);
  VALUE av_buf[16];
  VALUE *av = n <= 16 ? av_buf : (VALUE *)mrb_malloc(mrb, sizeof(VALUE) * (size_t)n);
  for (mrb_int i = 0; i < argc; i++) av[i] = V(args[i]);
  if (!mrb_nil_p(kw)) av[argc] = V(kw);
  VALUE s = V(self), r;
  rb_capi_anyargs_func f = cm->fn;
  ptr_next_gen();
  if (cm->arity == CAPI_ALLOC) {
    r = ((f0)f)(s);
  }
  else if (cm->arity == -1) {
    r = ((fv)f)(n, av, s);
  }
  else if (cm->arity == -2) {
    r = ((f1)f)(s, rb_ary_new_from_values(n, av));
  }
  else {
    if (n != cm->arity) rb_error_arity(n, cm->arity, cm->arity);
    switch (cm->arity) {
    case 0: r = ((f0)f)(s); break;
    case 1: r = ((f1)f)(s, av[0]); break;
    case 2: r = ((f2)f)(s, av[0], av[1]); break;
    case 3: r = ((f3)f)(s, av[0], av[1], av[2]); break;
    case 4: r = ((f4)f)(s, av[0], av[1], av[2], av[3]); break;
    case 5: r = ((f5)f)(s, av[0], av[1], av[2], av[3], av[4]); break;
    case 6: r = ((f6)f)(s, av[0], av[1], av[2], av[3], av[4], av[5]); break;
    case 7: r = ((f7)f)(s, av[0], av[1], av[2], av[3], av[4], av[5], av[6]); break;
    case 8: r = ((f8)f)(s, av[0], av[1], av[2], av[3], av[4], av[5], av[6], av[7]); break;
    case 9: r = ((f9)f)(s, av[0], av[1], av[2], av[3], av[4], av[5], av[6], av[7], av[8]); break;
    case 10: r = ((f10)f)(s, av[0], av[1], av[2], av[3], av[4], av[5], av[6], av[7], av[8], av[9]); break;
    default: rb_raise(rb_eNotImpError, "C method arity %d", cm->arity);
    }
  }
  if (av != av_buf) mrb_free(mrb, av);
  ptr_next_gen();
  return MV(r);
}

static void define_method(struct RClass *c, mrb_sym mid, rb_capi_anyargs_func fn, int arity, int vis) {
  if (arity < -2 && arity != CAPI_ALLOC) rb_raise(rb_eArgError, "arity out of range: %d for -2..15", arity);
  if (arity > 10) rb_raise(rb_eNotImpError, "C method arity %d (oruby supports up to 10)", arity);
  struct capi_method *cm = (struct capi_method *)malloc(sizeof *cm);
  if (!cm) rb_memerror();
  cm->fn = fn;
  cm->arity = arity;
  mrb_value env = mrb_cptr_value(M, cm);
  struct RProc *p = mrb_proc_new_cfunc_with_env(M, trampoline, 1, &env);
  mrb_method_t m;
  MRB_METHOD_FROM_PROC(m, p);
  MRB_METHOD_SET_VISIBILITY(m, vis);
  mrb_define_method_raw(M, c, mid, m);
}

void rb_define_method_id(VALUE k, ID mid, VALUE (*fn)(ANYARGS), int argc) {
  define_method(CLS(k), (mrb_sym)mid, fn, argc, MRB_METHOD_PUBLIC_FL);
}
void rb_define_method(VALUE k, const char *name, VALUE (*fn)(ANYARGS), int argc) {
  rb_define_method_id(k, rb_intern(name), fn, argc);
}
void rb_define_private_method(VALUE k, const char *name, VALUE (*fn)(ANYARGS), int argc) {
  define_method(CLS(k), rb_intern(name), fn, argc, MRB_METHOD_PRIVATE_FL);
}
void rb_define_protected_method(VALUE k, const char *name, VALUE (*fn)(ANYARGS), int argc) {
  define_method(CLS(k), rb_intern(name), fn, argc, MRB_METHOD_PROTECTED_FL);
}
void rb_define_singleton_method(VALUE obj, const char *name, VALUE (*fn)(ANYARGS), int argc) {
  define_method(mrb_class_ptr(mrb_singleton_class(M, MV(obj))), rb_intern(name), fn, argc, MRB_METHOD_PUBLIC_FL);
}
void rb_define_module_function(VALUE mod, const char *name, VALUE (*fn)(ANYARGS), int argc) {
  rb_define_private_method(mod, name, fn, argc);
  rb_define_singleton_method(mod, name, fn, argc);
}
void rb_define_global_function(const char *name, VALUE (*fn)(ANYARGS), int argc) {
  rb_define_module_function(rb_mKernel, name, fn, argc);
}
void rb_undef_method(VALUE k, const char *name) { mrb_undef_method(M, CLS(k), name); }
void rb_define_alias(VALUE k, const char *a, const char *b) { mrb_define_alias(M, CLS(k), a, b); }
void rb_define_attr(VALUE k, const char *name, int r, int w) {
  mrb_value sym = mrb_symbol_value(rb_intern(name));
  if (r) call1(MV(k), "attr_reader", sym);
  if (w) call1(MV(k), "attr_writer", sym);
}
void rb_define_const(VALUE mod, const char *name, VALUE v) { mrb_define_const(M, CLS(mod), name, MV(v)); }
void rb_define_global_const(const char *name, VALUE v) { mrb_define_global_const(M, name, MV(v)); }

static void cdata_class(struct RClass *c);
void rb_define_alloc_func(VALUE k, rb_alloc_func_t fn) {
  struct RClass *c = CLS(k);
  cdata_class(c);
  define_method(mrb_class_ptr(mrb_singleton_class(M, mrb_obj_value(c))), SYM("allocate"),
                (rb_capi_anyargs_func)fn, CAPI_ALLOC, MRB_METHOD_PUBLIC_FL);
  mrb_value a = mrb_obj_value(c);
  mrb_funcall_argv(M, kernel(), SYM("__capi_install_new"), 1, &a);
}
static VALUE undef_alloc(VALUE k) {
  rb_raise(rb_eTypeError, "allocator undefined for %" PRIsVALUE, k);
}
void rb_undef_alloc_func(VALUE k) { rb_define_alloc_func(k, undef_alloc); }

VALUE rb_define_class(const char *name, VALUE super) { return (VALUE)mrb_define_class(M, name, CLS(super)); }
VALUE rb_define_class_under(VALUE outer, const char *name, VALUE super) {
  return (VALUE)mrb_define_class_under(M, CLS(outer), name, CLS(super));
}
VALUE rb_define_class_id_under(VALUE outer, ID id, VALUE super) {
  return (VALUE)mrb_define_class_under_id(M, CLS(outer), (mrb_sym)id, CLS(super));
}
VALUE rb_define_module(const char *name) { return (VALUE)mrb_define_module(M, name); }
VALUE rb_define_module_under(VALUE outer, const char *name) { return (VALUE)mrb_define_module_under(M, CLS(outer), name); }
VALUE rb_define_module_id_under(VALUE outer, ID id) { return (VALUE)mrb_define_module_under_id(M, CLS(outer), (mrb_sym)id); }
VALUE rb_class_new(VALUE super) { mrb_value a = MV(super); return V(mrb_funcall_argv(M, mrb_obj_value(M->class_class), SYM("new"), 1, &a)); }
VALUE rb_module_new(void) { return V(call0(mrb_obj_value(M->module_class), "new")); }
void rb_include_module(VALUE k, VALUE mod) { mrb_include_module(M, CLS(k), CLS(mod)); }
void rb_extend_object(VALUE obj, VALUE mod) { call1(MV(obj), "extend", MV(mod)); }
void rb_prepend_module(VALUE k, VALUE mod) { mrb_prepend_module(M, CLS(k), CLS(mod)); }
VALUE rb_const_get(VALUE mod, ID id) { return V(mrb_const_get(M, MV(mod), (mrb_sym)id)); }
VALUE rb_const_get_at(VALUE mod, ID id) {
  mrb_value a[2] = {mrb_symbol_value((mrb_sym)id), mrb_false_value()};
  return V(mrb_funcall_argv(M, MV(mod), SYM("const_get"), 2, a));
}
VALUE rb_const_get_from(VALUE mod, ID id) { return rb_const_get(mod, id); }
void rb_const_set(VALUE mod, ID id, VALUE v) { mrb_const_set(M, MV(mod), (mrb_sym)id, MV(v)); }
int rb_const_defined(VALUE mod, ID id) { return mrb_const_defined(M, MV(mod), (mrb_sym)id); }
int rb_const_defined_at(VALUE mod, ID id) { return mrb_const_defined_at(M, MV(mod), (mrb_sym)id); }
VALUE rb_path2class(const char *path) {
  VALUE v = named(path);
  if (NIL_P(v)) rb_raise(rb_eArgError, "undefined class/module %s", path);
  return v;
}
VALUE rb_path_to_class(VALUE path) { return rb_path2class(RSTRING_PTR(MV(path))); }
VALUE rb_class_name(VALUE k) { mrb_value n = call0(MV(k), "name"); return mrb_nil_p(n) ? V(call0(MV(k), "inspect")) : V(n); }
VALUE rb_class_path(VALUE k) { return rb_class_name(k); }
VALUE rb_mod_name(VALUE k) { return V(call0(MV(k), "name")); }
const char *rb_class2name(VALUE k) { return RSTRING_PTR(MV(rb_class_name(k))); }
const char *rb_obj_classname(VALUE obj) { return rb_class2name(rb_obj_class(obj)); }
VALUE rb_class_of(VALUE v) { return (VALUE)mrb_class(M, MV(v)); }
VALUE rb_obj_class(VALUE v) { return (VALUE)mrb_obj_class(M, MV(v)); }
VALUE rb_class_real(VALUE k) { return (VALUE)mrb_class_real(CLS(k)); }
VALUE rb_class_superclass(VALUE k) { return V(call0(MV(k), "superclass")); }
VALUE rb_class_inherited_p(VALUE a, VALUE b) { return V(call1(MV(a), "<=", MV(b))); }
VALUE rb_singleton_class(VALUE v) { return V(mrb_singleton_class(M, MV(v))); }
static VALUE send_kw(mrb_value recv, mrb_sym mid, int argc, const VALUE *argv, mrb_value blk, int kw, int pub);
VALUE rb_class_new_instance_kw(int argc, const VALUE *argv, VALUE k, int kw) {
  return send_kw(MV(k), SYM("new"), argc, argv, mrb_nil_value(), kw, 0);
}
VALUE rb_class_new_instance(int argc, const VALUE *argv, VALUE k) { return rb_class_new_instance_kw(argc, argv, k, 0); }
VALUE rb_class_new_instance_pass_kw(int argc, const VALUE *argv, VALUE k) {
  return rb_class_new_instance_kw(argc, argv, k, rb_keyword_given_p());
}
/* a Struct is allocated with its members, all nil, as MRI's: Struct's own
   initialize (not the class's) sizes it, with no arguments */
VALUE rb_obj_alloc(VALUE k) {
  mrb_value obj = call0(MV(k), "allocate");
  if (mrb_type(obj) == MRB_TT_STRUCT && mrb_obj_is_kind_of(M, obj, CLS(rb_cStruct))) {
    mrb_value init = call1(MV(rb_cStruct), "instance_method", mrb_symbol_value(SYM("initialize")));
    call1(init, "bind_call", obj);
  }
  else if (mrb_type(obj) == MRB_TT_STRUCT) {
    /* a Data (mruby-data keeps its values as an Array): its members, nil
       and settable until it is frozen, as MRI's struct-based Data */
    mrb_ary_resize(M, obj, RARRAY_LEN(call0(MV(k), "members")));
  }
  return V(obj);
}
VALUE rb_obj_is_kind_of(VALUE v, VALUE k) { return B(mrb_obj_is_kind_of(M, MV(v), CLS(k))); }
VALUE rb_obj_is_instance_of(VALUE v, VALUE k) { return B(mrb_obj_is_instance_of(M, MV(v), CLS(k))); }
/* public methods only, through #respond_to? (a redefinition counts), as MRI's
   rb_obj_respond_to(obj, id, FALSE) */
int rb_respond_to(VALUE v, ID id) { return rb_obj_respond_to(v, id, 0); }
/* klass's instances have the method (ex: a public one) */
int rb_method_boundp(VALUE klass, ID id, int ex) {
  struct RClass *c = mrb_class_ptr(MV(klass));
  if (!mrb_obj_respond_to(M, c, (mrb_sym)id)) return 0;
  if (!ex) return 1;
  mrb_value a[1] = {mrb_symbol_value((mrb_sym)id)};
  return mrb_test(mrb_funcall_argv(M, mrb_obj_value(c), SYM("public_method_defined?"), 1, a));
}
int rb_obj_respond_to(VALUE v, ID id, int priv) {
  mrb_value a[2] = {mrb_symbol_value((mrb_sym)id), mrb_bool_value(priv)};
  return mrb_test(mrb_funcall_argv(M, MV(v), SYM("respond_to?"), 2, a));
}
VALUE rb_obj_freeze(VALUE v) { if (!SPECIAL_CONST_P(v)) mrb_obj_freeze(M, MV(v)); return v; }
VALUE rb_obj_frozen_p(VALUE v) { return SPECIAL_CONST_P(v) ? Qtrue : B(mrb_frozen_p(mrb_basic_ptr(MV(v)))); }
void rb_check_frozen(VALUE v) { if (!SPECIAL_CONST_P(v) && mrb_frozen_p(mrb_basic_ptr(MV(v)))) mrb_frozen_error(M, mrb_ptr(MV(v))); }
VALUE rb_obj_dup(VALUE v) { return V(mrb_obj_dup(M, MV(v))); }
VALUE rb_obj_clone(VALUE v) { return V(call0(MV(v), "clone")); }
VALUE rb_obj_id(VALUE v) { return rb_int2inum((intptr_t)mrb_obj_id(MV(v))); }
VALUE rb_equal(VALUE a, VALUE b) { return a == b ? Qtrue : B(mrb_equal(M, MV(a), MV(b))); }
int rb_eql(VALUE a, VALUE b) { return a == b || mrb_eql(M, MV(a), MV(b)); }
VALUE rb_inspect(VALUE v) { return V(mrb_inspect(M, MV(v))); }
VALUE rb_obj_as_string(VALUE v) { return V(mrb_obj_as_string(M, MV(v))); }
VALUE rb_any_to_s(VALUE v) { return V(mrb_any_to_s(M, MV(v))); }
VALUE rb_obj_method(VALUE v, VALUE name) { return V(call1(MV(v), "method", MV(name))); }
VALUE rb_ivar_get(VALUE v, ID id) { return SPECIAL_CONST_P(v) ? Qnil : V(mrb_iv_get(M, MV(v), (mrb_sym)id)); }
VALUE rb_ivar_set(VALUE v, ID id, VALUE x) { mrb_iv_set(M, MV(v), (mrb_sym)id, MV(x)); return x; }
VALUE rb_ivar_defined(VALUE v, ID id) { return SPECIAL_CONST_P(v) ? Qfalse : B(mrb_iv_defined(M, MV(v), (mrb_sym)id)); }
VALUE rb_iv_get(VALUE v, const char *name) { return rb_ivar_get(v, rb_intern(name)); }
VALUE rb_iv_set(VALUE v, const char *name, VALUE x) { return rb_ivar_set(v, rb_intern(name), x); }
VALUE rb_attr_get(VALUE v, ID id) { return rb_ivar_get(v, id); }
VALUE rb_cvar_get(VALUE k, ID id) { return V(mrb_cv_get(M, MV(k), (mrb_sym)id)); }
void rb_cvar_set(VALUE k, ID id, VALUE v) { mrb_cv_set(M, MV(k), (mrb_sym)id, MV(v)); }
static mrb_sym gvar_sym(const char *name) {
  if (name[0] == '$') return mrb_intern_cstr(M, name);
  size_t n = strlen(name);
  char *buf = (char *)mrb_malloc(M, n + 2);
  buf[0] = '$';
  memcpy(buf + 1, name, n + 1);
  mrb_sym s = mrb_intern(M, buf, n + 1);
  mrb_free(M, buf);
  return s;
}
VALUE rb_gv_get(const char *name) { return V(mrb_gv_get(M, gvar_sym(name))); }
VALUE rb_gv_set(const char *name, VALUE v) { mrb_gv_set(M, gvar_sym(name), MV(v)); return v; }
VALUE rb_hash(VALUE v) { return V(call0(MV(v), "hash")); }
VALUE rb_obj_instance_variables(VALUE v) { return V(call0(MV(v), "instance_variables")); }

VALUE rb_check_convert_type(VALUE v, int type, const char *tname, const char *method) {
  if (rb_type(v) == type) return v;
  mrb_value m = MV(v);
  if (!mrb_respond_to(M, m, mrb_intern_cstr(M, method))) return Qnil;
  VALUE r = V(call_id(m, mrb_intern_cstr(M, method), 0, NULL));
  if (!NIL_P(r) && rb_type(r) != type) {
    rb_raise(rb_eTypeError, "can't convert %s to %s (%s#%s gives %s)", display_class(v), tname,
             display_class(v), method, display_class(r));
  }
  return r;
}
VALUE rb_convert_type(VALUE v, int type, const char *tname, const char *method) {
  if (rb_type(v) == type) return v;
  VALUE r = rb_check_convert_type(v, type, tname, method);
  if (NIL_P(r)) rb_raise(rb_eTypeError, "no implicit conversion of %s into %s", display_class(v), tname);
  return r;
}
VALUE rb_check_string_type(VALUE v) { return rb_check_convert_type(v, T_STRING, "String", "to_str"); }
VALUE rb_check_array_type(VALUE v) { return rb_check_convert_type(v, T_ARRAY, "Array", "to_ary"); }
VALUE rb_check_hash_type(VALUE v) { return rb_check_convert_type(v, T_HASH, "Hash", "to_hash"); }
VALUE rb_check_funcall(VALUE recv, ID mid, int argc, const VALUE *argv) {
  mrb_value r = MV(recv);
  mrb_value a[2] = {mrb_symbol_value((mrb_sym)mid), mrb_true_value()};
  if (!mrb_test(mrb_funcall_argv(M, r, SYM("respond_to?"), 2, a))) return Qundef;
  return rb_funcallv(recv, mid, argc, argv);
}

/* ------------------------------------------------------------------ */
/* calls and blocks                                                    */

VALUE rb_funcallv(VALUE recv, ID mid, int argc, const VALUE *argv) {
  ptr_next_gen();
  mrb_value buf[8];
  mrb_value *mv = mv_array(argc, argv, buf, 8);
  mrb_value r = mrb_funcall_argv(M, MV(recv), (mrb_sym)mid, argc, mv);
  mv_done(mv, buf);
  return V(r);
}
VALUE rb_apply(VALUE recv, ID mid, VALUE args) {
  mrb_value a = MV(rb_Array(args));
  ptr_next_gen();
  return V(mrb_funcall_argv(M, MV(recv), (mrb_sym)mid, RARRAY_LEN(a), RARRAY_PTR(a)));
}
VALUE rb_funcall(VALUE recv, ID mid, int n, ...) {
  VALUE buf[16];
  VALUE *av = n <= 16 ? buf : (VALUE *)mrb_malloc(M, sizeof(VALUE) * (size_t)n);
  va_list ap;
  va_start(ap, n);
  for (int i = 0; i < n; i++) av[i] = va_arg(ap, VALUE);
  va_end(ap);
  VALUE r = rb_funcallv(recv, mid, n, av);
  if (av != buf) mrb_free(M, av);
  return r;
}

/* a call whose last argument is the keywords: Ruby passes them on */
static VALUE send_kw(mrb_value recv, mrb_sym mid, int argc, const VALUE *argv, mrb_value blk, int kw, int pub) {
  if (kw && argc > 0 && rb_type(argv[argc - 1]) == T_HASH) {
    mrb_value a[5] = {recv, mrb_symbol_value(mid), mrb_nil_value(), MV(argv[argc - 1]), blk};
    VALUE ary = rb_ary_new_from_values(argc - 1, argv);
    a[2] = MV(ary);
    return V(mrb_funcall_argv(M, kernel(), pub ? SYM("__capi_public_send_kw") : SYM("__capi_send_kw"), 5, a));
  }
  mrb_value buf[8];
  mrb_value *mv = mv_array(argc, argv, buf, 8);
  mrb_value r;
  if (pub) {
    mrb_value *pv = (mrb_value *)mrb_malloc(M, sizeof(mrb_value) * (size_t)(argc + 1));
    pv[0] = mrb_symbol_value(mid);
    for (int i = 0; i < argc; i++) pv[i + 1] = mv[i];
    r = mrb_funcall_with_block(M, recv, SYM("public_send"), argc + 1, pv, blk);
    mrb_free(M, pv);
  }
  else {
    r = mrb_funcall_with_block(M, recv, mid, argc, mv, blk);
  }
  mv_done(mv, buf);
  return V(r);
}
VALUE rb_funcallv_kw(VALUE recv, ID mid, int argc, const VALUE *argv, int kw) {
  return send_kw(MV(recv), (mrb_sym)mid, argc, argv, mrb_nil_value(), kw, 0);
}
VALUE rb_funcallv_public(VALUE recv, ID mid, int argc, const VALUE *argv) {
  return send_kw(MV(recv), (mrb_sym)mid, argc, argv, mrb_nil_value(), 0, 1);
}
VALUE rb_funcallv_public_kw(VALUE recv, ID mid, int argc, const VALUE *argv, int kw) {
  return send_kw(MV(recv), (mrb_sym)mid, argc, argv, mrb_nil_value(), kw, 1);
}
static mrb_value current_block(void) {
  struct RProc *b = M->c->ci->blk;
  return b ? mrb_obj_value(b) : mrb_nil_value();
}
VALUE rb_funcall_passing_block(VALUE recv, ID mid, int argc, const VALUE *argv) {
  return send_kw(MV(recv), (mrb_sym)mid, argc, argv, current_block(), 0, 1);
}
VALUE rb_funcall_with_block(VALUE recv, ID mid, int argc, const VALUE *argv, VALUE blk) {
  ptr_next_gen();
  return send_kw(MV(recv), (mrb_sym)mid, argc, argv, MV(blk), 0, 1);
}
VALUE rb_funcall_with_block_kw(VALUE recv, ID mid, int argc, const VALUE *argv, VALUE blk, int kw) {
  return send_kw(MV(recv), (mrb_sym)mid, argc, argv, MV(blk), kw, 1);
}
VALUE rb_call_super(int argc, const VALUE *argv) {
  ptr_next_gen();
  (void)argc; (void)argv;
  rb_raise(rb_eNotImpError, "rb_call_super is not supported by oruby's C API");
}
int rb_block_given_p(void) { return M->c->ci->blk != NULL; }
void rb_need_block(void) { if (!rb_block_given_p()) rb_raise(rb_eLocalJumpError, "no block given"); }
VALUE rb_block_proc(void) { return V(current_block()); }
VALUE rb_block_lambda(void) { return rb_block_proc(); }
VALUE rb_yield_values2(int argc, const VALUE *argv) {
  ptr_next_gen();
  mrb_value blk = current_block();
  if (mrb_nil_p(blk)) rb_raise(rb_eLocalJumpError, "no block given (yield)");
  mrb_value buf[8];
  mrb_value *mv = mv_array(argc, argv, buf, 8);
  mrb_value r = mrb_yield_argv(M, blk, argc, mv);
  mv_done(mv, buf);
  return V(r);
}
VALUE rb_yield(VALUE v) { return v == Qundef ? rb_yield_values2(0, NULL) : rb_yield_values2(1, &v); }
VALUE rb_yield_values(int n, ...) {
  VALUE buf[16];
  if (n > 16) n = 16;
  va_list ap;
  va_start(ap, n);
  for (int i = 0; i < n; i++) buf[i] = va_arg(ap, VALUE);
  va_end(ap);
  return rb_yield_values2(n, buf);
}
VALUE rb_yield_splat(VALUE ary) {
  mrb_value a = MV(ary);
  mrb_value blk = current_block();
  if (mrb_nil_p(blk)) rb_raise(rb_eLocalJumpError, "no block given (yield)");
  return V(mrb_yield_argv(M, blk, RARRAY_LEN(a), RARRAY_PTR(a)));
}

struct capi_block { rb_block_call_func_t fn; VALUE data; };
static mrb_value block_trampoline(mrb_state *mrb, mrb_value self) {
  (void)self;
  const struct capi_block *cb = (const struct capi_block *)mrb_cptr(mrb_proc_cfunc_env_get(mrb, 0));
  const mrb_value *args;
  mrb_int argc;
  mrb_value blk;
  mrb_get_args(mrb, "*!&", &args, &argc, &blk);
  VALUE buf[16];
  VALUE *av = argc <= 16 ? buf : (VALUE *)mrb_malloc(mrb, sizeof(VALUE) * (size_t)argc);
  for (mrb_int i = 0; i < argc; i++) av[i] = V(args[i]);
  ptr_next_gen();
  VALUE r = cb->fn(argc > 0 ? av[0] : Qnil, cb->data, (int)argc, av, V(blk));
  if (av != buf) mrb_free(mrb, av);
  ptr_next_gen();
  return MV(r);
}
static mrb_value block_proc(rb_block_call_func_t fn, VALUE data) {
  struct capi_block *cb = (struct capi_block *)mrb_malloc(M, sizeof *cb);
  cb->fn = fn;
  cb->data = data;
  /* the Data object frees the struct with the Proc (it sits in the env) */
  struct RData *holder = mrb_data_object_alloc(M, box_class, cb, &box_type);
  mrb_value env[2] = {mrb_cptr_value(M, cb), mrb_obj_value(holder)};
  if (!SPECIAL_CONST_P(data)) {
    mrb_value d = MV(data);
    mrb_iv_set(M, mrb_obj_value(holder), SYM("@data"), d);
  }
  return mrb_obj_value(mrb_proc_new_cfunc_with_env(M, block_trampoline, 2, env));
}
VALUE rb_block_call(VALUE obj, ID mid, int argc, const VALUE *argv, rb_block_call_func_t fn, VALUE data) {
  ptr_next_gen();
  mrb_value blk = fn ? block_proc(fn, data) : mrb_nil_value();
  mrb_value buf[8];
  mrb_value *mv = mv_array(argc, argv, buf, 8);
  mrb_value r = mrb_funcall_with_block(M, MV(obj), (mrb_sym)mid, argc, mv, blk);
  mv_done(mv, buf);
  return V(r);
}
VALUE rb_proc_new(rb_block_call_func_t fn, VALUE data) {
  mrb_value p = block_proc(fn, data);
  return V(mrb_funcall_with_block(M, kernel(), SYM("proc"), 0, NULL, p));
}
VALUE rb_proc_call(VALUE proc, VALUE args) {
  mrb_value a = MV(args);
  return V(mrb_funcall_argv(M, MV(proc), SYM("call"), RARRAY_LEN(a), RARRAY_PTR(a)));
}
VALUE rb_proc_call_with_block(VALUE proc, int argc, const VALUE *argv, VALUE blk) {
  ptr_next_gen();
  mrb_value buf[8];
  mrb_value *mv = mv_array(argc, argv, buf, 8);
  mrb_value r = mrb_funcall_with_block(M, MV(proc), SYM("call"), argc, mv, MV(blk));
  mv_done(mv, buf);
  return V(r);
}
VALUE rb_obj_is_proc(VALUE v) { return B(!SPECIAL_CONST_P(v) && mrb_proc_p(MV(v))); }
int rb_keyword_given_p(void) { return M->c->ci->kw; }
VALUE rb_eval_string(const char *src) { mrb_value s = mrb_str_new_cstr(M, src); return V(mrb_funcall_argv(M, kernel(), SYM("eval"), 1, &s)); }
VALUE rb_require(const char *name) { mrb_value s = mrb_str_new_cstr(M, name); return V(mrb_funcall_argv(M, kernel(), SYM("require"), 1, &s)); }
VALUE rb_f_require(VALUE self, VALUE name) { (void)self; mrb_value s = MV(name); return V(mrb_funcall_argv(M, kernel(), SYM("require"), 1, &s)); }
VALUE rb_current_receiver(void) { return V(M->c->ci->stack[0]); }
ID rb_frame_this_func(void) { return M->c->ci->mid; }
VALUE rb_enumeratorize(VALUE obj, VALUE meth, int argc, const VALUE *argv) {
  VALUE *av = (VALUE *)mrb_malloc(M, sizeof(VALUE) * (size_t)(argc + 1));
  av[0] = meth;
  for (int i = 0; i < argc; i++) av[i + 1] = argv[i];
  VALUE r = rb_funcallv(obj, SYM("enum_for"), argc + 1, av);
  mrb_free(M, av);
  return r;
}

/* kwf: RB_SCAN_ARGS_PASS_CALLED_KEYWORDS (0) takes a last Hash as the
   keywords when the call passed keywords; KEYWORDS and LAST_HASH_KEYWORDS
   take a last Hash as them in any case */
static int scan_args_va(int kwf, int argc, const VALUE *argv, const char *fmt, va_list ap);
int rb_scan_args(int argc, const VALUE *argv, const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  int n = scan_args_va(RB_SCAN_ARGS_PASS_CALLED_KEYWORDS, argc, argv, fmt, ap);
  va_end(ap);
  return n;
}
int rb_scan_args_kw(int kwf, int argc, const VALUE *argv, const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  int n = scan_args_va(kwf, argc, argv, fmt, ap);
  va_end(ap);
  return n;
}
static int scan_args_va(int kwf, int argc, const VALUE *argv, const char *fmt, va_list ap) {
  int lead = 0, opt = 0, trail = 0, rest = 0, hash = 0, block = 0;
  const char *p = fmt;
  if (isdigit((unsigned char)*p)) { lead = *p++ - '0'; if (isdigit((unsigned char)*p)) opt = *p++ - '0'; }
  if (*p == '*') { rest = 1; p++; }
  if (isdigit((unsigned char)*p)) trail = *p++ - '0';
  if (*p == ':') { hash = 1; p++; }
  if (*p == '&') { block = 1; p++; }
  VALUE kw = Qnil;
  if (hash && argc > 0 && (kwf != RB_SCAN_ARGS_PASS_CALLED_KEYWORDS || rb_keyword_given_p()) && rb_type(argv[argc - 1]) == T_HASH) {
    kw = argv[argc - 1];
    argc--;
  }
  if (argc < lead + trail || (!rest && argc > lead + opt + trail)) {
    rb_error_arity(argc, lead + trail, rest ? -1 : lead + opt + trail);
  }
  int i = 0;
  for (int k = 0; k < lead; k++, i++) { VALUE *v = va_arg(ap, VALUE *); if (v) *v = argv[i]; }
  int avail = argc - lead - trail;
  for (int k = 0; k < opt; k++) {
    VALUE *v = va_arg(ap, VALUE *);
    if (k < avail) { if (v) *v = argv[i]; i++; }
    else if (v) *v = Qnil;
  }
  if (rest) {
    int nrest = argc - i - trail;
    VALUE *v = va_arg(ap, VALUE *);
    if (v) *v = rb_ary_new_from_values(nrest > 0 ? nrest : 0, argv + i);
    if (nrest > 0) i += nrest;
  }
  for (int k = 0; k < trail; k++, i++) { VALUE *v = va_arg(ap, VALUE *); if (v) *v = argv[i]; }
  if (hash) { VALUE *v = va_arg(ap, VALUE *); if (v) *v = kw; }
  if (block) { VALUE *v = va_arg(ap, VALUE *); if (v) *v = rb_block_proc(); }
  return argc;
}

/* catch/throw: Kernel's, with the C function as the block */
VALUE rb_catch_obj(VALUE tag, rb_block_call_func_t fn, VALUE data) {
  mrb_value blk = MV(rb_proc_new(fn, data)), t = MV(tag);
  return V(mrb_funcall_with_block(M, kernel(), SYM("catch"), 1, &t, blk));
}
VALUE rb_catch(const char *tag, rb_block_call_func_t fn, VALUE data) { return rb_catch_obj(ID2SYM(rb_intern(tag)), fn, data); }
void rb_throw_obj(VALUE tag, VALUE val) {
  mrb_value a[2] = {MV(tag), MV(val)};
  mrb_funcall_argv(M, kernel(), SYM("throw"), 2, a);
  abort(); /* throw does not return */
}
void rb_throw(const char *tag, VALUE val) { rb_throw_obj(ID2SYM(rb_intern(tag)), val); }

int rb_get_kwargs(VALUE hash, const ID *table, int required, int optional, VALUE *values) {
  int rest = optional < 0;
  if (rest) optional = -1 - optional;
  int found = 0;
  mrb_value h = NIL_P(hash) ? mrb_nil_value() : MV(hash);
  for (int i = 0; i < required + optional; i++) {
    mrb_value key = mrb_symbol_value((mrb_sym)table[i]);
    if (!mrb_nil_p(h) && mrb_hash_key_p(M, h, key)) {
      if (values) values[i] = V(mrb_hash_delete_key(M, h, key));
      found++;
    }
    else {
      if (i < required) rb_raise(rb_eArgError, "missing keyword: :%s", mrb_sym_name(M, (mrb_sym)table[i]));
      if (values) values[i] = Qundef;
    }
  }
  if (!rest && !mrb_nil_p(h) && mrb_hash_size(M, h) > 0) {
    mrb_value keys = mrb_hash_keys(M, h);
    rb_raise(rb_eArgError, "unknown keyword%s: %" PRIsVALUE, RARRAY_LEN(keys) > 1 ? "s" : "",
             V(call1(keys, "join", mrb_str_new_cstr(M, ", "))));
  }
  return found;
}

/* ------------------------------------------------------------------ */
/* formatting: printf, with "%"PRIsVALUE ("%li\v") a VALUE's #to_s and   */
/* "%+"PRIsVALUE its #inspect                                           */

static void vformat(mrb_value out, const char *fmt, va_list ap) {
  const char *p = fmt;
  while (*p) {
    const char *pct = strchr(p, '%');
    if (!pct) { mrb_str_cat_cstr(M, out, p); break; }
    if (pct > p) mrb_str_cat(M, out, p, (size_t)(pct - p));
    const char *q = pct + 1;
    if (*q == '%') { mrb_str_cat(M, out, "%", 1); p = q + 1; continue; }
    char spec[64];
    size_t sn = 0;
    spec[sn++] = '%';
    int plus = 0;
    while (*q && strchr("-+ #0", *q)) { if (*q == '+') plus = 1; if (sn < 60) spec[sn++] = *q; q++; }
    int width = -1, prec = -1;
    if (*q == '*') { width = va_arg(ap, int); q++; spec[sn++] = '*'; }
    else while (isdigit((unsigned char)*q)) { if (sn < 60) spec[sn++] = *q; q++; }
    if (*q == '.') {
      if (sn < 60) spec[sn++] = '.';
      q++;
      if (*q == '*') { prec = va_arg(ap, int); q++; spec[sn++] = '*'; }
      else while (isdigit((unsigned char)*q)) { if (sn < 60) spec[sn++] = *q; q++; }
    }
    int lmod = 0; /* 1 h, 2 hh, 3 l, 4 ll, 5 z, 6 j, 7 t, 8 L */
    if (*q == 'h') { q++; lmod = 1; if (*q == 'h') { q++; lmod = 2; } }
    else if (*q == 'l') { q++; lmod = 3; if (*q == 'l') { q++; lmod = 4; } }
    else if (*q == 'z') { q++; lmod = 5; } else if (*q == 'j') { q++; lmod = 6; }
    else if (*q == 't') { q++; lmod = 7; } else if (*q == 'L') { q++; lmod = 8; }
    else if (*q == 'q') { q++; lmod = 4; }
    char conv = *q;
    if (!conv) break;
    q++;
    if (conv == 'i' && lmod == 3 && *q == '\v') {
      q++;
      VALUE v = va_arg(ap, VALUE);
      mrb_value s = plus ? mrb_inspect(M, MV(v)) : mrb_obj_as_string(M, MV(v));
      mrb_str_cat_str(M, out, s);
      p = q;
      continue;
    }
    static const char *lmods[] = {"", "h", "hh", "l", "ll", "z", "j", "t", "L"};
    size_t ll = strlen(lmods[lmod]);
    memcpy(spec + sn, lmods[lmod], ll);
    sn += ll;
    spec[sn++] = conv;
    spec[sn] = 0;
    char buf[512];
    int n = 0;
#define FMT(arg) do { \
      if (width >= 0 && prec >= 0) n = snprintf(buf, sizeof buf, spec, width, prec, arg); \
      else if (width >= 0) n = snprintf(buf, sizeof buf, spec, width, arg); \
      else if (prec >= 0) n = snprintf(buf, sizeof buf, spec, prec, arg); \
      else n = snprintf(buf, sizeof buf, spec, arg); } while (0)
    switch (conv) {
    case 'd': case 'i':
      if (lmod == 4) { long long a = va_arg(ap, long long); FMT(a); }
      else if (lmod >= 3) { long a = va_arg(ap, long); FMT(a); }
      else { int a = va_arg(ap, int); FMT(a); }
      break;
    case 'u': case 'x': case 'X': case 'o':
      if (lmod == 4) { unsigned long long a = va_arg(ap, unsigned long long); FMT(a); }
      else if (lmod >= 3) { unsigned long a = va_arg(ap, unsigned long); FMT(a); }
      else { unsigned int a = va_arg(ap, unsigned int); FMT(a); }
      break;
    case 'c': { int a = va_arg(ap, int); FMT(a); break; }
    case 's': {
      const char *a = va_arg(ap, const char *);
      if (!a) a = "(null)";
      if (width < 0 && prec < 0) { mrb_str_cat_cstr(M, out, a); n = -1; }
      else FMT(a);
      break;
    }
    case 'p': { void *a = va_arg(ap, void *); FMT(a); break; }
    case 'f': case 'F': case 'e': case 'E': case 'g': case 'G': case 'a': case 'A':
      if (lmod == 8) { long double a = va_arg(ap, long double); FMT(a); }
      else { double a = va_arg(ap, double); FMT(a); }
      break;
    default: n = 0; break;
    }
#undef FMT
    if (n > 0) mrb_str_cat(M, out, buf, (size_t)(n < (int)sizeof buf ? n : (int)sizeof buf - 1));
    p = q;
  }
}

static mrb_value str_binary(mrb_value s);
VALUE rb_vsprintf(const char *fmt, va_list ap) {
  mrb_value s = str_binary(mrb_str_new(M, NULL, 0)); /* ASCII-8BIT, as MRI's */
  va_list cp;
  va_copy(cp, ap);
  vformat(s, fmt, cp);
  va_end(cp);
  return V(s);
}
VALUE rb_sprintf(const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  VALUE r = rb_vsprintf(fmt, ap);
  va_end(ap);
  return r;
}
VALUE rb_str_vcatf(VALUE str, const char *fmt, va_list ap) {
  va_list cp;
  va_copy(cp, ap);
  vformat(MV(str), fmt, cp);
  va_end(cp);
  return str;
}
VALUE rb_str_catf(VALUE str, const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  rb_str_vcatf(str, fmt, ap);
  va_end(ap);
  return str;
}
VALUE rb_f_sprintf(int argc, const VALUE *argv) { return rb_funcallv(rb_mKernel, SYM("format"), argc, argv); }
VALUE rb_str_format(int argc, const VALUE *argv, VALUE fmt) {
  VALUE *av = (VALUE *)mrb_malloc(M, sizeof(VALUE) * (size_t)(argc + 1));
  av[0] = fmt;
  for (int i = 0; i < argc; i++) av[i + 1] = argv[i];
  VALUE r = rb_f_sprintf(argc + 1, av);
  mrb_free(M, av);
  return r;
}

/* ------------------------------------------------------------------ */
/* errors                                                              */

void rb_exc_raise(VALUE exc) { mrb_exc_raise(M, MV(exc)); }
static void raise_str(VALUE klass, mrb_value msg) {
  mrb_exc_raise(M, mrb_exc_new_str(M, CLS(klass), msg));
}
void rb_raise(VALUE klass, const char *fmt, ...) {
  mrb_value s = mrb_str_new(M, NULL, 0);
  va_list ap;
  va_start(ap, fmt);
  vformat(s, fmt, ap);
  va_end(ap);
  raise_str(klass, s);
  abort(); /* not reached */
}
void rb_fatal(const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  fputs("[FATAL] ", stderr);
  vfprintf(stderr, fmt, ap);
  fputc('\n', stderr);
  va_end(ap);
  abort();
}
void rb_bug(const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  fputs("[BUG] ", stderr);
  vfprintf(stderr, fmt, ap);
  fputc('\n', stderr);
  va_end(ap);
  abort();
}
void rb_exc_fatal(VALUE exc) { mrb_exc_raise(M, MV(exc)); }
void rb_sys_fail(const char *msg) { mrb_sys_fail(M, msg); abort(); }
void rb_sys_fail_str(VALUE msg) { mrb_sys_fail(M, NIL_P(msg) ? NULL : RSTRING_PTR(MV(msg))); abort(); }
void rb_notimplement(void) {
  rb_raise(rb_eNotImpError, "%s() function is unimplemented on this machine", mrb_sym_name(M, M->c->ci->mid));
}
void rb_error_arity(int argc, int min, int max) {
  if (min == max) rb_raise(rb_eArgError, "wrong number of arguments (given %d, expected %d)", argc, min);
  if (max < 0) rb_raise(rb_eArgError, "wrong number of arguments (given %d, expected %d+)", argc, min);
  rb_raise(rb_eArgError, "wrong number of arguments (given %d, expected %d..%d)", argc, min, max);
}
static VALUE errinfo = Qnil;
static int errinfo_registered;
static size_t nroots;
static void set_errinfo(mrb_value exc) {
  if (!errinfo_registered) { rb_gc_register_address(&errinfo); errinfo_registered = 1; }
  errinfo = V(exc);
}
void rb_jump_tag(int state) {
  (void)state;
  if (NIL_P(errinfo)) rb_raise(rb_eRuntimeError, "rb_jump_tag without an error");
  VALUE e = errinfo;
  errinfo = Qnil;
  rb_exc_raise(e);
}
void rb_error_frozen(const char *what) { rb_raise(rb_eFrozenError, "can't modify frozen %s", what); }
void rb_error_frozen_object(VALUE v) { mrb_frozen_error(M, mrb_ptr(MV(v))); abort(); }
void rb_frozen_error_raise(VALUE v, const char *fmt, ...) {
  (void)v;
  mrb_value s = mrb_str_new(M, NULL, 0);
  va_list ap;
  va_start(ap, fmt);
  vformat(s, fmt, ap);
  va_end(ap);
  raise_str(rb_eFrozenError, s);
  abort();
}
void rb_memerror(void) { mrb_raise(M, CLS(rb_eNoMemError), "failed to allocate memory"); abort(); }
void rb_num_zerodiv(void) { rb_raise(rb_eZeroDivError, "divided by 0"); }
void rb_cmperr(VALUE a, VALUE b) {
  rb_raise(rb_eArgError, "comparison of %s with %" PRIsVALUE " failed", rb_obj_classname(a),
           SPECIAL_CONST_P(b) ? rb_inspect(b) : rb_obj_class(b));
}
void rb_invalid_str(const char *str, const char *type) { rb_raise(rb_eArgError, "invalid value for %s: %s", type, str); }
static void warn_str(mrb_value msg) {
  mrb_value s = mrb_str_new_cstr(M, "warning: ");
  mrb_str_cat_str(M, s, msg);
  mrb_funcall_argv(M, kernel(), SYM("__capi_warn"), 1, &s);
}
void rb_warn(const char *fmt, ...) {
  mrb_value s = mrb_str_new(M, NULL, 0);
  va_list ap;
  va_start(ap, fmt);
  vformat(s, fmt, ap);
  va_end(ap);
  warn_str(s);
}
void rb_warning(const char *fmt, ...) {
  if (!mrb_test(mrb_gv_get(M, SYM("$VERBOSE")))) return;
  mrb_value s = mrb_str_new(M, NULL, 0);
  va_list ap;
  va_start(ap, fmt);
  vformat(s, fmt, ap);
  va_end(ap);
  warn_str(s);
}
VALUE rb_exc_new(VALUE k, const char *ptr, long len) { return V(mrb_exc_new(M, CLS(k), ptr, (size_t)len)); }
VALUE rb_exc_new_cstr(VALUE k, const char *s) { return V(mrb_exc_new(M, CLS(k), s, strlen(s))); }
VALUE rb_exc_new_str(VALUE k, VALUE s) { return V(mrb_exc_new_str(M, CLS(k), MV(s))); }

struct capi_call { VALUE (*fn)(VALUE); VALUE arg; };
static mrb_value protect_body(mrb_state *mrb, void *data) {
  (void)mrb;
  struct capi_call *c = (struct capi_call *)data;
  return MV(c->fn(c->arg));
}
VALUE rb_ensure(VALUE (*b)(VALUE), VALUE d1, VALUE (*e)(VALUE), VALUE d2) {
  struct capi_call body = {b, d1};
  mrb_bool failed = FALSE;
  mrb_value r = mrb_protect_error(M, protect_body, &body, &failed);
  e(d2);
  if (failed) mrb_exc_raise(M, r);
  return V(r);
}
VALUE rb_protect(VALUE (*fn)(VALUE), VALUE arg, int *state) {
  struct capi_call c = {fn, arg};
  mrb_bool failed = FALSE;
  mrb_value r = mrb_protect_error(M, protect_body, &c, &failed);
  if (failed) {
    set_errinfo(r);
    if (state) *state = 1;
    return Qnil;
  }
  if (state) *state = 0;
  return V(r);
}
static VALUE rescue(VALUE (*b)(VALUE), VALUE d1, VALUE (*r)(VALUE, VALUE), VALUE d2, int nclasses, const VALUE *classes) {
  struct capi_call c = {b, d1};
  mrb_bool failed = FALSE;
  mrb_value res = mrb_protect_error(M, protect_body, &c, &failed);
  if (!failed) return V(res);
  for (int i = 0; i < nclasses; i++) {
    if (mrb_obj_is_kind_of(M, res, CLS(classes[i]))) {
      if (!r) return Qnil;
      set_errinfo(res);
      VALUE out = r(d2, V(res));
      errinfo = Qnil;
      return out;
    }
  }
  mrb_exc_raise(M, res);
  return Qnil;
}
VALUE rb_rescue(VALUE (*b)(VALUE), VALUE d1, VALUE (*r)(VALUE, VALUE), VALUE d2) {
  return rescue(b, d1, r, d2, 1, &rb_eStandardError);
}
VALUE rb_rescue2(VALUE (*b)(VALUE), VALUE d1, VALUE (*r)(VALUE, VALUE), VALUE d2, ...) {
  VALUE classes[16];
  int n = 0;
  va_list ap;
  va_start(ap, d2);
  for (VALUE k; n < 16 && (k = va_arg(ap, VALUE)) != 0; ) classes[n++] = k;
  va_end(ap);
  return rescue(b, d1, r, d2, n, classes);
}
VALUE rb_errinfo(void) { return errinfo; }
void rb_set_errinfo(VALUE v) { set_errinfo(MV(v)); }
int rb_safe_level(void) { return 0; }

/* ------------------------------------------------------------------ */
/* Strings                                                             */

static mrb_value need_str(VALUE v) {
  mrb_value m = SPECIAL_CONST_P(v) ? mrb_nil_value() : MV(v);
  if (!mrb_string_p(m)) rb_raise(rb_eTypeError, "wrong argument type %s (expected String)", display_class(v));
  return m;
}
char *rb_capi_rstring_ptr(VALUE v) { return RSTRING_PTR(need_str(v)); }
long rb_capi_rstring_len(VALUE v) { return (long)RSTRING_LEN(need_str(v)); }
VALUE rb_string_value(volatile VALUE *v) {
  if (!SPECIAL_CONST_P(*v) && mrb_string_p(MV(*v))) return *v;
  *v = rb_convert_type(*v, T_STRING, "String", "to_str");
  return *v;
}
char *rb_string_value_ptr(volatile VALUE *v) { return RSTRING_PTR(MV(rb_string_value(v))); }
char *rb_string_value_cstr(volatile VALUE *v) {
  mrb_value s = MV(rb_string_value(v));
  const char *p = RSTRING_PTR(s);
  mrb_int n = RSTRING_LEN(s);
  if (memchr(p, 0, (size_t)n)) rb_raise(rb_eArgError, "string contains null byte");
  return (char *)mrb_string_cstr(M, s);
}
/* MRI's rb_str_new & co. make ASCII-8BIT strings (an encoding is what
   rb_enc_associate, rb_utf8_str_new, ... give one): a new string's binary
   flag is all oruby's String#encoding reads for that (no tag yet) */
static mrb_value str_binary(mrb_value s) {
  struct RString *r = mrb_str_ptr(s);
  r->flags = (r->flags & ~MRB_STR_ENCODING_MASK) | (MRB_STR_ENCODING_BINARY << MRB_STR_ENCODING_SHIFT);
  return s;
}
static mrb_value str_utf8(mrb_value s) {
  struct RString *r = mrb_str_ptr(s);
  r->flags &= ~MRB_STR_ENCODING_MASK;
  return s;
}
static mrb_value str_new(const char *p, long n) {
  if (n < 0) rb_raise(rb_eArgError, "negative string size (or size too big)");
  if (p) return str_binary(mrb_str_new(M, p, (size_t)n));
  mrb_value s = str_binary(mrb_str_new_capa(M, (size_t)n));
  memset(RSTRING_PTR(s), 0, (size_t)n);
  RSTR_SET_LEN(mrb_str_ptr(s), n);
  return s;
}
VALUE rb_str_new(const char *p, long n) { return V(str_new(p, n)); }
VALUE rb_str_new_cstr(const char *p) { return V(str_binary(mrb_str_new_cstr(M, p))); }
VALUE rb_str_new_shared(VALUE s) { return V(mrb_str_dup(M, MV(s))); }
VALUE rb_str_new_frozen(VALUE s) { mrb_value d = mrb_str_dup(M, MV(s)); mrb_obj_freeze(M, d); return V(d); }
VALUE rb_str_new_static(const char *p, long n) { return rb_str_new(p, n); }
VALUE rb_str_new_with_class(VALUE obj, const char *p, long n) {
  mrb_value s = str_new(p, n);
  mrb_str_ptr(s)->c = mrb_obj_class(M, MV(obj));
  return V(s);
}
static VALUE force_enc(VALUE s, const char *name) { call1(MV(s), "force_encoding", mrb_str_new_cstr(M, name)); return s; }
VALUE rb_usascii_str_new(const char *p, long n) { return force_enc(rb_str_new(p, n), "US-ASCII"); }
VALUE rb_usascii_str_new_cstr(const char *p) { return force_enc(rb_str_new_cstr(p), "US-ASCII"); }
VALUE rb_utf8_str_new(const char *p, long n) { return V(str_utf8(str_new(p, n))); }
VALUE rb_utf8_str_new_cstr(const char *p) { return V(mrb_str_new_cstr(M, p)); }
VALUE rb_external_str_new(const char *p, long n) { return rb_enc_associate(rb_str_new(p, n), rb_default_external_encoding()); }
VALUE rb_locale_str_new(const char *p, long n) { return rb_enc_associate(rb_str_new(p, n), rb_locale_encoding()); }
VALUE rb_str_buf_new(long capa) { return V(str_binary(mrb_str_new_capa(M, (size_t)(capa > 0 ? capa : 0)))); }
VALUE rb_str_buf_new_cstr(const char *p) { return rb_str_new_cstr(p); }
VALUE rb_str_tmp_new(long n) { return rb_str_new(NULL, n); }
VALUE rb_interned_str(const char *p, long n) { VALUE s = rb_usascii_str_new(p, n); rb_obj_freeze(s); return s; }
VALUE rb_interned_str_cstr(const char *p) { VALUE s = rb_usascii_str_new_cstr(p); rb_obj_freeze(s); return s; }
VALUE rb_fstring_cstr(const char *p) { return rb_interned_str_cstr(p); }
VALUE rb_str_cat(VALUE s, const char *p, long n) { mrb_str_cat(M, need_str(s), p, (size_t)n); return s; }
VALUE rb_str_cat_cstr(VALUE s, const char *p) { mrb_str_cat_cstr(M, need_str(s), p); return s; }
VALUE rb_str_buf_append(VALUE s, VALUE t) { mrb_str_cat_str(M, need_str(s), need_str(t)); return s; }
VALUE rb_str_append(VALUE s, VALUE t) { return rb_str_buf_append(s, t); }
VALUE rb_str_concat(VALUE s, VALUE t) { call1(MV(s), "concat", MV(t)); return s; }
VALUE rb_str_plus(VALUE s, VALUE t) { return V(mrb_str_plus(M, need_str(s), need_str(t))); }
VALUE rb_str_times(VALUE s, VALUE n) { return V(call1(MV(s), "*", MV(n))); }
VALUE rb_str_dup(VALUE s) { return V(mrb_str_dup(M, need_str(s))); }
VALUE rb_str_resurrect(VALUE s) { return rb_str_dup(s); }
VALUE rb_str_resize(VALUE s, long n) { mrb_str_resize(M, need_str(s), n); return s; }
void rb_str_set_len(VALUE s, long n) {
  mrb_value m = need_str(s);
  struct RString *r = mrb_str_ptr(m);
  mrb_str_modify(M, r);
  if (n > (long)RSTRING_CAPA(m)) rb_raise(rb_eArgError, "probable buffer overflow: %ld for %ld", n, (long)RSTRING_CAPA(m));
  RSTR_SET_LEN(r, n);
  RSTRING_PTR(m)[n] = 0;
}
void rb_str_modify(VALUE s) { mrb_str_modify(M, mrb_str_ptr(need_str(s))); }
void rb_str_modify_expand(VALUE s, long expand) {
  mrb_value m = need_str(s);
  mrb_int len = RSTRING_LEN(m);
  mrb_str_modify(M, mrb_str_ptr(m));
  if (expand > 0) { mrb_str_resize(M, m, len + expand); RSTR_SET_LEN(mrb_str_ptr(m), len); }
}
VALUE rb_str_freeze(VALUE s) { return rb_obj_freeze(s); }
VALUE rb_str_length(VALUE s) { return V(call0(MV(s), "length")); }
long rb_str_strlen(VALUE s) { return FIX2LONG(rb_str_length(s)); }
VALUE rb_str_substr(VALUE s, long beg, long len) { return V(mrb_str_substr(M, need_str(s), beg, len)); }
VALUE rb_str_subseq(VALUE s, long beg, long len) {
  mrb_value a[2] = {mrb_int_value(M, beg), mrb_int_value(M, len)};
  return V(mrb_funcall_argv(M, need_str(s), SYM("byteslice"), 2, a));
}
VALUE rb_str_equal(VALUE a, VALUE b) { return B(mrb_str_equal(M, MV(a), MV(b))); }
int rb_str_cmp(VALUE a, VALUE b) { return mrb_str_cmp(M, need_str(a), need_str(b)); }
VALUE rb_str_hash(VALUE s) { return V(call0(MV(s), "hash")); }
VALUE rb_str_inspect(VALUE s) { return V(mrb_inspect(M, MV(s))); }
VALUE rb_str_dump(VALUE s) { return V(call0(MV(s), "dump")); }
VALUE rb_str_split(VALUE s, const char *sep) { return V(call1(MV(s), "split", mrb_str_new_cstr(M, sep))); }
VALUE rb_str_to_str(VALUE s) { return rb_convert_type(s, T_STRING, "String", "to_str"); }
VALUE rb_str_export(VALUE s) { return s; }
VALUE rb_str_export_locale(VALUE s) { return s; }
long rb_str_offset(VALUE s, long pos) {
  mrb_value sub = MV(rb_str_substr(s, 0, pos));
  return mrb_string_p(sub) ? (long)RSTRING_LEN(sub) : 0;
}

/* ------------------------------------------------------------------ */
/* Arrays                                                              */

static mrb_value need_ary(VALUE v) {
  mrb_value m = SPECIAL_CONST_P(v) ? mrb_nil_value() : MV(v);
  if (!mrb_array_p(m)) rb_raise(rb_eTypeError, "wrong argument type %s (expected Array)", display_class(v));
  ptr_flush(); /* what was written through RARRAY_PTR, before the API reads it */
  return m;
}

/* RARRAY_PTR & co.: an Array's elements as VALUEs, in a buffer that is a
   mruby String, so the GC frees it after the C method that made it returns
   (the elements stay alive in the array). A snapshot is reused while
   ptr_gen is the same, the array has the same length and buffer, and no
   rb_ary_store came in between; ptr_gen moves on at the method's entry and
   return and at every call into Ruby. Writable snapshots (RARRAY_PTR) are
   written back at those points first. */
#define PTR_SLOTS 8
struct ptr_snap { struct RArray *ary; const mrb_value *aptr; mrb_int len; mrb_value buf; unsigned gen; int writable; };
static struct ptr_snap ptr_snaps[PTR_SLOTS];
static unsigned ptr_gen = 1;
static int ptr_next, ptr_dirty;

/* writable snapshots stay pending until their generation ends: the C code
   may write through the pointer again after any flush */
static void ptr_flush(void) {
  if (!ptr_dirty) return;
  for (int i = 0; i < PTR_SLOTS; i++) {
    struct ptr_snap *p = &ptr_snaps[i];
    if (!p->writable || p->gen != ptr_gen) continue;
    if (ARY_LEN(p->ary) != p->len) { p->writable = 0; continue; } /* changed meanwhile: the array wins */
    const VALUE *vals = (const VALUE *)RSTRING_PTR(p->buf);
    mrb_value a = mrb_obj_value(p->ary);
    for (mrb_int k = 0; k < p->len; k++) {
      mrb_value v = MV(vals[k]);
      const mrb_value *cur = ARY_PTR(p->ary) + k;
      if (!mrb_obj_eq(M, *cur, v) || mrb_type(*cur) != mrb_type(v)) mrb_ary_set(M, a, k, v);
    }
    p->aptr = ARY_PTR(p->ary);
  }
}
/* write back, then start a new generation of snapshots */
static void ptr_next_gen(void) {
  ptr_flush();
  ptr_dirty = 0;
  ptr_gen++;
}
void rb_capi_array_ptr_sync(void) { ptr_flush(); }

static VALUE *array_snapshot(VALUE v, int writable) {
  mrb_value m = need_ary(v);
  struct RArray *a = mrb_ary_ptr(m);
  mrb_int len = ARY_LEN(a);
  for (int i = 0; i < PTR_SLOTS; i++) {
    struct ptr_snap *p = &ptr_snaps[i];
    if (p->ary == a && p->gen == ptr_gen && p->len == len && p->aptr == ARY_PTR(a)) {
      if (writable) { p->writable = 1; ptr_dirty = 1; }
      return (VALUE *)RSTRING_PTR(p->buf);
    }
  }
  mrb_value buf = mrb_str_new_capa(M, (size_t)(len > 0 ? len : 1) * sizeof(VALUE));
  VALUE *vals = (VALUE *)RSTRING_PTR(buf);
  const mrb_value *src = ARY_PTR(a);
  for (mrb_int k = 0; k < len; k++) vals[k] = V(src[k]);
  struct ptr_snap *p = &ptr_snaps[ptr_next];
  ptr_next = (ptr_next + 1) % PTR_SLOTS;
  if (p->writable && p->gen == ptr_gen) ptr_flush(); /* the slot's pending writes first */
  *p = (struct ptr_snap){a, ARY_PTR(a), len, buf, ptr_gen, writable};
  if (writable) ptr_dirty = 1;
  return vals;
}
const VALUE *rb_array_const_ptr(VALUE v) { return array_snapshot(v, 0); }
VALUE *rb_capi_array_ptr(VALUE v) { return array_snapshot(v, 1); }
/* the length needs no flush: writes through RARRAY_PTR do not change it */
long rb_array_len(VALUE a) {
  mrb_value m = SPECIAL_CONST_P(a) ? mrb_nil_value() : MV(a);
  if (!mrb_array_p(m)) rb_raise(rb_eTypeError, "wrong argument type %s (expected Array)", display_class(a));
  return (long)RARRAY_LEN(m);
}
VALUE rb_ary_new(void) { return V(mrb_ary_new(M)); }
VALUE rb_ary_new_capa(long n) { return V(mrb_ary_new_capa(M, n)); }
VALUE rb_ary_new_from_values(long n, const VALUE *v) {
  mrb_value a = mrb_ary_new_capa(M, n);
  for (long i = 0; i < n; i++) mrb_ary_push(M, a, MV(v[i]));
  return V(a);
}
VALUE rb_ary_new_from_args(long n, ...) {
  mrb_value a = mrb_ary_new_capa(M, n);
  va_list ap;
  va_start(ap, n);
  for (long i = 0; i < n; i++) mrb_ary_push(M, a, MV(va_arg(ap, VALUE)));
  va_end(ap);
  return V(a);
}
VALUE rb_ary_tmp_new(long n) { return rb_ary_new_capa(n); }
VALUE rb_assoc_new(VALUE a, VALUE b) { return rb_ary_new_from_args(2, a, b); }
VALUE rb_ary_push(VALUE a, VALUE v) { mrb_ary_push(M, need_ary(a), MV(v)); return a; }
VALUE rb_ary_pop(VALUE a) { return V(mrb_ary_pop(M, need_ary(a))); }
VALUE rb_ary_shift(VALUE a) { return V(mrb_ary_shift(M, need_ary(a))); }
VALUE rb_ary_unshift(VALUE a, VALUE v) { mrb_ary_unshift(M, need_ary(a), MV(v)); return a; }
VALUE rb_ary_entry(VALUE a, long i) { return V(mrb_ary_entry(need_ary(a), i)); }
void rb_ary_store(VALUE a, long i, VALUE v) { mrb_ary_set(M, need_ary(a), i, MV(v)); ptr_gen++; }
VALUE rb_ary_dup(VALUE a) { return V(call0(need_ary(a), "dup")); }
VALUE rb_ary_clear(VALUE a) { mrb_ary_clear(M, need_ary(a)); return a; }
VALUE rb_ary_concat(VALUE a, VALUE b) { mrb_ary_concat(M, need_ary(a), need_ary(b)); return a; }
VALUE rb_ary_plus(VALUE a, VALUE b) { return V(call1(MV(a), "+", MV(b))); }
VALUE rb_ary_join(VALUE a, VALUE sep) { return V(mrb_ary_join(M, need_ary(a), MV(sep))); }
VALUE rb_ary_reverse(VALUE a) { call0(need_ary(a), "reverse!"); return a; }
VALUE rb_ary_includes(VALUE a, VALUE v) { return V(call1(need_ary(a), "include?", MV(v))); }
VALUE rb_ary_delete(VALUE a, VALUE v) { return V(call1(need_ary(a), "delete", MV(v))); }
VALUE rb_ary_delete_at(VALUE a, long i) { return V(call1(need_ary(a), "delete_at", mrb_int_value(M, i))); }
VALUE rb_ary_subseq(VALUE a, long beg, long len) {
  mrb_value args[2] = {mrb_int_value(M, beg), mrb_int_value(M, len)};
  return V(mrb_funcall_argv(M, need_ary(a), SYM("[]"), 2, args));
}
VALUE rb_ary_aref(int argc, const VALUE *argv, VALUE a) { return rb_funcallv(a, SYM("[]"), argc, argv); }
VALUE rb_ary_to_ary(VALUE v) { VALUE a = rb_check_array_type(v); return NIL_P(a) ? rb_ary_new_from_args(1, v) : a; }
VALUE rb_ary_to_s(VALUE a) { return V(call0(MV(a), "to_s")); }
VALUE rb_ary_cat(VALUE a, const VALUE *v, long n) { for (long i = 0; i < n; i++) rb_ary_push(a, v[i]); return a; }
VALUE rb_ary_freeze(VALUE a) { return rb_obj_freeze(a); }
VALUE rb_ary_sort_bang(VALUE a) { call0(need_ary(a), "sort!"); return a; }
VALUE rb_ary_resize(VALUE a, long n) { mrb_ary_resize(M, need_ary(a), n); return a; }
VALUE rb_ary_last(int argc, const VALUE *argv, VALUE a) { return rb_funcallv(a, SYM("last"), argc, argv); }
VALUE rb_ary_resurrect(VALUE a) { return rb_ary_dup(a); }
VALUE rb_get_values_at(VALUE obj, long olen, int argc, const VALUE *argv, VALUE (*func)(VALUE, long)) {
  VALUE r = rb_ary_new_capa(argc);
  for (int i = 0; i < argc; i++) {
    long k = NUM2LONG(argv[i]);
    if (k < 0) k += olen;
    rb_ary_push(r, func(obj, k));
  }
  return r;
}

/* ------------------------------------------------------------------ */
/* Hashes                                                              */

static mrb_value need_hash(VALUE v) {
  mrb_value m = SPECIAL_CONST_P(v) ? mrb_nil_value() : MV(v);
  if (!mrb_hash_p(m)) rb_raise(rb_eTypeError, "wrong argument type %s (expected Hash)", display_class(v));
  return m;
}
VALUE rb_hash_new(void) { return V(mrb_hash_new(M)); }
VALUE rb_hash_new_capa(long n) { return V(mrb_hash_new_capa(M, n)); }
VALUE rb_hash_aset(VALUE h, VALUE k, VALUE v) { mrb_hash_set(M, need_hash(h), MV(k), MV(v)); return v; }
VALUE rb_hash_aref(VALUE h, VALUE k) { return V(mrb_hash_get(M, need_hash(h), MV(k))); }
VALUE rb_hash_lookup2(VALUE h, VALUE k, VALUE def) { return V(mrb_hash_fetch(M, need_hash(h), MV(k), MV(def))); }
VALUE rb_hash_lookup(VALUE h, VALUE k) { return rb_hash_lookup2(h, k, Qnil); }
VALUE rb_hash_fetch(VALUE h, VALUE k) { return V(call1(need_hash(h), "fetch", MV(k))); }
VALUE rb_hash_delete(VALUE h, VALUE k) { return V(mrb_hash_delete_key(M, need_hash(h), MV(k))); }
VALUE rb_hash_delete_if(VALUE h) { return V(mrb_funcall_with_block(M, need_hash(h), SYM("delete_if"), 0, NULL, current_block())); }
VALUE rb_hash_clear(VALUE h) { mrb_hash_clear(M, need_hash(h)); return h; }
VALUE rb_hash_size(VALUE h) { return INT2FIX(mrb_hash_size(M, need_hash(h))); }
size_t rb_hash_size_num(VALUE h) { return (size_t)mrb_hash_size(M, need_hash(h)); }
VALUE rb_hash_keys(VALUE h) { return V(mrb_hash_keys(M, need_hash(h))); }
VALUE rb_hash_values(VALUE h) { return V(mrb_hash_values(M, need_hash(h))); }
VALUE rb_hash_dup(VALUE h) { return V(mrb_hash_dup(M, need_hash(h))); }
VALUE rb_hash_freeze(VALUE h) { return rb_obj_freeze(h); }
VALUE rb_hash_set_ifnone(VALUE h, VALUE v) { call1(need_hash(h), "default=", MV(v)); return h; }
VALUE rb_hash_update_by(VALUE h, VALUE other, VALUE (*fn)(VALUE, VALUE, VALUE)) {
  mrb_value o = need_hash(other), keys = mrb_hash_keys(M, o);
  mrb_value d = need_hash(h);
  for (mrb_int i = 0; i < RARRAY_LEN(keys); i++) {
    mrb_value k = RARRAY_PTR(keys)[i], v = mrb_hash_get(M, o, k);
    if (fn && mrb_hash_key_p(M, d, k)) v = MV(fn(V(k), V(mrb_hash_get(M, d, k)), V(v)));
    mrb_hash_set(M, d, k, v);
  }
  return h;
}
/* mruby's own walk of the table (no key array, no lookups); keys fn asks
   to delete are deleted after it, the walk not being safe for that */
struct hash_each { int (*fn)(VALUE, VALUE, VALUE); VALUE arg; mrb_value del; };
static int hash_each_i(mrb_state *mrb, mrb_value k, mrb_value v, void *p) {
  struct hash_each *e = (struct hash_each *)p;
  int r = e->fn(V(k), V(v), e->arg);
  if (r == ST_DELETE) {
    if (mrb_nil_p(e->del)) e->del = mrb_ary_new(mrb);
    mrb_ary_push(mrb, e->del, k);
  }
  return r == ST_STOP ? 1 : 0;
}
void rb_hash_foreach(VALUE h, int (*fn)(VALUE, VALUE, VALUE), VALUE arg) {
  mrb_value m = need_hash(h);
  struct hash_each e = {fn, arg, mrb_nil_value()};
  mrb_hash_foreach(M, mrb_hash_ptr(m), hash_each_i, &e);
  if (!mrb_nil_p(e.del)) {
    for (mrb_int i = 0; i < RARRAY_LEN(e.del); i++) mrb_hash_delete_key(M, m, RARRAY_PTR(e.del)[i]);
  }
}
void rb_hash_bulk_insert(long n, const VALUE *argv, VALUE h) {
  for (long i = 0; i + 1 < n; i += 2) rb_hash_aset(h, argv[i], argv[i + 1]);
}
VALUE rb_env_clear(void) { return V(call0(mrb_const_get(M, mrb_obj_value(M->object_class), SYM("ENV")), "clear")); }

/* ------------------------------------------------------------------ */
/* other objects                                                       */

VALUE rb_range_new(VALUE b, VALUE e, int excl) { return V(mrb_range_new(M, MV(b), MV(e), (mrb_bool)excl)); }
int rb_range_values(VALUE r, VALUE *b, VALUE *e, int *excl) {
  mrb_value m = MV(r);
  if (!mrb_range_p(m)) {
    if (!mrb_respond_to(M, m, SYM("begin")) || !mrb_respond_to(M, m, SYM("end")) ||
        !mrb_respond_to(M, m, SYM("exclude_end?"))) return 0;
  }
  if (b) *b = V(call0(m, "begin"));
  if (e) *e = V(call0(m, "end"));
  if (excl) *excl = mrb_test(call0(m, "exclude_end?"));
  return 1;
}
VALUE rb_struct_define(const char *name, ...) {
  mrb_value a = mrb_ary_new(M);
  if (name) mrb_ary_push(M, a, mrb_str_new_cstr(M, name));
  va_list ap;
  va_start(ap, name);
  for (const char *m; (m = va_arg(ap, const char *)) != NULL; ) mrb_ary_push(M, a, mrb_symbol_value(rb_intern(m)));
  va_end(ap);
  return V(mrb_funcall_argv(M, MV(rb_cStruct), SYM("new"), RARRAY_LEN(a), RARRAY_PTR(a)));
}
VALUE rb_struct_new(VALUE k, ...) {
  mrb_value members = call0(MV(k), "members");
  mrb_int n = RARRAY_LEN(members);
  mrb_value a = mrb_ary_new_capa(M, n);
  va_list ap;
  va_start(ap, k);
  for (mrb_int i = 0; i < n; i++) mrb_ary_push(M, a, MV(va_arg(ap, VALUE)));
  va_end(ap);
  return V(mrb_funcall_argv(M, MV(k), SYM("new"), n, RARRAY_PTR(a)));
}
/* MRI's Range and Data are structs too: a Range's members are begin, end
   and excl; a Data's, its class's members, read by name */
static mrb_value range_members(void) {
  mrb_value a = mrb_ary_new_capa(M, 3);
  mrb_ary_push(M, a, mrb_symbol_value(SYM("begin")));
  mrb_ary_push(M, a, mrb_symbol_value(SYM("end")));
  mrb_ary_push(M, a, mrb_symbol_value(SYM("excl")));
  return a;
}
static mrb_value struct_members_of(mrb_value s) {
  if (mrb_range_p(s)) return range_members();
  if (mrb_respond_to(M, s, SYM("members"))) return call0(s, "members");
  return call0(mrb_obj_value(mrb_obj_class(M, s)), "members");
}
VALUE rb_struct_aref(VALUE s, VALUE i) {
  mrb_value m = MV(s);
  if (mrb_type(m) == MRB_TT_STRUCT && mrb_respond_to(M, m, SYM("[]"))) return V(call1(m, "[]", MV(i)));
  long k = FIXNUM_P(i) ? FIX2LONG(i) : -1;
  if (mrb_range_p(m)) {
    if (k == 0) return V(call0(m, "begin"));
    if (k == 1) return V(call0(m, "end"));
    if (k == 2) return V(call0(m, "exclude_end?"));
    rb_raise(rb_eIndexError, "offset %ld too large for struct(size:3)", k);
  }
  mrb_value members = struct_members_of(m);
  mrb_value name = SYMBOL_P(i) ? MV(i) : (k >= 0 && k < RARRAY_LEN(members) ? RARRAY_PTR(members)[k] : mrb_nil_value());
  if (mrb_nil_p(name)) rb_raise(rb_eIndexError, "offset %ld too large for struct(size:%ld)", k, (long)RARRAY_LEN(members));
  return V(mrb_funcall_argv(M, m, mrb_symbol(name), 0, NULL));
}
VALUE rb_struct_aset(VALUE s, VALUE i, VALUE v) {
  mrb_value m = MV(s);
  if (mrb_type(m) == MRB_TT_STRUCT && !mrb_obj_is_kind_of(M, m, CLS(rb_cStruct))) {
    /* a Data rb_obj_alloc made, not yet frozen */
    if (mrb_frozen_p(mrb_basic_ptr(m))) rb_raise(rb_eFrozenError, "can't modify frozen %s", rb_obj_classname(s));
    long k = FIXNUM_P(i) ? FIX2LONG(i) : -1;
    if (k < 0 || k >= RARRAY_LEN(m)) rb_raise(rb_eIndexError, "offset %ld too large for data(size:%ld)", k, (long)RARRAY_LEN(m));
    mrb_ary_set(M, m, k, MV(v));
    return v;
  }
  if (mrb_type(m) != MRB_TT_STRUCT) rb_raise(rb_eFrozenError, "can't modify frozen %s", rb_obj_classname(s));
  mrb_value a[2] = {MV(i), MV(v)};
  mrb_funcall_argv(M, m, SYM("[]="), 2, a);
  return v;
}
VALUE rb_struct_getmember(VALUE s, ID id) { return V(call1(MV(s), "[]", mrb_symbol_value((mrb_sym)id))); }
VALUE rb_time_new(time_t sec, long usec) {
  mrb_value a[2] = {mrb_int_value(M, (mrb_int)sec), mrb_int_value(M, usec)};
  return V(mrb_funcall_argv(M, MV(rb_cTime), SYM("at"), 2, a));
}
VALUE rb_time_nano_new(time_t sec, long nsec) {
  mrb_value a[3] = {mrb_int_value(M, (mrb_int)sec), mrb_int_value(M, nsec), mrb_symbol_value(SYM("nsec"))};
  return V(mrb_funcall_argv(M, MV(rb_cTime), SYM("at"), 3, a));
}
VALUE rb_reg_new_str(VALUE s, int opts) {
  mrb_value a[2] = {MV(s), mrb_int_value(M, opts)};
  return V(mrb_funcall_argv(M, MV(rb_cRegexp), SYM("new"), 2, a));
}
VALUE rb_marshal_dump(VALUE v, VALUE port) {
  mrb_value a[2] = {MV(v), MV(port)};
  return V(mrb_funcall_argv(M, MV(named("Marshal")), SYM("dump"), NIL_P(port) ? 1 : 2, a));
}
VALUE rb_marshal_load(VALUE v) { return V(call1(MV(named("Marshal")), "load", MV(v))); }
VALUE rb_io_write(VALUE io, VALUE s) { return V(call1(MV(io), "write", MV(s))); }
VALUE rb_io_flush(VALUE io) { call0(MV(io), "flush"); return io; }

VALUE rb_capi_basic_class(VALUE v) {
  mrb_value m = MV(v);
  if (mrb_immediate_p(m)) return rb_obj_class(v);
  return (VALUE)mrb_obj_ptr(m)->c;
}
/* MRI hides an object by clearing its class; here it stays an object */
VALUE rb_obj_hide(VALUE v) { return v; }
VALUE rb_obj_reveal(VALUE v, VALUE klass) { (void)klass; return v; }

/* Bignums, through Integer's methods */
VALUE rb_big2str(VALUE v, int base) { return V(call1(MV(v), "to_s", mrb_int_value(M, base))); }
long long rb_big2ll(VALUE v) { return (long long)mrb_as_int64(M, MV(v)); }
long rb_big2long(VALUE v) { return (long)rb_big2ll(v); }
unsigned long long rb_big2ull(VALUE v) {
  mrb_value m = MV(v);
  if (mrb_integer_p(m)) return (unsigned long long)mrb_integer(m);
  mrb_value hex = call1(m, "to_s", mrb_int_value(M, 16));
  const char *p = RSTRING_PTR(hex);
  int neg = *p == '-';
  if (neg) p++;
  if (strlen(p) > 16) rb_raise(rb_eRangeError, "bignum too big to convert into 'unsigned long long'");
  unsigned long long u = strtoull(p, NULL, 16);
  if (neg) {
    if (u > (unsigned long long)LLONG_MAX + 1) rb_raise(rb_eRangeError, "bignum out of range of unsigned long long");
    u = (unsigned long long)(-(long long)u);
  }
  return u;
}
unsigned long rb_big2ulong(VALUE v) { return (unsigned long)rb_big2ull(v); }
double rb_big2dbl(VALUE v) { return mrb_float(call0(MV(v), "to_f")); }
int rb_big_sign(VALUE v) { return mrb_test(call1(MV(v), ">=", mrb_int_value(M, 0))) ? 1 : 0; }
/* bytes |v| takes; *nlz the zero bits above its highest one in them */
size_t rb_absint_size(VALUE v, int *nlz) {
  mrb_int bits = mrb_integer(call0(call0(MV(v), "abs"), "bit_length"));
  size_t n = (size_t)(bits + 7) / 8;
  if (nlz) *nlz = (int)(n * 8 - (size_t)bits);
  return n;
}

VALUE rb_struct_size(VALUE s) { return LONG2NUM((long)RARRAY_LEN(struct_members_of(MV(s)))); }
VALUE rb_struct_members(VALUE s) { return V(struct_members_of(MV(s))); }
VALUE rb_struct_s_members(VALUE k) {
  if (mrb_obj_is_kind_of(M, MV(k), M->class_class) && mrb_test(call1(MV(k), "<=", MV(rb_cRange)))) return V(range_members());
  return V(call0(MV(k), "members"));
}

int rb_obj_method_arity(VALUE obj, ID mid) {
  return (int)mrb_integer(call0(call1(MV(obj), "method", mrb_symbol_value((mrb_sym)mid)), "arity"));
}
int rb_mod_method_arity(VALUE mod, ID mid) {
  return (int)mrb_integer(call0(call1(MV(mod), "instance_method", mrb_symbol_value((mrb_sym)mid)), "arity"));
}
int rb_proc_arity(VALUE p) { return (int)mrb_integer(call0(MV(p), "arity")); }

struct ivar_each { int (*fn)(ID, VALUE, st_data_t); st_data_t arg; };
static int ivar_each_i(mrb_state *mrb, mrb_sym sym, mrb_value val, void *p) {
  (void)mrb;
  struct ivar_each *e = (struct ivar_each *)p;
  return e->fn((ID)sym, V(val), e->arg) == ST_STOP ? 1 : 0;
}
void rb_ivar_foreach(VALUE obj, int (*fn)(ID, VALUE, st_data_t), st_data_t arg) {
  if (SPECIAL_CONST_P(obj)) return;
  struct ivar_each e = {fn, arg};
  mrb_iv_foreach(M, MV(obj), ivar_each_i, &e);
}
st_index_t rb_ivar_count(VALUE obj) {
  if (SPECIAL_CONST_P(obj)) return 0;
  return (st_index_t)RARRAY_LEN(call0(MV(obj), "instance_variables"));
}
st_index_t rb_memhash(const void *p, long n) { return st_hash(p, (size_t)n, 0); }

/* a Time, or seconds as an Integer or Float */
struct timespec rb_time_timespec(VALUE t) {
  struct timespec ts;
  mrb_value m = MV(t);
  if (mrb_integer_p(m)) {
    ts.tv_sec = (time_t)mrb_integer(m);
    ts.tv_nsec = 0;
  } else if (mrb_float_p(m)) {
    double d = mrb_float(m), sec = floor(d);
    ts.tv_sec = (time_t)sec;
    ts.tv_nsec = (long)((d - sec) * 1e9);
  } else {
    ts.tv_sec = (time_t)mrb_integer(call0(m, "to_i"));
    ts.tv_nsec = (long)mrb_integer(call0(m, "nsec"));
  }
  return ts;
}
struct timeval rb_time_timeval(VALUE t) {
  struct timespec ts = rb_time_timespec(t);
  struct timeval tv = {ts.tv_sec, (int)(ts.tv_nsec / 1000)};
  return tv;
}
/* offset: seconds east of UTC, INT_MAX local time, INT_MAX-1 UTC */
VALUE rb_time_timespec_new(const struct timespec *ts, int offset) {
  VALUE t = rb_time_nano_new(ts->tv_sec, ts->tv_nsec);
  if (offset == INT_MAX) return t;
  if (offset == INT_MAX - 1) return V(call0(MV(t), "utc"));
  return V(call1(MV(t), "localtime", mrb_int_value(M, offset)));
}
VALUE rb_rational_new(VALUE n, VALUE d) {
  mrb_value a[2] = {MV(n), MV(d)};
  return V(mrb_funcall_argv(M, kernel(), SYM("Rational"), 2, a));
}
VALUE rb_complex_new(VALUE r, VALUE i) {
  mrb_value a[2] = {MV(r), MV(i)};
  return V(mrb_funcall_argv(M, kernel(), SYM("Complex"), 2, a));
}
VALUE rb_str_replace(VALUE s, VALUE src) { call1(MV(s), "replace", MV(src)); return s; }
void rb_str_update(VALUE s, long beg, long len, VALUE v) {
  mrb_value a[3] = {mrb_int_value(M, beg), mrb_int_value(M, len), MV(v)};
  mrb_funcall_argv(M, MV(s), SYM("[]="), 3, a);
}
VALUE rb_io_puts(int argc, const VALUE *argv, VALUE out) { return rb_funcallv(out, SYM("puts"), argc, argv); }
VALUE rb_p(VALUE v) { return V(call1(kernel(), "p", MV(v))); }
void rb_ext_ractor_safe(bool safe) { (void)safe; }

/* ------------------------------------------------------------------ */
/* GC                                                                  */

static VALUE **roots;
static size_t roots_capa;
static pthread_mutex_t roots_lock = PTHREAD_MUTEX_INITIALIZER;

static void mark_value(mrb_state *mrb, VALUE v) {
  if (SPECIAL_CONST_P(v)) return;
  mrb_gc_mark(mrb, (struct RBasic *)v);
}
static void root_hook(mrb_state *mrb) {
  if (mrb != M) return;
  for (size_t i = 0; i < nroots; i++) mark_value(mrb, *roots[i]);
}
void rb_gc_register_address(VALUE *addr) {
  pthread_mutex_lock(&roots_lock);
  if (nroots == roots_capa) {
    roots_capa = roots_capa ? roots_capa * 2 : 64;
    roots = (VALUE **)realloc(roots, sizeof(VALUE *) * roots_capa);
  }
  roots[nroots++] = addr;
  pthread_mutex_unlock(&roots_lock);
}
void rb_global_variable(VALUE *addr) { rb_gc_register_address(addr); }
void rb_gc_unregister_address(VALUE *addr) {
  pthread_mutex_lock(&roots_lock);
  for (size_t i = 0; i < nroots; i++) {
    if (roots[i] == addr) { roots[i] = roots[--nroots]; break; }
  }
  pthread_mutex_unlock(&roots_lock);
}
void rb_gc_register_mark_object(VALUE v) { if (!SPECIAL_CONST_P(v)) mrb_gc_register(M, MV(v)); }
void rb_gc_mark(VALUE v) { mark_value(M, v); }
void rb_gc_mark_maybe(VALUE v) { mark_value(M, v); }
void rb_gc_mark_movable(VALUE v) { mark_value(M, v); }
void rb_gc_mark_locations(const VALUE *s, const VALUE *e) { for (; s < e; s++) mark_value(M, *s); }
VALUE rb_gc_location(VALUE v) { return v; }
void rb_gc_writebarrier(VALUE a, VALUE b) {
  if (SPECIAL_CONST_P(a) || SPECIAL_CONST_P(b)) return;
  mrb_field_write_barrier(M, (struct RBasic *)a, (struct RBasic *)b);
}
void rb_gc_writebarrier_unprotect(VALUE v) { (void)v; }
void rb_gc(void) { mrb_full_gc(M); }
VALUE rb_gc_start(void) { mrb_full_gc(M); return Qnil; }
VALUE rb_gc_enable(void) { return V(call0(MV(rb_mGC), "enable")); }
VALUE rb_gc_disable(void) { return V(call0(MV(rb_mGC), "disable")); }
void rb_gc_adjust_memory_usage(ssize_t d) { (void)d; }

/* ------------------------------------------------------------------ */
/* Data and TypedData                                                  */

/* An extension's Data object is an RData whose data is this, so the type
   and the mark and free functions go with the pointer. */
struct capi_data {
  void *ptr;  /* first: DATA_PTR is a reference to it */
  const rb_data_type_t *type;
  RUBY_DATA_FUNC dmark, dfree;
};
static void capi_data_free(mrb_state *mrb, void *p) {
  (void)mrb;
  struct capi_data *d = (struct capi_data *)p;
  if (!d) return;
  RUBY_DATA_FUNC f = d->type ? d->type->function.dfree : d->dfree;
  if (d->ptr) {
    if (f == RUBY_DEFAULT_FREE) free(d->ptr);
    else if (f) f(d->ptr);
  }
  free(d);
}
static const mrb_data_type capi_data_type = {"oruby_capi_data", capi_data_free};

static size_t cdata_mark_hook(mrb_state *mrb, struct RData *rd) {
  if (mrb != M || rd->type != &capi_data_type || !rd->data) return 0;
  struct capi_data *d = (struct capi_data *)rd->data;
  RUBY_DATA_FUNC mark = d->type ? d->type->function.dmark : d->dmark;
  if (mark && d->ptr && mark != RUBY_DEFAULT_FREE) mark(d->ptr);
  return 1;
}

/* a class whose instances are Data objects: mruby allocates by its
   instance type */
static void cdata_class(struct RClass *c) {
  if (c != M->object_class && MRB_INSTANCE_TT(c) != MRB_TT_CDATA) MRB_SET_INSTANCE_TT(c, MRB_TT_CDATA);
}

static VALUE data_new(VALUE klass, void *ptr, const rb_data_type_t *type, RUBY_DATA_FUNC mark, RUBY_DATA_FUNC dfree) {
  struct RClass *c = (klass == 0 || NIL_P(klass)) ? M->object_class : CLS(klass);
  cdata_class(c);
  struct capi_data *d = (struct capi_data *)calloc(1, sizeof *d);
  if (!d) rb_memerror();
  d->ptr = ptr;
  d->type = type;
  d->dmark = mark;
  d->dfree = dfree;
  struct RData *rd = mrb_data_object_alloc(M, c, d, &capi_data_type);
  return (VALUE)rd;
}
VALUE rb_data_typed_object_wrap(VALUE klass, void *ptr, const rb_data_type_t *type) { return data_new(klass, ptr, type, NULL, NULL); }
VALUE rb_data_typed_object_zalloc(VALUE klass, size_t size, const rb_data_type_t *type) {
  void *p = calloc(1, size ? size : 1);
  if (!p) rb_memerror();
  return data_new(klass, p, type, NULL, NULL);
}
VALUE rb_data_object_wrap(VALUE klass, void *ptr, RUBY_DATA_FUNC mark, RUBY_DATA_FUNC dfree) { return data_new(klass, ptr, NULL, mark, dfree); }
VALUE rb_data_object_zalloc(VALUE klass, size_t size, RUBY_DATA_FUNC mark, RUBY_DATA_FUNC dfree) {
  void *p = calloc(1, size ? size : 1);
  if (!p) rb_memerror();
  return data_new(klass, p, NULL, mark, dfree);
}
static struct capi_data *capi_data_of(VALUE v) {
  if (SPECIAL_CONST_P(v)) return NULL;
  struct RBasic *b = (struct RBasic *)v;
  if (b->tt != MRB_TT_CDATA || ((struct RData *)b)->type != &capi_data_type) return NULL;
  return (struct capi_data *)((struct RData *)b)->data;
}
int rb_typeddata_inherited_p(const rb_data_type_t *child, const rb_data_type_t *parent) {
  for (; child; child = child->parent) if (child == parent) return 1;
  return 0;
}
int rb_typeddata_is_kind_of(VALUE v, const rb_data_type_t *type) {
  struct capi_data *d = capi_data_of(v);
  return d && d->type && rb_typeddata_inherited_p(d->type, type);
}
void *rb_check_typeddata(VALUE v, const rb_data_type_t *type) {
  struct capi_data *d = capi_data_of(v);
  if (!d || !d->type || !rb_typeddata_inherited_p(d->type, type)) {
    rb_raise(rb_eTypeError, "wrong argument type %s (expected %s)", display_class(v), type->wrap_struct_name);
  }
  return d->ptr;
}
void *rb_capi_data_get(VALUE v) {
  struct capi_data *d = capi_data_of(v);
  if (!d) rb_raise(rb_eTypeError, "wrong argument type %s (expected Data)", display_class(v));
  return d->ptr;
}
void **rb_capi_data_ptr_ref(VALUE v) {
  struct capi_data *d = capi_data_of(v);
  if (!d) rb_raise(rb_eTypeError, "wrong argument type %s (expected Data)", display_class(v));
  return &d->ptr;
}
const rb_data_type_t *rb_capi_typeddata_type(VALUE v) { struct capi_data *d = capi_data_of(v); return d ? d->type : NULL; }
int rb_capi_typeddata_p(VALUE v) { struct capi_data *d = capi_data_of(v); return d && d->type; }

/* ------------------------------------------------------------------ */
/* memory                                                              */

void *ruby_xmalloc(size_t n) { void *p = malloc(n ? n : 1); if (!p) rb_memerror(); return p; }
void *ruby_xmalloc2(size_t n, size_t size) {
  if (size && n > SIZE_MAX / size) rb_raise(rb_eArgError, "malloc: possible integer overflow");
  return ruby_xmalloc(n * size);
}
void *ruby_xcalloc(size_t n, size_t size) { void *p = calloc(n ? n : 1, size ? size : 1); if (!p) rb_memerror(); return p; }
void *ruby_xrealloc(void *p, size_t n) { void *q = realloc(p, n ? n : 1); if (!q) rb_memerror(); return q; }
void *ruby_xrealloc2(void *p, size_t n, size_t size) {
  if (size && n > SIZE_MAX / size) rb_raise(rb_eArgError, "realloc: possible integer overflow");
  return ruby_xrealloc(p, n * size);
}
void ruby_xfree(void *p) { free(p); }

/* ------------------------------------------------------------------ */
/* threads                                                             */

void *rb_thread_call_without_gvl(void *(*fn)(void *), void *d, rb_unblock_function_t *ubf, void *d2) { (void)ubf; (void)d2; return fn(d); }
void *rb_thread_call_without_gvl2(void *(*fn)(void *), void *d, rb_unblock_function_t *ubf, void *d2) { (void)ubf; (void)d2; return fn(d); }
void *rb_thread_call_with_gvl(void *(*fn)(void *), void *d) { return fn(d); }
void *rb_nogvl(void *(*fn)(void *), void *d, rb_unblock_function_t *ubf, void *d2, int flags) { (void)ubf; (void)d2; (void)flags; return fn(d); }
VALUE rb_thread_current(void) { return V(call0(MV(rb_cThread), "current")); }
void rb_thread_schedule(void) { call0(MV(rb_cThread), "pass"); }
void rb_thread_check_ints(void) {}
int rb_thread_interrupted(VALUE th) { (void)th; return 0; }

/* ------------------------------------------------------------------ */
/* encodings                                                           */

/* Encoding, looked up where it is used (a state may define it after the
   C API is bound) */
static mrb_value encoding_class(void) {
  mrb_value e = mrb_const_get(M, mrb_obj_value(M->object_class), SYM("Encoding"));
  rb_cEncoding = V(e);
  return e;
}

#define MAX_ENCODINGS 256
static rb_encoding encodings[MAX_ENCODINGS];
static int nencodings;

/* rb_enc_mbmaxlen / rb_enc_mbminlen: MRI's values, 1 for the rest */
static const struct { const char *name; int max, min; } enc_len_table[] = {
  {"UTF-8", 4, 1}, {"UTF8-MAC", 4, 1}, {"UTF8-DoCoMo", 4, 1}, {"UTF8-KDDI", 4, 1},
  {"UTF8-SoftBank", 4, 1}, {"CESU-8", 6, 1}, {"UTF-16BE", 4, 2}, {"UTF-16LE", 4, 2},
  {"UTF-32BE", 4, 4}, {"UTF-32LE", 4, 4}, {"EUC-JP", 3, 1}, {"eucJP-ms", 3, 1},
  {"CP51932", 3, 1}, {"EUC-JIS-2004", 3, 1}, {"EUC-TW", 4, 1}, {"Emacs-Mule", 4, 1},
  {"GB18030", 4, 1}, {"stateless-ISO-2022-JP", 4, 1}, {"stateless-ISO-2022-JP-KDDI", 4, 1},
  {"Big5", 2, 1}, {"Big5-HKSCS", 2, 1}, {"Big5-UAO", 2, 1}, {"CP949", 2, 1},
  {"CP950", 2, 1}, {"CP951", 2, 1}, {"EUC-KR", 2, 1}, {"GB12345", 2, 1},
  {"GB2312", 2, 1}, {"GBK", 2, 1}, {"MacJapanese", 2, 1}, {"SJIS-DoCoMo", 2, 1},
  {"SJIS-KDDI", 2, 1}, {"SJIS-SoftBank", 2, 1}, {"Shift_JIS", 2, 1}, {"Windows-31J", 2, 1},
};
static void enc_lengths(rb_encoding *e) {
  e->max_enc_len = e->min_enc_len = 1;
  for (size_t i = 0; i < sizeof(enc_len_table) / sizeof(enc_len_table[0]); i++) {
    if (strcmp(enc_len_table[i].name, e->name) == 0) {
      e->max_enc_len = enc_len_table[i].max;
      e->min_enc_len = enc_len_table[i].min;
      return;
    }
  }
}

/* st_hash: FNV-1a over the bytes, mixed (murmur3's finalizer) */
st_index_t st_hash_start(st_index_t h) { return h ^ (st_index_t)0xcbf29ce484222325ULL; }
st_index_t st_hash_uint(st_index_t h, st_index_t i) {
  for (int k = 0; k < (int)sizeof(i); k++) { h ^= (i >> (k * 8)) & 0xff; h *= (st_index_t)0x100000001b3ULL; }
  return h;
}
st_index_t st_hash_uint32(st_index_t h, uint32_t i) { return st_hash_uint(h, i); }
st_index_t st_hash_end(st_index_t h) {
  uint64_t x = h;
  x ^= x >> 33; x *= 0xff51afd7ed558ccdULL; x ^= x >> 33; x *= 0xc4ceb9fe1a85ec53ULL; x ^= x >> 33;
  return (st_index_t)x;
}
st_index_t st_hash(const void *ptr, size_t len, st_index_t h) {
  const unsigned char *p = (const unsigned char *)ptr;
  h = st_hash_start(h);
  for (size_t i = 0; i < len; i++) { h ^= p[i]; h *= (st_index_t)0x100000001b3ULL; }
  return st_hash_end(h ^ (st_index_t)len);
}

static rb_encoding *enc_named(const char *name) {
  for (int i = 0; i < nencodings; i++) {
    if (strcmp(encodings[i].name, name) == 0) return &encodings[i];
  }
  mrb_value enc = call1(encoding_class(), "find", mrb_str_new_cstr(M, name));
  mrb_value canon = call0(enc, "name");
  const char *cn = mrb_string_cstr(M, canon);
  for (int i = 0; i < nencodings; i++) {
    if (strcmp(encodings[i].name, cn) == 0) return &encodings[i];
  }
  if (nencodings == MAX_ENCODINGS) rb_raise(rb_eRuntimeError, "too many encodings");
  rb_encoding *e = &encodings[nencodings];
  e->name = strdup(cn);
  e->index = nencodings++;
  e->ascii_compatible = mrb_test(call0(enc, "ascii_compatible?"));
  e->dummy = mrb_test(call0(enc, "dummy?"));
  enc_lengths(e);
  return e;
}
static rb_encoding *enc_of_encoding(mrb_value enc) { return enc_named(mrb_string_cstr(M, call0(enc, "name"))); }
rb_encoding *rb_utf8_encoding(void) { static rb_encoding *e; if (!e) e = enc_named("UTF-8"); return e; }
rb_encoding *rb_ascii8bit_encoding(void) { static rb_encoding *e; if (!e) e = enc_named("ASCII-8BIT"); return e; }
rb_encoding *rb_usascii_encoding(void) { static rb_encoding *e; if (!e) e = enc_named("US-ASCII"); return e; }
rb_encoding *rb_locale_encoding(void) { return rb_default_external_encoding(); }
rb_encoding *rb_filesystem_encoding(void) { return rb_default_external_encoding(); }
rb_encoding *rb_default_external_encoding(void) { return enc_of_encoding(call0(encoding_class(), "default_external")); }
rb_encoding *rb_default_internal_encoding(void) {
  mrb_value e = call0(encoding_class(), "default_internal");
  return mrb_nil_p(e) ? NULL : enc_of_encoding(e);
}
int rb_utf8_encindex(void) { return rb_utf8_encoding()->index; }
int rb_ascii8bit_encindex(void) { return rb_ascii8bit_encoding()->index; }
int rb_usascii_encindex(void) { return rb_usascii_encoding()->index; }
rb_encoding *rb_enc_get(VALUE v) {
  if (SPECIAL_CONST_P(v) && !STATIC_SYM_P(v)) return NULL;
  mrb_value m = MV(v);
  if (mrb_obj_is_kind_of(M, m, mrb_class_ptr(encoding_class()))) return enc_of_encoding(m);
  if (mrb_string_p(m) && !RSTR_BINARY_P(mrb_str_ptr(m)) && !mrb_iv_defined(M, m, SYM("__encoding"))) return rb_utf8_encoding();
  if (!mrb_respond_to(M, m, SYM("encoding"))) return NULL;
  return enc_of_encoding(call0(m, "encoding"));
}
int rb_enc_get_index(VALUE v) { rb_encoding *e = rb_enc_get(v); return e ? e->index : -1; }
rb_encoding *rb_enc_from_index(int i) { return i >= 0 && i < nencodings ? &encodings[i] : NULL; }
int rb_enc_to_index(rb_encoding *e) { return e ? e->index : 0; }
rb_encoding *rb_enc_find(const char *name) {
  for (int i = 0; i < nencodings; i++) if (strcasecmp(encodings[i].name, name) == 0) return &encodings[i];
  mrb_value enc = call1(encoding_class(), "__capi_find", mrb_str_new_cstr(M, name));
  return mrb_nil_p(enc) ? NULL : enc_of_encoding(enc);
}
int rb_define_dummy_encoding(const char *name) {
  return enc_of_encoding(call1(encoding_class(), "__capi_define_dummy", mrb_str_new_cstr(M, name)))->index;
}
int rb_enc_find_index(const char *name) { rb_encoding *e = rb_enc_find(name); return e ? e->index : -1; }
rb_encoding *rb_to_encoding(VALUE v) {
  mrb_value m = MV(v);
  if (mrb_string_p(m)) {
    rb_encoding *e = rb_enc_find(mrb_string_cstr(M, m));
    if (!e) rb_raise(rb_eArgError, "unknown encoding name - %s", mrb_string_cstr(M, m));
    return e;
  }
  return enc_of_encoding(m);
}
VALUE rb_enc_from_encoding(rb_encoding *e) {
  if (!e) return Qnil;
  return V(call1(encoding_class(), "find", mrb_str_new_cstr(M, e->name)));
}
VALUE rb_enc_associate(VALUE s, rb_encoding *e) { return e ? force_enc(s, e->name) : s; }
VALUE rb_enc_associate_index(VALUE s, int i) { return rb_enc_associate(s, rb_enc_from_index(i)); }
void rb_enc_copy(VALUE dst, VALUE src) { rb_enc_associate(dst, rb_enc_get(src)); }
VALUE rb_enc_str_new(const char *p, long n, rb_encoding *e) {
  mrb_value s = str_new(p, n);
  if (!e || e == rb_ascii8bit_encoding()) return V(s);
  if (e == rb_utf8_encoding()) return V(str_utf8(s));
  return force_enc(V(s), e->name);
}
VALUE rb_enc_str_new_cstr(const char *p, rb_encoding *e) { return rb_enc_str_new(p, (long)strlen(p), e); }
VALUE rb_enc_str_new_static(const char *p, long n, rb_encoding *e) { return rb_enc_str_new(p, n, e); }
VALUE rb_enc_interned_str(const char *p, long n, rb_encoding *e) { VALUE s = rb_enc_str_new(p, n, e); rb_obj_freeze(s); return s; }
VALUE rb_external_str_new_with_enc(const char *p, long n, rb_encoding *e) { return rb_enc_str_new(p, n, e); }
/* MRI's: the string itself where it cannot be converted */
struct conv_enc { mrb_value s; mrb_value a[2]; };
static mrb_value conv_enc_body(mrb_state *mrb, void *data) {
  struct conv_enc *c = (struct conv_enc *)data;
  return mrb_funcall_argv(mrb, c->s, mrb_intern_lit(mrb, "encode"), 2, c->a);
}
VALUE rb_str_conv_enc(VALUE s, rb_encoding *from, rb_encoding *to) {
  if (!to || from == to) return s;
  struct conv_enc c = {MV(s), {mrb_str_new_cstr(M, to->name), mrb_str_new_cstr(M, from ? from->name : "UTF-8")}};
  mrb_bool failed = FALSE;
  mrb_value r = mrb_protect_error(M, conv_enc_body, &c, &failed);
  return failed ? s : V(r);
}
VALUE rb_str_export_to_enc(VALUE s, rb_encoding *e) { return V(call1(MV(s), "encode", mrb_str_new_cstr(M, e->name))); }
VALUE rb_str_encode(VALUE s, VALUE to, int ecflags, VALUE ecopts) {
  (void)ecflags;
  if (NIL_P(ecopts)) return V(call1(MV(s), "encode", MV(to)));
  VALUE args[2] = {to, ecopts};
  return rb_funcallv_kw(s, SYM("encode"), 2, args, 1);
}
int rb_enc_str_asciionly_p(VALUE s) { return mrb_test(call0(MV(s), "ascii_only?")); }
int rb_enc_str_coderange(VALUE s) {
  if (rb_enc_str_asciionly_p(s)) return ENC_CODERANGE_7BIT;
  return mrb_test(call0(MV(s), "valid_encoding?")) ? ENC_CODERANGE_VALID : ENC_CODERANGE_BROKEN;
}
VALUE rb_enc_sprintf(rb_encoding *e, const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  VALUE s = rb_vsprintf(fmt, ap);
  va_end(ap);
  return rb_enc_associate(s, e);
}
static int ascii_bytes(const char *p, long n) {
  for (long i = 0; i < n; i++) if ((unsigned char)p[i] >= 0x80) return 0;
  return 1;
}
ID rb_intern3(const char *p, long n, rb_encoding *e) {
  if (!e || e == rb_utf8_encoding() || e == rb_usascii_encoding() || (e->ascii_compatible && !e->dummy && ascii_bytes(p, n))) {
    return mrb_intern(M, p, (size_t)n);
  }
  return oruby_intern_enc(M, p, (size_t)n, oruby_sym_enc_slot(e->name));
}
VALUE rb_enc_reg_new(const char *p, long n, rb_encoding *e, int opts) { return rb_reg_new_str(rb_enc_str_new(p, n, e), opts); }
rb_encoding *rb_enc_compatible(VALUE a, VALUE b) {
  mrb_value args[2] = {MV(a), MV(b)};
  mrb_value e = mrb_funcall_argv(M, encoding_class(), SYM("compatible?"), 2, args);
  return mrb_nil_p(e) ? NULL : enc_of_encoding(e);
}
rb_encoding *rb_enc_check(VALUE a, VALUE b) {
  rb_encoding *e = rb_enc_compatible(a, b);
  if (!e) rb_raise(rb_eEncCompatError, "incompatible character encodings: %s and %s", rb_enc_get(a)->name, rb_enc_get(b)->name);
  return e;
}
static int utf8_len(unsigned char c) { return c < 0x80 ? 1 : c < 0xc2 ? 1 : c < 0xe0 ? 2 : c < 0xf0 ? 3 : c < 0xf5 ? 4 : 1; }
long rb_enc_strlen(const char *p, const char *e, rb_encoding *enc) {
  return (long)mrb_integer(call0(MV(rb_enc_str_new(p, (long)(e - p), enc)), "length"));
}
int rb_enc_mbclen(const char *p, const char *e, rb_encoding *enc) {
  if (p >= e) return 1;
  if (!enc || enc == rb_utf8_encoding()) { int n = utf8_len((unsigned char)*p); return n <= e - p ? n : (int)(e - p); }
  if (enc == rb_ascii8bit_encoding() || enc == rb_usascii_encoding()) return 1;
  long n = e - p < 8 ? (long)(e - p) : 8;
  mrb_value s = MV(rb_enc_str_new(p, n, enc));
  mrb_value c = call1(s, "[]", mrb_int_value(M, 0));
  return mrb_string_p(c) && RSTRING_LEN(c) > 0 ? (int)RSTRING_LEN(c) : 1;
}
int rb_enc_codelen(int code, rb_encoding *e) {
  mrb_value s = call1(mrb_int_value(M, code), "chr", MV(rb_enc_from_encoding(e)));
  return (int)RSTRING_LEN(s);
}
unsigned int rb_enc_codepoint_len(const char *p, const char *e, int *len, rb_encoding *enc) {
  if (p >= e) rb_raise(rb_eArgError, "empty string");
  /* UTF-8 and single-byte encodings decoded here: extensions walk strings
     a character at a time with this (fast_blank, json) */
  if (enc && enc->max_enc_len == 1) {
    if (len) *len = 1;
    return (unsigned char)*p;
  }
  if (!enc || enc == rb_utf8_encoding()) {
    const unsigned char *u = (const unsigned char *)p;
    int n = utf8_len(u[0]);
    unsigned int c = n == 1 ? u[0] : n == 2 ? (u[0] & 0x1f) : n == 3 ? (u[0] & 0x0f) : (u[0] & 0x07);
    int ok = n <= e - p && (u[0] < 0x80 || n > 1);
    for (int i = 1; ok && i < n; i++) {
      if ((u[i] & 0xc0) != 0x80) ok = 0;
      c = (c << 6) | (u[i] & 0x3f);
    }
    if (ok && n > 1 && (c < (n == 2 ? 0x80u : n == 3 ? 0x800u : 0x10000u) || (c >= 0xd800 && c <= 0xdfff) || c > 0x10ffff)) ok = 0;
    if (!ok) rb_raise(rb_eArgError, "invalid byte sequence in %s", enc ? enc->name : "UTF-8");
    if (len) *len = n;
    return c;
  }
  int n = rb_enc_mbclen(p, e, enc);
  mrb_value s = MV(rb_enc_str_new(p, n, enc));
  if (len) *len = n;
  return (unsigned int)mrb_integer(call0(s, "ord"));
}
int rb_enc_dummy_p(rb_encoding *e) { return e && e->dummy; }

/* ------------------------------------------------------------------ */
/* ruby/vm.h                                                           */

#include "ruby/vm.h"
#define MAX_VM_AT_EXIT 32
static void (*vm_at_exit[MAX_VM_AT_EXIT])(ruby_vm_t *);
static int nvm_at_exit;
static void run_vm_at_exit(mrb_state *mrb) {
  (void)mrb;
  for (int i = nvm_at_exit - 1; i >= 0; i--) vm_at_exit[i](NULL);
  nvm_at_exit = 0;
}
void ruby_vm_at_exit(void (*func)(ruby_vm_t *)) {
  if (nvm_at_exit == 0) mrb_state_atexit(M, run_vm_at_exit);
  if (nvm_at_exit < MAX_VM_AT_EXIT) vm_at_exit[nvm_at_exit++] = func;
}
int ruby_vm_destruct(ruby_vm_t *vm) { (void)vm; return 0; }

/* The state is closing: what the C API holds of it goes, so that another
   state can bind it (an extension's Init runs again there). */
static void capi_unbind(mrb_state *mrb) {
  if (mrb != M) return;
  pthread_mutex_lock(&roots_lock);
  nroots = 0;
  pthread_mutex_unlock(&roots_lock);
  errinfo = Qnil;
  errinfo_registered = 0;
  oruby_gc_root_hook = NULL;
  oruby_gc_cdata_mark_hook = NULL;
  M = NULL;
}

/* ------------------------------------------------------------------ */
/* loading an extension library (capi.go LoadLibrary)                   */

/* Opens the library at path (its symbols global, as MRI's dln has them)
   and finds init in it; NULL with the message otherwise. The C API it
   calls is the running oruby's, which exports it. */
void *orb_capi_dlopen(const char *path, const char *init, char *msg, size_t msglen) {
  /* every symbol bound now: a function of the API oruby lacks is a
     LoadError here, not a crash where it is first called */
  void *h = dlopen(path, RTLD_NOW | RTLD_GLOBAL);
  if (!h) {
    snprintf(msg, msglen, "%s", dlerror());
    return NULL;
  }
  if (!dlsym(h, "oruby_capi_abi_1")) {
    snprintf(msg, msglen, "%s: not built against oruby's C API headers (an extension built for MRI?): rebuild it with oruby", path);
    dlclose(h);
    return NULL;
  }
  void *fn = dlsym(h, init);
  if (!fn) {
    snprintf(msg, msglen, "%s - %s", dlerror(), path);
    dlclose(h);
    return NULL;
  }
  return fn;
}

/* ------------------------------------------------------------------ */
/* Quick-win C-API additions                                           */

VALUE rb_class_get_superclass(VALUE klass) {
  VALUE sup = rb_class_superclass(klass);
  return NIL_P(sup) ? Qfalse : sup;
}

int rb_is_const_id(ID id) {
  const char *s = rb_id2name(id);
  return s && s[0] >= 'A' && s[0] <= 'Z';
}

int rb_is_instance_id(ID id) {
  const char *s = rb_id2name(id);
  return s && s[0] == '@' && s[1] != '@' && s[1] != '\0';
}

int rb_is_class_id(ID id) {
  const char *s = rb_id2name(id);
  return s && s[0] == '@' && s[1] == '@' && s[2] != '\0';
}

VALUE rb_syserr_new_str(int n, VALUE arg) {
  VALUE cls = rb_const_get(rb_cObject, rb_intern("SystemCallError"));
  if (NIL_P(arg)) return rb_funcall(cls, rb_intern("new"), 1, INT2NUM(n));
  return rb_funcall(cls, rb_intern("new"), 2, arg, INT2NUM(n));
}

VALUE rb_syserr_new(int n, const char *mesg) {
  return rb_syserr_new_str(n, mesg ? rb_str_new_cstr(mesg) : Qnil);
}

void rb_syserr_fail(int n, const char *mesg) {
  rb_exc_raise(rb_syserr_new(n, mesg));
  abort();
}

void rb_syserr_fail_str(int n, VALUE mesg) {
  rb_exc_raise(rb_syserr_new_str(n, mesg));
  abort();
}

VALUE rb_make_exception(int argc, const VALUE *argv) {
  if (argc == 0) return Qnil;
  if (argc > 3) rb_raise(rb_eArgError, "wrong number of arguments (given %d, expected 0..3)", argc);
  if (argc == 1) {
    if (RB_TYPE_P(argv[0], T_STRING)) return rb_exc_new_str(rb_eRuntimeError, argv[0]);
    return rb_funcall(argv[0], rb_intern("exception"), 0);
  }
  VALUE exc = rb_funcall(argv[0], rb_intern("exception"), 1, argv[1]);
  if (argc == 3) rb_funcall(exc, rb_intern("set_backtrace"), 1, argv[2]);
  return exc;
}

int rb_long2int(long n) {
  if (n < INT_MIN || n > INT_MAX) rb_raise(rb_eRangeError, "integer %ld out of range of int", n);
  return (int)n;
}

static mrb_value enum_size_cb(mrb_state *mrb, mrb_value self) {
  (void)self;
  const struct RProc *p = mrb->c->ci->proc;
  rb_enumerator_size_func *fn = (rb_enumerator_size_func *)mrb_cptr(p->e.env->stack[0]);
  VALUE obj = V(p->e.env->stack[1]);
  VALUE args = V(p->e.env->stack[2]);
  VALUE en = V(p->e.env->stack[3]);
  return MV(fn(obj, args, en));
}

VALUE rb_enumeratorize_with_size(VALUE obj, VALUE meth, int argc, const VALUE *argv, rb_enumerator_size_func *size_fn) {
  VALUE e = rb_enumeratorize(obj, meth, argc, argv);
  if (size_fn) {
    VALUE args = argc > 0 ? rb_ary_new_from_values(argc, argv) : Qnil;
    mrb_value env[4] = { mrb_cptr_value(M, (void *)size_fn), MV(obj), MV(args), MV(e) };
    struct RProc *p = mrb_proc_new_cfunc_with_env(M, enum_size_cb, 4, env);
    mrb_iv_set(M, MV(e), SYM("@size_val"), mrb_obj_value(p));
  }
  return e;
}

VALUE rb_define_finalizer(VALUE obj, VALUE block) {
  VALUE os = rb_const_get(rb_cObject, rb_intern("ObjectSpace"));
  return rb_funcall(os, rb_intern("define_finalizer"), 2, obj, block);
}

VALUE rb_undefine_finalizer(VALUE obj) {
  VALUE os = rb_const_get(rb_cObject, rb_intern("ObjectSpace"));
  return rb_funcall(os, rb_intern("undefine_finalizer"), 1, obj);
}

void rb_mem_clear(VALUE *mem, long size) {
  for (long i = 0; i < size; i++) mem[i] = Qnil;
}

VALUE rb_ary_rotate(VALUE ary, long cnt) {
  return V(call1(need_ary(ary), "rotate!", mrb_int_value(M, cnt)));
}

VALUE rb_ary_sort(VALUE ary) {
  return V(call0(need_ary(ary), "sort"));
}

VALUE rb_get_path(VALUE obj) {
  VALUE f = rb_const_get(rb_cObject, rb_intern("File"));
  return rb_funcall(f, rb_intern("path"), 1, obj);
}

VALUE rb_file_open(const char *fname, const char *modestr) {
  return rb_file_open_str(rb_str_new_cstr(fname), modestr);
}

VALUE rb_file_open_str(VALUE fname, const char *modestr) {
  VALUE f = rb_const_get(rb_cObject, rb_intern("File"));
  return rb_funcall(f, rb_intern("open"), 2, fname, rb_str_new_cstr(modestr ? modestr : "r"));
}

VALUE rb_Hash(VALUE val) {
  return rb_funcall(rb_mKernel, rb_intern("Hash"), 1, val);
}

VALUE rb_ident_hash_new(void) {
  VALUE h = rb_hash_new();
  call0(MV(h), "compare_by_identity");
  return h;
}

void rb_alias(VALUE klass, ID new_name, ID old_name) {
  mrb_alias_method(M, mrb_class_ptr(MV(klass)), (mrb_sym)new_name, (mrb_sym)old_name);
}

void rb_undef(VALUE klass, ID name) {
  mrb_undef_method_id(M, mrb_class_ptr(MV(klass)), (mrb_sym)name);
}

VALUE rb_mod_ancestors(VALUE mod) {
  return V(call0(MV(mod), "ancestors"));
}

void rb_deprecate_constant(VALUE mod, const char *name) {
  call1(MV(mod), "deprecate_constant", mrb_symbol_value(mrb_intern_cstr(M, name)));
}

VALUE rb_Rational(VALUE num, VALUE den) {
  return rb_funcall(rb_mKernel, rb_intern("Rational"), 2, num, den);
}
VALUE rb_Rational1(VALUE num) {
  return rb_funcall(rb_mKernel, rb_intern("Rational"), 1, num);
}
VALUE rb_Rational2(VALUE num, VALUE den) { return rb_Rational(num, den); }
VALUE rb_rational_num(VALUE rat) { return V(call0(MV(rat), "numerator")); }
VALUE rb_rational_den(VALUE rat) { return V(call0(MV(rat), "denominator")); }

VALUE rb_Complex(VALUE real, VALUE imag) {
  return rb_funcall(rb_mKernel, rb_intern("Complex"), 2, real, imag);
}
VALUE rb_Complex1(VALUE real) {
  return rb_funcall(rb_mKernel, rb_intern("Complex"), 1, real);
}
VALUE rb_Complex2(VALUE real, VALUE imag) { return rb_Complex(real, imag); }
VALUE rb_complex_real(VALUE cmp) { return V(call0(MV(cmp), "real")); }
VALUE rb_complex_imag(VALUE cmp) { return V(call0(MV(cmp), "imag")); }

struct mutex_sync_data {
  VALUE (*func)(VALUE);
  VALUE arg;
  VALUE ret;
};

static mrb_value mutex_sync_body(mrb_state *mrb, void *p) {
  (void)mrb;
  struct mutex_sync_data *d = (struct mutex_sync_data *)p;
  d->ret = d->func(d->arg);
  return mrb_nil_value();
}

VALUE rb_mutex_new(void) {
  return rb_funcall(rb_const_get(rb_cObject, rb_intern("Mutex")), rb_intern("new"), 0);
}
VALUE rb_mutex_locked_p(VALUE mutex) { return V(call0(MV(mutex), "locked?")); }
VALUE rb_mutex_trylock(VALUE mutex) { return V(call0(MV(mutex), "try_lock")); }
VALUE rb_mutex_lock(VALUE mutex) { call0(MV(mutex), "lock"); return mutex; }
VALUE rb_mutex_unlock(VALUE mutex) { call0(MV(mutex), "unlock"); return mutex; }
VALUE rb_mutex_sleep(VALUE mutex, VALUE timeout) {
  return NIL_P(timeout) ? V(call0(MV(mutex), "sleep")) : V(call1(MV(mutex), "sleep", MV(timeout)));
}

VALUE rb_mutex_synchronize(VALUE mutex, VALUE (*func)(VALUE arg), VALUE arg) {
  call0(MV(mutex), "lock");
  struct mutex_sync_data d = { func, arg, Qnil };
  mrb_bool failed = FALSE;
  mrb_value exc = mrb_protect_error(M, mutex_sync_body, &d, &failed);
  call0(MV(mutex), "unlock");
  if (failed) mrb_exc_raise(M, exc);
  return d.ret;
}

VALUE rb_proc_call_kw(VALUE proc, VALUE args, int kw_splat) {
  mrb_value a = MV(args);
  long n = RARRAY_LEN(a);
  VALUE buf[8];
  VALUE *av = n <= 8 ? buf : (VALUE *)mrb_malloc(M, sizeof(VALUE) * (size_t)n);
  for (long i = 0; i < n; i++) av[i] = V(RARRAY_PTR(a)[i]);
  VALUE r = send_kw(MV(proc), SYM("call"), (int)n, av, mrb_nil_value(), kw_splat, 0);
  if (av != buf) mrb_free(M, av);
  return r;
}

VALUE rb_proc_call_with_block_kw(VALUE proc, int argc, const VALUE *argv, VALUE blk, int kw_splat) {
  return send_kw(MV(proc), SYM("call"), argc, argv, MV(blk), kw_splat, 0);
}
