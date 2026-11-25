// Package commonmarker provides a pure-Go implementation of the Ruby
// commonmarker (v1/v2) and CommonMarker (v0.23) gems backed by
// github.com/yuin/goldmark (CommonMark + GFM).
package commonmarker

import (
	"bytes"
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
	oruby.LazyGem("commonmarker", initCommonmarker)
	oruby.GemAlias("common_marker", "commonmarker")
}

type mdOpts struct {
	unsafe        bool
	hardbreaks    bool
	strikethrough bool
	table         bool
	autolink      bool
	tasklist      bool
	footnotes     bool
	smart         bool
}

func defaultMDOpts() mdOpts {
	return mdOpts{
		strikethrough: true,
		table:         true,
		autolink:      true,
		tasklist:      true,
		footnotes:     true,
	}
}

func renderMarkdown(src string, o mdOpts) (string, error) {
	var exts []goldmark.Extender
	if o.table {
		exts = append(exts, extension.Table)
	}
	if o.strikethrough {
		exts = append(exts, extension.Strikethrough)
	}
	if o.autolink {
		exts = append(exts, extension.Linkify)
	}
	if o.tasklist {
		exts = append(exts, extension.TaskList)
	}
	if o.footnotes {
		exts = append(exts, extension.Footnote)
	}
	if o.smart {
		exts = append(exts, extension.Typographer)
	}

	var ropts []renderer.Option
	if o.unsafe {
		ropts = append(ropts, html.WithUnsafe())
	}
	if o.hardbreaks {
		ropts = append(ropts, html.WithHardWraps())
	}

	md := goldmark.New(
		goldmark.WithExtensions(exts...),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(ropts...),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func parseModernOpts(mrb *oruby.MrbState, h oruby.Value) mdOpts {
	o := defaultMDOpts()
	if h.IsNil() || !h.IsHash() {
		return o
	}
	lookup := func(hash oruby.Value, key string) (oruby.Value, bool) {
		if v, err := mrb.Funcall(hash, mrb.Intern("[]"), mrb.Intern(key).Value()); err == nil && !v.IsNil() {
			return v, true
		}
		if v, err := mrb.Funcall(hash, mrb.Intern("[]"), mrb.StrNew(key)); err == nil && !v.IsNil() {
			return v, true
		}
		return mrb.NilValue(), false
	}
	if r, ok := lookup(h, "render"); ok && r.IsHash() {
		if v, ok := lookup(r, "unsafe"); ok {
			o.unsafe = v.Bool()
		}
		if v, ok := lookup(r, "hardbreaks"); ok {
			o.hardbreaks = v.Bool()
		}
	}
	if p, ok := lookup(h, "parse"); ok && p.IsHash() {
		if v, ok := lookup(p, "smart"); ok {
			o.smart = v.Bool()
		}
	}
	if e, ok := lookup(h, "extension"); ok && e.IsHash() {
		if v, ok := lookup(e, "strikethrough"); ok {
			o.strikethrough = v.Bool()
		}
		if v, ok := lookup(e, "table"); ok {
			o.table = v.Bool()
		}
		if v, ok := lookup(e, "autolink"); ok {
			o.autolink = v.Bool()
		}
		if v, ok := lookup(e, "tasklist"); ok {
			o.tasklist = v.Bool()
		}
		if v, ok := lookup(e, "footnotes"); ok {
			o.footnotes = v.Bool()
		}
	}
	return o
}

func parseLegacyOpts(mrb *oruby.MrbState, optsVal, extsVal oruby.Value) mdOpts {
	o := mdOpts{}
	addOpt := func(v oruby.Value) {
		name := strings.ToUpper(mrb.String(v))
		switch name {
		case "UNSAFE":
			o.unsafe = true
		case "HARDBREAKS":
			o.hardbreaks = true
		case "SMART":
			o.smart = true
		case "FOOTNOTES":
			o.footnotes = true
		}
	}
	if !optsVal.IsNil() {
		if optsVal.IsArray() {
			for i := 0; i < oruby.RArrayLen(optsVal); i++ {
				addOpt(mrb.AryRef(optsVal, i))
			}
		} else {
			addOpt(optsVal)
		}
	}
	if !extsVal.IsNil() && extsVal.IsArray() {
		for i := 0; i < oruby.RArrayLen(extsVal); i++ {
			switch strings.ToLower(mrb.String(mrb.AryRef(extsVal, i))) {
			case "table":
				o.table = true
			case "strikethrough":
				o.strikethrough = true
			case "autolink":
				o.autolink = true
			case "tasklist":
				o.tasklist = true
			}
		}
	}
	return o
}

type docNode struct {
	src  string
	opts mdOpts
}

func initCommonmarker(mrb *oruby.MrbState) any {
	// Modern Commonmarker (v1/v2)
	mod := mrb.DefineModule("Commonmarker")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("2.0.0"))

	nodeCls := rb.DataClass(mrb.DefineClassUnder(mod, "Node", mrb.ObjectClass()))
	nodeCls.DefineMethod("to_html", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		dn, ok := rb.Get[*docNode](mrb, self)
		if !ok {
			return mrb.StrNew("")
		}
		o := dn.opts
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			if ov, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern("options").Value()); err == nil && ov.IsHash() {
				o = parseModernOpts(mrb, ov)
			}
		}
		out, err := renderMarkdown(dn.src, o)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.StrNew(out)
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	nodeCls.DefineMethod("to_commonmark", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		dn, ok := rb.Get[*docNode](mrb, self)
		if !ok {
			return mrb.StrNew("")
		}
		return mrb.StrNew(dn.src)
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	nodeCls.DefineMethod("type", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.Intern("document").Value()
	}, mrb.ArgsNone())

	mod.DefineClassMethod("to_html", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).String()
		o := defaultMDOpts()
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			if ov, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern("options").Value()); err == nil && ov.IsHash() {
				o = parseModernOpts(mrb, ov)
			}
		}
		out, err := renderMarkdown(src, o)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.StrNew(out)
	}, mrb.ArgsReq(1)|mrb.ArgsKey(0, 1))

	mod.DefineClassMethod("parse", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).String()
		o := defaultMDOpts()
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			if ov, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern("options").Value()); err == nil && ov.IsHash() {
				o = parseModernOpts(mrb, ov)
			}
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, nodeCls)
		rb.Set(mrb, obj.Value(), &docNode{src: src, opts: o})
		return obj
	}, mrb.ArgsReq(1)|mrb.ArgsKey(0, 1))

	// Legacy CommonMarker (v0.23)
	legacy := mrb.DefineModule("CommonMarker")
	legacy.DefineClassMethod("render_html", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		src := args[0].String()
		var optsVal, extsVal oruby.Value = mrb.NilValue(), mrb.NilValue()
		if len(args) >= 2 {
			optsVal = args[1]
		}
		if len(args) >= 3 {
			extsVal = args[2]
		}
		o := parseLegacyOpts(mrb, optsVal, extsVal)
		out, err := renderMarkdown(src, o)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.StrNew(out)
	}, mrb.ArgsArg(1, 2))

	legacy.DefineClassMethod("render_doc", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		src := args[0].String()
		var optsVal, extsVal oruby.Value = mrb.NilValue(), mrb.NilValue()
		if len(args) >= 2 {
			optsVal = args[1]
		}
		if len(args) >= 3 {
			extsVal = args[2]
		}
		o := parseLegacyOpts(mrb, optsVal, extsVal)
		obj := mrb.ObjAlloc(oruby.MrbTTCData, nodeCls)
		rb.Set(mrb, obj.Value(), &docNode{src: src, opts: o})
		return obj
	}, mrb.ArgsArg(1, 2))

	return nil
}
