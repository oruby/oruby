package process

import (
	"github.com/oruby/oruby"
	"os"
)

func initPlatform(mrb *oruby.MrbState, mProc, mSys oruby.RClass) {

}

type limitsMap = int

func (runner *cmdRunner) checkLimitOption(limit int, option oruby.Value) {
	// do nothing on windows - rlimits are not supported
}

func (runner *cmdRunner) parseOptionsOS(options oruby.Value) func() {
	ret := func() {}
	mrb := runner.mrb
	if !options.IsHash() {
		return ret
	}

	if o := mrb.HashGet(options, mrb.Intern("new_pgroup")); o.Bool() {
		runner.cmd.SysProcAttr.CreationFlags += CREATE_NEW_PROCESS_GROUP
	}

	return ret
}

func (runner *cmdRunner) run() (int, error) {
	os.StartProcess()
	err := runner.cmd.Start()
	if err != nil {
		return 0, err
	}
	return runner.cmd.Process.Pid, nil
}
