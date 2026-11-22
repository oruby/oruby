//go:build !windows

package console

/*
#include <unistd.h>
#include <termios.h>
*/
import "C"

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

type termMode struct {
	termios unix.Termios
}

func (m *termMode) clone() *termMode {
	cp := *m
	return &cp
}

func (m *termMode) echo() bool {
	return m.termios.Lflag&unix.ECHO != 0
}

func (m *termMode) setEcho(on bool) {
	if on {
		m.termios.Lflag |= unix.ECHO
	} else {
		m.termios.Lflag &^= unix.ECHO
	}
}

func (m *termMode) setRaw(min, timeVal int, intr bool) {
	// POSIX cfmakeraw equivalent
	m.termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	m.termios.Oflag &^= unix.OPOST
	m.termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.IEXTEN
	if !intr {
		m.termios.Lflag &^= unix.ISIG
	} else {
		m.termios.Lflag |= unix.ISIG
	}
	m.termios.Cflag &^= unix.CSIZE | unix.PARENB
	m.termios.Cflag |= unix.CS8
	if min < 0 {
		min = 1
	}
	if timeVal < 0 {
		timeVal = 0
	}
	m.termios.Cc[unix.VMIN] = uint8(min)
	m.termios.Cc[unix.VTIME] = uint8(timeVal)
}

func (m *termMode) setCooked() {
	m.termios.Iflag |= unix.BRKINT | unix.ISTRIP | unix.ICRNL | unix.IXON
	m.termios.Oflag |= unix.OPOST
	m.termios.Lflag |= unix.ECHO | unix.ECHOE | unix.ECHOK | unix.ICANON | unix.ISIG | unix.IEXTEN
}

func (m *termMode) min() int {
	return int(m.termios.Cc[unix.VMIN])
}

func (m *termMode) setMin(v int) {
	m.termios.Cc[unix.VMIN] = uint8(v)
}

func (m *termMode) time() int {
	return int(m.termios.Cc[unix.VTIME])
}

func (m *termMode) setTime(v int) {
	m.termios.Cc[unix.VTIME] = uint8(v)
}

func getTermMode(fd int) (*termMode, error) {
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	return &termMode{termios: *t}, nil
}

func setTermMode(fd int, m *termMode) error {
	t := m.termios
	return unix.IoctlSetTermios(fd, ioctlSetTermios, &t)
}

func getWinsize(fd int) (int, int, error) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Row), int(ws.Col), nil
}

func setWinsize(fd int, rows, cols int) error {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		ws = &unix.Winsize{}
	}
	ws.Row = uint16(rows)
	ws.Col = uint16(cols)
	return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, ws)
}

func flushConsole(fd int, which int) error {
	var q C.int
	switch which {
	case flushInput:
		q = C.TCIFLUSH
	case flushOutput:
		q = C.TCOFLUSH
	default:
		q = C.TCIOFLUSH
	}
	if _, err := C.tcflush(C.int(fd), q); err != nil {
		return err
	}
	return nil
}

func ttyName(fd int) (string, error) {
	var buf [256]byte
	rc := C.ttyname_r(C.int(fd), (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
	if rc != 0 {
		return "", unix.Errno(rc)
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i]), nil
		}
	}
	return string(buf[:]), nil
}
