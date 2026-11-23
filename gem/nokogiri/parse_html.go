package nokogiri

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func parseHTMLDocument(src, url, encoding string, isHTML5 bool, noblanks bool) *domNode {
	doc := newDocument(true, isHTML5)
	doc.url = url
	if encoding != "" {
		doc.encoding = encoding
	}
	if strings.TrimSpace(src) == "" {
		return doc
	}

	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		doc.errors = append(doc.errors, domError{message: err.Error(), level: 2})
		return doc
	}
	convertHTMLChildren(doc, doc, root, noblanks)
	return doc
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
	for c := hn.FirstChild; c != nil; c = c.NextSibling {
		if dn := convertHTMLNode(doc, c, noblanks); dn != nil {
			parent.appendChild(dn)
		}
	}
}

func convertHTMLNode(doc *domNode, hn *html.Node, noblanks bool) *domNode {
	switch hn.Type {
	case html.DocumentNode:
		convertHTMLChildren(doc, doc, hn, noblanks)
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
		convertHTMLChildren(doc, dn, hn, noblanks)
		return dn
	default:
		return nil
	}
}
