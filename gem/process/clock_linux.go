package process

import (
	"github.com/oruby/oruby"
	"golang.org/x/sys/unix"
)

func initClockOS(mProc oruby.RClass) {
	mProc.Const("CLOCK_REALTIME_COARSE", unix.CLOCK_REALTIME_COARSE)
	mProc.Const("CLOCK_MONOTONIC_COARSE", unix.CLOCK_MONOTONIC_COARSE)
	mProc.Const("CLOCK_BOOTTIME", unix.CLOCK_BOOTTIME)
}
