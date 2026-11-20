//go:build darwin || linux || freebsd

// Package etc implements Ruby's etc library (passwd/group database,
// sysconf, confstr, uname) using libc via cgo.
package etc

/*
#include <unistd.h>
#include <pwd.h>
#include <grp.h>
#include <errno.h>
#include <stdlib.h>
#include <sys/utsname.h>

static long go_sysconf(int name, int *err) {
	errno = 0;
	long v = sysconf(name);
	*err = errno;
	return v;
}
*/
import "C"

import (
	_ "embed"
	"errors"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed etc.rb
var etcRb []byte

// libc passwd/group iteration is not reentrant
var dbMu sync.Mutex

func init() {
	oruby.LazyGem("etc", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Etc")
		for name, v := range sysconfNames {
			mod.Const(name, v)
		}
		for name, v := range confstrNames {
			mod.Const(name, v)
		}
		def := func(name string, f oruby.MrbFuncT, aspec oruby.MrbAspec) { mod.DefineClassMethod(name, f, aspec) }
		def("__getpwnam", getpwnam, mrb.ArgsReq(1))
		def("__getpwuid", getpwuid, mrb.ArgsReq(1))
		def("__getgrnam", getgrnam, mrb.ArgsReq(1))
		def("__getgrgid", getgrgid, mrb.ArgsReq(1))
		def("__getpwent", getpwent, mrb.ArgsNone())
		def("__getgrent", getgrent, mrb.ArgsNone())
		def("setpwent", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			dbMu.Lock()
			C.setpwent()
			dbMu.Unlock()
			return mrb.NilValue()
		}, mrb.ArgsNone())
		def("endpwent", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			dbMu.Lock()
			C.endpwent()
			dbMu.Unlock()
			return mrb.NilValue()
		}, mrb.ArgsNone())
		def("setgrent", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			dbMu.Lock()
			C.setgrent()
			dbMu.Unlock()
			return mrb.NilValue()
		}, mrb.ArgsNone())
		def("endgrent", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			dbMu.Lock()
			C.endgrent()
			dbMu.Unlock()
			return mrb.NilValue()
		}, mrb.ArgsNone())
		def("__getlogin", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			if p := C.getlogin(); p != nil {
				return mrb.StrNew(C.GoString(p))
			}
			if u := os.Getenv("USER"); u != "" {
				return mrb.StrNew(u)
			}
			return mrb.NilValue()
		}, mrb.ArgsNone())
		def("nprocessors", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return mrb.FixnumValue(runtime.NumCPU())
		}, mrb.ArgsNone())
		def("sysconf", sysconf, mrb.ArgsReq(1))
		def("confstr", confstr, mrb.ArgsReq(1))
		def("uname", uname, mrb.ArgsNone())
		def("systmpdir", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return mrb.StrNew(systmpdir())
		}, mrb.ArgsNone())
		rb.LoadRuby(mrb, "etc.rb", etcRb)
		return nil
	})
}

func cstr(p *C.char) interface{} {
	if p == nil {
		return nil
	}
	return C.GoString(p)
}

func pwValue(mrb *oruby.MrbState, pw *C.struct_passwd) oruby.MrbValue {
	if pw == nil {
		return mrb.NilValue()
	}
	return mrb.Value([]interface{}{cstr(pw.pw_name), cstr(pw.pw_passwd), int(pw.pw_uid), int(pw.pw_gid),
		cstr(pw.pw_gecos), cstr(pw.pw_dir), cstr(pw.pw_shell), pwExtra(pw)})
}

func grValue(mrb *oruby.MrbState, gr *C.struct_group) oruby.MrbValue {
	if gr == nil {
		return mrb.NilValue()
	}
	var mem []interface{}
	if gr.gr_mem != nil {
		ptrs := (*[1 << 20]*C.char)(unsafe.Pointer(gr.gr_mem))
		for i := 0; ptrs[i] != nil; i++ {
			mem = append(mem, C.GoString(ptrs[i]))
		}
	}
	if mem == nil {
		mem = []interface{}{}
	}
	return mrb.Value([]interface{}{cstr(gr.gr_name), cstr(gr.gr_passwd), int(gr.gr_gid), mem})
}

func getpwnam(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	name, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	dbMu.Lock()
	defer dbMu.Unlock()
	return pwValue(mrb, C.getpwnam(cs))
}

