// Package msgpack implements Ruby's msgpack gem (and its msgpack/msgpack
// native extension) in pure Go.
package msgpack

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed msgpack.rb
var msgpackRb []byte

const maxNestingDepth = 512

var (
	errIncomplete = errors.New("incomplete")
	errStack      = errors.New("stack level too deep")
	errMalformed  = errors.New("malformed format")
)

type extErr struct {
	kind string
	msg  string
}

func (e extErr) Error() string { return e.msg }

func init() {
	oruby.LazyGem("msgpack/msgpack", initExt)
	oruby.LazyGem("msgpack", initAll)
	for _, sub := range []string{
		"msgpack/version",
		"msgpack/buffer",
		"msgpack/packer",
		"msgpack/unpacker",
		"msgpack/factory",
		"msgpack/core_ext",
		"msgpack/symbol",
		"msgpack/timestamp",
		"msgpack/time",
		"msgpack/bigint",
	} {
		oruby.GemAlias(sub, "msgpack")
	}
}

func initAll(mrb *oruby.MrbState) any {
	initExt(mrb)
	rb.LoadRuby(mrb, "msgpack.rb", msgpackRb)
	return nil
}

func initExt(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("MessagePack")
	stdErr := mrb.ExcGet("StandardError")

	unpackErr := mrb.DefineClassUnder(mod, "UnpackError", stdErr)
	mrb.DefineClassUnder(mod, "MalformedFormatError", unpackErr)
	mrb.DefineClassUnder(mod, "StackError", unpackErr)
	typeErrMod := mrb.DefineModuleUnder(mod, "TypeError")
	unexpectedType := mrb.DefineClassUnder(mod, "UnexpectedTypeError", unpackErr)
	mrb.IncludeModule(unexpectedType, typeErrMod)
	mrb.DefineClassUnder(mod, "UnknownExtTypeError", unpackErr)

	defineBuffer(mrb, mod)
	definePacker(mrb, mod)
	defineUnpacker(mrb, mod)
	defineFactory(mrb, mod)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func binString(mrb *oruby.MrbState, b []byte) oruby.Value {
	return mrb.Call(mrb.BytesValue(b), "b")
}

func raiseMP(mrb *oruby.MrbState, name, msg string) oruby.MrbValue {
	return rb.Raise(mrb, "MessagePack::"+name, "%s", msg)
}

func rbCall(mrb *oruby.MrbState, obj oruby.MrbValue, name string, args ...any) (oruby.Value, error) {
	return mrb.Funcall(obj, mrb.Intern(name), args...)
}

func getBuf(mrb *oruby.MrbState, self oruby.Value) *mpBuffer {
	b, _ := rb.Get[*mpBuffer](mrb, self)
	return b
}

func getPacker(mrb *oruby.MrbState, self oruby.Value) *mpPacker {
	p, _ := rb.Get[*mpPacker](mrb, self)
	return p
}

func getUnpacker(mrb *oruby.MrbState, self oruby.Value) *mpUnpacker {
	u, _ := rb.Get[*mpUnpacker](mrb, self)
	return u
}

func getFactory(mrb *oruby.MrbState, self oruby.Value) *mpFactory {
	f, _ := rb.Get[*mpFactory](mrb, self)
	return f
}

func extractIOAndOpts(mrb *oruby.MrbState) (oruby.Value, oruby.Value) {
	args := mrb.GetArgs()
	kw := mrb.KeywordArgs().Value()
	ioVal := mrb.NilValue()
	optsVal := mrb.NilValue()

	switch args.Len() {
	case 0:
	case 1:
		a0 := args.Item(0)
		if a0.IsHash() {
			optsVal = a0
		} else if !a0.IsNil() {
			ioVal = a0
		}
	default:
		a0 := args.Item(0)
		a1 := args.Item(1)
		if a0.IsHash() && a1.IsNil() {
			optsVal = a0
		} else {
			if !a0.IsNil() {
				ioVal = a0
			}
			if a1.IsHash() {
				optsVal = a1
			}
		}
	}

	if !kw.IsNil() && kw.IsHash() {
		if optsVal.IsNil() {
			optsVal = kw
		} else {
			merged, err := rbCall(mrb, optsVal, "merge", kw)
			if err == nil {
				optsVal = merged
			}
		}
	}
	return ioVal, optsVal
}

func hashGetSym(mrb *oruby.MrbState, h oruby.Value, key string) oruby.Value {
	if h.IsNil() || !h.IsHash() {
		return mrb.NilValue()
	}
	v, err := rbCall(mrb, h, "[]", mrb.Intern(key).Value())
	if err != nil {
		return mrb.NilValue()
	}
	return v
}

func optBool(mrb *oruby.MrbState, opts oruby.Value, key string) bool {
	if opts.IsNil() || !opts.IsHash() {
		return false
	}
	v := hashGetSym(mrb, opts, key)
	if v.IsNil() {
		v, _ = rbCall(mrb, opts, "[]", mrb.StrNew(key))
	}
	return v.Bool()
}

func appendRef(mrb *oruby.MrbState, owner oruby.Value, v oruby.Value) {
	if v.IsNil() {
		return
	}
	sym := mrb.Intern("@__mp_refs")
	ary := mrb.IVGet(owner, sym)
	if ary.IsNil() {
		ary = mrb.AryNew().Value()
		mrb.IVSet(owner, sym, ary)
	}
	mrb.AryPush(ary, v)
}

// ---------------------------------------------------------------------------
// MessagePack::Buffer
// ---------------------------------------------------------------------------

type mpBuffer struct {
	data []byte
	pos  int
	io   oruby.Value
}

func (b *mpBuffer) remaining() int {
	return len(b.data) - b.pos
}

// Memsize implements the optional ObjectSpace.memsize_of interface for Go-backed data objects.
func (b *mpBuffer) Memsize() int {
	if b == nil || b.remaining() == 0 {
		return 0
	}
	return cap(b.data)
}

func (b *mpBuffer) bytes() []byte {
	return b.data[b.pos:]
}

func (b *mpBuffer) appendBytes(p []byte) {
	if b.pos > 0 && b.pos == len(b.data) {
		b.data = b.data[:0]
		b.pos = 0
	}
	b.data = append(b.data, p...)
}

func (b *mpBuffer) appendByte(c byte) {
	if b.pos > 0 && b.pos == len(b.data) {
		b.data = b.data[:0]
		b.pos = 0
	}
	b.data = append(b.data, c)
}

func (b *mpBuffer) clear() {
	if cap(b.data) > 4096 {
		b.data = nil
	} else {
		b.data = b.data[:0]
	}
	b.pos = 0
}

func (b *mpBuffer) compact() {
	if b.pos == 0 {
		return
	}
	if b.pos >= len(b.data) {
		if cap(b.data) > 4096 {
			b.data = nil
		} else {
			b.data = b.data[:0]
		}
		b.pos = 0
		return
	}
	if b.pos > 4096 && b.pos*2 >= len(b.data) {
		n := copy(b.data, b.data[b.pos:])
		b.data = b.data[:n]
		b.pos = 0
	}
}

func (b *mpBuffer) fillFromIO(mrb *oruby.MrbState, need int) (bool, error) {
	if b.io.IsNil() {
		return false, nil
	}
	if need < 4096 {
		need = 4096
	}
	chunk, err := rbCall(mrb, b.io, "read", mrb.FixnumValue(need))
	if err != nil {
		return false, err
	}
	if chunk.IsNil() {
		return false, nil
	}
	bs := chunk.Bytes()
	if len(bs) == 0 {
		return false, nil
	}
	b.appendBytes(bs)
	return true, nil
}

func (b *mpBuffer) flushToIO(mrb *oruby.MrbState) error {
	if b.io.IsNil() || b.remaining() == 0 {
		return nil
	}
	s := binString(mrb, b.bytes())
	if _, err := rbCall(mrb, b.io, "write", s); err != nil {
		return err
	}
	b.clear()
	return nil
}

func defineBuffer(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Buffer", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ioVal, _ := extractIOAndOpts(mrb)
		b := &mpBuffer{io: ioVal}
		rb.Set(mrb, self, b)
		if !ioVal.IsNil() {
			mrb.IVSet(self, mrb.Intern("@__io"), ioVal)
		}
		return self
	}, mrb.ArgsAny())

	cls.DefineMethod("clear", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getBuf(mrb, self).clear()
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getBuf(mrb, self).remaining())
	}, mrb.ArgsNone())

	cls.DefineMethod("empty?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getBuf(mrb, self).remaining() == 0)
	}, mrb.ArgsNone())

	bufToS := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return binString(mrb, getBuf(mrb, self).bytes())
	}
	cls.DefineMethod("to_s", bufToS, mrb.ArgsNone())
	cls.DefineMethod("to_str", bufToS, mrb.ArgsNone())

	cls.DefineMethod("to_a", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		b := getBuf(mrb, self)
		ary := mrb.AryNew()
		if b.remaining() > 0 {
			mrb.AryPush(ary, binString(mrb, b.bytes()))
		}
		return ary
	}, mrb.ArgsNone())

	cls.DefineMethod("write", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := mrb.GetArgsFirst()
		bs := s.Bytes()
		getBuf(mrb, self).appendBytes(bs)
		return mrb.FixnumValue(len(bs))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("<<", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s := mrb.GetArgsFirst()
		getBuf(mrb, self).appendBytes(s.Bytes())
		return self
	}, mrb.ArgsReq(1))

	bufRead := func(mrb *oruby.MrbState, self oruby.Value, exact bool) oruby.MrbValue {
		b := getBuf(mrb, self)
		args := mrb.GetArgs()
		if args.Len() == 0 || args.Item(0).IsNil() {
			for !b.io.IsNil() {
				ok, err := b.fillFromIO(mrb, 4096)
				if err != nil {
					return mrb.RaiseError(err)
				}
				if !ok {
					break
				}
			}
			out := binString(mrb, b.bytes())
			b.clear()
			return out
		}
		n := args.Item(0).Int()
		if n < 0 {
			return mrb.EArgumentError().Raisef("negative length %d given", n)
		}
		for b.remaining() < n && !b.io.IsNil() {
			ok, err := b.fillFromIO(mrb, n-b.remaining())
			if err != nil {
				return mrb.RaiseError(err)
			}
			if !ok {
				break
			}
		}
		if exact && b.remaining() < n {
			return mrb.Raise(mrb.ExcGet("EOFError"), "end of buffer reached")
		}
		if b.remaining() == 0 {
			if n == 0 {
				return binString(mrb, nil)
			}
			return mrb.NilValue()
		}
		take := n
		if take > b.remaining() {
			take = b.remaining()
		}
		out := binString(mrb, b.data[b.pos:b.pos+take])
		b.pos += take
		b.compact()
		return out
	}

	cls.DefineMethod("read", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return bufRead(mrb, self, false)
	}, mrb.ArgsOpt(2))

	cls.DefineMethod("read_all", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return bufRead(mrb, self, true)
	}, mrb.ArgsOpt(2))

	bufSkip := func(mrb *oruby.MrbState, self oruby.Value, exact bool) oruby.MrbValue {
		b := getBuf(mrb, self)
		n := mrb.GetArgsFirst().Int()
		if n < 0 {
			return mrb.EArgumentError().Raisef("negative length %d given", n)
		}
		for b.remaining() < n && !b.io.IsNil() {
			ok, err := b.fillFromIO(mrb, n-b.remaining())
			if err != nil {
				return mrb.RaiseError(err)
			}
			if !ok {
				break
			}
		}
		if exact && b.remaining() < n {
			return mrb.Raise(mrb.ExcGet("EOFError"), "end of buffer reached")
		}
		take := n
		if take > b.remaining() {
			take = b.remaining()
		}
		b.pos += take
		b.compact()
		return mrb.FixnumValue(take)
	}

	cls.DefineMethod("skip", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return bufSkip(mrb, self, false)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("skip_all", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return bufSkip(mrb, self, true)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("flush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if err := getBuf(mrb, self).flushToIO(mrb); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("write_to", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		b := getBuf(mrb, self)
		io := mrb.GetArgsFirst()
		n := b.remaining()
		if n > 0 {
			if _, err := rbCall(mrb, io, "write", binString(mrb, b.bytes())); err != nil {
				return mrb.RaiseError(err)
			}
			b.clear()
		}
		return mrb.FixnumValue(n)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("io", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getBuf(mrb, self).io
	}, mrb.ArgsNone())

	cls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		b := getBuf(mrb, self)
		if !b.io.IsNil() {
			if _, err := rbCall(mrb, b.io, "close"); err != nil {
				return mrb.RaiseError(err)
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
}

// ---------------------------------------------------------------------------
// Extension Registry
// ---------------------------------------------------------------------------

type packExt struct {
	typeID           int8
	klass            oruby.Value
	proc             oruby.Value
	recursive        bool
	oversizedInteger bool
}

type unpackExt struct {
	typeID           int8
	klass            oruby.Value
	proc             oruby.Value
	recursive        bool
	optimizedSymbols bool
}

func validateExtTypeVal(mrb *oruby.MrbState, v oruby.Value) (int8, error) {
	if v.Type() == oruby.MrbTTInteger {
		t := v.Int64()
		if t < -128 || t > 127 {
			return 0, oruby.ERangeError("ext type out of range (-128..127): %d", t)
		}
		return int8(t), nil
	}
	if v.Type() == oruby.MrbTTBigInt {
		z, err := mrb.BigInt(v)
		if err == nil && z.IsInt64() {
			t := z.Int64()
			if t >= -128 && t <= 127 {
				return int8(t), nil
			}
		}
		return 0, oruby.ERangeError("ext type out of range (-128..127)")
	}
	return 0, oruby.ETypeError("no implicit conversion of %s into Integer", mrb.ClassOf(v).Name())
}

// ---------------------------------------------------------------------------
// MessagePack::Packer
// ---------------------------------------------------------------------------

type mpPacker struct {
	buf    *mpBuffer
	bufVal oruby.Value
	compat bool
	exts   []packExt
}

func newBufferVal(mrb *oruby.MrbState, ioVal oruby.Value) (*mpBuffer, oruby.Value) {
	bufCls := rb.ClassPath(mrb, "MessagePack::Buffer")
	b := &mpBuffer{io: ioVal}
	v := mrb.DataWrapInterface(bufCls, b).Value()
	if !ioVal.IsNil() {
		mrb.IVSet(v, mrb.Intern("@__io"), ioVal)
	}
	return b, v
}

func (p *mpPacker) writeNil() {
	p.buf.appendByte(0xc0)
}

func (p *mpPacker) writeBool(b bool) {
	if b {
		p.buf.appendByte(0xc3)
	} else {
		p.buf.appendByte(0xc2)
	}
}

func (p *mpPacker) writeInt64(v int64) {
	if v >= 0 {
		p.writeUint64(uint64(v))
		return
	}
	if v >= -32 {
		p.buf.appendByte(byte(v))
	} else if v >= math.MinInt8 {
		p.buf.appendBytes([]byte{0xd0, byte(v)})
	} else if v >= math.MinInt16 {
		var tmp [3]byte
		tmp[0] = 0xd1
		binary.BigEndian.PutUint16(tmp[1:], uint16(int16(v)))
		p.buf.appendBytes(tmp[:])
	} else if v >= math.MinInt32 {
		var tmp [5]byte
		tmp[0] = 0xd2
		binary.BigEndian.PutUint32(tmp[1:], uint32(int32(v)))
		p.buf.appendBytes(tmp[:])
	} else {
		var tmp [9]byte
		tmp[0] = 0xd3
		binary.BigEndian.PutUint64(tmp[1:], uint64(v))
		p.buf.appendBytes(tmp[:])
	}
}

func (p *mpPacker) writeUint64(v uint64) {
	if v <= 0x7f {
		p.buf.appendByte(byte(v))
	} else if v <= math.MaxUint8 {
		p.buf.appendBytes([]byte{0xcc, byte(v)})
	} else if v <= math.MaxUint16 {
		var tmp [3]byte
		tmp[0] = 0xcd
		binary.BigEndian.PutUint16(tmp[1:], uint16(v))
		p.buf.appendBytes(tmp[:])
	} else if v <= math.MaxUint32 {
		var tmp [5]byte
		tmp[0] = 0xce
		binary.BigEndian.PutUint32(tmp[1:], uint32(v))
		p.buf.appendBytes(tmp[:])
	} else {
		var tmp [9]byte
		tmp[0] = 0xcf
		binary.BigEndian.PutUint64(tmp[1:], v)
		p.buf.appendBytes(tmp[:])
	}
}

func (p *mpPacker) writeFloat64(f float64) {
	var tmp [9]byte
	tmp[0] = 0xcb
	binary.BigEndian.PutUint64(tmp[1:], math.Float64bits(f))
	p.buf.appendBytes(tmp[:])
}

func (p *mpPacker) writeFloat32(f float32) {
	var tmp [5]byte
	tmp[0] = 0xca
	binary.BigEndian.PutUint32(tmp[1:], math.Float32bits(f))
	p.buf.appendBytes(tmp[:])
}

func (p *mpPacker) writeStrBytes(b []byte) {
	n := len(b)
	if n <= 31 {
		p.buf.appendByte(byte(0xa0 | n))
	} else if n <= 0xff && !p.compat {
		p.buf.appendBytes([]byte{0xd9, byte(n)})
	} else if n <= 0xffff {
		var tmp [3]byte
		tmp[0] = 0xda
		binary.BigEndian.PutUint16(tmp[1:], uint16(n))
		p.buf.appendBytes(tmp[:])
	} else {
		var tmp [5]byte
		tmp[0] = 0xdb
		binary.BigEndian.PutUint32(tmp[1:], uint32(n))
		p.buf.appendBytes(tmp[:])
	}
	p.buf.appendBytes(b)
}

func (p *mpPacker) writeBinHeader(n int) {
	if n <= 0xff {
		p.buf.appendBytes([]byte{0xc4, byte(n)})
	} else if n <= 0xffff {
		var tmp [3]byte
		tmp[0] = 0xc5
		binary.BigEndian.PutUint16(tmp[1:], uint16(n))
		p.buf.appendBytes(tmp[:])
	} else {
		var tmp [5]byte
		tmp[0] = 0xc6
		binary.BigEndian.PutUint32(tmp[1:], uint32(n))
		p.buf.appendBytes(tmp[:])
	}
}

func (p *mpPacker) writeBinBytes(b []byte) {
	if p.compat {
		p.writeStrBytes(b)
		return
	}
	p.writeBinHeader(len(b))
	p.buf.appendBytes(b)
}

func (p *mpPacker) writeArrayHeader(n int) {
	if n <= 15 {
		p.buf.appendByte(byte(0x90 | n))
	} else if n <= 0xffff {
		var tmp [3]byte
		tmp[0] = 0xdc
		binary.BigEndian.PutUint16(tmp[1:], uint16(n))
		p.buf.appendBytes(tmp[:])
	} else {
		var tmp [5]byte
		tmp[0] = 0xdd
		binary.BigEndian.PutUint32(tmp[1:], uint32(n))
		p.buf.appendBytes(tmp[:])
	}
}

func (p *mpPacker) writeMapHeader(n int) {
	if n <= 15 {
		p.buf.appendByte(byte(0x80 | n))
	} else if n <= 0xffff {
		var tmp [3]byte
		tmp[0] = 0xde
		binary.BigEndian.PutUint16(tmp[1:], uint16(n))
		p.buf.appendBytes(tmp[:])
	} else {
		var tmp [5]byte
		tmp[0] = 0xdf
		binary.BigEndian.PutUint32(tmp[1:], uint32(n))
		p.buf.appendBytes(tmp[:])
	}
}

func (p *mpPacker) writeExt(typeID int8, payload []byte) {
	n := len(payload)
	switch n {
	case 1:
		p.buf.appendBytes([]byte{0xd4, byte(typeID)})
	case 2:
		p.buf.appendBytes([]byte{0xd5, byte(typeID)})
	case 4:
		p.buf.appendBytes([]byte{0xd6, byte(typeID)})
	case 8:
		p.buf.appendBytes([]byte{0xd7, byte(typeID)})
	case 16:
		p.buf.appendBytes([]byte{0xd8, byte(typeID)})
	default:
		if n <= 0xff {
			p.buf.appendBytes([]byte{0xc7, byte(n), byte(typeID)})
		} else if n <= 0xffff {
			var tmp [4]byte
			tmp[0] = 0xc8
			binary.BigEndian.PutUint16(tmp[1:], uint16(n))
			tmp[3] = byte(typeID)
			p.buf.appendBytes(tmp[:])
		} else {
			var tmp [6]byte
			tmp[0] = 0xc9
			binary.BigEndian.PutUint32(tmp[1:], uint32(n))
			tmp[5] = byte(typeID)
			p.buf.appendBytes(tmp[:])
		}
	}
	p.buf.appendBytes(payload)
}

func (p *mpPacker) findPackExt(mrb *oruby.MrbState, obj oruby.Value) *packExt {
	if len(p.exts) == 0 {
		return nil
	}
	objCls := mrb.ClassOf(obj).Value()
	for i := range p.exts {
		if mrb.ObjEqual(p.exts[i].klass, objCls) {
			return &p.exts[i]
		}
	}
	for i := range p.exts {
		ok, err := rbCall(mrb, obj, "is_a?", p.exts[i].klass)
		if err == nil && ok.Bool() {
			return &p.exts[i]
		}
	}
	return nil
}

func (p *mpPacker) writeWithExt(mrb *oruby.MrbState, ext *packExt, obj oruby.Value) error {
	if ext.recursive {
		subBuf, subBufVal := newBufferVal(mrb, mrb.NilValue())
		subP := &mpPacker{buf: subBuf, bufVal: subBufVal, compat: p.compat, exts: p.exts}
		subVal := mrb.DataWrapInterface(rb.ClassPath(mrb, "MessagePack::Packer"), subP).Value()
		mrb.IVSet(subVal, mrb.Intern("@__buf"), subBufVal)
		_, err := rbCall(mrb, ext.proc, "call", obj, subVal)
		if err != nil {
			return err
		}
		p.writeExt(ext.typeID, subBuf.bytes())
		return nil
	}
	res, err := rbCall(mrb, ext.proc, "call", obj)
	if err != nil {
		return err
	}
	if !res.IsString() {
		return fmt.Errorf("extension packer must return String")
	}
	p.writeExt(ext.typeID, res.Bytes())
	return nil
}

func (p *mpPacker) writeInteger(mrb *oruby.MrbState, v oruby.Value) error {
	if v.Type() == oruby.MrbTTInteger {
		p.writeInt64(v.Int64())
		return nil
	}
	z, err := mrb.BigInt(v)
	if err != nil {
		return err
	}
	if z.IsInt64() {
		p.writeInt64(z.Int64())
		return nil
	}
	if z.IsUint64() {
		p.writeUint64(z.Uint64())
		return nil
	}
	if ext := p.findPackExt(mrb, v); ext != nil && ext.oversizedInteger {
		return p.writeWithExt(mrb, ext, v)
	}
	return oruby.ERangeError("bignum too big to convert into 'unsigned long long'")
}

func (p *mpPacker) writeStringVal(mrb *oruby.MrbState, v oruby.Value) error {
	if mrb.RespondTo(v, mrb.Intern("encoding")) {
		if enc, err := rbCall(mrb, v, "encoding"); err == nil && !enc.IsNil() {
			if nameVal, err2 := rbCall(mrb, enc, "name"); err2 == nil && nameVal.IsString() {
				switch nameVal.String() {
				case "ASCII-8BIT", "BINARY":
					if !p.compat {
						p.writeBinBytes(v.Bytes())
					} else {
						p.writeStrBytes(v.Bytes())
					}
					return nil
				case "UTF-8", "US-ASCII":
					p.writeStrBytes(v.Bytes())
					return nil
				default:
					u8, err3 := rbCall(mrb, v, "encode", mrb.StrNew("UTF-8"))
					if err3 != nil {
						return err3
					}
					p.writeStrBytes(u8.Bytes())
					return nil
				}
			}
		}
	}
	bs := v.Bytes()
	if oruby.StrBinary(v) && !p.compat {
		p.writeBinBytes(bs)
	} else {
		p.writeStrBytes(bs)
	}
	return nil
}

func (p *mpPacker) writeSymbolVal(mrb *oruby.MrbState, v oruby.Value) error {
	sv, err := rbCall(mrb, v, "to_s")
	if err == nil && sv.IsString() {
		return p.writeStringVal(mrb, sv)
	}
	p.writeStrBytes([]byte(mrb.SymName(v.Symbol())))
	return nil
}

func (p *mpPacker) writeValue(mrb *oruby.MrbState, self oruby.Value, v oruby.Value, depth int) error {
	if depth > maxNestingDepth {
		return errStack
	}

	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			p.writeNil()
		} else {
			p.writeBool(false)
		}
		return nil
	case oruby.MrbTTTrue:
		p.writeBool(true)
		return nil
	case oruby.MrbTTInteger:
		p.writeInt64(v.Int64())
		return nil
	case oruby.MrbTTBigInt:
		return p.writeInteger(mrb, v)
	case oruby.MrbTTFloat:
		p.writeFloat64(v.Float64())
		return nil
	case oruby.MrbTTString:
		if len(p.exts) > 0 {
			if ext := p.findPackExt(mrb, v); ext != nil {
				return p.writeWithExt(mrb, ext, v)
			}
		}
		return p.writeStringVal(mrb, v)
	case oruby.MrbTTSymbol:
		if ext := p.findPackExt(mrb, v); ext != nil {
			return p.writeWithExt(mrb, ext, v)
		}
		return p.writeSymbolVal(mrb, v)
	case oruby.MrbTTArray:
		if len(p.exts) > 0 {
			if ext := p.findPackExt(mrb, v); ext != nil {
				return p.writeWithExt(mrb, ext, v)
			}
		}
		n := oruby.RArrayLen(v)
		p.writeArrayHeader(n)
		for i := 0; i < n; i++ {
			if err := p.writeValue(mrb, self, mrb.AryRef(v, i), depth+1); err != nil {
				return err
			}
		}
		return nil
	case oruby.MrbTTHash:
		if len(p.exts) > 0 {
			if ext := p.findPackExt(mrb, v); ext != nil {
				return p.writeWithExt(mrb, ext, v)
			}
		}
		keys := mrb.HashKeys(v)
		n := oruby.RArrayLen(keys)
		p.writeMapHeader(n)
		for i := 0; i < n; i++ {
			k := mrb.AryRef(keys, i)
			if err := p.writeValue(mrb, self, k, depth+1); err != nil {
				return err
			}
			val := mrb.HashGet(v, k)
			if err := p.writeValue(mrb, self, val, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	// ExtensionValue?
	extValCls := rb.ClassPath(mrb, "MessagePack::ExtensionValue")
	if !extValCls.IsNil() && mrb.ObjIsKindOf(v, extValCls) {
		typeVal, err := rbCall(mrb, v, "type")
		if err != nil {
			return err
		}
		tid, err := validateExtTypeVal(mrb, typeVal)
		if err != nil {
			return err
		}
		payload, err := rbCall(mrb, v, "payload")
		if err != nil {
			return err
		}
		p.writeExt(tid, payload.Bytes())
		return nil
	}

	// Registered custom extension type?
	if ext := p.findPackExt(mrb, v); ext != nil {
		return p.writeWithExt(mrb, ext, v)
	}

	// Fallback to #to_msgpack(self)
	if mrb.RespondTo(v, mrb.Intern("to_msgpack")) {
		_, err := rbCall(mrb, v, "to_msgpack", self)
		return err
	}

	return oruby.ENoMethodError("undefined method 'to_msgpack' for %s", mrb.ClassOf(v).Name())
}

func (p *mpPacker) registerType(mrb *oruby.MrbState, owner oruby.Value, tid int8, klass, proc oruby.Value, recursive bool) {
	appendRef(mrb, owner, klass)
	appendRef(mrb, owner, proc)
	for i := range p.exts {
		if mrb.ObjEqual(p.exts[i].klass, klass) {
			p.exts[i] = packExt{typeID: tid, klass: klass, proc: proc, recursive: recursive}
			return
		}
	}
	p.exts = append(p.exts, packExt{typeID: tid, klass: klass, proc: proc, recursive: recursive})
}

func definePacker(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Packer", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ioVal, optsVal := extractIOAndOpts(mrb)
		b, bVal := newBufferVal(mrb, ioVal)
		p := &mpPacker{
			buf:    b,
			bufVal: bVal,
			compat: optBool(mrb, optsVal, "compatibility_mode"),
		}
		rb.Set(mrb, self, p)
		mrb.IVSet(self, mrb.Intern("@__buf"), bVal)
		return self
	}, mrb.ArgsAny())

	cls.DefineMethod("buffer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getPacker(mrb, self).bufVal
	}, mrb.ArgsNone())

	cls.DefineMethod("compatibility_mode?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getPacker(mrb, self).compat)
	}, mrb.ArgsNone())

	writeMethod := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		if err := p.writeValue(mrb, self, mrb.GetArgsFirst(), 0); err != nil {
			if errors.Is(err, errStack) {
				return raiseMP(mrb, "StackError", err.Error())
			}
			return mrb.RaiseError(err)
		}
		return self
	}
	cls.DefineMethod("write", writeMethod, mrb.ArgsReq(1))
	cls.DefineMethod("pack", writeMethod, mrb.ArgsReq(1))

	cls.DefineMethod("write_nil", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getPacker(mrb, self).writeNil()
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("write_true", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getPacker(mrb, self).writeBool(true)
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("write_false", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getPacker(mrb, self).writeBool(false)
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("write_int", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		v := mrb.GetArgsFirst()
		if v.Type() != oruby.MrbTTInteger && v.Type() != oruby.MrbTTBigInt {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected Integer)", mrb.ClassOf(v).Name()))
		}
		if err := p.writeInteger(mrb, v); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_float", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		f := v.Float64()
		if v.IsInt() {
			f = float64(v.Int64())
		}
		getPacker(mrb, self).writeFloat64(f)
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_float32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		var f float64
		switch v.Type() {
		case oruby.MrbTTFloat:
			f = v.Float64()
		case oruby.MrbTTInteger:
			f = float64(v.Int64())
		case oruby.MrbTTBigInt:
			z, err := mrb.BigInt(v)
			if err != nil {
				return mrb.Raise(mrb.EArgumentError(), err.Error())
			}
			f, _ = z.Float64()
		default:
			return mrb.Raise(mrb.EArgumentError(), fmt.Sprintf("expected Numeric, got %s", mrb.ClassOf(v).Name()))
		}
		getPacker(mrb, self).writeFloat32(float32(f))
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if !v.IsString() {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected String)", mrb.ClassOf(v).Name()))
		}
		if err := getPacker(mrb, self).writeStringVal(mrb, v); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_bin", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if !v.IsString() {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected String)", mrb.ClassOf(v).Name()))
		}
		getPacker(mrb, self).writeBinBytes(v.Bytes())
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_bin_header", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getPacker(mrb, self).writeBinHeader(mrb.GetArgsFirst().Int())
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_symbol", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		v := mrb.GetArgsFirst()
		if !v.IsSymbol() {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected Symbol)", mrb.ClassOf(v).Name()))
		}
		if ext := p.findPackExt(mrb, v); ext != nil {
			if err := p.writeWithExt(mrb, ext, v); err != nil {
				return mrb.RaiseError(err)
			}
			return self
		}
		if err := p.writeSymbolVal(mrb, v); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_array_header", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getPacker(mrb, self).writeArrayHeader(mrb.GetArgsFirst().Int())
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_array", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		v := mrb.GetArgsFirst()
		if !v.IsArray() {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected Array)", mrb.ClassOf(v).Name()))
		}
		n := oruby.RArrayLen(v)
		p.writeArrayHeader(n)
		for i := 0; i < n; i++ {
			if err := p.writeValue(mrb, self, mrb.AryRef(v, i), 1); err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_map_header", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getPacker(mrb, self).writeMapHeader(mrb.GetArgsFirst().Int())
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_hash", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		v := mrb.GetArgsFirst()
		if !v.IsHash() {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected Hash)", mrb.ClassOf(v).Name()))
		}
		keys := mrb.HashKeys(v)
		n := oruby.RArrayLen(keys)
		p.writeMapHeader(n)
		for i := 0; i < n; i++ {
			k := mrb.AryRef(keys, i)
			if err := p.writeValue(mrb, self, k, 1); err != nil {
				return mrb.RaiseError(err)
			}
			if err := p.writeValue(mrb, self, mrb.HashGet(v, k), 1); err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("write_ext", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		tid, err := validateExtTypeVal(mrb, args.Item(0))
		if err != nil {
			return mrb.RaiseError(err)
		}
		getPacker(mrb, self).writeExt(tid, args.Item(1).Bytes())
		return self
	}, mrb.ArgsReq(2))

	cls.DefineMethod("write_extension", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := mrb.GetArgsFirst()
		extValCls := rb.ClassPath(mrb, "MessagePack::ExtensionValue")
		if extValCls.IsNil() || !mrb.ObjIsKindOf(ev, extValCls) {
			return mrb.Raise(mrb.ETypeError(), fmt.Sprintf("wrong argument type %s (expected MessagePack::ExtensionValue)", mrb.ClassOf(ev).Name()))
		}
		typeVal, err := rbCall(mrb, ev, "type")
		if err != nil {
			return mrb.RaiseError(err)
		}
		tid, err := validateExtTypeVal(mrb, typeVal)
		if err != nil {
			return mrb.RaiseError(err)
		}
		payload, err := rbCall(mrb, ev, "payload")
		if err != nil {
			return mrb.RaiseError(err)
		}
		getPacker(mrb, self).writeExt(tid, payload.Bytes())
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("full_pack", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		if !p.buf.io.IsNil() {
			if err := p.buf.flushToIO(mrb); err != nil {
				return mrb.RaiseError(err)
			}
			return mrb.NilValue()
		}
		out := binString(mrb, p.buf.bytes())
		p.buf.clear()
		return out
	}, mrb.ArgsNone())

	packerToS := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return binString(mrb, getPacker(mrb, self).buf.bytes())
	}
	cls.DefineMethod("to_s", packerToS, mrb.ArgsNone())
	cls.DefineMethod("to_str", packerToS, mrb.ArgsNone())

	cls.DefineMethod("to_a", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		ary := mrb.AryNew()
		if p.buf.remaining() > 0 {
			mrb.AryPush(ary, binString(mrb, p.buf.bytes()))
		}
		return ary
	}, mrb.ArgsNone())

	clearFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getPacker(mrb, self).buf.clear()
		return mrb.NilValue()
	}
	cls.DefineMethod("clear", clearFn, mrb.ArgsNone())
	cls.DefineMethod("reset", clearFn, mrb.ArgsNone())

	cls.DefineMethod("size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getPacker(mrb, self).buf.remaining())
	}, mrb.ArgsNone())

	cls.DefineMethod("empty?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getPacker(mrb, self).buf.remaining() == 0)
	}, mrb.ArgsNone())

	cls.DefineMethod("flush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if err := getPacker(mrb, self).buf.flushToIO(mrb); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("write_to", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		io := mrb.GetArgsFirst()
		n := p.buf.remaining()
		if n > 0 {
			if _, err := rbCall(mrb, io, "write", binString(mrb, p.buf.bytes())); err != nil {
				return mrb.RaiseError(err)
			}
			p.buf.clear()
		}
		return mrb.FixnumValue(n)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("register_type_internal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		args := mrb.GetArgs()
		tid, err := validateExtTypeVal(mrb, args.Item(0))
		if err != nil {
			return mrb.RaiseError(err)
		}
		p.registerType(mrb, self, tid, args.Item(1), args.Item(2), false)
		return mrb.NilValue()
	}, mrb.ArgsReq(3))

	cls.DefineMethod("registered_types_internal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		p := getPacker(mrb, self)
		h := mrb.HashNew()
		for _, e := range p.exts {
			ary := mrb.AryNew()
			mrb.AryPush(ary, mrb.FixnumValue(int(e.typeID)))
			mrb.AryPush(ary, e.proc)
			mrb.AryPush(ary, mrb.FixnumValue(0))
			mrb.HashSet(h, e.klass, ary)
		}
		return h
	}, mrb.ArgsNone())
}

