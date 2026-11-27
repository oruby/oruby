//go:build !unix

package env

import "os"

func sysEnviron() []string                   { return os.Environ() }
func sysLookupEnv(key string) (string, bool) { return os.LookupEnv(key) }
func sysSetenv(key, val string) error        { return os.Setenv(key, val) }
func sysUnsetenv(key string) error           { return os.Unsetenv(key) }
func sysClearenv()                           { os.Clearenv() }
