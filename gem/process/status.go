package process

import (
	"fmt"

	"github.com/oruby/oruby"
)

type status struct {
	platformData interface{}
	ToI          uint32
	Pid          int
	IsStopped    bool
	Stopsig      *int
	IsSignaled   bool
	Termsig      *int
	IsExited     bool
	Exitstatus   int
	IsSucess     bool
	IsCoredump   bool
	err          error
}

func (s *status) Equal(s2 *status) bool {
	return s2 != nil && s.ToI == s2.ToI
}

func (s *status) BitAnd(v uint32) uint32 {
	return s.ToI & v
}

func (s *status) RShift(v uint32) uint32 {
	return s.ToI >> v
}

func (s *status) ToS() string {
	switch {
	case s.IsSignaled && s.Termsig != nil:
		str := fmt.Sprintf("pid %d SIG%s (signal %d)", s.Pid, signalName(*s.Termsig), *s.Termsig)
		if s.IsCoredump {
			str += " (core dumped)"
		}
		return str
	case s.IsStopped && s.Stopsig != nil:
		return fmt.Sprintf("pid %d stopped SIG%s (signal %d)", s.Pid, signalName(*s.Stopsig), *s.Stopsig)
	}
	return fmt.Sprintf("pid %d exit %d", s.Pid, s.Exitstatus)
}

func (s *status) Inspect() string {
	return "#<Process::Status: " + s.ToS() + ">"
}

func signalName(n int) string {
	for name, v := range map[string]int{"HUP": 1, "INT": 2, "QUIT": 3, "ILL": 4, "TRAP": 5, "ABRT": 6,
		"FPE": 8, "KILL": 9, "SEGV": 11, "PIPE": 13, "ALRM": 14, "TERM": 15, "USR1": 30, "USR2": 31} {
		if v == n {
			return name
		}
	}
	return fmt.Sprint(n)
}

// SetLastStatus sets $? (Process.last_status)
func SetLastStatus(mrb *oruby.MrbState, pid, exitCode int, exited, success bool) {
	st := &status{
		Pid:        pid,
		Exitstatus: exitCode,
		ToI:        uint32(exitCode),
		IsExited:   exited,
		IsSucess:   success,
	}
	mrb.SetGV("$?", mrb.Value(st))
}

func initStatus(mProc oruby.RClass) {
	mrb := mProc.Mrb()
	cProcessStatus := mProc.DefineClassUnder("Status", mrb.ObjectClass())
	mrb.DefineGoClassUnder(mProc, "Status", &status{})
	cProcessStatus.UndefClassMethod("new")
	cProcessStatus.DefineAlias("==", "equal")
	cProcessStatus.DefineAlias("&", "bit_and")
	cProcessStatus.DefineAlias(">>", "r_shift")
	cProcessStatus.DefineAlias("exited?", "is_exited")
	cProcessStatus.DefineAlias("signaled?", "is_signaled")
	cProcessStatus.DefineAlias("stopped?", "is_stopped")
	cProcessStatus.DefineAlias("success?", "is_sucess")
}
