package transcode

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Converter flags, MRI's ECONV_* values (Encoding::Converter constants).
const (
	InvalidMask          = 0x0000000f
	InvalidReplace       = 0x00000002
	UndefMask            = 0x000000f0
	UndefReplace         = 0x00000020
	UndefHexCharref      = 0x00000030
	DecoratorMask        = 0x0001ff00
	NewlineDecoratorMask = 0x00007f00
	UniversalNewline     = 0x00000100
	CRLFNewline          = 0x00001000
	CRNewline            = 0x00002000
	LFNewline            = 0x00004000
	XMLText              = 0x00004000 << 1
	XMLAttrContent       = 0x00004000 << 2
	XMLAttrQuote         = 0x00100000
	PartialInput         = 0x00020000
	AfterOutput          = 0x00040000
)

// Status is what a conversion stopped at (primitive_convert's result).
type Status string

const (
	InvalidByteSequence   Status = "invalid_byte_sequence"
	IncompleteInput       Status = "incomplete_input"
	UndefinedConversion   Status = "undefined_conversion"
	AfterOutputStatus     Status = "after_output"
	DestinationBufferFull Status = "destination_buffer_full"
	SourceBufferEmpty     Status = "source_buffer_empty"
	Finished              Status = "finished"
)

// ErrInfo is primitive_errinfo: the step of the conversion path an error
// happened in, and its bytes.
type ErrInfo struct {
	Status    Status
	Source    string
	Dest      string
	ErrBytes  []byte
	ReadAgain []byte
}

// Converter converts a stream from one encoding to another the way MRI's
// rb_econv_t does: through UTF-8 unless one side is UTF-8, keeping bytes of
// a character split over calls and output a full destination did not take.
type Converter struct {
	Src, Dst    string // "" and "" for a decorator-only converter
	Flags       int
	dec         decoder
	enc         encoder
	twoStep     bool
	replacement []byte // in Dst
	replEnc     string // the encoding Replacement reports
	pending     []byte // source bytes held back: a split character, or read-again bytes
	out         []byte // converted, not yet delivered
	pendingCR   bool
	started     bool
	ended       bool
	Last        ErrInfo
	errinfo     ErrInfo
	u           []byte
}

// ErrNotFound is returned by New when there is no converter between the
// encodings (Encoding::ConverterNotFoundError).
type ErrNotFound struct{ Src, Dst string }

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("code converter not found (%s to %s)", e.Src, e.Dst)
}

// Path is the conversion path from src to dst, as MRI's convpath lists it
// (without decorators), or nil when there is none.
func Path(src, dst string) [][2]string {
	if src == "" && dst == "" {
		return [][2]string{}
	}
	if strings.EqualFold(src, dst) {
		return nil
	}
	if _, ok := lookup(src); !ok {
		return nil
	}
	if _, ok := lookup(dst); !ok {
		return nil
	}
	if strings.EqualFold(src, "UTF-8") || strings.EqualFold(dst, "UTF-8") {
		return [][2]string{{src, dst}}
	}
	return [][2]string{{src, "UTF-8"}, {"UTF-8", dst}}
}

// Decorators names the decorators flags add, in MRI's convpath spelling.
func Decorators(flags int) []string {
	var d []string
	if flags&UniversalNewline != 0 {
		d = append(d, "universal_newline")
	}
	if flags&XMLText != 0 {
		d = append(d, "xml_text_escape")
	}
	if flags&XMLAttrContent != 0 {
		d = append(d, "xml_attr_content_escape")
	}
	if flags&XMLAttrQuote != 0 {
		d = append(d, "xml_attr_quote")
	}
	if flags&CRLFNewline != 0 {
		d = append(d, "crlf_newline")
	}
	if flags&CRNewline != 0 {
		d = append(d, "cr_newline")
	}
	if flags&LFNewline != 0 {
		d = append(d, "lf_newline")
	}
	return d
}

// New opens a converter from src to dst; src and dst both "" make one that
// only applies the decorators flags ask for.
func New(src, dst string, flags int) (*Converter, error) {
	c := &Converter{Src: src, Dst: dst, Flags: flags, Last: ErrInfo{Status: SourceBufferEmpty}}
	if src == "" && dst == "" {
		c.dec, c.enc = rawDec{}, rawEnc{}
	} else {
		if Path(src, dst) == nil {
			return nil, ErrNotFound{src, dst}
		}
		sc, _ := lookup(src)
		dc, _ := lookup(dst)
		c.dec, c.enc = sc.dec(), dc.enc()
		c.twoStep = !strings.EqualFold(src, "UTF-8") && !strings.EqualFold(dst, "UTF-8")
	}
	if unicodeDst(dst) {
		dc, _ := lookup(dst)
		c.replacement, _ = dc.enc().encode(nil, 0xFFFD)
		c.replEnc = "UTF-8"
	} else {
		c.replacement = []byte("?")
		c.replEnc = "US-ASCII"
	}
	return c, nil
}

