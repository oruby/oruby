package file

import (
	"os"

	"github.com/oruby/oruby"
)

func raiseDirErrno(mrb *oruby.MrbState, err error, op string) oruby.MrbValue {
	return mrb.SysFail(err)
}

func dirChroot(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb, self)
}

func dirSysRmdir(path string) error {
	return os.Remove(path)
}

func dirFileno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb, self)
}

func dirForFd(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb, self)
}

func dirSFchdir(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb, self)
}

func dirChdirInst(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb, self)
}
