package core_ext

// Array#pack and String#unpack as MRI's pack.c has them. The Ruby side
// (ruby/31_pack.rb) converts the format and keyword arguments; element
// conversions (#to_int, #to_str, Float) call back into Ruby helpers.

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/oruby/oruby"
)

// packError is raised as a Ruby exception by the method that recovers it
type packError struct {
	class string
	msg   string
	err   error // a Ruby exception from a callback, re-raised as is
}

func packFail(class, format string, a ...any) {
	panic(packError{class: class, msg: fmt.Sprintf(format, a...)})
}

func packRecover(mrb *oruby.MrbState, ret *oruby.MrbValue) {
	r := recover()
	if r == nil {
		return
	}
	pe, ok := r.(packError)
	if !ok {
		panic(r)
	}
	if pe.err != nil {
		*ret = mrb.RaiseError(pe.err)
		return
	}
	*ret = mrb.Raise(mrb.ExcGet(pe.class), pe.msg)
}

type packer struct {
	mrb *oruby.MrbState
}

func (p packer) call(recv oruby.MrbValue, name string, args ...interface{}) oruby.Value {
	v, err := p.mrb.Funcall(recv, p.mrb.Intern(name), args...)
	if err != nil {
		panic(packError{err: err})
	}
	return v
}

func (p packer) kernel(name string, args ...interface{}) oruby.Value {
	return p.call(p.mrb.KernelModule(), name, args...)
}

// toStr: #to_str conversion; nil allowed when nilOK
func (p packer) toStr(v oruby.Value, nilOK bool) []byte {
	if v.IsString() {
		return v.Bytes()
	}
	if nilOK && v.IsNil() {
		return nil
	}
	return p.kernel("__implicit", v, p.mrb.StringClass().Value()).Bytes()
}

func isFixnum(v oruby.Value) bool { return v.Type() == oruby.MrbTTInteger }

// toInt: the Integer an element packs as; big is set for a Bignum
func (p packer) toInt(v oruby.Value) (int64, *big.Int) {
	if !isFixnum(v) {
		v = p.kernel("__to_int", v)
	}
	if isFixnum(v) {
		return v.Int64(), nil
	}
	b, ok := new(big.Int).SetString(p.call(v, "to_s").String(), 10)
	if !ok {
		packFail("TypeError", "can't convert to Integer")
	}
	if b.IsInt64() {
		return b.Int64(), nil
	}
	return 0, b
}

func (p packer) toBig(v oruby.Value) *big.Int {
	i, b := p.toInt(v)
	if b != nil {
		return b
	}
	return big.NewInt(i)
}

func (p packer) toFloat(v oruby.Value) float64 {
	switch v.Type() {
	case oruby.MrbTTFloat:
		return v.Float64()
	case oruby.MrbTTInteger:
		return float64(v.Int64())
	}
	return p.kernel("__pack_float", v).Float64()
}

// integer from Ruby: a Fixnum, or a Bignum built from its decimal digits
func (p packer) intValue(b *big.Int) oruby.Value {
	if b.IsInt64() {
		return oruby.Int64(b.Int64())
	}
	return p.kernel("Integer", p.mrb.StrNew(b.String()))
}

const natTypes = "sSiIlLqQjJ"

// directive is one parsed format item
type directive struct {
	t           byte
	count       int
	star        bool
	hasCount    bool
	native      bool // _ or !
	big, little bool // > <
	align       int  // x!N, @!N: alignment
}

