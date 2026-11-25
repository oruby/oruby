// Package orubyox implements the Ruby Ox (Optimized XML) gem in pure Go using
// encoding/xml, registered as "oruby-ox" and aliased to "ox".
package orubyox

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

func init() {
	oruby.LazyGem("oruby-ox", initOx)
	oruby.GemAlias("ox", "oruby-ox")
}

func parseOxTree(mrb *oruby.MrbState, src string) (oruby.Value, error) {
	docCls := rb.ClassPath(mrb, "Ox::Document")
	elemCls := rb.ClassPath(mrb, "Ox::Element")
	commentCls := rb.ClassPath(mrb, "Ox::Comment")
	instructCls := rb.ClassPath(mrb, "Ox::Instruct")
	doctypeCls := rb.ClassPath(mrb, "Ox::DocType")

	doc, err := mrb.Funcall(docCls, mrb.Intern("new"))
	if err != nil {
		return mrb.NilValue(), err
	}

	stack := []oruby.Value{doc}
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = true

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return mrb.NilValue(), err
		}
		cur := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			if t.Name.Space != "" {
				name = t.Name.Space + ":" + name
			}
			el, err := mrb.Funcall(elemCls, mrb.Intern("new"), mrb.StrNew(name))
			if err != nil {
				return mrb.NilValue(), err
			}
			for _, a := range t.Attr {
				ak := a.Name.Local
				if a.Name.Space != "" {
					ak = a.Name.Space + ":" + ak
				}
				if _, err := mrb.Funcall(el, mrb.Intern("[]="), mrb.StrNew(ak), mrb.StrNew(a.Value)); err != nil {
					return mrb.NilValue(), err
				}
			}
			if _, err := mrb.Funcall(cur, mrb.Intern("<<"), el); err != nil {
				return mrb.NilValue(), err
			}
			stack = append(stack, el)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			s := string(t)
			if len(stack) > 1 && strings.TrimSpace(s) != "" {
				if _, err := mrb.Funcall(cur, mrb.Intern("<<"), mrb.StrNew(s)); err != nil {
					return mrb.NilValue(), err
				}
			}
		case xml.Comment:
			c, err := mrb.Funcall(commentCls, mrb.Intern("new"), mrb.StrNew(string(t)))
			if err != nil {
				return mrb.NilValue(), err
			}
			if _, err := mrb.Funcall(cur, mrb.Intern("<<"), c); err != nil {
				return mrb.NilValue(), err
			}
		case xml.ProcInst:
			pi, err := mrb.Funcall(instructCls, mrb.Intern("new"), mrb.StrNew(t.Target))
			if err != nil {
				return mrb.NilValue(), err
			}
			if _, err := mrb.Funcall(cur, mrb.Intern("<<"), pi); err != nil {
				return mrb.NilValue(), err
			}
		case xml.Directive:
			raw := strings.TrimSpace(string(t))
			if strings.HasPrefix(strings.ToUpper(raw), "DOCTYPE ") {
				dt, err := mrb.Funcall(doctypeCls, mrb.Intern("new"), mrb.StrNew(strings.TrimSpace(raw[8:])))
				if err != nil {
					return mrb.NilValue(), err
				}
				if _, err := mrb.Funcall(cur, mrb.Intern("<<"), dt); err != nil {
					return mrb.NilValue(), err
				}
			}
		}
	}
	return doc, nil
}

func runOxSAX(mrb *oruby.MrbState, handler oruby.Value, src string) error {
	call := func(method string, args ...any) error {
		if !mrb.RespondTo(handler, mrb.Intern(method)) {
			return nil
		}
		_, err := mrb.Funcall(handler, mrb.Intern(method), args...)
		return err
	}
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if cerr := call("error", mrb.StrNew(err.Error()), mrb.FixnumValue(0), mrb.FixnumValue(0)); cerr != nil {
				return cerr
			}
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			sym := mrb.Intern(t.Name.Local).Value()
			if err := call("start_element", sym); err != nil {
				return err
			}
			for _, a := range t.Attr {
				if err := call("attr", mrb.Intern(a.Name.Local).Value(), mrb.StrNew(a.Value)); err != nil {
					return err
				}
			}
			if err := call("attrs_done"); err != nil {
				return err
			}
		case xml.EndElement:
			if err := call("end_element", mrb.Intern(t.Name.Local).Value()); err != nil {
				return err
			}
		case xml.CharData:
			s := strings.TrimSpace(string(t))
			if s != "" {
				if err := call("text", mrb.StrNew(s)); err != nil {
					return err
				}
			}
		case xml.Comment:
			if err := call("comment", mrb.StrNew(string(t))); err != nil {
				return err
			}
		case xml.ProcInst:
			if err := call("instruct", mrb.StrNew(t.Target)); err != nil {
				return err
			}
		}
	}
	return nil
}

