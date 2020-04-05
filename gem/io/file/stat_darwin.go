package file

import (
	"os"
	"syscall"
	"time"
)

func getExtendedStat(stat os.FileInfo) extendedStat {
	s := stat.Sys().(*syscall.Stat_t)
	return extendedStat{
		ino:       s.Ino,
		dev:       uint64(s.Dev),
		nlink:     uint64(s.Nlink),
		uid:       int(s.Uid),
		gid:       int(s.Gid),
		rdev:      uint64(s.Rdev),
		blksize:   int64(s.Blksize),
		blocks:    s.Blocks,
		atime:     time.Unix(s.Atimespec.Unix()),
		ctime:     time.Unix(s.Ctimespec.Unix()),
		birthtime: time.Unix(s.Birthtimespec.Unix()),
	}
}