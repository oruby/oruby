package main

// C extensions of a project's gems, compiled into an oruby binary through
// the Ruby C API (gem/capi), and the binary itself (`oruby build
// --target=dev|exe`, binary.go).
//
// A gem's extension is configured as `gem install` does: its extconf.rb
// runs under oruby, whose mkmf checks against the C API's headers and
// libraries and writes a Makefile. Instead of running make, the Makefile's
// sources and flags become a Go package that compiles the extension's
// sources, unchanged, with cgo and registers its Init function
// (capi.GemExtension).

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/oruby/oruby/compiler"
)

// nativeExt is a gem's C extension, configured.
type nativeExt struct {
	Gem, Version string
	Feature      string // the require name: target_prefix/TARGET ("nkf", "json/ext/parser")
	Init         string // its Init function
	BuildDir     string // where extconf.rb ran (extconf.h is there)
	Sources      []string
	CFlags       []string // -I and compiler flags
	Defines      []string // NAME or NAME=VALUE
	LDFlags      []string
}

var gemspecExtensionsRe = regexp.MustCompile(`(?m)^\s*s\.extensions\s*=\s*\[(.*)\]`)
var quotedRe = regexp.MustCompile(`"([^"]*)"`)

// gemExtensions lists the extconf.rb files of an installed gem's gemspec
// (extensions built another way, with a Rakefile or CMake, are not).
func gemExtensions(g *resolvedGemDir) []string {
	spec := filepath.Join(g.GemRoot, "specifications", g.Name+"-"+g.Version+".gemspec")
	data, err := os.ReadFile(spec)
	if err != nil {
		return nil
	}
	m := gemspecExtensionsRe.FindSubmatch(data)
	if m == nil {
		return nil
	}
	var out []string
	for _, q := range quotedRe.FindAllSubmatch(m[1], -1) {
		e := string(q[1])
		if filepath.Base(e) == "extconf.rb" {
			out = append(out, e)
		}
	}
	return out
}

