package main

// An oruby executable for a project (`oruby build --target=dev|exe|native`):
//   - dev / exe: the oruby command (cmd/oruby, copied unchanged) built with the
//     C extensions (native.go) and opt-in Go extensions (goext/) of the project's
//     gems compiled in, and for exe with the project's .orb bundle embedded.
//   - native: AOT-compiled Go code (compiler/) plus the same C and opt-in Go
//     extensions in a ready-to-build Go module.
//
// It is a Go build of a module generated in <project>/tmp/oruby-build (or
// <output>_go for native) that uses the ORuby source tree (a replace directive),
// so it needs that tree, with libmruby built, and a Go toolchain.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/compiler"
)

// orubySourceRoot is the ORuby source tree the binary is built from:
// ORUBY_SRC, or the install prefix where it is one (an in-tree build).
func orubySourceRoot() (string, error) {
	cands := []string{os.Getenv("ORUBY_SRC"), oruby.InstallPrefix()}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if fileExists(filepath.Join(c, "go.mod")) && fileExists(filepath.Join(c, "cmd", "oruby", "oruby.go")) {
			if !fileExists(filepath.Join(c, "mruby", "build", "host", "lib", "libmruby.a")) {
				return "", fmt.Errorf("%s: libmruby is not built (run make there)", c)
			}
			return c, nil
		}
	}
	return "", fmt.Errorf("an oruby binary is built from the ORuby source tree: set ORUBY_SRC to it")
}

type binaryOptions struct {
	Out      string               // the executable
	Work     string               // the generated module
	Exts     []*nativeExt         // compiled-in C extensions (gem/capi)
	GoExts   []compiler.GoExtSpec // opt-in Go extensions (goext/)
	OrbImage string               // exe: the .orb to embed
	OrbName  string               // exe: the name it runs under ($0's .orb)
	Prefix   string               // dev: the ORuby install whose libraries it uses
	Verbose  bool
}

var goModuleRe = regexp.MustCompile(`(?m)^module\s+\S+\s*$`)
var goIgnoreRe = regexp.MustCompile(`(?m)^ignore\s+.*\r?\n`)
var goReplaceRelRe = regexp.MustCompile(`(?m)^(replace\s+\S+(?:\s+\S+)?\s+=>\s+)(\.\.?/\S*)`)

func hasGoExt(list []compiler.GoExtSpec, gem string) bool {
	for _, sp := range list {
		if sp.Gem == gem {
			return true
		}
	}
	return false
}

