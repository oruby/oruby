package signal

import "syscall"

func platformInitSignals() {
	signals[syscall.SIGHUP.String()]  = int(syscall.SIGHUP)
	signals[syscall.SIGINT.String()]  = int(syscall.SIGINT)
	signals[syscall.SIGQUIT.String()] = int(syscall.SIGQUIT)
	signals[syscall.SIGILL.String()]  = int(syscall.SIGILL)
	signals[syscall.SIGTRAP.String()] = int(syscall.SIGTRAP)
	signals[syscall.SIGABRT.String()] = int(syscall.SIGABRT)
	signals[syscall.SIGBUS.String()]  = int(syscall.SIGBUS)
	signals[syscall.SIGFPE.String()]  = int(syscall.SIGFPE)
	signals[syscall.SIGKILL.String()] = int(syscall.SIGKILL)
	signals[syscall.SIGSEGV.String()] = int(syscall.SIGSEGV)
	signals[syscall.SIGPIPE.String()] = int(syscall.SIGPIPE)
	signals[syscall.SIGALRM.String()] = int(syscall.SIGALRM)
	signals[syscall.SIGTERM.String()] = int(syscall.SIGTERM)
}
