package signal

import "syscall"

func platformInitSignals() {
	signals["ABRT"]   = int(syscall.SIGABRT)
	signals["ALRM"]   = int(syscall.SIGALRM)
	signals["BUS"]    = int(syscall.SIGBUS)
	signals["CHLD"]   = int(syscall.SIGCHLD)
	signals["CONT"]   = int(syscall.SIGCONT)
	signals["EMT"]    = int(syscall.SIGEMT)
	signals["FPE"]    = int(syscall.SIGFPE)
	signals["HUP"]    = int(syscall.SIGHUP)
	signals["ILL"]    = int(syscall.SIGILL)
	signals["INFO"]   = int(syscall.SIGINFO)
	signals["INT"]    = int(syscall.SIGINT)
	signals["IO"]     = int(syscall.SIGIO)
	signals["IOT"]    = int(syscall.SIGIOT)
	signals["KILL"]   = int(syscall.SIGKILL)
	signals["PIPE"]   = int(syscall.SIGPIPE)
	signals["PROF"]   = int(syscall.SIGPROF)
	signals["QUIT"]   = int(syscall.SIGQUIT)
	signals["SEGV"]   = int(syscall.SIGSEGV)
	signals["STOP"]   = int(syscall.SIGSTOP)
	signals["SYS"]    = int(syscall.SIGSYS)
	signals["TERM"]   = int(syscall.SIGTERM)
	signals["TRAP"]   = int(syscall.SIGTRAP)
	signals["TSTP"]   = int(syscall.SIGTSTP)
	signals["TTIN"]   = int(syscall.SIGTTIN)
	signals["TTOU"]   = int(syscall.SIGTTOU)
	signals["URG"]    = int(syscall.SIGURG)
	signals["USR1"]   = int(syscall.SIGUSR1)
	signals["USR2"]   = int(syscall.SIGUSR2)
	signals["VTALRM"] = int(syscall.SIGVTALRM)
	signals["WINCH"]  = int(syscall.SIGWINCH)
	signals["XCPU"]   = int(syscall.SIGXCPU)
	signals["XFSZ"]   = int(syscall.SIGXFSZ)
}

func platformReservedSignal(sig int) bool {
	switch sig {
	case int(syscall.SIGSEGV):
	case int(syscall.SIGBUS):
	case int(syscall.SIGILL):
	case int(syscall.SIGFPE):
	case int(syscall.SIGVTALRM):
		return true
	}
	return false
}
