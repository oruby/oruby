// Package nio4r implements Ruby's nio4r gem (NIO::Selector, NIO::Monitor,
// NIO::ByteBuffer) backed by Go's OS poller and wakeup pipe.
package nio4r

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	goio "github.com/oruby/oruby/gem/io"
)

//go:embed nio4r.rb
var nio4rRb []byte

func init() {
	oruby.LazyGem("nio4r_ext", initNIO)
	for _, alias := range []string{
		"nio",
		"nio4r",
		"nio/version",
		"nio/selector",
		"nio/monitor",
		"nio/bytebuffer",
	} {
		oruby.GemAlias(alias, "nio4r_ext")
	}
}

func initNIO(mrb *oruby.MrbState) any {
	mrb.Require("io")

	mod := mrb.DefineModule("NIO")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("2.7.5"))
	mrb.ConstSet(mod, mrb.Intern("ENGINE"), mrb.StrNew("oruby"))

	defineSelector(mrb, mod)
	defineMonitor(mrb, mod)
	defineByteBuffer(mrb, mod)

	rb.LoadRuby(mrb, "<internal:nio4r>", nio4rRb)
	return nil
}

// ---------------------------------------------------------------------------
// Monitor & Selector
// ---------------------------------------------------------------------------

type nioMonitor struct {
	ioVal   oruby.Value
	selVal  oruby.Value
	sel     *nioSelector
	fd      int
	wantR   bool
	wantW   bool
	readyR  bool
	readyW  bool
	value   oruby.Value
	closed  bool
	selfVal oruby.Value
}

func (m *nioMonitor) interestsSym(mrb *oruby.MrbState) oruby.Value {
	switch {
	case m.wantR && m.wantW:
		return mrb.Intern("rw").Value()
	case m.wantR:
		return mrb.Intern("r").Value()
	case m.wantW:
		return mrb.Intern("w").Value()
	default:
		return mrb.NilValue()
	}
}

func (m *nioMonitor) readinessSym(mrb *oruby.MrbState) oruby.Value {
	switch {
	case m.readyR && m.readyW:
		return mrb.Intern("rw").Value()
	case m.readyR:
		return mrb.Intern("r").Value()
	case m.readyW:
		return mrb.Intern("w").Value()
	default:
		return mrb.NilValue()
	}
}

func parseInterests(mrb *oruby.MrbState, v oruby.Value) (bool, bool, error) {
	if !v.IsSymbol() {
		return false, false, errors.New("interests must be :r, :w, or :rw")
	}
	name := mrb.SymName(v.Symbol())
	switch name {
	case "r":
		return true, false, nil
	case "w":
		return false, true, nil
	case "rw":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("invalid interest %q (expected :r, :w, or :rw)", name)
	}
}

func extractFD(mrb *oruby.MrbState, ioVal oruby.Value) (int, error) {
	if f, ok := mrb.Data(ioVal).(*os.File); ok {
		return goio.RawFd(f), nil
	}
	target := ioVal
	if mrb.RespondTo(target, mrb.Intern("to_io")) {
		v, err := mrb.Funcall(target, mrb.Intern("to_io"))
		if err != nil {
			return -1, err
		}
		target = v
		if f, ok := mrb.Data(target).(*os.File); ok {
			return goio.RawFd(f), nil
		}
	}
	if mrb.RespondTo(target, mrb.Intern("fileno")) {
		v, err := mrb.Funcall(target, mrb.Intern("fileno"))
		if err != nil {
			return -1, err
		}
		return v.Int(), nil
	}
	return -1, oruby.ETypeError("cannot convert %s to IO", mrb.ClassOf(ioVal).Name())
}

type nioSelector struct {
	mu       sync.Mutex
	wp       *wakePipe
	monitors []*nioMonitor
	closed   bool
}

func getSel(mrb *oruby.MrbState, self oruby.Value) *nioSelector {
	s, _ := rb.Get[*nioSelector](mrb, self)
	return s
}

func getMon(mrb *oruby.MrbState, self oruby.Value) *nioMonitor {
	m, _ := rb.Get[*nioMonitor](mrb, self)
	return m
}

func syncSelectorRefs(mrb *oruby.MrbState, selVal oruby.Value, s *nioSelector) {
	ary := mrb.AryNewCapa(len(s.monitors))
	for _, mon := range s.monitors {
		mrb.AryPush(ary, mon.selfVal)
	}
	mrb.IVSet(selVal, mrb.Intern("@__monitors"), ary)
}

