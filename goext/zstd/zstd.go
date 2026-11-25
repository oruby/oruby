// Package zstd is the zstd-ruby gem (2.0.10) for oruby: zstd-ruby's Ruby
// library unchanged (lib/) over a Go zstdruby.so (this file and zstdruby.rb),
// with github.com/klauspost/compress/zstd (RFC 8878) as the codec.
package zstd

import (
	"bytes"
	"embed"
	"encoding/binary"
	"io"
	"runtime"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

// zstdruby.rb is the Ruby half of zstdruby.so; lib/ is zstd-ruby 2.0.10's
// Ruby library, unchanged (SpringMT/zstd-ruby v2.0.10)
//
//go:embed zstdruby.rb
var zstdrubyRb []byte

//go:embed lib
var libFS embed.FS

// libzstd's version number the API is that of (Zstd.zstd_version)
const zstdVersion = 10507

const (
	frameMagic     = 0xFD2FB528
	skippableMagic = 0x184D2A50 // ... 0x184D2A5F
	dictMagic      = 0xEC30A437
	skippableMax   = 129 * 1024 // read_skippable_frame's buffer, as zstd-ruby's
)

func init() {
	oruby.LazyGem("zstd-ruby/zstdruby", initZstd)
	for _, f := range []string{"zstd-ruby", "zstd-ruby/version", "zstd-ruby/stream_writer", "zstd-ruby/stream_reader"} {
		src, err := libFS.ReadFile("lib/" + f + ".rb")
		if err != nil {
			panic(err)
		}
		name := f
		oruby.LazyGem(name, func(mrb *oruby.MrbState) interface{} {
			c := mrb.MrbCcontextNew()
			defer c.Free()
			c.Filename("<internal:" + name + ".rb>")
			if _, err := mrb.LoadBytesCxt(src, c); err != nil {
				return &oruby.InitError{Err: err}
			}
			return nil
		})
	}
	oruby.GemAlias("zstd", "zstd-ruby")
}

// encoders/decoders without a dictionary are shared: EncodeAll and
// DecodeAll may run concurrently on one
var (
	encoders sync.Map // level -> *zstd.Encoder
	decOnce  sync.Once
	decPlain *zstd.Decoder
	decErr   error
)

func isZstdDict(dict []byte) bool {
	return len(dict) >= 8 && binary.LittleEndian.Uint32(dict) == dictMagic
}

func encoderOptions(level int, dict []byte) []zstd.EOption {
	opts := []zstd.EOption{
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)),
		zstd.WithEncoderCRC(false), // libzstd's default: no checksum
		zstd.WithZeroFrames(true),  // "" is a frame of its own
		zstd.WithEncoderConcurrency(1),
	}
	switch {
	case len(dict) == 0:
	case isZstdDict(dict):
		opts = append(opts, zstd.WithEncoderDict(dict))
	default: // any other content is a raw dictionary, as ZSTD_CCtx_loadDictionary
		opts = append(opts, zstd.WithEncoderDictRaw(0, dict))
	}
	return opts
}

func decoderOptions(dict []byte) []zstd.DOption {
	opts := []zstd.DOption{zstd.WithDecoderConcurrency(1)}
	switch {
	case len(dict) == 0:
	case isZstdDict(dict):
		opts = append(opts, zstd.WithDecoderDicts(dict))
	default:
		opts = append(opts, zstd.WithDecoderDictRaw(0, dict))
	}
	return opts
}

func encoder(level int, dict []byte) (*zstd.Encoder, func(), error) {
	if len(dict) == 0 {
		if e, ok := encoders.Load(level); ok {
			return e.(*zstd.Encoder), func() {}, nil
		}
	}
	e, err := zstd.NewWriter(nil, encoderOptions(level, dict)...)
	if err != nil {
		return nil, nil, err
	}
	if len(dict) == 0 {
		if prev, loaded := encoders.LoadOrStore(level, e); loaded {
			e.Close()
			e = prev.(*zstd.Encoder)
		}
		return e, func() {}, nil
	}
	return e, func() { e.Close() }, nil
}

func decoder(dict []byte) (*zstd.Decoder, func(), error) {
	if len(dict) == 0 {
		decOnce.Do(func() { decPlain, decErr = zstd.NewReader(nil, decoderOptions(nil)...) })
		return decPlain, func() {}, decErr
	}
	d, err := zstd.NewReader(nil, decoderOptions(dict)...)
	if err != nil {
		return nil, nil, err
	}
	return d, d.Close, nil
}

