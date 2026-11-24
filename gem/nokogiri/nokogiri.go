package nokogiri

import (
	_ "embed"
	"fmt"
	"strings"
	"unsafe"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	_ "github.com/oruby/oruby/gem/racc"
)

//go:embed nokogiri.rb
var nokogiriRb []byte

// Exact Nokogiri::XML::ParseOptions bit flags
const (
	poStrict    = 0
	poRecover   = 1 << 0
	poNoEnt     = 1 << 1
	poDTDLoad   = 1 << 2
	poDTDAttr   = 1 << 3
	poDTDValid  = 1 << 4
	poNoError   = 1 << 5
	poNoWarning = 1 << 6
	poPedantic  = 1 << 7
	poNoBlanks  = 1 << 8
	poSAX1      = 1 << 9
	poXInclude  = 1 << 10
	poNoNet     = 1 << 11
	poNoDict    = 1 << 12
	poNSClean   = 1 << 13
	poNoCDATA   = 1 << 14
	poNoXInc    = 1 << 15
	poCompact   = 1 << 16
	poHuge      = 1 << 19
)

func init() {
	oruby.LazyGem("nokogiri", initNokogiri)
	for _, sub := range []string{
		"nokogiri/nokogiri",
		"nokogiri/xml",
		"nokogiri/html",
		"nokogiri/html4",
		"nokogiri/html5",
		"nokogiri/css",
		"nokogiri/version",
	} {
		oruby.GemAlias(sub, "nokogiri")
	}
}

func initNokogiri(mrb *oruby.MrbState) any {
	_, _ = mrb.Resolve("racc/parser")
	mod := mrb.DefineModule("Nokogiri")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("1.19.7"))

	stdErr := mrb.ExcGet("StandardError")
	synErr := mrb.DefineClassUnder(mod, "SyntaxError", stdErr)

	xmlMod := mrb.DefineModuleUnder(mod, "XML")
	mrb.ConstSet(xmlMod, mrb.Intern("XML_C14N_1_0"), mrb.FixnumValue(xmlC14N10))
	mrb.ConstSet(xmlMod, mrb.Intern("XML_C14N_EXCLUSIVE_1_0"), mrb.FixnumValue(xmlC14NExclusive10))
	mrb.ConstSet(xmlMod, mrb.Intern("XML_C14N_1_1"), mrb.FixnumValue(xmlC14N11))
	xmlSynErr := mrb.DefineClassUnder(xmlMod, "SyntaxError", synErr)
	xpathMod := mrb.DefineModuleUnder(xmlMod, "XPath")
	mrb.DefineClassUnder(xpathMod, "SyntaxError", xmlSynErr)

	cssMod := mrb.DefineModuleUnder(mod, "CSS")
	mrb.DefineClassUnder(cssMod, "SyntaxError", synErr)
	cssMod.DefineClassMethod("__xpath_for", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		sel := args[0].String()
		prefix := "//"
		if len(args) >= 2 && !args[1].IsNil() {
			prefix = args[1].String()
		}
		xp, err := cssToXPath(sel, prefix)
		if err != nil {
			return rb.Raise(mrb, "Nokogiri::CSS::SyntaxError", "%s", err.Error())
		}
		return mrb.StrNew(xp)
	}, mrb.ArgsArg(1, 1))

	// Node hierarchy
	nodeCls := mrb.DefineClassUnder(xmlMod, "Node", mrb.ObjectClass())
	rb.DataClass(nodeCls)

	elemCls := mrb.DefineClassUnder(xmlMod, "Element", nodeCls)
	rb.DataClass(elemCls)
	attrCls := mrb.DefineClassUnder(xmlMod, "Attr", nodeCls)
	rb.DataClass(attrCls)
	charDataCls := mrb.DefineClassUnder(xmlMod, "CharacterData", nodeCls)
	rb.DataClass(charDataCls)
	textCls := mrb.DefineClassUnder(xmlMod, "Text", charDataCls)
	rb.DataClass(textCls)
	cdataCls := mrb.DefineClassUnder(xmlMod, "CDATA", textCls)
	rb.DataClass(cdataCls)
	commentCls := mrb.DefineClassUnder(xmlMod, "Comment", charDataCls)
	rb.DataClass(commentCls)
	piCls := mrb.DefineClassUnder(xmlMod, "ProcessingInstruction", nodeCls)
	rb.DataClass(piCls)
	entityRefCls := mrb.DefineClassUnder(xmlMod, "EntityReference", nodeCls)
	rb.DataClass(entityRefCls)
	entityDeclCls := mrb.DefineClassUnder(xmlMod, "EntityDecl", nodeCls)
	rb.DataClass(entityDeclCls)
	for name, val := range map[string]int{
		"INTERNAL_GENERAL":          1,
		"EXTERNAL_GENERAL_PARSED":   2,
		"EXTERNAL_GENERAL_UNPARSED": 3,
		"INTERNAL_PARAMETER":        4,
		"EXTERNAL_PARAMETER":        5,
		"INTERNAL_PREDEFINED":       6,
	} {
		mrb.ConstSet(entityDeclCls, mrb.Intern(name), mrb.FixnumValue(val))
	}
	entityDeclCls.DefineMethod("external_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.pubID == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(n.pubID)
	}, mrb.ArgsNone())
	entityDeclCls.DefineMethod("system_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.sysID == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(n.sysID)
	}, mrb.ArgsNone())
	entityDeclCls.DefineMethod("entity_type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.entityType == 0 {
			return mrb.FixnumValue(1)
		}
		return mrb.FixnumValue(n.entityType)
	}, mrb.ArgsNone())
	entityDeclCls.DefineMethod("original_content", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.data)
	}, mrb.ArgsNone())
	entityDeclCls.DefineMethod("content", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.data)
	}, mrb.ArgsNone())

	elemDeclCls := mrb.DefineClassUnder(xmlMod, "ElementDecl", nodeCls)
	rb.DataClass(elemDeclCls)
	elemDeclCls.DefineMethod("__raw_content_spec", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.data)
	}, mrb.ArgsNone())
	elemDeclCls.DefineMethod("prefix", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.prefix == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(n.prefix)
	}, mrb.ArgsNone())
	elemDeclCls.DefineMethod("element_type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		spec := strings.TrimSpace(n.data)
		switch {
		case spec == "EMPTY":
			return mrb.FixnumValue(1)
		case spec == "ANY":
			return mrb.FixnumValue(2)
		case strings.Contains(spec, "#PCDATA"):
			return mrb.FixnumValue(3)
		default:
			return mrb.FixnumValue(4)
		}
	}, mrb.ArgsNone())
	elemDeclCls.DefineMethod("attribute_nodes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNew()
		if n.parent != nil {
			full := n.name
			if n.prefix != "" {
				full = n.prefix + ":" + n.name
			}
			for c := n.parent.first; c != nil; c = c.next {
				if c.kind == AttributeDecl && (c.attrElem == full || c.attrElem == n.name) {
					mrb.AryPush(ary, wrapNode(mrb, c))
				}
			}
		}
		return ary
	}, mrb.ArgsNone())

	attrDeclCls := mrb.DefineClassUnder(xmlMod, "AttributeDecl", nodeCls)
	rb.DataClass(attrDeclCls)
	attrDeclCls.DefineMethod("default", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.data == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(n.data)
	}, mrb.ArgsNone())
	attrDeclCls.DefineMethod("attribute_type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.attrType == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(n.attrType)
	}, mrb.ArgsNone())
	attrDeclCls.DefineMethod("enumeration", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNewCapa(len(n.enumVals))
		for _, v := range n.enumVals {
			mrb.AryPush(ary, mrb.StrNew(v))
		}
		return ary
	}, mrb.ArgsNone())

	dtdCls := mrb.DefineClassUnder(xmlMod, "DTD", nodeCls)
	rb.DataClass(dtdCls)
	docCls := mrb.DefineClassUnder(xmlMod, "Document", nodeCls)
	rb.DataClass(docCls)
	fragCls := mrb.DefineClassUnder(xmlMod, "DocumentFragment", nodeCls)
	rb.DataClass(fragCls)

	nsCls := mrb.DefineClassUnder(xmlMod, "Namespace", mrb.ObjectClass())
	rb.DataClass(nsCls)
	nsSetCls := mrb.DefineClassUnder(xmlMod, "NodeSet", mrb.ObjectClass())
	rb.DataClass(nsSetCls)

	// HTML4 / HTML / HTML5
	html4Mod := mrb.DefineModuleUnder(mod, "HTML4")
	mrb.ConstSet(mod, mrb.Intern("HTML"), html4Mod.Value())
	html4DocCls := mrb.DefineClassUnder(html4Mod, "Document", docCls)
	rb.DataClass(html4DocCls)
	html4FragCls := mrb.DefineClassUnder(html4Mod, "DocumentFragment", fragCls)
	rb.DataClass(html4FragCls)

	html5Mod := mrb.DefineModuleUnder(mod, "HTML5")
	html5DocCls := mrb.DefineClassUnder(html5Mod, "Document", html4DocCls)
	rb.DataClass(html5DocCls)
	html5FragCls := mrb.DefineClassUnder(html5Mod, "DocumentFragment", html4FragCls)
	rb.DataClass(html5FragCls)

	// Node type constants on XML::Node
	for name, val := range map[string]int{
		"ELEMENT_NODE":       ElementNode,
		"ATTRIBUTE_NODE":     AttributeNode,
		"TEXT_NODE":          TextNode,
		"CDATA_SECTION_NODE": CDATASectionNode,
		"ENTITY_REF_NODE":    EntityRefNode,
		"ENTITY_NODE":        EntityNode,
		"PI_NODE":            PINode,
		"COMMENT_NODE":       CommentNode,
		"DOCUMENT_NODE":      DocumentNode,
		"DOCUMENT_TYPE_NODE": DocumentTypeNode,
		"DOCUMENT_FRAG_NODE": DocumentFragNode,
		"NOTATION_NODE":      NotationNode,
		"HTML_DOCUMENT_NODE": HTMLDocumentNode,
		"DTD_NODE":           DTDNode,
		"ELEMENT_DECL":       ElementDecl,
		"ATTRIBUTE_DECL":     AttributeDecl,
		"ENTITY_DECL":        EntityDecl,
		"NAMESPACE_DECL":     NamespaceDecl,
		"XINCLUDE_START":     XIncludeStart,
		"XINCLUDE_END":       XIncludeEnd,
		"DOCB_DOCUMENT_NODE": DocbDocumentNode,
	} {
		mrb.ConstSet(nodeCls, mrb.Intern(name), mrb.FixnumValue(val))
	}

	defineNodeMethods(mrb, nodeCls, elemCls, textCls, cdataCls, commentCls, piCls, attrCls)
	entityRefCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		doc, ok := getDOMNode(mrb, args.Item(0))
		if !ok || (doc.kind != DocumentNode && doc.kind != HTMLDocumentNode) {
			return mrb.EArgumentError().Raise("Document must be a Nokogiri::XML::Document")
		}
		name := mrb.String(args.Item(1))
		rb.Set(mrb, self, newNode(doc, EntityRefNode, name, ""))
		return self
	}, mrb.ArgsReq(2))
	defineDTDMethods(mrb, dtdCls)
	defineDocumentMethods(mrb, docCls, fragCls, html4DocCls, html4FragCls, html5DocCls, html5FragCls)
	defineNamespaceMethods(mrb, nsCls)
	defineNodeSetMethods(mrb, nsSetCls)
	defineSAXAndReader(mrb, xmlMod, html4Mod)
	defineSchemaAndXSLT(mrb, mod, xmlMod)

	rb.LoadRuby(mrb, "<internal:nokogiri>", nokogiriRb)
	return nil
}

// ---------------------------------------------------------------------------
// Wrapping helpers
// ---------------------------------------------------------------------------

func decorateValue(mrb *oruby.MrbState, doc *domNode, val oruby.Value) {
	if doc == nil || !doc.hasDecorators || doc.decoratorsVal.IsNil() {
		return
	}
	// Root the decorators hash on this wrapper so GC keeps it alive as long as any wrapper lives.
	mrb.IVSet(val, mrb.Intern("@__nokogiri_decorators"), doc.decoratorsVal)
	h := doc.decoratorsVal
	keys := mrb.HashKeys(h)
	for i := 0; i < oruby.RArrayLen(keys); i++ {
		klass := mrb.AryRef(keys, i)
		isA, err := mrb.Funcall(val, mrb.Intern("is_a?"), klass)
		if err != nil || !isA.Bool() {
			continue
		}
		mods, err := mrb.Funcall(h, mrb.Intern("[]"), klass)
		if err != nil || !mods.IsArray() {
			continue
		}
		for j := 0; j < oruby.RArrayLen(mods); j++ {
			mod := mrb.AryRef(mods, j)
			_, _ = mrb.Funcall(val, mrb.Intern("extend"), mod)
		}
	}
}

