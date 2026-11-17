package transcode

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// result is what reading one character from the source came back with.
type result int

const (
	rOK         result = iota
	rInvalid           // the bytes are no character of the encoding
	rUndefined         // a character, but one Unicode has no mapping for
	rIncomplete        // the character's bytes go on past the end of input
)

// decoder reads one character at the start of b (len(b) > 0).
//
// For rOK it appends the character's UTF-8 to u (nothing for a byte sequence
// that only switches state, an ISO-2022-JP escape, say) and returns the byte
// count. For rUndefined n is the character's byte count. For rInvalid n is the
// length of the invalid bytes and readagain that of the bytes after them that
// told so, which MRI reads again. For rIncomplete n is len(b).
type decoder interface {
	decode(u, b []byte) (out []byte, n int, res result, readagain int)
	reset()
}

// encoder appends r in its encoding to dst, or answers false when the
// encoding has no such character; finish appends what ends the output (the
// return to ASCII of a stateful encoding).
type encoder interface {
	encode(dst []byte, r rune) ([]byte, bool)
	finish(dst []byte) []byte
	reset()
}

type codec struct {
	dec func() decoder
	enc func() encoder
}

// codecs maps MRI encoding names (upper case) to converters to and from
// UTF-8. Encodings without one (Emacs-Mule, UTF-7, ...) raise
// Encoding::ConverterNotFoundError, as in MRI.
var codecs = map[string]codec{}

func register(c codec, names ...string) {
	for _, n := range names {
		codecs[strings.ToUpper(n)] = c
	}
}

func lookup(name string) (codec, bool) {
	c, ok := codecs[strings.ToUpper(name)]
	return c, ok
}

