package io

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/oruby/oruby"
)

// The read buffer of an IO, as MRI's io.c keeps one (rbuf): bytes read ahead
// of the program and bytes pushed back by ungetc/ungetbyte. It lives in a
// hidden instance variable, a binary String, so it goes with the object.
//
// Every read goes through it (streamReader), so pushed-back bytes come out
// first and line reads take the descriptor's data in chunks instead of a
// system call per byte. The descriptor's own position is ahead of the
// program's by len(rbuf): pos subtracts it, seek and rewind drop it, and a
// write first gives the read-ahead back (seeks over it) so the bytes land
// where the program is.

const rbufChunk = 8192

func rbufSym(mrb *oruby.MrbState) oruby.MrbSym { return mrb.Intern("__oruby_rbuf") }
func rposSym(mrb *oruby.MrbState) oruby.MrbSym { return mrb.Intern("__oruby_rpos") }

// The buffer is a String and an offset into it: taking bytes moves the
// offset, so a byte at a time does not copy the rest; a refill or a
// pushback makes a new String.

// rbufView: the unread bytes, a view into the String (valid until the next
// change of the buffer), or nil
func rbufView(mrb *oruby.MrbState, self oruby.Value) []byte {
	v := mrb.IVGet(self, rbufSym(mrb))
	if !v.IsString() {
		return nil
	}
	p, n := oruby.RStringMem(v)
	pos := 0
	if pv := mrb.IVGet(self, rposSym(mrb)); pv.IsInteger() {
		pos = pv.Int()
	}
	if pos >= n {
		return nil
	}
	return unsafeBytes(p, n)[pos:]
}

// rbufGet: a copy of the unread bytes
func rbufGet(mrb *oruby.MrbState, self oruby.Value) []byte {
	return append([]byte(nil), rbufView(mrb, self)...)
}

func rbufSet(mrb *oruby.MrbState, self oruby.Value, b []byte) {
	if len(b) == 0 {
		if mrb.IVDefined(self, rbufSym(mrb)) {
			_ = mrb.IVSet(self, rbufSym(mrb), mrb.NilValue())
			_ = mrb.IVSet(self, rposSym(mrb), mrb.NilValue())
		}
		return
	}
	_ = mrb.IVSet(self, rbufSym(mrb), mrb.BytesValue(b))
	_ = mrb.IVSet(self, rposSym(mrb), mrb.FixnumValue(0))
}

// rbufTake consumes n buffered bytes
func rbufTake(mrb *oruby.MrbState, self oruby.Value, n int) {
	v := mrb.IVGet(self, rbufSym(mrb))
	if !v.IsString() || n <= 0 {
		return
	}
	_, size := oruby.RStringMem(v)
	pos := 0
	if pv := mrb.IVGet(self, rposSym(mrb)); pv.IsInteger() {
		pos = pv.Int()
	}
	pos += n
	if pos >= size {
		rbufSet(mrb, self, nil)
		return
	}
	_ = mrb.IVSet(self, rposSym(mrb), mrb.FixnumValue(pos))
}

func rubyBytesOf(v oruby.Value) []byte {
	p, n := oruby.RStringMem(v)
	if n == 0 {
		return nil
	}
	return append([]byte(nil), unsafeBytes(p, n)...)
}

// rbufLen: how many bytes are buffered
func rbufLen(mrb *oruby.MrbState, self oruby.Value) int {
	return len(rbufView(mrb, self))
}

// rbufUnread pushes b back in front of the buffered bytes
func rbufUnread(mrb *oruby.MrbState, self oruby.Value, b []byte) {
	if len(b) == 0 {
		return
	}
	rbufSet(mrb, self, append(append([]byte(nil), b...), rbufGet(mrb, self)...))
}

// rbufDrop forgets the read-ahead, putting the descriptor back where the
// program is (before a write or a seek)
func rbufDrop(mrb *oruby.MrbState, self oruby.Value) {
	n := rbufLen(mrb, self)
	if n == 0 {
		return
	}
	if s, ok := mrb.Data(self).(io.Seeker); ok {
		_, _ = s.Seek(-int64(n), io.SeekCurrent)
	}
	rbufSet(mrb, self, nil)
}

// ResetReadBuffer discards what self has buffered (reopen gives it another
// stream)
func ResetReadBuffer(mrb *oruby.MrbState, self oruby.Value) { rbufSet(mrb, self, nil) }

// DropReadBuffer gives the read-ahead back to the descriptor (before another
// IO takes it over)
func DropReadBuffer(mrb *oruby.MrbState, self oruby.Value) { rbufDrop(mrb, self) }

// ioBuffered is IO#__oruby_buffered?: bytes are read ahead or pushed back
func ioBuffered(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.BoolValue(rbufLen(mrb, self) > 0)
}

// SetCloseOnExec sets f's close-on-exec flag
func SetCloseOnExec(f *os.File, on bool) error { return setCloseOnExec(f, on) }

// streamReader reads an IO through its buffer
type streamReader struct {
	mrb  *oruby.MrbState
	self oruby.Value
	r    io.Reader
}

// ioReader is the reader of self, or an IOError for a stream not open for
// reading
func ioReader(mrb *oruby.MrbState, self oruby.Value) (*streamReader, error) {
	data := mrb.Data(self)
	if data == nil || isClosed(mrb, self) {
		return nil, oruby.EError("IOError", "closed stream")
	}
	r, ok := data.(io.Reader)
	if !ok || r == nil || !rubyReadable(mrb, self) {
		return nil, oruby.EError("IOError", "not opened for reading")
	}
	return &streamReader{mrb, self, interruptible(mrb, r)}, nil
}

