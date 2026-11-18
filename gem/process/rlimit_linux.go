package process

import "golang.org/x/sys/unix"

// rlimitResources: the RLIMIT_ names MRI defines on Linux
var rlimitResources = map[string]int{
	"AS": unix.RLIMIT_AS, "CORE": unix.RLIMIT_CORE, "CPU": unix.RLIMIT_CPU, "DATA": unix.RLIMIT_DATA,
	"FSIZE": unix.RLIMIT_FSIZE, "MEMLOCK": unix.RLIMIT_MEMLOCK, "MSGQUEUE": 12, "NICE": 13,
	"NOFILE": unix.RLIMIT_NOFILE, "NPROC": unix.RLIMIT_NPROC, "RSS": unix.RLIMIT_RSS, "RTPRIO": 14,
	"RTTIME": 15, "SIGPENDING": 11, "STACK": unix.RLIMIT_STACK,
}
