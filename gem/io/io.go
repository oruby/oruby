package io

import (
	"github.com/oruby/oruby"
	"os"
)

func init() {
	oruby.Gem("io", func(mrb *oruby.MrbState) interface{} {
		cIO := mrb.DefineClass("IO", mrb.ObjectClass())
		//MRB_SET_INSTANCE_TT(cIO, MRB_TT_DATA)

		cIO.Include(mrb.ModuleGet("Enumerable")) /* 15.2.20.3 */
		cIO.DefineClassMethod("_popen", ioSPopen, mrb.ArgsArg(1, 2))
		cIO.DefineClassMethod("_sysclose", ioSSysclose, mrb.ArgsReq(1))
		cIO.DefineClassMethod("for_fd", ioSForFD, mrb.ArgsArg(1, 2))
		cIO.DefineClassMethod("select", ioSSelect, mrb.ArgsArg(1, 3))
		cIO.DefineClassMethod("sysopen", ioSSysopen, mrb.ArgsArg(1, 2))
		cIO.DefineClassMethod("_pipe", ioSPipe, mrb.ArgsNone())

		cIO.DefineMethod("initialize", ioInit, mrb.ArgsArg(1, 2)) /* 15.2.20.5.21 (x)*/
		cIO.DefineMethod("initialize_copy", ioInitCopy, mrb.ArgsReq(1))
		cIO.DefineMethod("_check_readable", ioCheckReadable, mrb.ArgsNone())
		cIO.DefineMethod("isatty", ioIsatty, mrb.ArgsNone())
		cIO.DefineMethod("sync", ioSync, mrb.ArgsNone())
		cIO.DefineMethod("sync=", ioSetSync, mrb.ArgsReq(1))
		cIO.DefineMethod("sysread", ioSysread, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("sysseek", ioSysseek, mrb.ArgsArg(1, 1))
		cIO.DefineMethod("syswrite", ioSyswrite, mrb.ArgsReq(1))
		cIO.DefineMethod("close", ioClose, mrb.ArgsNone()) /* 15.2.20.5.1 */
		cIO.DefineMethod("close_write", ioCloseWrite, mrb.ArgsNone())
		cIO.DefineMethod("close_on_exec=", ioSetCloseOnExec, mrb.ArgsReq(1))
		cIO.DefineMethod("close_on_exec?", ioCloseOnExecP, mrb.ArgsNone())
		cIO.DefineMethod("closed?", ioClosed, mrb.ArgsNone()) /* 15.2.20.5.2 */
		cIO.DefineMethod("pid", ioPid, mrb.ArgsNone())        /* 15.2.20.5.2 */
		cIO.DefineMethod("fileno", ioFilenoM, mrb.ArgsNone())

		cIO.DefineClassMethod("_bufread", ioBufread, mrb.ArgsReq(2))

		return nil
	})
}

func modestrToFlags(mrb *oruby.MrbState, mode string) (int, error) {
	flags := 0

	if mode == "" {
		return 0, oruby.EArgumentError("illegal access mode %v", mode)
	}

	switch mode[0] {
	case 'r':
		flags = os.O_RDONLY
	case 'w':
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC;
	case 'a':
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND;
	default:
		return 0, oruby.EArgumentError("illegal access mode %v", mode)
	}

	if len(mode) == 1 {
		return flags, nil
	}

	for _, m := range mode {
		switch m {
		case 'b':
			//flags |= os.O_BINARY
		case 't':
			//flags |= os.O_TEXT
		case '+':
			flags = (flags & ^(os.O_RDONLY | os.O_WRONLY | os.O_RDWR)) | os.O_RDWR
		case 'x':
			if mode[0] != 'w' {
				return 0, oruby.EArgumentError("illegal access mode %v", mode)
			}
			flags |= os.O_EXCL
		case ':':
			// ignore BOM
			goto end
		default:
			return 0, oruby.EArgumentError("illegal access mode %v", mode)
		}
	}

end:
	return flags, nil
}

func modeToFlags(mrb *oruby.MrbState, mode oruby.Value) (int, error) {
	if mode.IsNil() {
		return modestrToFlags(mrb, "r")
	} else if mode.IsString() {
		return modestrToFlags(mrb, mode.String())
	} else if mode.IsFixnum() {
		return mode.Int(), nil
	} else  {
		return 0, oruby.EArgumentError("illegal access mode %v", mrb.Inspect(mode))
	}
}

func ioSSysopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	path, mode, perm := mrb.GetArgs3("", mrb.NilValue(), 0666)

	flags, err :=modeToFlags(mrb, mode)
	if err != nil {
		return mrb.RaiseError(err)
	}

	f, err := os.OpenFile(mrb.String(path), flags, os.FileMode(perm.Int()))
	if err != nil {
		return mrb.SysFail(err.Error())
	}

	return mrb.Value(f.Fd())
}

func ioInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {

	return self
}
