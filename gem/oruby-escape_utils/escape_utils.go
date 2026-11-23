// Package orubyescapeutils implements the Ruby escape_utils gem in pure Go,
// registered as "oruby-escape_utils" and aliased to "escape_utils".
package orubyescapeutils

import (
	"html"
	"net/url"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	oruby.LazyGem("oruby-escape_utils", initEscapeUtils)
	for _, alias := range []string{
		"escape_utils",
		"escape_utils/html/cgi",
		"escape_utils/html/erb",
		"escape_utils/html/haml",
		"escape_utils/html/rack",
		"escape_utils/url/cgi",
		"escape_utils/url/erb",
		"escape_utils/url/rack",
		"escape_utils/url/uri",
	} {
		oruby.GemAlias(alias, "oruby-escape_utils")
	}
}

func escapeHTML(s string, secure bool) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&#39;")
		case '/':
			if secure {
				sb.WriteString("&#47;")
			} else {
				sb.WriteByte('/')
			}
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

func escapeJS(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\'':
			sb.WriteString(`\'`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\n`)
		case '<':
			if i+1 < len(s) && s[i+1] == '/' {
				sb.WriteString(`<\/`)
				i++
			} else {
				sb.WriteByte('<')
			}
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

func escapeXML(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&apos;")
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

func initEscapeUtils(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("EscapeUtils")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("1.3.0"))

	htmlSecure := true

	mod.DefineClassMethod("html_secure", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(htmlSecure)
	}, mrb.ArgsNone())
	mod.DefineClassMethod("html_secure=", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		htmlSecure = v.Bool()
		return v
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("escape_html", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		if args[0].IsNil() {
			return mrb.StrNew("")
		}
		sec := htmlSecure
		if len(args) >= 2 {
			sec = args[1].Bool()
		}
		return mrb.StrNew(escapeHTML(args[0].String(), sec))
	}, mrb.ArgsArg(1, 1))
	mod.DefineClassMethod("escape_html_as_html_safe", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		s := ""
		if !args[0].IsNil() {
			s = escapeHTML(args[0].String(), htmlSecure)
		}
		res := mrb.StrNew(s)
		if mrb.RespondTo(res, mrb.Intern("html_safe")) {
			if hs, err := mrb.Funcall(res, mrb.Intern("html_safe")); err == nil {
				return hs
			}
		}
		return res
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("unescape_html", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.StrNew("")
		}
		return mrb.StrNew(html.UnescapeString(v.String()))
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("escape_javascript", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.StrNew("")
		}
		return mrb.StrNew(escapeJS(v.String()))
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("escape_url", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.StrNew("")
		}
		return mrb.StrNew(url.QueryEscape(v.String()))
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("unescape_url", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.StrNew("")
		}
		u, err := url.QueryUnescape(v.String())
		if err != nil {
			return mrb.StrNew(v.String())
		}
		return mrb.StrNew(u)
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("escape_uri", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.StrNew("")
		}
		return mrb.StrNew(url.PathEscape(v.String()))
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("unescape_uri", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.StrNew("")
		}
		u, err := url.PathUnescape(v.String())
		if err != nil {
			return mrb.StrNew(v.String())
		}
		return mrb.StrNew(u)
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("escape_xml", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if v.IsNil() {
			return mrb.StrNew("")
		}
		return mrb.StrNew(escapeXML(v.String()))
	}, mrb.ArgsReq(1))

	_ = rb.Str
	return nil
}
