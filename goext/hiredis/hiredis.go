// Package hiredis provides a pure-Go implementation of the Ruby hiredis gem
// (Hiredis::Reader for RESP2/RESP3 and Hiredis::Connection for TCP/Unix Redis).
package hiredis

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

func init() {
	oruby.LazyGem("hiredis", initHiredis)
	for _, alias := range []string{
		"hiredis/connection",
		"hiredis/reader",
		"hiredis/ext/connection",
		"hiredis/ext/reader",
		"hiredis/ext/hiredis_ext",
		"hiredis/version",
	} {
		oruby.GemAlias(alias, "hiredis")
	}
}

// ---------------------------------------------------------------------------
// Incremental RESP2 / RESP3 Reader
// ---------------------------------------------------------------------------

var errIncomplete = errors.New("incomplete RESP frame")

type respReader struct {
	buf []byte
	pos int
}

func (r *respReader) feed(data []byte) {
	if r.pos > 0 && r.pos == len(r.buf) {
		r.buf = r.buf[:0]
		r.pos = 0
	} else if r.pos > 4096 {
		copy(r.buf, r.buf[r.pos:])
		r.buf = r.buf[:len(r.buf)-r.pos]
		r.pos = 0
	}
	r.buf = append(r.buf, data...)
}

func (r *respReader) hasData() bool {
	return r.pos < len(r.buf)
}

func (r *respReader) readLine(off int) ([]byte, int, error) {
	idx := bytes.Index(r.buf[off:], []byte("\r\n"))
	if idx < 0 {
		return nil, off, errIncomplete
	}
	return r.buf[off : off+idx], off + idx + 2, nil
}

func (r *respReader) parseAt(mrb *oruby.MrbState, off int) (oruby.Value, int, error) {
	if off >= len(r.buf) {
		return mrb.FalseValue(), off, errIncomplete
	}
	prefix := r.buf[off]
	off++
	switch prefix {
	case '+': // Simple String
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		return mrb.StrNew(string(line)), next, nil

	case '-': // Simple Error -> RuntimeError object (matches hiredis-rb)
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		exc, _ := mrb.Funcall(mrb.ExcGet("RuntimeError"), mrb.Intern("new"), mrb.StrNew(string(line)))
		return exc, next, nil

	case ':': // Integer
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		iv, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return mrb.FalseValue(), off, fmt.Errorf("invalid RESP integer: %q", string(line))
		}
		return mrb.Int64Value(iv), next, nil

	case '$': // Bulk String
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return mrb.FalseValue(), off, fmt.Errorf("invalid RESP bulk length: %q", string(line))
		}
		if n < 0 {
			return mrb.NilValue(), next, nil
		}
		end := next + int(n)
		if end+2 > len(r.buf) {
			return mrb.FalseValue(), off, errIncomplete
		}
		if r.buf[end] != '\r' || r.buf[end+1] != '\n' {
			return mrb.FalseValue(), off, fmt.Errorf("expected CRLF after bulk string")
		}
		return mrb.BytesValue(r.buf[next:end]), end + 2, nil

	case '*', '~', '>': // Array / RESP3 Set / RESP3 Push
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return mrb.FalseValue(), off, fmt.Errorf("invalid RESP array length: %q", string(line))
		}
		if n < 0 {
			return mrb.NilValue(), next, nil
		}
		ary := mrb.AryNewCapa(int(n))
		cur := next
		for i := int64(0); i < n; i++ {
			item, nxt, err := r.parseAt(mrb, cur)
			if err != nil {
				return mrb.FalseValue(), off, err
			}
			mrb.AryPush(ary, item)
			cur = nxt
		}
		return ary.Value(), cur, nil

	case '_': // RESP3 Null
		_, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		return mrb.NilValue(), next, nil

	case '#': // RESP3 Boolean
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		return mrb.BoolValue(len(line) > 0 && line[0] == 't'), next, nil

	case ',': // RESP3 Double
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		s := string(line)
		var fv float64
		switch strings.ToLower(s) {
		case "inf":
			fv = math.Inf(1)
		case "-inf":
			fv = math.Inf(-1)
		case "nan":
			fv = math.NaN()
		default:
			var err2 error
			fv, err2 = strconv.ParseFloat(s, 64)
			if err2 != nil {
				return mrb.FalseValue(), off, fmt.Errorf("invalid RESP3 double: %q", s)
			}
		}
		return mrb.FloatValue(fv), next, nil

	case '(': // RESP3 Big Number
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		if iv, err := strconv.ParseInt(string(line), 10, 64); err == nil {
			return mrb.Int64Value(iv), next, nil
		}
		if bi, err := mrb.Funcall(mrb.StrNew(string(line)), oruby.Sym_to_i); err == nil {
			return bi, next, nil
		}
		return mrb.StrNew(string(line)), next, nil

	case '!': // RESP3 Blob Error
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil || n < 0 {
			return mrb.FalseValue(), off, fmt.Errorf("invalid RESP3 blob error length")
		}
		end := next + int(n)
		if end+2 > len(r.buf) {
			return mrb.FalseValue(), off, errIncomplete
		}
		exc, _ := mrb.Funcall(mrb.ExcGet("RuntimeError"), mrb.Intern("new"), mrb.StrNew(string(r.buf[next:end])))
		return exc, end + 2, nil

	case '=': // RESP3 Verbatim String (3-byte encoding + ':' + data)
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil || n < 0 {
			return mrb.FalseValue(), off, fmt.Errorf("invalid RESP3 verbatim length")
		}
		end := next + int(n)
		if end+2 > len(r.buf) {
			return mrb.FalseValue(), off, errIncomplete
		}
		body := r.buf[next:end]
		if len(body) >= 4 && body[3] == ':' {
			body = body[4:]
		}
		return mrb.BytesValue(body), end + 2, nil

	case '%': // RESP3 Map -> Hash
		line, next, err := r.readLine(off)
		if err != nil {
			return mrb.FalseValue(), off, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil || n < 0 {
			return mrb.FalseValue(), off, fmt.Errorf("invalid RESP3 map length")
		}
		h := mrb.HashNewCapa(int(n))
		cur := next
		for i := int64(0); i < n; i++ {
			k, nxt1, err := r.parseAt(mrb, cur)
			if err != nil {
				return mrb.FalseValue(), off, err
			}
			v, nxt2, err := r.parseAt(mrb, nxt1)
			if err != nil {
				return mrb.FalseValue(), off, err
			}
			mrb.HashSet(h, k, v)
			cur = nxt2
		}
		return h.Value(), cur, nil

	default:
		return mrb.FalseValue(), off, fmt.Errorf("RESP protocol error, got %q as reply type byte", prefix)
	}
}

