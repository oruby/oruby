// Package orubydigestcrc implements the Ruby digest-crc gem (CRC8, CRC16,
// CRC16CCITT, CRC16Modbus, CRC32, CRC32c/Castagnoli, CRC32MPEG, CRC64,
// CRC64XZ) in pure Go, registered as "oruby-digest-crc" and aliased to
// "digest/crc*".
package orubydigestcrc

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"hash/crc64"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

func init() {
	oruby.LazyGem("oruby-digest-crc", initDigestCRC)
	for _, alias := range []string{
		"digest-crc",
		"digest/crc",
		"digest/crc8",
		"digest/crc16",
		"digest/crc16_ccitt",
		"digest/crc16_modbus",
		"digest/crc32",
		"digest/crc32c",
		"digest/crc32_mpeg",
		"digest/crc64",
		"digest/crc64_xz",
	} {
		oruby.GemAlias(alias, "oruby-digest-crc")
	}
}

var (
	crc32IEEE       = crc32.IEEETable
	crc32Castagnoli = crc32.MakeTable(crc32.Castagnoli)
	crc64ISO        = crc64.MakeTable(crc64.ISO)
	crc64ECMA       = crc64.MakeTable(crc64.ECMA)
)

type crcAlgo int

const (
	algoCRC8 crcAlgo = iota
	algoCRC16
	algoCRC16CCITT
	algoCRC16Modbus
	algoCRC32
	algoCRC32c
	algoCRC32MPEG
	algoCRC64
	algoCRC64XZ
)

type crcState struct {
	algo crcAlgo
	v64  uint64
}

func newCRCState(algo crcAlgo) *crcState {
	s := &crcState{algo: algo}
	s.reset()
	return s
}

func (s *crcState) reset() {
	switch s.algo {
	case algoCRC16CCITT, algoCRC16Modbus:
		s.v64 = 0xFFFF
	case algoCRC32MPEG:
		s.v64 = 0xFFFFFFFF
	case algoCRC64XZ:
		s.v64 = 0xFFFFFFFFFFFFFFFF
	default:
		s.v64 = 0
	}
}

func (s *crcState) update(data []byte) {
	switch s.algo {
	case algoCRC8:
		c := uint8(s.v64)
		for _, b := range data {
			c ^= b
			for i := 0; i < 8; i++ {
				if c&0x80 != 0 {
					c = (c << 1) ^ 0x07
				} else {
					c <<= 1
				}
			}
		}
		s.v64 = uint64(c)
	case algoCRC16:
		c := uint16(s.v64)
		for _, b := range data {
			c ^= uint16(b)
			for i := 0; i < 8; i++ {
				if c&1 != 0 {
					c = (c >> 1) ^ 0xA001
				} else {
					c >>= 1
				}
			}
		}
		s.v64 = uint64(c)
	case algoCRC16Modbus:
		c := uint16(s.v64)
		for _, b := range data {
			c ^= uint16(b)
			for i := 0; i < 8; i++ {
				if c&1 != 0 {
					c = (c >> 1) ^ 0xA001
				} else {
					c >>= 1
				}
			}
		}
		s.v64 = uint64(c)
	case algoCRC16CCITT:
		c := uint16(s.v64)
		for _, b := range data {
			c ^= uint16(b) << 8
			for i := 0; i < 8; i++ {
				if c&0x8000 != 0 {
					c = (c << 1) ^ 0x1021
				} else {
					c <<= 1
				}
			}
		}
		s.v64 = uint64(c)
	case algoCRC32:
		s.v64 = uint64(crc32.Update(uint32(s.v64), crc32IEEE, data))
	case algoCRC32c:
		s.v64 = uint64(crc32.Update(uint32(s.v64), crc32Castagnoli, data))
	case algoCRC32MPEG:
		c := uint32(s.v64)
		for _, b := range data {
			c ^= uint32(b) << 24
			for i := 0; i < 8; i++ {
				if c&0x80000000 != 0 {
					c = (c << 1) ^ 0x04C11DB7
				} else {
					c <<= 1
				}
			}
		}
		s.v64 = uint64(c)
	case algoCRC64:
		s.v64 = crc64.Update(s.v64, crc64ISO, data)
	case algoCRC64XZ:
		// CRC-64/XZ (ECMA-182 reflected with init=^0, xorout=^0)
		// Go's crc64.Update(crc, crc64ECMA, data) already uses init=^0 / xorout=^0 when started from 0!
		// Maintain standard crc64.Update state by inverting around Update:
		cur := ^s.v64
		cur = crc64.Update(cur, crc64ECMA, data)
		s.v64 = ^cur
	}
}

func (s *crcState) checksum() uint64 {
	if s.algo == algoCRC64XZ {
		return ^s.v64
	}
	return s.v64
}