func registerDocNodeVal(mrb *oruby.MrbState, doc *domNode, n *domNode, val oruby.Value) {
	if doc == nil || n == nil || !doc.hasSelfVal || n.kind == AttributeNode || n.kind == NamespaceDecl {
		return
	}
	if n == doc {
		return
	}
	cacheSym := mrb.Intern("@__nodes")
	cache := mrb.IVGet(doc.selfVal, cacheSym)
	if cache.IsNil() || !cache.IsHash() {
		cache = mrb.HashNew().Value()
		mrb.IVSet(doc.selfVal, cacheSym, cache)
	}
	key := mrb.Int64Value(int64(uintptr(unsafe.Pointer(n))))
	mrb.HashSet(cache, key, val)
}

func lookupDocNodeVal(mrb *oruby.MrbState, doc *domNode, n *domNode) (oruby.Value, bool) {
	if doc == nil || n == nil || !doc.hasSelfVal || n.kind == AttributeNode || n.kind == NamespaceDecl {
		return mrb.NilValue(), false
	}
	if n == doc {
		return doc.selfVal, true
	}
	cache := mrb.IVGet(doc.selfVal, mrb.Intern("@__nodes"))
	if cache.IsNil() || !cache.IsHash() {
		return mrb.NilValue(), false
	}
	key := mrb.Int64Value(int64(uintptr(unsafe.Pointer(n))))
	if mrb.HashKeyP(cache, key) {
		v := mrb.HashGet(cache, key)
		if !v.IsNil() {
			return v, true
		}
	}
	return mrb.NilValue(), false
}

func wrapNode(mrb *oruby.MrbState, n *domNode) oruby.Value {
	if n == nil {
		return mrb.NilValue()
	}
	if n.kind == NamespaceDecl {
		ns := &domNS{prefix: n.prefix, href: n.nsURI, doc: n.doc}
		return mrb.DataWrapInterface(rb.ClassPath(mrb, "Nokogiri::XML::Namespace"), ns).Value()
	}
	if existing, ok := lookupDocNodeVal(mrb, n.doc, n); ok {
		return existing
	}
	var cls oruby.RClass
	if n.hasClass {
		cls = n.rbClass
	} else {
		var clsPath string
		switch n.kind {
		case DocumentNode:
			clsPath = "Nokogiri::XML::Document"
		case HTMLDocumentNode:
			if n.isHTML5 {
				clsPath = "Nokogiri::HTML5::Document"
			} else {
				clsPath = "Nokogiri::HTML4::Document"
			}
		case DocumentFragNode:
			if n.isHTML {
				if n.doc != nil && n.doc.isHTML5 {
					clsPath = "Nokogiri::HTML5::DocumentFragment"
				} else {
					clsPath = "Nokogiri::HTML4::DocumentFragment"
				}
			} else {
				clsPath = "Nokogiri::XML::DocumentFragment"
			}
		case ElementNode:
			clsPath = "Nokogiri::XML::Element"
		case AttributeNode:
			clsPath = "Nokogiri::XML::Attr"
		case TextNode:
			clsPath = "Nokogiri::XML::Text"
		case CDATASectionNode:
			clsPath = "Nokogiri::XML::CDATA"
		case EntityRefNode:
			clsPath = "Nokogiri::XML::EntityReference"
		case EntityDecl:
			clsPath = "Nokogiri::XML::EntityDecl"
		case ElementDecl:
			clsPath = "Nokogiri::XML::ElementDecl"
		case AttributeDecl:
			clsPath = "Nokogiri::XML::AttributeDecl"
		case CommentNode:
			clsPath = "Nokogiri::XML::Comment"
		case PINode:
			clsPath = "Nokogiri::XML::ProcessingInstruction"
		case DocumentTypeNode, DTDNode:
			clsPath = "Nokogiri::XML::DTD"
		default:
			clsPath = "Nokogiri::XML::Node"
		}
		cls = rb.ClassPath(mrb, clsPath)
	}
	val := mrb.DataWrapInterface(cls, n).Value()
	if n == n.doc {
		n.selfVal = val
		n.hasSelfVal = true
	} else {
		registerDocNodeVal(mrb, n.doc, n, val)
	}
	if n.doc != nil && n.doc.hasDecorators {
		decorateValue(mrb, n.doc, val)
	}
	return val
}

func wrapAttr(mrb *oruby.MrbState, a *domAttr) oruby.Value {
	if a == nil {
		return mrb.NilValue()
	}
	var doc *domNode
	if a.owner != nil {
		doc = a.owner.doc
	}
	n := &domNode{
		kind:   AttributeNode,
		doc:    doc,
		parent: a.owner,
		name:   a.name,
		prefix: a.prefix,
		nsURI:  a.nsURI,
		data:   a.value,
	}
	return mrb.DataWrapInterface(rb.ClassPath(mrb, "Nokogiri::XML::Attr"), n).Value()
}

func wrapNS(mrb *oruby.MrbState, ns *domNS) oruby.Value {
	if ns == nil {
		return mrb.NilValue()
	}
	if ns.doc != nil && ns.doc.hasSelfVal && !ns.doc.selfVal.IsNil() {
		cache := mrb.IVGet(ns.doc.selfVal, mrb.Intern("@__ns_cache"))
		key := mrb.StrNew(ns.prefix + "\x00" + ns.href)
		if !cache.IsNil() && cache.IsHash() {
			if existing := mrb.HashGet(cache, key); !existing.IsNil() {
				return existing
			}
		} else {
			cache = mrb.HashNew().Value()
			mrb.IVSet(ns.doc.selfVal, mrb.Intern("@__ns_cache"), cache)
		}
		val := mrb.DataWrapInterface(rb.ClassPath(mrb, "Nokogiri::XML::Namespace"), ns).Value()
		mrb.HashSet(cache, key, val)
		return val
	}
	return mrb.DataWrapInterface(rb.ClassPath(mrb, "Nokogiri::XML::Namespace"), ns).Value()
}

type nodeSetData struct {
	doc   *domNode
	nodes []*domNode
}

func wrapNodeSet(mrb *oruby.MrbState, doc *domNode, nodes []*domNode) oruby.Value {
	ns := &nodeSetData{doc: doc, nodes: nodes}
	val := mrb.DataWrapInterface(rb.ClassPath(mrb, "Nokogiri::XML::NodeSet"), ns).Value()
	if doc != nil && doc.hasDecorators {
		decorateValue(mrb, doc, val)
	}
	return val
}

func getDOMNode(mrb *oruby.MrbState, v oruby.Value) (*domNode, bool) {
	return rb.Get[*domNode](mrb, v)
}

func getNodeSet(mrb *oruby.MrbState, v oruby.Value) (*nodeSetData, bool) {
	return rb.Get[*nodeSetData](mrb, v)
}

func parseNodeArg(mrb *oruby.MrbState, doc *domNode, arg oruby.Value) (*domNode, error) {
	if n, ok := getDOMNode(mrb, arg); ok {
		if n.kind == DocumentNode || n.kind == HTMLDocumentNode || n.kind == AttributeNode {
			return nil, oruby.EArgumentError("Requires a Node, NodeSet or String argument, and cannot accept a %s.", mrb.ClassOf(arg).Name())
		}
		return n, nil
	}
	if arg.IsString() {
		if doc != nil && doc.isHTML {
			return parseHTMLFragment(doc, arg.String(), "body", false), nil
		}
		return parseXMLFragment(doc, arg.String(), false, false), nil
	}
	if ns, ok := getNodeSet(mrb, arg); ok {
		frag := newFragment(doc)
		for _, n := range ns.nodes {
			frag.appendChild(n)
		}
		return frag, nil
	}
	return nil, oruby.EArgumentError("Requires a Node, NodeSet or String argument, and cannot accept a %s.", mrb.ClassOf(arg).Name())
}

func makeErrorsArray(mrb *oruby.MrbState, errs []domError) oruby.Value {
	ary := mrb.AryNewCapa(len(errs))
	errCls := rb.ClassPath(mrb, "Nokogiri::XML::SyntaxError")
	for _, e := range errs {
		exc, err := mrb.Funcall(errCls, mrb.Intern("new"), mrb.StrNew(e.message))
		if err == nil {
			mrb.IVSet(exc, mrb.Intern("@line"), mrb.FixnumValue(e.line))
			mrb.IVSet(exc, mrb.Intern("@column"), mrb.FixnumValue(e.column))
			mrb.IVSet(exc, mrb.Intern("@level"), mrb.FixnumValue(e.level))
			if e.file != "" {
				mrb.IVSet(exc, mrb.Intern("@file"), mrb.StrNew(e.file))
			}
			mrb.AryPush(ary, exc)
		}
	}
	return ary.Value()
}

// ---------------------------------------------------------------------------
// Nokogiri::XML::Node methods
// ---------------------------------------------------------------------------

