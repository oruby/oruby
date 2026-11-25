package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"
)

// Extensions
const (
	RitebinExt = ".mrb"
	CExt       = ".c"
	GoExt      = ".go"
)

type mrbcArgs struct {
	prog         string
	outfile      string
	outdir       string
	orbfile      string
	entrypoint   string
	gemfile      string
	noGems       bool
	initname     string
	argv         []string
	idx          int
	dumpStruct   bool
	checkSyntax  bool
	verbose      bool
	removeLv     bool
	noExtOps     bool
	noOptimize   bool
	recursive    bool
	inplaceRb    bool
	rmSource     bool
	flags        uint8
	gatheredBins int
	gatheredLibs int

	closer func()
}

func usage(name string) {
	msg := []string{
		"subcommands:",
		"init [--docker] [--force] [dir]   create oruby.yml configuration file",
		"build [-c oruby.yml] [options]    compile and pack project using oruby.yml",
		"  --target=exe                    one executable: oruby, the gems' extensions, the .orb",
		"  --target=dev                    bin/oruby: oruby with the gems' extensions compiled in",
		"  --target=native                 hybrid Go AOT module with C and/or Go extensions",
		"  --ext-mode=c|go|auto            extension backend (default: c)",
		"  --go-ext=a,b                    use goext/ Go extensions for the listed gems",
		"  --c-ext=a,b                     force C extensions for the listed gems",
		"  --rbs=sig                       .rbs files/dirs for RBS-guided AOT specialization",
		"  --check-rbs                     fail --target=native on RBS arity/return mismatches",
		"",
		"switches:",
		"-c             check syntax only",
		"-o<outfile>    place the output into <outfile>",
		"--orb<file>    pack compiled .mrb files and assets into an .orb bundle",
		"--entry<file>  entrypoint script inside the .orb bundle",
		"--gemfile<f>   Gemfile to resolve and pack gems from (auto-detected if present)",
		"--no-gems      skip automatic Gemfile resolution when building an .orb bundle",
		"--outdir<dir>  output directory for compiled .mrb files (preserves tree layout)",
		"-r             recursively compile all .rb files in directory arguments",
		"--inplace-rb   write RITE bytecode into .rb filename (transparent to Dir.glob)",
		"--rm-source    remove original .rb source file after writing .mrb file",
		"-v             print version number, then turn on verbose mode",
		"-g             produce debugging information",
		"-B<symbol>     binary <symbol> output in C language format",
		"-S             dump C struct (requires -B)",
		"-s             define <symbol> as static variable",
		"--remove-lv    remove local variables",
		"--no-ext-ops   prohibit using OP_EXTs",
		"--no-optimize  disable peephole optimization",
		"--verbose      run at verbose mode",
		"--version      print the version",
		"--copyright    print the copyright",
	}

	fmt.Printf("Usage: %v [switches] programfile...\n", name)
	for _, line := range msg {
		fmt.Printf("  %v\n", line)
	}
}

func getOutfilename(infile, ext string) string {
	if ext == "" {
		ext = RitebinExt
	} else if ext != CExt && ext != GoExt {
		ext = RitebinExt
	}

	if infile == "" {
		return ext
	}
	if oldExt := filepath.Ext(infile); oldExt != "" {
		return strings.TrimSuffix(infile, oldExt) + ext
	}
	return infile + ext
}

