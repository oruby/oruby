package oruby

import "syscall"

func goRuntimeSignal(sig syscall.Signal) bool { return false }
