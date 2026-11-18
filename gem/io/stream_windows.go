package io

import (
	"errors"
	"os"
	"syscall"
)

const (
	seekData = 3
	seekHole = 4
)

func platformDup(f *os.File) (*os.File, error) {
	return nil, errors.New("dup() not supported on Windows")
}

func platformIsatty(f *os.File) bool {
	return false
}

func closeOnExec(f *os.File) (bool, error) { return true, nil }

func setCloseOnExec(f *os.File, on bool) error { return nil }

func platformFcntl(f *os.File, cmd, arg int) (int, error) {
	return 0, errors.New("fcntl() function is unimplemented on this machine")
}

// rawWrite is one WriteFile on fd
func rawWrite(fd uintptr, p []byte) (int, error) {
	return syscall.Write(syscall.Handle(fd), p)
}

func fdFlags(fd int) (int, bool) { return 0, false }
