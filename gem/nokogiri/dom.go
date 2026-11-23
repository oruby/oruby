// Package nokogiri implements a self-contained, pure-Go Nokogiri-compatible
// HTML5, HTML4, XML, CSS, XPath 1.0, SAX, and Reader library for ORuby.
package nokogiri

import (
	"fmt"
	"html"
	"strings"

	"github.com/oruby/oruby"
)

// Nokogiri XML::Node type constants (matching libxml2 xmlElementType).
const (
	ElementNode      = 1
	AttributeNode    = 2
	TextNode         = 3
	CDATASectionNode = 4
	EntityRefNode    = 5
	EntityNode       = 6
	PINode           = 7
	CommentNode      = 8
	DocumentNode     = 9
	DocumentTypeNode = 10
	DocumentFragNode = 11
	NotationNode     = 12
	HTMLDocumentNode = 13
	DTDNode          = 14
	ElementDecl      = 15
	AttributeDecl    = 16
	EntityDecl       = 17
	NamespaceDecl    = 18
	XIncludeStart    = 19
	XIncludeEnd      = 20
	DocbDocumentNode = 21
)

type domNS struct {
	prefix string
	href   string
}

type domAttr struct {
	name   string // local name
	prefix string
	nsURI  string
	value  string
	owner  *domNode
}

type domError struct {
	message string
	line    int
	column  int
	level   int // 1=warning, 2=error, 3=fatal
}

type domNode struct {
	kind   int
	doc    *domNode
	parent *domNode
	first  *domNode
	last   *domNode
	prev   *domNode
	next   *domNode

	name   string // element local name, PI target, doctype name, attr name
	prefix string // namespace prefix (if any)
	nsURI  string // namespace URI (if any)
	data   string // text, cdata, comment, PI content, doctype public/system
	pubID  string // doctype public id
	sysID  string // doctype system id
	line   int

	attrs  []*domAttr
	nsDefs []*domNS

	// Document-level metadata (only populated on Document / HTMLDocument / Fragment)
	isHTML        bool
	isHTML5       bool
	rbClass       oruby.RClass
	hasClass      bool
	hasDecorators bool
	decoratorsVal oruby.Value
	encoding      string
	url           string
	version       string
	errors        []domError
}

func newDocument(isHTML, isHTML5 bool) *domNode {
	kind := DocumentNode
	if isHTML {
		kind = HTMLDocumentNode
	}
	d := &domNode{
		kind:     kind,
		isHTML:   isHTML,
		isHTML5:  isHTML5,
		encoding: "UTF-8",
		version:  "1.0",
	}
	d.doc = d
	return d
}

func newFragment(doc *domNode) *domNode {
	if doc == nil {
		doc = newDocument(false, false)
	}
	return &domNode{
		kind:   DocumentFragNode,
		doc:    doc,
		isHTML: doc.isHTML,
	}
}

func newNode(doc *domNode, kind int, name, data string) *domNode {
	return &domNode{
		kind: kind,
		doc:  doc,
		name: name,
		data: data,
	}
}

// ---------------------------------------------------------------------------
// Tree manipulation
// ---------------------------------------------------------------------------

func (n *domNode) unlink() {
	if n.parent == nil {
		return
	}
	if n.kind == AttributeNode {
		key := n.name
		if n.prefix != "" {
			key = n.prefix + ":" + n.name
		}
		n.parent.removeAttr(key)
		n.parent = nil
		return
	}
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		n.parent.first = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		n.parent.last = n.prev
	}
	n.parent = nil
	n.prev = nil
	n.next = nil
}

func (n *domNode) inheritDefaultNS() {
	if n.kind != ElementNode || n.prefix != "" || n.nsURI != "" || n.parent == nil || n.isHTMLDoc() {
		return
	}
	for _, ns := range n.nsDefs {
		if ns.prefix == "" {
			return // explicit xmlns="" or local xmlns
		}
	}
	if href := n.parent.lookupNS(""); href != "" {
		n.nsURI = href
	}
}

func (n *domNode) setDocRecursive(doc *domNode) {
	if n.doc == doc {
		return
	}
	n.doc = doc
	for c := n.first; c != nil; c = c.next {
		c.setDocRecursive(doc)
	}
}

