//go:build !windows

package pty

import (
	"os"
	"os/exec"
	"syscall"

	creackpty "github.com/creack/pty"
)

func openPTY() (*os.File, *os.File, error) {
	return creackpty.Open()
}

func startPTY(cmd *exec.Cmd) (*os.File, *os.File, int, error) {
	ptmx, err := creackpty.Start(cmd)
	if err != nil {
		return nil, nil, 0, err
	}
	dupFD, err := syscall.Dup(int(ptmx.Fd()))
	if err != nil {
		_ = ptmx.Close()
		return nil, nil, 0, err
	}
	syscall.CloseOnExec(dupFD)
	writer := os.NewFile(uintptr(dupFD), ptmx.Name())
	return ptmx, writer, cmd.Process.Pid, nil
}