func defineNodeMethods(mrb *oruby.MrbState, nodeCls, elemCls, textCls, cdataCls, commentCls, piCls, attrCls oruby.RClass) {
	// Node.new(name, document) — Nokogiri::XML::Node.new returns XML::Element (see nokogiri.rb)
	nodeCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		name := args.Item(0).String()
		var doc *domNode
		if args.Len() >= 2 {
			d, ok := getDOMNode(mrb, args.Item(1))
			if !ok {
				return mrb.EArgumentError().Raise("Document must be a Nokogiri::XML::Document")
			}
			if d.kind == DocumentNode || d.kind == HTMLDocumentNode {
				doc = d
			} else if d.doc != nil {
				_, _ = mrb.Funcall(mrb.TopSelf(), mrb.Intern("warn"),
					mrb.StrNew("Passing a Node as the second parameter to Node.new is deprecated. Please pass a Document instead."))
				doc = d.doc
			} else {
				return mrb.EArgumentError().Raise("Document must be a Nokogiri::XML::Document")
			}
		}
		if doc == nil {
			doc = newDocument(false, false)
		}
		prefix, local := splitQName(name)
		n := newNode(doc, ElementNode, local, "")
		n.prefix = prefix
		n.rbClass = mrb.ClassOf(self)
		n.hasClass = true
		rb.Set(mrb, self, n)
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	attrCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		d, ok := getDOMNode(mrb, args.Item(0))
		if !ok || (d.kind != DocumentNode && d.kind != HTMLDocumentNode) {
			return mrb.EArgumentError().Raise("Document must be a Nokogiri::XML::Document")
		}
		name := args.Item(1).String()
		prefix, local := splitQName(name)
		n := newNode(d, AttributeNode, local, "")
		n.prefix = prefix
		rb.Set(mrb, self, n)
		return self
	}, mrb.ArgsReq(2))

	textCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		content := args.Item(0).String()
		var doc *domNode
		if args.Len() >= 2 {
			doc, _ = getDOMNode(mrb, args.Item(1))
		}
		rb.Set(mrb, self, newNode(doc, TextNode, "#text", content))
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	cdataCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		var doc *domNode
		var content string
		if args.Len() >= 2 {
			doc, _ = getDOMNode(mrb, args.Item(0))
			if !args.Item(1).IsNil() {
				content = mrb.String(args.Item(1))
			}
		} else if !args.Item(0).IsNil() {
			content = mrb.String(args.Item(0))
		}
		rb.Set(mrb, self, newNode(doc, CDATASectionNode, "#cdata-section", content))
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	commentCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		var doc *domNode
		var content string
		if args.Len() >= 2 {
			doc, _ = getDOMNode(mrb, args.Item(0))
			content = args.Item(1).String()
		} else {
			content = args.Item(0).String()
		}
		rb.Set(mrb, self, newNode(doc, CommentNode, "#comment", content))
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	piCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		doc, _ := getDOMNode(mrb, args.Item(0))
		name := args.Item(1).String()
		content := ""
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			content = args.Item(2).String()
		}
		rb.Set(mrb, self, newNode(doc, PINode, name, content))
		return self
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	// Identity & type
	nodeCls.DefineMethod("node_type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.FixnumValue(n.kind)
	}, mrb.ArgsNone())
	nodeCls.DefineAlias("type", "node_type")

	nodeCls.DefineMethod("pointer_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.Uint64Value(uint64(uintptr(unsafe.Pointer(n))))
	}, mrb.ArgsNone())

	eqFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n1, ok1 := getDOMNode(mrb, self)
		n2, ok2 := getDOMNode(mrb, mrb.GetArgsFirst())
		if !ok1 || !ok2 {
			return mrb.FalseValue()
		}
		if n1 == n2 {
			return mrb.TrueValue()
		}
		if n1.kind == AttributeNode && n2.kind == AttributeNode {
			return mrb.BoolValue(n1.parent == n2.parent && n1.name == n2.name && n1.prefix == n2.prefix)
		}
		return mrb.FalseValue()
	}
	nodeCls.DefineMethod("==", eqFn, mrb.ArgsReq(1))
	nodeCls.DefineMethod("eql?", eqFn, mrb.ArgsReq(1))

	nodeCls.DefineMethod("data_ptr?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, ok := getDOMNode(mrb, self)
		return mrb.BoolValue(ok && n != nil)
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("<=>", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n1, ok1 := getDOMNode(mrb, self)
		n2, ok2 := getDOMNode(mrb, mrb.GetArgsFirst())
		if !ok1 || !ok2 {
			return mrb.NilValue()
		}
		cmp, ok := compareDocOrder(n1, n2)
		if !ok {
			return mrb.NilValue()
		}
		return mrb.FixnumValue(cmp)
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("hash", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.Uint64Value(uint64(uintptr(unsafe.Pointer(n))))
	}, mrb.ArgsNone())

	// Name & line & path
	nameFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		switch n.kind {
		case DocumentNode, HTMLDocumentNode:
			return mrb.StrNew("document")
		case DocumentFragNode:
			return mrb.StrNew("#document-fragment")
		case TextNode:
			return mrb.StrNew("text")
		case CDATASectionNode:
			return mrb.StrNew("#cdata-section")
		case CommentNode:
			return mrb.StrNew("comment")
		default:
			return mrb.StrNew(n.name)
		}
	}
	nodeCls.DefineMethod("name", nameFn, mrb.ArgsNone())
	nodeCls.DefineMethod("node_name", nameFn, mrb.ArgsNone())

	setNameFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		s := mrb.GetArgsFirst().String()
		n.prefix, n.name = splitQName(s)
		return mrb.StrNew(s)
	}
	nodeCls.DefineMethod("name=", setNameFn, mrb.ArgsReq(1))
	nodeCls.DefineMethod("node_name=", setNameFn, mrb.ArgsReq(1))

	nodeCls.DefineMethod("line", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.FixnumValue(n.line)
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("line=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		n.line = v.Int()
		return v
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("path", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.path())
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("css_path", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.cssPath())
	}, mrb.ArgsNone())

	// Content / Text
	textFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.innerText())
	}
	nodeCls.DefineMethod("content", textFn, mrb.ArgsNone())
	nodeCls.DefineMethod("text", textFn, mrb.ArgsNone())
	nodeCls.DefineMethod("inner_text", textFn, mrb.ArgsNone())

	setContentFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		raw := mrb.GetArgsFirst()
		s := ""
		if !raw.IsNil() {
			s = mrb.String(raw)
		}
		switch n.kind {
		case TextNode, CDATASectionNode, CommentNode, PINode:
			n.data = s
		case AttributeNode:
			n.data = s
			if n.parent != nil {
				key := n.name
				if n.prefix != "" {
					key = n.prefix + ":" + n.name
				}
				n.parent.setAttr(key, s)
			}
			return raw
		default:
			n.clearChildren()
			if s != "" {
				n.appendChild(newNode(n.doc, TextNode, "#text", s))
			}
		}
		return mrb.StrNew(s)
	}
	nodeCls.DefineMethod("content=", setContentFn, mrb.ArgsReq(1))
	nodeCls.DefineMethod("native_content=", setContentFn, mrb.ArgsReq(1))

	// Attr#value / value=
	attrCls.DefineMethod("value", textFn, mrb.ArgsNone())
	attrCls.DefineMethod("value=", setContentFn, mrb.ArgsReq(1))

	// Attributes
	getAttrFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		k := mrb.String(mrb.GetArgsFirst())
		if a, ok := n.getAttr(k); ok {
			return mrb.StrNew(a.value)
		}
		return mrb.NilValue()
	}
	nodeCls.DefineMethod("[]", getAttrFn, mrb.ArgsReq(1))
	nodeCls.DefineMethod("get_attribute", getAttrFn, mrb.ArgsReq(1))
	nodeCls.DefineMethod("attr", getAttrFn, mrb.ArgsReq(1))

	setAttrFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		k := mrb.String(args.Item(0))
		v := args.Item(1)
		if v.IsNil() {
			n.removeAttr(k)
			return mrb.NilValue()
		}
		vs := mrb.String(v)
		n.setAttr(k, vs)
		return mrb.StrNew(vs)
	}
	nodeCls.DefineMethod("[]=", setAttrFn, mrb.ArgsReq(2))
	nodeCls.DefineMethod("set_attribute", setAttrFn, mrb.ArgsReq(2))

	nodeCls.DefineMethod("key?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		_, ok := n.getAttr(mrb.String(mrb.GetArgsFirst()))
		return mrb.BoolValue(ok)
	}, mrb.ArgsReq(1))
	nodeCls.DefineAlias("has_attribute?", "key?")

	delAttrFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if val, ok := n.removeAttr(mrb.String(mrb.GetArgsFirst())); ok {
			return mrb.StrNew(val)
		}
		return mrb.NilValue()
	}
	nodeCls.DefineMethod("delete", delAttrFn, mrb.ArgsReq(1))
	nodeCls.DefineMethod("remove_attribute", delAttrFn, mrb.ArgsReq(1))

	nodeCls.DefineMethod("attribute", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if a, ok := n.getAttrByLocalName(mrb.String(mrb.GetArgsFirst())); ok {
			return wrapAttr(mrb, a)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("attribute_with_ns", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		name := mrb.String(args.Item(0))
		nsURI := ""
		if !args.Item(1).IsNil() {
			nsURI = mrb.String(args.Item(1))
		}
		if a, ok := n.getAttrWithNS(name, nsURI); ok {
			return wrapAttr(mrb, a)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(2))

	nodeCls.DefineMethod("namespaced_key?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		name := mrb.String(args.Item(0))
		nsURI := ""
		if !args.Item(1).IsNil() {
			nsURI = mrb.String(args.Item(1))
		}
		_, ok := n.getAttrWithNS(name, nsURI)
		return mrb.BoolValue(ok)
	}, mrb.ArgsReq(2))

	nodeCls.DefineMethod("attribute_nodes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNewCapa(len(n.attrs))
		for _, a := range n.attrs {
			mrb.AryPush(ary, wrapAttr(mrb, a))
		}
		return ary
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("attributes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		h := mrb.HashNewCapa(len(n.attrs))
		for _, a := range n.attrs {
			mrb.HashSet(h, mrb.StrNew(a.name), wrapAttr(mrb, a))
		}
		return h
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("keys", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNewCapa(len(n.attrs))
		for _, a := range n.attrs {
			mrb.AryPush(ary, mrb.StrNew(a.name))
		}
		return ary
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("values", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNewCapa(len(n.attrs))
		for _, a := range n.attrs {
			mrb.AryPush(ary, mrb.StrNew(a.value))
		}
		return ary
	}, mrb.ArgsNone())

	// Tree navigation
	nodeCls.DefineMethod("document", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.doc)
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("parent", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.parent)
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("parent=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			n.unlink()
			return mrb.NilValue()
		}
		p, ok := getDOMNode(mrb, v)
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::Node")
		}
		p.appendChild(n)
		return v
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("children", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		var out []*domNode
		for c := n.first; c != nil; c = c.next {
			out = append(out, c)
		}
		return wrapNodeSet(mrb, n.doc, out)
	}, mrb.ArgsNone())

	elemChildrenFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		var out []*domNode
		for c := n.first; c != nil; c = c.next {
			if c.kind == ElementNode {
				out = append(out, c)
			}
		}
		return wrapNodeSet(mrb, n.doc, out)
	}
	nodeCls.DefineMethod("element_children", elemChildrenFn, mrb.ArgsNone())
	nodeCls.DefineMethod("elements", elemChildrenFn, mrb.ArgsNone())

	nodeCls.DefineMethod("first_element_child", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.firstElementChild())
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("last_element_child", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.lastElementChild())
	}, mrb.ArgsNone())

	nextSibFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.next)
	}
	nodeCls.DefineMethod("next_sibling", nextSibFn, mrb.ArgsNone())
	nodeCls.DefineMethod("next", nextSibFn, mrb.ArgsNone())

	prevSibFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.prev)
	}
	nodeCls.DefineMethod("previous_sibling", prevSibFn, mrb.ArgsNone())
	nodeCls.DefineMethod("previous", prevSibFn, mrb.ArgsNone())

	nodeCls.DefineMethod("next_element", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.nextElement())
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("previous_element", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, n.prevElement())
	}, mrb.ArgsNone())

	// Tree mutations (low-level Go primitives called by Ruby wrappers after coerce/parent validation)
	checkDocMultiRoot := func(parent, child *domNode) bool {
		if parent.kind != DocumentNode && parent.kind != HTMLDocumentNode {
			return false
		}
		existingRoot := parent.rootElement() != nil
		elemCount := 0
		if child.kind == ElementNode {
			elemCount = 1
		} else if child.kind == DocumentFragNode {
			for c := child.first; c != nil; c = c.next {
				if c.kind == ElementNode {
					elemCount++
				}
			}
		}
		return (existingRoot && elemCount > 0) || elemCount > 1
	}

	nodeCls.DefineMethod("__add_child", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		child, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if child.isAncestorOrSelf(n) {
			return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
		}
		if checkDocMultiRoot(n, child) {
			return mrb.ERuntimeError().Raise("Document already has a root node")
		}
		if child.kind == DocumentFragNode {
			var added []*domNode
			for c := child.first; c != nil; {
				nxt := c.next
				if c.isAncestorOrSelf(n) {
					return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
				}
				n.appendChild(c)
				added = append(added, c)
				c = nxt
			}
			return wrapNodeSet(mrb, n.doc, added)
		}
		return wrapNode(mrb, n.appendChild(child))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("__prepend_child", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		child, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if child.isAncestorOrSelf(n) {
			return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
		}
		if checkDocMultiRoot(n, child) {
			return mrb.ERuntimeError().Raise("Document already has a root node")
		}
		if child.kind == DocumentFragNode {
			var added []*domNode
			for c := child.first; c != nil; c = c.next {
				if c.isAncestorOrSelf(n) {
					return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
				}
				added = append(added, c)
			}
			for i := len(added) - 1; i >= 0; i-- {
				n.prependChild(added[i])
			}
			return wrapNodeSet(mrb, n.doc, added)
		}
		return wrapNode(mrb, n.prependChild(child))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("__add_next_sibling", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		sib, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if n.parent != nil && (n.parent.kind == DocumentNode || n.parent.kind == HTMLDocumentNode) && n.kind == ElementNode && sib.kind == ElementNode {
			return mrb.EArgumentError().Raise("cannot add a sibling element to the document root")
		}
		if n.parent != nil && sib.isAncestorOrSelf(n.parent) {
			return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
		}
		if sib.kind == DocumentFragNode {
			var added []*domNode
			anchor := n
			for c := sib.first; c != nil; {
				nxt := c.next
				if n.parent != nil && c.isAncestorOrSelf(n.parent) {
					return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
				}
				anchor.insertAfter(c)
				anchor = c
				added = append(added, c)
				c = nxt
			}
			return wrapNodeSet(mrb, n.doc, added)
		}
		return wrapNode(mrb, n.insertAfter(sib))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("__add_previous_sibling", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		sib, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if n.parent != nil && (n.parent.kind == DocumentNode || n.parent.kind == HTMLDocumentNode) && n.kind == ElementNode && sib.kind == ElementNode {
			return mrb.EArgumentError().Raise("cannot add a sibling element to the document root")
		}
		if n.parent != nil && sib.isAncestorOrSelf(n.parent) {
			return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
		}
		if sib.kind == DocumentFragNode {
			var added []*domNode
			for c := sib.first; c != nil; {
				nxt := c.next
				if n.parent != nil && c.isAncestorOrSelf(n.parent) {
					return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
				}
				n.insertBefore(c)
				added = append(added, c)
				c = nxt
			}
			return wrapNodeSet(mrb, n.doc, added)
		}
		return wrapNode(mrb, n.insertBefore(sib))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("__replace", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		rep, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if n.parent != nil && rep.isAncestorOrSelf(n.parent) {
			return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
		}
		if rep.kind == DocumentFragNode {
			var added []*domNode
			for c := rep.first; c != nil; {
				nxt := c.next
				if n.parent != nil && c.isAncestorOrSelf(n.parent) {
					return mrb.ERuntimeError().Raise("cannot reparent a node into its own descendant")
				}
				n.insertBefore(c)
				added = append(added, c)
				c = nxt
			}
			n.unlink()
			return wrapNodeSet(mrb, n.doc, added)
		}
		return wrapNode(mrb, n.replaceWith(rep))
	}, mrb.ArgsReq(1))

	unlinkFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		n.unlink()
		return self
	}
	nodeCls.DefineMethod("unlink", unlinkFn, mrb.ArgsNone())
	nodeCls.DefineMethod("remove", unlinkFn, mrb.ArgsNone())

	dupFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		deep := true
		args := mrb.GetArgs()
		if args.Len() >= 1 {
			if args.Item(0).IsInt() {
				deep = args.Item(0).Int() != 0
			} else {
				deep = args.Item(0).Bool()
			}
		}
		targetDoc := n.doc
		if args.Len() >= 2 {
			if d, ok := getDOMNode(mrb, args.Item(1)); ok {
				targetDoc = d
			}
		}
		cp := n.clone(deep, targetDoc)
		if clsVal, err := mrb.Funcall(self, mrb.Intern("class")); err == nil && !clsVal.IsNil() {
			cp.rbClass = mrb.ClassPtr(clsVal)
			cp.hasClass = true
		}
		return wrapNode(mrb, cp)
	}
	nodeCls.DefineMethod("dup", dupFn, mrb.ArgsOpt(2))
	nodeCls.DefineMethod("__dup_node", dupFn, mrb.ArgsOpt(2))

	nodeCls.DefineMethod("initialize_copy_with_args", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		other, ok := getDOMNode(mrb, args.Item(0))
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::Node")
		}
		deep := true
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			if args.Item(1).IsInt() {
				deep = args.Item(1).Int() != 0
			} else {
				deep = args.Item(1).Bool()
			}
		}
		targetDoc := other.doc
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			if d, ok := getDOMNode(mrb, args.Item(2)); ok {
				targetDoc = d
			}
		}
		rb.Set(mrb, self, other.clone(deep, targetDoc))
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(2))

	// Namespaces
	nodeCls.DefineMethod("namespace", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.nsURI == "" {
			return mrb.NilValue()
		}
		return wrapNS(mrb, &domNS{prefix: n.prefix, href: n.nsURI, doc: n.doc})
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("namespace=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		updateAttrNS := func(prefix, href string) {
			if n.kind == AttributeNode && n.parent != nil {
				for _, a := range n.parent.attrs {
					if a.name == n.name && (a.prefix == n.prefix || a.nsURI == n.nsURI) {
						a.prefix = prefix
						a.nsURI = href
						break
					}
				}
			}
		}
		if v.IsNil() {
			updateAttrNS("", "")
			n.prefix = ""
			n.nsURI = ""
			return mrb.NilValue()
		}
		ns, ok := rb.Get[*domNS](mrb, v)
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::Namespace")
		}
		if ns.doc != nil && n.doc != nil && ns.doc != n.doc {
			return mrb.EArgumentError().Raise("namespace must be in the same document")
		}
		updateAttrNS(ns.prefix, ns.href)
		n.prefix = ns.prefix
		n.nsURI = ns.href
		return v
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("default_namespace=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.NilValue()
		}
		href := mrb.String(v)
		ns := n.addNamespaceDef("", href)
		if n.prefix == "" {
			n.nsURI = href
		}
		return wrapNS(mrb, ns)
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("namespace_definitions", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNewCapa(len(n.nsDefs))
		for _, ns := range n.nsDefs {
			mrb.AryPush(ary, wrapNS(mrb, ns))
		}
		return ary
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("namespace_scopes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		scopes := n.namespaceScopes()
		ary := mrb.AryNewCapa(len(scopes))
		for _, ns := range scopes {
			mrb.AryPush(ary, wrapNS(mrb, ns))
		}
		return ary
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("add_namespace_definition", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		pref := ""
		if !args.Item(0).IsNil() {
			pref = args.Item(0).String()
		}
		href := args.Item(1).String()
		ns := n.addNamespaceDef(pref, href)
		if pref == n.prefix {
			n.nsURI = href
		}
		return wrapNS(mrb, ns)
	}, mrb.ArgsReq(2))
	nodeCls.DefineAlias("add_namespace", "add_namespace_definition")

	nodeCls.DefineMethod("namespaces", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		m := n.scopeNamespaces()
		h := mrb.HashNewCapa(len(m))
		for k, v := range m {
			mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(v))
		}
		return h
	}, mrb.ArgsNone())

	// Serialization
	nodeCls.DefineMethod("__serialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		asHTML := args.Item(0).Bool()
		indent := args.Item(1).Int()
		xmlDecl := args.Item(2).Bool()
		noEmptyTags := false
		if args.Len() >= 4 {
			noEmptyTags = args.Item(3).Bool()
		}
		preserveNewline := false
		if args.Len() >= 5 {
			preserveNewline = args.Item(4).Bool()
		}
		return mrb.StrNew(n.serializeWithOpts(asHTML, indent, xmlDecl, noEmptyTags, preserveNewline))
	}, mrb.ArgsReq(3)|mrb.ArgsOpt(2))

	nodeCls.DefineMethod("canonicalize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs().Slice()
		mode := xmlC14N10
		if len(args) >= 1 && !args[0].IsNil() {
			if !args[0].IsInt() {
				return mrb.ETypeError().Raise("mode must be an Integer")
			}
			mode = args[0].Int()
		}
		var incl []string
		if len(args) >= 2 && !args[1].IsNil() {
			if !args[1].IsArray() {
				return mrb.ETypeError().Raise("inclusive_namespaces must be an Array")
			}
			for i := 0; i < oruby.RArrayLen(args[1]); i++ {
				s, err := rb.Str(mrb, mrb.AryRef(args[1], i))
				if err != nil {
					return mrb.ETypeError().Raise(err.Error())
				}
				incl = append(incl, s)
			}
		}
		withComments := false
		if len(args) >= 3 && !args[2].IsNil() {
			withComments = args[2].Bool()
		}
		blk := mrb.GetArgsBlock()
		var includeFn func(node, parent *domNode) (bool, error)
		if !blk.IsNil() {
			includeFn = func(node, parent *domNode) (bool, error) {
				v, err := mrb.Funcall(blk, mrb.Intern("call"), wrapNode(mrb, node), wrapNode(mrb, parent))
				if err != nil {
					return false, err
				}
				return v.Bool(), nil
			}
		}
		out, err := n.canonicalizeEx(mode, incl, withComments, includeFn)
		if err != nil {
			return mrb.ERuntimeError().Raise(err.Error())
		}
		return mrb.StrNew(out)
	}, mrb.ArgsOpt(3)|mrb.ArgsBlock())

	nodeCls.DefineMethod("__inner_html", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		preserveNewline := false
		if args.Len() >= 1 {
			preserveNewline = args.Item(0).Bool()
		}
		return mrb.StrNew(n.innerHTMLWithOpts(n.isHTMLDoc(), preserveNewline))
	}, mrb.ArgsOpt(1))

	// XPath & CSS querying
	nodeCls.DefineMethod("__xpath", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		expr := args.Item(0).String()
		// Signature: __xpath(expr, ns_hash = nil, handler = nil, binds_hash = nil)
		nsMap := map[string]string{}
		vars := map[string]any{}
		var handler oruby.Value = mrb.NilValue()

		collectHash := func(h oruby.Value, isVar bool) {
			if h.IsNil() || !h.IsHash() {
				return
			}
			keys := mrb.HashKeys(h)
			for i := 0; i < oruby.RArrayLen(keys); i++ {
				k := mrb.AryRef(keys, i)
				v, err := mrb.Funcall(h, mrb.Intern("[]"), k)
				if err != nil {
					continue
				}
				ks := mrb.String(k)
				if isVar {
					switch v.Type() {
					case oruby.MrbTTTrue:
						vars[ks] = true
					case oruby.MrbTTFalse:
						if v.IsNil() {
							vars[ks] = ""
						} else {
							vars[ks] = false
						}
					case oruby.MrbTTInteger:
						vars[ks] = v.Int64()
					case oruby.MrbTTFloat:
						vars[ks] = v.Float64()
					default:
						vars[ks] = mrb.String(v)
					}
				} else {
					nsMap[ks] = mrb.String(v)
				}
			}
		}

		if args.Len() >= 2 && args.Item(1).IsHash() {
			collectHash(args.Item(1), false)
		}
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			if args.Item(2).IsHash() {
				collectHash(args.Item(2), true)
			} else {
				handler = args.Item(2)
			}
		}
		if args.Len() >= 4 && args.Item(3).IsHash() {
			collectHash(args.Item(3), true)
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			collectHash(kw, true)
		}

		if !handler.IsNil() {
			if hv, handled, err := evalXPathWithHandler(mrb, n, expr, nsMap, vars, handler); err != nil {
				return mrb.RaiseError(err)
			} else if handled {
				return hv
			}
		}

		res, err := evalXPath(n, expr, nsMap, vars)
		if err != nil {
			return rb.Raise(mrb, "Nokogiri::XML::XPath::SyntaxError", "%s", err.Error())
		}
		if res.isSet {
			return wrapNodeSet(mrb, n.doc, res.nodes)
		}
		switch v := res.val.(type) {
		case bool:
			return mrb.BoolValue(v)
		case float64:
			return mrb.FloatValue(v)
		case string:
			return mrb.StrNew(v)
		default:
			return mrb.NilValue()
		}
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

	nodeCls.DefineMethod("do_xinclude", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if err := doXInclude(n, ""); err != nil {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", err.Error())
		}
		return self
	}, mrb.ArgsOpt(1))

	nodeCls.DefineMethod("internal_subset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		d := n.doc
		if d == nil {
			d = n
		}
		for c := d.first; c != nil; c = c.next {
			if c.kind == DocumentTypeNode {
				return wrapNode(mrb, c)
			}
		}
		if d.isHTML && !d.isHTML5 {
			dt := newNode(d, DocumentTypeNode, "html", "")
			dt.pubID = "-//W3C//DTD HTML 4.0 Transitional//EN"
			dt.sysID = "http://www.w3.org/TR/REC-html40/loose.dtd"
			d.prependChild(dt)
			return wrapNode(mrb, dt)
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("external_subset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		d := n.doc
		if d == nil {
			d = n
		}
		for c := d.first; c != nil; c = c.next {
			if c.kind == DTDNode {
				return wrapNode(mrb, c)
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("create_internal_subset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		d := n.doc
		if d == nil {
			d = n
		}
		for c := d.first; c != nil; c = c.next {
			if c.kind == DocumentTypeNode {
				return mrb.ERuntimeError().Raise("Document already has an internal subset")
			}
		}
		args := mrb.GetArgs()
		name := args.Item(0).String()
		pubID, sysID := "", ""
		if !args.Item(1).IsNil() {
			pubID = args.Item(1).String()
		}
		if !args.Item(2).IsNil() {
			sysID = args.Item(2).String()
		}
		dt := newNode(d, DocumentTypeNode, name, "")
		dt.pubID = pubID
		dt.sysID = sysID
		d.prependChild(dt)
		return wrapNode(mrb, dt)
	}, mrb.ArgsReq(3))

	nodeCls.DefineMethod("create_external_subset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		d := n.doc
		if d == nil {
			d = n
		}
		for c := d.first; c != nil; c = c.next {
			if c.kind == DTDNode {
				return mrb.ERuntimeError().Raise("Document already has an external subset")
			}
		}
		args := mrb.GetArgs()
		name := args.Item(0).String()
		pubID, sysID := "", ""
		if !args.Item(1).IsNil() {
			pubID = args.Item(1).String()
		}
		if !args.Item(2).IsNil() {
			sysID = args.Item(2).String()
		}
		dt := newNode(d, DTDNode, name, "")
		dt.pubID = pubID
		dt.sysID = sysID
		d.prependChild(dt)
		return wrapNode(mrb, dt)
	}, mrb.ArgsReq(3))

	nodeCls.DefineMethod("__css", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		sel := args.Item(0).String()
		var nsMap map[string]string
		if args.Len() >= 2 && args.Item(1).IsHash() {
			nsMap = map[string]string{}
			keys := mrb.HashKeys(args.Item(1))
			for i := 0; i < oruby.RArrayLen(keys); i++ {
				k := mrb.AryRef(keys, i)
				if v, err := mrb.Funcall(args.Item(1), mrb.Intern("[]"), k); err == nil {
					nsMap[mrb.String(k)] = mrb.String(v)
				}
			}
		}
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			handler := args.Item(2)
			prefix := ".//"
			if n.kind == DocumentNode || n.kind == HTMLDocumentNode {
				prefix = "//"
			}
			xp, xerr := cssToXPath(sel, prefix)
			if xerr != nil {
				return rb.Raise(mrb, "Nokogiri::CSS::SyntaxError", "%s", xerr.Error())
			}
			hv, handled, err := evalXPathWithHandler(mrb, n, xp, nsMap, nil, handler)
			if err != nil {
				return mrb.RaiseError(err)
			}
			if handled {
				return hv
			}
		}
		nodes, err := queryCSS(n, sel, nsMap)
		if err != nil {
			return rb.Raise(mrb, "Nokogiri::CSS::SyntaxError", "%s", err.Error())
		}
		return wrapNodeSet(mrb, n.doc, nodes)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(2))

	nodeCls.DefineMethod("matches?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		sel := mrb.GetArgsFirst().String()
		if strings.HasPrefix(sel, "/") || strings.HasPrefix(sel, "./") || strings.HasPrefix(sel, "(") {
			if n.doc != nil {
				res, err := evalXPath(n.doc, sel, nil, nil)
				if err == nil && res.isSet {
					for _, cand := range res.nodes {
						if cand == n {
							return mrb.TrueValue()
						}
					}
				}
			}
			return mrb.FalseValue()
		}
		ok, err := matchesCSS(n, sel)
		if err != nil {
			return rb.Raise(mrb, "Nokogiri::CSS::SyntaxError", "%s", err.Error())
		}
		return mrb.BoolValue(ok)
	}, mrb.ArgsReq(1))
}

