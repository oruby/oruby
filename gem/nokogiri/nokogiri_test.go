package nokogiri_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/nokogiri"
	_ "github.com/oruby/oruby/gem/regexp"
)

func evalRuby(t *testing.T, code string) oruby.RValue {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	v, err := mrb.Eval(code)
	if err != nil {
		t.Fatalf("Ruby error: %v\ncode:\n%s", err, code)
	}
	return v
}

func TestXMLParsingAndNodes(t *testing.T) {
	evalRuby(t, `
require "nokogiri"

xml = <<~XML
  <?xml version="1.0" encoding="UTF-8"?>
  <!DOCTYPE catalog>
  <!-- a comment -->
  <?pi target-data?>
  <catalog lang="en">
    <book id="1">
      <title>Tom &amp; Jerry &#65;&#x42;</title>
      <desc><![CDATA[<b>raw & unescaped</b>]]></desc>
    </book>
    <book id="2">
      <title>Go &lt;3 Ruby</title>
    </book>
  </catalog>
XML

doc = Nokogiri::XML(xml)
raise "expected XML::Document" unless doc.is_a?(Nokogiri::XML::Document)
raise "version" unless doc.version == "1.0"
raise "encoding" unless doc.encoding == "UTF-8"
raise "errors: #{doc.errors.inspect}" unless doc.errors.empty?
raise "root" unless doc.root.name == "catalog"
raise "attr" unless doc.root["lang"] == "en"

books = doc.xpath("//book")
raise "books count #{books.length}" unless books.length == 2
raise "title 1: #{books[0].at_xpath('title').text.inspect}" unless books[0].at_xpath("title").text == "Tom & Jerry AB"
raise "title 2" unless books[1].at_xpath("title").text == "Go <3 Ruby"
raise "cdata" unless books[0].at_xpath("desc").text == "<b>raw & unescaped</b>"
raise "cdata node" unless books[0].at_xpath("desc").children.first.cdata?
raise "line > 0" unless books[0].line > 0

# Comment and PI present
comment = doc.children.find(&:comment?)
raise "comment missing" unless comment && comment.text.strip == "a comment"
pi = doc.children.find(&:processing_instruction?)
raise "pi missing" unless pi && pi.name == "pi" && pi.content == "target-data"

# NOBLANKS option
doc_nb = Nokogiri::XML("<root>\n  <a>1</a>\n  <b>2</b>\n</root>") { |cfg| cfg.noblanks }
raise "noblanks children #{doc_nb.root.children.length}" unless doc_nb.root.children.length == 2

# NOCDATA option converts CDATA to Text
doc_nc = Nokogiri::XML("<r><![CDATA[hi]]></r>") { |cfg| cfg.nocdata }
raise "nocdata should be text" unless doc_nc.root.children.first.text? && !doc_nc.root.children.first.cdata?

# STRICT mode raises on malformed XML, RECOVER records errors
raised = false
begin
  Nokogiri::XML("<root><a></root>") { |cfg| cfg.strict }
rescue Nokogiri::XML::SyntaxError
  raised = true
end
raise "expected SyntaxError in strict mode" unless raised

doc_rec = Nokogiri::XML("<root><a></root>")
raise "expected errors in recover mode" if doc_rec.errors.empty?
raise "expected line on error" unless doc_rec.errors.first.line >= 1

# Parse from IO-like object
fake_io = Struct.new(:data) { def read; data; end }.new("<ok>yes</ok>")
doc_io = Nokogiri::XML(fake_io)
raise "io parse" unless doc_io.at_xpath("//ok").text == "yes"
`)
}

