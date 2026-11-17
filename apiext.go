package oruby

// Functions that are similar to MRI API

// DefineGlobalFunction defines a Kernel module function, making it globally
// available: a private instance method and a public singleton method, as
// mrb_define_global_function and MRI's rb_define_global_function do
func (mrb *MrbState) DefineGlobalFunction(name string, f MrbFuncT, argc MrbAspec) {
	mrb.DefineModuleFunction(mrb.KernelModule(), name, f, argc)
}
