//go:build !windows

package oruby

import "syscall"

func goRuntimeSignal(sig syscall.Signal) bool { return sig == syscall.SIGURG }
