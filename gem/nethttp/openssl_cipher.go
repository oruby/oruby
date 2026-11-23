package nethttp

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"golang.org/x/crypto/chacha20poly1305"
)

// ---------------------------------------------------------------------------
// OpenSSL::Cipher (AES-128/192/256 GCM/CBC/CTR/CFB/OFB/ECB, ChaCha20-Poly1305)
// ---------------------------------------------------------------------------

type cipherMode int

const (
	cmGCM cipherMode = iota
	cmCBC
	cmCTR
	cmCFB
	cmOFB
	cmECB
	cmChaCha20Poly1305
)

type osslCipher struct {
	name      string
	mode      cipherMode
	keyLen    int
	ivLen     int
	blockSize int
	aead      bool

	encrypting bool
	dirSet     bool
	padding    bool
	key        []byte
	iv         []byte
	authData   []byte
	authTag    []byte
	authTagSet bool

	buf    bytes.Buffer // buffered plaintext/ciphertext (for GCM/ChaCha20 & CBC block alignment)
	stream cipher.Stream
	cbc    cipher.BlockMode
	block  cipher.Block
}

var supportedCiphers = []string{
	"aes-128-cbc", "aes-192-cbc", "aes-256-cbc",
	"aes-128-gcm", "aes-192-gcm", "aes-256-gcm",
	"aes-128-ctr", "aes-192-ctr", "aes-256-ctr",
	"aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
	"aes-128-ofb", "aes-192-ofb", "aes-256-ofb",
	"aes-128-ecb", "aes-192-ecb", "aes-256-ecb",
	"chacha20-poly1305",
}

func newOsslCipher(name string) (*osslCipher, error) {
	norm := strings.ToLower(strings.TrimSpace(name))
	if norm == "chacha20-poly1305" || norm == "chacha20_poly1305" {
		return &osslCipher{
			name:      "chacha20-poly1305",
			mode:      cmChaCha20Poly1305,
			keyLen:    32,
			ivLen:     12,
			blockSize: 1,
			aead:      true,
			padding:   true,
		}, nil
	}
	parts := strings.Split(strings.ReplaceAll(norm, "_", "-"), "-")
	if len(parts) == 3 && parts[0] == "aes" {
		bits, err := strconv.Atoi(parts[1])
		if err != nil || (bits != 128 && bits != 192 && bits != 256) {
			return nil, fmt.Errorf("unsupported cipher algorithm (%s)", name)
		}
		keyLen := bits / 8
		c := &osslCipher{
			name:      fmt.Sprintf("AES-%d-%s", bits, strings.ToUpper(parts[2])),
			keyLen:    keyLen,
			blockSize: aes.BlockSize,
			padding:   true,
		}
		switch parts[2] {
		case "gcm":
			c.mode = cmGCM
			c.ivLen = 12
			c.blockSize = 1
			c.aead = true
		case "cbc":
			c.mode = cmCBC
			c.ivLen = aes.BlockSize
		case "ctr":
			c.mode = cmCTR
			c.ivLen = aes.BlockSize
			c.blockSize = 1
		case "cfb":
			c.mode = cmCFB
			c.ivLen = aes.BlockSize
			c.blockSize = 1
		case "ofb":
			c.mode = cmOFB
			c.ivLen = aes.BlockSize
			c.blockSize = 1
		case "ecb":
			c.mode = cmECB
			c.ivLen = 0
		default:
			return nil, fmt.Errorf("unsupported cipher algorithm (%s)", name)
		}
		return c, nil
	}
	return nil, fmt.Errorf("unsupported cipher algorithm (%s)", name)
}

func (c *osslCipher) resetState() {
	c.buf.Reset()
	c.stream = nil
	c.cbc = nil
	c.block = nil
	if c.encrypting {
		c.authTag = nil
		c.authTagSet = false
	}
}