// ---------------------------------------------------------------------------
// MessagePack::Unpacker
// ---------------------------------------------------------------------------

type mpUnpacker struct {
	buf             *mpBuffer
	bufVal          oruby.Value
	symbolizeKeys   bool
	freeze          bool
	allowUnknownExt bool
	extDepth        int
	exts            map[int8]unpackExt
}

func (u *mpUnpacker) ensure(mrb *oruby.MrbState, off, need int) error {
	for len(u.buf.data)-off < need {
		ok, err := u.buf.fillFromIO(mrb, need-(len(u.buf.data)-off))
		if err != nil {
			return err
		}
		if !ok {
			return errIncomplete
		}
	}
	return nil
}

func (u *mpUnpacker) decode(mrb *oruby.MrbState, off int, depth int) (oruby.Value, int, error) {
	if depth > maxNestingDepth {
		return mrb.NilValue(), off, errStack
	}
	if err := u.ensure(mrb, off, 1); err != nil {
		return mrb.NilValue(), off, err
	}
	b := u.buf.data[off]
	off++

	if b <= 0x7f {
		return mrb.FixnumValue(int(b)), off, nil
	}
	if b >= 0xe0 {
		return mrb.FixnumValue(int(int8(b))), off, nil
	}
	if b >= 0x80 && b <= 0x8f {
		return u.decodeMap(mrb, off, int(b&0x0f), depth+1)
	}
	if b >= 0x90 && b <= 0x9f {
		return u.decodeArray(mrb, off, int(b&0x0f), depth+1)
	}
	if b >= 0xa0 && b <= 0xbf {
		return u.decodeStr(mrb, off, int(b&0x1f), false)
	}

	switch b {
	case 0xc0:
		return mrb.NilValue(), off, nil
	case 0xc1:
		return mrb.NilValue(), off, errMalformed
	case 0xc2:
		return mrb.FalseValue(), off, nil
	case 0xc3:
		return mrb.TrueValue(), off, nil
	case 0xc4:
		if err := u.ensure(mrb, off, 1); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(u.buf.data[off])
		return u.decodeStr(mrb, off+1, n, true)
	case 0xc5:
		if err := u.ensure(mrb, off, 2); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint16(u.buf.data[off:]))
		return u.decodeStr(mrb, off+2, n, true)
	case 0xc6:
		if err := u.ensure(mrb, off, 4); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint32(u.buf.data[off:]))
		return u.decodeStr(mrb, off+4, n, true)
	case 0xc7:
		if err := u.ensure(mrb, off, 2); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(u.buf.data[off])
		tid := int8(u.buf.data[off+1])
		return u.decodeExt(mrb, off+2, tid, n)
	case 0xc8:
		if err := u.ensure(mrb, off, 3); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint16(u.buf.data[off:]))
		tid := int8(u.buf.data[off+2])
		return u.decodeExt(mrb, off+3, tid, n)
	case 0xc9:
		if err := u.ensure(mrb, off, 5); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint32(u.buf.data[off:]))
		tid := int8(u.buf.data[off+4])
		return u.decodeExt(mrb, off+5, tid, n)
	case 0xca:
		if err := u.ensure(mrb, off, 4); err != nil {
			return mrb.NilValue(), off, err
		}
		bits := binary.BigEndian.Uint32(u.buf.data[off:])
		return mrb.FloatValue(float64(math.Float32frombits(bits))), off + 4, nil
	case 0xcb:
		if err := u.ensure(mrb, off, 8); err != nil {
			return mrb.NilValue(), off, err
		}
		bits := binary.BigEndian.Uint64(u.buf.data[off:])
		return mrb.FloatValue(math.Float64frombits(bits)), off + 8, nil
	case 0xcc:
		if err := u.ensure(mrb, off, 1); err != nil {
			return mrb.NilValue(), off, err
		}
		return mrb.FixnumValue(int(u.buf.data[off])), off + 1, nil
	case 0xcd:
		if err := u.ensure(mrb, off, 2); err != nil {
			return mrb.NilValue(), off, err
		}
		return mrb.FixnumValue(int(binary.BigEndian.Uint16(u.buf.data[off:]))), off + 2, nil
	case 0xce:
		if err := u.ensure(mrb, off, 4); err != nil {
			return mrb.NilValue(), off, err
		}
		return mrb.Int64Value(int64(binary.BigEndian.Uint32(u.buf.data[off:]))), off + 4, nil
	case 0xcf:
		if err := u.ensure(mrb, off, 8); err != nil {
			return mrb.NilValue(), off, err
		}
		v := binary.BigEndian.Uint64(u.buf.data[off:])
		return mrb.Uint64Value(v), off + 8, nil
	case 0xd0:
		if err := u.ensure(mrb, off, 1); err != nil {
			return mrb.NilValue(), off, err
		}
		return mrb.FixnumValue(int(int8(u.buf.data[off]))), off + 1, nil
	case 0xd1:
		if err := u.ensure(mrb, off, 2); err != nil {
			return mrb.NilValue(), off, err
		}
		return mrb.FixnumValue(int(int16(binary.BigEndian.Uint16(u.buf.data[off:])))), off + 2, nil
	case 0xd2:
		if err := u.ensure(mrb, off, 4); err != nil {
			return mrb.NilValue(), off, err
		}
		return mrb.Int64Value(int64(int32(binary.BigEndian.Uint32(u.buf.data[off:])))), off + 4, nil
	case 0xd3:
		if err := u.ensure(mrb, off, 8); err != nil {
			return mrb.NilValue(), off, err
		}
		return mrb.Int64Value(int64(binary.BigEndian.Uint64(u.buf.data[off:]))), off + 8, nil
	case 0xd4:
		return u.decodeFixExt(mrb, off, 1)
	case 0xd5:
		return u.decodeFixExt(mrb, off, 2)
	case 0xd6:
		return u.decodeFixExt(mrb, off, 4)
	case 0xd7:
		return u.decodeFixExt(mrb, off, 8)
	case 0xd8:
		return u.decodeFixExt(mrb, off, 16)
	case 0xd9:
		if err := u.ensure(mrb, off, 1); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(u.buf.data[off])
		return u.decodeStr(mrb, off+1, n, false)
	case 0xda:
		if err := u.ensure(mrb, off, 2); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint16(u.buf.data[off:]))
		return u.decodeStr(mrb, off+2, n, false)
	case 0xdb:
		if err := u.ensure(mrb, off, 4); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint32(u.buf.data[off:]))
		return u.decodeStr(mrb, off+4, n, false)
	case 0xdc:
		if err := u.ensure(mrb, off, 2); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint16(u.buf.data[off:]))
		return u.decodeArray(mrb, off+2, n, depth+1)
	case 0xdd:
		if err := u.ensure(mrb, off, 4); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint32(u.buf.data[off:]))
		return u.decodeArray(mrb, off+4, n, depth+1)
	case 0xde:
		if err := u.ensure(mrb, off, 2); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint16(u.buf.data[off:]))
		return u.decodeMap(mrb, off+2, n, depth+1)
	case 0xdf:
		if err := u.ensure(mrb, off, 4); err != nil {
			return mrb.NilValue(), off, err
		}
		n := int(binary.BigEndian.Uint32(u.buf.data[off:]))
		return u.decodeMap(mrb, off+4, n, depth+1)
	}
	return mrb.NilValue(), off, errMalformed
}

