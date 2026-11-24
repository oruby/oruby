package stdlib

/*
#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>
#include <limits.h>
#include <float.h>
#include <signal.h>
#include <time.h>
#include <wchar.h>
#include <wctype.h>
#include <complex.h>
#include <sys/types.h>

// rbconfig/sizeof: RbConfig::SIZEOF and RbConfig::LIMITS as MRI's
// configure finds them, from this platform's headers (char and wchar_t
// signedness, long double differ between platforms)
struct oruby_limit { const char *name; int kind; long long i; unsigned long long u; double d; };
enum { LIM_I, LIM_U, LIM_D };

#define S(t) { #t, (int)sizeof(t) }
static const struct { const char *name; int size; } oruby_sizeof[] = {
  S(int), S(short), S(long), {"long long", (int)sizeof(long long)},
#ifdef __SIZEOF_INT128__
  S(__int128),
#endif
  S(off_t), {"void*", (int)sizeof(void *)}, S(float), S(double), S(time_t), S(clock_t),
  S(size_t), S(ptrdiff_t), S(dev_t), S(int8_t), S(uint8_t), S(int16_t), S(uint16_t),
  S(int32_t), S(uint32_t), S(int64_t), S(uint64_t),
#ifdef __SIZEOF_INT128__
  {"int128_t", (int)sizeof(__int128)}, {"uint128_t", (int)sizeof(unsigned __int128)},
#endif
  S(intptr_t), S(uintptr_t), S(ssize_t), S(int_least8_t), S(int_least16_t),
  S(int_least32_t), S(int_least64_t), S(int_fast8_t), S(int_fast16_t), S(int_fast32_t),
  S(int_fast64_t), S(intmax_t), S(sig_atomic_t), S(wchar_t), S(wint_t), S(wctrans_t),
  S(wctype_t), S(_Bool), {"long double", (int)sizeof(long double)},
  {"float _Complex", (int)sizeof(float _Complex)}, {"double _Complex", (int)sizeof(double _Complex)},
  {"long double _Complex", (int)sizeof(long double _Complex)}, {"rb_len_t", (int)sizeof(long)},
};
#undef S
static int oruby_sizeof_n(void) { return (int)(sizeof(oruby_sizeof) / sizeof(oruby_sizeof[0])); }
static const char *oruby_sizeof_name(int i) { return oruby_sizeof[i].name; }
static int oruby_sizeof_size(int i) { return oruby_sizeof[i].size; }

static struct oruby_limit oruby_limits[128];
static int oruby_nlimits;
static void lim_i(const char *n, long long v) { oruby_limits[oruby_nlimits++] = (struct oruby_limit){n, LIM_I, v, 0, 0}; }
static void lim_u(const char *n, unsigned long long v) { oruby_limits[oruby_nlimits++] = (struct oruby_limit){n, LIM_U, 0, v, 0}; }
static void lim_d(const char *n, double v) { oruby_limits[oruby_nlimits++] = (struct oruby_limit){n, LIM_D, 0, 0, v}; }
#define LIM_SIGNED(m) lim_i(#m, (long long)(m))
#define LIM_UNSIGNED(m) lim_u(#m, (unsigned long long)(m))
#define LIM_EITHER(m, signed_p) do { if (signed_p) lim_i(#m, (long long)(m)); else lim_u(#m, (unsigned long long)(m)); } while (0)
static int oruby_limits_init(void) {
  oruby_nlimits = 0;
  LIM_EITHER(CHAR_MAX, CHAR_MIN < 0); LIM_SIGNED(CHAR_MIN); LIM_SIGNED(SCHAR_MAX); LIM_SIGNED(SCHAR_MIN); LIM_UNSIGNED(UCHAR_MAX);
  LIM_EITHER(WCHAR_MAX, WCHAR_MIN < 0); LIM_SIGNED(WCHAR_MIN); LIM_SIGNED(SHRT_MAX); LIM_SIGNED(SHRT_MIN); LIM_UNSIGNED(USHRT_MAX);
  LIM_SIGNED(INT_MAX); LIM_SIGNED(INT_MIN); LIM_UNSIGNED(UINT_MAX); LIM_SIGNED(LONG_MAX); LIM_SIGNED(LONG_MIN); LIM_UNSIGNED(ULONG_MAX);
  LIM_SIGNED(LLONG_MAX); LIM_SIGNED(LLONG_MIN); LIM_UNSIGNED(ULLONG_MAX);
  LIM_SIGNED(INT8_MAX); LIM_SIGNED(INT8_MIN); LIM_UNSIGNED(UINT8_MAX); LIM_SIGNED(INT_LEAST8_MAX); LIM_SIGNED(INT_LEAST8_MIN); LIM_UNSIGNED(UINT_LEAST8_MAX);
  LIM_SIGNED(INT_FAST8_MAX); LIM_SIGNED(INT_FAST8_MIN); LIM_UNSIGNED(UINT_FAST8_MAX);
  LIM_SIGNED(INT16_MAX); LIM_SIGNED(INT16_MIN); LIM_UNSIGNED(UINT16_MAX); LIM_SIGNED(INT_LEAST16_MAX); LIM_SIGNED(INT_LEAST16_MIN); LIM_UNSIGNED(UINT_LEAST16_MAX);
  LIM_SIGNED(INT_FAST16_MAX); LIM_SIGNED(INT_FAST16_MIN); LIM_UNSIGNED(UINT_FAST16_MAX);
  LIM_SIGNED(INT32_MAX); LIM_SIGNED(INT32_MIN); LIM_UNSIGNED(UINT32_MAX); LIM_SIGNED(INT_LEAST32_MAX); LIM_SIGNED(INT_LEAST32_MIN); LIM_UNSIGNED(UINT_LEAST32_MAX);
  LIM_SIGNED(INT_FAST32_MAX); LIM_SIGNED(INT_FAST32_MIN); LIM_UNSIGNED(UINT_FAST32_MAX);
  LIM_SIGNED(INT64_MAX); LIM_SIGNED(INT64_MIN); LIM_UNSIGNED(UINT64_MAX); LIM_SIGNED(INT_LEAST64_MAX); LIM_SIGNED(INT_LEAST64_MIN); LIM_UNSIGNED(UINT_LEAST64_MAX);
  LIM_SIGNED(INT_FAST64_MAX); LIM_SIGNED(INT_FAST64_MIN); LIM_UNSIGNED(UINT_FAST64_MAX);
  LIM_SIGNED(INTMAX_MAX); LIM_SIGNED(INTMAX_MIN); LIM_UNSIGNED(UINTMAX_MAX); LIM_SIGNED(INTPTR_MAX); LIM_SIGNED(INTPTR_MIN); LIM_UNSIGNED(UINTPTR_MAX);
  LIM_UNSIGNED(SIZE_MAX); LIM_SIGNED(PTRDIFF_MAX); LIM_SIGNED(PTRDIFF_MIN);
  LIM_SIGNED(FLT_RADIX); LIM_SIGNED(FLT_ROUNDS); LIM_SIGNED(FLT_EVAL_METHOD); LIM_SIGNED(FLT_MANT_DIG); LIM_SIGNED(DBL_MANT_DIG); LIM_SIGNED(LDBL_MANT_DIG);
  LIM_SIGNED(FLT_DIG); LIM_SIGNED(DBL_DIG); LIM_SIGNED(LDBL_DIG); LIM_SIGNED(FLT_MIN_EXP); LIM_SIGNED(DBL_MIN_EXP); LIM_SIGNED(LDBL_MIN_EXP);
  LIM_SIGNED(FLT_MIN_10_EXP); LIM_SIGNED(DBL_MIN_10_EXP); LIM_SIGNED(LDBL_MIN_10_EXP); LIM_SIGNED(FLT_MAX_EXP); LIM_SIGNED(DBL_MAX_EXP); LIM_SIGNED(LDBL_MAX_EXP);
  LIM_SIGNED(FLT_MAX_10_EXP); LIM_SIGNED(DBL_MAX_10_EXP); LIM_SIGNED(LDBL_MAX_10_EXP);
#ifdef FLT_DECIMAL_DIG
  LIM_SIGNED(FLT_DECIMAL_DIG); LIM_SIGNED(DBL_DECIMAL_DIG); LIM_SIGNED(LDBL_DECIMAL_DIG);
#endif
  LIM_SIGNED(DECIMAL_DIG);
#ifdef FLT_HAS_SUBNORM
  LIM_SIGNED(FLT_HAS_SUBNORM); LIM_SIGNED(DBL_HAS_SUBNORM); LIM_SIGNED(LDBL_HAS_SUBNORM);
#endif
  lim_d("FLT_MAX", FLT_MAX); lim_d("DBL_MAX", DBL_MAX); lim_d("FLT_EPSILON", FLT_EPSILON);
  lim_d("DBL_EPSILON", DBL_EPSILON); lim_d("FLT_MIN", FLT_MIN); lim_d("DBL_MIN", DBL_MIN);
#ifdef FLT_TRUE_MIN
  lim_d("FLT_TRUE_MIN", FLT_TRUE_MIN); lim_d("DBL_TRUE_MIN", DBL_TRUE_MIN);
#endif
  return oruby_nlimits;
}
#undef LIM_SIGNED
#undef LIM_UNSIGNED
#undef LIM_EITHER
static struct oruby_limit *oruby_limit_at(int i) { return &oruby_limits[i]; }
*/
import "C"

