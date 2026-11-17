package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
}