func parseArgs(mrb *oruby.MrbState, args *mrbcArgs) (bool, error) {
	args.prog = os.Args[0]
	flag.BoolVar(&args.checkSyntax, "c", false, "check syntax only")
	flag.StringVar(&args.outfile, "o", "", "place the output into <outfile>")
	flag.StringVar(&args.orbfile, "orb", "", "pack compiled .mrb files and assets into an .orb bundle")
	flag.StringVar(&args.entrypoint, "entry", "", "entrypoint script inside .orb bundle")
	flag.StringVar(&args.gemfile, "gemfile", "", "Gemfile path to resolve and pack gems into .orb bundle")
	flag.BoolVar(&args.noGems, "no-gems", false, "skip automatic Gemfile resolution when building an .orb bundle")
	flag.StringVar(&args.outdir, "outdir", "", "output directory for compiled files")
	flag.BoolVar(&args.recursive, "r", false, "recursively compile .rb files in directories")
	flag.BoolVar(&args.inplaceRb, "inplace-rb", false, "write compiled RITE bytecode directly to .rb path")
	flag.BoolVar(&args.rmSource, "rm-source", false, "remove original .rb file after compiling to .mrb")
	v := flag.Bool("v", false, "print version number, then run in verbose mode")
	debugInfo := flag.Bool("g", false, "produce debugging information")
	flag.StringVar(&args.initname, "B", "", "binary <symbol> output in C language format")
	flag.BoolVar(&args.dumpStruct, "S", false, "dump C struct (requires -B)")
	dumpStatic := flag.Bool("s", false, " define <symbol> as static variable")
	flag.BoolVar(&args.removeLv, "remove-lv", false, "remove local variables")
	flag.BoolVar(&args.noExtOps, "no-ext-ops", false, "prohibit using OP_EXTs")
	flag.BoolVar(&args.noOptimize, "no-optimize", false, "disable peephole optimization")
	flag.BoolVar(&args.verbose, "verbose", false, "run in verbose mode")
	version := flag.Bool("version", false, "print the version")
	copyright := flag.Bool("copyright", false, "print the copyright")

	flag.Parse()
	args.argv = flag.Args()

	if *version {
		mrb.ShowVersion()
		return false, nil
	}
	if *copyright {
		mrb.ShowCopyright()
		return false, nil
	}

	if *v {
		mrb.ShowVersion()
		args.verbose = true
	}

	if *debugInfo || args.recursive {
		args.flags |= oruby.MrbDumpDebugInfo
	}
	if *dumpStatic {
		args.flags |= oruby.MrbDumpStatic
	}

	for _, a := range args.argv {
		if fi, err := os.Stat(a); err == nil && fi.IsDir() {
			args.recursive = true
			args.flags |= oruby.MrbDumpDebugInfo
		}
	}

	if args.orbfile == "" && strings.EqualFold(filepath.Ext(args.outfile), oruby.OrbExt) {
		args.orbfile = args.outfile
	}
	if args.orbfile != "" {
		args.flags |= oruby.MrbDumpDebugInfo
	}

	if args.outfile == "" && args.outdir == "" && args.orbfile == "" && !args.checkSyntax && !args.recursive && !args.inplaceRb {
		if len(args.argv) == 1 {
			args.outfile = getOutfilename(args.argv[0], filepath.Ext(args.initname))
		}
	}
	return true, nil
}

func compileInputToProc(mrb *oruby.MrbState, filename string, data []byte, args *mrbcArgs) (oruby.Value, error) {
	c := mrb.MrbCcontextNew()
	defer c.Free()

	c.SetDumpResult(args.verbose)
	c.SetNoExec(true)
	c.SetNoExtOps(args.noExtOps)
	c.SetNoOptimize(args.noOptimize)
	c.Filename(filename)

	if len(data) == 0 {
		data = []byte("nil")
	}

	result, err := c.LoadBytes(data)
	if err != nil {
		return mrb.NilValue(), err
	}
	if mrb.UndefP(result) {
		return mrb.NilValue(), fmt.Errorf("undefined compile result for %s", filename)
	}
	return result, nil
}

func loadFile(mrb *oruby.MrbState, args *mrbcArgs) oruby.Value {
	var combined bytes.Buffer
	firstInput := args.argv[0]

	for i, input := range args.argv {
		var data []byte
		var err error
		if input == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(input)
		}
		if err != nil {
			fmt.Printf("%s: cannot open program file %v: %v\n", args.prog, input, err)
			return mrb.NilValue()
		}
		if i > 0 {
			combined.WriteByte('\n')
		}
		combined.Write(data)
	}

	result, err := compileInputToProc(mrb, firstInput, combined.Bytes(), args)
	if err != nil {
		fmt.Printf("%s: parsing error: %v\n", args.prog, err)
		return mrb.NilValue()
	}
	return result
}