func TestXMLNamespacesAndXPath(t *testing.T) {
	evalRuby(t, `
require "nokogiri"

xml = <<~XML
  <feed xmlns="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/elements/1.1/">
    <title>News</title>
    <entry id="10">
      <title>First</title>
      <dc:creator>Alice</dc:creator>
    </entry>
    <entry id="20">
      <title>Second</title>
      <dc:creator>Bob</dc:creator>
    </entry>
  </feed>
XML

doc = Nokogiri::XML(xml)

# Namespace inspection
ns = doc.collect_namespaces
raise "xmlns" unless ns["xmlns"] == "http://www.w3.org/2005/Atom"
raise "xmlns:dc" unless ns["xmlns:dc"] == "http://purl.org/dc/elements/1.1/"
raise "root ns" unless doc.root.namespace.href == "http://www.w3.org/2005/Atom"

# Automatic root prefix mapping (xmlns -> "xmlns:", dc -> "dc:")
creators = doc.xpath("//dc:creator")
raise "creators #{creators.map(&:text).inspect}" unless creators.map(&:text) == ["Alice", "Bob"]

titles = doc.xpath("//xmlns:entry/xmlns:title")
raise "default ns titles #{titles.map(&:text).inspect}" unless titles.map(&:text) == ["First", "Second"]

# Custom namespace mapping
custom = doc.xpath("//a:entry/d:creator", "a" => "http://www.w3.org/2005/Atom", "d" => "http://purl.org/dc/elements/1.1/")
raise "custom ns #{custom.map(&:text).inspect}" unless custom.map(&:text) == ["Alice", "Bob"]

# XPath variables
e20 = doc.at_xpath("//xmlns:entry[@id=$eid]", nil, eid: "20")
raise "xpath var" unless e20 && e20.at_xpath("./dc:creator").text == "Bob"

e10 = doc.at_xpath("//a:entry[@id=$eid]/a:title", {"a" => "http://www.w3.org/2005/Atom"}, eid: "10")
raise "xpath ns+var" unless e10 && e10.text == "First"

# Scalar XPath results
raise "count" unless doc.xpath("count(//xmlns:entry)") == 2.0
raise "string" unless doc.xpath("string(//xmlns:feed/xmlns:title)") == "News"
raise "boolean" unless doc.xpath("boolean(//dc:creator)") == true

# remove_namespaces!
doc.remove_namespaces!
raise "after remove_namespaces!" unless doc.xpath("//creator").map(&:text) == ["Alice", "Bob"]

# Invalid XPath raises Nokogiri::XML::XPath::SyntaxError
raised = false
begin
  doc.xpath("///[[[invalid")
rescue Nokogiri::XML::XPath::SyntaxError
  raised = true
end
raise "expected XPath::SyntaxError" unless raised
`)
}

func TestHTML4HTML5AndFragments(t *testing.T) {
	evalRuby(t, `
require "nokogiri"

html = <<~HTML
  <!DOCTYPE html>
  <html>
    <head><title>Hello Page</title></head>
    <body>
      <div id="main" class="box primary">
        <p>First <b>bold</b><br>line</p>
        <p class="note">Second</p>
        <script>if (a < b && c > d) {}</script>
      </div>
    </body>
  </html>
HTML

doc = Nokogiri::HTML(html)
raise "HTML4::Document" unless doc.is_a?(Nokogiri::HTML4::Document)
raise "html?" unless doc.html? && !doc.xml?
raise "title" unless doc.title == "Hello Page"
doc.title = "Updated Title"
raise "title=" unless doc.title == "Updated Title"

# Void elements & raw text serialization
out = doc.to_html
raise "br void element: #{out}" unless out.include?("<br>") && !out.include?("</br>")
raise "raw script text: #{out}" unless out.include?("if (a < b && c > d) {}")

# HTML5 API
doc5 = Nokogiri::HTML5("<div><p>Unclosed<span>tags</div>")
raise "HTML5::Document" unless doc5.is_a?(Nokogiri::HTML5::Document)
raise "html5 recovery" unless doc5.at_css("div p span").text == "tags"

# Nokogiri.parse auto-detection
raise "auto xml" unless Nokogiri.parse('<?xml version="1.0"?><a/>').is_a?(Nokogiri::XML::Document)
raise "auto html" unless Nokogiri.parse("<html><body>hi</body></html>").is_a?(Nokogiri::HTML4::Document)

# HTML DocumentFragment
frag = Nokogiri::HTML::DocumentFragment.parse("<p>one</p><p>two</p>")
raise "frag count #{frag.children.length}" unless frag.children.length == 2
raise "frag to_html" unless frag.to_html == "<p>one</p><p>two</p>"

frag5 = Nokogiri::HTML5.fragment("<span>a</span><span>b</span>")
raise "frag5" unless frag5.css("span").map(&:text) == ["a", "b"]

# XML DocumentFragment
xfrag = Nokogiri::XML::DocumentFragment.parse("<a/><b>hi</b>")
raise "xfrag" unless xfrag.element_children.length == 2
raise "xfrag xml" unless xfrag.to_xml == "<a/><b>hi</b>"
`)
}

