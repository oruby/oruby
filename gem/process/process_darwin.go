package process

import (
	"github.com/oruby/oruby"
)

func initPlatformUnix(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {
	mrb.DefineModuleFunc(mProc, "getsid", syscall.Getsid)
	mrb.DefineModuleFunc(mProc, "euid=", unix.Seteuid)
	mrb.DefineModuleFunc(mProc, "egid=", unix.Setegid)
	mrb.DefineModuleFunc(mSys, "issetugid", syscall.Issetugid)
}
