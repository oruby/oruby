package signal

import "syscall"

func platformInitSignals() {
	signals[syscall.SIGABRT.String()]   = int(syscall.SIGABRT)
	signals[syscall.SIGALRM.String()]   = int(syscall.SIGALRM)
	signals[syscall.SIGBUS.String()]    = int(syscall.SIGBUS)
	signals[syscall.SIGCHLD.String()]   = int(syscall.SIGCHLD)
	signals[syscall.SIGCONT.String()]   = int(syscall.SIGCONT)
	signals[syscall.SIGEMT.String()]    = int(syscall.SIGEMT)
	signals[syscall.SIGFPE.String()]    = int(syscall.SIGFPE)
	signals[syscall.SIGHUP.String()]    = int(syscall.SIGHUP)
	signals[syscall.SIGILL.String()]    = int(syscall.SIGILL)
	signals[syscall.SIGINFO.String()]   = int(syscall.SIGINFO)
	signals[syscall.SIGINT.String()]    = int(syscall.SIGINT)
	signals[syscall.SIGIO.String()]     = int(syscall.SIGIO)
	signals[syscall.SIGIOT.String()]    = int(syscall.SIGIOT)
	signals[syscall.SIGKILL.String()]   = int(syscall.SIGKILL)
	signals[syscall.SIGPIPE.String()]   = int(syscall.SIGPIPE)
	signals[syscall.SIGPROF.String()]   = int(syscall.SIGPROF)
	signals[syscall.SIGQUIT.String()]   = int(syscall.SIGQUIT)
	signals[syscall.SIGSEGV.String()]   = int(syscall.SIGSEGV)
	signals[syscall.SIGSTOP.String()]   = int(syscall.SIGSTOP)
	signals[syscall.SIGSYS.String()]    = int(syscall.SIGSYS)
	signals[syscall.SIGTERM.String()]   = int(syscall.SIGTERM)
	signals[syscall.SIGTRAP.String()]   = int(syscall.SIGTRAP)
	signals[syscall.SIGTSTP.String()]   = int(syscall.SIGTSTP)
	signals[syscall.SIGTTIN.String()]   = int(syscall.SIGTTIN)
	signals[syscall.SIGTTOU.String()]   = int(syscall.SIGTTOU)
	signals[syscall.SIGURG.String()]    = int(syscall.SIGURG)
	signals[syscall.SIGUSR1.String()]   = int(syscall.SIGUSR1)
	signals[syscall.SIGUSR2.String()]   = int(syscall.SIGUSR2)
	signals[syscall.SIGVTALRM.String()] = int(syscall.SIGVTALRM)
	signals[syscall.SIGWINCH.String()]  = int(syscall.SIGWINCH)
	signals[syscall.SIGXCPU.String()]   = int(syscall.SIGXCPU)
	signals[syscall.SIGXFSZ.String()]   = int(syscall.SIGXFSZ)
}

