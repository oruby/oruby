package compiler

import (
	"fmt"
	"sort"
	"strings"
)

// GoExtModule is the nested Go module holding ORuby's opt-in Go-based
// replacements for third-party Ruby extensions.
const GoExtModule = "github.com/oruby/oruby/goext"

// GoExtSpec describes one opt-in Go extension package in goext/.
// This catalog holds metadata only — it never imports goext/*, so standalone
// binaries (oruby, orbi, orubyc) stay lean and projects only link the Go
// extensions they select.
type GoExtSpec struct {
	Gem        string   // canonical RubyGems name (e.g. "pg", "zstd-ruby", "ruby-vips")
	Aliases    []string // alternative names accepted in oruby.yml / --go-ext
	ImportPath string   // Go package import path inside GoExtModule
	Version    string   // upstream gem version covered by this Go extension
	ExtOnly    bool     // true if Go package only replaces the native extension (gem's Ruby lib/ is still packed)
	AOTDirect  bool     // true if compiler/intrinsics.go can emit guarded direct Go calls
	PureGo     bool     // true if package has no cgo dependencies of its own
}

var goExtCatalog = []GoExtSpec{
	{Gem: "argon2", ImportPath: GoExtModule + "/argon2", Version: "2.3.0", PureGo: true},
	{Gem: "bcrypt", ImportPath: GoExtModule + "/bcrypt", Version: "3.1.20", PureGo: true},
	{Gem: "bcrypt_pbkdf", ImportPath: GoExtModule + "/bcrypt_pbkdf", Version: "1.1.1", PureGo: true},
	{Gem: "brotli", ImportPath: GoExtModule + "/brotli", Version: "0.6.0", AOTDirect: true, PureGo: true},
	{Gem: "cbor", Aliases: []string{"oruby-cbor"}, ImportPath: GoExtModule + "/oruby-cbor", Version: "0.5.9.8", PureGo: true},
	{Gem: "commonmarker", ImportPath: GoExtModule + "/commonmarker", Version: "2.0.0", PureGo: true},
	{Gem: "digest-crc", Aliases: []string{"oruby-digest-crc"}, ImportPath: GoExtModule + "/oruby-digest-crc", Version: "0.7.0", PureGo: true},
	{Gem: "ed25519", ImportPath: GoExtModule + "/ed25519", Version: "1.4.0", PureGo: true},
	{Gem: "escape_utils", Aliases: []string{"oruby-escape_utils"}, ImportPath: GoExtModule + "/oruby-escape_utils", Version: "1.3.0", AOTDirect: true, PureGo: true},
	{Gem: "fast_blank", Aliases: []string{"oruby-fast_blank"}, ImportPath: GoExtModule + "/oruby-fast_blank", Version: "1.0.1", AOTDirect: true, PureGo: true},
	{Gem: "hiredis", ImportPath: GoExtModule + "/hiredis", Version: "0.6.3", PureGo: true},
	{Gem: "lz4-ruby", Aliases: []string{"oruby-lz4", "lz4"}, ImportPath: GoExtModule + "/oruby-lz4", Version: "0.3.3", PureGo: true},
	{Gem: "msgpack", ImportPath: GoExtModule + "/msgpack", Version: "1.8.7", PureGo: true},
	{Gem: "mysql2", ImportPath: GoExtModule + "/mysql2", Version: "0.5.6", PureGo: true},
	{Gem: "nio4r", Aliases: []string{"nio"}, ImportPath: GoExtModule + "/nio4r", Version: "2.7.5", PureGo: true},
	{Gem: "nokogiri", ImportPath: GoExtModule + "/nokogiri", Version: "1.19.7", PureGo: true},
	{Gem: "oj", Aliases: []string{"oruby-oj"}, ImportPath: GoExtModule + "/oruby-oj", Version: "3.16.7", PureGo: true},
	{Gem: "ox", Aliases: []string{"oruby-ox"}, ImportPath: GoExtModule + "/oruby-ox", Version: "2.14.23", PureGo: true},
	{Gem: "pg", ImportPath: GoExtModule + "/pg", Version: "1.5.9", PureGo: true},
	{Gem: "puma", Aliases: []string{"puma_http11"}, ImportPath: GoExtModule + "/puma_http11", ExtOnly: true, PureGo: true},
	{Gem: "rbnacl", ImportPath: GoExtModule + "/rbnacl", Version: "7.1.2", PureGo: true},
	{Gem: "redcarpet", Aliases: []string{"oruby-redcarpet"}, ImportPath: GoExtModule + "/oruby-redcarpet", Version: "3.6.0", PureGo: true},
	{Gem: "ruby-vips", Aliases: []string{"oruby-vips", "vips"}, ImportPath: GoExtModule + "/oruby-vips", Version: "2.3.0", PureGo: true},
	{Gem: "scrypt", ImportPath: GoExtModule + "/scrypt", Version: "3.0.7", PureGo: true},
	{Gem: "snappy", Aliases: []string{"oruby-snappy"}, ImportPath: GoExtModule + "/oruby-snappy", Version: "0.4.0", PureGo: true},
	{Gem: "sqlite3", ImportPath: GoExtModule + "/sqlite3", Version: "2.6.0", PureGo: false},
	{Gem: "websocket-driver", Aliases: []string{"websocket_mask", "websocket"}, ImportPath: GoExtModule + "/websocket", ExtOnly: true, PureGo: true},
	{Gem: "xxhash", Aliases: []string{"oruby-xxhash"}, ImportPath: GoExtModule + "/oruby-xxhash", Version: "0.6.0", AOTDirect: true, PureGo: true},
	{Gem: "yajl-ruby", Aliases: []string{"oruby-yajl", "yajl"}, ImportPath: GoExtModule + "/oruby-yajl", Version: "1.4.3", PureGo: true},
	{Gem: "zstd-ruby", Aliases: []string{"zstd"}, ImportPath: GoExtModule + "/zstd", Version: "2.0.10", AOTDirect: true, PureGo: true},
}

