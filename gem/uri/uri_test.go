package uri_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/uri"
)

func evalURI(t *testing.T, code string) {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Eval(code); err != nil {
		t.Fatalf("ruby eval failed:\n%s\nerror: %v", code, err)
	}
}

func TestURIParseSplitAndJoin(t *testing.T) {
	evalURI(t, `
require "uri"

u = URI.parse("https://user:pass@example.com:8443/a/b?k=v&x=1#top")
raise "class" unless u.is_a?(URI::HTTPS)
raise "scheme" unless u.scheme == "https"
raise "userinfo" unless u.userinfo == "user:pass"
raise "host" unless u.host == "example.com"
raise "port" unless u.port == 8443
raise "path" unless u.path == "/a/b"
raise "query" unless u.query == "k=v&x=1"
raise "fragment" unless u.fragment == "top"
raise "request_uri" unless u.request_uri == "/a/b?k=v&x=1"

joined = URI.join("https://example.com/a/b/c", "../d?q=1")
raise "join: #{joined}" unless joined.to_s == "https://example.com/a/d?q=1"

mailto = URI.parse("mailto:test@example.com?subject=Hi")
raise "mailto" unless mailto.is_a?(URI::MailTo) && mailto.to == "test@example.com"

begin
  URI.parse("http://bad host/")
  raise "expected InvalidURIError"
rescue URI::InvalidURIError
end
`)
}

func TestURIEncodingAndFormHelpers(t *testing.T) {
	evalURI(t, `
require "uri"

raise "encode_www_form_component" unless URI.encode_www_form_component("a b+c&d=e") == "a+b%2Bc%26d%3De"
raise "decode_www_form_component" unless URI.decode_www_form_component("a+b%2Bc%26d%3De") == "a b+c&d=e"
raise "encode_uri_component" unless URI.encode_uri_component("hello world") == "hello%20world"
raise "decode_uri_component" unless URI.decode_uri_component("hello%20world+1") == "hello world+1"

form = URI.encode_www_form([["q", "ruby rails"], ["page", "2"], ["tag", ["a", "b"]]])
raise "encode_www_form: #{form}" unless form == "q=ruby+rails&page=2&tag=a&tag=b"
decoded = URI.decode_www_form(form)
raise "decode_www_form: #{decoded.inspect}" unless decoded == [["q", "ruby rails"], ["page", "2"], ["tag", "a"], ["tag", "b"]]
`)
}