func (u *mpUnpacker) decodeStr(mrb *oruby.MrbState, off, n int, binaryEnc bool) (oruby.Value, int, error) {
	if err := u.ensure(mrb, off, n); err != nil {
		return mrb.NilValue(), off, err
	}
	raw := u.buf.data[off : off+n]
	var s oruby.Value
	if binaryEnc {
		s = binString(mrb, raw)
	} else {
		s = mrb.BytesValue(raw)
	}
	if u.freeze {
		if dedup, err := rbCall(mrb, s, "-@"); err == nil && dedup.IsString() {
			s = dedup
		} else {
			s = mrb.ObjFreeze(s)
		}
	}
	return s, off + n, nil
}

const maxPreallocCapa = 1024

func cappedCapa(n int) int {
	if n < 0 {
		return 0
	}
	if n > maxPreallocCapa {
		return maxPreallocCapa
	}
	return n
}

func (u *mpUnpacker) decodeArray(mrb *oruby.MrbState, off, n, depth int) (oruby.Value, int, error) {
	ary := mrb.AryNewCapa(cappedCapa(n))
	for i := 0; i < n; i++ {
		elem, nextOff, err := u.decode(mrb, off, depth)
		if err != nil {
			return mrb.NilValue(), off, err
		}
		off = nextOff
		mrb.AryPush(ary, elem)
	}
	v := ary.Value()
	if u.freeze {
		v = mrb.ObjFreeze(v)
	}
	return v, off, nil
}

