package io

import (
	"os"
	"syscall"
	"time"
)

func getExtendedStat(stat os.FileInfo) extendedStat {
	s := stat.Sys().(*syscall.Stat_t)
	return extendedStat{
		ino:     s.Ino,
		dev:     uint64(s.Dev),
		nlink:   uint64(s.Nlink),
		uid:     int(s.Uid),
		gid:     int(s.Gid),
		rdev:    uint64(s.Rdev),
		blksize: int64(s.Blksize),
		blocks:  s.Blocks,
		atime:   time.Unix(s.Atim.Unix()),
		ctime:   time.Unix(s.Ctim.Unix()),
	}
}