// writeGeneratedGoMod writes go.mod and go.sum for a generated project module
// (orubyapp) pointing at the ORuby source tree and, when needGoExt is true,
// the nested goext module.
func writeGeneratedGoMod(root, work string, needGoExt bool) error {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	modText := goModuleRe.ReplaceAllString(string(mod), "module orubyapp")
	modText = goIgnoreRe.ReplaceAllString(modText, "")
	modText = goReplaceRelRe.ReplaceAllStringFunc(modText, func(s string) string {
		m := goReplaceRelRe.FindStringSubmatch(s)
		return m[1] + filepath.Join(root, m[2])
	})
	modText += fmt.Sprintf("\nrequire github.com/oruby/oruby v0.0.0-00010101000000-000000000000\n\nreplace github.com/oruby/oruby => %s\n", root)
	if needGoExt {
		goextRoot := filepath.Join(root, "goext")
		modText += fmt.Sprintf("\nrequire %s v0.0.0-00010101000000-000000000000\n\nreplace %s => %s\n",
			compiler.GoExtModule, compiler.GoExtModule, goextRoot)
	}
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(modText), 0o644); err != nil {
		return err
	}

	// Merge root go.sum (and goext/go.sum when goext is used) so builds work offline.
	var sumLines []string
	seen := map[string]bool{}
	addSum := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			sumLines = append(sumLines, line)
		}
	}
	addSum(filepath.Join(root, "go.sum"))
	if needGoExt {
		addSum(filepath.Join(root, "goext", "go.sum"))
	}
	if len(sumLines) > 0 {
		if err := os.WriteFile(filepath.Join(work, "go.sum"), []byte(strings.Join(sumLines, "\n")+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// buildBinary generates the module and builds the executable.
func buildBinary(root string, o binaryOptions) error {
	if err := writeGeneratedGoMod(root, o.Work, len(o.GoExts) > 0); err != nil {
		return err
	}
	// main: the oruby command's sources
	mainDir := filepath.Join(o.Work, "main")
	if err := os.RemoveAll(mainDir); err != nil {
		return err
	}
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		return err
	}
	srcs, err := filepath.Glob(filepath.Join(root, "cmd", "oruby", "*.go"))
	if err != nil {
		return err
	}
	skipOjCExt := hasGoExt(o.GoExts, "oj")
	for _, s := range srcs {
		base := filepath.Base(s)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		// If the project opted into goext/oruby-oj, omit standalone oruby's
		// compiled-in gem/oj C extension so the two don't register "oj" twice.
		if skipOjCExt && base == "ext_oj.go" {
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(mainDir, base), data, 0o644); err != nil {
			return err
		}
	}
	// C extensions (gem/capi)
	extRoot := filepath.Join(o.Work, "ext")
	if err := os.RemoveAll(extRoot); err != nil {
		return err
	}
	var imports []string
	for i, e := range o.Exts {
		pkg := fmt.Sprintf("ext%02d_%s", i, identRe.ReplaceAllString(e.Gem, "_"))
		if err := writeExtensionPackage(e, filepath.Join(extRoot, pkg), pkg); err != nil {
			return err
		}
		imports = append(imports, "orubyapp/ext/"+pkg)
	}
	// Opt-in Go extensions (goext/)
	for _, ge := range o.GoExts {
		imports = append(imports, ge.ImportPath)
	}

	var g bytes.Buffer
	g.WriteString("// Code generated by oruby build. DO NOT EDIT.\n\npackage main\n\nimport (\n")
	if o.OrbImage != "" {
		g.WriteString("\t_ \"embed\"\n")
	}
	if o.Prefix != "" {
		g.WriteString("\n\t\"github.com/oruby/oruby\"\n")
	}
	for _, imp := range imports {
		fmt.Fprintf(&g, "\t_ %q\n", imp)
	}
	g.WriteString(")\n\n")
	if o.OrbImage != "" {
		data, err := os.ReadFile(o.OrbImage)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(mainDir, "app.orb"), data, 0o644); err != nil {
			return err
		}
		g.WriteString("//go:embed app.orb\nvar embeddedOrbImage []byte\n\n")
		fmt.Fprintf(&g, "func init() {\n\tembeddedOrb = embeddedOrbImage\n\tembeddedOrbName = %q\n}\n", o.OrbName)
	}
	if o.Prefix != "" {
		fmt.Fprintf(&g, "\n// the libraries of the ORuby that built this binary\nfunc init() { oruby.SetInstallPrefix(%q) }\n", o.Prefix)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "zz_oruby_build.go"), g.Bytes(), 0o644); err != nil {
		return err
	}
	out, err := filepath.Abs(o.Out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "./main")
	cmd.Dir = o.Work
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOFLAGS=-mod=mod", "GOWORK=off",
		"CGO_CFLAGS_ALLOW=.*", "CGO_CXXFLAGS_ALLOW=.*", "CGO_LDFLAGS_ALLOW=.*")
	cmd.Stdout = os.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if o.Verbose {
		fmt.Fprintf(os.Stderr, "orubyc: go build in %s\n", o.Work)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %v\n%s", err, tail(stderr.Bytes(), 30))
	}
	return nil
}

// resolveProjectExtensions resolves both C extensions (via mkmf/gem/capi) and
// opt-in Go extensions (from goext/) for projectDir according to cfg and pol.
func resolveProjectExtensions(prog, projectDir string, cfg *OrubyConfig, groups []string, work string, pol *extPolicy, verbose bool) ([]*nativeExt, []compiler.GoExtSpec, error) {
	var gems []*resolvedGemDir
	var preselectedGo []compiler.GoExtSpec
	if cfg.Gems.Enabled {
		gemfile := cfg.Gems.Gemfile
		if gemfile == "" {
			gemfile = "Gemfile"
		}
		if !filepath.IsAbs(gemfile) {
			gemfile = filepath.Join(projectDir, filepath.FromSlash(gemfile))
		}
		if fileExists(gemfile) {
			var err error
			_, gems, preselectedGo, err = resolveGemfileGemsWithPolicy(gemfile, projectDir, groups, pol, verbose)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	orubyBin := filepath.Join(oruby.InstallPrefix(), "bin", "oruby")
	exts, goExts, errs := projectExtensionsWithPolicy(orubyBin, gems, work, pol, preselectedGo, verbose)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "%s build: warning: C extension left out: %v\n", prog, e)
	}
	for _, e := range exts {
		fmt.Printf("C extension %s (%s-%s)\n", e.Feature, e.Gem, e.Version)
	}
	for _, ge := range goExts {
		fmt.Printf("Go extension %s (%s)\n", ge.Gem, ge.ImportPath)
	}
	return exts, goExts, nil
}

