/* The Ruby C API of ORuby (gem/capi): MRI's extension API over mruby, for
 * the one mrb_state of a standalone oruby process or a compiled oruby app.
 *
 * This is not a copy of MRI's headers. It declares the part of the API that
 * gem/capi implements, with MRI's names, signatures and value layout, so an
 * extension compiles from source against it unchanged. What it leaves out
 * is a compile error rather than a wrong answer: there are no RBasic,
 * RString or RArray structs (mruby's objects are laid out differently), so
 * RSTRING_PTR and the like are functions, and RARRAY_PTR (VALUEs mruby
 * does not have) is a snapshot of the array (rb_array_const_ptr).
 *
 * A VALUE is a word, as MRI's: Qfalse, Qnil, Qtrue and Qundef, Fixnums
 * (2n+1), Symbols (ID << 8 | 0x0c) and flonums are immediates encoded as MRI
 * encodes them; any other object is the address of its mruby object. An ID
 * is an mrb_sym.
 */
#ifndef ORUBY_CAPI_RUBY_H
#define ORUBY_CAPI_RUBY_H 1

#include "ruby/config.h" /* HAVE_ and SIZEOF_, first, as MRI's ruby.h has it */
#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>
#define RBIMPL_STDBOOL_H 1 /* bool is stdbool's, as in MRI (ruby/internal/stdbool.h) */
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>
#include <math.h>
#include <sys/types.h>
/* what MRI's ruby.h brings in too, which extensions rely on */
#include <stdio.h>
#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <alloca.h>
#include <assert.h>
#include <unistd.h>
#include <time.h>
#include <sys/time.h>

