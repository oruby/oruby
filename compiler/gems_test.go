package compiler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/compiler"
)

func TestNeedsWebGems(t *testing.T) {
	for _, c := range []struct {
		features []string
		dynamic  bool
		need     bool
	}{
		{nil, false, false},
		{[]string{"json", "set", "fileutils", "uri"}, false, false},
		{[]string{"json", "socket"}, false, true},
		{[]string{"net/http"}, false, true},
		{[]string{"open-uri"}, false, true},
		{[]string{"ipaddr"}, false, true},
		{[]string{"rack/handler"}, false, true},
		{[]string{"json"}, true, true},
	} {
		if need, why := compiler.NeedsWebGems(c.features, c.dynamic); need != c.need || (need && why == "") {
			t.Errorf("NeedsWebGems(%v, %v) = %v, %q; want %v", c.features, c.dynamic, need, why, c.need)
		}
	}
}

func TestSlimBuildModes(t *testing.T) {
	if slim, why := compiler.SlimBuild(compiler.GemsAll, nil, false); slim || why != "" {
		t.Errorf("all: slim=%v why=%q", slim, why)
	}
	if slim, _ := compiler.SlimBuild(compiler.GemsCore, []string{"socket"}, true); !slim {
		t.Error("core must be slim whatever the program requires")
	}
	if slim, _ := compiler.SlimBuild(compiler.GemsAuto, []string{"json"}, false); !slim {
		t.Error("auto: a program that requires no web gem is slim")
	}
	if slim, _ := compiler.SlimBuild(compiler.GemsAuto, []string{"socket"}, false); slim {
		t.Error("auto: a program that requires socket is not slim")
	}
}

// The compiler records what the program requires, and whether it requires by
// a name only run time knows.
func TestCompilerCollectsRequires(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	compile := func(entry string) *compiler.Compiler {
		mrb := oruby.MrbOpen()
		defer mrb.Close()
		c := compiler.New(compiler.Options{PackageName: "main", Standalone: true})
		if _, err := c.CompilePackageDir(mrb, entry, nil, nil, filepath.Join(dir, "out")); err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := compile(write("static.rb", "require 'json'\nrequire \"socket\"\nautoload :Foo, 'foo_lib'\nputs 1\n"))
	if c.DynamicRequire {
		t.Error("literal requires are not dynamic")
	}
	got := map[string]bool{}
	for _, f := range c.Features {
		got[f] = true
	}
	for _, want := range []string{"json", "socket", "foo_lib"} {
		if !got[want] {
			t.Errorf("Features %v lacks %q", c.Features, want)
		}
	}
	if c = compile(write("dyn.rb", "name = ARGV[0]\nrequire name\n")); !c.DynamicRequire {
		t.Error("require of a variable is dynamic")
	}
	if c = compile(write("interp.rb", "x = 'a'\nrequire \"net/#{x}\"\n")); !c.DynamicRequire {
		t.Error("require of an interpolated name is dynamic")
	}
}
