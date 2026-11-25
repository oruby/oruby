package nokogiri_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/nokogiri"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestEncodingTranscodingParity(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
require "nokogiri"

# 1. ISO-8859-2 XML (Croatian/Serbian/Czech diacritics: č=0xE8, ć=0xE6, š=0xB9, ž=0xBE, đ=0xF0)
raw_iso2 = "<?xml version=\"1.0\" encoding=\"ISO-8859-2\"?><r>\xe8\xe6\xb9\xbe\xf0</r>".b
doc = Nokogiri::XML(raw_iso2)
raise "iso2 enc: #{doc.encoding}" unless doc.encoding == "ISO-8859-2"
raise "iso2 text: #{doc.root.text.inspect}" unless doc.root.text == "čćšžđ"

# 2. Windows-1250 XML (š=0x9A, ž=0x9E, č=0xE8, ć=0xE6, đ=0xF0)
raw_cp1250 = "<?xml version=\"1.0\" encoding=\"Windows-1250\"?><r>\xe8\xe6\x9a\x9e\xf0</r>".b
doc_cp = Nokogiri::XML(raw_cp1250)
raise "cp1250 text: #{doc_cp.root.text.inspect}" unless doc_cp.root.text == "čćšžđ"

# 3. UTF-16LE with BOM
raw_u16 = "\xff\xfe<\x00a\x00>\x00H\x00i\x00<\x00/\x00a\x00>\x00".b
doc_u16 = Nokogiri::XML(raw_u16)
raise "utf16le: #{doc_u16.root.text.inspect}" unless doc_u16.root.name == "a" && doc_u16.root.text == "Hi"