var goExtIndex = func() map[string]GoExtSpec {
	m := make(map[string]GoExtSpec, len(goExtCatalog)*2)
	for _, sp := range goExtCatalog {
		m[strings.ToLower(sp.Gem)] = sp
		for _, a := range sp.Aliases {
			m[strings.ToLower(a)] = sp
		}
	}
	return m
}()

// AllGoExts returns a copy of all opt-in Go extensions in canonical gem order.
func AllGoExts() []GoExtSpec {
	out := make([]GoExtSpec, len(goExtCatalog))
	copy(out, goExtCatalog)
	return out
}

// LookupGoExt finds a Go extension specification by RubyGems name or alias.
func LookupGoExt(name string) (GoExtSpec, bool) {
	sp, ok := goExtIndex[strings.ToLower(strings.TrimSpace(name))]
	return sp, ok
}

// AvailableGoExtNames returns the sorted canonical gem names in the catalog.
func AvailableGoExtNames() []string {
	names := make([]string, len(goExtCatalog))
	for i, sp := range goExtCatalog {
		names[i] = sp.Gem
	}
	sort.Strings(names)
	return names
}

// ResolveGoExtList validates and deduplicates a list of gem names into specs.
func ResolveGoExtList(names []string) ([]GoExtSpec, error) {
	seen := make(map[string]bool, len(names))
	var out []GoExtSpec
	for _, raw := range names {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		sp, ok := LookupGoExt(raw)
		if !ok {
			return nil, fmt.Errorf("unknown Go extension %q (available: %s)", raw, strings.Join(AvailableGoExtNames(), ", "))
		}
		if seen[sp.Gem] {
			continue
		}
		seen[sp.Gem] = true
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Gem < out[j].Gem })
	return out, nil
}
