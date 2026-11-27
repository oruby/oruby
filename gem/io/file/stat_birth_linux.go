package file

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// birthInfo is a FileInfo that remembers how to ask statx(2) for the birth
// time, which the stat(2) result lacks on Linux.
type birthInfo struct {
	os.FileInfo
	path  string
	flags int
}

// withBirth wraps the result of stat/lstat on path
func withBirth(fi os.FileInfo, err error, path string, nofollow bool) (os.FileInfo, error) {
	if err != nil || fi == nil || path == "" {
		return fi, err
	}
	flags := 0
	if nofollow {
		flags = unix.AT_SYMLINK_NOFOLLOW
	}
	return &birthInfo{FileInfo: fi, path: path, flags: flags}, nil
}

// birthtimeOf is the birth time, zero when the file system keeps none
func birthtimeOf(stat os.FileInfo, ext extendedStat) time.Time {
	b, ok := stat.(*birthInfo)
	if !ok {
		return ext.birthtime
	}
	var sx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, b.path, b.flags|unix.AT_STATX_SYNC_AS_STAT, unix.STATX_BTIME, &sx); err != nil ||
		sx.Mask&unix.STATX_BTIME == 0 {
		return time.Time{}
	}
	return time.Unix(sx.Btime.Sec, int64(sx.Btime.Nsec))
}

// bareInfo is the FileInfo os.SameFile and the like accept
func bareInfo(fi os.FileInfo) os.FileInfo {
	if b, ok := fi.(*birthInfo); ok {
		return b.FileInfo
	}
	return fi
}