func (r *respReader) gets(mrb *oruby.MrbState) (oruby.Value, bool, error) {
	val, next, err := r.parseAt(mrb, r.pos)
	if err != nil {
		if errors.Is(err, errIncomplete) {
			return mrb.FalseValue(), false, nil
		}
		// Reset buffer on protocol error
		r.buf = r.buf[:0]
		r.pos = 0
		return mrb.NilValue(), false, err
	}
	r.pos = next
	if r.pos == len(r.buf) {
		r.buf = r.buf[:0]
		r.pos = 0
	}
	return val, true, nil
}

// ---------------------------------------------------------------------------
// RESP Command Encoder & Hiredis::Connection
// ---------------------------------------------------------------------------

func encodeRESPCommand(mrb *oruby.MrbState, ary oruby.Value) ([]byte, error) {
	if !ary.IsArray() {
		return nil, errors.New("expected Array of command arguments")
	}
	n := oruby.RArrayLen(ary)
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", n)
	for i := 0; i < n; i++ {
		item := mrb.AryRef(ary, i)
		var raw []byte
		if item.IsString() {
			raw = item.Bytes()
		} else {
			sv, err := mrb.Funcall(item, oruby.Sym_to_s)
			if err != nil {
				return nil, err
			}
			raw = sv.Bytes()
		}
		fmt.Fprintf(&b, "$%d\r\n", len(raw))
		b.Write(raw)
		b.WriteString("\r\n")
	}
	return b.Bytes(), nil
}

type hiredisConn struct {
	conn    net.Conn
	timeout time.Duration // 0 = no timeout
	wbuf    bytes.Buffer
	reader  respReader
}

func (c *hiredisConn) setDeadline() {
	if c.conn == nil {
		return
	}
	if c.timeout > 0 {
		_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	} else {
		_ = c.conn.SetDeadline(time.Time{})
	}
}

func (c *hiredisConn) flush(mrb *oruby.MrbState) error {
	if c.conn == nil {
		return errors.New("not connected")
	}
	if c.wbuf.Len() == 0 {
		return nil
	}
	data := c.wbuf.Bytes()
	c.setDeadline()
	var err error
	mrb.Blocking(func() {
		_, err = c.conn.Write(data)
	})
	c.wbuf.Reset()
	return err
}

func (c *hiredisConn) readReply(mrb *oruby.MrbState) (oruby.Value, error) {
	if c.conn == nil {
		return mrb.NilValue(), errors.New("not connected")
	}
	if err := c.flush(mrb); err != nil {
		return mrb.NilValue(), err
	}
	tmp := make([]byte, 8192)
	for {
		val, ok, err := c.reader.gets(mrb)
		if err != nil {
			return mrb.NilValue(), err
		}
		if ok {
			return val, nil
		}
		c.setDeadline()
		var n int
		var rerr error
		mrb.Blocking(func() {
			n, rerr = c.conn.Read(tmp)
		})
		if n > 0 {
			c.reader.feed(tmp[:n])
		}
		if rerr != nil {
			if n > 0 {
				if val, ok, err := c.reader.gets(mrb); err == nil && ok {
					return val, nil
				}
			}
			return mrb.NilValue(), rerr
		}
	}
}

