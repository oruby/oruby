// Package zlib provides Ruby's Zlib API over the vendored native zlib library.
package zlib

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"github.com/oruby/oruby/gem/zlib/internal/native"
)

//go:embed zlib.rb
var zlibRb []byte

func init() {
	oruby.LazyGem("zlib", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Zlib")
		mrb.ConstSet(mod, oruby.Sym_ZLIB_VERSION, mrb.StrNew(native.Version()))
		mod.DefineClassMethod("__adler32", checksum(native.Adler32), mrb.ArgsReq(2))
		mod.DefineClassMethod("__crc32", checksum(native.CRC32), mrb.ArgsReq(2))
		mod.DefineClassMethod("__adler32_combine", combine(native.Adler32Combine), mrb.ArgsReq(3))
		mod.DefineClassMethod("__crc32_combine", combine(native.CRC32Combine), mrb.ArgsReq(3))
		// a module function, as MRI's (a private instance method too)
		mod.DefineModuleFunction("crc_table", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			return mrb.Value(native.CRCTable())
		}, mrb.ArgsNone())
		mod.DefineClassMethod("__gunzip", gunzipMember, mrb.ArgsReq(1))

		zs := rb.DataClass(mrb.DefineClassUnder(mod, "ZStream", mrb.ObjectClass()))
		zs.DefineMethod("__deflate_init", deflateInit, mrb.ArgsReq(4))
		zs.DefineMethod("__inflate_init", inflateInit, mrb.ArgsReq(1))
		zs.DefineMethod("__write", streamWrite, mrb.ArgsReq(2))
		zs.DefineMethod("__take", streamTake, mrb.ArgsNone())
		zs.DefineMethod("__take_unused", streamTakeInput, mrb.ArgsNone())
		zs.DefineMethod("__set_dictionary", streamSetDictionary, mrb.ArgsReq(1))
		zs.DefineMethod("__set_header", streamSetHeader, mrb.ArgsReq(3))
		zs.DefineMethod("__set_avail_out", streamSetAvailOut, mrb.ArgsReq(1))
		zs.DefineMethod("__state", streamState, mrb.ArgsNone())
		zs.DefineMethod("__reset", streamReset, mrb.ArgsNone())
		zs.DefineMethod("__close", streamClose, mrb.ArgsNone())
		zs.DefineMethod("__copy_deflate", streamCopyDeflate, mrb.ArgsReq(1))
		zs.DefineMethod("__params", streamParams, mrb.ArgsReq(2))
		zs.DefineMethod("__sync", streamSync, mrb.ArgsReq(1))
		zs.DefineMethod("__sync_point", streamSyncPoint, mrb.ArgsNone())
		rb.LoadRuby(mrb, "zlib.rb", zlibRb)
		return nil
	})
}

func checksum(fn func(uint32, []byte) uint32) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		data, err := rb.Bytes(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		return mrb.Uint64Value(uint64(fn(uint32(a[1].Int64()), data)))
	}
}

func combine(fn func(uint32, uint32, int64) uint32) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		length, err := mrb.AsInt64(a[2])
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.Uint64Value(uint64(fn(uint32(a[0].Int64()), uint32(a[1].Int64()), length)))
	}
}

type stream struct {
	codec    *native.Stream
	in       []byte
	out      []byte
	scratch  []byte
	availOut uint64
	finished bool
}

const chunkSize = 16384

func (s *stream) run(data []byte, flush int) int {
	s.addInput(data)
	for {
		mode := flush
		if uint64(len(s.in)) > math.MaxUint32 {
			mode = native.NoFlush
		}
		out := s.outputBuffer()
		consumed, written, status := s.codec.Step(s.in, out, mode)
		s.consume(consumed)
		s.appendOutput(out[:written])
		switch status {
		case native.StreamEnd:
			s.finished = true
			return status
		case native.OK, native.BufError:
			if written < len(out) && (mode == flush || consumed == 0) {
				return status
			}
		default:
			return status
		}
	}
}

func (s *stream) addInput(data []byte) {
	if len(s.in) == 0 {
		s.in = data
	} else {
		s.in = append(s.in, data...)
	}
}

