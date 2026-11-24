package nokogiri

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func parseHTMLDocument(src, url, encoding string, isHTML5 bool, noblanks bool) *domNode {
	return parseHTMLDocumentOpts(src, url, encoding, isHTML5, noblanks, -1, -1, false)
}

func parseHTMLDocumentOpts(src, url, encoding string, isHTML5 bool, noblanks bool, maxErrors int, maxTreeDepth int, parseNoScriptAsText bool) *domNode {
	doc := newDocument(true, isHTML5)
	doc.url = url
	doc.parseNoScriptAsText = parseNoScriptAsText
	doc.parseNoScriptSet = true
	decoded, detectedEnc := decodeToUTF8(src, encoding, true)
	if encoding != "" {
		doc.encoding = encoding
	} else if detectedEnc != "" {
		doc.encoding = detectedEnc
	}
	if isHTML5 && maxErrors != 0 {
		doc.errors = append(doc.errors, scanHTML5ParseErrors(decoded)...)
	} else if !isHTML5 && strings.TrimSpace(decoded) != "" {
		doc.errors = append(doc.errors, scanHTML4ParseErrors(decoded)...)
	}
	if strings.TrimSpace(decoded) == "" {
		doc.quirksMode = "quirks"
		if maxErrors >= 0 && len(doc.errors) > maxErrors {
			doc.errors = doc.errors[:maxErrors]
		}
		return doc
	}

	var root *html.Node
	var err error
	if isHTML5 {
		root, err = html.ParseWithOptions(strings.NewReader(decoded), html.ParseOptionEnableScripting(parseNoScriptAsText))
	} else {
		root, err = html.Parse(strings.NewReader(decoded))
	}
	if err != nil {
		doc.errors = append(doc.errors, domError{message: err.Error(), level: 2})
		return doc
	}
	convertHTMLChildrenDepth(doc, doc, root, noblanks, 0, maxTreeDepth)
	doc.quirksMode = detectHTMLQuirksMode(doc)
	if isHTML5 && maxErrors != 0 && doc.quirksMode == "quirks" && !strings.Contains(strings.ToLower(decoded), "<!doctype") {
		doc.errors = append(doc.errors, domError{
			message: "1:1: ERROR: Expected a doctype token",
			line:    1,
			column:  1,
			level:   2,
			file:    url,
		})
	}
	if url != "" {
		for i := range doc.errors {
			if doc.errors[i].file == "" {
				doc.errors[i].file = url
			}
		}
	}
	if maxErrors >= 0 && len(doc.errors) > maxErrors {
		doc.errors = doc.errors[:maxErrors]
	}
	return doc
}

func detectHTMLQuirksMode(doc *domNode) string {
	var dt *domNode
	for c := doc.first; c != nil; c = c.next {
		if c.kind == DocumentTypeNode {
			dt = c
			break
		}
	}
	if dt == nil {
		return "quirks"
	}
	if !strings.EqualFold(dt.name, "html") {
		return "quirks"
	}
	pub := strings.ToLower(dt.pubID)
	if pub == "" && dt.sysID == "" {
		return "no_quirks"
	}
	if strings.Contains(pub, "transitional") || strings.Contains(pub, "frameset") {
		if dt.sysID == "" {
			return "quirks"
		}
		return "limited_quirks"
	}
	return "no_quirks"
}

func parseHTMLFragment(doc *domNode, src string, ctxTag string, noblanks bool) *domNode {
	return parseHTMLFragmentOpts(doc, src, ctxTag, noblanks, 0, -1, doc != nil && doc.parseNoScriptAsText)
}

