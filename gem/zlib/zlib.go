// Package zlib implements Ruby's zlib library over Go compress/flate,
// compress/zlib and compress/gzip. Go keeps the streams (Zlib::Deflate,
// Zlib::Inflate, gzip members); zlib.rb builds the Ruby API on top.
package zlib

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	_ "embed"
	stdbinary "encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed zlib.rb
var zlibRb []byte

func init() {
	oruby.Gem("zlib", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Zlib")
		mod.DefineClassMethod("__adler32", zlibAdler32, mrb.ArgsReq(2))
		mod.DefineClassMethod("__crc32", zlibCrc32, mrb.ArgsReq(2))
		mod.DefineClassMethod("adler32_combine", zlibAdler32Combine, mrb.ArgsReq(3))
		mod.DefineClassMethod("crc32_combine", zlibCrc32Combine, mrb.ArgsReq(3))
		mod.DefineClassMethod("crc_table", zlibCrcTable, mrb.ArgsNone())
		mod.DefineClassMethod("__gunzip", zlibGunzipMember, mrb.ArgsReq(1))

		zs := rb.DataClass(mrb.DefineClassUnder(mod, "ZStream", mrb.ObjectClass()))
		zs.DefineMethod("__deflate_init", deflateInit, mrb.ArgsReq(4))
		zs.DefineMethod("__inflate_init", inflateInit, mrb.ArgsReq(1))
		zs.DefineMethod("__write", streamWrite, mrb.ArgsReq(1))
		zs.DefineMethod("__flush", streamFlush, mrb.ArgsReq(1))
		zs.DefineMethod("__take", streamTake, mrb.ArgsNone())
		zs.DefineMethod("__set_dictionary", streamSetDict, mrb.ArgsReq(1))
		zs.DefineMethod("__set_header", streamSetHeader, mrb.ArgsReq(3))
		zs.DefineMethod("__state", streamState, mrb.ArgsNone())
		zs.DefineMethod("__reset", streamReset, mrb.ArgsNone())
		zs.DefineMethod("__take_unused", streamTakeUnused, mrb.ArgsNone())
		rb.LoadRuby(mrb, "zlib.rb", zlibRb)
		return nil
	})
}

// -- checksums ------------------------------------------------------------------