// ---------------------------------------------------------------------------
// DTD methods
// ---------------------------------------------------------------------------

func defineDTDMethods(mrb *oruby.MrbState, dtdCls oruby.RClass) {
	dtdCls.DefineMethod("external_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.pubID == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(n.pubID)
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("system_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.sysID == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(n.sysID)
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("elements", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		h := mrb.HashNew()
		for c := n.first; c != nil; c = c.next {
			if c.kind == ElementDecl {
				mrb.HashSet(h, mrb.StrNew(c.name), wrapNode(mrb, c))
			}
		}
		return h
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("entities", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		h := mrb.HashNew()
		for c := n.first; c != nil; c = c.next {
			if c.kind == EntityDecl {
				mrb.HashSet(h, mrb.StrNew(c.name), wrapNode(mrb, c))
			}
		}
		return h
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("attributes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		h := mrb.HashNew()
		for c := n.first; c != nil; c = c.next {
			if c.kind == AttributeDecl {
				mrb.HashSet(h, mrb.StrNew(c.name), wrapNode(mrb, c))
			}
		}
		return h
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("keys", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNew()
		for c := n.first; c != nil; c = c.next {
			if c.kind == AttributeDecl {
				mrb.AryPush(ary, mrb.StrNew(c.name))
			}
		}
		return ary
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("notations", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		h := mrb.HashNew()
		notCls := rb.ClassPath(mrb, "Nokogiri::XML::Notation")
		for c := n.first; c != nil; c = c.next {
			if c.kind == NotationNode {
				var pubV, sysV oruby.Value = mrb.NilValue(), mrb.NilValue()
				if c.pubID != "" {
					pubV = mrb.StrNew(c.pubID)
				}
				if c.sysID != "" {
					sysV = mrb.StrNew(c.sysID)
				}
				if obj, err := mrb.Funcall(notCls, mrb.Intern("new"), mrb.StrNew(c.name), pubV, sysV); err == nil {
					mrb.HashSet(h, mrb.StrNew(c.name), obj)
				}
			}
		}
		return h
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("validate", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		target, ok := getDOMNode(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::Document")
		}
		errs := validateAgainstDTD(n.data, target)
		return makeErrorsArray(mrb, errs)
	}, mrb.ArgsReq(1))

	dtdCls.DefineMethod("html_dtd?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.BoolValue(strings.EqualFold(n.name, "html"))
	}, mrb.ArgsNone())

	dtdCls.DefineMethod("html5_dtd?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.BoolValue(strings.EqualFold(n.name, "html") && n.pubID == "" && (n.sysID == "" || strings.EqualFold(n.sysID, "about:legacy-compat")))
	}, mrb.ArgsNone())
}

// ---------------------------------------------------------------------------
// Document & DocumentFragment methods
// ---------------------------------------------------------------------------

func defineDocumentMethods(mrb *oruby.MrbState, docCls, fragCls, html4DocCls, html4FragCls, html5DocCls, html5FragCls oruby.RClass) {
	docCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if existing, ok := getDOMNode(mrb, self); ok && existing != nil && existing.kind == DocumentNode {
			existing.selfVal = self
			existing.hasSelfVal = true
			return self
		}
		args := mrb.GetArgs()
		d := newDocument(false, false)
		d.rbClass = mrb.ClassOf(self)
		d.hasClass = true
		d.selfVal = self
		d.hasSelfVal = true
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			d.version = args.Item(0).String()
		}
		rb.Set(mrb, self, d)
		return self
	}, mrb.ArgsAny())

	html4DocCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if existing, ok := getDOMNode(mrb, self); ok && existing != nil && existing.kind == HTMLDocumentNode && !existing.isHTML5 {
			existing.selfVal = self
			existing.hasSelfVal = true
			return self
		}
		d := newDocument(true, false)
		d.rbClass = mrb.ClassOf(self)
		d.hasClass = true
		d.selfVal = self
		d.hasSelfVal = true
		rb.Set(mrb, self, d)
		return self
	}, mrb.ArgsAny())

	html5DocCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if existing, ok := getDOMNode(mrb, self); ok && existing != nil && existing.kind == HTMLDocumentNode && existing.isHTML5 {
			existing.selfVal = self
			existing.hasSelfVal = true
			return self
		}
		d := newDocument(true, true)
		d.rbClass = mrb.ClassOf(self)
		d.hasClass = true
		d.selfVal = self
		d.hasSelfVal = true
		rb.Set(mrb, self, d)
		return self
	}, mrb.ArgsAny())

	docCls.DefineClassMethod("__parse_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).String()
		url := ""
		if !args.Item(1).IsNil() && args.Item(1).IsString() {
			url = args.Item(1).String()
		}
		enc := ""
		if !args.Item(2).IsNil() {
			if args.Item(2).IsString() {
				enc = args.Item(2).String()
			} else if mrb.RespondTo(args.Item(2), mrb.Intern("name")) {
				if nv, err := mrb.Funcall(args.Item(2), mrb.Intern("name")); err == nil && nv.IsString() {
					enc = nv.String()
				}
			}
		}
		opts := args.Item(3).Int()
		strict := (opts & poRecover) == 0
		noblanks := (opts & poNoBlanks) != 0
		nocdata := (opts & poNoCDATA) != 0

		doc := parseXMLDocument(src, url, enc, strict, noblanks, nocdata)
		if strict && len(doc.errors) > 0 {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", doc.errors[0].message)
		}
		if (opts & poDTDLoad) != 0 {
			var dt *domNode
			hasExt := false
			for c := doc.first; c != nil; c = c.next {
				if c.kind == DocumentTypeNode {
					dt = c
				} else if c.kind == DTDNode {
					hasExt = true
				}
			}
			if dt != nil && !hasExt && (dt.pubID != "" || dt.sysID != "") {
				ext := newNode(doc, DTDNode, dt.name, "")
				ext.pubID = dt.pubID
				ext.sysID = dt.sysID
				dt.insertAfter(ext)
			}
		}
		cls := mrb.ClassPtr(self)
		doc.rbClass = cls
		doc.hasClass = true
		val := mrb.DataWrapInterface(cls, doc).Value()
		if _, err := mrb.Funcall(val, mrb.Intern("initialize")); err != nil {
			return mrb.RaiseError(err)
		}
		return val
	}, mrb.ArgsReq(4))

	html4DocCls.DefineClassMethod("__parse_html", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).String()
		url := ""
		if !args.Item(1).IsNil() && args.Item(1).IsString() {
			url = args.Item(1).String()
		}
		enc := ""
		if !args.Item(2).IsNil() {
			if args.Item(2).IsString() {
				enc = args.Item(2).String()
			} else if mrb.RespondTo(args.Item(2), mrb.Intern("name")) {
				if nv, err := mrb.Funcall(args.Item(2), mrb.Intern("name")); err == nil && nv.IsString() {
					enc = nv.String()
				}
			}
		}
		opts := args.Item(3).Int()
		isHTML5 := args.Item(4).Bool()
		noblanks := (opts & poNoBlanks) != 0
		maxErrors := -1
		if args.Len() >= 6 && !args.Item(5).IsNil() {
			maxErrors = args.Item(5).Int()
		}
		maxTreeDepth := -1
		if args.Len() >= 7 && !args.Item(6).IsNil() {
			maxTreeDepth = args.Item(6).Int()
		}
		parseNoScriptAsText := false
		if args.Len() >= 8 && !args.Item(7).IsNil() {
			parseNoScriptAsText = args.Item(7).Bool()
		}

		doc := parseHTMLDocumentOpts(src, url, enc, isHTML5, noblanks, maxErrors, maxTreeDepth, parseNoScriptAsText)
		if !isHTML5 && (opts&poRecover) == 0 && len(doc.errors) > 0 {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "Parser without recover option encountered error or warning: %s", doc.errors[0].message)
		}
		cls := mrb.ClassPtr(self)
		doc.rbClass = cls
		doc.hasClass = true
		val := mrb.DataWrapInterface(cls, doc).Value()
		if _, err := mrb.Funcall(val, mrb.Intern("initialize")); err != nil {
			return mrb.RaiseError(err)
		}
		return val
	}, mrb.ArgsReq(5)|mrb.ArgsOpt(3))

	quirksModeFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		switch d.quirksMode {
		case "":
			return mrb.NilValue()
		case "quirks":
			return mrb.FixnumValue(1)
		case "limited_quirks":
			return mrb.FixnumValue(2)
		default:
			return mrb.FixnumValue(0)
		}
	}
	html5DocCls.DefineMethod("quirks_mode", quirksModeFn, mrb.ArgsNone())
	html5FragCls.DefineMethod("quirks_mode", quirksModeFn, mrb.ArgsNone())

	html5DocCls.DefineMethod("parse_noscript_content_as_text", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		return mrb.BoolValue(d.parseNoScriptAsText)
	}, mrb.ArgsNone())

	html5DocCls.DefineMethod("parse_noscript_content_as_text=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		d.parseNoScriptAsText = v.Bool()
		d.parseNoScriptSet = true
		return v
	}, mrb.ArgsReq(1))

	fragCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		var doc *domNode
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			doc, _ = getDOMNode(mrb, args.Item(0))
		}
		if doc == nil {
			doc = newDocument(false, false)
		}
		hasTags := args.Len() >= 2 && !args.Item(1).IsNil()
		tags := ""
		if hasTags {
			tags = args.Item(1).String()
		}
		var ctxNode *domNode
		ctxTag := ""
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			if cn, ok := getDOMNode(mrb, args.Item(2)); ok {
				ctxNode = cn
				ctxTag = cn.name
			} else if args.Item(2).IsString() {
				ctxTag = args.Item(2).String()
			}
		}
		defaultOpts := poRecover | poNoNet
		if doc.isHTML {
			defaultOpts = poRecover | poNoError | poNoWarning | poNoNet
		}
		var optArg oruby.Value = mrb.NilValue()
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			optArg = args.Item(3)
		}
		maxErrors := 0
		maxTreeDepth := -1
		hasNoScriptOpt := false
		noScriptOpt := false
		kw := mrb.KeywordArgs().Value()
		if !kw.IsNil() && kw.IsHash() {
			if v := mrb.HashGet(kw, mrb.SymbolValue(mrb.Intern("options"))); !v.IsNil() {
				optArg = v
			}
			if v := mrb.HashGet(kw, mrb.SymbolValue(mrb.Intern("context"))); !v.IsNil() {
				if cn, ok := getDOMNode(mrb, v); ok {
					ctxNode = cn
					ctxTag = cn.name
				} else if v.IsString() {
					ctxTag = v.String()
				} else {
					// Validate ancestor chain names (Nokogiri HTML5 context node duck-type check)
					cur := v
					for !cur.IsNil() {
						if mrb.RespondTo(cur, mrb.Intern("element?")) {
							if isEl, err := mrb.Funcall(cur, mrb.Intern("element?")); err == nil && isEl.Bool() {
								if nm, err := mrb.Funcall(cur, mrb.Intern("name")); err == nil && !nm.IsString() {
									return mrb.ETypeError().Raise("expected String for ancestor element name")
								}
							}
						}
						if mrb.RespondTo(cur, mrb.Intern("parent")) {
							nxt, err := mrb.Funcall(cur, mrb.Intern("parent"))
							if err != nil {
								break
							}
							cur = nxt
						} else {
							break
						}
					}
					if mrb.RespondTo(v, mrb.Intern("name")) {
						if nm, err := mrb.Funcall(v, mrb.Intern("name")); err == nil && nm.IsString() {
							ctxTag = nm.String()
						}
					}
				}
			}
			if v := mrb.HashGet(kw, mrb.SymbolValue(mrb.Intern("max_errors"))); !v.IsNil() {
				maxErrors = v.Int()
			}
			if v := mrb.HashGet(kw, mrb.SymbolValue(mrb.Intern("max_tree_depth"))); !v.IsNil() {
				maxTreeDepth = v.Int()
				if maxTreeDepth == 0 {
					return mrb.EArgumentError().Raise("max_tree_depth must be positive or -1")
				}
			}
			if mrb.HashKeyP(kw, mrb.SymbolValue(mrb.Intern("parse_noscript_content_as_text"))) {
				hasNoScriptOpt = true
				noScriptOpt = mrb.HashGet(kw, mrb.SymbolValue(mrb.Intern("parse_noscript_content_as_text"))).Bool()
			}
		}

		// Build ParseOptions object and yield to block if provided
		var optsVal oruby.Value
		poCls := mrb.ClassGetUnder(mrb.ModuleGetUnder(mrb.ModuleGet("Nokogiri"), "XML"), "ParseOptions")
		if !optArg.IsNil() && mrb.ObjIsKindOf(optArg, poCls) {
			optsVal = optArg
		} else {
			optInt := defaultOpts
			if !optArg.IsNil() {
				if iv, err := mrb.Funcall(optArg, mrb.Intern("to_i")); err == nil {
					optInt = iv.Int()
				}
			}
			var err error
			optsVal, err = mrb.Funcall(poCls.Value(), mrb.Intern("new"), mrb.FixnumValue(optInt))
			if err != nil {
				return mrb.RaiseError(err)
			}
		}
		if blk := mrb.GetArgsBlock(); !blk.IsNil() {
			if _, err := mrb.Funcall(blk, mrb.Intern("call"), optsVal); err != nil {
				return mrb.RaiseError(err)
			}
		}
		mrb.IVSet(self, mrb.Intern("@parse_options"), optsVal)
		optBits := defaultOpts
		if iv, err := mrb.Funcall(optsVal, mrb.Intern("to_i")); err == nil {
			optBits = iv.Int()
		}
		strict := (optBits & poRecover) == 0
		noblanks := (optBits & poNoBlanks) != 0
		nocdata := (optBits & poNoCDATA) != 0

		if doc.isHTML5 && hasNoScriptOpt {
			if doc.parseNoScriptSet && doc.parseNoScriptAsText != noScriptOpt {
				return mrb.EArgumentError().Raise("parse_noscript_content_as_text must match document's setting")
			}
		}
		parseNoScript := doc.parseNoScriptAsText
		if hasNoScriptOpt {
			parseNoScript = noScriptOpt
		}
		var frag *domNode
		if doc.isHTML {
			frag = parseHTMLFragmentOpts(doc, tags, ctxTag, noblanks, maxErrors, maxTreeDepth, parseNoScript)
			if !doc.isHTML5 && strict && len(frag.errors) > 0 {
				return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", frag.errors[0].message)
			}
			if doc.isHTML5 {
				if !hasTags {
					frag.quirksMode = ""
				} else if ctxNode != nil && doc.quirksMode == "quirks" {
					frag.quirksMode = "quirks"
				} else {
					frag.quirksMode = "no_quirks"
				}
				if hasNoScriptOpt && !doc.parseNoScriptSet {
					doc.parseNoScriptAsText = noScriptOpt
					doc.parseNoScriptSet = true
				}
			}
		} else {
			frag = parseXMLFragmentCtx(doc, ctxNode, tags, strict, noblanks, nocdata)
			if strict && len(frag.errors) > 0 {
				return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", frag.errors[0].message)
			}
		}
		frag.rbClass = mrb.ClassOf(self)
		frag.hasClass = true
		rb.Set(mrb, self, frag)
		if doc.hasDecorators {
			decorateValue(mrb, doc, self)
		}
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsKey(0, 1)|mrb.ArgsBlock())

	fragCls.DefineClassMethod("__parse_fragment", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		doc, _ := getDOMNode(mrb, args.Item(0))
		src := args.Item(1).String()
		isHTML := args.Item(2).Bool()
		ctxTag := ""
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			ctxTag = args.Item(3).String()
		}
		var frag *domNode
		if isHTML {
			frag = parseHTMLFragment(doc, src, ctxTag, false)
		} else {
			frag = parseXMLFragment(doc, src, false, false)
		}
		return wrapNode(mrb, frag)
	}, mrb.ArgsReq(3)|mrb.ArgsOpt(1))

	docCls.DefineMethod("root", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		return wrapNode(mrb, d.rootElement())
	}, mrb.ArgsNone())

	docCls.DefineMethod("root=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			if old := d.rootElement(); old != nil {
				old.unlink()
			}
			return mrb.NilValue()
		}
		n, ok := getDOMNode(mrb, v)
		if !ok {
			return mrb.EArgumentError().Raise(fmt.Sprintf("expected Nokogiri::XML::Node but received %s", mrb.ClassOf(v).Name()))
		}
		if n.kind != ElementNode {
			return mrb.ETypeError().Raise("root must be a Nokogiri::XML::Element")
		}
		if old := d.rootElement(); old != nil {
			old.unlink()
		}
		d.appendChild(n)
		return v
	}, mrb.ArgsReq(1))

	docCls.DefineMethod("encoding", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, ok := getDOMNode(mrb, self)
		if !ok || d == nil || d.encoding == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(d.encoding)
	}, mrb.ArgsNone())

	docCls.DefineMethod("encoding=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, ok := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		if !ok || d == nil {
			return v
		}
		if v.IsNil() {
			d.encoding = ""
		} else {
			d.encoding = v.String()
		}
		return v
	}, mrb.ArgsReq(1))

	docCls.DefineMethod("url", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		if d.url == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(d.url)
	}, mrb.ArgsNone())

	docCls.DefineMethod("version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		if d.version == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(d.version)
	}, mrb.ArgsNone())

	docCls.DefineMethod("errors", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if iv := mrb.IVGet(self, mrb.Intern("@errors")); !iv.IsNil() {
			return iv
		}
		d, _ := getDOMNode(mrb, self)
		return makeErrorsArray(mrb, d.errors)
	}, mrb.ArgsNone())

	docCls.DefineMethod("errors=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		mrb.IVSet(self, mrb.Intern("@errors"), v)
		return v
	}, mrb.ArgsReq(1))

	docCls.DefineMethod("decorators", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		klass := mrb.GetArgsFirst()
		if !d.hasDecorators || d.decoratorsVal.IsNil() {
			d.decoratorsVal = mrb.HashNew().Value()
			d.hasDecorators = true
			mrb.IVSet(self, mrb.Intern("@__nokogiri_decorators"), d.decoratorsVal)
		}
		cur, err := mrb.Funcall(d.decoratorsVal, mrb.Intern("[]"), klass)
		if err != nil || cur.IsNil() {
			ary := mrb.AryNew()
			_, _ = mrb.Funcall(d.decoratorsVal, mrb.Intern("[]="), klass, ary)
			return ary.Value()
		}
		return cur
	}, mrb.ArgsReq(1))

	docCls.DefineMethod("decorate", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		nodeVal := mrb.GetArgsFirst()
		if d.hasDecorators {
			decorateValue(mrb, d, nodeVal)
		}
		return nodeVal
	}, mrb.ArgsReq(1))

	fragCls.DefineMethod("errors", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if iv := mrb.IVGet(self, mrb.Intern("@errors")); !iv.IsNil() {
			return iv
		}
		f, _ := getDOMNode(mrb, self)
		return makeErrorsArray(mrb, f.errors)
	}, mrb.ArgsNone())

	fragCls.DefineMethod("errors=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		mrb.IVSet(self, mrb.Intern("@errors"), v)
		return v
	}, mrb.ArgsReq(1))

	docCls.DefineMethod("collect_namespaces", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		m := map[string]string{}
		d.collectNamespaces(m)
		h := mrb.HashNewCapa(len(m))
		for k, v := range m {
			mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(v))
		}
		return h
	}, mrb.ArgsNone())

	docCls.DefineMethod("remove_namespaces!", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		d.removeNamespacesRecursive()
		return self
	}, mrb.ArgsNone())

	docCls.DefineMethod("create_entity", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		name := args.Item(0).String()
		entType := 1
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			entType = args.Item(1).Int()
		}
		pubID, sysID, content := "", "", ""
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			pubID = args.Item(2).String()
		}
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			sysID = args.Item(3).String()
		}
		if args.Len() >= 5 && !args.Item(4).IsNil() {
			content = args.Item(4).String()
		}
		ent := newNode(d, EntityDecl, name, content)
		ent.entityType = entType
		ent.pubID = pubID
		ent.sysID = sysID
		return wrapNode(mrb, ent)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(4))

	// Suppress unused warning on html4/html5 fragment classes
	_ = html4FragCls
	_ = html5FragCls
}

