package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

// gemfileEntry represents a top-level gem statement in a Gemfile.
type gemfileEntry struct {
	Name       string
	Groups     []string
	Require    []string // nil = default ([name]), empty = require: false
	HasRequire bool     // true if require: option was explicitly specified
}

// lockedGemSpec represents a gem and its dependencies resolved in Gemfile.lock or GEM_PATH.
type lockedGemSpec struct {
	Name    string
	Version string
	Deps    []string
}

// orubyNativeGems lists gems that are either natively built into ORuby (in Go or C)
// or are MRI-specific C-extension tools that should not be packed from MRI's gem directory.
var orubyNativeGems = map[string]bool{
	"argon2":          true,
	"base64":          true,
	"bcrypt":          true,
	"bcrypt_pbkdf":    true,
	"bigdecimal":      true,
	"bindex":          true,
	"bootsnap":        true,
	"brotli":          true,
	"bundler":         true,
	"commonmarker":    true,
	"coverage":        true,
	"date":            true,
	"debug":           true,
	"debug_inspector": true,
	"digest":          true,
	"drb":             true,
	"ed25519":         true,
	"erubi":           true,
	"etc":             true,
	"fcntl":           true,
	"ffi":             true,
	"fiddle":          true,
	"find":            true,
	"hiredis":         true,
	"io-console":      true,
	"io-nonblock":     true,
	"io-wait":         true,
	"json":            true,
	"monitor":         true,
	"msgpack":         true,
	"mysql2":          true,
	"nio4r":           true,
	"nokogiri":        true,
	"objspace":        true,
	"oj":              true,
	"pg":              true,
	"pp":              true,
	"prettyprint":     true,
	"prism":           true,
	"psych":           true,
	"pty":             true,
	"racc":            true,
	"rbnacl":          true,
	"rbs":             true,
	"ripper":          true,
	"ruby2_keywords":  true,
	"scrypt":          true,
	"securerandom":    true,
	"set":             true,
	"shellwords":      true,
	"socket":          true,
	"sqlite3":         true,
	"stringio":        true,
	"strscan":         true,
	"syslog":          true,
	"tempfile":        true,
	"thread":          true,
	"time":            true,
	"timeout":         true,
	"tmpdir":          true,
	"uri":             true,
	"yaml":            true,
	"zlib":            true,
	"zstd":            true,
}

// railsSubGems lists the component gems of the "rails" meta-gem.
var railsSubGems = []string{
	"activesupport",
	"activemodel",
	"activerecord",
	"activejob",
	"actionview",
	"actionpack",
	"actiondispatch",
	"actioncontroller",
	"actioncable",
	"actionmailer",
	"actionmailbox",
	"actiontext",
	"activestorage",
	"railties",
}

var (
	reGemLine          = regexp.MustCompile(`^\s*gem\s+["']([^"']+)["'](.*)$`)
	reGroupBlock       = regexp.MustCompile(`^\s*group\s+(.+?)\s+do\b`)
	reSymbolOrStr      = regexp.MustCompile(`:([a-zA-Z0-9_]+)|["']([^"']+)["']`)
	reRequireFalse     = regexp.MustCompile(`\brequire\s*:\s*false\b|:\s*require\s*=>\s*false\b`)
	reRequireStr       = regexp.MustCompile(`\brequire\s*:\s*["']([^"']+)["']|:\s*require\s*=>\s*["']([^"']+)["']`)
	reRequireArr       = regexp.MustCompile(`\brequire\s*:\s*\[([^\]]*)\]|:\s*require\s*=>\s*\[([^\]]*)\]`)
	reGroupInline      = regexp.MustCompile(`\bgroup(?:s)?\s*:\s*(\[[^\]]+\]|:[a-zA-Z0-9_]+|["'][^"']+["'])`)
	reLockSpecHeader   = regexp.MustCompile(`^    ([a-zA-Z0-9_.-]+) \(([^)]+)\)$`)
	reLockSpecDep      = regexp.MustCompile(`^      ([a-zA-Z0-9_.-]+)(?: \(.*\))?$`)
	reGemspecDep       = regexp.MustCompile(`add_(?:runtime_)?dependency\(?[^"'<]*["'<]([a-zA-Z0-9_.-]+)["'>]`)
	reGemspecReqPaths  = regexp.MustCompile(`require_paths\s*=\s*\[([^\]]+)\]`)
	reGemspecPathToken = regexp.MustCompile(`["'<]([^"'>]+)["'>]`)
)

