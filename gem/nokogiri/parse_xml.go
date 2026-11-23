package nokogiri

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Pure-Go XML 1.0 parser supporting namespaces, CDATA, comments, processing
// instructions, DOCTYPE, entity decoding, line numbers, NOBLANKS, and
// STRICT / RECOVER error handling.
type xmlParser struct {
	src      string
	pos      int
	line     int
	doc      *domNode
	strict   bool
	noblanks bool
	nocdata  bool
}

func parseXMLDocument(src, url, encoding string, strict, noblanks, nocdata bool) *domNode {
	doc := newDocument(false, false)
	doc.url = url
	if encoding != "" {
		doc.encoding = encoding
	}
	if strings.TrimSpace(src) == "" {
		return doc
	}
	p := &xmlParser{
		src:      src,
		line:     1,
		doc:      doc,
		strict:   strict,
		noblanks: noblanks,
		nocdata:  nocdata,
	}
	p.parseInto(doc)
	return doc
}

func parseXMLFragment(doc *domNode, src string, noblanks, nocdata bool) *domNode {
	if doc == nil {
		doc = newDocument(false, false)
	}
	frag := newFragment(doc)
	if src == "" {
		return frag
	}
	p := &xmlParser{
		src:      src,
		line:     1,
		doc:      doc,
		strict:   false,
		noblanks: noblanks,
		nocdata:  nocdata,
	}
	p.parseInto(frag)
	frag.errors = append(frag.errors, doc.errors...)
	return frag
}

func (p *xmlParser) addError(msg string) {
	p.doc.errors = append(p.doc.errors, domError{
		message: msg,
		line:    p.line,
		level:   3,
	})
}

func (p *xmlParser) advance(n int) {
	for i := 0; i < n && p.pos < len(p.src); i++ {
		if p.src[p.pos] == '\n' {
			p.line++
		}
		p.pos++
	}
}

func (p *xmlParser) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.advance(1)
		default:
			return
		}
	}
}

func isXMLNameStart(r rune) bool {
	return r == ':' || r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r >= 0x80
}

func isXMLNameChar(r rune) bool {
	return isXMLNameStart(r) || r == '-' || r == '.' || (r >= '0' && r <= '9')
}

func (p *xmlParser) readName() string {
	start := p.pos
	for p.pos < len(p.src) {
		r, w := utf8.DecodeRuneInString(p.src[p.pos:])
		if p.pos == start {
			if !isXMLNameStart(r) {
				break
			}
		} else {
			if !isXMLNameChar(r) {
				break
			}
		}
		p.pos += w
	}
	return p.src[start:p.pos]
}

