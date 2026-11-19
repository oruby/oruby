package process

import (
	"fmt"
	"os"

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

// SetLastStatusFrom sets $? from a finished child, with the signal that
// ended or stopped it
func SetLastStatusFrom(mrb *oruby.MrbState, pid int, ps *os.ProcessState) {
	st := &status{Pid: pid, Exitstatus: ps.ExitCode(), ToI: uint32(ps.ExitCode()),
		IsExited: ps.Exited(), IsSucess: ps.Success()}
	platformUpdateState(st, ps.Sys())
	st.Pid = pid
	mrb.SetGV("$?", mrb.Value(st))
}

func initStatus(mProc oruby.RClass) {
	mrb := mProc.Mrb()
	cProcessStatus := mProc.DefineClassUnder("Status", mrb.ObjectClass())
	mrb.DefineGoClassUnder(mProc, "Status", &status{})
	// Process::Status.wait's status when there is no child (MRI's pid -1)
	cProcessStatus.DefineClassMethod("__oruby_nochild", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.Value(&status{Pid: -1, IsExited: true, IsSucess: true})
	}, mrb.ArgsNone())
	// $? = status, for a wait through a Fiber scheduler (core_ext)
	mrb.DefineModuleFunction(mProc, "__oruby_set_status", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		mrb.SetGV("$?", v)
		return v
	}, mrb.ArgsReq(1))
	cProcessStatus.UndefClassMethod("new")
	cProcessStatus.DefineAlias("==", "equal")
	cProcessStatus.DefineAlias("&", "bit_and")
	cProcessStatus.DefineAlias(">>", "r_shift")
	cProcessStatus.DefineAlias("exited?", "is_exited")
	cProcessStatus.DefineAlias("signaled?", "is_signaled")
	cProcessStatus.DefineAlias("stopped?", "is_stopped")
	cProcessStatus.DefineAlias("success?", "is_sucess")

	// nil unless the process ended that way, as in MRI
	optInt := func(get func(*status) *int) oruby.MrbFuncT {
		return func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			st, ok := mrb.Data(self).(*status)
			if !ok || get(st) == nil {
				return mrb.NilValue()
			}
			return mrb.FixnumValue(*get(st))
		}
	}
	cProcessStatus.DefineMethod("termsig", optInt(func(st *status) *int {
		if !st.IsSignaled {
			return nil
		}
		return st.Termsig
	}), mrb.ArgsNone())
	cProcessStatus.DefineMethod("stopsig", optInt(func(st *status) *int {
		if !st.IsStopped {
			return nil
		}
		return st.Stopsig
	}), mrb.ArgsNone())
	cProcessStatus.DefineMethod("exitstatus", optInt(func(st *status) *int {
		if !st.IsExited {
			return nil
		}
		return &st.Exitstatus
	}), mrb.ArgsNone())
}