func defineSelector(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Selector", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineClassMethod("backends", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ary := mrb.AryNewCapa(1)
		mrb.AryPush(ary, mrb.Intern("poll").Value())
		return ary
	}, mrb.ArgsNone())

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		wp, err := newWakePipe()
		if err != nil {
			return mrb.RaiseError(err)
		}
		s := &nioSelector{wp: wp}
		rb.Set(mrb, self, s)
		return self
	}, mrb.ArgsOpt(1))

	cls.DefineMethod("backend", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.Intern("poll").Value()
	}, mrb.ArgsNone())

	cls.DefineMethod("closed?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		s.mu.Lock()
		c := s.closed
		s.mu.Unlock()
		return mrb.BoolValue(c)
	}, mrb.ArgsNone())

	cls.DefineMethod("empty?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		s.mu.Lock()
		e := len(s.monitors) == 0
		s.mu.Unlock()
		return mrb.BoolValue(e)
	}, mrb.ArgsNone())

	cls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			for _, m := range s.monitors {
				m.closed = true
			}
			s.monitors = nil
			if s.wp != nil {
				s.wp.wake()
				s.wp.close()
				s.wp = nil
			}
		}
		s.mu.Unlock()
		syncSelectorRefs(mrb, self, s)
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("wakeup", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return mrb.Raise(mrb.ExcGet("IOError"), "selector is closed")
		}
		wp := s.wp
		s.mu.Unlock()
		wp.wake()
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("register", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return mrb.Raise(mrb.ExcGet("IOError"), "selector is closed")
		}
		s.mu.Unlock()

		args := mrb.GetArgs()
		ioVal := args.Item(0)
		intVal := args.Item(1)
		wantR, wantW, err := parseInterests(mrb, intVal)
		if err != nil {
			return mrb.EArgumentError().Raise(err.Error())
		}
		fd, err := extractFD(mrb, ioVal)
		if err != nil {
			return mrb.RaiseError(err)
		}

		s.mu.Lock()
		for _, m := range s.monitors {
			if mrb.ObjEqual(m.ioVal, ioVal) {
				s.mu.Unlock()
				return mrb.EArgumentError().Raise("IO already registered with selector")
			}
		}

		monCls := rb.ClassPath(mrb, "NIO::Monitor")
		mon := &nioMonitor{
			ioVal:  ioVal,
			selVal: self,
			sel:    s,
			fd:     fd,
			wantR:  wantR,
			wantW:  wantW,
			value:  mrb.NilValue(),
		}
		monVal := mrb.DataWrapInterface(monCls, mon).Value()
		mon.selfVal = monVal
		mrb.IVSet(monVal, mrb.Intern("@__io"), ioVal)
		mrb.IVSet(monVal, mrb.Intern("@__sel"), self)
		s.monitors = append(s.monitors, mon)
		s.mu.Unlock()

		syncSelectorRefs(mrb, self, s)
		return monVal
	}, mrb.ArgsReq(2))

	cls.DefineMethod("deregister", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		ioVal := mrb.GetArgsFirst()
		var removed oruby.Value = mrb.NilValue()
		s.mu.Lock()
		for i, m := range s.monitors {
			if mrb.ObjEqual(m.ioVal, ioVal) {
				removed = m.selfVal
				m.closed = true
				s.monitors = append(s.monitors[:i], s.monitors[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		syncSelectorRefs(mrb, self, s)
		return removed
	}, mrb.ArgsReq(1))

	cls.DefineMethod("registered?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		ioVal := mrb.GetArgsFirst()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, m := range s.monitors {
			if mrb.ObjEqual(m.ioVal, ioVal) {
				return mrb.TrueValue()
			}
		}
		return mrb.FalseValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("select", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := getSel(mrb, self)
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()

		hasTimeout := false
		var timeout time.Duration
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			tv := args.Item(0)
			var sec float64
			if tv.IsInt() {
				sec = float64(tv.Int64())
			} else if tv.IsFloat() {
				sec = tv.Float64()
			} else {
				return mrb.ETypeError().Raise("timeout must be Numeric or nil")
			}
			if sec < 0 {
				return mrb.EArgumentError().Raise("time interval must be positive")
			}
			hasTimeout = true
			timeout = time.Duration(sec * float64(time.Second))
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return mrb.Raise(mrb.ExcGet("IOError"), "selector is closed")
		}
		wp := s.wp
		mons := make([]*nioMonitor, len(s.monitors))
		copy(mons, s.monitors)
		entries := make([]pollEntry, len(mons))
		for i, mon := range mons {
			mon.readyR = false
			mon.readyW = false
			entries[i] = pollEntry{
				fd:    mon.fd,
				wantR: mon.wantR,
				wantW: mon.wantW,
			}
		}
		s.mu.Unlock()

		ctx := mrb.GoContext()
		ready, woken, err := pollEntries(ctx, mrb.SignalWake(), wp, entries, hasTimeout, timeout)
		if ctxErr := mrb.ContextErr(ctx); ctxErr != nil {
			return mrb.RaiseError(ctxErr)
		}
		if sigErr := mrb.CheckSignals(); sigErr != nil {
			return mrb.RaiseError(sigErr)
		}
		if err != nil {
			return mrb.RaiseError(err)
		}

		// Timed out with neither ready monitors nor wakeup
		if ready == 0 && !woken {
			return mrb.NilValue()
		}

		if blk.IsNil() {
			out := mrb.AryNewCapa(ready)
			for i, e := range entries {
				if e.readyR || e.readyW {
					mons[i].readyR = e.readyR
					mons[i].readyW = e.readyW
					mrb.AryPush(out, mons[i].selfVal)
				}
			}
			return out
		}

		count := 0
		for i, e := range entries {
			if e.readyR || e.readyW {
				mons[i].readyR = e.readyR
				mons[i].readyW = e.readyW
				count++
				if _, yErr := mrb.YieldE(blk, mons[i].selfVal); yErr != nil {
					return mrb.RaiseError(yErr)
				}
			}
		}
		return mrb.FixnumValue(count)
	}, mrb.ArgsOpt(1)|mrb.ArgsBlock())
}

