package core_ext

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"math"
	"math/big"
	randv2 "math/rand/v2"
	"sync"

	"github.com/oruby/oruby"
)

const (
	mtN          = 624
	mtM          = 397
	mtMatrixA    = 0x9908b0df
	mtUpperMask  = 0x80000000
	mtLowerMask  = 0x7fffffff
	mtDefaultVal = 19650218
)

type mt19937 struct {
	state [mtN]uint32
	left  int
	next  int
}

func (mt *mt19937) initGenrand(s uint32) {
	mt.state[0] = s
	for j := 1; j < mtN; j++ {
		mt.state[j] = 1812433253*(mt.state[j-1]^(mt.state[j-1]>>30)) + uint32(j)
	}
	mt.left = 1
	mt.next = 0
}

func (mt *mt19937) initByArray(key []uint32) {
	if len(key) == 0 {
		mt.initGenrand(0)
		return
	}
	if len(key) == 1 {
		mt.initGenrand(key[0])
		return
	}
	mt.initGenrand(mtDefaultVal)
	i, j := 1, 0
	k := mtN
	if len(key) > k {
		k = len(key)
	}
	for ; k > 0; k-- {
		mt.state[i] = (mt.state[i] ^ ((mt.state[i-1] ^ (mt.state[i-1] >> 30)) * 1664525)) + key[j] + uint32(j)
		i++
		j++
		if i >= mtN {
			mt.state[0] = mt.state[mtN-1]
			i = 1
		}
		if j >= len(key) {
			j = 0
		}
	}
	for k = mtN - 1; k > 0; k-- {
		mt.state[i] = (mt.state[i] ^ ((mt.state[i-1] ^ (mt.state[i-1] >> 30)) * 1566083941)) - uint32(i)
		i++
		if i >= mtN {
			mt.state[0] = mt.state[mtN-1]
			i = 1
		}
	}
	mt.state[0] = 0x80000000
	mt.left = 1
	mt.next = 0
}

func mtTwist(u, v uint32) uint32 {
	y := (u & mtUpperMask) | (v & mtLowerMask)
	if v&1 != 0 {
		return (y >> 1) ^ mtMatrixA
	}
	return y >> 1
}

func (mt *mt19937) nextState() {
	for j := 0; j < mtN-mtM; j++ {
		mt.state[j] = mt.state[j+mtM] ^ mtTwist(mt.state[j], mt.state[j+1])
	}
	for j := mtN - mtM; j < mtN-1; j++ {
		mt.state[j] = mt.state[j+mtM-mtN] ^ mtTwist(mt.state[j], mt.state[j+1])
	}
	mt.state[mtN-1] = mt.state[mtM-1] ^ mtTwist(mt.state[mtN-1], mt.state[0])
	mt.left = mtN
	mt.next = 0
}

