package io

func initPlatformConsts(consts oruby.RClass) {
	consts.Const("ALT_SEPARATOR", os.PathSeparator)

	consts.Const("NONBLOCK", syscall.O_NONBLOCK)
	consts.Const("NOCTTY", syscall.O_NOCTTY)
	//consts.Const("DSYNC", syscall.O_DSYNC)
	//consts.Const("NOFOLLOW", syscall.O_NOFOLLOW)
	//consts.Const("BINARY", syscall.O_BINARY)
	consts.Const("SHARE_DELETE", syscall.FILE_SHARE_DELETE)
	//consts.Const("RSYNC", syscall.O_RSYNC)
	//consts.Const("NOATIME",  syscall.O_NOATIME)
	//consts.Const("DIRECT", syscall.O_DIRECT)
	//consts.Const("TMPFILE", syscall.O_TMPFILE)
	consts.Const("ASYNC", syscall.O_ASYNC)
	consts.Const("CLOEXEC", syscall.O_CLOEXEC)
}

