// Package nokogiri implements a self-contained, pure-Go Nokogiri-compatible
// HTML5, HTML4, XML, CSS, XPath 1.0, SAX, and Reader library for ORuby.
package nokogiri

import (
	"fmt"
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
	doc    *domNode
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
	file    string
}

type domNode struct {
	kind   int
	doc    *domNode
	parent *domNode
	first  *domNode
	last   *domNode
	prev   *domNode
	next   *domNode

	name       string // element local name, PI target, doctype name, attr name
	prefix     string // namespace prefix (if any)
	nsURI      string // namespace URI (if any)
	data       string // text, cdata, comment, PI content, doctype public/system
	pubID      string // doctype public id / entity external_id
	sysID      string // doctype system id / entity system_id
	entityType int    // EntityDecl type (1..6)
	attrType   string // AttributeDecl type (CDATA, ID, ENUMERATION, ...)
	attrElem   string // AttributeDecl owning element name
	enumVals   []string
	line       int

	attrs  []*domAttr
	nsDefs []*domNS

	// Document-level metadata (only populated on Document / HTMLDocument / Fragment)
	isHTML              bool
	isHTML5             bool
	rbClass             oruby.RClass
	hasClass            bool
	hasSelfVal          bool
	selfVal             oruby.Value
	hasDecorators       bool
	decoratorsVal       oruby.Value
	encoding            string
	url                 string
	version             string
	hasXMLDecl          bool
	xmlDeclEncoding     string
	standalone          string
	quirksMode          string
	parseNoScriptAsText bool
	parseNoScriptSet    bool
	errors              []domError
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
		kind:    DocumentFragNode,
		doc:     doc,
		isHTML:  doc.isHTML,
		isHTML5: doc.isHTML5,
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
	if n.kind != ElementNode || n.nsURI != "" || n.parent == nil || n.isHTMLDoc() {
		return
	}
	if n.prefix != "" {
		if href := n.parent.lookupNS(n.prefix); href != "" {
			n.nsURI = href
		}
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

func (n *domNode) isAncestorOrSelf(target *domNode) bool {
	if n == nil || target == nil {
		return false
	}
	for cur := target; cur != nil; cur = cur.parent {
		if cur == n {
			return true
		}
	}
	return false
}

func (n *domNode) appendChild(child *domNode) *domNode {
	if child == nil || child == n || child.isAncestorOrSelf(n) {
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
	if child == nil || child == n || child.isAncestorOrSelf(n) {
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
	if sibling == nil || sibling == n {
		return sibling
	}
	if n.parent == nil || sibling.isAncestorOrSelf(n.parent) {
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
	sibling.inheritDefaultNS()
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
	if sibling == nil || sibling == n {
		return sibling
	}
	if n.parent == nil || sibling.isAncestorOrSelf(n.parent) {
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
	sibling.inheritDefaultNS()
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
	for _, a := range n.attrs {
		full := a.name
		if a.prefix != "" {
			full = a.prefix + ":" + a.name
		}
		if full == name {
			return a, true
		}
		if isHTML && strings.EqualFold(full, name) {
			return a, true
		}
	}
	return nil, false
}

func (n *domNode) getAttrByLocalName(name string) (*domAttr, bool) {
	isHTML := n.isHTMLDoc()
	for _, a := range n.attrs {
		if a.name == name {
			return a, true
		}
		if isHTML && strings.EqualFold(a.name, name) {
			return a, true
		}
	}
	return nil, false
}

func (n *domNode) setAttr(name, val string) *domAttr {
	if a, ok := n.getAttr(name); ok {
		a.value = val
		return a
	}
	prefix := ""
	local := name
	nsURI := ""
	if idx := strings.IndexByte(name, ':'); idx > 0 {
		p := name[:idx]
		l := name[idx+1:]
		if uri := n.lookupNS(p); uri != "" {
			prefix = p
			local = l
			nsURI = uri
		}
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
		if full == name || (isHTML && strings.EqualFold(full, name)) {
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
			ns.doc = n.doc
			return ns
		}
	}
	ns := &domNS{prefix: prefix, href: href, doc: n.doc}
	n.nsDefs = append(n.nsDefs, ns)
	return ns
}

func (n *domNode) namespaceScopes() []*domNS {
	seen := map[string]bool{}
	var out []*domNS
	for cur := n; cur != nil; cur = cur.parent {
		for _, ns := range cur.nsDefs {
			if !seen[ns.prefix] {
				seen[ns.prefix] = true
				out = append(out, ns)
			}
		}
	}
	return out
}

func (n *domNode) getAttrWithNS(name, nsURI string) (*domAttr, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			if nsURI == "" && a.nsURI == "" && a.prefix == "" {
				return a, true
			}
			uri := a.nsURI
			if uri == "" && a.prefix != "" {
				uri = n.lookupNS(a.prefix)
			}
			if nsURI != "" && uri == nsURI {
				return a, true
			}
		}
	}
	return nil, false
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
	if n.kind == DocumentFragNode {
		return "?"
	}
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

var html4BooleanAttrs = map[string]bool{
	"checked": true, "compact": true, "declare": true, "defer": true,
	"disabled": true, "ismap": true, "multiple": true, "nohref": true,
	"noresize": true, "noshade": true, "nowrap": true, "readonly": true,
	"selected": true,
}

func compareDocOrder(a, b *domNode) (int, bool) {
	if a == b {
		return 0, true
	}
	if a == nil || b == nil {
		return 0, false
	}
	// Walk up to find root ancestor of each
	pathA := []*domNode{}
	for cur := a; cur != nil; cur = cur.parent {
		pathA = append(pathA, cur)
	}
	pathB := []*domNode{}
	for cur := b; cur != nil; cur = cur.parent {
		pathB = append(pathB, cur)
	}
	if pathA[len(pathA)-1] != pathB[len(pathB)-1] {
		return 0, false
	}
	ia, ib := len(pathA)-1, len(pathB)-1
	for ia >= 0 && ib >= 0 && pathA[ia] == pathB[ib] {
		ia--
		ib--
	}
	if ia < 0 {
		return -1, true // a is ancestor of b
	}
	if ib < 0 {
		return 1, true // b is ancestor of a
	}
	// Siblings under common parent
	childA, childB := pathA[ia], pathB[ib]
	if childA.kind == AttributeNode || childB.kind == AttributeNode {
		return 0, false
	}
	for cur := childA.next; cur != nil; cur = cur.next {
		if cur == childB {
			return -1, true
		}
	}
	return 1, true
}

var htmlVoidElements = map[string]bool{
	"area": true, "base": true, "basefont": true, "bgsound": true, "br": true,
	"col": true, "embed": true, "frame": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true,
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

func escapeHTMLText(s string, isHTML5 bool) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch b {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case 0xC2:
			if isHTML5 && i+1 < len(s) && s[i+1] == 0xA0 {
				sb.WriteString("&nbsp;")
				i++
			} else {
				sb.WriteByte(b)
			}
		default:
			sb.WriteByte(b)
		}
	}
	return sb.String()
}

func escapeAttrVal(s string) string {
	return escapeHTMLAttrVal(s, false)
}

func escapeHTMLAttrVal(s string, isHTML5 bool) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch b {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case 0xC2:
			if isHTML5 && i+1 < len(s) && s[i+1] == 0xA0 {
				sb.WriteString("&nbsp;")
				i++
			} else {
				sb.WriteByte(b)
			}
		default:
			sb.WriteByte(b)
		}
	}
	return sb.String()
}

func (n *domNode) serialize(asHTML bool, indent int, includeXMLDecl bool) string {
	return n.serializeWithOpts(asHTML, indent, includeXMLDecl, false, false)
}

func (n *domNode) serializeWithOpts(asHTML bool, indent int, includeXMLDecl, noEmptyTags, preserveNewline bool) string {
	var sb strings.Builder
	n.writeTo(&sb, asHTML, indent, 0, includeXMLDecl, noEmptyTags, preserveNewline)
	return sb.String()
}

func (n *domNode) needsPreservedLeadingNewline(asHTML, preserveNewline bool) bool {
	if !preserveNewline || !asHTML || n.kind != ElementNode || n.nsURI != "" {
		return false
	}
	switch strings.ToLower(n.name) {
	case "pre", "listing", "textarea":
		return n.first != nil && n.first.kind == TextNode && strings.HasPrefix(n.first.data, "\n")
	}
	return false
}

func (n *domNode) innerHTML(asHTML bool) string {
	return n.innerHTMLWithOpts(asHTML, false)
}

func (n *domNode) innerHTMLWithOpts(asHTML, preserveNewline bool) string {
	var sb strings.Builder
	if n.needsPreservedLeadingNewline(asHTML, preserveNewline) {
		sb.WriteByte('\n')
	}
	for c := n.first; c != nil; c = c.next {
		c.writeTo(&sb, asHTML, 0, 0, false, false, preserveNewline)
	}
	return sb.String()
}

func (n *domNode) cssPath() string {
	if n.kind != ElementNode {
		return ""
	}
	var parts []string
	for cur := n; cur != nil && cur.kind == ElementNode; cur = cur.parent {
		seg := cur.name
		if id, ok := cur.getAttr("id"); ok && id.value != "" {
			parts = append(parts, "#"+id.value)
			break
		}
		if cur.parent != nil && cur.parent.kind == ElementNode {
			idx := 1
			total := 0
			for s := cur.parent.first; s != nil; s = s.next {
				if s.kind == ElementNode && s.name == cur.name {
					total++
					if s == cur {
						idx = total
					}
				}
			}
			if total > 1 {
				seg = fmt.Sprintf("%s:nth-of-type(%d)", cur.name, idx)
			}
		}
		parts = append(parts, seg)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " > ")
}

func (n *domNode) writeTo(sb *strings.Builder, asHTML bool, indent, depth int, includeXMLDecl, noEmptyTags, preserveNewline bool) {
	isHTML5 := n.isHTML5 || (n.doc != nil && n.doc.isHTML5)
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
			c.writeTo(sb, asHTML, indent, 0, false, noEmptyTags, preserveNewline)
		}
	case DocumentFragNode:
		for c := n.first; c != nil; c = c.next {
			c.writeTo(sb, asHTML, indent, depth, false, noEmptyTags, preserveNewline)
		}
	case DocumentTypeNode, DTDNode:
		if n.kind == DTDNode && n.parent != nil && (n.parent.kind == DocumentNode || n.parent.kind == HTMLDocumentNode) {
			for s := n.parent.first; s != nil; s = s.next {
				if s.kind == DocumentTypeNode {
					return
				}
			}
		}
		sb.WriteString("<!DOCTYPE ")
		if n.name != "" {
			sb.WriteString(n.name)
		} else {
			sb.WriteString("html")
		}
		if !(asHTML && isHTML5) {
			if n.pubID != "" {
				fmt.Fprintf(sb, " PUBLIC %q", n.pubID)
				if n.sysID != "" {
					fmt.Fprintf(sb, " %q", n.sysID)
				}
			} else if n.sysID != "" {
				fmt.Fprintf(sb, " SYSTEM %q", n.sysID)
			}
			sb.WriteString(">\n")
		} else {
			sb.WriteByte('>')
		}
	case EntityRefNode:
		sb.WriteByte('&')
		sb.WriteString(n.name)
		sb.WriteByte(';')
	case EntityDecl:
		sb.WriteString("<!ENTITY ")
		if n.entityType == 4 || n.entityType == 5 {
			sb.WriteString("% ")
		}
		sb.WriteString(n.name)
		if n.pubID != "" {
			fmt.Fprintf(sb, " PUBLIC %q %q", n.pubID, n.sysID)
		} else if n.sysID != "" {
			fmt.Fprintf(sb, " SYSTEM %q", n.sysID)
		} else {
			fmt.Fprintf(sb, " %q", n.data)
		}
		sb.WriteByte('>')
	case AttributeNode:
		if n.prefix != "" {
			sb.WriteString(n.prefix)
			sb.WriteByte(':')
		}
		sb.WriteString(n.name)
		if asHTML && !isHTML5 && n.prefix == "" && html4BooleanAttrs[strings.ToLower(n.name)] && n.data == "" {
			return
		}
		sb.WriteString(`="`)
		sb.WriteString(escapeHTMLAttrVal(n.data, asHTML && isHTML5))
		sb.WriteByte('"')
	case TextNode:
		rawParent := false
		if asHTML && n.parent != nil && n.parent.kind == ElementNode && n.parent.nsURI == "" {
			pn := strings.ToLower(n.parent.name)
			if htmlRawTextElements[pn] {
				rawParent = true
			} else if pn == "noscript" && n.doc != nil && n.doc.parseNoScriptAsText {
				rawParent = true
			}
		}
		if rawParent {
			sb.WriteString(n.data)
		} else if asHTML {
			sb.WriteString(escapeHTMLText(n.data, isHTML5))
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
		if !(asHTML && isHTML5) {
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
		}
		for _, a := range n.attrs {
			sb.WriteByte(' ')
			if asHTML && isHTML5 {
				local := a.name
				if idx := strings.IndexByte(local, ':'); idx >= 0 {
					local = local[idx+1:]
				}
				switch a.nsURI {
				case "http://www.w3.org/XML/1998/namespace":
					sb.WriteString("xml:")
					sb.WriteString(local)
				case "http://www.w3.org/2000/xmlns/":
					if local == "xmlns" {
						sb.WriteString("xmlns")
					} else {
						sb.WriteString("xmlns:")
						sb.WriteString(local)
					}
				case "http://www.w3.org/1999/xlink":
					sb.WriteString("xlink:")
					sb.WriteString(local)
				default:
					if a.prefix != "" {
						sb.WriteString(a.prefix)
						sb.WriteByte(':')
					}
					sb.WriteString(a.name)
				}
			} else {
				if a.prefix != "" {
					sb.WriteString(a.prefix)
					sb.WriteByte(':')
				}
				sb.WriteString(a.name)
				if asHTML && !isHTML5 && a.prefix == "" && html4BooleanAttrs[strings.ToLower(a.name)] && a.value == "" {
					continue
				}
			}
			sb.WriteString(`="`)
			val := a.value
			if asHTML && !isHTML5 && a.prefix == "" {
				switch strings.ToLower(a.name) {
				case "href", "src", "action", "name":
					val = strings.ReplaceAll(val, `"`, "%22")
				}
			}
			sb.WriteString(escapeHTMLAttrVal(val, asHTML && isHTML5))
			sb.WriteByte('"')
		}
		if asHTML && n.nsURI == "" && htmlVoidElements[strings.ToLower(n.name)] {
			sb.WriteByte('>')
			if indent > 0 {
				sb.WriteByte('\n')
			}
			return
		}
		if n.first == nil {
			if asHTML {
				sb.WriteString("></")
				sb.WriteString(tag)
				sb.WriteByte('>')
			} else if noEmptyTags {
				if n.isHTMLDoc() && n.nsURI == "" && htmlVoidElements[strings.ToLower(n.name)] {
					sb.WriteString(" />")
				} else {
					sb.WriteString("></")
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
		if n.needsPreservedLeadingNewline(asHTML, preserveNewline) {
			sb.WriteByte('\n')
		}
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
			c.writeTo(sb, asHTML, indent, depth+1, false, noEmptyTags, preserveNewline)
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
