//go:build !windows

package core_ext

/*
#include <stdlib.h>
#include <string.h>
#include <locale.h>
#include <langinfo.h>
#if defined(__APPLE__) || defined(__FreeBSD__)
#include <xlocale.h>
#endif

// the character set libc names for the LC_CTYPE locale `name` ("" is the
// environment's), as nl_langinfo(CODESET) after setlocale(LC_CTYPE, name) would
// give, without touching the process-wide locale. A locale libc does not have
// leaves setlocale at "C", so it is "C" here too.
static char *orb_locale_charmap(const char *name) {
	locale_t loc = newlocale(LC_CTYPE_MASK, name, (locale_t)0);
	if (loc == (locale_t)0) {
		loc = newlocale(LC_CTYPE_MASK, "C", (locale_t)0);
		if (loc == (locale_t)0) return NULL;
	}
	const char *cs = nl_langinfo_l(CODESET, loc);
	char *r = cs ? strdup(cs) : NULL;
	freelocale(loc);
	return r;
}
*/
import "C"

import (
	"unsafe"

	"github.com/oruby/oruby"
)

// Kernel.__oruby_locale_charmap(locale_name) -> String, nil when libc has none
func localeCharmap(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	name := C.CString(mrb.String(mrb.GetArgsFirst()))
	defer C.free(unsafe.Pointer(name))
	cs := C.orb_locale_charmap(name)
	if cs == nil {
		return mrb.NilValue()
	}
	defer C.free(unsafe.Pointer(cs))
	return mrb.StrNew(C.GoString(cs))
}

func initLocale(mrb *oruby.MrbState) {
	mrb.DefineModuleFunction(mrb.KernelModule(), "__oruby_locale_charmap", localeCharmap, mrb.ArgsReq(1))
}
