package nokogiri

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func parseHTMLDocument(src, url, encoding string, isHTML5 bool, noblanks bool) *domNode {
	return parseHTMLDocumentOpts(src, url, encoding, isHTML5, noblanks, -1, -1)
}

func parseHTMLDocumentOpts(src, url, encoding string, isHTML5 bool, noblanks bool, maxErrors int, maxTreeDepth int) *domNode {
	doc := newDocument(true, isHTML5)
	doc.url = url
	decoded, detectedEnc := decodeToUTF8(src, encoding, true)
	if encoding != "" {
		doc.encoding = encoding
	} else if detectedEnc != "" {
		doc.encoding = detectedEnc
	}
	if strings.TrimSpace(decoded) == "" {
		doc.quirksMode = "quirks"
		return doc
	}

	root, err := html.Parse(strings.NewReader(decoded))
	if err != nil {
		doc.errors = append(doc.errors, domError{message: err.Error(), level: 2})
		return doc
	}
	convertHTMLChildrenDepth(doc, doc, root, noblanks, 0, maxTreeDepth)
	doc.quirksMode = detectHTMLQuirksMode(doc)
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
	if doc == nil {
		doc = newDocument(true, true)
	}
	frag := newFragment(doc)
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
	nodes, err := html.ParseFragment(strings.NewReader(src), ctxNode)
	if err != nil {
		frag.errors = append(frag.errors, domError{message: err.Error(), level: 2})
		return frag
	}
	for _, hn := range nodes {
		if dn := convertHTMLNode(doc, hn, noblanks); dn != nil {
			frag.appendChild(dn)
		}
	}
	return frag
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
			if a.Key == "xmlns" && a.Namespace == "" {
				dn.addNamespaceDef("", a.Val)
				continue
			}
			if strings.HasPrefix(a.Key, "xmlns:") && a.Namespace == "" {
				dn.addNamespaceDef(a.Key[6:], a.Val)
				continue
			}
			attr := &domAttr{
				name:   a.Key,
				prefix: a.Namespace,
				value:  a.Val,
				owner:  dn,
			}
			dn.attrs = append(dn.attrs, attr)
		}
		convertHTMLChildrenDepth(doc, dn, hn, noblanks, depth+1, maxDepth)
		return dn
	default:
		return nil
	}
}
