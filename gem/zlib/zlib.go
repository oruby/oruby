// Package zlib implements gem thath does not use C zlib, but golang internal implementation
package zlib

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"hash/adler32"
	"hash/crc32"

	"github.com/oruby/oruby"
)

func init() {
	oruby.Gem("zlib", func(mrb *oruby.MrbState) interface{} {
		zlibModule := mrb.DefineModule("Zlib")

		zlibModule.Const("ASCII", 1)
		zlibModule.Const("BINARY", 0)
		zlibModule.Const("TEXT", 1)
		zlibModule.Const("UNKNOWN", 1)
		zlibModule.Const("NO_COMPRESSION", 0)
		zlibModule.Const("BEST_SPEED", 1)
		zlibModule.Const("BEST_COMPRESSION", 9)
		zlibModule.Const("DEFAULT_COMPRESSION", -1)
		zlibModule.Const("FILTERED", 1)
		zlibModule.Const("HUFFMAN_ONLY", 2)
		zlibModule.Const("RLE", 3)
		zlibModule.Const("FIXED", 4)
		zlibModule.Const("DEFAULT_STRATEGY", 0)
		zlibModule.Const("NO_FLUSH", 0)
		zlibModule.Const("PARTIAL_FLUSH", 1)
		zlibModule.Const("SYNC_FLUSH", 2)
		zlibModule.Const("FULL_FLUSH", 3)
		zlibModule.Const("FINISH", 4)
		zlibModule.Const("BLOCK", 5)
		zlibModule.Const("TREES", 6)

		zlibModule.DefineClassMethod("adler32", zlibAdler32, mrb.ArgsArg(1, 1))
		zlibModule.DefineClassMethod("adler32_combine", zlibAdler32Combine, mrb.ArgsReq(3))
		zlibModule.DefineClassMethod("crc32", zlibCrc32, mrb.ArgsArg(1, 1))
		zlibModule.DefineClassMethod("crc32_combine", zlibCrc32Combine, mrb.ArgsReq(3))
		zlibModule.DefineClassMethod("crc_table", zlibCrc32Table, mrb.ArgsReq(1))
		zlibModule.DefineClassMethod("deflate", zlibDeflate, mrb.ArgsArg(1, 1))
		zlibModule.DefineClassMethod("gunzip", zlibGUnZip, mrb.ArgsArg(1, 1))
		zlibModule.DefineClassMethod("gzip", zlibGZip, mrb.ArgsArg(1, 2))
		zlibModule.DefineClassMethod("inflate", zlibInflate, mrb.ArgsArg(1, 1))
		zlibModule.DefineClassMethod("zlib_version", zlibVersion, mrb.ArgsArg(1, 1))

		zerr := mrb.DefineClassUnder(zlibModule, "ZError", mrb.EStandardErrorClass())
		mrb.DefineClassUnder(zlibModule, "ZStreamEnd", zerr)
		mrb.DefineClassUnder(zlibModule, "ZNeedDict", zerr)
		mrb.DefineClassUnder(zlibModule, "DataError", zerr)
		mrb.DefineClassUnder(zlibModule, "StreamError", zerr)
		mrb.DefineClassUnder(zlibModule, "MemError", zerr)
		mrb.DefineClassUnder(zlibModule, "BufError", zerr)
		mrb.DefineClassUnder(zlibModule, "VersionError", zerr)

		zstream := mrb.DefineClassUnder(zlibModule, "ZStream", mrb.ObjectClass())
		mrb.DefineClassUnder(zlibModule, "ZInflate", zstream)
		mrb.DefineClassUnder(zlibModule, "ZDefalte", zstream)

		gzfile := mrb.DefineClassUnder(zlibModule, "GzipFile", mrb.ObjectClass())
		mrb.DefineClassUnder(zlibModule, "GzipReader", gzfile)
		mrb.DefineClassUnder(zlibModule, "GzipWriter", gzfile)

		gzerr := mrb.DefineClassUnder(zlibModule, "Error", gzfile)
		mrb.DefineClassUnder(zlibModule, "LengthError", gzerr)
		mrb.DefineClassUnder(zlibModule, "CRCError", gzerr)
		mrb.DefineClassUnder(zlibModule, "NoFooter", gzerr)

		return nil
	})
}

func zlibAdler32(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str, adlr := mrb.GetArgs2()
	s := mrb.Bytes(str)
	a32 := mrb.Bytes(adlr)

	a := adler32.New()
	a.Sum(a32)
	if !str.IsNil() {
		ret, err := a.Write(s)
		if err != nil {
			return mrb.Raise(mrb.EArgumentError(), err.Error())
		}
		return mrb.Value(ret)
	}
	return mrb.Value(a.Sum32())
}

// BASE is largest prime smaller than 65536
const BASE = uint(65521)