func init() {
	register(codec{func() decoder { return utf8Dec{} }, func() encoder { return utf8Enc{} }}, "UTF-8")
	register(codec{func() decoder { return asciiDec{binary: false} }, func() encoder { return asciiEnc{} }}, "US-ASCII")
	register(codec{func() decoder { return asciiDec{binary: true} }, func() encoder { return asciiEnc{} }}, "ASCII-8BIT")
	register(codec{func() decoder { return &macDec{} }, func() encoder { return macEnc{} }}, "UTF8-MAC")
	for _, u := range []struct {
		name      string
		size      int
		bigEndian bool
		bom       bool
	}{
		{"UTF-16BE", 2, true, false}, {"UTF-16LE", 2, false, false},
		{"UTF-32BE", 4, true, false}, {"UTF-32LE", 4, false, false},
		{"UTF-16", 2, true, true}, {"UTF-32", 4, true, true},
	} {
		u := u
		register(codec{
			func() decoder { return &utfDec{size: u.size, be: u.bigEndian, bom: u.bom, needBOM: u.bom} },
			func() encoder { return &utfEnc{size: u.size, be: u.bigEndian, bom: u.bom, needBOM: u.bom} },
		}, u.name)
	}

	sb := func(cm *charmap.Charmap, c1 bool, names ...string) {
		register(codec{
			func() decoder { return sbDec{cm, c1} },
			func() encoder { return sbEnc{cm, c1} },
		}, names...)
	}
	sb(charmap.ISO8859_1, false, "ISO-8859-1")
	sb(charmap.ISO8859_2, false, "ISO-8859-2")
	sb(charmap.ISO8859_3, false, "ISO-8859-3")
	sb(charmap.ISO8859_4, false, "ISO-8859-4")
	sb(charmap.ISO8859_5, false, "ISO-8859-5")
	sb(charmap.ISO8859_6, false, "ISO-8859-6")
	sb(charmap.ISO8859_7, false, "ISO-8859-7")
	sb(charmap.ISO8859_8, false, "ISO-8859-8")
	sb(charmap.ISO8859_9, false, "ISO-8859-9")
	sb(charmap.ISO8859_10, false, "ISO-8859-10")
	sb(charmap.Windows874, true, "ISO-8859-11", "TIS-620") // Windows-874 less its C1 additions
	sb(charmap.ISO8859_13, false, "ISO-8859-13")
	sb(charmap.ISO8859_14, false, "ISO-8859-14")
	sb(charmap.ISO8859_15, false, "ISO-8859-15")
	sb(charmap.ISO8859_16, false, "ISO-8859-16")
	sb(charmap.KOI8R, false, "KOI8-R")
	sb(charmap.KOI8U, false, "KOI8-U")
	sb(charmap.Windows874, false, "Windows-874")
	sb(charmap.Windows1250, false, "Windows-1250")
	sb(charmap.Windows1251, false, "Windows-1251")
	sb(charmap.Windows1252, false, "Windows-1252")
	sb(charmap.Windows1253, false, "Windows-1253")
	sb(charmap.Windows1254, false, "Windows-1254")
	sb(charmap.Windows1255, false, "Windows-1255")
	sb(charmap.Windows1256, false, "Windows-1256")
	sb(charmap.Windows1257, false, "Windows-1257")
	sb(charmap.Windows1258, false, "Windows-1258")
	sb(charmap.CodePage037, false, "IBM037")
	sb(charmap.CodePage437, false, "IBM437")
	sb(charmap.CodePage850, false, "CP850")
	sb(charmap.CodePage852, false, "IBM852", "CP852")
	sb(charmap.CodePage855, false, "IBM855", "CP855")
	sb(charmap.CodePage860, false, "IBM860")
	sb(charmap.CodePage862, false, "IBM862")
	sb(charmap.CodePage863, false, "IBM863")
	sb(charmap.CodePage865, false, "IBM865")
	sb(charmap.CodePage866, false, "IBM866")
	sb(charmap.Macintosh, false, "macRoman")
	sb(charmap.MacintoshCyrillic, false, "macCyrillic", "macUkraine")

	mbv := func(e encoding.Encoding, length func([]byte) (int, result, int), valid func([]byte) bool, names ...string) {
		register(codec{
			func() decoder { return &mbDec{t: e.NewDecoder(), length: length} },
			func() encoder { return &mbEnc{t: e.NewEncoder(), valid: valid} },
		}, names...)
	}
	mb := func(e encoding.Encoding, length func([]byte) (int, result, int), names ...string) {
		mbv(e, length, nil, names...)
	}
	mb(japanese.EUCJP, eucjpLen, "EUC-JP", "eucJP-ms", "CP51932", "EUC-JIS-2004")
	mb(japanese.ShiftJIS, sjisLen, "Shift_JIS", "Windows-31J", "SJIS-DoCoMo", "SJIS-KDDI", "SJIS-SoftBank", "MacJapanese")
	mb(korean.EUCKR, euckrLen, "EUC-KR")
	mb(korean.EUCKR, cp949Len, "CP949")
	mb(simplifiedchinese.GBK, gbkLen, "GBK")
	mb(simplifiedchinese.GBK, gb2312Len, "GB2312")
	mb(simplifiedchinese.GB18030, gb18030Len, "GB18030")
	// x/text's Big5 carries the HKSCS additions; MRI's Big5 and CP950 do not
	std := func(b []byte) bool { return b[0] >= 0xA1 && b[0] <= 0xF9 }
	mbv(traditionalchinese.Big5, big5Len, std, "Big5", "CP950")
	mb(traditionalchinese.Big5, big5Len, "Big5-HKSCS", "Big5-UAO", "CP951")
	register(codec{
		func() decoder { return &jisDec{euc: &mbDec{t: japanese.EUCJP.NewDecoder(), length: eucjpLen}} },
		func() encoder { return &jisEnc{euc: &mbEnc{t: japanese.EUCJP.NewEncoder()}} },
	}, "ISO-2022-JP", "CP50220", "CP50221")
}

// unicodeDst reports whether name is a Unicode encoding, whose replacement
// character is U+FFFD rather than "?".
func unicodeDst(name string) bool {
	switch strings.ToUpper(name) {
	case "UTF-8", "UTF8-MAC", "UTF-16BE", "UTF-16LE", "UTF-32BE", "UTF-32LE", "UTF-16", "UTF-32":
		return true
	}
	return false
}

// --- UTF-8 ---------------------------------------------------------------

type utf8Dec struct{}

func (utf8Dec) reset() {}

