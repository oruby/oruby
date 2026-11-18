package file

import (
	"errors"
	"os"
	"syscall"

	"github.com/oruby/oruby"
)

func raiseDirErrno(mrb *oruby.MrbState, err error, op string) oruby.MrbValue {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		sce := mrb.ClassGet("SystemCallError")
		if exc, e := mrb.ObjNew(sce, op, int(errno)); e == nil {
			mrb.ExcRaise(exc.Value())
			return mrb.NilValue()
		}
	}
	return mrb.SysFail(err)
}

func dirChroot(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	dir, err := pathString(mrb, mrb.GetArgsFirst())
	if err != nil {
		return raisePathError(mrb, err)
	}
	err = syscall.Chroot(dir)
	if err != nil {
		return raiseDirErrno(mrb, err, "chroot")
	}
	return oruby.Int(0)
}

func dirSysRmdir(path string) error {
	return syscall.Rmdir(path)
}

func dirFileno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f, err := dirFile(mrb, self)
	if err != nil {
		return raiseDirError(mrb, err)
	}
	return mrb.Value(int(f.Fd()))
}

func dirForFd(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fdVal := mrb.GetArgsFirst()
	fd, err := toInt(mrb, fdVal)
	if err != nil {
		return raisePathError(mrb, err)
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return raiseDirErrno(mrb, err, "for_fd")
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return raiseDirErrno(mrb, syscall.ENOTDIR, "for_fd")
	}
	f := os.NewFile(uintptr(fd), "")
	cls := mrb.ClassPtr(self)
	obj := mrb.ObjAlloc(oruby.MrbTTCData, cls)
	mrb.SetIV(obj, "@path", mrb.NilValue())
	mrb.SetIV(obj, "@pos", 0)
	mrb.SetIV(obj, "@closed", false)
	mrb.DataSetInterface(obj.Value(), f)
	return obj
}

func dirFchdirFd(mrb *oruby.MrbState, fd int, block oruby.RProc) oruby.MrbValue {
	if block.IsNil() {
		if err := syscall.Fchdir(fd); err != nil {
			return raiseDirErrno(mrb, err, "fchdir")
		}
		return oruby.Int(0)
	}
	origFd, err := syscall.Open(".", syscall.O_RDONLY, 0)
	if err != nil {
		return raiseDirErrno(mrb, err, "fchdir")
	}
	defer syscall.Close(origFd)
	if err := syscall.Fchdir(fd); err != nil {
		return raiseDirErrno(mrb, err, "fchdir")
	}
	res := mrb.YieldArgv(block)
	_ = syscall.Fchdir(origFd)
	return res
}

func dirSFchdir(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, block := mrb.GetArgsWithBlock()
	fd, err := toInt(mrb, args.Item(0))
	if err != nil {
		return raisePathError(mrb, err)
	}
	return dirFchdirFd(mrb, fd, block)
}

func dirChdirInst(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	f, err := dirFile(mrb, self)
	if err != nil {
		return raiseDirError(mrb, err)
	}
	return dirFchdirFd(mrb, int(f.Fd()), block)
}