// ---------------------------------------------------------------------------
// Namespace & NodeSet methods
// ---------------------------------------------------------------------------

func defineNamespaceMethods(mrb *oruby.MrbState, nsCls oruby.RClass) {
	nsCls.DefineMethod("prefix", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := rb.Get[*domNS](mrb, self)
		if ns.prefix == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(ns.prefix)
	}, mrb.ArgsNone())

	nsCls.DefineMethod("href", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := rb.Get[*domNS](mrb, self)
		return mrb.StrNew(ns.href)
	}, mrb.ArgsNone())

	nsCls.DefineMethod("document", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := rb.Get[*domNS](mrb, self)
		return wrapNode(mrb, ns.doc)
	}, mrb.ArgsNone())

	nsCls.DefineMethod("inspect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := rb.Get[*domNS](mrb, self)
		if ns.prefix == "" {
			return mrb.StrNew(fmt.Sprintf("#<Nokogiri::XML::Namespace:0x%x href=%q>", uintptr(unsafe.Pointer(ns)), ns.href))
		}
		return mrb.StrNew(fmt.Sprintf("#<Nokogiri::XML::Namespace:0x%x prefix=%q href=%q>", uintptr(unsafe.Pointer(ns)), ns.prefix, ns.href))
	}, mrb.ArgsNone())

	nsEq := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns1, ok1 := rb.Get[*domNS](mrb, self)
		ns2, ok2 := rb.Get[*domNS](mrb, mrb.GetArgsFirst())
		if !ok1 || !ok2 {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(ns1.prefix == ns2.prefix && ns1.href == ns2.href)
	}
	nsCls.DefineMethod("==", nsEq, mrb.ArgsReq(1))
	nsCls.DefineMethod("eql?", nsEq, mrb.ArgsReq(1))
}

