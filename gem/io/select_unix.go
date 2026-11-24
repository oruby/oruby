//go:build darwin || linux || freebsd

package io

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/oruby/oruby"
	"golang.org/x/sys/unix"
)

func init() { platformInit = initSelect }

func initSelect(mrb *oruby.MrbState, cIO oruby.RClass) {
	cIO.DefineClassMethod("__poll", ioPoll, mrb.ArgsReq(4))
	cIO.DefineMethod("__read_nonblock", ioReadNonblock, mrb.ArgsReq(1))
	cIO.DefineMethod("__write_nonblock", ioWriteNonblock, mrb.ArgsReq(1))
	cIO.DefineMethod("__nonblock", ioNonblock, mrb.ArgsReq(1))
	cIO.DefineMethod("__nonblock?", ioIsNonblock, mrb.ArgsNone())
	cIO.DefineMethod("__reader_gone?", ioReaderGone, mrb.ArgsNone())
}

func fdOf(mrb *oruby.MrbState, self oruby.Value) (int, bool) {
	if f, ok := mrb.Data(self).(*os.File); ok && f != nil {
		fd := RawFd(f)
		return fd, fd >= 0
	}
	if f, ok := mrb.Data(self).(interface{ Fd() uintptr }); ok {
		return int(f.Fd()), true
	}
	return -1, false
}

// __poll(read_fds, write_fds, err_fds, timeout_ms) -> [ready_r_idx, ready_w_idx, ready_e_idx] or nil
//
// A descriptor in more than one list is one poll entry (Darwin's poll
// misses events of a descriptor listed twice). The wait goes in slices so a
// signal or the deadline of a Timeout.timeout raises meanwhile.
func ioPoll(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	var fds []unix.PollFd
	slot := map[int32]int{}
	type ref struct{ kind, idx, slot int }
	var refs []ref
	for kind := 0; kind < 3; kind++ {
		if a[kind].IsNil() {
			continue
		}
		for i, v := range oruby.RArraySlice(a[kind]) {
			ev := int16(unix.POLLIN)
			if kind == 1 {
				ev = unix.POLLOUT
			} else if kind == 2 {
				ev = unix.POLLPRI
			}
			fd := int32(v.Int64())
			j, ok := slot[fd]
			if !ok {
				j = len(fds)
				slot[fd] = j
				fds = append(fds, unix.PollFd{Fd: fd})
			}
			fds[j].Events |= ev
			refs = append(refs, ref{kind, i, j})
		}
	}
	timeout := int(a[3].Int64())
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	ctx := mrb.GoContext()
	for {
		wait := int(readSlice / time.Millisecond)
		if timeout >= 0 {
			left := int(time.Until(deadline).Milliseconds())
			if left < 0 {
				left = 0
			}
			if left < wait {
				wait = left
			}
		}
		var n int
		var err error
		mrb.Blocking(func() {
			n, err = waitReady(fds, wait)
		})
		if errors.Is(err, syscall.EBADF) {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		if err != nil && !errors.Is(err, syscall.EINTR) {
			return mrb.RaiseError(err)
		}
		if err == nil && n > 0 {
			break
		}
		if err := interrupted(mrb, ctx); err != nil {
			return mrb.RaiseError(err)
		}
		if err == nil && timeout >= 0 && !time.Now().Before(deadline) {
			return mrb.NilValue()
		}
	}
	res := [3][]int{{}, {}, {}}
	for _, r := range refs {
		fd := fds[r.slot]
		if fd.Revents&unix.POLLNVAL != 0 {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		want := int16(unix.POLLIN)
		if r.kind == 1 {
			want = unix.POLLOUT
		} else if r.kind == 2 {
			want = unix.POLLPRI
		}
		hit := fd.Revents&want != 0
		if r.kind != 2 && fd.Revents&(unix.POLLHUP|unix.POLLERR) != 0 {
			hit = true
		}
		if hit {
			res[r.kind] = append(res[r.kind], r.idx)
		}
	}
	return mrb.Value([]interface{}{res[0], res[1], res[2]})
}

// waitReady waits up to ms for the events of fds, setting their Revents:
// select(2) as MRI's IO.select uses it, so the error set means exceptional
// conditions only (Darwin's poll reports POLLPRI for any readable pipe);
// poll when a descriptor is past FD_SETSIZE
func waitReady(fds []unix.PollFd, ms int) (int, error) {
	maxfd := -1
	for _, f := range fds {
		if int(f.Fd) > maxfd {
			maxfd = int(f.Fd)
		}
	}
	if maxfd >= unix.FD_SETSIZE {
		return unix.Poll(fds, ms)
	}
	var rs, ws, es unix.FdSet
	for _, f := range fds {
		if f.Events&unix.POLLIN != 0 {
			rs.Set(int(f.Fd))
		}
		if f.Events&unix.POLLOUT != 0 {
			ws.Set(int(f.Fd))
		}
		if f.Events&unix.POLLPRI != 0 {
			es.Set(int(f.Fd))
		}
	}
	tv := unix.NsecToTimeval(int64(ms) * int64(time.Millisecond))
	n, err := unix.Select(maxfd+1, &rs, &ws, &es, &tv)
	if err != nil || n == 0 {
		return 0, err
	}
	n = 0
	for i := range fds {
		fd := int(fds[i].Fd)
		fds[i].Revents = 0
		if fds[i].Events&unix.POLLIN != 0 && rs.IsSet(fd) {
			fds[i].Revents |= unix.POLLIN
		}
		if fds[i].Events&unix.POLLOUT != 0 && ws.IsSet(fd) {
			fds[i].Revents |= unix.POLLOUT
		}
		if fds[i].Events&unix.POLLPRI != 0 && es.IsSet(fd) {
			fds[i].Revents |= unix.POLLPRI
		}
		if fds[i].Revents != 0 {
			n++
		}
	}
	return n, nil
}

func ioNonblock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, ok := fdOf(mrb, self)
	if !ok {
		return mrb.NilValue()
	}
	if err := syscall.SetNonblock(fd, mrb.GetArgsFirst().Bool()); err != nil {
		return mrb.SysFail(err)
	}
	return mrb.GetArgsFirst()
}

func ioIsNonblock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, ok := fdOf(mrb, self)
	if !ok {
		return oruby.False
	}
	fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return mrb.SysFail(err)
	}
	return mrb.BoolValue(fl&unix.O_NONBLOCK != 0)
}

