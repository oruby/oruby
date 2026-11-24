package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oruby/oruby"
)

// buildInPrefix builds oruby into <prefix>/bin with the repository's vendored
// lib/oruby beside it, the install layout the binary finds its libraries by.
func buildInPrefix(t *testing.T) string {
	t.Helper()
	repoLib, err := filepath.Abs("../../lib/oruby")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoLib, "4.1.0", "rubygems.rb")); err != nil {
		t.Skip("vendored lib/oruby not present")
	}
	prefix := t.TempDir()
	exe := filepath.Join(prefix, "bin", "oruby")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoLib, filepath.Join(prefix, "lib", "oruby")); err != nil {
		t.Fatal(err)
	}
	return exe
}

func runOruby(t *testing.T, exe, dir string, args ...string) string {
	t.Helper()
	gemHome := t.TempDir()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GEM_HOME="+gemHome, "GEM_PATH="+gemHome, "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("oruby %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRubyGemsAndBundlerFromInstallTree(t *testing.T) {
	if testing.Short() {
		t.Skip("builds oruby")
	}
	exe := buildInPrefix(t)
	dir := t.TempDir()

	got := runOruby(t, exe, dir, "-e", `puts Gem::VERSION, Gem::Specification.find_by_name("bundler").version, Gem.default_specifications_dir.start_with?(RbConfig::CONFIG["rubylibprefix"])`)
	if want := "4.1.0.beta2\n4.1.0.beta2\ntrue"; got != want {
		t.Fatalf("RubyGems at startup = %q, want %q", got, want)
	}

	if got := runOruby(t, exe, dir, "--disable-gems", "-e", `p defined?(Gem)`); got != "nil" {
		t.Fatalf("--disable-gems: defined?(Gem) = %s", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte("source \"https://rubygems.org\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = runOruby(t, exe, dir, "-e", `require "bundler/setup"; puts Bundler::VERSION, Bundler.load.specs.map(&:name).join(",")`)
	if want := "4.1.0.beta2\nbundler"; got != want {
		t.Fatalf("bundler/setup = %q, want %q", got, want)
	}

	// Verify compiled-in ORuby native gems are registered as default Gem::Specifications
	got = runOruby(t, exe, dir, "-e", `
%w[nokogiri oruby-vips ruby-vips cbor yajl-ruby snappy lz4-ruby ox redcarpet xxhash digest-crc].each do |name|
  spec = Gem::Specification.find_by_name(name)
  gem name
  raise "not default: #{name}" unless spec.default_gem?
end
puts "ok:#{Gem.loaded_specs["nokogiri"].version}:#{Gem.loaded_specs["ruby-vips"].version}:#{Gem.loaded_specs["cbor"].version}"
`)
	if want := "ok:1.19.7:2.3.0:0.5.9.8"; got != want {
		t.Fatalf("builtin gemspecs = %q, want %q", got, want)
	}
}

// The executables scripts/gen_bin_stubs.rb writes beside oruby (gem,
// bundle, ...) run the oruby next to them, wherever the install is.
func TestBinStubs(t *testing.T) {
	if testing.Short() {
		t.Skip("builds oruby")
	}
	exe := buildInPrefix(t)
	bin := filepath.Dir(exe)
	gen, err := filepath.Abs("../../scripts/gen_bin_stubs.rb")
	if err != nil {
		t.Fatal(err)
	}
	runOruby(t, exe, t.TempDir(), gen, bin)
	dir := t.TempDir()
	for stub, want := range map[string]string{"gem": "4.1.0.beta2", "bundle": "4.1.0.beta2", "irb": "irb 1.18.0", "rbs": "rbs 4.2.0"} {
		cmd := exec.Command(filepath.Join(bin, stub), "--version")
		cmd.Dir = dir
		cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s --version: %v\n%s", stub, err, out)
		}
		if got := strings.TrimSpace(string(out)); !strings.HasPrefix(got, want) {
			t.Fatalf("%s --version = %q, want %q...", stub, got, want)
		}
	}
	// executables of gems installed in the default directory go beside oruby
	if got := runOruby(t, exe, dir, "-e", `puts Gem.default_bindir == RbConfig::CONFIG["bindir"]`); got != "true" {
		t.Fatalf("Gem.default_bindir = RbConfig bindir: %s", got)
	}
}

// An embedded gem (msgpack, a Go gem with an oruby.GemSpec) stands in for
// its versions up to its own: Bundler resolves to it where the Gemfile
// allows, such a version's extension is not built, and a newer version the
// Gemfile asks for is what Bundler picks and require loads. Offline: a fake
// msgpack 9.9.9 installed in GEM_HOME is the newer version.
func TestEmbeddedGemVersions(t *testing.T) {
	exe := buildInPrefix(t)
	gemHome := t.TempDir()
	fake := filepath.Join(gemHome, "gems", "msgpack-9.9.9")
	for path, body := range map[string]string{
		filepath.Join(fake, "lib", "msgpack.rb"): "module MessagePack; FAKE = :newer; end\n",
		filepath.Join(gemHome, "specifications", "msgpack-9.9.9.gemspec"): `Gem::Specification.new do |s|
  s.name = "msgpack"; s.version = "9.9.9"; s.summary = "fake"; s.authors = ["t"]
  s.files = ["lib/msgpack.rb"]; s.require_paths = ["lib"]
end
`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(exe, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GEM_HOME="+gemHome, "GEM_PATH="+gemHome, "HOME="+t.TempDir(), "BUNDLE_APP_CONFIG="+t.TempDir())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oruby %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	check := `require "bundler/setup"; require "msgpack"; p [Gem.loaded_specs["msgpack"].version.to_s, defined?(MessagePack::FAKE) ? :file : :embedded]`
	ev, ok := oruby.EmbeddedGemVersion("msgpack")
	if !ok {
		t.Skip("no embedded msgpack")
	}
	for _, c := range []struct{ req, want string }{
		{``, `["` + ev + `", :embedded]`},        // the embedded version, not the newer installed one
		{`, ">= 9"`, `["9.9.9", :file]`},         // the newer one asked for: its file loads
		{`, "< 2"`, `["` + ev + `", :embedded]`}, // the embedded one satisfies it
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`source "https://rubygems.org"`+"\ngem \"msgpack\""+c.req+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run(dir, "-e", `require "bundler"; require "bundler/cli"; Bundler::CLI.start(%w[lock --local])`)
		if got := run(dir, "-e", check); got != c.want {
			t.Errorf("gem \"msgpack\"%s: got %s, want %s\n%s", c.req, got, c.want, run(dir, "-e", `puts File.read("Gemfile.lock")`))
		}
	}

	// the extension of a version the embedded gem covers is not built
	got := run(t.TempDir(), "-e", `
require "rubygems/ext"
def spec(v)
  Gem::Specification.new { |s| s.name = "msgpack"; s.version = v; s.extensions = ["ext/msgpack/extconf.rb"]
    s.loaded_from = File.join(Gem.dir, "specifications", "msgpack-#{v}.gemspec") }
end
old = spec("1.0")
Gem::Ext::Builder.new(old).build_extensions
new = spec("9.9.9")
built = begin; Gem::Ext::Builder.new(new).build_extensions; :built; rescue Gem::Ext::BuildError, SystemCallError; :tried; end
p [File.exist?(old.gem_build_complete_path), old.missing_extensions?, built, Gem.__oruby_embedded_covers?("nkf", "0.1")]`)
	if lines := strings.Split(got, "\n"); lines[len(lines)-1] != `[true, false, :tried, false]` {
		t.Errorf("extension build of embedded versions: got %s", got)
	}
}