// decode reads UTF-8 the way MRI's transcoder does: an invalid lead byte is
// one invalid byte; a lead followed by a byte that cannot continue it is
// invalid up to there, and that byte is read again.
func (utf8Dec) decode(u, b []byte) ([]byte, int, result, int) {
	c := b[0]
	if c < 0x80 {
		return append(u, c), 1, rOK, 0
	}
	need := 0
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c >= 0xE1 && c <= 0xEC, c == 0xEE, c == 0xEF:
		need = 2
	case c == 0xED:
		need, hi = 2, 0x9F
	case c == 0xF0:
		need, lo = 3, 0x90
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	case c == 0xF4:
		need, hi = 3, 0x8F
	default:
		return u, 1, rInvalid, 0
	}
	for i := 1; i <= need; i++ {
		if i >= len(b) {
			return u, len(b), rIncomplete, 0
		}
		l, h := byte(0x80), byte(0xBF)
		if i == 1 {
			l, h = lo, hi
		}
		if b[i] < l || b[i] > h {
			return u, i, rInvalid, 1
		}
	}
	return append(u, b[:need+1]...), need + 1, rOK, 0
}

type utf8Enc struct{}

func (utf8Enc) encode(dst []byte, r rune) ([]byte, bool) { return utf8.AppendRune(dst, r), true }
func (utf8Enc) finish(dst []byte) []byte                 { return dst }
func (utf8Enc) reset()                                   {}

// --- US-ASCII and ASCII-8BIT --------------------------------------------------

// asciiDec reads US-ASCII, where a byte over 0x7F is invalid, or ASCII-8BIT,
// where it is a character Unicode does not define.
type asciiDec struct{ binary bool }

func (asciiDec) reset() {}

func (d asciiDec) decode(u, b []byte) ([]byte, int, result, int) {
	if b[0] < 0x80 {
		return append(u, b[0]), 1, rOK, 0
	}
	if d.binary {
		return u, 1, rUndefined, 0
	}
	return u, 1, rInvalid, 0
}

type asciiEnc struct{}

func (asciiEnc) encode(dst []byte, r rune) ([]byte, bool) {
	if r < 0x80 {
		return append(dst, byte(r)), true
	}
	return dst, false
}
func (asciiEnc) finish(dst []byte) []byte { return dst }
func (asciiEnc) reset()                   {}

// --- UTF-16 and UTF-32 ---------------------------------------------------------

// utfDec reads UTF-16/32 of either byte order; with bom (the dummy UTF-16 and
// UTF-32) the byte order comes from the byte order mark the input starts with.
type utfDec struct {
	size    int
	be, bom bool
	needBOM bool
}

func (d *utfDec) reset() { d.needBOM = d.bom }

func (d *utfDec) unit(b []byte) uint32 {
	if d.size == 2 {
		if d.be {
			return uint32(b[0])<<8 | uint32(b[1])
		}
		return uint32(b[1])<<8 | uint32(b[0])
	}
	if d.be {
		return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	}
	return uint32(b[3])<<24 | uint32(b[2])<<16 | uint32(b[1])<<8 | uint32(b[0])
}

func (d *utfDec) decode(u, b []byte) ([]byte, int, result, int) {
	if len(b) < d.size {
		return u, len(b), rIncomplete, 0
	}
	if d.needBOM {
		d.be = true
		switch {
		case d.unit(b) == 0xFEFF:
		case d.size == 2 && b[0] == 0xFF && b[1] == 0xFE, d.size == 4 && b[0] == 0xFF && b[1] == 0xFE && b[2] == 0 && b[3] == 0:
			d.be = false
		default:
			return u, d.size, rInvalid, 0
		}
		d.needBOM = false
		return u, d.size, rOK, 0
	}
	c := d.unit(b)
	if d.size == 4 {
		if c > 0x10FFFF || (c >= 0xD800 && c <= 0xDFFF) {
			return u, 4, rInvalid, 0
		}
		return utf8.AppendRune(u, rune(c)), 4, rOK, 0
	}
	switch {
	case c >= 0xD800 && c <= 0xDBFF:
		if len(b) < 4 {
			return u, len(b), rIncomplete, 0
		}
		c2 := d.unit(b[2:])
		if c2 < 0xDC00 || c2 > 0xDFFF {
			return u, 2, rInvalid, 2
		}
		return utf8.AppendRune(u, rune((c-0xD800)<<10|(c2-0xDC00))+0x10000), 4, rOK, 0
	case c >= 0xDC00 && c <= 0xDFFF:
		return u, 2, rInvalid, 0
	}
	return utf8.AppendRune(u, rune(c)), 2, rOK, 0
}