func (c *osslCipher) ensureInitialized() error {
	if !c.dirSet {
		return errors.New("cipher direction not set (call #encrypt or #decrypt)")
	}
	if len(c.key) == 0 {
		return errors.New("key not set")
	}
	if len(c.key) != c.keyLen {
		return fmt.Errorf("key length too short/long: expected %d, got %d", c.keyLen, len(c.key))
	}
	if c.ivLen > 0 && len(c.iv) == 0 {
		c.iv = make([]byte, c.ivLen)
	}
	if c.aead {
		return nil
	}
	if c.block != nil {
		return nil
	}
	blk, err := aes.NewCipher(c.key)
	if err != nil {
		return err
	}
	c.block = blk
	switch c.mode {
	case cmCBC:
		if len(c.iv) < aes.BlockSize {
			return fmt.Errorf("iv length too short: expected %d, got %d", aes.BlockSize, len(c.iv))
		}
		if c.encrypting {
			c.cbc = cipher.NewCBCEncrypter(blk, c.iv[:aes.BlockSize])
		} else {
			c.cbc = cipher.NewCBCDecrypter(blk, c.iv[:aes.BlockSize])
		}
	case cmCTR:
		c.stream = cipher.NewCTR(blk, c.iv[:aes.BlockSize])
	case cmCFB:
		if c.encrypting {
			c.stream = cipher.NewCFBEncrypter(blk, c.iv[:aes.BlockSize])
		} else {
			c.stream = cipher.NewCFBDecrypter(blk, c.iv[:aes.BlockSize])
		}
	case cmOFB:
		c.stream = cipher.NewOFB(blk, c.iv[:aes.BlockSize])
	}
	return nil
}

func (c *osslCipher) update(in []byte) ([]byte, error) {
	if err := c.ensureInitialized(); err != nil {
		return nil, err
	}
	if c.aead {
		// Buffer until final() where Seal/Open runs with full AAD + authTag
		c.buf.Write(in)
		return nil, nil
	}
	switch c.mode {
	case cmCTR, cmCFB, cmOFB:
		out := make([]byte, len(in))
		c.stream.XORKeyStream(out, in)
		return out, nil
	case cmCBC, cmECB:
		c.buf.Write(in)
		bs := aes.BlockSize
		// When decrypting with padding, hold back at least 1 full block for final() unpadding
		avail := c.buf.Len()
		var process int
		if !c.encrypting && c.padding {
			if avail <= bs {
				return nil, nil
			}
			process = ((avail - 1) / bs) * bs
		} else {
			process = (avail / bs) * bs
		}
		if process == 0 {
			return nil, nil
		}
		chunk := make([]byte, process)
		_, _ = c.buf.Read(chunk)
		out := make([]byte, process)
		if c.mode == cmCBC {
			c.cbc.CryptBlocks(out, chunk)
		} else {
			for i := 0; i < process; i += bs {
				if c.encrypting {
					c.block.Encrypt(out[i:i+bs], chunk[i:i+bs])
				} else {
					c.block.Decrypt(out[i:i+bs], chunk[i:i+bs])
				}
			}
		}
		return out, nil
	}
	return nil, nil
}