func getpwuid(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	uid, err := rb.Int(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	return pwValue(mrb, C.getpwuid(C.uid_t(uid)))
}

func getgrnam(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	name, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	dbMu.Lock()
	defer dbMu.Unlock()
	return grValue(mrb, C.getgrnam(cs))
}

func getgrgid(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	gid, err := rb.Int(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	return grValue(mrb, C.getgrgid(C.gid_t(gid)))
}

func getpwent(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	dbMu.Lock()
	defer dbMu.Unlock()
	return pwValue(mrb, C.getpwent())
}

func getgrent(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	dbMu.Lock()
	defer dbMu.Unlock()
	return grValue(mrb, C.getgrent())
}

func sysconf(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	n, err := rb.Int(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	var e C.int
	v := C.go_sysconf(C.int(n), &e)
	if v == -1 {
		if e != 0 {
			return rb.Raise(mrb, "Errno::EINVAL", "Invalid argument - sysconf")
		}
		return mrb.NilValue()
	}
	return mrb.FixnumValue(int(v))
}

func confstr(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	n, err := rb.Int(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	size, cerr := C.confstr(C.int(n), nil, 0)
	if size == 0 {
		// an unknown name is EINVAL; a known one without a value is nil
		if errors.Is(cerr, syscall.EINVAL) {
			return mrb.RaiseError(syscall.EINVAL)
		}
		return mrb.NilValue()
	}
	buf := (*C.char)(C.malloc(size))
	defer C.free(unsafe.Pointer(buf))
	C.confstr(C.int(n), buf, size)
	return mrb.StrNew(C.GoString(buf))
}

func uname(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	var u C.struct_utsname
	if C.uname(&u) != 0 {
		return rb.Raise(mrb, "SystemCallError", "uname failed")
	}
	h := mrb.HashNew()
	set := func(k string, p *C.char) {
		h.Set(mrb.SymbolValue(mrb.Intern(k)), mrb.StrNew(C.GoString(p)))
	}
	set("sysname", &u.sysname[0])
	set("nodename", &u.nodename[0])
	set("release", &u.release[0])
	set("version", &u.version[0])
	set("machine", &u.machine[0])
	return h
}

func systmpdir() string {
	if runtime.GOOS == "darwin" {
		size := C.confstr(C._CS_DARWIN_USER_TEMP_DIR, nil, 0)
		if size > 0 {
			buf := (*C.char)(C.malloc(size))
			defer C.free(unsafe.Pointer(buf))
			C.confstr(C._CS_DARWIN_USER_TEMP_DIR, buf, size)
			if s := C.GoString(buf); s != "" {
				if len(s) > 1 && s[len(s)-1] == '/' {
					s = s[:len(s)-1]
				}
				return s
			}
		}
	}
	return "/tmp"
}

var sysconfNames = map[string]int{
	"SC_ARG_MAX": C._SC_ARG_MAX, "SC_CHILD_MAX": C._SC_CHILD_MAX, "SC_HOST_NAME_MAX": C._SC_HOST_NAME_MAX,
	"SC_LOGIN_NAME_MAX": C._SC_LOGIN_NAME_MAX, "SC_NGROUPS_MAX": C._SC_NGROUPS_MAX, "SC_CLK_TCK": C._SC_CLK_TCK,
	"SC_OPEN_MAX": C._SC_OPEN_MAX, "SC_PAGESIZE": C._SC_PAGESIZE, "SC_PAGE_SIZE": C._SC_PAGESIZE,
	"SC_RE_DUP_MAX": C._SC_RE_DUP_MAX, "SC_STREAM_MAX": C._SC_STREAM_MAX, "SC_SYMLOOP_MAX": C._SC_SYMLOOP_MAX,
	"SC_TTY_NAME_MAX": C._SC_TTY_NAME_MAX, "SC_TZNAME_MAX": C._SC_TZNAME_MAX, "SC_VERSION": C._SC_VERSION,
	"SC_NPROCESSORS_CONF": C._SC_NPROCESSORS_CONF, "SC_NPROCESSORS_ONLN": C._SC_NPROCESSORS_ONLN,
	"SC_PHYS_PAGES": C._SC_PHYS_PAGES, "SC_LINE_MAX": C._SC_LINE_MAX, "SC_JOB_CONTROL": C._SC_JOB_CONTROL,
	"SC_SAVED_IDS": C._SC_SAVED_IDS, "SC_BC_BASE_MAX": C._SC_BC_BASE_MAX, "SC_BC_DIM_MAX": C._SC_BC_DIM_MAX,
	"SC_BC_SCALE_MAX": C._SC_BC_SCALE_MAX, "SC_BC_STRING_MAX": C._SC_BC_STRING_MAX,
	"SC_COLL_WEIGHTS_MAX": C._SC_COLL_WEIGHTS_MAX, "SC_EXPR_NEST_MAX": C._SC_EXPR_NEST_MAX,
	"SC_IOV_MAX": C._SC_IOV_MAX, "SC_GETPW_R_SIZE_MAX": C._SC_GETPW_R_SIZE_MAX,
	"SC_GETGR_R_SIZE_MAX": C._SC_GETGR_R_SIZE_MAX,
}

var confstrNames = map[string]int{
	"CS_PATH": C._CS_PATH,
}
