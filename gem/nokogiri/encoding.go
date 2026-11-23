package nokogiri

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
)

var (
	reXMLDeclEnc  = regexp.MustCompile(`(?i)<\?xml\b[^>]*\bencoding\s*=\s*["']([^"']+)["']`)
	reMetaCharset = regexp.MustCompile(`(?i)<meta\b[^>]*\bcharset\s*=\s*["']?\s*([A-Za-z0-9._-]+)`)
	reMetaHTTPCT  = regexp.MustCompile(`(?i)<meta\b[^>]*http-equiv\s*=\s*["']?content-type["']?[^>]*content\s*=\s*["'][^"']*charset\s*=\s*([A-Za-z0-9._-]+)`)
)

func lookupEncoding(name string) encoding.Encoding {
	n := strings.TrimSpace(name)
	if n == "" {
		return nil
	}
	low := strings.ToLower(n)
	switch low {
	case "utf-8", "utf8", "ascii", "us-ascii", "ascii-8bit", "binary":
		return nil
	case "utf-16le", "utf16le":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case "utf-16be", "utf16be":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	case "utf-16", "utf16":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)
	}
	if enc, err := htmlindex.Get(n); err == nil && enc != nil {
		return enc
	}
	if enc, err := ianaindex.IANA.Encoding(n); err == nil && enc != nil {
		return enc
	}
	if enc, err := ianaindex.MIB.Encoding(n); err == nil && enc != nil {
		return enc
	}
	return nil
}

// decodeToUTF8 sniffs BOM / XML declaration / HTML <meta charset> (or uses
// hintEnc), transcodes non-UTF-8 bytes to UTF-8, and returns the UTF-8 string
// together with the detected encoding label.
func decodeToUTF8(src string, hintEnc string, isHTML bool) (string, string) {
	raw := []byte(src)
	if len(raw) == 0 {
		return src, hintEnc
	}

	// 1. Check BOM first
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		enc := hintEnc
		if enc == "" {
			enc = "UTF-8"
		}
		return string(raw[3:]), enc
	}
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		dec := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
		if out, err := dec.Bytes(raw); err == nil {
			enc := hintEnc
			if enc == "" {
				enc = "UTF-16LE"
			}
			return string(out), enc
		}
	}
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		dec := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder()
		if out, err := dec.Bytes(raw); err == nil {
			enc := hintEnc
			if enc == "" {
				enc = "UTF-16BE"
			}
			return string(out), enc
		}
	}

	// 2. Determine declared encoding
	encName := strings.TrimSpace(hintEnc)
	if encName == "" {
		head := raw
		if len(head) > 4096 {
			head = head[:4096]
		}
		if !isHTML {
			if m := reXMLDeclEnc.FindSubmatch(head); len(m) >= 2 {
				encName = string(m[1])
			}
		} else {
			if m := reMetaCharset.FindSubmatch(head); len(m) >= 2 {
				encName = string(m[1])
			} else if m := reMetaHTTPCT.FindSubmatch(head); len(m) >= 2 {
				encName = string(m[1])
			}
		}
	}

	if encName == "" {
		return src, ""
	}
	enc := lookupEncoding(encName)
	if enc == nil {
		return src, encName
	}

	// Transcode raw bytes into UTF-8.
	out, err := enc.NewDecoder().Bytes(raw)
	if err != nil {
		return src, encName
	}
	// Strip UTF-8 BOM if decoder produced one
	out = bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})
	return string(out), encName
}