// buildProjectBinary builds the project's executable: the gems' C extensions
// and opt-in Go extensions compiled in, and orbPath embedded when set (exe),
// or ORuby's libraries used from its install (dev).
func buildProjectBinary(prog, projectDir string, cfg *OrubyConfig, groups []string, pol *extPolicy, orbPath, out string, verbose bool) error {
	root, err := orubySourceRoot()
	if err != nil {
		return err
	}
	work := filepath.Join(projectDir, "tmp", "oruby-build")
	exts, goExts, err := resolveProjectExtensions(prog, projectDir, cfg, groups, work, pol, verbose)
	if err != nil {
		return err
	}
	o := binaryOptions{Out: out, Work: work, Exts: exts, GoExts: goExts, Verbose: verbose}
	if orbPath != "" {
		o.OrbImage = orbPath
		o.OrbName = filepath.Base(orbPath)
	} else {
		o.Prefix = oruby.InstallPrefix()
	}
	return buildBinary(root, o)
}

// buildNativeProject generates a hybrid Go AOT module in goOutDir containing:
//   - go.mod / go.sum pointing at ORuby (and goext/ when Go extensions are used)
//   - ext/extNN_<gem>/ packages for compiled-in C extensions (gem/capi)
//   - main/ with compiler-generated AOT Go files (linking selected Go extensions
//     and enabling Category A pure-Go intrinsics) plus main/zz_oruby_build.go
//     importing any compiled-in C extensions.
func buildNativeProject(mrb *oruby.MrbState, prog, projectDir string, cfg *OrubyConfig, pol *extPolicy, orbPath, goOutDir string, verbose bool) error {
	root, err := orubySourceRoot()
	if err != nil {
		return err
	}
	exts, goExts, err := resolveProjectExtensions(prog, projectDir, cfg, cfg.Gems.Groups, goOutDir, pol, verbose)
	if err != nil {
		return err
	}
	if err := writeGeneratedGoMod(root, goOutDir, len(goExts) > 0); err != nil {
		return err
	}
	extRoot := filepath.Join(goOutDir, "ext")
	if err := os.RemoveAll(extRoot); err != nil {
		return err
	}
	var cImports []string
	for i, e := range exts {
		pkg := fmt.Sprintf("ext%02d_%s", i, identRe.ReplaceAllString(e.Gem, "_"))
		if err := writeExtensionPackage(e, filepath.Join(extRoot, pkg), pkg); err != nil {
			return err
		}
		cImports = append(cImports, "orubyapp/ext/"+pkg)
	}

	var rbsPaths []string
	for _, rp := range cfg.Compiler.RBS {
		if !filepath.IsAbs(rp) {
			rp = filepath.Join(projectDir, filepath.FromSlash(rp))
		}
		rbsPaths = append(rbsPaths, rp)
	}
	mainDir := filepath.Join(goOutDir, "main")
	comp := compiler.New(compiler.Options{
		PackageName: "main",
		Standalone:  true,
		GoExts:      goExts,
		RBSPaths:    rbsPaths,
		CheckRBS:    cfg.Compiler.CheckRBS,
		GroupSize:   -1,
		NoFormat:    true,
	})
	if _, err := comp.CompilePackageDir(mrb, orbPath, nil, nil, mainDir); err != nil {
		return fmt.Errorf("native Go codegen failed: %w", err)
	}
	for _, d := range comp.RBSDiagnostics {
		fmt.Fprintf(os.Stderr, "%s build: warning: RBS: %s\n", prog, d)
	}

	zzPath := filepath.Join(mainDir, "zz_oruby_build.go")
	if len(cImports) > 0 {
		var g bytes.Buffer
		g.WriteString("// Code generated by oruby build --target=native. DO NOT EDIT.\n\npackage main\n\nimport (\n")
		for _, imp := range cImports {
			fmt.Fprintf(&g, "\t_ %q\n", imp)
		}
		g.WriteString(")\n")
		if err := os.WriteFile(zzPath, g.Bytes(), 0o644); err != nil {
			return err
		}
	} else {
		_ = os.Remove(zzPath)
	}
	return nil
}

// packStdlib adds ORuby's Ruby libraries (lib/oruby/<version>) to an exe's
// bundle under stdlib/, on its load path: the executable runs where ORuby is
// not installed. RubyGems and Bundler are not (a bundle runs without them),
// nor the build tools.
func packStdlib(builder *oruby.OrbBuilder, pack func(inPath, vpath string) error) error {
	dir := filepath.Join(oruby.RubyLibPrefix(), oruby.RubyVersion)
	skip := map[string]bool{"rubygems": true, "rubygems.rb": true, "bundler": true, "bundler.rb": true, "mkmf": true, "mkmf.rb": true}
	if err := builder.AddDir("stdlib", true); err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if skip[top] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		return pack(p, "stdlib/"+filepath.ToSlash(rel))
	})
}
