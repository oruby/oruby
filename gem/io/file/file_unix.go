package file

import (
	"errors"
	"github.com/oruby/oruby"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func fileUmask(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) == 0 {
		old := syscall.Umask(0)
		syscall.Umask(old)
		return oruby.Integer(old)
	}
	if len(args) > 1 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 0..1)", len(args))
	}
	arg := args[0]
	if mrb.ClassOf(arg).Name() == "Bignum" {
		return mrb.Raise(mrb.ClassGet("RangeError"), "bignum too big to convert into 'int'")
	}
	var newMask int
	if arg.IsInt() {
		newMask = arg.Int()
	} else if mrb.RespondTo(arg, mrb.Intern("to_int")) {
		v, err := mrb.Funcall(arg, mrb.Intern("to_int"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		newMask = v.Int()
	} else {
		return mrb.Raisef(mrb.ETypeError(), "no implicit conversion of %s into Integer", mrb.ClassOf(arg).Name())
	}
	old := syscall.Umask(newMask)
	return oruby.Integer(old)
}

func fileFlock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	lockType := mrb.GetArgsFirst().Int()

	if err := syscall.Flock(int(f.Fd()), lockType); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return oruby.False
		}
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
}

func fileMkfifio(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	nameV, modeV := mrb.GetArgs2("", 0666)
	name, err := pathString(mrb, nameV)
	if err != nil {
		return raisePathError(mrb, err)
	}
	mode := 0666
	if !modeV.IsNil() && modeV.IsInteger() {
		mode = modeV.Int()
	}
	if err := syscall.Mkfifo(name, uint32(mode)); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int(0)
}

func platformDup(f *os.File) (*os.File, error) {
	if fd, err := syscall.Dup(int(f.Fd())); err == nil {
		return os.NewFile(uintptr(fd), f.Name()), nil
	}

	name := f.Name()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	ext := getExtendedStat(stat)
	perm := stat.Mode().Perm()
	uid := os.Geteuid()
	flags := os.O_RDONLY

	if uid == 0 && (perm&0111 != 0) {
		flags = os.O_RDWR
	} else if uid == ext.uid && (perm&0101 != 0) {
		flags = os.O_RDWR
	} else if os.Getegid() == ext.gid && (perm&0011 != 0) {
		flags = os.O_RDWR
	}

	return os.OpenFile(name, flags, perm)
}

func platformOpenFile(name string, mode int, perm os.FileMode) (int, error) {
	return syscall.Open(name, mode|syscall.O_CLOEXEC, uint32(perm))
}

func fileLutime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 2 {
		return mrb.Raisef(mrb.EArgumentError(), "wrong number of arguments (given %d, expected 2+)", len(args))
	}
	atime, err := parseTimeArg(mrb, args[0])
	if err != nil {
		return raisePathError(mrb, err)
	}
	mtime, err := parseTimeArg(mrb, args[1])
	if err != nil {
		return raisePathError(mrb, err)
	}
	paths := args[2:]
	for _, p := range paths {
		name, err := pathString(mrb, p)
		if err != nil {
			return raisePathError(mrb, err)
		}
		tv := []unix.Timeval{
			unix.NsecToTimeval(atime.UnixNano()),
			unix.NsecToTimeval(mtime.UnixNano()),
		}
		if err := unix.Lutimes(name, tv); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(len(paths))
}

func fileLchmod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs().Slice()
	if len(args) < 1 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1+)")
	}
	modeInt, err := parseMode(mrb, args[0])
	if err != nil {
		return mrb.RaiseError(err)
	}
	for _, pathArg := range args[1:] {
		p, err := pathString(mrb, pathArg)
		if err != nil {
			return raisePathError(mrb, err)
		}
		if err := unix.Fchmodat(unix.AT_FDCWD, p, uint32(modeInt), unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return mrb.SysFail(err)
		}
	}
	return oruby.Integer(len(args) - 1)
}

func checkFdMode(fd int, flags int) error {
	curFlags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err == nil {
		curAcc := curFlags & (syscall.O_RDONLY | syscall.O_WRONLY | syscall.O_RDWR)
		newAcc := flags & (syscall.O_RDONLY | syscall.O_WRONLY | syscall.O_RDWR)
		if curAcc != newAcc {
			return syscall.EINVAL
		}
	}
	return nil
}
