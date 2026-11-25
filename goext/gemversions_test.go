package goext_test

import (
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"

	_ "github.com/oruby/oruby/goext/argon2"
	_ "github.com/oruby/oruby/goext/bcrypt"
	_ "github.com/oruby/oruby/goext/bcrypt_pbkdf"
	_ "github.com/oruby/oruby/goext/brotli"
	_ "github.com/oruby/oruby/goext/commonmarker"
	_ "github.com/oruby/oruby/goext/ed25519"
	_ "github.com/oruby/oruby/goext/hiredis"
	_ "github.com/oruby/oruby/goext/msgpack"
	_ "github.com/oruby/oruby/goext/mysql2"
	_ "github.com/oruby/oruby/goext/nio4r"
	_ "github.com/oruby/oruby/goext/nokogiri"
	_ "github.com/oruby/oruby/goext/oruby-cbor"
	_ "github.com/oruby/oruby/goext/oruby-digest-crc"
	_ "github.com/oruby/oruby/goext/oruby-escape_utils"
	_ "github.com/oruby/oruby/goext/oruby-fast_blank"
	_ "github.com/oruby/oruby/goext/oruby-lz4"
	_ "github.com/oruby/oruby/goext/oruby-oj"
	_ "github.com/oruby/oruby/goext/oruby-ox"
	_ "github.com/oruby/oruby/goext/oruby-redcarpet"
	_ "github.com/oruby/oruby/goext/oruby-snappy"
	_ "github.com/oruby/oruby/goext/oruby-vips"
	_ "github.com/oruby/oruby/goext/oruby-xxhash"
	_ "github.com/oruby/oruby/goext/oruby-yajl"
	_ "github.com/oruby/oruby/goext/pg"
	_ "github.com/oruby/oruby/goext/puma_http11"
	_ "github.com/oruby/oruby/goext/rbnacl"
	_ "github.com/oruby/oruby/goext/scrypt"
	_ "github.com/oruby/oruby/goext/sqlite3"
	_ "github.com/oruby/oruby/goext/websocket"
	_ "github.com/oruby/oruby/goext/zstd"
)

// Every opt-in Go extension in goext/ registers its own GemSpec in init(), and
// its GemSpec version must match the Ruby VERSION constant it defines.
func TestGoExtGemSpecVersions(t *testing.T) {
	constants := map[string]string{
		"argon2":       "Argon2::VERSION",
		"brotli":       "Brotli::VERSION",
		"cbor":         "CBOR::VERSION",
		"commonmarker": "Commonmarker::VERSION",
		"ed25519":      "Ed25519::VERSION",
		"escape_utils": "EscapeUtils::VERSION",
		"hiredis":      "Hiredis::VERSION",
		"lz4-ruby":     "LZ4::VERSION",
		"msgpack":      "MessagePack::VERSION",
		"mysql2":       "Mysql2::VERSION",
		"nio4r":        "NIO::VERSION",
		"nokogiri":     "Nokogiri::VERSION",
		"oj":           "Oj::VERSION",
		"ox":           "Ox::VERSION",
		"pg":           "PG::VERSION",
		"rbnacl":       "RbNaCl::VERSION",
		"redcarpet":    "Redcarpet::VERSION",
		"scrypt":       "SCrypt::VERSION",
		"snappy":       "Snappy::VERSION",
		"sqlite3":      "SQLite3::VERSION",
		"xxhash":       "XXhash::VERSION",
		"yajl-ruby":    "Yajl::VERSION",
		"zstd-ruby":    "Zstd::VERSION",
	}
	root, err := filepath.Abs("..")
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
			t.Errorf("%s: no embedded GemSpec registered by goext", name)
		}
	}
}
