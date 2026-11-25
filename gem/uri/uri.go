// Package uri implements Ruby's uri library with Go regexp (RFC 3986
// splitting), net/url (reference resolution) and percent encoding helpers.
package uri

import (
	_ "embed"
	"net/url"
	"regexp"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed uri.rb
var uriRb []byte

// RFC 3986 appendix B, refined to separate userinfo, host and port
var (
	reURI       = regexp.MustCompile(`\A(?:([A-Za-z][A-Za-z0-9+\-.]*):)?(?://([^/?#]*))?([^?#]*)(?:\?([^#]*))?(?:#(.*))?\z`)
	reAuthority = regexp.MustCompile(`\A(?:([^@]*)@)?(\[[^\]]*\]|[^:]*)(?::(\d*))?\z`)
	reScheme    = regexp.MustCompile(`\A[A-Za-z][A-Za-z0-9+\-.]*\z`)
	rePchar     = regexp.MustCompile(`\A(?:[A-Za-z0-9\-._~!$&'()*+,;=:@/]|%[0-9A-Fa-f]{2})*\z`)
	reQuery     = regexp.MustCompile(`\A(?:[A-Za-z0-9\-._~!$&'()*+,;=:@/?\[\]]|%[0-9A-Fa-f]{2})*\z`)
	reUserinfo  = regexp.MustCompile(`\A(?:[A-Za-z0-9\-._~!$&'()*+,;=:]|%[0-9A-Fa-f]{2})*\z`)
	reHost      = regexp.MustCompile(`\A(?:\[[0-9A-Fa-f:.]+\]|\[v[0-9A-Fa-f]+\.[A-Za-z0-9\-._~!$&'()*+,;=:]+\]|(?:[A-Za-z0-9\-._~!$&'()*+,;=]|%[0-9A-Fa-f]{2})*)\z`)
)

func init() {
	oruby.LazyGem("uri", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("URI")
		mod.DefineClassMethod("__split", uriSplit, mrb.ArgsReq(1))
		mod.DefineClassMethod("__merge", uriMerge, mrb.ArgsReq(2))
		mod.DefineClassMethod("__valid", uriValid, mrb.ArgsReq(2))
		mod.DefineClassMethod("__escape", uriEscape, mrb.ArgsReq(2))
		mod.DefineClassMethod("__unescape", uriUnescape, mrb.ArgsReq(2))
		rb.LoadRuby(mrb, "uri.rb", uriRb)
		return nil
	})
	for _, name := range []string{"uri/common", "uri/generic", "uri/http", "uri/https", "uri/ftp", "uri/ldap",
		"uri/ldaps", "uri/mailto", "uri/file", "uri/ws", "uri/wss", "uri/rfc2396_parser", "uri/rfc3986_parser"} {
		oruby.GemAlias(name, "uri")
	}
}

func strArg(mrb *oruby.MrbState, v oruby.Value) (string, oruby.MrbValue) {
	s, err := rb.Str(mrb, v)
	if err != nil {
		return "", rb.TypeError(mrb, err)
	}
	return s, nil
}

func strOrNil(mrb *oruby.MrbState, s string, present bool) oruby.Value {
	if !present {
		return mrb.NilValue()
	}
	return mrb.StrNew(s)
}

