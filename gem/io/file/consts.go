package file

import (
	"github.com/oruby/oruby"
	"os"
)

//
const (
	fnmNoescape   = 0x01
	fnmPathname   = 0x02
	fnmDotmatch   = 0x04
	fnmCasefold   = 0x08
	fnmExtglob    = 0x10
	fnmShortname  = 0x20
	fnmGlobNosort = 0x40
)

func initFileConsts(mrb *oruby.MrbState, fileClass oruby.RClass) oruby.RClass {
	//mrb.SetGV("$>", osStdout)
	//mrb.SetGV("$<", osStdin) // or argf
	mrb.SetGV("STDIN", os.Stdin)
	mrb.SetGV("$stdin", mrb.GetGV("STDIN"))
	mrb.SetGV("STDOUT", os.Stdout)
	mrb.SetGV("$stdout", mrb.GetGV("STDOUT"))
	mrb.SetGV("STDERR", os.Stderr)
	mrb.SetGV("$stderr", mrb.GetGV("STDERR"))

	consts := mrb.DefineModuleUnder(fileClass, "Constants")

	consts.Const("LOCK_SH", 0x1)
	consts.Const("LOCK_EX", 0x2)
	consts.Const("LOCK_UN", 0x8)
	consts.Const("LOCK_NB", 0x4)

	consts.Const("Separator", "/")
	consts.Const("SEPARATOR", "/")
	consts.Const("PATH_SEPARATOR", os.PathListSeparator)

	consts.Const("NULL",   os.DevNull)

	consts.Const("RDONLY", os.O_RDONLY)
	consts.Const("WRONLY", os.O_WRONLY)
	consts.Const("RDWR",   os.O_RDWR)
	consts.Const("APPEND", os.O_APPEND)
	consts.Const("CREAT",  os.O_CREATE)
	consts.Const("EXCL",   os.O_EXCL)
	consts.Const("TRUNC",  os.O_TRUNC)
	consts.Const("SYNC",   os.O_SYNC)

	consts.Const("FNM_NOESCAPE",  fnmNoescape)
	consts.Const("FNM_PATHNAME",  fnmPathname)
	consts.Const("FNM_DOTMATCH",  fnmDotmatch)
	consts.Const("FNM_CASEFOLD",  fnmCasefold)
	consts.Const("FNM_EXTGLOB",   fnmExtglob)
	consts.Const("FNM_SYSCASE",   fnmSyscase)
	consts.Const("FNM_SHORTNAME", fnmShortname)
	consts.Const("FNM_GLOB_NOSORT", fnmGlobNosort)

	initPlatformConsts(consts)

	return consts
}