func zlibAdler32Combine(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	adlr1, adlr2, len2 := mrb.GetArgs3()
	adler1 := uint(adlr1.Int())
	adler2 := uint(adlr2.Int())

	// the derivation of this formula is left as an exercise for the reader
	rem := uint(len2.Int()) % BASE
	sum1 := adler1 & 0xffff
	sum2 := rem * sum1
	sum2 %= BASE
	sum1 += (adler2 & 0xffff) + BASE - 1
	sum2 += ((adler1 >> 16) & 0xffff) + ((adler2 >> 16) & 0xffff) + BASE - rem
	if sum1 >= BASE {
		sum1 -= BASE
	}
	if sum1 >= BASE {
		sum1 -= BASE
	}
	if sum2 >= (BASE << 1) {
		sum2 -= (BASE << 1)
	}
	if sum2 >= BASE {
		sum2 -= BASE
	}

	return mrb.FixnumValue(int(sum1 | (sum2 << 16)))
}

func zlibCrc32(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str, crc := mrb.GetArgs2()
	s := mrb.Bytes(str)
	c32 := mrb.Bytes(crc)

	a := crc32.NewIEEE()
	a.Sum(c32)
	if !str.IsNil() {
		ret, err := a.Write(s)
		if err != nil {
			return mrb.Raise(mrb.EArgumentError(), err.Error())
		}
		return mrb.Value(ret)
	}
	return mrb.Value(a.Sum32())
}

func gf2MatrixTimes(mat []uint, vec uint) uint {
	sum := uint(0)
	idx := 0
	for vec != 0 {
		if (vec & 1) != 0 {
			sum ^= mat[idx]
		}
		vec >>= 1
		idx++
	}
	return sum
}

// GF2DIM is dimension of GF(2) vectors (length of CRC)
const GF2DIM = 32

func gf2MatrixSquare(square, mat []uint) {
	for n := 0; n < GF2DIM; n++ {
		square[n] = gf2MatrixTimes(mat, mat[n])
	}
}

func zlibCrc32Combine(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	c1, c2, l2 := mrb.GetArgs3()
	crc1 := uint(c1.Int())
	crc2 := uint(c2.Int())
	len2 := uint(l2.Int())

	even := make([]uint, GF2DIM) // even-power-of-two zeros operator
	odd := make([]uint, GF2DIM)  // odd-power-of-two zeros operator

	// degenerate case (also disallow negative lengths)
	if len2 <= 0 {
		return mrb.FixnumValue(int(crc1))
	}

	// put operator for one zero bit in odd
	odd[0] = uint(0xedb88320) /* CRC-32 polynomial */
	row := uint(1)

	for n := 1; n < GF2DIM; n++ {
		odd[n] = row
		row <<= 1
	}

	// put operator for two zero bits in even
	gf2MatrixSquare(even, odd)

	// Put operator for four zero bits in odd
	gf2MatrixSquare(odd, even)

	/* apply len2 zeros to crc1 (first square will put the operator for one
	   zero byte, eight zero bits, in even) */
	for {
		/* apply zeros operator for this bit of len2 */
		gf2MatrixSquare(even, odd)
		if (len2 & 1) != 0 {
			crc1 = gf2MatrixTimes(even, crc1)
		}
		len2 >>= 1

		// if no more bits set, then done
		if len2 == 0 {
			break
		}

		// another iteration of the loop with odd and even swapped
		gf2MatrixSquare(odd, even)
		if (len2 & 1) != 0 {
			crc1 = gf2MatrixTimes(odd, crc1)
		}
		len2 >>= 1

		// if no more bits set, then done
		if len2 == 0 {
			break
		}
	}

	// return combined crc
	crc1 ^= crc2
	return mrb.FixnumValue(int(crc1))
}

func zlibCrc32Table(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.Value(crc32.IEEETable)
}

func zlibDeflate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data, level := mrb.GetArgs2("", flate.DefaultCompression)

	var b bytes.Buffer
	w, err := flate.NewWriter(&b, level.Int())
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	defer w.Close()

	_, err = w.Write(data.Bytes())
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}

	return mrb.BytesValue(b.Bytes())
}

func zlibInflate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.GetArgsFirst().Bytes()
	buf := bytes.NewBuffer(data)

	r := flate.NewReader(buf)
	defer r.Close()

	ret := make([]byte, 0, len(data)*2)
	_, err := r.Read(ret)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}

	return mrb.BytesValue(ret)
}

func zlibGUnZip(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.GetArgsFirst().Bytes()
	buf := bytes.NewBuffer(data)

	r, err := zlib.NewReader(buf)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	defer r.Close()

	ret := make([]byte, 0, len(data)*2)
	_, err = r.Read(ret)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}

	return mrb.BytesValue(ret)
}

func zlibGZip(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data, level := mrb.GetArgs2("", zlib.DefaultCompression)

	var b bytes.Buffer
	w, err := zlib.NewWriterLevel(&b, level.Int())
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	defer w.Close()

	_, err = w.Write(data.Bytes())
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}

	return mrb.BytesValue(b.Bytes())
}

// zlibVersion returns fake version 1.2.11. This gem does not use zlib, but
// golang internal implementation
func zlibVersion(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.StringValue("1.2.11")
}
