package process

import (
	"github.com/oruby/oruby"
	"golang.org/x/sys/unix"
)

func initClockOS(mProc oruby.RClass) {
	mProc.Const("CLOCK_MONOTONIC_RAW_APPROX", unix.CLOCK_MONOTONIC_RAW_APPROX)
	mProc.Const("CLOCK_UPTIME_RAW", unix.CLOCK_UPTIME_RAW)
	mProc.Const("CLOCK_UPTIME_RAW_APPROX", unix.CLOCK_UPTIME_RAW_APPROX)
}