func (c *osslCipher) final() ([]byte, error) {
	if err := c.ensureInitialized(); err != nil {
		return nil, err
	}
	defer func() {
		c.buf.Reset()
		c.stream = nil
		c.cbc = nil
		c.block = nil
	}()

	if c.aead {
		var aeadImpl cipher.AEAD
		var err error
		if c.mode == cmGCM {
			blk, berr := aes.NewCipher(c.key)
			if berr != nil {
				return nil, berr
			}
			if len(c.iv) == 12 {
				aeadImpl, err = cipher.NewGCM(blk)
			} else {
				aeadImpl, err = cipher.NewGCMWithNonceSize(blk, len(c.iv))
			}
		} else {
			aeadImpl, err = chacha20poly1305.New(c.key)
		}
		if err != nil {
			return nil, err
		}
		data := c.buf.Bytes()
		if c.encrypting {
			sealed := aeadImpl.Seal(nil, c.iv, data, c.authData)
			tagSize := aeadImpl.Overhead()
			ct := sealed[:len(sealed)-tagSize]
			c.authTag = append([]byte(nil), sealed[len(sealed)-tagSize:]...)
			c.authTagSet = true
			return ct, nil
		}
		if !c.authTagSet || len(c.authTag) == 0 {
			return nil, errors.New("auth_tag not set")
		}
		combined := make([]byte, len(data)+len(c.authTag))
		copy(combined, data)
		copy(combined[len(data):], c.authTag)
		plain, err := aeadImpl.Open(nil, c.iv, combined, c.authData)
		if err != nil {
			return nil, errors.New("cipher authentication failed")
		}
		return plain, nil
	}

	switch c.mode {
	case cmCTR, cmCFB, cmOFB:
		return nil, nil
	case cmCBC, cmECB:
		bs := aes.BlockSize
		rem := c.buf.Bytes()
		if c.encrypting {
			var block []byte
			if c.padding {
				padLen := bs - (len(rem) % bs)
				block = make([]byte, len(rem)+padLen)
				copy(block, rem)
				for i := len(rem); i < len(block); i++ {
					block[i] = byte(padLen)
				}
			} else {
				if len(rem)%bs != 0 {
					return nil, errors.New("data not multiple of block length")
				}
				if len(rem) == 0 {
					return nil, nil
				}
				block = append([]byte(nil), rem...)
			}
			out := make([]byte, len(block))
			if c.mode == cmCBC {
				c.cbc.CryptBlocks(out, block)
			} else {
				for i := 0; i < len(block); i += bs {
					c.block.Encrypt(out[i:i+bs], block[i:i+bs])
				}
			}
			return out, nil
		}
		// Decrypting
		if len(rem) == 0 {
			if c.padding {
				return nil, errors.New("wrong final block length")
			}
			return nil, nil
		}
		if len(rem)%bs != 0 {
			return nil, errors.New("wrong final block length")
		}
		out := make([]byte, len(rem))
		if c.mode == cmCBC {
			c.cbc.CryptBlocks(out, rem)
		} else {
			for i := 0; i < len(rem); i += bs {
				c.block.Decrypt(out[i:i+bs], rem[i:i+bs])
			}
		}
		if c.padding {
			padLen := int(out[len(out)-1])
			if padLen == 0 || padLen > bs || padLen > len(out) {
				return nil, errors.New("bad decrypt")
			}
			for i := len(out) - padLen; i < len(out); i++ {
				if int(out[i]) != padLen {
					return nil, errors.New("bad decrypt")
				}
			}
			out = out[:len(out)-padLen]
		}
		return out, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// OpenSSL::ASN1 DER encoder & decoder
// ---------------------------------------------------------------------------

func encodeDERLength(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var tmp [8]byte
	i := len(tmp)
	for v := n; v > 0; v >>= 8 {
		i--
		tmp[i] = byte(v)
	}
	cnt := len(tmp) - i
	out := make([]byte, 1+cnt)
	out[0] = byte(0x80 | cnt)
	copy(out[1:], tmp[i:])
	return out
}

func encodeDERElement(tagClass, tag int, constructed bool, content []byte) []byte {
	hdr := byte((tagClass & 0x03) << 6)
	if constructed {
		hdr |= 0x20
	}
	hdr |= byte(tag & 0x1F)
	lenBytes := encodeDERLength(len(content))
	out := make([]byte, 0, 1+len(lenBytes)+len(content))
	out = append(out, hdr)
	out = append(out, lenBytes...)
	out = append(out, content...)
	return out
}

func encodeDERBigInt(n *big.Int) []byte {
	if n == nil || n.Sign() == 0 {
		return []byte{0}
	}
	if n.Sign() > 0 {
		b := n.Bytes()
		if b[0]&0x80 != 0 {
			b = append([]byte{0}, b...)
		}
		return b
	}
	// Negative two's complement
	abs := new(big.Int).Abs(n)
	bitLen := abs.BitLen()
	byteLen := (bitLen + 8) / 8
	mod := new(big.Int).Lsh(big.NewInt(1), uint(byteLen*8))
	twos := new(big.Int).Sub(mod, abs)
	b := twos.Bytes()
	if len(b) < byteLen {
		pad := make([]byte, byteLen)
		for i := 0; i < byteLen-len(b); i++ {
			pad[i] = 0xFF
		}
		copy(pad[byteLen-len(b):], b)
		b = pad
	}
	return b
}

func decodeDERBigInt(b []byte) *big.Int {
	if len(b) == 0 {
		return big.NewInt(0)
	}
	n := new(big.Int).SetBytes(b)
	if b[0]&0x80 != 0 {
		mod := new(big.Int).Lsh(big.NewInt(1), uint(len(b)*8))
		n.Sub(n, mod)
	}
	return n
}

func decodeDERAt(mrb *oruby.MrbState, asn1Mod oruby.RClass, data []byte, pos int) (oruby.Value, int, error) {
	if pos >= len(data) {
		return mrb.NilValue(), pos, errors.New("unexpected end of ASN.1 DER")
	}
	b0 := data[pos]
	pos++
	tagClass := int((b0 >> 6) & 0x03)
	constructed := (b0 & 0x20) != 0
	tag := int(b0 & 0x1F)
	if tag == 0x1F {
		// High-tag-number form
		tag = 0
		for {
			if pos >= len(data) {
				return mrb.NilValue(), pos, errors.New("truncated ASN.1 high tag")
			}
			tb := data[pos]
			pos++
			tag = (tag << 7) | int(tb&0x7F)
			if tb&0x80 == 0 {
				break
			}
		}
	}
	if pos >= len(data) {
		return mrb.NilValue(), pos, errors.New("truncated ASN.1 length")
	}
	lb := data[pos]
	pos++
	var length int
	if lb&0x80 == 0 {
		length = int(lb)
	} else {
		cnt := int(lb & 0x7F)
		if cnt == 0 || cnt > 4 || pos+cnt > len(data) {
			return mrb.NilValue(), pos, errors.New("unsupported/truncated ASN.1 length")
		}
		for i := 0; i < cnt; i++ {
			length = (length << 8) | int(data[pos])
			pos++
		}
	}
	if pos+length > len(data) {
		return mrb.NilValue(), pos, errors.New("ASN.1 content exceeds buffer")
	}
	body := data[pos : pos+length]
	nextPos := pos + length

	tagClassSym := func(tc int) oruby.Value {
		switch tc {
		case 0:
			return mrb.Intern("UNIVERSAL").Value()
		case 1:
			return mrb.Intern("APPLICATION").Value()
		case 2:
			return mrb.Intern("CONTEXT_SPECIFIC").Value()
		default:
			return mrb.Intern("PRIVATE").Value()
		}
	}

	if tagClass == 0 { // UNIVERSAL
		switch tag {
		case 1: // BOOLEAN
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::Boolean")
			v, err := mrb.Funcall(cls, mrb.Intern("new"), mrb.BoolValue(len(body) > 0 && body[0] != 0))
			return v, nextPos, err
		case 2: // INTEGER
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::Integer")
			bn := newBNValue(mrb, decodeDERBigInt(body))
			v, err := mrb.Funcall(cls, mrb.Intern("new"), bn)
			return v, nextPos, err
		case 3: // BIT STRING
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::BitString")
			unused := 0
			payload := body
			if len(body) > 0 {
				unused = int(body[0])
				payload = body[1:]
			}
			sv := mrb.Call(mrb.BytesValue(payload), "force_encoding", mrb.StrNew("ASCII-8BIT"))
			v, err := mrb.Funcall(cls, mrb.Intern("new"), sv)
			if err == nil {
				mrb.SetIV(v, "@unused_bits", mrb.FixnumValue(unused))
			}
			return v, nextPos, err
		case 4: // OCTET STRING
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::OctetString")
			sv := mrb.Call(mrb.BytesValue(body), "force_encoding", mrb.StrNew("ASCII-8BIT"))
			v, err := mrb.Funcall(cls, mrb.Intern("new"), sv)
			return v, nextPos, err
		case 5: // NULL
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::Null")
			v, err := mrb.Funcall(cls, mrb.Intern("new"), mrb.NilValue())
			return v, nextPos, err
		case 6: // OBJECT IDENTIFIER
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::ObjectId")
			var oid asn1.ObjectIdentifier
			full := encodeDERElement(0, 6, false, body)
			oidStr := ""
			if _, err := asn1.Unmarshal(full, &oid); err == nil {
				oidStr = oid.String()
			}
			v, err := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(oidStr))
			return v, nextPos, err
		case 12: // UTF8String
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::UTF8String")
			v, err := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(string(body)))
			return v, nextPos, err
		case 19: // PrintableString
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::PrintableString")
			v, err := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(string(body)))
			return v, nextPos, err
		case 22: // IA5String
			cls := rb.ClassPath(mrb, "OpenSSL::ASN1::IA5String")
			v, err := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(string(body)))
			return v, nextPos, err
		case 16, 17: // SEQUENCE, SET
			ary := mrb.AryNew()
			cpos := 0
			for cpos < len(body) {
				child, nxt, err := decodeDERAt(mrb, asn1Mod, body, cpos)
				if err != nil {
					return mrb.NilValue(), pos, err
				}
				mrb.AryPush(ary, child)
				cpos = nxt
			}
			clsName := "OpenSSL::ASN1::Sequence"
			if tag == 17 {
				clsName = "OpenSSL::ASN1::Set"
			}
			cls := rb.ClassPath(mrb, clsName)
			v, err := mrb.Funcall(cls, mrb.Intern("new"), ary)
			return v, nextPos, err
		}
	}

	// Generic ASN1Data (context-specific / application / unknown universal)
	dataCls := rb.ClassPath(mrb, "OpenSSL::ASN1::ASN1Data")
	var val oruby.Value
	if constructed {
		ary := mrb.AryNew()
		cpos := 0
		for cpos < len(body) {
			child, nxt, err := decodeDERAt(mrb, asn1Mod, body, cpos)
			if err != nil {
				return mrb.NilValue(), pos, err
			}
			mrb.AryPush(ary, child)
			cpos = nxt
		}
		val = ary.Value()
	} else {
		val = mrb.Call(mrb.BytesValue(body), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}
	v, err := mrb.Funcall(dataCls, mrb.Intern("new"), val, mrb.FixnumValue(tag), tagClassSym(tagClass))
	return v, nextPos, err
}

