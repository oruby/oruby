// Package orubyxxhash implements the Ruby xxhash gem (XXH32 & XXH64 +
// streaming hashers) in zero-dependency pure Go, registered as "oruby-xxhash"
// and aliased to "xxhash".
package orubyxxhash

import (
	"encoding/binary"
	"math/bits"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	oruby.LazyGem("oruby-xxhash", initXXhash)
	oruby.GemAlias("xxhash", "oruby-xxhash")
}

// ---------------------------------------------------------------------------
// Pure-Go XXH32 (RFC-compliant reference implementation)
// ---------------------------------------------------------------------------

const (
	prime32_1 uint32 = 0x9E3779B1
	prime32_2 uint32 = 0x85EBCA77
	prime32_3 uint32 = 0xC2B2AE3D
	prime32_4 uint32 = 0x27D4EB2F
	prime32_5 uint32 = 0x165667B1
)

type xxh32State struct {
	seed  uint32
	total uint64
	v1    uint32
	v2    uint32
	v3    uint32
	v4    uint32
	mem   [16]byte
	memSz int
}

func newXXH32(seed uint32) *xxh32State {
	s := &xxh32State{seed: seed}
	s.reset()
	return s
}

func (s *xxh32State) reset() {
	s.total = 0
	s.memSz = 0
	s.v1 = s.seed + prime32_1 + prime32_2
	s.v2 = s.seed + prime32_2
	s.v3 = s.seed
	s.v4 = s.seed - prime32_1
}

func xxh32Round(acc, input uint32) uint32 {
	acc += input * prime32_2
	acc = bits.RotateLeft32(acc, 13)
	acc *= prime32_1
	return acc
}

func (s *xxh32State) update(b []byte) {
	s.total += uint64(len(b))
	if s.memSz+len(b) < 16 {
		copy(s.mem[s.memSz:], b)
		s.memSz += len(b)
		return
	}
	if s.memSz > 0 {
		fill := 16 - s.memSz
		copy(s.mem[s.memSz:], b[:fill])
		s.v1 = xxh32Round(s.v1, binary.LittleEndian.Uint32(s.mem[0:4]))
		s.v2 = xxh32Round(s.v2, binary.LittleEndian.Uint32(s.mem[4:8]))
		s.v3 = xxh32Round(s.v3, binary.LittleEndian.Uint32(s.mem[8:12]))
		s.v4 = xxh32Round(s.v4, binary.LittleEndian.Uint32(s.mem[12:16]))
		b = b[fill:]
		s.memSz = 0
	}
	for len(b) >= 16 {
		s.v1 = xxh32Round(s.v1, binary.LittleEndian.Uint32(b[0:4]))
		s.v2 = xxh32Round(s.v2, binary.LittleEndian.Uint32(b[4:8]))
		s.v3 = xxh32Round(s.v3, binary.LittleEndian.Uint32(b[8:12]))
		s.v4 = xxh32Round(s.v4, binary.LittleEndian.Uint32(b[12:16]))
		b = b[16:]
	}
	if len(b) > 0 {
		copy(s.mem[:], b)
		s.memSz = len(b)
	}
}

func (s *xxh32State) digest() uint32 {
	var h uint32
	if s.total >= 16 {
		h = bits.RotateLeft32(s.v1, 1) + bits.RotateLeft32(s.v2, 7) + bits.RotateLeft32(s.v3, 12) + bits.RotateLeft32(s.v4, 18)
	} else {
		h = s.seed + prime32_5
	}
	h += uint32(s.total)
	b := s.mem[:s.memSz]
	for len(b) >= 4 {
		h += binary.LittleEndian.Uint32(b[:4]) * prime32_3
		h = bits.RotateLeft32(h, 17) * prime32_4
		b = b[4:]
	}
	for _, c := range b {
		h += uint32(c) * prime32_5
		h = bits.RotateLeft32(h, 11) * prime32_1
	}
	h ^= h >> 15
	h *= prime32_2
	h ^= h >> 13
	h *= prime32_3
	h ^= h >> 16
	return h
}

