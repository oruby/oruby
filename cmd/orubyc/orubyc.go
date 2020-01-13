package main

import (
	"flag"
	"fmt"
	"github.com/oruby/oruby"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

const RitebinExt = ".mrb"
const CExt = ".c"
const GoExt = ".go"

type mrbcArgs struct {
	idx         int
	argv        []string
	prog        string
	outfile     string
	initname    string
	checkSyntax bool
	verbose     bool
	removeLv    bool
	flags       uint8
	closer      func()
}

func usage(name string) {
	msg := []string{
		"switches:",
		"-c           check syntax only",
		"-o<outfile>  place the output into <outfile>",
		"-v           print version number, then turn on verbose mode",
		"-g           produce debugging information",
		"-B<symbol>   binary <symbol> output in C language format",
		"-e           generate little endian iseq data",
		"-E           generate big endian iseq data",
		"--remove-lv  remove local variables",
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

func endianString() string {
	if oruby.BigEndianP() {
		return "big"
	}
	return "little"
}

func parseArgs(mrb *oruby.MrbState, args *mrbcArgs) bool {
	args.prog = os.Args[0]
	flag.StringVar(&args.outfile,"o", "", "place the output into <outfile>")
	flag.StringVar(&args.initname,"B", "", "binary <symbol> output in C language format")
	flag.BoolVar(&args.checkSyntax,"c", false, "check syntax only")
	flag.BoolVar(&args.removeLv,"remove-lv", false, "remove local variables")
	v    := flag.Bool("v", false, "print version number, then run in verbose mode")
	flag.BoolVar(&args.verbose,"verbose", false, "run in verbose mode")
	version := flag.Bool("version", false, "print the version")
	copyright := flag.Bool("copyright", false, "print the copyright")
	debugInfo := flag.Bool("g", false, "produce debugging information")
	bigEndian := flag.Bool("E", false, "generate big endian iseq data")
	lilEndian := flag.Bool("e", false, "generate little endian iseq data")

	flag.Parse()
	args.argv = flag.Args()

	if *version {
		mrb.ShowVersion()
		os.Exit(0)
		return false
	}
	if *copyright {
		mrb.ShowCopyright()
		os.Exit(0)
		return false
	}

	if *v {
		mrb.ShowVersion()
		args.verbose = true
	}

	if *debugInfo {
		args.flags |= oruby.DumpDebugInfo
	}
	if *bigEndian {
		args.flags = oruby.DumpEndianBig | (args.flags & ^oruby.DumpEndianMask)
	}
	if *lilEndian {
		args.flags = oruby.DumpEndianLil | (args.flags & ^oruby.DumpEndianMask)
	}

	if args.verbose && args.initname != "" && (args.flags & oruby.DumpEndianMask) == 0 {
		fmt.Printf("%v: generating %v endian C file. specify -e/-E for cross compiling.\n",
			args.prog, endianString())
	}
	return true
}

func setPartialHook(args *mrbcArgs) func(p oruby.MrbParserState)int {
	return func(p oruby.MrbParserState) int {
		if args.closer != nil {
			args.closer()
		}

		if args.idx >= len(args.argv) {
			p.SetS("")
			return -1
		}

		args.idx += 1
		fn := args.argv[args.idx]

		result, err := ioutil.ReadFile(fn)
		if err != nil {
			fmt.Printf("%s: cannot open program file. (%s)\n",  args.prog, fn)
			return -1
		}
		args.closer = p.SetS(string(result))
		p.SetFilename(fn)
		return 0
	}
}

func loadFile(mrb *oruby.MrbState, args *mrbcArgs) oruby.Value {
	var result oruby.Value
	var err error
	var data []byte

	c := mrb.MrbcContextNew()
	defer c.Free()

	c.SetDumpResult(args.verbose)
	c.SetNoExec(true)

	input := args.argv[args.idx]

	if input == "-" {
		data, err = ioutil.ReadAll(os.Stdin)
	} else {
		data, err = ioutil.ReadFile(input)
	}
	if err != nil {
		fmt.Printf("%s: cannot open program file %v: %v)\n", args.prog, input, err)
		return mrb.NilValue()
	}

	c.Filename(input)

	args.idx += 1
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
		n, _ = mrb.DumpIrepCFunc(irep, uint8(args.flags), wfp, args.initname)
		if n == oruby.MrbDumpInvalidArgument {
			fmt.Printf("%v: invalid C language symbol name\n", args.initname)
		}
	} else {
		n, _ = mrb.DumpIrepBinary(irep, uint8(args.flags), wfp)
	}
	if n != oruby.MrbDumpOK {
		fmt.Printf("%v: error in mrb dump (%v) %d\n", args.prog, outfile, n)
	}
	return n
}

func main() {
	var wfp *os.File
    var err error
	var result int
	args := mrbcArgs{}

	mrb := oruby.MrbOpen()
	if mrb == nil {
		log.Fatal("Invalid mrb_state, exiting mrbc\n")
		return
	}
	defer mrb.Close()

	ok := parseArgs(mrb, &args)
	if !ok {
		usage(os.Args[0])
		os.Exit(1)
		return
	}

	if len(args.argv) == 0 {
		mrb.Close()
		log.Fatalf("%v: no program file given\n", args.prog)
		return
	}

	if args.outfile == "" && !args.checkSyntax {
		if len(args.argv) == 1 {
			args.outfile = getOutfilename(args.argv[0], filepath.Ext(args.initname))
		} else {
			mrb.Close()
			log.Fatalf("%v: output file should be specified to compile multiple files\n", args.prog)
			return
		}
	}

	args.idx = 0
	load := loadFile(mrb, &args)
	if load.IsNil() {
		mrb.Close()
		os.Exit(1)
		return
	}

	if args.checkSyntax {
		mrb.Close()
		log.Fatalf("%s:%s:Syntax OK\n", args.prog, args.argv[0])
		return
	}

	if args.outfile == "" {
		mrb.Close()
		log.Fatalf("Output file is required\n")
		return
	} else if args.outfile == "-" {
		wfp = os.Stdout
	} else if wfp, err = os.Create(args.outfile); err != nil {
		mrb.Close()
		log.Fatalf("%v: cannot open output file %v: %v\n", args.prog, args.outfile, err)
		return
	}

	result = dumpFile(mrb, wfp, args.outfile, mrb.ProcPtr(load), &args)
	_=wfp.Close()

	if result != oruby.MrbDumpOK {
		mrb.Close()
		os.Exit(1)
	}
}