// parseGemfile parses a Gemfile to extract declared gems, their groups, and require options.
func parseGemfile(gemfilePath string) ([]gemfileEntry, error) {
	data, err := os.ReadFile(gemfilePath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	var entries []gemfileEntry
	var groupStack [][]string

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip trailing comment
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if m := reGroupBlock.FindStringSubmatch(line); m != nil {
			var grps []string
			for _, sm := range reSymbolOrStr.FindAllStringSubmatch(m[1], -1) {
				if sm[1] != "" {
					grps = append(grps, sm[1])
				} else if sm[2] != "" {
					grps = append(grps, sm[2])
				}
			}
			if len(grps) == 0 {
				grps = []string{"default"}
			}
			groupStack = append(groupStack, grps)
			continue
		}

		if strings.HasSuffix(line, " do") || strings.Contains(line, " do |") {
			// Non-group block (e.g.platforms / source)
			if len(groupStack) > 0 {
				groupStack = append(groupStack, groupStack[len(groupStack)-1])
			} else {
				groupStack = append(groupStack, []string{"default"})
			}
			continue
		}

		if line == "end" {
			if len(groupStack) > 0 {
				groupStack = groupStack[:len(groupStack)-1]
			}
			continue
		}

		m := reGemLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		rest := m[2]

		groups := []string{"default"}
		if len(groupStack) > 0 {
			groups = append([]string(nil), groupStack[len(groupStack)-1]...)
		}
		if gm := reGroupInline.FindStringSubmatch(rest); gm != nil {
			var inlineGrps []string
			for _, sm := range reSymbolOrStr.FindAllStringSubmatch(gm[1], -1) {
				if sm[1] != "" {
					inlineGrps = append(inlineGrps, sm[1])
				} else if sm[2] != "" {
					inlineGrps = append(inlineGrps, sm[2])
				}
			}
			if len(inlineGrps) > 0 {
				groups = inlineGrps
			}
		}

		entry := gemfileEntry{
			Name:   name,
			Groups: groups,
		}
		if reRequireFalse.MatchString(rest) {
			entry.HasRequire = true
			entry.Require = []string{}
		} else if rm := reRequireStr.FindStringSubmatch(rest); rm != nil {
			req := rm[1]
			if req == "" {
				req = rm[2]
			}
			entry.HasRequire = true
			entry.Require = []string{req}
		} else if am := reRequireArr.FindStringSubmatch(rest); am != nil {
			arrStr := am[1]
			if arrStr == "" {
				arrStr = am[2]
			}
			entry.HasRequire = true
			for _, sm := range reSymbolOrStr.FindAllStringSubmatch(arrStr, -1) {
				if sm[2] != "" {
					entry.Require = append(entry.Require, sm[2])
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parseGemfileLock parses Gemfile.lock and returns a map of gem name -> lockedGemSpec.
func parseGemfileLock(lockPath string) (map[string]*lockedGemSpec, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}
	specs := make(map[string]*lockedGemSpec)
	lines := strings.Split(string(data), "\n")
	inSpecs := false
	var current *lockedGemSpec

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if line == "  specs:" {
			inSpecs = true
			continue
		}
		if inSpecs && len(line) > 0 && !strings.HasPrefix(line, " ") {
			inSpecs = false
			current = nil
			continue
		}
		if !inSpecs {
			continue
		}
		if m := reLockSpecHeader.FindStringSubmatch(line); m != nil {
			name := m[1]
			ver := m[2]
			// Strip platform suffix from version if any (e.g. "1.19.1-arm64-darwin" -> "1.19.1")
			if dash := strings.IndexByte(ver, '-'); dash > 0 {
				ver = ver[:dash]
			}
			if existing, ok := specs[name]; ok {
				current = existing
			} else {
				current = &lockedGemSpec{Name: name, Version: ver}
				specs[name] = current
			}
			continue
		}
		if current != nil {
			if m := reLockSpecDep.FindStringSubmatch(line); m != nil {
				depName := m[1]
				found := false
				for _, d := range current.Deps {
					if d == depName {
						found = true
						break
					}
				}
				if !found {
					current.Deps = append(current.Deps, depName)
				}
			}
		}
	}
	return specs, nil
}

// candidateGemRoots returns all gem root directories (each containing "gems/" and "specifications/")
// searched in priority order.
func candidateGemRoots(projectRoot string) []string {
	var roots []string
	seen := make(map[string]bool)
	addRoot := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		if fi, err := os.Stat(filepath.Join(abs, "gems")); err == nil && fi.IsDir() {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}

	// 1. projectRoot/vendor/bundle/ruby/*
	if projectRoot != "" {
		if matches, err := filepath.Glob(filepath.Join(projectRoot, "vendor", "bundle", "ruby", "*")); err == nil {
			for _, m := range matches {
				addRoot(m)
			}
		}
		if matches, err := filepath.Glob(filepath.Join(projectRoot, "vendor", "gems", "ruby", "*")); err == nil {
			for _, m := range matches {
				addRoot(m)
			}
		}
	}

	// 2. GEM_HOME and GEM_PATH environment variables
	if gh := os.Getenv("GEM_HOME"); gh != "" {
		addRoot(gh)
	}
	if gp := os.Getenv("GEM_PATH"); gp != "" {
		for _, p := range filepath.SplitList(gp) {
			addRoot(p)
		}
	}

	// 3. ORuby's own gem directories (~/.gem/oruby/4.1.0 and <prefix>/lib/oruby/gems/4.1.0)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		addRoot(filepath.Join(home, ".gem", "oruby", oruby.RubyVersion))
		if matches, err := filepath.Glob(filepath.Join(home, ".gem", "ruby", "*")); err == nil {
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			for _, m := range matches {
				addRoot(m)
			}
		}
		// 4. Local version managers (mise, rbenv, asdf, rvm)
		for _, pattern := range []string{
			filepath.Join(home, ".local", "share", "mise", "installs", "ruby", "*", "lib", "ruby", "gems", "*"),
			filepath.Join(home, ".rbenv", "versions", "*", "lib", "ruby", "gems", "*"),
			filepath.Join(home, ".asdf", "installs", "ruby", "*", "lib", "ruby", "gems", "*"),
			filepath.Join(home, ".rvm", "gems", "ruby-*"),
		} {
			if matches, err := filepath.Glob(pattern); err == nil {
				sort.Sort(sort.Reverse(sort.StringSlice(matches)))
				for _, m := range matches {
					addRoot(m)
				}
			}
		}
	}

	addRoot(filepath.Join(oruby.RubyLibPrefix(), "gems", oruby.RubyVersion))
	return roots
}

type resolvedGemDir struct {
	Name         string
	Version      string
	Dir          string
	GemRoot      string
	RequirePaths []string
}

// compareGemVersionStrings compares two dotted version strings (e.g. "8.1.4.1" vs "8.0.4").
func compareGemVersionStrings(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var va, vb int
		var sa, sb string
		if i < len(pa) {
			sa = pa[i]
			va, _ = strconv.Atoi(sa)
		}
		if i < len(pb) {
			sb = pb[i]
			vb, _ = strconv.Atoi(sb)
		}
		if va != vb {
			if va > vb {
				return 1
			}
			return -1
		}
		if sa != sb {
			return strings.Compare(sa, sb)
		}
	}
	return 0
}

// findGemDir locates an installed gem directory across gemRoots.
// If exactVersion is installed, it is preferred; otherwise the highest installed version is chosen.
func findGemDir(name, exactVersion string, gemRoots []string) (*resolvedGemDir, bool) {
	if exactVersion != "" {
		for _, root := range gemRoots {
			cand := filepath.Join(root, "gems", name+"-"+exactVersion)
			if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
				return &resolvedGemDir{Name: name, Version: exactVersion, Dir: cand, GemRoot: root}, true
			}
		}
	}

	var best *resolvedGemDir
	prefix := name + "-"
	for _, root := range gemRoots {
		gemsDir := filepath.Join(root, "gems")
		entries, err := os.ReadDir(gemsDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
				continue
			}
			ver := strings.TrimPrefix(e.Name(), prefix)
			if ver == "" || ver[0] < '0' || ver[0] > '9' {
				continue
			}
			// Strip platform suffix if present
			cleanVer := ver
			if dash := strings.IndexByte(cleanVer, '-'); dash > 0 {
				cleanVer = cleanVer[:dash]
			}
			if best == nil || compareGemVersionStrings(cleanVer, best.Version) > 0 {
				best = &resolvedGemDir{
					Name:    name,
					Version: cleanVer,
					Dir:     filepath.Join(gemsDir, e.Name()),
					GemRoot: root,
				}
			}
		}
	}
	return best, best != nil
}

