package io

import (
	"github.com/oruby/oruby"
	"os"
	"syscall"
)

func fileUmask(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	umask := mrb.GetArgs().ItemDefInt(0, -1)
	if umask < 0 {
		umask = syscall.Umask(0)
	}
	return oruby.Integer(syscall.Umask(umask))
}

func fileFlock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	f := mrb.Data(self).(*os.File)
	lockType := mrb.GetArgsFirst().Int()

	if err := syscall.Flock(int(f.Fd()), lockType); err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Integer(0)
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
	uid := os.Getuid()
	flags := os.O_RDONLY

	if uid == 0 && (perm|0111 != 0) {
		flags = os.O_RDWR
	} else  if uid == ext.uid  && (perm|0101 != 0) {
		flags = os.O_RDWR
	} else if os.Getgid() == ext.gid && (perm|0011 != 0) {
		flags = os.O_RDWR
	}

	return os.OpenFile(name, flags, perm)
}

func platformOpenFile(name string, mode int, perm os.FileMode) (int, error) {
	return syscall.Open(name, mode|syscall.O_CLOEXEC, uint32(perm))
}