func (s *crcState) digestLen() int {
	switch s.algo {
	case algoCRC8:
		return 1
	case algoCRC16, algoCRC16CCITT, algoCRC16Modbus:
		return 2
	case algoCRC32, algoCRC32c, algoCRC32MPEG:
		return 4
	default:
		return 8
	}
}

func (s *crcState) digestBytes() []byte {
	v := s.checksum()
	switch s.digestLen() {
	case 1:
		return []byte{byte(v)}
	case 2:
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(v))
		return b[:]
	case 4:
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(v))
		return b[:]
	default:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		return b[:]
	}
}

func (s *crcState) hexdigest() string {
	return fmt.Sprintf("%0*x", s.digestLen()*2, s.checksum())
}

func defineCRCClass(mrb *oruby.MrbState, digestMod, baseCls oruby.RClass, name string, algo crcAlgo) oruby.RClass {
	cls := rb.DataClass(mrb.DefineClassUnder(digestMod, name, baseCls))

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st := newCRCState(algo)
		args := mrb.GetArgs().Slice()
		if len(args) >= 1 && !args[0].IsNil() {
			st.v64 = uint64(args[0].Int64())
		}
		rb.Set(mrb, self, st)
		return self
	}, mrb.ArgsOpt(1))

	getSt := func(mrb *oruby.MrbState, self oruby.Value) *crcState {
		st, _ := rb.Get[*crcState](mrb, self)
		if st == nil {
			st = newCRCState(algo)
			rb.Set(mrb, self, st)
		}
		return st
	}

	updFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		getSt(mrb, self).update(data)
		return self
	}
	cls.DefineMethod("update", updFn, mrb.ArgsReq(1))
	cls.DefineMethod("<<", updFn, mrb.ArgsReq(1))

	cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getSt(mrb, self).reset()
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("checksum", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.Uint64Value(getSt(mrb, self).checksum())
	}, mrb.ArgsNone())

	cls.DefineMethod("digest", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st := getSt(mrb, self)
		args := mrb.GetArgs().Slice()
		if len(args) >= 1 && !args[0].IsNil() {
			data, err := rb.Bytes(mrb, args[0])
			if err != nil {
				return rb.TypeError(mrb, err)
			}
			tmp := newCRCState(algo)
			tmp.update(data)
			return mrb.Call(mrb.BytesValue(tmp.digestBytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		}
		return mrb.Call(mrb.BytesValue(st.digestBytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsOpt(1))

	cls.DefineMethod("hexdigest", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st := getSt(mrb, self)
		args := mrb.GetArgs().Slice()
		if len(args) >= 1 && !args[0].IsNil() {
			data, err := rb.Bytes(mrb, args[0])
			if err != nil {
				return rb.TypeError(mrb, err)
			}
			tmp := newCRCState(algo)
			tmp.update(data)
			return mrb.StrNew(tmp.hexdigest())
		}
		return mrb.StrNew(st.hexdigest())
	}, mrb.ArgsOpt(1))
	cls.DefineAlias("to_s", "hexdigest")

	cls.DefineMethod("digest_length", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getSt(mrb, self).digestLen())
	}, mrb.ArgsNone())

	cls.DefineMethod("block_length", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(1)
	}, mrb.ArgsNone())

	// Class convenience methods: checksum, digest, hexdigest
	cls.DefineClassMethod("checksum", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		st := newCRCState(algo)
		st.update(data)
		return mrb.Uint64Value(st.checksum())
	}, mrb.ArgsReq(1))

	cls.DefineClassMethod("digest", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		st := newCRCState(algo)
		st.update(data)
		return mrb.Call(mrb.BytesValue(st.digestBytes()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	cls.DefineClassMethod("hexdigest", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		st := newCRCState(algo)
		st.update(data)
		return mrb.StrNew(st.hexdigest())
	}, mrb.ArgsReq(1))

	return cls
}

func initDigestCRC(mrb *oruby.MrbState) any {
	_, _ = mrb.Resolve("digest")
	digestMod := mrb.DefineModule("Digest")
	crcBase := mrb.DefineClassUnder(digestMod, "CRC", mrb.ObjectClass())

	defineCRCClass(mrb, digestMod, crcBase, "CRC8", algoCRC8)
	defineCRCClass(mrb, digestMod, crcBase, "CRC16", algoCRC16)
	defineCRCClass(mrb, digestMod, crcBase, "CRC16CCITT", algoCRC16CCITT)
	defineCRCClass(mrb, digestMod, crcBase, "CRC16Modbus", algoCRC16Modbus)
	defineCRCClass(mrb, digestMod, crcBase, "CRC32", algoCRC32)
	defineCRCClass(mrb, digestMod, crcBase, "CRC32c", algoCRC32c)
	defineCRCClass(mrb, digestMod, crcBase, "CRC32MPEG", algoCRC32MPEG)
	defineCRCClass(mrb, digestMod, crcBase, "CRC64", algoCRC64)
	defineCRCClass(mrb, digestMod, crcBase, "CRC64XZ", algoCRC64XZ)
	return nil
}
