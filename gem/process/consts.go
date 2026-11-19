package process

import "github.com/oruby/oruby"

// Clock consts
const (
	ClockRealtime = iota
	ClockMonotonic
	ClockProcessCputimeID
	ClockThreadCputimeID
	ClockMonotonicRaw
	ClockRealtimeCoarse
	ClockMonotonicCoarse
	ClockBoottime
	ClockRealtimeAlarm
	ClockBoottimeAlarm
	ClockSgiCycle
	ClockTai

	ClockMask = ClockRealtime | ClockMonotonic
)

func setConsts(mProc oruby.RClass) {
	mProc.Const("RLIM_SAVED_MAX", RLIM_INFINITY)
	mProc.Const("RLIM_INFINITY", RLIM_INFINITY)
	mProc.Const("RLIM_SAVED_CUR", RLIM_INFINITY)
	mProc.Const("RLIMIT_AS", RLIMIT_AS)
	mProc.Const("RLIMIT_CORE", RLIMIT_CORE)
	mProc.Const("RLIMIT_CPU", RLIMIT_CPU)
	mProc.Const("RLIMIT_DATA", RLIMIT_DATA)
	mProc.Const("RLIMIT_FSIZE", RLIMIT_FSIZE)
	//mProc.Const("RLIMIT_MEMLOCK", RLIMIT_MEMLOCK)
	//mProc.Const("RLIMIT_MSGQUEUE", RLIMIT_MSGQUEUE)
	//mProc.Const("RLIMIT_MSGQUEUE", RLIMIT_MSGQUEUE)
	//mProc.Const("RLIMIT_NICE", RLIMIT_NICE)
	mProc.Const("RLIMIT_NOFILE", RLIMIT_NOFILE)
	mProc.Const("RLIMIT_STACK", RLIMIT_STACK)
}
