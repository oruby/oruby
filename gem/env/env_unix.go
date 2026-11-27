//go:build unix

package env

/*
#include <stdlib.h>

extern char **environ;

static int env_count(void) {
	if (!environ) return 0;
	int n = 0;
	while (environ[n]) n++;
	return n;
}

static const char *env_at(int i) {
	return environ[i];
}
*/
import "C"

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var envMu sync.RWMutex

func sysEnviron() []string {
	envMu.RLock()
	defer envMu.RUnlock()

	n := int(C.env_count())
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if p := C.env_at(C.int(i)); p != nil {
			out = append(out, C.GoString(p))
		}
	}
	return out
}

func sysLookupEnv(key string) (string, bool) {
	if key == "" || strings.IndexByte(key, 0) >= 0 {
		return "", false
	}
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))

	envMu.RLock()
	defer envMu.RUnlock()

	v := C.getenv(ck)
	if v == nil {
		return "", false
	}
	return C.GoString(v), true
}

func sysSetenv(key, val string) error {
	if key == "" || strings.IndexByte(key, '=') >= 0 || strings.IndexByte(key, 0) >= 0 || strings.IndexByte(val, 0) >= 0 {
		return syscall.EINVAL
	}
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	cv := C.CString(val)
	defer C.free(unsafe.Pointer(cv))

	envMu.Lock()
	defer envMu.Unlock()

	if _, err := C.setenv(ck, cv, 1); err != nil {
		return err
	}
	return os.Setenv(key, val)
}

func sysUnsetenv(key string) error {
	if key == "" || strings.IndexByte(key, '=') >= 0 || strings.IndexByte(key, 0) >= 0 {
		return syscall.EINVAL
	}
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))

	envMu.Lock()
	defer envMu.Unlock()

	if _, err := C.unsetenv(ck); err != nil {
		return err
	}
	return os.Unsetenv(key)
}

func sysClearenv() {
	envMu.Lock()
	defer envMu.Unlock()

	n := int(C.env_count())
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if p := C.env_at(C.int(i)); p != nil {
			s := C.GoString(p)
			if eq := strings.IndexByte(s, '='); eq > 0 {
				keys = append(keys, s[:eq])
			}
		}
	}
	for _, k := range keys {
		ck := C.CString(k)
		C.unsetenv(ck)
		C.free(unsafe.Pointer(ck))
	}
	os.Clearenv()
}