// readInstalledGemspecInfo parses runtime dependencies and require_paths from an installed .gemspec file.
func readInstalledGemspecInfo(g *resolvedGemDir) (deps []string, requirePaths []string) {
	requirePaths = []string{"lib"}
	specFile := filepath.Join(g.GemRoot, "specifications", filepath.Base(g.Dir)+".gemspec")
	data, err := os.ReadFile(specFile)
	if err != nil {
		defaultDir := filepath.Join(g.GemRoot, "specifications", "default")
		if matches, merr := filepath.Glob(filepath.Join(defaultDir, g.Name+"-*.gemspec")); merr == nil && len(matches) > 0 {
			data, err = os.ReadFile(matches[0])
		}
	}
	if err != nil {
		return nil, requirePaths
	}
	text := string(data)
	seen := make(map[string]bool)
	for _, m := range reGemspecDep.FindAllStringSubmatch(text, -1) {
		dep := m[1]
		if dep != "" && !seen[dep] {
			seen[dep] = true
			deps = append(deps, dep)
		}
	}
	if rm := reGemspecReqPaths.FindStringSubmatch(text); rm != nil {
		var rpaths []string
		for _, tm := range reGemspecPathToken.FindAllStringSubmatch(rm[1], -1) {
			if p := strings.TrimSpace(tm[1]); p != "" {
				rpaths = append(rpaths, p)
			}
		}
		if len(rpaths) > 0 {
			requirePaths = rpaths
		}
	}
	return deps, requirePaths
}

