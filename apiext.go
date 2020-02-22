package oruby

// Functions that are similar to MRI API

// DefineGlobalFunction defines function in Kernel, making it globally available
func (mrb *MrbState) DefineGlobalFunction(name string, f MrbFuncT, argc MrbAspec) {
	mrb.DefineMethod(mrb.KernelModule(), name, f, argc)
}
