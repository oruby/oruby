package process

import (
	"syscall"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"golang.org/x/sys/unix"
)

// RLIM_INFINITY is RLIM_INFINITY as the rlim_t it is (2**63-1)
const RLIM_INFINITY = uint64(syscall.RLIM_INFINITY)

func initPlatformUnix(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {
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
		sid, err := syscall.Getsid(pid)
		if err != nil {
			return mrb.SysFail(err)
		}
		return mrb.FixnumValue(sid)
	}, mrb.ArgsOpt(1))
	mrb.DefineModuleFunction(mProc, "euid=", setUIDFunc(unix.Seteuid), mrb.ArgsReq(1))
	mrb.DefineModuleFunction(mProc, "egid=", setGIDFunc(unix.Setegid), mrb.ArgsReq(1))
	mrb.DefineModuleFunc(mSys, "issetugid", syscall.Issetugid)
}