// parseFormat splits a pack/unpack format into directives
func parseFormat(f string, what string) []directive {
	var ds []directive
	for i := 0; i < len(f); {
		c := f[i]
		i++
		switch c {
		case ' ', '\t', '\n', '\v', '\f', '\r':
			continue
		case '#':
			for i < len(f) && f[i] != '\n' {
				i++
			}
			continue
		}
		d := directive{t: c, count: 1}
		if (c == 'x' || c == '@') && i < len(f) && f[i] == '!' {
			// x!N / @!N align to N bytes; x!i to the size of directive i
			i++
			if i < len(f) && f[i] >= '0' && f[i] <= '9' {
				n := 0
				for i < len(f) && f[i] >= '0' && f[i] <= '9' {
					n = n*10 + int(f[i]-'0')
					i++
				}
				d.align = n
			} else if i < len(f) {
				ad := directive{t: f[i]}
				i++
				for i < len(f) && strings.IndexByte("_!<>", f[i]) >= 0 {
					if f[i] == '_' || f[i] == '!' {
						ad.native = true
					}
					i++
				}
				size, _, ok := intSize(ad)
				if !ok {
					switch ad.t {
					case 'd', 'D', 'E', 'G':
						size = 8
					case 'f', 'F', 'e', 'g':
						size = 4
					default:
						packFail("ArgumentError", "unknown alignment directive '%c'", ad.t)
					}
				}
				d.align = size
			}
			if d.align <= 0 {
				packFail("ArgumentError", "alignment must be positive")
			}
			ds = append(ds, d)
			continue
		}
	mods:
		for i < len(f) {
			switch f[i] {
			case '_', '!':
				if !strings.ContainsRune(natTypes, rune(c)) {
					packFail("ArgumentError", "'%c' allowed only after types %s", f[i], natTypes)
				}
				d.native = true
				i++
			case '<', '>':
				if !strings.ContainsRune(natTypes, rune(c)) {
					packFail("ArgumentError", "'%c' allowed only after types %s", f[i], natTypes)
				}
				if d.big || d.little {
					packFail("ArgumentError", "Can't use both '<' and '>'")
				}
				if f[i] == '>' {
					d.big = true
				} else {
					d.little = true
				}
				i++
			default:
				break mods
			}
		}
		if i < len(f) && f[i] == '*' {
			d.star, d.hasCount = true, true
			i++
		} else if i < len(f) && f[i] >= '0' && f[i] <= '9' {
			n := 0
			for i < len(f) && f[i] >= '0' && f[i] <= '9' {
				n = n*10 + int(f[i]-'0')
				if n > math.MaxInt32 {
					packFail("RangeError", "%s length too big", what)
				}
				i++
			}
			d.count, d.hasCount = n, true
		}
		ds = append(ds, d)
	}
	return ds
}

func unknownDirective(what string, c byte, f string) {
	if c >= 0x20 && c < 0x7f {
		packFail("ArgumentError", "unknown %s directive '%c' in '%s'", what, c, f)
	}
	packFail("ArgumentError", "unknown %s directive '\\x%02X' in '%s'", what, c, f)
}

// intSize is the byte size and signedness of an integer directive
func intSize(d directive) (size int, signed bool, ok bool) {
	switch d.t {
	case 'c', 'C':
		return 1, d.t == 'c', true
	case 's', 'S':
		return 2, d.t == 's', true
	case 'i', 'I':
		return 4, d.t == 'i', true
	case 'l', 'L':
		if d.native {
			return 8, d.t == 'l', true
		}
		return 4, d.t == 'l', true
	case 'q', 'Q', 'j', 'J':
		return 8, d.t == 'q' || d.t == 'j', true
	case 'n', 'v':
		return 2, false, true
	case 'N', 'V':
		return 4, false, true
	}
	return 0, false, false
}

func bigEndian(d directive) bool {
	switch d.t {
	case 'n', 'N':
		return true
	case 'v', 'V':
		return false
	}
	return d.big
}