func TestCSSSelectorsAndSearch(t *testing.T) {
	evalRuby(t, `
require "nokogiri"

html = <<~HTML
  <html><body>
    <ul id="nav" data-role="menu">
      <li class="item active"><a href="https://example.com/home">Home</a></li>
      <li class="item"><a href="https://example.com/about">About</a></li>
      <li class="item disabled"><a href="/contact">Contact</a></li>
    </ul>
    <section>
      <h2>Title</h2>
      <p class="lead">Intro</p>
      <p>Body</p>
    </section>
  </body></html>
HTML

doc = Nokogiri::HTML(html)

raise "id" unless doc.at_css("#nav")["data-role"] == "menu"
raise "class" unless doc.css("li.item").length == 3
raise "multi class" unless doc.at_css("li.item.active a").text == "Home"
raise "nth-child" unless doc.at_css("ul#nav > li:nth-child(2) a").text == "About"
raise "not" unless doc.css("li:not(.disabled)").length == 2
raise "attr ^=" unless doc.css('a[href^="https://"]').length == 2
raise "attr $=" unless doc.at_css('a[href$="/about"]').text == "About"
raise "attr *=" unless doc.at_css('a[href*="contact"]').text == "Contact"
raise "adjacent +" unless doc.at_css("h2 + p").text == "Intro"
raise "sibling ~" unless doc.css("h2 ~ p").length == 2
raise ":has" unless doc.at_css("li:has(a[href='/contact'])").classes.include?("disabled")

# Leading > child combinator scoped to element
nav = doc.at_css("#nav")
raise "leading >" unless nav.css("> li").length == 3

# Multi-selector
raise "multi sel" unless doc.css("h2", "p.lead").map(&:text) == ["Title", "Intro"]

# matches?
li1 = doc.at_css("li.active")
raise "matches css" unless li1.matches?("ul#nav > li.active")
raise "not matches css" if li1.matches?("li.disabled")
raise "matches xpath" unless li1.matches?("//li[@class='item active']")

# Unified search / / / at / %
raise "search css" unless (doc / "li.item").length == 3
raise "search xpath" unless (doc / "//li").length == 3
raise "at css" unless (doc % "h2").text == "Title"
raise "at xpath" unless (doc % "//h2").text == "Title"

# CSS syntax error
raised = false
begin
  doc.css("div[===")
rescue Nokogiri::CSS::SyntaxError
  raised = true
end
raise "expected CSS::SyntaxError" unless raised

# Nokogiri CSS.xpath_for & jQuery extensions (:contains, :first, :last, :eq, !=)
xps = Nokogiri::CSS.xpath_for("ul#nav > li.item a[href^='https://']")
raise "xpath_for=#{xps.inspect}" unless xps.length == 1 && xps[0].include?("starts-with(@href, 'https://')")
raise "css :contains" unless doc.at_css('a:contains("About")').text == "About"
raise "css :first" unless doc.at_css("li:first a").text == "Home"
raise "css :last" unless doc.at_css("li:last a").text == "Contact"
raise "css :eq" unless doc.at_css("li:eq(1) a").text == "About"
raise "css !=" unless doc.css('a[href!="/contact"]').length == 2

# XML Canonicalization (C14N 1.0 & Exclusive C14N)
cdoc = Nokogiri::XML('<root xmlns:b="urn:b" xmlns:a="urn:a" z="1" a="2"><empty/><!-- drop --><child b:y="1" a:x="2"/></root>')
c14n = cdoc.canonicalize
raise "c14n=#{c14n.inspect}" unless c14n == '<root xmlns:a="urn:a" xmlns:b="urn:b" a="2" z="1"><empty></empty><child a:x="2" b:y="1"></child></root>'
c14n_c = cdoc.canonicalize(Nokogiri::XML::XML_C14N_1_0, nil, true)
raise "c14n comments" unless c14n_c.include?("<!-- drop -->")
exc = cdoc.at_xpath("//empty").canonicalize(Nokogiri::XML::XML_C14N_EXCLUSIVE_1_0)
raise "exc c14n=#{exc.inspect}" unless exc == "<empty></empty>"
`)
}

