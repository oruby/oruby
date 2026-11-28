//go:build darwin || linux

package oruby

import (
	"os"

	"golang.org/x/sys/unix"
)

func mapOrbFile(file *os.File, size int) ([]byte, func(), error) {
	data, err := unix.Mmap(int(file.Fd()), 0, size, unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, nil, err
	}
	return data, func() { _ = unix.Munmap(data) }, nil
}