// Compress encodes src into a zstd frame at level in pure Go, reusing the
// shared per-level encoder cache.
func Compress(src []byte, level int) ([]byte, error) {
	enc, done, err := encoder(level, nil)
	if err != nil {
		return nil, err
	}
	defer done()
	return enc.EncodeAll(src, nil), nil
}

// Decompress decodes all concatenated zstd frames in src (skipping skippable
// frames and inter-frame padding, matching Zstd.decompress) in pure Go.
func Decompress(src []byte) ([]byte, error) {
	dec, done, err := decoder(nil)
	if err != nil {
		return nil, err
	}
	defer done()
	var out []byte
	found := false
	for off := 0; off+4 <= len(src); {
		if binary.LittleEndian.Uint32(src[off:])&0xFFFFFFF0 == skippableMagic {
			n, st := frameLen(src[off:])
			if st != frameOK {
				break
			}
			off += n
			continue
		}
		if binary.LittleEndian.Uint32(src[off:]) != frameMagic {
			off++
			continue
		}
		n, st := frameLen(src[off:])
		if st == frameShort {
			return nil, io.ErrUnexpectedEOF
		}
		if st == frameBad {
			return nil, zstd.ErrMagicMismatch
		}
		if out, err = dec.DecodeAll(src[off:off+n], out); err != nil {
			return nil, err
		}
		found = true
		off += n
	}
	if !found {
		return nil, zstd.ErrMagicMismatch
	}
	if out == nil {
		out = []byte{}
	}
	return out, nil
}

// frame status of frameLen
const (
	frameOK = iota
	frameShort
	frameBad
)

// frameLen gives the length of the zstd frame or skippable frame at the start
// of b (frameShort: b ends inside it; frameBad: no frame starts there)
func frameLen(b []byte) (int, int) {
	if len(b) < 4 {
		return 0, frameShort
	}
	magic := binary.LittleEndian.Uint32(b)
	if magic&0xFFFFFFF0 == skippableMagic {
		if len(b) < 8 {
			return 0, frameShort
		}
		n := 8 + int(binary.LittleEndian.Uint32(b[4:]))
		if n > len(b) {
			return 0, frameShort
		}
		return n, frameOK
	}
	if magic != frameMagic {
		return 0, frameBad
	}
	if len(b) < 5 {
		return 0, frameShort
	}
	fhd := b[4]
	if fhd&0x08 != 0 { // reserved bit
		return 0, frameBad
	}
	single := fhd&0x20 != 0
	n := 5
	if !single {
		n++ // window descriptor
	}
	n += [4]int{0, 1, 2, 4}[fhd&3]
	switch fhd >> 6 {
	case 0:
		if single {
			n++
		}
	case 1:
		n += 2
	case 2:
		n += 4
	case 3:
		n += 8
	}
	for {
		if n+3 > len(b) {
			return 0, frameShort
		}
		bh := uint32(b[n]) | uint32(b[n+1])<<8 | uint32(b[n+2])<<16
		n += 3
		size := int(bh >> 3)
		switch (bh >> 1) & 3 {
		case 1: // RLE: one byte repeated size times
			size = 1
		case 3:
			return 0, frameBad
		}
		n += size
		if n > len(b) {
			return 0, frameShort
		}
		if bh&1 != 0 {
			break
		}
	}
	if fhd&0x04 != 0 { // content checksum
		n += 4
		if n > len(b) {
			return 0, frameShort
		}
	}
	return n, frameOK
}

func binStr(mrb *oruby.MrbState, b []byte) oruby.MrbValue {
	return mrb.Call(mrb.BytesValue(b), "force_encoding", mrb.StrNew("ASCII-8BIT"))
}

func runtimeError(mrb *oruby.MrbState, msg string) oruby.MrbValue {
	return mrb.Raise(mrb.ExcGet("RuntimeError"), msg)
}

func dictArg(v oruby.Value) []byte {
	if v.IsString() {
		return v.Bytes()
	}
	return nil
}

type streamEnc struct {
	buf   bytes.Buffer
	enc   *zstd.Encoder
	ended bool // finish ended the frame: the next write starts another
}

// streamDec is what a Zstd::StreamingDecompress holds: klauspost's stream
// decoder runs on a goroutine of its own, reading the input fed so far from
// a feeder (blocking when it needs more), so each block's output is there as
// soon as the block is in, as libzstd's ZSTD_decompressStream. The goroutine
// references only the feeder: when the Ruby object goes, streamDec's
// finalizer closes the feeder and the goroutine ends.
type streamDec struct {
	f     *feeder
	scan  blockScan
	ahead int // raw-block bytes given out before the decoder's output of them
}

