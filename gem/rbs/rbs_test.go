package rbs

import (
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"
)

// The rbs gem's Ruby library over its C extension, compiled in through the
// Ruby C API (one state: the C API binds to it).
func TestParser(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	for _, g := range []string{"core_ext", "load"} {
		if _, err := mrb.Resolve(g); err != nil {
			t.Fatal(err)
		}
	}
	libs := []string{"../../lib/oruby/gems/4.1.0/gems/rbs-4.2.0/lib", "../../lib/oruby/4.1.0"}
	for _, l := range libs {
		abs, _ := filepath.Abs(l)
		mrb.SetGV("$__rbs_test_lib", abs)
		if _, err := mrb.Eval(`$LOAD_PATH.push($__rbs_test_lib)`); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range [][2]string{
		{`require "rbs"; RBS::Parser.parse_type("Array[String | Integer]?").to_s`, `Array[String | Integer]?`},
		{`RBS::Parser.parse_method_type("(String, ?Integer) { (String) -> void } -> Symbol").to_s`, `(String, ?Integer) { (String) -> void } -> Symbol`},
		{`_, _, d = RBS::Parser.parse_signature("class C[T]\n  def m: (T) -> T\nend\n"); "#{d[0].name} #{d[0].members[0].location.source}"`, `C def m: (T) -> T`},
		{`begin; RBS::Parser.parse_type("Array["); rescue RBS::ParsingError => e; e.class.name; end`, `RBS::ParsingError`},
	} {
		code, want := c[0], c[1]
		v, err := mrb.Eval(code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if got := mrb.String(v); got != want {
			t.Fatalf("%s\n got: %q\nwant: %q", code, got, want)
		}
	}
}
