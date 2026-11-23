// Package orubylz4 implements the Ruby lz4-ruby gem backed by
// github.com/pierrec/lz4/v4 (pure Go), registered as "oruby-lz4"
// and aliased to "lz4-ruby" and "lz4".
package orubylz4

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"github.com/pierrec/lz4/v4"
)

func init() {
	oruby.LazyGem("oruby-lz4", initLZ4)
	oruby.GemAlias("lz4-ruby", "oruby-lz4")
	oruby.GemAlias("lz4", "oruby-lz4")
}

// lz4-ruby prepends a little-endian varint (unsigned LEB128) uncompressed length
// before the raw LZ4 block.
func compressWithHeader(src []byte, hc bool) ([]byte, error) {
	var hdr [binary.MaxVarintLen64]byte
	hn := binary.PutUvarint(hdr[:], uint64(len(src)))
	if len(src) == 0 {
		return hdr[:hn], nil
	}
	bound := lz4.CompressBlockBound(len(src))
	dst := make([]byte, hn+bound)
	copy(dst, hdr[:hn])
	var n int
	var err error
	if hc {
		var c lz4.CompressorHC
		c.Level = lz4.Level9
		n, err = c.CompressBlock(src, dst[hn:])
	} else {
		var c lz4.Compressor
		n, err = c.CompressBlock(src, dst[hn:])
	}
	if err != nil {
		return nil, err
	}
	if n == 0 {
		// Incompressible block: store raw with LZ4 literal-only fallback via high bound
		// or return uncompressed marker — lz4.CompressBlock returns 0 only if dst is too small,
		// so with CompressBlockBound it succeeds; if still 0, return error.
		return nil, io.ErrShortBuffer
	}
	return dst[:hn+n], nil
}

func uncompressWithHeader(src []byte) ([]byte, error) {
	origLen, hn := binary.Uvarint(src)
	if hn <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if origLen == 0 {
		return nil, nil
	}
	dst := make([]byte, origLen)
	n, err := lz4.UncompressBlock(src[hn:], dst)
	if err != nil {
		return nil, err
	}
	return dst[:n], nil
}

func initLZ4(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("LZ4")
	errCls := mrb.DefineClassUnder(mod, "Error", mrb.ClassGet("StandardError"))
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("0.3.3"))

	mod.DefineClassMethod("compress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		src, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		if len(args) >= 2 && args[1].IsInt() {
			sz := args[1].Int()
			if sz >= 0 && sz < len(src) {
				src = src[:sz]
			}
		}
		out, err := compressWithHeader(src, false)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1))

	mod.DefineClassMethod("compressHC", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		src, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		out, err := compressWithHeader(src, true)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsArg(1, 1))

	uncompressFn := func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		src, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		out, err := uncompressWithHeader(src)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}
	mod.DefineClassMethod("uncompress", uncompressFn, mrb.ArgsArg(1, 1))
	mod.DefineClassMethod("decompress", uncompressFn, mrb.ArgsArg(1, 1))

	mod.DefineClassMethod("raw_compress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		dst := make([]byte, lz4.CompressBlockBound(len(src)))
		var c lz4.Compressor
		n, err := c.CompressBlock(src, dst)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(dst[:n]), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("raw_uncompress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		src, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		outSize := args[1].Int()
		dst := make([]byte, outSize)
		n, err := lz4.UncompressBlock(src, dst)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(dst[:n]), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(2))

	// Standard LZ4 Frame format (LZ4::Frame.compress / decompress)
	frameMod := mrb.DefineModuleUnder(mod, "Frame")
	frameMod.DefineClassMethod("compress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		var buf bytes.Buffer
		w := lz4.NewWriter(&buf)
		if _, err := w.Write(src); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := w.Close(); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(buf.Bytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	frameMod.DefineClassMethod("decompress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		r := lz4.NewReader(bytes.NewReader(src))
		out, err := io.ReadAll(r)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	return nil
}