func TestDOMMutationsAndNodeSet(t *testing.T) {
	evalRuby(t, `
require "nokogiri"

doc = Nokogiri::HTML("<html><body><div id='c'><p id='p1'>One</p><p id='p2'>Two</p></div></body></html>")
div = doc.at_css("#c")
p1 = doc.at_css("#p1")
p2 = doc.at_css("#p2")

# Siblings & ancestors
raise "next_element" unless p1.next_element == p2
raise "previous_element" unless p2.previous_element == p1
raise "ancestors" unless p1.ancestors.map(&:name) == ["div", "body", "html", "document"]
raise "ancestors(sel)" unless p1.ancestors("div").first == div

# add_child, <<, prepend_child, before, after
p3 = doc.create_element("p", "Three", id: "p3")
div << p3
raise "append" unless div.element_children.map(&:text) == ["One", "Two", "Three"]

p0 = doc.create_element("p", "Zero", id: "p0")
div.prepend_child(p0)
raise "prepend" unless div.first_element_child == p0

p1.before("<span>before-p1</span>")
p1.after("<span>after-p1</span>")
raise "before/after" unless div.element_children.map(&:name) == ["p", "span", "p", "span", "p", "p"]

# replace & unlink
p2.replace("<em id='em2'>Replaced</em>")
raise "replace" unless doc.at_css("#em2").text == "Replaced" && doc.at_css("#p2").nil?

p0.unlink
raise "unlink" unless doc.at_css("#p0").nil?

# wrap
p3.wrap("<article class='card'></article>")
raise "wrap" unless p3.parent.name == "article" && p3.parent["class"] == "card"

# classes / add_class / remove_class
div.add_class("alpha beta")
div.add_class("beta gamma")
raise "classes #{div.classes.inspect}" unless div.classes == ["alpha", "beta", "gamma"]
div.remove_class("beta")
raise "remove_class" unless div.classes == ["alpha", "gamma"]
div.remove_class
raise "remove all class" unless div["class"].nil?

# inner_html= & content=
div.inner_html = "<a href='#1'>L1</a><a href='#2'>L2</a>"
raise "inner_html=" unless div.css("a").map(&:text) == ["L1", "L2"]
div.at_css("a").content = "Updated <1>"
raise "content= escaped" unless div.at_css("a").to_html == '<a href="#1">Updated &lt;1&gt;</a>'

# dup
copy = div.dup
copy["id"] = "copy"
raise "dup independent" unless div["id"] == "c" && copy.css("a").length == 2

# traverse (post-order)
visited = []
Nokogiri::XML("<a><b><c/></b><d/></a>").root.traverse { |n| visited << n.name }
raise "traverse #{visited.inspect}" unless visited == ["c", "b", "d", "a"]

# NodeSet algebra, slicing, chaining
doc2 = Nokogiri::XML("<r><x id='1'/><x id='2'/><x id='3'/><y id='4'/></r>")
xs = doc2.xpath("//x")
ys = doc2.xpath("//y")
all = xs | ys
raise "union" unless all.length == 4
raise "diff" unless (all - ys) == xs
raise "intersect" unless (all & ys) == ys
raise "slice int" unless xs[-1]["id"] == "3"
raise "slice range" unless xs[0..1].map { |n| n["id"] } == ["1", "2"]
raise "slice len" unless xs[1, 2].map { |n| n["id"] } == ["2", "3"]
raise "reverse" unless xs.reverse.map { |n| n["id"] } == ["3", "2", "1"]
raise "filter" unless all.filter("x[id='2']").length == 1
xs.attr("data-ok", "1")
raise "nodeset attr" unless xs.all? { |n| n["data-ok"] == "1" }
xs.remove
raise "nodeset remove" unless doc2.xpath("//x").empty? && doc2.xpath("//y").length == 1
`)
}