func (n *domNode) appendChild(child *domNode) *domNode {
	if child == nil || child == n {
		return child
	}
	if child.kind == DocumentFragNode {
		var lastAdded *domNode
		for c := child.first; c != nil; {
			nxt := c.next
			c.unlink()
			n.appendChild(c)
			lastAdded = c
			c = nxt
		}
		return lastAdded
	}
	child.unlink()
	child.parent = n
	if n.doc != nil {
		child.setDocRecursive(n.doc)
	}
	child.inheritDefaultNS()
	if n.last == nil {
		n.first = child
		n.last = child
	} else {
		child.prev = n.last
		n.last.next = child
		n.last = child
	}
	return child
}

func (n *domNode) prependChild(child *domNode) *domNode {
	if child == nil || child == n {
		return child
	}
	if child.kind == DocumentFragNode {
		ref := n.first
		var firstAdded *domNode
		for c := child.first; c != nil; {
			nxt := c.next
			c.unlink()
			if ref == nil {
				n.appendChild(c)
			} else {
				ref.insertBefore(c)
			}
			if firstAdded == nil {
				firstAdded = c
			}
			c = nxt
		}
		return firstAdded
	}
	child.unlink()
	child.parent = n
	if n.doc != nil {
		child.setDocRecursive(n.doc)
	}
	child.inheritDefaultNS()
	if n.first == nil {
		n.first = child
		n.last = child
	} else {
		child.next = n.first
		n.first.prev = child
		n.first = child
	}
	return child
}

func (n *domNode) insertBefore(sibling *domNode) *domNode {
	if sibling == nil || n.parent == nil || sibling == n {
		return sibling
	}
	if sibling.kind == DocumentFragNode {
		var lastAdded *domNode
		for c := sibling.first; c != nil; {
			nxt := c.next
			c.unlink()
			n.insertBefore(c)
			lastAdded = c
			c = nxt
		}
		return lastAdded
	}
	sibling.unlink()
	sibling.parent = n.parent
	if n.doc != nil {
		sibling.setDocRecursive(n.doc)
	}
	sibling.prev = n.prev
	sibling.next = n
	if n.prev != nil {
		n.prev.next = sibling
	} else {
		n.parent.first = sibling
	}
	n.prev = sibling
	return sibling
}

func (n *domNode) insertAfter(sibling *domNode) *domNode {
	if sibling == nil || n.parent == nil || sibling == n {
		return sibling
	}
	if sibling.kind == DocumentFragNode {
		cur := n
		for c := sibling.first; c != nil; {
			nxt := c.next
			c.unlink()
			cur.insertAfter(c)
			cur = c
			c = nxt
		}
		return cur
	}
	sibling.unlink()
	sibling.parent = n.parent
	if n.doc != nil {
		sibling.setDocRecursive(n.doc)
	}
	sibling.next = n.next
	sibling.prev = n
	if n.next != nil {
		n.next.prev = sibling
	} else {
		n.parent.last = sibling
	}
	n.next = sibling
	return sibling
}

func (n *domNode) replaceWith(other *domNode) *domNode {
	if n.parent == nil || other == nil {
		return other
	}
	n.insertBefore(other)
	n.unlink()
	return other
}

func (n *domNode) clearChildren() {
	for c := n.first; c != nil; {
		nxt := c.next
		c.parent = nil
		c.prev = nil
		c.next = nil
		c = nxt
	}
	n.first = nil
	n.last = nil
}