// ---------------------------------------------------------------------------
// Pure-Go XXH64 (RFC-compliant reference implementation)
// ---------------------------------------------------------------------------

const (
	prime64_1 uint64 = 0x9E3779B185EBCA87
	prime64_2 uint64 = 0xC2B2AE3D27D4EB4F
	prime64_3 uint64 = 0x165667B19E3779F9
	prime64_4 uint64 = 0x85EBCA77C2B2AE63
	prime64_5 uint64 = 0x27D4EB2F165667C5
)

type xxh64State struct {
	seed  uint64
	total uint64
	v1    uint64
	v2    uint64
	v3    uint64
	v4    uint64
	mem   [32]byte
	memSz int
}

func newXXH64(seed uint64) *xxh64State {
	s := &xxh64State{seed: seed}
	s.reset()
	return s
}

func (s *xxh64State) reset() {
	s.total = 0
	s.memSz = 0
	s.v1 = s.seed + prime64_1 + prime64_2
	s.v2 = s.seed + prime64_2
	s.v3 = s.seed
	s.v4 = s.seed - prime64_1
}

func xxh64Round(acc, input uint64) uint64 {
	acc += input * prime64_2
	acc = bits.RotateLeft64(acc, 31)
	acc *= prime64_1
	return acc
}

func xxh64MergeRound(acc, val uint64) uint64 {
	val = xxh64Round(0, val)
	acc ^= val
	acc = acc*prime64_1 + prime64_4
	return acc
}

func (s *xxh64State) update(b []byte) {
	s.total += uint64(len(b))
	if s.memSz+len(b) < 32 {
		copy(s.mem[s.memSz:], b)
		s.memSz += len(b)
		return
	}
	if s.memSz > 0 {
		fill := 32 - s.memSz
		copy(s.mem[s.memSz:], b[:fill])
		s.v1 = xxh64Round(s.v1, binary.LittleEndian.Uint64(s.mem[0:8]))
		s.v2 = xxh64Round(s.v2, binary.LittleEndian.Uint64(s.mem[8:16]))
		s.v3 = xxh64Round(s.v3, binary.LittleEndian.Uint64(s.mem[16:24]))
		s.v4 = xxh64Round(s.v4, binary.LittleEndian.Uint64(s.mem[24:32]))
		b = b[fill:]
		s.memSz = 0
	}
	for len(b) >= 32 {
		s.v1 = xxh64Round(s.v1, binary.LittleEndian.Uint64(b[0:8]))
		s.v2 = xxh64Round(s.v2, binary.LittleEndian.Uint64(b[8:16]))
		s.v3 = xxh64Round(s.v3, binary.LittleEndian.Uint64(b[16:24]))
		s.v4 = xxh64Round(s.v4, binary.LittleEndian.Uint64(b[24:32]))
		b = b[32:]
	}
	if len(b) > 0 {
		copy(s.mem[:], b)
		s.memSz = len(b)
	}
}

func (s *xxh64State) digest() uint64 {
	var h uint64
	if s.total >= 32 {
		h = bits.RotateLeft64(s.v1, 1) + bits.RotateLeft64(s.v2, 7) + bits.RotateLeft64(s.v3, 12) + bits.RotateLeft64(s.v4, 18)
		h = xxh64MergeRound(h, s.v1)
		h = xxh64MergeRound(h, s.v2)
		h = xxh64MergeRound(h, s.v3)
		h = xxh64MergeRound(h, s.v4)
	} else {
		h = s.seed + prime64_5
	}
	h += s.total
	b := s.mem[:s.memSz]
	for len(b) >= 8 {
		k1 := xxh64Round(0, binary.LittleEndian.Uint64(b[:8]))
		h ^= k1
		h = bits.RotateLeft64(h, 27)*prime64_1 + prime64_4
		b = b[8:]
	}
	for len(b) >= 4 {
		h ^= uint64(binary.LittleEndian.Uint32(b[:4])) * prime64_1
		h = bits.RotateLeft64(h, 23)*prime64_2 + prime64_3
		b = b[4:]
	}
	for _, c := range b {
		h ^= uint64(c) * prime64_5
		h = bits.RotateLeft64(h, 11) * prime64_1
	}
	h ^= h >> 33
	h *= prime64_2
	h ^= h >> 29
	h *= prime64_3
	h ^= h >> 32
	return h
}

