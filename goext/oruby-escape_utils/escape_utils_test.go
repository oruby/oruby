package orubyescapeutils_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/oruby-escape_utils"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestEscapeUtils(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "escape_utils"

raise "html" unless EscapeUtils.escape_html("<a href='x'>&/</a>") == "&lt;a href=&#39;x&#39;&gt;&amp;&#47;&lt;&#47;a&gt;"
raise "html unsecure" unless EscapeUtils.escape_html("a/b", false) == "a/b"
raise "unescape_html" unless EscapeUtils.unescape_html("&lt;b&gt;&amp;&quot;&#39;&lt;/b&gt;") == %q{<b>&"'</b>}
raise "js" unless EscapeUtils.escape_javascript("hi \"a\"\n</script>") == 'hi \"a\"\n<\/script>'
raise "url" unless EscapeUtils.unescape_url(EscapeUtils.escape_url("a b+c&d=1")) == "a b+c&d=1"
raise "uri" unless EscapeUtils.unescape_uri(EscapeUtils.escape_uri("a b/c")) == "a b/c"
raise "xml" unless EscapeUtils.escape_xml("<x a='1'>&</x>") == "&lt;x a=&apos;1&apos;&gt;&amp;&lt;/x&gt;"
`)
	if err != nil {
		t.Fatalf("escape_utils test failed: %v", err)
	}
}
