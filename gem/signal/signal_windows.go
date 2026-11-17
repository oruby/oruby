package signal

import "syscall"

func platformInitSignals() {
	signals["HUP"] = int(syscall.SIGHUP)
	signals["INT"] = int(syscall.SIGINT)
	signals["QUIT"] = int(syscall.SIGQUIT)
	signals["ILL"] = int(syscall.SIGILL)
	signals["TRAP"] = int(syscall.SIGTRAP)
	signals["ABRT"] = int(syscall.SIGABRT)
	signals["BUS"] = int(syscall.SIGBUS)
	signals["FPE"] = int(syscall.SIGFPE)
	signals["KILL"] = int(syscall.SIGKILL)
	signals["SEGV"] = int(syscall.SIGSEGV)
	signals["PIPE"] = int(syscall.SIGPIPE)
	signals["ALRM"] = int(syscall.SIGALRM)
	signals["TERM"] = int(syscall.SIGTERM)
}

// NSIG: signal numbers are below it
const platformNSIG = 23