func xmlEscapeText(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func initOx(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Ox")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("2.14.23"))
	stdErr := mrb.ClassGet("StandardError")
	parseErr := mrb.DefineClassUnder(mod, "ParseError", stdErr)
	mrb.DefineClassUnder(mod, "SyntaxError", parseErr)

	mod.DefineClassMethod("__parse", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		src := mrb.GetArgsFirst().String()
		doc, err := parseOxTree(mrb, src)
		if err != nil {
			return mrb.Raise(parseErr, fmt.Sprintf("XML parse error: %s", err.Error()))
		}
		return doc
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("__sax_parse", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if err := runOxSAX(mrb, args[0], args[1].String()); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(2))

	mod.DefineClassMethod("__escape", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.StrNew(xmlEscapeText(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))

	rb.LoadRuby(mrb, "<internal:oruby-ox>", []byte(`
module Ox
  @default_options = {
    encoding: nil,
    indent: 2,
    with_xml: false,
    with_dtd: false,
    mode: :generic,
    symbolize_keys: true,
  }

  def self.default_options
    @default_options
  end

  def self.default_options=(opts)
    @default_options = @default_options.merge(opts)
  end

  class Node
    attr_accessor :value

    def initialize(value)
      @value = value
    end

    def ==(other)
      other.is_a?(self.class) && @value == other.value
    end
  end

  class Comment < Node; end
  class CData < Node; end
  class DocType < Node; end

  class Element < Node
    attr_reader :nodes, :attributes

    def initialize(name)
      super(name.to_s)
      @nodes = []
      @attributes = {}
    end

    def name
      @value
    end
    alias value name

    def name=(n)
      @value = n.to_s
    end

    def <<(node)
      @nodes << node
      self
    end

    def [](attr)
      @attributes[attr.to_sym] || @attributes[attr.to_s]
    end

    def []=(attr, val)
      @attributes[attr.to_sym] = val.to_s
    end

    def text
      @nodes.find { |n| n.is_a?(String) }
    end

    def each(&block)
      @nodes.each(&block)
    end

    def locate(path)
      parts = path.to_s.split("/")
      cur = [self]
      parts.each do |part|
        next if part.empty?
        if part.start_with?("@")
          key = part[1..-1].to_sym
          cur = cur.map { |n| n.is_a?(Element) ? n[key] : nil }.compact
        elsif part == "*"
          cur = cur.flat_map { |n| n.is_a?(Element) ? n.nodes.select { |c| c.is_a?(Element) } : [] }
        elsif part == "^Text"
          cur = cur.flat_map { |n| n.is_a?(Element) ? n.nodes.select { |c| c.is_a?(String) } : [] }
        else
          cur = cur.flat_map { |n| n.is_a?(Element) ? n.nodes.select { |c| c.is_a?(Element) && c.name == part } : [] }
        end
      end
      cur
    end

    def ==(other)
      other.is_a?(Element) && @value == other.value && @attributes == other.attributes && @nodes == other.nodes
    end
  end

  class Instruct < Element; end

  class Document < Element
    def initialize(prolog = {})
      super("")
      @attributes = prolog.is_a?(Hash) ? prolog.dup : {}
    end

    def root
      @nodes.find { |n| n.is_a?(Element) && !n.is_a?(Instruct) }
    end
  end

  class Sax
    def start_element(name); end
    def end_element(name); end
    def attr(name, value); end
    def attrs_done; end
    def text(value); end
    def comment(value); end
    def instruct(target); end
    def error(message, line, column); end
  end

  def self.parse(xml)
    doc = __parse(xml.to_s)
    doc.nodes.length == 1 && doc.root ? doc.root : doc
  end

  def self.load(xml, options = {})
    opts = @default_options.merge(options || {})
    mode = opts[:mode] || :generic
    doc = __parse(xml.to_s)
    case mode
    when :hash, :hash_no_attrs
      root = doc.root
      return nil unless root
      sym = opts.key?(:symbolize_keys) ? opts[:symbolize_keys] : true
      k = sym ? root.name.to_sym : root.name
      { k => __elem_to_hash(root, mode == :hash, sym) }
    else
      doc.nodes.length == 1 && doc.root ? doc.root : doc
    end
  end

  def self.sax_parse(handler, io_or_str, _options = {})
    src = io_or_str.respond_to?(:read) ? io_or_str.read : io_or_str.to_s
    __sax_parse(handler, src)
  end

  def self.dump(obj, options = {})
    opts = @default_options.merge(options || {})
    indent = opts[:indent].nil? ? 2 : opts[:indent].to_i
    out = +""
    if opts[:with_xml]
      enc = opts[:encoding] ? %( encoding="#{opts[:encoding]}") : ""
      out << %(<?xml version="1.0"#{enc}?>)
      out << "\n" if indent >= 0
    end
    out << __dump_node(obj, indent, 0)
    out
  end

  class << self
    private

    def __elem_to_hash(el, with_attrs, sym)
      children = el.nodes.select { |n| n.is_a?(Element) }
      texts = el.nodes.select { |n| n.is_a?(String) }
      if children.empty? && (!with_attrs || el.attributes.empty?)
        return texts.empty? ? nil : texts.join
      end
      h = {}
      if with_attrs
        el.attributes.each do |ak, av|
          h[sym ? ak.to_sym : ak.to_s] = av
        end
      end
      children.each do |c|
        ck = sym ? c.name.to_sym : c.name
        cv = __elem_to_hash(c, with_attrs, sym)
        if h.key?(ck)
          h[ck] = [h[ck]] unless h[ck].is_a?(Array)
          h[ck] << cv
        else
          h[ck] = cv
        end
      end
      if !texts.empty? && !children.empty?
        h[sym ? :_text : "_text"] = texts.join
      end
      h
    end

    def __dump_node(obj, indent, depth)
      pad = indent > 0 ? (" " * (indent * depth)) : ""
      nl = indent >= 0 ? "\n" : ""
      case obj
      when Document
        obj.nodes.map { |n| __dump_node(n, indent, depth) }.join
      when Instruct
        attrs = obj.attributes.map { |k, v| %( #{k}="#{__escape(v.to_s)}") }.join
        "#{pad}<?#{obj.name}#{attrs}?>#{nl}"
      when Comment
        "#{pad}<!--#{obj.value}-->#{nl}"
      when CData
        "#{pad}<![CDATA[#{obj.value}]]>#{nl}"
      when DocType
        "#{pad}<!DOCTYPE #{obj.value}>#{nl}"
      when Element
        attrs = obj.attributes.map { |k, v| %( #{k}="#{__escape(v.to_s)}") }.join
        if obj.nodes.empty?
          "#{pad}<#{obj.name}#{attrs}/>#{nl}"
        elsif obj.nodes.length == 1 && obj.nodes[0].is_a?(String)
          "#{pad}<#{obj.name}#{attrs}>#{__escape(obj.nodes[0])}</#{obj.name}>#{nl}"
        else
          inner = obj.nodes.map do |n|
            n.is_a?(String) ? "#{indent > 0 ? ' ' * (indent * (depth + 1)) : ''}#{__escape(n)}#{nl}" : __dump_node(n, indent, depth + 1)
          end.join
          "#{pad}<#{obj.name}#{attrs}>#{nl}#{inner}#{pad}</#{obj.name}>#{nl}"
        end
      when Hash
        obj.map do |k, v|
          el = Element.new(k.to_s)
          if v.is_a?(Hash) || v.is_a?(Element)
            el << v
          elsif !v.nil?
            el << v.to_s
          end
          __dump_node(el, indent, depth)
        end.join
      else
        __escape(obj.to_s)
      end
    end
  end
end
`))
	return nil
}