func dumpFile(mrb *oruby.MrbState, wfp *os.File, outfile string, proc oruby.RProc, args *mrbcArgs) int {
	n := oruby.MrbDumpOK
	irep := proc.IRep()

	if args.removeLv {
		args.flags |= oruby.MrbDumpNoLvar
	}

	if args.initname != "" {
		n, _ = mrb.DumpIrepCFunc(irep, args.flags, wfp, args.initname)
		if n == oruby.MrbDumpInvalidArgument {
			fmt.Printf("%v: invalid C language symbol name\n", args.initname)
		}
	} else {
		n, _ = mrb.DumpIrepBinary(irep, args.flags, wfp)
	}
	if n != oruby.MrbDumpOK {
		fmt.Printf("%v: error in mrb dump (%v) %d\n", args.prog, outfile, n)
	}
	return n
}

type compileTarget struct {
	inPath  string
	outPath string
}

func collectBatchTargets(args *mrbcArgs) ([]compileTarget, error) {
	var targets []compileTarget
	ext := RitebinExt
	if args.inplaceRb {
		ext = ".rb"
	} else if args.initname != "" {
		if e := filepath.Ext(args.initname); e == CExt || e == GoExt {
			ext = e
		}
	}

	addFile := func(inFile, baseDir string) {
		var outPath string
		if args.outdir != "" {
			rel := inFile
			if baseDir != "" {
				if r, err := filepath.Rel(baseDir, inFile); err == nil && !strings.HasPrefix(r, "..") {
					rel = r
				}
			}
			rel = filepath.Clean(rel)
			if filepath.IsAbs(rel) {
				rel = filepath.Base(rel)
			}
			outPath = filepath.Join(args.outdir, getOutfilename(rel, ext))
		} else if args.inplaceRb {
			outPath = inFile
		} else {
			outPath = getOutfilename(inFile, ext)
		}
		targets = append(targets, compileTarget{inPath: inFile, outPath: outPath})
	}

	for _, arg := range args.argv {
		fi, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("%s: cannot stat %s: %w", args.prog, arg, err)
		}
		if fi.IsDir() {
			baseDir := filepath.Dir(filepath.Clean(arg))
			err := filepath.WalkDir(arg, func(p string, d os.DirEntry, wErr error) error {
				if wErr != nil {
					return wErr
				}
				if d.IsDir() {
					return nil
				}
				if filepath.Ext(p) == ".rb" {
					addFile(p, baseDir)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else {
			addFile(arg, "")
		}
	}
	return targets, nil
}

func runBatch(mrb *oruby.MrbState, args *mrbcArgs) int {
	targets, err := collectBatchTargets(args)
	if err != nil {
		return exitFailure("%v\n", err)
	}
	if len(targets) == 0 {
		return exitFailure("%v: no .rb files found to compile\n", args.prog)
	}

	for _, t := range targets {
		data, err := os.ReadFile(t.inPath)
		if err != nil {
			return exitFailure("%s: cannot open program file %v: %v\n", args.prog, t.inPath, err)
		}
		// Skip files that are already compiled RITE bytecode
		if oruby.IsRiteBinary(data) {
			if args.outdir != "" && t.inPath != t.outPath {
				if err := os.MkdirAll(filepath.Dir(t.outPath), 0755); err != nil {
					return exitFailure("%s: cannot create output directory for %s: %v\n", args.prog, t.outPath, err)
				}
				if err := os.WriteFile(t.outPath, data, 0644); err != nil {
					return exitFailure("%s: cannot write output file %s: %v\n", args.prog, t.outPath, err)
				}
			}
			continue
		}

		ai := mrb.GCArenaSave()
		procVal, err := compileInputToProc(mrb, t.inPath, data, args)
		if err != nil || procVal.IsNil() {
			mrb.GCArenaRestore(ai)
			return exitFailure("%s: parsing error in %s: %v\n", args.prog, t.inPath, err)
		}

		if args.checkSyntax {
			log.Printf("%s:%s:Syntax OK\n", args.prog, t.inPath)
			mrb.GCArenaRestore(ai)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(t.outPath), 0755); err != nil {
			mrb.GCArenaRestore(ai)
			return exitFailure("%s: cannot create directory for %s: %v\n", args.prog, t.outPath, err)
		}

		// Preserve shebang if compiling in-place into an executable .rb script
		var prefix []byte
		if args.inplaceRb && bytes.HasPrefix(data, []byte("#!")) {
			if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
				prefix = data[:nl+1]
			}
		}

		var buf bytes.Buffer
		if len(prefix) > 0 {
			buf.Write(prefix)
		}
		if args.removeLv {
			args.flags |= oruby.MrbDumpNoLvar
		}
		st, dumpErr := mrb.DumpIrepBinary(mrb.RProc(procVal).IRep(), args.flags, &buf)
		mrb.GCArenaRestore(ai)
		if st != oruby.MrbDumpOK || dumpErr != nil {
			return exitFailure("%s: error dumping %s (%d): %v\n", args.prog, t.outPath, st, dumpErr)
		}

		mode := os.FileMode(0644)
		if fi, stErr := os.Stat(t.inPath); stErr == nil {
			mode = fi.Mode()
		}
		if err := os.WriteFile(t.outPath, buf.Bytes(), mode); err != nil {
			return exitFailure("%s: cannot write output file %s: %v\n", args.prog, t.outPath, err)
		}

		if args.rmSource && t.inPath != t.outPath {
			_ = os.Remove(t.inPath)
		}
	}
	return 0
}

func packSingleFileIntoOrb(mrb *oruby.MrbState, builder *oruby.OrbBuilder, inPath, vpath string, args *mrbcArgs) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", inPath, err)
	}
	mode := uint32(0644)
	if fi, stErr := os.Stat(inPath); stErr == nil {
		mode = uint32(fi.Mode().Perm())
	}

	ext := strings.ToLower(filepath.Ext(inPath))
	switch {
	case ext == ".mrb" || oruby.IsRiteBinary(data):
		if rite, ok := oruby.ExtractRiteBinary(data); ok {
			data = rite
		}
		// Store precompiled .mrb under its .rb virtual path for transparent require/Dir.glob
		if ext == ".mrb" {
			vpath = strings.TrimSuffix(vpath, filepath.Ext(vpath)) + ".rb"
		}
		return builder.AddEntry(vpath, oruby.OrbKindMrb, 0, mode, data)

	case ext == ".rb" || ext == ".ru" || ext == ".rake" || filepath.Base(inPath) == "Rakefile" || filepath.Base(inPath) == "Gemfile":
		vslash := filepath.ToSlash(vpath)
		if ext != ".rb" || strings.Contains(vslash, "/locale/") || strings.Contains(vslash, "/locales/") || strings.Contains(vslash, "/templates/") || strings.HasSuffix(vslash, ".rb.tt") {
			return builder.AddEntry(vpath, oruby.OrbKindRuby, 0, mode, data)
		}
		ai := mrb.GCArenaSave()
		defer mrb.GCArenaRestore(ai)

		procVal, err := compileInputToProc(mrb, vpath, data, args)
		if err != nil || procVal.IsNil() {
			return fmt.Errorf("compiling %s: %w", inPath, err)
		}
		var buf bytes.Buffer
		st, dumpErr := mrb.DumpIrepBinary(mrb.RProc(procVal).IRep(), args.flags, &buf)
		if st != oruby.MrbDumpOK || dumpErr != nil {
			return fmt.Errorf("dumping %s (%d): %v", inPath, st, dumpErr)
		}
		return builder.AddEntry(vpath, oruby.OrbKindMrb, 0, mode, buf.Bytes())

	default:
		// Executable ruby scripts in bin/ without an extension (e.g. bin/rails)
		if ext == "" && bytes.HasPrefix(data, []byte("#!")) && bytes.Contains(firstLineBytes(data), []byte("ruby")) {
			return builder.AddEntry(vpath, oruby.OrbKindRuby, oruby.OrbFlagExecutable, mode, data)
		}
		return builder.AddEntry(vpath, oruby.OrbKindAsset, 0, mode, data)
	}
}