#if defined(__cplusplus)
extern "C" {
#endif

#define ORUBY_CAPI 1
/* The layout these headers give a VALUE and the API: a library built with
   them carries the marker (a weak definition in each file), and oruby loads
   only a library that does (an MRI-built extension reads MRI's objects). */
#define ORUBY_CAPI_ABI 1
__attribute__((weak, used, visibility("default"))) int oruby_capi_abi_1 = ORUBY_CAPI_ABI;
#define RUBY_API_VERSION_MAJOR 4
#define RUBY_API_VERSION_MINOR 1
#define RUBY_API_VERSION_TEENY 0
#define RUBY_API_VERSION_CODE (RUBY_API_VERSION_MAJOR*10000+RUBY_API_VERSION_MINOR*100+RUBY_API_VERSION_TEENY)
#define HAVE_RUBY_ENCODING_H 1
#define HAVE_RB_EXT_RACTOR_SAFE 1
#define HAVE_RB_CLASS_NEW_INSTANCE_KW 1
#define HAVE_RB_HASH_BULK_INSERT 0

#define RUBY_EXTERN extern
#define RUBY_FUNC_EXPORTED __attribute__((__visibility__("default"))) /* Init_<name> under -fvisibility=hidden */
#define RUBY_SYMBOL_EXPORT_BEGIN
#define RUBY_SYMBOL_EXPORT_END
#define RBIMPL_ATTR_NORETURN() __attribute__((__noreturn__))
#ifndef NORETURN
# define NORETURN(x) __attribute__((__noreturn__)) x
#endif
#ifndef PRINTF_ARGS
# define PRINTF_ARGS(decl, string_index, first_to_check) decl
#endif
#ifndef RB_UNLIKELY
# define RB_UNLIKELY(x) __builtin_expect(!!(x), 0)
# define RB_LIKELY(x) __builtin_expect(!!(x), 1)
#endif
#ifndef UNLIKELY
# define UNLIKELY(x) RB_UNLIKELY(x)
# define LIKELY(x) RB_LIKELY(x)
#endif
#define RB_GNUC_EXTENSION __extension__
#define RB_GNUC_EXTENSION_BLOCK(x) __extension__ ({ x; })
#ifndef TRUE
# define TRUE 1
#endif
#ifndef FALSE
# define FALSE 0
#endif

typedef uintptr_t VALUE;
typedef uintptr_t ID;
typedef intptr_t SIGNED_VALUE;
#define SIZEOF_VALUE 8
#define SIZEOF_LONG 8
#define SIZEOF_INT 4
#define SIZEOF_VOIDP 8
#define PRI_VALUE_PREFIX "l"
#define PRI_LONG_PREFIX "l"
#define PRI_SIZE_PREFIX "z"
#define PRIdVALUE PRI_VALUE_PREFIX"d"
#define PRIuVALUE PRI_VALUE_PREFIX"u"
#define PRIxVALUE PRI_VALUE_PREFIX"x"
/* "%"PRIsVALUE formats a VALUE with #to_s, "%+"PRIsVALUE with #inspect */
#define PRIsVALUE PRI_VALUE_PREFIX"i" "\v"
#define PRI_SIZE_PREFIX "z"

/* ANYARGS: a method function is cast to this and called with the arguments
   its arity says; in C++, where () takes none, MRI's (...) */
#if defined(__cplusplus)
#define ANYARGS ...
#else
#define ANYARGS
#endif
typedef VALUE (*rb_capi_anyargs_func)(ANYARGS);
#define RUBY_METHOD_FUNC(func) ((rb_capi_anyargs_func)(func))

/* ---- special constants (MRI's 64-bit flonum layout) ---- */
#define RUBY_Qfalse ((VALUE)0x00)
#define RUBY_Qnil   ((VALUE)0x04)
#define RUBY_Qtrue  ((VALUE)0x14)
#define RUBY_Qundef ((VALUE)0x24)
#define RUBY_IMMEDIATE_MASK 0x07
#define RUBY_FIXNUM_FLAG 0x01
#define RUBY_FLONUM_MASK 0x03
#define RUBY_FLONUM_FLAG 0x02
#define RUBY_SYMBOL_FLAG 0x0c
#define RUBY_SPECIAL_SHIFT 8
#define Qfalse RUBY_Qfalse
#define Qnil   RUBY_Qnil
#define Qtrue  RUBY_Qtrue
#define Qundef RUBY_Qundef
#define IMMEDIATE_MASK RUBY_IMMEDIATE_MASK
#define FIXNUM_FLAG RUBY_FIXNUM_FLAG
#define SYMBOL_FLAG RUBY_SYMBOL_FLAG

#define RB_TEST(v) (((VALUE)(v) & ~RUBY_Qnil) != 0)
#define RTEST(v) RB_TEST(v)
#define RB_NIL_P(v) ((VALUE)(v) == RUBY_Qnil)
#define NIL_P(v) RB_NIL_P(v)
#define RB_UNDEF_P(v) ((VALUE)(v) == RUBY_Qundef)
#define UNDEF_P(v) RB_UNDEF_P(v)
#define RB_NIL_OR_UNDEF_P(v) (RB_NIL_P(v) || RB_UNDEF_P(v))
#define RB_FIXNUM_P(v) (((SIGNED_VALUE)(v)) & RUBY_FIXNUM_FLAG)
#define FIXNUM_P(v) RB_FIXNUM_P(v)
#define RB_STATIC_SYM_P(v) ((((VALUE)(v)) & 0xff) == RUBY_SYMBOL_FLAG)
#define STATIC_SYM_P(v) RB_STATIC_SYM_P(v)
#define RB_FLONUM_P(v) ((((VALUE)(v)) & RUBY_FLONUM_MASK) == RUBY_FLONUM_FLAG)
#define FLONUM_P(v) RB_FLONUM_P(v)
#define RB_IMMEDIATE_P(v) (((VALUE)(v)) & RUBY_IMMEDIATE_MASK)
#define IMMEDIATE_P(v) RB_IMMEDIATE_P(v)
#define RB_SPECIAL_CONST_P(v) (RB_IMMEDIATE_P(v) || (VALUE)(v) == RUBY_Qfalse)
#define SPECIAL_CONST_P(v) RB_SPECIAL_CONST_P(v)
static inline int rb_special_const_p(VALUE v) { return RB_SPECIAL_CONST_P(v); }

/* ---- Fixnum and Integer ---- */
#define RUBY_FIXNUM_MAX (LONG_MAX >> 1)
#define RUBY_FIXNUM_MIN (LONG_MIN >> 1)
#define FIXNUM_MAX RUBY_FIXNUM_MAX
#define FIXNUM_MIN RUBY_FIXNUM_MIN
#define RB_POSFIXABLE(f) ((f) < RUBY_FIXNUM_MAX + 1)
#define RB_NEGFIXABLE(f) ((f) >= RUBY_FIXNUM_MIN)
#define RB_FIXABLE(f) (RB_POSFIXABLE(f) && RB_NEGFIXABLE(f))
#define POSFIXABLE(f) RB_POSFIXABLE(f)
#define NEGFIXABLE(f) RB_NEGFIXABLE(f)
#define FIXABLE(f) RB_FIXABLE(f)
#define RB_INT2FIX(i) ((VALUE)((((unsigned long)(long)(i)) << 1) | RUBY_FIXNUM_FLAG))
#define INT2FIX(i) RB_INT2FIX(i)
#define LONG2FIX(i) RB_INT2FIX(i)
#define RB_FIX2LONG(v) ((long)(((SIGNED_VALUE)(v)) >> 1))
#define FIX2LONG(v) RB_FIX2LONG(v)
#define FIX2ULONG(v) ((unsigned long)RB_FIX2LONG(v))
#define FIX2INT(v) ((int)RB_FIX2LONG(v))
#define FIX2UINT(v) ((unsigned int)RB_FIX2LONG(v))
#define FIX2SHORT(v) ((short)RB_FIX2LONG(v))
#define INT2NUM(i) rb_int2inum((intptr_t)(i))
#define UINT2NUM(i) rb_uint2inum((uintptr_t)(i))
#define LONG2NUM(i) rb_int2inum((intptr_t)(i))
#define ULONG2NUM(i) rb_uint2inum((uintptr_t)(i))
#define LL2NUM(i) rb_ll2inum((long long)(i))
#define ULL2NUM(i) rb_ull2inum((unsigned long long)(i))
#define SIZET2NUM(i) rb_uint2inum((uintptr_t)(i))
#define SSIZET2NUM(i) rb_int2inum((intptr_t)(i))
#define CHR2FIX(c) INT2FIX((long)((c) & 0xff))
#define NUM2INT(v) ((int)rb_num2long(v))
#define NUM2UINT(v) ((unsigned int)rb_num2ulong(v))
#define NUM2LONG(v) rb_num2long(v)
#define NUM2ULONG(v) rb_num2ulong(v)
#define NUM2LL(v) rb_num2ll(v)
#define NUM2ULL(v) rb_num2ull(v)
#define NUM2SIZET(v) ((size_t)rb_num2ulong(v))
#define NUM2SSIZET(v) ((ssize_t)rb_num2long(v))
#define NUM2SHORT(v) ((short)rb_num2long(v))
#define NUM2CHR(v) rb_num2chr(v)
#define NUM2DBL(v) rb_num2dbl(v)
#define DBL2NUM(d) rb_float_new(d)
#define RFLOAT_VALUE(v) rb_float_value(v)
#define FIX2NUM(v) (v)

VALUE rb_int2inum(intptr_t);
VALUE rb_uint2inum(uintptr_t);
VALUE rb_ll2inum(long long);
VALUE rb_ull2inum(unsigned long long);
VALUE rb_int2big(intptr_t);
long rb_num2long(VALUE);
unsigned long rb_num2ulong(VALUE);
long long rb_num2ll(VALUE);
unsigned long long rb_num2ull(VALUE);
char rb_num2chr(VALUE);
double rb_num2dbl(VALUE);
VALUE rb_float_new(double);
VALUE rb_float_new_in_heap(double);
double rb_float_value(VALUE);
VALUE rb_Integer(VALUE);
VALUE rb_Float(VALUE);
VALUE rb_String(VALUE);
VALUE rb_Array(VALUE);
VALUE rb_cstr2inum(const char *, int);
VALUE rb_str2inum(VALUE, int);
VALUE rb_to_int(VALUE);
VALUE rb_fix2str(VALUE, int);
int rb_cmpint(VALUE, VALUE, VALUE);

/* ---- types ---- */
enum ruby_value_type {
  RUBY_T_NONE = 0x00, RUBY_T_OBJECT = 0x01, RUBY_T_CLASS = 0x02, RUBY_T_MODULE = 0x03,
  RUBY_T_FLOAT = 0x04, RUBY_T_STRING = 0x05, RUBY_T_REGEXP = 0x06, RUBY_T_ARRAY = 0x07,
  RUBY_T_HASH = 0x08, RUBY_T_STRUCT = 0x09, RUBY_T_BIGNUM = 0x0a, RUBY_T_FILE = 0x0b,
  RUBY_T_DATA = 0x0c, RUBY_T_MATCH = 0x0d, RUBY_T_COMPLEX = 0x0e, RUBY_T_RATIONAL = 0x0f,
  RUBY_T_NIL = 0x11, RUBY_T_TRUE = 0x12, RUBY_T_FALSE = 0x13, RUBY_T_SYMBOL = 0x14,
  RUBY_T_FIXNUM = 0x15, RUBY_T_UNDEF = 0x16, RUBY_T_IMEMO = 0x1a, RUBY_T_NODE = 0x1b,
  RUBY_T_ICLASS = 0x1c, RUBY_T_ZOMBIE = 0x1d, RUBY_T_MOVED = 0x1e, RUBY_T_MASK = 0x1f
};
#define T_NONE RUBY_T_NONE
#define T_OBJECT RUBY_T_OBJECT
#define T_CLASS RUBY_T_CLASS
#define T_MODULE RUBY_T_MODULE
#define T_FLOAT RUBY_T_FLOAT
#define T_STRING RUBY_T_STRING
#define T_REGEXP RUBY_T_REGEXP
#define T_ARRAY RUBY_T_ARRAY
#define T_HASH RUBY_T_HASH
#define T_STRUCT RUBY_T_STRUCT
#define T_BIGNUM RUBY_T_BIGNUM
#define T_FILE RUBY_T_FILE
#define T_DATA RUBY_T_DATA
#define T_MATCH RUBY_T_MATCH
#define T_COMPLEX RUBY_T_COMPLEX
#define T_RATIONAL RUBY_T_RATIONAL
#define T_NIL RUBY_T_NIL
#define T_TRUE RUBY_T_TRUE
#define T_FALSE RUBY_T_FALSE
#define T_SYMBOL RUBY_T_SYMBOL
#define T_FIXNUM RUBY_T_FIXNUM
#define T_UNDEF RUBY_T_UNDEF
#define T_ICLASS RUBY_T_ICLASS
#define T_MASK RUBY_T_MASK

int rb_type(VALUE);
#define RB_TYPE_P(v, t) (rb_type((VALUE)(v)) == (int)(t))
#define TYPE(v) rb_type((VALUE)(v))
#define BUILTIN_TYPE(v) rb_type((VALUE)(v))
#define RB_BUILTIN_TYPE(v) rb_type((VALUE)(v))
void rb_check_type(VALUE, int);
#define Check_Type(v, t) rb_check_type((VALUE)(v), (t))
#define RB_FLOAT_TYPE_P(v) RB_TYPE_P(v, T_FLOAT)
#define RB_INTEGER_TYPE_P(v) (RB_FIXNUM_P(v) || RB_TYPE_P(v, T_BIGNUM))
#define RB_SYMBOL_P(v) RB_STATIC_SYM_P(v)
#define SYMBOL_P(v) RB_SYMBOL_P(v)
#define RB_DYNAMIC_SYM_P(v) 0
#define DYNAMIC_SYM_P(v) 0

/* ---- Symbols and IDs (every Symbol is a static one) ---- */
#define RB_ID2SYM(id) ((VALUE)(((VALUE)(id) << RUBY_SPECIAL_SHIFT) | RUBY_SYMBOL_FLAG))
#define ID2SYM(id) RB_ID2SYM(id)
#define RB_SYM2ID(v) rb_sym2id(v)
#define SYM2ID(v) RB_SYM2ID(v)
ID rb_intern(const char *);
ID rb_intern2(const char *, long);
ID rb_intern_const(const char *);
ID rb_intern_str(VALUE);
ID rb_to_id(VALUE);
ID rb_check_id(volatile VALUE *);
VALUE rb_check_symbol(volatile VALUE *);
const char *rb_id2name(ID);
VALUE rb_id2str(ID);
VALUE rb_id2sym(ID);
ID rb_sym2id(VALUE);
VALUE rb_sym2str(VALUE);
VALUE rb_to_symbol(VALUE);
VALUE rb_str_intern(VALUE);
/* rb_intern of a string literal is looked up once per call site, as MRI's
   (mruby's symbol table is a slow place to look in); the cache is dropped
   when the C API binds another state, whose IDs differ */
extern unsigned int rb_capi_generation;
#define RB_CAPI_CONST_ID(str) __extension__ ({ \
    static ID rbimpl_id; static unsigned int rbimpl_gen; \
    if (rbimpl_gen != rb_capi_generation) { rbimpl_id = rb_intern_const(str); rbimpl_gen = rb_capi_generation; } \
    rbimpl_id; })
