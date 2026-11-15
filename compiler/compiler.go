package compiler

import (
	"fmt"

	"github.com/oruby/oruby"
)

// Options configures the oruby-compiler transpilation process.
type Options struct {
	PackageName string   // Output Go package name (default: "main")
	Standalone  bool     // Generate a standalone executable with main()
	EmbedDirs   []string // Optional directory paths to embed using //go:embed (empty = files on disk)
	Debug       bool     // Emit line-stepping debug hooks for IDE / debugger integration
}

// Compiler coordinates parsing Ruby code or mruby bytecode and producing Go code.
type Compiler struct {
	opts Options
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

// CompileRuby parses and compiles Ruby source code into Go code.
func (c *Compiler) CompileRuby(mrb *oruby.MrbState, code string) (string, error) {
	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.SetNoExec(true)

	parser, err := mrb.ParseString(code, cxt)
	if err != nil {
		return "", fmt.Errorf("parsing ruby source: %w", err)
	}

	proc, err := mrb.GenerateCode(parser)
	if err != nil {
		return "", fmt.Errorf("generating bytecode: %w", err)
	}

	return c.CompileIrep(mrb, proc.IRep())
}

// CompileBundle resolves all required files starting from entryFile, precompiles them, and bundles them into Go source.
func (c *Compiler) CompileBundle(mrb *oruby.MrbState, entryFile string, loadPaths []string) (string, error) {
	resolver := NewDependencyResolver(mrb, loadPaths)
	modules, err := resolver.Resolve(entryFile)
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