// shouldSkipGemForOrb reports whether gemName is provided natively by ORuby and should not be packed from RubyGems.
func shouldSkipGemForOrb(gemName string) bool {
	return orubyNativeGems[gemName]
}

// shouldSkipGemFile reports whether a file inside a gem directory should be excluded from the .orb bundle.
func shouldSkipGemFile(relPath string) bool {
	slash := filepath.ToSlash(relPath)
	parts := strings.Split(slash, "/")
	if len(parts) > 0 {
		top := parts[0]
		switch top {
		case "test", "tests", "spec", "specs", "benchmark", "benchmarks", "examples", "doc", "docs", "ext", ".git", ".github":
			return true
		}
	}
	ext := strings.ToLower(filepath.Ext(slash))
	switch ext {
	case ".c", ".h", ".cpp", ".hpp", ".o", ".so", ".bundle", ".dll", ".dylib", ".a", ".jar", ".java", ".rbs":
		return true
	}
	return false
}

// hasNativeMagic reports whether path begins with an ELF, Mach-O, or (when allowPE is true) Windows PE header.
func hasNativeMagic(path string, allowPE bool) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var hdr [4]byte
	n, err := io.ReadFull(f, hdr[:])
	if err != nil || n < 4 {
		return false
	}

	// ELF: 0x7F 'E' 'L' 'F'
	if hdr[0] == 0x7f && hdr[1] == 'E' && hdr[2] == 'L' && hdr[3] == 'F' {
		return true
	}
	// Mach-O 32/64-bit & universal fat binaries
	switch {
	case hdr[0] == 0xfe && hdr[1] == 0xed && hdr[2] == 0xfa && (hdr[3] == 0xce || hdr[3] == 0xcf),
		(hdr[0] == 0xce || hdr[0] == 0xcf) && hdr[1] == 0xfa && hdr[2] == 0xed && hdr[3] == 0xfe,
		hdr[0] == 0xca && hdr[1] == 0xfe && hdr[2] == 0xba && hdr[3] == 0xbe,
		hdr[0] == 0xbe && hdr[1] == 0xba && hdr[2] == 0xfe && hdr[3] == 0xca:
		return true
	}
	// Windows PE executable / DLL
	if allowPE && hdr[0] == 'M' && hdr[1] == 'Z' {
		return true
	}
	return false
}