# 4. HTML4 with <meta charset="ISO-8859-2">
raw_html = "<html><head><meta charset=\"ISO-8859-2\"></head><body><p>\xe8\xe6\xb9</p></body></html>".b
hdoc = Nokogiri::HTML4(raw_html)
raise "html iso2: #{hdoc.at_css('p').text.inspect}" unless hdoc.at_css("p").text == "čćš"
`)
	if err != nil {
		t.Fatalf("encoding transcoding parity failed: %v", err)
	}
}

func TestEntrypointsElementDescXPathContextCSSAndHTML5Parity(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
require "nokogiri"

# Top-level convenience entrypoints
raise "make" unless Nokogiri.make("<p>hi</p>").to_html == "<p>hi</p>"
raise "XML.parse" unless Nokogiri::XML.parse("<a/>").root.name == "a"
raise "XML.fragment" unless Nokogiri::XML.fragment("<a/><b/>").children.length == 2
raise "HTML4.parse" unless Nokogiri::HTML4.parse("<p>x</p>").at_css("p").text == "x"
raise "HTML4.fragment" unless Nokogiri::HTML4.fragment("<b>hi</b>").to_html == "<b>hi</b>"

# HTML4::ElementDescription & Node#description
br = Nokogiri::HTML4::ElementDescription["br"]
div = Nokogiri::HTML4::ElementDescription["div"]
font = Nokogiri::HTML4::ElementDescription["font"]
raise "br desc" unless br.empty? && br.save_end_tag?
raise "div desc" unless div.block? && !div.inline? && !div.empty?
raise "font desc" unless font.deprecated? && font.inline?
hdoc = Nokogiri::HTML4("<div><br></div>")
raise "node.description" unless hdoc.at_css("br").description.empty?

# HTML5::QuirksMode, HTML5::Builder, HTML5::DocumentFragment context:
raise "quirks consts" unless Nokogiri::HTML5::QuirksMode::NO_QUIRKS == 0 && Nokogiri::HTML5::QuirksMode::QUIRKS == 1
raise "quirks int" unless Nokogiri::HTML5("<!DOCTYPE html><p>ok</p>").quirks_mode == 0
frag = Nokogiri::HTML5::DocumentFragment.parse("<td>cell</td>", context: "tr")
raise "html5 context: #{frag.to_html}" unless frag.to_html == "<td>cell</td>"
b5 = Nokogiri::HTML5::Builder.new { |h| h.section { h.p("hi") } }
raise "html5 builder" unless b5.to_html.include?("<section><p>hi</p></section>")

# XML::XPathContext
xdoc = Nokogiri::XML('<root xmlns:u="urn:u"><u:item id="1">A</u:item><u:item id="2">B</u:item></root>')
ctx = Nokogiri::XML::XPathContext.new(xdoc)
ctx.register_ns("u", "urn:u")
ctx.register_variable("target", "2")
res = ctx.evaluate("//u:item[@id=$target]")
raise "xpath_context: #{res.map(&:text)}" unless res.map(&:text) == ["B"]

# CSS.parse / CSS::Parser / CSS::Node#to_xpath
nodes = Nokogiri::CSS.parse("div > p.foo, span#bar")
raise "css parse len" unless nodes.length == 2
raise "css to_xpath" unless nodes[0].to_xpath.include?("div/p")

# Remaining public API helpers
f = Nokogiri::XML.fragment("<a/><b/>")
a_node, b_node = *f.deconstruct
raise "frag deconstruct" unless a_node.name == "a" && b_node.name == "b"
f.errors = ["custom"]
raise "frag errors=" unless f.errors == ["custom"]
raise "frag parse_options" unless f.parse_options.is_a?(Nokogiri::XML::ParseOptions)

ctx.register_variables("target" => "1")
ctx.node = xdoc.root
raise "ctx register_variables/node=" unless ctx.evaluate(".//u:item[@id=$target]").map(&:text) == ["A"]

sp = Nokogiri::XML::SAX::Parser.new(Nokogiri::XML::SAX::Document.new, "ISO-8859-1")
raise "sax parser encoding" unless sp.encoding == "ISO-8859-1"
sp.encoding = "UTF-8"
raise "sax parser encoding=" unless sp.encoding == "UTF-8"

pp = Nokogiri::XML::SAX::PushParser.new
pp.replace_entities = true
pp.options = Nokogiri::XML::ParseOptions::RECOVER
raise "push parser opts" unless pp.replace_entities == true && pp.options == Nokogiri::XML::ParseOptions::RECOVER

raise "elem desc description" unless br.description == "br"
raise "xpath_doctype" unless xdoc.xpath_doctype == :xml && hdoc.xpath_doctype == :html4 && Nokogiri::HTML5("<p/>").xpath_doctype == :html5

# ClassResolver#related_class (Loofah-style subclass resolution)
module MyLoofah
  module HTML5
    class Document < Nokogiri::HTML5::Document; end
    class DocumentFragment < Nokogiri::HTML5::DocumentFragment; end
    class Builder < Nokogiri::HTML5::Builder; end
  end
end
ldoc = MyLoofah::HTML5::Document.new
raise "related_class frag" unless ldoc.related_class("DocumentFragment") == MyLoofah::HTML5::DocumentFragment
raise "subclass fragment" unless ldoc.fragment("<p>hi</p>").is_a?(MyLoofah::HTML5::DocumentFragment)
raise "related_class builder" unless MyLoofah::HTML5::Builder.new.related_class("Document") == MyLoofah::HTML5::Document
raise "related_class sax" unless Nokogiri::XML::SAX::Parser.new.related_class("Document") == Nokogiri::XML::SAX::Document

# Final 4 methods: coerce, initialize_copy_with_args, safe_process_xinclude, create_entity
coerced = xdoc.root.coerce("<x/>")
raise "coerce string" unless coerced.is_a?(Nokogiri::XML::NodeSet) && coerced.first.name == "x"
raise "coerce node" unless xdoc.root.coerce(xdoc.root) == xdoc.root
bad_coerce = false
begin
  xdoc.root.coerce(xdoc)
rescue ArgumentError
  bad_coerce = true
end
raise "coerce reject doc" unless bad_coerce

ent = xdoc.create_entity("copy", 1, nil, nil, "(c)")
raise "create_entity" unless ent.name == "copy" && ent.node_type == Nokogiri::XML::Node::ENTITY_DECL
raise "safe_process_xinclude" unless xdoc.safe_process_xinclude == xdoc
raise "css parser racc" unless Nokogiri::CSS::Parser < Racc::Parser
raise "unescape css" unless Nokogiri::CSS::Parser.new.unescape_css_element_name('foo\:bar') == "foo:bar"
`)
	if err != nil {
		t.Fatalf("entrypoints/parity test failed: %v", err)
	}
}