func (u *mpUnpacker) decodeMap(mrb *oruby.MrbState, off, n, depth int) (oruby.Value, int, error) {
	h := mrb.HashNewCapa(cappedCapa(n))
	for i := 0; i < n; i++ {
		k, nextOff, err := u.decode(mrb, off, depth)
		if err != nil {
			return mrb.NilValue(), off, err
		}
		off = nextOff
		if u.symbolizeKeys && k.IsString() {
			k = mrb.Intern(k.String()).Value()
		}
		val, nextOff, err := u.decode(mrb, off, depth)
		if err != nil {
			return mrb.NilValue(), off, err
		}
		off = nextOff
		mrb.HashSet(h, k, val)
	}
	v := h.Value()
	if u.freeze {
		v = mrb.ObjFreeze(v)
	}
	return v, off, nil
}

func (u *mpUnpacker) decodeFixExt(mrb *oruby.MrbState, off, n int) (oruby.Value, int, error) {
	if err := u.ensure(mrb, off, 1+n); err != nil {
		return mrb.NilValue(), off, err
	}
	tid := int8(u.buf.data[off])
	return u.decodeExt(mrb, off+1, tid, n)
}

func (u *mpUnpacker) decodeExt(mrb *oruby.MrbState, off int, tid int8, n int) (oruby.Value, int, error) {
	if err := u.ensure(mrb, off, n); err != nil {
		return mrb.NilValue(), off, err
	}
	payloadBytes := make([]byte, n)
	copy(payloadBytes, u.buf.data[off:off+n])
	nextOff := off + n

	if ext, ok := u.exts[tid]; ok {
		if ext.optimizedSymbols {
			isASCII := true
			for _, b := range payloadBytes {
				if b >= 0x80 {
					isASCII = false
					break
				}
			}
			s := mrb.BytesValue(payloadBytes)
			if isASCII {
				if encCls := mrb.ClassGet("Encoding"); !encCls.IsNil() {
					if asciiEnc := mrb.ConstGet(encCls, mrb.Intern("US_ASCII")); !asciiEnc.IsNil() {
						_, _ = rbCall(mrb, s, "force_encoding", asciiEnc)
					}
				}
			}
			sym, err := rbCall(mrb, s, "to_sym")
			return sym, nextOff, err
		}
		if ext.recursive {
			if u.extDepth >= 128 {
				return mrb.NilValue(), off, errStack
			}
			subBuf, subBufVal := newBufferVal(mrb, mrb.NilValue())
			subBuf.appendBytes(payloadBytes)
			subU := &mpUnpacker{
				buf:             subBuf,
				bufVal:          subBufVal,
				symbolizeKeys:   u.symbolizeKeys,
				freeze:          u.freeze,
				allowUnknownExt: u.allowUnknownExt,
				extDepth:        u.extDepth + 1,
				exts:            u.exts,
			}
			subVal := mrb.DataWrapInterface(rb.ClassPath(mrb, "MessagePack::Unpacker"), subU).Value()
			mrb.IVSet(subVal, mrb.Intern("@__buf"), subBufVal)
			res, err := rbCall(mrb, ext.proc, "call", subVal)
			return res, nextOff, err
		}
		res, err := rbCall(mrb, ext.proc, "call", binString(mrb, payloadBytes))
		return res, nextOff, err
	}

	if u.allowUnknownExt {
		evCls := rb.ClassPath(mrb, "MessagePack::ExtensionValue")
		ev, err := rbCall(mrb, evCls, "new", mrb.FixnumValue(int(tid)), binString(mrb, payloadBytes))
		if err != nil {
			return mrb.NilValue(), off, err
		}
		if u.freeze {
			ev = mrb.ObjFreeze(ev)
		}
		return ev, nextOff, nil
	}

	return mrb.NilValue(), off, extErr{kind: "UnknownExtTypeError", msg: fmt.Sprintf("unexpected extension type %d", tid)}
}