func (mt *mt19937) int32() uint32 {
	mt.left--
	if mt.left <= 0 {
		mt.nextState()
	}
	y := mt.state[mt.next]
	mt.next++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

func (mt *mt19937) real() float64 {
	a := mt.int32() >> 5
	b := mt.int32() >> 6
	return (float64(a)*67108864.0 + float64(b)) * (1.0 / 9007199254740992.0)
}

// limited is MRI's limited_rand: uniform in 0..limit, drawing 32-bit words
// from the most significant one and starting over as soon as the value
// passes limit (so a seeded sequence is MRI's)
func (mt *mt19937) limited(limit uint64) uint64 {
	if limit == 0 {
		return 0
	}
	mask := limit
	for _, sh := range []uint{1, 2, 4, 8, 16, 32} {
		mask |= mask >> sh
	}
retry:
	val := uint64(0)
	for i := 1; i >= 0; i-- {
		if (mask>>(uint(i)*32))&0xffffffff != 0 {
			val |= uint64(mt.int32()) << (uint(i) * 32)
			val &= mask
			if limit < val {
				goto retry
			}
		}
	}
	return val
}

func (mt *mt19937) bytes(n int) []byte {
	out := make([]byte, n)
	i := 0
	for i < n {
		r := mt.int32()
		for b := 0; b < 4 && i < n; b++ {
			out[i] = byte(r)
			r >>= 8
			i++
		}
	}
	return out
}

func (mt *mt19937) stateBytes() []byte {
	out := make([]byte, mtN*4)
	for i := 0; i < mtN; i++ {
		binary.LittleEndian.PutUint32(out[i*4:], mt.state[i])
	}
	return out
}

func initRandom(mrb *oruby.MrbState) {
	rnd := mrb.ClassGet("Random")
	mtCls := mrb.DefineGoClassUnder(rnd, "MTState", func() *mt19937 { return &mt19937{} })

	mrb.DefineClassMethod(rnd, "__urandom", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n := mrb.GetArgsFirst().Int()
		if n < 0 {
			return mrb.Raise(mrb.EArgumentError(), "negative string size (or size too big)")
		}
		buf := make([]byte, n)
		if n > 0 {
			if _, err := io.ReadFull(rand.Reader, buf); err != nil {
				return mrb.RaiseError(err)
			}
		}
		return mrb.StrNew(string(buf))
	}, oruby.ArgsReq(1))

	mrb.DefineClassMethod(rnd, "__boot_seed", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		var buf [16]byte
		binary.LittleEndian.PutUint64(buf[0:8], randv2.Uint64())
		binary.LittleEndian.PutUint64(buf[8:16], randv2.Uint64())
		return mrb.StrNew(string(buf[:]))
	}, oruby.ArgsNone())

	mrb.DefineMethod(mtCls, "seed_words", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		arg := mrb.GetArgsFirst()
		n := oruby.RArrayLen(arg)
		words := make([]uint32, n)
		for i := 0; i < n; i++ {
			words[i] = uint32(mrb.AryEntry(arg, i).Int64())
		}
		mt.initByArray(words)
		return self
	}, oruby.ArgsReq(1))

	mrb.DefineMethod(mtCls, "int32", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		return oruby.Int64(int64(mt.int32()))
	}, oruby.ArgsNone())

	// MTState#limited(n): 0..n, n < 2**63
	mrb.DefineMethod(mtCls, "limited", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		return oruby.Int64(int64(mt.limited(uint64(mrb.GetArgsFirst().Int64()))))
	}, oruby.ArgsReq(1))

	// Random.__shuffle(ary, rng, bang): Array#shuffle(!) for a Random
	// generator, which MRI drives directly (try_get_rnd): rng nil or the
	// Random class is Random::DEFAULT, an instance its own state. nil for
	// any other rng, whose #rand 06_array.rb calls.
	rndClass := rnd
	mrb.DefineClassMethod(rnd, "__shuffle", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		a, rng, bang := args.Item(0), args.Item(1), args.Item(2).Bool()
		var mt *mt19937
		switch {
		case rng.IsNil() || rng == rndClass.Value():
			mt = defaultMT(mrb)
		case mrb.ObjIsKindOf(rng, rndClass):
			mt, _ = mrb.Data(mrb.IVGet(rng, oruby.IvSym___mt)).(*mt19937)
		}
		if mt == nil {
			return mrb.NilValue()
		}
		var ra oruby.RArray
		var err error
		if bang {
			ra, err = mrb.EnsureArrayType(a)
		} else {
			ra, err = mrb.AryDup(a) // a plain Array, as MRI's shuffle returns
		}
		if err != nil {
			return mrb.RaiseError(err)
		}
		mrb.AryModify(ra)
		shuffleMT(mt, oruby.RArraySlice(ra.Value()))
		return ra
	}, oruby.ArgsReq(3))

	// Kernel#rand for the common arguments (none, nil, a Fixnum) on
	// Random::DEFAULT's state; the others go to 40_random.rb's
	mrb.DefineModuleFunction(mrb.KernelModule(), "rand", kernelRand, oruby.ArgsAny())

	mrb.DefineMethod(mtCls, "real", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		return mrb.FloatValue(mt.real())
	}, oruby.ArgsNone())

	mrb.DefineMethod(mtCls, "bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		n := mrb.GetArgsFirst().Int()
		if n < 0 {
			return mrb.Raise(mrb.EArgumentError(), "negative string size (or size too big)")
		}
		return mrb.StrNew(string(mt.bytes(n)))
	}, oruby.ArgsReq(1))

	mrb.DefineMethod(mtCls, "state_bytes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		return mrb.StrNew(string(mt.stateBytes()))
	}, oruby.ArgsNone())

	mrb.DefineMethod(mtCls, "left", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		return oruby.Int(mt.left)
	}, oruby.ArgsNone())

	mrb.DefineMethod(mtCls, "copy_from", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		dst := mrb.Data(self).(*mt19937)
		src := mrb.Data(mrb.GetArgsFirst()).(*mt19937)
		*dst = *src
		return self
	}, oruby.ArgsReq(1))

	mrb.DefineMethod(mtCls, "restore", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		mt := mrb.Data(self).(*mt19937)
		a := mrb.GetArgs().Slice()
		z, err := mrb.BigInt(a[0])
		if err != nil {
			if a[0].IsInt() {
				z = big.NewInt(a[0].Int64())
			} else {
				return mrb.Raise(mrb.ETypeError(), "invalid Random state")
			}
		}
		var buf [mtN * 4]byte
		raw := z.Bytes()
		for i := 0; i < len(raw) && i < len(buf); i++ {
			buf[i] = raw[len(raw)-1-i]
		}
		for i := 0; i < mtN; i++ {
			mt.state[i] = binary.LittleEndian.Uint32(buf[i*4:])
		}
		mt.left = a[1].Int()
		if mt.left >= 0 && mt.left <= mtN {
			mt.next = mtN - mt.left
		} else {
			mt.next = 0
		}
		return self
	}, oruby.ArgsReq(2))
}

