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
	mProc.Const("CLOCK_REALTIME", ClockRealtime)
	//	mProc.Const("CLOCK_REALTIME", RUBY_GETTIMEOFDAY_BASED_CLOCK_REALTIME)
	mProc.Const("CLOCK_MONOTONIC", ClockMonotonic)
	//	mProc.Const("CLOCK_MONOTONIC", RUBY_MACH_ABSOLUTE_TIME_BASED_CLOCK_MONOTONIC)
	mProc.Const("CLOCK_PROCESS_CPUTIME_ID", ClockProcessCputimeID)
	//	mProc.Const("CLOCK_PROCESS_CPUTIME_ID", RUBY_GETRUSAGE_BASED_CLOCK_PROCESS_CPUTIME_ID)
	mProc.Const("CLOCK_THREAD_CPUTIME_ID", ClockThreadCputimeID)
	//	mProc.Const("CLOCK_PROF", CLOCKID2NUM(CLOCK_PROF))
	//	mProc.Const("CLOCK_REALTIME_FAST", CLOCKID2NUM(CLOCK_REALTIME_FAST))
	//	mProc.Const("CLOCK_REALTIME_PRECISE", CLOCKID2NUM(CLOCK_REALTIME_PRECISE))
	mProc.Const("CLOCK_REALTIME_COARSE", ClockMonotonicCoarse)
	mProc.Const("CLOCK_REALTIME_ALARM", ClockRealtimeAlarm)
	//	mProc.Const("CLOCK_MONOTONIC_FAST", CLOCKID2NUM(CLOCK_MONOTONIC_FAST))
	//	mProc.Const("CLOCK_MONOTONIC_PRECISE", CLOCKID2NUM(CLOCK_MONOTONIC_PRECISE))
	mProc.Const("CLOCK_MONOTONIC_RAW", ClockMonotonicRaw)
	//	mProc.Const("CLOCK_MONOTONIC_RAW_APPROX", CLOCKID2NUM(CLOCK_MONOTONIC_RAW_APPROX))
	mProc.Const("CLOCK_MONOTONIC_COARSE", ClockMonotonicCoarse)
	mProc.Const("CLOCK_BOOTTIME", ClockBoottime)
	mProc.Const("CLOCK_BOOTTIME_ALARM", ClockBoottimeAlarm)
	//	mProc.Const("CLOCK_UPTIME", CLOCKID2NUM(CLOCK_UPTIME))
	//	mProc.Const("CLOCK_UPTIME_FAST", CLOCKID2NUM(CLOCK_UPTIME_FAST))
	//	mProc.Const("CLOCK_UPTIME_PRECISE", CLOCKID2NUM(CLOCK_UPTIME_PRECISE))
	//	mProc.Const("CLOCK_UPTIME_RAW", CLOCKID2NUM(CLOCK_UPTIME_RAW))
	//	mProc.Const("CLOCK_UPTIME_RAW_APPROX", CLOCKID2NUM(CLOCK_UPTIME_RAW_APPROX))
	//	mProc.Const("CLOCK_SECOND", CLOCKID2NUM(CLOCK_SECOND))
	mProc.Const("CLOCK_TAI", ClockTai)

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
	//mProc.Const("RLIMIT_NPROC", RLIMIT_NPROC)
	//mProc.Const("RLIMIT_RSS", RLIMIT_RSS)
	//mProc.Const("RLIMIT_RTPRIO", RLIMIT_RTPRIO)
	//mProc.Const("RLIMIT_RTTIME", RLIMIT_RTTIME)
	//mProc.Const("RLIMIT_SBSIZE", RLIMIT_SBSIZE)
	mProc.Const("RLIMIT_STACK", RLIMIT_STACK)

}