func (u *mpUnpacker) skipOne(mrb *oruby.MrbState, off, depth int) (int, error) {
	if depth >= maxNestingDepth {
		return off, errStack
	}
	if err := u.ensure(mrb, off, 1); err != nil {
		return off, err
	}
	b := u.buf.data[off]
	off++
	if b <= 0x7f || b >= 0xe0 {
		return off, nil
	}
	if b >= 0x80 && b <= 0x8f {
		n := int(b & 0x0f)
		for i := 0; i < n*2; i++ {
			var err error
			off, err = u.skipOne(mrb, off, depth+1)
			if err != nil {
				return off, err
			}
		}
		return off, nil
	}
	if b >= 0x90 && b <= 0x9f {
		n := int(b & 0x0f)
		for i := 0; i < n; i++ {
			var err error
			off, err = u.skipOne(mrb, off, depth+1)
			if err != nil {
				return off, err
			}
		}
		return off, nil
	}
	if b >= 0xa0 && b <= 0xbf {
		n := int(b & 0x1f)
		if err := u.ensure(mrb, off, n); err != nil {
			return off, err
		}
		return off + n, nil
	}
	switch b {
	case 0xc0, 0xc2, 0xc3:
		return off, nil
	case 0xc1:
		return off, errMalformed
	case 0xc4, 0xd9:
		if err := u.ensure(mrb, off, 1); err != nil {
			return off, err
		}
		n := int(u.buf.data[off])
		if err := u.ensure(mrb, off+1, n); err != nil {
			return off, err
		}
		return off + 1 + n, nil
	case 0xc5, 0xda:
		if err := u.ensure(mrb, off, 2); err != nil {
			return off, err
		}
		n := int(binary.BigEndian.Uint16(u.buf.data[off:]))
		if err := u.ensure(mrb, off+2, n); err != nil {
			return off, err
		}
		return off + 2 + n, nil
	case 0xc6, 0xdb:
		if err := u.ensure(mrb, off, 4); err != nil {
			return off, err
		}
		n := int(binary.BigEndian.Uint32(u.buf.data[off:]))
		if err := u.ensure(mrb, off+4, n); err != nil {
			return off, err
		}
		return off + 4 + n, nil
	case 0xc7:
		if err := u.ensure(mrb, off, 2); err != nil {
			return off, err
		}
		n := int(u.buf.data[off])
		if err := u.ensure(mrb, off+2, n); err != nil {
			return off, err
		}
		return off + 2 + n, nil
	case 0xc8:
		if err := u.ensure(mrb, off, 3); err != nil {
			return off, err
		}
		n := int(binary.BigEndian.Uint16(u.buf.data[off:]))
		if err := u.ensure(mrb, off+3, n); err != nil {
			return off, err
		}
		return off + 3 + n, nil
	case 0xc9:
		if err := u.ensure(mrb, off, 5); err != nil {
			return off, err
		}
		n := int(binary.BigEndian.Uint32(u.buf.data[off:]))
		if err := u.ensure(mrb, off+5, n); err != nil {
			return off, err
		}
		return off + 5 + n, nil
	case 0xca, 0xce, 0xd2:
		if err := u.ensure(mrb, off, 4); err != nil {
			return off, err
		}
		return off + 4, nil
	case 0xcb, 0xcf, 0xd3:
		if err := u.ensure(mrb, off, 8); err != nil {
			return off, err
		}
		return off + 8, nil
	case 0xcc, 0xd0:
		if err := u.ensure(mrb, off, 1); err != nil {
			return off, err
		}
		return off + 1, nil
	case 0xcd, 0xd1:
		if err := u.ensure(mrb, off, 2); err != nil {
			return off, err
		}
		return off + 2, nil
	case 0xd4:
		if err := u.ensure(mrb, off, 2); err != nil {
			return off, err
		}
		return off + 2, nil
	case 0xd5:
		if err := u.ensure(mrb, off, 3); err != nil {
			return off, err
		}
		return off + 3, nil
	case 0xd6:
		if err := u.ensure(mrb, off, 5); err != nil {
			return off, err
		}
		return off + 5, nil
	case 0xd7:
		if err := u.ensure(mrb, off, 9); err != nil {
			return off, err
		}
		return off + 9, nil
	case 0xd8:
		if err := u.ensure(mrb, off, 17); err != nil {
			return off, err
		}
		return off + 17, nil
	case 0xdc, 0xdd:
		var n int
		if b == 0xdc {
			if err := u.ensure(mrb, off, 2); err != nil {
				return off, err
			}
			n = int(binary.BigEndian.Uint16(u.buf.data[off:]))
			off += 2
		} else {
			if err := u.ensure(mrb, off, 4); err != nil {
				return off, err
			}
			n = int(binary.BigEndian.Uint32(u.buf.data[off:]))
			off += 4
		}
		for i := 0; i < n; i++ {
			var err error
			off, err = u.skipOne(mrb, off, depth+1)
			if err != nil {
				return off, err
			}
		}
		return off, nil
	case 0xde, 0xdf:
		var n int
		if b == 0xde {
			if err := u.ensure(mrb, off, 2); err != nil {
				return off, err
			}
			n = int(binary.BigEndian.Uint16(u.buf.data[off:]))
			off += 2
		} else {
			if err := u.ensure(mrb, off, 4); err != nil {
				return off, err
			}
			n = int(binary.BigEndian.Uint32(u.buf.data[off:]))
			off += 4
		}
		for i := 0; i < n*2; i++ {
			var err error
			off, err = u.skipOne(mrb, off, depth+1)
			if err != nil {
				return off, err
			}
		}
		return off, nil
	}
	return off, errMalformed
}