// isNativeSharedLibrary reports whether path is a runtime shared library
// (.so, .so.*, .dylib, or .dll) rather than an MRI C-extension .bundle or static archive.
func isNativeSharedLibrary(path string, info os.FileInfo) bool {
	if info == nil || info.IsDir() || info.Size() < 4 {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(base))
	isSharedExt := ext == ".so" || ext == ".dylib" || ext == ".dll" || strings.Contains(base, ".so.")
	if !isSharedExt {
		return false
	}
	return hasNativeMagic(path, ext == ".dll")
}

// isNativeExecutableBinary reports whether path is a compiled native executable
// (ELF, Mach-O, or Windows PE .exe) rather than a Ruby script, shared library, or data file.
func isNativeExecutableBinary(path string, info os.FileInfo) bool {
	if info == nil || info.IsDir() || info.Size() < 4 {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, ".so.") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".rb", ".rake", ".gemspec", ".yml", ".yaml", ".json", ".md", ".txt",
		".c", ".h", ".cpp", ".hpp", ".o", ".so", ".bundle", ".dll", ".dylib", ".a", ".jar", ".java", ".rbs":
		return false
	}
	return hasNativeMagic(path, ext == ".exe")
}

func gatherNativeArtifact(src, outDir, projectGuardDir string, mode os.FileMode) (string, bool, error) {
	if outDir == "" {
		return "", false, nil
	}
	dst := filepath.Join(outDir, filepath.Base(src))
	if filepath.Clean(outDir) == filepath.Clean(projectGuardDir) {
		if _, err := os.Stat(dst); err == nil {
			return "", false, nil
		}
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", false, err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", false, err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return "", false, err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", false, err
	}
	if err := out.Close(); err != nil {
		return "", false, err
	}
	return dst, true, nil
}

