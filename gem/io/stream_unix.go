//go:build !windows

package io

/*
#define _GNU_SOURCE // SEEK_DATA, SEEK_HOLE in glibc
#include <unistd.h>
#include <fcntl.h>

// fcntl is variadic: cgo calls it through these
static int oruby_fcntl_int(int fd, int cmd, long arg) { return fcntl(fd, cmd, arg); }
static int oruby_getfd(int fd) { return fcntl(fd, F_GETFD); }
static int oruby_setfd(int fd, int flags) { return fcntl(fd, F_SETFD, flags); }
*/
import "C"
import (
	"os"
	"syscall"
)

const (
	seekData = int(C.SEEK_DATA)
	seekHole = int(C.SEEK_HOLE)
)

func platformDup(f *os.File) (*os.File, error) {
	fd, err := syscall.Dup(RawFd(f))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

func platformIsatty(f *os.File) bool {
	return C.isatty(C.int(RawFd(f))) != 0
}

func closeOnExec(f *os.File) (bool, error) {
	r, err := C.oruby_getfd(C.int(RawFd(f)))
	if r < 0 {
		return false, err
	}
	return r&C.FD_CLOEXEC != 0, nil
}

func setCloseOnExec(f *os.File, on bool) error {
	fd := C.int(RawFd(f))
	r, err := C.oruby_getfd(fd)
	if r < 0 {
		return err
	}
	if on {
		r |= C.FD_CLOEXEC
	} else {
		r &^= C.FD_CLOEXEC
	}
	if s, err := C.oruby_setfd(fd, r); s < 0 {
		return err
	}
	return nil
}

// platformFcntl is fcntl(fd, cmd, arg) with an integer argument
func platformFcntl(f *os.File, cmd, arg int) (int, error) {
	r, err := C.oruby_fcntl_int(C.int(RawFd(f)), C.int(cmd), C.long(arg))
	if r < 0 {
		return 0, err
	}
	return int(r), nil
}

// rawWrite is one write(2) on fd
func rawWrite(fd uintptr, p []byte) (int, error) {
	n, err := syscall.Write(int(fd), p)
	if n < 0 {
		n = 0
	}
	return n, err
}

// fdFlags is fcntl(fd, F_GETFL); ok false for a descriptor not open
func fdFlags(fd int) (int, bool) {
	r, _ := C.oruby_fcntl_int(C.int(fd), C.F_GETFL, 0)
	return int(r), r >= 0
}
