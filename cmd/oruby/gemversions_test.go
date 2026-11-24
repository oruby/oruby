package main

import (
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
)

// An embedded gem's GemSpec version is the version of the upstream gem its
// Go implementation follows, and its library says the same (VERSION): the
// embedded gem stands in for the gem's versions up to that one (Bundler
// resolution, extension builds, require), so the two must not drift apart.
func TestEmbeddedGemSpecVersions(t *testing.T) {
	constants := map[string]string{
		"argon2": "Argon2::VERSION", "bigdecimal": "BigDecimal::VERSION", "brotli": "Brotli::VERSION",
		"cbor": "CBOR::VERSION", "commonmarker": "Commonmarker::VERSION", "csv": "CSV::VERSION",
		"drb": "DRb::VERSION", "ed25519": "Ed25519::VERSION", "escape_utils": "EscapeUtils::VERSION",
		"ffi": "FFI::VERSION", "hiredis": "Hiredis::VERSION", "logger": "Logger::VERSION",
		"lz4-ruby": "LZ4::VERSION", "matrix": "Matrix::VERSION", "msgpack": "MessagePack::VERSION",
		"mysql2": "Mysql2::VERSION", "nio4r": "NIO::VERSION", "nokogiri": "Nokogiri::VERSION",
		"observer": "Observable::VERSION", "oj": "Oj::VERSION", "ox": "Ox::VERSION", "pg": "PG::VERSION",
		"prime": "Prime::VERSION", "prism": "Prism::VERSION", "racc": "Racc::VERSION",
		"rbnacl": "RbNaCl::VERSION", "redcarpet": "Redcarpet::VERSION", "scrypt": "SCrypt::VERSION",
		"snappy": "Snappy::VERSION", "sqlite3": "SQLite3::VERSION", "tsort": "TSort::VERSION",
		"xxhash": "XXhash::VERSION", "yajl-ruby": "Yajl::VERSION", "zstd-ruby": "Zstd::VERSION",
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
