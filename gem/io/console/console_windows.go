//go:build windows

package console

import (
	"syscall"

	"golang.org/x/sys/windows"
)

type termMode struct {
	mode uint32
	minV int
	timV int
}

func (m *termMode) clone() *termMode {
	cp := *m
	return &cp
}

func (m *termMode) echo() bool {
	return m.mode&windows.ENABLE_ECHO_INPUT != 0
}

func (m *termMode) setEcho(on bool) {
	if on {
		m.mode |= windows.ENABLE_ECHO_INPUT
	} else {
		m.mode &^= windows.ENABLE_ECHO_INPUT
	}
}

func (m *termMode) setRaw(min, timeVal int, intr bool) {
	m.mode &^= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT
	if !intr {
		m.mode &^= windows.ENABLE_PROCESSED_INPUT
	} else {
		m.mode |= windows.ENABLE_PROCESSED_INPUT
	}
	if min >= 0 {
		m.minV = min
	}
	if timeVal >= 0 {
		m.timV = timeVal
	}
}

func (m *termMode) setCooked() {
	m.mode |= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT
}

func (m *termMode) min() int      { return m.minV }
func (m *termMode) setMin(v int)  { m.minV = v }
func (m *termMode) time() int     { return m.timV }
func (m *termMode) setTime(v int) { m.timV = v }

func getTermMode(fd int) (*termMode, error) {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &mode); err != nil {
		return nil, err
	}
	return &termMode{mode: mode, minV: 1}, nil
}

func setTermMode(fd int, m *termMode) error {
	return windows.SetConsoleMode(windows.Handle(fd), m.mode)
}

func getWinsize(fd int) (int, int, error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(fd), &info); err != nil {
		return 0, 0, err
	}
	rows := int(info.Window.Bottom-info.Window.Top) + 1
	cols := int(info.Window.Right-info.Window.Left) + 1
	return rows, cols, nil
}

func setWinsize(fd int, rows, cols int) error {
	return syscall.ENOTSUP
}

func flushConsole(fd int, which int) error {
	return nil
}

func ttyName(fd int) (string, error) {
	return "CON", nil
}
