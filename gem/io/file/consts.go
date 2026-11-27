package file

import (
	"github.com/oruby/oruby"
	"os"
)

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
	stdin := mrb.Value(os.Stdin)
	stdout := mrb.Value(os.Stdout)
	stderr := mrb.Value(os.Stderr)

	mrb.SetIV(stdin, "@__oruby_path", mrb.StrNew("<STDIN>"))
	mrb.SetIV(stdout, "@__oruby_path", mrb.StrNew("<STDOUT>"))
	mrb.SetIV(stderr, "@__oruby_path", mrb.StrNew("<STDERR>"))

	mrb.DefineGlobalConst("STDIN", stdin)
	mrb.DefineGlobalConst("STDOUT", stdout)
	mrb.DefineGlobalConst("STDERR", stderr)

	mrb.SetGV("$stdin", stdin)
	mrb.SetGV("$stdout", stdout)
	mrb.SetGV("$stderr", stderr)

	consts := mrb.DefineModuleUnder(fileClass, "Constants")

	consts.Const("LOCK_SH", 0x1)
	consts.Const("LOCK_EX", 0x2)
	consts.Const("LOCK_UN", 0x8)
	consts.Const("LOCK_NB", 0x4)

	consts.Const("Separator", "/")
	consts.Const("SEPARATOR", "/")
	consts.Const("PATH_SEPARATOR", string(os.PathListSeparator))

	consts.Const("NULL", os.DevNull)

	consts.Const("RDONLY", os.O_RDONLY)
	consts.Const("WRONLY", os.O_WRONLY)
	consts.Const("RDWR", os.O_RDWR)
	consts.Const("APPEND", os.O_APPEND)
	consts.Const("CREAT", os.O_CREATE)
	consts.Const("EXCL", os.O_EXCL)
	consts.Const("TRUNC", os.O_TRUNC)
	consts.Const("SYNC", os.O_SYNC)

	consts.Const("FNM_NOESCAPE", fnmNoescape)
	consts.Const("FNM_PATHNAME", fnmPathname)
	consts.Const("FNM_DOTMATCH", fnmDotmatch)
	consts.Const("FNM_CASEFOLD", fnmCasefold)
	consts.Const("FNM_EXTGLOB", fnmExtglob)
	consts.Const("FNM_SYSCASE", fnmSyscase)
	consts.Const("FNM_SHORTNAME", fnmShortname)
	consts.Const("FNM_GLOB_NOSORT", fnmGlobNosort)

	initPlatformConsts(consts)

	return consts
}