func zlibAdler32(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	data, err := rb.Bytes(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	return mrb.FixnumValue(int(adlerUpdate(uint32(a[1].Int64()), data)))
}

func adlerUpdate(adler uint32, p []byte) uint32 {
	const mod = 65521
	s1, s2 := adler&0xffff, adler>>16
	for _, b := range p {
		s1 = (s1 + uint32(b)) % mod
		s2 = (s2 + s1) % mod
	}
	return s2<<16 | s1
}

func zlibCrc32(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	data, err := rb.Bytes(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	return mrb.FixnumValue(int(crc32.Update(uint32(a[1].Int64()), crc32.IEEETable, data)))
}

func zlibAdler32Combine(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	const base = uint64(65521)
	adler1, adler2, len2 := uint64(a[0].Int64()), uint64(a[1].Int64()), uint64(a[2].Int64())
	rem := len2 % base
	sum1 := adler1 & 0xffff
	sum2 := (rem * sum1) % base
	sum1 += (adler2 & 0xffff) + base - 1
	sum2 += ((adler1 >> 16) & 0xffff) + ((adler2 >> 16) & 0xffff) + base - rem
	if sum1 >= base {
		sum1 -= base
	}
	if sum1 >= base {
		sum1 -= base
	}
	if sum2 >= base<<1 {
		sum2 -= base << 1
	}
	if sum2 >= base {
		sum2 -= base
	}
	return mrb.FixnumValue(int(sum1 | sum2<<16))
}

func gf2Times(mat []uint32, vec uint32) uint32 {
	var sum uint32
	for i := 0; vec != 0; i, vec = i+1, vec>>1 {
		if vec&1 != 0 {
			sum ^= mat[i]
		}
	}
	return sum
}

func gf2Square(square, mat []uint32) {
	for n := 0; n < 32; n++ {
		square[n] = gf2Times(mat, mat[n])
	}
}

func zlibCrc32Combine(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	crc1, crc2, len2 := uint32(a[0].Int64()), uint32(a[1].Int64()), a[2].Int64()
	if len2 <= 0 {
		return mrb.FixnumValue(int(crc1))
	}
	even, odd := make([]uint32, 32), make([]uint32, 32)
	odd[0] = 0xedb88320
	row := uint32(1)
	for n := 1; n < 32; n++ {
		odd[n] = row
		row <<= 1
	}
	gf2Square(even, odd)
	gf2Square(odd, even)
	for {
		gf2Square(even, odd)
		if len2&1 != 0 {
			crc1 = gf2Times(even, crc1)
		}
		len2 >>= 1
		if len2 == 0 {
			break
		}
		gf2Square(odd, even)
		if len2&1 != 0 {
			crc1 = gf2Times(odd, crc1)
		}
		len2 >>= 1
		if len2 == 0 {
			break
		}
	}
	return mrb.FixnumValue(int(crc1 ^ crc2))
}

func zlibCrcTable(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	t := make([]int, 256)
	for i, v := range crc32.IEEETable {
		t[i] = int(v)
	}
	return mrb.Value(t)
}

// -- streams --------------------------------------------------------------------

const (
	modeRaw = iota
	modeZlib
	modeGzip
	modeAuto
)

const (
	noFlush = 0
	finish  = 4
)

type flusher interface {
	io.WriteCloser
	Flush() error
}

type stream struct {
	deflate  bool
	mode     int
	level    int
	dict     []byte
	out      bytes.Buffer
	w        flusher
	gzHeader gzip.Header
	closed   bool // writer closed (finished)

	// inflate
	in       []byte
	finished bool
	unused   []byte
	totalIn  int
	totalOut int
	adler    uint32
	needDict bool
}

func wbitsMode(wbits int) int {
	switch {
	case wbits < 0:
		return modeRaw
	case wbits >= 32:
		return modeAuto
	case wbits >= 16:
		return modeGzip
	}
	return modeZlib
}

func zerr(mrb *oruby.MrbState, class, msg string) oruby.MrbValue {
	return rb.Raise(mrb, "Zlib::"+class, "%s", msg)
}

func getStream(mrb *oruby.MrbState, self oruby.Value) (*stream, oruby.MrbValue) {
	s, ok := rb.Get[*stream](mrb, self)
	if !ok {
		return nil, zerr(mrb, "Error", "stream is not ready")
	}
	return s, nil
}

// __deflate_init(level, wbits, memlevel, strategy)
func deflateInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	level := int(a[0].Int64())
	wbits := int(a[1].Int64())
	memlevel := int(a[2].Int64())
	strategy := int(a[3].Int64())
	if level < -1 || level > 9 || memlevel < 1 || memlevel > 9 || strategy < 0 || strategy > 4 {
		return zerr(mrb, "StreamError", "stream error")
	}
	if !(wbits >= -15 && wbits <= -9 || wbits >= 8 && wbits <= 15 || wbits >= 25 && wbits <= 31) {
		return zerr(mrb, "StreamError", "stream error")
	}
	if strategy == 2 && level != flate.NoCompression {
		level = flate.HuffmanOnly
	}
	s := &stream{deflate: true, mode: wbitsMode(wbits), level: level}
	s.reset()
	rb.Set(mrb, self, s)
	return mrb.NilValue()
}

// __inflate_init(wbits)
func inflateInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	wbits := int(mrb.GetArgsFirst().Int64())
	window := wbits
	if wbits < 0 {
		window = -wbits
	} else if wbits <= 47 {
		window = wbits % 16
	}
	if window > 15 || (window < 8 && !(window == 0 && wbits >= 0)) {
		return zerr(mrb, "StreamError", "stream error")
	}
	s := &stream{mode: wbitsMode(wbits)}
	s.reset()
	rb.Set(mrb, self, s)
	return mrb.NilValue()
}

func (s *stream) writer() (flusher, error) {
	if s.w != nil {
		return s.w, nil
	}
	var err error
	switch s.mode {
	case modeRaw:
		if s.dict != nil {
			s.w, err = flate.NewWriterDict(&s.out, s.level, s.dict)
		} else {
			s.w, err = flate.NewWriter(&s.out, s.level)
		}
	case modeGzip:
		var gw *gzip.Writer
		gw, err = gzip.NewWriterLevel(&s.out, s.level)
		if err == nil {
			gw.Header = s.gzHeader
			s.w = gw
		}
	default:
		if s.dict != nil {
			s.w, err = zlib.NewWriterLevelDict(&s.out, s.level, s.dict)
		} else {
			s.w, err = zlib.NewWriterLevel(&s.out, s.level)
		}
	}
	return s.w, err
}

