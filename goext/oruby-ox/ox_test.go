package orubyox_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/oruby-ox"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestOx(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "ox"

xml = '<catalog><book id="1"><title>ORuby</title><price>10</price></book><book id="2"><title>Go</title></book></catalog>'
root = Ox.parse(xml)
raise "root" unless root.is_a?(Ox::Element) && root.name == "catalog"
raise "locate titles" unless root.locate("book/title/^Text") == ["ORuby", "Go"]
raise "locate ids" unless root.locate("book/@id") == ["1", "2"]

# Hash mode
h = Ox.load(xml, mode: :hash)
raise "hash mode: #{h}" unless h[:catalog][:book].length == 2 && h[:catalog][:book][0][:id] == "1" && h[:catalog][:book][0][:title] == "ORuby"

# Dump Element
el = Ox::Element.new("item")
el[:code] = "A&B"
el << "hello <world>"
dumped = Ox.dump(el, indent: -1)
raise "dump: #{dumped}" unless dumped == '<item code="A&amp;B">hello &lt;world&gt;</item>'

# Streaming SAX
events = []
handler = Class.new(Ox::Sax) do
  define_method(:start_element) { |n| events << [:start, n] }
  define_method(:attr) { |k, v| events << [:attr, k, v] }
  define_method(:text) { |s| events << [:text, s] }
  define_method(:end_element) { |n| events << [:end, n] }
end.new
Ox.sax_parse(handler, '<a k="v">hi</a>')
raise "sax: #{events}" unless events == [[:start, :a], [:attr, :k, "v"], [:text, "hi"], [:end, :a]]
`)
	if err != nil {
		t.Fatalf("ox test failed: %v", err)
	}
}