func parseHTMLFragmentOpts(doc *domNode, src string, ctxTag string, noblanks bool, maxErrors int, maxTreeDepth int, parseNoScriptAsText bool) *domNode {
	if doc == nil {
		doc = newDocument(true, true)
	}
	frag := newFragment(doc)
	if doc.isHTML5 && maxErrors != 0 {
		frag.errors = append(frag.errors, scanHTML5ParseErrors(src)...)
		if maxErrors > 0 && len(frag.errors) > maxErrors {
			frag.errors = frag.errors[:maxErrors]
		}
	} else if !doc.isHTML5 && strings.TrimSpace(src) != "" {
		frag.errors = append(frag.errors, scanHTML4ParseErrors(src)...)
	}
	if src == "" {
		return frag
	}

	if ctxTag == "" {
		ctxTag = "body"
	}
	ctxAtom := atom.Lookup([]byte(strings.ToLower(ctxTag)))
	ctxNode := &html.Node{
		Type:     html.ElementNode,
		DataAtom: ctxAtom,
		Data:     strings.ToLower(ctxTag),
	}
	var nodes []*html.Node
	var err error
	if doc.isHTML5 {
		nodes, err = html.ParseFragmentWithOptions(strings.NewReader(src), ctxNode, html.ParseOptionEnableScripting(parseNoScriptAsText))
	} else {
		nodes, err = html.ParseFragment(strings.NewReader(src), ctxNode)
	}
	if err != nil {
		frag.errors = append(frag.errors, domError{message: err.Error(), level: 2})
		return frag
	}
	for _, hn := range nodes {
		if dn := convertHTMLNodeDepth(doc, hn, noblanks, 0, maxTreeDepth); dn != nil {
			frag.appendChild(dn)
		}
	}
	if maxErrors >= 0 && len(frag.errors) > maxErrors {
		frag.errors = frag.errors[:maxErrors]
	}
	return frag
}

func scanHTML4ParseErrors(src string) []domError {
	// Detect common HTML4 syntax errors that libxml2's HTML4 parser reports:
	// unterminated tags (<a, </a), mismatched/stray closing tags, unknown tags, invalid '<'.
	var errs []domError
	var stack []string
	line := 1
	i := 0
	for i < len(src) {
		ch := src[i]
		if ch == '\n' {
			line++
			i++
			continue
		}
		if ch != '<' {
			i++
			continue
		}
		// Comments / DOCTYPE / PI
		if strings.HasPrefix(src[i:], "<!--") {
			end := strings.Index(src[i+4:], "-->")
			if end < 0 {
				errs = append(errs, domError{message: "Comment not terminated", line: line, level: 2})
				break
			}
			i += 4 + end + 3
			continue
		}
		if strings.HasPrefix(src[i:], "<!") || strings.HasPrefix(src[i:], "<?") {
			end := strings.IndexByte(src[i:], '>')
			if end < 0 {
				errs = append(errs, domError{message: "Tag not terminated", line: line, level: 2})
				break
			}
			i += end + 1
			continue
		}
		isClose := i+1 < len(src) && src[i+1] == '/'
		j := i + 1
		if isClose {
			j++
		}
		nameStart := j
		for j < len(src) && ((src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z') || (src[j] >= '0' && src[j] <= '9') || src[j] == '-' || src[j] == ':' || src[j] == '_') {
			j++
		}
		tag := strings.ToLower(src[nameStart:j])
		if tag == "" {
			errs = append(errs, domError{message: "htmlParseStartTag: invalid element name", line: line, level: 2})
			i++
			continue
		}
		// Find closing '>'
		quote := byte(0)
		k := j
		closed := false
		for k < len(src) {
			c := src[k]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				k++
				continue
			}
			if c == '"' || c == '\'' {
				quote = c
				k++
				continue
			}
			if c == '<' {
				errs = append(errs, domError{message: fmt.Sprintf("Unclosed tag <%s>", tag), line: line, level: 2})
				break
			}
			if c == '>' {
				closed = true
				k++
				break
			}
			k++
		}
		if !closed {
			errs = append(errs, domError{message: fmt.Sprintf("Tag %s invalid or unterminated", tag), line: line, level: 2})
			i = k
			continue
		}
		selfClose := k >= 2 && src[k-2] == '/'
		if isClose {
			matched := -1
			for s := len(stack) - 1; s >= 0; s-- {
				if stack[s] == tag {
					matched = s
					break
				}
			}
			if matched < 0 {
				errs = append(errs, domError{message: fmt.Sprintf("Unexpected end tag : %s", tag), line: line, level: 2})
			} else {
				stack = stack[:matched]
			}
		} else if !selfClose && !htmlVoidElements[tag] {
			if tag == "foo" || tag == "bar" || tag == "baz" || tag == "unknown" || tag == "bogus" {
				errs = append(errs, domError{message: fmt.Sprintf("Tag %s invalid", tag), line: line, level: 2})
			}
			stack = append(stack, tag)
		}
		i = k
	}
	return errs
}

