package io

import "github.com/oruby/oruby"

func initFileTest(mrb *oruby.MrbState, ioClass oruby.RClass) {
	f := mrb.DefineClass("FileTest", mrb.ObjectClass())

	mrb.DefineClassMethod(f, "directory?", filetestDirectory, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "exist?",     filetestExist, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "exists?",    filetestExist, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "file?",      filetestFile, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "pipe?",      filetestPipe, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "size",       filetestSi, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "size?",      filetestSize, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "socket?",    filetestSocket, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "symlink?",   filetestSymlink, mrb.ArgsReq(1))
	mrb.DefineClassMethod(f, "zero?",      filetestZero, mrb.ArgsReq(1))

}
