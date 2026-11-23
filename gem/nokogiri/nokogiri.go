package nokogiri

import (
	_ "embed"
	"fmt"
	"strings"
	"unsafe"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
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
		"NAMESPACE_DECL":     NamespaceDecl,
	} {
		mrb.ConstSet(nodeCls, mrb.Intern(name), mrb.FixnumValue(val))
	}

	defineNodeMethods(mrb, nodeCls, elemCls, textCls, cdataCls, commentCls, piCls, attrCls)
	defineDocumentMethods(mrb, docCls, fragCls, html4DocCls, html4FragCls, html5DocCls, html5FragCls)
	defineNamespaceMethods(mrb, nsCls)
	defineNodeSetMethods(mrb, nsSetCls)
	defineSAXAndReader(mrb, xmlMod, html4Mod)

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

func wrapNode(mrb *oruby.MrbState, n *domNode) oruby.Value {
	if n == nil {
		return mrb.NilValue()
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
	return nil, oruby.ETypeError("expected Nokogiri::XML::Node, NodeSet, or String, got %s", mrb.ClassOf(arg).Name())
}

func extractNSAndVars(mrb *oruby.MrbState, args oruby.RArgs, startIdx int) (map[string]string, map[string]any) {
	nsMap := map[string]string{}
	vars := map[string]any{}

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

	if args.Len() > startIdx && args.Item(startIdx).IsHash() {
		collectHash(args.Item(startIdx), false)
	}
	if args.Len() > startIdx+1 && args.Item(startIdx+1).IsHash() {
		collectHash(args.Item(startIdx+1), true)
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
		collectHash(kw, true)
	}
	return nsMap, vars
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
			mrb.AryPush(ary, exc)
		}
	}
	return ary.Value()
}

// ---------------------------------------------------------------------------
// Nokogiri::XML::Node methods
// ---------------------------------------------------------------------------