func TestMidPathXPathHandlersIncrementalPushSAXAndLiveXSLTExtensions(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
require "nokogiri"

# 1. Mid-path custom Ruby XPath handler
h = Object.new
h.define_singleton_method(:eq) { |ns, s| ns.text == s }
h.define_singleton_method(:has_cls) { |ns, c| ns.any? { |n| n.classes.include?(c) } }

doc = Nokogiri::XML('<r><a><b>1</b></a><a><b>2</b></a></r>')
raise "mid-path eq" unless doc.xpath("//a[eq(b, '2')]/b", h).map(&:text) == ["2"]

doc2 = Nokogiri::XML('<div><section class="keep"><span>yes</span></section><section class="drop"><span>no</span></section></div>')
raise "mid-path desc" unless doc2.xpath("//section[has_cls(., 'keep')]//span", h).map(&:text) == ["yes"]
raise "mid-path not" unless doc2.xpath("//section[not(has_cls(., 'keep'))]//span", h).map(&:text) == ["no"]

# 2. True incremental SAX::PushParser (events fire immediately during <<, even split mid-tag!)
ev = []
sax_doc = Class.new(Nokogiri::XML::SAX::Document) do
  define_method(:start_document) { ev << :start_doc }
  define_method(:start_element) { |n, attrs| ev << [:start, n, attrs] }
  define_method(:characters) { |s| ev << [:text, s] unless s.strip.empty? }
  define_method(:end_element) { |n| ev << [:end, n] }
  define_method(:end_document) { ev << :end_doc }
end.new

pp = Nokogiri::XML::SAX::PushParser.new(sax_doc)
pp << "<r><a id='1'/>"
mid_events = ev.dup
raise "incremental mid: #{mid_events}" unless mid_events.include?([:start, "r", []]) && mid_events.include?([:start, "a", [["id", "1"]]]) && !mid_events.include?(:end_doc)

# Split tag and text across chunks
pp << "<b k="
pp << "'v'>hel"
pp << "lo &amp; bye</b></r>"
pp.finish
raise "incremental final: #{ev}" unless ev.last == :end_doc && ev.include?([:start, "b", [["k", "v"]]]) && ev.include?([:end, "r"])

# 3. Live XSLT Ruby extension function callbacks
ext_cls = Class.new do
  def shout(str)
    str.to_s.upcase + "!"
  end

  def join_pair(a, b)
    "#{a}=#{b}"
  end
end
Nokogiri::XSLT.register("http://example.com/oruby-ext", ext_cls)

xsl = Nokogiri::XSLT(<<~XSL)
  <xsl:stylesheet version="1.0"
      xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
      xmlns:ext="http://example.com/oruby-ext">
    <xsl:template match="/items">
      <out>
        <xsl:for-each select="item">
          <p><xsl:value-of select="ext:shout(text())"/>|<xsl:value-of select="ext:join_pair(@code, text())"/></p>
        </xsl:for-each>
      </out>
    </xsl:template>
  </xsl:stylesheet>
XSL

xml = Nokogiri::XML('<items><item code="k1">alpha</item><item code="k2">beta</item></items>')
transformed = xsl.transform(xml)
ps = transformed.xpath("//p").map(&:text)
raise "xslt ruby ext: #{ps.inspect}" unless ps == ["ALPHA!|k1=alpha", "BETA!|k2=beta"]
`)
	if err != nil {
		t.Fatalf("mid-path/push-sax/xslt-ext parity failed: %v", err)
	}
}
