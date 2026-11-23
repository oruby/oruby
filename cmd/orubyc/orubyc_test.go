package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oruby/oruby"
)

func TestOrubycBatchDirectoryAndInplaceRb(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	srcDir := t.TempDir()
	outDir := t.TempDir()

	appModel := filepath.Join(srcDir, "app", "models", "user.rb")
	if err := os.MkdirAll(filepath.Dir(appModel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appModel, []byte("class User; def greet = 'hello'; end\n"), 0644); err != nil {
		t.Fatal(err)
	}

	binScript := filepath.Join(srcDir, "bin", "run.rb")
	if err := os.MkdirAll(filepath.Dir(binScript), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binScript, []byte("#!/usr/bin/env oruby\nUser.new.greet\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Recursive compile to outDir as .mrb files
	args := mrbcArgs{
		prog:      "orubyc",
		outdir:    outDir,
		recursive: true,
		argv:      []string{filepath.Join(srcDir, "app")},
		flags:     oruby.MrbDumpDebugInfo,
	}
	if code := runBatch(mrb, &args); code != 0 {
		t.Fatalf("runBatch to outDir exited with %d", code)
	}

	compiledMrb := filepath.Join(outDir, "app", "models", "user.mrb")
	mrbData, err := os.ReadFile(compiledMrb)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", compiledMrb, err)
	}
	if !oruby.IsRiteBinary(mrbData) {
		t.Fatalf("expected %s to contain RITE binary", compiledMrb)
	}

	// 2. Compile in-place as .rb preserving shebang
	inplaceArgs := mrbcArgs{
		prog:      "orubyc",
		inplaceRb: true,
		argv:      []string{binScript},
		flags:     oruby.MrbDumpDebugInfo,
	}
	if code := runBatch(mrb, &inplaceArgs); code != 0 {
		t.Fatalf("runBatch inplace-rb exited with %d", code)
	}

	inplaceData, err := os.ReadFile(binScript)
	if err != nil {
		t.Fatal(err)
	}
	if !oruby.IsRiteBinary(inplaceData) {
		t.Fatalf("expected in-place .rb to be detected as RITE binary")
	}

	// 3. Execute both compiled files in a fresh MrbState
	runMrb := oruby.MrbOpen()
	defer runMrb.Close()
	if _, err := runMrb.LoadIrep(mrbData); err != nil {
		t.Fatalf("LoadIrep user.mrb failed: %v", err)
	}
	res, err := runMrb.LoadIrep(inplaceData)
	if err != nil {
		t.Fatalf("LoadIrep run.rb (with shebang + RITE) failed: %v", err)
	}
	if got := res.String(); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}

	// 4. Pack directory tree into a single .orb bundle via runOrbBuild
	orbOut := filepath.Join(t.TempDir(), "bundle.orb")
	orbArgs := mrbcArgs{
		prog:       "orubyc",
		orbfile:    orbOut,
		entrypoint: "app/models/user.rb",
		argv:       []string{srcDir},
		flags:      oruby.MrbDumpDebugInfo,
	}
	if code := runOrbBuild(mrb, &orbArgs); code != 0 {
		t.Fatalf("runOrbBuild exited with %d", code)
	}
	orbFile, err := oruby.ReadOrbFile(orbOut)
	if err != nil {
		t.Fatalf("ReadOrbFile: %v", err)
	}
	ep, ok := orbFile.Entrypoint()
	if !ok || ep.Path != "app/models/user.rb" {
		t.Fatalf("expected entrypoint app/models/user.rb, got %+v (%v)", ep, ok)
	}
}

func TestOrubycOrbGemfilePacking(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// Create a fake gem root with gems/ and specifications/
	gemRoot := t.TempDir()
	gemsDir := filepath.Join(gemRoot, "gems")
	specsDir := filepath.Join(gemRoot, "specifications")
	if err := os.MkdirAll(gemsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Gem 1: demo_widget-1.2.0 (depends on demo_helper)
	widgetLib := filepath.Join(gemsDir, "demo_widget-1.2.0", "lib")
	if err := os.MkdirAll(widgetLib, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(widgetLib, "demo_widget.rb"), []byte(`
require "demo_helper"
module DemoWidget
  def self.render(name)
    "Widget<#{DemoHelper.format(name)}>"
  end
end
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "demo_widget-1.2.0.gemspec"), []byte(`
Gem::Specification.new do |s|
  s.name = "demo_widget".freeze
  s.version = "1.2.0".freeze
  s.require_paths = ["lib".freeze]
  s.add_runtime_dependency(%q<demo_helper>.freeze, [">= 0.1.0".freeze])
end
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Gem 2: demo_helper-0.5.0 with custom require_paths = ["lib/demo_helper_core"]
	helperLib := filepath.Join(gemsDir, "demo_helper-0.5.0", "lib", "demo_helper_core")
	if err := os.MkdirAll(helperLib, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(helperLib, "demo_helper.rb"), []byte(`
module DemoHelper
  def self.format(s)
    s.to_s.upcase
  end
end
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "demo_helper-0.5.0.gemspec"), []byte(`
Gem::Specification.new do |s|
  s.name = "demo_helper".freeze
  s.version = "0.5.0".freeze
  s.require_paths = ["lib/demo_helper_core".freeze]
end
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an app directory with a Gemfile, config/boot.rb, and app.rb
	appDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(appDir, "Gemfile"), []byte(`
source "https://rubygems.org"
gem "demo_widget", "~> 1.2"
gem "bootsnap", require: false
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(appDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config", "boot.rb"), []byte(`
require "bundler/setup"
require "bootsnap/setup"
Bundler.require(:default)
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.rb"), []byte(`
require_relative "config/boot"
DemoWidget.render("rails")
`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GEM_PATH", gemRoot)

	orbOut := filepath.Join(t.TempDir(), "app_with_gems.orb")
	orbArgs := mrbcArgs{
		prog:       "orubyc",
		orbfile:    orbOut,
		entrypoint: "app.rb",
		argv:       []string{appDir},
		flags:      oruby.MrbDumpDebugInfo,
	}
	if code := runOrbBuild(mrb, &orbArgs); code != 0 {
		t.Fatalf("runOrbBuild exited with %d", code)
	}

	// Verify both gems and bundler/setup were packed into the .orb archive
	orbFile, err := oruby.ReadOrbFile(orbOut)
	if err != nil {
		t.Fatalf("ReadOrbFile: %v", err)
	}
	for _, expectedPath := range []string{
		"lib/bundler/setup.rb",
		"lib/bootsnap/setup.rb",
		"gems/demo_widget-1.2.0/lib/demo_widget.rb",
		"gems/demo_helper-0.5.0/lib/demo_helper_core/demo_helper.rb",
		"config/boot.rb",
		"app.rb",
	} {
		if _, ok := orbFile.Lookup(expectedPath); !ok {
			t.Fatalf("expected %q to be packed in .orb archive", expectedPath)
		}
	}

	// Remove the original appDir and gemRoot from disk and execute the .orb in a fresh MrbState!
	_ = os.RemoveAll(appDir)
	_ = os.RemoveAll(gemRoot)

	runMrb := oruby.MrbOpen()
	defer runMrb.Close()

	mounted, err := runMrb.MountOrbFile(orbOut)
	if err != nil {
		t.Fatalf("MountOrbFile: %v", err)
	}
	ep, ok := mounted.ResolveEntrypoint("app.rb")
	if !ok {
		t.Fatalf("expected entrypoint app.rb in mounted orb")
	}
	val, err := runMrb.ExecuteOrbEntry(mounted, ep, nil)
	if err != nil {
		t.Fatalf("ExecuteOrbEntry failed: %v", err)
	}
	if got := val.String(); got != "Widget<RAILS>" {
		t.Fatalf("got %q, want %q", got, "Widget<RAILS>")
	}
}

func TestOrubyInitAndBuildWithFiltersAndPublicToggle(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// Create fake gem root with a production gem and a development-only gem
	gemRoot := t.TempDir()
	for _, g := range []struct {
		name, ver, code string
	}{
		{"prod_gem", "1.0.0", "module ProdGem; def self.tag = 'prod_ok'; end"},
		{"dev_only_gem", "1.0.0", "module DevOnlyGem; def self.tag = 'dev_ok'; end"},
	} {
		gLib := filepath.Join(gemRoot, "gems", g.name+"-"+g.ver, "lib")
		if err := os.MkdirAll(gLib, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gLib, g.name+".rb"), []byte(g.code), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GEM_PATH", gemRoot)

	// Create a Rails-shaped project directory containing secrets, dotfiles, gitignored files, log/, storage/, public/
	appDir := t.TempDir()
	mustWrite := func(rel, content string) {
		full := filepath.Join(appDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("Gemfile", `
source "https://rubygems.org"
gem "prod_gem"
group :development, :test do
  gem "dev_only_gem"
end
`)
	mustWrite(".gitignore", "app/models/scratch_ignored.rb\n")
	mustWrite(".env", "SECRET_TOKEN=topsecret\n")
	mustWrite(".claude/settings.json", "{}\n")
	mustWrite("app/.hidden/secret.rb", "HIDDEN = 1\n")
	mustWrite("app/models/user.rb", "class User; def self.name = 'Alice'; end\n")
	mustWrite("app/models/scratch_ignored.rb", "IGNORED = 1\n")
	mustWrite("config/application.rb", "require 'prod_gem'\n")
	mustWrite("config/environment.rb", "require_relative 'application'\n")
	mustWrite("config/master.key", "0123456789abcdef\n")
	mustWrite("config/credentials.yml.enc", "encrypted_payload\n")
	mustWrite("log/production.log", "log line\n")
	mustWrite("storage/blob1", "binary blob\n")
	mustWrite("tmp/cache/data", "temp cache\n")
	mustWrite("public/robots.txt", "User-agent: *\n")
	mustWrite("run.rb", `
require_relative "config/environment"
require_relative "app/models/user"
"#{User.name}:#{ProdGem.tag}"
`)

	// 1. Run `oruby init --docker`
	if code := runInitSubcommand([]string{"--docker", appDir}); code != 0 {
		t.Fatalf("runInitSubcommand exited with %d", code)
	}
	cfgPath := filepath.Join(appDir, DefaultConfigFilename)
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected %s to be created: %v", cfgPath, err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "Dockerfile.oruby")); err != nil {
		t.Fatalf("expected Dockerfile.oruby to be created: %v", err)
	}

	// 2. Run `oruby build` (default embed_public: false)
	if code := runBuildSubcommand(mrb, "oruby", []string{appDir}); code != 0 {
		t.Fatalf("runBuildSubcommand exited with %d", code)
	}

	orbPath := filepath.Join(appDir, "build", filepath.Base(appDir)+".orb")
	orbFile, err := oruby.ReadOrbFile(orbPath)
	if err != nil {
		t.Fatalf("ReadOrbFile(%s): %v", orbPath, err)
	}

	// Verify included files and production gem are present
	for _, want := range []string{
		"app/models/user.rb",
		"config/application.rb",
		"config/credentials.yml.enc",
		"gems/prod_gem-1.0.0/lib/prod_gem.rb",
		"run.rb",
	} {
		if _, ok := orbFile.Lookup(want); !ok {
			t.Fatalf("expected %q to be packed in .orb", want)
		}
	}

	// Verify excluded files (master.key, dotfiles, gitignored, log, storage, tmp, public, dev_only_gem) are NOT in .orb
	for _, forbidden := range []string{
		"config/master.key",
		".env",
		".claude/settings.json",
		"app/.hidden/secret.rb",
		"app/models/scratch_ignored.rb",
		"log/production.log",
		"storage/blob1",
		"tmp/cache/data",
		"public/robots.txt",
		"gems/dev_only_gem-1.0.0/lib/dev_only_gem.rb",
	} {
		if _, ok := orbFile.Lookup(forbidden); ok {
			t.Fatalf("expected %q to be EXCLUDED from .orb, but it was packed!", forbidden)
		}
	}

	// Verify standalone public/ fallback on disk works when mounted!
	runMrb := oruby.MrbOpen()
	defer runMrb.Close()
	mounted, err := runMrb.MountOrbFile(orbPath)
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := runMrb.OrbReadAsset(mounted.MountRoot + "/public/robots.txt"); !ok || !strings.Contains(string(data), "User-agent") {
		t.Fatalf("expected disk fallback for /@orb/0/public/robots.txt to succeed, got ok=%v data=%q", ok, string(data))
	}

	// 3. Re-run `oruby build --embed-public` and verify public/robots.txt IS embedded inside .orb
	if code := runBuildSubcommand(mrb, "oruby", []string{"--embed-public", appDir}); code != 0 {
		t.Fatalf("runBuildSubcommand --embed-public exited with %d", code)
	}
	orbEmbedded, err := oruby.ReadOrbFile(orbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := orbEmbedded.Lookup("public/robots.txt"); !ok {
		t.Fatalf("expected public/robots.txt to be embedded when --embed-public is set")
	}
}

func TestOrubyBuildGathersNativeGemBinariesAndTwoStageDockerfile(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	gemRoot := t.TempDir()
	thrusterDir := filepath.Join(gemRoot, "gems", "thruster-0.1.27")
	if err := os.MkdirAll(filepath.Join(thrusterDir, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(thrusterDir, "exe", "arm64-darwin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thrusterDir, "lib", "thruster.rb"), []byte("module Thruster; end\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thrusterDir, "exe", "thrust"), []byte("#!/usr/bin/env ruby\nputs 'launcher'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	fakeMachO := []byte{0xcf, 0xfa, 0xed, 0xfe, 0x0c, 0x00, 0x00, 0x01, 't', 'h', 'r', 'u', 's', 't'}
	if err := os.WriteFile(filepath.Join(thrusterDir, "exe", "arm64-darwin", "thrust"), fakeMachO, 0755); err != nil {
		t.Fatal(err)
	}
	fakeELFSo := []byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00, 'v', 'i', 'p', 's'}
	if err := os.WriteFile(filepath.Join(thrusterDir, "lib", "libvips.so.42"), fakeELFSo, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEM_PATH", gemRoot)

	appDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(appDir, "Gemfile"), []byte("source 'https://rubygems.org'\ngem 'thruster'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.rb"), []byte("require 'thruster'\n'ok'\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if code := runInitSubcommand([]string{"--docker", appDir}); code != 0 {
		t.Fatalf("runInitSubcommand exited with %d", code)
	}
	dfBytes, err := os.ReadFile(filepath.Join(appDir, "Dockerfile.oruby"))
	if err != nil {
		t.Fatal(err)
	}
	df := string(dfBytes)
	for _, want := range []string{
		"AS builder",
		"AS runtime",
		"/usr/local/bin/oruby build",
		"COPY --from=builder /src/build/bin/ /app/bin/",
		"COPY --from=builder /src/build/lib/ /app/lib/",
		`LD_LIBRARY_PATH="/app/lib"`,
	} {
		if !strings.Contains(df, want) {
			t.Fatalf("expected Dockerfile.oruby to contain %q, got:\n%s", want, df)
		}
	}

	if code := runBuildSubcommand(mrb, "oruby", []string{appDir}); code != 0 {
		t.Fatalf("runBuildSubcommand exited with %d", code)
	}

	// Native binary should be gathered into build/bin/thrust
	gatheredPath := filepath.Join(appDir, "build", "bin", "thrust")
	gotBin, err := os.ReadFile(gatheredPath)
	if err != nil {
		t.Fatalf("expected gathered binary at %s: %v", gatheredPath, err)
	}
	if string(gotBin) != string(fakeMachO) {
		t.Fatalf("gathered binary mismatch: got %x, want %x", gotBin, fakeMachO)
	}
	fi, err := os.Stat(gatheredPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0111 == 0 {
		t.Fatalf("expected gathered binary to be executable, mode=%v", fi.Mode())
	}

	// Shared library should be gathered into build/lib/libvips.so.42
	gatheredLibPath := filepath.Join(appDir, "build", "lib", "libvips.so.42")
	gotLib, err := os.ReadFile(gatheredLibPath)
	if err != nil {
		t.Fatalf("expected gathered shared library at %s: %v", gatheredLibPath, err)
	}
	if string(gotLib) != string(fakeELFSo) {
		t.Fatalf("gathered shared library mismatch: got %x, want %x", gotLib, fakeELFSo)
	}

	// Native binary and shared library must NOT be packed inside .orb; lib/thruster.rb MUST be packed.
	orbPath := filepath.Join(appDir, "build", filepath.Base(appDir)+".orb")
	orbFile, err := oruby.ReadOrbFile(orbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []string{
		"gems/thruster-0.1.27/exe/arm64-darwin/thrust",
		"gems/thruster-0.1.27/lib/libvips.so.42",
	} {
		if _, ok := orbFile.Lookup(excluded); ok {
			t.Fatalf("expected %q to be excluded from .orb", excluded)
		}
	}
	if _, ok := orbFile.Lookup("gems/thruster-0.1.27/lib/thruster.rb"); !ok {
		t.Fatalf("expected gems/thruster-0.1.27/lib/thruster.rb to be packed in .orb")
	}
}