func initOpenSSLCipherPKCS5ASN1(mrb *oruby.MrbState, mod, osslErr, cipherCls, asn1Mod oruby.RClass) {
	cipherErr := rb.ClassPath(mrb, "OpenSSL::Cipher::CipherError")
	rb.DataClass(cipherCls)

	cipherCls.DefineClassMethod("ciphers", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		ary := mrb.AryNewCapa(len(supportedCiphers))
		for _, c := range supportedCiphers {
			mrb.AryPush(ary, mrb.StrNew(c))
		}
		return ary
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		name := mrb.GetArgsFirst().String()
		c, err := newOsslCipher(name)
		if err != nil {
			return mrb.Raise(mrb.ERuntimeError(), err.Error())
		}
		rb.Set(mrb, self, c)
		return self
	}, mrb.ArgsReq(1))

	getC := func(mrb *oruby.MrbState, self oruby.Value) (*osslCipher, oruby.MrbValue) {
		c, ok := rb.Get[*osslCipher](mrb, self)
		if !ok || c == nil {
			return nil, mrb.Raise(cipherErr, "Cipher not initialized")
		}
		return c, nil
	}

	cipherCls.DefineMethod("name", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		return mrb.StrNew(c.name)
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("encrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		c.encrypting = true
		c.dirSet = true
		c.resetState()
		return self
	}, mrb.ArgsOpt(2))

	cipherCls.DefineMethod("decrypt", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		c.encrypting = false
		c.dirSet = true
		c.resetState()
		return self
	}, mrb.ArgsOpt(2))

	cipherCls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		c.resetState()
		return self
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("key_len", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		return mrb.FixnumValue(c.keyLen)
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("key_len=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		c.keyLen = int(v.Int64())
		return v
	}, mrb.ArgsReq(1))

	cipherCls.DefineMethod("iv_len", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		return mrb.FixnumValue(c.ivLen)
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("iv_len=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		c.ivLen = int(v.Int64())
		return v
	}, mrb.ArgsReq(1))

	cipherCls.DefineMethod("block_size", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		return mrb.FixnumValue(c.blockSize)
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("authenticated?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		return mrb.BoolValue(c.aead)
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("padding=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		c.padding = v.Int64() != 0
		return v
	}, mrb.ArgsReq(1))

	cipherCls.DefineMethod("key=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		b, err := rb.Bytes(mrb, v)
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		if len(b) < c.keyLen {
			return mrb.Raise(mrb.EArgumentError(), fmt.Sprintf("key must be %d bytes", c.keyLen))
		}
		c.key = append([]byte(nil), b[:c.keyLen]...)
		c.resetState()
		return v
	}, mrb.ArgsReq(1))

	cipherCls.DefineMethod("iv=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		b, err := rb.Bytes(mrb, v)
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		if len(b) < c.ivLen {
			return mrb.Raise(mrb.EArgumentError(), fmt.Sprintf("iv must be %d bytes", c.ivLen))
		}
		c.iv = append([]byte(nil), b[:c.ivLen]...)
		c.resetState()
		return v
	}, mrb.ArgsReq(1))

	cipherCls.DefineMethod("random_key", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		k := make([]byte, c.keyLen)
		_, _ = rand.Read(k)
		c.key = k
		c.resetState()
		return mrb.Call(mrb.BytesValue(k), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("random_iv", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		iv := make([]byte, c.ivLen)
		_, _ = rand.Read(iv)
		c.iv = iv
		c.resetState()
		return mrb.Call(mrb.BytesValue(iv), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	cipherCls.DefineMethod("auth_data=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		b, err := rb.Bytes(mrb, v)
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		c.authData = append([]byte(nil), b...)
		return v
	}, mrb.ArgsReq(1))

	cipherCls.DefineMethod("auth_tag=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		v := mrb.GetArgsFirst()
		b, err := rb.Bytes(mrb, v)
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		c.authTag = append([]byte(nil), b...)
		c.authTagSet = true
		return v
	}, mrb.ArgsReq(1))

	cipherCls.DefineMethod("auth_tag", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		if !c.aead || !c.authTagSet {
			return mrb.Raise(cipherErr, "retrieving the authentication tag failed")
		}
		tagLen := len(c.authTag)
		if args := mrb.GetArgs().Slice(); len(args) >= 1 && args[0].IsInt() {
			req := int(args[0].Int64())
			if req < 1 || req > len(c.authTag) {
				return mrb.Raise(cipherErr, "invalid auth_tag length")
			}
			tagLen = req
		}
		return mrb.Call(mrb.BytesValue(c.authTag[:tagLen]), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsOpt(1))

	cipherCls.DefineMethod("update", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		args := mrb.GetArgs().Slice()
		in, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		out, err := c.update(in)
		if err != nil {
			return mrb.Raise(cipherErr, err.Error())
		}
		res := mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
		if len(args) >= 2 && args[1].IsString() {
			_, _ = mrb.Funcall(args[1], mrb.Intern("replace"), res)
			return args[1]
		}
		return res
	}, mrb.ArgsArg(1, 1))

	cipherCls.DefineMethod("final", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, exc := getC(mrb, self)
		if exc != nil {
			return exc
		}
		out, err := c.final()
		if err != nil {
			return mrb.Raise(cipherErr, err.Error())
		}
		return mrb.Call(mrb.BytesValue(out), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	// -- OpenSSL::PKCS5 -------------------------------------------------------
	pkcs5Mod := mrb.DefineModuleUnder(mod, "PKCS5")
	pkcs5Err := mrb.DefineClassUnder(pkcs5Mod, "PKCS5Error", osslErr)

	pkcs5Mod.DefineClassMethod("pbkdf2_hmac", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		pass, err := rb.Bytes(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		salt, err := rb.Bytes(mrb, a[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		iter := int(a[2].Int64())
		keyLen := int(a[3].Int64())
		digestName := "SHA256"
		if a[4].IsString() {
			digestName = a[4].String()
		} else if mrb.RespondTo(a[4], oruby.Sym_name) {
			if nv, err := mrb.Funcall(a[4], oruby.Sym_name); err == nil {
				digestName = nv.String()
			}
		}
		hFn, ok := hashConstructor(digestName)
		if !ok {
			return mrb.Raise(pkcs5Err, fmt.Sprintf("unsupported digest: %s", digestName))
		}
		if iter <= 0 || keyLen < 0 {
			return mrb.Raise(pkcs5Err, "invalid PBKDF2 parameters")
		}
		var dk []byte
		mrb.Blocking(func() {
			dk = pbkdf2HMAC(pass, salt, iter, keyLen, hFn)
		})
		return mrb.Call(mrb.BytesValue(dk), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(5))

	pkcs5Mod.DefineClassMethod("pbkdf2_hmac_sha1", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		pass, err := rb.Bytes(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		salt, err := rb.Bytes(mrb, a[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		iter := int(a[2].Int64())
		keyLen := int(a[3].Int64())
		hFn, _ := hashConstructor("SHA1")
		var dk []byte
		mrb.Blocking(func() {
			dk = pbkdf2HMAC(pass, salt, iter, keyLen, hFn)
		})
		return mrb.Call(mrb.BytesValue(dk), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(4))

	// -- OpenSSL::ASN1 DER decode & encode helpers ----------------------------
	asn1Err := mrb.DefineClassUnder(asn1Mod, "ASN1Error", osslErr)
	asn1Mod.Const("BOOLEAN", 1)

	asn1Mod.DefineClassMethod("decode", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		b, err := rb.Bytes(mrb, mrb.GetArgsFirst())
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		val, _, derr := decodeDERAt(mrb, asn1Mod, b, 0)
		if derr != nil {
			return mrb.Raise(asn1Err, derr.Error())
		}
		return val
	}, mrb.ArgsReq(1))

	asn1Mod.DefineClassMethod("__encode_int", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		bi, err := coerceToBigInt(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.Raise(asn1Err, err.Error())
		}
		return mrb.Call(mrb.BytesValue(encodeDERElement(0, 2, false, encodeDERBigInt(bi))), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	asn1Mod.DefineClassMethod("__encode_oid", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		s := mrb.GetArgsFirst().String()
		parts := strings.Split(s, ".")
		oid := make(asn1.ObjectIdentifier, 0, len(parts))
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				return mrb.Raise(asn1Err, fmt.Sprintf("invalid OID: %s", s))
			}
			oid = append(oid, n)
		}
		der, err := asn1.Marshal(oid)
		if err != nil {
			return mrb.Raise(asn1Err, err.Error())
		}
		return mrb.Call(mrb.BytesValue(der), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(1))

	asn1Mod.DefineClassMethod("__encode_elem", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		tc := int(a[0].Int64())
		tag := int(a[1].Int64())
		cons := a[2].Bool()
		body := a[3].Bytes()
		return mrb.Call(mrb.BytesValue(encodeDERElement(tc, tag, cons, body)), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(4))
}
