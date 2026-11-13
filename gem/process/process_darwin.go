package process

import (
	"syscall"

	"github.com/oruby/oruby"
	"golang.org/x/sys/unix"
)

func initPlatformUnix(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {
	mrb.DefineModuleFunc(mProc, "getsid", syscall.Getsid)
	mrb.DefineModuleFunc(mProc, "euid=", unix.Seteuid)
	mrb.DefineModuleFunc(mProc, "egid=", unix.Setegid)
	mrb.DefineModuleFunc(mSys, "issetugid", syscall.Issetugid)
}