func initHiredis(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Hiredis")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("0.6.3"))
	extMod := mrb.DefineModuleUnder(mod, "Ext")

	protoErr := mrb.DefineClassUnder(mod, "ProtocolError", mrb.ExcGet("StandardError"))

	// Hiredis::Ext::Reader (also aliased as Hiredis::Reader)
	readerCls := rb.DataClass(mrb.DefineClassUnder(extMod, "Reader", mrb.ObjectClass()))
	mrb.ConstSet(mod, mrb.Intern("Reader"), readerCls.Value())

	readerCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, &respReader{})
		return self
	}, mrb.ArgsNone())

	readerCls.DefineMethod("feed", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, ok := rb.Get[*respReader](mrb, self)
		if !ok {
			return mrb.Raise(mrb.ERuntimeError(), "Reader not initialized")
		}
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		r.feed(data)
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	readerCls.DefineMethod("gets", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, ok := rb.Get[*respReader](mrb, self)
		if !ok {
			return mrb.Raise(mrb.ERuntimeError(), "Reader not initialized")
		}
		val, ok, err := r.gets(mrb)
		if err != nil {
			return mrb.Raise(protoErr, err.Error())
		}
		if !ok {
			return mrb.FalseValue()
		}
		return val
	}, mrb.ArgsNone())

	readerCls.DefineMethod("has_data?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := rb.Get[*respReader](mrb, self)
		return mrb.BoolValue(r != nil && r.hasData())
	}, mrb.ArgsNone())

	// Hiredis::Ext::Connection (also aliased as Hiredis::Connection)
	connCls := rb.DataClass(mrb.DefineClassUnder(extMod, "Connection", mrb.ObjectClass()))
	mrb.ConstSet(mod, mrb.Intern("Connection"), connCls.Value())

	connCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, &hiredisConn{})
		return self
	}, mrb.ArgsNone())

	getConn := func(mrb *oruby.MrbState, self oruby.Value) *hiredisConn {
		c, _ := rb.Get[*hiredisConn](mrb, self)
		if c == nil {
			c = &hiredisConn{}
			rb.Set(mrb, self, c)
		}
		return c
	}

	raiseNetErr := func(mrb *oruby.MrbState, err error) oruby.MrbValue {
		if errors.Is(err, io.EOF) {
			return mrb.Raise(mrb.ClassGet("EOFError"), "EOF")
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return rb.Raise(mrb, "Errno::EAGAIN", "%s", err.Error())
		}
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}

	connCls.DefineMethod("connect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		args := mrb.GetArgs().Slice()
		host := args[0].String()
		port := int(args[1].Int64())
		var timeout time.Duration
		if len(args) >= 3 && !args[2].IsNil() {
			timeout = time.Duration(args[2].Int64()) * time.Microsecond
		}
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		var nc net.Conn
		var err error
		mrb.Blocking(func() {
			if timeout > 0 {
				nc, err = net.DialTimeout("tcp", addr, timeout)
			} else {
				nc, err = net.Dial("tcp", addr)
			}
		})
		if err != nil {
			return raiseNetErr(mrb, err)
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.conn = nc
		c.timeout = timeout
		c.wbuf.Reset()
		c.reader = respReader{}
		return mrb.NilValue()
	}, mrb.ArgsArg(2, 1))

	connCls.DefineMethod("connect_unix", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		args := mrb.GetArgs().Slice()
		path := args[0].String()
		var timeout time.Duration
		if len(args) >= 2 && !args[1].IsNil() {
			timeout = time.Duration(args[1].Int64()) * time.Microsecond
		}
		var nc net.Conn
		var err error
		mrb.Blocking(func() {
			if timeout > 0 {
				nc, err = net.DialTimeout("unix", path, timeout)
			} else {
				nc, err = net.Dial("unix", path)
			}
		})
		if err != nil {
			return raiseNetErr(mrb, err)
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.conn = nc
		c.timeout = timeout
		c.wbuf.Reset()
		c.reader = respReader{}
		return mrb.NilValue()
	}, mrb.ArgsArg(1, 1))

	connCls.DefineMethod("connected?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		return mrb.BoolValue(c.conn != nil)
	}, mrb.ArgsNone())

	connCls.DefineMethod("disconnect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		if c.conn != nil {
			_ = c.conn.Close()
			c.conn = nil
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	connCls.DefineMethod("timeout=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			c.timeout = 0
		} else {
			c.timeout = time.Duration(v.Int64()) * time.Microsecond
		}
		return v
	}, mrb.ArgsReq(1))

	connCls.DefineMethod("write", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		if c.conn == nil {
			return mrb.Raise(mrb.ERuntimeError(), "not connected")
		}
		raw, err := encodeRESPCommand(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		c.wbuf.Write(raw)
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	connCls.DefineMethod("flush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		if err := c.flush(mrb); err != nil {
			return raiseNetErr(mrb, err)
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	connCls.DefineMethod("read", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c := getConn(mrb, self)
		val, err := c.readReply(mrb)
		if err != nil {
			return raiseNetErr(mrb, err)
		}
		return val
	}, mrb.ArgsNone())

	return nil
}
