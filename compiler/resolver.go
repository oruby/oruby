package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oruby/oruby"
)

// ModuleUnit represents a single precompiled Ruby file.
type ModuleUnit struct {
	FilePath  string    // Absolute path on disk
	RelPath   string    // Normalized relative path (e.g. "calculator.rb", "models/user.rb")
	FuncName  string    // Name of the Go entry function
	Root      *IRepUnit // Root IRepUnit of this file
	LoadPaths []string  // Search paths for resolving relative require paths
}

// DependencyResolver finds and resolves all require/require_relative dependencies.
type DependencyResolver struct {
	mrb       *oruby.MrbState
	loadPaths []string
	visited   map[string]*ModuleUnit
}

// NewDependencyResolver creates a resolver.
func NewDependencyResolver(mrb *oruby.MrbState, loadPaths []string) *DependencyResolver {
	return &DependencyResolver{
		mrb:       mrb,
		loadPaths: loadPaths,
		visited:   make(map[string]*ModuleUnit),
	}
}

var reqRegex = regexp.MustCompile(`(?m)^\s*(require|require_relative)\s*\(?\s*['"]([^'"]+)['"]\)?`)

// Resolve walks the dependency tree starting from entryFile.
func (r *DependencyResolver) Resolve(entryFile string) ([]*ModuleUnit, error) {
	absEntry, err := filepath.Abs(entryFile)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute path for %s: %w", entryFile, err)
	}

	rootDir := filepath.Dir(absEntry)
	var modules []*ModuleUnit
	moduleIndex := 0

	err = r.resolveRecursive(absEntry, rootDir, &modules, &moduleIndex)
	if err != nil {
		return nil, err
	}

	return modules, nil
}

// ResolveWith resolves entryFile and additional files (and their
// dependencies) into one bundle. Extra files can be loaded at runtime with
// load/require by their absolute path. Extra files which fail to compile
// are skipped and reported in skipped.
func (r *DependencyResolver) ResolveWith(entryFile string, extra []string) (modules []*ModuleUnit, skipped []error, err error) {
	modules, err = r.Resolve(entryFile)
	if err != nil {
		return nil, nil, err
	}
	absEntry, _ := filepath.Abs(entryFile)
	rootDir := filepath.Dir(absEntry)
	for _, f := range extra {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, nil, err
		}
		// resolve into a separate list so a failing file leaves no partial modules
		var part []*ModuleUnit
		counter := len(modules)
		visited := make(map[string]*ModuleUnit, len(r.visited))
		for k, v := range r.visited {
			visited[k] = v
		}
		if err := r.resolveRecursive(abs, rootDir, &part, &counter); err != nil {
			r.visited = visited
			skipped = append(skipped, err)
			continue
		}
		modules = append(modules, part...)
	}
	return modules, skipped, nil
}

func (r *DependencyResolver) resolveRecursive(absPath, rootDir string, list *[]*ModuleUnit, counter *int) error {
	if _, ok := r.visited[absPath]; ok {
		return nil // already resolved
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("reading ruby file %s: %w", absPath, err)
	}

	// Compile this file to IRep
	cxt := r.mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.SetNoExec(true)
	cxt.Filename(absPath)

	parser, err := r.mrb.ParseString(string(content), cxt)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", absPath, err)
	}

	proc, err := r.mrb.GenerateCode(parser)
	if err != nil {
		return fmt.Errorf("generating bytecode for %s: %w", absPath, err)
	}

	decoder := NewDecoder(r.mrb)
	unit, err := decoder.Decode(proc.IRep())
	if err != nil {
		return fmt.Errorf("decoding irep for %s: %w", absPath, err)
	}

	relPath, err := filepath.Rel(rootDir, absPath)
	if err != nil {
		relPath = filepath.Base(absPath)
	}

	idx := *counter
	*counter++

	mod := &ModuleUnit{
		FilePath:  absPath,
		RelPath:   relPath,
		FuncName:  fmt.Sprintf("FileModule_%d", idx),
		Root:      unit,
		LoadPaths: r.loadPaths,
	}

	r.visited[absPath] = mod
	*list = append(*list, mod)

	// Discover dependencies
	matches := reqRegex.FindAllStringSubmatch(string(content), -1)
	currentDir := filepath.Dir(absPath)

	for _, m := range matches {
		kind := m[1]
		target := m[2]

		resolvedFile := ""
		if kind == "require_relative" {
			resolvedFile = r.findRelative(currentDir, target)
		} else {
			resolvedFile = r.findInLoadPaths(currentDir, target)
		}

		if resolvedFile != "" {
			if err := r.resolveRecursive(resolvedFile, rootDir, list, counter); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *DependencyResolver) findRelative(currentDir, target string) string {
	if filepath.IsAbs(target) {
		currentDir = ""
	}
	candidates := []string{
		filepath.Join(currentDir, target),
		filepath.Join(currentDir, target+".rb"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ""
}

func (r *DependencyResolver) findInLoadPaths(currentDir, target string) string {
	// First check relative to current dir
	if found := r.findRelative(currentDir, target); found != "" {
		return found
	}

	// Search load paths
	paths := append([]string{currentDir, "lib"}, r.loadPaths...)
	for _, lp := range paths {
		candidates := []string{
			filepath.Join(lp, target),
			filepath.Join(lp, target+".rb"),
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				abs, _ := filepath.Abs(c)
				return abs
			}
		}
	}
	return ""
}

// CanonicalKeys generates lookup keys for the module in the require registry.
func (m *ModuleUnit) CanonicalKeys() []string {
	keys := []string{m.RelPath}

	// Without extension: "foo/bar" for "foo/bar.rb"
	noExt := strings.TrimSuffix(m.RelPath, filepath.Ext(m.RelPath))
	if noExt != m.RelPath {
		keys = append(keys, noExt)
	}

	// If the file is inside any load path, add relative path to that load path!
	for _, lp := range m.LoadPaths {
		absLP, err := filepath.Abs(lp)
		if err == nil {
			if rel, err := filepath.Rel(absLP, m.FilePath); err == nil && !strings.HasPrefix(rel, "..") {
				keys = append(keys, rel)
				relNoExt := strings.TrimSuffix(rel, filepath.Ext(rel))
				if relNoExt != rel {
					keys = append(keys, relNoExt)
				}
			}
		}
	}

	// Base name: "bar.rb" and "bar"
	base := filepath.Base(m.RelPath)
	if base != m.RelPath {
		keys = append(keys, base)
		baseNoExt := strings.TrimSuffix(base, filepath.Ext(base))
		if baseNoExt != base {
			keys = append(keys, baseNoExt)
		}
	}

	return keys
}