// configureExtension runs extconf under oruby in a build directory below
// work and reads the Makefile mkmf wrote.
func configureExtension(orubyBin string, g *resolvedGemDir, extconf, work string, verbose bool) (*nativeExt, error) {
	src := filepath.Join(g.Dir, filepath.FromSlash(extconf))
	build := filepath.Join(work, "ext-build", g.Name+"-"+g.Version, filepath.Dir(filepath.FromSlash(extconf)))
	if err := os.RemoveAll(build); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(build, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command(orubyBin, src)
	cmd.Dir = build
	cmd.Env = append(os.Environ(), "MAKEFLAGS=")
	out, err := cmd.CombinedOutput()
	if verbose {
		os.Stderr.Write(out)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: extconf.rb failed: %v\n%s", g.Name, err, tail(out, 20))
	}
	mk, err := readMakefileVars(filepath.Join(build, "Makefile"))
	if err != nil {
		return nil, fmt.Errorf("%s: %v (extconf.rb wrote no Makefile)", g.Name, err)
	}
	ext := &nativeExt{Gem: g.Name, Version: g.Version, BuildDir: build}
	target := mk.expand("$(TARGET)")
	prefix := strings.Trim(mk.expand("$(target_prefix)"), "/")
	ext.Feature = strings.TrimPrefix(prefix+"/"+target, "/")
	ext.Init = strings.TrimSpace(mk.expand("$(TARGET_ENTRY)"))
	if ext.Init == "" {
		ext.Init = "Init_" + filepath.Base(target)
	}
	srcdir := mk.expand("$(srcdir)")
	dirs := []string{srcdir}
	for _, d := range filepath.SplitList(mk.expand("$(VPATH)")) {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	for _, s := range strings.Fields(mk.expand("$(SRCS)")) {
		found := ""
		for _, d := range append([]string{build}, dirs...) {
			p := s
			if !filepath.IsAbs(p) {
				p = filepath.Join(d, s)
			}
			if _, err := os.Stat(p); err == nil {
				found = p
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("%s: source %s not found", g.Name, s)
		}
		ext.Sources = append(ext.Sources, found)
	}
	if len(ext.Sources) == 0 {
		return nil, fmt.Errorf("%s: no sources in the Makefile", g.Name)
	}
	flags := shellSplit(mk.expand("$(INCFLAGS) $(DEFS) $(CPPFLAGS) $(CFLAGS)"))
	for _, f := range flags {
		switch {
		case strings.HasPrefix(f, "-D"):
			ext.Defines = append(ext.Defines, strings.TrimPrefix(f, "-D"))
		case f == "-I.":
			ext.CFlags = append(ext.CFlags, "-I"+build)
		case strings.HasPrefix(f, "-I") && !filepath.IsAbs(strings.TrimPrefix(f, "-I")):
			ext.CFlags = append(ext.CFlags, "-I"+filepath.Join(build, strings.TrimPrefix(f, "-I")))
		case f == "-pipe" || strings.HasPrefix(f, "-ggdb") || f == "-g":
			// cgo's own
		default:
			ext.CFlags = append(ext.CFlags, f)
		}
	}
	for _, f := range shellSplit(mk.expand("$(LOCAL_LIBS) $(LIBS) $(LDFLAGS)")) {
		switch {
		case f == "-L." || f == "-loruby-capi" || f == "-lmruby" || f == "-lm" || f == "-lpthread":
		case strings.HasPrefix(f, "-L") && strings.HasSuffix(filepath.Clean(strings.TrimPrefix(f, "-L")), "/lib") &&
			fileExists(filepath.Join(strings.TrimPrefix(f, "-L"), "liboruby-capi.a")):
			// the C API's link-test libraries: the binary has the real ones
		case strings.HasPrefix(f, "-Wl,-undefined"):
		default:
			ext.LDFlags = append(ext.LDFlags, f)
		}
	}
	return ext, nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func tail(b []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// makefileVars are the variable assignments of a Makefile mkmf wrote.
type makefileVars map[string]string

var makeAssignRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.]*)\s*[:+]?=\s*(.*)$`)

func readMakefileVars(path string) (makefileVars, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	vars := makefileVars{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var cont strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if strings.HasSuffix(line, "\\") {
			cont.WriteString(strings.TrimSuffix(line, "\\"))
			cont.WriteString(" ")
			continue
		}
		cont.WriteString(line)
		full := cont.String()
		cont.Reset()
		if strings.HasPrefix(full, "\t") || strings.HasPrefix(full, "#") {
			continue
		}
		if m := makeAssignRe.FindStringSubmatch(full); m != nil {
			vars[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return vars, sc.Err()
}

var makeRefRe = regexp.MustCompile(`\$\(([^()]+)\)|\$\{([^{}]+)\}`)

// expand replaces $(NAME) references, recursively (an unknown one is empty).
func (v makefileVars) expand(s string) string {
	for i := 0; i < 32 && strings.Contains(s, "$"); i++ {
		next := makeRefRe.ReplaceAllStringFunc(s, func(ref string) string {
			m := makeRefRe.FindStringSubmatch(ref)
			name := m[1] + m[2]
			if j := strings.IndexByte(name, ':'); j >= 0 { // $(VAR:.c=.o)
				name = name[:j]
			}
			return v[name]
		})
		if next == s {
			break
		}
		s = next
	}
	return s
}

// shellSplit splits flags as the shell running make would: quotes and
// backslashes (mkmf writes -DRUBY_EXTCONF_H=\"extconf.h\").
func shellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	in := false
	var quote rune
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
			in = true
		case r == '\\' && quote != '\'':
			esc = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			in = true
		case unicode.IsSpace(r):
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

var identRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

// writeExtensionPackage writes the Go package that compiles ext: a wrapper
// file per source that includes it after the defines, and the cgo glue.
func writeExtensionPackage(ext *nativeExt, dir, pkg string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var defs bytes.Buffer
	fmt.Fprintf(&defs, "/* the defines of %s-%s's Makefile (generated by oruby build) */\n", ext.Gem, ext.Version)
	for _, d := range ext.Defines {
		name, val, ok := strings.Cut(d, "=")
		if !ok {
			val = "1"
		}
		fmt.Fprintf(&defs, "#ifndef %s\n#define %s %s\n#endif\n", name, name, val)
	}
	if err := os.WriteFile(filepath.Join(dir, "oruby_ext_defs.h"), defs.Bytes(), 0o644); err != nil {
		return err
	}
	cxx := false
	for i, s := range ext.Sources {
		ext := filepath.Ext(s)
		wrapExt := ".c"
		switch ext {
		case ".cc", ".cpp", ".cxx", ".C":
			wrapExt = ".cc"
			cxx = true
		case ".m":
			wrapExt = ".m"
		}
		name := fmt.Sprintf("src%02d_%s%s", i, identRe.ReplaceAllString(strings.TrimSuffix(filepath.Base(s), ext), "_"), wrapExt)
		body := fmt.Sprintf("/* generated by oruby build */\n#include \"oruby_ext_defs.h\"\n#include %q\n", s)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	var g bytes.Buffer
	fmt.Fprintf(&g, "// Code generated by oruby build from %s-%s. DO NOT EDIT.\n\n", ext.Gem, ext.Version)
	fmt.Fprintf(&g, "// Package %s is the C extension %q of the gem %s, compiled\n// through ORuby's Ruby C API.\npackage %s\n\n/*\n", pkg, ext.Feature, ext.Gem, pkg)
	cflags := append([]string{"-I${SRCDIR}"}, ext.CFlags...)
	cflags = append(cflags, "-w")
	fmt.Fprintf(&g, "#cgo CFLAGS: %s\n", cgoJoin(cflags))
	if cxx {
		fmt.Fprintf(&g, "#cgo CXXFLAGS: %s\n", cgoJoin(cflags))
	}
	if len(ext.LDFlags) > 0 {
		fmt.Fprintf(&g, "#cgo LDFLAGS: %s\n", cgoJoin(ext.LDFlags))
	}
	fmt.Fprintf(&g, "void %s(void);\n*/\nimport \"C\"\n\nimport (\n\t\"unsafe\"\n\n\t\"github.com/oruby/oruby/gem/capi\"\n)\n\n", ext.Init)
	fmt.Fprintf(&g, "func init() {\n\tcapi.GemExtension(%q, %q, unsafe.Pointer(C.%s))\n}\n", ext.Gem, ext.Feature, ext.Init)
	return os.WriteFile(filepath.Join(dir, "ext.go"), g.Bytes(), 0o644)
}

// cgoJoin quotes the flags that need it for a #cgo line.
func cgoJoin(flags []string) string {
	out := make([]string, len(flags))
	for i, f := range flags {
		if strings.ContainsAny(f, " \t'\"") {
			out[i] = "'" + strings.ReplaceAll(f, "'", "") + "'"
		} else {
			out[i] = f
		}
	}
	return strings.Join(out, " ")
}

// extPolicy resolves whether a gem uses its C extension (via gem/capi) or an
// opt-in Go extension from github.com/oruby/oruby/goext.
type extPolicy struct {
	Mode       string                        // "c" (default), "go", or "auto"
	GoSet      map[string]compiler.GoExtSpec // canonical gem name -> explicitly selected Go ext
	CSet       map[string]bool               // lowercase/canonical gem name -> explicitly forced C ext
	ExplicitGo []compiler.GoExtSpec          // sorted explicit extensions.go specs
}

func newExtPolicy(cfg ExtensionsConfig) (*extPolicy, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "c"
	}
	switch mode {
	case "c", "go", "auto":
	default:
		return nil, fmt.Errorf("invalid extensions.mode %q (must be \"c\", \"go\", or \"auto\")", cfg.Mode)
	}
	explicitGo, err := compiler.ResolveGoExtList(cfg.Go)
	if err != nil {
		return nil, err
	}
	p := &extPolicy{
		Mode:       mode,
		GoSet:      make(map[string]compiler.GoExtSpec, len(explicitGo)),
		CSet:       make(map[string]bool, len(cfg.C)*2),
		ExplicitGo: explicitGo,
	}
	for _, sp := range explicitGo {
		p.GoSet[sp.Gem] = sp
	}
	for _, raw := range cfg.C {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		p.CSet[name] = true
		if sp, ok := compiler.LookupGoExt(name); ok {
			p.CSet[sp.Gem] = true
		}
	}
	return p, nil
}

// wantsGo reports whether gemName is resolved to a Go extension up front
// (explicit extensions.go or mode: go without an extensions.c override).
func (p *extPolicy) wantsGo(gemName string) (compiler.GoExtSpec, bool) {
	if p == nil {
		return compiler.GoExtSpec{}, false
	}
	sp, ok := compiler.LookupGoExt(gemName)
	if !ok {
		return compiler.GoExtSpec{}, false
	}
	if _, forced := p.GoSet[sp.Gem]; forced {
		return sp, true
	}
	if p.CSet[sp.Gem] || p.CSet[strings.ToLower(gemName)] {
		return compiler.GoExtSpec{}, false
	}
	if p.Mode == "go" {
		return sp, true
	}
	return compiler.GoExtSpec{}, false
}

// canAutoFallback reports whether gemName can fall back to a Go extension when
// its C extension cannot be built under mode: auto.
func (p *extPolicy) canAutoFallback(gemName string) (compiler.GoExtSpec, bool) {
	if p == nil || p.Mode != "auto" {
		return compiler.GoExtSpec{}, false
	}
	sp, ok := compiler.LookupGoExt(gemName)
	if !ok {
		return compiler.GoExtSpec{}, false
	}
	if p.CSet[sp.Gem] || p.CSet[strings.ToLower(gemName)] {
		return compiler.GoExtSpec{}, false
	}
	return sp, true
}

// hasNonExtconfExtensions reports whether g's gemspec declares native
// extensions that extconf.rb cannot build (e.g. Rust Cargo.toml or Rakefile).
func hasNonExtconfExtensions(g *resolvedGemDir) bool {
	spec := filepath.Join(g.GemRoot, "specifications", g.Name+"-"+g.Version+".gemspec")
	data, err := os.ReadFile(spec)
	if err != nil {
		return false
	}
	m := gemspecExtensionsRe.FindSubmatch(data)
	if m == nil {
		return false
	}
	for _, q := range quotedRe.FindAllSubmatch(m[1], -1) {
		e := string(q[1])
		if e != "" && filepath.Base(e) != "extconf.rb" {
			return true
		}
	}
	return false
}

// projectExtensions configures the C extensions of the resolved gems.
func projectExtensions(orubyBin string, gems []*resolvedGemDir, work string, verbose bool) ([]*nativeExt, []error) {
	cExts, _, errs := projectExtensionsWithPolicy(orubyBin, gems, work, nil, nil, verbose)
	return cExts, errs
}

// projectExtensionsWithPolicy resolves C extensions (via mkmf/gem/capi) and
// opt-in Go extensions (from goext/) according to pol.
func projectExtensionsWithPolicy(
	orubyBin string,
	gems []*resolvedGemDir,
	work string,
	pol *extPolicy,
	preselectedGo []compiler.GoExtSpec,
	verbose bool,
) ([]*nativeExt, []compiler.GoExtSpec, []error) {
	var exts []*nativeExt
	var errs []error
	goMap := make(map[string]compiler.GoExtSpec)
	if pol != nil {
		for _, sp := range pol.ExplicitGo {
			goMap[sp.Gem] = sp
		}
	}
	for _, sp := range preselectedGo {
		goMap[sp.Gem] = sp
	}
	sort.Slice(gems, func(i, j int) bool { return gems[i].Name < gems[j].Name })
	for _, g := range gems {
		if sp, ok := pol.wantsGo(g.Name); ok {
			goMap[sp.Gem] = sp
			continue
		}
		extconfs := gemExtensions(g)
		if len(extconfs) == 0 {
			if hasNonExtconfExtensions(g) {
				if sp, ok := pol.canAutoFallback(g.Name); ok {
					fmt.Fprintf(os.Stderr, "oruby build: using Go extension %s (non-extconf native build)\n", sp.Gem)
					goMap[sp.Gem] = sp
				}
			}
			continue
		}
		var gemExts []*nativeExt
		var gemErrs []error
		for _, e := range extconfs {
			ext, err := configureExtension(orubyBin, g, e, work, verbose)
			if err != nil {
				gemErrs = append(gemErrs, err)
				continue
			}
			gemExts = append(gemExts, ext)
		}
		if len(gemErrs) > 0 {
			if sp, ok := pol.canAutoFallback(g.Name); ok {
				fmt.Fprintf(os.Stderr, "oruby build: C extension for %s failed (%v); falling back to Go extension %s\n", g.Name, gemErrs[0], sp.ImportPath)
				goMap[sp.Gem] = sp
				continue
			}
			errs = append(errs, gemErrs...)
		}
		exts = append(exts, gemExts...)
	}
	goExts := make([]compiler.GoExtSpec, 0, len(goMap))
	for _, sp := range goMap {
		goExts = append(goExts, sp)
	}
	sort.Slice(goExts, func(i, j int) bool { return goExts[i].Gem < goExts[j].Gem })
	return exts, goExts, errs
}
