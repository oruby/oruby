//go:build windows

package nio4r

import (
	"context"
	"time"
)

type pollEntry struct {
	fd     int
	wantR  bool
	wantW  bool
	readyR bool
	readyW bool
}

type wakePipe struct {
	ch chan struct{}
}

func newWakePipe() (*wakePipe, error) {
	return &wakePipe{ch: make(chan struct{}, 1)}, nil
}

func (wp *wakePipe) wake() {
	if wp == nil {
		return
	}
	select {
	case wp.ch <- struct{}{}:
	default:
	}
}

func (wp *wakePipe) drain() {
	if wp == nil {
		return
	}
	for {
		select {
		case <-wp.ch:
		default:
			return
		}
	}
}

func (wp *wakePipe) close() {}

func pollEntries(ctx context.Context, sigWake <-chan struct{}, wp *wakePipe, entries []pollEntry, hasTimeout bool, timeout time.Duration) (int, bool, error) {
	var timerCh <-chan time.Time
	if hasTimeout {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timerCh = t.C
	}
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}
	select {
	case <-wp.ch:
		return 0, true, nil
	case <-ctxDone:
		return 0, true, nil
	case <-sigWake:
		return 0, true, nil
	case <-timerCh:
		return 0, false, nil
	}
}