func defineMonitor(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Monitor", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineMethod("io", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getMon(mrb, self).ioVal
	}, mrb.ArgsNone())

	cls.DefineMethod("selector", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getMon(mrb, self).selVal
	}, mrb.ArgsNone())

	cls.DefineMethod("interests", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getMon(mrb, self).interestsSym(mrb)
	}, mrb.ArgsNone())

	cls.DefineMethod("interests=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mon := getMon(mrb, self)
		v := mrb.GetArgsFirst()
		r, w, err := parseInterests(mrb, v)
		if err != nil {
			return mrb.EArgumentError().Raise(err.Error())
		}
		mon.wantR, mon.wantW = r, w
		return v
	}, mrb.ArgsReq(1))

	cls.DefineMethod("add_interest", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mon := getMon(mrb, self)
		r, w, err := parseInterests(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.EArgumentError().Raise(err.Error())
		}
		mon.wantR = mon.wantR || r
		mon.wantW = mon.wantW || w
		return mon.interestsSym(mrb)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("remove_interest", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mon := getMon(mrb, self)
		r, w, err := parseInterests(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.EArgumentError().Raise(err.Error())
		}
		if r {
			mon.wantR = false
		}
		if w {
			mon.wantW = false
		}
		return mon.interestsSym(mrb)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("readiness", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getMon(mrb, self).readinessSym(mrb)
	}, mrb.ArgsNone())

	cls.DefineMethod("readable?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getMon(mrb, self).readyR)
	}, mrb.ArgsNone())

	writableFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getMon(mrb, self).readyW)
	}
	cls.DefineMethod("writable?", writableFn, mrb.ArgsNone())
	cls.DefineMethod("writeable?", writableFn, mrb.ArgsNone())

	cls.DefineMethod("value", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getMon(mrb, self).value
	}, mrb.ArgsNone())

	cls.DefineMethod("value=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		getMon(mrb, self).value = v
		mrb.IVSet(self, mrb.Intern("@__val"), v)
		return v
	}, mrb.ArgsReq(1))

	cls.DefineMethod("closed?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getMon(mrb, self).closed)
	}, mrb.ArgsNone())

	cls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mon := getMon(mrb, self)
		dereg := true
		args := mrb.GetArgs()
		if args.Len() >= 1 {
			dereg = args.Item(0).Bool()
		}
		if !mon.closed {
			mon.closed = true
			if dereg && mon.sel != nil {
				_, _ = mrb.Funcall(mon.selVal, mrb.Intern("deregister"), mon.ioVal)
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsOpt(1))
}

// ---------------------------------------------------------------------------
// NIO::ByteBuffer
// ---------------------------------------------------------------------------

type nioByteBuffer struct {
	buf  []byte
	pos  int
	lim  int
	mark int
}

func getBB(mrb *oruby.MrbState, self oruby.Value) *nioByteBuffer {
	b, _ := rb.Get[*nioByteBuffer](mrb, self)
	return b
}

