package process

import "golang.org/x/sys/unix"

// rlimitResources: the RLIMIT_ names MRI defines on macOS
var rlimitResources = map[string]int{
	"AS": unix.RLIMIT_AS, "CORE": unix.RLIMIT_CORE, "CPU": unix.RLIMIT_CPU, "DATA": unix.RLIMIT_DATA,
	"FSIZE": unix.RLIMIT_FSIZE, "MEMLOCK": 6, "NOFILE": unix.RLIMIT_NOFILE, "NPROC": 7, "RSS": 5,
	"STACK": unix.RLIMIT_STACK,
}