#ifndef ORUBY_CAPI_IMPL
# define rb_intern(str) (__builtin_constant_p(str) ? RB_CAPI_CONST_ID(str) : (rb_intern)(str))
#endif
#define CONST_ID(var, str) ((var) = RB_CAPI_CONST_ID(str))

/* ---- classes and modules ---- */
RUBY_EXTERN VALUE rb_mKernel, rb_mComparable, rb_mEnumerable, rb_mErrno, rb_mGC, rb_mMath, rb_mProcess;
RUBY_EXTERN VALUE rb_cBasicObject, rb_cObject, rb_cModule, rb_cClass, rb_cArray, rb_cHash, rb_cString,
  rb_cSymbol, rb_cInteger, rb_cFloat, rb_cNumeric, rb_cNilClass, rb_cTrueClass, rb_cFalseClass, rb_cProc,
  rb_cMethod, rb_cRange, rb_cRegexp, rb_cMatch, rb_cStruct, rb_cTime, rb_cIO, rb_cFile, rb_cEncoding,
  rb_cRational, rb_cComplex, rb_cRandom, rb_cThread, rb_cEnumerator;
RUBY_EXTERN VALUE rb_eException, rb_eStandardError, rb_eRuntimeError, rb_eArgError, rb_eTypeError,
  rb_eNameError, rb_eNoMethodError, rb_eIndexError, rb_eKeyError, rb_eRangeError, rb_eNotImpError,
  rb_eIOError, rb_eEOFError, rb_eFrozenError, rb_eStopIteration, rb_eZeroDivError, rb_eFloatDomainError,
  rb_eScriptError, rb_eLoadError, rb_eSyntaxError, rb_eNoMemError, rb_eSystemExit, rb_eInterrupt,
  rb_eSignal, rb_eFatal, rb_eSecurityError, rb_eEncodingError, rb_eEncCompatError, rb_eLocalJumpError,
  rb_eRegexpError, rb_eSystemCallError, rb_eThreadError, rb_eMathDomainError, rb_eNoMatchingPatternError,
  rb_eNoMatchingPatternKeyError;
#define rb_eArgumentError rb_eArgError

VALUE rb_define_class(const char *, VALUE);
VALUE rb_define_class_under(VALUE, const char *, VALUE);
VALUE rb_define_class_id_under(VALUE, ID, VALUE);
VALUE rb_define_module(const char *);
VALUE rb_define_module_under(VALUE, const char *);
VALUE rb_define_module_id_under(VALUE, ID);
VALUE rb_class_new(VALUE);
VALUE rb_module_new(void);
void rb_define_method(VALUE, const char *, VALUE (*)(ANYARGS), int);
void rb_define_method_id(VALUE, ID, VALUE (*)(ANYARGS), int);
void rb_define_private_method(VALUE, const char *, VALUE (*)(ANYARGS), int);
void rb_define_protected_method(VALUE, const char *, VALUE (*)(ANYARGS), int);
void rb_define_singleton_method(VALUE, const char *, VALUE (*)(ANYARGS), int);
void rb_define_module_function(VALUE, const char *, VALUE (*)(ANYARGS), int);
void rb_define_global_function(const char *, VALUE (*)(ANYARGS), int);
void rb_undef_method(VALUE, const char *);
void rb_define_alias(VALUE, const char *, const char *);
void rb_define_attr(VALUE, const char *, int, int);
void rb_define_const(VALUE, const char *, VALUE);
void rb_define_global_const(const char *, VALUE);
typedef VALUE (*rb_alloc_func_t)(VALUE);
void rb_define_alloc_func(VALUE, rb_alloc_func_t);
void rb_undef_alloc_func(VALUE);
void rb_include_module(VALUE, VALUE);
void rb_extend_object(VALUE, VALUE);
void rb_prepend_module(VALUE, VALUE);
VALUE rb_const_get(VALUE, ID);
VALUE rb_const_get_at(VALUE, ID);
VALUE rb_const_get_from(VALUE, ID);
void rb_const_set(VALUE, ID, VALUE);
int rb_const_defined(VALUE, ID);
int rb_const_defined_at(VALUE, ID);
VALUE rb_path2class(const char *);
VALUE rb_path_to_class(VALUE);
VALUE rb_class_name(VALUE);
VALUE rb_class_path(VALUE);
VALUE rb_mod_name(VALUE);
const char *rb_class2name(VALUE);
const char *rb_obj_classname(VALUE);
VALUE rb_class_of(VALUE);
#define CLASS_OF(v) rb_class_of((VALUE)(v))
VALUE rb_obj_class(VALUE);
VALUE rb_class_real(VALUE);
VALUE rb_class_superclass(VALUE);
VALUE rb_class_inherited_p(VALUE, VALUE);
VALUE rb_singleton_class(VALUE);
VALUE rb_class_new_instance(int, const VALUE *, VALUE);
VALUE rb_class_new_instance_kw(int, const VALUE *, VALUE, int);
VALUE rb_class_new_instance_pass_kw(int, const VALUE *, VALUE);
VALUE rb_obj_alloc(VALUE);
VALUE rb_obj_is_kind_of(VALUE, VALUE);
VALUE rb_obj_is_instance_of(VALUE, VALUE);
int rb_respond_to(VALUE, ID);
int rb_obj_respond_to(VALUE, ID, int);
int rb_method_boundp(VALUE, ID, int);
VALUE rb_obj_freeze(VALUE);
VALUE rb_obj_frozen_p(VALUE);
void rb_check_frozen(VALUE);
#define rb_check_frozen_inline rb_check_frozen
#define OBJ_FROZEN(v) RTEST(rb_obj_frozen_p((VALUE)(v)))
#define RB_OBJ_FROZEN(v) OBJ_FROZEN(v)
#define OBJ_FREEZE(v) rb_obj_freeze((VALUE)(v))
#define RB_OBJ_FREEZE(v) OBJ_FREEZE(v)
VALUE rb_obj_dup(VALUE);
VALUE rb_obj_clone(VALUE);
VALUE rb_obj_id(VALUE);
VALUE rb_equal(VALUE, VALUE);
int rb_eql(VALUE, VALUE);
VALUE rb_inspect(VALUE);
VALUE rb_obj_as_string(VALUE);
VALUE rb_any_to_s(VALUE);
VALUE rb_obj_method(VALUE, VALUE);
VALUE rb_ivar_get(VALUE, ID);
VALUE rb_ivar_set(VALUE, ID, VALUE);
VALUE rb_ivar_defined(VALUE, ID);
VALUE rb_iv_get(VALUE, const char *);
VALUE rb_iv_set(VALUE, const char *, VALUE);
VALUE rb_attr_get(VALUE, ID);
VALUE rb_cvar_get(VALUE, ID);
void rb_cvar_set(VALUE, ID, VALUE);
VALUE rb_gv_get(const char *);
VALUE rb_gv_set(const char *, VALUE);
VALUE rb_hash(VALUE);
VALUE rb_obj_instance_variables(VALUE);
VALUE rb_convert_type(VALUE, int, const char *, const char *);
VALUE rb_check_convert_type(VALUE, int, const char *, const char *);
VALUE rb_check_string_type(VALUE);
VALUE rb_check_array_type(VALUE);
VALUE rb_check_hash_type(VALUE);
VALUE rb_check_funcall(VALUE, ID, int, const VALUE *);