func (u *mpUnpacker) raiseDecodeErr(mrb *oruby.MrbState, err error) oruby.MrbValue {
	if errors.Is(err, errIncomplete) {
		return mrb.Raise(mrb.ExcGet("EOFError"), "end of buffer reached")
	}
	if errors.Is(err, errStack) {
		return raiseMP(mrb, "StackError", err.Error())
	}
	if errors.Is(err, errMalformed) {
		return raiseMP(mrb, "MalformedFormatError", err.Error())
	}
	var ee extErr
	if errors.As(err, &ee) {
		return raiseMP(mrb, ee.kind, ee.msg)
	}
	return mrb.RaiseError(err)
}

func (u *mpUnpacker) registerType(mrb *oruby.MrbState, owner oruby.Value, tid int8, klass, proc oruby.Value, recursive bool) {
	appendRef(mrb, owner, klass)
	appendRef(mrb, owner, proc)
	if u.exts == nil {
		u.exts = make(map[int8]unpackExt)
	}
	u.exts[tid] = unpackExt{typeID: tid, klass: klass, proc: proc, recursive: recursive}
}

func defineUnpacker(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Unpacker", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ioVal, optsVal := extractIOAndOpts(mrb)
		b, bVal := newBufferVal(mrb, ioVal)
		u := &mpUnpacker{
			buf:             b,
			bufVal:          bVal,
			symbolizeKeys:   optBool(mrb, optsVal, "symbolize_keys"),
			freeze:          optBool(mrb, optsVal, "freeze"),
			allowUnknownExt: optBool(mrb, optsVal, "allow_unknown_ext"),
			exts:            make(map[int8]unpackExt),
		}
		rb.Set(mrb, self, u)
		mrb.IVSet(self, mrb.Intern("@__buf"), bVal)
		return self
	}, mrb.ArgsAny())

	cls.DefineMethod("buffer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return getUnpacker(mrb, self).bufVal
	}, mrb.ArgsNone())

	cls.DefineMethod("symbolize_keys?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getUnpacker(mrb, self).symbolizeKeys)
	}, mrb.ArgsNone())

	cls.DefineMethod("freeze?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getUnpacker(mrb, self).freeze)
	}, mrb.ArgsNone())

	cls.DefineMethod("allow_unknown_ext?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getUnpacker(mrb, self).allowUnknownExt)
	}, mrb.ArgsNone())

	feedFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getUnpacker(mrb, self).buf.appendBytes(mrb.GetArgsFirst().Bytes())
		return self
	}
	cls.DefineMethod("feed", feedFn, mrb.ArgsReq(1))
	cls.DefineMethod("feed_reference", feedFn, mrb.ArgsReq(1))

	readFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		v, nextOff, err := u.decode(mrb, u.buf.pos, 0)
		if err != nil {
			return u.raiseDecodeErr(mrb, err)
		}
		u.buf.pos = nextOff
		u.buf.compact()
		return v
	}
	cls.DefineMethod("read", readFn, mrb.ArgsNone())
	cls.DefineMethod("unpack", readFn, mrb.ArgsNone())

	cls.DefineMethod("full_unpack", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		v, nextOff, err := u.decode(mrb, u.buf.pos, 0)
		if err != nil {
			return u.raiseDecodeErr(mrb, err)
		}
		u.buf.pos = nextOff
		if u.buf.remaining() > 0 {
			extra := u.buf.remaining()
			u.buf.clear()
			return raiseMP(mrb, "MalformedFormatError", fmt.Sprintf("%d extra bytes after the deserialized object", extra))
		}
		u.buf.clear()
		return v
	}, mrb.ArgsNone())

	cls.DefineMethod("read_array_header", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		off := u.buf.pos
		if err := u.ensure(mrb, off, 1); err != nil {
			return u.raiseDecodeErr(mrb, err)
		}
		b := u.buf.data[off]
		var n int
		if b >= 0x90 && b <= 0x9f {
			n = int(b & 0x0f)
			off++
		} else if b == 0xdc {
			if err := u.ensure(mrb, off+1, 2); err != nil {
				return u.raiseDecodeErr(mrb, err)
			}
			n = int(binary.BigEndian.Uint16(u.buf.data[off+1:]))
			off += 3
		} else if b == 0xdd {
			if err := u.ensure(mrb, off+1, 4); err != nil {
				return u.raiseDecodeErr(mrb, err)
			}
			n = int(binary.BigEndian.Uint32(u.buf.data[off+1:]))
			off += 5
		} else {
			return raiseMP(mrb, "UnexpectedTypeError", "expected array")
		}
		u.buf.pos = off
		u.buf.compact()
		return mrb.FixnumValue(n)
	}, mrb.ArgsNone())

	cls.DefineMethod("read_map_header", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		off := u.buf.pos
		if err := u.ensure(mrb, off, 1); err != nil {
			return u.raiseDecodeErr(mrb, err)
		}
		b := u.buf.data[off]
		var n int
		if b >= 0x80 && b <= 0x8f {
			n = int(b & 0x0f)
			off++
		} else if b == 0xde {
			if err := u.ensure(mrb, off+1, 2); err != nil {
				return u.raiseDecodeErr(mrb, err)
			}
			n = int(binary.BigEndian.Uint16(u.buf.data[off+1:]))
			off += 3
		} else if b == 0xdf {
			if err := u.ensure(mrb, off+1, 4); err != nil {
				return u.raiseDecodeErr(mrb, err)
			}
			n = int(binary.BigEndian.Uint32(u.buf.data[off+1:]))
			off += 5
		} else {
			return raiseMP(mrb, "UnexpectedTypeError", "expected map")
		}
		u.buf.pos = off
		u.buf.compact()
		return mrb.FixnumValue(n)
	}, mrb.ArgsNone())

	cls.DefineMethod("skip", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		nextOff, err := u.skipOne(mrb, u.buf.pos, 0)
		if err != nil {
			return u.raiseDecodeErr(mrb, err)
		}
		u.buf.pos = nextOff
		u.buf.compact()
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("skip_nil", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		if err := u.ensure(mrb, u.buf.pos, 1); err != nil {
			return u.raiseDecodeErr(mrb, err)
		}
		if u.buf.data[u.buf.pos] == 0xc0 {
			u.buf.pos++
			u.buf.compact()
			return mrb.TrueValue()
		}
		return mrb.FalseValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getUnpacker(mrb, self).buf.clear()
		return mrb.NilValue()
	}, mrb.ArgsNone())

	eachLoop := func(mrb *oruby.MrbState, self oruby.Value, blk oruby.MrbValue) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		for {
			v, nextOff, err := u.decode(mrb, u.buf.pos, 0)
			if err != nil {
				if errors.Is(err, errIncomplete) {
					return mrb.NilValue()
				}
				return u.raiseDecodeErr(mrb, err)
			}
			u.buf.pos = nextOff
			u.buf.compact()
			if _, yErr := mrb.YieldE(blk, v); yErr != nil {
				return mrb.RaiseError(yErr)
			}
		}
	}

	cls.DefineMethod("each", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		blk := mrb.GetArgsBlock()
		if blk.IsNil() {
			return mrb.Call(self, "to_enum", mrb.Intern("each").Value())
		}
		return eachLoop(mrb, self, blk)
	}, mrb.ArgsBlock())

	cls.DefineMethod("feed_each", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		getUnpacker(mrb, self).buf.appendBytes(args.Item(0).Bytes())
		blk := mrb.GetArgsBlock()
		if blk.IsNil() {
			return mrb.Call(self, "to_enum", mrb.Intern("each").Value())
		}
		return eachLoop(mrb, self, blk)
	}, mrb.ArgsReq(1)|mrb.ArgsBlock())

	cls.DefineMethod("register_type_internal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		args := mrb.GetArgs()
		tid, err := validateExtTypeVal(mrb, args.Item(0))
		if err != nil {
			return mrb.RaiseError(err)
		}
		u.registerType(mrb, self, tid, args.Item(1), args.Item(2), false)
		return mrb.NilValue()
	}, mrb.ArgsReq(3))

	cls.DefineMethod("registered_types_internal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		u := getUnpacker(mrb, self)
		h := mrb.HashNew()
		for tid, e := range u.exts {
			ary := mrb.AryNew()
			mrb.AryPush(ary, e.klass)
			mrb.AryPush(ary, e.proc)
			mrb.AryPush(ary, mrb.FixnumValue(0))
			mrb.HashSet(h, mrb.FixnumValue(int(tid)), ary)
		}
		return h
	}, mrb.ArgsNone())
}

