package io

import (
	"github.com/oruby/oruby"
	"os"
)

func initFileConsts(mrb *oruby.MrbState, fileModule oruby.RClass) {
	consts := mrb.DefineModuleUnder(fileModule, "Constants")

	consts.Const("LOCK_SH", 0x1)
	consts.Const("LOCK_EX", 0x2)
	consts.Const("LOCK_UN", 0x8)
	consts.Const("LOCK_NB", 0x4)

	consts.Const("SEPARATOR", "/")
	consts.Const("PATH_SEPARATOR", os.PathListSeparator)
	consts.Const("ALT_SEPARATOR", os.PathSeparator)
	consts.Const("NULL", os.DevNull)
	consts.Const("RDONLY", os.O_RDONLY)
	consts.Const("WRONLY", os.O_WRONLY)
	consts.Const("RDWR", os.O_RDWR)
	consts.Const("APPEND", os.O_APPEND)
	consts.Const("CREAT", os.O_CREATE)
	consts.Const("EXCL", os.O_EXCL)
	consts.Const("TRUNC", os.O_TRUNC)
	consts.Const("SYNC", os.O_SYNC)

	initPlatformConsts(consts)
}
