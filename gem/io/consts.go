package io

import (
	"github.com/oruby/oruby"
	"io"
)

func initConsts(mrb *oruby.MrbState, ioClass oruby.RClass) {
	mrb.SetGV("$/", "\n")

	ioClass.Const("SEEK_SET", io.SeekStart)
	ioClass.Const("SEEK_CUR", io.SeekCurrent)
	ioClass.Const("SEEK_END", io.SeekEnd)
	//ioClass.Const("SEEK_DATA", )
	//ioClass.Const("SEEK_HOLE", )

	ioClass.Const("BUF_SIZE", 4096)
}
