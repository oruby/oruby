package regexp

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"

	"github.com/oruby/oruby"
)

type regexpText struct {
	text   string
	source []int
	coded  []int
}

func legacyRegexpEncoding(name string) bool {
	switch name {
	case "ASCII-8BIT", "Windows-31J", "Shift_JIS", "EUC-JP":
		return true
	}
	return false
}

func encodedWidth(s string, name string) int {
	width := 1
	if len(s) > 1 {
		b := s[0]
		switch name {
		case "Windows-31J", "Shift_JIS":
			trail := s[1]
			if (b >= 0x81 && b <= 0x9f || b >= 0xe0 && b <= 0xfc) &&
				(trail >= 0x40 && trail <= 0x7e || trail >= 0x80 && trail <= 0xfc) {
				width = 2
			}
		case "EUC-JP":
			if b >= 0xa1 && b <= 0xfe || b == 0x8e {
				width = 2
			} else if b == 0x8f && len(s) > 2 {
				width = 3
			}
		}
	}
	return width
}

func newRegexpText(s, enc string) regexpText {
	if !legacyRegexpEncoding(enc) || isASCII(s) {
		return regexpText{text: s}
	}
	var codec encoding.Encoding
	switch enc {
	case "Windows-31J", "Shift_JIS":
		codec = japanese.ShiftJIS
	case "EUC-JP":
		codec = japanese.EUCJP
	}
	var normalized strings.Builder
	normalized.Grow(len(s))
	var decode *encoding.Decoder
	if codec != nil {
		decode = codec.NewDecoder()
	}
	var src, dst []int
	var out [utf8.UTFMax]byte
	var raw [3]byte
	for start := 0; start < len(s); {
		src = append(src, start)
		dst = append(dst, normalized.Len())
		width := encodedWidth(s[start:], enc)
		if decode == nil || s[start] < utf8.RuneSelf {
			normalized.WriteRune(rune(s[start]))
		} else {
			copy(raw[:], s[start:start+width])
			n, _, _ := decode.Transform(out[:], raw[:width], true)
			if n == 0 {
				normalized.WriteRune(utf8.RuneError)
			} else {
				normalized.Write(out[:n])
			}
		}
		start += width
	}
	return regexpText{normalized.String(), append(src, len(s)), append(dst, normalized.Len())}
}

func (t regexpText) engineOffset(pos int) int {
	if t.source == nil {
		return pos
	}
	return t.coded[sort.SearchInts(t.source, pos)]
}

func (t regexpText) boundary(s string, pos int) bool {
	if t.source != nil {
		i := sort.SearchInts(t.source, pos)
		return i < len(t.source) && t.source[i] == pos
	}
	return pos == len(s) || utf8.RuneStart(s[pos])
}

func (t regexpText) previous(s string, pos int) int {
	if t.source != nil {
		return t.source[sort.SearchInts(t.source, pos)-1]
	}
	_, width := utf8.DecodeLastRuneInString(s[:pos])
	return pos - width
}

func (t regexpText) sourceMatch(m []int) []int {
	if t.source != nil {
		for i, pos := range m {
			if pos >= 0 {
				m[i] = t.source[sort.SearchInts(t.coded, pos)]
			}
		}
	}
	return m
}

func regexpPatternText(src, enc string) string {
	if !legacyRegexpEncoding(enc) {
		return src
	}
	// Escaped bytes that form a legacy character are interpreted together.
	// Leave ASCII-only escape runs and escaped backslashes to the translator.
	var result strings.Builder
	start := 0
	for i := 0; i+1 < len(src); {
		if src[i] != '\\' {
			i += encodedWidth(src[i:], enc)
			continue
		}
		j := i
		var raw []byte
		for j+3 < len(src) && src[j] == '\\' && src[j+1] == 'x' {
			b, err := strconv.ParseUint(src[j+2:j+4], 16, 8)
			if err != nil {
				break
			}
			raw = append(raw, byte(b))
			j += 4
		}
		if j == i {
			i += 2
			continue
		}
		if !isASCII(string(raw)) {
			result.WriteString(newRegexpText(src[start:i], enc).text)
			for _, ch := range newRegexpText(string(raw), enc).text {
				result.WriteString(`\u{`)
				result.WriteString(strconv.FormatInt(int64(ch), 16))
				result.WriteByte('}')
			}
			start = j
		}
		i = j
	}
	if start == 0 {
		return newRegexpText(src, enc).text
	}
	result.WriteString(newRegexpText(src[start:], enc).text)
	return result.String()
}

func regexpSubjectEncoding(mrb *oruby.MrbState, v oruby.Value) string {
	if v.IsString() && mrb.StrCoderange(v) == oruby.StrBytes {
		name := encName(mrb, strEnc(mrb, v))
		if name != "UTF-8" {
			return name
		}
		return "ASCII-8BIT"
	}
	return "UTF-8"
}

func regexpByteIndex(s string, pos int, enc string) (int, bool) {
	if !legacyRegexpEncoding(enc) {
		return byteIndex(s, pos)
	}
	if pos < 0 {
		pos += regexpCharIndex(s, len(s), enc)
	}
	for i := 0; pos >= 0 && i <= len(s); pos-- {
		if pos == 0 {
			return i, true
		}
		if i == len(s) {
			break
		}
		i += encodedWidth(s[i:], enc)
	}
	return 0, false
}

func regexpCharIndex(s string, pos int, enc string) int {
	if !legacyRegexpEncoding(enc) {
		return charIndex(s, pos)
	}
	n := 0
	for i := 0; i < pos; {
		i += encodedWidth(s[i:], enc)
		n++
	}
	return n
}
