package file

import (
	"github.com/oruby/oruby"
	"os"
	"syscall"
)

func dirChroot(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir := mrb.GetArgsFirst().String()
	err := syscall.Chroot(dir)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return oruby.Int(0)
}

func dirFileno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	return mrb.Value(int(f.Fd()))
}