// blockScan follows the frames and blocks of a stream's input as it comes,
// for the bytes of a raw block not all in yet: they are the block's output,
// given out at once (as libzstd does) and dropped from the decoder's output
// of the whole block later
type blockScan struct {
	st       int
	hdr      []byte
	rem      int
	raw      bool
	last     bool
	checksum bool
}

const (
	scanFrame = iota
	scanBlockHdr
	scanBody
	scanSkip // skippable frame content or content checksum
	scanOff  // input blockScan does not follow (the decoder reports it)
)

// scan follows src; raw is what src has of the raw block it ends in (if any)
func (s *blockScan) scan(src []byte) (raw []byte) {
	for len(src) > 0 && s.st != scanOff {
		switch s.st {
		case scanFrame:
			s.hdr = append(s.hdr, src[0])
			src = src[1:]
			if len(s.hdr) < 4 {
				continue
			}
			magic := binary.LittleEndian.Uint32(s.hdr)
			switch {
			case magic&0xFFFFFFF0 == skippableMagic:
				if len(s.hdr) == 8 {
					s.st, s.rem, s.hdr = scanSkip, int(binary.LittleEndian.Uint32(s.hdr[4:])), s.hdr[:0]
				}
			case magic != frameMagic:
				s.st = scanOff
			case len(s.hdr) >= 5:
				fhd := s.hdr[4]
				n := 5 + [4]int{0, 1, 2, 4}[fhd&3] + [4]int{0, 2, 4, 8}[fhd>>6]
				if fhd&0x20 == 0 {
					n++ // window descriptor
				} else if fhd>>6 == 0 {
					n++ // single segment: 1-byte content size
				}
				if len(s.hdr) == n {
					s.st, s.checksum, s.hdr = scanBlockHdr, fhd&0x04 != 0, s.hdr[:0]
				}
			}
		case scanBlockHdr:
			s.hdr = append(s.hdr, src[0])
			src = src[1:]
			if len(s.hdr) < 3 {
				continue
			}
			bh := uint32(s.hdr[0]) | uint32(s.hdr[1])<<8 | uint32(s.hdr[2])<<16
			s.hdr = s.hdr[:0]
			raw = raw[:0] // only the block in progress counts
			s.last, s.raw, s.rem = bh&1 != 0, (bh>>1)&3 == 0, int(bh>>3)
			switch (bh >> 1) & 3 {
			case 1:
				s.rem = 1
			case 3:
				s.st = scanOff
				continue
			}
			s.st = scanBody
			if s.rem == 0 {
				s.endBlock()
			}
		case scanBody:
			k := min(s.rem, len(src))
			if s.raw {
				raw = append(raw, src[:k]...)
			}
			src, s.rem = src[k:], s.rem-k
			if s.rem == 0 {
				s.endBlock()
			}
		case scanSkip:
			k := min(s.rem, len(src))
			src, s.rem = src[k:], s.rem-k
			if s.rem == 0 {
				s.st = scanFrame
			}
		}
	}
	return raw
}

func (s *blockScan) endBlock() {
	switch {
	case !s.last:
		s.st = scanBlockHdr
	case s.checksum:
		s.st, s.rem = scanSkip, 4
	default:
		s.st = scanFrame
	}
}

// decompress feeds src to the decoder and returns the output so far
func (sd *streamDec) decompress(src []byte) ([]byte, error) {
	out, err := sd.f.feed(src)
	if err != nil {
		return nil, err
	}
	// what the decoder gave out of the blocks src completed, less what was
	// given out ahead; then the raw bytes of a block still coming
	drop := min(sd.ahead, len(out))
	out, sd.ahead = out[drop:], sd.ahead-drop
	// (a raw block src finished the decoder gave out whole)
	if raw := sd.scan.scan(src); sd.scan.st == scanBody && sd.scan.raw {
		out = append(out, raw...)
		sd.ahead += len(raw)
	}
	return out, nil
}

type feeder struct {
	mu      sync.Mutex
	cond    *sync.Cond
	in      []byte
	out     []byte
	starved bool // the decoder waits for input
	closed  bool
	done    bool // the decoder ended (err)
	err     error
}

