package file

import (
	"github.com/oruby/oruby"
	"golang.org/x/sys/unix"
	"os"
)

func statIsExecutableReal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	uid := os.Getuid()

	if uid == 0 { // root: any execute bit (MRI's rb_stat_x)
		return oruby.Bool(stat.Mode().Perm()&0111 != 0)
	}

	perm := stat.Mode().Perm()

	if uid == ext.uid {
		return oruby.Bool((perm&0100 != 0))
	}
	if os.Getgid() == ext.gid {
		return oruby.Bool((perm&0010 != 0))
	}

	return oruby.Bool(perm&0001 != 0)
}

func statIsExecutable(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	euid := os.Geteuid()

	if euid == 0 { // root: any execute bit (MRI's rb_stat_x)
		return oruby.Bool(stat.Mode().Perm()&0111 != 0)
	}

	perm := stat.Mode().Perm()

	if euid == ext.uid {
		return oruby.Bool((perm&0100 != 0))
	}
	if os.Getegid() == ext.gid {
		return oruby.Bool((perm&0010 != 0))
	}

	return oruby.Bool(perm&0001 != 0)
}

func statIsGrpowned(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.gid == os.Getegid() {
		return oruby.True
	}
	groups, err := os.Getgroups()
	if err == nil {
		for _, g := range groups {
			if g == ext.gid {
				return oruby.True
			}
		}
	}
	return oruby.False
}

func major(dev uint64) uint32 {
	return unix.Major(dev)
}

func minor(dev uint64) uint32 {
	return unix.Minor(dev)
}