func (s *stream) outputBuffer() []byte {
	if s.scratch == nil {
		s.scratch = make([]byte, chunkSize)
	}
	if s.availOut == 0 {
		s.availOut = chunkSize
		if len(s.out) == 0 {
			s.availOut = 1024
		}
	}
	return s.scratch[:min(s.availOut, chunkSize)]
}

func (s *stream) appendOutput(data []byte) {
	s.out = append(s.out, data...)
	s.availOut -= uint64(len(data))
}

func (s *stream) consume(n int) {
	s.in = s.in[n:]
	if len(s.in) == 0 {
		s.in = nil
	}
}

func binaryString(mrb *oruby.MrbState, b []byte) oruby.MrbValue {
	return mrb.Call(mrb.BytesValue(b), "force_encoding", mrb.StrNew("ASCII-8BIT"))
}

func zerr(mrb *oruby.MrbState, status int, message string) oruby.MrbValue {
	var class, defaultMessage string
	switch status {
	case native.StreamEnd:
		class, defaultMessage = "StreamEnd", "stream end"
	case native.NeedDict:
		class, defaultMessage = "NeedDict", "need dictionary"
	case native.DataError:
		class, defaultMessage = "DataError", "data error"
	case native.StreamError:
		class, defaultMessage = "StreamError", "stream error"
	case native.MemError:
		class, defaultMessage = "MemError", "insufficient memory"
	case native.BufError:
		class, defaultMessage = "BufError", "buffer error"
	case native.VersionError:
		class, defaultMessage = "VersionError", "incompatible version"
	default:
		class, defaultMessage = "Error", fmt.Sprintf("zlib error %d", status)
	}
	if message == "" {
		message = defaultMessage
	}
	return rb.Raise(mrb, "Zlib::"+class, "%s", message)
}

func getStream(mrb *oruby.MrbState, self oruby.Value) (*stream, oruby.MrbValue) {
	s, ok := rb.Get[*stream](mrb, self)
	if !ok || s.codec == nil {
		return nil, rb.Raise(mrb, "Zlib::Error", "stream is not ready")
	}
	return s, nil
}

func deflateInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	codec, status := native.NewDeflate(a[0].Int(), a[1].Int(), a[2].Int(), a[3].Int())
	return attachStream(mrb, self, codec, status)
}

func inflateInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	codec, status := native.NewInflate(mrb.GetArgsFirst().Int())
	return attachStream(mrb, self, codec, status)
}

func attachStream(mrb *oruby.MrbState, self oruby.Value, codec *native.Stream, status int) oruby.MrbValue {
	if status != native.OK {
		return zerr(mrb, status, "")
	}
	rb.Set(mrb, self, &stream{codec: codec})
	return mrb.NilValue()
}

func streamWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	if s.finished {
		return zerr(mrb, native.StreamError, "")
	}
	a := mrb.GetArgs().Slice()
	data, err := rb.Bytes(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	status := s.run(data, a[1].Int())
	switch status {
	case native.OK, native.StreamEnd, native.BufError:
		return mrb.NilValue()
	default:
		return zerr(mrb, status, s.codec.Message())
	}
}

func streamTake(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	out := s.out
	s.out = nil
	return binaryString(mrb, out)
}

func streamTakeInput(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	in := s.in
	s.in = nil
	return binaryString(mrb, in)
}

func streamSetDictionary(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	dict, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if status := s.codec.SetDictionary(dict); status != native.OK {
		return zerr(mrb, status, "")
	}
	return mrb.NilValue()
}

func streamSetHeader(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	a := mrb.GetArgs().Slice()
	var name, comment *string
	if !a[0].IsNil() {
		v := a[0].String()
		name = &v
	}
	if !a[1].IsNil() {
		v := a[1].String()
		comment = &v
	}
	if status := s.codec.SetHeader(name, comment, uint32(a[2].Int64()), 3); status != native.OK {
		return zerr(mrb, status, "")
	}
	return mrb.NilValue()
}

func streamSetAvailOut(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	s.availOut = uint64(uint32(mrb.GetArgsFirst().Int64()))
	return mrb.NilValue()
}