// generateBundlerSetupSource creates the virtual bundler/setup.rb and bundler.rb content
// for a self-contained .orb bundle so `require "bundler/setup"` and `Bundler.require(*Rails.groups)` work seamlessly.
func generateBundlerSetupSource(entries []gemfileEntry) []byte {
	var b strings.Builder
	b.WriteString(`# Auto-generated by orubyc for self-contained .orb bundle
module Bundler
  GEMFILE_ENTRIES = [
`)
	for _, e := range entries {
		var grps []string
		for _, g := range e.Groups {
			grps = append(grps, fmt.Sprintf("%q", g))
		}
		reqs := "nil"
		if e.HasRequire {
			var rlist []string
			for _, r := range e.Require {
				rlist = append(rlist, fmt.Sprintf("%q", r))
			}
			reqs = "[" + strings.Join(rlist, ", ") + "]"
		}
		b.WriteString(fmt.Sprintf("    { name: %q, groups: [%s], require: %s },\n",
			e.Name, strings.Join(grps, ", "), reqs))
	}
	b.WriteString(`  ].freeze

  def self.setup(*groups)
    self
  end

  def self.require(*groups)
    groups = [:default] if groups.empty?
    target_groups = groups.flatten.compact.map(&:to_s)
    target_groups << "default" unless target_groups.include?("default")

    GEMFILE_ENTRIES.each do |entry|
      next if (entry[:groups] & target_groups).empty?
      reqs = entry[:require]
      if reqs.nil?
        begin
          Kernel.require(entry[:name])
        rescue LoadError
          if entry[:name].include?("-")
            begin
              Kernel.require(entry[:name].tr("-", "/"))
            rescue LoadError
            end
          end
        end
      else
        reqs.each { |r| Kernel.require(r) }
      end
    end
  end

  def self.root
    if defined?(Rails) && Rails.respond_to?(:root) && Rails.root
      Rails.root
    else
      Pathname.new(Dir.pwd) rescue Dir.pwd
    end
  end

  def self.bundle_path
    root
  end

  def self.with_unbundled_env(&block)
    block.call if block
  end

  def self.with_original_env(&block)
    block.call if block
  end

  def self.OriginalEnv
    ENV.to_h
  end
end

unless defined?(::Gem)
  module ::Gem
    def self.path
      $LOAD_PATH.grep(/\/@orb\/\d+\/gems\//).map { |p| p.sub(/\/gems\/[^\/]+\/.*$/, "") }.uniq
    end

    def self.default_dir
      path.first || "/@orb/0"
    end

    def self.loaded_specs
      @loaded_specs ||= {}
    end

    def self.try_activate(*args)
      false
    end

    class LoadError < ::LoadError; end
    class ConflictError < LoadError; end
    class MissingSpecError < LoadError; end

    class Version
      include Comparable
      attr_reader :version

      def self.new(v)
        return v if v.is_a?(Version)
        super
      end

      def initialize(v)
        @version = v.to_s
      end

      def segments
        @version.split(".").map { |s| s =~ /\A\d+\z/ ? s.to_i : s }
      end

      def <=>(other)
        other = Version.new(other) unless other.is_a?(Version)
        a = segments
        b = other.segments
        n = a.length > b.length ? a.length : b.length
        i = 0
        while i < n
          va = a[i] || 0
          vb = b[i] || 0
          cmp = (va <=> vb)
          return cmp if cmp && cmp != 0
          i += 1
        end
        0
      end

      def to_s
        @version
      end
    end

    def self.ruby_version
      Version.new(RUBY_VERSION)
    end
  end
end
`)
	return []byte(b.String())
}

// packGemfileGems resolves all gems from Gemfile / Gemfile.lock and packs their files into builder.
func packGemfileGems(
	mrb *oruby.MrbState,
	builder *oruby.OrbBuilder,
	gemfilePath string,
	projectRoot string,
	args *mrbcArgs,
	packFileWithVirtual func(inPath, vpath string) error,
) error {
	return packGemfileGemsWithGroups(mrb, builder, gemfilePath, projectRoot, nil, args, packFileWithVirtual)
}

func entryMatchesGroups(entry gemfileEntry, allowedGroups map[string]bool) bool {
	if len(allowedGroups) == 0 {
		return true
	}
	for _, g := range entry.Groups {
		if allowedGroups[g] {
			return true
		}
	}
	return false
}