func (n *domNode) clone(deep bool, targetDoc *domNode) *domNode {
	cp := &domNode{
		kind:          n.kind,
		doc:           targetDoc,
		name:          n.name,
		prefix:        n.prefix,
		nsURI:         n.nsURI,
		data:          n.data,
		pubID:         n.pubID,
		sysID:         n.sysID,
		line:          n.line,
		isHTML:        n.isHTML,
		isHTML5:       n.isHTML5,
		rbClass:       n.rbClass,
		hasClass:      n.hasClass,
		hasDecorators: n.hasDecorators,
		decoratorsVal: n.decoratorsVal,
		encoding:      n.encoding,
		url:           n.url,
		version:       n.version,
	}
	if cp.kind == DocumentNode || cp.kind == HTMLDocumentNode {
		cp.doc = cp
		targetDoc = cp
	} else if cp.doc == nil {
		cp.doc = n.doc
	}
	if len(n.attrs) > 0 {
		cp.attrs = make([]*domAttr, len(n.attrs))
		for i, a := range n.attrs {
			cp.attrs[i] = &domAttr{
				name:   a.name,
				prefix: a.prefix,
				nsURI:  a.nsURI,
				value:  a.value,
				owner:  cp,
			}
		}
	}
	if len(n.nsDefs) > 0 {
		cp.nsDefs = make([]*domNS, len(n.nsDefs))
		for i, ns := range n.nsDefs {
			cp.nsDefs[i] = &domNS{prefix: ns.prefix, href: ns.href}
		}
	}
	if deep {
		for c := n.first; c != nil; c = c.next {
			cp.appendChild(c.clone(true, targetDoc))
		}
	}
	return cp
}

// ---------------------------------------------------------------------------
// Attributes & Namespaces
// ---------------------------------------------------------------------------

func (n *domNode) isHTMLDoc() bool {
	return n.isHTML || (n.doc != nil && n.doc.isHTML)
}

func (n *domNode) getAttr(name string) (*domAttr, bool) {
	isHTML := n.isHTMLDoc()
	// Check exact match first, then local name (and case-insensitive for HTML)
	for _, a := range n.attrs {
		full := a.name
		if a.prefix != "" {
			full = a.prefix + ":" + a.name
		}
		if full == name || a.name == name {
			return a, true
		}
		if isHTML && (strings.EqualFold(full, name) || strings.EqualFold(a.name, name)) {
			return a, true
		}
	}
	return nil, false
}

func (n *domNode) setAttr(name, val string) *domAttr {
	prefix := ""
	local := name
	if idx := strings.IndexByte(name, ':'); idx > 0 {
		prefix = name[:idx]
		local = name[idx+1:]
	}
	if a, ok := n.getAttr(name); ok {
		a.value = val
		return a
	}
	nsURI := ""
	if prefix != "" {
		nsURI = n.lookupNS(prefix)
	}
	a := &domAttr{
		name:   local,
		prefix: prefix,
		nsURI:  nsURI,
		value:  val,
		owner:  n,
	}
	n.attrs = append(n.attrs, a)
	return a
}

func (n *domNode) removeAttr(name string) (string, bool) {
	isHTML := n.isHTMLDoc()
	for i, a := range n.attrs {
		full := a.name
		if a.prefix != "" {
			full = a.prefix + ":" + a.name
		}
		if full == name || a.name == name || (isHTML && strings.EqualFold(a.name, name)) {
			val := a.value
			n.attrs = append(n.attrs[:i], n.attrs[i+1:]...)
			return val, true
		}
	}
	return "", false
}

func (n *domNode) addNamespaceDef(prefix, href string) *domNS {
	for _, ns := range n.nsDefs {
		if ns.prefix == prefix {
			ns.href = href
			return ns
		}
	}
	ns := &domNS{prefix: prefix, href: href}
	n.nsDefs = append(n.nsDefs, ns)
	return ns
}

func (n *domNode) lookupNS(prefix string) string {
	if prefix == "xml" {
		return "http://www.w3.org/XML/1998/namespace"
	}
	for cur := n; cur != nil; cur = cur.parent {
		for _, ns := range cur.nsDefs {
			if ns.prefix == prefix {
				return ns.href
			}
		}
	}
	return ""
}

func (n *domNode) collectNamespaces(out map[string]string) {
	for _, ns := range n.nsDefs {
		k := "xmlns"
		if ns.prefix != "" {
			k = "xmlns:" + ns.prefix
		}
		if _, exists := out[k]; !exists {
			out[k] = ns.href
		}
	}
	for c := n.first; c != nil; c = c.next {
		c.collectNamespaces(out)
	}
}

func (n *domNode) scopeNamespaces() map[string]string {
	out := map[string]string{}
	for cur := n; cur != nil; cur = cur.parent {
		for _, ns := range cur.nsDefs {
			k := "xmlns"
			if ns.prefix != "" {
				k = "xmlns:" + ns.prefix
			}
			if _, exists := out[k]; !exists {
				out[k] = ns.href
			}
		}
	}
	return out
}

