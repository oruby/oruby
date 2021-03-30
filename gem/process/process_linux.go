package process

import (
	"golang.org/x/sys/unix"
	"syscall"
	"unsafe"

	"github.com/oruby/oruby"
)

func initPlatformUnix(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {
	mrb.DefineModuleFunc(mProc, "getsid", unix.Getsid)
	// mrb.DefineModuleFunc(mProc, "euid=", unix.Seteuid)
	// mrb.DefineModuleFunc(mProc, "egid=", unix.Setegid)
	// mrb.DefineModuleFunc(mSys, "issetugid", syscall.Issetugid)
}

func prLimit(pid int, limit uintptr, rlimit *syscall.Rlimit) (err error) {
	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRLIMIT64, uintptr(pid), limit, uintptr(unsafe.Pointer(rlimit)), 0, 0, 0)
	if errno != 0 {
		err = errno
	}
	return err
}
