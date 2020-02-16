package io

import (
	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("io", func(mrb *oruby.MrbState) interface{} {
		cIO := mrb.DefineClass("IO", mrb.ObjectClass())
		//MRB_SET_INSTANCE_TT(cIO, MRB_TT_DATA)

		cIO.Include(mrb.ModuleGet("Enumerable")) /* 15.2.20.3 */
		cIO.DefineClassMethod("_popen",    ioSPopen,   mrb.ArgsArg(1,2))
		cIO.DefineClassMethod("_sysclose", ioSSysclose, mrb.ArgsReq(1))
		cIO.DefineClassMethod("for_fd",    ioSForFD,   mrb.ArgsArg(1,2))
		cIO.DefineClassMethod("select",    ioSSelect,  mrb.ArgsArg(1,3))
		cIO.DefineClassMethod("sysopen",   ioSSysopen, mrb.ArgsArg(1,2))
		cIO.DefineClassMethod("_pipe",     ioSPipe,    mrb.ArgsNone())

		cIO.DefineMethod("initialize", ioInitialize, mrb.ArgsArg(1,2)) /* 15.2.20.5.21 (x)*/
		cIO.DefineMethod("initialize_copy", ioInitializeCopy, mrb.ArgsReq(1))
		cIO.DefineMethod("_check_readable", ioCheckReadable, mrb.ArgsNone())
		cIO.DefineMethod("isatty",     ioIsatty,     mrb.ArgsNone())
		cIO.DefineMethod("sync",       ioSync,       mrb.ArgsNone())
		cIO.DefineMethod("sync=",      ioSetSync,   mrb.ArgsReq(1))
		cIO.DefineMethod("sysread",    ioSysread,    mrb.ArgsArg(1,1))
		cIO.DefineMethod("sysseek",    ioSysseek,    mrb.ArgsArg(1,1))
		cIO.DefineMethod("syswrite",   ioSyswrite,   mrb.ArgsReq(1))
		cIO.DefineMethod("close",      ioClose,      mrb.ArgsNone()) /* 15.2.20.5.1 */
		cIO.DefineMethod("close_write",    ioCloseWrite,       mrb.ArgsNone())
		cIO.DefineMethod("close_on_exec=", ioSetCloseOnExec, mrb.ArgsReq(1))
		cIO.DefineMethod("close_on_exec?", ioCloseOnExecP,   mrb.ArgsNone())
		cIO.DefineMethod("closed?",     ioClosed,     mrb.ArgsNone()) /* 15.2.20.5.2 */
		cIO.DefineMethod("pid",         ioPid,        mrb.ArgsNone()) /* 15.2.20.5.2 */
		cIO.DefineMethod("fileno",      ioFilenoM,   mrb.ArgsNone())

		cIO.DefineClassMethod("_bufread", ioBufread, mrb.ArgsReq(2))

		return nil
	})
}