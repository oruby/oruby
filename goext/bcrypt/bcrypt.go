// Package bcrypt implements Ruby's bcrypt gem (and its bcrypt_ext native extension)
// in pure Go using golang.org/x/crypto/blowfish.
package bcrypt

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/blowfish"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed bcrypt.rb
var bcryptRb []byte

const bcryptAlphabet = "./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

var bcEncoding = base64.NewEncoding(bcryptAlphabet).WithPadding(base64.NoPadding)

var magicCipherData = []byte("OrpheanBeholderScryDoubt")

func init() {
	oruby.LazyGem("bcrypt_ext", initBCryptExt)
	oruby.LazyGem("bcrypt", func(mrb *oruby.MrbState) any {
		initBCryptExt(mrb)
		rb.LoadRuby(mrb, "bcrypt.rb", bcryptRb)
		return nil
	})
}

func initBCryptExt(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("BCrypt")
	eng := mrb.DefineClassUnder(mod, "Engine", mrb.ObjectClass())
	eng.DefineClassMethod("__bc_salt", bcSalt, mrb.ArgsReq(3))
	eng.DefineClassMethod("__bc_crypt", bcCrypt, mrb.ArgsReq(2))
	eng.DefineClassMethod("__random_bytes", bcRandomBytes, mrb.ArgsReq(1))
	return nil
}

func bcRandomBytes(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	n := mrb.GetArgsFirst().Int()
	if n < 0 {
		return mrb.Raise(mrb.EArgumentError(), "negative string size")
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}
	return mrb.Call(mrb.BytesValue(buf), "b")
}

// bcSalt implements BCrypt::Engine.__bc_salt(prefix, cost, raw_salt).
func bcSalt(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	prefix, err := rb.Str(mrb, args.Item(0))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	cost := args.Item(1).Int()
	rawSalt, err := rb.Bytes(mrb, args.Item(2))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if cost < 4 {
		cost = 4
	}
	if cost > 31 {
		cost = 31
	}
	if len(rawSalt) < 16 {
		return rb.Raise(mrb, "BCrypt::Errors::InvalidSalt", "invalid salt length")
	}
	// Strip surrounding '$' if passed like "$2a$"
	ver := strings.Trim(prefix, "$")
	if ver == "" {
		ver = "2a"
	}
	saltStr := fmt.Sprintf("$%s$%02d$%s", ver, cost, bcEncodeBytes(rawSalt[:16]))
	return mrb.StrNew(saltStr)
}

// bcCrypt implements BCrypt::Engine.__bc_crypt(secret, salt).
func bcCrypt(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	secret, err := rb.Bytes(mrb, args.Item(0))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if bytes.IndexByte(secret, 0) >= 0 {
		return mrb.Raise(mrb.EArgumentError(), "string contains null byte")
	}
	saltStr, err := rb.Str(mrb, args.Item(1))
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if strings.IndexByte(saltStr, 0) >= 0 {
		return mrb.Raise(mrb.EArgumentError(), "string contains null byte")
	}
	out, err := hashSecret(secret, saltStr)
	if err != nil {
		return rb.Raise(mrb, "BCrypt::Errors::InvalidSalt", "%s", err.Error())
	}
	return mrb.StrNew(out)
}

// bcEncodeBytes encodes bytes with bcrypt's radix-64 (no padding, 16->22, 23->31).
func bcEncodeBytes(src []byte) string {
	return bcEncoding.EncodeToString(src)
}

// bcDecodeSalt decodes a 22-character bcrypt radix-64 salt into 16 bytes.
func bcDecodeSalt(s string) ([]byte, error) {
	if len(s) < 22 {
		return nil, errors.New("salt too short")
	}
	s = s[:22]
	// Bcrypt's 22-char salt encodes 16 bytes (128 bits = 21*6 + 2 bits);
	// Go's base64 decoder rejects non-zero trailing bits unless masked or decoded directly.
	var out [16]byte
	var vals [22]byte
	for i := 0; i < 22; i++ {
		idx := strings.IndexByte(bcryptAlphabet, s[i])
		if idx < 0 {
			return nil, errors.New("invalid base64 character in salt")
		}
		vals[i] = byte(idx)
	}
	oi := 0
	for i := 0; i < 20; i += 4 {
		c1, c2, c3, c4 := vals[i], vals[i+1], vals[i+2], vals[i+3]
		out[oi] = (c1 << 2) | (c2 >> 4)
		out[oi+1] = ((c2 & 0x0f) << 4) | (c3 >> 2)
		out[oi+2] = ((c3 & 0x03) << 6) | c4
		oi += 3
	}
	out[15] = (vals[20] << 2) | (vals[21] >> 4)
	return out[:], nil
}

// hashSecret computes the 60-byte OpenBSD bcrypt hash for secret and saltStr.
func hashSecret(secret []byte, saltStr string) (string, error) {
	// Parse "$<ver>$<cost>$<22-char-salt>[<31-char-hash>]"
	if len(saltStr) < 29 || saltStr[0] != '$' {
		return "", errors.New("invalid salt format")
	}
	parts := strings.SplitN(saltStr[1:], "$", 3)
	if len(parts) != 3 {
		return "", errors.New("invalid salt format")
	}
	ver := parts[0]
	if ver != "2a" && ver != "2b" && ver != "2x" && ver != "2y" && ver != "2" {
		return "", fmt.Errorf("unsupported bcrypt version: %s", ver)
	}
	cost, err := strconv.Atoi(parts[1])
	if err != nil || cost < 4 || cost > 31 {
		return "", errors.New("invalid cost")
	}
	if len(parts[2]) < 22 {
		return "", errors.New("invalid salt length")
	}
	saltPart := parts[2][:22]
	rawSalt, err := bcDecodeSalt(saltPart)
	if err != nil {
		return "", err
	}

	// OpenBSD / bcrypt-ruby key preparation: truncate to 72 bytes, append NUL terminator.
	pw := secret
	if len(pw) > 72 {
		pw = pw[:72]
	}
	key := make([]byte, len(pw)+1)
	copy(key, pw)
	key[len(pw)] = 0

	c, err := blowfish.NewSaltedCipher(key, rawSalt)
	if err != nil {
		return "", err
	}
	rounds := uint64(1) << uint(cost)
	for i := uint64(0); i < rounds; i++ {
		blowfish.ExpandKey(key, c)
		blowfish.ExpandKey(rawSalt, c)
	}

	var cdata [24]byte
	copy(cdata[:], magicCipherData)
	for i := 0; i < 64; i++ {
		c.Encrypt(cdata[0:8], cdata[0:8])
		c.Encrypt(cdata[8:16], cdata[8:16])
		c.Encrypt(cdata[16:24], cdata[16:24])
	}

	// Canonicalize salt22 from decoded bytes (matches OpenBSD crypt_blowfish).
	canonSalt := bcEncodeBytes(rawSalt)
	hashPart := bcEncodeBytes(cdata[:23])
	return fmt.Sprintf("$%s$%02d$%s%s", ver, cost, canonSalt, hashPart), nil
}