/* ---- calls and blocks ---- */
#define RB_NO_KEYWORDS 0
#define RB_PASS_KEYWORDS 1
#define RB_PASS_CALLED_KEYWORDS rb_keyword_given_p()
VALUE rb_funcall(VALUE, ID, int, ...);
VALUE rb_funcallv(VALUE, ID, int, const VALUE *);
VALUE rb_funcallv_kw(VALUE, ID, int, const VALUE *, int);
VALUE rb_funcallv_public(VALUE, ID, int, const VALUE *);
VALUE rb_funcallv_public_kw(VALUE, ID, int, const VALUE *, int);
#define rb_funcall2 rb_funcallv
#define rb_funcall3 rb_funcallv_public
VALUE rb_funcall_passing_block(VALUE, ID, int, const VALUE *);
VALUE rb_funcall_with_block(VALUE, ID, int, const VALUE *, VALUE);
VALUE rb_funcall_with_block_kw(VALUE, ID, int, const VALUE *, VALUE, int);
VALUE rb_call_super(int, const VALUE *);
int rb_block_given_p(void);
void rb_need_block(void);
VALUE rb_block_proc(void);
VALUE rb_block_lambda(void);
VALUE rb_yield(VALUE);
VALUE rb_yield_values(int, ...);
VALUE rb_yield_values2(int, const VALUE *);
VALUE rb_yield_splat(VALUE);
typedef VALUE rb_block_call_func(VALUE yielded_arg, VALUE callback_arg, int argc, const VALUE *argv, VALUE blockarg);
typedef rb_block_call_func *rb_block_call_func_t;
#define RB_BLOCK_CALL_FUNC_ARGLIST(yielded_arg, callback_arg) \
  VALUE yielded_arg, VALUE callback_arg, int argc, const VALUE *argv, VALUE blockarg
VALUE rb_block_call(VALUE, ID, int, const VALUE *, rb_block_call_func_t, VALUE);
VALUE rb_proc_new(rb_block_call_func_t, VALUE);
VALUE rb_proc_call(VALUE, VALUE);
VALUE rb_proc_call_with_block(VALUE, int, const VALUE *, VALUE);
VALUE rb_obj_is_proc(VALUE);
int rb_keyword_given_p(void);
int rb_scan_args(int, const VALUE *, const char *, ...);
#define RB_SCAN_ARGS_PASS_CALLED_KEYWORDS 0
#define RB_SCAN_ARGS_KEYWORDS 1
#define RB_SCAN_ARGS_LAST_HASH_KEYWORDS 3
int rb_scan_args_kw(int, int, const VALUE *, const char *, ...);
int rb_get_kwargs(VALUE, const ID *, int, int, VALUE *);
VALUE rb_eval_string(const char *);
VALUE rb_eval_string_protect(const char *, int *);
VALUE rb_require(const char *);
VALUE rb_f_require(VALUE, VALUE);
VALUE rb_current_receiver(void);
ID rb_frame_this_func(void);
#define rb_frame_callee rb_frame_this_func

/* ---- errors ---- */
NORETURN(void rb_raise(VALUE, const char *, ...));
NORETURN(void rb_fatal(const char *, ...));
NORETURN(void rb_bug(const char *, ...));
NORETURN(void rb_exc_raise(VALUE));
NORETURN(void rb_sys_fail(const char *));
NORETURN(void rb_sys_fail_str(VALUE));
NORETURN(void rb_notimplement(void));
NORETURN(void rb_error_arity(int, int, int));
NORETURN(void rb_jump_tag(int));
NORETURN(void rb_error_frozen(const char *));
NORETURN(void rb_error_frozen_object(VALUE));
NORETURN(void rb_frozen_error_raise(VALUE, const char *, ...));
NORETURN(void rb_memerror(void));
NORETURN(void rb_exc_fatal(VALUE));
NORETURN(void rb_num_zerodiv(void));
NORETURN(void rb_cmperr(VALUE, VALUE));
NORETURN(void rb_invalid_str(const char *, const char *));
void rb_warn(const char *, ...);
void rb_warning(const char *, ...);
VALUE rb_exc_new(VALUE, const char *, long);
VALUE rb_exc_new_cstr(VALUE, const char *);
VALUE rb_exc_new_str(VALUE, VALUE);
#define rb_exc_new2 rb_exc_new_cstr
#define rb_exc_new3 rb_exc_new_str
VALUE rb_ensure(VALUE (*)(VALUE), VALUE, VALUE (*)(VALUE), VALUE);
VALUE rb_rescue(VALUE (*)(VALUE), VALUE, VALUE (*)(VALUE, VALUE), VALUE);
VALUE rb_rescue2(VALUE (*)(VALUE), VALUE, VALUE (*)(VALUE, VALUE), VALUE, ...);
VALUE rb_protect(VALUE (*)(VALUE), VALUE, int *);
VALUE rb_errinfo(void);
void rb_set_errinfo(VALUE);
int rb_safe_level(void);
#define rb_check_arity(argc, min, max) do { \
  int rbimpl_argc = (argc), rbimpl_min = (min), rbimpl_max = (max); \
  if (rbimpl_argc < rbimpl_min || (rbimpl_max != -1 && rbimpl_argc > rbimpl_max)) \
    rb_error_arity(rbimpl_argc, rbimpl_min, rbimpl_max); } while (0)
#define UNLIMITED_ARGUMENTS (-1)
#define RETURN_ENUMERATOR(obj, argc, argv) do { \
  if (!rb_block_given_p()) return rb_enumeratorize((obj), ID2SYM(rb_frame_this_func()), (argc), (argv)); } while (0)
#define RETURN_SIZED_ENUMERATOR(obj, argc, argv, size_fn) RETURN_ENUMERATOR(obj, argc, argv)
VALUE rb_enumeratorize(VALUE, VALUE, int, const VALUE *);

/* ---- Strings ---- */
char *rb_capi_rstring_ptr(VALUE);
long rb_capi_rstring_len(VALUE);
#define RSTRING_PTR(s) rb_capi_rstring_ptr((VALUE)(s))
#define RSTRING_LEN(s) rb_capi_rstring_len((VALUE)(s))
#define RSTRING_END(s) (RSTRING_PTR(s) + RSTRING_LEN(s))
#define RSTRING_LENINT(s) ((int)RSTRING_LEN(s))
#define RSTRING_GETMEM(s, ptrvar, lenvar) ((ptrvar) = RSTRING_PTR(s), (lenvar) = RSTRING_LEN(s))
VALUE rb_string_value(volatile VALUE *);
char *rb_string_value_ptr(volatile VALUE *);
char *rb_string_value_cstr(volatile VALUE *);
#define StringValue(v) rb_string_value(&(v))
#define StringValuePtr(v) rb_string_value_ptr(&(v))
#define StringValueCStr(v) rb_string_value_cstr(&(v))
#define SafeStringValue(v) StringValue(v)
#define ExportStringValue(v) StringValue(v)
VALUE rb_str_new(const char *, long);
VALUE rb_str_new_cstr(const char *);
VALUE rb_str_new_shared(VALUE);
VALUE rb_str_new_frozen(VALUE);
VALUE rb_str_new_static(const char *, long);
VALUE rb_str_new_with_class(VALUE, const char *, long);
VALUE rb_usascii_str_new(const char *, long);
VALUE rb_usascii_str_new_cstr(const char *);
VALUE rb_utf8_str_new(const char *, long);
VALUE rb_utf8_str_new_cstr(const char *);
VALUE rb_external_str_new(const char *, long);
VALUE rb_locale_str_new(const char *, long);
VALUE rb_str_buf_new(long);
VALUE rb_str_buf_new_cstr(const char *);
VALUE rb_str_tmp_new(long);
VALUE rb_interned_str(const char *, long);
VALUE rb_interned_str_cstr(const char *);
VALUE rb_fstring_cstr(const char *);
#define rb_str_new2 rb_str_new_cstr
#define rb_str_new3 rb_str_new_shared
#define rb_str_new4 rb_str_new_frozen
#define rb_str_new5 rb_str_new_with_class
#define rb_str_buf_new2 rb_str_buf_new_cstr
#define rb_usascii_str_new2 rb_usascii_str_new_cstr
#define rb_str_new_lit(s) rb_str_new((s), (long)(sizeof(s) - 1))
#define rb_str_new_literal(s) rb_str_new_lit(s)
#define rb_usascii_str_new_lit(s) rb_usascii_str_new((s), (long)(sizeof(s) - 1))
#define rb_utf8_str_new_lit(s) rb_utf8_str_new((s), (long)(sizeof(s) - 1))
#define rb_str_cat_lit(str, s) rb_str_cat((str), (s), (long)(sizeof(s) - 1))
VALUE rb_str_cat(VALUE, const char *, long);
VALUE rb_str_cat_cstr(VALUE, const char *);
#define rb_str_cat2 rb_str_cat_cstr
#define rb_str_buf_cat rb_str_cat
#define rb_str_buf_cat2 rb_str_cat_cstr
VALUE rb_str_buf_append(VALUE, VALUE);
VALUE rb_str_append(VALUE, VALUE);
VALUE rb_str_concat(VALUE, VALUE);
VALUE rb_str_plus(VALUE, VALUE);
VALUE rb_str_times(VALUE, VALUE);
VALUE rb_str_dup(VALUE);
VALUE rb_str_resurrect(VALUE);
VALUE rb_str_resize(VALUE, long);
void rb_str_set_len(VALUE, long);
void rb_str_modify(VALUE);
void rb_str_modify_expand(VALUE, long);
VALUE rb_str_freeze(VALUE);
VALUE rb_str_length(VALUE);
long rb_str_strlen(VALUE);
VALUE rb_str_substr(VALUE, long, long);
VALUE rb_str_subseq(VALUE, long, long);
VALUE rb_str_equal(VALUE, VALUE);
int rb_str_cmp(VALUE, VALUE);
VALUE rb_str_hash(VALUE);
VALUE rb_str_inspect(VALUE);
VALUE rb_str_dump(VALUE);
VALUE rb_str_split(VALUE, const char *);
VALUE rb_str_to_str(VALUE);
VALUE rb_str_export(VALUE);
VALUE rb_str_export_locale(VALUE);
VALUE rb_sprintf(const char *, ...);
VALUE rb_vsprintf(const char *, va_list);
VALUE rb_str_catf(VALUE, const char *, ...);
VALUE rb_str_vcatf(VALUE, const char *, va_list);
VALUE rb_f_sprintf(int, const VALUE *);
VALUE rb_str_format(int, const VALUE *, VALUE);
long rb_str_offset(VALUE, long);
#define rb_str_new_frozen rb_str_new_frozen