func (f *feeder) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for len(f.in) == 0 {
		if f.closed {
			return 0, io.EOF
		}
		f.starved = true
		f.cond.Broadcast()
		f.cond.Wait()
	}
	f.starved = false
	n := copy(p, f.in)
	f.in = f.in[n:]
	return n, nil
}

func newStreamDec(dict []byte) *streamDec {
	f := &feeder{}
	f.cond = sync.NewCond(&f.mu)
	go func() {
		dec, err := zstd.NewReader(f, decoderOptions(dict)...)
		if err == nil {
			defer dec.Close()
			buf := make([]byte, 128<<10)
			for err == nil {
				var n int
				n, err = dec.Read(buf)
				f.mu.Lock()
				f.out = append(f.out, buf[:n]...)
				f.mu.Unlock()
			}
		}
		f.mu.Lock()
		f.done, f.err = true, err
		f.cond.Broadcast()
		f.mu.Unlock()
	}()
	sd := &streamDec{f: f}
	runtime.SetFinalizer(sd, func(sd *streamDec) { sd.f.close() })
	return sd
}

func (f *feeder) close() {
	f.mu.Lock()
	f.closed = true
	f.cond.Broadcast()
	f.mu.Unlock()
}

// feed gives the decoder src and returns what it decodes from all the input
// so far, once it waits for more (or ends)
func (f *feeder) feed(src []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(src) > 0 {
		f.in = append(f.in, src...)
		f.starved = false
		f.cond.Broadcast()
	}
	for !f.done && !(f.starved && len(f.in) == 0) {
		f.cond.Wait()
	}
	out := f.out
	f.out = nil
	if f.done && f.err != nil && f.err != io.EOF {
		return out, f.err
	}
	return out, nil
}

