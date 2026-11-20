package coverage_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/coverage"
	_ "github.com/oruby/oruby/gem/load"
)

func TestCoverageLinesBranchesMethods(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	path := filepath.Join(t.TempDir(), "sign.rb")
	src := "module CovSign\n  def self.sign(x)\n    x < 0 ? :negative : :positive\n  end\nend\n"
	if err := os.WriteFile(path, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    require "coverage"
    Coverage.start(:all)
    load ` + strconv.Quote(path) + `
    CovSign.sign(1); CovSign.sign(-1); CovSign.sign(2)
    r = Coverage.result[` + strconv.Quote(path) + `]
    raise r.inspect unless r == {
      lines: [1, 1, 3, nil, nil],
      branches: { [:if, 0, 3, 4, 3, 33] => { [:then, 1, 3, 12, 3, 21] => 1, [:else, 2, 3, 24, 3, 33] => 2 } },
      methods: { [CovSign.singleton_class, :sign, 2, 2, 4, 5] => 3 }
    }
    Coverage.state == :idle
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("coverage result = %v, %v; want true, nil", got, err)
	}
}

func TestCoverageLineStubAndStates(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	path := filepath.Join(t.TempDir(), "stub.rb")
	src := "# comment\n\nx = 1\nif false\n  x = 2\nend\nx\n"
	if err := os.WriteFile(path, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    require "coverage"
    stub = Coverage.line_stub(` + strconv.Quote(path) + `)
    errs = []
    begin; Coverage.result; rescue RuntimeError => e; errs << e.message; end
    Coverage.setup
    begin; Coverage.setup; rescue RuntimeError => e; errs << e.message; end
    Coverage.resume
    states = [Coverage.state, Coverage.running?]
    Coverage.suspend
    states << Coverage.state
    Coverage.result
    [stub, errs, states]
  `)
	if err != nil {
		t.Fatal(err)
	}
	want := `[[nil, nil, 0, nil, nil, nil, 0], ["coverage measurement is not enabled", "coverage measurement is already setup"], [:running, true, :suspended]]`
	if s := mrb.String(mrb.Call(got, "inspect")); s != want {
		t.Fatalf("line_stub and states = %s; want %s", s, want)
	}
}