/* ---- Arrays ---- */
long rb_array_len(VALUE);
#define RARRAY_LEN(a) rb_array_len((VALUE)(a))
#define RARRAY_LENINT(a) ((int)RARRAY_LEN(a))
#define RARRAY_AREF(a, i) rb_ary_entry((VALUE)(a), (long)(i))
#define RARRAY_ASET(a, i, v) rb_ary_store((VALUE)(a), (long)(i), (VALUE)(v))
/* An Array's elements as VALUEs: mruby's arrays hold mrb_values, so this
   is a snapshot of them, valid while the C method (or block) runs and the
   array is not changed through the API or by Ruby code it calls. Writes
   through RARRAY_PTR reach the array before Ruby code or the array API can
   see it, and when the method returns; RARRAY_PTR_USE writes them back
   when its expression is done. */
const VALUE *rb_array_const_ptr(VALUE);
VALUE *rb_capi_array_ptr(VALUE);
void rb_capi_array_ptr_sync(void);
#define RARRAY_CONST_PTR(a) rb_array_const_ptr((VALUE)(a))
#define RARRAY_CONST_PTR_TRANSIENT(a) RARRAY_CONST_PTR(a)
#define RARRAY_PTR(a) rb_capi_array_ptr((VALUE)(a))
#define RARRAY_PTR_USE(ary, ptr_name, expr) do { \
    VALUE *ptr_name = rb_capi_array_ptr((VALUE)(ary)); \
    expr; \
    rb_capi_array_ptr_sync(); \
  } while (0)
#define RARRAY_PTR_USE_TRANSIENT(ary, ptr_name, expr) RARRAY_PTR_USE(ary, ptr_name, expr)
#define rb_array_len rb_array_len
VALUE rb_ary_new(void);
VALUE rb_ary_new_capa(long);
VALUE rb_ary_new_from_args(long, ...);
VALUE rb_ary_new_from_values(long, const VALUE *);
#define rb_ary_new2 rb_ary_new_capa
#define rb_ary_new3 rb_ary_new_from_args
#define rb_ary_new4 rb_ary_new_from_values
VALUE rb_ary_tmp_new(long);
VALUE rb_assoc_new(VALUE, VALUE);
VALUE rb_ary_push(VALUE, VALUE);
VALUE rb_ary_pop(VALUE);
VALUE rb_ary_shift(VALUE);
VALUE rb_ary_unshift(VALUE, VALUE);
VALUE rb_ary_entry(VALUE, long);
void rb_ary_store(VALUE, long, VALUE);
VALUE rb_ary_dup(VALUE);
VALUE rb_ary_clear(VALUE);
VALUE rb_ary_concat(VALUE, VALUE);
VALUE rb_ary_plus(VALUE, VALUE);
VALUE rb_ary_join(VALUE, VALUE);
VALUE rb_ary_reverse(VALUE);
VALUE rb_ary_includes(VALUE, VALUE);
VALUE rb_ary_delete(VALUE, VALUE);
VALUE rb_ary_delete_at(VALUE, long);
VALUE rb_ary_subseq(VALUE, long, long);
VALUE rb_ary_aref(int, const VALUE *, VALUE);
VALUE rb_ary_to_ary(VALUE);
VALUE rb_ary_to_s(VALUE);
VALUE rb_ary_cat(VALUE, const VALUE *, long);
VALUE rb_ary_freeze(VALUE);
VALUE rb_ary_sort_bang(VALUE);
VALUE rb_ary_resize(VALUE, long);
VALUE rb_ary_last(int, const VALUE *, VALUE);
VALUE rb_ary_resurrect(VALUE);
VALUE rb_get_values_at(VALUE, long, int, const VALUE *, VALUE (*)(VALUE, long));

/* ---- Hashes ---- */
enum st_retval { ST_CONTINUE, ST_STOP, ST_DELETE, ST_CHECK, ST_REPLACE };
typedef uintptr_t st_data_t;
typedef st_data_t st_index_t;
/* MRI's hash function for extensions' own tables (not MRI's values: a
   hash is not stable across implementations, or across runs in MRI) */
st_index_t st_hash(const void *, size_t, st_index_t);
st_index_t st_hash_uint32(st_index_t, uint32_t);
st_index_t st_hash_uint(st_index_t, st_index_t);
st_index_t st_hash_end(st_index_t);
st_index_t st_hash_start(st_index_t);
VALUE rb_hash_new(void);
VALUE rb_hash_new_capa(long);
VALUE rb_hash_aset(VALUE, VALUE, VALUE);
VALUE rb_hash_aref(VALUE, VALUE);
VALUE rb_hash_lookup(VALUE, VALUE);
VALUE rb_hash_lookup2(VALUE, VALUE, VALUE);
VALUE rb_hash_fetch(VALUE, VALUE);
VALUE rb_hash_delete(VALUE, VALUE);
VALUE rb_hash_delete_if(VALUE);
VALUE rb_hash_clear(VALUE);
VALUE rb_hash_size(VALUE);
size_t rb_hash_size_num(VALUE);
#define RHASH_SIZE(h) rb_hash_size_num((VALUE)(h))
#define RHASH_EMPTY_P(h) (RHASH_SIZE(h) == 0)
VALUE rb_hash_keys(VALUE);
VALUE rb_hash_values(VALUE);
VALUE rb_hash_dup(VALUE);
VALUE rb_hash_freeze(VALUE);
VALUE rb_hash_set_ifnone(VALUE, VALUE);
VALUE rb_hash_update_by(VALUE, VALUE, VALUE (*)(VALUE, VALUE, VALUE));
void rb_hash_foreach(VALUE, int (*)(VALUE, VALUE, VALUE), VALUE);
void rb_hash_bulk_insert(long, const VALUE *, VALUE);
VALUE rb_env_clear(void);