func (p packer) packIt(fmtStr string, ary, buffer oruby.Value) oruby.Value {
	mrb := p.mrb
	var res []byte
	if !buffer.IsNil() {
		res = append(res, buffer.Bytes()...)
	}
	base := len(res)
	n := oruby.RArrayLen(ary)
	idx := 0
	next := func() oruby.Value {
		if idx >= n {
			packFail("ArgumentError", "too few arguments")
		}
		v := mrb.AryEntry(ary, idx)
		idx++
		return v
	}
	encInfo := 1 // 0 BINARY, 1 US-ASCII, 2 UTF-8
	var assoc []oruby.Value
	for _, d := range parseFormat(fmtStr, "pack") {
		switch d.t {
		case 'U':
			if encInfo == 1 {
				encInfo = 2
			}
		case 'm', 'M', 'u':
		default:
			encInfo = 0
		}
		count := d.count
		if d.star {
			switch {
			case strings.IndexByte("@Xxu", d.t) >= 0:
				count = 0
			case strings.IndexByte("PMm", d.t) >= 0:
				count = 1
			default:
				count = n - idx
			}
		}
		switch d.t {
		case 'a', 'A', 'Z':
			s := p.toStr(next(), true)
			plen := len(s)
			if d.star {
				count = plen
				if d.t == 'Z' {
					count++
				}
			}
			if plen >= count {
				res = append(res, s[:count]...)
			} else {
				res = append(res, s...)
				pad := byte(0)
				if d.t == 'A' {
					pad = ' '
				}
				for i := plen; i < count; i++ {
					res = append(res, pad)
				}
			}
		case 'b', 'B':
			s := p.toStr(next(), true)
			if d.star {
				count = len(s)
			}
			nbytes := (count + 7) / 8
			out := make([]byte, nbytes)
			for i := 0; i < count && i < len(s); i++ {
				if s[i]&1 != 0 {
					if d.t == 'b' {
						out[i/8] |= 1 << (i % 8)
					} else {
						out[i/8] |= 0x80 >> (i % 8)
					}
				}
			}
			res = append(res, out...)
		case 'h', 'H':
			s := p.toStr(next(), true)
			if d.star {
				count = len(s)
			}
			out := make([]byte, (count+1)/2)
			for i := 0; i < count && i < len(s); i++ {
				c := s[i]
				var v byte
				switch {
				case c >= 'a' && c <= 'z':
					v = (c - 'a' + 10) & 15
				case c >= 'A' && c <= 'Z':
					v = (c - 'A' + 10) & 15
				default:
					v = c & 15
				}
				if (d.t == 'h') == (i%2 == 0) {
					out[i/2] |= v
				} else {
					out[i/2] |= v << 4
				}
			}
			res = append(res, out...)
		case 'c', 'C', 's', 'S', 'i', 'I', 'l', 'L', 'q', 'Q', 'j', 'J', 'n', 'N', 'v', 'V':
			size, _, _ := intSize(d)
			be := bigEndian(d)
			for k := 0; k < count; k++ {
				iv, bv := p.toInt(next())
				var u uint64
				if bv != nil {
					m := new(big.Int).Lsh(big.NewInt(1), 64)
					m.Sub(m, big.NewInt(1))
					u = new(big.Int).And(bv, m).Uint64()
				} else {
					u = uint64(iv)
				}
				var buf [8]byte
				if be {
					binary.BigEndian.PutUint64(buf[:], u)
					res = append(res, buf[8-size:]...)
				} else {
					binary.LittleEndian.PutUint64(buf[:], u)
					res = append(res, buf[:size]...)
				}
			}
		case 'U':
			for k := 0; k < count; k++ {
				iv, bv := p.toInt(next())
				if bv != nil || iv < 0 || iv > 0x7fffffff {
					packFail("RangeError", "pack(U): value out of range")
				}
				res = appendUTF8(res, iv)
			}
		case 'w':
			for k := 0; k < count; k++ {
				b := p.toBig(next())
				if b.Sign() < 0 {
					packFail("ArgumentError", "can't compress negative numbers")
				}
				res = append(res, berEncode(b)...)
			}
		case 'R', 'r':
			for k := 0; k < count; k++ {
				b := p.toBig(next())
				if d.t == 'R' && b.Sign() < 0 {
					packFail("ArgumentError", "can't encode negative numbers in ULEB128")
				}
				res = append(res, lebEncode(b, d.t == 'r')...)
			}
		case 'd', 'D', 'E', 'G', 'f', 'F', 'e', 'g':
			for k := 0; k < count; k++ {
				f := p.toFloat(next())
				if math.IsNaN(f) {
					f = math.Float64frombits(0x7ff8000000000000) // canonical quiet NaN (Go's math.NaN has a payload)
				}
				var buf [8]byte
				switch d.t {
				case 'd', 'D', 'E':
					binary.LittleEndian.PutUint64(buf[:], math.Float64bits(f))
					res = append(res, buf[:8]...)
				case 'G':
					binary.BigEndian.PutUint64(buf[:], math.Float64bits(f))
					res = append(res, buf[:8]...)
				case 'f', 'F', 'e':
					binary.LittleEndian.PutUint32(buf[:], math.Float32bits(float32(f)))
					res = append(res, buf[:4]...)
				case 'g':
					binary.BigEndian.PutUint32(buf[:], math.Float32bits(float32(f)))
					res = append(res, buf[:4]...)
				}
			}
		case 'm', 'u':
			s := p.toStr(next(), false)
			if d.t == 'm' && d.hasCount && !d.star && count == 0 {
				res = append(res, encodeLines(s, 'm', len(s)+1, false)...)
				break
			}
			lineLen := count
			if lineLen <= 2 {
				lineLen = 45
			} else if lineLen > 63 && d.t == 'u' {
				lineLen = 63
			} else {
				lineLen = lineLen / 3 * 3
			}
			res = append(res, encodeLines(s, d.t, lineLen, true)...)
		case 'M':
			v := next()
			var s []byte
			if v.IsString() {
				s = v.Bytes()
			} else {
				s = p.kernel("__pack_to_s", v).Bytes()
			}
			if count <= 1 {
				count = 72
			}
			res = append(res, qpEncode(s, count)...)
		case 'x':
			if d.align > 0 {
				for (len(res)-base)%d.align != 0 {
					res = append(res, 0)
				}
				break
			}
			for k := 0; k < count; k++ {
				res = append(res, 0)
			}
		case 'X':
			if count > len(res) {
				packFail("ArgumentError", "X outside of string")
			}
			res = res[:len(res)-count]
		case '@':
			if d.align > 0 {
				for len(res)%d.align != 0 {
					res = append(res, 0)
				}
				break
			}
			if count > len(res) {
				for len(res) < count {
					res = append(res, 0)
				}
			} else {
				res = res[:count]
			}
		case 'p', 'P':
			v := next()
			var addr uint64
			if !v.IsNil() {
				if !v.IsString() {
					v = p.kernel("__implicit", v, mrb.StringClass().Value())
				}
				addr = uint64(v.Pointer())
				assoc = append(assoc, v)
			}
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], addr)
			res = append(res, buf[:]...)
		case '%':
			packFail("ArgumentError", "%% is not supported")
		default:
			unknownDirective("pack", d.t, fmtStr)
		}
	}

	var out oruby.Value
	if !buffer.IsNil() {
		mrb.StrResize(buffer, 0)
		mrb.StrCat(buffer, string(res))
		out = buffer
	} else {
		out = mrb.StrNew(string(res))
		switch encInfo {
		case 1:
			p.call(out, "force_encoding", "US-ASCII")
		case 2:
			p.call(out, "force_encoding", "UTF-8")
		default:
			p.call(out, "force_encoding", "BINARY")
		}
	}
	if len(assoc) > 0 {
		list := mrb.AryNew()
		for _, a := range assoc {
			mrb.AryPush(list, a)
		}
		p.kernel("__pack_associate", out, list.Value())
	}
	return out
}

