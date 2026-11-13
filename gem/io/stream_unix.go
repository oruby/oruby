package io

// #include <unistd.h>
import "C"
import (
	"os"
	"syscall"
)

func platformDup(f *os.File) (*os.File, error) {
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

func platformIsatty(f *os.File) bool {
	return C.isatty(C.int(f.Fd())) != 0
}