// Replacement is the replacement string and the encoding it is in.
func (c *Converter) Replacement() ([]byte, string) {
	if c.replEnc == c.Dst || c.Dst == "" {
		return c.replacement, c.replEnc
	}
	if c.replEnc == "UTF-8" || c.replEnc == "US-ASCII" {
		r := "�"
		if c.replEnc == "US-ASCII" {
			r = "?"
		}
		return []byte(r), c.replEnc
	}
	return c.replacement, c.replEnc
}

// SetReplacement sets the replacement from str in encoding enc, converting
// it to the destination; it answers false when that is not possible.
func (c *Converter) SetReplacement(str []byte, enc string) bool {
	b, ok := ConvertString(str, enc, c.Dst)
	if !ok {
		return false
	}
	c.replacement = b
	c.replEnc = enc
	if c.Dst != "" {
		c.replEnc = c.Dst
	}
	return true
}

// ConvertString converts a whole string, answering false on any error.
func ConvertString(str []byte, src, dst string) ([]byte, bool) {
	if dst == "" || strings.EqualFold(src, dst) {
		return str, true
	}
	if isASCII(str) {
		if sc, ok := lookup(dst); ok {
			e := sc.enc()
			var out []byte
			for _, b := range str {
				out, _ = e.encode(out, rune(b))
			}
			return out, true
		}
	}
	k, err := New(src, dst, 0)
	if err != nil {
		return nil, false
	}
	st, _, out := k.Convert(str, true, -1, 0)
	return out, st == Finished
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// Insert puts str (in encoding enc) into the output, as insert_output does.
func (c *Converter) Insert(str []byte, enc string) bool {
	b, ok := ConvertString(str, enc, c.Dst)
	if !ok {
		return false
	}
	c.out = append(c.out, b...)
	return true
}

// Putback takes back up to n (all when n < 0) of the bytes held to be read
// again, last first.
func (c *Converter) Putback(n int) []byte {
	if n < 0 || n > len(c.pending) {
		n = len(c.pending)
	}
	b := append([]byte(nil), c.pending[len(c.pending)-n:]...)
	c.pending = c.pending[:len(c.pending)-n]
	return b
}

// Convert converts input (have false is a nil source buffer) and delivers up
// to limit bytes of output (any amount when limit < 0). It returns the status,
// how many input bytes it took and the output.
func (c *Converter) Convert(input []byte, have bool, limit int, flags int) (Status, int, []byte) {
	partial := flags&PartialInput != 0
	held := len(c.pending)
	buf := append(append([]byte(nil), c.pending...), input...)
	c.pending = nil
	st, pos := c.run(buf, partial)
	consumed := pos - held
	if consumed < 0 {
		c.pending = append(append([]byte(nil), buf[pos:held]...), c.pending...)
		consumed = 0
	}
	if !have {
		consumed = 0
	}
	out := c.out
	if limit >= 0 && len(out) > limit {
		c.out = append([]byte(nil), out[limit:]...)
		out = out[:limit]
		if st == Finished || st == SourceBufferEmpty {
			st = DestinationBufferFull
		}
	} else {
		c.out = nil
	}
	c.Last = ErrInfo{Status: st}
	if st == InvalidByteSequence || st == IncompleteInput || st == UndefinedConversion {
		c.Last = c.errinfo
	}
	return st, consumed, out
}

// the error run found, kept until Convert returns
func (c *Converter) setErr(st Status, src, dst string, errBytes, readagain []byte) {
	c.errinfo = ErrInfo{st, src, dst, append([]byte(nil), errBytes...), append([]byte(nil), readagain...)}
}

func (c *Converter) run(buf []byte, partial bool) (Status, int) {
	if !c.started {
		c.started = true
		if c.Flags&XMLAttrQuote != 0 {
			c.putASCII('"')
		}
	}
	errDst := c.Dst
	if c.twoStep {
		errDst = "UTF-8"
	}
	pos := 0
	for pos < len(buf) {
		u, n, res, ra := c.dec.decode(c.u[:0], buf[pos:])
		c.u = u
		switch res {
		case rOK:
			start := pos
			pos += n
			for len(u) > 0 {
				r, size := utf8.DecodeRune(u)
				if !c.put(r) {
					src := "UTF-8"
					eb := u[:size]
					if !c.twoStep && !strings.EqualFold(c.Src, "UTF-8") {
						src, eb = c.Src, buf[start:pos]
					}
					c.setErr(UndefinedConversion, src, c.Dst, eb, nil)
					return UndefinedConversion, pos
				}
				u = u[size:]
			}
		case rUndefined:
			pos += n
			if c.Flags&UndefMask == UndefReplace || c.Flags&UndefMask == UndefHexCharref {
				c.out = append(c.out, c.replacement...)
				continue
			}
			c.setErr(UndefinedConversion, c.Src, errDst, buf[pos-n:pos], nil)
			return UndefinedConversion, pos
		case rInvalid:
			if c.Flags&InvalidMask == InvalidReplace {
				c.out = append(c.out, c.replacement...)
				pos += n
				continue
			}
			c.setErr(InvalidByteSequence, c.Src, errDst, buf[pos:pos+n], buf[pos+n:pos+n+ra])
			c.pending = append(c.pending, buf[pos+n:pos+n+ra]...)
			return InvalidByteSequence, pos + n + ra
		case rIncomplete:
			if partial {
				c.pending = append(c.pending, buf[pos:]...)
				return SourceBufferEmpty, len(buf)
			}
			if c.Flags&InvalidMask == InvalidReplace {
				c.out = append(c.out, c.replacement...)
				pos = len(buf)
				continue
			}
			c.setErr(IncompleteInput, c.Src, errDst, buf[pos:], nil)
			return IncompleteInput, len(buf)
		}
	}
	if partial {
		return SourceBufferEmpty, pos
	}
	if !c.ended {
		c.ended = true
		c.out = c.enc.finish(c.out)
		if c.Flags&XMLAttrQuote != 0 {
			c.putASCII('"')
		}
	}
	return Finished, pos
}

func (c *Converter) putASCII(r rune) {
	c.out, _ = c.enc.encode(c.out, r)
}

// put runs r through the decorators and the encoder.
func (c *Converter) put(r rune) bool {
	f := c.Flags
	if f&(UniversalNewline|LFNewline) != 0 {
		if c.pendingCR && r == '\n' {
			c.pendingCR = false
			return true
		}
		c.pendingCR = r == '\r'
		if r == '\r' {
			r = '\n'
		}
	}
	switch {
	case r == '\n' && f&CRLFNewline != 0:
		c.putASCII('\r')
		c.putASCII('\n')
		return true
	case r == '\n' && f&CRNewline != 0:
		c.putASCII('\r')
		return true
	}
	if f&(XMLText|XMLAttrContent) != 0 {
		esc := ""
		switch r {
		case '&':
			esc = "&amp;"
		case '<':
			esc = "&lt;"
		case '>':
			esc = "&gt;"
		case '"':
			if f&XMLAttrContent != 0 {
				esc = "&quot;"
			}
		}
		if esc != "" {
			for _, e := range esc {
				c.putASCII(e)
			}
			return true
		}
	}
	out, ok := c.enc.encode(c.out, r)
	if ok {
		c.out = out
		return true
	}
	switch f & UndefMask {
	case UndefHexCharref:
		for _, e := range fmt.Sprintf("&#x%X;", r) {
			c.putASCII(e)
		}
		return true
	case UndefReplace:
		c.out = append(c.out, c.replacement...)
		return true
	}
	return false
}

// rawDec and rawEnc make the decorator-only converter: bytes pass as they
// are, ASCII characters through the decorators.
type rawDec struct{}

func (rawDec) reset() {}
func (rawDec) decode(u, b []byte) ([]byte, int, result, int) {
	return utf8.AppendRune(u, rawRune(b[0])), 1, rOK, 0
}

// bytes over 0x7F travel as private-use runes and come out as the byte
const rawBase = 0xF0000

func rawRune(b byte) rune {
	if b < 0x80 {
		return rune(b)
	}
	return rawBase + rune(b)
}

type rawEnc struct{}

func (rawEnc) encode(dst []byte, r rune) ([]byte, bool) {
	if r >= rawBase {
		return append(dst, byte(r-rawBase)), true
	}
	if r < 0x80 {
		return append(dst, byte(r)), true
	}
	return utf8.AppendRune(dst, r), true
}
func (rawEnc) finish(dst []byte) []byte { return dst }
func (rawEnc) reset()                   {}

// InvalidSpans lists the invalid byte sequences of str in encoding enc as
// start, length pairs (MRI's scrub units), or answers false when ORuby has
// no codec for enc.
func InvalidSpans(str []byte, enc string) ([][2]int, bool) {
	cd, ok := lookup(enc)
	if !ok {
		return nil, false
	}
	d := cd.dec()
	var spans [][2]int
	var u []byte
	for pos := 0; pos < len(str); {
		var n int
		var res result
		u, n, res, _ = d.decode(u[:0], str[pos:])
		if res == rInvalid || res == rIncomplete {
			spans = append(spans, [2]int{pos, n})
		}
		if n < 1 {
			n = 1
		}
		pos += n
	}
	return spans, true
}
