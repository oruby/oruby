//go:build windows

package pty

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("pty is not supported on Windows")

func openPTY() (*os.File, *os.File, error) {
	return nil, nil, errUnsupported
}

func startPTY(cmd *exec.Cmd) (*os.File, *os.File, int, error) {
	return nil, nil, 0, errUnsupported
}