func defineNodeSetMethods(mrb *oruby.MrbState, cls oruby.RClass) {
	mrb.IncludeModule(cls, mrb.ModuleGet("Enumerable"))

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		var doc *domNode
		var nodes []*domNode
		if args.Len() >= 1 {
			doc, _ = getDOMNode(mrb, args.Item(0))
		}
		if args.Len() >= 2 && args.Item(1).IsArray() {
			a := args.Item(1)
			for i := 0; i < oruby.RArrayLen(a); i++ {
				if n, ok := getDOMNode(mrb, mrb.AryRef(a, i)); ok {
					nodes = append(nodes, n)
				}
			}
		}
		rb.Set(mrb, self, &nodeSetData{doc: doc, nodes: nodes})
		return self
	}, mrb.ArgsOpt(2))

	cls.DefineMethod("initialize_copy", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		orig := mrb.GetArgsFirst()
		ons, ok := getNodeSet(mrb, orig)
		if !ok || ons == nil {
			rb.Set(mrb, self, &nodeSetData{})
			return self
		}
		cp := make([]*domNode, len(ons.nodes))
		copy(cp, ons.nodes)
		rb.Set(mrb, self, &nodeSetData{doc: ons.doc, nodes: cp})
		return self
	}, mrb.ArgsReq(1))

	cls.DefineMethod("document", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		return wrapNode(mrb, ns.doc)
	}, mrb.ArgsNone())

	cls.DefineMethod("document=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			ns.doc = nil
			return mrb.NilValue()
		}
		d, ok := getDOMNode(mrb, v)
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::Document")
		}
		ns.doc = d
		return v
	}, mrb.ArgsReq(1))

	lenFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		return mrb.FixnumValue(len(ns.nodes))
	}
	cls.DefineMethod("length", lenFn, mrb.ArgsNone())
	cls.DefineMethod("size", lenFn, mrb.ArgsNone())

	cls.DefineMethod("empty?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		return mrb.BoolValue(len(ns.nodes) == 0)
	}, mrb.ArgsNone())

	cls.DefineMethod("[]", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		args := mrb.GetArgs()
		n := len(ns.nodes)
		if args.Len() == 1 {
			a0 := args.Item(0)
			if a0.IsInt() {
				idx := a0.Int()
				if idx < 0 {
					idx += n
				}
				if idx < 0 || idx >= n {
					return mrb.NilValue()
				}
				return wrapNode(mrb, ns.nodes[idx])
			}
			// Range slice
			if a0.Type() == oruby.MrbTTRange {
				beg := mrb.Call(a0, "begin").Int()
				end := mrb.Call(a0, "end").Int()
				excl := mrb.Call(a0, "exclude_end?").Bool()
				if beg < 0 {
					beg += n
				}
				if end < 0 {
					end += n
				}
				if !excl {
					end++
				}
				if beg < 0 || beg > n {
					return mrb.NilValue()
				}
				if end > n {
					end = n
				}
				if end < beg {
					end = beg
				}
				cp := append([]*domNode(nil), ns.nodes[beg:end]...)
				return wrapNodeSet(mrb, ns.doc, cp)
			}
			return mrb.ETypeError().Raise(fmt.Sprintf("no implicit conversion of %s into Integer", mrb.ClassOf(a0).Name()))
		}
		if args.Len() >= 2 {
			start := args.Item(0).Int()
			cnt := args.Item(1).Int()
			if start < 0 {
				start += n
			}
			if start < 0 || start > n || cnt < 0 {
				return mrb.NilValue()
			}
			end := start + cnt
			if end > n {
				end = n
			}
			cp := append([]*domNode(nil), ns.nodes[start:end]...)
			return wrapNodeSet(mrb, ns.doc, cp)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))
	cls.DefineAlias("slice", "[]")

	cls.DefineMethod("dup", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		cp := make([]*domNode, len(ns.nodes))
		copy(cp, ns.nodes)
		return wrapNodeSet(mrb, ns.doc, cp)
	}, mrb.ArgsNone())

	pushFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		arg := mrb.GetArgsFirst()
		if n, ok := getDOMNode(mrb, arg); ok {
			ns.nodes = append(ns.nodes, n)
			return self
		}
		if nsObj, ok := rb.Get[*domNS](mrb, arg); ok {
			ns.nodes = append(ns.nodes, &domNode{
				kind:   NamespaceDecl,
				doc:    nsObj.doc,
				prefix: nsObj.prefix,
				nsURI:  nsObj.href,
			})
			return self
		}
		return mrb.EArgumentError().Raise("expected Nokogiri::XML::Node")
	}
	cls.DefineMethod("push", pushFn, mrb.ArgsReq(1))
	cls.DefineMethod("<<", pushFn, mrb.ArgsReq(1))

	cls.DefineMethod("pop", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		if len(ns.nodes) == 0 {
			return mrb.NilValue()
		}
		last := ns.nodes[len(ns.nodes)-1]
		ns.nodes = ns.nodes[:len(ns.nodes)-1]
		return wrapNode(mrb, last)
	}, mrb.ArgsNone())

	cls.DefineMethod("shift", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		if len(ns.nodes) == 0 {
			return mrb.NilValue()
		}
		first := ns.nodes[0]
		ns.nodes = ns.nodes[1:]
		return wrapNode(mrb, first)
	}, mrb.ArgsNone())

	cls.DefineMethod("delete", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		arg := mrb.GetArgsFirst()
		if target, ok := getDOMNode(mrb, arg); ok {
			for i, n := range ns.nodes {
				if n == target {
					ns.nodes = append(ns.nodes[:i], ns.nodes[i+1:]...)
					return wrapNode(mrb, target)
				}
			}
			return mrb.NilValue()
		}
		if nsTarget, ok := rb.Get[*domNS](mrb, arg); ok {
			for i, n := range ns.nodes {
				if n.kind == NamespaceDecl && n.prefix == nsTarget.prefix && n.nsURI == nsTarget.href {
					ns.nodes = append(ns.nodes[:i], ns.nodes[i+1:]...)
					return wrapNode(mrb, n)
				}
			}
			return mrb.NilValue()
		}
		return mrb.EArgumentError().Raise("expected Nokogiri::XML::Node")
	}, mrb.ArgsReq(1))

	cls.DefineMethod("include?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		arg := mrb.GetArgsFirst()
		if target, ok := getDOMNode(mrb, arg); ok {
			for _, n := range ns.nodes {
				if n == target {
					return mrb.TrueValue()
				}
			}
			return mrb.FalseValue()
		}
		if nsObj, ok := rb.Get[*domNS](mrb, arg); ok {
			for _, n := range ns.nodes {
				if n.kind == NamespaceDecl && n.prefix == nsObj.prefix && n.nsURI == nsObj.href {
					return mrb.TrueValue()
				}
			}
		}
		return mrb.FalseValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("index", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		if args.Len() == 0 {
			if blk.IsNil() {
				return mrb.NilValue()
			}
			for i, n := range ns.nodes {
				res, err := mrb.Funcall(blk, mrb.Intern("call"), wrapNode(mrb, n))
				if err != nil {
					return mrb.RaiseError(err)
				}
				if res.Bool() {
					return mrb.FixnumValue(i)
				}
			}
			return mrb.NilValue()
		}
		arg := args.Item(0)
		if target, ok := getDOMNode(mrb, arg); ok {
			for i, n := range ns.nodes {
				if n == target {
					return mrb.FixnumValue(i)
				}
			}
			return mrb.NilValue()
		}
		if nsObj, ok := rb.Get[*domNS](mrb, arg); ok {
			for i, n := range ns.nodes {
				if n.kind == NamespaceDecl && n.prefix == nsObj.prefix && n.nsURI == nsObj.href {
					return mrb.FixnumValue(i)
				}
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsOpt(1)|mrb.ArgsBlock())

	cls.DefineMethod("|", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns1, _ := getNodeSet(mrb, self)
		ns2, ok := getNodeSet(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.EArgumentError().Raise("expected Nokogiri::XML::NodeSet")
		}
		seen := map[*domNode]bool{}
		out := make([]*domNode, 0, len(ns1.nodes)+len(ns2.nodes))
		for _, n := range ns1.nodes {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
		for _, n := range ns2.nodes {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
		return wrapNodeSet(mrb, ns1.doc, out)
	}, mrb.ArgsReq(1))
	cls.DefineAlias("+", "|")

	cls.DefineMethod("&", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns1, _ := getNodeSet(mrb, self)
		ns2, ok := getNodeSet(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.EArgumentError().Raise("expected Nokogiri::XML::NodeSet")
		}
		in2 := map[*domNode]bool{}
		for _, n := range ns2.nodes {
			in2[n] = true
		}
		var out []*domNode
		for _, n := range ns1.nodes {
			if in2[n] {
				out = append(out, n)
			}
		}
		return wrapNodeSet(mrb, ns1.doc, out)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("-", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns1, _ := getNodeSet(mrb, self)
		ns2, ok := getNodeSet(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.EArgumentError().Raise("expected Nokogiri::XML::NodeSet")
		}
		in2 := map[*domNode]bool{}
		for _, n := range ns2.nodes {
			in2[n] = true
		}
		var out []*domNode
		for _, n := range ns1.nodes {
			if !in2[n] {
				out = append(out, n)
			}
		}
		return wrapNodeSet(mrb, ns1.doc, out)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("reverse", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		out := make([]*domNode, len(ns.nodes))
		for i, n := range ns.nodes {
			out[len(ns.nodes)-1-i] = n
		}
		return wrapNodeSet(mrb, ns.doc, out)
	}, mrb.ArgsNone())

	cls.DefineMethod("==", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns1, _ := getNodeSet(mrb, self)
		ns2, ok := getNodeSet(mrb, mrb.GetArgsFirst())
		if !ok || len(ns1.nodes) != len(ns2.nodes) {
			return mrb.FalseValue()
		}
		for i := range ns1.nodes {
			if ns1.nodes[i] != ns2.nodes[i] {
				return mrb.FalseValue()
			}
		}
		return mrb.TrueValue()
	}, mrb.ArgsReq(1))
}

// ---------------------------------------------------------------------------
// SAX & XML::Reader
// ---------------------------------------------------------------------------

func defineSAXAndReader(mrb *oruby.MrbState, xmlMod, html4Mod oruby.RClass) {
	saxMod := mrb.DefineModuleUnder(xmlMod, "SAX")
	saxMod.DefineClassMethod("__emit", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		handler := args.Item(0)
		src := args.Item(1).String()
		isHTML := args.Item(2).Bool()
		recovery := true
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			recovery = args.Item(3).Bool()
		}
		var ctxVal oruby.Value = mrb.NilValue()
		if args.Len() >= 5 {
			ctxVal = args.Item(4)
		}
		var doc *domNode
		if isHTML {
			doc = parseHTMLDocument(src, "", "UTF-8", false, false)
		} else {
			doc = parseXMLDocumentOpts(src, "", "UTF-8", false, !recovery, false, false)
		}
		if err := emitSAXEvents(mrb, handler, doc, ctxVal); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(3)|mrb.ArgsOpt(2))

	saxMod.DefineClassMethod("__emit_stream", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		handler := args.Item(0)
		src := args.Item(1).String()
		if err := emitStreamingXMLSAX(mrb, handler, src); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(2))

	pushStateCls := rb.DataClass(mrb.DefineClassUnder(saxMod, "PushState", mrb.ObjectClass()))
	pushStateCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		rb.Set(mrb, self, newXMLPushScanner())
		return self
	}, mrb.ArgsNone())
	pushStateCls.DefineMethod("write", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st, _ := rb.Get[*xmlPushScanner](mrb, self)
		args := mrb.GetArgs()
		if err := st.write(mrb, args.Item(0), args.Item(1).String()); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(2))
	pushStateCls.DefineMethod("finish", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st, _ := rb.Get[*xmlPushScanner](mrb, self)
		if err := st.finish(mrb, mrb.GetArgsFirst()); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	_ = mrb.DefineModuleUnder(html4Mod, "SAX")

	// XML::Reader
	readerCls := mrb.DefineClassUnder(xmlMod, "Reader", mrb.ObjectClass())
	rb.DataClass(readerCls)
	mrb.IncludeModule(readerCls, mrb.ModuleGet("Enumerable"))

	for name, val := range map[string]int{
		"TYPE_NONE":                   readerTypeNone,
		"TYPE_ELEMENT":                readerTypeElement,
		"TYPE_ATTRIBUTE":              readerTypeAttribute,
		"TYPE_TEXT":                   readerTypeText,
		"TYPE_CDATA":                  readerTypeCDATA,
		"TYPE_ENTITY_REFERENCE":       5,
		"TYPE_ENTITY":                 6,
		"TYPE_PROCESSING_INSTRUCTION": readerTypeProcessingInstruction,
		"TYPE_COMMENT":                readerTypeComment,
		"TYPE_DOCUMENT":               readerTypeDocument,
		"TYPE_DOCUMENT_TYPE":          readerTypeDocumentType,
		"TYPE_DOCUMENT_FRAGMENT":      11,
		"TYPE_NOTATION":               12,
		"TYPE_WHITESPACE":             13,
		"TYPE_SIGNIFICANT_WHITESPACE": readerTypeSignificantWhitespace,
		"TYPE_END_ELEMENT":            readerTypeEndElement,
		"TYPE_END_ENTITY":             16,
		"TYPE_XML_DECLARATION":        17,
	} {
		mrb.ConstSet(readerCls, mrb.Intern(name), mrb.FixnumValue(val))
	}

	readerCls.DefineClassMethod("from_memory", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		srcArg := args.Item(0)
		if srcArg.IsNil() {
			return mrb.EArgumentError().Raise("string cannot be nil")
		}
		var src string
		if srcArg.IsString() {
			src = srcArg.String()
		} else if mrb.RespondTo(srcArg, mrb.Intern("to_str")) {
			sv, err := mrb.Funcall(srcArg, mrb.Intern("to_str"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			if !sv.IsString() {
				return mrb.ETypeError().Raise("to_str did not return String")
			}
			src = sv.String()
		} else {
			return mrb.ETypeError().Raise("expected String")
		}
		url := ""
		if args.Len() >= 2 && !args.Item(1).IsNil() && args.Item(1).IsString() {
			url = args.Item(1).String()
		}
		enc := ""
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			if args.Item(2).IsString() {
				enc = args.Item(2).String()
			} else if mrb.RespondTo(args.Item(2), mrb.Intern("name")) {
				if nv, err := mrb.Funcall(args.Item(2), mrb.Intern("name")); err == nil && nv.IsString() {
					enc = nv.String()
				}
			}
		}
		optBits := poNoNet
		poCls := mrb.ClassGetUnder(mrb.ModuleGetUnder(mrb.ModuleGet("Nokogiri"), "XML"), "ParseOptions")
		var optsVal oruby.Value
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			if mrb.ObjIsKindOf(args.Item(3), poCls) {
				optsVal = args.Item(3)
			} else if iv, err := mrb.Funcall(args.Item(3), mrb.Intern("to_i")); err == nil {
				optBits = iv.Int()
			}
		}
		if optsVal.IsNil() {
			if v, err := mrb.Funcall(poCls.Value(), mrb.Intern("new"), mrb.FixnumValue(optBits)); err == nil {
				optsVal = v
			}
		}
		if blk := mrb.GetArgsBlock(); !blk.IsNil() {
			if _, err := mrb.Funcall(blk, mrb.Intern("call"), optsVal); err != nil {
				return mrb.RaiseError(err)
			}
		}
		if !optsVal.IsNil() {
			if iv, err := mrb.Funcall(optsVal, mrb.Intern("to_i")); err == nil {
				optBits = iv.Int()
			}
		}
		noblanks := (optBits & poNoBlanks) != 0
		nocdata := (optBits & poNoCDATA) != 0
		doc := parseXMLDocument(src, url, enc, false, noblanks, nocdata)
		r := buildReaderEvents(doc)
		r.source = src
		r.url = url
		if enc != "" {
			r.encoding = enc
		} else if doc.xmlDeclEncoding != "" {
			r.encoding = doc.xmlDeclEncoding
		} else {
			r.encoding = "UTF-8"
		}
		r.recover = (optBits & poRecover) != 0
		return mrb.DataWrapInterface(readerCls, r).Value()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())

	getR := func(mrb *oruby.MrbState, self oruby.Value) *xmlReader {
		r, _ := rb.Get[*xmlReader](mrb, self)
		return r
	}
	curEv := func(r *xmlReader) *readerEvent {
		if r == nil || r.idx < 0 || r.idx >= len(r.events) {
			return nil
		}
		return &r.events[r.idx]
	}

	readerCls.DefineMethod("__set_source", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		v := mrb.GetArgsFirst()
		if r != nil {
			r.sourceVal = v
			r.hasSourceVal = true
			mrb.IVSet(self, mrb.Intern("@__reader_source"), v)
		}
		return v
	}, mrb.ArgsReq(1))

	readerCls.DefineMethod("read", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r.idx+1 >= len(r.events) {
			r.idx = len(r.events)
			if !r.recover && r.doc != nil && len(r.doc.errors) > 0 {
				return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", r.doc.errors[0].message)
			}
			return mrb.NilValue()
		}
		r.idx++
		return self
	}, mrb.ArgsNone())

	readerCls.DefineMethod("each", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		blk := mrb.GetArgsBlock()
		if blk.IsNil() {
			return mrb.Call(self, "to_enum", mrb.Intern("each").Value())
		}
		r := getR(mrb, self)
		for r.idx+1 < len(r.events) {
			r.idx++
			if _, err := mrb.YieldE(blk, self); err != nil {
				return mrb.RaiseError(err)
			}
		}
		r.idx = len(r.events)
		if !r.recover && r.doc != nil && len(r.doc.errors) > 0 {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", r.doc.errors[0].message)
		}
		return self
	}, mrb.ArgsBlock())

	readerCls.DefineMethod("node_type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil {
			return mrb.FixnumValue(readerTypeNone)
		}
		return mrb.FixnumValue(ev.nodeType)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("name", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil {
			return mrb.NilValue()
		}
		if ev.node.prefix != "" {
			return mrb.StrNew(ev.node.prefix + ":" + ev.node.name)
		}
		return mrb.StrNew(ev.node.name)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("local_name", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil {
			return mrb.NilValue()
		}
		return mrb.StrNew(ev.node.name)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("value", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil {
			return mrb.NilValue()
		}
		if ev.nodeType == readerTypeElement || ev.nodeType == readerTypeEndElement {
			return mrb.NilValue()
		}
		return mrb.StrNew(ev.node.data)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("depth", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(ev.depth)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("self_closing?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		return mrb.BoolValue(ev != nil && ev.selfClosing)
	}, mrb.ArgsNone())
	readerCls.DefineAlias("empty_element?", "self_closing?")

	readerCls.DefineMethod("attribute", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		arg := mrb.GetArgsFirst()
		if ev == nil || ev.node == nil || arg.IsNil() {
			return mrb.NilValue()
		}
		name := arg.String()
		if a, ok := ev.node.getAttr(name); ok {
			return mrb.StrNew(a.value)
		}
		for _, ns := range ev.node.nsDefs {
			k := "xmlns"
			if ns.prefix != "" {
				k = "xmlns:" + ns.prefix
			}
			if k == name {
				return mrb.StrNew(ns.href)
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	readerCls.DefineMethod("attributes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		h := mrb.HashNew()
		if ev != nil && ev.node != nil && (ev.nodeType == readerTypeElement || ev.nodeType == readerTypeEndElement) {
			for _, ns := range ev.node.nsDefs {
				k := "xmlns"
				if ns.prefix != "" {
					k = "xmlns:" + ns.prefix
				}
				mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(ns.href))
			}
			for _, a := range ev.node.attrs {
				k := a.name
				if a.prefix != "" {
					k = a.prefix + ":" + a.name
				}
				mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(a.value))
			}
		}
		return h
	}, mrb.ArgsNone())

	readerCls.DefineMethod("attribute_hash", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		h := mrb.HashNew()
		if ev != nil && ev.node != nil && (ev.nodeType == readerTypeElement || ev.nodeType == readerTypeEndElement) {
			for _, a := range ev.node.attrs {
				k := a.name
				if a.prefix != "" {
					k = a.prefix + ":" + a.name
				}
				mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(a.value))
			}
		}
		return h
	}, mrb.ArgsNone())

	readerCls.DefineMethod("attribute_count", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil || ev.nodeType != readerTypeElement {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(len(ev.node.attrs) + len(ev.node.nsDefs))
	}, mrb.ArgsNone())

	readerCls.DefineMethod("attribute_at", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		arg := mrb.GetArgsFirst()
		if ev == nil || ev.node == nil || arg.IsNil() {
			return mrb.NilValue()
		}
		idx := arg.Int()
		if idx < 0 {
			return mrb.NilValue()
		}
		if idx < len(ev.node.attrs) {
			return mrb.StrNew(ev.node.attrs[idx].value)
		}
		nsIdx := idx - len(ev.node.attrs)
		if nsIdx < len(ev.node.nsDefs) {
			return mrb.StrNew(ev.node.nsDefs[nsIdx].href)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	readerCls.DefineMethod("attributes?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		return mrb.BoolValue(ev != nil && ev.node != nil && (len(ev.node.attrs)+len(ev.node.nsDefs)) > 0)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("namespace_uri", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil || ev.node.nsURI == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(ev.node.nsURI)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("prefix", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil || ev.node.prefix == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(ev.node.prefix)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("namespaces", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		h := mrb.HashNew()
		if ev != nil && ev.node != nil && (ev.nodeType == readerTypeElement || ev.nodeType == readerTypeEndElement) {
			for _, ns := range ev.node.nsDefs {
				k := "xmlns"
				if ns.prefix != "" {
					k = "xmlns:" + ns.prefix
				}
				mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(ns.href))
			}
		}
		return h
	}, mrb.ArgsNone())

	readerCls.DefineMethod("encoding", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r == nil || r.encoding == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(r.encoding)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("base_uri", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		ev := curEv(r)
		if r == nil || ev == nil || ev.node == nil {
			return mrb.NilValue()
		}
		u := nodeBaseURI(ev.node, r.url)
		if u == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(u)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("source", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r == nil {
			return mrb.StrNew("")
		}
		if r.hasSourceVal {
			return r.sourceVal
		}
		return mrb.StrNew(r.source)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("xml_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r == nil || r.idx < 0 || r.doc == nil || r.doc.version == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(r.doc.version)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("lang", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil {
			return mrb.NilValue()
		}
		for cur := ev.node; cur != nil; cur = cur.parent {
			if a, ok := cur.getAttr("xml:lang"); ok {
				return mrb.StrNew(a.value)
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	readerCls.DefineMethod("default?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FalseValue()
	}, mrb.ArgsNone())

	readerCls.DefineMethod("value?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil {
			return mrb.FalseValue()
		}
		if ev.nodeType == readerTypeElement || ev.nodeType == readerTypeEndElement {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(ev.node.data != "")
	}, mrb.ArgsNone())

	readerCls.DefineMethod("state", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r == nil || r.idx < 0 {
			return mrb.FixnumValue(0)
		}
		if r.idx >= len(r.events) {
			return mrb.FixnumValue(3) // EOF
		}
		return mrb.FixnumValue(1) // reading
	}, mrb.ArgsNone())

	readerCls.DefineMethod("inner_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		ev := curEv(r)
		if r != nil && !r.recover && r.doc != nil && len(r.doc.errors) > 0 {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", r.doc.errors[0].message)
		}
		if ev == nil || ev.node == nil {
			return mrb.StrNew("")
		}
		return mrb.StrNew(ev.node.innerHTML(false))
	}, mrb.ArgsNone())

	readerCls.DefineMethod("outer_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		ev := curEv(r)
		if r != nil && !r.recover && r.doc != nil && len(r.doc.errors) > 0 {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", r.doc.errors[0].message)
		}
		if ev == nil || ev.node == nil {
			return mrb.StrNew("")
		}
		if ev.nodeType == readerTypeEndElement {
			tag := ev.node.name
			if ev.node.prefix != "" {
				tag = ev.node.prefix + ":" + ev.node.name
			}
			return mrb.StrNew("<" + tag + "/>")
		}
		return mrb.StrNew(ev.node.serialize(false, 0, false))
	}, mrb.ArgsNone())

	readerCls.DefineMethod("errors", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r == nil {
			return mrb.AryNew()
		}
		if r.hasCustomErr {
			return r.customErrs
		}
		if r.doc == nil {
			return mrb.AryNew()
		}
		return makeErrorsArray(mrb, r.doc.errors)
	}, mrb.ArgsNone())

	readerCls.DefineMethod("errors=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		v := mrb.GetArgsFirst()
		if r != nil {
			r.hasCustomErr = true
			r.customErrs = v
			mrb.IVSet(self, mrb.Intern("@__reader_errors"), v)
		}
		return v
	}, mrb.ArgsReq(1))

	// Suppress unused fmt
	_ = fmt.Sprintf
}