/* ---- Ranges, Procs, misc objects ---- */
VALUE rb_range_new(VALUE, VALUE, int);
int rb_range_values(VALUE, VALUE *, VALUE *, int *);
VALUE rb_struct_define(const char *, ...);
VALUE rb_struct_new(VALUE, ...);
VALUE rb_struct_aref(VALUE, VALUE);
VALUE rb_struct_aset(VALUE, VALUE, VALUE);
VALUE rb_struct_getmember(VALUE, ID);
VALUE rb_time_new(time_t, long);
VALUE rb_time_nano_new(time_t, long);
VALUE rb_reg_new_str(VALUE, int);
VALUE rb_reg_nth_match(int, VALUE);
VALUE rb_backref_get(void);
VALUE rb_marshal_dump(VALUE, VALUE);
VALUE rb_marshal_load(VALUE);
VALUE rb_io_write(VALUE, VALUE);
VALUE rb_io_puts(int, const VALUE *, VALUE);
VALUE rb_p(VALUE);
RUBY_EXTERN VALUE rb_stdout, rb_stderr, rb_stdin, rb_fs, rb_output_fs, rb_rs, rb_default_rs, rb_output_rs;
void rb_ext_ractor_safe(bool);
#define RB_EXT_RACTOR_SAFE(f) rb_ext_ractor_safe(f)

/* ---- GC ---- */
void rb_gc_register_address(VALUE *);
void rb_global_variable(VALUE *);
void rb_gc_unregister_address(VALUE *);
void rb_gc_register_mark_object(VALUE);
void rb_gc_mark(VALUE);
void rb_gc_mark_maybe(VALUE);
void rb_gc_mark_movable(VALUE);
void rb_gc_mark_locations(const VALUE *, const VALUE *);
VALUE rb_gc_location(VALUE);
void rb_gc_writebarrier(VALUE, VALUE);
void rb_gc_writebarrier_unprotect(VALUE);
void rb_gc(void);
VALUE rb_gc_start(void);
VALUE rb_gc_enable(void);
VALUE rb_gc_disable(void);
void rb_gc_adjust_memory_usage(ssize_t);
#define RB_OBJ_WRITE(a, slot, b) rb_capi_obj_write((VALUE)(a), (VALUE *)(slot), (VALUE)(b))
#define RB_OBJ_WRITTEN(a, oldv, b) (rb_gc_writebarrier((VALUE)(a), (VALUE)(b)), (VALUE)(b))
static inline VALUE rb_capi_obj_write(VALUE a, VALUE *slot, VALUE b) { *slot = b; rb_gc_writebarrier(a, b); return a; }
#define RB_GC_GUARD(v) (*rb_capi_gc_guarded_ptr(&(v)))
static inline volatile VALUE *rb_capi_gc_guarded_ptr(volatile VALUE *p) { __asm__("" : : "m"(p) : "memory"); return p; }
#define RB_OBJ_WB_UNPROTECT(x) (x)
#define OBJ_WB_UNPROTECT(x) (x)

/* ---- Data and TypedData ---- */
typedef void (*RUBY_DATA_FUNC)(void *);
#define RUBY_DEFAULT_FREE ((RUBY_DATA_FUNC)-1)
#define RUBY_NEVER_FREE ((RUBY_DATA_FUNC)0)
#define RUBY_TYPED_DEFAULT_FREE RUBY_DEFAULT_FREE
#define RUBY_TYPED_NEVER_FREE RUBY_NEVER_FREE
#define RUBY_TYPED_FREE_IMMEDIATELY 1
#define RUBY_TYPED_FROZEN_SHAREABLE 0x100
#define RUBY_TYPED_WB_PROTECTED 0x20
#define RUBY_TYPED_EMBEDDABLE 2
#define RUBY_TYPED_DECL_MARKING 0x4000
#define RUBY_FL_WB_PROTECTED RUBY_TYPED_WB_PROTECTED
typedef struct rb_data_type_struct rb_data_type_t;
struct rb_data_type_struct {
  const char *wrap_struct_name;
  struct {
    RUBY_DATA_FUNC dmark;
    RUBY_DATA_FUNC dfree;
    size_t (*dsize)(const void *);
    RUBY_DATA_FUNC dcompact;
    void *reserved[1];
  } function;
  const rb_data_type_t *parent;
  void *data;
  VALUE flags;
};
VALUE rb_data_typed_object_wrap(VALUE, void *, const rb_data_type_t *);
VALUE rb_data_typed_object_zalloc(VALUE, size_t, const rb_data_type_t *);
VALUE rb_data_object_wrap(VALUE, void *, RUBY_DATA_FUNC, RUBY_DATA_FUNC);
VALUE rb_data_object_zalloc(VALUE, size_t, RUBY_DATA_FUNC, RUBY_DATA_FUNC);
void *rb_check_typeddata(VALUE, const rb_data_type_t *);
int rb_typeddata_is_kind_of(VALUE, const rb_data_type_t *);
int rb_typeddata_inherited_p(const rb_data_type_t *, const rb_data_type_t *);
void *rb_capi_data_get(VALUE);
void **rb_capi_data_ptr_ref(VALUE);
const rb_data_type_t *rb_capi_typeddata_type(VALUE);
int rb_capi_typeddata_p(VALUE);
#define DATA_PTR(obj) (*rb_capi_data_ptr_ref((VALUE)(obj)))
#define RDATA_PTR(obj) DATA_PTR(obj)
#define RTYPEDDATA_DATA(obj) DATA_PTR(obj)
#define RTYPEDDATA_GET_DATA(obj) rb_capi_data_get((VALUE)(obj))
#define RTYPEDDATA_TYPE(obj) rb_capi_typeddata_type((VALUE)(obj))
#define RTYPEDDATA_P(obj) rb_capi_typeddata_p((VALUE)(obj))
#define RB_TYPEDDATA_P(obj) (RB_TYPE_P(obj, T_DATA) && RTYPEDDATA_P(obj))
#define TypedData_Wrap_Struct(klass, data_type, sval) rb_data_typed_object_wrap((klass), (sval), (data_type))
#define TypedData_Make_Struct0(result, klass, type, size, data_type, sval) \
  VALUE result = rb_data_typed_object_zalloc((klass), (size), (data_type)); \
  (sval) = (type *)RTYPEDDATA_GET_DATA(result)
#define TypedData_Make_Struct(klass, type, data_type, sval) \
  RB_GNUC_EXTENSION({ VALUE rbimpl_result = rb_data_typed_object_zalloc((klass), sizeof(type), (data_type)); \
    (sval) = (type *)RTYPEDDATA_GET_DATA(rbimpl_result); rbimpl_result; })
#define TypedData_Get_Struct(obj, type, data_type, sval) \
  ((sval) = (type *)rb_check_typeddata((obj), (data_type)))
#define Data_Wrap_Struct(klass, mark, free, sval) \
  rb_data_object_wrap((klass), (sval), (RUBY_DATA_FUNC)(mark), (RUBY_DATA_FUNC)(free))
#define Data_Make_Struct(klass, type, mark, free, sval) \
  RB_GNUC_EXTENSION({ VALUE rbimpl_result = rb_data_object_zalloc((klass), sizeof(type), (RUBY_DATA_FUNC)(mark), (RUBY_DATA_FUNC)(free)); \
    (sval) = (type *)DATA_PTR(rbimpl_result); rbimpl_result; })
#define Data_Get_Struct(obj, type, sval) ((sval) = (type *)rb_capi_data_get((VALUE)(obj)))

/* ---- memory ---- */
void *ruby_xmalloc(size_t);
void *ruby_xmalloc2(size_t, size_t);
void *ruby_xcalloc(size_t, size_t);
void *ruby_xrealloc(void *, size_t);
void *ruby_xrealloc2(void *, size_t, size_t);
void ruby_xfree(void *);
#define xmalloc ruby_xmalloc
#define xmalloc2 ruby_xmalloc2
#define xcalloc ruby_xcalloc
#define xrealloc ruby_xrealloc
#define xrealloc2 ruby_xrealloc2
#define xfree ruby_xfree
#define ALLOC(type) ((type *)ruby_xmalloc(sizeof(type)))
#define ALLOC_N(type, n) ((type *)ruby_xmalloc2((n), sizeof(type)))
#define ZALLOC(type) ((type *)ruby_xcalloc(1, sizeof(type)))
#define ZALLOC_N(type, n) ((type *)ruby_xcalloc((n), sizeof(type)))
#define REALLOC_N(var, type, n) ((var) = (type *)ruby_xrealloc2((void *)(var), (n), sizeof(type)))
#define ALLOCA_N(type, n) ((type *)__builtin_alloca(sizeof(type) * (n)))
#define MEMZERO(p, type, n) memset((p), 0, sizeof(type) * (size_t)(n))
#define MEMCPY(p1, p2, type, n) memcpy((p1), (p2), sizeof(type) * (size_t)(n))
#define MEMMOVE(p1, p2, type, n) memmove((p1), (p2), sizeof(type) * (size_t)(n))
#define MEMCMP(p1, p2, type, n) memcmp((p1), (p2), sizeof(type) * (size_t)(n))
#define RB_ALLOCV_N(type, v, n) ((v) = 0, (type *)ruby_xmalloc2((n), sizeof(type)))
#define ALLOCV_N(type, v, n) RB_ALLOCV_N(type, v, n)
#define ALLOCV_END(v) ((void)(v))

