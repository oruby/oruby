package oruby_test

import (
	"testing"

	"github.com/oruby/oruby"
)

func TestGemDirVersion(t *testing.T) {
	for _, c := range []struct{ path, name, version string }{
		{"/h/gems/msgpack-1.8.7/lib/msgpack.rb", "msgpack", "1.8.7"},
		{"/h/gems/zstd-ruby-1.5.6/lib/zstd-ruby.rb", "zstd-ruby", "1.5.6"},
		{"/h/gems/nokogiri-1.19.8-aarch64-linux-gnu/lib/nokogiri.rb", "nokogiri", "1.19.8"},
		{"orb:/app.orb/gems/ed25519-1.4.0/lib/ed25519.rb", "ed25519", "1.4.0"},
		{"/usr/lib/oruby/4.1.0/json.rb", "", ""},
	} {
		name, version, ok := oruby.GemDirVersion(c.path)
		if name != c.name || version != c.version || ok != (c.name != "") {
			t.Errorf("GemDirVersion(%q) = %q, %q, %v", c.path, name, version, ok)
		}
	}
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.8.7", "1.8.0", 1}, {"1.10", "1.9.9", 1}, {"1.8", "1.8.0", 0}, {"0.5.9.8", "0.5.10", -1}} {
		if got := oruby.CompareGemVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareGemVersions(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
