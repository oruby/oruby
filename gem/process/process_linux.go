package process

import (
	"syscall"
	"unsafe"

	"github.com/oruby/oruby"
)

func initPlatform(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {

}

func prLimit(pid int, limit uintptr, rlimit *syscall.Rlimit) (err error) {
	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRLIMIT64, uintptr(pid), limit, uintptr(unsafe.Pointer(rlimit)), 0, 0, 0)
	if errno != 0 {
		err = errno
	}
	return err
}