// packGemfileGemsWithGroups resolves gems from Gemfile / Gemfile.lock filtered by groups (if non-empty) and packs their files into builder.
// resolveGemfileGems finds the gems of the Gemfile's groups, with their
// dependencies (Gemfile.lock, installed gemspecs), on disk: the Gemfile's
// entries and the gems found, sorted by name. Gems ORuby provides itself
// (orubyNativeGems) are left out.
func resolveGemfileGems(gemfilePath, projectRoot string, groups []string, verbose bool) ([]gemfileEntry, []*resolvedGemDir, error) {
	allEntries, err := parseGemfile(gemfilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", gemfilePath, err)
	}

	allowedGroups := make(map[string]bool)
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g != "" {
			allowedGroups[g] = true
		}
	}

	var entries []gemfileEntry
	for _, e := range allEntries {
		if entryMatchesGroups(e, allowedGroups) {
			entries = append(entries, e)
		}
	}

	lockPath := gemfilePath + ".lock"
	lockedSpecs, _ := parseGemfileLock(lockPath)

	gemRoots := candidateGemRoots(projectRoot)

	// Breadth-first traversal of required gems and their dependencies
	var queue []string
	queued := make(map[string]bool)
	enqueue := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || queued[name] {
			return
		}
		queued[name] = true
		queue = append(queue, name)
	}

	for _, e := range entries {
		enqueue(e.Name)
		if e.Name == "rails" {
			for _, sub := range railsSubGems {
				enqueue(sub)
			}
		}
	}

	var resolved []*resolvedGemDir
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		if name == "rails" {
			for _, sub := range railsSubGems {
				enqueue(sub)
			}
		}

		var lockedVer string
		if spec, ok := lockedSpecs[name]; ok && spec != nil {
			lockedVer = spec.Version
			for _, dep := range spec.Deps {
				enqueue(dep)
			}
		}

		// an embedded gem (oruby.GemSpec) stands in for its versions up to
		// its own: the locked one, else the one installed; a newer one is
		// packed and built as a gem
		if ev, embedded := oruby.EmbeddedGemVersion(name); embedded {
			v := lockedVer
			if v == "" {
				if gd, ok := findGemDir(name, "", gemRoots); ok {
					v = gd.Version
				}
			}
			if v == "" || oruby.CompareGemVersions(ev, v) >= 0 {
				continue
			}
		} else if shouldSkipGemForOrb(name) {
			continue
		}

		gemDir, ok := findGemDir(name, lockedVer, gemRoots)
		if !ok {
			if verbose {
				fmt.Fprintf(os.Stderr, "orubyc: note: gem %q not found on disk (may be built-in or unused)\n", name)
			}
			continue
		}

		// Also enqueue any dependencies from the installed .gemspec (important if Gemfile.lock had a different version or was absent)
		deps, reqPaths := readInstalledGemspecInfo(gemDir)
		gemDir.RequirePaths = reqPaths
		for _, dep := range deps {
			enqueue(dep)
		}

		resolved = append(resolved, gemDir)
	}

	// Sort resolved gems deterministically by name
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Name < resolved[j].Name
	})
	return entries, resolved, nil
}