// ioReaderGone reports whether nothing reads the other end of the IO (a
// pipe whose read end is closed: POLLERR on Linux, POLLHUP on macOS), where
// data MRI would still hold in its write buffer could no longer be written.
func ioReaderGone(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, ok := fdOf(mrb, self)
	if !ok || fd <= 2 {
		return oruby.False
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
	if n, err := unix.Poll(fds, 0); err != nil || n == 0 {
		return oruby.False
	}
	return mrb.BoolValue(fds[0].Revents&(unix.POLLERR|unix.POLLHUP) != 0)
}

func withNonblock(fd int, f func() (int, error)) (int, error) {
	keep := isSocket(fd)
	fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return 0, err
	}
	if fl&unix.O_NONBLOCK == 0 {
		if err := syscall.SetNonblock(fd, true); err != nil {
			return 0, err
		}
		if !keep {
			defer syscall.SetNonblock(fd, false)
		}
	}
	for {
		n, err := f()
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return n, err
	}
}

// __read_nonblock(maxlen) -> String, :wait_readable or nil at EOF
func ioReadNonblock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, ok := fdOf(mrb, self)
	if !ok {
		return RaiseIOError(mrb, "closed stream")
	}
	maxlen := int(mrb.GetArgsFirst().Int64())
	// what ungetc pushed back or a read took ahead comes first (MRI)
	if view := rbufView(mrb, self); len(view) > 0 {
		if maxlen > len(view) {
			maxlen = len(view)
		}
		out := append([]byte(nil), view[:maxlen]...)
		rbufTake(mrb, self, maxlen)
		return binaryStringValue(mrb, out)
	}
	buf := make([]byte, maxlen)
	n, err := withNonblock(fd, func() (int, error) { return syscall.Read(fd, buf) })
	if err != nil {
		if errors.Is(err, syscall.EAGAIN) {
			return mrb.SymbolValue(oruby.Sym_wait_readable)
		}
		return mrb.SysFail(err)
	}
	if n == 0 && len(buf) > 0 {
		return mrb.NilValue()
	}
	return binaryStringValue(mrb, buf[:n])
}

// __write_nonblock(str) -> Integer or :wait_writable
func ioWriteNonblock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, ok := fdOf(mrb, self)
	if !ok {
		return RaiseIOError(mrb, "closed stream")
	}
	data := mrb.GetArgsFirst().Bytes()
	rbufDrop(mrb, self)
	n, err := withNonblock(fd, func() (int, error) { return syscall.Write(fd, data) })
	if err != nil {
		if errors.Is(err, syscall.EAGAIN) {
			return mrb.SymbolValue(oruby.Sym_wait_writable)
		}
		return mrb.SysFail(err)
	}
	return mrb.FixnumValue(n)
}

// isSocket reports whether fd is a socket; read_nonblock leaves sockets in
// non-blocking mode like Ruby (they are polled), other fds are restored so
// blocking os.File reads keep working.
func isSocket(fd int) bool {
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return false
	}
	return st.Mode&syscall.S_IFMT == syscall.S_IFSOCK
}
