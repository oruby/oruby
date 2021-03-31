package file

import (
	"github.com/oruby/oruby"
	"os"
	"syscall"
	"time"
)

func getExtendedStat(stat os.FileInfo) extendedStat {
	s := stat.Sys().(*syscall.Win32FileAttributeData)
	return extendedStat{
		ino:       0,
		dev:       0x2,
		nlink:     1,
		uid:       0,
		gid:       0,
		rdev:      0x2,
		blksize:   0,
		blocks:    0,
		atime:     time.Since(time.Unix(0, stat.LastAccessTime.Nanoseconds())),
		ctime:     time.Since(time.Unix(0, stat.CreationTime.Nanoseconds())),
	}
}

func statIsExecutable(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&0111 != 0)
}

func statIsExecutableReal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&0111 != 0)
}

func statIsGrpowned(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return oruby.False
}

func major(dev uint64) uint32 {
	return 0
}

func minor(dev uint64) uint32 {
	return 0
}
