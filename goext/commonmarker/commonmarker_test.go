package commonmarker_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/commonmarker"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
)

func TestCommonmarker(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "commonmarker"

html = Commonmarker.to_html("# Hello\n\n**bold** ~~strike~~ https://example.com\n\n| A | B |\n|---|---|\n| 1 | 2 |\n")
raise "h1" unless html.include?("<h1") && html.include?("Hello</h1>")
raise "bold" unless html.include?("<strong>bold</strong>")
raise "strike" unless html.include?("<del>strike</del>")
raise "autolink" unless html.include?('<a href="https://example.com">')
raise "table" unless html.include?("<table>") && html.include?("<td>1</td>")

# Raw HTML omitted by default, preserved with render: { unsafe: true }
safe = Commonmarker.to_html("<div>hi</div>")
raise "safe omitted" if safe.include?("<div>hi</div>")
unsafe = Commonmarker.to_html("<div>hi</div>", options: { render: { unsafe: true } })
raise "unsafe kept" unless unsafe.include?("<div>hi</div>")

# Parse -> Node#to_html
doc = Commonmarker.parse("* item 1\n* item 2")
raise "doc type" unless doc.type == :document
raise "doc html" unless doc.to_html.include?("<li>item 1</li>")

# Legacy CommonMarker API
legacy = CommonMarker.render_html("**hi** ~~no~~", :DEFAULT, [:strikethrough])
raise "legacy" unless legacy.include?("<strong>hi</strong>") && legacy.include?("<del>no</del>")
`)
	if err != nil {
		t.Fatalf("commonmarker test failed: %v", err)
	}
}
