//go:build !windows

package nio4r

import (
	"context"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type pollEntry struct {
	fd     int
	wantR  bool
	wantW  bool
	readyR bool
	readyW bool
}

type wakePipe struct {
	rfd int
	wfd int
}

func newWakePipe() (*wakePipe, error) {
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		return nil, err
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	_ = unix.SetNonblock(fds[0], true)
	_ = unix.SetNonblock(fds[1], true)
	return &wakePipe{rfd: fds[0], wfd: fds[1]}, nil
}

func (wp *wakePipe) wake() {
	if wp == nil || wp.wfd < 0 {
		return
	}
	var b [1]byte
	b[0] = 1
	_, _ = unix.Write(wp.wfd, b[:])
}

func (wp *wakePipe) drain() {
	if wp == nil || wp.rfd < 0 {
		return
	}
	var buf [64]byte
	for {
		n, err := unix.Read(wp.rfd, buf[:])
		if n <= 0 || err != nil {
			break
		}
	}
}

func (wp *wakePipe) fd() int {
	if wp == nil {
		return -1
	}
	return wp.rfd
}

func (wp *wakePipe) close() {
	if wp == nil {
		return
	}
	if wp.rfd >= 0 {
		_ = unix.Close(wp.rfd)
		wp.rfd = -1
	}
	if wp.wfd >= 0 {
		_ = unix.Close(wp.wfd)
		wp.wfd = -1
	}
}

// pollEntries polls entries plus the wakeup pipe, returning (readyCount, woken, err).
func pollEntries(ctx context.Context, sigWake <-chan struct{}, wp *wakePipe, entries []pollEntry, hasTimeout bool, timeout time.Duration) (int, bool, error) {
	// Bridge context cancellation / signal wake onto the wakeup pipe so unix.Poll unblocks immediately.
	done := make(chan struct{})
	defer close(done)
	if ctx != nil || sigWake != nil {
		go func() {
			var ctxDone <-chan struct{}
			if ctx != nil {
				ctxDone = ctx.Done()
			}
			select {
			case <-done:
			case <-ctxDone:
				wp.wake()
			case <-sigWake:
				wp.wake()
			}
		}()
	}

	var deadline time.Time
	if hasTimeout {
		deadline = time.Now().Add(timeout)
	}

	wakeFD := wp.fd()
	pfds := make([]unix.PollFd, 0, len(entries)+1)
	if wakeFD >= 0 {
		pfds = append(pfds, unix.PollFd{Fd: int32(wakeFD), Events: unix.POLLIN})
	}
	for _, e := range entries {
		var ev int16
		if e.wantR {
			ev |= unix.POLLIN
		}
		if e.wantW {
			ev |= unix.POLLOUT
		}
		pfds = append(pfds, unix.PollFd{Fd: int32(e.fd), Events: ev})
	}

	for {
		ms := -1
		if hasTimeout {
			rem := time.Until(deadline)
			if rem <= 0 {
				ms = 0
			} else {
				ms = int(rem.Milliseconds())
				if ms == 0 {
					ms = 1
				}
			}
		}

		n, err := unix.Poll(pfds, ms)
		if err != nil {
			if err == syscall.EINTR {
				if ctx != nil && ctx.Err() != nil {
					return 0, true, nil
				}
				continue
			}
			return 0, false, err
		}
		if n == 0 {
			return 0, false, nil
		}

		offset := 0
		woken := false
		if wakeFD >= 0 {
			offset = 1
			if pfds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0 {
				wp.drain()
				woken = true
			}
		}

		ready := 0
		for i := range entries {
			rev := pfds[offset+i].Revents
			if rev == 0 {
				continue
			}
			if entries[i].wantR && (rev&(unix.POLLIN|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0) {
				entries[i].readyR = true
			}
			if entries[i].wantW && (rev&(unix.POLLOUT|unix.POLLERR|unix.POLLNVAL) != 0) {
				entries[i].readyW = true
			}
			if entries[i].readyR || entries[i].readyW {
				ready++
			}
		}
		return ready, woken, nil
	}
}