/* ---- threads (oruby's run on one mrb_state: no lock to give up) ---- */
typedef void rb_unblock_function_t(void *);
#define RUBY_UBF_IO ((rb_unblock_function_t *)-1)
#define RUBY_UBF_PROCESS ((rb_unblock_function_t *)-1)
void *rb_thread_call_without_gvl(void *(*)(void *), void *, rb_unblock_function_t *, void *);
void *rb_thread_call_without_gvl2(void *(*)(void *), void *, rb_unblock_function_t *, void *);
void *rb_thread_call_with_gvl(void *(*)(void *), void *);
void *rb_nogvl(void *(*)(void *), void *, rb_unblock_function_t *, void *, int);
VALUE rb_thread_current(void);
void rb_thread_schedule(void);
void rb_thread_check_ints(void);
int rb_thread_interrupted(VALUE);

/* ---- what MRI's newer headers name with RB_ (ruby/internal/...) ---- */
#define RUBY_INTEGER_UNIFICATION 1
#define RB_ALLOC_N(type, n) ALLOC_N(type, n)
#define RB_ALLOC(type) ALLOC(type)
#define RB_ZALLOC_N(type, n) ZALLOC_N(type, n)
#define RB_ZALLOC(type) ZALLOC(type)
#define RB_REALLOC_N(var, type, n) REALLOC_N(var, type, n)
#define RB_INT2NUM(v) INT2NUM(v)
#define RB_NUM2INT(v) NUM2INT(v)
#define RB_UINT2NUM(v) UINT2NUM(v)
#define RB_NUM2UINT(v) NUM2UINT(v)
#define RB_LONG2NUM(v) LONG2NUM(v)
#define RB_NUM2LONG(v) NUM2LONG(v)
#define RB_ULONG2NUM(v) ULONG2NUM(v)
#define RB_NUM2ULONG(v) NUM2ULONG(v)
#define RB_LL2NUM(v) LL2NUM(v)
#define RB_ULL2NUM(v) ULL2NUM(v)
#define RB_NUM2LL(v) NUM2LL(v)
#define RB_NUM2ULL(v) NUM2ULL(v)
#define RB_DBL2NUM(v) DBL2NUM(v)
#define RB_NUM2DBL(v) NUM2DBL(v)
#define RB_OBJ_FROZEN_RAW(v) RB_OBJ_FROZEN(v)
#define OBJ_FROZEN_RAW(v) RB_OBJ_FROZEN(v)

/* the object's class, its singleton class if it has one (RBasic klass) */
VALUE rb_capi_basic_class(VALUE);
#define RBASIC_CLASS(obj) rb_capi_basic_class((VALUE)(obj))
VALUE rb_obj_hide(VALUE);
VALUE rb_obj_reveal(VALUE, VALUE);

/* Bignum: an Integer outside the Fixnum range, through its methods */
VALUE rb_big2str(VALUE, int);
long rb_big2long(VALUE);
unsigned long rb_big2ulong(VALUE);
long long rb_big2ll(VALUE);
unsigned long long rb_big2ull(VALUE);
double rb_big2dbl(VALUE);
size_t rb_absint_size(VALUE, int *);
int rb_big_sign(VALUE);
#define RBIGNUM_SIGN(b) rb_big_sign(b)
#define RBIGNUM_POSITIVE_P(b) (RBIGNUM_SIGN(b) == 1)
#define RBIGNUM_NEGATIVE_P(b) (RBIGNUM_SIGN(b) == 0)

VALUE rb_struct_size(VALUE);
VALUE rb_struct_members(VALUE);
VALUE rb_struct_s_members(VALUE);
#define RSTRUCT_LEN(st) NUM2LONG(rb_struct_size(st))
#define RSTRUCT_GET(st, i) rb_struct_aref((st), LONG2NUM(i))
#define RSTRUCT_SET(st, i, v) rb_struct_aset((st), LONG2NUM(i), (v))

int rb_obj_method_arity(VALUE, ID);
int rb_mod_method_arity(VALUE, ID);
int rb_proc_arity(VALUE);
void rb_ivar_foreach(VALUE, int (*)(ID, VALUE, st_data_t), st_data_t);
st_index_t rb_ivar_count(VALUE);
st_index_t rb_memhash(const void *, long);

struct timespec rb_time_timespec(VALUE);
struct timeval rb_time_timeval(VALUE);
VALUE rb_time_timespec_new(const struct timespec *, int);
VALUE rb_rational_new(VALUE, VALUE);
VALUE rb_complex_new(VALUE, VALUE);
#define rb_rational_new1(x) rb_rational_new((x), INT2FIX(1))
#define rb_rational_new2(x, y) rb_rational_new((x), (y))
#define rb_complex_new1(x) rb_complex_new((x), INT2FIX(0))
#define rb_complex_new2(x, y) rb_complex_new((x), (y))

struct rb_capi_encoding; /* rb_encoding, ruby/encoding.h */
VALUE rb_check_symbol_cstr(const char *, long, struct rb_capi_encoding *);
VALUE rb_cstr_to_inum(const char *, int, int);
VALUE rb_str_to_inum(VALUE, int, int);
double rb_cstr_to_dbl(const char *, int);
double rb_str_to_dbl(VALUE, int);
RUBY_EXTERN VALUE rb_eSysStackError;
VALUE rb_apply(VALUE, ID, VALUE);
VALUE rb_catch_obj(VALUE, rb_block_call_func_t, VALUE);
VALUE rb_catch(const char *, rb_block_call_func_t, VALUE);
NORETURN(void rb_throw_obj(VALUE, VALUE));
NORETURN(void rb_throw(const char *, VALUE));

/* MRI's ASCII-only character classes (ruby/internal/ctype.h) */
static inline int rb_isascii(int c) { return '\0' <= c && c <= '\x7f'; }
static inline int rb_isupper(int c) { return 'A' <= c && c <= 'Z'; }
static inline int rb_islower(int c) { return 'a' <= c && c <= 'z'; }
static inline int rb_isalpha(int c) { return rb_isupper(c) || rb_islower(c); }
static inline int rb_isdigit(int c) { return '0' <= c && c <= '9'; }
static inline int rb_isalnum(int c) { return rb_isalpha(c) || rb_isdigit(c); }
static inline int rb_isxdigit(int c) { return rb_isdigit(c) || ('A' <= c && c <= 'F') || ('a' <= c && c <= 'f'); }
static inline int rb_isblank(int c) { return c == ' ' || c == '\t'; }
static inline int rb_isspace(int c) { return c == ' ' || ('\t' <= c && c <= '\r'); }
static inline int rb_iscntrl(int c) { return ('\0' <= c && c < ' ') || c == '\x7f'; }
static inline int rb_isprint(int c) { return ' ' <= c && c <= '\x7e'; }
static inline int rb_isgraph(int c) { return '!' <= c && c <= '\x7e'; }
static inline int rb_ispunct(int c) { return rb_isgraph(c) && !rb_isalnum(c); }
static inline int rb_tolower(int c) { return rb_isupper(c) ? (c | 0x20) : c; }
static inline int rb_toupper(int c) { return rb_islower(c) ? (c & ~0x20) : c; }
#ifndef ISDIGIT
#define ISASCII(c) rb_isascii(c)
#define ISPRINT(c) rb_isprint(c)
#define ISGRAPH(c) rb_isgraph(c)
#define ISSPACE(c) rb_isspace(c)
#define ISUPPER(c) rb_isupper(c)
#define ISLOWER(c) rb_islower(c)
#define ISALNUM(c) rb_isalnum(c)
#define ISALPHA(c) rb_isalpha(c)
#define ISDIGIT(c) rb_isdigit(c)
#define ISXDIGIT(c) rb_isxdigit(c)
#define ISBLANK(c) rb_isblank(c)
#define ISCNTRL(c) rb_iscntrl(c)
#define ISPUNCT(c) rb_ispunct(c)
#define TOUPPER(c) rb_toupper(c)
#define TOLOWER(c) rb_tolower(c)
#endif
#ifndef UNREACHABLE
#define UNREACHABLE __builtin_unreachable()
#endif
#define UNREACHABLE_RETURN(val) UNREACHABLE