type utfEnc struct {
	size    int
	be, bom bool
	needBOM bool
}

func (e *utfEnc) reset() { e.needBOM = e.bom }

func (e *utfEnc) put(dst []byte, c uint32) []byte {
	if e.size == 2 {
		if e.be {
			return append(dst, byte(c>>8), byte(c))
		}
		return append(dst, byte(c), byte(c>>8))
	}
	if e.be {
		return append(dst, byte(c>>24), byte(c>>16), byte(c>>8), byte(c))
	}
	return append(dst, byte(c), byte(c>>8), byte(c>>16), byte(c>>24))
}

func (e *utfEnc) encode(dst []byte, r rune) ([]byte, bool) {
	if e.needBOM {
		dst = e.put(dst, 0xFEFF)
		e.needBOM = false
	}
	if e.size == 2 && r >= 0x10000 {
		r -= 0x10000
		dst = e.put(dst, uint32(0xD800+(r>>10)))
		return e.put(dst, uint32(0xDC00+(r&0x3FF))), true
	}
	return e.put(dst, uint32(r)), true
}

func (e *utfEnc) finish(dst []byte) []byte { return dst }

// --- UTF8-MAC ----------------------------------------------------------------

// UTF8-MAC is UTF-8 decomposed as macOS (HFS+) stores file names; converting
// to UTF-8 composes it again.
type macDec struct{}

func (*macDec) reset() {}

func (*macDec) decode(u, b []byte) ([]byte, int, result, int) {
	start := len(u)
	u, n, res, ra := utf8Dec{}.decode(u, b)
	if res != rOK {
		return u, n, res, ra
	}
	// take the combining characters that follow along, then compose
	for n < len(b) {
		r, size := utf8.DecodeRune(b[n:])
		if r == utf8.RuneError || norm.NFC.PropertiesString(string(r)).CCC() == 0 && !composesWith(u[start:], r) {
			break
		}
		u = append(u, b[n:n+size]...)
		n += size
	}
	composed := norm.NFC.Bytes(u[start:])
	return append(u[:start], composed...), n, rOK, 0
}

func composesWith(base []byte, r rune) bool {
	s := string(base) + string(r)
	return len(norm.NFC.String(s)) < len(s)
}

type macEnc struct{}

func (macEnc) encode(dst []byte, r rune) ([]byte, bool) {
	// HFS+ leaves these ranges composed
	if (r >= 0x2000 && r <= 0x2FFF) || (r >= 0xF900 && r <= 0xFAFF) || (r >= 0x2F800 && r <= 0x2FAFF) {
		return utf8.AppendRune(dst, r), true
	}
	return append(dst, norm.NFD.String(string(r))...), true
}
func (macEnc) finish(dst []byte) []byte { return dst }
func (macEnc) reset()                   {}

// --- single-byte character sets -----------------------------------------------

// sbDec reads a single-byte charset; a byte the charset leaves undefined is
// an undefined character. c1 maps 0x80-0x9F to the C1 controls (ISO-8859-11
// and TIS-620 from the Windows-874 table).
type sbDec struct {
	cm *charmap.Charmap
	c1 bool
}

func (sbDec) reset() {}

func (d sbDec) decode(u, b []byte) ([]byte, int, result, int) {
	c := b[0]
	if d.c1 && c >= 0x80 && c <= 0x9F {
		return utf8.AppendRune(u, rune(c)), 1, rOK, 0
	}
	r := d.cm.DecodeByte(c)
	if r == utf8.RuneError {
		return u, 1, rUndefined, 0
	}
	return utf8.AppendRune(u, r), 1, rOK, 0
}