func initZstd(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Zstd")
	mod.DefineClassMethod("zstd_version", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(zstdVersion)
	}, mrb.ArgsNone())

	// Zstd.__compress(src, level, dict)
	mod.DefineClassMethod("__compress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		enc, done, err := encoder(int(args.Item(1).Int64()), dictArg(args.Item(2)))
		if err != nil {
			return runtimeError(mrb, "ZSTD_CCtx_loadDictionary failed")
		}
		defer done()
		return binStr(mrb, enc.EncodeAll(args.Item(0).Bytes(), nil))
	}, mrb.ArgsReq(3))

	// Zstd.__decompress(src, dict): each zstd frame of src, concatenated;
	// skippable frames and bytes outside a frame are passed over
	mod.DefineClassMethod("__decompress", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).Bytes()
		dec, done, err := decoder(dictArg(args.Item(1)))
		if err != nil {
			return runtimeError(mrb, "ZSTD_DCtx_loadDictionary failed")
		}
		defer done()
		var out []byte
		found := false
		for off := 0; off+4 <= len(src); {
			if binary.LittleEndian.Uint32(src[off:])&0xFFFFFFF0 == skippableMagic {
				n, st := frameLen(src[off:])
				if st != frameOK {
					break
				}
				off += n
				continue
			}
			if binary.LittleEndian.Uint32(src[off:]) != frameMagic {
				off++
				continue
			}
			n, st := frameLen(src[off:])
			if st == frameShort {
				return runtimeError(mrb, "ZSTD_decompressStream failed: truncated or incomplete frame")
			}
			if st == frameBad {
				return runtimeError(mrb, "ZSTD_decompressStream failed: Unknown frame descriptor")
			}
			if out, err = dec.DecodeAll(src[off:off+n], out); err != nil {
				return runtimeError(mrb, "ZSTD_decompressStream failed: "+err.Error())
			}
			found = true
			off += n
		}
		if !found {
			return runtimeError(mrb, "not a zstd frame (magic not found)")
		}
		if out == nil {
			out = []byte{}
		}
		return binStr(mrb, out)
	}, mrb.ArgsReq(2))

	mod.DefineClassMethod("__write_skippable", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		skip, variant := args.Item(0).Bytes(), args.Item(1).Int64()
		if variant < 0 || variant > 15 {
			return runtimeError(mrb, "write skippable frame failed: Parameter is out of bound")
		}
		out := make([]byte, 8, 8+len(skip))
		binary.LittleEndian.PutUint32(out, skippableMagic+uint32(variant))
		binary.LittleEndian.PutUint32(out[4:], uint32(len(skip)))
		return binStr(mrb, append(out, skip...))
	}, mrb.ArgsReq(2))

	mod.DefineClassMethod("__read_skippable", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src := mrb.GetArgsFirst().Bytes()
		if len(src) < 4 || binary.LittleEndian.Uint32(src)&0xFFFFFFF0 != skippableMagic {
			return mrb.NilValue()
		}
		if len(src) < 8 {
			return runtimeError(mrb, "read skippable frame failed: Src size is incorrect")
		}
		n := int(binary.LittleEndian.Uint32(src[4:]))
		if n+8 > len(src) {
			return runtimeError(mrb, "read skippable frame failed: Src size is incorrect")
		}
		if n > skippableMax {
			return runtimeError(mrb, "read skippable frame failed: Destination buffer is too small")
		}
		return binStr(mrb, append([]byte{}, src[8:8+n]...))
	}, mrb.ArgsReq(1))

	// Zstd::StreamingCompress: __init(level, dict), __write(src) -> output so
	// far, __flush, __finish
	scCls := rb.DataClass(mrb.DefineClassUnder(mod, "StreamingCompress", mrb.ObjectClass()))
	scCls.DefineMethod("__init", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		se := &streamEnc{}
		enc, err := zstd.NewWriter(&se.buf, encoderOptions(int(args.Item(0).Int64()), dictArg(args.Item(1)))...)
		if err != nil {
			return runtimeError(mrb, "ZSTD_CCtx_loadDictionary failed")
		}
		se.enc = enc
		rb.Set(mrb, self, se)
		return self
	}, mrb.ArgsReq(2))

	getEnc := func(mrb *oruby.MrbState, self oruby.Value) (*streamEnc, oruby.MrbValue, bool) {
		se, ok := rb.Get[*streamEnc](mrb, self)
		if !ok || se.enc == nil {
			return nil, runtimeError(mrb, "Zstd::StreamingCompress is not initialized"), false
		}
		if se.ended {
			se.enc.Reset(&se.buf)
			se.ended = false
		}
		return se, mrb.NilValue(), true
	}
	drain := func(mrb *oruby.MrbState, se *streamEnc) oruby.MrbValue {
		out := append([]byte{}, se.buf.Bytes()...)
		se.buf.Reset()
		return binStr(mrb, out)
	}

	scCls.DefineMethod("__write", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		se, exc, ok := getEnc(mrb, self)
		if !ok {
			return exc
		}
		if _, err := se.enc.Write(mrb.GetArgsFirst().Bytes()); err != nil {
			return runtimeError(mrb, "compress error error code: "+err.Error())
		}
		return drain(mrb, se)
	}, mrb.ArgsReq(1))

	scCls.DefineMethod("__flush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		se, exc, ok := getEnc(mrb, self)
		if !ok {
			return exc
		}
		if err := se.enc.Flush(); err != nil {
			return runtimeError(mrb, "flush error error code: "+err.Error())
		}
		return drain(mrb, se)
	}, mrb.ArgsNone())

	scCls.DefineMethod("__finish", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		se, exc, ok := getEnc(mrb, self)
		if !ok {
			return exc
		}
		if err := se.enc.Close(); err != nil {
			return runtimeError(mrb, "finish error error code: "+err.Error())
		}
		se.ended = true
		return drain(mrb, se)
	}, mrb.ArgsNone())

	// Zstd::StreamingDecompress: __init(dict), __feed(src) -> the output of
	// the blocks src completes
	sdCls := rb.DataClass(mrb.DefineClassUnder(mod, "StreamingDecompress", mrb.ObjectClass()))
	sdCls.DefineMethod("__init", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, newStreamDec(dictArg(mrb.GetArgsFirst())))
		return self
	}, mrb.ArgsReq(1))

	sdCls.DefineMethod("__feed", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sd, ok := rb.Get[*streamDec](mrb, self)
		if !ok {
			return runtimeError(mrb, "Zstd::StreamingDecompress is not initialized")
		}
		out, err := sd.decompress(mrb.GetArgsFirst().Bytes())
		if err != nil {
			return runtimeError(mrb, "decompress error error code: "+err.Error())
		}
		if out == nil {
			out = []byte{}
		}
		return binStr(mrb, out)
	}, mrb.ArgsReq(1))

	rb.LoadRuby(mrb, "<internal:zstd-ruby/zstdruby.so>", zstdrubyRb)
	mrb.AotRegisterModuleIntrinsic(oruby.AotIntrinsicZstdCompress, mod, mrb.Intern("compress"))
	mrb.AotRegisterModuleIntrinsic(oruby.AotIntrinsicZstdDecompress, mod, mrb.Intern("decompress"))
	return nil
}