func (n *domNode) removeNamespacesRecursive() {
	n.prefix = ""
	n.nsURI = ""
	n.nsDefs = nil
	for _, a := range n.attrs {
		a.prefix = ""
		a.nsURI = ""
	}
	for c := n.first; c != nil; c = c.next {
		c.removeNamespacesRecursive()
	}
}

// ---------------------------------------------------------------------------
// Text / Root / Navigation helpers
// ---------------------------------------------------------------------------

func (n *domNode) rootElement() *domNode {
	doc := n
	if doc.kind != DocumentNode && doc.kind != HTMLDocumentNode {
		doc = n.doc
	}
	if doc == nil {
		return nil
	}
	for c := doc.first; c != nil; c = c.next {
		if c.kind == ElementNode {
			return c
		}
	}
	return nil
}

func (n *domNode) innerText() string {
	switch n.kind {
	case TextNode, CDATASectionNode, CommentNode, PINode:
		return n.data
	case AttributeNode:
		return n.data
	}
	var sb strings.Builder
	n.collectText(&sb)
	return sb.String()
}

func (n *domNode) collectText(sb *strings.Builder) {
	for c := n.first; c != nil; c = c.next {
		switch c.kind {
		case TextNode, CDATASectionNode:
			sb.WriteString(c.data)
		case ElementNode, DocumentNode, HTMLDocumentNode, DocumentFragNode:
			c.collectText(sb)
		}
	}
}

func (n *domNode) nextElement() *domNode {
	for s := n.next; s != nil; s = s.next {
		if s.kind == ElementNode {
			return s
		}
	}
	return nil
}

func (n *domNode) prevElement() *domNode {
	for s := n.prev; s != nil; s = s.prev {
		if s.kind == ElementNode {
			return s
		}
	}
	return nil
}

func (n *domNode) firstElementChild() *domNode {
	for c := n.first; c != nil; c = c.next {
		if c.kind == ElementNode {
			return c
		}
	}
	return nil
}

func (n *domNode) lastElementChild() *domNode {
	for c := n.last; c != nil; c = c.prev {
		if c.kind == ElementNode {
			return c
		}
	}
	return nil
}

