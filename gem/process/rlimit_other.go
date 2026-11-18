//go:build !windows && !darwin && !linux

package process

import "syscall"

var rlimitResources = map[string]int{
	"AS": syscall.RLIMIT_AS, "CORE": syscall.RLIMIT_CORE, "CPU": syscall.RLIMIT_CPU, "DATA": syscall.RLIMIT_DATA,
	"FSIZE": syscall.RLIMIT_FSIZE, "NOFILE": syscall.RLIMIT_NOFILE, "STACK": syscall.RLIMIT_STACK,
}