func TestBuilderSAXReaderAndSlop(t *testing.T) {
	evalRuby(t, `
require "nokogiri"

# XML::Builder
b = Nokogiri::XML::Builder.new(encoding: "UTF-8") do |xml|
  xml.root("xmlns" => "http://example.com/ns", id: "r1") do
    xml.comment("generated")
    xml.item("Hello", count: 3)
    xml.send_("raw")
    xml.cdata("a < b")
  end
end
out = b.to_xml
raise "builder xml decl" unless out.start_with?('<?xml version="1.0" encoding="UTF-8"?>')
raise "builder root" unless b.doc.root["id"] == "r1"
raise "builder send_" unless b.doc.at_xpath("//xmlns:send").text == "raw"
raise "builder cdata" unless out.include?("<![CDATA[a < b]]>")

# Builder.with
Nokogiri::XML::Builder.with(b.doc.root) do |xml|
  xml.extra("more")
end
raise "Builder.with" unless b.doc.root.last_element_child.name == "extra"

# HTML::Builder
hb = Nokogiri::HTML::Builder.new do |html|
  html.div(class: "hi") { html.span("world") }
end
raise "html builder: #{hb.to_html}" unless hb.to_html == '<div class="hi"><span>world</span></div>'

# XML::SAX::Parser & PushParser
class MySAX < Nokogiri::XML::SAX::Document
  attr_reader :events
  def initialize; @events = []; end
  def start_document; @events << [:start_doc]; end
  def end_document; @events << [:end_doc]; end
  def start_element(name, attrs = []); @events << [:start, name, attrs]; end
  def start_element_namespace(name, attrs = [], prefix = nil, uri = nil, ns = [])
    @events << [:start_ns, name, prefix, uri, attrs.map { |a| [a.localname, a.value] }]
  end
  def characters(s); @events << [:text, s] unless s.strip.empty?; end
  def cdata_block(s); @events << [:cdata, s]; end
  def comment(s); @events << [:comment, s.strip]; end
  def end_element(name); @events << [:end, name]; end
end

handler = MySAX.new
parser = Nokogiri::XML::SAX::Parser.new(handler)
parser.parse('<ns:root xmlns:ns="urn:test" k="v"><!-- hi --><child><![CDATA[raw]]>txt</child></ns:root>')
raise "sax start_doc" unless handler.events.first == [:start_doc]
raise "sax end_doc" unless handler.events.last == [:end_doc]
raise "sax ns event" unless handler.events.include?([:start_ns, "root", "ns", "urn:test", [["k", "v"]]])
raise "sax cdata" unless handler.events.include?([:cdata, "raw"])
raise "sax comment" unless handler.events.include?([:comment, "hi"])
raise "sax text" unless handler.events.include?([:text, "txt"])

handler2 = MySAX.new
push = Nokogiri::XML::SAX::PushParser.new(handler2)
push << "<a>"
push << "<b>hi</b>"
push << "</a>"
push.finish
raise "push parser" unless handler2.events.include?([:text, "hi"])

# HTML4::SAX::Parser
hhandler = MySAX.new
Nokogiri::HTML4::SAX::Parser.new(hhandler).parse("<p>hello</p>")
raise "html sax" unless hhandler.events.include?([:text, "hello"])

# XML::Reader
reader = Nokogiri::XML::Reader.from_memory('<root><item id="1">A</item><empty/></root>')
seen = []
reader.each do |r|
  if r.node_type == Nokogiri::XML::Reader::TYPE_ELEMENT
    seen << [r.name, r.depth, r.attribute("id"), r.self_closing?]
  end
end
expected = [
  ["root", 0, nil, false],
  ["item", 1, "1", false],
  ["empty", 1, nil, true],
]
raise "reader seen=#{seen.inspect}" unless seen == expected

# Nokogiri.Slop
slop = Nokogiri.Slop("<employees><employee><title>Engineer</title></employee></employees>")
raise "slop" unless slop.employees.employee.title.text == "Engineer"

# Attr#remove, Attr#value=, encode_special_chars, VersionInfo, uses_gumbo?
doc_attr = Nokogiri::HTML("<div id='a' onclick='evil()'><b>keep</b></div>")
div = doc_attr.at_css("div")
div.attribute_nodes.each { |a| a.remove if a.name == "onclick" }
raise "attr remove kept children" unless div.to_html == '<div id="a"><b>keep</b></div>'
div.attribute("id").value = "b"
raise "attr value=" unless div["id"] == "b"
raise "encode_special_chars" unless div.encode_special_chars("a & <b>") == "a &amp; &lt;b&gt;"
raise "uses_gumbo?" unless Nokogiri.uses_gumbo? == true
raise "VersionInfo" unless Nokogiri::VersionInfo.instance.libxml2? == false

# Subclassing Document & DocumentFragment + decorators + dup
module CustomNodeBehavior
  def shout; text.upcase; end
end
class CustomDoc < Nokogiri::HTML5::Document
  def initialize(*args, &block)
    super
    decorators(Nokogiri::XML::Node) << CustomNodeBehavior
  end
end
class CustomFrag < Nokogiri::HTML5::DocumentFragment
  def self.parse(tags)
    new(CustomDoc.new, tags)
  end
end
cdoc = CustomDoc.parse("<p>hello</p>")
raise "custom doc class" unless cdoc.is_a?(CustomDoc)
raise "custom doc decorator" unless cdoc.at_css("p").shout == "HELLO"
raise "custom doc dup preserves subclass" unless cdoc.dup.is_a?(CustomDoc) && cdoc.dup.at_css("p").shout == "HELLO"
cfrag = CustomFrag.parse("<span>world</span>")
raise "custom frag class" unless cfrag.is_a?(CustomFrag)
raise "custom frag decorator" unless cfrag.at_css("span").shout == "WORLD"
`)
}
