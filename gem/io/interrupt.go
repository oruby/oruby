package io

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/oruby/oruby"
)

// A read that would block (a pipe, a socket, a terminal) waits in slices,
// checking between them for a signal (Ctrl-C, SIGTERM) and for the deadline
// of an enclosing Timeout.timeout or EvalContext (oruby.MrbState.GoContext):
// those then raise in Ruby, as MRI interrupts a blocking read. A regular
// file has no deadline (reading it does not block) and is read directly.

const readSlice = 100 * time.Millisecond

type interruptReader struct {
	mrb *oruby.MrbState
	f   *os.File
}

// interruptible wraps r when it is a file that can wait
func interruptible(mrb *oruby.MrbState, r io.Reader) io.Reader {
	if f, ok := r.(*os.File); ok {
		return &interruptReader{mrb, f}
	}
	// a stream over a pipe (IO.popen)
	if rf, ok := r.(interface{ ReadFile() *os.File }); ok {
		if f := rf.ReadFile(); f != nil {
			return &interruptReader{mrb, f}
		}
	}
	return r
}

func (ir *interruptReader) Read(p []byte) (int, error) {
	ctx := ir.mrb.GoContext()
	for {
		var n int
		var err error
		ir.mrb.Blocking(func() {
			if err = ir.f.SetReadDeadline(time.Now().Add(readSlice)); err != nil {
				n, err = ir.f.Read(p) // no deadline: a regular file
				return
			}
			n, err = ir.f.Read(p)
			_ = ir.f.SetReadDeadline(time.Time{})
		})
		if n > 0 || err == nil || !errors.Is(err, os.ErrDeadlineExceeded) {
			return n, err
		}
		if err := interrupted(ir.mrb, ctx); err != nil {
			return 0, err
		}
	}
}

// interrupted: the error a waiting call stops with (a signal, the
// deadline of ctx), or nil to wait on
func interrupted(mrb *oruby.MrbState, ctx context.Context) error {
	if err := mrb.CheckSignals(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		if err := mrb.ContextErr(ctx); err != nil {
			return err
		}
		return oruby.ERuntimeError("interrupted")
	}
	return nil
}

// interruptWriter is the writing side: a write into a full pipe or socket
// waits in slices too
type interruptWriter struct {
	mrb *oruby.MrbState
	f   *os.File
}

func interruptibleWriter(mrb *oruby.MrbState, w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok {
		return &interruptWriter{mrb, f}
	}
	if wf, ok := w.(interface{ WriteFile() *os.File }); ok {
		if f := wf.WriteFile(); f != nil {
			return &interruptWriter{mrb, f}
		}
	}
	return w
}

func (iw *interruptWriter) Write(p []byte) (int, error) {
	ctx := iw.mrb.GoContext()
	total := 0
	for {
		var n int
		var err error
		iw.mrb.Blocking(func() {
			if err = iw.f.SetWriteDeadline(time.Now().Add(readSlice)); err != nil {
				n, err = iw.f.Write(p[total:]) // a regular file
				return
			}
			n, err = iw.f.Write(p[total:])
			_ = iw.f.SetWriteDeadline(time.Time{})
		})
		total += n
		if err == nil || !errors.Is(err, os.ErrDeadlineExceeded) {
			return total, err
		}
		if err := interrupted(iw.mrb, ctx); err != nil {
			return total, err
		}
	}
}

// writeOnce is syswrite: one write(2), what it took (part of p when the
// descriptor takes less), waiting first while it would block, as MRI does
func writeOnce(mrb *oruby.MrbState, f *os.File, p []byte) (int, error) {
	sc, err := f.SyscallConn()
	if err != nil {
		return f.Write(p)
	}
	ctx := mrb.GoContext()
	for {
		var n int
		var werr, err error
		mrb.Blocking(func() {
			sliced := f.SetWriteDeadline(time.Now().Add(readSlice)) == nil
			err = sc.Write(func(fd uintptr) bool {
				n, werr = rawWrite(fd, p)
				return !errors.Is(werr, syscall.EAGAIN) && !errors.Is(werr, syscall.EINTR)
			})
			if sliced {
				_ = f.SetWriteDeadline(time.Time{})
			}
		})
		if err != nil && errors.Is(err, os.ErrDeadlineExceeded) {
			if err := interrupted(mrb, ctx); err != nil {
				return 0, err
			}
			continue
		}
		if werr != nil {
			return 0, werr
		}
		if err != nil {
			// a blocking descriptor the program made non-blocking: no poller
			return 0, syscall.EAGAIN
		}
		return n, nil
	}
}
