package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// progress prints how far the compilation is. On a terminal it keeps one line
// up to date; otherwise it prints a line when a stage starts and ends and at
// about every tenth of a stage, so a redirected log still shows where it is.
type progress struct {
	mu      sync.Mutex
	out     io.Writer
	tty     bool
	started time.Time
	stage   string
	began   time.Time
	last    time.Time
	tenth   int
	width   int
}

func newProgress(mode string) *progress {
	if mode == "off" {
		return nil
	}
	fi, err := os.Stderr.Stat()
	tty := err == nil && fi.Mode()&os.ModeCharDevice != 0
	if mode == "auto" && !tty {
		return nil
	}
	return &progress{out: os.Stderr, tty: tty, started: time.Now()}
}

func (p *progress) elapsed(t time.Time) string {
	return fmt.Sprintf("%.1fs", time.Since(t).Seconds())
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n+3:]
}

// Report is the compiler.ProgressFunc: a nil *progress reports nothing.
func (p *progress) Report(stage string, done, total int, item string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if stage != p.stage {
		p.finishStage(now)
		p.stage, p.began, p.tenth = stage, now, 0
		if !p.tty {
			fmt.Fprintf(p.out, "[%s] %s: start\n", p.elapsed(p.started), stage)
		}
	}
	if p.tty {
		if now.Sub(p.last) < 50*time.Millisecond && done != total {
			return
		}
		p.last = now
		p.line(stage, done, total, item)
		return
	}
	if total > 0 && done*10/total > p.tenth {
		p.tenth = done * 10 / total
		fmt.Fprintf(p.out, "[%s] %s: %d/%d (%d%%) %s\n", p.elapsed(p.started), stage, done, total, done*100/total, shorten(item, 70))
	}
}

func (p *progress) line(stage string, done, total int, item string) {
	var bar string
	if total > 0 {
		const w = 20
		n := done * w / total
		bar = fmt.Sprintf("[%s%s] %d/%d ", strings.Repeat("#", n), strings.Repeat(".", w-n), done, total)
	} else if done > 0 {
		bar = fmt.Sprintf("%d ", done)
	}
	msg := fmt.Sprintf("%-8s %s%s  (%s)", stage, bar, shorten(item, 50), p.elapsed(p.began))
	fmt.Fprintf(p.out, "\r\x1b[K%s", msg)
	p.width = len(msg)
}

func (p *progress) finishStage(now time.Time) {
	if p.stage == "" {
		return
	}
	if p.tty {
		fmt.Fprintf(p.out, "\r\x1b[K%-8s done in %s\n", p.stage, now.Sub(p.began).Round(100*time.Millisecond))
	} else {
		fmt.Fprintf(p.out, "[%s] %s: done in %s\n", p.elapsed(p.started), p.stage, now.Sub(p.began).Round(100*time.Millisecond))
	}
	p.stage = ""
}

// Stage marks a stage that has no item count and returns a function that ends
// it. While it runs, a ticker reports the elapsed time (the Go build can take
// minutes and prints nothing of its own).
func (p *progress) Stage(name string) (end func()) {
	if p == nil {
		return func() {}
	}
	p.mu.Lock()
	p.finishStage(time.Now())
	p.stage, p.began = name, time.Now()
	if !p.tty {
		fmt.Fprintf(p.out, "[%s] %s: start\n", p.elapsed(p.started), name)
	}
	p.mu.Unlock()
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		interval := time.Second
		if !p.tty {
			interval = 15 * time.Second
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.mu.Lock()
				if p.stage == name {
					if p.tty {
						p.line(name, 0, 0, "running")
					} else {
						fmt.Fprintf(p.out, "[%s] %s: running, %s\n", p.elapsed(p.started), name, p.elapsed(p.began))
					}
				}
				p.mu.Unlock()
			}
		}
	}()
	return func() {
		close(stop)
		<-finished
		p.mu.Lock()
		p.finishStage(time.Now())
		p.mu.Unlock()
	}
}

// Done ends the last stage and prints the total time.
func (p *progress) Done() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finishStage(time.Now())
	fmt.Fprintf(p.out, "total %s\n", p.elapsed(p.started))
}