func defineByteBuffer(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "ByteBuffer", mrb.ObjectClass())
	rb.DataClass(cls)
	mrb.IncludeModule(cls, mrb.ModuleGet("Enumerable"))

	stdErr := mrb.ExcGet("StandardError")
	mrb.DefineClassUnder(cls, "OverflowError", stdErr)
	mrb.DefineClassUnder(cls, "UnderflowError", stdErr)
	mrb.DefineClassUnder(cls, "MarkUnsetError", stdErr)

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		capN := mrb.GetArgsFirst().Int()
		if capN < 0 {
			return mrb.EArgumentError().Raise("capacity must be non-negative")
		}
		bb := &nioByteBuffer{
			buf:  make([]byte, capN),
			pos:  0,
			lim:  capN,
			mark: -1,
		}
		rb.Set(mrb, self, bb)
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("clear", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		bb.pos = 0
		bb.lim = len(bb.buf)
		bb.mark = -1
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("position", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getBB(mrb, self).pos)
	}, mrb.ArgsNone())

	cls.DefineMethod("position=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		p := mrb.GetArgsFirst().Int()
		if p < 0 || p > bb.lim {
			return mrb.EArgumentError().Raisef("position %d out of range 0..%d", p, bb.lim)
		}
		bb.pos = p
		if bb.mark > p {
			bb.mark = -1
		}
		return mrb.FixnumValue(p)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("limit", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getBB(mrb, self).lim)
	}, mrb.ArgsNone())

	cls.DefineMethod("limit=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		l := mrb.GetArgsFirst().Int()
		if l < 0 || l > len(bb.buf) {
			return mrb.EArgumentError().Raisef("limit %d out of range 0..%d", l, len(bb.buf))
		}
		bb.lim = l
		if bb.pos > l {
			bb.pos = l
		}
		if bb.mark > l {
			bb.mark = -1
		}
		return mrb.FixnumValue(l)
	}, mrb.ArgsReq(1))

	capFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(len(getBB(mrb, self).buf))
	}
	cls.DefineMethod("capacity", capFn, mrb.ArgsNone())
	cls.DefineMethod("size", capFn, mrb.ArgsNone())

	cls.DefineMethod("remaining", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		return mrb.FixnumValue(bb.lim - bb.pos)
	}, mrb.ArgsNone())

	cls.DefineMethod("full?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		return mrb.BoolValue(bb.pos == bb.lim)
	}, mrb.ArgsNone())

	cls.DefineMethod("flip", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		bb.lim = bb.pos
		bb.pos = 0
		bb.mark = -1
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("rewind", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		bb.pos = 0
		bb.mark = -1
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("mark", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		bb.mark = bb.pos
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		if bb.mark < 0 {
			return rb.Raise(mrb, "NIO::ByteBuffer::MarkUnsetError", "mark has not been set")
		}
		bb.pos = bb.mark
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("compact", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		rem := bb.lim - bb.pos
		copy(bb.buf[:rem], bb.buf[bb.pos:bb.lim])
		bb.pos = rem
		bb.lim = len(bb.buf)
		bb.mark = -1
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("[]", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		idx := mrb.GetArgsFirst().Int()
		if idx < 0 {
			idx = bb.lim + idx
		}
		if idx < 0 || idx >= bb.lim {
			return mrb.EIndexError().Raisef("index %d out of range", idx)
		}
		return mrb.FixnumValue(int(bb.buf[idx]))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("get", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		args := mrb.GetArgs()
		rem := bb.lim - bb.pos
		n := rem
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			n = args.Item(0).Int()
			if n < 0 {
				return mrb.EArgumentError().Raise("negative length")
			}
			if n > rem {
				return rb.Raise(mrb, "NIO::ByteBuffer::UnderflowError", "not enough bytes remaining")
			}
		}
		out := mrb.Call(mrb.BytesValue(bb.buf[bb.pos:bb.pos+n]), "b")
		bb.pos += n
		return out
	}, mrb.ArgsOpt(1))

	cls.DefineMethod("<<", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		str := mrb.GetArgsFirst()
		if !str.IsString() {
			return mrb.ETypeError().Raise("expected String")
		}
		bs := str.Bytes()
		if len(bs) > bb.lim-bb.pos {
			return rb.Raise(mrb, "NIO::ByteBuffer::OverflowError", "buffer is full")
		}
		copy(bb.buf[bb.pos:], bs)
		bb.pos += len(bs)
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("each", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		blk := mrb.GetArgsBlock()
		if blk.IsNil() {
			return mrb.Call(self, "to_enum", mrb.Intern("each").Value())
		}
		bb := getBB(mrb, self)
		for i := bb.pos; i < bb.lim; i++ {
			if _, err := mrb.YieldE(blk, mrb.FixnumValue(int(bb.buf[i]))); err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}, mrb.ArgsBlock())

	cls.DefineMethod("inspect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		bb := getBB(mrb, self)
		return mrb.StrNew(fmt.Sprintf("#<NIO::ByteBuffer @position=%d @limit=%d @capacity=%d>", bb.pos, bb.lim, len(bb.buf)))
	}, mrb.ArgsNone())
}
