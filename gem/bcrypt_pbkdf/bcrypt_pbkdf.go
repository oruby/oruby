// Package bcrypt_pbkdf implements Ruby's bcrypt_pbkdf gem (and its
// bcrypt_pbkdf_ext native extension) in pure Go using crypto/sha512 and
// golang.org/x/crypto/blowfish.
package bcrypt_pbkdf

import (
	"crypto/sha512"
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/blowfish"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

const blockSize = 32

func init() {
	oruby.LazyGem("bcrypt_pbkdf_ext", initExt)
	oruby.LazyGem("bcrypt_pbkdf", initGem)
}

func initExt(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("BCryptPbkdf")
	eng := mrb.DefineClassUnder(mod, "Engine", mrb.ObjectClass())
	eng.DefineClassMethod("__bc_crypt_pbkdf", pbkdfMethod, mrb.ArgsReq(4))
	return nil
}

func initGem(mrb *oruby.MrbState) any {
	initExt(mrb)
	mod := mrb.DefineModule("BCryptPbkdf")
	mod.DefineClassMethod("key", pbkdfMethod, mrb.ArgsReq(4))
	return nil
}

func pbkdfMethod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	pass, err := rb.Bytes(mrb, args.Item(0))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	salt, err := rb.Bytes(mrb, args.Item(1))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	keylen, err := rb.Int(mrb, args.Item(2))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	rounds, err := rb.Int(mrb, args.Item(3))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	key, err := deriveKey(pass, salt, int(rounds), int(keylen))
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return mrb.Call(mrb.BytesValue(key), "b")
}

// deriveKey implements OpenBSD's bcrypt_pbkdf key derivation function.
func deriveKey(password, salt []byte, iter, keyLen int) ([]byte, error) {
	if iter < 1 {
		return nil, errors.New("bcrypt_pbkdf: number of rounds must be >= 1")
	}
	if keyLen < 1 {
		return nil, errors.New("bcrypt_pbkdf: key length must be >= 1")
	}
	if keyLen > 1024 {
		return nil, errors.New("bcrypt_pbkdf: key length too large")
	}
	if len(password) == 0 || len(salt) == 0 {
		return nil, errors.New("bcrypt_pbkdf: password and salt must be non-empty")
	}

	numBlocks := (keyLen + blockSize - 1) / blockSize
	key := make([]byte, numBlocks*blockSize)

	h := sha512.New()
	h.Write(password)
	shapass := h.Sum(nil)

	shasalt := make([]byte, 0, sha512.Size)
	cnt := make([]byte, 4)
	tmp := make([]byte, blockSize)
	out := make([]byte, blockSize)

	for block := 1; block <= numBlocks; block++ {
		h.Reset()
		h.Write(salt)
		binary.BigEndian.PutUint32(cnt, uint32(block))
		h.Write(cnt)
		shasalt = h.Sum(shasalt[:0])

		bcryptHash(tmp, shapass, shasalt)
		copy(out, tmp)

		for i := 2; i <= iter; i++ {
			h.Reset()
			h.Write(tmp)
			shasalt = h.Sum(shasalt[:0])
			bcryptHash(tmp, shapass, shasalt)
			for j := 0; j < blockSize; j++ {
				out[j] ^= tmp[j]
			}
		}

		for i, v := range out {
			key[i*numBlocks+(block-1)] = v
		}
	}
	return key[:keyLen], nil
}

var pbkdfMagic = []byte("OxychromaticBlowfishSwatDynamite")

func bcryptHash(out, sha2pass, sha2salt []byte) {
	c, err := blowfish.NewSaltedCipher(sha2pass, sha2salt)
	if err != nil {
		panic(err)
	}
	for i := 0; i < 64; i++ {
		blowfish.ExpandKey(sha2salt, c)
		blowfish.ExpandKey(sha2pass, c)
	}
	copy(out, pbkdfMagic)
	for i := 0; i < 64; i++ {
		c.Encrypt(out[0:8], out[0:8])
		c.Encrypt(out[8:16], out[8:16])
		c.Encrypt(out[16:24], out[16:24])
		c.Encrypt(out[24:32], out[24:32])
	}
	// OpenBSD bcrypt_hash outputs each 32-bit word in little-endian order.
	for i := 0; i < 8; i++ {
		off := i * 4
		v := binary.BigEndian.Uint32(out[off : off+4])
		binary.LittleEndian.PutUint32(out[off:off+4], v)
	}
}