func packGemfileGemsWithGroups(
	mrb *oruby.MrbState,
	builder *oruby.OrbBuilder,
	gemfilePath string,
	projectRoot string,
	groups []string,
	args *mrbcArgs,
	packFileWithVirtual func(inPath, vpath string) error,
) error {
	entries, resolved, err := resolveGemfileGems(gemfilePath, projectRoot, groups, args != nil && args.verbose)
	if err != nil {
		return err
	}

	var binOutDir, libOutDir string
	if args != nil && args.orbfile != "" {
		outDir := filepath.Dir(args.orbfile)
		binOutDir = filepath.Join(outDir, "bin")
		libOutDir = filepath.Join(outDir, "lib")
	}
	projectBinDir := filepath.Join(projectRoot, "bin")
	projectLibDir := filepath.Join(projectRoot, "lib")

	for _, g := range resolved {
		gemVirtBase := "gems/" + g.Name + "-" + g.Version
		reqPaths := g.RequirePaths
		if len(reqPaths) == 0 {
			reqPaths = []string{"lib"}
		}
		for _, rp := range reqPaths {
			gemLibPath := filepath.Join(g.Dir, filepath.FromSlash(rp))
			if fi, err := os.Stat(gemLibPath); err == nil && fi.IsDir() {
				_ = builder.AddDir(oruby.NormalizeOrbPath(gemVirtBase+"/"+rp), true)
			}
		}

		err := filepath.WalkDir(g.Dir, func(p string, d os.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			rel, rerr := filepath.Rel(g.Dir, p)
			if rerr != nil {
				return rerr
			}
			if rel == "." {
				return nil
			}
			if d.IsDir() {
				if shouldSkipGemFile(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			if info, iErr := d.Info(); iErr == nil {
				if isNativeSharedLibrary(p, info) {
					if dst, copied, gErr := gatherNativeArtifact(p, libOutDir, projectLibDir, 0755); gErr != nil {
						return fmt.Errorf("copying gem shared library %s: %w", p, gErr)
					} else if copied {
						args.gatheredLibs++
						if args.verbose {
							fmt.Fprintf(os.Stderr, "orubyc: gathered library %s -> %s\n", p, dst)
						}
					}
					return nil
				}
				if isNativeExecutableBinary(p, info) {
					// Native executables cannot be exec()'d from inside .orb; gather them into <outDir>/bin/ instead.
					if dst, copied, gErr := gatherNativeArtifact(p, binOutDir, projectBinDir, 0755); gErr != nil {
						return fmt.Errorf("copying gem binary %s: %w", p, gErr)
					} else if copied {
						args.gatheredBins++
						if args.verbose {
							fmt.Fprintf(os.Stderr, "orubyc: gathered binary %s -> %s\n", p, dst)
						}
					}
					return nil
				}
			}
			if shouldSkipGemFile(rel) {
				return nil
			}
			vpath := oruby.NormalizeOrbPath(gemVirtBase + "/" + filepath.ToSlash(rel))
			if err := packFileWithVirtual(p, vpath); err != nil {
				// If an optional file inside a third-party gem fails to compile, store it as raw Ruby source fallback
				if strings.HasSuffix(strings.ToLower(p), ".rb") {
					if raw, rErr := os.ReadFile(p); rErr == nil {
						return builder.AddEntry(vpath, oruby.OrbKindRuby, 0, 0644, raw)
					}
				}
				return err
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("packing gem %s-%s: %w", g.Name, g.Version, err)
		}
	}

	// Pack virtual bundler/setup.rb, bundler.rb, and bootsnap/setup.rb into the bundle's lib/
	_ = builder.AddDir("lib", true)
	bundlerSrc := generateBundlerSetupSource(entries)
	packVirtualRuby := func(vpath string, src []byte) error {
		ai := mrb.GCArenaSave()
		defer mrb.GCArenaRestore(ai)
		procVal, err := compileInputToProc(mrb, vpath, src, args)
		if err != nil || procVal.IsNil() {
			return builder.AddEntry(vpath, oruby.OrbKindRuby, 0, 0644, src)
		}
		var buf bytes.Buffer
		st, dumpErr := mrb.DumpIrepBinary(mrb.RProc(procVal).IRep(), args.flags, &buf)
		if st != oruby.MrbDumpOK || dumpErr != nil {
			return builder.AddEntry(vpath, oruby.OrbKindRuby, 0, 0644, src)
		}
		return builder.AddEntry(vpath, oruby.OrbKindMrb, 0, 0644, buf.Bytes())
	}

	if err := packVirtualRuby("lib/bundler/setup.rb", bundlerSrc); err != nil {
		return err
	}
	if err := packVirtualRuby("lib/bundler.rb", bundlerSrc); err != nil {
		return err
	}
	if err := packVirtualRuby("lib/bootsnap/setup.rb", []byte("# bootsnap no-op inside .orb bundle\n")); err != nil {
		return err
	}
	return nil
}
