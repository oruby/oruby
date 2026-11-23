// Package brotli provides a pure-Go implementation of the Ruby brotli gem
// backed by github.com/andybalholm/brotli (RFC 7932).
package brotli

import (
	"bytes"
	"io"

	"github.com/andybalholm/brotli"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	oruby.LazyGem("brotli", initBrotli)
}

type writerWrap struct {
	ioVal  oruby.Value
	buf    bytes.Buffer
	w      *brotli.Writer
	closed bool
}

type readerWrap struct {
	ioVal  oruby.Value
	r      *brotli.Reader
	buf    *bytes.Reader
	eof    bool
	closed bool
}

func initBrotli(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Brotli")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("0.6.0"))
	errCls := mrb.DefineClassUnder(mod, "Error", mrb.ExcGet("StandardError"))

	mod.DefineClassMethod("deflate", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		data, err := rb.Bytes(mrb, args.Item(0))
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		quality := brotli.DefaultCompression
		lgwin := 22
		if args.Len() >= 2 && args.Item(1).IsHash() {
			quality, lgwin = parseBrotliOpts(mrb, args.Item(1), quality, lgwin)
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			quality, lgwin = parseBrotliOpts(mrb, kw, quality, lgwin)
		}
		var out bytes.Buffer
		bw := brotli.NewWriterOptions(&out, brotli.WriterOptions{Quality: quality, LGWin: lgwin})
		if _, err := bw.Write(data); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := bw.Close(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out.Bytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	mod.DefineClassMethod("inflate", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		br := brotli.NewReader(bytes.NewReader(data))
		out, err := io.ReadAll(br)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	// Brotli::Writer
	wCls := rb.DataClass(mrb.DefineClassUnder(mod, "Writer", mrb.ObjectClass()))
	wCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		ioVal := args.Item(0)
		quality := brotli.DefaultCompression
		lgwin := 22
		if args.Len() >= 2 && args.Item(1).IsHash() {
			quality, lgwin = parseBrotliOpts(mrb, args.Item(1), quality, lgwin)
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			quality, lgwin = parseBrotliOpts(mrb, kw, quality, lgwin)
		}
		ww := &writerWrap{ioVal: ioVal}
		ww.w = brotli.NewWriterOptions(&ww.buf, brotli.WriterOptions{Quality: quality, LGWin: lgwin})
		rb.Set(mrb, self, ww)
		mrb.SetIV(self, "@io", ioVal)
		return self
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	flushBuf := func(mrb *oruby.MrbState, ww *writerWrap) error {
		if ww.buf.Len() == 0 {
			return nil
		}
		chunk := mrb.Call(mrb.BytesValue(ww.buf.Bytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		ww.buf.Reset()
		_, err := mrb.Funcall(ww.ioVal, mrb.Intern("write"), chunk)
		return err
	}

	wCls.DefineMethod("write", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ww, ok := rb.Get[*writerWrap](mrb, self)
		if !ok || ww.closed {
			return mrb.Raise(errCls, "closed Brotli::Writer")
		}
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		n, err := ww.w.Write(data)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := flushBuf(mrb, ww); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.FixnumValue(n)
	}, mrb.ArgsReq(1))
	wCls.DefineAlias("<<", "write")

	wCls.DefineMethod("flush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ww, ok := rb.Get[*writerWrap](mrb, self)
		if !ok || ww.closed {
			return mrb.Raise(errCls, "closed Brotli::Writer")
		}
		if err := ww.w.Flush(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := flushBuf(mrb, ww); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsNone())

	wCls.DefineMethod("finish", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ww, ok := rb.Get[*writerWrap](mrb, self)
		if !ok || ww.closed {
			return mrb.GetIV(self, "@io")
		}
		ww.closed = true
		if err := ww.w.Close(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := flushBuf(mrb, ww); err != nil {
			return mrb.RaiseError(err)
		}
		return ww.ioVal
	}, mrb.ArgsNone())

	wCls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ww, ok := rb.Get[*writerWrap](mrb, self)
		if !ok || ww.closed {
			return mrb.NilValue()
		}
		ww.closed = true
		if err := ww.w.Close(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := flushBuf(mrb, ww); err != nil {
			return mrb.RaiseError(err)
		}
		if mrb.RespondTo(ww.ioVal, mrb.Intern("close")) {
			_, _ = mrb.Funcall(ww.ioVal, mrb.Intern("close"))
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	// Brotli::Reader
	rCls := rb.DataClass(mrb.DefineClassUnder(mod, "Reader", mrb.ObjectClass()))
	rCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ioVal := mrb.GetArgsFirst()
		rawVal, err := mrb.Funcall(ioVal, mrb.Intern("read"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		var raw []byte
		if !rawVal.IsNil() {
			raw = rawVal.Bytes()
		}
		br := bytes.NewReader(raw)
		rw := &readerWrap{ioVal: ioVal, buf: br, r: brotli.NewReader(br)}
		rb.Set(mrb, self, rw)
		mrb.SetIV(self, "@io", ioVal)
		return self
	}, mrb.ArgsReq(1))

	rCls.DefineMethod("read", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rw, ok := rb.Get[*readerWrap](mrb, self)
		if !ok || rw.closed {
			return mrb.Raise(errCls, "closed Brotli::Reader")
		}
		args := mrb.GetArgs().Slice()
		if len(args) == 0 || args[0].IsNil() {
			out, err := io.ReadAll(rw.r)
			if err != nil {
				return mrb.Raise(errCls, err.Error())
			}
			rw.eof = true
			return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		want := int(args[0].Int64())
		if want == 0 {
			return mrb.Call(mrb.StrNew(""), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		buf := make([]byte, want)
		n, err := io.ReadFull(rw.r, buf)
		if n == 0 && (err == io.EOF || err == io.ErrUnexpectedEOF) {
			rw.eof = true
			return mrb.NilValue()
		}
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return mrb.Raise(errCls, err.Error())
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			rw.eof = true
		}
		return mrb.Call(mrb.BytesValue(buf[:n]), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsOpt(1))

	rCls.DefineMethod("eof?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rw, _ := rb.Get[*readerWrap](mrb, self)
		return mrb.BoolValue(rw == nil || rw.eof || rw.closed)
	}, mrb.ArgsNone())

	rCls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rw, _ := rb.Get[*readerWrap](mrb, self)
		if rw != nil {
			rw.closed = true
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	return nil
}

func parseBrotliOpts(mrb *oruby.MrbState, h oruby.Value, quality, lgwin int) (int, int) {
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("quality").Value()); err == nil && v.IsInt() {
		quality = int(v.Int64())
	}
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("lgwin").Value()); err == nil && v.IsInt() {
		lgwin = int(v.Int64())
	}
	return quality, lgwin
}
