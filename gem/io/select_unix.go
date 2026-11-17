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
func ioPoll(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	var fds []unix.PollFd
	type ref struct{ kind, idx int }
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
			fds = append(fds, unix.PollFd{Fd: int32(v.Int64()), Events: ev})
			refs = append(refs, ref{kind, i})
		}
	}
	timeout := int(a[3].Int64())
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	for {
		n, err := unix.Poll(fds, timeout)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				if timeout >= 0 {
					timeout = int(time.Until(deadline).Milliseconds())
					if timeout < 0 {
						timeout = 0
					}
				}
				continue
			}
			return mrb.RaiseError(err)
		}
		if n == 0 {
			return mrb.NilValue()
		}
		break
	}
	res := [3][]int{{}, {}, {}}
	for i, fd := range fds {
		if fd.Revents&unix.POLLNVAL != 0 {
			return mrb.Raise(mrb.ClassGet("IOError"), "closed stream")
		}
		r := refs[i]
		hit := fd.Revents&fd.Events != 0
		if r.kind != 2 && fd.Revents&(unix.POLLHUP|unix.POLLERR) != 0 {
			hit = true
		}
		if hit {
			res[r.kind] = append(res[r.kind], r.idx)
		}
	}
	return mrb.Value([]interface{}{res[0], res[1], res[2]})
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
	buf := make([]byte, mrb.GetArgsFirst().Int64())
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
	return mrb.BytesValue(buf[:n])
}

// __write_nonblock(str) -> Integer or :wait_writable
func ioWriteNonblock(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	fd, ok := fdOf(mrb, self)
	if !ok {
		return RaiseIOError(mrb, "closed stream")
	}
	data := mrb.GetArgsFirst().Bytes()
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
