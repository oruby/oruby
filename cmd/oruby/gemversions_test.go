package main

import (
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
)

// An embedded gem's GemSpec version is the version of the upstream gem its
// implementation follows, and its library says the same (VERSION): the
// embedded gem stands in for the gem's versions up to that one (Bundler
// resolution, extension builds, require), so the two must not drift apart.
// Third-party Go extension replacements in goext/ are tested in
// goext/gemversions_test.go.
func TestEmbeddedGemSpecVersions(t *testing.T) {
	constants := map[string]string{
		"bigdecimal": "BigDecimal::VERSION",
		"csv":        "CSV::VERSION",
		"drb":        "DRb::VERSION",
		"ffi":        "FFI::VERSION",
		"logger":     "Logger::VERSION",
		"matrix":     "Matrix::VERSION",
		"observer":   "Observable::VERSION",
		"oj":         "Oj::VERSION",
		"prime":      "Prime::VERSION",
		"prism":      "Prism::VERSION",
		"racc":       "Racc::VERSION",
		"tsort":      "TSort::VERSION",
	}
	// the libraries ORuby ships as files (lib/oruby/4.1.0) come first, as in
	// the installed oruby: matrix, prime, logger, prism are upstream's there
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	oruby.SetInstallPrefix(root)
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	seen := map[string]bool{}
	for _, sp := range oruby.BuiltinGemSpecs() {
		c, ok := constants[sp.Name]
		if !ok {
			continue
		}
		seen[sp.Name] = true
		mrb.SetGV("$__files", sp.Files)
		v, err := mrb.Eval(`$__files.each { |f| require f.delete_prefix("lib/").delete_suffix(".rb") }; ` + c + `.to_s`)
		if err != nil {
			t.Errorf("%s: %v", sp.Name, err)
			continue
		}
		if got := mrb.String(v); got != sp.Version {
			t.Errorf("%s: GemSpec version %s, %s = %s", sp.Name, sp.Version, c, got)
		}
	}
	for name := range constants {
		if !seen[name] {
			t.Errorf("%s: no embedded GemSpec", name)
		}
	}
}