func appendUTF8(res []byte, c int64) []byte {
	switch {
	case c < 0x80:
		return append(res, byte(c))
	case c < 0x800:
		return append(res, byte(0xc0|c>>6), byte(0x80|c&0x3f))
	case c < 0x10000:
		return append(res, byte(0xe0|c>>12), byte(0x80|(c>>6)&0x3f), byte(0x80|c&0x3f))
	case c < 0x200000:
		return append(res, byte(0xf0|c>>18), byte(0x80|(c>>12)&0x3f), byte(0x80|(c>>6)&0x3f), byte(0x80|c&0x3f))
	case c < 0x4000000:
		return append(res, byte(0xf8|c>>24), byte(0x80|(c>>18)&0x3f), byte(0x80|(c>>12)&0x3f), byte(0x80|(c>>6)&0x3f), byte(0x80|c&0x3f))
	}
	return append(res, byte(0xfc|c>>30), byte(0x80|(c>>24)&0x3f), byte(0x80|(c>>18)&0x3f), byte(0x80|(c>>12)&0x3f), byte(0x80|(c>>6)&0x3f), byte(0x80|c&0x3f))
}

func berEncode(b *big.Int) []byte {
	x := new(big.Int).Set(b)
	var groups []byte
	mask := big.NewInt(0x7f)
	for {
		groups = append(groups, byte(new(big.Int).And(x, mask).Int64()))
		x.Rsh(x, 7)
		if x.Sign() == 0 {
			break
		}
	}
	out := make([]byte, len(groups))
	for i := range groups {
		v := groups[len(groups)-1-i]
		if i < len(groups)-1 {
			v |= 0x80
		}
		out[i] = v
	}
	return out
}

func lebEncode(b *big.Int, signed bool) []byte {
	x := new(big.Int).Set(b)
	var out []byte
	mask := big.NewInt(0x7f)
	for {
		v := byte(new(big.Int).And(x, mask).Int64())
		x.Rsh(x, 7) // arithmetic for negatives
		done := false
		if signed {
			done = (x.Sign() == 0 && v&0x40 == 0) || (x.Cmp(big.NewInt(-1)) == 0 && v&0x40 != 0)
		} else {
			done = x.Sign() == 0
		}
		if !done {
			v |= 0x80
		}
		out = append(out, v)
		if done {
			return out
		}
	}
}

