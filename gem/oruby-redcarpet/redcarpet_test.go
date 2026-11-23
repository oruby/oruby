package orubyredcarpet_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/oruby-redcarpet"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestRedcarpet(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "redcarpet"

md = Redcarpet::Markdown.new(Redcarpet::Render::HTML.new(hard_wrap: true, with_toc_data: true), tables: true, strikethrough: true, autolink: true)
html = md.render("# Hello\n\n~~old~~ **new** https://example.com\n\n| A | B |\n|---|---|\n| 1 | 2 |\n")
raise "h1: #{html}" unless html.include?("<h1") && html.include?("Hello</h1>")
raise "del: #{html}" unless html.include?("<del>old</del>")
raise "strong: #{html}" unless html.include?("<strong>new</strong>")
raise "table: #{html}" unless html.include?("<table>") && html.include?("<td>1</td>")

toc = Redcarpet::Markdown.new(Redcarpet::Render::HTML_TOC).render("# Intro\n## Details\n")
raise "toc: #{toc}" unless toc.include?("<ul>") && toc.include?("Intro") && toc.include?("Details")

stripped = Redcarpet::Markdown.new(Redcarpet::Render::StripDown).render("# Title\n\n**bold** text")
raise "strip: #{stripped}" unless stripped.include?("Title") && !stripped.include?("<strong>")

custom = Class.new(Redcarpet::Render::HTML) do
  def postprocess(doc)
    doc.upcase
  end
end
raise "postprocess" unless Redcarpet::Markdown.new(custom.new).render("hi").include?("<P>HI</P>")
`)
	if err != nil {
		t.Fatalf("redcarpet test failed: %v", err)
	}
}