func binary(mrb *oruby.MrbState, b []byte) oruby.MrbValue {
	v := mrb.BytesValue(b)
	return mrb.Call(v, "force_encoding", mrb.StrNew("ASCII-8BIT"))
}

// __write(str) -> deflate: nil; inflate: newly produced output
func streamWrite(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if s.deflate {
		if s.closed {
			return zerr(mrb, "StreamError", "stream error")
		}
		starting := s.w == nil
		w, err := s.writer()
		if err != nil {
			return zerr(mrb, "StreamError", err.Error())
		}
		if _, err := w.Write(data); err != nil {
			return zerr(mrb, "StreamError", err.Error())
		}
		s.totalIn += len(data)
		if s.mode == modeGzip {
			s.adler = crc32.Update(s.adler, crc32.IEEETable, data)
		} else if s.mode == modeZlib {
			if starting && s.dict != nil {
				s.adler = 1
			}
			s.adler = adlerUpdate(s.adler, data)
		}
		return mrb.NilValue()
	}
	return s.inflate(mrb, data)
}

func (s *stream) inflate(mrb *oruby.MrbState, data []byte) oruby.MrbValue {
	if s.finished {
		s.unused = append(s.unused, data...)
		return binary(mrb, nil)
	}
	s.in = append(s.in, data...)
	if len(s.in) == 0 {
		return binary(mrb, nil)
	}
	br := bytes.NewReader(s.in)
	var r io.Reader
	var err error
	mode := s.mode
	if mode == modeAuto {
		mode = modeZlib
		if len(s.in) >= 2 && s.in[0] == 0x1f && s.in[1] == 0x8b {
			mode = modeGzip
		}
	}
	switch mode {
	case modeRaw:
		if s.dict != nil {
			r = flate.NewReaderDict(br, s.dict)
		} else {
			r = flate.NewReader(br)
		}
	case modeGzip:
		var gr *gzip.Reader
		gr, err = gzip.NewReader(br)
		if err == nil {
			gr.Multistream(false)
			r = gr
		}
	default:
		if s.dict != nil {
			r, err = zlib.NewReaderDict(br, s.dict)
		} else {
			r, err = zlib.NewReader(br)
		}
	}
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			s.totalIn = len(s.in) - br.Len()
			return binary(mrb, nil)
		}
		if errors.Is(err, zlib.ErrDictionary) {
			s.needDict = true
			s.adler = stdbinary.BigEndian.Uint32(s.in[2:6])
			return zerr(mrb, "NeedDict", "need dictionary")
		}
		return zerr(mrb, "DataError", "incorrect header check")
	}
	out, err := io.ReadAll(r)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		if errors.Is(err, zlib.ErrChecksum) || errors.Is(err, gzip.ErrChecksum) {
			return zerr(mrb, "DataError", "incorrect data check")
		}
		return zerr(mrb, "DataError", "invalid compressed data -- format violated")
	}
	s.totalIn = len(s.in) - br.Len()
	if err == nil {
		s.finished = true
		s.unused = append([]byte(nil), s.in[s.totalIn:]...)
		s.in = nil
	}
	fresh := out[s.totalOut:]
	switch mode {
	case modeZlib:
		if s.needDict {
			s.adler = 1
		}
		s.adler = adlerUpdate(s.adler, fresh)
	case modeGzip:
		if s.totalOut == 0 {
			s.adler = 0
		}
		s.adler = crc32.Update(s.adler, crc32.IEEETable, fresh)
	}
	s.needDict = false
	s.totalOut = len(out)
	return binary(mrb, fresh)
}

// __flush(mode): deflate SYNC/FULL flush or FINISH
func streamFlush(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	mode := int(mrb.GetArgsFirst().Int64())
	if !s.deflate || s.closed || mode == noFlush {
		return mrb.NilValue()
	}
	starting := s.w == nil
	w, err := s.writer()
	if err != nil {
		return zerr(mrb, "StreamError", err.Error())
	}
	if mode == finish {
		err = w.Close()
		s.closed = true
	} else {
		err = w.Flush()
	}
	if err != nil {
		return zerr(mrb, "StreamError", err.Error())
	}
	if starting && s.mode == modeZlib && s.dict != nil {
		s.adler = 1
	}
	return mrb.NilValue()
}