const b64Table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
const uuTable = "`!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_"

// encodeLines is MRI's encodes: base64 ('m') or uuencode ('u'), lineLen
// input bytes per line, each line ended by "\n" when tailLF
func encodeLines(s []byte, t byte, lineLen int, tailLF bool) []byte {
	var out []byte
	table, padding := b64Table, byte('=')
	if t == 'u' {
		table, padding = uuTable, '`'
	}
	for len(s) > 0 {
		todo := len(s)
		if todo > lineLen {
			todo = lineLen
		}
		chunk := s[:todo]
		s = s[todo:]
		if t == 'u' {
			out = append(out, byte(todo)+' ')
		}
		for len(chunk) >= 3 {
			a, b, c := chunk[0], chunk[1], chunk[2]
			out = append(out, table[a>>2], table[(a<<4&060)|(b>>4&017)], table[(b<<2&074)|(c>>6&03)], table[c&077])
			chunk = chunk[3:]
		}
		switch len(chunk) {
		case 2:
			a, b := chunk[0], chunk[1]
			out = append(out, table[a>>2], table[(a<<4&060)|(b>>4&017)], table[b<<2&074], padding)
		case 1:
			a := chunk[0]
			out = append(out, table[a>>2], table[a<<4&060], padding, padding)
		}
		if tailLF {
			out = append(out, '\n')
		}
	}
	return out
}

func qpEncode(s []byte, lineLen int) []byte {
	const hex = "0123456789ABCDEF"
	var out []byte
	n := 0
	prev := -1
	for _, c := range s {
		switch {
		case c > 126 || (c < 32 && c != '\n' && c != '\t') || c == '=':
			out = append(out, '=', hex[c>>4], hex[c&15])
			n += 3
			prev = -1
		case c == '\n':
			if prev == ' ' || prev == '\t' {
				out = append(out, '=', c)
			}
			out = append(out, c)
			n = 0
			prev = int(c)
		default:
			out = append(out, c)
			n++
			prev = int(c)
		}
		if n > lineLen {
			out = append(out, '=', '\n')
			n = 0
			prev = '\n'
		}
	}
	if n > 0 {
		out = append(out, '=', '\n')
	}
	return out
}