// uriSplit returns [scheme, userinfo, host, port, registry, path, opaque,
// query, fragment] or nil when str is not a valid URI reference
func uriSplit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str, exc := strArg(mrb, mrb.GetArgsFirst())
	if exc != nil {
		return exc
	}
	m := reURI.FindStringSubmatchIndex(str)
	if m == nil {
		return mrb.NilValue()
	}
	group := func(i int) (string, bool) {
		if m[2*i] < 0 {
			return "", false
		}
		return str[m[2*i]:m[2*i+1]], true
	}
	scheme, hasScheme := group(1)
	authority, hasAuth := group(2)
	path, _ := group(3)
	query, hasQuery := group(4)
	fragment, hasFrag := group(5)

	if hasQuery && !reQuery.MatchString(query) || hasFrag && !reQuery.MatchString(fragment) {
		return mrb.NilValue()
	}
	var userinfo, host, port string
	var hasUser, hasHost, hasPort bool
	if hasAuth {
		am := reAuthority.FindStringSubmatchIndex(authority)
		if am == nil {
			return mrb.NilValue()
		}
		if am[2] >= 0 {
			userinfo, hasUser = authority[am[2]:am[3]], true
			if !reUserinfo.MatchString(userinfo) {
				return mrb.NilValue()
			}
		}
		host, hasHost = authority[am[4]:am[5]], true
		if !reHost.MatchString(host) {
			return mrb.NilValue()
		}
		if am[6] >= 0 {
			port, hasPort = authority[am[6]:am[7]], true
			if port == "" {
				hasPort = false
			}
		}
	}
	ary := mrb.AryNew()
	ary.Push(strOrNil(mrb, scheme, hasScheme))
	ary.Push(strOrNil(mrb, userinfo, hasUser))
	ary.Push(strOrNil(mrb, host, hasHost))
	ary.Push(strOrNil(mrb, port, hasPort))
	ary.Push(mrb.NilValue()) // registry
	// opaque: scheme without authority and path not starting with "/"
	if hasScheme && !hasAuth && path != "" && !strings.HasPrefix(path, "/") {
		opaque := path
		if hasQuery {
			opaque += "?" + query
		}
		if !rePchar.MatchString(path) {
			return mrb.NilValue()
		}
		ary.Push(mrb.NilValue())
		ary.Push(mrb.StrNew(opaque))
		ary.Push(mrb.NilValue())
	} else {
		if !rePchar.MatchString(path) {
			return mrb.NilValue()
		}
		ary.Push(mrb.StrNew(path))
		ary.Push(mrb.NilValue())
		ary.Push(strOrNil(mrb, query, hasQuery))
	}
	ary.Push(strOrNil(mrb, fragment, hasFrag))
	return ary
}

// uriMerge resolves reference against base (RFC 3986 section 5.2)
func uriMerge(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	base, err1 := url.Parse(args.Item(0).String())
	ref, err2 := url.Parse(args.Item(1).String())
	if err1 != nil || err2 != nil {
		return mrb.NilValue()
	}
	res := base.ResolveReference(ref)
	return mrb.StrNew(res.String())
}

// uriValid checks component against RFC 3986 syntax
func uriValid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	kind, v := args.Item(0).String(), args.Item(1).String()
	var ok bool
	switch kind {
	case "scheme":
		ok = reScheme.MatchString(v)
	case "userinfo", "user", "password":
		ok = reUserinfo.MatchString(v)
	case "host":
		ok = reHost.MatchString(v)
	case "path":
		ok = rePchar.MatchString(v)
	case "opaque", "query", "fragment":
		ok = reQuery.MatchString(v)
	default:
		ok = true
	}
	return mrb.BoolValue(ok)
}

const hexDigits = "0123456789ABCDEF"

// uriEscape percent-encodes bytes not in the given unreserved set.
// mode: "www" (application/x-www-form-urlencoded, space as +),
// "component" (RFC 3986 unreserved), or a set of extra safe characters
func uriEscape(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	s, exc := strArg(mrb, args.Item(0))
	if exc != nil {
		return exc
	}
	mode := args.Item(1).String()
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case mode == "www" && c == ' ':
			b.WriteByte('+')
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
			b.WriteByte(c)
		case mode == "www" && strings.IndexByte("*-._", c) >= 0:
			b.WriteByte(c)
		case mode == "component" && strings.IndexByte("-._~", c) >= 0:
			b.WriteByte(c)
		case mode != "www" && mode != "component" && strings.IndexByte(mode, c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
		}
	}
	return mrb.StrNew(b.String())
}

// uriUnescape decodes %XX (and + as space when plus is true)
func uriUnescape(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	s, exc := strArg(mrb, args.Item(0))
	if exc != nil {
		return exc
	}
	plus := args.Item(1).Bool()
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '+' && plus:
			b.WriteByte(' ')
		case c == '%' && i+2 < len(s)+0 && isHex(s[i+1]) && i+2 < len(s) && isHex(s[i+2]):
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return mrb.StrNew(b.String())
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}
