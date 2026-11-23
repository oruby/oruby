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
	FilePath  string    // Absolute path on disk (or virtual path anchor)
	RelPath   string    // Normalized relative path (e.g. "calculator.rb", "models/user.rb")
	FuncName  string    // Name of the Go entry function
	Root      *IRepUnit // Root IRepUnit of this file
	LoadPaths []string  // Search paths for resolving relative require paths
	Hash32    uint32    // FNV-1a 32-bit hash of the module bytecode/source payload
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

	if oruby.IsOrbBinary(content) {
		return r.resolveOrbBytes(absPath, rootDir, content, list, counter)
	}

	var irep oruby.MrbIrep
	isBytecode := IsRiteBinary(content)
	if isBytecode {
		irep, err = r.mrb.ReadIrep(content)
		if err != nil {
			return fmt.Errorf("reading bytecode %s: %w", absPath, err)
		}
	} else {
		// Compile this file to IRep
		cxt := r.mrb.MrbCcontextNew()
		defer cxt.Free()
		cxt.SetNoExec(true)
		cxt.Filename(absPath)

		parser, err := r.mrb.ParseString(string(content), cxt)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", absPath, err)
		}
		defer parser.Free()

		proc, err := r.mrb.GenerateCode(parser)
		if err != nil {
			return fmt.Errorf("generating bytecode for %s: %w", absPath, err)
		}
		irep = proc.IRep()
	}

	decoder := NewDecoder(r.mrb)
	unit, err := decoder.Decode(irep)
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

	// Discover dependencies from bytecode instructions (and source regex if source text)
	reqs := extractUnitRequires(unit)
	if !isBytecode {
		matches := reqRegex.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			reqs = append(reqs, requireRef{kind: m[1], target: m[2]})
		}
	}
	currentDir := filepath.Dir(absPath)
	seenReqs := make(map[requireRef]bool, len(reqs))

	for _, req := range reqs {
		if seenReqs[req] {
			continue
		}
		seenReqs[req] = true

		resolvedFile := ""
		if req.kind == "require_relative" {
			resolvedFile = r.findRelative(currentDir, req.target)
		} else {
			resolvedFile = r.findInLoadPaths(currentDir, req.target)
		}

		if resolvedFile != "" {
			if err := r.resolveRecursive(resolvedFile, rootDir, list, counter); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *DependencyResolver) resolveOrbBytes(absOrbPath, rootDir string, content []byte, list *[]*ModuleUnit, counter *int) error {
	orb, err := oruby.ParseOrb(absOrbPath, content)
	if err != nil {
		return fmt.Errorf("parsing orb archive %s: %w", absOrbPath, err)
	}

	orbLoadPaths := append([]string{}, r.loadPaths...)
	orbLoadPaths = append(orbLoadPaths, rootDir, filepath.Join(rootDir, "lib"))
	for i := range orb.Entries {
		e := &orb.Entries[i]
		if e.Flags&oruby.OrbFlagLoadPath != 0 && e.Kind == oruby.OrbKindDir {
			orbLoadPaths = append(orbLoadPaths, filepath.Join(rootDir, filepath.FromSlash(e.Path)))
		}
	}

	// Order entries so the designated entrypoint comes first (modules[0])
	order := make([]int, 0, len(orb.Entries))
	epIdx := int(orb.Header.EntrypointIdx)
	if epIdx >= 0 && epIdx < len(orb.Entries) {
		order = append(order, epIdx)
	}
	for i := range orb.Entries {
		if i != epIdx {
			order = append(order, i)
		}
	}

	decoder := NewDecoder(r.mrb)
	for _, i := range order {
		e := &orb.Entries[i]
		if e.Kind != oruby.OrbKindMrb && e.Kind != oruby.OrbKindRuby {
			continue
		}
		vfile := filepath.Join(rootDir, filepath.FromSlash(e.Path))
		if _, ok := r.visited[vfile]; ok {
			continue
		}

		payload := orb.EntryData(e)
		var irep oruby.MrbIrep
		if e.Kind == oruby.OrbKindMrb || IsRiteBinary(payload) {
			irep, err = r.mrb.ReadIrep(payload)
			if err != nil {
				return fmt.Errorf("reading bytecode for %s in %s: %w", e.Path, absOrbPath, err)
			}
		} else {
			cxt := r.mrb.MrbCcontextNew()
			cxt.SetNoExec(true)
			cxt.Filename(vfile)
			parser, perr := r.mrb.ParseString(string(payload), cxt)
			if perr != nil {
				cxt.Free()
				return fmt.Errorf("parsing %s in %s: %w", e.Path, absOrbPath, perr)
			}
			proc, gerr := r.mrb.GenerateCode(parser)
			parser.Free()
			cxt.Free()
			if gerr != nil {
				return fmt.Errorf("generating bytecode for %s in %s: %w", e.Path, absOrbPath, gerr)
			}
			irep = proc.IRep()
		}

		unit, err := decoder.Decode(irep)
		if err != nil {
			return fmt.Errorf("decoding irep for %s in %s: %w", e.Path, absOrbPath, err)
		}

		idx := *counter
		*counter++
		mod := &ModuleUnit{
			FilePath:  vfile,
			RelPath:   filepath.FromSlash(e.Path),
			FuncName:  fmt.Sprintf("FileModule_%d", idx),
			Root:      unit,
			LoadPaths: orbLoadPaths,
			Hash32:    e.Hash32,
		}
		r.visited[vfile] = mod
		*list = append(*list, mod)
	}
	r.visited[absOrbPath] = &ModuleUnit{FilePath: absOrbPath}
	return nil
}

type requireRef struct {
	kind   string
	target string
}

// extractUnitRequires scans decoded bytecode instructions across an IRepUnit tree
// to find static require and require_relative calls, enabling dependency resolution
// directly from precompiled .mrb files without needing Ruby source text.
func extractUnitRequires(u *IRepUnit) []requireRef {
	if u == nil {
		return nil
	}
	var out []requireRef
	jumpTargets := make(map[int]bool)
	for _, ins := range u.Instructions {
		if ins.IsJump() && ins.Target >= 0 {
			jumpTargets[ins.Target] = true
		}
	}

	regStr := make(map[uint32]string)
	for _, ins := range u.Instructions {
		if jumpTargets[ins.Offset] {
			clear(regStr)
		}
		switch ins.Op {
		case OpSTRING, OpLOADL:
			if int(ins.B) < len(u.Pool) {
				if s, ok := u.Pool[ins.B].(string); ok {
					regStr[ins.A] = s
					continue
				}
			}
			delete(regStr, ins.A)
		case OpMOVE:
			if s, ok := regStr[uint32(ins.B)]; ok {
				regStr[ins.A] = s
			} else {
				delete(regStr, ins.A)
			}
		case OpDEBUG:
			// chilled string debug hint leaves register unchanged
		case OpSEND0, OpSSEND0:
			sym := ""
			if int(ins.B) < len(u.Syms) {
				sym = u.Syms[ins.B]
			}
			// frozen string literal unary -@ or .freeze preserves the string value
			if sym != "-@" && sym != "freeze" {
				delete(regStr, ins.A)
			}
		case OpSSEND, OpSEND:
			sym := ""
			if int(ins.B) < len(u.Syms) {
				sym = u.Syms[ins.B]
			}
			n := int(ins.C & 0xf)
			nk := int((ins.C >> 4) & 0xf)
			if sym == "__oruby_debug_fstring" && n == 2 && nk == 0 {
				if s, ok := regStr[ins.A+1]; ok {
					regStr[ins.A] = s
					continue
				}
			}
			if (sym == "require" || sym == "require_relative") && n == 1 && nk == 0 {
				if target, ok := regStr[ins.A+1]; ok && target != "" {
					out = append(out, requireRef{kind: sym, target: target})
				}
			}
			delete(regStr, ins.A)
		default:
			delete(regStr, ins.A)
		}
	}

	for _, child := range u.Children {
		out = append(out, extractUnitRequires(child)...)
	}
	return out
}

func candidateFiles(dir, target string) []string {
	base := filepath.Join(dir, target)
	candidates := []string{
		base,
		base + ".rb",
		base + ".mrb",
	}
	if strings.HasSuffix(target, ".rb") {
		candidates = append(candidates, strings.TrimSuffix(base, ".rb")+".mrb")
	} else if strings.HasSuffix(target, ".mrb") {
		candidates = append(candidates, strings.TrimSuffix(base, ".mrb")+".rb")
	}
	return candidates
}

func (r *DependencyResolver) findRelative(currentDir, target string) string {
	if filepath.IsAbs(target) {
		currentDir = ""
	}
	for _, c := range candidateFiles(currentDir, target) {
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
		for _, c := range candidateFiles(lp, target) {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				abs, _ := filepath.Abs(c)
				return abs
			}
		}
	}
	return ""
}

func addPathVariants(keys *[]string, p string) {
	if p == "" {
		return
	}
	*keys = append(*keys, p)
	ext := filepath.Ext(p)
	if ext == ".rb" || ext == ".mrb" {
		noExt := strings.TrimSuffix(p, ext)
		*keys = append(*keys, noExt, noExt+".rb", noExt+".mrb")
	}
}

// CanonicalKeys generates lookup keys for the module in the require registry.
func (m *ModuleUnit) CanonicalKeys() []string {
	var keys []string
	addPathVariants(&keys, m.RelPath)
	addPathVariants(&keys, m.FilePath)

	// If the file is inside any load path, add relative path to that load path!
	for _, lp := range m.LoadPaths {
		absLP, err := filepath.Abs(lp)
		if err == nil {
			if rel, err := filepath.Rel(absLP, m.FilePath); err == nil && !strings.HasPrefix(rel, "..") {
				addPathVariants(&keys, rel)
			}
		}
	}

	// Base name: "bar.rb", "bar.mrb", and "bar"
	base := filepath.Base(m.RelPath)
	if base != m.RelPath {
		addPathVariants(&keys, base)
	}

	return keys
}