func decodeXMLEntities(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '&' {
			semi := strings.IndexByte(s[i:], ';')
			if semi > 1 && semi <= 12 {
				ent := s[i+1 : i+semi]
				if rep, ok := resolveXMLEntity(ent); ok {
					sb.WriteString(rep)
					i += semi + 1
					continue
				}
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func resolveXMLEntity(ent string) (string, bool) {
	switch ent {
	case "amp":
		return "&", true
	case "lt":
		return "<", true
	case "gt":
		return ">", true
	case "quot":
		return `"`, true
	case "apos":
		return "'", true
	}
	if strings.HasPrefix(ent, "#x") || strings.HasPrefix(ent, "#X") {
		if n, err := strconv.ParseInt(ent[2:], 16, 32); err == nil && n >= 0 {
			return string(rune(n)), true
		}
	} else if strings.HasPrefix(ent, "#") {
		if n, err := strconv.ParseInt(ent[1:], 10, 32); err == nil && n >= 0 {
			return string(rune(n)), true
		}
	}
	// Fallback HTML named entity
	if u := html.UnescapeString("&" + ent + ";"); u != "&"+ent+";" {
		return u, true
	}
	return "", false
}

func (p *xmlParser) parseInto(root *domNode) {
	// Skip UTF-8 BOM if present
	if strings.HasPrefix(p.src, "\xef\xbb\xbf") {
		p.pos = 3
	}

	stack := []*domNode{root}
	cur := func() *domNode { return stack[len(stack)-1] }

	for p.pos < len(p.src) {
		lt := strings.IndexByte(p.src[p.pos:], '<')
		if lt < 0 {
			txt := decodeXMLEntities(p.src[p.pos:])
			p.advance(len(p.src) - p.pos)
			if txt != "" && (!p.noblanks || strings.TrimSpace(txt) != "") {
				if cur().kind != DocumentNode || strings.TrimSpace(txt) != "" {
					tn := newNode(p.doc, TextNode, "#text", txt)
					tn.line = p.line
					cur().appendChild(tn)
				}
			}
			break
		}
		if lt > 0 {
			lineStart := p.line
			raw := p.src[p.pos : p.pos+lt]
			p.advance(lt)
			txt := decodeXMLEntities(raw)
			if txt != "" && (!p.noblanks || strings.TrimSpace(txt) != "") {
				if cur().kind != DocumentNode || strings.TrimSpace(txt) != "" {
					tn := newNode(p.doc, TextNode, "#text", txt)
					tn.line = lineStart
					cur().appendChild(tn)
				}
			}
		}

		// We are at '<'
		rem := p.src[p.pos:]
		lineNo := p.line

		// Comment: <!-- ... -->
		if strings.HasPrefix(rem, "<!--") {
			p.advance(4)
			end := strings.Index(p.src[p.pos:], "-->")
			if end < 0 {
				p.addError("unterminated comment")
				data := p.src[p.pos:]
				p.advance(len(data))
				cn := newNode(p.doc, CommentNode, "#comment", data)
				cn.line = lineNo
				cur().appendChild(cn)
				break
			}
			data := p.src[p.pos : p.pos+end]
			p.advance(end + 3)
			cn := newNode(p.doc, CommentNode, "#comment", data)
			cn.line = lineNo
			cur().appendChild(cn)
			continue
		}

		// CDATA: <![CDATA[ ... ]]>
		if strings.HasPrefix(rem, "<![CDATA[") {
			p.advance(9)
			end := strings.Index(p.src[p.pos:], "]]>")
			if end < 0 {
				p.addError("unterminated CDATA section")
				data := p.src[p.pos:]
				p.advance(len(data))
				kind := CDATASectionNode
				name := "#cdata-section"
				if p.nocdata {
					kind = TextNode
					name = "#text"
				}
				cn := newNode(p.doc, kind, name, data)
				cn.line = lineNo
				cur().appendChild(cn)
				break
			}
			data := p.src[p.pos : p.pos+end]
			p.advance(end + 3)
			kind := CDATASectionNode
			name := "#cdata-section"
			if p.nocdata {
				kind = TextNode
				name = "#text"
			}
			cn := newNode(p.doc, kind, name, data)
			cn.line = lineNo
			cur().appendChild(cn)
			continue
		}

		// DOCTYPE: <!DOCTYPE ...>
		if strings.HasPrefix(strings.ToUpper(rem), "<!DOCTYPE") {
			p.advance(9)
			p.skipWS()
			dtName := p.readName()
			// Read until matching '>' (respecting internal subset '[...]')
			depth := 0
			inBracket := false
			start := p.pos
			for p.pos < len(p.src) {
				ch := p.src[p.pos]
				if ch == '[' {
					inBracket = true
					depth++
				} else if ch == ']' && inBracket {
					depth--
					if depth == 0 {
						inBracket = false
					}
				} else if ch == '>' && !inBracket {
					break
				}
				p.advance(1)
			}
			meta := strings.TrimSpace(p.src[start:p.pos])
			if p.pos < len(p.src) && p.src[p.pos] == '>' {
				p.advance(1)
			}
			dn := newNode(p.doc, DocumentTypeNode, dtName, meta)
			dn.line = lineNo
			parseDoctypeIDs(dn, meta)
			cur().appendChild(dn)
			continue
		}

		// Processing instruction or XML declaration: <?target ... ?>
		if strings.HasPrefix(rem, "<?") {
			p.advance(2)
			target := p.readName()
			p.skipWS()
			end := strings.Index(p.src[p.pos:], "?>")
			var data string
			if end < 0 {
				p.addError("unterminated processing instruction")
				data = strings.TrimSpace(p.src[p.pos:])
				p.advance(len(p.src) - p.pos)
			} else {
				data = strings.TrimSpace(p.src[p.pos : p.pos+end])
				p.advance(end + 2)
			}
			if strings.EqualFold(target, "xml") {
				parseXMLDecl(p.doc, data)
			} else {
				pi := newNode(p.doc, PINode, target, data)
				pi.line = lineNo
				cur().appendChild(pi)
			}
			continue
		}

		// Closing tag: </name>
		if strings.HasPrefix(rem, "</") {
			p.advance(2)
			p.skipWS()
			fullName := p.readName()
			p.skipWS()
			if p.pos < len(p.src) && p.src[p.pos] == '>' {
				p.advance(1)
			} else {
				p.addError(fmt.Sprintf("expected '>' closing tag </%s>", fullName))
			}
			prefix, local := splitQName(fullName)
			// Find matching open tag on stack
			matched := -1
			for i := len(stack) - 1; i >= 1; i-- {
				if stack[i].name == local && stack[i].prefix == prefix {
					matched = i
					break
				}
			}
			if matched >= 1 {
				if matched != len(stack)-1 {
					p.addError(fmt.Sprintf("Opening and ending tag mismatch: %s and %s", stack[len(stack)-1].name, local))
				}
				stack = stack[:matched]
			} else {
				p.addError(fmt.Sprintf("Unexpected closing tag </%s>", fullName))
			}
			continue
		}

		// Opening tag: <name attrs...> or <name attrs.../>
		p.advance(1) // consume '<'
		fullName := p.readName()
		if fullName == "" {
			p.addError("invalid '<' in XML content")
			continue
		}
		prefix, local := splitQName(fullName)
		elem := newNode(p.doc, ElementNode, local, "")
		elem.prefix = prefix
		elem.line = lineNo

		selfClosing := false
		for p.pos < len(p.src) {
			p.skipWS()
			if p.pos >= len(p.src) {
				p.addError(fmt.Sprintf("unterminated start tag <%s>", fullName))
				break
			}
			if strings.HasPrefix(p.src[p.pos:], "/>") {
				p.advance(2)
				selfClosing = true
				break
			}
			if p.src[p.pos] == '>' {
				p.advance(1)
				break
			}
			attrName := p.readName()
			if attrName == "" {
				p.addError(fmt.Sprintf("malformed attribute in <%s>", fullName))
				p.advance(1)
				continue
			}
			p.skipWS()
			attrVal := ""
			if p.pos < len(p.src) && p.src[p.pos] == '=' {
				p.advance(1)
				p.skipWS()
				if p.pos < len(p.src) && (p.src[p.pos] == '"' || p.src[p.pos] == '\'') {
					quote := p.src[p.pos]
					p.advance(1)
					vStart := p.pos
					for p.pos < len(p.src) && p.src[p.pos] != quote {
						p.advance(1)
					}
					attrVal = decodeXMLEntities(p.src[vStart:p.pos])
					if p.pos < len(p.src) && p.src[p.pos] == quote {
						p.advance(1)
					} else {
						p.addError(fmt.Sprintf("unterminated attribute value for %s", attrName))
					}
				} else {
					p.addError(fmt.Sprintf("expected quoted attribute value for %s", attrName))
					vStart := p.pos
					for p.pos < len(p.src) && p.src[p.pos] != ' ' && p.src[p.pos] != '\t' && p.src[p.pos] != '>' && p.src[p.pos] != '/' {
						p.advance(1)
					}
					attrVal = decodeXMLEntities(p.src[vStart:p.pos])
				}
			}
			if attrName == "xmlns" {
				elem.addNamespaceDef("", attrVal)
			} else if strings.HasPrefix(attrName, "xmlns:") {
				elem.addNamespaceDef(attrName[6:], attrVal)
			} else {
				ap, al := splitQName(attrName)
				elem.attrs = append(elem.attrs, &domAttr{
					name:   al,
					prefix: ap,
					value:  attrVal,
					owner:  elem,
				})
			}
		}

		cur().appendChild(elem)
		// Resolve element & attribute namespaces against newly-attached parent chain
		elem.nsURI = elem.lookupNS(elem.prefix)
		for _, a := range elem.attrs {
			if a.prefix != "" {
				a.nsURI = elem.lookupNS(a.prefix)
			}
		}

		if !selfClosing {
			stack = append(stack, elem)
		}
	}

	if len(stack) > 1 {
		for i := len(stack) - 1; i >= 1; i-- {
			p.addError(fmt.Sprintf("Premature end of data in tag %s line %d", stack[i].name, stack[i].line))
		}
	}
}

func splitQName(full string) (prefix, local string) {
	if idx := strings.IndexByte(full, ':'); idx > 0 {
		return full[:idx], full[idx+1:]
	}
	return "", full
}

func parseXMLDecl(doc *domNode, data string) {
	if v := extractQuotedAttr(data, "version"); v != "" {
		doc.version = v
	}
	if e := extractQuotedAttr(data, "encoding"); e != "" {
		doc.encoding = e
	}
}

func extractQuotedAttr(s, name string) string {
	idx := strings.Index(s, name)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(name):]
	eq := strings.IndexByte(rest, '=')
	if eq < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[eq+1:])
	if len(rest) < 2 {
		return ""
	}
	q := rest[0]
	if q != '"' && q != '\'' {
		return ""
	}
	end := strings.IndexByte(rest[1:], q)
	if end < 0 {
		return ""
	}
	return rest[1 : 1+end]
}

func parseDoctypeIDs(dn *domNode, meta string) {
	upper := strings.ToUpper(meta)
	if idx := strings.Index(upper, "PUBLIC"); idx >= 0 {
		rest := meta[idx+6:]
		q1, rest := readNextQuoted(rest)
		q2, _ := readNextQuoted(rest)
		dn.pubID = q1
		dn.sysID = q2
	} else if idx := strings.Index(upper, "SYSTEM"); idx >= 0 {
		rest := meta[idx+6:]
		q1, _ := readNextQuoted(rest)
		dn.sysID = q1
	}
}

func readNextQuoted(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\'' {
			q := s[i]
			end := strings.IndexByte(s[i+1:], q)
			if end >= 0 {
				return s[i+1 : i+1+end], s[i+1+end+1:]
			}
		}
	}
	return "", ""
}
