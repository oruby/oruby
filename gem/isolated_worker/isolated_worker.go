// Package isolated_worker provides experimental isolated-state workers.
package isolated_worker

import "github.com/oruby/oruby"

func init() {
	oruby.LazyGem("isolated_worker", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("ORuby")
		worker := mrb.DefineClassUnder(mod, "IsolatedWorker", mrb.ObjectClass())
		worker.AttachType((*workerState)(nil))
		worker.DefineMethod("initialize", initializeWorker, mrb.ArgsAny()|mrb.ArgsBlock())
		worker.DefineMethod("join", joinWorker, mrb.ArgsOpt(1))
		worker.DefineMethod("value", workerValue, mrb.ArgsNone())
		worker.DefineMethod("kill", unavailable, mrb.ArgsNone())
		worker.DefineMethod("stop", unavailable, mrb.ArgsNone())
		return nil
	})
}

func unavailable(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Raise(mrb.ENotImplementedError(), "isolated workers cannot be interrupted")
}