// rubyReadable asks the Ruby side (33_io_encoding.rb) about the open mode;
// without one the descriptor decides
func rubyReadable(mrb *oruby.MrbState, self oruby.Value) bool {
	if !mrb.RespondTo(self, mrb.Intern("__oruby_readable?")) {
		return true
	}
	v, err := mrb.Funcall(self, mrb.Intern("__oruby_readable?"))
	return err != nil || v.Bool()
}

func rubyWritable(mrb *oruby.MrbState, self oruby.Value) bool {
	if !mrb.RespondTo(self, mrb.Intern("__oruby_writable?")) {
		return true
	}
	v, err := mrb.Funcall(self, mrb.Intern("__oruby_writable?"))
	return err != nil || v.Bool()
}

// Read takes buffered bytes first; with none it reads a chunk ahead (what
// the descriptor has: a pipe does not wait for more)
func (s *streamReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(rbufView(s.mrb, s.self)) == 0 {
		more, err := s.fill()
		if err != nil {
			return 0, err
		}
		if !more {
			return 0, io.EOF
		}
	}
	n := copy(p, rbufView(s.mrb, s.self))
	rbufTake(s.mrb, s.self, n)
	return n, nil
}

// fill reads a chunk ahead into the buffer; false at EOF
func (s *streamReader) fill() (bool, error) {
	chunk := make([]byte, rbufChunk)
	n, err := s.r.Read(chunk)
	if n > 0 {
		rbufSet(s.mrb, s.self, append(rbufGet(s.mrb, s.self), chunk[:n]...))
		return true, nil
	}
	if err == nil {
		return true, nil // nothing yet: read again
	}
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	return false, mapReadErr(err)
}

// ReadByte is one byte, nil-safe at EOF (io.EOF)
func (s *streamReader) ReadByte() (byte, error) {
	var b [1]byte
	for {
		n, err := s.Read(b[:])
		if n == 1 {
			return b[0], nil
		}
		if err != nil {
			return 0, err
		}
	}
}

// ReadUntil reads up to and including sep, or limit bytes (limit > 0), or
// to EOF; through the buffer, a chunk at a time
func (s *streamReader) ReadUntil(sep []byte, limit int) ([]byte, error) {
	var out []byte
	for {
		buf := rbufView(s.mrb, s.self)
		if len(buf) > 0 {
			take := len(buf)
			if len(sep) > 0 {
				// the separator may straddle what was read before
				start := len(out) - len(sep) + 1
				if start < 0 {
					start = 0
				}
				joined := append(append([]byte(nil), out[start:]...), buf...)
				if i := bytes.Index(joined, sep); i >= 0 {
					take = i + len(sep) - (len(out) - start)
				}
			}
			if limit > 0 && len(out)+take > limit {
				take = limit - len(out)
			}
			out = append(out, buf[:take]...)
			rbufTake(s.mrb, s.self, take)
			if (len(sep) > 0 && bytes.HasSuffix(out, sep)) || (limit > 0 && len(out) >= limit) {
				return out, nil
			}
			continue
		}
		more, err := s.fill()
		if err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, err
		}
		if !more {
			if len(out) > 0 {
				return out, nil
			}
			return nil, io.EOF
		}
	}
}

// ReadN reads n bytes (fewer only at EOF)
func (s *streamReader) ReadN(n int) ([]byte, error) {
	out := make([]byte, 0, n)
	for len(out) < n {
		if buf := rbufView(s.mrb, s.self); len(buf) > 0 {
			take := n - len(out)
			if take > len(buf) {
				take = len(buf)
			}
			out = append(out, buf[:take]...)
			rbufTake(s.mrb, s.self, take)
			continue
		}
		// what is left goes straight to the caller, no copy through the buffer
		chunk := make([]byte, n-len(out))
		m, err := s.r.Read(chunk)
		out = append(out, chunk[:m]...)
		if err != nil {
			if errors.Is(err, io.EOF) || len(out) > 0 {
				break
			}
			return nil, mapReadErr(err)
		}
		if m == 0 {
			break
		}
	}
	if len(out) == 0 && n > 0 {
		return nil, io.EOF
	}
	return out, nil
}

// ReadAll reads to EOF
func (s *streamReader) ReadAll() ([]byte, error) {
	out := rbufGet(s.mrb, s.self)
	rbufSet(s.mrb, s.self, nil)
	rest, err := io.ReadAll(s.r)
	out = append(out, rest...)
	if err != nil && len(out) == 0 {
		return nil, mapReadErr(err)
	}
	return out, nil
}

// Partial is readpartial: the buffered bytes if any, else what one read of
// the descriptor gives
func (s *streamReader) Partial(n int) ([]byte, error) {
	if len(rbufView(s.mrb, s.self)) == 0 {
		more, err := s.fill()
		if err != nil {
			return nil, err
		}
		if !more {
			return nil, io.EOF
		}
	}
	buf := rbufView(s.mrb, s.self)
	if n > len(buf) {
		n = len(buf)
	}
	out := append([]byte(nil), buf[:n]...)
	rbufTake(s.mrb, s.self, n)
	return out, nil
}

// Peek tells whether a byte is there without taking it
func (s *streamReader) AtEOF() (bool, error) {
	for rbufLen(s.mrb, s.self) == 0 {
		more, err := s.fill()
		if err != nil {
			return false, err
		}
		if !more {
			return true, nil
		}
	}
	return false, nil
}

// mapReadErr: a descriptor not open for reading is MRI's IOError; one
// closed while the read waited (a closed stream is refused before it
// starts) is MRI's IOError for a close in another thread
func mapReadErr(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	if errors.Is(err, os.ErrClosed) {
		return oruby.EError("IOError", "stream closed in another thread")
	}
	if errors.Is(err, syscall.EBADF) {
		return oruby.EError("IOError", "not opened for reading")
	}
	return err
}
