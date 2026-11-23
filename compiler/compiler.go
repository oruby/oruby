package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/oruby/oruby"
)

// IsRiteBinary reports whether data starts with a valid mruby RiteVM binary (.mrb) header.
func IsRiteBinary(data []byte) bool {
	return oruby.IsRiteBinary(data)
}

// Options configures the oruby-compiler transpilation process.
type Options struct {
	PackageName string   // Output Go package name (default: "main")
	Standalone  bool     // Generate a standalone executable with main()
	EmbedDirs   []string // Optional directory paths to embed using //go:embed (empty = files on disk)
	Debug       bool     // Emit line-stepping debug hooks for IDE / debugger integration
}

// Compiler coordinates parsing Ruby code or mruby bytecode and producing Go code.
type Compiler struct {
	opts    Options
	Skipped []error // extra bundle files skipped by the last CompileBundleWith
}

// New creates a new Compiler with the given options.
func New(opts Options) *Compiler {
	if opts.PackageName == "" {
		opts.PackageName = "main"
	}
	return &Compiler{opts: opts}
}

// CompileIrep takes an existing MrbIrep and emits the transpiled Go code.
func (c *Compiler) CompileIrep(mrb *oruby.MrbState, irep oruby.MrbIrep) (string, error) {
	decoder := NewDecoder(mrb)
	unit, err := decoder.Decode(irep)
	if err != nil {
		return "", fmt.Errorf("decoding irep: %w", err)
	}

	codegen := NewCodeGen(CodeGenOptions{
		PackageName: c.opts.PackageName,
		Standalone:  c.opts.Standalone,
		EmbedDirs:   c.opts.EmbedDirs,
		Debug:       c.opts.Debug,
	})

	return codegen.Generate(unit)
}

// CompileBytecode decodes precompiled mruby RiteVM bytecode (.mrb) and emits transpiled Go code.
func (c *Compiler) CompileBytecode(mrb *oruby.MrbState, bytecode []byte) (string, error) {
	irep, err := mrb.ReadIrep(bytecode)
	if err != nil {
		return "", fmt.Errorf("reading mruby bytecode: %w", err)
	}
	return c.CompileIrep(mrb, irep)
}

// CompileFile compiles either a precompiled .mrb bytecode file or a .rb Ruby source file into Go code.
func (c *Compiler) CompileFile(mrb *oruby.MrbState, filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filePath, err)
	}
	if IsRiteBinary(data) {
		return c.CompileBytecode(mrb, data)
	}
	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.SetNoExec(true)
	cxt.Filename(filePath)

	parser, err := mrb.ParseString(string(data), cxt)
	if err != nil {
		return "", fmt.Errorf("parsing ruby source %s: %w", filePath, err)
	}
	defer parser.Free()

	proc, err := mrb.GenerateCode(parser)
	if err != nil {
		return "", fmt.Errorf("generating bytecode for %s: %w", filePath, err)
	}
	return c.CompileIrep(mrb, proc.IRep())
}

// CompileRuby parses and compiles Ruby source code into Go code.
func (c *Compiler) CompileRuby(mrb *oruby.MrbState, code string) (string, error) {
	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.SetNoExec(true)

	parser, err := mrb.ParseString(code, cxt)
	if err != nil {
		return "", fmt.Errorf("parsing ruby source: %w", err)
	}
	defer parser.Free()

	proc, err := mrb.GenerateCode(parser)
	if err != nil {
		return "", fmt.Errorf("generating bytecode: %w", err)
	}

	return c.CompileIrep(mrb, proc.IRep())
}

// CompileBundle resolves all required files starting from entryFile, precompiles them, and bundles them into Go source.
func (c *Compiler) CompileBundle(mrb *oruby.MrbState, entryFile string, loadPaths []string) (string, error) {
	return c.CompileBundleWith(mrb, entryFile, nil, loadPaths)
}

// CompileBundleWith compiles entryFile with its dependencies and additional
// files (e.g. spec files loaded dynamically with Kernel#load) into one bundle.
// Extra files which fail to parse are skipped; see Skipped.
func (c *Compiler) CompileBundleWith(mrb *oruby.MrbState, entryFile string, extra []string, loadPaths []string) (string, error) {
	resolver := NewDependencyResolver(mrb, loadPaths)
	modules, skipped, err := resolver.ResolveWith(entryFile, extra)
	c.Skipped = skipped
	if err != nil {
		return "", fmt.Errorf("resolving dependencies for %s: %w", entryFile, err)
	}

	codegen := NewCodeGen(CodeGenOptions{
		PackageName: c.opts.PackageName,
		Standalone:  c.opts.Standalone,
		EmbedDirs:   c.opts.EmbedDirs,
		Debug:       c.opts.Debug,
	})

	return codegen.GenerateBundle(modules)
}

// CompilePackageDir resolves entryFile (whether .orb, .mrb, or .rb) and its
// dependencies, and writes a multi-file Go package (aot_runtime.go +
// aot_mod_NNN.go) into outDir. It returns the sorted list of written file paths.
func (c *Compiler) CompilePackageDir(mrb *oruby.MrbState, entryFile string, extra []string, loadPaths []string, outDir string) ([]string, error) {
	resolver := NewDependencyResolver(mrb, loadPaths)
	modules, skipped, err := resolver.ResolveWith(entryFile, extra)
	c.Skipped = skipped
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies for %s: %w", entryFile, err)
	}

	codegen := NewCodeGen(CodeGenOptions{
		PackageName: c.opts.PackageName,
		Standalone:  c.opts.Standalone,
		EmbedDirs:   c.opts.EmbedDirs,
		Debug:       c.opts.Debug,
	})

	files, err := codegen.GeneratePackageFiles(modules)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output directory %s: %w", outDir, err)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	written := make([]string, 0, len(names))
	for _, name := range names {
		full := filepath.Join(outDir, name)
		content := []byte(files[name])
		// Skip rewriting unchanged module files to preserve timestamps for Go build cache
		if existing, rerr := os.ReadFile(full); rerr == nil && string(existing) == files[name] {
			written = append(written, full)
			continue
		}
		if err := os.WriteFile(full, content, 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", full, err)
		}
		written = append(written, full)
	}
	return written, nil
}