func (p packer) unpackIt(self oruby.Value, fmtStr string, offset int, first bool) oruby.Value {
	mrb := p.mrb
	src := self.Bytes()
	if offset > len(src) {
		packFail("ArgumentError", "offset outside of string")
	}
	pos := offset
	res := mrb.AryNew()
	pushed := 0
	push := func(v oruby.Value) {
		mrb.AryPush(res, v)
		pushed++
	}
	binStr := func(b []byte) oruby.Value {
		v := mrb.StrNew(string(b))
		p.call(v, "force_encoding", "BINARY")
		return v
	}
	asciiStr := func(b []byte) oruby.Value {
		v := mrb.StrNew(string(b))
		p.call(v, "force_encoding", "US-ASCII")
		return v
	}
	for _, d := range parseFormat(fmtStr, "unpack") {
		if first && pushed > 0 {
			break
		}
		rest := len(src) - pos
		count := d.count
		switch d.t {
		case 'a', 'A', 'Z':
			if d.star || count > rest {
				count = rest
			}
			b := src[pos : pos+count]
			switch d.t {
			case 'Z':
				if i := indexByte(b, 0); i >= 0 {
					b = b[:i]
					if d.star {
						count = i + 1
					}
				}
			case 'A':
				for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == 0) {
					b = b[:len(b)-1]
				}
			}
			push(binStr(b))
			pos += count
		case 'b', 'B':
			if d.star || count > rest*8 {
				count = rest * 8
			}
			out := make([]byte, count)
			for i := 0; i < count; i++ {
				c := src[pos+i/8]
				var bit byte
				if d.t == 'b' {
					bit = c >> (i % 8) & 1
				} else {
					bit = c >> (7 - i%8) & 1
				}
				out[i] = '0' + bit
			}
			push(asciiStr(out))
			pos += (count + 7) / 8
		case 'h', 'H':
			if d.star || count > rest*2 {
				count = rest * 2
			}
			const hex = "0123456789abcdef"
			out := make([]byte, count)
			for i := 0; i < count; i++ {
				c := src[pos+i/2]
				var v byte
				if (d.t == 'h') == (i%2 == 0) {
					v = c & 15
				} else {
					v = c >> 4
				}
				out[i] = hex[v]
			}
			push(asciiStr(out))
			pos += (count + 1) / 2
		case 'c', 'C', 's', 'S', 'i', 'I', 'l', 'L', 'q', 'Q', 'j', 'J', 'n', 'N', 'v', 'V':
			size, signed, _ := intSize(d)
			be := bigEndian(d)
			avail := rest / size
			if d.star {
				count = avail
			}
			for k := 0; k < count; k++ {
				if first && pushed > 0 {
					break
				}
				if k >= avail {
					push(mrb.NilValue())
					continue
				}
				var buf [8]byte
				var u uint64
				if be {
					copy(buf[8-size:], src[pos:pos+size])
					u = binary.BigEndian.Uint64(buf[:])
				} else {
					copy(buf[:size], src[pos:pos+size])
					u = binary.LittleEndian.Uint64(buf[:])
				}
				pos += size
				if signed {
					shift := uint(64 - 8*size)
					push(oruby.Int64(int64(u<<shift) >> shift))
				} else if u > math.MaxInt64 {
					push(p.intValue(new(big.Int).SetUint64(u)))
				} else {
					push(oruby.Int64(int64(u)))
				}
			}
		case 'U':
			if d.star || count > rest {
				count = rest
			}
			for k := 0; k < count && pos < len(src); k++ {
				c, n := decodeUTF8(src[pos:])
				if n == 0 {
					packFail("ArgumentError", "malformed UTF-8 character")
				}
				push(oruby.Int64(c))
				pos += n
				if first {
					break
				}
			}
		case 'w', 'R', 'r':
			if d.star {
				count = rest
			}
			start := pos
			for count > 0 && pos < len(src) {
				if src[pos]&0x80 != 0 {
					pos++
					continue
				}
				pos++
				chunk := src[start:pos]
				start = pos
				var b *big.Int
				if d.t == 'w' {
					b = new(big.Int)
					for _, c := range chunk {
						b.Lsh(b, 7)
						b.Or(b, big.NewInt(int64(c&0x7f)))
					}
				} else {
					b = lebDecode(chunk, d.t == 'r')
				}
				push(p.intValue(b))
				count--
				if first {
					break
				}
			}
			if d.t != 'w' {
				pos = start
				if !d.star {
					for ; count > 0 && !(first && pushed > 0); count-- {
						push(mrb.NilValue())
					}
				}
			}
		case 'd', 'D', 'E', 'G', 'f', 'F', 'e', 'g':
			size := 8
			if strings.IndexByte("fFeg", d.t) >= 0 {
				size = 4
			}
			avail := rest / size
			if d.star {
				count = avail
			}
			for k := 0; k < count; k++ {
				if first && pushed > 0 {
					break
				}
				if k >= avail {
					push(mrb.NilValue())
					continue
				}
				b := src[pos : pos+size]
				pos += size
				var f float64
				switch d.t {
				case 'd', 'D', 'E':
					f = math.Float64frombits(binary.LittleEndian.Uint64(b))
				case 'G':
					f = math.Float64frombits(binary.BigEndian.Uint64(b))
				case 'f', 'F', 'e':
					f = float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
				case 'g':
					f = float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
				}
				push(mrb.FloatValue(f))
			}
		case 'm':
			strict := d.hasCount && !d.star && count == 0
			out, ok := base64Decode(src[pos:], strict)
			if !ok {
				packFail("ArgumentError", "invalid base64")
			}
			push(binStr(out))
			pos = len(src)
		case 'M':
			out, n := qpDecode(src[pos:])
			push(binStr(out))
			pos += n
		case 'u':
			out, n := uuDecode(src[pos:])
			push(binStr(out))
			pos += n
		case 'x':
			if d.align > 0 {
				for (pos-offset)%d.align != 0 {
					pos++
				}
				if pos > len(src) {
					packFail("ArgumentError", "x outside of string")
				}
				break
			}
			if d.star {
				count = rest
			}
			if count > rest {
				packFail("ArgumentError", "x outside of string")
			}
			pos += count
		case 'X':
			if d.star {
				count = rest
			}
			if count > pos {
				packFail("ArgumentError", "X outside of string")
			}
			pos -= count
		case '@':
			if d.align > 0 {
				for pos%d.align != 0 {
					pos++
				}
				if pos > len(src) {
					packFail("ArgumentError", "@ outside of string")
				}
				break
			}
			if d.star {
				break
			}
			if !d.hasCount {
				count = 0
			}
			if count+offset > len(src) {
				packFail("ArgumentError", "@ outside of string")
			}
			pos = offset + count
		case '^':
			push(oruby.Int(pos))
		case 'p', 'P':
			if d.t == 'P' && d.star {
				count = 1
			}
			n := 1
			if d.t == 'p' {
				n = count
				if d.star {
					n = rest / 8
				}
			}
			for k := 0; k < n; k++ {
				if rest-8*k < 8 {
					if d.t == 'p' && !d.star {
						push(mrb.NilValue())
					}
					continue
				}
				addr := binary.LittleEndian.Uint64(src[pos : pos+8])
				pos += 8
				if addr == 0 {
					push(mrb.NilValue())
					continue
				}
				target, ok := p.pointerTarget(self, addr)
				if !ok {
					packFail("ArgumentError", "no associated pointer")
				}
				b := target.Bytes()
				if d.t == 'P' {
					if count < len(b) {
						b = b[:count] // reads as far as the string's NUL terminator
					}
					push(binStr(b))
				} else {
					if i := indexByte(b, 0); i >= 0 {
						b = b[:i]
					}
					push(binStr(b))
				}
			}
		case '%':
			packFail("ArgumentError", "%% is not supported")
		default:
			unknownDirective("unpack", d.t, fmtStr)
		}
	}
	return res.Value()
}