func firstLineBytes(data []byte) []byte {
	if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
		return data[:nl]
	}
	return data
}

func runOrbBuild(mrb *oruby.MrbState, args *mrbcArgs) int {
	// If single directory argument has an oruby.yml and no explicit flags override, use runBuildSubcommand
	if len(args.argv) == 1 {
		if fi, err := os.Stat(args.argv[0]); err == nil && fi.IsDir() {
			cfgCand := filepath.Join(args.argv[0], DefaultConfigFilename)
			if _, cerr := os.Stat(cfgCand); cerr == nil {
				var buildArgs []string
				buildArgs = append(buildArgs, "-c", cfgCand, "--orb", args.orbfile)
				if args.entrypoint != "" {
					buildArgs = append(buildArgs, "--entry", args.entrypoint)
				}
				if args.noGems {
					buildArgs = append(buildArgs, "--no-gems")
				}
				if args.verbose {
					buildArgs = append(buildArgs, "--verbose")
				}
				return runBuildSubcommand(mrb, args.prog, buildArgs)
			}
		}
	}

	var hdrFlags uint16
	if args.flags&oruby.MrbDumpDebugInfo != 0 {
		hdrFlags |= oruby.OrbHdrDebugInfo
	}
	if args.removeLv {
		args.flags |= oruby.MrbDumpNoLvar
	}

	builder := oruby.NewOrbBuilder(hdrFlags)
	if args.entrypoint != "" {
		builder.SetEntrypoint(args.entrypoint)
	}

	absOut, _ := filepath.Abs(args.orbfile)

	// Determine common base directory when packing a single directory argument
	commonRoot := ""
	if len(args.argv) == 1 {
		if fi, err := os.Stat(args.argv[0]); err == nil && fi.IsDir() {
			commonRoot, _ = filepath.Abs(args.argv[0])
		}
	}
	if commonRoot == "" {
		commonRoot, _ = os.Getwd()
	}

	toVirtualPath := func(inPath, baseDir string) string {
		absIn, err := filepath.Abs(inPath)
		if err != nil {
			absIn = inPath
		}
		if baseDir != "" {
			if rel, rerr := filepath.Rel(baseDir, absIn); rerr == nil && !strings.HasPrefix(rel, "..") {
				return oruby.NormalizeOrbPath(rel)
			}
		}
		if commonRoot != "" {
			if rel, rerr := filepath.Rel(commonRoot, absIn); rerr == nil && !strings.HasPrefix(rel, "..") {
				return oruby.NormalizeOrbPath(rel)
			}
		}
		return oruby.NormalizeOrbPath(inPath)
	}

	packFileWithVirtual := func(inPath, vpath string) error {
		if absIn, err := filepath.Abs(inPath); err == nil && absIn == absOut {
			return nil
		}
		if vpath == "" {
			return nil
		}
		return packSingleFileIntoOrb(mrb, builder, inPath, vpath, args)
	}

	packFile := func(inPath, baseDir string) error {
		return packFileWithVirtual(inPath, toVirtualPath(inPath, baseDir))
	}

	// Resolve and pack Gemfile gems first (so app files under lib/ take precedence if overlapping)
	if !args.noGems {
		gemfilePath := args.gemfile
		if gemfilePath == "" && commonRoot != "" {
			cand := filepath.Join(commonRoot, "Gemfile")
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				gemfilePath = cand
			}
		}
		if gemfilePath != "" {
			if err := packGemfileGems(mrb, builder, gemfilePath, commonRoot, args, packFileWithVirtual); err != nil {
				return exitFailure("%s: %v\n", args.prog, err)
			}
		}
	}

	firstFileEntry := ""
	for _, arg := range args.argv {
		fi, err := os.Stat(arg)
		if err != nil {
			return exitFailure("%s: cannot stat %s: %v\n", args.prog, arg, err)
		}
		if fi.IsDir() {
			baseDir := commonRoot
			if len(args.argv) > 1 {
				absArg, _ := filepath.Abs(arg)
				baseDir = filepath.Dir(absArg)
			}
			err := filepath.WalkDir(arg, func(p string, d os.DirEntry, wErr error) error {
				if wErr != nil {
					return wErr
				}
				if d.IsDir() {
					switch d.Name() {
					case ".git", "node_modules", "tmp", "log", ".bundle":
						return filepath.SkipDir
					}
					vdir := toVirtualPath(p, baseDir)
					if vdir == "vendor/bundle" || vdir == "vendor/cache" {
						return filepath.SkipDir
					}
					if vdir == "lib" || strings.HasSuffix(vdir, "/lib") {
						_ = builder.AddDir(vdir, true)
					}
					return nil
				}
				return packFile(p, baseDir)
			})
			if err != nil {
				return exitFailure("%s: %v\n", args.prog, err)
			}
		} else {
			if filepath.Base(arg) == "Gemfile" && !args.noGems && args.gemfile == "" {
				absGemfile, _ := filepath.Abs(arg)
				if err := packGemfileGems(mrb, builder, absGemfile, filepath.Dir(absGemfile), args, packFileWithVirtual); err != nil {
					return exitFailure("%s: %v\n", args.prog, err)
				}
			}
			if firstFileEntry == "" && (filepath.Ext(arg) == ".rb" || filepath.Ext(arg) == ".mrb") {
				vpath := toVirtualPath(arg, "")
				if filepath.Ext(vpath) == ".mrb" {
					vpath = strings.TrimSuffix(vpath, ".mrb") + ".rb"
				}
				firstFileEntry = vpath
			}
			if err := packFile(arg, ""); err != nil {
				return exitFailure("%s: %v\n", args.prog, err)
			}
		}
	}

	if args.entrypoint == "" && firstFileEntry != "" {
		builder.SetEntrypoint(firstFileEntry)
	}

	if err := builder.WriteFile(args.orbfile); err != nil {
		return exitFailure("%s: cannot write orb bundle %s: %v\n", args.prog, args.orbfile, err)
	}
	return 0
}