type sbEnc struct {
	cm *charmap.Charmap
	c1 bool
}

func (e sbEnc) encode(dst []byte, r rune) ([]byte, bool) {
	if e.c1 && r >= 0x80 && r <= 0x9F {
		return append(dst, byte(r)), true
	}
	if e.c1 && r >= 0x80 && r <= 0xFF {
		if b, ok := e.cm.EncodeRune(r); ok && b >= 0xA0 {
			return append(dst, b), true
		}
		return dst, false
	}
	b, ok := e.cm.EncodeRune(r)
	if !ok || (e.c1 && b >= 0x80 && b <= 0x9F) {
		return dst, false
	}
	return append(dst, b), true
}
func (sbEnc) finish(dst []byte) []byte { return dst }
func (sbEnc) reset()                   {}

// --- multibyte character sets ---------------------------------------------------

// mbDec finds a character's bytes with length (the encoding's lead and trail
// byte ranges, which decide what is invalid), then maps them with x/text; a
// well formed sequence x/text has no character for is undefined.
type mbDec struct {
	t      transform.Transformer
	length func([]byte) (int, result, int)
	buf    [16]byte
}

func (d *mbDec) reset() {}

func (d *mbDec) decode(u, b []byte) ([]byte, int, result, int) {
	n, res, ra := d.length(b)
	if res != rOK {
		return u, n, res, ra
	}
	if n == 1 && b[0] < 0x80 {
		return append(u, b[0]), 1, rOK, 0
	}
	d.t.Reset()
	nd, _, err := d.t.Transform(d.buf[:], b[:n], true)
	if err != nil || nd == 0 || strings.ContainsRune(string(d.buf[:nd]), utf8.RuneError) {
		return u, n, rUndefined, 0
	}
	return append(u, d.buf[:nd]...), n, rOK, 0
}

type mbEnc struct {
	t     transform.Transformer
	valid func([]byte) bool // the characters of the table the encoding has
	in    [4]byte
	buf   [16]byte
}

func (e *mbEnc) encode(dst []byte, r rune) ([]byte, bool) {
	if r < 0x80 {
		return append(dst, byte(r)), true
	}
	n := utf8.EncodeRune(e.in[:], r)
	e.t.Reset()
	nd, _, err := e.t.Transform(e.buf[:], e.in[:n], true)
	if err != nil || nd == 0 || (e.valid != nil && !e.valid(e.buf[:nd])) {
		return dst, false
	}
	return append(dst, e.buf[:nd]...), true
}
func (e *mbEnc) finish(dst []byte) []byte { return dst }
func (e *mbEnc) reset()                   {}

type byteRange func(byte) bool

func in(lo, hi byte) byteRange { return func(c byte) bool { return c >= lo && c <= hi } }

func either(rs ...byteRange) byteRange {
	return func(c byte) bool {
		for _, r := range rs {
			if r(c) {
				return true
			}
		}
		return false
	}
}

// trail checks the bytes after a lead byte against ranges, one per byte.
func trail(b []byte, ranges ...byteRange) (int, result, int) {
	for i, r := range ranges {
		pos := i + 1
		if pos >= len(b) {
			return len(b), rIncomplete, 0
		}
		if !r(b[pos]) {
			return pos, rInvalid, 1
		}
	}
	return len(ranges) + 1, rOK, 0
}

func eucjpLen(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return 1, rOK, 0
	case c == 0x8E:
		return trail(b, in(0xA1, 0xDF))
	case c == 0x8F:
		return trail(b, in(0xA1, 0xFE), in(0xA1, 0xFE))
	case c >= 0xA1 && c <= 0xFE:
		return trail(b, in(0xA1, 0xFE))
	}
	return 1, rInvalid, 0
}

func sjisLen(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80, c >= 0xA1 && c <= 0xDF:
		return 1, rOK, 0
	case c >= 0x81 && c <= 0x9F, c >= 0xE0 && c <= 0xFC:
		return trail(b, either(in(0x40, 0x7E), in(0x80, 0xFC)))
	}
	return 1, rInvalid, 0
}

