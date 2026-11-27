//go:build unix

package env

/*
#include <stdlib.h>

extern char **environ;

static char **env_entries(int *count) {
	int n = 0;
	if (environ) while (environ[n]) n++;
	*count = n;
	return environ;
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

	entries := envEntries()
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = C.GoString(entry)
	}
	return out
}

// envEntries is a view of libc's environ, valid while envMu is held and no
// environment mutation is in progress.
func envEntries() []*C.char {
	var n C.int
	entries := C.env_entries(&n)
	return unsafe.Slice(entries, int(n))
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

	entries := envEntries()
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		s := C.GoString(entry)
		if eq := strings.IndexByte(s, '='); eq > 0 {
			keys = append(keys, s[:eq])
		}
	}
	for _, k := range keys {
		ck := C.CString(k)
		C.unsetenv(ck)
		C.free(unsafe.Pointer(ck))
	}
	os.Clearenv()
}
