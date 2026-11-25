// Package orubyredcarpet implements the Ruby Redcarpet Markdown gem backed by
// Goldmark (pure Go), registered as "oruby-redcarpet" and aliased to "redcarpet".
package orubyredcarpet

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
)

func init() {
	oruby.LazyGem("oruby-redcarpet", initRedcarpet)
	oruby.GemAlias("redcarpet", "oruby-redcarpet")
	oruby.GemAlias("redcarpet/render_strip", "oruby-redcarpet")
}

func hashBool(mrb *oruby.MrbState, h oruby.Value, key string) bool {
	if h.IsNil() || !h.IsHash() {
		return false
	}
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern(key).Value()); err == nil && !v.IsNil() {
		return v.Bool()
	}
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.StrNew(key)); err == nil && !v.IsNil() {
		return v.Bool()
	}
	return false
}

var (
	reStripTags = regexp.MustCompile(`<[^>]+>`)
	reImgTag    = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	reLinkOpen  = regexp.MustCompile(`(?is)<a\b[^>]*>`)
	reLinkClose = regexp.MustCompile(`(?is)</a>`)
	reHeading   = regexp.MustCompile(`(?is)<h([1-6])(?:\s+id="([^"]*)")?[^>]*>(.*?)</h[1-6]>`)
)

func renderMarkdown(mrb *oruby.MrbState, text string, rOpts, mExts oruby.Value, mode string) (string, error) {
	var exts []goldmark.Extender
	if hashBool(mrb, mExts, "tables") {
		exts = append(exts, extension.Table)
	}
	if hashBool(mrb, mExts, "strikethrough") {
		exts = append(exts, extension.Strikethrough)
	}
	if hashBool(mrb, mExts, "autolink") {
		exts = append(exts, extension.Linkify)
	}
	if hashBool(mrb, mExts, "footnotes") {
		exts = append(exts, extension.Footnote)
	}
	if mode == "smarty" || hashBool(mrb, rOpts, "smart_pants") {
		exts = append(exts, extension.Typographer)
	}

	var pOpts []parser.Option
	if hashBool(mrb, rOpts, "with_toc_data") || mode == "toc" {
		pOpts = append(pOpts, parser.WithAutoHeadingID())
	}

	var htmlOpts []renderer.Option
	if !hashBool(mrb, rOpts, "filter_html") && !hashBool(mrb, rOpts, "escape_html") {
		htmlOpts = append(htmlOpts, html.WithUnsafe())
	}
	if hashBool(mrb, rOpts, "hard_wrap") {
		htmlOpts = append(htmlOpts, html.WithHardWraps())
	}
	if hashBool(mrb, rOpts, "xhtml") || mode == "xhtml" {
		htmlOpts = append(htmlOpts, html.WithXHTML())
	}

	md := goldmark.New(
		goldmark.WithExtensions(exts...),
		goldmark.WithParserOptions(pOpts...),
		goldmark.WithRendererOptions(htmlOpts...),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(text), &buf); err != nil {
		return "", err
	}
	out := buf.String()

	if mode == "strip" {
		return strings.TrimSpace(reStripTags.ReplaceAllString(out, "")) + "\n", nil
	}
	if mode == "toc" {
		var sb strings.Builder
		matches := reHeading.FindAllStringSubmatch(out, -1)
		if len(matches) > 0 {
			sb.WriteString("<ul>\n")
			for _, m := range matches {
				id := m[2]
				label := reStripTags.ReplaceAllString(m[3], "")
				sb.WriteString("<li><a href=\"#")
				sb.WriteString(id)
				sb.WriteString("\">")
				sb.WriteString(label)
				sb.WriteString("</a></li>\n")
			}
			sb.WriteString("</ul>\n")
		}
		return sb.String(), nil
	}
	if hashBool(mrb, rOpts, "no_images") {
		out = reImgTag.ReplaceAllString(out, "")
	}
	if hashBool(mrb, rOpts, "no_links") {
		out = reLinkOpen.ReplaceAllString(out, "")
		out = reLinkClose.ReplaceAllString(out, "")
	}
	return out, nil
}

func initRedcarpet(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Redcarpet")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("3.6.0"))

	mod.DefineClassMethod("__render", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		text := a[0].String()
		rOpts := a[1]
		mExts := a[2]
		mode := a[3].String()
		out, err := renderMarkdown(mrb, text, rOpts, mExts, mode)
		if err != nil {
			return mrb.Raise(mrb.ERuntimeError(), err.Error())
		}
		return mrb.StrNew(out)
	}, mrb.ArgsReq(4))

	rb.LoadRuby(mrb, "<internal:oruby-redcarpet>", []byte(`
module Redcarpet
  module Render
    class Base
      attr_reader :options

      def initialize(options = {})
        @options = options || {}
      end

      def __mode
        "html"
      end
    end

    class HTML < Base
    end

    class XHTML < HTML
      def initialize(options = {})
        super({ xhtml: true }.merge(options || {}))
      end

      def __mode
        "xhtml"
      end
    end

    class Safe < HTML
      def initialize(options = {})
        super({ filter_html: true, no_styles: true, safe_links_only: true }.merge(options || {}))
      end
    end

    class SmartyHTML < HTML
      def __mode
        "smarty"
      end
    end

    class HTML_TOC < Base
      def __mode
        "toc"
      end
    end

    class StripDown < Base
      def __mode
        "strip"
      end
    end
  end

  class Markdown
    attr_reader :renderer, :extensions

    def initialize(renderer, extensions = {})
      @renderer = renderer.is_a?(Class) ? renderer.new : renderer
      @extensions = extensions || {}
    end

    def render(text)
      src = text.to_s
      src = @renderer.preprocess(src) if @renderer.respond_to?(:preprocess)
      opts = @renderer.respond_to?(:options) ? @renderer.options : {}
      mode = @renderer.respond_to?(:__mode) ? @renderer.__mode : "html"
      out = Redcarpet.__render(src, opts, @extensions, mode)
      out = @renderer.postprocess(out) if @renderer.respond_to?(:postprocess)
      out
    end
  end
end
`))
	return nil
}
