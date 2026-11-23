package file

import (
	"os"
	"syscall"
	"time"
)

func getExtendedStat(stat os.FileInfo) extendedStat {
	s, ok := stat.Sys().(*syscall.Stat_t)
	if !ok || s == nil {
		mode := uint32(stat.Mode().Perm())
		if stat.IsDir() {
			mode |= syscall.S_IFDIR
		} else {
			mode |= syscall.S_IFREG
		}
		return extendedStat{
			mode:  mode,
			nlink: 1,
			uid:   os.Geteuid(),
			gid:   os.Getegid(),
			atime: stat.ModTime(),
			ctime: stat.ModTime(),
		}
	}
	return extendedStat{
		ino:     s.Ino,
		dev:     uint64(s.Dev),
		mode:    uint32(s.Mode),
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