// ---------------------------------------------------------------------------
// MessagePack::Factory
// ---------------------------------------------------------------------------

type mpFactory struct {
	packExts   []packExt
	unpackExts map[int8]unpackExt
}

func (f *mpFactory) Memsize() int {
	if f == nil {
		return 0
	}
	return (len(f.packExts) + len(f.unpackExts)) * 64
}

func (p *mpPacker) Memsize() int {
	if p == nil {
		return 0
	}
	return p.buf.Memsize() + len(p.exts)*64
}

func (u *mpUnpacker) Memsize() int {
	if u == nil {
		return 0
	}
	return u.buf.Memsize() + len(u.exts)*64
}

func defineFactory(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Factory", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		f := &mpFactory{unpackExts: make(map[int8]unpackExt)}
		rb.Set(mrb, self, f)
		return self
	}, mrb.ArgsAny())

	cls.DefineMethod("dup", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		orig := getFactory(mrb, self)
		cp := &mpFactory{
			packExts:   append([]packExt(nil), orig.packExts...),
			unpackExts: make(map[int8]unpackExt, len(orig.unpackExts)),
		}
		for k, v := range orig.unpackExts {
			cp.unpackExts[k] = v
		}
		dupVal := mrb.DataWrapInterface(cls, cp).Value()
		for _, e := range cp.packExts {
			appendRef(mrb, dupVal, e.klass)
			appendRef(mrb, dupVal, e.proc)
		}
		for _, e := range cp.unpackExts {
			appendRef(mrb, dupVal, e.klass)
			appendRef(mrb, dupVal, e.proc)
		}
		return dupVal
	}, mrb.ArgsNone())

	cls.DefineMethod("register_type_internal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		f := getFactory(mrb, self)
		args := mrb.GetArgs()
		tid, err := validateExtTypeVal(mrb, args.Item(0))
		if err != nil {
			return mrb.RaiseError(err)
		}
		klass := args.Item(1)
		opts := args.Item(2)

		var packProc, unpackProc = mrb.NilValue(), mrb.NilValue()
		recursive := false
		oversizedInt := false
		optSym := false
		if !opts.IsNil() && opts.IsHash() {
			packProc = hashGetSym(mrb, opts, "packer")
			unpackProc = hashGetSym(mrb, opts, "unpacker")
			recursive = optBool(mrb, opts, "recursive")
			oversizedInt = optBool(mrb, opts, "oversized_integer_extension")
			optSym = optBool(mrb, opts, "optimized_symbols_parsing")
		}
		if !packProc.IsNil() {
			appendRef(mrb, self, klass)
			appendRef(mrb, self, packProc)
			pe := packExt{typeID: tid, klass: klass, proc: packProc, recursive: recursive, oversizedInteger: oversizedInt}
			updated := false
			for i := range f.packExts {
				if mrb.ObjEqual(f.packExts[i].klass, klass) {
					f.packExts[i] = pe
					updated = true
					break
				}
			}
			if !updated {
				f.packExts = append(f.packExts, pe)
			}
		}
		if !unpackProc.IsNil() || optSym {
			appendRef(mrb, self, klass)
			appendRef(mrb, self, unpackProc)
			f.unpackExts[tid] = unpackExt{typeID: tid, klass: klass, proc: unpackProc, recursive: recursive, optimizedSymbols: optSym}
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(3))

	cls.DefineMethod("registered_types_internal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		f := getFactory(mrb, self)
		ph := mrb.HashNew()
		for _, e := range f.packExts {
			ary := mrb.AryNew()
			mrb.AryPush(ary, mrb.FixnumValue(int(e.typeID)))
			mrb.AryPush(ary, e.proc)
			mrb.AryPush(ary, mrb.FixnumValue(0))
			mrb.HashSet(ph, e.klass, ary)
		}
		uh := mrb.HashNew()
		for tid, e := range f.unpackExts {
			ary := mrb.AryNew()
			mrb.AryPush(ary, e.klass)
			mrb.AryPush(ary, e.proc)
			mrb.AryPush(ary, mrb.FixnumValue(0))
			mrb.HashSet(uh, mrb.FixnumValue(int(tid)), ary)
		}
		pair := mrb.AryNew()
		mrb.AryPush(pair, ph)
		mrb.AryPush(pair, uh)
		return pair
	}, mrb.ArgsNone())

	cls.DefineMethod("packer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		f := getFactory(mrb, self)
		ioVal, optsVal := extractIOAndOpts(mrb)
		b, bVal := newBufferVal(mrb, ioVal)
		p := &mpPacker{
			buf:    b,
			bufVal: bVal,
			compat: optBool(mrb, optsVal, "compatibility_mode"),
			exts:   append([]packExt(nil), f.packExts...),
		}
		pVal := mrb.DataWrapInterface(rb.ClassPath(mrb, "MessagePack::Packer"), p).Value()
		mrb.IVSet(pVal, mrb.Intern("@__buf"), bVal)
		mrb.IVSet(pVal, mrb.Intern("@__factory"), self)
		return pVal
	}, mrb.ArgsAny())

	cls.DefineMethod("unpacker", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		f := getFactory(mrb, self)
		ioVal, optsVal := extractIOAndOpts(mrb)
		b, bVal := newBufferVal(mrb, ioVal)
		exts := make(map[int8]unpackExt, len(f.unpackExts))
		for k, v := range f.unpackExts {
			exts[k] = v
		}
		u := &mpUnpacker{
			buf:             b,
			bufVal:          bVal,
			symbolizeKeys:   optBool(mrb, optsVal, "symbolize_keys"),
			freeze:          optBool(mrb, optsVal, "freeze"),
			allowUnknownExt: optBool(mrb, optsVal, "allow_unknown_ext"),
			exts:            exts,
		}
		uVal := mrb.DataWrapInterface(rb.ClassPath(mrb, "MessagePack::Unpacker"), u).Value()
		mrb.IVSet(uVal, mrb.Intern("@__buf"), bVal)
		mrb.IVSet(uVal, mrb.Intern("@__factory"), self)
		return uVal
	}, mrb.ArgsAny())
}