// __state -> total_in, total_out, finished, avail_in, adler, data_type, avail_out
func streamState(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	state := s.codec.State()
	return mrb.Value([]interface{}{
		mrb.Uint64Value(state.TotalIn), mrb.Uint64Value(state.TotalOut), s.finished,
		len(s.in), mrb.Uint64Value(uint64(state.Adler)), state.DataType, mrb.Uint64Value(s.availOut),
	})
}

func streamReset(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	if status := s.codec.Reset(); status != native.OK {
		return zerr(mrb, status, "")
	}
	*s = stream{codec: s.codec, scratch: s.scratch}
	return mrb.NilValue()
}

func streamClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	s.codec.Close()
	*s = stream{}
	return mrb.NilValue()
}

func streamCopyDeflate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, mrb.GetArgsFirst())
	if exc != nil {
		return exc
	}
	codec, status := s.codec.CopyDeflate()
	if status != native.OK {
		return zerr(mrb, status, "")
	}
	copy := &stream{
		codec: codec, in: bytes.Clone(s.in), out: bytes.Clone(s.out),
		availOut: s.availOut, finished: s.finished,
	}
	rb.Set(mrb, self, copy)
	return self
}

func streamParams(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	if s.finished {
		return zerr(mrb, native.StreamError, "")
	}
	a := mrb.GetArgs().Slice()
	for {
		out := s.outputBuffer()
		written, status := s.codec.Params(out, a[0].Int(), a[1].Int())
		s.appendOutput(out[:written])
		if status == native.OK {
			return mrb.NilValue()
		}
		if status != native.BufError || written != len(out) {
			return zerr(mrb, status, s.codec.Message())
		}
	}
}

func streamSync(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	s.addInput(data)
	used, status := s.codec.Sync(s.in)
	s.consume(used)
	switch status {
	case native.OK:
		return mrb.TrueValue()
	case native.DataError, native.BufError:
		return mrb.FalseValue()
	default:
		return zerr(mrb, status, s.codec.Message())
	}
}

func streamSyncPoint(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	return mrb.BoolValue(s.codec.SyncPoint())
}

// gunzipMember decodes one gzip member. zlib validates the entire header and
// trailer. Read metadata from the validated header to preserve its raw bytes.
func gunzipMember(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if len(data) < 10 || data[0] != 0x1f || data[1] != 0x8b {
		return rb.Raise(mrb, "Zlib::GzipFile::Error", "not in gzip format")
	}
	codec, status := native.NewInflate(31)
	if status != native.OK {
		return zerr(mrb, status, "")
	}
	defer codec.Close()

	s := stream{codec: codec}
	switch status = s.run(data, native.NoFlush); status {
	case native.StreamEnd:
	case native.OK, native.BufError:
		return rb.Raise(mrb, "Zlib::GzipFile::NoFooter", "footer is not found")
	default:
		switch message := codec.Message(); message {
		case "incorrect data check":
			return rb.Raise(mrb, "Zlib::GzipFile::CRCError", "invalid compressed data -- crc error")
		case "incorrect length check":
			return rb.Raise(mrb, "Zlib::GzipFile::LengthError", "invalid compressed data -- length error")
		case "unknown compression method", "unknown header flags set", "header crc mismatch":
			return rb.Raise(mrb, "Zlib::GzipFile::Error", "%s", message)
		default:
			return zerr(mrb, status, message)
		}
	}

	rest := data[10:]
	flags := data[3]
	if flags&4 != 0 {
		rest = rest[2+int(binary.LittleEndian.Uint16(rest)):]
	}
	var name, comment, unused interface{}
	if flags&8 != 0 {
		var raw []byte
		raw, rest, _ = bytes.Cut(rest, []byte{0})
		name = binaryString(mrb, raw).Value()
	}
	if flags&16 != 0 {
		raw, _, _ := bytes.Cut(rest, []byte{0})
		comment = binaryString(mrb, raw).Value()
	}
	if len(s.in) > 0 {
		unused = binaryString(mrb, s.in).Value()
	}
	return mrb.Value([]interface{}{
		binaryString(mrb, s.out).Value(), name, comment, int64(binary.LittleEndian.Uint32(data[4:8])),
		int(data[9]), unused, int(data[8]),
	})
}