func exitFailure(format string, v ...any) int {
	log.Printf(format, v...)
	return 1
}

func main() {
	var err error
	var result int
	exitCode := 0
	defer func() { os.Exit(exitCode) }()

	args := mrbcArgs{}

	mrb := oruby.MrbOpen()
	if mrb == nil {
		exitCode = exitFailure("Invalid mrb_state, exiting mrbc\n")
		return
	}
	defer mrb.Close()

	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "init":
			exitCode = runInitSubcommand(os.Args[2:])
			return
		case "build":
			exitCode = runBuildSubcommand(mrb, os.Args[0], os.Args[2:])
			return
		}
	}

	resume, err := parseArgs(mrb, &args)
	if !resume {
		if err != nil {
			exitCode = exitFailure("%s", err.Error())
			usage(os.Args[0])
		}
		return
	}

	if len(args.argv) == 0 {
		if _, err := os.Stat(DefaultConfigFilename); err == nil {
			exitCode = runBuildSubcommand(mrb, args.prog, nil)
			return
		}
		exitCode = exitFailure("%v: no program file given\n", args.prog)
		return
	}

	if args.orbfile != "" {
		exitCode = runOrbBuild(mrb, &args)
		return
	}

	// Batch / recursive / multi-file-to-individual-mrb mode
	isBatch := args.recursive || args.outdir != "" || args.inplaceRb || args.rmSource ||
		(len(args.argv) > 1 && args.outfile == "")
	if isBatch {
		exitCode = runBatch(mrb, &args)
		return
	}

	args.idx = 0
	load := loadFile(mrb, &args)
	if load.IsNil() {
		exitCode = 1
		return
	}

	if args.checkSyntax {
		log.Printf("%s:%s:Syntax OK\n", args.prog, args.argv[0])
		return
	}

	var wfp *os.File

	if args.outfile == "" {
		exitCode = exitFailure("Output file is required\n")
		return
	} else if args.outfile == "-" {
		wfp = os.Stdout
	} else if wfp, err = os.Create(args.outfile); err != nil {
		exitCode = exitFailure("%v: cannot open output file %v: %v\n", args.prog, args.outfile, err)
		return
	}
	defer func() {
		if wfp != nil && wfp != os.Stdout {
			_ = wfp.Close()
		}
	}()

	result = dumpFile(mrb, wfp, args.outfile, mrb.RProc(load), &args)

	if result != oruby.MrbDumpOK {
		exitCode = 1
	}
}
