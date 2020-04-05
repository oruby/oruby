package file

import "github.com/oruby/oruby"

func initFileTest(mrb *oruby.MrbState ) oruby.RClass {

	f := mrb.DefineModule("FileTest")
	proxyClassMethodToStat(f, "blockdev?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "chardev?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "directory?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "empty?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "executable?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "executable_real?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "exist?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "exists?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "file?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "grpowned?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "identical?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "owned?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "pipe?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "readable?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "readable_real?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "setgid?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "setuid?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "size", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "size?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "socket?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "sticky?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "symlink?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "world_readable?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "world_writable?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "writable?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "writable_real?", mrb.ArgsReq(1))
	proxyClassMethodToStat(f, "zero?", mrb.ArgsReq(1))
	return f
}