// defaultMT is the state of Random.default (nil before 40_random.rb ran)
// defaultMTs: each state's default generator (Random's @__default's
// @__mt, which srand reseeds in place), looked up once
var defaultMTs sync.Map

func defaultMT(mrb *oruby.MrbState) *mt19937 {
	if v, ok := defaultMTs.Load(mrb); ok {
		return v.(*mt19937)
	}
	def := mrb.IVGet(mrb.ClassGetID(oruby.Sym_Random).Value(), oruby.IvSym___default)
	if def.IsNil() {
		return nil
	}
	mt, _ := mrb.Data(mrb.IVGet(def, oruby.IvSym___mt)).(*mt19937)
	if mt != nil {
		defaultMTs.Store(mrb, mt)
	}
	return mt
}

// shuffleMT is MRI's rb_ary_shuffle_bang: from the end, j = limited(i-1),
// swap. Swapping adds no new references, so no write barrier is needed.
func shuffleMT(mt *mt19937, s []oruby.Value) {
	for i := len(s); i > 1; {
		j := mt.limited(uint64(i - 1))
		i--
		s[i], s[j] = s[j], s[i]
	}
}

func kernelRand(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if args.Len() <= 1 {
		if mt := defaultMT(mrb); mt != nil {
			if args.Len() == 0 || args.Item(0).IsNil() {
				return mrb.FloatValue(mt.real())
			}
			if v := args.Item(0); v.Type() == oruby.MrbTTFixnum {
				n := v.Int64()
				if n < 0 && n != math.MinInt64 {
					n = -n
				}
				switch {
				case n == 0:
					return mrb.FloatValue(mt.real())
				case n > 0:
					return oruby.Int64(int64(mt.limited(uint64(n - 1))))
				}
			}
		}
	}
	call := make([]interface{}, 0, args.Len())
	for _, a := range args.Slice() {
		call = append(call, a)
	}
	v, err := mrb.Funcall(mrb.KernelModule().Value(), oruby.Sym___oruby_rand_slow, call...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return v
}
