package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/oruby/oruby"
)

// Extensions
const (
	RitebinExt = ".mrb"
	CExt       = ".c"
	GoExt      = ".go"
)

type mrbcArgs struct {
	prog        string
	outfile     string
	initname    string
	argv        []string
	idx         int
	dumpStruct  bool
	checkSyntax bool
	verbose     bool
	removeLv    bool
	noExtOps    bool
	noOptimize  bool
	flags       uint8

	closer func()
}

func usage(name string) {
	msg := []string{
		"switches:",
		"-c           check syntax only",
		"-o<outfile>  place the output into <outfile>",
		"-v           print version number, then turn on verbose mode",
		"-g           produce debugging information",
		"-B<symbol>   binary <symbol> output in C language format",
		"-S           dump C struct (requires -B)",
		"-s           define <symbol> as static variable",
		"--remove-lv  remove local variables",
		"--no-ext-ops prohibit using OP_EXTs",
		"--no-optimize disable peephole optimization",
		"--verbose    run at verbose mode",
		"--version    print the version",
		"--copyright  print the copyright",
	}

	fmt.Printf("Usage: %v [switches] programfile\n", name)
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

	if (infile == "") || infile[len(infile)-1] != '.' {
		return infile + ext
	}
	return infile[:len(infile)-2] + ext
}

func parseArgs(mrb *oruby.MrbState, args *mrbcArgs) (bool, error) {
	args.prog = os.Args[0]
	flag.BoolVar(&args.checkSyntax, "c", false, "check syntax only")
	flag.StringVar(&args.outfile, "o", "", "place the output into <outfile>")
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

	if *debugInfo {
		args.flags |= oruby.DumpDebugInfo
	}
	if *dumpStatic {
		args.flags |= oruby.DumpStatic
	}

	if args.initname == "" {

	}

	if args.outfile == "" && !args.checkSyntax {
		if len(args.argv) == 1 {
			args.outfile = getOutfilename(args.argv[0], filepath.Ext(args.initname))
		} else {
			return false, fmt.Errorf("%v: output file should be specified to compile multiple files\n", args.prog)
		}
	}
	return true, nil
}

func setPartialHook(args *mrbcArgs) func(p oruby.MrbParserState) int {
	return func(p oruby.MrbParserState) int {
		if args.closer != nil {
			args.closer()
		}

		if args.idx >= len(args.argv) {
			p.SetS("")
			return -1
		}

		args.idx++
		fn := args.argv[args.idx]

		result, err := os.ReadFile(fn)
		if err != nil {
			fmt.Printf("%s: cannot open program file. (%s)\n", args.prog, fn)
			return -1
		}
		args.closer = p.SetS(string(result))
		p.SetFilename(fn)
		return 0
	}
}

func loadFile(mrb *oruby.MrbState, args *mrbcArgs) oruby.Value {
	var err error
	var data []byte
	var result oruby.Value
	input := args.argv[args.idx]

	c := mrb.MrbcContextNew()
	defer c.Free()

	c.SetDumpResult(args.verbose)
	c.SetNoExec(true)
	c.SetNoExtOps(args.noExtOps)
	c.SetNoOptimize(args.noOptimize)

	if input == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(input)
	}
	if err != nil {
		fmt.Printf("%s: cannot open program file %v: %v)\n", args.prog, input, err)
		return mrb.NilValue()
	}

	c.Filename(input)

	args.idx++
	if args.idx < len(args.argv) {
		c.PartialHook(setPartialHook(args))
	}

	result, err = c.LoadBytes(data)
	if err != nil {
		fmt.Printf("%s: parsing error: %v)\n", args.prog, err)
		return mrb.NilValue()
	}

	if mrb.UndefP(result) {
		return mrb.NilValue()
	}
	return result
}

func dumpFile(mrb *oruby.MrbState, wfp *os.File, outfile string, proc oruby.RProc, args *mrbcArgs) int {
	n := oruby.MrbDumpOK
	irep := proc.IRep()

	if args.removeLv {
		mrb.IrepRemoveLV(irep)
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

	resume, err := parseArgs(mrb, &args)
	if !resume {
		if err != nil {
			exitCode = exitFailure(err.Error())
			usage(os.Args[0])
		}
		return
	}

	if len(args.argv) == 0 {
		exitCode = exitFailure("%v: no program file given\n", args.prog)
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

	result = dumpFile(mrb, wfp, args.outfile, mrb.RProc(load), &args)

	if result != oruby.MrbDumpOK {
		exitCode = 1
	}
}