func euckrLen(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return 1, rOK, 0
	case c >= 0xA1 && c <= 0xFE:
		return trail(b, in(0xA1, 0xFE))
	}
	return 1, rInvalid, 0
}

func cp949Len(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return 1, rOK, 0
	case c >= 0x81 && c <= 0xFE:
		return trail(b, either(in(0x41, 0x5A), in(0x61, 0x7A), in(0x81, 0xFE)))
	}
	return 1, rInvalid, 0
}

func gbkLen(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return 1, rOK, 0
	case c >= 0x81 && c <= 0xFE:
		return trail(b, either(in(0x40, 0x7E), in(0x80, 0xFE)))
	}
	return 1, rInvalid, 0
}

func gb2312Len(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return 1, rOK, 0
	case c >= 0xA1 && c <= 0xF7:
		return trail(b, in(0xA1, 0xFE))
	}
	return 1, rInvalid, 0
}

func gb18030Len(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return 1, rOK, 0
	case c >= 0x81 && c <= 0xFE:
		if len(b) > 1 && b[1] >= 0x30 && b[1] <= 0x39 {
			return trail(b, in(0x30, 0x39), in(0x81, 0xFE), in(0x30, 0x39))
		}
		return trail(b, either(in(0x40, 0x7E), in(0x80, 0xFE)))
	}
	return 1, rInvalid, 0
}

func big5Len(b []byte) (int, result, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return 1, rOK, 0
	case c >= 0x81 && c <= 0xFE:
		return trail(b, either(in(0x40, 0x7E), in(0xA1, 0xFE)))
	}
	return 1, rInvalid, 0
}

// --- ISO-2022-JP -------------------------------------------------------------------

// ISO-2022-JP switches between ASCII (ESC ( B), JIS X 0201 Roman (ESC ( J)
// and JIS X 0208 (ESC $ @, ESC $ B), whose two 7-bit bytes are EUC-JP's
// with the high bits cleared.
type jisDec struct {
	euc   *mbDec
	kanji bool
}

func (d *jisDec) reset() { d.kanji = false }

func (d *jisDec) decode(u, b []byte) ([]byte, int, result, int) {
	c := b[0]
	if c == 0x1B {
		if len(b) < 3 {
			return u, len(b), rIncomplete, 0
		}
		switch string(b[1:3]) {
		case "(B", "(J":
			d.kanji = false
		case "$@", "$B":
			d.kanji = true
		default:
			return u, 1, rInvalid, 0
		}
		return u, 3, rOK, 0
	}
	if d.kanji && c >= 0x21 && c <= 0x7E {
		if len(b) < 2 {
			return u, len(b), rIncomplete, 0
		}
		if b[1] < 0x21 || b[1] > 0x7E {
			return u, 1, rInvalid, 1
		}
		out, _, res, _ := d.euc.decode(u, []byte{c | 0x80, b[1] | 0x80})
		if res != rOK {
			return u, 2, rUndefined, 0
		}
		return out, 2, rOK, 0
	}
	if c < 0x80 {
		return append(u, c), 1, rOK, 0
	}
	return u, 1, rInvalid, 0
}

type jisEnc struct {
	euc   *mbEnc
	kanji bool
	tmp   []byte
}

func (e *jisEnc) reset() { e.kanji = false }

func (e *jisEnc) encode(dst []byte, r rune) ([]byte, bool) {
	if r < 0x80 {
		if e.kanji {
			dst = append(dst, 0x1B, '(', 'B')
			e.kanji = false
		}
		return append(dst, byte(r)), true
	}
	e.tmp, _ = e.euc.encode(e.tmp[:0], r)
	if len(e.tmp) != 2 || e.tmp[0] < 0xA1 || e.tmp[1] < 0xA1 {
		return dst, false
	}
	if !e.kanji {
		dst = append(dst, 0x1B, '$', 'B')
		e.kanji = true
	}
	return append(dst, e.tmp[0]&0x7F, e.tmp[1]&0x7F), true
}

func (e *jisEnc) finish(dst []byte) []byte {
	if e.kanji {
		e.kanji = false
		return append(dst, 0x1B, '(', 'B')
	}
	return dst
}
