package nokogiri

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

// emitSAXEvents walks a parsed DOM tree and fires standard Nokogiri::XML::SAX::Document
// callbacks in document order.
func emitSAXEvents(mrb *oruby.MrbState, handler oruby.Value, doc *domNode) error {
	call := func(name string, args ...any) error {
		if !mrb.RespondTo(handler, mrb.Intern(name)) {
			return nil
		}
		_, err := mrb.Funcall(handler, mrb.Intern(name), args...)
		return err
	}

	if !doc.isHTML && doc.version != "" {
		if err := call("xmldecl", mrb.StrNew(doc.version), mrb.StrNew(doc.encoding), mrb.NilValue()); err != nil {
			return err
		}
	}
	if err := call("start_document"); err != nil {
		return err
	}
	for _, e := range doc.errors {
		if err := call("error", mrb.StrNew(e.message)); err != nil {
			return err
		}
	}
	if err := walkSAXNode(mrb, handler, doc, call); err != nil {
		return err
	}
	return call("end_document")
}

func walkSAXNode(mrb *oruby.MrbState, handler oruby.Value, n *domNode, call func(string, ...any) error) error {
	for c := n.first; c != nil; c = c.next {
		switch c.kind {
		case ElementNode:
			qname := c.name
			if c.prefix != "" {
				qname = c.prefix + ":" + c.name
			}
			attrsAry := mrb.AryNewCapa(len(c.attrs) + len(c.nsDefs))
			for _, a := range c.attrs {
				ak := a.name
				if a.prefix != "" {
					ak = a.prefix + ":" + a.name
				}
				pair := mrb.AryNewCapa(2)
				mrb.AryPush(pair, mrb.StrNew(ak))
				mrb.AryPush(pair, mrb.StrNew(a.value))
				mrb.AryPush(attrsAry, pair)
			}
			for _, ns := range c.nsDefs {
				ak := "xmlns"
				if ns.prefix != "" {
					ak = "xmlns:" + ns.prefix
				}
				pair := mrb.AryNewCapa(2)
				mrb.AryPush(pair, mrb.StrNew(ak))
				mrb.AryPush(pair, mrb.StrNew(ns.href))
				mrb.AryPush(attrsAry, pair)
			}

			nsAttrsAry := mrb.AryNewCapa(len(c.attrs))
			attrCls := rb.ClassPath(mrb, "Nokogiri::XML::SAX::Parser::Attribute")
			for _, a := range c.attrs {
				var ap, au oruby.Value = mrb.NilValue(), mrb.NilValue()
				if a.prefix != "" {
					ap = mrb.StrNew(a.prefix)
				}
				if a.nsURI != "" {
					au = mrb.StrNew(a.nsURI)
				}
				if obj, err := mrb.Funcall(attrCls, mrb.Intern("new"), mrb.StrNew(a.name), ap, au, mrb.StrNew(a.value)); err == nil {
					mrb.AryPush(nsAttrsAry, obj)
				}
			}

			var prefVal, uriVal oruby.Value = mrb.NilValue(), mrb.NilValue()
			if c.prefix != "" {
				prefVal = mrb.StrNew(c.prefix)
			}
			if c.nsURI != "" {
				uriVal = mrb.StrNew(c.nsURI)
			}
			nsAry := mrb.AryNewCapa(len(c.nsDefs))
			for _, ns := range c.nsDefs {
				pair := mrb.AryNewCapa(2)
				if ns.prefix == "" {
					mrb.AryPush(pair, mrb.NilValue())
				} else {
					mrb.AryPush(pair, mrb.StrNew(ns.prefix))
				}
				mrb.AryPush(pair, mrb.StrNew(ns.href))
				mrb.AryPush(nsAry, pair)
			}

			if err := call("start_element_namespace", mrb.StrNew(c.name), nsAttrsAry, prefVal, uriVal, nsAry); err != nil {
				return err
			}
			if err := call("start_element", mrb.StrNew(qname), attrsAry); err != nil {
				return err
			}
			if err := walkSAXNode(mrb, handler, c, call); err != nil {
				return err
			}
			if err := call("end_element_namespace", mrb.StrNew(c.name), prefVal, uriVal); err != nil {
				return err
			}
			if err := call("end_element", mrb.StrNew(qname)); err != nil {
				return err
			}
		case TextNode:
			if err := call("characters", mrb.StrNew(c.data)); err != nil {
				return err
			}
		case CDATASectionNode:
			if err := call("cdata_block", mrb.StrNew(c.data)); err != nil {
				return err
			}
		case CommentNode:
			if err := call("comment", mrb.StrNew(c.data)); err != nil {
				return err
			}
		case PINode:
			if err := call("processing_instruction", mrb.StrNew(c.name), mrb.StrNew(c.data)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// XML::Reader event stream
// ---------------------------------------------------------------------------

const (
	readerTypeNone                  = 0
	readerTypeElement               = 1
	readerTypeAttribute             = 2
	readerTypeText                  = 3
	readerTypeCDATA                 = 4
	readerTypeProcessingInstruction = 7
	readerTypeComment               = 8
	readerTypeDocument              = 9
	readerTypeDocumentType          = 10
	readerTypeSignificantWhitespace = 14
	readerTypeEndElement            = 15
)

type readerEvent struct {
	nodeType    int
	node        *domNode
	depth       int
	selfClosing bool
}

type xmlReader struct {
	doc    *domNode
	events []readerEvent
	idx    int
}

func buildReaderEvents(doc *domNode) *xmlReader {
	var events []readerEvent
	var walk func(n *domNode, depth int)
	walk = func(n *domNode, depth int) {
		for c := n.first; c != nil; c = c.next {
			switch c.kind {
			case ElementNode:
				empty := c.first == nil
				events = append(events, readerEvent{
					nodeType:    readerTypeElement,
					node:        c,
					depth:       depth,
					selfClosing: empty,
				})
				if !empty {
					walk(c, depth+1)
					events = append(events, readerEvent{
						nodeType: readerTypeEndElement,
						node:     c,
						depth:    depth,
					})
				}
			case TextNode:
				events = append(events, readerEvent{nodeType: readerTypeText, node: c, depth: depth})
			case CDATASectionNode:
				events = append(events, readerEvent{nodeType: readerTypeCDATA, node: c, depth: depth})
			case CommentNode:
				events = append(events, readerEvent{nodeType: readerTypeComment, node: c, depth: depth})
			case PINode:
				events = append(events, readerEvent{nodeType: readerTypeProcessingInstruction, node: c, depth: depth})
			case DocumentTypeNode:
				events = append(events, readerEvent{nodeType: readerTypeDocumentType, node: c, depth: depth})
			}
		}
	}
	walk(doc, 0)
	return &xmlReader{doc: doc, events: events, idx: -1}
}
