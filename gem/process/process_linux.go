package process

import (
	"syscall"
	"unsafe"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	"golang.org/x/sys/unix"
)

// RLIM_INFINITY is RLIM_INFINITY as the rlim_t it is: syscall's is -1 on
// Linux (2**64-1, as MRI shows it)
const RLIM_INFINITY = ^uint64(0)

func initPlatformUnix(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {
	// Process.getsid(pid = 0)
	mrb.DefineModuleFunction(mProc, "getsid", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		pid := 0
		if len(a) > 0 && !a[0].IsNil() {
			v, err := rb.Int(mrb, a[0])
			if err != nil {
				return rb.TypeError(mrb, err)
			}
			pid = int(v)
		}
		sid, err := unix.Getsid(pid)
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.FixnumValue(sid)
	}, mrb.ArgsOpt(1))
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

// niceFromSys: the getpriority system call returns 20 - nice (glibc undoes it)
func niceFromSys(v int) int { return 20 - v }

// Process.uid= / gid= change the real id only (MRI: setresuid(id, -1, -1)), so
// they work after Process.euid= has dropped the privileges to the same id
func setRealUID(id int) error { return syscall.Setresuid(id, -1, -1) }

func setRealGID(id int) error { return syscall.Setresgid(id, -1, -1) }