func (n *domNode) path() string {
	if n.kind == DocumentNode || n.kind == HTMLDocumentNode {
		return "/"
	}
	var parts []string
	for cur := n; cur != nil && cur.kind != DocumentNode && cur.kind != HTMLDocumentNode && cur.kind != DocumentFragNode; cur = cur.parent {
		switch cur.kind {
		case ElementNode:
			seg := cur.name
			if cur.prefix != "" {
				seg = cur.prefix + ":" + cur.name
			}
			// Count siblings with same name
			idx := 1
			total := 0
			if cur.parent != nil {
				for s := cur.parent.first; s != nil; s = s.next {
					if s.kind == ElementNode && s.name == cur.name && s.prefix == cur.prefix {
						total++
						if s == cur {
							idx = total
						}
					}
				}
			}
			if total > 1 {
				seg = fmt.Sprintf("%s[%d]", seg, idx)
			}
			parts = append(parts, seg)
		case TextNode, CDATASectionNode:
			parts = append(parts, "text()")
		case CommentNode:
			parts = append(parts, "comment()")
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return "/" + strings.Join(parts, "/")
}

// ---------------------------------------------------------------------------
// Serialization (XML & HTML)
// ---------------------------------------------------------------------------

var htmlVoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "keygen": true, "link": true,
	"meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

var htmlRawTextElements = map[string]bool{
	"script": true, "style": true, "xmp": true, "iframe": true,
	"noembed": true, "noframes": true, "plaintext": true,
}

func escapeXMLText(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

func escapeAttrVal(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

func (n *domNode) serialize(asHTML bool, indent int, includeXMLDecl bool) string {
	var sb strings.Builder
	n.writeTo(&sb, asHTML, indent, 0, includeXMLDecl)
	return sb.String()
}

func (n *domNode) innerHTML(asHTML bool) string {
	var sb strings.Builder
	for c := n.first; c != nil; c = c.next {
		c.writeTo(&sb, asHTML, 0, 0, false)
	}
	return sb.String()
}

func (n *domNode) writeTo(sb *strings.Builder, asHTML bool, indent, depth int, includeXMLDecl bool) {
	switch n.kind {
	case DocumentNode, HTMLDocumentNode:
		if !asHTML && includeXMLDecl {
			ver := n.version
			if ver == "" {
				ver = "1.0"
			}
			enc := n.encoding
			if enc != "" {
				fmt.Fprintf(sb, "<?xml version=%q encoding=%q?>\n", ver, enc)
			} else {
				fmt.Fprintf(sb, "<?xml version=%q?>\n", ver)
			}
		}
		for c := n.first; c != nil; c = c.next {
			c.writeTo(sb, asHTML, indent, 0, false)
		}
	case DocumentFragNode:
		for c := n.first; c != nil; c = c.next {
			c.writeTo(sb, asHTML, indent, depth, false)
		}
	case DocumentTypeNode, DTDNode:
		sb.WriteString("<!DOCTYPE ")
		if n.name != "" {
			sb.WriteString(n.name)
		} else {
			sb.WriteString("html")
		}
		if n.pubID != "" {
			fmt.Fprintf(sb, " PUBLIC %q", n.pubID)
			if n.sysID != "" {
				fmt.Fprintf(sb, " %q", n.sysID)
			}
		} else if n.sysID != "" {
			fmt.Fprintf(sb, " SYSTEM %q", n.sysID)
		}
		sb.WriteString(">\n")
	case TextNode:
		if asHTML && n.parent != nil && htmlRawTextElements[strings.ToLower(n.parent.name)] {
			sb.WriteString(n.data)
		} else if asHTML {
			sb.WriteString(html.EscapeString(n.data))
		} else {
			sb.WriteString(escapeXMLText(n.data))
		}
	case CDATASectionNode:
		sb.WriteString("<![CDATA[")
		sb.WriteString(n.data)
		sb.WriteString("]]>")
	case CommentNode:
		sb.WriteString("<!--")
		sb.WriteString(n.data)
		sb.WriteString("-->")
	case PINode:
		sb.WriteString("<?")
		sb.WriteString(n.name)
		if n.data != "" {
			sb.WriteByte(' ')
			sb.WriteString(n.data)
		}
		sb.WriteString("?>")
	case ElementNode:
		if indent > 0 && depth > 0 {
			sb.WriteString(strings.Repeat(" ", indent*depth))
		}
		tag := n.name
		if n.prefix != "" {
			tag = n.prefix + ":" + n.name
		}
		sb.WriteByte('<')
		sb.WriteString(tag)
		for _, ns := range n.nsDefs {
			if ns.prefix == "" {
				sb.WriteString(` xmlns="`)
			} else {
				sb.WriteString(` xmlns:`)
				sb.WriteString(ns.prefix)
				sb.WriteString(`="`)
			}
			sb.WriteString(escapeAttrVal(ns.href))
			sb.WriteByte('"')
		}
		for _, a := range n.attrs {
			sb.WriteByte(' ')
			if a.prefix != "" {
				sb.WriteString(a.prefix)
				sb.WriteByte(':')
			}
			sb.WriteString(a.name)
			sb.WriteString(`="`)
			sb.WriteString(escapeAttrVal(a.value))
			sb.WriteByte('"')
		}
		if n.first == nil {
			if asHTML {
				sb.WriteByte('>')
				if !htmlVoidElements[strings.ToLower(n.name)] {
					sb.WriteString("</")
					sb.WriteString(tag)
					sb.WriteByte('>')
				}
			} else {
				sb.WriteString("/>")
			}
			if indent > 0 {
				sb.WriteByte('\n')
			}
			return
		}
		sb.WriteByte('>')
		hasElemChild := false
		if indent > 0 {
			for c := n.first; c != nil; c = c.next {
				if c.kind == ElementNode {
					hasElemChild = true
					break
				}
			}
			if hasElemChild {
				sb.WriteByte('\n')
			}
		}
		for c := n.first; c != nil; c = c.next {
			c.writeTo(sb, asHTML, indent, depth+1, false)
		}
		if indent > 0 && hasElemChild {
			sb.WriteString(strings.Repeat(" ", indent*depth))
		}
		sb.WriteString("</")
		sb.WriteString(tag)
		sb.WriteByte('>')
		if indent > 0 {
			sb.WriteByte('\n')
		}
	}
}