func defineNodeMethods(mrb *oruby.MrbState, nodeCls, elemCls, textCls, cdataCls, commentCls, piCls, attrCls oruby.RClass) {
	// Node.new(name, document)
	nodeCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		name := args.Item(0).String()
		var doc *domNode
		if args.Len() >= 2 {
			doc, _ = getDOMNode(mrb, args.Item(1))
		}
		if doc == nil {
			doc = newDocument(false, false)
		}
		prefix, local := splitQName(name)
		n := newNode(doc, ElementNode, local, "")
		n.prefix = prefix
		rb.Set(mrb, self, n)
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

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
			content = args.Item(1).String()
		} else {
			content = args.Item(0).String()
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

	nodeCls.DefineMethod("path", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.path())
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
		s := mrb.String(mrb.GetArgsFirst())
		switch n.kind {
		case TextNode, CDATASectionNode, CommentNode, PINode:
			n.data = s
		case AttributeNode:
			n.data = s
			if n.parent != nil {
				n.parent.setAttr(n.name, s)
			}
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
		if a, ok := n.getAttr(mrb.String(mrb.GetArgsFirst())); ok {
			return wrapAttr(mrb, a)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

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

	// Tree mutations
	nodeCls.DefineMethod("add_child", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		child, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if child.kind == DocumentFragNode {
			var added []*domNode
			for c := child.first; c != nil; {
				nxt := c.next
				n.appendChild(c)
				added = append(added, c)
				c = nxt
			}
			return wrapNodeSet(mrb, n.doc, added)
		}
		return wrapNode(mrb, n.appendChild(child))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("prepend_child", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		child, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return wrapNode(mrb, n.prependChild(child))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("add_next_sibling", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		sib, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return wrapNode(mrb, n.insertAfter(sib))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("add_previous_sibling", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		sib, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return wrapNode(mrb, n.insertBefore(sib))
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("replace", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		rep, err := parseNodeArg(mrb, n.doc, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
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
		return wrapNode(mrb, n.clone(deep, targetDoc))
	}
	nodeCls.DefineMethod("dup", dupFn, mrb.ArgsOpt(2))
	nodeCls.DefineMethod("clone", dupFn, mrb.ArgsOpt(2))

	// Namespaces
	nodeCls.DefineMethod("namespace", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		if n.nsURI == "" && n.prefix == "" {
			return mrb.NilValue()
		}
		return wrapNS(mrb, &domNS{prefix: n.prefix, href: n.nsURI})
	}, mrb.ArgsNone())

	nodeCls.DefineMethod("namespace=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			n.prefix = ""
			n.nsURI = ""
			return mrb.NilValue()
		}
		if ns, ok := rb.Get[*domNS](mrb, v); ok {
			n.prefix = ns.prefix
			n.nsURI = ns.href
		}
		return v
	}, mrb.ArgsReq(1))

	nodeCls.DefineMethod("namespace_definitions", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		ary := mrb.AryNewCapa(len(n.nsDefs))
		for _, ns := range n.nsDefs {
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
		return mrb.StrNew(n.serialize(asHTML, indent, xmlDecl))
	}, mrb.ArgsReq(3))

	nodeCls.DefineMethod("canonicalize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs().Slice()
		mode := xmlC14N10
		if len(args) >= 1 && !args[0].IsNil() {
			mode = args[0].Int()
		}
		var incl []string
		if len(args) >= 2 && args[1].IsArray() {
			for i := 0; i < oruby.RArrayLen(args[1]); i++ {
				incl = append(incl, mrb.AryRef(args[1], i).String())
			}
		}
		withComments := false
		if len(args) >= 3 && !args[2].IsNil() {
			withComments = args[2].Bool()
		}
		return mrb.StrNew(n.canonicalize(mode, incl, withComments))
	}, mrb.ArgsOpt(3))

	nodeCls.DefineMethod("inner_html", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		return mrb.StrNew(n.innerHTML(n.isHTMLDoc()))
	}, mrb.ArgsOpt(1))

	nodeCls.DefineMethod("inner_html=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		s := mrb.GetArgsFirst().String()
		n.clearChildren()
		var frag *domNode
		if n.isHTMLDoc() {
			ctxTag := "body"
			if n.kind == ElementNode && n.name != "" {
				ctxTag = n.name
			}
			frag = parseHTMLFragment(n.doc, s, ctxTag, false)
		} else {
			frag = parseXMLFragment(n.doc, s, false, false)
		}
		n.appendChild(frag)
		return mrb.StrNew(s)
	}, mrb.ArgsReq(1))

	// XPath & CSS querying
	nodeCls.DefineMethod("__xpath", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		args := mrb.GetArgs()
		expr := args.Item(0).String()
		nsMap, vars := extractNSAndVars(mrb, args, 1)
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
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(2))

	nodeCls.DefineMethod("__css", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		n, _ := getDOMNode(mrb, self)
		sel := mrb.GetArgsFirst().String()
		nodes, err := queryCSS(n, sel)
		if err != nil {
			return rb.Raise(mrb, "Nokogiri::CSS::SyntaxError", "%s", err.Error())
		}
		return wrapNodeSet(mrb, n.doc, nodes)
	}, mrb.ArgsReq(1))

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
// Document & DocumentFragment methods
// ---------------------------------------------------------------------------

func defineDocumentMethods(mrb *oruby.MrbState, docCls, fragCls, html4DocCls, html4FragCls, html5DocCls, html5FragCls oruby.RClass) {
	docCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if existing, ok := getDOMNode(mrb, self); ok && existing != nil {
			return self
		}
		args := mrb.GetArgs()
		d := newDocument(false, false)
		d.rbClass = mrb.ClassOf(self)
		d.hasClass = true
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			d.version = args.Item(0).String()
		}
		rb.Set(mrb, self, d)
		return self
	}, mrb.ArgsAny())

	html4DocCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if existing, ok := getDOMNode(mrb, self); ok && existing != nil {
			return self
		}
		d := newDocument(true, false)
		d.rbClass = mrb.ClassOf(self)
		d.hasClass = true
		rb.Set(mrb, self, d)
		return self
	}, mrb.ArgsAny())

	html5DocCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if existing, ok := getDOMNode(mrb, self); ok && existing != nil {
			return self
		}
		d := newDocument(true, true)
		d.rbClass = mrb.ClassOf(self)
		d.hasClass = true
		rb.Set(mrb, self, d)
		return self
	}, mrb.ArgsAny())

	docCls.DefineClassMethod("__parse_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).String()
		url := ""
		if !args.Item(1).IsNil() {
			url = args.Item(1).String()
		}
		enc := ""
		if !args.Item(2).IsNil() {
			enc = args.Item(2).String()
		}
		opts := args.Item(3).Int()
		strict := (opts & poRecover) == 0
		noblanks := (opts & poNoBlanks) != 0
		nocdata := (opts & poNoCDATA) != 0

		doc := parseXMLDocument(src, url, enc, strict, noblanks, nocdata)
		if strict && len(doc.errors) > 0 {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", doc.errors[0].message)
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
		if !args.Item(1).IsNil() {
			url = args.Item(1).String()
		}
		enc := ""
		if !args.Item(2).IsNil() {
			enc = args.Item(2).String()
		}
		opts := args.Item(3).Int()
		isHTML5 := args.Item(4).Bool()
		noblanks := (opts & poNoBlanks) != 0

		doc := parseHTMLDocument(src, url, enc, isHTML5, noblanks)
		cls := mrb.ClassPtr(self)
		doc.rbClass = cls
		doc.hasClass = true
		val := mrb.DataWrapInterface(cls, doc).Value()
		if _, err := mrb.Funcall(val, mrb.Intern("initialize")); err != nil {
			return mrb.RaiseError(err)
		}
		return val
	}, mrb.ArgsReq(5))

	fragCls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		var doc *domNode
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			doc, _ = getDOMNode(mrb, args.Item(0))
		}
		if doc == nil {
			doc = newDocument(false, false)
		}
		tags := ""
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			tags = args.Item(1).String()
		}
		ctxTag := ""
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			if ctxNode, ok := getDOMNode(mrb, args.Item(2)); ok {
				ctxTag = ctxNode.name
			} else if args.Item(2).IsString() {
				ctxTag = args.Item(2).String()
			}
		}
		var frag *domNode
		if doc.isHTML {
			frag = parseHTMLFragment(doc, tags, ctxTag, false)
		} else {
			frag = parseXMLFragment(doc, tags, false, false)
		}
		frag.rbClass = mrb.ClassOf(self)
		frag.hasClass = true
		rb.Set(mrb, self, frag)
		if doc.hasDecorators {
			decorateValue(mrb, doc, self)
		}
		return self
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

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
		if old := d.rootElement(); old != nil {
			old.unlink()
		}
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.NilValue()
		}
		n, ok := getDOMNode(mrb, v)
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::Node")
		}
		d.appendChild(n)
		return v
	}, mrb.ArgsReq(1))

	docCls.DefineMethod("encoding", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		if d.encoding == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(d.encoding)
	}, mrb.ArgsNone())

	docCls.DefineMethod("encoding=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, _ := getDOMNode(mrb, self)
		v := mrb.GetArgsFirst()
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
		d, _ := getDOMNode(mrb, self)
		return makeErrorsArray(mrb, d.errors)
	}, mrb.ArgsNone())

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
		f, _ := getDOMNode(mrb, self)
		return makeErrorsArray(mrb, f.errors)
	}, mrb.ArgsNone())

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

	cls.DefineMethod("document", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		return wrapNode(mrb, ns.doc)
	}, mrb.ArgsNone())

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

	cls.DefineMethod("each", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		blk := mrb.GetArgsBlock()
		if blk.IsNil() {
			return mrb.Call(self, "to_enum", mrb.Intern("each").Value())
		}
		ns, _ := getNodeSet(mrb, self)
		for _, node := range ns.nodes {
			if _, err := mrb.YieldE(blk, wrapNode(mrb, node)); err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}, mrb.ArgsBlock())

	pushFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		n, ok := getDOMNode(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::Node")
		}
		ns.nodes = append(ns.nodes, n)
		return self
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
		target, ok := getDOMNode(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.NilValue()
		}
		for i, n := range ns.nodes {
			if n == target {
				ns.nodes = append(ns.nodes[:i], ns.nodes[i+1:]...)
				return wrapNode(mrb, target)
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("include?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		target, ok := getDOMNode(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.FalseValue()
		}
		for _, n := range ns.nodes {
			if n == target {
				return mrb.TrueValue()
			}
		}
		return mrb.FalseValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("index", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns, _ := getNodeSet(mrb, self)
		target, ok := getDOMNode(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.NilValue()
		}
		for i, n := range ns.nodes {
			if n == target {
				return mrb.FixnumValue(i)
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("|", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ns1, _ := getNodeSet(mrb, self)
		ns2, ok := getNodeSet(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.ETypeError().Raise("expected Nokogiri::XML::NodeSet")
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
			return mrb.ETypeError().Raise("expected Nokogiri::XML::NodeSet")
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
			return mrb.ETypeError().Raise("expected Nokogiri::XML::NodeSet")
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
		var doc *domNode
		if isHTML {
			doc = parseHTMLDocument(src, "", "UTF-8", false, false)
		} else {
			doc = parseXMLDocument(src, "", "UTF-8", false, false, false)
		}
		if err := emitSAXEvents(mrb, handler, doc); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(3))

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
		"TYPE_PROCESSING_INSTRUCTION": readerTypeProcessingInstruction,
		"TYPE_COMMENT":                readerTypeComment,
		"TYPE_DOCUMENT":               readerTypeDocument,
		"TYPE_DOCUMENT_TYPE":          readerTypeDocumentType,
		"TYPE_SIGNIFICANT_WHITESPACE": readerTypeSignificantWhitespace,
		"TYPE_END_ELEMENT":            readerTypeEndElement,
	} {
		mrb.ConstSet(readerCls, mrb.Intern(name), mrb.FixnumValue(val))
	}

	readerCls.DefineClassMethod("from_memory", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).String()
		doc := parseXMLDocument(src, "", "UTF-8", false, false, false)
		return mrb.DataWrapInterface(readerCls, buildReaderEvents(doc)).Value()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

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

	readerCls.DefineMethod("read", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r.idx+1 >= len(r.events) {
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
		if ev == nil || ev.node == nil {
			return mrb.NilValue()
		}
		if a, ok := ev.node.getAttr(mrb.GetArgsFirst().String()); ok {
			return mrb.StrNew(a.value)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	readerCls.DefineMethod("attributes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		h := mrb.HashNew()
		if ev != nil && ev.node != nil {
			for _, a := range ev.node.attrs {
				mrb.HashSet(h, mrb.StrNew(a.name), mrb.StrNew(a.value))
			}
		}
		return h
	}, mrb.ArgsNone())

	readerCls.DefineMethod("inner_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil {
			return mrb.StrNew("")
		}
		return mrb.StrNew(ev.node.innerHTML(false))
	}, mrb.ArgsNone())

	readerCls.DefineMethod("outer_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ev := curEv(getR(mrb, self))
		if ev == nil || ev.node == nil {
			return mrb.StrNew("")
		}
		return mrb.StrNew(ev.node.serialize(false, 0, false))
	}, mrb.ArgsNone())

	readerCls.DefineMethod("errors", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getR(mrb, self)
		if r == nil || r.doc == nil {
			return mrb.AryNew()
		}
		return makeErrorsArray(mrb, r.doc.errors)
	}, mrb.ArgsNone())

	// Suppress unused fmt
	_ = fmt.Sprintf
}