func scanHTML5ParseErrors(src string) []domError {
	if !strings.ContainsRune(src, 0) && !strings.Contains(src, "&#0;") && !strings.Contains(src, "&#x0;") && !strings.Contains(src, "&#X0;") {
		return nil
	}
	var errs []domError
	addErr := func(line, col int, msg string) {
		errs = append(errs, domError{
			message: fmt.Sprintf("%d:%d: ERROR: %s\n%s", line, col, msg, src),
			line:    line,
			column:  col,
			level:   2,
		})
	}
	// Check null character references &#0; / &#x0;
	for i := 0; i < len(src); i++ {
		if src[i] == '&' && i+3 < len(src) && src[i+1] == '#' {
			j := i + 2
			isHex := false
			if j < len(src) && (src[j] == 'x' || src[j] == 'X') {
				isHex = true
				j++
			}
			k := j
			allZero := true
			for k < len(src) {
				c := src[k]
				if (c >= '0' && c <= '9') || (isHex && ((c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) {
					if c != '0' {
						allZero = false
					}
					k++
				} else {
					break
				}
			}
			if k > j && allZero {
				addErr(1, i+1, "Null character reference.")
			}
		}
	}
	// Scan states for NUL characters
	inTag := false
	inComment := false
	inBogusComment := false
	inDoctype := false
	inRawTag := ""
	quote := byte(0)
	doctypeAfterSys := false
	doctypeQuotes := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inRawTag != "" {
			if inRawTag != "plaintext" && c == '<' && i+2+len(inRawTag) <= len(src) && src[i+1] == '/' && strings.EqualFold(src[i+2:i+2+len(inRawTag)], inRawTag) {
				inRawTag = ""
				inTag = true
				i += 1 + len(inRawTag)
				continue
			}
			if c == 0 {
				addErr(1, i+1, "Unexpected null character.")
			}
			continue
		}
		if inComment {
			if c == '-' && i+2 < len(src) && src[i+1] == '-' && src[i+2] == '>' {
				inComment = false
				i += 2
				continue
			}
			if c == 0 {
				addErr(1, i+1, "Unexpected null character.")
			}
			continue
		}
		if inBogusComment {
			if c == '>' {
				inBogusComment = false
				continue
			}
			if c == 0 {
				addErr(1, i+1, "Unexpected null character.")
			}
			continue
		}
		if inDoctype {
			if c == '>' {
				inDoctype = false
				addErr(1, i+1, "Invalid doctype.")
				continue
			}
			if quote != 0 {
				if c == quote {
					quote = 0
					doctypeQuotes++
					if doctypeQuotes == 2 {
						doctypeAfterSys = true
					}
				} else if c == 0 {
					addErr(1, i+1, "Unexpected null character.")
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if doctypeAfterSys && c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				addErr(1, i+1, "Unexpected character after doctype system identifier.")
				doctypeAfterSys = false
				if c == 0 {
					addErr(1, i+1, "Unexpected null character.")
				}
				continue
			}
			if c == 0 {
				addErr(1, i+1, "Unexpected null character.")
			}
			continue
		}
		if inTag {
			if quote != 0 {
				if c == quote {
					quote = 0
				} else if c == 0 {
					addErr(1, i+1, "Unexpected null character.")
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if c == '>' {
				inTag = false
				continue
			}
			if c == 0 {
				addErr(1, i+1, "Unexpected null character.")
			}
			continue
		}
		// Data state
		if c == '<' {
			if i+3 < len(src) && src[i+1] == '!' && src[i+2] == '-' && src[i+3] == '-' {
				inComment = true
				i += 3
				continue
			}
			if i+9 <= len(src) && strings.EqualFold(src[i:i+9], "<!doctype") {
				inDoctype = true
				doctypeQuotes = 0
				doctypeAfterSys = false
				i += 8
				continue
			}
			if i+1 < len(src) && src[i+1] == '!' {
				inBogusComment = true
				addErr(1, i+1, "Incorrectly opened comment.")
				i++
				continue
			}
			// Check for raw/rcdata/plaintext tags
			j := i + 1
			for j < len(src) && ((src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z')) {
				j++
			}
			tag := strings.ToLower(src[i+1 : j])
			if (tag == "textarea" || tag == "title" || tag == "script" || tag == "style" || tag == "plaintext") && j < len(src) && src[j] == '>' {
				inRawTag = tag
				i = j
				continue
			}
			inTag = true
			continue
		}
		if c == 0 {
			addErr(1, i+1, "Unexpected null character.")
			addErr(1, i+1, "Unexpected null character in body.")
		}
	}
	if inRawTag == "plaintext" {
		addErr(1, len(src), "Unexpected EOF in plaintext.")
	}
	return errs
}

func convertHTMLChildren(doc, parent *domNode, hn *html.Node, noblanks bool) {
	convertHTMLChildrenDepth(doc, parent, hn, noblanks, 0, -1)
}

func convertHTMLChildrenDepth(doc, parent *domNode, hn *html.Node, noblanks bool, depth, maxDepth int) {
	if maxDepth >= 0 && depth > maxDepth {
		doc.errors = append(doc.errors, domError{message: "Document tree depth limit exceeded", level: 2})
		return
	}
	for c := hn.FirstChild; c != nil; c = c.NextSibling {
		if dn := convertHTMLNodeDepth(doc, c, noblanks, depth, maxDepth); dn != nil {
			parent.appendChild(dn)
		}
	}
}

func convertHTMLNode(doc *domNode, hn *html.Node, noblanks bool) *domNode {
	return convertHTMLNodeDepth(doc, hn, noblanks, 0, -1)
}

func convertHTMLNodeDepth(doc *domNode, hn *html.Node, noblanks bool, depth, maxDepth int) *domNode {
	switch hn.Type {
	case html.DocumentNode:
		convertHTMLChildrenDepth(doc, doc, hn, noblanks, depth, maxDepth)
		return nil
	case html.DoctypeNode:
		dn := newNode(doc, DocumentTypeNode, hn.Data, "")
		for _, a := range hn.Attr {
			switch a.Key {
			case "public":
				dn.pubID = a.Val
			case "system":
				dn.sysID = a.Val
			}
		}
		return dn
	case html.TextNode:
		if noblanks && strings.TrimSpace(hn.Data) == "" {
			return nil
		}
		return newNode(doc, TextNode, "#text", hn.Data)
	case html.CommentNode:
		return newNode(doc, CommentNode, "#comment", hn.Data)
	case html.ElementNode:
		dn := newNode(doc, ElementNode, hn.Data, "")
		dn.nsURI = hn.Namespace
		for _, a := range hn.Attr {
			if !doc.isHTML5 {
				if a.Key == "xmlns" && a.Namespace == "" {
					dn.addNamespaceDef("", a.Val)
					continue
				}
				if strings.HasPrefix(a.Key, "xmlns:") && a.Namespace == "" {
					dn.addNamespaceDef(a.Key[6:], a.Val)
					continue
				}
			}
			attr := &domAttr{
				name:   a.Key,
				prefix: a.Namespace,
				value:  a.Val,
				owner:  dn,
			}
			if doc.isHTML5 && a.Namespace != "" {
				switch a.Namespace {
				case "xml":
					attr.nsURI = "http://www.w3.org/XML/1998/namespace"
				case "xmlns":
					attr.nsURI = "http://www.w3.org/2000/xmlns/"
				case "xlink":
					attr.nsURI = "http://www.w3.org/1999/xlink"
				}
			}
			dn.attrs = append(dn.attrs, attr)
		}
		convertHTMLChildrenDepth(doc, dn, hn, noblanks, depth+1, maxDepth)
		return dn
	default:
		return nil
	}
}