func (p packer) pointerTarget(self oruby.Value, addr uint64) (oruby.Value, bool) {
	list := p.kernel("__pack_associated", self)
	if !list.IsArray() {
		return list, false
	}
	for i := 0; i < oruby.RArrayLen(list); i++ {
		v := p.mrb.AryEntry(list, i)
		if uint64(v.Pointer()) == addr {
			return v, true
		}
	}
	return list, false
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// decodeUTF8 accepts MRI's up-to-6-byte forms; n is 0 when malformed
func decodeUTF8(b []byte) (int64, int) {
	c := b[0]
	if c < 0x80 {
		return int64(c), 1
	}
	var n int
	var v int64
	switch {
	case c&0xe0 == 0xc0:
		n, v = 2, int64(c&0x1f)
	case c&0xf0 == 0xe0:
		n, v = 3, int64(c&0x0f)
	case c&0xf8 == 0xf0:
		n, v = 4, int64(c&0x07)
	case c&0xfc == 0xf8:
		n, v = 5, int64(c&0x03)
	case c&0xfe == 0xfc:
		n, v = 6, int64(c&0x01)
	default:
		return 0, 0
	}
	if len(b) < n {
		return 0, 0
	}
	for i := 1; i < n; i++ {
		if b[i]&0xc0 != 0x80 {
			return 0, 0
		}
		v = v<<6 | int64(b[i]&0x3f)
	}
	// redundant (overlong) forms are malformed
	minimum := []int64{0, 0, 0x80, 0x800, 0x10000, 0x200000, 0x4000000}
	if v < minimum[n] {
		return 0, 0
	}
	_ = utf8.RuneError
	return v, n
}

func lebDecode(chunk []byte, signed bool) *big.Int {
	b := new(big.Int)
	shift := uint(0)
	for _, c := range chunk {
		b.Or(b, new(big.Int).Lsh(big.NewInt(int64(c&0x7f)), shift))
		shift += 7
	}
	if signed && len(chunk) > 0 && chunk[len(chunk)-1]&0x40 != 0 {
		b.Sub(b, new(big.Int).Lsh(big.NewInt(1), shift))
	}
	return b
}

func b64Value(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 26
	case c >= '0' && c <= '9':
		return int(c-'0') + 52
	case c == '+':
		return 62
	case c == '/':
		return 63
	}
	return -1
}

