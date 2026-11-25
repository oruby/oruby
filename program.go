package oruby

import (
	"fmt"
	"os"
	"syscall"
)

// The end of a program run by oruby, as MRI ends one: the at_exit handlers
// run, an uncaught exception is reported, and the status is SystemExit's, 1
// for any other exception, or the one an at_exit handler decided. An uncaught
// SignalException ends the process with its signal. cmd/oruby and compiled
// programs (compiler: the main of a standalone binary) both end this way.

// FinishProgram ends a program that ran to its end: its at_exit handlers run,
// and an exception or exit in one sets the status. prog is the name to report
// errors under.
func (mrb *MrbState) FinishProgram(prog string) int {
	mrb.RunAtExitHandlers()
	if mrb.Exc() != nil {
		code, _ := mrb.ReportException(prog, mrb.Err())
		return code
	}
	if st, ok := mrb.AtExitStatus(); ok {
		return st
	}
	return 0
}

// ReportException prints an uncaught exception as MRI does and returns the
// exit status: SystemExit's status, 1 otherwise. done is false when there was
// no pending exception.
func (mrb *MrbState) ReportException(prog string, err error) (code int, done bool) {
	exc := mrb.Exc()
	if exc == nil {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", prog, err)
			return 1, true
		}
		return 0, false
	}
	mrb.ExcClear()
	// at_exit handlers see the exception that ends the program in $!
	mrb.SetGV("$!", exc.Value())
	if exc.IsKindOf(mrb.ClassGet("SystemExit")) {
		status := mrb.Call(exc.Value(), "status")
		code := 0
		if !status.IsNil() {
			code = int(status.Int())
		}
		mrb.RunAtExitHandlers()
		// an exit in a handler decides the status (MRI's)
		if st, ok := mrb.AtExitStatus(); ok {
			code = st
		}
		return code, true
	}
	mrb.SetGV("$__oruby_top_exc__", exc.Value())
	// at_exit handlers run before the error is printed, as in MRI
	mrb.RunAtExitHandlers()
	mrb.ExcClear()
	// an uncaught SignalException ends the process with its signal; only
	// Interrupt is reported
	signo := 0
	if mrb.ClassDefined("SignalException") && exc.IsKindOf(mrb.ClassGet("SignalException")) {
		if v := mrb.Call(exc.Value(), "signo"); v.IsInteger() {
			signo = v.Int()
		}
		if !exc.IsKindOf(mrb.ClassGet("Interrupt")) {
			mrb.SetGV("$__oruby_top_exc__", mrb.NilValue())
			mrb.LoadString("$stdout.flush rescue nil; $stderr.flush rescue nil")
			SignalKillSelf(syscall.Signal(signo))
		}
	}
	_, perr := mrb.LoadString(`begin
  e = $__oruby_top_exc__
  $__oruby_top_exc__ = nil
  $stderr.write(e.full_message(highlight: false))
  $stderr.flush
rescue Exception
  $stderr.write("#{e.class}: #{e.message rescue '?'}\n") rescue nil
end`)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", prog, err)
	}
	if signo > 0 {
		mrb.LoadString("$stdout.flush rescue nil")
		SignalKillSelf(syscall.Signal(signo))
	}
	return 1, true
}
