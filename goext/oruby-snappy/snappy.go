// Package orubysnappy implements the Ruby snappy gem backed by
// github.com/klauspost/compress/s2 (Snappy-compatible pure Go),
// registered as "oruby-snappy" and aliased to "snappy".
package orubysnappy

import (
	"bytes"
	"io"

	"github.com/klauspost/compress/s2"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

func init() {
	oruby.LazyGem("oruby-snappy", initSnappy)
	oruby.GemAlias("snappy", "oruby-snappy")
}

func initSnappy(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Snappy")
	errCls := mrb.DefineClassUnder(mod, "Error", mrb.ClassGet("StandardError"))
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("0.4.0"))

	deflateFn := func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		out := s2.EncodeSnappy(nil, src)
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}
	mod.DefineClassMethod("deflate", deflateFn, mrb.ArgsReq(1))
	mod.DefineClassMethod("compress", deflateFn, mrb.ArgsReq(1))

	inflateFn := func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		out, err := s2.Decode(nil, src)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}
	mod.DefineClassMethod("inflate", inflateFn, mrb.ArgsReq(1))
	mod.DefineClassMethod("uncompress", inflateFn, mrb.ArgsReq(1))

	mod.DefineClassMethod("__frame_encode", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		var buf bytes.Buffer
		w := s2.NewWriter(&buf, s2.WriterSnappyCompat())
		if _, err := w.Write(src); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := w.Close(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(buf.Bytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("__frame_decode", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		r := s2.NewReader(bytes.NewReader(src))
		out, err := io.ReadAll(r)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	rb.LoadRuby(mrb, "<internal:oruby-snappy>", []byte(`
module Snappy
  class Writer
    def initialize(io)
      @io = io
      @buf = +"".b
      if block_given?
        begin
          yield self
        ensure
          flush
        end
      end
    end

    def <<(data)
      @buf << data.to_s.b
      self
    end
    alias write <<

    def flush
      unless @buf.empty?
        @io.write(Snappy.__frame_encode(@buf))
        @buf = +"".b
      end
      self
    end
    alias dump! flush
  end

  class Reader
    def initialize(io)
      @io = io
    end

    def read
      Snappy.__frame_decode(@io.read)
    end

    def each
      return to_enum(:each) unless block_given?
      yield read
    end
  end
end
`))
	return nil
}