// base64Decode is MRI's 'm' (lenient: skips other bytes, stops at '=') and
// 'm0' (strict RFC 4648)
func base64Decode(s []byte, strict bool) ([]byte, bool) {
	var out []byte
	if strict {
		if len(s)%4 != 0 {
			return nil, false
		}
		for i := 0; i < len(s); i += 4 {
			q := s[i : i+4]
			a, b := b64Value(q[0]), b64Value(q[1])
			if a < 0 || b < 0 {
				return nil, false
			}
			if q[2] == '=' {
				if q[3] != '=' || i+4 != len(s) || b&15 != 0 {
					return nil, false
				}
				out = append(out, byte(a<<2|b>>4))
				break
			}
			c := b64Value(q[2])
			if c < 0 {
				return nil, false
			}
			if q[3] == '=' {
				if i+4 != len(s) || c&3 != 0 {
					return nil, false
				}
				out = append(out, byte(a<<2|b>>4), byte(b<<4|c>>2))
				break
			}
			e := b64Value(q[3])
			if e < 0 {
				return nil, false
			}
			out = append(out, byte(a<<2|b>>4), byte(b<<4|c>>2), byte(c<<6|e))
		}
		return out, true
	}
	var vals [4]int
	n := 0
	for _, c := range s {
		if c == '=' {
			break
		}
		v := b64Value(c)
		if v < 0 {
			continue
		}
		vals[n] = v
		n++
		if n == 4 {
			out = append(out, byte(vals[0]<<2|vals[1]>>4), byte(vals[1]<<4|vals[2]>>2), byte(vals[2]<<6|vals[3]))
			n = 0
		}
	}
	switch n {
	case 2:
		out = append(out, byte(vals[0]<<2|vals[1]>>4))
	case 3:
		out = append(out, byte(vals[0]<<2|vals[1]>>4), byte(vals[1]<<4|vals[2]>>2))
	}
	return out, true
}

func hexNum(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func qpDecode(s []byte) ([]byte, int) {
	var out []byte
	i, ss := 0, 0
	for i < len(s) {
		if s[i] == '=' {
			i++
			if i == len(s) {
				break
			}
			if i+1 < len(s) && s[i] == '\r' && s[i+1] == '\n' {
				i++
			}
			if s[i] != '\n' {
				c1 := hexNum(s[i])
				if c1 < 0 {
					break
				}
				i++
				if i == len(s) {
					break
				}
				c2 := hexNum(s[i])
				if c2 < 0 {
					break
				}
				out = append(out, byte(c1<<4|c2))
			}
		} else {
			out = append(out, s[i])
		}
		i++
		ss = i
	}
	out = append(out, s[ss:]...)
	return out, len(s)
}

func uuDecode(s []byte) ([]byte, int) {
	var out []byte
	i := 0
	limit := len(s) * 3 / 4
	for i < len(s) && s[i] > ' ' && s[i] < 'a' {
		n := int((s[i] - ' ') & 077)
		i++
		if len(out)+n > limit {
			n = limit - len(out)
		}
		for n > 0 {
			var q [4]byte
			for k := 0; k < 4; k++ {
				if i < len(s) && s[i] >= ' ' && s[i] < 'a' {
					q[k] = (s[i] - ' ') & 077
					i++
				}
			}
			hunk := []byte{q[0]<<2 | q[1]>>4, q[1]<<4 | q[2]>>2, q[2]<<6 | q[3]}
			m := n
			if m > 3 {
				m = 3
			}
			out = append(out, hunk[:m]...)
			n -= m
		}
		if i < len(s) && s[i] != '\r' && s[i] != '\n' {
			i++
		}
		if i < len(s) && s[i] == '\r' {
			i++
		}
		if i < len(s) && s[i] == '\n' {
			i++
		}
	}
	return out, i
}

func initPack(mrb *oruby.MrbState) {
	mrb.DefineMethod(mrb.ArrayClass(), "__oruby_pack", func(mrb *oruby.MrbState, self oruby.Value) (ret oruby.MrbValue) {
		defer packRecover(mrb, &ret)
		args := mrb.GetArgs()
		return packer{mrb}.packIt(args.Item(0).String(), self, args.Item(1))
	}, oruby.ArgsReq(2))
	mrb.DefineMethod(mrb.StringClass(), "__oruby_unpack", func(mrb *oruby.MrbState, self oruby.Value) (ret oruby.MrbValue) {
		defer packRecover(mrb, &ret)
		args := mrb.GetArgs()
		return packer{mrb}.unpackIt(self, args.Item(0).String(), args.Item(1).Int(), args.Item(2).Bool())
	}, oruby.ArgsReq(3))
}
