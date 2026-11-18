//go:build !windows

package file

import (
	"os"
	"syscall"
)

// fileID identifies a directory (device and inode) for the cycle check of
// "***/"
type fileID struct{ dev, ino uint64 }

func statID(path string) (fileID, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileID{}, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, false
	}
	return fileID{uint64(st.Dev), uint64(st.Ino)}, true
}
