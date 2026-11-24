package nokogiri_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/nokogiri"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestBuilderNamespacesAndSaveOptions(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	val, err := mrb.Eval(`
		require "nokogiri"
		b = Nokogiri::XML::Builder.new(encoding: "UTF-8") do |xml|
			xml.Invoice("xmlns" => "urn:inv", "xmlns:cbc" => "urn:cbc", "xmlns:cac" => "urn:cac") {
				xml["cbc"].ID("INV-2026-01")
				xml["cbc"].PayableAmount("125.00", currencyID: "EUR")
				xml["cac"].Party {
					xml["cbc"].Name("Acme")
					xml.EmptyTag
				}
			}
		end
		doc = b.doc
		ns = { "inv" => "urn:inv", "cbc" => "urn:cbc", "cac" => "urn:cac" }

		opts_no_decl = Nokogiri::XML::Node::SaveOptions.new.no_declaration.no_empty_tags.as_xml
		xml_no_decl = doc.to_xml(save_with: opts_no_decl)
		xml_fmt = doc.to_xml(save_with: Nokogiri::XML::Node::SaveOptions::FORMAT | Nokogiri::XML::Node::SaveOptions::NO_DECLARATION)

		buf = +""
		doc.write_xml_to(buf, save_with: opts_no_decl)

		[
			b.arity,
			doc.at_xpath("//cbc:ID", ns).text,
			doc.at_xpath("//cbc:PayableAmount", ns)["currencyID"],
			doc.at_xpath("//cac:Party/cbc:Name", ns).text,
			doc.at_xpath("//cbc:ID", ns).namespace.href,
			!xml_no_decl.start_with?("<?xml"),
			xml_no_decl.include?("<cac:EmptyTag></cac:EmptyTag>"),
			xml_fmt.include?("\n  <cbc:ID>"),
			buf == xml_no_decl,
		]
	`)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	res := mrb.Intf(val).([]any)
	if res[0].(int) != 1 {
		t.Fatalf("expected arity 1, got %v", res[0])
	}
	if res[1] != "INV-2026-01" || res[2] != "EUR" || res[3] != "Acme" {
		t.Fatalf("builder xpath mismatch: %v", res[1:4])
	}
	if res[4] != "urn:cbc" {
		t.Fatalf("expected inherited prefix ns urn:cbc, got %v", res[4])
	}
	for i := 5; i < len(res); i++ {
		if res[i] != true {
			t.Fatalf("save_options assertion %d failed: %v", i, res)
		}
	}
}

func TestNodeNodeSetNamespaceAndHTMLHelpers(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	val, err := mrb.Eval(`
		require "nokogiri"

		# Node helpers & pattern matching
		doc = Nokogiri::XML('<root xmlns:a="urn:a" xml:lang="en"><item id="i1" a:code="X1" class="btn primary">One</item><item class="btn">Two</item></root>')
		item1 = doc.at_css("#i1")
		item1.append_class("primary")
		item1.kwattr_add("data-tags", "alpha beta")
		item1.kwattr_remove("data-tags", "alpha")
		item1.line = 42

		item1 => { name: matched_name, content: matched_content }
		ns_def = doc.root.namespace_definitions.first
		ns_def => { prefix: ns_pref, href: ns_href }

		visited = []
		visitor = Object.new
		visitor.define_singleton_method(:visit) { |n| visited << n.name }
		item1.accept(visitor)

		# NodeSet helpers
		items = doc.root > "item"
		items.append_class("extra")
		items.attr("data-k", "v")
		items.remove_attr("data-k")
		first_item, second_item = *items.deconstruct

		# create_internal_subset / default_namespace=
		doc2 = Nokogiri::XML("<doc/>")
		dtd = doc2.create_internal_subset("doc", "-//PUB//EN", "http://example.com/doc.dtd")
		doc2.root.default_namespace = "urn:default"

		# HTML4 meta_encoding
		h4 = Nokogiri::HTML4('<html><head><meta charset="iso-8859-1"><title>Hi</title></head><body><p>ok</p></body></html>')
		old_enc = h4.meta_encoding
		h4.meta_encoding = "UTF-8"
		new_enc = h4.meta_encoding

		# HTML5 quirks_mode & options
		h5_std = Nokogiri::HTML5("<!DOCTYPE html><html><body><p>ok</p></body></html>", parse_noscript_content_as_text: true)
		h5_quirks = Nokogiri::HTML5("<html><body><p>no doctype</p></body></html>")
		h5_deep = Nokogiri::HTML5("<!DOCTYPE html><div><div><div><div>x</div></div></div></div>", max_tree_depth: 2, max_errors: 1)

		# Reader extras
		rdr = Nokogiri::XML::Reader('<root xmlns:p="urn:p" xml:lang="de"><p:item a="1" b="2">hi</p:item></root>', "http://example.com/test.xml", "UTF-8")
		rdr_info = nil
		rdr.each do |node|
			if node.node_type == Nokogiri::XML::Reader::TYPE_ELEMENT && node.local_name == "item"
				rdr_info = [
					node.attribute_count,
					node.attribute_at(1),
					node.attributes?,
					node.prefix,
					node.namespace_uri,
					node.lang,
					node.encoding,
					node.base_uri,
					node.xml_version,
					node.value?,
					node.default?,
					node.state,
					node.source.start_with?("<root"),
				]
			end
		end

		[
			item1.css_path,
			item1.attribute_with_ns("code", "urn:a").value,
			item1.namespaced_key?("code", "urn:a"),
			item1.namespaced_key?("code", "urn:other"),
			item1.classes,
			item1.kwattr_values("data-tags"),
			item1.lang,
			item1.line,
			item1.to_str,
			item1.value?("X1"),
			item1.read_only?,
			item1.namespace_scopes.map(&:prefix),
			matched_name,
			matched_content,
			ns_pref,
			ns_href,
			ns_def.document == doc,
			visited,
			items.length,
			first_item["data-k"].nil?,
			second_item.classes.include?("extra"),
			dtd.external_id,
			dtd.system_id,
			doc2.root.namespace.href,
			old_enc,
			new_enc,
			h5_std.quirks_mode == Nokogiri::HTML5::QuirksMode::NO_QUIRKS,
			h5_std.parse_noscript_content_as_text,
			h5_quirks.quirks_mode == Nokogiri::HTML5::QuirksMode::QUIRKS,
			h5_deep.errors.length <= 1 && h5_deep.errors.length > 0,
			rdr_info,
		]
	`)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	res := mrb.Intf(val).([]any)
	if res[0] != "#i1" {
		t.Fatalf("expected css_path '#i1', got %v", res[0])
	}
	if res[1] != "X1" || res[2] != true || res[3] != false {
		t.Fatalf("attribute_with_ns / namespaced_key? failed: %v", res[1:4])
	}
	if res[6] != "en" || res[7].(int) != 42 || res[8] != "One" || res[9] != true || res[10] != false {
		t.Fatalf("node scalar helpers failed: %v", res[6:11])
	}
	if res[12] != "item" || res[13] != "One" || res[14] != "a" || res[15] != "urn:a" || res[16] != true {
		t.Fatalf("pattern matching / namespace failed: %v", res[12:17])
	}
	if res[18].(int) != 2 || res[19] != true || res[20] != true {
		t.Fatalf("nodeset helpers failed: %v", res[18:21])
	}
	if res[21] != "-//PUB//EN" || res[22] != "http://example.com/doc.dtd" || res[23] != "urn:default" {
		t.Fatalf("subset / default_namespace failed: %v", res[21:24])
	}
	if res[24] != "iso-8859-1" || res[25] != "UTF-8" {
		t.Fatalf("meta_encoding failed: %v %v", res[24], res[25])
	}
	if res[26] != true || res[27] != true || res[28] != true || res[29] != true {
		t.Fatalf("HTML5 options/quirks failed: %v", res[26:30])
	}
	rdrInfo := res[30].([]any)
	if rdrInfo[0].(int) != 2 || rdrInfo[1] != "2" || rdrInfo[2] != true || rdrInfo[3] != "p" ||
		rdrInfo[4] != "urn:p" || rdrInfo[5] != "de" || rdrInfo[6] != "UTF-8" ||
		rdrInfo[7] != "http://example.com/test.xml" || rdrInfo[8] != "1.0" ||
		rdrInfo[9] != false || rdrInfo[10] != false || rdrInfo[11].(int) != 1 || rdrInfo[12] != true {
		t.Fatalf("Reader extras failed: %v", rdrInfo)
	}
}

func TestXPathCustomRubyHandler(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	val, err := mrb.Eval(`
		require "nokogiri"
		doc = Nokogiri::XML('<ul><li class="item active">A</li><li class="item">B</li><li class="active special">C</li></ul>')

		handler = Class.new do
			def has_class(node_set, cls)
				node_set.any? { |n| n.classes.include?(cls) }
			end

			def count_text(node_set)
				node_set.map(&:text).join("-")
			end
		end.new

		active = doc.xpath("//li[has_class(., 'active')]", nil, handler).map(&:text)
		active_short = doc.xpath("//li[has_class(., 'active')]", handler).map(&:text)
		inactive = doc.xpath("//li[not(has_class(., 'active'))]", handler).map(&:text)
		joined = doc.xpath("count_text(//li)", handler)
		[active, active_short, inactive, joined]
	`)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	res := mrb.Intf(val).([]any)
	active := res[0].([]any)
	activeShort := res[1].([]any)
	inactive := res[2].([]any)
	if len(active) != 2 || active[0] != "A" || active[1] != "C" {
		t.Fatalf("expected [A, C], got %v", active)
	}
	if len(activeShort) != 2 || activeShort[0] != "A" || activeShort[1] != "C" {
		t.Fatalf("expected [A, C] with shorthand, got %v", activeShort)
	}
	if len(inactive) != 1 || inactive[0] != "B" {
		t.Fatalf("expected [B], got %v", inactive)
	}
	if res[3] != "A-B-C" {
		t.Fatalf("expected 'A-B-C', got %v", res[3])
	}
}

func TestRelaxNGAndDTDValidation(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	val, err := mrb.Eval(`
		require "nokogiri"

		# 1. RelaxNG validation
		rng_src = <<~RNG
			<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start>
					<element name="addressBook">
						<oneOrMore>
							<ref name="card"/>
						</oneOrMore>
					</element>
				</start>
				<define name="card">
					<element name="card">
						<attribute name="id"><data type="integer"/></attribute>
						<interleave>
							<element name="name"><text/></element>
							<element name="email"><text/></element>
						</interleave>
					</element>
				</define>
			</grammar>
		RNG

		rng = Nokogiri::XML::RelaxNG(rng_src)
		good = Nokogiri::XML('<addressBook><card id="1"><email>a@b.com</email><name>Alice</name></card></addressBook>')
		bad_attr = Nokogiri::XML('<addressBook><card id="abc"><name>Alice</name><email>a@b.com</email></card></addressBook>')
		bad_empty = Nokogiri::XML('<addressBook/>')

		# 2. DTD internal subset validation
		dtd_good = Nokogiri::XML(<<~XML)
			<!DOCTYPE note [
				<!ELEMENT note (to, from, body)>
				<!ELEMENT to (#PCDATA)>
				<!ELEMENT from (#PCDATA)>
				<!ELEMENT body (#PCDATA)>
				<!ATTLIST note id ID #REQUIRED>
				<!ENTITY sig "Best regards">
			]>
			<note id="n1"><to>Bob</to><from>Alice</from><body>Hello</body></note>
		XML

		dtd_bad = Nokogiri::XML(<<~XML)
			<!DOCTYPE note [
				<!ELEMENT note (to, from)>
				<!ELEMENT to (#PCDATA)>
				<!ELEMENT from (#PCDATA)>
				<!ATTLIST note id ID #REQUIRED>
			]>
			<note><to>Bob</to><extra>Oops</extra></note>
		XML

		dtd = dtd_good.internal_subset
		[
			rng.valid?(good),
			rng.valid?(bad_attr),
			rng.valid?(bad_empty),
			dtd.elements.keys.sort,
			dtd.entities["sig"],
			dtd_good.validate.empty?,
			dtd_bad.validate.length >= 2,
		]
	`)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	res := mrb.Intf(val).([]any)
	if res[0] != true || res[1] != false || res[2] != false {
		t.Fatalf("RelaxNG validation failed: %v", res[0:3])
	}
	if res[4] != "Best regards" || res[5] != true || res[6] != true {
		t.Fatalf("DTD validation failed: %v", res[3:])
	}
}

func TestXIncludeStreamingSAXAndXSLTRegister(t *testing.T) {
	tmp := t.TempDir()
	partXML := filepath.Join(tmp, "part.xml")
	partTxt := filepath.Join(tmp, "note.txt")
	mainXML := filepath.Join(tmp, "main.xml")

	if err := os.WriteFile(partXML, []byte(`<included id="42"><msg>from-file</msg></included>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partTxt, []byte(`plain text payload`), 0o600); err != nil {
		t.Fatal(err)
	}
	mainContent := `<doc xmlns:xi="http://www.w3.org/2001/XInclude">` +
		`<xi:include href="part.xml"/>` +
		`<txt><xi:include href="note.txt" parse="text"/></txt>` +
		`<xi:include href="missing.xml"><xi:fallback><fb>recovered</fb></xi:fallback></xi:include>` +
		`</doc>`
	if err := os.WriteFile(mainXML, []byte(mainContent), 0o600); err != nil {
		t.Fatal(err)
	}

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.DefineGlobalConst("TEST_MAIN_XML", mrb.StrNew(mainXML))

	val, err := mrb.Eval(`
		require "nokogiri"

		# 1. XInclude
		doc = Nokogiri::XML(File.read(TEST_MAIN_XML), TEST_MAIN_XML)
		doc.do_xinclude

		# 2. Streaming SAX PushParser
		events = []
		handler = Class.new(Nokogiri::XML::SAX::Document) do
			define_method(:start_document) { events << :start_doc }
			define_method(:end_document) { events << :end_doc }
			define_method(:start_element) { |name, attrs| events << [:start, name, attrs] }
			define_method(:characters) { |s| events << [:chars, s] unless s.strip.empty? }
			define_method(:end_element) { |name| events << [:end, name] }
		end.new

		push = Nokogiri::XML::SAX::PushParser.new(handler)
		push << "<root><item k='1'>"
		push << "hello"
		push << "</item></root>"
		push.finish

		# 3. XSLT.register & Schema#parse_options
		ext_cls = Class.new
		Nokogiri::XSLT.register("http://example.com/ext", ext_cls)
		registered = Nokogiri::XSLT.registered_extensions["http://example.com/ext"] == ext_cls

		[
			doc.at_xpath("//included/@id").value,
			doc.at_xpath("//included/msg").text,
			doc.at_xpath("//txt").text,
			doc.at_xpath("//fb").text,
			events.first == :start_doc && events.last == :end_doc,
			events.include?([:chars, "hello"]),
			events.any? { |e| e.is_a?(Array) && e[0] == :start && e[1] == "item" },
			registered,
		]
	`)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	res := mrb.Intf(val).([]any)
	if res[0] != "42" || res[1] != "from-file" || res[2] != "plain text payload" || res[3] != "recovered" {
		t.Fatalf("XInclude failed: %v", res[0:4])
	}
	for i := 4; i < len(res); i++ {
		if res[i] != true {
			t.Fatalf("assertion %d failed: %v", i, res)
		}
	}
}