// __take -> pending deflate output
func streamTake(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	b := append([]byte(nil), s.out.Bytes()...)
	s.totalOut += len(b)
	s.out.Reset()
	return binary(mrb, b)
}

func streamSetDict(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	d, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	checksum := adlerUpdate(1, d)
	if s.deflate {
		if s.w != nil || s.mode == modeGzip {
			return zerr(mrb, "StreamError", "stream error")
		}
		if s.mode == modeZlib {
			s.adler = checksum
			s.totalIn = len(d)
		}
	} else if s.mode == modeRaw {
		if len(s.in) > 0 || s.finished {
			return zerr(mrb, "StreamError", "stream error")
		}
	} else {
		if !s.needDict {
			return zerr(mrb, "StreamError", "stream error")
		}
		if checksum != s.adler {
			return zerr(mrb, "DataError", "incorrect dictionary")
		}
	}
	s.dict = bytes.Clone(d)
	return mrb.NilValue()
}

// __set_header(orig_name, comment, mtime) for gzip deflate streams
func streamSetHeader(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	a := mrb.GetArgs().Slice()
	if !a[0].IsNil() {
		s.gzHeader.Name = a[0].String()
	}
	if !a[1].IsNil() {
		s.gzHeader.Comment = a[1].String()
	}
	if !a[2].IsNil() && a[2].Int64() != 0 {
		s.gzHeader.ModTime = time.Unix(a[2].Int64(), 0)
	}
	s.gzHeader.OS = 3
	return mrb.NilValue()
}

// __state -> [total_in, total_out, finished, unused, adler]
func streamState(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	unused := oruby.Nil
	if len(s.unused) > 0 {
		unused = binary(mrb, s.unused).Value()
	}
	fin := s.finished
	if s.deflate {
		fin = s.closed
	}
	return mrb.Value([]interface{}{s.totalIn, s.totalOut + s.out.Len(), fin, unused, int(s.adler)})
}

// __take_unused -> the input after the end of the stream (nil if none),
// which the stream then no longer holds
func streamTakeUnused(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	if len(s.unused) == 0 {
		return mrb.NilValue()
	}
	u := binary(mrb, s.unused)
	s.unused = nil
	return u
}

func streamReset(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, exc := getStream(mrb, self)
	if exc != nil {
		return exc
	}
	s.reset()
	return mrb.NilValue()
}

func (s *stream) reset() {
	*s = stream{deflate: s.deflate, mode: s.mode, level: s.level, adler: 1}
	if s.mode == modeGzip || (!s.deflate && s.mode == modeRaw) {
		s.adler = 0
	}
}

// Zlib.__gunzip(str) -> [data, orig_name, comment, mtime, os, unused, level_flag]
func zlibGunzipMember(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	br := bytes.NewReader(data)
	gr, err := gzip.NewReader(br)
	if err != nil {
		return rb.Raise(mrb, "Zlib::GzipFile::Error", "not in gzip format")
	}
	gr.Multistream(false)
	out, err := io.ReadAll(gr)
	if err != nil {
		switch {
		case errors.Is(err, gzip.ErrChecksum):
			return rb.Raise(mrb, "Zlib::GzipFile::CRCError", "invalid compressed data -- crc error")
		case errors.Is(err, io.ErrUnexpectedEOF):
			return rb.Raise(mrb, "Zlib::GzipFile::NoFooter", "footer is not found")
		}
		return rb.Raise(mrb, "Zlib::DataError", "invalid compressed data -- format violated")
	}
	var unused interface{}
	if br.Len() > 0 {
		unused = binary(mrb, data[len(data)-br.Len():]).Value()
	}
	var mtime int64
	if !gr.ModTime.IsZero() {
		mtime = gr.ModTime.Unix()
	}
	var name, comment interface{}
	if gr.Name != "" {
		name = gr.Name
	}
	if gr.Comment != "" {
		comment = gr.Comment
	}
	level := 0
	if len(data) > 8 {
		level = int(data[8])
	}
	return mrb.Value([]interface{}{binary(mrb, out).Value(), name, comment, mtime, int(gr.OS), unused, level})
}
