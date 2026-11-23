// Package zstd provides a pure-Go implementation of the zstd-ruby gem
// backed by github.com/klauspost/compress/zstd (RFC 8878).
package zstd

import (
	"bytes"

	"github.com/klauspost/compress/zstd"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	oruby.LazyGem("zstd-ruby", initZstd)
	oruby.GemAlias("zstd", "zstd-ruby")
}

func zstdLevel(lvl int) zstd.EncoderLevel {
	switch {
	case lvl <= 1:
		return zstd.SpeedFastest
	case lvl <= 4:
		return zstd.SpeedDefault
	case lvl <= 9:
		return zstd.SpeedBetterCompression
	default:
		return zstd.SpeedBestCompression
	}
}

type streamEnc struct {
	buf bytes.Buffer
	enc *zstd.Encoder
}

type streamDec struct {
	buf bytes.Buffer
}

func initZstd(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Zstd")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("1.5.4"))
	errCls := mrb.DefineClassUnder(mod, "Error", mrb.ExcGet("StandardError"))

	mod.DefineClassMethod("compress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		data, err := rb.Bytes(mrb, args.Item(0))
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		level := 3
		var dict []byte
		if args.Len() >= 2 && args.Item(1).IsHash() {
			level, dict = parseZstdOpts(mrb, args.Item(1), level, dict)
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			level, dict = parseZstdOpts(mrb, kw, level, dict)
		}
		opts := []zstd.EOption{zstd.WithEncoderLevel(zstdLevel(level))}
		if len(dict) > 0 {
			opts = append(opts, zstd.WithEncoderDict(dict))
		}
		enc, err := zstd.NewWriter(nil, opts...)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		defer enc.Close()
		out := enc.EncodeAll(data, nil)
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	mod.DefineClassMethod("decompress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		data, err := rb.Bytes(mrb, args.Item(0))
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		var dict []byte
		if args.Len() >= 2 && args.Item(1).IsHash() {
			_, dict = parseZstdOpts(mrb, args.Item(1), 0, dict)
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			_, dict = parseZstdOpts(mrb, kw, 0, dict)
		}
		var opts []zstd.DOption
		if len(dict) > 0 {
			opts = append(opts, zstd.WithDecoderDicts(dict))
		}
		dec, err := zstd.NewReader(nil, opts...)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		defer dec.Close()
		out, err := dec.DecodeAll(data, nil)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1)|mrb.ArgsKey(0, 1))

	// Zstd::StreamingCompress
	scCls := rb.DataClass(mrb.DefineClassUnder(mod, "StreamingCompress", mrb.ObjectClass()))
	scCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		level := 3
		if args := mrb.GetArgs(); args.Len() >= 1 && args.Item(0).IsHash() {
			level, _ = parseZstdOpts(mrb, args.Item(0), level, nil)
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			level, _ = parseZstdOpts(mrb, kw, level, nil)
		}
		se := &streamEnc{}
		enc, err := zstd.NewWriter(&se.buf, zstd.WithEncoderLevel(zstdLevel(level)))
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		se.enc = enc
		rb.Set(mrb, self, se)
		return self
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	scCls.DefineMethod("compress", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		se, ok := rb.Get[*streamEnc](mrb, self)
		if !ok || se.enc == nil {
			return mrb.Raise(errCls, "StreamingCompress closed")
		}
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		if _, err := se.enc.Write(data); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		out := append([]byte(nil), se.buf.Bytes()...)
		se.buf.Reset()
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))
	scCls.DefineAlias("<<", "compress")

	scCls.DefineMethod("flush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		se, ok := rb.Get[*streamEnc](mrb, self)
		if !ok || se.enc == nil {
			return mrb.Raise(errCls, "StreamingCompress closed")
		}
		if err := se.enc.Flush(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		out := append([]byte(nil), se.buf.Bytes()...)
		se.buf.Reset()
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	scCls.DefineMethod("finish", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		se, ok := rb.Get[*streamEnc](mrb, self)
		if !ok || se.enc == nil {
			return mrb.Call(mrb.StrNew(""), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		if err := se.enc.Close(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		se.enc = nil
		out := append([]byte(nil), se.buf.Bytes()...)
		se.buf.Reset()
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	// Zstd::StreamingDecompress
	sdCls := rb.DataClass(mrb.DefineClassUnder(mod, "StreamingDecompress", mrb.ObjectClass()))
	sdCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, &streamDec{})
		return self
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	sdCls.DefineMethod("decompress", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sd, ok := rb.Get[*streamDec](mrb, self)
		if !ok {
			return mrb.Raise(errCls, "StreamingDecompress not initialized")
		}
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		sd.buf.Write(data)
		dec, err := zstd.NewReader(nil)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		defer dec.Close()
		out, err := dec.DecodeAll(sd.buf.Bytes(), nil)
		if err != nil {
			// Incomplete frame; buffer until complete
			return mrb.Call(mrb.StrNew(""), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		sd.buf.Reset()
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))
	sdCls.DefineAlias("<<", "decompress")

	return nil
}

func parseZstdOpts(mrb *oruby.MrbState, h oruby.Value, level int, dict []byte) (int, []byte) {
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("level").Value()); err == nil && v.IsInt() {
		level = int(v.Int64())
	}
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("dict").Value()); err == nil && v.IsString() {
		dict = v.Bytes()
	}
	return level, dict
}
