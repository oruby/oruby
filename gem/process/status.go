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
	Stopsig      int
	IsSignaled   bool
	Termsig      int
	IsExited     bool
	Exitstatus   int
	IsSucess     bool
	IsCoredump   bool
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
	return fmt.Sprintf("<>")
}

func (s *status) Inspect() string {
	return s.ToS()
}

func initStatus(mProc oruby.RClass) {
	mrb := mProc.Mrb()
	cProcessStatus := mProc.DefineClassUnder("Status", mrb.ObjectClass())
	mrb.DefineGoClassUnder(mProc, "Status", &status{})
	cProcessStatus.UndefClassMethod("new")
	cProcessStatus.DefineAlias("equal", "==")
	cProcessStatus.DefineAlias("bit_and", "==")
	cProcessStatus.DefineAlias("r_shift", ">>")
}