import (
	"fmt"
	"strconv"
	"strings"
)

// sizeofSource is rbconfig/sizeof's Ruby: the tables of this platform.
// FIXNUM_MAX/MIN are oruby's (a Fixnum is a 64-bit integer here).
func sizeofSource() string {
	var b strings.Builder
	b.WriteString("module RbConfig\n  SIZEOF = {\n")
	for i := 0; i < int(C.oruby_sizeof_n()); i++ {
		fmt.Fprintf(&b, "    %q => %d,\n", C.GoString(C.oruby_sizeof_name(C.int(i))), int(C.oruby_sizeof_size(C.int(i))))
	}
	b.WriteString("  }.freeze\n  LIMITS = {\n    \"FIXNUM_MAX\" => 9223372036854775807, \"FIXNUM_MIN\" => -9223372036854775808,\n")
	for i := 0; i < int(C.oruby_limits_init()); i++ {
		l := C.oruby_limit_at(C.int(i))
		var v string
		switch l.kind {
		case C.LIM_I:
			v = strconv.FormatInt(int64(l.i), 10)
		case C.LIM_U:
			v = strconv.FormatUint(uint64(l.u), 10)
		default:
			v = strconv.FormatFloat(float64(l.d), 'g', -1, 64)
			if !strings.ContainsAny(v, ".e") {
				v += ".0"
			}
		}
		fmt.Fprintf(&b, "    %q => %s,\n", C.GoString(l.name), v)
	}
	b.WriteString("  }.freeze\nend\n")
	return b.String()
}