func initXXhash(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("XXhash")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("0.6.0"))

	mod.DefineClassMethod("xxh32", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		data, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		var seed uint32
		if len(args) >= 2 && !args[1].IsNil() {
			seed = uint32(args[1].Int64())
		}
		s := newXXH32(seed)
		s.update(data)
		return mrb.Uint64Value(uint64(s.digest()))
	}, mrb.ArgsArg(1, 1))

	mod.DefineClassMethod("xxh64", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		data, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		var seed uint64
		if len(args) >= 2 && !args[1].IsNil() {
			seed = uint64(args[1].Int64())
		}
		s := newXXH64(seed)
		s.update(data)
		return mrb.Uint64Value(s.digest())
	}, mrb.ArgsArg(1, 1))

	s32Cls := rb.DataClass(mrb.DefineClassUnder(mod, "StreamingHash32", mrb.ObjectClass()))
	s32Cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		var seed uint32
		args := mrb.GetArgs().Slice()
		if len(args) >= 1 && !args[0].IsNil() {
			seed = uint32(args[0].Int64())
		}
		rb.Set(mrb, self, newXXH32(seed))
		return self
	}, mrb.ArgsOpt(1))
	s32Cls.DefineMethod("update", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*xxh32State](mrb, self)
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		s.update(data)
		return self
	}, mrb.ArgsReq(1))
	s32Cls.DefineAlias("<<", "update")
	s32Cls.DefineMethod("digest", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*xxh32State](mrb, self)
		return mrb.Uint64Value(uint64(s.digest()))
	}, mrb.ArgsNone())
	s32Cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*xxh32State](mrb, self)
		s.reset()
		return self
	}, mrb.ArgsNone())

	s64Cls := rb.DataClass(mrb.DefineClassUnder(mod, "StreamingHash64", mrb.ObjectClass()))
	s64Cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		var seed uint64
		args := mrb.GetArgs().Slice()
		if len(args) >= 1 && !args[0].IsNil() {
			seed = uint64(args[0].Int64())
		}
		rb.Set(mrb, self, newXXH64(seed))
		return self
	}, mrb.ArgsOpt(1))
	s64Cls.DefineMethod("update", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*xxh64State](mrb, self)
		data, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		s.update(data)
		return self
	}, mrb.ArgsReq(1))
	s64Cls.DefineAlias("<<", "update")
	s64Cls.DefineMethod("digest", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*xxh64State](mrb, self)
		return mrb.Uint64Value(s.digest())
	}, mrb.ArgsNone())
	s64Cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, _ := rb.Get[*xxh64State](mrb, self)
		s.reset()
		return self
	}, mrb.ArgsNone())

	rb.LoadRuby(mrb, "<internal:oruby-xxhash>", []byte(`
module XXhash
  def self.xxh32_stream(io, seed = 0, chunk_size = 32768)
    h = StreamingHash32.new(seed)
    while (chunk = io.read(chunk_size))
      h.update(chunk)
    end
    h.digest
  end

  def self.xxh64_stream(io, seed = 0, chunk_size = 32768)
    h = StreamingHash64.new(seed)
    while (chunk = io.read(chunk_size))
      h.update(chunk)
    end
    h.digest
  end
end
`))
	return nil
}