VALUE rb_str_replace(VALUE, VALUE);
void rb_str_update(VALUE, long, long, VALUE);
VALUE rb_io_flush(VALUE);

#if defined(__cplusplus)
}

/* C++ (MRI's ruby/backward/cxxanyargs.hpp): a method function of any
   signature, without the cast to ANYARGS that C++ would otherwise need, and
   the ANYARGS casts older extensions make for the typed callbacks */
extern "C++" {
#define ORUBY_CAPI_CXX_DEFINE(name, params, args)                          \
  template <typename R, typename... A>                                     \
  inline void name params {                                                \
    name args;                                                             \
  }
#define ORUBY_CAPI_CXX_FN reinterpret_cast<rb_capi_anyargs_func>(func)
ORUBY_CAPI_CXX_DEFINE(rb_define_method, (VALUE klass, const char *mid, R (*func)(A...), int arity), (klass, mid, ORUBY_CAPI_CXX_FN, arity))
ORUBY_CAPI_CXX_DEFINE(rb_define_method_id, (VALUE klass, ID mid, R (*func)(A...), int arity), (klass, mid, ORUBY_CAPI_CXX_FN, arity))
ORUBY_CAPI_CXX_DEFINE(rb_define_private_method, (VALUE klass, const char *mid, R (*func)(A...), int arity), (klass, mid, ORUBY_CAPI_CXX_FN, arity))
ORUBY_CAPI_CXX_DEFINE(rb_define_protected_method, (VALUE klass, const char *mid, R (*func)(A...), int arity), (klass, mid, ORUBY_CAPI_CXX_FN, arity))
ORUBY_CAPI_CXX_DEFINE(rb_define_singleton_method, (VALUE obj, const char *mid, R (*func)(A...), int arity), (obj, mid, ORUBY_CAPI_CXX_FN, arity))
ORUBY_CAPI_CXX_DEFINE(rb_define_module_function, (VALUE mod, const char *mid, R (*func)(A...), int arity), (mod, mid, ORUBY_CAPI_CXX_FN, arity))
ORUBY_CAPI_CXX_DEFINE(rb_define_global_function, (const char *mid, R (*func)(A...), int arity), (mid, ORUBY_CAPI_CXX_FN, arity))
#undef ORUBY_CAPI_CXX_FN
#undef ORUBY_CAPI_CXX_DEFINE

typedef VALUE (*rb_capi_cxx_value_func)(VALUE);
typedef VALUE (*rb_capi_cxx_rescue_func)(VALUE, VALUE);
inline VALUE rb_ensure(VALUE (*b)(...), VALUE d1, VALUE (*e)(...), VALUE d2) {
  return rb_ensure(reinterpret_cast<rb_capi_cxx_value_func>(b), d1, reinterpret_cast<rb_capi_cxx_value_func>(e), d2);
}
inline VALUE rb_rescue(VALUE (*b)(...), VALUE d1, VALUE (*r)(...), VALUE d2) {
  return rb_rescue(reinterpret_cast<rb_capi_cxx_value_func>(b), d1, reinterpret_cast<rb_capi_cxx_rescue_func>(r), d2);
}
inline VALUE rb_protect(VALUE (*b)(...), VALUE d, int *state) {
  return rb_protect(reinterpret_cast<rb_capi_cxx_value_func>(b), d, state);
}
inline void rb_hash_foreach(VALUE h, int (*f)(...), VALUE arg) {
  rb_hash_foreach(h, reinterpret_cast<int (*)(VALUE, VALUE, VALUE)>(f), arg);
}
inline VALUE rb_block_call(VALUE obj, ID mid, int argc, const VALUE *argv, VALUE (*f)(...), VALUE data) {
  return rb_block_call(obj, mid, argc, argv, reinterpret_cast<rb_block_call_func_t>(f), data);
}
inline VALUE rb_proc_new(VALUE (*f)(...), VALUE data) {
  return rb_proc_new(reinterpret_cast<rb_block_call_func_t>(f), data);
}
}
#endif

#include "ruby/encoding.h"

/* Quick-win C-API additions */
static inline int rb_type_p(VALUE obj, int type) { return rb_type(obj) == type; }
VALUE rb_class_get_superclass(VALUE klass);
int rb_is_const_id(ID id);
int rb_is_instance_id(ID id);
int rb_is_class_id(ID id);

VALUE rb_syserr_new(int n, const char *mesg);
VALUE rb_syserr_new_str(int n, VALUE arg);
void rb_syserr_fail(int n, const char *mesg);
void rb_syserr_fail_str(int n, VALUE mesg);
VALUE rb_make_exception(int argc, const VALUE *argv);

#define TIMET2NUM(v) LL2NUM((long long)(v))
#define NUM2TIMET(v) ((time_t)NUM2LL(v))
#define NUM2PIDT(v) ((pid_t)NUM2LL(v))
#define PIDT2NUM(v) LL2NUM((long long)(v))
int rb_long2int(long n);

typedef VALUE rb_enumerator_size_func(VALUE, VALUE, VALUE);
VALUE rb_enumeratorize_with_size(VALUE obj, VALUE meth, int argc, const VALUE *argv, rb_enumerator_size_func *size_fn);

VALUE rb_define_finalizer(VALUE obj, VALUE block);
VALUE rb_undefine_finalizer(VALUE obj);

void rb_mem_clear(VALUE *mem, long size);
VALUE rb_ary_rotate(VALUE ary, long cnt);
VALUE rb_ary_sort(VALUE ary);

VALUE rb_get_path(VALUE obj);
#define FilePathValue(v) ((v) = rb_get_path(v))
VALUE rb_file_open(const char *fname, const char *modestr);
VALUE rb_file_open_str(VALUE fname, const char *modestr);

VALUE rb_Hash(VALUE val);
VALUE rb_ident_hash_new(void);
#define rb_hash_start st_hash_start
#define rb_hash_uint32 st_hash_uint32
#define rb_hash_uint st_hash_uint
#define rb_hash_end st_hash_end

void rb_alias(VALUE klass, ID new_name, ID old_name);
void rb_undef(VALUE klass, ID name);
VALUE rb_mod_ancestors(VALUE mod);
void rb_deprecate_constant(VALUE mod, const char *name);

VALUE rb_Rational(VALUE num, VALUE den);
VALUE rb_Rational1(VALUE num);
VALUE rb_Rational2(VALUE num, VALUE den);
VALUE rb_rational_num(VALUE rat);
VALUE rb_rational_den(VALUE rat);
VALUE rb_Complex(VALUE real, VALUE imag);
VALUE rb_Complex1(VALUE real);
VALUE rb_Complex2(VALUE real, VALUE imag);
VALUE rb_complex_real(VALUE cmp);
VALUE rb_complex_imag(VALUE cmp);

VALUE rb_mutex_new(void);
VALUE rb_mutex_locked_p(VALUE mutex);
VALUE rb_mutex_trylock(VALUE mutex);
VALUE rb_mutex_lock(VALUE mutex);
VALUE rb_mutex_unlock(VALUE mutex);
VALUE rb_mutex_sleep(VALUE mutex, VALUE timeout);
VALUE rb_mutex_synchronize(VALUE mutex, VALUE (*func)(VALUE arg), VALUE arg);
VALUE rb_proc_call_kw(VALUE proc, VALUE args, int kw_splat);
VALUE rb_proc_call_with_block_kw(VALUE proc, int argc, const VALUE *argv, VALUE blk, int kw_splat);

#endif /* ORUBY_CAPI_RUBY_H */
