package io

import (
	"github.com/oruby/oruby"
	"os"
)


func fileUmask(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	umask := mrb.GetArgs().ItemDefInt(0, 0)
	return oruby.Integer(0)
}

func fileFlock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NotImplemented(mrb, self)
}

func platformDup(f *os.File) (*os.File, error) {
	name := f.Name()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	flags := os.O_RDONLY
	statT := stat.Sys().(*syscall.Win32FileAttributeData)
	if statT != nil {
		m := int(statT.FileAttributes)
		if m & syscall.FILE_ATTRIBUTE_READONLY != 0 {
			flags = os.O_RDONLY
		} else {
			flags = os.O_RDWR
		}
	}

	return os.OpenFile(name, flags, os.modePerm)
}
